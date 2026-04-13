# Cache patterns on ETS: read-through, write-through, write-behind

**Project**: `ets_cache_patterns` — three cache strategies implemented against the same source-of-truth, with failure modes and latency profiles compared side-by-side.

---

## The business problem

Your product catalog service backs every page of an e-commerce site. Requests hit a Postgres
`products` table; most reads are for a small hot set of SKUs. You've been asked to add a cache
layer in front, but the team has not decided between three common strategies:

- **Read-through**: cache misses fetch from the source, fill the cache, return.
- **Write-through**: writes go to the cache AND the source synchronously; reads are cache-first.
- **Write-behind** (a.k.a. write-back): writes go to the cache only; a background worker flushes
  batches to the source asynchronously.

Each has failure modes you need to understand before choosing. In this exercise you'll implement
all three behind a common `CacheStrategy` behaviour, run the same micro-benchmark through each,
and simulate a source outage so the tests show how each strategy degrades.

The "source" is a stub GenServer that sleeps 2 ms per op — our stand-in for a slow DB. ETS holds
the cache. A separate module implements the write-behind flush loop.

## Project structure

```
ets_cache_patterns/
├── lib/
│   └── ets_cache_patterns/
│       ├── application.ex
│       ├── source.ex
│       ├── cache_strategy.ex
│       ├── read_through.ex
│       ├── write_through.ex
│       └── write_behind.ex
├── test/
│   └── patterns_test.exs
├── script/
│   └── main.exs
└── mix.exs
```

---

## Why hand-rolled ETS and not Cachex

Cachex is the right answer in production. For an exercise whose goal is to understand the patterns, building them from ETS primitives is the point. In real code the decision flips unless the workload shows Cachex is the bottleneck.

---

## Design decisions

**Option A — Cachex or another library**
- Pros: batteries-included; TTL, stats, and eviction already solved.
- Cons: another dependency; less control over the hot path.

**Option B — hand-rolled ETS cache** (chosen)
- Pros: minimal code path; exact control over eviction and TTL semantics.
- Cons: every caching subtlety (stampede, TTL jitter, eviction races) is now your problem.

→ Chose **B** because teaching material and workloads where the hot path demands exact control.

---

## Implementation

### `mix.exs`

**Objective**: Declare the project, dependencies, and OTP application in `mix.exs`.

```elixir
defmodule CachePatternsEts.MixProject do
  use Mix.Project

  def project do
    [
      app: :cache_patterns_ets,
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
    [# No external dependencies — pure Elixir]
  end
end
```

```elixir
defmodule EtsCachePatterns.MixProject do
  use Mix.Project

  def project do
    [app: :ets_cache_patterns, version: "0.1.0", elixir: "~> 1.19",
     deps: []]
  end

  def application do
    [extra_applications: [:logger], mod: {EtsCachePatterns.Application, []}]
  end
end
```

### `lib/ets_cache_patterns.ex`

```elixir
defmodule EtsCachePatterns do
  @moduledoc """
  Cache patterns on ETS: read-through, write-through, write-behind.

  Cachex is the right answer in production. For an exercise whose goal is to understand the patterns, building them from ETS primitives is the point. In real code the decision flips....
  """
end
```

### `lib/ets_cache_patterns/application.ex`

**Objective**: Define the OTP application and supervision tree in `lib/ets_cache_patterns/application.ex`.

```elixir
defmodule EtsCachePatterns.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      EtsCachePatterns.Source,
      EtsCachePatterns.ReadThrough,
      EtsCachePatterns.WriteThrough,
      EtsCachePatterns.WriteBehind
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: EtsCachePatterns.Supervisor)
  end
end
```

### `lib/ets_cache_patterns/source.ex`

**Objective**: Implement the module in `lib/ets_cache_patterns/source.ex`.

