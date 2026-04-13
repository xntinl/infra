# `:shutdown` — graceful termination, timeouts, and `terminate/2`

**Project**: `shutdown_timeouts_demo` — three workers with different shutdown policies (timeout, `:brutal_kill`, `:infinity`) to observe what happens when `terminate/2` takes too long.

---

## Project context

When a supervisor stops (either because it's shutting down or restarting
a `:one_for_all` group), it has to tell each child to shut down. That's
not a kill — it's a polite request, with a deadline. The `:shutdown`
option on a child spec controls that deadline.

Get it wrong and you have two failure modes: processes that never drain
their buffers because they were killed mid-flush, or shutdowns that hang
indefinitely because a slow `terminate/2` never returns. This exercise
builds all three variants and observes each failure mode.

Project structure:

```
shutdown_timeouts_demo/
├── lib/
│   ├── shutdown_timeouts_demo.ex
│   ├── shutdown_timeouts_demo/
│   │   ├── slow_worker.ex
│   │   └── supervisor.ex
├── test/
│   └── shutdown_timeouts_demo_test.exs
└── mix.exs
```

---


## Why X and not Y

- **Why not `:brutal_kill`?** Data loss on inflight work. Bounded graceful shutdown with a timeout is the ops-safe middle ground.

## Core concepts

### 1. The shutdown sequence

```
  Supervisor decides to stop child ──▶ sends EXIT with reason :shutdown
     │
     ▼
  Child (which trap_exits via GenServer) runs terminate/2
     │
     ▼  terminate/2 returns
     child exits normally ─────▶ supervisor proceeds
     │
     ▼  OR terminate/2 takes too long
     supervisor sends :kill ────▶ child is force-killed after :shutdown ms
```

### 2. Three values for `:shutdown`

```elixir
shutdown: 5_000       # default: give the child 5s, then :kill
shutdown: :brutal_kill # don't even ask, kill immediately — terminate/2 does NOT run
shutdown: :infinity    # wait forever — ONLY for supervisor children
```

`:infinity` is only appropriate for children that are themselves
supervisors (they need to propagate shutdown to their own children
before returning). Using it on a regular worker risks a hung shutdown.

### 3. `terminate/2` is best-effort, not guaranteed

`terminate/2` runs only when:

- The process exits with `:normal`, `:shutdown`, or `{:shutdown, _}`
- The process traps exits (GenServer does by default)
- The shutdown deadline hasn't elapsed

On `:brutal_kill` or VM death, `terminate/2` does NOT run. Don't put
critical cleanup there — treat it as a hint, not a contract.

### 4. Application stop follows the same rules

When your OTP app stops, `Application.stop/1` walks the supervision tree
top-down and applies each child's `:shutdown` policy. Misconfigured
`:shutdown` is why some apps hang on deploy.

---

## Design decisions

**Option A — `:brutal_kill` everywhere**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — bounded `shutdown: N_ms` with graceful `terminate/2` (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because brutal kill risks data loss on inflight work; bounded graceful shutdown caps worst-case stop time.


## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  [
    # stdlib-only by default; add `{:benchee, "~> 1.3", only: :dev}` if you benchmark
  ]
end
```


### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.


```bash
mix new shutdown_timeouts_demo
cd shutdown_timeouts_demo
```

### Step 2: `lib/shutdown_timeouts_demo/slow_worker.ex`

**Objective**: Implement `slow_worker.ex` — a worker whose crash behavior is the whole point — it exists so the supervisor strategy can be observed.


```elixir
defmodule ShutdownTimeoutsDemo.SlowWorker do
  @moduledoc """
  A GenServer with a configurable-duration terminate/2. Used to explore
  what happens when termination takes longer than :shutdown allows.
  """

  use GenServer

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    name = Keyword.fetch!(opts, :name)
    terminate_ms = Keyword.get(opts, :terminate_ms, 0)
    notify_to = Keyword.get(opts, :notify_to)
    GenServer.start_link(__MODULE__, {terminate_ms, notify_to}, name: name)
  end

  @impl true
  def init({terminate_ms, notify_to}) do
    # Trap exits so terminate/2 runs on shutdown EXIT signals.
    Process.flag(:trap_exit, true)
    {:ok, %{terminate_ms: terminate_ms, notify_to: notify_to}}
  end

  @impl true
  def terminate(reason, %{terminate_ms: ms, notify_to: notify}) do
    # Simulate flushing a buffer / closing a connection.
    if ms > 0, do: Process.sleep(ms)
    if notify, do: send(notify, {:terminated, self(), reason})
    :ok
  end
