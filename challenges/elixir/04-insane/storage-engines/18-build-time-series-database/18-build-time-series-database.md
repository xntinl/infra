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

## Design decisions

**Option A — Row-oriented storage (one row per (metric, timestamp) pair)**
- Pros: simple schema; easy updates.
- Cons: disk layout is wrong for range scans; compression ratios are poor.

**Option B — Columnar chunks with per-chunk timestamp indexing** (chosen)
- Pros: column-wise compression (delta, delta-of-delta, XOR for floats) reaches 10–20x; range scans read contiguous disk bytes.
- Cons: point updates are awkward; chunks are immutable once closed.

→ Chose **B** because time-series workloads are write-once-read-many and range-dominated — columnar is the native layout.

## Project Structure

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

## Implementation milestones

### Step 1: Create the project

**Objective**: Generate `--sup` skeleton so ingestion, chunk store, and query engine restart independently without losing in-flight series state.


```bash
mix new chronos --sup
cd chronos
mkdir -p lib/chronos test/chronos bench
```

### Step 2: `mix.exs` — dependencies

**Objective**: Keep deps to `:benchee` only; raw bitstrings and ETS cover Gorilla encoding and the label index without pulling a compression NIF.


```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: Gorilla encoding

**Objective**: Delta-of-delta timestamps and XOR-compress floats so monotonic series reach the 4:1 ratio Facebook's Gorilla paper guarantees.


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

  Float encoding (XOR):
    First value: stored as full 64-bit IEEE 754.
    XOR with previous value:
      if XOR == 0:           1-bit code: 0
      otherwise:             1-bit header 1, 6-bit leading zeros, 6-bit significant bits length, significant bits
  """

  use Bitwise

  @doc "Encodes a list of {timestamp_ms, float_value} pairs into a compressed binary."
  @spec encode([{integer(), float()}]) :: binary()
  def encode([]), do: <<>>

  def encode([{first_ts, first_val} | rest]) do
    first_val_bits = float_to_bits(first_val)
    init_bits = <<first_ts::64, first_val_bits::64>>

    case rest do
      [] ->
        pad_to_bytes(init_bits)

      [{second_ts, second_val} | tail] ->
        delta = second_ts - first_ts
        second_val_bits = float_to_bits(second_val)
        xor = bxor(first_val_bits, second_val_bits)

        acc = <<init_bits::bitstring, delta::signed-32>>
        acc = encode_xor_value(acc, xor, 0, 0)

        {final_bits, _prev_ts, _prev_delta, _prev_val_bits, _prev_leading, _prev_trailing} =
          Enum.reduce(tail, {acc, second_ts, delta, second_val_bits, 0, 0}, fn {ts, val}, {bits, prev_ts, prev_delta, prev_val_bits, prev_leading, prev_trailing} ->
            current_delta = ts - prev_ts
            dod = current_delta - prev_delta

            new_bits = encode_dod(bits, dod)

            val_bits = float_to_bits(val)
            xor = bxor(prev_val_bits, val_bits)
            new_bits = encode_xor_value(new_bits, xor, prev_leading, prev_trailing)

            {leading, trailing} =
              if xor == 0 do
                {prev_leading, prev_trailing}
              else
                l = count_leading_zeros_64(xor)
                t = count_trailing_zeros_64(xor)
                {l, t}
              end

            {new_bits, ts, current_delta, val_bits, leading, trailing}
          end)

        pad_to_bytes(final_bits)
    end
  end

  @doc "Decodes a compressed binary back to [{timestamp_ms, float_value}]."
  @spec decode(binary()) :: [{integer(), float()}]
  def decode(<<>>), do: []

  def decode(binary) do
    bits = binary_to_bitstring(binary)

    case bits do
      <<first_ts::64, first_val_bits::64, rest::bitstring>> ->
        first_val = bits_to_float(first_val_bits)

        case rest do
          <<>> ->
            [{first_ts, first_val}]

          <<delta::signed-32, rest2::bitstring>> ->
            second_ts = first_ts + delta
            {second_val_bits, rest3, leading, trailing} = decode_xor_value(rest2, first_val_bits, 0, 0)
            second_val = bits_to_float(second_val_bits)

            decode_loop(rest3, [{second_ts, second_val}, {first_ts, first_val}],
                       second_ts, delta, second_val_bits, leading, trailing)
        end

      _ ->
        []
    end
  end

  defp decode_loop(<<>>, acc, _prev_ts, _prev_delta, _prev_val_bits, _pl, _pt) do
    Enum.reverse(acc)
  end

  defp decode_loop(bits, acc, prev_ts, prev_delta, prev_val_bits, prev_leading, prev_trailing) do
    case decode_dod(bits) do
      :eof ->
        Enum.reverse(acc)

      {dod, rest} ->
        current_delta = prev_delta + dod
        current_ts = prev_ts + current_delta

        {current_val_bits, rest2, new_leading, new_trailing} =
          decode_xor_value(rest, prev_val_bits, prev_leading, prev_trailing)
        current_val = bits_to_float(current_val_bits)

        decode_loop(rest2, [{current_ts, current_val} | acc],
                   current_ts, current_delta, current_val_bits, new_leading, new_trailing)
    end
  end

  defp encode_dod(bits, 0), do: <<bits::bitstring, 0::1>>

  defp encode_dod(bits, dod) when dod >= -63 and dod <= 64 do
    <<bits::bitstring, 0b10::2, dod::signed-7>>
  end

  defp encode_dod(bits, dod) when dod >= -255 and dod <= 256 do
    <<bits::bitstring, 0b110::3, dod::signed-9>>
  end

  defp encode_dod(bits, dod) do
    <<bits::bitstring, 0b1110::4, dod::signed-32>>
  end

  defp decode_dod(<<0::1, rest::bitstring>>), do: {0, rest}
  defp decode_dod(<<0b10::2, dod::signed-7, rest::bitstring>>), do: {dod, rest}
  defp decode_dod(<<0b110::3, dod::signed-9, rest::bitstring>>), do: {dod, rest}
  defp decode_dod(<<0b1110::4, dod::signed-32, rest::bitstring>>), do: {dod, rest}
  defp decode_dod(_), do: :eof

  defp encode_xor_value(bits, 0, _prev_leading, _prev_trailing) do
    <<bits::bitstring, 0::1>>
  end

  defp encode_xor_value(bits, xor, _prev_leading, _prev_trailing) do
    leading = count_leading_zeros_64(xor)
    trailing = count_trailing_zeros_64(xor)
    significant_bits = 64 - leading - trailing
    significant = (xor >>> trailing) &&& ((1 <<< significant_bits) - 1)

    <<bits::bitstring, 1::1, leading::6, significant_bits::6, significant::size(significant_bits)>>
  end

  defp decode_xor_value(<<0::1, rest::bitstring>>, prev_val_bits, prev_leading, prev_trailing) do
    {prev_val_bits, rest, prev_leading, prev_trailing}
  end

  defp decode_xor_value(<<1::1, leading::6, sig_len::6, rest::bitstring>>, prev_val_bits, _prev_leading, _prev_trailing) do
    <<significant::size(sig_len), rest2::bitstring>> = rest
    trailing = 64 - leading - sig_len
    xor = significant <<< trailing
    val_bits = bxor(prev_val_bits, xor)
    {val_bits, rest2, leading, trailing}
  end

  defp decode_xor_value(<<>>, prev_val_bits, prev_leading, prev_trailing) do
    {prev_val_bits, <<>>, prev_leading, prev_trailing}
  end

  defp float_to_bits(f) do
    <<bits::64>> = <<f::float-64>>
    bits
  end

  defp bits_to_float(bits) do
    <<f::float-64>> = <<bits::64>>
    f
  end

  defp count_leading_zeros_64(0), do: 64
  defp count_leading_zeros_64(n), do: do_clz(n, 63, 0)

  defp do_clz(_n, -1, count), do: count
  defp do_clz(n, bit, count) do
    if (n >>> bit &&& 1) == 0 do
      do_clz(n, bit - 1, count + 1)
    else
      count
    end
  end

  defp count_trailing_zeros_64(0), do: 64
  defp count_trailing_zeros_64(n), do: do_ctz(n, 0)

  defp do_ctz(n, bit) do
    if (n >>> bit &&& 1) == 0 do
      do_ctz(n, bit + 1)
    else
      bit
    end
  end

  defp pad_to_bytes(bits) do
    bit_size = bit_size(bits)
    padding = rem(8 - rem(bit_size, 8), 8)
    padded = <<bits::bitstring, 0::size(padding)>>
    :erlang.bitstring_to_list(padded) |> :erlang.list_to_binary()
  end

  defp binary_to_bitstring(bin), do: bin
end
```

