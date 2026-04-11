# Phoenix Presence: Connected Client Tracking

## Overview

Implement distributed, conflict-free tracking of connected clients for an API gateway
using Phoenix Presence. Track who is connected, how long they've been connected, and enforce
per-room limits on concurrent debug sessions -- all with automatic cleanup when sockets die.

Project structure:

```
api_gateway/
├── lib/
│   └── api_gateway_web/
│       ├── presence.ex
│       └── channels/
│           ├── user_socket.ex
│           ├── debug_session_channel.ex
│           └── client_monitor_channel.ex
└── test/
    └── api_gateway_web/channels/
        └── debug_session_channel_test.exs
```

---

## Why Presence and not a custom ETS table

When you track connected clients in ETS directly, you get a race condition: between `join/3`
checking the count and `track/3` inserting the new entry, another connection can slip in.
You also get stale entries when a node crashes -- ETS is in-memory and lost.

Phoenix Presence is built on `Phoenix.Tracker`, a CRDTs-based distributed tracker. It:
- Automatically removes entries when a socket process dies (via monitoring)
- Synchronizes across nodes using delta CRDTs -- no centralized coordinator
- Broadcasts `presence_diff` events to all subscribers when membership changes

---

## The `after_join` pattern

Calling `Presence.track/3` directly inside `join/3` creates a race condition: the socket
is not yet fully initialized. The correct pattern:

```elixir
def join("room:x", params, socket) do
  send(self(), :after_join)
  {:ok, socket}
end

def handle_info(:after_join, socket) do
  {:ok, _} = Presence.track(socket, ...)
  push(socket, "presence_state", Presence.list(socket))
  {:noreply, socket}
end
```

---

## Implementation

### Step 1: `lib/api_gateway_web/presence.ex`

```elixir
defmodule ApiGatewayWeb.Presence do
  use Phoenix.Presence,
    otp_app: :api_gateway,
    pubsub_server: ApiGateway.PubSub
end
```

Add to `application.ex`:

```elixir
children = [
  ApiGatewayWeb.Endpoint,
  ApiGatewayWeb.Presence
]
```

### Step 2: `lib/api_gateway_web/channels/user_socket.ex`

```elixir
defmodule ApiGatewayWeb.UserSocket do
  use Phoenix.Socket

  channel "debug:*",    ApiGatewayWeb.DebugSessionChannel
  channel "ops:monitor", ApiGatewayWeb.ClientMonitorChannel

  @impl true
  def connect(%{"token" => token}, socket, _connect_info) do
    case Phoenix.Token.verify(ApiGatewayWeb.Endpoint, "socket", token, max_age: 86_400) do
      {:ok, client_id} ->
        {:ok, assign(socket, :client_id, client_id)}
      {:error, _reason} ->
        :error
    end
  end

  @impl true
  def id(socket), do: "client_socket:#{socket.assigns.client_id}"
end
```

### Step 3: `lib/api_gateway_web/channels/debug_session_channel.ex`

Debug sessions allow clients to receive detailed trace logs for their requests. The gateway
limits each debug room to 5 concurrent sessions to prevent resource exhaustion.

```elixir
defmodule ApiGatewayWeb.DebugSessionChannel do
  use Phoenix.Channel
  alias ApiGatewayWeb.Presence

  @max_sessions 5

  @impl true
  def join("debug:" <> room_id, %{"trace_level" => level}, socket) do
    presences = Presence.list("debug:#{room_id}")

    cond do
      Map.has_key?(presences, socket.assigns.client_id) ->
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
    {:ok, _} = Presence.track(socket, socket.assigns.client_id, %{
      trace_level: socket.assigns.trace_level,
      joined_at: DateTime.utc_now()
    })

    push(socket, "presence_state", Presence.list(socket))

    broadcast_from!(socket, "session_joined", %{
      client_id: socket.assigns.client_id,
      trace_level: socket.assigns.trace_level
    })

    {:noreply, socket}
  end

  @impl true
  def handle_in("set_trace_level", %{"level" => level}, socket)
      when level in ["debug", "info", "warn", "error"] do
    {:ok, _} = Presence.update(socket, socket.assigns.client_id, fn meta ->
      Map.put(meta, :trace_level, level)
    end)

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

### Step 4: `lib/api_gateway_web/channels/client_monitor_channel.ex`

This channel gives the ops team a live view of all connected clients.

```elixir
defmodule ApiGatewayWeb.ClientMonitorChannel do
  use Phoenix.Channel
  alias ApiGatewayWeb.Presence

  @topic "ops:clients"

  @impl true
  def join("ops:monitor", _params, socket) do
    if socket.assigns[:role] == :ops do
      send(self(), :after_join)
      {:ok, socket}
    else
      {:error, %{reason: "unauthorized"}}
    end
  end

  @impl true
  def handle_info(:after_join, socket) do
    {:ok, _} = Presence.track(socket, socket.assigns.client_id, %{
      role: :ops,
      connected_at: DateTime.utc_now()
    })

    push(socket, "presence_state", Presence.list(@topic))

    {:noreply, socket}
  end

  @impl true
  def handle_in("disconnect_client", %{"client_id" => client_id}, socket) do
    ApiGatewayWeb.Endpoint.broadcast("client_socket:#{client_id}", "disconnect", %{})
    {:reply, :ok, socket}
  end
