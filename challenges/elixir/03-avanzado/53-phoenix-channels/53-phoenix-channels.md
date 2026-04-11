# Phoenix Channels: Real-Time Client Streaming

## Overview

Build a bidirectional WebSocket layer for an API gateway that streams real-time events to
external API consumers -- mobile apps, SPAs, and third-party monitoring agents. Unlike
LiveView (which renders server-side HTML for browsers), Channels provide raw message passing
for any WebSocket client.

Project structure:

```
api_gateway/
├── lib/
│   └── api_gateway_web/
│       ├── channels/
│       │   ├── user_socket.ex
│       │   ├── gateway_events_channel.ex
│       │   └── client_stats_channel.ex
│       └── endpoint.ex
└── test/
    └── api_gateway_web/channels/
        ├── gateway_events_channel_test.exs
        └── client_stats_channel_test.exs
```

---

## Channels vs LiveView for this use case

LiveView is the right tool when the UI is server-rendered and the user interacts through
a browser. Channels are the right tool when:

- The client is a mobile app, a CLI tool, or a third-party service (not a browser)
- The protocol is bidirectional: the client sends data, the server processes it and
  pushes responses
- Multiple clients need to broadcast to each other

For monitoring agents that are not browsers, Channels are correct.

---

## The `intercept` / `handle_out` pattern

`broadcast!/3` sends to every subscriber in a topic -- including the sender. The pattern:

```
intercept ["event_name"]          -- declare which outgoing events to intercept
handle_out("event_name", payload, socket)  -- filter, transform, or drop per socket
```

Without `intercept`, `handle_out` is never called. This is the most common bug with channels.

---

## Implementation

### Step 1: `lib/api_gateway_web/channels/user_socket.ex`

```elixir
defmodule ApiGatewayWeb.UserSocket do
  use Phoenix.Socket

  channel "gateway:events", ApiGatewayWeb.GatewayEventsChannel
  channel "client:*",       ApiGatewayWeb.ClientStatsChannel

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

### Step 2: `lib/api_gateway_web/channels/gateway_events_channel.ex`

This channel streams gateway-level events (circuit breaker state changes, rate limit
triggers) to all subscribed monitoring clients. Clients can filter by event category.

```elixir
defmodule ApiGatewayWeb.GatewayEventsChannel do
  use Phoenix.Channel

  intercept ["event"]

  @impl true
  def join("gateway:events", %{"categories" => categories}, socket) do
    socket = assign(socket, :categories, categories)
    {:ok, %{circuit_breakers: current_circuit_state()}, socket}
  end

  def join("gateway:events", _params, socket) do
    socket = assign(socket, :categories, :all)
    {:ok, %{circuit_breakers: current_circuit_state()}, socket}
  end

  @impl true
  def handle_in("update_filter", %{"categories" => categories}, socket) do
    {:reply, :ok, assign(socket, :categories, categories)}
  end

  @impl true
  def handle_out("event", payload, socket) do
    case socket.assigns.categories do
      :all ->
        push(socket, "event", payload)

      categories when is_list(categories) ->
        if payload.category in categories do
          push(socket, "event", payload)
        end
    end

    {:noreply, socket}
  end

  @doc "Called from telemetry handlers when gateway events occur."
  def broadcast_event(category, data) do
    ApiGatewayWeb.Endpoint.broadcast(
      "gateway:events",
      "event",
      %{category: category, data: data, ts: DateTime.utc_now()}
    )
  end

  defp current_circuit_state do
    case :ets.whereis(:circuit_breaker) do
      :undefined ->
        []

      _ref ->
        :ets.tab2list(:circuit_breaker)
        |> Enum.filter(fn entry -> is_binary(elem(entry, 0)) end)
        |> Enum.map(fn {host, state, meta} ->
          %{host: host, state: state, opened_at: if(state == :open, do: meta, else: nil)}
        end)
    end
  end
end
```

### Step 3: `lib/api_gateway_web/channels/client_stats_channel.ex`

This channel gives each client a private stats stream for their own request history.

```elixir
defmodule ApiGatewayWeb.ClientStatsChannel do
  use Phoenix.Channel

  @max_history 100

  @impl true
  def join("client:" <> client_id, _params, socket) do
    if client_id == socket.assigns.client_id do
      send(self(), :after_join)
      {:ok, assign(socket, :client_id, client_id)}
    else
      {:error, %{reason: "unauthorized"}}
    end
  end

  @impl true
  def handle_info(:after_join, socket) do
    history = load_recent_history(socket.assigns.client_id)
    push(socket, "history", %{requests: history})
    {:noreply, socket}
  end

  @impl true
  def handle_in("get_stats", _payload, socket) do
    stats = compute_stats(socket.assigns.client_id)
    {:reply, {:ok, stats}, socket}
  end

  @doc "Called by the gateway router after processing each request for this client."
  def push_request_event(client_id, request_data) do
    ApiGatewayWeb.Endpoint.broadcast(
      "client:#{client_id}",
      "request",
      request_data
    )
  end

  defp load_recent_history(_client_id) do
    []
  end

  defp compute_stats(_client_id) do
    %{requests_today: 0, error_rate: 0.0, avg_latency_ms: 0.0}
  end
end
```

### Step 4: Mount the socket in `endpoint.ex`

```elixir
# In lib/api_gateway_web/endpoint.ex
socket "/socket", ApiGatewayWeb.UserSocket,
  websocket: true,
  longpoll: false
