# 8. Observability Metrics

<!--
difficulty: insane
concepts: [observability, prometheus-metrics, histograms, latency-percentiles, request-tracing, metric-cardinality, golden-signals, exponential-histograms]
tools: [go]
estimated_time: 3h
bloom_level: create
prerequisites: [42-capstone-service-mesh-data-plane/07-rate-limiting, 15-sync-primitives, 26-memory-model-and-optimization]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-07 (proxy through rate limiting) or equivalent service mesh data plane experience
- Familiarity with Prometheus metric types (counters, gauges, histograms) and exposition format

## Learning Objectives

- **Design** an observability metrics system that captures the four golden signals (latency, traffic, errors, saturation) for a service mesh data plane
- **Create** a high-performance metrics collection pipeline with lock-free counters, concurrent-safe histograms, and Prometheus-compatible exposition
- **Evaluate** the impact of metric cardinality on memory consumption and the trade-offs between granularity and resource usage

## The Challenge

Observability is what makes a service mesh operationally viable. Without metrics, you cannot know whether the proxy is healthy, how traffic is flowing, or where latency is introduced. Production data planes like Envoy emit thousands of metrics covering every aspect of proxy behavior: request counts, latency distributions, connection pool utilization, circuit breaker state, retry counts, and TLS handshake statistics.

You will build a metrics collection system from scratch -- not using the Prometheus client library, but implementing the core primitives yourself. You need three metric types: counters (monotonically increasing values like total requests), gauges (values that go up and down like active connections), and histograms (distribution of values like request latency). The histogram implementation must use configurable bucket boundaries and support computing percentiles (p50, p95, p99) from the bucket distribution. All metrics must be labeled with dimensions (upstream name, response status, method) and exposed in Prometheus text exposition format via an admin HTTP endpoint.

The real challenge is performance. Metrics are updated on every single request, often from thousands of concurrent goroutines. Lock contention on metric updates directly impacts proxy throughput. You must use atomic operations for counters and gauges, and design histograms that minimize contention while maintaining accuracy. You must also address cardinality: unconstrained label values (like full request paths) can cause unbounded memory growth.

## Requirements

1. Implement a `Counter` metric type that supports atomic `Inc()` and `Add(float64)` operations, returning the current value via `Value() float64`
2. Implement a `Gauge` metric type that supports atomic `Set(float64)`, `Inc()`, `Dec()`, and `Add(float64)` operations
3. Implement a `Histogram` metric type with configurable bucket boundaries that records observations via `Observe(float64)` and tracks count, sum, and per-bucket counts
4. Support labeled metrics: each metric can have a set of label names, and `WithLabels(map[string]string)` returns a child metric for that specific label combination
5. Implement a `Registry` that stores all registered metrics and exposes them via Prometheus text exposition format at a configurable HTTP endpoint
6. Record the four golden signals for the proxy: request latency histogram (labeled by upstream, method, status), request counter (labeled by upstream, method, status), error counter (labeled by upstream, error type), and active connections gauge (labeled by upstream)
7. Implement cardinality limiting: reject or truncate label values that would create more than a configurable number of unique label combinations per metric
8. Compute approximate percentiles (p50, p95, p99) from histogram bucket distributions using linear interpolation between bucket boundaries
9. Implement an exponential histogram option using power-of-two bucket boundaries for latency distributions where the range spans multiple orders of magnitude
10. Integrate metrics collection into the proxy request path with minimal overhead -- metric updates must not block request processing
11. Write benchmarks that demonstrate metric update throughput under high contention (1000+ goroutines)

## Hints

- Use `atomic.Uint64` with `math.Float64bits` and `math.Float64frombits` for lock-free float64 counter updates via compare-and-swap loops
- For histograms, use an array of `atomic.Uint64` counters (one per bucket) to avoid any locking on the hot path
- For labeled metrics, use a `sync.Map` keyed by the sorted, concatenated label values to store child metric instances
- For cardinality limiting, maintain an atomic counter of unique label combinations and reject new combinations once the limit is reached
- Prometheus text format is straightforward: `metric_name{label1="value1",label2="value2"} value timestamp` with special `_bucket`, `_count`, and `_sum` suffixes for histograms
- For percentile computation from histograms, use linear interpolation: if percentile falls in bucket [lower, upper] with count C, estimate the value as `lower + (upper - lower) * fraction_through_bucket`
- Use `sync.Pool` for temporary string builders when formatting the exposition output to reduce allocation pressure

## Success Criteria

1. Counters and gauges update correctly under concurrent access from 1000+ goroutines
2. Histogram observations are correctly distributed across bucket boundaries
3. Labeled metrics correctly isolate values for different label combinations
4. Cardinality limiting prevents unbounded memory growth from high-cardinality labels
5. Prometheus text exposition format is correct and parseable by a Prometheus server
6. Percentile computation from histogram buckets is within 5% of actual percentile values
7. Metric update throughput exceeds 1 million operations per second on a single core
8. All tests and benchmarks pass with the `-race` flag enabled

## Research Resources

- [Prometheus data model](https://prometheus.io/docs/concepts/data_model/) -- metric types, labels, and naming conventions
- [Prometheus exposition format](https://prometheus.io/docs/instrumenting/exposition_formats/) -- text format specification for metric output
- [Envoy statistics architecture](https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/observability/statistics) -- reference design for high-performance proxy metrics
- [Go sync/atomic package](https://pkg.go.dev/sync/atomic) -- atomic operations for lock-free metric updates
- [HDR Histogram](http://hdrhistogram.org/) -- high dynamic range histogram design for latency measurement
- [The Four Golden Signals (Google SRE)](https://sre.google/sre-book/monitoring-distributed-systems/#xref_monitoring_golden-signals) -- the foundational metrics for service monitoring

## What's Next

Continue to [Control Plane gRPC](../09-control-plane-grpc/09-control-plane-grpc.md) where you will implement a gRPC-based control plane interface to dynamically configure the data plane.

## Summary

- Counters, gauges, and histograms form the three core metric types needed for proxy observability
- Lock-free atomic operations are essential for high-throughput metric updates on the request hot path
- Labeled metrics with cardinality limits balance granularity against memory consumption
- Prometheus text exposition format provides interoperability with standard monitoring infrastructure
- Histogram-based percentile computation provides approximate latency distribution analysis without storing individual observations
- The four golden signals (latency, traffic, errors, saturation) provide comprehensive visibility into proxy health
