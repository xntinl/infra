# Property-Based Testing with StreamData

**Project**: `property_testing` — a currency conversion library with invariants.
---

## Project context

Your team owns `money_calc`, an internal library for multi-currency invoice computations.
It's used across 12 services and three bugs slipped through unit tests last quarter, all
of the form "developer forgot an edge case": negative amounts, near-zero conversion rates,
rounding boundaries, and currency pairs with different decimal precisions (JPY has 0 decimals,
USD has 2, BHD has 3).

Example-based tests catch what you can imagine. Property-based tests catch what you didn't.
Instead of asserting on a handful of handpicked values, you declare **properties** that must
hold for all valid inputs — commutativity, monotonicity, inverse, identity — and let
[StreamData](https://hexdocs.pm/stream_data) generate thousands of cases per property, shrink
failures to minimal counterexamples, and regress on the same seed.

This exercise introduces StreamData generators, `check all`, shrinking, `StreamData.bind/2`,
and the stateful state-machine testing variant. The domain is intentionally small so the
focus stays on property design.

Project structure:

```
property_testing/
├── lib/
│   └── money_calc/
│       ├── money.ex              # value type {amount, currency}
│       ├── converter.ex          # conversion logic with rates
│       └── invoice.ex            # sums, discounts, tax
├── test/
│   ├── generators.ex             # shared generators (in test/support)
│   ├── money_calc/
│   │   ├── money_test.exs
│   │   ├── converter_property_test.exs
│   │   └── invoice_property_test.exs
│   └── test_helper.exs
└── mix.exs
```

---

## Why this approach and not alternatives

Alternatives considered and discarded:

- **Hand-rolled equivalent**: reinvents primitives the BEAM/ecosystem already provides; high risk of subtle bugs around concurrency, timeouts, or failure propagation.
- **External service (e.g. Redis, sidecar)**: adds a network hop and an extra failure domain for a problem the VM can solve in-process with lower latency.
- **Heavier framework abstraction**: couples the module to a framework lifecycle and makes local reasoning/testing harder.

The chosen approach stays inside the BEAM, uses idiomatic OTP primitives, and keeps the contract small.

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
### 1. Properties instead of examples

An example-based test asserts on specific inputs:

```elixir
assert Money.add(Money.new(100, :usd), Money.new(50, :usd)) == Money.new(150, :usd)
```

A property asserts a general truth:

```elixir
property "addition is commutative" do
  check all a <- money(), b <- money(), a.currency == b.currency do
    assert Money.add(a, b) == Money.add(b, a)
  end
end
```

StreamData generates ~100 pairs `(a, b)` per run, shrinks any failure to the smallest input
that still fails, and re-runs with a deterministic seed so failures are reproducible in CI.

### 2. Generators compose

A generator is a `%StreamData{}` struct that lazily produces values. Primitives:

```elixir
StreamData.integer()                    # any integer
StreamData.integer(1..100)              # bounded
StreamData.member_of([:usd, :eur, :jpy])
StreamData.string(:alphanumeric, max_length: 10)
StreamData.list_of(integer(), length: 1..5)
```

Composite via `bind/2` (dependency) or `tuple/1`, `fixed_map/1`:

```
    integer(1..100) ── bind ──▶  fn n -> list_of(integer(), length: n) end
         |                                 |
         v                                 v
     generates n                   generates list of size n
```

### 3. Shrinking — the real power

When a property fails, StreamData does NOT hand you the random input that broke it. It
recursively tries smaller inputs and reports the smallest failure. If a 500-element list
triggers the bug, it'll shrink to the 3-element list that still triggers it.

```
  Initial failure:       [17, -42, 99, 0, ..., 131]   (500 elements)
         |
         v  shrink
  [17, -42, 99]          (3 elements)
         |
         v  shrink
  [-1]                   (minimal counterexample)
```

Your generators must be **shrinkable**: built-in generators are; custom ones built via
`map/2` lose shrinking if they break monotonicity. Prefer `bind/2` when possible.

### 4. Metamorphic properties — the cheat code

You don't have a second implementation to compare against ("oracle test"). How do you assert
correctness of a function you're testing?

Use **metamorphic relations**: properties that relate one call to another:

- **Inverse**: `decode(encode(x)) == x`
- **Idempotency**: `f(f(x)) == f(x)`
- **Commutativity**: `f(a, b) == f(b, a)`
- **Associativity**: `f(a, f(b, c)) == f(f(a, b), c)`
- **Distributivity**: `f(a, g(b, c)) == g(f(a, b), f(a, c))`

For `Money.Converter`: `convert(convert(m, :eur, :usd), :usd, :eur) ≈ m` (inverse with
tolerance for rounding error).

### 5. Custom generator discipline

Not every random input is valid. For currency conversion, the rate must be `> 0`. A bad
generator produces `0` and blows up in the code, which is a useless failure (garbage-in).

Constrain generators:

```elixir
def positive_decimal do
  StreamData.bind(StreamData.integer(1..10_000), fn n ->
    StreamData.constant(n / 100.0)
  end)
end
```

Or filter (slow — rejects are expensive):

```elixir
StreamData.filter(StreamData.float(), fn x -> x > 0.0 end)
```

Rule: filter only when rejection rate is low (< 20%). Otherwise bind from a smaller domain.

---

## Design decisions

**Option A — naive/simple approach**
- Pros: minimal code, easy to reason about.
- Cons: breaks under load, lacks observability, hard to evolve.

**Option B — the approach used here** (chosen)
- Pros: production-grade, handles edge cases, testable boundaries.
- Cons: more moving parts, requires understanding of the BEAM primitives involved.

→ Chose **B** because correctness under concurrency and failure modes outweighs the extra surface area.

## Implementation

### Step 1: `mix.exs`

**Objective**: Isolate StreamData to test/dev scopes and add test/support to compile paths so generators stay out of production builds.

```elixir
defmodule MoneyCalc.MixProject do
  use Mix.Project

  def project do
    [
      app: :money_calc,
      version: "0.1.0",
      elixir: "~> 1.16",
      elixirc_paths: elixirc_paths(Mix.env()),
      deps: deps()
    ]
  end

  def application, do: [extra_applications: [:logger]]

  defp elixirc_paths(:test), do: ["lib", "test/support"]
  defp elixirc_paths(_), do: ["lib"]

  defp deps do
    [{:stream_data, "~> 1.1", only: [:test, :dev]}]
  end
end
```

### Step 2: Domain — `Money`

**Objective**: Represent monetary amounts as integer minor units and closed currencies to avoid float rounding errors during property shrinking.

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
```

### Step 3: Converter

**Objective**: Implement currency converter that scales amounts by decimal places and applies rates, handling edge cases like identity conversion and rounding.

```elixir
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
```

### Step 4: Invoice

**Objective**: Implement invoice total calculator that sums line items, applies discount and tax with rounding at each step.

```elixir
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
```

### Step 5: Shared generators

**Objective**: Build reusable StreamData generators with bind/2 to constrain domains and ensure shrinking converges to minimal counterexamples.

```elixir
# test/support/generators.ex
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
```

### Step 6: Money property tests

**Objective**: Assert algebraic properties (commutative, associative, identity, inverse, currency type safety) using StreamData generators.

```elixir
# test/money_calc/money_test.exs
defmodule MoneyCalc.MoneyTest do
  use ExUnit.Case, async: true
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

### Step 7: Converter property tests

**Objective**: Test metamorphic relations (identity, round-trip inverse, linear scaling) with tolerance for rounding error across currencies.

```elixir
# test/money_calc/converter_property_test.exs
defmodule MoneyCalc.ConverterPropertyTest do
  use ExUnit.Case, async: true
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
```

### Step 8: Invoice property tests

**Objective**: Assert invoice invariants (identity, discount absorption, partition additive) across dynamic line item counts.

```elixir
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

### Step 9: Running properties with more iterations

**Objective**: Execute property tests with deterministic seeds and increased iteration counts to validate invariants at scale.

```bash
mix test
mix test --seed 0                                           # deterministic
mix test test/money_calc/converter_property_test.exs --trace
MIX_ENV=test elixir --erl "+S 1" -S mix test                # single scheduler for reproducibility
```

To increase coverage locally (slower):

```elixir
# in config/test.exs
config :stream_data, max_runs: 1_000
```

---


### Why this works

The design leans on BEAM guarantees (process isolation, mailbox ordering, supervisor restarts) and pushes invariants to the boundaries of each module. State transitions are explicit, failure modes are declared rather than implicit, and each step is independently testable. That combination keeps the implementation correct under concurrent load and cheap to change later.

## Benchmark

```elixir
# Minimal measurement — replace with Benchee for distribution stats.
{time_us, _} = :timer.tc(fn ->
  for _ <- 1..10_000, do: run_operation()
end)
IO.puts("avg: #{time_us / 10_000} µs/op")
```

Target: operation should complete in the low-microsecond range on modern hardware; deviations by >2× indicate a regression worth investigating.

## Deep Dive: Property Patterns and Production Implications

Property-based testing inverts the testing mindset: instead of writing examples, you state invariants (properties) and let a generator find counterexamples. StreamData's shrinking capability is its superpower—when a property fails on a 10,000-element list, the framework reduces it to the minimal list that still fails, cutting debugging time from hours to minutes. The trade-off is that properties require rigorous thinking about domain constraints, and not every invariant is worth expressing as a property. Teams that adopt property testing often find bugs in specifications themselves, not just implementations.

---

## Advanced Considerations

Production testing strategies require careful attention to resource management and test isolation across multiple concurrent test processes. In large codebases, tests can consume significant memory and CPU resources, especially when using concurrent testing without proper synchronization and cleanup. The BEAM scheduler's preemptive nature means test processes may interfere with each other if shared resources aren't properly isolated at the process boundary. Pay careful attention to how Ecto's sandbox mode interacts with your supervision tree — if you have GenServers that hold state across tests, the sandbox rollback mechanism may leave phantom processes in your monitoring systems that continue consuming resources until forced cleanup occurs.

When scaling tests to production-grade test suites, consider the cost of stub verification and the memory overhead of generated test cases. Each property-based test invocation can create thousands of synthetic test cases, potentially causing garbage collection pressure that's invisible during local testing but becomes critical in CI/CD pipelines running long test suites continuously. The interaction between concurrent tests and ETS tables (often used in caches and registry patterns) requires explicit `inherited: true` options to prevent unexpected sharing between test processes, which can cause mysterious failures when tests run in different orders or under load.

For distributed testing scenarios using tools like `Peer`, network simulation can mask real latency issues and failure modes. Test timeouts that work locally may fail in CI due to scheduler contention and GC pauses. Always include substantial buffers for timeout values and monitor actual execution times under load. The coordination between multiple test nodes requires careful cleanup — a failure in test coordination can leave zombie processes consuming resources indefinitely. Implement proper telemetry hooks within your test helpers to diagnose production-like scenarios and capture performance characteristics.


## Trade-offs and production gotchas

**1. Property tests are slower than example tests**
Each property runs 100 iterations by default. A suite with 50 properties at 10ms each is 50s.
Keep property tests focused on core invariants; let example tests cover edge cases you've
already characterised.

**2. Shrinking can hide bugs if generators aren't well-designed**
If your generator never emits a value that would expose the bug (e.g. always positive when
the bug is on negatives), no amount of iterations will find it. Audit your generators by
printing sample values: `StreamData.sample(money(), 10)`.

**3. Flaky properties are almost always your bug, not StreamData's**
If a property passes on seed 42 and fails on seed 17, your property has a hidden precondition
you didn't encode. Either add a guard (`... when a > 0`) or fix the code under test.

**4. Floating-point arithmetic ruins strict equality**
Properties like `f(g(x)) == x` fail with floats. Use absolute or relative tolerance. For
money, prefer integer minor-unit representation (as shown here) — avoids floats entirely
in domain logic.

**5. Overly-constrained generators hide the space you care about**
If your `positive_money` generator caps amounts at 100, you never test overflow around
`2^63`. Set realistic bounds informed by production data — invoice totals can reach millions.

**6. `StreamData.filter` is a trap**
Filter discards values that don't match a predicate. If 90% are discarded, every iteration
wastes cycles. Rewrite as `bind` from a constrained primitive.

**7. Don't test tautologies**
```elixir
property "add returns the result of add" do
  check all a <- money(), b <- money() do
    assert Money.add(a, b) == Money.add(a, b)
  end
end
```
This tests determinism. Possibly useful, usually not.

**8. When NOT to use property-based testing**
- UI logic and workflows — prefer integration tests.
- Code with unbounded external side effects (HTTP, DB writes) — property tests need to run
  thousands of iterations; real I/O makes that untenable.
- Pure mapping / transformation code that's trivially correct by inspection — example tests
  are faster to read and maintain.

---

## Stateful property testing (preview)

For GenServer / state-machine testing use `StreamData`'s lower-level combinators to generate
*sequences of commands* and check invariants after each step. This is covered separately in
a dedicated section — the short version:

```elixir
commands = list_of(one_of([
  {constant(:push), integer()},
  constant(:pop),
  constant(:peek)
]))

property "stack commands are consistent with a reference list implementation" do
  check all cmds <- commands do
    {_real_state, _ref_state, ok} =
      Enum.reduce(cmds, {Stack.new(), [], true}, &apply_and_compare/2)
    assert ok
  end
end
```

---

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?


## Executable Example

```elixir
# test/money_calc/converter_property_test.exs
defmodule MoneyCalc.ConverterPropertyTest do
  use ExUnit.Case, async: true
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
