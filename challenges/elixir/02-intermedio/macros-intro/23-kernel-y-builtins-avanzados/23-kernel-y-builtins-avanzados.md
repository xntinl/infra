# Advanced Kernel macros — defguard, defdelegate, then, tap

**Project**: `kernel_tools` — a module that puts `Kernel`'s less-obvious macros to work: `defguard` for reusable guards, `defdelegate` for thin wrappers, `then/2` and `tap/2` for pipeline ergonomics.

---

## Project context

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

Project structure:

```
kernel_tools/
├── lib/
│   ├── kernel_tools.ex
│   └── kernel_tools/math.ex
├── test/
│   └── kernel_tools_test.exs
└── mix.exs
```

---

## Why these four Kernel macros and not a library

Cada uno resuelve un problema recurrente que antes requería
boilerplate o dependencia. Vienen en stdlib porque ganan en cada
proyecto — sin version drift, sin gap de dialyzer.

---

## Core concepts

### 1. `defguard/1` — reusable guard expressions

Guards (`when` clauses) only accept a small subset of Elixir: arithmetic,
comparison, type tests, and a fixed list of BIFs. You can't call regular
functions. `defguard` lets you give a **name** to a guard expression so you
don't duplicate it across twenty function heads:

```elixir
defguard is_even(n) when is_integer(n) and rem(n, 2) == 0

def halve(n) when is_even(n), do: div(n, 2)
```

`defguard` is itself a macro — it expands into an ordinary macro that only
emits AST allowed in guards. It pairs naturally with
`Macro.Env.in_guard?/1` for guards that need to behave differently
inside vs. outside `when`.

### 2. `defdelegate/2` — forward without boilerplate

`defdelegate foo(x), to: Bar` generates `def foo(x), do: Bar.foo(x)`. It's
trivial, but it makes **facade modules** (re-exporting a curated API)
almost free to maintain, and the docs and `@spec` live on the target.

Use it when the wrapper adds **nothing** — not even argument reordering.
The moment you need to transform inputs, write a real function.

### 3. `then/2` — inject any function into a pipe

`|>` passes the left side as the **first** argument of the right-side call.
That's fine 90% of the time, but sometimes you want to call a function
whose relevant argument is second, or whose "call shape" is a fun. `then/2`
fixes that:

```elixir
value
|> do_something()
|> then(fn x -> SomeModule.other(other_arg, x) end)
|> finish()
```

Before `then/2` existed (Elixir 1.12) people reached for anonymous-function
pipelines with `&`, which worked but looked noisy.

### 4. `tap/2` — side effect without losing the value

`tap` calls a function *for its side effect* and returns the original
value, untouched. Perfect for logging inside a pipeline:

```elixir
user
|> fetch_profile()
|> tap(&Logger.debug("profile: #{inspect(&1)}"))
|> render()
```

Without `tap`, you'd either break the pipe or wrap the log in an
anonymous function that accidentally changes the value.

---

## Design decisions

**Option A — Helpers como funciones planas**
- Pros: Dialyzer-friendly; sin superficie de macro.
- Cons: `even?/1` no sirve en `when`; pipelines pierden ergonomía.

**Option B — `defguard` + `defdelegate` + `tap` + `then` juntos** (elegida)
- Pros: Primitivas stdlib que componen.
- Cons: Cuatro mecánicas distintas.

→ Elegida **B** porque este ejercicio *es* el tour.

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
mix new kernel_tools
cd kernel_tools
```

### Step 2: `lib/kernel_tools/math.ex` — the delegate target

**Objective**: Edit `math.ex` — the delegate target, exposing AST manipulation that runs at compile time — making the macro's hygiene and unquoting choices observable.


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

### Step 3: `lib/kernel_tools.ex`

**Objective**: Implement `kernel_tools.ex` — AST manipulation that runs at compile time — making the macro's hygiene and unquoting choices observable.


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

  Illustrates how `defguard` lets a single predicate drive both the
  dispatch (`when`) and a clean error clause.
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

  `tap/2` is used for the logging side-effect (value is untouched).
  `then/2` is used to call `String.replace/3` — whose "payload" argument
  is *first* but whose shape doesn't fit the implicit pipe because we also
  want to uppercase and trim around it.
  """
  @spec shout(String.t()) :: String.t()
  def shout(text) do
    text
    |> String.trim()
    |> tap(&send_log/1)
    |> then(fn s -> String.replace(s, " ", "_") end)
    |> String.upcase()
  end

  # Kept private to prove the side-effect path is exercised in tests via
  # `Process.group_leader/0` + CaptureIO, not via the return value.
  defp send_log(value), do: IO.puts("[shout] trimmed = #{inspect(value)}")
end
```

### Step 4: `test/kernel_tools_test.exs`

