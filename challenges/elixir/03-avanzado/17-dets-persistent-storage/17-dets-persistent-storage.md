# DETS: Persistent Storage

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`. The gateway now has in-memory rate limits and caches,
but every restart loses all configuration. The platform team needs route-level config
(custom rate limits, backend URLs, feature flags) to survive node restarts without
requiring an external database.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       ├── router.ex
│       ├── rate_limiter/
│       ├── metrics/
│       └── config/
│           ├── store.ex        # ← you implement this
│           └── persistent_cache.ex  # ← and this
├── test/
│   └── api_gateway/
│       └── config/
│           ├── store_test.exs
│           └── persistent_cache_test.exs
└── mix.exs
```

---

## The business problem

Two requirements:

1. **Persistent route config**: operators use a CLI to set per-route rate limits and backend
   URLs. Those settings must survive gateway restarts. The dataset is small (< 10k routes)
   and access is read-heavy. An external database is not justified.

2. **Warm cache across restarts**: the gateway caches upstream responses. On restart, the
   cache is cold and the first minutes have high miss rates spiking latency. A persistent
   L2 layer would preserve cache entries across restarts.

---

## Why DETS and not a file + JSON

Writing JSON to a file works for simple cases but requires:
- Manual locking to prevent concurrent write corruption
- Loading the entire file into memory to read one key
- Writing the entire file to update one key

DETS solves all three: it has a file-level lock, supports lookup by key without loading
the whole file, and updates are in-place. The API is nearly identical to ETS.

## Why DETS is not a general-purpose database

DETS has hard limits that make it unsuitable as a primary data store:

- **2 GB per file** — no workaround, it's a format constraint
- **No `:ordered_set` type** — no range queries
- **No concurrent writers** — file-level lock serializes all writes
- **`insert/2` does not guarantee durability** — data goes to an OS buffer first;
  only `sync/1` or the `auto_save` timer writes to disk. A crash between insert and
  sync loses the data.

For audit logs, config, and warm caches, DETS is the right tool. For anything
requiring transactions, ACID guarantees, or more than 2 GB, use Mnesia or PostgreSQL.

---

## Implementation

### Step 1: `lib/api_gateway/config/store.ex`

```elixir
defmodule ApiGateway.Config.Store do
  @moduledoc """
  Persistent key-value store for gateway route configuration.

  Backed by DETS for durability. All writes go through the GenServer to
  serialize access. Reads go directly to DETS — DETS has its own file lock
  and is safe for concurrent reads.

  Key format: {namespace, key} — e.g., {:rate_limits, "/api/users"}
  """

  use GenServer

  @table :config_store

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  @spec get(atom(), term()) :: {:ok, term()} | :error
  def get(namespace, key), do: get({namespace, key})

  @spec get(term()) :: {:ok, term()} | :error
  def get(key) do
    # HINT: :dets.lookup/2 returns [{key, value}] or []
    # Read directly from DETS — no need for GenServer.call
    # TODO: implement
  end

  @spec put(atom(), term(), term()) :: :ok
  def put(namespace, key, value), do: put({namespace, key}, value)

  @spec put(term(), term()) :: :ok
  def put(key, value) do
    GenServer.call(__MODULE__, {:put, key, value})
  end

  @spec delete(atom(), term()) :: :ok
  def delete(namespace, key), do: delete({namespace, key})

  @spec delete(term()) :: :ok
  def delete(key) do
    GenServer.call(__MODULE__, {:delete, key})
  end

  @spec all(atom()) :: [{term(), term()}]
  def all(namespace) do
    # HINT: :dets.match/2 with pattern {{{namespace, :"$1"}, :"$2"}, [], ...}
    # Or build a select match spec for the namespace prefix
    # TODO: implement
  end

  @spec all() :: [{term(), term()}]
  def all do
    :dets.match(@table, {:"$1", :"$2"})
    |> Enum.map(fn [k, v] -> {k, v} end)
  end

  # ---------------------------------------------------------------------------
  # GenServer callbacks
  # ---------------------------------------------------------------------------

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(opts) do
    path =
      Keyword.get(opts, :path, Path.join(System.tmp_dir!(), "api_gateway_config.dets"))

    {:ok, _ref} =
      :dets.open_file(@table, [
        type: :set,
        file: String.to_charlist(path),
        # Flush to disk every 10 seconds.
        # Critical config changes should call :dets.sync explicitly.
        auto_save: 10_000
      ])

    count = :dets.info(@table, :size)
    IO.puts("ConfigStore: loaded #{count} entries from #{path}")

    {:ok, %{path: path}}
  end

  @impl true
  def handle_call({:put, key, value}, _from, state) do
    :ok = :dets.insert(@table, {key, value})
    {:reply, :ok, state}
  end

  @impl true
  def handle_call({:delete, key}, _from, state) do
    :ok = :dets.delete(@table, key)
    {:reply, :ok, state}
  end

  @impl true
  def terminate(_reason, _state) do
    # ALWAYS sync and close DETS on shutdown.
    # Without this, the next startup triggers automatic repair (slow for large tables).
    :dets.sync(@table)
    :dets.close(@table)
  end
end
```

