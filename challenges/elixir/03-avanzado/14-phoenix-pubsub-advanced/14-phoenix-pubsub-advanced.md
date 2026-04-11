# Phoenix.PubSub: Cluster-Wide Event Broadcasting in `api_gateway`

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`. The cluster is connected, circuit breakers are distributed,
and route tables are coordinated. One capability is still missing: when something happens
on one node (a circuit breaker trips, a route is updated, a rate limit threshold is
crossed), the other nodes need to know about it immediately.

Three concrete requirements from the product team:

1. **Circuit breaker state sync**: when gateway_a trips the breaker for `payment-service`,
   gateway_b and gateway_c should stop routing to it within milliseconds — not after they
   independently accumulate 5 failures.

2. **Route table invalidation**: when an operator pushes a route update, all nodes must
   clear their local route caches so the next request picks up the new config.

3. **Audit event fan-out**: the audit writer on each node buffers and flushes to storage.
   A separate compliance service subscribes to real-time audit events across the cluster
   for anomaly detection. It needs all events from all nodes.

All three problems are publish-subscribe fan-out. `Phoenix.PubSub` solves them with one
consistent API, without requiring a message broker outside the Erlang cluster.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       ├── circuit_breaker/
│       │   └── worker.ex           # ← extend with PubSub broadcast on state change
│       ├── route_table/
│       │   └── server.ex           # ← extend with cache invalidation via PubSub
│       ├── middleware/
│       │   └── audit_writer.ex     # ← extend with PubSub fan-out
│       └── events/
│           └── subscriber.ex       # ← you implement (cross-cutting event consumer)
├── test/
│   └── api_gateway/
│       └── events/
│           └── pubsub_test.exs     # given tests — must pass
└── mix.exs
```

---

## The business problem

Before this exercise, each `api_gateway` node is event-silent: it changes its own state
but never tells the other nodes. The consequences:

- `payment-service` is tripped on gateway_a. gateway_b routes to it — the very next
  request causes another failure. The user sees errors despite the cluster "protecting" them.
- A route is updated on gateway_a. Requests load-balanced to gateway_b still use the old
  route for up to 60 seconds (TTL of the local cache).
- The compliance service polls each node's API every 30 seconds to collect audit events.
  Events that happened between polls are missed.

The fix is a single, well-typed event bus: processes subscribe to named topics, and
anything that changes state broadcasts an event. The change propagates to all subscribers
on all nodes within milliseconds.

---

## Why `Phoenix.PubSub` and not direct `send` to remote processes

You could implement fan-out manually:

```elixir
Node.list()
|> Enum.each(fn node ->
  send({:route_table_server, node}, {:invalidate, route_id})
end)
```

This works for two nodes. At scale it breaks:

- You must know every subscriber's registered name — coupling publisher to consumer
- Adding a new subscriber (e.g., the compliance service) requires changing the publisher
- There is no backpressure or delivery guarantee API
- Monitoring node reachability and skipping dead nodes is manual boilerplate

`Phoenix.PubSub` decouples publishers from subscribers completely. The publisher calls
`broadcast/3` without knowing how many subscribers exist or on which nodes they run.
New subscribers self-register by calling `subscribe/2`. The publisher code never changes.

---

## How `Phoenix.PubSub` works

### The subscription model

Topics are arbitrary strings. Processes subscribe with `Phoenix.PubSub.subscribe/2`:

```elixir
# In your GenServer's init/1:
Phoenix.PubSub.subscribe(ApiGateway.PubSub, "circuit_breaker:events")

# Messages arrive in handle_info:
def handle_info({:circuit_breaker, :tripped, service, node}, state) do ...
```

Phoenix.PubSub monitors the subscribing process. When it dies (for any reason), the
subscription is removed automatically. No cleanup needed.

### Broadcasting — three variants

```elixir
# Send to ALL subscribers on ALL nodes (the common case)
Phoenix.PubSub.broadcast(
  ApiGateway.PubSub,
  "circuit_breaker:events",
  {:circuit_breaker, :tripped, "payment-service", node()}
)

# Send only to subscribers on THIS node (efficient for local cache invalidation)
Phoenix.PubSub.local_broadcast(
  ApiGateway.PubSub,
  "route_table:invalidate",
  {:invalidate, route_id}
)

# Send only to subscribers on a SPECIFIC node (rare — explicit routing)
Phoenix.PubSub.direct_broadcast(
  :"gateway_b@10.0.1.6",
  ApiGateway.PubSub,
  "audit:events",
  {:audit_batch, events}
)
```

**Which to use**: `broadcast` is correct for the vast majority of cases. Use
`local_broadcast` only when you are certain the effect is local (e.g., invalidating
a per-node in-memory cache). Use `direct_broadcast` when you have explicit routing
requirements — it is rare.

