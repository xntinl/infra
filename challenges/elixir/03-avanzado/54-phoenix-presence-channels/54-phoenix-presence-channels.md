# Phoenix Presence: Connected Client Tracking

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

The `api_gateway` now has a Channel layer (exercise 53). Clients connect via WebSocket and
receive real-time events. The operations team wants to know, at any moment: which clients
are connected, how long they've been connected, and what they're doing. The gateway also
needs to enforce per-room limits on concurrent debug sessions.

Phoenix Presence provides distributed, conflict-free tracking of who is connected where.

Project structure for this exercise:

```
api_gateway_umbrella/apps/gateway_api/
├── lib/gateway_api_web/
│   ├── presence.ex                      # ← you implement this
│   └── channels/
│       ├── debug_session_channel.ex     # ← and this
│       └── client_monitor_channel.ex    # ← and this
└── test/gateway_api_web/channels/
    └── debug_session_channel_test.exs   # given tests
```

---

## Why Presence and not a custom ETS table

When you track connected clients in ETS directly, you get a race condition: between `join/3`
checking the count and `track/3` inserting the new entry, another connection can slip in.
You also get stale entries when a node crashes — ETS is in-memory and lost.

Phoenix Presence is built on `Phoenix.Tracker`, a CRDTs-based distributed tracker. It:
- Automatically removes entries when a socket process dies (via monitoring)
- Synchronizes across nodes using delta CRDTs — no centralized coordinator
- Broadcasts `presence_diff` events to all subscribers when membership changes

The cost: Presence state is eventually consistent across nodes. For connection tracking
where a few milliseconds of lag is acceptable, this is fine. For strict capacity limits,
the exercise shows how to mitigate the inherent race condition.

---

## The `after_join` pattern

Calling `Presence.track/3` directly inside `join/3` creates a race condition: the socket
is not yet fully initialized. The correct pattern:

```elixir
def join("room:x", params, socket) do
  send(self(), :after_join)   # schedule track for AFTER join returns
  {:ok, socket}
end

def handle_info(:after_join, socket) do
  {:ok, _} = Presence.track(socket, ...)
  push(socket, "presence_state", Presence.list(socket))
  {:noreply, socket}
end
```

`send(self(), :after_join)` enqueues the message in the channel process's mailbox. It will
be processed after `join/3` has returned and the socket is fully registered.

---

## Implementation

### Step 1: `lib/gateway_api_web/presence.ex`

```elixir
defmodule GatewayApiWeb.Presence do
  use Phoenix.Presence,
    otp_app: :gateway_api,
    pubsub_server: GatewayApi.PubSub
end
```

Add to `application.ex`:

```elixir
# apps/gateway_api/lib/gateway_api/application.ex
children = [
  GatewayApiWeb.Endpoint,
  GatewayApiWeb.Presence   # must be supervised explicitly
]
```

### Step 2: `lib/gateway_api_web/channels/debug_session_channel.ex`

Debug sessions allow clients to receive detailed trace logs for their requests. The gateway
limits each debug room to 5 concurrent sessions to prevent resource exhaustion.

```elixir
defmodule GatewayApiWeb.DebugSessionChannel do
  use Phoenix.Channel
  alias GatewayApiWeb.Presence

  @max_sessions 5

  @impl true
  def join("debug:" <> room_id, %{"trace_level" => level}, socket) do
    presences = Presence.list("debug:#{room_id}")

    cond do
      Map.has_key?(presences, socket.assigns.client_id) ->
        # Reconnect — this client is already tracked, allow re-join
        socket = socket
        |> assign(:room_id, room_id)
        |> assign(:trace_level, level)
        send(self(), :after_join)
        {:ok, socket}

      map_size(presences) >= @max_sessions ->
        {:error, %{
          reason: "room_full",
          current: map_size(presences),
          max: @max_sessions
        }}

      true ->
        socket = socket
        |> assign(:room_id, room_id)
        |> assign(:trace_level, level)
        send(self(), :after_join)
        {:ok, socket}
    end
  end

  @impl true
  def handle_info(:after_join, socket) do
    # TODO:
    # 1. Presence.track/3 with metadata: client_id, trace_level, joined_at
    # 2. push "presence_state" with current presences to this socket
    # 3. broadcast! "session_joined" to all others in the room
    {:noreply, socket}
  end

  @impl true
  def handle_in("set_trace_level", %{"level" => level}, socket)
      when level in ["debug", "info", "warn", "error"] do
    # Update presence metadata with new trace level without reconnecting
    # TODO: Presence.update/3 to change the :trace_level field
    {:reply, :ok, assign(socket, :trace_level, level)}
  end

  def handle_in("set_trace_level", _, socket) do
    {:reply, {:error, %{reason: "invalid trace level"}}, socket}
  end

  @impl true
  def handle_in("get_sessions", _payload, socket) do
    sessions = Presence.list(socket)
    |> Enum.map(fn {client_id, %{metas: [meta | _]}} ->
      %{client_id: client_id, trace_level: meta.trace_level, joined_at: meta.joined_at}
    end)
    {:reply, {:ok, %{sessions: sessions}}, socket}
  end
end
```

