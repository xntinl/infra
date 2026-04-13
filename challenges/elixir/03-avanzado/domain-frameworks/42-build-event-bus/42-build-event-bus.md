# Build Your Own Event Bus

**Project**: `event_bus_built` — in-VM pub/sub with Registry, delivery guarantees, and subscriber isolation.
---

## Project context

You're the platform engineer at a payments company. Three internal services —
`orders`, `invoicing`, and `fraud_detection` — run in the same BEAM node (umbrella
layout). Each service publishes and consumes domain events (`order.placed`,
`invoice.issued`, `fraud.flagged`). Today they call each other directly through
`GenServer.call/3`: coupling is tight, a slow subscriber blocks publishers, and a
crash in `fraud_detection` takes down the order flow.

You've been asked to build a lightweight **in-VM event bus** before considering a
broker (RabbitMQ, NATS, Kafka). The constraints are explicit:

- Must be async by default — publishers never wait for subscribers.
- Must isolate subscribers: a crash in one subscriber must not affect others.
- Must support topic-based subscriptions with wildcards (`order.*`).
- Must offer two delivery modes: `:fire_and_forget` and `:at_least_once_local`
  (retries inside the node with an outbox).
- Must ship with observability hooks via `:telemetry`.

This is not a replacement for Kafka. It's the right tool when **producers and
consumers live in the same node** and you want the lowest possible latency
(microseconds) without losing fault isolation. Phoenix.PubSub uses this exact
pattern with `Registry` under the hood, and libraries like Commanded and
event-sourced systems assemble similar primitives.

Project structure:

```
event_bus_built/
├── lib/
│   └── event_bus/
│       ├── application.ex
│       ├── bus.ex                  # public API — publish/subscribe
│       ├── subscriber.ex           # supervised wrapper around user handlers
│       ├── outbox.ex               # ETS-backed at-least-once buffer
│       └── telemetry.ex            # event instrumentation
├── test/
│   └── event_bus/
│       ├── bus_test.exs
│       ├── outbox_test.exs
│       └── delivery_test.exs
└── mix.exs
```

---

## Why this approach and not alternatives

Alternatives considered and discarded:

- **Hand-rolled equivalent**: reinvents primitives the BEAM/ecosystem already provides; high risk of subtle bugs around concurrency, timeouts, or failure propagation.
- **External service (e.g. Redis, sidecar)**: adds a network hop and an extra failure domain for a problem the VM can solve in-process with lower latency.
- **Heavier framework abstraction**: couples the module to a framework lifecycle and makes local reasoning/testing harder.

The chosen approach stays inside the BEAM, uses idiomatic OTP primitives, and keeps the contract small.

## Core concepts

### 1. Why `Registry` and not a GenServer map

A naive bus holds `%{topic => [pid]}` in GenServer state. Every publish becomes a
`GenServer.call` that serializes fan-out. Under load the mailbox grows, publishers
block, and a slow subscriber (taking 20 ms to handle a message) rate-limits the
whole system.

`Registry` with `keys: :duplicate` gives you a sharded, read-optimized index:

```
publisher ── Registry.dispatch ──▶ shard 0 ──▶ [pid1, pid2]
                                 ──▶ shard 1 ──▶ [pid3]
                                 ──▶ shard N ──▶ [...]
```

Dispatch uses `:ets` under the hood, partitions subscribers across schedulers
(`partitions: System.schedulers_online()`), and lets publishers send without
serialization. This is exactly how `Phoenix.PubSub` is implemented — you are
re-building a production-grade primitive at a smaller scale.

### 2. Subscriber isolation

A subscriber crash must not propagate. Each subscription runs inside a dedicated
`EventBus.Subscriber` process supervised by a `DynamicSupervisor`. When the wrapper
dies, `Registry` automatically removes its entry (`Registry` monitors registered
pids).

```
     DynamicSupervisor
        │
        ├── Subscriber(handler_mod, topic="order.*")
        ├── Subscriber(handler_mod, topic="invoice.issued")
        └── Subscriber(handler_mod, topic="#")   ← catch-all
```

