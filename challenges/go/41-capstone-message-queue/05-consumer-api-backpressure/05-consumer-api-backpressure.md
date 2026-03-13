<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 2h
-->

# Consumer API with Backpressure

The consumer side of a message queue is where the real complexity lies. Consumers must balance throughput against processing capacity, handle failures without losing messages, manage their position in the message stream, and coordinate with the broker to avoid overwhelming downstream systems. Your task is to build a consumer API in Go with pull-based message fetching, configurable flow control and backpressure, at-least-once and at-most-once delivery guarantees, prefetch buffers for latency hiding, and graceful shutdown with offset commit. This is the client-side component that applications use to consume messages reliably.

## Requirements

1. Implement a `Consumer` struct with a poll-based API: `Poll(timeout time.Duration) ([]*ConsumerRecord, error)` fetches the next batch of available messages from assigned partitions. The consumer internally maintains a prefetch buffer per partition that is filled by background fetch goroutines. `Poll` drains from the prefetch buffer, returning up to `max.poll.records` (default 500) records. If the buffer is empty, it blocks for up to `timeout`. `ConsumerRecord` includes the message data plus partition, offset, and a reference to the consumer for acknowledgment.

2. Implement the prefetch buffer with flow control: a background goroutine per assigned partition continuously fetches messages from the broker into a bounded channel (size configurable via `fetch.buffer.size`, default 1000 messages or 1 MB). When the buffer is full, the fetcher pauses (backpressure). When `Poll` drains records from the buffer, the fetcher resumes. Implement `Pause(partitions ...int)` and `Resume(partitions ...int)` to manually control fetching per partition -- paused partitions stop filling their buffer but retain buffered messages.

3. Implement offset management with two modes: **Auto-commit** (committed offsets are updated automatically after each `Poll` at a configurable interval, default every 5 seconds) and **Manual commit** (`CommitSync(offsets map[int]int64) error` blocks until the offset is durably stored, `CommitAsync(offsets map[int]int64, callback func(error))` commits asynchronously). Implement `Committed(partition int) (int64, error)` to retrieve the last committed offset and `Position(partition int) int64` to get the current consumer position (next offset to fetch).

4. Implement delivery guarantees: **At-most-once** (commit offsets before processing -- if processing fails, the message is lost but never reprocessed), **At-least-once** (commit offsets after processing -- if the consumer crashes between processing and committing, the message is reprocessed), and **Effectively-once** (combine idempotent processing with at-least-once delivery). Implement these as configurable modes that change the relationship between `Poll`, processing, and `CommitSync`. Document the trade-offs of each mode clearly in code comments.

5. Implement consumer position control: `Seek(partition int, offset int64) error` sets the consumer's position for a partition to a specific offset (the next `Poll` will return messages starting from this offset). `SeekToBeginning(partitions ...int) error` sets position to the earliest available offset. `SeekToEnd(partitions ...int) error` sets position to the latest offset (only new messages). `SeekToTimestamp(partition int, timestamp time.Time) (int64, error)` finds the offset of the first message at or after the given timestamp using binary search on the partition's time index.

6. Implement graceful shutdown and rebalance handling: `Close() error` stops all background fetchers, commits final offsets (if auto-commit is enabled), leaves the consumer group, and releases all resources. During rebalance, the consumer must: commit current offsets for revoked partitions, clear prefetch buffers for revoked partitions, and start fetching for newly assigned partitions. Implement the `ConsumerRebalanceListener` interface with `OnPartitionsRevoked` and `OnPartitionsAssigned` callbacks.

7. Implement consumer metrics and health monitoring: `Metrics() ConsumerMetrics` returns: messages consumed per second (throughput), bytes consumed per second, average processing latency (time between poll and next poll), fetch latency (time to fill prefetch buffer), consumer lag per partition (difference between latest broker offset and consumer position), prefetch buffer utilization (percentage full), and rebalance count. Implement a `HealthCheck() HealthStatus` that returns HEALTHY, DEGRADED (high lag), or UNHEALTHY (no heartbeat, rebalance stuck).

8. Write tests covering: basic poll-process-commit cycle, prefetch buffer filling and draining under varying consumer speeds, backpressure (slow consumer causes fetcher to pause), pause/resume per partition, auto-commit vs manual commit behavior, seek to specific offset and verify correct messages received, seek to timestamp with various edge cases, graceful shutdown with final offset commit, rebalance with partition revocation and assignment, delivery guarantee modes (simulate crash after processing but before commit), concurrent access to consumer from multiple goroutines (verify thread safety), and a throughput benchmark consuming 1 million messages measuring end-to-end latency percentiles.

## Hints

- The prefetch buffer is the key to hiding fetch latency. Model it as a buffered channel: `buffer chan []*ConsumerRecord` with capacity `fetch.buffer.size / batchSize`. The fetcher goroutine fills it, and `Poll` drains it.
- For backpressure, the fetcher goroutine's `send` to the channel will block when the buffer is full. This naturally creates backpressure without any explicit signaling.
- Pause/Resume: use an `atomic.Bool` per partition. The fetcher checks it before each fetch. When paused, the fetcher sleeps briefly and rechecks rather than exiting (so it can resume quickly).
- For SeekToTimestamp, the broker needs a time-indexed data structure. If your storage engine stores timestamps, implement a binary search over the segment's timestamp index. If not, scan the segment log from the beginning.
- Auto-commit timing: use a `time.Ticker` in the poll loop. On each tick, commit the offsets of all records returned by previous polls. Be careful to only commit offsets of records that have been returned to (and presumably processed by) the application.
- For the effectively-once mode, the consumer must track processed message IDs externally (in a database or cache). Provide this as a pattern/example rather than a built-in mode, since it requires application-specific logic.

## Success Criteria

1. A consumer polling at maximum speed receives all messages in offset order with no gaps or duplicates.
2. A slow consumer (processing takes 100ms per message) naturally backpressures the prefetch fetcher without memory growing unboundedly.
3. Pause/Resume correctly halts and resumes fetching for individual partitions without affecting other partitions.
4. Auto-commit correctly commits offsets at the configured interval, verified by checking committed offsets from another consumer instance.
5. Manual `CommitSync` durably stores offsets that survive consumer restart.
6. `Seek` correctly repositions the consumer, and subsequent polls return messages starting from the seeked offset.
7. Graceful shutdown commits final offsets and stops all background goroutines within 5 seconds.
8. Consumer lag metrics accurately reflect the number of unprocessed messages and decrease as messages are consumed.

## Research Resources

- [Kafka Consumer API Design](https://kafka.apache.org/documentation/#consumerapi)
- [Kafka Consumer Internals (Confluent)](https://developer.confluent.io/courses/architecture/consumer-group-protocol/)
- [Backpressure in Message Processing Systems](https://www.reactivemanifesto.org/glossary#Back-Pressure)
- [At-Least-Once vs At-Most-Once vs Exactly-Once](https://www.confluent.io/blog/exactly-once-semantics-are-possible-heres-how-apache-kafka-does-it/)
- [Go Channels as Bounded Buffers](https://go.dev/tour/concurrency/3)
- [Consumer Offset Management Best Practices](https://docs.confluent.io/platform/current/clients/consumer.html)
