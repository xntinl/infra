# Strategy pattern with behaviours — pluggable pricing strategies

**Project**: `pricing_strategy` — a `PricingStrategy` behaviour with `Flat`, `Tiered`, and `Discount` implementations, selectable per-call.

**Difficulty**: ★★☆☆☆
**Estimated time**: 1–2 hours

---

## Project context

Strategy is the GoF pattern for "same problem, multiple algorithms". In a
checkout flow you might compute order totals with a flat price, tiered
volume discount, or a percentage-off promotion. The choice is per-order,
not per-environment, so the adapter-at-config-time approach
76 doesn't fit — the strategy is a runtime parameter.

A `@behaviour` still works: define the contract, implement each strategy,
and let callers pass the strategy module (or an `{module, opts}` tuple) at
call time. This gives you open/closed extensibility — new strategies drop
in without touching callers.

Project structure:

```
pricing_strategy/
├── lib/
│   ├── pricing_strategy.ex
│   ├── pricing_strategy/flat.ex
│   ├── pricing_strategy/tiered.ex
│   └── pricing_strategy/discount.ex
├── test/
│   └── pricing_strategy_test.exs
└── mix.exs
```

---

## Core concepts

### 1. The contract is a single `calculate/2` function

```elixir
@callback calculate(quantity :: pos_integer(), opts :: keyword()) ::
            {:ok, amount_cents :: non_neg_integer()} | {:error, term()}
```

All strategies accept the same inputs and return the same shape. The
difference is internal: how the number is computed.

### 2. Strategy is passed at call time, not configured

```elixir
PricingStrategy.calculate(PricingStrategy.Tiered, 50, tiers: [...])
```

Compare with the Adapter pattern, where the module is read
from `Application.get_env/2`. Strategy is runtime-chosen; Adapter is
config-chosen.

### 3. Options are strategy-specific

`Flat` takes `:unit_price`. `Tiered` takes `:tiers`. `Discount` takes
`:unit_price` and `:percent`. The behaviour doesn't constrain option
shapes — only the call's signature. Document and validate per-strategy.

### 4. Strategies should be stateless functions

Strategies are pure: same inputs, same outputs. Don't put state in the
strategy module; pass it through `opts`. This makes strategies trivially
testable and swappable.

---

### Dependencies (`mix.exs`)

```elixir
def deps do
  [
    {error},
    {exunit},
    {invalid},
    {ok},
  ]
end
```
## Implementation

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.


```bash
mix new pricing_strategy
cd pricing_strategy
```

### Step 2: `lib/pricing_strategy.ex`

**Objective**: Implement `pricing_strategy.ex` — polymorphism via dispatch on the data's type (protocol) or via an explicit contract (behaviour).


```elixir
defmodule PricingStrategy do
  @moduledoc """
  Pluggable pricing strategies. Callers pass the strategy module explicitly;
  implementations are stateless modules that implement `@behaviour
  PricingStrategy`.
  """

  @type quantity :: pos_integer()
  @type cents :: non_neg_integer()

  @callback calculate(quantity, keyword()) :: {:ok, cents} | {:error, term()}

  @doc """
  Run `strategy.calculate/2`. Exists so callers have one entry point and
  so you can add cross-cutting concerns (logging, metrics) in one place.
  """
  @spec calculate(module(), quantity, keyword()) :: {:ok, cents} | {:error, term()}
  def calculate(strategy, quantity, opts)
      when is_atom(strategy) and is_integer(quantity) and quantity > 0 do
    strategy.calculate(quantity, opts)
  end
end
```

### Step 3: `lib/pricing_strategy/flat.ex`

**Objective**: Implement `flat.ex` — polymorphism via dispatch on the data's type (protocol) or via an explicit contract (behaviour).


```elixir
defmodule PricingStrategy.Flat do
  @moduledoc """
  Flat price: `quantity * unit_price`. Simplest strategy — useful as a
  baseline and as a fallback for promotions that don't apply.
  """

  @behaviour PricingStrategy

  @impl PricingStrategy
  def calculate(quantity, opts) do
    case Keyword.fetch(opts, :unit_price) do
      {:ok, unit_price} when is_integer(unit_price) and unit_price >= 0 ->
        {:ok, quantity * unit_price}

      _ ->
        {:error, :missing_or_invalid_unit_price}
    end
  end
end
```

