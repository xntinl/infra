# 35. Lock Manager with Deadlock Detection

<!--
difficulty: advanced
category: databases
languages: [go, rust]
concepts: [lock-management, deadlock-detection, wait-for-graph, shared-exclusive-locks, intention-locks, lock-upgrade, cycle-detection]
estimated_time: 10-14 hours
bloom_level: evaluate
prerequisites: [concurrency-basics, graph-algorithms, mutex-rwlock, transaction-concepts]
-->

## Languages

- Go (1.22+)
- Rust (stable)

## Prerequisites

- Mutex, RwLock, and condition variable semantics
- Graph data structures and cycle detection algorithms (DFS)
- Basic transaction concepts (isolation, read/write conflicts)
- Understanding of why concurrent database access requires locking
- Familiarity with channels (Go) or `std::sync` primitives (Rust)

## Learning Objectives

- **Implement** a lock manager that grants shared and exclusive locks to concurrent transactions on database resources
- **Design** a wait-for graph that tracks blocking dependencies between transactions
- **Analyze** deadlock scenarios by detecting cycles in the wait-for graph
- **Evaluate** deadlock resolution strategies and their impact on transaction throughput
- **Apply** intention locks (IS, IX) to enable hierarchical locking across tables and rows
- **Implement** lock upgrade from shared to exclusive without introducing conversion deadlocks

## The Challenge

When multiple transactions access the same database rows concurrently, a lock manager decides who waits and who proceeds. Shared locks allow concurrent readers. Exclusive locks block everyone else. But locks introduce a fundamental trade-off: more locking means more correctness but less concurrency; less locking means more throughput but risks inconsistency. And locks create a new problem that does not exist without them: deadlocks, where two or more transactions each hold a lock the other needs, and neither can proceed.

A deadlock between two transactions is easy to visualize: T1 holds lock A and wants lock B, while T2 holds lock B and wants lock A. But deadlocks can involve arbitrary numbers of transactions in arbitrary chain lengths. Detecting them requires maintaining a directed graph of waiting dependencies and running cycle detection on every lock request (or periodically).

Build a lock manager that supports multiple lock modes, detects deadlocks by maintaining a wait-for graph, and resolves them by aborting the youngest transaction. The lock manager must also support hierarchical locking through intention locks, which signal to parent resources (tables) what kind of locks their children (rows) will need, avoiding unnecessary lock conflicts at the table level.

Intention locks solve the granularity problem. Without them, a transaction that wants to lock an entire table for reading must check every row lock to ensure no writer holds a conflicting lock -- an O(rows) operation. Intention locks provide a summary at the table level: IS means "some descendant holds or will hold S," IX means "some descendant holds or will hold X." The compatibility matrix for intention locks allows concurrent IS-IS, IS-IX, and IX-IX (different rows being locked differently), while blocking IS-X, IX-S, IX-X, S-X, and X-X (true conflicts at the table level).

This is a core component of every database concurrency control system. MySQL's InnoDB, PostgreSQL, and SQL Server all implement lock managers with wait-for graphs, hierarchical locking, and deadlock detection. After this challenge, you will understand why `SELECT ... FOR UPDATE` blocks other writers, how deadlocks are detected and resolved without user intervention, why intention locks exist, and why lock contention is often the bottleneck in OLTP workloads.

## Lock Compatibility Matrix

For reference, the full compatibility matrix for all lock modes:

| Held \ Requested | IS  | IX  | S   | X   |
|-------------------|-----|-----|-----|-----|
| **IS**            | Yes | Yes | Yes | No  |
| **IX**            | Yes | Yes | No  | No  |
| **S**             | Yes | No  | Yes | No  |
| **X**             | No  | No  | No  | No  |

Two locks are compatible if and only if both the row and column entries yield "Yes." When any held lock is incompatible with the requested mode, the requesting transaction must wait.

## Requirements

1. Implement a lock manager that tracks lock grants and requests for named resources (represented as strings or integer IDs)
2. Support Shared (S) and Exclusive (X) lock modes with the standard compatibility matrix: S-S compatible, S-X incompatible, X-X incompatible
3. `lock(txn_id, resource_id, mode) -> Result<(), LockError>`: grants the lock immediately if compatible with existing grants, otherwise blocks the transaction until the lock becomes available or a deadlock is detected
4. `unlock(txn_id, resource_id)`: releases the lock and wakes any waiting transactions that can now proceed
5. `unlock_all(txn_id)`: releases all locks held by a transaction (used during commit or abort)
6. Implement lock upgrade: a transaction holding an S lock can request an upgrade to X. If no other transaction holds an S lock on the same resource, the upgrade succeeds immediately. Otherwise, the transaction waits
7. Maintain a wait-for graph: when a transaction blocks, add edges from the waiting transaction to all transactions holding conflicting locks. When a lock is released, remove the corresponding edges
8. Implement deadlock detection via cycle detection in the wait-for graph. Run detection either on every lock request or periodically (configurable)
9. Deadlock resolution: when a cycle is detected, abort the transaction with the highest transaction ID (youngest) by returning a deadlock error. The aborted transaction must release all its locks
10. Implement intention locks: IS (intention shared) and IX (intention exclusive). Before acquiring an S lock on a row, the transaction must hold IS or stronger on the table. Before acquiring X on a row, must hold IX or stronger on the table. Compatibility: IS-IS yes, IS-IX yes, IS-X no, IX-IX yes, IX-S no, IX-X no, S-X no, X-X no
11. Implement lock timeout: if a transaction waits longer than a configurable duration, return a timeout error

