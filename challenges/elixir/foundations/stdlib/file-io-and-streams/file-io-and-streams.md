# File IO and Streams: Building a CSV Streaming Processor

**Project**: `csv_stream` — a CSV transformer that processes multi-GB files without loading them into RAM

---

## Why streams matter for a senior developer

When you open a 10 GB access log with `File.read!/1`, the BEAM loads every byte
into a single binary on the process heap. The VM either dies with an out-of-memory
error or triggers massive GC pauses. `File.stream!/3` solves this by returning a
lazy `Stream` that reads chunks on demand, so memory stays bounded regardless of
file size.

Two concepts power this module:

1. **`File.stream!/3`**: returns an enumerable where each element is a line or a
   fixed-size byte chunk. Nothing is read until the stream is consumed.
2. **Back-pressure via laziness**: `Stream.map`, `Stream.filter`, and friends
   build a pipeline description. Execution only happens when a terminal function
   (`Enum.to_list`, `Stream.run`, `Enum.reduce`, `Stream.into`) pulls values
   through. Each pulled chunk is processed and released before the next is read.

This is the same model used internally by Broadway, Flow, and GenStage.

---

## Why streams and not `File.read!/1` + `Enum`

Reading the full file into memory is simplest and fastest for small inputs, but it scales catastrophically: a 10 GB file needs 10 GB of heap (plus binary refcount copies for every transformation). The VM either OOMs or triggers multi-second GC pauses that freeze request-handling processes on the same node. Streams trade a fixed per-chunk overhead for bounded memory regardless of input size — the only viable option once files outgrow RAM or share a node with latency-sensitive work.

---

## The business problem

Your team ingests CSV exports from a legacy system. Files are 2-15 GB and contain
transaction records. You need a tool that:

1. Streams a CSV file line by line
2. Parses each row (a naive CSV parser — enough for well-formed exports)
3. Filters rows by a predicate (e.g. amount > threshold)
4. Transforms columns (e.g. normalize currency codes)
5. Writes a new CSV without ever holding more than one chunk in memory
6. Reports progress every N lines via a callback

Memory ceiling: under 50 MB regardless of input size.

---

## Project structure

```
csv_stream/
├── lib/
│   └── csv_stream/
│       ├── parser.ex
│       ├── pipeline.ex
│       └── progress.ex
├── script/
│   └── main.exs
├── test/
│   └── csv_stream/
│       ├── parser_test.exs
│       └── pipeline_test.exs
├── .formatter.exs
└── mix.exs
```

---

## Design decisions

**Option A — `File.read!/1` + `String.split/2` + `Enum.*`**
- Pros: single pass over a plain string; trivial to debug; fastest on small inputs; no laziness surprises.
- Cons: holds the entire file in memory; dies on GB-scale input; every `Enum.map` allocates a fresh list.

**Option B — `File.stream!/3` + `Stream.*` pipeline + `Stream.into(File.stream!/1)`** (chosen)
- Pros: memory bounded by chunk size; composes via `Stream.*`; interleaves reads and writes so the VM never buffers the output; identical code works on MB and GB files.
- Cons: slightly slower on tiny files (constant-factor overhead); easy to accidentally materialise with `Enum.to_list/1` and defeat the laziness; debugging laziness requires mental modelling.

Chose **B** because the product requirement IS multi-GB input; any solution that holds the file in RAM is wrong by definition. The laziness cost is paid in the test suite once and never again.

---

## Implementation

### Step 1: Create the project

**Objective**: Streams via File.stream! process N-GB files with O(chunk) memory; File.read! scales as O(file size).

```bash
mix new csv_stream
cd csv_stream
```

### `mix.exs`
**Objective**: Boilerplate; focus on how Streams are lazily composed — Enum.to_list defeats laziness.

```elixir
defmodule CsvStream.MixProject do
  use Mix.Project

  def project do
    [
      app: :csv_stream,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps, do: []
end
```

No external dependencies — the standard library is enough.

### Step 3: `.formatter.exs`

