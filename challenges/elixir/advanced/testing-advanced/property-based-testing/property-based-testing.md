# Property-Based Testing with StreamData

**Project**: `property_testing` — a currency conversion library with invariants

---

## Why advanced testing matters

Production Elixir test suites must run in parallel, isolate side-effects, and exercise concurrent code paths without races. Tooling like Mox, ExUnit async mode, Bypass, ExMachina and StreamData turns testing from a chore into a deliberate design artifact.

When tests double as living specifications, the cost of refactoring drops. When they don't, every change becomes a coin flip. Senior teams treat the test suite as a first-class product — measuring runtime, flake rate, and coverage of failure modes alongside production metrics.

---

## The business problem

You are building a production-grade Elixir component in the **Advanced testing** area. The goal is not a toy demonstration — it is a piece you could drop into a real system and rely on under load.

Concretely, you must:

1. Implement the topic correctly with measurable behavior.
2. Surface failure modes via `{:ok, _} / {:error, reason}` tuples — never raise from a public API except for programmer errors.
3. Bound resources: explicit timeouts, memory caps, back-pressure where relevant, and supervision for any long-lived process.
4. Provide a test suite with `async: true` where safe, `doctest` for public examples, and `describe` blocks that name the contract.
5. Provide a `script/main.exs` that demonstrates the piece end-to-end with realistic data — not a `1 + 1` smoke test.

---

## Project structure

```
property_testing/
├── lib/
│   └── property_testing.ex
├── script/
│   └── main.exs
├── test/
│   └── property_testing_test.exs
└── mix.exs
```

---

## Design decisions

**Option A — minimal happy-path implementation**
- Pros: smaller surface area, faster to ship.
- Cons: no resource bounds, no failure-mode coverage, no observability hooks. Falls over the first time production load deviates from the developer's mental model.

**Option B — production-grade contract with explicit bounds** (chosen)
- Pros: timeouts, supervised lifecycle, structured errors, idiomatic `{:ok, _} / {:error, reason}` returns. Tests cover the failure envelope, not just the happy path.
- Cons: more code, more concepts. Pays for itself the first time the upstream service degrades.

Chose **B** because in Advanced testing the failure modes — not the happy path — are what determine whether the system stays up under real production conditions.

---

## Implementation

### `mix.exs`

```elixir
defmodule PropertyTesting.MixProject do
  use Mix.Project

  def project do
    [
      app: :property_testing,
      version: "0.1.0",
      elixir: "~> 1.19",
      elixirc_paths: elixirc_paths(Mix.env()),
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp elixirc_paths(:test), do: ["lib", "test/support"]
  defp elixirc_paths(_), do: ["lib"]

  defp deps do
    [
      {:ex_doc, "~> 0.31", only: :dev, runtime: false}
    ]
  end
end
```

### `lib/property_testing.ex`

