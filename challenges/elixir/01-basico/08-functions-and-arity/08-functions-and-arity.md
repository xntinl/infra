# Functions and Arity: The Transaction Module API

**Project**: `payments_cli` — built incrementally across the basic level

---

## Project context

You're building `payments_cli`. The `Transaction` module needs a well-designed
public API: functions that clearly express their intent through multiple clauses,
guards, and arity conventions. This exercise is about what makes a function
interface good in Elixir.

Project structure at this point:

```
payments_cli/
├── lib/
│   └── payments_cli/
│       ├── cli.ex
│       ├── transaction.ex   # ← you extend this
│       ├── ledger.ex
│       ├── formatter.ex
│       ├── pipeline.ex
│       └── processor.ex
├── test/
│   └── payments_cli/
│       └── transaction_api_test.exs  # given tests — must pass without modification
└── mix.exs
```

---

## Why arity is part of the function identity

In Elixir, `Transaction.describe/1` and `Transaction.describe/2` are completely
distinct functions — as different as `describe` and `describe_with_context`. The
notation `Module.function/arity` is the canonical identifier for a function in:

- Documentation: `see Transaction.classify_status/1`
- Error messages: `UndefinedFunctionError: function Transaction.classify_status/0 is undefined`
- Function capture: `&Transaction.classify_status/1`

This design eliminates a class of bugs where calling a function with the wrong number
of arguments silently uses defaults. In Elixir, calling `classify_status()` with no
arguments is a compile error — it looks for `classify_status/0` which does not exist.

Multiple clauses with pattern matching let you express business rules as data, not as
procedural conditionals. A payment processor that routes transactions based on currency
reads better as:

```elixir
def route_to_processor(:USD), do: :stripe
def route_to_processor(:EUR), do: :adyen
def route_to_processor(_),    do: :fallback_processor
```

than as a long `cond` block.

---

## The business problem

Extend the `Transaction` module with a complete API:

1. Describe a transaction for logging (multiple clauses by status)
2. Determine if a transaction is reversible (guard-based business rule)
3. Compare two transactions by amount (multiple arity variant)
4. Build a display label (default argument for optional currency symbol)

---

## Implementation

### Extend `lib/payments_cli/transaction.ex`

Each function demonstrates a different aspect of Elixir's function design.
`describe/1` uses one clause per status — each case is isolated, testable, and
adding a new status means adding one clause. `reversible?/1` puts the business
rule in a guard, making the condition visible at the function head. `compare/2`
and `compare/3` show arity-based differentiation. `label/1` and `label/2` use a
default argument with a header declaration required by multiple clauses.

