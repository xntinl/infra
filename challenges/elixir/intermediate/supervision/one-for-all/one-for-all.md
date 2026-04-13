# `:one_for_all` — when siblings must fail together

**Project**: `one_for_all_demo` — three workers that form a coherent group; any crash resets them all together.

---

## Why one for all matters

Sometimes the correct response to a child crashing is "restart everybody".
That sounds drastic but is exactly right when children share in-memory
state or hold references to each other's pids — if one dies, the others
now hold stale data and the whole group is in an inconsistent state.

`:one_for_all` says: **when any child crashes, terminate all remaining
children and restart them all together**. It's the "nuke the group and
start over" strategy, and it's the right call more often than you'd
expect for tightly coupled components.

---

## Project structure

```
one_for_all_demo/
├── lib/
│   └── one_for_all_demo.ex
├── script/
│   └── main.exs
├── test/
│   └── one_for_all_demo_test.exs
└── mix.exs
```

---

## Why X and not Y

- **Why not the other strategies?** Each encodes a different coupling assumption; picking the wrong one either over-restarts or under-restarts.

## Core concepts

### 1. Crash in ANY child = restart ALL children

```
        Supervisor(:one_for_all)
         │
         ├── Writer (state=X) ─ crash
         ├── Reader₁           ─ terminated by supervisor, restarted
         └── Reader₂           ─ terminated by supervisor, restarted

        After restart: all three run fresh init/1.
```

Contrast with `:one_for_one` where only the Writer would restart and the
Readers would continue holding possibly-stale references.

### 2. Termination order follows child-spec order in reverse

On a group restart, the supervisor terminates children in **reverse**
order of the `children` list, then starts them again in forward order.
That's how you encode "start A before B; shut B before A" dependencies.

### 3. The `max_restarts` budget counts group restarts as one event

When the whole group restarts, that's one tick of the restart intensity
counter — not three (even though three children restarted). The budget
is about supervisor-level restart *events*, not child-level ones.

### 4. Transient children still honor normal exits

Even under `:one_for_all`, a child with `restart: :transient` that exits
with `:normal` does NOT trigger a group restart. Only abnormal exits do.

---

## Design decisions

