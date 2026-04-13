# Channel Authentication with Signed Tokens and `socket_info`

**Project**: `channels_auth` — multi-tenant collaborative editor authn/z at the socket layer.

---

## Project context

`channels_auth` is the realtime layer for a multi-tenant collaborative editor. Each
customer (a "workspace") has its own namespace. Users inside a workspace may be
`:viewer`, `:editor` or `:owner`. The REST API has already been authenticating requests
with a JWT for months; the new realtime features (cursor presence, live comments) need
to honour the exact same claims without a second round trip to the identity service.

Phoenix Channels sit on top of a long-lived WebSocket. Authentication happens **once**
at `connect/3`; authorization happens **on every `join`**. Getting the split wrong is
the single most common realtime security bug: people authorize the WebSocket handshake
and then trust every subsequent `join` as "already authenticated," forgetting that a
valid-but-under-privileged user can `join` any topic they can name.

```
channels_auth/
├── lib/
│   └── channels_auth/
│       ├── application.ex
│       ├── endpoint.ex
│       ├── token.ex
│       ├── user_socket.ex
│       └── channels/
│           ├── workspace_channel.ex
│           └── document_channel.ex
├── test/
│   └── channels_auth/
│       ├── user_socket_test.exs
│       └── channels/
│           ├── workspace_channel_test.exs
│           └── document_channel_test.exs
└── mix.exs
```

---

## Why per-channel auth and not socket-only

A socket is a connection; a channel is a topic. They are different units of authorization. A user permitted to connect is not automatically permitted to join every topic.

---

## Core concepts

### 1. Connect vs join: two gates, not one

```
┌─────────────────┐   connect (authn)    ┌────────────────┐
│   Client TCP    │─────────────────────▶│   UserSocket   │  identity set once
│   WebSocket     │   token verified     │   .connect/3   │
└─────────────────┘                      └────────────────┘
                                                 │
                                                 ▼
┌─────────────────┐   join "room:42"     ┌────────────────┐
│   Client        │─────────────────────▶│   Channel      │  authz per topic
│                 │   role verified      │   .join/3      │  based on assigns
└─────────────────┘                      └────────────────┘
```

`connect/3` answers *"is this a real user?"* It should reject on invalid/expired tokens
with `:error`. `join/3` answers *"is this user allowed on THIS topic?"* It should reject
under-privileged users with `{:error, %{reason: "unauthorized"}}` even when the token
itself is valid.

### 2. `Phoenix.Token` vs JWT

`Phoenix.Token` wraps `Plug.Crypto.sign/4` + `Plug.Crypto.verify/4`. It signs any Erlang
term (not just JSON), encodes it as URL-safe base64, and appends an HMAC-SHA256
signature. Use it when:

- The producer and consumer are the same Phoenix app (shared `secret_key_base`).
- You don't need to interoperate with non-Elixir systems.
- You want ~2µs sign/verify.

Use JWT (via `Joken` or `JOSE`) when you need cross-language tokens or standard claims
(`exp`, `iss`, `aud`) that third parties also understand.

For this exercise we use `Phoenix.Token` — the identity service already stamps the same
token for REST and realtime.

### 3. `socket_info` and `connect_info`

The WebSocket handshake is an HTTP upgrade. Phoenix exposes parts of the upgrade request
through the `connect_info` option on the `socket/3` macro:

```elixir
socket "/socket", MyApp.UserSocket,
  websocket: [connect_info: [:peer_data, :x_headers, session: @session_options]]
```

This makes `peer_data` (IP, port), selected `x-*` headers, and the Plug session
available inside `connect/3`. The canonical use: read a cookie-signed session, fall
back to a query-string token, and reject when neither is present.

### 4. Topic namespacing enforces multi-tenancy

Prefix every topic with the tenant id you pulled from the token:

```
workspace:acme:lobby
workspace:acme:documents:42
```

If the client joins `workspace:globex:lobby` while `socket.assigns.workspace_id` is
`"acme"`, the channel's `join/3` must return `{:error, ...}`. This is defence in depth:
one accidental `:public` PubSub broadcast across topics doesn't leak across tenants,
because the client could never have joined the foreign topic in the first place.

