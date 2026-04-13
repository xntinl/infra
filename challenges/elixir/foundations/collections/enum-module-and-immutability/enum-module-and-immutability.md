# Enum and Immutability: Building a Data Analytics Pipeline

**Project**: `analytics` — an event log processor that groups, reduces, sorts, and filters using Enum

---

## Why immutability changes how you think about data

In Java or Python, you mutate objects in place: `list.sort()`, `map.put(k, v)`,
`user.setName("Alice")`. In Elixir, every operation returns a new value. The
original is never modified.

This is not a limitation — it is a guarantee:

1. **No defensive copying**: you can pass data to any function without fear of mutation
2. **Concurrent safety**: immutable data can be shared across processes without locks
3. **Time travel debugging**: every intermediate value still exists until GC collects it

The `Enum` module is the primary tool for transforming immutable collections. It
provides 70+ functions that cover virtually every collection operation. Understanding
when to use `map`, `reduce`, `filter`, `group_by`, and `flat_map` — and how to
compose them — is essential for writing production Elixir.

---

## The business problem

Your analytics system receives event logs as lists of maps. You need to:

1. Filter events by type, date range, and custom predicates
2. Group events by user, action, or time bucket
3. Aggregate metrics (counts, sums, averages) per group
4. Sort results by various criteria
5. Build summary reports combining multiple transformations

---

## Project structure

```
analytics/
├── lib/
│   └── analytics.ex
├── script/
│   └── main.exs
├── test/
│   └── analytics_test.exs
└── mix.exs
```

---

## Design decisions

**Option A — write each transformation as a named function that takes and returns a list**
- Pros: each step is individually testable; the pipeline reads top-to-bottom; discoverable for readers new to `Enum`.
- Cons: many small functions for what could be expressive one-liners; intermediate lists allocated at each step; performance penalty on large inputs.

**Option B — chain `Enum` functions via `|>` inside higher-level operations** (chosen)
- Pros: idiomatic Elixir; the pipeline reads like a SQL query; fewer moving parts; `Stream` can be swapped in where laziness matters without reshaping the code.
- Cons: intermediate `Enum` passes still allocate (only `Stream` avoids that); debugging a failing pipe stage requires breaking the chain or using `IO.inspect/2`.

Chose **B** because the language optimizes for this shape: the pipe operator, `Enum`'s uniform signatures, and `Stream`'s drop-in laziness all assume you compose transformations left-to-right. Naming each step is sometimes right, but only when the step carries non-trivial business meaning.

---

## Implementation

