# Exercise 14: Phoenix.PubSub Advanced
# Level: Advanced
# Topic: Distributed pub/sub, Presence tracking, and backpressure
#
# Prerequisites:
#   - Exercises 11-12 completed
#   - Phoenix.PubSub added to mix.exs:
#       {:phoenix_pubsub, "~> 2.1"}
#
# Setup — mix.exs:
#   defp deps do
#     [
#       {:phoenix_pubsub, "~> 2.1"}
#     ]
#   end
#
# mix deps.get && mix compile
#
# ============================================================
# BACKGROUND
# ============================================================
#
# Phoenix.PubSub provides a distributed publish-subscribe system.
# It decouples publishers from subscribers — a publisher broadcasts
# to a topic string; all subscribers on any node receive the message.
#
# Architecture:
#   PubSub adapter (pg2 / Redis)
#     ↓  coordinates across nodes
#   Local dispatch (ETS-backed fast path)
#     ↓  delivers to subscribers
#   Subscriber processes (GenServer, LiveView, Channel, etc.)
#
# Adapters:
#   Phoenix.PubSub.PG2  — default, uses :pg under the hood.
#                          Works for same-cluster (BEAM-to-BEAM) setups.
#                          No external dependencies.
#   phoenix_pubsub_redis — routes through Redis pub/sub.
#                          Needed when nodes are NOT in the same Erlang cluster
#                          (e.g., separate Docker networks, different clouds).
#
# Key API:
#   Phoenix.PubSub.subscribe(pubsub, topic)
#   Phoenix.PubSub.unsubscribe(pubsub, topic)
#   Phoenix.PubSub.broadcast(pubsub, topic, message)
#   Phoenix.PubSub.broadcast_from(pubsub, from_pid, topic, message)
#       — excludes `from_pid` from receiving the message (useful in
#         echo-prevention when a process both sub and pub to the same topic)
#   Phoenix.PubSub.local_broadcast(pubsub, topic, message)
#       — only delivers to subscribers on the current node (no network hop)
#
# Topic naming conventions:
#   Use namespaced, colon-separated strings:
#     "chat:room:42"
#     "user:#{user_id}:events"
#     "market:prices:BTC-USD"
#
# ============================================================
# SETUP
# ============================================================
#
# In your Application supervisor:
#   children = [
#     {Phoenix.PubSub, name: MyApp.PubSub, adapter: Phoenix.PubSub.PG2}
#   ]

# ============================================================
# EXERCISE 1 — Basic PubSub: Multi-Node Subscribe and Broadcast
# ============================================================
#
# Goal: set up PubSub, subscribe from multiple processes (possibly on
# different nodes), broadcast a message, and verify all subscribers
# receive it.

defmodule Exercise14.ChatRoom do
  @moduledoc """
  A simple chat room backed by Phoenix.PubSub.

  Multiple processes (on any node) can join a room and receive messages.
  """

  @pubsub MyApp.PubSub

  @doc """
  Subscribes the calling process to the given room.
  Messages will arrive as {:chat_message, room_id, sender, text}.
  """
  def join(room_id) do
    # TODO: implement using Phoenix.PubSub.subscribe/2
    # Topic convention: "chat:#{room_id}"
    :not_implemented
  end

  @doc """
  Unsubscribes the calling process from the room.
  """
  def leave(room_id) do
    # TODO: implement using Phoenix.PubSub.unsubscribe/2
    :not_implemented
  end

  @doc """
  Broadcasts a message to all subscribers in the room.
  The sender's own process also receives the message.
  """
  def send_message(room_id, sender, text) do
    # TODO: implement using Phoenix.PubSub.broadcast/3
    # Message shape: {:chat_message, room_id, sender, text}
    :not_implemented
  end

  @doc """
  Broadcasts a message to all subscribers EXCEPT the sender.
  Use this to prevent echo when the sender is also subscribed.
  """
  def send_message_no_echo(room_id, sender, text) do
    # TODO: implement using Phoenix.PubSub.broadcast_from/4
    # Hint: broadcast_from excludes the current process (self()) from receiving
    :not_implemented
  end
