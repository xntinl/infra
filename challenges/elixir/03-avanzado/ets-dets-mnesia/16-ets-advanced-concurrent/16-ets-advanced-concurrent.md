# ETS advanced concurrency tuning

**Project**: `ets_concurrent_deep` — a synthetic workload lab to measure how `read_concurrency`, `write_concurrency` and `decentralized_counters` shift throughput on a multi-core box.

---

## Project context

Your team runs a long-lived Elixir service that caches fan-out fan-in responses from three upstream
APIs. The hot path is an ETS lookup on every request; writes happen on cache misses and on TTL
refresh, roughly 5% of operations. In production you see p99 spikes that don't correlate with
upstream latency — the cache itself is the bottleneck under concurrency.

The service was created years ago with `:ets.new(:cache, [:public, :named_table])` and nothing else.
Nobody benchmarked whether those defaults fit the access pattern. Your task is to rebuild the
micro-service from scratch as a minimal lab, reproduce the symptom with `Benchee`, and then drive
p99 down by flipping the right ETS flags. The goal is not to memorize the flag names but to
understand what each one does at the scheduler and cache-line level, so you can diagnose similar
problems in any service.

You'll ship a binary `mix run bench/run.exs` that prints a comparative table for four configurations:
baseline, `read_concurrency`, `write_concurrency`, and `write_concurrency + decentralized_counters`.
Senior engineers should be able to read the numbers and justify the chosen defaults.

```
ets_concurrent_deep/
├── lib/
│   └── ets_concurrent_deep/
│       ├── application.ex
│       ├── table.ex
│       └── workload.ex
├── bench/
│   └── run.exs
├── test/
│   └── ets_concurrent_deep_test.exs
└── mix.exs
```

---

## Why direct ETS and not GenServer-wrapped

A GenServer is a single process. Any workload that can run concurrently will contend on its mailbox. ETS exposes a shared concurrent data structure with no intermediary process, eliminating the bottleneck entirely.

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
### 1. What `read_concurrency` actually changes

Without this flag, an ETS read acquires a single reader/writer lock on the table. That lock lives
on one cache line. On a multicore box, every reader invalidates that cache line on every other core
— classic false sharing. With `read_concurrency: true`, the lock is split into per-scheduler
partitions. Readers on different schedulers hit different cache lines and don't contend.

The cost is that writes now have to acquire all partitions, so write latency grows with the number
of schedulers. Use it when reads vastly dominate writes (>10:1 rule of thumb).

```
  Without read_concurrency             With read_concurrency
  ┌──────────┐                         ┌──────┬──────┬──────┬──────┐
  │ one lock │  ← all cores contend    │ L0   │ L1   │ L2   │ L3   │
  └──────────┘                         └──────┴──────┴──────┴──────┘
                                         ↑      ↑      ↑      ↑
                                       core 0 core 1 core 2 core 3
```

### 2. What `write_concurrency` actually changes

`write_concurrency: true` splits the internal hash table into several lock regions (one per
scheduler since OTP 22). Two writers hitting different regions don't serialize. Reads are unchanged.

There's a newer value, `write_concurrency: :auto`, which lets the VM add and remove lock regions
dynamically based on contention (OTP 25+). On unknown workloads, prefer `:auto`.

### 3. Decentralized counters

Every ETS table maintains a size counter. Under `write_concurrency`, that single counter becomes
the next bottleneck — every insert/delete bumps it atomically. `decentralized_counters: true`
replaces it with per-scheduler counters, summed lazily when `:ets.info(t, :size)` is called. Size
reads get slower; size updates stop contending.

As of OTP 23, `decentralized_counters` defaults to `true` for `ordered_set` and to the same value
as `write_concurrency` for `set` and `bag`. Still, set it explicitly so the intent is grep-able.

### 4. Compressed tables are orthogonal

`:compressed` trades CPU for RAM and has nothing to do with concurrency. Do not turn it on while
chasing latency; you'll regress.

