# Cache Patterns with ETS

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`. The gateway proxies requests to upstream services.
Some upstream responses are expensive — they hit a database, aggregate multiple services,
or require cryptographic verification. You need a cache layer between the router and the
upstream clients.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       ├── router.ex
│       ├── rate_limiter/
│       ├── metrics/
│       ├── config/
│       └── cache/
│           ├── store.ex           # ← you implement this
│           └── stampede_guard.ex  # ← and this
├── test/
│   └── api_gateway/
│       └── cache/
│           ├── store_test.exs
│           └── stampede_guard_test.exs
├── bench/
│   └── cache_bench.exs
└── mix.exs
```

---

## The business problem

The platform team reported two problems:

1. **Memory leak**: the cache has been running for 3 days. It's consuming 4 GB of RAM
   because expired entries are never removed. The TTL is stored but never enforced.

2. **Cache stampede**: when a popular cache entry expires (e.g., the product catalog),
   all 200 concurrent requests that hit the gateway simultaneously detect the miss and
   each fires a request to the upstream service. The upstream service falls over under
   the 200x load spike.

---

## Why `System.monotonic_time` for TTL, not `System.os_time`

TTL is a duration measurement: "this entry is valid for 60 seconds from now."
Duration measurements require a clock that only moves forward.

`System.os_time` can go backwards. NTP synchronization, leap second adjustments, and
manual clock changes all cause `os_time` to jump backwards. If the clock moves back
by 5 seconds while an entry has a 60-second TTL, that entry will appear valid for
65 seconds instead of 60.

`System.monotonic_time` only ever increases. It is the correct clock for TTL comparisons.

The exception: `PersistentCache` (exercise 17) uses `os_time` for L2 entries because
monotonic time resets to zero on restart — a value stored before restart would appear
expired immediately after restart if compared against the new monotonic clock.

---

## Why cache stampede is a systemic risk, not an edge case

When a high-traffic entry expires, the window between expiry and cache repopulation
exposes the upstream to a burst equal to the concurrent request rate. For a gateway
handling 10k req/s with a 60-second TTL, that window produces 10k simultaneous upstream
requests if not mitigated.

The fix: only one process fetches. The rest wait. This is the **singleton fetch** pattern,
implemented with `:ets.insert_new/2` as a lightweight mutex.

The double-check inside the lock is not optional:

```
Process A: cache miss → try to acquire lock → wins
Process B: cache miss → try to acquire lock → waits (polling)
Process A: fetches from upstream → populates cache → releases lock
Process B: polling loop → checks cache → HIT (populated by A)
  without double-check → B would fetch again despite the cache being warm
```

---

## Implementation

### Step 1: `lib/api_gateway/cache/store.ex`

