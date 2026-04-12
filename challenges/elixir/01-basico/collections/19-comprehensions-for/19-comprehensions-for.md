# Comprehensions with `for`: Building a Grid Combination Engine

**Project**: `grid_combo` — generates cartesian products, filters combinations, and reshapes grids for a product configurator

---

## Why `for` comprehensions matter for a senior developer

Elixir's `for` is not a loop. It is a comprehension that iterates over one or more
generators, applies filters, and collects results into a target collection. Three
features make it special:

1. **Multiple generators** — `for x <- xs, y <- ys` produces the cartesian product
   in a single expression. The equivalent with `Enum.flat_map` is significantly
   noisier.
2. **Inline filters** — `for x <- xs, x > 0` skips elements without a separate
   `Enum.filter` pass.
3. **`into:` option** — `for x <- xs, into: %{}` collects results into any
   collectable (Map, MapSet, File stream, custom Collectable). No need for a
   trailing `Enum.into/2`.
4. **`:uniq` option** — `for x <- xs, uniq: true` deduplicates results on the
   fly using a process dictionary-less mechanism.

Understanding `for` matters when you:

- Generate combinations for configuration spaces (product options, A/B variants)
- Build lookup maps from list pairs in one pass
- Flatten nested data with conditions (inventory × locations × prices)
- Write readable code where nested `Enum.flat_map` chains would hide intent

---

## The business problem

An e-commerce platform sells custom t-shirts. A customer picks combinations of:

- **Colors**: `[:red, :blue, :black, :white]`
- **Sizes**: `[:s, :m, :l, :xl]`
- **Styles**: `[:crew, :vneck, :polo]`

Not every combination is valid. Business rules:

- Polo style is only available in size M and L
- White color is unavailable in polo style
- Some combinations are temporarily out of stock

You need a module that:

1. Produces all valid combinations (cartesian product + business filters)
2. Builds a pricing map keyed by `{color, size, style}`
3. Groups combinations by style into a map `%{style => [variant, ...]}`
4. Deduplicates color palettes across combinations

---

## Project structure

```
grid_combo/
├── lib/
│   └── grid_combo/
│       ├── catalog.ex
│       └── pricing.ex
├── test/
│   └── grid_combo/
│       ├── catalog_test.exs
│       └── pricing_test.exs
└── mix.exs
```

---

## Core concepts applied here

### Multiple generators (cartesian product)

```elixir
for x <- [1, 2], y <- [:a, :b], do: {x, y}
# [{1, :a}, {1, :b}, {2, :a}, {2, :b}]
```

The rightmost generator varies fastest. With 3 generators of sizes N, M, K,
the result has N × M × K elements.

### Filters interleaved with generators

```elixir
for x <- 1..10, rem(x, 2) == 0, y <- 1..x, do: {x, y}
```

Filters apply to the generators BEFORE them. `rem(x, 2) == 0` short-circuits
early — `y` is never enumerated for odd `x`. This matters for performance on
large outer generators.

### `into:` for direct collection

```elixir
for {k, v} <- [{:a, 1}, {:b, 2}], into: %{}, do: {k, v}
# %{a: 1, b: 2}
```

Target must implement `Collectable`. Built-ins include `%{}`, `MapSet.new()`,
`""`, `File.stream!/1`. Skipping `into:` yields a list.

### `:uniq` to deduplicate

```elixir
for x <- [1, 1, 2, 2, 3], uniq: true, do: x * 10
# [10, 20, 30]
```

Runs a deduplication set internally. Useful when the comprehension produces
duplicates you would otherwise remove with `Enum.uniq/1`.

### Pattern matching in generators

Generators are pattern matches. Non-matching elements are skipped silently —
this is a FILTER in disguise, not an error:

```elixir
for {:ok, value} <- [{:ok, 1}, {:error, :nope}, {:ok, 2}], do: value
# [1, 2]
```

---

## Design decisions

**Option A — chained `Enum.flat_map/2` for cartesian products, followed by `Enum.filter/2`**
- Pros: only uses core `Enum`; no new syntax to learn; stages can be named and reused.
- Cons: 3+ generators become nested `flat_map` pyramids; the cartesian intent is hidden under callback noise; `into:` shape has to be applied as a separate `Enum.into/2`.

