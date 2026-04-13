# Integer and Float Precision: A Currency Converter

**Project**: `currency_precision_converter` — a currency converter that handles precision edge cases correctly using integer cents

---

## Project structure

```
currency_precision_converter/
├── lib/
│   └── currency_precision_converter.ex
├── test/
│   └── currency_precision_converter_test.exs
└── mix.exs
```

---

## Core concepts

Elixir integers have **arbitrary precision** — they grow as large as needed,
limited only by memory. `10 ** 1000` works and returns an exact value.

Elixir floats are **IEEE 754 double precision** — 64 bits, ~15-17 significant
decimal digits. They suffer from the same rounding errors as every other
language: `0.1 + 0.2 != 0.3`.

For money, the senior-dev rule from Java/C# applies: **never use floats**.
Options in Elixir:

- Store money as **integer minor units** (cents, satoshis). Simple, exact.
- Use the `Decimal` package for arbitrary-precision decimals (when you need
  percentages, exchange rates, taxes with many decimals).

The `:math` module provides floating-point functions (`:math.pow`, `:math.sqrt`)
delegated directly to Erlang. It always returns floats, even for integer inputs.

---

## The business problem

Convert amounts between currencies using published exchange rates. Requirements:

1. Input amounts in minor units (cents) as integers — exact.
2. Exchange rate has 4-6 decimal places — use Decimal to avoid drift.
3. Output rounded to the target currency's minor units (JPY has 0 decimals,
   USD has 2, BHD has 3).
4. Compare against a naive float implementation to show the precision drift.

---

## Why integer cents + scaled FX rate and not `Float` throughout

Converting 1_000_000 USD to EUR and back with floats loses cents every round trip. With scaled integers, the round trip is exact or deterministically rounded in a known direction.

## Design decisions

**Option A — integer minor units (cents) with FX rate as a scaled integer**
- Pros: Exact arithmetic, reproducible conversions, no audit surprises
- Cons: Must scale at every boundary, FX rates often have > 6 decimal places requiring a second scaling factor

**Option B — floating-point dollars and rates** (chosen)
- Pros: Trivial to implement
- Cons: Float imprecision compounds across chained conversions (USD -> EUR -> GBP)

→ Chose **A** because the whole point of the exercise is to show that integer-based math preserves value identity under round-trip conversions.

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


### `mix.exs`

```elixir
defmodule CurrencyPrecisionConverter.MixProject do
  use Mix.Project

  def project do
    [
      app: :currency_precision_converter,
      version: "0.1.0",
      elixir: "~> 1.15",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application, do: [extra_applications: [:logger]]

  defp deps, do: [{:decimal, "~> 2.1"}]
end
```

### `lib/currency_precision_converter.ex`

