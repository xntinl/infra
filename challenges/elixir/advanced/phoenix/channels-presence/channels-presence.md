# Phoenix Channels with Presence for Multi-Node Collaboration

**Project**: `collab_room` — a chat channel with presence tracking that works across a cluster of Phoenix nodes, with diff-based presence events instead of full list broadcasts.

## The business problem

You run a customer-support platform. Agents and customers meet in rooms. Product wants "who is typing", "who is online", and "last seen". The previous implementation stored presence in Redis and polled every 5 seconds. Latency was 3–7 seconds and the Redis box was a single point of failure.

`Phoenix.Presence` uses a CRDT on top of `Phoenix.PubSub` to replicate presence state across the cluster in O(log n) and deliver diffs (joined/left) rather than the full list. It needs no external store.

## Project structure

```
collab_room/
├── lib/
│   ├── collab_room/
│   │   ├── application.ex
│   │   └── presence.ex
│   └── collab_room_web/
│       ├── endpoint.ex
│       ├── user_socket.ex
│       └── channels/
│           └── room_channel.ex
├── test/
│   └── collab_room_web/
│       └── channels/
│           └── room_channel_test.exs
├── script/
│   └── main.exs
└── mix.exs
```

## Why Phoenix.Presence and not Redis pub/sub

Redis pub/sub gives you broadcast; it does NOT give you a shared state snapshot. You would still need a Redis `HASH` per room and manual diffing — and you are back to polling to reconstruct state after a reconnect.

`Phoenix.Presence` is a state-based CRDT (specifically a PN-ORSWOT; see the Loeffler-Nicoll paper). Every node keeps its own view of who is present; when nodes gossip through `Phoenix.PubSub`, views converge without conflicts. Clients get `presence_diff` events that include only joiners and leavers — no full rebroadcast.

**Why not a GenServer with `{:DOWN, ...}` tracking?** Works on a single node. Breaks the second you add a second node: the GenServer does not see sockets on the other node. You would end up reimplementing Presence, poorly.

## Design decisions

- **Option A — GenServer registry, local only**: fastest, breaks across nodes.
- **Option B — Redis, polled**: cross-node, 3–7s stale, SPOF.
- **Option C — `Phoenix.Presence`**: cross-node, diff-based, eventually consistent (sub-second under normal gossip), no external deps.

Chosen: Option C. If you need strict consistency (e.g., "exactly one user can hold this lock"), Presence is the wrong tool — use `:global.register_name/2` or a `Registry` with a single coordinator.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule CollabRoom.MixProject do
  use Mix.Project
  def project, do: [app: :collab_room, version: "0.1.0", elixir: "~> 1.19", deps: deps()]

  def application do
    [mod: {CollabRoom.Application, []}, extra_applications: [:logger, :crypto]]
  end

  defp deps do
    [
      {:phoenix, "~> 1.7.14"},
      {:phoenix_pubsub, "~> 2.1"},
      {:jason, "~> 1.4"},
      {:plug_cowboy, "~> 2.7"}
    ]
  end
