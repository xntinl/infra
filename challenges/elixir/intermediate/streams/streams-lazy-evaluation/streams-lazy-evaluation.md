# Streams and lazy evaluation — measuring the memory difference

**Project**: `lazy_pipeline` — run the same pipeline through `Enum` and
`Stream` and measure the memory cost in bytes.

---

## Why streams lazy evaluation matters

`Enum` is *strict*: every function walks the full collection and returns a
new collection. `Stream` is *lazy*: each function returns a description of
work to do, and nothing actually runs until a terminal `Enum` function (or
`Stream.run/1`) consumes it. The difference is invisible on tiny inputs and
decisive on large ones.

In this exercise you'll write the exact same pipeline twice — once with
`Enum`, once with `Stream` — and use `:erlang.memory/0` to see how the
strict version allocates each intermediate list while the lazy version
composes a single walk.

---

## Project structure

```
lazy_pipeline/
├── lib/
│   └── lazy_pipeline.ex
├── script/
│   └── main.exs
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

## Design decisions

**Option A — Use `Enum` throughout for simplicity**
- Pros: one module to learn; predictable strict semantics; often faster for small inputs.
- Cons: allocates every intermediate list; cannot early-terminate over huge sources; dominates RAM on multi-stage pipelines.

**Option B — Use `Stream` for the body, terminate with `Enum`** (chosen)
- Pros: single-pass composition, early exit on `Enum.take`, O(1) extra memory regardless of input size.
- Cons: closure-per-element overhead; side effects are deferred; forgetting the terminal call is a common bug.

→ Chose **B** because the whole point of the exercise is to *measure* the difference, and the measurement only pays off when input size and partial consumption meet the Stream sweet spot.

---

## Implementation

### `mix.exs`

```elixir
defmodule LazyPipeline.MixProject do
  use Mix.Project

  def project do
    [
      app: :lazy_pipeline,
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
mix new lazy_pipeline
cd lazy_pipeline
```

### `lib/lazy_pipeline.ex`

**Objective**: Implement `lazy_pipeline.ex` — the lazy operator whose resource and memory profile only becomes visible when the stream is actually run.

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

**Objective**: Write `lazy_pipeline_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.

```elixir
defmodule LazyPipelineTest do
  use ExUnit.Case, async: true

  doctest LazyPipeline

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

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.

```bash
mix test
# Run only the memory benchmark:
mix test --only memory
```

### Why this works

Each `Stream.*` call returns a `%Stream{}` struct holding the enumerable source and a composed reducer function. No list is ever built between stages — a terminal `Enum.*` pulls elements one at a time through the whole pipeline, and `Enum.take/2` halts the pull as soon as it has enough. The memory delta measured with `:erlang.memory(:total)` reflects only the final result size plus a constant per-element closure cost.

---

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== LazyPipeline Demo ===\n")

    result_1 = LazyPipeline.strict_pipeline(nil)
    IO.puts("Demo 1 - strict_pipeline: #{inspect(result_1)}")
    result_2 = LazyPipeline.lazy_pipeline(nil)
    IO.puts("Demo 2 - lazy_pipeline: #{inspect(result_2)}")
    result_3 = LazyPipeline.measure(nil)
    IO.puts("Demo 3 - measure: #{inspect(result_3)}")
  end
end

Main.main()
```

## Benchmark

```elixir
n = 1_000_000
{_, strict_us} = :timer.tc(fn -> LazyPipeline.strict_pipeline(n) end)
{_, lazy_us}   = :timer.tc(fn -> LazyPipeline.lazy_pipeline(n) end)
IO.puts("strict=#{strict_us}µs  lazy=#{lazy_us}µs")
```

Target esperado: on modern hardware with n=1_000_000, `strict` runs ~50-150 ms and allocates >50 MB; `lazy` runs <1 ms and allocates a few KB — an order-of-magnitude win on both axes.

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
parallelism, use `Task.async_stream/3` or `Flow` (Investigates 107 and 109).

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

## Reflection

- Your service computes a 3-stage transform over a 500k-row daily export and you always consume the full result. Would you still prefer `Stream` over `Enum`? Justify in terms of closure overhead vs allocation savings.
- How would you redesign `measure/1` to be safe under concurrent load (multiple processes calling it)? `:erlang.memory(:total)` is VM-global — what does that imply?

## Resources

- [`Stream` — Elixir stdlib](https://hexdocs.pm/elixir/Stream.html)
- [`Enum` — Elixir stdlib](https://hexdocs.pm/elixir/Enum.html)
- ["Enumerables and Streams" — Elixir getting started](https://hexdocs.pm/elixir/enumerables-and-streams.html)
- [José Valim — "Comprehensions and Streams"](https://elixir-lang.org/blog/2013/08/21/elixir-streams/) — the original blog post introducing Streams
- Saša Jurić — *Elixir in Action*, 2nd/3rd ed., chapter on the `Enumerable`
  protocol and lazy evaluation
- [`Benchee`](https://hexdocs.pm/benchee/) — for serious memory/time benchmarks

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

### `test/lazy_pipeline_test.exs`

```elixir
defmodule LazyPipelineTest do
  use ExUnit.Case, async: true

  doctest LazyPipeline

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert LazyPipeline.run(:noop) == :ok
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
