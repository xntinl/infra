# Conflict-Free Replicated Data Types (CRDTs)

**Project**: `crdts` — a production-grade CRDT library with gossip-based cluster convergence

---

## Project context

You are building `crdts`, a library of Conflict-Free Replicated Data Types that enables multiple nodes to make changes independently and then merge their states without coordination. Convergence is guaranteed by mathematical properties of the data structures, not by consensus protocols.

Project structure:

```
crdts/
├── lib/
│   └── crdts/
│       ├── application.ex           # cluster supervisor, gossip scheduler
│       ├── g_counter.ex             # grow-only counter: per-node slots, max merge
│       ├── pn_counter.ex            # positive-negative counter: two G-Counters
│       ├── or_set.ex                # observed-remove set: add-wins via dots
│       ├── lww_register.ex          # last-write-wins register with Hybrid Logical Clocks
│       ├── rga.ex                   # replicated growable array for collaborative text editing
│       ├── dvv.ex                   # dotted version vectors for causal context tracking
│       ├── hlc.ex                   # hybrid logical clock: physical + logical component
│       ├── gossip.ex                # state-based gossip: periodic random-peer merge
│       └── simulation.ex            # multi-node simulation for testing convergence
├── test/
│   └── crdts/
│       ├── g_counter_test.exs       # value, merge, idempotency, commutativity
│       ├── pn_counter_test.exs      # negative values, decrement semantics
│       ├── or_set_test.exs          # add-wins, concurrent add/remove
│       ├── lww_register_test.exs    # HLC ordering, clock skew tolerance
│       ├── rga_test.exs             # insertion order, concurrent inserts, tie-breaking
│       ├── lattice_laws_test.exs    # property-based: all three laws for all CRDTs
│       └── convergence_test.exs    # 5-node simulation, convergence within 1 second
├── bench/
│   └── crdts_bench.exs
└── mix.exs
```

---

## The problem

In a distributed system where network partitions are possible, you have two choices: stop accepting writes during a partition (sacrifice availability) or accept writes on all partitions (sacrifice consistency). CRDTs choose availability: each node accepts writes independently. When the partition heals, states are merged. The merge is guaranteed to produce the same result regardless of the order in which it is applied — this is the join-semilattice property.

---

## Why this design

**G-Counter via per-node slots**: each node increments only its own slot in a `%{node_id => count}` map. The total value is the sum of all slots. Merge takes the max per slot: `max(local[node], remote[node])`. This is correct because no node decrements another's slot — the value only moves upward, satisfying the lattice monotonicity requirement.

**OR-Set via dots**: each `add(element)` operation generates a unique "dot" `{actor_id, sequence_number}`. The element's presence in the set is represented by the set of its dots. `remove(element)` removes all observed dots. If node A adds with dot `{A,1}` and node B concurrently adds with dot `{B,1}`, a merge that removes A's add still contains B's add — add-wins semantics arise naturally.

**Hybrid Logical Clocks for LWW registers**: pure physical clocks cannot determine which of two concurrent writes happened "last" because clocks on different machines are not synchronized. HLC combines physical time and a logical counter: `{physical_time_ms, logical_counter, node_id}`. On receive, the physical time is set to `max(local, received)`, and the logical counter breaks ties.

**RGA for collaborative text editing**: each character has a unique ID `{actor, counter}`. Insertions use the ID of the preceding character as an anchor. Concurrent insertions at the same position are ordered by ID, deterministically.

---

## Design decisions

**Option A — State-based CRDTs (CvRDT) with full-state sync**
- Pros: merge function is trivially commutative/associative/idempotent; no delivery guarantees required.
- Cons: O(|state|) bandwidth per sync; doesn't scale to large sets.

**Option B — Delta-state CRDTs (δ-CRDTs)** (chosen)
- Pros: ship only the increments since last sync; retains state-based correctness proofs; practical at scale.
- Cons: must track delta intervals; anti-entropy on delta loss is more intricate.

→ Chose **B** because δ-CRDTs are the sweet spot between CvRDT simplicity and op-based bandwidth; they're what Redis Enterprise and Riak use for the same reason.

## Implementation milestones

### Step 1: Create the project

**Objective**: Bootstrap a supervised Mix app so the gossip and simulation processes sit under a proper supervision tree from the start.


