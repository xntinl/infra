# 34. 2-Phase Commit Coordinator

<!--
difficulty: advanced
category: concurrency-fundamentals
languages: [go]
concepts: [distributed-transactions, two-phase-commit, write-ahead-log, fault-tolerance, consensus]
estimated_time: 6-8 hours
bloom_level: evaluate
prerequisites: [go-basics, goroutines, channels, context-package, file-io, error-handling-patterns]
-->

## Languages

- Go (1.22+)

## Prerequisites

- Goroutines, channels, and `select` for concurrent RPC management
- `context.Context` for timeout propagation
- File I/O for write-ahead log persistence
- Error handling patterns for partial failure scenarios
- Understanding of ACID properties and distributed transaction semantics

## Learning Objectives

- **Evaluate** the failure modes of two-phase commit and the trade-offs between safety and availability
- **Implement** the prepare-vote-commit/abort protocol with correct timeout handling at each phase
- **Design** a write-ahead log that enables coordinator crash recovery without data loss
- **Analyze** how presumed-abort optimization reduces log writes and network messages in the common failure path
- **Create** a simulation framework that injects coordinator crashes, participant failures, and network partitions to validate protocol correctness

## The Challenge

Distributed transactions are the hardest problem in distributed systems. When a single operation must modify data across multiple services (debit account A, credit account B), either all services commit or all abort. Partial commitment means your system is inconsistent: money disappeared from A but never appeared in B. This is not a theoretical concern -- it is the failure mode that causes real financial discrepancies, inventory mismatches, and data corruption in production systems.

Two-Phase Commit (2PC) is the classical protocol for solving this. In Phase 1 (Prepare), the coordinator asks all participants to prepare the transaction -- lock resources and confirm readiness. In Phase 2 (Commit/Abort), if all participants voted "yes", the coordinator sends Commit; if any voted "no" or timed out, it sends Abort. The protocol guarantees atomicity: all participants reach the same decision. Every major database that supports distributed transactions (PostgreSQL with foreign data wrappers, MySQL Cluster, Oracle RAC) implements some variant of 2PC.

The challenge is fault tolerance. 2PC has a well-known vulnerability: the **blocking problem**. If the coordinator crashes after sending Prepare but before sending Commit, participants are stuck holding locks with no decision. They cannot safely commit (the coordinator might decide to abort) and they cannot safely abort (the coordinator might decide to commit). They are blocked, holding locks, until the coordinator recovers. Your implementation must handle this through a Write-Ahead Log (WAL) that records the coordinator's decisions durably before communicating them.

If a participant crashes after voting Yes, it must recover and complete the transaction according to the coordinator's decision. The participant queries the coordinator on restart to learn the outcome. If the coordinator has also crashed and recovered, the WAL is the single source of truth for the decision.

You will also implement the presumed-abort optimization (Mohan et al., 1986): if the coordinator has no record of a transaction (because it crashed before logging a commit decision), it assumes the transaction was aborted. This eliminates the need to log abort decisions and send abort acknowledgments, reducing WAL I/O by roughly half since abort is the most common outcome when failures occur.

## Key Concepts

Before implementing, understand these foundational concepts:

**The point of no return.** In 2PC, the commit decision is irrevocable once logged to the WAL. Before the WAL write, the coordinator can still decide to abort. After the WAL write, the coordinator must commit -- even if it crashes and restarts, even if some participants are unreachable. Every design decision in 2PC revolves around this single invariant.

**Unanimous vote requirement.** Unlike Raft (which uses majority quorum), 2PC requires ALL participants to vote Yes for a commit. A single No vote, a single timeout, or a single crash during the prepare phase triggers an abort. This gives each participant veto power, which is necessary for safety but makes the protocol fragile under partial failures.

**Write-Ahead Logging.** The WAL is not just a log file -- it is the mechanism that makes crash recovery possible. The protocol is: (1) log the decision, (2) fsync to disk, (3) act on the decision. If the process crashes between steps 2 and 3, the recovery procedure reads the log and re-executes step 3. If it crashes before step 2, the decision was never made (presumed abort). This pattern -- log first, act second -- appears in every durable system from databases to filesystems.

**Presumed abort.** If the coordinator crashes before writing COMMIT to the WAL, the transaction has no commit record. On recovery, the absence of a record is itself the decision: abort. This eliminates the need to explicitly log abort decisions, which reduces WAL I/O and simplifies the abort path. The trade-off is that a crash during the window between "all votes received" and "COMMIT written" always results in abort, even though all participants agreed.

## Requirements

1. Implement a `Coordinator` that manages distributed transactions across N participants
2. Phase 1: send Prepare to all participants concurrently, collect votes with a configurable timeout
3. Phase 2: if all votes are Yes, log Commit to WAL, then send Commit to all participants; if any vote is No or timeout, send Abort
4. Implement a `Participant` interface with `Prepare(txID) (Vote, error)` and `Commit(txID) error` and `Abort(txID) error`
5. Write-Ahead Log: the coordinator logs the transaction decision to a persistent file before sending Phase 2 messages
6. Coordinator crash recovery: on restart, read the WAL, re-send Commit for committed transactions, send Abort for transactions without a commit record
7. Presumed-abort optimization: transactions not found in the WAL are treated as aborted, eliminating abort log entries
8. Participant crash recovery: on restart, a participant queries the coordinator for the decision on any in-doubt transaction
9. Configurable timeouts for: prepare phase, commit phase, and participant response
10. Build a simulation framework with simulated participants that support: vote delay, vote failure, crash before/after vote, crash before/after commit
11. The coordinator must handle concurrent transactions (multiple transactions in-flight simultaneously)
12. Provide metrics: transactions committed, aborted, timed out, recovered, average commit latency

