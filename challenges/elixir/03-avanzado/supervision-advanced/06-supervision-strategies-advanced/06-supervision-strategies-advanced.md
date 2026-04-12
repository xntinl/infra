# Advanced Supervision Strategies in Production

**Project**: `advanced_strategies` — compare `:one_for_one`, `:one_for_all`, and `:rest_for_one` against real failure scenarios.

**Difficulty**: ★★★☆☆
**Estimated time**: 3–5 hours

---

## Project context

You work on the backend of a trading platform. A single OTP node hosts three cooperating subsystems: a `MarketDataFeed` that pulls ticks from an upstream exchange, a `PricingEngine` that computes fair prices from those ticks, and an `OrderRouter` that submits orders using those prices. These three live inside the same application because the pricing engine must consume ticks at sub-millisecond latency — putting them behind a network boundary was measured to add ~400 µs per tick.

The engine was born with `:one_for_one` everywhere because "it's the default in every tutorial". After a year in production, the team discovered two classes of incidents: (1) when the `MarketDataFeed` died for 20 s, the `PricingEngine` kept serving stale prices to `OrderRouter`, producing trades at prices that no longer reflected the market; (2) when the `PricingEngine` was restarted because of a bug, its in-memory book was empty and `OrderRouter` rejected every request for 1.5 s, but `MarketDataFeed` kept buffering ticks that were discarded on arrival.

This exercise is about picking the **correct strategy per failure domain**, not about picking one strategy globally. You will see that a realistic tree mixes `:one_for_one`, `:rest_for_one`, and `:one_for_all` at different levels, each tuned with its own `max_restarts / max_seconds` budget.

```
advanced_strategies/
├── lib/
│   └── advanced_strategies/
│       ├── application.ex
│       ├── market_data/
│       │   ├── supervisor.ex          # :rest_for_one
│       │   ├── feed.ex                # producer
│       │   ├── pricing_engine.ex      # consumer
│       │   └── order_router.ex        # downstream
│       ├── telemetry/
│       │   ├── supervisor.ex          # :one_for_one
│       │   ├── metrics_reporter.ex
│       │   └── log_shipper.ex
│       └── session/
│           ├── supervisor.ex          # :one_for_all
│           ├── auth_token.ex
│           └── rpc_channel.ex
└── test/
    └── advanced_strategies/
        ├── strategies_test.exs
        └── restart_budget_test.exs
```

---

## Core concepts

### 1. Strategies map to dependency topology

The three strategies are not style preferences — each describes a different kind of dependency between children:

```
:one_for_one   →   A ⟂ B ⟂ C          (independent peers)
:one_for_all   →   A ↔ B ↔ C          (mutual shared state)
:rest_for_one  →   A → B → C          (linear pipeline)
```

For market data: `Feed → PricingEngine → OrderRouter` is a linear pipeline. If the feed dies, the engine's book is stale the moment the feed comes back with a gap; you must restart the engine too. But the router can keep running — unless you also invalidate it. In practice for this subsystem, `:rest_for_one` in the order `[Feed, PricingEngine, OrderRouter]` encodes exactly the dataflow.

For session: `AuthToken` and `RpcChannel` share the same TLS session key. If either dies, the key is gone; the other is useless. `:one_for_all`.

For telemetry: `MetricsReporter` and `LogShipper` are unrelated. `:one_for_one`.

### 2. `max_restarts / max_seconds` is a token bucket

The restart budget is not "3 restarts ever". It is a sliding window:

```
t=0.0s   crash → restart #1 recorded
t=1.2s   crash → restart #2 recorded
t=2.3s   crash → restart #3 recorded
t=2.4s   crash → 4 restarts in 2.4 s > budget (3 in 5 s) → supervisor exits
```

Defaults are `max_restarts: 3, max_seconds: 5`. For a pipeline that legitimately may lose its upstream for seconds (market data feed during an exchange hiccup), this is too tight; set `max_restarts: 10, max_seconds: 30`. For a root supervisor, keep it tight — if the root can't stabilize fast, exit so the OS supervisor (systemd, Kubernetes) can intervene.

### 3. Restart types: `:permanent`, `:temporary`, `:transient`

Strategy decides *who else* restarts when a child dies. Restart type decides *whether* a specific child is restarted at all:

| Restart type | Normal exit (`:normal`, `:shutdown`) | Abnormal exit |
|---|---|---|
| `:permanent` (default) | restart | restart |
| `:temporary` | do not restart | do not restart |
| `:transient` | do not restart | restart |

