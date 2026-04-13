# Function Capture with the Ampersand Operator

**Project**: `pipeline_composer` — a tiny stage composer that treats functions as first-class references

---

## The business problem

A pipeline whose stages are hardcoded with `|>` is rigid:

---

## Project structure

```
pipeline_composer/
├── lib/
│   └── pipeline_composer/
│       ├── stages.ex        # named stage functions
│       └── pipeline.ex      # runs a list of captured stages
├── script/
│   └── main.exs
├── test/
│   └── pipeline_composer_test.exs
└── mix.exs
```

---

## What you will learn

Two concepts, nothing more:

1. **Capture operator `&`** — turn a named function into a first-class value (`&Module.fun/arity`).
2. **Shorthand lambdas** — `&(&1 + 1)` is sugar for `fn x -> x + 1 end`.

We use them to build a pipeline where each stage is **passed around as a reference** rather
than hardcoded inside a pipe chain. Stages become data, and data can be composed.

---

## The concept in 60 seconds

In Elixir, functions are values. But writing `fn x -> Stages.normalize(x) end` just to pass
`normalize/1` around is noisy. The capture operator shortens it:

```elixir
# These are equivalent:
fn x -> Stages.normalize(x) end
&Stages.normalize/1

# These are also equivalent:
fn x -> x * 2 end
&(&1 * 2)
```

**Rule of thumb**: use `&Mod.fun/arity` when you already have a named function.
Use `&(&1 + 1)` only for trivial one-liners — anything with two positional args
(`&1`, `&2`) fast becomes unreadable.

---

## Why this matters

A pipeline whose stages are hardcoded with `|>` is rigid:

```elixir
input |> normalize() |> validate() |> enrich()
```

A pipeline that receives stages as a list of captured functions is configurable at runtime —
you can reorder, skip, or inject stages without editing the call site.

---

## Why capture and not full `fn`

- `fn x -> Mod.fun(x) end` works but adds visual noise — each named-function reference becomes three tokens instead of one.
- Capture preserves arity in the syntax (`&Mod.fun/1`), so mismatches fail at compile time instead of silently closing over the wrong arity.
- Anonymous `fn` is still the right tool for multi-line bodies or pattern matching on arguments — capture is the **named-function shortcut**, not a replacement.

---

## Design decisions

**Option A — hardcoded pipe chain**
- Pros: zero abstraction, trivially readable for a fixed pipeline.
- Cons: reorder, skip, or inject a stage means editing the call site.

**Option B — list of captured functions fed into `Enum.reduce`** (chosen)
- Pros: pipeline shape is data; stages can be reordered or filtered at runtime; easy to test one stage in isolation.
- Cons: one extra indirection; a typo in the stage list only fails when the pipeline runs.

→ Chose **B** because the whole point of the exercise is treating functions as first-class values. A hardcoded `|>` chain would defeat the lesson.

---

## Implementation

### `mix.exs`
```elixir
defmodule PipelineComposer.MixProject do
  use Mix.Project

  def project do
    [
      app: :pipeline_composer,
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

### Step 1 — Create the project

**Objective**: Split stages from the pipeline runner so captured function references, not lexical imports, define the order of execution.

```bash
mix new pipeline_composer
cd pipeline_composer
```

### Step 2 — `lib/pipeline_composer/stages.ex`

**Objective**: Keep stages as named unary functions so `&Module.fun/1` captures yield stable, serializable references — unlike anonymous closures.

```elixir
defmodule PipelineComposer.Stages do
  @moduledoc """
  Named stages for the pipeline. Each is a plain `fun/1` so it can be captured
  as `&Stages.name/1` and passed as data.
  """

  @spec normalize(String.t()) :: String.t()
  def normalize(text), do: text |> String.trim() |> String.downcase()

  @spec validate(String.t()) :: String.t()
  def validate(""), do: raise(ArgumentError, "empty input after normalization")
  def validate(text), do: text

  @spec enrich(String.t()) :: map()
  # Why a map here: downstream consumers want structured output, not raw strings.
  def enrich(text), do: %{text: text, length: String.length(text)}
