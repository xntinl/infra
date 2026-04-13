# Enum.reduce Patterns for Aggregation

**Project**: `event_aggregator` — aggregating event logs with `Enum.reduce/3` and `Enum.reduce_while/3`

---

## Project structure

```
event_aggregator/
├── lib/
│   └── event_aggregator.ex        # all aggregation logic lives here
├── test/
│   └── event_aggregator_test.exs  # ExUnit tests
└── mix.exs
```

---

## What you will learn

Two core concepts:

1. **`Enum.reduce/3`** — the "swiss army knife" of Enum. Every `map`, `filter`, `group_by` can be
   expressed as a reduce. You need it when none of the high-level helpers fit.
2. **`Enum.reduce_while/3`** — reduce with early termination. Critical when the input is large
   and you want to stop as soon as a condition is met (saves CPU and memory).

You will also see the accumulator trade-off: using a **map** as accumulator (O(log n) updates)
vs a **list** that you reverse at the end (idiomatic for order-preserving builds).

---

## The business problem

You operate a product analytics pipeline. Every day you receive a list of events:

```elixir
[
  %{type: :click, user_id: 42, ts: 1_700_000_000},
  %{type: :purchase, user_id: 42, ts: 1_700_000_050, amount: 19.90},
  %{type: :click, user_id: 7, ts: 1_700_000_100},
  ...
]
```

You need:

1. A count of events grouped by `:type` — for the dashboard.
2. Revenue statistics (`count`, `sum`, `avg`) for `:purchase` events only.
3. A "first N unique users" scanner that stops early once N distinct users are seen —
   the input stream can have millions of events, you do not want to walk all of them.

---

## Why `reduce` and not `group_by` + `map`

`Enum.group_by/2` followed by `Enum.map/2` traverses the list twice and allocates a full map of
lists (`%{click: [e1, e2, ...], purchase: [e3, ...]}`) just to throw most of it away.
For a counter you only need a `%{type => integer}` map, which a single `reduce` builds in one pass.

Rule of thumb: if you only need aggregated numbers (counters, sums, min/max), reach for `reduce`.
If you need the grouped items themselves for downstream work, `group_by` is fine.

---

## Design decisions

**Option A — `Enum.group_by/2` then `Enum.map/2` to reduce each group**
- Pros: reads declaratively; the two passes mirror how SQL would express it (GROUP BY + aggregate); each pass is independently testable.
- Cons: allocates `%{type => [event, event, ...]}` — the full partitioned input — only to immediately discard it; wastes memory proportional to input size when you only want counters.

**Option B — single `Enum.reduce/3` with a map accumulator `%{type => count}`** (chosen)
- Pros: one pass; memory proportional to the number of groups, not events; composes naturally with `reduce_while/3` for early exit; same shape handles counters, sums, and more complex aggregates uniformly.
- Cons: the reducer function is slightly denser; less obvious than `group_by` at a glance; requires discipline to always prepend-and-reverse when building lists inside the accumulator.

Chose **B** because the aggregate shape (`count`, `sum`, `avg`) never needs the grouped rows themselves — keeping them would burn memory for nothing. When downstream code DOES need the groups, `group_by` is right; this file is about the counter case.

---

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

**Objective**: Build minimal library so reducer lives alone and accumulator-shape decisions are isolated from framework noise.

```bash
mix new event_aggregator
cd event_aggregator
```

### Step 2 — `lib/event_aggregator.ex`

**Objective**: Build counters/revenue/early-exit as single-pass reduce to avoid double-walk cost of group_by+map.

