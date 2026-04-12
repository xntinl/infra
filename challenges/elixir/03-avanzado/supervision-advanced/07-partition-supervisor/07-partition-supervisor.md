# PartitionSupervisor: Eliminating GenServer Serialization Points

## Goal

Replace a single-process rate limiter bottleneck with a `PartitionSupervisor` that shards the workload across N processes (one per scheduler). Each client is deterministically routed to one shard by hashing `client_id`. Operations within a shard are serialized only against each other, not against all other clients.

---

## How `PartitionSupervisor` works

`PartitionSupervisor` (Elixir 1.14+) creates N identical copies of a child spec and registers them as `{supervisor_name, 0}` through `{supervisor_name, N-1}`. Routing uses `:erlang.phash2(key, n_partitions)` to map any term to a partition index.

```elixir
{PartitionSupervisor,
  child_spec: ApiGateway.RateLimiter.Server,
  name:       ApiGateway.RateLimiter.Partitions,
  partitions: System.schedulers_online()
}

# Routing:
GenServer.call(
  {:via, PartitionSupervisor, {ApiGateway.RateLimiter.Partitions, client_id}},
  {:check, client_id, limit, window_ms}
)
```

The same `client_id` always hashes to the same partition -- all operations for a single client are serialized within one process.

---

## Full implementation

### `lib/api_gateway/rate_limiter/server.ex`

Key changes from a single-process server:
1. No fixed name registration -- each partition gets its own name from the PartitionSupervisor.
2. Unnamed ETS tables -- each partition creates its own private ETS table.
3. Via-tuple routing -- public API functions route through the PartitionSupervisor.

```elixir
defmodule ApiGateway.RateLimiter.Server do
  use GenServer

  @cleanup_interval_ms 60_000

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  @doc """
  Checks whether client_id is within its rate limit.
  Routes to the correct partition via PartitionSupervisor.
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

  @doc "Records a request for client_id. Cast -- fire and forget."
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
  # ---------------------------------------------------------------------------

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts)
  end

  @impl true
  def init(_opts) do
    # Each partition creates its own unnamed ETS table.
    # Using :named_table would crash the second partition.
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

    count =
      :ets.lookup(state.table, client_id)
      |> Enum.count(fn {_id, ts} -> ts > cutoff end)

    result =
      if count >= limit do
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
    :ets.select_delete(state.table, [{{:_, :"$1"}, [{:<, :"$1", cutoff}], [true]}])
    Process.send_after(self(), :cleanup, @cleanup_interval_ms)
    {:noreply, state}
  end
end
```

### `lib/api_gateway/rate_limiter/partition_counter.ex`

Collects aggregate stats across all partitions in parallel.

```elixir
defmodule ApiGateway.RateLimiter.PartitionCounter do
  @doc "Returns the total number of active window entries across all partitions."
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

  @doc "Returns partition-level stats for observability."
  @spec partition_stats() :: [map()]
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

### `application.ex` setup

```elixir
# In children list:
{PartitionSupervisor,
  child_spec: ApiGateway.RateLimiter.Server,
  name:       ApiGateway.RateLimiter.Partitions,
  partitions: System.schedulers_online()
}
```

### Tests

```elixir
# test/api_gateway/rate_limiter/server_test.exs
defmodule ApiGateway.RateLimiter.ServerTest do
  use ExUnit.Case, async: false

  alias ApiGateway.RateLimiter.Server

  setup do
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

---

## How it works

1. **Deterministic routing**: `:erlang.phash2(client_id, n_partitions)` always maps the same client to the same partition. All operations for a single client are serialized within one process.

2. **Unnamed ETS tables**: each partition creates its own ETS table without `:named_table` (which would crash the second partition).

3. **Via-tuple**: `{:via, PartitionSupervisor, {supervisor_name, routing_key}}` transparently routes calls to the correct partition. Application code does not need to know the partition count.

4. **Parallel aggregation**: `PartitionCounter` collects per-partition data using `Task.async_stream` for dashboard/monitoring use.

---

## Common production mistakes

**1. Using `:named_table` in each partition**
The second partition crashes because the name is already taken by the first.

**2. Assuming uniform hash distribution with small key spaces**
With 5 distinct clients and 8 partitions, some partitions will receive 0 traffic.

**3. Changing partition count is a breaking change**
`:erlang.phash2("client-x", 8)` and `:erlang.phash2("client-x", 16)` return different values. Existing window entries are in the old partitions.

---

## Resources

- [HexDocs -- PartitionSupervisor](https://hexdocs.pm/elixir/PartitionSupervisor.html)
- [Elixir 1.14 release notes](https://elixir-lang.org/blog/2022/09/01/elixir-v1-14-0-released/)
- [`:erlang.phash2/2` -- Erlang docs](https://www.erlang.org/doc/man/erlang.html#phash2-2)
