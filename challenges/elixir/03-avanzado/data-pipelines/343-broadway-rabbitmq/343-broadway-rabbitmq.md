# Broadway with RabbitMQ — At-Least-Once Processing with Acks

**Project**: `order_processor` — consumes `orders.created` messages from RabbitMQ, validates and persists each order, and acknowledges only on success.

## Project context

You run an e-commerce backend. Orders flow through a RabbitMQ exchange at
2k–10k msg/s. Each message must be validated, enriched, and inserted into
the warehouse queue. If processing fails (DB timeout, downstream outage),
the message must be re-delivered — no silent drops, no duplicate inserts.

`Broadway` is the right layer for this: it wraps GenStage with explicit
ack semantics, batching, retries, and partition-aware processing. The
`BroadwayRabbitMQ.Producer` handles AMQP 0.9.1 prefetch, delivery tags,
and reconnect logic so the application code only deals with business logic.

```
order_processor/
├── lib/
│   └── order_processor/
│       ├── application.ex
│       ├── pipeline.ex            # Broadway definition
│       ├── validator.ex
│       └── repo.ex                # stubbed Ecto-like repo
├── test/
│   └── order_processor/
│       └── pipeline_test.exs      # uses Broadway.test_message/3
├── bench/
│   └── throughput_bench.exs
└── mix.exs
```

## Why Broadway and not a raw AMQP consumer

`AMQP` (the library) gives you `Basic.consume` and raw message handling. You can
write a consumer that processes one message at a time and acks. Works for low
volume. Falls apart at 5k msg/s because:

- Single-process throughput is limited by message round-trip.
- No batching → one `INSERT` per message → DB is the bottleneck.
- Reconnect logic, backoff, prefetch tuning all hand-rolled.
- No back-pressure → mailbox grows if the DB is slow.

Alternatives:

- **`AMQP` directly + worker pool**: you re-implement Broadway's batcher and
  ack bookkeeping. Error-prone.
- **`GenStage` with a custom producer**: Broadway's `BroadwayRabbitMQ.Producer`
  already is a GenStage producer that speaks AMQP. Reinventing it is waste.
- **`Oban` / `Exq`**: these are job queues backed by PostgreSQL / Redis, not
  message brokers. Different use case — if you need cross-service messaging
  with pub/sub semantics, RabbitMQ wins.

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
RabbitMQ ──► Producer ──► Processor (N) ──► Batcher ──► BatchProcessor (M)
                                                │
                                                └──► ack to RabbitMQ on success
                                                     requeue on failure
```

### 2. Ack semantics

Broadway tracks each message's `acknowledger` tuple. When the processor returns
`{:ok, message}`, Broadway routes it to a batcher. When the batch finishes, the
batcher calls `ack/3` on the acknowledger — which for RabbitMQ sends
`Basic.Ack` with the delivery tag. On `Message.failed/2`, the acknowledger
sends `Basic.Reject` with `requeue: true`.

### 3. Prefetch vs demand

The RabbitMQ producer sets `qos: [prefetch_count: N]` to tell the broker how
many unacked messages it will hold. This is the client-side buffer. Broadway's
`concurrency` and `max_demand` govern how many of those messages are in flight
across processors.

## Design decisions

- **Option A — One processor stage, many workers**:
  - Pros: simple, uniform handling.
  - Cons: no per-key ordering. Two messages for the same order could run in parallel.
- **Option B — Processors with `partition_by:`**:
  - Pros: per-key serial processing. Messages with same `order_id` land on same processor.
  - Cons: imbalanced load if key distribution is skewed.
- **Option C — Batchers for grouping writes**:
  - Pros: bulk DB inserts. One transaction per N messages.
  - Cons: batch-level failure blows up the whole batch.

Picked: **A + C**. Orders are independent events, partitioning is unnecessary.
Batching is essential to hit 5k msg/s on a DB that does 2ms per single insert
but <50ms per 1000-row bulk insert.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule OrderProcessor.MixProject do
  use Mix.Project

  def project do
    [
      app: :order_processor,
      version: "0.1.0",
      elixir: "~> 1.16",
      deps: deps()
    ]
  end

  def application do
    [mod: {OrderProcessor.Application, []}, extra_applications: [:logger]]
  end

  defp deps do
    [
      {:broadway, "~> 1.1"},
      {:broadway_rabbitmq, "~> 0.8"},
      {:jason, "~> 1.4"},
      {:benchee, "~> 1.3", only: :dev}
    ]
  end
end
```

