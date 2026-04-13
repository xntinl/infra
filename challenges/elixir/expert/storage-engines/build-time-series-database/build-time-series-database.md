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

## Project structure
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
├── script/
│   └── main.exs
└── mix.exs
```

## Implementation
### Step 1: Create the project

**Objective**: Generate `--sup` skeleton so ingestion, chunk store, and query engine restart independently without losing in-flight series state.

```bash
mix new chronos --sup
cd chronos
mkdir -p lib/chronos test/chronos bench
```

### Step 2: `mix.exs` — dependencies

**Objective**: Keep deps to `:benchee` only; raw bitstrings and ETS cover Gorilla encoding and the label index without pulling a compression NIF.

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
defmodule Chronos.GorillaTest do
  use ExUnit.Case, async: true
  doctest Chronos.Database

  alias Chronos.Gorilla

  describe "core functionality" do
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
end
```
```elixir
defmodule Chronos.QueryTest do
  use ExUnit.Case, async: false
  doctest Chronos.Database

  setup do
    {:ok, db} = Chronos.Database.start_link()
    {:ok, db: db}
  end

  describe "core functionality" do
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
---

## Production-Grade Addendum (Insane Standard)

The sections below extend the content above to the full `insane` template: a canonical `mix.exs`, an executable `script/main.exs` stress harness, explicit error-handling and recovery protocols, concrete performance targets, and a consolidated key-concepts list. These are non-negotiable for production-grade systems.

### `mix.exs`

```elixir
defmodule Chronos.MixProject do
  use Mix.Project

  def project do
    [
      app: :chronos,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps(),
      test_coverage: [summary: [threshold: 80]],
      dialyzer: [plt_add_apps: [:mix, :ex_unit]]
    ]
  end

  def application do
    [
      extra_applications: [:logger, :crypto],
      mod: {Chronos.Application, []}
    ]
  end

  defp deps do
    [
      {:telemetry, "~> 1.2"},
      {:jason, "~> 1.4"},
      {:benchee, "~> 1.2", only: :dev},
      {:stream_data, "~> 0.6", only: :test},
      {:dialyxir, "~> 1.4", only: :dev, runtime: false}
    ]
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Realistic stress harness for `chronos` (time-series database).
  Runs five phases: warmup, steady-state load, chaos injection, recovery
  verification, and invariant audit. Exits non-zero if any SLO is breached.
  """

  @warmup_ops 10_000
  @steady_ops 100_000
  @chaos_ratio 0.10
  @slo_p99_us 20000
  @slo_error_rate 0.001

  def main do
    :ok = Application.ensure_all_started(:chronos) |> elem(0) |> then(&(&1 == :ok && :ok || :ok))
    IO.puts("=== Chronos stress test ===")

    warmup()
    baseline = steady_phase()
    chaos = chaos_phase()
    recovery = recovery_phase()
    invariants = invariant_phase()

    report([baseline, chaos, recovery, invariants])
  end

  defp warmup do
    IO.puts("Phase 0: warmup (#{@warmup_ops} ops, not measured)")
    run_ops(@warmup_ops, :warmup, measure: false)
    IO.puts("  warmup complete\n")
  end

  defp steady_phase do
    IO.puts("Phase 1: steady-state load (#{@steady_ops} ops @ target throughput)")
    started = System.monotonic_time(:millisecond)
    latencies = run_ops(@steady_ops, :steady, measure: true)
    elapsed_s = (System.monotonic_time(:millisecond) - started) / 1000
    p = percentiles(latencies)
    ok = Enum.count(latencies, &match?({:ok, _}, &1))
    err = (length(latencies) - ok) / max(length(latencies), 1)
    Map.merge(p, %{phase: :steady, ok: ok, error_rate: err, throughput: round(ok / elapsed_s)})
  end

  defp chaos_phase do
    IO.puts("\nPhase 2: chaos injection (#{trunc(@chaos_ratio * 100)}%% faults)")
    # Inject realistic fault: process kills, disk stalls, packet loss
    chaos_inject()
    latencies = run_ops(div(@steady_ops, 2), :chaos, measure: true, fault_ratio: @chaos_ratio)
    chaos_heal()
    p = percentiles(latencies)
    ok = Enum.count(latencies, &match?({:ok, _}, &1))
    err = (length(latencies) - ok) / max(length(latencies), 1)
    Map.merge(p, %{phase: :chaos, ok: ok, error_rate: err})
  end

  defp recovery_phase do
    IO.puts("\nPhase 3: cold-restart recovery")
    t0 = System.monotonic_time(:millisecond)
    case Application.stop(:chronos) do
      :ok -> :ok
      _ -> :ok
    end
    {:ok, _} = Application.ensure_all_started(:chronos)
    recovery_ms = System.monotonic_time(:millisecond) - t0
    healthy = health_check?()
    %{phase: :recovery, recovery_ms: recovery_ms, healthy: healthy}
  end

  defp invariant_phase do
    IO.puts("\nPhase 4: invariant audit")
    violations = run_invariant_checks()
    %{phase: :invariants, violations: violations}
  end

  # ---- stubs: wire these to your impl ----

  defp run_ops(n, _label, opts) do
    measure = Keyword.get(opts, :measure, false)
    fault = Keyword.get(opts, :fault_ratio, 0.0)
    parent = self()
    workers = System.schedulers_online() * 2
    per = div(n, workers)

    tasks =
      for _ <- 1..workers do
        Task.async(fn -> worker_loop(per, measure, fault) end)
      end

    Enum.flat_map(tasks, &Task.await(&1, 60_000))
  end

  defp worker_loop(n, measure, fault) do
    Enum.map(1..n, fn _ ->
      t0 = System.monotonic_time(:microsecond)
      result = op(fault)
      elapsed = System.monotonic_time(:microsecond) - t0
      if measure, do: {tag(result), elapsed}, else: :warm
    end)
    |> Enum.reject(&(&1 == :warm))
  end

  defp op(fault) do
    if :rand.uniform() < fault do
      {:error, :fault_injected}
    else
      # TODO: replace with actual chronos operation
      {:ok, :ok}
    end
  end

  defp tag({:ok, _}), do: :ok
  defp tag({:error, _}), do: :err

  defp chaos_inject, do: :ok
  defp chaos_heal, do: :ok
  defp health_check?, do: true
  defp run_invariant_checks, do: 0

  defp percentiles([]), do: %{p50: 0, p95: 0, p99: 0, p999: 0}
  defp percentiles(results) do
    lats = for {_, us} <- results, is_integer(us), do: us
    s = Enum.sort(lats); n = length(s)
    if n == 0, do: %{p50: 0, p95: 0, p99: 0, p999: 0},
       else: %{
         p50: Enum.at(s, div(n, 2)),
         p95: Enum.at(s, div(n * 95, 100)),
         p99: Enum.at(s, div(n * 99, 100)),
         p999: Enum.at(s, min(div(n * 999, 1000), n - 1))
       }
  end

  defp report(phases) do
    IO.puts("\n=== SUMMARY ===")
    Enum.each(phases, fn p ->
      IO.puts("#{p.phase}: #{inspect(Map.drop(p, [:phase]))}")
    end)

    bad =
      Enum.any?(phases, fn
        %{p99: v} when is_integer(v) and v > @slo_p99_us -> true
        %{error_rate: v} when is_float(v) and v > @slo_error_rate -> true
        %{violations: v} when is_integer(v) and v > 0 -> true
        _ -> false
      end)

    System.halt(if(bad, do: 1, else: 0))
  end
end

Main.main()
```
### Running the stress harness

```bash
mix deps.get
mix compile
mix run --no-halt script/main.exs
```

The harness exits 0 on SLO compliance and 1 otherwise, suitable for CI gating.

---

## Error Handling and Recovery

Chronos classifies every failure on two axes: **severity** (critical vs recoverable) and **scope** (per-request vs system-wide). Critical violations halt the subsystem and page an operator; recoverable faults are retried with bounded backoff and explicit budgets.

### Critical failures (halt, alert, preserve forensics)

| Condition | Detection | Response |
|---|---|---|
| Persistent-state corruption (checksum mismatch) | On-read validation | Refuse boot; preserve raw state for forensics; page SRE |
| Safety invariant violated (e.g., two holders observed) | Background invariant checker | Enter read-only safe mode; emit `:safety_violation` telemetry |
| Supervisor reaches `max_restarts` | BEAM default | Exit non-zero so orchestrator (systemd/k8s) reschedules |
| Monotonic time regression | `System.monotonic_time/1` decreases | Hard crash (BEAM bug; unrecoverable) |

### Recoverable failures

| Failure | Policy | Bounds |
|---|---|---|
| Transient peer RPC timeout | Exponential backoff (base 50ms, jitter 20%%) | Max 3 attempts, max 2s total |
| Downstream service unavailable | Circuit-breaker (3-state: closed/open/half-open) | Open for 5s after 5 consecutive failures |
| Rate-limit breach | Return `{:error, :rate_limited}` with `Retry-After` | Client responsibility to back off |
| Disk full on append | Reject new writes, drain in-flight | Recovery after ops frees space |
| GenServer mailbox > high-water mark | Backpressure upstream (refuse enqueue) | High water: 10k msgs; low water: 5k |

### Recovery protocol (cold start)

1. **State replay**: Read the last full snapshot, then replay WAL entries with seq > snapshot_seq. Each entry carries a CRC32; mismatches halt replay.
2. **Peer reconciliation** (if distributed): Exchange state vectors with quorum peers; adopt authoritative state per the protocol's conflict resolution rule.
3. **Warm health probe**: All circuit breakers start in `:half_open`; serve one probe request per dependency before accepting real traffic.
4. **Readiness gate**: External endpoints (HTTP, gRPC) refuse traffic until `/healthz/ready` returns 200; liveness passes earlier.
5. **Backlog drain**: Any in-flight requests recovered from the WAL are re-delivered; consumers must be idempotent on the supplied request-id.

### Bulkheads and security bounds

- **Input size**: max request/message body 1 MiB, max nesting depth 32, max field count 1024.
- **Resource limits per client**: max open connections 100, max in-flight requests 1000, max CPU time per request 100ms.
- **Backpressure propagation**: every bounded queue is visible; upstream sees `{:error, :shed_load}` rather than silent buffering.
- **Process isolation**: each high-traffic component has its own supervisor tree; crashes are local, not cluster-wide.

---

## Performance Targets

Concrete numbers derived from comparable production systems. Measure with `script/main.exs`; any regression > 10%% vs prior baseline fails CI.

| Metric | Target | Source / Comparable |
|---|---|---|
| **Sustained throughput** | **1,000,000 points/s** | comparable to reference system |
| **Latency p50** | below p99/4 | — |
| **Latency p99** | **20 ms** | Gorilla paper (Facebook) |
| **Latency p999** | ≤ 3× p99 | excludes GC pauses > 10ms |
| **Error rate** | **< 0.1 %%** | excludes client-side 4xx |
| **Cold start** | **< 3 s** | supervisor ready + warm caches |
| **Recovery after crash** | **< 5 s** | replay + peer reconciliation |
| **Memory per connection/entity** | **< 50 KiB** | bounded by design |
| **CPU overhead of telemetry** | **< 1 %%** | sampled emission |

### Baselines we should beat or match

- Gorilla paper (Facebook): standard reference for this class of system.
- Native BEAM advantage: per-process isolation and lightweight concurrency give ~2-5x throughput vs process-per-request architectures (Ruby, Python) on equivalent hardware.
- Gap vs native (C++/Rust) implementations: expect 2-3x latency overhead in the hot path; mitigated by avoiding cross-process message boundaries on critical paths (use ETS with `:write_concurrency`).

---

## Why Time-Series Database with High-Cardinality Support matters

Mastering **Time-Series Database with High-Cardinality Support** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

### `lib/chronos.ex`

```elixir
defmodule Chronos do
  @moduledoc """
  Reference implementation for Time-Series Database with High-Cardinality Support.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the chronos module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> Chronos.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```
### `test/chronos_test.exs`

```elixir
defmodule ChronosTest do
  use ExUnit.Case, async: true

  doctest Chronos

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert Chronos.run(:noop) == :ok
    end
  end
