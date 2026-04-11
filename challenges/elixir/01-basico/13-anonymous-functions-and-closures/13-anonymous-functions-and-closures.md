# Anonymous Functions and Closures: Configurable Processing Rules

**Project**: `payments_cli` — built incrementally across the basic level

---

## Project context

You're building `payments_cli`. The system needs configurable processing rules —
fee calculators, formatters, and validators that can be swapped at runtime without
changing module APIs. Anonymous functions and closures are the mechanism.

Project structure at this point:

```
payments_cli/
├── lib/
│   └── payments_cli/
│       ├── cli.ex
│       ├── transaction.ex
│       ├── ledger.ex
│       ├── formatter.ex
│       ├── pipeline.ex
│       ├── processor.ex
│       ├── router.ex
│       ├── analytics.ex
│       ├── report.ex
│       └── rules.ex        # ← you implement this
├── test/
│   └── payments_cli/
│       └── rules_test.exs  # given tests — must pass without modification
└── mix.exs
```

---

## Why closures enable configurable systems

A named function is defined at compile time and cannot capture runtime state.
An anonymous function (closure) captures variables from the scope where it was
created. This enables runtime configuration without changing the calling API.

Consider a fee calculator. Different payment processors have different fee structures.
Instead of a `calculate_fee(amount, processor)` function with a long `case`, you
create a closure per processor at startup:

```elixir
stripe_fee    = make_fee_calculator(250, 30)  # 2.5% + $0.30
paypal_fee    = make_fee_calculator(290, 0)   # 2.9% + $0
enterprise_fee = make_fee_calculator(100, 0)  # 1.0% + $0

# Later, the processor just calls the function — it does not know the rates
fee = stripe_fee.(transaction.amount_cents)
```

This is the **Strategy pattern** from OO design, expressed as higher-order functions.
The closure is the strategy. The caller does not need to know which strategy it has.

The production consequence: configuration loaded from environment variables or a
database at startup can be captured in closures and used by the processing pipeline
without threading configuration through every function signature.

---

## The business problem

The `Rules` module provides factory functions that return configured closures:

1. A fee calculator factory (configured with rate and fixed fee)
2. A transaction filter factory (configured with criteria)
3. A formatter factory (configured with locale/currency options)
4. A validator factory (configured with validation rules)

---

## Implementation

### `lib/payments_cli/rules.ex`

Each factory function returns a closure that captures its configuration. The caller
receives a function and does not need to know the configuration — it just calls the
function. This decouples configuration from invocation.

`make_fee_calculator/2` returns a function that computes `floor(amount * rate) + fixed`.
`make_filter/1` returns a predicate that applies all active filter criteria.
`make_formatter/1` returns a function that formats a transaction with captured options.
`make_validator/1` composes multiple validator functions into one.

