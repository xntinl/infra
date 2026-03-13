# 18. Distributed Systems Primitives

**Difficulty**: Insane

## The Challenge

Distributed systems are the backbone of modern infrastructure, yet most engineers interact with them only through opaque libraries and managed services. The algorithms that make these systems work --- consensus protocols, conflict-free replicated data types, failure detectors, and consistency verification tools --- are elegant, well-studied, and notoriously difficult to implement correctly. Getting them wrong leads to data loss, split-brain scenarios, and silent corruption. Getting them right requires understanding both the theoretical foundations and the practical engineering challenges of async network programming.

Your task is to implement two foundational distributed systems primitives from scratch in async Rust: the Raft consensus protocol and a suite of Conflict-Free Replicated Data Types (CRDTs). For Raft, you will implement leader election, log replication, and safety guarantees as described in the extended Raft paper. For CRDTs, you will implement a G-Counter (grow-only counter), an OR-Set (observed-remove set), and an LWW-Register (last-writer-wins register). Both components must operate over an in-process simulated network that supports configurable latency, message loss, message duplication, network partitions, and node crashes.

The capstone of this exercise is a verification layer: you will build a linearizability checker inspired by the Jepsen framework that records the history of operations performed on your distributed system and verifies that the observed history is consistent with a sequential specification. This means you will not just build distributed systems --- you will build the tools to prove they are correct under adversarial conditions. The entire system must be deterministic when seeded, allowing you to reproduce failures.

## Acceptance Criteria

### Raft Consensus Protocol

