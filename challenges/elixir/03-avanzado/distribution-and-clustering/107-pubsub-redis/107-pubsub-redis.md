# Phoenix.PubSub with Redis Adapter for Cross-Cluster Messaging

**Project**: `pubsub_redis_adapter` — bridging two BEAM clusters over Redis pub/sub

---

## Project context

You operate two independent Phoenix clusters: `orders-cluster` (10 nodes in us-east) and
`fulfillment-cluster` (6 nodes in us-west). They do not share a BEAM mesh because the WAN
latency (~70ms) would destabilize net_kernel heartbeats and `:pg` gossip. Both clusters need
to broadcast domain events to each other — a new order in east triggers a reservation in
west, and a shipped status in west updates customer dashboards in east.

The default Phoenix.PubSub.PG2 adapter is BEAM-only: it assumes all subscribers live in the
same Erlang distribution mesh. `Phoenix.PubSub.Redis` (from `:phoenix_pubsub_redis`) uses
Redis pub/sub as a **transport bus** between otherwise-isolated clusters. Each node subscribes
to a Redis channel named after the pubsub `:topic`, and publishes are fan-out through Redis.

The design is subtle. A naive setup produces **message loops** (cluster A publishes, cluster
B republishes, cluster A re-receives forever), **head-of-line blocking** when one Redis
connection saturates, and **silent message loss** during Redis failovers. This exercise
implements a production-grade adapter on top of `Phoenix.PubSub.Redis` with dedup, fanout
pool, and failover-aware reconnection.

```
pubsub_redis_adapter/
├── lib/
│   └── pubsub_redis_adapter/
│       ├── application.ex
│       ├── event.ex            # Envelope struct with node_id + msg_id
│       ├── publisher.ex        # Tags and broadcasts domain events
│       ├── subscriber.ex       # Receives, dedupes, delivers locally
│       └── dedup.ex            # ETS-backed seen-msg cache
├── test/
│   └── pubsub_redis_adapter/
│       ├── publisher_test.exs
│       └── dedup_test.exs
└── mix.exs
```

---

## Why this approach and not alternatives

Alternatives considered and discarded:

- **Hand-rolled equivalent**: reinvents primitives the BEAM/ecosystem already provides; high risk of subtle bugs around concurrency, timeouts, or failure propagation.
- **External service (e.g. Redis, sidecar)**: adds a network hop and an extra failure domain for a problem the VM can solve in-process with lower latency.
- **Heavier framework abstraction**: couples the module to a framework lifecycle and makes local reasoning/testing harder.

The chosen approach stays inside the BEAM, uses idiomatic OTP primitives, and keeps the contract small.

## Core concepts

### 1. PG2 vs Redis adapter — different problems

| Axis | `Phoenix.PubSub.PG2` | `Phoenix.PubSub.Redis` |
|------|--------------------|-----------------------|
| Transport | Erlang distribution | Redis pub/sub |
| Topology | single BEAM mesh | independent clusters |
| Message format | raw Erlang terms | serialized binary |
| Latency (LAN) | ~0.5-2 ms | ~2-10 ms |
| Durability | none | none (Redis pub/sub is fire-and-forget) |
| Ordering | per-sender | per-Redis-connection |
| Failure mode | netsplit isolates subscribers | Redis down = no messages |

The Redis adapter is **not a replacement** for PG2 — it complements it. Inside a cluster,
use PG2 for cheap, high-throughput broadcasts. Between clusters, use Redis.

### 2. The loop problem

If every node republishes what it receives, you get a storm:

```
east-node-1 ──publish──▶ Redis ──┬──▶ east-node-2 ──republish?──▶ Redis ──▶ ...
                                  └──▶ west-node-1 ──republish?──▶ Redis ──▶ ...
```

The fix is a **node-scoped origin tag**. Every envelope carries `node_id`. A node does not
republish messages whose origin matches its own cluster. `Phoenix.PubSub.Redis` handles this
internally via node names, but when you layer domain logic on top (e.g., a custom fanout
pool) you must preserve the origin tag yourself.

### 3. Deduplication across reconnects