```elixir
defmodule PaymentsCli.Rules do
  @moduledoc """
  Factory functions that produce configured closures for transaction processing.

  Each factory captures its configuration at creation time. The returned function
  can be passed to Enum functions, stored in maps, or called directly — it carries
  its configuration without requiring it to be threaded through every call site.
  """

  @doc """
  Returns a fee calculator function configured with basis points and fixed fee.

  The returned function takes amount_cents (integer) and returns fee_cents (integer).
  Fee = floor(amount * rate) + fixed_fee

  ## Examples

      iex> calc = PaymentsCli.Rules.make_fee_calculator(250, 30)
      iex> calc.(1000)
      55

      iex> calc = PaymentsCli.Rules.make_fee_calculator(0, 0)
      iex> calc.(9999)
      0

  """
  @spec make_fee_calculator(non_neg_integer(), non_neg_integer()) ::
          (integer() -> integer())
  def make_fee_calculator(basis_points, fixed_fee_cents)
      when is_integer(basis_points) and basis_points >= 0 and
           is_integer(fixed_fee_cents) and fixed_fee_cents >= 0 do
    fn amount_cents ->
      percentage_fee = div(amount_cents * basis_points, 10_000)
      percentage_fee + fixed_fee_cents
    end
  end

  @doc """
  Returns a filter predicate function configured with filter criteria.

  criteria is a map with optional keys:
    - min_amount: integer (inclusive)
    - max_amount: integer (inclusive)
    - statuses: list of atoms (if present, only these statuses pass)
    - currencies: list of strings (if present, only these currencies pass)

  The returned function takes a transaction map and returns a boolean.

  ## Examples

      iex> filter = PaymentsCli.Rules.make_filter(%{min_amount: 1000, statuses: [:approved]})
      iex> filter.(%{amount_cents: 500,  status: :approved})
      false
      iex> filter.(%{amount_cents: 1500, status: :approved})
      true
      iex> filter.(%{amount_cents: 1500, status: :declined})
      false

  """
  @spec make_filter(map()) :: (map() -> boolean())
  def make_filter(criteria) when is_map(criteria) do
    fn tx ->
      passes_min?(tx, criteria) and
        passes_max?(tx, criteria) and
        passes_statuses?(tx, criteria) and
        passes_currencies?(tx, criteria)
    end
  end

  @doc """
  Returns a formatting function configured with display options.

  opts is a keyword list with:
    - currency_symbol: string (default: use currency code)
    - show_status: boolean (default: false)
    - max_merchant_length: integer (default: 30)

  The returned function takes a transaction map and returns a formatted string.

  ## Examples

      iex> fmt = PaymentsCli.Rules.make_formatter(currency_symbol: "$", show_status: true)
      iex> fmt.(%{id: "T1", amount_cents: 1234, currency: "USD", status: :approved, merchant: "Shop"})
      "T1 | Shop | $12.34 | approved"

  """
  @spec make_formatter(keyword()) :: (map() -> String.t())
  def make_formatter(opts \\ []) when is_list(opts) do
    symbol = Keyword.get(opts, :currency_symbol, nil)
    show_status = Keyword.get(opts, :show_status, false)
    max_len = Keyword.get(opts, :max_merchant_length, 30)

    fn tx ->
      amount_str = format_amount(tx.amount_cents, tx.currency, symbol)
      merchant = String.slice(tx.merchant || "", 0, max_len)
      base = "#{tx.id} | #{merchant} | #{amount_str}"

      if show_status do
        base <> " | #{tx.status}"
      else
        base
      end
    end
  end

  @doc """
  Returns a composed validator function from a list of individual validator functions.

  Each validator in the list takes a transaction and returns :ok or {:error, reason}.
  The composed validator runs all validators and returns :ok only if all pass,
  or {:error, [reasons]} with all failure reasons.

  ## Examples

      iex> v1 = fn tx -> if tx.amount_cents > 0, do: :ok, else: {:error, "bad amount"} end
      iex> v2 = fn tx -> if is_binary(tx.currency), do: :ok, else: {:error, "bad currency"} end
      iex> combined = PaymentsCli.Rules.make_validator([v1, v2])
      iex> combined.(%{amount_cents: 100, currency: "USD"})
      :ok
      iex> combined.(%{amount_cents: -1, currency: "USD"})
      {:error, ["bad amount"]}

  """
  @spec make_validator([(map() -> :ok | {:error, String.t()})]) ::
          (map() -> :ok | {:error, [String.t()]})
  def make_validator(validators) when is_list(validators) do
    fn tx ->
      errors =
        Enum.reduce(validators, [], fn validator, acc ->
          case validator.(tx) do
            :ok -> acc
            {:error, reason} -> [reason | acc]
          end
        end)

      case errors do
        [] -> :ok
        reasons -> {:error, Enum.reverse(reasons)}
      end
    end
  end

  # ---------------------------------------------------------------------------
  # Private helpers
  # ---------------------------------------------------------------------------

  defp passes_min?(tx, %{min_amount: min}), do: tx.amount_cents >= min
  defp passes_min?(_tx, _criteria), do: true

  defp passes_max?(tx, %{max_amount: max}), do: tx.amount_cents <= max
  defp passes_max?(_tx, _criteria), do: true

  defp passes_statuses?(tx, %{statuses: statuses}), do: tx.status in statuses
  defp passes_statuses?(_tx, _criteria), do: true

  defp passes_currencies?(tx, %{currencies: currencies}), do: tx.currency in currencies
  defp passes_currencies?(_tx, _criteria), do: true

  defp format_amount(cents, currency, nil) do
    dollars = div(cents, 100)
    minor = rem(cents, 100)
    "#{currency} #{dollars}.#{String.pad_leading(Integer.to_string(minor), 2, "0")}"
  end

  defp format_amount(cents, _currency, symbol) do
    dollars = div(cents, 100)
    minor = rem(cents, 100)
    "#{symbol}#{dollars}.#{String.pad_leading(Integer.to_string(minor), 2, "0")}"
  end
end
```

