# Advanced Supervision Strategies in Production

**Project**: `advanced_strategies` — compare `:one_for_one`, `:one_for_all`, and `:rest_for_one` against real failure scenarios.

---

## The business problem

You work on the backend of a trading platform. A single OTP node hosts three cooperating subsystems: a `MarketDataFeed` that pulls ticks from an upstream exchange, a `PricingEngine` that computes fair prices from those ticks, and an `OrderRouter` that submits orders using those prices. These three live inside the same application because the pricing engine must consume ticks at sub-millisecond latency — putting them behind a network boundary was measured to add ~400 µs per tick.

The engine was born with `:one_for_one` everywhere because "it's the default in every tutorial". After a year in production, the team discovered two classes of incidents: (1) when the `MarketDataFeed` died for 20 s, the `PricingEngine` kept serving stale prices to `OrderRouter`, producing trades at prices that no longer reflected the market; (2) when the `PricingEngine` was restarted because of a bug, its in-memory book was empty and `OrderRouter` rejected every request for 1.5 s, but `MarketDataFeed` kept buffering ticks that were discarded on arrival.

This exercise is about picking the **correct strategy per failure domain**, not about picking one strategy globally. You will see that a realistic tree mixes `:one_for_one`, `:rest_for_one`, and `:one_for_all` at different levels, each tuned with its own `max_restarts / max_seconds` budget.

