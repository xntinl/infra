# Ordered Set ETS for Efficient Range Queries

**Project**: `time_series_ets` — store ordered time-series samples in ETS and run range queries in logarithmic time.

## Project context

You build a metrics store that keeps the last hour of samples for a few thousand metrics. Clients query "give me all samples of metric X between t1 and t2". The classic hashed ETS table (`:set`, `:bag`) requires a full scan to answer range queries because hash tables do not preserve key order. `:ordered_set` uses an AVL tree internally; range queries are O(log n + k) where k is the number of rows returned.

The catch: ordered_set is slower for point lookups (still tree traversal) and has lower insert throughput than `:set` (tree balancing vs O(1) hash insert). Use it only when you actually need ordered operations: range queries, prefix scans, next/previous navigation.

```
time_series_ets/
├── lib/
│   └── time_series_ets/
│       ├── application.ex
│       ├── store.ex
│       └── range.ex
├── test/
│   └── time_series_ets/
│       └── range_test.exs
├── bench/
│   └── range_bench.exs
└── mix.exs
```

## Why `:ordered_set` and not `:set` with a sorted index

With a `:set`, range queries need either a full scan or a secondary index keyed by timestamp. The secondary index is another ETS table you must keep in sync — more code, same memory cost, two inserts per write. `:ordered_set` gives you ordering for free as a property of the underlying tree.

## Why composite keys and not per-metric tables

Option A: one `:ordered_set` table per metric, keyed by `timestamp`. Pros: cleaner range iteration. Cons: one ETS table per metric (10k metrics → 10k tables → `ets` creation limit, process table pollution, loss of atomic multi-metric queries).

Option B: one table, composite key `{metric, timestamp}`. Pros: single table scales to millions of samples. Cons: need to query `{metric, :_}` carefully — the composite key is compared element-wise, so a bounded range needs explicit bounds.

We pick Option B.

## Core concepts



---

**Why this matters:**
These concepts form the foundation of production Elixir systems. Understanding them deeply allows you to build fault-tolerant, scalable applications that operate correctly under load and failure.

**Real-world use case:**
This pattern appears in systems like:
- Phoenix applications handling thousands of concurrent connections
- Distributed data processing pipelines
- Financial transaction systems requiring consistency and fault tolerance
- Microservices communicating over unreliable networks

**Common pitfall:**
Many developers overlook that Elixir's concurrency model differs fundamentally from threads. Processes are isolated; shared mutable state does not exist. Trying to force shared-memory patterns leads to deadlocks, race conditions, or silently incorrect behavior. Always think in terms of message passing and immutability.
### 1. Composite key ordering

ETS `:ordered_set` compares keys using Erlang's standard term ordering. Tuples are compared element-wise. So `{"cpu", 100} < {"cpu", 200} < {"memory", 50}`. For a range query on metric `"cpu"` between `t1` and `t2`, you iterate from key `{"cpu", t1}` to `{"cpu", t2}`.

### 2. `:ets.next/2` and `:ets.prev/2`

These return the next/previous key in sort order. O(log n) per step. Useful for cursor-style iteration.

### 3. `:ets.select/2` with range guards

The efficient way: a match spec with guards constraining both the metric and timestamp. ETS uses the ordered structure to skip non-matching prefixes.

### 4. Atomicity

`:ordered_set` inserts are atomic per row. Range reads return a point-in-time consistent snapshot **only** if you read with `:ets.select/2` as a single call. A loop using `next/2` can observe partial writes from concurrent inserts.

## Design decisions

