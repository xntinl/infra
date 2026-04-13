# Phoenix.PubSub Local vs Distributed (`:pg` adapter)

**Project**: `events_bus` — a Phoenix.PubSub wiring that demonstrates the difference between local broadcast, node-local `:pg2`-style broadcast, and cross-cluster `:pg` broadcast, plus fastlaning.

## Project context

Your team runs 12 Phoenix nodes behind a load balancer. A user action on node A needs to invalidate a cache on all other nodes. The naive solution — `GenServer.call({:global, MyCache}, ...)` — serializes every invalidation through one process and crushed the cluster at 500 ops/sec. You need a fanout primitive that is node-local when you want it and cluster-wide when you must.

`Phoenix.PubSub` is that primitive. The default adapter on OTP 24+ is `Phoenix.PubSub.PG2` — misnamed, it actually uses `:pg` underneath. The adapter decides whether broadcasts stay local or cross nodes; subscribers are always local.

```
events_bus/
├── lib/
│   ├── events_bus/
│   │   ├── application.ex
│   │   ├── cache_invalidator.ex
│   │   └── listener.ex
│   └── events_bus_web/
│       └── endpoint.ex
├── test/
│   └── events_bus/
│       └── pubsub_test.exs
├── bench/
│   └── broadcast_bench.exs
└── mix.exs
```

## Why `:pg` and not `:pg2`

`:pg2` was OTP's original process group module. On OTP 24+, it was rewritten as `:pg` — lock-free, eventually consistent, no global coordinator. `:pg2` is kept for compatibility but deprecated in OTP 27. `Phoenix.PubSub.PG2` was renamed internally to use `:pg`; the adapter name was kept for backward compatibility.

**Why not Redis pub/sub?** Cross-datacenter, sure. Single-cluster, you add a network hop, a SPOF, and serialization overhead. `:pg` sends Erlang terms directly node-to-node — no serialization, no external process.

## Core concepts



---

**Why this matters:**
These concepts form the foundation of production Elixir systems. Understanding them deeply allows you to build fault-tolerant, scalable applications that operate correctly under load and failure.

**Real-world use case:**
This pattern appears in systems like:
- Phoenix applications handling thousands of concurrent connections
- Distributed data processing pipelines
- Financial transaction systems requiring consistency and fault tolerance
- Microservices communicating over unreliable networks

**Common pitfall:**
Many developers overlook that Elixir's concurrency model differs fundamentally from threads. Processes are isolated; shared mutable state does not exist. Trying to force shared-memory patterns leads to deadlocks, race conditions, or silently incorrect behavior. Always think in terms of message passing and immutability.
### 1. Adapter vs subscriber process

`Phoenix.PubSub` is a behaviour. The adapter (`:pg` by default) handles fanout. Subscribers register with `PubSub.subscribe/2`; each subscription is a monitored link on the calling process.

### 2. `broadcast/3` vs `local_broadcast/3`

- `PubSub.broadcast/3` — fanout to all nodes in the cluster (if the adapter supports it).
- `PubSub.local_broadcast/3` — skip the cluster dispatch, deliver only to local subscribers. Cheaper; use when you know the subscribers are on this node (e.g., the LV sessions for one user).

### 3. Fastlane

A subscriber can register a "fastlane" — a transport (typically `Phoenix.Channel.Server`) that serializes the message once and pushes the serialized frame directly to sockets, bypassing the Elixir message queue. For a room with 10k users on one node, fastlane is 50x faster than plain broadcast.

### 4. Shard count

`Phoenix.PubSub` spawns `N` shards (default `System.schedulers_online/0`). Messages are dispatched by `:erlang.phash2(topic, N)`. On a 32-core box with 10k topics, this spreads broadcast load across 32 shard processes.

### 5. `:pg` scope

`Phoenix.PubSub` uses a named `:pg` scope per PubSub server. Scopes are isolated; two apps on the same node with different PubSub servers do not cross-talk.

## Design decisions

