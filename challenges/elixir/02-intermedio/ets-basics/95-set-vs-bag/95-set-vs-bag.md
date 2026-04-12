# ETS table types — `:set`, `:ordered_set`, `:bag`, `:duplicate_bag`

**Project**: `set_vs_bag` — four tiny demos that show exactly how each ETS
table type behaves when you insert duplicates, look up by key, and traverse
in order.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Project context

Picking the wrong ETS table type is the #1 silent performance bug in Elixir
systems that use ETS heavily. A `:bag` where a `:set` would do wastes memory
and forces list traversal on every lookup; an `:ordered_set` where a `:set`
would do gives you O(log N) reads when O(1) was free. And `:duplicate_bag`
is almost never the right answer, yet it shows up in codebases because
"bag sounded close enough".

In this exercise you'll implement a tiny "tag store" four times — once per
table type — and write tests that pin down the exact semantic difference.
By the end you'll know, from muscle memory, which type to reach for.

Project structure:

```
set_vs_bag/
├── lib/
│   ├── set_vs_bag.ex
│   └── set_vs_bag/tag_store.ex
├── test/
│   └── set_vs_bag_test.exs
└── mix.exs
```

---

## Core concepts

### 1. The four types in one table

| Type              | Duplicate keys? | Duplicate tuples? | Ordered? | Lookup cost |
|-------------------|-----------------|-------------------|----------|-------------|
| `:set`            | No              | No                | No       | O(1) (hash) |
| `:ordered_set`    | No              | No                | Yes (by key) | O(log N) |
| `:bag`            | Yes             | No (dedup)        | No       | O(1) + list |
| `:duplicate_bag`  | Yes             | Yes               | No       | O(1) + list |

