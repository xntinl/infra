# 7. Build a Distributed Append-Only Event Log (Kafka-Like)

**Difficulty**: Insane

## Prerequisites
- Mastered: All Elixir/OTP intermediate and advanced concepts (GenServer, Supervisor, File I/O, :gen_tcp, distributed Erlang, ETS)
- Mastered: Storage engine fundamentals — segment files, index structures, sequential I/O, mmap, fsync semantics
- Familiarity with: The Kafka architecture (brokers, partitions, replication, consumer groups, ISR), binary framing protocols, log-structured storage
- Reading: Jay Kreps' "The Log: What every software engineer should know about unification" (LinkedIn Engineering Blog, 2013), the Kafka paper (Kreps et al., 2011)

## Problem Statement

Build a distributed, append-only event log in Elixir/OTP. Producers write messages to named topics; consumers read from those topics at any offset and at any speed. The log is distributed across multiple nodes using partitions. Each partition is replicated for fault tolerance. The system must expose a custom binary protocol over TCP — no HTTP, no JSON API.

Your system must implement:
1. A topic model with P partitions per topic (configurable per topic); each partition is an independent, ordered, append-only sequence of messages stored as segment files on disk
2. A replication protocol: each partition has one leader and R-1 followers; producers write to the leader only; the leader replicates to followers and tracks the In-Sync Replica (ISR) set — a follower is removed from ISR if it falls too far behind; commits require ISR quorum acknowledgment
3. Three producer acknowledgment modes: `acks=0` (fire-and-forget), `acks=1` (leader-ack), `acks=all` (ISR quorum-ack); the producer specifies the mode per-write
4. Consumer groups: multiple consumer instances sharing a group ID collectively consume all partitions of a topic, with each partition assigned to exactly one consumer in the group at a time; when a consumer joins or leaves the group, partition assignments are rebalanced
5. Offset management: each consumer group tracks its committed offset per partition independently; a consumer can reset its offset to any valid value (beginning, end, or specific offset); committed offsets survive consumer restarts
6. Retention: each partition's segment files are rotated when a segment exceeds MAX_SEGMENT_BYTES; segments older than RETENTION_MS or beyond RETENTION_BYTES total are deleted in a background cleanup process
7. Idempotent producer: each producer carries a producer ID and sequence number; the leader deduplicates writes from the same producer within a session — a retry with the same sequence number is a no-op, not a duplicate append
8. A binary framing protocol over TCP: messages are framed as `[4-byte length][N bytes payload]`; the payload is a msgpack-encoded map with defined keys for request type, topic, partition, offset, and data

## Acceptance Criteria

- [ ] **Produce and consume — basic**: Producer writes 1,000 messages to `topic:orders, partition:0`; consumer reads from offset 0 and receives all 1,000 messages in order without gaps or duplicates
- [ ] **Partition ordering**: Within a single partition, messages must arrive at the consumer in the exact order they were produced; verify with sequence numbers embedded in message payloads
- [ ] **Replication — leader to followers**: With R=2, produce 100 messages; immediately query both leader and follower for their log end offset; confirm both have end offset = 100 (synchronous replication with `acks=all`)
- [ ] **Replica failover**: Kill the partition leader; confirm a follower in the ISR is promoted to leader within 10 seconds; confirm the new leader serves produce and consume requests without data loss
- [ ] **ISR shrink and grow**: Pause a follower (artificial stall, not crash); produce 1,000 messages; confirm the follower falls behind and is removed from ISR; resume the follower; confirm it catches up and is re-added to ISR
- [ ] **Consumer group rebalance**: Start a consumer group with 2 consumers on a 4-partition topic; each consumer gets 2 partitions; add a 3rd consumer; trigger rebalance; confirm partitions are reassigned such that no partition is assigned to more than one consumer
- [ ] **Offset commit and recovery**: Consumer reads 500 messages, commits offset 500; kill and restart the consumer; confirm it resumes from offset 500, not offset 0
- [ ] **Retention — time-based**: Configure `RETENTION_MS=5000`; produce 1,000 messages; wait 6 seconds; confirm segments containing those messages are deleted; confirm new produces succeed and are readable
- [ ] **Idempotent producer**: Produce a message with producer_id=42, sequence=1; produce the same message again (retry scenario) with producer_id=42, sequence=1; confirm the log contains exactly one copy of the message
- [ ] **Benchmark — write**: Sustain 500,000 messages/second write throughput to a single partition on localhost with `acks=1`
- [ ] **Benchmark — read**: Sustain 1,000,000 messages/second read throughput from a single partition on localhost using sequential disk reads

## What You Will Learn
- Why append-only logs are the correct abstraction for many distributed systems problems and how they unify messaging, database change data capture, and event sourcing
- How Kafka's ISR (In-Sync Replica) set differs from simple quorum replication — and why ISR makes producer throughput more predictable under follower lag
- How segment-based storage enables O(1) retention cleanup without compaction overhead
- Why sequential I/O is orders of magnitude faster than random I/O — and how Kafka exploits this with append-only writes and sequential consumer reads
- How consumer group coordination (leader election, partition assignment, rebalancing) works without a centralized coordinator — and why the Kafka group coordinator is a special-purpose broker role
- How idempotent delivery differs from exactly-once delivery — and why idempotent producers require sequence numbers tracked per partition
- How to design a compact binary framing protocol that is both space-efficient and easy to parse with pattern matching in Elixir

## Hints

This exercise is intentionally sparse. You are expected to:
- Read Jay Kreps' "The Log" post before touching any code — it provides the conceptual foundation that makes all implementation decisions coherent
- Study the Kafka replication protocol documentation carefully — the ISR set and high-watermark mechanism are the most subtle parts; implement them before producers and consumers
- Segment files should be named by their base offset (e.g., `00000000000.log`); this enables O(log N) offset lookup via binary search over segment file names without a separate index
- Consumer group rebalancing requires a "generation" counter to handle the case where two rebalances overlap — a consumer must not act on an assignment from a stale generation
- For the binary protocol, define your schema as a module with constants before writing any encoder/decoder — a protocol with magic numbers inline is unmaintainable

## Reference Material (Research Required)
- Kreps, J. (2013). *The Log: What every software engineer should know about unification* — LinkedIn Engineering Blog — do NOT read the summary version, read the full post
- Kreps, J., Narkhede, N. & Rao, J. (2011). *Kafka: a Distributed Messaging System for Log Processing* — the original Kafka paper from LinkedIn
- Apache Kafka documentation — *Kafka Replication* and *Kafka Consumer Group* sections — https://kafka.apache.org/documentation/
- Kafka protocol documentation — https://kafka.apache.org/protocol.html — the binary request/response encoding reference

## Difficulty Rating
★★★★★★★

## Estimated Time
6–8 weeks for an experienced Elixir developer with storage engine and distributed systems background
