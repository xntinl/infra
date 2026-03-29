# 114. Log-Structured Merge with Write-Ahead Log

<!--
difficulty: advanced
category: database-internals
languages: [go]
concepts: [wal, lsm-tree, crash-recovery, durability, group-commit, memtable, sstable, write-amplification]
estimated_time: 14-18 hours
bloom_level: evaluate
prerequisites: [go-interfaces, file-io, concurrency, binary-encoding, sorted-data-structures]
-->

## Languages

- Go (1.22+)

## Prerequisites

- File I/O in Go with `os.File`, `bufio.Writer`, explicit `fsync`
- Concurrency with `sync.Mutex`, `sync.RWMutex`, `sync.Cond`, channels
- Binary encoding with `encoding/binary`
- Sorted data structures (Go `btree` package or hand-rolled skip list)
- Understanding of durability guarantees: what survives a process crash vs. a power failure

## Learning Objectives

- **Implement** a Write-Ahead Log that guarantees durability by writing every mutation to a sequential log before applying it to the in-memory memtable
- **Design** WAL recovery that replays committed entries after a crash to reconstruct the memtable state
- **Apply** group commit to batch multiple pending writes into a single fsync, amortizing disk flush cost across concurrent writers
- **Analyze** the relationship between WAL, memtable, and SSTable: how WAL protects the volatile memtable, and when the WAL can be safely truncated
- **Evaluate** the write amplification cost of WAL (data written twice: once to WAL, once to SSTable) versus the durability guarantee it provides
- **Implement** WAL truncation after SSTable flush to prevent unbounded log growth

## The Challenge

Every durable database writes data twice: once to the Write-Ahead Log for crash recovery, and once to the main storage structure. This seems wasteful, but it solves a fundamental problem: the memtable (an in-memory sorted structure) is volatile. If the process crashes before the memtable is flushed to an SSTable on disk, all data in the memtable is lost. The WAL prevents this by recording every write to a sequential append-only file that can be replayed after a crash.

The WAL write path is: (1) append the key-value pair to the WAL file, (2) fsync the WAL file to guarantee it is on disk, (3) apply the write to the memtable, (4) acknowledge to the client. If the process crashes after step 2 but before the memtable is flushed, recovery replays the WAL to reconstruct the memtable.

The critical performance optimization is group commit. Individual fsync calls are expensive (1-5ms on SSD, 5-15ms on HDD). Group commit collects multiple pending writes, appends them all to the WAL in a single batch, issues one fsync, and then unblocks all waiting writers. This amortizes the fsync cost across N writers, turning N fsyncs into 1.

When the memtable is flushed to an SSTable, the WAL entries that correspond to the flushed memtable are no longer needed. The WAL can be truncated (or a new WAL file started) at this point. If there are multiple WAL segments, old segments can be deleted once all their entries have been flushed to SSTables.

Understanding durability levels is important for this challenge:

- **No durability**: writes go only to the memtable. A process crash loses all unflushed data. This is fast but unacceptable for any database that must not lose acknowledged writes.
- **WAL with fsync per write**: every write is durable after acknowledgment. A crash loses zero acknowledged writes. This is the safest mode but limits throughput to the fsync rate of the disk (typically 1-5k/sec on SSD).
- **WAL with group commit**: multiple writes share a single fsync. Acknowledged writes are durable. Throughput scales with concurrency because the fsync cost is amortized. This is what production databases use.
- **WAL with periodic sync**: fsync every N milliseconds. Up to N ms of acknowledged writes can be lost on crash. Trades durability for throughput. PostgreSQL offers this via `synchronous_commit = off`.

This challenge implements the first three levels. The WAL is the difference between "database that handles crashes" and "cache that pretends to be a database."

Build an LSM-tree storage engine with a Write-Ahead Log for durability.

## Requirements

