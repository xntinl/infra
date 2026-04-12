# Streams and lazy evaluation — measuring the memory difference

**Project**: `lazy_pipeline` — run the same pipeline through `Enum` and
`Stream` and measure the memory cost in bytes.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Project context

`Enum` is *strict*: every function walks the full collection and returns a
new collection. `Stream` is *lazy*: each function returns a description of
work to do, and nothing actually runs until a terminal `Enum` function (or
`Stream.run/1`) consumes it. The difference is invisible on tiny inputs and
decisive on large ones.

In this exercise you'll write the exact same pipeline twice — once with
`Enum`, once with `Stream` — and use `:erlang.memory/0` to see how the
strict version allocates each intermediate list while the lazy version
composes a single walk.

Project structure:

```
lazy_pipeline/
├── lib/
│   └── lazy_pipeline.ex
├── test/
│   └── lazy_pipeline_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `Enum` is eager, `Stream` is lazy

```elixir
1..1_000_000
|> Enum.map(&(&1 * 2))       # allocates a 1M-element list
|> Enum.filter(&(&1 > 100))  # walks it again, allocates another list
|> Enum.take(5)              # walks a third time, returns 5 items
```

Versus:

```elixir
1..1_000_000
|> Stream.map(&(&1 * 2))
|> Stream.filter(&(&1 > 100))
|> Enum.take(5)              # walks JUST enough to produce 5 items
```

The stream pipeline is a *recipe*. It composes into a single pass where
each element flows through map → filter → take, and the pass stops as soon
as `take` has what it needs. No intermediate lists are ever allocated.

### 2. Streams need a terminal operation

`Stream.map/2`, `Stream.filter/2`, `Stream.take/2` all return a `%Stream{}`
— still lazy. You must end with `Enum.to_list/1`, `Enum.reduce/3`,
`Enum.take/2`, `Stream.run/1`, etc., to actually execute work. Forgetting
the terminal step is a common beginner mistake — your "pipeline" simply
doesn't run.

### 3. Early termination is the killer feature

`Enum.take(stream, 5)` stops pulling after 5 elements. Over a 1M-element
source you walk 5 + small constant, not 1M. For `Enum.take(list, 5)` on
`list = Enum.map(1..1_000_000, ...)`, you already paid for the full map.

### 4. Measuring memory with `:erlang.memory/0`

```elixir
:erlang.garbage_collect()
before = :erlang.memory(:total)
result = do_work()
after_mem = :erlang.memory(:total)
after_mem - before
```

Numbers fluctuate — garbage collection and scheduler state both move them.
Run several times and look at the order of magnitude, not the last digit.

---

## Implementation

### Step 1: Create the project

```bash
mix new lazy_pipeline
cd lazy_pipeline
```

### Step 2: `lib/lazy_pipeline.ex`

```elixir
defmodule LazyPipeline do
  @moduledoc """
  Side-by-side strict (`Enum`) vs lazy (`Stream`) pipelines over a range,
  plus a tiny memory-measurement helper so you can see the difference
  instead of taking it on faith.
  """

  @doc """
  Strict pipeline: each intermediate collection is fully materialized.

  For `n = 1_000_000` this allocates one list per `Enum.*` stage.
  """
  @spec strict_pipeline(pos_integer()) :: [integer()]
  def strict_pipeline(n) do
    1..n
    |> Enum.map(&(&1 * &1))
    |> Enum.filter(&(rem(&1, 2) == 0))
    |> Enum.take(10)
  end

  @doc """
  Lazy pipeline: identical semantics, single-pass execution, early exit.

  The `Stream.*` calls just build a computation graph. Only `Enum.take/2`
  (the terminal operation) pulls elements — and it stops at 10.
  """
  @spec lazy_pipeline(pos_integer()) :: [integer()]
  def lazy_pipeline(n) do
    1..n
    |> Stream.map(&(&1 * &1))
    |> Stream.filter(&(rem(&1, 2) == 0))
    |> Enum.take(10)
  end

  @doc """
  Runs `fun` and returns `{result, bytes_delta}` based on `:erlang.memory(:total)`.

  We force a GC before and after so transient allocations from earlier work
  don't contaminate the measurement. The number is approximate but useful
  for order-of-magnitude comparisons between strict and lazy pipelines.
  """
  @spec measure((-> result)) :: {result, integer()} when result: var
  def measure(fun) when is_function(fun, 0) do
    :erlang.garbage_collect()
    before = :erlang.memory(:total)
    result = fun.()
    :erlang.garbage_collect()
    after_mem = :erlang.memory(:total)
    {result, after_mem - before}
  end