### The `Phoenix.PubSub.PG2` adapter

Under the hood, PubSub topics are `:pg` process groups. When you call `subscribe/2`,
your PID joins the `:pg` group for that topic. When you call `broadcast/3`, PubSub
sends the message to every PID in the group across all nodes.

This means `Phoenix.PubSub` works immediately across any Erlang cluster — no additional
configuration beyond starting the `Phoenix.PubSub` child in `application.ex`.

### `Phoenix.Presence` — distributed subscriber tracking

Presence builds on PubSub to answer "who is subscribed to this topic right now?"
with eventual consistency guarantees. It uses delta CRDTs (same mechanism as Horde)
to reconcile presence state after netsplits.

```elixir
# Track that this process is "present" on a topic
ApiGateway.Presence.track(self(), "audit:subscribers", "compliance-service", %{
  node: node(),
  subscribed_at: DateTime.utc_now()
})

# Query who is currently subscribed — from any node
ApiGateway.Presence.list("audit:subscribers")
#=> %{
#     "compliance-service" => %{metas: [%{node: :"gateway_a@...", subscribed_at: ...}]}
#   }
```

---

## Implementation

### Step 1: Add `Phoenix.PubSub` to `mix.exs` and `application.ex`

```elixir
# mix.exs
defp deps do
  [
    {:phoenix_pubsub, "~> 2.1"},
    # ...existing deps...
  ]
end
```

```elixir
# In lib/api_gateway/application.ex start/2, add before CoreSupervisor:
{Phoenix.PubSub, name: ApiGateway.PubSub}
```

### Step 2: Extend `CircuitBreaker.Worker` to broadcast state changes

```elixir
# In lib/api_gateway/circuit_breaker/worker.ex

# After transitioning from :closed → :open, add:
defp broadcast_state_change(service_name, new_status) do
  Phoenix.PubSub.broadcast(
    ApiGateway.PubSub,
    "circuit_breaker:events",
    {
      :circuit_breaker_state_changed,
      %{service: service_name, status: new_status, node: node(), at: DateTime.utc_now()}
    }
  )
end

# Call from the state transition points in handle_call:
# TODO: add broadcast_state_change(state.service_name, :open) after tripping
# TODO: add broadcast_state_change(state.service_name, :closed) after recovery
```

### Step 3: `lib/api_gateway/events/subscriber.ex`

```elixir
defmodule ApiGateway.Events.Subscriber do
  use GenServer
  require Logger

  @topics [
    "circuit_breaker:events",
    "route_table:events",
    "audit:events",
  ]

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @doc "Returns events received since startup, newest first. Capped at 500 entries."
  def recent_events do
    GenServer.call(__MODULE__, :recent_events)
  end

  # ---------------------------------------------------------------------------
  # GenServer lifecycle
  # ---------------------------------------------------------------------------

  @impl true
  def init(_opts) do
    # TODO: subscribe to all topics in @topics
    # HINT: Enum.each(@topics, &Phoenix.PubSub.subscribe(ApiGateway.PubSub, &1))

    {:ok, %{events: []}}
  end

  # ---------------------------------------------------------------------------
  # Event handlers
  # ---------------------------------------------------------------------------

  @impl true
  def handle_info({:circuit_breaker_state_changed, payload}, state) do
    # TODO:
    # 1. Log at appropriate level (warning for :open, info for :closed/:half_open)
    # 2. Prepend {:circuit_breaker, payload} to state.events
    # 3. Keep only last 500 events
    # 4. {:noreply, new_state}
    {:noreply, state}
  end

  @impl true
  def handle_info({:route_table_updated, payload}, state) do
    # TODO: log and record event
    {:noreply, state}
  end

  @impl true
  def handle_info({:audit_entry, payload}, state) do
    # TODO: log at debug level and record event
    {:noreply, state}
  end

  @impl true
  def handle_call(:recent_events, _from, state) do
    {:reply, state.events, state}
  end
end
```

### Step 4: Route table cache invalidation with `local_broadcast`

```elixir
# In lib/api_gateway/route_table/server.ex

# After a successful route update write, notify other local processes to clear caches:
defp invalidate_local_caches(route_id) do
  # TODO: use local_broadcast — only THIS node's subscribers need to react.
  # Other nodes manage their own route caches and will receive the route update
  # via their own route table sync mechanism.
  #
  # HINT:
  #   Phoenix.PubSub.local_broadcast(
  #     ApiGateway.PubSub,
  #     "route_table:events",
  #     {:route_table_updated, %{route_id: route_id, at: DateTime.utc_now()}}
  #   )
end
```