- **Option A — local only (`:ets`-backed `Registry`)**: perfect for single-node dev. Fails the moment you add node 2.
- **Option B — Redis pub/sub**: cross-cluster, adds 1–5ms latency, extra dependency.
- **Option C — `Phoenix.PubSub.PG2` (actually `:pg`)**: native BEAM distribution, sub-millisecond within a rack, no extra deps.
- **Option D — `Phoenix.PubSub.Redis`**: use when nodes cannot distribute (different BEAM cookies, firewalled, k8s cross-region).

Chosen: Option C for the cluster, with `local_broadcast/3` when we know the fanout is node-local.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule EventsBus.MixProject do
  use Mix.Project
  def project, do: [app: :events_bus, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  def application, do: [mod: {EventsBus.Application, []}, extra_applications: [:logger]]

  defp deps do
    [
      {:phoenix, "~> 1.7.14"},
      {:phoenix_pubsub, "~> 2.1"},
      {:jason, "~> 1.4"},
      {:benchee, "~> 1.3", only: :dev}
    ]
  end
end
```

### Dependencies (mix.exs)

```elixir
```elixir
defmodule EventsBus.MixProject do
  use Mix.Project
  def project, do: [app: :events_bus, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  def application, do: [mod: {EventsBus.Application, []}, extra_applications: [:logger]]

  defp deps do
    [
      {:phoenix, "~> 1.7.14"},
      {:phoenix_pubsub, "~> 2.1"},
      {:jason, "~> 1.4"},
      {:benchee, "~> 1.3", only: :dev}
    ]
  end
end
```

### Step 1: Application — `lib/events_bus/application.ex`

**Objective**: Start `Phoenix.PubSub` with the `PG2` adapter and a per-scheduler pool so cluster fan-out survives a node restart independently of consumers.

```elixir
defmodule EventsBus.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {Phoenix.PubSub,
       name: EventsBus.PubSub,
       adapter: Phoenix.PubSub.PG2,
       pool_size: System.schedulers_online()}
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: EventsBus.Supervisor)
  end
end
```

### Step 2: Listener GenServer — `lib/events_bus/listener.ex`

**Objective**: Subscribe inside `init/1` so the process is guaranteed to receive every message published after `start_link/1` returns.

```elixir
defmodule EventsBus.Listener do
  use GenServer

  def start_link(topic), do: GenServer.start_link(__MODULE__, topic)

  @impl true
  def init(topic) do
    :ok = Phoenix.PubSub.subscribe(EventsBus.PubSub, topic)
    {:ok, %{topic: topic, received: []}}
  end

  def received(pid), do: GenServer.call(pid, :received)

  @impl true
  def handle_call(:received, _from, state), do: {:reply, Enum.reverse(state.received), state}

  @impl true
  def handle_info(message, state) do
    {:noreply, update_in(state.received, &[message | &1])}
  end
end
```

### Step 3: Cache invalidator — `lib/events_bus/cache_invalidator.ex`

**Objective**: Split cluster-wide `broadcast/3` from `local_broadcast/3` so node-bound UI pushes skip PG2 gossip and cache invalidations still fan out everywhere.

```elixir
defmodule EventsBus.CacheInvalidator do
  @moduledoc """
  Demonstrates both cluster-wide and node-local broadcast.
  Use cluster-wide for cache invalidations that must reach every node.
  Use node-local for per-user UI updates that are bound to one LV session on one node.
  """

  @pubsub EventsBus.PubSub

  def invalidate_cluster(cache_key) do
    Phoenix.PubSub.broadcast(@pubsub, "cache", {:invalidate, cache_key})
  end

  def push_local_ui(user_id, payload) do
    Phoenix.PubSub.local_broadcast(@pubsub, "user:#{user_id}", {:ui, payload})
  end
end
```

## Why this works

`Phoenix.PubSub.PG2` registers the PubSub server in a `:pg` scope named after the server (e.g., `EventsBus.PubSub`). `broadcast/3` issues `:pg.get_members/1` and sends the message to every remote shard process. The remote shard dispatches to local subscribers via `Registry`.

`local_broadcast/3` skips `:pg` entirely — it reads the local `Registry` directly. That saves one inter-node hop and, more importantly, avoids serializing the Erlang term over the distribution channel.

`pool_size: schedulers_online` ensures broadcast work is spread across all CPU cores. On a single-shard config, you bottleneck at one process per PubSub server.

## Tests — `test/events_bus/pubsub_test.exs`

```elixir
defmodule EventsBus.PubSubTest do
  use ExUnit.Case, async: false
  alias Phoenix.PubSub
  alias EventsBus.{Listener, CacheInvalidator}

  setup do
    start_supervised!({Phoenix.PubSub, name: TestPubSub, adapter: Phoenix.PubSub.PG2})
    :ok
  end

  describe "subscribe/broadcast" do
    test "local subscriber receives a broadcast message" do
      :ok = PubSub.subscribe(TestPubSub, "topic")
      :ok = PubSub.broadcast(TestPubSub, "topic", {:hello, 1})
      assert_receive {:hello, 1}, 500
    end
  end

  describe "local_broadcast" do
    test "local_broadcast delivers to local subscribers" do
      :ok = PubSub.subscribe(TestPubSub, "topic2")
      :ok = PubSub.local_broadcast(TestPubSub, "topic2", :ping)
      assert_receive :ping, 500
    end
  end

  describe "cache invalidator" do
    test "invalidate_cluster fans out" do
      {:ok, l1} = Listener.start_link("cache")
      {:ok, l2} = Listener.start_link("cache")
      :ok = CacheInvalidator.invalidate_cluster(:user_42)
      Process.sleep(50)
      assert [{:invalidate, :user_42}] = Listener.received(l1)
      assert [{:invalidate, :user_42}] = Listener.received(l2)
    end
  end

  describe "topic scoping" do
    test "subscriber on topic A does not see topic B" do
      {:ok, la} = Listener.start_link("a")
      {:ok, lb} = Listener.start_link("b")
      :ok = PubSub.broadcast(EventsBus.PubSub, "a", :for_a)
      Process.sleep(20)
      assert [:for_a] = Listener.received(la)
      assert [] = Listener.received(lb)
    end
  end
end
```

## Benchmark — `bench/broadcast_bench.exs`

```elixir
{:ok, _} = Application.ensure_all_started(:events_bus)

# Subscribe 1_000 processes to the same topic on this node.
for _ <- 1..1_000 do
  spawn(fn ->
    Phoenix.PubSub.subscribe(EventsBus.PubSub, "bench")
    Process.sleep(:infinity)
  end)
end

Process.sleep(100)

Benchee.run(
  %{
    "broadcast (cluster)" => fn ->
      Phoenix.PubSub.broadcast(EventsBus.PubSub, "bench", :msg)
    end,
    "local_broadcast" => fn ->
      Phoenix.PubSub.local_broadcast(EventsBus.PubSub, "bench", :msg)
    end
  },
  time: 3,
  warmup: 1
)
```

**Expected (single node, 1k subs)**: `local_broadcast` ~250µs, `broadcast` ~400µs. With a second node in the cluster, `broadcast` adds one RTT (typically 100–300µs intra-rack).

## Advanced Considerations: LiveView Real-Time Patterns and Pubsub Scale

LiveView bridges the browser and BEAM via WebSocket, allowing server-side renders to push incremental DOM diffs to the client. A LiveView process is long-lived, receiving events (clicks, form submissions) and broadcasting updates. For real-time features (collaborative editing, live notifications), LiveView processes subscribe to PubSub topics and receive broadcast messages.

Phoenix.PubSub partitions topics across a pool of processes, allowing horizontal scaling. By default, `:local` mode uses in-memory ETS; `:redis` mode distributes across nodes via Redis. At scale (thousands of concurrent LiveViews), topic fanout can bottleneck: broadcasting to a million subscribers means delivering one million messages. The BEAM handles this, but the network cost matters on multi-node deployments.

`Presence` module tracks which users are viewing which pages, syncing state via PubSub. A presence join/leave is broadcast to all nodes, allowing real-time "who's online" updates. Under partition, presence state can diverge; the library uses unique presence keys to detect and reconcile. Operationally, watching presence on every page load can amplify server load if users are flaky (mobile networks, browser reloads). Consider presence only for features where it's user-facing (collaborative editors, live sports scoreboards).

---


## Deep Dive: Phoenix Patterns and Production Implications

Phoenix's conn struct represents an HTTP request/response in flight, accumulating transformations through middleware and handler code. Testing a Phoenix endpoint end-to-end (not just the controller) catches middleware order bugs, header mismatches, and plug composition issues. The trade-off is that full integration tests are slower and harder to parallelize than unit tests. Production bugs in auth, CORS, or session handling are often due to middleware assumptions that live tests reveal.

---

## Trade-offs and production gotchas

**1. `broadcast_from!/4` exists for a reason.** If you broadcast from inside the topic you subscribed to, you receive your own message. `broadcast_from(pubsub, self(), topic, msg)` excludes yourself.

**2. Ordering is not global.** Two concurrent broadcasts from node A and node B arrive in different orders on different subscribers. Never assume causal ordering.

**3. Large payloads cost double on cluster broadcast.** The term is serialized once per receiving node. A 1 MB payload broadcast to 10 nodes sends 10 MB across the distribution channel. Send ids, not blobs.

**4. Shard pool sizing.** `pool_size: 1` serializes everything — suitable only for dev. Use `System.schedulers_online/0` as a floor.

**5. `local_broadcast` bypasses cluster dispatch silently.** If you split a feature between `local_broadcast` (per-user UI) and `broadcast` (cache invalidation) and forget which one, a deploy that moves the user's LV to another node quietly breaks. Centralize the choice in a helper like `CacheInvalidator`.

**6. When NOT to use `:pg`.** Multi-DC, firewalled nodes, or polyglot consumers. Use `Phoenix.PubSub.Redis` or a full broker (NATS, Kafka).

## Reflection

Your company expands from one datacenter to three, with 150ms RTT between them. You have a single PubSub serving all three. List the symptoms you will see in the logs, and decide: do you move to `Phoenix.PubSub.Redis`, use `:pg` with cross-DC Erlang distribution, or introduce per-DC PubSubs with a manual replication layer?


## Executable Example

```elixir
defmodule EventsBus.PubSubTest do
  use ExUnit.Case, async: false
  alias Phoenix.PubSub
  alias EventsBus.{Listener, CacheInvalidator}

  setup do
    start_supervised!({Phoenix.PubSub, name: TestPubSub, adapter: Phoenix.PubSub.PG2})
    :ok
  end

  describe "subscribe/broadcast" do
    test "local subscriber receives a broadcast message" do
      :ok = PubSub.subscribe(TestPubSub, "topic")
      :ok = PubSub.broadcast(TestPubSub, "topic", {:hello, 1})
      assert_receive {:hello, 1}, 500
    end
  end

  describe "local_broadcast" do
    test "local_broadcast delivers to local subscribers" do
      :ok = PubSub.subscribe(TestPubSub, "topic2")
      :ok = PubSub.local_broadcast(TestPubSub, "topic2", :ping)
      assert_receive :ping, 500
    end
  end

  describe "cache invalidator" do
    test "invalidate_cluster fans out" do
      {:ok, l1} = Listener.start_link("cache")
      {:ok, l2} = Listener.start_link("cache")
      :ok = CacheInvalidator.invalidate_cluster(:user_42)
      Process.sleep(50)
      assert [{:invalidate, :user_42}] = Listener.received(l1)
      assert [{:invalidate, :user_42}] = Listener.received(l2)
    end
  end

  describe "topic scoping" do
    test "subscriber on topic A does not see topic B" do
      {:ok, la} = Listener.start_link("a")
      {:ok, lb} = Listener.start_link("b")
      :ok = PubSub.broadcast(EventsBus.PubSub, "a", :for_a)
      Process.sleep(20)
      assert [:for_a] = Listener.received(la)
      assert [] = Listener.received(lb)
    end
  end
end

defmodule Main do
  def main do
    IO.puts("✓ Phoenix.PubSub Local vs Distributed (`:pg` adapter)")
  - Demonstrating core concepts
    - Implementation patterns and best practices
  end
end

Main.main()
```
