# Flow — parallel word count over a large file

**Project**: `parallel_wordcount` — count word occurrences in a large text
file in parallel, using `Flow` partitions to do the reduction across
multiple cores.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

### Dependencies (`mix.exs`)

```elixir
def deps do
  {exunit},
  {flow},
end
```
## Project context

`Flow` is to `GenStage` what `Stream` is to `Enum` — a higher-level,
composable API. Under the hood each `Flow` stage is a tree of GenStage
stages arranged for data parallelism. You describe the computation with
familiar `Enum`-like functions (`map`, `filter`, `reduce`), and Flow
distributes the work across `System.schedulers_online()` worker
processes.

Word count is the "hello world" of map/reduce because it exposes the
core challenge: counting needs a reduction, but naive parallel reduction
over a shared accumulator serializes. Flow solves this with
**partitioning**: a hash of the key routes each word to a fixed worker,
so each worker maintains its own local map and no locking is ever needed.

Project structure:

```
parallel_wordcount/
├── lib/
│   └── parallel_wordcount.ex
├── test/
│   └── parallel_wordcount_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `Flow.from_enumerable/2` as the source

Any enumerable works — `File.stream!/1`, a `List`, a `Range`, a custom
`Stream`. Flow wraps it in a producer stage that distributes elements to
downstream worker processes. For I/O-bound sources, pair with
`:max_demand` to avoid over-eager reading.

### 2. Map phase — stateless, embarrassingly parallel

```elixir
flow
|> Flow.flat_map(&String.split(&1, ~r/\W+/, trim: true))
|> Flow.map(&String.downcase/1)
```

Each transformation runs on each worker independently. No coordination,
no shared state — linear speedup up to scheduler count.

### 3. `Flow.partition/2` — the shuffle

```elixir
|> Flow.partition(stages: 4)
```

After partitioning, events are routed to a fixed worker based on the
hash of the element (or a specified key). This is the same idea as
MapReduce's shuffle step. Now a *reduce* on the partitioned flow sees
*all occurrences of the same key* on one worker — so local reductions
are correct.

### 4. `Flow.reduce/3` — local accumulators per partition

```elixir
|> Flow.reduce(fn -> %{} end, fn word, acc ->
  Map.update(acc, word, 1, &(&1 + 1))
end)
```

`Flow.reduce/3` builds a *per-partition* accumulator. After the flow
completes, `Enum.to_list/1` emits each partition's accumulator as an
event. We merge them with a final `Enum.reduce/3` — trivial because each
partition owns a disjoint set of keys.

### 5. Order is NOT preserved

Flow is unordered by design — events are interleaved across workers. If
you need ordering, use `Stream` instead, or wrap ordered blocks into
events themselves.

---

## Implementation

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.


```bash
mix new parallel_wordcount
cd parallel_wordcount
```

Add `flow` to `mix.exs`:

```elixir
defp deps, do: [{:flow, "~> 1.2"}]
```

Then `mix deps.get`.

### Step 2: `lib/parallel_wordcount.ex`

**Objective**: Implement `parallel_wordcount.ex` — the lazy operator whose resource and memory profile only becomes visible when the stream is actually run.


```elixir
defmodule ParallelWordcount do
  @moduledoc """
  Parallel word count over a file using `Flow`. Demonstrates map
  (tokenize, normalize), partition (by word), reduce (per-partition
  accumulator), and final merge.
  """

  @doc """
  Counts case-insensitive word occurrences in `path`. Returns a map of
  `%{word => count}`.

  Flow parallelizes across `stages` worker processes (default:
  `System.schedulers_online()`).
  """
  @spec count_file(Path.t(), keyword()) :: %{String.t() => pos_integer()}
  def count_file(path, opts \\ []) do
    stages = Keyword.get(opts, :stages, System.schedulers_online())

    path
    |> File.stream!([], :line)
    |> count_stream(stages)
  end

  @doc """
  Counts words in an arbitrary line stream. Extracted so tests can inject
  a small in-memory stream without hitting disk.
  """
  @spec count_stream(Enumerable.t(), pos_integer()) :: %{String.t() => pos_integer()}
  def count_stream(line_stream, stages) when is_integer(stages) and stages > 0 do
    line_stream
    |> Flow.from_enumerable(stages: stages, max_demand: 100)
    # Tokenize each line into words. flat_map emits 0..N events per input.
    |> Flow.flat_map(&tokenize/1)
    # Shuffle: route every occurrence of `word` to the same reducer.
    |> Flow.partition(stages: stages)
    # Per-partition accumulator — each partition builds its own map.
    |> Flow.reduce(fn -> %{} end, fn word, acc ->
      Map.update(acc, word, 1, &(&1 + 1))
    end)
    # Flow emits each partition's accumulator as a separate event once
    # the source is exhausted. Merge the partition maps into the final
    # result — since partitioning routed unique keys per partition, the
    # per-key sums never collide across partitions in correctness terms.
    |> Enum.reduce(%{}, fn partition_map, acc ->
      Map.merge(acc, partition_map, fn _word, a, b -> a + b end)
    end)
  end

  # Extracted so the regex is compiled once (module attribute) and the
  # tokenization logic is obvious and testable on its own.
  @word_splitter ~r/\W+/u
  defp tokenize(line) do
    line
    |> String.downcase()
    |> String.split(@word_splitter, trim: true)
  end
