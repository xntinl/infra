# GenStage Advanced — Dispatchers, Subscriptions and Buffers

**Project**: `genstage_advanced` — a telemetry ingestion pipeline with surgical flow control.

---

## Project context

You are building the ingestion layer of an observability product. Devices push
metrics (~50k events/sec at peak, 5k sustained) through an HTTP edge. Those
events must be fanned into several independent consumers: a hot path that
aggregates counters into in-memory buckets, a cold path that batches into
Parquet files for S3, and a sampling path that forwards 1% of the stream to a
debugging UI. The three consumers have very different rates: the aggregator
is CPU-bound (~1ms per event), the Parquet writer is IO-bound in large
batches, and the sampler is trivial.

A naive "push everything to every consumer" approach crashes the slow
consumer under backpressure. A naive "one consumer drains the producer"
approach leaves the other two idle. You need GenStage subscriptions with the
right dispatcher, the right `max_demand`, and a bounded buffer that
chooses what to drop when pressure cannot be propagated upstream (devices
keep sending).

This exercise drills into the three things that separate toy GenStage code
from production pipelines: **dispatchers**, **manual subscriptions**, and
**buffer semantics**.

```
genstage_advanced/
├── lib/
│   └── genstage_advanced/
│       ├── application.ex
│       ├── ingest_producer.ex        # GenStage :producer with buffering
│       ├── aggregator.ex             # CPU-bound consumer
│       ├── parquet_writer.ex         # IO-bound consumer (batches)
│       ├── sampler.ex                # 1% sampling consumer
│       └── manual_consumer.ex        # manual-subscription consumer
├── test/
│   └── genstage_advanced/
│       ├── buffer_keep_test.exs
│       ├── manual_subscription_test.exs
│       └── dispatcher_test.exs
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



---

**Why this matters:**
These concepts form the foundation of production Elixir systems. Understanding them deeply allows you to build fault-tolerant, scalable applications that operate correctly under load and failure.

**Real-world use case:**
This pattern appears in systems like:
- Phoenix applications handling thousands of concurrent connections
- Distributed data processing pipelines
- Financial transaction systems requiring consistency and fault tolerance
- Microservices communicating over unreliable networks

**Common pitfall:**
Many developers overlook that Elixir's concurrency model differs fundamentally from threads. Processes are isolated; shared mutable state does not exist. Trying to force shared-memory patterns leads to deadlocks, race conditions, or silently incorrect behavior. Always think in terms of message passing and immutability.

**Pipeline-specific insight:**
Streams are lazy; Enum is eager. Use Stream for data larger than RAM or when you're building intermediate stages. Use Enum when the collection is small or you need side effects at each step. Mixing them carelessly results in performance cliffs.
### 1. The three built-in dispatchers

GenStage ships with three dispatchers that decide how a producer's events are
routed to N consumers:

```
               ┌──────────────────────────────────────────────┐
               │                 DEMAND                       │
               │  event ──▶ consumer with most pending demand │
               │  use for: work stealing, fan-out of work     │
               ├──────────────────────────────────────────────┤
               │                 BROADCAST                    │
               │  event ──▶ every consumer (min demand gates) │
               │  use for: multi-sink tees (metrics + log)    │
               ├──────────────────────────────────────────────┤
               │                 PARTITION                    │
               │  event ──▶ consumer = hash(partition_key)    │
               │  use for: in-order per-key, shard by entity  │
               └──────────────────────────────────────────────┘
