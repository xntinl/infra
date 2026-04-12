# `Stream.zip/2` — merging event timelines from independent sources

**Project**: `zip_timelines` — zip two or more independent event streams
(timestamps from different sources) into aligned tuples, then merge them
into a single chronologically-ordered timeline.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Project context

Many real systems have multiple event sources running at independent rates:
one sensor ticks every 100 ms, another every 500 ms, a third on demand.
Sometimes you want them *paired* (the Nth event of each stream together);
sometimes you want them *interleaved chronologically*. `Stream.zip/2` gives
you the first; a custom merge using `Stream.unfold/2` with a peek handles
the second.

Zipping is lazy: it pulls one element from each source per tuple and stops
as soon as *any* source halts. That last rule is critical and trips up
beginners who expect "zip until both exhaust".

Project structure:

```
zip_timelines/
├── lib/
│   └── zip_timelines.ex
├── test/
│   └── zip_timelines_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `Stream.zip/2` halts at the shortest source

```elixir
Stream.zip([1, 2, 3], [:a, :b])
|> Enum.to_list()
# => [{1, :a}, {2, :b}]
```

The third `3` is never pulled because the second stream ran out. This is
the *and* semantics: emit a tuple only when every source has a value.

### 2. `Stream.zip/1` zips a list of streams

For N sources, pass a list:

```elixir
Stream.zip([s1, s2, s3]) |> Enum.to_list()
# => [{v1, v2, v3}, ...]
```

This is how you generalize beyond pairs. Each emission is an N-tuple.

### 3. Zipping is about index alignment, not time alignment

`zip` pairs by *position* in each source, not by any timestamp. If your
sources are timestamped and you want chronological interleaving, `zip` is
the wrong tool — you want a merge based on the event timestamps.

### 4. Chronological merge via peeked sources

To merge N sorted streams into one sorted stream we need to *peek* at the
next element of each, pick the smallest, emit it, and advance that source.
`Stream.unfold/2` with a state of `[{next_value, remaining_stream}, ...]`
does this cleanly. This is a streaming k-way merge — the same algorithm
used by external sort.

---

## Implementation

### Step 1: Create the project

```bash
mix new zip_timelines
cd zip_timelines
```

### Step 2: `lib/zip_timelines.ex`

```elixir
defmodule ZipTimelines do
  @moduledoc """
  Merging multiple event streams by position (`Stream.zip`) and by
  timestamp (custom k-way merge via `Stream.unfold`).
  """

  @type event :: {timestamp :: integer(), source :: atom(), payload :: any()}

  @doc """
  Position-based zip: pairs the Nth event of each source together. Halts
  when any source runs out.
  """
  @spec paired(Enumerable.t(), Enumerable.t()) :: Enumerable.t()
  def paired(a, b), do: Stream.zip(a, b)

  @doc """
  Zips N sources into N-tuples. Same halt rule as `paired/2`: stops at
  the shortest input.
  """
  @spec paired_n([Enumerable.t()]) :: Enumerable.t()
  def paired_n(streams) when is_list(streams), do: Stream.zip(streams)

  @doc """
  Chronologically merges any number of timestamped event streams into a
  single stream, ordered by the event's timestamp (the first element of
  the tuple).

  Each input stream must already be sorted by timestamp; we do not sort
  within a stream — that would require buffering. This is the streaming
  k-way merge algorithm: peek each source, emit the smallest.

  Lazy: if the caller takes only 5 events, at most 5 peeks happen.
  """
  @spec merge_by_time([Enumerable.t()]) :: Enumerable.t()
  def merge_by_time(streams) when is_list(streams) do
    # Convert each stream into a continuation by wrapping it in an Enumerable
    # reducer. We use Stream.transform-style peek via a helper.
    initial = Enum.map(streams, &take_one/1)

    Stream.unfold(initial, fn heads ->
      # Each head is {:ok, event, rest} or :empty. Find the smallest-timestamp one.
      active = for {:ok, _, _} = h <- heads, do: h

      case active do
        [] ->
          nil

        _ ->
          # Pick the head with the earliest timestamp.
          {{:ok, chosen_event, chosen_rest}, index} =
            active
            |> Enum.with_index()
            |> Enum.min_by(fn {{:ok, {ts, _, _}, _}, _i} -> ts end)

          # Replace that slot with the next peek from the chosen source.
          new_heads = replace_slot(heads, chosen_event, chosen_rest, index)
          {chosen_event, new_heads}
      end
    end)
  end

  # ── Helpers ─────────────────────────────────────────────────────────────

  # Takes one element from a stream, returning {:ok, value, remaining_stream}
  # or :empty if the stream is exhausted.
  defp take_one(stream) do
    case Enum.take(stream, 1) do
      [] -> :empty
      [value] -> {:ok, value, Stream.drop(stream, 1)}
    end
  end

  # We replaced the `index`-th ACTIVE head; we need to map that back onto the
  # full `heads` list (which may contain :empty slots). We rebuild by walking
  # once and replacing the matching active slot.
  defp replace_slot(heads, chosen_event, chosen_rest, active_index) do
    {new_heads, _} =
      Enum.map_reduce(heads, 0, fn
        :empty, active_i ->
          {:empty, active_i}

        {:ok, event, _rest} = h, active_i ->
          if active_i == active_index and event == chosen_event do
            {take_one(chosen_rest), active_i + 1}
          else
            {h, active_i + 1}
          end
      end)

    new_heads
  end