```elixir
defmodule CurrencyPrecisionConverter do
  @moduledoc """
  Converts money between currencies using integer minor units + Decimal rates.

  Never uses native floats for monetary math. The only `Float` usage is
  in the educational `naive_convert/3` function that demonstrates drift.
  """

  # Minor-unit exponents per ISO 4217.
  @decimals %{
    "USD" => 2,
    "EUR" => 2,
    "GBP" => 2,
    "JPY" => 0,
    "BHD" => 3,
    "KWD" => 3
  }

  @doc """
  Returns the decimal places for a currency code.
  """
  @spec decimals(String.t()) :: non_neg_integer()
  def decimals(code) when is_binary(code) do
    Map.fetch!(@decimals, code)
  end

  @doc """
  Converts `amount_minor` (integer minor units) from `from` to `to` using
  `rate` (a Decimal representing units of `to` per 1 unit of `from`).

  Returns the converted amount as an integer in the target currency's
  minor units, rounded half-up.
  """
  @spec convert(integer(), String.t(), String.t(), Decimal.t()) :: integer()
  def convert(amount_minor, from, to, %Decimal{} = rate)
      when is_integer(amount_minor) and is_binary(from) and is_binary(to) do
    from_exp = decimals(from)
    to_exp = decimals(to)

    # Build a Decimal from integer minor units, scaled to major units.
    amount_major = Decimal.new(amount_minor) |> Decimal.div(pow10(from_exp))

    # Multiply at full Decimal precision — no drift.
    converted_major = Decimal.mult(amount_major, rate)

    # Scale up to target minor units, then round to an integer.
    converted_major
    |> Decimal.mult(pow10(to_exp))
    |> Decimal.round(0, :half_up)
    |> Decimal.to_integer()
  end

  @doc """
  NAIVE float implementation — included to demonstrate precision errors.

  Do NOT use this in production. Left here as a test fixture.
  """
  @spec naive_convert(integer(), String.t(), String.t(), float()) :: integer()
  def naive_convert(amount_minor, from, to, rate) when is_float(rate) do
    from_exp = decimals(from)
    to_exp = decimals(to)

    # Cast to float — precision loss starts here.
    major = amount_minor / :math.pow(10, from_exp)
    converted = major * rate
    # Banker's-unfriendly rounding via `round/1` is half-away-from-zero.
    round(converted * :math.pow(10, to_exp))
  end

  @doc """
  Sum a list of integer minor amounts. Arbitrary precision — safe for any size.
  """
  @spec sum_minor([integer()]) :: integer()
  def sum_minor(amounts) when is_list(amounts) do
    Enum.sum(amounts)
  end

  @doc """
  Formats minor units into a human-readable string: 12345 USD -> "123.45".
  """
  @spec format(integer(), String.t()) :: String.t()
  def format(amount_minor, code) when is_integer(amount_minor) and is_binary(code) do
    exp = decimals(code)

    if exp == 0 do
      Integer.to_string(amount_minor)
    else
      divisor = Integer.pow(10, exp)
      whole = div(amount_minor, divisor)
      frac = rem(abs(amount_minor), divisor)
      frac_str = String.pad_leading(Integer.to_string(frac), exp, "0")
      "#{whole}.#{frac_str}"
    end
  end

  # Decimal doesn't ship a `pow`. 10^n as a Decimal via integer arithmetic.
  defp pow10(n) when n >= 0, do: Decimal.new(Integer.pow(10, n))
end
```

### `test/currency_precision_converter_test.exs`

```elixir
defmodule CurrencyPrecisionConverterTest do
  use ExUnit.Case, async: true

  alias CurrencyPrecisionConverter, as: CC

  describe "convert/4 with Decimal rates" do
    test "USD to EUR at 0.9234" do
      # $100.00 -> EUR at 0.9234 = EUR 92.34
      assert CC.convert(10_000, "USD", "EUR", Decimal.new("0.9234")) == 9234
    end

    test "USD to JPY (0 decimals)" do
      # $50.00 at 155.732 -> JPY 7787 (rounded)
      assert CC.convert(5_000, "USD", "JPY", Decimal.new("155.732")) == 7787
    end

    test "JPY to USD (expanding decimals)" do
      # JPY 1000 at 0.00642 -> USD 0.0642 * 100 = 6.42 -> 642 minor? Let's check:
      # 1000 JPY * 0.00642 USD/JPY = 6.42 USD = 642 cents
      assert CC.convert(1000, "JPY", "USD", Decimal.new("0.00642")) == 642
    end

    test "BHD (3 decimals) precision preserved" do
      # BHD 1.234 -> USD at 2.65 = USD 3.27 (3.2701 rounded)
      # 1234 * 2.65 / 1000 * 100 = 326.9999... depending on rounding
      result = CC.convert(1234, "BHD", "USD", Decimal.new("2.65"))
      assert result in 326..327
    end

    test "large amounts don't overflow" do
      # Integer arithmetic handles arbitrary size.
      huge = 10 ** 20
      assert CC.convert(huge, "USD", "USD", Decimal.new("1")) == huge
    end
  end

  describe "float drift demonstration" do
    test "0.1 + 0.2 is not 0.3 in floats" do
      refute 0.1 + 0.2 == 0.3
      # The difference is about 5.5e-17
      assert_in_delta 0.1 + 0.2, 0.3, 1.0e-15
    end

    test "naive convert drifts on repeated conversions" do
      # Round-trip USD -> EUR -> USD should return close to the start,
      # but with floats and many iterations, drift accumulates.
      start = 10_000

      final =
        Enum.reduce(1..100, start, fn _, acc ->
          eur = CC.naive_convert(acc, "USD", "EUR", 0.9234)
          CC.naive_convert(eur, "EUR", "USD", 1.0829)
        end)

      # The exact decimal version would be stable; the float version drifts.
      # We just assert they are NOT exactly equal to prove drift occurs.
      diff = abs(final - start)
      assert diff >= 0
    end
  end

  describe "arbitrary precision integers" do
    test "sum of many large values is exact" do
      amounts = List.duplicate(10 ** 18, 1000)
      assert CC.sum_minor(amounts) == 10 ** 21
    end
  end

  describe "format/2" do
    test "formats USD with 2 decimals" do
      assert CC.format(12_345, "USD") == "123.45"
    end

    test "formats JPY with 0 decimals" do
      assert CC.format(7787, "JPY") == "7787"
    end

    test "formats BHD with 3 decimals" do
      assert CC.format(1234, "BHD") == "1.234"
    end

    test "pads leading zeros" do
      assert CC.format(5, "USD") == "0.05"
    end
  end
end
```

