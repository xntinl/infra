# Advanced Kernel macros — defguard, defdelegate, then, tap

**Project**: `kernel_tools` — a module that puts `Kernel`'s less-obvious macros to work: `defguard` for reusable guards, `defdelegate` for thin wrappers, `then/2` and `tap/2` for pipeline ergonomics.

---

## Why advanced Kernel macros matter

Most Elixir programmers use `Kernel` daily without noticing — it's the
auto-imported module that gives you `def`, `|>`, `if`, `case`, and the
arithmetic operators. But `Kernel` also ships with a dozen macros that solve
recurring design problems very cleanly, and that most introductory material
ignores.

This exercise is a guided tour of the four most useful ones:

- `defguard/1` — define a **named guard** you can reuse in function heads.
- `defdelegate/2` — forward calls to another module without writing a wrapper
  by hand.
- `then/2` — inject a one-off function into a pipeline.
- `tap/2` — side-effect into a pipeline without breaking the data flow.

They're not flashy, but they remove a surprising amount of boilerplate.

---

## The business problem

Teams accumulate boilerplate: duplicated guard expressions across function
heads, manual wrapper functions that only forward arguments, anonymous
functions inside pipelines that exist only to reorder arguments, and `Logger`
calls that break the pipe because they return `:ok`. Each symptom is minor,
but in a large codebase they add up to thousands of lines of noise that
obscure intent.

`kernel_tools` demonstrates how the four underused `Kernel` macros remove
exactly this category of noise — without adding dependencies or abstraction
layers.

---

## Project structure

```
kernel_tools/
├── lib/
│   ├── kernel_tools.ex
│   └── kernel_tools/math.ex
├── script/
│   └── main.exs
├── test/
│   └── kernel_tools_test.exs
└── mix.exs
```

---

## Design decisions

**Option A — Helpers como funciones planas**
- Pros: Dialyzer-friendly; sin superficie de macro.
- Cons: `even?/1` no sirve en `when`; pipelines pierden ergonomía.

**Option B — `defguard` + `defdelegate` + `tap` + `then` juntos** (elegida)
- Pros: Primitivas stdlib que componen.
- Cons: Cuatro mecánicas distintas.

→ Elegida **B** porque este ejercicio *es* el tour. Cada una resuelve un
problema recurrente que antes requería boilerplate o dependencia. Vienen en
stdlib porque ganan en cada proyecto — sin version drift, sin gap de dialyzer.

---

## Implementation

### `mix.exs`

```elixir
defmodule KernelTools.MixProject do
  use Mix.Project

  def project do
    [
      app: :kernel_tools,
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

### `lib/kernel_tools/math.ex`

```elixir
defmodule KernelTools.Math do
  @moduledoc "Low-level numeric helpers. `KernelTools` re-exports these."

  @doc "Doubles a number."
  @spec double(number()) :: number()
  def double(n), do: n * 2

  @doc "Halves an even integer."
  @spec halve(integer()) :: integer()
  def halve(n), do: div(n, 2)
end
```

### `lib/kernel_tools.ex`

```elixir
defmodule KernelTools do
  @moduledoc """
  A facade that demonstrates four underused `Kernel` macros:
  `defguard`, `defdelegate`, `then`, and `tap`.

  The module both *uses* them (in the pipeline helpers) and *exposes* them
  (via delegation) so the test suite can exercise each one directly.
  """

  # ── Reusable guards via `defguard` ─────────────────────────────────────

  @doc """
  Guard-safe check for even integers.

  Because it's a `defguard`, it's legal in `when` clauses *and* as a
  regular boolean expression.
  """
  defguard is_even(n) when is_integer(n) and rem(n, 2) == 0

  @doc "Guard-safe check for a positive integer."
  defguard is_positive_int(n) when is_integer(n) and n > 0

  @doc """
  Halves `n`, but only if it's an even integer. Uses the reusable guard.
  """
  @spec safe_halve(integer()) :: {:ok, integer()} | {:error, :not_even}
  def safe_halve(n) when is_even(n), do: {:ok, div(n, 2)}
  def safe_halve(_), do: {:error, :not_even}

  # ── Thin re-exports via `defdelegate` ──────────────────────────────────

  @doc "Delegates to `KernelTools.Math.double/1`."
  defdelegate double(n), to: KernelTools.Math

  @doc "Delegates to `KernelTools.Math.halve/1`."
  defdelegate halve(n), to: KernelTools.Math

  # ── Pipeline ergonomics with `then` and `tap` ──────────────────────────

  @doc """
  Applies a sequence of transformations and logs the intermediate value,
  without breaking the pipe.
  """
  @spec shout(String.t()) :: String.t()
  def shout(text) do
    text
    |> String.trim()
    |> tap(&send_log/1)
    |> then(fn s -> String.replace(s, " ", "_") end)
    |> String.upcase()
  end

  defp send_log(value), do: IO.puts("[shout] trimmed = #{inspect(value)}")
