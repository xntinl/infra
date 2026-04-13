# ETS Match Specs with `:ets.fun2ms` and `:ets.select`

**Project**: `session_store` — an in-memory session store where the cleanup path and analytics queries avoid copying the full table.

## Project context

You store user sessions in an ETS table: `{session_id, user_id, expires_at, metadata}`. The table has 500,000 live entries at peak. Two operations must not copy the whole table:

1. **Cleanup** — every minute, delete all entries where `expires_at < now`. Copying 500k rows into the cleanup process, filtering, then calling `:ets.delete/2` for each match would stall the GenServer for tens of milliseconds and thrash the VM's heap.
2. **Analytics** — "how many sessions are held by user 42?". A naive `:ets.tab2list/1` + `Enum.filter/2` is O(n) in memory; a match spec runs inside ETS without copying non-matching rows.

`:ets.match/2`, `:ets.select/2` and the variants `select_delete/2`, `select_count/2`, `select_replace/2` all take a **match specification** — a compact DSL that runs inside ETS in C. Match specs are notoriously hard to write by hand. `:ets.fun2ms/1` is a parse transform that turns a restricted subset of Elixir/Erlang fun expressions into match specs at compile time.

```
session_store/
├── lib/
│   └── session_store/
│       ├── application.ex
│       ├── store.ex
│       └── analytics.ex
├── test/
│   └── session_store/
│       └── store_test.exs
├── bench/
│   └── matchspec_bench.exs
└── mix.exs
```

## Why match specs and not `:ets.tab2list` + Enum

`tab2list/1` copies every row into the calling process. For 500k rows each ~200 B this is 100 MB of data copied into the process heap, triggering a major GC. Match specs run inside ETS; only matching rows cross the heap boundary.

## Why `fun2ms` and not hand-written match specs

A hand-written match spec for "expired sessions" looks like:

```erlang
[{{'$1', '$2', '$3', '$4'}, [{'<', '$3', {const, 1700000000}}], [true]}]
```

The `fun2ms` form:

```elixir
:ets.fun2ms(fn {_id, _user, exp, _meta} when exp < 1_700_000_000 -> true end)
```

Identical behaviour, readable, type-checked by the compiler.

## Core concepts

### 1. Match spec structure

```
[ {MatchHead, [Guard1, Guard2, ...], [Result1, ...]} ]
```

- **MatchHead**: a tuple with literals and `'$N'` variables (bound positionally).
- **Guards**: a list of BIF-like tuples (`{:<, :"$3", some_val}`) that filter matches.
- **Result**: what to return per match. `:"$_"` = the whole object, `:"$$"` = all variables as a list, or a constructed term.

### 2. `:ets.select/2` vs `:ets.select/3`

`select/2` returns all matches at once. `select/3` with a limit and a continuation enables streaming — essential for tables too big to fit in a single result.

### 3. `:ets.select_delete/2` and `:ets.select_count/2`

These run the match spec inside ETS without returning the rows at all — only a count (or the number of deletions). Zero copy. Use them whenever you do not need the rows.

### 4. `fun2ms` limitations

- Must be called at compile time (`require Ex2ms` or `:ets.fun2ms/1` with `use`; at the very least it must appear literally in source).
- Fun body is restricted: only a subset of guards, no user function calls.
- Variable capture from outer scope works only for bound constants (`^var`).

## Design decisions

