# Guard-friendly macros with defguard and Macro.Env.in_guard?

**Project**: `guard_macros` — reusable guards via `defguard`, plus a dual-purpose macro that emits guard-legal code inside `when` and richer code outside, using `Macro.Env.in_guard?/1`.

---

## Project context

Guards in Elixir are intentionally restricted: only a fixed list of
operators and BIFs are allowed inside `when`. That keeps pattern matching
fast and side-effect-free. But every non-trivial project eventually wants
"check that X is a valid user id" or "check that this is a positive even
integer" as a predicate that works **both** in a `when` clause **and** as
a regular boolean.

Elixir ships two tools for this:

- `defguard/1` — names a guard-legal expression so you can reuse it.
- `Macro.Env.in_guard?/1` — at macro expansion time, asks "am I being
  expanded inside a `when` clause right now?". Lets you emit guard-legal
  code in guard context and richer code (like `raise` or `log`) outside.

This exercise builds both patterns and a test suite that verifies the
macro works in `def ... when`, in `if`, and in `case ... when` equally.

Project structure:

```
guard_macros/
├── lib/
│   └── guard_macros.ex
├── test/
│   └── guard_macros_test.exs
└── mix.exs
```

---

## Why dual-context and not two separate names

La alternativa obvia es `is_user_id_guard/1` (un `defguard`) más
`user_id?/1` (una función). Funciona, pero cada caller tiene que
elegir el nombre correcto para su contexto — un impuesto que crece con
el codebase. Un único `is_user_id/1` que introspecciona
`Macro.Env.in_guard?/1` permite escribir el mismo nombre en todos
lados, y el macro emite la forma legal para el contexto actual.

---

## Core concepts

### 1. What counts as guard-safe

Inside a `when` clause, you may only call:

- The operators in `Kernel` (`+`, `-`, `<`, `and`, etc.).
- A fixed list of BIFs like `is_atom/1`, `is_integer/1`, `map_size/1`,
  `binary_part/3`, `tuple_size/1`.
- Other `defguard`s (they expand to guard-safe AST).

Anything else — user-defined functions, `String.length/1`, `Enum.any?/2` —
is not allowed. The compiler will reject the clause.

### 2. `defguard` is a macro, not a function

`defguard is_adult(age) when age >= 18` expands into a regular macro that
takes the `age` AST and returns a guard-legal AST. Because it's a macro,
it's invisible in stack traces (there's no call at runtime) and has no
overhead.

### 3. `Macro.Env.in_guard?/1` — the context sniffer

Inside a `defmacro`, `__CALLER__` is a `%Macro.Env{}`. Calling
`Macro.Env.in_guard?(__CALLER__)` returns `true` if the macro is being
expanded inside a `when` clause. You can branch on it to emit different
code for the two contexts:

```elixir
defmacro my_check(x) do
  if Macro.Env.in_guard?(__CALLER__) do
    quote do: is_integer(unquote(x)) and unquote(x) > 0
  else
    quote do
      case unquote(x) do
        n when is_integer(n) and n > 0 -> true
        _ -> false
      end
    end
  end
end
```

### 4. Function-head vs. case-clause guards

Both `def foo(x) when ...` and `case x do ... when ... -> ... end` count
as guard context for the purposes of `Macro.Env.in_guard?/1`. Your macro
doesn't need to distinguish between them.

---

## Design decisions

**Option A — Duplicar `unquote(value)` en ambas ramas del form no-guard**
- Pros: Expansión más simple.
- Cons: Cualquier argumento con efectos corre dos veces.

**Option B — Bindear `value` a un local y pattern-matchear** (elegida)
- Pros: Argumento evaluado exactamente una vez; seguro para callers
  con side effects.
- Cons: Un local extra; AST generado marginalmente mayor.

→ Elegida **B** porque duplicar evaluación es un bug latente; bindear
una vez es el default correcto.

---

## Implementation

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Standard library: no external dependencies required
  ]
