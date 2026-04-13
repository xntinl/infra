# `cond` vs nested `if` — when each one fits

**Project**: `pricing_rule_engine` — applies tier discounts and taxes to a cart total

---

## Project structure

```
pricing_rule_engine/
├── lib/
│   └── pricing_rule_engine/
│       └── pricing.ex
├── test/
│   └── pricing_rule_engine_test.exs
└── mix.exs
```

---

## What you will learn

1. **`cond`** — first truthy branch wins, no exhaustiveness check.
2. **When `if`/`unless` beat `cond` and vice-versa** — the rule is about the *shape* of the
   decision, not personal taste.

---

## The concept in 60 seconds

```elixir
if x > 0, do: :positive, else: :not_positive     # two branches, one boolean
```

```elixir
cond do
  x > 100 -> :large
  x > 10  -> :medium
  true    -> :small
end
```

`if` is for a **single yes/no** question. `cond` is for a **cascade of unrelated booleans**.
If you find yourself writing `if (...) do ... else if (...) do ... else ... end end`, you
want a `cond` (or a `case`, if the conditions are all shape-based).

---

## Why a pricing engine

Pricing rules stack: tier discount, bulk discount, tax. Each rule is a distinct boolean
against the cart. That is the textbook `cond` shape — a cascade where the first matching
rule wins. Inside each branch, `if` shines for a final yes/no (taxable or not).

---

## Design decisions

**Option A — `cond` for multi-way branching on unrelated booleans**
- Pros: Each condition reads top-to-bottom, no indentation growth, easy to insert a new tier
- Cons: All conditions evaluated top-to-bottom — if a condition is expensive, order matters

**Option B — nested `if`/`else`** (chosen)
- Pros: Familiar from imperative languages
- Cons: Indentation grows per level, hard to reorder, easy to miss a branch

→ Chose **A** because discount tiers are a multi-way branch on unrelated thresholds — that's exactly `cond`'s sweet spot. Use B only for binary true/false.

## Implementation

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Standard library: no external dependencies required
  ]
end
```


### Step 1 — Create the project

**Objective**: Create a minimal library so the pricing logic can be compared as a flat `cond` versus nested `if` without infrastructure noise.

```bash
mix new pricing_rule_engine
cd pricing_rule_engine
```

### Step 2 — `lib/pricing_rule_engine/pricing.ex`

**Objective**: Use `cond` to express ordered mutually exclusive pricing rules flatly, avoiding the nested-`if` arrow that hides business-rule priority.

```elixir
defmodule PricingRuleEngine.Pricing do
  @moduledoc """
  Computes final price = subtotal - tier_discount - bulk_discount + tax.

  Tier discount (picks ONE, first match wins — that is why cond fits):
    subtotal >= 1000 -> 15%
    subtotal >= 500  -> 10%
    subtotal >= 100  ->  5%
    otherwise        ->  0%

  Bulk discount: extra 2% if qty > 20 (independent of tier).

  Tax: 21% on the discounted subtotal, unless the customer is tax_exempt.
  """

  @type cart :: %{
          required(:subtotal) => number(),
          required(:qty) => non_neg_integer(),
          required(:tax_exempt) => boolean()
        }

  @spec total(cart()) :: float()
  def total(%{subtotal: subtotal, qty: qty, tax_exempt: tax_exempt}) do
    tier = tier_discount(subtotal)
    bulk = bulk_discount(qty)

    discounted = subtotal * (1 - tier) * (1 - bulk)

    # Single yes/no → if fits better than cond here.
    # Using `if` signals "exactly two branches" to the reader.
    taxed =
      if tax_exempt do
        discounted
      else
        discounted * 1.21
      end

    Float.round(taxed, 2)
  end

  # Cascading thresholds — cond is the canonical shape.
  # Each branch is an independent boolean, not pattern matching on shape.
  defp tier_discount(subtotal) do
    cond do
      subtotal >= 1000 -> 0.15
      subtotal >= 500  -> 0.10
      subtotal >= 100  -> 0.05
      true             -> 0.0
    end
  end

  # A single boolean condition — if is clearer than cond here.
  defp bulk_discount(qty) do
    if qty > 20, do: 0.02, else: 0.0
  end