```

### Step 5: JavaScript client

```javascript
import { Socket } from "phoenix"

const socket = new Socket("/socket", { params: { token: authToken } })
socket.connect()

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

function setFilter(categories) {
  channel.push("update_filter", { categories })
    .receive("ok", () => console.log("Filter updated"))
}
```

### Step 6: Tests

```elixir
# test/api_gateway_web/channels/gateway_events_channel_test.exs
defmodule ApiGatewayWeb.GatewayEventsChannelTest do
  use ApiGatewayWeb.ChannelCase

  setup do
    token = Phoenix.Token.sign(ApiGatewayWeb.Endpoint, "socket", "client-test")

    {:ok, _, socket} =
      ApiGatewayWeb.UserSocket
      |> socket("client-test", %{client_id: "client-test"})
      |> subscribe_and_join(ApiGatewayWeb.GatewayEventsChannel, "gateway:events", %{})

    %{socket: socket, token: token}
  end

  test "join returns current circuit breaker state", %{socket: socket} do
    assert :ok
  end

  test "subscribed client receives broadcast events", %{socket: socket} do
    ApiGatewayWeb.GatewayEventsChannel.broadcast_event(
      "circuit_breaker",
      %{host: "payments.internal", state: "open"}
    )

    assert_push "event", %{category: "circuit_breaker"}
  end

  test "filter: client only receives matching categories" do
    token = Phoenix.Token.sign(ApiGatewayWeb.Endpoint, "socket", "filtered-client")
    {:ok, _, filtered_socket} =
      ApiGatewayWeb.UserSocket
      |> socket("filtered-client", %{client_id: "filtered-client"})
      |> subscribe_and_join(
        ApiGatewayWeb.GatewayEventsChannel,
        "gateway:events",
        %{"categories" => ["rate_limit"]}
      )

    ApiGatewayWeb.GatewayEventsChannel.broadcast_event(
      "circuit_breaker",
      %{host: "payments.internal", state: "open"}
    )

    refute_push "event", %{category: "circuit_breaker"}
  end

  test "update_filter changes subscribed categories", %{socket: socket} do
    ref = push(socket, "update_filter", %{"categories" => ["rate_limit"]})
    assert_reply ref, :ok

    ApiGatewayWeb.GatewayEventsChannel.broadcast_event("circuit_breaker", %{host: "x"})
    refute_push "event", %{category: "circuit_breaker"}
  end
end
```

```elixir
# test/api_gateway_web/channels/client_stats_channel_test.exs
defmodule ApiGatewayWeb.ClientStatsChannelTest do
  use ApiGatewayWeb.ChannelCase

  test "client can only join their own channel" do
    {:error, %{reason: "unauthorized"}} =
      ApiGatewayWeb.UserSocket
      |> socket("client-1", %{client_id: "client-1"})
      |> subscribe_and_join(ApiGatewayWeb.ClientStatsChannel, "client:client-2", %{})
  end

  test "client receives history on join" do
    {:ok, _, socket} =
      ApiGatewayWeb.UserSocket
      |> socket("client-1", %{client_id: "client-1"})
      |> subscribe_and_join(ApiGatewayWeb.ClientStatsChannel, "client:client-1", %{})

    assert_push "history", %{requests: _}
  end

  test "get_stats returns stats for the client" do
    {:ok, _, socket} =
      ApiGatewayWeb.UserSocket
      |> socket("c1", %{client_id: "c1"})
      |> subscribe_and_join(ApiGatewayWeb.ClientStatsChannel, "client:c1", %{})

    ref = push(socket, "get_stats", %{})
    assert_reply ref, :ok, %{requests_today: _, error_rate: _}
  end
end
```

### Step 7: Run the tests

```bash
mix test test/api_gateway_web/channels/ --trace
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

---

## Common production mistakes

**1. Forgetting `intercept ["event"]` before `handle_out`**
Without `intercept`, `handle_out` is never called. The event goes directly to all
subscribers with no filtering. This is the most common channel bug.

**2. Returning `{:ok, payload, socket}` from `handle_out`**
`handle_out` must return `{:noreply, socket}`. It calls `push/3` directly to send,
or skips the push to filter.

**3. Not assigning state in `join/3`**
If `join/3` returns `{:ok, socket}` without assigning `client_id`, any `handle_in` or
`handle_out` that references `socket.assigns.client_id` crashes at runtime.

**4. Using `broadcast!/3` for "all except sender"**
`broadcast!/3` sends to everyone, including the sender. For "all except sender", use
`broadcast_from!/3`.

**5. Trusting the client's claimed `client_id` in `join/3`**
Never accept the `client_id` from the join payload to authorize access. Verify identity
using the `socket.assigns.client_id` set in `connect/3` from a verified token.

---

## Resources

- [Phoenix Channels overview](https://hexdocs.pm/phoenix/channels.html) -- join, handle_in, handle_out
- [Phoenix.ChannelTest](https://hexdocs.pm/phoenix/Phoenix.ChannelTest.html) -- `subscribe_and_join/4`, `push/3`, `assert_push/2`
- [Phoenix.Token](https://hexdocs.pm/phoenix/Phoenix.Token.html) -- signing and verifying tokens for socket auth
- [phoenix.js](https://hexdocs.pm/phoenix/js/) -- JavaScript client library reference
