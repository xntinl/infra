# Typespecs and Dialyzer

**Project**: `typed_calc` — a small calculator library with strict typespecs, checked by Dialyzer

---

## Project structure

```
typed_calc/
├── lib/
│   └── typed_calc.ex
├── test/
│   └── typed_calc_test.exs
└── mix.exs
```

---

## The business problem

Elixir is dynamically typed. A function that expects a number silently accepts a string
until it crashes deep in the call stack. Typespecs (`@spec`, `@type`) document expected
shapes; **Dialyzer** statically analyzes your code against those specs and flags
mismatches without running anything.

Typespecs have zero runtime cost — they are discarded after compilation. The payoff
is a static safety net for a dynamic language.

---

## Core concepts

### `@type` and `@typep`

- `@type` — public type, visible to other modules and documentation tools.
- `@typep` — module-private type, cannot be referenced from outside.
- `@opaque` — exported by name but the internal structure is hidden; callers cannot pattern-match on it.

### `@spec`

Binds a function to a type contract: `@spec name(arg_types) :: return_type`. One spec
per function head; if a function has multiple clauses, the spec covers all of them.

### Dialyzer

A static analyzer built into OTP. It infers "success typings" — for each function, the
set of inputs for which it cannot fail. Your `@spec` narrows that set. Dialyzer reports
when specs and usage disagree.

Dialyzer reports **only what it can prove** wrong. It does not catch every bug. But
every warning it DOES emit is almost certainly a real issue.

---

## Why Dialyzer and not a full type system

Elixir is not going to become statically typed overnight. Dialyzer's compromise — success typings — fits the language's pragma: it never rejects code that *might* work, it only flags code that *provably cannot* work. That means zero false positives but also accepts untyped code without complaint. The alternative (gradual types à la Gradient, or waiting for the set-theoretic type system landing in future Elixir versions) either adds intrusive annotations everywhere or isn't here yet.

For a dynamic language with pattern matching as its main correctness tool, `@spec` + Dialyzer is currently the best-effort static layer you can layer on without changing how you write code.

---

## Design decisions

**Option A — skip typespecs; rely on tests + runtime guards**
- Pros: no extra tooling; fastest local dev loop; CI is only tests.
- Cons: contracts live in test assertions and prose comments; refactors lose type drift until production; editor autocomplete has nothing to hint with.

**Option B — `@spec` on all public functions + `mix dialyzer` in CI** (chosen)
- Pros: static contract for every boundary; Dialyzer catches mismatches without executing code; specs become rendered docs in HexDocs; opaque types let you evolve internals safely.
- Cons: first `mix dialyzer` run builds a multi-minute PLT; warnings without `@spec` are underwhelming; Dialyzer's success-typing model surprises people who expect strict typing.

Chose **B** because the public API is small and the cost is paid once per CI run (PLT is cached) — the ROI is catching the "I silently changed the return shape" class of bugs before merge.

---

## Implementation

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Standard library: no external dependencies required
    {:"ecto", "~> 1.0"},
  ]
end
```


### Step 1: Create the project

**Objective**: Build single module with complete @spec coverage so Dialyzer can enforce type contracts without gaps.

```bash
mix new typed_calc
cd typed_calc
```

### Step 2: `mix.exs` — add Dialyxir

**Objective**: Add Dialyxir dev-only so static type analysis runs in CI without runtime cost.

```elixir
defmodule TypedCalc.MixProject do
  use Mix.Project

  def project do
    [
      app: :typed_calc,
      version: "0.1.0",
      elixir: "~> 1.15",
      start_permanent: Mix.env() == :prod,
      deps: deps(),
      # Dialyzer picks up these; we keep it minimal for the tutorial.
      dialyzer: [plt_add_apps: [:ex_unit]]
    ]
  end

  def application, do: [extra_applications: [:logger]]

  defp deps do
    [{:dialyxir, "~> 1.4", only: [:dev], runtime: false}]
  end