`OrderRouter` has replayable state on restart — `:permanent` is correct. `AuthToken` completing with `:shutdown` after a deliberate logout should NOT restart — `:transient`. A short-lived `Task` that either succeeds or fails once — `:temporary`.

### 4. `shutdown:` decides how cleanly the child dies

```elixir
%{
  id: PricingEngine,
  start: {PricingEngine, :start_link, []},
  shutdown: 5_000,          # ms to drain before brutal_kill
  restart: :permanent,
  type: :worker
}
```

`:brutal_kill` issues `Process.exit(pid, :kill)` immediately — no `terminate/2` callback runs. Reserve it for stateless tasks. For anything with downstream effects (open sockets, file writes, in-flight requests), use a positive timeout so `terminate/2` can flush.

### 5. Failure propagation inverts the tree

A supervisor that exhausts its restart budget dies with reason `:shutdown`. Its parent then treats that like any other child death — applying the parent's strategy. This is how "escalation" happens in OTP: failure domains absorb small crashes locally; only persistent, unrecoverable faults travel upward.

```
                  Application Supervisor (:one_for_one, 3/5)
                 /              |               \
         MarketData        Telemetry          Session
      (:rest_for_one,    (:one_for_one,    (:one_for_all,
          10/30)              3/60)            3/5)
         / | \                / \               / \
      Feed Eng Router    Reporter Shipper   Auth  Rpc
```

If `Feed` flaps 11 times in 30 s, `MarketData` supervisor exits. `Application` supervisor sees a child died → restarts MarketData. If THAT happens 3 times in 5 s, the whole app is dead.

---

## Implementation

### Step 1: Application and root supervisor

```elixir
# lib/advanced_strategies/application.ex
defmodule AdvancedStrategies.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      AdvancedStrategies.MarketData.Supervisor,
      AdvancedStrategies.Telemetry.Supervisor,
      AdvancedStrategies.Session.Supervisor
    ]

    Supervisor.start_link(children,
      strategy: :one_for_one,
      max_restarts: 3,
      max_seconds: 5,
      name: AdvancedStrategies.Supervisor
    )
  end
end
```

### Step 2: Market data subsystem — `:rest_for_one`

```elixir
# lib/advanced_strategies/market_data/supervisor.ex
defmodule AdvancedStrategies.MarketData.Supervisor do
  @moduledoc """
  Linear pipeline: Feed → PricingEngine → OrderRouter.

  If Feed dies we MUST restart the engine (its book references a snapshot
  tied to the feed session). If the engine dies, the router's cached prices
  are stale — restart it too. If only the router dies, leave the rest alone.
  """
  use Supervisor

  def start_link(_opts), do: Supervisor.start_link(__MODULE__, :ok, name: __MODULE__)

  @impl true
  def init(:ok) do
    children = [
      AdvancedStrategies.MarketData.Feed,
      AdvancedStrategies.MarketData.PricingEngine,
      AdvancedStrategies.MarketData.OrderRouter
    ]

    Supervisor.init(children,
      strategy: :rest_for_one,
      max_restarts: 10,
      max_seconds: 30
    )
  end
end
```

```elixir
# lib/advanced_strategies/market_data/feed.ex
defmodule AdvancedStrategies.MarketData.Feed do
  @moduledoc "Pulls ticks from upstream. Fails if the upstream URL returns 5xx."
  use GenServer

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec latest_tick() :: {:ok, float()} | {:error, :no_data}
  def latest_tick, do: GenServer.call(__MODULE__, :latest_tick)

  @spec crash() :: :ok
  def crash, do: GenServer.cast(__MODULE__, :crash)

  @impl true
  def init(_opts), do: {:ok, %{tick: nil}}

  @impl true
  def handle_call(:latest_tick, _from, %{tick: nil} = s), do: {:reply, {:error, :no_data}, s}
  def handle_call(:latest_tick, _from, %{tick: t} = s), do: {:reply, {:ok, t}, s}

  @impl true
  def handle_cast(:crash, _state), do: raise("simulated upstream 5xx")
end
```

