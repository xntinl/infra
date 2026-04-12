# `Agent`-backed TTL cache with periodic sweep

**Project**: `ttl_cache_agent` — a key/value cache where every entry expires, plus a sweeper to reap the dead.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Project context

You need a tiny in-process cache — say, short-lived memoization of
database lookups or a sliding window of recent API responses — and you
want entries to disappear automatically after their TTL rather than grow
unbounded. The cache itself is simple; the interesting part is the
**sweeper** that walks the store and evicts stale entries.

`Agent` holds the state. A separate process drives the sweep on an
interval and calls the agent to purge expired keys. This split keeps
responsibilities clean: `Agent` = state, `sweeper` = time.

Alternative shapes exist (everything in a single `GenServer`, or `:ets`
with `:timer.send_interval`), and the trade-offs section covers when
each wins.

Project structure:

```
ttl_cache_agent/
├── lib/
│   ├── ttl_cache_agent.ex
│   └── ttl_cache_agent/sweeper.ex
├── test/
│   └── ttl_cache_agent_test.exs
└── mix.exs
```

---

## Core concepts

### 1. Expiry is a stored timestamp, not a timer

Storing one `:timer` per entry does not scale: 10_000 keys = 10_000
timer messages. Instead, store `{value, expires_at_ms}` and check at
read-time plus sweep periodically.

```
 key     value    expires_at (monotonic ms)
 "u:42"  %User{}  125_430_123
 "u:17"  %User{}  125_430_089   # ← already stale at 125_430_100
```

`System.monotonic_time(:millisecond)` is the right clock for TTL — it
never jumps backwards when the wall clock is corrected.

### 2. Lazy eviction + periodic sweep

Two eviction strategies, best used together:

- **Lazy**: on every `get`, if the entry is expired, delete it and
  return `:miss`. Keeps reads honest without a sweeper.
- **Periodic**: a sweeper process walks the whole map every `sweep_ms`
  and drops everything stale. Keeps memory bounded even for write-heavy
  caches that never read stale keys.

Either alone leaks memory or wastes CPU. Together they give bounded
memory with amortized cost.

### 3. `Agent` for state, a second process for time

`Agent` has no `handle_info`, so it can't self-schedule. The sweeper is
a tiny `GenServer` (or a `:timer` loop) that owns the cadence and calls
the agent to do the actual purge. This is a useful design pattern: keep
your data-holder dumb, and let dedicated processes drive behavior on
top.

```
  sweeper (GenServer)
     │
     │ sweep/0  every N ms
     ▼
  cache (Agent) ── purges expired keys
```

### 4. Purge runs inside the agent

The sweep function is an `Agent.update` closure that iterates the map
and filters out expired entries. Because it runs inside the agent, no
read or write can see a half-purged state.

---

## Implementation

### Step 1: Create the project

```bash
mix new ttl_cache_agent
cd ttl_cache_agent
```

### Step 2: `lib/ttl_cache_agent.ex`