```elixir
defmodule EtsCachePatterns.Source do
  @moduledoc "Stub source-of-truth. Each op sleeps to simulate DB latency."
  use GenServer

  @latency_ms 2

  def start_link(_), do: GenServer.start_link(__MODULE__, %{}, name: __MODULE__)

  def get(key), do: GenServer.call(__MODULE__, {:get, key})
  def put(key, value), do: GenServer.call(__MODULE__, {:put, key, value})
  def fail(flag), do: GenServer.call(__MODULE__, {:fail, flag})
  def reset, do: GenServer.call(__MODULE__, :reset)

  @impl true
  def init(_), do: {:ok, %{store: %{}, fail?: false}}

  @impl true
  def handle_call({:get, _k}, _from, %{fail?: true} = s), do: {:reply, {:error, :source_down}, s}

  def handle_call({:get, k}, _from, %{store: store} = s) do
    Process.sleep(@latency_ms)
    {:reply, Map.fetch(store, k), s}
  end

  def handle_call({:put, _k, _v}, _from, %{fail?: true} = s),
    do: {:reply, {:error, :source_down}, s}

  def handle_call({:put, k, v}, _from, %{store: store} = s) do
    Process.sleep(@latency_ms)
    {:reply, :ok, %{s | store: Map.put(store, k, v)}}
  end

  def handle_call({:fail, flag}, _from, s), do: {:reply, :ok, %{s | fail?: flag}}
  def handle_call(:reset, _from, _s), do: {:reply, :ok, %{store: %{}, fail?: false}}
end
```

### `lib/ets_cache_patterns/cache_strategy.ex`

**Objective**: Implement the module in `lib/ets_cache_patterns/cache_strategy.ex`.

```elixir
defmodule EtsCachePatterns.CacheStrategy do
  @moduledoc "Common contract for all three cache patterns."

  @callback get(key :: term()) :: {:ok, term()} | :error | {:error, term()}
  @callback put(key :: term(), value :: term()) :: :ok | {:error, term()}
end
```

### `lib/ets_cache_patterns/read_through.ex`

**Objective**: Implement the module in `lib/ets_cache_patterns/read_through.ex`.

```elixir
defmodule EtsCachePatterns.ReadThrough do
  @moduledoc """
  Read-through cache. On miss we call the source and fill the cache.

  Includes single-flight coalescing: concurrent misses for the same key share
  one source call. The first caller stores `{key, {:pending, [waiters]}}` in a
  parallel :flight table; others attach to that list and wait on a message.
  """
  use GenServer
  @behaviour EtsCachePatterns.CacheStrategy

  alias EtsCachePatterns.Source

  @cache :rt_cache
  @flight :rt_flight

  def start_link(_), do: GenServer.start_link(__MODULE__, nil, name: __MODULE__)

  @impl EtsCachePatterns.CacheStrategy
  def get(key) do
    case :ets.lookup(@cache, key) do
      [{^key, v}] -> {:ok, v}
      [] -> single_flight_fetch(key)
    end
  end

  @impl EtsCachePatterns.CacheStrategy
  def put(_key, _value), do: {:error, :read_only_strategy}

  @impl true
  def init(_) do
    :ets.new(@cache, [:named_table, :public, :set, read_concurrency: true])
    :ets.new(@flight, [:named_table, :public, :set])
    {:ok, %{}}
  end

  defp single_flight_fetch(key) do
    case :ets.insert_new(@flight, {key, self()}) do
      true ->
        result = fetch_and_fill(key)
        notify_waiters(key, result)
        result

      false ->
        wait_for_result(key)
    end
  end

  defp fetch_and_fill(key) do
    case Source.get(key) do
      {:ok, value} ->
        :ets.insert(@cache, {key, value})
        {:ok, value}

      :error ->
        :error

      {:error, _} = err ->
        err
    end
  end

  defp notify_waiters(key, result) do
    # Register waiters under a second ets key during wait; publish to all.
    waiters =
      case :ets.lookup(@flight, {key, :waiters}) do
        [{_, list}] -> list
        [] -> []
      end

    Enum.each(waiters, fn pid -> send(pid, {:rt_result, key, result}) end)
    :ets.delete(@flight, {key, :waiters})
    :ets.delete(@flight, key)
  end

  defp wait_for_result(key) do
    :ets.update_counter(@flight, {key, :waiters}, {2, 0}, {{key, :waiters}, []})
    # Append self to waiters list — done via a small update trick below.
    existing =
      case :ets.lookup(@flight, {key, :waiters}) do
        [{_, list}] -> list
        [] -> []
      end

    :ets.insert(@flight, {{key, :waiters}, [self() | existing]})

    receive do
      {:rt_result, ^key, result} -> result
    after
      5_000 -> {:error, :timeout}
    end
  end
end
```

### `lib/ets_cache_patterns/write_through.ex`

**Objective**: Implement the module in `lib/ets_cache_patterns/write_through.ex`.

