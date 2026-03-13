# 24. Consistent Prefix Reads

<!--
difficulty: insane
concepts: [consistent-prefix, causal-consistency, session-guarantee, monotonic-reads, monotonic-writes, read-your-writes, snapshot-isolation]
tools: [go]
estimated_time: 2h
bloom_level: create
prerequisites: [vector-clocks, quorum-based-replication, cqrs-eventual-consistency]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of vector clocks and causal ordering
- Completed quorum-based replication (exercise 23)
- Familiarity with consistency models (eventual, causal, strong)

## Learning Objectives

- **Create** a system that enforces consistent prefix reads and session guarantees
- **Analyze** the spectrum of consistency models between eventual and strong consistency
- **Evaluate** the implementation complexity and performance cost of each session guarantee

## The Challenge

In a distributed system with multiple replicas, a reader might see updates out of order: they see the reply to a question before the question itself, or they see a later version of data after having seen an earlier version on a different replica. Consistent prefix reads guarantee that if a sequence of writes happens in some order, any reader will see them in that order (though they may not see all of them).

Session guarantees provide consistency within a client session: read-your-writes (you see your own writes), monotonic reads (you never see time go backward), monotonic writes (your writes are applied in order), and writes-follow-reads (a write that depends on a read is ordered after that read).

Build a replicated system that enforces these session guarantees. Use vector clocks or version vectors to track causal dependencies, and implement per-session consistency enforcement.

## Requirements

1. Implement a replicated data store with multiple replicas that process writes asynchronously (eventual consistency as the baseline)
2. Implement consistent prefix reads: readers see writes in the order they were applied. If write A happened before write B, no reader sees B without seeing A first.
3. Implement the four session guarantees:
   - **Read Your Writes**: after a client writes value V, subsequent reads from the same session return V (or a later version)
   - **Monotonic Reads**: if a client reads version X, subsequent reads in the same session return X or later (never earlier)
   - **Monotonic Writes**: writes from a single session are applied in order across all replicas
   - **Writes Follow Reads**: if a client reads value V and then writes W, all replicas that apply W have already applied the write that produced V
4. Use vector clocks or version vectors to track causal dependencies between operations
5. Implement session state tracking: each client session maintains a vector clock representing the latest state it has observed
6. Implement replica selection: route reads to replicas that are caught up to at least the session's observed state
7. Demonstrate violations: show what goes wrong without each guarantee (read another session's write in wrong order, see time go backward, etc.)
8. Benchmark the overhead of session guarantees: additional latency and metadata per operation

## Hints

- Session state: each session maintains a `sessionClock` (vector clock). After a read, update `sessionClock` to `max(sessionClock, readClock)`. After a write, update `sessionClock` to `max(sessionClock, writeClock)`.
- Read Your Writes: when a client reads, select a replica whose state vector clock is >= the session's clock. If no replica is caught up, either wait or read from the write master.
- Monotonic Reads: track the highest version seen per key. Reject reads that would return an older version.
- Consistent Prefix: assign a global sequence number or use vector clocks to order writes. Replicas apply writes in order. Readers are restricted to a prefix of the sequence.
- For replica selection, each replica publishes its current state version (vector clock or sequence number). The session selects replicas that are at least as current as the session's requirements.
- Causal consistency is achieved when all four session guarantees hold.
- COPS (Clusters of Order-Preserving Servers) and Eiger are academic systems that implement causal consistency at scale. Study them for design inspiration.

## Success Criteria

1. Consistent prefix reads never show an effect without its cause
2. Read Your Writes: a session always sees its own writes
3. Monotonic Reads: a session never sees time go backward
4. Monotonic Writes: a session's writes are applied in order on all replicas
5. Writes Follow Reads: a write that depends on a read is ordered after the read's write
6. Violations are clearly demonstrated without each guarantee
7. Vector clocks correctly track causal dependencies
8. The overhead of session guarantees is measured and documented

## Research Resources

- [Session Guarantees for Weakly Consistent Replicated Data (Terry et al.)](https://dl.acm.org/doi/10.1145/190163.190169) -- the foundational paper defining the four guarantees
- [Designing Data-Intensive Applications, Chapter 5](https://dataintensive.net/) -- consistency models and session guarantees
- [Causal Consistency (Jepsen)](https://jepsen.io/consistency/models/causal) -- formal definitions
- [COPS: Causal+ Consistency](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf) -- causal consistency at scale
- [Consistency Models (Viotti & Vukolic)](https://arxiv.org/abs/1512.00168) -- comprehensive survey of consistency models

## What's Next

Congratulations -- you have completed the Distributed Systems Fundamentals section. You have built implementations of the core algorithms and patterns that underpin modern distributed systems: consensus (Raft, Paxos), replication (quorums, anti-entropy), coordination (locks, sagas, 2PC), data structures (CRDTs, Merkle trees, consistent hashing), and consistency models (session guarantees, causal consistency). Continue to the capstone projects to apply these fundamentals in larger systems.

## Summary

- Consistent prefix reads guarantee that writes are seen in the order they occurred
- Session guarantees provide per-client consistency: read-your-writes, monotonic reads, monotonic writes, writes-follow-reads
- Vector clocks track causal dependencies between operations
- Session state (a vector clock) records the latest state observed by each client
- Replica selection routes reads to replicas caught up to the session's requirements
- Causal consistency is achieved when all four session guarantees are enforced
- This represents a practical middle ground between eventual and strong consistency
