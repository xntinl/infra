# Typespecs basics: `@spec`, `@type`, `@typep`

**Project**: `typespec_basics` — a tiny money/pricing module fully annotated with
typespecs, validated by Dialyxir.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Project context

You're introducing Elixir to a team coming from TypeScript. They keep asking
"where are the types?". The honest answer: Elixir is dynamically typed, but
`@spec` + Dialyzer gives you a separate static analyzer that catches real
bugs — especially around `nil`, error tuples, and "I thought this always
returned a map" surprises.

This exercise builds a small `Pricing` module — the kind of code that ends
up handling money in every startup — and annotates it fully. You'll see
`@type` (public), `@typep` (private), and `@spec` in action, and run
Dialyzer on it to confirm the types match the code.

Project structure:

```
typespec_basics/
├── lib/
│   └── pricing.ex
├── test/
│   └── pricing_test.exs
└── mix.exs
```

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
comes later (see exercise 115).

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

## Implementation

### Step 1: Create the project

```bash
mix new typespec_basics
cd typespec_basics
```

Add Dialyxir to `mix.exs`:

```elixir
defp deps do
  [{:dialyxir, "~> 1.4", only: [:dev, :test], runtime: false}]
end
```

Then `mix deps.get`.

### Step 2: `lib/pricing.ex`

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

```elixir
defmodule PricingTest do
  use ExUnit.Case, async: true

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

```bash
mix test
mix dialyzer
```

The first `mix dialyzer` run builds the PLT and is slow (minutes). Subsequent
runs are fast. A clean run prints `done (passed successfully)`.

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

## Resources

- [Typespecs — Elixir reference](https://hexdocs.pm/elixir/typespecs.html)
- [Dialyxir](https://hexdocs.pm/dialyxir/readme.html)
- ["Success Typings for Erlang" — Lindahl & Sagonas, 2006](https://user.it.uu.se/~kostis/Papers/succ_types.pdf) — the paper behind Dialyzer
- [Learn You Some Erlang: Dialyzer](https://learnyousomeerlang.com/dialyzer)
