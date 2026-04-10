# 20. Build Conflict-Free Replicated Data Types (CRDTs)

**Difficulty**: Insane

## Prerequisites

- Mastered: GenServer, distributed Elixir (node connections, `:rpc`), ETS, binary encoding
- Mastered: Set theory, monotonic lattices, partial ordering concepts
- Familiarity with: CRDT survey paper (Shapiro et al. 2011), vector clocks, Hybrid Logical Clocks, Riak's riak_dt library, Automerge internals

## Problem Statement

Implement a suite of Conflict-Free Replicated Data Types in Elixir that enable multiple
nodes to make changes independently and then merge their states without coordination or
consensus — convergence is guaranteed by mathematical properties of the data structures:

1. Implement a **G-Counter** (grow-only counter): each node has its own counter slot;
   the global value is the sum of all slots; merge takes the max per slot.
2. Implement a **PN-Counter** (positive-negative counter): two G-Counters (increments
   and decrements); value is `sum(P) - sum(D)`; supports negative values and decrement.
3. Implement an **OR-Set** (Observed-Remove Set): each `add(element)` tags the element
   with a unique dot (actor, counter); `remove(element)` removes all observed dots for
   that element; concurrent add and remove resolves in favor of add.
4. Implement a **LWW-Register** (Last-Write-Wins Register): stores a single value with
   a timestamp; merge selects the value with the highest timestamp; use Hybrid Logical
   Clocks (HLC) as the timestamp to maintain causal consistency across nodes with
   unsynchronized clocks.
5. Implement an **RGA** (Replicated Growable Array) for collaborative text editing:
   each character is tagged with a unique ID; insertions use the ID of the preceding
   character as an anchor; concurrent insertions at the same position are resolved by
   a tie-breaking rule (higher actor ID wins or lexicographic order).
6. All CRDTs must implement a `merge/2` function that is commutative, associative, and
   idempotent (merge of identical states is a no-op).
7. Causal consistency: use Dotted Version Vectors (DVV) or a per-actor vector clock to
   track causality. A state update must carry its causal context so that merges can
   detect concurrent vs causally ordered operations.
8. Simulate a 5-node cluster: each node makes independent changes while partitioned;
   after partition heals, states are merged via gossip until all nodes converge to the
   same state. All 5 nodes must converge within 1 second after the last merge round.

## Acceptance Criteria

- [ ] `GCounter.increment(counter, :node_a)` increments node_a's slot; `GCounter.value(counter)`
      returns the sum of all slots; `GCounter.merge(c1, c2)` takes slot-wise max.
- [ ] `GCounter.merge(c1, c2)` is idempotent: `merge(c1, merge(c1, c2)) == merge(c1, c2)`.
- [ ] `PNCounter.decrement(counter, :node_a)` produces a negative contribution;
      `PNCounter.value/1` correctly returns negative values when decrements exceed increments.
- [ ] `ORSet.add(set, :apple, :node_a)` followed by `ORSet.remove(set, :apple)` removes
      the element; a concurrent `add` on another node that is merged after the remove wins
      (add-wins semantics).
- [ ] `LWWRegister.write(reg, "value", :node_a)` stores the value with an HLC timestamp;
      `LWWRegister.merge(r1, r2)` selects the value with the higher HLC; the register
      never goes backwards in time even across nodes with clock skew up to 500ms.
- [ ] `RGA.insert(doc, after_id: :root, value: "H", actor: :node_a)` inserts "H" at the
      beginning; subsequent inserts build correct string order regardless of arrival order.
- [ ] Concurrent inserts at the same position by two different actors produce the same
      final character order on all nodes (tie-breaking is deterministic).
- [ ] A simulation with 5 nodes, each making 200 random operations while disconnected,
      converges to the same state on all nodes within 1 second of reconnection.
- [ ] `CRDT.merge(a, b)` satisfies: `merge(a, b) == merge(b, a)` (commutative),
      `merge(a, merge(b, c)) == merge(merge(a, b), c)` (associative),
      `merge(a, a) == a` (idempotent) — verified by property-based tests with `StreamData`.

## What You Will Learn

- Join-semilattice structure: what makes a data type a valid CRDT (monotonic merge, no information loss)
- Dotted Version Vectors (DVV): how they improve on plain vector clocks for detecting concurrent vs. causally ordered events
- Hybrid Logical Clocks: combining physical time and logical time to achieve total order compatible with wall-clock time
- OR-Set mechanics: why "remove all observed tags" is the correct semantics and why simpler timestamp-based approaches fail
- RGA text editing: the anchor-based insertion model and how it avoids the interleaving anomaly present in OT approaches
- Gossip protocol design: how to spread state updates across a cluster with bounded communication cost

## Hints

This exercise is intentionally sparse. Research:

- G-Counter: represent as a map `%{node_id => count}`; merge is `Map.merge(c1, c2, fn _k, v1, v2 -> max(v1, v2) end)`
- OR-Set: represent each element as `{element, MapSet.t(dot)}` where a dot is `{actor, sequence_number}`; the DVV context tracks the maximum seen sequence per actor
- HLC: `{physical_time_ms, logical_counter, node_id}`; on message receive, `new_physical = max(local_physical, received_physical)`, then increment counter if tied
- RGA: store the sequence as a list of `{id, char, deleted?}` where `id = {actor, counter}`; on insert, find the anchor and insert after all elements with a higher-priority ID at the same position
- Property-based tests: use `StreamData.member_of([:node_a, :node_b, :node_c])` and `StreamData.integer()` to generate random sequences of operations; verify all three lattice laws hold for any generated pair of states

## Reference Material

- CRDT survey: Shapiro et al., "A Comprehensive Study of Convergent and Commutative Replicated Data Types", INRIA RR-7506, 2011
- Dotted Version Vectors: Preguiça et al., "Dotted Version Vectors: Logical Clocks for Optimistic Replication", 2010
- Hybrid Logical Clocks paper: Kulkarni et al., "Logical Physical Clocks and Consistent Snapshots in Globally Distributed Databases", 2014
- RGA paper: Roh et al., "Replicated abstract data types: Building blocks for collaborative applications", JSS 2011
- Automerge implementation (JavaScript reference): https://github.com/automerge/automerge
- riak_dt Elixir/Erlang reference: https://github.com/basho/riak_dt

## Difficulty Rating

★★★★★★

## Estimated Time

55–80 hours