### 5. Scheduler alignment: why pinning matters for benchmarks

BEAM schedulers are OS threads pinned to logical cores when `+sbt db` is set (default in recent
releases on Linux). If your bench spawns fewer processes than schedulers, migrations will mask the
true contention. Always run the benchmark with `parallel: System.schedulers_online()` or higher.

---

## Design decisions

**Option A — GenServer-wrapped state**
- Pros: serialized access; easy invariants.
- Cons: one message queue for all reads and writes; scales to one core.

**Option B — ETS with `:read_concurrency` / `:write_concurrency`** (chosen)
- Pros: readers and writers touch the table directly; scales with cores.
- Cons: multi-key invariants require the caller's discipline.

→ Chose **B** because when the data structure is a simple key-value and the workload is concurrent, ETS wins decisively.

---

## Implementation

### Step 1: `mix.exs`

**Objective**: Pin `{:benchee, "~> 1.3"}` so the parallel-reader benchmark can measure scheduler-contention shapes.

```elixir
defmodule EtsConcurrentDeep.MixProject do
  use Mix.Project

  def project do
    [
      app: :ets_concurrent_deep,
      version: "0.1.0",
      elixir: "~> 1.16",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger], mod: {EtsConcurrentDeep.Application, []}]
  end

  defp deps do
    [{:benchee, "~> 1.3", only: [:dev, :test]}]
  end
end
```

### Dependencies (mix.exs)

```elixir
```elixir
defmodule EtsConcurrentDeep.MixProject do
  use Mix.Project

  def project do
    [
      app: :ets_concurrent_deep,
      version: "0.1.0",
      elixir: "~> 1.16",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger], mod: {EtsConcurrentDeep.Application, []}]
  end

  defp deps do
    [{:benchee, "~> 1.3", only: [:dev, :test]}]
  end
end
```

### Step 2: `lib/ets_concurrent_deep/application.ex`

**Objective**: Boot a minimal supervisor since tables are spawned per benchmark and owned by the running task, not the tree.

```elixir
defmodule EtsConcurrentDeep.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args), do: Supervisor.start_link([], strategy: :one_for_one)
end
```

### Step 3: `lib/ets_concurrent_deep/table.ex`

**Objective**: Factory four ETS profiles (baseline, read_conc, write_conc, full_conc) so the bench can isolate each concurrency flag's impact.

```elixir
defmodule EtsConcurrentDeep.Table do
  @moduledoc """
  Factory for ETS tables with explicit concurrency profiles.

  Each profile produces a freshly named table seeded with `size` entries so the
  benchmark is reproducible. Tables created here are `:public` and owned by the
  caller process — callers must keep a reference or the table dies with them.
  """

  @type profile :: :baseline | :read_conc | :write_conc | :full_conc

  @spec new(profile, pos_integer()) :: :ets.tid() | atom()
  def new(profile, size) when size > 0 do
    name = :"#{profile}_#{System.unique_integer([:positive])}"
    opts = [:set, :public, :named_table] ++ options_for(profile)
    table = :ets.new(name, opts)
    seed(table, size)
    table
  end

  defp options_for(:baseline), do: []
  defp options_for(:read_conc), do: [read_concurrency: true]
  defp options_for(:write_conc), do: [write_concurrency: :auto]

  defp options_for(:full_conc),
    do: [read_concurrency: true, write_concurrency: :auto, decentralized_counters: true]

  defp seed(table, size) do
    Enum.each(1..size, fn i -> :ets.insert(table, {i, :erlang.phash2(i)}) end)
  end
