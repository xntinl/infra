# Implementing `unless` from scratch — your first real macro

**Project**: `my_unless` — re-implement Elixir's `unless` control structure using `defmacro`, `quote`, and `unquote`. The "hello world" of metaprogramming.

**Difficulty**: ★★☆☆☆
**Estimated time**: 1–2 hours

---

## Project context

Elixir is aggressively minimal at its core: `if`, `case`, and `cond` are
all you need, and *every other control structure* — including `unless`,
`while`, `with` — is either a macro in the standard library or sugar
over those primitives. `Kernel.unless/2`, for instance, is literally
`defmacro unless(condition, clauses) do ... end` in the Elixir source.

Rebuilding `unless` is the cleanest possible exercise in macros: small
enough to fit in your head, big enough to teach you how keyword
`do: ... else: ...` blocks are really passed, and generic enough that the
patterns transfer to every macro you'll ever write.

Project structure:

```
my_unless/
├── lib/
│   └── my_unless.ex
├── test/
│   └── my_unless_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `do:` and `else:` are just keyword list entries

When you write

```elixir
my_unless some_condition do
  :a
else
  :b
end
```

the compiler hands your macro two arguments: the condition AST, and the
keyword list `[do: quote_of_a, else: quote_of_b]`. The `do/end` block is
not magical; it's syntactic sugar for a keyword list whose values happen
to be the quoted block bodies.

### 2. Pattern-match on the keyword list, not on `do` alone

If you accept `do: body` you handle the single-block case. Most control
structures also want `else:`, so the idiomatic shape is two clauses or one
clause that matches both and defaults the `else` branch:

```elixir
defmacro my_unless(cond, do: do_block, else: else_block) do ... end
defmacro my_unless(cond, do: do_block),                 do: ... # else -> nil
```

### 3. The macro emits *more code* — it doesn't interpret anything

The goal of `unless` is to produce the AST of `if not cond, do: ..., else: ...`.
You don't evaluate `cond` in the macro. You *quote* around it. The compiler
substitutes your output in place of the macro call and carries on.

### 4. Literal `do_block` vs `unquote(do_block)`

This is where first-time macro writers get burned. The body you received is
AST — if you inline it verbatim in `quote`, it becomes a *reference to a
variable called `do_block`*, not the code the caller wrote. You must
`unquote(do_block)` to splice the AST in.

---

## Implementation

### Step 1: Create the project

```bash
mix new my_unless
cd my_unless
```

### Step 2: `lib/my_unless.ex`

```elixir
defmodule MyUnless do
  @moduledoc """
  A from-scratch implementation of `Kernel.unless/2`, intended as a
  teaching exercise. Do not use in production — `Kernel.unless` already
  exists, is faster at compile time, and is recognized by every tool.
  """

  @doc """
  Executes `do_block` when `condition` is falsy; otherwise executes
  `else_block` (or returns `nil` if none was given).

  Expands to `if/2` under the hood, which means no runtime overhead
  versus the built-in.
  """
  defmacro my_unless(condition, do: do_block, else: else_block) do
    quote do
      if unquote(condition) do
        unquote(else_block)
      else
        unquote(do_block)
      end
    end
  end

  defmacro my_unless(condition, do: do_block) do
    quote do
      if unquote(condition) do
        nil
      else
        unquote(do_block)
      end
    end
  end
end
```

### Step 3: `test/my_unless_test.exs`

```elixir
defmodule MyUnlessTest do
  use ExUnit.Case, async: true
  import MyUnless

  describe "my_unless/2 with do only" do
    test "runs body when condition is false" do
      assert (my_unless false, do: :ran) == :ran
    end

    test "runs body when condition is nil" do
      assert (my_unless nil, do: :ran) == :ran
    end

    test "returns nil when condition is truthy" do
      assert (my_unless true, do: :ran) == nil
      assert (my_unless 1, do: :ran) == nil
    end
  end

  describe "my_unless/2 with do/else" do
    test "runs do branch when condition is falsy" do
      result =
        my_unless false do
          :primary
        else
          :fallback
        end

      assert result == :primary
    end

    test "runs else branch when condition is truthy" do
      result =
        my_unless :something do
          :primary
        else
          :fallback
        end

      assert result == :fallback
    end
  end

  describe "lazy evaluation" do
    test "does not evaluate the do branch when condition is truthy" do
      # If `unquote` were evaluated eagerly, this side-effecting expression
      # would always fire. It should only fire when the branch is taken.
      {:ok, agent} = Agent.start_link(fn -> 0 end)

      my_unless true, do: Agent.update(agent, &(&1 + 1))
      assert Agent.get(agent, & &1) == 0

      my_unless false, do: Agent.update(agent, &(&1 + 1))
      assert Agent.get(agent, & &1) == 1
    end
  end
end
```

### Step 4: Run

```bash
mix test
```

Poke at the expansion to see what's happening:

```
iex> require MyUnless
iex> ast = quote do: MyUnless.my_unless(x, do: :a, else: :b)
iex> Macro.expand(ast, __ENV__) |> Macro.to_string() |> IO.puts
```

You should see a plain `if` expression — no trace of `my_unless` remains
after expansion.

---

## Trade-offs and production gotchas

**1. A macro must not evaluate its arguments eagerly**
If your macro accidentally calls `unquote(expr)` outside a `quote` — for
example in a `case expr do ... end` at the top of the macro body — you
will evaluate the *AST*, not the code the caller wrote, and get bizarre
failures. Keep `unquote` strictly inside `quote`.

**2. Lazy evaluation is a guarantee, not an accident**
The whole reason `unless` is a macro and not a function is that each branch
must be evaluated *only if taken*. With a function, both branches run (to
produce the arguments). Tests that cover "the other branch isn't evaluated"
protect that guarantee from regressions.

**3. Operator precedence of `unless` with pipes is surprising**
`value |> transform |> unless cond, do: :a` doesn't parse the way most
readers expect. This is a Kernel-level reason to avoid `unless` entirely
and write `if not ...` in pipelines.

**4. Lint rules disagree with `unless/else`**
Credo and community style guides generally flag `unless ... else ...`
because it forces the reader to negate the condition mentally twice.
Implementing it is educational; using it is usually bad style.

**5. When NOT to use a handwritten `unless`**
In production, `Kernel.unless/2` is the correct answer — it's already
imported, documented, tooled, and battle-tested. This exercise exists
purely so you understand what's happening when you read Elixir source.

---

## Resources

- [`Kernel.unless/2` source](https://github.com/elixir-lang/elixir/blob/main/lib/elixir/lib/kernel.ex) — search for `defmacro unless`
- [`Kernel.SpecialForms.quote/2`](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#quote/2)
- ["Macros" — Elixir guide](https://hexdocs.pm/elixir/macros.html) — re-implements `unless` in the same spirit
- ["Metaprogramming Elixir" — Chris McCord](https://pragprog.com/titles/cmelixir/metaprogramming-elixir/) — chapter 1 walks through exactly this pattern
- [Credo `Credo.Check.Refactor.UnlessWithElse`](https://hexdocs.pm/credo/Credo.Check.Refactor.UnlessWithElse.html) — why `unless/else` is discouraged
