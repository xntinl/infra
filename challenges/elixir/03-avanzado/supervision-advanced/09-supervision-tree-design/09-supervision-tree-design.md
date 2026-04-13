# Supervision tree design: isolation boundaries and dependency graphs

**Project**: `tree_design` — design a supervision tree from first principles using failure domains.

---

## Project context

You are rescuing an inherited Phoenix-less OTP application. `application.ex` has 27 children in a flat `:one_for_one` list. When the Redis client crashes, the Kafka consumer is restarted too (because `:one_for_one` budget hits the root). When someone deploys a change that makes the DB migrator crash on startup, the HTTP listener never starts, blocking readiness probes even though the app could serve cached traffic.

The core problem is **no explicit failure domains**. Every process is treated as equally important, so any flap affects everything. You need to redesign the tree with three principles:

1. **Isolation boundaries** — group children by blast radius. A crash in telemetry must never reach payments.
2. **Dependency order** — within a boundary, children start in dataflow order and `:rest_for_one` encodes it.
3. **Start order respects hard dependencies** — the DB pool starts before anything that queries it; the cache starts before the warmer.

This exercise walks you through modeling an e-commerce backend: Infrastructure (DB, cache, message bus) → Domain (inventory, pricing, orders) → Edge (HTTP API, background jobs) → Observability (metrics, tracing). You end up with a three-level tree whose top-level supervisor uses `:rest_for_one`.

```
tree_design/
├── lib/
│   └── tree_design/
│       ├── application.ex
│       ├── infra/
│       │   ├── supervisor.ex
│       │   ├── db_pool.ex
│       │   ├── cache.ex
│       │   └── message_bus.ex
│       ├── domain/
│       │   ├── supervisor.ex
│       │   ├── inventory.ex
│       │   ├── pricing.ex
│       │   └── orders.ex
│       ├── edge/
│       │   ├── supervisor.ex
│       │   ├── http_api.ex
│       │   └── job_runner.ex
│       └── observability/
│           ├── supervisor.ex
│           └── metrics.ex
└── test/
    └── tree_design/
        └── tree_topology_test.exs
```

---

## Core concepts

### 1. Failure domains from dependency graphs

Draw your system as a directed graph of "uses":

```
observability  ←──────┐
                       │
edge      →  domain  →  infra
(http)      (orders)   (db, cache, bus)
```

Observability is read-only: it SUBSCRIBES to events. It has no children depending on it. It should be in its own supervisor that can die freely without affecting anything else.

Infra has no dependencies (it IS the bottom). It should start first and die last. Everything above depends on it — if infra dies, everything above must restart too.

Domain uses infra. If infra restarts (losing DB connections), domain processes with cached schema metadata must also restart. `:rest_for_one`.

Edge uses domain. Same logic.

This gives the shape:

```
                 Application (:rest_for_one, 5/30)
                 │
        ┌────────┼────────┬──────────────┐
        │        │        │              │
      Infra   Domain    Edge       Observability
    (:o4o)  (:rfo)   (:o4o)        (:o4o)
```

Root is `:rest_for_one` in the order `[Infra, Domain, Edge, Observability]`. If Infra restarts, Domain and Edge restart, but Observability is unaffected (comes later and only observes via events, not calls).

Actually — reread that. `:rest_for_one` restarts children *after* the failed one. So Observability (last) would restart if Domain dies. We want the opposite. Solution: put Observability FIRST in the list so it starts first, dies only when explicitly killed, and is not affected by later restarts.

```
Supervisor.init(
  [Observability, Infra, Domain, Edge],
  strategy: :rest_for_one
)
```

### 2. Three-level tree pattern

Each subsystem has its own supervisor. Leaves are workers; middle nodes are supervisors. Rule: **a supervisor contains either workers OR supervisors, never both**. Mixing them obscures the failure semantics and makes `:rest_for_one` ordering confusing.

### 3. Start order is child-list order

Supervisor starts children in list order and terminates in reverse. This is your most important lever. Whatever appears first must be fully started before the second starts.