### Step 2: `lib/api_gateway/config/persistent_cache.ex`

```elixir
defmodule ApiGateway.Config.PersistentCache do
  @moduledoc """
  Two-level cache for upstream responses.

  L1: ETS (:public, read_concurrency: true) — nanosecond reads, lost on restart
  L2: DETS — microsecond reads, survives restart

  Read path: L1 hit → return. L1 miss → L2 lookup → warm L1 → return.
  Write path: write both L1 and L2 simultaneously.
  Expiry: stored as {value, expires_at_ms} in both layers. Lazy eviction on read.
  """

  use GenServer

  @l1_table :persistent_cache_l1
  @l2_table :persistent_cache_l2
  @cleanup_interval_ms 60_000

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  @spec get(term()) :: {:ok, term()} | :miss
  def get(key) do
    case l1_get(key) do
      {:ok, value} -> {:ok, value}
      :miss -> l2_get_and_warm(key)
    end
  end

  @spec put(term(), term(), pos_integer()) :: :ok
  def put(key, value, ttl_ms \\ 300_000) do
    GenServer.call(__MODULE__, {:put, key, value, ttl_ms})
  end

  @spec invalidate(term()) :: :ok
  def invalidate(key) do
    GenServer.call(__MODULE__, {:invalidate, key})
  end

  # ---------------------------------------------------------------------------
  # Private read helpers — bypass GenServer entirely
  # ---------------------------------------------------------------------------

  defp l1_get(key) do
    now = System.monotonic_time(:millisecond)

    case :ets.lookup(@l1_table, key) do
      [{^key, value, expires_at}] when expires_at > now -> {:ok, value}
      [{^key, _value, _expired}] ->
        :ets.delete(@l1_table, key)
        :miss
      [] -> :miss
    end
  end

  defp l2_get_and_warm(key) do
    # HINT: :dets.lookup returns [{key, value, expires_at}] or []
    # HINT: compare expires_at with System.os_time(:millisecond) (wall clock, not monotonic)
    #       because DETS entries survive across restarts — os_time is appropriate here
    # HINT: on L2 hit: insert into L1 with remaining TTL, return {:ok, value}
    # HINT: on expired: :dets.delete to lazy-evict, return :miss
    # TODO: implement
  end

  # ---------------------------------------------------------------------------
  # GenServer callbacks
  # ---------------------------------------------------------------------------

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(opts) do
    path = Keyword.get(opts, :path, Path.join(System.tmp_dir!(), "api_gateway_cache.dets"))

    :ets.new(@l1_table, [:set, :public, :named_table, {:read_concurrency, true}])

    {:ok, _ref} =
      :dets.open_file(@l2_table, [
        type: :set,
        file: String.to_charlist(path),
        auto_save: 30_000
      ])

    Process.send_after(self(), :cleanup_l1, @cleanup_interval_ms)
    {:ok, %{path: path}}
  end

  @impl true
  def handle_call({:put, key, value, ttl_ms}, _from, state) do
    # L1 uses monotonic time (can't survive restart anyway)
    l1_expires = System.monotonic_time(:millisecond) + ttl_ms
    # L2 uses wall clock time (survives restart and can be compared after reboot)
    l2_expires = System.os_time(:millisecond) + ttl_ms

    :ets.insert(@l1_table, {key, value, l1_expires})
    :dets.insert(@l2_table, {key, value, l2_expires})

    {:reply, :ok, state}
  end

  @impl true
  def handle_call({:invalidate, key}, _from, state) do
    :ets.delete(@l1_table, key)
    :dets.delete(@l2_table, key)
    {:reply, :ok, state}
  end

  @impl true
  def handle_info(:cleanup_l1, state) do
    now = System.monotonic_time(:millisecond)
    ms = :ets.fun2ms(fn {_k, _v, expires_at} when expires_at =< now -> true end)
    deleted = :ets.select_delete(@l1_table, ms)

    if deleted > 0 do
      IO.puts("[PersistentCache] L1 cleanup: removed #{deleted} expired entries")
    end

    Process.send_after(self(), :cleanup_l1, @cleanup_interval_ms)
    {:noreply, state}
  end

  @impl true
  def terminate(_reason, _state) do
    :dets.sync(@l2_table)
    :dets.close(@l2_table)
  end
end
```