### Step 1: Application supervisor

**Objective**: Root the pipeline under `:one_for_one` so a Broadway crash restarts topology without taking the OTP app down.

```elixir
defmodule OrderProcessor.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [OrderProcessor.Pipeline]
    Supervisor.start_link(children, strategy: :one_for_one, name: OrderProcessor.Supervisor)
  end
end
```

### Step 2: Broadway pipeline definition

**Objective**: Bind Rabbit prefetch to processor concurrency so `reject_and_requeue` preserves at-least-once without drowning the broker.

```elixir
defmodule OrderProcessor.Pipeline do
  use Broadway

  alias Broadway.Message
  alias OrderProcessor.{Validator, Repo}

  def start_link(_opts) do
    Broadway.start_link(__MODULE__,
      name: __MODULE__,
      producer: [
        module:
          {BroadwayRabbitMQ.Producer,
           queue: "orders.created",
           connection: [host: System.get_env("RABBIT_HOST", "localhost")],
           qos: [prefetch_count: 500],
           on_failure: :reject_and_requeue,
           metadata: [:routing_key, :headers]},
        concurrency: 1
      ],
      processors: [
        default: [
          concurrency: System.schedulers_online(),
          max_demand: 100
        ]
      ],
      batchers: [
        warehouse: [concurrency: 2, batch_size: 200, batch_timeout: 1_000]
      ]
    )
  end

  # ---- callbacks ---------------------------------------------------------

  @impl true
  def handle_message(_processor, %Message{data: data} = message, _context) do
    case Jason.decode(data) do
      {:ok, payload} ->
        case Validator.validate(payload) do
          {:ok, order} ->
            message
            |> Message.update_data(fn _ -> order end)
            |> Message.put_batcher(:warehouse)

          {:error, reason} ->
            Message.failed(message, "validation: #{inspect(reason)}")
        end

      {:error, _} ->
        # Malformed JSON — reject without requeue to avoid poison-pill loops.
        Message.failed(message, "invalid json") |> Message.configure_ack(on_failure: :reject)
    end
  end

  @impl true
  def handle_batch(:warehouse, messages, _batch_info, _context) do
    orders = Enum.map(messages, & &1.data)

    case Repo.insert_all_orders(orders) do
      {:ok, _count} ->
        messages

      {:error, reason} ->
        Enum.map(messages, &Message.failed(&1, "db: #{inspect(reason)}"))
    end
  end
end
```

### Step 3: Validator and Repo stubs

**Objective**: Fail invalid payloads synchronously so poison pills reject instead of requeuing into infinite retry loops.

```elixir
defmodule OrderProcessor.Validator do
  @required ~w(order_id user_id amount)

  def validate(%{} = payload) do
    missing = Enum.filter(@required, fn k -> is_nil(payload[k]) end)

    cond do
      missing != [] -> {:error, {:missing, missing}}
      payload["amount"] <= 0 -> {:error, :invalid_amount}
      true -> {:ok, normalise(payload)}
    end
  end

  def validate(_), do: {:error, :not_a_map}

  defp normalise(p) do
    %{
      order_id: p["order_id"],
      user_id: p["user_id"],
      amount_cents: round(p["amount"] * 100),
      received_at: System.system_time(:millisecond)
    }
  end
end

defmodule OrderProcessor.Repo do
  @moduledoc """
  Stubbed bulk insert. Replace with `MyApp.Repo.insert_all(Order, orders, ...)`.
  """

  def insert_all_orders(orders) when is_list(orders) do
    :telemetry.execute([:order_processor, :batch], %{count: length(orders)}, %{})
    {:ok, length(orders)}
  end
end
```