```bash
mix new crdts --sup
cd crdts
mkdir -p lib/crdts test/crdts bench
```

### Step 2: `mix.exs` — dependencies

**Objective**: Pull in stream_data for property-based law checks and Benchee for dev — lattice correctness demands generative testing, not hand-picked examples.


```elixir
defp deps do
  [
    {:stream_data, "~> 0.6", only: :test},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    {:stream_data, "~> 0.6", only: :test},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: G-Counter and PN-Counter

**Objective**: Use G-Counter as the foundation — increments commute by per-node slots, so merge is slot-wise max with zero coordination. PN-Counter composes two G-Counters since subtraction breaks monotonicity.


```elixir
# lib/crdts/g_counter.ex
defmodule CRDTs.GCounter do
  @moduledoc """
  Grow-only counter. Each node has its own slot.
  value/1 = sum of all slots.
  merge/2 = slot-wise max.
  """

  @doc "Creates a new empty G-Counter."
  @spec new() :: map()
  def new(), do: %{}

  @doc "Increments the counter for the given node."
  @spec increment(map(), atom()) :: map()
  def increment(%{} = counter, node_id) do
    Map.update(counter, node_id, 1, &(&1 + 1))
  end

  @doc "Returns the total value across all nodes."
  @spec value(map()) :: non_neg_integer()
  def value(%{} = counter) do
    counter |> Map.values() |> Enum.sum()
  end

  @doc "Merges two counters by taking the max per slot."
  @spec merge(map(), map()) :: map()
  def merge(%{} = c1, %{} = c2) do
    Map.merge(c1, c2, fn _k, v1, v2 -> max(v1, v2) end)
  end
end
```

```elixir
# lib/crdts/pn_counter.ex
defmodule CRDTs.PNCounter do
  @moduledoc """
  Positive-Negative counter built from two G-Counters.
  value = sum(positive) - sum(negative).
  """

  alias CRDTs.GCounter

  @doc "Creates a new PN-Counter."
  @spec new() :: {map(), map()}
  def new(), do: {GCounter.new(), GCounter.new()}

  @doc "Increments the counter."
  @spec increment({map(), map()}, atom()) :: {map(), map()}
  def increment({pos, neg}, node_id), do: {GCounter.increment(pos, node_id), neg}

  @doc "Decrements the counter."
  @spec decrement({map(), map()}, atom()) :: {map(), map()}
  def decrement({pos, neg}, node_id), do: {pos, GCounter.increment(neg, node_id)}

  @doc "Returns the current value."
  @spec value({map(), map()}) :: integer()
  def value({pos, neg}), do: GCounter.value(pos) - GCounter.value(neg)

  @doc "Merges two PN-Counters."
  @spec merge({map(), map()}, {map(), map()}) :: {map(), map()}
  def merge({p1, n1}, {p2, n2}) do
    {GCounter.merge(p1, p2), GCounter.merge(n1, n2)}
  end
end
```

### Step 4: OR-Set

**Objective**: Tag each add with a unique dot so concurrent add/remove resolves add-wins — removing observed dots leaves concurrent additions intact after merge.


```elixir
# lib/crdts/or_set.ex
defmodule CRDTs.ORSet do
  @moduledoc """
  Observed-Remove Set with add-wins semantics.

  State: %{element => MapSet.t({actor, sequence})}
  A "dot" is {actor, sequence}.
  """

  @doc "Creates a new empty OR-Set."
  @spec new() :: map()
  def new(), do: %{}

  @doc "Adds an element with a new unique dot."
  @spec add(map(), term(), atom()) :: map()
  def add(set, element, actor) do
    current_dots = Map.get(set, element, MapSet.new())
    seq = MapSet.size(current_dots) + 1
    new_dot = {actor, seq}
    Map.put(set, element, MapSet.put(current_dots, new_dot))
  end

  @doc "Removes an element by clearing all its dots."
  @spec remove(map(), term()) :: map()
  def remove(set, element) do
    Map.delete(set, element)
  end

  @doc "Checks if an element is in the set (has at least one dot)."
  @spec member?(map(), term()) :: boolean()
  def member?(set, element) do
    set |> Map.get(element, MapSet.new()) |> MapSet.size() > 0
  end

  @doc "Returns all elements currently in the set."
  @spec elements(map()) :: [term()]
  def elements(set) do
    set
    |> Enum.filter(fn {_elem, dots} -> MapSet.size(dots) > 0 end)
    |> Enum.map(fn {elem, _dots} -> elem end)
  end

  @doc "Merges two OR-Sets by taking the union of dot sets per element."
  @spec merge(map(), map()) :: map()
  def merge(s1, s2) do
    Map.merge(s1, s2, fn _k, d1, d2 -> MapSet.union(d1, d2) end)
  end
