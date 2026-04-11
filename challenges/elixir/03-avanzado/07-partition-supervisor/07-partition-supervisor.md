# PartitionSupervisor: Eliminating GenServer Serialization Points

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`. The rate limiter (exercise — ETS + sliding window)
is fast for reads, but all writes still go through a single `RateLimiter.Server`
GenServer. Under load testing at 50,000 req/s with 10,000 distinct clients,
`:erlang.process_info(pid, :message_queue_len)` on the server shows a backlog of
8,000+ messages. The GenServer is the bottleneck.

The fix: shard the rate limiter across N processes using `PartitionSupervisor`
(introduced in Elixir 1.14). Each client is deterministically routed to one shard
by hashing `client_id`. Reads and writes within a shard are serialized only against
each other, not against all other clients.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       ├── router.ex
│       └── rate_limiter/
│           ├── server.ex          # ← rewrite to support sharding
│           ├── window.ex
│           └── partition_counter.ex  # ← you implement this (exercise 2)
├── test/
│   └── api_gateway/
│       └── rate_limiter/
│           ├── server_test.exs         # given tests — must pass
│           └── partition_bench_test.exs # benchmark test — run last
├── bench/
│   └── partition_bench.exs
└── mix.exs
```

---

## How `PartitionSupervisor` works

`PartitionSupervisor` creates N identical copies of a child spec and registers them
as `{supervisor_name, 0}` through `{supervisor_name, N-1}`. Routing uses
`:erlang.phash2(key, n_partitions)` to map any Elixir term to a partition index.

```elixir
# In application.ex
{PartitionSupervisor,
  child_spec: ApiGateway.RateLimiter.Server,
  name:       ApiGateway.RateLimiter.Partitions,
  partitions: System.schedulers_online()
}

# Routing from a client call:
GenServer.call(
  {:via, PartitionSupervisor, {ApiGateway.RateLimiter.Partitions, client_id}},
  {:check, client_id, limit, window_ms}
)
```

The same `client_id` always hashes to the same partition — this is the key property.
All operations for a single client are serialized within one process, preserving
correctness. Operations for different clients in different partitions run in parallel.

---

## When to use `PartitionSupervisor`

The decisive question: does the workload have a **natural sharding key** where all
operations for a given key can safely be isolated to one process?

| Workload | Good fit? | Reason |
|----------|-----------|--------|
| Rate limiting per client_id | Yes | Each client's state is independent |
| Global request counter | No | Requires cross-shard coordination |
| Cache by cache_key | Yes | Locality of reference per key |
| Circuit breaker per service | Yes | State is per-service |
| Distributed lock (one mutex) | No | Cannot shard a single mutex |
| Session state per user_id | Yes | State is per-user |

Anti-pattern: using `PartitionSupervisor` when keys have low cardinality (e.g.,
only 3 possible values). With 8 partitions and 3 distinct keys, 5 partitions receive
zero traffic. The overhead of extra processes exceeds the benefit.

---

## Implementation

### Step 1: Update `application.ex`

Replace the single `RateLimiter.Server` with a `PartitionSupervisor` that creates
N copies (one per scheduler). Each partition is an independent GenServer with its own
ETS table.

```elixir
# In lib/api_gateway/application.ex, inside CoreSupervisor children:

# Replace the single RateLimiter.Server with a PartitionSupervisor:
{PartitionSupervisor,
  child_spec: ApiGateway.RateLimiter.Server,
  name:       ApiGateway.RateLimiter.Partitions,
  partitions: System.schedulers_online()
}
```

### Step 2: Rewrite `lib/api_gateway/rate_limiter/server.ex`

The key changes from the original single-process server:

1. **No fixed name registration**: each partition gets its own name from the
   PartitionSupervisor, so `start_link` must NOT register with a fixed `name:`.
2. **Unnamed ETS tables**: each partition creates its own private ETS table. Using
   `:named_table` would crash the second partition because the name is already taken.