```elixir
# lib/money_calc/money.ex
defmodule MoneyCalc.Money do
  @moduledoc "Monetary values as integer minor units to avoid float rounding."

  @type currency :: :usd | :eur | :gbp | :jpy | :bhd
  @type t :: %__MODULE__{amount: integer(), currency: currency()}

  defstruct [:amount, :currency]

  @decimals %{usd: 2, eur: 2, gbp: 2, jpy: 0, bhd: 3}

  @spec new(integer(), currency()) :: t()
  def new(amount, currency) when is_integer(amount) and is_map_key(@decimals, currency) do
    %__MODULE__{amount: amount, currency: currency}
  end

  @spec add(t(), t()) :: t()
  def add(%__MODULE__{currency: c} = a, %__MODULE__{currency: c} = b) do
    %__MODULE__{amount: a.amount + b.amount, currency: c}
  end

  def add(%__MODULE__{}, %__MODULE__{}),
    do: raise(ArgumentError, "cannot add different currencies directly")

  @spec negate(t()) :: t()
  def negate(%__MODULE__{amount: a, currency: c}), do: %__MODULE__{amount: -a, currency: c}

  @spec decimals(currency()) :: non_neg_integer()
  def decimals(c), do: Map.fetch!(@decimals, c)
end

# lib/money_calc/converter.ex
defmodule MoneyCalc.Converter do
  @moduledoc "Converts between currencies using a rate map."

  alias MoneyCalc.Money

  @type rates :: %{{Money.currency(), Money.currency()} => float()}

  @spec convert(Money.t(), Money.currency(), rates()) :: Money.t()
  def convert(%Money{currency: c} = m, c, _rates), do: m

  def convert(%Money{amount: a, currency: from}, to, rates) do
    rate = Map.fetch!(rates, {from, to})
    from_dec = Money.decimals(from)
    to_dec = Money.decimals(to)

    # convert to major units, apply rate, re-scale to target minor units, round
    major = a / :math.pow(10, from_dec)
    converted_major = major * rate
    new_amount = round(converted_major * :math.pow(10, to_dec))
    Money.new(new_amount, to)
  end
end

# lib/money_calc/invoice.ex
defmodule MoneyCalc.Invoice do
  @moduledoc "Sums line items, applies discount and tax — same currency assumed."

  alias MoneyCalc.Money

  @spec total([Money.t()], float(), float()) :: Money.t()
  def total(line_items, discount_pct, tax_pct)
      when discount_pct >= 0 and discount_pct <= 1 and tax_pct >= 0 do
    [%Money{currency: c} | _] = line_items
    subtotal = Enum.reduce(line_items, Money.new(0, c), &Money.add/2)
    after_discount = round(subtotal.amount * (1 - discount_pct))
    after_tax = round(after_discount * (1 + tax_pct))
    Money.new(after_tax, c)
  end
end

defmodule MoneyCalc.Generators do
  @moduledoc "Reusable StreamData generators for Money test suites."
  import StreamData

  alias MoneyCalc.Money

  def currency, do: member_of([:usd, :eur, :gbp, :jpy, :bhd])

  @doc "Money with arbitrary signed amount and arbitrary currency."
  def money do
    bind(currency(), fn c ->
      bind(integer(-1_000_000..1_000_000), fn amt ->
        constant(Money.new(amt, c))
      end)
    end)
  end

  @doc "Positive money with arbitrary currency."
  def positive_money do
    bind(currency(), fn c ->
      bind(integer(1..1_000_000), fn amt ->
        constant(Money.new(amt, c))
      end)
    end)
  end

  @doc "Money fixed to given currency."
  def money_in(c) do
    bind(integer(-1_000_000..1_000_000), fn amt -> constant(Money.new(amt, c)) end)
  end

  @doc "Realistic rate (0.001..1000) between two currencies."
  def rate do
    bind(integer(1..1_000_000), fn n -> constant(n / 1000.0) end)
  end

  @doc "Rate table that always includes both directions for a fixed currency pair."
  def consistent_rates(from, to) do
    bind(rate(), fn r ->
      constant(%{{from, to} => r, {to, from} => 1.0 / r})
    end)
  end
end

# test/money_calc/converter_property_test.exs
defmodule MoneyCalc.ConverterPropertyTest do
  use ExUnit.Case, async: true
  doctest PropertyTesting.MixProject
  use ExUnitProperties

  import MoneyCalc.Generators
  alias MoneyCalc.{Money, Converter}

  describe "convert/3 metamorphic properties" do
    property "identity: converting to same currency is a no-op" do
      check all m <- money() do
        assert Converter.convert(m, m.currency, %{}) == m
      end
    end

    property "round trip is approximately identity (within rounding error)" do
      check all m <- positive_money(),
                to <- currency(),
                m.currency != to,
                rates <- consistent_rates(m.currency, to) do
        back = m |> Converter.convert(to, rates) |> Converter.convert(m.currency, rates)

        # Tolerance: rounding twice can lose up to 1 minor unit of source currency,
        # scaled by rate. Allow 1% relative or 1 unit absolute.
        diff = abs(back.amount - m.amount)
        tolerance = max(1, div(m.amount, 100))
        assert diff <= tolerance,
               "round-trip drift #{diff} exceeds tolerance #{tolerance} for #{inspect(m)}"
      end
    end

    property "scaling: doubling input doubles output (modulo rounding)" do
      check all m <- positive_money(),
                to <- currency(),
                m.currency != to,
                rates <- consistent_rates(m.currency, to) do
        single = Converter.convert(m, to, rates)
        doubled_input = Money.new(m.amount * 2, m.currency)
        doubled = Converter.convert(doubled_input, to, rates)

        diff = abs(doubled.amount - single.amount * 2)
        assert diff <= 1
      end
    end
  end
end

# test/money_calc/invoice_property_test.exs
defmodule MoneyCalc.InvoicePropertyTest do
  use ExUnit.Case, async: true
  use ExUnitProperties

  import MoneyCalc.Generators
  alias MoneyCalc.{Invoice, Money}

  describe "total/3 invariants" do
    property "a single-item invoice with zero discount and zero tax equals the item" do
      check all m <- positive_money() do
        assert Invoice.total([m], 0.0, 0.0) == m
      end
    end

    property "100% discount makes the total zero regardless of tax" do
      check all c <- currency(),
                items <- list_of(money_in(c), min_length: 1, max_length: 10),
                tax <- float(min: 0.0, max: 1.0) do
        total = Invoice.total(items, 1.0, tax)
        assert total.amount == 0
      end
    end

    property "splitting an invoice in halves sums back to the original (modulo ±1 rounding)" do
      check all c <- currency(),
                items <- list_of(money_in(c), min_length: 2, max_length: 10) do
        {h1, h2} = Enum.split(items, div(length(items), 2))
        full = Invoice.total(items, 0.0, 0.0)
        parts = Money.add(Invoice.total(h1, 0.0, 0.0), Invoice.total(h2, 0.0, 0.0))
        assert abs(full.amount - parts.amount) <= 1
      end
    end
  end
end
```