### `mix.exs`
```elixir
defmodule Analytics.MixProject do
  use Mix.Project

  def project do
    [
      app: :analytics,
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

### `lib/analytics.ex`

```elixir
defmodule Analytics do
  @moduledoc """
  Event log analytics using Enum transformations on immutable data.

  All functions are pure — they take data in and return data out.
  No state mutation, no side effects, no database calls.
  """

  @type event :: %{
          id: String.t(),
          user_id: String.t(),
          action: String.t(),
          timestamp: DateTime.t(),
          metadata: map()
        }

  @type metric :: %{
          group: term(),
          count: non_neg_integer(),
          events: [event()]
        }

  @doc """
  Filters events by a predicate function.

  The predicate receives an event and returns a boolean.

  ## Examples

      iex> events = [%{action: "click", user_id: "u1"}, %{action: "view", user_id: "u2"}]
      iex> Analytics.filter_by(events, fn e -> e.action == "click" end)
      [%{action: "click", user_id: "u1"}]

  """
  @spec filter_by([event()], (event() -> boolean())) :: [event()]
  def filter_by(events, predicate) when is_list(events) and is_function(predicate, 1) do
    Enum.filter(events, predicate)
  end

  @doc """
  Groups events by a key function and returns a map of group => events.

  ## Examples

      iex> events = [
      ...>   %{action: "click", user_id: "u1"},
      ...>   %{action: "view", user_id: "u1"},
      ...>   %{action: "click", user_id: "u2"}
      ...> ]
      iex> groups = Analytics.group_by_key(events, fn e -> e.action end)
      iex> length(groups["click"])
      2
      iex> length(groups["view"])
      1

  """
  @spec group_by_key([event()], (event() -> term())) :: %{term() => [event()]}
  def group_by_key(events, key_fn) when is_list(events) and is_function(key_fn, 1) do
    Enum.group_by(events, key_fn)
  end

  @doc """
  Counts events per group.

  Returns a list of {group_key, count} tuples sorted by count descending.

  ## Examples

      iex> events = [
      ...>   %{action: "click", user_id: "u1"},
      ...>   %{action: "click", user_id: "u2"},
      ...>   %{action: "view", user_id: "u1"}
      ...> ]
      iex> Analytics.count_by(events, fn e -> e.action end)
      [{"click", 2}, {"view", 1}]

  """
  @spec count_by([event()], (event() -> term())) :: [{term(), non_neg_integer()}]
  def count_by(events, key_fn) when is_list(events) and is_function(key_fn, 1) do
    events
    |> Enum.group_by(key_fn)
    |> Enum.map(fn {key, group} -> {key, length(group)} end)
    |> Enum.sort_by(fn {_key, count} -> count end, :desc)
  end

  @doc """
  Computes aggregate metrics per group.

  For each group, returns the count, and the list of events.

  ## Examples

      iex> events = [
      ...>   %{action: "purchase", user_id: "u1", metadata: %{amount: 100}},
      ...>   %{action: "purchase", user_id: "u1", metadata: %{amount: 200}},
      ...>   %{action: "purchase", user_id: "u2", metadata: %{amount: 50}}
      ...> ]
      iex> metrics = Analytics.aggregate(events, fn e -> e.user_id end)
      iex> Enum.find(metrics, fn m -> m.group == "u1" end).count
      2

  """
  @spec aggregate([event()], (event() -> term())) :: [metric()]
  def aggregate(events, group_fn) when is_list(events) and is_function(group_fn, 1) do
    events
    |> Enum.group_by(group_fn)
    |> Enum.map(fn {group, group_events} ->
      %{
        group: group,
        count: length(group_events),
        events: group_events
      }
    end)
    |> Enum.sort_by(fn m -> m.count end, :desc)
  end

  @doc """
  Sums a numeric field from event metadata across all events.

  Uses Enum.reduce/3 — the most general Enum function. Any Enum
  operation can be expressed as a reduce.

  ## Examples

      iex> events = [
      ...>   %{metadata: %{amount: 100}},
      ...>   %{metadata: %{amount: 200}},
      ...>   %{metadata: %{amount: 50}}
      ...> ]
      iex> Analytics.sum_field(events, [:metadata, :amount])
      350

  """
  @spec sum_field([map()], [atom()]) :: number()
  def sum_field(events, field_path) when is_list(events) and is_list(field_path) do
    Enum.reduce(events, 0, fn event, acc ->
      value = get_in(event, field_path) || 0
      acc + value
    end)
  end

  @doc """
  Returns the top N events sorted by a comparator function.

  ## Examples

      iex> events = [
      ...>   %{metadata: %{amount: 50}},
      ...>   %{metadata: %{amount: 300}},
      ...>   %{metadata: %{amount: 100}}
      ...> ]
      iex> top = Analytics.top_n(events, 2, fn e -> e.metadata.amount end)
      iex> length(top)
      2
      iex> hd(top).metadata.amount
      300

  """
  @spec top_n([event()], non_neg_integer(), (event() -> term())) :: [event()]
  def top_n(events, n, sort_fn) when is_list(events) and is_integer(n) do
    events
    |> Enum.sort_by(sort_fn, :desc)
    |> Enum.take(n)
  end

  @doc """
  Builds a full analytics report from raw events.

  Demonstrates composing multiple Enum operations into a single
  data transformation pipeline.

  Returns a map with:
    - `:total_events` — total event count
    - `:by_action` — count per action type
    - `:by_user` — count per user
    - `:total_amount` — sum of metadata.amount across all events

  ## Examples

      iex> events = [
      ...>   %{id: "1", action: "purchase", user_id: "u1", timestamp: ~U[2024-01-01 00:00:00Z], metadata: %{amount: 100}},
      ...>   %{id: "2", action: "purchase", user_id: "u2", timestamp: ~U[2024-01-01 01:00:00Z], metadata: %{amount: 200}},
      ...>   %{id: "3", action: "view", user_id: "u1", timestamp: ~U[2024-01-01 02:00:00Z], metadata: %{}}
      ...> ]
      iex> report = Analytics.build_report(events)
      iex> report.total_events
      3
      iex> report.total_amount
      300

  """
  @spec build_report([event()]) :: map()
  def build_report(events) when is_list(events) do
    %{
      total_events: length(events),
      by_action: count_by(events, fn e -> e.action end),
      by_user: count_by(events, fn e -> e.user_id end),
      total_amount: sum_field(events, [:metadata, :amount])
    }
  end

  @doc """
  Demonstrates the difference between Enum (eager) and Stream (lazy).

  For small datasets, Enum is faster (no overhead from lazy evaluation).
  For large datasets or when you only need the first N results,
  Stream avoids building intermediate collections.

  This function shows both approaches for comparison.
  """
  @spec unique_users_eager([event()]) :: [String.t()]
  def unique_users_eager(events) do
    events
    |> Enum.map(fn e -> e.user_id end)
    |> Enum.uniq()
    |> Enum.sort()
  end

  @spec unique_users_lazy([event()]) :: [String.t()]
  def unique_users_lazy(events) do
    events
    |> Stream.map(fn e -> e.user_id end)
    |> Stream.uniq()
    |> Enum.sort()
  end