```elixir
defmodule EventAggregator do
  @moduledoc """
  Aggregations over event streams using Enum.reduce/reduce_while.
  """

  @doc """
  Counts events grouped by their `:type` field.

  Returns a map `%{type => count}`. Single pass, O(n) time, O(k) memory
  where k = number of distinct types (typically small).
  """
  @spec count_by_type([map()]) :: %{atom() => non_neg_integer()}
  def count_by_type(events) do
    # Map accumulator: Map.update/4 is O(log n) but n = number of types (small).
    # We avoid the double traversal of group_by + map_values.
    Enum.reduce(events, %{}, fn %{type: type}, acc ->
      Map.update(acc, type, 1, &(&1 + 1))
    end)
  end

  @doc """
  Revenue statistics for `:purchase` events.

  Returns `%{count: n, sum: s, avg: a}`. If there are no purchases, `avg` is `0.0`
  to avoid a division-by-zero crash on empty datasets — fail fast, but don't crash
  on a legitimately empty input.
  """
  @spec revenue_stats([map()]) :: %{count: non_neg_integer(), sum: float(), avg: float()}
  def revenue_stats(events) do
    # Tuple accumulator {count, sum} — cheaper than a map when keys are fixed.
    {count, sum} =
      Enum.reduce(events, {0, 0.0}, fn
        %{type: :purchase, amount: amount}, {c, s} -> {c + 1, s + amount}
        _other, acc -> acc
      end)

    avg = if count == 0, do: 0.0, else: sum / count
    %{count: count, sum: sum, avg: avg}
  end

  @doc """
  Walks the stream and returns the first `n` distinct user_ids, stopping as soon as
  `n` are found. Uses `reduce_while` to avoid scanning the rest of the input.

  Returns a list in the order users were first seen.
  """
  @spec first_unique_users(Enumerable.t(), pos_integer()) :: [integer()]
  def first_unique_users(events, n) when n > 0 do
    # Accumulator: {MapSet for O(1) membership test, list in reverse for O(1) prepend}.
    # We reverse once at the end — the classic "build reversed, flip once" pattern.
    {_seen, acc} =
      Enum.reduce_while(events, {MapSet.new(), []}, fn %{user_id: uid}, {seen, acc} ->
        cond do
          MapSet.member?(seen, uid) ->
            {:cont, {seen, acc}}

          MapSet.size(seen) + 1 == n ->
            # Found the nth unique user — halt immediately.
            {:halt, {seen, [uid | acc]}}

          true ->
            {:cont, {MapSet.put(seen, uid), [uid | acc]}}
        end
      end)

    Enum.reverse(acc)
  end
end
```

### Step 3 — `test/event_aggregator_test.exs`

**Objective**: Prove `reduce_while` truly halts by feeding a million-event tail that must never be inspected when N is satisfied.

```elixir
defmodule EventAggregatorTest do
  use ExUnit.Case, async: true

  describe "count_by_type/1" do
    test "counts events by type in a single pass" do
      events = [
        %{type: :click, user_id: 1},
        %{type: :click, user_id: 2},
        %{type: :purchase, user_id: 1, amount: 10.0},
        %{type: :view, user_id: 3}
      ]

      assert EventAggregator.count_by_type(events) == %{click: 2, purchase: 1, view: 1}
    end

    test "returns empty map for empty input" do
      assert EventAggregator.count_by_type([]) == %{}
    end
  end

  describe "revenue_stats/1" do
    test "ignores non-purchase events" do
      events = [
        %{type: :click, user_id: 1},
        %{type: :purchase, user_id: 1, amount: 10.0},
        %{type: :purchase, user_id: 2, amount: 30.0}
      ]

      assert EventAggregator.revenue_stats(events) == %{count: 2, sum: 40.0, avg: 20.0}
    end

    test "returns zeros (not NaN) when there are no purchases" do
      assert EventAggregator.revenue_stats([%{type: :click, user_id: 1}]) ==
               %{count: 0, sum: 0.0, avg: 0.0}
    end
  end

  describe "first_unique_users/2" do
    test "stops as soon as n distinct users are found" do
      events =
        [%{user_id: 1}, %{user_id: 1}, %{user_id: 2}, %{user_id: 3}] ++
          # These extras should never be inspected
          for(i <- 1..1_000_000, do: %{user_id: i})

      assert EventAggregator.first_unique_users(events, 3) == [1, 2, 3]
    end

    test "returns fewer than n when the stream is exhausted" do
      events = [%{user_id: 1}, %{user_id: 2}]
      assert EventAggregator.first_unique_users(events, 5) == [1, 2]
    end
  end
end
```

### Step 4 — Run the tests

**Objective**: Confirm the early-halt test finishes under the reduce-all budget, proving the accumulator shape is the hot path.

```bash
mix test
```

All 6 tests should pass.

### Why this works

`Enum.reduce/3` walks the input once, threading the accumulator through each call to the reducer. Using a map for counters keeps per-step work at O(log n) on the key count (small in practice) and avoids the intermediate list that `group_by` would materialize. `Enum.reduce_while/3` adds the `{:halt, acc}` escape hatch: the scanner for unique users returns as soon as it has N, so an input of a million events can terminate after processing the first few hundred. The tuple accumulator for revenue stats (`{count, sum}`) avoids map allocation churn when you know the shape is fixed — `{c + 1, s + amount}` is pure stack arithmetic.

