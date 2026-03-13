<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 4h+
-->

# Full Message Queue System

This is the culmination of the message queue capstone. You will integrate every component you have built -- in-memory topics, persistent storage, consumer groups, producer API with batching, consumer API with backpressure, retention and compaction, and the TCP protocol -- into a single cohesive, production-style distributed message queue. The result will be a system that applications can use to publish and consume messages over the network, with durable storage, consumer group coordination, configurable retention, log compaction, and monitoring. This is your Kafka-in-miniature.

## Requirements

1. Create a `Broker` struct that composes all subsystems: a `LogManager` for persistent message storage (segmented logs with indexes), a `TopicManager` for topic lifecycle and partition management, a `GroupCoordinator` for consumer group membership and offset tracking, a `RetentionManager` for cleanup policies, a `CompactionManager` for log compaction, and a `TCPServer` for the network protocol. Implement `NewBroker(config BrokerConfig) (*Broker, error)` that initializes all subsystems and `Start() error` that begins listening for connections and starts all background goroutines. Implement `Shutdown(ctx context.Context) error` for graceful shutdown with a timeout.

2. Implement the complete produce path end-to-end: a client sends a PRODUCE request over TCP, the server decodes it, the broker validates the topic and partition, the message is appended to the partition's persistent log (with WAL for durability), the partition's index is updated, waiting consumers are notified (via condition variable), and the response with the assigned offset is sent back to the client. The entire path must handle errors at every stage and return appropriate error codes. Implement acknowledgment modes: `acks=0` (fire-and-forget, respond immediately), `acks=1` (respond after leader write), and `acks=all` (for future replication support, behaves like `acks=1` for now).

3. Implement the complete consume path end-to-end: a client joins a consumer group via JOIN_GROUP request, receives its partition assignments, sends FETCH requests for its assigned partitions, receives messages, processes them, and sends COMMIT_OFFSET requests to record progress. The fetch path reads from the persistent log via the index, serves from the OS page cache for recent messages, and falls back to disk for older messages. Implement long-polling: if no new messages are available, hold the FETCH request for up to `fetch.wait.max.ms` (default 500ms) before responding with an empty result.

4. Implement topic management: CREATE_TOPIC creates the topic directory, partition subdirectories, and initial segment files. DELETE_TOPIC closes all partitions, removes all consumer group state for the topic, and deletes all files. METADATA returns the list of topics with their partition counts and leader information (for single-broker, the leader is always this broker). Implement topic configuration stored in a JSON file per topic: retention.ms, retention.bytes, cleanup.policy, max.message.bytes, num.partitions, and compaction settings.

5. Implement the consumer group coordinator as a first-class broker component: handle JOIN_GROUP (register consumer, trigger rebalance if group membership changes), HEARTBEAT (update consumer liveness), LEAVE_GROUP (deregister consumer, trigger rebalance), COMMIT_OFFSET (store offsets durably in the `__consumer_offsets` internal topic), FETCH_OFFSET (retrieve committed offsets). The coordinator must handle the full rebalance protocol: detect the trigger (new member, departed member, failed heartbeat), stop message delivery during rebalance, compute new partition assignment, notify all group members of their new assignments, and resume delivery.

6. Implement a monitoring and administration system: expose a `/metrics` HTTP endpoint (separate from the TCP protocol) with Prometheus-compatible metrics including: messages produced per second (by topic), messages consumed per second (by group), consumer lag per group per partition, active connections count, bytes in/out per second, segment count and total size per topic, compaction progress, and error rates. Implement admin CLI commands: `mq-admin topics list`, `mq-admin topics create <name> --partitions N`, `mq-admin topics delete <name>`, `mq-admin groups list`, `mq-admin groups describe <name>`, `mq-admin topics describe <name>` (show partition details, retention config, size).

7. Implement crash recovery and data integrity: on broker startup, scan all topic directories, load segment metadata, rebuild indexes if necessary (by scanning segment files), recover consumer group offsets from the `__consumer_offsets` topic, and resume all background processes (retention, compaction, heartbeat checking). Implement a consistency check: `mq-admin fsck` validates all segment CRC checksums, index consistency, and offset continuity, reporting any corruption found.