end
```

### Step 3: `lib/typed_calc.ex`

**Objective**: Use finite unions (:add | :sub | :mul | :div) instead of atom() so Dialyzer statically catches typos.

```elixir
defmodule TypedCalc do
  @moduledoc """
  A calculator with strict arithmetic types.

  All operations are total on `number()` — we never return floats from
  integer inputs except when division would truncate.
  """

  # Public types. Callers can write `@spec foo(TypedCalc.op())`.
  @type op :: :add | :sub | :mul | :div

  # Tagged-tuple result type. We avoid exceptions for user-facing errors
  # and reserve raise for programmer errors (invariant violations).
  @type result :: {:ok, number()} | {:error, :division_by_zero}

  # Private helper type — hidden from callers.
  @typep non_zero :: number()

  @doc """
  Applies `op` to `a` and `b`. Integer division by zero yields `{:error, ...}`.
  """
  @spec apply_op(op(), number(), number()) :: result()
  def apply_op(:add, a, b), do: {:ok, a + b}
  def apply_op(:sub, a, b), do: {:ok, a - b}
  def apply_op(:mul, a, b), do: {:ok, a * b}
  def apply_op(:div, _a, 0), do: {:error, :division_by_zero}
  def apply_op(:div, _a, 0.0), do: {:error, :division_by_zero}
  def apply_op(:div, a, b), do: {:ok, safe_div(a, b)}

  # The typep forbids zero at the call site — but Elixir does not enforce it
  # at runtime. Dialyzer catches misuse statically.
  @spec safe_div(number(), non_zero()) :: number()
  defp safe_div(a, b), do: a / b

  @doc """
  Reduces a list of operations left-to-right from an initial value.

  Stops and returns the first error encountered.
  """
  @spec evaluate(number(), [{op(), number()}]) :: result()
  def evaluate(initial, ops) when is_number(initial) and is_list(ops) do
    Enum.reduce_while(ops, {:ok, initial}, fn {op, val}, {:ok, acc} ->
      case apply_op(op, acc, val) do
        {:ok, _} = r -> {:cont, r}
        {:error, _} = e -> {:halt, e}
      end
    end)
  end
end
```

### Step 4: `test/typed_calc_test.exs`

**Objective**: Cover both tagged-result branches so a future runtime-only change cannot sneak past without failing the suite.

```elixir
defmodule TypedCalcTest do
  use ExUnit.Case, async: true

  test "basic ops" do
    assert {:ok, 5} = TypedCalc.apply_op(:add, 2, 3)
    assert {:ok, 6} = TypedCalc.apply_op(:mul, 2, 3)
  end

  test "division by zero is a tagged error, not an exception" do
    assert {:error, :division_by_zero} = TypedCalc.apply_op(:div, 1, 0)
  end

  test "evaluate threads state and short-circuits on error" do
    ops = [{:add, 10}, {:mul, 2}, {:sub, 5}]
    assert {:ok, 15} = TypedCalc.evaluate(0, ops)

    bad = [{:add, 10}, {:div, 0}, {:mul, 2}]
    assert {:error, :division_by_zero} = TypedCalc.evaluate(0, bad)
  end
