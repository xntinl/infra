# 6. Build a Distributed Tracing System Compatible with OpenTelemetry

**Difficulty**: Insane

## Prerequisites
- Mastered: All Elixir/OTP intermediate and advanced concepts (GenServer, :telemetry, process dictionary, ETS, binary encoding)
- Mastered: Observability principles — traces vs metrics vs logs, sampling theory, structured telemetry
- Familiarity with: OpenTelemetry specification, Jaeger architecture, Thrift binary encoding, W3C TraceContext propagation header format
- Reading: The Dapper paper (Sigelman et al., 2010), the OpenTelemetry specification (OTEP docs), the Jaeger Thrift IDL definitions

## Problem Statement

Build a distributed tracing system in Elixir/OTP that instruments Elixir applications, collects spans from across a BEAM cluster, samples intelligently, stores spans in memory, and exports them in Jaeger Thrift format. The system must be transparent to application code: a single `use MyTracer` macro should add tracing to any GenServer without modifying business logic.

Your system must implement:
1. A span API with high-resolution timestamps: `start_span(name, attributes)` starts a new span under the current trace context; `finish_span(span)` records duration and emits the span to the local collector; spans carry a 128-bit trace ID and a 64-bit span ID
2. Context propagation through GenServer calls: when process A calls a GenServer B, the trace context (trace_id + parent_span_id) must automatically flow into B's `handle_call` without the developer manually passing it — use the process dictionary as the implicit context carrier
3. A macro-based auto-instrumentation layer: `use MyTracer.GenServer` wraps `handle_call`, `handle_cast`, and `handle_info` to automatically start/finish spans, tag them with the message pattern, and propagate context from the caller
4. Two sampling strategies, composable: head-based sampling (sample X% of incoming traces at the entry point, propagate the sampling decision downstream) and tail-based sampling (buffer all spans for a trace; after the root span finishes, inspect the full trace and always keep traces that contain errors or exceed a latency threshold)
5. A collector process per node that receives spans via a local ETS-backed buffer, flushes them to a central aggregator on a configurable interval, and handles backpressure (drop oldest when buffer exceeds MAX_SIZE)
6. An in-memory storage engine on the aggregator that can hold 1,000,000 spans, supports point lookup by trace_id, and supports range queries by timestamp
7. A Jaeger Thrift exporter: serialize collected spans into Jaeger's Thrift binary format and POST them to the Jaeger collector HTTP endpoint (or emit to stdout if Jaeger is not available)
8. A text-based dashboard that shows: top 10 slowest traces in the last 60 seconds, error rate per service, span count per operation, and a sample trace rendered as an ASCII tree

## Acceptance Criteria

- [ ] **Span creation and finish**: `start_span("db.query", %{table: "users"})` returns a span struct with non-zero timestamps and a valid 128-bit trace ID; `finish_span/1` records a `duration_us` field with microsecond precision
- [ ] **Context propagation — same process**: A span started in process A is visible as the parent when a child span is started in the same process; child span's `parent_span_id` equals the parent's `span_id`
- [ ] **Context propagation — across GenServer call**: Process A holds an active span and calls GenServer B via `GenServer.call`; within B's `handle_call`, a new span is automatically created with the correct `trace_id` and `parent_span_id` referencing A's span
- [ ] **Auto-instrumentation**: Add `use MyTracer.GenServer` to a GenServer module; without any other changes, every `handle_call` invocation is automatically wrapped in a span; verify via the dashboard that spans appear with operation names derived from the message pattern
- [ ] **Head-based sampling**: Configure 10% sampling rate; send 10,000 distinct traces through the system; confirm that approximately 1,000 (±5%) are retained and 9,000 are dropped; confirm the sampling decision is consistent across all spans in a given trace
- [ ] **Tail-based sampling**: Configure tail-based sampling to always keep error traces; inject 100 traces where the root span carries an `error: true` attribute; send 900 normal traces; confirm all 100 error traces are retained regardless of head-sample decision
- [ ] **1M span storage**: Insert 1,000,000 spans into the in-memory store; perform 10,000 point lookups by `trace_id`; confirm median lookup latency is under 1ms and no lookup returns a wrong or missing result
- [ ] **Jaeger export**: After inserting 100 spans, invoke the exporter and confirm the output is valid Jaeger Thrift binary (parse it with the Jaeger Thrift IDL); if Jaeger is running locally, confirm traces appear in the Jaeger UI
- [ ] **Backpressure**: Flood the collector with 100,000 spans/second; confirm the buffer never exceeds MAX_SIZE; confirm the oldest spans are dropped (not the newest); confirm the system remains stable and does not crash
- [ ] **Text dashboard**: The dashboard must refresh every 10 seconds and correctly show the 10 slowest traces (ranked by root span duration), error rate per service, and a correct ASCII tree for a selected trace

## What You Will Learn
- Why distributed tracing requires an implicit context carrier (process dictionary in Elixir, ThreadLocal in Java, context.Context in Go) rather than explicit parameter threading
- The difference between head-based and tail-based sampling and why tail-based sampling is fundamentally harder to implement — it requires buffering the full trace before making a sampling decision
- How to implement a macro in Elixir that wraps existing callbacks transparently using `defoverridable` and `__using__/1`
- How Thrift binary encoding works — field IDs, type tags, zigzag encoding for integers — and why it is more compact than JSON for high-volume telemetry
- Why high-resolution timestamps (`System.monotonic_time/1`) are necessary for span duration and why wall-clock time is unreliable for ordering events across processes
- How to implement a bounded buffer with O(1) oldest-drop eviction using a ring buffer or double-ended queue in ETS
- The architectural split between a per-node local agent (low overhead, always-on) and a central aggregator (stateful, queryable) — the same split used by Datadog Agent, Jaeger Agent, and OpenTelemetry Collector

## Hints

This exercise is intentionally sparse. You are expected to:
- Read the Dapper paper fully — pay special attention to section 3 (distributed tracing infrastructure) and section 4 (instrumentation) before designing your span API
- The W3C TraceContext header spec defines how trace context crosses HTTP boundaries — your GenServer propagation is the intra-process equivalent; the same semantics apply
- Auto-instrumentation via `use` macros requires you to understand Elixir's `__using__/1`, `defoverridable`, and `quote/unquote` deeply — experiment with a minimal example before building the full system
- Tail-based sampling requires a trace buffer that expires after a timeout — if the root span never finishes (bug or very long trace), you must eventually flush or discard
- Study the Jaeger Thrift IDL file (`jaeger.thrift`) before writing any serialization code — the schema defines which fields are required, their type IDs, and the expected field ordering

## Reference Material (Research Required)
- Sigelman, B. et al. (2010). *Dapper, a Large-Scale Distributed Systems Tracing Infrastructure* (Google Technical Report) — the paper that defined modern distributed tracing
- OpenTelemetry Specification — https://opentelemetry.io/docs/specs/ — focus on the Trace API, SDK, and Data Model sections; do not read introductory blog posts
- W3C TraceContext specification — https://www.w3.org/TR/trace-context/ — the standard for cross-service context propagation
- Jaeger Thrift IDL — `model/proto/api_v2/jaeger.proto` and `jaeger-idl/thrift/jaeger.thrift` in the Jaeger GitHub repository
- Thrift binary protocol specification — Apache Thrift documentation, Binary Protocol Encoding section

## Difficulty Rating
★★★★★★

## Estimated Time
4–6 weeks for an experienced Elixir developer with observability and binary protocol background
