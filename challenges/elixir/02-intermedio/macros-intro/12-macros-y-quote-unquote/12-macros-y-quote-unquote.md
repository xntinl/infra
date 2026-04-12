# Macros and quote/unquote — the AST in your hands

**Project**: `macro_basics` — a handful of tiny macros that show how Elixir code becomes data, how `quote` captures it, and how `unquote` splices runtime values back in.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Project context

Before you can write a useful macro you need a very concrete mental model of
what a macro *is*: a function that runs at compile time, takes **AST** as
input, and returns **AST** as output. The compiler then splices that AST back
into the caller's code.

In this exercise you'll poke at the AST directly with `quote/2`, splice values
back in with `unquote/1`, and write your first `defmacro` that does something
you couldn't do with a regular function: inspect the *code* its caller wrote,
not the value that code evaluates to.

You'll also meet **hygiene** for the first time: why a variable you bind
inside a quoted block doesn't leak into the caller, and why that's a feature,
not a bug.

Project structure:

```
macro_basics/
├── lib/
│   └── macro_basics.ex
├── test/
│   └── macro_basics_test.exs
└── mix.exs
```

---

## Core concepts

### 1. Elixir code is a tree of tuples

Every Elixir expression has an AST representation: a 3-tuple
`{name, metadata, args}` for calls, or a literal for atoms, numbers, and
binaries. `quote do ... end` returns that tree *without* evaluating it:

```
quote do: 1 + 2
#=> {:+, [context: Elixir, import: Kernel], [1, 2]}
```

A macro is just a function `AST -> AST`. `defmacro` wires it into the
compiler so the returned AST is spliced at the call site.

### 2. `unquote/1` injects a value into quoted code

Inside a `quote` block, `unquote(expr)` evaluates `expr` *now* (at macro
expansion time) and splices the result into the AST. Without it, the name
would be a literal reference, not a substitution:

```
value = 42
quote do: x = unquote(value)   # => x = 42 in the AST
quote do: x = value            # => x = value — references a *variable* named value
```

### 3. `defmacro` vs `def`

A `def` receives **values**. A `defmacro` receives **AST**. That's the whole
difference. Everything else (pattern matching, guards, clauses) works the
same, but the arguments are trees, not runtime data.

```elixir
defmacro my_macro(expr) do
  # expr is AST, e.g. {:+, _, [1, 2]} — not the number 3
  quote do: IO.inspect(unquote(expr))
end
```

### 4. Hygiene, in one sentence

Variables introduced inside `quote` live in the macro's scope, not the
caller's. That prevents accidental capture and makes macros composable. You
*can* break hygiene with `var!/2` when you really need to — but you almost
never should. See exercise 94 for the pitfalls.

---

## Implementation

### Step 1: Create the project

```bash
mix new macro_basics
cd macro_basics
```

### Step 2: `lib/macro_basics.ex`

```elixir
defmodule MacroBasics do
  @moduledoc """
  A small tour of quote/unquote and defmacro.

  The macros here are deliberately trivial — the point is to see the AST
  transformation, not to build anything production-worthy.
  """

  @doc """
  Returns the AST of an expression without evaluating it.

  This is a macro because it needs the *unevaluated* form of its argument —
  a regular function would receive the already-computed value.
  """
  defmacro ast_of(expr) do
    # `expr` is already AST here. We quote it *inside another quote* so that
    # at the call site we get back a literal representation of that AST.
    quoted = Macro.escape(expr)
    quote do: unquote(quoted)
  end

  @doc """
  Logs the source form of an expression alongside its value.

  Example:

      iex> MacroBasics.debug(1 + 2 * 3)
      [debug] 1 + 2 * 3 = 7
      7
  """
  defmacro debug(expr) do
    # Macro.to_string turns AST back into source — very useful for error
    # messages and logging macros. Notice we use it at *expansion time*,
    # so the string is baked into the compiled output.
    source = Macro.to_string(expr)

    quote do
      value = unquote(expr)
      IO.puts("[debug] " <> unquote(source) <> " = " <> inspect(value))
      value
    end
  end

  @doc """
  `times(n, do: block)` — run `block` `n` times.

  Demonstrates a macro that wraps a block (`do: ...`) and splices it into
  a generated loop. Because the block is AST, it is *re-evaluated* on every
  iteration — exactly what you'd expect from a language construct.
  """
  defmacro times(n, do: block) do
    quote do
      Enum.each(1..unquote(n), fn _ -> unquote(block) end)
    end
  end

  @doc """
  Defines a constant function at compile time.

  `defconst greeting, "hello"` expands into `def greeting, do: "hello"`.
  A first taste of code-generating macros.
  """
  defmacro defconst(name, value) do
    quote do
      def unquote(name)(), do: unquote(value)
    end
  end
end
```

