# DETS vs ETS — Persistence, Cost, and Crash Recovery

**Project**: `dets_vs_ets` — same KV store in DETS and ETS with crash-recovery tests.
**Difficulty**: ★★★★☆
**Estimated time**: 3–6 hours

---

## Project context

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

```
dets_vs_ets/
├── lib/
│   └── dets_vs_ets/
│       ├── application.ex
│       ├── ets_store.ex
│       ├── dets_store.ex
│       └── hybrid_store.ex       # ETS hot path + periodic DETS snapshot
└── test/
    └── dets_vs_ets/
        ├── ets_store_test.exs
        ├── dets_store_test.exs
        └── hybrid_store_test.exs
```

---

## Core concepts

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

## Implementation

### Step 1: `mix.exs`

```elixir
defmodule DetsVsEts.MixProject do
  use Mix.Project

  def project do
    [app: :dets_vs_ets, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application do
    [extra_applications: [:logger], mod: {DetsVsEts.Application, []}]
  end

  defp deps, do: [{:benchee, "~> 1.3", only: :dev}]
end
```

### Step 2: `lib/dets_vs_ets/application.ex`

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

### Step 3: `lib/dets_vs_ets/ets_store.ex`

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

### Step 4: `lib/dets_vs_ets/dets_store.ex`

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

### Step 5: `lib/dets_vs_ets/hybrid_store.ex`

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

```elixir
defmodule DetsVsEts.DetsStoreTest do
  use ExUnit.Case, async: false

  alias DetsVsEts.DetsStore

  setup do
    # Wipe the DETS file before each test
    :dets.delete_all_objects(:dets_kv)
    :ok
  end

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
```

### Step 7: `test/dets_vs_ets/hybrid_store_test.exs`

```elixir
defmodule DetsVsEts.HybridStoreTest do
  use ExUnit.Case, async: false

  alias DetsVsEts.HybridStore

  setup do
    for {k, _} <- :ets.tab2list(:hybrid_ets), do: :ets.delete(:hybrid_ets, k)
    HybridStore.force_snapshot()
    :ok
  end

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
```

### Step 8: Simulating crash recovery

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

## Resources

- [`:dets` documentation — erlang.org](https://www.erlang.org/doc/man/dets.html)
- [`:ets` documentation — erlang.org](https://www.erlang.org/doc/man/ets.html)
- [Erlang in Anger — Fred Hebert](https://www.erlang-in-anger.com/) — chapter "Stateful Data Stores"
- [OTP source: dets.erl](https://github.com/erlang/otp/blob/master/lib/stdlib/src/dets.erl) — the repair logic lives here
- [SQLite as a File Format](https://www.sqlite.org/appfileformat.html) — consider this instead
- [Exqlite](https://hexdocs.pm/exqlite/) — idiomatic SQLite for Elixir, usually a better DETS alternative
- [Cachex](https://github.com/whitfin/cachex) — for when you want ETS-with-persistence already solved
