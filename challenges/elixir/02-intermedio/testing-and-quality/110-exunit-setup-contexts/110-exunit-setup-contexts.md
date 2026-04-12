# ExUnit setup, setup_all and the context map

**Project**: `setup_contexts` — a `UserRepo` backed by an Agent, tested with
per-test and per-module fixtures via `setup`, `setup_all`, and context tags.

---

## Project context

Anything non-trivial needs fixtures: a started process, a seeded dataset,
an authenticated user. ExUnit's answer is a layered setup system: `setup_all`
for module-wide, `setup` for per-test, plus the context map that flows
through both.

Done right, this eliminates three bad habits at once:
1. Copy-pasting fixture code across tests.
2. Relying on global `Application.put_env` mutations.
3. Using `Process.sleep/1` to wait for a process to be "ready".

## Why ExUnit setup and not X

**Why not module-level `@fixture` attributes?** Module attributes are
evaluated once at compile time — they can't hold a live pid or a freshly
seeded process. They're data; fixtures are procedures.

**Why not a macro-based "factories" library?** Most of what ExUnit supports
via `setup`/`setup_all` + tags needs no DSL. Factories pay off when you
need graph-shaped object construction; for a pid + seed they're overkill.

**Why not `Process.put` globals?** Because `async: true` tests run in
parallel processes, and a global write-path destroys test isolation.

Project structure:

```
setup_contexts/
├── lib/
│   └── user_repo.ex
├── test/
│   ├── user_repo_test.exs
│   └── test_helper.exs
└── mix.exs
```

---

## Core concepts

### 1. `setup_all` vs `setup`

- `setup_all` runs **once** per module, in its own process. Great for
  expensive setup whose result is immutable (seed data, compiled schemas).
- `setup` runs **before every test**. Great for per-test state that must
  be isolated.

Both can return `:ok`, `{:ok, context_map}`, or a plain map. The returned
map is merged into the test's context.

### 2. The context map

Every test receives a context — a map ExUnit populates with `:test`, `:case`,
`:file`, `:line`, `:module`, plus anything your setups returned. Destructure
it in the test signature:

```elixir
test "creates a user", %{repo: repo} do
  ...
end
```

If you don't need the context, just write `test "name" do ... end`.

### 3. Tag-driven setup with `@tag`

Tags attach metadata to a test. Setups can pattern-match on tags to branch:

```elixir
@tag seed: 10
test "with 10 users", %{repo: repo} do ... end

setup context do
  if n = context[:seed], do: seed(context.repo, n)
  :ok
end
```

This is the idiomatic way to parameterize fixtures without a test factory DSL.

### 4. `on_exit/1` for cleanup

`setup` blocks can register cleanup callbacks with `on_exit/1`. They run
after the test finishes, regardless of pass/fail — the right place to
kill processes, delete files, or drop temp tables. For the higher-level
`start_supervised!/1`, see the supervised-start exercise.

---

## Design decisions

**Option A — Per-test `setup`, fresh state every time**
- Pros: Full isolation; no test depends on another.
- Cons: Per-test cost of starting the Agent.

**Option B — `setup_all` once, shared state across the module** (rejected)
- Pros: Faster overall.
- Cons: Mutation between tests; breaks under `async: true`.

**Option C — `setup_all` for immutable data + `setup` for mutable pids** (chosen)
- Pros: Best of both — expensive constants computed once, per-test pids
  isolated.
- Cons: Two layers to understand.

→ Chose **C** because it's the idiomatic ExUnit shape. `setup_all` carries
seed names (immutable), `setup` starts the Agent (mutable).

---

## Implementation

### Step 1: Create the project

```bash
mix new setup_contexts
cd setup_contexts
```

### Step 2: `lib/user_repo.ex`

```elixir
defmodule UserRepo do
  @moduledoc """
  A trivial in-memory user repository backed by an Agent, used purely to
  demonstrate ExUnit setup patterns.
  """

  use Agent

  @type user :: %{id: pos_integer(), name: String.t(), email: String.t()}

  @spec start_link(keyword()) :: Agent.on_start()
  def start_link(_opts \\ []) do
    Agent.start_link(fn -> %{} end)
  end

  @spec insert(pid(), String.t(), String.t()) :: {:ok, user()}
  def insert(repo, name, email) do
    Agent.get_and_update(repo, fn state ->
      id = map_size(state) + 1
      user = %{id: id, name: name, email: email}
      {{:ok, user}, Map.put(state, id, user)}
    end)
  end

  @spec get(pid(), pos_integer()) :: {:ok, user()} | {:error, :not_found}
  def get(repo, id) do
    case Agent.get(repo, &Map.get(&1, id)) do
      nil -> {:error, :not_found}
      user -> {:ok, user}
    end
  end

  @spec all(pid()) :: [user()]
  def all(repo), do: Agent.get(repo, &Map.values/1)

  @spec count(pid()) :: non_neg_integer()
  def count(repo), do: Agent.get(repo, &map_size/1)
end
```

### Step 3: `test/user_repo_test.exs`

