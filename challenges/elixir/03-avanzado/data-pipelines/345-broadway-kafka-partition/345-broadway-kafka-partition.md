# Broadway with Kafka — `partition_by` for Per-Key Ordering

**Project**: `user_events_consumer` — consumes `user-events` Kafka topic and processes events per `user_id` in arrival order while still using all cores in parallel.

## Project context

You consume a Kafka topic `user-events` with 24 partitions, producing ~30k
events/sec. Downstream must process events for a given user in strict
arrival order (e.g. `user.login` must be applied before `user.profile_updated`)
but events for different users are independent.

Kafka already guarantees per-partition ordering. The producer hashes `user_id
→ partition`, so all events for user 42 land on the same partition. On the
consumer side, Broadway's `BroadwayKafka.Producer` assigns partitions to
processor stages. If you use `partition_by:` with the same hash as the
producer, you preserve ordering end-to-end even across processors within a
partition (edge case: concurrent processors reading the same partition).

```
user_events_consumer/
├── lib/
│   └── user_events_consumer/
│       ├── application.ex
│       ├── pipeline.ex
│       └── processor.ex
├── test/
│   └── user_events_consumer/
│       └── pipeline_test.exs
├── bench/
│   └── throughput_bench.exs
└── mix.exs
```

## Why BroadwayKafka and not brod directly

`brod` is the Erlang Kafka client. It works, but:

- Offset commit is manual.
- No built-in concurrency model — you plumb processes yourself.
- No back-pressure.
- No ack/fail semantics tied to offset commits.

`BroadwayKafka` wraps brod:

- Subscribes to consumer group, handles partition rebalance.
- Translates messages to `Broadway.Message` struct with offset tracking.
- Commits offsets only for successfully acked messages — at-least-once with
  no manual bookkeeping.

Alternatives we rejected:

- **brod directly**: acceptable only for trivial single-partition consumers.
- **Kafka Connect sink to PostgreSQL + Oban**: heavy ops surface.
- **Debezium + another Broadway**: double-hop with no gain for this workload.

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
### 1. Partition-aware concurrency

```
Kafka topic (24 partitions)
        │
        ▼
BroadwayKafka.Producer (fetches from assigned partitions)
        │
        ▼
Processors (concurrency: N, partition_by: user_id → stage)
```

If `concurrency: 8, partition_by: &(&1.user_id |> :erlang.phash2(8))`, events
for the same `user_id` always go to the same processor stage. Per-user
ordering is preserved even when Kafka puts multiple partitions on the same
node (rare but possible during rebalance).

### 2. Offset semantics

Broadway commits offsets in batches after successful ack. If a message fails,
its offset is NOT committed — on restart, Kafka re-delivers from the last
committed offset (at-least-once). You never manually call commit.

### 3. Rebalance safety

When a Kafka consumer joins or leaves the group, partitions are reassigned.
`BroadwayKafka` pauses the producer, drains in-flight messages, commits
offsets, then resumes on the new assignment. Your code doesn't need to be
aware of rebalance events for correctness — but long handle_message/batch
times can cause rebalance-driven duplicate delivery.

## Design decisions

- **Option A — No `partition_by`, rely on Kafka partition**:
  - Pros: simplest. Kafka already gives you per-partition order.
  - Cons: within a processor stage handling messages from multiple partitions,
    order across partitions is up to processing interleaving.
- **Option B — `partition_by` on the consumer using the same key as the producer**:
  - Pros: preserves ordering per key through the whole Elixir pipeline.
  - Cons: some processors may be underutilised if key distribution is skewed.
- **Option C — Single processor (concurrency: 1)**:
  - Pros: strict global order.
  - Cons: one core utilised. Throughput collapses.

We pick **Option B** — it matches the semantics our business needs
(per-user order) while using all cores.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule UserEventsConsumer.MixProject do
  use Mix.Project

  def project do
    [
      app: :user_events_consumer,
      version: "0.1.0",
      elixir: "~> 1.16",
      deps: deps()
    ]
  end

  def application do
    [mod: {UserEventsConsumer.Application, []}, extra_applications: [:logger]]
  end

  defp deps do
    [
      {:broadway, "~> 1.1"},
      {:broadway_kafka, "~> 0.4"},
      {:jason, "~> 1.4"},
      {:benchee, "~> 1.3", only: :dev}
    ]
  end