3. **Via-tuple routing**: public API functions route through the PartitionSupervisor
   using `{:via, PartitionSupervisor, {supervisor_name, routing_key}}`.

```elixir
defmodule ApiGateway.RateLimiter.Server do
  use GenServer

  @cleanup_interval_ms 60_000

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  @doc """
  Checks whether client_id is within its rate limit.
  Routes to the correct partition automatically via PartitionSupervisor.
  Returns {:allow, remaining} or {:deny, retry_after_ms}.
  """
  @spec check(String.t(), pos_integer(), pos_integer()) ::
          {:allow, non_neg_integer()} | {:deny, pos_integer()}
  def check(client_id, limit, window_ms) do
    GenServer.call(
      {:via, PartitionSupervisor, {ApiGateway.RateLimiter.Partitions, client_id}},
      {:check, client_id, limit, window_ms}
    )
  end

  @doc """
  Records a request for client_id. Cast — fire and forget.
  Routes to the same partition as check/3.
  """
  @spec record(String.t()) :: :ok
  def record(client_id) do
    GenServer.cast(
      {:via, PartitionSupervisor, {ApiGateway.RateLimiter.Partitions, client_id}},
      {:record, client_id}
    )
    :ok
  end

  # ---------------------------------------------------------------------------
  # GenServer lifecycle
  # Each partition manages state for the clients that hash to it.
  # ---------------------------------------------------------------------------

  def start_link(opts \\ []) do
    # Do NOT register with a fixed name here.
    # PartitionSupervisor manages the naming via the partition index.
    GenServer.start_link(__MODULE__, opts)
  end

  @impl true
  def init(_opts) do
    # Each partition creates its own unnamed ETS table.
    # Using :named_table would crash the second partition because the
    # name would already be taken by the first.
    table = :ets.new(:rate_limiter_shard, [:bag, :public])
    Process.send_after(self(), :cleanup, @cleanup_interval_ms)
    {:ok, %{table: table}}
  end

  # ---------------------------------------------------------------------------
  # Callbacks
  # ---------------------------------------------------------------------------

  @impl true
  def handle_call({:check, client_id, limit, window_ms}, _from, state) do
    now = System.monotonic_time(:millisecond)
    cutoff = now - window_ms

    # Count only timestamps within the current window
    count =
      :ets.lookup(state.table, client_id)
      |> Enum.count(fn {_id, ts} -> ts > cutoff end)

    result =
      if count >= limit do
        # Find the oldest timestamp in the window to calculate retry_after
        oldest_in_window =
          :ets.lookup(state.table, client_id)
          |> Enum.filter(fn {_id, ts} -> ts > cutoff end)
          |> Enum.min_by(fn {_id, ts} -> ts end, fn -> {nil, now} end)
          |> elem(1)

        retry_after = window_ms - (now - oldest_in_window)
        {:deny, max(retry_after, 1)}
      else
        remaining = limit - count
        {:allow, remaining}
      end

    {:reply, result, state}
  end

  @impl true
  def handle_cast({:record, client_id}, state) do
    ts = System.monotonic_time(:millisecond)
    :ets.insert(state.table, {client_id, ts})
    {:noreply, state}
  end

  @impl true
  def handle_info(:cleanup, state) do
    cutoff = System.monotonic_time(:millisecond) - 3_600_000

    # Delete entries older than 1 hour. :ets.select_delete with a match spec
    # efficiently removes matching entries without building an intermediate list.
    :ets.select_delete(state.table, [{{:_, :"$1"}, [{:<, :"$1", cutoff}], [true]}])

    Process.send_after(self(), :cleanup, @cleanup_interval_ms)
    {:noreply, state}
  end
end
```

### Step 3: `lib/api_gateway/rate_limiter/partition_counter.ex`

The gateway dashboard needs aggregate stats across all partitions. This module
collects per-partition data in parallel using `Task.async_stream`.

