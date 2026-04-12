# `Stream.chunk_every/4` — batching for stream processing

**Project**: `chunk_batch` — batch a stream of events with explicit control
over chunk size, step (stride), and leftover handling.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Project context

Batching is everywhere: sending N records per database insert, uploading
files in S3 multipart chunks, plotting a moving average, writing telemetry
every K events. A loop with a local accumulator works, but it's verbose
and doesn't compose. `Stream.chunk_every/4` gives you the batching
primitive — and its less-obvious arguments (step and leftover) make it
powerful enough to express moving windows, strided reads, and forced flush.

This exercise builds three small utilities on top of `chunk_every/4` so you
see each argument in isolation.

Project structure:

```
chunk_batch/
├── lib/
│   └── chunk_batch.ex
├── test/
│   └── chunk_batch_test.exs
└── mix.exs
```

---

## Core concepts

### 1. The four arguments

```elixir
Stream.chunk_every(enum, count, step \\ count, leftover \\ [])
```

- `count` — size of each chunk.
- `step` — how many elements to advance before starting the next chunk.
  `step == count` = non-overlapping (default). `step < count` = sliding
  window. `step > count` = strided sampling.
- `leftover` — what to do with the final partial chunk:
  - `[]` (default) — emit the partial chunk as-is.
  - `:discard` — drop the partial chunk.
  - any list — pad the partial chunk with elements from this list.

### 2. Non-overlapping batches

```elixir
Stream.chunk_every(1..10, 3)
# => [[1,2,3], [4,5,6], [7,8,9], [10]]
```

The last chunk is short; default leftover `[]` emits it anyway.

### 3. Sliding window

```elixir
Stream.chunk_every(1..6, 3, 1, :discard)
# => [[1,2,3], [2,3,4], [3,4,5], [4,5,6]]
```

`step: 1` advances one at a time, so each window overlaps the previous by
`count - step` elements. `:discard` drops the tail windows that can't be
filled. Essential for moving averages and n-gram analyses.

### 4. `:discard` vs padding

```elixir
Stream.chunk_every([1, 2, 3, 4, 5], 3, 3, :discard)
# => [[1,2,3]]          # drop [4,5]

Stream.chunk_every([1, 2, 3, 4, 5], 3, 3, [0, 0])
# => [[1,2,3], [4,5,0]] # pad [4,5] with leading elements of [0,0]
```

Padding is useful when downstream requires uniform shape (FFT, matrix ops,
fixed-schema writes).

---

## Implementation

### Step 1: Create the project

```bash
mix new chunk_batch
cd chunk_batch
```

### Step 2: `lib/chunk_batch.ex`

```elixir
defmodule ChunkBatch do
  @moduledoc """
  Three stream utilities built on `Stream.chunk_every/4`: fixed-size
  batching with optional forced flush, sliding windows, and n-gram
  generation.
  """

  @doc """
  Batches `stream` into lists of at most `size`. By default the trailing
  partial batch is kept; pass `:discard` to drop it.

  Useful for bulk operations where each batch is sent independently
  (DB inserts, HTTP POSTs, log lines).
  """
  @spec batches(Enumerable.t(), pos_integer(), :keep | :discard) :: Enumerable.t()
  def batches(stream, size, tail \\ :keep)
      when is_integer(size) and size > 0 and tail in [:keep, :discard] do
    leftover = if tail == :discard, do: :discard, else: []
    Stream.chunk_every(stream, size, size, leftover)
  end

  @doc """
  Emits a moving window of size `size` advancing `1` element at a time.
  The tail partial windows are discarded so every emitted window is
  exactly `size` long — which is what downstream averages/variances
  assume.
  """
  @spec sliding(Enumerable.t(), pos_integer()) :: Enumerable.t()
  def sliding(stream, size) when is_integer(size) and size > 0 do
    Stream.chunk_every(stream, size, 1, :discard)
  end

  @doc """
  Produces consecutive n-grams from a stream of tokens. Identical to
  `sliding/2` but renamed for intent — "sliding window of strings" is
  exactly what "n-grams" means in NLP.
  """
  @spec ngrams(Enumerable.t(), pos_integer()) :: Enumerable.t()
  def ngrams(stream, n) when is_integer(n) and n > 0 do
    sliding(stream, n)
  end

  @doc """
  Classic use-case: compute a moving average over a numeric stream.
  Demonstrates composing `sliding/2` with a simple `Stream.map/2`.
  """
  @spec moving_average(Enumerable.t(), pos_integer()) :: Enumerable.t()
  def moving_average(stream, window) when is_integer(window) and window > 0 do
    stream
    |> sliding(window)
    |> Stream.map(fn chunk -> Enum.sum(chunk) / window end)
  end
end
```

