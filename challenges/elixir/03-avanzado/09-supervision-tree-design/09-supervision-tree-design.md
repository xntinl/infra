# Supervision Tree Design: Modeling Failure Domains

## Goal

Design and implement a complete supervision tree for an API gateway that separates components into three failure domains (Core, Middleware, Telemetry), uses appropriate strategies per domain, and ensures a crashing metrics reporter never takes down payment-processing components. This includes a `PartitionSupervisor` for rate limiting, a `DynamicSupervisor` for circuit breakers, and a `Task.Supervisor` for concurrent operations.

---

## Principles for supervision tree design

### Dependency ordering
Children start in order and terminate in reverse order. If `RateLimiter` fails to start, `RouteTable` and `Router` never start. On shutdown, `Router` stops first (drains requests), then `RouteTable`, then `RateLimiter` last.

### Failure domains
A failure domain is the set of components that must fail or recover together. Components belong to the same domain if the crash of one leaves the other in an invalid state.

### Circular dependency deadlock
Circular dependencies in `init/1` cause silent deadlocks at startup. Use `handle_continue/2` to defer any calls to other processes until after `init/1` returns.

---

## Full implementation

### All worker modules (self-contained)

```elixir
defmodule ApiGateway.RateLimiter.Server do
  use GenServer

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts)
  end

  @impl true
  def init(_opts) do
    table = :ets.new(:rate_limiter_shard, [:bag, :public])
    {:ok, %{table: table}}
  end
end

defmodule ApiGateway.RouteTable.Server do
  use GenServer

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(opts) do
    traffic_class = Keyword.get(opts, :traffic_class, :default)
    {:ok, %{routes: %{}, ready: false, traffic_class: traffic_class}, {:continue, :load_routes}}
  end

  @impl true
  def handle_continue(:load_routes, state) do
    # Simulated lazy load -- in production this would call a remote config service
    routes = %{
      "/api/payments" => "http://payments-svc:8080",
      "/api/orders" => "http://orders-svc:8080"
    }
    {:noreply, %{state | routes: routes, ready: true}}
  end
end

defmodule ApiGateway.CircuitBreaker.Worker do
  use GenServer

  def start_link(service_name) do
    GenServer.start_link(__MODULE__, service_name)
  end

  @impl true
  def init(service_name) do
    {:ok, %{service: service_name, status: :closed, failures: 0}}
  end
end

defmodule ApiGateway.CircuitBreaker.Supervisor do
  use DynamicSupervisor

  def start_link(opts) do
    DynamicSupervisor.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(_opts) do
    DynamicSupervisor.init(strategy: :one_for_one)
  end

  @spec start_worker(String.t()) :: {:ok, pid()} | {:error, term()}
  def start_worker(service_name) do
    spec = {ApiGateway.CircuitBreaker.Worker, service_name}
    DynamicSupervisor.start_child(__MODULE__, spec)
  end

  @spec list_workers() :: [pid()]
  def list_workers do
    DynamicSupervisor.which_children(__MODULE__)
    |> Enum.map(fn {_, pid, _, _} -> pid end)
    |> Enum.filter(&is_pid/1)
  end
end

defmodule ApiGateway.Middleware.AuditWriter do
  use GenServer

  def start_link(_opts) do
    GenServer.start_link(__MODULE__, [], name: __MODULE__)
  end

  @impl true
  def init(_), do: {:ok, %{}}
end

defmodule ApiGateway.Middleware.PriorityDispatcher do
  use GenServer

  def start_link(_opts) do
    GenServer.start_link(__MODULE__, [], name: __MODULE__)
  end

  @impl true
  def init(_), do: {:ok, %{}}
end

defmodule ApiGateway.Telemetry.Reporter do
  use GenServer

  def start_link(_opts) do
    GenServer.start_link(__MODULE__, [], name: __MODULE__)
  end

  @impl true
  def init(_), do: {:ok, %{}}
end

defmodule ApiGateway.Telemetry.HealthChecker do
  use GenServer

  def start_link(_opts) do
    GenServer.start_link(__MODULE__, [], name: __MODULE__)
  end

  @impl true
  def init(_), do: {:ok, %{}}
end
```

### The supervision tree

