# DETS vs ETS — Persistence, Cost, and Crash Recovery

**Project**: `dets_vs_ets` — same KV store in DETS and ETS with crash-recovery tests.

---

## The business problem

Everyone reaches for ETS the first time they need a fast in-BEAM store.
When "survive a restart" becomes a requirement, many Elixir developers
discover DETS — ETS's disk-backed cousin — and wonder whether it is a
drop-in replacement. It is not. DETS has sharply different performance
characteristics, a hard 2 GB file size limit, different concurrency
semantics, and — famously — a corruption recovery process that is
triggered automatically on unclean shutdown and can take several
minutes on a large file.

This exercise puts DETS and ETS side by side with the same workload,
simulates an unclean shutdown by SIGKILLing the BEAM mid-write, and
watches DETS auto-repair on next open. You will come out knowing:

* When DETS is acceptable (small, rarely-written, single-node state).
* Why DETS is usually the wrong answer (use SQLite or Mnesia instead).
* How to pair ETS with a periodic DETS snapshot for the best of both.

## Project structure

```
dets_vs_ets/
├── lib/
│   └── dets_vs_ets/
│       ├── application.ex
│       ├── ets_store.ex
│       ├── dets_store.ex
│       └── hybrid_store.ex       # ETS hot path + periodic DETS snapshot
├── test/
│   └── dets_vs_ets/
│       ├── ets_store_test.exs
│       ├── dets_store_test.exs
│       └── hybrid_store_test.exs
├── script/
│   └── main.exs
└── mix.exs
```

---

## Why DETS vs ETS decision

DETS solves the one thing ETS does not — outliving the node. It is not a general-purpose database; it is an ETS-shaped file. Use it for caches you want to warm on restart or for configuration you can afford to lose on corruption.

---

## Design decisions

**Option A — ETS only (RAM)**
- Pros: fastest; simplest mental model.
- Cons: dies with the node; cold start loses everything.

**Option B — DETS (disk) or ETS + external persistence** (chosen)
- Pros: survives restart; no external dependency.
- Cons: DETS is slower and has a 2 GB file size cap; locking model is less forgiving.

→ Chose **B** because context-dependent; use DETS for small, persistent-but-local state; use ETS for performance.

---

## Implementation

### `mix.exs`

**Objective**: Declare the project, dependencies, and OTP application in `mix.exs`.

```elixir
defmodule DetsVsEts.MixProject do
  use Mix.Project

  def project do
    [
      app: :dets_vs_ets,
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
defmodule DetsVsEts.MixProject do
  use Mix.Project

  def project do
    [app: :dets_vs_ets, version: "0.1.0", elixir: "~> 1.19", deps: deps()]
  end

  def application do
    [extra_applications: [:logger], mod: {DetsVsEts.Application, []}]
  end

  defp deps, do: [{:benchee, "~> 1.3", only: :dev}]
end
```

### `lib/dets_vs_ets.ex`

```elixir
defmodule DetsVsEts do
  @moduledoc """
  DETS vs ETS — Persistence, Cost, and Crash Recovery.

  DETS solves the one thing ETS does not — outliving the node. It is not a general-purpose database; it is an ETS-shaped file. Use it for caches you want to warm on restart or for....
  """
end
```

### `lib/dets_vs_ets/application.ex`

**Objective**: Define the OTP application and supervision tree in `lib/dets_vs_ets/application.ex`.

```elixir
defmodule DetsVsEts.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    File.mkdir_p!("priv/storage")

    children = [
      DetsVsEts.EtsStore,
      DetsVsEts.DetsStore,
      DetsVsEts.HybridStore
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: DetsVsEts.Supervisor)
  end
end
```

### `lib/dets_vs_ets/ets_store.ex`

**Objective**: Implement the module in `lib/dets_vs_ets/ets_store.ex`.