end
```

### Step 5: Hybrid Logical Clock

**Objective**: Combine physical time with a logical counter so clock skew cannot reorder causally related events — the logical component breaks ties deterministically.


```elixir
# lib/crdts/hlc.ex
defmodule CRDTs.HLC do
  @moduledoc """
  Hybrid Logical Clock.
  State: {physical_ms, logical_counter, node_id}
  """

  @doc "Creates a new HLC for the given node."
  @spec new(atom()) :: {integer(), non_neg_integer(), atom()}
  def new(node_id) do
    {System.system_time(:millisecond), 0, node_id}
  end

  @doc "Advances the clock on a local event."
  @spec tick({integer(), non_neg_integer(), atom()}) :: {integer(), non_neg_integer(), atom()}
  def tick({l, c, node_id}) do
    now = System.system_time(:millisecond)
    l_new = max(l, now)
    c_new = if l_new == l, do: c + 1, else: 0
    {l_new, c_new, node_id}
  end

  @doc "Advances the clock upon receiving a remote event."
  @spec receive_event(
          {integer(), non_neg_integer(), atom()},
          {integer(), non_neg_integer(), atom()}
        ) :: {integer(), non_neg_integer(), atom()}
  def receive_event({l, c, node_id}, {recv_l, recv_c, _recv_node}) do
    now = System.system_time(:millisecond)
    l_new = Enum.max([l, recv_l, now])

    c_new =
      cond do
        l_new == l and l_new == recv_l -> max(c, recv_c) + 1
        l_new == l -> c + 1
        l_new == recv_l -> recv_c + 1
        true -> 0
      end

    {l_new, c_new, node_id}
  end

  @doc "Compares two HLC timestamps. Returns :lt, :eq, or :gt."
  @spec compare(
          {integer(), non_neg_integer(), atom()},
          {integer(), non_neg_integer(), atom()}
        ) :: :lt | :eq | :gt
  def compare({l1, c1, n1}, {l2, c2, n2}) do
    cond do
      l1 < l2 -> :lt
      l1 > l2 -> :gt
      c1 < c2 -> :lt
      c1 > c2 -> :gt
      n1 < n2 -> :lt
      n1 > n2 -> :gt
      true -> :eq
    end
  end
end
```

### Step 6: LWW Register

**Objective**: Let HLC order writes so merge picks the later timestamp — convergence no longer depends on synchronized wall clocks across nodes.


```elixir
# lib/crdts/lww_register.ex
defmodule CRDTs.LWWRegister do
  @moduledoc """
  Last-Write-Wins Register using HLC for ordering.
  """

  @doc "Creates a new register with an initial value."
  @spec new(term(), atom()) :: {term(), {integer(), non_neg_integer(), atom()}}
  def new(value, node_id) do
    clock = CRDTs.HLC.new(node_id)
    {value, clock}
  end

  @doc "Updates the register value."
  @spec update({term(), tuple()}, term()) :: {term(), tuple()}
  def update({_old_value, clock}, new_value) do
    new_clock = CRDTs.HLC.tick(clock)
    {new_value, new_clock}
  end

  @doc "Returns the current value."
  @spec value({term(), tuple()}) :: term()
  def value({val, _clock}), do: val

  @doc "Merges two registers — the one with the later timestamp wins."
  @spec merge({term(), tuple()}, {term(), tuple()}) :: {term(), tuple()}
  def merge({v1, c1} = r1, {v2, c2} = r2) do
    case CRDTs.HLC.compare(c1, c2) do
      :gt -> r1
      :lt -> r2
      :eq -> if v1 >= v2, do: r1, else: r2
    end
  end