end
```

### `mix.exs`
```elixir
defmodule ChannelsPresence.MixProject do
  use Mix.Project

  def project do
    [
      app: :channels_presence,
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
    []
  end
end
```elixir
defmodule CollabRoom.MixProject do
  use Mix.Project
  def project, do: [app: :collab_room, version: "0.1.0", elixir: "~> 1.19", deps: deps()]

  def application do
    [mod: {CollabRoom.Application, []}, extra_applications: [:logger, :crypto]]
  end

  defp deps do
    [
      {:phoenix, "~> 1.7.14"},
      {:phoenix_pubsub, "~> 2.1"},
      {:jason, "~> 1.4"},
      {:plug_cowboy, "~> 2.7"}
    ]
  end
end
end
```

### Step 1: Presence module — `lib/collab_room/presence.ex`

**Objective**: Enrich presence metas via `fetch/2` so profile lookups batch once per broadcast, not N+1 per subscriber.

```elixir
defmodule CollabRoom.Presence do
  use Phoenix.Presence,
    otp_app: :collab_room,
    pubsub_server: CollabRoom.PubSub

  @doc """
  Override `fetch/2` to enrich meta with profile info.
  Called ONCE per key per broadcast — cheap way to avoid N+1 DB queries.
  """
  def fetch(_topic, presences) do
    query_users(Map.keys(presences))
    |> Enum.reduce(presences, fn {id, profile}, acc ->
      update_in(acc[id].metas, fn metas ->
        Enum.map(metas, &Map.put(&1, :profile, profile))
      end)
    end)
  end

  defp query_users(ids) do
    # Stub: in production, batch-fetch from Ecto. Here we fake it.
    for id <- ids, into: %{} do
      {id, %{name: "User #{id}", avatar: "/a/#{id}.png"}}
    end
  end
end
```

### Step 2: Application — `lib/collab_room/application.ex`

**Objective**: Start `PubSub` before `Presence` before `Endpoint` so tracker CRDTs replicate before any socket connects.

```elixir
defmodule CollabRoom.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {Phoenix.PubSub, name: CollabRoom.PubSub},
      CollabRoom.Presence,
      CollabRoomWeb.Endpoint
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: CollabRoom.Supervisor)
  end
end
```

### Step 3: Channel — `lib/collab_room_web/channels/room_channel.ex`

**Objective**: Defer `Presence.track/3` to `:after_join` so the subscription exists before tracking, preventing a lost initial `presence_diff`.

```elixir
defmodule CollabRoomWeb.RoomChannel do
  use Phoenix.Channel
  alias CollabRoom.Presence

  @impl true
  def join("room:" <> room_id, %{"user_id" => user_id}, socket) do
    send(self(), {:after_join, user_id})
    {:ok, assign(socket, room_id: room_id, user_id: user_id)}
  end

  @impl true
  def handle_info({:after_join, user_id}, socket) do
    {:ok, _ref} =
      Presence.track(socket, to_string(user_id), %{
        online_at: System.system_time(:second),
        typing?: false
      })

    push(socket, "presence_state", Presence.list(socket))
    {:noreply, socket}
  end

  @impl true
  def handle_in("typing", %{"typing?" => typing?}, socket) do
    {:ok, _ref} =
      Presence.update(socket, to_string(socket.assigns.user_id), fn meta ->
        %{meta | typing?: typing?}
      end)

    {:noreply, socket}
  end

  def handle_in("message", %{"body" => body}, socket) do
    broadcast!(socket, "message", %{
      user_id: socket.assigns.user_id,
      body: body,
      at: System.system_time(:millisecond)
    })

    {:noreply, socket}
  end
end
```

### Step 4: Socket — `lib/collab_room_web/user_socket.ex`

**Objective**: Authenticate via `Phoenix.Token.verify/4` in `connect/3` so presence keys come from a signed identity, never from channel params.

```elixir
defmodule CollabRoomWeb.UserSocket do
  use Phoenix.Socket

  channel "room:*", CollabRoomWeb.RoomChannel

  @impl true
  def connect(%{"token" => token}, socket, _connect_info) do
    case Phoenix.Token.verify(CollabRoomWeb.Endpoint, "user socket", token, max_age: 86_400) do
      {:ok, user_id} -> {:ok, assign(socket, user_id: user_id)}
      {:error, _} -> :error
    end
  end

  @impl true
  def id(socket), do: "user_socket:#{socket.assigns.user_id}"
end
```

## Why this works

`Presence.track(socket, key, meta)` links the tracking lifecycle to the channel process pid. When the socket disconnects, the pid exits, `Phoenix.Tracker` detects it, and a `leaves` diff is broadcast within one heartbeat interval (default 1.5s). `Presence.update/3` triggers a differential broadcast carrying only the changed meta — not the full state.

Across nodes, `Phoenix.PubSub` (in `:pg` mode on OTP 24+) gossips Tracker deltas. Two agents on two different nodes see the same presence list within a gossip interval.

## Tests — `test/collab_room_web/channels/room_channel_test.exs`

```elixir
defmodule CollabRoomWeb.RoomChannelTest do
  use ExUnit.Case, async: false
  import Phoenix.ChannelTest

  @endpoint CollabRoomWeb.Endpoint

  setup do
    {:ok, _, socket} =
      CollabRoomWeb.UserSocket
      |> socket("user_socket:42", %{user_id: 42})
      |> subscribe_and_join(CollabRoomWeb.RoomChannel, "room:lobby", %{"user_id" => 42})

    {:ok, socket: socket}
  end

  describe "join" do
    test "receives presence_state immediately", %{socket: _} do
      assert_push "presence_state", state
      assert Map.has_key?(state, "42")
    end
  end

  describe "typing" do
    test "update triggers a presence_diff with new meta", %{socket: socket} do
      assert_push "presence_state", _
      push(socket, "typing", %{"typing?" => true})
      assert_broadcast "presence_diff", %{joins: joins}
      [%{typing?: true}] = get_in(joins, ["42", :metas])
    end
  end

  describe "message" do
    test "message is broadcast to the topic", %{socket: socket} do
      push(socket, "message", %{"body" => "hello"})
      assert_broadcast "message", %{user_id: 42, body: "hello"}
    end
  end

  describe "disconnect" do
    test "closing the socket emits a leave diff", %{socket: socket} do
      Process.unlink(socket.channel_pid)
      close(socket)
      assert_broadcast "presence_diff", %{leaves: %{"42" => _}}
    end
  end
end
```

## Benchmark

```elixir
# bench/presence_bench.exs
alias CollabRoom.Presence

{:ok, _} = Application.ensure_all_started(:collab_room)

pids =
  for i <- 1..10_000 do
    {:ok, pid} = Task.start_link(fn -> Process.sleep(:infinity) end)
    Presence.track(pid, "room:bench", to_string(i), %{})
    pid
  end

{us, _} = :timer.tc(fn -> Presence.list("room:bench") end)
IO.puts("list/1 for 10k presences: #{us}µs")

Enum.each(pids, &Process.exit(&1, :kill))
```

**Expected**: `list/1` for 10k presences < 20ms. If you see > 100ms, the `fetch/2` callback is doing synchronous DB work per key — batch the queries.

## Advanced Considerations: LiveView Real-Time Patterns and Pubsub Scale

LiveView bridges the browser and BEAM via WebSocket, allowing server-side renders to push incremental DOM diffs to the client. A LiveView process is long-lived, receiving events (clicks, form submissions) and broadcasting updates. For real-time features (collaborative editing, live notifications), LiveView processes subscribe to PubSub topics and receive broadcast messages.

Phoenix.PubSub partitions topics across a pool of processes, allowing horizontal scaling. By default, `:local` mode uses in-memory ETS; `:redis` mode distributes across nodes via Redis. At scale (thousands of concurrent LiveViews), topic fanout can bottleneck: broadcasting to a million subscribers means delivering one million messages. The BEAM handles this, but the network cost matters on multi-node deployments.

`Presence` module tracks which users are viewing which pages, syncing state via PubSub. A presence join/leave is broadcast to all nodes, allowing real-time "who's online" updates. Under partition, presence state can diverge; the library uses unique presence keys to detect and reconcile. Operationally, watching presence on every page load can amplify server load if users are flaky (mobile networks, browser reloads). Consider presence only for features where it's user-facing (collaborative editors, live sports scoreboards).

---

## Deep Dive: Phoenix Patterns and Production Implications

Phoenix's conn struct represents an HTTP request/response in flight, accumulating transformations through middleware and handler code. Testing a Phoenix endpoint end-to-end (not just the controller) catches middleware order bugs, header mismatches, and plug composition issues. The trade-off is that full integration tests are slower and harder to parallelize than unit tests. Production bugs in auth, CORS, or session handling are often due to middleware assumptions that live tests reveal.

---

## Trade-offs and production gotchas

**1. Eventual consistency.** Two tabs submitting simultaneously may see each other arrive out-of-order on remote nodes by up to one gossip interval. Never base auth decisions on Presence state.

**2. `fetch/2` runs on every broadcast.** If it does a DB query, latency spikes on large rooms. Use `Cachex` or `:persistent_term` if the profile rarely changes.

**3. Presence per metadata tab.** One user with 3 open tabs is ONE `key` with 3 metas. If you count `length(metas)`, remember to dedupe by user.

**4. `:down_period` tuning.** Default 15s is good for WAN clusters. For same-rack clusters, drop to 5s for faster leave detection; too low and network blips cause flapping.

**5. Presence does not replace a message log.** `presence_diff` is not durable — if the client was offline during the diff, it is lost. Snapshot on reconnect via `presence_state`.

**6. When NOT to use Presence.** For strict "exactly one worker" semantics, use `Horde.Registry` or `:global`. For high-throughput counters (thousands of updates per second per key), Presence is not the right shape; use a CRDT counter library (e.g., `DeltaCrdt` directly).

## Reflection

A PM asks for a "who was last seen when" feature — including users who are NOT currently connected. Presence only tracks live connections. Sketch the architecture change: what do you persist, where, and how do you reconcile it with Presence on reconnect?

### `script/main.exs`
```elixir
defmodule CollabRoomWeb.RoomChannelTest do
  use ExUnit.Case, async: false
  import Phoenix.ChannelTest

  @endpoint CollabRoomWeb.Endpoint

  setup do
    {:ok, _, socket} =
      CollabRoomWeb.UserSocket
      |> socket("user_socket:42", %{user_id: 42})
      |> subscribe_and_join(CollabRoomWeb.RoomChannel, "room:lobby", %{"user_id" => 42})

    {:ok, socket: socket}
  end

  describe "join" do
    test "receives presence_state immediately", %{socket: _} do
      assert_push "presence_state", state
      assert Map.has_key?(state, "42")
    end
  end

  describe "typing" do
    test "update triggers a presence_diff with new meta", %{socket: socket} do
      assert_push "presence_state", _
      push(socket, "typing", %{"typing?" => true})
      assert_broadcast "presence_diff", %{joins: joins}
      [%{typing?: true}] = get_in(joins, ["42", :metas])
    end
  end

  describe "message" do
    test "message is broadcast to the topic", %{socket: socket} do
      push(socket, "message", %{"body" => "hello"})
      assert_broadcast "message", %{user_id: 42, body: "hello"}
    end
  end

  describe "disconnect" do
    test "closing the socket emits a leave diff", %{socket: socket} do
      Process.unlink(socket.channel_pid)
      close(socket)
      assert_broadcast "presence_diff", %{leaves: %{"42" => _}}
    end
  end
end

defmodule Main do
  def main do
    IO.puts("✓ Phoenix Channels with Presence for Multi-Node Collaboration")
  - Demonstrating core concepts
    - Implementation patterns and best practices
  end
end

Main.main()
```

---

## Why Phoenix Channels with Presence for Multi-Node Collaboration matters

Mastering **Phoenix Channels with Presence for Multi-Node Collaboration** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `lib/collab_room.ex`

```elixir
defmodule CollabRoom do
  @moduledoc """
  Reference implementation for Phoenix Channels with Presence for Multi-Node Collaboration.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the collab_room module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> CollabRoom.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```

### `test/collab_room_test.exs`

```elixir
defmodule CollabRoomTest do
  use ExUnit.Case, async: true

  doctest CollabRoom

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert CollabRoom.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts

### 1. `Phoenix.Tracker` under the hood

`Phoenix.Presence` wraps `Phoenix.Tracker`. The Tracker uses a heartbeat-based failure detector. If a node does not gossip within `:down_period` (default 15s), its presences are marked `:down` and synced away.

### 2. Events

- `presence_state` — full snapshot; sent once on join and on major re-syncs.
- `presence_diff` — `%{joins: %{...}, leaves: %{...}}`; sent incrementally.

Clients MUST handle both. A naive client that only handles `presence_diff` will start with an empty list.

### 3. `track/3` semantics

`Presence.track(pid, topic, key, meta)` binds the lifetime of the presence to `pid`. When the pid exits, the presence is removed and a `leaves` diff is broadcast. The `key` is usually the user id; multiple `pid`s can share a key (same user on two tabs). `meta` is a map that is merged across tabs.

### 4. `list/1` vs `fetch/2`

`Presence.list(topic)` returns the current snapshot on this node. `Presence.fetch/2` is a callback you override to enrich meta with DB-backed data (e.g., fetch full user profile from Ecto); it is called ONCE per `key`, regardless of how many devices.
