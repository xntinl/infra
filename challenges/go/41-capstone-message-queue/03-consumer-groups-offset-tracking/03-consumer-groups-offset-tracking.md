<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 2h
-->

# Consumer Groups and Offset Tracking

In a real message queue, multiple consumers must coordinate to process messages without duplicates or gaps. Consumer groups enable this: a group of consumers shares the work of processing a topic, with each message delivered to exactly one consumer in the group. The critical challenge is tracking which messages each consumer has processed (offset tracking) and handling consumer failures gracefully through rebalancing. Your task is to implement a complete consumer group system with partition assignment, offset commit and fetch, rebalancing on consumer join/leave, and exactly-once processing guarantees.

## Requirements

1. Implement topic partitioning: a topic with N partitions distributes messages across partitions using a configurable partitioning strategy. Implement three strategies: **Round-Robin** (messages assigned to partitions in rotation), **Key-Based** (hash the message key modulo N to determine the partition, ensuring messages with the same key always go to the same partition), and **Custom** (accept a `Partitioner` interface `func(key []byte, numPartitions int) int`). Each partition is an independent ordered log with its own offset sequence.

2. Implement `ConsumerGroup` struct that manages a named group of consumers subscribing to a topic. `JoinGroup(groupName, consumerID string) (*GroupMember, error)` adds a consumer to the group. `LeaveGroup(consumerID string) error` removes a consumer. The group maintains a member list, the current partition assignment, and a generation ID that increments on every rebalance. Each `GroupMember` has a unique ID, assigned partitions, and a heartbeat timestamp.

3. Implement partition assignment strategies: **Range** (sort partitions and consumers, assign contiguous ranges), **RoundRobin** (assign partitions to consumers in rotation), and **Sticky** (try to preserve the previous assignment as much as possible when rebalancing, only reassigning partitions from departed consumers). The assignment is recalculated whenever a consumer joins or leaves the group. Implement the `Assignor` interface so custom strategies can be plugged in.

4. Implement offset tracking with durable storage: each consumer group tracks committed offsets per partition. `CommitOffset(partition int, offset int64) error` records that all messages up to and including the given offset have been processed. `FetchOffset(partition int) (int64, error)` returns the last committed offset for a partition. Store offsets in a dedicated internal topic (`__consumer_offsets`) or in a file-based store, with periodic checkpointing for durability. Support auto-commit (commit every N seconds) and manual commit modes.

5. Implement consumer heartbeats and failure detection: each active consumer sends periodic heartbeats to the group coordinator (configurable interval, default 3 seconds). If a consumer misses heartbeats for longer than the session timeout (configurable, default 30 seconds), it is considered dead and removed from the group, triggering a rebalance. Implement `Heartbeat(consumerID string) error` and a background goroutine in the coordinator that checks for expired consumers.

6. Implement rebalancing: when the group membership changes (join, leave, or failure), halt message delivery to all consumers, recalculate the partition assignment using the configured strategy, notify all consumers of their new partition assignments, and resume delivery. During rebalancing, consumers must not receive messages (they observe a rebalance-in-progress state). Implement a `RebalanceListener` callback interface with `OnPartitionsRevoked(partitions []int)` and `OnPartitionsAssigned(partitions []int)` so consumers can commit offsets and clean up state before/after rebalancing.

7. Implement consumer lag tracking: `GetLag(groupName string) map[int]int64` returns the difference between the latest offset in each partition and the consumer group's committed offset for that partition. This represents the number of unprocessed messages. Implement `GetGroupInfo(groupName string) GroupInfo` returning member list, partition assignments, committed offsets, and total lag. These metrics are essential for monitoring.

8. Write tests covering: partition assignment correctness for all three strategies (Range, RoundRobin, Sticky) with various consumer/partition counts, offset commit and fetch round-trips, rebalancing triggered by consumer join (verify partition redistribution), rebalancing triggered by consumer departure (verify orphaned partitions are reassigned), failure detection via missed heartbeats (mock time for fast testing), sticky assignment minimizing partition movement on rebalance, consumer lag calculation accuracy, concurrent commit and fetch operations, and an end-to-end test with 3 consumers and 12 partitions processing 100,000 messages with correct exactly-once processing verified by offset gaps.

## Hints

- For key-based partitioning, use `hash/fnv` (FNV-1a) for fast, well-distributed hashing: `h := fnv.New32a(); h.Write(key); partition := int(h.Sum32()) % numPartitions`.
- For the Range assignment: sort partitions [0..11] and consumers [A, B, C]. With 12 partitions and 3 consumers, each gets 4: A=[0,1,2,3], B=[4,5,6,7], C=[8,9,10,11]. With 10 partitions and 3 consumers: A=[0,1,2,3], B=[4,5,6], C=[7,8,9] (first consumers get the remainder).
- Sticky assignment: start with the previous assignment, identify unassigned partitions (from departed consumers), distribute them among remaining consumers preferring those with fewer partitions.
- For the rebalance protocol, use a state machine: STABLE (normal operation) -> PREPARING_REBALANCE (triggered by membership change, wait for all consumers to acknowledge) -> COMPLETING_REBALANCE (compute new assignment) -> STABLE.
- Auto-commit can be implemented as a background goroutine per consumer that calls `CommitOffset` at the configured interval with the latest consumed offset. Be careful: auto-commit may commit offsets for messages that haven't been fully processed.
- Mock time in tests using a `Clock` interface with `Now() time.Time` and `After(d time.Duration) <-chan time.Time`, substituting a controllable fake clock in tests.

## Success Criteria

1. Key-based partitioning always routes messages with the same key to the same partition, verified across 10,000 messages with 100 distinct keys.
2. Three consumers in a group collectively receive all messages from all partitions with no duplicates (verified by offset tracking).
3. When a consumer leaves, its partitions are reassigned to remaining consumers within the session timeout period.
4. Sticky assignment moves the minimum number of partitions on rebalance: when 1 of 3 consumers leaves, only the departed consumer's partitions move.
5. Committed offsets survive consumer restart: stop a consumer, restart it, verify it resumes from the committed offset.
6. Consumer lag accurately reflects the number of unprocessed messages and decreases as consumers process messages.
7. Heartbeat-based failure detection removes dead consumers within 2x the session timeout.
8. 100,000 messages across 12 partitions are processed exactly once by a group of 3 consumers without any gaps or duplicates in the committed offsets.

## Research Resources

- [Kafka Consumer Groups and Partition Assignment](https://kafka.apache.org/documentation/#consumerconfigs)
- [Kafka Rebalance Protocol](https://cwiki.apache.org/confluence/display/KAFKA/KIP-429%3A+Kafka+Consumer+Incremental+Rebalance+Protocol)
- [Sticky Partition Assignment Strategy (KIP-54)](https://cwiki.apache.org/confluence/display/KAFKA/KIP-54+-+Sticky+Partition+Assignment+Strategy)
- [Offset Management in Kafka](https://kafka.apache.org/documentation/#impl_offsettracking)
- [Consumer Group Protocol (Confluent)](https://developer.confluent.io/courses/architecture/consumer-group-protocol/)
- [FNV Hash in Go](https://pkg.go.dev/hash/fnv)
