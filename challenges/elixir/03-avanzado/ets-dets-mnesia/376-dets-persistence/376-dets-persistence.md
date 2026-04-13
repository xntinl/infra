# DETS for Simple Persistence Without SQL

**Project**: `kv_cache` — a disk-backed key-value cache for config data that survives restarts without introducing a database.

## Project context

You run a small tool — a CLI, a desktop app, an embedded service — that needs to persist a few thousand key-value pairs between restarts. You do not want to ship PostgreSQL, SQLite or any external store. You want files on disk, opened by the BEAM, queried like an ETS table. DETS (Disk ETS) is that.

DETS is older than Mnesia, still maintained, and the right tool for two niches:

1. **Embedded state**: a CLI remembering the last selected project; a desktop app persisting user preferences; `rebar3` using DETS to cache dependencies.
2. **Fallback persistence for ETS**: a GenServer holds data in ETS for fast reads, mirrors writes to DETS for durability. On restart, the ETS table is rebuilt from DETS.

DETS limitations: no transactions, 2 GB per file, single process owner, `:bag` and `:duplicate_bag` types but no `:ordered_set`. For anything bigger or more concurrent, use Mnesia or SQLite.

```
kv_cache/
├── lib/
│   └── kv_cache/
│       ├── application.ex
│       ├── cache.ex
│       └── persistence.ex
├── priv/
├── test/
│   └── kv_cache/
│       └── cache_test.exs
├── bench/
│   └── dets_bench.exs
└── mix.exs
```

## Why DETS and not SQLite

SQLite is objectively better for anything with joins, transactions or ad-hoc queries. DETS wins only when:

- you do not want a non-BEAM dependency,
- your operations are strictly key-value,
- your data fits in ≤ 2 GB,
- you want the same API as ETS (`:dets.insert/2`, `:dets.lookup/2`) for zero-cost migration between in-memory and on-disk.

## Why ETS-backed-by-DETS and not DETS alone

Pure DETS operations go to disk. On spinning disks, `:dets.lookup/2` can be 100× slower than `:ets.lookup/2`. For hot reads, keep the working set in ETS and sync to DETS in the background or on commit. This is the pattern used by `mnesia` `:disc_copies` and by `rebar3`.

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
### 1. `:dets.open_file/2`

Opens (or creates) a DETS file. Options include `:type` (`:set`, `:bag`, `:duplicate_bag`), `:auto_save` (ms between background flushes), `:access` (`:read_write` | `:read`). The returned name is the table reference for subsequent operations.

### 2. Auto-save vs explicit `:dets.sync/1`

DETS defaults to `auto_save: 180_000` (3 minutes). A crash within that window loses unsaved data. For durability, call `:dets.sync/1` after critical writes or reduce `auto_save`.

### 3. File integrity

If the BEAM crashes while DETS is holding the file, the next `open_file` runs a recovery pass: `:ok` or `{:repaired, _}`. If the file is corrupt, it moves it aside and returns `{:error, need_repair}`. Always check the return value.

### 4. Mirror pattern — ETS front + DETS back

On startup: load DETS into ETS. On write: update both. On read: hit ETS only. On shutdown: sync DETS.

## Design decisions

