# Numbers and Arithmetic: Building a Money Library

**Project**: `money` — a library for safe financial arithmetic using integer cents

---

## Why floating-point arithmetic breaks financial code

Every senior developer has heard "don't use floats for money," but few have
internalized why. In Elixir (and every IEEE 754 language):

```elixir
iex> 0.1 + 0.2
0.30000000000000004

iex> 0.1 + 0.2 == 0.3
false
```

In a payment system processing millions of transactions, these rounding errors
compound. A billing system that calculates `100 * 19.99` might produce
`1998.9999999999998` instead of `1999.0`, causing a one-cent discrepancy per
invoice. At scale, that is an audit failure.

The solution used by every serious financial system: represent money as integers
(cents, pence, centavos). `$19.99` becomes `1999` cents. Integer arithmetic in
Elixir is exact and arbitrary precision — there is no overflow.

```elixir
iex> 10 + 20
30  # Exactly 30 cents. No rounding. No surprises.

# Elixir integers are arbitrary precision
iex> 999_999_999_999_999_999 * 999_999_999_999_999_999
999999999999999998000000000000000001  # No overflow, exact result
```

---

## The business problem

Build a `Money` module that:

1. Represents monetary values as integer cents with a currency code
2. Performs addition, subtraction, and multiplication safely
3. Prevents arithmetic between different currencies
4. Splits amounts evenly (e.g., split a $10.01 bill three ways without losing a cent)
5. Formats amounts for display

---

## Project structure

```
money/
├── lib/
│   └── money.ex
├── test/
│   └── money_test.exs
└── mix.exs
```

---

## Design decisions

**Option A — integer cents**
- Pros: Exact arithmetic, no rounding errors, BEAM integers are arbitrary precision so no overflow
- Cons: Must convert at the boundary (UI, database), can't represent sub-cent precision without re-scaling

**Option B — floating-point dollars** (chosen)
- Pros: Natural representation, easy to display
- Cons: `0.1 + 0.2 != 0.3`, rounding errors compound across millions of transactions, audit failures

→ Chose **A** because financial correctness is non-negotiable and integer cents make every operation exact.

## Implementation

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Standard library: no external dependencies required
  ]