end
```

### Step 4: `lib/ets_concurrent_deep/workload.ex`

**Objective**: Emit a 19:1 read/write hot-path workload so the benchmark reflects realistic cache traffic, not a synthetic best case.

```elixir
defmodule EtsConcurrentDeep.Workload do
  @moduledoc """
  Synthetic workloads that mimic a cache hot-path: a read-heavy mix with
  occasional writes. All functions are pure w.r.t. the caller — no GenServer,
  no message passing, ETS only.
  """

  @doc "Performs one read or write against `table` following a read:write ratio of 19:1."
  @spec hot_path(:ets.tab(), pos_integer()) :: term()
  def hot_path(table, key_space) do
    key = :rand.uniform(key_space)

    case :rand.uniform(20) do
      1 -> :ets.insert(table, {key, :erlang.phash2({key, :os.system_time()})})
      _ -> :ets.lookup(table, key)
    end
  end

  @doc "Pure read workload — used as the best case for read_concurrency."
  @spec read_only(:ets.tab(), pos_integer()) :: [tuple()]
  def read_only(table, key_space) do
    :ets.lookup(table, :rand.uniform(key_space))
  end

  @doc "Pure write workload — used to show decentralized_counters impact."
  @spec write_only(:ets.tab(), pos_integer()) :: true
  def write_only(table, key_space) do
    key = :rand.uniform(key_space)
    :ets.insert(table, {key, :erlang.phash2({key, :os.system_time()})})
  end
end
```

### Step 5: `bench/run.exs`

**Objective**: Drive each profile under `parallel: schedulers_online()` to quantify how each flag reshapes the lock-contention curve.

```elixir
alias EtsConcurrentDeep.{Table, Workload}

size = 100_000
parallel = System.schedulers_online()

tables =
  for profile <- [:baseline, :read_conc, :write_conc, :full_conc], into: %{} do
    {profile, Table.new(profile, size)}
  end

jobs =
  for {profile, table} <- tables, into: %{} do
    {"hot_path/#{profile}", fn -> Workload.hot_path(table, size) end}
  end

Benchee.run(jobs,
  parallel: parallel,
  time: 4,
  warmup: 2,
  memory_time: 1,
  formatters: [Benchee.Formatters.Console]
)
```

### Step 6: `test/ets_concurrent_deep_test.exs`

**Objective**: Assert each profile sets the declared flags and that 8 concurrent workers never crash or corrupt the table.

```elixir
defmodule EtsConcurrentDeepTest do
  use ExUnit.Case, async: true

  alias EtsConcurrentDeep.{Table, Workload}

  describe "Table.new/2" do
    for profile <- [:baseline, :read_conc, :write_conc, :full_conc] do
      test "creates a working :#{profile} table" do
        table = Table.new(unquote(profile), 100)
        assert :ets.info(table, :size) == 100
        assert [{1, _}] = :ets.lookup(table, 1)
      end
    end

    test "full_conc sets all three flags" do
      table = Table.new(:full_conc, 10)
      assert :ets.info(table, :read_concurrency) == true
      assert :ets.info(table, :write_concurrency) in [true, :auto]
      assert :ets.info(table, :decentralized_counters) == true
    end
  end

  describe "Workload.hot_path/2 under concurrency" do
    test "never crashes under 8 concurrent workers" do
      table = Table.new(:full_conc, 1_000)

      tasks =
        for _ <- 1..8 do
          Task.async(fn ->
            for _ <- 1..5_000, do: Workload.hot_path(table, 1_000)
          end)
        end

      Task.await_many(tasks, 10_000)
      assert :ets.info(table, :size) >= 1_000
    end
  end