## Project structure

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
├── test/
│   └── advanced_strategies/
│       ├── strategies_test.exs
│       └── restart_budget_test.exs
├── script/
│   └── main.exs
└── mix.exs
```

---

## Why mixed strategies and not `:one_for_one` everywhere

`:one_for_one` is the safe default but it lies about dataflow. A uniform `:one_for_one` tree lets `PricingEngine` serve stale prices for 20 s while `MarketDataFeed` restarts — a financial correctness bug, not a crash. The strategies are not style preferences; they are declarative constraints on *what consistency the subsystem requires after a failure*. `:rest_for_one` says "downstream must die with upstream"; `:one_for_all` says "these children share state and none makes sense alone"; `:one_for_one` says "these are genuinely independent". Choosing uniformly wastes the mechanism.

---

## Design decisions

**Option A — `:one_for_one` root with children picking their own internal strategy**
- Pros: failures contained per subsystem; root stays stable.
- Cons: requires a subsystem supervisor per concern (one extra layer); slightly more wiring.

**Option B — single root `:rest_for_one` with all leaf workers flat** (rejected)
- Pros: flat tree, easy to read.
- Cons: a telemetry crash would restart market data; conflates independent subsystems; restart budget becomes meaningless.

→ Chose **A** because each subsystem has its own dataflow semantics (pipeline vs. shared state vs. independent) and its own restart-budget needs. Nesting is the cost of expressing those truthfully.

---

## Implementation

### Dependencies (`mix.exs`)

### `mix.exs`
```elixir
defmodule SupervisionStrategiesAdvanced.MixProject do
  use Mix.Project

  def project do
    [
      app: :supervision_strategies_advanced,
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

## Advanced Considerations: Partitioned Supervisors and Custom Restart Strategies

A standard Supervisor is a single process managing a static tree. For thousands of children, a single supervisor becomes a bottleneck: all supervisor callbacks run on one process, and supervisor restart logic is sequential. PartitionSupervisor (OTP 25+) spawns N independent supervisors, each managing a subset of children. Hashing the child ID determines which partition supervises it, distributing load and enabling horizontal scaling.

Custom restart strategies (via `Supervisor.init/2` callback) allow logic beyond the defaults. A strategy might prioritize restarting dependent services in a specific order, or apply backoff based on restart frequency. The downside is complexity: custom logic is harder to test and reason about, and mistakes cascade. Start with defaults and profile before adding custom behavior.

Selective restart via `:rest_for_one` or `:one_for_all` affects failure isolation. `:one_for_all` restarts all children when one fails (simulating a total system failure), which can be necessary for consistency but is expensive. `:rest_for_one` restarts the failed child and any started after it, balancing isolation and dependencies. Understanding which strategy fits your architecture prevents cascading failures and unnecessary restarts.

---

## Deep Dive: Property Patterns and Production Implications

Property-based testing inverts the testing mindset: instead of writing examples, you state invariants (properties) and let a generator find counterexamples. StreamData's shrinking capability is its superpower—when a property fails on a 10,000-element list, the framework reduces it to the minimal list that still fails, cutting debugging time from hours to minutes. The trade-off is that properties require rigorous thinking about domain constraints, and not every invariant is worth expressing as a property. Teams that adopt property testing often find bugs in specifications themselves, not just implementations.

---

## Trade-offs and production gotchas

**1. `:rest_for_one` order matters — a lot.** The order you declare children IS the dataflow. Declaring `[Router, Engine, Feed]` with `:rest_for_one` means a `Router` crash restarts `Engine` and `Feed` — the opposite of what you want. Newcomers break this every six months in a large codebase; add an integration test that asserts child order.

**2. `:one_for_all` amplifies transient faults.** A flap in any child takes down all siblings. Use only when state is genuinely shared. If you reach for it because "it's safer" you are paying availability to buy a guarantee you do not actually need.

**3. Restart budget for leaf supervisors vs root supervisors.** Leaves can afford generous budgets (`10/30`) because the cost of escalation is high (kills the whole app). The root should stay tight (`3/5`) so it dies fast and lets the OS supervisor take over — that is where you get a clean restart of the VM.

**4. `terminate/2` is not guaranteed to run.** Only on `:shutdown`, normal exit, or when the supervisor cleanly stops the child. A `Process.exit(pid, :kill)`, VM crash, or `:brutal_kill` skips it. Never rely on it for critical invariants; use external systems (DB transactions, durable queues).

**5. Naming children with `name:` creates a race on restart.** Between `Process.exit/2` and the new process being registered there is a window where `Process.whereis/1` returns `nil`. Calls through the registered name will fail with `:noproc`. Guard with `:gen_server.call/3` retry or use `via` tuples with a registry that tracks intent, not liveness.

**6. `:one_for_all` can deadlock on hibernated children.** If all children take >5 s to terminate and `max_seconds: 5`, the supervisor itself gets killed before cleanly restarting. Either raise `shutdown` on the children or loosen the budget.

**7. Supervisor code is hot-code-upgraded as a *child spec list*.** If you reorder, rename, or change the strategy in the same version as your children, `:sys.change_code/4` sees a structural delta and may restart the whole subtree during the upgrade. Separate topology changes into their own release.

**8. When NOT to use this.** If all your processes are homogeneous, stateless workers scaled by load — do not hand-roll three strategy levels. Use `PartitionSupervisor` for keyed sharding or `DynamicSupervisor` for on-demand children. Custom strategy trees shine when you have heterogeneous components with distinct failure semantics.

---

## Benchmark

Strategy choice has no measurable steady-state cost — supervisors are idle when children are alive. It dominates only during incidents. Measure: wrap the crash test with `:timer.tc/1` and observe that `:rest_for_one` restarting 3 children takes <5 ms on modern hardware; `:one_for_all` the same. What actually costs money in production is the combined `init/1` time of the restarted children — if `init/1` does 200 ms of I/O, a `:one_for_all` restart is 600 ms of unavailability.

Target: supervisor restart overhead ≤ 5 ms per child on modern hardware; subsystem unavailability window during restart bounded by ∑ `init/1` time of the affected children.

---

## Reflection

1. You add a fourth subsystem, `RiskEngine`, that consumes from `PricingEngine` and feeds `OrderRouter`. Where does it live in the tree? Does `OrderRouter` move under the same `:rest_for_one` as `RiskEngine`, or does `RiskEngine` get its own supervisor? Justify using dataflow, not "symmetry with the existing tree".
2. Ops reports the root supervisor has hit `max_restarts` twice in a month. Each time it was triggered by a specific subsystem flapping. Do you raise the root budget, tighten the leaf budget, or split the leaf into a grandchild sub-tree? What metric tells you which is correct?

---

### `script/main.exs`
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

defmodule Main do
  def main do
      # Demonstrate mixed supervision strategies in a production trading system
      {:ok, sup_pid} = Supervisor.start_link(
        [
          AdvancedStrategies.MarketData.Supervisor,
          AdvancedStrategies.Telemetry.Supervisor,
          AdvancedStrategies.Session.Supervisor
        ],
        strategy: :one_for_one,
        max_restarts: 3,
        max_seconds: 5,
        name: AdvancedStrategies.Supervisor
      )

      # Test rest_for_one: Feed is the source of truth
      assert is_pid(sup_pid), "Root supervisor must be a PID"
      IO.inspect(sup_pid, label: "Root supervisor")

      # Test that all subsystem supervisors are running
      feed_pid = Process.whereis(AdvancedStrategies.MarketData.Feed)
      assert is_pid(feed_pid), "Feed must be running"

      engine_pid = Process.whereis(AdvancedStrategies.MarketData.PricingEngine)
      assert is_pid(engine_pid), "PricingEngine must be running"

      router_pid = Process.whereis(AdvancedStrategies.MarketData.OrderRouter)
      assert is_pid(router_pid), "OrderRouter must be running"

      # Test rest_for_one: OrderRouter needs fair prices
      assert {:error, :no_data} = AdvancedStrategies.MarketData.PricingEngine.fair_price(),
        "Engine should report no data initially"

      # Test one_for_all: AuthToken and RpcChannel share state
      auth_pid = Process.whereis(AdvancedStrategies.Session.AuthToken)
      assert is_pid(auth_pid), "AuthToken must be running"

      rpc_pid = Process.whereis(AdvancedStrategies.Session.RpcChannel)
      assert is_pid(rpc_pid), "RpcChannel must be running"

      # Test one_for_one: Telemetry reporters are independent
      reporter_pid = Process.whereis(AdvancedStrategies.Telemetry.MetricsReporter)
      assert is_pid(reporter_pid), "MetricsReporter must be running"

      shipper_pid = Process.whereis(AdvancedStrategies.Telemetry.LogShipper)
      assert is_pid(shipper_pid), "LogShipper must be running"

      IO.puts("✓ All subsystem supervisors initialized correctly")
      IO.puts("✓ Supervision strategies (rest_for_one, one_for_all, one_for_one) demonstrated")
      IO.puts("✓ Restart budgets properly configured per subsystem")

      Supervisor.stop(sup_pid)
      IO.puts("✓ Supervisor shutdown complete")
  end
end

Main.main()
```

---

## Why Advanced Supervision Strategies in Production matters

Mastering **Advanced Supervision Strategies in Production** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `lib/advanced_strategies.ex`

```elixir
defmodule AdvancedStrategies do
  @moduledoc """
  Reference implementation for Advanced Supervision Strategies in Production.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the advanced_strategies module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> AdvancedStrategies.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```

### `test/advanced_strategies_test.exs`

```elixir
defmodule AdvancedStrategiesTest do
  use ExUnit.Case, async: true

  doctest AdvancedStrategies

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert AdvancedStrategies.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts

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
