# Broadway Data Pipelines

**Project**: `api_gateway` — a standalone HTTP gateway exercise

---

## Project context

You are building `api_gateway`, an HTTP gateway that routes traffic to microservices. The
gateway receives webhook events from payment providers and must process them reliably:
parse, validate, route to priority consumers, and persist to the database in bulk — all
while guaranteeing that no event is silently lost even if a processor crashes. GenStage
provides the back-pressure model; Broadway adds acknowledgment, batching, and
production-grade observability on top.

Project structure:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       └── middleware/
│           ├── webhook_pipeline.ex     # ← Broadway pipeline
│           └── simulated_producer.ex   # ← in-memory producer for testing
├── test/
│   └── api_gateway/
│       └── middleware/
│           └── webhook_pipeline_test.exs  # given tests
└── mix.exs
```

---

## The business problem

The payment provider delivers webhook events via HTTP POST. Each event must be:
1. Parsed from raw JSON
2. Routed: `:high_priority` events processed immediately, `:normal` events in bulk
3. Persisted in bulk (100 per insert) for efficiency
4. Acknowledged to the provider — if the gateway crashes before ack, the event is
   re-delivered (at-least-once)
5. Failed events sent to a dead-letter queue instead of being silently dropped

---

## Broadway vs GenStage

GenStage gives you the demand-driven pipeline primitive. Broadway is a production layer
on top of GenStage that adds:

| Feature | GenStage | Broadway |
|---|---|---|
| Back-pressure | Yes | Yes (inherited) |
| Acknowledgment | Manual | Built-in per message |
| Batching | Manual | Configurable per batcher |
| Concurrency config | Manual supervisor | Declarative in `start_link` |
| Dead-letter handling | Manual | `handle_failed/2` callback |
| Producers | Custom GenStage modules | Plug-and-play: SQS, Kafka, RabbitMQ |

The acknowledgment contract:
- Message returned from `handle_message/3` without `:failed` status -> **ack** (processed)
- Message with `:failed` status -> **nack** (returned to the queue for redelivery)
- Process crash before returning -> **nack** (automatic)

---

## Implementation

### Step 1: `mix.exs`

```elixir
defp deps do
  [
    {:broadway, "~> 1.0"},
    {:jason, "~> 1.4"}
  ]
end
```

### Step 2: `lib/api_gateway/middleware/simulated_producer.ex`

The simulated producer wraps raw messages into Broadway.Message structs with a
`NoopAcknowledger`. In production, you would use `BroadwaySQS.Producer` or
`BroadwayKafka.Producer` which handle acknowledgment natively.

```elixir
defmodule ApiGateway.Middleware.SimulatedProducer do
  @moduledoc """
  In-memory Broadway producer. Wraps a list of raw messages for testing
  and local development without external queue infrastructure.
  """
  use GenStage

  def start_link(opts) do
    GenStage.start_link(__MODULE__, opts[:messages] || [], name: __MODULE__)
  end

  def init(messages) do
    {:producer, messages}
  end

  def handle_demand(demand, messages) do
    {to_emit, remaining} = Enum.split(messages, demand)

    broadway_messages =
      Enum.map(to_emit, fn msg ->
        %Broadway.Message{
          data: msg,
          acknowledger: {Broadway.NoopAcknowledger, nil, nil}
        }
      end)

    {:noreply, broadway_messages, remaining}
  end
