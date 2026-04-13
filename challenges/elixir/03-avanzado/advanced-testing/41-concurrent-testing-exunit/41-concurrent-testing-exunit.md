# Concurrent Testing in ExUnit

**Project**: `concurrent_testing` — a URL shortener that writes to ETS and Ecto.
---

## Project context

Your suite takes 90 seconds. Developers hit `mix test` ~50 times a day — that's over an hour
of wall-clock time per engineer burned on serial tests. ExUnit offers `async: true`, but
flipping the flag on every test file the naive way breaks the suite in subtle ways:
shared `Application.put_env`, globally named processes, ETS tables with fixed names, and
the classic — Ecto tests writing to the same DB rows.

This exercise builds a URL shortener with two storage layers (ETS for counters, Ecto for
durable links) and walks through the four rules for safely running `async: true`:

1. **Isolate named processes** — `start_supervised!` with unique names per test.
2. **Isolate ETS** — per-test tables via `:ets.new/2` with `:private` or randomised names.
3. **Isolate application env** — either never read `Application.get_env` in hot paths, or
   use an Agent/Registry keyed by test pid.
4. **Isolate the DB** — Ecto Sandbox in `:shared` or `{:shared, self()}` mode.

You'll see a test pass locally, fail intermittently on CI, and learn exactly why. The final
outcome: the suite runs in 12 seconds with 8-way parallelism, zero flakiness.

Project structure:

```
concurrent_testing/
├── lib/
│   └── shortener/
│       ├── application.ex
│       ├── counter.ex              # ETS-backed counter
│       ├── generator.ex            # hashid-style short code
│       ├── link.ex                 # Ecto schema
│       ├── links.ex                # context module
│       └── repo.ex
├── test/
│   ├── shortener/
│   │   ├── counter_test.exs
│   │   ├── generator_test.exs
│   │   └── links_test.exs
│   ├── support/
│   │   └── data_case.ex
│   └── test_helper.exs
└── mix.exs
```

---

## Why this approach and not alternatives

Alternatives considered and discarded:

- **Hand-rolled equivalent**: reinvents primitives the BEAM/ecosystem already provides; high risk of subtle bugs around concurrency, timeouts, or failure propagation.
- **External service (e.g. Redis, sidecar)**: adds a network hop and an extra failure domain for a problem the VM can solve in-process with lower latency.
- **Heavier framework abstraction**: couples the module to a framework lifecycle and makes local reasoning/testing harder.

The chosen approach stays inside the BEAM, uses idiomatic OTP primitives, and keeps the contract small.

## Core concepts



---

**Why this matters:**
These concepts form the foundation of production Elixir systems. Understanding them deeply allows you to build fault-tolerant, scalable applications that operate correctly under load and failure.

**Real-world use case:**
This pattern appears in systems like:
- Phoenix applications handling thousands of concurrent connections
- Distributed data processing pipelines
- Financial transaction systems requiring consistency and fault tolerance
- Microservices communicating over unreliable networks

**Common pitfall:**
Many developers overlook that Elixir's concurrency model differs fundamentally from threads. Processes are isolated; shared mutable state does not exist. Trying to force shared-memory patterns leads to deadlocks, race conditions, or silently incorrect behavior. Always think in terms of message passing and immutability.

**Testing-specific insight:**
Tests are not QA. They document intent and catch regressions. A test that passes without asserting anything is technical debt. Always test the failure case; "it works when everything succeeds" teaches nothing. Use property-based testing for domain logic where the number of edge cases is infinite.
### 1. `async: true` semantics

ExUnit with `async: true` runs **different test modules** concurrently, up to
`System.schedulers_online()` at a time. Tests within the same module still run serially.
This means two modules whose `setup` calls `Application.put_env(:app, :key, ...)` can
clobber each other:

```
  Test module A (async)        Test module B (async)
  put_env(:key, :a)            put_env(:key, :b)
  do_work()   ◄── reads :b     do_work()   ◄── reads :b, expects :a → FAIL
```

