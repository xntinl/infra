# 101. MVCC Snapshot Isolation

<!--
difficulty: advanced
category: database-internals
languages: [rust]
concepts: [mvcc, snapshot-isolation, transactions, conflict-detection, garbage-collection, versioning, timestamps]
estimated_time: 14-18 hours
bloom_level: evaluate
prerequisites: [rust-ownership, concurrency-primitives, hash-maps, transaction-theory, atomic-operations]
-->

## Languages

- Rust (stable)

## Prerequisites

- Rust concurrency: `RwLock`, `Mutex`, `Arc`, atomic types
- Hash maps for key-to-version-chain lookups
- Understanding of transaction isolation levels (read committed, snapshot isolation, serializable)
- Monotonically increasing timestamps or transaction IDs
- Basic understanding of write-write conflicts and lost update anomaly

## Learning Objectives

- **Implement** Multi-Version Concurrency Control where each write creates a new version rather than overwriting in place
- **Design** snapshot reads that provide a consistent point-in-time view without blocking concurrent writers
- **Apply** write-write conflict detection using first-committer-wins semantics
- **Analyze** the trade-offs between snapshot isolation and serializable isolation, including write skew anomalies
- **Implement** garbage collection that safely removes versions no longer visible to any active transaction
- **Evaluate** how MVCC eliminates the need for read locks while maintaining consistency

## The Challenge

Every major database (PostgreSQL, MySQL/InnoDB, Oracle, CockroachDB) uses MVCC as its concurrency control mechanism. The core idea is deceptively simple: instead of locking rows to prevent conflicts, keep multiple versions of each row. Each transaction sees a consistent snapshot of the database as of its start time. Writers create new versions without disturbing readers. Conflicts are detected only between concurrent writers modifying the same key.

When a transaction begins, it receives a timestamp (or transaction ID) that defines its snapshot. A read at key K returns the most recent version of K whose commit timestamp is less than or equal to the transaction's start timestamp. Versions created by transactions that started after this transaction, or by transactions that have not yet committed, are invisible.

When a transaction writes key K, it creates a new version tagged with its transaction ID. At commit time, the system checks whether any other transaction committed a write to the same key between this transaction's start and commit. If so, the transaction must abort (first-committer-wins). If no conflict exists, the new versions become visible by setting their commit timestamp.