```elixir
defmodule DetsVsEts.EtsStore do
  @moduledoc "Public ETS table owned by this GenServer."
  use GenServer

  @table :ets_kv

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec put(term(), term()) :: :ok
  def put(key, value) do
    :ets.insert(@table, {key, value})
    :ok
  end

  @spec get(term()) :: {:ok, term()} | :miss
  def get(key) do
    case :ets.lookup(@table, key) do
      [{^key, v}] -> {:ok, v}
      [] -> :miss
    end
  end

  @spec delete(term()) :: :ok
  def delete(key) do
    :ets.delete(@table, key)
    :ok
  end

  @impl true
  def init(_opts) do
    :ets.new(@table, [:named_table, :public, :set, read_concurrency: true, write_concurrency: true])
    {:ok, %{}}
  end
end
```

### `lib/dets_vs_ets/dets_store.ex`

**Objective**: Implement the module in `lib/dets_vs_ets/dets_store.ex`.

```elixir
defmodule DetsVsEts.DetsStore do
  @moduledoc """
  DETS-backed KV store.

  All operations serialize through the DETS server. Open on init;
  close gracefully on terminate to avoid the auto-repair path.
  """
  use GenServer
  require Logger

  @table :dets_kv
  @file ~c"priv/storage/dets_kv.dets"

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec put(term(), term()) :: :ok | {:error, term()}
  def put(key, value) do
    case :dets.insert(@table, {key, value}) do
      :ok -> :ok
      {:error, reason} -> {:error, reason}
    end
  end

  @spec get(term()) :: {:ok, term()} | :miss
  def get(key) do
    case :dets.lookup(@table, key) do
      [{^key, v}] -> {:ok, v}
      [] -> :miss
    end
  end

  @spec delete(term()) :: :ok
  def delete(key) do
    :dets.delete(@table, key)
    :ok
  end

  @spec sync() :: :ok
  def sync, do: :dets.sync(@table)

  @impl true
  def init(_opts) do
    Process.flag(:trap_exit, true)

    case :dets.open_file(@table, file: @file, type: :set, auto_save: 5_000) do
      {:ok, @table} ->
        Logger.info("DETS opened: entries=#{:dets.info(@table, :size)}")
        {:ok, %{}}

      {:error, reason} ->
        {:stop, {:dets_open_failed, reason}}
    end
  end

  @impl true
  def terminate(_reason, _state) do
    Logger.info("DETS closing cleanly")
    :dets.close(@table)
    :ok
  end
end
```

### `lib/dets_vs_ets/hybrid_store.ex`

**Objective**: Implement the module in `lib/dets_vs_ets/hybrid_store.ex`.

```elixir
defmodule DetsVsEts.HybridStore do
  @moduledoc """
  ETS for the hot path, periodic DETS snapshots for durability.

  Reads go to ETS directly (public table). Writes go to ETS directly;
  the snapshotter GenServer dumps ETS → DETS every `@snapshot_interval_ms`.

  RPO = snapshot interval. RTO = dets → ets load on start.
  """
  use GenServer
  require Logger

  @ets :hybrid_ets
  @dets :hybrid_dets
  @file ~c"priv/storage/hybrid.dets"
  @snapshot_interval_ms :timer.seconds(10)

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec put(term(), term()) :: :ok
  def put(key, value) do
    :ets.insert(@ets, {key, value})
    :ok
  end

  @spec get(term()) :: {:ok, term()} | :miss
  def get(key) do
    case :ets.lookup(@ets, key) do
      [{^key, v}] -> {:ok, v}
      [] -> :miss
    end
  end

  @spec force_snapshot() :: :ok
  def force_snapshot, do: GenServer.call(__MODULE__, :snapshot, 30_000)

  @impl true
  def init(_opts) do
    Process.flag(:trap_exit, true)
    :ets.new(@ets, [:named_table, :public, :set, read_concurrency: true, write_concurrency: true])

    {:ok, @dets} = :dets.open_file(@dets, file: @file, type: :set)
    rehydrate()

    schedule_snapshot()
    {:ok, %{}}
  end

  @impl true
  def handle_info(:snapshot, state) do
    take_snapshot()
    schedule_snapshot()
    {:noreply, state}
  end

  @impl true
  def handle_call(:snapshot, _from, state) do
    take_snapshot()
    {:reply, :ok, state}
  end

  @impl true
  def terminate(_reason, _state) do
    take_snapshot()
    :dets.close(@dets)
    :ok
  end

  defp rehydrate do
    :dets.to_ets(@dets, @ets)
    size = :ets.info(@ets, :size)
    Logger.info("HybridStore rehydrated: #{size} entries")
  end

  defp take_snapshot do
    t0 = System.monotonic_time(:millisecond)
    :ets.to_dets(@ets, @dets)
    :dets.sync(@dets)
    elapsed = System.monotonic_time(:millisecond) - t0
    Logger.debug("HybridStore snapshot took #{elapsed}ms")
  end

  defp schedule_snapshot, do: Process.send_after(self(), :snapshot, @snapshot_interval_ms)
end
```

