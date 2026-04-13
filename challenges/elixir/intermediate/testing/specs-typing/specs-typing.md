# Typespecs basics: `@spec`, `@type`, `@typep`

**Project**: `typespec_basics` — a tiny money/pricing module fully annotated with
typespecs, validated by Dialyxir.

---

## Why spec y tipado matters

You're introducing Elixir to a team coming from TypeScript. They keep asking
"where are the types?". The honest answer: Elixir is dynamically typed, but
`@spec` + Dialyzer gives you a separate static analyzer that catches real
bugs — especially around `nil`, error tuples, and "I thought this always
returned a map" surprises.

This exercise builds a small `Pricing` module — the kind of code that ends
up handling money in every startup — and annotates it fully. You'll see
`@type` (public), `@typep` (private), and `@spec` in action, and run
Dialyzer on it to confirm the types match the code.

---

## Project structure

```
typespec_basics/
├── lib/
│   └── typespec_basics.ex
├── script/
│   └── main.exs
├── test/
│   └── typespec_basics_test.exs
└── mix.exs
```

---

## Why `@spec` + Dialyzer and not runtime type checks

Los runtime checks cuestan ciclos en cada call y duplican información
que el compilador ya tiene. `@spec` + Dialyzer mueve esa verificación
a build time: cero costo runtime, atrapa violaciones cross call-sites.
El trade-off es precisión — success typing se pierde algunos bugs —
pero `@spec` + tests + guards en IO boundaries cubre más que cualquier
disciplina sola.

---

## Core concepts

### 1. `@type` vs `@typep` vs `@opaque`

```elixir
@type money :: %{amount: integer(), currency: String.t()}   # public
@typep internal_rate :: float()                              # private to this module
@opaque handle :: reference()                                # public name, hidden shape
```
Rule of thumb: expose `@type` for anything callers need to *pattern-match* or
*construct*. Use `@typep` for intermediate helpers that don't leak. `@opaque`
is for types whose shape you want to hide from callers entirely.

### 2. `@spec` is not runtime enforcement

`@spec` is a hint for humans and for Dialyzer. It is NOT checked at runtime.
If your code actually returns `nil` but the spec says `:: integer()`, the
program runs happily — only `mix dialyzer` will complain. This is by design:
specs are zero-cost.

### 3. Dialyxir's success typing

Dialyzer uses *success typing*: it only flags code that *cannot succeed*
for any input type. This means it rarely produces false positives but it
also misses some real bugs. Treat Dialyzer as a high-signal linter, not a
safety net.

### 4. Common built-in types worth memorizing

- `non_neg_integer()` / `pos_integer()` — integer with lower bound.
- `String.t()` — UTF-8 binary (not `char_list()`).
- `keyword()` / `keyword(t)` — a list of `{atom, any}` / `{atom, t}`.
- `mfa()` — `{module, atom, arity}`.
- `GenServer.on_start()` — the canonical `{:ok, pid} | {:error, reason}`.

---

## Design decisions

**Option A — Spec solo funciones públicas**
- Pros: Mantenimiento mínimo.
- Cons: Helpers privados pueden desviarse silenciosamente.

**Option B — Spec públicas y helpers privados no triviales** (elegida)
- Pros: Dialyzer tiene ancla en cada capa; warnings apuntan al
  helper, no a un caller tres frames arriba.
- Cons: Más líneas de `@spec`.

→ Elegida **B** para módulos de dominio. Para glue/CRUD, Option A.

---

## Implementation

### `mix.exs`