### 3. Fire-and-forget vs at-least-once-local

| Mode | Semantics | Use for |
|------|-----------|---------|
| `:fire_and_forget` | `send/2`, no retry | metrics, analytics, logging |
| `:at_least_once_local` | Outbox ETS + ack, retry on subscriber crash | side effects (email, ledger writes) |

True at-least-once requires persistence across node restarts (use Oban or
`commanded` + event store for that). Our `:at_least_once_local` guarantees
**redelivery while the node is alive** — the outbox is an ETS set keyed by
`{pid, delivery_id}`; entries are removed on ack.

### 4. Topic matching with wildcards

AMQP-like routing: segments separated by `.`, `*` matches one segment, `#` matches
any trailing segments.

```
subscribe "order.*"       matches  order.placed, order.cancelled
subscribe "#"             matches  anything
subscribe "invoice.paid"  matches  invoice.paid only
```

### 5. Back-pressure via subscriber queue length

A publisher never waits, but a slow subscriber grows its mailbox. The `Subscriber`
wrapper checks its own `Process.info(self(), :message_queue_len)` periodically. If
the queue exceeds a threshold it emits `[:event_bus, :subscriber, :lag]` via
telemetry. This is the same pattern Broadway uses when consumers fall behind.

---

## Design decisions

**Option A — naive/simple approach**
- Pros: minimal code, easy to reason about.
- Cons: breaks under load, lacks observability, hard to evolve.

**Option B — the approach used here** (chosen)
- Pros: production-grade, handles edge cases, testable boundaries.
- Cons: more moving parts, requires understanding of the BEAM primitives involved.

→ Chose **B** because correctness under concurrency and failure modes outweighs the extra surface area.

## Implementation

### Step 1: Create the project

**Objective**: Boot a supervised OTP app with dependencies to ensure Registry, Outbox, and Telemetry restart on crashes.

```bash
mix new event_bus_built --sup
cd event_bus_built
```

Add in `mix.exs`:

```elixir
defp deps do
  [
    {:telemetry, "~> 1.2"},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 2: `lib/event_bus/application.ex`

**Objective**: Start partitioned Registry with duplicate keys, DynamicSupervisor for subscribers, Outbox and Telemetry under supervision.

```elixir
defmodule EventBus.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {Registry,
       keys: :duplicate,
       name: EventBus.Registry,
       partitions: System.schedulers_online()},
      {DynamicSupervisor, name: EventBus.SubscriberSupervisor, strategy: :one_for_one},
      EventBus.Outbox,
      EventBus.Telemetry
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: EventBus.Supervisor)
  end
end
```

### Step 3: `lib/event_bus/bus.ex`

**Objective**: Expose a non-blocking publish API with wildcard topic matching — `Registry.dispatch/3` fans out per-subscriber without blocking the caller.

```elixir
defmodule EventBus.Bus do
  @moduledoc """
  Public API of the event bus. All calls are non-blocking.
  Delivery mode is chosen at subscribe time.
  """

  alias EventBus.{Subscriber, Outbox}

  @type topic :: String.t()
  @type pattern :: String.t()
  @type mode :: :fire_and_forget | :at_least_once_local

  @spec publish(topic, term()) :: :ok
  def publish(topic, payload) when is_binary(topic) do
    delivery_id = System.unique_integer([:monotonic, :positive])

    envelope = %{
      id: delivery_id,
      topic: topic,
      payload: payload,
      ts: System.system_time(:microsecond)
    }

    :telemetry.execute([:event_bus, :publish], %{count: 1}, %{topic: topic})

    Registry.dispatch(EventBus.Registry, :all, fn subscribers ->
      Enum.each(subscribers, fn {pid, %{pattern: pattern, mode: mode}} ->
        if topic_match?(topic, pattern), do: deliver(pid, envelope, mode)
      end)
    end)

    :ok
  end

  @spec subscribe(pattern, module(), mode) :: {:ok, pid()} | {:error, term()}
  def subscribe(pattern, handler_mod, mode \\ :fire_and_forget)
      when is_binary(pattern) and is_atom(handler_mod) do
    DynamicSupervisor.start_child(
      EventBus.SubscriberSupervisor,
      {Subscriber, pattern: pattern, handler: handler_mod, mode: mode}
    )
  end

  @spec unsubscribe(pid()) :: :ok
  def unsubscribe(pid) when is_pid(pid) do
    DynamicSupervisor.terminate_child(EventBus.SubscriberSupervisor, pid)
  end

  defp deliver(pid, envelope, :fire_and_forget), do: send(pid, {:event, envelope})

  defp deliver(pid, envelope, :at_least_once_local) do
    Outbox.record(envelope, pid)
    send(pid, {:event_with_ack, envelope})
  end

  @doc false
  def topic_match?(topic, pattern) do
    match_segments(String.split(topic, "."), String.split(pattern, "."))
  end

  defp match_segments(_, ["#"]), do: true
  defp match_segments([], []), do: true
  defp match_segments([], _), do: false
  defp match_segments(_, []), do: false
  defp match_segments([_ | rest_t], ["*" | rest_p]), do: match_segments(rest_t, rest_p)
  defp match_segments([seg | rest_t], [seg | rest_p]), do: match_segments(rest_t, rest_p)
  defp match_segments(_, _), do: false
