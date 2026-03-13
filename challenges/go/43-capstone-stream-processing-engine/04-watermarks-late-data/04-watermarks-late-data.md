# 4. Watermarks and Late Data

<!--
difficulty: insane
concepts: [watermarks, late-data, allowed-lateness, event-time-progress, watermark-propagation, side-outputs, retractions, out-of-order-processing]
tools: [go]
estimated_time: 3h
bloom_level: create
prerequisites: [43-capstone-stream-processing-engine/03-windowing, 14-select-and-context, 15-sync-primitives]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-03 (sources, operators, windowing) or equivalent stream processing experience
- Understanding of event time versus processing time and why records arrive out of order in distributed systems

## Learning Objectives

- **Design** a watermark tracking system that measures event-time progress across a multi-source stream processing pipeline
- **Create** a late data handling mechanism with configurable allowed lateness, side outputs for dropped records, and accumulating/retracting window modes
- **Evaluate** the trade-off between watermark aggressiveness (lower latency) and conservatism (fewer late records) and its impact on result correctness

## The Challenge

In a perfect world, events arrive in order and windows close at precisely the right moment. In reality, events arrive out of order due to network delays, clock skew, and buffering. A watermark is the engine's assertion that "no more events with a timestamp earlier than W will arrive." When a watermark passes a window's end time, the window can be safely closed and its results emitted. But watermarks are heuristic -- late data that arrives after the watermark has passed the window boundary must be handled explicitly.

You will implement a watermark tracking system that observes event timestamps across all sources and computes a global watermark as the minimum event-time progress across all input partitions. You will then integrate watermarks with the windowing system: windows fire when the watermark passes their end time, and records arriving after the watermark has passed their window are classified as late data.

Late data handling has three strategies: drop (discard with a metric), allow (accept into the already-fired window and emit an updated result), or side-output (redirect to a separate channel for special handling). For the allow strategy, you must support both accumulating mode (the updated result includes all records seen so far) and accumulating-and-retracting mode (emit a retraction of the previous result followed by the new result, so downstream aggregations remain correct).

## Requirements

1. Define a `Watermark` type as a `time.Time` representing the assertion that no records with earlier timestamps will arrive from a given source
2. Implement per-source watermark tracking: each source maintains its own watermark based on the maximum observed timestamp minus a configurable out-of-orderness bound
3. Implement global watermark computation as the minimum watermark across all active sources -- the global watermark advances only when the slowest source advances
4. Implement watermark propagation through operators: each operator outputs a watermark that is at least as large as the minimum of its input watermarks
5. Integrate watermarks with the windowing system: replace the event-time trigger's timestamp comparison with watermark comparison -- a window fires when the global watermark passes the window's end time
6. Implement an allowed lateness parameter per window assigner: records arriving after the watermark has passed the window end but within the allowed lateness period are accepted into the window
7. When a late record is accepted into an already-fired window, re-fire the window with the updated aggregation (accumulating mode)
8. Implement accumulating-and-retracting mode: when a window re-fires due to late data, emit a retraction record (negation of the previous result) followed by the new accumulated result
9. Implement a side output for late records that arrive after the allowed lateness period has expired: redirect them to a separate output channel with metadata indicating the intended window
10. Track watermark-related metrics: current watermark per source, global watermark, late records accepted, late records dropped, and retractions emitted
11. Implement idle source detection: if a source has not emitted any records for a configurable duration, exclude it from the global watermark minimum to prevent watermark stalling

## Hints

- Store each source's watermark as an `atomic.Int64` holding `time.Time.UnixMilli()` for lock-free reads from the global watermark computation goroutine
- Compute the global watermark periodically (e.g., every 100ms) in a dedicated goroutine rather than on every record arrival to reduce computation overhead
- For allowed lateness, keep window state in memory for `window.End + allowedLateness` duration before purging -- use a cleanup timer registered when the window first fires
- For retractions, define a `RecordType` field in `Record.Metadata`: `"normal"`, `"retraction"`, or `"update"` so downstream operators can distinguish them
- For idle source detection, maintain a `lastActivity` timestamp per source and exclude sources idle longer than the threshold from the minimum watermark calculation
- For accumulating mode, simply re-run the reduce function over all records in the window -- this requires keeping the window's raw records in memory until the allowed lateness period expires
- The global watermark should never go backwards -- enforce monotonicity by taking `max(currentGlobalWatermark, newComputedWatermark)`

## Success Criteria

1. Per-source watermarks advance correctly based on observed timestamps and the configured out-of-orderness bound
2. The global watermark is the minimum of all active source watermarks and never decreases
3. Windows fire when the global watermark passes their end time, not based on record timestamps alone
4. Late records within the allowed lateness period are accepted and cause window re-firing
5. Retractions are correctly emitted in accumulating-and-retracting mode
6. Late records beyond the allowed lateness period are redirected to the side output channel
7. Idle sources are excluded from global watermark computation after the configured idle threshold
8. Watermark metrics accurately reflect the current state of all sources and the global watermark
9. All tests pass with the `-race` flag enabled

## Research Resources

- [Streaming 102 (Tyler Akidau)](https://www.oreilly.com/radar/the-world-beyond-batch-streaming-102/) -- deep dive into watermarks, triggers, and accumulation modes
- [Apache Flink Event Time and Watermarks](https://nightlies.apache.org/flink/flink-docs-stable/docs/concepts/time/) -- reference design for watermark generation and propagation
- [Google Dataflow Model paper](https://research.google/pubs/the-dataflow-model-a-practical-approach-to-balancing-correctness-latency-and-cost-in-massive-scale-unbounded-out-of-order-data-processing/) -- formal treatment of watermarks, triggers, and accumulation
- [Apache Flink late data handling](https://nightlies.apache.org/flink/flink-docs-stable/docs/dev/datastream/operators/windows/#allowed-lateness) -- allowed lateness and side outputs
- [Apache Beam programming model](https://beam.apache.org/documentation/programming-guide/#watermarks-and-late-data) -- watermarks and late data in the Beam model

## What's Next

Continue to [Checkpointing](../05-checkpointing/05-checkpointing.md) where you will implement distributed snapshots for fault-tolerant exactly-once processing.

## Summary

- Watermarks measure event-time progress and tell the engine when it is safe to close a window
- The global watermark is the minimum of all source watermarks, advancing only as fast as the slowest source
- Allowed lateness extends the window's lifetime beyond the watermark, accepting out-of-order records at the cost of memory
- Accumulating mode re-fires windows with updated results when late data arrives
- Retracting mode emits corrections that allow downstream aggregations to remain accurate after updates
- Idle source detection prevents a single stalled source from freezing the entire pipeline's event-time progress