```elixir
defmodule TypespecBasics.MixProject do
  use Mix.Project

  def project do
    [
      app: :typespec_basics,
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

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.

```bash
mix new typespec_basics
cd typespec_basics
```

Add Dialyxir to `mix.exs`:

Then `mix deps.get`.

### `lib/pricing.ex`

**Objective**: Implement `pricing.ex` — the subject under test — shaped specifically to make the testing technique of this lab observable.

```elixir
defmodule Pricing do
  @moduledoc """
  Minimal pricing helpers for orders with line items. All public functions
  carry `@spec`s so Dialyzer can verify the module end-to-end.
  """

  # Public types — callers may pattern-match or construct these.
  @type currency :: String.t()
  @type money :: %{amount: integer(), currency: currency()}
  @type line_item :: %{sku: String.t(), unit_price: money(), quantity: pos_integer()}
  @type discount :: {:percent, 0..100} | {:flat, money()}
  @type pricing_error :: :currency_mismatch | :empty_cart

  # Private type — only used by helpers inside this module.
  @typep subtotal_acc :: %{currency: currency() | nil, amount: integer()}

  @doc "Builds a money struct. Amount is in minor units (cents)."
  @spec money(integer(), currency()) :: money()
  def money(amount, currency) when is_integer(amount) and is_binary(currency) do
    %{amount: amount, currency: currency}
  end

  @doc """
  Computes the subtotal of a cart. Returns `{:error, :empty_cart}` if empty,
  `{:error, :currency_mismatch}` if line items mix currencies.
  """
  @spec subtotal([line_item()]) :: {:ok, money()} | {:error, pricing_error()}
  def subtotal([]), do: {:error, :empty_cart}

  def subtotal(items) when is_list(items) do
    Enum.reduce_while(items, %{currency: nil, amount: 0}, &accumulate/2)
    |> finalize_subtotal()
  end

  @spec accumulate(line_item(), subtotal_acc()) ::
          {:cont, subtotal_acc()} | {:halt, {:error, :currency_mismatch}}
  defp accumulate(%{unit_price: %{currency: c, amount: a}, quantity: q}, %{currency: nil} = acc) do
    {:cont, %{currency: c, amount: acc.amount + a * q}}
  end

  defp accumulate(%{unit_price: %{currency: c, amount: a}, quantity: q}, %{currency: c} = acc) do
    {:cont, %{acc | amount: acc.amount + a * q}}
  end

  defp accumulate(_item, _acc), do: {:halt, {:error, :currency_mismatch}}

  @spec finalize_subtotal(subtotal_acc() | {:error, :currency_mismatch}) ::
          {:ok, money()} | {:error, pricing_error()}
  defp finalize_subtotal({:error, _} = err), do: err
  defp finalize_subtotal(%{currency: c, amount: a}), do: {:ok, money(a, c)}

  @doc "Applies a discount to a `money()` total. Never goes below zero."
  @spec apply_discount(money(), discount()) :: money()
  def apply_discount(%{amount: a, currency: c}, {:percent, p}) when p in 0..100 do
    money(max(a - div(a * p, 100), 0), c)
  end

  def apply_discount(%{amount: a, currency: c}, {:flat, %{amount: d, currency: c}}) do
    money(max(a - d, 0), c)
  end
end
```
### Step 3: `test/pricing_test.exs`

**Objective**: Write `pricing_test.exs` exercising the exact ExUnit feature under study — assertions should fail loudly if the technique is misused.

```elixir
defmodule PricingTest do
  use ExUnit.Case, async: true

  doctest Pricing

  describe "subtotal/1" do
    test "sums line items in the same currency" do
      items = [
        %{sku: "A", unit_price: Pricing.money(1000, "USD"), quantity: 2},
        %{sku: "B", unit_price: Pricing.money(500, "USD"), quantity: 3}
      ]

      assert {:ok, %{amount: 3500, currency: "USD"}} = Pricing.subtotal(items)
    end

    test "rejects mixed currencies" do
      items = [
        %{sku: "A", unit_price: Pricing.money(1000, "USD"), quantity: 1},
        %{sku: "B", unit_price: Pricing.money(800, "EUR"), quantity: 1}
      ]

      assert {:error, :currency_mismatch} = Pricing.subtotal(items)
    end

    test "rejects empty cart" do
      assert {:error, :empty_cart} = Pricing.subtotal([])
    end
  end

  describe "apply_discount/2" do
    test "percent discount" do
      assert %{amount: 800, currency: "USD"} =
               Pricing.apply_discount(Pricing.money(1000, "USD"), {:percent, 20})
    end

    test "flat discount cannot go below zero" do
      assert %{amount: 0} =
               Pricing.apply_discount(Pricing.money(500, "USD"), {:flat, Pricing.money(800, "USD")})
    end
  end