**Option B — `for` comprehension with multiple generators, filters, and `into:`** (chosen)
- Pros: declares intent — "for each x, y, z where P, yield T"; multi-generator is first-class syntax; `into:` lets you target any `Collectable` (list, map, MapSet, stream); pattern matching in generators silently skips non-matching elements without an `if`.
- Cons: syntax diverges from Python/JS `for`; very long comprehensions resist extraction into helpers; debugging a bad filter inside the comprehension is less obvious than an `Enum.filter/2` stage.

Chose **B** because the problem IS cartesian product with filtering, which is exactly what `for` was designed for. The declarative form reads like a set-builder, which is how the domain thinks about it.

---

## Implementation

### Step 1: Create the project

```bash
mix new grid_combo
cd grid_combo
```

### Step 2: `mix.exs`

```elixir
defmodule GridCombo.MixProject do
  use Mix.Project

  def project do
    [
      app: :grid_combo,
      version: "0.1.0",
      elixir: "~> 1.17",
      start_permanent: Mix.env() == :prod,
      deps: []
    ]
  end

  def application, do: [extra_applications: [:logger]]
end
```

### Step 3: `lib/grid_combo/catalog.ex`

```elixir
defmodule GridCombo.Catalog do
  @moduledoc """
  Generates and filters t-shirt variant combinations using `for`
  comprehensions with multiple generators and business-rule filters.
  """

  @type color :: :red | :blue | :black | :white
  @type size :: :s | :m | :l | :xl
  @type style :: :crew | :vneck | :polo
  @type variant :: {color(), size(), style()}

  @colors [:red, :blue, :black, :white]
  @sizes [:s, :m, :l, :xl]
  @styles [:crew, :vneck, :polo]

  @doc """
  Generates every combination allowed by business rules.

  Using a single `for` with three generators and two filters is clearer
  than chaining three Enum.flat_map calls with intermediate Enum.filter.
  """
  @spec all_variants() :: [variant()]
  def all_variants do
    for color <- @colors,
        size <- @sizes,
        style <- @styles,
        valid_combination?(color, size, style) do
      {color, size, style}
    end
  end

  @doc """
  Groups variants by style into a map. `into: %{}` would not aggregate;
  we build a keyword-style accumulator and merge at the end.
  """
  @spec by_style() :: %{style() => [variant()]}
  def by_style do
    Enum.group_by(all_variants(), fn {_c, _s, style} -> style end)
  end

  @doc """
  Unique color palette across all valid variants.

  `:uniq` deduplicates during the comprehension so we do not need a
  trailing `Enum.uniq/1`.
  """
  @spec available_colors() :: [color()]
  def available_colors do
    for {color, _size, _style} <- all_variants(), uniq: true, do: color
  end

  # Business rules centralized here. Adding a rule means one extra
  # function clause — no change to the comprehension itself.
  defp valid_combination?(:white, _size, :polo), do: false
  defp valid_combination?(_color, size, :polo) when size not in [:m, :l], do: false
  defp valid_combination?(_color, _size, _style), do: true
end
```

### Step 4: `lib/grid_combo/pricing.ex`

```elixir
defmodule GridCombo.Pricing do
  @moduledoc """
  Builds a lookup map of variant => price using `for ..., into: %{}`.
  """

  alias GridCombo.Catalog

  @base_prices %{crew: 1990, vneck: 2290, polo: 3490}
  @size_surcharge %{s: 0, m: 0, l: 0, xl: 500}
  # Black and white are premium dyes in this fictional business
  @color_surcharge %{red: 0, blue: 0, black: 300, white: 300}

  @doc """
  Builds a price map keyed by `{color, size, style}`.

  Using `into: %{}` writes directly to the target map — no intermediate
  list allocated, no Enum.into post-pass.
  """
  @spec price_table() :: %{Catalog.variant() => non_neg_integer()}
  def price_table do
    for {color, size, style} = variant <- Catalog.all_variants(), into: %{} do
      price =
        @base_prices[style] +
          @size_surcharge[size] +
          @color_surcharge[color]

      {variant, price}
    end
  end

  @doc """
  Lists variants whose price falls in the given range. Pattern matching
  in the generator filters out mismatches before the range check.
  """
  @spec variants_in_range(Range.t()) :: [Catalog.variant()]
  def variants_in_range(%Range{} = range) do
    table = price_table()

    for {variant, price} <- table, price in range, do: variant
  end
end
```

### Step 5: Tests