### Step 5: `Phoenix.Presence` for audit subscriber tracking

```elixir
# lib/api_gateway/presence.ex
defmodule ApiGateway.Presence do
  use Phoenix.Presence,
    otp_app: :api_gateway,
    pubsub_server: ApiGateway.PubSub
end
```

```elixir
# In lib/api_gateway/application.ex, add after Phoenix.PubSub:
ApiGateway.Presence
```

Usage in `Events.Subscriber.init/1`:

```elixir
# After subscribing to topics:
ApiGateway.Presence.track(self(), "audit:subscribers", "gateway-subscriber", %{
  node: node(),
  topics: @topics,
  started_at: DateTime.utc_now()
})
```

### Step 6: Given tests — must pass without modification

```elixir
# test/api_gateway/events/pubsub_test.exs
defmodule ApiGateway.Events.PubSubTest do
  use ExUnit.Case, async: false

  alias ApiGateway.Events.Subscriber

  setup do
    {:ok, _} = start_supervised({Phoenix.PubSub, name: TestPubSub})
    :ok
  end

  describe "subscribe and receive" do
    test "subscriber receives broadcast messages" do
      Phoenix.PubSub.subscribe(TestPubSub, "test:topic")

      Phoenix.PubSub.broadcast(TestPubSub, "test:topic", {:hello, "world"})

      assert_receive {:hello, "world"}, 500
    end

    test "local_broadcast reaches local subscribers only" do
      Phoenix.PubSub.subscribe(TestPubSub, "local:topic")

      Phoenix.PubSub.local_broadcast(TestPubSub, "local:topic", :local_event)

      assert_receive :local_event, 500
    end

    test "unsubscribed process does not receive messages" do
      Phoenix.PubSub.subscribe(TestPubSub, "topic:a")
      Phoenix.PubSub.unsubscribe(TestPubSub, "topic:a")

      Phoenix.PubSub.broadcast(TestPubSub, "topic:a", :should_not_arrive)

      refute_receive :should_not_arrive, 200
    end

    test "dead subscriber is automatically removed" do
      parent = self()

      pid = spawn(fn ->
        Phoenix.PubSub.subscribe(TestPubSub, "transient:topic")
        send(parent, :subscribed)
        receive do: (:stop -> :ok)
      end)

      assert_receive :subscribed, 500

      # Kill the subscriber
      ref = Process.monitor(pid)
      send(pid, :stop)
      assert_receive {:DOWN, ^ref, :process, _, _}, 500

      # Broadcast should not crash even with dead subscriber cleaned up
      assert :ok = Phoenix.PubSub.broadcast(TestPubSub, "transient:topic", :any)
    end
  end

  describe "Events.Subscriber GenServer" do
    setup do
      {:ok, _} = start_supervised({Phoenix.PubSub, name: ApiGateway.PubSub})
      {:ok, _} = start_supervised(Subscriber)
      :ok
    end

    test "starts with empty event log" do
      assert Subscriber.recent_events() == []
    end

    test "records circuit_breaker_state_changed events" do
      Phoenix.PubSub.broadcast(ApiGateway.PubSub, "circuit_breaker:events", {
        :circuit_breaker_state_changed,
        %{service: "payment-svc", status: :open, node: node(), at: DateTime.utc_now()}
      })

      Process.sleep(50)

      events = Subscriber.recent_events()
      assert length(events) == 1
      assert match?({:circuit_breaker, %{service: "payment-svc", status: :open}}, hd(events))
    end

    test "records route_table_updated events" do
      Phoenix.PubSub.broadcast(ApiGateway.PubSub, "route_table:events", {
        :route_table_updated,
        %{route_id: "route-1", at: DateTime.utc_now()}
      })

      Process.sleep(50)

      events = Subscriber.recent_events()
      assert length(events) == 1
    end

    test "event log is capped at 500 entries" do
      for i <- 1..600 do
        Phoenix.PubSub.broadcast(ApiGateway.PubSub, "audit:events", {
          :audit_entry, %{i: i}
        })
      end

      Process.sleep(200)

      assert length(Subscriber.recent_events()) == 500
    end

    test "most recent event is first" do
      Phoenix.PubSub.broadcast(ApiGateway.PubSub, "circuit_breaker:events", {
        :circuit_breaker_state_changed,
        %{service: "svc-a", status: :open, node: node(), at: DateTime.utc_now()}
      })
      Phoenix.PubSub.broadcast(ApiGateway.PubSub, "circuit_breaker:events", {
        :circuit_breaker_state_changed,
        %{service: "svc-b", status: :closed, node: node(), at: DateTime.utc_now()}
      })

      Process.sleep(50)

      [{:circuit_breaker, first} | _] = Subscriber.recent_events()
      assert first.service == "svc-b"
    end
  end
end
```

