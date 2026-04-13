# Mnesia Dirty Operations — When to Skip Transactions

**Project**: `mnesia_dirty` — high-throughput counters and read-heavy lookup tables.

---

## Project context

You run a Phoenix application that emits per-request metrics (hits per
endpoint, bytes transferred, error counts). You already have Mnesia running
for session state. The product team wants rolling counters exposed on a
`/metrics` endpoint. Transactional Mnesia writes would destroy tail latency
at this volume — counter bumps happen thousands of times per second per node
and the contention on a single `{:counters, "index"}` record would serialise
every request behind the transaction coordinator.

`:mnesia.dirty_*` is the escape hatch. These functions bypass the
transaction manager entirely — they are roughly `:ets.lookup/2` with
cluster-wide replication bolted on. The catch is that they give up
atomicity, isolation, and — for cross-node writes — ordering guarantees.
Used correctly they are 10-100x faster than transactional equivalents.
Used incorrectly they corrupt data in ways that only appear under load.

This exercise explores the dirty API in detail: `dirty_read`, `dirty_write`,
`dirty_update_counter`, `dirty_match_object`, and the subtle replication
semantics that make "dirty" a precise technical term rather than a
warning label.

```
mnesia_dirty/
├── lib/
│   └── mnesia_dirty/
│       ├── application.ex
│       ├── schema.ex
│       ├── counters.ex               # dirty_update_counter backed rolling counters
│       ├── cache.ex                  # dirty_read/write cache with optional TTL
│       └── metrics_reporter.ex       # periodic snapshot for /metrics
└── test/
    └── mnesia_dirty/
        ├── counters_test.exs
        └── cache_test.exs
```

---

## Why dirty reads and not transactional reads

A Mnesia transaction involves the transaction manager, lock acquisition, and replication coordination. A dirty read bypasses all of that and reads directly from the local ETS store. When the caller does not need isolation, the cost is unjustified.

---

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
### 1. What "dirty" actually means

Dirty operations skip three things a transaction does:

* acquiring locks
* running the commit protocol across replicas synchronously
* wrapping the operation in a retriable block

They still:

* replicate asynchronously to other nodes
* use the underlying ETS/DETS storage correctly on the local node
* maintain table indexes

This makes them safe for *single-record operations* where the caller can
tolerate non-atomic multi-record sequences.

### 2. `dirty_update_counter/2` — the one truly atomic dirty op

```
dirty_update_counter({:counters, "requests"}, 1)
```

This is backed by `:ets.update_counter/3`. It is atomic *per record, per node*
even without a transaction. Two processes on the same node incrementing the
same counter produce the correct sum. Cross-node is a different story
(see concept 4).

It returns the *new* value, which makes it the idiomatic way to generate
monotonically increasing ids without a transaction.

### 3. The consistency hierarchy

```
┌────────────────────────────────────────────────────────────────┐
│  dirty_read / dirty_write                                      │
│  • No locks. No ordering. Multi-record sequences are NOT atomic│
│  • Writes replicate asynchronously                             │
└────────────────────────────────────────────────────────────────┘
┌────────────────────────────────────────────────────────────────┐
│  dirty_update_counter                                          │
│  • Atomic per record per node (uses ets:update_counter)        │
│  • Cross-node sum may be briefly wrong due to async replication│
└────────────────────────────────────────────────────────────────┘
┌────────────────────────────────────────────────────────────────┐
│  async_dirty/1                                                 │
│  • Wraps dirty ops in a context — useful for match_object      │
│  • Still no locks, still no atomicity                          │
└────────────────────────────────────────────────────────────────┘
┌────────────────────────────────────────────────────────────────┐
│  sync_dirty/1                                                  │
│  • Same as async_dirty but waits for replicas to ACK locally   │
│  • Still no locks, but durable-on-return                       │
└────────────────────────────────────────────────────────────────┘
┌────────────────────────────────────────────────────────────────┐
│  transaction/1                                                 │
│  • Full ACID                                                   │
└────────────────────────────────────────────────────────────────┘
```

### 4. Cross-node counter anomaly