## Hints

Hints for this challenge are intentionally minimal. Study the 2PC protocol specification and WAL design independently.

1. The WAL is the single source of truth. The invariant is: if "COMMIT txID" appears in the WAL, the transaction is committed. If it does not appear, the transaction is aborted (presumed abort). This means you must `fsync` the WAL before sending any Phase 2 messages. In Go, this is `file.Sync()` after `file.Write()`. This single rule -- log before act -- is what makes crash recovery possible.

2. Model participants as goroutines communicating with the coordinator through channels or direct function calls. Each participant maintains its own state per transaction. To simulate a crash, set a flag that causes all future calls to return errors. To simulate delay, add a `time.Sleep` before responding. To simulate a crash at a specific point (e.g., after voting Yes but before receiving Commit), use the failure configuration to control exactly when the crash occurs.

3. The blocking problem in 2PC: if the coordinator crashes after logging COMMIT but before sending it to all participants, some participants may have committed and some may be waiting. The recovery procedure must re-send Commit to all participants. This is why 2PC is called a "blocking" protocol -- participants hold locks until the coordinator recovers. Understanding why this is unavoidable (Fischer-Lynch-Paterson impossibility) is as important as implementing the protocol.

4. For concurrent transactions, the coordinator needs a transaction table (map of txID to transaction state) protected by a mutex. Each transaction progresses through its phases independently. The WAL is append-only and shared across all transactions -- use a mutex on the WAL writer to serialize appends.

5. Test the most dangerous scenario: coordinator crashes after logging COMMIT for one transaction but not another. On recovery, the first transaction should be committed and the second should be aborted (presumed abort). Verify that all participants end up in the correct state for both transactions.

6. Make participant operations idempotent. During recovery, the coordinator may re-send Commit to a participant that already committed. If `Commit()` is not idempotent, recovery will corrupt data. Design the participant to treat duplicate commits as no-ops.

## Acceptance Criteria

- [ ] Happy path: all participants vote Yes, transaction commits, all participants confirm commit
- [ ] Any single participant voting No causes the entire transaction to abort across all participants
- [ ] Participant timeout during prepare phase triggers abort (no indefinite waiting)
- [ ] WAL is written and fsynced before any Phase 2 messages are sent (verify by checking file content before commit delivery)
- [ ] Coordinator crash and recovery: committed transactions are re-committed, unknown transactions are aborted
- [ ] Presumed-abort: transactions without a WAL commit record are treated as aborted without logging the abort
- [ ] Participant crash and recovery: in-doubt transactions are resolved by querying the coordinator
- [ ] Concurrent transactions (10+ in-flight) process independently without interference or deadlock
- [ ] All tests pass with `-race` flag with zero data races detected
- [ ] Simulation framework supports: participant delay, participant crash at configurable points, coordinator crash, network partition
- [ ] At least 10 test scenarios covering: happy path, single/multiple participant failure, coordinator failure at each phase, recovery, concurrent transactions, timeout cascades
- [ ] Metrics accurately report committed, aborted, timed out, and recovered transaction counts

## Going Further

Once the basic 2PC coordinator works correctly, these extensions explore the boundaries of the protocol:

- **3-Phase Commit (3PC)**: Add a pre-commit phase that breaks the blocking problem. Participants can safely abort if they have not received a pre-commit, even if the coordinator crashes.
- **Saga pattern**: Implement compensating transactions as an alternative to 2PC for long-running operations that cannot hold locks across services.
- **Paxos Commit**: Replace the single coordinator with a Paxos group so that coordinator failure does not block the protocol. This combines consensus with atomic commit.
- **Group commit optimization**: Batch multiple transaction decisions into a single WAL fsync to amortize the disk I/O cost. Measure the throughput improvement.
- **Participant-side WAL**: Add a WAL to each participant so they can independently recover their prepare/commit state without querying the coordinator.

## Starting Points

Study these references in order for a complete understanding of the protocol:

- **Jim Gray's original paper**: Read the 2PC section for the canonical protocol description. Gray invented 2PC and the WAL -- everything since is optimization and fault tolerance on top of his design.

- **Mohan et al., "Presumed Abort"**: The optimization you will implement. The key insight is that if the coordinator does not remember a transaction, it must have been aborted (because commit is only decided after a durable WAL write). This eliminates abort logging entirely.

- **CockroachDB's parallel commits blog post**: Shows how modern systems reduce 2PC latency by pipelining the prepare and commit phases. Read this after you have a working basic implementation to understand where the field has moved.

## Research Resources

- [Jim Gray: Notes on Data Base Operating Systems (1978)](https://jimgray.azurewebsites.net/papers/dbos.pdf) -- the original description of 2PC
- [Bernstein, Hadzilacos, Goodman: Concurrency Control and Recovery in Database Systems, Chapter 7](https://www.microsoft.com/en-us/research/wp-content/uploads/2016/02/ccontrol.pdf) -- formal treatment of 2PC with WAL
- [Martin Kleppmann: Designing Data-Intensive Applications, Chapter 9](https://dataintensive.net/) -- modern treatment of distributed transactions and their limitations
- [Google Spanner: TrueTime and External Consistency](https://research.google/pubs/pub39966/) -- how Google moved beyond 2PC with globally synchronized clocks
- [CockroachDB: Parallel Commits](https://www.cockroachlabs.com/blog/parallel-commits/) -- modern optimization to reduce 2PC latency
- [Jepsen: Distributed Systems Safety Analysis](https://jepsen.io/analyses) -- failure injection testing methodology for distributed protocols