end
```


### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — isolated from any external state, so we demonstrate this concept cleanly without dependencies.


```bash
mix new guard_macros
cd guard_macros
```

### Step 2: `lib/guard_macros.ex`

**Objective**: Implement `guard_macros.ex` — AST manipulation that runs at compile time — making the macro's hygiene and unquoting choices observable.


```elixir
defmodule GuardMacros do
  @moduledoc """
  Demonstrates two guard-related metaprogramming patterns:

  1. Reusable guards with `defguard` (zero-overhead, guard-legal).
  2. Dual-context macros that emit different AST depending on whether
     they are expanded inside a `when` clause — using
     `Macro.Env.in_guard?/1`.
  """

  # ── Reusable guards ─────────────────────────────────────────────────────

  @doc """
  True when `n` is a non-negative integer. Safe in `when` clauses.
  """
  defguard is_nat(n) when is_integer(n) and n >= 0

  @doc """
  True when `v` is a non-empty list.
  """
  defguard is_nonempty_list(v) when is_list(v) and v != []

  @doc """
  True when `s` is a non-empty binary. Uses `byte_size/1`, which is
  guard-safe (BIF).
  """
  defguard is_nonempty_binary(s) when is_binary(s) and byte_size(s) > 0

  # ── Dual-context macro ──────────────────────────────────────────────────

  @doc """
  Checks that `value` is a valid "user id": a positive integer OR a
  non-empty binary.

  Inside a guard, expands to a guard-legal boolean expression.
  Outside a guard, expands to a `case` that can (optionally) log in the
  future — the shape is the same boolean today, but the richer context
  lets you evolve the macro without breaking guard callers.
  """
  defmacro is_user_id(value) do
    if Macro.Env.in_guard?(__CALLER__) do
      # Guard context: must be a single boolean AST node.
      quote do
        (is_integer(unquote(value)) and unquote(value) > 0) or
          (is_binary(unquote(value)) and byte_size(unquote(value)) > 0)
      end
    else
      # Non-guard context: free to use a more expressive form. Here we
      # evaluate `value` only once (important if the caller passed a
      # side-effecting expression).
      quote do
        case unquote(value) do
          v when is_integer(v) and v > 0 -> true
          v when is_binary(v) and byte_size(v) > 0 -> true
          _ -> false
        end
      end
    end
  end

  # ── Consumers of the guards, for easy testing ──────────────────────────

  @doc """
  Returns `:ok` only for valid user ids. Uses the dual-context macro
  inside a function-head guard.
  """
  @spec validate(term()) :: :ok | :error
  def validate(id) when is_user_id(id), do: :ok
  def validate(_), do: :error

  @doc """
  Counts natural numbers in a list using the reusable guard.
  """
  @spec count_nats(list()) :: non_neg_integer()
  def count_nats(list) when is_list(list) do
    Enum.count(list, fn
      n when is_nat(n) -> true
      _ -> false
    end)
  end
end
```

### Step 3: `test/guard_macros_test.exs`

**Objective**: Write `guard_macros_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule GuardMacrosTest do
  use ExUnit.Case, async: true
  import GuardMacros

  describe "is_nat/1 defguard" do
    test "accepts non-negative integers in guards" do
      assert count_nats([0, 1, 2, -1, :a, "x", 3]) == 4
    end

    test "works as a boolean outside a guard" do
      assert is_nat(0)
      assert is_nat(5)
      refute is_nat(-1)
      refute is_nat(:nope)
    end
  end

  describe "is_nonempty_binary/1" do
    test "distinguishes empty from non-empty binaries" do
      assert is_nonempty_binary("hi")
      refute is_nonempty_binary("")
      refute is_nonempty_binary(:atom)
    end
  end

  describe "is_user_id/1 — dual context" do
    test "in function-head guard: accepts positive ints and non-empty binaries" do
      assert GuardMacros.validate(1) == :ok
      assert GuardMacros.validate(42) == :ok
      assert GuardMacros.validate("abc") == :ok

      assert GuardMacros.validate(0) == :error
      assert GuardMacros.validate(-3) == :error
      assert GuardMacros.validate("") == :error
      assert GuardMacros.validate(:atom) == :error
    end

    test "in case-clause guard: same semantics" do
      classify = fn id ->
        case id do
          x when is_user_id(x) -> :valid
          _ -> :invalid
        end
      end

      assert classify.(7) == :valid
      assert classify.("u1") == :valid
      assert classify.(nil) == :invalid
    end

    test "in expression context: behaves as a boolean" do
      assert is_user_id(42)
      assert is_user_id("abc")
      refute is_user_id(0)
      refute is_user_id("")
    end

    test "in expression context: evaluates the argument only once" do
      {:ok, agent} = Agent.start_link(fn -> 0 end)

      side_effect = fn ->
        Agent.update(agent, &(&1 + 1))
        42
      end

      # If the non-guard branch duplicated `value`, the counter would be 2.
      assert is_user_id(side_effect.())
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

### Why this works

