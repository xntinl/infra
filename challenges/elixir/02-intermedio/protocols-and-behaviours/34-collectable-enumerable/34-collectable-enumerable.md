# `Enumerable` and `Collectable` — a custom `Bag` collection

**Project**: `custom_bag` — a multiset (`Bag`) struct that fully implements `Enumerable` (so `Enum.*` works) and `Collectable` (so `Enum.into/2` works).

---

## Project context

You're building a small multiset — a collection where each element is stored
with its count, and order doesn't matter. You want it to feel native: iterate
with `Enum.map/2`, count with `Enum.count/1`, and build from other
collections with `Enum.into(list, %Bag{})`. That means implementing two
companion protocols:

- `Enumerable` — how `Enum` reads from you.
- `Collectable` — how `Enum.into` / `for ... into:` writes into you.

These are the two most complex stdlib protocols because they deal with
streaming, suspension, and early termination. This exercise implements the
minimum correct version.

Project structure:

```
custom_bag/
├── lib/
│   └── bag.ex
├── test/
│   └── bag_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `Enumerable.reduce/3` is the heart of `Enum`

Every `Enum` function is built on top of `reduce/3`. Its signature is:

```
reduce(enum, acc, reducer) :: result
acc      :: {:cont, term} | {:halt, term} | {:suspend, term}
result   :: {:done, term} | {:halted, term} | {:suspended, term, continuation}
```

You must honor the three accumulator states: `:cont` means keep going,
`:halt` means stop now, `:suspend` means pause and return a continuation.
Suspension is what makes `Stream.zip/2` work without loading both sides.

### 2. `count/1` and `member?/2` can opt out

Returning `{:error, __MODULE__}` from `count/1` or `member?/2` tells `Enum`
"I can't answer in O(1), please derive this from `reduce/3`". Always opt
out when the O(1) answer requires traversing anyway — it's honest and it
keeps the protocol usable.

### 3. `Collectable.into/1` returns a two-arity collector

```
{initial_acc, collector_fun}
collector_fun(acc, {:cont, elem}) :: acc'
collector_fun(acc, :done)         :: final_collection
collector_fun(acc, :halt)         :: :ok   # cleanup on error
```

`:halt` is called if `Enum.into` raises mid-stream — use it to release
resources (file handles, ports). For pure data structures, return `:ok`.

### 4. `slice/1` is optional — leave it as `{:error, _}` unless O(1) slicing applies

For list-like structures with random access, implementing `slice/1` unlocks
`Enum.at/2` and `Enum.slice/2` in O(1). For a `Bag` it doesn't make sense —
opt out.

---

## Why implement both `Enumerable` and `Collectable` together

**`Enumerable` only.** `Enum.map/2` works, but `Enum.into/2` and `for ... into:` don't — callers must write a manual `Enum.reduce/3` to build a `Bag` from a list.

**`Collectable` only.** `Enum.into(list, bag)` works, but reading back with `Enum.count/1`, `Enum.map/2`, etc. doesn't — callers pattern-match `bag.counts` directly, leaking the internal shape.

**Both (chosen).** `Bag` behaves like any stdlib collection: `Enum.*` reads, `Enum.into` writes, `for ... into:` builds. The implementation cost is modest and the ergonomic payoff is the whole reason custom collections exist.

---

## Design decisions

**Option A — Implement `Enumerable.reduce/3` by walking the count map directly**
- Pros: Zero intermediate allocation; streams cleanly with `:suspend`.
- Cons: More state-machine code in the reduce loop; easier to get `:halt` / `:suspend` wrong.

**Option B — Materialize to a list-with-duplicates and reuse a list reducer** (chosen)
- Pros: The reduce loop is the canonical 4-clause pattern; easy to audit for correctness on `:cont` / `:halt` / `:suspend`.
- Cons: Allocates `O(size)` temporary list; bad for large bags in a hot path.

→ Chose **B** because correctness of the `Enumerable` contract is the main learning goal, and the list-materialization cost is localized to one function you can optimize later without changing the public surface.

---

### Dependencies (`mix.exs`)

```elixir
def deps do
  [
    {cont},
    {done},
    {error},
    {exunit},
    {halt},
    {halted},
    {ok},
    {suspend},
    {suspended},
  ]