```elixir
# Add to PaymentsCli.Transaction

@doc """
Returns a human-readable log description for a transaction.

Uses multiple clauses — one per status — so each case is explicit.
A catch-all handles statuses added in the future without breaking existing code.

## Examples

    iex> PaymentsCli.Transaction.describe(%{id: "T1", status: :approved, amount_cents: 1000, currency: "USD"})
    "T1: approved USD 10.00"

    iex> PaymentsCli.Transaction.describe(%{id: "T2", status: :declined, amount_cents: 500, currency: "USD"})
    "T2: DECLINED (amount: USD 5.00)"

"""
@spec describe(map()) :: String.t()
def describe(%{id: id, status: :approved, amount_cents: cents, currency: currency}) do
  "#{id}: approved #{format_display(cents, currency)}"
end

def describe(%{id: id, status: :declined, amount_cents: cents, currency: currency}) do
  "#{id}: DECLINED (amount: #{format_display(cents, currency)})"
end

def describe(%{id: id, status: :flagged, amount_cents: cents, currency: currency}) do
  "#{id}: FLAGGED FOR REVIEW — #{format_display(cents, currency)}"
end

def describe(%{id: id, status: :reversed, amount_cents: cents, currency: currency}) do
  "#{id}: reversed #{format_display(cents, currency)}"
end

def describe(%{id: id, status: status}) do
  "#{id}: #{status}"
end

@doc """
Returns true if a transaction can be reversed.

Business rules:
- Only :approved transactions can be reversed
- Amount must be positive (> 0)
- Uses guards so the rule is enforced at the function head, not in the body

## Examples

    iex> PaymentsCli.Transaction.reversible?(%{status: :approved, amount_cents: 1000})
    true

    iex> PaymentsCli.Transaction.reversible?(%{status: :declined, amount_cents: 1000})
    false

"""
@spec reversible?(map()) :: boolean()
def reversible?(%{status: :approved, amount_cents: cents}) when cents > 0, do: true
def reversible?(_), do: false

@doc """
Compares two transactions by amount.

Returns :gt, :lt, or :eq.

reversible?/1 and compare/2 demonstrate arity-based differentiation:
compare/2 takes two transactions; compare/3 takes two transactions and a field.

## Examples

    iex> t1 = %{amount_cents: 1000}
    iex> t2 = %{amount_cents: 500}
    iex> PaymentsCli.Transaction.compare(t1, t2)
    :gt

"""
@spec compare(map(), map()) :: :gt | :lt | :eq
def compare(%{amount_cents: a}, %{amount_cents: b}) do
  cond do
    a > b -> :gt
    a < b -> :lt
    true  -> :eq
  end
end

@doc """
Compares two transactions by a specified field.

The field must exist in both transactions and must be comparable.

## Examples

    iex> t1 = %{amount_cents: 1000, id: "TXN002"}
    iex> t2 = %{amount_cents: 500,  id: "TXN001"}
    iex> PaymentsCli.Transaction.compare(t1, t2, :id)
    :gt

"""
@spec compare(map(), map(), atom()) :: :gt | :lt | :eq
def compare(tx1, tx2, field) when is_atom(field) do
  v1 = Map.get(tx1, field)
  v2 = Map.get(tx2, field)

  cond do
    v1 > v2 -> :gt
    v1 < v2 -> :lt
    true    -> :eq
  end
end

@doc """
Builds a short display label for a transaction.

`symbol` is optional — defaults to the currency code when not provided.

## Examples

    iex> tx = %{id: "T1", amount_cents: 1234, currency: "USD"}
    iex> PaymentsCli.Transaction.label(tx)
    "T1 [USD 12.34]"

    iex> PaymentsCli.Transaction.label(tx, "$")
    "T1 [$12.34]"

"""
@spec label(map(), String.t()) :: String.t()
def label(tx, symbol \\ nil)

def label(%{id: id, amount_cents: cents, currency: currency}, nil) do
  "#{id} [#{currency} #{format_cents(cents)}]"
end

def label(%{id: id, amount_cents: cents}, symbol) when is_binary(symbol) do
  "#{id} [#{symbol}#{format_cents(cents)}]"
end

# ---------------------------------------------------------------------------
# Private helpers
# ---------------------------------------------------------------------------

defp format_cents(cents) do
  major = div(cents, 100)
  minor = rem(cents, 100)
  "#{major}.#{minor |> Integer.to_string() |> String.pad_leading(2, "0")}"
end

defp format_display(cents, currency) do
  "#{currency} #{format_cents(cents)}"
end
```

**Why this works:**

- `describe/1` has five clauses, one per known status plus a catch-all. The catch-all
  `%{id: id, status: status}` handles any future status without crashing. Clauses are
  ordered from most specific to least specific — the catch-all is always last.

- `reversible?/1` puts the business rule (`cents > 0`) in a guard. The guard is
  evaluated before the function body executes. If the guard fails, the clause does not
  match and the next clause is tried. The second clause `def reversible?(_)` catches
  everything else and returns `false`.

- `compare/2` pattern-matches `amount_cents` from both maps in the function head, then
  uses `cond` for the three-way comparison. `compare/3` is a separate function (different
  arity) that accepts an arbitrary field name and uses `Map.get/2` to extract values.

- `label/1` and `label/2` use a default argument `symbol \\ nil`. When a function
  with a default argument has multiple clauses, a header-only declaration is required
  (`def label(tx, symbol \\ nil)` with no body). The compiler generates `label/1`
  which calls `label/2` with `nil` as the second argument.

- `format_cents/1` splits the integer into major and minor units using `div/2` and
  `rem/2`, then pads the minor unit to two digits. `format_display/2` prepends the
  currency code.

### Given tests — must pass without modification