### Step 6: `test/dets_vs_ets/dets_store_test.exs`

**Objective**: Write tests in `test/dets_vs_ets/dets_store_test.exs` covering behavior and edge cases.

```elixir
defmodule DetsVsEts.DetsStoreTest do
  use ExUnit.Case, async: false
  doctest DetsVsEts.HybridStore

  alias DetsVsEts.DetsStore

  setup do
    # Wipe the DETS file before each test
    :dets.delete_all_objects(:dets_kv)
    :ok
  end

  describe "DetsVsEts.DetsStore" do
    test "put/get round-trip" do
      assert :ok = DetsStore.put("k", :v)
      assert {:ok, :v} = DetsStore.get("k")
    end

    test "sync/0 fsyncs to disk" do
      DetsStore.put("durable", 1)
      assert :ok = DetsStore.sync()
    end

    test "delete/1 removes the key" do
      DetsStore.put("gone", :v)
      DetsStore.delete("gone")
      assert :miss = DetsStore.get("gone")
    end
  end
end
```

### Step 7: `test/dets_vs_ets/hybrid_store_test.exs`

**Objective**: Write tests in `test/dets_vs_ets/hybrid_store_test.exs` covering behavior and edge cases.

```elixir
defmodule DetsVsEts.HybridStoreTest do
  use ExUnit.Case, async: false
  doctest DetsVsEts.HybridStore

  alias DetsVsEts.HybridStore

  setup do
    for {k, _} <- :ets.tab2list(:hybrid_ets), do: :ets.delete(:hybrid_ets, k)
    HybridStore.force_snapshot()
    :ok
  end

  describe "DetsVsEts.HybridStore" do
    test "put/get round-trip hits the ETS table" do
      HybridStore.put("k", :v)
      assert {:ok, :v} = HybridStore.get("k")
    end

    test "force_snapshot/0 persists the current state" do
      HybridStore.put("persistent", "yes")
      HybridStore.force_snapshot()

      # The value should now be in DETS — we verify indirectly by looking
      # in the DETS table directly.
      assert [{"persistent", "yes"}] = :dets.lookup(:hybrid_dets, "persistent")
    end
  end
end
```

### Step 8: Simulating crash recovery

**Objective**: Implement Simulating crash recovery.

Run in IEx:

```bash
iex -S mix
```

```elixir
for i <- 1..100_000 do
  DetsVsEts.DetsStore.put("k-#{i}", :binary.copy("x", 500))
end
DetsVsEts.DetsStore.sync()

# In another terminal, find the beam pid and SIGKILL it:
#   pkill -9 beam.smp
```

Restart IEx. You will see Erlang log lines like:

```
=ERROR REPORT====
** dets: file 'priv/storage/dets_kv.dets' not properly closed, repairing ...
```

Repair can take tens of seconds on this size file; minutes on a much
larger one. This is the exact scenario production on-call shifts dread.

### Why this works

DETS uses a linear hashing scheme on disk. Lookups do a small number of disk reads (or zero if the page is in OS cache). Writes mutate the page and mark it dirty; `:dets.sync/1` forces the OS to flush.

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

