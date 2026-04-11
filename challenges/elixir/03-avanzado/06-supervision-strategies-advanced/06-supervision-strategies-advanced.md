# Supervision Strategies: Designing Failure Domains

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`. The application now has several components running
together: the rate limiter, circuit breakers, route table, audit writer, and priority
dispatcher. They were all placed in a flat list under one supervisor during prototyping.

A `MetricsReporter` that calls an external Datadog agent has started crashing every
30 seconds when the agent is unreachable. Because it shares a supervisor with
`RateLimiter.Server`, the supervisor's `max_restarts` threshold is being hit and the
entire gateway is restarting — including the rate limiter that was working fine.

This exercise redesigns the supervision tree to contain failures.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex              # ← you redesign this
│       ├── router.ex
│       ├── rate_limiter/
│       │   └── server.ex
│       ├── circuit_breaker/
│       │   ├── supervisor.ex           # ← you implement this
│       │   └── worker.ex
│       ├── route_table/
│       │   └── server.ex
│       ├── middleware/
│       │   ├── audit_writer.ex
│       │   └── priority_dispatcher.ex
│       └── telemetry/
│           └── reporter.ex             # already exists — crashes when agent is down
├── test/
│   └── api_gateway/
│       └── supervision_test.exs        # given tests — must pass without modification
└── mix.exs
```

---

## The three supervision strategies

### `:one_for_one`

Only the failing process is restarted. All siblings are unaffected.

Use when workers are **completely independent** — the failure of one does not
invalidate the state of the others.

Trap: if workers share implicit state (e.g., an ETS table that worker A writes and
worker B reads), `:one_for_one` can leave worker B with stale data after A restarts.

### `:one_for_all`

When any child fails, **all children** are terminated and restarted together.

Use when children have **mutual state dependencies** — if one falls, the rest
are in an inconsistent state anyway.

Trap: a noisy worker that crashes frequently will drag all siblings with it
on every crash. The cost of a single failure is multiplied by N children.

### `:rest_for_one`

When child N fails, child N and every child started **after** N are restarted.
Children started before N are not touched.

Use when there is a **linear dependency chain**: B depends on A, C depends on B.
If A crashes, B and C are invalidated; but A's crash does not affect any prior siblings.

Trap: the **position** in the child list is semantically significant. Reordering
children silently changes failure semantics.

---

## `max_restarts` / `max_seconds` — the sliding window

```elixir
Supervisor.init(children,
  strategy:     :one_for_one,
  max_restarts: 3,    # up to 3 restarts...
  max_seconds:  5     # ...within any 5-second window
)
```

When a child restarts more than `max_restarts` times within `max_seconds` seconds,
the supervisor **gives up** and terminates itself, propagating the failure upward
in the tree.

This is intentional: a process that crashes in a tight loop masks a real bug.
The supervisor gives up to force visibility. Default values (`3/5`) are aggressive.
Production workers with legitimate transient spikes may need wider windows.

**The window is sliding, not fixed.** The supervisor keeps timestamps of the last
`max_restarts` restarts and checks whether the oldest falls within `max_seconds` of
the most recent.

---

## Restart types

```elixir
%{
  id:      MyWorker,
  start:   {MyWorker, :start_link, [[]]},
  restart: :permanent   # always restart (default)
  # restart: :temporary # never restart — fire-and-forget
  # restart: :transient # restart only on abnormal exit (not :normal or :shutdown)
}
```

- `:permanent` — critical workers that must always run
- `:temporary` — one-shot tasks; use with `Task.Supervisor`
- `:transient` — optional workers; restart only on crashes, not on clean shutdown

---

## Implementation

### Step 1: Redesign `application.ex`

The target tree separates components into three failure domains. The top-level
supervisor uses `:one_for_one` because the three domain supervisors are independent:
a telemetry crash should never affect the core domain.