end
```

**Why this works:**

- Every function takes data in and returns data out. No mutation. The original
  `events` list is never modified — each transformation creates a new list.
- `count_by/2` composes `Enum.group_by/2` -> `Enum.map/2` -> `Enum.sort_by/3`.
  Each step transforms the data shape without mutating the previous result.
- `sum_field/2` uses `Enum.reduce/3`, which is the Swiss army knife of Enum.
  Any Enum operation can be expressed as a reduce. When none of the specialized
  functions (`map`, `filter`, `group_by`) fit, reach for `reduce`.
- `build_report/1` calls multiple analytics functions on the same input. Because
  the input is immutable, there is no risk of one function's result affecting
  another's input.

### `test/analytics_test.exs`
```elixir
defmodule AnalyticsTest do
  use ExUnit.Case, async: true

  doctest Analytics

  @events [
    %{id: "1", action: "purchase", user_id: "u1", timestamp: ~U[2024-01-01 10:00:00Z], metadata: %{amount: 100}},
    %{id: "2", action: "purchase", user_id: "u2", timestamp: ~U[2024-01-01 11:00:00Z], metadata: %{amount: 250}},
    %{id: "3", action: "view", user_id: "u1", timestamp: ~U[2024-01-01 12:00:00Z], metadata: %{}},
    %{id: "4", action: "click", user_id: "u3", timestamp: ~U[2024-01-01 13:00:00Z], metadata: %{}},
    %{id: "5", action: "purchase", user_id: "u1", timestamp: ~U[2024-01-01 14:00:00Z], metadata: %{amount: 75}}
  ]

  describe "filter_by/2" do
    test "filters events by predicate" do
      result = Analytics.filter_by(@events, fn e -> e.action == "purchase" end)
      assert length(result) == 3
    end

    test "returns empty list when nothing matches" do
      result = Analytics.filter_by(@events, fn e -> e.action == "nonexistent" end)
      assert result == []
    end
  end

  describe "group_by_key/2" do
    test "groups by action" do
      groups = Analytics.group_by_key(@events, fn e -> e.action end)
      assert length(groups["purchase"]) == 3
      assert length(groups["view"]) == 1
      assert length(groups["click"]) == 1
    end

    test "groups by user" do
      groups = Analytics.group_by_key(@events, fn e -> e.user_id end)
      assert length(groups["u1"]) == 3
      assert length(groups["u2"]) == 1
    end
  end

  describe "count_by/2" do
    test "counts and sorts descending" do
      result = Analytics.count_by(@events, fn e -> e.action end)
      assert [{"purchase", 3}, {"click", 1}, {"view", 1}] = result
    end
  end

  describe "aggregate/2" do
    test "returns metrics per group" do
      metrics = Analytics.aggregate(@events, fn e -> e.user_id end)
      u1 = Enum.find(metrics, fn m -> m.group == "u1" end)
      assert u1.count == 3
    end
  end

  describe "sum_field/2" do
    test "sums nested field" do
      assert Analytics.sum_field(@events, [:metadata, :amount]) == 425
    end

    test "handles missing values as zero" do
      events = [%{metadata: %{}}, %{metadata: %{amount: 100}}]
      assert Analytics.sum_field(events, [:metadata, :amount]) == 100
    end
  end

  describe "top_n/3" do
    test "returns top N by sort function" do
      top = Analytics.top_n(@events, 2, fn e -> e.metadata[:amount] || 0 end)
      assert length(top) == 2
      amounts = Enum.map(top, fn e -> e.metadata[:amount] || 0 end)
      assert hd(amounts) >= List.last(amounts)
    end
  end

  describe "build_report/1" do
    test "builds complete report" do
      report = Analytics.build_report(@events)
      assert report.total_events == 5
      assert report.total_amount == 425
      assert is_list(report.by_action)
      assert is_list(report.by_user)
    end
  end

  describe "immutability" do
    test "original data is not modified by operations" do
      original = @events
      _filtered = Analytics.filter_by(@events, fn e -> e.action == "purchase" end)
      _sorted = Analytics.top_n(@events, 2, fn e -> e.metadata[:amount] || 0 end)
      assert original == @events
    end
  end

  describe "lazy vs eager" do
    test "both approaches return same result" do
      eager = Analytics.unique_users_eager(@events)
      lazy = Analytics.unique_users_lazy(@events)
      assert eager == lazy
    end
  end
