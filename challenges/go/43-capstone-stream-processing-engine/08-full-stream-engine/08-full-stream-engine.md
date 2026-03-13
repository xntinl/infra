# 8. Full Stream Engine

<!--
difficulty: insane
concepts: [stream-processing-engine, job-graph, task-scheduling, fault-tolerance, exactly-once, end-to-end-latency, backpressure-monitoring, job-lifecycle]
tools: [go]
estimated_time: 4h
bloom_level: create
prerequisites: [43-capstone-stream-processing-engine/07-sink-connectors]
-->

## Prerequisites

- Go 1.22+ installed
- Completed all exercises 01-07 in this section or equivalent experience building each component independently
- Understanding of how production stream processing engines (Apache Flink, Kafka Streams) orchestrate sources, operators, windowing, checkpointing, and sinks into a unified runtime

## Learning Objectives

- **Design** a complete stream processing engine by integrating sources, operators, windowing, watermarks, checkpointing, parallel execution, and sinks into a unified runtime with a declarative job API
- **Create** a job graph compiler that transforms a user-defined pipeline declaration into an executable task graph with automatic parallelism, partitioning, and checkpoint coordination
- **Evaluate** the end-to-end behavior of the integrated engine under failure scenarios, backpressure, and dynamic scaling

## The Challenge

You have built every component of a stream processing engine. Now you must integrate them into a system that a user can program with a simple, declarative API, and that the engine executes reliably across failures, backpressure, and configuration changes. This is the capstone integration challenge for stream processing.

A user should be able to write a job definition like: read from a TCP source, filter records matching a pattern, key by a field, window into 5-minute tumbling windows, aggregate with a sum function, and write results to a file sink with exactly-once delivery. The engine must compile this declaration into an executable task graph: determine the parallelism of each operator, insert partitioning (shuffle) stages where keyed operations require data redistribution, wire up channels between tasks, coordinate checkpointing across all tasks, and manage the job lifecycle (submit, run, checkpoint, cancel, recover).

The integration challenges include: ensuring the checkpointing coordinator correctly handles mixed parallel and non-parallel operators, ensuring watermarks propagate correctly through shuffles (the global watermark must account for all partitions of all parallel operators), ensuring backpressure propagates from sinks through shuffles back to sources, and ensuring that recovery restores the entire pipeline -- sources, operators, windows, and sinks -- to a consistent state.

## Requirements

1. Define a declarative `Job` API: `NewJob("name").Source(source).Map(fn).KeyBy(fn).Window(tumbling(5*time.Minute)).Reduce(fn).Sink(sink).Build()` that returns a `JobGraph`
2. Implement a `JobGraph` compiler that analyzes the declared pipeline and produces a `TaskGraph`: a DAG of `Task` nodes connected by channels, with parallelism and partitioning automatically determined
3. Automatically insert shuffle (repartition) stages before keyed operators when the upstream parallelism or partitioning does not match the keyed operator's requirements
4. Implement a `JobManager` that manages the job lifecycle: `Submit(job)`, `Cancel(jobID)`, `GetStatus(jobID)` returning states `Created`, `Running`, `Checkpointing`, `Failing`, `Cancelled`, `Finished`
5. Implement task scheduling: the `JobManager` starts all tasks in dependency order, monitors their health, and restarts failed tasks from the latest checkpoint
6. Integrate the `CheckpointCoordinator` with the full task graph: barriers must flow through shuffle stages correctly, and completion requires acknowledgment from all tasks across all parallel instances
7. Integrate watermark propagation through the full task graph: watermarks must propagate through shuffles by taking the minimum watermark across all partitions of the upstream operator
8. Implement end-to-end exactly-once delivery by coordinating source offsets, operator state checkpoints, and sink two-phase commits in a single checkpoint cycle
9. Implement backpressure monitoring: track the buffer utilization of every channel in the task graph and expose it as a metric, enabling identification of bottleneck operators
10. Implement a simple web UI or CLI tool that displays: job status, task graph topology, per-task metrics (throughput, latency, backpressure), checkpoint history, and watermark progress
11. Write an end-to-end integration test that submits a word-count job (read lines from file, split into words, key by word, tumbling window count, write to file), injects a failure mid-stream, recovers from checkpoint, and verifies that the final output contains exactly-once word counts
12. Write a performance benchmark that measures end-to-end latency and throughput for a pass-through pipeline (source -> map(identity) -> sink) to establish the engine's baseline overhead