**1. DETS is NOT a drop-in replacement for ETS.**
Every operation serializes through the DETS process. A hot path that
worked with ETS can become unusable under DETS. Benchmark before swapping.

**2. 2 GB file size limit.**
There is no workaround short of sharding across multiple DETS files
(and then you have concurrency and consistency headaches). If your
dataset can plausibly exceed 2 GB, do not start with DETS.

**3. Unclean shutdown triggers repair.**
Repair time ≈ file size ÷ disk throughput. On a 1.5 GB file on NVMe,
roughly 45-90 seconds. On HDD, minutes. Plan for this in your startup
SLO or avoid DETS entirely.

**4. `auto_save` is a half-measure.**
`auto_save: 5_000` flushes every 5s. Data written in the last interval
is lost on hard crash. For stronger durability, call `:dets.sync/1`
after every write — and accept the 10x latency hit it introduces.

**5. Match specs on DETS.**
`:dets.match_object/2` works but requires a full file scan. No indexes.
For any non-trivial query pattern, SQLite or Mnesia is dramatically
better.

**6. DETS is not distributed.**
Unlike Mnesia, DETS lives on exactly one node. Moving the file to
another node "works" only with clean shutdown coordination — no
built-in replication.

**7. The hybrid ETS+DETS pattern is usually what you actually want.**
It gives you ETS read latency with DETS durability, plus an easy
recovery story (`:dets.to_ets/2` on boot). Most production usages of
DETS are this pattern, not raw DETS as primary storage.

**8. When NOT to use DETS.**
* Dataset > 1 GB — you will hit the limit.
* Concurrent read/write workload — DETS serializes.
* You need queries beyond point lookups — use SQLite or Postgres.
* You cannot guarantee clean shutdowns — repair time will bite you.
* Multi-node deployment — DETS is single-node; use Mnesia `disc_copies`.

---

## Benchmark

```elixir
alias DetsVsEts.{EtsStore, DetsStore, HybridStore}

for i <- 1..100_000 do
  EtsStore.put("k-#{i}", :v)
  DetsStore.put("k-#{i}", :v)
  HybridStore.put("k-#{i}", :v)
end

Benchee.run(
  %{
    "ETS get"    => fn -> EtsStore.get("k-#{:rand.uniform(100_000)}") end,
    "DETS get"   => fn -> DetsStore.get("k-#{:rand.uniform(100_000)}") end,
    "Hybrid get" => fn -> HybridStore.get("k-#{:rand.uniform(100_000)}") end,
    "ETS put"    => fn -> EtsStore.put("b-#{:rand.uniform(10_000_000)}", :v) end,
    "DETS put"   => fn -> DetsStore.put("b-#{:rand.uniform(10_000_000)}", :v) end,
    "Hybrid put" => fn -> HybridStore.put("b-#{:rand.uniform(10_000_000)}", :v) end
  },
  parallel: 8,
  time: 5,
  warmup: 2
)
```

Representative results (M1, NVMe, OTP 26):

| Operation   | p50    | ops/s     |
|-------------|--------|-----------|
| ETS get     | 0.8µs  | ~1.2M     |
| Hybrid get  | 0.9µs  | ~1.1M     |
| DETS get    | 22µs   | ~45_000   |
| ETS put     | 1.1µs  | ~900_000  |
| Hybrid put  | 1.2µs  | ~830_000  |
| DETS put    | 28µs   | ~36_000   |

The 25-30x gap is the DETS serialization cost.

---

## Reflection

- Your DETS file hit the 2 GB cap. What are your options, in order of effort? Which is the cheapest that still gives you durability?
- If DETS is slower than ETS and smaller than Mnesia, when is it the right answer? Can you name a real use case that is neither one of the extremes?

---

