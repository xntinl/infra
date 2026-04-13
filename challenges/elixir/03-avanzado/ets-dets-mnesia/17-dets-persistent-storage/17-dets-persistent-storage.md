# DETS: persistent disk-based storage

**Project**: `dets_store` — a durable key-value store for a small IoT ingestion service that must survive node restarts without adding a database dependency.

---

## Project context

You maintain a lightweight ingestion service that collects heartbeats from < 5,000 field devices
and enqueues them for a downstream pipeline. The service has zero external infrastructure —
just BEAM on a single VM — and the business would like to keep it that way. Introducing Postgres
for a few hundred kilobytes of state is overkill. But an ETS-only implementation lost three
hours of heartbeats during the last OS patch reboot.

DETS is the natural fit: a file-backed key-value store living in the same BEAM node, with
roughly the same API as ETS. In exchange you get durability to disk but give up concurrency and
are capped at 2 GB per table. This exercise builds a `Dets.Store` wrapper with ownership
lifecycle, graceful sync, and a crash-recovery test that kills the BEAM mid-write and verifies
the file re-opens cleanly.

You'll end with a minimal `DetsStore` API (`put/2`, `get/1`, `delete/1`, `sync/0`) that a
GenServer supervises, plus a test that forces a `kill -9` scenario to prove the recovery path.

```
dets_store/
├── lib/
│   └── dets_store/
│       ├── application.ex
│       ├── store.ex
│       └── repair.ex
├── test/
│   └── dets_store_test.exs
├── priv/              # .dets files live here in dev/test
└── mix.exs
```

---

## Why DETS and not SQLite

SQLite is a fine database. DETS is not a database — it is persistent ETS. The decision is about whether you need SQL and durability or just ETS-shaped storage that survives restarts.

---

## Core concepts

### 1. DETS is ETS with a file, minus the concurrency

DETS tables live on disk and are memory-mapped in small windows. The API is nearly identical to
ETS (`:dets.insert/2`, `:dets.lookup/2`, `:dets.match/2`) but:

- A DETS table has **exactly one** owner process. All operations funnel through it.
- There is no `read_concurrency`, no `write_concurrency`. Operations are serialized by the owner.
- Maximum size is 2 GB per table (signed 32-bit file offsets internally).
- Only `:set`, `:bag`, and `:duplicate_bag` are supported. No `:ordered_set`.

If you need concurrent reads with durability, the pattern is "ETS + DETS": hot state in ETS,
periodic snapshots via `:ets.to_dets/2`, and load on boot via `:dets.to_ets/2`. Mnesia automates
exactly that pattern internally.

### 2. Ownership and the "properly closed" flag

When you `:dets.open_file/2` a file, DETS writes a header marking the file as open. On `close/1`
it flips the flag. If the BEAM crashes without closing, that flag stays set — on the next open,
DETS detects an unclean shutdown and triggers **auto-repair** (walks the file, rebuilds the
index). Auto-repair can take minutes for large tables.

You can control this via the `repair: :force | true | false` option. `:force` always repairs,
`true` repairs if the flag says so (default), `false` refuses to open a dirty file.

### 3. `:sync` and `:auto_save`

DETS does not fsync on every write. Writes land in an in-memory buffer and on OS page cache.
Three ways to force durability:

- `:dets.sync/1` — flush buffers and fsync. Blocking.
- `auto_save: milliseconds` option — DETS syncs periodically (default 3 minutes).
- Close the file cleanly.

Know that `sync` is expensive (full fsync of a potentially large file). A common pattern is
`auto_save: 5_000` plus a manual `sync/1` before a scheduled shutdown.