```elixir
defmodule TtlCacheAgent do
  @moduledoc """
  A process-local key/value cache where every entry has a time-to-live
  (TTL). Expired entries are removed lazily on read and periodically by
  a sweeper (`TtlCacheAgent.Sweeper`).
  """

  use Agent

  @type key :: term()
  @type value :: term()
  @type ttl_ms :: pos_integer()

  @doc """
  Starts the cache. Options:

    * `:name` — optional registered name.
    * `:default_ttl_ms` — default TTL if none is given to `put/4`
      (default 60_000).
  """
  @spec start_link(keyword()) :: Agent.on_start()
  def start_link(opts \\ []) do
    default_ttl = Keyword.get(opts, :default_ttl_ms, 60_000)
    name_opts = Keyword.take(opts, [:name])
    Agent.start_link(fn -> %{default_ttl: default_ttl, entries: %{}} end, name_opts)
  end

  @doc """
  Puts `value` at `key` with a TTL in milliseconds. If `ttl_ms` is nil,
  the default TTL configured at start-up is used.
  """
  @spec put(Agent.agent(), key(), value(), ttl_ms() | nil) :: :ok
  def put(cache, key, value, ttl_ms \\ nil) do
    Agent.update(cache, fn %{default_ttl: default, entries: entries} = state ->
      ttl = ttl_ms || default
      expires_at = now_ms() + ttl
      %{state | entries: Map.put(entries, key, {value, expires_at})}
    end)
  end

  @doc """
  Reads `key`. Returns `{:ok, value}` if present and fresh, `:miss`
  otherwise. A stale entry is deleted on this call (lazy eviction).
  """
  @spec get(Agent.agent(), key()) :: {:ok, value()} | :miss
  def get(cache, key) do
    Agent.get_and_update(cache, fn %{entries: entries} = state ->
      case Map.get(entries, key) do
        nil ->
          {:miss, state}

        {value, expires_at} ->
          if expires_at > now_ms() do
            {{:ok, value}, state}
          else
            {:miss, %{state | entries: Map.delete(entries, key)}}
          end
      end
    end)
  end

  @doc "Returns the number of entries currently stored (fresh or stale)."
  @spec size(Agent.agent()) :: non_neg_integer()
  def size(cache), do: Agent.get(cache, fn %{entries: e} -> map_size(e) end)

  @doc """
  Removes every entry whose `expires_at` is in the past. Returns the
  number of entries removed. Called by the sweeper on a timer.
  """
  @spec sweep(Agent.agent()) :: non_neg_integer()
  def sweep(cache) do
    Agent.get_and_update(cache, fn %{entries: entries} = state ->
      now = now_ms()

      {kept, removed_count} =
        Enum.reduce(entries, {%{}, 0}, fn {k, {_v, exp} = entry}, {acc, n} ->
          if exp > now, do: {Map.put(acc, k, entry), n}, else: {acc, n + 1}
        end)

      {removed_count, %{state | entries: kept}}
    end)
  end

  defp now_ms, do: System.monotonic_time(:millisecond)
end
```

### Step 3: `lib/ttl_cache_agent/sweeper.ex`

```elixir
defmodule TtlCacheAgent.Sweeper do
  @moduledoc """
  Drives periodic `TtlCacheAgent.sweep/1` calls. A thin GenServer whose
  only job is to own a self-scheduled `:sweep` tick.
  """

  use GenServer

  @type opts :: [cache: Agent.agent(), interval_ms: pos_integer(), name: GenServer.name()]

  @spec start_link(opts()) :: GenServer.on_start()
  def start_link(opts) do
    {name_opts, init_opts} = Keyword.split(opts, [:name])
    GenServer.start_link(__MODULE__, init_opts, name_opts)
  end

  @impl true
  def init(opts) do
    cache = Keyword.fetch!(opts, :cache)
    interval = Keyword.get(opts, :interval_ms, 1_000)
    {:ok, schedule(%{cache: cache, interval: interval, timer: nil})}
  end

  @impl true
  def handle_info(:sweep, %{cache: cache} = state) do
    _removed = TtlCacheAgent.sweep(cache)
    {:noreply, schedule(state)}
  end

  def handle_info(_other, state), do: {:noreply, state}

  defp schedule(%{interval: interval} = state) do
    ref = Process.send_after(self(), :sweep, interval)
    %{state | timer: ref}
  end
end
```

### Step 4: `test/ttl_cache_agent_test.exs`

