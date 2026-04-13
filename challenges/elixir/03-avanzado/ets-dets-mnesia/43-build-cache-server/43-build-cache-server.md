# Build a full cache server: TTL, LRU, eviction, telemetry, sharding

**Project**: `cache_server_full` — a production-grade in-memory cache with TTL, approximate LRU eviction, telemetry hooks, and optional sharding across N tables.

---

## Project context

This is the capstone ETS exercise where you assemble multiple cache techniques (concurrent flags,
counter primitives, LRU eviction patterns) into
a single component that a real service could depend on.

The goal: a `CacheServer.start_link/1` that accepts `max_size`, `ttl_ms`, `shards`, and a
`telemetry_prefix`, and exposes `get/1`, `put/2`, `put/3`, `delete/1`, `size/0`. The cache must
be safe under high concurrency, evict the oldest entries when `size > max_size` (approximate LRU),
and emit `:telemetry` events for hit/miss/eviction so operators can dashboard it.

This is the shape you'd expect from a library like Cachex or Nebulex, minus the 2k-line general
framework. You keep only the features your service needs, which is the pragmatic production
choice.

```
cache_server_full/
├── lib/
│   └── cache_server_full/
│       ├── application.ex
│       ├── cache.ex
│       ├── shard.ex
│       ├── janitor.ex
│       └── telemetry.ex
├── test/
│   └── cache_test.exs
└── mix.exs
```

---

## Why building a cache and not importing one

Cache semantics are where correctness bugs hide. Building one end-to-end forces engagement with each subtlety — TTL precision, stampede, negative caching — that a library would hide.

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
### 1. Sharding for contention reduction

A single ETS table under very high write contention bottlenecks on its lock regions. Splitting
into N tables keyed by `:erlang.phash2(key, N)` multiplies throughput by roughly N (up to
scheduler count). Each shard is an independent ETS table with its own `write_concurrency` set.

```
  put("user:42")  ─▶ phash2 → shard_3 ─▶ ets.insert(shard_3_table, ...)
  put("cart:10")  ─▶ phash2 → shard_0 ─▶ ets.insert(shard_0_table, ...)
```

### 2. TTL with lazy + active expiration

Each row is `{key, value, inserted_at, last_accessed_at}`. On `get`:

1. If `now - inserted_at > ttl_ms`, treat as miss and delete.
2. Else update `last_accessed_at` and return.

A background **Janitor** GenServer runs every `:sweep_interval_ms` and deletes rows where
`inserted_at < now - ttl_ms`. Without the Janitor, cold keys never get read and pile up.

### 3. Approximate LRU eviction

True LRU requires a doubly-linked list maintained on every access — expensive. An approximate
LRU samples K random rows, evicts the one with the oldest `last_accessed_at`, and repeats until
`size ≤ max_size`. This is the Redis approach ("allkeys-lru" uses K=5 by default).

### 4. Telemetry integration

Every `get` emits `[:cache_server_full, :get]` with `%{result: :hit | :miss}`. Every eviction
emits `[:cache_server_full, :evict]`. Consumers (Prometheus, LiveDashboard) attach handlers.

```elixir
:telemetry.attach("my-cache", [:cache_server_full, :get], &handler/4, nil)
```

### 5. Why not just use Cachex or Nebulex?

You can, and usually should. Building it yourself is valuable for:

- Pedagogy: understanding what those libraries actually do.
- Smaller dependency surface when the requirements are narrow.
- Custom telemetry or event schemas those libraries don't expose.

---

## Design decisions

**Option A — library (Cachex, Nebulex)**
- Pros: solved problem; battle-tested; feature-complete.
- Cons: learning by using, not by understanding.

**Option B — build your own on ETS + Supervisor** (chosen)
- Pros: full understanding of every trade-off; tailored to the exact use case.
- Cons: all the subtleties (TTL, stampede, eviction) are now yours.

→ Chose **B** because for learning, building is the point; in production pick the library unless you have a measured reason.

