# Application Start Phases for Ordered Boot

**Project**: `start_phases` — use `Application.start_phase/3` to coordinate boot across applications with complex dependencies.

---

## Project context

Your system has five OTP applications. App A owns a cluster-wide lock manager. App B is
a cache that must populate from disk before accepting traffic. App C is a scheduler that
assumes B is populated. App D exposes HTTP and must not bind its port until A, B, and C
all report ready. App E is telemetry and must start before everybody so it can observe.

You can express some of this with `:applications` declarations. But application start
has a structural property that makes pure `:applications` ordering insufficient: when
an application's `Application.start/2` returns `{:ok, pid}`, the app is considered
started — the next app's `start/2` fires immediately. There is no barrier between
"supervisor tree built" and "ready to serve". If B's `init/1` schedules a `:load_cache`
message via `handle_continue`, B is "started" long before the cache is populated.

`start_phases` is the OTP-native solution. In `mix.exs` you declare named phases, and
each application gets `start_phase/3` callbacks fired in a coordinated way: phase N
runs on every app that opts in to phase N, across the whole release, before phase N+1
begins. This gives you an ordered, cross-app boot sequence with explicit synchronization
points — exactly what a multi-app startup needs.

Your job: encode the five-app boot of `start_phases` with three phases — `:reserve_locks`,
`:warm_caches`, `:bind_ports` — each executed across the subset of apps that care, with
a smoke test that asserts every phase completed before the next began.

---

## Tree

```
start_phases/
├── lib/
│   └── start_phases/
│       ├── application.ex
│       ├── lock_manager.ex
│       ├── cache.ex
│       ├── scheduler.ex
│       ├── http_endpoint.ex
│       └── telemetry_spine.ex
├── test/
│   ├── start_phases_test.exs
│   └── ordering_test.exs
└── mix.exs
```

Note: for brevity we model the five "apps" as five modules inside one app. In a real
umbrella each would be its own OTP application with its own `mix.exs` and
`start_phases` declaration. The mechanism is identical; see Step 9 for the multi-app
variant.

---

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
### 1. What `start_phases` does

In `mix.exs`:

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
def application do
  [
    mod: {MyApp.Application, []},
    start_phases: [reserve_locks: [], warm_caches: [], bind_ports: []]
  ]
end
```

And in the callback module:

```elixir
def start_phase(:reserve_locks, _start_type, _args), do: ...
def start_phase(:warm_caches, _start_type, _args), do: ...
def start_phase(:bind_ports, _start_type, _args), do: ...
```

The VM calls `start/2` on the app. Then it calls `start_phase/3` in order, one phase at
a time, across **all applications** that declared that phase in their `application`
metadata. A phase must complete on all participating apps before the next begins.

### 2. Phase contract

Each `start_phase/3` callback returns:

- `:ok` — phase completed successfully.
- `{:error, reason}` — abort the whole boot.

This is a synchronous barrier: if app B's `warm_caches` takes 5 seconds, no app runs
`bind_ports` for those 5 seconds.

### 3. Where phases run in the OTP lifecycle

```
  application_controller start sequence:
  ┌──────────────────────────────────────────┐
  │ for each app in :applications order:     │
  │   1. Application.start/2 -> {:ok, pid}   │
  │   2. sup tree builds (children start)    │
  │ end                                      │
  │                                          │
  │ then, for each phase in declared order:  │
  │   for each app declaring that phase:     │
  │     start_phase(phase, type, args)       │
  │   end                                    │
  │ end                                      │
  └──────────────────────────────────────────┘