**Why this works:**

- `make_fee_calculator/2` returns a `fn` that closes over `basis_points` and
  `fixed_fee_cents`. Each call to the factory creates an independent closure with its
  own captured values. Multiple calculators coexist without interference.

- `make_filter/1` returns a `fn` that closes over `criteria` and delegates to private
  helpers. Each helper uses pattern matching: `passes_min?(tx, %{min_amount: min})`
  matches when the criteria map has `:min_amount`; the catch-all `passes_min?(_tx, _criteria)`
  returns `true` when the key is absent. This means absent criteria = no constraint.

- `make_formatter/1` extracts options once at factory time (not per call), capturing
  the resolved values in the closure. The returned function only does formatting work —
  it does not re-read options on every invocation.

- `make_validator/1` returns a closure that reduces over the captured list of validators.
  Each validator is called with the transaction; errors are collected into a list.
  If no errors, return `:ok`. If errors, return them in order (reversed back from
  accumulation order).

### Given tests — must pass without modification

```elixir
# test/payments_cli/rules_test.exs
defmodule PaymentsCli.RulesTest do
  use ExUnit.Case, async: true

  alias PaymentsCli.Rules

  describe "make_fee_calculator/2" do
    test "returns a function" do
      calc = Rules.make_fee_calculator(250, 30)
      assert is_function(calc, 1)
    end

    test "calculates fee with percentage and fixed component" do
      # 2.5% of 1000 = 25 cents + 30 cents fixed = 55 cents
      calc = Rules.make_fee_calculator(250, 30)
      assert calc.(1000) == 55
    end

    test "zero rate and zero fixed" do
      calc = Rules.make_fee_calculator(0, 0)
      assert calc.(9999) == 0
    end

    test "different calculators are independent" do
      stripe = Rules.make_fee_calculator(250, 30)
      paypal = Rules.make_fee_calculator(290, 0)
      # Same amount, different fees
      assert stripe.(1000) != paypal.(1000)
    end

    test "each calculator closure captures its own configuration" do
      calcs = Enum.map([100, 200, 300], fn bp -> Rules.make_fee_calculator(bp, 0) end)
      # Each calc should use its own basis points
      fees = Enum.map(calcs, fn calc -> calc.(10_000) end)
      assert fees == [100, 200, 300]
    end
  end

  describe "make_filter/1" do
    test "returns a function" do
      filter = Rules.make_filter(%{})
      assert is_function(filter, 1)
    end

    test "min_amount filter" do
      filter = Rules.make_filter(%{min_amount: 1000})
      assert filter.(%{amount_cents: 999,  status: :approved, currency: "USD"}) == false
      assert filter.(%{amount_cents: 1000, status: :approved, currency: "USD"}) == true
    end

    test "status filter" do
      filter = Rules.make_filter(%{statuses: [:approved]})
      assert filter.(%{amount_cents: 100, status: :declined, currency: "USD"}) == false
      assert filter.(%{amount_cents: 100, status: :approved, currency: "USD"}) == true
    end

    test "combined criteria" do
      filter = Rules.make_filter(%{min_amount: 500, statuses: [:approved]})
      assert filter.(%{amount_cents: 1000, status: :approved, currency: "USD"}) == true
      assert filter.(%{amount_cents: 100,  status: :approved, currency: "USD"}) == false
      assert filter.(%{amount_cents: 1000, status: :declined, currency: "USD"}) == false
    end

    test "empty criteria passes everything" do
      filter = Rules.make_filter(%{})
      assert filter.(%{amount_cents: 1, status: :declined, currency: "XYZ"}) == true
    end
  end

  describe "make_formatter/1" do
    @tx %{id: "T1", amount_cents: 1234, currency: "USD", status: :approved, merchant: "Coffee Shop"}

    test "returns a function" do
      fmt = Rules.make_formatter()
      assert is_function(fmt, 1)
    end

    test "formats without symbol by default" do
      fmt = Rules.make_formatter()
      result = fmt.(@tx)
      assert String.contains?(result, "T1")
      assert String.contains?(result, "USD")
    end

    test "formats with currency symbol" do
      fmt = Rules.make_formatter(currency_symbol: "$")
      result = fmt.(@tx)
      assert String.contains?(result, "$12.34")
    end

    test "includes status when show_status is true" do
      fmt = Rules.make_formatter(show_status: true)
      result = fmt.(@tx)
      assert String.contains?(result, "approved")
    end

    test "omits status by default" do
      fmt = Rules.make_formatter()
      result = fmt.(@tx)
      refute String.contains?(result, "approved")
    end
  end

  describe "make_validator/1" do
    @amount_v fn tx -> if tx.amount_cents > 0, do: :ok, else: {:error, "bad amount"} end
    @currency_v fn tx -> if is_binary(tx.currency), do: :ok, else: {:error, "bad currency"} end

    test "returns a function" do
      v = Rules.make_validator([@amount_v])
      assert is_function(v, 1)
    end

    test "returns :ok when all validators pass" do
      v = Rules.make_validator([@amount_v, @currency_v])
      assert v.(%{amount_cents: 100, currency: "USD"}) == :ok
    end

    test "collects all error reasons" do
      v = Rules.make_validator([@amount_v, @currency_v])
      {:error, reasons} = v.(%{amount_cents: -1, currency: 123})
      assert length(reasons) == 2
    end

    test "empty validator list always passes" do
      v = Rules.make_validator([])
      assert v.(%{amount_cents: -999, currency: nil}) == :ok
    end
  end
end
```

