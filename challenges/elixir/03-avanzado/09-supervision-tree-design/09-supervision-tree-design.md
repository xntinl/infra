# Supervision Tree Design: Modeling Failure Domains

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`. After several months in production the application has
grown organically. Components were added where convenient rather than where they belong
architecturally. The result: a `MetricsReporter` that crashes when Datadog is unreachable
takes down `PaymentService` — a service that handles live transactions and should never
be impacted by telemetry failures.

This exercise redesigns the full supervision tree from scratch, applying failure domain
analysis to produce a tree that is both correct and maintainable.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex              # ← full redesign here
│       ├── supervisors/
│       │   ├── core_supervisor.ex      # ← you implement
│       │   ├── middleware_supervisor.ex # ← you implement
│       │   └── telemetry_supervisor.ex # ← you implement
│       ├── rate_limiter/
│       ├── circuit_breaker/
│       ├── route_table/
│       ├── middleware/
│       └── telemetry/
├── test/
│   └── api_gateway/
│       └── supervision_tree_test.exs   # given tests — must pass
└── mix.exs
```

---

## Principles for supervision tree design

### Dependency ordering

Children start **in order** and terminate **in reverse order**. This is not optional —
it is the OTP contract:

```elixir
children = [
  ApiGateway.RateLimiter.Server,    # starts 1st, stops last
  ApiGateway.RouteTable.Server,     # starts 2nd, stops 2nd to last
  ApiGateway.Router,                # starts 3rd, stops 1st
]
```

If `RateLimiter.Server` fails to start, `RouteTable.Server` and `Router` never start.
This is correct behaviour — you do not want a router running without a rate limiter.

On shutdown, `Router` stops first (drains in-flight requests), then `RouteTable.Server`
(can still serve route lookups during drain), then `RateLimiter.Server` last.

### Failure domains

A **failure domain** is the set of components that must fail or recover together.
Two components belong to the same domain if the crash of one leaves the other in
an invalid or inconsistent state.

```
Core domain (critical — gateway cannot operate without these):
  RateLimiter.Server, RouteTable.Server, CircuitBreaker.Supervisor, Router

Middleware domain (important — gateway degrades without these, does not stop):
  AuditWriter, PriorityDispatcher

Telemetry domain (optional — gateway operates normally without these):
  MetricsReporter, HealthChecker
```

Separating into supervisor subtrees means that the telemetry domain crashing (hitting
its `max_restarts`) only takes down the telemetry supervisor, not the core supervisor.

### Circular dependency deadlock

Circular dependencies in `init/1` cause silent deadlocks at startup:

```
Process A: init/1 → GenServer.call(ProcessB, :ready?) → waits
Process B: init/1 → GenServer.call(ProcessA, :config) → waits

Neither returns → supervisor waits forever → application never starts
```

Detection: startup hangs with no error logged. Use `handle_continue/2` to defer any
calls to other processes until after `init/1` returns.

---

## Implementation

### Step 1: Design the tree on paper first

Before writing code, draw the full tree in comments:

```
ApiGateway.Application
  → ApiGateway.Supervisors.CoreSupervisor          (rest_for_one)
      → ApiGateway.RateLimiter.Partitions           PartitionSupervisor
      → ApiGateway.RouteTable.Server                GenServer
      → ApiGateway.CircuitBreaker.Supervisor        DynamicSupervisor
      → ApiGateway.TaskSupervisor                   Task.Supervisor
  → ApiGateway.Supervisors.MiddlewareSupervisor     (one_for_one)
      → ApiGateway.Middleware.AuditWriter            GenServer
      → ApiGateway.Middleware.PriorityDispatcher     GenServer
  → ApiGateway.Supervisors.TelemetrySupervisor      (one_for_one)
      → ApiGateway.Telemetry.Reporter               GenServer  :transient
      → ApiGateway.Telemetry.HealthChecker          GenServer  :permanent
```

### Step 2: `lib/api_gateway/application.ex`

The top-level supervisor uses `:one_for_one` because the three domain supervisors are
independent failure domains. A telemetry crash should never cascade to core components.

```elixir
defmodule ApiGateway.Application do
  use Application
  require Logger

  @impl true
  def start(_type, _args) do
    Logger.info("ApiGateway starting")

    children = [
      # Order matters: Core must be up before Middleware can serve requests.
      # Telemetry is independent — can start last.
      ApiGateway.Supervisors.CoreSupervisor,
      ApiGateway.Supervisors.MiddlewareSupervisor,
      ApiGateway.Supervisors.TelemetrySupervisor
    ]

    Supervisor.start_link(children,
      strategy: :one_for_one,
      name:     ApiGateway.Supervisor
    )
  end

  @impl true
  def prep_stop(_state) do
    Logger.info("ApiGateway initiating graceful shutdown")
  end

  @impl true
  def stop(_state) do
    Logger.info("ApiGateway shutdown complete")
  end
end
```

### Step 3: `lib/api_gateway/supervisors/core_supervisor.ex`

