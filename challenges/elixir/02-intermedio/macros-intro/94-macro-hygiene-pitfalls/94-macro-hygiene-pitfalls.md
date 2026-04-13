# Macro hygiene pitfalls — var!, accidental capture, and when to break the rules

**Project**: `hygiene_pitfalls` — reproduce and understand the classic macro hygiene pitfalls: accidental variable capture, how Elixir prevents it by default, when `var!/2` is the correct escape hatch, and how to use `Macro.expand/2` to audit what your macros actually generate.

---

## Project context

Hygiene is Elixir's built-in protection against macro-generated code
accidentally colliding with caller-defined variables. It's what lets you
use a macro dozens of times in a function body without ever having to
wonder what secret variables the macro is using internally.

But hygiene also means that **you cannot casually share variables between
macro output and caller code**. Some DSLs genuinely need that — Ecto's
`from/2` "introduces" a bindings variable into the caller's scope, for
instance. For those rare cases Elixir offers `var!/2`, which deliberately
breaks hygiene.

This exercise will:

1. Show the default, hygienic behaviour and why it's good.
2. Reproduce an accidental-capture pitfall and explain how hygiene
   prevented it.
3. Use `var!/2` to intentionally introduce a variable into the caller's
   scope.
4. Use `Macro.expand/2` to audit any macro's output so you stop guessing.

Project structure:

```
hygiene_pitfalls/
├── lib/
│   └── hygiene_pitfalls.ex
├── test/
│   └── hygiene_pitfalls_test.exs
└── mix.exs
```

---

## Why hygiene by default and not opt-in

Macros no-hygienic ponen la carga en cada caller: "acordate qué
variables usa este macro". Hygiene invierte el default: macros
seguros por defecto, `var!/2` como opt-out explícito para DSLs que
necesitan compartir una variable nombrada (Ecto `from/2`). Hacer que
el caso peligroso sea el ruidoso es todo el diseño.

---

## Core concepts

### 1. Hygiene in one sentence

Variables introduced inside a `quote` live in the macro's context, not
the caller's. Even if the names match, they're different variables from
the compiler's point of view (they carry different `:context` metadata).

### 2. Accidental capture — the pitfall hygiene prevents

In many Lisps without hygiene, a macro that binds `result` internally
would clobber a `result` variable in the caller. In Elixir:

```elixir
defmacro with_result(do: block) do
  quote do
    result = unquote(block)
    {:ok, result}
  end
end

# Caller:
result = :caller_owned
{:ok, x} = with_result(do: 42)
IO.inspect(result)   # still :caller_owned — hygiene kept them separate
```

Without hygiene, `result = :caller_owned` would have been clobbered. With
hygiene, the macro's `result` is a different variable from the caller's
`result`.

### 3. `var!/2` — intentional capture

`var!(x)` inside `quote` means "resolve `x` in the **caller's context**,
not mine." This is the escape hatch for DSLs that genuinely need to
introduce or read a named variable in the caller's scope. Use it
sparingly and document it loudly — you are opting out of the safety net.

```elixir
defmacro bind_counter do
  quote do
    var!(counter) = 0
  end
end

# Caller:
bind_counter()
counter = counter + 1   # works — macro introduced `counter` into caller's scope
```

### 4. `Macro.expand/2` — audit your macro's output

When you're not sure what a macro generates (especially around hygiene),
evaluate:

```
Macro.expand(quote(do: my_macro(...)), __ENV__)
|> Macro.to_string()
|> IO.puts
```

Look at the variable metadata with `Macro.expand_once/2` and
`IO.inspect(ast, structs: false)` — the `:context` field on each variable
node tells you which scope it belongs to.

---

## Design decisions

**Option A — Pasar estado compartido como argumento al macro**
- Pros: Explícito; sin break de hygiene.
- Cons: Boilerplate; no encaja con DSLs de contexto implícito.

**Option B — Usar `var!/2` para introducir una variable nombrada** (elegida solo para casos DSL)
- Pros: Habilita DSLs de binding implícito.
- Cons: Contrato-por-nombre permanente; colisiones silenciosas.

→ Elegida **A** como regla default: romper hygiene solo cuando la
semántica del DSL **es** introducir un binding nombrado.

---

## Implementation

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Standard library: no external dependencies required
    {:"ecto", "~> 1.0"},
    {:"plug", "~> 1.0"},
  ]