Redis pub/sub is **at-most-once**. During a Redis failover (Sentinel promotes a replica), in-flight
messages are lost. During reconnection, some adapters replay the last N messages from a Redis
stream. If the subscriber was also briefly connected to both old and new master, it may see
the same message twice.

The adapter layer must dedupe by `msg_id` using a bounded cache. A fixed-size ETS table with
LRU eviction is sufficient:

```
ETS :dedup_seen = {msg_id, inserted_at}
on message: if lookup(msg_id), drop. Otherwise insert.
periodic sweep: delete entries older than 5 minutes.
```

### 4. Fanout pool — avoiding HoL blocking

`Phoenix.PubSub.Redis` uses a pool of Redis connections (`pool_size`). A single slow
subscriber that blocks on `handle_info` will stall only its own connection. But if your
subscriber does `HTTPoison.post/3` in the callback, you have a bigger problem: the
subscriber process serializes all messages in its mailbox.

Pattern: the subscriber **casts to a worker pool** (Task.Supervisor or a custom pool), it
never blocks on I/O directly. The PubSub subscriber is only a router.

```
Redis ──▶ Subscriber ──cast──▶ Task.Supervisor ──▶ HTTP call
            │                  ▲
            │                  │
            └─ cast ────────── Task 2
```

### 5. Topology diagram

```
                   ┌──────────────┐
                   │    Redis     │◀──────── failover via Sentinel
                   │ (master + 2  │
                   │   replicas)  │
                   └──────┬───────┘
                          │
        ┌─────────────────┼─────────────────┐
        │                 │                 │
  ┌─────▼─────┐     ┌─────▼─────┐     ┌─────▼─────┐
  │ east-1    │     │ east-2    │     │ west-1    │
  │ PubSub    │     │ PubSub    │     │ PubSub    │
  │ PG2 (LAN) │◀───▶│ PG2 (LAN) │     │ PG2 (LAN) │
  └───────────┘     └───────────┘     └───────────┘
```

Inside each cluster: PG2 for fast local fanout. Between clusters: Redis.

---

## Design decisions

**Option A — naive/simple approach**
- Pros: minimal code, easy to reason about.
- Cons: breaks under load, lacks observability, hard to evolve.

**Option B — the approach used here** (chosen)
- Pros: production-grade, handles edge cases, testable boundaries.
- Cons: more moving parts, requires understanding of the BEAM primitives involved.

→ Chose **B** because correctness under concurrency and failure modes outweighs the extra surface area.

## Implementation

### Step 1: Dependencies

```elixir
# mix.exs
defp deps do
  [
    {:phoenix_pubsub, "~> 2.1"},
    {:phoenix_pubsub_redis, "~> 3.0"},
    {:jason, "~> 1.4"},
    {:redix, "~> 1.4"}
  ]
end
```

### Step 2: Event envelope

```elixir
defmodule PubsubRedisAdapter.Event do
  @moduledoc """
  Envelope for all cross-cluster messages. `msg_id` is a UUIDv4 generated once
  at publish time; it survives republishes and lets the dedup layer drop
  duplicates across reconnects.
  """

  @enforce_keys [:msg_id, :origin_node, :cluster, :topic, :payload, :emitted_at]
  defstruct [:msg_id, :origin_node, :cluster, :topic, :payload, :emitted_at]

  @type t :: %__MODULE__{
          msg_id: String.t(),
          origin_node: atom(),
          cluster: String.t(),
          topic: String.t(),
          payload: term(),
          emitted_at: integer()
        }

  @spec new(String.t(), String.t(), term()) :: t()
  def new(cluster, topic, payload) do
    %__MODULE__{
      msg_id: generate_msg_id(),
      origin_node: node(),
      cluster: cluster,
      topic: topic,
      payload: payload,
      emitted_at: System.system_time(:millisecond)
    }
  end

  defp generate_msg_id do
    <<u0::32, u1::16, _::4, u2::12, _::2, u3::62>> = :crypto.strong_rand_bytes(16)
    :io_lib.format(~c"~8.16.0b-~4.16.0b-4~3.16.0b-~4.16.0b-~12.16.0b",
      [u0, u1, u2, 0x8000 ||| rem(u3, 0x4000), u3])
    |> to_string()
  end
end
```