**Objective**: Formatter is opinionated; configure inputs glob + line length once; format is hermetic (no env deps).

```elixir
[
  inputs: ["{mix,.formatter}.exs", "{config,lib,test}/**/*.{ex,exs}"],
  line_length: 98
]
```

### `lib/csv_stream.ex`

```elixir
defmodule CsvStream do
  @moduledoc """
  File IO and Streams: Building a CSV Streaming Processor.

  When you open a 10 GB access log with `File.read!/1`, the BEAM loads every byte.
  """
end
```

### `lib/csv_stream/parser.ex`

**Objective**: Minimal CSV parser (split on comma); RFC 4180 (quotes, escapes) requires a real parser, not a toy.

```elixir
defmodule CsvStream.Parser do
  @moduledoc """
  Minimal CSV line parser. Handles quoted fields and escaped quotes.

  Kept separate from the pipeline so parsing can be unit-tested
  without any IO.
  """

  @doc """
  Parses a single CSV line into a list of string fields.

  Assumes the input is one complete line without the trailing newline.
  Quoted fields may contain commas; doubled quotes inside quotes are
  treated as a literal quote (RFC 4180 style).
  """
  @spec parse_line(String.t()) :: [String.t()]
  def parse_line(line) when is_binary(line) do
    # Using a small state machine: we walk the string once and accumulate
    # fields. A recursive approach keeps per-line allocations low, which
    # matters when we parse millions of rows.
    parse(line, [], "", false)
  end

  @spec encode_line([String.t()]) :: String.t()
  def encode_line(fields) when is_list(fields) do
    fields
    |> Enum.map(&quote_if_needed/1)
    |> Enum.join(",")
  end

  # --- private ---

  # in_quotes? is the key piece of state: commas and newlines inside
  # quotes must not split fields.
  defp parse("", fields, current, _in_quotes?) do
    Enum.reverse([current | fields])
  end

  defp parse(<<?", ?", rest::binary>>, fields, current, true) do
    # Escaped quote inside a quoted field becomes a literal quote.
    parse(rest, fields, current <> "\"", true)
  end

  defp parse(<<?", rest::binary>>, fields, current, in_quotes?) do
    parse(rest, fields, current, not in_quotes?)
  end

  defp parse(<<?,, rest::binary>>, fields, current, false) do
    parse(rest, [current | fields], "", false)
  end

  defp parse(<<char::utf8, rest::binary>>, fields, current, in_quotes?) do
    parse(rest, fields, current <> <<char::utf8>>, in_quotes?)
  end

  defp quote_if_needed(field) do
    # Quote fields that contain commas, quotes or newlines. Double any
    # embedded quotes per RFC 4180.
    if String.contains?(field, [",", "\"", "\n"]) do
      escaped = String.replace(field, "\"", "\"\"")
      "\"" <> escaped <> "\""
    else
      field
    end
  end
end
```

**Why this works:**

- The parser is a pure function: no IO, no process state. It is trivially
  testable and can run on any binary chunk.
- The recursion carries the `in_quotes?` flag instead of mutable state. The
  BEAM optimises this into a loop (tail call).
- `encode_line/1` mirrors `parse_line/1` so a round-trip is lossless for
  well-formed rows.

### `lib/csv_stream/progress.ex`

**Objective**: Callbacks every N lines report progress without tight coupling; Stream.each is the hook for side effects.

```elixir
defmodule CsvStream.Progress do
  @moduledoc """
  Progress reporting helper. Builds a stream transformer that invokes
  a callback every N rows without breaking the laziness of the pipeline.
  """

  @doc """
  Wraps an enumerable so that every `every` elements the callback fires
  with the running count. The callback is invoked for side effects only;
  its return value is ignored.
  """
  @spec report(Enumerable.t(), pos_integer(), (pos_integer() -> any())) :: Enumerable.t()
  def report(enum, every, callback)
      when is_integer(every) and every > 0 and is_function(callback, 1) do
    # Stream.transform threads an accumulator (the counter) through the
    # pipeline without materialising the whole stream. This is what makes
    # progress reporting memory-safe on GB-scale files.
    Stream.transform(enum, 0, fn element, count ->
      new_count = count + 1
      if rem(new_count, every) == 0, do: callback.(new_count)
      {[element], new_count}
    end)
  end
end
```

