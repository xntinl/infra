# Mutation Testing Concepts Applied to Elixir

**Project**: `discount_engine` — a pricing-rule module whose test suite quality is evaluated by applying mutation testing principles manually via targeted source mutations.

## Project context

`discount_engine` applies tiered discounts to shopping carts. The test suite has 95%
line coverage. The team treats this as "well tested". A prod incident shows otherwise:
a boundary condition (>= vs >) slipped through. The test asserted on `assert discount > 0`
for a value of exactly 10 — which the buggy code also produced. The test passed; the
logic was wrong.

**Coverage is a lower bound on test quality, not a ceiling.** Mutation testing asks the
stronger question: if I deliberately break the source code in a small way (replace `>=`
with `>`, negate a boolean, replace `+` with `-`), do the tests fail? If the tests pass
on the mutated code, they are not actually checking the behaviour.

No mature Elixir mutation-testing library exists (there are prototypes; none production-
ready). This exercise teaches the **principles** through manual mutations and targeted
test strengthening — a practice you can apply to your existing suite today.

```
discount_engine/
├── lib/
│   └── discount_engine/
│       └── rules.ex                # module under mutation
├── test/
│   ├── discount_engine/
│   │   └── rules_test.exs          # strong tests that kill mutants
│   └── test_helper.exs
└── mix.exs
```

## Why mutation testing when coverage is already high

Coverage tells you **which lines ran**. It does not tell you **which assertions are
meaningful**. A line covered by a test that asserts `assert is_integer(result)` is still
covered — but the assertion is too weak to catch a swapped sign.

Mutation testing forces tests to assert on the **specific output value**, the **exact
boundary**, the **correct operator**. Tests that "kill the mutant" (fail on the broken
code) are of higher quality than tests that survive the mutation.

## Core concepts

### 1. A mutant is a small, syntactic change to source code
Swap `>` for `>=`. Replace `true` with `false`. Negate a pattern. Delete a line. Each
is a candidate "bug" a tool (or human) could plausibly introduce.

### 2. A mutant is "killed" when at least one test fails on the mutated code
A mutant that "survives" means no test distinguished the mutated behaviour from the
original. That survival reveals a weak test.

### 3. Mutation score
`mutants_killed / mutants_generated`. 100% is ideal. Realistically, 80%+ on critical
paths is a strong signal.

### 4. Equivalent mutants
Some mutations produce code that is semantically identical to the original (e.g.
`x * 1` → `x`). These cannot be killed and should be excluded.

## Common mutation operators (catalogue)

| Operator | Example | Kills? |
|----------|---------|--------|
| **ROR** (relational operator replacement) | `a >= b` → `a > b` | Test must cover the boundary |
| **AOR** (arithmetic operator replacement) | `a + b` → `a - b` | Test must assert exact value |
| **COR** (conditional operator replacement) | `a and b` → `a or b` | Test must exercise each branch |
| **LCR** (logical constant replacement) | `true` → `false` | Test must cover the negative branch |
| **UOI** (unary operator insertion) | `x` → `-x` | Test must assert sign |
| **SDL** (statement deletion) | remove a side-effect line | Test must verify the side effect |

## Design decisions

- **Option A — wait for a mature Elixir mutation-testing tool**: nothing matches
  Stryker (JS) or PIT (Java) yet. Waiting means shipping weak tests today.
- **Option B — manually generate mutants for the critical module**: low-tech but
  effective. Pick a module, apply the catalogue, run the tests, observe survivors,
  write tests that kill them.
- **Option C — run Excoveralls for coverage + the manual mutant protocol**: best of
  both. Coverage finds unexecuted lines; mutation finds unexamined ones.

Chosen: **Option C**. The workflow is pragmatic and teaches the discipline.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  [
    {:excoveralls, "~> 0.18", only: :test}
  ]
end
```

### Step 1: the module under test

**Objective**: Express discount tiers as guarded clauses with integer-cent arithmetic so each branch becomes a distinct mutation target with an exact expected value.

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

  @spec discount_cents(cart()) :: non_neg_integer()
  def discount_cents(%{total_cents: t, member?: true}) when t >= 1_000,
    do: percent(t, 15)

  def discount_cents(%{total_cents: t, member?: false}) when t >= 1_000,
    do: percent(t, 10)

  def discount_cents(%{total_cents: t, member?: true}) when t >= 500,
    do: percent(t, 5)

  def discount_cents(%{total_cents: _}), do: 0

  defp percent(total, pct), do: div(total * pct, 100)
end
```

### Step 2: weak tests (the "before")

**Objective**: Write coverage-only assertions like `result > 0` so mutants at boundaries and boolean flips visibly survive — evidence that line coverage lies.

These are tests written with coverage in mind but not mutation resilience. Each passes
on the original code but survives at least one common mutant.

```elixir
# WEAK TESTS — retained only as a reference for "what not to do".
# Kept in a describe block so they still run; they will still pass on the real code.

defmodule DiscountEngine.WeakRulesTest do
  use ExUnit.Case, async: true
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

### Step 3: strong tests that kill mutants

**Objective**: Assert exact values at 499/500/999/1000 boundaries and both member flags so ROR, COR, AOR, and SDL mutants all die.

```elixir
# test/discount_engine/rules_test.exs
defmodule DiscountEngine.RulesTest do
  use ExUnit.Case, async: true

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

