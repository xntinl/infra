# Phoenix Channels — Topics, Authorization, and Backpressure

**Project**: `channels_deep` — a multi-tenant chat backend where native mobile clients connect directly to Phoenix Channels.

**Difficulty**: ★★★★☆
**Estimated time**: 4–6 hours

---

## Project context

The mobile team of a B2B SaaS product is shipping iOS and Android clients that need
real-time messaging between support agents and end customers. They evaluated
Firebase and Pusher, then chose Phoenix Channels for three reasons: a single backend
stack with the rest of the product, no per-message pricing, and fine-grained
authorization tied to the existing tenancy model.

Your task is the channel layer. Requirements agreed with product and security:

1. **Topic scheme**: `"room:<tenant_id>:<room_id>"`. A user is authorized to join
   a topic only if they belong to `tenant_id` AND have access to `room_id`.
2. **Token-based auth** on socket connect — mobile clients pass a short-lived JWT
   issued by the REST API, not cookies.
3. **Per-client rate limiting** — a misbehaving client cannot flood the server
   with `new_message` events. After the limit, the channel replies `:error` and
   keeps the connection.
4. **Structured crashes** — if the channel process crashes, the supervisor restarts
   it, the client reconnects, and we emit a telemetry event so we can track
   crash rates per tenant.

This exercise covers channels only; a sibling exercise (54) adds `Phoenix.Presence`
on top.

Project structure at this point:

```
channels_deep/
├── lib/
│   ├── channels_deep/
│   │   ├── application.ex
│   │   ├── auth.ex                    # JWT verify, Phoenix.Token fallback
│   │   ├── authorization.ex           # tenant/room ACL
│   │   └── rate_limiter.ex            # per-user counter
│   └── channels_deep_web/
│       ├── endpoint.ex
│       ├── channels/
│       │   ├── user_socket.ex
│       │   └── room_channel.ex
│       └── telemetry.ex
└── test/
    └── channels_deep_web/
        └── channels/
            └── room_channel_test.exs
```

---

## Core concepts

### 1. Socket vs. Channel — two layers

```
┌─────────────────────────────────────────────────────┐
│ WebSocket (one per client)                          │
│ ┌────────────────────────────────────────────────┐  │
│ │ UserSocket — authenticated, holds user_id       │ │
│ │                                                 │ │
│ │  ┌──────────┐ ┌──────────┐ ┌──────────┐        │ │
│ │  │ room:1:1 │ │ room:1:2 │ │ room:2:7 │        │ │
│ │  │ channel  │ │ channel  │ │ channel  │        │ │
│ │  │ process  │ │ process  │ │ process  │        │ │
│ │  └──────────┘ └──────────┘ └──────────┘        │ │
│ └────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────┘
```

One WebSocket carries multiple channels. The `UserSocket` authenticates once
at connect; each channel authorizes per join. Each channel is its own BEAM
process (a `Phoenix.Channel` wraps a GenServer). If a channel crashes, the
socket survives and the client can rejoin.

---

### 2. `connect/3` — authenticate or reject

`UserSocket.connect/3` runs when the WebSocket handshake lands. Return
`{:ok, socket}` with the authenticated identity stashed in `socket.assigns`,
or `:error` to refuse the handshake (the client sees a 403 and gives up).

Do NOT do expensive DB lookups here. The connect path is hot under
reconnect storms. Rely on a signed token the client presents — `Phoenix.Token`
for first-party web clients, JWT for mobile where you already have infra.

---

### 3. `join/3` — authorize or reject this topic

Each time the client calls `channel.join()` for a topic, the framework calls
the channel's `join/3`. Return `{:ok, socket}` to accept, `{:ok, reply, socket}`
to accept with an initial payload, or `{:error, reason}` to reject this topic
(other topics on the same socket continue working).

This is the right place for the per-topic ACL check: "is user X allowed in
room Y of tenant Z?"

---

### 4. `handle_in/3` — the inbound event handler

Client sends `channel.push("new_message", %{body: "hi"})`. The server receives
it in `handle_in("new_message", payload, socket)`. Return:

| Return | Meaning |
|--------|---------|
| `{:reply, {:ok, data}, socket}` | ACK with payload |
| `{:reply, {:error, reason}, socket}` | NACK with reason |
| `{:noreply, socket}` | No ACK to client |
| `{:stop, reason, socket}` | Terminate the channel |

