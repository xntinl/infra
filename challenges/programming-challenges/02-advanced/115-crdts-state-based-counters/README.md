# 115. CRDTs: State-Based Replicated Data Types

<!--
difficulty: advanced
category: distributed-systems
languages: [go, rust]
concepts: [crdts, eventual-consistency, semilattice, merge-function, state-based-replication, conflict-resolution]
estimated_time: 8-10 hours
bloom_level: evaluate
prerequisites: [go-basics, rust-basics, generics, hash-maps, concurrency-primitives, set-theory-basics]
-->

## Languages

- Go (1.22+)
- Rust (stable)

## Prerequisites

- Go generics and interfaces; Rust traits and generics
- Hash maps and set data structures in both languages
- Concurrency primitives: mutexes, channels (Go), Arc/Mutex (Rust)
- Basic understanding of partial orders and lattice theory (join semilattice)
- Familiarity with eventual consistency and the CAP theorem

## Learning Objectives

- **Evaluate** the trade-offs between CRDTs and consensus-based replication (Raft, Paxos) for different use cases: when eventual consistency is sufficient versus when strong consistency is required
- **Implement** state-based CRDTs (CvRDTs) with correct merge functions that satisfy the semilattice properties: commutativity, associativity, and idempotency
- **Design** a replication framework that propagates state between replicas and demonstrates convergence without coordination
- **Analyze** the convergence guarantees of each CRDT type under concurrent operations on independent replicas, including adversarial operation orderings
- **Create** both Go and Rust implementations that demonstrate language-specific approaches to the same mathematical structure

## The Challenge

In distributed systems, replicas must agree on state. The traditional approach is consensus: replicas coordinate (via Raft, Paxos, 2PC) to agree on a single value before proceeding. This provides strong consistency but requires coordination, which means latency and unavailability during network partitions. For many applications -- counters, shopping carts, collaborative editing, presence indicators -- this coordination is unnecessary. The operations have mathematical structure that guarantees convergence without coordination.

Conflict-free Replicated Data Types (CRDTs) exploit this structure. A state-based CRDT (CvRDT) defines a merge function that combines the states of two replicas. If the merge function forms a **join semilattice** -- meaning it is commutative (`merge(a,b) = merge(b,a)`), associative (`merge(merge(a,b),c) = merge(a,merge(b,c))`), and idempotent (`merge(a,a) = a`) -- then replicas are guaranteed to converge regardless of the order or number of times states are merged. No coordination needed. No conflict resolution logic. The mathematics handles it.

This is not a theoretical curiosity. Redis uses CRDTs for active-active geo-replication. Riak is built entirely on CRDTs. Apple uses CRDTs in CloudKit. SoundCloud uses CRDTs for real-time play counters. The Shapiro et al. paper (2011) formalized the theory and catalogued a family of CRDTs that you will implement:

- **G-Counter** (Grow-only Counter): Each replica maintains its own counter. The value is the sum of all replica counters. Merge takes the max of each replica's counter. Only supports increment.
- **PN-Counter** (Positive-Negative Counter): Two G-Counters -- one for increments, one for decrements. The value is P - N. Supports both increment and decrement.
- **G-Set** (Grow-only Set): Elements can be added but never removed. Merge is set union.
- **2P-Set** (Two-Phase Set): Two G-Sets -- one for additions, one for removals. An element is in the set if it is in the add-set and not in the remove-set. Once removed, an element cannot be re-added.
- **OR-Set** (Observed-Remove Set): Supports add and remove with re-add. Each add generates a unique tag. Remove removes all currently observed tags for an element. A concurrent add (with a new tag) survives a concurrent remove. This is the most complex CRDT in this challenge.
- **LWW-Register** (Last-Writer-Wins Register): Each write carries a timestamp. Merge keeps the value with the highest timestamp. Requires loosely synchronized clocks.

For each CRDT, you will implement the data structure, its operations, the merge function, and a query function. Then you will build a replication simulator that runs operations on independent replicas and merges them in random order, verifying that all replicas converge to the same state.

## Requirements

1. Implement all six CRDTs: G-Counter, PN-Counter, G-Set, 2P-Set, OR-Set, LWW-Register
2. Each CRDT must expose: operation methods (increment, add, remove, etc.), a `Merge(other)` method, and a `Query()` method that returns the current value
3. The merge function must satisfy semilattice properties: commutative, associative, idempotent. Write property-based tests that verify these properties with randomized inputs
4. Implement a `Replica` wrapper that holds a CRDT instance and a replica ID. Replicas operate independently (no shared memory)
5. Build a replication simulator that: creates N replicas (minimum 3), applies random operations to random replicas, merges replica states in random order, and verifies convergence
6. OR-Set must use unique tags (e.g., replica ID + sequence number) for add operations. Remove must only remove tags observed at the time of removal. Concurrent add and remove of the same element must result in the element being present (add-wins semantics)
7. LWW-Register must handle timestamp ties deterministically (e.g., by replica ID ordering)
8. Go implementation must use generics for type-safe sets and registers
9. Rust implementation must use traits to define a common CRDT interface (`merge`, `query`) with concrete types for each CRDT
10. Both implementations must include comprehensive tests: unit tests per CRDT, convergence tests with concurrent operations, property-based tests for semilattice laws
11. Metrics per simulation run: operations per replica, merges performed, final state comparison, convergence verification

## Hints