**Option A — `:one_for_one`**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — `:one_for_all` (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because children share in-memory state (e.g. ETS owner) that must be rebuilt atomically.

## Implementation

### `mix.exs`

```elixir
defmodule OneForAllDemo.MixProject do
  use Mix.Project

  def project do
    [
      app: :one_for_all_demo,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    []
  end
end
```

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.

```bash
mix new one_for_all_demo
cd one_for_all_demo
```

### `lib/one_for_all_demo.ex`

```elixir
defmodule OneForAllDemo do
  @moduledoc """
  `:one_for_all` — when siblings must fail together.

  Sometimes the correct response to a child crashing is "restart everybody".
  """
end
```

### `lib/one_for_all_demo/writer.ex`

**Objective**: Provide `lib/one_for_all_demo/writer.ex` and `reader.ex` — these are the supporting fixtures the main module depends on to make its concept demonstrable.

```elixir
defmodule OneForAllDemo.Writer do
  @moduledoc """
  Holds an opaque "session token" that Readers expect to be valid. If the
  Writer dies, every Reader is holding an expired token — so the whole
  group must reset together.
  """

  use GenServer

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(_opts), do: GenServer.start_link(__MODULE__, :ok, name: __MODULE__)

  @spec token() :: reference()
  def token, do: GenServer.call(__MODULE__, :token)

  @spec crash() :: :ok
  def crash, do: GenServer.cast(__MODULE__, :crash)

  @impl true
  def init(:ok), do: {:ok, make_ref()}

  @impl true
  def handle_call(:token, _from, ref), do: {:reply, ref, ref}

  @impl true
  def handle_cast(:crash, _ref), do: raise("writer blew up")
end

defmodule OneForAllDemo.Reader do
  @moduledoc """
  Caches the Writer's token at boot. If the Writer restarts without us,
  the cached token is stale — which is exactly why this group uses
  :one_for_all.
  """

  use GenServer

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    name = Keyword.fetch!(opts, :name)
    GenServer.start_link(__MODULE__, :ok, name: name)
  end

  @spec cached_token(atom()) :: reference()
  def cached_token(name), do: GenServer.call(name, :cached_token)

  @impl true
  def init(:ok) do
    # Fetched ONCE at boot; treated as immutable for the process's lifetime.
    {:ok, OneForAllDemo.Writer.token()}
  end

  @impl true
  def handle_call(:cached_token, _from, ref), do: {:reply, ref, ref}
end
```

### `lib/one_for_all_demo/supervisor.ex`

**Objective**: Encode the restart policy in `supervisor.ex` — the supervisor strategy is the lesson; the children exist to make it observable.

```elixir
defmodule OneForAllDemo.Supervisor do
  @moduledoc """
  :one_for_all supervisor. Any crash in Writer or Reader restarts the
  whole group so no Reader holds a stale Writer token.
  """

  use Supervisor

  @spec start_link(keyword()) :: Supervisor.on_start()
  def start_link(opts \\ []), do: Supervisor.start_link(__MODULE__, :ok, opts)

  @impl true
  def init(:ok) do
    children = [
      # Writer MUST start before Readers (Readers read the token in init/1).
      OneForAllDemo.Writer,
      Supervisor.child_spec({OneForAllDemo.Reader, [name: :r1]}, id: :r1),
      Supervisor.child_spec({OneForAllDemo.Reader, [name: :r2]}, id: :r2)
    ]

    Supervisor.init(children, strategy: :one_for_all)
  end
end
```

### Step 4: `test/one_for_all_demo_test.exs`

**Objective**: Write `one_for_all_demo_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.

```elixir
defmodule OneForAllDemoTest do
  use ExUnit.Case, async: false

  doctest OneForAllDemo

  alias OneForAllDemo.{Writer, Reader}

  setup do
    start_supervised!(OneForAllDemo.Supervisor)
    :ok
  end

  describe "core functionality" do
    test "all readers cache the same token at boot" do
      token = Writer.token()
      assert Reader.cached_token(:r1) == token
      assert Reader.cached_token(:r2) == token
    end

    test "crashing the writer restarts the WHOLE group" do
      old_writer = Process.whereis(Writer)
      old_r1 = Process.whereis(:r1)
      old_r2 = Process.whereis(:r2)
      old_token = Writer.token()

      ref_w = Process.monitor(old_writer)
      ref_r1 = Process.monitor(old_r1)
      ref_r2 = Process.monitor(old_r2)

      Writer.crash()

      # All three processes went down.
      assert_receive {:DOWN, ^ref_w, :process, ^old_writer, _}, 500
      assert_receive {:DOWN, ^ref_r1, :process, ^old_r1, _}, 500
      assert_receive {:DOWN, ^ref_r2, :process, ^old_r2, _}, 500

      # After restart the whole group is fresh and consistent.
      wait_until_alive([Writer, :r1, :r2])

      new_token = Writer.token()
      assert new_token != old_token
      assert Reader.cached_token(:r1) == new_token
      assert Reader.cached_token(:r2) == new_token
    end

    test "crashing a reader also restarts the writer and the other reader" do
      old_writer = Process.whereis(Writer)
      ref_w = Process.monitor(old_writer)

      # Kill r1 brutally — any abnormal exit triggers group restart.
      Process.exit(Process.whereis(:r1), :crash)

      assert_receive {:DOWN, ^ref_w, :process, ^old_writer, _}, 500

      wait_until_alive([Writer, :r1, :r2])
      assert Process.whereis(Writer) != old_writer
    end

    defp wait_until_alive(names, timeout \\ 500) do
      deadline = System.monotonic_time(:millisecond) + timeout
      do_wait(names, deadline)
    end

    defp do_wait(names, deadline) do
      if Enum.all?(names, &(Process.whereis(&1) |> is_pid())) do
        :ok
      else
        if System.monotonic_time(:millisecond) > deadline do
          flunk("processes did not come back: #{inspect(names)}")
        else
          Process.sleep(10)
          do_wait(names, deadline)
        end
      end
    end
  end
end
```

### Step 5: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.

```bash
mix test
```

---

### Why this works

The design leans on OTP primitives that already encode the invariants we care about (supervision, back-pressure, explicit message semantics), so failure modes are visible at the right layer instead of being reinvented ad-hoc. Tests exercise the edges (timeouts, crashes, boundary states), which is where hand-rolled alternatives silently drift over time.

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Runnable demo of `OneForAllDemo`.

  Exercises the public API end-to-end so behaviour is observable
  in addition to documented.
  """

  def main do
    IO.puts("=== OneForAllDemo demo ===")
    run()
    IO.puts("\nDone — demo finished without errors.")
  end

  defp run do
    # No public functions detected; replace with calls into the module.
    :ok
  end
end

Main.main()
```

## Key Concepts: Restart Strategy — One-for-All

With `:one_for_all`, when **any** child crashes, all children are restarted. This is stricter but ensures consistency: either all are alive or all are restarting. Use it when multiple children form a tightly coupled service (e.g., a cache + cache invalidation process).

If children have heavy initialization costs, `:one_for_all` can cause thrashing (restart loop). Use `:rest_for_one` (restart only the crashed child and descendants) for more granular control.

## Benchmark

<!-- benchmark N/A: tema conceptual -->

## Trade-offs and production gotchas

**1. Group restarts are expensive**
Restarting 10 children for one crash means 10 × `init/1`, 10 × connection
re-establishment, 10 × cache warmup. Don't use `:one_for_all` as "just in
case" — only when the coupling actually requires it.

**2. Startup order matters — shutdown order is reversed**
Children are started in the order listed and terminated in reverse. If
`Reader` depends on `Writer`, list `Writer` first. Getting this wrong
produces flaky tests (`Reader.init/1` crashes because `Writer` isn't up
yet) that look random.

**3. `max_restarts` triggers faster with big groups**
A `:one_for_all` group that flaps three times burns the default
`max_restarts: 3` budget and takes down the supervisor. Bump it (exercise
59) if the group is expected to occasionally reset under normal load.

**4. Shared in-memory caches still die with the group — consider ETS**
State held inside the supervised children is lost on group restart. If
you want the group to restart but the cache to survive, put the cache in
an ETS table owned by a longer-lived parent (or a separate supervised
ETS holder) and have the group read/write through it.

**5. When NOT to use `:one_for_all`**
When children are independent, use `:one_for_one` — there's no upside to
punishing healthy siblings for one child's crash. When the dependency is
*directional* (B depends on A but not vice versa), use `:rest_for_one`
to avoid restarting A when only B crashes.

---

## Reflection

- Con 20 children, ¿`:one_for_all` sigue siendo apropiado o es momento de romper en subtrees? Justificá con una métrica concreta.

## Resources

- [`Supervisor` strategies — `:one_for_all`](https://hexdocs.pm/elixir/Supervisor.html#module-strategies)
- [Erlang `supervisor` — restart strategies](https://www.erlang.org/doc/man/supervisor.html#restart-strategies)
- ["Designing for Scalability with Erlang/OTP" — Ch. 8](https://www.oreilly.com/library/view/designing-for-scalability/9781449361556/)

## Advanced Considerations

Supervision trees encode your application's fault tolerance strategy. The tree structure, restart policy, and shutdown semantics directly determine behavior during crashes, dependencies, and graceful shutdown.

**Supervision tree design:**
A well-designed tree mirrors data/message flow: dependencies point upward. If process A depends on process B, B should be higher in the tree (started first, shut down last). Supervisor strategies (`:one_for_one`, `:one_for_all`, `:rest_for_one`) define the scope of cascading restarts. `:one_for_one` isolates failures (each crash restarts only that child); `:one_for_all` is for tightly-coupled groups (e.g., a reader-writer pair).

**Restart strategies and intensity:**
`max_restarts: 3, max_seconds: 5` means "if 3+ restarts occur in 5 seconds, kill the supervisor." This circuit-breaker pattern prevents restart loops that consume resources. The key decision: should a crashing child take down the whole app (escalate to parent) or just itself? Transient/temporary children exit "cleanly" and don't trigger restarts — useful for request handlers.

**Error propagation and shutdown ordering:**
When a supervisor exits, it sends `:shutdown` to children in reverse start order (LIFO). Children have `shutdown: 5000` milliseconds to terminate gracefully before hard killing. Nested supervisors propagate this signal recursively. Understanding this order prevents resource leaks: a child waiting on another child's graceful shutdown will deadlock if not designed carefully.

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

### `test/one_for_all_demo_test.exs`

```elixir
defmodule OneForAllDemoTest do
  use ExUnit.Case, async: true

  doctest OneForAllDemo

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert OneForAllDemo.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts

### 1. Model the problem with the right primitive

Choose the OTP primitive that matches the failure semantics of the problem: `GenServer` for stateful serialization, `Task` for fire-and-forget async, `Agent` for simple shared state, `Supervisor` for lifecycle management. Reaching for the wrong primitive is the most common source of accidental complexity in Elixir systems.

### 2. Make invariants explicit in code

Guards, pattern matching, and `@spec` annotations turn invariants into enforceable contracts. If a value *must* be a positive integer, write a guard — do not write a comment. The compiler and Dialyzer will catch what documentation cannot.

### 3. Let it crash, but bound the blast radius

"Let it crash" is not permission to ignore failures — it is a directive to design supervision trees that contain them. Every process should be supervised, and every supervisor should have a restart strategy that matches the failure mode it is recovering from.
