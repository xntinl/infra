# 7. Sink Connectors

<!--
difficulty: insane
concepts: [sink-connectors, delivery-guarantees, at-least-once, exactly-once, two-phase-commit, batching, output-buffering, idempotent-writes]
tools: [go]
estimated_time: 2h
bloom_level: create
prerequisites: [43-capstone-stream-processing-engine/06-parallel-execution, 19-io-and-filesystem, 33-tcp-udp-and-networking]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-06 (sources through parallel execution) or equivalent stream processing pipeline experience
- Completed Sections 19 (I/O and filesystem) and 33 (networking) or equivalent experience

## Learning Objectives

- **Design** a unified `Sink` interface that abstracts over heterogeneous output destinations with configurable delivery guarantees
- **Create** file, TCP, and HTTP sink connectors with batching, buffering, and exactly-once delivery semantics
- **Evaluate** the trade-offs between at-least-once and exactly-once delivery guarantees and their implementation cost in terms of complexity and latency

## The Challenge

A stream processing engine that cannot write results anywhere is useless. Sink connectors are the output side of the pipeline -- they take processed records and write them to external systems: files, databases, message queues, HTTP endpoints, or TCP sockets. The critical challenge is not writing bytes, but providing delivery guarantees in the face of failures.

At-least-once delivery ensures every record is written at least once, but may produce duplicates on recovery. Exactly-once delivery ensures every record is written exactly once, but requires coordination between the sink and the checkpointing system. The standard approach is the two-phase commit pattern: during a checkpoint, the sink pre-commits its buffered writes, and on checkpoint completion, it commits them atomically. If a failure occurs before commit, the pre-committed data is rolled back and replayed from the checkpoint.

You will build three sink connectors (file, TCP, HTTP) with configurable batching and buffering, then implement the two-phase commit protocol for exactly-once delivery on the file sink. Each sink must handle partial failures (e.g., a batch write that partially succeeds), implement retry logic for transient errors, and integrate with the checkpointing system for coordinated commits.

## Requirements

1. Define a `Sink` interface with `Open(ctx context.Context) error`, `Write(records []Record) error`, `Flush() error`, and `Close() error` methods
2. Define a `CheckpointableSink` interface extending `Sink` with `PrepareCommit(checkpointID uint64) error` and `Commit(checkpointID uint64) error` for two-phase commit integration
3. Implement `FileSink` that writes records as newline-delimited JSON to a configurable file path, supporting both append and rotate-on-size modes
4. Implement `TCPSink` that connects to a configurable address and writes records as newline-delimited messages, with automatic reconnection on connection loss
5. Implement `HTTPSink` that POSTs batches of records to a configurable URL, with configurable batch size, flush interval, retry count, and retry backoff
6. Implement batching for all sinks: accumulate records in an internal buffer and flush when either the batch size is reached or the flush interval expires, whichever comes first
7. Implement the two-phase commit protocol for `FileSink`: on `PrepareCommit`, write buffered records to a temporary file; on `Commit`, atomically rename the temporary file to its final path
8. Handle partial write failures: if a batch write to TCP or HTTP partially succeeds, track which records were successfully written and retry only the failed records
9. Implement idempotent write support for `HTTPSink`: include a deduplication key (derived from checkpoint ID and batch sequence number) in each request so the receiving endpoint can detect and ignore duplicates
10. Expose per-sink metrics: records written, bytes written, batches flushed, flush latency, errors, and retries

## Hints

- For file rotation, track the current file size and open a new file (with a timestamped suffix) when the size exceeds the configured limit
- For TCP reconnection, use a state machine: Connected -> Disconnected -> Reconnecting, with exponential backoff on reconnection attempts and buffering records during disconnection
- For HTTP batching, use a `time.Timer` that resets on every record arrival -- when it fires, flush the batch; also flush when the batch size limit is reached
- For two-phase commit, the "prepare" phase writes to a file named `{path}.checkpoint-{id}.tmp` and the "commit" phase renames it to `{path}.checkpoint-{id}` -- on recovery, delete uncommitted `.tmp` files
- For partial failure tracking, assign each record in a batch a sequence number and use a bitset or slice of booleans to track which records were successfully written
- For idempotent HTTP writes, include an `Idempotency-Key` header with the value `{checkpointID}-{batchSequence}` in each POST request
- Use `bufio.Writer` for file sinks to reduce syscall overhead from frequent small writes

## Success Criteria

1. `FileSink` correctly writes records as newline-delimited JSON and rotates files at the configured size
2. `TCPSink` reconnects automatically after connection loss and resumes writing without data loss
3. `HTTPSink` batches records and retries failed requests with exponential backoff
4. Batching correctly flushes on either batch size or flush interval, whichever comes first
5. Two-phase commit on `FileSink` produces exactly-once output after checkpoint and recovery
6. Partial write failures are handled by retrying only failed records
7. Idempotent HTTP writes prevent duplicate records at the receiving endpoint
8. Per-sink metrics accurately reflect the number of records and bytes written
9. All tests pass with the `-race` flag enabled

## Research Resources

- [Apache Flink Sink API](https://nightlies.apache.org/flink/flink-docs-stable/docs/dev/datastream/sink/) -- reference design for sink connectors with delivery guarantees
- [Two-phase commit protocol](https://en.wikipedia.org/wiki/Two-phase_commit_protocol) -- the coordination protocol for exactly-once delivery
- [Apache Kafka exactly-once semantics](https://www.confluent.io/blog/exactly-once-semantics-are-possible-heres-how-apache-kafka-does-it/) -- production exactly-once implementation in Kafka
- [Go bufio.Writer](https://pkg.go.dev/bufio#Writer) -- buffered writer for efficient file I/O
- [Idempotency keys (Stripe)](https://stripe.com/docs/api/idempotent_requests) -- practical idempotent write pattern

## What's Next

Continue to [Full Stream Engine](../08-full-stream-engine/08-full-stream-engine.md) where you will integrate all components into a complete stream processing engine.

## Summary

- Sink connectors write processed records to external systems with configurable delivery guarantees
- Batching reduces I/O overhead by accumulating records and flushing them in groups
- The two-phase commit protocol coordinates sinks with checkpointing to achieve exactly-once delivery
- Idempotent writes provide deduplication at the receiving endpoint as a defense-in-depth measure
- Automatic reconnection and retry logic handle transient failures without data loss
- Partial failure tracking ensures that only failed records are retried, avoiding duplicates from successful portions of a batch
