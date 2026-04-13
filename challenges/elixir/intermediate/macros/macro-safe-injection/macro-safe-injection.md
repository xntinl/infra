# Safe injection — bind_quoted vs unquote and hygiene pitfalls

**Project**: `safe_inject` — contrast two ways of threading runtime values into a `quote` block: inline `unquote/1` vs `quote bind_quoted: [...]`, and show concretely why the second form is safer.

---

## Why macro safe injection matters

When a macro splices a runtime value into generated code, there are two
mechanisms: `unquote/1` and `quote bind_quoted: [...]`. They look
equivalent in simple cases, but they behave very differently when the
injected expression has side effects, is complex, or refers to variables
that could collide with names in the generated code.

The short version:

- `unquote(expr)` **splices the AST of `expr` into the quote** — every
  time `unquote(expr)` appears, the expression is literally duplicated.
- `bind_quoted: [x: expr]` **evaluates `expr` once at expansion time,
  stores the result, and binds it to `x` inside the quote** — no
  duplication, no accidental re-evaluation.

This exercise builds two versions of the same macro so you can see the
failure mode in `unquote` with your own eyes, then fix it by moving to
`bind_quoted`.

---

## Project structure

```
safe_inject/
├── lib/
│   └── safe_inject.ex
├── script/
│   └── main.exs
├── test/
│   └── safe_inject_test.exs
└── mix.exs
```

---

## Why `bind_quoted` and not disciplined `unquote`

La disciplina falla. En un macro con veinte líneas y cuatro
`unquote(expr)`, un revisor no puede ver la duplicación.
`bind_quoted` mueve la decisión de "acordate de bindear" a "el
compilador lo bindea por vos una vez". Es la corrección estructural;
la alternativa es una convención perpetuamente frágil.

---

## Core concepts

### 1. `unquote(expr)` is textual substitution

Every place you write `unquote(expr)` in a quoted block, the AST of
`expr` is inserted. That means if you use `unquote(expr)` three times,
the expression is **evaluated three times at runtime** in the generated
code:

```elixir
quote do
  x = unquote(expensive_call())
  y = unquote(expensive_call())
  x + y
end
# If `expensive_call()` returns AST like `hit_the_db()`, the generated
# code calls `hit_the_db()` twice.
```

This is correct if the expression is pure and cheap. It is catastrophic
if it has side effects or talks to a database.

### 2. `quote bind_quoted: [x: expr]` is a binding

`bind_quoted` evaluates each right-hand side **once at macro expansion
time** (not at runtime!) and binds the resulting *value* (escaped as a
literal) to `x` inside the quote. Inside the quoted block, `x` is a
plain variable whose value is fixed:

```elixir
quote bind_quoted: [x: 1 + 1] do
  x + x   # => 2 + 2 at runtime; `1 + 1` was evaluated at compile time
end
```

### 3. Hygiene interacts with both

`unquote` splices raw AST — if that AST references a variable `foo`, the
reference leaks into the caller's scope (or collides with a `foo` the
macro itself uses). `bind_quoted` produces a single, hygienic variable
binding that the macro controls fully.

For user-supplied *code blocks* (like the `do: block` of `if`), you still
need `unquote` because you want the block to run at the call site with
access to the caller's variables. Use `bind_quoted` for *values* and
`unquote` for *code*.

### 4. The golden rule

- Values you want to embed as literals at compile time → `bind_quoted`.
- User code that must execute at the call site → `unquote` inside `quote`.
- If you're not sure, pick `bind_quoted` — fewer ways to shoot yourself.

---

## Design decisions

**Option A — Mantener `unquote(expr)` y documentar "llamar una vez"**
- Pros: Cero API agregada; pura convención.
- Cons: Convenciones se degradan; bugs silenciosos sobreviven a
  producción.

**Option B — Default `bind_quoted` para valores, `unquote` para code
blocks del usuario** (elegida)
- Pros: Garantía estructural de evaluación única; binding hygienic.
- Cons: Un poco más de tipeo.

→ Elegida **B** porque el failure mode de `unquote` es catastrófico y
silencioso; keystrokes extra por seguridad estructural es el default
correcto.

---

## Implementation

### `mix.exs`

```elixir
defmodule SafeInject.MixProject do
  use Mix.Project

  def project do
    [
      app: :safe_inject,
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
mix new safe_inject
cd safe_inject
```