end
```

### Step 3: `test/zip_timelines_test.exs`

```elixir
defmodule ZipTimelinesTest do
  use ExUnit.Case, async: true

  describe "paired/2" do
    test "zips two streams by position, halting at the shortest" do
      a = [1, 2, 3]
      b = [:a, :b]
      assert ZipTimelines.paired(a, b) |> Enum.to_list() == [{1, :a}, {2, :b}]
    end

    test "is lazy — only the needed prefix is realized" do
      infinite = Stream.iterate(0, &(&1 + 1))
      assert ZipTimelines.paired(infinite, [:x, :y, :z]) |> Enum.to_list() ==
               [{0, :x}, {1, :y}, {2, :z}]
    end
  end

  describe "paired_n/1" do
    test "three-way zip produces 3-tuples" do
      assert ZipTimelines.paired_n([[1, 2], [:a, :b], [true, false]]) |> Enum.to_list() ==
               [{1, :a, true}, {2, :b, false}]
    end
  end

  describe "merge_by_time/1" do
    test "merges two sorted event streams chronologically" do
      sensor_a = [{100, :a, :hot}, {300, :a, :cold}, {500, :a, :hot}]
      sensor_b = [{150, :b, :wet}, {400, :b, :dry}]

      result = ZipTimelines.merge_by_time([sensor_a, sensor_b]) |> Enum.to_list()

      assert result == [
               {100, :a, :hot},
               {150, :b, :wet},
               {300, :a, :cold},
               {400, :b, :dry},
               {500, :a, :hot}
             ]
    end

    test "handles an empty source alongside non-empty ones" do
      assert ZipTimelines.merge_by_time([[], [{1, :x, :y}]]) |> Enum.to_list() ==
               [{1, :x, :y}]
    end

    test "is lazy — taking the first few events does not drain sources" do
      sensor_a = [{1, :a, 1}, {3, :a, 3}, {5, :a, 5}]
      sensor_b = [{2, :b, 2}, {4, :b, 4}, {6, :b, 6}]

      assert ZipTimelines.merge_by_time([sensor_a, sensor_b]) |> Enum.take(3) ==
               [{1, :a, 1}, {2, :b, 2}, {3, :a, 3}]
    end

    test "single source passes through unchanged" do
      events = [{10, :x, :ping}, {20, :x, :pong}]
      assert ZipTimelines.merge_by_time([events]) |> Enum.to_list() == events
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

**1. `zip` stopping at the shortest source is often not what you want**
If you want "zip until both exhaust, padding with `nil`", use
`Stream.zip_with/2` with an explicit merge function and `Enum.concat/2` on
tails, or fall back to materializing and padding with `Enum.zip/2` —
ergonomics is not `zip`'s strong suit for ragged sources.

**2. `merge_by_time` with many sources needs a min-heap**
Our implementation picks the min by scanning — O(k) per emit for k
sources. Fine for k=2..20; for k in the hundreds (large fan-in), replace
the scan with a priority queue (`:gb_trees` or an Erlang library). The
algorithmic shape stays the same — it's still streaming.

**3. Peeking via `Enum.take(stream, 1) |> Stream.drop(stream, 1)` is
stream-dependent**
Some streams are single-pass (e.g., ones built from `Stream.resource/3`
around a file handle) and calling `take` then `drop` reopens/rewinds in
surprising ways. `File.stream!/1` reopens on each enumeration, which is
fine but costly. For true one-shot sources, wrap them in a process you
can poll (a GenStage or a simple GenServer) before merging.

**4. Sources must be pre-sorted**
`merge_by_time` does *not* sort within a stream. If you feed it unsorted
events, you get incorrect merged output. Either pre-sort (which kills
laziness) or consume already-sorted sources (most timestamped streams
are ordered by construction).

**5. Tuples vs structs as events**
We use `{ts, source, payload}` for simplicity. In production, a
`defstruct` (with `@enforce_keys [:ts]`) gives better errors if a source
emits malformed events. The merge logic just needs a consistent `ts`
accessor.

**6. When NOT to use `Stream.zip` or this merge**
- For joining two logically-related collections (users + addresses by
  user_id), use a `Map` keyed by the join column — zipping by position
  is brittle.
- When each source is actually a live process emitting events, use
  `GenStage` with a dispatcher — `Stream.zip` is for pull-based
  enumerables, not push-based processes.

---

## Resources

- [`Stream.zip/1,2` — hexdocs](https://hexdocs.pm/elixir/Stream.html#zip/1)
- [`Stream.zip_with/2,3` — hexdocs](https://hexdocs.pm/elixir/Stream.html#zip_with/2) — custom combiner
- [Wikipedia — k-way merge](https://en.wikipedia.org/wiki/K-way_merge_algorithm) — the algorithm behind `merge_by_time/1`
- [`:gb_trees` — Erlang docs](https://www.erlang.org/doc/man/gb_trees.html) — priority-queue-like structure for scaling the merge to many sources
- Saša Jurić — *Elixir in Action*, section on composing lazy enumerables