end

# --------------------------------------------------
# Manual test (single node):
# --------------------------------------------------
#
#   iex -S mix
#   Phoenix.PubSub.start_link(name: MyApp.PubSub)
#   Exercise14.ChatRoom.join("room-1")
#   # In another process:
#   spawn(fn ->
#     Exercise14.ChatRoom.join("room-1")
#     receive do
#       msg -> IO.inspect(msg)
#     end
#   end)
#   Exercise14.ChatRoom.send_message("room-1", "alice", "Hello!")
#   flush()  # see the message in the shell process

# --------------------------------------------------
# Hints for Exercise 1
# --------------------------------------------------
#
# Hint 1: topic/1 helper — define a private function:
#   defp topic(room_id), do: "chat:#{room_id}"
#
# Hint 2: Phoenix.PubSub.subscribe(pubsub, topic) subscribes self().
#   You cannot subscribe a different PID without sending it a message
#   asking it to subscribe itself.
#
# Hint 3: broadcast_from uses self() as the "from" pid. The message is
#   sent to all subscribers EXCEPT self(). This is ideal in GenServers
#   that relay messages — they don't need to filter their own messages.
#
# Hint 4: On a multi-node cluster, broadcast/3 reaches subscribers on
#   ALL nodes via the PG2 adapter automatically. No extra code needed.

# ============================================================
# EXERCISE 2 — Phoenix.Presence: Tracking Online Users
# ============================================================
#
# Goal: use Phoenix.Presence to track which users are connected to
# which room, across the cluster.
#
# Phoenix.Presence:
#   Built on top of PubSub + CRDTs. Each node maintains a local
#   Presence for its processes. Presences are broadcast and merged
#   cluster-wide. When a tracked process dies, its Presence is
#   automatically removed (via process monitoring).
#
# Setup — define a Presence module:
#   defmodule MyApp.Presence do
#     use Phoenix.Presence,
#       otp_app: :my_app,
#       pubsub_server: MyApp.PubSub
#   end
#
# Add to Application supervisor:
#   children = [
#     {Phoenix.PubSub, name: MyApp.PubSub},
#     MyApp.Presence
#   ]
#
# API:
#   MyApp.Presence.track(pid, topic, key, meta)
#   MyApp.Presence.untrack(pid, topic, key)
#   MyApp.Presence.list(topic)     → %{key => %{metas: [%{...}]}}
#   MyApp.Presence.get_by_key(topic, key)

defmodule MyApp.Presence do
  @moduledoc """
  Cluster-wide presence tracking for the app.
  Must be started in the Application supervisor.
  """
  use Phoenix.Presence,
    otp_app: :my_app,
    pubsub_server: MyApp.PubSub
end

defmodule Exercise14.PresenceTracker do
  @moduledoc """
  Tracks user presence in rooms using Phoenix.Presence.

  When a user's connection process dies (crash, disconnect), Presence
  automatically removes their entry — no manual cleanup needed.
  """

  @presence MyApp.Presence

  @doc """
  Marks `user_id` as present in `room_id`.
  Presence is tied to `pid` — when pid dies, the entry is removed.

  `meta` is a map of arbitrary user metadata.
  """
  def track(room_id, user_id, pid, meta \\ %{}) do
    # TODO: implement using MyApp.Presence.track/4
    # topic: "presence:#{room_id}"
    # key: user_id (string)
    :not_implemented
  end

  @doc """
  Returns all users currently present in `room_id`.
  Return shape: [%{user_id: user_id, node: node, meta: meta}]
  """
  def list_users(room_id) do
    # TODO: implement using MyApp.Presence.list/1
    # Presence.list returns %{key => %{metas: [meta_map]}}
    # Flatten it into a list of user maps
    :not_implemented
  end

  @doc """
  Returns the number of unique users in a room (across the cluster).
  """
  def user_count(room_id) do
    # TODO: implement
    :not_implemented
  end

  @doc """
  Returns true if `user_id` is present in `room_id` on any node.
  """
  def online?(room_id, user_id) do
    # TODO: implement using MyApp.Presence.get_by_key/2
    :not_implemented
  end

  @doc """
  Subscribes to presence diff events for `room_id`.
  The calling process will receive:
    {:presence_diff, %{joins: %{}, leaves: %{}}}
  whenever users join or leave the room.
  """
  def subscribe_to_diffs(room_id) do
    # TODO: implement
    # Hint: Presence sends diffs on the same topic used for tracking.
    # Just subscribe to the topic with Phoenix.PubSub.
    :not_implemented
  end
