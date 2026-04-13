# Range queries on `:ordered_set` with `select/2` and match specs

**Project**: `ordered_set_range` — use an `:ordered_set` plus a hand-written
match spec to answer "give me every event between timestamps A and B" in
O(log N + K) time.

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

## Why `:ordered_set` + match spec and not X

**Why not `:set` + `Enum.filter`?** A `:set` cannot walk keys in order; any
range query becomes a full table scan regardless of how small the window is.
For a 1M-row event log with a 100-event window, that's 10 000× more work.

**Why not a SQL store for "events between A and B"?** For in-memory, short-lived,
node-local event windows (rate limiters, tail buffers, debug traces), ETS is
orders of magnitude faster and requires no schema or connection pool.

**Why not an in-process ordered structure like `:gb_trees`?** Because multiple
consumer processes need to read the same events without routing through one
serializer. ETS gives you shared-memory + concurrent reads in one primitive.

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

Match specs are notoriously hard to write by hand. `:ets.fun2ms/1`
compiles an Elixir/Erlang fun into the equivalent match spec at compile time —
much nicer. But you should be able to read a raw match spec in logs and in
OTP library internals, so this exercise writes them by hand deliberately.

---

## Design decisions

**Option A — Hand-written match spec**
- Pros: Explicit, no library dependency, forces understanding of the shape.
- Cons: Easy to get wrong (double braces, prefix operators, `:"$N"` quoting).

**Option B — `:ets.fun2ms/1` / `Ex2ms.fun`** (chosen in real code, but not here)
- Pros: Write a normal Elixir fun, compiler generates the spec.
- Cons: Hides the shape; when you have to debug a spec in prod logs you still
  need to read raw match specs.

→ Chose **A (hand-written) for this exercise** because reading raw match specs
is a required skill — you'll encounter them in OTP library internals and in
tracing (`:dbg`). `fun2ms` is introduced in a later exercise.

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


### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — isolated from any external state, so we demonstrate this concept cleanly without dependencies.


```bash
mix new ordered_set_range
cd ordered_set_range
```

### Step 2: `lib/ordered_set_range.ex`

**Objective**: Implement `ordered_set_range.ex` — the access pattern that exposes the trade-off between ETS concurrency flags, match specs, and lookup cost.


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

  For anything more complex than this, reach for `:ets.fun2ms/1`
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

**Objective**: Write `ordered_set_range_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


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

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
```

### Why this works

`:ordered_set` stores keys in term order inside a balanced tree, so
`:ets.select/2` can start at `from`, walk in order, and stop when the key
exceeds `to`. The match spec's key-bound guards (`:>=` and `:"=<"` on
`:"$1"`) are the signal the engine uses to decide it can prune — without
them it would scan the whole table. Returning `{{:"$1", :"$2"}}` as the body
preserves the original tuple shape so the caller sees familiar data.

---


## Deep Dive: ETS Concurrency Trade-Offs and Operation Atomicity

ETS (Erlang Term Storage) is mutable, shared, in-process state—antithetical to Elixir's immutability. But it's required for specific cases: large shared datasets, fast lookups under contention, atomic counters across processes. Trade-off: operations aren't composable (no atomic multi-table updates without extra bookkeeping), and debugging is harder because mutations are invisible in code.

Use ETS when: (1) true sharing between processes, (2) data is large (megabytes), (3) sub-millisecond latency required. Use GenServer when: (1) single process owns state, (2) dataset is small, (3) complex transition logic.

Most common mistake: using ETS to work around GenServer bottlenecks without profiling. Profile usually shows either handler logic is expensive (move it out) or contention from N processes calling it. ETS solves contention via sharding: split state across tables/processes indexed by key. Always profile before choosing ETS.

## Benchmark

