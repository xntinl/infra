# Numbers and Arithmetic: Transaction Amount Calculations

**Project**: `payments_cli` — built incrementally across the basic level

---

## Project context

You're building `payments_cli`. The `Transaction` module needs arithmetic to compute
fees, totals, and exchange-rate conversions. This is where Elixir's numeric types
and their gotchas directly affect correctness.

Project structure at this point:

```
payments_cli/
├── lib/
│   └── payments_cli/
│       ├── cli.ex              # from exercise 01
│       ├── transaction.ex      # from exercise 02
│       └── ledger.ex           # ← you implement this
├── test/
│   └── payments_cli/
│       └── ledger_test.exs     # given tests — must pass without modification
└── mix.exs
```

---

## Why numeric types matter for payments

Payment systems expose the most common numeric bugs in software:

1. **Float rounding errors** — `0.1 + 0.2 != 0.3` in IEEE 754. If you store
   `$0.10` as `0.1` and sum 10 such transactions, the total may be `$1.0000000000000002`.
   This is a real problem in production payment systems.

2. **Integer overflow** — Python and Ruby integers have arbitrary precision. Java's
   `int` overflows at ~2 billion. Elixir (and Erlang) integers have arbitrary precision
   like Python — no overflow, ever.

3. **Division confusion** — `10 / 2` returning `5.0` (float) instead of `5` (integer)
   breaks functions that expect an integer for pagination, list splitting, or index math.

The canonical solution in financial systems: **store amounts as integers in the smallest
unit** (cents for USD, pence for GBP, yen for JPY). Never store `$12.34` as `12.34`.
Store it as `1234` cents. All arithmetic happens on integers. Display formatting is
a presentation concern, not a domain concern.

---

## The business problem

The `Ledger` module needs to:

1. Sum transaction amounts (stored in cents as integers)
2. Calculate a processing fee (percentage of amount)
3. Convert amounts between currencies using exchange rates
4. Format amounts for display (`1234` → `"$12.34"`)

---

## Implementation

### `lib/payments_cli/ledger.ex`

```elixir
defmodule PaymentsCli.Ledger do
  @moduledoc """
  Financial calculations for the payments ledger.

  All amounts are stored and computed in the smallest unit of the currency
  (cents for USD, pence for GBP). This avoids IEEE 754 floating-point
  accumulation errors that are unacceptable in financial systems.

  Exchange rates are received as floats (from external APIs) and converted
  to integer arithmetic using a fixed precision factor.
  """

  @doc """
  Sums a list of transaction amounts in cents.

  Returns the total as an integer. Empty list returns 0.

  ## Examples

      iex> PaymentsCli.Ledger.sum_amounts([1000, 2500, 750])
      4250

      iex> PaymentsCli.Ledger.sum_amounts([])
      0

  """
  @spec sum_amounts([integer()]) :: integer()
  def sum_amounts(amounts) when is_list(amounts) do
    # TODO: use Enum.sum/1
    # Why not a recursive sum? Enum.sum is implemented in C in the BEAM and
    # is faster. Write your own only when you need custom accumulation logic.
  end

  @doc """
  Calculates the processing fee for an amount in cents.

  fee_basis_points is the fee expressed in basis points (1 basis point = 0.01%).
  100 basis points = 1%. This avoids floating-point fee rates.

  Returns the fee rounded DOWN (floor) to the nearest cent.
  The merchant always pays less than or equal to the theoretical fee.

  ## Examples

      iex> PaymentsCli.Ledger.calculate_fee(10_000, 250)
      250

      iex> PaymentsCli.Ledger.calculate_fee(333, 100)
      3

  """
  @spec calculate_fee(integer(), integer()) :: integer()
  def calculate_fee(amount_cents, fee_basis_points)
      when is_integer(amount_cents) and amount_cents >= 0 and
           is_integer(fee_basis_points) and fee_basis_points >= 0 do
    # TODO: implement integer-only fee calculation
    #
    # HINT: fee = amount_cents * fee_basis_points / 10_000
    # But / returns a float! Use div/2 for integer division (truncates toward zero).
    # For fees, truncating toward zero = rounding down = merchant-favorable.
    #
    # Why basis points? A fee of 2.5% as a float is 0.025.
    # As basis points it is 250 (integer). Integer math throughout.
  end

  @doc """
  Converts an amount from one currency to another.

  rate is the exchange rate as a float (e.g. 1.08 for USD to EUR).
  Returns the converted amount in cents, rounded to nearest cent.

  ## Examples

      iex> PaymentsCli.Ledger.convert_currency(10_000, 1.08)
      10800

      iex> PaymentsCli.Ledger.convert_currency(10_000, 0.92)
      9200

  """
  @spec convert_currency(integer(), float()) :: integer()
  def convert_currency(amount_cents, rate)
      when is_integer(amount_cents) and is_float(rate) and rate > 0 do
    # TODO: implement
    #
    # HINT: multiply amount_cents by rate (produces a float), then round/1
    # to get the nearest integer cent.
    # round/1 uses banker's rounding (round half to even). For most payment
    # rounding, this is acceptable.
  end

  @doc """
  Formats an amount in cents as a display string with currency symbol.

  ## Examples

      iex> PaymentsCli.Ledger.format_amount(1234, "USD")
      "$12.34"

      iex> PaymentsCli.Ledger.format_amount(100, "GBP")
      "£1.00"

      iex> PaymentsCli.Ledger.format_amount(50, "USD")
      "$0.50"

  """
  @spec format_amount(integer(), String.t()) :: String.t()
  def format_amount(amount_cents, currency) when is_integer(amount_cents) do
    # TODO: implement formatting
    #
    # HINT: use div/2 and rem/2 to split cents into major and minor units:
    #   major = div(amount_cents, 100)
    #   minor = rem(amount_cents, 100)
    #
    # Then format minor with leading zero: String.pad_leading(Integer.to_string(minor), 2, "0")
    #
    # Currency symbol lookup:
    #   "USD" -> "$"
    #   "GBP" -> "£"
    #   "EUR" -> "€"
    #   other -> currency <> " "  (e.g. "JPY 500")
  end
end
```

