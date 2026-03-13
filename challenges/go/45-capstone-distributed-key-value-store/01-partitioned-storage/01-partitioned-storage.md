<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 3h
-->

# Partitioned Storage Engine

## The Challenge

Build a partitioned key-value storage engine that distributes data across multiple virtual nodes using consistent hashing. Your engine must support a configurable number of virtual nodes per physical node to ensure even data distribution, handle partition ownership changes when nodes join or leave, and persist data to disk using a log-structured merge tree (LSM) approach within each partition. The storage engine must support concurrent reads and writes across partitions without global locking, implement partition-level write-ahead logging for crash recovery, and provide an iterator interface that can scan across partition boundaries in key order. This is the foundational layer of a distributed key-value store -- every subsequent exercise in this capstone builds on the primitives you create here.

## Requirements

1. Implement consistent hashing with configurable virtual nodes (default 256 per physical node) using a hash ring backed by a sorted slice with binary search for O(log n) lookups.
2. Build a partition-level storage engine using an in-memory skip list or red-black tree as the memtable, flushing to sorted string table (SSTable) files on disk when the memtable exceeds a configurable size threshold (default 4 MB).
3. Implement write-ahead logging (WAL) per partition that records every mutation before it is applied to the memtable, supporting replay on crash recovery to restore unflushed data.
4. Support `Put(key, value, timestamp)`, `Get(key)`, `Delete(key, timestamp)`, and `Scan(startKey, endKey)` operations where deletes are tombstone-based and timestamps enable last-write-wins conflict resolution.
5. Implement SSTable compaction using a size-tiered strategy that merges SSTables of similar size, discarding superseded values and expired tombstones older than a configurable grace period.
6. Handle partition reassignment when physical nodes are added or removed by streaming key ranges from the old owner to the new owner without blocking concurrent reads on unaffected partitions.
7. Provide a cross-partition `Scan` iterator that merges results from multiple partition iterators in key order using a min-heap, correctly handling tombstones and duplicate keys across partitions.
8. All partition operations must be safe for concurrent use from multiple goroutines, using per-partition read-write locks for memtable access and lock-free reads on immutable SSTable files.

## Hints

- Use `crypto/sha256` or `xxhash` for consistent hashing; the hash ring is just a sorted slice of `uint64` tokens mapped to partition IDs.
- The memtable can be a simple sorted map (`sync.Map` is unsuitable for ordered iteration -- use a skip list or a `btree` package).
- SSTables on disk should include an index block at the end for binary search without scanning the entire file.
- Write-ahead log entries need a CRC32 checksum to detect corruption during replay.
- For partition reassignment, snapshot the memtable and stream SSTables as files rather than re-reading every key.
- Use `container/heap` for the merge iterator across partitions.
- Tombstone grace period prevents resurrection of deleted keys during compaction; a typical value is 10 seconds for testing, 24 hours in production.

## Success Criteria

1. A 3-node cluster with 256 virtual nodes each distributes 100,000 random keys with less than 10% standard deviation across partitions.
2. Crash recovery restores all committed writes by replaying the WAL with zero data loss after a simulated `SIGKILL`.
3. SSTable compaction reduces total disk usage by at least 50% after overwriting the same 1,000 keys 100 times each.
4. Cross-partition `Scan` returns keys in strictly sorted order across all partitions.
5. Adding a fourth node to a 3-node cluster migrates approximately 25% of partitions without disrupting reads on non-migrating partitions.
6. Concurrent writes from 64 goroutines complete without data races (verified by `go test -race`).
7. `Get` latency on a dataset of 1 million keys stays below 1 ms at the 99th percentile.

## Research Resources

- "Dynamo: Amazon's Highly Available Key-Value Store" (DeCandia et al., 2007) -- consistent hashing and virtual nodes
- "Bigtable: A Distributed Storage System for Structured Data" (Chang et al., 2006) -- LSM tree design and SSTable format
- "LevelDB implementation documentation" -- https://github.com/google/leveldb/blob/main/doc/impl.md
- "Consistent Hashing and Random Trees" (Karger et al., 1997) -- original consistent hashing paper
- Go `encoding/binary` package for SSTable serialization
- Go `container/heap` package for merge iterators