```elixir
# lib/advanced_strategies/market_data/pricing_engine.ex
defmodule AdvancedStrategies.MarketData.PricingEngine do
  use GenServer

  def start_link(_), do: GenServer.start_link(__MODULE__, :ok, name: __MODULE__)

  @spec fair_price() :: {:ok, float()} | {:error, term()}
  def fair_price, do: GenServer.call(__MODULE__, :fair_price)

  @impl true
  def init(:ok), do: {:ok, %{last: nil, started_at: System.monotonic_time(:millisecond)}}

  @impl true
  def handle_call(:fair_price, _from, state) do
    case AdvancedStrategies.MarketData.Feed.latest_tick() do
      {:ok, tick} -> {:reply, {:ok, tick * 1.0001}, %{state | last: tick}}
      {:error, _} = err -> {:reply, err, state}
    end
  end
end
```

```elixir
# lib/advanced_strategies/market_data/order_router.ex
defmodule AdvancedStrategies.MarketData.OrderRouter do
  use GenServer

  def start_link(_), do: GenServer.start_link(__MODULE__, :ok, name: __MODULE__)

  @spec submit(String.t(), pos_integer()) :: {:ok, String.t()} | {:error, term()}
  def submit(symbol, qty), do: GenServer.call(__MODULE__, {:submit, symbol, qty})

  @impl true
  def init(:ok), do: {:ok, %{id_seq: 0}}

  @impl true
  def handle_call({:submit, symbol, qty}, _from, state) do
    case AdvancedStrategies.MarketData.PricingEngine.fair_price() do
      {:ok, price} ->
        id = "ord-#{state.id_seq}-#{symbol}-#{qty}-#{price}"
        {:reply, {:ok, id}, %{state | id_seq: state.id_seq + 1}}

      {:error, reason} ->
        {:reply, {:error, reason}, state}
    end
  end
end
```

### Step 3: Session subsystem — `:one_for_all`

```elixir
# lib/advanced_strategies/session/supervisor.ex
defmodule AdvancedStrategies.Session.Supervisor do
  @moduledoc """
  AuthToken and RpcChannel share a TLS session. Losing either invalidates both.
  """
  use Supervisor

  def start_link(_), do: Supervisor.start_link(__MODULE__, :ok, name: __MODULE__)

  @impl true
  def init(:ok) do
    Supervisor.init(
      [
        AdvancedStrategies.Session.AuthToken,
        AdvancedStrategies.Session.RpcChannel
      ],
      strategy: :one_for_all,
      max_restarts: 3,
      max_seconds: 5
    )
  end
end

# lib/advanced_strategies/session/auth_token.ex
defmodule AdvancedStrategies.Session.AuthToken do
  use GenServer
  def start_link(_), do: GenServer.start_link(__MODULE__, :ok, name: __MODULE__)
  def token, do: GenServer.call(__MODULE__, :token)
  def invalidate, do: GenServer.cast(__MODULE__, :invalidate)

  @impl true
  def init(:ok), do: {:ok, %{token: "tok-" <> Integer.to_string(System.unique_integer([:positive]))}}
  @impl true
  def handle_call(:token, _from, state), do: {:reply, state.token, state}
  @impl true
  def handle_cast(:invalidate, _state), do: {:stop, :invalidated, %{}}
end

# lib/advanced_strategies/session/rpc_channel.ex
defmodule AdvancedStrategies.Session.RpcChannel do
  use GenServer
  def start_link(_), do: GenServer.start_link(__MODULE__, :ok, name: __MODULE__)
  def call_remote(op), do: GenServer.call(__MODULE__, {:call_remote, op})

  @impl true
  def init(:ok), do: {:ok, %{channel_id: System.unique_integer([:positive])}}

  @impl true
  def handle_call({:call_remote, op}, _from, state) do
    {:reply, {:ok, "ch#{state.channel_id}:#{op}"}, state}
  end
end
```

### Step 4: Telemetry subsystem — `:one_for_one`

