# Safe injection — bind_quoted vs unquote and hygiene pitfalls

**Project**: `safe_inject` — contrast two ways of threading runtime values into a `quote` block: inline `unquote/1` vs `quote bind_quoted: [...]`, and show concretely why the second form is safer.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Project context

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

Project structure:

```
safe_inject/
├── lib/
│   └── safe_inject.ex
├── test/
│   └── safe_inject_test.exs
└── mix.exs
```

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

## Implementation

### Step 1: Create the project

```bash
mix new safe_inject
cd safe_inject
```

### Step 2: `lib/safe_inject.ex`

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

```elixir
defmodule SafeInjectTest do
  use ExUnit.Case, async: true
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

## Resources

- [`Kernel.SpecialForms.quote/2` — `:bind_quoted` option](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#quote/2-binding-and-unquote-fragments)
- [`Macro.escape/1`](https://hexdocs.pm/elixir/Macro.html#escape/1)
- ["Metaprogramming Elixir" — Chris McCord](https://pragprog.com/titles/cmelixir/metaprogramming-elixir/) — chapter on macro hygiene, bind_quoted
- [Sasa Juric — "Understanding Elixir macros, part 3: macro tricks"](https://www.theerlangelist.com/article/macros_3)
- [`Macro.expand/2`](https://hexdocs.pm/elixir/Macro.html#expand/2) — the tool for proving duplication by inspection