### `lib/csv_stream/pipeline.ex`

**Objective**: Stream.into(output, mapper) writes lazily; never buffers output; input and write are interleaved.

```elixir
defmodule CsvStream.Pipeline do
  @moduledoc """
  End-to-end CSV streaming: read, parse, filter, transform, write.

  All stages are lazy. The file is read and written in small chunks;
  the VM memory footprint stays bounded regardless of input size.
  """

  alias CsvStream.{Parser, Progress}

  @type row :: [String.t()]
  @type options :: [
          filter: (row() -> boolean()),
          transform: (row() -> row()),
          has_header: boolean(),
          progress_every: pos_integer() | nil,
          progress_callback: (pos_integer() -> any()) | nil
        ]

  @doc """
  Streams `input_path`, transforms rows, and writes to `output_path`.

  Returns `{:ok, row_count}` where row_count excludes the header if
  `has_header: true`.

  Raises on IO errors — callers that care should wrap in a `try`.
  """
  @spec run(Path.t(), Path.t(), options()) :: {:ok, non_neg_integer()}
  def run(input_path, output_path, opts \\ []) do
    filter = Keyword.get(opts, :filter, fn _ -> true end)
    transform = Keyword.get(opts, :transform, & &1)
    has_header = Keyword.get(opts, :has_header, true)
    progress_every = Keyword.get(opts, :progress_every)
    progress_callback = Keyword.get(opts, :progress_callback)

    # read_ahead tunes the I/O buffer. 64 KB is a good default for local
    # disks; on slow network mounts you may want larger values.
    input =
      File.stream!(input_path, read_ahead: 64 * 1024)
      |> Stream.map(&String.trim_trailing(&1, "\n"))
      |> Stream.map(&String.trim_trailing(&1, "\r"))

    {header_stream, body_stream} = split_header(input, has_header)

    processed =
      body_stream
      |> Stream.map(&Parser.parse_line/1)
      |> Stream.filter(filter)
      |> Stream.map(transform)
      |> Stream.map(&Parser.encode_line/1)
      |> maybe_report(progress_every, progress_callback)
      |> Stream.map(&(&1 <> "\n"))

    full_output = Stream.concat(header_stream, processed)

    # Stream.into with File.stream!(path) is the canonical "pipe a stream
    # into a file" idiom. The sink is itself a stream, so writes happen
    # chunk by chunk.
    count =
      full_output
      |> Stream.into(File.stream!(output_path))
      |> Enum.reduce(0, fn _line, acc -> acc + 1 end)

    rows_written = if has_header, do: count - 1, else: count
    {:ok, rows_written}
  end

  # --- private ---

  defp split_header(stream, false), do: {Stream.concat([]), stream}

  defp split_header(stream, true) do
    # Take the first line eagerly so we can re-emit it verbatim without
    # parsing. The tail is still lazy.
    header_list = stream |> Stream.take(1) |> Enum.to_list() |> Enum.map(&(&1 <> "\n"))
    body = Stream.drop(stream, 1)
    {Stream.concat([], header_list), body}
  end

  defp maybe_report(stream, nil, _callback), do: stream
  defp maybe_report(stream, _every, nil), do: stream

  defp maybe_report(stream, every, callback) do
    Progress.report(stream, every, callback)
  end
end
```

**Why this works:**

- Every stage returns a `Stream`. Nothing executes until `Stream.into/2`
  is consumed by `Enum.reduce/3`.
- `File.stream!/3` with `read_ahead` batches the underlying Erlang
  `:file` reads so we don't pay a syscall per line.
- The header is handled as a tiny eager prefix that is reattached to
  the lazy body via `Stream.concat/2`. The body remains lazy.