### Given tests — must pass without modification

```elixir
# test/payments_cli/ledger_test.exs
defmodule PaymentsCli.LedgerTest do
  use ExUnit.Case, async: true

  alias PaymentsCli.Ledger

  describe "sum_amounts/1" do
    test "sums a list of amounts" do
      assert Ledger.sum_amounts([1000, 2500, 750]) == 4250
    end

    test "returns 0 for empty list" do
      assert Ledger.sum_amounts([]) == 0
    end

    test "handles a single amount" do
      assert Ledger.sum_amounts([9999]) == 9999
    end
  end

  describe "calculate_fee/2" do
    test "2.5% fee on 100 USD (10000 cents)" do
      # 250 basis points = 2.5%
      assert Ledger.calculate_fee(10_000, 250) == 250
    end

    test "1% fee on $3.33 rounds down" do
      # $3.33 = 333 cents, 1% = 100 bp, fee = 3.33 cents -> floor -> 3 cents
      assert Ledger.calculate_fee(333, 100) == 3
    end

    test "zero fee" do
      assert Ledger.calculate_fee(10_000, 0) == 0
    end

    test "zero amount" do
      assert Ledger.calculate_fee(0, 250) == 0
    end
  end

  describe "convert_currency/2" do
    test "USD to EUR at 1.08 rate" do
      # $100.00 at 1.08 = $108.00
      assert Ledger.convert_currency(10_000, 1.08) == 10_800
    end

    test "USD to GBP at 0.79 rate" do
      assert Ledger.convert_currency(10_000, 0.79) == 7_900
    end

    test "identity rate" do
      assert Ledger.convert_currency(5_000, 1.0) == 5_000
    end
  end

  describe "format_amount/2" do
    test "formats USD" do
      assert Ledger.format_amount(1234, "USD") == "$12.34"
    end

    test "formats GBP" do
      assert Ledger.format_amount(100, "GBP") == "£1.00"
    end

    test "formats amount with leading zero in cents" do
      assert Ledger.format_amount(50, "USD") == "$0.50"
    end

    test "formats zero" do
      assert Ledger.format_amount(0, "USD") == "$0.00"
    end
  end
end
```

### Run the tests

```bash
mix test test/payments_cli/ledger_test.exs --trace
```

---

## Trade-off analysis

| Aspect | Integer cents (your impl) | Float dollars | Decimal library |
|--------|--------------------------|---------------|-----------------|
| Precision | Exact for integers | IEEE 754 errors accumulate | Exact decimal arithmetic |
| Performance | Fastest (native integer ops) | Fast | Slower (software arithmetic) |
| Display formatting | Manual (`div`/`rem`) | `Float.round` + string | Built-in formatting |
| External libraries | None needed | None needed | `{:decimal, "~> 2.0"}` |
| Use case | Simple currencies with fixed minor units | Approximations, not money | Currencies with variable precision |

Reflection question: `format_amount/2` uses `rem(amount_cents, 100)`. What happens if
`amount_cents` is negative? What does `rem(-50, 100)` return, and does your
formatting handle it correctly?

---

## Common production mistakes

**1. Storing money as floats**
`0.1 + 0.2` in IEEE 754 is `0.30000000000000004`. In a ledger that sums thousands
of transactions, these errors accumulate. Use integers in the smallest monetary unit.

**2. Using `/` where `div/2` is needed**
`div(total, count)` for computing average order value returns an integer.
`total / count` returns a float. Passing a float to `Enum.split/2` or as an
array index raises `FunctionClauseError`. The error message is confusing if
you don't know this rule.

**3. `rem/2` sign follows the dividend**
`rem(-7, 3)` is `-1` in Elixir (and C/Java). It is `2` in Python (`%` operator).
For displaying negative amounts, use `abs/1` on the result of `rem`.

**4. Float comparison with `==`**
Never write `rate == 1.0` to check for identity rate. Use `abs(rate - 1.0) < 1.0e-9`.
Float equality is almost never what you want.

**5. Integer precision on large amounts**
Elixir integers have arbitrary precision. `2 ** 100` works perfectly.
This is not true in all languages. You can safely accumulate millions of cent-valued
transactions without overflow.

---

## Resources

- [Integer — HexDocs](https://hexdocs.pm/elixir/Integer.html)
- [Float — HexDocs](https://hexdocs.pm/elixir/Float.html)
- [The Floating-Point Guide](https://floating-point-gui.de/) — essential reading for anyone handling money
- [Decimal library for Elixir](https://github.com/ericmj/decimal) — when you need exact decimal arithmetic
- [Erlang integer precision — efficiency guide](https://www.erlang.org/doc/efficiency_guide/advanced.html)