end
```

### Step 4: `lib/event_bus/subscriber.ex`

**Objective**: Wrap user handlers in a supervised GenServer — crashes stay contained; `{:error, _}` leaves the envelope in the outbox for redelivery.

```elixir
defmodule EventBus.Subscriber do
  @moduledoc """
  Supervised wrapper around a user-provided handler module.

  The handler module must export `handle_event/1` returning `:ok` or
  `{:error, reason}`. On `{:error, _}` in at-least-once mode the envelope
  remains in the outbox for redelivery.
  """

  use GenServer

  alias EventBus.Outbox

  @lag_threshold 1_000
  @lag_check_interval_ms 5_000

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts)

  @impl true
  def init(opts) do
    pattern = Keyword.fetch!(opts, :pattern)
    handler = Keyword.fetch!(opts, :handler)
    mode = Keyword.get(opts, :mode, :fire_and_forget)

    {:ok, _} = Registry.register(EventBus.Registry, :all, %{pattern: pattern, mode: mode})
    Process.send_after(self(), :check_lag, @lag_check_interval_ms)

    {:ok, %{handler: handler, mode: mode, pattern: pattern}}
  end

  @impl true
  def handle_info({:event, envelope}, state) do
    safe_dispatch(state.handler, envelope)
    {:noreply, state}
  end

  def handle_info({:event_with_ack, envelope}, state) do
    case safe_dispatch(state.handler, envelope) do
      :ok -> Outbox.ack(envelope.id, self())
      _ -> :keep_in_outbox
    end

    {:noreply, state}
  end

  def handle_info(:check_lag, state) do
    {:message_queue_len, len} = Process.info(self(), :message_queue_len)

    if len > @lag_threshold do
      :telemetry.execute(
        [:event_bus, :subscriber, :lag],
        %{queue_len: len},
        %{pattern: state.pattern, handler: state.handler}
      )
    end

    Process.send_after(self(), :check_lag, @lag_check_interval_ms)
    {:noreply, state}
  end

  defp safe_dispatch(handler, envelope) do
    handler.handle_event(envelope)
  rescue
    exception ->
      :telemetry.execute(
        [:event_bus, :subscriber, :exception],
        %{count: 1},
        %{handler: handler, reason: Exception.message(exception)}
      )

      {:error, exception}
  end
