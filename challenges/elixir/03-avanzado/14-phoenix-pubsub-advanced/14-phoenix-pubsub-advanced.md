# Phoenix.PubSub: Cluster-Wide Event Broadcasting

## Goal

Build a cluster-wide event bus using `Phoenix.PubSub` for an API gateway. Circuit breaker state changes are broadcast to all nodes immediately, route table invalidation uses `local_broadcast` for efficiency, and a cross-cutting `Events.Subscriber` GenServer collects all events in a bounded log for monitoring.

---

## Why `Phoenix.PubSub` and not direct `send` to remote processes

Manual fan-out via `Node.list()` couples publishers to subscribers, requires knowing every subscriber's registered name, and breaks when adding new consumers. `Phoenix.PubSub` decouples completely: publishers call `broadcast/3` without knowing subscribers; new subscribers self-register by calling `subscribe/2`. The publisher code never changes.

---

## How `Phoenix.PubSub` works

### Subscription model

Topics are arbitrary strings. Processes subscribe with `Phoenix.PubSub.subscribe/2`. Messages arrive in `handle_info`. PubSub monitors the subscribing process and removes subscriptions automatically on death.

### Broadcasting variants

```elixir
# ALL subscribers on ALL nodes
Phoenix.PubSub.broadcast(ApiGateway.PubSub, "topic", message)

# Only subscribers on THIS node (efficient for local cache invalidation)
Phoenix.PubSub.local_broadcast(ApiGateway.PubSub, "topic", message)

# Only subscribers on a SPECIFIC node
Phoenix.PubSub.direct_broadcast(:"gateway_b@10.0.1.6", ApiGateway.PubSub, "topic", message)
```

Under the hood, PubSub topics are `:pg` process groups. It works across any Erlang cluster with no additional configuration beyond starting the `Phoenix.PubSub` child.

---

## Full implementation

### `mix.exs` dependency

```elixir
defp deps do
  [{:phoenix_pubsub, "~> 2.1"}]
end
```

### `application.ex` setup

```elixir
# In children list:
{Phoenix.PubSub, name: ApiGateway.PubSub}
```

### Broadcasting from circuit breaker worker

```elixir
# Add this function to your circuit breaker worker module:
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

# Call after transitioning to :open:
#   broadcast_state_change(state.service, :open)
# Call after transitioning from :half_open to :closed:
#   broadcast_state_change(state.service, :closed)
```

### Route table cache invalidation with `local_broadcast`

```elixir
# In route table server -- only local node needs to invalidate:
defp invalidate_local_caches(route_id) do
  Phoenix.PubSub.local_broadcast(
    ApiGateway.PubSub,
    "route_table:events",
    {:route_table_updated, %{route_id: route_id, at: DateTime.utc_now()}}
  )
end
```

### `lib/api_gateway/events/subscriber.ex`

Cross-cutting event consumer that subscribes to all event topics and maintains a bounded event log.

```elixir
defmodule ApiGateway.Events.Subscriber do
  use GenServer
  require Logger

  @topics [
    "circuit_breaker:events",
    "route_table:events",
    "audit:events"
  ]

  @max_events 500

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @doc "Returns events received since startup, newest first. Capped at #{@max_events} entries."
  @spec recent_events() :: [tuple()]
  def recent_events do
    GenServer.call(__MODULE__, :recent_events)
  end

  # ---------------------------------------------------------------------------
  # GenServer lifecycle
  # ---------------------------------------------------------------------------

  @impl true
  def init(_opts) do
    Enum.each(@topics, &Phoenix.PubSub.subscribe(ApiGateway.PubSub, &1))
    {:ok, %{events: []}}
  end

  # ---------------------------------------------------------------------------
  # Event handlers
  # ---------------------------------------------------------------------------

  @impl true
  def handle_info({:circuit_breaker_state_changed, payload}, state) do
    level = if payload.status == :open, do: :warning, else: :info
    Logger.log(level, "Circuit breaker #{payload.service} -> #{payload.status} on #{payload.node}")

    new_events = [{:circuit_breaker, payload} | state.events] |> Enum.take(@max_events)
    {:noreply, %{state | events: new_events}}
  end

  @impl true
  def handle_info({:route_table_updated, payload}, state) do
    Logger.info("Route table updated: #{inspect(payload)}")

    new_events = [{:route_table, payload} | state.events] |> Enum.take(@max_events)
    {:noreply, %{state | events: new_events}}
  end

  @impl true
  def handle_info({:audit_entry, payload}, state) do
    Logger.debug("Audit entry received: #{inspect(payload)}")

    new_events = [{:audit, payload} | state.events] |> Enum.take(@max_events)
    {:noreply, %{state | events: new_events}}
  end

  @impl true
  def handle_call(:recent_events, _from, state) do
    {:reply, state.events, state}
  end
end
```

### Tests

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

      ref = Process.monitor(pid)
      send(pid, :stop)
      assert_receive {:DOWN, ^ref, :process, _, _}, 500

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

---

## How it works

1. **Subscribe in `init/1`**: the Subscriber subscribes to all event topics during initialization. PubSub monitors the process and auto-removes subscriptions on death.

2. **Events in `handle_info`**: each event type has its own `handle_info` clause. Events are prepended (newest first) and the list is truncated to `@max_events`.

3. **`broadcast` vs `local_broadcast`**: circuit breaker state changes use `broadcast` (all nodes need to know). Route cache invalidation uses `local_broadcast` (only the local node's caches need clearing).

4. **Fire-and-forget**: `broadcast/3` returns `:ok` immediately. If a subscriber's mailbox is full, the message is dropped silently. There is no delivery guarantee or back-pressure.

---

## Common production mistakes

**1. Using `broadcast` when `local_broadcast` is correct**
For events whose effect is local (clearing a per-node cache), `broadcast` generates unnecessary cross-node traffic.

**2. Publishing high-frequency events on a single topic**
10,000 subscribers on one topic means every event triggers 10,000 message sends. Scope topics: `"gateway:requests:payment-service"` instead of `"gateway:requests"`.

**3. Blocking in `handle_info` for PubSub messages**
Synchronous work (DB writes, HTTP calls) in `handle_info` causes the message queue to grow faster than it drains. Delegate I/O-bound processing to `Task.Supervisor`.

**4. Confusing `Presence.track` with `PubSub.subscribe`**
`Presence.track/4` registers presence -- it does NOT subscribe to messages. These are two separate operations.

---

## Resources

- [Phoenix.PubSub documentation](https://hexdocs.pm/phoenix_pubsub/Phoenix.PubSub.html)
- [Phoenix.Presence documentation](https://hexdocs.pm/phoenix/Phoenix.Presence.html)
- [Erlang :pg (PubSub's underlying primitive)](https://www.erlang.org/doc/man/pg.html)
- [Phoenix PubSub source](https://github.com/phoenixframework/phoenix_pubsub)
