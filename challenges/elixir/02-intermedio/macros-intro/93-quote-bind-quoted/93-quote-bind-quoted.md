# quote bind_quoted vs unquote — when to prefer which

**Project**: `bind_quoted_demo` — a deeper look at `quote bind_quoted: [...]`, comparing it head-to-head with inline `unquote/1`, and showing the three canonical use cases where `bind_quoted` is strictly better.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Project context

Exercise 92 introduced `bind_quoted` as the fix for accidental
duplicate evaluation. This exercise zooms in and shows **three distinct
situations** where `bind_quoted` matters:

1. **Loops at compile time** — generating N function heads from a list,
   where every head embeds a value. `unquote` inside `for` interacts
   poorly with the macro expansion; `bind_quoted` handles it
   transparently.
2. **Shared sub-expressions** — the side-effect-once pattern from
   exercise 92.
3. **Accidental variable capture** — avoiding a hygiene pitfall where
   `unquote` of an expression referencing caller variables propagates
   surprising bindings.

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

## Implementation

### Step 1: Create the project

```bash
mix new bind_quoted_demo
cd bind_quoted_demo
```

### Step 2: `lib/bind_quoted_demo.ex`

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

```bash
mix test
```

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

## Resources

- [`Kernel.SpecialForms.quote/2` — `:bind_quoted` option](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#quote/2-binding-and-unquote-fragments)
- [`Macro.escape/1`](https://hexdocs.pm/elixir/Macro.html#escape/1)
- ["Metaprogramming Elixir" — Chris McCord](https://pragprog.com/titles/cmelixir/metaprogramming-elixir/) — extensive treatment of the pattern
- [`Ecto.Schema` source](https://github.com/elixir-ecto/ecto/blob/master/lib/ecto/schema.ex) — a large-scale production use of `bind_quoted` in a DSL
- [Sasa Juric — "Understanding Elixir macros"](https://www.theerlangelist.com/article/macros_1) — the whole series pays off when you re-read it after doing this exercise
