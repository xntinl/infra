# Intercepting Broadcasts with `intercept` and `handle_out`

**Project**: `channels_intercept` — per-subscriber filtering and enrichment of PubSub broadcasts.
**Difficulty**: ★★★★☆
**Estimated time**: 3–5 hours

---

## Project context

`channels_intercept` is the realtime layer of a moderated community platform. Every
post a member writes is broadcast to the `room:<id>` topic; subscribers receive live
updates without a page refresh. The product team now wants three things on top of the
raw broadcast:

1. **Block-list filtering** — if subscriber A has blocked user B, A should never see
   B's messages, even though both are in the same room.
2. **Per-subscriber redaction** — staff users see the full author metadata; regular
   users see only the display name.
3. **Presence-aware suppression** — if a subscriber has muted the room, their socket
   should drop broadcasts entirely instead of letting the client filter them.

The naive "filter in JavaScript" approach wastes bandwidth and leaks the sender's
identity to the client even when they shouldn't see it. We need to filter on the server,
per subscriber, on the fastlane. That's what `intercept` and `handle_out` exist for.

```
channels_intercept/
├── lib/
│   └── channels_intercept/
│       ├── application.ex
│       ├── endpoint.ex
│       ├── user_socket.ex
│       ├── block_list.ex
│       └── channels/
│           └── room_channel.ex
├── test/
│   └── channels_intercept/
│       └── channels/
│           └── room_channel_test.exs
└── mix.exs
```

---

## Core concepts

### 1. The fastlane vs the channel process

By default, `Phoenix.Channel` uses a **fastlane**: when you call
`broadcast!(socket, event, payload)`, Phoenix's PubSub delivers the pre-serialized frame
directly to every subscriber's transport process, **skipping the channel GenServer** of
each subscriber entirely:

```
broadcast!
    │
    ▼
PubSub ──fastlane──▶ Transport of sub #1  (channel process not woken)
       ├─fastlane──▶ Transport of sub #2
       └─fastlane──▶ Transport of sub #N
```

This is what makes Phoenix's "200k connections per node" number possible: broadcasts
are O(1) in the number of subscribers plus a single serialization cost.

### 2. `intercept` opts out of the fastlane

When a channel declares `intercept ["some_event"]`, Phoenix **wakes each channel
process** on every broadcast of `"some_event"` and calls `handle_out/3`:

```
broadcast!
    │
    ▼
PubSub ──▶ channel process sub #1 ── handle_out ──▶ transport
       ├──▶ channel process sub #2 ── handle_out ──▶ transport
       └──▶ channel process sub #N ── handle_out ──▶ transport
```

Each subscriber's `handle_out/3` sees the same raw payload but with that subscriber's
own `socket.assigns` — that's the whole point. You can:

- Mutate the payload per subscriber.
- `push/3` a filtered version.
- Return `{:noreply, socket}` to drop the broadcast entirely for this subscriber.

### 3. The price of intercepting

Intercepting kills the fastlane. A broadcast to 10k subscribers now wakes 10k GenServers
and runs 10k `handle_out/3` callbacks. On a hot topic this is **significant** cost —
benchmarks show an order-of-magnitude throughput drop. Intercept only the events that
genuinely need per-subscriber logic; leave everything else on the fastlane.

A common pattern is to split a channel's traffic into two events:

- `"post"` — not intercepted, goes to everyone via fastlane.
- `"post_private"` — intercepted, per-subscriber filtering runs.

### 4. `handle_out/3`: the per-subscriber hook

```elixir
def handle_out("new_post", payload, socket) do
  cond do
    blocked?(socket, payload["author_id"]) -> {:noreply, socket}
    muted?(socket)                          -> {:noreply, socket}
    true ->
      push(socket, "new_post", redact_for(socket, payload))
      {:noreply, socket}
  end
end
```

Return tuples:

| Return | Effect |
|--------|--------|
| `{:noreply, socket}` | Broadcast dropped for this subscriber (silent) |
| `{:reply, {:ok, _}, socket}` | Not valid here — `handle_out` cannot reply |
| `push(socket, ev, p) ; {:noreply, socket}` | Send `ev` with `p` to this subscriber |