Over time, old versions accumulate. Garbage collection identifies versions that are no longer visible to any active transaction (all active transactions have a start timestamp greater than the version's commit timestamp, and a newer committed version exists) and removes them.

There are three main approaches to version storage:

- **Append-only storage** (used in this challenge): new versions are appended to the version chain. The chain is walked from newest to oldest during reads. Simple to implement, but version chains grow unbounded without GC.
- **Time-travel storage**: the latest version lives in the main table, older versions are moved to a separate time-travel table. Reads of the current version are fast, but reading historical versions requires a separate lookup.
- **Delta storage**: the main table stores the latest version. Older versions are stored as reverse deltas (the diff to reconstruct the prior version). Memory-efficient for large rows with small updates, but reconstruction cost grows with chain length.

PostgreSQL uses append-only storage in the heap with `xmin`/`xmax` markers. MySQL/InnoDB uses delta storage with undo logs. Oracle uses delta storage with rollback segments. Each approach optimizes for different read/write patterns.

Build an MVCC engine with snapshot isolation.

## Requirements

1. Implement `begin() -> TxnId` that assigns a monotonically increasing transaction ID, records the start timestamp, and takes a snapshot of all currently committed transaction IDs. The snapshot determines which versions are visible to this transaction
2. Implement `read(txn_id, key) -> Option<Value>` that scans the version chain for key K and returns the most recent version visible to the transaction's snapshot. A version is visible if it was committed by a transaction that committed before this transaction's start, or if it was written by this transaction itself
3. Implement `write(txn_id, key, value)` that creates a new uncommitted version in the version chain. If another uncommitted transaction has already written to this key, return an error immediately (eager conflict detection)
4. Implement `delete(txn_id, key)` that writes a tombstone version, following the same conflict rules as write
5. Implement `commit(txn_id) -> Result<(), ConflictError>` that validates: for each key written by this transaction, no other transaction committed a version of that key between this transaction's start timestamp and now. If validation passes, assign a commit timestamp and make all versions visible. If validation fails, abort and roll back all versions
6. Implement `abort(txn_id)` that marks all versions created by this transaction as aborted, making them invisible to all future reads and eligible for immediate garbage collection
7. Implement garbage collection that removes versions that satisfy: (a) a newer committed version exists for the same key, and (b) no active transaction could possibly see the old version (its commit timestamp is less than the minimum start timestamp of all active transactions)
8. All operations must be safe for concurrent access from multiple threads
9. Implement `scan(start_key, end_key)` within a transaction that returns all visible key-value pairs in the range, consistent with the transaction's snapshot. The scan must not see partial results from concurrent transactions
10. Track transaction statistics: count of active transactions, total versions in memory, GC watermark timestamp. Expose these as a `stats()` method for monitoring

## Hints

Understanding the visibility rules is the single most important aspect of MVCC. For a transaction T reading key K, a version V is visible if and only if ALL of the following hold:

1. V's creating transaction has committed (its status is Committed, not Active or Aborted)
2. V's commit timestamp is less than T's start timestamp (it was committed before T began)
3. No newer visible version of K exists (V is the most recent version satisfying rules 1 and 2)

The special case: T always sees its own uncommitted writes, regardless of commit timestamp.

For conflict detection, there are two strategies:

- **Eager (pessimistic)**: check for conflicts at write time. If another active transaction holds an uncommitted write to the same key, fail immediately. Prevents wasted work but may reject transactions that would not actually conflict at commit time.
- **Lazy (optimistic)**: allow all writes to proceed. Check for conflicts only at commit time. If another transaction committed a write to the same key between our start and commit timestamps, abort. Allows more concurrency but wastes work on transactions that will abort.

This challenge implements both: eager detection for uncommitted conflicts (two active transactions writing the same key) and lazy validation at commit time (checking for committed conflicts).

The version chain for each key is a list of `(txn_id, commit_ts, value)` tuples ordered by creation time (newest first). Reads walk the chain from newest to oldest, returning the first version whose `commit_ts` is less than the reader's start timestamp. Uncommitted versions (commit_ts = None) are visible only to the transaction that created them.

For conflict detection, maintain a write set per transaction: the set of keys written during the transaction. At commit time, for each key in the write set, check whether any version was committed with a timestamp between `txn.start_ts` and `now`. If so, the transaction has a write-write conflict and must abort.

Garbage collection needs the minimum active start timestamp (the watermark). Any committed version with a commit timestamp below this watermark that also has a newer committed version above it can be safely removed. Use an `Arc<AtomicU64>` for the watermark, updated whenever a transaction begins or ends.

Snapshot isolation prevents lost updates and dirty reads but allows write skew. Write skew occurs when two transactions read overlapping data, make disjoint writes based on what they read, and both commit successfully. True serializability requires additional checks (serializable snapshot isolation, or SSI), which is outside the scope of this challenge.

## Acceptance Criteria

- [ ] A transaction reads its own uncommitted writes
- [ ] A transaction does not see writes committed after its start timestamp
- [ ] Two concurrent readers see consistent snapshots without blocking each other
- [ ] A reader does not block a writer and a writer does not block a reader
- [ ] Write-write conflict: if two transactions write the same key, the second to commit aborts
- [ ] After abort, all versions created by the aborted transaction are invisible
- [ ] Committed writes become visible to transactions that start after the commit
- [ ] Garbage collection removes old versions that no active transaction can see
- [ ] After GC, the most recent committed version of each key is always preserved
- [ ] Stress test: 8+ threads running concurrent transactions with mixed reads and writes, no deadlocks, no data corruption
- [ ] Delete (tombstone) prevents reads of prior versions by newer transactions
- [ ] Range scan within a transaction returns a consistent snapshot of all keys in [start, end]
- [ ] Transaction statistics correctly report active count, version count, and watermark
- [ ] Overwriting a key within the same transaction updates the version in place rather than creating a duplicate

## Going Further

- Implement Serializable Snapshot Isolation (SSI) by tracking read sets and detecting read-write conflicts (rw-antidependencies) in addition to write-write conflicts. This prevents write skew anomalies
- Implement delta-based version storage: store the latest version inline and older versions as reverse diffs. Measure the trade-off between storage savings and reconstruction cost
- Add a transaction log (commit log) that records committed transaction IDs. On restart, rebuild the set of committed transactions to restore visibility rules
- Implement long-running transaction detection: warn when a transaction holds the GC watermark low for more than a configurable threshold, preventing version cleanup
- Build a read-only transaction mode that skips write set tracking and conflict detection for better performance on read-heavy workloads
- Implement optimistic read validation: instead of maintaining a read set, validate at commit time that no key read by this transaction was modified by a concurrent committed transaction

## Research Resources

- [CMU 15-445: Multi-Version Concurrency Control (Andy Pavlo)](https://15445.courses.cs.cmu.edu/fall2024/slides/16-mvcc.pdf) -- lecture slides covering MVCC design, version storage, garbage collection
- [CMU 15-445: Timestamp Ordering Concurrency Control](https://15445.courses.cs.cmu.edu/fall2024/slides/17-timestampordering.pdf) -- timestamp-based protocols that complement MVCC
- [A Critique of ANSI SQL Isolation Levels (Berenson et al., 1995)](https://www.microsoft.com/en-us/research/wp-content/uploads/2016/02/tr-95-51.pdf) -- defines snapshot isolation and its anomalies
- [An Empirical Evaluation of In-Memory MVCC (Wu et al., 2017)](https://www.vldb.org/pvldb/vol10/p781-Wu.pdf) -- benchmarks and design trade-offs for in-memory MVCC
- [PostgreSQL MVCC Documentation](https://www.postgresql.org/docs/current/mvcc.html) -- how a production database implements MVCC with xmin/xmax
- [Serializable Snapshot Isolation in PostgreSQL](https://drkp.net/papers/ssi-vldb12.pdf) -- extending SI to prevent write skew
- [Database Internals by Alex Petrov, Ch. 5](https://www.databass.dev/) -- transaction processing and MVCC strategies
- [Andy Pavlo's MVCC Lecture (YouTube)](https://www.youtube.com/watch?v=jEB8M9-paM0) -- full CMU 15-445 lecture on multi-version concurrency control
- [Hekaton: SQL Server's Memory-Optimized OLTP Engine](https://www.vldb.org/pvldb/vol6/p1726-diaconu.pdf) -- Microsoft's in-memory MVCC engine with lock-free data structures
- [CockroachDB Transaction Layer](https://www.cockroachlabs.com/docs/stable/architecture/transaction-layer.html) -- distributed MVCC with serializable snapshot isolation in production
- [Concurrency Control and Recovery in Database Systems (Bernstein, Hadzilacos, Goodman)](https://www.microsoft.com/en-us/research/people/philbe/book/) -- comprehensive textbook on transaction theory, free online
