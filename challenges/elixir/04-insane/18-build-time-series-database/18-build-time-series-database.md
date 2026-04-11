# Time-Series Database with High-Cardinality Support

**Project**: `chronos` — a purpose-built time-series database with Gorilla compression and multi-tier retention

---

## Project context

You are building `chronos`, a time-series database optimized for high-throughput metric ingestion and high cardinality (many unique label combinations). The database implements the compression techniques from Facebook's Gorilla paper and supports a multi-tier retention policy with automatic downsampling.

Project structure:

```
chronos/
├── lib/
│   └── chronos/
│       ├── application.ex           # database supervisor, retention scheduler
│       ├── database.ex              # public API: ingest, query, cardinality, stats
│       ├── series.ex                # series identity: metric + labels → series_id
│       ├── chunk.ex                 # time bucket: compressed binary chunk per series per hour
│       ├── gorilla.ex               # Gorilla encoding: delta-of-delta timestamps, XOR floats
│       ├── query_engine.ex          # decompress, filter, aggregate over time ranges
│       ├── downsampler.ex           # GenServer: raw → hourly → daily aggregates on schedule
│       ├── retention.ex             # GenServer: delete raw/hourly/daily buckets past threshold
│       └── label_index.ex           # inverted index: label key=value → [series_id]
├── test/
│   └── chronos/
│       ├── gorilla_test.exs         # compression ratio, correctness
│       ├── ingest_test.exs          # throughput, ordering
│       ├── query_test.exs           # range queries, aggregation, gap fill
│       ├── cardinality_test.exs     # 1M series, O(1) lookup
│       └── retention_test.exs       # downsampling, deletion schedule
├── bench/
│   └── chronos_bench.exs
└── mix.exs
```

---

## The problem

Monitoring systems ingest millions of metric data points per second from thousands of sources. Each metric has a name and a set of labels (e.g., `{host: "web-01", region: "eu-west"}`). The unique combination of metric name + labels is called a "series." A large cluster can have millions of unique series ("high cardinality"). The database must ingest fast, compress aggressively (metrics are repetitive), and query efficiently by time range and label filter.

---

## Why this design

**Gorilla delta-of-delta encoding for timestamps**: consecutive metric timestamps are almost always uniformly spaced (e.g., every 10 seconds). The delta between consecutive timestamps is nearly constant. The delta-of-delta (second derivative) is nearly zero. Gorilla encodes this with 1 bit for the common case (delta-of-delta == 0), 2-3 bits for small changes, and a full 64-bit integer for anomalies. A sequence of 1000 uniform timestamps compresses from 8KB to under 100 bytes.

**Gorilla XOR encoding for float values**: consecutive values of a counter or gauge are similar. XOR of two similar floats has many leading and trailing zeros. Gorilla stores only the non-zero middle bits plus the leading/trailing zero counts. A monotonically increasing counter with small increments achieves 4:1 compression.

**Time bucketing by hour**: each (series_id, unix_hour) pair is one ETS entry holding a compressed chunk. Range queries decompress only the relevant hourly chunks. Retention cleanup deletes chunks by key pattern without scanning data content.

**Label inverted index for cardinality queries**: for each label key=value pair, maintain a set of series IDs that have that label. A query like `{host: "web-01"}` returns the series IDs from the inverted index in O(1), then decompresses only those series' chunks.

---

## Implementation milestones

### Step 1: Create the project

```bash
mix new chronos --sup
cd chronos
mkdir -p lib/chronos test/chronos bench
```

### Step 2: `mix.exs` — dependencies

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: Gorilla encoding

