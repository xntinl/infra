# Phoenix Presence — CRDT-Backed User Tracking

**Project**: `presence_channels` — "who's online in this chat room" across a multi-node Phoenix cluster.

---

## The business problem

You are building a chat backend where product wants a live "who's here"
indicator in each room: agent avatars on the left, customer avatars on the right,
with typing indicators. The tricky part is that the app runs on three Phoenix
nodes behind a load balancer. User A's WebSocket may land on node 1; user B's
on node 2. Both must see each other, and when node 1 restarts, user A's presence
must disappear from node 2's view.

The classical shared-nothing approach — a central Redis with TTLs — has two
problems:

1. **Single point of failure**. If Redis goes down, presence is lost across
   the cluster.
2. **Staleness**. TTL-based expiry is coarse (typical 30s); a user who closes
   their laptop takes 30s to disappear.

Phoenix Presence solves both by replicating presence state as a CRDT (ORSWOT —
Observed-Remove Set Without Tombstones) across the cluster. Each node gossips
its local presence to peers; merges are commutative and conflict-free. No
Redis, no coordinator, no quorum. When a node dies, BEAM's process monitoring
(`:net_kernel.monitor_nodes/1`) triggers removal of its entries from every
peer.

Requirements for this exercise:

1. Track `{user_id, meta}` per room topic.
2. Broadcast join/leave diffs to clients subscribed to the room.
3. Support metadata updates (typing indicator) without rejoining.
4. Work across at least two nodes — tests include a cluster scenario.

Project structure at this point:

## Project structure

```
presence_channels/
├── lib/
│   ├── presence_channels/
│   │   ├── application.ex
│   │   ├── auth.ex
│   │   └── presence.ex                # use Phoenix.Presence
│   └── presence_channels_web/
│       ├── endpoint.ex
│       ├── channels/
│       │   ├── user_socket.ex
│       │   └── room_channel.ex
│       └── telemetry.ex
├── test/
│   └── presence_channels_web/
│       └── channels/
│           └── room_channel_test.exs
├── script/
│   └── main.exs
└── mix.exs
```

---

## Why Presence and not roster-by-hand

A per-node roster is straightforward; a distributed roster with netsplit recovery is a distributed systems project. Phoenix.Presence packages the hard parts.

---

## Design decisions

**Option A — custom roster GenServer + broadcasts**
- Pros: full control; simple per-node model.
- Cons: cross-node replication and netsplit recovery are your problem.

**Option B — Phoenix.Presence** (chosen)
- Pros: distributed, CRDT-backed; drops into any channel with a `handle_info({:after_join, ...}, ...)`.
- Cons: eventual consistency; metadata needs care to stay small.

→ Chose **B** because distributed presence is a solved problem; custom versions keep rediscovering the same bugs.

---

## Implementation

### Step 1: Create the project and the Presence module

**Objective**: Use `Phoenix.Presence` with a `fetch/2` callback so metas are enriched server-side — clients never forge display names.

```bash
mix phx.new presence_channels --no-ecto --no-mailer --no-html
cd presence_channels
```

`lib/presence_channels/presence.ex`:

```elixir
defmodule PresenceChannels.Presence do
  @moduledoc """
  Presence tracker. The `fetch/2` callback is called on every diff and on
  initial state to enrich metas — we use it to attach display names without
  asking the client to send them.
  """
  use Phoenix.Presence,
    otp_app: :presence_channels,
    pubsub_server: PresenceChannels.PubSub

  @impl true
  def fetch(_topic, presences) do
    Enum.into(presences, %{}, fn {key, %{metas: metas}} ->
      display = display_for(key)
      {key, %{metas: metas, display_name: display}}
    end)
  end

  defp display_for("user-" <> n), do: "User #{n}"
  defp display_for(other), do: other
end
```

### Step 2: Supervise Presence

**Objective**: Order children so `PubSub` starts before `Presence` before `Endpoint` — later children depend on earlier ones.

```elixir
# lib/presence_channels/application.ex — children list
children = [
  PresenceChannelsWeb.Telemetry,
  {Phoenix.PubSub, name: PresenceChannels.PubSub},
  PresenceChannels.Presence,
  PresenceChannelsWeb.Endpoint
]
```