### Step 4: Series identity and label index

**Objective**: Hash `{metric, sorted_labels}` into a stable series_id and invert labels in ETS so multi-label filters become set intersections.


```elixir
# lib/chronos/series.ex
defmodule Chronos.Series do
  @moduledoc """
  Maps (metric_name, labels_map) to a stable series_id integer.
  Maintains an inverted index: label_key=value -> MapSet(series_id).
  """

  @doc "Returns a stable integer series_id for the given metric and label combination."
  @spec series_id(String.t(), map()) :: non_neg_integer()
  def series_id(metric, labels) do
    sorted_labels = labels |> Map.to_list() |> Enum.sort()
    :erlang.phash2({metric, sorted_labels}, 1_000_000_000)
  end

  @doc "Indexes the labels for a given series_id in the inverted index ETS table."
  @spec index_labels(non_neg_integer(), map()) :: :ok
  def index_labels(sid, labels) do
    ensure_table()
    Enum.each(labels, fn {k, v} ->
      :ets.insert(:chronos_label_index, {{k, v}, sid})
    end)
    :ok
  end

  @doc "Looks up all series_ids matching a label key=value pair."
  @spec lookup_by_label(atom() | String.t(), term()) :: [non_neg_integer()]
  def lookup_by_label(key, value) do
    ensure_table()
    :ets.lookup(:chronos_label_index, {key, value})
    |> Enum.map(fn {_key, sid} -> sid end)
    |> Enum.uniq()
  end

  @doc "Looks up series_ids matching all provided labels (intersection)."
  @spec lookup_by_labels(map()) :: [non_neg_integer()]
  def lookup_by_labels(labels) do
    labels
    |> Enum.map(fn {k, v} -> lookup_by_label(k, v) |> MapSet.new() end)
    |> Enum.reduce(fn set, acc -> MapSet.intersection(acc, set) end)
    |> MapSet.to_list()
  end

  defp ensure_table do
    case :ets.whereis(:chronos_label_index) do
      :undefined -> :ets.new(:chronos_label_index, [:named_table, :public, :bag])
      _ -> :ok
    end
  end
end
```

