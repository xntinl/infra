# Gossip-Based Membership Protocol with Failure Detection

**Project**: `swimlane` — a SWIM-based membership protocol with probabilistic failure detection

---

## Project context

You are building `swimlane`, a gossip-based cluster membership protocol from scratch using UDP for communication. The protocol discovers, tracks, and maintains a consistent view of cluster membership across all nodes without a central coordinator. No Erlang's built-in node detection — every byte of the protocol is yours.

Project structure:

```
swimlane/
├── lib/
│   └── swimlane/
│       ├── application.ex           # starts node supervisor
│       ├── node.ex                  # GenServer: gossip rounds, membership view, timers
│       ├── failure_detector.ex      # SWIM probe/indirect-probe logic
│       ├── disseminator.ex          # gossip fanout: selects K random peers, sends deltas
│       ├── membership.ex            # membership list: alive/suspect/dead transitions
│       ├── incarnation.ex           # incarnation numbers: refutation mechanism
│       ├── transport.ex             # UDP send/receive, packet framing
│       └── simulation.ex            # 100-node in-process simulation (no UDP)
├── test/
│   └── swimlane/
│       ├── propagation_test.exs     # O(log N) convergence verification
│       ├── failure_detection_test.exs # true positive, false positive rate
│       ├── refutation_test.exs      # incarnation number-based refutation
│       ├── partition_test.exs       # split and merge with anti-entropy
│       └── simulation_test.exs      # 100-node simulation end-to-end
├── bench/
│   └── gossip_bench.exs
└── mix.exs
```

---

## The problem

A 100-node cluster must maintain a consistent view of which nodes are alive, suspect, and dead — without a central coordinator that can itself fail. Each node must independently detect failures and propagate membership changes. The key constraints are: propagation must reach all N nodes in O(log N) rounds, and the false positive rate (incorrectly marking a live node as dead) must remain low under message loss.

SWIM (Scalable Weakly-consistent Infection-style Membership) solves this by separating failure detection (direct and indirect probing) from dissemination (gossip). Both operate at configurable rates, allowing you to tune the false positive rate against detection latency independently.

---

## Why this design

**Gossip achieves O(log N) convergence by infection**: each node infects K random peers per round. After `ceil(log_K(N))` rounds, in expectation, every node has been infected. This is the same math as epidemic spreading in biology (SIR model). The math holds regardless of N — a 1000-node cluster converges in roughly the same number of rounds as a 100-node cluster, just with larger absolute K.

**Indirect probing reduces false positives**: when node A fails to get a response from node B within the probe timeout, A does not immediately declare B dead. Instead, A asks K other nodes to ping B indirectly. Only if all K indirect probers also fail does B become `suspect`. This handles the case where the A-B network path is congested but other paths to B are fine.

**Incarnation numbers enable refutation**: if a live node receives a rumor that it is `suspect` or `dead`, it can increment its incarnation number and broadcast an `alive` message with the new incarnation. Any node that sees a higher incarnation than the suspect rumor discards the suspect rumor. This is simpler and more efficient than vector clocks for this specific problem.

---

## Implementation milestones

### Step 1: Create the project

```bash
mix new swimlane --sup
cd swimlane
mkdir -p lib/swimlane test/swimlane bench
```

### Step 2: `mix.exs` — dependencies

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev},
    {:stream_data, "~> 0.6", only: :test}
  ]
end
```

### Step 3: Membership state machine

```elixir
# lib/swimlane/membership.ex
defmodule Swimlane.Membership do
  @moduledoc """
  Membership view for a single node. Each entry tracks:
    - state: :alive | :suspect | :dead
    - incarnation: integer — the node's own monotonic counter
    - last_updated_at: monotonic timestamp

  State transition rules:
    alive(inc N)  → suspect if probe fails
    suspect(inc N) → dead if suspicion timeout elapses
    suspect(inc N) → alive if a refutation with incarnation > N arrives
    dead  → removed from view after cleanup_timeout
  """

  defstruct [:node_id, :state, :incarnation, :address, :last_updated_at]

  @state_priority %{alive: 3, suspect: 2, dead: 1}

  @doc "Merges an incoming membership update into the local view."
  @spec merge(map(), %__MODULE__{}) :: map()
  def merge(local_view, %__MODULE__{} = update) do
    case Map.get(local_view, update.node_id) do
      nil ->
        Map.put(local_view, update.node_id, update)

      existing ->
        winner = resolve_conflict(existing, update)
        if winner != existing do
          Map.put(local_view, update.node_id, %{winner | last_updated_at: System.monotonic_time(:millisecond)})
        else
          local_view
        end
    end
  end

  defp resolve_conflict(a, b) do
    cond do
      b.incarnation > a.incarnation -> b
      a.incarnation > b.incarnation -> a
      Map.get(@state_priority, b.state, 0) > Map.get(@state_priority, a.state, 0) -> b
      true -> a
    end
  end

  @doc "Returns nodes that should receive the next probe."
  @spec probe_candidates(map(), [term()]) :: [%__MODULE__{}]
  def probe_candidates(view, exclude \\ []) do
    view
    |> Map.values()
    |> Enum.filter(fn member ->
      member.state in [:alive, :suspect] and member.node_id not in exclude
    end)
  end
