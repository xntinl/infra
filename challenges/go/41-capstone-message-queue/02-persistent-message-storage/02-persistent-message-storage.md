<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 3h
-->

# Persistent Message Storage

An in-memory message queue loses all data on restart -- unacceptable for any serious use case. Your task is to build a durable, high-performance persistent storage engine for messages, inspired by Apache Kafka's log-structured storage. You will implement an append-only segmented log where messages are sequentially written to segment files, an index file that maps offsets to file positions for O(1) lookups, memory-mapped I/O for fast reads, configurable fsync policies for durability vs. performance trade-offs, and segment compaction. This storage engine will replace the in-memory slice from the previous exercise.

## Requirements

1. Implement a segmented log: messages are stored in segment files named by their base offset (e.g., `00000000000000000000.log`, `00000000000000000042.log`). Each segment has a configurable maximum size (default 1 GB). When the active segment exceeds the maximum, a new segment is created with its base offset set to the next offset. Implement `Log` struct managing a directory of segment files with `Append(msg *Message) (int64, error)` and `Read(offset int64) (*Message, error)`.

2. Define a binary message encoding format for on-disk storage. Each record consists of: offset (8 bytes), timestamp (8 bytes), key length (4 bytes), key data (variable), value length (4 bytes), value data (variable), header count (2 bytes), headers (repeated: key length 2 bytes + key data + value length 2 bytes + value data), and CRC32 checksum (4 bytes) over all preceding fields. Implement `Encode(msg *Message) ([]byte, error)` and `Decode(data []byte) (*Message, error)` with checksum validation.

3. Implement a sparse index file for each segment: the index maps offsets to physical file positions within the segment file. Store an index entry every N messages (configurable, default every 4096 messages) to keep the index small. Each index entry is 16 bytes: 8 bytes for the relative offset (offset minus base offset) and 8 bytes for the file position. To find a message by offset, binary search the index to find the nearest entry at or below the target offset, then scan forward in the segment file from that position.

4. Implement memory-mapped I/O for reading: use `syscall.Mmap` (or `golang.org/x/exp/mmap`) to memory-map segment files for reading, enabling zero-copy access to message data. Mapped segments should be cached and only unmapped when the segment is deleted or the process shuts down. Falls back to regular file I/O if mmap fails. For writing, use buffered `os.File` writes with configurable fsync policy.

5. Implement configurable fsync policies: **Every Message** (fsync after every append -- safest, slowest), **Every N Messages** (fsync after every N appends -- configurable batch), **Every Duration** (fsync at a time interval, e.g., every 100ms), and **OS Default** (never explicitly fsync -- fastest, relies on OS page cache). The policy is configurable per log. Implement `Sync() error` for manual sync. Track the last synced offset to know which messages are durable.

6. Implement segment lifecycle management: `RollSegment() error` closes the active segment, freezes its index, and opens a new segment. `DeleteSegment(baseOffset int64) error` removes a segment file and its index (used by retention policies). `ListSegments() []SegmentInfo` returns metadata about all segments (base offset, size, message count, creation time). Implement `TruncateBefore(offset int64) error` that deletes all segments whose messages are entirely before the given offset.

7. Implement a recovery mechanism: on startup, scan the active segment from the beginning, validating each record's CRC32 checksum. If a corrupt or truncated record is found at the tail, truncate the file to the last valid record and rebuild the index. For inactive (sealed) segments, validate lazily (on first read) or eagerly (on startup with a configurable flag). Report corruption with details: which segment, which offset, what kind of corruption.

8. Write tests covering: append and read round-trip for 100,000 messages, segment rotation when size exceeds the limit, sparse index lookup accuracy (read every Nth message and verify correct data), CRC32 corruption detection (flip a byte in a segment file and verify read fails), recovery from truncated tail (write half a message at the end, restart, verify clean truncation and correct read of all prior messages), memory-mapped read performance vs. regular file I/O (benchmark comparison), concurrent append from 10 goroutines, and a durability test that writes messages, kills the process after fsync, restarts, and verifies all synced messages are present.

## Hints

- Kafka's log format is an excellent reference. Each segment file is a pure append-only sequence of records. The offset is global (not per-segment), so the base offset of a segment plus the relative position in the index gives the absolute offset.
- For binary encoding, use `encoding/binary.Write` with `binary.BigEndian` (Kafka convention) or `binary.LittleEndian`. Pre-allocate a `bytes.Buffer` with the expected size to avoid re-allocation.
- Memory mapping in Go: `data, err := syscall.Mmap(fd, 0, fileSize, syscall.PROT_READ, syscall.MAP_SHARED)`. Access `data[offset]` directly. Unmap with `syscall.Munmap(data)`. Be careful with file growth -- you cannot mmap beyond the current file size, so only mmap sealed segments.
- For the sparse index binary search, `sort.Search` works well: `sort.Search(len(entries), func(i int) bool { return entries[i].Offset >= targetOffset })` gives you the first entry at or above the target.
- The active segment is append-only and not memory-mapped (it is actively growing). Only sealed (rotated) segments are candidates for mmap.
- For the durability test, fork a subprocess that writes messages and syncs, then kill it with SIGKILL (not SIGTERM), restart, and verify. This simulates a real crash.

## Success Criteria

1. 100,000 messages written and read back with identical content (verified by comparing payloads and metadata).
2. Segment rotation occurs automatically at the configured size boundary, producing correctly named segment files.
3. Sparse index lookups find the correct message for any random offset within 1 segment scan (at most N records scanned, where N is the index interval).
4. CRC32 validation catches single-byte corruption with 100% detection rate.
5. Recovery from a truncated tail correctly removes the partial record and leaves all prior records intact.
6. Memory-mapped reads are at least 2x faster than regular file I/O reads in the benchmark.
7. Concurrent appends from 10 goroutines produce no corrupt records, no duplicate offsets, and no lost messages.
8. After fsync + simulated crash + restart, all synced messages are present and readable.

## Research Resources

- [Apache Kafka Log Internals](https://kafka.apache.org/documentation/#log)
- [The Log: What every software engineer should know (Jay Kreps)](https://engineering.linkedin.com/distributed-systems/log-what-every-software-engineer-should-know-about-real-time-datas-unifying)
- [Log-Structured Storage (Designing Data-Intensive Applications)](https://dataintensive.net/)
- [Memory-Mapped Files in Go](https://pkg.go.dev/golang.org/x/exp/mmap)
- [CRC32 Checksumming in Go](https://pkg.go.dev/hash/crc32)
- [How Kafka Stores Messages on Disk](https://strimzi.io/blog/2021/12/17/kafka-segment-retention/)