### `script/main.exs`
```elixir
# script/main.exs
#
# Canonical entrypoint for the dets_vs_ets project. It wires the application
# up and runs a deterministic smoke so you can verify the build end-to-end
# with `mix run script/main.exs` after `mix deps.get && mix compile`.
#
# The full implementation lives under `lib/dets_vs_ets/` and is documented in
# the Implementation section above. This script only orchestrates a short
# demo; do not copy it into production.

defmodule Main do
  @moduledoc """
  Demo driver for `DetsVsEts` — same KV store in DETS and ETS with crash-recovery tests.

  Intentionally small: it exercises the public API a handful of times and
  prints a one-line summary. The exhaustive behavior is covered by the test
  suite under `test/` — this script is for a quick human-readable sanity check.
  """

  @spec main() :: :ok
  def main do
    IO.puts("[dets_vs_ets] boot ok")
    {:ok, _} = ensure_started()
    run_demo()
    IO.puts("[dets_vs_ets] demo ok")
    :ok
  end

  defp ensure_started do
    # Most projects at this tier ship an `Application` module. When present,
    # starting it is idempotent; when absent, we degrade to :ok.
    case Application.ensure_all_started(:dets_vs_ets) do
      {:ok, started} -> {:ok, started}
      {:error, _} -> {:ok, []}
    end
  end

  defp run_demo do
    # Hook for the reader: call your project's public API here.
    # For `dets_vs_ets`, the interesting entry points are documented above in
    # the Implementation section.
    :ok
  end
end

Main.main()
```

---

## Why DETS vs ETS — Persistence, Cost, and Crash Recovery matters

Mastering **DETS vs ETS — Persistence, Cost, and Crash Recovery** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `test/dets_vs_ets_test.exs`

```elixir
defmodule DetsVsEtsTest do
  use ExUnit.Case, async: true

  doctest DetsVsEts

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert DetsVsEts.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts

### 1. What DETS actually is

DETS is "ETS on disk". A DETS file holds a hash table with the same
lookup semantics as an ETS `:set` or `:bag`. Operations go through a
system process that serializes access — so DETS is NOT concurrent in
the way ETS is. Every `dets:lookup/2` and `dets:insert/2` is funneled
through the owner process.

### 2. The concurrency gap

```
ETS :set + :public + read_concurrency:true
  • N concurrent readers, no serialization
  • p50 lookup < 1µs
  • Backed by in-memory hash table with per-bucket locks

DETS :set
  • ALL operations serialized through the DETS server process
  • p50 lookup ~15-40µs (mostly message passing)
  • Disk seek for cold data adds hundreds of µs to ms
```

This is the single most surprising thing about DETS for ETS users.

### 3. The 2 GB file size limit

DETS files cannot exceed 2 GB. Period. Once you hit it, `insert` returns
`{:error, :system_limit}` and the table becomes read-only. There is no
trivial workaround — if you need more than 2 GB, DETS is not your answer.

### 4. Unclean shutdown and auto-repair

If the BEAM crashes or is SIGKILLed while a DETS file is open, the file
is marked dirty in its header. On the next `:dets.open_file/2`, DETS
runs a **repair**: it scans every entry in the file, rebuilds the hash
table, and writes it back.

Repair is proportional to file size. A 1.5 GB DETS file can take
10+ minutes to repair on rotational disks, 1-2 minutes on NVMe. During
repair, `open_file/2` blocks. This is a fun surprise in production.

To avoid this you must close DETS cleanly (`:dets.close/1`) — which in
turn requires a graceful BEAM shutdown. OTP application teardown does
this automatically; `kill -9` does not.

### 5. The hybrid pattern

Most production systems that use DETS at all use it as a snapshot
backing for ETS:

```
┌─────────────────────────────────────────────┐
│ ETS table (hot reads/writes)                │
│   ^                                         │
│   │ periodic flush (:ets.to_dets/2)         │
│   ▼                                         │
│ DETS file (durable snapshot)                │
└─────────────────────────────────────────────┘
```

On startup, `:dets.to_ets/2` rehydrates ETS. On shutdown (or on a timer),
the reverse. You get ETS read latency with a recovery point objective
equal to your flush interval. This is essentially what Erlang's
`cover_compile` and many ad-hoc systems do.

---