The CoreSupervisor uses `:rest_for_one` because children form a linear dependency chain.
The PartitionedRateLimiter is first because everything that follows may call it. If the
rate limiter crashes and restarts with fresh ETS tables, the RouteTable, CircuitBreaker,
and TaskSupervisor should also restart to clear any stale handles.

```elixir
defmodule ApiGateway.Supervisors.CoreSupervisor do
  use Supervisor

  def start_link(opts) do
    Supervisor.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(_opts) do
    children = [
      # 1. Rate limiter partitions — everything else depends on rate limiting
      {PartitionSupervisor,
        child_spec: ApiGateway.RateLimiter.Server,
        name: ApiGateway.RateLimiter.Partitions,
        partitions: System.schedulers_online()},

      # 2. Route table — loaded lazily via handle_continue, does not block startup
      {ApiGateway.RouteTable.Server, [traffic_class: :default]},

      # 3. Circuit breaker dynamic supervisor — workers added at runtime
      ApiGateway.CircuitBreaker.Supervisor,

      # 4. Task supervisor — used by watchdog, webhook notifier, upstream prober
      {Task.Supervisor, name: ApiGateway.TaskSupervisor}
    ]

    Supervisor.init(children,
      strategy:     :rest_for_one,
      max_restarts: 5,
      max_seconds:  30
    )
  end
end
```

### Step 4: `lib/api_gateway/supervisors/middleware_supervisor.ex`

AuditWriter and PriorityDispatcher are independent — if one crashes, the other keeps
working. `:one_for_one` is the correct strategy.

```elixir
defmodule ApiGateway.Supervisors.MiddlewareSupervisor do
  use Supervisor

  def start_link(opts) do
    Supervisor.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(_opts) do
    children = [
      ApiGateway.Middleware.AuditWriter,
      ApiGateway.Middleware.PriorityDispatcher
    ]

    Supervisor.init(children,
      strategy:     :one_for_one,
      max_restarts: 10,
      max_seconds:  60
    )
  end
end
```

### Step 5: `lib/api_gateway/supervisors/telemetry_supervisor.ex`

The Reporter uses `:transient` restart — if it exits cleanly (`:normal` or `:shutdown`),
it is not restarted. This prevents crash loops when the Datadog agent is permanently
unavailable. The HealthChecker uses `:permanent` because we always want health checks.

```elixir
defmodule ApiGateway.Supervisors.TelemetrySupervisor do
  use Supervisor

  def start_link(opts) do
    Supervisor.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(_opts) do
    children = [
      # Reporter: :transient restart — if it exits cleanly (e.g., Datadog agent
      # permanently unavailable and it gives up), do not restart it in a loop.
      %{
        id:      ApiGateway.Telemetry.Reporter,
        start:   {ApiGateway.Telemetry.Reporter, :start_link, [[]]},
        restart: :transient
      },
      # HealthChecker: :permanent — we always want health checks running.
      {ApiGateway.Telemetry.HealthChecker, []}
    ]

    # Generous thresholds: telemetry workers are legitimately noisy.
    # Let them crash 20 times per minute before we give up entirely.
    Supervisor.init(children,
      strategy:     :one_for_one,
      max_restarts: 20,
      max_seconds:  60
    )
  end
end
```

### Step 6: Fix the RouteTable startup circular dependency

The RouteTable server uses `handle_continue` so `init/1` returns immediately. By the
time `handle_continue` runs, all siblings in CoreSupervisor have started — safe to call
any other core component.

```elixir
# lib/api_gateway/route_table/server.ex

@impl true
def init(opts) do
  # Return immediately — supervisor can continue starting other children.
  {:ok, %{routes: %{}, ready: false, traffic_class: Keyword.get(opts, :traffic_class, :default)},
   {:continue, :load_routes}}
end

@impl true
def handle_continue(:load_routes, state) do
  # By the time handle_continue runs, all siblings in CoreSupervisor are started.
  # Safe to call any other core component here.
  case ApiGateway.RouteTable.Loader.load(state.traffic_class) do
    {:ok, routes} ->
      {:noreply, %{state | routes: routes, ready: true}}

    {:error, reason} ->
      require Logger
      Logger.error("Failed to load routes: #{inspect(reason)}, retrying...")
      {:noreply, state, {:continue, :load_routes}}
  end
end
```

### Step 7: Given tests — must pass without modification