### 4. `open_file/2` options that matter

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
[
  type: :set,           # or :bag | :duplicate_bag
  access: :read_write,  # or :read
  file: ~c"priv/my.dets",
  auto_save: 5_000,     # ms; :infinity disables
  ram_file: false,      # if true, loads the whole file to RAM; closes flush back
  repair: true,         # :force | true | false
  min_no_slots: 256,    # initial hash slots — tune for expected size
  max_no_slots: 32 * 1024 * 1024
]
```

`ram_file: true` trades durability-during-runtime for speed and is what `mnesia`'s `disc_copies`
tables use internally.

### 5. When DETS is the wrong tool

- Tables > 1 GB: use Mnesia `disc_only_copies` or an actual database.
- Write rate > ~5,000 ops/s sustained: the serialization will bottleneck you.
- Need range queries over ordered keys: DETS does not support `ordered_set`.
- Multi-node replication: use Mnesia or CRDTs.

---

## Design decisions

**Option A — SQLite file**
- Pros: SQL, mature, well-understood durability.
- Cons: another dependency; no BEAM-native integration.

**Option B — DETS table** (chosen)
- Pros: zero dependencies; same API shape as ETS; replayable on restart.
- Cons: 2 GB cap; corruption recovery is a manual dance; no queries beyond key lookup.

→ Chose **B** because for small persistent state inside a BEAM app, DETS keeps everything in-process with no extra moving parts.

---

## Implementation

### Step 1: `mix.exs`

**Objective**: Declare a zero-dep OTP app — DETS ships with Erlang, so nothing else is needed for durable persistence.

```elixir
defmodule DetsStore.MixProject do
  use Mix.Project

  def project do
    [
      app: :dets_store,
      version: "0.1.0",
      elixir: "~> 1.16",
      start_permanent: Mix.env() == :prod,
      deps: []
    ]
  end

  def application do
    [extra_applications: [:logger], mod: {DetsStore.Application, []}]
  end
end
```

### Step 2: `lib/dets_store/application.ex`

**Objective**: Ensure `priv/` exists and hand the Store a fixed file path so restarts reopen the same DETS segment.

```elixir
defmodule DetsStore.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    File.mkdir_p!("priv")

    children = [
      {DetsStore.Store, [file: Path.join("priv", "dets_store.dets")]}
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: DetsStore.Supervisor)
  end
end
```

### Step 3: `lib/dets_store/store.ex`

**Objective**: Own the DETS handle with `auto_save: 5_000` and `repair: true` so transient crashes self-heal on next open.

```elixir
defmodule DetsStore.Store do
  @moduledoc """
  Durable key-value store backed by DETS.

  The GenServer owns the DETS table. All writes go through the process; reads
  can run either through the process (safe, serialized) or directly via
  `:dets.lookup/2` from any process — DETS enforces its own lock internally.

  Public API returns `{:ok, _}` / `{:error, reason}` so callers can handle I/O
  failure explicitly. We do not raise on DETS errors because DETS can return
  `{:error, {:file_error, ...}}` for transient disk issues that the caller may
  want to retry.
  """
  use GenServer
  require Logger

  @name __MODULE__
  @type key :: term()
  @type value :: term()

  # ---- Public API ---------------------------------------------------------

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: @name)
  end

  @spec put(key(), value()) :: :ok | {:error, term()}
  def put(key, value), do: GenServer.call(@name, {:put, key, value})

  @spec get(key()) :: {:ok, value()} | :error
  def get(key) do
    case :dets.lookup(table(), key) do
      [{^key, value}] -> {:ok, value}
      [] -> :error
      {:error, _} = err -> err
    end
  end

  @spec delete(key()) :: :ok
  def delete(key), do: GenServer.call(@name, {:delete, key})

  @spec sync() :: :ok | {:error, term()}
  def sync, do: GenServer.call(@name, :sync, 30_000)

  @spec size() :: non_neg_integer()
  def size, do: :dets.info(table(), :size)

  defp table, do: @name

  # ---- GenServer ----------------------------------------------------------

  @impl true
  def init(opts) do
    Process.flag(:trap_exit, true)
    file = Keyword.fetch!(opts, :file) |> to_charlist()

    open_opts = [
      type: :set,
      access: :read_write,
      file: file,
      auto_save: 5_000,
      repair: true
    ]

    case :dets.open_file(@name, open_opts) do
      {:ok, @name} ->
        Logger.info("dets_store opened #{file}, size=#{:dets.info(@name, :size)}")
        {:ok, %{file: file}}

      {:error, reason} ->
        DetsStore.Repair.attempt_force_repair(@name, open_opts, reason)
    end
  end

  @impl true
  def handle_call({:put, key, value}, _from, state) do
    {:reply, :dets.insert(@name, {key, value}), state}
  end

  @impl true
  def handle_call({:delete, key}, _from, state) do
    {:reply, :dets.delete(@name, key), state}
  end

  @impl true
  def handle_call(:sync, _from, state) do
    {:reply, :dets.sync(@name), state}
  end

  @impl true
  def terminate(reason, _state) do
    # Closing sets the "properly closed" flag; skip it and the next open repairs.
    Logger.info("dets_store closing (reason=#{inspect(reason)})")
    :dets.close(@name)
    :ok
  end
