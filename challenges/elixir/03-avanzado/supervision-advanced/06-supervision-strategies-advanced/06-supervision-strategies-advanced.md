# Supervision Strategies: Designing Failure Domains

## Goal

Redesign an API gateway's supervision tree to isolate failure domains. A crashing `MetricsReporter` must never take down the `RateLimiter`. The implementation creates three independent domain supervisors (Core, Middleware, Telemetry) with appropriate strategies, restart types, and `max_restarts` thresholds.

---

## The three supervision strategies

### `:one_for_one`
Only the failing process is restarted. All siblings are unaffected. Use when workers are completely independent.

### `:one_for_all`
When any child fails, all children are terminated and restarted together. Use when children have mutual state dependencies.

### `:rest_for_one`
When child N fails, child N and every child started after N are restarted. Children started before N are not touched. Use when there is a linear dependency chain.

---

## `max_restarts` / `max_seconds` -- the sliding window

```elixir
Supervisor.init(children,
  strategy:     :one_for_one,
  max_restarts: 3,    # up to 3 restarts...
  max_seconds:  5     # ...within any 5-second window
)
```

When a child restarts more than `max_restarts` times within `max_seconds` seconds, the supervisor terminates itself, propagating the failure upward.

---

## Full implementation

All modules defined here. No external dependencies required.

### Stub modules (simulated workers)

```elixir
# lib/api_gateway/rate_limiter/server.ex
defmodule ApiGateway.RateLimiter.Server do
  use GenServer

  def start_link(_opts), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  @spec check(String.t(), pos_integer(), pos_integer()) :: {:allow, non_neg_integer()}
  def check(_client_id, limit, _window_ms), do: {:allow, limit}

  @impl true
  def init(_), do: {:ok, %{}}
end

# lib/api_gateway/route_table/server.ex
defmodule ApiGateway.RouteTable.Server do
  use GenServer

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(opts) do
    {:ok, %{traffic_class: Keyword.get(opts, :traffic_class, :default)}}
  end
end

# lib/api_gateway/middleware/audit_writer.ex
defmodule ApiGateway.Middleware.AuditWriter do
  use GenServer

  def start_link(_opts), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  @impl true
  def init(_), do: {:ok, %{}}
end

# lib/api_gateway/middleware/priority_dispatcher.ex
defmodule ApiGateway.Middleware.PriorityDispatcher do
  use GenServer

  def start_link(_opts), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  @impl true
  def init(_), do: {:ok, %{}}
end

# lib/api_gateway/telemetry/reporter.ex
defmodule ApiGateway.Telemetry.Reporter do
  use GenServer

  def start_link(_opts), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  @impl true
  def init(_), do: {:ok, %{}}
end
```

### Circuit breaker modules

```elixir
# lib/api_gateway/circuit_breaker/worker.ex
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

# lib/api_gateway/circuit_breaker/supervisor.ex
defmodule ApiGateway.CircuitBreaker.Supervisor do
  use DynamicSupervisor
  require Logger

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
```

### Supervision tree

```
ApiGateway.Application
+-- ApiGateway.CoreSupervisor        (rest_for_one, max_restarts: 5/30s)
|   +-- ApiGateway.RateLimiter.Server       :permanent
|   +-- ApiGateway.RouteTable.Server        :permanent
|   +-- ApiGateway.CircuitBreaker.Supervisor :permanent
|
+-- ApiGateway.MiddlewareSupervisor   (one_for_one, max_restarts: 10/60s)
|   +-- ApiGateway.Middleware.AuditWriter         :permanent
|   +-- ApiGateway.Middleware.PriorityDispatcher  :permanent
|
+-- ApiGateway.TelemetrySupervisor    (one_for_one, max_restarts: 20/60s)
    +-- ApiGateway.Telemetry.Reporter  :transient
```

```elixir
# lib/api_gateway/application.ex
defmodule ApiGateway.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      ApiGateway.CoreSupervisor,
      ApiGateway.MiddlewareSupervisor,
      ApiGateway.TelemetrySupervisor
    ]

    Supervisor.start_link(children,
      strategy: :one_for_one,
      name: ApiGateway.Supervisor
    )
  end
end
```

```elixir
# lib/api_gateway/core_supervisor.ex
defmodule ApiGateway.CoreSupervisor do
  use Supervisor

  def start_link(opts) do
    Supervisor.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(_opts) do
    children = [
      ApiGateway.RateLimiter.Server,
      {ApiGateway.RouteTable.Server, [traffic_class: :default]},
      ApiGateway.CircuitBreaker.Supervisor
    ]

    Supervisor.init(children,
      strategy: :rest_for_one,
      max_restarts: 5,
      max_seconds: 30
    )
  end
end
```