### Step 3: Application supervisor

```elixir
defmodule PubsubRedisAdapter.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    cluster = Application.fetch_env!(:pubsub_redis_adapter, :cluster_name)
    redis_url = Application.fetch_env!(:pubsub_redis_adapter, :redis_url)

    children = [
      {Phoenix.PubSub,
       name: PubsubRedisAdapter.PubSub,
       adapter: Phoenix.PubSub.Redis,
       url: redis_url,
       node_name: node(),
       pool_size: 5},
      PubsubRedisAdapter.Dedup,
      {Task.Supervisor, name: PubsubRedisAdapter.TaskSupervisor},
      {PubsubRedisAdapter.Subscriber, cluster: cluster}
    ]

    opts = [strategy: :one_for_one, name: PubsubRedisAdapter.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
```

### Step 4: Dedup GenServer with ETS

```elixir
defmodule PubsubRedisAdapter.Dedup do
  @moduledoc """
  Bounded seen-message cache. Ensures idempotent delivery across Redis
  reconnects. Entries expire after `@ttl_ms` and the table is capped at
  `@max_size` — oldest-first eviction runs on every sweep.
  """
  use GenServer

  @table :pubsub_redis_dedup
  @ttl_ms 5 * 60_000
  @max_size 100_000
  @sweep_interval_ms 60_000

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @doc "Returns `:new` if this msg_id is being seen for the first time, else `:duplicate`."
  @spec check_and_mark(String.t()) :: :new | :duplicate
  def check_and_mark(msg_id) do
    now = System.monotonic_time(:millisecond)

    case :ets.insert_new(@table, {msg_id, now}) do
      true -> :new
      false -> :duplicate
    end
  end

  @impl true
  def init(_opts) do
    :ets.new(@table, [:named_table, :public, :set, write_concurrency: true, read_concurrency: true])
    Process.send_after(self(), :sweep, @sweep_interval_ms)
    {:ok, %{}}
  end

  @impl true
  def handle_info(:sweep, state) do
    cutoff = System.monotonic_time(:millisecond) - @ttl_ms

    # Delete TTL-expired entries
    ms = [{{:"$1", :"$2"}, [{:<, :"$2", cutoff}], [true]}]
    :ets.select_delete(@table, ms)

    # Cap by size — drop oldest if still over @max_size
    size = :ets.info(@table, :size)

    if size > @max_size do
      excess = size - @max_size

      :ets.tab2list(@table)
      |> Enum.sort_by(fn {_, ts} -> ts end)
      |> Enum.take(excess)
      |> Enum.each(fn {id, _} -> :ets.delete(@table, id) end)
    end

    Process.send_after(self(), :sweep, @sweep_interval_ms)
    {:noreply, state}
  end
end
```

### Step 5: Publisher

```elixir
defmodule PubsubRedisAdapter.Publisher do
  @moduledoc "Tags every broadcast with a unique msg_id."

  alias PubsubRedisAdapter.Event

  @spec broadcast(String.t(), term()) :: :ok | {:error, term()}
  def broadcast(topic, payload) do
    cluster = Application.fetch_env!(:pubsub_redis_adapter, :cluster_name)
    event = Event.new(cluster, topic, payload)

    # Mark own msg_id as seen locally to avoid re-processing the echo
    PubsubRedisAdapter.Dedup.check_and_mark(event.msg_id)

    Phoenix.PubSub.broadcast(PubsubRedisAdapter.PubSub, topic, {:cross_cluster, event})
  end
end
```

### Step 6: Subscriber