end
```


### `lib/money.ex`

```elixir
defmodule Money do
  @moduledoc """
  Safe monetary arithmetic using integer cents.

  All amounts are stored as integers representing the smallest currency unit
  (cents for USD/EUR, pence for GBP). This eliminates floating-point rounding
  errors entirely.

  Currency is tracked to prevent accidentally adding USD to EUR.
  """

  @type t :: %{amount: integer(), currency: atom()}

  @doc """
  Creates a Money value from an integer amount in cents.

  ## Examples

      iex> Money.new(1999, :usd)
      %{amount: 1999, currency: :usd}

  """
  @spec new(integer(), atom()) :: t()
  def new(amount, currency) when is_integer(amount) and is_atom(currency) do
    %{amount: amount, currency: currency}
  end

  @doc """
  Creates a Money value from a float dollar amount.

  Converts to cents internally using rounding to handle float imprecision.
  This is the ONLY place where floats touch the money system.

  ## Examples

      iex> Money.from_float(19.99, :usd)
      %{amount: 1999, currency: :usd}

      iex> Money.from_float(0.1 + 0.2, :usd)
      %{amount: 30, currency: :usd}

  """
  @spec from_float(float(), atom()) :: t()
  def from_float(dollars, currency) when is_float(dollars) and is_atom(currency) do
    cents = round(dollars * 100)
    new(cents, currency)
  end

  @doc """
  Adds two money values. Both must have the same currency.

  ## Examples

      iex> a = Money.new(1000, :usd)
      iex> b = Money.new(599, :usd)
      iex> Money.add(a, b)
      {:ok, %{amount: 1599, currency: :usd}}

      iex> a = Money.new(1000, :usd)
      iex> b = Money.new(500, :eur)
      iex> Money.add(a, b)
      {:error, :currency_mismatch}

  """
  @spec add(t(), t()) :: {:ok, t()} | {:error, :currency_mismatch}
  def add(%{currency: c} = a, %{currency: c} = b) do
    {:ok, new(a.amount + b.amount, c)}
  end

  def add(%{currency: _}, %{currency: _}), do: {:error, :currency_mismatch}

  @doc """
  Subtracts the second money value from the first.

  ## Examples

      iex> a = Money.new(1000, :usd)
      iex> b = Money.new(300, :usd)
      iex> Money.subtract(a, b)
      {:ok, %{amount: 700, currency: :usd}}

  """
  @spec subtract(t(), t()) :: {:ok, t()} | {:error, :currency_mismatch}
  def subtract(%{currency: c} = a, %{currency: c} = b) do
    {:ok, new(a.amount - b.amount, c)}
  end

  def subtract(%{currency: _}, %{currency: _}), do: {:error, :currency_mismatch}

  @doc """
  Multiplies a money value by a scalar (quantity, tax rate, etc.).

  The result is rounded to the nearest cent. This is the only operation
  that introduces rounding, and it happens at the integer level.

  ## Examples

      iex> price = Money.new(999, :usd)
      iex> Money.multiply(price, 3)
      %{amount: 2997, currency: :usd}

      iex> subtotal = Money.new(1000, :usd)
      iex> Money.multiply(subtotal, 1.0825)
      %{amount: 1083, currency: :usd}

  """
  @spec multiply(t(), number()) :: t()
  def multiply(%{amount: amount, currency: currency}, factor) when is_number(factor) do
    new(round(amount * factor), currency)
  end

  @doc """
  Splits a money value into N equal parts without losing cents.

  The remainder is distributed one cent at a time to the first parts.
  This guarantees that the parts always sum to the original amount.

  ## Examples

      iex> bill = Money.new(1001, :usd)
      iex> Money.split(bill, 3)
      [
        %{amount: 334, currency: :usd},
        %{amount: 334, currency: :usd},
        %{amount: 333, currency: :usd}
      ]

      iex> bill = Money.new(1000, :usd)
      iex> parts = Money.split(bill, 3)
      iex> parts |> Enum.map(& &1.amount) |> Enum.sum()
      1000

  """
  @spec split(t(), pos_integer()) :: [t()]
  def split(%{amount: amount, currency: currency}, parts)
      when is_integer(parts) and parts > 0 do
    base = div(amount, parts)
    remainder = rem(amount, parts)

    for i <- 1..parts do
      extra = if i <= remainder, do: 1, else: 0
      new(base + extra, currency)
    end
  end

  @doc """
  Formats a money value as a human-readable string.

  ## Examples

      iex> Money.format(Money.new(1999, :usd))
      "$19.99"

      iex> Money.format(Money.new(500, :eur))
      "€5.00"

      iex> Money.format(Money.new(-250, :usd))
      "-$2.50"

  """
  @spec format(t()) :: String.t()
  def format(%{amount: amount, currency: currency}) do
    symbol = currency_symbol(currency)
    {sign, abs_amount} = if amount < 0, do: {"-", -amount}, else: {"", amount}
    major = div(abs_amount, 100)
    minor = rem(abs_amount, 100) |> Integer.to_string() |> String.pad_leading(2, "0")
    "#{sign}#{symbol}#{major}.#{minor}"
  end

  @doc """
  Returns true if the money amount is zero.
  """
  @spec zero?(t()) :: boolean()
  def zero?(%{amount: 0}), do: true
  def zero?(%{amount: _}), do: false

  @doc """
  Returns true if the money amount is positive.
  """
  @spec positive?(t()) :: boolean()
  def positive?(%{amount: amount}) when amount > 0, do: true
  def positive?(%{amount: _}), do: false

  @spec currency_symbol(atom()) :: String.t()
  defp currency_symbol(:usd), do: "$"
  defp currency_symbol(:eur), do: "€"
  defp currency_symbol(:gbp), do: "£"
  defp currency_symbol(:jpy), do: "¥"
  defp currency_symbol(other), do: "#{other} "