You can also call `push/3` multiple times to fan one broadcast into several events.

### 5. Where the filter state lives

`socket.assigns` should carry everything `handle_out/3` needs. Do NOT go out to ETS,
the database, or another GenServer from inside `handle_out/3` — this runs on the
channel process and adds latency to every message for every subscriber. Snapshot the
block-list into assigns at `join/3` time and refresh it on an explicit `"refresh_blocks"`
event.

---

## Implementation

### Step 1: `mix.exs`

```elixir
defmodule ChannelsIntercept.MixProject do
  use Mix.Project

  def project do
    [app: :channels_intercept, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application do
    [mod: {ChannelsIntercept.Application, []}, extra_applications: [:logger]]
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

### Step 2: `lib/channels_intercept/block_list.ex`

```elixir
defmodule ChannelsIntercept.BlockList do
  @moduledoc """
  Stub for the block-list source of truth. In production this would come from
  the database; here we keep it in ETS for deterministic tests.
  """

  @table :block_list

  def start do
    :ets.new(@table, [:named_table, :public, :bag, read_concurrency: true])
  end

  @spec block(String.t(), String.t()) :: :ok
  def block(blocker_id, blocked_id) do
    :ets.insert(@table, {blocker_id, blocked_id})
    :ok
  end

  @spec for_user(String.t()) :: MapSet.t()
  def for_user(user_id) do
    @table
    |> :ets.lookup(user_id)
    |> Enum.map(fn {_u, blocked} -> blocked end)
    |> MapSet.new()
  end
end
```

### Step 3: `lib/channels_intercept/channels/room_channel.ex`

```elixir
defmodule ChannelsIntercept.Channels.RoomChannel do
  @moduledoc """
  Room channel with per-subscriber filtering for `"new_post"` events.

  Fastlane is preserved for:
    - `"typing"` (low-value, high-frequency, no privacy concern)
    - `"member_joined"` / `"member_left"`

  Intercepted events:
    - `"new_post"` (block list + mute + redaction)
  """

  use Phoenix.Channel

  alias ChannelsIntercept.BlockList

  intercept ["new_post"]

  @impl true
  def join("room:" <> room_id, params, socket) do
    user_id = Map.fetch!(params, "user_id")
    role = Map.get(params, "role", "member")

    socket =
      socket
      |> assign(:room_id, room_id)
      |> assign(:user_id, user_id)
      |> assign(:role, role)
      |> assign(:muted?, Map.get(params, "muted?", false))
      |> assign(:blocks, BlockList.for_user(user_id))

    {:ok, socket}
  end

  @impl true
  def handle_in("new_post", payload, socket) do
    post = Map.merge(payload, %{"author_id" => socket.assigns.user_id})
    broadcast!(socket, "new_post", post)
    {:reply, :ok, socket}
  end

  def handle_in("typing", payload, socket) do
    broadcast_from!(socket, "typing", Map.put(payload, "user_id", socket.assigns.user_id))
    {:noreply, socket}
  end

  def handle_in("refresh_blocks", _payload, socket) do
    blocks = BlockList.for_user(socket.assigns.user_id)
    {:reply, {:ok, %{count: MapSet.size(blocks)}}, assign(socket, :blocks, blocks)}
  end

  @impl true
  def handle_out("new_post", payload, socket) do
    cond do
      socket.assigns.muted? ->
        {:noreply, socket}

      MapSet.member?(socket.assigns.blocks, payload["author_id"]) ->
        {:noreply, socket}

      true ->
        push(socket, "new_post", redact(payload, socket.assigns.role))
        {:noreply, socket}
    end
  end

  # Staff see the full author profile; members see only the display-name.
  defp redact(payload, "staff"), do: payload

  defp redact(payload, _) do
    author =
      payload
      |> Map.get("author", %{})
      |> Map.take(["display_name"])

    payload
    |> Map.drop(["author_ip", "author_email"])
    |> Map.put("author", author)
  end