The order matters: `PubSub` must start before `Presence`, `Presence` before
the `Endpoint` (channels reference it).

### Step 3: UserSocket

**Objective**: Authenticate sockets via `Phoenix.Token` so presence keys come from verified identities, never client-supplied strings.

```elixir
defmodule PresenceChannelsWeb.UserSocket do
  use Phoenix.Socket

  channel "room:*", PresenceChannelsWeb.RoomChannel

  @impl true
  def connect(%{"token" => token}, socket, _connect_info) do
    case PresenceChannels.Auth.verify(PresenceChannelsWeb.Endpoint, token) do
      {:ok, user_id} -> {:ok, assign(socket, :user_id, user_id)}
      {:error, _} -> :error
    end
  end

  def connect(_params, _socket, _connect_info), do: :error

  @impl true
  def id(socket), do: "user_socket:#{socket.assigns.user_id}"
end
```

(The `PresenceChannels.Auth` module mirrors the earlier chat channel's auth,
using `Phoenix.Token.sign/verify` — included in the starter repo.)

### Step 4: RoomChannel

**Objective**: Call `Presence.track/3` from `:after_join` so tracking runs after subscription is established, avoiding a race.

```elixir
defmodule PresenceChannelsWeb.RoomChannel do
  use Phoenix.Channel

  alias PresenceChannels.Presence

  @impl true
  def join("room:" <> _rest, _payload, socket) do
    send(self(), :after_join)
    {:ok, socket}
  end

  @impl true
  def handle_info(:after_join, socket) do
    {:ok, _ref} =
      Presence.track(socket, socket.assigns.user_id, %{
        online_at: System.system_time(:second),
        typing?: false,
        node: Node.self()
      })

    push(socket, "presence_state", Presence.list(socket))
    {:noreply, socket}
  end

  @impl true
  def handle_in("typing", %{"typing?" => typing?}, socket) when is_boolean(typing?) do
    {:ok, _} =
      Presence.update(socket, socket.assigns.user_id, fn meta ->
        Map.put(meta, :typing?, typing?)
      end)

    {:reply, :ok, socket}
  end

  def handle_in(_other, _payload, socket), do: {:reply, {:error, :unknown_event}, socket}
end
```

Two important details:

- `Presence.track/3` must be called from the channel process **after** join
  completes. That's why we use `send(self(), :after_join)` — calling `track`
  inside `join/3` races with the subscription setup.
- `push(socket, "presence_state", ...)` sends the initial state to the newly
  joined client. From then on, the framework emits `presence_diff` events
  automatically.

### `test/presence_channels_test.exs`

**Objective**: Assert `presence_state` and `presence_diff` pushes with `async: false` so shared tracker CRDT state is deterministic across scenarios.

```elixir
defmodule PresenceChannelsWeb.RoomChannelTest do
  use PresenceChannelsWeb.ChannelCase, async: false
  # async: false because Presence state is shared across tests

  alias PresenceChannels.{Auth, Presence}
  alias PresenceChannelsWeb.{Endpoint, UserSocket, RoomChannel}

  defp join_as(user_id, topic) do
    token = Auth.sign(Endpoint, user_id)
    {:ok, socket} = connect(UserSocket, %{"token" => token})
    subscribe_and_join(socket, RoomChannel, topic)
  end

  describe "presence lifecycle" do
    test "tracks user on join and broadcasts state" do
      {:ok, _, _channel} = join_as("user-1", "room:test:1")

      assert_push "presence_state", state
      assert Map.has_key?(state, "user-1")
    end

    test "two users see each other" do
      {:ok, _, channel_a} = join_as("user-1", "room:test:2")
      assert_push "presence_state", _

      {:ok, _, _channel_b} = join_as("user-2", "room:test:2")

      # A receives a diff announcing B's join
      assert_push "presence_diff", %{joins: joins, leaves: leaves}
      assert Map.has_key?(joins, "user-2")
      assert leaves == %{}

      # A still sees both
      list = Presence.list(channel_a)
      assert Map.has_key?(list, "user-1")
      assert Map.has_key?(list, "user-2")
    end

    test "typing update emits a leave+join diff" do
      {:ok, _, _channel} = join_as("user-3", "room:test:3")
      assert_push "presence_state", _

      {:ok, _, channel} = join_as("user-4", "room:test:3")
      assert_push "presence_state", _

      ref = push(channel, "typing", %{"typing?" => true})
      assert_reply ref, :ok

      assert_push "presence_diff", %{joins: joins, leaves: leaves}
      assert Map.has_key?(joins, "user-4")
      assert Map.has_key?(leaves, "user-4")
      [meta | _] = joins["user-4"][:metas]
      assert meta.typing? == true
    end

    test "leave removes presence on channel termination" do
      {:ok, _, channel} = join_as("user-5", "room:test:4")
      assert_push "presence_state", _

      Process.unlink(channel.channel_pid)
      ref = leave(channel)
      assert_reply ref, :ok

      # Re-join fresh to inspect state
      {:ok, _, channel2} = join_as("user-6", "room:test:4")
      assert_push "presence_state", state
      refute Map.has_key?(state, "user-5")
      _ = channel2
    end
  end

  describe "enriched metadata via fetch/2" do
    test "injects display_name without client input" do
      {:ok, _, _channel} = join_as("user-7", "room:test:5")
      assert_push "presence_state", state
      assert state["user-7"].display_name == "User 7"
    end
  end
end
```