### Step 3: Given tests — must pass without modification

```elixir
# test/api_gateway/config/store_test.exs
defmodule ApiGateway.Config.StoreTest do
  use ExUnit.Case, async: false

  alias ApiGateway.Config.Store

  @test_path "/tmp/api_gateway_test_config_#{:erlang.unique_integer([:positive])}.dets"

  setup do
    {:ok, _pid} = Store.start_link(path: @test_path)
    on_exit(fn ->
      GenServer.stop(Store)
      File.rm(@test_path)
    end)
    :ok
  end

  describe "put/3 and get/2" do
    test "stores and retrieves a namespaced value" do
      :ok = Store.put(:rate_limits, "/api/users", 1000)
      assert {:ok, 1000} = Store.get(:rate_limits, "/api/users")
    end

    test "returns :error for unknown key" do
      assert :error = Store.get(:rate_limits, "/nonexistent")
    end

    test "overwrites existing value" do
      Store.put(:config, :timeout_ms, 5_000)
      Store.put(:config, :timeout_ms, 10_000)
      assert {:ok, 10_000} = Store.get(:config, :timeout_ms)
    end
  end

  describe "delete/2" do
    test "removes the entry" do
      Store.put(:flags, :dark_mode, true)
      Store.delete(:flags, :dark_mode)
      assert :error = Store.get(:flags, :dark_mode)
    end
  end

  describe "all/1" do
    test "returns all entries for a namespace" do
      Store.put(:backends, "/api/a", "http://a.internal")
      Store.put(:backends, "/api/b", "http://b.internal")
      Store.put(:other, :key, :value)

      results = Store.all(:backends)
      assert length(results) == 2
      routes = Enum.map(results, fn {k, _v} -> k end) |> Enum.sort()
      assert routes == ["/api/a", "/api/b"]
    end
  end

  describe "persistence across restarts" do
    test "data survives a GenServer stop and restart" do
      Store.put(:rate_limits, "/api/test", 500)
      GenServer.stop(Store)

      {:ok, _pid} = Store.start_link(path: @test_path)
      assert {:ok, 500} = Store.get(:rate_limits, "/api/test")
    end
  end
end
```

