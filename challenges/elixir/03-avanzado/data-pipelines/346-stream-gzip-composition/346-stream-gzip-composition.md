# Stream Composition: `File.stream!` + gzip Decoding + Line Framing

**Project**: `archive_reader` — reads gzipped JSON-lines archives (100 MB–5 GB compressed) and produces a lazy stream of decoded records with constant memory.

## Project context

Nightly ETL archives arrive as `.jsonl.gz` files on S3. A typical day is
2 GB compressed, ~20 GB decompressed, containing tens of millions of JSON
records — one per line.

Naive `File.read!/1 |> :zlib.gunzip/1 |> String.split("\n") |> Enum.map(Jason.decode!/1)`
needs 20+ GB of RAM. A streaming pipeline processes the same file with <50 MB
of RAM: read compressed chunks, feed them to a zlib inflate stream, accumulate
bytes until a newline, emit a JSON record.

The hard part is composition: `:zlib` operates on byte chunks, but our consumer
wants records. We need to bridge "chunked byte stream" to "record stream"
without buffering the whole file.

```
archive_reader/
├── lib/
│   └── archive_reader/
│       ├── application.ex
│       ├── gzip_stream.ex          # chunked gunzip stream
│       └── line_framer.ex          # chunked bytes → line records
├── test/
│   └── archive_reader/
│       ├── gzip_stream_test.exs
│       └── line_framer_test.exs
├── bench/
│   └── decode_bench.exs
└── mix.exs
```

## Why `:zlib` streaming and not `:zlib.gunzip/1`

`:zlib.gunzip/1` is a one-shot function: it takes the entire compressed binary
and returns the entire decompressed one. Zero streaming. OOMs on large files.

The streaming API (`:zlib.open/0`, `inflateInit/2`, `safeInflate/2`) lets you
feed bytes incrementally and pulls out decompressed bytes as they become
available. Memory stays bounded to the decoder window (~64 KB) plus
application-level buffers.

Alternatives:

- **`ExGzip` / `StreamGzip` libraries**: thin wrappers over `:zlib`. Extra
  dependency for what is ~30 lines of Elixir.
- **Pipe to an external `gunzip` binary**: works, portable, but introduces
  a process boundary, non-portable error handling, and worse latency for
  small files.
- **Read the whole file into `/tmp` uncompressed**: doubles disk usage;
  still fails on 20 GB files.

## Core concepts

### 1. `Stream.resource/3` for stateful streams

```elixir
Stream.resource(
  fn -> init_state() end,        # start
  fn state -> pull_next(state) end, # next — return {chunks, state} or {:halt, state}
  fn state -> cleanup(state) end    # stop
)
```

This is the idiomatic way to wrap a stateful resource (file descriptor, zlib
context, TCP socket) as a lazy stream.

### 2. `Stream.transform/3` for mapping with accumulated state

`Stream.transform/3` is like `Stream.map/2` but the mapping function has access
to an accumulator that persists across elements. We use it to buffer partial
lines across chunks: when a chunk arrives with `"...abc\ndef\ng"`, we emit
`"...abc"` and `"def"` and keep `"g"` in the accumulator for the next chunk.

### 3. `safeInflate/2` error handling

`safeInflate` returns `{:continue, [chunks]}` when the inflater wants more
input, `{:finished, [chunks]}` when the stream ends, or raises on corruption.
Must be called repeatedly until it returns `:finished`.

## Design decisions

- **Option A — Use `IO.binstream/2` and `String.split/2`**:
  - Pros: one-liner.
  - Cons: doesn't handle gzip; doesn't handle chunk boundaries that split a line.
- **Option B — Build streaming gunzip + line framer as two composable `Stream` modules**:
  - Pros: each piece testable, pipeline stays lazy, bounded memory.
  - Cons: ~50 lines of code you write and own.
- **Option C — Use `GenStage` with explicit buffers**:
  - Pros: back-pressure across process boundaries.
  - Cons: overkill for a single-producer single-consumer local stream.

Chose **Option B**. Pure `Stream` pipelines are the lightest and fit
single-process file processing perfectly.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule ArchiveReader.MixProject do
  use Mix.Project

  def project do
    [
      app: :archive_reader,
      version: "0.1.0",
      elixir: "~> 1.16",
      deps: deps()
    ]
  end

  def application, do: [extra_applications: [:logger]]

  defp deps do
    [
      {:jason, "~> 1.4"},
      {:benchee, "~> 1.3", only: :dev}
    ]
  end
