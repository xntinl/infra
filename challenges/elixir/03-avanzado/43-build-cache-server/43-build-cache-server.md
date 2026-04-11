# Cache Layer with ETS, TTL, and LRU Eviction

## Overview

Build a shared cache layer for an HTTP API gateway using ETS with TTL-based expiration
and LRU eviction. The cache allows concurrent reads without serializing through a single
process, expires entries automatically, and evicts the least recently used entry when
memory pressure is reached.

Project structure:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       └── cache/
│           ├── server.ex               # GenServer that owns the ETS table
│           ├── lru.ex                  # LRU order tracking
│           └── ttl_expirer.ex          # periodic sweep of expired entries
├── test/
│   └── api_gateway/
│       └── cache_test.exs
├── bench/
│   └── cache_bench.exs
└── mix.exs
```

---

## The business problem

An internal API gateway routes traffic to downstream microservices. The payments service
receives identical exchange-rate lookups thousands of times per minute, but rates change
at most once per minute. The cache must:

1. Return cached values in O(1) without serializing through a single process
2. Expire entries automatically -- values must not be served stale beyond their TTL
3. Evict the least recently used entry when a fixed size limit is reached
4. Never grow unbounded -- the system runs 24/7

---

## Why reads bypass the GenServer

A GenServer holding a map serializes all operations through a single mailbox. Under load,
read latency climbs proportionally to backlog.

ETS with `:protected` and `read_concurrency: true` allows concurrent reads without touching
the GenServer process:

```
request A ──ets:lookup──> ETS table  (concurrent, no serialization)
request B ──ets:lookup──> ETS table
request C ──ets:lookup──> ETS table
request D ──GenServer.call──> GenServer ──ets:insert──> ETS table
```

Only writes (`put/3`) and eviction decisions go through the GenServer. Reads (`get/1`) go
directly to ETS. This is the **protected ETS owner** pattern: the GenServer owns the table
and serializes writes; ETS serves reads lock-free.

---

## Why LRU eviction and not random eviction

Random eviction wastes cache space on entries that are frequently accessed. LRU ensures
that the entries most likely to be requested again survive under memory pressure. For a
gateway serving a limited set of downstream endpoints, the working set is small and LRU
approximates it well.

The cost of LRU: O(n) to move an entry to the front on each access, unless you maintain an
auxiliary doubly-linked list with O(1) move. This implementation uses the simpler O(n) list.

---

## Implementation

### Step 1: Create the project structure

```bash
mix new api_gateway --sup
cd api_gateway
mkdir -p lib/api_gateway/cache
mkdir -p test/api_gateway
mkdir -p bench
```

### Step 2: `mix.exs` -- add benchee

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: `lib/api_gateway/cache/lru.ex`

The LRU order is maintained as a simple list where the head is the Most Recently Used (MRU)
and the tail is the Least Recently Used (LRU). `touch/2` moves a key to the front.
`evict_lru/1` removes the last element.

```elixir
defmodule ApiGateway.Cache.LRU do
  @moduledoc """
  LRU order tracking as a simple list [MRU, ..., LRU].

  The list-based implementation is O(n) for touch/1.
  For caches with max_size < 10_000, this is acceptable.
  """

  @doc """
  Moves `key` to the front (MRU position). If not present, inserts it.
  """
  @spec touch([term()], term()) :: [term()]
  def touch(order, key) do
    [key | List.delete(order, key)]
  end

  @doc """
  Removes the LRU entry (last in list) and returns {lru_key, new_order}.
  Returns {nil, []} if the list is empty.
  """
  @spec evict_lru([term()]) :: {term() | nil, [term()]}
  def evict_lru([]), do: {nil, []}

  def evict_lru(order) do
    lru_key = List.last(order)
    new_order = Enum.drop(order, -1)
    {lru_key, new_order}
  end

  @doc """
  Removes a specific key from the order list (used on explicit delete).
  """
  @spec remove([term()], term()) :: [term()]
  def remove(order, key) do
    List.delete(order, key)
  end