end
```

### Step 3: `test/lazy_pipeline_test.exs`

```elixir
defmodule LazyPipelineTest do
  use ExUnit.Case, async: true

  describe "equivalence" do
    test "strict and lazy pipelines produce the same result" do
      assert LazyPipeline.strict_pipeline(1_000) ==
               LazyPipeline.lazy_pipeline(1_000)
    end
  end

  describe "lazy_pipeline/1" do
    test "returns the first 10 even squares starting from 2*2" do
      # Squares: 1, 4, 9, 16, 25, 36, ... — even ones are 4, 16, 36, 64, ...
      assert LazyPipeline.lazy_pipeline(100) ==
               [4, 16, 36, 64, 100, 144, 196, 256, 324, 400]
    end

    test "handles a range too large to materialize eagerly in reasonable time" do
      # 1..100_000_000 is 100M elements — Enum.map would allocate ~800MB+ of list.
      # Stream only walks until it has 10 even squares.
      result = LazyPipeline.lazy_pipeline(100_000_000)
      assert length(result) == 10
    end
  end

  describe "memory measurement" do
    @tag :memory
    test "lazy pipeline allocates dramatically less than strict on large n" do
      n = 1_000_000

      {_, strict_bytes} = LazyPipeline.measure(fn -> LazyPipeline.strict_pipeline(n) end)
      {_, lazy_bytes} = LazyPipeline.measure(fn -> LazyPipeline.lazy_pipeline(n) end)

      # The strict version materializes a ~1M-element list of squares plus a
      # filtered list; the lazy version walks just enough to find 10 evens.
      # We assert an order-of-magnitude difference, not exact bytes, because
      # GC and scheduler state affect the reading.
      assert strict_bytes > lazy_bytes * 10,
             "expected strict to allocate >>> lazy, got strict=#{strict_bytes} lazy=#{lazy_bytes}"
    end
  end
end
```

### Step 4: Run

```bash
mix test
# Run only the memory benchmark:
mix test --only memory
```

---

## Trade-offs and production gotchas

**1. Streams have per-element overhead**
Each `Stream.map` adds a small closure call per element. For *small* inputs
or when you need the full result anyway, `Enum` is often faster — fewer
allocations of closures, more chances for the VM to optimize a tight loop.
The lazy win only shows up when the input is big **and** you don't consume
all of it, or when intermediate lists would dominate memory.

**2. Streams are not parallel**
`Stream.map/2` runs on the caller process, sequentially. If you want
parallelism, use `Task.async_stream/3` or `Flow` (see exercises 107 and 109).

**3. Side effects in streams are deferred**
`Stream.map(stream, fn x -> IO.puts(x); x end)` prints nothing until a
terminal operation runs. If the terminal never runs, the side effects
never happen. This is a feature, but surprises newcomers.

**4. `Stream.run/1` is for side-effect-only pipelines**
If you only care about the IO, not a return value, `Stream.run/1` drains
the stream returning `:ok`. It's the "I don't want a list" terminal.

**5. `Enum.to_list(stream)` undoes laziness**
Calling `Enum.to_list/1` on a stream materializes the whole thing. If the
source is infinite (`Stream.iterate`, `Stream.cycle`) that call never
returns. Always pair infinite streams with a `Stream.take/2` *before* any
terminal operation.

**6. `:erlang.memory/0` is a coarse instrument**
For rigorous benchmarks use `Benchee` with the `:memory_time` option, which
runs each scenario in isolation and reports allocations deterministically.
Our helper is fine for "bigger vs smaller" — not for comparing two streams
that differ by 20 percent.

**7. When NOT to use `Stream`**
- Input fits comfortably in memory and you need the entire transformed
  list — `Enum` is simpler and often faster.
- You already have a concrete list on hand and plan to consume all of it.
- You're composing two steps — the readability cost of `Stream.` prefixes
  usually isn't worth it below three stages.

---

## Resources

- [`Stream` — Elixir stdlib](https://hexdocs.pm/elixir/Stream.html)
- [`Enum` — Elixir stdlib](https://hexdocs.pm/elixir/Enum.html)
- ["Enumerables and Streams" — Elixir getting started](https://hexdocs.pm/elixir/enumerables-and-streams.html)
- [José Valim — "Comprehensions and Streams"](https://elixir-lang.org/blog/2013/08/21/elixir-streams/) — the original blog post introducing Streams
- Saša Jurić — *Elixir in Action*, 2nd/3rd ed., chapter on the `Enumerable`
  protocol and lazy evaluation
- [`Benchee`](https://hexdocs.pm/benchee/) — for serious memory/time benchmarks
