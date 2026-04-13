# quote bind_quoted vs unquote — when to prefer which

**Project**: `bind_quoted_demo` — a deeper look at `quote bind_quoted: [...]`, comparing it head-to-head with inline `unquote/1`, and showing the three canonical use cases where `bind_quoted` is strictly better.

---

## Project context

Dado `quote bind_quoted: [x: expr]`, el compilador evalúa `expr` una
vez en expand-time y bindea el resultado a una `x` hygienic dentro del
bloque generado. Esa regla tiene tres payoffs distintos:

1. **Loops en compile-time** — generar N function heads desde una
   lista, donde cada head embede un valor.
2. **Subexpresiones compartidas** — evaluar una expresión en runtime
   una sola vez y referenciar el resultado muchas veces dentro del
   bloque generado.
3. **Captura accidental de variables** — evitar un pitfall de hygiene
   donde `unquote` de una expresión referenciando variables del caller
   propaga bindings sorpresivos.

By the end you'll have a short mental rule: **values → `bind_quoted`;
user code → `unquote`**.

Project structure:

```
bind_quoted_demo/
├── lib/
│   └── bind_quoted_demo.ex
├── test/
│   └── bind_quoted_demo_test.exs
└── mix.exs
```

---

## Why `bind_quoted` by default and not "unquote first, refactor later"

Empezar con `unquote(expr)` por ser menos caracteres lleva a descubrir
un bug de duplicación o shadowing seis meses después. Defaultear a
`bind_quoted` elimina la clase de bug antes que empiece; `unquote`
queda reservado para code blocks del usuario.

---

## Core concepts

### 1. `bind_quoted` evaluates keys once at expansion time

Given `quote bind_quoted: [x: expr, y: other_expr], do: ...`, the
compiler:

1. Evaluates `expr` and `other_expr` **at macro expansion time**.
2. Escapes the resulting values with `Macro.escape/1`.
3. Binds them to `x` and `y` inside the generated block as hygienic
   variables.

Inside the `do: ...`, `x` and `y` look like ordinary variables. They can
be used in loops, conditionals, or nested quotes without re-evaluation.

### 2. Inside a `for` loop, `bind_quoted` is the easy path

When generating code from a list of values:

```elixir
for value <- values do
  quote bind_quoted: [v: value] do
    def name(unquote(v)), do: :matched
  end
end
```

…wait, you still need `unquote(v)` inside `def` because a function-head
pattern must be *AST*, not a variable reference. Which brings us to a
subtlety: `bind_quoted` replaces **body-level** values, not
pattern-position values. For pattern positions you still want `unquote`.
Exercise covers both cases.

### 3. `bind_quoted` refuses unquotable values

If you try `bind_quoted: [p: self()]`, the compiler errors because PIDs
aren't valid AST literals. `unquote` has the same restriction, but in
practice you notice errors later (at runtime) with `unquote`, whereas
`bind_quoted` rejects it cleanly at compile time.

### 4. The three use cases

- **Expensive or side-effecting expressions** used more than once —
  `bind_quoted` ensures one evaluation.
- **Loops that embed values** into repeated code — `bind_quoted` makes
  the intent (one value per iteration) explicit.
- **Values that would otherwise shadow caller variables** —
  `bind_quoted` generates hygienic bindings.

---

## Design decisions

**Option A — Usar `unquote` uniformemente**
- Pros: Un modelo mental para cada splice.
- Cons: Depende de que cada contribuidor recuerde "no compartir
  expresiones"; loops y valores con side effects son footguns.

**Option B — `bind_quoted` para valores, `unquote` para code/patterns** (elegida)
- Pros: Garantía del compilador; pattern positions usan `unquote`
  explícitamente.
- Cons: Dos modelos mentales.

→ Elegida **B** porque el split mapea a qué diseñó cada mecanismo.

---

## Implementation

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Standard library: no external dependencies required
    {:"ecto", "~> 1.0"},
    {:"phoenix", "~> 1.0"},
  ]