- **Option A — `tab2list + Enum.filter`**: readable but O(n) heap pressure. Unacceptable at our scale.
- **Option B — `select/2` with literal match specs**: fastest but unreadable.
- **Option C — `fun2ms`** (chosen): same performance as B, readability of A.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule SessionStore.MixProject do
  use Mix.Project

  def project do
    [app: :session_store, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application do
    [extra_applications: [:logger], mod: {SessionStore.Application, []}]
  end

  defp deps do
    [{:benchee, "~> 1.3", only: :dev}]
  end
end
```

### Dependencies (mix.exs)

```elixir
```elixir
:ets.fun2ms(fn {_id, _user, exp, _meta} when exp < 1_700_000_000 -> true end)
```

Identical behaviour, readable, type-checked by the compiler.

## Core concepts

### 1. Match spec structure

```
[ {MatchHead, [Guard1, Guard2, ...], [Result1, ...]} ]
```

- **MatchHead**: a tuple with literals and `'$N'` variables (bound positionally).
- **Guards**: a list of BIF-like tuples (`{:<, :"$3", some_val}`) that filter matches.
- **Result**: what to return per match. `:"$_"` = the whole object, `:"$$"` = all variables as a list, or a constructed term.

### 2. `:ets.select/2` vs `:ets.select/3`

`select/2` returns all matches at once. `select/3` with a limit and a continuation enables streaming — essential for tables too big to fit in a single result.

### 3. `:ets.select_delete/2` and `:ets.select_count/2`

These run the match spec inside ETS without returning the rows at all — only a count (or the number of deletions). Zero copy. Use them whenever you do not need the rows.

### 4. `fun2ms` limitations

- Must be called at compile time (`require Ex2ms` or `:ets.fun2ms/1` with `use`; at the very least it must appear literally in source).
- Fun body is restricted: only a subset of guards, no user function calls.
- Variable capture from outer scope works only for bound constants (`^var`).

## Design decisions

- **Option A — `tab2list + Enum.filter`**: readable but O(n) heap pressure. Unacceptable at our scale.
- **Option B — `select/2` with literal match specs**: fastest but unreadable.
- **Option C — `fun2ms`** (chosen): same performance as B, readability of A.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule SessionStore.MixProject do
  use Mix.Project

  def project do
    [app: :session_store, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application do
    [extra_applications: [:logger], mod: {SessionStore.Application, []}]
  end

  defp deps do
    [{:benchee, "~> 1.3", only: :dev}]
  end
end
```

### Step 1: The store

**Objective**: Own the session table and expose `select_delete/2` so expired rows vanish inside the ETS driver, never copied to heap.

```elixir
# lib/session_store/store.ex
defmodule SessionStore.Store do
  @moduledoc """
  Session store owner. Sessions are `{session_id, user_id, expires_at_ms, metadata}`.
  """
  use GenServer
  require Logger

  @table :sessions

  def start_link(_opts), do: GenServer.start_link(__MODULE__, nil, name: __MODULE__)

  @spec put(String.t(), String.t(), non_neg_integer(), map()) :: :ok
  def put(session_id, user_id, expires_at_ms, metadata) do
    :ets.insert(@table, {session_id, user_id, expires_at_ms, metadata})
    :ok
  end

  @spec get(String.t()) :: {:ok, map()} | :not_found
  def get(session_id) do
    case :ets.lookup(@table, session_id) do
      [{^session_id, user_id, exp, meta}] ->
        {:ok, %{session_id: session_id, user_id: user_id, expires_at: exp, metadata: meta}}

      [] ->
        :not_found
    end
  end

  @doc "Deletes expired sessions inside ETS with zero heap copies. Returns count deleted."
  @spec cleanup_expired(non_neg_integer()) :: non_neg_integer()
  def cleanup_expired(now_ms) do
    ms =
      :ets.fun2ms(fn {_id, _user, exp, _meta} when exp < :"$now" -> true end)
      |> bind_now(now_ms)

    :ets.select_delete(@table, ms)
  end

  # Helper: substitute the placeholder symbol with the actual cutoff at runtime.
  # fun2ms cannot capture outer vars in its guard, so we build the spec in a
  # compile-time-safe shape and patch the literal afterwards.
  defp bind_now([{head, [{:<, var, :"$now"}], result}], now_ms) do
    [{head, [{:<, var, now_ms}], result}]
  end

  @impl true
  def init(_) do
    :ets.new(@table, [:named_table, :set, :public, {:read_concurrency, true}])
    {:ok, %{}}
  end
end
```

### Step 2: Analytics — count-without-copy and range queries

**Objective**: Run `select_count/2` for per-user totals and stream expirations via `select/3` continuations so heap stays flat at any scale.

```elixir
# lib/session_store/analytics.ex
defmodule SessionStore.Analytics do
  @table :sessions

  @doc "Count sessions for a given user — zero heap copies."
  @spec session_count(String.t()) :: non_neg_integer()
  def session_count(user_id) do
    ms = [{{:_, user_id, :_, :_}, [], [true]}]
    :ets.select_count(@table, ms)
  end

  @doc """
  Stream session ids for all sessions expiring before a cutoff.
  Uses continuation-based select to bound memory regardless of match size.
  """
  @spec expiring_before(non_neg_integer(), pos_integer()) :: Enumerable.t()
  def expiring_before(cutoff_ms, batch_size \\ 1_000) do
    ms = [
      {{:"$1", :_, :"$2", :_}, [{:<, :"$2", cutoff_ms}], [:"$1"]}
    ]

    Stream.resource(
      fn -> :ets.select(@table, ms, batch_size) end,
      fn
        :"$end_of_table" -> {:halt, nil}
        {rows, cont} -> {rows, :ets.select(cont)}
      end,
      fn _ -> :ok end
    )
  end
end
```

### Step 3: Application

**Objective**: Supervise the Store so a crash recreates `:sessions` before analytics callers observe `:badarg`.

```elixir
# lib/session_store/application.ex
defmodule SessionStore.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [SessionStore.Store]
    Supervisor.start_link(children, strategy: :one_for_one, name: SessionStore.Supervisor)
  end
end
```

## Data flow diagram

```
  Process heap                          ETS table (off-heap)
  ┌──────────────────┐                  ┌────────────────────────┐
  │                  │                  │ {s1, u1, 1700..., m1}  │
  │   fun2ms spec    │──select_delete──▶│ {s2, u2, 1600..., m2}  │ match runs
  │                  │                  │ {s3, u1, 1900..., m3}  │ in C, no copy
  │                  │◀── count ────────│                        │
  └──────────────────┘                  └────────────────────────┘

  tab2list + Enum.filter (the wrong way):
  ┌──────────────────┐                  ┌────────────────────────┐
  │                  │◀── copy all ─────│ 500k rows              │
  │   heap usage     │  100 MB+         │                        │
  │   fills up       │                  └────────────────────────┘
  │   GC triggered   │
  └──────────────────┘
```

## Why this works

ETS match specs execute inside the ETS driver (BIF, written in C). The match spec is compiled once into a small bytecode and applied row by row. Non-matching rows never touch the calling process. For `select_delete/2`, matches are deleted in place; only an integer count is returned. This makes cleanup and count queries O(n) in table size but O(1) in heap pressure — which is what the BEAM scheduler cares about.

## Tests

```elixir
# test/session_store/store_test.exs
defmodule SessionStore.StoreTest do
  use ExUnit.Case, async: false

  alias SessionStore.{Store, Analytics}

  setup do
    :ets.delete_all_objects(:sessions)
    :ok
  end

  describe "put/4 and get/1" do
    test "retrieves a stored session" do
      Store.put("s1", "u1", 1_700_000_000, %{ua: "curl"})
      assert {:ok, %{session_id: "s1", user_id: "u1"}} = Store.get("s1")
    end

    test "returns :not_found for missing session" do
      assert Store.get("nope") == :not_found
    end
  end

  describe "cleanup_expired/1 — match spec delete" do
    test "deletes only expired entries" do
      Store.put("s_old", "u1", 100, %{})
      Store.put("s_new", "u1", 999_999_999, %{})

      deleted = Store.cleanup_expired(1_000)
      assert deleted == 1
      assert Store.get("s_old") == :not_found
      assert {:ok, _} = Store.get("s_new")
    end

    test "returns 0 when nothing is expired" do
      Store.put("s1", "u1", 999_999_999, %{})
      assert Store.cleanup_expired(100) == 0
    end
  end

  describe "Analytics.session_count/1" do
    test "counts sessions per user" do
      for i <- 1..5, do: Store.put("a_#{i}", "user_a", 999_999_999, %{})
      for i <- 1..2, do: Store.put("b_#{i}", "user_b", 999_999_999, %{})

      assert Analytics.session_count("user_a") == 5
      assert Analytics.session_count("user_b") == 2
      assert Analytics.session_count("user_none") == 0
    end
  end

  describe "Analytics.expiring_before/2" do
    test "streams session ids in batches" do
      for i <- 1..50 do
        Store.put("s#{i}", "u", if(rem(i, 2) == 0, do: 100, else: 999_999_999), %{})
      end

      ids = Analytics.expiring_before(1_000, 10) |> Enum.to_list()
      assert length(ids) == 25
      assert Enum.all?(ids, &String.starts_with?(&1, "s"))
    end
  end
end
```

## Benchmark

```elixir
# bench/matchspec_bench.exs
alias SessionStore.{Store, Analytics}

# Seed 100k sessions, half expired
for i <- 1..100_000 do
  exp = if rem(i, 2) == 0, do: 100, else: 999_999_999
  Store.put("s#{i}", "u#{rem(i, 1000)}", exp, %{})
end

Benchee.run(
  %{
    "select_delete (match spec)" => fn ->
      # Repopulate per run to keep the benchmark meaningful
      Store.cleanup_expired(1_000)
      for i <- 1..100_000, rem(i, 2) == 0, do: Store.put("s#{i}", "u", 100, %{})
    end,
    "tab2list + Enum.filter + delete" => fn ->
      :sessions
      |> :ets.tab2list()
      |> Enum.filter(fn {_, _, exp, _} -> exp < 1_000 end)
      |> Enum.each(fn t -> :ets.delete_object(:sessions, t) end)

      for i <- 1..100_000, rem(i, 2) == 0, do: Store.put("s#{i}", "u", 100, %{})
    end
  },
  time: 5,
  warmup: 2
)
```

Target: `select_delete` 10–50× faster than the `tab2list` variant, with < 1 MB of heap growth per run vs tens of MB.

## Deep Dive

ETS (Erlang Term Storage) is RAM-only and process-linked; table destruction triggers if the owner crashes, causing silent data loss in careless designs. Match specifications (match_specs) are micro-programs that filter/transform data at the C layer, orders of magnitude faster than fetching all records and filtering in Elixir. Mnesia adds disk persistence and replication but introduces transaction overhead and deadlock potential; dirty operations bypass locks for speed but sacrifice consistency guarantees. For caching, named tables (public by design) are globally visible but require careful name management; consider ETS sharding (multiple small tables) to reduce lock contention on hot keys. DETS (Disk ETS) persists to disk but is single-process bottleneck and slower than a real database. At scale, prefer ETS for in-process state and Mnesia/PostgreSQL for shared, persistent data.
## Advanced Considerations

ETS and DETS performance characteristics change dramatically based on access patterns and table types. Ordered sets provide range queries but slower access than hash tables; set types don't support duplicate keys while bags do. The `heir` option for ETS tables is essential for fault tolerance — when a table owner crashes, the heir process can take ownership and prevent data loss. Without it, the table is lost immediately. Mnesia replicates entire tables across nodes; choosing which nodes should have replicas and whether they're RAM or disk replicas affects both consistency guarantees and network traffic during cluster operations.

DETS persistence comes with significant performance implications — writes are synchronous to disk by default, creating latency spikes. Using `sync: false` improves throughput but risks data loss on crashes. The maximum DETS table size is limited by available memory and the file system; planning capacity requires understanding your growth patterns. Mnesia's transaction system provides ACID guarantees, but dirty operations bypass these guarantees for performance. Understanding when to use dirty reads versus transactional reads significantly impacts both correctness and latency.

Debugging ETS and DETS issues is challenging because problems often emerge under load when many processes contend for the same table. Table memory fragmentation is invisible to code but can exhaust memory. Using match specs instead of iteration over large tables can dramatically improve performance but requires careful construction. The interaction between ETS, replication, and distributed systems creates subtle consistency issues — a node with a stale ETS replica can serve incorrect data during network partitions. Always monitor table sizes and replication status with structured logging.


## Deep Dive: Etsdets Patterns and Production Implications

ETS tables are in-memory, non-distributed key-value stores with tunable semantics (ordered_set, duplicate_bag). Under concurrent read/write load, ETS table semantics matter: bag semantics allow fast appends but slow deletes; ordered_set allows range queries but slower inserts. Testing ETS behavior under concurrent load is non-trivial; single-threaded tests miss lock contention. Production ETS tables often fail under load due to concurrency assumptions that quiet tests don't exercise.

---

## Trade-offs and production gotchas

1. **`fun2ms` must be called with a literal fun at source position** — it is a parse transform, not a runtime function. Building match specs dynamically (from user input) requires hand-rolling the spec tuple.
2. **Match spec guards are restricted**: only a whitelisted set of BIFs (`<`, `>`, `==`, `is_integer`, `element`, etc.). You cannot call `String.contains?/2` or `Date.compare/2` inside a match spec. Precompute the value before building the spec.
3. **`select/2` with no limit returns everything at once**: if a match returns 10M rows, you copy 10M rows to your heap. Use `select/3` with a batch size for any query that can match large subsets.
4. **`select_delete` does not traverse in insertion order** on `:set` or `:bag` — your deletion order is undefined. If order matters, use `:ordered_set`.
5. **Compiled match specs are not cached automatically**: `:ets.match_spec_compile/1` returns an opaque term that you can reuse. For hot paths executing the same spec thousands of times per second, cache the compiled form in `:persistent_term`.
6. **When NOT to use match specs**: for single-key lookups (`:ets.lookup/2`) or full-table counts (`:ets.info(table, :size)`). Both are O(1) with no need for a spec.

## Reflection

You need to atomically update a counter field inside a row (read-modify-write). `:ets.select_replace/2` can do this in a single pass without copying. Write the `fun2ms` form for "increment the third element of every row where the first element equals `"alice"`", and explain what makes this operation atomic with respect to concurrent readers using `:read_concurrency`.

## Resources

- [`:ets.fun2ms/1` docs](https://www.erlang.org/doc/man/ets.html#fun2ms-1)
- [`:ets.select/2` docs](https://www.erlang.org/doc/man/ets.html#select-2)
- [Match specifications in Erlang](https://www.erlang.org/doc/apps/erts/match_spec.html)
- [`Ex2ms` — Elixir wrapper](https://hex.pm/packages/ex2ms)
- [ETS under the hood — Erlang in Anger](https://www.erlang-in-anger.com/)
