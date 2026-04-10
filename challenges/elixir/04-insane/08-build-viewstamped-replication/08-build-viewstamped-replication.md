# 8. Build the Viewstamped Replication Protocol

**Difficulty**: Insane

## Prerequisites
- Mastered: All Elixir/OTP intermediate and advanced concepts (GenServer, distributed Erlang, process monitoring, ETS, :erpc)
- Mastered: Consensus protocol theory — safety vs liveness properties, quorum systems, replicated state machines, the relationship between VR, Paxos, and Raft
- Familiarity with: The original VR paper (Liskov & Cowling, 2012), how VR differs from Raft in its view-change protocol
- Reading: *Viewstamped Replication Revisited* (Liskov & Cowling, 2012 MIT Technical Report) — the complete revised version, not the 1988 original

## Problem Statement

Implement the Viewstamped Replication (VR) protocol in Elixir/OTP. VR is a primary-backup replication protocol that predates Paxos and shares the same theoretical foundations. Unlike Raft, VR does not elect leaders by log comparison — it uses a separate view-change sub-protocol that is worth studying in its own right.

Your system must implement:
1. The normal operation protocol: the primary accepts client requests, assigns them an op-number, sends `PREPARE` to all replicas, waits for f+1 acknowledgments (where f is the failure threshold), adds the op to the commit log, and replies to the client; replicas apply ops to their state machine only up to the commit number broadcast by the primary
2. The view-change protocol: when a replica suspects the primary has failed (via timeout), it broadcasts `START_VIEW_CHANGE`; when it receives f+1 `START_VIEW_CHANGE` messages for the same view, it sends `DO_VIEW_CHANGE` to the replica that will be the new primary (determined by `view_number mod num_replicas`); the new primary collects f+1 `DO_VIEW_CHANGE` messages, selects the log with the highest op-number, broadcasts `START_VIEW`, and resumes normal operation
3. The recovery protocol: a replica that crashes and restarts (losing in-memory state) sends `RECOVERY` to all replicas; replicas reply with their current view number, op log, and commit number; the recovering replica integrates the responses to reconstruct a consistent state and rejoins the group
4. A replicated key-value state machine: `get(key)`, `put(key, value)`, `delete(key)` operations are submitted via the VR client protocol and applied to the state machine in op-number order on all replicas
5. Exactly-once semantics via nonces: each client request carries a client ID and a nonce; if a client retries a request (due to timeout), the primary recognizes the nonce and returns the cached reply without re-applying the operation to the state machine
6. An epoch-based view numbering scheme: every view change increments the view number; replicas reject messages from stale views (lower view number than their current view) to prevent old primaries from interfering

## Acceptance Criteria

- [ ] **Normal operation — commit**: Client sends `put("balance", 100)` to the primary; primary replicates to f+1 replicas, commits, replies `:ok`; all replicas have `"balance" => 100` in their state machine
- [ ] **Linearizability**: Under concurrent clients issuing interleaved `get`s and `put`s, all reads return values consistent with a serial execution of all operations — no client reads a value that has not yet been committed
- [ ] **View change — primary failure**: Kill the primary (5-node cluster, f=2); within 10 seconds, a new primary is established in a higher view number; the new primary correctly inherits the log (including uncommitted ops from the dead primary) and continues serving requests
- [ ] **View change — log selection**: Simulate a scenario where different replicas have different op-numbers at the time of view change (due to partial replication); confirm the new primary selects the replica with the highest op-number as the authoritative log, not an arbitrary one
- [ ] **Recovery**: Kill a replica, lose its in-memory state, restart it; the recovering replica sends `RECOVERY`, receives responses, integrates them, and rejoins the group in the correct view with the correct log and commit number — without human intervention
- [ ] **Exactly-once via nonce**: Client sends `put("x", 1)` (nonce=42) and times out before receiving a reply; client retries with the same nonce; confirm the state machine applies the operation exactly once and the client receives a valid reply on retry
- [ ] **Stale view rejection**: Send a `PREPARE` message with a view number lower than the replica's current view number; confirm the replica rejects it (no state change) and responds with its current view number for the sender to update
- [ ] **Benchmark**: Sustain 5,000 linearizable operations per second on a 5-replica cluster on localhost (f=2); measure p50, p95, p99 latency for `put` operations

## What You Will Learn
- How VR's view-change protocol differs from Raft's leader election: VR uses a two-phase view-change with explicit log reconciliation, while Raft uses log comparison during election voting
- Why the replica that will be the new primary is determined deterministically (`view mod N`) rather than by a vote — and how this simplifies the view-change protocol while still being safe
- The exact log reconciliation rule in the `DO_VIEW_CHANGE` phase: why the new primary must adopt the log from the replica with the highest op-number, not the highest commit number
- How the recovery protocol avoids the need for persistent state (VR is a memory-resident protocol by design) — and the trade-offs this creates
- The relationship between VR, Paxos, and Raft: all three solve the same problem (replicated state machine consensus); studying all three illuminates what is fundamental vs what is incidental to each design
- How exactly-once semantics at the protocol level differ from application-level idempotency — and why the nonce mechanism in VR is sufficient for the former but not a substitute for the latter
- How to verify linearizability using a checker like Knossos or Elle and what a linearizability violation trace looks like

## Hints

This exercise is intentionally sparse. You are expected to:
- Read Liskov & Cowling (2012) in its entirety — Figure 1 (normal operation), Figure 2 (view change), and Figure 3 (recovery) are the complete protocol specification; implement them exactly
- Understand the difference between `op-number` (position in the primary's log) and `commit-number` (highest op applied to the state machine); these are separate concepts and must not be conflated
- The view-change protocol has a subtle correctness requirement: the new primary must not start serving requests until it has received f+1 `DO_VIEW_CHANGE` messages and broadcast `START_VIEW` — implement this gating precisely
- For testing, implement an event-sourced log of all messages sent and received; this will be invaluable when debugging view-change bugs that only appear under specific timing conditions
- Compare your implementation to Raft (or your implementation from exercise 01) — the comparison will reveal the design choices each protocol makes explicitly versus implicitly

## Reference Material (Research Required)
- Liskov, B. & Cowling, J. (2012). *Viewstamped Replication Revisited* — MIT Technical Report MIT-CSAIL-TR-2012-021 — the primary source; do NOT use summaries or blog posts as a substitute
- Liskov, B. (1988). *Viewstamped Replication: A New Primary Copy Method to Support Highly-Available Distributed Systems* — the original 1988 paper; compare it to the 2012 revision to understand what changed
- Ongaro, D. (2014). *Consensus: Bridging Theory and Practice* — Chapter 2 compares VR, Paxos, and Raft in depth; this comparison is essential for understanding what is fundamental about each
- Lamport, L. (1998). *The Part-Time Parliament* — the original Paxos paper; reading it alongside VR reveals the deep equivalence between the two protocols

## Difficulty Rating
★★★★★★

## Estimated Time
4–6 weeks for an experienced Elixir developer who has studied at least one other consensus protocol in depth
