# 113. Metrics Pipeline Aggregation

<!--
difficulty: advanced
category: observability-and-monitoring
languages: [go, rust]
concepts: [metrics, counters, gauges, histograms, time-windows, statsd, prometheus, pipeline-architecture, aggregation]
estimated_time: 8-10 hours
bloom_level: evaluate, create
prerequisites: [go-basics, rust-basics, concurrency, networking, udp-sockets, http-servers, time-handling, data-structures]
-->

## Languages

- Go (1.22+)
- Rust (stable)

## Prerequisites

- UDP socket programming for StatsD protocol ingestion
- HTTP server for Prometheus exposition format endpoint
- Concurrent data structures: atomic counters, mutex-guarded maps
- Time windowing: bucketing events by wall clock intervals
- Go: `sync/atomic`, `sync.Map` or sharded maps, `net`, `net/http`
- Rust: `std::net::UdpSocket`, `std::sync::Arc`, `parking_lot` or `std::sync::Mutex`, `hyper` or `tiny_http`

## Learning Objectives

- **Design** a multi-stage metrics pipeline that separates collection, aggregation, and export concerns
- **Implement** the three fundamental metric types (counters, gauges, histograms) with correct semantics
- **Evaluate** the trade-offs between push-based (StatsD) and pull-based (Prometheus) metric export models
- **Analyze** time window aggregation strategies and their impact on memory, accuracy, and latency
- **Create** a pipeline that handles sustained throughput of 100,000+ metrics per second without data loss

## The Challenge

Observability at scale requires metrics: counters that track how many times something happened, gauges that record current values, and histograms that capture distributions. Every production monitoring stack -- Prometheus, Datadog, CloudWatch -- is built on these three primitives. But between the application emitting a metric and the dashboard displaying it, there is a pipeline: collection (receiving raw metric events), aggregation (combining events into time-windowed summaries), and export (serving the aggregated data to monitoring backends).

Building this pipeline teaches you how metrics systems actually work. Why does Prometheus scrape on intervals instead of receiving pushes? Because pull-based collection decouples the application from the monitoring system and provides natural backpressure. Why does StatsD use UDP? Because fire-and-forget semantics mean a slow monitoring system never blocks the application. Why are histograms more useful than averages? Because averages hide outliers -- you need distribution data to understand tail latency.

Your task is to build a complete metrics pipeline in both Go and Rust. The pipeline has three stages. **Collection** receives metrics from two sources: a StatsD-compatible UDP listener (push-based) and a programmatic API (in-process). **Aggregation** rolls up raw events into time-windowed summaries at configurable intervals (1 second, 10 seconds, 1 minute). **Export** serves aggregated metrics in two formats: Prometheus exposition text (pull-based, served over HTTP) and StatsD-compatible UDP output (push to a downstream aggregator).

The hard part is concurrent aggregation. Metrics arrive continuously from multiple sources while the aggregation layer must atomically rotate time windows and the export layer must read consistent snapshots. A naive mutex around the entire metric store serializes all operations and kills throughput. You need a design that allows concurrent writes and periodic consistent reads.

## Key Concepts

**Counters** are monotonically increasing values. They only go up (or reset to zero on restart). The interesting value is the rate of change, not the absolute value. Your aggregation must compute per-window deltas.

**Gauges** are point-in-time values that can go up or down. The last value in a window wins. Use cases: queue depth, memory usage, active connections.

**Histograms** record the distribution of values in configurable buckets. A histogram with buckets `[5, 10, 25, 50, 100, 250]` counts how many observations fell into each range. Prometheus histograms are cumulative (the `le=100` bucket includes all observations <= 100). Your implementation must follow this convention.

**Time windows** divide the metric stream into fixed intervals. A 10-second window starting at T=0 covers [0, 10). At T=10, the window rotates: the current window becomes the previous window (available for export), and a new empty window starts. Double-buffering (current + previous) avoids locking between writers and readers.

**StatsD protocol** is line-based over UDP: `metric.name:value|type|@sample_rate`. Types: `c` (counter), `g` (gauge), `ms` (timer/histogram). Example: `http.requests:1|c`, `cpu.usage:72.5|g`, `api.latency:34|ms|@0.5` (sampled at 50%).

**Prometheus exposition format** is text-based, served over HTTP on `/metrics`:
```
# HELP http_requests_total Total HTTP requests
# TYPE http_requests_total counter
http_requests_total{method="GET",path="/api"} 1234
# TYPE api_latency_seconds histogram
api_latency_seconds_bucket{le="0.005"} 24
api_latency_seconds_bucket{le="0.01"} 33
api_latency_seconds_bucket{le="+Inf"} 144
api_latency_seconds_sum 53.42
api_latency_seconds_count 144
```

## Requirements

