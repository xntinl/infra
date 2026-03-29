# 82. LSM-Tree Compaction Engine

<!--
difficulty: advanced
category: database-internals
languages: [rust]
concepts: [lsm-tree, memtable, sstable, compaction, bloom-filters, tombstones, range-scans, merge-sort]
estimated_time: 14-18 hours
bloom_level: evaluate
prerequisites: [rust-ownership, btreemap, file-io, binary-search, iterators, basic-probability]
-->

## Languages

- Rust (stable)

## Prerequisites

- Rust ownership and borrowing (managing memtable references during flush)
- `BTreeMap` for sorted in-memory storage
- File I/O with `std::fs::File`, `BufWriter`, `BufReader`
- Binary search for SSTable lookups
- Iterator trait implementation for merge iterators
- Basic probability: understanding false positive rates for bloom filters

## Learning Objectives

- **Implement** a Log-Structured Merge tree with an in-memory memtable and on-disk sorted string tables
- **Design** both tiered and leveled compaction strategies and analyze their trade-offs for read amplification, write amplification, and space amplification
- **Apply** bloom filters to eliminate unnecessary SSTable reads during point lookups
- **Implement** a multi-way merge iterator that combines data from the memtable and multiple SSTable levels while respecting tombstones
- **Evaluate** how compaction policies affect tail latency, disk usage, and read performance under different workloads
- **Analyze** the write amplification cost of leveled compaction versus the read amplification cost of tiered compaction

## The Challenge

The Log-Structured Merge tree is the storage engine behind LevelDB, RocksDB, Cassandra, HBase, and most modern write-optimized databases. Its design principle is simple: buffer writes in a sorted in-memory structure (the memtable), flush it to disk as a sorted immutable file (an SSTable) when it reaches a size threshold, and periodically merge SSTables to bound read amplification.

The memtable is typically a skip list or balanced tree that supports O(log N) inserts and lookups. When the memtable reaches its size limit, it becomes immutable, a new empty memtable takes its place, and a background thread flushes the immutable memtable to disk as a new SSTable. Each SSTable is a sorted sequence of key-value pairs with an index block for binary search and a bloom filter for fast negative lookups.

The critical design decision is the compaction strategy. **Tiered compaction** (used by Cassandra, HBase) groups SSTables into size tiers and merges all tables in a tier when the tier is full. It optimizes for write throughput but causes read amplification because a key might exist in any tier. **Leveled compaction** (used by LevelDB, RocksDB) organizes SSTables into levels with non-overlapping key ranges per level (except L0). Compaction picks an SSTable from level L, merges it with overlapping SSTables in level L+1, and writes the result back to L+1. This bounds read amplification but increases write amplification.

Deletes in an LSM-tree do not remove data immediately. Instead, a tombstone marker is written. During compaction, when a tombstone reaches the bottom level, the key is finally dropped. Until then, the tombstone must shadow all older versions of the key.

The three dimensions of amplification in an LSM-tree are:

- **Write amplification (W)**: how many times each byte is written to disk across its lifetime. In leveled compaction with a size ratio of 10, a key-value pair may be rewritten 10 times per level as it is compacted downward. With 4 levels, worst-case write amplification is ~40x.
- **Read amplification (R)**: how many SSTables must be consulted for a single point lookup. In tiered compaction, every tier may contain a relevant SSTable. With 4 tiers of 4 tables each, read amplification is up to 16 SSTables (mitigated by bloom filters).
- **Space amplification (S)**: how much disk space is consumed relative to the logical data size. Tiered compaction may temporarily store multiple copies of the same key across tiers, causing 2-10x space amplification during compaction.

The RUM conjecture (Read, Update, Memory) formalizes this: no storage engine can simultaneously be optimal for reads, writes, and space. An LSM-tree sacrifices read performance (multiple levels to check) and space (old versions coexist across levels) to optimize writes (sequential appends). A B+ tree optimizes reads (single tree traversal) at the cost of random write I/O (updating pages in place).

These three dimensions form a fundamental trade-off: improving one necessarily worsens at least one other. The compaction strategy determines where on this trade-off surface the engine operates.

Build an LSM-tree storage engine with configurable compaction.

## Requirements

