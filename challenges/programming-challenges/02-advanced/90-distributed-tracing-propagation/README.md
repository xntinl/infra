# 90. Distributed Tracing Propagation

<!--
difficulty: advanced
category: observability-and-monitoring
languages: [go]
concepts: [distributed-tracing, w3c-trace-context, context-propagation, http-middleware, span-lifecycle, trace-reconstruction]
estimated_time: 6-8 hours
bloom_level: evaluate
prerequisites: [go-basics, http-middleware, context-package, goroutines, channels, json-encoding, net-http]
-->

## Languages

- Go (1.22+)

## Prerequisites

- `context.Context` for carrying trace state through call chains
- HTTP middleware patterns (`http.Handler` wrapping)
- `net/http` client and server with header manipulation
- Goroutine management and channel-based collection
- Understanding of distributed systems: request flows across service boundaries
- Familiarity with the concept of trace context (trace ID, span ID, parent span ID)

## Learning Objectives

- **Evaluate** the correctness of trace context propagation across service boundaries by detecting broken or orphaned traces
- **Implement** the W3C Trace Context specification (`traceparent` and `tracestate` headers) for HTTP-based propagation
- **Design** a span lifecycle manager that handles creation, timing, attribute attachment, status recording, and export
- **Analyze** broken trace scenarios: missing headers, malformed context, clock skew, and out-of-order span arrival
- **Create** HTTP middleware for automatic context injection (client) and extraction (server) that works with any `http.Handler`

## The Challenge

When a user request enters your system, it may traverse an API gateway, an authentication service, a business logic service, and a database proxy before returning a response. Without distributed tracing, debugging latency or errors requires correlating logs from each service by timestamp -- a fragile and error-prone process. Distributed tracing solves this by assigning a unique trace ID to the request and propagating it through every service boundary, creating a connected graph of spans that reveals exactly what happened.

The W3C Trace Context specification standardizes how trace context is propagated over HTTP. The `traceparent` header carries the trace ID, parent span ID, and trace flags. The `tracestate` header carries vendor-specific key-value pairs. Every service in the chain must extract the incoming context, create a child span, and inject the updated context into outgoing requests. If any service fails to propagate correctly, the trace is broken: spans become orphans that cannot be stitched into the trace tree.

Your task is to implement a complete distributed tracing propagation library in Go. This includes: a span model with timing and attributes, a trace context propagator implementing the W3C spec, HTTP middleware for automatic extraction and injection, a span collector that receives completed spans, a trace tree reconstructor that detects broken or incomplete traces, and an exporter that outputs traces in a format consumable by backends.

The implementation does not need to support gRPC, messaging, or any transport other than HTTP. But it must handle the hard cases: malformed headers, missing context (start a new trace), sampling decisions, clock skew detection, and concurrent span creation within a single service.

## Key Concepts

**W3C Trace Context.** The `traceparent` header has the format: `{version}-{trace-id}-{parent-id}-{trace-flags}`. Version is `00`. Trace ID is 32 hex characters (16 bytes). Parent ID is 16 hex characters (8 bytes). Trace flags is 2 hex characters (1 byte), where bit 0 indicates the trace is sampled. Example: `00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01`.

**tracestate header.** A comma-separated list of vendor-specific key-value pairs: `congo=t61rcWkgMzE,rojo=00f067aa0ba902b7`. Your propagator must preserve existing entries and can prepend your own. The total must not exceed 32 entries.

**Span lifecycle.** A span is created (start time recorded), has attributes and events added during execution, records a status (OK, Error), and ends (end time recorded). After ending, the span is immutable and exported to the collector.

**Sampling.** Not every trace should be recorded. A sampling decision is made at the trace root (encoded in trace flags) and must be respected by all downstream services. Your propagator must honor the sampling flag and not create child spans for unsampled traces. Head-based sampling (deciding at the root) is simpler but means you cannot retroactively decide to sample a trace that turns out to have errors.

**Broken traces.** A trace is broken when spans reference parent IDs that do not exist in the collected data. This happens when a service fails to propagate context, when spans are lost in transit, or when a service is not instrumented. Your trace reconstructor must detect these gaps and report them explicitly rather than silently ignoring orphan spans.

## Requirements

1. Implement the `traceparent` header format: parsing, validation, and generation per the W3C Trace Context spec
2. Implement `tracestate` header: parsing, validation, prepending vendor entries, and enforcing the 32-entry limit
3. Span model: trace ID, span ID, parent span ID, operation name, service name, start/end time, status (OK/ERROR/UNSET), attributes (string key-value), events (timestamped messages)
4. Span lifecycle: `StartSpan(ctx, name) -> (Span, ctx)`, `span.SetAttribute(k, v)`, `span.AddEvent(name)`, `span.SetStatus(code, message)`, `span.End()`
5. Context integration: active span stored in `context.Context`, child spans automatically link to parent
6. Server middleware: `func TracingMiddleware(next http.Handler) http.Handler` -- extracts `traceparent`/`tracestate`, creates a server span, injects span into context
7. Client middleware: `func TracingTransport(base http.RoundTripper) http.RoundTripper` -- extracts span from context, injects `traceparent`/`tracestate` into outgoing request headers
8. If no incoming trace context exists, start a new root trace with a fresh trace ID
9. Sampling: respect the trace flags sampled bit; provide a configurable sampler (always-on, always-off, probability-based)
10. Span collector: receives completed spans via channel, stores them in memory, provides query by trace ID
11. Trace tree reconstruction: given a trace ID, build the span tree and report: complete trace, broken trace (orphan spans with unknown parents), missing spans (referenced parent IDs with no matching span)
12. Exporter: serialize collected traces to JSON in a format compatible with OTLP