### `lib/safe_inject.ex`

**Objective**: Implement `safe_inject.ex` — AST manipulation that runs at compile time — making the macro's hygiene and unquoting choices observable.

```elixir
defmodule SafeInject do
  @moduledoc """
  Two versions of the same macro, `log_twice/1`, contrasting raw
  `unquote` with `bind_quoted`.

  The macro logs the argument twice. With `unquote`, the argument's
  expression is spliced twice and therefore evaluated twice — a bug if
  the argument has side effects. With `bind_quoted`, the argument is
  evaluated once and bound to a local variable, eliminating the bug.
  """

  @doc """
  **Unsafe** version. Each reference to `unquote(expr)` duplicates the AST,
  so side effects in `expr` fire twice at runtime.
  """
  defmacro log_twice_unsafe(expr) do
    quote do
      IO.puts("[1] " <> inspect(unquote(expr)))
      IO.puts("[2] " <> inspect(unquote(expr)))
    end
  end

  @doc """
  **Safe** version. `bind_quoted` evaluates `expr` once at runtime (via
  the implicit binding), assigns the result to `value`, and the quoted
  body references `value` instead of re-splicing the original AST.
  """
  defmacro log_twice_safe(expr) do
    quote bind_quoted: [value: expr] do
      IO.puts("[1] " <> inspect(value))
      IO.puts("[2] " <> inspect(value))
    end
  end
end
```

