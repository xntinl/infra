# ETS Advanced: Concurrency Patterns

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`. The rate limiter (previous exercise) uses a basic ETS
`:bag` table. Now you need to push further: the gateway must track per-route request
statistics under heavy concurrent load, and the ops team wants real-time range queries
on request timestamps without full table scans.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       ├── router.ex
│       ├── rate_limiter/
│       │   └── server.ex
│       └── metrics/
│           ├── counter.ex     # ← you implement this
│           └── event_log.ex   # ← and this
├── test/
│   └── api_gateway/
│       └── metrics/
│           ├── counter_test.exs
│           └── event_log_test.exs
├── bench/
│   └── metrics_bench.exs
└── mix.exs
```

---

## The business problem

Two separate requirements arrived this sprint:

1. **Per-route counters**: the dashboard polls every 5 seconds for `{route, requests, errors, bytes}`.
   With 50k req/s across 200 routes and 8 dashboard pollers, the naive GenServer approach
   (one process serializing all counter updates) saturates its mailbox within minutes.

2. **Event log with time range queries**: the security team needs to query "all requests to
   `/api/payments` between T1 and T2" without loading the entire log into memory.

---

## Why `:write_concurrency` and `:read_concurrency` are not the same flag

`:read_concurrency true` replaces the default exclusive lock with a read-write lock.
Multiple concurrent readers proceed in parallel. It helps when reads dominate writes
by a ratio of at least 10:1. It **adds overhead** when reads and writes are balanced,
because RW lock acquisition is more expensive than an exclusive lock.

`:write_concurrency true` shards the table internally. Writes to different keys can
proceed in parallel if they land on different shards. It degrades for operations that
touch many keys at once (`tab2list`, `first/next` iteration) because those must acquire
all shards.

`decentralized_counters: true` (OTP 23+) takes sharding further for numeric counters:
each scheduler maintains its own copy. Reads must sum all scheduler-local copies, making
reads O(schedulers). Use only when writes dramatically outnumber reads.

The right combination for the gateway counters:

```elixir
:ets.new(:route_metrics, [
  :set,
  :public,
  :named_table,
  {:write_concurrency, true},
  {:decentralized_counters, true}   # 50k writes/s, 5s poll = writes >> reads
])
```

---

## Why `:ordered_set` for time-range queries

`:set` uses a hash table — O(1) lookup but no ordering. To query events between T1 and T2
on a `:set`, you must read every record and filter. That is O(n) and copies the entire table.

`:ordered_set` uses an AVL tree — O(log n) lookup and guaranteed key ordering. Range queries
use `:ets.select/2` with guards on the key field: the tree traversal starts at T1 and stops
at T2. No full scan, no copy.

The cost: 30–40% more memory per entry and O(log n) for individual lookups vs O(1).
Use `:ordered_set` only when your access patterns are temporal or require ordered iteration.

---

## Implementation

### Step 1: `lib/api_gateway/metrics/counter.ex`

