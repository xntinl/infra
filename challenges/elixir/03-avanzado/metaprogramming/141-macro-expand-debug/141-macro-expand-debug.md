# Debugging Macros with `Macro.expand` and `Macro.to_string`

**Project**: `macro_expand_debug` — build a tiny interactive diagnostic tool that takes a quoted expression and shows the step-by-step expansion, the final AST, and the final source, mirroring what you do by hand when a macro misbehaves.

**Difficulty**: ★★★☆☆
**Estimated time**: 3–4 hours

---

## Project context

You're pairing with a junior on a macro-heavy module. Something outputs a wrong
result. They IO.inspect everywhere, but the macro expands at compile time, so their
`IO.inspect` statements inside the `quote` body only print AST forms — unreadable.

The senior move: step macro expansion manually using `Macro.expand_once/2` and
`Macro.to_string/1`, build a small helper that drives this interactively. Phoenix
internally calls `Macro.to_string/1` when generating controller actions at
compile time — knowing these APIs cold is table stakes for DSL authors.

```
macro_expand_debug/
├── lib/
│   └── macro_expand_debug/
│       ├── inspector.ex         # expand_step/1, inspect_ast/1
│       └── sample_macros.ex     # a small set of macros to debug
├── test/
│   └── inspector_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `Macro.expand_once/2` vs `Macro.expand/2`

- `expand_once/2`: expands the outermost macro one step; returns the result
- `expand/2`: repeatedly calls `expand_once/2` until the AST no longer changes

For debugging, `expand_once/2` is essential — it lets you watch the expansion stairs
instead of jumping to the fully expanded form.

### 2. `Macro.Env`

An environment records the current module, file, function, imports, aliases, and more.
Macros that depend on imports (`is_nil/1`, `unless/2`) only expand correctly in an env
that contains those imports. `__ENV__` gives you the env of the current call site —
for manual expansion use `Macro.Env.prune_compile_info/1` or build an env via
`Code.env_for_eval/1`.

### 3. `Macro.to_string/1`

Pretty-prints a quoted expression back into source. After expansion, read
`Macro.to_string(expanded)` as the "generated code". For complex macros this is the
fastest path to understanding what you actually built.

### 4. `dbg/1` and its macro

Elixir 1.14+ has `dbg/1`, a macro that prints the expression, its value, and pretty
output. You can customize via the `:dbg` compiler option. Under the hood it is a
macro that rewrites its argument with `Macro.prewalk/2`.

### 5. Limits of expansion

Some constructs look like macros but are quoted forms — `quote`, `unquote`,
`__MODULE__`, `__ENV__`. They do not expand further. A debugger should detect
these leaves and stop.

---

## Implementation

### Step 1: `lib/macro_expand_debug/inspector.ex`

```elixir
defmodule MacroExpandDebug.Inspector do
  @moduledoc """
  Tools for inspecting macro expansion step by step.

  Usage:

      iex> env = __ENV__
      iex> ast = quote do: unless(true, do: :ok)
      iex> MacroExpandDebug.Inspector.trace_expansion(ast, env)
  """

  @type step :: %{ast: Macro.t(), source: String.t(), step_number: non_neg_integer()}

  @spec expand_step(Macro.t(), Macro.Env.t()) :: {Macro.t(), boolean()}
  def expand_step(ast, env) do
    expanded = Macro.expand_once(ast, env)
    {expanded, expanded != ast}
  end

  @spec trace_expansion(Macro.t(), Macro.Env.t(), pos_integer()) :: [step()]
  def trace_expansion(ast, env, max_steps \\ 10) do
    trace_loop(ast, env, max_steps, 0, [])
  end

  defp trace_loop(ast, env, max, step, acc) when step >= max do
    Enum.reverse([to_step(ast, step) | acc])
  end

  defp trace_loop(ast, env, max, step, acc) do
    entry = to_step(ast, step)
    {expanded, changed?} = expand_step(ast, env)

    if changed? do
      trace_loop(expanded, env, max, step + 1, [entry | acc])
    else
      Enum.reverse([entry | acc])
    end
  end

  defp to_step(ast, step) do
    %{ast: ast, source: safe_to_string(ast), step_number: step}
  end

  @spec inspect_ast(Macro.t()) :: :ok
  def inspect_ast(ast) do
    IO.puts("== AST ==")
    IO.inspect(ast, structs: false, limit: :infinity)
    IO.puts("\n== SOURCE ==")
    IO.puts(safe_to_string(ast))
    :ok
  end

  @spec print_trace([step()]) :: :ok
  def print_trace(steps) do
    Enum.each(steps, fn %{step_number: n, source: src} ->
      IO.puts("--- step #{n} ---")
      IO.puts(src)
    end)

    :ok
  end

  defp safe_to_string(ast) do
    try do
      Macro.to_string(ast)
    rescue
      _ -> inspect(ast)
    end
  end