### Step 7: Run the tests

```bash
mix test test/api_gateway/events/pubsub_test.exs --trace
```

---

## Trade-off analysis

| Design choice | Benefit | Risk |
|---------------|---------|------|
| `Phoenix.PubSub` over manual `Node.list` fan-out | Publishers do not need to know subscribers; adding consumers requires no publisher changes | Messages are fire-and-forget — no delivery guarantee; a subscriber with a full mailbox silently drops messages |
| `local_broadcast` for route cache invalidation | No cross-node overhead for events that only affect local state | If a node misses the invalidation (process restarted after broadcast), its cache stays stale until TTL |
| Capping `Events.Subscriber` history at 500 | Prevents unbounded memory growth under high event rate | Historical events beyond 500 are lost; long debugging sessions may miss early events |
| `Phoenix.Presence` for subscriber tracking | AP semantics — survives netsplits; auto-cleanup on process death | Brief inconsistency window during netsplit means subscriber counts may be wrong by 1–2 |
| Decoupled subscriber (no direct call to CircuitBreaker.Worker) | Zero coupling between event producer and consumer | If the subscriber crashes or is slow, it silently falls behind; no back-pressure to the producer |

Reflection question: `Phoenix.PubSub.broadcast/3` is fire-and-forget — if the receiving
process's mailbox is full, the message is dropped silently. The `Events.Subscriber` receives
audit events at potentially high rates. How would you detect that the subscriber is falling
behind? What architectural change would you make if the audit event volume exceeds what one
GenServer can process?

---

## Common production mistakes

**1. Using `broadcast` when `local_broadcast` is correct**
`Phoenix.PubSub.broadcast/3` sends to all nodes. For events that only matter locally
(clearing a per-node in-memory cache, updating a local ETS table), this generates
unnecessary network traffic. Use `local_broadcast` for events whose effect is local.
The distinction matters most at scale: 10 nodes × N subscribers per node vs 1 node × N
subscribers for the same logical event.

**2. Publishing high-frequency events on a single topic**
If every HTTP request publishes `{:request_received, metadata}` to `"gateway:requests"`,
and 10,000 subscribers exist across the cluster, every request fan-outs to 10,000 message
sends. Topics should be scoped: `"gateway:requests:payment-service"` instead of
`"gateway:requests"`. Subscribers that care about all services can subscribe to a summary
topic that receives aggregated events at a lower frequency.

**3. Confusing `Presence.track` with `PubSub.subscribe`**
`Presence.track/4` registers that a process is "present" in a topic — it does NOT
subscribe that process to messages on the topic. These are two separate operations.
A process can be tracked in Presence without receiving messages, and vice versa.
Both calls are needed if you want both presence tracking and message delivery.

**4. Blocking in `handle_info` for PubSub messages**
If a GenServer subscribes to a high-volume topic and does synchronous work in
`handle_info` (database write, HTTP call), the message queue grows faster than it
drains. The process falls behind and eventually the node runs out of memory.
For I/O-bound event processing, delegate to `Task.Supervisor` from `handle_info` and
return `{:noreply, state}` immediately.

**5. Not starting `Phoenix.Presence` in the supervision tree**
`Phoenix.Presence` is an OTP process. If you call `MyApp.Presence.track/4` before
`MyApp.Presence` has been started, you get `{:noproc, ...}`. In tests, this often
manifests as mysterious crashes unrelated to the test logic.

**6. Publishing `node()` in events but not using it**
Including `node: node()` in event payloads is good practice for observability.
But if the subscriber never logs or stores the node field, debugging "why did this
event come from the wrong node" becomes impossible post-hoc. Log the full payload
at `debug` level on event receipt, not just the fields you think you need.

---

## Resources

- [Phoenix.PubSub documentation](https://hexdocs.pm/phoenix_pubsub/Phoenix.PubSub.html)
- [Phoenix.Presence documentation](https://hexdocs.pm/phoenix/Phoenix.Presence.html)
- [Phoenix PubSub source — github.com/phoenixframework/phoenix_pubsub](https://github.com/phoenixframework/phoenix_pubsub)
- [HexDocs — Erlang :pg (PubSub's underlying primitive)](https://www.erlang.org/doc/man/pg.html)
- [Phoenix Presence — how it uses CRDTs (DockYard blog)](https://dockyard.com/blog/2016/03/25/what-makes-phoenix-presence-special-sneak-peek)
- [Elixir in Action, 3rd ed. — Saša Jurić](https://www.manning.com/books/elixir-in-action-third-edition) — ch. 13, distributed systems
