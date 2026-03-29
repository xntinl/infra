# 79. Bitcask Log-Structured Store

<!--
difficulty: intermediate-advanced
category: database-internals
languages: [go]
concepts: [log-structured-storage, append-only-writes, keydir-hash-index, compaction, crash-recovery, hint-files]
estimated_time: 8-12 hours
bloom_level: apply
prerequisites: [go-interfaces, file-io, hash-maps, binary-encoding, concurrency-basics]
-->

## Languages

- Go (1.22+)

## Prerequisites

- File I/O in Go (`os.File`, `io.Reader`, `io.Writer`)
- Binary encoding with `encoding/binary` for fixed-size records
- Hash maps and their performance characteristics
- Basic concurrency with `sync.RWMutex`
- Understanding of append-only data structures and why they simplify crash recovery

## Learning Objectives

- **Implement** a Bitcask storage engine with append-only log files and an in-memory hash index
- **Apply** the log-structured storage pattern where writes never modify existing data on disk
- **Design** a compaction process that merges old log files while the system remains available for reads and writes
- **Analyze** how hint files accelerate startup by avoiding full log replay
- **Evaluate** the trade-offs of Bitcask's design: fast writes and predictable reads at the cost of keys fitting in memory

## The Challenge

Bitcask is one of the most elegant storage engine designs ever published. Created by the Riak team at Basho Technologies, it achieves high write throughput with a single disk seek per read by combining two ideas: append-only log files for durability and an in-memory hash index (called the keydir) that maps every key to the exact file position of its latest value.

Every write appends a new record to the active log file. The keydir updates the entry for that key to point to the new record's file offset. Reads consult the keydir to find the file ID and offset, then perform a single `pread` to fetch the value. Deletes write a special tombstone record and remove the key from the keydir.

Over time, old values accumulate in log files. Compaction reads old log files, keeps only the latest value for each key (consulting the keydir), and writes them into new merged files. The old files are then deleted.

On startup, the engine must rebuild the keydir by replaying all log files -- or, if hint files exist, by reading just the hint files. Hint files contain the same key-to-position mappings as the keydir but serialized to disk, avoiding the need to read every value during recovery.

Build a complete Bitcask storage engine.

## Requirements

1. Implement `Put(key, value []byte) error` that appends a record (timestamp, key size, value size, key, value, CRC) to the active log file and updates the keydir entry to point to the new record's file ID, offset, and value size
2. Implement `Get(key []byte) ([]byte, error)` that looks up the key in the keydir, reads the value directly from the indicated file at the stored offset, and verifies the CRC before returning
3. Implement `Delete(key []byte) error` that appends a tombstone record (value size = 0 or a sentinel marker) and removes the key from the keydir
4. Rotate the active log file when it exceeds a configurable size threshold (default 256 MB). New writes go to a fresh file. Old files become immutable
5. Implement compaction that reads all immutable log files, writes only the live entries (latest value per key, no tombstones) into new merged files, generates a hint file per merged file, and deletes the old files. Compaction must not block concurrent reads or writes
6. Implement hint files: each hint file contains records of (timestamp, key size, value size, offset in data file, key) -- everything the keydir needs without storing the actual values. On startup, if hint files exist, rebuild the keydir from them instead of replaying the full data files
7. Implement crash recovery: on startup, replay any data files that lack a corresponding hint file to rebuild the keydir. Handle partially written records at the end of a file (truncated writes due to crash)
8. All keydir operations must be safe for concurrent access (multiple readers, single writer)

## Hints

<details>
<summary>Hint 1 -- Record format on disk</summary>

Use a fixed header followed by variable-length key and value. A good layout is: `[crc:4][timestamp:8][key_size:4][value_size:4][key:key_size][value:value_size]`. CRC covers everything after the CRC field itself. Use `encoding/binary.LittleEndian` for all integers.
</details>

<details>
<summary>Hint 2 -- Keydir structure</summary>

The keydir is a `map[string]KeydirEntry` where each entry holds `{FileID, Offset, ValueSize, Timestamp}`. Protect it with `sync.RWMutex`. Get operations take a read lock; Put and Delete take a write lock. The keydir is the source of truth for which value is current.
</details>

<details>
<summary>Hint 3 -- Compaction strategy</summary>

Iterate over all immutable files oldest-first. For each record, check the keydir: if the keydir points to this exact file and offset, the record is live -- write it to the merge output. Otherwise, skip it. After merging, update the keydir entries to point to the new merged file offsets, then delete old files.
</details>

<details>
<summary>Hint 4 -- Handling partial writes</summary>

When replaying a log file, if you cannot read a complete header or the CRC does not match, treat the record as the crash boundary. Truncate the file at that offset and stop replaying. All records before that point are valid.
</details>

## Acceptance Criteria

- [ ] Put followed by Get returns the correct value for arbitrary binary keys and values
- [ ] Overwriting a key with Put returns the new value on subsequent Get
- [ ] Delete removes the key: Get returns a not-found error
- [ ] Log file rotates when exceeding the configured size threshold
- [ ] After compaction, old log files are deleted and all live keys remain accessible
- [ ] Compaction does not block concurrent Get or Put operations
- [ ] Hint files are generated during compaction and used on subsequent startup
- [ ] Startup with hint files is at least 5x faster than full log replay for 100k+ keys
- [ ] Crash recovery: kill the process mid-write, restart, and verify all previously committed data is intact
- [ ] CRC validation detects corrupted records

## Research Resources

- [Bitcask: A Log-Structured Hash Table for Fast Key/Value Data (Basho, 2010)](https://riak.com/assets/bitcask-intro.pdf) -- the original design paper, 6 pages, covers the full architecture
- [CMU 15-445: Storage Models & Compression (Andy Pavlo)](https://15445.courses.cs.cmu.edu/fall2024/slides/04-storage2.pdf) -- log-structured storage in the context of database engines
- [Designing Data-Intensive Applications, Ch. 3 (Martin Kleppmann)](https://dataintensive.net/) -- hash indexes, SSTables, and LSM-trees compared with Bitcask
- [Riak Bitcask Source Code (Erlang)](https://github.com/basho/bitcask) -- the original implementation for reference on file format and merge logic
- [Database Internals by Alex Petrov, Ch. 7](https://www.databass.dev/) -- log-structured storage engines and compaction strategies