```elixir
# test/payments_cli/transaction_api_test.exs
defmodule PaymentsCli.TransactionApiTest do
  use ExUnit.Case, async: true

  alias PaymentsCli.Transaction

  @approved  %{id: "T1", status: :approved,  amount_cents: 1000, currency: "USD"}
  @declined  %{id: "T2", status: :declined,  amount_cents: 500,  currency: "USD"}
  @flagged   %{id: "T3", status: :flagged,   amount_cents: 750,  currency: "EUR"}
  @reversed  %{id: "T4", status: :reversed,  amount_cents: 200,  currency: "GBP"}
  @pending   %{id: "T5", status: :pending,   amount_cents: 300,  currency: "USD"}

  describe "describe/1" do
    test "describes approved transaction" do
      result = Transaction.describe(@approved)
      assert String.contains?(result, "T1")
      assert String.contains?(result, "approved")
    end

    test "describes declined transaction with emphasis" do
      result = Transaction.describe(@declined)
      assert String.contains?(result, "DECLINED")
    end

    test "describes flagged transaction" do
      result = Transaction.describe(@flagged)
      assert String.contains?(result, "FLAGGED")
    end

    test "describes reversed transaction" do
      result = Transaction.describe(@reversed)
      assert String.contains?(result, "reversed")
    end

    test "catch-all handles pending" do
      result = Transaction.describe(@pending)
      assert is_binary(result)
      assert String.contains?(result, "T5")
    end
  end

  describe "reversible?/1" do
    test "approved transaction with positive amount is reversible" do
      assert Transaction.reversible?(@approved) == true
    end

    test "declined transaction is not reversible" do
      assert Transaction.reversible?(@declined) == false
    end

    test "approved transaction with zero amount is not reversible" do
      tx = %{status: :approved, amount_cents: 0}
      assert Transaction.reversible?(tx) == false
    end
  end

  describe "compare/2 and compare/3" do
    test "returns :gt when first amount is greater" do
      assert Transaction.compare(@approved, @declined) == :gt
    end

    test "returns :lt when first amount is less" do
      assert Transaction.compare(@declined, @approved) == :lt
    end

    test "returns :eq for equal amounts" do
      same = %{amount_cents: 1000}
      assert Transaction.compare(@approved, same) == :eq
    end

    test "compare/3 compares by specified field" do
      t1 = %{amount_cents: 1000, id: "TXN002"}
      t2 = %{amount_cents: 500,  id: "TXN001"}
      assert Transaction.compare(t1, t2, :id) == :gt
    end
  end

  describe "label/1 and label/2" do
    test "label/1 uses currency code" do
      result = Transaction.label(@approved)
      assert String.contains?(result, "T1")
      assert String.contains?(result, "USD")
    end

    test "label/2 uses provided symbol" do
      result = Transaction.label(@approved, "$")
      assert String.contains?(result, "$")
      refute String.contains?(result, "USD")
    end
  end
end
```

### Run the tests

```bash
mix test test/payments_cli/transaction_api_test.exs --trace
```

---

## Trade-off analysis

| Aspect | Multiple clauses (your impl) | Single function with `cond` | Single function with `if` |
|--------|-----------------------------|-----------------------------|--------------------------|
| Adding a new status | Add one clause | Edit the `cond` block | Nest another `if` |
| Pattern match guards | First-class, in the head | Separate condition | Separate condition |
| Dialyzer exhaustiveness | Can warn on unhandled | Cannot | Cannot |
| Readability | Each case isolated | All cases in one block | Deeply nested |
| Catch-all | Natural final clause | `true ->` at end | Final `else` |

Reflection question: `label/1` uses a default argument `symbol \\ nil`. The
compiler generates two functions: `label/1` and `label/2`. What does `label/1`
actually do at the call site? Look at what Mix generates with `mix compile` and
inspect the beam file with `:beam_lib.chunks/2`.

---

## Common production mistakes

**1. Catch-all clause before specific clauses**
```elixir
def describe(_tx), do: "unknown"  # catches everything — wrong position
def describe(%{status: :approved}), do: "approved"  # never reached
```
Elixir evaluates clauses top-to-bottom and uses the first match. Specific clauses
must come before the catch-all. The compiler warns, but only if `--warnings-as-errors`
is set (which it should be in CI).

**2. Default arguments with multiple clauses need a header declaration**
```elixir
# WRONG — compiler error with multiple clauses that have a default
def label(tx, symbol \\ nil)  # this is the header-only declaration
def label(tx, nil), do: ...   # clause 1
def label(tx, symbol), do: ... # clause 2
```
The header-only declaration (`def label(tx, symbol \\ nil)` with no body) is
required when a function with a default argument has multiple clauses. Omitting it
causes a compilation error.

**3. Guards cannot call user-defined functions**
`when reversible?(tx)` is not valid in a guard. Guards are limited to BIFs
(built-in functions) like `is_integer/1`, `is_binary/1`, `>`, `<`, `rem/2`.
The reason: guards must be pure and side-effect-free. Move complex conditions
to the function body.

**4. Function capture captures the arity**
`&Transaction.compare/2` and `&Transaction.compare/3` are different captures.
Passing `&Transaction.compare/2` to `Enum.sort_by/3` will fail if you intended
the three-argument version.

**5. Forgetting that `defp` breaks the public contract**
Changing `def validate/1` to `defp validate/1` breaks every caller outside the
module. Unlike access modifiers in OO languages, there is no warning from the
compiler — callers just get `UndefinedFunctionError` at runtime.

---

## Resources

- [Modules and Functions — Elixir Getting Started](https://elixir-lang.org/getting-started/modules-and-functions.html)
- [def/defp — Kernel docs](https://hexdocs.pm/elixir/Kernel.html#def/2)
- [Guards — Elixir Getting Started](https://elixir-lang.org/getting-started/case-cond-and-if.html#guards)
- [Elixir School — Functions](https://elixirschool.com/en/lessons/basics/functions)