```elixir
defmodule ApiGateway.RateLimiter.PartitionCounter do
  @doc """
  Returns the total number of active window entries across all partitions.
  Each partition contributes its local ETS table size.
  Uses Task.async_stream for parallel collection.
  """
  @spec total_entries() :: non_neg_integer()
  def total_entries do
    n = PartitionSupervisor.partitions(ApiGateway.RateLimiter.Partitions)

    0..(n - 1)
    |> Task.async_stream(fn partition_index ->
      pid = GenServer.whereis(
        {:via, PartitionSupervisor, {ApiGateway.RateLimiter.Partitions, partition_index}}
      )
      state = :sys.get_state(pid)
      :ets.info(state.table, :size)
    end, max_concurrency: n, timeout: 5_000)
    |> Enum.reduce(0, fn {:ok, count}, acc -> acc + count end)
  end

  @doc """
  Returns partition-level stats for observability/debugging.
  """
  @spec partition_stats() :: [%{partition: integer(), entries: integer(), pid: pid(), queue_len: integer()}]
  def partition_stats do
    n = PartitionSupervisor.partitions(ApiGateway.RateLimiter.Partitions)

    Enum.map(0..(n - 1), fn index ->
      pid = GenServer.whereis(
        {:via, PartitionSupervisor, {ApiGateway.RateLimiter.Partitions, index}}
      )
      state = :sys.get_state(pid)
      entries = :ets.info(state.table, :size)
      {:message_queue_len, queue_len} = Process.info(pid, :message_queue_len)

      %{partition: index, pid: pid, entries: entries, queue_len: queue_len}
    end)
  end
end
```

### Step 4: Given tests — must pass without modification

```elixir
# test/api_gateway/rate_limiter/server_test.exs
defmodule ApiGateway.RateLimiter.ServerTest do
  use ExUnit.Case, async: false

  alias ApiGateway.RateLimiter.Server

  setup do
    # Clear all partition tables between tests
    n = PartitionSupervisor.partitions(ApiGateway.RateLimiter.Partitions)
    for i <- 0..(n - 1) do
      pid = GenServer.whereis(
        {:via, PartitionSupervisor, {ApiGateway.RateLimiter.Partitions, i}}
      )
      state = :sys.get_state(pid)
      :ets.delete_all_objects(state.table)
    end
    :ok
  end

  describe "routing" do
    test "same client_id always routes to same partition" do
      # Record on one call path, check on another — must find the records
      for _ <- 1..5, do: Server.record("routing-test")
      Process.sleep(20)
      assert {:allow, 5} = Server.check("routing-test", 10, 60_000)
    end

    test "different clients can have independent limits" do
      for _ <- 1..10, do: Server.record("client-a")
      for _ <- 1..3,  do: Server.record("client-b")
      Process.sleep(20)

      assert {:deny, _}   = Server.check("client-a", 10, 60_000)
      assert {:allow, 7}  = Server.check("client-b", 10, 60_000)
    end
  end

  describe "sliding window semantics" do
    test "expired timestamps do not count" do
      n = PartitionSupervisor.partitions(ApiGateway.RateLimiter.Partitions)
      partition = :erlang.phash2("expired-client", n)
      pid = GenServer.whereis(
        {:via, PartitionSupervisor, {ApiGateway.RateLimiter.Partitions, partition}}
      )
      state = :sys.get_state(pid)

      old_ts = System.monotonic_time(:millisecond) - 90_000
      for _ <- 1..10 do
        :ets.insert(state.table, {"expired-client", old_ts})
      end

      assert {:allow, _} = Server.check("expired-client", 10, 60_000)
    end

    test "new client has full limit available" do
      assert {:allow, 50} = Server.check("brand-new-#{:rand.uniform(10_000)}", 50, 60_000)
    end
  end

  describe "partition stats" do
    test "total_entries returns a non-negative integer" do
      Server.record("stat-client")
      Process.sleep(20)
      assert ApiGateway.RateLimiter.PartitionCounter.total_entries() >= 1
    end

    test "partition_stats returns one entry per partition" do
      n = PartitionSupervisor.partitions(ApiGateway.RateLimiter.Partitions)
      stats = ApiGateway.RateLimiter.PartitionCounter.partition_stats()
      assert length(stats) == n
    end
  end
end
```

