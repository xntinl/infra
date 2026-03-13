<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 4h+
-->

# Full Embedded Database Engine

This is the culmination of the entire database engine capstone. You will integrate every component you have built -- WAL, B+Tree indexes, buffer pool manager, SQL lexer, SQL parser, query planner, MVCC, transaction manager, and network protocol -- into a single cohesive, functional embedded database engine. The result will be a database that accepts SQL queries over a PostgreSQL-compatible wire protocol, stores data durably on disk, supports concurrent transactions with snapshot isolation, recovers from crashes, and optimizes queries using indexes. This is your magnum opus.

## Requirements

1. Create a `Database` struct that composes all subsystems: a `WAL` for durability, a `BufferPoolManager` for page caching, a `DiskManager` for raw file I/O, a `Catalog` for schema metadata, a `TransactionManager` for ACID transactions, a `QueryEngine` (lexer + parser + planner + executor), and a `Server` for the network protocol. Implement `Open(path string, opts Options) (*Database, error)` that initializes all subsystems, runs crash recovery if a WAL exists from a previous run, and opens the database for use. Implement `Close() error` for clean shutdown with WAL flush and buffer pool flush.

2. Implement a heap file storage engine where each table is stored as a collection of pages. Each page has a slotted-page layout: a page header with a slot directory at the beginning (slot count, free space pointer, array of <offset, length> pairs) and tuple data packed from the end of the page toward the middle. Implement `InsertTuple`, `ReadTuple`, `UpdateTuple`, `DeleteTuple` operations on heap pages. Tuple updates that change size may require moving the tuple to a new page (leaving a forwarding pointer).

3. Implement a system catalog stored as regular tables within the database itself: `sys_tables` (table_id, table_name, page_count, row_count_estimate), `sys_columns` (table_id, column_name, column_type, ordinal_position, is_nullable, default_value), and `sys_indexes` (index_id, table_id, index_name, column_names, is_unique). The catalog is bootstrapped on first database creation and queried by the planner to resolve table/column references and find available indexes.

4. Wire everything together for DML execution: a `SELECT` query goes through lexer -> parser -> planner (which consults the catalog and chooses scan operators) -> executor (which fetches pages through the buffer pool) -> result serialization. An `INSERT` acquires an exclusive lock via the transaction manager, writes a WAL record, inserts the tuple into the heap file via the buffer pool, updates any B+Tree indexes, and updates catalog statistics. `UPDATE` and `DELETE` follow similar paths with appropriate WAL records and MVCC version management.

5. Wire everything together for DDL execution: `CREATE TABLE` adds entries to the catalog tables, allocates an initial heap file page, and optionally creates a primary key index. `CREATE INDEX` scans the existing table data and builds a B+Tree index. `DROP TABLE` removes catalog entries, frees all heap pages, and removes associated indexes. All DDL operations must be transactional (rollback on failure).

6. Implement the complete transaction lifecycle end-to-end: `BEGIN` starts a new transaction with a snapshot. Subsequent queries within the transaction see a consistent snapshot (MVCC) and acquire locks (for writes). `COMMIT` flushes WAL records, releases locks, and makes changes visible. `ROLLBACK` undoes all changes. Transactions that encounter errors enter a failed state where only `ROLLBACK` is accepted. Implement auto-commit mode for statements issued outside an explicit transaction.

7. Implement crash recovery that actually works end-to-end: on `Open()`, if a WAL exists, run ARIES recovery (analysis, redo, undo) against the actual heap file pages and indexes via the buffer pool. After recovery, the database must be in a consistent state with all committed transactions' effects present and all uncommitted transactions' effects removed. Checkpoint periodically (configurable interval) to bound recovery time.

8. Write integration tests that exercise the complete system: create tables, insert thousands of rows, query with JOINs and aggregation, update and delete rows, create and use indexes, run concurrent transactions with conflicts, simulate crashes by killing the process after random WAL writes and verifying recovery, connect with `pgx` and execute a realistic workload (e.g., a simplified TPC-C-like scenario with orders, items, and inventory), and benchmark query throughput (target: 1000+ simple SELECTs/second, 500+ INSERTs/second on commodity hardware).

## Hints

- The integration challenge is harder than any individual component. Define clean interfaces between subsystems and test each integration point independently before the full system test.
- For the heap file slotted-page layout, the slot directory grows from the front and tuple data grows from the back. When they meet, the page is full. Deleted tuples leave holes that can be compacted.
- Bootstrap the catalog by hard-coding the initial creation of `sys_tables`, `sys_columns`, and `sys_indexes` (you cannot query the catalog to create the catalog). After bootstrap, all subsequent catalog operations use normal SQL internally.
- For auto-commit, wrap each standalone statement in an implicit `BEGIN`/`COMMIT` pair.
- Crash simulation in tests: write N WAL records, close the database without flushing the buffer pool (simulating a crash where dirty pages were lost), reopen and verify that recovery reconstructs the correct state from the WAL.
- Performance tuning: the buffer pool size, WAL group commit interval, and checkpoint frequency are the main knobs. Start with conservative values and tune based on benchmark results.

## Success Criteria

1. The database can be opened, tables created, data inserted, queried, updated, and deleted through the PostgreSQL wire protocol using `pgx` or `psql`.
2. Concurrent transactions with MVCC produce correct results under snapshot isolation, with write-write conflicts properly detected and reported.
3. After a simulated crash (dirty pages lost, WAL intact), reopening the database and running recovery produces a state consistent with all committed transactions.
4. B+Tree indexes are used by the query planner when appropriate, and indexed queries are measurably faster than full table scans on tables with 10,000+ rows.
5. The system catalog accurately reflects all created/dropped tables, columns, and indexes, and the planner correctly uses it for query compilation.
6. The full TPC-C-like integration test completes without errors, data corruption, or deadlocks.
7. The database achieves at least 1,000 point-SELECT queries/second and 500 INSERT queries/second in single-client benchmarks.
8. All tests pass with the `-race` detector enabled.

## Research Resources

- [Architecture of a Database System (Hellerstein, Stonebraker)](https://dsf.berkeley.edu/papers/fntdb07-architecture.pdf)
- [CMU 15-445 Full Course (Andy Pavlo)](https://15445.courses.cs.cmu.edu/fall2024/)
- [Let's Build a Simple Database (cstack)](https://cstack.github.io/db_tutorial/)
- [How Does a Relational Database Work (Coding Geek)](https://coding-geek.com/how-databases-work/)
- [BoltDB Source Code (Go embedded database)](https://github.com/etcd-io/bbolt)
- [go-mysql-server (SQL engine in Go, for reference)](https://github.com/dolthub/go-mysql-server)
- [rqlite - Distributed SQLite in Go](https://github.com/rqlite/rqlite)