```elixir
defmodule UserRepoTest do
  # async: true is safe because each test gets its OWN unregistered Agent pid.
  use ExUnit.Case, async: true

  # Runs ONCE for the whole module. Put expensive, read-only setup here.
  setup_all do
    # In a real app this might be seed data you compute once.
    seed_names = ["Ada", "Alan", "Grace", "Linus", "Rob"]
    {:ok, seed_names: seed_names}
  end

  # Runs before EVERY test. Starts a fresh repo and registers cleanup.
  setup context do
    {:ok, repo} = UserRepo.start_link()

    # Cleanup runs after the test finishes, regardless of outcome.
    on_exit(fn ->
      # Agent may already be down if the test stopped it; that's fine.
      if Process.alive?(repo), do: Agent.stop(repo)
    end)

    # If a tag like `@tag seed: 3` was applied, pre-populate the repo.
    if n = context[:seed] do
      context.seed_names
      |> Enum.take(n)
      |> Enum.each(fn name ->
        UserRepo.insert(repo, name, String.downcase(name) <> "@example.com")
      end)
    end

    # Merge the repo pid into the context for tests to use.
    {:ok, repo: repo}
  end

  describe "without seeded data" do
    test "count is zero on a fresh repo", %{repo: repo} do
      assert UserRepo.count(repo) == 0
    end

    test "insert assigns sequential ids", %{repo: repo} do
      {:ok, u1} = UserRepo.insert(repo, "A", "a@x")
      {:ok, u2} = UserRepo.insert(repo, "B", "b@x")

      assert u1.id == 1
      assert u2.id == 2
    end
  end

  describe "with seeded data" do
    # Tag-driven setup: the `setup` callback reads `:seed` from context.
    @describetag :with_seed

    @tag seed: 3
    test "three users are preloaded", %{repo: repo} do
      assert UserRepo.count(repo) == 3
    end

    @tag seed: 5
    test "five users are preloaded and all have emails", %{repo: repo} do
      users = UserRepo.all(repo)
      assert length(users) == 5
      assert Enum.all?(users, &String.contains?(&1.email, "@"))
    end

    @tag seed: 1
    test "can retrieve a seeded user by id", %{repo: repo, seed_names: [first | _]} do
      assert {:ok, %{name: ^first}} = UserRepo.get(repo, 1)
    end
  end

  describe "context merging" do
    test "setup_all context is visible to tests", %{seed_names: names} do
      # setup_all's seed_names flowed all the way through.
      assert is_list(names)
      assert "Ada" in names
    end
  end
end
```

### Step 4: Run

```bash
mix test
mix test --only with_seed
mix test --trace  # see each test name and duration
```

### Why this works

`setup_all` runs once per module in its own process and merges its return
value into every test's context. `setup` runs before each test in the test's
own process, so pids it starts are isolated from other tests — `async: true`
is safe. Tag-driven branching (`if n = context[:seed]`) keeps fixtures
parameterized without a DSL; `on_exit/1` guarantees cleanup regardless of
pass/fail.

---

## Benchmark

<!-- benchmark N/A: tema conceptual sobre estructura de fixtures; la
performance relevante es "tests totales / duración del suite" y se mide
con `mix test --trace`, no con un microbenchmark. -->

---

## Trade-offs and production gotchas

**1. `setup_all` runs in its OWN process**
Any process started in `setup_all` is linked to that process, not to each
test. If the process dies between tests, everyone using it fails. Prefer
`setup` for anything that needs per-test isolation.

**2. `setup_all` and `async: true` can still coexist**
But only if what `setup_all` produces is truly immutable and read-only.
Anything writable across tests breaks determinism.

**3. Tag-driven setup can become a DSL**
It's tempting to grow `setup` into a mini-framework that branches on ten
different tags. When you notice this, extract a plain module function
(`TestFixtures.seed/2`) and call it explicitly — easier to read than
magic tags.

**4. `on_exit/1` runs in a separate process**
So it cannot affect the test's process state. Use it for external cleanup,
not to reset a `Process.put/2` value in the test process.

**5. When NOT to use `setup_all`**
If you're tempted to put a started GenServer in `setup_all`, stop. It
introduces test-order coupling. Use `setup` + `start_supervised!/1` instead.

---

## Reflection

- Your test suite has 500 tests and a shared Ecto Sandbox setup. A new hire
  proposes moving sandbox checkout into `setup_all` "for speed". What goes
  wrong, and how do you explain it with a minimal example?
- At what point does the branching inside a tag-driven `setup` become bad
  enough that you should extract it into a `TestFixtures` module? Write
  the rule you'd enforce on code review.

---

## Resources

- [`ExUnit.Callbacks`](https://hexdocs.pm/ex_unit/ExUnit.Callbacks.html)
- [`ExUnit.Case` — context, tags, `@describetag`](https://hexdocs.pm/ex_unit/ExUnit.Case.html)
- ["Testing in Elixir: Fixtures and Factories" — Dashbit blog](https://dashbit.co/blog) — the Dashbit team's ongoing series on disciplined fixtures
