# 3. Windowing

<!--
difficulty: insane
concepts: [windowing, tumbling-windows, sliding-windows, session-windows, event-time, processing-time, window-triggers, window-assigners, window-aggregation]
tools: [go]
estimated_time: 3h
bloom_level: create
prerequisites: [43-capstone-stream-processing-engine/02-operators-map-filter-flatmap, 15-sync-primitives, 14-select-and-context]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-02 (source connectors, operators) or equivalent stream processing pipeline experience
- Completed Sections 14-15 (concurrency and sync primitives) or equivalent experience
- Understanding of the difference between event time and processing time in stream processing

## Learning Objectives

- **Design** a windowing framework supporting tumbling, sliding, and session windows with configurable window assigners and triggers
- **Create** window aggregation operators that accumulate records into windows and emit results when windows close
- **Evaluate** the trade-offs between event-time and processing-time windowing and their impact on result correctness and latency

## The Challenge

Unbounded streams have no natural boundaries, but most useful computations require aggregating records over finite intervals. Windowing is the mechanism that imposes structure on infinite streams by grouping records into finite, overlapping, or gap-defined buckets. A five-minute tumbling window counts events in non-overlapping five-minute intervals. A sliding window of five minutes with a one-minute slide produces a result every minute covering the last five minutes. A session window groups events by activity, closing the window after a configurable gap of inactivity.

You will build a windowing framework with three window types, each driven by configurable window assigners. Each window maintains an accumulator that aggregates records using a user-provided reduce function. Windows are closed by triggers -- a trigger fires when a condition is met (time expires, record count reached, or watermark advances past the window boundary). When a trigger fires, the window emits its aggregated result downstream and optionally purges its state.

The real complexity lives in event-time windowing. Records may arrive out of order -- a record with timestamp 10:04:59 may arrive after a record with timestamp 10:05:01. The window for 10:00-10:05 must decide whether to include the late record or discard it. This exercise builds the windowing mechanics; watermarks and late data handling are covered in exercise 04.

## Requirements

1. Define a `Window` struct containing: window start time, window end time, a keyed accumulator (aggregation state per key), and trigger state
2. Define a `WindowAssigner` interface with `AssignWindows(record Record) []Window` that returns the windows a record belongs to (one for tumbling, potentially multiple for sliding)
3. Implement `TumblingWindowAssigner` with a configurable fixed window size (e.g., 5 minutes) that assigns each record to exactly one non-overlapping window based on its timestamp
4. Implement `SlidingWindowAssigner` with configurable window size and slide interval that assigns each record to all overlapping windows it falls within
5. Implement `SessionWindowAssigner` with a configurable inactivity gap that groups records into sessions per key, extending the window on each arrival and closing it after the gap expires
6. Define a `Trigger` interface with `OnRecord(record Record, window Window) TriggerResult` and `OnTimer(time time.Time, window Window) TriggerResult` where `TriggerResult` is `Fire`, `Continue`, or `FireAndPurge`
7. Implement `EventTimeTrigger` that fires when the current event time (approximated by the latest record timestamp seen) passes the window's end time
8. Implement `CountTrigger` that fires when the number of records in the window reaches a configurable threshold
9. Implement `ProcessingTimeTrigger` that fires when the wall-clock time passes the window's end time, using `time.AfterFunc` for timer registration
10. Implement a `WindowOperator` that integrates window assigners, triggers, and a user-provided `ReduceFunc(accumulator Record, incoming Record) Record` to aggregate records within each window
11. Support keyed windows: records are grouped by a user-provided key extraction function, and each unique key maintains independent window state
12. Emit window results as `Record` values with the window boundaries stored in `Metadata` and the aggregation result in `Value`

## Hints

- For tumbling windows, compute the window start as `timestamp.Truncate(windowSize)` and end as `start.Add(windowSize)`
- For sliding windows, a record with timestamp `t` belongs to windows with start times `t - windowSize + slide, t - windowSize + 2*slide, ...` up to the window containing `t` -- iterate backwards from `t` in `slide` steps
- For session windows, maintain a sorted list of active sessions per key and merge sessions whose gap between the end of one and the start of the next is less than the gap threshold
- Use a `map[WindowKey]WindowState` where `WindowKey` is `{Key string, WindowStart time.Time}` to store per-window accumulation state
- For processing-time triggers, register a timer via `time.AfterFunc` when a window is first created and fire the trigger when the callback executes
- Session window merging is the hardest part: when a new record bridges two existing sessions, merge them into one window combining their accumulators
- Protect window state with a `sync.Mutex` since records may arrive concurrently from parallel upstream operators

## Success Criteria

1. Tumbling windows correctly assign each record to exactly one non-overlapping window
2. Sliding windows correctly assign each record to all overlapping windows
3. Session windows correctly merge sessions when records arrive within the gap threshold
4. The reduce function correctly aggregates all records within a window
5. Event-time triggers fire when the observed event time passes the window boundary
6. Count triggers fire when the configured record count is reached
7. Processing-time triggers fire based on wall-clock time regardless of record timestamps
8. Keyed windows maintain independent state for each key
9. Window results include correct window boundaries in metadata
10. All tests pass with the `-race` flag enabled

## Research Resources

- [Apache Flink Windows](https://nightlies.apache.org/flink/flink-docs-stable/docs/dev/datastream/operators/windows/) -- comprehensive reference for windowing semantics
- [Streaming 101 (Tyler Akidau)](https://www.oreilly.com/radar/the-world-beyond-batch-streaming-101/) -- foundational concepts of event time, processing time, and windowing
- [Google Dataflow Model paper](https://research.google/pubs/the-dataflow-model-a-practical-approach-to-balancing-correctness-latency-and-cost-in-massive-scale-unbounded-out-of-order-data-processing/) -- the academic foundation for windowing in stream processing
- [Session windows in stream processing](https://nightlies.apache.org/flink/flink-docs-stable/docs/dev/datastream/operators/windows/#session-windows) -- session window merge semantics
- [Go time package](https://pkg.go.dev/time) -- time truncation, duration arithmetic, and timer management

## What's Next

Continue to [Watermarks and Late Data](../04-watermarks-late-data/04-watermarks-late-data.md) where you will implement watermark tracking and late data handling to produce correct results from out-of-order event streams.

## Summary

- Tumbling windows provide non-overlapping fixed-size intervals for simple periodic aggregation
- Sliding windows provide overlapping intervals that produce results at a higher frequency than the window size
- Session windows dynamically group records by activity with merge semantics for overlapping sessions
- Triggers control when windows emit results: on event time, processing time, or record count
- Keyed windows enable per-entity aggregation by maintaining independent window state per key
- The window assigner, trigger, and reduce function form a composable framework for arbitrary windowed computation