- [ ] Implement the Raft protocol as a library crate `raft_core` with no dependency on a specific async runtime (use trait abstractions for I/O)
- [ ] Implement the three Raft roles: Follower, Candidate, and Leader, with correct state transitions
- [ ] Implement leader election with randomized election timeouts, RequestVote RPCs, and majority quorum
- [ ] Implement log replication with AppendEntries RPCs, including:
  - Log matching property (same index and term implies same command and all prior entries)
  - Leader completeness (committed entries are present in all future leaders' logs)
  - Follower log truncation and repair when inconsistencies are detected
- [ ] Implement the Raft commit rule: an entry is committed only when stored on a majority of servers AND the entry is from the current term
- [ ] Implement a state machine interface (`trait StateMachine`) that applies committed log entries
- [ ] Handle all edge cases from Figure 2 of the Raft paper: term comparison in RPCs, vote persistence, log consistency checks, commit index advancement
- [ ] Implement pre-vote protocol extension (Section 9.6 of the Raft dissertation) to prevent disruptive servers from triggering unnecessary elections
- [ ] Implement leadership transfer extension allowing graceful leader handoff
- [ ] All persistent state (currentTerm, votedFor, log entries) must be written to a pluggable storage backend before responding to RPCs
- [ ] The Raft implementation must be fully deterministic when the timer and network are controlled

### CRDTs

- [ ] Implement a `GCounter` (grow-only counter) with:
  - `increment(node_id)` operation
  - `value()` query returning the total count
  - `merge(other)` operation combining two counters
  - Proof that merge is commutative, associative, and idempotent via property-based tests
- [ ] Implement an `ORSet<T>` (observed-remove set) with:
  - `add(element, node_id)` operation using unique tags
  - `remove(element)` operation that removes all observed tags for the element
  - `contains(element)` query
  - `elements()` query returning all current elements
  - `merge(other)` operation
  - Correct handling of concurrent add/remove (add wins if the tag was not observed by the remove)
- [ ] Implement an `LWWRegister<T>` (last-writer-wins register) with:
  - `write(value, timestamp)` operation
  - `read()` query returning the value with the highest timestamp
  - `merge(other)` operation taking the value with the higher timestamp
  - Tie-breaking strategy for equal timestamps (e.g., by node ID)
- [ ] All CRDTs must implement a common `Crdt` trait with `merge`, `state_eq`, and convergence properties
- [ ] Property-based tests (using `proptest` or `quickcheck`) proving all CRDT convergence laws:
  - Commutativity: `merge(a, b) == merge(b, a)`
  - Associativity: `merge(merge(a, b), c) == merge(a, merge(b, c))`
  - Idempotency: `merge(a, a) == a`

### Network Simulation

- [ ] Implement a `NetworkSimulator` that runs multiple Raft nodes (or CRDT replicas) in a single process using `tokio` tasks
- [ ] Support configurable network conditions:
  - Message latency (uniform or normal distribution)
  - Message loss probability (per-link configurable)
  - Message duplication probability
  - Message reordering
- [ ] Support network partitions: `partition(group_a, group_b)` isolates two groups of nodes so no messages cross the partition boundary
- [ ] Support `heal()` to restore full connectivity
- [ ] Support node crash and restart: `crash(node_id)` stops the node and `restart(node_id)` brings it back with only its persistent state
- [ ] All randomness must be seeded with a configurable `u64` seed for deterministic replay
- [ ] Support a "turbo" mode that runs as fast as possible (no real-time delays) for test throughput
- [ ] Log all network events (send, receive, drop, duplicate, delay) to a structured event log for debugging and visualization

### Linearizability Checker

- [ ] Implement a linearizability checker that takes a history of concurrent operations (each with invoke time, response time, and input/output) and determines whether the history is linearizable with respect to a sequential specification
- [ ] The sequential specification is provided as a trait: `trait SequentialSpec { fn apply(&mut self, op: Op) -> Result; fn eq(&self, other: &Self) -> bool; }`
- [ ] Implement the WGL (Wing & Gong, Linearizability) algorithm or the P-compositionality optimization for checking
- [ ] Support histories with at least 100 concurrent operations across 5 clients and 5 servers (the checker must terminate in under 60 seconds for these sizes)
- [ ] When linearizability is violated, produce a minimal counterexample showing the conflicting operations
- [ ] Write a `KVStore` sequential specification and use it to verify Raft-replicated key-value operations
- [ ] Include at least 3 deliberately broken histories (e.g., stale reads, lost writes) and verify the checker rejects them
- [ ] Include at least 3 correct histories and verify the checker accepts them

### Integration Tests

- [ ] Run a 5-node Raft cluster through the following scenario and verify correctness:
  - All nodes start, leader is elected within 2 election timeout periods
  - 100 write operations are submitted and all are committed
  - The leader is crashed, a new leader is elected, 50 more writes succeed
  - A network partition isolates the leader from the majority; the majority elects a new leader; the old leader's uncommitted entries are overwritten on rejoin
  - All nodes eventually converge to the same state machine state
- [ ] Run a 5-replica CRDT system through concurrent operations with message loss and partitions, and verify eventual convergence when connectivity is restored
- [ ] Run the linearizability checker on the Raft cluster's operation history and verify it passes
- [ ] Achieve a test that reproduces a specific bug by seed: document a seed value that triggers a partition during log replication and verify the system handles it correctly
- [ ] All tests pass under Miri where applicable (network simulation tests may need to skip Miri due to async runtime limitations)

### Raft Log Compaction

- [ ] Implement log compaction via snapshots: when the log grows beyond a configurable threshold, the state machine takes a snapshot and the log is truncated
- [ ] Implement the `InstallSnapshot` RPC: when a follower is so far behind that the leader has already compacted past its position, the leader sends the snapshot instead of individual log entries
- [ ] The snapshot must include the state machine state, the last included index, and the last included term
- [ ] After receiving a snapshot, the follower replaces its state machine state and discards all log entries up to the snapshot's last included index
- [ ] Test that a node that crashes and restarts after log compaction can still catch up via snapshot transfer

### Raft Cluster Membership Changes

- [ ] Implement single-server membership changes (add one node or remove one node at a time) as described in Section 4 of the Raft paper
- [ ] Membership changes are committed as special log entries --- the new configuration takes effect as soon as the entry is appended (not when committed)
- [ ] The leader must not accept a new membership change while a previous one is still uncommitted
- [ ] Test adding a node to a running 3-node cluster (making it a 4-node cluster) and verify that the new node catches up and participates in elections
- [ ] Test removing a node from a 5-node cluster and verify the remaining 4 nodes continue to operate correctly

### Code Quality

- [ ] The Raft implementation must be separated from the network layer via traits (no `tokio` dependency in `raft_core`)
- [ ] Use `tracing` for structured logging throughout with appropriate log levels
- [ ] All public APIs must have documentation with examples
- [ ] No `unsafe` code anywhere in the implementation
- [ ] Total test count: at least 40 tests across unit, property-based, and integration categories
- [ ] Organize the workspace into at least three crates: `raft_core` (protocol logic, no async runtime), `crdt` (CRDT implementations), and `simulation` (network simulator, integration tests)
- [ ] Error handling uses `thiserror` for library errors and `anyhow` in tests and binary targets only
- [ ] All async code uses `tokio` with the `rt-multi-thread` feature for the simulation, but `raft_core` must not depend on `tokio` at all

## Starting Points

- Study the [Raft extended paper](https://raft.github.io/raft.pdf) by Diego Ongaro --- Figure 2 is your implementation specification, and Sections 5-8 cover the core algorithm. Pay particular attention to Section 5.4 (Safety) and the proof in Section 5.4.3
- Read [Diego Ongaro's PhD dissertation](https://web.stanford.edu/~ouster/cgi-bin/papers/OngaroPhD.pdf), especially Chapter 6 (cluster membership changes), Chapter 9 (pre-vote), and Chapter 10 (leadership transfer)
- Study the [`tikv/raft-rs`](https://github.com/tikv/raft-rs) implementation in Rust --- this is a production-grade Raft library used by TiKV. Focus on `src/raft.rs` and `src/raw_node.rs` for the state machine structure
- Read the [Jepsen linearizability checker source](https://github.com/jepsen-io/knossos) and the [original WGL paper (Wing & Gong 1993)](https://www.cs.cmu.edu/~wing/publications/WingGong93.pdf) for the linearizability checking algorithm
- Study the [Maelstrom distributed systems workbench](https://github.com/jepsen-io/maelstrom) for inspiration on network simulation and testing methodology
- Read [Marc Shapiro's CRDT survey paper](https://hal.inria.fr/inria-00555588/document) "A comprehensive study of Convergent and Commutative Replicated Data Types" for formal definitions of G-Counter, OR-Set, and LWW-Register
- Study the [`crdts` crate](https://github.com/rust-crdt/rust-crdt) for a Rust CRDT implementation --- examine how they handle the OR-Set's unique tag generation
- Read the [`proptest` book](https://proptest-rs.github.io/proptest/intro.html) for property-based testing patterns, especially the section on stateful testing
- Study [Paxos Made Simple by Leslie Lamport](https://lamport.azurewebsites.net/pubs/paxos-simple.pdf) for foundational consensus understanding that informs Raft's design
- Examine the [Tokio documentation on deterministic testing](https://tokio.rs/tokio/topics/testing) and consider using `tokio::time::pause()` for controlling time in tests
- Look at the [`turmoil` crate](https://github.com/tokio-rs/turmoil) for inspiration on deterministic network simulation in Rust
- Study the [Viewstamped Replication Revisited paper](https://pmg.csail.mit.edu/papers/vr-revisited.pdf) for an alternative consensus protocol that can inform your understanding of Raft's design decisions
- Read [Testing Distributed Systems](https://asatarin.github.io/testing-distributed-systems/) --- a curated list of resources on testing distributed systems, including techniques for deterministic simulation
- Study the [FoundationDB testing methodology](https://apple.github.io/foundationdb/testing.html) which uses deterministic simulation to find bugs --- this is the gold standard for distributed systems testing

## Hints

1. Start with the CRDTs --- they are simpler and will get you comfortable with the merge semantics and property-based testing before tackling consensus. A working G-Counter can be implemented in under 50 lines; use it to validate your test infrastructure.

2. For the G-Counter, use a `HashMap<NodeId, u64>` where each node only increments its own entry. Merge takes the element-wise maximum. This is trivially commutative, associative, and idempotent.

3. The OR-Set is the hardest CRDT. The key insight: each `add` operation generates a globally unique tag (e.g., `(node_id, sequence_number)`). The set stores `(element, tag)` pairs. A `remove` operation records all tags currently associated with the element. On merge, a pair `(element, tag)` is in the result if it is in either replica AND the tag was not observed and removed by the other replica.

4. For Raft, structure your code around a `RaftNode` struct that holds the state and a `step(message: Message) -> Vec<Message>` method that processes one message at a time and returns messages to send. This "step function" architecture (used by `etcd/raft` and `tikv/raft-rs`) decouples the protocol logic from I/O entirely.

5. Raft state transitions to get right first: Follower receives no heartbeat -> becomes Candidate -> sends RequestVote -> receives majority -> becomes Leader -> sends AppendEntries heartbeats. Get this loop working before adding log replication.

6. The most common Raft bug is in commit index advancement. The leader's commit index advances to the highest index N such that a majority of `matchIndex[i] >= N` AND `log[N].term == currentTerm`. The term check is critical --- read Section 5.4.2 of the paper carefully.

7. For the network simulator, use `tokio::sync::mpsc` channels between nodes. The simulator sits in the middle: all messages go through a central router that can delay, drop, duplicate, or reorder them. Use a `BinaryHeap` ordered by delivery time for deterministic message scheduling.

8. To make the simulation deterministic, replace all calls to `Instant::now()` with a virtual clock. Use `tokio::time::pause()` and `tokio::time::advance()` in tests, or build your own `Clock` trait. All random number generation must go through a seeded `rand::rngs::StdRng`.

9. For the linearizability checker, represent each operation as an interval `[invoke_time, response_time]` with input and output. The WGL algorithm works by trying to find a valid sequential ordering: pick any operation whose invoke time is before the current "linearization point," apply it to the sequential spec, check if the output matches, and recurse. Backtrack if no valid ordering exists.

10. The WGL algorithm is NP-complete in the general case, but P-compositionality helps: if your data structure's operations can be partitioned by key (like a key-value store), you can check linearizability per key independently. This reduces the complexity dramatically.

11. For partition testing, create a helper: `with_partition(simulator, [1,2], [3,4,5], || async { /* test body */ })`. The partition should affect both directions --- nodes in group A cannot send to or receive from nodes in group B.

12. When testing Raft under partitions, the critical scenario is: leader is in the minority partition, submits entries that get replicated to the minority but NOT the majority. The majority elects a new leader and commits new entries. When the partition heals, the old leader must step down and truncate its uncommitted entries. This tests the log matching property.

13. For the persistent storage trait, define `trait Storage { async fn save_term_and_vote(&mut self, term: u64, voted_for: Option<NodeId>); async fn append_entries(&mut self, entries: Vec<LogEntry>); async fn truncate_after(&mut self, index: u64); }`. In tests, use an in-memory implementation. The key property: the node must not respond to an RPC until the relevant state is persisted.

14. Use `tracing` spans to tag every log message with the node ID and current term. This makes debugging multi-node scenarios vastly easier. Example: `let _span = tracing::info_span!("raft", node = %self.id, term = self.current_term).entered();`

15. For property-based CRDT tests, generate random sequences of operations and random merge orderings. After applying all operations through different merge orders, all replicas must converge to the same state. Use `proptest`'s `prop_compose!` to generate operation sequences.

16. The LWW-Register's timestamp should use a hybrid logical clock (HLC) rather than wall clock time. An HLC combines a physical timestamp with a logical counter, providing monotonicity even when clocks are skewed. However, for this exercise, a simple `(u64_timestamp, node_id)` pair with lexicographic ordering is sufficient.

17. When implementing leadership transfer, the outgoing leader sends a `TimeoutNow` message to the target follower, which immediately starts an election without waiting for the election timeout. The outgoing leader stops accepting new client requests during the transfer.

18. For the counterexample in the linearizability checker, record the decision tree during the search. When no valid linearization is found, extract the minimal set of operations that conflict. Present them as: "Operation A (read key X, got value 1) at time [3, 7] cannot be linearized with Operation B (write key X, value 2) at time [1, 5] and Operation C (read key X, got value 2) at time [2, 6]."

19. Test your linearizability checker first with hand-crafted histories before connecting it to the Raft cluster. A simple linearizable history: `{invoke(write(1)), ok(write(1)), invoke(read()), ok(read() -> 1)}`. A simple non-linearizable history: `{invoke(write(1)), ok(write(1)), invoke(read()), ok(read() -> 0)}` (stale read).

20. Consider building a simple visualization of the Raft cluster state over time. Even just printing a timeline of terms, leaders, and committed entries to a file can be invaluable for debugging. Format it as a TSV or JSON that can be loaded into a spreadsheet or custom tool.

21. For snapshot implementation, the state machine trait should include `fn snapshot(&self) -> Vec<u8>` and `fn restore(&mut self, snapshot: &[u8])`. The Raft node triggers a snapshot when `log.len() > snapshot_threshold` and then truncates the log. Store the snapshot alongside the persistent state.

22. The `InstallSnapshot` RPC is the most complex Raft RPC to implement. The leader sends chunks of the snapshot (for large snapshots, you may need to chunk). The follower accumulates chunks and, once complete, replaces its state. During this process, the follower must not accept AppendEntries for indices covered by the snapshot.

23. For membership changes, the subtlety is that the new configuration takes effect immediately on each server when the configuration entry is appended to its log (not when committed). This means there is a brief period where different servers may disagree on the cluster membership. The single-server change restriction ensures that any majority under the old configuration overlaps with any majority under the new configuration, preventing split-brain.

24. When implementing the `ORSet`, a common mistake is confusing "observed" with "in the set." The remove operation captures the set of tags currently associated with the element at the removing replica. On merge, a tag survives if it is in either replica AND was not captured by a remove on the other replica. The formal definition uses the "add set" minus the "remove set" per replica, merged by taking the union of adds and the union of removes.

25. For the network simulator's "turbo" mode, use `tokio::time::pause()` at the start of the test and then advance time programmatically. Each tick of the simulation advances the virtual clock. This allows thousands of election cycles to happen in milliseconds of wall clock time.

26. A subtle Raft correctness requirement: when a candidate or leader discovers that its term is stale (receives a message with a higher term), it must immediately revert to follower state. This is specified in the "Rules for Servers" section of Figure 2. Missing this causes liveness bugs where stale leaders continue to send AppendEntries.

27. For the CRDT simulation, use a gossip protocol: each replica periodically sends its state to a random peer, and the peer merges it. With message loss, convergence takes longer but is still guaranteed (as long as the gossip graph is eventually connected). Test that convergence happens within O(n log n) gossip rounds for n replicas.

28. When implementing the linearizability checker's backtracking search, use memoization to avoid re-exploring states you have already visited. The state is the set of operations that have been linearized so far plus the current sequential state. Since operations have unique IDs, you can represent the "linearized set" as a bitmask for small histories.

29. For the integration test that reproduces a bug by seed, implement a test harness that accepts a seed parameter: `#[test] fn regression_seed_42() { run_scenario(42); }`. During development, run randomized tests in a loop: `for seed in 0..10000 { run_scenario(seed); }`. When a seed fails, add it as a named regression test.

30. Consider implementing a `PNCounter` (positive-negative counter) as a bonus CRDT. It is built from two G-Counters: one for increments and one for decrements. The value is `increments.value() - decrements.value()`. This demonstrates how CRDTs compose and is trivial once you have the G-Counter working.

31. For the `Clock` trait used by Raft, define: `trait Clock { fn now(&self) -> Instant; fn sleep(&self, duration: Duration) -> impl Future<Output = ()>; }`. In production, this wraps `tokio::time`. In tests, it wraps a virtual clock that advances only when told to. This separation is what makes deterministic testing possible.

32. Test Raft leader election under high contention: start 5 nodes simultaneously and verify that exactly one leader is elected within a bounded number of election rounds. The randomized election timeout should prevent livelock, but you should test with tight timeout ranges (e.g., 150ms-300ms) to exercise the contention path.
