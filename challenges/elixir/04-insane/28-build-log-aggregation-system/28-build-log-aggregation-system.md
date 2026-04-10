# 28. Build a Log Aggregation System

**Difficulty**: Insane

---

## Prerequisites

- Elixir TCP/UDP socket servers with `:gen_tcp` and `:gen_udp`
- GenServer and Supervisor for service lifecycle
- Inverted index and full-text search fundamentals
- Time-series data structures
- Syslog RFC 5424 format
- ETS for concurrent read-heavy workloads
- Basic HTTP server implementation

---

## Problem Statement

Build a log aggregation and search system comparable to a minimal ELK stack. The system must:

1. Ingest logs from multiple sources concurrently over TCP (syslog format) and HTTP (JSON payload)
2. Parse ingested logs using grok-like pattern matching to extract structured fields from unstructured messages
3. Index message content in an inverted index to support full-text search across millions of log entries
4. Aggregate indexed data to answer queries like "count of errors in the last 5 minutes" or "P99 response time over the last hour"
5. Evaluate alerting rules continuously and fire notifications when conditions are met
6. Enforce per-source retention policies, compacting old data and eventually deleting it
7. Separate log storage and queries by source and application identifier (multi-tenancy)
8. Display a text-based dashboard showing current error rates, request rates, and latency percentiles

---

## Acceptance Criteria

- [ ] Log ingestion: a TCP server listens for syslog-formatted messages (RFC 5424 and RFC 3164); an HTTP endpoint accepts `POST /ingest` with a JSON body `{source, level, message, timestamp, fields}`; both are available simultaneously
- [ ] Parsing: a pattern library supports at least nginx access log, PostgreSQL log, and plain JSON formats; patterns extract fields (IP, method, path, status, duration) into a structured map; custom patterns can be added at runtime via API
- [ ] Full-text search: messages are tokenized and stored in an inverted index; `GET /search?q=error+connection+refused&source=nginx&from=2024-01-01T00:00:00Z` returns matching log entries sorted by timestamp descending
- [ ] Aggregation: `GET /aggregate` accepts a field, function (`count`, `avg`, `p50`, `p95`, `p99`), source filter, and time window; returns the computed value; percentile computation uses a streaming algorithm (T-Digest or DDSketch)
- [ ] Alerting: `POST /alerts` creates a rule with a condition (e.g., `count(level=error) > 50 in 5m`); the system evaluates all rules every 30 seconds and calls a webhook URL when the condition is first true and again when it resolves
- [ ] Retention: each source has a configurable `retention_days`; entries older than that threshold are compacted (aggregates preserved, raw messages dropped) and eventually deleted; a background task runs this on a schedule
- [ ] Multi-tenancy: all API endpoints require a `source` or `application` filter; data from different sources is physically separated in ETS or on disk; one source cannot read another's data
- [ ] Dashboard: a periodic terminal output (or HTTP `/dashboard` endpoint returning plain text) shows the top 5 error messages, current ingestion rate (events/sec), and P99 latency for the last 5 minutes

---

## What You Will Learn

- Building inverted indexes for full-text search
- Streaming percentile algorithms (T-Digest, GK-summary) for approximate quantiles
- Pattern-based log parsing (grok-like DSL)
- Time-bucketed storage for efficient range queries
- Continuous alerting rule evaluation with state tracking (firing vs. resolved)
- Multi-tenant data isolation strategies in Elixir
- Designing an ingestion pipeline that does not drop events under burst load

---

## Hints

- Research RFC 5424 (syslog) — the format is more structured than RFC 3164 but both are widely used
- Study how Elasticsearch builds inverted indexes; a simpler version using ETS maps works for this scale
- T-Digest is a well-documented algorithm for approximate streaming percentiles — a reference implementation exists in most languages
- Grok patterns are named regex fragments that can be composed; research the Logstash grok filter documentation
- Think about how to prevent a slow full-text search from blocking log ingestion — separate processes and pools
- Investigate how to implement range queries over time-bucketed data without scanning all entries

---

## Reference Material

- RFC 5424: The Syslog Protocol
- Elasticsearch: "Inverted Index" documentation
- "T-Digest: Computing Accurate Quantiles Using Clusters" — Ted Dunning
- Logstash Grok Filter documentation (elastic.co)
- "Logs vs. Metrics vs. Traces" — Peter Bourgon (blog)

---

## Difficulty Rating ★★★★★★

Full-text indexing, streaming percentiles, multi-tenant isolation, and continuous alerting evaluation combine into a system that exercises nearly every area of Elixir/OTP.

---

## Estimated Time

60–100 hours