1. Implement a memtable using `BTreeMap<Vec<u8>, Option<Vec<u8>>>` where `None` represents a tombstone. Support `put(key, value)`, `get(key)`, and `delete(key)` operations. Track the memtable's approximate byte size and freeze it when it exceeds a configurable threshold (default 4 MB)
2. Implement SSTable file format: a sorted data block of key-value entries, followed by an index block mapping key prefixes to data block offsets, followed by a bloom filter, followed by a footer with block offsets and entry count. Each entry is encoded as `[key_len:4][value_len:4][key][value]` with a flag byte distinguishing values from tombstones
3. Implement SSTable writer that takes a sorted iterator and produces the file format above. Implement SSTable reader with `get(key)` using the bloom filter for early rejection and binary search on the index block, plus a `scan(start, end)` iterator
4. Implement a bloom filter with configurable false positive rate (default 1%). Use double hashing with two independent hash functions. Serialize the bit vector and hash function count into the SSTable footer
5. Implement tiered compaction: maintain T tiers (default 4). Flushed SSTables enter tier 0. When tier K has more than a configurable number of tables (default 4), merge all tables in tier K into a single table in tier K+1. The bottom tier has no size limit
6. Implement leveled compaction: L0 holds flushed SSTables (may overlap). When L0 has 4+ tables, pick one and merge with all overlapping tables in L1. Levels L1+ have non-overlapping key ranges. When level L exceeds its size limit (10^L * base_size), pick the table with the most overlap into L+1 and merge
7. Implement a merge iterator that merges entries from the active memtable, the frozen memtable (if being flushed), and all SSTables across levels. For duplicate keys, the most recent version wins. Tombstones suppress older values. Expose this as `scan(start, end) -> Iterator<(Key, Value)>`
8. Implement `get(key)` that checks memtable, then frozen memtable, then SSTables from newest to oldest level. Use bloom filters to skip SSTables that definitely do not contain the key
9. Background compaction: run compaction in a separate thread triggered when level sizes exceed thresholds. Compaction must not block reads or writes (except briefly to swap the memtable reference)
10. Implement tombstone-aware compaction: tombstones at the bottom level can be dropped (no older version exists below). Tombstones at non-bottom levels must be preserved to shadow older versions in lower levels. Track which level is the bottom level and drop tombstones only during bottom-level compaction

## Hints

Understanding the SSTable file format is critical. An SSTable has four sections laid out sequentially:

```
[Data Block: sorted key-value entries]
[Index Block: key -> offset mappings for binary search]
[Bloom Filter: serialized bit array + hash count]
[Footer: offsets to each section + entry count + magic number]
```

The reader first reads the footer (fixed size at end of file), then loads the bloom filter and index block into memory. For a point lookup, the bloom filter is checked first. If it returns false, the key definitely does not exist in this SSTable. If true, the index block is binary-searched to find the data block offset, then the data block is read from disk. This design ensures that negative lookups (the most common case) require zero data block I/O.

The LSM-tree has three dimensions of amplification that trade off against each other: write amplification (how many times each byte is written to disk), read amplification (how many SSTables must be checked for a point lookup), and space amplification (how much disk space is used relative to the logical data size). Tiered compaction minimizes write amplification at the cost of higher read and space amplification. Leveled compaction minimizes read and space amplification at the cost of higher write amplification.

For the bloom filter, use the formula `m = -n * ln(p) / (ln(2))^2` for the number of bits given n keys and false positive rate p, and `k = (m/n) * ln(2)` for the optimal number of hash functions. Double hashing generates k hash values from two base hashes: `h_i(x) = h1(x) + i * h2(x)`.

The merge iterator is the hardest part. Use a min-heap (binary heap) of iterators, each pointing to their current entry. Pop the minimum key, advance that iterator, and push it back if not exhausted. When multiple iterators have the same key, take the one from the newest source and skip the rest.

For leveled compaction, the key insight is that levels L1+ have non-overlapping key ranges. This means a point lookup needs to check at most one SSTable per level (plus bloom filter). L0 is special because flushed memtables may have overlapping ranges, so all L0 tables must be checked.

## Acceptance Criteria