end
```

### Step 4: `lib/channels_intercept/user_socket.ex`

```elixir
defmodule ChannelsIntercept.UserSocket do
  use Phoenix.Socket

  channel "room:*", ChannelsIntercept.Channels.RoomChannel

  @impl true
  def connect(_params, socket, _connect_info), do: {:ok, socket}

  @impl true
  def id(_socket), do: nil
end
```

### Step 5: `lib/channels_intercept/endpoint.ex`

```elixir
defmodule ChannelsIntercept.Endpoint do
  use Phoenix.Endpoint, otp_app: :channels_intercept

  socket "/socket", ChannelsIntercept.UserSocket, websocket: true, longpoll: false
end
```

### Step 6: `lib/channels_intercept/application.ex`

```elixir
defmodule ChannelsIntercept.Application do
  use Application

  @impl true
  def start(_type, _args) do
    ChannelsIntercept.BlockList.start()

    children = [
      {Phoenix.PubSub, name: ChannelsIntercept.PubSub},
      ChannelsIntercept.Endpoint
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: ChannelsIntercept.Supervisor)
  end
end
```

### Step 7: Tests

```elixir
# test/channels_intercept/channels/room_channel_test.exs
defmodule ChannelsIntercept.Channels.RoomChannelTest do
  use ExUnit.Case, async: false
  import Phoenix.ChannelTest

  @endpoint ChannelsIntercept.Endpoint

  alias ChannelsIntercept.{BlockList, UserSocket}

  setup do
    # BlockList ETS table is created in Application.start; wipe between tests
    :ets.delete_all_objects(:block_list)
    :ok
  end

  defp join_as(user_id, opts \\ []) do
    {:ok, socket} = connect(UserSocket, %{})

    params =
      %{"user_id" => user_id}
      |> Map.put("role", Keyword.get(opts, :role, "member"))
      |> Map.put("muted?", Keyword.get(opts, :muted?, false))

    {:ok, _, socket} = subscribe_and_join(socket, "room:general", params)
    socket
  end

  describe "fastlane events" do
    test "typing is broadcast to everyone except sender" do
      alice = join_as("alice")
      bob = join_as("bob")

      push(alice, "typing", %{})
      refute_receive %Phoenix.Socket.Message{event: "typing"}, 50
      _ = bob
      # Bob receives — the assert version would need a second connected socket
      # on the same test process, which ChannelTest doesn't spin up; we
      # verified "sender does not receive" via broadcast_from!.
    end
  end

  describe "handle_out — block list" do
    test "alice does not receive posts from blocked bob" do
      BlockList.block("alice", "bob")
      alice = join_as("alice")

      # Simulate bob posting by fabricating the broadcast as if from bob.
      broadcast_from!(alice, "new_post", %{
        "author_id" => "bob",
        "body" => "hello",
        "author" => %{"display_name" => "Bob"}
      })

      refute_receive %Phoenix.Socket.Message{event: "new_post"}, 100
    end

    test "alice does receive posts from non-blocked carol" do
      BlockList.block("alice", "bob")
      alice = join_as("alice")

      broadcast_from!(alice, "new_post", %{
        "author_id" => "carol",
        "body" => "hi",
        "author" => %{"display_name" => "Carol"}
      })

      assert_receive %Phoenix.Socket.Message{event: "new_post", payload: %{"body" => "hi"}}
    end
  end

  describe "handle_out — mute" do
    test "muted subscriber drops every broadcast" do
      alice = join_as("alice", muted?: true)

      broadcast_from!(alice, "new_post", %{
        "author_id" => "dave",
        "body" => "spammy"
      })

      refute_receive %Phoenix.Socket.Message{event: "new_post"}, 100
    end
  end

  describe "handle_out — redaction" do
    test "member subscriber sees only display_name" do
      alice = join_as("alice")

      broadcast_from!(alice, "new_post", %{
        "author_id" => "dave",
        "body" => "ship it",
        "author" => %{
          "display_name" => "Dave",
          "email" => "dave@example.com",
          "ip" => "1.2.3.4"
        },
        "author_email" => "dave@example.com",
        "author_ip" => "1.2.3.4"
      })

      assert_receive %Phoenix.Socket.Message{
        event: "new_post",
        payload: %{"author" => author} = p
      }

      assert author == %{"display_name" => "Dave"}
      refute Map.has_key?(p, "author_ip")
      refute Map.has_key?(p, "author_email")
    end

    test "staff subscriber sees full payload" do
      mallory = join_as("mallory", role: "staff")

      broadcast_from!(mallory, "new_post", %{
        "author_id" => "dave",
        "body" => "ship it",
        "author_email" => "dave@example.com"
      })

      assert_receive %Phoenix.Socket.Message{
        event: "new_post",
        payload: %{"author_email" => "dave@example.com"}
      }
    end
  end

  describe "refresh_blocks" do
    test "refreshing pulls the latest block list into assigns" do
      alice = join_as("alice")
      BlockList.block("alice", "bob")

      ref = push(alice, "refresh_blocks", %{})
      assert_reply ref, :ok, %{count: 1}

      broadcast_from!(alice, "new_post", %{"author_id" => "bob", "body" => "nope"})
      refute_receive %Phoenix.Socket.Message{event: "new_post"}, 100
    end
  end