```elixir
defmodule ApiGateway.Cache.Store do
  @moduledoc """
  TTL cache backed by ETS with automatic cleanup.

  Design:
  - Reads bypass the GenServer entirely (direct :ets.lookup)
  - Writes go through the GenServer only to ensure the table exists while alive
  - Cleanup runs periodically via handle_info — no separate supervisor needed
  - Lazy eviction on read removes expired entries on access
  - Periodic cleanup removes entries that expire without being accessed
  """

  use GenServer

  @table :cache_store
  @cleanup_interval_ms 30_000

  # ---------------------------------------------------------------------------
  # Public API — reads do not go through the GenServer
  # ---------------------------------------------------------------------------

  @doc """
  Returns {:ok, value} if the key exists and has not expired.
  Returns :miss otherwise. Expired entries are lazily deleted on miss.
  """
  @spec get(term()) :: {:ok, term()} | :miss
  def get(key) do
    now = System.monotonic_time(:millisecond)

    case :ets.lookup(@table, key) do
      [{^key, value, expires_at}] when expires_at > now ->
        {:ok, value}

      [{^key, _value, _expired}] ->
        # Lazy eviction: delete expired entry immediately on access
        :ets.delete(@table, key)
        :miss

      [] ->
        :miss
    end
  end

  @doc """
  Stores a value with the given TTL in milliseconds.
  Direct ETS write — no GenServer serialization needed for writes
  because :ets.insert is atomic for a single record.
  """
  @spec put(term(), term(), pos_integer()) :: :ok
  def put(key, value, ttl_ms \\ 60_000) do
    expires_at = System.monotonic_time(:millisecond) + ttl_ms
    :ets.insert(@table, {key, value, expires_at})
    :ok
  end

  @doc """
  Returns the value if cached and fresh.
  On miss: calls fetch_fn/0, caches the result, returns it.
  NOT stampede-safe — use StampedeGuard.get_or_fetch/3 for hot keys.
  """
  @spec get_or_put(term(), (-> term()), pos_integer()) :: {:ok, term(), :hit | :miss}
  def get_or_put(key, fetch_fn, ttl_ms \\ 60_000) do
    case get(key) do
      {:ok, value} ->
        {:ok, value, :hit}

      :miss ->
        value = fetch_fn.()
        put(key, value, ttl_ms)
        {:ok, value, :miss}
    end
  end

  @spec delete(term()) :: :ok
  def delete(key) do
    :ets.delete(@table, key)
    :ok
  end

  @doc """
  Returns %{total: N, expired: N, fresh: N} without removing any entries.
  Uses :ets.select_count — no full table copy.
  """
  @spec stats() :: %{total: non_neg_integer(), expired: non_neg_integer(), fresh: non_neg_integer()}
  def stats do
    now = System.monotonic_time(:millisecond)
    total = :ets.info(@table, :size)
    # HINT: :ets.fun2ms generates compile-time match specs — valid here because
    #       `now` is bound at call time and captured in the closure correctly
    expired_ms = :ets.fun2ms(fn {_k, _v, expires_at} when expires_at =< now -> true end)
    expired = :ets.select_count(@table, expired_ms)
    %{total: total, expired: expired, fresh: total - expired}
  end

  @spec flush() :: :ok
  def flush do
    :ets.delete_all_objects(@table)
    :ok
  end

  # ---------------------------------------------------------------------------
  # GenServer — owns the table and runs periodic cleanup
  # ---------------------------------------------------------------------------

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(opts) do
    table = :ets.new(@table, [
      :set,
      :public,
      :named_table,
      {:read_concurrency, true},
      {:write_concurrency, true}
    ])

    interval = Keyword.get(opts, :cleanup_interval_ms, @cleanup_interval_ms)
    Process.send_after(self(), :cleanup, interval)

    {:ok, %{table: table, interval: interval}}
  end

  @impl true
  def handle_info(:cleanup, state) do
    now = System.monotonic_time(:millisecond)
    ms = :ets.fun2ms(fn {_k, _v, expires_at} when expires_at =< now -> true end)
    deleted = :ets.select_delete(@table, ms)

    if deleted > 0 do
      IO.puts("[Cache.Store] cleanup: removed #{deleted} expired entries")
    end

    Process.send_after(self(), :cleanup, state.interval)
    {:noreply, state}
  end
end
```

### Step 2: `lib/api_gateway/cache/stampede_guard.ex`

```elixir
defmodule ApiGateway.Cache.StampedeGuard do
  @moduledoc """
  Stampede-safe cache wrapper.

  Guarantees that fetch_fn is called at most once per key per TTL window,
  even when many processes concurrently detect the same cache miss.

  Implementation: uses a separate ETS lock table with :ets.insert_new/2 as
  a lightweight mutex. The process that wins the lock fetches; the rest poll
  with exponential backoff until the key appears in the cache.
  """

  use GenServer

  @cache_table :stampede_cache
  @lock_table :stampede_locks
  @default_ttl_ms 60_000
  @lock_timeout_ms 10_000
  @poll_interval_ms 5

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  @spec get_or_fetch(term(), (-> term()), pos_integer()) ::
          {:ok, term()} | {:error, :timeout}
  def get_or_fetch(key, fetch_fn, ttl_ms \\ @default_ttl_ms) do
    case direct_get(key) do
      {:ok, value} -> {:ok, value}
      :miss -> singleton_fetch(key, fetch_fn, ttl_ms)
    end
  end

  @spec get_stats() :: %{fetch_count: non_neg_integer(), wait_count: non_neg_integer()}
  def get_stats do
    GenServer.call(__MODULE__, :stats)
  end

  # ---------------------------------------------------------------------------
  # Private — direct ETS reads, no GenServer
  # ---------------------------------------------------------------------------

  defp direct_get(key) do
    now = System.monotonic_time(:millisecond)

    case :ets.lookup(@cache_table, key) do
      [{^key, value, expires_at}] when expires_at > now -> {:ok, value}
      [{^key, _v, _expired}] ->
        :ets.delete(@cache_table, key)
        :miss
      [] -> :miss
    end
  end

  defp singleton_fetch(key, fetch_fn, ttl_ms) do
    lock_key = {:lock, key}

    if :ets.insert_new(@lock_table, {lock_key, self()}) do
      # This process wins the lock — responsible for fetching
      GenServer.cast(__MODULE__, :increment_fetch)

      try do
        value = fetch_fn.()
        expires_at = System.monotonic_time(:millisecond) + ttl_ms
        :ets.insert(@cache_table, {key, value, expires_at})
        {:ok, value}
      rescue
        e ->
          :ets.delete(@lock_table, lock_key)
          reraise e, __STACKTRACE__
      after
        :ets.delete(@lock_table, lock_key)
      end
    else
      # Another process is fetching — wait with polling
      GenServer.cast(__MODULE__, :increment_wait)
      deadline = System.monotonic_time(:millisecond) + @lock_timeout_ms
      wait_for_key(key, deadline)
    end
  end

  defp wait_for_key(key, deadline) do
    if System.monotonic_time(:millisecond) >= deadline do
      {:error, :timeout}
    else
      case direct_get(key) do
        {:ok, value} -> {:ok, value}
        :miss ->
          # HINT: Process.sleep(@poll_interval_ms) then recurse
          # TODO: implement — return :miss path
      end
    end
  end

  # ---------------------------------------------------------------------------
  # GenServer callbacks
  # ---------------------------------------------------------------------------

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(_opts) do
    :ets.new(@cache_table, [:set, :public, :named_table, {:read_concurrency, true}, {:write_concurrency, true}])
    :ets.new(@lock_table, [:set, :public, :named_table, {:write_concurrency, true}])
    {:ok, %{fetch_count: 0, wait_count: 0}}
  end

  @impl true
  def handle_cast(:increment_fetch, state) do
    {:noreply, %{state | fetch_count: state.fetch_count + 1}}
  end

  @impl true
  def handle_cast(:increment_wait, state) do
    {:noreply, %{state | wait_count: state.wait_count + 1}}
  end

  @impl true
  def handle_call(:stats, _from, state) do
    {:reply, state, state}
  end
end
```

