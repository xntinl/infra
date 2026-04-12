# Range queries on `:ordered_set` with `select/2` and match specs

**Project**: `ordered_set_range` — use an `:ordered_set` plus a hand-written
match spec to answer "give me every event between timestamps A and B" in
O(log N + K) time.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Project context

You have events keyed by monotonic timestamp and you want to retrieve a time
window: `events between 10:00 and 10:05`. On a `:set`, the only way is a full
scan. On an `:ordered_set`, you can use `:ets.select/2` with a match spec that
compares the key against a range, and OTP will walk only the relevant slice
of the tree — the same shape of operation a SQL B-tree index does for a
`BETWEEN` clause.

Match specs look alien the first time; this exercise keeps them simple and
focuses on the two patterns that cover 80% of real usage: **range on the key**
and **range plus a value filter**.

Project structure:

```
ordered_set_range/
├── lib/
│   └── ordered_set_range.ex
├── test/
│   └── ordered_set_range_test.exs
└── mix.exs
```

---

## Core concepts

### 1. Why `:ordered_set` enables range queries

`:ordered_set` stores tuples in a tree sorted by key in Erlang term order. That
means the tree can be traversed from any starting key onward, and the engine
can stop as soon as the key exceeds the upper bound. Internally `:ets.select/2`
on an `:ordered_set` with a range guard does exactly that — no full scan.

### 2. Anatomy of a match spec

A match spec is a list of `{match_head, guards, body}` triples:

```elixir
[
  {
    {:"$1", :"$2"},            # match head: pattern to match each stored tuple
    [{:>=, :"$1", 10}, {:"=<", :"$1", 20}],  # guards: boolean tests on bindings
    [{{:"$1", :"$2"}}]         # body: shape to return (double braces = literal tuple)
  }
]
```

- `:"$1"`, `:"$2"`, ... are **binding variables** — they capture the parts of
  the tuple.
- `:"_"` is a wildcard that matches anything and discards it.
- The guard list uses an Erlang-y syntax: `{:>=, lhs, rhs}` not `lhs >= rhs`.
- The body controls what `select/2` returns per match. `[{{...}}]` — note the
  double braces — returns a tuple, because a single-element list that is a
  tuple literal would otherwise be confused with the list form.

