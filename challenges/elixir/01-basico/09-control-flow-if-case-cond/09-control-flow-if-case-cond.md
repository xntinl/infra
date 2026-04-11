# Control Flow: Transaction Routing and Validation Rules

**Project**: `payments_cli` — built incrementally across the basic level

---

## Project context

You're building `payments_cli`. The system needs to make routing decisions
(which processor handles which transaction), apply validation rules with multiple
conditions, and chain steps that can each fail. The choice between `if`, `case`,
`cond`, and `with` determines code readability and correctness.

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
│       └── router.ex       # ← you implement this
├── test/
│   └── payments_cli/
│       └── router_test.exs  # given tests — must pass without modification
└── mix.exs
```

---

## The control flow decision matrix

Elixir has four control flow mechanisms. The choice is not arbitrary:

| Mechanism | Use when |
|-----------|----------|
| `if` / `unless` | Simple boolean check, one or two branches |
| `case` | Matching against shapes/patterns of a value |
| `cond` | Multiple independent boolean conditions |
| `with` | Chain of operations that each can return `{:ok, _}` or `{:error, _}` |

Common mistakes:
- Using `cond` with literal values when `case` is cleaner
- Using nested `case` when `with` flattens the code
- Using `if` chains when `cond` expresses intent better

The key insight: **all four are expressions**. They return a value. You can assign
the result of a `case` directly:

```elixir
fee_rate = case currency do
  "USD" -> 0.025
  "EUR" -> 0.020
  _     -> 0.030