```elixir
[Infra.Supervisor,  Domain.Supervisor,  Edge.Supervisor]
   ↑ starts first    ↑ can query DB     ↑ can call domain
```

During shutdown: Edge drains first, then Domain, then Infra last — so outbound requests can still query the DB while draining.

### 4. `:one_for_one` at leaves

Within `Infra`, DB, cache, and message bus are independent — each can flap without affecting the others. `:one_for_one`. If ALL three repeatedly fail, the budget expires and `Infra.Supervisor` dies, which triggers `:rest_for_one` at the root.

### 5. Identifying boundaries: the "same tier" test

Two children belong in the SAME supervisor if:
- they have the same failure radius (either both affect users or neither does)
- they share restart policy (`:permanent` vs `:transient`)
- their startup order can be ignored OR is naturally encoded by list order

They belong in DIFFERENT supervisors if:
- one is a hard dependency of the other (and you want explicit `:rest_for_one`)
- they have different `max_restarts` budgets (telemetry: generous; payments: strict)
- one owns shared state (ETS table) the other reads from

---

## Why layered supervisors and not a flat `:one_for_one`

A flat list treats every child as interchangeable. It cannot express "DB crash must cascade to consumers" (needs `:rest_for_one`) and it cannot express "telemetry flap must not reach payments" (needs isolation). Layering gives you both axes: inter-subsystem dataflow at the root (`:rest_for_one`), intra-subsystem peer independence at the leaves (`:one_for_one`). Flat trees scale in line count but not in expressiveness; at ~10+ children the "27-child flat list" anti-pattern becomes a correctness problem, not a style problem.

---

## Design decisions

**Option A — single root supervisor with all 27 workers flat**
- Pros: no indirection; `Supervisor.which_children/1` returns everything at once.
- Cons: impossible to encode inter-subsystem dependencies; restart budget shared across unrelated concerns; any flap pressures the whole root.

**Option B — three-level tree (root → subsystem → workers)** (chosen)
- Pros: failure domain boundaries are first-class; `:rest_for_one` at root encodes dataflow; subsystems can have independent budgets; trivial to add a new subsystem without touching existing ones.
- Cons: more files; more cognitive load; `:rest_for_one` ordering pitfalls (Observability-last trap described above).

→ Chose **B** because each of the four subsystems has distinct failure semantics and must be reasoned about independently. Flat supervision is the right default for < 5 children; above that it stops scaling organizationally.

---

## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  []
end
```

### Dependencies (mix.exs)

```elixir
```elixir
[Infra.Supervisor,  Domain.Supervisor,  Edge.Supervisor]
   ↑ starts first    ↑ can query DB     ↑ can call domain
```

During shutdown: Edge drains first, then Domain, then Infra last — so outbound requests can still query the DB while draining.

### 4. `:one_for_one` at leaves

Within `Infra`, DB, cache, and message bus are independent — each can flap without affecting the others. `:one_for_one`. If ALL three repeatedly fail, the budget expires and `Infra.Supervisor` dies, which triggers `:rest_for_one` at the root.

### 5. Identifying boundaries: the "same tier" test

Two children belong in the SAME supervisor if:
- they have the same failure radius (either both affect users or neither does)
- they share restart policy (`:permanent` vs `:transient`)
- their startup order can be ignored OR is naturally encoded by list order

They belong in DIFFERENT supervisors if:
- one is a hard dependency of the other (and you want explicit `:rest_for_one`)
- they have different `max_restarts` budgets (telemetry: generous; payments: strict)
- one owns shared state (ETS table) the other reads from

---

## Why layered supervisors and not a flat `:one_for_one`

A flat list treats every child as interchangeable. It cannot express "DB crash must cascade to consumers" (needs `:rest_for_one`) and it cannot express "telemetry flap must not reach payments" (needs isolation). Layering gives you both axes: inter-subsystem dataflow at the root (`:rest_for_one`), intra-subsystem peer independence at the leaves (`:one_for_one`). Flat trees scale in line count but not in expressiveness; at ~10+ children the "27-child flat list" anti-pattern becomes a correctness problem, not a style problem.

