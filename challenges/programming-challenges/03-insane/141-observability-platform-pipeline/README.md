# 141. Observability Platform Pipeline

<!--
difficulty: insane
category: distributed-systems
languages: [go, rust]
concepts: [observability, telemetry-pipeline, otlp, metrics-aggregation, log-indexing, trace-correlation, backpressure, time-series-storage]
estimated_time: 40-60 hours
bloom_level: create
prerequisites: [tcp-udp-networking, protobuf-or-binary-serialization, concurrency-advanced, time-series-concepts, inverted-index-basics, tree-data-structures]
-->

## Languages

- Go (1.22+)
- Rust (stable)

## Prerequisites

- TCP and UDP server programming for multiple protocol ingestion
- Binary serialization (protobuf-like framing or custom binary format)
- Advanced concurrency: pipeline stages, fan-in/fan-out, backpressure
- Time-series storage concepts: bucketing, downsampling, retention
- Inverted index data structures for full-text search
- Tree construction for trace assembly from spans

## Learning Objectives

- **Create** a unified observability pipeline that ingests, processes, stores, and queries logs, metrics, and traces through a single system
- **Evaluate** backpressure strategies and their impact on data loss versus latency under load
- **Design** storage backends optimized for each telemetry type: time-bucketed for metrics, inverted index for logs, trace trees for distributed traces
- **Implement** a processing pipeline with configurable stages connected via bounded channels

## The Challenge

Build a unified observability platform that handles the three pillars of observability: logs (events), metrics (measurements), and traces (request flows). The system ingests telemetry from multiple protocols, processes it through a configurable pipeline, stores it in type-specific backends, and exposes a query API.

The platform has four layers: collectors (protocol-specific ingestion), processing pipeline (parse, enrich, sample, aggregate), storage backends (optimized per telemetry type), and query API (unified interface across all telemetry).

Collectors accept data in multiple formats: OTLP-like binary protocol (the OpenTelemetry standard), StatsD (UDP-based metrics), and syslog (RFC 5424 structured logs). Each collector normalizes data into an internal telemetry model before feeding the pipeline.

The processing pipeline is a chain of stages connected by bounded channels. Each stage transforms or filters telemetry data: parsing extracts structured fields, enrichment adds metadata (hostname, service name, environment), sampling reduces volume (head-based or tail-based for traces), and aggregation pre-computes metric rollups. Backpressure propagates from storage through the pipeline to collectors: when a downstream stage is full, upstream stages slow down rather than dropping data silently.

Storage backends are specialized per telemetry type. Metrics use time-bucketed storage with configurable resolution (1s, 10s, 1m, 5m, 1h). Logs use an inverted index mapping terms to log entry IDs for full-text search. Traces are stored as trees of spans, assembled from individual span reports.

## Requirements

1. **Internal Telemetry Model**: define common types for `LogEntry` (timestamp, severity, body, attributes), `MetricPoint` (name, timestamp, value, tags, type: counter/gauge/histogram), `Span` (trace ID, span ID, parent span ID, operation name, start/end time, attributes, status)
2. **Collectors**: implement at least three ingestion endpoints:
   - OTLP-like: TCP server accepting binary-framed telemetry (simplified OTLP, not full protobuf)
   - StatsD: UDP server parsing `metric.name:value|type|#tag:value` format
   - Syslog: TCP server parsing RFC 5424 format (`<priority>version timestamp hostname app-name procid msgid structured-data msg`)