end
```

This functional style is idiomatic Elixir and prevents the "assign default, then
maybe override" pattern from imperative languages.

---

## The business problem

The `Router` module needs to:

1. Route transactions to the correct payment processor based on currency and amount
2. Validate a transaction through multiple independent business rules (`cond`)
3. Run the full processing pipeline using `with` to chain fallible steps
4. Decide whether to retry a failed transaction based on error type

---

## Implementation

### `lib/payments_cli/router.ex`

```elixir
defmodule PaymentsCli.Router do
  @moduledoc """
  Routes transactions to payment processors and applies business validation.

  Demonstrates the four control flow mechanisms in context:
  - if: simple binary decisions
  - case: routing based on pattern/shape
  - cond: multi-condition validation
  - with: chaining fallible pipeline steps
  """

  @doc """
  Routes a transaction to the appropriate payment processor.

  Routing rules (in priority order):
  1. Amounts > $5000 always go to :enterprise_processor
  2. EUR currency goes to :sepa_processor
  3. GBP currency goes to :bacs_processor
  4. USD and all others go to :stripe

  Returns the processor atom.

  ## Examples

      iex> PaymentsCli.Router.route(%{currency: "USD", amount_cents: 1000})
      :stripe

      iex> PaymentsCli.Router.route(%{currency: "EUR", amount_cents: 1000})
      :sepa_processor

      iex> PaymentsCli.Router.route(%{currency: "USD", amount_cents: 600_000})
      :enterprise_processor

  """
  @spec route(map()) :: atom()
  def route(%{currency: currency, amount_cents: amount}) do
    # TODO: use cond to implement the routing rules
    # Rules are checked in priority order — cond evaluates top-to-bottom
    # and uses the first truthy condition.
    #
    # cond do
    #   amount > 500_000         -> :enterprise_processor
    #   currency == "EUR"        -> :sepa_processor
    #   currency == "GBP"        -> :bacs_processor
    #   true                     -> :stripe
    # end
  end

  @doc """
  Validates a transaction against multiple independent business rules.

  Returns :ok if all rules pass, or {:error, reason} for the first failure.

  Rules:
  1. amount_cents must be > 0
  2. currency must be exactly 3 characters
  3. id must be non-empty
  4. status must be :pending (only pending transactions are processed)

  ## Examples

      iex> tx = %{id: "T1", amount_cents: 1000, currency: "USD", status: :pending}
      iex> PaymentsCli.Router.validate(tx)
      :ok

      iex> PaymentsCli.Router.validate(%{id: "T1", amount_cents: -1, currency: "USD", status: :pending})
      {:error, "amount must be positive"}

  """
  @spec validate(map()) :: :ok | {:error, String.t()}
  def validate(%{id: id, amount_cents: amount, currency: currency, status: status}) do
    # TODO: use cond to check each rule in order
    # Each condition returns an {:error, reason} string
    # The final true -> :ok is the happy path
    #
    # cond do
    #   amount <= 0                       -> {:error, "amount must be positive"}
    #   String.length(currency) != 3      -> {:error, "invalid currency code"}
    #   id == "" or is_nil(id)            -> {:error, "id is required"}
    #   status != :pending                -> {:error, "only pending transactions can be processed"}
    #   true                              -> :ok
    # end
  end

  @doc """
  Runs the full processing pipeline for a transaction.

  Steps (each can fail):
  1. validate/1 — business rule validation
  2. route/1 — determine processor
  3. simulate_processor_call/2 — call the processor (simulated)

  Uses `with` to chain the steps. If any step fails, the error propagates
  immediately without executing subsequent steps.

  Returns {:ok, %{processor: atom, transaction: map}} or {:error, reason}.
  """
  @spec process(map()) :: {:ok, map()} | {:error, term()}
  def process(transaction) when is_map(transaction) do
    # TODO: implement using with
    #
    # with :ok             <- validate(transaction),
    #      processor       = route(transaction),
    #      {:ok, result}   <- simulate_processor_call(processor, transaction) do
    #   {:ok, %{processor: processor, transaction: result}}
    # end
    #
    # Note: `validate/1` returns :ok (not {:ok, _}) on success.
    # The `with` clause `<-` matches :ok against :ok and continues.
    # If validate returns {:error, reason}, with immediately returns {:error, reason}.
    #
    # The `processor = route(transaction)` line uses `=` (not `<-`) because
    # route/1 never fails — no need to match on {:ok, _}.
  end

  @doc """
  Determines whether a failed transaction should be retried.

  Uses `case` to match on the error shape — not `if` chains.

  Retryable errors: :timeout, :network_error, :rate_limited
  Non-retryable: :insufficient_funds, :card_declined, :fraud_detected

  ## Examples

      iex> PaymentsCli.Router.retryable?({:error, :timeout})
      true

      iex> PaymentsCli.Router.retryable?({:error, :card_declined})
      false

  """
  @spec retryable?({:error, atom()}) :: boolean()
  def retryable?(error_result) do
    # TODO: use case to match on the error
    #
    # case error_result do
    #   {:error, :timeout}            -> true
    #   {:error, :network_error}      -> true
    #   {:error, :rate_limited}       -> true
    #   {:error, _other}              -> false
    # end
  end

  # ---------------------------------------------------------------------------
  # Private helper — simulates a processor API call
  # ---------------------------------------------------------------------------

  defp simulate_processor_call(:enterprise_processor, tx) do
    # Enterprise processor requires a reference field
    if Map.has_key?(tx, :reference) do
      {:ok, Map.put(tx, :status, :approved)}
    else
      {:error, :missing_reference}
    end
  end

  defp simulate_processor_call(_processor, tx) do
    # Standard processors always approve in this simulation
    {:ok, Map.put(tx, :status, :approved)}
  end