```elixir
# test/grid_combo/catalog_test.exs
defmodule GridCombo.CatalogTest do
  use ExUnit.Case, async: true

  alias GridCombo.Catalog

  describe "all_variants/0" do
    test "produces a cartesian product filtered by business rules" do
      variants = Catalog.all_variants()

      # 4 colors × 4 sizes × 3 styles = 48 raw combinations.
      # Polo excludes sizes S and XL: removes 4 colors × 2 sizes = 8.
      # Polo excludes white: but white polo rows were already removed above
      # for sizes S and XL. Still need to remove white × {M, L} × polo = 2 more.
      # Total: 48 - 8 - 2 = 38.
      assert length(variants) == 38
    end

    test "no white polo variant exists" do
      variants = Catalog.all_variants()
      refute Enum.any?(variants, fn v -> v == {:white, :m, :polo} end)
      refute Enum.any?(variants, fn v -> v == {:white, :l, :polo} end)
    end

    test "polo is only available in M and L" do
      polo_sizes =
        Catalog.all_variants()
        |> Enum.filter(fn {_c, _s, style} -> style == :polo end)
        |> Enum.map(fn {_c, size, _s} -> size end)
        |> Enum.uniq()
        |> Enum.sort()

      assert polo_sizes == [:l, :m]
    end

    test "crew and vneck are available in all sizes and colors" do
      crew_count =
        Enum.count(Catalog.all_variants(), fn {_c, _s, style} -> style == :crew end)

      # 4 colors × 4 sizes = 16
      assert crew_count == 16
    end
  end

  describe "by_style/0" do
    test "returns a map keyed by style" do
      grouped = Catalog.by_style()

      assert Map.keys(grouped) |> Enum.sort() == [:crew, :polo, :vneck]
    end

    test "polo group has the expected count (3 colors × 2 sizes)" do
      %{polo: polos} = Catalog.by_style()
      assert length(polos) == 6
    end
  end

  describe "available_colors/0" do
    test "returns each color once even though many variants share it" do
      colors = Catalog.available_colors()
      assert Enum.sort(colors) == [:black, :blue, :red, :white]
      assert length(colors) == length(Enum.uniq(colors))
    end
  end
end
```

```elixir
# test/grid_combo/pricing_test.exs
defmodule GridCombo.PricingTest do
  use ExUnit.Case, async: true

  alias GridCombo.Pricing

  describe "price_table/0" do
    test "builds a map keyed by {color, size, style}" do
      table = Pricing.price_table()

      assert is_map(table)
      # 38 valid variants — same as Catalog.all_variants()
      assert map_size(table) == 38
    end

    test "applies size surcharge only to XL" do
      table = Pricing.price_table()

      red_m_crew = table[{:red, :m, :crew}]
      red_xl_crew = table[{:red, :xl, :crew}]

      assert red_xl_crew - red_m_crew == 500
    end

    test "applies color surcharge to black and white" do
      table = Pricing.price_table()

      red_m_crew = table[{:red, :m, :crew}]
      black_m_crew = table[{:black, :m, :crew}]

      assert black_m_crew - red_m_crew == 300
    end

    test "polo base price is higher than crew" do
      table = Pricing.price_table()

      assert table[{:red, :m, :polo}] > table[{:red, :m, :crew}]
    end
  end

  describe "variants_in_range/1" do
    test "returns only variants within the given price range" do
      cheap = Pricing.variants_in_range(0..2000)

      # Cheapest: crew (1990) with red/blue and no XL surcharge → 1990
      # Nothing else is at or below 2000
      assert Enum.all?(cheap, fn v ->
               price = Pricing.price_table()[v]
               price in 0..2000
             end)

      assert length(cheap) > 0
    end

    test "empty range returns empty list" do
      assert Pricing.variants_in_range(0..100) == []
    end
  end
end
```

### Step 6: Run and verify

```bash
mix compile --warnings-as-errors
mix test --trace
```

### Why this works

A `for` comprehension desugars to nested enumerations with filters short-circuiting per combination. Each generator iterates its source; pattern matching in the generator head silently skips elements that don't match (so `for {:ok, v} <- results` drops errors with no `case`). Filters apply per combination, so excluded combinations never reach the body — no wasted allocation. `into:` routes the result to any `Collectable`: a list by default, a `Map`, a `MapSet`, or a `File.Stream` for write-through. The compiler fuses the generators and filters into a single pass whenever the shape allows.