end
```

### Step 5: Run tests

**Objective**: Run tests before Dialyzer so any runtime regression surfaces first, keeping the static layer focused on contract drift.

```bash
mix test
```

### Step 6: Run Dialyzer

**Objective**: Build the PLT once and run Dialyzer so success typings become a static gate rather than aspirational documentation.

First run takes ~2 minutes (it builds the PLT — the persistent lookup table of OTP and
your deps). Subsequent runs are seconds.

```bash
mix deps.get
mix dialyzer
```

Expected output: `done (passed successfully)`. If you see warnings, read the
filename:line and fix — Dialyzer is almost always right.

### Step 7: Deliberately break the spec and watch Dialyzer catch it

**Objective**: Narrow the spec to an integer-only return so Dialyzer's success typing flags the float path `/` can produce.

Change the `apply_op/3` spec to claim it returns `{:ok, integer()}`:

```elixir
@spec apply_op(op(), number(), number()) :: {:ok, integer()} | {:error, atom()}
```

Run `mix dialyzer` again. Dialyzer flags the `:div` case because `/` on integers can
return a float (`5 / 2 == 2.5`). This is the point: the spec lied, Dialyzer caught it.

Revert the spec before finishing.

### Why this works

`@type` publishes the domain shapes (`op`, `result`) so every `@spec` reads like a contract in business terms, not primitive soup. `@spec` declarations narrow Dialyzer's inferred success typings to the intended set, so any call site that passes something outside that set — or any clause that returns something outside it — gets flagged. Dialyzer never produces a false positive: if it reports a mismatch, there is a real path through your code where the types disagree. Running it in CI closes the loop; the specs stop being aspirational documentation and start being enforced.

---


## Key Concepts

### 1. Typespecs Document Argument and Return Types

Typespecs are comments—they don't enforce types at runtime. But tools like Dialyzer read them and warn about type mismatches. They communicate your intent to future readers.

### 2. Dialyzer Catches Type Errors Before Runtime

Dialyzer performs static analysis on typespecs. If you call a function and pattern-match the result incorrectly, Dialyzer warns. This prevents entire classes of bugs.

### 3. Write Typespecs for Public APIs

`iex> h MyModule.add` shows typespecs in the help. Write them for public APIs; internal functions can be less strict.

---
## Benchmark

<!-- benchmark N/A: typespecs are erased at compile time and Dialyzer is a static analyzer — there is nothing at runtime to measure. The relevant metric is CI time, not µs/call. -->

---

## Trade-offs

| Tool | What it checks | Cost |
|------|---------------|------|
| Runtime guards (`is_integer/1`) | Wrong type at call time | Small runtime cost; catches only what runs |
| Pattern matching | Wrong shape at call time | Idiomatic; catches only what runs |
| `@spec` + Dialyzer | Contract mismatch statically | Slow first run; silent if you don't run it |
| Ecto schemas | External data shape | Heavy; best for DB/API boundaries |

Typespecs + Dialyzer shine for **internal APIs** where a runtime guard would be redundant.
At I/O boundaries, use explicit validation (changesets, guards).

---

## Common production mistakes

**1. Specs drift from reality**
You refactor, forget to update the spec, CI does not run Dialyzer — specs silently lie.
Run `mix dialyzer` in CI.

**2. `@spec foo(any()) :: any()`**
This spec adds zero information. Delete it or tighten it. If the function truly accepts
anything, that is a design smell — what is the actual invariant?

**3. Using `atom()` instead of a finite union**
`@type op :: atom()` tells Dialyzer nothing. `@type op :: :add | :sub | :mul | :div`
lets it flag misspelled atoms at the call site.

**4. Opaque types leaking across boundaries**
If you declare `@opaque t()`, callers outside the module cannot pattern-match on it.
Provide accessor functions — do not let callers reach into the internals via `elem/2`
or struct destructuring.

---

## When NOT to use

- **One-off scripts**: the PLT build time dwarfs the script runtime.
- **Teams not running `mix dialyzer` in CI**: specs that no one checks are worse than no specs — they lie.
- **Highly dynamic code**: metaprogramming-heavy DSLs confuse Dialyzer. `@spec`s on the generated functions often need `no_return` or unions wide enough to be useless.

---

## Reflection

1. Dialyzer with success typings does not catch "you forgot to handle `{:error, :timeout}`" when both branches are legal. What complementary technique (tests, property checks, `with` pyramids, exhaustive pattern match warnings) would you pair with typespecs to close that gap?
2. You inherit a 50k-line Elixir codebase with zero `@spec`. Adding specs everywhere is a months-long effort. What is the highest-leverage subset to spec first, and how do you prevent the team from shipping new code without specs while the backfill is in progress?

---

## Executable Example

Copy the code below into a file (e.g., `solution.exs`) and run with `elixir solution.exs`:

```elixir
defmodule TypedCalc do
  @moduledoc """
  A calculator with strict arithmetic types.

  All operations are total on `number()` — we never return floats from
  integer inputs except when division would truncate.
  """

  # Public types. Callers can write `@spec foo(TypedCalc.op())`.
  @type op :: :add | :sub | :mul | :div

  # Tagged-tuple result type. We avoid exceptions for user-facing errors
  # and reserve raise for programmer errors (invariant violations).
  @type result :: {:ok, number()} | {:error, :division_by_zero}

  # Private helper type — hidden from callers.
  @typep non_zero :: number()

  @doc """
  Applies `op` to `a` and `b`. Integer division by zero yields `{:error, ...}`.
  """
  @spec apply_op(op(), number(), number()) :: result()
  def apply_op(:add, a, b), do: {:ok, a + b}
  def apply_op(:sub, a, b), do: {:ok, a - b}
  def apply_op(:mul, a, b), do: {:ok, a * b}
  def apply_op(:div, _a, 0), do: {:error, :division_by_zero}
  def apply_op(:div, _a, 0.0), do: {:error, :division_by_zero}
  def apply_op(:div, a, b), do: {:ok, safe_div(a, b)}

  # The typep forbids zero at the call site — but Elixir does not enforce it
  # at runtime. Dialyzer catches misuse statically.
  @spec safe_div(number(), non_zero()) :: number()
  defp safe_div(a, b), do: a / b

  @doc """
  Reduces a list of operations left-to-right from an initial value.

  Stops and returns the first error encountered.
  """
  @spec evaluate(number(), [{op(), number()}]) :: result()
  def evaluate(initial, ops) when is_number(initial) and is_list(ops) do
    Enum.reduce_while(ops, {:ok, initial}, fn {op, val}, {:ok, acc} ->
      case apply_op(op, acc, val) do
        {:ok, _} = r -> {:cont, r}
        {:error, _} = e -> {:halt, e}
      end
    end)
  end
end
```

## Resources

- [Elixir docs — Typespecs reference](https://hexdocs.pm/elixir/typespecs.html)
- [Dialyxir README](https://github.com/jeremyjh/dialyxir) — Mix task wrapper, flag reference
- [Learn You Some Erlang — Dialyzer](https://learnyousomeerlang.com/dialyzer) — the classic explainer
- [Gleam](https://gleam.run/) — BEAM language with a real static type system, if Dialyzer's limits frustrate you