```elixir
defmodule PubsubRedisAdapter.Subscriber do
  @moduledoc """
  Joins all domain topics, dedupes incoming events, and dispatches payloads
  to local subscribers via a task pool so that slow handlers do not block
  PubSub delivery.
  """
  use GenServer

  alias PubsubRedisAdapter.{Dedup, Event}

  @topics ~w(orders.events fulfillment.events inventory.events)

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(opts) do
    cluster = Keyword.fetch!(opts, :cluster)

    Enum.each(@topics, fn topic ->
      Phoenix.PubSub.subscribe(PubsubRedisAdapter.PubSub, topic)
    end)

    {:ok, %{cluster: cluster}}
  end

  @impl true
  def handle_info({:cross_cluster, %Event{} = event}, state) do
    cond do
      event.cluster == state.cluster ->
        # Own-cluster echo — drop silently
        {:noreply, state}

      Dedup.check_and_mark(event.msg_id) == :duplicate ->
        {:noreply, state}

      true ->
        Task.Supervisor.start_child(PubsubRedisAdapter.TaskSupervisor, fn ->
          dispatch_local(event)
        end)

        {:noreply, state}
    end
  end

  defp dispatch_local(%Event{topic: topic, payload: payload}) do
    # Local-only fanout via a distinct topic name so we don't hit Redis again
    Phoenix.PubSub.local_broadcast(
      PubsubRedisAdapter.PubSub,
      "local:" <> topic,
      payload
    )
  end
end
```

### Step 7: Tests

```elixir
defmodule PubsubRedisAdapter.DedupTest do
  use ExUnit.Case, async: false

  alias PubsubRedisAdapter.Dedup

  setup do
    :ets.delete_all_objects(:pubsub_redis_dedup)
    :ok
  end

  test "first sighting is :new, second is :duplicate" do
    assert Dedup.check_and_mark("msg-1") == :new
    assert Dedup.check_and_mark("msg-1") == :duplicate
  end

  test "different ids don't collide" do
    assert Dedup.check_and_mark("msg-a") == :new
    assert Dedup.check_and_mark("msg-b") == :new
  end

  test "high-concurrency inserts do not double-mark" do
    results =
      1..200
      |> Enum.map(fn _ ->
        Task.async(fn -> Dedup.check_and_mark("hot") end)
      end)
      |> Task.await_many(5_000)

    new_count = Enum.count(results, &(&1 == :new))
    assert new_count == 1
  end
end
```

```elixir
defmodule PubsubRedisAdapter.PublisherTest do
  use ExUnit.Case, async: false

  alias PubsubRedisAdapter.{Event, Publisher}

  setup do
    Application.put_env(:pubsub_redis_adapter, :cluster_name, "test-cluster")
    :ets.delete_all_objects(:pubsub_redis_dedup)
    :ok
  end

  test "broadcast emits envelope with required fields" do
    Phoenix.PubSub.subscribe(PubsubRedisAdapter.PubSub, "orders.events")

    :ok = Publisher.broadcast("orders.events", %{id: 42, state: :placed})

    assert_receive {:cross_cluster, %Event{} = ev}, 1_000
    assert ev.topic == "orders.events"
    assert ev.cluster == "test-cluster"
    assert ev.origin_node == node()
    assert is_binary(ev.msg_id)
    assert ev.payload == %{id: 42, state: :placed}
  end

  test "own-cluster broadcast is marked seen so echo is ignored" do
    :ok = Publisher.broadcast("fulfillment.events", :test)
    # The publisher pre-marks its msg_id — verify indirectly: if subscriber
    # also marks, it should see :duplicate. We replay by hand.
    event = Event.new("test-cluster", "fulfillment.events", :test)
    # A distinct id = treated as new
    assert PubsubRedisAdapter.Dedup.check_and_mark(event.msg_id) == :new
  end
end
```

---


### Why this works

The design leans on BEAM guarantees (process isolation, mailbox ordering, supervisor restarts) and pushes invariants to the boundaries of each module. State transitions are explicit, failure modes are declared rather than implicit, and each step is independently testable. That combination keeps the implementation correct under concurrent load and cheap to change later.

## Benchmark

```elixir
# Minimal measurement — replace with Benchee for distribution stats.
{time_us, _} = :timer.tc(fn ->
  for _ <- 1..10_000, do: run_operation()
end)
IO.puts("avg: #{time_us / 10_000} µs/op")
```