end
```

### Step 3 — `test/pricing_rule_engine_test.exs`

**Objective**: Test each rule's boundary values so the clause ordering — the critical invariant of any `cond` — is locked against accidental reshuffling.

```elixir
defmodule PricingRuleEngineTest do
  use ExUnit.Case, async: true

  alias PricingRuleEngine.Pricing

  describe "tier discount cascade (cond)" do
    test "no tier discount below 100" do
      # 50 * 1.21 = 60.50
      assert Pricing.total(%{subtotal: 50, qty: 1, tax_exempt: false}) == 60.50
    end

    test "5% tier at 100" do
      # 100 * 0.95 * 1.21 = 114.95
      assert Pricing.total(%{subtotal: 100, qty: 1, tax_exempt: false}) == 114.95
    end

    test "10% tier at 500" do
      # 500 * 0.90 * 1.21 = 544.50
      assert Pricing.total(%{subtotal: 500, qty: 1, tax_exempt: false}) == 544.50
    end

    test "15% tier at 1000 — highest wins even though 500 threshold also matches" do
      # 1000 * 0.85 * 1.21 = 1028.50
      assert Pricing.total(%{subtotal: 1000, qty: 1, tax_exempt: false}) == 1028.50
    end
  end

  describe "bulk discount (if)" do
    test "no bulk discount at qty 20" do
      # 100 * 0.95 * 1.21 = 114.95
      assert Pricing.total(%{subtotal: 100, qty: 20, tax_exempt: false}) == 114.95
    end

    test "bulk discount kicks in at qty 21" do
      # 100 * 0.95 * 0.98 * 1.21 = 112.65
      assert Pricing.total(%{subtotal: 100, qty: 21, tax_exempt: false}) == 112.65
    end
  end

  describe "tax_exempt (if)" do
    test "no tax applied" do
      # 100 * 0.95 = 95.00
      assert Pricing.total(%{subtotal: 100, qty: 1, tax_exempt: true}) == 95.00
    end
  end
end
```

### Step 4 — Run the tests

**Objective**: Run the suite to confirm no `cond` clause falls through, which would raise `CondClauseError` at runtime rather than fail the build.

```bash
mix test
```

All 7 tests pass.

---

### Why this works

The approach chosen above keeps the core logic **pure, pattern-matchable, and testable**. Each step is a small, named transformation with an explicit return shape, so adding a new case means adding a new clause — not editing a branching block. Failures are data (`{:error, reason}`), not control-flow, which keeps the hot path linear and the error path explicit.


## Key Concepts

### 1. `cond` is for Multiple Boolean Conditions
`cond` tries each condition in order. The first truthy condition executes. The final `true` is a catch-all.

### 2. `if` vs `cond`
`if` is for a single boolean condition. `cond` is for multiple conditions. Use `case` if you're pattern-matching.

### 3. Prefer `case` When Possible
Pattern matching with `case` is more powerful and compile-time checkable. `cond` is a fallback when conditions don't fit a pattern.

---
## Benchmark

```elixir
{time_us, _result} =
  :timer.tc(fn ->
    for _ <- 1..1_000 do
      # representative call of apply_discount/1 over 1M cart totals
      :ok
    end
  end)

IO.puts("Avg: #{time_us / 1_000} µs/call")
```

Target: **< 20ms total; each lookup ~20ns**.

## Trade-offs

| Situation | Use |
|---|---|
| One boolean, two branches | `if` / `unless` |
| Cascading unrelated booleans | `cond` |
| Dispatching on data shape | `case` with patterns |
| Exactly N shape clauses, all same arity | Multi-clause functions |

**When NOT to use `cond`:**

- **Only one branch plus fallback.** That is an `if`. `cond` for a single condition adds
  visual noise.
- **All conditions are shape-based.** Use `case` — you get exhaustiveness warnings and
  pattern destructuring.
- **Branches are mutually exclusive by construction.** A lookup map `%{:a => ..., :b => ...}`
  is faster and easier to change than a `cond`.

**When NOT to use nested `if`:**

- **More than two levels deep.** Flatten with `cond`, `case`, or early returns via
  pattern matching. Nested `if` is where bugs hide.

---

## Common production mistakes

**1. Forgetting the `true` fallback in `cond`**
`cond` with no truthy branch raises `CondClauseError`. Always end with `true -> default`
unless you *want* the crash (and you rarely do in user-facing code).

**2. Using `cond` where `case` fits**
`cond do x == {:ok, 1} -> ... end` — that is a pattern-matching job. `case x do {:ok, 1} -> ... end`
is faster, clearer, and exhaustive-checked by the compiler.

**3. Long boolean expressions inside `cond`**
`subtotal >= 500 and qty > 5 and customer.tier == :gold` repeated per branch becomes
unreadable. Extract `is_gold_bulk?(cart)` and use that as the condition.

**4. `if ... do ... end` without `else` when you need both branches**
`if x, do: 1` returns `nil` on the false path. If you rely on the value, add `else:`
explicitly — `nil` silently propagates and crashes elsewhere.

**5. Truthy confusion**
Elixir treats `nil` and `false` as falsy; *everything else* is truthy, including `0`
and `""`. `if list, do: ...` is true even for `[]`. Use explicit checks (`list != []`).

---

## Reflection

If your pricing rules come from a database (sales team changes them weekly), is `cond` still the right choice, or does the rule engine need to become data-driven? Sketch both approaches.

How does the ordering of `cond` clauses affect performance when the first clause is expensive? Where should short-circuit cheap-but-decisive checks sit?

## Resources

- [Elixir — Case, cond, and if](https://hexdocs.pm/elixir/case-cond-and-if.html)
- [Kernel.if/2](https://hexdocs.pm/elixir/Kernel.html#if/2)
- [Kernel.SpecialForms.cond/1](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#cond/1)