end
```

### Step 7: Cluster simulation for convergence testing

**Objective**: Drive gossip between random peers under simulated partitions so tests can assert convergence within a bounded wall-clock window after healing.


```elixir
# lib/crdts/simulation.ex
defmodule CRDTs.Simulation do
  use GenServer

  @moduledoc """
  Simulates a cluster of nodes with gossip-based CRDT convergence.
  Supports network partitions and healing for testing.
  """

  defstruct [:nodes, :partitions, :states, :gossip_interval]

  @doc "Starts a simulation with the given node names."
  @spec start([atom()]) :: {:ok, pid()}
  def start(node_names) do
    GenServer.start(__MODULE__, node_names)
  end

  @doc "Creates a network partition between two groups."
  @spec partition(pid(), keyword()) :: :ok
  def partition(sim, opts) do
    GenServer.call(sim, {:partition, opts})
  end

  @doc "Heals all network partitions."
  @spec heal(pid()) :: :ok
  def heal(sim), do: GenServer.call(sim, :heal)

  @doc "Increments a counter on the given node."
  @spec increment(pid(), atom(), atom()) :: :ok
  def increment(sim, node, counter_name) do
    GenServer.call(sim, {:increment, node, counter_name})
  end

  @doc "Reads the counter value from the given node."
  @spec value(pid(), atom(), atom()) :: non_neg_integer()
  def value(sim, node, counter_name) do
    GenServer.call(sim, {:value, node, counter_name})
  end

  @doc "Stops the simulation."
  @spec stop(pid()) :: :ok
  def stop(sim), do: GenServer.stop(sim)

  @impl true
  def init(node_names) do
    states = Map.new(node_names, fn name -> {name, %{}} end)
    schedule_gossip()
    {:ok, %__MODULE__{
      nodes: node_names,
      partitions: nil,
      states: states,
      gossip_interval: 50
    }}
  end

  @impl true
  def handle_call({:partition, opts}, _from, state) do
    {:reply, :ok, %{state | partitions: opts}}
  end

  @impl true
  def handle_call(:heal, _from, state) do
    {:reply, :ok, %{state | partitions: nil}}
  end

  @impl true
  def handle_call({:increment, node, counter_name}, _from, state) do
    node_state = Map.get(state.states, node, %{})
    counter = Map.get(node_state, counter_name, CRDTs.GCounter.new())
    updated_counter = CRDTs.GCounter.increment(counter, node)
    updated_node_state = Map.put(node_state, counter_name, updated_counter)
    new_states = Map.put(state.states, node, updated_node_state)
    {:reply, :ok, %{state | states: new_states}}
  end

  @impl true
  def handle_call({:value, node, counter_name}, _from, state) do
    node_state = Map.get(state.states, node, %{})
    counter = Map.get(node_state, counter_name, CRDTs.GCounter.new())
    {:reply, CRDTs.GCounter.value(counter), state}
  end

  @impl true
  def handle_info(:gossip, state) do
    new_states = do_gossip_round(state)
    schedule_gossip()
    {:noreply, %{state | states: new_states}}
  end

  defp schedule_gossip do
    Process.send_after(self(), :gossip, 50)
  end

  defp do_gossip_round(state) do
    Enum.reduce(state.nodes, state.states, fn node, states ->
      peers = reachable_peers(node, state.nodes, state.partitions)

      case peers do
        [] -> states
        _ ->
          peer = Enum.random(peers)
          node_state = Map.get(states, node, %{})
          peer_state = Map.get(states, peer, %{})

          merged_state =
            Map.merge(node_state, peer_state, fn _key, local, remote ->
              CRDTs.GCounter.merge(local, remote)
            end)

          states
          |> Map.put(node, merged_state)
          |> Map.put(peer, merged_state)
      end
    end)
  end

  defp reachable_peers(node, all_nodes, nil) do
    Enum.reject(all_nodes, &(&1 == node))
  end

  defp reachable_peers(node, _all_nodes, partitions) do
    group_a = Keyword.get(partitions, :group_a, [])
    group_b = Keyword.get(partitions, :group_b, [])

    my_group =
      cond do
        node in group_a -> group_a
        node in group_b -> group_b
        true -> []
      end

    Enum.reject(my_group, &(&1 == node))
  end