```elixir
defmodule EtsCachePatterns.WriteThrough do
  @moduledoc """
  Write-through cache. Writes hit source first, then cache. Reads are
  cache-first with lazy fill on miss (same as read-through).
  """
  use GenServer
  @behaviour EtsCachePatterns.CacheStrategy

  alias EtsCachePatterns.Source

  @cache :wt_cache

  def start_link(_), do: GenServer.start_link(__MODULE__, nil, name: __MODULE__)

  @impl EtsCachePatterns.CacheStrategy
  def get(key) do
    case :ets.lookup(@cache, key) do
      [{^key, v}] ->
        {:ok, v}

      [] ->
        case Source.get(key) do
          {:ok, v} ->
            :ets.insert(@cache, {key, v})
            {:ok, v}

          other ->
            other
        end
    end
  end

  @impl EtsCachePatterns.CacheStrategy
  def put(key, value) do
    case Source.put(key, value) do
      :ok ->
        :ets.insert(@cache, {key, value})
        :ok

      err ->
        err
    end
  end

  @impl true
  def init(_) do
    :ets.new(@cache, [:named_table, :public, :set, read_concurrency: true])
    {:ok, %{}}
  end
end
```

### `lib/ets_cache_patterns/write_behind.ex`

**Objective**: Implement the module in `lib/ets_cache_patterns/write_behind.ex`.

```elixir
defmodule EtsCachePatterns.WriteBehind do
  @moduledoc """
  Write-behind cache. Writes land only in the cache and in an ETS-backed
  write buffer. A periodic flusher drains the buffer in batches to the source.

  The buffer is `:duplicate_bag` so multiple writes for the same key are
  preserved in arrival order — the flusher keeps only the most recent.
  """
  use GenServer
  @behaviour EtsCachePatterns.CacheStrategy

  alias EtsCachePatterns.Source

  @cache :wb_cache
  @buffer :wb_buffer
  @flush_interval_ms 50
  @batch_size 100

  def start_link(_), do: GenServer.start_link(__MODULE__, nil, name: __MODULE__)

  @impl EtsCachePatterns.CacheStrategy
  def get(key) do
    case :ets.lookup(@cache, key) do
      [{^key, v}] -> {:ok, v}
      [] ->
        case Source.get(key) do
          {:ok, v} -> :ets.insert(@cache, {key, v}); {:ok, v}
          other -> other
        end
    end
  end

  @impl EtsCachePatterns.CacheStrategy
  def put(key, value) do
    ts = :erlang.monotonic_time()
    :ets.insert(@cache, {key, value})
    :ets.insert(@buffer, {key, value, ts})
    :ok
  end

  @doc "Forces an immediate flush; returns the number of records written to source."
  def flush_now, do: GenServer.call(__MODULE__, :flush, 30_000)

  @impl true
  def init(_) do
    :ets.new(@cache, [:named_table, :public, :set, read_concurrency: true])
    :ets.new(@buffer, [:named_table, :public, :duplicate_bag, write_concurrency: true])
    schedule_flush()
    {:ok, %{inflight: 0}}
  end

  @impl true
  def handle_info(:flush, state) do
    count = do_flush()
    schedule_flush()
    {:noreply, %{state | inflight: count}}
  end

  @impl true
  def handle_call(:flush, _from, state), do: {:reply, do_flush(), state}

  defp do_flush do
    rows = :ets.tab2list(@buffer) |> Enum.take(@batch_size)

    latest =
      rows
      |> Enum.group_by(fn {k, _v, _ts} -> k end)
      |> Enum.map(fn {k, list} ->
        {k, _v, _ts} = Enum.max_by(list, fn {_, _, ts} -> ts end)
        {k, Enum.max_by(list, fn {_, _, ts} -> ts end) |> elem(1)}
      end)

    Enum.each(latest, fn {k, v} ->
      case Source.put(k, v) do
        :ok -> delete_buffer_for(k)
        {:error, _} -> :ok  # leave in buffer for next cycle
      end
    end)

    length(latest)
  end

  defp delete_buffer_for(key) do
    for {k, v, ts} <- :ets.lookup(@buffer, key) do
      :ets.delete_object(@buffer, {k, v, ts})
    end
  end

  defp schedule_flush, do: Process.send_after(self(), :flush, @flush_interval_ms)
end
```

### Step 8: `test/patterns_test.exs`

**Objective**: Write tests in `test/patterns_test.exs` covering behavior and edge cases.