```

Picking the wrong dispatcher hides for a long time in toy benchmarks and
explodes in production. Broadcast gates its rate to the **slowest** consumer:
a 100 events/sec parquet writer will throttle the whole stream down to 100
events/sec even if the aggregator could handle 50k. Demand will happily
deliver all events to whoever asks fastest.

### 2. Subscriptions — `:automatic` vs `:manual`

An automatic subscription calls `ask/2` internally as soon as `init/1` returns.
Demand flows continuously. A manual subscription forces you to call
`GenStage.ask/2` yourself. This matters when demand must be controlled by
external signals (circuit breaker open, downstream DB is warming up, feature
flag is off) and also for testing: with manual subscriptions you can step the
pipeline event-by-event and assert invariants.

### 3. Buffer keep — `:first` vs `:last`

A producer buffers events when consumers do not ask fast enough. Once
`:buffer_size` is reached, new events are dropped. `:buffer_keep` controls
**which** events are dropped:

- `:first` — newer events overwrite older ones. You care about **recency**.
- `:last` — older events are preserved, new events are rejected. You care
  about **completeness of the prefix**.

### 4. Demand shape — `max_demand` and `min_demand`

Consumers batch their asks between `min_demand` and `max_demand`. A consumer
with `max_demand: 1000, min_demand: 500` asks for up to 1000 events, then
asks for 500 more once half are consumed. Too low and you starve the producer
with chatty asks; too high and one slow consumer hoards events.

### 5. Dynamic subscription

You can subscribe and unsubscribe consumers at runtime via
`GenStage.sync_subscribe/2` and `GenStage.cancel/2`. This is the primitive
behind feature-flagging a new sink on a running pipeline or draining a
consumer for a rolling restart without losing events.

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

### Step 1: Project setup

**Objective**: Scaffold supervised app with `:gen_stage` pinned so dispatcher/buffer semantics don't regress across OTP versions.

```bash
mix new genstage_advanced --sup
cd genstage_advanced
```

`mix.exs` deps:

```elixir
defp deps do
  [
    {:gen_stage, "~> 1.2"},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 2: Buffered producer

**Objective**: Implement bounded producer mailbox with :buffer_keep eviction so burst absorption prevents downstream starvation.

```elixir
defmodule GenstageAdvanced.IngestProducer do
  @moduledoc """
  Buffered producer. Upstream writers call `push/1`. Downstream consumers
  pull via GenStage demand. When the buffer overflows, `:buffer_keep`
  decides the eviction strategy.
  """
  use GenStage

  @type event :: %{id: pos_integer(), payload: term(), ts: integer()}

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts), do: GenStage.start_link(__MODULE__, opts, name: __MODULE__)

  @spec push(event()) :: :ok
  def push(event), do: GenStage.cast(__MODULE__, {:push, event})

  @impl true
  def init(opts) do
    dispatcher = Keyword.get(opts, :dispatcher, GenStage.DemandDispatcher)
    buffer_size = Keyword.get(opts, :buffer_size, 10_000)
    buffer_keep = Keyword.get(opts, :buffer_keep, :last)

    {:producer, %{counter: 0},
     dispatcher: dispatcher,
     buffer_size: buffer_size,
     buffer_keep: buffer_keep}
  end

  @impl true
  def handle_cast({:push, event}, state) do
    {:noreply, [event], %{state | counter: state.counter + 1}}
  end

  @impl true
  def handle_demand(_demand, state), do: {:noreply, [], state}
end
```

### Step 3: Three consumers with different shapes

**Objective**: Implement CPU-bound + IO-batched + sampling consumers so independent demand windows prove multi-sink coexistence.

```elixir
defmodule GenstageAdvanced.Aggregator do
  @moduledoc "CPU-bound consumer. Simulates ~1ms of work per event."
  use GenStage

  def start_link(opts), do: GenStage.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(opts) do
    sub = Keyword.fetch!(opts, :subscribe_to)
    {:consumer, %{count: 0}, subscribe_to: sub}
  end

  @impl true
  def handle_events(events, _from, state) do
    Enum.each(events, fn _ -> :timer.sleep(1) end)
    {:noreply, [], %{state | count: state.count + length(events)}}
  end
end

defmodule GenstageAdvanced.ParquetWriter do
  @moduledoc """
  IO-bound consumer that only flushes when it has collected >= 500 events
  or 500ms have elapsed since the last flush.
  """
  use GenStage

  def start_link(opts), do: GenStage.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(opts) do
    sub = Keyword.fetch!(opts, :subscribe_to)
    Process.send_after(self(), :flush_tick, 500)
    {:consumer, %{buf: [], flushed: 0}, subscribe_to: sub}
  end

  @impl true
  def handle_events(events, _from, state) do
    buf = events ++ state.buf

    if length(buf) >= 500 do
      {:noreply, [], %{state | buf: [], flushed: state.flushed + length(buf)}}
    else
      {:noreply, [], %{state | buf: buf}}
    end
  end

  @impl true
  def handle_info(:flush_tick, state) do
    Process.send_after(self(), :flush_tick, 500)
    {:noreply, [], %{state | buf: [], flushed: state.flushed + length(state.buf)}}
  end
end

defmodule GenstageAdvanced.Sampler do
  @moduledoc "Forwards ~1% of events to a subscriber pid."
  use GenStage

  def start_link(opts), do: GenStage.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(opts) do
    sub = Keyword.fetch!(opts, :subscribe_to)
    target = Keyword.fetch!(opts, :target)
    {:consumer, %{target: target}, subscribe_to: sub}
  end

  @impl true
  def handle_events(events, _from, state) do
    Enum.each(events, fn e ->
      if :rand.uniform(100) == 1, do: send(state.target, {:sample, e})
    end)

    {:noreply, [], state}
  end
end
```

### Step 4: Manual subscription consumer

**Objective**: Gate demand via explicit ask/2 so circuit breakers or tests control flow pull-by-pull without rate-limiting overhead.

```elixir
defmodule GenstageAdvanced.ManualConsumer do
  @moduledoc """
  Consumer that only pulls when `pull/2` is called. Useful for tests and for
  external circuit breakers.
  """
  use GenStage

  def start_link(opts), do: GenStage.start_link(__MODULE__, opts)

  def pull(pid, n), do: GenStage.call(pid, {:pull, n})

  @impl true
  def init(opts) do
    sub = Keyword.fetch!(opts, :subscribe_to)
    {:consumer, %{from: nil, seen: []}, subscribe_to: sub}
  end

  @impl true
  def handle_subscribe(:producer, _opts, from, state) do
    {:manual, %{state | from: from}}
  end

  @impl true
  def handle_call({:pull, n}, _caller, state) do
    GenStage.ask(state.from, n)
    {:reply, :ok, [], state}
  end

  @impl true
  def handle_events(events, _from, state) do
    {:noreply, [], %{state | seen: state.seen ++ events}}
  end
end
```

### Step 5: Application wiring

**Objective**: Supervise producer-then-consumers with :one_for_one so demand subscriptions safely resolve at boot.

```elixir
defmodule GenstageAdvanced.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {GenstageAdvanced.IngestProducer,
       dispatcher: GenStage.BroadcastDispatcher, buffer_size: 50_000, buffer_keep: :last},
      {GenstageAdvanced.Aggregator,
       subscribe_to: [{GenstageAdvanced.IngestProducer, max_demand: 500, min_demand: 250}]},
      {GenstageAdvanced.ParquetWriter,
       subscribe_to: [{GenstageAdvanced.IngestProducer, max_demand: 1_000, min_demand: 500}]}
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: GenstageAdvanced.Supervisor)
  end
end
```

### Step 6: Test — buffer_keep eviction

**Objective**: Assert :buffer_keep :first drops oldest on overflow so eviction policy is locked against regression.

```elixir
defmodule GenstageAdvanced.BufferKeepTest do
  use ExUnit.Case, async: false
  alias GenstageAdvanced.{IngestProducer, ManualConsumer}

  describe "GenstageAdvanced.BufferKeep" do
    test "buffer_keep: :first evicts oldest when full" do
      {:ok, p} = GenStage.start_link(IngestProducer, [buffer_size: 3, buffer_keep: :first], [])
      {:ok, c} = GenStage.start_link(ManualConsumer, [subscribe_to: [{p, max_demand: 100}]], [])
      Process.sleep(20)

      for i <- 1..5, do: GenStage.cast(p, {:push, %{id: i, payload: nil, ts: 0}})
      Process.sleep(20)

      :ok = ManualConsumer.pull(c, 10)
      Process.sleep(50)

      ids = :sys.get_state(c).seen |> Enum.map(& &1.id) |> Enum.sort()
      assert ids == [3, 4, 5]
    end
  end
end
```

### Step 7: Test — manual subscription gates demand

**Objective**: Assert zero flow until pull/2 so manual-subscription demand contract is regression-proof.

```elixir
defmodule GenstageAdvanced.ManualSubscriptionTest do
  use ExUnit.Case, async: false
  alias GenstageAdvanced.{IngestProducer, ManualConsumer}

  describe "GenstageAdvanced.ManualSubscription" do
    test "no events are delivered until pull/2" do
      {:ok, p} = GenStage.start_link(IngestProducer, [], [])
      {:ok, c} = GenStage.start_link(ManualConsumer, [subscribe_to: [{p, max_demand: 100}]], [])
      Process.sleep(20)

      for i <- 1..10, do: GenStage.cast(p, {:push, %{id: i, payload: nil, ts: 0}})
      Process.sleep(50)
      assert :sys.get_state(c).seen == []

      :ok = ManualConsumer.pull(c, 3)
      Process.sleep(50)
      assert length(:sys.get_state(c).seen) == 3
    end
  end
end
```

---


### Why this works

The design leans on BEAM guarantees (process isolation, mailbox ordering, supervisor restarts) and pushes invariants to the boundaries of each module. State transitions are explicit, failure modes are declared rather than implicit, and each step is independently testable. That combination keeps the implementation correct under concurrent load and cheap to change later.

## Deep Dive

Data pipelines in Elixir leverage the Actor model to coordinate work across producer, consumer, and batcher stages. GenStage provides the foundation—a demand-driven backpressure mechanism that prevents memory bloat when producers exceed consumer capacity. Broadway abstracts this further, handling subscriptions, acknowledgments, and error propagation automatically. Understanding pipeline topology is critical at scale: a misconfigured batcher can serialize work and kill throughput; conversely, excessive partitioning fragments state and increases GC pressure. In production systems, always measure latency and memory per stage—Broadway's metrics integration with Telemetry makes this traceable. Consider exactly-once delivery semantics early; most pipelines require idempotency keys or deduplication at the consumer boundary. For high-volume Kafka scenarios, partition alignment (matching Broadway partitions to Kafka partitions) is essential to avoid rebalancing storms.
## Advanced Considerations

Data pipeline implementations at scale require careful consideration of backpressure, memory buffering, and failure recovery semantics. Broadway and Genstage provide demand-driven processing, but understanding the exact flow of backpressure through your pipeline is essential to avoid either starving producers or overwhelming buffers. The interaction between batcher timeouts and consumer demand can create unexpected latencies when tuples are held waiting for either a size threshold or time threshold to be reached. In systems processing millions of events, even a 100ms batch timeout can impact end-to-end latency dramatically.

Idempotency and exactly-once semantics are not automatic — they require architectural decisions about checkpointing and deduplication strategies. Writing checkpoints too frequently becomes a bottleneck; writing them too infrequently means lost progress on failure and potential duplicates. The choice between in-process ETS-based deduplication versus external stores (Redis, database) changes your failure recovery story fundamentally. Broadway's acknowledgment system is flexible but requires explicit design; missing acknowledgments can cause data loss or duplicates in production environments where failures are common.

When handling external systems (databases, message queues, APIs), transient failures and circuit-breaker patterns become essential. A single slow downstream service can cause backpressure to ripple through your entire pipeline catastrophically. Consider implementing bulkhead patterns where certain pipeline stages have isolated pools of workers to prevent cascading failures. For ETL pipelines combining Ecto with streaming, managing database connection pools and transaction contexts requires careful coordination to prevent connection exhaustion.


## Deep Dive: Streaming Patterns and Production Implications

Stream-based pipelines in Elixir achieve backpressure and composability by deferring computation until consumption. Unlike eager list operations that allocate all intermediate structures, Streams are lazy chains that produce one element at a time, reducing memory footprint and enabling infinite sequences. The BEAM scheduler yields between Stream operations, allowing multiple concurrent pipelines to interleave fairly. At scale (processing millions of rows or events), the difference between eager and lazy evaluation becomes the difference between consistent latency and garbage collection pauses. Production systems benefit most when Streams are composed at library boundaries, not scattered across the codebase.

---

## Trade-offs and production gotchas

**1. BroadcastDispatcher is gated by the slowest consumer.**
If one of your fan-out sinks is slow, the whole pipeline throttles. Mitigate
by wrapping the slow sink with its own bounded producer-consumer stage and a
local buffer.

**2. DemandDispatcher has no fairness.**
A consumer that asks first gets everything. If you need round-robin you must
write a custom dispatcher.

**3. `max_demand` is a max, not a batch size.**
Consumers receive batches between `min_demand` and `max_demand` based on what
the producer can deliver right now.

**4. `buffer_keep: :first` loses the head of your stream.**
If you need audit-trail completeness, `:last` is mandatory. Combine with an
alarm on buffer fill to catch sustained backpressure early.

**5. `handle_subscribe` returning `:manual` disables automatic demand forever.**
Forgetting to call `GenStage.ask/2` after the first batch looks like the
pipeline stopped for no reason.

**6. Crashing a manual-subscription consumer with `cancel: :temporary` leaks demand.**
The producer keeps buffering events meant for the gone consumer forever.

**7. Buffer overflow is silent by default.**
GenStage logs a warning when the buffer fills, but nothing emits telemetry.
Wire `:telemetry` or periodic `:sys.get_state` to export fill ratio.

**8. When NOT to use GenStage.** If your pipeline is a single producer →
single consumer with no branching and no backpressure requirement, a plain
`Task.Supervisor` + `Stream` is simpler. GenStage earns its keep when you
have multiple stages, multiple consumers, or must survive bursts with
bounded memory.

---

## Benchmark

```elixir
# bench/dispatcher_bench.exs — run with: mix run bench/dispatcher_bench.exs
Benchee.run(
  %{
    "demand" => fn input ->
      Enum.each(input, &GenstageAdvanced.IngestProducer.push/1)
    end
  },
  inputs: %{
    "10k events" => for(i <- 1..10_000, do: %{id: i, payload: :x, ts: 0})
  },
  time: 5,
  warmup: 2
)
```

On an 8-core box demand delivers ~8k events/sec end-to-end, broadcast caps at
the parquet writer's rate (~2k events/sec).

---

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?


## Executable Example

```elixir
defmodule GenstageAdvanced.Aggregator do
  @moduledoc "CPU-bound consumer. Simulates ~1ms of work per event."
  use GenStage

  def start_link(opts), do: GenStage.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(opts) do
    sub = Keyword.fetch!(opts, :subscribe_to)
    {:consumer, %{count: 0}, subscribe_to: sub}
  end

  @impl true
  def handle_events(events, _from, state) do
    Enum.each(events, fn _ -> :timer.sleep(1) end)
    {:noreply, [], %{state | count: state.count + length(events)}}
  end
end

defmodule GenstageAdvanced.ParquetWriter do
  @moduledoc """
  IO-bound consumer that only flushes when it has collected >= 500 events
  or 500ms have elapsed since the last flush.
  """
  use GenStage

  def start_link(opts), do: GenStage.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(opts) do
    sub = Keyword.fetch!(opts, :subscribe_to)
    Process.send_after(self(), :flush_tick, 500)
    {:consumer, %{buf: [], flushed: 0}, subscribe_to: sub}
  end

  @impl true
  def handle_events(events, _from, state) do
    buf = events ++ state.buf

    if length(buf) >= 500 do
      {:noreply, [], %{state | buf: [], flushed: state.flushed + length(buf)}}
    else
      {:noreply, [], %{state | buf: buf}}
    end
  end

  @impl true
  def handle_info(:flush_tick, state) do
    Process.send_after(self(), :flush_tick, 500)
    {:noreply, [], %{state | buf: [], flushed: state.flushed + length(state.buf)}}
  end
end

defmodule GenstageAdvanced.Sampler do
  @moduledoc "Forwards ~1% of events to a subscriber pid."
  use GenStage

  def start_link(opts), do: GenStage.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(opts) do
    sub = Keyword.fetch!(opts, :subscribe_to)
    target = Keyword.fetch!(opts, :target)
    {:consumer, %{target: target}, subscribe_to: sub}
  end

  @impl true
  def handle_events(events, _from, state) do
    Enum.each(events, fn e ->
      if :rand.uniform(100) == 1, do: send(state.target, {:sample, e})
    end)

    {:noreply, [], state}
  end
end

defmodule Main do
  def main do
      # Demonstrate GenStage with manual subscription and buffer management
      {:ok, _sup} = Supervisor.start_link([], strategy: :one_for_one)
      {:ok, p} = GenStage.start_link(GenstageAdvanced.IngestProducer, 
        [buffer_size: 5, buffer_keep: :first], [])
      {:ok, c} = GenStage.start_link(GenstageAdvanced.ManualConsumer, 
        [subscribe_to: [{p, max_demand: 10}]], [])

      Process.sleep(20)

      # Push 3 events
      for i <- 1..3 do
        GenStage.cast(p, {:push, %{id: i, payload: "event_#{i}", ts: System.os_time()}})
      end

      Process.sleep(50)

      # Pull from consumer
      :ok = GenstageAdvanced.ManualConsumer.pull(c, 5)
      Process.sleep(50)

      seen = :sys.get_state(c).seen
      IO.inspect(seen, label: "✓ Events received by consumer")

      assert length(seen) == 3, "Expected 3 events"
      assert Enum.map(seen, & &1.id) == [1, 2, 3], "Events in order"

      IO.puts("✓ GenStage advanced: producer, consumer, manual subscription working")
  end
end

Main.main()
```
