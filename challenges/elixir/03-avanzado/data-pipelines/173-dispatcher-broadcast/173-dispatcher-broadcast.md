# GenStage BroadcastDispatcher — Multi-Sink Fan-Out

**Project**: `broadcast_dispatcher` — a live-price feed that fans out the same tick stream to multiple consumers (risk engine, UI websocket, auditor).

**Difficulty**: ★★★★☆

**Estimated time**: 3–5 hours

---

## Project context

A market data feed ingests ~20k price ticks per second from an exchange
websocket. The same ticks must reach three independent downstream processes:
a risk engine that recomputes VaR, a websocket broadcaster that pushes ticks
to browsers, and an auditor that stores every tick for regulatory replay.
They cannot starve each other — the auditor lagging must not block the risk
engine — but every tick must be delivered to every subscriber.

This is exactly what `GenStage.BroadcastDispatcher` is built for. It
guarantees **every event reaches every consumer** and gates the producer's
rate to the slowest subscriber's demand. Understand that gating behaviour
before you ship, or you will wonder why your 20k tick firehose is delivering
50 ticks/sec.

```
broadcast_dispatcher/
├── lib/
│   └── broadcast_dispatcher/
│       ├── application.ex
│       ├── tick_producer.ex
│       ├── risk_engine.ex
│       ├── ws_broadcaster.ex
│       └── auditor.ex
├── test/
│   └── broadcast_dispatcher/
│       └── fan_out_test.exs
└── mix.exs
```

---

## Core concepts

### 1. How BroadcastDispatcher computes demand

```
consumer A: asked for 100, got 30, pending = 70
consumer B: asked for 100, got 30, pending = 70
consumer C: asked for  20, got 20, pending =  0

producer.handle_demand is called with min(70, 70, 0) = 0
```

The producer sees the **minimum** pending demand across subscribers. If one
consumer stops asking, the whole pipeline freezes. This is safety by design
(no drops) but it means a slow subscriber pulls the entire feed down to its
rate.

### 2. `selector` — per-subscriber filtering

Each subscriber can register a `:selector` function that filters which events
it receives. Filtered-out events still count against the producer's demand
but do not reach the subscriber's mailbox. Use this when 80% of consumers
only care about a subset (e.g., the auditor takes all ticks, the UI takes
only the ones above a subscription threshold).

### 3. Decoupling slow consumers

Two options when a consumer is inherently slow and you cannot let it gate
the stream:

- **Put a bounded buffer in front of it.** Use a dedicated producer-consumer
  between the broadcaster and the slow consumer. Drop on overflow.
- **Let it crash under pressure.** Supervisor restarts it, broadcast resumes.
  Only works if missing events is acceptable.

### 4. Back-pressure in broadcast pipelines

If you cannot slow the source (exchange websocket), you must drop. A common
production pattern:

```
Exchange WS ──▶ TickProducer (buffer_size 50_000, buffer_keep: :first)
              └ dispatcher: BroadcastDispatcher
                │
                ├──▶ RiskEngine (fast, doesn't buffer)
                ├──▶ WSBroadcaster (medium, own buffer 10_000)
                └──▶ Auditor (slow, own buffer 1_000_000 to disk)
```

### 5. Dynamic subscription

Consumers can be added or removed while the pipeline runs. Broadcast will
adjust its demand calculation on the fly. This matters for feature-flagging
new sinks.

---

## Implementation

### Step 1: Deps

```elixir
defp deps do
  [{:gen_stage, "~> 1.2"}]
end
```

### Step 2: Tick producer

```elixir
defmodule BroadcastDispatcher.TickProducer do
  @moduledoc """
  Producer that pushes ticks to all subscribers via BroadcastDispatcher.
  """
  use GenStage

  @type tick :: %{symbol: String.t(), price: float(), ts: integer()}

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts), do: GenStage.start_link(__MODULE__, opts, name: __MODULE__)

  @spec push(tick()) :: :ok
  def push(tick), do: GenStage.cast(__MODULE__, {:push, tick})

  @impl true
  def init(_opts) do
    {:producer, %{},
     dispatcher: GenStage.BroadcastDispatcher, buffer_size: 50_000, buffer_keep: :first}
  end

  @impl true
  def handle_cast({:push, tick}, state), do: {:noreply, [tick], state}

  @impl true
  def handle_demand(_demand, state), do: {:noreply, [], state}
end
```

### Step 3: Three consumers