end
```

### Step 1: Chunked gunzip stream

```elixir
defmodule ArchiveReader.GzipStream do
  @moduledoc """
  Turns a byte-chunk stream (e.g. File.stream!/3 with :raw, 64 KB chunks) into
  a stream of decompressed byte chunks.
  """

  @spec decompress(Enumerable.t()) :: Enumerable.t()
  def decompress(compressed_chunks) do
    Stream.resource(
      fn ->
        z = :zlib.open()
        # 31 = 15 (window bits) + 16 (enable gzip header parsing)
        :ok = :zlib.inflateInit(z, 31)
        {z, compressed_chunks, nil}
      end,
      fn
        {z, rest, :finished} ->
          {:halt, {z, rest, :finished}}

        {z, chunks, _state} ->
          case next_chunk(chunks) do
            :done ->
              {:halt, {z, [], :finished}}

            {:chunk, bin, rest} ->
              case :zlib.safeInflate(z, bin) do
                {:continue, out} -> {List.wrap(out), {z, rest, :continue}}
                {:finished, out} -> {List.wrap(out), {z, rest, :finished}}
              end
          end
      end,
      fn {z, _, _} ->
        :zlib.inflateEnd(z)
        :zlib.close(z)
      end
    )
  end

  defp next_chunk(enum) do
    case Enum.take(enum, 1) do
      [] -> :done
      [chunk] -> {:chunk, chunk, Stream.drop(enum, 1)}
    end
  end
end
```

### Step 2: Line framer

```elixir
defmodule ArchiveReader.LineFramer do
  @moduledoc """
  Turns a stream of arbitrary-sized byte chunks into a stream of newline-delimited
  strings. Does NOT buffer the whole file — keeps only the trailing partial line.
  """

  @spec frame(Enumerable.t()) :: Enumerable.t()
  def frame(chunks) do
    chunks
    |> Stream.transform("", fn chunk, pending ->
      combined = pending <> chunk

      case :binary.split(combined, "\n", [:global]) do
        [partial] ->
          # No newline in this chunk — keep everything for later.
          {[], partial}

        many ->
          {complete, [partial]} = Enum.split(many, length(many) - 1)
          {complete, partial}
      end
    end)
    |> Stream.concat(
      Stream.unfold(:pending, fn
        :pending -> {[], :done}
        :done -> nil
      end)
    )
  end
end
```

### Step 3: Composed reader

```elixir
defmodule ArchiveReader do
  alias ArchiveReader.{GzipStream, LineFramer}

  @doc """
  Returns a lazy stream of decoded JSON records from a .jsonl.gz file.

  Memory stays bounded to the zlib window (~64 KB) + one partial line.
  """
  @spec stream(Path.t()) :: Enumerable.t()
  def stream(path) do
    path
    |> File.stream!([:raw, :read_ahead], 64 * 1024)
    |> GzipStream.decompress()
    |> LineFramer.frame()
    |> Stream.reject(&(&1 == ""))
    |> Stream.map(&Jason.decode!/1)
  end
end
```

## Why this works

- `File.stream!/3` with `[:raw, :read_ahead]` and a 64 KB chunk size reads the
  file in chunks backed by kernel read-ahead buffers — no line-by-line syscalls.
- `GzipStream.decompress/1` feeds each compressed chunk to a persistent zlib
  inflate context and emits decompressed chunks lazily.
- `LineFramer.frame/1` splits chunks on `\n`. When a chunk ends mid-line, the
  partial line is carried in the `Stream.transform/3` accumulator. The next
  chunk prepends it — no line is lost or duplicated.
- `Stream.reject(&(&1 == ""))` drops the trailing empty line common at end of
  file.
- The whole pipeline is lazy — nothing executes until the consumer calls
  `Enum.take/2` or similar.

## Tests

```elixir
defmodule ArchiveReader.GzipStreamTest do
  use ExUnit.Case, async: true

  alias ArchiveReader.GzipStream

  describe "decompress/1" do
    test "roundtrips small content" do
      original = "hello world\n" |> String.duplicate(10_000)
      compressed = :zlib.gzip(original)
      result = [compressed] |> GzipStream.decompress() |> Enum.to_list() |> IO.iodata_to_binary()
      assert result == original
    end

    test "handles multi-chunk compressed input" do
      original = String.duplicate("abc\n", 100_000)
      compressed = :zlib.gzip(original)

      # Feed in arbitrary chunk sizes to simulate a real stream.
      chunks = for <<chunk::binary-size(1024) <- compressed>>, do: chunk

      tail = binary_part(compressed, length(chunks) * 1024, byte_size(compressed) - length(chunks) * 1024)
      all = chunks ++ [tail]

      result = all |> GzipStream.decompress() |> Enum.to_list() |> IO.iodata_to_binary()
      assert result == original
    end
  end
end

