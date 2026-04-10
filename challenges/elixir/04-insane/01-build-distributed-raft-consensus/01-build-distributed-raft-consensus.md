# 1. Build a Distributed Raft Consensus Engine

**Difficulty**: Insane

## Prerequisites
- Mastered: All Elixir/OTP intermediate and advanced concepts (GenServer, Supervisor trees, distributed Erlang, ETS, persistent term, :pg, :net_kernel)
- Mastered: Distributed systems fundamentals — clock synchronization, network partitions, CAP theorem, linearizability vs serializability
- Familiarity with: Consensus algorithms, replicated state machines, write-ahead logging, Paxos history
- Reading: The Raft paper in full (Ongaro & Ousterhout, 2014) — not a summary, the actual paper

## Problem Statement

Build a complete implementation of the Raft consensus algorithm using native BEAM clustering (`:net_kernel`, `:erpc`, `Node.connect/1`). Do not use any distributed systems libraries. No third-party consensus layers. Every byte of the protocol is yours to own.

Your system must implement:
1. Leader election via randomized election timeouts, vote RequestVote RPCs, and quorum counting (N/2 + 1)
2. Log replication via AppendEntries RPCs — the leader must replicate entries to a majority of followers before acknowledging the client
3. Safety invariants: no two nodes may believe they are leader in the same term; election safety must hold even under message delay or reorder
4. Log compaction: when the replicated log exceeds 10,000 entries, take a snapshot of the state machine and truncate the log suffix — followers must be able to install snapshots via InstallSnapshot RPC
5. A key-value state machine layered on top of Raft: `get/2`, `put/3`, `delete/2` with full linearizability
6. Client session management with exactly-once semantics: a client retrying a command must not cause duplicate application to the state machine
7. Joint consensus membership changes: safely add or remove a node from the cluster without any window where two disjoint majorities could simultaneously exist
8. An observable cluster: structured log output that shows term numbers, role transitions, log index progress, and commit index advancement

## Acceptance Criteria

- [ ] **Leader election**: On cluster start with 5 nodes, a single leader is elected within 10 seconds; election completes with a quorum of votes (N/2 + 1 = 3); the leader broadcasts its authority via AppendEntries heartbeats
- [ ] **Log replication**: A `put/3` call returns `:ok` only after the entry has been persisted to a majority of nodes; followers that were temporarily partitioned catch up upon reconnection
- [ ] **Safety — election**: Kill the leader; observe a new election; confirm via term numbers that the old leader never issues commands in the new term (no split-brain, ever)
- [ ] **Safety — log**: Apply a sequence of 1000 commands; verify that all 5 nodes have identical log and state machine state after convergence
- [ ] **Log compaction**: Drive the log past 10,000 entries; confirm that snapshot is taken, log is truncated, and a freshly joined node installs the snapshot instead of replaying the full log
- [ ] **Network partition — quorum preserved**: With 5 nodes, isolate 2 into a minority partition; confirm the 3-node majority continues to accept writes; confirm the minority rejects writes (no quorum); after healing, minority catches up
- [ ] **Leader failover**: Kill the current leader; measure time to new leader acknowledgment; assert it is under 5 seconds on a local network
- [ ] **Key-value linearizability**: Run a concurrent benchmark with 10 clients issuing interleaved gets and puts; verify results are linearizable (no stale reads after a confirmed write)
- [ ] **Exactly-once semantics**: A client issues a `put` that times out and retries; confirm the state machine applies the command exactly once, not twice
- [ ] **Membership change**: Add a 6th node to a running 5-node cluster; confirm the cluster continues to serve requests throughout; confirm the new node participates in elections and log replication after joining
- [ ] **Benchmark**: Sustain 10,000 linearizable writes per second on a 3-node cluster running on localhost; measure p50, p95, p99 latency

## What You Will Learn
- How Raft separates leader election, log replication, and safety into independently verifiable sub-problems
- Why randomized timeouts are sufficient (and why they work in practice despite being non-deterministic)
- The subtlety of the "Leader Completeness" property and why a new leader must not commit entries from previous terms directly
- How joint consensus avoids the two-disjoint-majority problem that plagues naive membership change schemes
- How to build exactly-once delivery on top of an at-least-once RPC layer
- The performance trade-offs between fsync durability and throughput in write-ahead logging
- How BEAM's distribution primitives (`:erpc`, node monitoring, `:net_kernel`) map onto the RPC layer Raft assumes
- What linearizability means formally and how to test for it using violation-detection tools like Knossos

## Hints

This exercise is intentionally sparse. You are expected to:
- Read the Raft paper (extended version, not the conference version) — pay special attention to Figure 2; it is the complete specification
- Study what "committed" means versus "applied" and why those are different states
- Design your own RPC layer before writing a single GenServer — Raft's correctness depends on the semantics of your message-passing substrate
- Think carefully about timer management: election timeouts must reset on heartbeat receipt; AppendEntries and RequestVote must not race
- Understand why Raft forbids committing log entries from previous terms by index alone — this is the most commonly misimplemented rule
- Implement a simulation harness first: inject message drops, delays, and partitions before testing on real BEAM nodes; bugs surface much earlier
- For the state machine layer, treat it as a pure function: `apply(command, state) -> {reply, new_state}`; never let it have side effects that bypass the log

## Reference Material (Research Required)
- Ongaro, D. & Ousterhout, J. (2014). *In Search of an Understandable Consensus Algorithm (Extended Version)* — do NOT look for tutorials, study the paper directly
- Ongaro, D. (2014). *Consensus: Bridging Theory and Practice* (PhD dissertation) — chapters 3–6 cover correctness proofs and membership change in depth
- etcd source code (Go) — the reference implementation of Raft; study the `raft/` package structure, not the wrapper
- TiKV Raft implementation (Rust) — alternative implementation with excellent comments on subtle correctness points
- Kingsbury, K. — *Jepsen analyses of distributed databases* (https://jepsen.io) — understand how linearizability violations are detected
- Heidi Howard — *Flexible Paxos* — context for understanding why majority quorums are sufficient but not necessary

## Difficulty Rating
★★★★★★

## Estimated Time
4–6 weeks for an experienced Elixir developer with prior distributed systems exposure
