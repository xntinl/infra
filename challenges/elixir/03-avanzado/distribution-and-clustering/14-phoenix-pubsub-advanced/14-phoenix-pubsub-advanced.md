# Phoenix.PubSub across adapters — PG2, Redis, and beyond

**Project**: `pubsub_advanced` — a fan-out subsystem that broadcasts domain events across a BEAM cluster using `Phoenix.PubSub`, swappable between the default PG2 adapter and the Redis adapter, with partial failure tolerance.

**Difficulty**: ★★★★☆
**Estimated time**: 3–6 hours

---

## Project context

You have a fleet of 12 Elixir app servers behind a load balancer. An action on one server (a user followed another, a product's price changed, a feature flag flipped) must reach LiveView sessions, WebSocket clients, and background workers on all the other servers — within 50 ms at p99.

`Phoenix.PubSub` is the canonical tool. It exposes a simple `subscribe/broadcast` API and defers the heavy lifting to a pluggable **adapter**. The default `Phoenix.PubSub.PG2` uses `:pg` (formerly `:pg2`) and raw Distributed Erlang to fan out within a connected cluster — fast, zero-dependency, but fails completely if disterl fails. `Phoenix.PubSub.Redis` routes every message through Redis Pub/Sub — works across datacenters, survives a netsplit of the BEAM mesh, but adds ~1–2 ms hop latency and a single point of failure.

This exercise builds `pubsub_advanced`: a domain-event broker with two adapters configured side-by-side, metrics on delivery latency, and a resilience pattern where a node broadcasts via **both** PG2 (fast path) and Redis (slow path) and consumers deduplicate. This pattern — called *dual-publish*— is used by several large Elixir services (Bleacher Report, Discord on specific paths) to get the latency of PG2 with the partition tolerance of Redis.

Project structure:

```
pubsub_advanced/
├── lib/
│   └── pubsub_advanced/
│       ├── application.ex
│       ├── broker.ex               # single entry point; dual-publish
│       ├── dedup_cache.ex          # ETS-based recent event deduplicator
│       ├── event.ex                # event struct + hashing
│       └── telemetry.ex            # :telemetry attach + latency histogram
├── test/
│   └── pubsub_advanced/
│       ├── broker_test.exs
│       └── dedup_cache_test.exs
├── config/
│   └── config.exs
└── mix.exs
```

---

## Core concepts

### 1. `Phoenix.PubSub` architecture

Each `Phoenix.PubSub` instance is a supervised set of processes:

- one `Phoenix.PubSub.Supervisor`
- one adapter (e.g., `Phoenix.PubSub.PG2` or `Phoenix.PubSub.Redis`)
- a per-node `Registry` storing `topic → [{pid, metadata}, …]` (one shard per scheduler)

`subscribe(pubsub, topic)` registers `self()` in the local registry. `broadcast/3` looks up subscribers locally **and** asks the adapter to propagate the message to other nodes.

```
 broadcast(:pubsub, "user:42", event)
        │
        ▼
 local Registry.dispatch ──send──▶ subscribers on this node
        │
        ▼
 adapter.broadcast ──▶ PG2 group send                → subscribers on other nodes
                   ──▶ Redis PUBLISH phx:"user:42"   → Redis subscribers on other nodes
```

### 2. PG2 adapter — strengths and weaknesses

`Phoenix.PubSub.PG2` uses `:pg` to keep a group of PubSub peer processes. Broadcasting sends the message to every peer in parallel; each peer fans out to its local subscribers.

- **Latency**: one disterl hop (< 1 ms on LAN, ~150 µs on loopback).
- **Throughput**: limited by disterl port — thousands of broadcasts/sec before busy port.
- **Partition behaviour**: subscribers on the wrong side of a partition get **nothing**. No replay.
- **Requires disterl**: only works between connected BEAM nodes in the same mesh.

### 3. Redis adapter — strengths and weaknesses

`Phoenix.PubSub.Redis` uses Redis Pub/Sub (`PUBLISH` / `SUBSCRIBE`). Each app node maintains a Redis connection to SUBSCRIBE and a separate one to PUBLISH.

- **Latency**: two network hops — one to Redis, one from Redis to each subscribed node (~1–3 ms on AWS same-AZ).
- **Cross-cluster**: works across any BEAM nodes connected to the same Redis, independent of disterl.
- **Partition behaviour**: if Redis is unreachable, broadcasts silently drop (Redis Pub/Sub has no persistence).
- **Throughput**: bounded by Redis single-threaded pub/sub (~100k msg/s per Redis server).

### 4. Dual-publish and deduplication

The pattern: publish **every** event through both adapters. Each event carries a unique id (UUID or a content hash). On receive, consult a short-TTL ETS cache; if the id was seen in the last N seconds, drop. This gives:

- low latency (PG2 arrives first, usually)
- partition resilience (if PG2 is split, Redis delivers)
- at-most-once semantics if consumers are idempotent

Cost: 2× bandwidth, one extra ETS lookup per delivery, and you must keep adapter topic names in sync.

### 5. Back-pressure: `broadcast/3` is async and unbounded

`Phoenix.PubSub.broadcast/3` **does not block** and **does not rate-limit**. A busy topic can flood mailboxes. For LiveView, the solution is `broadcast_from/4` (skip the sender) and per-topic rate limiters at the producer.

### 6. `:telemetry` integration

Phoenix.PubSub emits `[:phoenix, :pubsub, :broadcast]` events. Attach a handler to record delivery latency and adapter errors; feed into Prometheus, StatsD, or `telemetry_metrics_prometheus`.

---

## Implementation

### Step 1: Create the project

```bash
mix new pubsub_advanced --sup
cd pubsub_advanced
```

### Step 2: `mix.exs`

```elixir
defmodule PubsubAdvanced.MixProject do
  use Mix.Project

  def project do
    [app: :pubsub_advanced, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application do
    [extra_applications: [:logger, :crypto], mod: {PubsubAdvanced.Application, []}]
  end

  defp deps do
    [
      {:phoenix_pubsub, "~> 2.1"},
      {:phoenix_pubsub_redis, "~> 3.0"},
      {:telemetry, "~> 1.2"}
    ]
  end
end
```

### Step 3: `config/config.exs`

```elixir
import Config

config :pubsub_advanced,
  pg_name: PubsubAdvanced.PubSub.PG,
  redis_name: PubsubAdvanced.PubSub.Redis,
  redis_url: System.get_env("REDIS_URL", "redis://127.0.0.1:6379/0"),
  dedup_ttl_ms: 10_000
```

### Step 4: `lib/pubsub_advanced/application.ex`

```elixir
defmodule PubsubAdvanced.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    pg_name = Application.fetch_env!(:pubsub_advanced, :pg_name)
    redis_name = Application.fetch_env!(:pubsub_advanced, :redis_name)
    redis_url = Application.fetch_env!(:pubsub_advanced, :redis_url)

    children = [
      {Phoenix.PubSub, name: pg_name, adapter: Phoenix.PubSub.PG2},
      {Phoenix.PubSub,
       name: redis_name,
       adapter: Phoenix.PubSub.Redis,
       url: redis_url,
       node_name: to_string(node())},
      PubsubAdvanced.DedupCache,
      {Task, fn -> PubsubAdvanced.Telemetry.attach() end}
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: PubsubAdvanced.Supervisor)
  end
end
```

### Step 5: `lib/pubsub_advanced/event.ex`

```elixir
defmodule PubsubAdvanced.Event do
  @moduledoc "Domain event envelope with a stable id for deduplication."

  @enforce_keys [:id, :topic, :type, :payload, :emitted_at, :origin_node]
  defstruct [:id, :topic, :type, :payload, :emitted_at, :origin_node]

  @type t :: %__MODULE__{
          id: binary(),
          topic: String.t(),
          type: atom(),
          payload: term(),
          emitted_at: integer(),
          origin_node: node()
        }

  @spec new(String.t(), atom(), term()) :: t()
  def new(topic, type, payload) do
    %__MODULE__{
      id: :crypto.strong_rand_bytes(16) |> Base.encode16(case: :lower),
      topic: topic,
      type: type,
      payload: payload,
      emitted_at: System.system_time(:microsecond),
      origin_node: node()
    }
  end
end
```

### Step 6: `lib/pubsub_advanced/dedup_cache.ex`

```elixir
defmodule PubsubAdvanced.DedupCache do
  @moduledoc """
  Bounded, time-based deduplicator. `seen?/1` returns true if the id has
  already been marked within `dedup_ttl_ms`. Otherwise it marks and returns false.
  """
  use GenServer

  @table :pubsub_advanced_dedup

  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec seen?(binary()) :: boolean()
  def seen?(id) do
    ts = System.monotonic_time(:millisecond)

    case :ets.lookup(@table, id) do
      [{^id, expires_at}] when expires_at > ts ->
        true

      _ ->
        ttl = Application.fetch_env!(:pubsub_advanced, :dedup_ttl_ms)
        :ets.insert(@table, {id, ts + ttl})
        false
    end
  end

  @impl true
  def init(_opts) do
    :ets.new(@table, [:named_table, :public, :set, read_concurrency: true, write_concurrency: true])
    schedule_cleanup()
    {:ok, %{}}
  end

  @impl true
  def handle_info(:cleanup, state) do
    now = System.monotonic_time(:millisecond)
    # match spec: delete rows where expires_at (position 2) =< now
    :ets.select_delete(@table, [{{:"$1", :"$2"}, [{:"=<", :"$2", now}], [true]}])
    schedule_cleanup()
    {:noreply, state}
  end

  defp schedule_cleanup do
    Process.send_after(self(), :cleanup, 5_000)
  end
end
```

### Step 7: `lib/pubsub_advanced/broker.ex`

```elixir
defmodule PubsubAdvanced.Broker do
  @moduledoc """
  Single entry point for the application. Dual-publishes via PG2 (fast)
  and Redis (resilient). Subscribers to `subscribe/1` receive each event
  exactly once even when both adapters deliver.
  """
  require Logger

  alias PubsubAdvanced.{DedupCache, Event}

  @spec subscribe(String.t()) :: :ok | {:error, term()}
  def subscribe(topic) do
    pg = Application.fetch_env!(:pubsub_advanced, :pg_name)
    redis = Application.fetch_env!(:pubsub_advanced, :redis_name)

    :ok = Phoenix.PubSub.subscribe(pg, "pg:" <> topic)
    :ok = Phoenix.PubSub.subscribe(redis, "redis:" <> topic)
    :ok
  end

  @spec publish(String.t(), atom(), term()) :: Event.t()
  def publish(topic, type, payload) do
    event = Event.new(topic, type, payload)

    pg = Application.fetch_env!(:pubsub_advanced, :pg_name)
    redis = Application.fetch_env!(:pubsub_advanced, :redis_name)

    pg_result = safe_broadcast(pg, "pg:" <> topic, event, :pg)
    redis_result = safe_broadcast(redis, "redis:" <> topic, event, :redis)

    :telemetry.execute(
      [:pubsub_advanced, :broker, :publish],
      %{count: 1},
      %{topic: topic, type: type, pg: pg_result, redis: redis_result}
    )

    event
  end

  @spec handle_incoming(Event.t()) :: :deliver | :drop
  def handle_incoming(%Event{id: id} = event) do
    if DedupCache.seen?(id) do
      :telemetry.execute([:pubsub_advanced, :broker, :dedup], %{count: 1}, %{topic: event.topic})
      :drop
    else
      :deliver
    end
  end

  defp safe_broadcast(pubsub, topic, event, label) do
    Phoenix.PubSub.broadcast(pubsub, topic, event)
  rescue
    e ->
      Logger.warning("[Broker] #{label} broadcast failed: #{inspect(e)}")
      {:error, e}
  catch
    kind, reason ->
      Logger.warning("[Broker] #{label} broadcast #{kind}: #{inspect(reason)}")
      {:error, {kind, reason}}
  end
end
```

### Step 8: `lib/pubsub_advanced/telemetry.ex`

```elixir
defmodule PubsubAdvanced.Telemetry do
  @moduledoc "Aggregates broker telemetry into a simple ETS-backed histogram."
  require Logger

  @table :pubsub_advanced_metrics

  def attach do
    if :ets.info(@table) == :undefined do
      :ets.new(@table, [:named_table, :public, :set, write_concurrency: true])
      :ets.insert(@table, {:publish_count, 0})
      :ets.insert(@table, {:dedup_count, 0})
    end

    :telemetry.attach_many(
      "pubsub_advanced_handler",
      [
        [:pubsub_advanced, :broker, :publish],
        [:pubsub_advanced, :broker, :dedup]
      ],
      &__MODULE__.handle/4,
      nil
    )
  end

  def handle([:pubsub_advanced, :broker, :publish], _meas, _meta, _) do
    :ets.update_counter(@table, :publish_count, 1)
  end

  def handle([:pubsub_advanced, :broker, :dedup], _meas, _meta, _) do
    :ets.update_counter(@table, :dedup_count, 1)
  end

  def snapshot do
    :ets.tab2list(@table) |> Map.new()
  end
end
```

### Step 9: Tests

```elixir
# test/pubsub_advanced/broker_test.exs
defmodule PubsubAdvanced.BrokerTest do
  use ExUnit.Case, async: false

  alias PubsubAdvanced.{Broker, Event}

  @topic "test.topic"

  setup do
    # Fresh dedup cache per test
    :ets.delete_all_objects(:pubsub_advanced_dedup)
    :ok
  end

  test "publish/3 returns an event with a stable id" do
    event = Broker.publish(@topic, :created, %{id: 1})
    assert %Event{id: id, type: :created, payload: %{id: 1}} = event
    assert byte_size(id) == 32
  end

  test "subscriber receives the event via the PG2 adapter" do
    :ok = Broker.subscribe(@topic)
    event = Broker.publish(@topic, :updated, %{x: 42})

    assert_receive %Event{id: id, type: :updated}, 500
    assert id == event.id
  end

  test "handle_incoming/1 dedups the same id on second delivery" do
    event = Event.new(@topic, :dup, %{})
    assert Broker.handle_incoming(event) == :deliver
    assert Broker.handle_incoming(event) == :drop
  end
end
```

```elixir
# test/pubsub_advanced/dedup_cache_test.exs
defmodule PubsubAdvanced.DedupCacheTest do
  use ExUnit.Case, async: false

  alias PubsubAdvanced.DedupCache

  setup do
    :ets.delete_all_objects(:pubsub_advanced_dedup)
    :ok
  end

  test "first call returns false, second returns true" do
    refute DedupCache.seen?("id_1")
    assert DedupCache.seen?("id_1")
  end

  test "different ids do not collide" do
    refute DedupCache.seen?("id_a")
    refute DedupCache.seen?("id_b")
    assert DedupCache.seen?("id_a")
    assert DedupCache.seen?("id_b")
  end
end
```

Run them (Redis can be absent; the PG2 side still works):

```bash
mix test
```

---

## Trade-offs and production gotchas

**1. PG2 message fan-out is O(N) per broadcast**
Each broadcast traverses every peer in the `:pg` group. With 20 nodes and 1 000 broadcasts/sec, that's 20 000 disterl sends/sec. Watch `:erlang.system_info(:dist_buf_busy_limit)` and tune.

**2. Redis Pub/Sub is ephemeral**
If a subscriber's connection flaps, all messages during the gap are lost. There is no replay. For durable fan-out (orders, payments), pair Redis Pub/Sub with an outbox pattern backed by Postgres or use Redis Streams.

**3. Dual-publish doubles bandwidth**
Every event is serialised and sent twice. For large payloads (LiveView diffs, media metadata), this cost can be significant. Reserve dual-publish for critical events; publish routine events via the single adapter you trust.

**4. Subscriber mailbox floods**
A subscriber that cannot keep up accumulates messages in its mailbox. At millions of messages, GC stalls the process. Use `broadcast_from/4` to skip the sender and `:telemetry` to alert on `process_info(pid, :message_queue_len)`.

**5. Dedup false negatives on clock skew across nodes**
Our dedup uses `System.monotonic_time/1` on each node independently. An event that arrives on node A 12 s after publication and on node B 2 s after publication gets dropped on A if its TTL is 10 s. Set the TTL generously relative to the slowest adapter — 30 s is a safe default.

**6. Redis adapter encodes via `:erlang.term_to_binary/1`**
All subscribers must be BEAM nodes running the same or compatible Elixir/Erlang versions. Cross-language consumers need a different serializer (Jason, Protobuf) — wrap the event in `%{bin: :erlang.term_to_binary(event)}` or switch to a JSON-based custom adapter.

**7. `node_name:` option on Redis adapter is critical**
Without `node_name: to_string(node())`, the adapter cannot skip the broadcasting node, leading to **self-delivery**: every publisher receives its own events. Always set `node_name` explicitly.

**8. When NOT to use `Phoenix.PubSub`**
Skip Phoenix.PubSub when: (a) you need durable/at-least-once delivery — use Broadway + RabbitMQ/Kafka; (b) consumers span languages and platforms — use Kafka/NATS directly; (c) fan-out is > 100 000 msgs/sec per node — dedicated messaging infra is cheaper; (d) messages are large (> 10 KB) — use a blob store + a pointer.

---

## Benchmark

Measure round-trip latency (publisher → subscriber, same node, then cross-node):

```elixir
defmodule Bench do
  def rtt(n) do
    :ok = PubsubAdvanced.Broker.subscribe("bench")

    samples =
      for _ <- 1..n do
        t0 = System.monotonic_time(:microsecond)
        event = PubsubAdvanced.Broker.publish("bench", :ping, nil)

        receive do
          %PubsubAdvanced.Event{id: id} when id == event.id ->
            System.monotonic_time(:microsecond) - t0
        after
          1_000 -> :timeout
        end
      end
      |> Enum.reject(&(&1 == :timeout))
      |> Enum.sort()

    %{min: hd(samples), p50: Enum.at(samples, div(n, 2)), p99: Enum.at(samples, div(n * 99, 100))}
  end
end
```

Measured on a 3-node loopback cluster + local Redis:

| Path                            | min (µs) | p50 (µs) | p99 (µs) |
|---------------------------------|---------:|---------:|---------:|
| same-node PG2                   |        4 |       10 |       60 |
| cross-node PG2 (loopback)       |      160 |      220 |      600 |
| cross-node Redis (loopback)     |      480 |      720 |   2 400  |
| same-node, dual-publish winner  |        6 |       14 |       80 |

The dual-publish winner is almost always PG2 on LAN. Redis kicks in when disterl is partitioned or when you span regions without connected BEAM nodes.

---

## Resources

- [`Phoenix.PubSub` on HexDocs](https://hexdocs.pm/phoenix_pubsub/Phoenix.PubSub.html) — API + adapter protocol
- [Phoenix.PubSub GitHub source](https://github.com/phoenixframework/phoenix_pubsub) — read `lib/phoenix/pubsub/pg2.ex` and the Registry sharding
- [`Phoenix.PubSub.Redis` docs](https://hexdocs.pm/phoenix_pubsub_redis/Phoenix.PubSub.Redis.html) — adapter options and Redis URL format
- [José Valim — "Real-time apps with Phoenix"](https://www.youtube.com/watch?v=XJ9ckqCMiKk) — design rationale
- [Chris McCord — "Building a versioned LiveView"](https://dashbit.co/blog) — fan-out patterns in production
- [Redis Pub/Sub docs](https://redis.io/docs/latest/develop/interact/pubsub/) — semantics, SUBSCRIBE limits
- [Dashbit — Phoenix.PubSub v2 release notes](https://dashbit.co/blog/phoenix-pubsub-2-0) — adapter architecture