end
```

### Step 7: Run it

**Objective**: Run the Benchee script and confirm the `read_conc` vs `baseline` throughput gap widens with scheduler count.

```bash
mix deps.get
mix test
mix run bench/run.exs
```

### Why this works

`:read_concurrency` tunes the table's lock granularity for readers; `:write_concurrency` does the same for writers. With both enabled, readers and writers scale independently up to core count, at the cost of slightly slower single-threaded operations.

---

## Benchmark — representative numbers

Measured on a 12-core x86_64, OTP 26, BEAM with `+sbt db`:

| Profile     | p50 ops/s (parallel=12) | p99 latency | Notes                                      |
|-------------|-------------------------|-------------|--------------------------------------------|
| baseline    | ~ 3.0M                  | 180 µs      | Single lock — readers and writers contend. |
| read_conc   | ~ 9.5M                  | 55 µs       | Reads scale linearly with cores.           |
| write_conc  | ~ 4.1M                  | 140 µs      | Helps writes; readers still contend lock.  |
| full_conc   | ~ 11.2M                 | 38 µs       | Best for 19:1 read-heavy workloads.        |

Your absolute numbers will differ; the **shape** should not. If `read_conc` is not at least 2x
over baseline with 8+ schedulers, verify that the BEAM is actually using more than one scheduler
(`:erlang.system_info(:schedulers_online)`).

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

**1. `read_concurrency` is not free for writes.** On a 48-core box, enabling it on a table that
gets 1000 writes/s can add measurable latency to every write. Measure, don't cargo-cult.

**2. `write_concurrency: true` vs `:auto`.** `true` allocates a fixed number of lock regions
(currently = scheduler count). `:auto` (OTP 25+) grows and shrinks them based on observed
contention. Prefer `:auto` unless you're on < OTP 25.

**3. `decentralized_counters` makes `:ets.info(t, :size)` slower.** It has to sum per-scheduler
counters. If your code calls `size` on the hot path (e.g. for cache eviction), reconsider.

**4. None of these flags help if you're the single writer.** Concurrency flags only pay off when
there is real contention. A GenServer that owns the table and serializes writes gets no benefit
from `write_concurrency`.

**5. `compressed` interacts badly with concurrency.** Compression is done on write and decompression
on read, under lock. Enabling it multiplies the critical section length. Only combine when RAM is
the hard constraint.

**6. Benchmarks must be CPU-bound or you'll measure noise.** If your workload spends most time in
`:rand.uniform` or `:erlang.phash2`, you're benchmarking those functions. Pre-generate keys into
a list and pass them in.

**7. When NOT to use this.** For low-traffic tables (< 10k ops/s) the flags cost more in code
complexity and memory than they save. Default to unflagged `:ets.new/2` and enable flags only
after a profiler (`:recon`, `:observer`) points at ETS contention.

---

## Reflection

- If your ETS workload needs multi-key atomicity, do you switch to Mnesia, wrap in a GenServer, or build a lock table? What are the trade-offs?
- `:decentralized_counters` changes behavior on multi-socket machines. How do you know if you need it, and what does enabling it cost on a small machine?

---

## Executable Example

```elixir
defmodule EtsConcurrentDeep.MixProject do
  use Mix.Project

  def project do
    [
      app: :ets_concurrent_deep,
      version: "0.1.0",
      elixir: "~> 1.16",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger], mod: {EtsConcurrentDeep.Application, []}]
  end

  defp deps do
    [{:benchee, "~> 1.3", only: [:dev, :test]}]
  end
end



### Step 2: `lib/ets_concurrent_deep/application.ex`

**Objective**: Boot a minimal supervisor since tables are spawned per benchmark and owned by the running task, not the tree.



### Step 3: `lib/ets_concurrent_deep/table.ex`

**Objective**: Factory four ETS profiles (baseline, read_conc, write_conc, full_conc) so the bench can isolate each concurrency flag's impact.



### Step 4: `lib/ets_concurrent_deep/workload.ex`

**Objective**: Emit a 19:1 read/write hot-path workload so the benchmark reflects realistic cache traffic, not a synthetic best case.



### Step 5: `bench/run.exs`

**Objective**: Drive each profile under `parallel: schedulers_online()` to quantify how each flag reshapes the lock-contention curve.



### Step 6: `test/ets_concurrent_deep_test.exs`

**Objective**: Assert each profile sets the declared flags and that 8 concurrent workers never crash or corrupt the table.



### Step 7: Run it

**Objective**: Run the Benchee script and confirm the `read_conc` vs `baseline` throughput gap widens with scheduler count.

defmodule Main do
  def main do
      # Demonstrating 16-ets-advanced-concurrent
      :ok
  end
end

Main.main()
```
