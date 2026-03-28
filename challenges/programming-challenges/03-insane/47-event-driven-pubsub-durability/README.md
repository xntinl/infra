# 47. Event-Driven Pub/Sub with Durability

<!--
difficulty: insane
category: networking-and-protocols
languages: [go]
concepts: [pubsub, append-only-log, consumer-groups, at-least-once-delivery, dead-letter-queue, backpressure, tcp-protocol, partitioning]
estimated_time: 25-35 hours
bloom_level: create
prerequisites: [go-concurrency, tcp-networking, binary-protocols, file-io, synchronization-primitives, system-design]
-->

## Languages

- Go (1.22+)

## Prerequisites

- Advanced Go concurrency: goroutines, channels, select, mutexes, WaitGroups, atomic operations
- TCP socket programming with `net.Listener` and `net.Conn`
- Binary protocol design with length-prefixed framing
- File I/O: `os.File`, `mmap`, append-only writes, fsync semantics
- Understanding of messaging systems (Kafka, NATS, or RabbitMQ concepts)

## Learning Objectives

- **Create** a durable publish-subscribe system with persistent message storage and reliable delivery guarantees
- **Design** a topic-based routing system with wildcard subscription matching
- **Architect** consumer group semantics with partition assignment and rebalancing
- **Evaluate** the trade-offs between delivery guarantees (at-most-once, at-least-once, exactly-once) and their implementation complexity
- **Build** a dead letter queue mechanism for handling poison messages and delivery failures

## The Challenge

Every distributed system needs a way to decouple producers from consumers. A payment service should not care whether the notification service, the analytics pipeline, and the fraud detector are all running at this exact moment. It publishes an event; the messaging system guarantees delivery when each consumer is ready.

Build a durable publish-subscribe messaging system from scratch in Go. No Kafka, no NATS, no RabbitMQ -- raw TCP connections, your own binary protocol, your own storage engine, your own consumer group coordinator.

Messages must survive process restarts. Consumers must be able to replay from any offset. Consumer groups must divide partitions among members so that each message is processed exactly once within the group (at-least-once delivery). Failed messages must land in a dead letter queue after exceeding retry limits. Slow consumers must trigger backpressure rather than unbounded memory growth.

The system communicates over TCP using length-prefixed binary framing. Design the protocol, the storage format, the consumer coordination, and the metrics layer.

## Requirements

1. **Topic and partition model**: topics are divided into N partitions (configurable at creation). Each partition is an independent, ordered, append-only log. Messages within a partition are assigned monotonically increasing offsets
2. **Persistent storage**: each partition stores messages in an append-only file. Each message record contains: offset (8 bytes), timestamp (8 bytes), key length (4 bytes), key, value length (4 bytes), value. Write-ahead to disk with configurable fsync policy (every message, every N messages, or every T milliseconds)
3. **Topic-based routing with wildcards**: subscribers specify topic patterns. `orders.*` matches `orders.created` and `orders.cancelled`. `events.>` matches `events.user.login` and `events.payment.refund.completed` (multi-level wildcard)
4. **Publishing**: clients connect via TCP, send a PUBLISH command with topic, optional key, and value. The key determines the partition (hash(key) % num_partitions). If no key, round-robin across partitions
5. **Consumer groups**: multiple consumers form a group identified by name. Partitions are assigned to group members using range assignment (partitions divided evenly). When a member joins or leaves, partitions rebalance across remaining members
6. **At-least-once delivery**: consumers receive messages and must send an ACK with the offset. Unacknowledged messages are redelivered after a configurable timeout. The broker tracks the committed offset per consumer group per partition
7. **Message replay**: consumers can seek to any offset in any partition and replay messages from that point forward
8. **Dead letter queue (DLQ)**: messages that fail delivery more than N times (configurable) are moved to a DLQ topic (`__dlq.<original-topic>`). The DLQ is itself a regular topic that can be consumed
9. **Backpressure**: each consumer connection has a send buffer. When the buffer is full (consumer is slow), the broker stops reading from the partition for that consumer rather than buffering unboundedly. Expose the pending message count per consumer
10. **Network protocol**: length-prefixed binary frames over TCP. Commands: PUBLISH, SUBSCRIBE, ACK, SEEK, CREATE_TOPIC, CONSUMER_JOIN, CONSUMER_LEAVE, STATS. Responses: OK, ERROR, MESSAGE, STATS_RESPONSE
11. **Metrics**: expose via a STATS command: messages published per topic, messages consumed per group, consumer lag (latest offset minus committed offset per partition per group), throughput (messages/second)
12. **Graceful shutdown**: broker drains in-flight deliveries, flushes all partition files, and persists consumer group offsets before exiting

## Acceptance Criteria

- [ ] Topics with configurable partition counts are created and accept published messages
- [ ] Messages are persisted to disk and survive broker restart
- [ ] Wildcard subscriptions match topics correctly (`*` single-level, `>` multi-level)
- [ ] Consumer groups distribute partitions among members, rebalance on join/leave
- [ ] Unacknowledged messages are redelivered after timeout
- [ ] Consumer groups track committed offsets that persist across restarts
- [ ] Message replay from arbitrary offset works correctly
- [ ] Dead letter queue receives messages after max retry attempts
- [ ] Slow consumers trigger backpressure without unbounded memory growth
- [ ] STATS command reports lag, throughput, and per-group metrics
- [ ] Broker handles 50+ concurrent client connections without deadlock
- [ ] Graceful shutdown preserves all state

## Resources

- [Kafka Protocol Guide](https://kafka.apache.org/protocol) -- binary protocol design for a production messaging system
- [Kafka Design: Log](https://kafka.apache.org/documentation/#design_filesystem) -- append-only log storage design and offset semantics
- [NATS Protocol](https://docs.nats.io/reference/reference-protocols/nats-protocol) -- text-based pub/sub protocol with wildcard subjects
- [CloudEvents Specification](https://cloudevents.io/) -- event metadata standards for interoperable messaging
- [Jay Kreps: The Log](https://engineering.linkedin.com/distributed-data/log-what-every-software-engineer-should-know-about-real-time-datas-unifying) -- foundational essay on append-only logs as a unifying abstraction
- [Martin Kleppmann: Designing Data-Intensive Applications, Chapter 11](https://dataintensive.net/) -- stream processing, messaging guarantees, and exactly-once semantics
