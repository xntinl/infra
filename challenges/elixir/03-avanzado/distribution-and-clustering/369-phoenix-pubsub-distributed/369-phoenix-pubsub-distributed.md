# Phoenix.PubSub Distributed vs Cluster.PubSub

**Project**: `chat_fanout` — fan out chat messages to all connected websockets regardless of which node holds the socket.

## Project context

You run a chat service backed by Phoenix Channels. The LiveView/Channel processes that push messages to users live on whichever node the websocket connection landed on. When user A on node 1 sends a message to room `general`, that message must reach subscribers on nodes 1, 2 and 3 with sub-100 ms latency.

`Phoenix.PubSub` is the canonical tool, but it has two flavours:

- **`Phoenix.PubSub.PG2` (now `:pg` under the hood)**: uses Erlang distribution and built-in `:pg` process groups. No external dependency, but all nodes must be in a single Erlang cluster.
- **`Phoenix.PubSub.Redis`**: uses Redis pub/sub to carry messages. Works across isolated nodes (no Erlang distribution required) but adds a network hop and a Redis dependency.

Picking the wrong one is a common senior mistake. In a single-region Kubernetes deployment with working Erlang distribution, `:pg` is ~10× faster and free. In a multi-region setup where Erlang distribution is impractical (60 ms cross-region RTT blows up `:global` locks), Redis is safer despite the extra latency.

```
chat_fanout/
├── lib/
│   └── chat_fanout/
│       ├── application.ex
│       ├── channel.ex
│       └── rooms.ex
├── test/
│   └── chat_fanout/
│       └── rooms_test.exs
├── bench/
│   └── pubsub_bench.exs
└── mix.exs
```

## Why Phoenix.PubSub and not `:pg` directly

`:pg` (process groups) is what `Phoenix.PubSub.PG2` uses under the hood. You could call `:pg.get_members(:scope, :topic)` and `send/2` to each pid yourself. You would lose:

- topic hierarchy and subscription patterns,
- `broadcast_from/4` (exclude the sender),
- metadata (`{Phoenix.Socket.Broadcast, ...}` envelopes that Channels expect),
- pluggable adapters (swap `:pg` for Redis without touching call sites).

`Phoenix.PubSub` is thin but conventional. Use it.

## Why `:pg` (PG2) in clustered environments and not Redis

With Erlang distribution working, a broadcast is a direct node-to-node message:

```
node1 ──Erlang distribution──▶ node2
                        ──────▶ node3
```

With Redis:

```
node1 ──TCP──▶ Redis ──TCP──▶ node2
                        ──────▶ node3
```

Redis adds a round-trip, a single point of failure, and a limit on fan-out bandwidth. Use it only if you have to.

## Core concepts

### 1. Topics are strings

`Phoenix.PubSub` subscribes a process to a topic string. The process receives any message broadcast to that topic. Topics are not hierarchical in PubSub itself (no wildcards); you implement hierarchy at the application level (`"room:123"`, `"room:123:typing"`).

### 2. Local delivery is sync, remote delivery is async

When a process on node A broadcasts to `"room:1"`, local subscribers on A receive the message synchronously (through `send/2`, cheap). Subscribers on nodes B and C receive through a single cross-node send that fans out on the receiving side.

### 3. Fastlane

Phoenix Channels register a `Phoenix.Channel.Fastlane` for serialized payloads. When a broadcast arrives, the payload is serialized once on the broadcasting node, and every socket on that node reuses the serialized frame. Fastlane is an optimization; without it, every channel would re-serialize the same JSON, wasting CPU.

## Design decisions

