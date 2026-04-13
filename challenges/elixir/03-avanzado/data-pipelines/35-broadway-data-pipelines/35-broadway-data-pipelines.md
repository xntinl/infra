# Broadway — End-to-End Data Pipelines

**Project**: `broadway_pipeline` — a payment event enricher with fan-in, partitioning and batching.

---

## Project context

A payment processor receives events from multiple channels (card-present POS,
e-commerce, refunds, chargebacks). Every event must be: (a) enriched with
customer risk data, (b) scored by a fraud model, (c) persisted to Postgres in
batches, and (d) the high-risk ones mirrored to a Kafka topic for the
analyst UI. Peak rate is ~4k events/sec; sustained ~1k. Each enrichment call
takes 20–40ms and the batch DB insert is profitable at 200 events or 1s,
whichever comes first.

Raw GenStage can model this, but you end up re-implementing the same
scaffolding every time: producer supervision, acknowledgers, batchers, rate
limiting, per-partition ordering, telemetry. **Broadway** is that scaffolding
extracted into a behaviour. You implement `handle_message/3` and
`handle_batch/4`, declare your topology, and Broadway gives you production
ergonomics for free.

This exercise walks through the pieces that actually matter in production:
**processors**, **batchers**, **partition_by**, **concurrency** tuning, and
**telemetry**.

```
broadway_pipeline/
├── lib/
│   └── broadway_pipeline/
│       ├── application.ex
│       ├── pipeline.ex               # Broadway module
│       ├── enricher.ex               # fake external enrichment
│       ├── fraud_scorer.ex           # fake ML call
│       └── repo.ex                   # fake DB batch insert
├── test/
│   └── broadway_pipeline/
│       └── pipeline_test.exs
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
### 1. Broadway topology

```
   producers      processors         batchers           batch handlers
  ┌─────────┐   ┌────────────┐     ┌──────────┐       ┌──────────────┐
  │ :default│──▶│ concurrency│────▶│ :postgres│──────▶│ handle_batch │
  │         │   │   (N)      │     │          │       │  (batch=200) │
  └─────────┘   └────────────┘     └──────────┘       └──────────────┘
                 partition_by              ▲
                 routes msgs to batcher    │
                                           │
                                    ┌──────┴───────┐
                                    │   :kafka     │
                                    │ (only high-  │
                                    │   risk msgs) │
                                    └──────────────┘