### Step 3: `test/chunk_batch_test.exs`

```elixir
defmodule ChunkBatchTest do
  use ExUnit.Case, async: true

  describe "batches/3" do
    test "non-overlapping fixed-size batches, default keep tail" do
      assert ChunkBatch.batches(1..10, 3) |> Enum.to_list() ==
               [[1, 2, 3], [4, 5, 6], [7, 8, 9], [10]]
    end

    test "discard drops the short trailing batch" do
      assert ChunkBatch.batches(1..10, 3, :discard) |> Enum.to_list() ==
               [[1, 2, 3], [4, 5, 6], [7, 8, 9]]
    end

    test "exact multiple has no tail to worry about" do
      assert ChunkBatch.batches(1..9, 3) |> Enum.to_list() ==
               [[1, 2, 3], [4, 5, 6], [7, 8, 9]]
    end

    test "lazy — only the first batch is materialized for take(1)" do
      # An infinite source; batches/3 must not blow up.
      infinite = Stream.iterate(1, &(&1 + 1))
      assert ChunkBatch.batches(infinite, 3) |> Enum.take(2) ==
               [[1, 2, 3], [4, 5, 6]]
    end
  end

  describe "sliding/2" do
    test "emits every contiguous window of exact size" do
      assert ChunkBatch.sliding(1..6, 3) |> Enum.to_list() ==
               [[1, 2, 3], [2, 3, 4], [3, 4, 5], [4, 5, 6]]
    end

    test "input smaller than window yields no output" do
      assert ChunkBatch.sliding([1, 2], 3) |> Enum.to_list() == []
    end
  end

  describe "ngrams/2" do
    test "bigrams over a token stream" do
      tokens = ["the", "quick", "brown", "fox"]
      assert ChunkBatch.ngrams(tokens, 2) |> Enum.to_list() ==
               [["the", "quick"], ["quick", "brown"], ["brown", "fox"]]
    end
  end

  describe "moving_average/2" do
    test "3-point moving average of 1..6" do
      # Windows: [1,2,3]=2.0, [2,3,4]=3.0, [3,4,5]=4.0, [4,5,6]=5.0
      assert ChunkBatch.moving_average(1..6, 3) |> Enum.to_list() ==
               [2.0, 3.0, 4.0, 5.0]
    end
  end
end
```

### Step 4: Run

```bash
mix test
```

---

## Trade-offs and production gotchas

**1. Memory per chunk, not per stream**
Each chunk is a list fully held in memory when emitted. For `size: 10_000`
over 10M-element streams, you use O(10_000) at any moment — great. Don't
pick chunk sizes so large that a single chunk dominates memory.

**2. Timed flushing is NOT built in**
`chunk_every` only flushes on count or end-of-stream. For "flush every N
events OR every T ms, whichever comes first" (common in log shippers,
metrics agents), you need `GenStage` with a `:buffer` or a GenServer with
a self-scheduled tick — `chunk_every` cannot do time-based flush.

**3. Leftover padding uses elements from the list, not repeats**
`leftover: [0, 0]` is `[0, 0]` *once* — if the partial chunk is 5 short
and leftover is only 2 long, you get 2 padding elements and that's it.
Use `Stream.cycle/1` or construct the leftover list with the required
length if you need deterministic padding.

**4. `:discard` silently drops data**
A production bug I've shipped more than once: batch writer with
`:discard` drops the last 1..size events on shutdown. Default to `:keep`
(and flush a short batch) unless your use case truly doesn't care about
tail data.

**5. Sliding windows over mutable things are tricky**
If chunks are references to large maps/records rather than the records
themselves, a sliding window keeps `size` refs alive at a time and blocks
GC. For high-memory elements, either reduce the window or transform to a
smaller representation before windowing.

**6. When NOT to use `chunk_every`**
- For streaming aggregation (running sum, exp moving average),
  `Stream.transform/3` with a scalar accumulator is O(1) memory per step,
  not O(window). See exercise 106.
- For time-windowed batching, use GenStage / Broadway with explicit
  buffer options — see exercise 108.

---

## Resources

- [`Stream.chunk_every/4` — hexdocs](https://hexdocs.pm/elixir/Stream.html#chunk_every/4)
- [`Enum.chunk_every/4` — the strict sibling](https://hexdocs.pm/elixir/Enum.html#chunk_every/4)
- [`Stream.chunk_while/4`](https://hexdocs.pm/elixir/Stream.html#chunk_while/4) — when "when to emit" is runtime-determined
- [José Valim — Elixir 1.5 release notes](https://elixir-lang.org/blog/2017/07/25/elixir-v1-5-0-released/) — when `chunk_every` replaced the older `chunk/2,4` API