end
```


### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — isolated from any external state, so we demonstrate this concept cleanly without dependencies.


```bash
mix new hygiene_pitfalls
cd hygiene_pitfalls
```

### Step 2: `lib/hygiene_pitfalls.ex`

**Objective**: Implement `hygiene_pitfalls.ex` — AST manipulation that runs at compile time — making the macro's hygiene and unquoting choices observable.


```elixir
defmodule HygienePitfalls do
  @moduledoc """
  A tour of macro hygiene: what it protects you from, how to break it
  on purpose with `var!/2`, and how to audit a macro's expansion with
  `Macro.expand/2`.
  """

  # ── Hygienic by default ─────────────────────────────────────────────────

  @doc """
  Internally binds a variable named `result`. Hygiene ensures that a
  caller-owned `result` is not clobbered.
  """
  defmacro tag(do: block) do
    quote do
      # `result` here belongs to the macro's context — a different
      # variable from any `result` the caller may have bound.
      result = unquote(block)
      {:tagged, result}
    end
  end

  # ── Deliberately unhygienic: var!/2 ─────────────────────────────────────

  @doc """
  Introduces a variable named `counter` **into the caller's scope**.

  Using `var!/2` opts out of hygiene. This is the right tool when the
  macro is an explicit binding construct (Ecto's `from/2` uses the same
  mechanism to introduce the bindings it queries).
  """
  defmacro bind_counter(initial \\ 0) do
    quote do
      var!(counter) = unquote(initial)
    end
  end

  @doc """
  Reads and increments the caller's `counter` variable.

  Pairs with `bind_counter/1` — the two macros *share* a caller-owned
  variable. Only use this shape when the shared variable is part of your
  DSL's documented contract.
  """
  defmacro inc_counter do
    quote do
      var!(counter) = var!(counter) + 1
    end
  end

  # ── Expansion auditor ──────────────────────────────────────────────────

  @doc """
  Returns the fully-expanded source of an AST, as a string.

  Useful during development to see what a macro actually emits —
  including the hidden `:context` markers that drive hygiene.
  """
  @spec expand_source(Macro.t(), Macro.Env.t()) :: String.t()
  def expand_source(ast, env) do
    ast
    |> Macro.expand(env)
    |> Macro.to_string()
  end
end
```

### Step 3: `test/hygiene_pitfalls_test.exs`

**Objective**: Write `hygiene_pitfalls_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule HygienePitfallsTest do
  use ExUnit.Case, async: true
  require HygienePitfalls

  describe "hygiene by default" do
    test "macro-internal `result` does not clobber caller's `result`" do
      result = :caller_owned

      {:tagged, macro_result} =
        HygienePitfalls.tag do
          :macro_owned
        end

      # The macro's internal `result` was distinct from the caller's.
      assert result == :caller_owned
      assert macro_result == :macro_owned
    end

    test "macro can be nested without stepping on its own scope" do
      {:tagged, inner} =
        HygienePitfalls.tag do
          {:tagged, inner} = HygienePitfalls.tag(do: 1)
          inner
        end

      assert inner == 1
    end
  end

  describe "var!/2 — intentional capture" do
    test "bind_counter introduces `counter` into the caller's scope" do
      HygienePitfalls.bind_counter(10)
      # `counter` is now a real binding in THIS function's scope.
      assert counter == 10
    end

    test "inc_counter reads and writes the same caller-owned variable" do
      HygienePitfalls.bind_counter(0)
      HygienePitfalls.inc_counter()
      HygienePitfalls.inc_counter()
      HygienePitfalls.inc_counter()
      assert counter == 3
    end
  end

  describe "Macro.expand/2 — auditing expansions" do
    test "expands a hygienic macro and shows the generated structure" do
      ast = quote do: HygienePitfalls.tag(do: 42)
      source = HygienePitfalls.expand_source(ast, __ENV__)

      # After expansion we see a case/assign/tuple shape.
      assert source =~ "result"
      assert source =~ "{:tagged,"
    end

    test "expands an unhygienic macro and surfaces the caller-var reference" do
      ast = quote do: HygienePitfalls.bind_counter(5)
      source = HygienePitfalls.expand_source(ast, __ENV__)

      # The generated source assigns into `counter` (caller scope, via var!).
      assert source =~ "counter"
      assert source =~ "5"
    end
  end
end
```

### Step 4: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
```

Experiment in IEx:

```
iex> require HygienePitfalls
iex> ast = quote do: HygienePitfalls.tag(do: 1)
iex> Macro.expand(ast, __ENV__) |> Macro.to_string() |> IO.puts
```

### Why this works