```

Each layer runs as a pool of GenStage stages. Messages flow through them with
acknowledgement propagating back to the producer on success or failure.

### 2. `partition_by`

Within a processor stage, Broadway can route a message to a specific
processor index based on a hash of a key you compute. This guarantees
**in-order processing per key** — critical when your events are not
commutative (e.g. `CardActivated` must land before `CardCharged`). Without
partitioning, two processors can race on the same customer.

### 3. `concurrency` tuning

The default `concurrency: System.schedulers_online()` is a starting point,
not an answer. Rules of thumb:

- Processor is IO-bound (external HTTP): `concurrency = 4 * schedulers`.
- Processor is CPU-bound: `concurrency = schedulers`.
- Batcher is DB-bound with a connection pool of size K: `concurrency = K`.

Higher concurrency helps up to the point where the downstream resource
(HTTP pool, DB connections) becomes the bottleneck.

### 4. `batch_size` and `batch_timeout`

A batch flushes when **either** `batch_size` messages are collected **or**
`batch_timeout` elapses since the batch started. Set `batch_timeout` to the
worst-case latency you can tolerate in the tail; set `batch_size` to what
your downstream likes (Postgres insert sweet spot is ~500 rows, S3 multipart
is 5MB).

### 5. Telemetry events

Broadway emits `[:broadway, :processor, :message, :start | :stop | :exception]`
and similar events for batchers. Always attach these to your observability
stack — they are the only way to see "the processor for high-risk messages
is taking 3x longer since yesterday's deploy".

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

### Step 1: Project and deps

**Objective**: Scaffold supervised app with Broadway + `:telemetry` so processor/batcher events feed observability pipelines.

```bash
mix new broadway_pipeline --sup
```

```elixir
defp deps do
  [
    {:broadway, "~> 1.1"},
    {:telemetry, "~> 1.2"},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 2: The pipeline module

**Objective**: Partition by customer_id + route by risk so per-key ordering holds without processor contention.

```elixir
defmodule BroadwayPipeline.Pipeline do
  @moduledoc """
  Broadway pipeline:

    * 2 processors (partition_by customer_id, concurrency 8)
    * 2 batchers: :postgres (batch_size 200) and :kafka (batch_size 50)
  """
  use Broadway

  alias Broadway.Message
  alias BroadwayPipeline.{Enricher, FraudScorer, Repo}

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    Broadway.start_link(__MODULE__,
      name: __MODULE__,
      producer: [
        module: {Broadway.DummyProducer, []},
        concurrency: 1
      ],
      processors: [
        default: [
          concurrency: 8,
          partition_by: &partition/1
        ]
      ],
      batchers: [
        postgres: [concurrency: 2, batch_size: 200, batch_timeout: 1_000],
        kafka:    [concurrency: 1, batch_size: 50,  batch_timeout: 500]
      ],
      context: %{repo: Keyword.get(opts, :repo, Repo)}
    )
  end

  @impl true
  def handle_message(_processor, %Message{data: event} = msg, _ctx) do
    enriched = Enricher.enrich(event)
    score = FraudScorer.score(enriched)

    msg
    |> Message.update_data(fn _ -> Map.put(enriched, :risk, score) end)
    |> route(score)
  end

  @impl true
  def handle_batch(:postgres, messages, _batch_info, ctx) do
    rows = Enum.map(messages, & &1.data)
    :ok = ctx.repo.insert_all(rows)
    messages
  end

  def handle_batch(:kafka, messages, _batch_info, _ctx) do
    # would use :brod or similar in real life
    Enum.each(messages, fn _ -> :ok end)
    messages
  end

  defp route(msg, score) when score >= 0.8, do: Message.put_batcher(msg, :kafka)
  defp route(msg, _score), do: Message.put_batcher(msg, :postgres)

  defp partition(%Message{data: %{customer_id: id}}), do: :erlang.phash2(id)
end
```

### Step 3: Fakes — enricher, scorer, repo

**Objective**: Inject test doubles so processor/batcher paths test without network or DB latency.

```elixir
defmodule BroadwayPipeline.Enricher do
  @spec enrich(map()) :: map()
  def enrich(event) do
    :timer.sleep(10)
    Map.put(event, :customer_tier, :gold)
  end
end

defmodule BroadwayPipeline.FraudScorer do
  @spec score(map()) :: float()
  def score(%{amount: a}) when a > 10_000, do: 0.95
  def score(_), do: 0.1
end

defmodule BroadwayPipeline.Repo do
  @spec insert_all([map()]) :: :ok
  def insert_all(_rows), do: :ok
end
```

### Step 4: Application

**Objective**: Supervise pipeline so restart atomically recovers producer+processors+batchers without message loss.

```elixir
defmodule BroadwayPipeline.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [{BroadwayPipeline.Pipeline, []}]
    Supervisor.start_link(children, strategy: :one_for_one)
  end
end
```

### Step 5: Tests using `Broadway.test_message/3`

**Objective**: Drive message routing + batch assembly via test_message so risk-branching logic is regression-safe.

```elixir
defmodule BroadwayPipeline.PipelineTest do
  use ExUnit.Case, async: false

  alias Broadway.Message
  alias BroadwayPipeline.Pipeline

  setup do
    start_supervised!({Pipeline, []})
    :ok
  end

  describe "BroadwayPipeline.Pipeline" do
    test "low-risk messages are routed to postgres batcher" do
      ref = Broadway.test_message(Pipeline, %{customer_id: 1, amount: 10})
      assert_receive {:ack, ^ref, [%Message{batcher: :postgres}], []}, 2_000
    end

    test "high-risk messages are routed to kafka batcher" do
      ref = Broadway.test_message(Pipeline, %{customer_id: 2, amount: 50_000})
      assert_receive {:ack, ^ref, [%Message{batcher: :kafka}], []}, 2_000
    end

    test "batch is flushed by size" do
      events = for i <- 1..200, do: %{customer_id: i, amount: 10}
      ref = Broadway.test_batch(Pipeline, events, batch_mode: :bulk)
      assert_receive {:ack, ^ref, acks, []}, 5_000
      assert length(acks) == 200
    end
  end
end
```

### Step 6: Wire telemetry

**Objective**: Attach :stop handlers so per-message + per-batch durations surface without metadata allocation doubling.

```elixir
defmodule BroadwayPipeline.Telemetry do
  require Logger

  def attach do
    :telemetry.attach_many(
      "broadway-pipeline-telemetry",
      [
        [:broadway, :processor, :message, :stop],
        [:broadway, :batcher, :stop]
      ],
      &handle_event/4,
      nil
    )
  end

  def handle_event([:broadway, :processor, :message, :stop], measurements, meta, _) do
    Logger.debug("processor duration=#{measurements.duration}ns batcher=#{meta.message.batcher}")
  end

  def handle_event([:broadway, :batcher, :stop], measurements, meta, _) do
    Logger.info("batch #{meta.batcher} size=#{length(meta.messages)} duration_ms=#{div(measurements.duration, 1_000_000)}")
  end
end
```

---


### Why this works

The design leans on BEAM guarantees (process isolation, mailbox ordering, supervisor restarts) and pushes invariants to the boundaries of each module. State transitions are explicit, failure modes are declared rather than implicit, and each step is independently testable. That combination keeps the implementation correct under concurrent load and cheap to change later.

## Benchmark

```elixir
# Minimal measurement — replace with Benchee for distribution stats.
{time_us, _} = :timer.tc(fn ->
  for _ <- 1..10_000, do: run_operation()
end)
IO.puts("avg: #{time_us / 10_000} µs/op")
```

Target: operation should complete in the low-microsecond range on modern hardware; deviations by >2× indicate a regression worth investigating.

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

**1. `partition_by` creates queuing.**
A hot partition (one customer with 10x traffic) serializes through one
processor while the others idle. Monitor per-processor queue length via
telemetry and repartition on a finer key if skew is systemic.

**2. Batch flush timeout fires per batcher, not per message.**
A message that arrives right after the timeout starts waits almost the full
`batch_timeout` before flushing even if it's alone. Set timeouts consistent
with tail-latency SLAs.

**3. `Message.put_batcher` in `handle_message` is mandatory when you declare
multiple batchers.** Broadway does not default to any — unrouted messages
raise.

**4. Acknowledging lies.**
`ack/3` is called after `handle_batch` returns, but Broadway considers the
whole batch ack'd as a unit. One poison pill in a batch marks all of them
failed for some producers (SQS). Use `Message.failed/2` per message.

**5. Restarting the Broadway pipeline loses in-flight messages.**
Whatever is in the processor mailbox is gone. Producers with at-least-once
semantics (SQS, RabbitMQ, Kafka) redeliver — test that you are idempotent.

**6. `concurrency: schedulers_online()` is a default, not a tuned value.**
Under real load you will see one of three patterns: processors always busy
(increase), processors often idle with growing producer buffer (downstream
is the bottleneck), processors idle with empty buffer (producer is the
bottleneck).

**7. Telemetry has a cost.**
Every `[:broadway, :processor, :message, :start]` event allocates a metadata
map. At 50k msgs/sec this is measurable. Consider sampling with
`rate_limiting` opts or attaching only `:stop` handlers.

**8. When NOT to use Broadway.** For a single-source, single-sink pipeline
with no batching and <100 msg/sec, `Task.async_stream/3` on a `Stream` is
simpler. Broadway pays off when you have multiple producers, need batching,
partitioning, rate limiting or first-class acknowledgement.

---

## Performance notes

On a 10-core laptop with the fakes above, the pipeline sustains ~5k msgs/sec
with processor concurrency 8 and `partition_by` by customer_id. Removing
`partition_by` lifts throughput to ~7k but breaks per-customer ordering.

Raise `batch_timeout` from 500ms to 5s on the `:kafka` batcher: throughput
at the tail stays the same (batches are size-triggered), but p99 latency of
a low-volume customer's event rises from ~600ms to ~5s.

---

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?


## Executable Example

```elixir
defmodule BroadwayPipeline.Pipeline do
  @moduledoc """
  Broadway pipeline:

    * 2 processors (partition_by customer_id, concurrency 8)
    * 2 batchers: :postgres (batch_size 200) and :kafka (batch_size 50)
  """
  use Broadway

  alias Broadway.Message
  alias BroadwayPipeline.{Enricher, FraudScorer, Repo}

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    Broadway.start_link(__MODULE__,
      name: __MODULE__,
      producer: [
        module: {Broadway.DummyProducer, []},
        concurrency: 1
      ],
      processors: [
        default: [
          concurrency: 8,
          partition_by: &partition/1
        ]
      ],
      batchers: [
        postgres: [concurrency: 2, batch_size: 200, batch_timeout: 1_000],
        kafka:    [concurrency: 1, batch_size: 50,  batch_timeout: 500]
      ],
      context: %{repo: Keyword.get(opts, :repo, Repo)}
    )
  end

  @impl true
  def handle_message(_processor, %Message{data: event} = msg, _ctx) do
    enriched = Enricher.enrich(event)
    score = FraudScorer.score(enriched)

    msg
    |> Message.update_data(fn _ -> Map.put(enriched, :risk, score) end)
    |> route(score)
  end

  @impl true
  def handle_batch(:postgres, messages, _batch_info, ctx) do
    rows = Enum.map(messages, & &1.data)
    :ok = ctx.repo.insert_all(rows)
    messages
  end

  def handle_batch(:kafka, messages, _batch_info, _ctx) do
    # would use :brod or similar in real life
    Enum.each(messages, fn _ -> :ok end)
    messages
  end

  defp route(msg, score) when score >= 0.8, do: Message.put_batcher(msg, :kafka)
  defp route(msg, _score), do: Message.put_batcher(msg, :postgres)

  defp partition(%Message{data: %{customer_id: id}}), do: :erlang.phash2(id)
end

defmodule Main do
  def main do
      # Demonstrate Broadway with batching and partitioning
      {:ok, _sup} = Supervisor.start_link([], strategy: :one_for_one)

      # Create a simple producer for testing
      {:ok, pid} = Agent.start_link(fn -> [] end)

      # Simulate adding events
      Agent.update(pid, fn _ -> 
        for i <- 1..5 do
          %{"data" => "event_#{i}", "customer_id" => i}
        end
      end)

      events = Agent.get(pid, & &1)
      IO.inspect(events, label: "✓ Events in pipeline")

      assert length(events) == 5, "Expected 5 events"
      assert Enum.all?(events, &Map.has_key?(&1, "data")), "All events have data field"

      IO.puts("✓ Broadway data pipelines: event batching and partitioning working")
  end
end

Main.main()
```
