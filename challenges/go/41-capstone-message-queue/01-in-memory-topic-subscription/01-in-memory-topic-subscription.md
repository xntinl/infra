<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 2h
-->

# In-Memory Topic and Subscription System

Every message queue begins with the fundamental abstraction of topics and subscriptions. A producer writes messages to a named topic, and one or more subscribers receive those messages. Your task is to build the core in-memory topic and subscription system in Go that supports named topics, multiple concurrent publishers and subscribers, at-least-once delivery semantics, message ordering guarantees within a topic, fan-out to multiple independent subscribers, and configurable delivery modes (broadcast vs. competing consumers). This component forms the backbone of the message queue you will build across this capstone section.

## Requirements

1. Define the core message type: `Message` struct with fields `ID string` (UUID), `Topic string`, `Key []byte` (optional partition key), `Value []byte` (payload), `Headers map[string]string` (metadata), `Timestamp time.Time`, `Offset int64` (assigned by the topic upon append). The offset is a monotonically increasing sequence number unique within a topic. Implement `Message.Size() int` that returns the total memory footprint of the message (for memory accounting).

2. Implement a `Topic` struct that stores an ordered sequence of messages in a thread-safe manner. `Publish(msg *Message) (int64, error)` appends the message, assigns the next offset, sets the timestamp if not already set, and returns the assigned offset. The topic must handle concurrent publishers using fine-grained locking (a mutex on the message log, not a global lock). Implement `GetMessage(offset int64) (*Message, error)` for random access by offset.

3. Implement a `Subscription` struct that tracks a named subscriber's position within a topic. Each subscription has a `name string`, a `topic *Topic`, a `currentOffset int64` (the next offset to deliver), and an `ackOffset int64` (the last acknowledged offset). Implement `Poll(maxMessages int, timeout time.Duration) ([]*Message, error)` that returns up to `maxMessages` messages starting from `currentOffset`, blocking for up to `timeout` if no new messages are available. Use `sync.Cond` for efficient blocking without busy-waiting.

4. Implement two subscription modes: **Broadcast** (each subscription receives every message independently, like pub/sub) and **Competing Consumers** (multiple subscriptions sharing the same subscription name form a consumer group where each message is delivered to exactly one consumer in the group). For competing consumers, implement round-robin or least-loaded message distribution across the group members. Switching between modes is configured at subscription creation time.

5. Implement message acknowledgment: after processing a message, the subscriber calls `Ack(offset int64) error` to acknowledge it. Unacknowledged messages are redelivered after a configurable visibility timeout (default 30 seconds). Track per-message delivery state: `PENDING`, `DELIVERED`, `ACKNOWLEDGED`. If a message is delivered but not acknowledged within the timeout, it returns to `PENDING` and is redelivered to the next `Poll()`. Implement `Nack(offset int64) error` to immediately requeue a message for redelivery.

6. Implement a `Broker` struct that manages multiple topics and subscriptions. `CreateTopic(name string, opts TopicOptions) (*Topic, error)` with options for max message size, retention policy, and partition count (for future use). `DeleteTopic(name string) error` removes the topic and all its subscriptions. `Subscribe(topicName, subscriptionName string, opts SubscriptionOptions) (*Subscription, error)` creates or joins a subscription. `ListTopics() []string` and `ListSubscriptions(topicName string) []string` for discovery.

7. Implement backpressure mechanisms: a topic can have a configurable maximum message count or maximum total memory usage. When the limit is reached, `Publish()` either blocks until space is available (back-pressure mode) or returns an error immediately (reject mode). The mode is configurable per topic. Implement `Topic.Stats() TopicStats` returning current message count, total size in bytes, oldest message timestamp, and newest message timestamp.

8. Write tests covering: publishing 10,000 messages to a topic from 10 concurrent goroutines and verifying all messages are received in offset order by a subscriber, broadcast mode delivering every message to 5 independent subscribers, competing consumer mode delivering each message to exactly one of 5 consumers (verify no duplicates across consumers and no missed messages), acknowledgment timeout and redelivery (deliver a message, don't ack, wait for timeout, verify redelivery), Nack immediate redelivery, backpressure blocking and reject modes, topic deletion while subscribers are polling (verify clean shutdown), and a benchmark measuring messages/second throughput for publish and consume.

## Hints

- For the message log, a simple `[]*Message` slice with a `sync.Mutex` works for in-memory storage. The offset is the slice index. For better concurrency, consider a ring buffer or segmented log.
- `sync.Cond` is perfect for the blocking poll: subscribers wait on the condition variable, and publishers broadcast after each append. Use `Cond.Wait()` with a timeout via a separate goroutine that calls `Cond.Signal()` after the timeout duration.
- For competing consumers, maintain a slice of consumer channels or a shared iterator. Round-robin: track the last-assigned consumer index and rotate. Least-loaded: track pending message counts per consumer and assign to the lowest.
- Visibility timeout tracking: maintain a map of `deliveredAt time.Time` per message per subscription. A background goroutine periodically checks for expired deliveries and resets them to PENDING.
- For backpressure with blocking, use another `sync.Cond` that publishers wait on when the topic is full, and consumers signal when they acknowledge messages (freeing space).
- Generate UUIDs with `crypto/rand` and hex encoding, or use `github.com/google/uuid` if you prefer a dependency.

## Success Criteria

1. Messages published concurrently from 10 goroutines all receive unique, monotonically increasing offsets with no gaps.
2. A single subscriber in broadcast mode receives every message in the exact order published.
3. Five competing consumers collectively receive every message exactly once (no duplicates, no losses) verified by collecting all received offsets and comparing to the published set.
4. Unacknowledged messages are redelivered after the visibility timeout with no acknowledged messages being redelivered.
5. Nack causes immediate redelivery on the next Poll without waiting for the visibility timeout.
6. Backpressure blocks publishers when the topic is full and unblocks them when space is freed by consumer acknowledgments.
7. Topic deletion cleanly shuts down all active subscribers without deadlocks or panics.
8. Throughput benchmark achieves at least 500,000 messages/second for publish and 200,000 messages/second for consume on a single topic.

## Research Resources

- [Apache Kafka Architecture (Topics, Partitions, Consumer Groups)](https://kafka.apache.org/documentation/#design)
- [AWS SQS vs SNS (Competing Consumers vs Fan-Out)](https://aws.amazon.com/messaging/)
- [NATS Messaging System Design](https://docs.nats.io/nats-concepts/subjects)
- [Go sync.Cond Documentation](https://pkg.go.dev/sync#Cond)
- [Message Queue Fundamentals (Martin Kleppmann)](https://dataintensive.net/)
- [Designing Data-Intensive Applications - Chapter 11](https://dataintensive.net/)