end
```

### Run the tests

```bash
mix test --trace
```

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== Analytics: demo ===\n")

    result_1 = Analytics.filter_by(events, fn e -> e.action == "click" end)
    IO.puts("Demo 1: #{inspect(result_1)}")

    result_2 = Analytics.count_by(events, fn e -> e.action end)
    IO.puts("Demo 2: #{inspect(result_2)}")

    result_3 = Analytics.sum_field(events, [:metadata, :amount])
    IO.puts("Demo 3: #{inspect(result_3)}")

    IO.puts("\n=== Done ===")
  end
end

Main.main()
```

Run with: `elixir script/main.exs`

### Why this works

Every `Enum` function takes an enumerable, produces a new enumerable, and never touches the input. That turns each pipe stage into a pure function whose only contract is shape-in / shape-out — testable in isolation, composable in any order, safe to reuse from concurrent processes because immutability removes the shared-mutable-state class of bugs. The BEAM's per-process heap and persistent data structures mean "new list at each step" is cheap: structural sharing keeps allocations bounded, and short-lived garbage is collected almost for free.

---

## Benchmark

```elixir
# bench.exs
defmodule Bench do
  def run do
    events =
      for i <- 1..100_000 do
        %{id: i, user_id: rem(i, 500), type: Enum.random([:click, :view, :buy]), amount: i}
      end

    {enum_us, _} =
      :timer.tc(fn ->
        events
        |> Enum.filter(&(&1.type == :buy))
        |> Enum.group_by(& &1.user_id, & &1.amount)
        |> Enum.map(fn {uid, amts} -> {uid, Enum.sum(amts)} end)
      end)

    {stream_us, _} =
      :timer.tc(fn ->
        events
        |> Stream.filter(&(&1.type == :buy))
        |> Enum.group_by(& &1.user_id, & &1.amount)
        |> Enum.map(fn {uid, amts} -> {uid, Enum.sum(amts)} end)
      end)

    IO.puts("Enum   100k events: #{enum_us} µs")
    IO.puts("Stream 100k events: #{stream_us} µs")
  end
end

Bench.run()
```