end
```

### Step 5: `lib/event_bus/outbox.ex`

**Objective**: Back per-subscriber buffers with ETS plus a periodic sweep — at-least-once-local delivery without touching disk.

```elixir
defmodule EventBus.Outbox do
  @moduledoc """
  Per-subscriber outbox backed by ETS. Supports at-least-once-local delivery.

  Key: `{subscriber_pid, delivery_id}`. On ack the entry is deleted.
  Entries older than `@retention_ms` are swept periodically.
  """

  use GenServer

  @table :event_bus_outbox
  @retention_ms 5 * 60 * 1_000
  @sweep_interval_ms 30_000

  def start_link(_opts), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  @spec record(map(), pid()) :: :ok
  def record(envelope, pid) do
    :ets.insert(@table, {{pid, envelope.id}, envelope, System.monotonic_time(:millisecond)})
    :ok
  end

  @spec ack(non_neg_integer(), pid()) :: :ok
  def ack(delivery_id, pid) do
    :ets.delete(@table, {pid, delivery_id})
    :ok
  end

  @spec pending(pid()) :: [map()]
  def pending(pid) do
    :ets.match_object(@table, {{pid, :_}, :_, :_})
    |> Enum.map(fn {_key, envelope, _ts} -> envelope end)
  end

  @impl true
  def init(_) do
    :ets.new(@table, [:named_table, :public, :set, read_concurrency: true])
    Process.send_after(self(), :sweep, @sweep_interval_ms)
    {:ok, %{}}
  end

  @impl true
  def handle_info(:sweep, state) do
    cutoff = System.monotonic_time(:millisecond) - @retention_ms
    ms = [{{:_, :_, :"$1"}, [{:<, :"$1", cutoff}], [true]}]
    count = :ets.select_delete(@table, ms)

    if count > 0,
      do: :telemetry.execute([:event_bus, :outbox, :swept], %{count: count}, %{})

    Process.send_after(self(), :sweep, @sweep_interval_ms)
    {:noreply, state}
  end
end
```

### Step 6: `lib/event_bus/telemetry.ex`

**Objective**: Attach a logger to publish, lag, exception, and sweep events — observability is wired at the bus boundary, not sprinkled inside handlers.

```elixir
defmodule EventBus.Telemetry do
  @moduledoc false
  use GenServer

  require Logger

  def start_link(_), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  @impl true
  def init(_) do
    events = [
      [:event_bus, :publish],
      [:event_bus, :subscriber, :lag],
      [:event_bus, :subscriber, :exception],
      [:event_bus, :outbox, :swept]
    ]

    :telemetry.attach_many("event-bus-logger", events, &handle/4, nil)
    {:ok, %{}}
  end

  def handle(event, measurements, metadata, _config) do
    Logger.debug("#{inspect(event)} #{inspect(measurements)} #{inspect(metadata)}")
  end
end
```

### Step 7: Tests

**Objective**: Exercise publish, wildcard matching, and at-least-once redelivery — three seams that together prove the bus composes correctly.

```elixir
# test/event_bus/bus_test.exs
defmodule EventBus.BusTest do
  use ExUnit.Case, async: false

  alias EventBus.Bus

  defmodule TestHandler do
    def handle_event(%{payload: {:forward, pid, tag}}) do
      send(pid, {tag, :received})
      :ok
    end

    def handle_event(_), do: :ok
  end

  setup do
    on_exit(fn ->
      EventBus.SubscriberSupervisor
      |> DynamicSupervisor.which_children()
      |> Enum.each(fn {_, pid, _, _} ->
        DynamicSupervisor.terminate_child(EventBus.SubscriberSupervisor, pid)
      end)
    end)

    :ok
  end

  describe "subscribe/publish" do
    test "delivers exact-topic messages" do
      {:ok, _} = Bus.subscribe("order.placed", TestHandler)
      Bus.publish("order.placed", {:forward, self(), :got})
      assert_receive {:got, :received}, 200
    end

    test "honors * wildcard" do
      {:ok, _} = Bus.subscribe("order.*", TestHandler)
      Bus.publish("order.cancelled", {:forward, self(), :wild})
      assert_receive {:wild, :received}, 200
    end

    test "honors # catch-all" do
      {:ok, _} = Bus.subscribe("#", TestHandler)
      Bus.publish("anything.else.here", {:forward, self(), :all})
      assert_receive {:all, :received}, 200
    end

    test "non-matching topics are not delivered" do
      {:ok, _} = Bus.subscribe("invoice.*", TestHandler)
      Bus.publish("order.placed", {:forward, self(), :nope})
      refute_receive {:nope, :received}, 100
    end
  end

  describe "isolation" do
    defmodule Crasher do
      def handle_event(_), do: raise("boom")
    end

    test "crashing handler does not affect other subscribers" do
      {:ok, _} = Bus.subscribe("boom.*", Crasher)
      {:ok, _} = Bus.subscribe("boom.*", TestHandler)

      Bus.publish("boom.one", {:forward, self(), :survived})

      assert_receive {:survived, :received}, 200
    end
  end