- [ ] Point lookups return correct values after arbitrary sequences of puts, deletes, and overwrites
- [ ] Deleted keys return `None` even when older versions exist in lower SSTable levels
- [ ] Range scans return all live keys in sorted order, correctly merging across memtable and all SSTable levels
- [ ] Bloom filters reject at least 99% of lookups for non-existent keys (measure false positive rate empirically)
- [ ] Tiered compaction merges SSTables when a tier exceeds its table count limit
- [ ] Leveled compaction maintains non-overlapping key ranges in levels L1+
- [ ] After compaction, all data remains accessible and correct
- [ ] Memtable freezes and flushes to a new SSTable when exceeding the size threshold
- [ ] Concurrent reads and writes operate without data races or deadlocks
- [ ] Write throughput exceeds 100k ops/sec for 256-byte values on local SSD
- [ ] Point lookup latency stays under 1ms for a dataset of 1 million keys after compaction
- [ ] Tombstones at the bottom level are dropped during compaction; tombstones at non-bottom levels are preserved
- [ ] SSTable file format includes magic number validation: corrupted files are detected on open
- [ ] Multiple sequential flush-compact cycles produce a valid tree with all data accessible

## Going Further

- Implement prefix compression within SSTable data blocks: consecutive keys sharing a common prefix store only the differing suffix, reducing SSTable size by 30-50% for keys with hierarchical structure
- Add block-level compression (LZ4 or snappy) to SSTables and measure the trade-off between CPU overhead and reduced I/O
- Implement a universal compaction strategy that generalizes both tiered and leveled by allowing partial merges
- Build a rate limiter for compaction I/O to prevent compaction from starving foreground reads and writes
- Implement key-value separation (WiscKey style): store only keys in the LSM-tree, values in a separate value log with garbage collection
- Add a write-ahead log (WAL) for crash recovery of the memtable (see challenge 114)

## Research Resources

- [CMU 15-445: Storage Models & Compression (Andy Pavlo)](https://15445.courses.cs.cmu.edu/fall2024/slides/04-storage2.pdf) -- LSM-trees in the context of database storage
- [The Log-Structured Merge-Tree (O'Neil et al., 1996)](https://www.cs.umb.edu/~poneil/lsmtree.pdf) -- the original LSM-tree paper
- [LevelDB Implementation Notes](https://github.com/google/leveldb/blob/main/doc/impl.md) -- Google's concise description of LevelDB's LSM design
- [RocksDB Compaction Wiki](https://github.com/facebook/rocksdb/wiki/Compaction) -- detailed comparison of tiered vs leveled compaction in production
- [Designing Data-Intensive Applications, Ch. 3 (Martin Kleppmann)](https://dataintensive.net/) -- SSTables, LSM-trees, and compaction explained for practitioners
- [Database Internals by Alex Petrov, Ch. 7](https://www.databass.dev/) -- deep dive into LSM-tree compaction strategies
- [Monkey: Optimal Navigable Key-Value Store (Dayan et al., 2017)](https://stratos.seas.harvard.edu/files/stratos/files/monkeyspaper.pdf) -- optimal bloom filter allocation across LSM levels
- [CMU 15-445: Lecture 6 - Storage (YouTube)](https://www.youtube.com/watch?v=n1FnhGHIzGo) -- Andy Pavlo explaining LSM storage engines
- [WiscKey: Separating Keys from Values in SSD-Conscious Storage](https://www.usenix.org/system/files/conference/fast16/fast16-papers-lu.pdf) -- key-value separation optimization for LSM-trees on SSDs
- [Dostoevsky: Better Space-Time Trade-Offs for LSM-Tree Based Key-Value Stores (Dayan & Idreos, 2018)](https://stratos.seas.harvard.edu/files/stratos/files/dostoevskypaper.pdf) -- lazy leveling and fluid LSM-tree variants
- [LSM-Based Storage Techniques: A Survey (Luo & Carey, 2020)](https://www.vldb.org/pvldb/vol13/p3217-luo.pdf) -- comprehensive survey of LSM-tree optimizations across production systems
- [Building a Simple LSM-Tree (GitHub)](https://github.com/facebook/rocksdb/wiki/RocksDB-Overview) -- RocksDB wiki overview with architectural diagrams