### Step 5: Run the tests

```bash
mix test test/api_gateway/rate_limiter/server_test.exs --trace
```

### Step 6: Benchmark single vs partitioned

```elixir
# bench/partition_bench.exs
alias ApiGateway.RateLimiter.Server

n_clients  = 1_000
n_requests = 200

clients = for i <- 1..n_clients, do: "bench-client-#{i}"

{time_us, _} =
  :timer.tc(fn ->
    clients
    |> Task.async_stream(
      fn client ->
        for _ <- 1..n_requests do
          Server.record(client)
          Server.check(client, 10_000, 60_000)
        end
      end,
      max_concurrency: n_clients,
      timeout: 60_000
    )
    |> Stream.run()
  end)

total_ops = n_clients * n_requests * 2
throughput = total_ops / (time_us / 1_000_000)

IO.puts("Partitions:  #{PartitionSupervisor.partitions(ApiGateway.RateLimiter.Partitions)}")
IO.puts("Total ops:   #{total_ops}")
IO.puts("Duration:    #{Float.round(time_us / 1_000, 1)} ms")
IO.puts("Throughput:  #{Float.round(throughput, 0)} ops/s")
```

```bash
mix run bench/partition_bench.exs
```

**Expected result**: throughput scales roughly linearly with partition count up to
`System.schedulers_online()`. Beyond that, scheduling overhead dominates.

---

## Trade-off analysis

| Aspect | Single GenServer | N Partitions | Notes |
|--------|-----------------|--------------|-------|
| Max throughput | 1× (serialized) | ~N× (parallel) | Measured with benchmark |
| Consistency | Strong per-client | Strong per-client | Same key → same shard |
| Global state | Trivial | Requires aggregation | See `PartitionCounter` |
| Observability | One process to inspect | N processes to inspect | Use `partition_stats/0` |
| Changing N partitions | N/A | Requires state migration | Hash changes on resize |
| Memory | One ETS table | N ETS tables | Pro-rated per partition |

Reflection question: if you change `partitions: 8` to `partitions: 16` in a running
system, what happens to the existing window entries in each partition's ETS table?
Why is this a problem, and how would you safely resize in production?

---

## Common production mistakes

**1. Using `:named_table` in each partition**
Each partition is a separate process. If `init/1` calls `:ets.new(:rate_limiter, [:named_table, ...])`,
the second partition to start crashes with `{:badarg, ...}` because the name is already
taken. Use unnamed tables and store the ref in state.

**2. Assuming uniform hash distribution with small key spaces**
`:erlang.phash2/2` distributes evenly over large key sets. With only 5 distinct clients
and 8 partitions, some partitions will receive 0 traffic. The bottleneck simply moves
to the 2–3 hot partitions. Measure actual distribution before declaring victory.

**3. Forgetting that changing partition count is a breaking change**
`:erlang.phash2("client-x", 8)` and `:erlang.phash2("client-x", 16)` return different
values. After a resize, all clients route to different partitions. Their existing window
entries are in the old partitions and will not be found. For stateful sharding, treat
a partition count change as a data migration.

**4. Using the routing key as the process identity**
The routing key (e.g., `client_id`) determines *which* partition receives the message.
The partition process does not "know" its routing key — it handles all clients that
hash to its index. The client_id must be in the message payload, not inferred from
how the process was started.

---

## Resources

- [HexDocs — PartitionSupervisor](https://hexdocs.pm/elixir/PartitionSupervisor.html)
- [Elixir 1.14 release notes — PartitionSupervisor introduction](https://elixir-lang.org/blog/2022/09/01/elixir-v1-14-0-released/)
- [`:erlang.phash2/2` — Erlang docs](https://www.erlang.org/doc/man/erlang.html#phash2-2)
- [Process.info/2 — HexDocs](https://hexdocs.pm/elixir/Process.html#info/2) — message queue inspection