end

# --------------------------------------------------
# Hints for Exercise 2
# --------------------------------------------------
#
# Hint 1: list_users/1 — flatten the Presence map:
#   @presence.list("presence:#{room_id}")
#   |> Enum.flat_map(fn {user_id, %{metas: metas}} ->
#     Enum.map(metas, fn meta -> Map.put(meta, :user_id, user_id) end)
#   end)
#
# Hint 2: Each meta entry automatically includes a :phx_ref field
#   (a unique reference Presence uses internally). You can ignore it
#   or filter it out when displaying to users.
#
# Hint 3: Presence is cluster-wide, but eventually consistent.
#   Right after a node joins, its presences may not yet be visible
#   on other nodes. Allow ~100ms for sync in tests.
#
# Hint 4: subscribe_to_diffs/1 — simply use Phoenix.PubSub.subscribe:
#   Phoenix.PubSub.subscribe(MyApp.PubSub, "presence:#{room_id}")
#   Presence broadcasts diffs on the tracking topic automatically.
#   The diff message shape is: %Phoenix.Presence.Diff{joins: %{}, leaves: %{}}
#   (or the older {:presence_diff, %{joins, leaves}} form — check your version)
#
# Hint 5: Adding node info to meta:
#   Exercise14.PresenceTracker.track("room-1", "alice", self(),
#     %{node: Node.self(), joined_at: DateTime.utc_now()}
#   )

# ============================================================
# EXERCISE 3 — Filtered Subscriptions and Backpressure
# ============================================================
#
# Goal: implement a subscriber that filters messages by content,
# and handle backpressure when the subscriber is slow.

defmodule Exercise14.FilteredSubscriber do
  @moduledoc """
  A GenServer that subscribes to a PubSub topic but only processes
  messages matching a filter function.

  Demonstrates:
  - Topic-level filtering (subscribe to specific sub-topics)
  - Message-level filtering (ignore unwanted messages)
  - Backpressure via bounded mailbox monitoring
  """
  use GenServer
  require Logger

  @pubsub MyApp.PubSub

  defstruct topic: nil,
            filter_fn: nil,
            processed: 0,
            dropped: 0,
            max_queue: 100

  # --- Client API ---

  @doc """
  Starts a subscriber that listens to `topic` and applies `filter_fn`
  to each message. Messages that return false from filter_fn are dropped.
  """
  def start_link(topic, filter_fn, opts \\ []) do
    max_queue = Keyword.get(opts, :max_queue, 100)
    GenServer.start_link(__MODULE__, {topic, filter_fn, max_queue})
  end

  def stats(pid) do
    GenServer.call(pid, :stats)
  end

  # --- Server Callbacks ---

  @impl true
  def init({topic, filter_fn, max_queue}) do
    Phoenix.PubSub.subscribe(@pubsub, topic)

    state = %__MODULE__{
      topic: topic,
      filter_fn: filter_fn,
      max_queue: max_queue
    }

    {:ok, state}
  end

  @impl true
  def handle_info(message, state) do
    # Backpressure check: if our mailbox is too full, drop messages
    {:message_queue_len, queue_len} = Process.info(self(), :message_queue_len)

    cond do
      queue_len > state.max_queue ->
        Logger.warning("Subscriber overloaded (queue=#{queue_len}). Dropping message.")
        {:noreply, %{state | dropped: state.dropped + 1}}

      state.filter_fn.(message) ->
        process_message(message, state)

      true ->
        {:noreply, %{state | dropped: state.dropped + 1}}
    end
  end

  @impl true
  def handle_call(:stats, _from, state) do
    {:reply, %{processed: state.processed, dropped: state.dropped}, state}
  end

  # --- Private ---

  defp process_message(message, state) do
    # TODO: implement message processing
    # In a real system this would call a handler, write to DB, etc.
    # For now, just log and count it.
    Logger.debug("Processing: #{inspect(message)}")
    {:noreply, %{state | processed: state.processed + 1}}
  end