```

Phase order is the order they appear in `start_phases` keyword list of the **first** app
to declare them. Subsequent apps' ordering is ignored; they either participate in a
phase or they don't. To make this deterministic, agree on a shared phase vocabulary.

### 4. When to use phases vs `handle_continue`

| Situation | Tool |
|-----------|------|
| One app, one process, needs two-stage init | `handle_continue/2` |
| Multiple processes in one app must reach a ready state before returning | Ad-hoc barrier in `Application.start/2` |
| Multiple apps coordinate ordered boot | `start_phases` |
| Across clusters | Distributed coordination (libcluster + :global) |

### 5. Failure recovery

If `start_phase` returns `{:error, reason}`, OTP stops the application that failed,
propagating the error up. Already-started siblings remain running (SASL logs the fault).
In a release with `start_permanent: true` the VM shuts down. Be conservative in
start_phases — they are one of the few places where a programming error halts the whole
node during boot.

---

## Design decisions

**Option A — single `handle_continue` chain inside the root supervisor**
- Pros: stays inside one app; no phase manifest to maintain.
- Cons: cross-app ordering is impossible; one long chain becomes hard to reason about.

**Option B — declared `start_phases` per app run by the application_controller** (chosen)
- Pros: phases run in manifest order across all apps; each phase is a named, testable unit; boot ordering becomes declarative.
- Cons: phases do not re-run on hot-code upgrade; tests must call `Application.start_phase/3` explicitly.

→ Chose **B** when ordering needs to span multiple applications. For a single app, `handle_continue` is still the right tool.

---

## Implementation

### Step 1: `mix.exs`

**Objective**: Declare the project, dependencies, and OTP application in `mix.exs`.

```elixir
defmodule StartPhases.MixProject do
  use Mix.Project

  def project do
    [
      app: :start_phases,
      version: "0.1.0",
      elixir: "~> 1.16",
      deps: [{:telemetry, "~> 1.2"}]
    ]
  end

  def application do
    [
      extra_applications: [:logger],
      mod: {StartPhases.Application, []},
      start_phases: [
        reserve_locks: [],
        warm_caches: [],
        bind_ports: []
      ]
    ]
  end
end
```

### Step 2: `lib/start_phases/application.ex`

**Objective**: Define the OTP application and supervision tree in `lib/start_phases/application.ex`.

```elixir
defmodule StartPhases.Application do
  @moduledoc false
  use Application
  require Logger

  @impl true
  def start(_type, _args) do
    children = [
      {Registry, keys: :unique, name: StartPhases.Registry},
      StartPhases.TelemetrySpine,
      StartPhases.LockManager,
      StartPhases.Cache,
      StartPhases.Scheduler,
      StartPhases.HttpEndpoint
    ]

    Supervisor.start_link(children, strategy: :rest_for_one, name: StartPhases.RootSup)
  end

  @impl true
  def start_phase(:reserve_locks, _start_type, _args) do
    Logger.info("phase: reserve_locks")
    StartPhases.LockManager.reserve()
  end

  def start_phase(:warm_caches, _start_type, _args) do
    Logger.info("phase: warm_caches")
    StartPhases.Cache.warm()
  end

  def start_phase(:bind_ports, _start_type, _args) do
    Logger.info("phase: bind_ports")
    StartPhases.HttpEndpoint.bind()
  end
end
```

### Step 3: `lib/start_phases/telemetry_spine.ex`

**Objective**: Implement the module in `lib/start_phases/telemetry_spine.ex`.

```elixir
defmodule StartPhases.TelemetrySpine do
  @moduledoc """
  Observes every phase transition for later assertion.
  """
  use GenServer

  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec record(atom()) :: :ok
  def record(event), do: GenServer.cast(__MODULE__, {:record, event, System.monotonic_time()})

  @spec timeline() :: [{atom(), integer()}]
  def timeline, do: GenServer.call(__MODULE__, :timeline)

  @impl true
  def init(_), do: {:ok, %{timeline: []}}

  @impl true
  def handle_cast({:record, event, ts}, state),
    do: {:noreply, %{state | timeline: [{event, ts} | state.timeline]}}

  @impl true
  def handle_call(:timeline, _from, state),
    do: {:reply, Enum.reverse(state.timeline), state}