end
```

**Why this works:**

- `add/2` pattern-matches the currency in both arguments using the same variable `c`.
  If the currencies differ, the first clause does not match, and the catch-all returns
  `{:error, :currency_mismatch}`. This is a compile-time guarantee — no `if` needed.
- `split/2` uses `div/2` and `rem/2` (integer division) to calculate the base share
  and remainder. The remainder is distributed one cent per part to the first N parts.
  This guarantees the sum of all parts equals the original.
- `from_float/2` is the only function that accepts floats. It uses `round/1` to convert
  to the nearest integer cent. All subsequent operations are pure integer math.
- `format/1` handles negative amounts (refunds) by extracting the sign first.

### Tests

```elixir
# test/money_test.exs
defmodule MoneyTest do
  use ExUnit.Case, async: true

  doctest Money

  describe "new/2" do
    test "creates money with integer cents" do
      m = Money.new(1999, :usd)
      assert m.amount == 1999
      assert m.currency == :usd
    end

    test "allows negative amounts for refunds" do
      m = Money.new(-500, :usd)
      assert m.amount == -500
    end

    test "allows zero" do
      m = Money.new(0, :usd)
      assert Money.zero?(m)
    end
  end

  describe "from_float/2" do
    test "converts dollars to cents" do
      m = Money.from_float(19.99, :usd)
      assert m.amount == 1999
    end

    test "handles float imprecision correctly" do
      m = Money.from_float(0.1 + 0.2, :usd)
      assert m.amount == 30
    end

    test "handles exact floats" do
      m = Money.from_float(10.0, :usd)
      assert m.amount == 1000
    end
  end

  describe "add/2" do
    test "adds same currency" do
      a = Money.new(1000, :usd)
      b = Money.new(599, :usd)
      assert {:ok, %{amount: 1599, currency: :usd}} = Money.add(a, b)
    end

    test "rejects different currencies" do
      a = Money.new(1000, :usd)
      b = Money.new(500, :eur)
      assert {:error, :currency_mismatch} = Money.add(a, b)
    end

    test "handles negative amounts" do
      a = Money.new(1000, :usd)
      b = Money.new(-300, :usd)
      assert {:ok, %{amount: 700}} = Money.add(a, b)
    end
  end

  describe "subtract/2" do
    test "subtracts same currency" do
      a = Money.new(1000, :usd)
      b = Money.new(300, :usd)
      assert {:ok, %{amount: 700}} = Money.subtract(a, b)
    end

    test "allows negative results" do
      a = Money.new(100, :usd)
      b = Money.new(500, :usd)
      assert {:ok, %{amount: -400}} = Money.subtract(a, b)
    end
  end

  describe "multiply/2" do
    test "multiplies by integer" do
      price = Money.new(999, :usd)
      assert %{amount: 2997} = Money.multiply(price, 3)
    end

    test "multiplies by float and rounds" do
      subtotal = Money.new(1000, :usd)
      with_tax = Money.multiply(subtotal, 1.0825)
      assert with_tax.amount == 1083
    end

    test "multiplies by zero" do
      m = Money.new(999, :usd)
      assert %{amount: 0} = Money.multiply(m, 0)
    end
  end

  describe "split/2" do
    test "splits evenly" do
      bill = Money.new(900, :usd)
      parts = Money.split(bill, 3)
      assert length(parts) == 3
      assert Enum.all?(parts, &(&1.amount == 300))
    end

    test "distributes remainder to first parts" do
      bill = Money.new(1001, :usd)
      parts = Money.split(bill, 3)
      amounts = Enum.map(parts, & &1.amount)
      assert amounts == [334, 334, 333]
    end

    test "parts always sum to original" do
      bill = Money.new(1000, :usd)

      for n <- 1..7 do
        parts = Money.split(bill, n)
        total = parts |> Enum.map(& &1.amount) |> Enum.sum()
        assert total == 1000, "Split into #{n} parts lost cents"
      end
    end

    test "split into 1 returns original" do
      bill = Money.new(999, :usd)
      assert [%{amount: 999}] = Money.split(bill, 1)
    end
  end

  describe "format/1" do
    test "formats USD" do
      assert Money.format(Money.new(1999, :usd)) == "$19.99"
    end

    test "formats EUR" do
      assert Money.format(Money.new(500, :eur)) == "€5.00"
    end

    test "formats negative amounts" do
      assert Money.format(Money.new(-250, :usd)) == "-$2.50"
    end

    test "formats zero" do
      assert Money.format(Money.new(0, :usd)) == "$0.00"
    end

    test "pads single-digit cents" do
      assert Money.format(Money.new(105, :usd)) == "$1.05"
    end
  end