Replies are keyed to the client's push ref so multiple in-flight pushes can
interleave. The client gets back exactly the reply for its request.

---

### 5. Backpressure and rate limiting

A WebSocket can be abused by a compromised client. The attack surface:

- Flood `new_message` events to fan-out through PubSub to thousands of
  subscribers.
- Open many channels on one socket to amplify state.
- Send malformed payloads that are expensive to parse.

You defend by:

1. Validating payloads early and rejecting with `{:reply, {:error, :bad_request}, socket}`.
2. Counting events per user and returning `{:reply, {:error, :rate_limited}, socket}` once over budget.
3. Keeping channel state bounded — no growing in-memory history in `socket.assigns`.

We reuse the ETS rate limiter from exercise 71 conceptually; here we keep it
simple with a counter in socket assigns and a periodic reset.

---

## Implementation

### Step 1: Create the project

```bash
mix phx.new channels_deep --no-ecto --no-mailer --no-html
cd channels_deep
```

### Step 2: Authorization — `lib/channels_deep/authorization.ex`

In production this would be a DB-backed check. For the exercise we hardcode
an ACL that the tests will use:

```elixir
defmodule ChannelsDeep.Authorization do
  @moduledoc "Tenant/room access control. In production, back with Ecto."

  @acl %{
    # user_id => %{tenant_id => MapSet of room_ids}
    "user-1" => %{"1" => MapSet.new(["a", "b"])},
    "user-2" => %{"1" => MapSet.new(["a"]), "2" => MapSet.new(["z"])},
    "user-3" => %{}
  }

  @spec allow_room?(String.t(), String.t(), String.t()) :: boolean()
  def allow_room?(user_id, tenant_id, room_id) do
    @acl
    |> Map.get(user_id, %{})
    |> Map.get(tenant_id, MapSet.new())
    |> MapSet.member?(room_id)
  end
end
```

### Step 3: Auth — `lib/channels_deep/auth.ex`

Use `Phoenix.Token` — symmetric, HMAC over the Endpoint secret:

```elixir
defmodule ChannelsDeep.Auth do
  @moduledoc """
  Token verification. `Phoenix.Token` is HMAC-signed and includes an issue
  timestamp — we enforce a 1-hour max age on every verify.
  """

  @salt "user socket"
  @max_age_s 3600

  @spec sign(module(), String.t()) :: String.t()
  def sign(endpoint, user_id), do: Phoenix.Token.sign(endpoint, @salt, user_id)

  @spec verify(module(), String.t()) :: {:ok, String.t()} | {:error, atom()}
  def verify(endpoint, token) do
    Phoenix.Token.verify(endpoint, @salt, token, max_age: @max_age_s)
  end
end
```

### Step 4: UserSocket — `lib/channels_deep_web/channels/user_socket.ex`

```elixir
defmodule ChannelsDeepWeb.UserSocket do
  use Phoenix.Socket

  channel "room:*", ChannelsDeepWeb.RoomChannel

  @impl true
  def connect(%{"token" => token}, socket, _connect_info) do
    case ChannelsDeep.Auth.verify(ChannelsDeepWeb.Endpoint, token) do
      {:ok, user_id} ->
        {:ok, assign(socket, :user_id, user_id)}

      {:error, reason} ->
        :telemetry.execute([:channels, :connect, :denied], %{count: 1}, %{reason: reason})
        :error
    end
  end

  def connect(_params, _socket, _connect_info), do: :error

  @impl true
  def id(socket), do: "user_socket:#{socket.assigns.user_id}"
end
```

`id/1` matters for forced disconnect: `Endpoint.broadcast("user_socket:#{uid}", "disconnect", %{})`
kicks all sockets for that user. Use it for logout, password change, or
revoked tokens.

### Step 5: RoomChannel — `lib/channels_deep_web/channels/room_channel.ex`

