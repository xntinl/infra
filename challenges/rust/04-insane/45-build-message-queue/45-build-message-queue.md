# 45. Build a Message Queue

**Difficulty**: Insane

## The Challenge

Message queues are the backbone of modern distributed systems. Apache Kafka transformed the industry by reimagining the message broker as a distributed commit log — an append-only, partitioned, replicated sequence of records that consumers read at their own pace by tracking offsets rather than having messages pushed to them. This simple abstraction enables event sourcing, stream processing, change data capture, and decoupled microservice communication at enormous scale. Kafka routinely handles millions of messages per second in production deployments, achieved through careful engineering: sequential disk I/O that outperforms random access, zero-copy data transfer via `sendfile`, batched and compressed writes, and a replication protocol that balances durability with throughput.

Your task is to build a Kafka-like message queue from scratch in Rust. You will implement the core abstractions — topics divided into partitions, each partition an append-only commit log stored on disk — along with the producer path (batching, compression, acknowledgment), the consumer path (consumer groups, offset tracking, rebalancing), the replication protocol (leader-follower with in-sync replicas), and the retention system (time-based and size-based log cleanup). The system communicates over an async TCP protocol of your own design, and reads are optimized using zero-copy techniques. This is a systems programming challenge in the deepest sense: you will be working with file I/O, memory-mapped files, network protocols, concurrency, and distributed coordination.

This challenge is not about building a toy. Kafka's design is the product of years of engineering at LinkedIn and the broader open-source community, and understanding it deeply — by rebuilding it — teaches you more about distributed systems, storage engines, and high-performance I/O than any textbook. You will confront real decisions: how to lay out data on disk for sequential access, how to index records by offset for efficient seeks, how to handle consumer group rebalancing when members join and leave, how to replicate data without halving throughput, and how to implement retention without blocking reads. Rust's async ecosystem (tokio), zero-cost abstractions, and control over memory layout make it an ideal language for this challenge.

---

## Acceptance Criteria

