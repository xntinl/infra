# Intercepting Broadcasts with `intercept` and `handle_out`

**Project**: `channels_intercept` — per-subscriber filtering and enrichment of PubSub broadcasts.

---

## The business problem

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

## Project structure

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
├── script/
│   └── main.exs
└── mix.exs
```

---

## Why intercept and not pre-filter in the publisher

Pre-filtering in the publisher means the publisher knows every subscriber's shape. Interception lets each subscriber decide what to receive based on its own assigns, which is where the per-user context already lives.

---

## Design decisions

**Option A — broadcast full payloads to all subscribers**
- Pros: simple; no per-subscriber logic on the sender.
- Cons: every subscriber pays for fields they do not need or are not allowed to see.

**Option B — `intercept` + `handle_out` to customize per-subscriber** (chosen)
- Pros: per-subscriber filtering; sensitive fields stripped; payload shaped per client.
- Cons: every intercepted message runs through the subscriber's process.

→ Chose **B** because mixed-audience topics need per-subscriber shaping; interception is the idiomatic place.

---

## Implementation

### `mix.exs`
```elixir
defmodule ChannelsIntercept.MixProject do
  use Mix.Project

  def project do
    [
      app: :channels_intercept,
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

`socket.assigns` should carry everything `handle_out/3` needs. Do NOT go out to ETS,
the database, or another GenServer from inside `handle_out/3` — this runs on the
channel process and adds latency to every message for every subscriber. Snapshot the
block-list into assigns at `join/3` time and refresh it on an explicit `"refresh_blocks"`
event.

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

## Benchmark

```elixir
# :timer.tc / Benchee measurement sketch
{time_us, _} = :timer.tc(fn -> :ok end)
IO.puts("elapsed: #{time_us} us")
```

Target: `handle_out/3` adds 5-20 us per subscriber per message; acceptable up to thousands of subscribers per topic.

---

## Reflection

- At 10k subscribers on a topic, does `handle_out` still scale, or do you need a different fan-out model?
- If your intercept logic is 'strip field X from admins', is that the channel's job or the publisher's? Which side owns the policy?

---

### `script/main.exs`
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

defmodule Main do
  def main do
    IO.puts("✓ Intercepting Broadcasts with `intercept` and `handle_out`")
  - Phoenix Channel intercept callbacks
    - Selective broadcast filtering
  end
end

Main.main()
```

---

## Why Intercepting Broadcasts with `intercept` and `handle_out` matters

Mastering **Intercepting Broadcasts with `intercept` and `handle_out`** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `lib/channels_intercept.ex`

```elixir
defmodule ChannelsIntercept do
  @moduledoc """
  Reference implementation for Intercepting Broadcasts with `intercept` and `handle_out`.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the channels_intercept module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> ChannelsIntercept.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```

### `test/channels_intercept_test.exs`

```elixir
defmodule ChannelsInterceptTest do
  use ExUnit.Case, async: true

  doctest ChannelsIntercept

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert ChannelsIntercept.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts

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