```elixir
# lib/chronos/gorilla.ex
defmodule Chronos.Gorilla do
  @moduledoc """
  Gorilla compression for time-series data.

  Timestamp encoding (delta-of-delta):
    First timestamp: stored as full 64-bit value.
    Delta_1 = t1 - t0: stored as full 32-bit value.
    For subsequent timestamps:
      DoD = delta_N - delta_{N-1}
      if DoD == 0:           1-bit code: 0
      if DoD in [-63, 64]:   2-bit header 10, 7-bit value
      if DoD in [-255, 256]: 3-bit header 110, 9-bit value
      otherwise:             4-bit header 1110, 12-bit value
      full 64-bit:           5-bit header 11110, 64-bit value

  Float encoding (XOR):
    First value: stored as full 64-bit IEEE 754.
    XOR with previous value:
      if XOR == 0:           1-bit code: 0
      if leading/trailing zeros same as previous: 1-bit header 1 0, M significant bits
      otherwise: 1-bit header 1 1, 5-bit leading zeros, 6-bit M, M significant bits
  """

  @doc "Encodes a list of {timestamp_ms, float_value} pairs into a compressed binary."
  def encode(samples) do
    # TODO: implement as a bitstring accumulator using << existing :: bitstring, new_bits :: size(n) >>
    # HINT: represent state as {prev_ts, prev_delta, prev_xor_state, bitstring}
  end

  @doc "Decodes a compressed binary back to [{timestamp_ms, float_value}]."
  def decode(binary) do
    # TODO
  end
end
```

### Step 4: Series identity and label index

```elixir
# lib/chronos/series.ex
defmodule Chronos.Series do
  @moduledoc """
  Maps (metric_name, labels_map) to a stable series_id integer.
  Maintains an inverted index: label_key=value → MapSet(series_id).
  """

  def series_id(metric, labels) do
    # TODO: hash {metric, :erlang.phash2(:lists.sort(Map.to_list(labels)))}
    # HINT: use :erlang.phash2/2 with a large max value for stable 32-bit ID
  end

  def index_labels(series_id, labels) do
    # TODO: for each {k, v} in labels, add series_id to the inverted index
    # HINT: :ets.insert(:label_index, {{k, v}, series_id}) with :bag type
  end

  def lookup_by_label(key, value) do
    # TODO: :ets.lookup(:label_index, {key, value}) → [series_id]
  end
end
```

### Step 5: Given tests — must pass without modification

```elixir
# test/chronos/gorilla_test.exs
defmodule Chronos.GorillaTest do
  use ExUnit.Case, async: true

  alias Chronos.Gorilla

  test "uniform timestamps compress to under 100 bytes for 1000 samples" do
    now = System.system_time(:millisecond)
    samples = for i <- 0..999, do: {now + i * 10_000, 0.0}

    compressed = Gorilla.encode(samples)
    raw_size = length(samples) * (8 + 8)  # 8 bytes ts + 8 bytes float

    assert byte_size(compressed) < 100,
      "expected < 100 bytes, got #{byte_size(compressed)} (raw: #{raw_size})"
  end

  test "monotonic counter achieves at least 4:1 compression" do
    samples = for i <- 1..1_000, do: {i * 10_000, i * 1.0}
    compressed = Gorilla.encode(samples)
    raw_size = length(samples) * 16

    ratio = raw_size / byte_size(compressed)
    assert ratio >= 4.0, "expected >= 4:1 compression, got #{Float.round(ratio, 2)}:1"
  end

  test "decode reverses encode with zero loss" do
    now = System.system_time(:millisecond)
    original = for i <- 0..99, do: {now + i * 1_000, :rand.uniform() * 100.0}

    decoded = original |> Gorilla.encode() |> Gorilla.decode()

    for {{ots, oval}, {dts, dval}} <- Enum.zip(original, decoded) do
      assert ots == dts
      assert_in_delta oval, dval, 0.000001
    end
  end
end
```