Target: under 80 ms for the full Enum pipeline on 100k events. `Stream` shines when you add `.take(n)` at the end; with `group_by` as a terminal operation, the benefit shrinks.

---

## Lazy vs eager: when to use Stream

```elixir
# Eager (Enum) — builds a new list at each step
result =
  huge_list
  |> Enum.map(&transform/1)      # Allocates new list
  |> Enum.filter(&valid?/1)      # Allocates new list
  |> Enum.take(10)               # Only needed 10!

# Lazy (Stream) — builds computation, runs once at the end
result =
  huge_list
  |> Stream.map(&transform/1)    # No allocation yet
  |> Stream.filter(&valid?/1)    # No allocation yet
  |> Enum.take(10)               # Processes only until 10 found
```

Use `Stream` when:
- The input is very large (millions of elements)
- You only need a subset of results (`.take(n)`)
- You want to compose transformations without intermediate allocations

Use `Enum` when:
- The dataset fits comfortably in memory
- You need the full result
- Performance is not critical (Enum has less overhead per operation)

---

## Common production mistakes

**1. Multiple passes when one reduce suffices**
```elixir
# Bad — three passes over the data
count = Enum.count(events)
sum = Enum.sum(Enum.map(events, & &1.amount))
max = Enum.max_by(events, & &1.amount)

# Good — one pass
{count, sum, max} = Enum.reduce(events, {0, 0, nil}, fn e, {c, s, m} ->
  {c + 1, s + e.amount, if(m == nil or e.amount > m.amount, do: e, else: m)}
end)
```

**2. Using `Enum.count/1` to check emptiness**
`Enum.count(list) > 0` is O(n). `list != []` or `match?([_ | _], list)` is O(1).

**3. Forgetting that `Enum.sort/1` is not stable in all cases**
Elixir's sort is stable (preserves relative order of equal elements), but
`Enum.sort_by/3` with a key function that maps multiple elements to the same
key preserves their original relative order.

**4. Mutating accumulators in reduce (impossible but attempted)**
Coming from JavaScript, you might write `Map.put(acc, key, value)` thinking
you are mutating `acc`. You are not — `Map.put` returns a new map. This is
correct but sometimes confusing.

---

## Reflection

1. Your analytics module runs all pipelines in-process. Events now arrive at 10k/sec and a single summary takes 200 ms. Do you parallelize with `Task.async_stream/3`, switch to `Flow`, precompute per-bucket aggregates incrementally, or push the aggregation to the database? What are the failure modes of each?
2. An `Enum.reduce/3` one-liner combines filter+group+sum into a single pass. At what point does consolidating multiple `Enum` passes into a single reduce become worth the readability hit? Describe a concrete signal that the refactor pays off.

---