8. Write integration tests that exercise the complete system end-to-end: (a) produce 100,000 messages to a 3-partition topic using 5 concurrent producer clients, consume with a 3-consumer group, verify every message is received exactly once; (b) consumer failure and rebalance: kill one of 3 consumers, verify its partitions are reassigned and all subsequent messages are still processed; (c) crash recovery: produce messages, kill the broker process, restart, verify all acknowledged messages are present; (d) retention: produce messages, configure 1-second retention, verify old segments are deleted after expiry; (e) compaction: produce messages with 100 unique keys, trigger compaction, verify only 100 messages remain with latest values; (f) performance benchmarks: measure end-to-end latency (p50, p99, p99.9), throughput (messages/second for produce and consume), and disk space usage over time with retention active. Target: 100,000+ messages/second produce throughput, sub-10ms p99 end-to-end latency on localhost.

## Hints

- The integration challenge is harder than any individual component. Start by getting the produce and consume paths working end-to-end with a single partition and a single consumer, then add partitions, consumer groups, retention, and compaction incrementally.
- For the broker's internal architecture, use a channel-based dispatcher: the TCP server decodes requests and sends them to a request channel, a pool of handler goroutines processes them and sends responses to a response channel, and the TCP writer sends responses back to clients.
- The `__consumer_offsets` topic is special: it is an internal compacted topic where the key is `(group, topic, partition)` and the value is the committed offset. On startup, compact and replay this topic to rebuild the in-memory offset map.
- For the admin CLI, use a separate binary (`cmd/mq-admin/main.go`) that connects to the broker's HTTP admin endpoint. The broker exposes both TCP (for clients) and HTTP (for admin/metrics) on different ports.
- Long-polling for FETCH: when there are no new messages, register a waiter on the partition's condition variable with a timeout. When a new message is produced, the producer signals the condition variable, waking all waiting fetchers. Use `context.WithTimeout` for clean timeout handling.
- For Prometheus-compatible metrics, use the text exposition format: `# HELP metric_name Description\n# TYPE metric_name counter\nmetric_name{labels} value\n`. You can generate this from Go structs without importing the Prometheus client library.

## Success Criteria

1. The broker starts, accepts TCP connections, and handles produce/fetch requests correctly for multiple topics and partitions.
2. 100,000 messages produced by 5 concurrent clients are all consumed exactly once by a 3-consumer group with no message loss or duplication.
3. Consumer group rebalance after a consumer failure correctly reassigns partitions and resumes processing within 30 seconds.
4. Crash recovery correctly restores all acknowledged messages and consumer group state after an unclean broker shutdown.
5. Time-based retention automatically deletes expired segments, and log compaction reduces the log to latest-per-key entries.
6. The `/metrics` HTTP endpoint exposes accurate, up-to-date metrics in Prometheus format.
7. The admin CLI can list topics, describe groups, and run filesystem checks against a running broker.
8. Performance benchmarks achieve 100,000+ messages/second produce throughput and sub-10ms p99 latency on localhost.
9. All tests pass with the `-race` detector enabled, confirming no data races in the concurrent system.

## Research Resources

- [Apache Kafka Architecture (comprehensive)](https://kafka.apache.org/documentation/#design)
- [Designing Data-Intensive Applications - Chapter 11: Stream Processing](https://dataintensive.net/)
- [The Log: What every software engineer should know (Jay Kreps)](https://engineering.linkedin.com/distributed-systems/log-what-every-software-engineer-should-know-about-real-time-datas-unifying)
- [Building a Distributed Log from Scratch (Travis Jeffery)](https://github.com/travisjeffery/proglog)
- [NATS Server Implementation in Go](https://github.com/nats-io/nats-server)
- [Redpanda - Kafka-compatible Queue in C++ (for design reference)](https://github.com/redpanda-data/redpanda)
- [Prometheus Exposition Format](https://prometheus.io/docs/instrumenting/exposition_formats/)
- [NSQ - Real-time Distributed Message Queue in Go](https://github.com/nsqio/nsq)
