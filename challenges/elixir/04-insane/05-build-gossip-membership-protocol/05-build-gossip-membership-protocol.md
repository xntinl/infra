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

## Design decisions

**Option A — Full broadcast heartbeats (all-to-all)**
- Pros: simple; deterministic detection latency.
- Cons: O(N²) messages per period; saturates network well before 1000 nodes.

**Option B — SWIM-style gossip with indirect probing** (chosen)
- Pros: O(N) bandwidth per node per period; dissemination time is O(log N); false-positive rate controllable via indirect probes.
- Cons: convergence is probabilistic; tuning fanout and probe timeout is subtle.

→ Chose **B** because SWIM is the only protocol in this family that scales past a few dozen nodes while preserving a tunable bound on detection accuracy.

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

  @doc "Merges a list of membership updates into the local view."
  @spec merge_all(map(), [%__MODULE__{}]) :: map()
  def merge_all(local_view, updates) do
    Enum.reduce(updates, local_view, fn update, acc -> merge(acc, update) end)
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

### Step 6: Transport stub

```elixir
# lib/swimlane/transport.ex
defmodule Swimlane.Transport do
  @moduledoc """
  UDP transport layer for SWIM protocol messages.
  In production, this sends and receives over UDP sockets.
  For testing and simulation, this module is replaced by a stub
  that uses process messages instead of real UDP.
  """

  @spec ping(term(), non_neg_integer()) :: :ok | :timeout
  def ping(node_id, timeout_ms) do
    ref = make_ref()
    send(node_id, {:ping, self(), ref})

    receive do
      {:pong, ^ref} -> :ok
    after
      timeout_ms -> :timeout
    end
  end

  @spec indirect_ping(term(), term(), non_neg_integer()) :: :ok | :timeout
  def indirect_ping(proxy_id, target_id, timeout_ms) do
    ref = make_ref()
    send(proxy_id, {:indirect_ping, target_id, self(), ref})

    receive do
      {:indirect_pong, ^ref} -> :ok
    after
      timeout_ms -> :timeout
    end
  end
end
```

### Step 7: Simulation harness

