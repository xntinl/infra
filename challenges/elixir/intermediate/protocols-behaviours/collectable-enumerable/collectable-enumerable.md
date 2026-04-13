# `Enumerable` and `Collectable` — a custom `Bag` collection

**Project**: `custom_bag` — a multiset (`Bag`) struct that fully implements `Enumerable` (so `Enum.*` works) and `Collectable` (so `Enum.into/2` works).

---

## Why Enumerable and Collectable matter

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

---

## The business problem

Domain-specific collections (multisets, ordered sets, count-min sketches, ring
buffers) show up in real systems: tag counters, dedup layers, rate-limiters,
and metric aggregators. If they don't integrate with `Enum` and `for`, every
caller has to peek inside the internal map — a leak that couples consumers to
an implementation detail and blocks future refactors.

`custom_bag` closes that leak by implementing both protocols correctly,
including the tricky `:suspend` branch that makes `Stream.zip/2` work.

---

## Project structure

```
custom_bag/
├── lib/
│   └── bag.ex
├── script/
│   └── main.exs
├── test/
│   └── bag_test.exs
└── mix.exs
```

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

## Implementation

### `mix.exs`

```elixir
defmodule CustomBag.MixProject do
  use Mix.Project

  def project do
    [
      app: :custom_bag,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    []
  end
end
```
### `lib/bag.ex`

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
  def count(%Bag{} = bag), do: {:ok, Bag.size(bag)}

  def member?(%Bag{counts: counts}, element) do
    {:ok, Map.has_key?(counts, element)}
  end

  def slice(_bag), do: {:error, __MODULE__}

  def reduce(%Bag{counts: counts}, acc, fun) do
    counts
    |> Enum.flat_map(fn {element, n} -> List.duplicate(element, n) end)
    |> do_reduce(acc, fun)
  end

  defp do_reduce(_list, {:halt, acc}, _fun), do: {:halted, acc}

  defp do_reduce(list, {:suspend, acc}, fun) do
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
### `test/bag_test.exs`

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

    test "Enum.take/2 halts early (uses :halt)" do
      bag = Bag.new() |> Bag.put(:a) |> Bag.put(:a) |> Bag.put(:a) |> Bag.put(:a)
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
### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== Enumerable & Collectable Demo ===\n")

    bag = Bag.new() |> Bag.put(:a) |> Bag.put(:a) |> Bag.put(:b) |> Bag.put(:c)

    IO.puts("Enum.count: #{Enum.count(bag)}")
    IO.puts("Enum.to_list: #{inspect(Enum.to_list(bag) |> Enum.sort())}")

    mapped = bag |> Enum.map(&to_string/1) |> Enum.sort()
    IO.puts("Enum.map: #{inspect(mapped)}")

    taken = Enum.take(bag, 2)
    IO.puts("Enum.take(bag, 2): #{length(taken)} elements")

    new_bag = Enum.into([:x, :y, :y, :z], Bag.new())
    IO.puts("Enum.into: count = #{Enum.count(new_bag)}")

    IO.puts("\n=== Done ===")
  end
end

Main.main()
```
Run with: `elixir script/main.exs`

---

## Key concepts

### 1. `Enumerable.reduce/3` is the heart of `Enum`

Every `Enum` function is built on top of `reduce/3`. You must honor the three
accumulator states: `:cont` means keep going, `:halt` means stop now,
`:suspend` means pause and return a continuation. Suspension is what makes
`Stream.zip/2` work without loading both sides.

### 2. `count/1` and `member?/2` can opt out

Returning `{:error, __MODULE__}` from `count/1` or `member?/2` tells `Enum`
"I can't answer in O(1), please derive this from `reduce/3`". Always opt
out when the O(1) answer requires traversing anyway.

### 3. `Collectable.into/1` returns a two-arity collector

The collector handles `{:cont, elem}` (add), `:done` (finalize), and `:halt`
(cleanup on error). For pure data structures, return `:ok` on halt. For file
handles or ports, release resources.

### 4. Protocols vs behaviours

Protocols dispatch on the type of the first argument at runtime. Behaviours
enforce contracts between modules at compile time. Use protocols for
type-driven dispatch (any type can conform); behaviours for plugin systems
(user defines modules conforming to the contract).

### 5. Production gotchas

- Materializing to a list defeats streaming — fine for small bags, bad for millions.
- `{:ok, count}` is a promise of O(1); return `{:error, __MODULE__}` if it isn't.
- `:halt` in Collectable is for cleanup — ignoring it leaks handles.
- If iteration has no natural order (e.g. a graph), don't force `Enumerable` on it.

---

## Resources

- [`Enumerable` — Elixir stdlib](https://hexdocs.pm/elixir/Enumerable.html)
- [`Collectable` — Elixir stdlib](https://hexdocs.pm/elixir/Collectable.html)
- [`Stream` — lazy enumerables](https://hexdocs.pm/elixir/Stream.html)
- ["Writing assertive code with Elixir" — José Valim](http://blog.plataformatec.com.br/2014/09/writing-assertive-code-with-elixir/)