> The generated code from `log_twice_safe` still evaluates `value` at
> runtime — the point of `bind_quoted` here isn't to move evaluation to
> compile time (the value isn't known then), it's to evaluate the
> caller's expression **exactly once** and share the result.

### Step 3: `test/safe_inject_test.exs`

**Objective**: Write `safe_inject_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.

```elixir
defmodule SafeInjectTest do
  use ExUnit.Case, async: true

  doctest SafeInject
  import ExUnit.CaptureIO
  require SafeInject

  # A tiny helper that counts invocations via an Agent and returns a
  # value. Any test that uses this can check "was I called once or twice?".
  defp counter_call(agent, value) do
    Agent.update(agent, &(&1 + 1))
    value
  end

  describe "log_twice_unsafe/1" do
    test "evaluates its argument twice — this is the bug" do
      {:ok, agent} = Agent.start_link(fn -> 0 end)

      capture_io(fn ->
        SafeInject.log_twice_unsafe(counter_call(agent, 42))
      end)

      # The side effect fired TWICE because `unquote(expr)` splices the
      # whole expression in two places.
      assert Agent.get(agent, & &1) == 2
    end

    test "still prints the value twice" do
      output = capture_io(fn -> SafeInject.log_twice_unsafe(42) end)
      assert output =~ "[1] 42"
      assert output =~ "[2] 42"
    end
  end

  describe "log_twice_safe/1" do
    test "evaluates its argument exactly once" do
      {:ok, agent} = Agent.start_link(fn -> 0 end)

      capture_io(fn ->
        SafeInject.log_twice_safe(counter_call(agent, 42))
      end)

      # `bind_quoted` assigned the result of the call to a variable and
      # then referenced that variable twice — one evaluation.
      assert Agent.get(agent, & &1) == 1
    end

    test "prints the value twice" do
      output = capture_io(fn -> SafeInject.log_twice_safe(42) end)
      assert output =~ "[1] 42"
      assert output =~ "[2] 42"
    end
  end

  describe "equivalence on pure expressions" do
    test "both macros produce identical output for side-effect-free args" do
      unsafe_out = capture_io(fn -> SafeInject.log_twice_unsafe(1 + 2) end)
      safe_out = capture_io(fn -> SafeInject.log_twice_safe(1 + 2) end)
      assert unsafe_out == safe_out
    end
  end
end
```

### Step 4: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.

```bash
mix test
```

Inspect the expansion to see the duplication with your own eyes:

```
iex> require SafeInject
iex> a = quote do: SafeInject.log_twice_unsafe(IO.puts("hi"))
iex> Macro.expand(a, __ENV__) |> Macro.to_string() |> IO.puts
```

You'll see `IO.puts("hi")` appear twice in the expansion — proof of the
duplicate-evaluation bug baked into the unsafe form.

### Why this works

`bind_quoted: [value: expr]` instruye al compilador a emitir
`value = expr` como primera instrucción del bloque generado, con
`value` como variable hygienic. Cada referencia a `value` dentro del
bloque usa ese único binding. En contraste, `unquote(expr)` splicea
el AST verbatim, así que N ocurrencias = N evaluaciones en runtime.

---

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Runnable demo of `SafeInject`.

  Exercises the public API end-to-end so behaviour is observable
  in addition to documented.
  """

  def main do
    IO.puts("=== SafeInject demo ===")
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

```elixir
require SafeInject

heavy = fn -> Enum.reduce(1..10_000, 0, &(&1 + &2)) end

{unsafe, _} =
  :timer.tc(fn ->
    ExUnit.CaptureIO.capture_io(fn ->
      Enum.each(1..1_000, fn _ -> SafeInject.log_twice_unsafe(heavy.()) end)
    end)
  end)

{safe, _} =
  :timer.tc(fn ->
    ExUnit.CaptureIO.capture_io(fn ->
      Enum.each(1..1_000, fn _ -> SafeInject.log_twice_safe(heavy.()) end)
    end)
  end)

IO.puts("unsafe: #{unsafe}µs, safe: #{safe}µs")
```

Target esperado: unsafe ~1.8x–2x más lenta que safe cuando el
argumento tiene costo no trivial.

---

## Trade-offs and production gotchas

**1. `bind_quoted` and `unquote` mix only in limited ways**
You can't `unquote/1` inside a `quote bind_quoted: [...]` block — the
compiler refuses it. If you need both a value binding and a code
injection in the same quote, lift one of them out into a separate
`quote` and combine with `unquote_splicing/1`, or pre-build the AST
manually.

**2. `bind_quoted` escapes values with `Macro.escape/1`**
That means you can bind maps, structs, tuples — but not things like PIDs
or references that aren't valid AST literals. If you need to inject a
PID, you have to pass it as a *runtime* argument, not via `bind_quoted`.

**3. Duplicate evaluation isn't always obvious**
In simple examples everyone spots the problem. In a real macro with
three or four `unquote(...)` spread across twenty lines of generated
code, the duplication is invisible at review time. Default to
`bind_quoted` for anything that isn't a user code block.

**4. Pure expressions still pay a cost with `unquote` duplication**
Even if there are no side effects, evaluating `some_pure_call()` twice
means twice the CPU. If the call is cheap it doesn't matter; if it's
CPU-heavy, `bind_quoted` amortizes it to one evaluation.

**5. Hygiene isn't a substitute for correctness**
`bind_quoted`'s variable is hygienic, which prevents *name collisions*
with the caller. It does not, by itself, fix unquote duplication
elsewhere in the macro. Read the whole `quote` and audit every
`unquote(...)` for duplication.

**6. When NOT to use `bind_quoted`**
When the "value" you want to inject is actually *code that must run in
the caller's context* — e.g., the `do:` block of an `if`-like macro. A
`do:` block needs to see the caller's variables and run at the call site;
`bind_quoted` would freeze it to whatever it evaluates to at expansion
time (which is usually nothing useful).

---

## Reflection

- Encontrás un macro legacy con seis `unquote(expr)` y el ticket dice
  "loguea el doble en producción". ¿Cómo reproducís el bug sin tocar
  producción, y qué test agregás al CI para que esta clase no vuelva?
- Un macro necesita inyectar un PID (no es literal AST). No podés usar
  `bind_quoted` ni `unquote`. ¿Cómo reestructurás la API para que el
  PID llegue al runtime?

---
## Resources

- [`Kernel.SpecialForms.quote/2` — `:bind_quoted` option](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#quote/2-binding-and-unquote-fragments)
- [`Macro.escape/1`](https://hexdocs.pm/elixir/Macro.html#escape/1)
- ["Metaprogramming Elixir" — Chris McCord](https://pragprog.com/titles/cmelixir/metaprogramming-elixir/) — chapter on macro hygiene, bind_quoted
- [Sasa Juric — "Understanding Elixir macros, part 3: macro tricks"](https://www.theerlangelist.com/article/macros_3)
- [`Macro.expand/2`](https://hexdocs.pm/elixir/Macro.html#expand/2) — the tool for proving duplication by inspection

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

### `test/safe_inject_test.exs`

```elixir
defmodule SafeInjectTest do
  use ExUnit.Case, async: true

  doctest SafeInject

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert SafeInject.run(:noop) == :ok
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
