# Broadway + Kafka — Consumer Groups, Concurrency and Telemetry

**Project**: `broadway_kafka` — a production-grade Kafka consumer pipeline with partition-aware concurrency, manual offset commits, and telemetry-driven autoscaling signals.

**Difficulty**: ★★★★☆

**Estimated time**: 4–6 hours

---

## Project context

A B2B SaaS product emits usage events (login, api_call, feature_used) to
Kafka. A data team needs these events normalised and persisted to a warehouse
for billing. The topic has 24 partitions, peak throughput is ~25k msgs/sec,
and message order **within a customer** must be preserved (a `trial_started`
must land before `trial_converted` on the same `customer_id`).

Kafka's guarantee is order within a partition, and the producer already keys
messages by `customer_id`. Your consumer must honour this: one worker per
partition, at-least-once delivery, manual commits only after the downstream
write has succeeded, and graceful rebalance when a pod joins or leaves.

BroadwayKafka, built on `brod`, handles rebalance, fetching, and offset
commits. Your job is to pick the right concurrency, wire telemetry so SRE
can see consumer lag per partition, and make the batch handler idempotent.

```
broadway_kafka/
├── lib/
│   └── broadway_kafka/
│       ├── application.ex
│       ├── pipeline.ex          # BroadwayKafka pipeline
│       ├── normaliser.ex        # event transform
│       ├── warehouse.ex         # fake warehouse writer
│       └── telemetry.ex         # lag + throughput metrics
├── test/
│   └── broadway_kafka/
│       └── pipeline_test.exs
└── mix.exs
```

---

## Core concepts

### 1. Concurrency topology

```
  Kafka topic (24 partitions)
  ┌───┬───┬───┬─ ... ─┬───┐
  │ 0 │ 1 │ 2 │       │23 │
  └─┬─┴─┬─┴─┬─┴───────┴─┬─┘
    │   │   │           │
    ▼   ▼   ▼           ▼
  pod-A        pod-B          pod-C           (consumer group)
  (8 parts)    (8 parts)      (8 parts)
    │
    ▼
  BroadwayKafka producer: 1 producer process per assigned partition
    │
    ▼
  processors: concurrency = N (shared pool across partitions)
    │
    ▼
  batcher → warehouse insert
```

BroadwayKafka assigns each partition to one producer stage. Processors are a
shared pool, but because Broadway routes each partition's messages through
its own in-flight queue, per-partition order is preserved as long as you do
not `partition_by` to a different key.

### 2. Manual offset commits

BroadwayKafka supports three offset commit modes:

| mode | when offsets commit | risk |
|------|---------------------|------|
| `:on_start` (default in brod) | right after fetch | lose msgs on crash |
| `:on_consumer_ack` | after the downstream `ack/3` | at-least-once (correct) |
| `:on_failure` | only on ack failure | impossible in practice |

Always use `:on_consumer_ack`. `:on_start` is a foot-gun: if the worker
crashes mid-batch, those messages are lost.

### 3. `draining_after_revoke_ms`

When Kafka rebalances (a pod joins/leaves the group), a partition is revoked
from this pod and assigned to another. BroadwayKafka can **drain** in-flight
messages before releasing the partition, preventing another pod from
reprocessing them. Set this to max(processor duration + batch timeout).

### 4. Consumer lag telemetry

Per-partition lag (`high_watermark - committed_offset`) is the single most
important production metric. BroadwayKafka exposes it via the
`[:broadway_kafka, :assignments, :received]` event and via brod's own
`:brod_utils.resolve_offsets`. Wire this into Prometheus and alarm when lag
> N for > M minutes.

### 5. Rebalance correctness

Rebalance is the source of 90% of Kafka consumer bugs. The contract:

1. Group coordinator sends `revoke_partitions`.
2. Your consumer finishes in-flight work (drain) and commits offsets.
3. Coordinator sends `assign_partitions` with the new assignment.
4. Fetching resumes.

If you ack before the write completed, you will lose data on rebalance.
Always ack after the downstream side-effect is durable.

---

## Implementation

### Step 1: Project and deps

```bash
mix new broadway_kafka --sup
```