```
ApiGateway.Application
├── ApiGateway.CoreSupervisor        (rest_for_one, max_restarts: 5/30s)
│   ├── ApiGateway.RateLimiter.Server       :permanent
│   ├── ApiGateway.RouteTable.Server        :permanent
│   └── ApiGateway.CircuitBreaker.Supervisor :permanent  (supervises workers)
│
├── ApiGateway.MiddlewareSupervisor   (one_for_one, max_restarts: 10/60s)
│   ├── ApiGateway.Middleware.AuditWriter         :permanent
│   └── ApiGateway.Middleware.PriorityDispatcher  :permanent
│
└── ApiGateway.TelemetrySupervisor    (one_for_one, max_restarts: 20/60s)
    └── ApiGateway.Telemetry.Reporter  :transient
```

```elixir
# lib/api_gateway/application.ex
defmodule ApiGateway.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      # Order matters: Core must be up before Middleware can serve requests.
      # TelemetrySupervisor is independent — can go last.
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

### Step 2: `lib/api_gateway/core_supervisor.ex`

The CoreSupervisor uses `:rest_for_one` because the children form a linear dependency
chain: RouteTable depends on RateLimiter (it checks rate limits before looking up routes),
and CircuitBreaker.Supervisor depends on RouteTable (it needs route information to know
which upstream each breaker protects).

If RateLimiter crashes and restarts, RouteTable and CircuitBreaker.Supervisor are also
restarted — they may hold stale handles or cached state that depends on the limiter's
ETS tables. If RouteTable crashes alone, only it and CircuitBreaker.Supervisor restart;
the RateLimiter (listed before RouteTable) is unaffected.

```elixir
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

### Step 3: `lib/api_gateway/circuit_breaker/supervisor.ex`

The CircuitBreaker.Supervisor is a DynamicSupervisor because workers are created at
runtime (one per upstream service discovered). Each worker is independent — if one
crashes, only that worker restarts. The `:one_for_one` strategy is the only option
for DynamicSupervisor.

```elixir
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

  @doc """
  Starts a circuit breaker worker for a service.
  Returns {:ok, pid} or {:error, reason}.
  """
  @spec start_worker(String.t()) :: {:ok, pid()} | {:error, term()}
  def start_worker(service_name) do
    spec = {ApiGateway.CircuitBreaker.Worker, service_name}
    DynamicSupervisor.start_child(__MODULE__, spec)
  end

  @doc "Lists all currently supervised circuit breaker workers."
  @spec list_workers() :: [pid()]
  def list_workers do
    DynamicSupervisor.which_children(__MODULE__)
    |> Enum.map(fn {_, pid, _, _} -> pid end)
    |> Enum.filter(&is_pid/1)
  end
end
```

### Step 4: `lib/api_gateway/middleware_supervisor.ex`

AuditWriter and PriorityDispatcher are independent — if one crashes, the other keeps
working. `:one_for_one` is correct. The `max_restarts` threshold is generous (10 in
60 seconds) because middleware components may crash transiently under load spikes.

```elixir
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

### Step 5: `lib/api_gateway/telemetry_supervisor.ex`

The Reporter uses `restart: :transient` — if it exits cleanly (e.g., the Datadog agent
is permanently unavailable and the Reporter gives up gracefully with `:normal`), the
supervisor does NOT restart it. This prevents the crash loop that was taking down the
entire gateway. If it crashes abnormally (unexpected error), it still restarts.

The generous `max_restarts: 20, max_seconds: 60` allows the Reporter to crash 20 times
per minute before the supervisor gives up — telemetry is legitimately noisy.

```elixir
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

### Step 6: Given tests — must pass without modification