### Run the tests

```bash
mix test test/payments_cli/rules_test.exs --trace
```

---

## Trade-off analysis

| Aspect | Closures as strategy (your impl) | Module callbacks (behaviour) | Config map + case |
|--------|----------------------------------|------------------------------|-------------------|
| Runtime configuration | Yes — captured at creation | No — fixed at compile time | Yes — passed per call |
| Type safety | Limited — function type only | Full — @callback specs | None |
| Testability | Test the closure directly | Test the callback module | Test the case branch |
| Discovery | Implicit — who has the function? | Explicit — `@impl` | Explicit — case clauses |
| Use case | Configurable runtime rules | Plugin systems, adapters | Simple branching |

Reflection question: `make_fee_calculator/2` returns a function with type
`(integer() -> integer())`. How would you make multiple instances of this function
in a map keyed by processor name, and then look up and call the right one for
each transaction? Think about `Map.fetch/2` and calling the returned function.

---

## Common production mistakes

**1. Calling anonymous functions without the dot**
```elixir
calc = Rules.make_fee_calculator(250, 30)
calc(1000)    # WRONG — UndefinedFunctionError: function calc/1 is undefined
calc.(1000)   # CORRECT — dot is required for functions stored in variables
```

**2. Closures capture the value, not the variable**
```elixir
rate = 250
calc = fn amount -> div(amount * rate, 10_000) end
rate = 500  # This does NOT affect calc — it captured rate = 250
calc.(1000)  # still 25, not 50
```
The closure captured the value `250` at creation. Re-binding `rate` creates a new
variable in the enclosing scope; the closure holds its own reference.

**3. Generating closures in a loop with a shared mutable reference**
In Elixir this is not a problem (variables are immutable). But developers from
JavaScript backgrounds expect the classic "closure in a loop" bug where all closures
share the last value of the loop variable. In Elixir, each iteration's binding is
independent.

**4. `&` syntax cannot capture multi-clause functions**
```elixir
# WRONG — & cannot express multiple clauses
filter = &(if &1.status == :approved and &1.amount_cents > 0, do: true, else: false)
# This works but is hard to read. Prefer fn ... end for anything non-trivial.
```
Use `fn ... end` for functions with logic. Reserve `&` for simple expressions
and function captures (`&String.upcase/1`).

**5. Forgetting that `is_function/2` checks arity**
```elixir
calc = Rules.make_fee_calculator(250, 30)
is_function(calc)    # true
is_function(calc, 1) # true — arity 1
is_function(calc, 2) # false — wrong arity
```
Use `is_function(f, expected_arity)` in guards and assertions to verify the
contract, not just that something is a function.

---

## Resources

- [Anonymous functions — Elixir Getting Started](https://elixir-lang.org/getting-started/basic-types.html#anonymous-functions)
- [Capture operator &/1 — Kernel.SpecialForms](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#&/1)
- [Elixir School — Functions](https://elixirschool.com/en/lessons/basics/functions)
- [Function — HexDocs](https://hexdocs.pm/elixir/Function.html) — `is_function/2`, `capture/3`
