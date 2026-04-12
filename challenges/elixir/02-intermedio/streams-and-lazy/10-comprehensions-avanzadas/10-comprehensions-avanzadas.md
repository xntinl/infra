# Advanced comprehensions — multi-generator `for`, `:into`, `:uniq`, filters

**Project**: `compr_advanced` — rebuild a small set of "what would be a nested
loop in other languages" problems using a single Elixir `for` comprehension.

---

## Project context

In Elixir, `for` is not a loop — it's a comprehension. It takes one or more
generators, zero or more filters, and an optional `:into` collectable target,
and returns a new collection. Once you internalize the *generator × filter ×
into* model, a lot of code that starts as `Enum.flat_map(...) |> Enum.filter(...)
|> Enum.into(...)` collapses into a single readable expression.

This exercise covers the four features that most elevate `for` above a
beginner's "list comprehension": multiple generators (cartesian product),
pattern-matching generators (implicit filtering), `:into` for non-list targets,
and `:uniq` for deduplication inside the comprehension.

Project structure:

```
compr_advanced/
├── lib/
│   └── compr_advanced.ex
├── test/
│   └── compr_advanced_test.exs
└── mix.exs
```

---

## Core concepts

### 1. Multiple generators = cartesian product

```elixir
for x <- [1, 2, 3], y <- [:a, :b], do: {x, y}
# => [{1,:a},{1,:b},{2,:a},{2,:b},{3,:a},{3,:b}]
```

Each additional generator nests inside the previous one. Later generators
can reference earlier variables — this is what makes `for` more expressive
than `Enum.zip/2` (which pairs) or `Enum.flat_map/2` (which you'd nest
manually).

### 2. Pattern-matching generators filter silently

A generator pattern that doesn't match is skipped, no error raised:

```elixir
for {:ok, v} <- [{:ok, 1}, {:error, :x}, {:ok, 2}], do: v
# => [1, 2]
```

This is idiomatic for extracting values from tagged tuples. Prefer it over
a later `Enum.filter` + `Enum.map` pair.

### 3. Boolean filters

Any expression after generators — separated by commas — acts as a filter.
Non-truthy values drop that combination:

```elixir
for x <- 1..10, rem(x, 2) == 0, do: x * x
# => [4, 16, 36, 64, 100]
```

### 4. `:into` targets any `Collectable`

`:into` changes the result type. Maps, MapSets, IO streams, and `%{}` all
implement `Collectable`. This is the cleanest way to build a map from a list
of pairs inside a single expression:

```elixir
for {k, v} <- pairs, into: %{}, do: {k, v}
```

### 5. `:uniq` deduplicates inside the comprehension

`:uniq: true` discards repeated *results* as they are produced — no need
for a trailing `Enum.uniq/1`. It compares the final value (after `do:`),
not the generator input.

---

## Design decisions

**Option A — Pipeline of `Enum.map/flat_map/filter/uniq/into`**
- Pros: every step has a name, easy to comment per-stage.
- Cons: allocates an intermediate list at each stage; verbose for multi-generator combinations.

**Option B — Single `for` comprehension with generators, filters, `:into`, `:uniq`** (chosen)
- Pros: one expression, compiles to nested reduces, pattern-matching generators filter silently, `:into` targets any `Collectable`.
- Cons: harder to debug mid-pipeline; strict (not lazy).

→ Chose **B** because the exercise is specifically about the generator × filter × into model and how it collapses nested-loop patterns into one readable expression.

---

## Implementation

### Step 1: Create the project

```bash
mix new compr_advanced
cd compr_advanced
```

### Step 2: `lib/compr_advanced.ex`

```elixir
defmodule ComprAdvanced do
  @moduledoc """
  A tour of `for` comprehensions in Elixir beyond the one-generator basics:
  multiple generators, pattern-matching generators, boolean filters,
  `:into`, and `:uniq`.
  """

  @doc """
  Returns the cartesian product of two enumerables as a list of tuples.
  Demonstrates multiple generators in a single comprehension.
  """
  @spec cartesian(Enumerable.t(), Enumerable.t()) :: [tuple()]
  def cartesian(xs, ys) do
    for x <- xs, y <- ys, do: {x, y}
  end

  @doc """
  Returns all *unique* unordered pairs `{a, b}` from `enum` where `a < b`.
  The `a < b` filter both avoids duplicates like `{1,2}`/`{2,1}` and is far
  cheaper than generating the full product and deduping afterwards.
  """
  @spec unordered_pairs(Enumerable.t()) :: [{any(), any()}]
  def unordered_pairs(enum) do
    list = Enum.to_list(enum)
    for a <- list, b <- list, a < b, do: {a, b}
  end

  @doc """
  Given a list of tagged results, keep only successful values.

  Uses a *pattern-matching generator*: entries that don't match `{:ok, v}`
  are silently skipped — much cleaner than an explicit filter + map.
  """
  @spec oks([{:ok, any()} | {:error, any()}]) :: [any()]
  def oks(results) do
    for {:ok, v} <- results, do: v
  end

  @doc """
  Builds a map of `word => length` from a list of words.
  Demonstrates `:into` targeting a Collectable other than a list.
  """
  @spec length_map([String.t()]) :: %{String.t() => non_neg_integer()}
  def length_map(words) do
    for w <- words, into: %{}, do: {w, String.length(w)}
  end

  @doc """
  Returns the unique products of pairs drawn from two lists.

  `:uniq: true` deduplicates results as they are produced — no extra
  `Enum.uniq/1` pass, and duplicates don't even accumulate in memory.
  """
  @spec unique_products([integer()], [integer()]) :: [integer()]
  def unique_products(xs, ys) do
    for x <- xs, y <- ys, uniq: true, do: x * y
  end

  @doc """
  Produces every pythagorean triple `{a, b, c}` with `a <= b` and `c <= max`.

  Three generators + two filters — a one-liner that would be a triple-nested
  loop in most other languages.
  """
  @spec pythagorean_triples(pos_integer()) :: [{pos_integer(), pos_integer(), pos_integer()}]
  def pythagorean_triples(max) when is_integer(max) and max > 0 do
    for a <- 1..max,
        b <- a..max,
        c <- b..max,
        a * a + b * b == c * c,
        do: {a, b, c}
  end
end
```

