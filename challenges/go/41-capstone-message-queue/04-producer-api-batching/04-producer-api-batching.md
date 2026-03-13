<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 2h
-->

# Producer API with Batching

In a high-throughput message queue, sending messages one at a time is a recipe for poor performance. The network round-trip cost per message dominates, and the disk sync cost per write is wasteful. A production-grade producer API batches messages together, amortizing network and I/O costs across many messages. Your task is to build a producer API in Go with configurable batching by size and time, asynchronous sending with futures/callbacks, retry logic with exponential backoff, idempotent production to prevent duplicates, and message compression. This is the client-side component that applications will use to publish messages.

## Requirements

1. Implement a `Producer` struct with a high-level API: `Send(topic string, key, value []byte, headers map[string]string) (*Future, error)` that enqueues a message for batched sending and returns a `Future` that resolves when the message is durably stored (acknowledged by the broker). The `Future` has `Get(timeout time.Duration) (*RecordMetadata, error)` (blocking) and `OnComplete(fn func(*RecordMetadata, error))` (callback). `RecordMetadata` contains the topic, partition, offset, and timestamp.

2. Implement the batching accumulator: messages are accumulated in per-topic-partition batches (`RecordBatch`). A batch is sent when either: (a) the batch reaches the maximum size in bytes (`batch.size`, default 16 KB), (b) the configured linger time has elapsed since the first message was added to the batch (`linger.ms`, default 5ms), or (c) `Flush()` is called explicitly. The accumulator maintains a `map[TopicPartition]*RecordBatch` of in-progress batches and a background sender goroutine that drains ready batches.

3. Implement the background sender: a dedicated goroutine that continuously checks for ready batches and sends them. It must handle multiple in-flight batches (configurable `max.in.flight.requests`, default 5) to maximize throughput. Use a semaphore (buffered channel) to limit concurrency. After sending a batch, resolve all Futures in the batch with the result (success with metadata, or error). If sending fails, move the batch to the retry queue.

4. Implement retry logic with exponential backoff: when a batch fails to send (network error, broker unavailable), retry up to `retries` times (default 3) with exponential backoff starting at `retry.backoff.ms` (default 100ms), doubling each retry up to a maximum backoff. Implement jitter (random +-25% of the backoff duration) to prevent thundering herd. Non-retryable errors (message too large, invalid topic) should fail immediately without retry. After exhausting retries, resolve all Futures in the batch with the error.

5. Implement idempotent production: assign each producer instance a unique Producer ID (PID) and assign each message a monotonically increasing sequence number per topic-partition. The broker uses the PID + sequence number to detect and deduplicate retried messages. If a batch is retried and the broker has already received it (based on sequence number), the broker returns success without storing the duplicate. Implement the producer-side sequence number tracking in a `map[TopicPartition]int64`.

6. Implement message compression: before sending a batch, optionally compress the entire batch payload using a configurable algorithm: **None** (default), **Gzip** (`compress/gzip`), **Snappy** (`github.com/golang/snappy` or a pure Go implementation), or **LZ4**. The compression type is recorded in the batch header so the broker can decompress or pass through to consumers. Implement `CompressBatch(batch *RecordBatch, codec CompressionCodec) ([]byte, error)` and `DecompressBatch(data []byte, codec CompressionCodec) (*RecordBatch, error)`.

7. Implement producer configuration and lifecycle: `NewProducer(config ProducerConfig) (*Producer, error)` with all configurable options (batch size, linger time, retries, compression, idempotence, max in-flight requests). `Close() error` flushes all pending batches, waits for all in-flight requests to complete (with a timeout), and shuts down background goroutines cleanly. `Flush() error` sends all accumulated batches immediately without waiting for linger time. Implement `Metrics() ProducerMetrics` returning: total messages sent, total bytes sent, total batches sent, average batch size, retry count, error count, and p50/p99 latency.

8. Write tests covering: single message send with future resolution, batch accumulation by size (send enough messages to trigger a size-based flush), batch accumulation by time (send one message and verify it is sent after linger time), explicit flush, retry with exponential backoff (mock a broker that fails twice then succeeds), idempotent deduplication (send a batch, simulate retry, verify only one copy stored), compression round-trip for all codecs, concurrent sends from 20 goroutines with correct future resolution, producer close with pending messages (verify all are flushed), and a throughput benchmark targeting 1 million messages/second with batching enabled.

## Hints

- The core pattern is: `Send()` adds to the accumulator and returns a future, a background goroutine drains ready batches and sends them, and futures are resolved from the send result. Use channels to communicate between the API goroutine and the sender goroutine.
- For the `Future`, use a struct with a channel: `type Future struct { ch chan result }`. `Get()` reads from the channel (with timeout via `select`). `OnComplete()` spawns a goroutine that reads and calls the callback. Resolve by sending the result to the channel (buffer size 1).
- The linger timer: when the first message is added to an empty batch, start a timer. When the timer fires, mark the batch as ready. Use `time.AfterFunc` for this. Cancel the timer if the batch fills up by size before the timer fires.
- For exponential backoff with jitter: `backoff := baseBackoff * (1 << retryCount)`, then `jitteredBackoff := backoff * (0.75 + rand.Float64() * 0.5)`.
- Idempotent production requires the broker to maintain a mapping of `(PID, TopicPartition, SequenceNumber) -> bool`. The producer assigns sequence numbers, and the broker checks them on append.
- For compression, batch all the record values together before compressing. The batch header contains a codec byte (0=none, 1=gzip, 2=snappy, 3=lz4).

## Success Criteria

1. A single `Send()` call returns a `Future` that resolves with correct `RecordMetadata` (topic, partition, offset).
2. Batching accumulates messages and sends them together: 1000 messages sent rapidly result in far fewer than 1000 broker round-trips.
3. Linger time works: a single message is sent after the configured linger duration even without more messages.
4. Retries with exponential backoff succeed after transient failures, with the correct number of retry attempts and increasing backoff durations.
5. Idempotent production prevents duplicate messages: a retried batch results in exactly one copy stored.
6. Compression reduces batch size by at least 50% for repetitive text payloads, verified by comparing compressed and uncompressed sizes.
7. Concurrent sends from 20 goroutines all resolve their futures correctly with no data races (verified with `-race`).
8. Throughput benchmark achieves at least 500,000 messages/second with batching and compression enabled.

## Research Resources

- [Kafka Producer Internals](https://kafka.apache.org/documentation/#producerconfigs)
- [Kafka Producer Design (Confluent)](https://developer.confluent.io/courses/architecture/producer/)
- [Idempotent Producer (KIP-98)](https://cwiki.apache.org/confluence/display/KAFKA/KIP-98+-+Exactly+Once+Delivery+and+Transactional+Messaging)
- [Exponential Backoff and Jitter (AWS Architecture Blog)](https://aws.amazon.com/blogs/architecture/exponential-backoff-and-jitter/)
- [Go compress/gzip Package](https://pkg.go.dev/compress/gzip)
- [Snappy Compression in Go](https://github.com/golang/snappy)