### 5. Token rotation on long-lived sockets

A WebSocket can stay open for hours. The signed token it was opened with will expire.
Three strategies:

- **Hard cut-off**: reject `join` when the token is older than `max_age`. Client must
  reconnect with a fresh token. Simple, aggressive, inconvenient for users.
- **Soft refresh**: client periodically calls a `:refresh` event and receives a new
  `connect` token to use next time it reconnects. The current connection stays alive.
- **Sliding**: treat every successful `join` as a liveness signal and extend the
  identity's validity in a side channel (ETS). Best UX, most complex.

This exercise implements the hard cut-off with an explicit `max_age` argument to
`Phoenix.Token.verify/4`.

---

## Design decisions

**Option A — authenticate once on socket connect, trust the socket thereafter**
- Pros: simple; minimal per-message overhead.
- Cons: long-lived sockets outlive their tokens; topic-level authorization is left to convention.

**Option B — per-channel `join/3` authentication + token refresh** (chosen)
- Pros: topic-level control; limited blast radius when a token leaks.
- Cons: more code; needs a refresh protocol.

→ Chose **B** because channels often represent different permission domains; per-join auth is the only correct model.

---

## Implementation

### Step 1: `mix.exs`

**Objective**: Pin Phoenix 1.7 with Bandit so sockets terminate on a modern Plug-native server, avoiding Cowboy-specific pitfalls.

```elixir
defmodule ChannelsAuth.MixProject do
  use Mix.Project

  def project do
    [
      app: :channels_auth,
      version: "0.1.0",
      elixir: "~> 1.16",
      deps: deps()
    ]
  end

  def application do
    [mod: {ChannelsAuth.Application, []}, extra_applications: [:logger]]
  end

  defp deps do
    [
      {:phoenix, "~> 1.7"},
      {:phoenix_pubsub, "~> 2.1"},
      {:jason, "~> 1.4"},
      {:bandit, "~> 1.5"}
    ]
  end
end
```

### Dependencies (mix.exs)

```elixir
```elixir
socket "/socket", MyApp.UserSocket,
  websocket: [connect_info: [:peer_data, :x_headers, session: @session_options]]
```

This makes `peer_data` (IP, port), selected `x-*` headers, and the Plug session
available inside `connect/3`. The canonical use: read a cookie-signed session, fall
back to a query-string token, and reject when neither is present.

### 4. Topic namespacing enforces multi-tenancy

Prefix every topic with the tenant id you pulled from the token:

```
workspace:acme:lobby
workspace:acme:documents:42
```

If the client joins `workspace:globex:lobby` while `socket.assigns.workspace_id` is
`"acme"`, the channel's `join/3` must return `{:error, ...}`. This is defence in depth:
one accidental `:public` PubSub broadcast across topics doesn't leak across tenants,
because the client could never have joined the foreign topic in the first place.

### 5. Token rotation on long-lived sockets

A WebSocket can stay open for hours. The signed token it was opened with will expire.
Three strategies:

- **Hard cut-off**: reject `join` when the token is older than `max_age`. Client must
  reconnect with a fresh token. Simple, aggressive, inconvenient for users.
- **Soft refresh**: client periodically calls a `:refresh` event and receives a new
  `connect` token to use next time it reconnects. The current connection stays alive.
- **Sliding**: treat every successful `join` as a liveness signal and extend the
  identity's validity in a side channel (ETS). Best UX, most complex.

This exercise implements the hard cut-off with an explicit `max_age` argument to
`Phoenix.Token.verify/4`.

---

## Design decisions

**Option A — authenticate once on socket connect, trust the socket thereafter**
- Pros: simple; minimal per-message overhead.
- Cons: long-lived sockets outlive their tokens; topic-level authorization is left to convention.

**Option B — per-channel `join/3` authentication + token refresh** (chosen)
- Pros: topic-level control; limited blast radius when a token leaks.
- Cons: more code; needs a refresh protocol.

→ Chose **B** because channels often represent different permission domains; per-join auth is the only correct model.

---

## Implementation

### Step 1: `mix.exs`

**Objective**: Pin Phoenix 1.7 with Bandit so sockets terminate on a modern Plug-native server, avoiding Cowboy-specific pitfalls.

```elixir
defmodule ChannelsAuth.MixProject do
  use Mix.Project

  def project do
    [
      app: :channels_auth,
      version: "0.1.0",
      elixir: "~> 1.16",
      deps: deps()
    ]
  end

  def application do
    [mod: {ChannelsAuth.Application, []}, extra_applications: [:logger]]
  end

  defp deps do
    [
      {:phoenix, "~> 1.7"},
      {:phoenix_pubsub, "~> 2.1"},
      {:jason, "~> 1.4"},
      {:bandit, "~> 1.5"}
    ]
  end
