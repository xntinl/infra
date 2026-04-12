# NimbleCSV for Parsing Multi-GB CSV Files

**Project**: `billing_importer` — parses vendor-supplied `usage.csv` files (5 GB – 80 GB) containing billing events, with quoted fields, embedded commas, and non-UTF8 garbage in a tiny fraction of rows.

## Project context

The telco vendor delivers monthly usage exports as CSV. Files are 80 GB with
roughly 400 million rows. Each row contains `msisdn, event_ts, service, bytes, cost_usd`.
Some rows have fields wrapped in double quotes with embedded commas (addresses,
service descriptions). A small fraction (<0.01%) contain invalid UTF-8 from
legacy systems.

Constraints:

- Must parse in <30 minutes on a single 8-core host.
- Must not OOM.
- Must skip malformed rows and report them (row number + raw content) without
  failing the whole import.

`NimbleCSV` is the fastest pure-Elixir CSV parser. It precompiles the dialect
(delimiters, escapes) into a single binary pattern match, processing ~200 MB/s
on a single core — roughly 5× faster than `CSV` or `ex_csv`.

```
billing_importer/
├── lib/
│   └── billing_importer/
│       ├── application.ex
│       ├── parser.ex              # NimbleCSV dialect module
│       └── importer.ex            # stream pipeline
├── test/
│   └── billing_importer/
│       ├── parser_test.exs
│       └── importer_test.exs
├── bench/
│   └── parse_bench.exs
└── mix.exs
```

## Why NimbleCSV and not CSV / ex_csv

- **CSV** (the hex package): pure Elixir, correct, but ~5× slower because it
  uses `String.splitter/3` and per-row allocations.
- **ex_csv**: older, slower still, and unmaintained.
- **CSV via `:erl_csv` NIF**: fast but adds a C dependency and a NIF crash
  takes down the BEAM.
- **`NimbleCSV`**: precompiled pattern-matching dialect in pure Elixir. No NIF.
  The `NimbleCSV.RFC4180` dialect handles standard CSV; custom dialects take
  3 lines.

The only time you'd pick something else is if your CSV is actually TSV or
custom-delimited and you want to avoid the `NimbleCSV.define/2` call
(you shouldn't — it's trivial).

## Core concepts

### 1. Precompiled dialect

```elixir
NimbleCSV.define(BillingImporter.Parser, separator: ",", escape: "\"")
```

This generates a module at compile time containing `parse_string/2`,
`parse_stream/2`, and `parse_enumerable/2`. The parser is a pattern-match
over the literal `,` and `"` bytes — no dynamic dispatch.

### 2. `parse_stream/2` vs `parse_string/2`

- `parse_string/2` takes a whole binary. OK for small payloads, OOMs on big files.
- `parse_stream/2` takes an `Enumerable` of chunks (line-based or byte-based),
  handles partial rows across chunks, and returns a lazy stream of row lists.

### 3. Header handling

`skip_headers: true` (default) drops the first row. To use headers for
column-name access, pair with `Stream.map/2`:

```elixir
[headers | rest] = ...
rest |> Stream.map(&Enum.zip(headers, &1) |> Map.new())
```

## Design decisions

- **Option A — Custom regex parser**:
  - Pros: full control.
  - Cons: slow, error-prone with quoting edge cases.
- **Option B — NimbleCSV + `File.stream!/3` line mode**:
  - Pros: fast, simple, handles quoting.
  - Cons: line mode breaks on embedded newlines inside quoted fields.
- **Option C — NimbleCSV + `File.stream!/3` byte mode + `parse_stream/2`**:
  - Pros: correctly handles embedded newlines, fast, streaming.
  - Cons: slightly more setup.

Chosen: **Option C**. Embedded newlines (e.g. in `"123 Main St,\nApt 4"`) are
real in vendor data — line splitting before CSV parsing corrupts those rows.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule BillingImporter.MixProject do
  use Mix.Project

  def project do
    [
      app: :billing_importer,
      version: "0.1.0",
      elixir: "~> 1.16",
      deps: deps()
    ]
  end

  def application, do: [extra_applications: [:logger]]

  defp deps do
    [
      {:nimble_csv, "~> 1.2"},
      {:benchee, "~> 1.3", only: :dev}
    ]
  end
end
```

### Step 1: Define the dialect

```elixir
defmodule BillingImporter.Parser do
  # Standard RFC4180-ish: comma separator, double-quote escape, CRLF line endings.
  NimbleCSV.define(__MODULE__, separator: ",", escape: "\"", line_separator: "\n")