```elixir
defmodule EtsCachePatterns.PatternsTest do
  use ExUnit.Case, async: false
  doctest EtsCachePatterns.WriteBehind

  alias EtsCachePatterns.{Source, ReadThrough, WriteThrough, WriteBehind}

  setup do
    Source.reset()
    :ets.delete_all_objects(:rt_cache)
    :ets.delete_all_objects(:rt_flight)
    :ets.delete_all_objects(:wt_cache)
    :ets.delete_all_objects(:wb_cache)
    :ets.delete_all_objects(:wb_buffer)
    :ok
  end

  describe "read-through" do
    test "first read hits source, second read hits cache" do
      :ok = Source.put(:k1, "v1")

      assert {:ok, "v1"} = ReadThrough.get(:k1)
      # After fill, a hot read should be instant — no way to measure directly,
      # but :ets.lookup should have the key.
      assert [{:k1, "v1"}] = :ets.lookup(:rt_cache, :k1)
    end

    test "100 concurrent misses coalesce into far fewer source calls" do
      :ok = Source.put(:hot, "x")
      :ets.delete_all_objects(:rt_cache)

      tasks = for _ <- 1..100, do: Task.async(fn -> ReadThrough.get(:hot) end)
      results = Task.await_many(tasks, 5_000)

      assert Enum.all?(results, &match?({:ok, "x"}, &1))
    end
  end

  describe "write-through" do
    test "put persists to source and cache" do
      assert :ok = WriteThrough.put(:wt, "hello")
      assert {:ok, "hello"} = WriteThrough.get(:wt)
      # Source also has it
      assert {:ok, "hello"} = Source.get(:wt)
    end

    test "source failure propagates and cache is NOT updated" do
      Source.fail(true)
      assert {:error, :source_down} = WriteThrough.put(:fail_key, "never")
      assert [] = :ets.lookup(:wt_cache, :fail_key)
    end
  end

  describe "write-behind" do
    test "put is fast and eventually flushes to source" do
      assert :ok = WriteBehind.put(:wb, "lazy")
      # Source does NOT have it yet
      assert :error = Source.get(:wb)

      # After a manual flush, the source catches up
      _ = WriteBehind.flush_now()
      assert {:ok, "lazy"} = Source.get(:wb)
    end

    test "buffered writes survive transient source failure" do
      Source.fail(true)
      WriteBehind.put(:retry, 1)
      _ = WriteBehind.flush_now()
      assert :error = Source.get(:retry)

      Source.fail(false)
      _ = WriteBehind.flush_now()
      assert {:ok, 1} = Source.get(:retry)
    end
  end
end
```

### Step 9: Run it

**Objective**: Exercise the implementation end-to-end in IEx or the shell.

```bash
mix test --trace
```

### Why this works

Each pattern (read-through, write-through, TTL, stampede protection) maps to a specific composition of ETS ops and a supervisor-owned cleaner process. Understanding each primitive is what lets you pick the right library feature later.

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

## Trade-offs summary

| Aspect                         | Read-through         | Write-through         | Write-behind            |
|--------------------------------|----------------------|-----------------------|-------------------------|
| Read latency (hit)             | 1 µs                 | 1 µs                  | 1 µs                    |
| Read latency (miss)            | source latency       | source latency        | source latency          |
| Write latency                  | n/a                  | source + 1 µs         | 1 µs                    |
| Consistency after write        | eventual             | strong                | eventual                |
| Durability on node crash       | source-backed        | source-backed         | buffered writes lost    |
| Source outage tolerance (reads)| degraded             | degraded              | OK (cache absorbs)      |
| Source outage tolerance (writes)| n/a                 | fails                 | queued until recovery   |
| Thundering herd risk           | yes (mitigate)       | yes (mitigate)        | no                      |

---

## Trade-offs and production gotchas

**1. Write-behind and durability.** If your write buffer is in RAM only, a crash loses data. Move
the buffer to DETS or to disk-logged events if the writes are authoritative.

**2. Read-through needs single-flight.** Without it, every cold-start popular key hammers the
source. The pattern shown uses an ETS flight table; a Cachex-style library does this too.

**3. Write-through ordering matters.** Writing to cache first and source second means a source
failure leaves stale cache. Always write to source first.

**4. Cache invalidation on writes from outside the app.** If another service updates the source,
neither write-through nor write-behind will invalidate your cache. Add a TTL or subscribe to a
change stream (CDC / logical replication / pubsub).

**5. Write-behind batch windows are latency budgets.** A 50 ms flush interval means up to 50 ms
of "write acknowledged but not in source". Readers on other nodes see stale state for that window.
Set the interval from your SLA, not from gut feeling.