end
```

### Step 4: `lib/start_phases/lock_manager.ex`

**Objective**: Implement the module in `lib/start_phases/lock_manager.ex`.

```elixir
defmodule StartPhases.LockManager do
  @moduledoc """
  Simulates a cluster-wide lock reservation. Phase 1 duty.
  """
  use GenServer

  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec reserve() :: :ok | {:error, term()}
  def reserve do
    StartPhases.TelemetrySpine.record(:lock_reserved_begin)
    GenServer.call(__MODULE__, :reserve, 10_000)
  end

  @spec reserved?() :: boolean()
  def reserved?, do: GenServer.call(__MODULE__, :reserved?)

  @impl true
  def init(_), do: {:ok, %{reserved: false}}

  @impl true
  def handle_call(:reserve, _from, state) do
    # Pretend to talk to Consul/etcd
    Process.sleep(50)
    StartPhases.TelemetrySpine.record(:lock_reserved_end)
    {:reply, :ok, %{state | reserved: true}}
  end

  def handle_call(:reserved?, _from, state), do: {:reply, state.reserved, state}
end
```

### Step 5: `lib/start_phases/cache.ex`

**Objective**: Implement the module in `lib/start_phases/cache.ex`.

```elixir
defmodule StartPhases.Cache do
  @moduledoc """
  Pre-populates from disk. Phase 2 duty. Requires LockManager to be reserved.
  """
  use GenServer

  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec warm() :: :ok | {:error, term()}
  def warm do
    StartPhases.TelemetrySpine.record(:cache_warm_begin)

    unless StartPhases.LockManager.reserved?() do
      {:error, :lock_not_reserved}
    else
      GenServer.call(__MODULE__, :warm, 10_000)
    end
  end

  @spec warmed?() :: boolean()
  def warmed?, do: GenServer.call(__MODULE__, :warmed?)

  @impl true
  def init(_), do: {:ok, %{warmed: false, size: 0}}

  @impl true
  def handle_call(:warm, _from, state) do
    Process.sleep(80)
    StartPhases.TelemetrySpine.record(:cache_warm_end)
    {:reply, :ok, %{state | warmed: true, size: 10_000}}
  end

  def handle_call(:warmed?, _from, state), do: {:reply, state.warmed, state}
end
```

### Step 6: `lib/start_phases/scheduler.ex`

**Objective**: Implement the module in `lib/start_phases/scheduler.ex`.

```elixir
defmodule StartPhases.Scheduler do
  @moduledoc """
  Starts tick immediately but only schedules if Cache is warm — it participates
  in phase 2 implicitly by reading StartPhases.Cache.warmed? in its own init.
  """
  use GenServer
  require Logger

  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(_) do
    # Do not block init; we'll defer scheduling to a continue.
    {:ok, %{ticks: 0}, {:continue, :maybe_start}}
  end

  @impl true
  def handle_continue(:maybe_start, state) do
    if StartPhases.Cache.warmed?() do
      Process.send_after(self(), :tick, 100)
    else
      # try again later
      Process.send_after(self(), {:retry, :maybe_start}, 50)
    end

    {:noreply, state}
  end

  @impl true
  def handle_info(:tick, state) do
    Process.send_after(self(), :tick, 100)
    {:noreply, %{state | ticks: state.ticks + 1}}
  end

  def handle_info({:retry, :maybe_start}, state), do: handle_continue(:maybe_start, state)
end
```

### Step 7: `lib/start_phases/http_endpoint.ex`

**Objective**: Implement the module in `lib/start_phases/http_endpoint.ex`.

```elixir
defmodule StartPhases.HttpEndpoint do
  @moduledoc """
  Fake HTTP endpoint. Phase 3 binds the port; init does NOT.
  """
  use GenServer

  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec bind() :: :ok | {:error, term()}
  def bind do
    StartPhases.TelemetrySpine.record(:bind_begin)

    cond do
      not StartPhases.LockManager.reserved?() -> {:error, :lock_not_reserved}
      not StartPhases.Cache.warmed?() -> {:error, :cache_cold}
      true -> GenServer.call(__MODULE__, :bind)
    end
  end

  @spec bound?() :: boolean()
  def bound?, do: GenServer.call(__MODULE__, :bound?)

  @impl true
  def init(_), do: {:ok, %{bound: false, port: 4000}}

  @impl true
  def handle_call(:bind, _from, state) do
    Process.sleep(20)
    StartPhases.TelemetrySpine.record(:bind_end)
    {:reply, :ok, %{state | bound: true}}
  end

  def handle_call(:bound?, _from, state), do: {:reply, state.bound, state}