end
```

### Step 3 — `lib/pipeline_composer/pipeline.ex`

**Objective**: Run `Enum.reduce/3` over a list of captures so the pipeline is data: stages can be reordered, mocked, or persisted without recompiling.

```elixir
defmodule PipelineComposer.Pipeline do
  @moduledoc """
  Runs an ordered list of unary functions against an input.

  Stages are captured function references — either named (`&Mod.fun/1`) or
  shorthand lambdas (`&(&1 + 1)`). The pipeline does not care which form;
  it only requires `is_function(stage, 1)`.
  """

  @spec run(term(), [(term() -> term())]) :: term()
  def run(input, stages) when is_list(stages) do
    # Enum.reduce feeds the accumulator (current value) to each captured stage.
    # Capture + reduce = a pipeline whose shape is decided at runtime.
    Enum.reduce(stages, input, fn stage, acc -> stage.(acc) end)
  end
end
```

### Step 4 — `test/pipeline_composer_test.exs`

**Objective**: Test the pipeline with captured anonymous and named functions mixed, proving both capture forms satisfy the same call contract.

```elixir
defmodule PipelineComposerTest do
  use ExUnit.Case, async: true
  doctest PipelineComposer.Pipeline

  alias PipelineComposer.{Pipeline, Stages}

  describe "capture of named functions" do
    test "runs normalize -> validate -> enrich in order" do
      stages = [&Stages.normalize/1, &Stages.validate/1, &Stages.enrich/1]
      assert Pipeline.run("  Hello  ", stages) == %{text: "hello", length: 5}
    end

    test "stage order is configurable — skip enrichment" do
      stages = [&Stages.normalize/1, &Stages.validate/1]
      assert Pipeline.run("  World  ", stages) == "world"
    end
  end

  describe "shorthand lambdas" do
    test "mixes captured named functions with &(&1) shorthand" do
      stages = [&Stages.normalize/1, &(&1 <> "!"), &String.length/1]
      assert Pipeline.run("  Hi  ", stages) == 3
    end
  end

  describe "validation" do
    test "validate/1 raises on empty input after normalization" do
      stages = [&Stages.normalize/1, &Stages.validate/1]
      assert_raise ArgumentError, fn -> Pipeline.run("    ", stages) end
    end
  end
end
```

### Step 5 — Run the tests

**Objective**: Run the suite to confirm captured stages keep their arity invariant; an arity mismatch is the one error capture cannot catch at compile.

```bash
mix test
```

All 4 tests should pass.

### Why this works

`Enum.reduce/3` is the minimal primitive that threads an accumulator through a list — which is exactly the shape of a pipeline. Because every stage is a `fun/1`, the reducer doesn't need to know anything about the domain; it just calls `stage.(acc)`. Capture syntax keeps the stage list dense and arity-checked, and `is_list(stages)` guarantees the caller passed a composable collection rather than a single function.

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== PipelineComposer: demo ===\n")

    result_1 = Pipeline.run("  Hello  ", stages)
    IO.puts("Demo 1: #{inspect(result_1)}")

    result_2 = Pipeline.run("  World  ", stages)
    IO.puts("Demo 2: #{inspect(result_2)}")

    result_3 = Pipeline.run("  Hi  ", stages)
    IO.puts("Demo 3: #{inspect(result_3)}")
    IO.puts("\n=== Done ===")
  end
end

Main.main()
```

Run with: `elixir script/main.exs`

---

## Benchmark

```elixir
# bench/capture_vs_fn.exs
stages_capture = [&String.trim/1, &String.downcase/1, &(&1 <> "!")]
stages_fn = [fn s -> String.trim(s) end, fn s -> String.downcase(s) end, fn s -> s <> "!" end]

{t_cap, _} = :timer.tc(fn ->
  Enum.each(1..100_000, fn _ -> PipelineComposer.Pipeline.run("  Hi  ", stages_capture) end)
end)

{t_fn, _} = :timer.tc(fn ->
  Enum.each(1..100_000, fn _ -> PipelineComposer.Pipeline.run("  Hi  ", stages_fn) end)
end)

IO.puts("capture: #{t_cap} µs   fn: #{t_fn} µs")
```