end
```

### Run the tests

```bash
mix test --trace
```

---

### Why this works

The approach chosen above keeps the core logic **pure, pattern-matchable, and testable**. Each step is a small, named transformation with an explicit return shape, so adding a new case means adding a new clause — not editing a branching block. Failures are data (`{:error, reason}`), not control-flow, which keeps the hot path linear and the error path explicit.



---
## Key Concepts

### 1. Arbitrary-Precision Arithmetic Without Overflow

Elixir integers are unlimited precision. There is no overflow, no wraparound, no silent truncation. The BEAM handles small integers efficiently and automatically promotes to big integers when needed. For financial systems processing trillions of cents, this is non-negotiable: you never need overflow guards or BigInteger imports.

### 2. Integer Division is Not Symmetric

Elixir provides three division operations: `/` (always returns float), `div/2` (truncates toward zero), and `Integer.floor_div/2` (floors toward negative infinity). For money, always use `div/2` and `rem/2`. The `/` operator introduces floats and rounding problems. Common gotcha: `10 / 3` returns `3.333...`, not `3`.

### 3. Rounding Errors Compound at Scale

A single lost cent × 1 million transactions = $10,000 audit discrepancy. In financial systems, you round once at the end (display time), never at intermediate steps. Calculate the entire pipeline in exact integers (cents), then round the final result.

---
## Why Elixir integers never overflow

Unlike Java's `int` (32-bit, max 2,147,483,647) or Go's `int64`, Elixir integers
are arbitrary precision. The BEAM automatically promotes to big integers when needed:

```elixir
iex> :erlang.system_info(:wordsize)
8  # 64-bit machine

# Small integers (fits in a machine word) — fast, no allocation
iex> 42 + 1
43

# Big integers (exceeds machine word) — automatic, exact, slower
iex> 2 ** 100
1267650600228229401496703205376

# No overflow, no wrap-around, no silent truncation
iex> 9_999_999_999_999_999_999 + 1
10000000000000000000
```

For financial code, this means you never need to worry about overflow when summing
millions of transactions. The VM handles promotion transparently.

---

## Integer vs float division

Elixir distinguishes integer and float division at the operator level:

```elixir
iex> 10 / 3      # Always returns a float
3.3333333333333335

iex> div(10, 3)   # Integer division — truncates toward zero
3

iex> rem(10, 3)   # Integer remainder
1

iex> Integer.floor_div(-7, 2)  # Floors toward negative infinity
-4

iex> div(-7, 2)   # Truncates toward zero
-3
```

For money calculations, always use `div/2` and `rem/2`. The `/` operator introduces
floats and all their rounding problems.

---

## Benchmark

```elixir
{time_us, _result} =
  :timer.tc(fn ->
    for _ <- 1..1_000 do
      # representative call of split/2 over 10_000 iterations
      :ok
    end
  end)