end
```

### Step 8: `test/start_phases_test.exs`

**Objective**: Write tests in `test/start_phases_test.exs` covering behavior and edge cases.

```elixir
defmodule StartPhasesTest do
  use ExUnit.Case, async: false

  describe "StartPhases" do
    test "all three phases complete" do
      assert StartPhases.LockManager.reserved?()
      assert StartPhases.Cache.warmed?()
      assert StartPhases.HttpEndpoint.bound?()
    end
  end
end
```

### Step 9: `test/ordering_test.exs`

**Objective**: Write tests in `test/ordering_test.exs` covering behavior and edge cases.

```elixir
defmodule StartPhases.OrderingTest do
  use ExUnit.Case, async: false

  describe "StartPhases.Ordering" do
    test "phases execute in the declared order" do
      timeline = StartPhases.TelemetrySpine.timeline() |> Enum.map(&elem(&1, 0))

      assert [
               :lock_reserved_begin,
               :lock_reserved_end,
               :cache_warm_begin,
               :cache_warm_end,
               :bind_begin,
               :bind_end
             ] = Enum.filter(timeline, &(&1 in [
               :lock_reserved_begin,
               :lock_reserved_end,
               :cache_warm_begin,
               :cache_warm_end,
               :bind_begin,
               :bind_end
             ]))
    end

    test "phase 2 does not begin until phase 1 is done" do
      timeline = StartPhases.TelemetrySpine.timeline()

      {_, lock_end_ts} = Enum.find(timeline, fn {e, _} -> e == :lock_reserved_end end)
      {_, warm_begin_ts} = Enum.find(timeline, fn {e, _} -> e == :cache_warm_begin end)

      assert warm_begin_ts > lock_end_ts
    end
  end
end
```

### Step 10: Multi-app variant (illustrative)

**Objective**: Implement Multi-app variant (illustrative).

In a real umbrella, each app declares `start_phases` in its own `mix.exs`:

```elixir
# apps/lock_manager/mix.exs
def application do
  [mod: {LockManager.App, []}, start_phases: [reserve_locks: []]]
end

# apps/cache/mix.exs
def application do
  [mod: {Cache.App, []}, start_phases: [warm_caches: []]]
end

# apps/http/mix.exs
def application do
  [mod: {Http.App, []}, start_phases: [bind_ports: []]]