1. Start with G-Counter -- it is the simplest and the foundation for PN-Counter. A G-Counter is a map from replica ID to count. Increment adds 1 to the local replica's entry. Query sums all entries. Merge takes `max(local[id], remote[id])` for each replica ID. The semilattice is the pointwise maximum over a vector of natural numbers. Once you understand why this works (max is commutative, associative, and idempotent), every other CRDT follows the same pattern with different lattice structures.

2. OR-Set is the hardest. The key insight is that each `add(element)` creates a unique tag (e.g., `"replica1:42"`). The set stores pairs of `(element, tag)`. `remove(element)` removes all `(element, *)` pairs currently in the local replica. A concurrent `add(element)` on another replica creates a new tag that the removing replica has never seen, so it survives the merge. Merge is set union on the `(element, tag)` pairs. Query returns the set of elements that have at least one tag present.

3. For property-based testing in Go, generate random CRDT states and verify: `merge(a, merge(b, c)) == merge(merge(a, b), c)` (associativity), `merge(a, b) == merge(b, a)` (commutativity), `merge(a, a) == a` (idempotency). In Rust, use the `proptest` or `quickcheck` crate. These three properties are the entire correctness specification. If they hold, convergence is guaranteed by the CRDT theory.

4. The replication simulator does not need real networking. Create N CRDT instances in memory. Apply operations locally. Then merge states pairwise in random order until all replicas have merged with all others. The convergence guarantee means the final merge order does not matter. Verify by checking that all replicas return the same `Query()` result.

5. For the Rust implementation, define a trait `CRDT` with `merge(&mut self, other: &Self)` and an associated type for query results. Use `#[derive(Clone, PartialEq, Debug)]` on all CRDT types for easy comparison in tests. The `HashMap` and `HashSet` from `std::collections` are sufficient -- no external crates needed for the core implementation.

6. LWW-Register seems simple but has a subtle trap: if two replicas write at the exact same timestamp, you need a deterministic tiebreaker. Use replica ID as a secondary sort key. Without this, merge is not deterministic, and convergence fails under timestamp collisions.

## Acceptance Criteria

- [ ] All six CRDTs implemented in both Go and Rust: G-Counter, PN-Counter, G-Set, 2P-Set, OR-Set, LWW-Register
- [ ] Each CRDT's merge function satisfies commutativity, associativity, and idempotency (verified by property-based tests)
- [ ] G-Counter: increment on any replica is reflected in query after merge across all replicas
- [ ] PN-Counter: increment and decrement produce correct net count after merge
- [ ] G-Set: add-only semantics, set union merge, no element loss
- [ ] 2P-Set: removed elements cannot be re-added; add-set minus remove-set semantics correct
- [ ] OR-Set: concurrent add and remove of same element results in element present (add-wins); re-add after remove works
- [ ] LWW-Register: last write (by timestamp, then replica ID) wins after merge
- [ ] Replication simulator demonstrates convergence with 3+ replicas under concurrent operations
- [ ] Go tests pass with `-race` flag; Rust tests pass with no undefined behavior
- [ ] At least 12 test scenarios per language covering each CRDT type and edge cases
- [ ] Both implementations compile and pass all tests independently

## Going Further

- **Delta-state CRDTs**: Instead of sending full state on merge, compute and send only the delta (the part that changed since the last sync). This reduces bandwidth from O(state_size) to O(delta_size) per synchronization.
- **Map CRDT**: Implement an OR-Map where keys map to nested CRDTs (e.g., a map of counters). The merge function recursively merges nested CRDTs.
- **CRDT garbage collection**: Implement causal stability tracking to garbage-collect tombstones in 2P-Set and metadata in OR-Set, reclaiming memory from old operations.
- **Network simulation**: Add a network layer with configurable partitions and message loss. Verify convergence after partitions heal.

## Starting Points

- **Shapiro et al. (2011)**: The definitive survey of CRDTs. Read Sections 3 (state-based CRDTs) and 4 (catalogue of CRDTs). Every CRDT you implement is formally specified here with proofs of convergence.
- **Shapiro et al. (2011, techreport)**: The comprehensive technical report with 16 CRDT specifications. The OR-Set specification in Section 3.3.5 is the reference for your implementation.
- **Kleppmann (2017, DDIA Chapter 5)**: Practical context for when CRDTs are the right choice versus consensus-based replication.

## Research Resources

- [Shapiro et al.: Conflict-free Replicated Data Types (2011)](https://hal.inria.fr/inria-00609399/document) -- the foundational CRDT paper with formal definitions and convergence proofs
- [Shapiro et al.: A Comprehensive Study of CRDTs (2011, techreport)](https://hal.inria.fr/inria-00555588/document) -- 50-page catalogue of CRDTs with specifications and proofs
- [Bieniusa et al.: An Optimized Conflict-free Replicated Set (2012)](https://arxiv.org/abs/1210.3368) -- optimized OR-Set that reduces metadata overhead
- [Martin Kleppmann: Designing Data-Intensive Applications, Chapter 5](https://dataintensive.net/) -- CRDTs in the context of replication and consistency models
- [Almeida et al.: Delta State Replicated Data Types (2018)](https://arxiv.org/abs/1603.01529) -- delta-state CRDTs for efficient synchronization
- [Redis CRDT documentation](https://redis.io/docs/latest/operate/rs/databases/active-active/) -- CRDTs in production at Redis for active-active replication
- [Bartosz Sypytkowski: CRDT primer](https://www.bartoszsypytkowski.com/the-state-of-a-state-based-crdts/) -- accessible introduction with implementation guidance