- **Option A — DETS only**: simple but slow reads.
- **Option B — ETS only**: fast but volatile.
- **Option C — ETS front + DETS back** (chosen): fast reads, durability, minor complexity.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule KvCache.MixProject do
  use Mix.Project

  def project do
    [app: :kv_cache, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application do
    [extra_applications: [:logger], mod: {KvCache.Application, []}]
  end

  defp deps do
    [{:benchee, "~> 1.3", only: :dev}]
  end
end
```

### Dependencies (mix.exs)

```elixir
```elixir
defmodule KvCache.MixProject do
  use Mix.Project

  def project do
    [app: :kv_cache, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application do
    [extra_applications: [:logger], mod: {KvCache.Application, []}]
  end

  defp deps do
    [{:benchee, "~> 1.3", only: :dev}]
  end
end
```

### Step 1: Persistence layer (DETS)

**Objective**: Isolate every DETS call behind a single module and handle `{:repaired, ...}` so corrupt files don't crash boot.

```elixir
# lib/kv_cache/persistence.ex
defmodule KvCache.Persistence do
  @moduledoc "Opens and syncs a DETS file. No other module touches the DETS handle."
  require Logger

  @spec open(Path.t()) :: {:ok, atom()} | {:error, term()}
  def open(path) do
    File.mkdir_p!(Path.dirname(path))
    name = :"dets_#{:erlang.phash2(path)}"

    case :dets.open_file(name, [{:file, to_charlist(path)}, {:type, :set}, {:auto_save, 5_000}]) do
      {:ok, ^name} ->
        {:ok, name}

      {:repaired, ^name, _r, _b} ->
        Logger.warning("DETS file #{path} was repaired on open")
        {:ok, name}

      {:error, reason} ->
        {:error, reason}
    end
  end

  @spec load_all(atom()) :: [{term(), term()}]
  def load_all(name), do: :dets.match_object(name, :_)

  @spec put(atom(), term(), term()) :: :ok | {:error, term()}
  def put(name, key, value), do: :dets.insert(name, {key, value})

  @spec delete(atom(), term()) :: :ok | {:error, term()}
  def delete(name, key), do: :dets.delete(name, key)

  @spec sync(atom()) :: :ok
  def sync(name), do: :dets.sync(name)

  @spec close(atom()) :: :ok
  def close(name), do: :dets.close(name)
end
```

### Step 2: Cache owner (ETS front + DETS back)

**Objective**: Serve hot reads from ETS and replay DETS into ETS at boot so cold starts don't hit disk per lookup.

```elixir
# lib/kv_cache/cache.ex
defmodule KvCache.Cache do
  @moduledoc """
  Key-value cache. Reads go to ETS. Writes update ETS and DETS.
  On startup, DETS is replayed into ETS.
  """
  use GenServer

  alias KvCache.Persistence

  @table :kv_cache_ets

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec get(term()) :: {:ok, term()} | :not_found
  def get(key) do
    case :ets.lookup(@table, key) do
      [{^key, value}] -> {:ok, value}
      [] -> :not_found
    end
  end

  @spec put(term(), term()) :: :ok
  def put(key, value), do: GenServer.call(__MODULE__, {:put, key, value})

  @spec delete(term()) :: :ok
  def delete(key), do: GenServer.call(__MODULE__, {:delete, key})

  @spec sync() :: :ok
  def sync, do: GenServer.call(__MODULE__, :sync)

  @impl true
  def init(opts) do
    path = Keyword.fetch!(opts, :path)
    :ets.new(@table, [:named_table, :set, :public, {:read_concurrency, true}])

    case Persistence.open(path) do
      {:ok, dets} ->
        dets
        |> Persistence.load_all()
        |> Enum.each(fn {k, v} -> :ets.insert(@table, {k, v}) end)

        Process.flag(:trap_exit, true)
        {:ok, %{dets: dets}}

      {:error, reason} ->
        {:stop, {:dets_open_failed, reason}}
    end
  end

  @impl true
  def handle_call({:put, k, v}, _from, state) do
    :ets.insert(@table, {k, v})
    Persistence.put(state.dets, k, v)
    {:reply, :ok, state}
  end

  def handle_call({:delete, k}, _from, state) do
    :ets.delete(@table, k)
    Persistence.delete(state.dets, k)
    {:reply, :ok, state}
  end

  def handle_call(:sync, _from, state) do
    Persistence.sync(state.dets)
    {:reply, :ok, state}
  end

  @impl true
  def terminate(_reason, state), do: Persistence.close(state.dets)
end
```

### Step 3: Application

**Objective**: Supervise the Cache with a configurable DETS path so tests and prod can point at different files.

```elixir
# lib/kv_cache/application.ex
defmodule KvCache.Application do
  use Application

  @impl true
  def start(_type, _args) do
    path = Application.get_env(:kv_cache, :path, "priv/kv_cache.dets")
    children = [{KvCache.Cache, path: path}]
    Supervisor.start_link(children, strategy: :one_for_one, name: KvCache.Supervisor)
  end
end
```

## Data flow diagram

```
  Startup:
    DETS file on disk ─── load_all ──▶ ETS table (in-memory)

  Read path (hot):
    Client ─── Cache.get(k) ─── :ets.lookup(k) ───▶ value
    (no disk I/O)

  Write path:
    Client ─── Cache.put(k, v) ──▶ GenServer
                                    │
                                    ├──▶ :ets.insert   (fast)
                                    └──▶ :dets.insert  (disk, durable within auto_save)

  Shutdown (graceful):
    terminate/2 ──▶ :dets.close ──▶ file flushed, consistent state
```

## Why this works

ETS holds the working set in DRAM for O(1) lookup. DETS provides durability via a file that is crash-recoverable. The asymmetry (reads fast, writes have disk cost) matches the common cache workload: 100× more reads than writes. `auto_save` limits data loss to 5 seconds in the worst case; `sync/0` gives a durability barrier for specific critical updates.

## Tests

```elixir
# test/kv_cache/cache_test.exs
defmodule KvCache.CacheTest do
  use ExUnit.Case, async: false

  alias KvCache.Cache

  @dets_path "priv/test_kv_cache.dets"

  setup do
    File.rm(@dets_path)
    :ok
  end

  describe "put/2 + get/1" do
    test "stores and retrieves a value" do
      :ok = Cache.put("k1", %{count: 1})
      assert {:ok, %{count: 1}} = Cache.get("k1")
    end

    test "returns :not_found for missing keys" do
      assert Cache.get("missing") == :not_found
    end
  end

  describe "delete/1" do
    test "removes an entry from ETS and DETS" do
      :ok = Cache.put("del_k", 42)
      :ok = Cache.delete("del_k")
      assert Cache.get("del_k") == :not_found
    end
  end

  describe "sync/0 — explicit durability" do
    test "returns :ok without error" do
      :ok = Cache.put("s", "v")
      assert :ok = Cache.sync()
    end
  end

  describe "DETS integrity helper (direct)" do
    alias KvCache.Persistence

    test "open creates the file and returns a handle" do
      path = "priv/test_standalone_dets.dets"
      File.rm(path)
      {:ok, name} = Persistence.open(path)
      :ok = Persistence.put(name, "a", 1)
      :ok = Persistence.sync(name)
      :ok = Persistence.close(name)

      {:ok, name2} = Persistence.open(path)
      assert [{"a", 1}] = Persistence.load_all(name2)
      Persistence.close(name2)
    end
  end
end
```

## Benchmark

```elixir
# bench/dets_bench.exs
alias KvCache.{Cache, Persistence}

{:ok, dets} = Persistence.open("priv/bench.dets")

Benchee.run(
  %{
    "ETS-front get (cached)" => fn ->
      Cache.get("warm_key")
    end,
    "ETS+DETS put" => fn ->
      Cache.put("warm_key", :rand.uniform(10_000))
    end,
    "DETS-only lookup (uncached)" => fn ->
      :dets.lookup(dets, :rand.uniform(10_000))
    end,
    "DETS-only insert" => fn ->
      :dets.insert(dets, {:rand.uniform(10_000), "v"})
    end
  },
  time: 5,
  warmup: 2
)

Cache.put("warm_key", :warm)
```

Target: `ETS-front get` under 2 µs. `ETS+DETS put` dominated by DETS write (~50–200 µs on SSD). `DETS-only lookup` 5–10 µs. The cache pattern gives you ETS-like reads and DETS durability.

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

1. **2 GB file limit is hard**: DETS files above ~2 GB fail with `{:error, system_limit}`. Shard across multiple files or migrate to Mnesia / SQLite.
2. **Single-process ownership**: the process that opened the DETS table must live as long as you want the table open. If that process dies, the table closes. Wrap in a supervised GenServer.
3. **No transactions across keys**: DETS operations are atomic per row only. For multi-row atomicity use Mnesia.
4. **Recovery is slow for large files**: an unclean shutdown with a 1 GB DETS file takes tens of seconds to repair on next open. Blocks application startup. Consider more frequent `sync/1` to shrink the recovery window.
5. **`:bag` type fragments inserts**: for set-like duplicate keys DETS is fine, but for high-insert duplicate-bag workloads it becomes inefficient. Prefer `:set` with composite keys.
6. **When NOT to use DETS**: anything above 1 GB, anything requiring transactions, anything that needs distributed replicas, anything needing ad-hoc SQL queries. Use SQLite for embedded, Mnesia for distributed BEAM-native, PostgreSQL for everything else.

## Reflection

Your cache file grew to 1.8 GB and you need to migrate to SQLite with zero downtime. Design the migration path: double-write period, read-through SQLite, cutover. How would you detect inconsistencies between the two stores during the migration window, and what is the rollback plan if SQLite performance is worse than expected?

## Executable Example

```elixir
defmodule KvCache.MixProject do
  use Mix.Project

  def project do
    [app: :kv_cache, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application do
    [extra_applications: [:logger], mod: {KvCache.Application, []}]
  end

  defp deps do
    [{:benchee, "~> 1.3", only: :dev}]
  end
end



### Step 1: Persistence layer (DETS)

**Objective**: Isolate every DETS call behind a single module and handle `{:repaired, ...}` so corrupt files don't crash boot.



### Step 2: Cache owner (ETS front + DETS back)

**Objective**: Serve hot reads from ETS and replay DETS into ETS at boot so cold starts don't hit disk per lookup.



### Step 3: Application

**Objective**: Supervise the Cache with a configurable DETS path so tests and prod can point at different files.

defmodule Main do
  def main do
      # Demonstrating 376-dets-persistence
      :ok
  end
end

Main.main()
```