## Hints

<details>
<summary>Hint 1 -- Lock table structure</summary>

Use a `HashMap<ResourceId, LockEntry>` where each `LockEntry` contains: a list of current lock grants (each with txn_id and lock mode), and a FIFO queue of waiting lock requests (each with txn_id, requested mode, and a notification channel/condvar). When a lock is released, scan the wait queue front-to-back and grant all requests whose mode is compatible with the current grants.

For S mode, multiple transactions can hold the lock simultaneously (the grants list has multiple entries). For X mode, exactly one transaction holds the lock. When checking compatibility, test the requested mode against EVERY current grant.
</details>

<details>
<summary>Hint 2 -- Wait-for graph and cycle detection</summary>

Use an adjacency list `HashMap<TxnId, HashSet<TxnId>>`. An edge from T1 to T2 means "T1 is waiting for T2 to release a lock." When T1 requests a lock that conflicts with T2's grant, add edge T1->T2. When T2 releases, remove all edges pointing to T2.

Detect cycles with a DFS that tracks the recursion stack. If you revisit a node already on the stack, you have a deadlock. Keep a `visited` set and an `on_stack` set. A node is on the cycle only if it is revisited while still on the recursion stack, not merely if it has been visited in a previous DFS branch. The youngest transaction (highest ID) in the cycle is the victim.
</details>

<details>
<summary>Hint 3 -- Lock upgrade and conversion deadlock</summary>

If T1 and T2 both hold S locks on resource R and both try to upgrade to X, they deadlock: T1 waits for T2 to release S, T2 waits for T1 to release S. The wait-for graph cycle detection will catch this, but it is more efficient to detect it as a special case at upgrade time.

When a transaction requests an upgrade (it already holds S and wants X), check if any other transaction is also waiting for an upgrade on the same resource. If so, report the conversion deadlock immediately. Also check if the transaction is the sole S holder: if yes, the upgrade can be granted instantly without waiting.
</details>

## Acceptance Criteria

- [ ] Multiple transactions acquire S locks on the same resource concurrently without blocking
- [ ] X lock request blocks until all S locks on the resource are released
- [ ] X lock request blocks until any existing X lock on the resource is released
- [ ] Lock upgrade from S to X succeeds immediately when the requester is the only S holder
- [ ] Lock upgrade correctly blocks when other S holders exist and grants when they release
- [ ] Conversion deadlock (two S holders both upgrading to X) is detected immediately
- [ ] Wait-for graph accurately reflects current blocking dependencies at all times
- [ ] Deadlocks involving 2 transactions are detected and one is aborted
- [ ] Deadlocks involving 3 or more transactions in a cycle are detected and resolved
- [ ] The youngest transaction (highest ID) in a deadlock cycle is always the one aborted
- [ ] Intention locks enforce hierarchical compatibility: IS-IX compatible, IX-S incompatible, IX-X incompatible
- [ ] Lock timeout returns an error after the configured duration without deadlock being involved
- [ ] `unlock_all` releases every lock held by a transaction and wakes all compatible waiters
- [ ] No lock leaks: after all transactions commit or abort, the lock table has zero active grants
- [ ] Both Go and Rust implementations pass identical test scenarios with identical outcomes

## Going Further

- Implement lock escalation: automatically upgrade from row-level to table-level locking when a transaction holds more than N row locks on the same table
- Add wait-die or wound-wait deadlock prevention schemes as alternatives to detection
- Implement multi-version concurrency control (MVCC) as an alternative to locking, where readers never block writers
- Add lock partitioning: divide the lock table into N buckets by resource ID hash, each with its own mutex, to reduce contention
- Benchmark deadlock detection overhead: compare eager (per-request) vs periodic (every Nms) detection under varying contention levels

## Research Resources

- [CMU 15-445: Two-Phase Locking (Andy Pavlo)](https://15445.courses.cs.cmu.edu/fall2024/slides/16-twophaselocking.pdf) -- lock modes, 2PL protocol, deadlock handling
- [CMU 15-445: Concurrency Control Theory](https://15445.courses.cs.cmu.edu/fall2024/slides/15-concurrencycontrol.pdf) -- serializability, lock compatibility matrices
- [MySQL InnoDB Locking](https://dev.mysql.com/doc/refman/8.0/en/innodb-locking.html) -- shared, exclusive, intention locks in a production database
- [MySQL InnoDB Deadlock Detection](https://dev.mysql.com/doc/refman/8.0/en/innodb-deadlock-detection.html) -- how InnoDB detects and resolves deadlocks
- [PostgreSQL Explicit Locking](https://www.postgresql.org/docs/current/explicit-locking.html) -- lock modes, deadlock detection behavior, advisory locks
- [PostgreSQL Deadlock Detection](https://www.postgresql.org/docs/current/runtime-config-locks.html) -- deadlock_timeout and detection mechanism
- [Gray & Reuter: Transaction Processing, Ch. 7-8](https://www.amazon.com/Transaction-Processing-Concepts-Techniques-Management/dp/1558601902) -- definitive reference on lock management and granularity
- [Database Internals by Alex Petrov, Ch. 12-13](https://www.databass.dev/) -- concurrency control, lock hierarchies, deadlock detection algorithms
- [Andy Pavlo's Intro to Database Systems (YouTube)](https://www.youtube.com/playlist?list=PLSE8ODhjZXjbj8BMuIrRcacnQh20hmY9g) -- full CMU 15-445 lecture playlist, concurrency control is lectures 15-17