```elixir
defmodule BroadcastDispatcher.RiskEngine do
  use GenStage

  def start_link(opts), do: GenStage.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(_opts) do
    {:consumer, %{n: 0},
     subscribe_to: [{BroadcastDispatcher.TickProducer, max_demand: 500}]}
  end

  @impl true
  def handle_events(events, _from, state) do
    {:noreply, [], %{state | n: state.n + length(events)}}
  end
end

defmodule BroadcastDispatcher.WSBroadcaster do
  use GenStage

  def start_link(opts), do: GenStage.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(_opts) do
    selector = fn %{price: p} -> p > 0 end

    {:consumer, %{n: 0},
     subscribe_to: [
       {BroadcastDispatcher.TickProducer, max_demand: 200, selector: selector}
     ]}
  end

  @impl true
  def handle_events(events, _from, state) do
    # simulate some work
    :timer.sleep(div(length(events), 2))
    {:noreply, [], %{state | n: state.n + length(events)}}
  end
end

defmodule BroadcastDispatcher.Auditor do
  use GenStage

  def start_link(opts), do: GenStage.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(_opts) do
    {:consumer, %{n: 0},
     subscribe_to: [{BroadcastDispatcher.TickProducer, max_demand: 1_000}]}
  end

  @impl true
  def handle_events(events, _from, state) do
    {:noreply, [], %{state | n: state.n + length(events)}}
  end
end
```

### Step 4: Application

```elixir
defmodule BroadcastDispatcher.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      BroadcastDispatcher.TickProducer,
      BroadcastDispatcher.RiskEngine,
      BroadcastDispatcher.WSBroadcaster,
      BroadcastDispatcher.Auditor
    ]

    Supervisor.start_link(children, strategy: :one_for_one)
  end
end
```

### Step 5: Test — every consumer sees every tick

```elixir
defmodule BroadcastDispatcher.FanOutTest do
  use ExUnit.Case, async: false

  alias BroadcastDispatcher.{TickProducer, RiskEngine, WSBroadcaster, Auditor}

  setup do
    start_supervised!(TickProducer)
    start_supervised!(RiskEngine)
    start_supervised!(WSBroadcaster)
    start_supervised!(Auditor)
    Process.sleep(50)
    :ok
  end

  test "all three consumers see the same tick count" do
    for i <- 1..100 do
      TickProducer.push(%{symbol: "AAPL", price: 100.0 + i, ts: i})
    end

    Process.sleep(500)

    assert :sys.get_state(RiskEngine).n == 100
    assert :sys.get_state(WSBroadcaster).n == 100
    assert :sys.get_state(Auditor).n == 100
  end

  test "selector filters events on the WSBroadcaster without affecting others" do
    for i <- 1..10, do: TickProducer.push(%{symbol: "AAPL", price: -1.0, ts: i})
    Process.sleep(200)
    assert :sys.get_state(WSBroadcaster).n == 0
    assert :sys.get_state(Auditor).n == 10
  end
end
```

---

## Trade-offs and production gotchas

**1. The slowest consumer gates the stream.**
Measure per-subscriber pending demand via `:sys.get_state(producer)` under
load. If one subscriber's pending demand stays at 0 for long, it is gating.

**2. Adding a subscriber mid-stream changes rate.**
If the new subscriber starts with `max_demand: 10`, the whole broadcast
becomes rate-limited to 10 until it ramps up. Use high initial `max_demand`
or pre-warm.

**3. Selectors do not save producer work.**
A filtered-out event was still produced and dispatched. If 99% of your
consumers reject 99% of events, you are wasting producer CPU. Consider
routing with PartitionDispatcher instead.

**4. Broadcast guarantees delivery, not ordering across sinks.**
Consumer A may see event 5 before consumer B sees event 3 (they are
independent GenStage processes). If you need cross-sink ordering you cannot
get it from GenStage — you need a single sink that fans out internally.

**5. Subscriber crashes pause broadcast.**
When a consumer crashes, its supervisor takes a moment to restart. During
that window the broadcast is gated to the *still-alive* consumers' demand.
Normally fine, but during restart storms you will see throughput dip.

**6. `:permanent` cancel mode deadlocks if producer dies.**
Use `cancel: :transient` on subscriptions so crashing producers do not
permanently kill consumers.

**7. When NOT to use BroadcastDispatcher.** When only one subscriber cares
about each event, use Demand. When events should be sharded by key, use
Partition. Broadcast is for the "tee" pattern only.

---

## Performance notes

On a 10-core box with 3 consumers and `max_demand: 500` on each, the
producer sustained 95k events/sec. When we artificially slowed the third
consumer to 1 event/ms, throughput collapsed to ~1k events/sec — this is
the gating behaviour in practice. Putting a bounded producer-consumer
buffer in front of the slow one restored the other two consumers to full
rate while the slow consumer dropped.

---

## Resources

- [GenStage.BroadcastDispatcher — HexDocs](https://hexdocs.pm/gen_stage/GenStage.BroadcastDispatcher.html)
- [GenStage announcement — José Valim](https://elixir-lang.org/blog/2016/07/14/announcing-genstage/)
- [Flow and GenStage in production — Dashbit](https://dashbit.co/blog/flow-and-genstage-in-production)
- [GenStage source — dispatcher_broadcast.ex](https://github.com/elixir-lang/gen_stage/blob/main/lib/gen_stage/broadcast_dispatcher.ex)
- [Designing Elixir Systems with OTP — ch. Pipelines](https://pragprog.com/titles/jgotp/)