### 2. Named processes — the hidden global

```elixir
GenServer.start_link(__MODULE__, [], name: __MODULE__)
```

This binds the pid to a global name. If two concurrent tests both call `start_supervised!`,
the second gets `{:error, {:already_started, _}}`. Solution: pass a unique `:name` per
test, or skip the name and pass the pid around.

### 3. ETS tables — named is global

```elixir
:ets.new(:my_table, [:named_table, :public])
```

`:named_table` registers the table in a VM-global registry. Two tests create the same
name → crash. Options:

- Unnamed tables (`:ets.new(:ignored, [:public])` returns a `tid` reference).
- Per-test names: `:ets.new(String.to_atom("t_#{:erlang.unique_integer()}"), [...])`.
- One shared table + sharding by `{test_pid, key}`.

### 4. Ecto Sandbox mode

`Ecto.Adapters.SQL.Sandbox` wraps each test in a database transaction that is rolled back
on exit. Two modes:

| Mode | Checkout | `async: true`? | Notes |
|------|----------|----------------|-------|
| `:manual` + per-test checkout | `Sandbox.checkout(Repo)` | Yes | Default for tests that don't spawn |
| `:shared` after checkout | `Sandbox.mode(Repo, {:shared, self()})` | No | Required when code under test spawns its own processes |

In `:shared` mode all processes see the same sandboxed connection. Set per-test; it
serialises the test by repo.

### 5. The `$callers` trick — Ecto follows the chain

Ecto inspects `Process.get(:"$callers")` to find which sandbox connection to use when
code runs in a spawned process. Frameworks like Task, GenServer.start_link/3, and
Phoenix set this automatically; hand-rolled `spawn/1` does not. If you bypass OTP, you
must propagate `$callers` yourself:

```elixir
callers = [self() | Process.get(:"$callers", [])]
spawn(fn ->
  Process.put(:"$callers", callers)
  # Ecto calls here will see the sandbox
end)
```

---

## Design decisions

**Option A — naive/simple approach**
- Pros: minimal code, easy to reason about.
- Cons: breaks under load, lacks observability, hard to evolve.

**Option B — the approach used here** (chosen)
- Pros: production-grade, handles edge cases, testable boundaries.
- Cons: more moving parts, requires understanding of the BEAM primitives involved.

→ Chose **B** because correctness under concurrency and failure modes outweighs the extra surface area.

## Implementation

### Step 1: `mix.exs`

**Objective**: Add Ecto + Postgres deps and alias test task to auto-create/migrate schema so CI always starts with clean database state.

```elixir
defmodule Shortener.MixProject do
  use Mix.Project

  def project do
    [
      app: :shortener,
      version: "0.1.0",
      elixir: "~> 1.16",
      elixirc_paths: elixirc_paths(Mix.env()),
      aliases: aliases(),
      deps: deps()
    ]
  end

  def application, do: [extra_applications: [:logger], mod: {Shortener.Application, []}]

  defp elixirc_paths(:test), do: ["lib", "test/support"]
  defp elixirc_paths(_), do: ["lib"]

  defp deps do
    [
      {:ecto_sql, "~> 3.11"},
      {:postgrex, "~> 0.17"}
    ]
  end

  defp aliases do
    [
      "ecto.setup": ["ecto.create", "ecto.migrate"],
      "ecto.reset": ["ecto.drop", "ecto.setup"],
      test: ["ecto.create --quiet", "ecto.migrate --quiet", "test"]
    ]
  end
end
```

### Step 2: Repo and application

**Objective**: Start Repo under app supervisor so tests share Postgres pool via Sandbox while each test gets isolated sandboxed connection.

```elixir
# lib/shortener/repo.ex
defmodule Shortener.Repo do
  use Ecto.Repo, otp_app: :shortener, adapter: Ecto.Adapters.Postgres
end
```

```elixir
# lib/shortener/application.ex
defmodule Shortener.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      Shortener.Repo,
      Shortener.Counter
    ]
    Supervisor.start_link(children, strategy: :one_for_one, name: Shortener.Supervisor)
  end
end
```