end
```
### Step 4: Run tests and Dialyzer

**Objective**: Run tests and Dialyzer.

```bash
mix test
mix dialyzer
```

The first `mix dialyzer` run builds the PLT and is slow (minutes). Subsequent
runs are fast. A clean run prints `done (passed successfully)`.

### Why this works

`@type`/`@typep`/`@opaque` registran tipos nombrados en la metadata
del módulo; `@spec` ata arity a tipos input/output. Dialyzer lee eso
más los tipos success inferidos, y marca cualquier call site que no
pueda matchear el contrato. Por ser estático, el check es gratis en
runtime; por usar success typing, los falsos positivos son bajos.

---

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Runnable demo of `TypespecBasics`.

  Exercises the public API end-to-end so behaviour is observable
  in addition to documented.
  """

  def main do
    IO.puts("=== TypespecBasics demo ===")
    run()
    IO.puts("\nDone — demo finished without errors.")
  end

  defp run do
    :ok
  end
end

Main.main()
```
## Benchmark

<!-- benchmark N/A: `@spec` no agrega código en runtime. La medición
relevante es el tiempo de `mix dialyzer` y la cantidad de warnings;
ninguno se mide con `:timer.tc`. -->

---

## Trade-offs and production gotchas

**1. `@spec` does not run — tests still matter**
Specs catch type mismatches; they don't catch wrong *values*. You still need
tests for `apply_discount` returning 0 vs a negative number.

**2. Precision matters — vague specs don't help Dialyzer**
`@spec subtotal(list()) :: any()` is worse than no spec: it tells Dialyzer
to trust you. Use concrete element types like `[line_item()]`.

**3. `@type` exports leak into your public API**
Once a `@type` is exported, removing or changing it is a breaking change for
callers who reference `Pricing.money()` in their own specs. Use `@typep` if
the type is internal.

**4. Dialyzer loves `no_return()` for raising functions**
A function that only raises should be specced `:: no_return()`. Otherwise
Dialyzer infers its "return" and propagates nonsense through call sites.

**5. When NOT to bother with typespecs**
Throwaway scripts, tiny modules with self-evident types, and test support
code. Spec every *public* function of every *library* or *domain* module,
and don't agonize over specing every private one.

---

## Reflection

- Agregás `@spec` a 40 funciones legacy y Dialyzer pasa de 0 a 120
  warnings. ¿Por qué aumentan? ¿Cómo priorizás cuáles arreglar versus
  cuáles agregás a `.dialyzer_ignore.exs`?
- `Pricing` es consumido por 10 servicios. Una migración cambia
  `money()` de map a struct. ¿Qué rompés al hacerlo `@opaque` y cómo
  facilitás la migración sin big-bang?

---
## Resources

- [Typespecs — Elixir reference](https://hexdocs.pm/elixir/typespecs.html)
- [Dialyxir](https://hexdocs.pm/dialyxir/readme.html)
- ["Success Typings for Erlang" — Lindahl & Sagonas, 2006](https://user.it.uu.se/~kostis/Papers/succ_types.pdf) — the paper behind Dialyzer
- [Learn You Some Erlang: Dialyzer](https://learnyousomeerlang.com/dialyzer)

## Key concepts
ExUnit testing in Elixir balances speed, isolation, and readability. The framework provides fixtures, setup hooks, and async mode to achieve both performance and determinism.

**ExUnit patterns and fixtures:**
`setup_all` runs once per module (module-scoped state); `setup` runs before each test. Returning `{:ok, map}` injects variables into the test context. For side-effectful setup (e.g., starting supervised processes), use `start_supervised` — it automatically stops the process when the test ends, ensuring cleanup.

**Async safety and isolation:**
Tests with `async: true` run in parallel, but they must be isolated. Shared resources (database, ETS tables, Registry) require careful locking. A common pattern: `setup :set_myflag` — a private setup that configures a unique state for that test. Avoid global state unless protected by locks.

**Mocking trade-offs:**
Libraries like `Mox` provide compile-time mock modules that behave like real modules but with controlled behavior. The benefit: you catch missing function implementations at test time. The trade-off: mocks don't catch runtime errors (e.g., a real function that crashes). For critical paths, complement mocks with integration tests against real dependencies. Dependency injection (passing modules as arguments) is more testable than direct calls.

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

### `test/typespec_basics_test.exs`

```elixir
defmodule TypespecBasicsTest do
  use ExUnit.Case, async: true

  doctest TypespecBasics

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert TypespecBasics.run(:noop) == :ok
    end
  end
end
```