end
```

### Step 3: `lib/shutdown_timeouts_demo/supervisor.ex`

**Objective**: Encode the restart policy in `supervisor.ex` — the supervisor strategy is the lesson; the children exist to make it observable.


```elixir
defmodule ShutdownTimeoutsDemo.Supervisor do
  @moduledoc """
  Starts three workers with contrasting :shutdown policies:

    * :fast   — default 5_000 ms, terminate/2 finishes well within
    * :slow   — 100 ms shutdown but terminate/2 sleeps 500 ms → killed
    * :brutal — :brutal_kill, terminate/2 never runs
  """

  use Supervisor

  @spec start_link(keyword()) :: Supervisor.on_start()
  def start_link(opts) do
    Supervisor.start_link(__MODULE__, opts, Keyword.take(opts, [:name]))
  end

  @impl true
  def init(opts) do
    notify_to = Keyword.fetch!(opts, :notify_to)

    children = [
      Supervisor.child_spec(
        {ShutdownTimeoutsDemo.SlowWorker, [name: :fast, terminate_ms: 10, notify_to: notify_to]},
        id: :fast,
        shutdown: 5_000
      ),
      Supervisor.child_spec(
        {ShutdownTimeoutsDemo.SlowWorker, [name: :slow, terminate_ms: 500, notify_to: notify_to]},
        id: :slow,
        shutdown: 100
      ),
      Supervisor.child_spec(
        {ShutdownTimeoutsDemo.SlowWorker, [name: :brutal, terminate_ms: 500, notify_to: notify_to]},
        id: :brutal,
        shutdown: :brutal_kill
      )
    ]

    Supervisor.init(children, strategy: :one_for_one)
  end
end
```

### Step 4: `test/shutdown_timeouts_demo_test.exs`

**Objective**: Write `shutdown_timeouts_demo_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule ShutdownTimeoutsDemoTest do
  use ExUnit.Case, async: false

  test ":fast — terminate/2 finishes in time, notification received" do
    {:ok, sup} = ShutdownTimeoutsDemo.Supervisor.start_link(notify_to: self())
    fast = Process.whereis(:fast)

    Supervisor.terminate_child(sup, :fast)

    assert_receive {:terminated, ^fast, :shutdown}, 1_000
    refute Process.alive?(fast)
  end

  test ":slow — terminate/2 takes 500 ms but :shutdown is 100 ms, child is killed" do
    {:ok, sup} = ShutdownTimeoutsDemo.Supervisor.start_link(notify_to: self())
    slow = Process.whereis(:slow)

    t0 = System.monotonic_time(:millisecond)
    Supervisor.terminate_child(sup, :slow)
    t1 = System.monotonic_time(:millisecond)

    # Supervisor waits only ~100 ms (the :shutdown) then kills.
    assert t1 - t0 < 300
    refute Process.alive?(slow)

    # terminate/2 did NOT get to finish (it was sleeping 500 ms), so no
    # notification should arrive.
    refute_receive {:terminated, ^slow, _}, 150
  end

  test ":brutal — terminate/2 never runs" do
    {:ok, sup} = ShutdownTimeoutsDemo.Supervisor.start_link(notify_to: self())
    brutal = Process.whereis(:brutal)

    Supervisor.terminate_child(sup, :brutal)

    refute Process.alive?(brutal)
    # Brutal kill bypasses terminate/2 entirely.
    refute_receive {:terminated, ^brutal, _}, 150
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


## Benchmark

<!-- benchmark N/A: tema conceptual -->

## Trade-offs and production gotchas

**1. Default 5_000 ms is a generous fallback, not a target**
Your `terminate/2` should finish in a fraction of that. If you need
longer, something is wrong — probably a synchronous external call
(database, HTTP) in terminate that should have been async or pre-drained.

**2. `:infinity` is a landmine on non-supervisor workers**
It's correct for supervisor-type children (they need to propagate). On a
regular worker it means "this child can veto shutdown indefinitely". A
single stuck worker will hang your whole deploy. Reserve for supervisors.