Source: [erlang.org/doc/man/ets.html — table types](https://www.erlang.org/doc/man/ets.html#new-2).

### 2. `:set` — one value per key

The default. `insert/2` on an existing key silently overwrites the old
tuple. Internally a hash table. Use this when your data model says
"each key has exactly one current value" — caches, registries, session
stores.

### 3. `:ordered_set` — one value per key, sorted

Same uniqueness rules as `:set`, but internally a balanced tree (AA-tree in
OTP). Keys are kept in **Erlang term order** (numbers < atoms < refs < …
< tuples < lists < binaries — see [Erlang reference](https://www.erlang.org/doc/reference_manual/expressions.html#term-comparisons)).
This unlocks range queries via `:ets.select/2` and `first/next` traversal
in sorted order. Trade-off: lookups are O(log N) instead of O(1), and
key comparison uses term compare, not `==`, which matters for floats vs
integers (`1` and `1.0` are considered equal on an `:ordered_set` key).

### 4. `:bag` — many values per key, no duplicate tuples

`insert(t, {:color, :red})` twice stores the tuple **once**. But
`insert(t, {:color, :red})` and `insert(t, {:color, :blue})` keep both.
`lookup/2` returns the full list of tuples for that key. Good for
"multi-value" indexes where the value set is already meaningful as a set.

### 5. `:duplicate_bag` — everything goes in

Every insert adds a new tuple, duplicates and all. Useful when tuples
carry timestamps or sequence numbers and you truly want an append-only
log per key. Rarely what you want; almost always a `:bag` (if you're
deduping) or a purpose-built log (if you're not) is clearer.

---

## Implementation

### Step 1: Create the project

```bash
mix new set_vs_bag
cd set_vs_bag
```

### Step 2: `lib/set_vs_bag/tag_store.ex`

```elixir
defmodule SetVsBag.TagStore do
  @moduledoc """
  A "which tags does this item have?" store, implemented four ways so you can
  compare table types side by side. Tuples are `{item_id, tag}`.

  The *behavior* of `add_tag/3` changes dramatically based on the table type,
  even though the code is identical. That's the whole point.
  """

  @type type :: :set | :ordered_set | :bag | :duplicate_bag

  @doc "Creates a store of the given ETS type."
  @spec new(type()) :: :ets.tid()
  def new(type) when type in [:set, :ordered_set, :bag, :duplicate_bag] do
    :ets.new(:tag_store, [type, :public])
  end

  @doc "Adds a tag for an item. The *effect* depends on the table type."
  @spec add_tag(:ets.tid(), term(), term()) :: true
  def add_tag(t, item_id, tag), do: :ets.insert(t, {item_id, tag})

  @doc "Returns all tuples for an item. Always a list, always `{item_id, tag}`."
  @spec tags_for(:ets.tid(), term()) :: [{term(), term()}]
  def tags_for(t, item_id), do: :ets.lookup(t, item_id)

  @doc """
  Traverses the entire table in key order. Meaningful only for `:ordered_set`;
  for the other types the traversal order is implementation-defined.
  """
  @spec all_in_order(:ets.tid()) :: [tuple()]
  def all_in_order(t) do
    # tab2list walks the whole table; for :ordered_set the order is by key,
    # for the others it's whatever the hash iteration happens to produce.
    :ets.tab2list(t)
  end
end
```

### Step 3: `lib/set_vs_bag.ex`

```elixir
defmodule SetVsBag do
  @moduledoc """
  Top-level helpers that *explain* the differences by constructing identical
  inputs against all four table types and returning what each one stored.
  Think of it as a live truth table.
  """

  alias SetVsBag.TagStore

  @doc """
  Inserts `{:item1, :red}` twice and `{:item1, :blue}` once into a table of
  the requested type, then returns `tags_for(:item1)`. The shape of the
  returned list is the signature of the table type.
  """
  @spec demo(TagStore.type()) :: [{term(), term()}]
  def demo(type) do
    t = TagStore.new(type)

    TagStore.add_tag(t, :item1, :red)
    TagStore.add_tag(t, :item1, :red)
    TagStore.add_tag(t, :item1, :blue)

    result = TagStore.tags_for(t, :item1)
    :ets.delete(t)
    result
  end
end
```

### Step 4: `test/set_vs_bag_test.exs`

```elixir
defmodule SetVsBagTest do
  use ExUnit.Case, async: true

  alias SetVsBag.TagStore

  describe "demo/1 — duplicate semantics per type" do
    test ":set keeps the last tuple only (unique key)" do
      # All three inserts share the same key :item1, so only the last wins.
      assert SetVsBag.demo(:set) == [{:item1, :blue}]
    end

    test ":ordered_set keeps the last tuple only (same key rule as :set)" do
      assert SetVsBag.demo(:ordered_set) == [{:item1, :blue}]
    end

    test ":bag deduplicates *identical tuples* but keeps distinct ones" do
      # `{:item1, :red}` inserted twice counts as once; `{:item1, :blue}` is distinct.
      result = SetVsBag.demo(:bag)
      assert Enum.sort(result) == [{:item1, :blue}, {:item1, :red}]
    end

    test ":duplicate_bag keeps EVERY insert, duplicates and all" do
      result = SetVsBag.demo(:duplicate_bag)
      # Two reds (both inserts survive) + one blue.
      assert Enum.sort(result) == [{:item1, :blue}, {:item1, :red}, {:item1, :red}]
    end
  end

  describe ":ordered_set — traversal order" do
    test "tab2list returns keys in Erlang term order" do
      t = TagStore.new(:ordered_set)
      for k <- [:c, :a, :b, :d], do: TagStore.add_tag(t, k, :x)

      # Atoms compare lexicographically by name in Erlang term order.
      assert TagStore.all_in_order(t) ==
               [{:a, :x}, {:b, :x}, {:c, :x}, {:d, :x}]

      :ets.delete(t)
    end

    test ":ordered_set treats 1 and 1.0 as the same key" do
      # Term comparison on :ordered_set uses `==`-like equality for numbers,
      # not `===`. This is a classic gotcha — noted in the OTP ets docs.
      t = TagStore.new(:ordered_set)
      :ets.insert(t, {1, :int})
      :ets.insert(t, {1.0, :float})

      # Second insert overwrites the first because the keys are "equal" here.
      assert :ets.lookup(t, 1) == [{1.0, :float}]
      :ets.delete(t)
    end
  end
end
```

### Step 5: Run

```bash
mix test
```

---

## Trade-offs and production gotchas

**1. `:ordered_set` integer/float equality will bite you**
On `:ordered_set`, the key `1` and the key `1.0` refer to the **same slot**
(term comparison is `==`, not `===`). On `:set` they're distinct keys.
If you're keying by floats or mixing numerics, either normalize types or
use `:set`. This is called out explicitly in the
[ets docs for `:ordered_set`](https://www.erlang.org/doc/man/ets.html#new-2).

**2. `:bag` lookups are a list — watch the cost**
`lookup(bag, key)` returns the full list of tuples for that key, and each
tuple is copied out of ETS memory. A key with 10k entries is 10k copies per
lookup. If your bag keys grow unboundedly, you want a different data model.

**3. `:duplicate_bag` is almost never what you want**
If you need dedup, use `:bag`. If you need an append-only log per key with
order preserved, ETS isn't really the right tool — a per-key queue process
or a proper log store fits better. The most common correct use of
`:duplicate_bag` is stats sampling where order doesn't matter and exact
duplicate counts do.

**4. `:ordered_set` does not support `:write_concurrency`**
With `write_concurrency: true` you get better write parallelism on `:set`,
`:bag`, and `:duplicate_bag`, but the flag is ignored for `:ordered_set`.
For write-hot workloads, this makes `:ordered_set` a concurrency
bottleneck. See exercise 100 for the full access/concurrency story.

**5. When NOT to differentiate**
If your store is small (a few thousand entries) and read-light, the type
barely matters — `:set` is the safe default. The differences become
significant at scale (>100k entries), under high concurrency, or when you
need range queries (exercise 96) or match patterns (exercise 97).

---

## Resources

- [Erlang `ets` — `new/2` table types](https://www.erlang.org/doc/man/ets.html#new-2)
- ["Learn You Some Erlang — ETS"](https://learnyousomeerlang.com/ets) — walks through the four types with examples
- [Erlang term ordering reference](https://www.erlang.org/doc/reference_manual/expressions.html#term-comparisons) — required reading for `:ordered_set`
- [Fred Hébert — "Erlang in Anger", ETS chapter](https://www.erlang-in-anger.com/) — operational consequences of each type