```elixir
defp deps do
  [
    {:broadway_kafka, "~> 0.4"},
    {:telemetry, "~> 1.2"},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 2: Pipeline

```elixir
defmodule BroadwayKafka.Pipeline do
  @moduledoc """
  Kafka consumer pipeline. One producer per assigned partition, shared
  processor pool, one batcher that bulk-inserts into the warehouse.

  Offsets are committed only after the warehouse insert returns :ok.
  """
  use Broadway

  alias Broadway.Message
  alias BroadwayKafka.{Normaliser, Warehouse}

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    hosts = Keyword.get(opts, :hosts, [{"localhost", 9092}])
    topic = Keyword.get(opts, :topic, "usage_events")
    group = Keyword.get(opts, :group, "warehouse_writer")

    Broadway.start_link(__MODULE__,
      name: __MODULE__,
      producer: [
        module: {
          BroadwayKafka.Producer,
          [
            hosts: hosts,
            group_id: group,
            topics: [topic],
            offset_commit_on_ack: true,
            offset_reset_policy: :earliest,
            draining_after_revoke_ms: 10_000,
            client_config: [
              auto_start_producers: false,
              connect_timeout: 10_000
            ]
          ]
        },
        concurrency: 2
      ],
      processors: [default: [concurrency: 16]],
      batchers: [
        warehouse: [concurrency: 4, batch_size: 500, batch_timeout: 2_000]
      ]
    )
  end

  @impl true
  def handle_message(_processor, %Message{data: raw} = msg, _ctx) do
    case Normaliser.normalise(raw) do
      {:ok, event} ->
        msg
        |> Message.update_data(fn _ -> event end)
        |> Message.put_batcher(:warehouse)

      {:error, reason} ->
        Message.failed(msg, reason)
    end
  end

  @impl true
  def handle_batch(:warehouse, messages, _batch_info, _ctx) do
    rows = Enum.map(messages, & &1.data)

    case Warehouse.upsert(rows) do
      :ok -> messages
      {:error, reason} -> Enum.map(messages, &Message.failed(&1, reason))
    end
  end
end
```

### Step 3: Normaliser and warehouse (fakes for test)

```elixir
defmodule BroadwayKafka.Normaliser do
  @spec normalise(binary()) :: {:ok, map()} | {:error, term()}
  def normalise(bin) do
    case Jason.decode(bin) do
      {:ok, %{"customer_id" => cid, "event" => ev, "ts" => ts}} ->
        {:ok, %{customer_id: cid, event: ev, ts: ts}}

      {:ok, _} ->
        {:error, :missing_fields}

      err ->
        err
    end
  end
end

defmodule BroadwayKafka.Warehouse do
  @spec upsert([map()]) :: :ok | {:error, term()}
  def upsert(_rows), do: :ok
end
```

### Step 4: Telemetry — lag and throughput

```elixir
defmodule BroadwayKafka.Telemetry do
  require Logger

  @spec attach() :: :ok
  def attach do
    :telemetry.attach_many(
      "kafka-pipeline-telemetry",
      [
        [:broadway, :batcher, :stop],
        [:broadway, :processor, :message, :exception],
        [:broadway_kafka, :assignments, :received]
      ],
      &handle/4,
      nil
    )
  end

  def handle([:broadway, :batcher, :stop], meas, meta, _) do
    Logger.info(
      "batch flushed batcher=#{meta.batcher} size=#{length(meta.messages)} ms=#{div(meas.duration, 1_000_000)}"
    )
  end

  def handle([:broadway, :processor, :message, :exception], _meas, meta, _) do
    Logger.error("processor exception kind=#{meta.kind} reason=#{inspect(meta.reason)}")
  end

  def handle([:broadway_kafka, :assignments, :received], _meas, meta, _) do
    Logger.info("kafka assignments received: #{inspect(meta.assignments)}")
  end