end
```


### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — isolated from any external state, so we demonstrate this concept cleanly without dependencies.


```bash
mix new bind_quoted_demo
cd bind_quoted_demo
```

### Step 2: `lib/bind_quoted_demo.ex`

**Objective**: Implement `bind_quoted_demo.ex` — AST manipulation that runs at compile time — making the macro's hygiene and unquoting choices observable.


```elixir
defmodule BindQuotedDemo do
  @moduledoc """
  Side-by-side patterns showing where `bind_quoted` is the right choice
  and where `unquote` remains necessary.
  """

  @doc """
  Use case 1 — shared sub-expression.

  `expr` is a runtime expression we want to log twice. With
  `bind_quoted`, the expression is evaluated once and bound to `value`,
  which is then referenced in both log lines.
  """
  defmacro log_twice(expr) do
    quote bind_quoted: [value: expr] do
      IO.puts("[1] " <> inspect(value))
      IO.puts("[2] " <> inspect(value))
      value
    end
  end

  @doc """
  Use case 2 — compile-time loop over a list, generating function heads.

  Each element `code` becomes a `def reason_for(code), do: "..."` pattern
  clause. Note: `unquote(code)` appears in the pattern position inside
  `def` — `bind_quoted` is not used for pattern positions, only for body
  values. Here we don't need `bind_quoted` at all — but we *do* need
  `Macro.escape` when the value is a complex term.
  """
  defmacro defcodes(pairs) do
    for {code, message} <- pairs do
      quote do
        def reason_for(unquote(code)), do: unquote(message)
      end
    end
  end

  @doc """
  Use case 3 — `bind_quoted` for body values inside a compile-time loop.

  `for {name, value} <- pairs, do: quote bind_quoted: [n: name, v: value] do ... end`
  generates one block per iteration, with `n` and `v` bound hygienically
  in the body. This is the common Phoenix/Ecto pattern when a macro
  iterates over schema fields.
  """
  defmacro defkv(pairs) do
    for {name, value} <- pairs do
      quote bind_quoted: [n: name, v: value] do
        # Each `n` here is a compile-time-known atom; `v` is the value.
        # We emit a getter per pair.
        def unquote(n)(), do: unquote(v)
      end
    end
  end
end
```

> Note: in `defkv`, we wrote `def unquote(n)()` — proving that even
> inside a `bind_quoted` block, you still use `unquote` to splice a
> bound name into *code structure* (here, a function name). The
> `bind_quoted` binding just guarantees the value of `n` is the same
> atom the macro expansion decided on, with no re-evaluation drama.

### Step 3: A consumer module for the generator macros

**Objective**: Provide A consumer module for the generator macros — these are the supporting fixtures the main module depends on to make its concept demonstrable.


```elixir
defmodule BindQuotedDemo.Codes do
  @moduledoc "Generated error-code lookup. Exists to exercise defcodes/1."

  require BindQuotedDemo

  BindQuotedDemo.defcodes(
    e404: "not found",
    e500: "server error",
    e429: "rate limited"
  )

  # Catch-all must be last to be overridable by generated heads above.
  def reason_for(_), do: "unknown"
end

defmodule BindQuotedDemo.Config do
  @moduledoc "Generated getters. Exists to exercise defkv/1."

  require BindQuotedDemo

  BindQuotedDemo.defkv(
    version: "1.0.0",
    service: :auth,
    max_retries: 3
  )
end
```

### Step 4: `test/bind_quoted_demo_test.exs`

**Objective**: Write `bind_quoted_demo_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule BindQuotedDemoTest do
  use ExUnit.Case, async: true
  import ExUnit.CaptureIO
  require BindQuotedDemo

  describe "log_twice/1 — evaluates once" do
    test "side effect fires only once" do
      {:ok, agent} = Agent.start_link(fn -> 0 end)

      side_effect = fn ->
        Agent.update(agent, &(&1 + 1))
        :payload
      end

      capture_io(fn -> BindQuotedDemo.log_twice(side_effect.()) end)

      assert Agent.get(agent, & &1) == 1
    end

    test "returns the value" do
      capture_io(fn ->
        assert BindQuotedDemo.log_twice(99) == 99
      end)
    end
  end

  describe "defcodes/1 — generated function heads" do
    test "each code resolves to its message" do
      assert BindQuotedDemo.Codes.reason_for(:e404) == "not found"
      assert BindQuotedDemo.Codes.reason_for(:e500) == "server error"
      assert BindQuotedDemo.Codes.reason_for(:e429) == "rate limited"
    end

    test "unknown codes fall through to the catch-all" do
      assert BindQuotedDemo.Codes.reason_for(:nope) == "unknown"
    end
  end

  describe "defkv/1 — generated getters with bind_quoted" do
    test "getters return the compile-time-bound values" do
      assert BindQuotedDemo.Config.version() == "1.0.0"
      assert BindQuotedDemo.Config.service() == :auth
      assert BindQuotedDemo.Config.max_retries() == 3
    end
  end