end
```

### Step 4: SWIM failure detection

```elixir
# lib/swimlane/failure_detector.ex
defmodule Swimlane.FailureDetector do
  @moduledoc """
  Implements the SWIM probe protocol:

  1. Pick a random node B from the membership list.
  2. Send a direct probe to B; wait probe_timeout_ms.
  3. If B responds: mark B alive, done.
  4. If no response: send indirect probe requests to K random nodes,
     asking them to ping B on your behalf.
  5. If any indirect prober succeeds: mark B alive, done.
  6. If all K indirect probes fail: mark B :suspect.
  7. After suspicion_timeout_ms with no refutation: mark B :dead.
  """

  @spec probe(term(), map(), keyword()) :: :alive | :suspect
  def probe(node_b, membership, opts \\ []) do
    probe_timeout = Keyword.get(opts, :probe_timeout_ms, 500)
    indirect_count = Keyword.get(opts, :k, 3)
    transport = Keyword.get(opts, :transport, Swimlane.Transport)

    case transport.ping(node_b, probe_timeout) do
      :ok ->
        :alive

      :timeout ->
        candidates =
          Swimlane.Membership.probe_candidates(membership, [node_b])
          |> Enum.take_random(indirect_count)

        indirect_results =
          candidates
          |> Enum.map(fn candidate ->
            Task.async(fn ->
              transport.indirect_ping(candidate.node_id, node_b, probe_timeout)
            end)
          end)
          |> Task.await_many(probe_timeout * 2)

        if Enum.any?(indirect_results, fn r -> r == :ok end) do
          :alive
        else
          :suspect
        end
    end
  end
end
```

### Step 5: Gossip disseminator

```elixir
# lib/swimlane/disseminator.ex
defmodule Swimlane.Disseminator do
  @moduledoc """
  Gossip fanout: on each round, select K random peers and send
  the most recent membership deltas.

  Delta selection: prioritize events with the fewest disseminations so far.
  Each event carries a dissemination count; events are dropped after
  ceil(log(N)) disseminations (they have likely reached all nodes).
  """

  @spec next_round(map(), term(), pos_integer()) :: {[term()], [map()]}
  def next_round(membership, self_id, k) do
    alive_nodes =
      membership
      |> Map.values()
      |> Enum.filter(fn m -> m.state in [:alive, :suspect] and m.node_id != self_id end)

    peers =
      alive_nodes
      |> Enum.take_random(min(k, length(alive_nodes)))
      |> Enum.map(& &1.node_id)

    max_disseminations = :math.log2(max(map_size(membership), 2)) |> ceil()

    events =
      membership
      |> Map.values()
      |> Enum.filter(fn m -> Map.get(m, :dissemination_count, 0) < max_disseminations end)
      |> Enum.sort_by(fn m -> Map.get(m, :dissemination_count, 0) end)

    {peers, events}
  end
end
```

### Step 6: Given tests — must pass without modification

```elixir
# test/swimlane/propagation_test.exs
defmodule Swimlane.PropagationTest do
  use ExUnit.Case, async: false

  test "O(log N) propagation in 100-node simulation" do
    sim = Swimlane.Simulation.start(node_count: 100, fanout: 3, round_interval_ms: 50)

    # Inject a single join event on node 1
    Swimlane.Simulation.inject_join(sim, :new_node_x)

    # Measure rounds until all 100 nodes see the event
    rounds_to_converge = Swimlane.Simulation.measure_convergence(sim, :new_node_x, timeout_ms: 5_000)

    # O(log2(100)) * 2 = 14 rounds max
    assert rounds_to_converge <= 14,
      "took #{rounds_to_converge} rounds, expected ≤14 for N=100, K=3"

    Swimlane.Simulation.stop(sim)
  end

  test "failure detection: dead node propagates to all survivors within 20 rounds" do
    sim = Swimlane.Simulation.start(node_count: 50, fanout: 3, round_interval_ms: 50)
    Process.sleep(500)  # let cluster stabilize

    victim = Swimlane.Simulation.random_node(sim)
    Swimlane.Simulation.kill_node(sim, victim)

    rounds_to_propagate = Swimlane.Simulation.measure_convergence(sim, {:dead, victim}, timeout_ms: 10_000)
    assert rounds_to_propagate <= 20

    Swimlane.Simulation.stop(sim)
  end