```elixir
defmodule ApiGateway.Metrics.Counter do
  @moduledoc """
  Per-route request counters backed by ETS with decentralized_counters.

  Record layout: {route, requests, errors, bytes}
                    ^1      ^2        ^3     ^4
  Position indices are 1-based and matter for update_counter/4.
  """

  use GenServer

  @table :route_metrics

  # ---------------------------------------------------------------------------
  # Public API — reads go directly to ETS, never through the GenServer
  # ---------------------------------------------------------------------------

  @doc """
  Records a completed request. Fire-and-forget via :ets.update_counter.
  Thread-safe: multiple processes can call this concurrently.
  """
  @spec record(String.t(), non_neg_integer(), 200..599) :: :ok
  def record(route, bytes, status_code) do
    error_inc = if status_code >= 400, do: 1, else: 0
    # HINT: :ets.update_counter/4 with a list of {position, increment} tuples
    # HINT: fourth arg is the default record if key doesn't exist: {route, 0, 0, 0}
    # HINT: positions: requests=2, errors=3, bytes=4
    # TODO: implement — this must NOT go through the GenServer
    :ok
  end

  @doc """
  Returns current stats for a route.
  Direct ETS read — no GenServer call.
  """
  @spec get(String.t()) :: %{requests: integer(), errors: integer(), bytes: integer()}
  def get(route) do
    # HINT: :ets.lookup(@table, route) returns [{route, req, err, bytes}] or []
    # TODO: implement
  end

  @doc """
  Returns stats for all routes.
  """
  @spec all() :: [%{route: String.t(), requests: integer(), errors: integer(), bytes: integer()}]
  def all do
    # HINT: :ets.tab2list(@table) |> Enum.map(...)
    # TODO: implement
  end

  # ---------------------------------------------------------------------------
  # GenServer — only responsible for table creation
  # ---------------------------------------------------------------------------

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(_opts) do
    :ets.new(@table, [
      :set,
      :public,
      :named_table,
      {:write_concurrency, true},
      {:decentralized_counters, true}
    ])

    {:ok, %{}}
  end
end
```

### Step 2: `lib/api_gateway/metrics/event_log.ex`

```elixir
defmodule ApiGateway.Metrics.EventLog do
  @moduledoc """
  Append-only event log for gateway requests, backed by ETS :ordered_set.
  Key: {timestamp_microsecond, unique_id} — ensures uniqueness without losing ordering.

  GenServer owns the table. Writes go through the GenServer to serialize inserts.
  Reads use :ets.select directly — no GenServer bottleneck.
  """

  use GenServer

  @table :gateway_event_log

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  @doc """
  Appends a request event. Serialized through the GenServer.
  """
  @spec append(String.t(), 200..599, non_neg_integer()) :: {:ok, integer()}
  def append(route, status_code, bytes) do
    GenServer.call(__MODULE__, {:append, route, status_code, bytes})
  end

  @doc """
  Returns all events in the time range [from_us, to_us] (microseconds).
  Direct ETS select — no GenServer bottleneck.
  """
  @spec range(integer(), integer()) :: list()
  def range(from_us, to_us) do
    # HINT: build a match spec manually — fun2ms can't capture runtime variables
    # Match spec pattern: {{:"$1", :"$2"}, :"$3", :"$4", :"$5"}
    # Guard: [{:>=, :"$1", from_us}, {:"=<", :"$1", to_us}]
    # Return: [{{:"$1", :"$2"}, :"$3", :"$4", :"$5"}]
    # TODO: implement
  end

  @doc """
  Returns events in [from_us, to_us] filtered by route.
  Direct ETS select.
  """
  @spec range_by_route(integer(), integer(), String.t()) :: list()
  def range_by_route(from_us, to_us, route) do
    # HINT: same as range/2 but add {:==, :"$3", route} to the guard list
    # TODO: implement
  end

  @doc """
  Deletes events older than max_age_seconds.
  Uses :ets.select_delete — operates directly in ETS, no memory copy.
  """
  @spec purge_older_than(pos_integer()) :: {:purged, non_neg_integer()}
  def purge_older_than(max_age_seconds) do
    GenServer.call(__MODULE__, {:purge, max_age_seconds})
  end

  def size, do: :ets.info(@table, :size)

  # ---------------------------------------------------------------------------
  # GenServer callbacks
  # ---------------------------------------------------------------------------

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(_opts) do
    :ets.new(@table, [:ordered_set, :protected, :named_table])
    {:ok, %{}}
  end

  @impl true
  def handle_call({:append, route, status_code, bytes}, _from, state) do
    ts = System.os_time(:microsecond)
    # unique_integer guarantees uniqueness when two events share the same microsecond
    id = :erlang.unique_integer([:monotonic, :positive])
    :ets.insert(@table, {{ts, id}, route, status_code, bytes})
    {:reply, {:ok, ts}, state}
  end

  @impl true
  def handle_call({:purge, max_age_seconds}, _from, state) do
    cutoff = System.os_time(:microsecond) - max_age_seconds * 1_000_000
    # HINT: :ets.fun2ms can be used here because cutoff is known at call time
    # but it's compile-time only. Build the match spec manually instead.
    ms = [
      {{{:"$1", :_}, :_, :_, :_}, [{:<, :"$1", cutoff}], [true]}
    ]
    deleted = :ets.select_delete(@table, ms)
    {:reply, {:purged, deleted}, state}
  end
end
```