end
```

### Step 4: `lib/api_gateway/cache/server.ex`

The GenServer owns the ETS table and serializes all write operations.
Reads go directly to ETS via `get/1` -- they never touch the GenServer mailbox.

```elixir
defmodule ApiGateway.Cache.Server do
  use GenServer

  @table :cache_entries
  @default_ttl_ms 60_000

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  @doc """
  Looks up a cached value. Returns `{:ok, value}` or `{:miss}`.

  Reads directly from ETS -- does NOT go through the GenServer process.
  If the entry exists but has expired, deletes it and returns `{:miss}`.
  """
  @spec get(term()) :: {:ok, term()} | {:miss}
  def get(key) do
    now = System.monotonic_time(:millisecond)

    case :ets.lookup(@table, key) do
      [{^key, value, expiry_ms}] when expiry_ms > now ->
        {:ok, value}

      [{^key, _value, _expiry_ms}] ->
        GenServer.cast(__MODULE__, {:lazy_delete, key})
        {:miss}

      [] ->
        {:miss}
    end
  end

  @doc """
  Stores a value with an optional TTL (default #{@default_ttl_ms}ms).

  Goes through the GenServer to serialize the LRU order update and eviction check.
  """
  @spec put(term(), term(), keyword()) :: :ok
  def put(key, value, opts \\ []) do
    ttl_ms = Keyword.get(opts, :ttl_ms, @default_ttl_ms)
    GenServer.call(__MODULE__, {:put, key, value, ttl_ms})
  end

  @doc "Removes an entry explicitly."
  @spec delete(term()) :: :ok
  def delete(key) do
    GenServer.call(__MODULE__, {:delete, key})
  end

  @doc "Removes all entries."
  @spec flush() :: :ok
  def flush do
    GenServer.call(__MODULE__, :flush)
  end

  @doc "Returns the number of entries currently in the cache."
  @spec size() :: non_neg_integer()
  def size, do: :ets.info(@table, :size)

  # ---------------------------------------------------------------------------
  # GenServer lifecycle
  # ---------------------------------------------------------------------------

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(opts) do
    max_size = Keyword.get(opts, :max_size, 1_000)

    table = :ets.new(@table, [:named_table, :protected, :set, read_concurrency: true])

    {:ok, %{table: table, max_size: max_size, lru_order: [], hits: 0, misses: 0}}
  end

  # ---------------------------------------------------------------------------
  # Callbacks
  # ---------------------------------------------------------------------------

  @impl true
  def handle_call({:put, key, value, ttl_ms}, _from, state) do
    expiry = System.monotonic_time(:millisecond) + ttl_ms

    already_exists = :ets.lookup(@table, key) != []

    state =
      if :ets.info(@table, :size) >= state.max_size and not already_exists do
        {lru_key, new_order} = ApiGateway.Cache.LRU.evict_lru(state.lru_order)

        if lru_key do
          :ets.delete(@table, lru_key)
        end

        %{state | lru_order: new_order}
      else
        state
      end

    :ets.insert(@table, {key, value, expiry})

    new_order = ApiGateway.Cache.LRU.touch(state.lru_order, key)

    {:reply, :ok, %{state | lru_order: new_order}}
  end

  def handle_call({:delete, key}, _from, state) do
    :ets.delete(@table, key)
    new_order = ApiGateway.Cache.LRU.remove(state.lru_order, key)
    {:reply, :ok, %{state | lru_order: new_order}}
  end

  def handle_call(:flush, _from, state) do
    :ets.delete_all_objects(@table)
    {:reply, :ok, %{state | lru_order: []}}
  end

  @impl true
  def handle_cast({:lazy_delete, key}, state) do
    :ets.delete(@table, key)
    new_order = ApiGateway.Cache.LRU.remove(state.lru_order, key)
    {:noreply, %{state | lru_order: new_order}}
  end
end
```

### Step 5: `lib/api_gateway/cache/ttl_expirer.ex`

Lazy expiry on `get/1` handles hot entries. Cold entries -- keys that are never requested
again -- accumulate indefinitely without the periodic sweep. This GenServer runs a sweep
every 30 seconds to clean them up.

```elixir
defmodule ApiGateway.Cache.TTLExpirer do
  use GenServer

  @sweep_interval_ms 30_000
  @table :cache_entries

  def start_link(_opts), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  @impl true
  def init(_) do
    Process.send_after(self(), :sweep, @sweep_interval_ms)
    {:ok, %{}}
  end

  @impl true
  def handle_info(:sweep, state) do
    now = System.monotonic_time(:millisecond)

    @table
    |> :ets.tab2list()
    |> Enum.each(fn {key, _value, expiry} ->
      if expiry < now do
        :ets.delete(@table, key)
      end
    end)

    Process.send_after(self(), :sweep, @sweep_interval_ms)
    {:noreply, state}
  end