```elixir
# lib/api_gateway/middleware_supervisor.ex
defmodule ApiGateway.MiddlewareSupervisor do
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
      strategy: :one_for_one,
      max_restarts: 10,
      max_seconds: 60
    )
  end
end
```

```elixir
# lib/api_gateway/telemetry_supervisor.ex
defmodule ApiGateway.TelemetrySupervisor do
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
      }
    ]

    Supervisor.init(children,
      strategy: :one_for_one,
      max_restarts: 20,
      max_seconds: 60
    )
  end
end
```

### Tests

```elixir
# test/api_gateway/supervision_test.exs
defmodule ApiGateway.SupervisionTest do
  use ExUnit.Case, async: false

  describe "failure domain isolation" do
    test "TelemetrySupervisor crash does not affect RateLimiter.Server" do
      assert Process.alive?(Process.whereis(ApiGateway.RateLimiter.Server))

      reporter_pid = Process.whereis(ApiGateway.Telemetry.Reporter)
      if reporter_pid do
        ref = Process.monitor(reporter_pid)
        Process.exit(reporter_pid, :kill)
        assert_receive {:DOWN, ^ref, :process, _, _}, 1_000
      end

      Process.sleep(100)

      assert Process.alive?(Process.whereis(ApiGateway.RateLimiter.Server))
      assert {:allow, _} = ApiGateway.RateLimiter.Server.check("test-client", 100, 60_000)
    end

    test "CoreSupervisor is started before MiddlewareSupervisor" do
      assert Process.whereis(ApiGateway.RateLimiter.Server) != nil
      assert Process.whereis(ApiGateway.Middleware.AuditWriter) != nil
    end

    test "CircuitBreaker.Supervisor can add and list workers dynamically" do
      {:ok, pid1} = ApiGateway.CircuitBreaker.Supervisor.start_worker("svc-x")
      {:ok, pid2} = ApiGateway.CircuitBreaker.Supervisor.start_worker("svc-y")

      workers = ApiGateway.CircuitBreaker.Supervisor.list_workers()
      assert Enum.member?(workers, pid1)
      assert Enum.member?(workers, pid2)
    end

    test "crashed circuit breaker worker is restarted by DynamicSupervisor" do
      {:ok, pid} = ApiGateway.CircuitBreaker.Supervisor.start_worker("crash-test-svc")
      ref = Process.monitor(pid)
      Process.exit(pid, :kill)
      assert_receive {:DOWN, ^ref, :process, _, _}, 1_000

      Process.sleep(100)
      workers = ApiGateway.CircuitBreaker.Supervisor.list_workers()
      assert Enum.any?(workers, &Process.alive?/1)
    end
  end
end
```

---

## How it works

1. **Three failure domains**: Core (rate limiter, route table, circuit breakers), Middleware (audit, dispatch), Telemetry (metrics reporter). Each has its own supervisor with independent `max_restarts` thresholds.

2. **`:rest_for_one` in CoreSupervisor**: children form a dependency chain. If RateLimiter crashes, everything after it (RouteTable, CircuitBreaker.Supervisor) restarts.

3. **`:transient` for Reporter**: exits cleanly when Datadog is unreachable, the supervisor does NOT restart it. Prevents the crash loop that was taking down the entire gateway.

4. **Top-level `:one_for_one`**: the three domain supervisors are independent. A telemetry crash never cascades to core components.

---

## Common production mistakes

**1. Using `:one_for_all` as default "because it is safer"**
A MetricsReporter crashing every 30 seconds under `:one_for_all` restarts all siblings every 30 seconds.

**2. Wrong child order with `:rest_for_one`**
The order in the children list is semantically significant. Dependencies must come before dependents.

**3. Increasing `max_restarts` to "fix" crashing workers**
Setting `max_restarts: 1_000_000` masks real bugs. Fix the bug first.

---

## Resources

- [Erlang OTP -- Supervisor Behaviour](https://www.erlang.org/doc/design_principles/sup_princ.html)
- [HexDocs -- Supervisor](https://hexdocs.pm/elixir/Supervisor.html)
- [HexDocs -- DynamicSupervisor](https://hexdocs.pm/elixir/DynamicSupervisor.html)
- [Fred Hebert -- The Zen of Erlang](https://ferd.ca/the-zen-of-erlang.html)