---

## Design decisions

**Option A — single root supervisor with all 27 workers flat**
- Pros: no indirection; `Supervisor.which_children/1` returns everything at once.
- Cons: impossible to encode inter-subsystem dependencies; restart budget shared across unrelated concerns; any flap pressures the whole root.

**Option B — three-level tree (root → subsystem → workers)** (chosen)
- Pros: failure domain boundaries are first-class; `:rest_for_one` at root encodes dataflow; subsystems can have independent budgets; trivial to add a new subsystem without touching existing ones.
- Cons: more files; more cognitive load; `:rest_for_one` ordering pitfalls (Observability-last trap described above).

→ Chose **B** because each of the four subsystems has distinct failure semantics and must be reasoned about independently. Flat supervision is the right default for < 5 children; above that it stops scaling organizationally.

---

## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  []
end
```


### Step 1: Root application

**Objective**: Order root children by dataflow (Observability, Infra, Domain, Edge) so `:rest_for_one` tears down the right downstream.

```elixir
# lib/tree_design/application.ex
defmodule TreeDesign.Application do
  use Application

  @impl true
  def start(_type, _args) do
    # Order is dataflow. Observability first (no deps); Infra next
    # (pure infrastructure); Domain (uses Infra); Edge (uses Domain).
    children = [
      TreeDesign.Observability.Supervisor,
      TreeDesign.Infra.Supervisor,
      TreeDesign.Domain.Supervisor,
      TreeDesign.Edge.Supervisor
    ]

    Supervisor.start_link(children,
      strategy: :rest_for_one,
      max_restarts: 5,
      max_seconds: 30,
      name: TreeDesign.Supervisor
    )
  end
end
```

### Step 2: Infra — `:one_for_one` of independent resources

**Objective**: Isolate DbPool, Cache, and MessageBus so a cache blip never drops database connections held by unrelated queries.

```elixir
# lib/tree_design/infra/supervisor.ex
defmodule TreeDesign.Infra.Supervisor do
  use Supervisor
  def start_link(_), do: Supervisor.start_link(__MODULE__, :ok, name: __MODULE__)

  @impl true
  def init(:ok) do
    Supervisor.init(
      [
        TreeDesign.Infra.DbPool,
        TreeDesign.Infra.Cache,
        TreeDesign.Infra.MessageBus
      ],
      strategy: :one_for_one,
      max_restarts: 10,
      max_seconds: 30
    )
  end
end

# lib/tree_design/infra/db_pool.ex
defmodule TreeDesign.Infra.DbPool do
  use GenServer
  def start_link(_), do: GenServer.start_link(__MODULE__, :ok, name: __MODULE__)
  def query(sql), do: GenServer.call(__MODULE__, {:query, sql})
  @impl true
  def init(:ok), do: {:ok, %{conns: 10}}
  @impl true
  def handle_call({:query, _sql}, _from, s), do: {:reply, {:ok, []}, s}
end

# lib/tree_design/infra/cache.ex
defmodule TreeDesign.Infra.Cache do
  use GenServer
  def start_link(_), do: GenServer.start_link(__MODULE__, :ok, name: __MODULE__)
  def get(key), do: GenServer.call(__MODULE__, {:get, key})
  def put(key, val), do: GenServer.call(__MODULE__, {:put, key, val})
  @impl true
  def init(:ok), do: {:ok, %{}}
  @impl true
  def handle_call({:get, k}, _from, s), do: {:reply, Map.get(s, k), s}
  def handle_call({:put, k, v}, _from, s), do: {:reply, :ok, Map.put(s, k, v)}
end

# lib/tree_design/infra/message_bus.ex
defmodule TreeDesign.Infra.MessageBus do
  use GenServer
  def start_link(_), do: GenServer.start_link(__MODULE__, :ok, name: __MODULE__)
  def publish(topic, msg), do: GenServer.cast(__MODULE__, {:publish, topic, msg})
  @impl true
  def init(:ok), do: {:ok, %{}}
  @impl true
  def handle_cast({:publish, _t, _m}, s), do: {:noreply, s}
