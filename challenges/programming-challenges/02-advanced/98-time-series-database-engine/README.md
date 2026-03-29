<!-- difficulty: advanced -->
<!-- category: databases-time-series-tools -->
<!-- languages: [go, rust] -->
<!-- concepts: [time-series, columnar-storage, delta-encoding, xor-compression, downsampling, retention-policies] -->
<!-- estimated_time: 25-40 hours -->
<!-- bloom_level: analyze, evaluate, create -->
<!-- prerequisites: [binary-encoding, file-io, floating-point-representation, data-compression, btree-or-sorted-structures] -->

# Challenge 98: Time-Series Database Engine

## Languages

- Go (1.22+)
- Rust (stable)

## Prerequisites

- Binary data encoding and file I/O
- IEEE 754 floating-point bit representation (for XOR compression)
- Delta encoding and variable-length integer encoding
- Sorted data structures for range queries
- Understanding of time-series workloads (append-heavy, range-query-heavy, time-ordered)

## Learning Objectives

- **Design** a time-bucketed storage engine that partitions data by time range for efficient writes and queries
- **Implement** Gorilla-style compression: delta-of-delta for timestamps and XOR encoding for float values
- **Build** a columnar storage layout within time buckets that enables cache-efficient aggregation scans
- **Analyze** the trade-offs between write throughput, query latency, and storage size under different compression strategies
- **Create** downsampling and retention policies that automatically manage data lifecycle

## The Challenge

Time-series data is everywhere: server metrics, IoT sensor readings, financial ticks, application logs with numeric values. This data has unique properties that general-purpose databases handle poorly: it arrives in time order, is almost never updated, queries almost always span time ranges, and older data can be downsampled without losing analytical value.