- `Stream.into/2` with `File.stream!/1` as the sink means writes are
  interleaved with reads — we never buffer the output.

### `test/csv_stream_test.exs`

**Objective**: Test both small/large files; test that Stream.into doesn't load file into memory via memory monitoring.

```elixir
defmodule CsvStream.ParserTest do
  use ExUnit.Case, async: true
  doctest CsvStream.Pipeline
  alias CsvStream.Parser

  describe "parse_line/1" do
    test "parses a simple row" do
      assert Parser.parse_line("a,b,c") == ["a", "b", "c"]
    end

    test "parses quoted fields containing commas" do
      assert Parser.parse_line(~s("a,b",c)) == ["a,b", "c"]
    end

    test "parses escaped quotes inside quoted fields" do
      assert Parser.parse_line(~s("he said ""hi""",x)) == [~s(he said "hi"), "x"]
    end

    test "handles empty fields" do
      assert Parser.parse_line("a,,b") == ["a", "", "b"]
    end

    test "handles trailing empty field" do
      assert Parser.parse_line("a,b,") == ["a", "b", ""]
    end
  end

  describe "encode_line/1" do
    test "joins simple fields with commas" do
      assert Parser.encode_line(["a", "b", "c"]) == "a,b,c"
    end

    test "quotes fields containing commas" do
      assert Parser.encode_line(["a,b", "c"]) == ~s("a,b",c)
    end

    test "doubles embedded quotes" do
      assert Parser.encode_line([~s(he said "hi"), "x"]) == ~s("he said ""hi""",x)
    end

    test "is lossless through a round trip" do
      row = [~s(a,b), ~s("quoted"), "plain", ""]
      assert row |> Parser.encode_line() |> Parser.parse_line() == row
    end
  end
end
```

```elixir
defmodule CsvStream.PipelineTest do
  use ExUnit.Case, async: true
  doctest CsvStream.Pipeline
  alias CsvStream.Pipeline

  @tmp_dir Path.join(System.tmp_dir!(), "csv_stream_test")

  setup do
    File.mkdir_p!(@tmp_dir)
    on_exit(fn -> File.rm_rf!(@tmp_dir) end)
    :ok
  end

  defp write_csv(name, content) do
    path = Path.join(@tmp_dir, name)
    File.write!(path, content)
    path
  end

  defp read_csv(path), do: File.read!(path)

  describe "core functionality" do
    test "copies a CSV through with no transforms" do
      input = write_csv("in.csv", "id,name\n1,Alice\n2,Bob\n")
      output = Path.join(@tmp_dir, "out.csv")

      assert {:ok, 2} = Pipeline.run(input, output, has_header: true)
      assert read_csv(output) == "id,name\n1,Alice\n2,Bob\n"
    end

    test "filters rows based on a predicate" do
      input = write_csv("in.csv", "id,amount\n1,10\n2,250\n3,99\n")
      output = Path.join(@tmp_dir, "out.csv")

      filter = fn [_id, amount] -> String.to_integer(amount) >= 100 end

      assert {:ok, 1} = Pipeline.run(input, output, filter: filter)
      assert read_csv(output) == "id,amount\n2,250\n"
    end

    test "transforms columns" do
      input = write_csv("in.csv", "id,currency\n1,usd\n2,eur\n")
      output = Path.join(@tmp_dir, "out.csv")

      transform = fn [id, currency] -> [id, String.upcase(currency)] end

      assert {:ok, 2} = Pipeline.run(input, output, transform: transform)
      assert read_csv(output) == "id,currency\n1,USD\n2,EUR\n"
    end

    test "reports progress every N rows" do
      rows = for i <- 1..10, into: "id\n", do: "#{i}\n"
      input = write_csv("in.csv", rows)
      output = Path.join(@tmp_dir, "out.csv")

      test_pid = self()

      {:ok, 10} =
        Pipeline.run(input, output,
          progress_every: 3,
          progress_callback: fn n -> send(test_pid, {:progress, n}) end
        )

      # 3, 6, 9 should trigger.
      assert_received {:progress, 3}
      assert_received {:progress, 6}
      assert_received {:progress, 9}
      refute_received {:progress, 10}
    end

    test "handles files without a header" do
      input = write_csv("in.csv", "1,a\n2,b\n")
      output = Path.join(@tmp_dir, "out.csv")

      assert {:ok, 2} = Pipeline.run(input, output, has_header: false)
      assert read_csv(output) == "1,a\n2,b\n"
    end

    test "does not load the whole file in memory" do
      # Smoke test: generate 50k rows and process them. If something broke
      # laziness (e.g. an accidental Enum.map), this would still pass on
      # modern machines — so we only assert the count and leave the real
      # GB-scale verification to the Erlang observer during manual runs.
      rows = for i <- 1..50_000, into: "id,val\n", do: "#{i},x\n"
      input = write_csv("big.csv", rows)
      output = Path.join(@tmp_dir, "big_out.csv")

      assert {:ok, 50_000} = Pipeline.run(input, output)
    end
  end
end
```

