# 29. Build a Metrics Collector (Prometheus-like)

**Difficulty**: Insane

---

## Prerequisites

- Elixir GenServer, ETS, and `:atomics` for concurrent counters
- HTTP server implementation (or understanding of Plug/Cowboy internals)
- Time-series data storage concepts
- Understanding of statistical aggregates (histograms, quantiles)
- Prometheus text exposition format specification
- PromQL query language semantics
- Cron/timer-based periodic evaluation

---

## Problem Statement

Build a metrics collection and alerting system compatible with the Prometheus ecosystem. The system must:

1. Expose a registry for the four Prometheus metric types with label support for multi-dimensional data
2. Serve an HTTP `/metrics` endpoint in the Prometheus text exposition format so any standard Prometheus scraper can collect data
3. Accept push-based metrics from short-lived jobs that cannot be scraped
4. Pre-compute expensive queries as recording rules evaluated on a schedule
5. Evaluate alerting rules against current metric values and fire notifications when thresholds are crossed
6. Store time-series data efficiently, supporting range queries over historical data
7. Answer a meaningful subset of PromQL including rate calculations, aggregations, and histogram quantiles

---

## Acceptance Criteria

- [ ] Metric types: `Counter` (monotonically increasing, never decreasing), `Gauge` (arbitrary up/down value), `Histogram` (configurable bucket boundaries, sums observations), `Summary` (streaming quantile calculation over a sliding window)
- [ ] Labels: every metric can be dimensioned with an arbitrary label set `{key: "value", ...}`; each unique label combination is a separate time series; label cardinality is enforced with a configurable limit per metric
- [ ] HTTP scraping: `GET /metrics` returns all registered metrics in Prometheus text exposition format (0.0.4); each metric family includes the `# HELP` and `# TYPE` lines followed by all time series; the endpoint is renderable by `prometheus` and `victoria-metrics`
- [ ] Push gateway: `POST /push/{job}/{instance}` accepts a Prometheus text body and stores it under the given job and instance labels; metrics pushed are available on the `/metrics` endpoint and expire after a configurable TTL
- [ ] Recording rules: a YAML/Elixir config defines rules like `record: job:http_requests:rate5m, expr: rate(http_requests_total[5m])`; the rule evaluator runs every 15 seconds and stores results as new time series
- [ ] Alerting rules: rules with `alert: HighErrorRate, expr: rate(http_errors_total[5m]) > 0.05` are evaluated every 15 seconds; when the expression is true for `for: 5m` (pending → firing), the system POSTs an alert payload to a configured webhook; resolving fires a resolved notification
- [ ] TSDB: time-series data is stored in compressed chunks of N samples; range queries retrieve only the relevant chunks; data older than the retention period is deleted in background sweeps
- [ ] PromQL subset: implement `rate(metric[duration])`, `increase(metric[duration])`, `sum(metric) by (label)`, `avg(metric)`, `min(metric)`, `max(metric)`, `histogram_quantile(phi, metric)`, scalar math (`+`, `-`, `*`, `/`, `>`); available via `GET /query?expr=...&time=...`

---

## What You Will Learn

- Implementing lock-free counters with Elixir `:atomics`
- Histogram bucket boundary design and `histogram_quantile` math
- Prometheus text exposition format parsing and generation
- Time-series database chunk compression (delta encoding, XOR encoding)
- PromQL evaluation model: instant vectors, range vectors, and matrix selectors
- Alerting state machines: inactive → pending → firing → resolved
- Label cardinality management to prevent unbounded memory growth

---

## Hints

- Read the Prometheus data model documentation carefully — the distinction between metric name, label sets, and time series is fundamental
- Study the Prometheus text format spec; it is short and precise
- Research XOR encoding for floating-point time-series compression (Gorilla paper by Pelkonen et al.)
- `histogram_quantile` requires understanding the assumption that observations are uniformly distributed within a bucket
- Look into how Prometheus evaluates range queries by fetching chunks that overlap the query range
- Research the alerting state machine — a firing alert must remain firing for the `for` duration before a notification is sent (pending state prevents flapping)

---

## Reference Material

- Prometheus Data Model and Text Exposition Format (prometheus.io/docs)
- "Gorilla: A Fast, Scalable, In-Memory Time Series Database" — Pelkonen et al., VLDB 2015
- "Prometheus: Up & Running" — Brian Brazil, O'Reilly
- OpenMetrics Specification (openmetrics.io)
- PromQL documentation (prometheus.io/docs/prometheus/latest/querying)

---

## Difficulty Rating ★★★★★★

Implementing a PromQL evaluator on top of a custom TSDB with correct histogram and rate semantics requires deep understanding of both time-series theory and the Prometheus data model.

---

## Estimated Time

70–110 hours