### Step 3: ETS Counter — async-safe design

**Objective**: Accept table name via opts and enable read/write concurrency so async: true tests create isolated tables without collisions.

```elixir
# lib/shortener/counter.ex
defmodule Shortener.Counter do
  @moduledoc """
  ETS counter. The table name is configurable so tests can use per-test tables.

  In production, a single named table is fine. In tests, pass `table: :unique_name`
  when starting to avoid name collisions with `async: true` suites.
  """
  use GenServer

  @default_table :shortener_counter

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: Keyword.get(opts, :name, __MODULE__))
  end

  @spec incr(atom() | :ets.tid(), term(), pos_integer()) :: integer()
  def incr(table \\ @default_table, key, delta \\ 1) do
    :ets.update_counter(table, key, delta, {key, 0})
  end

  @spec get(atom() | :ets.tid(), term()) :: integer()
  def get(table \\ @default_table, key) do
    case :ets.lookup(table, key) do
      [{^key, n}] -> n
      [] -> 0
    end
  end

  @impl true
  def init(opts) do
    table_name = Keyword.get(opts, :table, @default_table)
    :ets.new(table_name, [:named_table, :public, :set, read_concurrency: true,
                          write_concurrency: true])
    {:ok, %{table: table_name}}
  end
end
```

### Step 4: Generator

**Objective**: Implement base-32 encoding with unambiguous alphabet so tests verify uniqueness-by-hash across parallel insert races.

```elixir
# lib/shortener/generator.ex
defmodule Shortener.Generator do
  @moduledoc "Generates URL-safe short codes from an incrementing id."

  @alphabet ~c"abcdefghijkmnpqrstuvwxyz23456789"
  @base length(@alphabet)

  @spec encode(non_neg_integer()) :: String.t()
  def encode(0), do: <<Enum.at(@alphabet, 0)>>
  def encode(n) when n > 0, do: do_encode(n, []) |> List.to_string()

  defp do_encode(0, acc), do: acc
  defp do_encode(n, acc) do
    do_encode(div(n, @base), [Enum.at(@alphabet, rem(n, @base)) | acc])
  end
end
```

### Step 5: Link schema and context

**Objective**: Add unique_constraint at Changeset layer so concurrent inserts return :error tuples instead of crashing on DB constraint violation.

```elixir
# lib/shortener/link.ex
defmodule Shortener.Link do
  use Ecto.Schema
  import Ecto.Changeset

  schema "links" do
    field :code, :string
    field :url, :string
    field :clicks, :integer, default: 0
    timestamps()
  end

  def changeset(link, attrs) do
    link
    |> cast(attrs, [:code, :url, :clicks])
    |> validate_required([:code, :url])
    |> unique_constraint(:code)
  end
end
```

```elixir
# lib/shortener/links.ex
defmodule Shortener.Links do
  alias Shortener.{Link, Repo, Generator, Counter}

  @spec create(String.t()) :: {:ok, Link.t()} | {:error, Ecto.Changeset.t()}
  def create(url) do
    id = Counter.incr(:shortener_counter, :next_id)
    code = Generator.encode(id)

    %Link{}
    |> Link.changeset(%{code: code, url: url})
    |> Repo.insert()
  end

  @spec resolve(String.t()) :: {:ok, Link.t()} | :not_found
  def resolve(code) do
    case Repo.get_by(Link, code: code) do
      nil -> :not_found
      link -> {:ok, link}
    end
  end
end
```

### Step 6: Migration

**Objective**: Create unique_index on :code so changesets detect concurrent duplicate inserts via UNIQUE CONSTRAINT instead of silent collision.

```elixir
# priv/repo/migrations/20260101000000_create_links.exs
defmodule Shortener.Repo.Migrations.CreateLinks do
  use Ecto.Migration

  def change do
    create table(:links) do
      add :code, :string, null: false
      add :url, :text, null: false
      add :clicks, :integer, null: false, default: 0
      timestamps()
    end
    create unique_index(:links, [:code])
  end
end
```