---

## Implementation

### Step 1: `mix.exs`

**Objective**: Pin `:telemetry` for hit/miss/eviction events and `:benchee` dev-only to measure shard scaling.

```elixir
defmodule CacheServerFull.MixProject do
  use Mix.Project

  def project do
    [app: :cache_server_full, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application, do: [extra_applications: [:logger], mod: {CacheServerFull.Application, []}]

  defp deps do
    [{:telemetry, "~> 1.2"}, {:benchee, "~> 1.3", only: [:dev, :test]}]
  end
end
```

### Dependencies (mix.exs)

```elixir
```elixir
:telemetry.attach("my-cache", [:cache_server_full, :get], &handler/4, nil)
```

### 5. Why not just use Cachex or Nebulex?

You can, and usually should. Building it yourself is valuable for:

- Pedagogy: understanding what those libraries actually do.
- Smaller dependency surface when the requirements are narrow.
- Custom telemetry or event schemas those libraries don't expose.

---

## Design decisions

**Option A — library (Cachex, Nebulex)**
- Pros: solved problem; battle-tested; feature-complete.
- Cons: learning by using, not by understanding.

**Option B — build your own on ETS + Supervisor** (chosen)
- Pros: full understanding of every trade-off; tailored to the exact use case.
- Cons: all the subtleties (TTL, stampede, eviction) are now yours.

→ Chose **B** because for learning, building is the point; in production pick the library unless you have a measured reason.

---

## Implementation

### Step 1: `mix.exs`

**Objective**: Pin `:telemetry` for hit/miss/eviction events and `:benchee` dev-only to measure shard scaling.

```elixir
defmodule CacheServerFull.MixProject do
  use Mix.Project

  def project do
    [app: :cache_server_full, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application, do: [extra_applications: [:logger], mod: {CacheServerFull.Application, []}]

  defp deps do
    [{:telemetry, "~> 1.2"}, {:benchee, "~> 1.3", only: [:dev, :test]}]
  end
end
```

### Step 2: `lib/cache_server_full/application.ex`

**Objective**: Size shards to `schedulers_online` and wire TTL + max_size via child args so the cache is tuneable per release.

```elixir
defmodule CacheServerFull.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {CacheServerFull.Cache,
       name: :default_cache, max_size: 10_000, ttl_ms: 60_000,
       shards: System.schedulers_online(), telemetry_prefix: [:cache_server_full]}
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: CacheServerFull.Supervisor)
  end
end
```

### Step 3: `lib/cache_server_full/shard.ex`

**Objective**: Create one `:public` ETS shard with `write_concurrency: :auto` so direct readers/writers bypass the GenServer owner.

```elixir
defmodule CacheServerFull.Shard do
  @moduledoc """
  One ETS shard. Each shard holds rows `{key, value, inserted_at, last_accessed_at}`.
  Created as `:public` so any process can read/write — writes pass through
  the janitor-aware logic in `Cache`, not through a GenServer.
  """

  @spec new(atom()) :: :ets.tid() | atom()
  def new(name) do
    :ets.new(name, [
      :named_table, :public, :set,
      read_concurrency: true,
      write_concurrency: :auto,
      decentralized_counters: true
    ])
  end
end
```

### Step 4: `lib/cache_server_full/telemetry.ex`

**Objective**: Implement the module in `lib/cache_server_full/telemetry.ex`.

```elixir
defmodule CacheServerFull.Telemetry do
  @moduledoc false

  @spec emit(list(atom()), atom(), map(), map()) :: :ok
  def emit(prefix, event, meta \\ %{}, measurements \\ %{}) do
    :telemetry.execute(prefix ++ [event], Map.merge(%{count: 1}, measurements), meta)
  end