```elixir
# test/chronos/query_test.exs
defmodule Chronos.QueryTest do
  use ExUnit.Case, async: false

  setup do
    {:ok, db} = Chronos.Database.start_link()
    {:ok, db: db}
  end

  test "range query returns correct aggregation", %{db: db} do
    now = System.system_time(:millisecond)

    for i <- 0..99 do
      Chronos.ingest(db, "cpu.usage", %{host: "web-01"}, 50.0 + i * 0.1, now + i * 60_000)
    end

    results = Chronos.query(db, "cpu.usage", %{host: "web-01"},
      from: now, to: now + 100 * 60_000, aggregate: :avg, step: "5m")

    # Should get one data point per 5-minute window
    assert length(results) == 20

    for {_ts, avg} <- results do
      assert is_float(avg)
      assert avg >= 50.0 and avg <= 60.0
    end
  end

  test "gap fill returns previous value for missing windows", %{db: db} do
    now = System.system_time(:millisecond)

    Chronos.ingest(db, "requests", %{svc: "api"}, 100.0, now)
    # Skip 3 windows, then add another point
    Chronos.ingest(db, "requests", %{svc: "api"}, 200.0, now + 4 * 60_000)

    results = Chronos.query(db, "requests", %{svc: "api"},
      from: now, to: now + 5 * 60_000, aggregate: :last, step: "1m",
      gap_fill: :fill_previous)

    values = Enum.map(results, fn {_ts, v} -> v end)
    # Gap windows must be filled with previous value (100.0)
    assert Enum.at(values, 1) == 100.0
    assert Enum.at(values, 2) == 100.0
  end
end
```

### Step 6: Run the tests

```bash
mix test test/chronos/ --trace
```

### Step 7: Benchmark

```elixir
# bench/chronos_bench.exs
{:ok, db} = Chronos.Database.start_link()
now = System.system_time(:millisecond)

Benchee.run(
  %{
    "ingest — single data point" => fn ->
      host = "host_#{:rand.uniform(10_000)}"
      Chronos.ingest(db, "cpu.usage", %{host: host}, :rand.uniform() * 100.0,
                     System.system_time(:millisecond))
    end,
    "query — 1h range, :avg, 1m step" => fn ->
      Chronos.query(db, "cpu.usage", %{host: "host_1"},
        from: now - 3_600_000, to: now, aggregate: :avg, step: "1m")
    end
  },
  parallel: 4,
  time: 10,
  warmup: 3,
  formatters: [Benchee.Formatters.Console]
)
```

Target: 1M data points/second sustained ingest, queries returning within 10ms for high-cardinality series.

---

## Trade-off analysis

| Aspect | Gorilla compression | Raw IEEE 754 | Run-length encoding |
|--------|--------------------|--------------|--------------------|
| Compression ratio (uniform data) | 50-100:1 | 1:1 | 10-20:1 |
| Encoding complexity | high (variable-width codes) | none | low |
| Decode speed | fast (bitstring scan) | zero (no decode) | moderate |
| Random access within chunk | not supported | O(1) | O(chunk) |
| Works on non-uniform data | degrades gracefully | n/a | degrades significantly |

Reflection: Gorilla encoding works best when consecutive values are similar. For a metric that changes randomly (e.g., cryptographically random data), what compression ratio would you expect and why?

---

## Common production mistakes

**1. Bitstring accumulation creating large binary copies**
`<< existing :: bitstring, new :: size(1) >>` creates a new binary on every append. For 1000 samples, this is 1000 allocations. Use an iolist or accumulate as a list of bit values and join at the end with `:erlang.list_to_bitstring/1`.

**2. Series ID collision from weak hashing**
If `series_id` collides for two different metric+label combinations, their data is mixed in the same chunks. Use a strong hash (SHA-256 truncated to 64 bits) and verify collision rate empirically at 1M series.

**3. Downsampling running during active ingest**
If the downsampler reads hourly chunks while ingest is writing them, it may read a partially written chunk. Gate downsampling on completed hours only: `current_unix_hour - 1` or older.

**4. Retention deleting aggregates before raw data**
Retain raw data for 24h, hourly for 30 days, daily for 1 year. Deleting hourly aggregates before raw data is compacted to hourly would create a query gap. Ensure the downsampling schedule runs before the raw retention window expires.

---

## Resources

- Pelkonen, T. et al. (2015). *Gorilla: A Fast, Scalable, In-Memory Time Series Database* — VLDB — the primary compression reference
- [InfluxDB TSM storage engine](https://docs.influxdata.com/influxdb/v1/concepts/storage_engine/)
- [Prometheus storage documentation](https://prometheus.io/docs/prometheus/latest/storage/)
- Kleppmann, M. — *Designing Data-Intensive Applications* — Chapter 3 (Column-Oriented Storage)
