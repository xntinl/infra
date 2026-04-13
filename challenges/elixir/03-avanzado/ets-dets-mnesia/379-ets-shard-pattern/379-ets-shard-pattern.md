# ETS Shard Pattern (N Tables, Hash-Based) for Concurrent Writes

**Project**: `write_heavy_counter` — a counter store that sustains millions of concurrent increments across BEAM schedulers without contention on a single ETS table.

## Project context

You collect request counters per endpoint across a high-traffic API. Every request bumps a counter. With `:write_concurrency: true`, a single ETS `:set` table scales well — but not infinitely. Under a sustained write load of ~1M increments/s, contention on the internal lock striping becomes the bottleneck: schedulers spin waiting for the lock associated with a bucket.

The shard pattern splits a logical table into N physical ETS tables. A key's shard is `:erlang.phash2(key, N)`. Writes to different shards never contend because they are different tables with different lock stripes. Reads that touch a specific key go to one shard (O(1) fan-in); reads that aggregate across all keys fan out to all N shards. This is the same idea as Java's `ConcurrentHashMap` or Go's `sync.Map`-by-striping.

```
write_heavy_counter/
├── lib/
│   └── write_heavy_counter/
│       ├── application.ex
│       ├── sharder.ex
│       └── counter.ex
├── test/
│   └── write_heavy_counter/
│       └── counter_test.exs
├── bench/
│   └── shard_bench.exs
└── mix.exs
```

## Why shard and not a single table with `:write_concurrency`

OTP 22+ `:write_concurrency: true` uses lock striping — the table is internally split into M lock buckets (defaults to 16–128 depending on scheduler count). This helps, but all buckets still live in a single table with a single owner process, a single GC pressure, and a single resize boundary. Under extreme write rates:

- all writers share the same internal lock-striping array, which caps at a fixed number of buckets,
- `ets:info/1` and housekeeping operate on a single table and can block writers,
- memory fragmentation is concentrated.

Sharding gives you **N times the bucket count** (N × internal stripes), plus isolated maintenance overhead. With N equal to `:erlang.system_info(:schedulers_online)` you give each scheduler its own hot table.

## Why `:erlang.phash2` and not `:erlang.crc32`

`phash2` is the BEAM's optimized hash for terms, designed for exactly this use case. `crc32` is for binary data and requires converting the key to a binary first (extra allocation). Use `phash2`.

## Core concepts

### 1. Shard count heuristic

A common default: `N = schedulers_online()` — one shard per scheduler. Going higher gives diminishing returns. Powers of two (16, 32, 64) also avoid modulo bias issues if you ever switch hash functions. For pure write-throughput, N = schedulers_online() works well; for mixed read-aggregate workloads you may want fewer shards (fewer lookups during aggregation).

### 2. `:counters` as an alternative

For pure integer counters, OTP ≥ 21.2 provides `:counters.new/2` — a lock-free atomic array. Faster and simpler than ETS for integers only. We build the ETS shard pattern because it generalizes to any value (maps, tuples, binaries), not just integers.

### 3. Aggregation cost

A read like "total requests across all endpoints" must iterate every shard. With N = 64 this is 64 lookups / matches. Still O(N) — acceptable if reads are rare.

### 4. Persistent shard table reference

Store the list of shard tables in `:persistent_term` — a single read gets the whole list with no copy. Avoid recomputing or looking up per operation.

## Design decisions

