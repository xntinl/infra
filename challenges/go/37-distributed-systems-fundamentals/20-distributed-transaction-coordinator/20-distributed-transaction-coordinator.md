# 20. Distributed Transaction Coordinator

<!--
difficulty: insane
concepts: [transaction-coordinator, distributed-transactions, xa-protocol, transaction-log, recovery-manager, deadlock-detection, transaction-isolation]
tools: [go]
estimated_time: 3h
bloom_level: create
prerequisites: [two-phase-commit, saga-orchestrator, distributed-locking]
-->

## Prerequisites

- Go 1.22+ installed
- Completed 2PC (exercise 16) and Saga (exercise 17) exercises
- Understanding of ACID properties and transaction isolation levels

## Learning Objectives

- **Create** a distributed transaction coordinator that manages cross-partition transactions
- **Analyze** the interaction between transaction isolation, distributed locking, and deadlock detection
- **Evaluate** the performance and correctness tradeoffs of different transaction coordination strategies

## The Challenge

A distributed transaction coordinator manages transactions that span multiple data partitions or services. It must ensure ACID properties across partition boundaries: atomicity (all-or-nothing), consistency (invariants hold), isolation (concurrent transactions do not interfere), and durability (committed data survives failures).

Build a comprehensive transaction coordinator that combines 2PC for atomicity, distributed locking for isolation, deadlock detection for liveness, and a persistent transaction log for durability and recovery.

## Requirements

1. Implement a `TransactionCoordinator` that manages the lifecycle of distributed transactions: Begin, Execute operations on multiple partitions, Commit/Rollback
2. Implement a `TransactionLog` (write-ahead log) that durably records transaction state transitions for crash recovery
3. Implement distributed locking per partition with deadlock detection using a wait-for graph
4. Implement at least two isolation levels: Read Committed (readers do not block writers) and Serializable (strict ordering)
5. Implement 2PC-based commit with timeout handling and automatic abort for unresponsive participants
6. Implement a recovery manager that, on coordinator restart, resolves in-doubt transactions by reading the transaction log
7. Implement concurrent transaction handling: multiple transactions executing simultaneously with proper isolation
8. Build a concrete example: a distributed banking system where transfers between accounts on different partitions use distributed transactions
9. Benchmark: measure transaction throughput, latency, and abort rate under increasing concurrency
10. Write tests for: successful cross-partition transactions, abort on conflict, deadlock detection and resolution, crash recovery

## Hints

- The wait-for graph tracks which transaction is waiting for which other transaction's lock. A cycle in the graph indicates a deadlock. Resolve by aborting the youngest transaction.
- Read Committed: each read sees the latest committed value. Write locks are held until commit. Read locks are released immediately.
- Serializable: both read and write locks are held until commit. This prevents phantom reads and non-repeatable reads.
- The transaction log records: `BEGIN txid`, `LOCK resource txid`, `WRITE resource txid old_value new_value`, `PREPARE txid`, `COMMIT txid`, `ABORT txid`.
- Recovery: replay the log. Committed transactions: apply their writes. Aborted transactions: undo their writes. In-doubt transactions (PREPARE but no COMMIT/ABORT): wait for coordinator decision or use timeout-based presumed abort.
- Use lock ordering (alphabetical by resource) as an additional deadlock prevention strategy.
- Partition each resource into a separate data store. The coordinator routes operations to the correct partition.

## Success Criteria

1. Cross-partition transactions commit atomically
2. Concurrent transactions are properly isolated (no dirty reads, no lost updates)
3. Deadlocks are detected and resolved within bounded time
4. The recovery manager correctly resolves in-doubt transactions after a crash
5. The banking example correctly handles concurrent transfers without losing money
6. Transaction throughput scales with the number of partitions (for non-conflicting transactions)
7. The abort rate increases gracefully under high contention
8. All ACID properties hold under stress testing

## Research Resources

- [Jim Gray: Transaction Processing](https://www.amazon.com/Transaction-Processing-Concepts-Techniques-Management/dp/1558601902) -- the definitive reference
- [Spanner: Google's Globally Distributed Database](https://research.google/pubs/pub39966/) -- distributed transactions at scale
- [CockroachDB Transaction Layer](https://www.cockroachlabs.com/docs/stable/architecture/transaction-layer.html) -- modern distributed transaction implementation
- [Designing Data-Intensive Applications, Chapter 7](https://dataintensive.net/) -- transactions and isolation
- [Percolator: Large-Scale Incremental Processing](https://research.google/pubs/pub36726/) -- distributed transaction protocol

## What's Next

Continue to [21 - Anti-Entropy Protocol](../21-anti-entropy-protocol/21-anti-entropy-protocol.md) to implement background consistency repair.

## Summary

- Distributed transaction coordinators ensure ACID properties across partition boundaries
- 2PC provides atomicity; distributed locking provides isolation; WAL provides durability
- Deadlock detection using wait-for graphs prevents livelock under high contention
- Recovery managers resolve in-doubt transactions after crashes using the transaction log
- Higher isolation levels provide stronger guarantees but reduce concurrency
- This is the architecture behind distributed databases like Spanner, CockroachDB, and TiDB
