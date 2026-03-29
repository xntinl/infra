# 136. Distributed Commit Log (Kafka-Style)

<!--
difficulty: insane
category: distributed-systems
languages: [go, rust]
concepts: [distributed-log, partitioning, replication, leader-election, consumer-groups, log-compaction, isr, append-only-storage]
estimated_time: 40-60 hours
bloom_level: create
prerequisites: [tcp-networking, file-io, concurrency-advanced, consensus-basics, binary-protocols, crc-checksums]
-->

## Languages

- Go (1.22+)
- Rust (stable)

## Prerequisites

- TCP server/client programming in both languages
- Memory-mapped files or buffered I/O for segment management
- Advanced concurrency: lock-free queues, producer-consumer patterns
- Binary protocol design and serialization
- Understanding of replication, leader election, and ISR concepts
- CRC32 checksums for data integrity

## Learning Objectives

- **Create** a distributed commit log system handling topics, partitions, replication, and consumer groups with production-grade fault tolerance
- **Evaluate** the trade-offs between availability and durability at each acknowledgment level (acks=0, acks=1, acks=all)
- **Design** a storage engine with append-only segment files, offset indexing, and log compaction
- **Implement** consumer group coordination with partition assignment strategies and offset management

## The Challenge

Build a distributed commit log inspired by Apache Kafka. The system consists of brokers (servers that store and serve log data), producers (clients that append records), and consumers (clients that read records by offset). Topics are logical streams of records. Each topic is divided into partitions for parallelism. Each partition is an ordered, append-only sequence of records with monotonically increasing offsets.

Partitions are replicated across brokers. One replica is the leader (handles reads and writes), the others are followers (replicate from the leader). The In-Sync Replica set (ISR) tracks which followers are caught up. If the leader fails, a follower from the ISR is elected as the new leader.

Producers send records to the partition leader. The acknowledgment level controls durability: acks=0 (fire-and-forget), acks=1 (leader acknowledged), acks=all (all ISR replicas acknowledged). Consumer groups enable parallel consumption: each partition is assigned to exactly one consumer in the group.

Storage uses append-only segment files with offset-based indexing. When a segment exceeds a size threshold, a new segment is created. Log compaction retains only the latest record per key, enabling the log to serve as a materialized view.

## Requirements

1. **Broker**: TCP server that hosts partitions, handles produce/fetch/metadata requests, and replicates data to followers
2. **Topics and Partitions**: create topics with configurable partition count and replication factor. Each partition is an independent ordered log
3. **Storage Engine**: append-only segment files with binary record format (offset, timestamp, key, value, CRC32). Configurable segment size (default 1GB, use smaller for testing). Offset index file per segment for O(1) offset lookup
4. **Producer Protocol**: produce request with topic, partition (or key-based routing), records batch, and acks level. Partition assignment by key hash (murmur3 or fnv) or round-robin
5. **Consumer Protocol**: fetch request with topic, partition, offset. Returns records from offset up to max bytes. Long-polling with configurable timeout for new data
6. **Consumer Groups**: group coordinator assigns partitions to consumers. Assignment strategies: range and round-robin. Rebalance on consumer join/leave. Offset commit and fetch per group/topic/partition
7. **Replication**: leader writes to local log, followers fetch from leader. ISR tracking: follower falls out of ISR if it falls behind by more than configurable lag (time or offsets). Follower rejoins ISR when caught up
8. **Leader Election**: when leader fails (detected by missed heartbeats), elect new leader from ISR. If ISR is empty, either wait (prefer durability) or elect any replica (prefer availability) based on configuration
9. **Producer Acknowledgments**: acks=0 returns immediately, acks=1 returns after leader write, acks=all returns after all ISR replicas acknowledge
10. **Log Compaction**: background process that scans segments, retains only the latest record per key, and produces compacted segments. Configurable compaction interval
11. **Wire Protocol**: binary protocol over TCP with request/response framing: 4-byte length prefix, request type, correlation ID, payload. Both languages must implement compatible protocols
12. **Metrics**: records produced/consumed per second, replication lag per follower, ISR changes, leader elections, segment count, storage bytes

## Hints

Minimal. This is an insane-level challenge. Study the Kafka protocol specification and storage internals independently. Two starting points:

1. **Storage first**: get the segment file format working before anything else. A record is: `[offset:8][timestamp:8][key_len:4][key:N][value_len:4][value:M][crc32:4]`. The index file maps offset to file position: `[offset:8][position:8]`. Use sparse indexing (one entry per N records) to keep index files small. Read Kafka's `LogSegment.scala` for the production design.

2. **Protocol second**: define a minimal wire protocol. Each message has `[length:4][api_key:2][correlation_id:4][payload:...]`. Start with three API keys: Produce (0), Fetch (1), Metadata (2). Add consumer group APIs (JoinGroup, SyncGroup, OffsetCommit, OffsetFetch) after the core works.

## Acceptance Criteria

- [ ] Brokers start, accept TCP connections, and handle produce/fetch/metadata requests
- [ ] Topics with multiple partitions: records routed to correct partition by key hash
- [ ] Segment files rotate at configured size, records persist across broker restart
- [ ] Offset index enables O(1) lookup: fetch at any valid offset returns correct record
- [ ] Producer acks=0/1/all return at the correct point in the replication pipeline
- [ ] Consumer groups: partitions assigned to consumers, rebalance on join/leave
- [ ] Replication: followers replicate from leader, ISR tracked correctly
- [ ] Leader election: new leader elected from ISR on leader failure, producers/consumers redirect
- [ ] Log compaction retains only the latest record per key
- [ ] Wire protocol compatible between Go and Rust implementations
- [ ] All tests pass with race detector (Go) / no undefined behavior (Rust)
- [ ] End-to-end test: produce 100K records, consume all, verify ordering and completeness

## Going Further

- **Exactly-once semantics**: implement idempotent producers with producer ID and sequence numbers, and transactional produce across partitions
- **Tiered storage**: offload old segments to object storage (S3-compatible), fetch transparently from either local or remote
- **Schema registry**: enforce record schemas (Avro, Protobuf) with compatibility checks on produce
- **Rack-aware replication**: assign replicas to different racks/zones for fault domain isolation

## Research Resources

- [Kafka protocol specification](https://kafka.apache.org/protocol) -- the complete wire protocol reference
- [Jay Kreps: The Log (2013)](https://engineering.linkedin.com/distributed-systems/log-what-every-software-engineer-should-know-about-real-time-datas-unifying) -- the philosophical foundation of distributed logs
- [Kafka: a Distributed Messaging System for Log Processing (2011)](https://www.microsoft.com/en-us/research/wp-content/uploads/2017/09/Kafka.pdf) -- original Kafka paper
- [Martin Kleppmann: Designing Data-Intensive Applications, Chapter 11](https://dataintensive.net/) -- stream processing and log-based messaging
- [Kafka source: LogSegment.scala](https://github.com/apache/kafka/blob/trunk/storage/src/main/java/org/apache/kafka/storage/internals/log/LogSegment.java) -- production segment implementation
- [Redpanda architecture](https://docs.redpanda.com/current/get-started/architecture/) -- Kafka-compatible system in C++, useful for alternative design perspectives