```elixir
# lib/advanced_strategies/telemetry/supervisor.ex
defmodule AdvancedStrategies.Telemetry.Supervisor do
  use Supervisor
  def start_link(_), do: Supervisor.start_link(__MODULE__, :ok, name: __MODULE__)

  @impl true
  def init(:ok) do
    Supervisor.init(
      [
        AdvancedStrategies.Telemetry.MetricsReporter,
        AdvancedStrategies.Telemetry.LogShipper
      ],
      strategy: :one_for_one,
      max_restarts: 3,
      max_seconds: 60
    )
  end
end

# lib/advanced_strategies/telemetry/metrics_reporter.ex
defmodule AdvancedStrategies.Telemetry.MetricsReporter do
  use GenServer
  def start_link(_), do: GenServer.start_link(__MODULE__, :ok, name: __MODULE__)
  def ping, do: GenServer.call(__MODULE__, :ping)
  @impl true
  def init(:ok), do: {:ok, %{}}
  @impl true
  def handle_call(:ping, _from, s), do: {:reply, :pong, s}
end

# lib/advanced_strategies/telemetry/log_shipper.ex
defmodule AdvancedStrategies.Telemetry.LogShipper do
  use GenServer
  def start_link(_), do: GenServer.start_link(__MODULE__, :ok, name: __MODULE__)
  def ping, do: GenServer.call(__MODULE__, :ping)
  @impl true
  def init(:ok), do: {:ok, %{}}
  @impl true
  def handle_call(:ping, _from, s), do: {:reply, :pong, s}
end
```

### Step 5: Tests

```elixir
# test/advanced_strategies/strategies_test.exs
defmodule AdvancedStrategies.StrategiesTest do
  use ExUnit.Case, async: false

  alias AdvancedStrategies.MarketData
  alias AdvancedStrategies.Telemetry
  alias AdvancedStrategies.Session

  describe ":rest_for_one in market data pipeline" do
    test "Feed crash restarts Engine and Router but not the supervisor" do
      pid_feed_before = Process.whereis(MarketData.Feed)
      pid_engine_before = Process.whereis(MarketData.PricingEngine)
      pid_router_before = Process.whereis(MarketData.OrderRouter)
      ref = Process.monitor(pid_feed_before)

      MarketData.Feed.crash()
      assert_receive {:DOWN, ^ref, :process, ^pid_feed_before, _reason}, 500

      wait_until(fn -> Process.whereis(MarketData.Feed) != nil end)
      wait_until(fn -> Process.whereis(MarketData.PricingEngine) != pid_engine_before end)
      wait_until(fn -> Process.whereis(MarketData.OrderRouter) != pid_router_before end)
    end

    test "Router crash does NOT restart Feed or Engine" do
      pid_feed = Process.whereis(MarketData.Feed)
      pid_engine = Process.whereis(MarketData.PricingEngine)
      pid_router = Process.whereis(MarketData.OrderRouter)

      ref = Process.monitor(pid_router)
      Process.exit(pid_router, :kill)
      assert_receive {:DOWN, ^ref, :process, ^pid_router, _}, 500

      wait_until(fn ->
        Process.whereis(MarketData.OrderRouter) not in [nil, pid_router]
      end)

      assert Process.whereis(MarketData.Feed) == pid_feed
      assert Process.whereis(MarketData.PricingEngine) == pid_engine
    end
  end

  describe ":one_for_all in session subsystem" do
    test "AuthToken crash restarts RpcChannel too" do
      pid_auth = Process.whereis(Session.AuthToken)
      pid_rpc = Process.whereis(Session.RpcChannel)

      ref = Process.monitor(pid_auth)
      Process.exit(pid_auth, :kill)
      assert_receive {:DOWN, ^ref, :process, ^pid_auth, _}, 500

      wait_until(fn ->
        new_auth = Process.whereis(Session.AuthToken)
        new_rpc = Process.whereis(Session.RpcChannel)
        new_auth not in [nil, pid_auth] and new_rpc not in [nil, pid_rpc]
      end)
    end
  end

  describe ":one_for_one in telemetry" do
    test "MetricsReporter crash does not affect LogShipper" do
      pid_shipper = Process.whereis(Telemetry.LogShipper)
      pid_reporter = Process.whereis(Telemetry.MetricsReporter)

      ref = Process.monitor(pid_reporter)
      Process.exit(pid_reporter, :kill)
      assert_receive {:DOWN, ^ref, :process, ^pid_reporter, _}, 500

      wait_until(fn ->
        Process.whereis(Telemetry.MetricsReporter) not in [nil, pid_reporter]
      end)

      assert Process.whereis(Telemetry.LogShipper) == pid_shipper
    end
  end

  defp wait_until(fun, timeout \\ 1_000, interval \\ 10) do
    deadline = System.monotonic_time(:millisecond) + timeout

    Stream.repeatedly(fn -> fun.() end)
    |> Enum.find(fn
      true -> true
      _ -> if System.monotonic_time(:millisecond) > deadline, do: raise("timeout"), else: (Process.sleep(interval); false)
    end)
  end
end
```