1. Implement a WAL that appends records to a sequential file. Each WAL record contains: `[length:4][crc:4][type:1][key_len:4][val_len:4][key][value]`. The type field distinguishes puts (1) from deletes (2). CRC covers everything after the CRC field
2. Implement `Put(key, value)` that first appends to the WAL, then fsyncs (or batches via group commit), then applies to the memtable. The write is not acknowledged until the WAL fsync completes
3. Implement `Delete(key)` following the same WAL-first protocol, writing a delete record to the WAL and a tombstone to the memtable
4. Implement WAL recovery: on startup, if a WAL file exists, replay all valid records into the memtable. Handle truncated records at the end of the file (partial writes due to crash). After replay, the memtable contains all committed state
5. Implement group commit: when multiple goroutines issue concurrent writes, batch their WAL entries and issue a single fsync. Use a `sync.Cond` or channel to coordinate. Each writer blocks until the batch containing its entry has been fsynced
6. Implement memtable flush to SSTable: when the memtable exceeds a size threshold, freeze it, start a new memtable (and a new WAL segment), flush the frozen memtable to an SSTable file, then delete the old WAL segment. The transition must be atomic from the perspective of concurrent readers and writers
7. Implement `Get(key)` that checks the active memtable, then the frozen memtable (if one exists during flush), then SSTables from newest to oldest
8. Implement WAL segment management: maintain a mapping of WAL segment ID to memtable. When a memtable is fully flushed to an SSTable, its corresponding WAL segment can be deleted. Never delete a WAL segment whose memtable has not been flushed
9. Crash recovery must handle: (a) crash during normal operation (replay WAL), (b) crash during memtable flush (WAL still exists, replay it), (c) crash after flush but before WAL deletion (detect that the SSTable already contains the data, skip duplicate replay)
10. Implement a `ListKeys()` method that returns all live keys by merging keys from the active memtable, frozen memtable, and all SSTables, excluding deleted keys
11. Track and expose write statistics: WAL bytes written, memtable flushes completed, SSTables on disk, average group commit batch size

## Hints

The critical invariant of the write path is: a write is NOT acknowledged to the client until the WAL entry is on stable storage (fsync completed). Breaking this invariant means the database can report a write as successful and then lose it on crash. The sequence matters:

```
1. Serialize record to WAL buffer
2. Write buffer to WAL file
3. fsync WAL file          <-- durability point
4. Apply to memtable       <-- visibility point
5. Acknowledge to client   <-- ONLY after step 3
```

If the process crashes between steps 2 and 3, the write is lost but was never acknowledged -- this is correct. If it crashes between steps 3 and 4, recovery replays the WAL and applies the write to the memtable -- this is also correct. The only dangerous bug is acknowledging before step 3.

For group commit, use a leader-follower pattern: the first writer to arrive becomes the leader. It collects entries from other writers that arrive during a short window (or up to a batch size), writes them all to the WAL, issues fsync, and unblocks all followers. Subsequent writers wait on a `sync.Cond` until the leader signals completion. This is how PostgreSQL, MySQL/InnoDB, and SQLite implement WAL commit.

WAL recovery must be idempotent. If the WAL contains entries that were already flushed to an SSTable (because the crash happened after flush but before WAL deletion), applying them again is harmless since they will be shadowed by the SSTable entries during reads. Alternatively, track the last flushed WAL sequence number in the SSTable metadata and skip entries below that during recovery.

For the memtable, `google/btree` provides a B-tree implementation for Go. Alternatively, use a simple sorted slice with binary search insertion. The memtable does not need to be concurrent-safe if all writes go through a single writer goroutine.

The SSTable format can be minimal for this challenge: a sorted sequence of `[key_len:4][val_len:4][key][value]` entries followed by a footer with the entry count. Point lookups use binary search on the key index. This is simpler than a full SSTable with block compression and bloom filters.

## Acceptance Criteria

