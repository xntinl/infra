# 18. Build a Time-Series Database with High-Cardinality Support

**Difficulty**: Insane

## Prerequisites

- Mastered: Binary encoding/decoding, bitwise operations, ETS, file I/O, GenServer
- Mastered: Float representation (IEEE 754), delta encoding, basic compression concepts
- Familiarity with: Gorilla paper (Facebook TSDB), InfluxDB storage engine internals, Prometheus remote write format, cardinality problem in monitoring

## Problem Statement

Build a purpose-built time-series database in Elixir optimized for metric ingestion at
high throughput and high cardinality (many unique label combinations). The database must
implement the compression techniques from Facebook's Gorilla paper and support a
multi-tier retention policy:

1. Accept data points as `{metric_name, labels_map, value_float, timestamp_unix_ms}`.
   Ingest at a sustained rate of 1 million data points per second.
2. Compress timestamps using delta-of-delta encoding: store only the difference between
   consecutive delta values. Use the variable-length encoding described in the Gorilla paper
   (1-bit, 2-bit, 3-bit, or full 64-bit codes based on delta magnitude).
3. Compress float values using XOR encoding: XOR consecutive values and store only the
   meaningful bits (leading zero count + trailing zero count + significant bits).
4. Organize data into time buckets: raw data lives in hourly buckets, each stored as a
   separate compressed binary chunk in ETS and/or on disk.
5. Implement automatic downsampling: after 24 hours, raw buckets are aggregated into
   hourly summaries (min, max, avg, count). After 30 days, hourly summaries are aggregated
   into daily summaries.
6. Enforce retention: delete raw data after 24 hours, hourly data after 30 days, and daily
   data after 1 year. This must happen automatically without operator intervention.
7. Support a query API: `query(metric, labels, from, to, aggregate, step)` where `aggregate`
   is one of `:avg`, `:sum`, `:min`, `:max`, `:count`, and `step` is a duration string
   like `"5m"` or `"1h"`.
8. Implement gap filling: when no data exists in a query window segment, interpolate using
   the previous known value or fill with a configurable fill value.
9. Support label filtering: `query("cpu.usage", %{host: "web-01"}, ...)` returns only
   the series matching that label set.
10. Support 1 million unique label combinations (series) without performance degradation.
    Index series by label hash for O(1) series lookup.

## Acceptance Criteria

- [ ] Ingestion of 1M data points per second is sustained for 60 seconds without error;
      confirmed by measuring actual throughput with `Benchee` and reporting p50/p95/p99 latency.
- [ ] Timestamp delta-of-delta encoding reduces a sequence of 1000 uniform-interval timestamps
      to fewer than 100 bytes (versus 8000 bytes raw).
- [ ] XOR float encoding achieves a compression ratio of at least 4:1 on a monotonically
      increasing counter with small increments, versus raw IEEE 754 storage.
- [ ] `query("cpu.usage", %{host: "web-01"}, t1, t2, :avg, "5m")` returns a list of
      `{timestamp, avg_value}` tuples covering the range `[t1, t2)` with 5-minute resolution.
- [ ] Gap filling: a query window segment with no data returns `{timestamp, previous_value}`
      when gap strategy is `:fill_previous`, and `{timestamp, nil}` when strategy is `:null`.
- [ ] Downsampling runs automatically after the raw-to-hourly threshold; querying a time range
      older than 24 hours returns hourly aggregates, not raw points.
- [ ] Retention enforcement deletes raw buckets after 24 hours; the storage used by raw data
      does not grow unboundedly during a 48-hour continuous write test.
- [ ] 1 million distinct label combinations (series) are inserted; `query` with a specific
      label filter returns only the matching series in under 10ms.
- [ ] `DB.cardinality(db, "cpu.usage")` returns the count of unique label combinations for
      that metric name.

## What You Will Learn

- The Gorilla compression scheme: delta-of-delta for timestamps, XOR for floats — and why they work for typical metric data distributions
- Time bucketing: how to partition a time axis into chunks for efficient range queries and retention enforcement
- The cardinality explosion problem in time-series databases and how label indexing (inverted index on label key-value pairs) addresses it
- Downsampling pipelines: how to aggregate raw data into coarser granularities without losing summary statistics
- Gap-fill semantics: the difference between step-aligned queries and raw event queries
- Retention policy scheduling with GenServer timers and how to avoid memory leaks in long-running processes

## Hints

This exercise is intentionally sparse. Research:

- Gorilla encoding: represent the chunk as a bitstring in Elixir; use `<<bit::size(1)>>` concatenation for variable-width codes
- Series identity: hash `{metric_name, :erlang.phash2(sorted_labels)}` to get a stable series ID; store in ETS `{series_id, metric, labels}`
- Time bucket key: `{series_id, unix_hour}` where `unix_hour = div(timestamp_ms, 3_600_000)` — use this as ETS key for the raw chunk
- Downsampling trigger: a GenServer with `Process.send_after(self(), :downsample, :timer.hours(1))` that scans for buckets older than the threshold
- Query execution: compute `floor(from / step_ms) * step_ms` for each bucket boundary, decompress relevant chunks, aggregate in memory

## Reference Material

- Gorilla paper: Pelkonen et al., "Gorilla: A Fast, Scalable, In-Memory Time Series Database", VLDB 2015
- InfluxDB TSM storage engine: https://docs.influxdata.com/influxdb/v1/concepts/storage_engine/
- Prometheus storage documentation: https://prometheus.io/docs/prometheus/latest/storage/
- "Designing Data-Intensive Applications" — Martin Kleppmann, Chapter 3 (Column-Oriented Storage)
- IEEE 754 double precision format: https://en.wikipedia.org/wiki/Double-precision_floating-point_format

## Difficulty Rating

★★★★★★

## Estimated Time

60–85 hours