end
```

### Step 2: Row validator

```elixir
defmodule BillingImporter.Row do
  @moduledoc """
  Typed representation of a billing row. Invalid rows return {:error, reason}.
  """

  defstruct [:msisdn, :event_ts, :service, :bytes, :cost_cents]

  @type t :: %__MODULE__{
          msisdn: String.t(),
          event_ts: DateTime.t(),
          service: String.t(),
          bytes: non_neg_integer(),
          cost_cents: non_neg_integer()
        }

  @spec from_row([String.t()]) :: {:ok, t()} | {:error, atom()}
  def from_row([msisdn, ts_s, service, bytes_s, cost_s]) do
    with {:ts, {:ok, ts, _}} <- {:ts, DateTime.from_iso8601(ts_s)},
         {:b, {bytes, ""}} <- {:b, Integer.parse(bytes_s)},
         {:c, {cost_f, ""}} <- {:c, Float.parse(cost_s)},
         true <- String.match?(msisdn, ~r/^\+?\d{8,15}$/) do
      {:ok,
       %__MODULE__{
         msisdn: msisdn,
         event_ts: ts,
         service: service,
         bytes: bytes,
         cost_cents: round(cost_f * 100)
       }}
    else
      {:ts, _} -> {:error, :bad_timestamp}
      {:b, _} -> {:error, :bad_bytes}
      {:c, _} -> {:error, :bad_cost}
      false -> {:error, :bad_msisdn}
    end
  end

  def from_row(_), do: {:error, :wrong_column_count}
end
```

### Step 3: Importer pipeline

```elixir
defmodule BillingImporter do
  alias BillingImporter.{Parser, Row}

  @doc """
  Reads the CSV, yields {:ok, %Row{}} or {:error, {row_number, reason, raw}}.

  Lazy — the file is not loaded into memory. Memory stays bounded.
  """
  @spec stream(Path.t()) :: Enumerable.t()
  def stream(path) do
    path
    |> File.stream!([:raw, :read_ahead], 128 * 1024)
    |> Parser.parse_stream(skip_headers: true)
    |> Stream.with_index(1)
    |> Stream.map(fn {row, row_num} ->
      case Row.from_row(row) do
        {:ok, r} -> {:ok, r}
        {:error, reason} -> {:error, {row_num, reason, row}}
      end
    end)
  end

  @doc """
  Imports `path`, inserting valid rows and collecting errors.

  Returns {inserted_count, errors}. Errors are capped to avoid unbounded memory.
  """
  @spec import(Path.t(), keyword()) :: {non_neg_integer(), [tuple()]}
  def import(path, opts \\ []) do
    error_cap = Keyword.get(opts, :error_cap, 1_000)
    batch_size = Keyword.get(opts, :batch_size, 5_000)

    path
    |> stream()
    |> Stream.chunk_every(batch_size)
    |> Enum.reduce({0, []}, fn batch, {n, errs} ->
      {ok, bad} = Enum.split_with(batch, &match?({:ok, _}, &1))
      :ok = persist(Enum.map(ok, fn {:ok, r} -> r end))

      new_errs =
        bad
        |> Enum.take(max(0, error_cap - length(errs)))
        |> Enum.map(fn {:error, e} -> e end)

      {n + length(ok), errs ++ new_errs}
    end)
  end

  # Replace with `Repo.insert_all(Usage, rows, on_conflict: :nothing)`.
  defp persist(rows) do
    :telemetry.execute([:billing, :persist], %{count: length(rows)}, %{})
    :ok
  end
end
```

## Why this works

- `File.stream!/3` with `[:raw, :read_ahead]` and 128 KB chunks gives maximum
  disk throughput.
- `Parser.parse_stream/2` is NimbleCSV's chunk-aware parser. It keeps a small
  internal buffer to handle rows that straddle chunk boundaries and rows with
  embedded newlines inside quoted fields.
- `Row.from_row/1` is strict — any parse failure surfaces as `{:error, reason}`
  with enough context (row number, raw row) to investigate in production.
- `Stream.chunk_every/2` groups validated rows into DB-insert-friendly batches.
- `error_cap` bounds memory: even a completely broken 80 GB file won't fill
  RAM with error tuples.

## Tests

```elixir
defmodule BillingImporter.ParserTest do
  use ExUnit.Case, async: true

  alias BillingImporter.Parser

  describe "parse_string/2" do
    test "parses a simple row" do
      csv = "msisdn,event_ts,service,bytes,cost\n+441234567,2024-10-10T13:55:36Z,data,1024,0.05\n"
      [row] = Parser.parse_string(csv)
      assert row == ["+441234567", "2024-10-10T13:55:36Z", "data", "1024", "0.05"]
    end

    test "handles quoted fields with embedded commas" do
      csv =
        ~s(msisdn,event_ts,service,bytes,cost\n+44,2024-10-10T13:55:36Z,"sms,bulk",100,0.01\n)

      [row] = Parser.parse_string(csv)
      assert Enum.at(row, 2) == "sms,bulk"
    end

    test "handles embedded newlines inside quotes" do
      csv = ~s(a,b,c,d,e\n"one\ntwo",2024-10-10T00:00:00Z,svc,1,1.0\n)
      [row] = Parser.parse_string(csv)
      assert List.first(row) == "one\ntwo"
    end
  end
end