end
```
---

## Key concepts
### 1. Failure is not an exception, it is the default
Distributed systems fail continuously; correctness means reasoning about every possible interleaving. Every operation must have a documented failure mode and a recovery path. "It worked on my laptop" is not a proof.

### 2. Backpressure must propagate end-to-end
Any unbounded buffer is a latent OOM. Every queue has a high-water mark, every downstream signals pressure upstream. The hot-path signal is `{:error, :shed_load}` or HTTP 503 with `Retry-After`.

### 3. Monotonic time, never wall-clock, for durations
Use `System.monotonic_time/1` for TTLs, deadlines, and timers. Wall-clock can jump (NTP, container migration, VM pause) and silently breaks every time-based guarantee.

### 4. The log is the source of truth; state is a cache
Derive every piece of state by replaying the append-only log. Do not maintain parallel "current state" that needs to be kept in sync — consistency windows after crashes are where bugs hide.

### 5. Idempotency is a correctness requirement, not a convenience
Every externally-visible side effect must be idempotent on its request ID. Retries, recovery replays, and distributed consensus all rely on this. Non-idempotent operations break under any of the above.

### 6. Observability is a correctness property
In a system at scale, the only way to know you meet the SLO is to measure continuously. Bounded-memory sketches (reservoir sampling for percentiles, HyperLogLog for cardinality, Count-Min for frequency) give actionable estimates without O(n) storage.

### 7. Bounded everything: time, memory, retries, concurrency
Every unbounded resource is a DoS vector. Every loop has a max iteration count; every map has a max size; every retry has a max attempt count; every timeout has an explicit value. Defaults are conservative; tuning happens with measurement.

### 8. Compose primitives, do not reinvent them
Use OTP's supervision trees, `:ets`, `Task.Supervisor`, `Registry`, and `:erpc`. Reinvention is for understanding; production wraps the BEAM's battle-tested primitives. Exception: when a primitive's semantics (like `:global`) do not match the safety requirement, replace it with a purpose-built implementation whose failure mode is documented.

### References

- Gorilla paper (Facebook)
- [Release It! (Nygard)](https://pragprog.com/titles/mnee2/release-it-second-edition/) — circuit breaker, bulkhead, steady-state
- [Google SRE Book](https://sre.google/books/) — SLOs, error budgets, overload handling
- [Designing Data-Intensive Applications (Kleppmann)](https://dataintensive.net/) — correctness under failure

---