```elixir
# test/advanced_strategies/restart_budget_test.exs
defmodule AdvancedStrategies.RestartBudgetTest do
  use ExUnit.Case, async: false

  test ":one_for_one supervisor dies when its children flap beyond budget" do
    {:ok, sup} =
      Supervisor.start_link(
        [
          %{
            id: :flappy,
            start: {Agent, :start_link, [fn -> raise "boom" end]},
            restart: :permanent
          }
        ],
        strategy: :one_for_one,
        max_restarts: 2,
        max_seconds: 5
      )

    ref = Process.monitor(sup)
    assert_receive {:DOWN, ^ref, :process, ^sup, :shutdown}, 2_000
  end
end
```

---

## Trade-offs and production gotchas

**1. `:rest_for_one` order matters — a lot.** The order you declare children IS the dataflow. Declaring `[Router, Engine, Feed]` with `:rest_for_one` means a `Router` crash restarts `Engine` and `Feed` — the opposite of what you want. Newcomers break this every six months in a large codebase; add an integration test that asserts child order.

**2. `:one_for_all` amplifies transient faults.** A flap in any child takes down all siblings. Use only when state is genuinely shared. If you reach for it because "it's safer" you are paying availability to buy a guarantee you do not actually need.

**3. Restart budget for leaf supervisors vs root supervisors.** Leaves can afford generous budgets (`10/30`) because the cost of escalation is high (kills the whole app). The root should stay tight (`3/5`) so it dies fast and lets the OS supervisor take over — that is where you get a clean restart of the VM.

**4. `terminate/2` is not guaranteed to run.** Only on `:shutdown`, normal exit, or when the supervisor cleanly stops the child. A `Process.exit(pid, :kill)`, VM crash, or `:brutal_kill` skips it. Never rely on it for critical invariants; use external systems (DB transactions, durable queues).

**5. Naming children with `name:` creates a race on restart.** Between `Process.exit/2` and the new process being registered there is a window where `Process.whereis/1` returns `nil`. Calls through the registered name will fail with `:noproc`. Guard with `:gen_server.call/3` retry or use `via` tuples with a registry that tracks intent, not liveness.

**6. `:one_for_all` can deadlock on hibernated children.** If all children take >5 s to terminate and `max_seconds: 5`, the supervisor itself gets killed before cleanly restarting. Either raise `shutdown` on the children or loosen the budget.

**7. Supervisor code is hot-code-upgraded as a *child spec list*.** If you reorder, rename, or change the strategy in the same version as your children, `:sys.change_code/4` sees a structural delta and may restart the whole subtree during the upgrade. Separate topology changes into their own release.

**8. When NOT to use this.** If all your processes are homogeneous, stateless workers scaled by load — do not hand-roll three strategy levels. Use `PartitionSupervisor` (exercise 07) or `DynamicSupervisor` (exercise 08). Custom strategy trees shine when you have heterogeneous components with distinct failure semantics.

---

## Performance notes

Strategy choice has no measurable steady-state cost — supervisors are idle when children are alive. It dominates only during incidents. Measure: wrap the crash test with `:timer.tc/1` and observe that `:rest_for_one` restarting 3 children takes <5 ms on modern hardware; `:one_for_all` the same. What actually costs money in production is the combined `init/1` time of the restarted children — if `init/1` does 200 ms of I/O, a `:one_for_all` restart is 600 ms of unavailability.

---

## Resources

- [Supervisor — hexdocs](https://hexdocs.pm/elixir/Supervisor.html) — canonical reference for strategies and child specs.
- [OTP Design Principles: Supervisor Behaviour](https://www.erlang.org/doc/design_principles/sup_princ.html) — the original formulation; strategies existed in Erlang before Elixir.
- [The Zen of Erlang — Fred Hébert](https://ferd.ca/the-zen-of-erlang.html) — essay on failure domains and why "let it crash" is about *where*, not just *whether*.
- [Designing for scalability with Erlang/OTP — Cesarini & Vinoski](https://www.oreilly.com/library/view/designing-for-scalability/9781449361556/) — chapters 8–9 cover supervision patterns at scale.
- [Phoenix.Endpoint supervisor tree](https://github.com/phoenixframework/phoenix/blob/main/lib/phoenix/endpoint/supervisor.ex) — real-world `:one_for_one` with dozens of children.
- [Broadway supervision layout](https://github.com/dashbitco/broadway/blob/main/lib/broadway/topology.ex) — mixed `:rest_for_one` + `:one_for_one` pipeline.