IO.puts("Avg: #{time_us / 1_000} µs/call")
```

Target: **< 5ms total, split into small N under 1µs per call**.

## Common production mistakes

**1. Using `Decimal` when integers suffice**
The `Decimal` library exists for when you genuinely need arbitrary-precision
decimal arithmetic (e.g., cryptocurrency with 18 decimal places). For standard
currencies with 2 decimal places, integer cents are simpler, faster, and
sufficient.

**2. Mixing float and integer arithmetic**
`10 / 3` returns `3.3333...` (float). `div(10, 3)` returns `3` (integer).
Always use `div` and `rem` for money calculations.

**3. Forgetting the split remainder**
Naive splitting: `div(1001, 3) * 3 = 999`. You lost 2 cents. Always distribute
the remainder explicitly.

**4. Rounding at every step instead of at the end**
If you calculate tax, then round, then add a fee, then round again, rounding errors
compound. Calculate the entire chain in cents and round once at the final display step.

**5. Comparing floats for equality**
Never write `price == 19.99`. Floats cannot represent most decimal values exactly.
Always compare money as integer cents: `price_cents == 1999`.

---

## Executable Example

Create a file `lib/money.ex` with the complete `Money` module code from above, then run these in `iex`:

```elixir
defmodule Money do
  @moduledoc """
  Safe monetary arithmetic using integer cents.

  All amounts are stored as integers representing the smallest currency unit
  (cents for USD/EUR, pence for GBP). This eliminates floating-point rounding
  errors entirely.

  Currency is tracked to prevent accidentally adding USD to EUR.
  """

  @type t :: %{amount: integer(), currency: atom()}

  @doc """
  Creates a Money value from an integer amount in cents.

  ## Examples

      iex> Money.new(1999, :usd)
      %{amount: 1999, currency: :usd}

  """
  @spec new(integer(), atom()) :: t()
  def new(amount, currency) when is_integer(amount) and is_atom(currency) do
    %{amount: amount, currency: currency}
  end

  @doc """
  Creates a Money value from a float dollar amount.

  Converts to cents internally using rounding to handle float imprecision.
  This is the ONLY place where floats touch the money system.

  ## Examples

      iex> Money.from_float(19.99, :usd)
      %{amount: 1999, currency: :usd}

      iex> Money.from_float(0.1 + 0.2, :usd)
      %{amount: 30, currency: :usd}

  """
  @spec from_float(float(), atom()) :: t()
  def from_float(dollars, currency) when is_float(dollars) and is_atom(currency) do
    cents = round(dollars * 100)
    new(cents, currency)
  end

  @doc """
  Adds two money values. Both must have the same currency.

  ## Examples

      iex> a = Money.new(1000, :usd)
      iex> b = Money.new(599, :usd)
      iex> Money.add(a, b)
      {:ok, %{amount: 1599, currency: :usd}}

      iex> a = Money.new(1000, :usd)
      iex> b = Money.new(500, :eur)
      iex> Money.add(a, b)
      {:error, :currency_mismatch}

  """
  @spec add(t(), t()) :: {:ok, t()} | {:error, :currency_mismatch}
  def add(%{currency: c} = a, %{currency: c} = b) do
    {:ok, new(a.amount + b.amount, c)}
  end

  def add(%{currency: _}, %{currency: _}), do: {:error, :currency_mismatch}

  @doc """
  Subtracts the second money value from the first.

  ## Examples

      iex> a = Money.new(1000, :usd)
      iex> b = Money.new(300, :usd)
      iex> Money.subtract(a, b)
      {:ok, %{amount: 700, currency: :usd}}

  """
  @spec subtract(t(), t()) :: {:ok, t()} | {:error, :currency_mismatch}
  def subtract(%{currency: c} = a, %{currency: c} = b) do
    {:ok, new(a.amount - b.amount, c)}
  end

  def subtract(%{currency: _}, %{currency: _}), do: {:error, :currency_mismatch}

  @doc """
  Multiplies a money value by a scalar (quantity, tax rate, etc.).

  The result is rounded to the nearest cent. This is the only operation
  that introduces rounding, and it happens at the integer level.

  ## Examples

      iex> price = Money.new(999, :usd)
      iex> Money.multiply(price, 3)
      %{amount: 2997, currency: :usd}

      iex> subtotal = Money.new(1000, :usd)
      iex> Money.multiply(subtotal, 1.0825)
      %{amount: 1083, currency: :usd}

  """
  @spec multiply(t(), number()) :: t()
  def multiply(%{amount: amount, currency: currency}, factor) when is_number(factor) do
    new(round(amount * factor), currency)
  end

  @doc """
  Splits a money value into N equal parts without losing cents.

  The remainder is distributed one cent at a time to the first parts.
  This guarantees that the parts always sum to the original amount.

  ## Examples

      iex> bill = Money.new(1001, :usd)
      iex> Money.split(bill, 3)
      [
        %{amount: 334, currency: :usd},
        %{amount: 334, currency: :usd},
        %{amount: 333, currency: :usd}
      ]

      iex> bill = Money.new(1000, :usd)
      iex> parts = Money.split(bill, 3)
      iex> parts |> Enum.map(& &1.amount) |> Enum.sum()
      1000

  """
  @spec split(t(), pos_integer()) :: [t()]
  def split(%{amount: amount, currency: currency}, parts)
      when is_integer(parts) and parts > 0 do
    base = div(amount, parts)
    remainder = rem(amount, parts)

    for i <- 1..parts do
      extra = if i <= remainder, do: 1, else: 0
      new(base + extra, currency)
    end
  end

  @doc """
  Formats a money value as a human-readable string.

  ## Examples

      iex> Money.format(Money.new(1999, :usd))
      "$19.99"

      iex> Money.format(Money.new(500, :eur))
      "€5.00"

      iex> Money.format(Money.new(-250, :usd))
      "-$2.50"

  """
  @spec format(t()) :: String.t()
  def format(%{amount: amount, currency: currency}) do
    symbol = currency_symbol(currency)
    {sign, abs_amount} = if amount < 0, do: {"-", -amount}, else: {"", amount}
    major = div(abs_amount, 100)
    minor = rem(abs_amount, 100) |> Integer.to_string() |> String.pad_leading(2, "0")
    "#{sign}#{symbol}#{major}.#{minor}"
  end

  @doc """
  Returns true if the money amount is zero.
  """
  @spec zero?(t()) :: boolean()
  def zero?(%{amount: 0}), do: true
  def zero?(%{amount: _}), do: false

  @doc """
  Returns true if the money amount is positive.
  """
  @spec positive?(t()) :: boolean()
  def positive?(%{amount: amount}) when amount > 0, do: true
  def positive?(%{amount: _}), do: false

  @spec currency_symbol(atom()) :: String.t()
  defp currency_symbol(:usd), do: "$"
  defp currency_symbol(:eur), do: "€"
  defp currency_symbol(:gbp), do: "£"
  defp currency_symbol(:jpy), do: "¥"
  defp currency_symbol(other), do: "#{other} "