end

defmodule Exercise14.TopicRouter do
  @moduledoc """
  Demonstrates topic naming patterns and sub-topic routing.

  Phoenix.PubSub does NOT support wildcard subscriptions natively —
  you subscribe to exact topic strings. This module shows the two
  approaches for "subscribe to all user events":
    1. Multiple subscriptions (subscribe to each known sub-topic)
    2. Single broad subscription + message-level filtering
  """

  @pubsub MyApp.PubSub

  @doc """
  Approach 1: Subscribe to all event types for a user.
  Pros: precise — no wasted message delivery
  Cons: you must know all topic variants upfront
  """
  def subscribe_all_user_events(user_id) do
    # TODO: subscribe to multiple topics:
    # "user:#{user_id}:login"
    # "user:#{user_id}:logout"
    # "user:#{user_id}:purchase"
    # "user:#{user_id}:error"
    :not_implemented
  end

  @doc """
  Approach 2: Subscribe to a broad topic and filter messages.
  Pros: simple subscription management, easy to add new event types
  Cons: more messages delivered, filter runs on every message
  """
  def subscribe_user_broad(user_id) do
    Phoenix.PubSub.subscribe(@pubsub, "user:#{user_id}")
  end

  @doc """
  Broadcasts an event on the specific sub-topic AND the broad topic.
  This supports both subscription strategies simultaneously.
  """
  def broadcast_user_event(user_id, event_type, payload) do
    message = {event_type, user_id, payload}

    # Broadcast to specific topic (for precise subscribers)
    Phoenix.PubSub.broadcast(@pubsub, "user:#{user_id}:#{event_type}", message)

    # Broadcast to broad topic (for broad subscribers)
    Phoenix.PubSub.broadcast(@pubsub, "user:#{user_id}", message)
  end

  @doc """
  Broadcasts to all users in a list.
  Useful for bulk notifications (e.g., system announcements).
  """
  def broadcast_bulk(user_ids, event_type, payload) do
    # TODO: implement
    # Hint: Enum.each over user_ids and call broadcast_user_event/3
    # Note: this is O(N) broadcasts — for very large N, use a shared
    # topic like "system:announcements" instead.
    :not_implemented
  end
end

# --------------------------------------------------
# Hints for Exercise 3
# --------------------------------------------------
#
# Hint 1: Backpressure alternatives — instead of dropping messages,
#   consider a bounded GenStage or Broadway pipeline:
#   - The producer (PubSub) is replaced by a custom GenStage producer
#   - Consumers pull at their own rate
#   - Back-pressure flows upstream automatically
#   This is overkill for most use cases but necessary when the
#   subscriber is reliably slower than the publisher.
#
# Hint 2: Process.info(self(), :message_queue_len) returns the current
#   mailbox depth. Checking it in handle_info/2 is O(1) but has
#   a race — messages may arrive between the check and the action.
#   It's a "best effort" backpressure, not a guarantee.
#
# Hint 3: For truly bounded pub/sub with acknowledged delivery, use a
#   proper message queue (RabbitMQ, SQS, NATS) rather than PubSub.
#   PubSub is fire-and-forget — no persistence, no ack, no replay.
#
# Hint 4: local_broadcast/3 skips the adapter and delivers only to
#   subscribers on the current node. Use it when you know the
#   subscriber is local (e.g., LiveView processes always run on the
#   same node as the socket).