end
```

### Step 1: Pipeline

**Objective**: Hash `user_id` via `partition_by` so events for the same user serialise on one processor while peers parallelise freely.

```elixir
defmodule UserEventsConsumer.Pipeline do
  use Broadway

  alias Broadway.Message

  @stages 8

  def start_link(_opts) do
    Broadway.start_link(__MODULE__,
      name: __MODULE__,
      producer: [
        module:
          {BroadwayKafka.Producer,
           hosts: [localhost: 9092],
           group_id: "user_events_consumer",
           topics: ["user-events"],
           offset_commit_interval_seconds: 5,
           client_config: [connect_timeout: 30_000]},
        concurrency: 1
      ],
      processors: [
        default: [
          concurrency: @stages,
          max_demand: 100,
          partition_by: &partition_by_user/1
        ]
      ],
      batchers: [
        default: [concurrency: 2, batch_size: 200, batch_timeout: 500]
      ]
    )
  end

  @impl true
  def handle_message(_processor, %Message{data: data} = message, _ctx) do
    case Jason.decode(data) do
      {:ok, %{"user_id" => uid} = event} ->
        UserEventsConsumer.Processor.apply_event(uid, event)
        message

      {:ok, _} ->
        Message.failed(message, "missing user_id")

      {:error, _} ->
        Message.failed(message, "invalid json") |> Message.configure_ack(on_failure: :reject)
    end
  end

  @impl true
  def handle_batch(:default, messages, _info, _ctx), do: messages

  # ---- routing ---------------------------------------------------------

  # Keep the hash stable across releases — it determines per-user order.
  defp partition_by_user(%Message{data: data}) do
    case Jason.decode(data) do
      {:ok, %{"user_id" => uid}} -> :erlang.phash2(uid, @stages)
      _ -> 0
    end
  end
end
```

### Step 2: Per-user processor

**Objective**: Apply events without locks — `partition_by` guarantees single-writer per `user_id`, so no CAS or mutex is needed.

```elixir
defmodule UserEventsConsumer.Processor do
  @moduledoc """
  Applies an event to the read model. Replace with real repo writes.

  Because Broadway routes events for the same user_id to the same processor
  stage, two concurrent calls to apply_event/2 for the same user cannot
  happen — we do not need a lock or CAS here.
  """

  def apply_event(user_id, event) do
    :telemetry.execute(
      [:user_events, :applied],
      %{count: 1},
      %{user_id: user_id, type: event["type"]}
    )

    :ok
  end
end
```

## Why this works

- Kafka partitions `user-events` by `user_id` at the producer. All events
  for user 42 land on the same partition.
- BroadwayKafka assigns partitions to the single producer stage. Messages
  flow to processors.
- `partition_by: &partition_by_user/1` hashes `user_id` to a processor index.
  Same hash function, same stage: per-user events are serialised.
- Offsets commit every 5 seconds or at batch end. On restart the consumer
  resumes from the last committed offset — at-least-once.

## Tests

```elixir
defmodule UserEventsConsumer.PipelineTest do
  use ExUnit.Case, async: false

  alias UserEventsConsumer.Pipeline

  describe "processing" do
    test "applies a valid event and acks" do
      msg = ~s({"user_id":"u42","type":"login"})
      ref = Broadway.test_message(Pipeline, msg)
      assert_receive {:ack, ^ref, [%Broadway.Message{}], []}, 2_000
    end

    test "fails messages without user_id" do
      msg = ~s({"type":"orphan"})
      ref = Broadway.test_message(Pipeline, msg)
      assert_receive {:ack, ^ref, [], [%Broadway.Message{status: {:failed, _}}]}, 2_000
    end
  end

  describe "partition_by ordering" do
    test "events for the same user_id hit the same processor index" do
      events =
        for i <- 1..200 do
          ~s({"user_id":"u42","type":"e#{i}"})
        end

      # Snapshot which processor PID handles each message.
      # With partition_by, all should route to the same stage.
      :telemetry.attach(
        "test-stage",
        [:user_events, :applied],
        fn _e, _m, meta, parent -> send(parent, {:applied, meta.user_id, self()}) end,
        self()
      )

      ref = Broadway.test_batch(Pipeline, events)
      assert_receive {:ack, ^ref, _ok, _fail}, 5_000

      :telemetry.detach("test-stage")

      pids =
        for _ <- 1..200 do
          receive do
            {:applied, "u42", pid} -> pid
          after
            100 -> nil
          end
        end
        |> Enum.reject(&is_nil/1)
        |> Enum.uniq()

      assert length(pids) == 1, "events for u42 were split across #{length(pids)} processors"
    end
  end
end
```

## Benchmark

```elixir
# bench/throughput_bench.exs
# Requires a local Kafka broker on localhost:9092.
# Pre-create topic: bin/kafka-topics.sh --create --topic user-events --partitions 24 ...

# This benchmark uses the Broadway test harness with batches of synthetic events.
events =
  for i <- 1..50_000 do
    Jason.encode!(%{user_id: "u#{rem(i, 1_000)}", type: "e#{i}"})
  end