## Hints

Hints for this challenge are intentionally concise. The W3C Trace Context specification is your primary reference.

1. Generate trace IDs with `crypto/rand` (16 bytes for trace ID, 8 bytes for span ID). Convert to hex with `hex.EncodeToString`. Never use `math/rand` for IDs in production -- collisions break trace correlation. In your tests, you may use deterministic IDs for reproducibility.

2. Store the active span in `context.Context` using a private key type to avoid collisions: `type spanContextKey struct{}`. Extract with `SpanFromContext(ctx) *Span`. This is how the client middleware finds the current span to inject into outgoing headers.

3. The server middleware should handle all error cases gracefully: missing `traceparent` header (start new trace), malformed header (start new trace and log warning), valid header (create child span). Never fail the request because of a tracing error -- tracing is non-critical.

4. For trace tree reconstruction, build a `map[spanID]*SpanNode` and then wire parent-child links. Orphan detection: any span whose parent ID is not in the map AND is not the root span is an orphan. Missing span detection: collect all referenced parent IDs, subtract all known span IDs -- the remainder are missing.

5. Clock skew detection: if a child span's start time is before its parent span's start time, flag it. This does not mean the trace is broken, but it indicates clock synchronization issues between services.

6. For the probability-based sampler, hash the trace ID and compare against the probability threshold. This ensures the same trace ID always gets the same sampling decision across all services, which is critical for consistency.

## Acceptance Criteria

- [ ] `traceparent` header is correctly parsed and generated per W3C spec (version 00, valid hex lengths, valid flags)
- [ ] `tracestate` header preserves existing entries and respects the 32-entry limit
- [ ] Server middleware creates a child span linked to the incoming trace context
- [ ] Server middleware starts a new trace when no `traceparent` header is present
- [ ] Client middleware injects `traceparent` and `tracestate` into outgoing HTTP requests
- [ ] Span lifecycle works: create, set attributes, add events, set status, end -- immutable after end
- [ ] Context propagation: child spans within a service automatically link to parent spans via context
- [ ] Sampling decisions are respected: unsampled traces produce no spans
- [ ] Probability sampler produces consistent decisions for the same trace ID
- [ ] Trace tree reconstruction correctly identifies: complete traces, orphan spans, missing spans
- [ ] Clock skew detection flags child-before-parent timing anomalies
- [ ] All tests pass with `-race` flag
- [ ] At least 10 test scenarios: normal propagation, missing headers, malformed headers, sampling, multi-service chain (3+ hops), concurrent spans, broken trace detection, clock skew, tracestate round-trip, exporter output validation

## Going Further

- **gRPC propagation**: Extend context injection/extraction to gRPC metadata using interceptors. The gRPC ecosystem uses different header conventions (binary metadata vs HTTP text headers), requiring a separate propagator implementation.
- **Baggage propagation**: Implement the W3C Baggage spec for carrying application-specific key-value pairs (tenant ID, feature flags) across service boundaries without adding them to spans.
- **Tail-based sampling**: Instead of deciding at the root, collect all spans and decide after the trace is complete. Sample all error traces and a percentage of healthy ones. This requires buffering complete traces before export.
- **Span links**: Support linking spans across trace boundaries (e.g., a consumer span linked to the producer span that created the message). Links enable tracing across asynchronous boundaries like message queues.
- **Trace completeness timeout**: In production, spans arrive out of order and some may never arrive. Implement a configurable timeout after which a trace is declared complete (or broken) and exported regardless.

## Starting Points

Study these implementations for reference:

- **OpenTelemetry Go SDK** (`opentelemetry-go`): The canonical implementation. Study `propagation/trace_context.go` for the W3C propagator and `sdk/trace/span.go` for the span lifecycle. The SDK cleanly separates API (interfaces) from SDK (implementation).

- **OpenTelemetry Collector** (`opentelemetry-collector`): The production pipeline for receiving, processing, and exporting traces. Study the receiver and exporter interfaces to understand how spans flow through the system.

- **Zipkin Go** (`openzipkin/zipkin-go`): An alternative tracing library with B3 propagation format. Comparing B3 and W3C propagation implementations highlights the design choices each format makes.

## Research Resources

- [W3C Trace Context Specification](https://www.w3.org/TR/trace-context/) -- the authoritative standard for `traceparent` and `tracestate`
- [W3C Trace Context Level 2](https://www.w3.org/TR/trace-context-2/) -- extensions including tracestate mutations
- [OpenTelemetry Go SDK](https://github.com/open-telemetry/opentelemetry-go) -- production implementation; study `propagation/` and `trace/` packages
- [OpenTelemetry Specification: Context Propagation](https://opentelemetry.io/docs/specs/otel/context/api-propagators/) -- how propagators integrate with transport layers
- [Dapper: Google's Distributed Tracing Paper](https://research.google/pubs/pub36356/) -- the foundational paper on distributed tracing at scale
- [Jaeger Client Libraries](https://www.jaegertracing.io/docs/client-libraries/) -- study propagation format differences (Jaeger vs B3 vs W3C)