### Step 4: `lib/pricing_strategy/tiered.ex`

**Objective**: Implement `tiered.ex` — polymorphism via dispatch on the data's type (protocol) or via an explicit contract (behaviour).


```elixir
defmodule PricingStrategy.Tiered do
  @moduledoc """
  Tiered/volume pricing. `tiers` is a list of `{min_quantity, unit_price}`
  sorted ascending by `min_quantity`. The matching tier is the highest
  `min_quantity` ≤ `quantity`.
  """

  @behaviour PricingStrategy

  @impl PricingStrategy
  def calculate(quantity, opts) do
    with {:ok, tiers} <- fetch_tiers(opts),
         {:ok, unit_price} <- pick_tier(tiers, quantity) do
      {:ok, quantity * unit_price}
    end
  end

  defp fetch_tiers(opts) do
    case Keyword.fetch(opts, :tiers) do
      {:ok, tiers} when is_list(tiers) and tiers != [] -> {:ok, tiers}
      _ -> {:error, :missing_tiers}
    end
  end

  # Walk tiers sorted descending and pick the first whose min_quantity fits.
  # Sorting here rather than trusting caller input avoids a sneaky bug class.
  defp pick_tier(tiers, quantity) do
    match =
      tiers
      |> Enum.sort_by(fn {min, _price} -> min end, :desc)
      |> Enum.find(fn {min, _price} -> quantity >= min end)

    case match do
      {_min, price} -> {:ok, price}
      nil -> {:error, :no_matching_tier}
    end
  end
end
```

### Step 5: `lib/pricing_strategy/discount.ex`

**Objective**: Implement `discount.ex` — polymorphism via dispatch on the data's type (protocol) or via an explicit contract (behaviour).


```elixir
defmodule PricingStrategy.Discount do
  @moduledoc """
  Percentage-off strategy wrapping a base unit price. `percent` is a
  number from 0 to 100. Rounds to nearest cent (banker's rounding via
  `round/1`).
  """

  @behaviour PricingStrategy

  @impl PricingStrategy
  def calculate(quantity, opts) do
    with {:ok, unit_price} <- fetch_non_neg_int(opts, :unit_price),
         {:ok, percent} <- fetch_percent(opts) do
      gross = quantity * unit_price
      # Subtract the discount, then round once at the end — rounding each
      # item individually introduces rounding drift on large orders.
      net = round(gross * (100 - percent) / 100)
      {:ok, net}
    end
  end

  defp fetch_non_neg_int(opts, key) do
    case Keyword.fetch(opts, key) do
      {:ok, v} when is_integer(v) and v >= 0 -> {:ok, v}
      _ -> {:error, {:invalid, key}}
    end
  end

  defp fetch_percent(opts) do
    case Keyword.fetch(opts, :percent) do
      {:ok, p} when is_number(p) and p >= 0 and p <= 100 -> {:ok, p}
      _ -> {:error, :invalid_percent}
    end
  end
end
```

### Step 6: `test/pricing_strategy_test.exs`