end
```

### Step 5: `lib/cache_server_full/cache.ex`

**Objective**: Implement the module in `lib/cache_server_full/cache.ex`.

```elixir
defmodule CacheServerFull.Cache do
  @moduledoc """
  Public API and configuration holder. The GenServer owns the shard tables
  and the Janitor; reads/writes bypass the GenServer and hit ETS directly.
  """
  use GenServer

  alias CacheServerFull.{Shard, Telemetry, Janitor}

  @type opts :: [
          name: atom(),
          max_size: pos_integer(),
          ttl_ms: pos_integer(),
          shards: pos_integer(),
          telemetry_prefix: list(atom())
        ]

  # ---- Public API ---------------------------------------------------------

  def start_link(opts) do
    name = Keyword.fetch!(opts, :name)
    GenServer.start_link(__MODULE__, opts, name: name)
  end

  @spec get(atom(), term()) :: {:ok, term()} | :miss
  def get(cache \\ :default_cache, key) do
    {shards, ttl_ms, prefix} = config(cache)
    shard = shard_for(key, shards, cache)
    now = monotonic()

    case :ets.lookup(shard, key) do
      [{^key, value, inserted_at, _last}] ->
        if now - inserted_at > ttl_ms do
          :ets.delete(shard, key)
          Telemetry.emit(prefix, :get, %{cache: cache, result: :expired})
          :miss
        else
          :ets.update_element(shard, key, {4, now})
          Telemetry.emit(prefix, :get, %{cache: cache, result: :hit})
          {:ok, value}
        end

      [] ->
        Telemetry.emit(prefix, :get, %{cache: cache, result: :miss})
        :miss
    end
  end

  @spec put(atom(), term(), term()) :: :ok
  def put(cache \\ :default_cache, key, value) do
    {shards, _ttl_ms, _prefix} = config(cache)
    shard = shard_for(key, shards, cache)
    now = monotonic()
    :ets.insert(shard, {key, value, now, now})
    :ok
  end

  @spec delete(atom(), term()) :: :ok
  def delete(cache \\ :default_cache, key) do
    {shards, _ttl_ms, _prefix} = config(cache)
    shard = shard_for(key, shards, cache)
    :ets.delete(shard, key)
    :ok
  end

  @spec size(atom()) :: non_neg_integer()
  def size(cache \\ :default_cache) do
    {shards, _ttl_ms, _prefix} = config(cache)

    0..(shards - 1)
    |> Enum.map(fn i -> :ets.info(shard_name(cache, i), :size) end)
    |> Enum.sum()
  end

  # ---- GenServer ----------------------------------------------------------

  @impl true
  def init(opts) do
    name = Keyword.fetch!(opts, :name)
    shards = Keyword.fetch!(opts, :shards)
    ttl_ms = Keyword.fetch!(opts, :ttl_ms)
    max_size = Keyword.fetch!(opts, :max_size)
    prefix = Keyword.fetch!(opts, :telemetry_prefix)

    for i <- 0..(shards - 1) do
      Shard.new(shard_name(name, i))
    end

    :persistent_term.put({__MODULE__, name}, {shards, ttl_ms, prefix})

    {:ok, janitor} =
      Janitor.start_link(
        cache: name, shards: shards, ttl_ms: ttl_ms,
        max_size: max_size, telemetry_prefix: prefix
      )

    {:ok, %{name: name, janitor: janitor}}
  end

  # ---- helpers ------------------------------------------------------------

  defp config(cache), do: :persistent_term.get({__MODULE__, cache})

  defp shard_for(key, shards, cache) do
    i = :erlang.phash2(key, shards)
    shard_name(cache, i)
  end

  defp shard_name(cache, i), do: :"#{cache}_shard_#{i}"

  defp monotonic, do: System.monotonic_time(:millisecond)