```elixir
defmodule TtlCacheAgentTest do
  use ExUnit.Case, async: true

  alias TtlCacheAgent, as: Cache

  setup do
    {:ok, cache} = Cache.start_link(default_ttl_ms: 30)
    %{cache: cache}
  end

  describe "put/4 and get/2" do
    test "returns fresh values", %{cache: cache} do
      :ok = Cache.put(cache, :k, "v", 1_000)
      assert Cache.get(cache, :k) == {:ok, "v"}
    end

    test "misses on unknown keys", %{cache: cache} do
      assert Cache.get(cache, :nope) == :miss
    end

    test "expires entries past their TTL (lazy eviction)", %{cache: cache} do
      :ok = Cache.put(cache, :k, "v", 10)
      Process.sleep(30)
      assert Cache.get(cache, :k) == :miss
      # Lazy eviction deleted it, so size is back to 0.
      assert Cache.size(cache) == 0
    end
  end

  describe "sweep/1 — periodic eviction" do
    test "removes only stale entries", %{cache: cache} do
      :ok = Cache.put(cache, :fresh, 1, 10_000)
      :ok = Cache.put(cache, :stale_a, 2, 5)
      :ok = Cache.put(cache, :stale_b, 3, 5)

      Process.sleep(25)
      removed = Cache.sweep(cache)

      assert removed == 2
      assert Cache.size(cache) == 1
      assert Cache.get(cache, :fresh) == {:ok, 1}
    end
  end

  describe "Sweeper integration" do
    test "sweeper process periodically purges the cache" do
      {:ok, cache} = Cache.start_link(default_ttl_ms: 5)
      {:ok, _sweeper} =
        TtlCacheAgent.Sweeper.start_link(cache: cache, interval_ms: 10)

      :ok = Cache.put(cache, :a, 1, 5)
      :ok = Cache.put(cache, :b, 2, 5)
      assert Cache.size(cache) == 2

      # Give the sweeper time to run at least once after entries expire.
      Process.sleep(50)

      assert Cache.size(cache) == 0
    end
  end
end
```

### Step 5: Run

```bash
mix test
```

---

## Trade-offs and production gotchas

**1. `Agent` is single-threaded — sweep blocks reads and writes**
While `sweep/1` walks the map, every other `get` and `put` queues up.
For big caches (say, >10_000 entries), consider doing the sweep in
chunks, or moving storage to `:ets` so reads don't serialize through a
single process.

**2. Monotonic time, not wall time**
`System.monotonic_time/1` is the right clock. Using `:os.system_time/1`
or `DateTime.utc_now/0` ties TTL to the wall clock — NTP adjustments can
make entries expire early, late, or never.

**3. Sweep interval is a knob, not a default**
Too frequent: CPU waste on empty sweeps. Too rare: memory grows between
sweeps. A reasonable rule of thumb is `sweep_ms ≈ median_ttl_ms`, but
tune based on churn and peak entry count.

**4. Lazy eviction alone is not enough**
If write-only keys never get read, they live forever. If the sweeper
dies and is not restarted, likewise. Supervise the sweeper
(`Supervisor`/`one_for_one`) so a crash automatically restarts it.

**5. No hit/miss metrics by default**
Real caches report hit ratio. Wire up `:telemetry` events around `get`
so you can see whether the TTL is actually useful — a 99% miss rate
means your TTL is too short or your keys are too sparse.

**6. When NOT to use this design**
- Hot read paths at >10k rps: use `:ets` (with `read_concurrency: true`)
  backed by a sweeper — the agent becomes a bottleneck fast.
- Multi-node caches: `Agent` is local. Use `nebulex`, `cachex`, or
  dedicated infrastructure (Redis) for distributed caching.
- When you need LRU or LFU eviction instead of TTL: this design drops
  only by age, not by usage. `cachex` supports both.

---

## Resources

- [`Agent` — Elixir stdlib](https://hexdocs.pm/elixir/Agent.html)
- [`System.monotonic_time/1`](https://hexdocs.pm/elixir/System.html#monotonic_time/1)
- [`cachex`](https://hexdocs.pm/cachex/) — production-grade caching on `:ets` with TTL, LRU, metrics
- [`nebulex`](https://hexdocs.pm/nebulex/) — multi-tier, distributed cache framework
- ["Why `:timer` is a bottleneck" — Erlang docs](https://www.erlang.org/doc/man/timer.html)