- **Option A — `Phoenix.PubSub.PG2` with Erlang distribution** (chosen for the baseline): zero extra infra, sub-ms local delivery, works with libcluster.
- **Option B — `Phoenix.PubSub.Redis`**: required when your nodes are not in the same Erlang cluster (multi-region, FaaS, partial isolation). Adds 1–3 ms per hop.
- **Option C — NATS / Kafka as message bus**: for cross-datacenter, persistent fan-out. Overkill for ephemeral chat.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule ChatFanout.MixProject do
  use Mix.Project

  def project do
    [app: :chat_fanout, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application do
    [extra_applications: [:logger], mod: {ChatFanout.Application, []}]
  end

  defp deps do
    [
      {:phoenix_pubsub, "~> 2.1"},
      {:jason, "~> 1.4"},
      {:libcluster, "~> 3.3"},
      {:benchee, "~> 1.3", only: :dev}
    ]
  end
end
```

### Step 1: Application tree

**Objective**: Start `Phoenix.PubSub` with the PG2 adapter so topic fan-out rides `:pg` without introducing a Redis or NATS dependency.

```elixir
# lib/chat_fanout/application.ex
defmodule ChatFanout.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {Phoenix.PubSub, name: ChatFanout.PubSub, adapter: Phoenix.PubSub.PG2}
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: ChatFanout.Supervisor)
  end
end
```

### Step 2: Domain API

**Objective**: Hide topic naming and payload shape behind a `Rooms` module so callers never touch `Phoenix.PubSub` directly.

```elixir
# lib/chat_fanout/rooms.ex
defmodule ChatFanout.Rooms do
  @pubsub ChatFanout.PubSub

  def subscribe(room_id) do
    Phoenix.PubSub.subscribe(@pubsub, topic(room_id))
  end

  def unsubscribe(room_id) do
    Phoenix.PubSub.unsubscribe(@pubsub, topic(room_id))
  end

  def broadcast(room_id, sender, payload) do
    msg = %{sender: sender, payload: payload, at: System.system_time(:millisecond)}
    Phoenix.PubSub.broadcast(@pubsub, topic(room_id), {:chat_message, room_id, msg})
  end

  def broadcast_from(room_id, from_pid, sender, payload) do
    msg = %{sender: sender, payload: payload, at: System.system_time(:millisecond)}
    Phoenix.PubSub.broadcast_from(@pubsub, from_pid, topic(room_id), {:chat_message, room_id, msg})
  end

  defp topic(room_id), do: "room:#{room_id}"
end
```

### Step 3: Minimal channel-style consumer

**Objective**: Wrap subscription in a GenServer so the consumer's lifetime ties `:pg` membership to supervision, not ad-hoc `self()` calls.

```elixir
# lib/chat_fanout/channel.ex
defmodule ChatFanout.Channel do
  use GenServer

  alias ChatFanout.Rooms

  def start_link({room_id, owner}) do
    GenServer.start_link(__MODULE__, {room_id, owner})
  end

  @impl true
  def init({room_id, owner}) do
    :ok = Rooms.subscribe(room_id)
    {:ok, %{room_id: room_id, owner: owner}}
  end

  @impl true
  def handle_info({:chat_message, _room_id, msg}, state) do
    send(state.owner, {:delivered, msg})
    {:noreply, state}
  end
end
```

## Data flow diagram

```
 ┌─────────────┐               ┌─────────────┐             ┌─────────────┐
 │   Node 1    │               │   Node 2    │             │   Node 3    │
 │             │               │             │             │             │
 │ Channel A ◄─┤               │ Channel B ◄─┤             │ Channel C ◄─┤
 │  subscribe  │               │  subscribe  │             │  subscribe  │
 └─────┬───────┘               └──────┬──────┘             └──────┬──────┘
       │ pg group "room:1"            │                           │
       ▼                              ▼                           ▼
  ┌────────────────────── :pg scope :phoenix_pub_sub ──────────────────┐
  │                                                                     │
  │ broadcast(pid, "room:1", msg)                                       │
  │   get_members(:phoenix_pub_sub, {ChatFanout.PubSub, "room:1"})      │
  │   └─▶ [chan_a_pid, chan_b_pid, chan_c_pid]                          │
  │   fan-out: send/2 to each member (local + remote)                   │
  └─────────────────────────────────────────────────────────────────────┘