end
```

### Step 5: JavaScript client

```javascript
import { Socket, Presence } from "phoenix"

const socket = new Socket("/socket", { params: { token: authToken } })
socket.connect()

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

function setTraceLevel(level) {
  channel.push("set_trace_level", { level })
    .receive("ok", () => console.log("Trace level updated"))
    .receive("error", ({ reason }) => console.error("Invalid level:", reason))
}
```

### Step 6: Tests

```elixir
# test/api_gateway_web/channels/debug_session_channel_test.exs
defmodule ApiGatewayWeb.DebugSessionChannelTest do
  use ApiGatewayWeb.ChannelCase

  alias ApiGatewayWeb.Presence

  defp make_socket(client_id) do
    ApiGatewayWeb.UserSocket
    |> socket(client_id, %{client_id: client_id})
  end

  test "join succeeds and pushes presence_state" do
    {:ok, _, _socket} =
      make_socket("c1")
      |> subscribe_and_join(
        ApiGatewayWeb.DebugSessionChannel,
        "debug:test-room",
        %{"trace_level" => "debug"}
      )

    assert_push "presence_state", _
  end

  test "rejects when room is at max capacity" do
    Enum.each(1..5, fn i ->
      {:ok, _, _} =
        make_socket("client-#{i}")
        |> subscribe_and_join(
          ApiGatewayWeb.DebugSessionChannel,
          "debug:full-room",
          %{"trace_level" => "info"}
        )
    end)

    assert {:error, %{reason: "room_full", max: 5}} =
      make_socket("client-6")
      |> subscribe_and_join(
        ApiGatewayWeb.DebugSessionChannel,
        "debug:full-room",
        %{"trace_level" => "info"}
      )
  end

  test "presence clears when client disconnects" do
    {:ok, _, socket} =
      make_socket("dc-client")
      |> subscribe_and_join(
        ApiGatewayWeb.DebugSessionChannel,
        "debug:dc-room",
        %{"trace_level" => "warn"}
      )

    Process.sleep(50)
    assert 1 == Presence.list("debug:dc-room") |> map_size()

    Process.exit(socket.channel_pid, :normal)
    Process.sleep(50)

    assert 0 == Presence.list("debug:dc-room") |> map_size()
  end

  test "set_trace_level updates presence metadata" do
    {:ok, _, socket} =
      make_socket("level-client")
      |> subscribe_and_join(
        ApiGatewayWeb.DebugSessionChannel,
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
        ApiGatewayWeb.DebugSessionChannel,
        "debug:inv-room",
        %{"trace_level" => "info"}
      )

    ref = push(socket, "set_trace_level", %{"level" => "verbose"})
    assert_reply ref, :error, %{reason: "invalid trace level"}
  end
end
```

### Step 7: Run the tests

```bash
mix test test/api_gateway_web/channels/ --trace
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

---

## Common production mistakes

**1. Not adding `Presence` to the supervision tree**
`Presence.track/3` calls the Presence GenServer process. If it's not supervised, the call
raises `no process`. Add it explicitly to `application.ex` children.

**2. Calling `Presence.track/3` directly in `join/3`**
The socket is not fully initialized when `join/3` runs. The `send(self(), :after_join)`
pattern defers tracking until after `join/3` has returned.

**3. Not canceling timers before creating new ones**
If you add timer-based metadata (e.g., "typing" indicator), calling `Process.send_after`
on every keystroke without canceling the previous timer accumulates timers. Store the
`timer_ref` in socket assigns and call `Process.cancel_timer/1` first.

**4. Using `map_size(Presence.list(...))` for strict capacity enforcement**
`Presence.list` reflects the CRDT state at query time. Under concurrent joins, this is
eventually consistent. For hard capacity limits, a GenServer with serialized join logic
is more appropriate.

**5. Sending `presence_state` before `Presence.track/3`**
If you push `presence_state` before tracking yourself, the joining client sees the list
without themselves. Always track first, then push the state.

---

## Resources

- [`Phoenix.Presence`](https://hexdocs.pm/phoenix/Phoenix.Presence.html) -- API reference and CRDTs explanation
- [`Phoenix.Tracker`](https://hexdocs.pm/phoenix_pubsub/Phoenix.Tracker.html) -- the underlying distributed tracker
- [phoenix.js `Presence` class](https://hexdocs.pm/phoenix/js/) -- `onSync`, `onJoin`, `onLeave` callbacks
- [CRDTs explained](https://crdt.tech/) -- the theory behind conflict-free replicated data types