end
```

### Step 6: `lib/cache_server_full/janitor.ex`

**Objective**: Implement the module in `lib/cache_server_full/janitor.ex`.

```elixir
defmodule CacheServerFull.Janitor do
  @moduledoc """
  Background process that:
    - Deletes TTL-expired rows (sweep).
    - Enforces max_size via approximate-LRU eviction.
  """
  use GenServer

  alias CacheServerFull.Telemetry

  @sweep_interval_ms 1_000
  @lru_sample 5

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts)

  @impl true
  def init(opts) do
    state = Map.new(opts)
    Process.send_after(self(), :sweep, @sweep_interval_ms)
    {:ok, state}
  end

  @impl true
  def handle_info(:sweep, state) do
    expire_ttl(state)
    enforce_max_size(state)
    Process.send_after(self(), :sweep, @sweep_interval_ms)
    {:noreply, state}
  end

  defp expire_ttl(%{cache: cache, shards: n, ttl_ms: ttl_ms, telemetry_prefix: prefix}) do
    cutoff = System.monotonic_time(:millisecond) - ttl_ms

    Enum.each(0..(n - 1), fn i ->
      shard = :"#{cache}_shard_#{i}"
      # Match spec: delete rows whose inserted_at (pos 3) is < cutoff
      ms = [{{:_, :_, :"$1", :_}, [{:<, :"$1", cutoff}], [true]}]
      deleted = :ets.select_delete(shard, ms)

      if deleted > 0 do
        Telemetry.emit(prefix, :evict, %{cache: cache, reason: :ttl}, %{count: deleted})
      end
    end)
  end

  defp enforce_max_size(%{cache: cache, shards: n, max_size: max, telemetry_prefix: prefix}) do
    total =
      Enum.sum(for i <- 0..(n - 1), do: :ets.info(:"#{cache}_shard_#{i}", :size))

    if total > max do
      to_evict = total - max

      Enum.each(1..to_evict, fn _ ->
        evict_one_lru(cache, n, prefix)
      end)
    end
  end

  defp evict_one_lru(cache, n, prefix) do
    # Sample @lru_sample rows across random shards, drop the oldest.
    samples =
      for _ <- 1..@lru_sample do
        shard = :"#{cache}_shard_#{:rand.uniform(n) - 1}"
        sample_one(shard)
      end
      |> Enum.reject(&is_nil/1)

    case samples do
      [] ->
        :ok

      rows ->
        {shard, key, _last} = Enum.min_by(rows, fn {_s, _k, last} -> last end)
        :ets.delete(shard, key)
        Telemetry.emit(prefix, :evict, %{cache: cache, reason: :lru})
    end
  end

  defp sample_one(shard) do
    case :ets.first(shard) do
      :"$end_of_table" ->
        nil

      key ->
        case :ets.lookup(shard, key) do
          [{^key, _v, _inserted, last}] -> {shard, key, last}
          _ -> nil
        end
    end
  end
end
```

### Step 7: `test/cache_test.exs`

**Objective**: Write tests in `test/cache_test.exs` covering behavior and edge cases.

```elixir
defmodule CacheServerFull.CacheTest do
  use ExUnit.Case, async: false

  alias CacheServerFull.Cache

  @cache :test_cache

  setup do
    stop_if_running(@cache)

    start_supervised!({Cache,
      name: @cache, max_size: 50, ttl_ms: 100,
      shards: 4, telemetry_prefix: [:test_cache]})

    :ok
  end

  defp stop_if_running(name) do
    case Process.whereis(name) do
      nil -> :ok
      pid -> GenServer.stop(pid, :normal, 1_000)
    end
  end

  describe "put/get/delete" do
    test "round-trip" do
      :ok = Cache.put(@cache, :k1, "v1")
      assert {:ok, "v1"} = Cache.get(@cache, :k1)
    end

    test "miss returns :miss" do
      assert :miss = Cache.get(@cache, :nope)
    end

    test "delete removes" do
      Cache.put(@cache, :k, 1)
      Cache.delete(@cache, :k)
      assert :miss = Cache.get(@cache, :k)
    end
  end

  describe "TTL" do
    test "entry expires after ttl_ms" do
      Cache.put(@cache, :ttl_key, "v")
      Process.sleep(150)
      assert :miss = Cache.get(@cache, :ttl_key)
    end
  end

  describe "LRU eviction" do
    test "size stays under max_size after the janitor runs" do
      for i <- 1..200, do: Cache.put(@cache, {:k, i}, i)
      # Give janitor at least one sweep cycle (1s) to catch up
      Process.sleep(1_300)
      assert Cache.size(@cache) <= 50
    end
  end

  describe "telemetry" do
    test "emits :hit, :miss, :expired" do
      ref = make_ref()
      parent = self()

      :telemetry.attach(
        "test-#{inspect(ref)}",
        [:test_cache, :get],
        fn _name, _measure, meta, _ -> send(parent, {ref, meta.result}) end,
        nil
      )

      Cache.get(@cache, :absent)
      assert_receive {^ref, :miss}, 500

      Cache.put(@cache, :present, 1)
      Cache.get(@cache, :present)
      assert_receive {^ref, :hit}, 500

      :telemetry.detach("test-#{inspect(ref)}")
    end
  end