### Step 3: Given tests — must pass without modification

```elixir
# test/api_gateway/cache/store_test.exs
defmodule ApiGateway.Cache.StoreTest do
  use ExUnit.Case, async: false

  alias ApiGateway.Cache.Store

  setup do
    Store.flush()
    :ok
  end

  describe "get/1 and put/3" do
    test "returns :miss for unknown key" do
      assert :miss = Store.get("unknown")
    end

    test "returns value before TTL expiry" do
      Store.put("key1", "value1", 5_000)
      assert {:ok, "value1"} = Store.get("key1")
    end

    test "returns :miss after TTL expiry" do
      Store.put("short_key", "v", 50)
      Process.sleep(100)
      assert :miss = Store.get("short_key")
    end

    test "expired entry is removed on access (lazy eviction)" do
      Store.put("lazy_key", "v", 50)
      Process.sleep(100)
      Store.get("lazy_key")
      # After lazy eviction, the key should not appear in stats as expired
      stats = Store.stats()
      # total may be 0 if cleanup also ran, but expired should be 0
      assert stats.expired == 0 or stats.total == 0
    end
  end

  describe "get_or_put/3" do
    test "calls fetch_fn on miss and caches result" do
      fetch_called = :counters.new(1, [])

      {:ok, val, :miss} =
        Store.get_or_put("computed", fn ->
          :counters.add(fetch_called, 1, 1)
          "expensive_result"
        end, 10_000)

      assert val == "expensive_result"
      assert :counters.get(fetch_called, 1) == 1
    end

    test "does not call fetch_fn on subsequent hit" do
      Store.put("warm_key", "cached", 10_000)
      fetch_called = :counters.new(1, [])

      {:ok, _val, :hit} = Store.get_or_put("warm_key", fn ->
        :counters.add(fetch_called, 1, 1)
        "never_called"
      end, 10_000)

      assert :counters.get(fetch_called, 1) == 0
    end
  end

  describe "stats/0" do
    test "reports correct counts" do
      Store.put("s1", 1, 10_000)
      Store.put("s2", 2, 10_000)
      Store.put("s3", 3, 50)
      Process.sleep(100)

      stats = Store.stats()
      assert stats.fresh >= 2
      assert stats.expired >= 1
      assert stats.total == stats.fresh + stats.expired
    end
  end
end
```

```elixir
# test/api_gateway/cache/stampede_guard_test.exs
defmodule ApiGateway.Cache.StampedeGuardTest do
  use ExUnit.Case, async: false

  alias ApiGateway.Cache.StampedeGuard

  setup do
    :ets.delete_all_objects(:stampede_cache)
    :ets.delete_all_objects(:stampede_locks)
    :ok
  end

  describe "get_or_fetch/3" do
    test "returns the value on cache hit" do
      expires_at = System.monotonic_time(:millisecond) + 60_000
      :ets.insert(:stampede_cache, {"hit_key", "cached_value", expires_at})

      assert {:ok, "cached_value"} = StampedeGuard.get_or_fetch("hit_key", fn -> "never" end)
    end

    test "calls fetch_fn exactly once for 50 concurrent misses on the same key" do
      fetch_count = :counters.new(1, [])

      tasks =
        for _ <- 1..50 do
          Task.async(fn ->
            StampedeGuard.get_or_fetch("hot_key", fn ->
              :counters.add(fetch_count, 1, 1)
              Process.sleep(30)
              "computed"
            end, 30_000)
          end)
        end

      results = Task.await_many(tasks, 10_000)

      ok_count = Enum.count(results, &match?({:ok, _}, &1))
      actual_fetches = :counters.get(fetch_count, 1)

      assert ok_count == 50
      assert actual_fetches == 1, "Expected 1 fetch, got #{actual_fetches}"
    end

    test "stats reflect fetch vs wait counts" do
      fetch_count_ref = :counters.new(1, [])

      tasks =
        for _ <- 1..10 do
          Task.async(fn ->
            StampedeGuard.get_or_fetch("stats_key", fn ->
              :counters.add(fetch_count_ref, 1, 1)
              Process.sleep(20)
              "result"
            end, 30_000)
          end)
        end

      Task.await_many(tasks, 5_000)
      stats = StampedeGuard.get_stats()

      assert stats.fetch_count == 1
      assert stats.wait_count == 9
    end
  end
end
```

