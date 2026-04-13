# Channel Authentication with Signed Tokens and `socket_info`

**Project**: `channels_auth` — multi-tenant collaborative editor authn/z at the socket layer.

---

## The business problem

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

## Project structure

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
├── script/
│   └── main.exs
└── mix.exs
```

---

## Why per-channel auth and not socket-only

A socket is a connection; a channel is a topic. They are different units of authorization. A user permitted to connect is not automatically permitted to join every topic.

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

### `mix.exs`
```elixir
defmodule ChannelsAuth.MixProject do
  use Mix.Project

  def project do
    [
      app: :channels_auth,
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
socket "/socket", MyApp.UserSocket,
  websocket: [connect_info: [:peer_data, :x_headers, session: @session_options]]
```

This makes `peer_data` (IP, port), selected `x-*` headers, and the Plug session
available inside `connect/3`. The canonical use: read a cookie-signed session, fall
back to a query-string token, and reject when neither is present.

Prefix every topic with the tenant id you pulled from the token:

```
workspace:acme:lobby
workspace:acme:documents:42
```

If the client joins `workspace:globex:lobby` while `socket.assigns.workspace_id` is
`"acme"`, the channel's `join/3` must return `{:error, ...}`. This is defence in depth:
one accidental `:public` PubSub broadcast across topics doesn't leak across tenants,
because the client could never have joined the foreign topic in the first place.

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

### `script/main.exs`
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

defmodule Main do
  def main do
    IO.puts("✓ Channel Authentication with Signed Tokens and `socket_info`")
  - Phoenix Channel authentication
    - Join predicates and access control
  end
end

Main.main()
```

---

## Why Channel Authentication with Signed Tokens and `socket_info` matters

Mastering **Channel Authentication with Signed Tokens and `socket_info`** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `lib/channels_auth.ex`

```elixir
defmodule ChannelsAuth do
  @moduledoc """
  Reference implementation for Channel Authentication with Signed Tokens and `socket_info`.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the channels_auth module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> ChannelsAuth.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```

### `test/channels_auth_test.exs`

```elixir
defmodule ChannelsAuthTest do
  use ExUnit.Case, async: true

  doctest ChannelsAuth

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert ChannelsAuth.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts

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
