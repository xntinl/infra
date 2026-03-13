<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 3h
-->

# Write-Ahead Log (WAL)

Every serious database guarantees durability through a deceptively simple idea: before changing any data page on disk, first write the intended change to a sequential log. If the process crashes mid-operation, the log provides a definitive record of what happened and what didn't, enabling the database to recover to a consistent state. Your task is to build a production-grade Write-Ahead Log from scratch in Go. You will implement log segment management with configurable rotation, CRC32 checksumming for corruption detection, binary record encoding with length-prefixed framing, fsync-based durability guarantees, crash recovery by replaying the log from the last checkpoint, and a checkpoint mechanism that marks safe truncation points. This component forms the durability backbone of the database engine you will build across this capstone section.

## Requirements

1. Define a binary log record format with a header containing: record length (4 bytes), CRC32 checksum (4 bytes), log sequence number (8 bytes), transaction ID (8 bytes), record type enum (1 byte: INSERT, UPDATE, DELETE, CHECKPOINT, COMMIT, ABORT), and a variable-length payload. Implement `Encode()` and `Decode()` methods that serialize and deserialize records to/from byte slices with no allocation in the hot path for decoding.

2. Implement a `WAL` struct that manages an active log segment file and rotates to a new segment file when the current segment exceeds a configurable maximum size (default 64 MB). Segment files must be named with zero-padded sequence numbers (e.g., `wal-000001.log`, `wal-000002.log`) and stored in a configurable directory.

3. Implement `Append(record *LogRecord) (LSN, error)` that writes a record to the active segment, assigns a monotonically increasing log sequence number, computes and embeds the CRC32 checksum, and returns the assigned LSN. The append must be safe for concurrent callers using a mutex, and must call `fsync` on the underlying file after every write (with a configurable option for group commit batching).

4. Implement group commit batching: instead of calling `fsync` after every single append, accumulate writes in a buffer and flush/sync at a configurable interval (e.g., every 10ms) or when the buffer reaches a configurable size. Waiting goroutines must block on a condition variable until their batch has been synced, then receive their assigned LSN.

5. Implement `Recover(dir string) ([]LogRecord, error)` that reads all segment files in order, decodes each record, validates its CRC32 checksum, detects and reports any corruption (truncated records, bad checksums), and returns all valid records up to the last good record. Partial/corrupt records at the tail of the last segment must be truncated, not treated as fatal errors.

6. Implement a checkpoint mechanism: `Checkpoint() (LSN, error)` writes a special CHECKPOINT record to the log and returns its LSN. Implement `Truncate(upToLSN LSN) error` that deletes all segment files whose records are entirely below the given LSN, freeing disk space. The truncation must be crash-safe (never delete a segment that contains records at or above the truncation LSN).

7. Implement a `Reader` that can tail the WAL in real-time, starting from a given LSN, yielding new records as they are appended. This is used by replication consumers and the buffer pool manager. The reader must handle segment rotation transparently and block (via channel or condition variable) when it has caught up to the head.

8. Write comprehensive tests including: unit tests for record encoding/decoding round-trips, corruption detection (flip bits in encoded records and verify CRC failure), concurrent append stress tests (100+ goroutines appending simultaneously), crash recovery simulation (write partial records and verify recovery truncates correctly), segment rotation under load, group commit latency verification, and a benchmark comparing fsync-per-write vs. group commit throughput.

## Hints

- Use `encoding/binary` with `binary.LittleEndian` for all integer encoding to avoid endianness bugs across platforms.
- For CRC32, use `hash/crc32.NewIEEE()` and checksum everything in the record except the checksum field itself; a common pattern is to checksum the header (with checksum field zeroed) concatenated with the payload.
- For group commit, model the batching goroutine as a dedicated flusher that wakes on a timer tick or buffer-full signal, flushes the accumulated buffer, calls `fsync`, then broadcasts to all waiting appenders.
- `os.File.Sync()` is Go's wrapper for `fsync`; on macOS you may need `syscall.Fdatasync` or `F_FULLFSYNC` via `syscall.Fcntl` for true durability.
- For the tailing reader, `sync.Cond` is a natural fit: the appender broadcasts after each flush, and the reader waits when caught up.
- When simulating crashes in tests, simply close the file mid-write (write half a record), then reopen and run recovery.

## Success Criteria

1. A single-goroutine append benchmark achieves at least 50,000 records/second with group commit enabled on an SSD.
2. 100 concurrent goroutines can append 1,000 records each without data races (verified with `-race`), lost records, or duplicate LSNs.
3. Recovery correctly replays all committed records and truncates exactly the partial tail record after a simulated crash.
4. CRC32 validation detects single-bit corruption with 100% accuracy in test cases covering every byte position in a record.
5. Segment rotation produces correctly named files and the reader transparently follows across segment boundaries.
6. Truncation after checkpoint frees disk space and does not delete any segment containing records above the truncation LSN.
7. The tailing reader receives all new records within one group commit interval of their append, verified by a timing test.

## Research Resources

- [How Does a Database Work? - WAL Fundamentals](https://cstack.github.io/db_tutorial/parts/part1.html)
- [PostgreSQL WAL Internals](https://www.postgresql.org/docs/current/wal-internals.html)
- [SQLite WAL Mode Documentation](https://www.sqlite.org/wal.html)
- [CRC32 in Go standard library](https://pkg.go.dev/hash/crc32)
- [Write-Ahead Logging in LSM-Trees (RocksDB Wiki)](https://github.com/facebook/rocksdb/wiki/Write-Ahead-Log)
- [Group Commit Optimization in Database Systems](https://dsf.berkeley.edu/papers/vldb89-groupcommit.pdf)