end
```
## Implementation

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.


```bash
mix new custom_bag
cd custom_bag
```

### Step 2: `lib/bag.ex`

**Objective**: Implement `bag.ex` — polymorphism via dispatch on the data's type (protocol) or via an explicit contract (behaviour).


```elixir
defmodule Bag do
  @moduledoc """
  A multiset: each element is stored with a positive integer count. Order is
  not preserved. Iteration yields one copy of each element per count.

  Implements `Enumerable` and `Collectable`, so all `Enum` functions work.
  """

  defstruct counts: %{}

  @type t :: %__MODULE__{counts: %{optional(term()) => pos_integer()}}

  @doc "An empty bag."
  @spec new() :: t
  def new, do: %__MODULE__{}

  @doc "Add `element` to the bag (increments its count by 1)."
  @spec put(t, term()) :: t
  def put(%__MODULE__{counts: counts} = bag, element) do
    %{bag | counts: Map.update(counts, element, 1, &(&1 + 1))}
  end

  @doc "Total number of elements (counting duplicates)."
  @spec size(t) :: non_neg_integer()
  def size(%__MODULE__{counts: counts}) do
    counts |> Map.values() |> Enum.sum()
  end
end

defimpl Enumerable, for: Bag do
  # ── count/member?/slice — cheap metadata hooks ──────────────────────────
  def count(%Bag{} = bag), do: {:ok, Bag.size(bag)}

  def member?(%Bag{counts: counts}, element) do
    {:ok, Map.has_key?(counts, element)}
  end

  # Opt out: Bag has no meaningful random-access slice.
  def slice(_bag), do: {:error, __MODULE__}

  # ── reduce/3 — the iteration engine ─────────────────────────────────────
  #
  # We materialize the bag as a list of elements (duplicated by count) and
  # reuse the standard list reduction. For a real collection with millions
  # of entries you would stream counts without allocating a list.
  def reduce(%Bag{counts: counts}, acc, fun) do
    counts
    |> Enum.flat_map(fn {element, n} -> List.duplicate(element, n) end)
    |> do_reduce(acc, fun)
  end

  # Canonical reduce loop — honor :cont, :halt, and :suspend exactly.
  defp do_reduce(_list, {:halt, acc}, _fun), do: {:halted, acc}

  defp do_reduce(list, {:suspend, acc}, fun) do
    # Suspend: return a continuation the caller can resume later.
    {:suspended, acc, &do_reduce(list, &1, fun)}
  end

  defp do_reduce([], {:cont, acc}, _fun), do: {:done, acc}

  defp do_reduce([head | tail], {:cont, acc}, fun) do
    do_reduce(tail, fun.(head, acc), fun)
  end
end

defimpl Collectable, for: Bag do
  def into(%Bag{} = bag) do
    collector = fn
      acc, {:cont, element} -> Bag.put(acc, element)
      acc, :done -> acc
      _acc, :halt -> :ok
    end

    {bag, collector}
  end