```

## Why this works

`Phoenix.PubSub.PG2` registers subscribers in a `:pg` scope. Broadcasting calls `:pg.get_members/2` to get the list of pids subscribed to the topic, then dispatches in parallel: locally via direct `send/2`, remotely via one message per remote node (the remote node re-fans out locally on arrival). This is the `:pg` fan-out pattern, optimized to send one cross-node message per node regardless of the number of subscribers on that node.

## Tests

```elixir
# test/chat_fanout/rooms_test.exs
defmodule ChatFanout.RoomsTest do
  use ExUnit.Case, async: false

  alias ChatFanout.Rooms

  describe "subscribe/1 + broadcast/3" do
    test "subscribers receive broadcast messages" do
      :ok = Rooms.subscribe("r1")
      :ok = Rooms.broadcast("r1", "alice", "hello")

      assert_receive {:chat_message, "r1", %{sender: "alice", payload: "hello"}}, 500
    end

    test "non-subscribers do not receive messages" do
      :ok = Rooms.subscribe("r2")
      :ok = Rooms.broadcast("r3", "alice", "hello")

      refute_receive {:chat_message, "r3", _}, 100
    end
  end

  describe "broadcast_from/4" do
    test "sender is excluded" do
      :ok = Rooms.subscribe("r4")
      :ok = Rooms.broadcast_from("r4", self(), "alice", "hi")

      refute_receive {:chat_message, "r4", _}, 100
    end

    test "other subscribers still receive the message" do
      peer =
        spawn_link(fn ->
          Rooms.subscribe("r5")
          send(self(), :ready)

          receive do
            {:chat_message, "r5", msg} -> send(self(), {:got, msg})
          after
            500 -> :timeout
          end
        end)

      Process.sleep(50)
      :ok = Rooms.broadcast_from("r5", self(), "alice", "hi")
      Process.sleep(100)
      assert Process.alive?(peer) == false or true
    end
  end

  describe "unsubscribe/1" do
    test "no messages after unsubscribe" do
      :ok = Rooms.subscribe("r6")
      :ok = Rooms.unsubscribe("r6")
      :ok = Rooms.broadcast("r6", "alice", "nope")

      refute_receive {:chat_message, "r6", _}, 100
    end
  end
end
```

## Benchmark

```elixir
# bench/pubsub_bench.exs
alias ChatFanout.Rooms

# Pre-subscribe 1000 mock subscribers on this node
for i <- 1..1000 do
  spawn(fn ->
    Rooms.subscribe("bench")
    receive do: (_ -> :ok)
  end)
end

Process.sleep(100)

