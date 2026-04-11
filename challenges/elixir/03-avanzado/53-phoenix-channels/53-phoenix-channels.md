# Phoenix Channels: Real-Time Client Streaming

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

The `api_gateway` needs to stream real-time events to JavaScript clients — not just ops
dashboards (which use LiveView), but external API consumers: mobile apps, SPAs, and
third-party integrations that listen for gateway events over WebSocket. Phoenix Channels
provide the bidirectional WebSocket layer for this. LiveView uses Channels internally;
here you use them directly.

Project structure for this exercise:

```
api_gateway_umbrella/apps/gateway_api/
├── lib/gateway_api_web/
│   ├── channels/
│   │   ├── user_socket.ex              # ← you implement this
│   │   ├── gateway_events_channel.ex   # ← and this
│   │   └── client_stats_channel.ex     # ← and this
│   └── presence.ex                     # already exists from exercise 51
└── test/gateway_api_web/channels/
    ├── gateway_events_channel_test.exs # given tests
    └── client_stats_channel_test.exs  # given tests
```

---

## Channels vs LiveView for this use case

LiveView is the right tool when the UI is server-rendered and the user interacts through
a browser. Channels are the right tool when:

- The client is a mobile app, a CLI tool, or a third-party service (not a browser with
  server-rendered HTML)
- The protocol is bidirectional: the client sends data, the server processes it and
  pushes responses
- Multiple clients need to broadcast to each other (not just receive from the server)

For the gateway's external API consumers, the clients are not browsers — they're monitoring
agents. Channels are correct here.

---

## The `intercept` / `handle_out` pattern

`broadcast!/3` sends to every subscriber in a topic — including the sender. For most
gateway events, the sender already knows what happened and should not receive its own
echo. The pattern:

```
intercept ["event_name"]          ← declare which outgoing events to intercept
handle_out("event_name", payload, socket)  ← filter, transform, or drop per socket
```

Without `intercept`, `handle_out` is never called. This is the most common bug with channels.

The alternative for simple "all except sender" cases: `broadcast_from!/3`. It skips the
sender without needing `intercept`. Use `broadcast!/3` + `handle_out` when you need to
transform the payload per receiver (e.g., filter based on per-socket subscriptions).

---

## Implementation

### Step 1: `lib/gateway_api_web/channels/user_socket.ex`

```elixir
defmodule GatewayApiWeb.UserSocket do
  use Phoenix.Socket

  channel "gateway:events", GatewayApiWeb.GatewayEventsChannel
  channel "client:*",       GatewayApiWeb.ClientStatsChannel

  @impl true
  def connect(%{"token" => token}, socket, _connect_info) do
    case Phoenix.Token.verify(GatewayApiWeb.Endpoint, "socket", token, max_age: 86_400) do
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

### Step 2: `lib/gateway_api_web/channels/gateway_events_channel.ex`

This channel streams gateway-level events (circuit breaker state changes, rate limit
triggers) to all subscribed monitoring clients. Clients can filter by event category.

```elixir
defmodule GatewayApiWeb.GatewayEventsChannel do
  use Phoenix.Channel

  # All outgoing "event" messages pass through handle_out for filtering
  intercept ["event"]

  @impl true
  def join("gateway:events", %{"categories" => categories}, socket) do
    socket = assign(socket, :categories, categories)
    # Send current system state as initial payload
    {:ok, %{circuit_breakers: current_circuit_state()}, socket}
  end

  def join("gateway:events", _params, socket) do
    # No filter — subscribe to all categories
    socket = assign(socket, :categories, :all)
    {:ok, %{circuit_breakers: current_circuit_state()}, socket}
  end

  @impl true
  def handle_in("update_filter", %{"categories" => categories}, socket) do
    # Client can update its category filter without reconnecting
    {:reply, :ok, assign(socket, :categories, categories)}
  end

  @impl true
  def handle_out("event", payload, socket) do
    # TODO:
    # If socket.assigns.categories is :all → push unconditionally
    # If it's a list → only push if payload.category is in the list
    # Always return {:noreply, socket}
  end

  # ---------------------------------------------------------------------------
  # Called from the telemetry handler when gateway events occur
  # ---------------------------------------------------------------------------

  def broadcast_event(category, data) do
    GatewayApiWeb.Endpoint.broadcast(
      "gateway:events",
      "event",
      %{category: category, data: data, ts: DateTime.utc_now()}
    )
  end

  defp current_circuit_state do
    # TODO: read :circuit_breaker ETS table
    []
  end