### Step 3: Given tests — must pass without modification

```elixir
# test/api_gateway/metrics/counter_test.exs
defmodule ApiGateway.Metrics.CounterTest do
  use ExUnit.Case, async: false

  alias ApiGateway.Metrics.Counter

  setup do
    :ets.delete_all_objects(:route_metrics)
    :ok
  end

  describe "record/3 and get/1" do
    test "records requests for a route" do
      Counter.record("/api/users", 512, 200)
      Counter.record("/api/users", 256, 200)
      Process.sleep(5)  # allow decentralized_counters to coalesce

      stats = Counter.get("/api/users")
      assert stats.requests == 2
      assert stats.bytes == 768
      assert stats.errors == 0
    end

    test "counts error responses separately" do
      Counter.record("/api/payments", 100, 500)
      Counter.record("/api/payments", 200, 200)
      Process.sleep(5)

      stats = Counter.get("/api/payments")
      assert stats.requests == 2
      assert stats.errors == 1
    end

    test "returns zero stats for unknown route" do
      stats = Counter.get("/unknown")
      assert stats.requests == 0
      assert stats.errors == 0
      assert stats.bytes == 0
    end
  end

  describe "concurrent writes" do
    test "100 concurrent writers produce correct totals" do
      tasks =
        for _ <- 1..100 do
          Task.async(fn -> Counter.record("/api/concurrent", 100, 200) end)
        end

      Task.await_many(tasks, 5_000)
      Process.sleep(20)

      stats = Counter.get("/api/concurrent")
      assert stats.requests == 100
      assert stats.bytes == 10_000
    end
  end
end
```

```elixir
# test/api_gateway/metrics/event_log_test.exs
defmodule ApiGateway.Metrics.EventLogTest do
  use ExUnit.Case, async: false

  alias ApiGateway.Metrics.EventLog

  setup do
    EventLog.purge_older_than(0)
    :ok
  end

  describe "append/3 and range/2" do
    test "appended events appear in range queries" do
      t_before = System.os_time(:microsecond)
      {:ok, _} = EventLog.append("/api/users", 200, 512)
      {:ok, _} = EventLog.append("/api/orders", 404, 128)
      t_after = System.os_time(:microsecond)

      results = EventLog.range(t_before, t_after)
      assert length(results) == 2
    end

    test "events outside the range are excluded" do
      t_before = System.os_time(:microsecond) - 10_000_000
      t_cutoff = System.os_time(:microsecond) - 5_000_000

      # Insert an old event via direct ETS (simulating a past event)
      :ets.insert(:gateway_event_log, {{t_before, 99999}, "/old", 200, 0})

      {:ok, _} = EventLog.append("/api/new", 200, 100)

      old_events = EventLog.range(t_before - 1, t_cutoff)
      assert length(old_events) == 1

      new_events = EventLog.range(t_cutoff + 1, System.os_time(:microsecond))
      assert length(new_events) == 1
    end
  end

  describe "range_by_route/3" do
    test "filters events by route within the time range" do
      t_before = System.os_time(:microsecond)
      EventLog.append("/api/users", 200, 100)
      EventLog.append("/api/orders", 200, 100)
      EventLog.append("/api/users", 500, 100)
      t_after = System.os_time(:microsecond)

      results = EventLog.range_by_route(t_before, t_after, "/api/users")
      assert length(results) == 2
    end
  end

  describe "purge_older_than/1" do
    test "removes old events and leaves recent ones" do
      EventLog.append("/api/users", 200, 100)
      {:purged, n} = EventLog.purge_older_than(0)
      assert n >= 1
    end
  end
end
```