# ============================================================
# TRADE-OFFS
# ============================================================
#
# Phoenix.PubSub (PG2 adapter) vs Redis adapter:
#
#   PG2 adapter:
#     PRO: No external dependency. Zero latency overhead (in-process).
#          Works automatically across Erlang cluster nodes.
#     CON: Only works within a single Erlang cluster (same cookie).
#          No persistence — messages lost if subscriber is down.
#          No replay — can't ask "what did I miss while offline?"
#
#   Redis adapter:
#     PRO: Works across separate Erlang clusters (different datacenters,
#          separate k8s namespaces).
#          Redis fan-out can serve thousands of connections.
#     CON: Redis is a single point of failure (mitigate with Redis Sentinel).
#          Higher latency (~1ms+). Serialization cost.
#
# Phoenix.Presence vs Custom presence tracking:
#   Presence PRO: automatic cleanup when process dies, CRDT-based cluster sync,
#                 diff notifications built-in, battle-tested in Phoenix.
#   Presence CON: eventually consistent (brief staleness after node join/leave),
#                 meta must be serializable (no PIDs in meta).
#
# Backpressure in PubSub:
#   PubSub has no built-in backpressure — it's fire-and-forget.
#   Options:
#     1. Drop messages (simplest — accept data loss)
#     2. Hibernate the subscriber (buy time, not a real solution)
#     3. Use GenStage/Broadway for pull-based flow control
#     4. Offload work to a bounded queue (ETS, Redis, NATS)
#
# PRODUCTION TIP:
#   Monitor mailbox lengths in production with Telemetry:
#     :telemetry.attach("mailbox-monitor", [:vm, :proc, :mailbox], handler, nil)
#   Alert when any process exceeds 1000 messages — that's a sign of
#   a slow consumer that will eventually cause memory exhaustion.

# ============================================================
# ONE POSSIBLE SOLUTION (sparse)
# ============================================================

defmodule Exercise14.Solution do
  @pubsub MyApp.PubSub
  @presence MyApp.Presence

  # Exercise 1
  defp topic(room_id), do: "chat:#{room_id}"

  def join(room_id), do: Phoenix.PubSub.subscribe(@pubsub, topic(room_id))
  def leave(room_id), do: Phoenix.PubSub.unsubscribe(@pubsub, topic(room_id))

  def send_message(room_id, sender, text) do
    Phoenix.PubSub.broadcast(@pubsub, topic(room_id), {:chat_message, room_id, sender, text})
  end

  def send_message_no_echo(room_id, sender, text) do
    Phoenix.PubSub.broadcast_from(@pubsub, self(), topic(room_id),
      {:chat_message, room_id, sender, text})
  end

  # Exercise 2
  def track(room_id, user_id, pid, meta \\ %{}) do
    @presence.track(pid, "presence:#{room_id}", user_id, meta)
  end

  def list_users(room_id) do
    @presence.list("presence:#{room_id}")
    |> Enum.flat_map(fn {user_id, %{metas: metas}} ->
      Enum.map(metas, &Map.put(&1, :user_id, user_id))
    end)
  end

  def user_count(room_id) do
    @presence.list("presence:#{room_id}") |> map_size()
  end

  def online?(room_id, user_id) do
    @presence.get_by_key("presence:#{room_id}", user_id) != []
  end

  # Exercise 3
  def subscribe_all_user_events(user_id) do
    Enum.each(["login", "logout", "purchase", "error"], fn type ->
      Phoenix.PubSub.subscribe(@pubsub, "user:#{user_id}:#{type}")
    end)
  end

  def broadcast_bulk(user_ids, event_type, payload) do
    Enum.each(user_ids, fn uid ->
      Exercise14.TopicRouter.broadcast_user_event(uid, event_type, payload)
    end)
  end
end