end
```

### Step 3: `test/bag_test.exs`

**Objective**: Write `bag_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule BagTest do
  use ExUnit.Case, async: true

  describe "Enumerable" do
    test "Enum.count/1 returns total size with duplicates" do
      bag = Bag.new() |> Bag.put(:a) |> Bag.put(:a) |> Bag.put(:b)
      assert Enum.count(bag) == 3
    end

    test "Enum.member?/2 checks element presence" do
      bag = Bag.new() |> Bag.put(:a)
      assert Enum.member?(bag, :a)
      refute Enum.member?(bag, :b)
    end

    test "Enum.to_list/1 yields one entry per count" do
      bag = Bag.new() |> Bag.put(:x) |> Bag.put(:x) |> Bag.put(:y)
      assert Enum.sort(Enum.to_list(bag)) == [:x, :x, :y]
    end

    test "Enum.map/2 works and yields count-many results" do
      bag = Bag.new() |> Bag.put(1) |> Bag.put(2) |> Bag.put(2)
      assert bag |> Enum.map(&(&1 * 10)) |> Enum.sort() == [10, 20, 20]
    end

    test "Enum.take/2 halts early (uses :halt)" do
      bag = Bag.new() |> Bag.put(:a) |> Bag.put(:a) |> Bag.put(:a) |> Bag.put(:a)
      # take/2 must halt after n elements; if :halt isn't honored, this loops forever.
      assert Enum.take(bag, 2) |> length() == 2
    end
  end

  describe "Collectable" do
    test "Enum.into/2 collects from a list" do
      bag = Enum.into([:a, :a, :b], Bag.new())
      assert bag.counts == %{a: 2, b: 1}
    end

    test "for ... into: builds a bag" do
      bag = for x <- 1..3, into: Bag.new(), do: rem(x, 2)
      # 1 -> 1, 2 -> 0, 3 -> 1
      assert bag.counts == %{0 => 1, 1 => 2}
    end

    test "collecting into a non-empty bag accumulates" do
      start = Bag.new() |> Bag.put(:a)
      bag = Enum.into([:a, :b], start)
      assert bag.counts == %{a: 2, b: 1}
    end
  end
end
```

### Step 4: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
```

### Why this works

`Enumerable.reduce/3` is a four-clause state machine that honors `:cont` (keep going), `:halt` (stop now), and `:suspend` (return a continuation) — those three states are what make `Enum.take/2`, `Stream.zip/2`, and lazy interop work. `count/1` is `{:ok, total}` because the count map makes size an O(map-values) operation, but `slice/1` returns `{:error, __MODULE__}` because there is no meaningful random-access order for a multiset. `Collectable.into/1` returns a 2-arity collector that funnels `{:cont, elem}` through `Bag.put/2` and cleanly handles `:halt` for error paths.

---


## Key Concepts: Enumerable and Collectable Protocols

`Enumerable` lets you use `Enum.map/filter/reduce` on custom collections. `Collectable` is the inverse: it lets you build a collection incrementally (`Enum.into/2`). Together they enable composition: any enumerable can be into any collectable.

Example: implement `Enumerable` on your custom tree, and you can `Enum.map(tree, fn x -> x * 2 end)`. Implement `Collectable`, and you can `Enum.into(list, new_tree())` to build a tree from a list. Most users don't implement these; `Enum` has fallbacks. But for domain-specific collections, these protocols are essential.


## Benchmark

```elixir
bag =
  Enum.reduce(1..10_000, Bag.new(), fn i, acc ->
    Bag.put(acc, rem(i, 100))
  end)

{count_time, _} = :timer.tc(fn -> Enum.count(bag) end)
{map_time, _}   = :timer.tc(fn -> Enum.map(bag, &(&1 * 2)) end)

IO.puts("count=#{count_time} µs  map=#{map_time} µs (size=#{Bag.size(bag)})")
```

Target esperado: `Enum.count/1` debería completar en <50 µs (O(keys)); `Enum.map/2` escala con el tamaño del multiset materializado (~10k elementos aquí) y debería completar en unos pocos ms. Si `map` tarda órdenes de magnitud más, probablemente olvidaste una cláusula de `reduce/3`.

---

## Trade-offs and production gotchas

**1. `reduce/3` MUST honor `:suspend`**
Skipping the suspension clause breaks `Stream.zip/2`, `Enum.zip/2` against
streams, and any caller that pauses iteration. The test suite above doesn't
exercise it directly — add a stream-zip assertion if you intend to rely on
lazy interop.