end
```

### Step 3: Domain — `:rest_for_one` pipeline

**Objective**: Chain Inventory → Pricing → Orders with `:rest_for_one` so a stock recount invalidates cached prices before orders commit.

```elixir
# lib/tree_design/domain/supervisor.ex
defmodule TreeDesign.Domain.Supervisor do
  use Supervisor
  def start_link(_), do: Supervisor.start_link(__MODULE__, :ok, name: __MODULE__)

  @impl true
  def init(:ok) do
    # Inventory feeds Pricing; Pricing feeds Orders. Linear dataflow.
    Supervisor.init(
      [
        TreeDesign.Domain.Inventory,
        TreeDesign.Domain.Pricing,
        TreeDesign.Domain.Orders
      ],
      strategy: :rest_for_one,
      max_restarts: 5,
      max_seconds: 30
    )
  end
end

# lib/tree_design/domain/inventory.ex
defmodule TreeDesign.Domain.Inventory do
  use GenServer
  def start_link(_), do: GenServer.start_link(__MODULE__, :ok, name: __MODULE__)
  def stock(sku), do: GenServer.call(__MODULE__, {:stock, sku})
  @impl true
  def init(:ok), do: {:ok, %{"sku-a" => 10, "sku-b" => 3}}
  @impl true
  def handle_call({:stock, sku}, _from, s), do: {:reply, Map.get(s, sku, 0), s}
end

# lib/tree_design/domain/pricing.ex
defmodule TreeDesign.Domain.Pricing do
  use GenServer
  def start_link(_), do: GenServer.start_link(__MODULE__, :ok, name: __MODULE__)
  def price(sku), do: GenServer.call(__MODULE__, {:price, sku})
  @impl true
  def init(:ok), do: {:ok, %{"sku-a" => 199, "sku-b" => 499}}
  @impl true
  def handle_call({:price, sku}, _from, s), do: {:reply, Map.get(s, sku, 0), s}
end

# lib/tree_design/domain/orders.ex
defmodule TreeDesign.Domain.Orders do
  use GenServer
  def start_link(_), do: GenServer.start_link(__MODULE__, :ok, name: __MODULE__)
  def place(sku, qty), do: GenServer.call(__MODULE__, {:place, sku, qty})
  @impl true
  def init(:ok), do: {:ok, %{seq: 0}}
  @impl true
  def handle_call({:place, sku, qty}, _from, s) do
    {:reply, {:ok, "ord-#{s.seq}-#{sku}-#{qty}"}, %{s | seq: s.seq + 1}}
  end
end
```

### Step 4: Edge — `:one_for_one` (http and jobs are peers)

**Objective**: Keep HTTP and JobRunner as peers since each owns its own sockets — coupling their restarts would waste availability.

```elixir
# lib/tree_design/edge/supervisor.ex
defmodule TreeDesign.Edge.Supervisor do
  use Supervisor
  def start_link(_), do: Supervisor.start_link(__MODULE__, :ok, name: __MODULE__)

  @impl true
  def init(:ok) do
    Supervisor.init(
      [TreeDesign.Edge.HttpApi, TreeDesign.Edge.JobRunner],
      strategy: :one_for_one,
      max_restarts: 3,
      max_seconds: 10
    )
  end
end

defmodule TreeDesign.Edge.HttpApi do
  use GenServer
  def start_link(_), do: GenServer.start_link(__MODULE__, :ok, name: __MODULE__)
  @impl true
  def init(:ok), do: {:ok, %{}}
end

defmodule TreeDesign.Edge.JobRunner do
  use GenServer
  def start_link(_), do: GenServer.start_link(__MODULE__, :ok, name: __MODULE__)
  @impl true
  def init(:ok), do: {:ok, %{}}