`defguard` expande a un macro cuya salida es garantizada guard-legal
(sin call en runtime, sin overhead). Para `is_user_id/1`, el macro
chequea `Macro.Env.in_guard?(__CALLER__)` en **expand-time**: dentro
de un `when` emite una única expresión booleana de BIFs; afuera, un
`case` que bindea el argumento una vez y patternmatchea. Mismo call
site, dos ASTs válidos — todo dirigido por la vista del compilador.

---


## Deep Dive: State Management and Message Handling Patterns

Understanding state transitions is central to reliable OTP systems. Every `handle_call` or `handle_cast` receives current state and returns new state—immutability forces explicit reasoning. This prevents entire classes of bugs: missing state updates are immediately visible.

Key insight: separate pure logic (state → new state) from side effects (logging, external calls). Move pure logic to private helpers; use handlers for orchestration. This makes servers testable—test pure functions independently.

In production, monitor state size and mutation frequency. Unbounded growth is a memory leak; excessive mutations signal hot spots needing optimization. Always profile before reaching for performance solutions like ETS.

## Benchmark

```elixir
import GuardMacros

{guard_path, _} =
  :timer.tc(fn ->
    Enum.each(1..1_000_000, fn _ ->
      GuardMacros.validate(42)
    end)
  end)

{expr_path, _} =
  :timer.tc(fn ->
    Enum.each(1..1_000_000, fn _ ->
      is_user_id(42)
    end)
  end)

IO.puts("guard: #{guard_path}µs, expr: #{expr_path}µs")
```

Target esperado: ambas rutas <0.5µs por call. Diferencia grande indica
mala optimización del `case`.

---

## Trade-offs and production gotchas

**1. Guard-legal AST is a small island**
Not every predicate you can express as a function can be expressed as a
guard. If your logic needs `String.length/1`, `Enum.any?/2`, or anything
beyond BIFs, you cannot make it a guard — you must make it a regular
function and call it before the `when`.

**2. `defguard` bodies are *expanded at each call site***
There's no function call at runtime — the expression is inlined. That's
great for performance but means a complicated guard balloons compiled
code size. Keep guards small; factor shared subexpressions into
additional `defguard`s.

**3. `Macro.Env.in_guard?/1` is the only reliable context sniffer**
Trying to detect guard context by inspecting the caller's AST is a
fool's errand — the AST has been normalized by the parser. Only
`__CALLER__` holds the metadata that answers the question.

**4. Duplicated AST means duplicated evaluation — outside guards**
Inside guards, the variable is already bound; duplicating its reference
is free. Outside guards, the argument is an arbitrary expression, and
spraying `unquote(value)` twice means evaluating it twice. Always bind
once in the non-guard branch.

**5. Macro expansion errors are cryptic**
When you emit something not guard-legal inside a `when`, the compiler
complains with "cannot invoke remote function X inside guard." Read
carefully, trace which branch of `in_guard?` you landed in, and fix the
AST — not the caller.

**6. When NOT to use `defguard` or dual-context macros**
If the predicate is used in exactly one `when` clause in your codebase,
just inline it. `defguard` is for reuse. And if you find yourself
branching on `in_guard?` more than once in a project, your API is asking
for two separate macros (or a macro + a function) with clearer names.

---

## Reflection

- Necesitás validar un "código postal argentino": 4 u 8 dígitos.
  `String.length/1` no es guard-legal. ¿Cómo reestructurás la API para
  que la validación rápida sea usable en `when`? Proponé dos capas.
- Un compañero duplica `is_user_id/1` como `is_admin_id/1` copiando.
  ¿Qué extraés en común? ¿Vale un helper que genere "dual-context ID
  checkers" o es sobreingeniería para dos usos?

---

## Resources

- [`Kernel.defguard/1`](https://hexdocs.pm/elixir/Kernel.html#defguard/1)
- [`Macro.Env.in_guard?/1`](https://hexdocs.pm/elixir/Macro.Env.html#in_guard?/1)
- ["Patterns and guards" — Elixir guide](https://hexdocs.pm/elixir/patterns-and-guards.html) — full list of guard-safe BIFs
- [`Kernel.SpecialForms.quote/2`](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#quote/2)
- ["Metaprogramming Elixir" — Chris McCord](https://pragprog.com/titles/cmelixir/metaprogramming-elixir/) — section on macro environments and context
- [Elixir 1.6 release notes](https://elixir-lang.org/blog/2018/01/17/elixir-v1-6-0-released/) — introduced `defguard/1`