end
```

### Step 3: `test/parallel_wordcount_test.exs`

**Objective**: Write `parallel_wordcount_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule ParallelWordcountTest do
  use ExUnit.Case, async: true

  describe "count_stream/2" do
    test "counts words case-insensitively" do
      lines = ["The quick brown fox", "jumps over the Lazy dog", "The dog."]
      result = ParallelWordcount.count_stream(lines, 2)

      assert result == %{
               "the" => 3,
               "quick" => 1,
               "brown" => 1,
               "fox" => 1,
               "jumps" => 1,
               "over" => 1,
               "lazy" => 1,
               "dog" => 2
             }
    end

    test "single stage produces same result as multiple stages" do
      lines = ["a a b c", "b c c d", "a d d"]
      one_stage = ParallelWordcount.count_stream(lines, 1)
      four_stage = ParallelWordcount.count_stream(lines, 4)
      assert one_stage == four_stage
    end

    test "handles empty stream" do
      assert ParallelWordcount.count_stream([], 2) == %{}
    end
  end

  describe "count_file/2" do
    @tag :tmp_file
    test "counts words in a real file" do
      path = Path.join(System.tmp_dir!(), "parallel_wc_#{:erlang.unique_integer([:positive])}.txt")

      on_exit(fn -> File.rm(path) end)

      File.write!(path, """
      foo bar
      BAR baz
      foo FOO
      """)

      result = ParallelWordcount.count_file(path, stages: 2)
      assert result == %{"foo" => 3, "bar" => 2, "baz" => 1}
    end
  end
end
```

### Step 4: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
```

---

## Trade-offs and production gotchas

**1. Parallelism overhead beats serial on small inputs**
Starting a Flow pipeline launches N GenStage processes and a partitioner.
For inputs that fit in microseconds of `Enum.reduce`, this is strictly
slower. Benchmark before assuming "parallel = faster" — the crossover is
typically around tens of thousands of elements, depending on per-element
work.

**2. `stages: System.schedulers_online()` is a good default, not a rule**
For CPU-bound work, scheduler count is the sweet spot. For I/O-bound
work (HTTP, DB), `stages: 2 * schedulers_online()` or more can help
because processes spend most of their time waiting. Profile; don't
cargo-cult.

**3. Partitioning is a hash, not a sort**
`Flow.partition/2` routes events by hash. Workers see *all* events for
their hash bucket, but not in any particular order within. If you need
ordered per-key processing (e.g., sequential state updates per user ID),
that's exactly what partitioning guarantees *within a single run* —
same key always to the same worker. Across restarts, assignments change.

