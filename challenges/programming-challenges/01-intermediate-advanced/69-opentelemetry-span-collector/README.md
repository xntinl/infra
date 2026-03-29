# 69. OpenTelemetry Span Collector

<!--
difficulty: intermediate-advanced
category: observability-and-monitoring
languages: [go]
concepts: [opentelemetry, tracing, spans, otlp, http-api, query-engine, latency-histograms]
estimated_time: 4-5 hours
bloom_level: apply, analyze
prerequisites: [go-basics, http-handlers, json-parsing, concurrency, time-handling, data-structures]
-->

## Languages

- Go (1.22+)

## Prerequisites

- HTTP server with `net/http` and JSON request/response handling
- `encoding/json` for OTLP JSON format parsing
- `sync.RWMutex` for concurrent read/write access to the span store
- Understanding of distributed tracing concepts: traces, spans, parent-child relationships
- Basic histogram construction (bucket counting)

## Learning Objectives

- **Implement** an HTTP endpoint that ingests trace spans in a simplified OTLP JSON format
- **Design** an in-memory store that indexes spans by trace ID, service name, and time range for efficient querying
- **Apply** parent-child span reconstruction to build complete trace trees from flat span lists
- **Analyze** latency distributions by computing histograms from span durations
- **Create** an ASCII waterfall visualization that renders trace timelines in the terminal

## The Challenge

Distributed tracing tells you what happened during a request as it traversed multiple services. Each unit of work is a span, and spans are linked into traces through parent-child relationships. OpenTelemetry is the industry standard for producing and collecting these traces. Tools like Jaeger and Tempo receive spans, store them, and let you query and visualize them. Understanding how a trace backend works -- ingestion, indexing, querying, visualization -- makes you a far more effective debugger of distributed systems.

Your task is to build a minimal span collector service in Go. It exposes an HTTP endpoint that receives spans in a simplified OTLP JSON format. Spans are stored in memory and indexed for efficient lookup by trace ID, service name, and time range. Given a trace ID, the collector reconstructs the full span tree (parent-child hierarchy) and can render it as an ASCII waterfall diagram showing the timeline of each span relative to the trace root.

Additionally, the collector computes latency histograms across all collected spans: given a service name and operation, it returns the distribution of span durations in configurable buckets (p50, p90, p99).

## Requirements

1. HTTP endpoint `POST /v1/traces` accepts spans in simplified OTLP JSON format (array of span objects)
2. Each span has: `traceId`, `spanId`, `parentSpanId` (empty for root), `operationName`, `serviceName`, `startTimeUnixNano`, `endTimeUnixNano`, `status` (OK/ERROR), `attributes` (key-value map)
3. In-memory store with indexes: by trace ID (primary), by service name, and by start time (for range queries)
4. `GET /v1/traces/:traceId` returns all spans for a trace, reconstructed as a tree (each span includes its children)
5. `GET /v1/traces/:traceId/waterfall` returns an ASCII waterfall visualization of the trace timeline
6. `GET /v1/services/:name/latency?operation=X` returns a latency histogram for the given service and operation
7. `GET /v1/services` returns a list of all known service names
8. Thread-safe: concurrent ingestion and querying must not corrupt data
9. Span validation: reject spans with missing required fields, duplicate span IDs within a trace, or invalid timestamps
10. Support a maximum store size (span count) with oldest-first eviction when the limit is reached

## Hints

<details>
<summary>Hint 1: Span store with multiple indexes</summary>

```go
type SpanStore struct {
    mu       sync.RWMutex
    byTrace  map[string][]*Span          // traceId -> spans
    byService map[string][]*Span         // serviceName -> spans
    allSpans []*Span                     // ordered by ingestion for eviction
    maxSpans int
}
```

Write operations lock with `mu.Lock()`, reads with `mu.RLock()`. When `len(allSpans) > maxSpans`, evict the oldest entries and remove them from all indexes.
</details>

<details>
<summary>Hint 2: Tree reconstruction from flat spans</summary>

```go
func buildTree(spans []*Span) *SpanNode {
    nodes := make(map[string]*SpanNode)
    for _, s := range spans {
        nodes[s.SpanID] = &SpanNode{Span: s}
    }
    var root *SpanNode
    for _, s := range spans {
        node := nodes[s.SpanID]
        if s.ParentSpanID == "" {
            root = node
        } else if parent, ok := nodes[s.ParentSpanID]; ok {
            parent.Children = append(parent.Children, node)
        }
    }
    return root
}
```
</details>

<details>
<summary>Hint 3: ASCII waterfall rendering</summary>

Calculate each span's offset and duration relative to the trace root's start time. Scale to terminal width (e.g., 80 chars). For each span, print the operation name and a bar:

```
[root-api] |========================================|  200ms
  [auth]   |  ====|                                    45ms
  [db]     |       =============|                     120ms
    [idx]  |        ====|                              35ms
```

Use the tree depth for indentation and `startTime - rootStart` for the bar's left offset.
</details>

<details>
<summary>Hint 4: Latency histogram computation</summary>

```go
type Histogram struct {
    Buckets []HistogramBucket
    Count   int
    Sum     time.Duration
    Min     time.Duration
    Max     time.Duration
}

func computeHistogram(durations []time.Duration, boundaries []float64) Histogram {
    // boundaries in milliseconds: [1, 5, 10, 25, 50, 100, 250, 500, 1000]
    // count how many durations fall into each bucket
}
```
</details>

## Acceptance Criteria

- [ ] `POST /v1/traces` accepts valid spans and returns 200; rejects invalid spans with 400 and error details
- [ ] `GET /v1/traces/:traceId` returns all spans for the trace, structured as a parent-child tree
- [ ] `GET /v1/traces/:traceId/waterfall` returns a readable ASCII waterfall with correct relative timing
- [ ] `GET /v1/services/:name/latency?operation=X` returns a histogram with bucket counts, p50, p90, p99
- [ ] `GET /v1/services` returns all known service names
- [ ] Concurrent ingestion (50 goroutines posting spans) produces no data races (`go test -race`)
- [ ] Store eviction removes oldest spans when the limit is exceeded and updates all indexes
- [ ] Duplicate span IDs within a trace are rejected
- [ ] Spans with invalid timestamps (end before start) are rejected
- [ ] Waterfall visualization correctly reflects parent-child nesting and relative timing

## Research Resources

- [OpenTelemetry Specification: Tracing](https://opentelemetry.io/docs/specs/otel/trace/) -- the authoritative specification for spans, traces, and context
- [OTLP JSON Format](https://opentelemetry.io/docs/specs/otlp/) -- the protocol for transmitting telemetry data
- [Jaeger Architecture](https://www.jaegertracing.io/docs/architecture/) -- how a production trace backend stores and queries spans
- [Grafana Tempo](https://grafana.com/docs/tempo/latest/) -- trace backend optimized for storage efficiency; study its query model
- [Go net/http ServeMux](https://pkg.go.dev/net/http#ServeMux) -- Go 1.22 enhanced routing with path parameters
- [HDR Histogram](https://hdrhistogram.github.io/HdrHistogram/) -- high dynamic range histogram for latency measurement
