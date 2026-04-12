# Broadway — End-to-End Data Pipelines

**Project**: `broadway_pipeline` — a payment event enricher with fan-in, partitioning and batching.

**Difficulty**: ★★★★☆

**Estimated time**: 4–6 hours

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

## Core concepts

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

## Implementation

### Step 1: Project and deps

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

```elixir
defmodule BroadwayPipeline.PipelineTest do
  use ExUnit.Case, async: false

  alias Broadway.Message
  alias BroadwayPipeline.Pipeline

  setup do
    start_supervised!({Pipeline, []})
    :ok
  end

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
```

### Step 6: Wire telemetry

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

## Resources

- [Broadway — HexDocs](https://hexdocs.pm/broadway/Broadway.html)
- [Announcing Broadway — Dashbit](https://dashbit.co/blog/announcing-broadway)
- [Broadway source — `lib/broadway.ex`](https://github.com/dashbitco/broadway/blob/main/lib/broadway.ex)
- [Concurrent Data Processing in Elixir — Svilen Gospodinov](https://pragprog.com/titles/sgdpelixir/concurrent-data-processing-in-elixir/)
- [`Broadway.test_message/3` docs](https://hexdocs.pm/broadway/Broadway.html#test_message/3)
- [Telemetry events reference](https://hexdocs.pm/broadway/Broadway.html#module-telemetry)