### Step 5: Chunk storage and query engine

**Objective**: Bucket samples into one-hour Gorilla-compressed chunks per series so range scans touch only the buckets that intersect the query window.


```elixir
# lib/chronos/chunk.ex
defmodule Chronos.Chunk do
  @moduledoc """
  Stores compressed chunks keyed by {series_id, hour_bucket}.
  Each chunk holds a Gorilla-compressed binary of samples for that hour.
  """

  @bucket_ms 3_600_000

  def ensure_table do
    case :ets.whereis(:chronos_chunks) do
      :undefined -> :ets.new(:chronos_chunks, [:named_table, :public, :set])
      _ -> :ok
    end
  end

  @doc "Returns the hour bucket for a given timestamp in milliseconds."
  @spec bucket_for(integer()) :: integer()
  def bucket_for(ts_ms), do: div(ts_ms, @bucket_ms) * @bucket_ms

  @doc "Appends a sample to the chunk for the given series and timestamp."
  @spec append(non_neg_integer(), integer(), float()) :: :ok
  def append(series_id, ts_ms, value) do
    ensure_table()
    bucket = bucket_for(ts_ms)
    key = {series_id, bucket}

    existing =
      case :ets.lookup(:chronos_chunks, key) do
        [{^key, samples}] -> samples
        [] -> []
      end

    :ets.insert(:chronos_chunks, {key, existing ++ [{ts_ms, value}]})
    :ok
  end

  @doc "Reads and returns raw samples for a series within the given time range."
  @spec read(non_neg_integer(), integer(), integer()) :: [{integer(), float()}]
  def read(series_id, from_ms, to_ms) do
    ensure_table()
    start_bucket = bucket_for(from_ms)
    end_bucket = bucket_for(to_ms)

    buckets = Stream.iterate(start_bucket, &(&1 + @bucket_ms))
              |> Enum.take_while(&(&1 <= end_bucket))

    Enum.flat_map(buckets, fn bucket ->
      key = {series_id, bucket}
      case :ets.lookup(:chronos_chunks, key) do
        [{^key, samples}] ->
          Enum.filter(samples, fn {ts, _v} -> ts >= from_ms and ts < to_ms end)
        [] -> []
      end
    end)
  end
end
```

