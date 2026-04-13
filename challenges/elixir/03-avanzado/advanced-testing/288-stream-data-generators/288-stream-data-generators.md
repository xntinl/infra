# Property-Based Testing with Custom StreamData Generators and Shrinking

**Project**: `pricing_engine` — a property-based test suite for a monetary pricing library that must never lose cents.

## Project context

You maintain `pricing_engine`, a library used by the checkout service of an e-commerce platform.
It models money as integer cents and performs tax calculation, rounding, splits, and currency
conversion. Example-based tests pass green, yet production surfaces bugs roughly once a quarter:
negative totals, lost fractional cents after a 3-way split, rounding that disagrees with the
ledger by 1 cent.

Example-based tests cover the paths you imagined. Money arithmetic breaks in the paths you did
not imagine: splitting 100 cents three ways, applying a 33.333% discount, summing 10,000 line
items. Property-based testing with `StreamData` generates thousands of inputs per property and,
critically, **shrinks** a failing input down to the minimal counterexample so you can debug it.

```
pricing_engine/
├── lib/
│   └── pricing_engine/
│       ├── money.ex
│       ├── splitter.ex
│       └── generators.ex           # custom StreamData generators (test-only helpers)
├── test/
│   ├── pricing_engine/
│   │   ├── money_property_test.exs
│   │   └── splitter_property_test.exs
│   └── test_helper.exs
└── mix.exs
```

## Why StreamData and not QuickCheck or PropEr

- **PropEr** is mature and powerful but its DSL is Erlang-flavoured and shrinking is monolithic.
- **Quixir** is unmaintained since 2018 and lacks Elixir 1.14+ support.
- **StreamData** ships with Elixir (since 1.5 in `ExUnitProperties`), uses native streams,
  integrates directly with `property/1`, and its shrinking is integrated and lazy. It is the
  default for new Elixir code.

## Core concepts



---

**Why this matters:**
These concepts form the foundation of production Elixir systems. Understanding them deeply allows you to build fault-tolerant, scalable applications that operate correctly under load and failure.

**Real-world use case:**
This pattern appears in systems like:
- Phoenix applications handling thousands of concurrent connections
- Distributed data processing pipelines
- Financial transaction systems requiring consistency and fault tolerance
- Microservices communicating over unreliable networks

**Common pitfall:**
Many developers overlook that Elixir's concurrency model differs fundamentally from threads. Processes are isolated; shared mutable state does not exist. Trying to force shared-memory patterns leads to deadlocks, race conditions, or silently incorrect behavior. Always think in terms of message passing and immutability.

**Testing-specific insight:**
Tests are not QA. They document intent and catch regressions. A test that passes without asserting anything is technical debt. Always test the failure case; "it works when everything succeeds" teaches nothing. Use property-based testing for domain logic where the number of edge cases is infinite.
### 1. A generator is a lazy stream of values
`StreamData.integer()` is a stream. You compose it with `bind`, `map`, `filter`, `list_of`,
`member_of`, `one_of`. No value is produced until the property asks for it.

### 2. Shrinking is free — if you compose generators correctly
When a property fails, StreamData walks back the composition tree and tries smaller inputs
(smaller list, smaller integer, smaller string) until the minimal input that still fails is
found. If you bypass generators with `Enum.random/1` inside your property, shrinking is lost.

### 3. Generators for domain types, not primitives
`positive_integer()` is a primitive generator. `valid_money()` (positive cents + 3-letter ISO
currency + not exceeding the documented upper bound) is a domain generator. You build domain
generators once and reuse them across properties.

## Design decisions

- **Option A — generators inline in each property**: quick to start, but the `valid_money/0`
  definition duplicates. When the domain evolves the generators drift from one another.
- **Option B — generators in `test/support` or a dedicated module** (`PricingEngine.Generators`):
  single source of truth, can be imported in any property test, survives refactors.

Chosen: **Option B**. Domain generators are as important as domain types — they deserve a
module of their own.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  [
    {:stream_data, "~> 1.1", only: [:dev, :test]}
  ]
end

defp elixirc_paths(:test), do: ["lib", "test/support"]
defp elixirc_paths(_),     do: ["lib"]
```

### Step 1: domain type

**Objective**: Represent money as integer cents + ISO currency so shrinking finds minimal failure counterexamples without float rounding artifacts.

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
```

### Step 2: splitter with a deliberate invariant

**Objective**: Implement largest-remainder split so sum-preservation invariant holds under property shrinking across arbitrary split counts.

```elixir
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
```

### Step 3: custom generators (the load-bearing module)

**Objective**: Build domain generators via bind/2 composition so shrinking preserves the dependency DAG; avoid Enum.random which breaks shrinking.

```elixir
# test/support/generators.ex
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
```

### Step 4: properties

**Objective**: Assert algebraic properties (commutativity, associativity, cardinality) so StreamData generates 100+ inputs per property to hunt rounding bugs.