**3. Trap exits, or `terminate/2` doesn't run at all**
`GenServer` traps exits by default, so this usually just works. But if
you're writing a raw process (no GenServer), you must
`Process.flag(:trap_exit, true)` or `terminate/2` is never called.

**4. `:brutal_kill` can leak external resources**
If your worker holds an open TCP connection or an exclusive file lock,
`:brutal_kill` may leave them in a half-cleaned state on the OS until
the VM eventually reclaims them. Use it only for workers with no
external state.

**5. When NOT to tune `:shutdown` manually**
If all your workers are cheap to kill (stateless, no external resources)
accept the 5_000 ms default. It's a safety margin, not a commitment to
wait that long. Only tune when you can measure the actual drain time.

---


## Reflection

- Tu proceso escribe a S3 en `terminate/2`. ¿Qué `shutdown` ponés y por qué? ¿Cambia si es `:brutal_kill` por default upstream?

## Executable Example

Copy the code below into a file (e.g., `solution.exs`) and run with `elixir solution.exs`:

```elixir
defmodule Main do
  defmodule ShutdownTimeoutsDemo.SlowWorker do
    @moduledoc """
    A GenServer with a configurable-duration terminate/2. Used to explore
    what happens when termination takes longer than :shutdown allows.
    """

    use GenServer

    @spec start_link(keyword()) :: GenServer.on_start()
    def start_link(opts) do
      name = Keyword.fetch!(opts, :name)
      terminate_ms = Keyword.get(opts, :terminate_ms, 0)
      notify_to = Keyword.get(opts, :notify_to)
      GenServer.start_link(__MODULE__, {terminate_ms, notify_to}, name: name)
    end

    @impl true
    def init({terminate_ms, notify_to}) do
      # Trap exits so terminate/2 runs on shutdown EXIT signals.
      Process.flag(:trap_exit, true)
      {:ok, %{terminate_ms: terminate_ms, notify_to: notify_to}}
    end

    @impl true
    def terminate(reason, %{terminate_ms: ms, notify_to: notify}) do
      # Simulate flushing a buffer / closing a connection.
      if ms > 0, do: Process.sleep(ms)
      if notify, do: send(notify, {:terminated, self(), reason})
      :ok
    end
  end

  def main do
    IO.puts("ShutdownTimeoutsDemo OK")
  end

end

Main.main()
```


## Resources

- [`Supervisor` — child specifications](https://hexdocs.pm/elixir/Supervisor.html#module-child-specification)
- [`GenServer.terminate/2`](https://hexdocs.pm/elixir/GenServer.html#c:terminate/2)
- [Erlang `supervisor` — shutdown](https://www.erlang.org/doc/man/supervisor.html#shutdown)
- ["Lies my supervisor told me" — Fred Hebert on graceful shutdown](https://ferd.ca/) (talk; search for "shutdown")


## Advanced Considerations

Supervision trees encode your application's fault tolerance strategy. The tree structure, restart policy, and shutdown semantics directly determine behavior during crashes, dependencies, and graceful shutdown.

**Supervision tree design:**
A well-designed tree mirrors data/message flow: dependencies point upward. If process A depends on process B, B should be higher in the tree (started first, shut down last). Supervisor strategies (`:one_for_one`, `:one_for_all`, `:rest_for_one`) define the scope of cascading restarts. `:one_for_one` isolates failures (each crash restarts only that child); `:one_for_all` is for tightly-coupled groups (e.g., a reader-writer pair).

**Restart strategies and intensity:**
`max_restarts: 3, max_seconds: 5` means "if 3+ restarts occur in 5 seconds, kill the supervisor." This circuit-breaker pattern prevents restart loops that consume resources. The key decision: should a crashing child take down the whole app (escalate to parent) or just itself? Transient/temporary children exit "cleanly" and don't trigger restarts — useful for request handlers.

**Error propagation and shutdown ordering:**
When a supervisor exits, it sends `:shutdown` to children in reverse start order (LIFO). Children have `shutdown: 5000` milliseconds to terminate gracefully before hard killing. Nested supervisors propagate this signal recursively. Understanding this order prevents resource leaks: a child waiting on another child's graceful shutdown will deadlock if not designed carefully.