end
```

### Step 2: `lib/channels_auth/token.ex`

**Objective**: Wrap `Phoenix.Token` with a purpose-specific salt and 24h `max_age` so claim tokens are scoped and leaked tokens self-expire.

```elixir
defmodule ChannelsAuth.Token do
  @moduledoc """
  Thin wrapper over `Phoenix.Token` that carries a user-claims map.

  The token payload is a plain map:

      %{user_id: "u_42", workspace_id: "acme", role: :editor}

  Signed with the endpoint's `secret_key_base` plus a per-purpose salt.
  """

  @salt "channels_auth user socket"
  # Tokens are valid for 24h. Fresh enough to cap blast radius of theft, long
  # enough to avoid forcing users to reauth mid-session.
  @max_age_seconds 24 * 60 * 60

  @type claims :: %{user_id: String.t(), workspace_id: String.t(), role: atom()}

  @spec sign(module(), claims()) :: String.t()
  def sign(endpoint, %{user_id: _, workspace_id: _, role: _} = claims) do
    Phoenix.Token.sign(endpoint, @salt, claims)
  end

  @spec verify(module(), String.t()) ::
          {:ok, claims()} | {:error, :invalid | :expired | :missing}
  def verify(_endpoint, nil), do: {:error, :missing}
  def verify(_endpoint, ""), do: {:error, :missing}

  def verify(endpoint, token) when is_binary(token) do
    case Phoenix.Token.verify(endpoint, @salt, token, max_age: @max_age_seconds) do
      {:ok, claims} -> {:ok, claims}
      {:error, :expired} -> {:error, :expired}
      {:error, _other} -> {:error, :invalid}
    end
  end
end
```

### Step 3: `lib/channels_auth/user_socket.ex`

**Objective**: Verify the token once in `connect/3` and expose `id/1` so per-user force-disconnect via `Endpoint.broadcast/3` works across all sockets.

```elixir
defmodule ChannelsAuth.UserSocket do
  @moduledoc """
  Entry point for the WebSocket. Authenticates the bearer token once at
  connect time and stores the resulting claims in socket assigns. Every
  Channel receiving this socket inherits those assigns and uses them for
  topic-level authorization.
  """

  use Phoenix.Socket

  alias ChannelsAuth.Token

  channel "workspace:*", ChannelsAuth.Channels.WorkspaceChannel
  channel "document:*", ChannelsAuth.Channels.DocumentChannel

  @impl true
  def connect(%{"token" => token}, socket, connect_info) do
    with {:ok, claims} <- Token.verify(ChannelsAuth.Endpoint, token) do
      socket =
        socket
        |> assign(:user_id, claims.user_id)
        |> assign(:workspace_id, claims.workspace_id)
        |> assign(:role, claims.role)
        |> assign(:peer_ip, peer_ip(connect_info))

      {:ok, socket}
    else
      {:error, :expired} -> {:error, %{reason: "token_expired"}}
      {:error, _} -> {:error, %{reason: "unauthorized"}}
    end
  end

  def connect(_params, _socket, _connect_info), do: {:error, %{reason: "missing_token"}}

  @impl true
  def id(socket), do: "user_socket:" <> socket.assigns.user_id

  defp peer_ip(%{peer_data: %{address: address}}), do: :inet.ntoa(address) |> to_string()
  defp peer_ip(_), do: "unknown"