```elixir
# test/api_gateway/supervision_test.exs
defmodule ApiGateway.SupervisionTest do
  use ExUnit.Case, async: false

  describe "failure domain isolation" do
    test "TelemetrySupervisor crash does not affect RateLimiter.Server" do
      # Verify core is running
      assert Process.alive?(Process.whereis(ApiGateway.RateLimiter.Server))

      # Kill the telemetry supervisor's Reporter
      reporter_pid = Process.whereis(ApiGateway.Telemetry.Reporter)
      if reporter_pid do
        ref = Process.monitor(reporter_pid)
        Process.exit(reporter_pid, :kill)
        assert_receive {:DOWN, ^ref, :process, _, _}, 1_000
      end

      Process.sleep(100)

      # RateLimiter must still be alive and functional
      assert Process.alive?(Process.whereis(ApiGateway.RateLimiter.Server))
      assert {:allow, _} = ApiGateway.RateLimiter.Server.check("test-client", 100, 60_000)
    end

    test "CoreSupervisor is started before MiddlewareSupervisor" do
      # Verify start ordering by checking that core components exist
      # before middleware components
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

      # DynamicSupervisor restarts it; check list has a live pid for the service
      Process.sleep(100)
      workers = ApiGateway.CircuitBreaker.Supervisor.list_workers()
      assert Enum.any?(workers, &Process.alive?/1)
    end
  end
end
```

### Step 7: Run the tests

```bash
mix test test/api_gateway/supervision_test.exs --trace
```

---

## Trade-off analysis

| Strategy | Best for | Key risk |
|----------|----------|----------|
| `:one_for_one` | Independent workers | Silent state corruption if workers share ETS |
| `:one_for_all` | Tightly coupled state | Noisy worker takes down all siblings |
| `:rest_for_one` | Linear dependency chain | Child list order is now load-bearing |

Reflection question: the `CoreSupervisor` uses `:rest_for_one`. If you later add a
fourth child between `RouteTable.Server` and `CircuitBreaker.Supervisor`, what are
the failure semantics of the new child? Why is this dangerous if the change is made
without reviewing the dependency graph?

---

## Common production mistakes

**1. Using `:one_for_all` as default "because it is safer"**
A `MetricsReporter` that crashes every 30 seconds under `:one_for_all` will restart
`DBPool`, `RateLimiter`, and every other sibling every 30 seconds. Your application
effectively restarts every 30 seconds. `:one_for_all` is the most expensive strategy —
only use it when you can demonstrate that worker state is mutually invalidated on failure.

**2. Wrong child order with `:rest_for_one`**
If `HTTPServer` is listed before `DBPool` in a `:rest_for_one` supervisor:
- `HTTPServer` crash → restarts `HTTPServer` AND `DBPool` (unnecessary)
- `DBPool` crash → restarts only `DBPool` (HTTPServer keeps stale connections)
The order is the opposite of what you need. Always list dependencies before dependents.

**3. Increasing `max_restarts` as a fix for crashing workers**
Setting `max_restarts: 1_000_000` prevents the supervisor from giving up but does nothing
about the underlying crash. The worker crashes a million times per second, fills logs, and
saturates the CPU with restart overhead. `max_restarts` is a circuit breaker for the
supervisor itself — raising it masks real bugs. Fix the bug first.

**4. Confusing `max_seconds` with a fixed-window reset timer**
`max_seconds` is a **sliding window** based on timestamps, not a fixed window that resets
every N seconds. With `max_restarts: 3, max_seconds: 5`: crashes at t=0, t=4, t=8 do NOT
exceed the threshold because at t=8 the crash at t=0 is outside the 5-second window.
Only crashes within the last 5 seconds of the most recent crash are counted.

---

## Resources

- [Erlang OTP — Supervisor Behaviour](https://www.erlang.org/doc/design_principles/sup_princ.html)
- [HexDocs — Elixir Supervisor](https://hexdocs.pm/elixir/Supervisor.html)
- [HexDocs — DynamicSupervisor](https://hexdocs.pm/elixir/DynamicSupervisor.html)
- [Fred Hébert — The Zen of Erlang](https://ferd.ca/the-zen-of-erlang.html)
- [Designing Elixir Systems with OTP — James Edward Gray II & Bruce Tate](https://pragprog.com/titles/jgotp/)