### Step 4: Run the tests

```bash
mix test test/api_gateway/metrics/ --trace
```

### Step 5: Benchmark — GenServer vs ETS for concurrent reads

```elixir
# bench/metrics_bench.exs
alias ApiGateway.Metrics.Counter

# Pre-populate some routes
for i <- 1..50 do
  Counter.record("/api/route_#{i}", 512, 200)
end

Benchee.run(
  %{
    "Counter.get — direct ETS read" => fn ->
      Counter.get("/api/route_#{:rand.uniform(50)}")
    end,
    "Counter.all — full table scan" => fn ->
      Counter.all()
    end
  },
  parallel: 50,
  warmup: 2,
  time: 5,
  formatters: [Benchee.Formatters.Console]
)
```

```bash
mix run bench/metrics_bench.exs
```

**Expected**: `Counter.get` should complete in under 5µs at p99 under 50 concurrent readers.
If you see serialization (latency growing with concurrency), verify that `record/3`
does NOT call `GenServer.call` — it must write directly to ETS.

---

## Trade-off analysis

Fill in this table based on your implementation and benchmark results:

| Aspect | `:set` + `write_concurrency` | `:ordered_set` | GenServer map |
|--------|------------------------------|---------------|--------------|
| Lookup by key | O(1) | O(log n) | O(1) with map |
| Range query | O(n) full scan | O(log n) tree walk | O(n) with Enum.filter |
| Concurrent reads | No bottleneck | No bottleneck | Serialized by mailbox |
| Memory overhead | Low | +30-40% vs `:set` | In-process heap |
| `decentralized_counters` | Supported | Not supported | N/A |
| Write ordering guarantee | None | None | FIFO mailbox |

Reflection: the event log uses `:protected` access while counters use `:public`.
What does that mean for who can write to each table, and why is it the right choice?

---

## Common production mistakes

**1. Using `read_concurrency: true` on tables you iterate frequently**
`tab2list/1`, `first/1`, `next/2` acquire a full table lock regardless of `read_concurrency`.
If your workload mixes point lookups with full scans, benchmark both flag combinations.

**2. Applying `decentralized_counters` when reads are frequent**
With `decentralized_counters`, reading the current counter value requires summing
scheduler-local copies — O(schedulers). For a dashboard polling every 5 seconds
this is fine. For a rate limiter checking every request, it adds up.

**3. Using `:ordered_set` for point lookups that don't need ordering**
O(log n) vs O(1) is a 3–5x difference at 1M entries. Only use `:ordered_set` when
range queries or ordered iteration are part of the actual access pattern.

**4. Writing match specs by hand instead of building them programmatically**
`fun2ms` is compile-time only and cannot capture runtime variables. For dynamic
range queries (where `from_us` and `to_us` come from function arguments), you must
build the match spec as a list of tuples at runtime. See `range/2` above.

**5. Creating the ETS table in a process that can die**
The process that creates an ETS table owns it. When the owner dies, the table is
destroyed. Always create tables in supervised, long-lived processes (Application or
GenServer in the supervision tree).

---

## Resources

- [`:ets` documentation — Erlang/OTP](https://www.erlang.org/doc/man/ets.html) — read the `type`, `access`, and `write_concurrency` sections
- [Erlang efficiency guide — ETS](https://www.erlang.org/doc/efficiency_guide/tablesDatabases.html) — when to use each table type
- [`:ets.fun2ms` / ms_transform](https://www.erlang.org/doc/man/ms_transform.html) — compile-time match spec generation
- [Erlang in Anger — Fred Hebert](https://www.erlang-in-anger.com/) — production ETS patterns (free PDF)
