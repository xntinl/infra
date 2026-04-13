# MapSet Operations for Set Arithmetic

**Project**: `visitor_tracker` — returning/new/total visitor metrics via `MapSet` set algebra

---

## Project structure

```
visitor_tracker/
├── lib/
│   └── visitor_tracker.ex         # set operations over daily visitors
├── test/
│   └── visitor_tracker_test.exs   # ExUnit tests
└── mix.exs
```

---

## What you will learn

Two core concepts:

1. **`MapSet`** — an unordered collection of unique values with O(log n) membership,
   insertion, and removal. Backed by Erlang's persistent map. Use it whenever you find
   yourself calling `Enum.uniq/1` more than once on the same data.
2. **Set algebra** — `MapSet.union/2`, `MapSet.intersection/2`, `MapSet.difference/2`.
   These are the building blocks for "which users visited on both days", "who's new this week",
   "which pages appear in both cohorts".

---

## The business problem

Your analytics service ingests a list of visitor IDs per day. Product needs three metrics
every morning for yesterday and the day before:

1. **Returning visitors** — users present on both days (intersection).
2. **New visitors** — users present today but not yesterday (difference).
3. **Total reach** — unique users across both days (union).

The raw input is a plain list of IDs with duplicates (the same user may open the app five
times). Doing `Enum.uniq/1` + `Enum.filter/2` + `Enum.member?/2` for each question re-scans
the list repeatedly. `MapSet` gives you O(log n) operations and deduplicates for free.

---

## Why `MapSet` and not a plain `Map` with `true` values

You can fake a set with `%{id => true}`, but:

- You have to remember the convention everywhere you use it.
- `Map.merge/2` is **not** set union if the values ever differ.
- There's no idiomatic `intersection` — you'd reimplement it.

`MapSet` gives you a type-safe API (`MapSet.member?/2`, `MapSet.union/2`) and correct
semantics out of the box. It costs nothing extra at runtime — internally it's a map.

---

## Why not plain lists with `Enum.uniq/1` + `--`

`list1 -- list2` is O(n * m). For 100k visitors per day that's 10 billion comparisons.
`MapSet.difference/2` is O(n log m). On the same input: ~1.7 million operations.
Four orders of magnitude faster, and you stop worrying about it in code review.

---

## Design decisions

**Option A — lists + `Enum.uniq/1` + `--`**
- Pros: zero new types; every Elixir developer knows lists cold; fine for small inputs (<1k).
- Cons: `list1 -- list2` is O(n × m); on 100k visitors per day that's 10 billion comparisons; no semantic type ("is this a set or a bag?" is a comment, not a type).

**Option B — `MapSet` with `union/2`, `intersection/2`, `difference/2`** (chosen)
- Pros: O(n log n) build and O(log n) membership; the type name documents intent; operations are exactly the words the domain uses (returning = intersection, new = difference); internally a map, so allocation profile is familiar.
- Cons: not directly JSON-serializable (must call `MapSet.to_list/1`); no stable iteration order; one more type for newcomers to learn.

Chose **B** because the problem is literally set algebra and the algorithmic gap between the two options is four orders of magnitude at production scale. The type hint alone makes review easier.

---

## Implementation

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Standard library: no external dependencies required
    {:"jason", "~> 1.0"},
  ]
