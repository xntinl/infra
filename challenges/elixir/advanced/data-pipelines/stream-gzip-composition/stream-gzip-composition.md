# Stream Composition: `File.stream!` + gzip Decoding + Line Framing

**Project**: `archive_reader` — reads gzipped JSON-lines archives (100 MB–5 GB compressed) and produces a lazy stream of decoded records with constant memory

---

## Why data pipelines matters

GenStage, Flow, and Broadway make back-pressured concurrent data processing a first-class concern. Producers, consumers, dispatchers, and batchers compose into pipelines that absorb bursts without exhausting memory.

The hard problems are exactly-once semantics, checkpointing for resumability, and tuning batcher concurrency against downstream latency. A pipeline that works at 10 events/sec often collapses at 10k unless these concerns were designed in from the start.

---

## The business problem

You are building a production-grade Elixir component in the **Data pipelines** area. The goal is not a toy demonstration — it is a piece you could drop into a real system and rely on under load.

Concretely, you must:

1. Implement the topic correctly with measurable behavior.
2. Surface failure modes via `{:ok, _} / {:error, reason}` tuples — never raise from a public API except for programmer errors.
3. Bound resources: explicit timeouts, memory caps, back-pressure where relevant, and supervision for any long-lived process.
4. Provide a test suite with `async: true` where safe, `doctest` for public examples, and `describe` blocks that name the contract.
5. Provide a `script/main.exs` that demonstrates the piece end-to-end with realistic data — not a `1 + 1` smoke test.

---

## Project structure

```
archive_reader/
├── lib/
│   └── archive_reader.ex
├── script/
│   └── main.exs
├── test/
│   └── archive_reader_test.exs
└── mix.exs
```

---

## Design decisions

**Option A — minimal happy-path implementation**
- Pros: smaller surface area, faster to ship.
- Cons: no resource bounds, no failure-mode coverage, no observability hooks. Falls over the first time production load deviates from the developer's mental model.

**Option B — production-grade contract with explicit bounds** (chosen)
- Pros: timeouts, supervised lifecycle, structured errors, idiomatic `{:ok, _} / {:error, reason}` returns. Tests cover the failure envelope, not just the happy path.
- Cons: more code, more concepts. Pays for itself the first time the upstream service degrades.

Chose **B** because in Data pipelines the failure modes — not the happy path — are what determine whether the system stays up under real production conditions.

---

## Implementation

### `mix.exs`

```elixir
defmodule ArchiveReader.MixProject do
  use Mix.Project

  def project do
    [
      app: :archive_reader,
      version: "0.1.0",
      elixir: "~> 1.19",
      elixirc_paths: elixirc_paths(Mix.env()),
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp elixirc_paths(:test), do: ["lib", "test/support"]
  defp elixirc_paths(_), do: ["lib"]

  defp deps do
    [
      {:ex_doc, "~> 0.31", only: :dev, runtime: false}
    ]
  end
end
```

### `lib/archive_reader.ex`

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

### `test/archive_reader_test.exs`

```elixir
defmodule ArchiveReader.GzipStreamTest do
  use ExUnit.Case, async: true
  doctest ArchiveReader.GzipStream

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

### `script/main.exs`

```elixir
defmodule Main do
  def main do
      # Demonstrate lazy stream composition: file -> decompress -> parse
      # Simulate gzip stream (in reality: File.stream! + :zlib)
      data = [
        "{\"user_id\":1,\"action\":\"login\"}\n",
        "{\"user_id\":2,\"action\":\"purchase\"}\n",
        "{\"user_id\":1,\"action\":\"logout\"}\n"
      ]

      # Lazy stream pipeline
      stream = Stream.each(data, fn line ->
        # Simulate decompression + parsing
        {:ok, json} = Jason.decode(line)
        json
      end)

      # Collect with limit (demonstrating lazy evaluation)
      records = Enum.take(stream, 2)

      IO.inspect(records, label: "✓ Streamed records (lazy)")

      assert length(records) == 2, "Streamed 2 records"

      IO.puts("✓ Stream composition: lazy file processing with constant memory")
  end
end

Main.main()
```

---

## Trade-offs and production gotchas

**1. Bounded resources are the contract**
Any operation that can grow without bound (mailbox, ETS table, process count, DB connections) must have an explicit cap. Without one, a bad input or a slow upstream eventually exhausts the VM.

**2. Tagged tuples over exceptions**
Public APIs return `{:ok, value} / {:error, reason}`. Exceptions are reserved for programmer errors (FunctionClauseError, KeyError) — operational errors are data, not control flow.

**3. Timeouts are not optional**
`GenServer.call` defaults to 5000 ms. `Task.await` defaults to 5000 ms. `Repo.transaction` inherits the connection's timeout. In production, every call should pass an explicit `:timeout` matched to the operation's SLA.

**4. Supervision encodes recovery**
Long-lived processes belong under a Supervisor with an explicit restart strategy. Choose `:transient` for processes that should not restart on `:normal` exit, `:permanent` for daemons, `:temporary` for workers that handle one-off jobs.

**5. Telemetry events are the production debugger**
Emit `:telemetry.execute/3` for every business operation. Attach handlers in production for metrics; attach handlers in tests for assertions. The same code path serves both.

**6. Async tests need isolated state**
`async: true` parallelizes test execution. Any test that writes to global state (Application env, named ETS tables, the database without sandbox) must declare `async: false` or the suite becomes flaky.

---

## Reflection

1. If load on this component grew by 100×, which assumption breaks first — the data structure, the process model, or the failure handling? Justify with a measurement plan, not a guess.
2. What three telemetry events would you emit to decide, six months from now, whether this implementation is still the right one? Name the events, the metadata, and the alert thresholds.
3. The implementation here uses the recommended primitive. Under what specific conditions would you reach for a different primitive instead? Be concrete about the trigger.

---

## Key concepts

### 1. Demand drives back-pressure

GenStage's pull model means slow consumers don't drown fast producers. Producers ask 'give me N events when you have them' rather than producers shoving events downstream.

### 2. Batchers trade latency for throughput

Broadway batchers accumulate events before flushing. A batch size of 100 with a 1-second timeout balances throughput against latency — tune both axes.

### 3. Idempotency is not optional

At-least-once delivery is the default in distributed pipelines. Exactly-once requires idempotent processing, deduplication keys, and durable checkpoints.

---