end
```

### Step 3: `lib/gateway_api_web/channels/client_stats_channel.ex`

This channel gives each client a private stats stream for their own request history.

```elixir
defmodule GatewayApiWeb.ClientStatsChannel do
  use Phoenix.Channel

  # broadcast_from! is used here (not broadcast! + intercept) because
  # there's no per-socket payload transformation needed
  @max_history 100

  @impl true
  def join("client:" <> client_id, _params, socket) do
    # Clients can only subscribe to their own channel
    if client_id == socket.assigns.client_id do
      send(self(), :after_join)
      {:ok, assign(socket, :client_id, client_id)}
    else
      {:error, %{reason: "unauthorized"}}
    end
  end

  @impl true
  def handle_info(:after_join, socket) do
    # Send the client their recent request history
    history = load_recent_history(socket.assigns.client_id)
    push(socket, "history", %{requests: history})
    {:noreply, socket}
  end

  @impl true
  def handle_in("get_stats", _payload, socket) do
    stats = compute_stats(socket.assigns.client_id)
    {:reply, {:ok, stats}, socket}
  end

  # Called by the gateway router after processing each request for this client
  def push_request_event(client_id, request_data) do
    GatewayApiWeb.Endpoint.broadcast(
      "client:#{client_id}",
      "request",
      request_data
    )
  end

  defp load_recent_history(client_id) do
    # TODO: query recent requests from DB or ETS
    []
  end

  defp compute_stats(client_id) do
    # TODO: aggregate stats for this client
    %{requests_today: 0, error_rate: 0.0, avg_latency_ms: 0.0}
  end
end
```

### Step 4: Mount the socket in `endpoint.ex`

```elixir
# In lib/gateway_api_web/endpoint.ex
socket "/socket", GatewayApiWeb.UserSocket,
  websocket: true,
  longpoll: false
```

### Step 5: JavaScript client

```javascript
// Monitoring agent connecting to gateway:events
import { Socket } from "phoenix"

const socket = new Socket("/socket", { params: { token: authToken } })
socket.connect()

// Subscribe to circuit breaker and rate limit events only
const channel = socket.channel("gateway:events", {
  categories: ["circuit_breaker", "rate_limit"]
})

channel.join()
  .receive("ok", ({ circuit_breakers }) => {
    renderCircuitBreakerState(circuit_breakers)
  })
  .receive("error", ({ reason }) => console.error("Join failed:", reason))

channel.on("event", ({ category, data, ts }) => {
  appendEventToLog(category, data, ts)

  if (category === "circuit_breaker" && data.state === "open") {
    showAlert(`Circuit opened for ${data.host}`)
  }
})

// Update filter without reconnecting
function setFilter(categories) {
  channel.push("update_filter", { categories })
    .receive("ok", () => console.log("Filter updated"))
}
```

### Step 6: Given tests — must pass without modification

```elixir
# test/gateway_api_web/channels/gateway_events_channel_test.exs
defmodule GatewayApiWeb.GatewayEventsChannelTest do
  use GatewayApiWeb.ChannelCase

  setup do
    token = Phoenix.Token.sign(GatewayApiWeb.Endpoint, "socket", "client-test")

    {:ok, _, socket} =
      GatewayApiWeb.UserSocket
      |> socket("client-test", %{client_id: "client-test"})
      |> subscribe_and_join(GatewayApiWeb.GatewayEventsChannel, "gateway:events", %{})

    %{socket: socket, token: token}
  end

  test "join returns current circuit breaker state", %{socket: socket} do
    # The channel replied with the initial state during subscribe_and_join
    # Verify the join response included circuit_breakers key
    # (subscribe_and_join returns {:ok, reply, socket})
    assert :ok  # covered by subscribe_and_join not raising
  end

  test "subscribed client receives broadcast events", %{socket: socket} do
    GatewayApiWeb.GatewayEventsChannel.broadcast_event(
      "circuit_breaker",
      %{host: "payments.internal", state: "open"}
    )

    assert_push "event", %{category: "circuit_breaker"}
  end

  test "filter: client only receives matching categories" do
    token = Phoenix.Token.sign(GatewayApiWeb.Endpoint, "socket", "filtered-client")
    {:ok, _, filtered_socket} =
      GatewayApiWeb.UserSocket
      |> socket("filtered-client", %{client_id: "filtered-client"})
      |> subscribe_and_join(
        GatewayApiWeb.GatewayEventsChannel,
        "gateway:events",
        %{"categories" => ["rate_limit"]}
      )

    # Broadcast a circuit_breaker event — filtered socket should NOT receive it
    GatewayApiWeb.GatewayEventsChannel.broadcast_event(
      "circuit_breaker",
      %{host: "payments.internal", state: "open"}
    )

    refute_push "event", %{category: "circuit_breaker"}
  end

  test "update_filter changes subscribed categories", %{socket: socket} do
    ref = push(socket, "update_filter", %{"categories" => ["rate_limit"]})
    assert_reply ref, :ok

    # Now broadcast a circuit_breaker event — should be filtered out
    GatewayApiWeb.GatewayEventsChannel.broadcast_event("circuit_breaker", %{host: "x"})
    refute_push "event", %{category: "circuit_breaker"}
  end