end
```

### Given tests — must pass without modification

```elixir
# test/payments_cli/router_test.exs
defmodule PaymentsCli.RouterTest do
  use ExUnit.Case, async: true

  alias PaymentsCli.Router

  describe "route/1" do
    test "routes large amounts to enterprise processor" do
      tx = %{currency: "USD", amount_cents: 600_000}
      assert Router.route(tx) == :enterprise_processor
    end

    test "routes EUR to SEPA" do
      tx = %{currency: "EUR", amount_cents: 1000}
      assert Router.route(tx) == :sepa_processor
    end

    test "routes GBP to BACS" do
      tx = %{currency: "GBP", amount_cents: 1000}
      assert Router.route(tx) == :bacs_processor
    end

    test "routes USD to Stripe" do
      tx = %{currency: "USD", amount_cents: 1000}
      assert Router.route(tx) == :stripe
    end

    test "large EUR amount still goes to enterprise" do
      tx = %{currency: "EUR", amount_cents: 600_000}
      assert Router.route(tx) == :enterprise_processor
    end
  end

  describe "validate/1" do
    @valid %{id: "T1", amount_cents: 1000, currency: "USD", status: :pending}

    test "valid transaction returns :ok" do
      assert Router.validate(@valid) == :ok
    end

    test "negative amount fails" do
      tx = %{@valid | amount_cents: -1}
      assert {:error, _} = Router.validate(tx)
    end

    test "zero amount fails" do
      tx = %{@valid | amount_cents: 0}
      assert {:error, _} = Router.validate(tx)
    end

    test "invalid currency code fails" do
      tx = %{@valid | currency: "US"}
      assert {:error, _} = Router.validate(tx)
    end

    test "non-pending status fails" do
      tx = %{@valid | status: :approved}
      assert {:error, _} = Router.validate(tx)
    end
  end

  describe "process/1" do
    @valid %{id: "T1", amount_cents: 1000, currency: "USD", status: :pending}

    test "processes a valid transaction" do
      assert {:ok, result} = Router.process(@valid)
      assert result.processor == :stripe
      assert result.transaction.status == :approved
    end

    test "returns error for invalid transaction" do
      assert {:error, _} = Router.process(%{@valid | amount_cents: 0})
    end

    test "enterprise processor requires reference" do
      large = %{@valid | amount_cents: 600_000}
      assert {:error, :missing_reference} = Router.process(large)
    end

    test "enterprise processor succeeds with reference" do
      large = %{@valid | amount_cents: 600_000, reference: "REF001"}
      assert {:ok, _result} = Router.process(large)
    end
  end

  describe "retryable?/1" do
    test "timeout is retryable" do
      assert Router.retryable?({:error, :timeout}) == true
    end

    test "network error is retryable" do
      assert Router.retryable?({:error, :network_error}) == true
    end

    test "card declined is not retryable" do
      assert Router.retryable?({:error, :card_declined}) == false
    end

    test "fraud detected is not retryable" do
      assert Router.retryable?({:error, :fraud_detected}) == false
    end
  end
end
```

### Run the tests

```bash
mix test test/payments_cli/router_test.exs --trace
```

---

## Trade-off analysis

| Aspect | `with` (your impl) | Nested `case` | `try/rescue` |
|--------|-------------------|---------------|-------------|
| Happy path readability | Linear — no nesting | Pyramid of indentation | Flat but implicit flow |
| Error propagation | Automatic on `<-` mismatch | Manual return at each level | Exception unwinds stack |
| Error handling location | `else` block at bottom | At each `case` level | `rescue` block |
| Testing | Test each step independently | Same | Harder to isolate |

Reflection question: `process/1` has a line `processor = route(transaction)` with
`=` (not `<-`). What does `<-` do that `=` does not? When would using `<-` with
a function that never returns `{:error, _}` cause a problem?

---

## Common production mistakes

**1. `cond` without a `true ->` catch-all**
If no condition is truthy, `cond` raises `CondClauseError` at runtime. In payment
processing, an unexpected transaction shape silently crashes the process. Always end
`cond` with `true ->`.

**2. Using `if` chains instead of `cond`**
```elixir
# WRONG — reads like imperative code, each branch re-evaluates
if amount <= 0 do
  {:error, "bad amount"}
else
  if String.length(currency) != 3 do ...
```
Use `cond` — it communicates "evaluate conditions until one is true" clearly.

**3. Missing `else` block in `with`**
Without an `else` block, `with` returns the first non-matching value as-is.
For `process/1`, if `validate/1` returns `{:error, reason}`, `with` returns
`{:error, reason}` directly. This is usually correct. But if you need to
transform errors (add context, re-tag), add `else error -> handle(error) end`.

**4. Elixir's truthiness surprise: `0` is truthy**
In JavaScript and Python, `if 0` is falsy. In Elixir, only `false` and `nil` are
falsy. `if 0, do: "yes"` returns `"yes"`. Write `if amount > 0` not `if amount`.

**5. `case` without catch-all on external data**
```elixir
case Map.get(config, :mode) do
  :fast -> ...
  :safe -> ...
  # No catch-all — crashes on unexpected :ultra_safe
end
```
For data that comes from configuration, environment variables, or external APIs,
always include a catch-all clause in `case`.

---

## Resources

- [case, cond, and if — Elixir Getting Started](https://elixir-lang.org/getting-started/case-cond-and-if.html)
- [with — Kernel.SpecialForms](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#with/1)
- [Elixir School — Control Structures](https://elixirschool.com/en/lessons/basics/control_structures)
- [Elixir in Action — Saša Jurić — Chapter 3 (control flow)](https://www.manning.com/books/elixir-in-action-third-edition)