- **Option A — single ETS with `:write_concurrency`**: simple, fine up to ~500k writes/s.
- **Option B — `:counters` (atomic array)**: fastest for integer counters only; does not fit general key-value.
- **Option C — N-shard ETS tables** (chosen): scales to millions of writes/s, generalizes to any value, slightly more code.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule WriteHeavyCounter.MixProject do
  use Mix.Project

  def project do
    [app: :write_heavy_counter, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application do
    [extra_applications: [:logger], mod: {WriteHeavyCounter.Application, []}]
  end

  defp deps do
    [{:benchee, "~> 1.3", only: :dev}]
  end
end
```

### Dependencies (mix.exs)

```elixir
```elixir
defmodule WriteHeavyCounter.MixProject do
  use Mix.Project

  def project do
    [app: :write_heavy_counter, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application do
    [extra_applications: [:logger], mod: {WriteHeavyCounter.Application, []}]
  end

  defp deps do
    [{:benchee, "~> 1.3", only: :dev}]
  end
end
```

### Step 1: The sharder

**Objective**: Create one `:write_concurrency` ETS table per scheduler and publish the tuple via `:persistent_term` for lock-free routing.

```elixir
# lib/write_heavy_counter/sharder.ex
defmodule WriteHeavyCounter.Sharder do
  @moduledoc """
  Creates and owns N ETS tables. Exposes `table_for/1` to route a key to its shard.
  """
  use GenServer

  @shard_count_key :write_heavy_counter_shards

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec shards() :: tuple()
  def shards, do: :persistent_term.get(@shard_count_key)

  @spec table_for(term()) :: atom()
  def table_for(key) do
    shards = shards()
    idx = :erlang.phash2(key, tuple_size(shards))
    elem(shards, idx)
  end

  @spec all() :: [atom()]
  def all, do: shards() |> Tuple.to_list()

  @impl true
  def init(opts) do
    n = Keyword.get(opts, :shards, System.schedulers_online())

    tables =
      for i <- 0..(n - 1) do
        name = :"counter_shard_#{i}"
        :ets.new(name, [
          :named_table,
          :set,
          :public,
          {:write_concurrency, true},
          {:read_concurrency, true},
          {:decentralized_counters, true}
        ])

        name
      end

    :persistent_term.put(@shard_count_key, List.to_tuple(tables))
    {:ok, %{tables: tables}}
  end
end
```

### Step 2: The counter API

**Objective**: Drive increments via `update_counter/4` with a default tuple so hot keys never bounce through a GenServer.

```elixir
# lib/write_heavy_counter/counter.ex
defmodule WriteHeavyCounter.Counter do
  @moduledoc "Increment, read and aggregate counters backed by N sharded ETS tables."

  alias WriteHeavyCounter.Sharder

  @spec increment(term(), integer()) :: integer()
  def increment(key, delta \\ 1) when is_integer(delta) do
    table = Sharder.table_for(key)
    :ets.update_counter(table, key, delta, {key, 0})
  end

  @spec value(term()) :: integer()
  def value(key) do
    table = Sharder.table_for(key)

    case :ets.lookup(table, key) do
      [{^key, v}] -> v
      [] -> 0
    end
  end

  @spec total() :: integer()
  def total do
    ms = [{{:_, :"$1"}, [], [:"$1"]}]

    Sharder.all()
    |> Enum.reduce(0, fn table, acc ->
      acc + Enum.sum(:ets.select(table, ms))
    end)
  end

  @spec reset() :: :ok
  def reset do
    Enum.each(Sharder.all(), &:ets.delete_all_objects/1)
  end
end
```

### Step 3: Application

**Objective**: Supervise the Sharder so a crash rebuilds all shard tables before any writer observes a stale `:persistent_term` tuple.

```elixir
# lib/write_heavy_counter/application.ex
defmodule WriteHeavyCounter.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [WriteHeavyCounter.Sharder]
    Supervisor.start_link(children, strategy: :one_for_one, name: WriteHeavyCounter.Supervisor)
  end
end
```

## Data flow diagram

```
  Writers running on all schedulers:

    inc("/api/a") ─┐
    inc("/api/b") ─┼── phash2(key, N) ──▶ shard index
    inc("/api/c") ─┘

           │                │               │
           ▼                ▼               ▼
       shard_0          shard_1         shard_N-1
      (own lock        (own lock       (own lock
       stripes)         stripes)        stripes)

    No shared lock → writers on different shards never contend.

  Read path:
    value(key)     ─── one shard lookup       ─── O(1)
    total()        ─── N shard scans summed   ─── O(total rows)
```

## Why this works

Each ETS table has its own internal lock-striping array. By distributing keys across N tables, we multiply the total number of independent lock buckets by N. Because `:erlang.phash2` distributes evenly, writes are uniformly spread. `:decentralized_counters: true` (OTP 23+) further reduces contention on size counters when `ets:info(:size)` is called concurrently. For aggregation, the cost of visiting N tables is measured once per read — acceptable when reads are infrequent.

## Tests

```elixir
# test/write_heavy_counter/counter_test.exs
defmodule WriteHeavyCounter.CounterTest do
  use ExUnit.Case, async: false

  alias WriteHeavyCounter.{Counter, Sharder}

  setup do
    Counter.reset()
    :ok
  end

  describe "increment/2 and value/1" do
    test "increments start at 0 and accumulate" do
      assert 1 = Counter.increment("a")
      assert 2 = Counter.increment("a")
      assert 2 = Counter.value("a")
    end

    test "independent keys do not interfere" do
      Counter.increment("x", 10)
      Counter.increment("y", 5)

      assert Counter.value("x") == 10
      assert Counter.value("y") == 5
    end

    test "unknown keys read as 0" do
      assert Counter.value("ghost") == 0
    end
  end

  describe "total/0" do
    test "sums all counters across shards" do
      for i <- 1..100, do: Counter.increment("k#{i}", i)
      assert Counter.total() == Enum.sum(1..100)
    end

    test "is 0 after reset" do
      Counter.increment("k1")
      Counter.reset()
      assert Counter.total() == 0
    end
  end

  describe "Sharder.table_for/1 — stability" do
    test "same key always routes to the same shard" do
      assert Sharder.table_for("alpha") == Sharder.table_for("alpha")
    end

    test "different keys can route to different shards" do
      shards =
        for i <- 1..100 do
          Sharder.table_for("k#{i}")
        end
        |> Enum.uniq()

      assert length(shards) > 1
    end
  end

  describe "concurrent increments" do
    test "1000 concurrent writers land the correct total" do
      tasks =
        for _ <- 1..1_000 do
          Task.async(fn -> Counter.increment("hot") end)
        end

      Task.await_many(tasks, 10_000)
      assert Counter.value("hot") == 1_000
    end
  end
end
```

## Benchmark

```elixir
# bench/shard_bench.exs
alias WriteHeavyCounter.Counter

# Comparison table: single ETS with write_concurrency vs sharded tables
:ets.new(:single_counter, [
  :named_table, :set, :public,
  {:write_concurrency, true}, {:decentralized_counters, true}
])

Benchee.run(
  %{
    "sharded increment" => fn ->
      Counter.increment("k_#{:rand.uniform(10_000)}")
    end,
    "single-table increment" => fn ->
      key = "k_#{:rand.uniform(10_000)}"
      :ets.update_counter(:single_counter, key, 1, {key, 0})
    end
  },
  time: 5,
  warmup: 2,
  parallel: System.schedulers_online()
)
```

Target (8 schedulers, parallel writers): sharded is typically 2–4× faster than single-table at the high end. Under moderate load both are similar; under contention the shard pattern pulls ahead substantially.

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

1. **Aggregation scans every shard**: if you compute `total/0` every second and have 64 shards with 10k keys each, that is 640k row scans per call. Cache the aggregate and refresh on a timer.
2. **Shard count is fixed at start**: changing N at runtime means re-hashing every key. Not supported in the simple design. Pick a sane default (scheduler count) and live with it.
3. **Hot keys still contend**: sharding does not help if one key absorbs 80% of writes. For true hot keys, use `:counters` (atomic array) or a sampling strategy (increment 1 of every K writes by K).
4. **`decentralized_counters` is OTP 23+**: on older OTP you see unexpected contention on `ets:info(:size)`. Upgrade or avoid calling `size` in hot paths.
5. **Memory overhead**: each ETS table has a fixed overhead (~50 KB). 64 shards × 50 KB = 3 MB per logical table before inserting anything. Negligible for servers, noticeable on embedded devices.
6. **When NOT to shard**: single-digit-thousand writes per second, or workloads already dominated by read aggregation. A single table with `write_concurrency` is simpler and performs as well.

## Reflection

Your benchmark shows the sharded version is 2.5× faster at 16 schedulers but only 1.2× faster at 4 schedulers. Why? Plot the expected cross-over point (schedulers where sharding starts to pay for itself) and relate it to the ETS internal stripe count. How would you verify this empirically on your production hardware before committing to the design?

## Resources

- [`:ets` — `write_concurrency` and lock striping](https://www.erlang.org/doc/man/ets.html)
- [`:counters` module](https://www.erlang.org/doc/man/counters.html)
- [OTP 23 ETS improvements — `decentralized_counters`](https://www.erlang.org/blog/otp-23-highlights/)
- [`:erlang.phash2/2`](https://www.erlang.org/doc/man/erlang.html#phash2-2)
- [Concurrent ETS patterns — Saša Jurić](https://www.erlang-solutions.com/blog/)