1. Three metric types: Counter (increment by N), Gauge (set, increment, decrement), Histogram (observe value into configurable buckets)
2. Metric identity: name (string) + labels (key-value pairs). `http_requests{method="GET"}` and `http_requests{method="POST"}` are distinct time series
3. StatsD UDP listener: parse the StatsD line protocol, support counter, gauge, and timer types, handle sample rates
4. Programmatic API: `metrics.Counter("name", labels).Add(n)`, `metrics.Gauge("name", labels).Set(v)`, `metrics.Histogram("name", labels, buckets).Observe(v)`
5. Time-windowed aggregation at configurable intervals (1s, 10s, 1m): counters accumulate per window, gauges keep last value, histograms accumulate bucket counts
6. Window rotation: at each interval boundary, atomically swap current and previous windows. Previous window is available for export, current window starts empty
7. Prometheus exporter: HTTP endpoint `/metrics` serving all metrics in Prometheus exposition text format
8. StatsD exporter: push aggregated metrics to a downstream UDP endpoint in StatsD format
9. Pipeline stages are decoupled: collection feeds aggregation via an internal channel/queue; aggregation feeds export on demand (pull) or on timer (push)
10. Sustained throughput: handle 100k+ metric events per second without dropping data (benchmark required)
11. Graceful shutdown: drain in-flight metrics, flush the current window, close listeners and exporters
12. Label cardinality protection: reject or drop metrics that would create more than a configurable number of distinct label combinations per metric name

## Hints

Hints for this challenge are intentionally minimal. Studying Prometheus client library internals is the most productive preparation.

1. For concurrent counter increments, use atomic operations (`atomic.Int64` in Go, `AtomicI64` in Rust). For gauges, use atomic float64 (Go: `math.Float64bits`/`math.Float64frombits` with `atomic.Uint64`; Rust: `AtomicU64` with `f64::to_bits`/`from_bits`). Avoid holding a mutex for individual metric updates -- it becomes a bottleneck at high throughput.

2. Double-buffer aggregation: maintain two window slots (current and previous). Writers always write to current. On rotation, swap the pointers atomically. Readers always read from previous. This eliminates read-write contention entirely. The only synchronization point is the swap, which is a single atomic pointer exchange.

3. For the StatsD parser, split on `|` first, then parse each segment. Handle edge cases: negative gauge values (prefix with `-` or `+`), sample rates (`@0.1` means multiply the value by 10), and multi-metric packets (multiple lines in one UDP datagram).

4. For the Prometheus exporter, iterate all metrics and format them into the exposition text on each scrape. Sort output by metric name for deterministic output. Include `# HELP` and `# TYPE` lines. Histogram buckets must include `+Inf` and be cumulative. Compute `_sum` and `_count` from the raw observations.

5. Label cardinality is a real production problem. An unbounded label (like user ID or request path) can create millions of time series, exhausting memory. Track the number of distinct label sets per metric name, and when it exceeds the limit, drop new label combinations and increment a `_dropped_total` counter.

6. For the benchmark, use `testing.B` in Go and `criterion` in Rust. The hot path is `Counter.Add()` / `Histogram.Observe()` -- these must be lock-free or nearly lock-free. Shard the metric store by metric name hash to reduce contention across cores.

## Acceptance Criteria

- [ ] Counters increment correctly and report per-window deltas
- [ ] Gauges report last observed value per window
- [ ] Histograms accumulate observations into correct cumulative buckets
- [ ] StatsD listener parses counter, gauge, and timer metrics correctly, including sample rates
- [ ] Programmatic API allows in-process metric recording
- [ ] Time windows rotate at configured intervals; previous window data is available for export
- [ ] Prometheus endpoint serves valid exposition format (verifiable with `promtool check metrics`)
- [ ] StatsD exporter pushes aggregated metrics to a downstream UDP endpoint
- [ ] Metric labels create distinct time series; same name with different labels are independent
- [ ] Label cardinality protection drops new label sets beyond the configured limit
- [ ] Benchmark demonstrates 100k+ metric events per second sustained throughput
- [ ] Concurrent writers (100+ goroutines/threads) produce no data races
- [ ] Graceful shutdown flushes all buffered data
- [ ] Both Go and Rust implementations pass their respective test suites

## Going Further

- **Delta vs. cumulative temporality**: Support both emission modes and understand which backends prefer which
- **Exemplars**: Attach trace IDs to histogram observations for linking metrics to traces
- **Remote write**: Implement Prometheus remote write protocol to push metrics to Cortex/Thanos
- **Hot-reload configuration**: Allow changing aggregation intervals and label limits without restart

## Research Resources

- [Prometheus Data Model](https://prometheus.io/docs/concepts/data_model/) -- metric types, labels, and naming conventions
- [Prometheus Exposition Format](https://prometheus.io/docs/instrumenting/exposition_formats/) -- the text format your exporter must produce
- [StatsD Protocol Specification](https://github.com/statsd/statsd/blob/master/docs/metric_types.md) -- the wire format for the UDP listener
- [Prometheus Go Client](https://github.com/prometheus/client_golang) -- study `prometheus/counter.go`, `prometheus/histogram.go` for implementation patterns
- [OpenTelemetry Metrics Specification](https://opentelemetry.io/docs/specs/otel/metrics/) -- the modern standard for metric collection semantics
- [VictoriaMetrics Architecture](https://docs.victoriametrics.com/single-server-victoriametrics/#architecture-overview) -- high-performance metrics storage; study its ingestion pipeline
- [Brendan Gregg: USE Method](https://www.brendangregg.com/usemethod.html) -- framework for deciding what metrics to collect (utilization, saturation, errors)