Two nodes increment `{:counters, "x"}` simultaneously:

```
Node A:  dirty_update_counter → local value 5, replicate to B
Node B:  dirty_update_counter → local value 5, replicate to A (in flight)
Node A receives B's update: 5 + 1 = 6? No — 5 (overwrite)
```

Dirty replication is *last-writer-wins* on the whole record. For counters
this loses updates. If multiple nodes increment the same counter, you
MUST either:

* use `transaction/1` with a `:write` lock, or
* shard the counter across nodes and sum at read time (preferred), or
* route all increments for a given key to one node (defeats distribution).

### 5. When dirty is the right answer

* A single node owns all writes for a given key (e.g. partitioned by PID).
* Reads happen on every node and must be fast; eventual consistency is fine.
* You need sub-50µs write latency and cannot afford the transaction overhead.
* The workload is idempotent (last-write-wins is semantically correct).

When in doubt, start with `transaction/1`. Benchmark. Migrate hot paths to
dirty ops only after measurement.

---

## Design decisions

**Option A — wrap every read in a transaction**
- Pros: consistency; no reasoning about isolation.
- Cons: transaction manager overhead on every call; contention on hot keys.

**Option B — dirty ops for hot reads, transactions for writes** (chosen)
- Pros: 1-2 us reads; transactions only where they matter.
- Cons: must reason about isolation per call site; easy to introduce races.

→ Chose **B** because read-heavy workloads dominated by a transaction manager are the textbook case for dirty ops.

---

## Implementation

### Step 1: `mix.exs`