**Objective**: Write `pricing_strategy_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule PricingStrategyTest do
  use ExUnit.Case, async: true
  alias PricingStrategy.{Flat, Tiered, Discount}

  describe "Flat" do
    test "quantity * unit_price" do
      assert {:ok, 500} = PricingStrategy.calculate(Flat, 5, unit_price: 100)
    end

    test "rejects missing unit_price" do
      assert {:error, :missing_or_invalid_unit_price} =
               PricingStrategy.calculate(Flat, 5, [])
    end
  end

  describe "Tiered" do
    @tiers [{1, 100}, {10, 90}, {100, 75}]

    test "picks the highest matching tier" do
      assert {:ok, 500} = PricingStrategy.calculate(Tiered, 5, tiers: @tiers)
      assert {:ok, 900} = PricingStrategy.calculate(Tiered, 10, tiers: @tiers)
      assert {:ok, 15_000} = PricingStrategy.calculate(Tiered, 200, tiers: @tiers)
    end

    test "works regardless of tier input order" do
      reversed = Enum.reverse(@tiers)
      assert {:ok, 900} = PricingStrategy.calculate(Tiered, 10, tiers: reversed)
    end

    test "rejects quantity below the lowest tier" do
      high_tiers = [{10, 90}, {100, 75}]
      assert {:error, :no_matching_tier} =
               PricingStrategy.calculate(Tiered, 5, tiers: high_tiers)
    end
  end

  describe "Discount" do
    test "applies percent off the gross total" do
      # 10 * 200 = 2000 gross; 10% off = 1800.
      assert {:ok, 1800} =
               PricingStrategy.calculate(Discount, 10,
                 unit_price: 200,
                 percent: 10
               )
    end

    test "zero percent equals flat price" do
      assert {:ok, 2000} =
               PricingStrategy.calculate(Discount, 10,
                 unit_price: 200,
                 percent: 0
               )
    end

    test "rejects out-of-range percent" do
      assert {:error, :invalid_percent} =
               PricingStrategy.calculate(Discount, 10,
                 unit_price: 200,
                 percent: 150
               )
    end
  end

  describe "polymorphic dispatch" do
    test "same facade call, different strategy → different result" do
      qty = 10

      flat = PricingStrategy.calculate(Flat, qty, unit_price: 100)
      tiered = PricingStrategy.calculate(Tiered, qty, tiers: [{1, 100}, {10, 90}])

      assert {:ok, 1000} = flat
      assert {:ok, 900} = tiered
    end
  end
end
```

### Step 7: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
```

---

## Trade-offs and production gotchas

**1. Strategy is runtime; adapter is config — don't conflate them**
Strategy picks per-call (this order uses Tiered, that order uses Discount).
Adapter picks per-environment (dev uses TestAdapter, prod uses Email).
Using `Application.get_env/2` for a Strategy means every order uses the
same pricing, which defeats the pattern.

**2. Options are strategy-specific — validate at the boundary**
The behaviour can't express "Tiered needs `:tiers`, Discount needs `:percent`".
Each impl must validate its opts and return a clear `{:error, ...}`, which
the tests verify. Don't let a missing option crash inside the calculation.

**3. Rounding at the right moment**
Percentage discounts make the result non-integer. Round once at the end,
not per-item — per-item rounding drifts by cents on large orders. The
`Discount` strategy rounds gross, not per line.

**4. Strategies should be pure functions**
Do not read process state, the database, or the clock inside `calculate/2`.
If the strategy needs external data (e.g., current tax rate), pass it in
`opts`. Impure strategies are unpredictable and hard to test.

**5. When NOT to use the Strategy pattern**
If there's only ever one algorithm, a plain function is clearer. If the
"strategies" differ by a few branching lines, a `case` expression is
smaller and more obvious than three modules. Strategy pays off when the
algorithms have distinct shapes and likely continue to diverge.

---

## Resources

- [`Module.add_behaviour/3` and behaviours](https://hexdocs.pm/elixir/Module.html)
- ["Programming Elixir 1.6" — Strategy via behaviours](https://pragprog.com/titles/elixir16/programming-elixir-1-6/)
- ["Design Patterns in Elixir" — João Britto blog](https://blog.appsignal.com/2019/09/10/design-patterns-in-elixir.html)


## Key Concepts

Protocols and behaviors are Elixir's mechanism for ad-hoc and static polymorphism. They solve different problems and are often confused.

**Protocols:**
Dispatch based on the type/struct of the first argument at runtime. A protocol defines a contract (e.g., `Enumerable`); any type can implement it by adding a corresponding implementation block. Protocols excel when you control neither the type nor the caller — e.g., a library that needs to iterate any collection. The fallback is `:any` — if no specific implementation exists, the `:any` handler is tried. This enables "optional" protocol implementations.

**Behaviours:**
Static polymorphism enforced at compile time. A module implements a behavior by defining callbacks (functions). Behaviors are about contracts between modules, not types. Use when you need multiple implementations of the same interface and the caller chooses which to use (e.g., different database adapters, different strategies). Callbacks are checked at compile time — missing a required callback is a compiler error.

**Architectural patterns:**
Behaviors excel in plugin systems (user defines modules conforming to the behavior). Protocols excel in type-driven dispatch (any type can conform). Mix both: a behavior can require that its callbacks operate on types that implement a protocol. Example: `MyAdapter` behavior requiring callbacks that work with `Enumerable` types.