```elixir
# test/api_gateway/supervision_tree_test.exs
defmodule ApiGateway.SupervisionTreeTest do
  use ExUnit.Case, async: false

  describe "failure domain isolation" do
    test "telemetry supervisor crash does not affect core components" do
      rate_limiter_pid = Process.whereis(ApiGateway.RateLimiter.Server) ||
        GenServer.whereis({:via, PartitionSupervisor,
          {ApiGateway.RateLimiter.Partitions, 0}})

      reporter_pid = Process.whereis(ApiGateway.Telemetry.Reporter)

      if reporter_pid && Process.alive?(reporter_pid) do
        ref = Process.monitor(reporter_pid)
        Process.exit(reporter_pid, :kill)
        assert_receive {:DOWN, ^ref, :process, _, _}, 1_000
      end

      Process.sleep(200)

      # Core supervisor must be unaffected
      core_pid = Process.whereis(ApiGateway.Supervisors.CoreSupervisor)
      assert core_pid != nil
      assert Process.alive?(core_pid)
    end

    test "middleware supervisor is independent of core supervisor" do
      audit_pid = Process.whereis(ApiGateway.Middleware.AuditWriter)
      assert audit_pid != nil

      core_pid = Process.whereis(ApiGateway.Supervisors.CoreSupervisor)
      assert core_pid != nil

      # They should be under different supervisors
      audit_sup = Process.info(audit_pid, :dictionary)[:"$ancestors"] |> List.first()
      core_name = ApiGateway.Supervisors.CoreSupervisor
      assert audit_sup != Process.whereis(core_name)
    end

    test "all three domain supervisors are running at startup" do
      assert Process.alive?(Process.whereis(ApiGateway.Supervisors.CoreSupervisor))
      assert Process.alive?(Process.whereis(ApiGateway.Supervisors.MiddlewareSupervisor))
      assert Process.alive?(Process.whereis(ApiGateway.Supervisors.TelemetrySupervisor))
    end
  end

  describe "startup ordering" do
    test "core components are available before middleware" do
      # CoreSupervisor starts first, so its children exist before MiddlewareSupervisor
      audit_pid = Process.whereis(ApiGateway.Middleware.AuditWriter)
      route_table_pid = Process.whereis(ApiGateway.RouteTable.Server)

      # Both must be alive — ordering was correct
      assert audit_pid != nil
      assert route_table_pid != nil
    end
  end

  describe "dynamic circuit breaker workers" do
    test "workers can be added after startup" do
      {:ok, pid} = ApiGateway.CircuitBreaker.Supervisor.start_worker("test-upstream")
      assert Process.alive?(pid)
      assert Enum.member?(ApiGateway.CircuitBreaker.Supervisor.list_workers(), pid)
    end
  end
end
```

### Step 8: Run the tests

```bash
mix test test/api_gateway/supervision_tree_test.exs --trace
```

---

## Trade-off analysis

| Design choice | Benefit | Risk |
|---------------|---------|------|
| Three-layer supervisor hierarchy | Failures contained to their domain | More modules to maintain |
| `rest_for_one` in CoreSupervisor | Dependent children restart on dependency crash | Order of children list is load-bearing |
| `:transient` for Reporter | No crash loop when Datadog is down | Reporter never restarts after clean exit |
| `handle_continue` for route loading | No deadlock on startup | Routes not immediately available — callers must handle `:not_ready` |

Reflection question: the `TelemetrySupervisor` uses `max_restarts: 20, max_seconds: 60`.
If the `MetricsReporter` crashes every 2 seconds due to a Datadog API change (a permanent
error, not transient), what eventually happens? Is this the desired behaviour?
How would you distinguish transient errors (network blip) from permanent errors
(API key revoked) in the reporter's `init/1`?

---

## Common production mistakes

**1. A flat list of children for complex systems**
A single supervisor governing all workers with shared `max_restarts` means a noisy
metrics worker can hit the threshold and take down database connections. Partition
workers by failure domain. Each domain has its own supervisor with appropriate thresholds.

**2. Implicit dependencies through global names**
```elixir
def init(_) do
  pool = DBPool.checkout()  # works only if DBPool started before this process
  {:ok, %{conn: pool}}
end
```
If someone reorders the children list, this breaks silently at startup. Make dependencies
explicit: list them in order, and use `handle_continue` for any cross-process calls.

**3. Expensive external calls in `init/1`**
```elixir
def init(_) do
  {:ok, config} = RemoteConfigService.fetch()  # 2-second HTTP call
  {:ok, config}
end
```
If the remote service is down at deploy time, every restart attempt blocks the supervisor
for 2 seconds. With `max_restarts: 3, max_seconds: 5`, the supervisor gives up in 6
seconds. Use `handle_continue` so `init/1` always returns immediately, and retry fetch
logic in `handle_continue`.

**4. Relying on shutdown order for cleanup that depends on other processes**
Children terminate in reverse startup order. If `Router` terminates first and calls
`DBPool` in its `terminate/2`, DBPool may still be alive — but if it was listed before
Router it is actually terminated last, so it is still up. However, if you ever reorder
the list, the cleanup breaks. Keep `terminate/2` self-contained.

---

## Resources

- [OTP Design Principles — Supervisor Behaviour](https://www.erlang.org/doc/design_principles/sup_princ.html)
- [Designing Elixir Systems with OTP — James Edward Gray II & Bruce Tate](https://pragprog.com/titles/jgotp/)
- [HexDocs — Supervisor](https://hexdocs.pm/elixir/Supervisor.html)
- [HexDocs — DynamicSupervisor](https://hexdocs.pm/elixir/DynamicSupervisor.html)
- [Elixir in Action, 3rd ed. — Saša Jurić](https://www.manning.com/books/elixir-in-action-third-edition) — ch. 8, fault tolerance