**2. Materializing to a list defeats streaming**
The implementation above expands counts into a list, which is fine for small
bags but bad for millions. A production `Bag` should iterate the count map
directly, emitting one element per iteration without a flat_map.

**3. Returning `{:ok, count}` from `count/1` is a promise of O(1)**
If your "count" actually traverses the whole collection, return
`{:error, __MODULE__}` instead — `Enum.count/1` will do the traversal itself,
and callers who want O(1) size won't be misled.

**4. Collectable's `:halt` is for cleanup**
If you implement `Collectable` for a file or a port, `:halt` is your chance
to close the resource when the surrounding pipeline crashes. Ignoring it
leaks handles.

**5. When NOT to implement Enumerable**
If iteration doesn't have a natural single-pass order (e.g. a graph), forcing
it into `Enumerable` makes a meaningless order feel meaningful. Offer
explicit traversal functions (`walk_bfs/1`, `walk_dfs/1`) instead.

---

## Reflection

- Rewrite `reduce/3` to iterate the count map directly, without materializing an intermediate list. What's the trickiest part — tracking the remaining count of the current key, or threading the `:suspend` continuation through that state?
- A teammate proposes `count/1` return `{:ok, map_size(counts)}` (number of distinct elements) because "it's O(1)". What breaks in the test suite, and what does this reveal about the difference between *cardinality* and *size* in a multiset?

---

## Executable Example

Copy the code below into a file (e.g., `solution.exs`) and run with `elixir solution.exs`:

```elixir
defmodule Bag do
  @moduledoc """
  A multiset: each element is stored with a positive integer count. Order is
  not preserved. Iteration yields one copy of each element per count.

  Implements `Enumerable` and `Collectable`, so all `Enum` functions work.
  """

  defstruct counts: %{}

  @type t :: %__MODULE__{counts: %{optional(term()) => pos_integer()}}

  @doc "An empty bag."
  @spec new() :: t
  def new, do: %__MODULE__{}

  @doc "Add `element` to the bag (increments its count by 1)."
  @spec put(t, term()) :: t
  def put(%__MODULE__{counts: counts} = bag, element) do
    %{bag | counts: Map.update(counts, element, 1, &(&1 + 1))}
  end

  @doc "Total number of elements (counting duplicates)."
  @spec size(t) :: non_neg_integer()
  def size(%__MODULE__{counts: counts}) do
    counts |> Map.values() |> Enum.sum()
  end
end

defimpl Enumerable, for: Bag do
  # ── count/member?/slice — cheap metadata hooks ──────────────────────────
  def count(%Bag{} = bag), do: {:ok, Bag.size(bag)}

  def member?(%Bag{counts: counts}, element) do
    {:ok, Map.has_key?(counts, element)}
  end

  # Opt out: Bag has no meaningful random-access slice.
  def slice(_bag), do: {:error, __MODULE__}

  # ── reduce/3 — the iteration engine ─────────────────────────────────────
  #
  # We materialize the bag as a list of elements (duplicated by count) and
  # reuse the standard list reduction. For a real collection with millions
  # of entries you would stream counts without allocating a list.
  def reduce(%Bag{counts: counts}, acc, fun) do
    counts
    |> Enum.flat_map(fn {element, n} -> List.duplicate(element, n) end)
    |> do_reduce(acc, fun)
  end

  # Canonical reduce loop — honor :cont, :halt, and :suspend exactly.
  defp do_reduce(_list, {:halt, acc}, _fun), do: {:halted, acc}

  defp do_reduce(list, {:suspend, acc}, fun) do
    # Suspend: return a continuation the caller can resume later.
    {:suspended, acc, &do_reduce(list, &1, fun)}
  end

  defp do_reduce([], {:cont, acc}, _fun), do: {:done, acc}

  defp do_reduce([head | tail], {:cont, acc}, fun) do
    do_reduce(tail, fun.(head, acc), fun)
  end
end

defimpl Collectable, for: Bag do
  def into(%Bag{} = bag) do
    collector = fn
      acc, {:cont, element} -> Bag.put(acc, element)
      acc, :done -> acc
      _acc, :halt -> :ok
    end

    {bag, collector}
  end
end

# Demonstrate Enumerable and Collectable protocols
IO.puts("=== Enumerable & Collectable Demo ===")

# Build a bag by putting elements
bag = Bag.new() |> Bag.put(:a) |> Bag.put(:a) |> Bag.put(:b) |> Bag.put(:c)

# Test Enumerable
assert Enum.count(bag) == 4
assert Enum.member?(bag, :a)
assert Enum.member?(bag, :d) == false

IO.puts("Enumerable.count: #{Enum.count(bag)}")

# Test Enum.to_list
list = Enum.to_list(bag) |> Enum.sort()
assert list == [:a, :a, :b, :c]
IO.puts("Enum.to_list: #{inspect(list)}")

# Test Enum.map
mapped = bag |> Enum.map(&to_string/1) |> Enum.sort()
IO.puts("Enum.map over bag: #{inspect(mapped)}")

# Test Enum.take (tests :halt)
taken = Enum.take(bag, 2)
assert length(taken) == 2
IO.puts("Enum.take(bag, 2): took #{length(taken)} elements")

# Test Collectable via Enum.into
new_bag = Enum.into([:x, :y, :y, :z], Bag.new())
assert Enum.count(new_bag) == 4
assert Enum.member?(new_bag, :y)
IO.puts("Enum.into created bag: counts = #{Enum.count(new_bag)}")

# Test for...into syntax
for_bag = for i <- 1..3, into: Bag.new(), do: rem(i, 2)
IO.puts("for...into created bag: #{Enum.count(for_bag)} elements")

# Test accumulation
start = Bag.new() |> Bag.put(:a)
accumulated = Enum.into([:a, :b], start)
assert Enum.count(accumulated) == 3
assert Enum.member?(accumulated, :a)
assert Enum.member?(accumulated, :b)

IO.puts("Enum.into on non-empty bag accumulated correctly")
IO.puts("All Enumerable & Collectable assertions passed!")
```