## Why this works

- **Prefetch 500** + **10 processors with max_demand 100** means up to 500
  unacked messages from RabbitMQ are in flight, chunked across processors.
- **Batcher with `batch_size: 200, batch_timeout: 1_000`** flushes either when
  200 validated messages accumulate or after 1 second — latency-bounded but
  throughput-friendly.
- **`on_failure: :reject_and_requeue`** ensures transient DB failures result
  in message redelivery. RabbitMQ's redelivery flag lets us detect loops and
  send poison messages to a DLQ.
- **Malformed JSON gets `Message.configure_ack(on_failure: :reject)`** without
  requeue, avoiding infinite loops on poison pills. A DLX (dead letter exchange)
  on the queue captures these for later inspection.

## Tests

```elixir
defmodule OrderProcessor.PipelineTest do
  use ExUnit.Case, async: false

  alias OrderProcessor.Pipeline

  describe "handle_message/3" do
    test "routes valid messages to the warehouse batcher" do
      ref = Broadway.test_message(Pipeline, ~s({"order_id":"o1","user_id":"u1","amount":12.5}))
      assert_receive {:ack, ^ref, [%Broadway.Message{batcher: :warehouse}], []}, 2_000
    end

    test "fails messages with missing fields" do
      ref = Broadway.test_message(Pipeline, ~s({"order_id":"o2"}))
      assert_receive {:ack, ^ref, [], [%Broadway.Message{status: {:failed, _}}]}, 2_000
    end

    test "fails (without requeue) on malformed JSON" do
      ref = Broadway.test_message(Pipeline, "not json")
      assert_receive {:ack, ^ref, [], [%Broadway.Message{} = msg]}, 2_000
      assert match?({:failed, _}, msg.status)
    end
  end

  describe "handle_batch/4" do
    test "batches multiple valid messages" do
      payloads =
        for i <- 1..3 do
          ~s({"order_id":"o#{i}","user_id":"u","amount":1.0})
        end

      ref = Broadway.test_batch(Pipeline, payloads)
      assert_receive {:ack, ^ref, successful, []}, 2_000
      assert length(successful) == 3
    end
  end
end
```

## Benchmark

```elixir
# bench/throughput_bench.exs
# Requires a local RabbitMQ running on localhost:5672.
# Seeds 10_000 messages and measures end-to-end processing time.

{:ok, conn} = AMQP.Connection.open(host: "localhost")
{:ok, chan} = AMQP.Channel.open(conn)
AMQP.Queue.declare(chan, "orders.created", durable: true)

payloads =
  for i <- 1..10_000 do
    Jason.encode!(%{order_id: "o#{i}", user_id: "u#{rem(i, 100)}", amount: :rand.uniform() * 100})
  end

start = System.monotonic_time(:millisecond)

for p <- payloads do
  AMQP.Basic.publish(chan, "", "orders.created", p, persistent: true)
end

# Wait for queue to drain via `rabbitmqctl list_queues`.
IO.puts("10k messages published at #{System.monotonic_time(:millisecond) - start}ms")
```

**Target**: 5k–8k msg/s on a single node with batcher concurrency 2 and
batch_size 200. Throughput is bounded by RabbitMQ publish confirms or DB
bulk-insert speed — whichever is slower.

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

**1. `prefetch_count: :infinity` kills back-pressure.**
The broker will ship as many messages as it can. Your node's memory grows
unbounded. Always set a finite prefetch. Rule of thumb: prefetch ≈ 5×
(processor concurrency × max_demand).

**2. Unhandled exceptions in `handle_message/3` trigger restart loops.**
Broadway catches them and marks the message as failed, but if the supervisor
restarts the pipeline every few seconds you likely have a crashing dependency
(e.g. Repo). Investigate with `:observer` and pipeline telemetry.

**3. Batch failure discards a whole batch.**
If `handle_batch/4` fails, all 200 messages get rejected and requeued, even
if only one was bad. For a long-running batch operation, wrap in a try/rescue
and mark only the failing message as failed to protect the rest.

