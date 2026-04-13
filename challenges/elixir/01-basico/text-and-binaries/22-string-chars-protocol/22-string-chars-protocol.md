# String.Chars and Inspect Protocols: Domain Types That Display Themselves

**Project**: `domain_display` — domain value objects (Money, Email, UserId) with custom rendering

**Difficulty**: ★★☆☆☆
**Estimated time**: 2 hours

---

## Why protocols matter for a senior developer

Protocols are Elixir's mechanism for polymorphism. A protocol defines a contract;
implementations provide the behavior for specific data types. Two protocols are
unavoidable in real systems:

- `String.Chars` — controls what `to_string/1` and `"#{value}"` interpolation produce
- `Inspect` — controls what `inspect/1` (and `IO.inspect/1`, logs, `iex` output) show

If your domain types don't implement these, you get the default: either a crash
(for `String.Chars` on a struct) or the internal representation (`%MyApp.Money{cents: 1999, currency: "EUR"}`),
which leaks implementation details into user-facing logs and breaks when the struct changes.

Understanding these protocols matters when you need to:

- Keep audit logs readable: `user(42)` instead of `%UserId{value: 42}`
- Prevent accidental secret leaks: implement `Inspect` for tokens/passwords to show `[REDACTED]`
- Support direct interpolation: `"Total: #{money}"` without manual formatting everywhere
- Build domain primitives that behave correctly everywhere in the system

---

## The business problem

Your team's logs are a mess. Every log line for a payment shows:

```
%MyApp.Money{__meta__: ..., cents: 1999, currency: "EUR", ...}
```

When a developer pastes that into a bug ticket, sensitive data leaks and the
message is twice as long as needed. Worse, `"Charged #{money}"` raises
`Protocol.UndefinedError`.

You need domain value objects that:

1. Print nicely in interpolation (`"€19.99"`, `"user(42)"`, `"a***@example.com"`)
2. Display safely in logs and `iex` (no secrets, no internal fields)
3. Are immutable and validated at construction
4. Can be compared and serialized predictably

---

## Project structure

```
domain_display/
├── lib/
│   └── domain_display/
│       ├── money.ex
│       ├── email.ex
│       └── user_id.ex
├── test/
│   └── domain_display/
│       ├── money_test.exs
│       ├── email_test.exs
│       └── user_id_test.exs
└── mix.exs
```

---

## `String.Chars` vs `Inspect` — when to implement which

They solve different problems:

| Scenario | Protocol | Example |
|----------|----------|---------|
| `"#{value}"` interpolation | `String.Chars` | `"Total: #{money}" → "Total: €19.99"` |
| `to_string(value)` | `String.Chars` | user-facing strings, HTML, emails |
| `IO.inspect(value)` | `Inspect` | debugging, logging |
| `Logger.info("#{value}")` | `String.Chars` | log message interpolation |
| `Logger.info("got #{inspect(value)}")` | `Inspect` | explicit debug dump |
| `iex` REPL output | `Inspect` | interactive exploration |

A rule that works 90% of the time: implement `Inspect` for every domain struct
(so logs are sane), implement `String.Chars` only when the value has an
unambiguous user-facing representation.

---

## Implementation

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Standard library: no external dependencies required
  ]
