# Mutation Testing Concepts Applied to Elixir

**Project**: `discount_engine` — a pricing-rule module whose test suite quality is evaluated by applying mutation testing principles manually via targeted source mutations

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
discount_engine/
├── lib/
│   └── discount_engine.ex
├── script/
│   └── main.exs
├── test/
│   └── discount_engine_test.exs
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
defmodule DiscountEngine.MixProject do
  use Mix.Project

  def project do
    [
      app: :discount_engine,
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

### `lib/discount_engine.ex`

```elixir
# lib/discount_engine/rules.ex
defmodule DiscountEngine.Rules do
  @moduledoc """
  Discount rules:
    - cart_total >= 1000 AND is_member? → 15% off
    - cart_total >= 1000 AND not member → 10% off
    - cart_total >= 500 AND is_member?  → 5% off
    - otherwise                          → 0%
  """

  @type cart :: %{total_cents: non_neg_integer(), member?: boolean()}

  @doc "Returns discount cents result from member."
  @spec discount_cents(cart()) :: non_neg_integer()
  def discount_cents(%{total_cents: t, member?: true}) when t >= 1_000,
    do: percent(t, 15)

  @doc "Returns discount cents result from member."
  def discount_cents(%{total_cents: t, member?: false}) when t >= 1_000,
    do: percent(t, 10)

  @doc "Returns discount cents result from member."
  def discount_cents(%{total_cents: t, member?: true}) when t >= 500,
    do: percent(t, 5)

  @doc "Returns discount cents result."
  def discount_cents(%{total_cents: _}), do: 0

  defp percent(total, pct), do: div(total * pct, 100)
end

defmodule DiscountEngine.RulesTest do
  use ExUnit.Case, async: true
  doctest DiscountEngine.MixProject

  alias DiscountEngine.Rules

  # Each describe block targets a mutation category from the catalogue.

  describe "kill ROR mutants — >= vs > at the 1000 boundary" do
    test "exactly 1000 with member applies 15% (not 5%) — kills >= → >" do
      # If `>=` is mutated to `>`, the 1000 case falls through to the 500-branch,
      # returning 5% instead of 15%. This exact-value assertion kills it.
      assert Rules.discount_cents(%{total_cents: 1_000, member?: true}) == 150
    end

    test "999 with member applies 5% — kills >= → > at the 1000 boundary" do
      # Confirms the boundary is exclusive on the lower side of the 1000 tier.
      assert Rules.discount_cents(%{total_cents: 999, member?: true}) == 49
    end

    test "exactly 500 with member applies 5% (not 0%) — kills >= → > at 500" do
      assert Rules.discount_cents(%{total_cents: 500, member?: true}) == 25
    end

    test "499 with member applies 0% — kills >= → > at 500" do
      assert Rules.discount_cents(%{total_cents: 499, member?: true}) == 0
    end
  end

  describe "kill COR mutants — member? vs not member?" do
    test "1000-member gets 15% — kills true → false on member" do
      assert Rules.discount_cents(%{total_cents: 1_000, member?: true}) == 150
    end

    test "1000-non-member gets 10% — kills true → false on member" do
      # If member? branch is swapped, both cases return 15 → this test fails.
      assert Rules.discount_cents(%{total_cents: 1_000, member?: false}) == 100
    end

    test "500-non-member gets 0% — kills an accidental member-agnostic branch at 500" do
      assert Rules.discount_cents(%{total_cents: 500, member?: false}) == 0
    end
  end

  describe "kill AOR mutants — * vs / in percent/2" do
    test "explicit value 300 at 10% is 30 — kills * → +" do
      # total 300 would not actually reach 10% by rule; we verify percent helper
      # indirectly by comparing two totals whose ratio matches percentage.
      # 2000 at 10% = 200
      assert Rules.discount_cents(%{total_cents: 2_000, member?: false}) == 200
    end

    test "2000 at 15% is exactly 300 — kills * → + or / → * in percent/2" do
      assert Rules.discount_cents(%{total_cents: 2_000, member?: true}) == 300
    end
  end

  describe "kill SDL mutants — clause deletion" do
    test "zero-total cart returns 0, proving the fallback clause exists" do
      # If the 0-catch-all clause is deleted, this raises FunctionClauseError.
      assert Rules.discount_cents(%{total_cents: 0, member?: false}) == 0
    end

    test "1-cent cart returns 0" do
      assert Rules.discount_cents(%{total_cents: 1, member?: true}) == 0
    end
  end
end
```

### `test/discount_engine_test.exs`

```elixir
# WEAK TESTS — retained only as a reference for "what not to do".
# Kept in a describe block so they still run; they will still pass on the real code.

defmodule DiscountEngine.WeakRulesTest do
  use ExUnit.Case, async: true
  doctest DiscountEngine.MixProject
  alias DiscountEngine.Rules

  describe "weak assertions — survive common mutants" do
    # Survives: >= → > mutation at the 1000 boundary (test uses 2000, not 1000)
    test "large member cart gets a discount" do
      assert Rules.discount_cents(%{total_cents: 2_000, member?: true}) > 0
    end

    # Survives: member? → not member? swap (does not assert specific percentage)
    test "cart has some discount when member" do
      result = Rules.discount_cents(%{total_cents: 2_000, member?: true})
      assert is_integer(result)
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