3. **Processing Pipeline**: configurable chain of stages, each running in its own goroutine (Go) or task (Rust), connected by bounded channels. Stages: Parse (extract structured fields from raw data), Enrich (add hostname, service, environment labels), Sample (probabilistic sampling with configurable rate), Aggregate (pre-compute metric sums/counts/min/max over time windows)
4. **Backpressure**: bounded channels between stages. When a downstream channel is full, upstream blocks (no silent drops). Configurable channel capacity. Metrics on channel utilization and backpressure events
5. **Metrics Storage**: time-bucketed storage. Each metric is stored in a bucket for its time window. Support multiple resolutions: raw (1s), 10s, 1m, 5m, 1h. Downsampling aggregates finer-resolution buckets into coarser ones. Query by metric name, time range, and tags
6. **Log Storage**: inverted index mapping tokenized terms to log entry IDs. Tokenizer splits on whitespace and punctuation. Query by search terms (AND semantics). Return matching log entries sorted by timestamp
7. **Trace Storage**: store spans indexed by trace ID. Assemble spans into trace trees (parent-child relationships). Query by trace ID returns the full trace tree. Support finding traces by service name or operation name
8. **Query API**: HTTP or function-based API with endpoints: query metrics (name, time range, tags, resolution), search logs (terms, time range, severity), get trace (trace ID), find traces (service, operation, time range)
9. **Pipeline Configuration**: define pipeline topology in code or config: which stages are active, their order, sampling rates, aggregation windows, channel capacities
10. **Metrics on the pipeline itself**: ingestion rate per collector, processing latency per stage, storage write rate, query latency, dropped/backpressured events count

## Hints

Minimal. Two starting points:

1. **Pipeline-first design**: get data flowing through bounded channels before worrying about storage. A pipeline with Parse -> Enrich -> Print-to-stdout is a valid first milestone. Add storage and query later. The pipeline's backpressure behavior under load is the hardest part to get right.

2. **Storage is three separate problems**: do not build a generic storage engine. Metrics need time-bucketed arrays (a `map[metricKey]map[timeBucket][]float64`). Logs need an inverted index (a `map[token][]logID`). Traces need a span store (a `map[traceID][]Span`). Each has completely different access patterns and data structures. Treat them as three independent stores behind a common query interface.

## Acceptance Criteria

- [ ] Three collectors running concurrently: OTLP-like (TCP), StatsD (UDP), Syslog (TCP)
- [ ] Processing pipeline with at least 4 stages connected by bounded channels
- [ ] Backpressure: when storage is slow, pipeline stages block without dropping data (verified by test)
- [ ] Metrics stored in time-bucketed format, queryable by name, time range, and tags
- [ ] Logs indexed by inverted index, searchable by terms with AND semantics
- [ ] Traces assembled into trees from spans, queryable by trace ID
- [ ] Downsampling: finer-resolution metric data aggregated into coarser buckets
- [ ] Pipeline configuration: stages, sampling rate, channel capacity all configurable
- [ ] Pipeline self-metrics: ingestion rate, processing latency, backpressure events
- [ ] End-to-end test: ingest 10K mixed telemetry items, verify all queryable in storage
- [ ] All tests pass with race detector (Go) / no undefined behavior (Rust)

## Going Further

- **Alerting engine**: define alert rules on metric thresholds (e.g., error_rate > 0.05 for 5m). Evaluate rules against stored metrics, fire alerts
- **Trace-to-logs correlation**: link trace spans to log entries by shared trace ID in log attributes. Query API returns logs for a given trace
- **Adaptive sampling**: adjust sampling rate based on error rate or latency percentiles. Sample 100% of error traces, 1% of healthy traces
- **Distributed deployment**: run pipeline stages on different nodes, connected by network channels instead of in-process channels

## Research Resources

- [OpenTelemetry specification](https://opentelemetry.io/docs/specs/otel/) -- the industry standard for telemetry collection and format
- [OTLP protocol specification](https://opentelemetry.io/docs/specs/otlp/) -- the wire protocol for telemetry transport
- [StatsD protocol](https://github.com/statsd/statsd/blob/master/docs/metric_types.md) -- UDP-based metrics protocol
- [RFC 5424: The Syslog Protocol](https://datatracker.ietf.org/doc/html/rfc5424) -- structured syslog format
- [Prometheus TSDB design](https://fabxc.org/tsdb/) -- time-series storage design with inverted index
- [Grafana Loki architecture](https://grafana.com/docs/loki/latest/get-started/architecture/) -- log aggregation system design
- [Jaeger architecture](https://www.jaegertracing.io/docs/1.53/architecture/) -- distributed tracing backend design
- [Martin Kleppmann: Designing Data-Intensive Applications, Chapter 3](https://dataintensive.net/) -- storage engines and indexing
