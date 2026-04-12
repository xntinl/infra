# Debugging with `IO.inspect`, `dbg/2`, and `IEx.pry`

**Project**: `debug_tools` — a tiny pipeline where you practice the three
everyday Elixir debugging tools without reaching for a debugger.

**Difficulty**: ★★☆☆☆
**Estimated time**: 1–2 hours

---

## Project context

Before you install `:observer`, before you hook up tracing, before you even
think about a real debugger — Elixir gives you three tools that solve 90% of
the "what is this value right now?" problems:

- `IO.inspect/2` — inline printing that is pipeline-friendly because it
  returns its argument unchanged.
- `dbg/2` (Elixir 1.14+) — a macro that prints the expression and its value,
  and in IEx opens an interactive debugger that lets you step through pipes.
- `IEx.pry/0` — freezes execution at a point in your code and drops you into
  an IEx prompt bound to the local variables in scope.

This exercise builds a trivial `TextPipeline` module and practices each tool
on it. You'll understand *when* to use which, and the traps that bite people
who reach for `IO.puts` and `inspect/1` instead.

Project structure:

```
debug_tools/
├── lib/
│   └── text_pipeline.ex
├── test/
│   └── text_pipeline_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `IO.inspect/2` returns its argument — that's the whole point

```elixir
"hello"
|> String.upcase()
|> IO.inspect(label: "after upcase")
|> String.reverse()
```

Because `IO.inspect/2` returns the value it prints, you can drop it anywhere
in a pipeline without breaking the data flow. Use `label:` to name the
probe, `limit: :infinity` to defeat the default truncation of long lists,
and `pretty: true` (the default in most terminals) for readable maps/structs.

### 2. `dbg/2` — inspect the *expression*, not just the value

```elixir
x = 2
dbg(x + 3)   # => x + 3 #=> 5
```

`dbg/2` is a macro, so it sees the AST. It prints the source expression
alongside the result, which is wildly more useful than `IO.inspect(x + 3)`
(which only prints `5`). In **IEx with `--dbg pry`** (Elixir 1.14+), calling
a function that contains `dbg()` pauses execution and lets you step through
each stage of the pipe, inspecting intermediate values.

### 3. `IEx.pry/0` — a breakpoint you write in source

```elixir
require IEx
IEx.pry()
```

When execution hits `IEx.pry()`, if you are running inside `iex -S mix`, the
VM pauses at that line and gives you an IEx prompt *with the local variables
in scope*. Type `continue` to resume, `respawn` to restart the shell, or
just inspect whatever you want.

### 4. None of this is a replacement for tests

Debug prints get committed by accident and pollute production logs. Every
`IO.inspect`, `dbg`, and `pry` should be temporary. Credo and even the
compiler (in strict mode) will warn about stray `dbg` calls — lean into that.

---

## Implementation

### Step 1: Create the project

```bash
mix new debug_tools
cd debug_tools
```

### Step 2: `lib/text_pipeline.ex`

```elixir
defmodule TextPipeline do
  @moduledoc """
  A trivial pipeline used as a target for debugging practice.

  `process/1` takes a string, normalizes it, and returns a map of word
  frequencies. The individual steps are exposed so you can probe each one
  with `IO.inspect`, `dbg`, or `IEx.pry`.
  """

  @doc """
  Normalizes, tokenizes, and counts word frequencies.

  ## Examples

      iex> TextPipeline.process("Hello hello world")
      %{"hello" => 2, "world" => 1}
  """
  @spec process(String.t()) :: %{String.t() => non_neg_integer()}
  def process(text) when is_binary(text) do
    text
    |> normalize()
    |> tokenize()
    |> count()
  end

  @doc "Lowercases and trims extra whitespace."
  @spec normalize(String.t()) :: String.t()
  def normalize(text) do
    text
    |> String.downcase()
    |> String.trim()
  end

  @doc "Splits on whitespace, drops empties."
  @spec tokenize(String.t()) :: [String.t()]
  def tokenize(text) do
    String.split(text, ~r/\s+/, trim: true)
  end

  @doc "Counts occurrences of each word."
  @spec count([String.t()]) :: %{String.t() => non_neg_integer()}
  def count(words), do: Enum.frequencies(words)

  # ── Debugging showcase ──────────────────────────────────────────────────

  @doc """
  Same as `process/1`, but instrumented with `IO.inspect` probes at every
  stage. Use this to SEE the pipeline values without changing control flow.
  Each probe returns its argument unchanged, so the final result is identical.
  """
  @spec process_with_inspect(String.t()) :: %{String.t() => non_neg_integer()}
  def process_with_inspect(text) do
    text
    |> IO.inspect(label: "input")
    |> normalize()
    |> IO.inspect(label: "normalized")
    |> tokenize()
    |> IO.inspect(label: "tokens", limit: :infinity)
    |> count()
    |> IO.inspect(label: "counts", pretty: true)
  end

  @doc """
  Same pipeline wrapped in `dbg/2`. In `iex --dbg pry -S mix`, calling this
  function PAUSES at each pipe stage and lets you step through interactively.
  Outside IEx it just prints the expressions and their values.
  """
  @spec process_with_dbg(String.t()) :: %{String.t() => non_neg_integer()}
  def process_with_dbg(text) do
    text
    |> normalize()
    |> tokenize()
    |> count()
    |> dbg()
  end

  @doc """
  Demonstrates `IEx.pry/0`. Run `iex -S mix` and call
  `TextPipeline.process_with_pry("hello world")` — execution pauses at the
  `IEx.pry()` line and you can inspect `normalized`, `tokens`, and `counts`
  by name from the IEx prompt.
  """
  @spec process_with_pry(String.t()) :: %{String.t() => non_neg_integer()}
  def process_with_pry(text) do
    require IEx

    normalized = normalize(text)
    tokens = tokenize(normalized)
    counts = count(tokens)

    IEx.pry()

    counts
  end