end
```

The `id/1` callback returns a per-user identifier that lets the app force-disconnect a
user across every socket they have open:

```elixir
ChannelsAuth.Endpoint.broadcast("user_socket:u_42", "disconnect", %{})
```

Use this when a user signs out, changes password, or has their role revoked.

### Step 4: `lib/channels_auth/channels/workspace_channel.ex`

**Objective**: Compare the topic's `workspace_id` against socket-assigned claims so cross-tenant joins are rejected before any state allocates.

```elixir
defmodule ChannelsAuth.Channels.WorkspaceChannel do
  @moduledoc """
  Lobby-level channel. Any authenticated user of the workspace may join.
  Foreign workspaces are rejected at the topic-matching step.
  """

  use Phoenix.Channel

  @impl true
  def join("workspace:" <> workspace_id, _params, socket) do
    cond do
      socket.assigns.workspace_id != workspace_id ->
        {:error, %{reason: "workspace_mismatch"}}

      true ->
        {:ok, %{role: socket.assigns.role}, socket}
    end
  end

  @impl true
  def handle_in("ping", _payload, socket) do
    {:reply, {:ok, %{pong: System.os_time(:millisecond)}}, socket}
  end
end
```

### Step 5: `lib/channels_auth/channels/document_channel.ex`

**Objective**: Rank roles owner>editor>viewer so `handle_in/3` authorizes per-event, keeping write paths distinct from read-only joins.

```elixir
defmodule ChannelsAuth.Channels.DocumentChannel do
  @moduledoc """
  Document-level channel. Tenant isolation + role enforcement.

  Topic shape: `document:<workspace_id>:<document_id>`.
  """

  use Phoenix.Channel

  @impl true
  def join("document:" <> rest, _params, socket) do
    with [workspace_id, _document_id] <- String.split(rest, ":", parts: 2),
         :ok <- check_workspace(workspace_id, socket),
         :ok <- check_role(socket.assigns.role, :viewer) do
      {:ok, socket}
    else
      {:error, reason} -> {:error, %{reason: reason}}
      _ -> {:error, %{reason: "invalid_topic"}}
    end
  end

  @impl true
  def handle_in("edit", payload, socket) do
    case check_role(socket.assigns.role, :editor) do
      :ok ->
        broadcast_from!(socket, "edit", Map.put(payload, "by", socket.assigns.user_id))
        {:reply, :ok, socket}

      {:error, reason} ->
        {:reply, {:error, %{reason: reason}}, socket}
    end
  end

  def handle_in("delete", _payload, socket) do
    case check_role(socket.assigns.role, :owner) do
      :ok ->
        broadcast_from!(socket, "deleted", %{by: socket.assigns.user_id})
        {:reply, :ok, socket}

      {:error, reason} ->
        {:reply, {:error, %{reason: reason}}, socket}
    end
  end

  # Role precedence: owner > editor > viewer. A user with a higher role
  # always satisfies the requirement for a lower one.
  @role_rank %{viewer: 1, editor: 2, owner: 3}

  defp check_role(actual, required) do
    if Map.fetch!(@role_rank, actual) >= Map.fetch!(@role_rank, required) do
      :ok
    else
      {:error, "role_insufficient"}
    end
  end

  defp check_workspace(workspace_id, socket) do
    if socket.assigns.workspace_id == workspace_id do
      :ok
    else
      {:error, "workspace_mismatch"}
    end
  end
end
```

### Step 6: `lib/channels_auth/endpoint.ex`

**Objective**: Expose `peer_data` through `connect_info` so socket assigns can record the caller's IP for audit and rate-limit keys.

```elixir
defmodule ChannelsAuth.Endpoint do
  use Phoenix.Endpoint, otp_app: :channels_auth

  socket "/socket", ChannelsAuth.UserSocket,
    websocket: [connect_info: [:peer_data, :x_headers]],
    longpoll: false