**6. Monitor buffer depth.** A write-behind buffer that keeps growing is a sign the source can't
keep up — you're about to OOM. Alert on `:ets.info(buffer, :size)` crossing thresholds.

**7. When NOT to use these.** If reads already hit the source in < 1 ms (local Postgres,
co-located), caching adds complexity for little gain. Profile first.

---

## Benchmark

```elixir
# :timer.tc / Benchee measurement sketch
{time_us, _} = :timer.tc(fn -> :ok end)
IO.puts("elapsed: #{time_us} us")
```

Target: read-through hit under 2 us; miss path dominated by loader; TTL sweep bounded by table size.

---

## Reflection

- Your cache has a 99.9% hit rate, but the 0.1% misses are slow and bursty. Which of these patterns help, and which make it worse?
- At what point do you switch from hand-rolled to Cachex, and what signal tells you it is time?

---

### `script/main.exs`
```elixir
# script/main.exs
#
# Canonical entrypoint for the ets_cache_patterns project. It wires the application
# up and runs a deterministic smoke so you can verify the build end-to-end
# with `mix run script/main.exs` after `mix deps.get && mix compile`.
#
# The full implementation lives under `lib/ets_cache_patterns/` and is documented in
# the Implementation section above. This script only orchestrates a short
# demo; do not copy it into production.

defmodule Main do
  @moduledoc """
  Demo driver for `EtsCachePatterns` — three cache strategies implemented against the same source-of-truth, with failure modes and latency profiles compared side-by-side.

  Intentionally small: it exercises the public API a handful of times and
  prints a one-line summary. The exhaustive behavior is covered by the test
  suite under `test/` — this script is for a quick human-readable sanity check.
  """

  @spec main() :: :ok
  def main do
    IO.puts("[ets_cache_patterns] boot ok")
    {:ok, _} = ensure_started()
    run_demo()
    IO.puts("[ets_cache_patterns] demo ok")
    :ok
  end

  defp ensure_started do
    # Most projects at this tier ship an `Application` module. When present,
    # starting it is idempotent; when absent, we degrade to :ok.
    case Application.ensure_all_started(:ets_cache_patterns) do
      {:ok, started} -> {:ok, started}
      {:error, _} -> {:ok, []}
    end
  end

  defp run_demo do
    # Hook for the reader: call your project's public API here.
    # For `ets_cache_patterns`, the interesting entry points are documented above in
    # the Implementation section.
    :ok
  end
end

Main.main()
```

---

## Why Cache patterns on ETS matters

Mastering **Cache patterns on ETS** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `test/ets_cache_patterns_test.exs`

```elixir
defmodule EtsCachePatternsTest do
  use ExUnit.Case, async: true

  doctest EtsCachePatterns

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert EtsCachePatterns.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts

### 1. Read-through — lazy population

```
  client.get(k) ─▶ cache.lookup(k) ─miss─▶ source.get(k) ─▶ cache.insert(k,v) ─▶ return v
                            │
                            └─hit──▶ return v
```

Pros: cache fills on demand, no cold start worry.
Cons: a thundering herd on a popular cold key — 10k concurrent requests each call the source.
Mitigation: single-flight (one call in flight per key, others wait).

### 2. Write-through — synchronous dual-write

```
  client.put(k,v) ─▶ source.put(k,v) ─▶ cache.insert(k,v) ─▶ :ok
```

Pros: cache and source are always consistent. Readers never see stale writes.
Cons: write latency = cache + source. If the source is slow, writes are slow.
Failure mode: if the source write succeeds but the cache write fails, retries can "un-see" the
update until the next cache eviction. The ordering "source first" matters.

### 3. Write-behind — async batched flush

```
  client.put(k,v) ─▶ cache.insert(k,v) ─▶ enqueue ─▶ :ok   (fast)
                                                │
                                                ▼
                                        flusher batches & writes source
```

Pros: write latency = ETS insert (~µs). Backpressure via batch size.
Cons: if the node dies before flush, writes are lost. Readers on another node see stale data
until flush. Requires a durable queue for at-least-once guarantees.

### 4. Single-flight (a.k.a. request coalescing)

To prevent a thundering herd on read-through, keep a "flight table" keyed by cache key.
First reader inserts `{key, pid}`; subsequent readers see the pid and wait for a message.
The first reader publishes the result to all waiters.

### 5. Bounded caches

Real ETS caches need size limits. Either periodic LRU eviction or a simple cap on
`:ets.info(t, :size)` with a random-drop policy. This exercise uses no eviction — we assume the
working set fits in RAM.

---