Your task is to build a storage engine optimized for these properties. The engine must partition data into time buckets (e.g., 1-hour blocks), store data in columnar format within each bucket, compress timestamps using delta-of-delta encoding, compress float values using XOR encoding (the technique from Facebook's Gorilla paper), support range queries with aggregation functions, and implement retention policies that automatically delete data older than a configured threshold.

This is the core of what Prometheus, InfluxDB, and TimescaleDB do under the hood. Your implementation will be smaller but architecturally complete. The compression alone is the most interesting part: Gorilla encoding achieves 12:1 compression on real-world metrics data by exploiting the fact that consecutive timestamps have near-constant deltas and consecutive float values often share most of their bits.

Both Go and Rust implementations are required.

## Requirements

1. **Data model**:
   - A series is identified by a metric name and a set of labels (key-value tags): `cpu_usage{host="web01", core="0"}`
   - Each data point is a `(timestamp_ns int64, value float64)` pair
   - Series are append-only: new points must have timestamps >= the last point in the series
   - Support ingesting data points individually and in batch

2. **Time-bucketed storage**:
   - Partition data into configurable time buckets (default: 2 hours)
   - Each bucket contains all series data for its time range
   - Active bucket (current time range) accepts writes; older buckets are read-only and compressed
   - Flush active bucket to storage when the time range ends or memory threshold is reached

3. **Columnar storage within buckets**:
   - Store timestamps and values in separate byte arrays (columns) per series
   - This layout enables efficient scans for aggregation (only read the columns needed)
   - Each column is independently compressed

4. **Timestamp compression (delta-of-delta)**:
   - First timestamp stored as raw int64
   - Second value stored as delta from first
   - Subsequent values stored as delta-of-delta (difference between consecutive deltas)
   - Delta-of-delta values are typically 0 for regular-interval metrics, requiring very few bits
   - Use variable-bit encoding: 0 = 1 bit, small values = header + 7 bits, medium = header + 9 bits, large = header + 12 bits, huge = 32 bits

5. **Value compression (XOR encoding, Gorilla paper)**:
   - First value stored as raw 64-bit float
   - Subsequent values: XOR with previous value
   - If XOR = 0 (same value), store single `0` bit
   - If XOR != 0, find leading and trailing zeros in the XOR result
   - If leading/trailing zeros fit within previous block, store `10` + meaningful bits
   - Otherwise store `11` + 5-bit leading zeros count + 6-bit meaningful bits length + meaningful bits

6. **Range queries with aggregations**:
   - `Query(metric, labels, start_time, end_time)` returns raw data points
   - Aggregation functions: `avg`, `min`, `max`, `sum`, `count`, `first`, `last`
   - `QueryAgg(metric, labels, start, end, step, agg_func)` returns downsampled results at the given step interval
   - Queries span multiple buckets transparently

7. **Downsampling/rollups**:
   - Configure rollup rules: e.g., "after 24 hours, downsample to 5-minute averages"
   - Rollup produces new series at lower resolution, stored in their own buckets
   - Original high-resolution data can be retained or deleted per policy

8. **Retention policies**:
   - Configure per-metric retention: e.g., "delete data older than 7 days"
   - Background process periodically scans and removes expired buckets
   - Retention applies to both raw and downsampled data independently

## Hints

1. The delta-of-delta encoding for timestamps is a bit-level operation. You need a bit writer that can write arbitrary numbers of bits to a byte buffer. Implement `WriteBits(value uint64, numBits int)` that accumulates bits and flushes complete bytes. The corresponding `ReadBits(numBits int) uint64` reads from the same format.

2. For XOR float compression, work with the raw bits of the float (`math.Float64bits` in Go, `f64::to_bits` in Rust). The XOR of two similar floats has many leading and trailing zeros because the exponent bits are usually identical and the mantissa differs in the low bits. Count leading zeros with `bits.LeadingZeros64` (Go) or `u64::leading_zeros` (Rust).

3. The bucket structure can be an in-memory map from series ID to `([]int64, []float64)` pairs. When flushed, compress each pair independently and write to a file. The file format can be: `[series_count: u32] [series_id: varint_string] [ts_compressed_len: u32] [ts_compressed_bytes] [val_compressed_len: u32] [val_compressed_bytes] ...`.

4. For range queries across multiple buckets, identify which buckets overlap the query time range (O(1) with bucket duration arithmetic), decompress only those buckets, binary-search within each for the start offset, then scan forward to the end time. Decompression is sequential -- you cannot random-access into a delta-encoded stream.

5. Keep the retention policy simple: a goroutine/thread that runs every minute, checks each bucket's time range against the retention period, and deletes expired buckets. Since buckets are immutable after flushing, deletion is just removing the file and the index entry.

## Acceptance Criteria

- [ ] Data points ingest correctly with metric name, labels, timestamp, and value
- [ ] Series are append-only: inserting an out-of-order timestamp returns an error
- [ ] Time buckets partition data correctly (points land in the right bucket based on timestamp)
- [ ] Delta-of-delta timestamp compression produces fewer bytes than raw encoding for regular-interval data
- [ ] XOR value compression produces fewer bytes than raw encoding for slowly-changing float values
- [ ] Compression round-trips: decompress(compress(data)) == data (bit-exact for floats)
- [ ] Range queries return correct data points within the specified time window
- [ ] Aggregation functions (avg, min, max, sum, count) produce correct results
- [ ] Queries spanning multiple buckets merge results correctly in time order
- [ ] Downsampling produces lower-resolution series with correct aggregate values
- [ ] Retention policies delete buckets older than the configured threshold
- [ ] Batch ingestion handles 100,000+ points per second (single-threaded benchmark)
- [ ] Both Go and Rust implementations produce identical query results for the same data
- [ ] All tests pass (`go test ./...` and `cargo test`)

## Research Resources

- [Gorilla: A Fast, Scalable, In-Memory Time Series Database (Facebook, 2015)](http://www.vldb.org/pvldb/vol8/p1816-teller.pdf) -- the paper that defines XOR float compression and delta-of-delta timestamp encoding
- [Prometheus TSDB Design](https://fabxc.org/tsdb/) -- Fabian Reinartz's design doc for the Prometheus storage engine
- [InfluxDB Storage Engine (TSI)](https://docs.influxdata.com/influxdb/v2/reference/internals/storage-engine/) -- how InfluxDB organizes time-bucketed data
- [Writing a Time Series Database from Scratch](https://fabxc.org/tsdb/) -- practical implementation guide
- [Variable-Byte Encoding](https://nlp.stanford.edu/IR-book/html/htmledition/variable-byte-codes-1.html) -- the compression building block
- [IEEE 754 Floating Point (Wikipedia)](https://en.wikipedia.org/wiki/IEEE_754) -- understanding float bit layout for XOR compression
