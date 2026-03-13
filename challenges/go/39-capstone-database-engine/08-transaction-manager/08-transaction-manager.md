<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 3h
-->

# Transaction Manager

A transaction manager is the orchestration layer that ties together the WAL, buffer pool, MVCC, and lock management into a coherent system that guarantees ACID properties. Your task is to build a transaction manager in Go that coordinates begin, commit, and abort operations across all these subsystems, implements strict two-phase locking for serializability (as an alternative to MVCC snapshot isolation), provides deadlock detection, and handles crash recovery using the ARIES algorithm. This is the component that makes your database engine trustworthy.

## Requirements

1. Implement a `TransactionManager` struct that coordinates transaction lifecycle. `Begin() (*Transaction, error)` assigns a new transaction ID, writes a BEGIN record to the WAL, and initializes the transaction's lock set and write set. `Commit(tx *Transaction) error` writes all dirty pages, writes a COMMIT record to the WAL, ensures the WAL is flushed to disk, releases all locks, and marks the transaction as committed. `Abort(tx *Transaction) error` undoes all changes using the WAL undo records, writes an ABORT record to the WAL, releases all locks, and marks the transaction as aborted.

2. Implement a lock manager with a lock table supporting shared (S) and exclusive (X) locks on row-level granularity. `LockShared(tx *Transaction, key LockKey) error` acquires a shared lock (multiple readers allowed). `LockExclusive(tx *Transaction, key LockKey) error` acquires an exclusive lock (no other readers or writers). Shared locks can coexist with other shared locks but not exclusive locks. If a lock cannot be immediately granted, the requesting transaction blocks on a per-lock condition variable.

3. Implement strict two-phase locking (S2PL): transactions acquire locks as needed during execution (growing phase) and release ALL locks only at commit or abort time (shrinking phase is instantaneous). This guarantees serializability. The transaction's lock set must track all held locks for release at commit/abort. Implement lock upgrade: a transaction holding a shared lock can upgrade to exclusive if no other transaction holds a shared lock on the same key.

4. Implement deadlock detection using a wait-for graph. Maintain a directed graph where an edge from T1 to T2 means T1 is waiting for a lock held by T2. Run cycle detection (DFS-based) either on every lock wait or periodically (every 100ms). When a deadlock is detected, choose a victim transaction (the youngest transaction by ID) and abort it, returning a deadlock error to the caller.

5. Implement WAL-based undo: for every write operation (insert, update, delete), write a WAL record containing enough information to undo the operation (the "before image" for updates, the key for inserts, the full tuple for deletes). On abort, read the transaction's WAL records in reverse order and apply undo operations. Implement redo as well: for crash recovery, redo all operations from committed transactions whose effects may not have reached disk.

6. Implement ARIES-style crash recovery with three phases. **Analysis phase**: scan the WAL from the last checkpoint to identify active transactions at crash time and dirty pages. **Redo phase**: replay all WAL records from the oldest dirty page's LSN forward, re-applying changes to bring pages up to date. **Undo phase**: rollback all transactions that were active at crash time by processing their WAL records in reverse. Implement compensation log records (CLRs) during undo to ensure idempotent recovery if a crash occurs during recovery.

7. Implement savepoints: `Savepoint(tx *Transaction, name string) error` records the current position in the transaction's WAL stream. `RollbackToSavepoint(tx *Transaction, name string) error` undoes all operations after the savepoint without aborting the entire transaction, releases locks acquired after the savepoint, and allows the transaction to continue. Support nested savepoints.

8. Write tests covering: basic commit and abort with WAL verification, concurrent transactions with lock contention (verify serializable execution), deadlock detection and resolution (create a deliberate cycle between 3 transactions), crash recovery simulation (write WAL records, simulate crash before flush, recover and verify data integrity), savepoint creation and partial rollback, lock upgrade success and failure cases, stress test with 20 concurrent transactions performing random operations with deadlock potential, and verification that ARIES recovery is idempotent (recovering an already-recovered database produces the same state).

## Hints

- ARIES recovery is complex but follows a clear pattern. The key invariant is that WAL records describe both how to redo and how to undo each operation. The checkpoint provides a starting point to avoid replaying the entire log.
- For the lock table, use `map[LockKey]*LockEntry` where `LockEntry` contains the lock mode, a set of holding transaction IDs, and a `sync.Cond` for waiters. Waiters should recheck the lock state when signaled (spurious wakeups).
- Deadlock detection via DFS: build the wait-for graph from the lock table (who holds what, who waits for what), then check for cycles. Aborting the youngest transaction in the cycle is a common victim selection strategy.
- Compensation Log Records (CLRs) during undo are WAL records that describe the undo action itself. They have a "undo-next LSN" pointer that skips over the undone record, preventing re-undoing during a crash during recovery.
- For savepoints, store the LSN at savepoint creation. Rolling back to a savepoint means undoing all WAL records with LSN greater than the savepoint LSN that belong to this transaction.
- Strict 2PL releases locks at commit/abort, which is the "strict" part. Plain 2PL can release locks as soon as no more locks will be acquired, but strict 2PL is simpler and prevents cascading aborts.

## Success Criteria

1. Committed transactions are durable: simulating a crash after commit and running recovery produces the committed data.
2. Aborted transactions leave no trace: all their modifications are undone and invisible after abort.
3. Concurrent transactions execute serializably under strict 2PL: the result is equivalent to some serial execution order.
4. Deadlocks between 3+ transactions are detected within 200ms and resolved by aborting exactly one victim.
5. ARIES recovery correctly redoes committed work and undoes uncommitted work, with the final state matching what was committed before the crash.
6. Recovery is idempotent: running recovery twice produces the same result as running it once.
7. Savepoints allow partial rollback without affecting earlier operations within the same transaction.
8. No data races under concurrent operation, verified with the `-race` detector.

## Research Resources

- [ARIES: A Transaction Recovery Method (Mohan et al.)](https://cs.stanford.edu/people/chr101/aries.html)
- [CMU 15-445 Concurrency Control](https://15445.courses.cs.cmu.edu/fall2024/slides/16-twophaselocking.pdf)
- [Two-Phase Locking Protocol](https://en.wikipedia.org/wiki/Two-phase_locking)
- [Deadlock Detection Algorithms](https://www.geeksforgeeks.org/deadlock-detection-algorithm-in-operating-system/)
- [ARIES Recovery Algorithm (detailed walkthrough)](https://web.stanford.edu/class/cs245/slides/08-Recovery.pdf)
- [PostgreSQL Transaction Management Internals](https://www.interdb.jp/pg/pgsql05.html)