```elixir
defmodule Analytics do
  @moduledoc """
  Event log analytics using Enum transformations on immutable data.

  All functions are pure — they take data in and return data out.
  No state mutation, no side effects, no database calls.
  """

  @type event :: %{
          id: String.t(),
          user_id: String.t(),
          action: String.t(),
          timestamp: DateTime.t(),
          metadata: map()
        }

  @type metric :: %{
          group: term(),
          count: non_neg_integer(),
          events: [event()]
        }

  @doc """
  Filters events by a predicate function.

  The predicate receives an event and returns a boolean.

  ## Examples

      iex> events = [%{action: "click", user_id: "u1"}, %{action: "view", user_id: "u2"}]
      iex> Analytics.filter_by(events, fn e -> e.action == "click" end)
      [%{action: "click", user_id: "u1"}]

  """
  @spec filter_by([event()], (event() -> boolean())) :: [event()]
  def filter_by(events, predicate) when is_list(events) and is_function(predicate, 1) do
    Enum.filter(events, predicate)
  end

  @doc """
  Groups events by a key function and returns a map of group => events.

  ## Examples

      iex> events = [
      ...>   %{action: "click", user_id: "u1"},
      ...>   %{action: "view", user_id: "u1"},
      ...>   %{action: "click", user_id: "u2"}
      ...> ]
      iex> groups = Analytics.group_by_key(events, fn e -> e.action end)
      iex> length(groups["click"])
      2
      iex> length(groups["view"])
      1

  """
  @spec group_by_key([event()], (event() -> term())) :: %{term() => [event()]}
  def group_by_key(events, key_fn) when is_list(events) and is_function(key_fn, 1) do
    Enum.group_by(events, key_fn)
  end

  @doc """
  Counts events per group.

  Returns a list of {group_key, count} tuples sorted by count descending.

  ## Examples

      iex> events = [
      ...>   %{action: "click", user_id: "u1"},
      ...>   %{action: "click", user_id: "u2"},
      ...>   %{action: "view", user_id: "u1"}
      ...> ]
      iex> Analytics.count_by(events, fn e -> e.action end)
      [{"click", 2}, {"view", 1}]

  """
  @spec count_by([event()], (event() -> term())) :: [{term(), non_neg_integer()}]
  def count_by(events, key_fn) when is_list(events) and is_function(key_fn, 1) do
    events
    |> Enum.group_by(key_fn)
    |> Enum.map(fn {key, group} -> {key, length(group)} end)
    |> Enum.sort_by(fn {_key, count} -> count end, :desc)
  end

  @doc """
  Computes aggregate metrics per group.

  For each group, returns the count, and the list of events.

  ## Examples

      iex> events = [
      ...>   %{action: "purchase", user_id: "u1", metadata: %{amount: 100}},
      ...>   %{action: "purchase", user_id: "u1", metadata: %{amount: 200}},
      ...>   %{action: "purchase", user_id: "u2", metadata: %{amount: 50}}
      ...> ]
      iex> metrics = Analytics.aggregate(events, fn e -> e.user_id end)
      iex> Enum.find(metrics, fn m -> m.group == "u1" end).count
      2

  """
  @spec aggregate([event()], (event() -> term())) :: [metric()]
  def aggregate(events, group_fn) when is_list(events) and is_function(group_fn, 1) do
    events
    |> Enum.group_by(group_fn)
    |> Enum.map(fn {group, group_events} ->
      %{
        group: group,
        count: length(group_events),
        events: group_events
      }
    end)
    |> Enum.sort_by(fn m -> m.count end, :desc)
  end

  @doc """
  Sums a numeric field from event metadata across all events.

  Uses Enum.reduce/3 — the most general Enum function. Any Enum
  operation can be expressed as a reduce.

  ## Examples

      iex> events = [
      ...>   %{metadata: %{amount: 100}},
      ...>   %{metadata: %{amount: 200}},
      ...>   %{metadata: %{amount: 50}}
      ...> ]
      iex> Analytics.sum_field(events, [:metadata, :amount])
      350

  """
  @spec sum_field([map()], [atom()]) :: number()
  def sum_field(events, field_path) when is_list(events) and is_list(field_path) do
    Enum.reduce(events, 0, fn event, acc ->
      value = get_in(event, field_path) || 0
      acc + value
    end)
  end

  @doc """
  Returns the top N events sorted by a comparator function.

  ## Examples

      iex> events = [
      ...>   %{metadata: %{amount: 50}},
      ...>   %{metadata: %{amount: 300}},
      ...>   %{metadata: %{amount: 100}}
      ...> ]
      iex> top = Analytics.top_n(events, 2, fn e -> e.metadata.amount end)
      iex> length(top)
      2
      iex> hd(top).metadata.amount
      300

  """
  @spec top_n([event()], non_neg_integer(), (event() -> term())) :: [event()]
  def top_n(events, n, sort_fn) when is_list(events) and is_integer(n) do
    events
    |> Enum.sort_by(sort_fn, :desc)
    |> Enum.take(n)
  end

  @doc """
  Builds a full analytics report from raw events.

  Demonstrates composing multiple Enum operations into a single
  data transformation pipeline.

  Returns a map with:
    - `:total_events` — total event count
    - `:by_action` — count per action type
    - `:by_user` — count per user
    - `:total_amount` — sum of metadata.amount across all events

  ## Examples

      iex> events = [
      ...>   %{id: "1", action: "purchase", user_id: "u1", timestamp: ~U[2024-01-01 00:00:00Z], metadata: %{amount: 100}},
      ...>   %{id: "2", action: "purchase", user_id: "u2", timestamp: ~U[2024-01-01 01:00:00Z], metadata: %{amount: 200}},
      ...>   %{id: "3", action: "view", user_id: "u1", timestamp: ~U[2024-01-01 02:00:00Z], metadata: %{}}
      ...> ]
      iex> report = Analytics.build_report(events)
      iex> report.total_events
      3
      iex> report.total_amount
      300

  """
  @spec build_report([event()]) :: map()
  def build_report(events) when is_list(events) do
    %{
      total_events: length(events),
      by_action: count_by(events, fn e -> e.action end),
      by_user: count_by(events, fn e -> e.user_id end),
      total_amount: sum_field(events, [:metadata, :amount])
    }
  end

  @doc """
  Demonstrates the difference between Enum (eager) and Stream (lazy).

  For small datasets, Enum is faster (no overhead from lazy evaluation).
  For large datasets or when you only need the first N results,
  Stream avoids building intermediate collections.

  This function shows both approaches for comparison.
  """
  @spec unique_users_eager([event()]) :: [String.t()]
  def unique_users_eager(events) do
    events
    |> Enum.map(fn e -> e.user_id end)
    |> Enum.uniq()
    |> Enum.sort()
  end

  @spec unique_users_lazy([event()]) :: [String.t()]
  def unique_users_lazy(events) do
    events
    |> Stream.map(fn e -> e.user_id end)
    |> Stream.uniq()
    |> Enum.sort()
  end
end
```

## Resources

- [Enum — HexDocs](https://hexdocs.pm/elixir/Enum.html)
- [Stream — HexDocs](https://hexdocs.pm/elixir/Stream.html)
- [Enumerables and Streams — Elixir Getting Started](https://elixir-lang.org/getting-started/enumerables-and-streams.html)

---

## Why Enum and Immutability matters

Mastering **Enum and Immutability** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

---

## Key concepts
### 1. Enum Returns New Collections, Never Mutates

In imperative languages, iterating often modifies in place. In Elixir, every Enum function returns a new collection. This guarantees functions are composable, you can safely share collections between processes, and testing is deterministic.

### 2. Lazy vs Eager Evaluation

`Enum` is eager—all operations execute immediately. `Stream` is lazy—operations compose and execute only when consumed. For large datasets or infinite sequences, use Streams. For finite collections you need immediately, use Enum.

### 3. Enum.reduce is the Workhorse

Every aggregation (sum, group, filter-and-map) can be built with `reduce`. Mastering `reduce` means you understand how to fold any data structure into any shape.

---