## Hints

- For the job graph compiler, walk the declared pipeline linearly and create a `Task` for each operator, then analyze adjacent tasks to determine where shuffles are needed (before any `KeyBy` that follows a non-keyed or differently-keyed operator)
- Use a channel of `interface{}` (or a tagged union) between tasks to carry both `Record` and `CheckpointBarrier` messages through the same pipeline
- For watermark propagation through shuffles, each partition of a shuffle sends its watermark to the downstream operator, which computes the minimum across all upstream partitions
- For backpressure monitoring, use `len(ch)` and `cap(ch)` on buffered channels to compute utilization percentage -- poll periodically rather than on every record
- For the end-to-end exactly-once test, use a file source with known content, a counting window, and a file sink with two-phase commit -- after recovery, compare the output file contents with the expected counts
- For task scheduling, use a simple state machine per task: `Created -> Starting -> Running -> Failed -> Restarting -> Running` with the `JobManager` driving transitions
- The web UI can be as simple as an HTTP endpoint returning JSON that a user can query with `curl` -- the focus is on the data, not the presentation

## Success Criteria

1. The declarative job API correctly compiles a multi-operator pipeline into an executable task graph
2. Shuffle stages are automatically inserted where needed for keyed operations
3. The job manager correctly manages job lifecycle transitions including submission, cancellation, and failure recovery
4. Checkpointing coordinates correctly across all tasks in the graph, including parallel operators and shuffle stages
5. Watermarks propagate correctly through shuffles, advancing only when all upstream partitions advance
6. End-to-end exactly-once delivery is verified by the word-count test with mid-stream failure and recovery
7. Backpressure metrics accurately identify bottleneck operators
8. The status endpoint displays correct job topology, metrics, and checkpoint history
9. The engine baseline overhead is less than 100 microseconds per record for a pass-through pipeline
10. All tests pass with the `-race` flag enabled
11. No goroutine leaks after job submission, execution, failure, recovery, and completion

## Research Resources

- [Apache Flink architecture](https://nightlies.apache.org/flink/flink-docs-stable/docs/concepts/flink-architecture/) -- reference architecture for a production stream processing engine
- [Apache Flink job graph compilation](https://nightlies.apache.org/flink/flink-docs-stable/docs/internals/job_scheduling/) -- how Flink transforms user APIs into executable task graphs
- [Kafka Streams architecture](https://kafka.apache.org/documentation/streams/architecture) -- alternative stream processing architecture embedded in the application
- [Millwheel: Fault-Tolerant Stream Processing at Internet Scale](https://research.google/pubs/millwheel-fault-tolerant-stream-processing-at-internet-scale/) -- Google's production stream processing system
- [Go net/http package](https://pkg.go.dev/net/http) -- for the status/metrics web endpoint

## Summary

- A declarative job API hides the complexity of task graph construction, shuffling, and checkpoint coordination from the user
- Automatic shuffle insertion ensures keyed operations receive correctly partitioned data without manual pipeline wiring
- Job lifecycle management coordinates startup, failure detection, checkpoint-based recovery, and graceful cancellation
- End-to-end exactly-once delivery requires coordinated source offsets, operator state, and sink commits within each checkpoint cycle
- Watermark propagation through shuffles requires minimum-tracking across all upstream partitions
- Backpressure monitoring provides operational visibility into pipeline bottlenecks
- Integration testing with failure injection validates that the assembled system behaves correctly under realistic conditions