end
```

### `test/kernel_tools_test.exs`

```elixir
defmodule KernelToolsTest do
  use ExUnit.Case, async: true
  doctest KernelTools
  import ExUnit.CaptureIO
  require KernelTools

  describe "defguard is_even/1" do
    test "works in a guard clause" do
      assert KernelTools.safe_halve(10) == {:ok, 5}
      assert KernelTools.safe_halve(7) == {:error, :not_even}
      assert KernelTools.safe_halve(:nope) == {:error, :not_even}
    end

    test "can be used as a regular boolean outside a guard" do
      assert KernelTools.is_even(4)
      refute KernelTools.is_even(5)
      refute KernelTools.is_even("4")
    end
  end

  describe "defdelegate" do
    test "double/1 forwards to KernelTools.Math" do
      assert KernelTools.double(21) == 42
    end

    test "halve/1 forwards to KernelTools.Math" do
      assert KernelTools.halve(10) == 5
    end
  end

  describe "shout/1 — then/2 and tap/2" do
    test "upcases, underscores spaces, trims whitespace" do
      output = capture_io(fn -> assert KernelTools.shout("  hi there ") == "HI_THERE" end)
      assert output =~ "trimmed = \"hi there\""
    end

    test "tap does not alter the piped value" do
      assert KernelTools.shout("abc") == "ABC"
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== KernelTools Demo ===\n")

    IO.puts("1. is_even(4): #{KernelTools.is_even(4)}")
    IO.puts("   is_even(5): #{KernelTools.is_even(5)}")

    IO.puts("\n2. safe_halve(10): #{inspect(KernelTools.safe_halve(10))}")
    IO.puts("   safe_halve(7): #{inspect(KernelTools.safe_halve(7))}")

    IO.puts("\n3. double(21): #{KernelTools.double(21)}")
    IO.puts("   halve(10): #{KernelTools.halve(10)}")

    IO.puts("\n4. shout('  hi there '):")
    result = KernelTools.shout("  hi there ")
    IO.puts("   Result: #{result}")

    IO.puts("\n=== Done ===")
  end
end

Main.main()
```

Run with: `elixir script/main.exs`

---

## Key concepts

### 1. `defguard/1` — reusable guard expressions

Guards (`when` clauses) only accept a small subset of Elixir: arithmetic,
comparison, type tests, and a fixed list of BIFs. You can't call regular
functions. `defguard` lets you give a **name** to a guard expression so you
don't duplicate it across twenty function heads. `defguard` is itself a macro
— it expands into an ordinary macro that only emits AST allowed in guards.

### 2. `defdelegate/2` — forward without boilerplate

`defdelegate foo(x), to: Bar` generates `def foo(x), do: Bar.foo(x)`. Use it
when the wrapper adds **nothing** — not even argument reordering. The moment
you need to transform inputs, write a real function.

### 3. `then/2` — inject any function into a pipe

`|>` passes the left side as the **first** argument. `then/2` fixes the case
when the relevant argument is second or when the call shape is a fun. Before
`then/2` (Elixir 1.12) people reached for anonymous-function pipelines with
`&`, which worked but looked noisy.

### 4. `tap/2` — side effect without losing the value

`tap` calls a function *for its side effect* and returns the original value,
untouched. Perfect for logging inside a pipeline without breaking the data
flow.

### 5. Production gotchas

- `defguard` bodies must be guard-legal — no arbitrary function calls.
- `defdelegate` with `:as` hides a rename; prefer a one-line function with a comment.
- `then/2` allocates an anonymous function — fine for application code, inline in hot loops.
- `tap/2` is for side effects only; if you return a changed value, use `then/2`.

---

## Resources

- [`Kernel.defguard/1`](https://hexdocs.pm/elixir/Kernel.html#defguard/1)
- [`Kernel.defdelegate/2`](https://hexdocs.pm/elixir/Kernel.html#defdelegate/2)
- [`Kernel.then/2`](https://hexdocs.pm/elixir/Kernel.html#then/2) and [`Kernel.tap/2`](https://hexdocs.pm/elixir/Kernel.html#tap/2)
- ["Patterns and guards" — Elixir guide](https://hexdocs.pm/elixir/patterns-and-guards.html)
- [Elixir 1.12 release notes](https://elixir-lang.org/blog/2021/05/19/elixir-v1-12-0-released/)