end
```


### Step 1 — Create the project

**Objective**: Build single module so MapSet vs list trade-off is visible and O(n log n) set algebra cost is proven.

```bash
mix new visitor_tracker
cd visitor_tracker
```

### Step 2 — `lib/visitor_tracker.ex`

**Objective**: Use domain-driven names (returning/new_today/reach) so MapSet.union/intersection/difference map directly to business questions.

```elixir
defmodule VisitorTracker do
  @moduledoc """
  Daily unique-visitor tracking with set algebra.
  """

  @type visitor_id :: integer() | String.t()

  @doc """
  Builds a MapSet from a raw list of visitor IDs (duplicates OK).

  We accept an Enumerable so callers can pass a lazy stream from a DB cursor
  without materializing the full list in memory.
  """
  @spec from_events(Enumerable.t()) :: MapSet.t(visitor_id())
  def from_events(events), do: MapSet.new(events)

  @doc """
  Visitors that appeared on BOTH days — "returning users".

  Intersection is commutative: order of args doesn't matter.
  """
  @spec returning(MapSet.t(), MapSet.t()) :: MapSet.t()
  def returning(yesterday, today), do: MapSet.intersection(yesterday, today)

  @doc """
  Visitors that appear only `today` — "new users".

  Difference is NOT commutative: `difference(today, yesterday)` =
  "today minus yesterday", which is what we want here.
  """
  @spec new_today(MapSet.t(), MapSet.t()) :: MapSet.t()
  def new_today(yesterday, today), do: MapSet.difference(today, yesterday)

  @doc """
  All unique visitors across both days — "total reach".
  """
  @spec total_reach(MapSet.t(), MapSet.t()) :: MapSet.t()
  def total_reach(yesterday, today), do: MapSet.union(yesterday, today)

  @doc """
  Full daily report as a plain map of sizes (cheap to log, easy to compare).

  We return counts, not the sets themselves — callers rarely need the IDs,
  and serializing a 100k-element MapSet to JSON is a footgun.
  """
  @spec report(MapSet.t(), MapSet.t()) :: %{
          returning: non_neg_integer(),
          new_today: non_neg_integer(),
          total_reach: non_neg_integer(),
          retention: float()
        }
  def report(yesterday, today) do
    returning_count = MapSet.size(returning(yesterday, today))
    yesterday_count = MapSet.size(yesterday)

    # Guard against empty yesterday — dividing by zero would crash the whole batch.
    retention =
      if yesterday_count == 0, do: 0.0, else: returning_count / yesterday_count

    %{
      returning: returning_count,
      new_today: MapSet.size(new_today(yesterday, today)),
      total_reach: MapSet.size(total_reach(yesterday, today)),
      retention: retention
    }
  end
end
```

### Step 3 — `test/visitor_tracker_test.exs`

**Objective**: Exercise the empty-yesterday branch so retention avoids division-by-zero and the aggregation stays robust on day one.

```elixir
defmodule VisitorTrackerTest do
  use ExUnit.Case, async: true

  setup do
    # Raw events with duplicates (same user opens the app multiple times)
    yesterday = VisitorTracker.from_events([1, 2, 3, 3, 4, 4, 4])
    today = VisitorTracker.from_events([3, 4, 5, 5, 6])
    {:ok, yesterday: yesterday, today: today}
  end

  test "from_events/1 deduplicates", %{yesterday: y} do
    assert MapSet.size(y) == 4
  end

  test "returning/2 finds users present both days", %{yesterday: y, today: t} do
    assert VisitorTracker.returning(y, t) == MapSet.new([3, 4])
  end

  test "new_today/2 finds users only in today", %{yesterday: y, today: t} do
    assert VisitorTracker.new_today(y, t) == MapSet.new([5, 6])
  end

  test "total_reach/2 unions both days", %{yesterday: y, today: t} do
    assert VisitorTracker.total_reach(y, t) == MapSet.new([1, 2, 3, 4, 5, 6])
  end

  test "report/2 computes counts and retention", %{yesterday: y, today: t} do
    assert VisitorTracker.report(y, t) == %{
             returning: 2,
             new_today: 2,
             total_reach: 6,
             retention: 0.5
           }
  end

  test "report/2 handles empty yesterday without crashing" do
    empty = MapSet.new()
    today = MapSet.new([1, 2, 3])
    assert VisitorTracker.report(empty, today).retention == 0.0
  end