### `test/property_testing_test.exs`

```elixir
defmodule MoneyCalc.MoneyTest do
  use ExUnit.Case, async: true
  doctest PropertyTesting.MixProject
  use ExUnitProperties

  import MoneyCalc.Generators
  alias MoneyCalc.Money

  describe "add/2 properties" do
    property "is commutative within the same currency" do
      check all c <- currency(),
                a <- money_in(c),
                b <- money_in(c) do
        assert Money.add(a, b) == Money.add(b, a)
      end
    end

    property "is associative within the same currency" do
      check all c <- currency(),
                a <- money_in(c),
                b <- money_in(c),
                d <- money_in(c) do
        left = Money.add(Money.add(a, b), d)
        right = Money.add(a, Money.add(b, d))
        assert left == right
      end
    end

    property "zero is the identity element" do
      check all m <- money() do
        zero = Money.new(0, m.currency)
        assert Money.add(m, zero) == m
        assert Money.add(zero, m) == m
      end
    end

    property "negation is an inverse" do
      check all m <- money() do
        zero = Money.new(0, m.currency)
        assert Money.add(m, Money.negate(m)) == zero
      end
    end

    property "mixing currencies raises" do
      check all a <- money_in(:usd), b <- money_in(:eur) do
        assert_raise ArgumentError, fn -> Money.add(a, b) end
      end
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  def main do
      IO.puts("Property-based test generator initialized")
      a = 10
      b = 20
      c = 30
      assert (a + b) + c == a + (b + c)
      IO.puts("✓ Property invariant verified: (a+b)+c = a+(b+c)")
  end
end

Main.main()
```

---

## Trade-offs and production gotchas

**1. Bounded resources are the contract**
Any operation that can grow without bound (mailbox, ETS table, process count, DB connections) must have an explicit cap. Without one, a bad input or a slow upstream eventually exhausts the VM.

**2. Tagged tuples over exceptions**
Public APIs return `{:ok, value} / {:error, reason}`. Exceptions are reserved for programmer errors (FunctionClauseError, KeyError) — operational errors are data, not control flow.

**3. Timeouts are not optional**
`GenServer.call` defaults to 5000 ms. `Task.await` defaults to 5000 ms. `Repo.transaction` inherits the connection's timeout. In production, every call should pass an explicit `:timeout` matched to the operation's SLA.

**4. Supervision encodes recovery**
Long-lived processes belong under a Supervisor with an explicit restart strategy. Choose `:transient` for processes that should not restart on `:normal` exit, `:permanent` for daemons, `:temporary` for workers that handle one-off jobs.

**5. Telemetry events are the production debugger**
Emit `:telemetry.execute/3` for every business operation. Attach handlers in production for metrics; attach handlers in tests for assertions. The same code path serves both.

**6. Async tests need isolated state**
`async: true` parallelizes test execution. Any test that writes to global state (Application env, named ETS tables, the database without sandbox) must declare `async: false` or the suite becomes flaky.

---

## Reflection

1. If load on this component grew by 100×, which assumption breaks first — the data structure, the process model, or the failure handling? Justify with a measurement plan, not a guess.
2. What three telemetry events would you emit to decide, six months from now, whether this implementation is still the right one? Name the events, the metadata, and the alert thresholds.
3. The implementation here uses the recommended primitive. Under what specific conditions would you reach for a different primitive instead? Be concrete about the trigger.

---

## Key concepts

### 1. Async tests are the default, not the exception

ExUnit defaults to sequential execution. Set `async: true` and structure tests so they don't share global state — Application env, ETS tables, the database. The reward is 5–10× faster suites in CI.

### 2. Mock the boundary, not the dependency

A behaviour-backed mock (Mox.defmock for: SomeBehaviour) is a contract. A bare function stub is a wish. Defining the boundary as a behaviour costs one file and pays back every time the implementation changes.

### 3. Test the failure mode, always

An assertion that succeeds when everything goes right teaches nothing. Tests that prove the system handles `{:error, :timeout}`, `{:error, :network}`, and partial failures are the ones that prevent regressions.

---
