# `Stream.transform/3` — stateful transformations over a stream

**Project**: `transform_stateful` — use `Stream.transform/3` with an
accumulator to detect duplicates, running-sum, and emit zero, one, or
many elements per input.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Why stream transform stateful matters

`Stream.map/2` is 1-to-1: one element in, one element out, stateless.
`Stream.filter/2` is 1-to-0-or-1: drop or keep. Neither can express a
stateful transform like "drop duplicates" or "running sum", where the
decision depends on everything seen so far.

`Stream.transform/3` is the general case. It threads an accumulator
through the stream and, per input, lets you emit *zero, one, or many*
output elements. It's the bridge between `Enum.reduce/3` (which folds
to a single value) and `Stream.map/2` (which is stateless). Mastering it
lets you express almost any streaming transformation without falling
back to recursion.

---

## Project structure

```
transform_stateful/
├── lib/
│   └── transform_stateful.ex
├── script/
│   └── main.exs
├── test/
│   └── transform_stateful_test.exs
└── mix.exs
```

---

## Core concepts

### 1. The shape of `Stream.transform/3`

```elixir
Stream.transform(enum, initial_acc, fn element, acc ->
  {events_to_emit, new_acc}  # continue
  # or
  {:halt, acc}               # stop
end)
```
Per input, you choose what to emit (a list — possibly empty, possibly
many) and how to update state. Returning `{:halt, _}` stops the stream
early, letting any `after` clause in a resource clean up.

### 2. Detecting duplicates: acc is a MapSet

```elixir
Stream.transform(stream, MapSet.new(), fn x, seen ->
  if MapSet.member?(seen, x), do: {[], seen}, else: {[x], MapSet.put(seen, x)}
end)
```
Emit on first occurrence, drop on repeat. This is `Stream.uniq/1`
implemented by hand — and the standard library's version does almost
exactly this.

### 3. Running sum: emit one enriched element per input

```elixir
Stream.transform(stream, 0, fn x, total ->
  new_total = total + x
  {[new_total], new_total}
end)
```
State *and* output both use the running total.

### 4. One-to-many: expanding each element

Returning a list of *multiple* events per input turns one element into
many downstream — useful for explosion patterns (replicating, enumerating
sub-elements). The stream stays lazy: downstream pulls one expanded
element at a time.

### 5. Compared to `Enum.scan/3`

`Enum.scan/3` / `Stream.scan/2` is the 1-to-1-with-acc shortcut — simpler
syntax when you always emit exactly one element per input. Reach for
`transform/3` when you need to emit zero or more than one.

---

## Design decisions

**Option A — Ad-hoc implementation without OTP primitives**
- Pros: Less ceremony; the stream transform stateful flow fits in a single short module.
- Cons: Reinvents supervision, restart, back-pressure, and observability — the four properties OTP gives us for free.

**Option B — Use the canonical OTP shape for stream transform stateful** (chosen)
- Pros: Predictable failure semantics; integrates with `:observer`, telemetry, and supervision trees; future maintainers recognise the pattern.
- Cons: One extra layer of indirection; you must learn the callback shape and the lifecycle rules.

Chose **B** because the abstraction cost is paid once and its benefits are paid every day — especially in production where partial failure is the norm, not the exception.

## Implementation

### `mix.exs`

```elixir
defmodule TransformStateful.MixProject do
  use Mix.Project

  def project do
    [
      app: :transform_stateful,
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

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.

```bash
mix new transform_stateful
cd transform_stateful
```

### `lib/transform_stateful.ex`

**Objective**: Implement `transform_stateful.ex` — the lazy operator whose resource and memory profile only becomes visible when the stream is actually run.

```elixir
defmodule TransformStateful do
  @moduledoc """
  Stateful stream transformations built on `Stream.transform/3`.
  Each function keeps constant additional memory relative to the acc it
  uses — `running_sum/1` is O(1); `dedup/1` is O(n_unique).
  """

  @doc """
  Removes duplicates lazily. Equivalent to `Stream.uniq/1`, reimplemented
  to show the mechanism: the acc is a `MapSet` of values seen so far.
  """
  @spec dedup(Enumerable.t()) :: Enumerable.t()
  def dedup(stream) do
    Stream.transform(stream, MapSet.new(), fn element, seen ->
      if MapSet.member?(seen, element) do
        {[], seen}
      else
        {[element], MapSet.put(seen, element)}
      end
    end)
  end

  @doc """
  Running sum: emits the cumulative total at each step.

    running_sum([1, 2, 3, 4]) => [1, 3, 6, 10]
  """
  @spec running_sum(Enumerable.t()) :: Enumerable.t()
  def running_sum(stream) do
    Stream.transform(stream, 0, fn x, total ->
      new_total = total + x
      {[new_total], new_total}
    end)
  end

  @doc """
  Takes elements from the stream while `pred.(element, acc)` is truthy,
  threading an acc so the predicate can change behaviour based on past
  inputs.

  Example: take while the running sum is under a cap.
  """
  @spec take_while_running(Enumerable.t(), any(), (any(), any() -> {boolean(), any()})) ::
          Enumerable.t()
  def take_while_running(stream, initial_acc, fun) do
    Stream.transform(stream, initial_acc, fn element, acc ->
      case fun.(element, acc) do
        {true, new_acc} -> {[element], new_acc}
        {false, _} -> {:halt, acc}
      end
    end)
  end

  @doc """
  Expands each element into N copies. Demonstrates the *one-to-many*
  variant of `Stream.transform/3` — the events list has more than one
  entry, but the stream stays lazy and is driven by downstream demand.
  """
  @spec replicate_each(Enumerable.t(), pos_integer()) :: Enumerable.t()
  def replicate_each(stream, n) when is_integer(n) and n > 0 do
    Stream.transform(stream, nil, fn element, _acc ->
      {List.duplicate(element, n), nil}
    end)
  end
