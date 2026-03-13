# 8. GC Impact on Tail Latency

<!--
difficulty: insane
concepts: [tail-latency, p99-latency, gc-pauses, gc-assists, latency-histogram, stw-impact, request-scheduling]
tools: [go]
estimated_time: 60m
bloom_level: create
prerequisites: [gc-phases, gogc-and-gomemlimit, observing-gc-godebug, gc-pacer]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-06 in this section
- Understanding of GC phases, pacer, and tuning
- Familiarity with HTTP server programming and latency measurement

## Learning Objectives

- **Create** a latency measurement framework that isolates GC impact on request handling
- **Analyze** the correlation between GC events and tail latency spikes
- **Evaluate** different GC tuning strategies for minimizing p99/p999 latency

## The Challenge

Garbage collection is the single largest source of tail latency in most Go services. While Go's concurrent GC keeps median latency low, the stop-the-world pauses and GC assists during marking can spike p99 and p999 latencies. A request that arrives during a STW pause waits for the pause to complete. A request in a goroutine that gets drafted for GC assist work experiences added latency proportional to its allocation rate.

Build a realistic HTTP server benchmark that measures the impact of GC on tail latency. Run identical workloads under different GC configurations, capture latency distributions, and correlate individual high-latency requests with GC events. Then implement and measure mitigation strategies.

## Requirements

1. Build an HTTP server that performs a realistic workload: decode a JSON request, allocate intermediate objects, query an in-memory data structure, and return a JSON response
2. Build a load generator that sends requests at a controlled rate (e.g., 10,000 req/s) and records per-request latency with nanosecond precision
3. Capture a latency histogram with p50, p90, p95, p99, and p999 percentiles
4. Run the benchmark under at least four GC configurations: (a) GOGC=100 (default), (b) GOGC=50, (c) GOGC=off with GOMEMLIMIT, (d) GOGC=1000
5. Correlate latency spikes with GC events by logging GC cycle timestamps (from `runtime/metrics`) alongside request timestamps
6. Implement at least two mitigation strategies: (a) object pooling with `sync.Pool` to reduce allocation rate, (b) pre-allocated buffers to eliminate per-request heap allocation
7. Measure the before/after impact of each mitigation on p99 latency
8. Produce a summary report showing: configuration, p50, p99, p999, max latency, GC cycles, and GC CPU fraction

## Hints

- Use `time.Now()` with `time.Since()` for per-request timing. The monotonic clock ensures accuracy even across NTP adjustments.
- STW pauses affect all goroutines simultaneously -- you will see correlated latency spikes across multiple concurrent requests.
- GC assists affect individual goroutines proportional to their allocation rate. Heavy allocators see more assist overhead.
- `sync.Pool` reduces allocation pressure but objects may be collected between GC cycles. Use it for short-lived buffers, not long-term caches.
- The `/gc/pauses:seconds` metric from `runtime/metrics` provides a histogram of pause durations.
- Consider using `runtime.SetFinalizer` sparingly -- finalizers add GC overhead and delay object collection.
- A realistic test requires sustained load, not just burst testing. Run each configuration for at least 30 seconds.

## Success Criteria

1. Latency histograms clearly show the difference between median and tail latencies
2. GC-induced latency spikes are visibly correlated with GC cycle timestamps
3. Higher GOGC reduces GC frequency (fewer spikes) but increases memory usage
4. Object pooling measurably reduces p99 latency by lowering allocation rate
5. The summary report provides actionable data for choosing a GC configuration
6. The mitigation strategies produce a measurable improvement in p99/p999

## Research Resources

- [Go GC Guide -- Latency](https://tip.golang.org/doc/gc-guide#Latency) -- official latency considerations
- [sync.Pool](https://pkg.go.dev/sync#Pool) -- object pooling to reduce allocation pressure
- [runtime/metrics](https://pkg.go.dev/runtime/metrics) -- GC pause histograms and cycle counts
- [Gil Tene: How NOT to Measure Latency](https://www.infoq.com/presentations/latency-response-time/) -- latency measurement best practices
- [HDR Histogram](https://github.com/HdrHistogram/hdrhistogram-go) -- high-dynamic-range histogram for latency recording

## What's Next

Continue to [09 - Reducing GC Pressure](../09-reducing-gc-pressure/09-reducing-gc-pressure.md) to learn systematic techniques for reducing garbage collector workload.

## Summary

- GC is the primary source of tail latency in Go services
- STW pauses affect all goroutines; GC assists affect individual high-allocating goroutines
- Higher GOGC reduces GC frequency but increases memory -- a direct latency vs memory tradeoff
- Object pooling and pre-allocation are the most effective mitigation strategies
- Always measure p99/p999, not just median -- GC impact is invisible at the median
- Correlating GC events with request timestamps reveals the true cause of latency spikes