## Resources

- [`Enumerable` — Elixir stdlib](https://hexdocs.pm/elixir/Enumerable.html)
- [`Collectable` — Elixir stdlib](https://hexdocs.pm/elixir/Collectable.html)
- [`Stream` — lazy enumerables](https://hexdocs.pm/elixir/Stream.html)
- ["Writing assertive code with Elixir" — José Valim](http://blog.plataformatec.com.br/2014/09/writing-assertive-code-with-elixir/)


## Key Concepts

Protocols and behaviors are Elixir's mechanism for ad-hoc and static polymorphism. They solve different problems and are often confused.

**Protocols:**
Dispatch based on the type/struct of the first argument at runtime. A protocol defines a contract (e.g., `Enumerable`); any type can implement it by adding a corresponding implementation block. Protocols excel when you control neither the type nor the caller — e.g., a library that needs to iterate any collection. The fallback is `:any` — if no specific implementation exists, the `:any` handler is tried. This enables "optional" protocol implementations.

**Behaviours:**
Static polymorphism enforced at compile time. A module implements a behavior by defining callbacks (functions). Behaviors are about contracts between modules, not types. Use when you need multiple implementations of the same interface and the caller chooses which to use (e.g., different database adapters, different strategies). Callbacks are checked at compile time — missing a required callback is a compiler error.

**Architectural patterns:**
Behaviors excel in plugin systems (user defines modules conforming to the behavior). Protocols excel in type-driven dispatch (any type can conform). Mix both: a behavior can require that its callbacks operate on types that implement a protocol. Example: `MyAdapter` behavior requiring callbacks that work with `Enumerable` types.