**Objective**: Write `kernel_tools_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule KernelToolsTest do
  use ExUnit.Case, async: true
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
      # tap preserves the value; the log line is a side effect.
      assert output =~ "trimmed = \"hi there\""
    end

    test "tap does not alter the piped value" do
      # If tap leaked the log's return value (:ok), upcase would fail.
      assert KernelTools.shout("abc") == "ABC"
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

`defguard` expande en compile-time a un macro cuyo body es AST
guard-legal — así que `is_even(n)` inlinado en `when` es idéntico a
`is_integer(n) and rem(n, 2) == 0`. `defdelegate` reescribe
`def foo(x), do: Target.foo(x)` en compile time. `tap/2` devuelve su
primer argumento intacto. `then/2` aplica una función al valor pipeado
cuando la forma no encaja con primer-argumento de `|>`. Los cuatro
expanden a código directo.

---


## Deep Dive: State Management and Message Handling Patterns

Understanding state transitions is central to reliable OTP systems. Every `handle_call` or `handle_cast` receives current state and returns new state—immutability forces explicit reasoning. This prevents entire classes of bugs: missing state updates are immediately visible.

Key insight: separate pure logic (state → new state) from side effects (logging, external calls). Move pure logic to private helpers; use handlers for orchestration. This makes servers testable—test pure functions independently.

In production, monitor state size and mutation frequency. Unbounded growth is a memory leak; excessive mutations signal hot spots needing optimization. Always profile before reaching for performance solutions like ETS.

## Benchmark

<!-- benchmark N/A: las cuatro macros expanden a código equivalente al
manual. Microbenchmarks miden el cuerpo de la función, no la macro. -->

---

## Trade-offs and production gotchas

**1. `defguard` is a macro — it expands, it doesn't call**
The body you pass must be legal inside a guard. You can't call an arbitrary
function, even if that function "could" be pure. When you need more, use
a regular function and call it *before* the guard, or redesign so the guard
can do its job with the allowed primitives.

**2. `defdelegate` discards private context**
Delegation is a compile-time rewrite: `defdelegate foo/1, to: Bar` becomes
`def foo(x), do: Bar.foo(x)`. That means the caller's `@spec`, `@doc`,
and `@impl` live on the delegating module, but the *implementation* lives
on the target. If you care about dialyzer, declare the `@spec` on both
sides or skip delegation.

**3. `defdelegate` with `:as` is a trap for readers**
`defdelegate foo(x), to: Bar, as: :bar` means "call `Bar.bar/1` when the
user writes `foo/1`." It compiles fine and looks innocent, but it hides a
rename. Prefer writing a one-line function with a comment.

**4. `then/2` is cheap but not free**
Each `then/2` allocates an anonymous function. In a hot loop you may want
to inline the call. For ordinary application code, the clarity is worth it.

**5. `tap/2` is for side effects, not transformations**
If you find yourself assigning inside a `tap` or returning a changed value,
you probably want `then/2` or a named function. Reading code that uses
`tap` for side effects only is cognitively cheap — subverting that is a
footgun for future readers.

**6. When NOT to use these**
Reach for `defguard` only when a predicate is used in **multiple** `when`
clauses — otherwise it's overengineering. Reach for `defdelegate` only
when the wrapper truly adds nothing. And don't use `then/2` for every
minor pipeline — sometimes a temporary variable is the right answer.

---

## Reflection

- Tu módulo delega 12 funciones con `defdelegate`. Dialyzer se queja
  porque los `@spec` viven solo en el delegador. ¿Duplicás, movés o
  abandonás `defdelegate`?
- Un pipeline tiene tres `tap(&Logger.debug/1)` consecutivos. Un
  compañero propone un `trace` macro. ¿Cuándo vale la abstracción y
  cuándo el `tap` explícito sigue mejor?

---

## Executable Example

Copy the code below into a file (e.g., `solution.exs`) and run with `elixir solution.exs`:

```elixir
defmodule Main do
  defmodule KernelTools.Math do
    @moduledoc "Low-level numeric helpers. `KernelTools` re-exports these."

    @doc "Doubles a number."
    @spec double(number()) :: number()
    def double(n), do: n * 2

    @doc "Halves an even integer."
    @spec halve(integer()) :: integer()
    def halve(n), do: div(n, 2)
  end

  def main do
    require KernelTools
    import ExUnit.CaptureIO
  
    IO.puts("=== KernelTools Demo ===\n")
  
    # Demo 1: defguard is_even/1
    IO.puts("1. is_even(4): #{KernelTools.is_even(4)}")
    assert KernelTools.is_even(4) == true
    IO.puts("   is_even(5): #{KernelTools.is_even(5)}")
    assert KernelTools.is_even(5) == false
  
    # Demo 2: Guard usage in function
    IO.puts("\n2. safe_halve(10): #{inspect(KernelTools.safe_halve(10))}")
    assert KernelTools.safe_halve(10) == {:ok, 5}
    IO.puts("   safe_halve(7): #{inspect(KernelTools.safe_halve(7))}")
    assert KernelTools.safe_halve(7) == {:error, :not_even}
  
    # Demo 3: defdelegate
    IO.puts("\n3. double(21): #{KernelTools.double(21)}")
    assert KernelTools.double(21) == 42
    IO.puts("   halve(10): #{KernelTools.halve(10)}")
    assert KernelTools.halve(10) == 5
  
    # Demo 4: then/2 and tap/2
    IO.puts("\n4. shout('  hi there '):")
    output = capture_io(fn ->
      result = KernelTools.shout("  hi there ")
      IO.puts("   Result: #{result}")
      assert result == "HI_THERE"
    end)
    IO.write(output)
  
    IO.puts("\n✓ All KernelTools demos completed!")
  end

end

Main.main()
```


## Resources

- [`Kernel.defguard/1`](https://hexdocs.pm/elixir/Kernel.html#defguard/1)
- [`Kernel.defdelegate/2`](https://hexdocs.pm/elixir/Kernel.html#defdelegate/2)
- [`Kernel.then/2`](https://hexdocs.pm/elixir/Kernel.html#then/2) and [`Kernel.tap/2`](https://hexdocs.pm/elixir/Kernel.html#tap/2)
- ["Patterns and guards" — Elixir guide](https://hexdocs.pm/elixir/patterns-and-guards.html)
- [Elixir 1.12 release notes](https://elixir-lang.org/blog/2021/05/19/elixir-v1-12-0-released/) — introduced `then/2` and `tap/2`
- ["Metaprogramming Elixir" — Chris McCord](https://pragprog.com/titles/cmelixir/metaprogramming-elixir/) — chapter on compile-time code generation
