# Function Capture with the Ampersand Operator

**Project**: `pipeline_composer` — a tiny stage composer that treats functions as first-class references

**Difficulty**: ★☆☆☆☆
**Estimated time**: 1–2 hours

---

## Project structure

```
pipeline_composer/
├── lib/
│   └── pipeline_composer/
│       ├── stages.ex        # named stage functions
│       └── pipeline.ex      # runs a list of captured stages
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

## Implementation

### Step 1 — Create the project

```bash
mix new pipeline_composer
cd pipeline_composer
```

### Step 2 — `lib/pipeline_composer/stages.ex`

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

```elixir
defmodule PipelineComposerTest do
  use ExUnit.Case, async: true

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

```bash
mix test
```

All 4 tests should pass.

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

## Resources

- [Kernel.SpecialForms — `&/1`](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#&/1)
- [Elixir Getting Started — Anonymous functions](https://hexdocs.pm/elixir/anonymous-functions.html)
- [José Valim on capture vs fn](https://elixirforum.com/t/when-to-use-capture-vs-fn/3863) — idiomatic guidance from the language author