**4. Reducer accumulators live in worker memory**
A `Flow.reduce/3` that builds a 2GB map pins 2GB per worker. With 8
workers that's 16GB. For very high-cardinality reductions (unique-value
counts), consider emitting intermediate writes to disk or ETS.

**5. Error in one worker stops the flow**
Unhandled exceptions propagate and tear down the pipeline. For
partial-failure tolerance, catch inside the map/reduce function and
emit error markers as events — or run each independent unit in a
`Task.async_stream/3` with `on_timeout: :kill_task`.

**6. Flow is not a queue, it's a computation**
Each `Flow` run processes a finite source. For long-lived streams
(Kafka topics, continuous ingestion), use `Broadway` — it's built on
GenStage for the same reason but with acknowledgements and rate control.

**7. When NOT to use Flow**
- Data fits in memory and work is cheap → plain `Enum` is faster.
- You need total ordering → `Stream`.
- You need durability, retries, rate-limits → `Broadway`.
- You're doing pure per-element transformation with no reduction →
  `Task.async_stream/3` is simpler than a full Flow pipeline.

---

## Executable Example

Copy the code below into a file (e.g., `solution.exs`) and run with `elixir solution.exs`:

```elixir
defmodule Main do
  defmodule Solution do
    def main do
      IO.puts("=== Lazy Stream Example ===")
    
      result = 1..100
        |> Stream.map(&(&1 * 2))
        |> Stream.filter(&(rem(&1, 4) == 0))
        |> Stream.take(10)
        |> Enum.to_list()
    
      IO.puts("First 10 multiples of 4 from 1-100:")
      IO.inspect(result)

      IO.puts("\n=== Unfold Stream ===")
      fib = Stream.unfold({0, 1}, fn {a, b} -> {a, {b, a + b}} end)
        |> Stream.take(10)
    
      IO.puts("First 10 Fibonacci numbers:")
      IO.inspect(Enum.to_list(fib))
    end
  end

  def main do
    IO.puts("Solution OK")
  end

end

Main.main()
```


## Resources

- [`Flow` — hexdocs](https://hexdocs.pm/flow/Flow.html)
- [`Flow.partition/2` — hexdocs](https://hexdocs.pm/flow/Flow.html#partition/2)
- [José Valim — "Announcing Flow"](https://elixir-lang.org/blog/2016/07/14/announcing-genstage/) — Flow is introduced alongside GenStage in the original announcement
- [Plataformatec — Flow wordcount example](https://github.com/dashbitco/flow) — the canonical example, very close to this exercise
- [`Task.async_stream/3`](https://hexdocs.pm/elixir/Task.html#async_stream/3) — the simpler alternative for stateless parallel map
- [`Broadway`](https://hexdocs.pm/broadway/Broadway.html) — Flow's streaming cousin for ingestion pipelines


## Deep Dive

Streams are lazy, composable data pipelines that process one element at a time without materializing intermediate collections. This is fundamentally different from Enum, which materializes the entire dataset before the next operation.

**Lazy evaluation semantics:**
Stream operations return a `%Stream{}` struct containing a function. The actual computation is deferred until consumed by a terminal operation (`.run()`, `Enum.to_list()`, etc.). This allows streams to:
- Chain indefinite sequences (e.g., `Stream.iterate(0, &(&1 + 1))`)
- Transform without memory bloat (e.g., processing multi-gigabyte files)
- Compose reusable pipelines as first-class values

**Resource lifecycle in streams:**
Streams wrapping resources (`Stream.resource/3`) must define cleanup functions. A stream created from a file remains "open" (in terms of the lambda) until the consumer finishes or errors. If the consumer crashes or stops early, the cleanup function still runs — critical for proper file/socket/port management.

**Backpressure and demand:**
Unlike streams in other languages, Elixir's synchronous streams don't inherently implement backpressure. Backpressure is demand-based: the consumer pulls data at its own pace. `GenStage` and `Flow` add explicit backpressure — the producer waits for the consumer to request more elements. This is why benchmarking matters: a naive stream consumer can overwhelm memory if the pipeline produces faster than it consumes.