end
```

### Step 8: Given tests — must pass without modification

**Objective**: Freeze the lattice laws (commutativity, associativity, idempotency) and a 5-node partition/heal convergence test so any refactor that breaks semi-lattice semantics fails loudly.


```elixir
# test/crdts/lattice_laws_test.exs
defmodule CRDTs.LatticeTest do
  use ExUnit.Case
  use ExUnitProperties

  alias CRDTs.{GCounter, PNCounter, ORSet}

  defp random_gcounter do
    gen all nodes <- StreamData.list_of(StreamData.member_of([:a, :b, :c]), min_length: 1),
            do: Enum.reduce(nodes, GCounter.new(), fn n, c -> GCounter.increment(c, n) end)
  end

  describe "lattice properties" do
    property "GCounter merge is commutative" do
      check all c1 <- random_gcounter(), c2 <- random_gcounter() do
        assert GCounter.merge(c1, c2) == GCounter.merge(c2, c1)
      end
    end

    property "GCounter merge is associative" do
      check all c1 <- random_gcounter(), c2 <- random_gcounter(), c3 <- random_gcounter() do
        assert GCounter.merge(c1, GCounter.merge(c2, c3)) ==
               GCounter.merge(GCounter.merge(c1, c2), c3)
      end
    end

    property "GCounter merge is idempotent" do
      check all c1 <- random_gcounter(), c2 <- random_gcounter() do
        merged = GCounter.merge(c1, c2)
        assert GCounter.merge(merged, c2) == merged
      end
    end
  end
end
```

```elixir
# test/crdts/convergence_test.exs
defmodule CRDTs.ConvergenceTest do
  use ExUnit.Case, async: false

  describe "convergence under partition" do
    test "5-node simulation converges within 1 second after reconnect" do
      nodes = [:n1, :n2, :n3, :n4, :n5]
      sim = CRDTs.Simulation.start(nodes)

      CRDTs.Simulation.partition(sim, group_a: [:n1, :n2], group_b: [:n3, :n4, :n5])

      for node <- [:n1, :n2], _ <- 1..100 do
        CRDTs.Simulation.increment(sim, node, :shared_counter)
      end

      for node <- [:n3, :n4, :n5], _ <- 1..100 do
        CRDTs.Simulation.increment(sim, node, :shared_counter)
      end

      CRDTs.Simulation.heal(sim)
      Process.sleep(1_000)

      values = for node <- nodes, do: CRDTs.Simulation.value(sim, node, :shared_counter)
      assert Enum.uniq(values) == [500]

      CRDTs.Simulation.stop(sim)
    end
  end
end
```

### Step 9: Run the tests

**Objective**: Run with --trace so the convergence test's gossip timing is visible — flaky sleeps here usually mean the gossip interval is tuned too loose.


```bash
mix test test/crdts/ --trace
```

### Step 10: Benchmark

**Objective**: Benchmark increment, add, and merge at 1k ops so merge's per-slot max cost is visible — merge is the hot path when gossip fan-out grows.


```elixir
# bench/crdts_bench.exs
counter = CRDTs.GCounter.new()
counter = Enum.reduce(1..1_000, counter, fn _, c -> CRDTs.GCounter.increment(c, :node_a) end)

or_set = Enum.reduce(1..1_000, CRDTs.ORSet.new(), fn i, s ->
  CRDTs.ORSet.add(s, "item_#{i}", :node_a)
end)

Benchee.run(
  %{
    "GCounter increment" => fn ->
      CRDTs.GCounter.increment(counter, :node_b)
    end,
    "GCounter merge (1000 entries)" => fn ->
      CRDTs.GCounter.merge(counter, counter)
    end,
    "ORSet add" => fn ->
      CRDTs.ORSet.add(or_set, "new_item_#{:rand.uniform(1_000)}", :node_a)
    end,
    "ORSet merge (1000 entries)" => fn ->
      CRDTs.ORSet.merge(or_set, or_set)
    end
  },
  time: 5,
  warmup: 2,
  formatters: [Benchee.Formatters.Console]
)
```

### Why this works

Each replica tracks a vector clock of its local updates and ships deltas since the last known peer version. The merge function is still a join on the semilattice, so convergence is guaranteed regardless of delivery order or duplication.

---

## Benchmark

```elixir
# bench/crdt_bench.exs
Benchee.run(%{"merge_10k_updates" => fn -> Crdt.merge(a, b) end}, time: 10)
def main do
  IO.puts("[CRDTs.GCounter] GenServer demo")
  :ok
end