### Topic and Partition Model
- [ ] Implement a `Topic` that is divided into a configurable number of `Partition`s (set at creation time)
- [ ] Each `Partition` is an independent, ordered, append-only commit log
- [ ] Messages (records) within a partition are assigned a monotonically increasing `offset` (u64) starting from 0
- [ ] Each record consists of: offset, timestamp, optional key (bytes), value (bytes), and optional headers (key-value pairs)
- [ ] Support creating and deleting topics via the management API
- [ ] Support listing topics and their partition counts
- [ ] Producers specify a topic and optionally a partition; if no partition is specified, partition assignment uses the key hash (murmur2 or similar) or round-robin for null keys
- [ ] The partition count is immutable after creation (matching Kafka's behavior; explain why in comments)

### Append-Only Commit Log on Disk
- [ ] Each partition's data is stored as a sequence of segment files on disk
- [ ] Each segment file has a base offset (the offset of its first record) and a configurable maximum size (e.g., 1 GB)
- [ ] When a segment reaches its size limit, a new segment is created (log rolling)
- [ ] Records are written to the active (latest) segment using append-only sequential writes
- [ ] Each segment has an associated index file that maps offsets to physical file positions for fast seeking
- [ ] The index is sparse: store an entry every N records (e.g., every 4096 bytes of data) to balance index size with seek performance
- [ ] Implement a time-based index as well: map timestamps to offsets for time-based seeking
- [ ] Use `mmap` (memory-mapped files) for reading index files for fast lookup without explicit I/O
- [ ] The log file format is a simple framed binary format: `[length: u32][crc: u32][record bytes]` for each record
- [ ] Implement CRC32 checksums on each record for data integrity verification

### Producer Path
- [ ] Implement a producer client that connects to the broker over TCP
- [ ] Support batching: the producer accumulates records in memory and sends them in batches to reduce network round trips
- [ ] Batching is configurable: by count (e.g., 1000 records), by size (e.g., 64KB), and by time (e.g., 100ms linger time)
- [ ] Support compression: implement at least one compression algorithm (e.g., `lz4` or `snappy`) applied to the entire batch
- [ ] The broker decompresses for indexing but stores the compressed batch on disk (or stores compressed and decompresses on read — document your choice)
- [ ] Implement acknowledgment modes: `acks=0` (fire-and-forget), `acks=1` (leader acknowledged), `acks=all` (all in-sync replicas acknowledged)
- [ ] The producer receives an acknowledgment containing the offset of the first record in the batch
- [ ] Implement producer retries with configurable retry count and backoff strategy (exponential backoff)
- [ ] Implement idempotent producer: assign each producer a unique ID and sequence number per partition to detect and deduplicate retries

### Consumer Path
- [ ] Implement a consumer client that connects to the broker and reads records from specified partitions starting at a given offset
- [ ] The consumer sends a fetch request specifying topic, partition, offset, and max bytes
- [ ] The broker responds with a batch of records starting from the requested offset
- [ ] The consumer maintains its position (offset) and advances it as records are processed
- [ ] Implement long polling: if no records are available at the requested offset, the broker holds the request for a configurable timeout (e.g., 500ms) before returning an empty response
- [ ] Support "earliest" and "latest" offset reset strategies for consumers that request an invalid offset
- [ ] Implement offset-based seeking: given a target offset, use the sparse index to find the nearest entry and scan forward
- [ ] Implement timestamp-based seeking: given a target timestamp, use the time index to find the approximate offset

### Consumer Groups and Offset Tracking
- [ ] Implement consumer groups: multiple consumers with the same group ID share the partitions of a topic
- [ ] Each partition is assigned to exactly one consumer within a group at any time
- [ ] Implement partition assignment strategies: range assignment and round-robin assignment
- [ ] When a consumer joins or leaves a group, trigger a rebalance that reassigns partitions
- [ ] Implement a group coordinator on the broker that tracks group membership and handles join/leave/heartbeat
- [ ] Consumers send periodic heartbeats; if a heartbeat is missed for a configurable timeout, the consumer is considered dead and a rebalance is triggered
- [ ] Implement committed offset storage: consumers periodically commit their current offset to the broker
- [ ] Committed offsets are stored in a special internal topic (like Kafka's `__consumer_offsets`) or a simpler key-value store
- [ ] On rebalance, a consumer assigned a new partition starts reading from the last committed offset for that partition

### Replication and In-Sync Replicas (ISR)
- [ ] Support a configurable replication factor per topic (e.g., replication factor 3 means each partition has 3 replicas)
- [ ] Each partition has one leader and N-1 follower replicas
- [ ] All produce and consume requests are served by the leader
- [ ] Followers continuously fetch records from the leader to stay in sync (pull-based replication)
- [ ] Define "in-sync replica" (ISR): a follower is in-sync if it has fetched records up to the leader's log end offset within a configurable lag threshold (time-based or offset-based)
- [ ] The leader tracks the ISR set and the "high watermark" — the offset up to which all ISR members have replicated
- [ ] Consumers can only read up to the high watermark (prevents reading uncommitted data that could be lost)
- [ ] Implement `acks=all`: the producer's write is acknowledged only after all ISR members have replicated the record
- [ ] If a follower falls out of the ISR (too far behind), it is removed from the ISR set and must catch up to rejoin
- [ ] Implement leader election: if the leader fails, one of the ISR members is elected as the new leader

### Retention Policies
- [ ] Implement time-based retention: delete segments whose newest record is older than a configurable retention period (e.g., 7 days)
- [ ] Implement size-based retention: delete the oldest segments when the total partition size exceeds a configurable limit (e.g., 100 GB)
- [ ] Retention runs as a background task that periodically checks each partition
- [ ] Segment deletion is safe: only delete segments that are not being read by any active consumer (or use a read lock / reference count)
- [ ] Implement log compaction as an alternative retention policy: for each key, retain only the latest record, discarding older records with the same key
- [ ] Log compaction runs as a background thread that reads old segments, filters, and writes new compacted segments
- [ ] Tombstone records (key with null value) are retained for a configurable "delete retention time" before being removed during compaction

### Async TCP Protocol
- [ ] Design a binary wire protocol with request/response framing: `[message_size: u32][api_key: u16][api_version: u16][correlation_id: u32][payload]`
- [ ] Implement at least these API calls: Produce, Fetch, ListOffsets, Metadata, CreateTopics, DeleteTopics, OffsetCommit, OffsetFetch, JoinGroup, SyncGroup, Heartbeat, LeaveGroup
- [ ] Use `tokio` for async I/O with one task per client connection
- [ ] Implement connection multiplexing: multiple in-flight requests on a single TCP connection, matched by correlation ID
- [ ] Implement request rate limiting per client connection to prevent abuse
- [ ] Serialize and deserialize protocol messages efficiently using a custom binary codec (not JSON — binary for performance)
- [ ] The protocol is versioned: include an API version in each request to support future evolution
- [ ] Handle malformed requests gracefully: return an error response, do not crash the broker

### Zero-Copy and Performance Optimizations
- [ ] Implement zero-copy reads using `sendfile` (or Rust's `tokio::io::copy` with `File` and `TcpStream`) to transfer data from disk to network without copying through userspace
- [ ] Alternatively, use `mmap` to map log segments into memory and write directly from the mapped region to the socket
- [ ] Benchmark: achieve at least 100,000 messages/second for 1KB messages on a single partition (single producer, single consumer) on commodity hardware
- [ ] Benchmark: achieve at least 500 MB/s read throughput when a consumer is reading sequentially from disk
- [ ] Use page cache effectively: sequential writes and reads naturally leverage the OS page cache; avoid `O_DIRECT` unless benchmarks show it helps
- [ ] Batch disk writes: use `writev` (vectored I/O) or accumulate records in a write buffer before flushing
- [ ] Implement configurable fsync policy: `fsync` every N records, every M milliseconds, or never (rely on OS for durability)
- [ ] Profile with `perf` or `flamegraph` and document the hot paths

### Broker Metadata and Cluster Coordination
- [ ] Implement a metadata service that tracks: which topics exist, how many partitions each has, which broker is the leader for each partition, and the current ISR set
- [ ] Metadata is returned in response to a Metadata API call, allowing clients to discover topic/partition layout
- [ ] When leader election occurs, metadata is updated and clients are notified (or discover the change on next metadata refresh)
- [ ] Implement a controller broker that is responsible for partition assignment and leader election across the cluster
- [ ] The controller maintains an epoch/generation number that increments with each leader election, preventing stale leaders from accepting writes

### Testing and Reliability
- [ ] Unit tests for the commit log: append, read, segment rolling, index lookup, retention
- [ ] Unit tests for the wire protocol: serialization round-trip for all message types
- [ ] Integration test: single broker, one producer, one consumer, verify all produced messages are consumed in order
- [ ] Integration test: single broker, one producer, consumer group with 3 consumers, verify partition assignment and full consumption
- [ ] Integration test: produce 1 million messages, restart the broker, verify all messages are recoverable
- [ ] Integration test: simulate consumer failure during processing, verify rebalance and no message loss (at-least-once delivery)
- [ ] Integration test: verify retention policy correctly deletes old segments while active consumers are unaffected
- [ ] Chaos test: randomly kill and restart broker processes during continuous produce/consume, verify no data corruption (CRC checks pass)
- [ ] Test replication: verify follower replicas stay in sync, verify leader election on leader failure, verify high watermark advances correctly
- [ ] Test idempotent producer: produce the same batch twice (simulating retry), verify only one copy appears in the log
- [ ] Test consumer group rebalance under load: continuously produce while consumers join and leave, verify no messages are lost or duplicated
- [ ] End-to-end latency test: measure p50, p95, p99 latency from producer send to consumer receive under steady-state load

---

## Starting Points

These are real resources to study before and during implementation:

1. **Apache Kafka Documentation — Design Section** (https://kafka.apache.org/documentation/#design) — The official design document covers the commit log, partitions, replication, consumer groups, and performance optimizations. Sections 4.2 (Persistence), 4.3 (Efficiency), 5.2 (Consumer Position), and 5.4 (Replication) are essential reading.

2. **Jay Kreps - "The Log: What every software engineer should know about real-time data's unifying abstraction"** (LinkedIn Engineering blog, 2013) — The foundational essay that explains why an append-only commit log is such a powerful primitive. Motivates the entire Kafka design.

3. **Kafka Protocol Specification** (https://kafka.apache.org/protocol.html) — The complete wire protocol specification. You do not need to implement the full Kafka protocol, but studying its request/response format, error codes, and API design will inform your own protocol design.

4. **Redpanda Architecture** (https://redpanda.com/blog/tpc-buffers) — Redpanda is a C++ Kafka-compatible message broker. Their architecture blog posts cover thread-per-core design, Raft-based replication (instead of Kafka's ISR protocol), and memory management. Useful for understanding alternative design choices.

5. **Franz-go** (https://github.com/twmb/franz-go) — A high-performance Go Kafka client library. Study its producer batching implementation (`pkg/kgo/producer.go`) and consumer group coordination (`pkg/kgo/consumer_group.go`) for clean implementations of these protocols.

6. **Kafka Internals: Replication** (Kafka source code, `core/src/main/scala/kafka/server/ReplicaManager.scala`) — The ISR management, high watermark tracking, and leader election logic. Dense but definitive.

7. **Martin Kleppmann - "Designing Data-Intensive Applications"** — Chapter 11 covers stream processing and message brokers, including Kafka's architecture. Chapter 9 covers consistency and replication, which applies to the ISR protocol.

8. **Jepsen: Kafka** (https://jepsen.io/analyses/kafka) — Kyle Kingsbury's analysis of Kafka's consistency guarantees under failure. Reveals subtle edge cases in replication and consumer offsets that you should understand and test for.

9. **`tokio` documentation and examples** (https://tokio.rs) — You will build the entire networking layer on tokio. Study the `TcpListener`/`TcpStream` examples, `tokio::io::AsyncRead`/`AsyncWrite` traits, and the `bytes` crate for zero-copy buffer management.

10. **`memmap2` crate** (https://crates.io/crates/memmap2) — Rust library for memory-mapped files. Use this for reading index files and potentially for zero-copy reads of log segments. Study the safety considerations around mmap (file truncation races, etc.).

---

## Hints

1. Start with the commit log. Implement a single `Partition` that can append records and read them back by offset. Ignore networking, replication, and consumer groups entirely. Get the on-disk format right first: segment files, sparse index, CRC checksums. Verify with tests that you can write 1 million records, close the partition, reopen it, and read them all back with correct offsets and checksums.

2. For the segment file format, keep it simple. Each record is: `[total_length: u32][magic: u8][crc32: u32][timestamp: i64][key_length: i32][key: bytes][value_length: i32][value: bytes]`. The `total_length` prefix lets you skip records efficiently during scanning. The CRC covers everything after the CRC field. Use -1 for null keys/values (matching Kafka's convention).

3. The sparse index is a simple sorted array of `(offset, physical_position)` pairs. To find offset X: binary search the index to find the largest offset <= X, seek to that physical position in the segment file, and scan forward until you find offset X. With an entry every 4096 bytes of data, the scan is always short. Store the index as a flat binary file of `(u64, u64)` pairs, and mmap it for fast binary search.

4. For the TCP protocol, use `tokio::net::TcpListener` and spawn a task per connection. Frame messages with a 4-byte length prefix. Use the `bytes` crate's `BytesMut` for efficient buffer management. Parse request headers (api_key, version, correlation_id) to dispatch to the right handler. Return responses with the matching correlation_id. This is a standard request-response protocol pattern.

5. Producer batching on the client side is a background task. The producer API is: `send(topic, partition, key, value) -> Future<Offset>`. Internally, records are accumulated in a per-partition buffer. A background task flushes the buffer when any trigger fires: batch size reached, byte size reached, or linger timeout elapsed. Each batch is a single Produce request to the broker. The returned future resolves when the broker acknowledges the batch.

6. For compression, wrap the entire batch of records in a single compressed envelope. On the producer: serialize all records in the batch into a byte buffer, compress the buffer (using `lz4_flex` crate or `snap` crate for snappy), send the compressed buffer with a compression type header. On the broker: decompress to build the index, then store either compressed or decompressed (compressed saves disk, decompressed simplifies reads — pick one and document why).

7. Consumer group coordination is the most complex part. Implement it in phases: (a) First, implement a simple "one consumer per partition" model with manual offset tracking. (b) Then add the group coordinator: a broker-side component that accepts JoinGroup requests, waits for all members to join (with a timeout), assigns partitions, and sends SyncGroup responses with assignments. (c) Then add heartbeats and rebalance triggering. This mirrors the Kafka group protocol.

8. For the rebalance protocol: (a) Consumer sends JoinGroup with its subscribed topics. (b) Coordinator waits for all members (or a timeout). (c) Coordinator picks a leader among the members and sends JoinGroup responses. (d) The leader computes the partition assignment and sends it in a SyncGroup request. (e) Coordinator broadcasts the assignment to all members via SyncGroup responses. (f) Members start fetching from their assigned partitions. This client-side assignment allows custom partition strategies without changing the broker.

9. For replication, implement pull-based replication: each follower runs a fetch loop that continuously pulls new records from the leader. The fetch request includes the follower's current offset. The leader responds with records from that offset. After applying fetched records, the follower sends an updated offset in the next fetch. The leader tracks each follower's offset and computes the high watermark as the minimum offset across all ISR members.

10. The ISR set is dynamic. A follower is in-sync if: `(leader_log_end_offset - follower_offset) < max_lag_offsets` OR `(current_time - follower_last_fetch_time) < max_lag_time`. When a follower falls behind, the leader removes it from the ISR. The follower continues fetching and, once caught up, the leader adds it back. Shrinking the ISR means writes with `acks=all` require fewer acknowledgments, trading durability for availability.

11. Zero-copy reads using `sendfile` (on Linux) or equivalent: Rust's `tokio::fs::File` and `tokio::io::copy` may or may not use `sendfile` under the hood depending on the platform and version. For guaranteed zero-copy, use the `nix` crate's `sendfile` directly (on Linux) or accept the minor overhead of a userspace buffer. Measure the difference — for most workloads, the OS page cache dominates and zero-copy provides modest gains.

12. For retention, run a background task (tokio interval timer) that iterates over each partition's segments. For time-based retention: check the segment's maximum timestamp (stored in a segment metadata file or derived from the last record). For size-based retention: sum segment sizes and delete from oldest. Never delete the active (current) segment. Use a read-write lock on the segment list: readers (consumers) hold a read lock, the retention task holds a write lock when deleting.

13. Log compaction is significantly more complex than simple retention. The compaction thread reads all records in the "dirty" (uncompacted) portion of the log, builds a hash map of `key -> latest_offset`, then writes a new compacted segment containing only the latest record for each key. Tombstones (null-value records) are retained for a grace period. Compaction must not interfere with active reads — write the new segment to a temporary file and atomically rename it.

14. For idempotent producers, the broker maintains a map of `(producer_id, partition) -> last_sequence_number`. On each produce request, check if the sequence number is consecutive. If it is a duplicate (same sequence as already seen), return success without re-appending. If it is out of order (gap in sequence), return an error. This state must survive broker restarts — persist it periodically.

15. Test your protocol by building both the client and server in the same crate, separated by a trait. Define `trait MessageBroker { async fn produce(...); async fn fetch(...); ... }` and implement it for both the in-process version (direct function calls) and the network version (TCP client). Run the same test suite against both implementations. This catches serialization bugs early.

16. For the chaos test, use `tokio::time::pause()` in tests to control time, and inject failures at specific points: drop TCP connections mid-write, delay follower fetches, kill the leader process (simulated by dropping its task). Verify that: (a) no acknowledged writes are lost, (b) consumers eventually see all acknowledged writes, (c) CRC checksums pass on all records after recovery. This is where the real bugs hide.

17. For the controller/metadata service in a multi-broker setup, you have two options: (a) use an external coordination service like etcd or ZooKeeper (Kafka's traditional approach), or (b) implement Raft-based consensus among the brokers themselves (Kafka's KRaft mode, Redpanda's approach). For this challenge, option (b) is more instructive but significantly more complex. A simpler middle ground: implement a single-controller model where one broker is the designated controller, and handle controller failover as a stretch goal.

18. For the segment file naming convention, use the base offset zero-padded to 20 digits: `00000000000000000000.log` for the first segment, `00000000000000004096.log` for a segment starting at offset 4096. This makes segments naturally sortable by filename and makes it easy to find the segment containing a given offset by binary searching the directory listing.

19. When implementing the consumer group's committed offset storage, consider storing offsets in a compacted internal topic (like Kafka's `__consumer_offsets`). The key is `(group_id, topic, partition)` and the value is the committed offset. Compaction ensures only the latest offset per key is retained. This dogfoods your own system — if the commit log and compaction work correctly, offset storage comes for free.

20. Think carefully about thread safety and concurrent access patterns. The commit log is written by the producer handler (single writer per partition) and read by multiple consumer handlers and follower replication handlers concurrently. Use `RwLock<Vec<Arc<Segment>>>` for the segment list — readers acquire the read lock and clone the `Arc` to the segment they need, then release the lock before doing I/O. This minimizes lock contention. For the active segment's write position, use an `AtomicU64` to allow lock-free reads of the current log end offset.
