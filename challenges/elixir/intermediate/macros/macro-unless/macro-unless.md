# Implementing `unless` from scratch — your first real macro

**Project**: `my_unless` — re-implement Elixir's `unless` control structure using `defmacro`, `quote`, and `unquote`. The "hello world" of metaprogramming.

---

## Why macro unless matters

Elixir is aggressively minimal at its core: `if`, `case`, and `cond` are
all you need, and *every other control structure* — including `unless`,
`while`, `with` — is either a macro in the standard library or sugar
over those primitives. `Kernel.unless/2`, for instance, is literally
`defmacro unless(condition, clauses) do ... end` in the Elixir source.

Rebuilding `unless` is the cleanest possible exercise in macros: small
enough to fit in your head, big enough to teach you how keyword
`do: ... else: ...` blocks are really passed, and generic enough that the
patterns transfer to every macro you'll ever write.

---

## Project structure

```
my_unless/
├── lib/
│   └── my_unless.ex
├── script/
│   └── main.exs
├── test/
│   └── my_unless_test.exs
└── mix.exs
```

---

## Why a macro and not a function

A function evaluates **both** arguments before running — its body and its
else-branch would always execute, defeating the "only run one branch"
contract. A macro receives the *unevaluated AST* of each branch and
emits code that evaluates the right one only when the condition demands
it. That lazy dispatch is what makes control structures possible.

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

## Design decisions

**Option A — Build `unless` with explicit `case not cond`**
- Pros: No dependency on `if/2`; entirely self-contained.
- Cons: Duplicates the `if` optimizer logic; slightly larger AST.

**Option B — Expand to `if not cond` and let the compiler optimize** (chosen)
- Pros: Inherits every compiler optimization that applies to `if`.
- Cons: Relies on `if/2` being stable (it is — it's a special form).

→ Chose **B** because `unless` is semantically "if with an inverted
condition"; reusing `if` is idiomatic and produces identical bytecode.

---

## Implementation

### `mix.exs`

```elixir
defmodule MyUnless.MixProject do
  use Mix.Project

  def project do
    [
      app: :my_unless,
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

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — isolated from any external state, so we demonstrate this concept cleanly without dependencies.

```bash
mix new my_unless
cd my_unless
```

### `lib/my_unless.ex`

**Objective**: Implement `my_unless.ex` — AST manipulation that runs at compile time — making the macro's hygiene and unquoting choices observable.

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

**Objective**: Write `my_unless_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.

```elixir
defmodule MyUnlessTest do
  use ExUnit.Case, async: true

  doctest MyUnless
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

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.

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

### Why this works

Each `defmacro` clause receives the caller's code as AST, wraps it in a
fresh `quote` whose body is `if unquote(condition) do ... else ... end`,
and returns that AST. Because the do/else blocks are spliced with
`unquote`, they run only when their branch is selected — lazy
evaluation preserved. The compiler then inlines the macro output at the
call site, leaving zero runtime overhead versus `Kernel.unless/2`.

---

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Runnable demo of `MyUnless`.

  Exercises the public API end-to-end so behaviour is observable
  in addition to documented.
  """

  def main do
    IO.puts("=== MyUnless demo ===")
    run()
    IO.puts("\nDone — demo finished without errors.")
  end

  defp run do
    :ok
  end
end

Main.main()
```

## Deep Dive: State Management and Message Handling Patterns

Understanding state transitions is central to reliable OTP systems. Every `handle_call` or `handle_cast` receives current state and returns new state—immutability forces explicit reasoning. This prevents entire classes of bugs: missing state updates are immediately visible.

Key insight: separate pure logic (state → new state) from side effects (logging, external calls). Move pure logic to private helpers; use handlers for orchestration. This makes servers testable—test pure functions independently.

In production, monitor state size and mutation frequency. Unbounded growth is a memory leak; excessive mutations signal hot spots needing optimization. Always profile before reaching for performance solutions like ETS.

## Benchmark

<!-- benchmark N/A: `my_unless` expands to the same AST as `if`; any
benchmark is effectively measuring `Kernel.if/2`, not the macro. La
comparación significativa es `Macro.expand/2` mostrando igualdad con la
forma `if`. -->

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

## Reflection

- Estás leyendo un codebase que usa `unless user.admin? && !feature_flag`
  anidado tres niveles. Antes de refactorizar, reformulá la condición en
  positivo y justificá si `unless` era la elección correcta aquí, o si
  `if` con un predicado nombrado habría sido más claro desde el inicio.
- Supongamos que Elixir elimina `if/2` mañana y solo queda `case/2`.
  ¿Cómo reescribís `my_unless` para que siga compilando? ¿Qué te enseña
  esa expansión sobre qué es realmente `unless`?

---
## Resources

- [`Kernel.unless/2` source](https://github.com/elixir-lang/elixir/blob/main/lib/elixir/lib/kernel.ex) — search for `defmacro unless`
- [`Kernel.SpecialForms.quote/2`](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#quote/2)
- ["Macros" — Elixir guide](https://hexdocs.pm/elixir/macros.html) — re-implements `unless` in the same spirit
- ["Metaprogramming Elixir" — Chris McCord](https://pragprog.com/titles/cmelixir/metaprogramming-elixir/) — chapter 1 walks through exactly this pattern
- [Credo `Credo.Check.Refactor.UnlessWithElse`](https://hexdocs.pm/credo/Credo.Check.Refactor.UnlessWithElse.html) — why `unless/else` is discouraged

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

### `test/my_unless_test.exs`

```elixir
defmodule MyUnlessTest do
  use ExUnit.Case, async: true

  doctest MyUnless

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert MyUnless.run(:noop) == :ok
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