end
```

### Step 2: `lib/macro_expand_debug/sample_macros.ex`

```elixir
defmodule MacroExpandDebug.SampleMacros do
  @moduledoc """
  A set of macros that expand in interesting ways — used to exercise the Inspector.
  """

  defmacro greet(name) do
    quote do
      "hello, " <> unquote(name)
    end
  end

  defmacro unless_positive(n, do: block) do
    quote do
      if unquote(n) > 0 do
        :skipped
      else
        unquote(block)
      end
    end
  end

  defmacro assert_type!(value, type) do
    check =
      case type do
        :integer -> quote do: is_integer(unquote(value))
        :binary -> quote do: is_binary(unquote(value))
        :map -> quote do: is_map(unquote(value))
      end

    quote do
      unless unquote(check) do
        raise ArgumentError, "expected #{unquote(type)}, got: " <> inspect(unquote(value))
      end

      unquote(value)
    end
  end
end
```

### Step 3: Tests

```elixir
defmodule MacroExpandDebug.InspectorTest do
  use ExUnit.Case, async: true

  alias MacroExpandDebug.Inspector
  require MacroExpandDebug.SampleMacros, as: SM

  describe "expand_step/2" do
    test "expands unless into if-not" do
      ast = quote do: unless(true, do: :x)
      {expanded, changed?} = Inspector.expand_step(ast, __ENV__)
      assert changed?
      source = Macro.to_string(expanded)
      assert source =~ "if"
      refute source =~ "unless"
    end

    test "returns changed? false for fully expanded AST" do
      ast = quote do: 1 + 1
      {result, changed?} = Inspector.expand_step(ast, __ENV__)
      refute changed?
      assert result == ast
    end
  end

  describe "trace_expansion/3" do
    test "captures multiple expansion steps" do
      ast = quote do: SM.greet("x")
      steps = Inspector.trace_expansion(ast, __ENV__, 5)
      assert length(steps) >= 2
      assert hd(steps).source =~ "greet"
      assert List.last(steps).source =~ "hello"
    end

    test "stops when AST no longer changes" do
      ast = quote do: 42
      steps = Inspector.trace_expansion(ast, __ENV__, 10)
      assert length(steps) == 1
    end

    test "respects max_steps" do
      ast = quote do: unless(true, do: (unless false, do: :ok))
      steps = Inspector.trace_expansion(ast, __ENV__, 1)
      assert length(steps) == 2  # initial + 1 step
    end
  end

  describe "sample macros" do
    test "assert_type! injects a runtime check" do
      ast = quote do: SM.assert_type!(x, :integer)
      expanded = Macro.expand(ast, __ENV__)
      source = Macro.to_string(expanded)
      assert source =~ "is_integer"
      assert source =~ "raise"
    end
  end
end
```

---

## Trade-offs and production gotchas

**1. `Macro.expand_once/2` requires a valid `Macro.Env`.** In tests, `__ENV__` works.
When feeding user input from a script, build the env with `Code.env_for_eval/1`.
Without it, macros that rely on imports raise "is reserved" errors.

**2. `Macro.to_string/1` is lossy.** Re-parsing its output may produce a slightly
different AST (metadata lost). It is good enough for debugging, not for
round-tripping source transformations.

**3. `expand/2` can loop.** If a macro expands to itself (rare but possible during
buggy development), `Macro.expand/2` would loop. `trace_expansion/3` with
`max_steps` prevents this.

**4. `dbg/1` replaces print debugging at runtime, not compile time.** Using `dbg/1`
inside a macro body — remember that it runs at *compile time*, printing AST forms.
Prefer `IO.puts(Macro.to_string(...))` or the Inspector module.

**5. Hygiene complicates the output.** Variables appear as `{:var, [counter: 123], nil}`
after expansion. `Macro.to_string/1` usually renders them reasonably, but
collisions appear as generated suffixes.

**6. `Macro.Env` carries compile-time state.** Calling expansion from outside a
compilation context (e.g. a script) means imports/aliases are NOT the ones the
original macro user had — resulting in false "does not expand" results.

**7. `dbg/1` CLI integration.** In Elixir 1.14+, `iex -S mix` + `dbg` is typically
enough for runtime debugging. Reserve `Inspector` for compile-time diagnosis.

**8. When NOT to build this.** For casual macro debugging, the REPL
one-liner `IO.puts(Macro.to_string(Macro.expand(quote(do: ...), __ENV__)))` does
the job. Build an Inspector module only if macro debugging is a team-wide
activity.

---

## Resources

- [`Macro` — hexdocs.pm](https://hexdocs.pm/elixir/Macro.html) — `expand_once`, `expand`, `to_string`
- [`Macro.Env` docs](https://hexdocs.pm/elixir/Macro.Env.html)
- [Kernel.dbg/1 source](https://github.com/elixir-lang/elixir/blob/main/lib/elixir/lib/kernel.ex) — search for `defmacro dbg`
- [`Code.env_for_eval/1`](https://hexdocs.pm/elixir/Code.html#env_for_eval/1)
- [*Metaprogramming Elixir* — debugging chapter](https://pragprog.com/titles/cmelixir/metaprogramming-elixir/)
- [Dashbit blog on macro internals](https://dashbit.co/blog)