Full rules: [erlang.org/doc/apps/erts/match_spec.html](https://www.erlang.org/doc/apps/erts/match_spec.html).

### 3. `:ets.select/2` on `:ordered_set` is range-aware

When the guards bound the key (`:"$1"` in position 1) with `>=` / `<` / `=<`,
the engine uses the tree ordering to walk only the matching slice. You do not
need any special "range" API — just a match spec where the guards restrict the
key, and use `:ordered_set` as the table type.

### 4. When a hand-written match spec is OK vs when to use `fun2ms`

Match specs are notoriously hard to write by hand. `:ets.fun2ms/1` (exercise 98)
compiles an Elixir/Erlang fun into the equivalent match spec at compile time —
much nicer. But you should be able to read a raw match spec in logs and in
OTP library internals, so this exercise writes them by hand deliberately.

---

## Implementation

### Step 1: Create the project

```bash
mix new ordered_set_range
cd ordered_set_range
```

### Step 2: `lib/ordered_set_range.ex`

```elixir
defmodule OrderedSetRange do
  @moduledoc """
  An event store keyed by integer timestamp. Uses `:ordered_set` so that
  `range/3` can use a match spec with `:ets.select/2` and touch only the
  events inside the requested window.

  Events are stored as `{timestamp, payload}`. Timestamps must be unique
  (it's a set by timestamp); if you need multiple events at the same
  timestamp, key by `{timestamp, counter}` instead.
  """

  @type event :: {integer(), term()}

  @doc "Creates a new ordered-set event table."
  @spec new() :: :ets.tid()
  def new do
    :ets.new(:events, [:ordered_set, :public])
  end

  @doc "Stores an event. Overwrites any existing event at the same timestamp."
  @spec put(:ets.tid(), integer(), term()) :: true
  def put(table, ts, payload), do: :ets.insert(table, {ts, payload})

  @doc """
  Returns all events with `from <= timestamp <= to`, in ascending timestamp order.

  This uses a hand-written match spec. On an `:ordered_set` the engine walks
  only the relevant slice of the tree — O(log N + K) where K is the number of
  matches — not O(N).
  """
  @spec range(:ets.tid(), integer(), integer()) :: [event()]
  def range(table, from, to) do
    match_spec = [
      {
        # Match head: every tuple in the table shaped `{ts, payload}`.
        # :"$1" captures the timestamp, :"$2" captures the payload.
        {:"$1", :"$2"},
        # Guards: the range condition. Erlang-style prefix notation.
        [{:>=, :"$1", from}, {:"=<", :"$1", to}],
        # Body: what to return — the original tuple. Double-braces = tuple literal.
        [{{:"$1", :"$2"}}]
      }
    ]

    :ets.select(table, match_spec)
  end

  @doc """
  Returns events in `[from, to]` whose payload is a map containing `:level`
  equal to `level`. Demonstrates a range PLUS a value-shape filter in the
  same match spec.

  For anything more complex than this, reach for `:ets.fun2ms/1` (exercise 98)
  — hand-writing deep match specs is a debugging nightmare.
  """
  @spec range_with_level(:ets.tid(), integer(), integer(), atom()) :: [event()]
  def range_with_level(table, from, to, level) do
    # Here the match head pins the payload to a map shape with a :level key.
    # Match specs cannot destructure arbitrary maps richly, so we keep this
    # example intentionally simple — the value filter is done in the guard.
    match_spec = [
      {
        {:"$1", :"$2"},
        [
          {:>=, :"$1", from},
          {:"=<", :"$1", to},
          # `map_get/2` is allowed in match spec guards since OTP 22.
          {:==, {:map_get, :level, :"$2"}, level}
        ],
        [{{:"$1", :"$2"}}]
      }
    ]

    :ets.select(table, match_spec)
  end
end
```

### Step 3: `test/ordered_set_range_test.exs`

```elixir
defmodule OrderedSetRangeTest do
  use ExUnit.Case, async: true

  setup do
    t = OrderedSetRange.new()
    on_exit(fn -> if :ets.info(t) != :undefined, do: :ets.delete(t) end)
    %{t: t}
  end

  describe "range/3" do
    test "returns only events inside [from, to], in ascending order", %{t: t} do
      for {ts, msg} <- [{10, :a}, {20, :b}, {30, :c}, {40, :d}, {50, :e}] do
        OrderedSetRange.put(t, ts, msg)
      end

      assert OrderedSetRange.range(t, 20, 40) ==
               [{20, :b}, {30, :c}, {40, :d}]
    end

    test "boundaries are inclusive", %{t: t} do
      for ts <- [1, 2, 3], do: OrderedSetRange.put(t, ts, :x)
      assert OrderedSetRange.range(t, 1, 3) == [{1, :x}, {2, :x}, {3, :x}]
      assert OrderedSetRange.range(t, 1, 1) == [{1, :x}]
    end

    test "empty range returns []", %{t: t} do
      OrderedSetRange.put(t, 100, :x)
      assert OrderedSetRange.range(t, 0, 10) == []
    end
  end

  describe "range_with_level/4" do
    test "filters by payload shape plus range", %{t: t} do
      OrderedSetRange.put(t, 1, %{level: :info, msg: "a"})
      OrderedSetRange.put(t, 2, %{level: :warn, msg: "b"})
      OrderedSetRange.put(t, 3, %{level: :info, msg: "c"})
      OrderedSetRange.put(t, 4, %{level: :error, msg: "d"})

      assert OrderedSetRange.range_with_level(t, 1, 4, :info) ==
               [{1, %{level: :info, msg: "a"}}, {3, %{level: :info, msg: "c"}}]
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

**1. Range optimization only applies when the KEY is bounded**
`:ordered_set` walks a slice only when the guard restricts `:"$1"` (or whatever
position the key is in, per `:keypos`). A guard on `:"$2"` still forces a full
scan. If you need range queries on a non-key field, either change your key
design or accept the full scan (and bound the table size).

**2. `:ordered_set` is O(log N) even for point lookups**
The cost you pay for range queries: `lookup/2` is O(log N) instead of the
O(1) you'd get on `:set`. For workloads that are 99% point lookups and 1%
ranges, you may be better off with `:set` + a secondary index table.

**3. Match specs don't play with rich Elixir pattern matching**
Match specs are a low-level DSL; they predate maps and structs. You cannot
destructure a map with more than a handful of keys cleanly, and struct
matching requires matching `:__struct__`. For anything elaborate, use
`:ets.fun2ms/1` — it translates `fn {ts, v} when ts in from..to -> v end`
into the same match spec you'd write by hand, with less pain.

**4. `:ordered_set` and `:write_concurrency`**
`write_concurrency: true` is silently ignored on `:ordered_set`; the tree
requires serialized writes. Under write-heavy concurrent load, this makes
`:ordered_set` a bottleneck — consider sharding by prefix (one table per
hour/bucket) if writes dominate.

**5. Don't `select/2` without bounds on a big table**
An unbounded select on a million-row table returns a copy of a million
tuples into the caller's heap. Use `:ets.select/3` (with a limit + continuation)
when you don't know the result size — it streams chunks.

**6. When NOT to use `:ordered_set` + match spec**
If your "range" is always "the last N events", a bounded ring-buffer inside
a GenServer is simpler and faster. ETS range queries shine when the window
is arbitrary and ad-hoc.

---

## Resources

- [Erlang match spec — official reference](https://www.erlang.org/doc/apps/erts/match_spec.html)
- [`:ets.select/2`](https://www.erlang.org/doc/man/ets.html#select-2)
- [`:ets.select/3` and continuations](https://www.erlang.org/doc/man/ets.html#select-3)
- ["Learn You Some Erlang — ETS"](https://learnyousomeerlang.com/ets) — covers match specs in the ETS chapter
- [Erlang term ordering](https://www.erlang.org/doc/reference_manual/expressions.html#term-comparisons) — why `:ordered_set` ordering works the way it does