end
```

### Step 5: Observability

**Objective**: Boot observability first so incident-era logs and metrics exist before the subsystems that will crash come alive.

```elixir
# lib/tree_design/observability/supervisor.ex
defmodule TreeDesign.Observability.Supervisor do
  use Supervisor
  def start_link(_), do: Supervisor.start_link(__MODULE__, :ok, name: __MODULE__)

  @impl true
  def init(:ok) do
    Supervisor.init(
      [TreeDesign.Observability.Metrics],
      strategy: :one_for_one,
      max_restarts: 10,
      max_seconds: 60
    )
  end
end

defmodule TreeDesign.Observability.Metrics do
  use GenServer
  def start_link(_), do: GenServer.start_link(__MODULE__, :ok, name: __MODULE__)
  @impl true
  def init(:ok), do: {:ok, %{}}
end
```

### Step 6: Topology tests — encode the design

**Objective**: Freeze child order and strategy as executable tests — newcomers reorder siblings every few months and break dataflow assumptions.

```elixir
# test/tree_design/tree_topology_test.exs
defmodule TreeDesign.TreeTopologyTest do
  use ExUnit.Case, async: false

  describe "TreeDesign.TreeTopology" do
    test "root children start in declared order" do
      children = Supervisor.which_children(TreeDesign.Supervisor)
      ids = Enum.map(children, fn {id, _, _, _} -> id end) |> Enum.reverse()

      assert ids == [
               TreeDesign.Observability.Supervisor,
               TreeDesign.Infra.Supervisor,
               TreeDesign.Domain.Supervisor,
               TreeDesign.Edge.Supervisor
             ]
    end

    test "infra crash restarts domain and edge (rest_for_one at root)" do
      pid_obs = Process.whereis(TreeDesign.Observability.Supervisor)
      pid_domain = Process.whereis(TreeDesign.Domain.Supervisor)
      pid_edge = Process.whereis(TreeDesign.Edge.Supervisor)
      pid_infra = Process.whereis(TreeDesign.Infra.Supervisor)

      ref = Process.monitor(pid_infra)
      Process.exit(pid_infra, :kill)
      assert_receive {:DOWN, ^ref, :process, ^pid_infra, _}, 500

      wait_until(fn ->
        pid_domain_new = Process.whereis(TreeDesign.Domain.Supervisor)
        pid_edge_new = Process.whereis(TreeDesign.Edge.Supervisor)

        is_pid(pid_domain_new) and pid_domain_new != pid_domain and
          is_pid(pid_edge_new) and pid_edge_new != pid_edge
      end)

      # Observability is BEFORE infra in the rest_for_one order → untouched.
      assert Process.whereis(TreeDesign.Observability.Supervisor) == pid_obs
    end

    test "leaf inventory crash does not affect orders' siblings above" do
      pid_inv = Process.whereis(TreeDesign.Domain.Inventory)
      pid_pricing = Process.whereis(TreeDesign.Domain.Pricing)
      pid_orders = Process.whereis(TreeDesign.Domain.Orders)

      ref = Process.monitor(pid_inv)
      Process.exit(pid_inv, :kill)
      assert_receive {:DOWN, ^ref, :process, ^pid_inv, _}, 500

      wait_until(fn ->
        pricing_new = Process.whereis(TreeDesign.Domain.Pricing)
        orders_new = Process.whereis(TreeDesign.Domain.Orders)
        # rest_for_one: inventory crash restarts pricing and orders
        pricing_new != pid_pricing and orders_new != pid_orders
      end)
    end
  end

  defp wait_until(fun, timeout \\ 1_000) do
    deadline = System.monotonic_time(:millisecond) + timeout

    Stream.repeatedly(fn -> fun.() end)
    |> Enum.find(fn
      true -> true
      _ ->
        if System.monotonic_time(:millisecond) > deadline,
          do: raise("wait_until timeout"),
          else: (Process.sleep(10); false)
    end)
  end
