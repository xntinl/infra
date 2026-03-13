# 9. CRDTs: Conflict-Free Replicated Data Types

<!--
difficulty: insane
concepts: [crdt, convergent-replicated, commutative-replicated, g-counter, pn-counter, g-set, or-set, lww-register, merge-function]
tools: [go]
estimated_time: 2h
bloom_level: create
prerequisites: [vector-clocks, gossip-protocol]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of vector clocks and eventual consistency
- Familiarity with gossip protocols for state dissemination

## Learning Objectives

- **Create** multiple CRDT types: G-Counter, PN-Counter, G-Set, OR-Set, and LWW-Register
- **Analyze** the mathematical properties (commutativity, associativity, idempotency) that guarantee convergence
- **Evaluate** the tradeoffs of CRDTs vs consensus-based replication

## The Challenge

CRDTs are data structures that can be replicated across nodes and updated independently without coordination. When replicas merge, they converge to the same state regardless of the order in which updates and merges are applied. This is possible because the merge function is commutative, associative, and idempotent -- forming a join semilattice.

Implement the five fundamental CRDTs, test that they converge under all orderings of operations and merges, and build a replicated application using them.

## Requirements

1. Implement a `GCounter` (grow-only counter): each node has its own counter that only increments. The value is the sum of all node counters. Merge takes the component-wise maximum.
2. Implement a `PNCounter` (positive-negative counter): two G-Counters, one for increments and one for decrements. The value is the difference. Supports both increment and decrement.
3. Implement a `GSet` (grow-only set): elements can be added but never removed. Merge is set union.
4. Implement an `ORSet` (observed-remove set): supports both add and remove. Each add is tagged with a unique ID. Remove deletes all known tags for an element. Concurrent add and remove of the same element results in the element being present (add-wins semantics).
5. Implement an `LWWRegister` (last-writer-wins register): stores a single value with a timestamp. Merge keeps the value with the highest timestamp.
6. Write property-based tests verifying the semilattice laws for each CRDT:
   - Commutativity: `merge(a, b) == merge(b, a)`
   - Associativity: `merge(merge(a, b), c) == merge(a, merge(b, c))`
   - Idempotency: `merge(a, a) == a`
7. Build a replicated shopping cart using ORSet and PNCounter: add items (ORSet), track quantities (PNCounter), replicate across 3 nodes, and verify convergence after network partition heals
8. Benchmark merge operations for each CRDT type at different scales

## Hints

- A G-Counter is a map `{nodeID: count}`. Increment only touches the local node's entry. Merge takes the max of each entry. Value is the sum of all entries.
- PN-Counter: `value = GCounter(increments).Value() - GCounter(decrements).Value()`.
- OR-Set: each add generates a unique tag (e.g., UUID). The set stores `{element: set_of_tags}`. Remove deletes specific observed tags. An element is present if it has any tags.
- LWW-Register: tie-break timestamps with node ID for determinism.
- For property tests, generate random operation sequences, apply them in different orders, merge the results, and verify convergence.
- CRDTs trade expressiveness for availability: they work without coordination but only support monotonic operations (or operations decomposable into monotonic ones).

## Success Criteria

1. All five CRDTs produce correct values after local operations
2. Merge produces identical results regardless of the order in which replicas are merged
3. Property tests confirm commutativity, associativity, and idempotency for each type
4. The OR-Set correctly handles concurrent add/remove (add wins)
5. The shopping cart application converges after a simulated network partition
6. Benchmarks show that merge operations are efficient

## Research Resources

- [A Comprehensive Study of CRDTs (Shapiro et al.)](https://hal.inria.fr/inria-00555588/document) -- the foundational CRDT survey paper
- [CRDTs: An Update (Shapiro)](https://www.youtube.com/watch?v=ebWVLVhiaiY) -- video overview
- [Designing Data-Intensive Applications, Chapter 5](https://dataintensive.net/) -- Kleppmann's coverage of CRDTs
- [Automerge](https://automerge.org/) -- a CRDT-based JSON document library
- [Riak Data Types](https://riak.com/posts/technical/distributed-data-types-riak-2-0/) -- production CRDT usage

## What's Next

Continue to [10 - Merkle Tree](../10-merkle-tree/10-merkle-tree.md) to implement hash trees for efficient data verification.

## Summary

- CRDTs are data structures that converge without coordination through mathematical properties of their merge function
- The five fundamental CRDTs: G-Counter, PN-Counter, G-Set, OR-Set, LWW-Register
- Convergence is guaranteed by commutativity, associativity, and idempotency of the merge operation
- OR-Set uses unique tags to support both add and remove with add-wins semantics
- CRDTs trade expressiveness for availability -- ideal for eventually consistent systems
- Production use: Riak, Redis (CRDTs), Automerge, collaborative editing systems