end
```

### Step 7: `lib/channels_auth/application.ex`

**Objective**: Start `PubSub` before `Endpoint` so channel broadcasts have a registry to dispatch on before the first socket connects.

```elixir
defmodule ChannelsAuth.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {Phoenix.PubSub, name: ChannelsAuth.PubSub},
      ChannelsAuth.Endpoint
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: ChannelsAuth.Supervisor)
  end
end
```

### Step 8: Tests

**Objective**: Drive `connect/2` and `subscribe_and_join/3` with valid, expired, and cross-tenant tokens so every auth branch is covered deterministically.

```elixir
# test/channels_auth/user_socket_test.exs
defmodule ChannelsAuth.UserSocketTest do
  use ExUnit.Case, async: true
  import Phoenix.ChannelTest

  @endpoint ChannelsAuth.Endpoint

  alias ChannelsAuth.{Token, UserSocket}

  defp valid_claims, do: %{user_id: "u_1", workspace_id: "acme", role: :editor}

  test "accepts a valid token" do
    token = Token.sign(@endpoint, valid_claims())

    assert {:ok, socket} = connect(UserSocket, %{"token" => token})
    assert socket.assigns.user_id == "u_1"
    assert socket.assigns.workspace_id == "acme"
    assert socket.assigns.role == :editor
  end

  test "rejects missing token" do
    assert {:error, %{reason: "missing_token"}} = connect(UserSocket, %{})
  end

  test "rejects garbage token" do
    assert {:error, %{reason: "unauthorized"}} = connect(UserSocket, %{"token" => "junk"})
  end

  test "rejects expired token" do
    # Travel back in time by signing with a past signed_at
    signed_at = System.system_time(:second) - 48 * 3600

    token =
      Phoenix.Token.sign(@endpoint, "channels_auth user socket", valid_claims(),
        signed_at: signed_at
      )

    assert {:error, %{reason: "token_expired"}} = connect(UserSocket, %{"token" => token})
  end
end
```

```elixir
# test/channels_auth/channels/document_channel_test.exs
defmodule ChannelsAuth.Channels.DocumentChannelTest do
  use ExUnit.Case, async: true
  import Phoenix.ChannelTest

  @endpoint ChannelsAuth.Endpoint

  alias ChannelsAuth.{Token, UserSocket}

  defp connect_as(role, workspace \\ "acme") do
    token =
      Token.sign(@endpoint, %{user_id: "u_1", workspace_id: workspace, role: role})

    {:ok, socket} = connect(UserSocket, %{"token" => token})
    socket
  end

  describe "join/3" do
    test "viewer can join a document in their workspace" do
      socket = connect_as(:viewer)

      assert {:ok, _reply, _socket} =
               subscribe_and_join(socket, "document:acme:42", %{})
    end

    test "rejects joining a document in another workspace" do
      socket = connect_as(:editor, "acme")

      assert {:error, %{reason: "workspace_mismatch"}} =
               subscribe_and_join(socket, "document:globex:42", %{})
    end
  end

  describe "edit" do
    test "editor can edit" do
      socket = connect_as(:editor)
      {:ok, _, socket} = subscribe_and_join(socket, "document:acme:1", %{})

      ref = push(socket, "edit", %{"op" => "insert", "at" => 5, "text" => "foo"})
      assert_reply ref, :ok
    end

    test "viewer cannot edit" do
      socket = connect_as(:viewer)
      {:ok, _, socket} = subscribe_and_join(socket, "document:acme:1", %{})

      ref = push(socket, "edit", %{"op" => "insert", "at" => 5, "text" => "foo"})
      assert_reply ref, :error, %{reason: "role_insufficient"}
    end
  end

  describe "delete" do
    test "only owner can delete" do
      socket = connect_as(:editor)
      {:ok, _, socket} = subscribe_and_join(socket, "document:acme:1", %{})

      ref = push(socket, "delete", %{})
      assert_reply ref, :error, %{reason: "role_insufficient"}

      owner = connect_as(:owner)
      {:ok, _, owner_socket} = subscribe_and_join(owner, "document:acme:1", %{})

      ref2 = push(owner_socket, "delete", %{})
      assert_reply ref2, :ok
    end
  end