```elixir
defmodule ChannelsDeepWeb.RoomChannel do
  use Phoenix.Channel

  alias ChannelsDeep.Authorization

  @max_events_per_window 20
  @window_ms 10_000
  @max_body_bytes 4_000

  @impl true
  def join("room:" <> rest, _payload, socket) do
    case String.split(rest, ":", parts: 2) do
      [tenant_id, room_id] ->
        if Authorization.allow_room?(socket.assigns.user_id, tenant_id, room_id) do
          socket =
            socket
            |> assign(:tenant_id, tenant_id)
            |> assign(:room_id, room_id)
            |> reset_rate()

          Process.send_after(self(), :reset_rate, @window_ms)
          {:ok, %{joined_at: System.os_time(:second)}, socket}
        else
          {:error, %{reason: "unauthorized"}}
        end

      _ ->
        {:error, %{reason: "bad_topic"}}
    end
  end

  @impl true
  def handle_in("new_message", %{"body" => body}, socket) when is_binary(body) do
    cond do
      byte_size(body) > @max_body_bytes ->
        {:reply, {:error, %{reason: "payload_too_large"}}, socket}

      over_budget?(socket) ->
        :telemetry.execute([:channels, :rate_limited], %{count: 1}, %{
          user_id: socket.assigns.user_id
        })

        {:reply, {:error, %{reason: "rate_limited"}}, socket}

      true ->
        broadcast_from!(socket, "message", %{
          user_id: socket.assigns.user_id,
          body: body,
          ts: System.os_time(:millisecond)
        })

        socket = update_in(socket.assigns.event_count, &(&1 + 1))
        {:reply, {:ok, %{accepted: true}}, socket}
    end
  end

  def handle_in("new_message", _malformed, socket) do
    {:reply, {:error, %{reason: "bad_payload"}}, socket}
  end

  @impl true
  def handle_info(:reset_rate, socket) do
    Process.send_after(self(), :reset_rate, @window_ms)
    {:noreply, reset_rate(socket)}
  end

  @impl true
  def terminate(reason, socket) do
    :telemetry.execute([:channels, :terminate], %{count: 1}, %{
      reason: inspect(reason),
      user_id: socket.assigns[:user_id]
    })

    :ok
  end

  defp over_budget?(socket), do: socket.assigns.event_count >= @max_events_per_window
  defp reset_rate(socket), do: assign(socket, :event_count, 0)
end
```

### Step 6: Endpoint wiring

```elixir
# lib/channels_deep_web/endpoint.ex
socket "/socket", ChannelsDeepWeb.UserSocket,
  websocket: [
    connect_info: [:peer_data, :x_headers],
    max_frame_size: 64_000,
    compress: true
  ],
  longpoll: false
```

### Step 7: Tests — `test/channels_deep_web/channels/room_channel_test.exs`

```elixir
defmodule ChannelsDeepWeb.RoomChannelTest do
  use ChannelsDeepWeb.ChannelCase, async: true

  alias ChannelsDeep.Auth
  alias ChannelsDeepWeb.{Endpoint, UserSocket, RoomChannel}

  defp connect_as(user_id) do
    token = Auth.sign(Endpoint, user_id)
    {:ok, socket} = connect(UserSocket, %{"token" => token})
    socket
  end

  describe "connect" do
    test "rejects without token" do
      assert :error = connect(UserSocket, %{})
    end

    test "rejects invalid token" do
      assert :error = connect(UserSocket, %{"token" => "garbage"})
    end

    test "accepts valid token" do
      socket = connect_as("user-1")
      assert socket.assigns.user_id == "user-1"
    end
  end

  describe "join" do
    test "allows authorized topic" do
      socket = connect_as("user-1")
      assert {:ok, %{joined_at: _}, _socket} = subscribe_and_join(socket, RoomChannel, "room:1:a")
    end

    test "rejects unauthorized tenant" do
      socket = connect_as("user-1")
      assert {:error, %{reason: "unauthorized"}} = subscribe_and_join(socket, RoomChannel, "room:99:a")
    end

    test "rejects malformed topic" do
      socket = connect_as("user-1")
      assert {:error, %{reason: "bad_topic"}} = subscribe_and_join(socket, RoomChannel, "room:invalid")
    end
  end

  describe "handle_in new_message" do
    setup do
      socket = connect_as("user-1")
      {:ok, _, channel} = subscribe_and_join(socket, RoomChannel, "room:1:a")
      %{channel: channel}
    end

    test "accepts a well-formed message", %{channel: channel} do
      ref = push(channel, "new_message", %{"body" => "hello"})
      assert_reply ref, :ok, %{accepted: true}
      assert_broadcast "message", %{body: "hello"}
    end

    test "rejects oversized body", %{channel: channel} do
      big = String.duplicate("x", 5_000)
      ref = push(channel, "new_message", %{"body" => big})
      assert_reply ref, :error, %{reason: "payload_too_large"}
    end

    test "rate limits after 20 events", %{channel: channel} do
      for _ <- 1..20 do
        ref = push(channel, "new_message", %{"body" => "x"})
        assert_reply ref, :ok, _
      end

      ref = push(channel, "new_message", %{"body" => "x"})
      assert_reply ref, :error, %{reason: "rate_limited"}
    end
  end
end
```