### Step 6: Multi-node smoke test (optional but recommended)

**Objective**: Connect two `--sname` nodes with a shared cookie so the Presence CRDT merges across the cluster and diffs cross the BEAM boundary.

Start two nodes in separate terminals:

```bash
iex --sname node_a --cookie secret -S mix phx.server
iex --sname node_b --cookie secret -S mix phx.server
```

From either node:

```elixir
Node.connect(:"node_b@hostname")
# Now open two browser tabs, one per node, join the same room.
# Users from both nodes appear in each other's presence list.
```

### Why this works

On channel join, the channel tracks the user via `Phoenix.Presence.track/3`. The Presence module rides on Phoenix.Tracker, which gossips state as a CRDT. `list/1` returns the current roster, with metas merged across connections.

---

## Advanced Considerations: LiveView Real-Time Patterns and Pubsub Scale

LiveView bridges the browser and BEAM via WebSocket, allowing server-side renders to push incremental DOM diffs to the client. A LiveView process is long-lived, receiving events (clicks, form submissions) and broadcasting updates. For real-time features (collaborative editing, live notifications), LiveView processes subscribe to PubSub topics and receive broadcast messages.

Phoenix.PubSub partitions topics across a pool of processes, allowing horizontal scaling. By default, `:local` mode uses in-memory ETS; `:redis` mode distributes across nodes via Redis. At scale (thousands of concurrent LiveViews), topic fanout can bottleneck: broadcasting to a million subscribers means delivering one million messages. The BEAM handles this, but the network cost matters on multi-node deployments.

`Presence` module tracks which users are viewing which pages, syncing state via PubSub. A presence join/leave is broadcast to all nodes, allowing real-time "who's online" updates. Under partition, presence state can diverge; the library uses unique presence keys to detect and reconcile. Operationally, watching presence on every page load can amplify server load if users are flaky (mobile networks, browser reloads). Consider presence only for features where it's user-facing (collaborative editors, live sports scoreboards).

---

## Deep Dive: Phoenix Patterns and Production Implications

Phoenix's conn struct represents an HTTP request/response in flight, accumulating transformations through middleware and handler code. Testing a Phoenix endpoint end-to-end (not just the controller) catches middleware order bugs, header mismatches, and plug composition issues. The trade-off is that full integration tests are slower and harder to parallelize than unit tests. Production bugs in auth, CORS, or session handling are often due to middleware assumptions that live tests reveal.

---

## Trade-offs and production gotchas

**1. Presence is not a persistence layer**
Presence state lives in RAM. If the entire cluster restarts, presence is gone.
This is correct semantics ("who is connected RIGHT NOW") but don't use it for
"last seen" — use Ecto for that.

**2. Meta should be small**
Every update ships the full meta in a diff. A 10KB meta × 1000 users × 10 updates/s =
100MB/s of gossip. Keep metas to keys the UI actually renders. Put heavy data
behind a separate query.