Las variables dentro de `quote` llevan un campo `:context` en su
metadata AST (el módulo del macro), que el compilador usa para
distinguir "mi `result`" de "tu `result`". `var!(x)` setea ese
contexto a `nil`, diciéndole al compilador "resolvé esto en el
scope del caller". `Macro.expand/2` devuelve el AST para inspeccionar
los `:context` directamente.

---


## Deep Dive: State Management and Message Handling Patterns

Understanding state transitions is central to reliable OTP systems. Every `handle_call` or `handle_cast` receives current state and returns new state—immutability forces explicit reasoning. This prevents entire classes of bugs: missing state updates are immediately visible.

Key insight: separate pure logic (state → new state) from side effects (logging, external calls). Move pure logic to private helpers; use handlers for orchestration. This makes servers testable—test pure functions independently.

In production, monitor state size and mutation frequency. Unbounded growth is a memory leak; excessive mutations signal hot spots needing optimization. Always profile before reaching for performance solutions like ETS.

## Benchmark

<!-- benchmark N/A: hygiene es expand-time; no agrega costo en
runtime. Las variables hygienic compilan a los mismos opcodes BEAM. -->

---

## Trade-offs and production gotchas

**1. Hygiene is a feature, not an obstacle**
Every time hygiene "prevents" something you wanted, take a breath before
reaching for `var!/2`. Nine times out of ten, passing the value as an
argument or returning it from the macro is the right design. `var!` is
for the tenth case.

**2. `var!/2` turns your macro into a contract-by-variable-name**
Once you `var!(foo)`, everyone calling your macro has to know that
`foo` exists in their scope. Document it. Warn when it doesn't exist
(Elixir will give a compile warning about undefined `foo` in the
caller). If the variable name collides with something the caller
already uses, you *will* overwrite it — silently.

**3. Hygiene does not protect against remote function calls**
If your macro emits `IO.inspect(...)` and the caller shadows `IO` with
their own alias, the emitted code will follow the caller's alias, not
your intent. Use fully-qualified references and `alias` inside the
macro with care. `Kernel.SpecialForms.alias/2` has `:as` if you need to
be explicit.

**4. Generated helper functions live in the caller's module**
A macro that emits `def helper(...)` places that function in the
**caller's** module. If two users of your macro both import it, they
both grow a `helper/1`. Use hard-to-collide names or generate a module
instead.

**5. `Macro.expand/2` vs `Macro.expand_once/2`**
`expand/2` expands recursively until no more macros fire —
`expand_once/2` stops after one step. When auditing for hygiene,
`expand_once/2` is usually more informative because it shows your
macro's output before subsequent expansions dilute it.

**6. When NOT to break hygiene**
Almost always. Breaking hygiene turns local reasoning into global
reasoning: the caller now needs to know about your macro's variable
names forever. Ecto and a handful of library-level DSLs earn it. Most
application code does not. Treat `var!` as a "license required" tool.

---

## Reflection

- Heredás un macro con `var!(conn)` asumiendo que el caller tiene
  `conn` de Plug. Un compañero lo llama desde LiveView donde `conn`
  no existe. El compilador emite warning pero compila. ¿Qué agregás
  al macro para convertir eso en error explícito en expand-time?
- Diseñás un DSL que necesita "introducir una variable con el nombre
  de una tabla SQL" (Ecto con `u in User`). ¿`var!/2` o un approach
  distinto (fn bindings -> ...)? Justificá en términos de legibilidad.

---

## Resources

- [`Kernel.SpecialForms.quote/2` — the `:context` option and hygiene](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#quote/2-hygiene-in-variables)
- [`Kernel.SpecialForms.var!/2`](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#var!/2)
- [`Macro.expand/2`](https://hexdocs.pm/elixir/Macro.html#expand/2) and [`Macro.expand_once/2`](https://hexdocs.pm/elixir/Macro.html#expand_once/2)
- [`Macro.var/2`](https://hexdocs.pm/elixir/Macro.html#var/2) — generating hygienic variables from a macro programmatically
- ["Metaprogramming Elixir" — Chris McCord](https://pragprog.com/titles/cmelixir/metaprogramming-elixir/) — chapter dedicated to hygiene
- [`Ecto.Query.from/2` source](https://github.com/elixir-ecto/ecto/blob/master/lib/ecto/query.ex) — a disciplined, production-grade use of `var!/2`
- [Sasa Juric — "Understanding Elixir macros, part 4: diving deeper"](https://www.theerlangelist.com/article/macros_4)