### Step 4: Run the tests

```bash
mix test test/api_gateway/cache/ --trace
```

### Step 5: Cache performance benchmark

```elixir
# bench/cache_bench.exs
alias ApiGateway.Cache.Store

# Pre-populate 1000 entries
for i <- 1..1_000 do
  Store.put("key:#{i}", "value:#{i}", 300_000)
end

Benchee.run(
  %{
    "cache hit — direct ETS read" => fn ->
      Store.get("key:#{:rand.uniform(1_000)}")
    end,
    "cache miss — key not found" => fn ->
      Store.get("nonexistent:#{:rand.uniform(1_000_000)}")
    end
  },
  parallel: 100,
  warmup: 2,
  time: 5,
  formatters: [Benchee.Formatters.Console]
)
```

```bash
mix run bench/cache_bench.exs
```

**Expected**: cache hit < 5µs at p99 under 100 parallel readers. If you see higher latency,
verify that `get/1` does NOT use `GenServer.call` — it must read directly from ETS.

---

## Trade-off analysis

| Aspect | Lazy eviction only | Lazy + periodic cleanup | Stampede prevention |
|--------|-------------------|------------------------|---------------------|
| Memory growth | Unbounded for never-accessed expired keys | Bounded | No effect on memory |
| Cleanup overhead | Zero | O(n) per interval | Lock table overhead |
| Cache stampede | Possible for any miss | Possible for any miss | Prevented |
| Complexity | Low | Medium | High |
| `fetch_fn` called per miss | Once per process | Once per process | Once per key per TTL |

Reflection: `StampedeGuard` uses a polling loop with `Process.sleep` to wait for
the winning process to populate the cache. What are the downsides of polling vs
a mechanism based on process monitoring (`Process.monitor`)? Under what conditions
would monitoring be worth the added complexity?

---

## Common production mistakes

**1. Not using `monotonic_time` for TTL**
`System.system_time` can go backwards. An NTP adjustment can cause entries to stay
valid longer than intended, or expire immediately. Always use `System.monotonic_time`
for duration comparisons.

**2. Stampede prevention without double-check inside the lock**
Between the time a process detects a miss and acquires the lock, another process may
have already populated the cache. Without a re-check inside the lock, both processes
fetch. The second fetch is wasted and may cause a double-write race:

```elixir
# WRONG — no double-check
if :ets.insert_new(locks, {key, self()}) do
  value = fetch_fn.()  # another process may have already done this
  cache_put(key, value)
end
```

**3. Cleanup that deletes while iterating**
Never call `:ets.delete` inside a `first/next` iteration loop. The iterator is
invalidated. Use `:ets.select_delete` with a match spec — it is atomic and safe.

**4. Using `get_or_put/3` for stampede-sensitive keys**
`get_or_put` has no stampede protection — all concurrent misses fetch independently.
Use `StampedeGuard.get_or_fetch/3` for keys that are frequently accessed and expensive
to recompute.

**5. Assuming ETS is always faster than an in-process map**
For a single process accessing its own state, a Map in process state has zero IPC
overhead. ETS is faster only when multiple processes access the same data concurrently.
Do not add ETS complexity unless you have actual concurrent access.

---

## Resources

- [ETS documentation — Erlang/OTP](https://www.erlang.org/doc/man/ets.html)
- [`System.monotonic_time/1` — Elixir docs](https://hexdocs.pm/elixir/System.html#monotonic_time/1)
- [Cachex — production ETS cache library](https://hexdocs.pm/cachex/Cachex.html) — reference implementation of all patterns discussed here
- [Designing Data-Intensive Applications — Martin Kleppmann](https://www.oreilly.com/library/view/designing-data-intensive-applications/9781491903063/) — Chapter 11: cache patterns and consistency