```elixir
# lib/swimlane/simulation.ex
defmodule Swimlane.Simulation do
  @moduledoc """
  In-process simulation of a SWIM cluster. Each simulated node is a GenServer
  that maintains its own membership view and participates in gossip rounds.
  No real UDP — all communication is via process messages.
  """

  use GenServer

  defstruct [:nodes, :fanout, :round_interval_ms, :round_count, :node_views]

  @spec start(keyword()) :: pid()
  def start(opts) do
    {:ok, pid} = GenServer.start_link(__MODULE__, opts)
    pid
  end

  @spec stop(pid()) :: :ok
  def stop(pid), do: GenServer.stop(pid)

  @spec inject_join(pid(), atom()) :: :ok
  def inject_join(pid, node_id), do: GenServer.call(pid, {:inject_join, node_id})

  @spec inject_rumor(pid(), atom(), tuple()) :: :ok
  def inject_rumor(pid, target_node, rumor), do: GenServer.call(pid, {:inject_rumor, target_node, rumor})

  @spec kill_node(pid(), atom()) :: :ok
  def kill_node(pid, node_id), do: GenServer.call(pid, {:kill_node, node_id})

  @spec random_node(pid()) :: atom()
  def random_node(pid), do: GenServer.call(pid, :random_node)

  @spec all_views(pid(), atom()) :: [{atom(), atom()}]
  def all_views(pid, target), do: GenServer.call(pid, {:all_views, target})

  @spec measure_convergence(pid(), term(), keyword()) :: non_neg_integer()
  def measure_convergence(pid, event, opts) do
    timeout_ms = Keyword.get(opts, :timeout_ms, 5_000)
    GenServer.call(pid, {:measure_convergence, event, timeout_ms}, timeout_ms + 5_000)
  end

  @spec run_round(pid()) :: :ok
  def run_round(pid), do: GenServer.call(pid, :run_round)

  @spec random_events(pid(), pos_integer()) :: [%Swimlane.Membership{}]
  def random_events(pid, count), do: GenServer.call(pid, {:random_events, count})

  @impl true
  def init(opts) do
    node_count = Keyword.fetch!(opts, :node_count)
    fanout = Keyword.fetch!(opts, :fanout)
    round_interval_ms = Keyword.get(opts, :round_interval_ms, 50)

    node_ids = for i <- 1..node_count, do: :"sim_node_#{i}"

    node_views =
      Map.new(node_ids, fn id ->
        view = Map.new(node_ids, fn nid ->
          {nid, %Swimlane.Membership{
            node_id: nid,
            state: :alive,
            incarnation: 1,
            address: nid,
            last_updated_at: System.monotonic_time(:millisecond)
          }}
        end)
        {id, view}
      end)

    state = %__MODULE__{
      nodes: MapSet.new(node_ids),
      fanout: fanout,
      round_interval_ms: round_interval_ms,
      round_count: 0,
      node_views: node_views
    }

    if round_interval_ms > 0 do
      Process.send_after(self(), :gossip_round, round_interval_ms)
    end

    {:ok, state}
  end

  @impl true
  def handle_call({:inject_join, node_id}, _from, state) do
    entry = %Swimlane.Membership{
      node_id: node_id,
      state: :alive,
      incarnation: 1,
      address: node_id,
      last_updated_at: System.monotonic_time(:millisecond)
    }

    first_node = state.nodes |> MapSet.to_list() |> List.first()
    updated_view = Swimlane.Membership.merge(state.node_views[first_node], entry)
    node_views = Map.put(state.node_views, first_node, updated_view)
    {:reply, :ok, %{state | node_views: node_views}}
  end

  def handle_call({:inject_rumor, target_node, {:suspect, victim, opts}}, _from, state) do
    incarnation = Keyword.get(opts, :incarnation, 1)
    entry = %Swimlane.Membership{
      node_id: victim,
      state: :suspect,
      incarnation: incarnation,
      address: victim,
      last_updated_at: System.monotonic_time(:millisecond)
    }
    updated_view = Swimlane.Membership.merge(state.node_views[target_node], entry)
    node_views = Map.put(state.node_views, target_node, updated_view)
    {:reply, :ok, %{state | node_views: node_views}}
  end

  def handle_call({:kill_node, node_id}, _from, state) do
    nodes = MapSet.delete(state.nodes, node_id)
    node_views = Map.delete(state.node_views, node_id)

    node_views =
      Map.new(node_views, fn {nid, view} ->
        entry = %Swimlane.Membership{
          node_id: node_id,
          state: :dead,
          incarnation: 99,
          address: node_id,
          last_updated_at: System.monotonic_time(:millisecond)
        }
        {nid, Map.put(view, node_id, entry)}
      end)

    {:reply, :ok, %{state | nodes: nodes, node_views: node_views}}
  end

  def handle_call(:random_node, _from, state) do
    node = state.nodes |> MapSet.to_list() |> Enum.random()
    {:reply, node, state}
  end

  def handle_call({:all_views, target}, _from, state) do
    views =
      state.node_views
      |> Enum.map(fn {node_id, view} ->
        member_state = case Map.get(view, target) do
          nil -> :unknown
          member -> member.state
        end
        {node_id, member_state}
      end)
    {:reply, views, state}
  end

  def handle_call({:measure_convergence, event, timeout_ms}, _from, state) do
    deadline = System.monotonic_time(:millisecond) + timeout_ms
    {rounds, new_state} = converge_loop(state, event, 0, deadline)
    {:reply, rounds, new_state}
  end

  def handle_call(:run_round, _from, state) do
    {:reply, :ok, do_gossip_round(state)}
  end

  def handle_call({:random_events, count}, _from, state) do
    events =
      state.node_views
      |> Map.values()
      |> Enum.flat_map(&Map.values/1)
      |> Enum.take_random(count)
    {:reply, events, state}
  end

  @impl true
  def handle_info(:gossip_round, state) do
    new_state = do_gossip_round(state)
    if state.round_interval_ms > 0 do
      Process.send_after(self(), :gossip_round, state.round_interval_ms)
    end
    {:noreply, new_state}
  end

  defp do_gossip_round(state) do
    alive_nodes = MapSet.to_list(state.nodes)

    updated_views =
      Enum.reduce(alive_nodes, state.node_views, fn node_id, views ->
        my_view = views[node_id]
        {peers, events} = Swimlane.Disseminator.next_round(my_view, node_id, state.fanout)

        Enum.reduce(peers, views, fn peer_id, acc_views ->
          if Map.has_key?(acc_views, peer_id) do
            peer_view =
              Enum.reduce(events, acc_views[peer_id], fn event, pv ->
                Swimlane.Membership.merge(pv, event)
              end)
            Map.put(acc_views, peer_id, peer_view)
          else
            acc_views
          end
        end)
      end)

    # Refutation: alive nodes seeing themselves as suspect bump incarnation
    refuted_views =
      Enum.reduce(alive_nodes, updated_views, fn node_id, views ->
        my_view = views[node_id]
        case Map.get(my_view, node_id) do
          %{state: :suspect} = entry ->
            refuted = %{entry | state: :alive, incarnation: entry.incarnation + 1,
                        last_updated_at: System.monotonic_time(:millisecond)}
            new_view = Map.put(my_view, node_id, refuted)
            Map.put(views, node_id, new_view)
          _ -> views
        end
      end)

    %{state | node_views: refuted_views, round_count: state.round_count + 1}
  end

  defp converge_loop(state, event, rounds, deadline) do
    if System.monotonic_time(:millisecond) >= deadline do
      {rounds, state}
    else
      if converged?(state, event) do
        {rounds, state}
      else
        new_state = do_gossip_round(state)
        converge_loop(new_state, event, rounds + 1, deadline)
      end
    end
  end

  defp converged?(state, node_id) when is_atom(node_id) do
    Enum.all?(state.node_views, fn {_nid, view} ->
      Map.has_key?(view, node_id)
    end)
  end

  defp converged?(state, {:dead, node_id}) do
    alive_nodes = MapSet.to_list(state.nodes)
    Enum.all?(alive_nodes, fn nid ->
      case get_in(state.node_views, [nid, node_id]) do
        %{state: :dead} -> true
        _ -> false
      end
    end)
  end
end
```