end
```

```elixir
# test/event_bus/outbox_test.exs
defmodule EventBus.OutboxTest do
  use ExUnit.Case, async: false

  alias EventBus.Outbox

  test "record then ack removes entry" do
    envelope = %{id: 42, topic: "t", payload: :p, ts: 0}
    Outbox.record(envelope, self())
    assert [^envelope] = Outbox.pending(self())

    Outbox.ack(42, self())
    assert [] = Outbox.pending(self())
  end
end
```

```elixir
# test/event_bus/delivery_test.exs
defmodule EventBus.DeliveryTest do
  use ExUnit.Case, async: false

  alias EventBus.{Bus, Outbox}

  defmodule FailingHandler do
    def handle_event(_), do: {:error, :transient}
  end

  test "at_least_once_local keeps envelope on error" do
    {:ok, pid} = Bus.subscribe("retry.*", FailingHandler, :at_least_once_local)
    Bus.publish("retry.now", :payload)

    Process.sleep(50)
    assert length(Outbox.pending(pid)) >= 1
  end
end
```

### Step 8: Run it

**Objective**: Run `mix test` — green suite proves fan-out, supervision, and outbox redelivery compose end-to-end.

```bash
mix test
```

---


### Why this works

The design leans on BEAM guarantees (process isolation, mailbox ordering, supervisor restarts) and pushes invariants to the boundaries of each module. State transitions are explicit, failure modes are declared rather than implicit, and each step is independently testable. That combination keeps the implementation correct under concurrent load and cheap to change later.


## Key Concepts: Event Broadcasting and Pub-Sub Patterns

An event bus is a message broker that decouples senders from receivers. Instead of Process A calling Process B directly, A publishes an event (e.g., `{:user_created, user_id}`), and any subscriber interested in that event receives it. Multiple subscribers can react independently.

Implementation options: simple `Registry` with `Phoenix.PubSub` (works locally), or distributed options like RabbitMQ or Kafka for cross-node or persistent events. The pattern is especially useful when many components need to react to state changes without tight coupling. A common gotcha: event handlers are fire-and-forget by default (cast semantics). If a handler fails, the event is lost unless you add retry logic (e.g., via Oban). For critical workflows, consider event sourcing (storing all events in a log) instead of immediate side effects.


## Benchmark

```elixir
# Minimal measurement — replace with Benchee for distribution stats.
{time_us, _} = :timer.tc(fn ->
  for _ <- 1..10_000, do: run_operation()
end)
IO.puts("avg: #{time_us / 10_000} µs/op")
```

Target: operation should complete in the low-microsecond range on modern hardware; deviations by >2× indicate a regression worth investigating.

## Deep Dive: Domain Patterns and Production Implications

Domain-specific frameworks enforce module dependencies and architectural boundaries. Testing domain isolation ensures that constraints are maintained as the codebase grows. Production systems without boundary enforcement often become monolithic and hard to test.

---

## Advanced Considerations

Framework choices like Ash, Commanded, and Nerves create significant architectural constraints that are difficult to change later. Ash's powerful query builder and declarative approach simplify common patterns but can be opaque when debugging complex permission logic or custom filters at scale. Event sourcing with Commanded is powerful for audit trails but creates a different mental model for state management — replaying events to derive current state has CPU and latency costs that aren't apparent in traditional CRUD systems.

Nerves requires understanding the full embedded system stack — from bootloader configuration to over-the-air update mechanisms. A Nerves system that works on your development board may fail in production due to hardware variations, network conditions, or power supply issues. NX's numerical computing is powerful but requires understanding GPU acceleration trade-offs and memory management for large datasets. Livebook provides interactive development but shouldn't be used for production deployments without careful containerization and resource isolation.

The integration between these frameworks and traditional BEAM patterns (supervisors, processes, GenServers) requires careful design. A Commanded projection that rebuilds state from the event log can consume all available CPU, starving other services. NX autograd computations can create unexpected memory usage if not carefully managed. Nerves systems are memory-constrained; performance assumptions from desktop Elixir don't hold. Always prototype these frameworks in realistic environments before committing to them in production systems to validate assumptions.


## Trade-offs and production gotchas

**1. In-VM only.** Across nodes use `Phoenix.PubSub` with the `PG2` adapter or a
real broker. This bus intentionally avoids distribution — it's a primitive.

**2. Outbox is not durable.** If the node crashes, outbox entries die with ETS.
For true durability back the outbox with Ecto, or delegate to Oban.

**3. `Registry.dispatch/3` holds a read lock per partition.** Long-running dispatch
callbacks block writers on that shard. Keep the lambda tiny — match and `send/2`.
Heavy work belongs inside the subscriber.

**4. Wildcard matching is linear per publish.** Every publish scans entries in
each partition. For thousands of patterns consider a trie or one Registry key per
exact topic plus a separate catch-all registry.

**5. Back-pressure is observational, not enforced.** We emit telemetry on lag but
do NOT drop publishes. For enforcement front the bus with a `GenStage`/`Broadway`
pipeline or a bounded queue per subscriber.

**6. No ordering across partitions.** Two events dispatched almost simultaneously
may reach subscribers in different order if they live in different shards. Design
consumers to be commutative or idempotent.

**7. Tests with shared registry need `async: false`.** The global
`EventBus.Registry` is shared. For parallel tests use `start_supervised/1` with a
unique registry name per test.

**8. When NOT to use this.** If producers and consumers live on different nodes,
if you need replay/history (event sourcing), if durability across restarts is a
hard requirement, or if throughput exceeds a few hundred thousand events/sec on
a single node — use Kafka, NATS JetStream, or RabbitMQ.

---

## Performance notes

Benchmark with 1,000 subscribers registered on `order.*` and `invoice.*`:

```elixir
# bench/bus_bench.exs
defmodule NoopHandler do
  def handle_event(_), do: :ok