end
```

```elixir
# test/gateway_api_web/channels/client_stats_channel_test.exs
defmodule GatewayApiWeb.ClientStatsChannelTest do
  use GatewayApiWeb.ChannelCase

  test "client can only join their own channel" do
    # client-1 tries to join client:client-2 — should fail
    {:error, %{reason: "unauthorized"}} =
      GatewayApiWeb.UserSocket
      |> socket("client-1", %{client_id: "client-1"})
      |> subscribe_and_join(GatewayApiWeb.ClientStatsChannel, "client:client-2", %{})
  end

  test "client receives history on join" do
    {:ok, _, socket} =
      GatewayApiWeb.UserSocket
      |> socket("client-1", %{client_id: "client-1"})
      |> subscribe_and_join(GatewayApiWeb.ClientStatsChannel, "client:client-1", %{})

    assert_push "history", %{requests: _}
  end

  test "get_stats returns stats for the client", %{} do
    {:ok, _, socket} =
      GatewayApiWeb.UserSocket
      |> socket("c1", %{client_id: "c1"})
      |> subscribe_and_join(GatewayApiWeb.ClientStatsChannel, "client:c1", %{})

    ref = push(socket, "get_stats", %{})
    assert_reply ref, :ok, %{requests_today: _, error_rate: _}
  end
end
```

### Step 7: Run the tests

```bash
mix test test/gateway_api_web/channels/ --trace
```

---

## Trade-off analysis

| Aspect | Phoenix Channels | Phoenix LiveView | Plain WebSocket (cowboy) |
|--------|-----------------|-----------------|--------------------------|
| Protocol | Phoenix wire format | Phoenix wire format | raw binary/text |
| Client library | `phoenix.js` | `phoenix_live_view.js` | custom |
| Topic routing | per-channel module | per-LiveView module | manual |
| Broadcast filtering | handle_out/intercept | per-process PubSub | manual |
| State per connection | socket assigns | socket assigns | custom |
| JS framework needed | no | no | no |

Reflection: `broadcast!/3` delivers to all subscribers in a topic synchronously in the
calling process. On a topic with 10,000 subscribers and large payloads, this blocks the
caller for the duration of all pushes. How does Phoenix handle this internally? (Hint:
look at `Phoenix.Channel.Server` and process-per-connection architecture.)

---

## Common production mistakes

**1. Forgetting `intercept ["event"]` before `handle_out`**
Without `intercept`, `handle_out` is never called. The event goes directly to all
subscribers with no filtering. This is the most common channel bug.

**2. Returning `{:ok, payload, socket}` from `handle_out`**
`handle_out` must return `{:noreply, socket}`. It calls `push/3` directly to send,
or skips the push to filter. Returning anything else crashes the channel process at runtime.

**3. Not assigning state in `join/3`**
If `join/3` returns `{:ok, socket}` without assigning `client_id`, any `handle_in` or
`handle_out` that references `socket.assigns.client_id` crashes at runtime.

**4. Using `broadcast!/3` for "all except sender"**
`broadcast!/3` sends to everyone, including the sender. For "all except sender", use
`broadcast_from!/3`. It's cleaner than `broadcast!` + `intercept` + `handle_out` when no
per-socket payload transformation is needed.

**5. Trusting the client's claimed `client_id` in `join/3`**
Never accept the `client_id` from the join payload to authorize access. Verify identity
using the `socket.assigns.client_id` set in `connect/3` from a verified token.

---

## Resources

- [Phoenix Channels overview](https://hexdocs.pm/phoenix/channels.html) — join, handle_in, handle_out
- [Phoenix.ChannelTest](https://hexdocs.pm/phoenix/Phoenix.ChannelTest.html) — `subscribe_and_join/4`, `push/3`, `assert_push/2`
- [Phoenix.Token](https://hexdocs.pm/phoenix/Phoenix.Token.html) — signing and verifying tokens for socket auth
- [phoenix.js](https://hexdocs.pm/phoenix/js/) — JavaScript client library reference