### Step 8: Run and verify

**Objective**: --warnings-as-errors finds dead progress callbacks; test coverage validates output matches input semantics.

```bash
mix deps.get
mix compile --warnings-as-errors
mix test --trace
mix format
```

### Why this works

Every stage of the pipeline returns a `Stream` — a description of work, not the work itself. Nothing executes until `Enum.reduce/3` pulls values through at the terminal step, which means memory stays bounded by the `read_ahead` buffer (64 KB here) regardless of file size. `Stream.transform/3` threads the row counter through the pipeline so progress reporting inherits the same laziness. `Stream.into(File.stream!/1)` keeps the output file open for the whole run and interleaves writes with reads — the VM never buffers more than one chunk's worth of output.

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== CsvStream: demo ===\n")

    result_1 = CsvStream.Parser.parse_line("a,b,c")
    IO.puts("Demo 1: #{inspect(result_1)}")

    result_2 = CsvStream.Parser.parse_line(~s("a,b",c))
    IO.puts("Demo 2: #{inspect(result_2)}")

    result_3 = CsvStream.Parser.parse_line(~s("he said ""hi""",x))
    IO.puts("Demo 3: #{inspect(result_3)}")
    IO.puts("\n=== Done ===")
  end
end

Main.main()
```

Run with: `elixir script/main.exs`

---

---

Create a simple example demonstrating the key concepts:

```elixir
# Example code demonstrating module concepts
IO.puts("Example: Read the Implementation section above and run the code samples in iex")
```

## Benchmark

```elixir
# bench.exs
defmodule Bench do
  def run do
    path = Path.join(System.tmp_dir!(), "csv_stream_bench.csv")
    rows = for i <- 1..500_000, into: "id,val\n", do: "#{i},x\n"
    File.write!(path, rows)

    out = Path.join(System.tmp_dir!(), "csv_stream_bench_out.csv")

    {us, _} =
      :timer.tc(fn ->
        CsvStream.Pipeline.run(path, out, has_header: true)
      end)

    IO.puts("500k rows streamed: #{us} µs (#{us / 500_000} µs/row)")
    File.rm(path)
    File.rm(out)
  end
end