### Step 3: `test/macro_basics_test.exs`

```elixir
defmodule MacroBasicsTest do
  use ExUnit.Case, async: true
  import ExUnit.CaptureIO
  require MacroBasics

  describe "ast_of/1" do
    test "returns AST, not the value" do
      ast = MacroBasics.ast_of(1 + 2)
      assert match?({:+, _, [1, 2]}, ast)
    end
  end

  describe "debug/1" do
    test "prints the source and returns the value" do
      output =
        capture_io(fn ->
          assert MacroBasics.debug(1 + 2 * 3) == 7
        end)

      assert output =~ "1 + 2 * 3"
      assert output =~ "= 7"
    end
  end

  describe "times/2" do
    test "runs the block n times" do
      {:ok, agent} = Agent.start_link(fn -> 0 end)

      MacroBasics.times(5, do: Agent.update(agent, &(&1 + 1)))

      assert Agent.get(agent, & &1) == 5
    end
  end

  describe "defconst/2" do
    # Use a throwaway module to exercise code generation.
    defmodule Consts do
      require MacroBasics
      MacroBasics.defconst(:pi, 3.14159)
      MacroBasics.defconst(:answer, 42)
    end

    test "generates compile-time constants" do
      assert Consts.pi() == 3.14159
      assert Consts.answer() == 42
    end
  end
end
```

### Step 4: Run

```bash
mix test
```

Play with it in IEx to build intuition:

```
iex> quote do: 1 + 2
iex> quote do: if true, do: :yes, else: :no
iex> Macro.to_string(quote do: Enum.map([1,2,3], &(&1 * 2)))
```

---

## Trade-offs and production gotchas

**1. Macros complicate stack traces and debuggers**
When a macro expands, the resulting code appears to come from the caller's
line, but errors may reference the macro's internals. Tooling (ElixirLS,
error messages) handles this well, but junior readers get lost fast. Write
a function unless the macro earns its keep.

**2. `require` is mandatory to call a macro**
`defmacro` is not callable without `require Module` first (or `import`).
This is because macros run at compile time and the compiler needs to know
to load the module early. Forgetting `require` yields a confusing
"undefined function" error.

**3. Do everything you can at runtime in a function; use macros for code shape**
Rule of thumb: if the same thing could be done by a function, it should be.
Macros are for things functions *cannot* do: inspecting source code, generating
new function heads, compile-time assertions, DSL syntax.

**4. `Macro.escape/1` is needed for non-AST values inside `quote`**
If you want to splice a complex term (a map, a struct, a tuple that happens
to look like AST) into a quote, wrap it in `Macro.escape/1`. Otherwise the
compiler tries to interpret its shape as code and usually crashes.

**5. Quoted code is a contract with your caller**
Every identifier you leak, every import you assume, every variable you
capture is part of the public surface of the macro. Treat macro output like
a public API — small, well-documented, hygienic by default.

**6. When NOT to use a macro**
Skip the macro when a higher-order function, protocol, or behaviour would
do the job. Libraries like Ecto and Phoenix use macros heavily, but they do
so because they need to generate functions at compile time from user
declarations — a job functions can't do. If your use case is "pass some
logic around," use a function or a fun.

---

## Resources

- [`Kernel.SpecialForms.quote/2`](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#quote/2) — the compiler primitive, exhaustively documented
- [`Macro` — stdlib helpers](https://hexdocs.pm/elixir/Macro.html) — `to_string/1`, `escape/1`, `expand/2`, `prewalk/2`
- ["Metaprogramming Elixir" — Chris McCord](https://pragprog.com/titles/cmelixir/metaprogramming-elixir/) — the canonical book; chapters 1–3 cover this exercise
- ["Macros" — Elixir guide](https://hexdocs.pm/elixir/macros.html) — the official conceptual intro
- [Sasa Juric — "Understanding Elixir macros"](https://www.theerlangelist.com/article/macros_1) — a six-part series, start here