### Step 3: `test/compr_advanced_test.exs`

```elixir
defmodule ComprAdvancedTest do
  use ExUnit.Case, async: true

  describe "cartesian/2" do
    test "produces every combination in row-major order" do
      assert ComprAdvanced.cartesian([1, 2], [:a, :b]) ==
               [{1, :a}, {1, :b}, {2, :a}, {2, :b}]
    end

    test "empty generator yields empty result" do
      assert ComprAdvanced.cartesian([], [:a, :b]) == []
    end
  end

  describe "unordered_pairs/1" do
    test "returns each pair once with a < b" do
      assert ComprAdvanced.unordered_pairs([3, 1, 2]) == [{1, 2}, {1, 3}, {2, 3}]
    end
  end

  describe "oks/1" do
    test "pattern-matching generator silently skips non-:ok entries" do
      assert ComprAdvanced.oks([{:ok, 1}, {:error, :x}, {:ok, 2}, :other]) == [1, 2]
    end
  end

  describe "length_map/1" do
    test "collects into a map via :into" do
      assert ComprAdvanced.length_map(["a", "hi", "yes"]) ==
               %{"a" => 1, "hi" => 2, "yes" => 3}
    end
  end

  describe "unique_products/2" do
    test ":uniq removes duplicates as results are produced" do
      # 1*4=4, 2*2=4, 2*4=8, 1*2=2 — 4 appears twice and must be kept once.
      assert ComprAdvanced.unique_products([1, 2], [2, 4]) |> Enum.sort() ==
               [2, 4, 8]
    end
  end

  describe "pythagorean_triples/1" do
    test "finds the classic 3-4-5 and 6-8-10 up to 10" do
      assert ComprAdvanced.pythagorean_triples(10) == [{3, 4, 5}, {6, 8, 10}]
    end
  end
end
```

### Step 4: Run

```bash
mix test
```

### Why this works

`for` compiles to nested `Enum.reduce/3` calls: each generator becomes an outer reduce, each filter becomes a conditional inside, and `:into` wraps the final accumulator in the target `Collectable`. Pattern-matching generators simply skip non-matching values without raising. `:uniq: true` adds a set-based dedup check as results are produced, avoiding a trailing `Enum.uniq/1`.

---

<!-- benchmark N/A: syntactic-sugar topic; the underlying reduce has the same perf as an explicit pipeline -->

## Trade-offs and production gotchas

**1. Comprehensions are syntactic sugar over `Enum.reduce`**
`for` compiles to nested reduces. For one or two generators with simple
filters, it reads better than a pipeline. For five-stage transformations,
a `|>` pipeline is clearer because each step has a name.

**2. `:uniq` buys memory, not laziness**
`for` is strict (not a stream). `:uniq` still materializes the full result
list — it just hashes outputs to skip duplicates. For truly lazy deduplication
over large inputs, use `Stream.uniq/1` inside a `Stream` pipeline.

**3. Pattern-matching generators can hide bugs**
`for {:ok, v} <- results` silently ignores `{:error, reason}`. That's the
feature — but if an error should be *loud*, don't use a pattern generator.
Use an explicit `case` or `with` so failures are visible.

**4. Multiple generators explode combinatorially**
`for x <- 1..1000, y <- 1..1000, do: {x, y}` allocates one million tuples.
Think about input sizes before chaining generators; consider `Stream` for
large cartesian walks where you only need part of the output.

**5. `:into` on large results can be slower than you expect**
`into: %{}` builds a map via `Collectable.Map`, which is essentially
`Map.put/3` per entry. For hot paths building huge maps, `Map.new/1` over
a list result is often faster in practice because it uses bulk insert.

**6. When NOT to use `for`**
- When you actually need laziness → use `Stream`.
- When each step would benefit from a name for readability → use a pipeline.
- When you want early termination → `for` always walks every generator;
  use `Enum.reduce_while/3` or a `Stream.take_while/2`.

---

## Reflection

- If you had to express a three-source join (users × orders × line_items) with filters on all three levels and an accumulator map keyed by `{user_id, order_id}`, would you keep a single `for`, or split into an explicit pipeline? Justify based on debuggability and allocation profile.
- Pattern-matching generators silently drop non-matches. When is that dangerous, and how would you detect the drop statistically in a production data pipeline?

---

## Resources

- [`Kernel.SpecialForms.for/1` — the comprehension spec](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#for/1)
- ["Comprehensions" — Elixir getting started](https://hexdocs.pm/elixir/comprehensions.html)
- [`Collectable` protocol](https://hexdocs.pm/elixir/Collectable.html) — what `:into` targets
- [José Valim — "Comprehensions in Elixir"](https://elixir-lang.org/blog/2015/12/10/keynote-and-elixir-1-2/) — original design notes on the generator-filter-into model