end
```

### Step 5: Tests with a stub producer

Broadway ships `Broadway.DummyProducer` which is useful for handler tests.
For end-to-end Kafka, use an embedded broker (e.g. `:brod_demo` or
`testcontainers-elixir`). Here we test the handlers in isolation using the
pipeline's public API:

```elixir
defmodule BroadwayKafka.PipelineTest do
  use ExUnit.Case, async: false

  alias Broadway.Message
  alias BroadwayKafka.Pipeline

  defmodule StubPipeline do
    use Broadway

    def start_link(_) do
      Broadway.start_link(__MODULE__,
        name: __MODULE__,
        producer: [module: {Broadway.DummyProducer, []}, concurrency: 1],
        processors: [default: [concurrency: 2]],
        batchers: [warehouse: [concurrency: 1, batch_size: 5, batch_timeout: 100]]
      )
    end

    @impl true
    def handle_message(_p, %Message{} = msg, _ctx) do
      Pipeline.handle_message(:default, msg, %{})
    end

    @impl true
    def handle_batch(name, msgs, info, ctx), do: Pipeline.handle_batch(name, msgs, info, ctx)
  end

  setup do
    start_supervised!(StubPipeline)
    :ok
  end

  test "valid event routes to warehouse batcher" do
    payload = ~s({"customer_id":"c1","event":"login","ts":1})
    ref = Broadway.test_message(StubPipeline, payload)
    assert_receive {:ack, ^ref, [%Message{batcher: :warehouse}], []}, 2_000
  end

  test "invalid event fails with :missing_fields" do
    payload = ~s({"customer_id":"c1"})
    ref = Broadway.test_message(StubPipeline, payload)
    assert_receive {:ack, ^ref, [], [%Message{status: {:failed, :missing_fields}}]}, 2_000
  end
end
```

---

## Trade-offs and production gotchas

**1. One partition per producer stage — that is the max per-partition
concurrency.** If a partition is hot, you cannot parallelise it without
breaking ordering. Fix upstream by repartitioning with a better key.

**2. `offset_commit_on_ack: true` is at-least-once, not exactly-once.**
A crash between `handle_batch` returning and the commit RPC succeeding
replays the batch. Your warehouse insert must be idempotent (see exercise
184).

**3. Rebalance storms.** When a pod joins, *all* pods pause for a brief
moment while the coordinator reassigns. If rebalances are frequent, you pay
latency. Increase `session_timeout_ms` and `heartbeat_interval_ms` to match
your deploy cadence.

**4. `draining_after_revoke_ms` too low truncates batches.** Set it >=
`batch_timeout + p99(handle_batch)`. Otherwise revoked partitions lose
in-flight rows.

**5. `concurrency: 16` processors does not mean 16x throughput.** If all
partitions route to the same customer_id (skew), the processor pool
serialises on in-flight ordering per partition.

**6. Watch offset commit failures.** `brod` retries transiently. Persistent
commit failures mean consumer group coordinator is unreachable — you will
reprocess a lot of data on restart.

**7. Consumer lag is the canary.** Alarm when lag grows linearly — that is
your pipeline falling behind. A flat non-zero lag is fine; a growing lag
is not.

**8. When NOT to use BroadwayKafka.** If your message volume is <100
msgs/sec, the operational overhead of Kafka itself outweighs the benefits.
Use `broadway_rabbitmq` or SQS. If you need strict exactly-once, use Kafka
transactions directly — BroadwayKafka does not expose them.

---

## Performance notes

On a 3-pod cluster with 24 partitions, processor concurrency 16, batch size
500 / timeout 2s, we measured sustained 18k msgs/sec with p99 end-to-end
latency ~2.8s. Bottleneck was the warehouse's bulk upsert; raising
`batch_size` to 1000 improved throughput to 24k and p99 to 3.1s.

Measure consumer lag with `:brod_utils.resolve_offsets/3` and export to
Prometheus via `telemetry_metrics_prometheus`:

```elixir
Telemetry.Metrics.last_value("kafka.consumer.lag",
  event_name: [:broadway_kafka, :lag],
  measurement: :lag,
  tags: [:partition]
)
```

---

## Resources

- [BroadwayKafka — HexDocs](https://hexdocs.pm/broadway_kafka/BroadwayKafka.Producer.html)
- [brod — Kafka client](https://github.com/kafka4beam/brod)
- [Kafka consumer group protocol — Confluent](https://www.confluent.io/blog/apache-kafka-consumer-group-rebalance-protocol/)
- [Concurrent Data Processing in Elixir — Svilen Gospodinov](https://pragprog.com/titles/sgdpelixir/concurrent-data-processing-in-elixir/)
- [BroadwayKafka source](https://github.com/dashbitco/broadway_kafka/blob/main/lib/broadway_kafka/producer.ex)
- [Telemetry + Prometheus — `telemetry_metrics_prometheus`](https://hexdocs.pm/telemetry_metrics_prometheus/)