```elixir
# test/api_gateway/config/persistent_cache_test.exs
defmodule ApiGateway.Config.PersistentCacheTest do
  use ExUnit.Case, async: false

  alias ApiGateway.Config.PersistentCache

  @test_path "/tmp/api_gateway_test_cache_#{:erlang.unique_integer([:positive])}.dets"

  setup do
    {:ok, _pid} = PersistentCache.start_link(path: @test_path)

    on_exit(fn ->
      GenServer.stop(PersistentCache)
      File.rm(@test_path)
    end)

    :ok
  end

  describe "put/3 and get/1" do
    test "returns :miss for unknown key" do
      assert :miss = PersistentCache.get("unknown")
    end

    test "returns the value after put" do
      PersistentCache.put("key1", "value1", 60_000)
      assert {:ok, "value1"} = PersistentCache.get("key1")
    end

    test "returns :miss after TTL expires" do
      PersistentCache.put("short_key", "v", 50)
      Process.sleep(100)
      assert :miss = PersistentCache.get("short_key")
    end
  end

  describe "invalidate/1" do
    test "removes the entry from both L1 and L2" do
      PersistentCache.put("inv_key", "data", 60_000)
      PersistentCache.invalidate("inv_key")
      assert :miss = PersistentCache.get("inv_key")
    end
  end

  describe "L2 warm-up after restart" do
    test "entry survives GenServer restart if not expired" do
      PersistentCache.put("persistent_key", "persistent_value", 300_000)
      GenServer.stop(PersistentCache)

      {:ok, _pid} = PersistentCache.start_link(path: @test_path)
      assert {:ok, "persistent_value"} = PersistentCache.get("persistent_key")
    end
  end
end
```

### Step 4: Run the tests

```bash
mix test test/api_gateway/config/ --trace
```

---

## Trade-off analysis

| Aspect | DETS | ETS + DETS (L1/L2) | Mnesia (disc_copies) | PostgreSQL |
|--------|------|-------------------|---------------------|------------|
| Durability | Per auto_save or explicit sync | L2 durable, L1 lost on crash | Per transaction | Per commit |
| Read latency | µs (disk I/O) | ns for L1 hit, µs for L2 miss | µs (RAM copy) | ms (network + disk) |
| Max size | 2 GB per file | 2 GB for L2 | RAM + disk (no hard limit) | Unlimited |
| Concurrent writes | Serialized (file lock) | GenServer serializes | Transactional | Transactional |
| Range queries | No (no `:ordered_set`) | No | Yes (QLC) | Yes (SQL) |
| Crash recovery | Auto-repair on next open | L2 auto-repair | Mnesia transaction log | WAL |
| Operational complexity | Zero (no infra) | Zero | Medium (schema management) | High |

Reflection: the `PersistentCache` uses `System.monotonic_time` for L1 TTL and
`System.os_time` for L2 TTL. Why? What would break if you used the same clock for both?

---

## Common production mistakes

**1. Not calling `terminate/2` before the process dies**
If the GenServer dies without calling `:dets.close/1`, DETS marks the file as needing
repair. On the next startup, repair runs automatically — and it can take 10–30 seconds
for large files. Always implement `terminate/2`.

**2. Treating `insert/2` as durable**
`:dets.insert/2` writes to an OS buffer. The data is not on disk until `:dets.sync/1`
runs (either explicitly or via `auto_save`). For config that must survive a power loss,
call `sync/1` explicitly after critical writes.

**3. Opening the same DETS file from multiple processes**
DETS has a file-level lock. Opening the same file from two Erlang processes is undefined
behavior and can corrupt the file. Use a single GenServer as the exclusive owner.

**4. Using DETS for high-frequency writes**
DETS writes involve syscalls. Above ~1000 writes/second, DETS becomes a bottleneck.
If you need high-frequency durable writes, batch them (accumulate in ETS, flush to DETS
every N seconds) or switch to a write-ahead-log approach.

**5. Ignoring the 2 GB limit until it's too late**
DETS does not emit warnings as the file approaches 2 GB. Operations near the limit
start failing silently. Monitor file size in production and plan the migration to Mnesia
or an external store well before hitting the limit.

---

## Resources

- [Erlang DETS documentation](https://www.erlang.org/doc/man/dets.html) — `open_file`, `auto_save`, `repair` options
- [DETS vs ETS comparison — Erlang efficiency guide](https://www.erlang.org/doc/efficiency_guide/tablesDatabases.html)
- [Erlang in Anger — Fred Hebert](https://www.erlang-in-anger.com/) — storage patterns in production (free PDF)
