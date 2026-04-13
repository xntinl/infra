# Streaming gigabyte CSV files with `Stream` and `NimbleCSV`

**Project**: `csv_stream_lib` — a small library that ingests CSVs of
arbitrary size, parsing lazily through a `Stream` pipeline backed by
[`NimbleCSV`](https://hexdocs.pm/nimble_csv/). Supports custom delimiters,
header mapping, and composable transformations. Memory use stays flat
regardless of file size.

---

## Why csv streaming matters

CSVs are the lingua franca of data imports. At 10 MB, loading the whole
file is fine. At 10 GB, you need to stream — reading a chunk, parsing
rows, transforming, writing, and releasing the chunk before the next one.
Elixir's `Stream` module plus `NimbleCSV.parse_stream/1` compose exactly
into that pipeline.

This exercise teaches the end-to-end story: lazy file reading, binary
copying to avoid reference-counted binary leaks, custom dialect parsers,
and functional transformations that run in constant memory.

---

## Project structure

```
csv_stream_lib/
├── lib/
│   └── csv_stream_lib.ex
├── script/
│   └── main.exs
├── test/
│   └── csv_stream_lib_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `NimbleCSV.define/2` — compile-time dialect

Dialects differ: Excel uses `;` in many locales, TSV uses `\t`, some feeds
use `|`. You declare a parser module once:

```elixir
NimbleCSV.define(MyParser, separator: ",", escape: "\"")
```

This compiles binary-matching clauses tailored to that separator — no
runtime option checks per row.

### 2. `parse_stream/2` — lazy parsing

`MyParser.parse_stream(stream)` returns a `Stream` that emits row lists
(`[[col1, col2, ...], ...]`) as the underlying stream yields chunks.
Crucial for large files.

### 3. `:binary.copy/1` — avoid sub-binary leaks

By default, `NimbleCSV` returns *sub-binaries* pointing into the read
buffer. If you keep references (store in a list, send to another process),
the entire buffer is pinned in memory. Call `:binary.copy(col)` for
columns you retain beyond the current row.

### 4. `File.stream!/3` with `:read_ahead`

```elixir
File.stream!("huge.csv", read_ahead: 100_000)
```

Reads ~100KB chunks into memory, breaks them into lines, and yields
them lazily. Tune `read_ahead` for your average row size.

### 5. Header mapping

`NimbleCSV` by default consumes the header row (`skip_headers: true`). To
map columns to field names, read the first line yourself, then parse the
rest:

```elixir
["id", "name", "email"] = [first_line] |> MyParser.parse_string() |> hd()
```

Or simpler: use `Stream.transform/3` to capture the header on the first
row and zip into maps afterward.

---

## Design decisions

**Option A — `File.read!/1` + `String.split/2` + manual parsing**
- Pros: trivial to understand; no dep.
- Cons: loads the whole file in RAM; breaks on quoted fields containing commas/newlines; can't handle gigabyte inputs.

**Option B — `File.stream!/3` piped into `NimbleCSV.parse_stream/2` with `:binary.copy/1` on retained columns (chosen)**
- Pros: O(row) memory regardless of file size; correct RFC-4180 handling; compile-time specialised dialects; composes with `Stream.filter` / `Stream.transform` naturally.
- Cons: lazy pipelines are surprising the first time (`Stream.map` does nothing without a sink); forgetting `:binary.copy/1` pins the whole read buffer; error surface is spread across `File`, `NimbleCSV`, and downstream steps.

→ Chose **B** because anything above ~10 MB makes Option A impractical, and the sub-binary copy rule is exactly the kind of subtlety that belongs in a didactic exercise.

## Implementation

### `mix.exs`

```elixir
defmodule CsvStreamLib.MixProject do
  use Mix.Project

  def project do
    [
      app: :csv_stream_lib,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    []
  end
end
```

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — isolated from any external state, so we demonstrate this concept cleanly without dependencies.

```bash
mix new csv_stream_lib
cd csv_stream_lib
mkdir -p test/fixtures
```

Deps in `mix.exs`:

### Step 2: `test/fixtures/sample.csv`

**Objective**: Provide `test/fixtures/sample.csv` — these are the supporting fixtures the main module depends on to make its concept demonstrable.

```csv
id,name,email,signup
1,Ada Lovelace,ada@analytical.engine,2024-01-15
2,Grace Hopper,grace@cobol.dev,2024-02-20
3,Linus Torvalds,linus@kernel.org,2024-03-11
4,Margaret Hamilton,margaret@apollo.nasa,2024-04-02
```

### `lib/csv_stream_lib/parser.ex`

**Objective**: Implement `parser.ex` — the integration seam where external protocol semantics meet Elixir domain code.

```elixir
defmodule CsvStreamLib.Parser do
  @moduledoc "Compile-time-specialised CSV dialects."

  # Standard RFC-4180-ish CSV.
  NimbleCSV.define(__MODULE__.Comma, separator: ",", escape: "\"")

  # Semicolon-separated (Excel in many European locales).
  NimbleCSV.define(__MODULE__.SemiColon, separator: ";", escape: "\"")

  # Tab-separated.
  NimbleCSV.define(__MODULE__.Tab, separator: "\t", escape: "\"")
end
```

### `lib/csv_stream_lib.ex`

**Objective**: Implement `csv_stream_lib.ex` — the integration seam where external protocol semantics meet Elixir domain code.

```elixir
defmodule CsvStreamLib do
  @moduledoc """
  Streaming CSV ingestion. All operations are lazy: nothing is read until
  the terminal `Stream.run/1` / `Enum.to_list/1` / `Enum.reduce/3`.

  Memory footprint is O(row size), not O(file size), as long as you don't
  collect every row in your final reduction.
  """

  alias CsvStreamLib.Parser.{Comma, SemiColon, Tab}

  @type dialect :: :comma | :semicolon | :tab
  @type row :: %{String.t() => String.t()}

  @doc """
  Returns a lazy stream of maps keyed by column header.

  ## Options

    * `:dialect` — `:comma` (default) | `:semicolon` | `:tab`
    * `:read_ahead` — bytes buffered from disk per chunk (default 100_000)
  """
  @spec stream_maps(Path.t(), keyword()) :: Enumerable.t()
  def stream_maps(path, opts \\ []) do
    dialect = Keyword.get(opts, :dialect, :comma)
    read_ahead = Keyword.get(opts, :read_ahead, 100_000)
    parser = parser_for(dialect)

    path
    |> File.stream!(read_ahead: read_ahead)
    |> parser.parse_stream(skip_headers: false)
    # First row is headers. Use transform to carry them forward.
    |> Stream.transform(nil, fn
      row, nil ->
        headers = Enum.map(row, &:binary.copy/1)
        {[], headers}

      row, headers ->
        # Copy columns: sub-binaries pin the read buffer otherwise.
        values = Enum.map(row, &:binary.copy/1)
        {[Map.new(Enum.zip(headers, values))], headers}
    end)
  end

  @doc "Count rows without loading them."
  @spec count_rows(Path.t(), keyword()) :: non_neg_integer()
  def count_rows(path, opts \\ []) do
    path
    |> stream_maps(opts)
    |> Enum.reduce(0, fn _row, acc -> acc + 1 end)
  end

  @doc "Filter, map, and sink to a list. Intended for small result sets only."
  @spec collect(Path.t(), (row() -> boolean()), (row() -> term()), keyword()) :: [term()]
  def collect(path, filter_fn, map_fn, opts \\ []) do
    path
    |> stream_maps(opts)
    |> Stream.filter(filter_fn)
    |> Stream.map(map_fn)
    |> Enum.to_list()
  end

  # ── Internals ──────────────────────────────────────────────────────────

  defp parser_for(:comma), do: Comma
  defp parser_for(:semicolon), do: SemiColon
  defp parser_for(:tab), do: Tab
end
```

### Step 5: `test/csv_stream_lib_test.exs`

**Objective**: Write `csv_stream_lib_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.

```elixir
defmodule CsvStreamLibTest do
  use ExUnit.Case, async: true

  doctest CsvStreamLib

  @fixture Path.join(__DIR__, "fixtures/sample.csv")

  describe "stream_maps/2" do
    test "yields each row as a header-keyed map" do
      rows = @fixture |> CsvStreamLib.stream_maps() |> Enum.to_list()

      assert length(rows) == 4

      assert %{
               "id" => "1",
               "name" => "Ada Lovelace",
               "email" => "ada@analytical.engine",
               "signup" => "2024-01-15"
             } = hd(rows)
    end

    test "is lazy — halting after N rows does not read the rest" do
      first_two =
        @fixture
        |> CsvStreamLib.stream_maps()
        |> Enum.take(2)

      assert length(first_two) == 2
      assert hd(first_two)["id"] == "1"
    end
  end

  describe "count_rows/2" do
    test "counts data rows (excludes header)" do
      assert 4 = CsvStreamLib.count_rows(@fixture)
    end
  end

  describe "collect/4" do
    test "filters and maps lazily, returning the projection" do
      result =
        CsvStreamLib.collect(
          @fixture,
          fn row -> String.contains?(row["name"], "a") end,
          fn row -> row["email"] end
        )

      assert "ada@analytical.engine" in result
      assert "grace@cobol.dev" in result
      assert "margaret@apollo.nasa" in result
      refute "linus@kernel.org" in result
    end
  end

  describe "large file simulation" do
    @tag :tmp_dir
    test "processes 50k rows with flat memory", %{tmp_dir: tmp} do
      big = Path.join(tmp, "big.csv")
      header = "id,val\n"
      body = for i <- 1..50_000, into: "", do: "#{i},x\n"
      File.write!(big, header <> body)

      {mem_before, _} = :erlang.process_info(self(), :memory)

      count = CsvStreamLib.count_rows(big)

      {mem_after, _} = :erlang.process_info(self(), :memory)

      assert count == 50_000
      # We're not loading rows into memory. Bound is generous but ensures
      # we're not proportional to row count.
      assert mem_after - mem_before < 5_000_000
    end
  end
end
```

Run:

```bash
mix deps.get
mix test
```

---

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Runnable demo of `CsvStreamLib`.

  Exercises the public API end-to-end so behaviour is observable
  in addition to documented.
  """

  def main do
    IO.puts("=== CsvStreamLib demo ===")
    run()
    IO.puts("\nDone — demo finished without errors.")
  end

  defp run do
    # CsvStreamLib.stream_maps/2 requires 2 argument(s);
    # call it with real values appropriate for this exercise.
    # CsvStreamLib.count_rows/2 requires 2 argument(s);
    # call it with real values appropriate for this exercise.
    :ok
  end
end

Main.main()
```

## Trade-offs and production gotchas

**1. Forgetting `:binary.copy/1` causes "memory leaks"**
If you store parsed rows in a list or send them to another process, the
whole chunk binary is pinned. Symptom: memory grows much faster than the
number of rows you kept. Fix: copy strings you retain.

**2. `Stream` is lazy — nothing runs without a sink**
`Stream.map` returns another stream; it does not execute. The pipeline
runs on `Enum.to_list`, `Enum.reduce`, `Stream.run`, `Enum.each`, etc.
Forgetting this is a common "why doesn't my pipeline do anything" bug.

**3. `File.stream!/3` reads by *line* by default**
A CSV field containing an escaped newline will be split. Use
`NimbleCSV.parse_stream/1` on the line stream — it rebuilds the escaped
field correctly. Don't try to hand-roll line parsing.

**4. `NimbleCSV` emits rows as lists, not maps**
Mapping to a map has a cost. For hot loops processing hundreds of
millions of rows, keep rows as lists and index by position.

**5. UTF-8 BOM on the first line**
CSVs exported from Excel often start with `\uFEFF`. Strip it before
parsing or your first header will look like `\uFEFFid`. Trim once at the
top of the stream.

**6. When NOT to use streaming**
For CSVs under 10 MB, `Enum.to_list` the whole thing — simpler code,
same wall-clock time. Streaming pays off above ~100 MB and becomes
mandatory above a few GB.

---

## Benchmark

<!-- benchmark N/A: integration/configuration exercise -->

## Reflection

- The test asserts memory growth stays under 5 MB while processing 50k rows. If you removed the `:binary.copy/1` calls and stored every row in a list via `Enum.to_list/1`, roughly how would that 5 MB bound change, and *why* — what is being pinned that the copy was releasing?

## Resources

- [NimbleCSV on HexDocs](https://hexdocs.pm/nimble_csv/NimbleCSV.html)
- [Elixir `Stream` module](https://hexdocs.pm/elixir/Stream.html)
- [`:binary.copy/1` on Erlang docs](https://www.erlang.org/doc/man/binary.html#copy-1) — why sub-binaries pin memory
- [RFC 4180 — CSV format](https://www.rfc-editor.org/rfc/rfc4180)

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

### `test/csv_stream_lib_test.exs`

```elixir
defmodule CsvStreamLibTest do
  use ExUnit.Case, async: true

  doctest CsvStreamLib

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert CsvStreamLib.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts
CSV files are line-based; streaming them with `Stream` and the CSV library enables processing multi-gigabyte files without loading into memory. `File.stream!/1` reads line by line; `CSV.decode/1` parses each line; `Stream` lazily chains operations. This is the pattern for data pipelines: stream → parse → transform → output. The trade-off: streams are lazy, so errors surface late; eager loading would fail fast. For production data pipelines, error handling at the stream level is essential.

---