end
```

### Step 5: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
```

### Why this works

`bind_quoted: [k: expr]` evalúa `expr` en expand-time, escapa el
resultado con `Macro.escape/1`, y emite una asignación hygienic
`k = value` al tope del bloque. Las posiciones de pattern (dentro de
`def`, `case`, function-head args) siguen requiriendo `unquote(k)`
porque el compilador debe ver el AST del pattern.

---


## Deep Dive: State Management and Message Handling Patterns

Understanding state transitions is central to reliable OTP systems. Every `handle_call` or `handle_cast` receives current state and returns new state—immutability forces explicit reasoning. This prevents entire classes of bugs: missing state updates are immediately visible.

Key insight: separate pure logic (state → new state) from side effects (logging, external calls). Move pure logic to private helpers; use handlers for orchestration. This makes servers testable—test pure functions independently.

In production, monitor state size and mutation frequency. Unbounded growth is a memory leak; excessive mutations signal hot spots needing optimization. Always profile before reaching for performance solutions like ETS.

## Benchmark

```elixir
require BindQuotedDemo

{val, _} =
  :timer.tc(fn ->
    Enum.each(1..1_000_000, fn _ ->
      BindQuotedDemo.Config.version()
    end)
  end)

IO.puts("getter: #{val}µs / 1M calls")
```

Target esperado: ~0.1µs por call; función generada es indistinguible
de una función escrita a mano.

---

## Trade-offs and production gotchas

**1. `bind_quoted` values must be escapable**
Maps, lists, tuples, atoms, numbers, binaries — all fine. PIDs, refs,
ports, and functions — not fine. If the macro truly needs a
non-escapable value, it must arrive at runtime; `bind_quoted` can't
carry it.

**2. `bind_quoted` doesn't help inside pattern positions**
A pattern (function-head argument, `case` pattern, etc.) is compiled
before the `do:` body. `bind_quoted` binds body variables, not pattern
slots. Use `unquote` there — and use `Macro.escape/1` if you need to
embed a literal complex term.

**3. `unquote_splicing` pairs with `bind_quoted` awkwardly**
You cannot `unquote_splicing` inside a `bind_quoted` block. If you need
splicing, structure the macro as outer `for` + inner quote (as in
`defcodes/1` above), with each inner quote standalone.

**4. Pre-computing at expansion time is occasionally too early**
`bind_quoted` evaluates its bindings *during compilation*. If the value
depends on the environment at runtime (like a user request or a live
config reload), you don't want `bind_quoted` — you want the expression
to be carried as AST and evaluated later. This is rare but real.

**5. Readability varies by team**
Some Elixir teams consider `bind_quoted` the default, and reach for
plain `unquote` only when necessary. Others find `unquote` more
readable. Pick a team convention; don't mix them randomly in one file.

**6. When NOT to use `bind_quoted`**
When you're injecting a *code block* that should execute at the call
site (the `do:` of an `unless`, the body of a `with`, a lambda the user
passed). Those are code, not values — `unquote` is the right tool.

---

## Reflection

- El equipo mantiene un DSL con ~40 macros que mezclan `unquote` y
  `bind_quoted` según el gusto del autor. ¿Qué regla convertís en
  lint rule para que el estilo converja?
- `defkv` necesita un valor calculado a partir de otro ya bound
  (`full_name` = `first` <> `last`). ¿Lo hacés en el macro o en el
  body del getter? Analizá qué se fosiliza.

---

## Executable Example

Copy the code below into a file (e.g., `solution.exs`) and run with `elixir solution.exs`:

```elixir
defmodule Main do
  defmodule BindQuotedDemo do
    @moduledoc """
    Side-by-side patterns showing where `bind_quoted` is the right choice
    and where `unquote` remains necessary.
    """

    @doc """
    Use case 1 — shared sub-expression.

    `expr` is a runtime expression we want to log twice. With
    `bind_quoted`, the expression is evaluated once and bound to `value`,
    which is then referenced in both log lines.
    """
    defmacro log_twice(expr) do
      quote bind_quoted: [value: expr] do
        IO.puts("[1] " <> inspect(value))
        IO.puts("[2] " <> inspect(value))
        value
      end
    end

    @doc """
    Use case 2 — compile-time loop over a list, generating function heads.

    Each element `code` becomes a `def reason_for(code), do: "..."` pattern
    clause. Note: `unquote(code)` appears in the pattern position inside
    `def` — `bind_quoted` is not used for pattern positions, only for body
    values. Here we don't need `bind_quoted` at all — but we *do* need
    `Macro.escape` when the value is a complex term.
    """
    defmacro defcodes(pairs) do
      for {code, message} <- pairs do
        quote do
          def reason_for(unquote(code)), do: unquote(message)
        end
      end
    end

    @doc """
    Use case 3 — `bind_quoted` for body values inside a compile-time loop.

    `for {name, value} <- pairs, do: quote bind_quoted: [n: name, v: value] do ... end`
    generates one block per iteration, with `n` and `v` bound hygienically
    in the body. This is the common Phoenix/Ecto pattern when a macro
    iterates over schema fields.
    """
    defmacro defkv(pairs) do
      for {name, value} <- pairs do
        quote bind_quoted: [n: name, v: value] do
          # Each `n` here is a compile-time-known atom; `v` is the value.
          # We emit a getter per pair.
          def unquote(n)(), do: unquote(v)
        end
      end
    end
  end

  defmodule BindQuotedDemo.Codes do
    @moduledoc "Generated error-code lookup."
    require BindQuotedDemo

    BindQuotedDemo.defcodes(
      e404: "not found",
      e500: "server error",
      e429: "rate limited"
    )

    def reason_for(_), do: "unknown"
  end

  defmodule BindQuotedDemo.Config do
    @moduledoc "Generated getters."
    require BindQuotedDemo

    BindQuotedDemo.defkv(
      version: "1.0.0",
      service: :auth,
      max_retries: 3
    )
  end

  def main do
    require BindQuotedDemo
    import ExUnit.CaptureIO
  
    IO.puts("=== BindQuotedDemo ===\n")
  
    # Demo 1: log_twice with side-effect (should fire once)
    IO.puts("1. log_twice/1 (bind_quoted evaluates once):")
    {:ok, agent} = Agent.start_link(fn -> 0 end)
    counter_fn = fn ->
      Agent.update(agent, &(&1 + 1))
      42
    end
  
    output = capture_io(fn ->
      result = BindQuotedDemo.log_twice(counter_fn.())
      IO.puts("   Result: #{result}")
    end)
    IO.write(output)
    calls = Agent.get(agent, & &1)
    IO.puts("   Side-effects: #{calls} (expected 1)")
    assert calls == 1
  
    # Demo 2: Generated error code lookup
    IO.puts("\n2. defcodes/1 - Generated function heads:")
    IO.puts("   reason_for(:e404) = '#{BindQuotedDemo.Codes.reason_for(:e404)}'")
    assert BindQuotedDemo.Codes.reason_for(:e404) == "not found"
    IO.puts("   reason_for(:e500) = '#{BindQuotedDemo.Codes.reason_for(:e500)}'")
    assert BindQuotedDemo.Codes.reason_for(:e500) == "server error"
    IO.puts("   reason_for(:unknown) = '#{BindQuotedDemo.Codes.reason_for(:unknown)}'")
    assert BindQuotedDemo.Codes.reason_for(:unknown) == "unknown"
  
    # Demo 3: Generated key-value getters
    IO.puts("\n3. defkv/1 - Generated getters:")
    IO.puts("   version() = '#{BindQuotedDemo.Config.version()}'")
    assert BindQuotedDemo.Config.version() == "1.0.0"
    IO.puts("   service() = #{inspect(BindQuotedDemo.Config.service())}")
    assert BindQuotedDemo.Config.service() == :auth
    IO.puts("   max_retries() = #{BindQuotedDemo.Config.max_retries()}")
    assert BindQuotedDemo.Config.max_retries() == 3
  
    IO.puts("\n✓ All BindQuotedDemo demos passed!")
  end

end

Main.main()
```


## Resources

- [`Kernel.SpecialForms.quote/2` — `:bind_quoted` option](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#quote/2-binding-and-unquote-fragments)
- [`Macro.escape/1`](https://hexdocs.pm/elixir/Macro.html#escape/1)
- ["Metaprogramming Elixir" — Chris McCord](https://pragprog.com/titles/cmelixir/metaprogramming-elixir/) — extensive treatment of the pattern
- [`Ecto.Schema` source](https://github.com/elixir-ecto/ecto/blob/master/lib/ecto/schema.ex) — a large-scale production use of `bind_quoted` in a DSL
- [Sasa Juric — "Understanding Elixir macros"](https://www.theerlangelist.com/article/macros_1) — the whole series pays off when you re-read it after doing this exercise