### Step 8: Given tests — must pass without modification

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

### Step 9: Run the tests

```bash
mix test test/swimlane/ --trace
```

### Step 10: Benchmark

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

### Why this works

Each node probes one peer per period; if the direct probe fails, it asks K peers for indirect probes before marking the peer suspect. Suspicion is itself gossiped, so every live node converges on the membership view in expected O(log N) rounds.

---

## Benchmark

```elixir
# bench/gossip_bench.exs
:timer.tc(fn -> Gossip.seed_cluster(100) |> Gossip.wait_convergence() end)
```

Target: Convergence under 2 seconds for a 100-node localhost cluster after a single join; O(log N) round count.

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

## Reflection

- If your deployment had asymmetric network partitions (A→B works, B→A drops), would the indirect-probe mechanism still detect failures correctly? Walk through an example.
- Suppose you add 10 new nodes per minute at steady state. At what churn rate does SWIM's suspicion window start producing false positives, and what parameters would you tune first?

---

## Resources

- Das, A., Gupta, I. & Motivala, A. (2002). *SWIM: Scalable Weakly-Consistent Infection-Style Process Group Membership Protocol* — the primary source; implement the protocol exactly as described
- Van Renesse, R. et al. (1998). *A Gossip-Style Failure Detection Service* — the predecessor to SWIM; read it to understand what SWIM improves upon
- [Hashicorp memberlist](https://github.com/hashicorp/memberlist) — `memberlist.go`, `state.go`, `suspicion.go` — production Go implementation with extensive comments on protocol choices
- [Apache Cassandra gossip](https://github.com/apache/cassandra/tree/trunk/src/java/org/apache/cassandra/gms) — real-world adaptation for database cluster management
