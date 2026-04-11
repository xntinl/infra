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
│       └── gossip.ex                # state-based gossip: periodic random-peer merge
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

**Hybrid Logical Clocks for LWW registers**: pure physical clocks cannot determine which of two concurrent writes happened "last" because clocks on different machines are not synchronized. Logical clocks (Lamport timestamps) are monotonic but lose wall-clock ordering information. HLC combines both: `{physical_time_ms, logical_counter, node_id}`. On receive, the physical time is set to `max(local, received)`, and the logical counter breaks ties. Clock skew up to 500ms is tolerated.

**RGA for collaborative text editing**: each character has a unique ID `{actor, counter}`. Insertions use the ID of the preceding character as an anchor. Concurrent insertions at the same position are ordered by ID, deterministically. This avoids the interleaving anomaly that plagues operational transformation (OT) approaches.

---

## Implementation milestones

### Step 1: Create the project

```bash
mix new crdts --sup
cd crdts
mkdir -p lib/crdts test/crdts bench
```

### Step 2: `mix.exs` — dependencies

```elixir
defp deps do
  [
    {:stream_data, "~> 0.6", only: :test},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: G-Counter and PN-Counter

```elixir
# lib/crdts/g_counter.ex
defmodule CRDTs.GCounter do
  @moduledoc """
  Grow-only counter. Each node has its own slot.
  value/1 = sum of all slots.
  merge/2 = slot-wise max.
  """

  def new(), do: %{}

  def increment(%{} = counter, node_id) do
    Map.update(counter, node_id, 1, &(&1 + 1))
  end

  def value(%{} = counter) do
    counter |> Map.values() |> Enum.sum()
  end

  def merge(%{} = c1, %{} = c2) do
    # TODO: Map.merge(c1, c2, fn _k, v1, v2 -> max(v1, v2) end)
  end
end
```

```elixir
# lib/crdts/or_set.ex
defmodule CRDTs.ORSet do
  @moduledoc """
  Observed-Remove Set with add-wins semantics.

  State: %{element => MapSet.t({actor, sequence})}
  A "dot" is {actor, sequence}.

  add(set, element, actor): generate a new dot, add to element's dot set
  remove(set, element):     remove all current dots for element
  member?(set, element):    true if element has at least one dot
  merge(s1, s2):            union of dot sets per element; concurrent adds always win
  """

  def new(), do: %{}

  def add(set, element, actor) do
    current_dots = Map.get(set, element, MapSet.new())
    seq = MapSet.size(current_dots) + 1  # simple sequence; in production use a vector clock
    new_dot = {actor, seq}
    Map.put(set, element, MapSet.put(current_dots, new_dot))
  end

  def remove(set, element) do
    # TODO: remove all dots for element; the element effectively disappears
    # HINT: Map.delete(set, element)
  end

  def member?(set, element) do
    set |> Map.get(element, MapSet.new()) |> MapSet.size() > 0
  end

  def merge(s1, s2) do
    # TODO: for each element in either set, union the dot sets
    # HINT: Map.merge(s1, s2, fn _k, d1, d2 -> MapSet.union(d1, d2) end)
  end
end
```

### Step 4: Hybrid Logical Clock

```elixir
# lib/crdts/hlc.ex
defmodule CRDTs.HLC do
  @moduledoc """
  Hybrid Logical Clock.
  State: {physical_ms, logical_counter, node_id}

  On send: l' = max(l, physical_ms); if l' == l, c' = c + 1; else c' = 0
  On receive: l' = max(l, recv_l, physical_ms); if l' == recv_l, c' = max(c, recv_c) + 1
                                                 if l' == l, c' = c + 1
                                                 else c' = 0
  """

  def new(node_id) do
    {System.system_time(:millisecond), 0, node_id}
  end

  def tick({l, c, node_id}) do
    now = System.system_time(:millisecond)
    l_new = max(l, now)
    c_new = if l_new == l, do: c + 1, else: 0
    {l_new, c_new, node_id}
  end

  def receive_event({l, c, node_id}, {recv_l, recv_c, _recv_node}) do
    # TODO
  end

  def compare({l1, c1, n1}, {l2, c2, n2}) do
    # TODO: total order: first by l, then c, then node_id (for tie-breaking)
  end
