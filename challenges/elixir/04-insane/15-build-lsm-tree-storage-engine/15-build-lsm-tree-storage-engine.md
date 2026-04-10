# 15. Build an LSM-Tree Storage Engine

**Difficulty**: Insane

## Prerequisites

- Mastered: ETS, file I/O (`:file`, `File`, `IO.binwrite`), binary pattern matching, bitstrings
- Mastered: GenServer, Supervisor, sorted data structures, binary search
- Familiarity with: LSM-tree paper (O'Neil et al. 1996), LevelDB implementation, Bloom filter mathematics, MVCC

## Problem Statement

Implement a production-grade Log-Structured Merge-tree (LSM-tree) storage engine in pure
Elixir. The engine must provide key-value storage with durability, crash recovery, and
the read/write performance characteristics that make LSM-trees suitable for write-heavy
workloads:

1. All writes go to an in-memory MemTable backed by a sorted ETS table. The MemTable
   accepts `put(key, value)` and `delete(key)` operations.
2. Every write is also appended to a Write-Ahead Log (WAL) on disk before the in-memory
   ack is returned. On process restart, the WAL is replayed to reconstruct the MemTable.
3. When the MemTable exceeds a configurable size threshold, it is flushed to disk as an
   immutable SSTable file. The SSTable is written in sorted key order with a footer
   containing a key index for binary search lookups.
4. Implement compaction: when the number of SSTables at a level exceeds a threshold,
   merge adjacent SSTables into a single larger SSTable at the next level, discarding
   superseded versions and tombstones for deleted keys.
5. Each SSTable is accompanied by a Bloom filter persisted alongside the data file.
   On reads, check the Bloom filter before performing disk I/O; skip the SSTable if the
   filter reports the key is absent.
6. Support `scan(from_key, to_key)` that returns a lazy stream of `{key, value}` pairs
   in sorted order, merging MemTable and all SSTable levels correctly.
7. Implement snapshot isolation for reads: a `snapshot()` call returns a handle; all
   reads through that handle see a consistent view of the data at that moment, even
   if compaction runs concurrently.
8. Meet the following performance targets on an M-series Mac or equivalent: 100k
   sequential writes/second, 200k random reads/second with a warm Bloom filter cache,
   tested against a dataset that does not fit in RAM.
9. Validate checksums: each SSTable block is written with a CRC32 checksum. On read,
   verify the checksum and return `{:error, :corruption}` on mismatch.
10. Implement compression: SSTable data blocks are compressed with LZ4 or zstd using
    a NIF or a pure-Elixir implementation. Compression ratio must be reported in metrics.

## Acceptance Criteria

- [ ] `Engine.put(engine, key, value)` returns `:ok` only after the WAL write is fsynced to disk.
- [ ] Killing the engine process and restarting it replays the WAL and recovers all data
      written before the kill without any manual intervention.
- [ ] After 500k sequential writes, all data is readable with `Engine.get/2` returning
      the correct latest value.
- [ ] `Engine.delete(engine, key)` inserts a tombstone; subsequent `get` returns `{:error, :not_found}`;
      the tombstone disappears after compaction when no older SSTable can resurface the key.
- [ ] `Engine.scan(engine, "a", "m")` returns all keys in `["a", "m")` range in order,
      including keys split across MemTable and multiple SSTable levels.
- [ ] Bloom filter false positive rate is within 1% of the configured target at the
      designed capacity.
- [ ] A `snapshot()` handle opened before a compaction run reads the pre-compaction data
      correctly even after compaction completes.
- [ ] CRC32 mismatch on any SSTable block causes `{:error, :corruption}` — not a crash,
      not silent data loss.
- [ ] Benchmark results (sequential writes, random reads) are within 20% of stated targets
      on the test machine, documented with dataset size and hardware.
- [ ] All SSTables at a level are compacted into the next level when count exceeds the threshold;
      total SSTable count does not grow unboundedly during a sustained write workload.

## What You Will Learn

- The write path of an LSM-tree: MemTable → WAL → SSTable → compaction levels
- Bloom filter construction: hash functions, bit array sizing, false positive rate tradeoff
- The merge algorithm for sorted runs: k-way merge using a priority queue
- MVCC snapshot isolation: how version vectors or epoch numbers fence reads from concurrent compaction
- Binary file format design: block structure, key index, footer, checksum layout
- The compaction scheduling problem: when to compact, which files to pick, how to avoid write amplification

## Hints

This exercise is intentionally sparse. Research:

- WAL format: each entry is `<<crc32::32, key_len::32, val_len::32, key::binary, value::binary>>`; truncate on partial write
- SSTable index: store `{key, byte_offset}` pairs in the footer; use binary search on the key list to find the correct block
- Bloom filter: use two independent hash functions derived from `:erlang.phash2/2` with different seeds; store as a bitstring
- Compaction trigger: maintain an ETS table of `{level, file_path, min_key, max_key, size}` records; compact when `count(level) > threshold`
- For MVCC snapshots, assign a monotonic sequence number to each MemTable flush; a snapshot holds the sequence number at creation time

## Reference Material

- LSM-tree original paper: O'Neil et al., "The Log-Structured Merge-Tree", Acta Informatica, 1996
- LevelDB implementation notes: https://github.com/google/leveldb/blob/main/doc/impl.md
- RocksDB tuning guide: https://github.com/facebook/rocksdb/wiki/RocksDB-Tuning-Guide
- Bloom filter design: https://en.wikipedia.org/wiki/Bloom_filter (focus on optimal k and m/n ratio)
- "Designing Data-Intensive Applications" — Martin Kleppmann, Chapter 3

## Difficulty Rating

★★★★★★

## Estimated Time

60–90 hours