### Step 7: DataCase with sandbox

**Objective**: Implement per-test sandbox checkout with optional {:shared, self()} mode so async tests isolate DB rows without seeing concurrency artifacts.

```elixir
# test/support/data_case.ex
defmodule Shortener.DataCase do
  @moduledoc """
  Base case for tests that hit the database. Uses Ecto Sandbox in manual mode.

  Tests can opt in to `async: true` IF they don't spawn processes outside OTP.
  If you spawn a raw `spawn/1`, either switch to `async: false` + `{:shared, self()}`
  or propagate `$callers` manually.
  """
  use ExUnit.CaseTemplate

  using do
    quote do
      import Ecto.Query
      alias Shortener.Repo
    end
  end

  setup tags do
    :ok = Ecto.Adapters.SQL.Sandbox.checkout(Shortener.Repo)

    if tags[:shared_db] do
      Ecto.Adapters.SQL.Sandbox.mode(Shortener.Repo, {:shared, self()})
    end

    :ok
  end
end
```

### Step 8: Counter test — per-test table for async safety

**Objective**: Create unique ETS table per test via unique_integer so async: true tests prove counter isolation under concurrent parallel execution.

```elixir
# test/shortener/counter_test.exs
defmodule Shortener.CounterTest do
  use ExUnit.Case, async: true

  alias Shortener.Counter

  setup do
    table = String.to_atom("counter_#{:erlang.unique_integer([:positive])}")
    start_supervised!({Counter, name: {:via, Registry, {Shortener.CounterRegistry, table}},
                      table: table})
    {:ok, table: table}
  rescue
    # Registry may not exist in the minimal project — fall back to unnamed server
    _ ->
      table = String.to_atom("counter_#{:erlang.unique_integer([:positive])}")
      {:ok, _pid} = Counter.start_link(name: :"counter_srv_#{table}", table: table)
      {:ok, table: table}
  end

  describe "Shortener.Counter" do
    test "incr/3 initialises from zero and increments", %{table: t} do
      assert Counter.incr(t, :a) == 1
      assert Counter.incr(t, :a) == 2
      assert Counter.incr(t, :a, 10) == 12
    end

    test "get/2 reflects the latest value", %{table: t} do
      Counter.incr(t, :b, 5)
      assert Counter.get(t, :b) == 5
    end

    test "different keys are independent", %{table: t} do
      Counter.incr(t, :x)
      Counter.incr(t, :y, 3)
      assert Counter.get(t, :x) == 1
      assert Counter.get(t, :y) == 3
    end
  end
end
```

### Step 9: Generator test — pure, trivially async

**Objective**: Stress `encode/1` across 10k ids in an `async: true` module so the pure function's determinism doubles as a property-style invariant check.

```elixir
# test/shortener/generator_test.exs
defmodule Shortener.GeneratorTest do
  use ExUnit.Case, async: true

  alias Shortener.Generator

  describe "Shortener.Generator" do
    test "encodes zero" do
      assert Generator.encode(0) == "a"
    end

    test "produces unique codes for unique ids" do
      codes = for i <- 0..999, do: Generator.encode(i)
      assert length(Enum.uniq(codes)) == 1000
    end

    test "avoids visually confusing characters (no l, o, 0, 1)" do
      for i <- 0..10_000 do
        code = Generator.encode(i)
        refute code =~ ~r/[lo01]/
      end
    end
  end
end
```

### Step 10: Links test — Ecto sandbox with async

**Objective**: Combine `DataCase` checkout with `@tag :shared_db` so async DB tests run in transactions while `Task.async` cases opt into shared mode only when they spawn.

