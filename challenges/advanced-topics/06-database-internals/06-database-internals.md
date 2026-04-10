# Database Internals — Reference Overview

> This section bridges the gap between knowing how to call a database API and understanding the storage engine decisions that determine why your queries are fast, your transactions are safe, and your disk usage triples under heavy write load.

## Why This Section Matters

Every production database problem is a storage problem in disguise. A query that takes 300ms instead of 3ms is an I/O problem — the optimizer chose a sequential scan over an index because the cardinality estimate was wrong. A PostgreSQL table that grows to 100GB despite only 20GB of live data is an MVCC problem — dead row versions accumulate faster than VACUUM can collect them. A RocksDB instance where write throughput collapses under sustained load is a compaction problem — the LSM tree's write amplification factor hit 30x and the disk can no longer keep up. A distributed database that silently returns stale reads is an isolation level problem — READ COMMITTED was configured where REPEATABLE READ was required.

These are not edge cases. They are the daily reality of running databases in production, and they are invisible if you only know the query layer. The engineers who diagnose and fix them share a common foundation: they have a working mental model of what the storage engine is doing at the page level, at the WAL level, at the buffer pool level. This section builds that foundation.

The Go and Rust implementations are not toys. They show the binary layout of actual on-disk structures, the locking strategies that real engines use, and the places where the language's memory model forces you to reason carefully. After working through this section, you will be able to read the PostgreSQL source code, understand a RocksDB compaction log, and make storage engine selection decisions with justified confidence.

## Subtopics

| # | Topic | Key Concepts | Est. Reading | Difficulty |
|---|-------|-------------|-------------|------------|
| 1 | [B-Tree and Variants](./01-btree-and-variants/01-btree-and-variants.md) | B+ tree, CoW B-tree, fractal tree, page splits, range scans | 75 min | advanced |
| 2 | [LSM-Tree and SSTable](./02-lsm-tree-and-sstable/02-lsm-tree-and-sstable.md) | MemTable, SSTable format, Bloom filters, compaction strategies, write amplification | 90 min | advanced |
| 3 | [Write-Ahead Log](./03-write-ahead-log/03-write-ahead-log.md) | WAL record format, LSN, group commit, physiological logging, crash recovery | 70 min | advanced |
| 4 | [Buffer Pool and Page Cache](./04-buffer-pool-and-page-cache/04-buffer-pool-and-page-cache.md) | clock replacement, dirty page tracking, double-write buffer, eviction policies | 65 min | advanced |
| 5 | [MVCC and Concurrency Control](./05-mvcc-and-concurrency-control/05-mvcc-and-concurrency-control.md) | xmin/xmax, snapshot isolation, MVCC garbage collection, CockroachDB HLC | 80 min | advanced |
| 6 | [Query Optimization](./06-query-optimization/06-query-optimization.md) | cost-based optimizer, cardinality estimation, join ordering, System R approach | 85 min | advanced |
| 7 | [Columnar Storage](./07-columnar-storage/07-columnar-storage.md) | column encoding, RLE, dictionary encoding, PAX layout, vectorized execution | 75 min | advanced |
| 8 | [Transaction Isolation Levels](./08-transaction-isolation-levels/08-transaction-isolation-levels.md) | read phenomena, SQL isolation levels, Snapshot Isolation, SSI | 70 min | advanced |

## Storage Engine Decision Map

Choosing the wrong storage engine for a workload is a category error that no amount of tuning will fix. Use this map before reaching for a specific engine.

