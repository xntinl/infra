# 10. Prometheus Metrics Exposition

<!--
difficulty: advanced
concepts: [prometheus, counter, gauge, histogram, summary, metrics-endpoint, labels, collector]
tools: [go, curl, prometheus]
estimated_time: 35m
bloom_level: analyze
prerequisites: [http-programming, goroutines, sync-primitives]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of HTTP servers
- Familiarity with observability concepts (metrics, monitoring)
- `github.com/prometheus/client_golang` module

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** a `/metrics` endpoint using `prometheus/client_golang`
- **Analyze** when to use Counters, Gauges, Histograms, and Summaries
- **Design** metrics with appropriate labels and naming conventions
- **Test** metric values programmatically using the `testutil` package

## Why Prometheus Metrics Matter

Prometheus is the de facto standard for metrics collection in cloud-native systems. Your Go application exposes a `/metrics` endpoint in Prometheus exposition format, and Prometheus scrapes it at regular intervals. These metrics power dashboards, alerts, and capacity planning.

The four metric types serve different purposes:
- **Counter**: monotonically increasing (requests served, errors)
- **Gauge**: can go up or down (active connections, queue depth)
- **Histogram**: distribution of values in buckets (request latency)
- **Summary**: similar to histogram but calculates quantiles client-side

## The Problem

Build an HTTP API server that exposes Prometheus metrics about its own operations. The server has two business endpoints (`/api/orders` and `/api/users`) and must track:

1. Total requests per endpoint and status code
2. Request duration distribution
3. Active in-flight requests
4. Business metrics (orders created, users registered)

## Requirements

1. **Metrics endpoint** -- serve `/metrics` using `promhttp.Handler()`
2. **Counter** -- `http_requests_total` with labels `method`, `path`, `status`
3. **Histogram** -- `http_request_duration_seconds` with labels `method`, `path` and custom buckets (10ms, 50ms, 100ms, 250ms, 500ms, 1s, 5s)
4. **Gauge** -- `http_requests_in_flight` with label `path`
5. **Business counter** -- `orders_created_total` incremented when an order is created
6. **Middleware** -- implement metrics collection as HTTP middleware that wraps handlers
7. **Tests** -- verify metric values using `testutil.ToFloat64()` after exercising the handlers

## Hints

<details>
<summary>Hint 1: Defining metrics</summary>

```go
var (
    httpRequestsTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "http_requests_total",
            Help: "Total number of HTTP requests",
        },
        []string{"method", "path", "status"},
    )

    httpRequestDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "http_request_duration_seconds",
            Help:    "HTTP request duration in seconds",
            Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 5},
        },
        []string{"method", "path"},
    )

    httpRequestsInFlight = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "http_requests_in_flight",
            Help: "Number of HTTP requests currently being processed",
        },
        []string{"path"},
    )
)

func init() {
    prometheus.MustRegister(httpRequestsTotal, httpRequestDuration, httpRequestsInFlight)
}
```

</details>

<details>
<summary>Hint 2: Metrics middleware</summary>

```go
func metricsMiddleware(path string, next http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        inFlight := httpRequestsInFlight.WithLabelValues(path)
        inFlight.Inc()
        defer inFlight.Dec()

        start := time.Now()
        rw := &responseWriter{ResponseWriter: w, statusCode: 200}
        next(rw, r)

        duration := time.Since(start).Seconds()
        status := strconv.Itoa(rw.statusCode)
        httpRequestsTotal.WithLabelValues(r.Method, path, status).Inc()
        httpRequestDuration.WithLabelValues(r.Method, path).Observe(duration)
    }
}
```

</details>

<details>
<summary>Hint 3: Testing metrics</summary>

```go
import "github.com/prometheus/client_golang/prometheus/testutil"

// After making a request
count := testutil.ToFloat64(httpRequestsTotal.WithLabelValues("GET", "/api/orders", "200"))
if count != 1.0 {
    t.Errorf("expected 1 request, got %f", count)
}
```

</details>

## Verification

```bash
go test -v -race ./...

# Manual testing
go run main.go &
curl http://localhost:8080/api/orders
curl http://localhost:8080/api/orders
curl http://localhost:8080/metrics | grep http_requests_total
```

Your tests should confirm:
- `http_requests_total` increments per request with correct labels
- `http_request_duration_seconds` records latency in the correct bucket range
- `http_requests_in_flight` goes up during a request and back down after
- `/metrics` endpoint returns valid Prometheus exposition format
- Business metrics (`orders_created_total`) track domain events

## What's Next

Continue to [11 - OpenTelemetry Collector Integration](../11-opentelemetry-collector-integration/11-opentelemetry-collector-integration.md) to integrate with the OpenTelemetry Collector for traces and metrics export.

## Summary

- Use `prometheus/client_golang` to expose a `/metrics` endpoint scraped by Prometheus
- Counters count events (requests, errors); gauges measure current values (in-flight, queue depth)
- Histograms distribute values into buckets (request latency); choose buckets based on your SLOs
- Labels add dimensions to metrics but high cardinality (e.g., user IDs) destroys performance
- Implement metrics collection as middleware to keep business logic clean
- Test metric values with `testutil.ToFloat64()` for deterministic assertions

## Reference

- [prometheus/client_golang](https://pkg.go.dev/github.com/prometheus/client_golang)
- [Prometheus metric types](https://prometheus.io/docs/concepts/metric_types/)
- [Naming conventions](https://prometheus.io/docs/practices/naming/)