```bash
mix test
```

---

## Trade-offs and production gotchas

**1. Per-user vs. per-socket rate limits**
Our limiter is per-channel-process. A user who opens 50 channels from a single
socket can send 50 × 20 = 1000 events per window. For a stricter limit, move
the counter into a shared ETS keyed by `user_id` (see exercise 71).

**2. `:reset_rate` timer drift after GC pauses**
If the channel process is suspended 2 seconds, the timer fires late. Over hours,
the window drifts. For precision, compute the current window based on
`System.monotonic_time/1` instead of a scheduled reset — the pattern from the
sliding-window limiter.

**3. `broadcast_from!` vs. `broadcast!`**
`broadcast_from!` sends to every subscriber **except** the current socket. This
is what clients usually want (the sender already has the message optimistically).
Using `broadcast!` echoes the message back and produces double renders on the sender.

**4. `terminate/2` runs best-effort**
The BEAM will skill the process hard on a kill signal. Don't rely on `terminate/2`
to flush critical state — log for observability, nothing else. Persist inline.

**5. Tokens in query strings leak through access logs**
The standard JS client puts the token in the query string (`?token=...`). Proxies
and CDNs log query strings. Prefer `connect_info: [:x_headers]` and custom
headers, or rotate tokens aggressively.

**6. `max_frame_size` matters**
Cowboy defaults to 64KB per WebSocket frame. If a client sends a 1MB message,
Cowboy disconnects them. Set an explicit `max_frame_size` that matches your
product requirements; don't rely on defaults changing.

**7. When NOT to use Channels**
If you don't need bidirectional real-time (only server→client), Server-Sent
Events are simpler, survive intermediaries better, and need no custom client
library. If your "real-time" is request/response under 2 seconds, plain HTTP
with long poll is cheaper to operate.

**8. Clustering and broadcast cost**
`Endpoint.broadcast/3` fans out across the cluster by default when the PubSub
adapter is clustered. For 10 nodes and 100k subscribers, one broadcast is
10 inter-node messages, each fanned out to ~10k subscribers. Measure. If
cross-node traffic becomes the bottleneck, consider partitioning rooms by
node affinity (Horde, Swarm).

---

## Performance notes

Load-test the channel with [websocket-bench](https://github.com/edgurgel/websocket-bench)
or [Tsung](http://tsung.erlang-projects.org/):

```bash
# 5000 concurrent connections, each sending 1 msg/s for 60s
websocket-bench broadcast --concurrent 5000 --rate 1 --sample-size 50 ws://localhost:4000/socket/websocket
```

Expected on a modest dev box: 5k connections with 1 msg/s sustained, p99
broadcast latency < 50ms. The main limits you'll hit before the BEAM caps out
are (1) kernel FD limits (`ulimit -n`) and (2) Cowboy's process-per-connection
overhead at very high connection counts (consider `Bandit` as the HTTP adapter).

---

## Resources

- [`Phoenix.Channel`](https://hexdocs.pm/phoenix/Phoenix.Channel.html) — complete callback reference
- [`Phoenix.Socket`](https://hexdocs.pm/phoenix/Phoenix.Socket.html) — connect lifecycle
- [`Phoenix.Token`](https://hexdocs.pm/phoenix/Phoenix.Token.html) — signing, verification, max age
- [Phoenix — WebSocket client API](https://hexdocs.pm/phoenix/js/)
- [Chris McCord — 2M connections on a single node](https://www.phoenixframework.org/blog/the-road-to-2-million-websocket-connections) — the historical Phoenix benchmark, still useful reading
- [Bandit HTTP adapter](https://github.com/mtrudel/bandit) — modern Cowboy alternative
