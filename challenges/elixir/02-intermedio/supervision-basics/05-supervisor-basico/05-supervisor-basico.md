# Basic Supervisor with static children

**Project**: `basic_supervisor` — a supervisor that starts two GenServer workers at boot and restarts any one that crashes.

---

## Project context

You've written GenServers, you know how to link and trap exits, and now you
need the piece that turns those primitives into a production-grade tree: the
`Supervisor`. A supervisor's job is simple but load-bearing — start a fixed
set of child processes at boot, monitor them via links, and restart any child
that crashes according to a strategy.

This exercise builds a supervisor with **two static workers** declared at
startup and the default `:one_for_one` strategy. It is the skeleton you'll
see in every real OTP application's `lib/my_app/application.ex`.

Project structure:

```
basic_supervisor/
├── lib/
│   ├── basic_supervisor.ex
│   ├── basic_supervisor/
│   │   ├── worker.ex
│   │   └── supervisor.ex
├── test/
│   └── basic_supervisor_test.exs
└── mix.exs
```

---


## Why X and not Y

- **Why not a lower-level alternative?** For supervisor basico, OTP's pattern is what reviewers will expect and what observability tools support out of the box.

## Core concepts

### 1. A supervisor is a process that traps exits

Under the hood, `Supervisor` is a specialized process that calls
`Process.flag(:trap_exit, true)`, links each child via `start_link`, and
turns every `{:EXIT, child, reason}` message into a restart decision.
Nothing magical — just low-level link/trap_exit primitives wrapped in a
reusable behavior.

### 2. Child specs describe HOW to start and restart a child

A child spec is a map (or a tuple) that tells the supervisor:

- `:id` — how to identify this child within the tree
- `:start` — the MFA the supervisor calls to start it
- `:restart` — `:permanent` (default), `:transient`, or `:temporary`
- `:shutdown` — how long to wait on shutdown before killing
- `:type` — `:worker` or `:supervisor`

Most modules expose `child_spec/1` automatically when they `use GenServer`,
so you can just write `{MyWorker, arg}` in the children list.

### 3. `:one_for_one` — the default strategy

```
  Supervisor
   ├── Worker A     crash       ──▶    Worker A restarted
   └── Worker B  (unaffected)   ──▶    Worker B keeps running
```

Only the crashed child is restarted. Siblings are untouched. This is the
right default when children are independent — the vast majority of cases.
See exercises 57 and 58 for when siblings depend on each other.

### 4. `start_link` vs `start`

Always use `Supervisor.start_link/2` so the supervisor is linked to its
parent. If the parent dies, the whole subtree is taken down deterministically
instead of leaking orphan processes.

---

## Design decisions