**4. No idempotency → duplicate inserts on requeue.**
At-least-once delivery means the same message can be processed twice (node
restarts, ack lost before DB commit confirms). Use a unique constraint on
`order_id` and `ON CONFLICT DO NOTHING` for the insert.

**5. DLX is not configured by default.**
Without a dead-letter exchange on the queue, rejected-without-requeue messages
are silently dropped. Always set `x-dead-letter-exchange` on the queue
declaration.

**6. When NOT to use Broadway with RabbitMQ.**
For bounded batch processing of files, use `Flow`. For streaming from Kafka,
use `BroadwayKafka`. For lightweight task queues backed by your own database,
use `Oban`.

## Reflection

Your pipeline processes 5k msg/s in steady state. A deploy rolls out a
validator bug that rejects 100% of messages. RabbitMQ's queue size starts
climbing and memory alarms fire on the broker. What is the fastest safe
remediation: pause the consumer, flip the DLX policy to drop, or rollback
the code — and what data is lost or duplicated in each scenario?


## Executable Example

```elixir
defmodule OrderProcessor.Pipeline do
  use Broadway

  alias Broadway.Message
  alias OrderProcessor.{Validator, Repo}

  def start_link(_opts) do
    Broadway.start_link(__MODULE__,
      name: __MODULE__,
      producer: [
        module:
          {BroadwayRabbitMQ.Producer,
           queue: "orders.created",
           connection: [host: System.get_env("RABBIT_HOST", "localhost")],
           qos: [prefetch_count: 500],
           on_failure: :reject_and_requeue,
           metadata: [:routing_key, :headers]},
        concurrency: 1
      ],
      processors: [
        default: [
          concurrency: System.schedulers_online(),
          max_demand: 100
        ]
      ],
      batchers: [
        warehouse: [concurrency: 2, batch_size: 200, batch_timeout: 1_000]
      ]
    )
  end

  # ---- callbacks ---------------------------------------------------------

  @impl true
  def handle_message(_processor, %Message{data: data} = message, _context) do
    case Jason.decode(data) do
      {:ok, payload} ->
        case Validator.validate(payload) do
          {:ok, order} ->
            message
            |> Message.update_data(fn _ -> order end)
            |> Message.put_batcher(:warehouse)

          {:error, reason} ->
            Message.failed(message, "validation: #{inspect(reason)}")
        end

      {:error, _} ->
        # Malformed JSON — reject without requeue to avoid poison-pill loops.
        Message.failed(message, "invalid json") |> Message.configure_ack(on_failure: :reject)
    end
  end

  @impl true
  def handle_batch(:warehouse, messages, _batch_info, _context) do
    orders = Enum.map(messages, & &1.data)

    case Repo.insert_all_orders(orders) do
      {:ok, _count} ->
        messages

      {:error, reason} ->
        Enum.map(messages, &Message.failed(&1, "db: #{inspect(reason)}"))
    end
  end
end

defmodule Main do
  def main do
      # Simulate RabbitMQ order processing with validation and acknowledgement
      orders = [
        %{id: "o1", customer: "c1", amount: 100},
        %{id: "o2", customer: "c2", amount: 0},  # Invalid
        %{id: "o3", customer: "c3", amount: 250}
      ]

      # Validate and acknowledge
      results = Enum.map(orders, fn order ->
        if order.amount > 0 do
          Map.put(order, :status, :validated)
        else
          Map.put(order, :status, :invalid)
        end
      end)

      # Count valid orders
      valid_orders = Enum.filter(results, &(&1.status == :validated))

      IO.inspect(valid_orders, label: "✓ Valid orders")
      IO.puts("✓ Processed #{length(results)} orders, #{length(valid_orders)} valid")

      assert length(valid_orders) == 2, "Expected 2 valid orders"
      assert Enum.all?(valid_orders, &(&1.status == :validated)), "All valid"

      IO.puts("✓ Broadway RabbitMQ: at-least-once processing working")
  end
end

Main.main()
```