end
```

### Step 4 — Run the tests

**Objective**: Run the suite to confirm set-algebra semantics match the domain wording (returning, new, reach) before any benchmarking.

```bash
mix test
```

All 6 tests should pass.

### Why this works

`MapSet` is backed by Erlang's persistent map — the same data structure as `%{}` — so insertion and membership are O(log n) with a small constant. Operations that look like set algebra (`union`, `intersection`, `difference`) are implemented in terms of map merge/filter primitives, not list scans, so they stay logarithmic. Building the set deduplicates for free: `Enum.into(list, MapSet.new())` doesn't care how many duplicates the list has. Because values are compared by structural equality, visitor IDs being atoms, integers, or strings all work uniformly as long as you're consistent.

---


## Key Concepts

### 1. MapSets Are Unordered, Unique Collections
MapSets automatically deduplicate and are implemented as hash arrays, providing O(1) membership testing and insertion.

### 2. MapSet vs List for Membership Testing
List: O(n) membership check. MapSet: O(1) membership check. If you frequently test membership, use a MapSet.

### 3. Set Operations
`MapSet.union`, `MapSet.intersection`, `MapSet.difference` are efficient on MapSets. For data deduplication and set math, MapSets are the right choice.

---
## Benchmark

```elixir
# bench.exs
defmodule Bench do
  def run do
    today = Enum.map(1..100_000, &(rem(&1, 80_000)))       # duplicates + overlap
    yesterday = Enum.map(1..100_000, &(rem(&1, 80_000) + 20_000))

    {mapset_us, _} =
      :timer.tc(fn ->
        s1 = MapSet.new(today)
        s2 = MapSet.new(yesterday)
        {MapSet.intersection(s1, s2), MapSet.difference(s1, s2), MapSet.union(s1, s2)}
      end)

    {list_us, _} =
      :timer.tc(fn ->
        t = Enum.uniq(today)
        y = Enum.uniq(yesterday)
        {t -- (t -- y), t -- y, Enum.uniq(t ++ y)}
      end)

    IO.puts("MapSet algebra, 100k each: #{mapset_us} µs")
    IO.puts("List algebra,   100k each: #{list_us} µs (expect >10,000× slower)")
  end
end

Bench.run()
```

Target: MapSet algebra under 200 ms for 100k visitors on both sides. The list version will either take minutes or be the kind of slow that makes CI time out — that IS the lesson.

---

## Trade-offs

| Operation | `MapSet` | `Enum.uniq/1` + `--` | Plain `Map` |
|-----------|----------|---------------------|-------------|
| Build from list with duplicates | O(n log n) | O(n) + O(n²) lookup later | O(n log n) |
| Membership check | O(log n) | O(n) | O(log n) |
| Intersection / Difference | O(n log n) | O(n²) | not built-in |
| Serializable to JSON | Needs `MapSet.to_list/1` | native | native |

---

## Common production mistakes

**1. Using `list -- other_list` on large inputs**  
Quadratic. Works fine in tests (100 items), catches fire in production (100k items).

**2. Relying on MapSet order**  
MapSet has no defined iteration order. If you need sorted output, convert to a list and sort
at the boundary: `set |> MapSet.to_list() |> Enum.sort()`. Do not assume insertion order.

**3. Pattern-matching against `%MapSet{}` internals**  
The struct fields are private implementation detail. Use the `MapSet.*` API only —
the internal representation has changed across OTP versions and may change again.

**4. Serializing a `MapSet` directly with Jason/JSON**  
Most encoders won't know how to handle the struct. Convert with `MapSet.to_list/1` at the
serialization boundary.

---

## When NOT to use `MapSet`

- When order matters and duplicates are meaningful (event log, request history). Use a list.
- When you need a value per key (not just membership). Use a `Map`.
- When the collection is tiny (< 20 items) and built once. `Enum.uniq/1` + `Enum.member?/2`
  is readable and fast enough; don't over-engineer.

---

## Reflection

1. You now have 100k active users and the daily visitor set fits in memory. At 10M users the sets (~80 MB each) still fit but GC pressure is noticeable. Would you keep `MapSet` in-process, move to Redis sets, or maintain an ETS table? What trade-off breaks first?
2. The intersection/difference pattern works for pairwise comparison. Product now asks "users present in ALL of the last 7 days". Do you fold `MapSet.intersection/2` across the 7 sets, compute from raw logs with a single-pass reduce counting per-user appearances, or push the problem to the database? Which approach scales best?

---

## Resources

- [`MapSet` docs](https://hexdocs.pm/elixir/MapSet.html)
- [Elixir School — MapSet](https://elixirschool.com/en/lessons/data_processing/sets)
- Erlang's [`gb_sets` comparison](https://www.erlang.org/doc/man/gb_sets.html) — MapSet uses maps, `gb_sets` uses balanced trees