end
```

### Step 6: Tests

```elixir
# test/api_gateway/cache_test.exs
defmodule ApiGateway.CacheTest do
  use ExUnit.Case, async: false

  alias ApiGateway.Cache.Server

  setup do
    :ets.delete_all_objects(:cache_entries)
    GenServer.call(Server, :flush)
    :ok
  end

  describe "get/1 and put/3" do
    test "returns miss for unknown key" do
      assert {:miss} = Server.get("unknown")
    end

    test "returns ok with value after put" do
      Server.put("key1", "value1")
      Process.sleep(5)
      assert {:ok, "value1"} = Server.get("key1")
    end

    test "expired entry returns miss" do
      Server.put("expiring", "val", ttl_ms: 50)
      Process.sleep(100)
      assert {:miss} = Server.get("expiring")
    end

    test "delete removes the entry" do
      Server.put("del_key", "val")
      Process.sleep(5)
      Server.delete("del_key")
      assert {:miss} = Server.get("del_key")
    end

    test "flush removes all entries" do
      Server.put("a", 1)
      Server.put("b", 2)
      Process.sleep(5)
      Server.flush()
      assert {:miss} = Server.get("a")
      assert {:miss} = Server.get("b")
    end
  end

  describe "LRU eviction" do
    test "evicts LRU entry when max_size is reached" do
      GenServer.stop(Server)
      {:ok, _} = Server.start_link(max_size: 3)

      Server.put("a", 1)
      Server.put("b", 2)
      Server.put("c", 3)
      Process.sleep(5)

      Server.get("a")

      Server.put("d", 4)
      Process.sleep(5)

      assert {:ok, 1} = Server.get("a")
      assert {:miss}  = Server.get("b")
      assert {:ok, 3} = Server.get("c")
      assert {:ok, 4} = Server.get("d")
    end
  end

  describe "concurrent reads" do
    test "100 concurrent readers without errors" do
      Server.put("shared", "data")
      Process.sleep(5)

      tasks = for _ <- 1..100, do: Task.async(fn -> Server.get("shared") end)
      results = Task.await_many(tasks, 5_000)

      assert Enum.all?(results, &match?({:ok, "data"}, &1))
    end
  end
end
```

### Step 7: Run the tests

```bash
mix test test/api_gateway/cache_test.exs --trace
```

### Step 8: Benchmark

```elixir
# bench/cache_bench.exs
Benchee.run(
  %{
    "get -- miss (ETS read)" => fn ->
      ApiGateway.Cache.Server.get("nonexistent_#{:rand.uniform(10_000)}")
    end,
    "get -- hit (ETS read)" => fn ->
      ApiGateway.Cache.Server.get("exchange_rates")
    end,
    "put (GenServer call)" => fn ->
      ApiGateway.Cache.Server.put("bench_key_#{:rand.uniform(1_000)}", :data)
    end
  },
  parallel: 8,
  time: 5,
  warmup: 2,
  formatters: [Benchee.Formatters.Console]
)
```

Seed the `hit` key before running:

```bash
# In iex -S mix:
ApiGateway.Cache.Server.put("exchange_rates", %{usd: 1.08, gbp: 0.86}, ttl_ms: 300_000)
```

```bash
mix run bench/cache_bench.exs
```

**Expected result on modern hardware**: `get` < 5us at p99. If `get` is > 50us, verify
it is reading directly from ETS and not making a `GenServer.call`.

---

## Trade-off analysis

| Aspect | ETS `:protected` + LRU list | GenServer map (no ETS) | Redis |
|--------|-----------------------------|------------------------|-------|
| Concurrent reads | lock-free after ETS lookup | serialized by mailbox | network round-trip |
| Eviction policy | LRU (O(n) list) | configurable | configurable |
| p50 read latency | < 2us (measure) | proportional to backlog | > 500us |
| Memory for 1k entries | measure | measure | off-heap |
| TTL enforcement | lazy (on read) + periodic sweep | lazy or periodic | native |
| Survives node crash | no | no | yes (persistence) |

---

## Common production mistakes

**1. `get/1` as a `GenServer.call`**
If `get/1` serializes through the GenServer, you've paid the cost of a process message
for every cache read. The whole point of ETS is to avoid that. Read directly with
`:ets.lookup/2`.

**2. Not limiting the LRU list size**
If `max_size` is not enforced strictly, the list grows with every new key. An unbounded
LRU list causes O(n) degradation in `put/3` even when the cache is operating normally.

**3. `System.os_time` instead of `System.monotonic_time` for TTL**
`os_time` can go backward (NTP, leap seconds). For time comparisons like `expiry > now`,
you need a monotonically increasing clock.

**4. Not sweeping expired entries**
Lazy expiry on `get/1` handles hot entries. Cold entries accumulate indefinitely without
the periodic sweep. After 24 hours, a busy gateway accumulates millions of expired entries.

**5. O(n) LRU touch on every `get/1` through the GenServer**
If `get/1` calls `GenServer.call` to update the LRU order on every read, you've made reads
as expensive as writes. Either accept that LRU order is only updated on `put/3` (approximate
LRU), or keep reads ETS-only and update LRU asynchronously with `cast`.

---

## Resources

- [`:ets` documentation -- Erlang/OTP](https://www.erlang.org/doc/man/ets.html) -- table types and access control
- [Erlang in Anger -- Fred Hebert](https://www.erlang-in-anger.com/) -- ETS in production (free PDF)
- [Caffeine cache paper](https://dl.acm.org/doi/10.1145/2806777.2806888) -- TinyLFU, a superior eviction policy to LRU
- [Benchee](https://github.com/bencheeorg/benchee) -- idiomatic benchmarking in Elixir