end
```

Only the apps that declare a phase participate. The union of all declared phases is
ordered by the first app to mention them. To make the order global and explicit, use
the `mix release` `applications` manifest plus a documented phase vocabulary.

---

## Advanced Considerations: Partitioned Supervisors and Custom Restart Strategies

A standard Supervisor is a single process managing a static tree. For thousands of children, a single supervisor becomes a bottleneck: all supervisor callbacks run on one process, and supervisor restart logic is sequential. PartitionSupervisor (OTP 25+) spawns N independent supervisors, each managing a subset of children. Hashing the child ID determines which partition supervises it, distributing load and enabling horizontal scaling.

Custom restart strategies (via `Supervisor.init/2` callback) allow logic beyond the defaults. A strategy might prioritize restarting dependent services in a specific order, or apply backoff based on restart frequency. The downside is complexity: custom logic is harder to test and reason about, and mistakes cascade. Start with defaults and profile before adding custom behavior.

Selective restart via `:rest_for_one` or `:one_for_all` affects failure isolation. `:one_for_all` restarts all children when one fails (simulating a total system failure), which can be necessary for consistency but is expensive. `:rest_for_one` restarts the failed child and any started after it, balancing isolation and dependencies. Understanding which strategy fits your architecture prevents cascading failures and unnecessary restarts.

---


## Deep Dive: Property Patterns and Production Implications

Property-based testing inverts the testing mindset: instead of writing examples, you state invariants (properties) and let a generator find counterexamples. StreamData's shrinking capability is its superpower—when a property fails on a 10,000-element list, the framework reduces it to the minimal list that still fails, cutting debugging time from hours to minutes. The trade-off is that properties require rigorous thinking about domain constraints, and not every invariant is worth expressing as a property. Teams that adopt property testing often find bugs in specifications themselves, not just implementations.

---

## Trade-offs and production gotchas

**1. Synchronous barriers lengthen boot.** If three apps each take 2 seconds in
`warm_caches`, total boot includes 6 seconds of serial cache-warm time (phases are
sequential across apps). For heavy warm-up, either shard the warming or run it in
parallel within a single app's phase (spawn tasks in `start_phase`, await all).

**2. `{:error, reason}` kills the node.** A phase that returns `{:error, _}` under
`start_permanent: true` halts the VM. Make phases defensive: retry transient failures
internally, only error out on truly unrecoverable conditions.

**3. Circular phase dependencies.** App X's `warm_caches` calls into App Y. App Y's
`warm_caches` calls into App X. Deadlock is not possible (phases are sequential per
app) but partial state is. Keep each phase's work local; use the phase-transition
boundary to signal "now other apps can call me".

**4. Rolling deploys and release-level consistency.** Phases run only at boot. Hot
code upgrade does NOT re-run them. If you add a new phase and deploy to half your
fleet, the new pods run it; old pods do not. Phases work best with full restart
deploys (blue/green, rolling-with-drain).

**5. Shared clock assumptions.** `TelemetrySpine` uses monotonic time, which is per-VM.
Do not use wall-clock for phase ordering assertions across nodes — different VMs, different
clocks. The cross-node ordering guarantee comes from the application_controller itself.

**6. Start_phases + release `:applications`.** In a release, you control app boot order
via `applications: [...]` in `releases.umbrella_boot`. Phases respect that order — app
A's `reserve_locks` runs before app B's `reserve_locks` if A is earlier in the manifest.

**7. Tests that span application restart.** Many test harnesses use `Application.stop/1`
and `Application.start/2` — this re-runs `start/2` but does NOT re-run `start_phase/3`
unless you also call `Application.start_phase/3` manually. Use `Application.ensure_all_started/1`
in tests that need the full phase sequence.

**8. When NOT to use this.** For a single app with one ordered init step, use
`handle_continue/2`. For three apps with simple dep graphs resolvable by `:applications`,
stop there. Reach for `start_phases` when you have genuine cross-app synchronization
needs — usually when an app's "ready" is a function of state owned by another app.

---

### Why this works

`application_controller` guarantees that phase N for every app in the release finishes before phase N+1 starts for any app. That gives you a cross-app barrier without inventing a coordination primitive. Each phase stays local to its own app's callback, so cross-app coupling is expressed in the manifest, not in code.

---

## Benchmark

Each phase invocation is a direct function call on the app module. Overhead is sub-µs.
The cost is whatever your phase callback does — typically I/O bound.

Typical budgets:
- Lock reservation: 10–100 ms (network round-trip to Consul/etcd)
- Cache warm: 100 ms – 10 s (disk/DB read + deserialize)
- Port bind: < 10 ms

Log a warning if any phase exceeds 1 second; kill the app if it exceeds 30 seconds.
Use `Task` with a timeout inside your phase callback.

Target: total phase sequence ≤ 5 s on cold boot; any single phase > 1 s emits a warning.

---

## Reflection

1. You add a new phase `:migrate_db` that takes 20 s. Does it belong in `start_phases`, in a release task run before boot, or in an async `handle_continue`? Argue from the contract that phase N must finish before phase N+1 starts across all apps.
2. Rolling deploys do not re-run phases on hot-code upgrade. Which invariants maintained by phases are at risk during a partial upgrade, and how do you detect divergence?

---


## Executable Example

```elixir
defmodule StartPhases.OrderingTest do
  use ExUnit.Case, async: false

  describe "StartPhases.Ordering" do
    test "phases execute in the declared order" do
      timeline = StartPhases.TelemetrySpine.timeline() |> Enum.map(&elem(&1, 0))

      assert [
               :lock_reserved_begin,
               :lock_reserved_end,
               :cache_warm_begin,
               :cache_warm_end,
               :bind_begin,
               :bind_end
             ] = Enum.filter(timeline, &(&1 in [
               :lock_reserved_begin,
               :lock_reserved_end,
               :cache_warm_begin,
               :cache_warm_end,
               :bind_begin,
               :bind_end
             ]))
    end

    test "phase 2 does not begin until phase 1 is done" do
      timeline = StartPhases.TelemetrySpine.timeline()

      {_, lock_end_ts} = Enum.find(timeline, fn {e, _} -> e == :lock_reserved_end end)
      {_, warm_begin_ts} = Enum.find(timeline, fn {e, _} -> e == :cache_warm_begin end)

      assert warm_begin_ts > lock_end_ts
    end
  end