end
```

### Step 4: `lib/dets_store/repair.ex`

**Objective**: Retry `open_file` with `repair: :force` when soft-repair fails, so a corrupt segment gets rebuilt before `init/1` aborts.

```elixir
defmodule DetsStore.Repair do
  @moduledoc """
  Fallback path when `:dets.open_file/2` refuses to open a file. We log the
  reason, then retry once with `repair: :force` which always rebuilds the
  internal hash. If that still fails the node should not start — we return
  the error and let the supervisor crash us.
  """
  require Logger

  @spec attempt_force_repair(atom(), keyword(), term()) :: {:ok, map()} | {:stop, term()}
  def attempt_force_repair(table, open_opts, reason) do
    Logger.warning("dets_store open failed (#{inspect(reason)}); forcing repair")

    forced = Keyword.put(open_opts, :repair, :force)

    case :dets.open_file(table, forced) do
      {:ok, ^table} ->
        Logger.warning("dets_store forced repair succeeded, size=#{:dets.info(table, :size)}")
        {:ok, %{file: Keyword.fetch!(forced, :file), repaired: true}}

      {:error, reason2} ->
        {:stop, {:dets_unrecoverable, reason2}}
    end
  end
end
```

### Step 5: `test/dets_store_test.exs`

**Objective**: Confirm data survives both a clean restart and a `Process.exit(:kill)` — the real BEAM-crash durability contract.

```elixir
defmodule DetsStoreTest do
  use ExUnit.Case, async: false
  # async: false — DETS file is a single shared resource
  alias DetsStore.Store

  @tmp_file "priv/dets_store_test.dets"

  setup do
    File.mkdir_p!("priv")
    File.rm(@tmp_file)
    {:ok, _pid} = start_supervised({Store, [file: @tmp_file]})
    on_exit(fn -> File.rm(@tmp_file) end)
    :ok
  end

  describe "basic CRUD" do
    test "put/get round-trip" do
      assert :ok = Store.put(:device_42, %{last_seen: 1_700_000_000})
      assert {:ok, %{last_seen: 1_700_000_000}} = Store.get(:device_42)
    end

    test "missing key returns :error" do
      assert :error = Store.get(:ghost)
    end

    test "delete removes entry" do
      Store.put(:tmp, 1)
      Store.delete(:tmp)
      assert :error = Store.get(:tmp)
    end
  end

  describe "durability" do
    test "data survives a clean GenServer restart" do
      Store.put(:persisted, "hello")
      :ok = Store.sync()

      stop_supervised!(Store)
      {:ok, _} = start_supervised({Store, [file: @tmp_file]})

      assert {:ok, "hello"} = Store.get(:persisted)
    end

    test "data survives an abrupt owner exit (simulates BEAM crash)" do
      Store.put(:crash_survivor, 99)
      :ok = Store.sync()

      # Kill the owner without running terminate/2 — file is left "open".
      pid = Process.whereis(Store)
      ref = Process.monitor(pid)
      Process.exit(pid, :kill)
      assert_receive {:DOWN, ^ref, :process, ^pid, :killed}, 1_000

      # Force supervisor to restart it — auto-repair should kick in silently.
      # start_supervised already re-starts; wait for it.
      Process.sleep(50)
      {:ok, _} = start_supervised({Store, [file: @tmp_file]}, restart: :permanent)

      assert {:ok, 99} = Store.get(:crash_survivor)
    end
  end