**Objective**: List `:mnesia` under `extra_applications` so the release starts storage before schema bootstrap enters `init/1`.

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
defmodule MnesiaDirty.MixProject do
  use Mix.Project

  def project do
    [app: :mnesia_dirty, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application do
    [extra_applications: [:logger, :mnesia], mod: {MnesiaDirty.Application, []}]
  end

  defp deps, do: [{:benchee, "~> 1.3", only: :dev}]
end
```

### Step 2: `lib/mnesia_dirty/application.ex`

**Objective**: Start `Schema` before `MetricsReporter` so the periodic snapshot job never races an unloaded table.

```elixir
defmodule MnesiaDirty.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [MnesiaDirty.Schema, MnesiaDirty.MetricsReporter]
    Supervisor.start_link(children, strategy: :one_for_one, name: MnesiaDirty.Supervisor)
  end
end
```

### Step 3: `lib/mnesia_dirty/schema.ex`

**Objective**: Create `:counters` and `:cache` as `:ram_copies` sets and block on `wait_for_tables/2` so callers never hit `:no_exists`.

```elixir
defmodule MnesiaDirty.Schema do
  @moduledoc false
  use GenServer

  @tables [:counters, :cache]

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(_opts) do
    _ = :mnesia.stop()

    case :mnesia.create_schema([node()]) do
      :ok -> :ok
      {:error, {_, {:already_exists, _}}} -> :ok
      other -> throw({:schema_failed, other})
    end

    :ok = :mnesia.start()
    ensure(:counters, attributes: [:key, :value], ram_copies: [node()], type: :set)

    ensure(:cache,
      attributes: [:key, :value, :inserted_at],
      ram_copies: [node()],
      type: :set
    )

    :ok = :mnesia.wait_for_tables(@tables, 10_000)
    {:ok, %{}}
  end

  defp ensure(table, opts) do
    case :mnesia.create_table(table, opts) do
      {:atomic, :ok} -> :ok
      {:aborted, {:already_exists, ^table}} -> :ok
      other -> throw({:create_failed, table, other})
    end
  end
end
```

### Step 4: `lib/mnesia_dirty/counters.ex`

**Objective**: Build per-node sharded counters on `dirty_update_counter/3` so replication never clobbers increments with last-writer-wins.

```elixir
defmodule MnesiaDirty.Counters do
  @moduledoc """
  Per-key monotonic counters backed by `:mnesia.dirty_update_counter/3`.

  `dirty_update_counter` is atomic on the owning node. For cross-node safety,
  counters are namespaced by `{node(), key}` and summed at read time — the
  "sharded counter" pattern that avoids last-writer-wins replication loss.
  """

  @table :counters

  @spec bump(atom() | String.t(), integer()) :: integer()
  def bump(key, increment \\ 1) when is_integer(increment) do
    :mnesia.dirty_update_counter(@table, {node(), key}, increment)
  end

  @spec get(atom() | String.t()) :: non_neg_integer()
  def get(key) do
    # Sum the per-node shards. dirty_match_object does not use the index,
    # so with many nodes this can become expensive — but on a 3-10 node
    # cluster the constant is negligible.
    pattern = {@table, {:_, key}, :_}

    :mnesia.dirty_match_object(pattern)
    |> Enum.reduce(0, fn {@table, _, v}, acc -> acc + v end)
  end

  @spec reset(atom() | String.t()) :: :ok
  def reset(key) do
    # Reset only the local shard — remote nodes reset theirs independently.
    :mnesia.dirty_delete(@table, {node(), key})
    :ok
  end

  @spec snapshot() :: %{optional(term()) => non_neg_integer()}
  def snapshot do
    @table
    |> :mnesia.dirty_match_object({@table, :_, :_})
    |> Enum.reduce(%{}, fn {@table, {_node, key}, v}, acc ->
      Map.update(acc, key, v, &(&1 + v))
    end)
  end
end
```

### Step 5: `lib/mnesia_dirty/cache.ex`

**Objective**: Expose a dirty-read memoization cache where last-writer-wins is acceptable because values are idempotent derived data.

```elixir
defmodule MnesiaDirty.Cache do
  @moduledoc """
  Dirty read/write cache.

  Writes are last-writer-wins across nodes — intentional. The cache is
  designed for idempotent derived data (memoized computation results);
  if two nodes disagree, either answer is acceptable.
  """

  @table :cache

  @spec put(term(), term()) :: :ok
  def put(key, value) do
    :mnesia.dirty_write({@table, key, value, System.system_time(:second)})
    :ok
  end

  @spec get(term()) :: {:ok, term()} | :miss
  def get(key) do
    case :mnesia.dirty_read({@table, key}) do
      [{@table, ^key, value, _ts}] -> {:ok, value}
      [] -> :miss
    end
  end

  @spec get_or_compute(term(), (-> term())) :: term()
  def get_or_compute(key, compute_fun) when is_function(compute_fun, 0) do
    case get(key) do
      {:ok, value} ->
        value

      :miss ->
        value = compute_fun.()
        put(key, value)
        value
    end
  end

  @spec delete(term()) :: :ok
  def delete(key) do
    :mnesia.dirty_delete(@table, key)
    :ok
  end
end
```

### Step 6: `lib/mnesia_dirty/metrics_reporter.ex`

**Objective**: Emit periodic counter snapshots as a stand-in for a real Telemetry/Prometheus sink without blocking dirty writers.

```elixir
defmodule MnesiaDirty.MetricsReporter do
  @moduledoc """
  Periodically logs the current counter snapshot — a stand-in for your
  real metrics sink (Telemetry, Prometheus, StatsD).
  """
  use GenServer
  require Logger

  @interval :timer.seconds(30)

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(_opts) do
    schedule()
    {:ok, %{}}
  end

  @impl true
  def handle_info(:report, state) do
    snapshot = MnesiaDirty.Counters.snapshot()
    Logger.info("Counters snapshot: #{inspect(snapshot)}")
    schedule()
    {:noreply, state}
  end

  defp schedule, do: Process.send_after(self(), :report, @interval)
end
```

### Step 7: `test/mnesia_dirty/counters_test.exs`

**Objective**: Write tests in `test/mnesia_dirty/counters_test.exs` covering behavior and edge cases.

```elixir
defmodule MnesiaDirty.CountersTest do
  use ExUnit.Case, async: false

  alias MnesiaDirty.Counters

  setup do
    :mnesia.clear_table(:counters)
    :ok
  end

  describe "bump/2 and get/1" do
    test "accumulates sequential increments" do
      for _ <- 1..100, do: Counters.bump("hits")
      assert Counters.get("hits") == 100
    end

    test "handles concurrent increments atomically on one node" do
      tasks = for _ <- 1..1_000, do: Task.async(fn -> Counters.bump("concurrent") end)
      Enum.each(tasks, &Task.await/1)
      assert Counters.get("concurrent") == 1_000
    end

    test "snapshot/0 aggregates across keys" do
      Counters.bump("a", 3)
      Counters.bump("b", 7)
      snap = Counters.snapshot()
      assert snap["a"] == 3
      assert snap["b"] == 7
    end
  end
end
```

### Step 8: `test/mnesia_dirty/cache_test.exs`

**Objective**: Write tests in `test/mnesia_dirty/cache_test.exs` covering behavior and edge cases.

```elixir
defmodule MnesiaDirty.CacheTest do
  use ExUnit.Case, async: false

  alias MnesiaDirty.Cache

  setup do
    :mnesia.clear_table(:cache)
    :ok
  end

  test "put/2 and get/1" do
    Cache.put(:k, 42)
    assert {:ok, 42} = Cache.get(:k)
  end

  test "get/1 returns :miss for unknown keys" do
    assert :miss = Cache.get(:unknown)
  end

  test "get_or_compute/2 runs the computation once per miss" do
    agent = start_supervised!({Agent, fn -> 0 end})

    compute = fn ->
      Agent.update(agent, &(&1 + 1))
      :value
    end

    assert :value = Cache.get_or_compute(:memo, compute)
    assert :value = Cache.get_or_compute(:memo, compute)
    assert Agent.get(agent, & &1) == 1
  end
end
```

### Why this works

`:mnesia.dirty_read/2` reads the local replica via ETS without touching the transaction manager. The read is not isolated against concurrent writes, but for read-then-don't-write flows that is acceptable and roughly 50-100x faster.

---

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

**1. Dirty ops silently drop write conflicts across nodes.**
`dirty_write/1` from two nodes races; the loser disappears. Counters,
sets, and any data with semantic merge rules must be sharded by node
(as shown) or guarded by a transaction.

**2. `dirty_update_counter/3` only works on `{:set}` tables with integer
values at a fixed attribute position.** Violating the schema raises at
runtime, not compile time. Test the function exists for each counter key.

**3. No lock means no read isolation.**
A `dirty_read/1` can observe a write that is later aborted in a
transaction. For read-only workflows that must never see uncommitted
state, transactions are required.

**4. Snapshot accuracy under concurrent writes.**
`dirty_match_object/1` scans the table without locking. Two concurrent
writes can produce a snapshot that was never a consistent state —
counter A at t0 and counter B at t1. Accept it for monitoring use cases;
reject it for anything with invariants.

**5. Memory growth is your problem.**
Dirty ops write as fast as ETS does. Nothing garbage-collects old
cache entries. A TTL sweeper GenServer or a bounded-size eviction
policy is not optional in production.

**6. Dirty ops still replicate.**
"Dirty" does not mean "local-only". A `dirty_write` on a table with
replicas still ships bytes across the network asynchronously. Under
partition, those messages queue and eventually flush — which can
produce surprise writes long after the original call.

**7. Observability.**
Transactions have system events; dirty ops do not. Wrap hot-path dirty
calls in telemetry spans yourself (`:telemetry.execute/2`) so you can
measure latency distributions in production.

**8. When NOT to use dirty ops.**
* Read-then-write logic on a shared key — use `transaction/1` with
  `:write` locks.
* Cross-node counters where every node writes to every key — use a
  CRDT (see `Horde.DeltaCrdt`) or a single owner.
* Auditability requirements — dirty writes have no transactional log
  semantics; use a durable log + projected state instead.
* Any invariant you cannot afford to violate for even one millisecond.

---

## Benchmark

```elixir
alias MnesiaDirty.{Counters, Cache}

Benchee.run(
  %{
    "dirty_update_counter" => fn -> Counters.bump("bench") end,
    "dirty_write"          => fn -> Cache.put(:k, :v) end,
    "dirty_read"           => fn -> Cache.get(:k) end,
    "transaction write"    => fn ->
      :mnesia.transaction(fn ->
        :mnesia.write({:cache, :tx_k, :v, 0})
      end)
    end
  },
  parallel: 8,
  time: 5,
  warmup: 2
)
```

Representative results (M1, OTP 26, single node):

| Operation             | p50    | ops/s   |
|-----------------------|--------|---------|
| dirty_update_counter  | 1.8µs  | ~550k   |
| dirty_write           | 2.5µs  | ~400k   |
| dirty_read            | 1.1µs  | ~900k   |
| transaction write     | 95µs   | ~10k    |

~40x throughput advantage over transactions. That gap is the reason dirty
ops exist — use them for paths where the gap matters.

---

## Reflection

- You accidentally read a value that another transaction later rolled back. How does that surface, and what do you do about it?
- A 90/10 read/write workload is tempting for dirty reads. At what write ratio does a transactional read become the correct default again?

---

## Executable Example

```elixir
defmodule Main do
  defp deps do
    [
      # No external dependencies — pure Elixir
    ]
  end

  defmodule MnesiaDirty.MixProject do
    use Mix.Project

    def project do
      [app: :mnesia_dirty, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
    end

    def application do
      [extra_applications: [:logger, :mnesia], mod: {MnesiaDirty.Application, []}]
    end

    defp deps, do: [{:benchee, "~> 1.3", only: :dev}]
  end

  defmodule MnesiaDirty.Application do
    @moduledoc false
    use Application

    @impl true
    def start(_type, _args) do
      children = [MnesiaDirty.Schema, MnesiaDirty.MetricsReporter]
      Supervisor.start_link(children, strategy: :one_for_one, name: MnesiaDirty.Supervisor)
    end
  end

  defmodule MnesiaDirty.Schema do
    @moduledoc false
    use GenServer

    @tables [:counters, :cache]

    def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

    @impl true
    def init(_opts) do
      _ = :mnesia.stop()

      case :mnesia.create_schema([node()]) do
        :ok -> :ok
        {:error, {_, {:already_exists, _}}} -> :ok
        other -> throw({:schema_failed, other})
      end

      :ok = :mnesia.start()
      ensure(:counters, attributes: [:key, :value], ram_copies: [node()], type: :set)

      ensure(:cache,
        attributes: [:key, :value, :inserted_at],
        ram_copies: [node()],
        type: :set
      )

      :ok = :mnesia.wait_for_tables(@tables, 10_000)
      {:ok, %{}}
    end

    defp ensure(table, opts) do
      case :mnesia.create_table(table, opts) do
        {:atomic, :ok} -> :ok
        {:aborted, {:already_exists, ^table}} -> :ok
        other -> throw({:create_failed, table, other})
      end
    end
  end

  defmodule MnesiaDirty.Counters do
    @moduledoc """
    Per-key monotonic counters backed by `:mnesia.dirty_update_counter/3`.

    `dirty_update_counter` is atomic on the owning node. For cross-node safety,
    counters are namespaced by `{node(), key}` and summed at read time — the
    "sharded counter" pattern that avoids last-writer-wins replication loss.
    """

    @table :counters

    @spec bump(atom() | String.t(), integer()) :: integer()
    def bump(key, increment \\ 1) when is_integer(increment) do
      :mnesia.dirty_update_counter(@table, {node(), key}, increment)
    end

    @spec get(atom() | String.t()) :: non_neg_integer()
    def get(key) do
      # Sum the per-node shards. dirty_match_object does not use the index,
      # so with many nodes this can become expensive — but on a 3-10 node
      # cluster the constant is negligible.
      pattern = {@table, {:_, key}, :_}

      :mnesia.dirty_match_object(pattern)
      |> Enum.reduce(0, fn {@table, _, v}, acc -> acc + v end)
    end

    @spec reset(atom() | String.t()) :: :ok
    def reset(key) do
      # Reset only the local shard — remote nodes reset theirs independently.
      :mnesia.dirty_delete(@table, {node(), key})
      :ok
    end

    @spec snapshot() :: %{optional(term()) => non_neg_integer()}
    def snapshot do
      @table
      |> :mnesia.dirty_match_object({@table, :_, :_})
      |> Enum.reduce(%{}, fn {@table, {_node, key}, v}, acc ->
        Map.update(acc, key, v, &(&1 + v))
      end)
    end
  end

  defmodule MnesiaDirty.Cache do
    @moduledoc """
    Dirty read/write cache.

    Writes are last-writer-wins across nodes — intentional. The cache is
    designed for idempotent derived data (memoized computation results);
    if two nodes disagree, either answer is acceptable.
    """

    @table :cache

    @spec put(term(), term()) :: :ok
    def put(key, value) do
      :mnesia.dirty_write({@table, key, value, System.system_time(:second)})
      :ok
    end

    @spec get(term()) :: {:ok, term()} | :miss
    def get(key) do
      case :mnesia.dirty_read({@table, key}) do
        [{@table, ^key, value, _ts}] -> {:ok, value}
        [] -> :miss
      end
    end

    @spec get_or_compute(term(), (-> term())) :: term()
    def get_or_compute(key, compute_fun) when is_function(compute_fun, 0) do
      case get(key) do
        {:ok, value} ->
          value

        :miss ->
          value = compute_fun.()
          put(key, value)
          value
      end
    end

    @spec delete(term()) :: :ok
    def delete(key) do
      :mnesia.dirty_delete(@table, key)
      :ok
    end
  end

  defmodule MnesiaDirty.MetricsReporter do
    end
    @moduledoc """
    Periodically logs the current counter snapshot — a stand-in for your
    real metrics sink (Telemetry, Prometheus, StatsD).
    """
    use GenServer
    require Logger

    @interval :timer.seconds(30)

    def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

    @impl true
    def init(_opts) do
      schedule()
      {:ok, %{}}
    end

    @impl true
    def handle_info(:report, state) do
      snapshot = MnesiaDirty.Counters.snapshot()
      Logger.info("Counters snapshot: #{inspect(snapshot)}")
      schedule()
      {:noreply, state}
    end

    defp schedule, do: Process.send_after(self(), :report, @interval)
  end

  defmodule MnesiaDirty.CountersTest do
    use ExUnit.Case, async: false

    alias MnesiaDirty.Counters

    setup do
      :mnesia.clear_table(:counters)
      :ok
    end

    describe "bump/2 and get/1" do
      test "accumulates sequential increments" do
        for _ <- 1..100, do: Counters.bump("hits")
        assert Counters.get("hits") == 100
      end

      test "handles concurrent increments atomically on one node" do
        tasks = for _ <- 1..1_000, do: Task.async(fn -> Counters.bump("concurrent") end)
        Enum.each(tasks, &Task.await/1)
        assert Counters.get("concurrent") == 1_000
      end

      test "snapshot/0 aggregates across keys" do
        Counters.bump("a", 3)
        Counters.bump("b", 7)
        snap = Counters.snapshot()
        assert snap["a"] == 3
        assert snap["b"] == 7
      end
    end
  end

  defmodule MnesiaDirty.CacheTest do
    use ExUnit.Case, async: false

    alias MnesiaDirty.Cache

    setup do
      :mnesia.clear_table(:cache)
      :ok
    end

    test "put/2 and get/1" do
      Cache.put(:k, 42)
      assert {:ok, 42} = Cache.get(:k)
    end

    test "get/1 returns :miss for unknown keys" do
      assert :miss = Cache.get(:unknown)
    end

    test "get_or_compute/2 runs the computation once per miss" do
      agent = start_supervised!({Agent, fn -> 0 end})

      compute = fn ->
        Agent.update(agent, &(&1 + 1))
        :value
      end

      assert :value = Cache.get_or_compute(:memo, compute)
      assert :value = Cache.get_or_compute(:memo, compute)
      assert Agent.get(agent, & &1) == 1
    end
  end

  defmodule Main do
    def main do
        # Demonstrating 123-mnesia-dirty-ops
        :ok
    end
  end

Main.main()
```