### Step 4: the manual mutation workflow

**Objective**: Codify a one-operator-at-a-time edit-test-revert loop so mutation discipline becomes a repeatable PR ritual, not an ad-hoc intuition.

```
1. Pick a critical module (`lib/discount_engine/rules.ex`).
2. Pick an operator from the catalogue (e.g., ROR).
3. Apply ONE mutation to the source (e.g., change `t >= 1_000` to `t > 1_000`).
4. Run `mix test`.
5. If tests pass → you have a "surviving mutant". Write a test that kills it
   (typically an exact-value assertion at the boundary).
6. Revert the mutation. Repeat for the next operator.
```

Apply this process to ~5 mutations per module per review cycle. Boundaries and
operators catch the majority of real bugs.

## Why this works

A test that asserts `result > 0` cannot distinguish `15% * 1000 = 150` from
`10% * 1000 = 100` — both are positive. The mutation `member? = true` → `member? = false`
would change the result but not fail the weak assertion. Asserting on the **exact
expected value** (`== 150`) kills both common mutants.

Boundary testing — `999`, `1000`, `1001` — kills relational-operator mutants. Branch
coverage alone runs the line; value-level assertions prove each branch produces the
right output.

## Tests

See Step 3.

## Benchmark

The cost is human time, not wall-clock. One mutation + test run = ~5 seconds of CPU,
but ~1 minute of reading and editing. Target: review 5 mutations per module per PR
that touches the module.

## Deep Dive: Mutation Patterns and Production Implications

Mutation testing modifies your code and re-runs tests; if a test still passes, you've found a gap in your assertions. It's a powerful way to find weak tests that don't actually verify behavior. The cost is execution time—mutation testing can take 10–100× longer than normal tests. Production bugs from insufficient test coverage are real; mutation testing forces rigor by exposing dead assertions.

---

## Advanced Considerations

Production testing strategies require careful attention to resource management and test isolation across multiple concurrent test processes. In large codebases, tests can consume significant memory and CPU resources, especially when using concurrent testing without proper synchronization and cleanup. The BEAM scheduler's preemptive nature means test processes may interfere with each other if shared resources aren't properly isolated at the process boundary. Pay careful attention to how Ecto's sandbox mode interacts with your supervision tree — if you have GenServers that hold state across tests, the sandbox rollback mechanism may leave phantom processes in your monitoring systems that continue consuming resources until forced cleanup occurs.

When scaling tests to production-grade test suites, consider the cost of stub verification and the memory overhead of generated test cases. Each property-based test invocation can create thousands of synthetic test cases, potentially causing garbage collection pressure that's invisible during local testing but becomes critical in CI/CD pipelines running long test suites continuously. The interaction between concurrent tests and ETS tables (often used in caches and registry patterns) requires explicit `inherited: true` options to prevent unexpected sharing between test processes, which can cause mysterious failures when tests run in different orders or under load.

For distributed testing scenarios using tools like `Peer`, network simulation can mask real latency issues and failure modes. Test timeouts that work locally may fail in CI due to scheduler contention and GC pauses. Always include substantial buffers for timeout values and monitor actual execution times under load. The coordination between multiple test nodes requires careful cleanup — a failure in test coordination can leave zombie processes consuming resources indefinitely. Implement proper telemetry hooks within your test helpers to diagnose production-like scenarios and capture performance characteristics.


## Trade-offs and production gotchas

**1. Ignoring equivalent mutants**
`x * 1 → x`, `a + 0 → a`: semantically identical. Don't try to kill; mark as equivalent
and skip.

**2. Asserting on overly-strict output**
`assert result == 150` is strong but fragile if the rule changes. Document the
reasoning in a comment — "150 = 15% of 1000" — so future edits understand why the
literal matters.

**3. Only mutating operators**
Operator swaps are easy but incomplete. Don't forget: clause deletion, loop-bound
changes, exception replacement. The fuller the catalogue, the stronger the test suite.

**4. Running mutation testing on non-critical code**
Mutating a log-formatting helper is wasted effort. Focus on modules where behaviour
matters: pricing, security, authorization, financial calculation.

**5. False confidence from coverage**
95% line coverage with mutation score 40% is a weak suite. Both metrics together tell
the truth.

**6. When NOT to use this**
Prototype code that changes weekly does not benefit — the tests will be rewritten.
Apply mutation principles to STABLE, critical code.

## Reflection

A mutation score of 80% on a critical module is impressive, yet the remaining 20% of
surviving mutants include both equivalent mutants and real test gaps. What signal do
you use to decide which category a given surviving mutant falls into, and when is the
cost of classifying it higher than the value of the answer?

## Resources

- [PIT (Java) — the gold standard](https://pitest.org/) — read the operator catalogue
- [Stryker (JS)](https://stryker-mutator.io/) — comparable concepts
- [Offutt & Untch — "Mutation 2000" survey](https://cs.gmu.edu/~offutt/rsrch/papers/mut-survey.pdf)
- [Excoveralls](https://github.com/parroty/excoveralls) — pair coverage with this discipline

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```