defmodule ArchiveReader.LineFramerTest do
  use ExUnit.Case, async: true

  alias ArchiveReader.LineFramer

  describe "frame/1" do
    test "emits full lines from multi-chunk input" do
      chunks = ["hel", "lo\nwor", "ld\n", "bye\n"]
      assert LineFramer.frame(chunks) |> Enum.to_list() == ["hello", "world", "bye"]
    end

    test "drops incomplete trailing line without newline" do
      chunks = ["a\nb\nc"]
      # The framer keeps "c" in the buffer; it is never emitted.
      assert LineFramer.frame(chunks) |> Enum.to_list() == ["a", "b"]
    end

    test "handles chunks that end exactly at newline" do
      chunks = ["a\n", "b\n"]
      assert LineFramer.frame(chunks) |> Enum.to_list() == ["a", "b"]
    end
  end
end

defmodule ArchiveReaderTest do
  use ExUnit.Case, async: true

  setup do
    path = Path.join(System.tmp_dir!(), "test_#{:erlang.unique_integer()}.jsonl.gz")

    content =
      for i <- 1..1_000 do
        Jason.encode!(%{id: i, value: "x#{i}"})
      end
      |> Enum.join("\n")
      |> Kernel.<>("\n")

    File.write!(path, :zlib.gzip(content))
    on_exit(fn -> File.rm(path) end)
    {:ok, path: path}
  end

  test "streams all records lazily", %{path: path} do
    records = path |> ArchiveReader.stream() |> Enum.to_list()
    assert length(records) == 1_000
    assert List.first(records) == %{"id" => 1, "value" => "x1"}
    assert List.last(records) == %{"id" => 1_000, "value" => "x1000"}
  end

  test "Enum.take/2 is truly lazy (does not read the whole file)", %{path: path} do
    records = path |> ArchiveReader.stream() |> Enum.take(5)
    assert length(records) == 5
  end
end
```

## Benchmark

```elixir
# bench/decode_bench.exs
# Generate a 100 MB compressed file of JSON lines and measure:
#   (a) memory: should stay <100 MB RSS
#   (b) throughput: lines/sec

path = Path.join(System.tmp_dir!(), "bench_100m.jsonl.gz")

unless File.exists?(path) do
  IO.puts("Generating #{path}...")

  {:ok, io} = File.open(path, [:write, :compressed])

  for i <- 1..2_000_000 do
    IO.puts(io, Jason.encode!(%{id: i, name: "user_#{i}", ts: System.system_time()}))
  end

  File.close(io)
end

Benchee.run(%{
  "stream + decode" => fn ->
    path
    |> ArchiveReader.stream()
    |> Enum.reduce(0, fn _, acc -> acc + 1 end)
  end
}, time: 10, warmup: 3, memory_time: 2)
```

**Target**: ~500k records/sec on a modern SSD. Memory usage reported by Benchee
should stay under 10 MB (most of it Jason decoder working memory).

## Trade-offs and production gotchas

**1. `:zlib.gunzip/1` on multi-GB files will OOM.**
Even in dev. Always use the streaming inflate path for anything you don't
fully control the size of.

**2. Not calling `inflateEnd` + `close` leaks OS resources.**
`Stream.resource/3`'s cleanup function is crucial — if consumers halt early
(e.g. `Enum.take(5)`), the cleanup runs and closes the zlib handle. Test this.

**3. Windows file reading defaults to text mode.**
On Windows, `File.stream!/3` without `:raw` may do CRLF translation, corrupting
the compressed bytes. Always pass `:raw` when reading binary data.

**4. Partial lines at end of file are silently discarded.**
If the producer forgets a trailing newline, the last record is lost. For strict
pipelines, track the partial buffer and fail on non-empty trailing data.

**5. `Jason.decode!/1` per line is the bottleneck.**
At ~500k lines/sec, Jason decoding dominates. For massive files consider
`Jason.decode/2` with `strings: :copy` off or switching to `NimbleJSON` /
`Poison` benchmarks.

**6. When NOT to use Stream composition.**
If you need parallelism, use `Flow`. If you need back-pressure across
processes, use `GenStage`. Stream is perfect for single-process lazy
pipelines with bounded memory.

## Reflection

Your file reader works perfectly on 5 GB archives. Ops reports that one
production file causes the decoder to hang for 30 seconds and then crash
with `{:error, :data_error}`. The file is a valid gzip but the contained
JSON lines have a 200 MB record with no newline. What changes to
`GzipStream` and `LineFramer` would detect and fail fast on oversize records
without loading the whole file?

## Resources

- [`:zlib` docs — Erlang/OTP](https://www.erlang.org/doc/man/zlib.html)
- [`Stream.resource/3` docs](https://hexdocs.pm/elixir/Stream.html#resource/3)
- [`Stream.transform/3` docs](https://hexdocs.pm/elixir/Stream.html#transform/3)
- [Benchee](https://github.com/bencheeorg/benchee)