- **Option A — one table per metric (per-metric ordered_set)**: simple iteration, poor scaling.
- **Option B — one table, composite key `{metric, ts}`** (chosen): scales to millions of metrics, single atomic range via `select/2`.
- **Option C — sorted list in GenServer state**: great for tiny datasets, terrible for concurrent reads.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule TimeSeriesEts.MixProject do
  use Mix.Project

  def project do
    [app: :time_series_ets, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application do
    [extra_applications: [:logger], mod: {TimeSeriesEts.Application, []}]
  end

  defp deps do
    [{:benchee, "~> 1.3", only: :dev}]
  end
end
```

### Dependencies (mix.exs)

```elixir
```elixir
defmodule TimeSeriesEts.MixProject do
  use Mix.Project

  def project do
    [app: :time_series_ets, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application do
    [extra_applications: [:logger], mod: {TimeSeriesEts.Application, []}]
  end

  defp deps do
    [{:benchee, "~> 1.3", only: :dev}]
  end
end
```

### Step 1: Store owner

**Objective**: Own an `:ordered_set` ETS table keyed by `{metric, ts_ms}` so range scans stay O(log n + k).

```elixir
# lib/time_series_ets/store.ex
defmodule TimeSeriesEts.Store do
  @moduledoc """
  Time-series store with composite key `{metric, timestamp_ms}`.
  The table is `:ordered_set` so range queries are O(log n + k).
  """
  use GenServer

  @table :time_series

  def start_link(_), do: GenServer.start_link(__MODULE__, nil, name: __MODULE__)

  @spec record(String.t(), non_neg_integer(), number()) :: :ok
  def record(metric, timestamp_ms, value) do
    :ets.insert(@table, {{metric, timestamp_ms}, value})
    :ok
  end

  @impl true
  def init(_) do
    :ets.new(@table, [
      :named_table,
      :ordered_set,
      :public,
      {:read_concurrency, true},
      {:write_concurrency, true}
    ])

    {:ok, %{}}
  end
end
```

### Step 2: Range query engine

**Objective**: Drive bounded range scans via match specs + `select/3` continuations so result sets never materialize fully in memory.

```elixir
# lib/time_series_ets/range.ex
defmodule TimeSeriesEts.Range do
  @table :time_series

  @doc """
  Returns `[{timestamp_ms, value}, ...]` for `metric` within `[from_ms, to_ms]` inclusive.
  Uses an ordered match spec: the ETS driver walks the AVL tree only within the range.
  """
  @spec query(String.t(), non_neg_integer(), non_neg_integer()) ::
          [{non_neg_integer(), number()}]
  def query(metric, from_ms, to_ms) when from_ms <= to_ms do
    ms = [
      {
        {{metric, :"$1"}, :"$2"},
        [{:>=, :"$1", from_ms}, {:"=<", :"$1", to_ms}],
        [{{:"$1", :"$2"}}]
      }
    ]

    :ets.select(@table, ms)
  end

  @doc """
  Lazy streaming variant. Each batch of up to `batch` rows avoids loading the whole
  result into memory.
  """
  @spec stream(String.t(), non_neg_integer(), non_neg_integer(), pos_integer()) ::
          Enumerable.t()
  def stream(metric, from_ms, to_ms, batch \\ 1_000) do
    ms = [
      {
        {{metric, :"$1"}, :"$2"},
        [{:>=, :"$1", from_ms}, {:"=<", :"$1", to_ms}],
        [{{:"$1", :"$2"}}]
      }
    ]

    Stream.resource(
      fn -> :ets.select(@table, ms, batch) end,
      fn
        :"$end_of_table" -> {:halt, nil}
        {rows, cont} -> {rows, :ets.select(cont)}
      end,
      fn _ -> :ok end
    )
  end

  @doc "Returns the most recent sample for a metric, or nil."
  @spec latest(String.t()) :: {non_neg_integer(), number()} | nil
  def latest(metric) do
    case :ets.prev(@table, {metric, :infinity}) do
      {^metric, ts} ->
        [{{^metric, ^ts}, v}] = :ets.lookup(@table, {metric, ts})
        {ts, v}

      _ ->
        nil
    end
  end
end
```

### Step 3: Application

**Objective**: Boot the Store under a `:one_for_one` supervisor so a crashed owner rebuilds the table before callers retry.

```elixir
# lib/time_series_ets/application.ex
defmodule TimeSeriesEts.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [TimeSeriesEts.Store]
    Supervisor.start_link(children, strategy: :one_for_one, name: TimeSeriesEts.Supervisor)
  end
end
```

## Data flow diagram

```
  ordered_set (AVL tree) — keys compared element-wise:

       {"cpu", 1000}
      /             \
  {"cpu", 500}    {"cpu", 1500}
                        \
                    {"mem", 100}

  Range query: "cpu" between 500 and 1500
    ETS driver descends to leftmost key ≥ {"cpu", 500}
    walks in-order until key > {"cpu", 1500}
    three rows returned, no full-table scan
```

## Why this works

`:ordered_set` is an AVL tree indexed by the standard Erlang term order. Tuple keys compare element-wise, so all samples of a given metric form a contiguous segment of the tree ordered by timestamp. The match spec compiler recognizes a guard of the form `{:>=, :"$1", Val}` combined with a prefix-literal in the match head as an "ordered range" and iterates only within that subtree — the complexity is O(log n + k) instead of O(n).

## Tests

```elixir
# test/time_series_ets/range_test.exs
defmodule TimeSeriesEts.RangeTest do
  use ExUnit.Case, async: false

  alias TimeSeriesEts.{Store, Range}

  setup do
    :ets.delete_all_objects(:time_series)
    :ok
  end

  describe "query/3 — range semantics" do
    test "returns inclusive range for a metric" do
      for ts <- [100, 200, 300, 400, 500], do: Store.record("cpu", ts, ts * 1.0)
      for ts <- [150, 250], do: Store.record("mem", ts, 9.9)

      assert [{200, 200.0}, {300, 300.0}, {400, 400.0}] = Range.query("cpu", 200, 400)
    end

    test "empty result for out-of-range query" do
      Store.record("cpu", 100, 1.0)
      assert Range.query("cpu", 200, 300) == []
    end

    test "does not return samples from other metrics" do
      Store.record("cpu", 100, 1.0)
      Store.record("mem", 100, 2.0)

      assert [{100, 1.0}] = Range.query("cpu", 0, 1_000)
    end
  end

  describe "stream/4 — lazy iteration" do
    test "streams all rows in batches" do
      for ts <- 1..100, do: Store.record("net", ts, ts)

      rows = Range.stream("net", 1, 100, 10) |> Enum.to_list()
      assert length(rows) == 100
      assert List.first(rows) == {1, 1}
      assert List.last(rows) == {100, 100}
    end
  end

  describe "latest/1" do
    test "returns the most recent sample" do
      Store.record("cpu", 100, 1.0)
      Store.record("cpu", 300, 3.0)
      Store.record("cpu", 200, 2.0)

      assert {300, 3.0} = Range.latest("cpu")
    end

    test "returns nil for unknown metric" do
      assert Range.latest("unknown") == nil
    end
  end
end
```

## Benchmark

```elixir
# bench/range_bench.exs
alias TimeSeriesEts.{Store, Range}

# Seed 100k samples across 100 metrics
for m <- 1..100, ts <- 1..1_000 do
  Store.record("metric_#{m}", ts, :rand.uniform())
end

Benchee.run(
  %{
    "query 100-row range (ordered_set)" => fn ->
      Range.query("metric_42", 500, 600)
    end,
    "latest sample" => fn ->
      Range.latest("metric_42")
    end
  },
  time: 5,
  warmup: 2,
  parallel: 4
)
```

Target: `query 100-row range` under 50 µs. `latest` under 5 µs. A `:set` equivalent with the same data would need a full-table scan — expect 10–100× slowdown at this size.

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

1. **Writes are slower on ordered_set**: inserts rebalance the tree. Under sustained ≥200k writes/s per table, `:set` outperforms `:ordered_set`. If your workload is write-heavy and range queries are rare, keep `:set` and build the range scan differently (e.g. use a secondary sorted list updated periodically).
2. **`write_concurrency` on ordered_set is limited**: prior to OTP 22 it had no effect on `:ordered_set`; since 22 the `decentralized_counters` and coarse-grained locking help but are still below `:set` throughput.
3. **Composite key design matters**: `{metric, ts}` sorts by metric first, then ts. If you frequently query "all metrics at a specific ts" you need the reverse `{ts, metric}` — pick the shape that matches your dominant query.
4. **`:infinity` as a sentinel works** because the term order ranks atoms above integers and `:infinity` is the largest atom in practice for timestamps. It is a convention, not a guarantee; document it.
5. **Large value payloads slow down range queries**: the match spec copies values across the heap boundary. If values are big (e.g., maps of metadata), store a pointer and resolve on demand.
6. **When NOT to use `:ordered_set`**: for pure get-by-key / insert-by-key workloads. The performance gap vs `:set` is measurable and pointless if you never do range operations.

## Reflection

Your system ingests 500k samples/s across 1000 metrics. Range queries are < 100/s. Your benchmark shows `:ordered_set` inserts at 250k/s single-threaded. Where do you shard, and how do you preserve cross-shard range query atomicity? Is there a point where a columnar store (e.g. DuckDB, ClickHouse) beats this architecture, and what is the observable signal that tells you it is time to migrate?

## Executable Example

```elixir
defmodule TimeSeriesEts.MixProject do
  use Mix.Project

  def project do
    [app: :time_series_ets, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application do
    [extra_applications: [:logger], mod: {TimeSeriesEts.Application, []}]
  end

  defp deps do
    [{:benchee, "~> 1.3", only: :dev}]
  end
end



### Step 1: Store owner

**Objective**: Own an `:ordered_set` ETS table keyed by `{metric, ts_ms}` so range scans stay O(log n + k).



### Step 2: Range query engine

**Objective**: Drive bounded range scans via match specs + `select/3` continuations so result sets never materialize fully in memory.



### Step 3: Application

**Objective**: Boot the Store under a `:one_for_one` supervisor so a crashed owner rebuilds the table before callers retry.

defmodule Main do
  def main do
      # Demonstrating 375-ordered-set-range-queries
      :ok
  end
end

Main.main()
```