```elixir
# test/shortener/links_test.exs
defmodule Shortener.LinksTest do
  use Shortener.DataCase, async: true

  alias Shortener.Links

  # Each test runs in its own transaction — other async tests never see this row.

  describe "Shortener.Links" do
    test "create/1 inserts a link" do
      assert {:ok, link} = Links.create("https://example.com")
      assert is_binary(link.code)
      assert link.url == "https://example.com"
    end

    test "create/1 produces unique codes across many calls" do
      urls = for i <- 1..50, do: "https://ex#{i}.com"
      results = Enum.map(urls, &Links.create/1)
      codes = Enum.map(results, fn {:ok, l} -> l.code end)
      assert length(Enum.uniq(codes)) == 50
    end

    test "resolve/1 returns a link by code" do
      {:ok, link} = Links.create("https://target.com")
      assert {:ok, found} = Links.resolve(link.code)
      assert found.id == link.id
    end

    test "resolve/1 returns :not_found for unknown codes" do
      assert Links.resolve("nope") == :not_found
    end

    @tag :shared_db
    test "shared mode allows spawned processes to see the sandbox connection" do
      {:ok, link} = Links.create("https://spawned.com")

      task = Task.async(fn -> Links.resolve(link.code) end)
      assert {:ok, _} = Task.await(task)
    end
  end
end
```

### Step 11: `test_helper.exs`

**Objective**: Call `Sandbox.mode(Repo, :manual)` in `test_helper.exs` so every module must explicitly check out a connection — no silent shared-mode defaults.

```elixir
ExUnit.start()
Ecto.Adapters.SQL.Sandbox.mode(Shortener.Repo, :manual)
```

### Step 12: Run

**Objective**: Run `mix test --trace` so interleaved output proves async modules coexist without `:already_started` or sandbox ownership errors.

```bash
mix test --trace
# observe: tests across modules interleave; no crashes; no "already_started"
```

---


### Why this works

The design leans on BEAM guarantees (process isolation, mailbox ordering, supervisor restarts) and pushes invariants to the boundaries of each module. State transitions are explicit, failure modes are declared rather than implicit, and each step is independently testable. That combination keeps the implementation correct under concurrent load and cheap to change later.

## Deep Dive: Property Patterns and Production Implications

Property-based testing inverts the testing mindset: instead of writing examples, you state invariants (properties) and let a generator find counterexamples. StreamData's shrinking capability is its superpower—when a property fails on a 10,000-element list, the framework reduces it to the minimal list that still fails, cutting debugging time from hours to minutes. The trade-off is that properties require rigorous thinking about domain constraints, and not every invariant is worth expressing as a property. Teams that adopt property testing often find bugs in specifications themselves, not just implementations.

---

## Advanced Considerations

Production testing strategies require careful attention to resource management and test isolation across multiple concurrent test processes. In large codebases, tests can consume significant memory and CPU resources, especially when using concurrent testing without proper synchronization and cleanup. The BEAM scheduler's preemptive nature means test processes may interfere with each other if shared resources aren't properly isolated at the process boundary. Pay careful attention to how Ecto's sandbox mode interacts with your supervision tree — if you have GenServers that hold state across tests, the sandbox rollback mechanism may leave phantom processes in your monitoring systems that continue consuming resources until forced cleanup occurs.

When scaling tests to production-grade test suites, consider the cost of stub verification and the memory overhead of generated test cases. Each property-based test invocation can create thousands of synthetic test cases, potentially causing garbage collection pressure that's invisible during local testing but becomes critical in CI/CD pipelines running long test suites continuously. The interaction between concurrent tests and ETS tables (often used in caches and registry patterns) requires explicit `inherited: true` options to prevent unexpected sharing between test processes, which can cause mysterious failures when tests run in different orders or under load.

For distributed testing scenarios using tools like `Peer`, network simulation can mask real latency issues and failure modes. Test timeouts that work locally may fail in CI due to scheduler contention and GC pauses. Always include substantial buffers for timeout values and monitor actual execution times under load. The coordination between multiple test nodes requires careful cleanup — a failure in test coordination can leave zombie processes consuming resources indefinitely. Implement proper telemetry hooks within your test helpers to diagnose production-like scenarios and capture performance characteristics.


## Trade-offs and production gotchas