defmodule BillingImporter.RowTest do
  use ExUnit.Case, async: true

  alias BillingImporter.Row

  describe "from_row/1" do
    test "returns {:ok, %Row{}} for a valid row" do
      row = ["+441234567", "2024-10-10T13:55:36Z", "data", "1024", "0.05"]
      assert {:ok, %Row{bytes: 1024, cost_cents: 5}} = Row.from_row(row)
    end

    test "rejects a malformed msisdn" do
      row = ["not-a-number", "2024-10-10T13:55:36Z", "data", "1024", "0.05"]
      assert {:error, :bad_msisdn} = Row.from_row(row)
    end

    test "rejects rows with the wrong column count" do
      assert {:error, :wrong_column_count} = Row.from_row(["a", "b"])
    end
  end
end

defmodule BillingImporterTest do
  use ExUnit.Case, async: true

  setup do
    path = Path.join(System.tmp_dir!(), "usage_#{:erlang.unique_integer()}.csv")

    File.write!(path, """
    msisdn,event_ts,service,bytes,cost
    +441234567,2024-10-10T13:55:36Z,data,1024,0.05
    +441234568,2024-10-10T13:55:37Z,"sms,bulk",0,0.01
    garbage,2024-10-10T13:55:37Z,data,1024,0.05
    """)

    on_exit(fn -> File.rm(path) end)
    {:ok, path: path}
  end

  test "imports valid rows and captures errors", %{path: path} do
    {inserted, errors} = BillingImporter.import(path, batch_size: 100)
    assert inserted == 2
    assert length(errors) == 1
    assert match?({3, :bad_msisdn, _}, List.first(errors))
  end
end
```

## Benchmark

```elixir
# bench/parse_bench.exs
# Generates a 1 GB CSV and measures parse throughput.

path = Path.join(System.tmp_dir!(), "bench_1g.csv")

unless File.exists?(path) do
  {:ok, io} = File.open(path, [:write, :raw, :delayed_write])
  IO.write(io, "msisdn,event_ts,service,bytes,cost\n")

  for i <- 1..10_000_000 do
    line =
      "+44#{1_000_000_000 + i},2024-10-10T13:55:36Z,data,#{rem(i, 10_000)},#{:rand.uniform_real() * 10}\n"

    IO.write(io, line)
  end

  File.close(io)
end

Benchee.run(%{
  "NimbleCSV parse_stream" => fn ->
    path
    |> BillingImporter.stream()
    |> Enum.reduce(0, fn _, acc -> acc + 1 end)
  end
}, time: 15, warmup: 3)
```

**Target**: ~200 MB/s sustained on a single core. For a 1 GB file that is
~5 seconds of parse time; adding `Flow` on top for validation/enrichment
can bring total import time to <30 s for 10 M rows.

## Trade-offs and production gotchas

**1. `parse_string/2` on a multi-GB file OOMs.**
Always use `parse_stream/2` for anything larger than your RAM can comfortably
hold. `parse_string/2` is for small, in-memory payloads only.

**2. Non-UTF8 bytes in the middle of a quoted field crash NimbleCSV.**
If the vendor gives you Latin-1 or garbage, pre-filter with
`:unicode.characters_to_binary(chunk, :latin1, :utf8)` at the
`File.stream!` output before it hits the parser, or wrap `parse_stream/2`
in a try/rescue and skip offending rows.

**3. BOM at the start of the file.**
Microsoft-authored CSVs often start with `\uFEFF` (UTF-8 BOM). That byte
becomes part of the first column name. Strip it explicitly.

**4. Windows CRLF line endings.**
The default dialect uses `\n`. If files have CRLF, configure the dialect
with `line_separator: "\r\n"` or strip CR with `String.trim_trailing/2`
post-parse.

**5. `error_cap` is a ceiling, not a target.**
If you hit `error_cap` the import continues but silently discards further
errors. Log clearly when the cap is hit — 1001 errors "by design" in a
production run is usually a data-quality red flag worth escalating.

**6. When NOT to use NimbleCSV.**
If your data has arbitrary delimiters or non-CSV format (e.g. fixed-width,
JSON lines, Avro), use the appropriate parser. Don't bend NimbleCSV dialects
to parse something that isn't CSV.

## Reflection

You migrate the pipeline to run under `Flow` with 8 stages. The parser is no
longer the bottleneck. Your DB insert throughput is 40k rows/sec but you're
measuring end-to-end 25k rows/sec. Top shows one BEAM thread pinned at 100%
while seven sit at 30%. What is most likely the serial phase, and how do you
confirm it with `:observer` or `:recon`?

## Resources

- [NimbleCSV — hexdocs](https://hexdocs.pm/nimble_csv/NimbleCSV.html)
- [NimbleCSV source — GitHub](https://github.com/dashbitco/nimble_csv)
- [RFC 4180 — CSV format](https://datatracker.ietf.org/doc/html/rfc4180)
- [Benchee](https://github.com/bencheeorg/benchee)