end
```
### Step 3: `test/transform_stateful_test.exs`

**Objective**: Write `transform_stateful_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.

```elixir
defmodule TransformStatefulTest do
  use ExUnit.Case, async: true

  doctest TransformStateful

  describe "dedup/1" do
    test "keeps only first occurrence" do
      assert TransformStateful.dedup([1, 2, 1, 3, 2, 4]) |> Enum.to_list() ==
               [1, 2, 3, 4]
    end

    test "lazy — large duplicate-heavy stream only keeps uniques in memory" do
      # 10_000 elements, only 10 unique values.
      input = Stream.cycle(1..10) |> Stream.take(10_000)
      assert TransformStateful.dedup(input) |> Enum.to_list() == Enum.to_list(1..10)
    end
  end

  describe "running_sum/1" do
    test "emits cumulative totals" do
      assert TransformStateful.running_sum([1, 2, 3, 4]) |> Enum.to_list() ==
               [1, 3, 6, 10]
    end

    test "empty stream yields nothing" do
      assert TransformStateful.running_sum([]) |> Enum.to_list() == []
    end
  end

  describe "take_while_running/3" do
    test "stops as soon as predicate returns false" do
      # Take while running sum <= 10.
      result =
        TransformStateful.take_while_running([1, 2, 3, 4, 5, 6, 7], 0, fn x, sum ->
          new_sum = sum + x
          {new_sum <= 10, new_sum}
        end)
        |> Enum.to_list()

      # 1 (sum=1), 2 (sum=3), 3 (sum=6), 4 (sum=10) — next would be 15 > 10.
      assert result == [1, 2, 3, 4]
    end

    test "halt semantics — downstream sees the stream as finished" do
      result =
        TransformStateful.take_while_running(Stream.iterate(1, &(&1 + 1)), 0, fn x, sum ->
          {sum < 10, sum + x}
        end)
        |> Enum.to_list()

      # Emits 1, 2, 3, 4 — sum starts at 0, after emitting 4 sum becomes 10 -> halt on 5.
      assert result == [1, 2, 3, 4]
    end
  end

  describe "replicate_each/2" do
    test "one-to-many expansion" do
      assert TransformStateful.replicate_each([:a, :b], 3) |> Enum.to_list() ==
               [:a, :a, :a, :b, :b, :b]
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

### `script/main.exs`

```elixir
defmodule Main do
  defmodule TransformStateful do
    @moduledoc """
    Stateful stream transformations built on `Stream.transform/3`.
    Each function keeps constant additional memory relative to the acc it
    uses — `running_sum/1` is O(1); `dedup/1` is O(n_unique).
    """

    @doc """
    Removes duplicates lazily. Equivalent to `Stream.uniq/1`, reimplemented
    to show the mechanism: the acc is a `MapSet` of values seen so far.
    """
    @spec dedup(Enumerable.t()) :: Enumerable.t()
    def dedup(stream) do
      Stream.transform(stream, MapSet.new(), fn element, seen ->
        if MapSet.member?(seen, element) do
          {[], seen}
        else
          {[element], MapSet.put(seen, element)}
        end
      end)
    end

    @doc """
    Running sum: emits the cumulative total at each step.

      running_sum([1, 2, 3, 4]) => [1, 3, 6, 10]
    """
    @spec running_sum(Enumerable.t()) :: Enumerable.t()
    def running_sum(stream) do
      Stream.transform(stream, 0, fn x, total ->
        new_total = total + x
        {[new_total], new_total}
      end)
    end

    @doc """
    Takes elements from the stream while `pred.(element, acc)` is truthy,
    threading an acc so the predicate can change behaviour based on past
    inputs.

    Example: take while the running sum is under a cap.
    """
    @spec take_while_running(Enumerable.t(), any(), (any(), any() -> {boolean(), any()})) ::
            Enumerable.t()
    def take_while_running(stream, initial_acc, fun) do
      Stream.transform(stream, initial_acc, fn element, acc ->
        case fun.(element, acc) do
          {true, new_acc} -> {[element], new_acc}
          {false, _} -> {:halt, acc}
        end
      end)
    end

    @doc """
    Expands each element into N copies. Demonstrates the *one-to-many*
    variant of `Stream.transform/3` — the events list has more than one
    entry, but the stream stays lazy and is driven by downstream demand.
    """
    @spec replicate_each(Enumerable.t(), pos_integer()) :: Enumerable.t()
    def replicate_each(stream, n) when is_integer(n) and n > 0 do
      Stream.transform(stream, nil, fn element, _acc ->
        {List.duplicate(element, n), nil}
      end)
    end
  end

  def main do
    IO.puts("TransformStateful OK")
  end