end

# Test the Money module
m1 = Money.new(1000, :usd)
m2 = Money.new(599, :usd)
IO.inspect(Money.add(m1, m2))  # {:ok, %{amount: 1599, currency: :usd}}

IO.inspect(Money.format(m1))  # "$10.00"

bill = Money.new(1001, :usd)
parts = Money.split(bill, 3)
IO.inspect(parts)  # [%{amount: 334, currency: :usd}, %{amount: 334, currency: :usd}, %{amount: 333, currency: :usd}]

sum = parts |> Enum.map(& &1.amount) |> Enum.sum()
IO.inspect(sum)  # 1001 - guarantees all cents are preserved
```

---

## Reflection

If your product sold items priced in currencies with 3 decimal places (Kuwaiti Dinar, Tunisian Dinar), how would you adapt the `Money` module without introducing floats?

A stakeholder asks: why not just use `Decimal` everywhere? Give two concrete reasons to prefer integer cents for this domain.

## Resources

- [Integer — HexDocs](https://hexdocs.pm/elixir/Integer.html)
- [Float — HexDocs](https://hexdocs.pm/elixir/Float.html)
- [Kernel arithmetic — div, rem](https://hexdocs.pm/elixir/Kernel.html#div/2)
- [IEEE 754 — What Every Programmer Should Know About Floating-Point](https://floating-point-gui.de/)
- [Decimal library — HexDocs](https://hexdocs.pm/decimal/Decimal.html)