end
```

### Why this works

`UserSocket.connect/3` verifies the initial token and assigns user context. Each `Channel.join/3` independently checks that user's permission for the specific topic. Tokens carry an expiry; long-lived sockets refresh periodically.

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

**1. Don't re-authenticate in `handle_in`.** Once `connect/3` has verified the token,
`socket.assigns` is the source of truth. Re-verifying the token on every message
doubles latency and invites bugs (what if the token rotated mid-session?). Instead,
keep assigns immutable and force-disconnect via `Endpoint.broadcast("user_socket:<id>",
"disconnect", %{})` when the identity changes.

**2. Topic strings are untrusted input.** Clients send whatever topic string they want.
`String.split(rest, ":")` with no `parts:` limit happily accepts `"document:acme:42:evil"`.
Always pin the shape with a pattern match or `parts: N` split.

**3. Role checks on `broadcast` paths.** `broadcast_from!` sends to every subscriber —
including lower-privilege users who legitimately joined. That's correct for "edit"
(viewers see live edits). It is wrong for "owner-only events"; use
`Phoenix.Endpoint.broadcast("user_socket:<owner_id>", ...)` instead.

**4. Session-based fallback.** Browser-based apps often prefer reading the Plug session
(cookie-signed) over a query-string token: URLs leak into logs. Wire this by adding
`session: Endpoint.session_options()` to `connect_info` and reading
`connect_info.session["user_token"]` as the fallback path. Exercise left as extension.

**5. Revocation is not free.** Signed tokens are stateless — you cannot "un-sign" them.
If you need hard revocation you need either a short `max_age` (seconds-to-minutes) plus
silent refresh, or a revocation list in ETS/Redis consulted by `connect/3`. Pick your
poison; don't pretend the problem doesn't exist.

**6. Rate-limit connects, not joins.** A botnet will open 10k sockets and sit idle.
`join` attempts are cheap for the attacker. `connect` is where you enforce per-IP
caps — the `peer_data` is right there. A dead-simple ETS counter keyed on IP keeps
this honest.

**7. `channel "*"` is a footgun.** The docs show `channel "room:*", RoomChannel` — the
asterisk matches everything after `room:`. If you write `channel "*", MyChannel` you've
made every topic joinable by your most permissive module. Never use a bare wildcard.

**8. When NOT to use this.** If you have a single non-tenanted topic (e.g., a public
status page over WebSockets) and no PII flows over it, the full connect-and-authorize
dance is over-engineering. Accept anonymous sockets and publish anything you'd have put
on a public HTTP endpoint.

---

## Benchmark

```elixir
# :timer.tc / Benchee measurement sketch
{time_us, _} = :timer.tc(fn -> :ok end)
IO.puts("elapsed: #{time_us} us")
```

Target: join overhead under 1 ms including token verification; message overhead under 100 us.

---

## Reflection

- A user's permissions change mid-session. How fast does the channel notice, and what is the UX during the gap?
- Your token verifier is slow (50 ms). What do you cache, and what do you refuse to cache?

---

## Resources

- [`Phoenix.Socket` docs](https://hexdocs.pm/phoenix/Phoenix.Socket.html)
- [`Phoenix.Token` docs](https://hexdocs.pm/phoenix/Phoenix.Token.html) — read the `max_age` semantics carefully
- [`Phoenix.ChannelTest` docs](https://hexdocs.pm/phoenix/Phoenix.ChannelTest.html) — `connect/3`, `subscribe_and_join/3`, `push/3`, `assert_reply`
- [Phoenix security guide — Channels section](https://hexdocs.pm/phoenix/channels.html#authentication)
- [Chris McCord — Real-time Phoenix (Pragmatic Bookshelf)](https://pragprog.com/titles/sbsockets/real-time-phoenix/) — Chapter 5 covers authn/z end to end
- [OWASP WebSocket Security cheatsheet](https://cheatsheetseries.owasp.org/cheatsheets/HTML5_Security_Cheat_Sheet.html#websockets)