end
```


### Step 1: Create the project

**Objective**: Protocols dispatch on type; implement Inspect for debugging/logging of domain types without leaking internals.

```bash
mix new domain_display
cd domain_display
```

### Step 2: `mix.exs`

**Objective**: Boilerplate; focus on testing custom Inspect output — it's hard to debug without it.

```elixir
defmodule DomainDisplay.MixProject do
  use Mix.Project

  def project do
    [
      app: :domain_display,
      version: "0.1.0",
      elixir: "~> 1.17",
      start_permanent: Mix.env() == :prod,
      deps: []
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end
end
```

### Step 3: `lib/domain_display/money.ex`

**Objective**: Money is opaque; avoid string manipulation — cents + ISO atom prevents rounding errors and silly bugs.

```elixir
defmodule DomainDisplay.Money do
  @moduledoc """
  A monetary amount as integer cents plus a 3-letter ISO currency code.

  Money is stored in the smallest unit (cents) to avoid the float arithmetic
  drift that plagues naive implementations. 0.1 + 0.2 != 0.3 in IEEE 754;
  10 + 20 always equals 30.
  """

  @enforce_keys [:cents, :currency]
  defstruct [:cents, :currency]

  @type t :: %__MODULE__{cents: integer(), currency: String.t()}

  @supported_currencies ~w(EUR USD GBP JPY)

  @spec new(integer(), String.t()) :: {:ok, t()} | {:error, atom()}
  def new(cents, currency) when is_integer(cents) and is_binary(currency) do
    if currency in @supported_currencies do
      {:ok, %__MODULE__{cents: cents, currency: currency}}
    else
      {:error, :unsupported_currency}
    end
  end

  def new(_cents, _currency), do: {:error, :invalid_input}

  @spec new!(integer(), String.t()) :: t()
  def new!(cents, currency) do
    case new(cents, currency) do
      {:ok, money} -> money
      {:error, reason} -> raise ArgumentError, "invalid money: #{reason}"
    end
  end
end

defimpl String.Chars, for: DomainDisplay.Money do
  @doc """
  User-facing formatting: "€19.99", "$1,234.50".

  JPY has no subunit so it formats without decimals. Thousand separators are
  added with a simple regex — for real i18n you would use a library like `cldr`.
  """
  def to_string(%DomainDisplay.Money{cents: cents, currency: "JPY"}) do
    symbol = "¥"
    symbol <> format_int(cents)
  end

  def to_string(%DomainDisplay.Money{cents: cents, currency: currency}) do
    symbol = symbol_for(currency)
    {whole, frac} = {div(cents, 100), rem(abs(cents), 100)}
    sign = if cents < 0, do: "-", else: ""

    "#{sign}#{symbol}#{format_int(abs(whole))}.#{pad2(frac)}"
  end

  defp symbol_for("EUR"), do: "€"
  defp symbol_for("USD"), do: "$"
  defp symbol_for("GBP"), do: "£"

  defp format_int(n) do
    n
    |> Integer.to_string()
    |> String.reverse()
    |> String.replace(~r/(\d{3})(?=\d)/, "\\1,")
    |> String.reverse()
  end

  defp pad2(n) when n < 10, do: "0#{n}"
  defp pad2(n), do: "#{n}"
end

defimpl Inspect, for: DomainDisplay.Money do
  @doc """
  In logs and iex, use the same human-readable form wrapped in `#Money<...>`.

  The `#Name<...>` convention is the community standard for custom Inspect —
  it signals "this is a struct, rendered custom" so readers know it's not raw
  data. See `#Reference<...>`, `#PID<...>`, `#Function<...>` for prior art.
  """
  def inspect(money, _opts) do
    "#Money<" <> to_string(money) <> ">"
  end
end
```

### Step 4: `lib/domain_display/email.ex`

**Objective**: Domain types hide parsing logic; downstreams don't parse/validate again — single source of truth.

```elixir
defmodule DomainDisplay.Email do
  @moduledoc """
  An email address. The full value is preserved internally, but `Inspect`
  masks the local part so emails never leak into logs in plain text.

  This is a GDPR/PII pattern: the data is available to code that explicitly
  calls `to_string/1`, but accidental `IO.inspect` or Logger interpolation
  never dumps raw addresses.
  """

  @enforce_keys [:value]
  defstruct [:value]

  @type t :: %__MODULE__{value: String.t()}

  @email_regex ~r/^[^\s@]+@[^\s@]+\.[^\s@]+$/

  @spec new(String.t()) :: {:ok, t()} | {:error, :invalid_email}
  def new(value) when is_binary(value) do
    if Regex.match?(@email_regex, value) do
      {:ok, %__MODULE__{value: value}}
    else
      {:error, :invalid_email}
    end
  end

  def new(_), do: {:error, :invalid_email}

  @doc "The raw email. Callers that need the full address call this explicitly."
  @spec to_plain(t()) :: String.t()
  def to_plain(%__MODULE__{value: value}), do: value

  @doc "Masked form safe for logs: `j***@example.com`."
  @spec masked(t()) :: String.t()
  def masked(%__MODULE__{value: value}) do
    [local, domain] = String.split(value, "@", parts: 2)
    first = String.first(local) || ""
    "#{first}***@#{domain}"
  end
end

defimpl String.Chars, for: DomainDisplay.Email do
  # Interpolation returns the masked form. Code that wants the raw value must
  # use `Email.to_plain/1` explicitly — opt-in, not opt-out.
  def to_string(email), do: DomainDisplay.Email.masked(email)
end

defimpl Inspect, for: DomainDisplay.Email do
  def inspect(email, _opts) do
    "#Email<#{DomainDisplay.Email.masked(email)}>"
  end
end
```

### Step 5: `lib/domain_display/user_id.ex`

**Objective**: NewType pattern (struct wrapping a single field) makes passing wrong ID type a compile-time type error.

```elixir
defmodule DomainDisplay.UserId do
  @moduledoc """
  A strongly-typed user identifier. Wrapping an integer in a struct prevents
  accidental mixing with other integer IDs (order IDs, product IDs, etc.) — a
  common source of bugs in systems that pass raw integers around.
  """

  @enforce_keys [:value]
  defstruct [:value]

  @type t :: %__MODULE__{value: pos_integer()}

  @spec new(pos_integer()) :: {:ok, t()} | {:error, :invalid_id}
  def new(value) when is_integer(value) and value > 0 do
    {:ok, %__MODULE__{value: value}}
  end

  def new(_), do: {:error, :invalid_id}

  @spec value(t()) :: pos_integer()
  def value(%__MODULE__{value: v}), do: v
end

defimpl String.Chars, for: DomainDisplay.UserId do
  def to_string(%DomainDisplay.UserId{value: v}), do: "user(#{v})"
end

defimpl Inspect, for: DomainDisplay.UserId do
  def inspect(%DomainDisplay.UserId{value: v}, _opts), do: "#UserId<#{v}>"
end
```

### Step 6: Tests

**Objective**: Test Inspect protocol output — it's the only UI users of your type see when debugging.

```elixir
# test/domain_display/money_test.exs
defmodule DomainDisplay.MoneyTest do
  use ExUnit.Case, async: true

  alias DomainDisplay.Money

  describe "new/2" do
    test "builds a valid money struct" do
      assert {:ok, %Money{cents: 1999, currency: "EUR"}} = Money.new(1999, "EUR")
    end

    test "rejects unsupported currency" do
      assert {:error, :unsupported_currency} = Money.new(100, "XYZ")
    end

    test "rejects invalid types" do
      assert {:error, :invalid_input} = Money.new("100", "EUR")
    end
  end

  describe "String.Chars — interpolation" do
    test "EUR with subunits" do
      {:ok, m} = Money.new(1999, "EUR")
      assert "#{m}" == "€19.99"
    end

    test "USD with thousands separator" do
      {:ok, m} = Money.new(1_234_567, "USD")
      assert "#{m}" == "$12,345.67"
    end

    test "JPY has no decimals" do
      {:ok, m} = Money.new(1250, "JPY")
      assert "#{m}" == "¥1,250"
    end

    test "negative amount" do
      {:ok, m} = Money.new(-500, "EUR")
      assert "#{m}" == "-€5.00"
    end

    test "cents below 10 are zero-padded" do
      {:ok, m} = Money.new(105, "EUR")
      assert "#{m}" == "€1.05"
    end
  end

  describe "Inspect" do
    test "uses the #Money<> convention" do
      {:ok, m} = Money.new(1999, "EUR")
      assert inspect(m) == "#Money<€19.99>"
    end
  end
end
```

```elixir
# test/domain_display/email_test.exs
defmodule DomainDisplay.EmailTest do
  use ExUnit.Case, async: true

  alias DomainDisplay.Email

  describe "new/1" do
    test "accepts a valid email" do
      assert {:ok, %Email{value: "jane@example.com"}} = Email.new("jane@example.com")
    end

    test "rejects malformed input" do
      assert {:error, :invalid_email} = Email.new("not-an-email")
      assert {:error, :invalid_email} = Email.new("@example.com")
      assert {:error, :invalid_email} = Email.new(nil)
    end
  end

  describe "masking behavior" do
    test "interpolation returns masked form" do
      {:ok, email} = Email.new("jane@example.com")
      assert "#{email}" == "j***@example.com"
    end

    test "inspect returns #Email<masked>" do
      {:ok, email} = Email.new("jane@example.com")
      assert inspect(email) == "#Email<j***@example.com>"
    end

    test "to_plain/1 returns the raw value" do
      {:ok, email} = Email.new("jane@example.com")
      assert Email.to_plain(email) == "jane@example.com"
    end
  end
end
```

```elixir
# test/domain_display/user_id_test.exs
defmodule DomainDisplay.UserIdTest do
  use ExUnit.Case, async: true

  alias DomainDisplay.UserId

  test "rejects zero and negative" do
    assert {:error, :invalid_id} = UserId.new(0)
    assert {:error, :invalid_id} = UserId.new(-1)
  end

  test "interpolation reads like a function call" do
    {:ok, uid} = UserId.new(42)
    assert "#{uid}" == "user(42)"
  end

  test "inspect uses the #UserId<> convention" do
    {:ok, uid} = UserId.new(42)
    assert inspect(uid) == "#UserId<42>"
  end

  test "wrapping prevents mixing with raw ints" do
    # The type system enforces this at the function-spec level.
    # Here we just confirm the access path.
    {:ok, uid} = UserId.new(42)
    assert UserId.value(uid) == 42
  end
end
```

### Step 7: Run the tests

**Objective**: --warnings-as-errors finds unused matches in protocols; test coverage validates all clauses fire.

```bash
mix test --trace
```

All tests pass. Open `iex -S mix` and try:

```elixir
{:ok, m} = DomainDisplay.Money.new(1999, "EUR")
m           # shows #Money<€19.99>
"#{m}"      # shows "€19.99"
[m, m]      # list renders as [#Money<€19.99>, #Money<€19.99>]
```

---


## Key Concepts

### 1. The `String.Chars` Protocol Converts Types to Strings
When you interpolate a value in a string, Elixir calls `to_string/1`, which uses the `String.Chars` protocol. Implementing it for your types makes them automatically stringify.

### 2. `String.Chars` vs `Inspect`
`String.Chars` converts to user-friendly strings. `Inspect` converts to Elixir syntax (for debugging). `#{user}` calls `String.Chars`; `IO.inspect(user)` calls `Inspect`.

### 3. Default Implementations
Most types have default `String.Chars` implementations. For custom types, implement the protocol to control how they stringify.

---
## Trade-off analysis

| Aspect | Protocols (this impl) | Behaviours | Plain functions |
|--------|----------------------|------------|-----------------|
| Polymorphism | dispatch by type, automatic | explicit adapter modules | manual `case` per type |
| `"#{value}"` support | yes (`String.Chars`) | no | no |
| `IO.inspect` / logs | clean (`Inspect`) | leaks internals | leaks internals |
| Compile-time safety | consolidation warns on missing impls | callbacks enforced | none |
| Where impls live | in `defimpl` blocks | in adapter modules | anywhere |

When behaviours win: when you need multiple methods that coordinate (e.g., a
`Storage` adapter with `read/1`, `write/2`, `delete/1`). Protocols are for
single-function polymorphism; behaviours are for interfaces with multiple
callbacks.

---

## Common production mistakes

**1. Forgetting `Inspect` for secret-bearing structs**
A `%ApiKey{token: "sk_live_..."}` struct without custom `Inspect` will dump its
token into every Logger line that interpolates with `inspect/1`. Always provide
a redacting `Inspect` for anything carrying a secret:

```elixir
defimpl Inspect, for: MyApp.ApiKey do
  def inspect(_key, _opts), do: "#ApiKey<REDACTED>"
end
```

**2. Implementing `String.Chars` for values that have no single canonical form**
A `Decimal` or a `DateTime` has many reasonable formats (localized, ISO, short,
long). If you implement `String.Chars`, you lock in one choice for the whole
system. When in doubt, don't implement `String.Chars` — require callers to
choose a format explicitly.

**3. `@derive Inspect` with `except:` to hide fields**
This is the easiest win you're not using:

```elixir
defmodule Session do
  @derive {Inspect, except: [:secret_token]}
  defstruct [:user_id, :secret_token, :expires_at]
end
```

You get a free `Inspect` implementation that omits the sensitive field. Consider
this before writing a full `defimpl`.

**4. Protocol consolidation warnings ignored**
In production, `mix compile` consolidates protocols for performance. If you add
new implementations after compilation, they don't take effect until you run
`mix clean` and recompile. CI should run `mix compile --warnings-as-errors` so
consolidation warnings fail the build.

**5. `to_string(nil)` returns `""`**
`String.Chars` is implemented for `nil` and `BitString`. `"#{nil}"` returns `""`,
which can produce confusing empty strings in error messages. If a value might
be `nil`, handle it before interpolation.

---

## When NOT to implement these protocols

- Internal structs never seen by humans (pure data-in-transit between modules)
- Structs whose fields change often — the implementation becomes a maintenance tax
- Cases where `@derive {Inspect, except: [...]}` already solves the problem

---

## Resources

- [Protocols — Elixir Getting Started](https://hexdocs.pm/elixir/protocols.html)
- [`String.Chars` protocol](https://hexdocs.pm/elixir/String.Chars.html)
- [`Inspect` protocol and `Inspect.Algebra`](https://hexdocs.pm/elixir/Inspect.html)
- [`@derive Inspect, except:` pattern](https://hexdocs.pm/elixir/Kernel.html#defstruct/1)
- [Why consolidate protocols — José Valim](https://groups.google.com/g/elixir-lang-core/c/0M6T_u8nk4Y)