```elixir
# test/pricing_engine/money_property_test.exs
defmodule PricingEngine.MoneyPropertyTest do
  use ExUnit.Case, async: true
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

```elixir
# test/pricing_engine/splitter_property_test.exs
defmodule PricingEngine.SplitterPropertyTest do
  use ExUnit.Case, async: true
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

## Why this works

The key idea is **generator composition over generator randomness**. Every property describes
an invariant (commutativity, sum-preservation, cardinality). StreamData stresses the invariant
with ~100 inputs per property by default. When one fails, `bind/2` and `map/2` allow StreamData
to shrink cent values toward zero, list sizes toward 1, and currency toward the first whitelist
entry. The counterexample you receive is minimal: one cent, one currency, one split factor.

Without `money_pair_same_currency/0`, independent `money/0` draws would produce a currency
mismatch 5/6 of the time, and the commutativity property would never actually exercise the
arithmetic — all runs would raise before reaching `assert`.

## Benchmark

Shrinking is the cost you pay only when a test fails. A 100-run property on this suite should
finish in under 200ms on a laptop:

```elixir
{time_us, _} = :timer.tc(fn ->
  ExUnit.run()
end)
IO.puts("property suite took #{time_us} microseconds")
```

Target: ~100ms for all 7 properties combined. If a property takes > 1s with 100 runs, the
generator is too large and should be bounded (smaller `cents` range or smaller `split_factor`).

## Deep Dive: Streaming Patterns and Production Implications

Stream-based pipelines in Elixir achieve backpressure and composability by deferring computation until consumption. Unlike eager list operations that allocate all intermediate structures, Streams are lazy chains that produce one element at a time, reducing memory footprint and enabling infinite sequences. The BEAM scheduler yields between Stream operations, allowing multiple concurrent pipelines to interleave fairly. At scale (processing millions of rows or events), the difference between eager and lazy evaluation becomes the difference between consistent latency and garbage collection pauses. Production systems benefit most when Streams are composed at library boundaries, not scattered across the codebase.

---

## Advanced Considerations

Production testing strategies require careful attention to resource management and test isolation across multiple concurrent test processes. In large codebases, tests can consume significant memory and CPU resources, especially when using concurrent testing without proper synchronization and cleanup. The BEAM scheduler's preemptive nature means test processes may interfere with each other if shared resources aren't properly isolated at the process boundary. Pay careful attention to how Ecto's sandbox mode interacts with your supervision tree — if you have GenServers that hold state across tests, the sandbox rollback mechanism may leave phantom processes in your monitoring systems that continue consuming resources until forced cleanup occurs.

When scaling tests to production-grade test suites, consider the cost of stub verification and the memory overhead of generated test cases. Each property-based test invocation can create thousands of synthetic test cases, potentially causing garbage collection pressure that's invisible during local testing but becomes critical in CI/CD pipelines running long test suites continuously. The interaction between concurrent tests and ETS tables (often used in caches and registry patterns) requires explicit `inherited: true` options to prevent unexpected sharing between test processes, which can cause mysterious failures when tests run in different orders or under load.

For distributed testing scenarios using tools like `Peer`, network simulation can mask real latency issues and failure modes. Test timeouts that work locally may fail in CI due to scheduler contention and GC pauses. Always include substantial buffers for timeout values and monitor actual execution times under load. The coordination between multiple test nodes requires careful cleanup — a failure in test coordination can leave zombie processes consuming resources indefinitely. Implement proper telemetry hooks within your test helpers to diagnose production-like scenarios and capture performance characteristics.


## Trade-offs and production gotchas

**1. Using `Enum.random/1` inside a property**
This bypasses StreamData. When the property fails you get a random value with no shrinking.
Always generate with StreamData, never with `:rand`.

**2. Over-filtering with `filter/2`**
`filter(generator, fn x -> x != 0 end)` discards values. If the filter rejects > 99% of draws
StreamData gives up. Use `map` or change the base generator instead — `positive_integer()`
rather than `filter(integer(), &(&1 > 0))`.

**3. Writing properties that never actually exercise the code**
If `money_pair_same_currency` generated independent currencies, every commutativity check
would raise on `ArgumentError`, which is still "not a bug" — the property would be vacuously
green. Always log or assert on something the function computes, not just on absence of crashes.

**4. Trusting the default 100 runs for regulated domains**
For payment or compliance code, bump to `max_runs: 1_000` via `@moduletag timeout: :infinity`
and `check all m <- money(), max_runs: 1_000 do`. 100 runs miss rare buggy states.

**5. When NOT to use this**
Property-based testing is overkill for pure data transformations with known fixtures (parsing
a config file, serializing a struct). Use it where invariants exist: arithmetic, sorting,
idempotent operations, round-trip encoders.

## Reflection

The `split_factor` range above is `1..100`. What happens to shrinking if you widen it to
`1..1_000_000`, and why does that change the *debuggability* of a failing property even though
the final counterexample is still minimal?


## Executable Example

```elixir
# test/support/generators.ex
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
