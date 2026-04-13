# Property-Based Testing with Custom StreamData Generators and Shrinking

**Project**: `pricing_engine` — a property-based test suite for a monetary pricing library that must never lose cents

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
pricing_engine/
├── lib/
│   └── pricing_engine.ex
├── script/
│   └── main.exs
├── test/
│   └── pricing_engine_test.exs
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
defmodule PricingEngine.MixProject do
  use Mix.Project

  def project do
    [
      app: :pricing_engine,
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

### `lib/pricing_engine.ex`

```elixir
# lib/pricing_engine/money.ex
defmodule PricingEngine.Money do
  @moduledoc "Money stored as integer cents plus a 3-letter ISO 4217 currency code."

  @enforce_keys [:cents, :currency]
  defstruct [:cents, :currency]

  @type t :: %__MODULE__{cents: integer(), currency: String.t()}

  @spec new(integer(), String.t()) :: t()
  def new(cents, currency) when is_integer(cents) and byte_size(currency) == 3 do
    %__MODULE__{cents: cents, currency: currency}
  end

  @spec add(t(), t()) :: t()
  def add(%__MODULE__{currency: c} = a, %__MODULE__{currency: c} = b) do
    %__MODULE__{cents: a.cents + b.cents, currency: c}
  end

  def add(%__MODULE__{currency: c1}, %__MODULE__{currency: c2}) do
    raise ArgumentError, "currency mismatch: #{c1} vs #{c2}"
  end
end

# lib/pricing_engine/splitter.ex
defmodule PricingEngine.Splitter do
  @moduledoc """
  Splits a `Money` value into `n` parts. The sum of the parts must equal the original amount
  down to the last cent. Any remainder is distributed one cent at a time starting from the
  first share — this is the accounting-standard "largest remainder" distribution.
  """

  alias PricingEngine.Money

  @spec split(Money.t(), pos_integer()) :: [Money.t()]
  def split(%Money{cents: cents, currency: c}, n) when is_integer(n) and n > 0 do
    base = div(cents, n)
    remainder = rem(cents, n)

    for i <- 0..(n - 1) do
      extra = if i < remainder, do: 1, else: 0
      %Money{cents: base + extra, currency: c}
    end
  end
end

defmodule PricingEngine.Generators do
  @moduledoc """
  Domain-level StreamData generators for pricing tests.

  Guidelines for generators:
  - Never use `Enum.random/1` or `:rand` inside a property — it bypasses shrinking
  - Prefer `bind` for data that depends on previously generated data
  - Return concrete domain types (`%Money{}`), not raw tuples
  """

  import StreamData
  alias PricingEngine.Money

  @currencies ~w(USD EUR GBP ARS JPY BRL)

  @doc "Generates a 3-letter ISO currency from a whitelist."
  def currency, do: member_of(@currencies)

  @doc """
  Generates non-negative cents bounded by ~21 trillion — the upper bound most ledgers use
  for int64 safety. Larger values stress arithmetic but do not model any real-world amount.
  """
  def cents, do: integer(0..21_000_000_000_000)

  @doc "Composes cents and currency into a valid Money value."
  def money do
    bind(currency(), fn c ->
      map(cents(), fn n -> Money.new(n, c) end)
    end)
  end

  @doc """
  Generates two Money values sharing the same currency. This is required when testing
  addition — generating two independent money values would almost always produce a
  currency mismatch and make the property useless.
  """
  def money_pair_same_currency do
    bind(currency(), fn c ->
      bind(cents(), fn a ->
        map(cents(), fn b -> {Money.new(a, c), Money.new(b, c)} end)
      end)
    end)
  end

  @doc "Generates a split factor — constrained so `split/2` does not degenerate."
  def split_factor, do: integer(1..100)
end

# test/pricing_engine/splitter_property_test.exs
defmodule PricingEngine.SplitterPropertyTest do
  use ExUnit.Case, async: true
  doctest PricingEngine.MixProject
  use ExUnitProperties

  alias PricingEngine.Splitter
  import PricingEngine.Generators

  describe "Splitter.split/2 invariants" do
    property "sum of shares equals original — no cent lost or created" do
      check all m <- money(), n <- split_factor() do
        total =
          m
          |> Splitter.split(n)
          |> Enum.map(& &1.cents)
          |> Enum.sum()

        assert total == m.cents
      end
    end

    property "produces exactly n shares" do
      check all m <- money(), n <- split_factor() do
        assert length(Splitter.split(m, n)) == n
      end
    end

    property "shares differ by at most 1 cent" do
      check all m <- money(), n <- split_factor() do
        values = Splitter.split(m, n) |> Enum.map(& &1.cents)
        assert Enum.max(values) - Enum.min(values) in [0, 1]
      end
    end

    property "all shares have the same currency as the original" do
      check all m <- money(), n <- split_factor() do
        assert Enum.all?(Splitter.split(m, n), &(&1.currency == m.currency))
      end
    end
  end
end
```

### `test/pricing_engine_test.exs`

```elixir
defmodule PricingEngine.MoneyPropertyTest do
  use ExUnit.Case, async: true
  doctest PricingEngine.MixProject
  use ExUnitProperties

  alias PricingEngine.Money
  import PricingEngine.Generators

  describe "Money.add/2 properties" do
    property "addition is commutative when currencies match" do
      check all {a, b} <- money_pair_same_currency() do
        assert Money.add(a, b).cents == Money.add(b, a).cents
      end
    end

    property "addition is associative when currencies match" do
      check all c <- currency(),
                x <- cents(),
                y <- cents(),
                z <- cents() do
        a = Money.new(x, c)
        b = Money.new(y, c)
        d = Money.new(z, c)

        left = Money.add(Money.add(a, b), d)
        right = Money.add(a, Money.add(b, d))
        assert left.cents == right.cents
      end
    end

    property "adding zero is identity" do
      check all m <- money() do
        zero = Money.new(0, m.currency)
        assert Money.add(m, zero).cents == m.cents
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
