# 16. Build an In-Memory Database with MVCC and SQL Subset

**Difficulty**: Insane

## Prerequisites

- Mastered: ETS, GenServer, binary pattern matching, recursive data structures
- Mastered: Transaction concepts (ACID), B-tree fundamentals, query planning basics
- Familiarity with: MVCC internals (PostgreSQL or MySQL InnoDB), serializable snapshot isolation, deadlock detection algorithms (wait-for graph)

## Problem Statement

Build a fully featured in-memory relational database engine in Elixir, capable of executing
a subset of SQL via an Elixir API. The database must provide true MVCC isolation so that
readers never block writers and vice versa:

1. Support table creation with the following column types: `:integer`, `:text`, `:boolean`,
   and `:timestamp`. Enforce NOT NULL and UNIQUE constraints at write time.
2. Implement INSERT, SELECT (with WHERE filtering and column projection), UPDATE (with WHERE),
   and DELETE (with WHERE).
3. Implement B-tree indexing on any column. The query planner must automatically use an
   index when the WHERE clause references an indexed column with an equality or range predicate.
4. Implement Multi-Version Concurrency Control: each row has a creation transaction ID and
   an expiration transaction ID. Readers see only versions that were committed before their
   snapshot was taken. Writers create new row versions rather than updating in place.
5. Implement transactions with `BEGIN`, `COMMIT`, and `ROLLBACK`. Transactions have
   serializable isolation: the result of concurrent committed transactions must be
   equivalent to some serial execution order.
6. Detect deadlocks using a wait-for graph: when a cycle is detected, select a victim
   transaction (lowest priority or youngest) and abort it with `{:error, :deadlock}`.
7. Implement query optimization: choose between index scan and full table scan based on
   estimated selectivity. Document the cost model used.
8. Garbage collect old row versions that are no longer visible to any active transaction.
   GC must run without blocking ongoing queries.
9. Reach benchmark targets: 1 million read operations per second and 100k write operations
   per second on a table of 1 million rows, measured with `Benchee`.

## Acceptance Criteria

- [ ] `DB.create_table(db, :users, columns: [id: :integer, name: :text, active: :boolean])`
      creates a table; a second call on the same name returns `{:error, :table_exists}`.
- [ ] `DB.insert(db, :users, %{id: 1, name: "Alice", active: true})` returns `:ok`; inserting
      a duplicate on a UNIQUE column returns `{:error, :constraint_violation}`.
- [ ] `DB.select(db, :users, where: [active: true], columns: [:id, :name])` returns only
      active users with the projected columns; WHERE supports `=`, `<`, `>`, `<=`, `>=`, `!=`.
- [ ] A transaction started with `DB.begin(db)` sees a consistent snapshot; writes by
      another concurrent transaction committed after `begin` are not visible.
- [ ] `DB.commit(db, txn)` makes the transaction's writes visible atomically;
      `DB.rollback(db, txn)` discards all writes and releases locks.
- [ ] Two transactions updating the same row concurrently are serialized; one completes
      and the other either waits or aborts with `{:error, :deadlock}`.
- [ ] `DB.create_index(db, :users, :name)` causes subsequent queries with `where: [name: "Alice"]`
      to use the index; confirmed by an `EXPLAIN`-equivalent function showing `plan: :index_scan`.
- [ ] GC runs periodically; after all transactions that could see old versions commit,
      `DB.stats(db)` shows `dead_row_count` decreasing.
- [ ] Benchmark meets 1M reads/s and 100k writes/s targets on a 1M-row table;
      documented with machine spec, row size, and query pattern.

## What You Will Learn

- MVCC row versioning: creation XID, expiration XID, visibility rules per snapshot
- B-tree insertion, deletion, and range scan implementation in a functional style
- The wait-for graph algorithm for deadlock detection and the victim selection policy
- Query cost estimation: how selectivity, table cardinality, and index statistics feed a simple cost model
- Garbage collection in a live database: identifying the oldest active transaction (horizon) and pruning dead versions below it
- The serializable snapshot isolation (SSI) protocol and its write skew anomaly prevention

## Hints

This exercise is intentionally sparse. Research:

- Store rows as `{row_id, data, created_xid, expired_xid}` in ETS; a row is visible if `created_xid < snapshot_xid` and `expired_xid > snapshot_xid` (or 0 meaning alive)
- B-tree in Elixir: implement as a persistent tree using immutable Elixir maps; store the root reference in the GenServer state; update produces a new root
- Deadlock detection: maintain a GenServer holding the wait-for graph as an adjacency list; detect cycles with DFS; run the check on each lock-wait event
- For SSI, track write sets and read sets per transaction and check for conflicting read-write pairs at commit time
- GC horizon: `min(active_transaction_xids)` — any row version expired before the horizon and with `expired_xid < horizon` can be deleted

## Reference Material

- PostgreSQL MVCC documentation: https://www.postgresql.org/docs/current/mvcc-intro.html
- "Serializable Snapshot Isolation in PostgreSQL" — Ports & Gritter, VLDB 2012
- MySQL InnoDB MVCC internals: https://dev.mysql.com/doc/refman/8.0/en/innodb-multi-versioning.html
- "Database Internals" — Alex Petrov, Part I (Storage Engines), Part II (Distributed Systems)
- B-tree original paper: Bayer & McCreight, 1972

## Difficulty Rating

★★★★★★

## Estimated Time

70–100 hours