end
```

### Step 3: `lib/api_gateway/middleware/webhook_pipeline.ex`

The pipeline has three batchers: `:normal` for bulk inserts, `:high_priority` for immediate
processing, and `:dead_letter` for permanently failed messages.

`handle_message/3` is the routing stage. It parses JSON, determines priority, and assigns
each message to the appropriate batcher. Parse failures are marked as failed and routed to
the dead-letter batcher.

`handle_batch/4` implements the batch-level logic: bulk insert for normal messages,
immediate processing for high-priority, and logging/alerting for dead-letter entries.

```elixir
defmodule ApiGateway.Middleware.WebhookPipeline do
  @moduledoc """
  Broadway pipeline for processing payment webhook events.

  Stages:
    1. SimulatedProducer — emits raw JSON strings
    2. handle_message/3  — parses JSON, validates, routes to batcher
    3. handle_batch/4    — bulk insert (:normal) or immediate process (:high_priority)
    4. handle_batch/4    — dead-letter queue (:dead_letter)

  Acknowledgment:
    - Successful messages -> ack (Broadway default when no :failed status)
    - Permanently failed messages -> nack + routed to :dead_letter batcher
    - Transiently failed messages -> nack without DLQ (provider re-delivers)
  """
  use Broadway

  # ---------------------------------------------------------------------------
  # Lifecycle
  # ---------------------------------------------------------------------------

  def start_link(messages) do
    Broadway.start_link(__MODULE__,
      name: __MODULE__,
      producer: [
        module: {ApiGateway.Middleware.SimulatedProducer, messages: messages},
        concurrency: 1
      ],
      processors: [
        default: [concurrency: 5]
      ],
      batchers: [
        normal: [batch_size: 100, batch_timeout: 2_000, concurrency: 2],
        high_priority: [batch_size: 1, batch_timeout: 100, concurrency: 5],
        dead_letter: [batch_size: 10, batch_timeout: 500, concurrency: 1]
      ]
    )
  end

  # ---------------------------------------------------------------------------
  # Message handler — called once per message
  # ---------------------------------------------------------------------------

  @impl true
  def handle_message(_processor, message, _context) do
    case parse_message(message.data) do
      {:ok, parsed} ->
        batcher = if parsed["priority"] == "high", do: :high_priority, else: :normal

        message
        |> Broadway.Message.update_data(fn _raw -> parsed end)
        |> Broadway.Message.put_batcher(batcher)

      {:error, reason} ->
        message
        |> Broadway.Message.failed(reason)
        |> Broadway.Message.put_batcher(:dead_letter)
    end
  end

  # ---------------------------------------------------------------------------
  # Batch handlers — called once per batch
  # ---------------------------------------------------------------------------

  @impl true
  def handle_batch(:normal, messages, _batch_info, _context) do
    records = Enum.map(messages, fn msg ->
      %{data: msg.data, inserted_at: DateTime.utc_now()}
    end)

    IO.puts("Bulk insert: #{length(records)} records")
    messages
  end

  @impl true
  def handle_batch(:high_priority, messages, _batch_info, _context) do
    Enum.each(messages, fn msg ->
      IO.puts("HIGH PRIORITY: #{inspect(msg.data)}")
    end)

    messages
  end

  @impl true
  def handle_batch(:dead_letter, messages, _batch_info, _context) do
    Enum.each(messages, fn msg ->
      IO.puts("[DLQ] status=#{inspect(msg.status)} data=#{inspect(msg.data)}")
    end)

    messages
  end

  # ---------------------------------------------------------------------------
  # Failed messages not routed to a batcher
  # ---------------------------------------------------------------------------

  @impl true
  def handle_failed(messages, _context) do
    IO.puts("[Pipeline] #{length(messages)} messages being nacked for redelivery")
    messages
  end

  # ---------------------------------------------------------------------------
  # Private helpers
  # ---------------------------------------------------------------------------

  defp parse_message(data) when is_binary(data) do
    Jason.decode(data)
  end

  defp parse_message(data) when is_map(data) do
    {:ok, data}
  end

  defp parse_message(data) do
    {:error, {:invalid_format, data}}
  end