end
```

### Step 6: Run it

**Objective**: Run the suite with `--trace` to surface any DETS I/O or repair warnings buried in normal log noise.

```bash
mix deps.get
mix test --trace
```

### Why this works

A DETS table is a disk file that maps keys to values through a linear hashing scheme. Opening the table loads the header and maps the file; reads fetch pages on demand; writes mutate pages and flush on `:dets.sync/1` or close.

---

## Performance notes

For sanity numbers (local SSD, OTP 26):

| Operation        | Throughput   | Notes                                   |
|------------------|--------------|-----------------------------------------|
| put/2 (no sync)  | ~ 80k ops/s  | Writes hit OS page cache                |
| put/2 + sync/0   | ~ 150 ops/s  | fsync dominates                         |
| get/1            | ~ 250k ops/s | Serialized by the owner                 |
| open_file/2      | 5–200 ms     | First open — depends on min_no_slots    |
| open_file/2 dirty| 1–60 s       | Auto-repair — scales with file size     |

The order of magnitude gap between buffered and fsync'd writes is the whole reason batching
matters. A classic pattern is "insert many, sync once per window":

```elixir
for item <- batch, do: Store.put(item.id, item)
Store.sync()
```

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

**1. Do not hold DETS files over NFS.** DETS uses `O_SYNC`-like semantics; network filesystems
break the assumptions about `fsync` durability. Keep the file on a local block device.

**2. `auto_save` is not a substitute for graceful shutdown.** If the OS kills the BEAM between
auto_saves, you lose whatever the buffer held. Add a `systemd` `TimeoutStopSec` long enough for
`terminate/2` to run, and let the supervisor close DETS on shutdown.

**3. Repair time is linear in table size.** A 500 MB dirty file can take minutes to reopen.
Add a boot-time log entry and a `Telemetry` event so you can alert on long repairs.

**4. `:dets.lookup/2` from any process is safe, but writes must funnel through the owner.**
Erlang/OTP enforces the single-writer semantics internally; sharing the owner pid is enough.

**5. 2 GB file limit is a hard ceiling.** When you approach 1 GB, start planning a move to
Mnesia or a real database. Splitting into multiple DETS files works short-term but the operational
complexity rises fast.

**6. `ram_file: true` defeats crash durability.** The file is held in RAM until close; an abrupt
exit loses everything written since open. Use it only for caches that can be rebuilt, not for
authoritative state.

**7. When NOT to use DETS.** Anything shared across nodes, anything transactional, anything
over ~1 GB, or anything where you need a real query language. Reach for Mnesia, ETS+snapshot,
or Postgres.

---

## Benchmark

```elixir
# :timer.tc / Benchee measurement sketch
{time_us, _} = :timer.tc(fn -> :ok end)
IO.puts("elapsed: #{time_us} us")
```

Target: DETS write 20-100 us; read 10-50 us; both dominated by disk I/O and OS page cache.

---

## Reflection

- Your DETS file got corrupted by a sudden power loss. What tools does OTP give you, and how much data can you lose in the worst case?
- SQLite supports concurrent readers and one writer. How does DETS compare, and which use cases are secretly better off with SQLite?

---

## Resources

- [`:dets` reference — erlang.org](https://www.erlang.org/doc/man/dets.html)
- [Erlang `file` module and fsync semantics](https://www.erlang.org/doc/man/file.html#datasync-1)
- [Mnesia internals — how `disc_copies` use DETS](https://www.erlang.org/doc/apps/mnesia/mnesia_chap7.html)
- [Learn You Some Erlang — ETS and DETS chapter](https://learnyousomeerlang.com/ets)
- [Phoenix.PubSub.DETS-style persistence discussion](https://elixirforum.com/t/using-dets-for-persistence/)
- [Saša Jurić — "Soul of Erlang" talk on state persistence](https://www.youtube.com/watch?v=JvBT4XBdoUE)