end
```

### Step 5: Given tests — must pass without modification

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
```

```elixir
# test/crdts/convergence_test.exs
defmodule CRDTs.ConvergenceTest do
  use ExUnit.Case, async: false

  test "5-node simulation converges within 1 second after reconnect" do
    nodes = [:n1, :n2, :n3, :n4, :n5]
    sim = CRDTs.Simulation.start(nodes)

    # Partition into two groups
    CRDTs.Simulation.partition(sim, group_a: [:n1, :n2], group_b: [:n3, :n4, :n5])

    # Each partition makes 200 operations on a shared GCounter
    for node <- [:n1, :n2], _ <- 1..100 do
      CRDTs.Simulation.increment(sim, node, :shared_counter)
    end

    for node <- [:n3, :n4, :n5], _ <- 1..100 do
      CRDTs.Simulation.increment(sim, node, :shared_counter)
    end

    # Heal partition
    CRDTs.Simulation.heal(sim)

    # Allow gossip rounds to converge
    Process.sleep(1_000)

    values = for node <- nodes, do: CRDTs.Simulation.value(sim, node, :shared_counter)

    assert Enum.uniq(values) == [500],
      "expected all nodes to converge to 500, got: #{inspect(values)}"

    CRDTs.Simulation.stop(sim)
  end
end
```

### Step 6: Run the tests

```bash
mix test test/crdts/ --trace
```

### Step 7: Benchmark

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

---

## Trade-off analysis

| CRDT | Merge cost | Space cost | Semantics | Suitable for |
|------|-----------|------------|-----------|-------------|
| G-Counter | O(N) per-node slots | O(N) | monotonic increment | view counts, likes |
| PN-Counter | O(N) | O(2N) | increment and decrement | inventory, balances |
| OR-Set | O(elements × dots) | O(elements × dots) | add-wins | shopping cart, tag sets |
| LWW-Register | O(1) | O(1) | last-write-wins | settings, config |
| RGA | O(sequence length) | O(sequence length) | insertion order | collaborative text |

Reflection: OR-Set has add-wins semantics. Design a remove-wins variant. What changes to the merge function and the add/remove operations? What use cases prefer remove-wins over add-wins?

---

## Common production mistakes

**1. Using physical timestamps instead of HLC for LWW**
Physical clocks can go backward. An NTP adjustment on node A might make its timestamp earlier than node B's, causing node B's older write to "win." HLC prevents this by advancing the logical component when physical time is tied.

**2. OR-Set dots not unique across nodes**
If two nodes use the same sequence generator (e.g., a simple integer counter starting at 1), they can generate the same dot `{A, 1}` and `{B, 1}` is not a collision, but `{A, 1}` on two different nodes is. The actor component of the dot must be unique per node.

**3. RGA not handling concurrent inserts at the same anchor**
Two nodes insert at the same position concurrently. Without a deterministic tie-breaking rule (e.g., higher actor ID wins), the two nodes produce different orderings after merge. The tie-breaking rule must be total and deterministic.

**4. Gossip not accounting for partial state exchange**
State-based gossip sends the full CRDT state to a random peer. For a large ORSet with millions of elements, this is expensive. Delta-CRDT gossip sends only the changes since the last exchange. Design the gossip protocol with delta-state in mind from the start.

---

## Resources

- Shapiro, M. et al. (2011). *A Comprehensive Study of Convergent and Commutative Replicated Data Types* — INRIA RR-7506 — the primary survey
- Preguiça, N. et al. (2010). *Dotted Version Vectors: Logical Clocks for Optimistic Replication*
- Kulkarni, S. et al. (2014). *Logical Physical Clocks and Consistent Snapshots in Globally Distributed Databases*
- Roh, H.G. et al. (2011). *Replicated abstract data types: Building blocks for collaborative applications* — JSS
- [Automerge](https://github.com/automerge/automerge) — JavaScript CRDT library with RGA implementation
- [riak_dt](https://github.com/basho/riak_dt) — Erlang/Elixir CRDT reference