### Step 3: `lib/gateway_api_web/channels/client_monitor_channel.ex`

This channel gives the ops team a live view of all connected clients.

```elixir
defmodule GatewayApiWeb.ClientMonitorChannel do
  use Phoenix.Channel
  alias GatewayApiWeb.Presence

  @topic "ops:clients"

  @impl true
  def join("ops:monitor", _params, socket) do
    # Only ops-role clients can join this channel
    # Assume the socket assigns :role from the token verification in UserSocket
    if socket.assigns[:role] == :ops do
      send(self(), :after_join)
      {:ok, socket}
    else
      {:error, %{reason: "unauthorized"}}
    end
  end

  @impl true
  def handle_info(:after_join, socket) do
    # TODO:
    # 1. Track this ops client in Presence with metadata: role, connected_at
    # 2. Push "presence_state" with ALL connected clients (not just ops ones)
    #    HINT: Presence.list(@topic) where @topic tracks all gateway clients
    {:noreply, socket}
  end

  @impl true
  def handle_in("disconnect_client", %{"client_id" => client_id}, socket) do
    # Force-disconnect a misbehaving client
    # TODO: GatewayApiWeb.Endpoint.broadcast("client_socket:#{client_id}", "disconnect", %{})
    # This triggers the UserSocket's id/1 — matches "client_socket:{id}"
    {:reply, :ok, socket}
  end
end
```

### Step 4: JavaScript client

```javascript
import { Socket, Presence } from "phoenix"

const socket = new Socket("/socket", { params: { token: authToken } })
socket.connect()

// Join a debug session room
const channel = socket.channel("debug:payments-service", {
  trace_level: "debug"
})

const presence = new Presence(channel)

channel.join()
  .receive("ok", () => console.log("Joined debug session"))
  .receive("error", ({ reason, current, max }) => {
    if (reason === "room_full") {
      showError(`Debug room full: ${current}/${max} sessions active`)
    }
  })

// Track who else is in the debug session
presence.onSync(() => {
  const sessions = presence.list((id, { metas: [first] }) => ({
    clientId: id,
    traceLevel: first.trace_level,
    joinedAt: first.joined_at
  }))
  renderSessionList(sessions)
})

presence.onJoin((id, _current, newPresence) => {
  console.log(`${id} joined with trace level: ${newPresence.metas[0].trace_level}`)
})

presence.onLeave((id, current) => {
  if (current.metas.length === 0) {
    console.log(`${id} left the debug session`)
  }
})

// Update trace level without reconnecting
function setTraceLevel(level) {
  channel.push("set_trace_level", { level })
    .receive("ok", () => console.log("Trace level updated"))
    .receive("error", ({ reason }) => console.error("Invalid level:", reason))
}
```

### Step 5: Given tests — must pass without modification