```
                     WORKLOAD
                         │
          ┌──────────────┼──────────────┐
          │              │              │
        OLTP          MIXED           OLAP
    (point reads,   (moderate)     (aggregations,
    writes, txns)                  full scans)
          │              │              │
          ▼              ▼              ▼
    ┌─────────┐    ┌─────────┐   ┌──────────┐
    │  B+Tree │    │LSM Tree │   │ Columnar │
    │         │    │         │   │ Storage  │
    │ InnoDB  │    │ RocksDB │   │ClickHouse│
    │Postgres │    │Cassandra│   │  DuckDB  │
    │ SQLite  │    │  TiKV   │   │  Parquet │
    └─────────┘    └─────────┘   └──────────┘
          │              │
          ▼              ▼
   Read amplification  Write amplification
   is low (1-3 I/Os    is high (10-30x for
   per point lookup)   leveled compaction)
   Write amplification Read amplification
   is moderate (2-5x)  is higher (multiple
                       SSTable levels)

SPECIAL CASES:
  ┌─────────────────────────────────────────┐
  │  Append-only workload (time series,     │
  │  event log): LSM with FIFO compaction   │
  │  or dedicated TSDB (InfluxDB, TimescaleDB)
  ├─────────────────────────────────────────┤
  │  Read-heavy, immutable data: B+Tree or  │
  │  CoW B-tree (LMDB) — zero WAL overhead  │
  ├─────────────────────────────────────────┤
  │  Multi-version history queries: MVCC    │
  │  matters — PostgreSQL or CockroachDB    │
  └─────────────────────────────────────────┘
```

### The Three Amplification Factors

Every storage engine makes a three-way tradeoff. You cannot minimize all three simultaneously:

| Engine | Write Amplification | Read Amplification | Space Amplification |
|--------|--------------------|--------------------|---------------------|
| B+Tree | Low (1-2x) | Low (1-3 I/Os) | Moderate (page fragmentation) |
| LSM Leveled | High (10-30x) | Low (1-2 levels) | Low (1.1x) |
| LSM Size-Tiered | Low (2-5x) | High (all tiers) | High (2x peak) |
| Columnar | Very high (rewrite on update) | Very low (skip columns) | Very low (compression) |

Understanding which amplification factor matters for your workload is the first question storage engineers ask. A write-heavy SSD workload cares deeply about write amplification (SSD wear). A read-heavy analytical workload cares about read amplification and space efficiency for compression.

## Dependency Map

```
WAL ─────────────────────────────► Buffer Pool
(WAL provides durability;          (Buffer pool determines
buffer pool is what WAL            what gets written to disk
protects against losing)           and when)
       │                                │
       ▼                                ▼
B+Tree / LSM Tree  ◄────────────── Page Cache
(indexes live in pages;            (shared between WAL
page format determines             and data pages)
WAL record content)
       │
       ▼
MVCC  ─────────────────────────────► Transaction Isolation
(MVCC is the mechanism;            (isolation levels define
isolation levels are               the visibility semantics
the policy it enforces)            MVCC must implement)
       │
       ▼
Query Optimizer  ◄──────────────── Columnar Storage
(optimizer must know               (columnar changes the
storage layout to                  cost model entirely:
produce correct cost               full scan becomes cheap)
estimates)
```

**Recommended read order for a first pass:**

1. Write-Ahead Log (foundational — everything else depends on WAL for durability)
2. Buffer Pool and Page Cache (how pages move between disk and memory)
3. B-Tree and Variants (the dominant OLTP index structure)
4. LSM-Tree and SSTable (the dominant write-optimized structure)
5. MVCC and Concurrency Control (how concurrent readers and writers coexist)
6. Transaction Isolation Levels (the policy layer over MVCC)
7. Query Optimization (how the query layer interacts with storage choices)
8. Columnar Storage (a completely different storage model for OLAP)

## Time Investment

- **Survey** (Mental Model + comparison tables only, all 8 subtopics): ~8h
- **Working knowledge** (read fully + run both implementations per subtopic): ~20h
- **Mastery** (all exercises + further reading per subtopic): ~80-120h

## Prerequisites

Before starting this section you should be comfortable with:

- **Systems programming**: File I/O syscalls (`read`, `write`, `fsync`, `mmap`); page-aligned memory allocation; understanding of what a disk sector and OS page are
- **Data structures**: B-trees at the level of knowing split and merge operations; hash tables; sorted arrays and binary search
- **Concurrency**: Mutexes, read-write locks, compare-and-swap; understanding of what a race condition looks like in practice
- **Go**: `os.File`, `[]byte` slices, `sync.RWMutex`, `encoding/binary` for byte-level serialization
- **Rust**: Ownership and borrowing; `std::fs::File`; `unsafe` for raw pointer arithmetic and aligned allocation; `memmap2` crate basics
- **Recommended prior sections**: Advanced Data Structures (skip lists appear in LSM MemTable implementations; persistent data structures explain CoW B-trees)