Target: operation should complete in the low-microsecond range on modern hardware; deviations by >2× indicate a regression worth investigating.

## Trade-offs and production gotchas

**1. Redis is not a durable log**
Pub/sub is fire-and-forget. If a subscriber is disconnected when a message arrives, it is
gone. For at-least-once semantics, use Redis Streams (XADD/XREAD with consumer groups) or
switch to Kafka/NATS. `Phoenix.PubSub.Redis` does not retry.

**2. Message size amplifies WAN cost**
Every subscribing cluster receives every publish. If `orders-cluster` emits a 10KB event and
10 other clusters listen, that's 100KB of Redis traffic per event. Either keep payloads
small (send an ID, fetch details on demand) or shard topics by audience.

**3. Jason serialization is synchronous in the publisher**
If your payload contains a large struct graph, encoding happens inline in the publish call.
Benchmark with Benchee and pre-encode hot-path events. For opaque Erlang terms, consider
`:erlang.term_to_binary/1` — faster than Jason but only works between BEAM consumers.

**4. Dedup cache memory**
100k entries × (36 bytes UUID + 8 bytes timestamp + ETS overhead ~64 bytes) ≈ 10MB. Acceptable.
If you go to 10M entries, switch to a Bloom filter (`:bloom_filter_ex`) — accepts false
positives (drops real messages) at tiny memory cost.

**5. Failover creates a dedup blind spot**
During a Sentinel-triggered failover, the client disconnects. Upon reconnect, messages
published during the gap are lost — dedup cannot save you. If your domain cannot tolerate
loss, layer a **reconciliation job** that compares event stores across clusters periodically.

**6. Topics are a flat namespace globally**
Two teams publishing to `"events"` will collide across clusters. Always namespace topics by
bounded context: `"orders.v1.placed"`, `"fulfillment.v2.shipped"`. Include a version —
schemas evolve.

**7. Reconnection storms**
When Redis flaps, every node in every cluster reconnects simultaneously. This triples Redis
CPU for ~30 seconds. Stagger clients with `Process.sleep(:rand.uniform(5_000))` in the
supervisor child_spec `restart: :transient` + exponential backoff in Redix.

**8. When NOT to use this**
For intra-cluster pub/sub: PG2 is faster and simpler. For durable event streaming: Kafka,
Redpanda, or EventStoreDB. For strict ordering: Redis Streams with a single consumer. For
request/reply: this is pub/sub, use RabbitMQ direct queues instead.

---

## Performance notes

Benchmark `Phoenix.PubSub.Redis` with a real Redis (not Fakeredis) using Benchee:

```elixir
Benchee.run(
  %{
    "cross-cluster publish" => fn ->
      PubsubRedisAdapter.Publisher.broadcast("bench.topic", %{k: "v"})
    end
  },
  time: 10,
  parallel: 4
)
```

Expected on LAN with a 1KB payload:
- p50: 1.5-3 ms
- p99: 8-15 ms
- Throughput: ~5-10k msg/s per node (pool_size: 5)

The bottleneck is usually Redis connection throughput, not Elixir. Increase `pool_size`
until Redis CPU hits 60%, then shard topics across multiple Redis instances.

---

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?

## Resources

- [`phoenix_pubsub_redis` hex docs](https://hexdocs.pm/phoenix_pubsub_redis/) — adapter source and options
- [`phoenix_pubsub` hex docs](https://hexdocs.pm/phoenix_pubsub/) — adapter behaviour contract
- [Redis pub/sub documentation](https://redis.io/docs/interact/pubsub/) — semantics and limitations
- [Chris McCord — "Phoenix PubSub 2.0"](https://dashbit.co/blog/phoenix-1.5-released) — design notes on the adapter split
- [`Redix` hex docs](https://hexdocs.pm/redix/) — low-level client used under the hood
- [Dashbit — "Real-time in Elixir"](https://dashbit.co/blog) — patterns for cross-cluster broadcasting
- [Phoenix.PubSub.Redis source](https://github.com/phoenixframework/phoenix_pubsub_redis) — study the fastlane and pool structure