```elixir
# test/gateway_api_web/channels/debug_session_channel_test.exs
defmodule GatewayApiWeb.DebugSessionChannelTest do
  use GatewayApiWeb.ChannelCase

  alias GatewayApiWeb.Presence

  defp make_socket(client_id) do
    GatewayApiWeb.UserSocket
    |> socket(client_id, %{client_id: client_id})
  end

  test "join succeeds and pushes presence_state" do
    {:ok, _, _socket} =
      make_socket("c1")
      |> subscribe_and_join(
        GatewayApiWeb.DebugSessionChannel,
        "debug:test-room",
        %{"trace_level" => "debug"}
      )

    assert_push "presence_state", _
  end

  test "rejects when room is at max capacity" do
    # Fill the room to @max_sessions
    Enum.each(1..5, fn i ->
      {:ok, _, _} =
        make_socket("client-#{i}")
        |> subscribe_and_join(
          GatewayApiWeb.DebugSessionChannel,
          "debug:full-room",
          %{"trace_level" => "info"}
        )
    end)

    assert {:error, %{reason: "room_full", max: 5}} =
      make_socket("client-6")
      |> subscribe_and_join(
        GatewayApiWeb.DebugSessionChannel,
        "debug:full-room",
        %{"trace_level" => "info"}
      )
  end

  test "presence clears when client disconnects" do
    {:ok, _, socket} =
      make_socket("dc-client")
      |> subscribe_and_join(
        GatewayApiWeb.DebugSessionChannel,
        "debug:dc-room",
        %{"trace_level" => "warn"}
      )

    # Give Presence time to register
    Process.sleep(50)
    assert 1 == Presence.list("debug:dc-room") |> map_size()

    # Simulate disconnect
    Process.exit(socket.channel_pid, :normal)
    Process.sleep(50)

    assert 0 == Presence.list("debug:dc-room") |> map_size()
  end

  test "set_trace_level updates presence metadata" do
    {:ok, _, socket} =
      make_socket("level-client")
      |> subscribe_and_join(
        GatewayApiWeb.DebugSessionChannel,
        "debug:level-room",
        %{"trace_level" => "info"}
      )

    ref = push(socket, "set_trace_level", %{"level" => "debug"})
    assert_reply ref, :ok

    Process.sleep(50)
    presences = Presence.list("debug:level-room")
    [meta | _] = presences["level-client"].metas
    assert meta.trace_level == "debug"
  end

  test "rejects invalid trace level" do
    {:ok, _, socket} =
      make_socket("inv-client")
      |> subscribe_and_join(
        GatewayApiWeb.DebugSessionChannel,
        "debug:inv-room",
        %{"trace_level" => "info"}
      )

    ref = push(socket, "set_trace_level", %{"level" => "verbose"})
    assert_reply ref, :error, %{reason: "invalid trace level"}
  end
end
```

### Step 6: Run the tests

```bash
mix test test/gateway_api_web/channels/ --trace
```

---

## Trade-off analysis

| Aspect | Phoenix Presence | Manual ETS tracking | External Redis SETEX |
|--------|-----------------|--------------------|--------------------|
| Auto-cleanup on disconnect | yes (monitors process) | no (manual cleanup) | no (TTL required) |
| Distributed sync | yes (delta CRDTs) | no (node-local) | yes (shared Redis) |
| Consistency | eventual | strong (single node) | strong |
| Race condition on join | mitigated (after_join pattern) | inherent | mitigated (SETNX) |
| Metadata updates | Presence.update/3 | :ets.insert | HSET |
| Overhead | PubSub broadcast per change | ETS write | network round-trip |

Reflection: the capacity check in `join/3` is not atomic — two clients can both read
`map_size(presences) == 4` and both pass the check, resulting in 6 sessions in a room
capped at 5. How would you tighten this without introducing a serializing GenServer?
(Hint: `handle_info(:after_join, ...)` can check again and kick itself out.)

---

## Common production mistakes

**1. Not adding `Presence` to the supervision tree**
`Presence.track/3` calls the Presence GenServer process. If it's not supervised, the call
raises `no process`. Add it explicitly to `application.ex` children.

**2. Calling `Presence.track/3` directly in `join/3`**
The socket is not fully initialized when `join/3` runs. The `send(self(), :after_join)`
pattern defers tracking until after `join/3` has returned and the socket is registered.

**3. Not canceling timers before creating new ones**
If you add timer-based metadata (e.g., "typing" indicator that clears after 3s), calling
`Process.send_after` on every keystroke without canceling the previous timer accumulates
timers. Store the `timer_ref` in socket assigns and call `Process.cancel_timer/1` first.

**4. Using `map_size(Presence.list(...))` for strict capacity enforcement**
`Presence.list` reflects the CRDT state at query time. Under concurrent joins, this is
eventually consistent. For hard capacity limits on critical resources, a GenServer with
serialized join logic is more appropriate than Presence alone.

**5. Sending `presence_state` before `Presence.track/3`**
If you push `presence_state` before tracking yourself, the joining client sees the list
without themselves. Always track first, then push the state.

---

## Resources

- [`Phoenix.Presence`](https://hexdocs.pm/phoenix/Phoenix.Presence.html) — API reference and CRDTs explanation
- [`Phoenix.Tracker`](https://hexdocs.pm/phoenix_pubsub/Phoenix.Tracker.html) — the underlying distributed tracker
- [phoenix.js `Presence` class](https://hexdocs.pm/phoenix/js/) — `onSync`, `onJoin`, `onLeave` callbacks
- [CRDTs explained](https://crdt.tech/) — the theory behind conflict-free replicated data types