end

Main.main()
```
## Trade-offs and production gotchas

**1. The accumulator lives for the entire stream**
`dedup` keeps a MapSet of every unique value — memory grows with the
cardinality. For bounded-cardinality streams this is fine; for unbounded
ones (user IDs across forever), use a bloom filter or a sliding
deduplication window instead.

**2. `{:halt, acc}` does NOT run an `after` block**
Unlike `Stream.resource/3`, `transform/3` has no built-in cleanup hook.
If you need to close a handle when halting, use `Stream.transform/4` (the
four-arity variant with `after_fun`) or wrap the source with
`Stream.resource/3` so cleanup lives there.

**3. Emitting many elements builds an intermediate list**
Each call to the transform function returns a list. If you return
`Enum.to_list(1..1_000_000)` for a single input, you allocate a million-
element list for that one step, defeating laziness. Prefer returning a
small number of elements per step; for big fan-outs, use `Stream.flat_map/2`
with another stream inside.

**4. It's still sequential**
`Stream.transform/3` runs on the consumer process. The state thread makes
parallelism hard by definition — you can't split a sequential dependency
across workers. If the step function is expensive but stateless, don't
use `transform/3`; use `Task.async_stream/3`.

**5. Emit order is preserved**
Obvious but worth saying: events emitted within one step appear in the
output in the order you listed them, and steps are visited in source
order. You can rely on total ordering downstream.

**6. When NOT to use `Stream.transform/3`**
- Pure 1-to-1 mapping → `Stream.map/2`.
- Pure filtering → `Stream.filter/2`.
- Running aggregate emitting once per input → `Stream.scan/2` is shorter.
- When the "state" is actually a resource (file, port, ETS) → use
  `Stream.resource/3` so cleanup is guaranteed.

## Resources

- [`Stream.transform/3` — hexdocs](https://hexdocs.pm/elixir/Stream.html#transform/3)
- [`Stream.transform/4`](https://hexdocs.pm/elixir/Stream.html#transform/4) — with `after_fun` for cleanup
- [`Stream.scan/2`](https://hexdocs.pm/elixir/Stream.html#scan/2) — simpler 1-to-1-with-acc
- [`Stream.uniq/1` source](https://github.com/elixir-lang/elixir/blob/main/lib/elixir/lib/stream.ex) — see the implementation for a real-world use of `transform`
- José Valim — Elixir `Stream` announcement: <https://elixir-lang.org/blog/2013/08/21/elixir-streams/>

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

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

### `test/transform_stateful_test.exs`

```elixir
defmodule TransformStatefulTest do
  use ExUnit.Case, async: true

  doctest TransformStateful

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert TransformStateful.run(:noop) == :ok
    end
  end
end
```
---

## Key concepts

### 1. Model the problem with the right primitive

Choose the OTP primitive that matches the failure semantics of the problem: `GenServer` for stateful serialization, `Task` for fire-and-forget async, `Agent` for simple shared state, `Supervisor` for lifecycle management. Reaching for the wrong primitive is the most common source of accidental complexity in Elixir systems.

### 2. Make invariants explicit in code

Guards, pattern matching, and `@spec` annotations turn invariants into enforceable contracts. If a value *must* be a positive integer, write a guard — do not write a comment. The compiler and Dialyzer will catch what documentation cannot.

### 3. Let it crash, but bound the blast radius

"Let it crash" is not permission to ignore failures — it is a directive to design supervision trees that contain them. Every process should be supervised, and every supervisor should have a restart strategy that matches the failure mode it is recovering from.