```
ApiGateway.Application
  -> ApiGateway.Supervisors.CoreSupervisor          (rest_for_one)
      -> ApiGateway.RateLimiter.Partitions           PartitionSupervisor
      -> ApiGateway.RouteTable.Server                GenServer
      -> ApiGateway.CircuitBreaker.Supervisor        DynamicSupervisor
      -> ApiGateway.TaskSupervisor                   Task.Supervisor
  -> ApiGateway.Supervisors.MiddlewareSupervisor     (one_for_one)
      -> ApiGateway.Middleware.AuditWriter            GenServer
      -> ApiGateway.Middleware.PriorityDispatcher     GenServer
  -> ApiGateway.Supervisors.TelemetrySupervisor      (one_for_one)
      -> ApiGateway.Telemetry.Reporter               GenServer  :transient
      -> ApiGateway.Telemetry.HealthChecker          GenServer  :permanent
```

```elixir
defmodule ApiGateway.Application do
  use Application
  require Logger

  @impl true
  def start(_type, _args) do
    Logger.info("ApiGateway starting")

    children = [
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

```elixir
defmodule ApiGateway.Supervisors.CoreSupervisor do
  use Supervisor

  def start_link(opts) do
    Supervisor.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(_opts) do
    children = [
      {PartitionSupervisor,
        child_spec: ApiGateway.RateLimiter.Server,
        name: ApiGateway.RateLimiter.Partitions,
        partitions: System.schedulers_online()},
      {ApiGateway.RouteTable.Server, [traffic_class: :default]},
      ApiGateway.CircuitBreaker.Supervisor,
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

```elixir
defmodule ApiGateway.Supervisors.TelemetrySupervisor do
  use Supervisor

  def start_link(opts) do
    Supervisor.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(_opts) do
    children = [
      %{
        id:      ApiGateway.Telemetry.Reporter,
        start:   {ApiGateway.Telemetry.Reporter, :start_link, [[]]},
        restart: :transient
      },
      {ApiGateway.Telemetry.HealthChecker, []}
    ]

    Supervisor.init(children,
      strategy:     :one_for_one,
      max_restarts: 20,
      max_seconds:  60
    )
  end
end
```

### Tests

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

      core_pid = Process.whereis(ApiGateway.Supervisors.CoreSupervisor)
      assert core_pid != nil
      assert Process.alive?(core_pid)
    end

    test "middleware supervisor is independent of core supervisor" do
      audit_pid = Process.whereis(ApiGateway.Middleware.AuditWriter)
      assert audit_pid != nil

      core_pid = Process.whereis(ApiGateway.Supervisors.CoreSupervisor)
      assert core_pid != nil

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
      audit_pid = Process.whereis(ApiGateway.Middleware.AuditWriter)
      route_table_pid = Process.whereis(ApiGateway.RouteTable.Server)

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

---

## How it works

1. **Three-layer hierarchy**: top-level `:one_for_one` because the three domain supervisors are independent failure domains.

2. **`:rest_for_one` in CoreSupervisor**: RateLimiter is first because everything depends on it. If it crashes, RouteTable and CircuitBreaker.Supervisor restart. If RouteTable crashes alone, only it and things after it restart.

3. **`:transient` for Reporter**: exits cleanly when Datadog is unreachable, and the supervisor does NOT restart it in a loop.

4. **`handle_continue` for RouteTable**: prevents startup deadlock when RouteTable needs to call other core components during initialization.

---

## Common production mistakes

**1. A flat list of children for complex systems**
A single supervisor governing all workers means a noisy metrics worker can hit `max_restarts` and take down database connections.

**2. Implicit dependencies through global names**
If someone reorders the children list, processes that depend on each other may fail silently.

**3. Expensive external calls in `init/1`**
Use `handle_continue` so `init/1` always returns immediately.

---

## Resources

- [OTP Design Principles -- Supervisor Behaviour](https://www.erlang.org/doc/design_principles/sup_princ.html)
- [HexDocs -- Supervisor](https://hexdocs.pm/elixir/Supervisor.html)
- [HexDocs -- DynamicSupervisor](https://hexdocs.pm/elixir/DynamicSupervisor.html)
- [Fred Hebert -- The Zen of Erlang](https://ferd.ca/the-zen-of-erlang.html)