end
```

```elixir
# test/swimlane/refutation_test.exs
defmodule Swimlane.RefutationTest do
  use ExUnit.Case, async: false

  test "a live node refutes a suspect rumor with higher incarnation" do
    sim = Swimlane.Simulation.start(node_count: 10, fanout: 3, round_interval_ms: 50)
    Process.sleep(300)

    target = Swimlane.Simulation.random_node(sim)

    # Inject a suspect rumor directly into another node's view
    Swimlane.Simulation.inject_rumor(sim, :node_1, {:suspect, target, incarnation: 1})

    # Wait a few rounds for the target to detect and refute
    Process.sleep(500)

    # All nodes must see the target as alive
    views = Swimlane.Simulation.all_views(sim, target)
    assert Enum.all?(views, fn {_node, state} -> state == :alive end),
      "not all nodes believe #{target} is alive: #{inspect(views)}"

    Swimlane.Simulation.stop(sim)
  end
end
```

### Step 7: Run the tests

```bash
mix test test/swimlane/ --trace
```

### Step 8: Benchmark

```elixir
# bench/gossip_bench.exs
sim = Swimlane.Simulation.start(node_count: 100, fanout: 3, round_interval_ms: 0)

Benchee.run(
  %{
    "single gossip round — 100 nodes" => fn ->
      Swimlane.Simulation.run_round(sim)
    end,
    "membership merge — 100 events" => fn ->
      events = Swimlane.Simulation.random_events(sim, 100)
      Swimlane.Membership.merge_all(%{}, events)
    end
  },
  time: 10,
  warmup: 3,
  formatters: [Benchee.Formatters.Console]
)
```

---

## Trade-off analysis

| Aspect | SWIM (your impl) | `:net_kernel` / epmd | Paxos-based membership |
|--------|-----------------|---------------------|----------------------|
| Coordinator required | none | epmd daemon | leader node |
| Convergence | O(log N) rounds | event-driven | quorum-dependent |
| False positive rate | tunable via indirect probes | low (TCP-based) | none (consensus) |
| Network overhead | O(K) messages per round per node | O(N) on topology change | O(N) per decision |
| Partition behavior | eventual consistency | BEAM node isolation | blocks minority |
| Suitable scale | thousands of nodes | hundreds of nodes | dozens of nodes |

Reflection: SWIM gives you eventually consistent membership, not strongly consistent. What applications can tolerate a 2-round window where a node is incorrectly marked suspect before being refuted?

---

## Common production mistakes

**1. Marking a node dead after the first failed probe**
A single direct probe failure is not sufficient evidence of death. Congestion, GC pauses, and momentary packet loss cause false direct-probe failures. The indirect probe step exists precisely for this. Skipping it dramatically increases your false positive rate.

**2. Gossip fanout K too low**
With K=1, convergence is O(N) rounds in the worst case. The O(log N) bound requires K ≥ 2; K=3 is the practical minimum for a 100-node cluster. Derive K from the convergence formula before choosing a value.

**3. Embedding metrics collection in the gossip hot path**
Incrementing counters or writing to ETS on every gossip message adds latency to the round interval. Metrics must be sampled by a separate process, not instrumented inline.

**4. Not bounding the event buffer**
Gossip events accumulate indefinitely if not pruned. After `ceil(log(N))` disseminations, an event has reached all nodes with high probability. Drop it from the outbound buffer. An unbounded buffer causes memory growth proportional to cluster churn rate.

**5. Using wall-clock time for suspicion timeouts**
Use `System.monotonic_time/1` for all timeout calculations. NTP adjustments can cause wall-clock time to jump backward, extending or collapsing suspicion windows unexpectedly.

---

## Resources

- Das, A., Gupta, I. & Motivala, A. (2002). *SWIM: Scalable Weakly-Consistent Infection-Style Process Group Membership Protocol* — the primary source; implement the protocol exactly as described
- Van Renesse, R. et al. (1998). *A Gossip-Style Failure Detection Service* — the predecessor to SWIM; read it to understand what SWIM improves upon
- [Hashicorp memberlist](https://github.com/hashicorp/memberlist) — `memberlist.go`, `state.go`, `suspicion.go` — production Go implementation with extensive comments on protocol choices
- [Apache Cassandra gossip](https://github.com/apache/cassandra/tree/trunk/src/java/org/apache/cassandra/gms) — real-world adaptation for database cluster management