**Option A — manual `spawn_link` + restart loop**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — a Supervisor (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because supervisors encode restart intensity, strategy, and shutdown timeouts that hand-rolled loops always get wrong.


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
mix new basic_supervisor
cd basic_supervisor
```

### Step 2: `lib/basic_supervisor/worker.ex`

**Objective**: Encode the restart policy in `worker.ex` — the supervisor strategy is the lesson; the children exist to make it observable.


```elixir
defmodule BasicSupervisor.Worker do
  @moduledoc """
  A trivial GenServer used to demonstrate supervision. Holds a counter and
  exposes `bump/1`, `value/1`, and `crash/1` so tests can observe the
  effect of a restart (state is lost — counters go back to zero).
  """

  use GenServer

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    name = Keyword.fetch!(opts, :name)
    GenServer.start_link(__MODULE__, 0, name: name)
  end

  @spec bump(GenServer.server()) :: :ok
  def bump(server), do: GenServer.cast(server, :bump)

  @spec value(GenServer.server()) :: non_neg_integer()
  def value(server), do: GenServer.call(server, :value)

  @spec crash(GenServer.server()) :: :ok
  def crash(server), do: GenServer.cast(server, :crash)

  @impl true
  def init(count), do: {:ok, count}

  @impl true
  def handle_cast(:bump, count), do: {:noreply, count + 1}
  def handle_cast(:crash, _count), do: raise("boom")

  @impl true
  def handle_call(:value, _from, count), do: {:reply, count, count}
end
```

### Step 3: `lib/basic_supervisor/supervisor.ex`

**Objective**: Encode the restart policy in `supervisor.ex` — the supervisor strategy is the lesson; the children exist to make it observable.


```elixir
defmodule BasicSupervisor.Supervisor do
  @moduledoc """
  Static supervisor with two named workers. Uses the default `:one_for_one`
  strategy: only the crashed child is restarted, siblings keep running.
  """

  use Supervisor

  @spec start_link(keyword()) :: Supervisor.on_start()
  def start_link(opts \\ []) do
    Supervisor.start_link(__MODULE__, :ok, opts)
  end

  @impl true
  def init(:ok) do
    children = [
      # We pass different :id values so the supervisor can tell the two
      # Worker children apart — without that, both would have id: Worker.
      Supervisor.child_spec({BasicSupervisor.Worker, [name: :worker_a]}, id: :worker_a),
      Supervisor.child_spec({BasicSupervisor.Worker, [name: :worker_b]}, id: :worker_b)
    ]

    # :one_for_one is the default; spelled out for clarity.
    Supervisor.init(children, strategy: :one_for_one)
  end
end
```

### Step 4: `test/basic_supervisor_test.exs`

**Objective**: Write `basic_supervisor_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule BasicSupervisorTest do
  use ExUnit.Case, async: false

  setup do
    # start_supervised!/1 ties the supervisor's lifetime to the test.
    pid = start_supervised!(BasicSupervisor.Supervisor)
    {:ok, sup: pid}
  end

  test "both workers start at boot", %{sup: sup} do
    ids =
      sup
      |> Supervisor.which_children()
      |> Enum.map(&elem(&1, 0))
      |> Enum.sort()

    assert ids == [:worker_a, :worker_b]
  end

  test "one_for_one: crashing worker_a restarts only worker_a" do
    old_a = Process.whereis(:worker_a)
    old_b = Process.whereis(:worker_b)
    assert Process.alive?(old_a)
    assert Process.alive?(old_b)

    ref = Process.monitor(old_a)
    BasicSupervisor.Worker.crash(:worker_a)
    assert_receive {:DOWN, ^ref, :process, ^old_a, _reason}, 500

    new_a = wait_for_pid(:worker_a, old_a)
    assert new_a != nil
    assert new_a != old_a
    # Sibling is untouched by :one_for_one.
    assert Process.whereis(:worker_b) == old_b
  end

  test "state is lost on restart (no persistence)" do
    BasicSupervisor.Worker.bump(:worker_a)
    BasicSupervisor.Worker.bump(:worker_a)
    assert BasicSupervisor.Worker.value(:worker_a) == 2

    old_a = Process.whereis(:worker_a)
    ref = Process.monitor(old_a)
    BasicSupervisor.Worker.crash(:worker_a)
    assert_receive {:DOWN, ^ref, :process, _, _}, 500

    _ = wait_for_pid(:worker_a, old_a)
    assert BasicSupervisor.Worker.value(:worker_a) == 0
  end

  defp wait_for_pid(name, old_pid, timeout \\ 500) do
    deadline = System.monotonic_time(:millisecond) + timeout
    do_wait(name, old_pid, deadline)
  end

  defp do_wait(name, old_pid, deadline) do
    case Process.whereis(name) do
      nil when deadline > 0 -> Process.sleep(10); do_wait(name, old_pid, deadline - 10)
      pid when pid != nil and pid != old_pid -> pid
      _ when deadline <= 0 -> nil
      _ -> Process.sleep(10); do_wait(name, old_pid, deadline - 10)
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



## Key Concepts: Process Trees and Fault Tolerance

A Supervisor is a special process that starts and monitors child processes. If a child crashes, the supervisor restarts it according to a restart strategy (`:one_for_one`, `:one_for_all`, `:rest_for_one`). Supervisors form a tree: you have a root supervisor that starts application-level services, and deeper supervisors manage pools of workers.

The key insight: **let it crash** is safe because supervisors restart failed processes automatically. You design for recovery, not prevention. Supervisors also enforce clean shutdown: when the supervisor shuts down, it waits for all children to terminate (respecting shutdown timeouts) before exiting. This is how Elixir achieves graceful shutdowns and zero-downtime deployments. The gotcha: a restart loop (process crashes, supervisor restarts, crashes again immediately) will eventually hit a max restart limit and the supervisor itself terminates. Use exponential backoff or circuit breakers to break the loop.


## Benchmark

<!-- benchmark N/A: tema conceptual -->

## Trade-offs and production gotchas

**1. State is lost on restart — by design**
A restarted GenServer runs `init/1` from scratch. Its previous state is
gone. If you need to survive restarts, persist state externally (ETS, DB)
and rehydrate in `init/1`. Do not fight this — the "let it crash" model
assumes your state was suspicious when the process crashed.

**2. Named workers make the test easy but couple callers to the name**
Registering `:worker_a` globally is fine for an example but tightly couples
consumers to a hard-coded atom. In real apps, prefer `Registry` or pass
pids via dependency injection so you can run multiple instances in tests.

**3. Default `max_restarts = 3` in 5 seconds**
If the same child crashes more than 3 times in 5 seconds, the supervisor
itself crashes, which escalates to its parent. This is a safety valve
against crash loops — 

**4. Static children only — for dynamic children use `DynamicSupervisor`**
`Supervisor` expects its `init/1` to return the full child list up front.
If you need to start workers after boot (one per user, per job, etc.),
reach for `DynamicSupervisor`.

**5. When NOT to use a plain Supervisor**
If children depend on each other in startup order AND any crash should
reset the whole group, you want `:one_for_all`. If they form a pipeline
where later stages depend on earlier ones, you want `:rest_for_one`. Plain
`:one_for_one` is for independent children only.

---


## Reflection

- Tu supervisor reinicia un child 100 veces en 5s por un bug de config. ¿Qué valores de `max_restarts`/`max_seconds` te hacen fallar rápido sin ser frágil en condiciones normales?

## Executable Example

Copy the code below into a file (e.g., `solution.exs`) and run with `elixir solution.exs`:

```elixir
defmodule BasicSupervisor.Worker do
  @moduledoc """
  A trivial GenServer used to demonstrate supervision. Holds a counter and
  exposes `bump/1`, `value/1`, and `crash/1` so tests can observe the
  effect of a restart (state is lost — counters go back to zero).
  """

  use GenServer

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    name = Keyword.fetch!(opts, :name)
    GenServer.start_link(__MODULE__, 0, name: name)
  end

  @spec bump(GenServer.server()) :: :ok
  def bump(server), do: GenServer.cast(server, :bump)

  @spec value(GenServer.server()) :: non_neg_integer()
  def value(server), do: GenServer.call(server, :value)

  @spec crash(GenServer.server()) :: :ok
  def crash(server), do: GenServer.cast(server, :crash)

  @impl true
  def init(count), do: {:ok, count}

  @impl true
  def handle_cast(:bump, count), do: {:noreply, count + 1}
  def handle_cast(:crash, _count), do: raise("boom")

  @impl true
  def handle_call(:value, _from, count), do: {:reply, count, count}
end

defmodule BasicSupervisor.Supervisor do
  @moduledoc """
  Static supervisor with two named workers. Uses the default `:one_for_one`
  strategy: only the crashed child is restarted, siblings keep running.
  """

  use Supervisor

  @spec start_link(keyword()) :: Supervisor.on_start()
  def start_link(opts \\ []) do
    Supervisor.start_link(__MODULE__, :ok, opts)
  end

  @impl true
  def init(:ok) do
    children = [
      # We pass different :id values so the supervisor can tell the two
      # Worker children apart — without that, both would have id: Worker.
      Supervisor.child_spec({BasicSupervisor.Worker, [name: :worker_a]}, id: :worker_a),
      Supervisor.child_spec({BasicSupervisor.Worker, [name: :worker_b]}, id: :worker_b)
    ]

    # :one_for_one is the default; spelled out for clarity.
    Supervisor.init(children, strategy: :one_for_one)
  end
end

# Demonstrate supervision with real assertions
IO.puts("=== BasicSupervisor Demo ===")
{:ok, _sup} = BasicSupervisor.Supervisor.start_link()

# Verify both workers are running
assert Process.whereis(:worker_a) != nil
assert Process.whereis(:worker_b) != nil

old_a = Process.whereis(:worker_a)
old_b = Process.whereis(:worker_b)

# Bump and verify state
BasicSupervisor.Worker.bump(:worker_a)
BasicSupervisor.Worker.bump(:worker_a)
assert BasicSupervisor.Worker.value(:worker_a) == 2
assert BasicSupervisor.Worker.value(:worker_b) == 0

IO.puts("Supervisor started both workers successfully!")
IO.puts("Worker A counter: #{BasicSupervisor.Worker.value(:worker_a)}")
IO.puts("Worker B counter: #{BasicSupervisor.Worker.value(:worker_b)}")
IO.puts("All supervision assertions passed!")
```


## Resources

- [`Supervisor` — Elixir stdlib](https://hexdocs.pm/elixir/Supervisor.html)
- ["Supervisor and Application" — Elixir getting started](https://hexdocs.pm/elixir/supervisor-and-application.html)
- [Erlang `supervisor` behavior](https://www.erlang.org/doc/man/supervisor.html) — the canonical reference
- [Designing for Scalability with Erlang/OTP — Ch. 8 "Supervisors"](https://www.oreilly.com/library/view/designing-for-scalability/9781449361556/) — the clearest explanation of restart strategies in print


## Advanced Considerations

Supervision trees encode your application's fault tolerance strategy. The tree structure, restart policy, and shutdown semantics directly determine behavior during crashes, dependencies, and graceful shutdown.

**Supervision tree design:**
A well-designed tree mirrors data/message flow: dependencies point upward. If process A depends on process B, B should be higher in the tree (started first, shut down last). Supervisor strategies (`:one_for_one`, `:one_for_all`, `:rest_for_one`) define the scope of cascading restarts. `:one_for_one` isolates failures (each crash restarts only that child); `:one_for_all` is for tightly-coupled groups (e.g., a reader-writer pair).

**Restart strategies and intensity:**
`max_restarts: 3, max_seconds: 5` means "if 3+ restarts occur in 5 seconds, kill the supervisor." This circuit-breaker pattern prevents restart loops that consume resources. The key decision: should a crashing child take down the whole app (escalate to parent) or just itself? Transient/temporary children exit "cleanly" and don't trigger restarts — useful for request handlers.

**Error propagation and shutdown ordering:**
When a supervisor exits, it sends `:shutdown` to children in reverse start order (LIFO). Children have `shutdown: 5000` milliseconds to terminate gracefully before hard killing. Nested supervisors propagate this signal recursively. Understanding this order prevents resource leaks: a child waiting on another child's graceful shutdown will deadlock if not designed carefully.