```elixir
# Compare a 1% window on :ordered_set vs :set full-scan filter.
t_os = :ets.new(:os, [:ordered_set, :public])
t_s  = :ets.new(:s,  [:set, :public])
for i <- 1..100_000 do
  :ets.insert(t_os, {i, :event})
  :ets.insert(t_s,  {i, :event})
end

{us_os, _} = :timer.tc(fn ->
  :ets.select(t_os, [{{:"$1", :"$2"}, [{:>=, :"$1", 50_000}, {:"=<", :"$1", 51_000}], [{{:"$1", :"$2"}}]}])
end)
{us_s, _} = :timer.tc(fn ->
  :ets.select(t_s, [{{:"$1", :"$2"}, [{:>=, :"$1", 50_000}, {:"=<", :"$1", 51_000}], [{{:"$1", :"$2"}}]}])
end)

IO.puts("ordered_set: #{us_os}µs  set: #{us_s}µs")
```

Target esperado: sobre 100k filas con ventana del 1%, `:ordered_set` debería
completar en <1ms mientras `:set` escanea la tabla completa (~5–20ms). La
relación debería ser al menos 10× a favor de `:ordered_set`.

---

## Key Concepts

Ordered sets (`ordered_set` tables) maintain sort order on keys, enabling powerful range queries. Instead of scanning the entire table, `ets:match_spec/2` with range patterns allows queries like "all keys between X and Y" in O(log n) time—far faster than scanning a regular `set` table. This is crucial for time-series data, pagination, and sorted indexes. The binary search-like behavior means 1M keys might need only ~20 comparisons to find a range. The trade-off: inserts are slightly slower because keys must be maintained in sort order, and memory usage is slightly higher. Use `ordered_set` when you need sorted iteration or range queries; use plain `set` for equality lookups only. In production systems tracking time-based data (events, metrics, logs), `ordered_set` becomes invaluable for efficient pagination and time-window queries.

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
`:ets.fun2ms/1` — it translates a normal Elixir fun into the same match
spec you'd write by hand, with less pain.

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

## Reflection

- Your service stores 10M audit events keyed by `{user_id, timestamp}` and
  serves two query shapes: "everything for user U" and "everything in
  window [A, B] across all users". Would a single `:ordered_set` handle
  both efficiently? Design the alternative if not.
- Under what circumstances would you shard a single `:ordered_set` into
  multiple tables by time bucket (e.g. one per hour) to escape the write
  serialization penalty? At what write rate does this pay off?

---

## Executable Example

Copy the code below into a file (e.g., `solution.exs`) and run with `elixir solution.exs`:

```elixir
defmodule Main do
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

    For anything more complex than this, reach for `:ets.fun2ms/1`
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

  def main do
    # Demo: range queries en :ordered_set
    t = OrderedSetRange.new()
  
    # Insertar eventos con timestamps
    for {ts, msg} <- [{10, :a}, {20, :b}, {30, :c}, {40, :d}, {50, :e}] do
      OrderedSetRange.put(t, ts, msg)
    end
  
    # Consultas de rango
    range_20_40 = OrderedSetRange.range(t, 20, 40)
    assert range_20_40 == [{20, :b}, {30, :c}, {40, :d}], "rango debe incluir límites"
  
    range_10_30 = OrderedSetRange.range(t, 10, 30)
    assert range_10_30 == [{10, :a}, {20, :b}, {30, :c}], "rango debe estar en orden"
  
    range_outside = OrderedSetRange.range(t, 60, 70)
    assert range_outside == [], "rango fuera de datos debe estar vacío"
  
    :ets.delete(t)
  
    IO.puts("OrderedSetRange: demostración de consultas de rango exitosa")
    IO.puts("  rango [20, 40]: #{inspect(range_20_40)}")
    IO.puts("  rango [10, 30]: #{inspect(range_10_30)}")
  end

end

Main.main()
```


## Resources

- [Erlang match spec — official reference](https://www.erlang.org/doc/apps/erts/match_spec.html)
- [`:ets.select/2`](https://www.erlang.org/doc/man/ets.html#select-2)
- [`:ets.select/3` and continuations](https://www.erlang.org/doc/man/ets.html#select-3)
- ["Learn You Some Erlang — ETS"](https://learnyousomeerlang.com/ets) — covers match specs in the ETS chapter
- [Erlang term ordering](https://www.erlang.org/doc/reference_manual/expressions.html#term-comparisons) — why `:ordered_set` ordering works the way it does