Benchee.run(%{
  "50k events" => fn ->
    ref = Broadway.test_batch(UserEventsConsumer.Pipeline, events)
    receive do
      {:ack, ^ref, _ok, _fail} -> :ok
    after 60_000 -> flunk("timeout") end
  end
}, time: 10, warmup: 3)
```

**Target**: 20k–40k events/sec on an 8-core host with the stub processor.
Real throughput is bounded by downstream writes.

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

**1. `partition_by` must use the SAME hash as the Kafka producer.**
If the Elixir consumer uses `:erlang.phash2` but the producer (Java,
Go, Python) uses Kafka's default MurmurHash, messages for the same key
will land on different processor stages (and lose per-key order) unless
you override. Either align hash functions or keep the consumer side
partitioning as a lookup only, trusting Kafka's per-partition order.

**2. Re-balancing under load = duplicate delivery.**
When a consumer joins or leaves the group, Kafka rebalances partitions.
In-flight messages whose offsets are not yet committed will be redelivered
to a different consumer. Design for idempotency (unique keys, upserts).

**3. Slow handle_message triggers session timeout.**
Default session timeout is 30s. If handle_message takes >30s the broker
considers the consumer dead, rebalances, and the message gets redelivered.
Monitor `:telemetry.event_duration` and keep handle_message fast; move slow
work to an async queue if needed.

**4. Per-user lag can hide behind average lag.**
If user 42 sends 1k events/s and other users send 1/s, measuring average
partition lag tells you nothing about user 42. Expose per-partition lag
(Kafka's `consumer_lag_max` metric) to catch skewed hot keys.

**5. Offset commit latency vs at-least-once window.**
With `offset_commit_interval_seconds: 5` the worst-case re-delivery window
is 5s of processed-but-not-committed messages. Tighten to 1s if your
downstream is expensive to re-run, but pay more Kafka API calls.

**6. When NOT to use BroadwayKafka.**
If your workload is request/response, use HTTP. If your workload is
bounded file processing, use Flow. Kafka shines for streaming with
replay, high throughput, and strict per-key ordering.

## Reflection

You deploy `partition_by: &:erlang.phash2(&1.user_id, 8)` with
`concurrency: 8`. A week later you need to scale to 16 processors. You
bump `concurrency: 16` but forget to update the hash mod. What breaks,
and what's the minimal-downtime migration path to 16 stages while
preserving per-user ordering?


## Executable Example

```elixir
defmodule UserEventsConsumer.Pipeline do
  use Broadway

  alias Broadway.Message

  @stages 8

  def start_link(_opts) do
    Broadway.start_link(__MODULE__,
      name: __MODULE__,
      producer: [
        module:
          {BroadwayKafka.Producer,
           hosts: [localhost: 9092],
           group_id: "user_events_consumer",
           topics: ["user-events"],
           offset_commit_interval_seconds: 5,
           client_config: [connect_timeout: 30_000]},
        concurrency: 1
      ],
      processors: [
        default: [
          concurrency: @stages,
          max_demand: 100,
          partition_by: &partition_by_user/1
        ]
      ],
      batchers: [
        default: [concurrency: 2, batch_size: 200, batch_timeout: 500]
      ]
    )
  end

  @impl true
  def handle_message(_processor, %Message{data: data} = message, _ctx) do
    case Jason.decode(data) do
      {:ok, %{"user_id" => uid} = event} ->
        UserEventsConsumer.Processor.apply_event(uid, event)
        message

      {:ok, _} ->
        Message.failed(message, "missing user_id")

      {:error, _} ->
        Message.failed(message, "invalid json") |> Message.configure_ack(on_failure: :reject)
    end
  end

  @impl true
  def handle_batch(:default, messages, _info, _ctx), do: messages

  # ---- routing ---------------------------------------------------------

  # Keep the hash stable across releases — it determines per-user order.
  defp partition_by_user(%Message{data: data}) do
    case Jason.decode(data) do
      {:ok, %{"user_id" => uid}} -> :erlang.phash2(uid, @stages)
      _ -> 0
    end
  end
end

defmodule Main do
  def main do
      # Simulate Kafka partition_by for per-user-id ordering
      events = [
        %{id: "e1", user_id: "u1", action: "view"},
        %{id: "e2", user_id: "u2", action: "buy"},
        %{id: "e3", user_id: "u1", action: "review"},
        %{id: "e4", user_id: "u3", action: "follow"}
      ]

      # Partition by user_id using hash
      partitioned = Enum.group_by(events, & &1.user_id)

      IO.inspect(Map.keys(partitioned), label: "✓ Partitions created")

      # Verify ordering per partition
      u1_events = partitioned["u1"]

      IO.inspect(u1_events, label: "✓ u1 events (in order)")

      assert length(partitioned) == 3, "3 partitions created"
      assert Enum.all?(u1_events, &(&1.user_id == "u1")), "All u1 events in partition"

      IO.puts("✓ Kafka partition_by: per-key ordering with parallel processing")
  end
end

Main.main()
```