end
```

### Why this works

Placing `Observability.Supervisor` first in the root list keeps it untouched by any `:rest_for_one` cascade — passive observers never need to restart because of dataflow changes. `Infra` → `Domain` → `Edge` after it encodes the real dependency chain: a DB flap cascades forward through domain and edge, exactly where stale caches and open connections need reset. Each subsystem runs its own budget, so a flap in Edge cannot use up Infra's restart budget, and the root stays tight (5/30) so OS-level supervisors can intervene on whole-app instability.

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

**1. Flat trees are tempting but lie about your system.** A 27-child flat `:one_for_one` says "everything is equally important and independent". That is never true. A three-level tree is more code but encodes real failure semantics.

**2. `:rest_for_one` ordering traps.** If you put Observability last, a Domain crash restarts Observability — but Observability may be publishing metrics that Domain subscribers read, creating oscillation. Put passive observers FIRST or outside the subtree.

**3. Start order is NOT async-safe.** `Supervisor.init/1` returns before `init/1` of children completes. A child that is "started" may not yet be `ready`. If Domain.Pricing.init queries DbPool during init, the DbPool might still be connecting. Use `Application.ensure_all_started/1` + explicit readiness probes.

**4. Budget exhaustion cascades.** If a leaf flaps 10 times and its parent has `max_restarts: 3`, the parent dies. If that parent is a child of the root with `max_restarts: 5`, you've used 1 of 5. Five such cascades and the whole app dies. Monitor supervisor terminations via `:telemetry` or SASL logs.

**5. `Supervisor.which_children/1` is O(n) under mutex.** Do NOT call it from hot paths. For topology assertions (tests), it's fine. For observability, sample it every few seconds.

**6. Child specs with `name:` break topology on restart.** Between exit and re-registration, `Process.whereis/1` returns `nil`. Tests that grab a pid and then send it a message race with restart. Always re-lookup after a restart.

**7. Circular dependencies are invisible until deploy.** If Domain calls Edge (say, to notify of price changes) and Edge calls Domain, your `:rest_for_one` ordering becomes impossible. Break the cycle with a message bus (publish-subscribe) so neither directly depends on the other.

**8. When NOT to use this.** For a script, a single-purpose service with <5 processes, or an early-stage prototype, a flat `:one_for_one` is fine. The three-level tree pays for itself past ~10 processes with heterogeneous failure characteristics.

---

## Benchmark

Supervisor dispatch is ~1 µs per message. Deep trees (5+ levels) don't measurably slow startup. What does slow startup is `init/1` I/O — each `init/1` is serial within its supervisor. Do heavy work in `handle_continue/2` so `init/1` returns in microseconds and the supervisor proceeds to the next child.

Target: cold-boot end-to-end ≤ 200 ms for the full tree; any single `init/1` ≤ 5 ms; supervisor cascade restart ≤ 50 ms on Infra death.

---

## Reflection

1. Product adds a `Recommendations` subsystem that consumes Domain events and publishes to Edge. Where does it live — under Domain, under Edge, or as its own subsystem? Justify using the "same tier test" from Core concepts.
2. Your root runs at `max_restarts: 5, max_seconds: 30`. Ops reports the root exited twice in a week, each time after a cascading Infra → Domain → Edge restart triggered by a Redis blip. Do you widen the root budget, move Redis out of Infra, or add a circuit breaker in front of Redis? What's the minimum change that preserves cascade correctness?

---

## Resources

- [Designing Elixir Systems with OTP — James Edward Gray II & Bruce Tate](https://pragprog.com/titles/jgotp/designing-elixir-systems-with-otp/) — the definitive book on tree design.
- [Supervisor — hexdocs](https://hexdocs.pm/elixir/Supervisor.html) — strategies, start/shutdown order.
- [Fred Hébert — Stuff Goes Bad: Erlang in Anger](https://www.erlang-in-anger.com/) — free PDF, chapter on supervision trees in production.
- [Phoenix Application supervisor](https://github.com/phoenixframework/phoenix/blob/main/lib/phoenix/endpoint/supervisor.ex) — read a real three-level tree.
- [Dashbit blog — Your OTP app as an umbrella or not](https://dashbit.co/blog/are-umbrella-apps-dead-in-elixir) — José Valim on structural choices.
- [Saša Jurić — Elixir in Action, 2nd ed., Ch. 9](https://www.manning.com/books/elixir-in-action-second-edition) — worked examples of supervisor hierarchies.