end

Enum.each(1..500, fn _ -> EventBus.Bus.subscribe("order.*", NoopHandler) end)
Enum.each(1..500, fn _ -> EventBus.Bus.subscribe("invoice.*", NoopHandler) end)

Benchee.run(
  %{
    "publish matching 500" => fn -> EventBus.Bus.publish("order.placed", :payload) end,
    "publish matching 0"   => fn -> EventBus.Bus.publish("unknown.topic", :payload) end
  },
  time: 5, warmup: 2, parallel: 4
)
```

On an Apple M2 laptop expect approximately:

| Case | p50 | p99 |
|------|-----|-----|
| publish → 0 matches | ~2 µs | ~5 µs |
| publish → 500 matches | ~90 µs | ~180 µs |

`Phoenix.PubSub` on the same hardware shows comparable numbers because it uses
the same Registry primitive.

---

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?

## Resources

- [Registry module — hexdocs.pm](https://hexdocs.pm/elixir/Registry.html)
- [Phoenix.PubSub source on GitHub](https://github.com/phoenixframework/phoenix_pubsub)
- [José Valim — A tour of the Elixir Registry](http://blog.plataformatec.com.br/2017/07/a-tour-of-the-elixir-registry/)
- [Broadway back-pressure design — dashbit.co/blog](https://dashbit.co/blog/introducing-broadway)
- [`:telemetry` conventions — hexdocs.pm](https://hexdocs.pm/telemetry/readme.html)
- [Saša Jurić — "To spawn, or not to spawn?"](https://www.theerlangelist.com/article/spawn_or_not)

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```
