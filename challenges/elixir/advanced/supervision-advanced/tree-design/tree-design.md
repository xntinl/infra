# Supervision tree design: isolation boundaries and dependency graphs

**Project**: `tree_design` — design a supervision tree from first principles using failure domains.

---

## The business problem

You are rescuing an inherited Phoenix-less OTP application. `application.ex` has 27 children in a flat `:one_for_one` list. When the Redis client crashes, the Kafka consumer is restarted too (because `:one_for_one` budget hits the root). When someone deploys a change that makes the DB migrator crash on startup, the HTTP listener never starts, blocking readiness probes even though the app could serve cached traffic.

The core problem is **no explicit failure domains**. Every process is treated as equally important, so any flap affects everything. You need to redesign the tree with three principles:

1. **Isolation boundaries** — group children by blast radius. A crash in telemetry must never reach payments.
2. **Dependency order** — within a boundary, children start in dataflow order and `:rest_for_one` encodes it.
3. **Start order respects hard dependencies** — the DB pool starts before anything that queries it; the cache starts before the warmer.

This exercise walks you through modeling an e-commerce backend: Infrastructure (DB, cache, message bus) → Domain (inventory, pricing, orders) → Edge (HTTP API, background jobs) → Observability (metrics, tracing). You end up with a three-level tree whose top-level supervisor uses `:rest_for_one`.

## Project structure

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
├── test/
│   └── tree_design/
│       └── tree_topology_test.exs
├── script/
│   └── main.exs
└── mix.exs
```

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

### `mix.exs`
```elixir
defmodule SupervisionTreeDesign.MixProject do
  use Mix.Project

  def project do
    [
      app: :supervision_tree_design,
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
[Infra.Supervisor,  Domain.Supervisor,  Edge.Supervisor]
   ↑ starts first    ↑ can query DB     ↑ can call domain
```

During shutdown: Edge drains first, then Domain, then Infra last — so outbound requests can still query the DB while draining.

Within `Infra`, DB, cache, and message bus are independent — each can flap without affecting the others. `:one_for_one`. If ALL three repeatedly fail, the budget expires and `Infra.Supervisor` dies, which triggers `:rest_for_one` at the root.

Two children belong in the SAME supervisor if:
- they have the same failure radius (either both affect users or neither does)
- they share restart policy (`:permanent` vs `:transient`)
- their startup order can be ignored OR is naturally encoded by list order

They belong in DIFFERENT supervisors if:
- one is a hard dependency of the other (and you want explicit `:rest_for_one`)
- they have different `max_restarts` budgets (telemetry: generous; payments: strict)
- one owns shared state (ETS table) the other reads from

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

### `script/main.exs`
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

defmodule Main do
  def main do
      # Demonstrate realistic multi-layer supervision tree design

      # Start the application supervisor
      {:ok, sup_pid} = TreeDesign.Application.start(:normal, [])

      assert is_pid(sup_pid), "Root supervisor must start"
      IO.inspect(sup_pid, label: "Root supervisor PID")

      # Verify the tree structure: root → Observability + Infrastructure
      obs_sup = Process.whereis(TreeDesign.Observability.Supervisor)
      infra_sup = Process.whereis(TreeDesign.Infrastructure.Supervisor)

      assert is_pid(obs_sup), "Observability.Supervisor must be running"
      assert is_pid(infra_sup), "Infrastructure.Supervisor must be running"

      IO.puts("✓ Root one_for_one supervisor started")
      IO.puts("✓ Observability subsystem (independent, tight budget) initialized")
      IO.puts("✓ Infrastructure subsystem (dependent stages) initialized")

      # Verify infrastructure's rest_for_one structure: Cache → Database → Edge
      cache_pid = Process.whereis(TreeDesign.Infrastructure.Cache)
      db_pid = Process.whereis(TreeDesign.Infrastructure.Database)
      edge_pid = Process.whereis(TreeDesign.Infrastructure.Edge)

      assert is_pid(cache_pid), "Cache must exist"
      assert is_pid(db_pid), "Database must exist"
      assert is_pid(edge_pid), "Edge must exist"

      IO.puts("✓ rest_for_one pipeline verified (Cache → Database → Edge)")

      # Verify domain's rest_for_one structure: Inventory → Pricing → Orders
      domain_sup = Process.whereis(TreeDesign.Domain.Supervisor)
      assert is_pid(domain_sup), "Domain.Supervisor must be running"

      inv_pid = Process.whereis(TreeDesign.Domain.Inventory)
      pricing_pid = Process.whereis(TreeDesign.Domain.Pricing)
      orders_pid = Process.whereis(TreeDesign.Domain.Orders)

      assert is_pid(inv_pid), "Inventory must exist"
      assert is_pid(pricing_pid), "Pricing must exist"
      assert is_pid(orders_pid), "Orders must exist"

      IO.puts("✓ Domain rest_for_one structure verified (Inventory → Pricing → Orders)")

      # Test independent leaf: Observability should survive domain failures
      obs_before = Process.whereis(TreeDesign.Observability.Supervisor)

      # Kill an infrastructure leaf (Cache)
      ref = Process.monitor(cache_pid)
      Process.exit(cache_pid, :kill)
      assert_receive {:DOWN, ^ref, :process, ^cache_pid, _}, 500

      Process.sleep(100)

      # Observability should still be the same PID (rest_for_one at root only affects its children)
      obs_after = Process.whereis(TreeDesign.Observability.Supervisor)
      assert obs_before == obs_after, "Observability should be unaffected by infrastructure crash"

      IO.puts("✓ Failure isolation: Observability unaffected by infrastructure crash")

      # Verify cache was restarted (rest_for_one restarts it)
      cache_new = Process.whereis(TreeDesign.Infrastructure.Cache)
      assert is_pid(cache_new), "Cache should be restarted"
      assert cache_new != cache_pid, "Cache PID should be new after restart"

      IO.puts("✓ rest_for_one restart verified: Cache restarted after crash")

      IO.puts("\n✓ Multi-layer supervision tree structure demonstrated:")
      IO.puts("  - Root (one_for_one): contains independent subsystems")
      IO.puts("  - Observability (one_for_one): independent telemetry workers")
      IO.puts("  - Infrastructure (rest_for_one): stateful pipeline (Cache → DB → Edge)")
      IO.puts("  - Domain (rest_for_one): business logic pipeline")
      IO.puts("✓ Failure domain isolation working correctly")

      Supervisor.stop(sup_pid)
      IO.puts("✓ Supervision tree shutdown complete")
  end
end

Main.main()
```

---

## Why Supervision tree design matters

Mastering **Supervision tree design** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `lib/tree_design.ex`

```elixir
defmodule TreeDesign do
  @moduledoc """
  Reference implementation for Supervision tree design: isolation boundaries and dependency graphs.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the tree_design module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> TreeDesign.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```

### `test/tree_design_test.exs`

```elixir
defmodule TreeDesignTest do
  use ExUnit.Case, async: true

  doctest TreeDesign

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert TreeDesign.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts

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