Target: both forms within ~5% of each other (< 1 µs per run on modern hardware). Capture is pure syntax sugar — there should be no runtime difference.

---

## Trade-offs

| Aspect | `&Mod.fun/arity` | `&(&1 + 1)` shorthand | Full `fn ... end` |
|---|---|---|---|
| Readability (named logic) | best | poor | verbose |
| Readability (trivial ops) | overkill | best | acceptable |
| Multi-line body | impossible | impossible | required |
| Pattern matching in args | no | no | yes |
| Closure over variables | only at capture time | only at capture time | yes |

**When NOT to use the capture operator:**

- The body has more than one expression — use `fn -> ... end`.
- You need pattern matching on arguments — capture does not allow it.
- The "function" is actually a macro (`if`, `&&`, `unless`). Macros cannot be captured.

---

## Common production mistakes

**1. Capturing with the wrong arity**
`&String.replace/2` and `&String.replace/3` are different values. Capturing the wrong
one raises `UndefinedFunctionError` at call time, not at capture time. Read the docs.

**2. `&(&1)` identity abuse**
Some devs write `Enum.map(list, &(&1))` as a no-op. That is not a no-op — it allocates
a closure and calls it per element. Just use the list as-is.

**3. Shorthand with repeated args**
`&(&1 + &1)` works but is cryptic. If you need the same input twice, write `fn x -> x + x end`.

**4. Capturing private functions from outside the module**
`&MyMod.private_fun/1` fails — capture sees only exported functions. Move it to a public
helper or capture inside the module.

**5. Capturing in a hot loop**
Every `&Mod.fun/1` inside a hot `Enum.map` allocates a function value. If the loop runs
millions of iterations, capture once outside the loop and reuse the reference.

---

## Reflection

- If your pipeline needs to branch (stage C runs only when stage B returns `{:ok, _}`), is a flat list of captures still the right shape, or does it become a glorified `case`? How would you redesign it?
- You want to trace every stage (log input/output) without modifying `Stages`. How do you wrap each captured function in the list? What does the capture syntax force you to give up?

---

## Resources

- [Kernel.SpecialForms — `&/1`](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#&/1)
- [Elixir Getting Started — Anonymous functions](https://hexdocs.pm/elixir/anonymous-functions.html)
- [José Valim on capture vs fn](https://elixirforum.com/t/when-to-use-capture-vs-fn/3863) — idiomatic guidance from the language author

### `lib/pipeline_composer.ex`

```elixir
defmodule PipelineComposer do
  @moduledoc """
  Reference implementation for Function Capture with the Ampersand Operator.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the pipeline_composer module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> PipelineComposer.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```

### `test/pipeline_composer_test.exs`

```elixir
defmodule PipelineComposerTest do
  use ExUnit.Case, async: true

  doctest PipelineComposer

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert PipelineComposer.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts
### 1. The Ampersand `&` Captures Functions as Values

```elixir
add = &+/2
add.(1, 2)  # 3
```

The ampersand syntax `&function/arity` creates a closure capturing that function. This is powerful for passing functions to higher-order functions like `Enum.map(&String.upcase/1)`.

### 2. Shorthand with `&` and Placeholders

```elixir
Enum.map([1, 2, 3], &(&1 * 2))
```

The `&(...)` syntax with `&1`, `&2` placeholders is shorthand for an anonymous function. `&(&1 * 2)` is equivalent to `fn x -> x * 2 end`.

### 3. Module Functions vs Anonymous Functions

Captured module functions are slightly more efficient than anonymous functions because they don't close over variables. For performance-sensitive code, prefer capturing: `Enum.map(list, &String.upcase/1)` over `Enum.map(list, fn x -> String.upcase(x) end)`.

---