end
```

### Step 3: `test/text_pipeline_test.exs`

```elixir
defmodule TextPipelineTest do
  use ExUnit.Case, async: true

  import ExUnit.CaptureIO

  doctest TextPipeline

  describe "process/1" do
    test "counts words case-insensitively" do
      assert TextPipeline.process("Hello HELLO world") == %{"hello" => 2, "world" => 1}
    end

    test "handles extra whitespace" do
      assert TextPipeline.process("  a   b  a  ") == %{"a" => 2, "b" => 1}
    end

    test "empty string yields an empty map" do
      assert TextPipeline.process("") == %{}
    end
  end

  describe "process_with_inspect/1" do
    test "returns the same result as process/1 and prints the probes" do
      io =
        capture_io(fn ->
          result = TextPipeline.process_with_inspect("Hi hi")
          assert result == %{"hi" => 2}
        end)

      # The labels we added with IO.inspect appear in stdout.
      assert io =~ "input:"
      assert io =~ "normalized:"
      assert io =~ "tokens:"
      assert io =~ "counts:"
    end
  end
end
```

### Step 4: Run and explore

```bash
mix test

# Inspect everything inline:
iex -S mix
iex> TextPipeline.process_with_inspect("Hello world hello")

# Step-through debugging (Elixir 1.14+):
iex --dbg pry -S mix
iex> TextPipeline.process_with_dbg("Hello world hello")
# press Enter / n at each prompt to step through the pipe

# Breakpoint-style:
iex -S mix
iex> TextPipeline.process_with_pry("a b a")
# you drop into a nested IEx prompt; type `normalized`, `tokens`, `counts`
# then type `continue` to resume.
```

---

## Trade-offs and production gotchas

**1. `IO.inspect` in hot paths is slow and noisy**
Formatting and printing takes microseconds and synchronizes on `:stdio`. In
a tight loop, printing once per iteration can dominate runtime. It also
scrambles log output in concurrent tests. Keep probes scoped and remove
them before committing.

**2. `inspect/1` vs `IO.inspect/2`** — they are NOT the same
`inspect/1` returns a string. `IO.inspect/2` prints *and* returns the value.
If you write `x |> inspect() |> foo()`, you just passed a string to `foo/1`
— that's almost never what you want.

**3. `dbg/2` leaves pretty output in production logs**
`dbg` prints colored, formatted output to stderr. If a stray `dbg(x)` ships,
your logs are noisy AND colored (with ANSI escapes) in environments that
can't render them. Configure Credo's `Credo.Check.Warning.Dbg` to fail CI.

**4. `IEx.pry` only works when the code is reached from an IEx session**
If your code runs inside a Task spawned by Phoenix during a request, the
`pry` prompt opens in the IEx shell *on the BEAM node* — you need to have
`iex -S mix phx.server` (not `mix phx.server`) for it to be usable.

**5. `IO.inspect` truncates — `:infinity` exists for a reason**
Default `:limit` is 50 items, default `:printable_limit` is 4096 bytes.
Missing this leads to "why is the list cut off?" confusion. When in doubt:
`IO.inspect(x, limit: :infinity, printable_limit: :infinity)`.

**6. When NOT to use these tools**
If you need to inspect *why* a production node is misbehaving right now,
you want `:recon`, `:observer`, or `:sys.get_state/1` — not `IO.inspect`.
If you're debugging concurrency / scheduling, reach for tracing (`:dbg`,
`:recon_trace`), not print statements.

---

## Resources

- [`IO.inspect/2` — Elixir stdlib](https://hexdocs.pm/elixir/IO.html#inspect/2)
- [`Kernel.dbg/2`](https://hexdocs.pm/elixir/Kernel.html#dbg/2) — the macro, including the `--dbg pry` flag
- [`IEx.pry/0`](https://hexdocs.pm/iex/IEx.html#pry/0) — source-level breakpoints
- [`Inspect.Opts`](https://hexdocs.pm/elixir/Inspect.Opts.html) — every flag you can pass (`limit`, `printable_limit`, `pretty`, `structs`, `syntax_colors`, etc.)
- ["Debugging" — the Elixir guide](https://hexdocs.pm/elixir/debugging.html) — official walkthrough of all three tools