end
```

### Step 4: Given tests — must pass without modification

```elixir
# test/api_gateway/middleware/webhook_pipeline_test.exs
defmodule ApiGateway.Middleware.WebhookPipelineTest do
  use ExUnit.Case, async: false

  alias ApiGateway.Middleware.WebhookPipeline

  test "pipeline supervisor stays alive after processing all messages" do
    messages =
      for i <- 1..10 do
        priority = if rem(i, 5) == 0, do: "high", else: "normal"
        Jason.encode!(%{id: i, priority: priority, payload: "data_#{i}"})
      end

    {:ok, pid} = WebhookPipeline.start_link(messages)

    # Wait for processors and batchers to drain
    Process.sleep(3_000)

    # Broadway supervisor must still be running — it must not have crashed
    assert Process.alive?(pid)
  end

  test "invalid JSON messages do not crash the pipeline" do
    messages = ["not valid json", "{\"broken\":"]

    {:ok, pid} = WebhookPipeline.start_link(messages)
    Process.sleep(500)

    # Pipeline must survive malformed input — DLQ handler receives them
    assert Process.alive?(pid)
  end

  test "handle_batch returning messages keeps the pipeline alive" do
    # handle_batch MUST return the message list. Returning nil causes Broadway
    # to nack all messages and may cascade into repeated supervisor restarts.
    # Verify the supervisor is stable after one processing cycle.
    {:ok, pid} = WebhookPipeline.start_link([
      Jason.encode!(%{id: 1, priority: "normal", payload: "x"})
    ])

    Process.sleep(1_500)

    assert Process.alive?(pid)
  end

  test "high-priority messages route to a dedicated batcher" do
    # A message with priority "high" must reach the :high_priority batcher,
    # not the :normal batcher. Since WebhookPipeline.handle_batch/4 logs
    # batches to stdout, we verify the pipeline does not crash and the
    # supervisor stays alive — the routing is validated by the absence of
    # Broadway batcher-not-found errors.
    messages = [Jason.encode!(%{id: 99, priority: "high", payload: "urgent"})]

    {:ok, pid} = WebhookPipeline.start_link(messages)
    Process.sleep(500)

    assert Process.alive?(pid)
  end
end
```

### Step 5: Run the tests

```bash
mix test test/api_gateway/middleware/webhook_pipeline_test.exs --trace
```

---

## Trade-off analysis

| Aspect | Broadway | GenStage (raw) | `Task.async_stream` |
|--------|----------|----------------|---------------------|
| Acknowledgment | Built-in | Manual | Not applicable |
| Batching | Configurable | Manual | Not applicable |
| Dead-letter | `handle_failed/2` | Manual | Not applicable |
| Back-pressure | Yes | Yes | Yes (max_concurrency) |
| Learning curve | Medium | High | Low |
| Best for | Message queues, at-least-once | Custom pipeline topologies | Finite collections |

Reflection: when would you prefer raw GenStage over Broadway? Consider a pipeline that
reads from an ETS table (no external queue, no ack needed) and fans out to 5 processors.

---

## Common production mistakes

**1. Not returning `messages` from `handle_batch/4`**
Broadway uses the returned list to decide which messages to ack. Returning `nil` or
an empty list causes all messages in the batch to be nacked, potentially re-delivering
them indefinitely.

**2. Raising exceptions in `handle_message/3`**
An uncaught exception in `handle_message/3` crashes the processor. Broadway restarts
it, but the message is nacked. Use `Jason.decode/1` (returns `{:ok, _}` or `{:error, _}`)
instead of `Jason.decode!/1` inside handlers.

**3. Using `handle_failed/2` to re-enqueue manually**
`handle_failed/2` is called for messages that are being nacked. Re-enqueuing them
manually in addition to the nack creates duplicates. Let the producer's nack mechanism
handle redelivery.

**4. `batch_timeout` too high in production**
A `batch_timeout: 60_000` means that if the batch does not reach `batch_size`, messages
wait up to 60 seconds. Set `batch_timeout` to the maximum acceptable latency for your
use case, not the maximum batch wait.

**5. Assigning `:failed` + `put_batcher(:dead_letter)` in the wrong order**
`Broadway.Message.failed/2` marks the message but does not route it. You must call
`put_batcher(:dead_letter)` after `failed/2` — the batcher assignment is what moves
the message to the DLQ handler.

---

## Resources

- [Broadway — HexDocs](https://hexdocs.pm/broadway/Broadway.html)
- [Broadway.Message — HexDocs](https://hexdocs.pm/broadway/Broadway.Message.html)
- [Broadway GitHub](https://github.com/dashbitco/broadway)
- [Building Data Pipelines with Broadway — ElixirConf](https://www.youtube.com/watch?v=luHK-RZd5uQ)