```elixir
# lib/chronos/query_engine.ex
defmodule Chronos.QueryEngine do
  @moduledoc """
  Executes range queries with aggregation and gap-fill support.
  """

  @doc "Queries a metric with label filter, time range, aggregation, step, and optional gap-fill."
  @spec query(String.t(), map(), keyword()) :: [{integer(), float()}]
  def query(metric, labels, opts) do
    from = Keyword.fetch!(opts, :from)
    to = Keyword.fetch!(opts, :to)
    aggregate = Keyword.get(opts, :aggregate, :avg)
    step_str = Keyword.get(opts, :step, "1m")
    gap_fill = Keyword.get(opts, :gap_fill)

    step_ms = parse_step(step_str)

    series_ids = Chronos.Series.lookup_by_labels(labels)

    all_samples =
      Enum.flat_map(series_ids, fn sid ->
        Chronos.Chunk.read(sid, from, to)
      end)
      |> Enum.sort_by(fn {ts, _v} -> ts end)

    windows = build_windows(from, to, step_ms)

    results =
      Enum.map(windows, fn {win_start, win_end} ->
        window_samples =
          Enum.filter(all_samples, fn {ts, _v} -> ts >= win_start and ts < win_end end)
          |> Enum.map(fn {_ts, v} -> v end)

        agg_value =
          case window_samples do
            [] -> nil
            vals -> aggregate_values(vals, aggregate)
          end

        {win_start, agg_value}
      end)

    case gap_fill do
      :fill_previous -> fill_previous(results)
      _ -> Enum.reject(results, fn {_ts, v} -> is_nil(v) end)
    end
  end

  defp build_windows(from, to, step_ms) do
    Stream.iterate(from, &(&1 + step_ms))
    |> Enum.take_while(&(&1 < to))
    |> Enum.map(fn start -> {start, start + step_ms} end)
  end

  defp aggregate_values(values, :avg), do: Enum.sum(values) / length(values)
  defp aggregate_values(values, :sum), do: Enum.sum(values)
  defp aggregate_values(values, :min), do: Enum.min(values)
  defp aggregate_values(values, :max), do: Enum.max(values)
  defp aggregate_values(values, :last), do: List.last(values)
  defp aggregate_values(values, :count), do: length(values) * 1.0

  defp fill_previous(results) do
    {filled, _} =
      Enum.map_reduce(results, nil, fn {ts, val}, prev ->
        if is_nil(val) do
          {{ts, prev}, prev}
        else
          {{ts, val}, val}
        end
      end)

    Enum.reject(filled, fn {_ts, v} -> is_nil(v) end)
  end

  defp parse_step("1m"), do: 60_000
  defp parse_step("5m"), do: 300_000
  defp parse_step("15m"), do: 900_000
  defp parse_step("1h"), do: 3_600_000
  defp parse_step(str) do
    cond do
      String.ends_with?(str, "m") ->
        str |> String.trim_trailing("m") |> String.to_integer() |> Kernel.*(60_000)
      String.ends_with?(str, "h") ->
        str |> String.trim_trailing("h") |> String.to_integer() |> Kernel.*(3_600_000)
      true ->
        String.to_integer(str)
    end
  end
end
```

### Step 6: Database — public API

**Objective**: Front ingest and range-aggregate behind one GenServer so callers never juggle series_ids, chunk buckets, or Gorilla buffers.


```elixir
# lib/chronos/database.ex
defmodule Chronos.Database do
  use GenServer

  @moduledoc """
  Public API for the time-series database.
  """

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts)
  end

  @impl true
  def init(_opts) do
    Chronos.Chunk.ensure_table()
    {:ok, %{}}
  end
end

defmodule Chronos do
  @moduledoc "Top-level convenience API for the time-series database."

  @doc "Ingests a single data point."
  @spec ingest(GenServer.server(), String.t(), map(), float(), integer()) :: :ok
  def ingest(_db, metric, labels, value, timestamp_ms) do
    sid = Chronos.Series.series_id(metric, labels)
    Chronos.Series.index_labels(sid, labels)
    Chronos.Chunk.append(sid, timestamp_ms, value)
    :ok
  end

  @doc "Queries a metric with filters and aggregation."
  @spec query(GenServer.server(), String.t(), map(), keyword()) :: [{integer(), float()}]
  def query(_db, metric, labels, opts) do
    Chronos.QueryEngine.query(metric, labels, opts)
  end
end
```

### Step 7: Given tests — must pass without modification

**Objective**: Pin Gorilla compression ratios, decode round-trips, and gap-fill semantics so encoder tuning cannot silently lose precision or windows.


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

### Step 8: Run the tests

**Objective**: Run `--trace` so any drift in bit-packing alignment surfaces as a visible outlier instead of a float-compare flake.


```bash
mix test test/chronos/ --trace
```

### Step 9: Benchmark

**Objective**: Drive high-cardinality ingest through Benchee so label-index and chunk-append costs are exposed before compression hides the per-sample overhead.


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

### Why this works

Incoming points land in an in-memory head chunk; when it closes by size or time, it's compressed and written as a columnar segment. Queries narrow by timestamp range first (chunk index), then scan compressed columns, so I/O is proportional to the query window not the total data.

---
## Main Entry Point

```elixir
def main do
  IO.puts("======== 18-build-time-series-database ========")
  IO.puts("Build Time Series Database")
  IO.puts("")
  
  Chronos.Gorilla.start_link([])
  IO.puts("Chronos.Gorilla started")
  
  IO.puts("Run: mix test")
end
```