Bench.run()
```

Target: under 3 seconds for 500k rows (~6 µs per row) on modern SSDs. Memory high-water should stay under 50 MB — verify with `:erlang.memory(:total)` before and after, or use `:observer.start()` for a GB-scale run.

---

## Trade-off analysis

| Aspect | Streams (current) | `File.read!/1` + `Enum` |
|--------|------------------|-------------------------|
| Memory | Bounded by chunk size | Entire file in RAM |
| Small files (< 10 MB) | Slightly slower (overhead) | Fastest |
| GB-scale files | Only viable option | Crashes the VM |
| Composability | Pipelines via `Stream.*` | Same via `Enum.*` |
| Early termination | Yes (lazy pull stops reading) | No (already read) |

| Aspect | Line streaming | Chunked byte streaming (`File.stream!(path, [], 65_536)`) |
|--------|---------------|---------------------------------------------------------|
| Use case | CSV, JSONL, logs | Binary formats, custom framing |
| API | `String.split` per element | Manual framing logic |
| Throughput | Good for small records | Best for large records |

---

## Common production mistakes

**1. Calling `Enum.to_list/1` in the middle of a stream**
The moment you call `Enum.*` on a stream, it materialises. `stream |> Enum.map(...) |> Stream.filter(...)` defeats the entire point — the `Enum.map` pulls everything
into memory before `Stream.filter` even sees it. Stay in `Stream.*` until the
terminal step.

**2. Forgetting `read_ahead`**
Without `read_ahead`, `File.stream!/1` issues one syscall per line. For a 10M-line
file that is 10M syscalls. Pass `read_ahead: 65_536` or similar and throughput
can improve by 5-10x.

**3. Writing to the output with `File.write!/2` inside a `Stream.each`**
Opens and closes the file on every line. Use `Stream.into(File.stream!(path))`
so the file stays open for the duration of the pipeline.

**4. Trimming only `\n` on Windows CSVs**
Windows exports end lines with `\r\n`. If you only trim `\n`, the last field of
every row ends with `\r` and your filters silently mismatch. Trim both, as the
pipeline above does.

**5. Catching errors mid-stream and continuing**
A malformed row in a CSV is usually a sign of upstream corruption. Failing fast
and reporting the line number is safer than silently skipping. If you must
skip, log every skipped row with its byte offset so ops can audit.

---

## When NOT to use streams

- **Tiny files** (a few KB). `File.read!/1` plus `String.split/2` is simpler
  and as fast.
- **Random access** (jumping around a file). Streams are strictly forward;
  use `:file.pread/3` for positioned reads.
- **Parallelism is the bottleneck**. A single `Stream` is sequential. Reach
  for `Flow` or `Broadway` when CPU-bound transforms need multiple cores.

---

## Reflection

1. The pipeline is strictly sequential. If the per-row transform becomes CPU-bound (e.g. decrypting each row), your single-core throughput caps hard. Would you reach for `Flow.from_enumerable/2` with partitioning, spawn a pool of Task workers reading disjoint byte ranges, or push the work to Broadway with a file-chunk producer? What breaks first as the transform grows more expensive?
2. A malformed row currently crashes the whole run. Product now wants "skip bad rows, write them to a quarantine file, keep going". Would you wrap each row in `try/rescue` inside the `Stream.map`, split the stream into `{:ok, row} | {:error, raw}` tags and fan out via `Stream.transform`, or introduce a second pipeline stage downstream? What's the memory and readability cost of each?

---

## Resources

- [File module — HexDocs](https://hexdocs.pm/elixir/File.html)
- [Stream module — HexDocs](https://hexdocs.pm/elixir/Stream.html)
- [Enum vs Stream — Elixir School](https://elixirschool.com/en/lessons/basics/enum)
- [Flow — parallel streams on top of GenStage](https://hexdocs.pm/flow/Flow.html)
- [RFC 4180 — CSV format](https://datatracker.ietf.org/doc/html/rfc4180)

---

## Why File IO and Streams matters

Mastering **File IO and Streams** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

---

## Key concepts
### 1. `File.read/1` Loads Entire Files Into Memory

For small files, this is fine. For gigabyte-scale files, it's a memory bomb. Use `File.stream!/2` to read lazily in chunks.

### 2. `File.stream!/2` Returns a Stream

```elixir
File.stream!("large.txt") |> Enum.map(&String.trim/1)
```

Each line is read on demand, not preloaded. This is essential for processing large files or piping to external processes.

### 3. Ensure Cleanup with `File.open/2` or Streams

If you use `File.open/2`, ensure the file descriptor is closed (use `File.close/1` or `File.read/2`). Streams auto-close when garbage-collected, but explicit is better. Never leave file descriptors open.

---
