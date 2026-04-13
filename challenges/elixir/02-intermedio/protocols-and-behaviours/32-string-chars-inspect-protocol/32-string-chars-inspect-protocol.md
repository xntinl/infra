# `String.Chars` and `Inspect` — pretty printing a `Money` struct

**Project**: `pretty_money` — a `Money` struct that renders nicely through `to_string/1` and the IEx/`inspect` output.

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
`break`, `group`.

### 4. Human vs developer output — do NOT conflate

`to_string(user)` returning `"Jane Doe"` is great for UIs, terrible for
debugging (you can't tell a `User` from a `String`). `inspect(user)` returning
`#User<id: 42, name: "Jane Doe">` is debuggable. Always implement both, and
keep them different.

---

## Why implement both protocols instead of "one good `to_string`"

**`to_string/1` (via `String.Chars`) only.** You lose the developer/human distinction — your IEx output either lies or looks like a string, which is terrible for debugging.

**`inspect/1` (via `Inspect`) only.** Users interpolating `"#{money}"` get `Protocol.UndefinedError`. Library ergonomics suffer.

**Both (chosen).** `String.Chars` for user-facing interpolation (`"12.34 EUR"`), `Inspect` for developer-facing dumps (`#Money<12.34 EUR>`). The `#Type<...>` convention signals "intentional custom inspect" so readers don't confuse it with the default struct dump.

---

## Design decisions

**Option A — `@derive Inspect` and skip `String.Chars`**
- Pros: Zero handwritten code.
- Cons: Shows raw struct fields (including integer cents) in IEx; interpolation still crashes.

**Option B — Hand-rolled `String.Chars` + hand-rolled `Inspect` using `Inspect.Algebra`** (chosen)
- Pros: Two distinct audiences, two distinct outputs; sensitive fields can be masked in `Inspect`; interpolation just works.
- Cons: Two more small impls to maintain; easy to let them drift if formatting rules change.

→ Chose **B** because the whole point is separating user-facing from developer-facing rendering, and the impls are tiny.

---

### Dependencies (`mix.exs`)

```elixir
def deps do
  [
    {exunit},
    {inspect},
  ]
end
```
## Implementation

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.


```bash
mix new pretty_money
cd pretty_money
```

### Step 2: `lib/money.ex`

**Objective**: Implement `money.ex` — polymorphism via dispatch on the data's type (protocol) or via an explicit contract (behaviour).


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

**Objective**: Write `money_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


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

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
```

### Why this works

Integer cents kill floating-point drift at the boundary (`round(amount * 100)`), so every render is deterministic. `String.Chars` returns the plain human form used by interpolation; `Inspect` wraps it in `#Money<...>` so IEx output is unambiguous. Both share `Money.format_amount/1`, which is the single source of truth for formatting — change it once, both outputs stay consistent.

---


## Key Concepts: String Conversion and Display Protocols

`String.Chars` protocol defines `to_string/1` for any type. Implement it for your custom structs to customize how they display in string contexts. `Inspect` protocol (related but different) defines `inspect/2` for introspection-friendly output (usually more verbose).

Example: a `Money` struct can implement `String.Chars` to return "$100.00", and `Inspect` to return `#Money<value: 10000, currency: :usd>`. This distinction lets you have friendly user-facing strings and detailed debug output.


## Benchmark

<!-- benchmark N/A: string formatting of a small struct — not a meaningful hot path. The interesting complexity is in `Inspect.Algebra` layouts, covered in the custom-algebra exercise. -->

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

## Reflection

- You add a `:metadata` field to `Money` that may contain arbitrary user data (including PII). Which impl must you change to prevent leaking the metadata into IEx and assertion diffs, and what pattern (`@derive {Inspect, except: [...]}` vs hand-rolled) is more maintainable?
- A teammate proposes making `String.Chars` include the currency symbol (`€12.34`) instead of the code (`12.34 EUR`). Which downstream consumers (logs, CSV exports, other services parsing your output) does that break, and how would you stage the change?

---

## Executable Example

Copy the code below into a file (e.g., `solution.exs`) and run with `elixir solution.exs`:

```elixir
defmodule Main do
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

# Demonstrate String.Chars and Inspect protocols
IO.puts("=== Money Protocol Demo ===")

m1 = Money.new(12.34, "EUR")
m2 = Money.new(5, "USD")
m3 = Money.new(0.05, "GBP")

# Test String.Chars (used by to_string/1 and interpolation)
assert to_string(m1) == "12.34 EUR"
assert to_string(m2) == "5.00 USD"
assert to_string(m3) == "0.05 GBP"

# Test Inspect protocol (different format for development)
assert inspect(m1) == "#Money<12.34 EUR>"
assert inspect(m2) == "#Money<5.00 USD>"

# Verify they differ
refute inspect(m1) == to_string(m1)

# Test interpolation
msg = "You paid #{m1} for your purchase"
assert msg == "You paid 12.34 EUR for your purchase"

IO.puts("String.Chars output: #{to_string(m1)}")
IO.puts("Inspect output: #{inspect(m1)}")
IO.puts("Interpolation: #{msg}")
IO.puts("All Money protocol assertions passed!")
end

Main.main()
```


## Resources

- [`String.Chars` — Elixir stdlib](https://hexdocs.pm/elixir/String.Chars.html)
- [`Inspect` — Elixir stdlib](https://hexdocs.pm/elixir/Inspect.html)
- [`Inspect.Algebra`](https://hexdocs.pm/elixir/Inspect.Algebra.html)
- [`ex_money`](https://hexdocs.pm/ex_money/) — a production money library


## Key Concepts

Protocols and behaviors are Elixir's mechanism for ad-hoc and static polymorphism. They solve different problems and are often confused.

**Protocols:**
Dispatch based on the type/struct of the first argument at runtime. A protocol defines a contract (e.g., `Enumerable`); any type can implement it by adding a corresponding implementation block. Protocols excel when you control neither the type nor the caller — e.g., a library that needs to iterate any collection. The fallback is `:any` — if no specific implementation exists, the `:any` handler is tried. This enables "optional" protocol implementations.

**Behaviours:**
Static polymorphism enforced at compile time. A module implements a behavior by defining callbacks (functions). Behaviors are about contracts between modules, not types. Use when you need multiple implementations of the same interface and the caller chooses which to use (e.g., different database adapters, different strategies). Callbacks are checked at compile time — missing a required callback is a compiler error.

**Architectural patterns:**
Behaviors excel in plugin systems (user defines modules conforming to the behavior). Protocols excel in type-driven dispatch (any type can conform). Mix both: a behavior can require that its callbacks operate on types that implement a protocol. Example: `MyAdapter` behavior requiring callbacks that work with `Enumerable` types.