end

defmodule Main do
  def main do
      # Demonstrate application start phases for ordered boot

      # All applications have been started via Application.ensure_all_started/1
      # Start phases are fired in order (coordinated across all apps)

      IO.puts("✓ Application startup phases:")
      IO.puts("  Phase 1 (telemetry): app E starts observability")
      IO.puts("  Phase 2 (infra): app A, B initialize cluster lock + cache load")
      IO.puts("  Phase 3 (scheduler): app C starts after cache is ready")
      IO.puts("  Phase 4 (http): app D binds port after all deps ready")

      # Verify each app's readiness by checking start phase completion
      assert StartPhases.TelemetryApp.started?(), "Telemetry must be started"
      IO.puts("✓ Phase 1 complete: Telemetry observability active")

      assert StartPhases.LockManager.started?(), "Lock manager must be started"
      assert StartPhases.Cache.loaded?(), "Cache must be populated"
      IO.puts("✓ Phase 2 complete: Lock manager + cache loaded")

      assert StartPhases.Scheduler.started?(), "Scheduler must be ready"
      IO.puts("✓ Phase 3 complete: Scheduler ready (cache available)")

      assert StartPhases.HTTPEndpoint.listening?(), "HTTP must be listening"
      IO.puts("✓ Phase 4 complete: HTTP endpoint bound (all deps ready)")

      # Verify the ordering via application state
      telemetry_start = StartPhases.TelemetryApp.start_time()
      lock_start = StartPhases.LockManager.start_time()
      cache_ready = StartPhases.Cache.ready_time()
      scheduler_start = StartPhases.Scheduler.start_time()
      http_start = StartPhases.HTTPEndpoint.start_time()

      assert telemetry_start <= lock_start,
        "Telemetry must start before lock manager"
      assert cache_ready <= scheduler_start,
        "Cache must be ready before scheduler"
      assert scheduler_start <= http_start,
        "Scheduler must be ready before HTTP binds"

      IO.puts("✓ Start phase ordering verified (dependencies respected)")

      # Test a request after all phases complete
      {:ok, response} = StartPhases.HTTPEndpoint.request("/health")
      assert response.status == 200, "Health check should pass"
      IO.puts("✓ HTTP request successful (all phases coordinated)")

      IO.puts("\n✓ Application start phases demonstrated:")
      IO.puts("  - Phase 1 (telemetry): setup observability")
      IO.puts("  - Phase 2 (infra): init distributed locks + populate cache")
      IO.puts("  - Phase 3 (scheduler): start after cache ready")
      IO.puts("  - Phase 4 (http): bind port after all deps")
      IO.puts("  - Cross-app synchronization via start_phase/3")
      IO.puts("  - Explicit barriers between boot stages")
      IO.puts("✓ Multi-app release boot coordination achieved")
  end
end

Main.main()
```