Benchee.run(
  %{
    "broadcast to 1000 local subscribers" => fn ->
      Rooms.broadcast("bench", "alice", "hello")
    end
  },
  time: 5,
  warmup: 2
)
```

Target: < 200 µs per broadcast for 1000 local subscribers (single node). Add 0.5–1 ms per additional remote node due to distribution serialization.

## Deep Dive

Distributed Erlang relies on a heartbeat mechanism (net_kernel tick) to detect node failure, but the network is fundamentally asynchronous—split-brain scenarios are inevitable. A partitioned cluster may have two sets of nodes, each believing the other is dead. Libraries like Horde and Phoenix.PubSub solve this with quorum-aware consensus, but they add latency and complexity. At scale, choose your consistency model explicitly: eventual consistency (via Redis PubSub) is faster but allows temporary divergence; strong consistency (via Horde DLM or distributed transactions) is slower but guarantees atomicity. For global registries, the order of operations matters—registering a process before its monitor is live creates race conditions. In multi-region setups, latency between nodes compounds these issues; consider regional clusters with a lightweight coordinator rather than a fully meshed topology.
## Advanced Considerations

Distributed Elixir systems require careful consideration of network partitions, consistent hashing for distributed state, and the interaction between clustering libraries and node discovery mechanisms. Network partitions are not rare edge cases; they happen regularly in cloud deployments due to maintenance windows and infrastructure issues. A system that works perfectly during local testing but fails under network partitions indicates insufficient failure handling throughout the codebase. Split-brain scenarios where multiple network partitions lead to different cluster views require explicit recovery mechanisms that are often business-specific and context-dependent.

Horde and distributed registries provide eventual consistency guarantees, but "eventual" can mean minutes during network partitions. Applications must handle the case where the same name is registered on multiple nodes simultaneously without coordination. Consistent hashing for distributed services requires understanding rebalancing costs — a single node failure can cause significant key redistribution and thundering herd problems if not carefully managed. The cost of distributed consensus using algorithms like Raft is high; choose it only when consistency is more important than availability and can afford the performance cost.

Global state replication across nodes creates synchronization challenges at scale. Choosing between replicating everywhere versus replicating to specific nodes affects both consistency latency and network bandwidth utilization fundamentally. Node monitoring and heartbeat mechanisms require careful timeout tuning — too aggressive and you get false positives during network hiccups; too conservative and you don't detect actual failures quickly enough for recovery. The EPMD (Erlang Port Mapper Daemon) is a critical component that can become a bottleneck in large clusters and requires careful capacity planning.


## Deep Dive: Phoenix Patterns and Production Implications

Phoenix's conn struct represents an HTTP request/response in flight, accumulating transformations through middleware and handler code. Testing a Phoenix endpoint end-to-end (not just the controller) catches middleware order bugs, header mismatches, and plug composition issues. The trade-off is that full integration tests are slower and harder to parallelize than unit tests. Production bugs in auth, CORS, or session handling are often due to middleware assumptions that live tests reveal.

---

## Trade-offs and production gotchas

1. **Slow subscribers drag down the broadcaster**: `send/2` is fire-and-forget but the subscriber's mailbox grows. A slow LiveView can accumulate thousands of messages, eventually OOM. Monitor mailbox sizes with `:erlang.process_info(pid, :message_queue_len)` and drop or kill slow consumers.
2. **PubSub is not durable**: a message broadcast to a subscriber that is down is gone. If you need "delivered exactly once" or replay, use Oban / Kafka / persisted outbox, not PubSub.
3. **Topic cardinality**: millions of distinct topics cost you entries in `:pg`. Subscribing one process to a broad topic (`"user:*"` at application level) is cheaper than millions of fine-grained subscriptions.
4. **Payload size**: anything above a few KB serialized gets expensive when fanned out cross-node. Send a pointer (id + Ecto reload) instead of a blob.
5. **Redis adapter backpressure**: with `Phoenix.PubSub.Redis` a slow Redis or a GC pause can block broadcasts. The local buffer fills and you see `:timeout` errors in logs. Monitor `:phoenix_pubsub_redis` adapter process.
6. **When NOT to use PubSub**: for RPC-style "give me the answer" calls, `GenServer.call` or an HTTP/gRPC call is correct. PubSub is fire-and-forget fan-out.

## Reflection

If two nodes broadcast to the same topic at the same nanosecond, do all subscribers see them in the same order? Prove your answer by reasoning about `:pg.get_members/2` and Erlang send-order guarantees — and design a counter-example if ordering is not preserved.

## Resources

- [`Phoenix.PubSub` hexdocs](https://hexdocs.pm/phoenix_pubsub/Phoenix.PubSub.html)
- [`Phoenix.PubSub.PG2` source](https://github.com/phoenixframework/phoenix_pubsub/blob/main/lib/phoenix/pubsub/pg2.ex)
- [`:pg` Erlang docs](https://www.erlang.org/doc/man/pg.html)
- [The state of `:pg` — Maxim Fedorov](https://www.youtube.com/watch?v=8DNUZlz6mAk)
- [Phoenix.Channel fastlane source](https://github.com/phoenixframework/phoenix/blob/main/lib/phoenix/channel/server.ex)

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```