- [ ] Put followed by Get returns the correct value
- [ ] Delete removes the key from subsequent Get calls
- [ ] After a simulated crash (kill process, restart), all acknowledged writes are recovered from the WAL
- [ ] Partial writes at the end of the WAL (truncated records) are detected and skipped during recovery
- [ ] Group commit: 8 concurrent writers achieve at least 3x the throughput of sequential writers with individual fsync per write
- [ ] Memtable flush produces a valid SSTable and the corresponding WAL segment is deleted
- [ ] During memtable flush, concurrent reads and writes are not blocked (new writes go to the new memtable)
- [ ] After flush, Get still returns values from the flushed SSTable
- [ ] Multiple flush cycles work correctly: data migrates from WAL to memtable to SSTable
- [ ] WAL segments do not grow unbounded: old segments are deleted after successful flush
- [ ] Recovery after crash during flush: WAL is replayed, producing a correct memtable
- [ ] ListKeys returns all live keys correctly across memtable and SSTables
- [ ] Write statistics accurately report WAL bytes, flush count, and group commit batch sizes
- [ ] WAL record CRC detects corruption: a single flipped bit causes the record to be rejected

## Going Further

- Implement checkpointing: periodically record the current memtable state to a checkpoint file. On recovery, start from the checkpoint and only replay WAL entries after the checkpoint sequence number, reducing recovery time
- Add transaction support: batch multiple puts and deletes into a single WAL entry that is atomically committed or rolled back
- Implement WAL compression: compress WAL records with LZ4 before writing. Measure the trade-off between CPU cost and reduced I/O
- Add a sync mode configuration that allows trading durability for performance: `sync_every_write`, `sync_every_n_ms`, `sync_on_flush_only`
- Implement WAL archiving: instead of deleting old WAL segments after flush, move them to an archive directory for point-in-time recovery
- Build a replication protocol that ships WAL segments to a replica, which replays them to maintain a consistent copy

## Research Resources

- [CMU 15-445: Database Recovery (Andy Pavlo)](https://15445.courses.cs.cmu.edu/fall2024/slides/21-recovery.pdf) -- ARIES recovery, WAL protocols, checkpointing
- [CMU 15-445: Logging Schemes](https://15445.courses.cs.cmu.edu/fall2024/slides/20-logging.pdf) -- physical vs logical logging, WAL structure
- [LevelDB Write Path and WAL](https://github.com/google/leveldb/blob/main/doc/impl.md) -- how LevelDB combines WAL with LSM-tree
- [RocksDB WAL Documentation](https://github.com/facebook/rocksdb/wiki/Write-Ahead-Log) -- production WAL design with group commit
- [SQLite WAL Mode](https://www.sqlite.org/wal.html) -- WAL design for a single-writer embedded database
- [Designing Data-Intensive Applications, Ch. 3 & 7 (Martin Kleppmann)](https://dataintensive.net/) -- durability, crash recovery, and write-ahead logging
- [ARIES: A Transaction Recovery Method (Mohan et al., 1992)](https://cs.stanford.edu/people/chr101/cs345/aries.pdf) -- the foundational paper on WAL-based recovery
- [Andy Pavlo's Recovery Lecture (YouTube)](https://www.youtube.com/watch?v=S9nctHdkggk) -- full CMU 15-445 lecture on database recovery
- [WiredTiger WAL Implementation (MongoDB)](https://source.wiredtiger.com/develop/arch-log.html) -- WAL architecture in MongoDB's storage engine
- [InnoDB Redo Log Design](https://dev.mysql.com/doc/refman/8.0/en/innodb-redo-log.html) -- MySQL's approach to write-ahead logging and crash recovery
- [etcd WAL Design](https://github.com/etcd-io/etcd/tree/main/server/storage/wal) -- Go-based WAL implementation in the distributed key-value store
- [PostgreSQL WAL Internals](https://www.postgresql.org/docs/current/wal-internals.html) -- detailed documentation on PostgreSQL's WAL record format and recovery mechanics