**3. Clock skew in `online_at`**
`System.system_time(:second)` varies across nodes by NTP skew. If your UI shows
"connected 3s ago" computed from `online_at`, it can go negative on a peer with
a slightly fast clock. Compute duration client-side relative to when the meta
arrived.

**4. `fetch/2` runs on every list call**
Don't do DB queries there. It's called when the initial state is computed and
when clients call `Presence.list`. For display names, cache in ETS or pre-compute.

**5. `Presence.track` vs. `Presence.track(pid, ...)`**
The channel variant binds the tracked entry to the channel's pid — when the
pid dies, the entry is removed. The pid variant binds to an arbitrary process.
Pick the right lifetime owner.

**6. Cluster misconfiguration is silent**
If nodes are running but not clustered (`Node.list/0` is empty), presence still
works within a single node. You won't see errors — just missing users. Add a
startup check that logs cluster membership.

**7. `:after_join` vs. `join/3`**
Tracking inside `join/3` can race with the framework's subscription setup:
the client may receive the first `presence_diff` before the `presence_state`,
causing a momentarily inconsistent UI. Always track in `handle_info(:after_join, ...)`.

**8. When NOT to use Presence**
If you need to query "who is online" from a non-Elixir service (analytics, a
Python worker), CRDT state in-process is the wrong shape. Pair Presence with
a projection: a GenServer that subscribes to presence diffs and writes to
Postgres, so external systems can read a stable row.

---

## Performance notes

Phoenix Presence scales in published benchmarks to hundreds of thousands of
tracked entries per node. The hot path is the diff merge on `:presence_diff`
broadcasts. Measure with `:telemetry`:

```elixir
:telemetry.attach(
  "presence-diff",
  [:phoenix, :presence, :broadcast],
  fn _event, measurements, metadata, _ ->
    IO.inspect({measurements, metadata.topic}, label: "presence broadcast")
  end,
  nil
)
```

If diffs become CPU-heavy, look at (a) meta size, (b) topic fan-out (10k
subscribers per topic is a lot — consider sharding rooms).

---

## Benchmark

```elixir
# :timer.tc / Benchee measurement sketch
{time_us, _} = :timer.tc(fn -> :ok end)
IO.puts("elapsed: #{time_us} us")
```

Target: join-to-roster-visible latency under 100 ms cluster-wide; roster size bounded by meta payload.

---

## Reflection

- Your presence list shows a user as online for 30 seconds after they disconnect. Which timer is at fault, and is that the right default for your app?
- If the channel process crashes, what does Presence do, and does the user experience a flicker in the roster? How do you test that?

---

### `script/main.exs`
```elixir
defmodule PresenceChannelsWeb.RoomChannelTest do
  use PresenceChannelsWeb.ChannelCase, async: false
  # async: false because Presence state is shared across tests

  alias PresenceChannels.{Auth, Presence}
  alias PresenceChannelsWeb.{Endpoint, UserSocket, RoomChannel}

  defp join_as(user_id, topic) do
    token = Auth.sign(Endpoint, user_id)
    {:ok, socket} = connect(UserSocket, %{"token" => token})
    subscribe_and_join(socket, RoomChannel, topic)
  end

  describe "presence lifecycle" do
    test "tracks user on join and broadcasts state" do
      {:ok, _, _channel} = join_as("user-1", "room:test:1")

      assert_push "presence_state", state
      assert Map.has_key?(state, "user-1")
    end

    test "two users see each other" do
      {:ok, _, channel_a} = join_as("user-1", "room:test:2")
      assert_push "presence_state", _

      {:ok, _, _channel_b} = join_as("user-2", "room:test:2")

      # A receives a diff announcing B's join
      assert_push "presence_diff", %{joins: joins, leaves: leaves}
      assert Map.has_key?(joins, "user-2")
      assert leaves == %{}

      # A still sees both
      list = Presence.list(channel_a)
      assert Map.has_key?(list, "user-1")
      assert Map.has_key?(list, "user-2")
    end

    test "typing update emits a leave+join diff" do
      {:ok, _, _channel} = join_as("user-3", "room:test:3")
      assert_push "presence_state", _

      {:ok, _, channel} = join_as("user-4", "room:test:3")
      assert_push "presence_state", _

      ref = push(channel, "typing", %{"typing?" => true})
      assert_reply ref, :ok

      assert_push "presence_diff", %{joins: joins, leaves: leaves}
      assert Map.has_key?(joins, "user-4")
      assert Map.has_key?(leaves, "user-4")
      [meta | _] = joins["user-4"][:metas]
      assert meta.typing? == true
    end

    test "leave removes presence on channel termination" do
      {:ok, _, channel} = join_as("user-5", "room:test:4")
      assert_push "presence_state", _

      Process.unlink(channel.channel_pid)
      ref = leave(channel)
      assert_reply ref, :ok

      # Re-join fresh to inspect state
      {:ok, _, channel2} = join_as("user-6", "room:test:4")
      assert_push "presence_state", state
      refute Map.has_key?(state, "user-5")
      _ = channel2
    end
  end

  describe "enriched metadata via fetch/2" do
    test "injects display_name without client input" do
      {:ok, _, _channel} = join_as("user-7", "room:test:5")
      assert_push "presence_state", state
      assert state["user-7"].display_name == "User 7"
    end
  end
end

defmodule Main do
  def main do
    IO.puts("✓ Phoenix Presence — CRDT-Backed User Tracking")
  - Demonstrating core concepts
    - Implementation patterns and best practices
  end
end

Main.main()
```