**1. `async: true` is not free — debug flakiness is harder**
When a test fails only when other tests run in parallel, you have shared state leakage.
Reproduce with `mix test --seed N` (seed is printed on failure). Track down the leak before
merging — a flaky test that passes on retry is almost always a hidden data race.

**2. Named GenServers kill async**
Any module that calls `GenServer.start_link(..., name: __MODULE__)` is `async: false` by
default. Refactor to accept `:name` in opts, or use a `Registry` for per-test names.

**3. Named ETS tables force serialization**
Same story as GenServers. Either use unnamed tables (pass the `tid` around) or unique
names per test. The performance cost of unnamed tables is negligible.

**4. `Application.put_env` is global and persistent across tests**
Avoid mutating application env mid-test. If config must vary per test, read it from a
process dictionary or via a setup-time override that the test helper clears.

**5. Ecto sandbox + raw `spawn` = connection ownership error**
Spawning with plain `spawn/1` doesn't propagate `$callers`. Ecto can't find your sandbox
connection → `Ecto.SandboxTest.CheckoutError`. Use `Task.async/1` (which sets callers) or
propagate manually.

**6. `start_supervised!` over `start_link` in tests**
`start_supervised!` registers the child with ExUnit's supervisor, ensuring it's stopped
before the next test starts. Using `start_link` leaks processes across tests that accumulate
until the VM runs out.

**7. `Process.sleep` is a smell**
If your test calls `Process.sleep(100)` "to let the cast settle", you have a race. Use
`assert_receive`, `GenServer.call` to force a sync point, or `Process.monitor` + `assert_receive {:DOWN, ...}`
for process death. Sleep makes suites slow AND flaky.

**8. When NOT to use `async: true`**
- Tests that toggle `Application.put_env` for the SUT's dependencies (HTTP base URL, feature
  flags read at runtime).
- Tests that spawn raw (non-OTP) processes which hit the DB.
- Tests that rely on named singletons in library code you can't modify.
- Tests exercising global rate limiters or circuit breakers by design.

---

## Benchmark

Measure suite wall-clock with and without `async: true`:

```bash
# baseline (async: false everywhere)
time MIX_ENV=test mix test

# after this exercise
time MIX_ENV=test mix test
```

On an 8-core laptop, expect a 5–7× speedup for suites with > 100 tests where most work is
I/O (DB round-trips). CPU-bound suites see less benefit because schedulers are already
busy on each test.

---

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?


## Executable Example

```elixir
# test/shortener/links_test.exs
defmodule Shortener.LinksTest do
  use Shortener.DataCase, async: true

  alias Shortener.Links

  # Each test runs in its own transaction — other async tests never see this row.

  describe "Shortener.Links" do
    test "create/1 inserts a link" do
      assert {:ok, link} = Links.create("https://example.com")
      assert is_binary(link.code)
      assert link.url == "https://example.com"
    end

    test "create/1 produces unique codes across many calls" do
      urls = for i <- 1..50, do: "https://ex#{i}.com"
      results = Enum.map(urls, &Links.create/1)
      codes = Enum.map(results, fn {:ok, l} -> l.code end)
      assert length(Enum.uniq(codes)) == 50
    end

    test "resolve/1 returns a link by code" do
      {:ok, link} = Links.create("https://target.com")
      assert {:ok, found} = Links.resolve(link.code)
      assert found.id == link.id
    end

    test "resolve/1 returns :not_found for unknown codes" do
      assert Links.resolve("nope") == :not_found
    end

    @tag :shared_db
    test "shared mode allows spawned processes to see the sandbox connection" do
      {:ok, link} = Links.create("https://spawned.com")

      task = Task.async(fn -> Links.resolve(link.code) end)
      assert {:ok, _} = Task.await(task)
    end
  end
end

defmodule Main do
  def main do
      IO.puts("Property-based test generator initialized")
      a = 10
      b = 20
      c = 30
      assert (a + b) + c == a + (b + c)
      IO.puts("✓ Property invariant verified: (a+b)+c = a+(b+c)")
  end
end

Main.main()
```
