# `String.Chars` and `Inspect` — pretty printing a `Money` struct

**Project**: `pretty_money` — a `Money` struct that renders nicely through `to_string/1` and the IEx/`inspect` output.

**Difficulty**: ★★☆☆☆
**Estimated time**: 1–2 hours

---

## Project context

Every struct you define eventually shows up in two places: somewhere a human
reads `to_string(value)` or an interpolation (`"the price is #{amount}"`),
and somewhere a developer reads `IO.inspect(value)` or IEx output. Elixir
uses two distinct protocols for these — `String.Chars` for the first,
`Inspect` for the second — and mixing them up is a common source of
surprise.

This exercise builds a tiny `Money` struct and implements both protocols so
that:

- `"cost: #{money}"` shows `cost: 12.34 EUR` (human output).
- `inspect(money)` shows `#Money<12.34 EUR>` (developer output).

Project structure:

```
pretty_money/
├── lib/
│   └── money.ex
├── test/
│   └── money_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `String.Chars` powers interpolation and `to_string/1`

`"x = #{value}"` calls `String.Chars.to_string(value)`. If your type has no
impl, interpolation raises `Protocol.UndefinedError`. Implementing it is how
you opt into "I can appear in a user-facing string".

### 2. `Inspect` powers IEx, `IO.inspect`, and error messages

`Inspect` output is for developers. It should be unambiguous — show the type,
show enough state to debug, and **never lie**. The convention is `#Type<...>`
when the default struct rendering isn't good enough.

### 3. `Inspect.Algebra` for complex layouts

Simple inspect can return a string via `concat/1`. For nested or
configurable output, use `Inspect.Algebra` primitives: `concat`, `nest`,
`break`, `group`. Exercise 80 goes deep on this.

### 4. Human vs developer output — do NOT conflate

`to_string(user)` returning `"Jane Doe"` is great for UIs, terrible for
debugging (you can't tell a `User` from a `String`). `inspect(user)` returning
`#User<id: 42, name: "Jane Doe">` is debuggable. Always implement both, and
keep them different.

---

## Implementation

### Step 1: Create the project

```bash
mix new pretty_money
cd pretty_money
```

### Step 2: `lib/money.ex`

```elixir
defmodule Money do
  @moduledoc """
  A minimal money value with integer-cent precision and a currency code.

  Demonstrates `String.Chars` (for interpolation) and `Inspect` (for
  developer output). Not a real money type — use `ex_money` in production.
  """

  @enforce_keys [:cents, :currency]
  defstruct [:cents, :currency]

  @type t :: %__MODULE__{cents: integer(), currency: String.t()}

  @doc "Build a `Money` from a decimal amount (float) and a currency code."
  @spec new(number(), String.t()) :: t
  def new(amount, currency) when is_number(amount) and is_binary(currency) do
    # Round to integer cents once, at construction — avoids FP drift later.
    %__MODULE__{cents: round(amount * 100), currency: currency}
  end

  @doc "Format as a decimal string (no currency), e.g. `12.34`."
  @spec format_amount(t) :: String.t()
  def format_amount(%__MODULE__{cents: c}) do
    sign = if c < 0, do: "-", else: ""
    abs_c = abs(c)
    whole = div(abs_c, 100)
    frac = rem(abs_c, 100)
    # Pad cents to two digits so 5 cents renders as "0.05", not "0.5".
    "#{sign}#{whole}.#{String.pad_leading(Integer.to_string(frac), 2, "0")}"
  end
end

defimpl String.Chars, for: Money do
  @moduledoc """
  Human-facing: `"paid #{money}"` becomes `"paid 12.34 EUR"`.
  """
  def to_string(%Money{currency: cur} = m) do
    Money.format_amount(m) <> " " <> cur
  end
end

defimpl Inspect, for: Money do
  @moduledoc """
  Developer-facing: `inspect(money)` returns `#Money<12.34 EUR>`. The
  `#Type<...>` convention signals "this is a custom inspect, not the
  default struct dump" so readers know it's intentional.
  """
  import Inspect.Algebra

  def inspect(%Money{currency: cur} = m, _opts) do
    concat(["#Money<", Money.format_amount(m), " ", cur, ">"])
  end
end
```

### Step 3: `test/money_test.exs`

```elixir
defmodule MoneyTest do
  use ExUnit.Case, async: true

  describe "construction and formatting" do
    test "new/2 stores cents as integer" do
      assert %Money{cents: 1234, currency: "EUR"} = Money.new(12.34, "EUR")
    end

    test "format_amount/1 pads cents to two digits" do
      assert Money.format_amount(Money.new(0.05, "USD")) == "0.05"
      assert Money.format_amount(Money.new(7, "USD")) == "7.00"
    end

    test "format_amount/1 renders negatives" do
      assert Money.format_amount(Money.new(-3.50, "USD")) == "-3.50"
    end
  end

  describe "String.Chars (interpolation)" do
    test "interpolation uses human format" do
      assert "#{Money.new(12.34, "EUR")}" == "12.34 EUR"
    end

    test "to_string/1 matches interpolation" do
      m = Money.new(5, "USD")
      assert to_string(m) == "5.00 USD"
    end
  end

  describe "Inspect (developer output)" do
    test "inspect/1 uses the #Money<...> form" do
      assert inspect(Money.new(12.34, "EUR")) == "#Money<12.34 EUR>"
    end

    test "inspect/1 differs from to_string/1" do
      m = Money.new(1, "GBP")
      refute inspect(m) == to_string(m)
    end
  end
end
```

### Step 4: Run

```bash
mix test
```

---

## Trade-offs and production gotchas

**1. `String.Chars` errors are confusing at the call site**
A missing impl manifests as `protocol String.Chars not implemented for %MyStruct{}`
raised from a completely unrelated interpolation — frustrating for users of
your library. Either implement it or document clearly that the type is not
meant to be stringified.

**2. Don't derive `Inspect` when sensitive fields exist**
`@derive Inspect` is convenient, but it will happily print passwords, tokens,
and API keys. Prefer `@derive {Inspect, except: [:password, :token]}`, or
write a hand-rolled impl.

**3. Inspect output is read by humans AND by diffing tools**
ExUnit diffs values via `inspect`. If your inspect is noisy or unstable
(timestamps, random refs), assertions produce unreadable diffs. Keep it
deterministic.

**4. `to_string/1` should not truncate or lie**
`to_string(money)` returning `"$12"` when the actual value is `$12.34` breaks
everything downstream that parses it. Either show the full value or don't
implement the protocol.

**5. When NOT to implement these protocols**
If the value has no sensible text representation (a giant binary, a socket),
don't force one — let callers who need a string build it explicitly. A
missing impl is clearer than a garbage string.

---

## Resources

- [`String.Chars` — Elixir stdlib](https://hexdocs.pm/elixir/String.Chars.html)
- [`Inspect` — Elixir stdlib](https://hexdocs.pm/elixir/Inspect.html)
- [`Inspect.Algebra`](https://hexdocs.pm/elixir/Inspect.Algebra.html)
- [`ex_money`](https://hexdocs.pm/ex_money/) — a production money library
