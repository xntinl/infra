<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 4h
-->

# Multi-Version Concurrency Control (MVCC)

Traditional locking-based concurrency control forces readers to block writers and writers to block readers, destroying throughput under concurrent workloads. Multi-Version Concurrency Control solves this by maintaining multiple versions of each row, allowing readers to see a consistent snapshot without blocking writers. Your task is to implement MVCC in Go with snapshot isolation semantics, version chains, garbage collection of obsolete versions, and write-write conflict detection. This is the concurrency control mechanism that enables your database engine to serve multiple concurrent transactions with high throughput and strong isolation guarantees.

## Requirements

1. Implement a version chain storage structure where each row has a primary (latest) version and a linked list of older versions. Each version contains: the tuple data, a creation transaction ID (`xmin`), a deletion transaction ID (`xmax`, zero if not deleted), and a pointer to the previous version. Store version chains in a `VersionStore` backed by an in-memory map keyed by primary key, with the chain ordered from newest to oldest.

2. Implement a `TransactionManager` that assigns monotonically increasing transaction IDs, tracks active transactions in an "active set," and provides `Begin() *Transaction`, `Commit(tx *Transaction) error`, and `Abort(tx *Transaction) error`. Each `Transaction` holds its ID, start timestamp, read set (for validation), write set (for rollback), and status (ACTIVE, COMMITTED, ABORTED).

3. Implement snapshot isolation visibility rules. A version is visible to transaction T if and only if: (a) the creating transaction (`xmin`) committed before T started, AND (b) the version has not been deleted (`xmax == 0`) OR the deleting transaction (`xmax`) had not committed when T started. Implement `IsVisible(version *Version, tx *Transaction) bool` that checks these rules against the transaction manager's commit log.

4. Implement MVCC-aware read operations: `Read(key PrimaryKey, tx *Transaction) (*Tuple, error)` walks the version chain from newest to oldest and returns the first visible version. `Scan(tx *Transaction) ([]*Tuple, error)` scans all keys and returns visible versions. Both operations must never block on concurrent writers -- readers always see a consistent snapshot determined by their start timestamp.

5. Implement MVCC-aware write operations: `Insert(key PrimaryKey, tuple *Tuple, tx *Transaction) error` creates a new version with `xmin = tx.ID`. `Update(key PrimaryKey, tuple *Tuple, tx *Transaction) error` marks the current visible version's `xmax = tx.ID` and creates a new version. `Delete(key PrimaryKey, tx *Transaction) error` marks the current visible version's `xmax = tx.ID`. All write operations must detect write-write conflicts: if the current visible version's `xmax` is set by a different active transaction, return a "write-write conflict" error (first-writer-wins policy).

6. Implement transaction commit and abort. On commit, record the transaction ID and commit timestamp in a global commit log. On abort, undo all writes in the transaction's write set: for inserts, remove the version; for updates, clear the `xmax` on the previous version and remove the new version; for deletes, clear the `xmax` on the deleted version. The abort must be atomic -- no intermediate state should be visible to other transactions.

7. Implement garbage collection of obsolete versions. A version is obsolete when no active or future transaction can ever see it: specifically, when there is a newer committed version and the oldest active transaction started after the obsolete version was superseded. Implement a `GarbageCollect()` method that walks all version chains, identifies and removes obsolete versions, and reclaims storage. Run GC periodically or on-demand.

8. Write tests covering: snapshot isolation correctness (transaction T1 does not see uncommitted changes from T2), read-your-own-writes (T1 sees its own uncommitted inserts/updates), write-write conflict detection (T1 and T2 both update the same key, second one fails), phantom read prevention verification (or demonstrate that snapshot isolation does NOT prevent phantoms), abort correctly restoring all modified rows, garbage collection removing exactly the obsolete versions and not visible ones, and a concurrent stress test with 50 transactions performing random reads, inserts, updates, and deletes on a shared dataset.

## Hints

- The visibility check is the heart of MVCC. Think of each transaction as having a "snapshot" of the database at its start time. The snapshot is defined by the set of committed transactions at that point.
- For the commit log, a simple `map[TxID]Timestamp` works. A transaction's status is COMMITTED if it appears in the commit log, ABORTED if it appears in an abort set, and ACTIVE otherwise.
- Write-write conflict detection happens at write time, not commit time. When T2 tries to update a row that T1 has already modified (xmax set to T1's ID and T1 is still active), T2 must abort or wait. First-writer-wins is simplest: T2 gets an error.
- For rollback, maintain a write set as `[]WriteEntry` where each entry records the key, operation type, and pointers to the old and new versions, allowing precise undo.
- Garbage collection's "oldest active transaction" threshold is called the "low watermark." Any version superseded before the low watermark is safe to collect.
- `sync.RWMutex` per version chain provides good concurrency: readers take read locks, writers take write locks, and different chains are independent.

## Success Criteria

1. Two concurrent transactions reading the same key see consistent snapshots: T1 sees the value as of its start time even after T2 commits an update.
2. Write-write conflicts are detected immediately and the conflicting transaction receives an error.
3. Aborted transactions leave no trace: all their writes are rolled back and invisible to any other transaction.
4. Read-your-own-writes works: a transaction sees its own uncommitted inserts and updates.
5. Garbage collection removes all versions that are unreachable by any active or future transaction, and does not remove any version that could still be needed.
6. 50 concurrent transactions operating on shared data produce no data races (verified with `-race`), no deadlocks, and consistent final state after all commit.
7. The system correctly handles a sequence of insert -> update -> update -> delete on the same key across different transactions with proper visibility at each step.

## Research Resources

- [CMU 15-445 Multi-Version Concurrency Control](https://15445.courses.cs.cmu.edu/fall2024/slides/18-mvcc.pdf)
- [PostgreSQL MVCC Implementation](https://www.postgresql.org/docs/current/mvcc-intro.html)
- [An Empirical Evaluation of In-Memory MVCC (Hyper)](https://db.in.tum.de/~muehlbau/papers/mvcc.pdf)
- [How Postgres Makes Transactions Atomic (Brandur)](https://brandur.org/postgres-atomicity)
- [Snapshot Isolation (Wikipedia)](https://en.wikipedia.org/wiki/Snapshot_isolation)
- [Write Skew Anomaly Explained](https://www.cockroachlabs.com/blog/what-write-skew-looks-like/)