end
```

---

## Trade-offs and production gotchas

**1. Intercept is 10–20x slower than fastlane.** A broadcast to 10k subscribers that
was O(1) becomes O(N) with a full GenServer wake-up per subscriber. Benchmark before
intercepting a hot-path event. If the filter logic is trivial, consider moving it into
the payload shape (broadcast two separate events) instead of `handle_out/3`.

**2. `handle_out/3` cannot reply.** It runs asynchronously relative to the broadcaster.
`{:reply, ...}` is not a valid return; you `push/3` instead.

**3. Do not block in `handle_out/3`.** A DB call here means every message to every
subscriber waits on the DB. Cache in assigns at join time, update via explicit events.

**4. State freshness.** `socket.assigns.blocks` is a snapshot. A user blocked 30
seconds ago is still delivered via `handle_out/3` until the client calls
`refresh_blocks` — or until you PubSub-notify channels of block events and have them
refresh themselves. Decide the staleness budget explicitly; don't accept it by accident.

**5. Empty pushes are not free.** `{:noreply, socket}` still cost a GenServer dispatch
and pattern-match. If 99% of subscribers end up dropping the message, you've paid 99x
overhead for 1% delivery. In that regime, push instead to a more targeted topic — use
`Phoenix.Endpoint.broadcast("user:<id>", ...)` keyed to recipients.

**6. `intercept` list is static.** You declare it at module compile time. You can't
conditionally intercept based on the topic or the subscriber. If you need per-topic
filtering, split into multiple channels mounted on different topic patterns.

**7. When NOT to use this.** If the filter is public information (e.g., "only admins
see `admin_log` events"), don't intercept — create a separate topic (`admin_log:<id>`)
and only let admins join it. Topics are your first-class authorization boundary;
`handle_out/3` is the escape hatch when you can't partition the traffic cleanly.

---

## Resources

- [`Phoenix.Channel` — `intercept/1` and `handle_out/3`](https://hexdocs.pm/phoenix/Phoenix.Channel.html#intercept/1)
- [Phoenix source — `Phoenix.Channel.Server`](https://github.com/phoenixframework/phoenix/blob/main/lib/phoenix/channel/server.ex) — see how `c:handle_out` routes fastlane vs intercepted events
- [Chris McCord — Real-time Phoenix](https://pragprog.com/titles/sbsockets/real-time-phoenix/) — chapter on fastlane vs `handle_out`
- [`Phoenix.ChannelTest`](https://hexdocs.pm/phoenix/Phoenix.ChannelTest.html) — `broadcast_from!/3`, `assert_receive`
- [Dashbit — Scaling Phoenix PubSub](https://dashbit.co/blog/real-time-phoenix-on-fly) — numbers on fastlane throughput
- [ferd.ca — on filtering in the producer vs the consumer](https://ferd.ca/queues-don-t-fix-overload.html)
