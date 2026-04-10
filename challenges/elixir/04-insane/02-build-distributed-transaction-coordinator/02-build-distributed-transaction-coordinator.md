# 2. Build a Distributed Transaction Coordinator

**Difficulty**: Insane

## Prerequisites
- Mastered: All Elixir/OTP intermediate and advanced concepts (GenServer, ETS, distributed Erlang, process monitoring)
- Mastered: Database internals — MVCC, locking protocols, WAL, ACID guarantees
- Familiarity with: Two-phase locking (2PL), two-phase commit (2PC), deadlock detection algorithms
- Reading: Jim Gray's original 2PC paper, the Spanner paper (Corbett et al., 2012), the PostgreSQL documentation on two-phase commit

## Problem Statement

Build a distributed transaction coordinator in Elixir/OTP that provides ACID semantics across multiple independent data partitions. Each partition is a separate Elixir node running an embedded key-value store. Your coordinator must orchestrate transactions that span partitions without relying on any external database engine.

Your system must implement:
1. A two-phase commit protocol where your coordinator drives the prepare/commit/abort decision and each participant votes independently
2. Crash recovery for the coordinator: if the coordinator process dies after sending `prepare` but before sending `commit`, the system must be able to recover the correct decision when the coordinator restarts — no participant should be left in an indefinite blocking state
3. Snapshot isolation for concurrent transactions: readers must not block writers; writers must not block readers; a transaction sees a consistent snapshot of committed data as of its start timestamp
4. Deadlock detection via a distributed wait-for graph: when a cycle is detected (T1 waits for T2 waits for T1), abort the youngest transaction in the cycle within 1 second of the cycle forming
5. A banking simulation with 1,000 accounts distributed across 3 partitions: concurrent transfer transactions must preserve the invariant that the total balance across all accounts never changes
6. Write-ahead logging on each participant: every prepare decision is written to WAL before voting YES; recovery replays the WAL to restore pre-crash state

## Acceptance Criteria

- [ ] **2PC happy path**: A transaction spanning 3 partitions commits atomically; all participants reflect the new values; no partial commit is observable
- [ ] **Coordinator crash after prepare**: Kill the coordinator after all participants have voted YES but before `commit` is sent; restart the coordinator; it reads its WAL, determines the commit decision, and drives all participants to commit — no participant is stuck in `prepared` state permanently
- [ ] **Participant crash during prepare**: Kill one participant before it votes; the coordinator receives a missing vote (timeout), aborts the transaction, and all other participants roll back — data is as if the transaction never started
- [ ] **Snapshot isolation — no dirty reads**: Transaction T1 modifies a key but has not committed; Transaction T2 reads the same key and sees the pre-T1 value; after T1 commits, a new T3 reads the key and sees the T1 value
- [ ] **Snapshot isolation — no lost updates**: Two transactions concurrently increment the same counter; the final value must reflect both increments (first-committer-wins or write-write conflict detection and retry)
- [ ] **Deadlock detection**: Construct a scenario where T1 holds lock on key A and waits for key B (held by T2); T2 holds key B and waits for key A (held by T1); confirm one transaction is aborted within 1 second and the other completes
- [ ] **Atomicity — banking invariant**: Run 10,000 concurrent transfer transactions across 1,000 accounts on 3 partitions; after all transactions complete, sum all balances and confirm it equals the initial total
- [ ] **Durability**: Commit a transaction; immediately kill all nodes; restart all nodes; confirm the committed values are present and the WAL is replayed correctly
- [ ] **Throughput**: Sustain 1,000 distributed transactions per second on a 3-node cluster on localhost, with each transaction spanning at least 2 partitions

## What You Will Learn
- Why 2PC is a blocking protocol and what failure scenarios leave participants permanently blocked
- The exact recovery algorithm that makes 2PC safe: what the coordinator writes to WAL, when it writes it, and what it reads on restart
- How MVCC (Multi-Version Concurrency Control) enables snapshot isolation without readers blocking writers
- The difference between optimistic and pessimistic concurrency control and which is appropriate for which workloads
- Why deadlock detection in a distributed system requires a distributed wait-for graph, not a local one
- How write-ahead logging guarantees durability — specifically the "write the log record before the data page" rule (WAL protocol)
- The fundamental impossibility result of 3PC and why distributed systems still use 2PC despite its blocking nature
- How Spanner avoids 2PC blocking using TrueTime — and why that approach is impractical without atomic clocks

## Hints

This exercise is intentionally sparse. You are expected to:
- Understand what "blocking protocol" means in the context of 2PC before writing a single line of code — the coordinator's WAL is your escape hatch
- Design your MVCC storage layer first: what does a "version" look like, how do you track which snapshot a transaction sees, when do you garbage-collect old versions
- Think carefully about lock granularity: row-level vs. partition-level locking; finer granularity improves concurrency but increases coordinator overhead
- The deadlock detector must run on a timer, not in the critical path of lock acquisition — build it as a separate process that samples the wait-for graph
- For the banking simulation, use property-based testing (StreamData) to generate random transfer sequences and assert the invariant after each batch

## Reference Material (Research Required)
- Gray, J. & Lamport, L. — *Consensus on Transaction Commit* — the formal analysis of 2PC and Paxos Commit as an alternative
- Gray, J. & Reuter, A. — *Transaction Processing: Concepts and Techniques* (1992) — chapters on 2PC, locking, and recovery are the canonical reference
- Corbett, J. et al. (2012). *Spanner: Google's Globally Distributed Database* — study how TrueTime enables external consistency without 2PC blocking
- PostgreSQL source code — `src/backend/access/transam/twophase.c` — the reference implementation of coordinator-side 2PC with WAL
- Bernstein, P. & Goodman, N. (1983). *Multiversion Concurrency Control* — the original MVCC paper; snapshot isolation is defined here

## Difficulty Rating
★★★★★★

## Estimated Time
4–6 weeks for an experienced Elixir developer with database internals background
