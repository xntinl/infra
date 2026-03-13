# 6. Parallel Execution

<!--
difficulty: insane
concepts: [data-parallelism, partitioning, key-based-partitioning, hash-partitioning, rebalancing, operator-parallelism, shuffle, fan-out-fan-in]
tools: [go]
estimated_time: 3h
bloom_level: create
prerequisites: [43-capstone-stream-processing-engine/05-checkpointing, 13-goroutines-and-channels, 15-sync-primitives, 16-concurrency-patterns]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-05 (sources through checkpointing) or equivalent stream processing pipeline experience
- Completed Sections 13-16 (concurrency, channels, sync primitives, concurrency patterns) or equivalent experience

## Learning Objectives

- **Design** a parallel execution framework that partitions streams across multiple operator instances for horizontal throughput scaling
- **Create** key-based, hash-based, and round-robin partitioning strategies with configurable parallelism per operator
- **Evaluate** the impact of partitioning strategy on data locality, key-group isolation, and checkpoint consistency in a parallel pipeline

## The Challenge

A single-threaded stream processing pipeline eventually hits a throughput ceiling. Production engines scale by running multiple parallel instances of each operator, partitioning the input stream so each instance processes a subset of records. The partitioning strategy matters enormously: keyed operations (windows, aggregations) require all records with the same key to reach the same operator instance, while stateless operations (map, filter) can use any partitioning strategy.

You will implement a parallel execution layer that wraps operators in a parallelism harness. Given a configured parallelism level (e.g., 4), the harness creates that many instances of the operator, partitions incoming records across them using a configurable strategy, and merges their outputs back into a single stream. Three partitioning strategies are required: key-based (hash the record key to determine the partition), round-robin (distribute evenly regardless of key), and broadcast (send every record to every partition).

The hard problems are correctness under parallelism. Key-based partitioning must guarantee that all records with the same key reach the same partition -- across restarts, rebalances, and partition count changes. Checkpointing must coordinate across parallel instances, ensuring all instances of a parallel operator snapshot at the same barrier. And output ordering must be deterministic for keyed operations (records for the same key must maintain their relative order).

## Requirements

1. Define a `Partitioner` interface with `Partition(record Record, numPartitions int) int` that returns the target partition index for a record
2. Implement `KeyPartitioner` that hashes the record key using a consistent hash function and maps it to a partition index
3. Implement `RoundRobinPartitioner` that distributes records evenly across partitions using an atomic counter
4. Implement `BroadcastPartitioner` that sends every record to all partitions (returns a special value indicating broadcast)
5. Implement a `ParallelOperator` wrapper that takes an operator factory `func() Operator`, a parallelism level, and a partitioner, creating multiple operator instances with partitioned input channels
6. The `ParallelOperator` must fan-out input records to the correct partition's input channel based on the partitioner, and fan-in all partition output channels into a single output channel
7. For keyed operations, guarantee that records with the same key always reach the same partition instance and maintain their relative order within that partition
8. Implement partition rebalancing: when the parallelism level changes at runtime (e.g., scaling from 4 to 8 partitions), redistribute records to new partitions without losing state
9. Integrate with the checkpointing system: checkpoint barriers must be delivered to all parallel instances, and checkpoint completion requires acknowledgment from all instances of all parallel operators
10. Implement backpressure-aware fan-out: if one partition is slow, only records destined for that partition should be blocked -- records for other partitions must continue flowing
11. Expose per-partition metrics: records processed, processing latency, and backlog size for each partition of each parallel operator

## Hints

- Use `crc32.ChecksumIEEE(record.Key) % uint32(numPartitions)` for key-based partitioning -- CRC32 is fast and provides reasonable distribution
- For the fan-out, spawn a dedicated goroutine that reads from the merged input, computes the partition, and sends to the corresponding partition's input channel -- use a select with `ctx.Done()` on each send
- For backpressure-aware fan-out, use non-blocking sends with a per-partition buffer and fall back to blocking only for the target partition -- this prevents head-of-line blocking
- For broadcast, iterate over all partition channels and send the record to each one, respecting backpressure on each independently
- For the fan-in, spawn one goroutine per partition output channel that forwards records to the merged output channel, using a `sync.WaitGroup` to close the merged output when all partitions complete
- For rebalancing, implement a state migration protocol: pause the operator, snapshot current state, repartition keys to new partition assignments, redistribute state, and resume
- For checkpoint barrier integration, the fan-out goroutine must forward barrier messages to all partitions (similar to broadcast), and the fan-in must collect barriers from all partitions before forwarding a single barrier downstream

## Success Criteria

1. Key-based partitioning routes all records with the same key to the same partition instance
2. Round-robin partitioning distributes records evenly (within 5% variance) across partitions
3. Broadcast partitioning delivers every record to every partition
4. Parallel operator instances process records concurrently, achieving near-linear throughput scaling up to the CPU core count
5. Checkpoint barriers are correctly distributed to all parallel instances and completion requires all instances
6. Records for the same key maintain their relative ordering within and across partitions
7. Backpressure from one partition does not block records destined for other partitions
8. Per-partition metrics accurately reflect the workload distribution
9. All tests pass with the `-race` flag enabled

## Research Resources

- [Apache Flink parallel execution](https://nightlies.apache.org/flink/flink-docs-stable/docs/dev/datastream/execution/parallel/) -- reference design for data parallelism in stream processing
- [Consistent hashing](https://en.wikipedia.org/wiki/Consistent_hashing) -- partitioning strategy for key-based routing
- [Go concurrency patterns: fan-out, fan-in](https://go.dev/blog/pipelines) -- goroutine patterns for parallel processing
- [Apache Kafka partitioning](https://kafka.apache.org/documentation/#producerconfigs_partitioner.class) -- partitioner design in a production system
- [Go sync/atomic package](https://pkg.go.dev/sync/atomic) -- atomic counter for round-robin partitioning

## What's Next

Continue to [Sink Connectors](../07-sink-connectors/07-sink-connectors.md) where you will build the output side of the pipeline with delivery guarantees and batching.

## Summary

- Data parallelism scales stream processing throughput by running multiple instances of each operator
- Key-based partitioning guarantees that all records with the same key reach the same operator instance, which is essential for stateful operations
- Round-robin and broadcast partitioning serve stateless operations and fan-out scenarios respectively
- Backpressure-aware fan-out prevents one slow partition from blocking the entire pipeline
- Checkpoint coordination across parallel instances requires barrier broadcast and aggregated completion tracking
- Partition rebalancing enables dynamic scaling but requires state migration to maintain correctness