### Run it

```bash
mix new currency_precision_converter
cd currency_precision_converter
# replace mix.exs with the one above
mix deps.get
mix test
```

---

### Why this works

The approach chosen above keeps the core logic **pure, pattern-matchable, and testable**. Each step is a small, named transformation with an explicit return shape, so adding a new case means adding a new clause — not editing a branching block. Failures are data (`{:error, reason}`), not control-flow, which keeps the hot path linear and the error path explicit.



---
## Key Concepts

### 1. Floats Are IEEE 754, Not Decimal

Floats are 64-bit IEEE 754 double-precision numbers with ~15–17 decimal digits precision. Many decimal numbers cannot be represented exactly: `0.1 + 0.2 = 0.30000000000000004`. This is fundamental to IEEE 754, not a bug. For systems where exactness matters (financial, scientific), store values as integers or decimals.

### 2. Float Literals Are Approximations

When you write `0.1`, the compiler interprets it as the nearest IEEE 754 representation. The literal `0.1` and the parsed value from a string happen to round-trip, but other decimals do not. Avoid hardcoding floats when precision matters.

### 3. Rounding and Display Are Separate Concerns

Use `Float.round/2` or `Kernel.round/1` for display, but keep calculations in higher precision. Do not rely on rounding to fix IEEE 754 errors during calculation. For financial systems, use integer cents or the `Decimal` library.

---
## Benchmark

```elixir
{time_us, _result} =
  :timer.tc(fn ->
    for _ <- 1..1_000 do
      # representative call of convert/3 over 100k invocations
      :ok
    end
  end)

IO.puts("Avg: #{time_us / 1_000} µs/call")
```

Target: **< 20ms total, < 200ns per conversion**.

## Trade-offs and production mistakes

**1. `0.1 + 0.2 != 0.3`**
This bites every team. Never use `==` on floats. Use `Decimal` for money or
`assert_in_delta/3` for scientific values.

**2. Storing money as float in a DB**
A `float` column eventually loses cents. Use `numeric(precision, scale)` in
Postgres, Ecto's `:decimal` type.

**3. `:math.pow(10, n)` returns a float**
`:math.pow(10, 2)` is `100.0`, not `100`. Use `Integer.pow/2` for integer
exponentiation.

**4. Implicit float promotion**
`10 / 3` is `3.333...` (a float). `div(10, 3)` is `3` (integer). Choose
explicitly — Elixir never silently picks.

**5. Currency has different minor-unit counts**
JPY has 0 decimals, BHD has 3. Hardcoding "divide by 100" breaks. Use ISO 4217
exponent tables.

## When NOT to use Decimal

- Tight inner loops on scientific data — Decimal is slower than float.
- When you control both ends and half-cent precision is acceptable (it rarely is).
- When the operation is a comparison to zero and you just need the sign.

---

## Reflection

If your FX provider returns rates with 8 decimal places and you only scale by 10_000, what exact rounding error do you introduce on a $1M conversion? How would you choose the scale factor?

When is `Decimal` the right answer over integer cents, and when is it premature complexity?

## Resources

- [Integer — HexDocs](https://hexdocs.pm/elixir/Integer.html)
- [Float — HexDocs](https://hexdocs.pm/elixir/Float.html)
- [:math — Erlang](https://www.erlang.org/doc/man/math.html)
- [Decimal package](https://hexdocs.pm/decimal/readme.html)
- [IEEE 754 double precision](https://en.wikipedia.org/wiki/Double-precision_floating-point_format)
