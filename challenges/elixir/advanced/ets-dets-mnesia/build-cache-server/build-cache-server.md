# Build a full cache server: TTL, LRU, eviction, telemetry, sharding

**Project**: `cache_server_full` — a production-grade in-memory cache with TTL, approximate LRU eviction, telemetry hooks, and optional sharding across N tables.

---

## The business problem

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

## Project structure

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
├── script/
│   └── main.exs
└── mix.exs
```

---

## Why building a cache and not importing one

Cache semantics are where correctness bugs hide. Building one end-to-end forces engagement with each subtlety — TTL precision, stampede, negative caching — that a library would hide.

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

### `mix.exs`
```elixir
defmodule BuildCacheServer.MixProject do
  use Mix.Project

  def project do
    [
      app: :build_cache_server,
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
```elixir
:telemetry.attach("my-cache", [:cache_server_full, :get], &handler/4, nil)
```

You can, and usually should. Building it yourself is valuable for:

- Pedagogy: understanding what those libraries actually do.
- Smaller dependency surface when the requirements are narrow.
- Custom telemetry or event schemas those libraries don't expose.

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

### `script/main.exs`
```elixir
defmodule CacheServerFull.MixProject do
  use Mix.Project

  def project do
    [app: :cache_server_full, version: "0.1.0", elixir: "~> 1.19", deps: deps()]
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

---

## Why Build a full cache server matters

Mastering **Build a full cache server** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `lib/cache_server_full.ex`

```elixir
defmodule CacheServerFull do
  @moduledoc """
  Reference implementation for Build a full cache server: TTL, LRU, eviction, telemetry, sharding.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the cache_server_full module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> CacheServerFull.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```

### `test/cache_server_full_test.exs`

```elixir
defmodule CacheServerFullTest do
  use ExUnit.Case, async: true

  doctest CacheServerFull

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert CacheServerFull.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts

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