---

## Why Phoenix Presence — CRDT-Backed User Tracking matters

Mastering **Phoenix Presence — CRDT-Backed User Tracking** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `lib/presence_channels.ex`

```elixir
defmodule PresenceChannels do
  @moduledoc """
  Reference implementation for Phoenix Presence — CRDT-Backed User Tracking.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the presence_channels module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> PresenceChannels.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```

---

## Key concepts

### 1. Why CRDTs for presence

Presence is a set of `{key, meta}` entries per topic. Two nodes observe slightly
different sets because of network delay. The classical mutex-based sync would
serialize through a coordinator; CRDTs let each node merge independently and
always converge to the same result.

Phoenix Presence uses an **ORSWOT** (Observed-Remove Set Without Tombstones):

```
Node A: {"user-1" => [meta_A]}    ─┐
                                    ├─▶ gossip → merge → {"user-1" => [meta_A, meta_B]}
Node B: {"user-1" => [meta_B]}    ─┘
```

The same key can appear with multiple metas (one per connection). A user with
three tabs open shows up as one presence key with three entries in the metas list.

---

### 2. Diffs, not full state

Presence broadcasts deltas: `joins` and `leaves`. The client state is built by
applying diffs to the initial `presence_state` payload received on join:

```
server        client
  │    presence_state {u1: [m1], u2: [m2]}
  │───────────────────────────────────────▶
  │    presence_diff  {joins: {u3: [m3]}, leaves: {}}
  │───────────────────────────────────────▶
  │    presence_diff  {joins: {}, leaves: {u2: [m2]}}
  │───────────────────────────────────────▶
```

This keeps the wire payload proportional to the changes, not the total presence.
In a 1000-user room, one person joining transmits ~100 bytes, not 100KB.

---

### 3. Keys and metas

### `mix.exs`
```elixir
defmodule PhoenixPresenceChannels.MixProject do
  use Mix.Project

  def project do
    [
      app: :phoenix_presence_channels,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    [# No external dependencies — pure Elixir]
  end
end
```

```elixir
Presence.track(socket, _key = "user-1", _meta = %{online_at: 1234, typing?: false})
```

Key is typically `user_id`. Meta is a map — whatever your UI needs. You can call
`track/3` multiple times with the same key but different metas to represent
multiple connections. Each `untrack/2` (or process exit) removes one meta,
not the whole key.

---

### 4. Updating meta without rejoining

Use `Presence.update/4`:

```elixir
Presence.update(socket, user_id, &Map.put(&1, :typing?, true))
```

This emits a `leave` + `join` diff (the only way to represent a meta change in
an observed-remove set). Clients reconcile: same key appears in both `leaves`
and `joins` within one diff — they should replace the meta rather than animating
an exit/enter.

---

### 5. Node failure is handled by BEAM

When a node leaves the cluster, its PubSub-over-`:pg` membership drops, and
every peer removes that node's tracked keys. No TTL polling. No heartbeat
timeout to tune. This is a major reason to prefer Presence over ad-hoc
Redis-based solutions.

---