---



---
## Key Concepts

### 1. `reduce` is the Universal Aggregator

Every aggregation (sum, count, group, partition) can be built with `reduce`. It's the most powerful Enum function because it composes any operation.

```elixir
Enum.reduce([1, 2, 3], 0, fn x, acc -> acc + x end)  # sum
```

### 2. Accumulator Must Include All State

If you need to track multiple pieces of information while reducing, put them in the accumulator (a tuple or map). At the end, extract what you need.

### 3. `reduce` is Linear, Not Composable

While powerful, `reduce` does not compose well with pipes—you must write the accumulator logic inline. Use higher-level functions (`map`, `filter`, `group_by`) when available for readability.

---
## Benchmark

```elixir
# bench.exs
defmodule Bench do
  def run do
    events =
      for i <- 1..1_000_000 do
        %{type: Enum.random([:click, :purchase, :view]), user_id: rem(i, 10_000), amount: 10.0}
      end

    {reduce_us, _} =
      :timer.tc(fn -> EventAggregator.count_by_type(events) end)

    {first_n_us, _} =
      :timer.tc(fn -> EventAggregator.first_unique_users(events, 500) end)

    IO.puts("count_by_type 1M events:            #{reduce_us} µs")
    IO.puts("first_unique_users (N=500, 1M in):  #{first_n_us} µs")
  end
end

Bench.run()
```

Target: `count_by_type` under 300 ms for 1M events (dominated by map updates); `first_unique_users` under 5 ms for N=500 because `reduce_while` halts long before the 1M tail runs. The second number IS the point — it's bound to N, not to input size.

---

## Trade-offs

| Accumulator shape | When to use | Cost |
|-------------------|-------------|------|
| Map `%{key => value}` | Aggregating by dynamic keys (group-by, counters) | O(log n) per update, small n in practice |
| Tuple `{a, b}` | Fixed number of aggregates (sum+count, min+max) | O(1) per update, no allocation churn |
| `[item \| acc]` + reverse | Building an ordered list from a stream | O(1) prepend, one O(n) reverse at end |
| `list ++ [item]` | **Never** — O(n) per step, O(n²) total | Catastrophic on large inputs |

---

## Common mistakes

**1. Appending to a list with `++` inside reduce**  
`acc ++ [item]` copies the whole list on each iteration. 100k events = 100k list copies.
Always prepend (`[item | acc]`) and reverse at the end.

**2. Using `reduce` when `reduce_while` is needed**  
If you only need the first N results, `reduce` walks the entire input anyway. On infinite
or very large streams this either blocks forever or wastes CPU.

**3. Rebuilding the map on every step with `Map.put(acc, k, (acc[k] || 0) + 1)`**  
Works, but does two map lookups per step. `Map.update/4` does one.

---

## When NOT to use reduce

If you're computing a single summary that already has a dedicated helper — `Enum.sum/1`,
`Enum.count/1`, `Enum.min/1` — use the helper. It's shorter and equally fast. Reduce is for
aggregations the standard library doesn't cover.

If you need to preserve all items and only transform them, use `Enum.map/2`. Reduce to build
a list is a code smell unless you're also filtering or combining.

---

## Reflection

1. Your `count_by_type` reducer uses `Map.update/4`. A teammate suggests `Map.put(acc, k, (acc[k] || 0) + 1)` because it "reads more clearly". Benchmark both on 1M events — which wins, and why? Is the winner also the one you'd merge?
2. `first_unique_users` halts after N. What happens if the input is a `Stream` emitting events as an external API returns them, and the API is slow? Does `Enum.reduce_while/3` still short-circuit, or does it force the stream to materialize first? Design a test that distinguishes the two behaviours.

---

## Resources

- [`Enum.reduce/3` docs](https://hexdocs.pm/elixir/Enum.html#reduce/3)
- [`Enum.reduce_while/3` docs](https://hexdocs.pm/elixir/Enum.html#reduce_while/3)
- José Valim — ["Comparing tail-recursive and body-recursive functions"](https://dashbit.co/blog/elixir-reduce-vs-map-reduce)
- *Elixir in Action* (Sasa Juric), chapter on higher-order functions
