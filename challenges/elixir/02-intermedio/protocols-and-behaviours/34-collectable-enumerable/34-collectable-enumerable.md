# `Enumerable` and `Collectable` — a custom `Bag` collection

**Project**: `custom_bag` — a multiset (`Bag`) struct that fully implements `Enumerable` (so `Enum.*` works) and `Collectable` (so `Enum.into/2` works).

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

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
minimum correct version; exercise 79 extends the idea to a tree with a
full lazy `reduce/3`.

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

## Implementation

### Step 1: Create the project

```bash
mix new custom_bag
cd custom_bag
```

### Step 2: `lib/bag.ex`

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

```bash
mix test
```

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

## Resources

- [`Enumerable` — Elixir stdlib](https://hexdocs.pm/elixir/Enumerable.html)
- [`Collectable` — Elixir stdlib](https://hexdocs.pm/elixir/Collectable.html)
- [`Stream` — lazy enumerables](https://hexdocs.pm/elixir/Stream.html)
- ["Writing assertive code with Elixir" — José Valim](http://blog.plataformatec.com.br/2014/09/writing-assertive-code-with-elixir/)