end
```

### Step 8: Run it

**Objective**: Exercise the implementation end-to-end in IEx or the shell.

```bash
mix deps.get
mix test --trace
```

### Why this works

A cache server is an ETS table, a supervisor that owns it, a TTL sweeper, and a stampede protection strategy. Each piece is small; the interaction between them is what matters.

---

## Benchmark suggestions

Drop this into `bench/cache_bench.exs` to see sharding in action:

```elixir
Benchee.run(
  %{
    "get (hit)" => fn ->
      CacheServerFull.Cache.get(:default_cache, :erlang.phash2({:k, :rand.uniform(1_000)}))
    end,
    "put" => fn ->
      CacheServerFull.Cache.put(:default_cache, :erlang.phash2({:k, :rand.uniform(1_000)}), 1)
    end
  },
  parallel: System.schedulers_online(), time: 5, warmup: 2
)
```

Expected: with `shards: 12` on a 12-core box, sustained put throughput ≈ 8–12 M ops/s.

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

**1. `:persistent_term` for config is a trade-off.** Reads are zero-copy fast, but every
`:persistent_term.put/2` triggers a global GC. Set config once at init, never at runtime per
request.

**2. Approximate LRU can evict a "hot" key occasionally.** The sampling method can miss a
recently-used key if it wasn't in the sample. Acceptable for caches; unacceptable for exact LRU
requirements (use a doubly-linked list instead).

**3. TTL semantics: lazy + active, not just lazy.** Without the janitor, cold expired entries
occupy memory forever. Don't skip it "to save CPU".

**4. Sharding helps writes, not cross-shard operations.** `size/0` sums N shards — O(N). For
millions of shards you'd need a decentralized counter per shard and lazy aggregation.

**5. Telemetry handlers run synchronously in the caller.** A slow handler (logging to disk,
calling Prometheus with HTTP) blocks every `get`. Attach fast handlers or dispatch to a separate
process.

**6. `:persistent_term.put` triggers global GC — DO NOT use it for TTL bookkeeping.** Use ETS
for anything that mutates.

**7. When NOT to use this.** If you need distributed caching across a cluster, this is the wrong
shape. Use `:pg`/`Phoenix.PubSub` for invalidation, Cachex's distributed mode, or an external
Redis.

---

## Reflection

- At 100k QPS, does your hand-built cache beat a library, or have you accidentally rebuilt a worse version? How would you measure?
- Which feature of a real production cache did you skip, and how would you notice in production that you needed it?

---

## Executable Example

```elixir
defmodule CacheServerFull.MixProject do
  end
  use Mix.Project

  def project do
    [app: :cache_server_full, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application, do: [extra_applications: [:logger], mod: {CacheServerFull.Application, []}]

  defp deps do
    [{:telemetry, "~> 1.2"}, {:benchee, "~> 1.3", only: [:dev, :test]}]
  end
end

defmodule Main do
  def main do
      # Demonstrating 43-build-cache-server
      :ok
  end
end

Main.main()
```