---

## Benchmark

```elixir
# bench.exs
defmodule Bench do
  def run do
    sizes = [10, 20, 40]  # 10x10x10, 20x20x20, 40x40x40 cartesian products

    for n <- sizes do
      {us, count} =
        :timer.tc(fn ->
          result =
            for x <- 1..n,
                y <- 1..n,
                z <- 1..n,
                rem(x + y + z, 3) == 0,
                do: {x, y, z}

          length(result)
        end)

      IO.puts("n=#{n}: #{count} tuples in #{us} µs")
    end
  end
end

Bench.run()
```

Target: under 50 ms for n=40 (64k filtered tuples out of 64k × 3 iterations). The cost is dominated by allocation, not the filter — if you're building a giant list only to consume it once, consider a `Stream` instead.

---

## Trade-off analysis

| Aspect                 | `for` comprehension (this)          | Chained Enum.flat_map                  | Nested for-loops (other langs) |
|------------------------|-------------------------------------|----------------------------------------|--------------------------------|
| Readability            | high (declares intent)              | medium (hides cartesian intent)        | high but mutable               |
| Performance            | same as Enum (single pass when possible) | same                              | n/a                            |
| `into:` target         | any Collectable                     | requires Enum.into at the end          | manual accumulator             |
| Filter placement       | inline, short-circuits              | requires extra Enum.filter             | inline                         |
| Multi-generator        | first-class                         | nested flat_map (noisy)                | first-class                    |

When the comprehension grows past ~4 generators or 3 filters, extract the
filters into named functions — the clause head stays readable and unit-testable.

---

## Common production mistakes

**1. Expecting `for` to short-circuit on `return`/`break`**
There is no `break`. `for` always enumerates every generator element. To stop
early, use `Enum.reduce_while/3` or filter at the source (e.g. `Stream.take/2`
before the comprehension).

**2. Mutating state in `for`**
Elixir has no mutation. Anything assigned inside `for` is scoped to that
iteration. `acc = acc + 1` inside a comprehension does NOT accumulate across
iterations. Use `Enum.reduce/3` when you need accumulation.

**3. Forgetting `into: %{}` and then calling `Enum.into/2`**
```elixir
# Wastes an intermediate list
for {k, v} <- pairs, do: {k, v} |> Enum.into(%{})

# Direct — no intermediate allocation
for {k, v} <- pairs, into: %{}, do: {k, v}
```

**4. Relying on pattern-match failure as a filter without documenting it**
```elixir
for {:ok, value} <- results, do: value
```
This silently drops `{:error, _}` entries. It is idiomatic but surprising to
readers unfamiliar with Elixir. Add a comment or make the intent explicit with
a `match?/2` filter.

**5. Huge cartesian products without bounds**
`for x <- 1..10_000, y <- 1..10_000, do: {x, y}` is 100M tuples. Elixir does
not warn you. Always sanity-check the product of generator sizes or use
`Stream.unfold/2` for lazy combinations.

---

## When NOT to use `for`

- You need early termination based on a running value — use
  `Enum.reduce_while/3`.
- You need parallel evaluation — use `Task.async_stream/3` or `Flow`.
- The transformation is a simple `Enum.map` or `Enum.filter` with no filters
  or multiple generators — using `for` is overkill and obscures intent.
- You are building a recursive algorithm that depends on accumulated state —
  write a tail-recursive function instead.

---

## Reflection

1. A `for` comprehension with 4 generators and 3 filters fits in 10 lines but the code review calls it "clever". When is dense declarative syntax worth defending, and when is the pragmatic answer to expand into explicit `Enum` stages? What signal tells you?
2. The cartesian product in this project is bounded (a few thousand combinations). For a combinatorial explosion (1M+ tuples), what changes: do you switch to `Stream.flat_map/2`, precompute only the prefixes that matter, or reject the cartesian approach entirely? How does `for`'s eager evaluation guide the decision?

---

## Resources

- [Comprehensions — Elixir Getting Started](https://hexdocs.pm/elixir/comprehensions.html)
- [Kernel.SpecialForms.for/1](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#for/1) — full syntax reference
- [Collectable protocol](https://hexdocs.pm/elixir/Collectable.html) — what `into:` uses under the hood
- [Enum.group_by/3](https://hexdocs.pm/elixir/Enum.html#group_by/3) — complement to `for` when grouping
