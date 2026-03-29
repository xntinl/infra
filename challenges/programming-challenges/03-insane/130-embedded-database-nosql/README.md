# 130. Embedded NoSQL Database

<!--
difficulty: insane
category: databases-storage
languages: [rust]
concepts: [b-plus-tree, mvcc, wal, acid-transactions, key-value-store, document-storage, compaction, secondary-indexes]
estimated_time: 30-40 hours
bloom_level: create
prerequisites: [b-trees, concurrency, file-io, serialization, json-parsing, memory-mapped-files, crash-recovery]
-->

## Languages

- Rust (1.75+ stable)

## Prerequisites

- B-tree data structures and disk-based index design
- File I/O, `mmap`, and fsync semantics
- ACID properties and isolation levels
- Write-ahead logging concepts
- JSON serialization/deserialization
- Concurrency primitives (Mutex, RwLock, atomics)

## Learning Objectives

By the end of this challenge you will be able to **create** a fully functional embedded document database that provides ACID transactions, crash recovery, and efficient range queries -- all embeddable as a Rust library with no external dependencies beyond `serde` and `serde_json`.

## The Challenge

Build an embedded database engine combining key-value storage with document (JSON) capabilities. The storage layer uses a B+ tree for ordered key lookups and range scans. A write-ahead log guarantees crash recovery. Multi-version concurrency control (MVCC) provides snapshot isolation for concurrent readers and writers. The engine supports batch writes, secondary indexes on JSON fields, prefix scans, compaction, and an iterator interface.

This is not a wrapper around SQLite or a HashMap persisted to disk. You are implementing the storage engine itself: page management, tree balancing, transaction isolation, and crash recovery from the WAL. The database must survive `kill -9` at any point during a write and recover to a consistent state on restart.

## Requirements

- [ ] B+ tree index with configurable page size (default 4096 bytes), supporting insert, delete, point lookup, and range queries
- [ ] Key-value API: `put(key, value)`, `get(key)`, `delete(key)`, `scan(start..end)`
- [ ] Document API: `insert_doc(collection, doc)`, `find(collection, query)` where documents are JSON objects with auto-generated IDs
- [ ] ACID transactions: `begin()`, `commit()`, `rollback()` with snapshot isolation via MVCC
- [ ] Write-ahead log: all mutations logged before applied; replay on crash recovery
- [ ] Batch writes: atomic multi-key operations that either all succeed or all roll back
- [ ] Secondary indexes: create index on a JSON field path, used automatically by `find` queries
- [ ] Prefix scan: `scan_prefix(prefix)` returns all keys sharing a byte prefix
- [ ] Compaction: merge old MVCC versions, reclaim dead pages, shrink the data file
- [ ] Snapshot reads: read from a consistent point-in-time without blocking writers
- [ ] Iterator interface: `Iter` that yields `(key, value)` pairs lazily, holding a read snapshot
- [ ] Embeddable: the entire engine is a library crate with `Database::open(path)` as entry point
- [ ] Crash safety: database recovers correctly after simulated crash at any write stage
- [ ] No `unsafe` code (or justified and isolated if absolutely necessary for mmap)

## Hints

1. Separate the page cache from the B+ tree logic. The B+ tree operates on logical page IDs; a page manager handles reading/writing physical pages to disk. This separation lets you swap storage strategies (file-backed, mmap, in-memory for tests) without touching the tree code.

2. MVCC timestamps do not need wall-clock time. Use a monotonically increasing `u64` transaction ID. Each value is tagged with `(created_at_txn, deleted_at_txn)`. A reader at snapshot `T` sees values where `created_at <= T` and `(deleted_at > T or deleted_at == 0)`.

3. The WAL is append-only and sequential -- the simplest file format possible. Each record is: `[length: u32][txn_id: u64][operation: u8][key_len: u32][key][value_len: u32][value][crc32: u32]`. On recovery, replay all committed transactions; discard incomplete ones.

## Acceptance Criteria

- [ ] B+ tree handles 100K insertions and maintains O(log n) lookup performance
- [ ] Range queries return keys in sorted order with correct bounds
- [ ] Transactions provide snapshot isolation (concurrent reader sees consistent state)
- [ ] WAL replay recovers all committed data after simulated crash
- [ ] Batch writes are atomic (partial failure leaves no trace)
- [ ] Secondary index accelerates field-based document queries
- [ ] Compaction reclaims space from deleted/overwritten values
- [ ] Iterator holds a stable snapshot even as writes occur concurrently
- [ ] All operations are accessible through a clean library API
- [ ] `cargo test` passes with unit and integration tests

## Research Resources

- [Database Internals (Alex Petrov)](https://www.databass.dev/) -- B-trees, WAL, MVCC, and compaction covered in depth
- [CMU 15-445 Database Systems](https://15445.courses.cs.cmu.edu/) -- buffer pool management, B+ trees, concurrency control lectures
- [Architecture of a Database System (Hellerstein et al.)](https://dsf.berkeley.edu/papers/fntdb07-architecture.pdf) -- end-to-end database architecture
- [The Design of Postgres Storage System](https://dsf.berkeley.edu/papers/ERL-M87-06.pdf) -- MVCC design origin
- [BoltDB design doc](https://github.com/boltdb/bolt) -- single-file B+ tree database, excellent simplicity reference
- [RocksDB wiki](https://github.com/facebook/rocksdb/wiki) -- LSM tree compaction strategies (contrast with B+ tree approach)
- [Rust `serde_json` crate](https://docs.rs/serde_json/latest/serde_json/) -- JSON serialization for document storage