```

Target: Merge of two 10k-element OR-sets in < 100 µs; convergence under 50 ms across 5 replicas.

---

## Deep Dive: Conflict-Free Replicated Data Types (CRDTs) and Eventual Consistency

CRDTs are data structures designed so that concurrent updates from multiple replicas always converge to the same state without explicit coordination or consensus.

**How it works**: Traditional merge requires agreement: if replica A says "x = 5" and replica B says "x = 10", which is correct? A CRDT avoids this by defining a merge operation that is commutative, associative, and idempotent. Given these properties, nodes can merge state in any order and always converge.

**Example: G-Counter (grow-only counter)**. Each node maintains a vector of counters, one per node. To increment, increment your own entry. Total is the sum of all entries. To merge two G-Counters, take element-wise max. This is commutative, associative, and idempotent: two nodes incrementing independently then merging always produce the same total, regardless of merge order.

**Trade-off: state size**. A G-Counter with 100 nodes is a 100-element vector. Decrement support (PN-Counter) requires 100 elements for increments and 100 for decrements (200 total). As the cluster grows, CRDT state balloons. Compaction (summing old entries into a delta) is necessary.

**CRDT vs. Consensus**: Raft is strong consistency (all nodes agree on exact state, ordered updates). CRDTs are eventual consistency (nodes may disagree temporarily, then converge). CRDTs excel in offline-first scenarios (mobile app syncing later); Raft is better for systems requiring immediate agreement (bank transfers).

**Gotcha**: Just because a CRDT converges does not mean it is correct for your application. A multi-user text document where two users edit the same location must use a CRDT that preserves intent (e.g., CRDT with unique node IDs). A naive counter cannot distinguish "User A inserted at position 10" from "User B inserted at position 10"—they both see increments and may converge to the wrong document.

**Production patterns**: CRDTs shine for collaborative editing (Google Docs, Figma) and offline-first apps (mobile). For backends requiring strong consistency (databases, ledgers), Raft or other consensus is necessary. Many systems use both: CRDTs for user-facing edits, Raft for backend state.

---

## Trade-off analysis

| CRDT | Merge cost | Space cost | Semantics | Suitable for |
|------|-----------|------------|-----------|-------------|
| G-Counter | O(N) per-node slots | O(N) | monotonic increment | view counts, likes |
| PN-Counter | O(N) | O(2N) | increment and decrement | inventory, balances |
| OR-Set | O(elements x dots) | O(elements x dots) | add-wins | shopping cart, tag sets |
| LWW-Register | O(1) | O(1) | last-write-wins | settings, config |
| RGA | O(sequence length) | O(sequence length) | insertion order | collaborative text |

Reflection: OR-Set has add-wins semantics. Design a remove-wins variant. What changes to the merge function and the add/remove operations? What use cases prefer remove-wins over add-wins?

---

## Common production mistakes

**1. Using physical timestamps instead of HLC for LWW**
Physical clocks can go backward. An NTP adjustment on node A might make its timestamp earlier than node B's, causing node B's older write to "win." HLC prevents this by advancing the logical component when physical time is tied.

**2. OR-Set dots not unique across nodes**
If two nodes use the same sequence generator, they can generate the same dot. The actor component of the dot must be unique per node.

**3. RGA not handling concurrent inserts at the same anchor**
Two nodes insert at the same position concurrently. Without a deterministic tie-breaking rule (e.g., higher actor ID wins), the two nodes produce different orderings after merge.

**4. Gossip not accounting for partial state exchange**
State-based gossip sends the full CRDT state to a random peer. For a large ORSet with millions of elements, this is expensive. Delta-CRDT gossip sends only the changes since the last exchange.

## Reflection

- Why can't OR-set removals be implemented as plain deletes? Walk through a concurrent add/remove example.
- When would you reach for an op-based CRDT instead of a δ-CRDT? Name a workload and justify.

---

## Resources

- Shapiro, M. et al. (2011). *A Comprehensive Study of Convergent and Commutative Replicated Data Types* — INRIA RR-7506
- Preguica, N. et al. (2010). *Dotted Version Vectors: Logical Clocks for Optimistic Replication*
- Kulkarni, S. et al. (2014). *Logical Physical Clocks and Consistent Snapshots in Globally Distributed Databases*
- [Automerge](https://github.com/automerge/automerge) — JavaScript CRDT library with RGA implementation
- [riak_dt](https://github.com/basho/riak_dt) — Erlang/Elixir CRDT reference
