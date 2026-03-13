# 16. Two-Phase Commit

<!--
difficulty: insane
concepts: [two-phase-commit, 2pc, distributed-transactions, coordinator, participant, prepare-commit, abort, blocking-protocol, recovery-log]
tools: [go]
estimated_time: 2h
bloom_level: create
prerequisites: [distributed-locking, raft-log-replication]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of distributed locking and consensus
- Familiarity with ACID transactions

## Learning Objectives

- **Create** a two-phase commit coordinator and participant implementation
- **Analyze** the blocking problem and how coordinator failure causes participants to block indefinitely
- **Evaluate** 2PC against alternatives (3PC, Saga) for distributed transaction coordination

## The Challenge

Two-Phase Commit (2PC) is the classic protocol for distributed transactions. A coordinator asks all participants to prepare (Phase 1), and if all agree, tells them to commit (Phase 2). If any participant votes to abort, the coordinator tells everyone to abort. 2PC guarantees atomicity: either all participants commit or all abort.

The critical weakness is blocking: if the coordinator fails after collecting votes but before sending the commit/abort decision, participants that voted to prepare are stuck -- they cannot commit (they do not know if all participants agreed) and cannot abort (the coordinator might have decided to commit). They must wait for the coordinator to recover.

Implement a complete 2PC system with write-ahead logging for recovery, timeout handling, and demonstrate the blocking problem.

## Requirements

1. Implement a `Coordinator` that manages distributed transactions across multiple participants
2. Implement Phase 1 (Prepare): coordinator sends Prepare to all participants, collects votes (Yes/No)
3. Implement Phase 2 (Commit/Abort): if all voted Yes, send Commit; if any voted No, send Abort
4. Implement `Participant` with write-ahead logging: log Prepare, Yes/No vote, Commit/Abort decision before acting
5. Implement coordinator recovery: on restart, the coordinator reads its log to determine the fate of in-progress transactions
6. Implement participant recovery: on restart, a participant that voted Yes but has not received a decision must ask the coordinator for the outcome
7. Demonstrate the blocking problem: crash the coordinator after collecting votes, show that prepared participants are stuck
8. Implement a timeout mechanism: if a participant does not receive a decision within a timeout, it can ask other participants (cooperative termination protocol)
9. Write tests covering: successful commit, abort (one participant votes No), coordinator failure, participant failure, recovery

## Hints

- The write-ahead log is critical for recovery. Before sending any message, log the intent. Before acting on a received message, log the receipt. On recovery, replay the log.
- The coordinator log entries: `BEGIN txid`, `PREPARE txid`, `COMMIT txid` or `ABORT txid`. The decision is durable once the COMMIT or ABORT entry is logged.
- Participant log entries: `VOTE_YES txid` or `VOTE_NO txid`, then `COMMITTED txid` or `ABORTED txid`.
- A participant that has not voted can unilaterally abort. A participant that voted Yes must wait for the coordinator's decision.
- The cooperative termination protocol: if a participant times out waiting for a decision, it asks other participants. If any participant has already committed, the answer is Commit. If any has aborted, the answer is Abort. If all are in the "voted Yes" state, they are all stuck (blocking).
- Simulate the coordinator and participants as goroutines with channel-based communication. Use files or in-memory logs.
- 3PC (Three-Phase Commit) adds a Pre-Commit phase to reduce blocking. Consider implementing it as an extension.

## Success Criteria

1. Successful transactions commit atomically across all participants
2. If any participant votes No, the transaction is aborted everywhere
3. Write-ahead logging enables correct recovery after coordinator or participant failure
4. The blocking problem is clearly demonstrated: prepared participants cannot proceed without the coordinator
5. The cooperative termination protocol resolves the decision when possible
6. Recovered nodes reach the correct final state
7. All state transitions are logged before execution

## Research Resources

- [Gray & Lamport: Consensus on Transaction Commit (2006)](https://www.microsoft.com/en-us/research/wp-content/uploads/2016/02/tr-2003-96.pdf) -- Paxos Commit as an alternative
- [Designing Data-Intensive Applications, Chapter 9](https://dataintensive.net/) -- distributed transactions
- [Two-Phase Commit (Wikipedia)](https://en.wikipedia.org/wiki/Two-phase_commit_protocol)
- [Google Spanner: TrueTime and 2PC](https://research.google/pubs/pub39966/) -- 2PC at global scale

## What's Next

Continue to [17 - Saga Orchestrator](../17-saga-orchestrator/17-saga-orchestrator.md) to implement the Saga pattern as an alternative to 2PC.

## Summary

- 2PC ensures atomic distributed transactions through Prepare/Commit phases
- Write-ahead logging enables recovery after crashes
- The blocking problem is 2PC's fundamental weakness: coordinator failure blocks prepared participants
- Cooperative termination can resolve some blocking scenarios by consulting other participants
- 2PC is used in databases (XA transactions) and distributed systems where strong consistency is required
- Alternatives (3PC, Saga, Paxos Commit) address the blocking problem with different tradeoffs
