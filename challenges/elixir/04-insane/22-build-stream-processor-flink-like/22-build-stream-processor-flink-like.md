# 22. Build a Stream Processor (Flink-like)

**Difficulty**: Insane

---

## Prerequisites

- Elixir GenStage and Flow fundamentals
- OTP GenServer and Supervisor
- Understanding of distributed systems concepts (fault tolerance, state machines)
- Familiarity with data stream concepts (event time vs. processing time)
- ETS for in-memory state management
- Binary serialization (`:erlang.term_to_binary/1` or similar)

---

## Problem Statement

Build a stream processing engine that provides exactly-once semantics, stateful computation, and windowed aggregations. The system must:

1. Accept a job definition expressed as a DAG (Directed Acyclic Graph) of typed operators
2. Execute the DAG with configurable parallelism, partitioning state by key across parallel workers
3. Implement tumbling, sliding, and session windows with correct event-time semantics
4. Handle late-arriving data using watermarks and a configurable grace period before closing windows
5. Checkpoint the full state of all operators periodically so the job can resume from the last checkpoint after a failure, with exactly-once guarantees
6. Apply back-pressure from slow downstream operators up through the pipeline to prevent unbounded buffering
7. Process at least 1 million events per second through a five-operator pipeline on a single machine

---

## Acceptance Criteria

- [ ] Windowing: implement tumbling (fixed, non-overlapping), sliding (overlapping with step), and session (gap-triggered) windows; each window fires when its watermark closes it
- [ ] Watermarks: each source emits periodic watermarks; operators propagate the minimum watermark across all parallel instances; data arriving after `watermark - grace_period` is dropped with a metric
- [ ] Stateful processing: operators that key by a field maintain independent state per key; state is isolated between keys and between parallel instances
- [ ] Checkpointing: a coordinator triggers a distributed snapshot (Chandy-Lamport or barrier-based); all operator states are serialized and stored atomically
- [ ] Exactly-once: after restoring from a checkpoint, replayed events do not cause duplicate output; output operators use idempotent writes or transactional commits
- [ ] Back-pressure: when a downstream stage's buffer exceeds a high-water mark, upstream stages block on `send`; no unbounded memory growth under sustained overload
- [ ] Parallelism: each operator in the DAG has a configurable parallelism factor N; events for the same key always route to the same parallel instance
- [ ] Job graph: the DAG is expressed as a data structure (not hardcoded); edges carry type information; the runtime validates connectivity before starting
- [ ] Benchmark: a pipeline of source → filter → map → keyBy → window-aggregate → sink sustains 1M events/second; P99 end-to-end latency under 200ms

---

## What You Will Learn

- Implementing the Dataflow / Flink execution model in a functional language
- Chandy-Lamport distributed snapshots for fault-tolerant state
- Event-time processing and watermark propagation
- Efficient state partitioning and routing in parallel pipelines
- Back-pressure protocols (credit-based flow control)
- DAG validation and topology planning
- Performance tuning for high-throughput Elixir pipelines

---

## Hints

- Read the Dataflow Model paper by Akidau et al. before writing a single line of code — the concepts directly map to your implementation
- Study how Apache Flink uses checkpoint barriers injected into the event stream
- Investigate how GenStage's demand-driven model implements back-pressure natively
- Research how to express a DAG as an adjacency list and topologically sort it for execution order
- Think carefully about what "exactly-once" requires at the output sink, not just in the operator state
- Session windows require tracking per-key gap timers — consider how to do this without one timer process per key

---

## Reference Material

- "The Dataflow Model" — Akidau et al., VLDB 2015
- "MillWheel: Fault-Tolerant Stream Processing at Internet Scale" — Google, VLDB 2013
- "Lightweight Asynchronous Snapshots for Distributed Dataflows" — Carbone et al. (Flink checkpoint paper)
- Apache Flink architecture documentation
- GenStage documentation and back-pressure design

---

## Difficulty Rating ★★★★★★

Distributed checkpointing combined with exactly-once semantics and high-throughput requirements places this at the frontier of what can be built in Elixir.

---

## Estimated Time

80–120 hours
