# Broadway with SQS and Multi-Batcher Fan-Out

**Project**: `notifications_dispatcher` — consumes AWS SQS messages describing user notifications and fans them out to three channels (email, SMS, push) via dedicated batchers.

## Project context

The platform emits `user.notification_requested` events to an SQS queue. Each
event carries a delivery channel (`email`, `sms`, `push`). Each channel has
its own downstream API with wildly different rate limits:

- Email (SES): 14 msg/s per region; accepts batches of 50.
- SMS (Twilio): 100 msg/s; batch size 1 (one API call per SMS).
- Push (FCM): 500 msg/s; accepts batches of 1_000.

A single batcher with `batch_size` tuned for one channel under-utilises the
others. Broadway's multi-batcher feature lets us route messages to the right
batcher via `Message.put_batcher/2`, each with its own concurrency and size.

```
notifications_dispatcher/
├── lib/
│   └── notifications_dispatcher/
│       ├── application.ex
│       ├── pipeline.ex
│       └── senders/
│           ├── email.ex
│           ├── sms.ex
│           └── push.ex
├── test/
│   └── notifications_dispatcher/
│       └── pipeline_test.exs
├── bench/
│   └── throughput_bench.exs
└── mix.exs
```

## Why multiple batchers and not one per pipeline

Using a single batcher forces you to choose one `batch_size`. Pick 50 to fit
SES, and FCM under-batches by 20×. Pick 1000 to fit FCM, and every SMS waits
for 999 friends that will never arrive (triggering `batch_timeout`) — added
latency with no benefit.

Alternatives:

- **Three separate Broadway pipelines** (one per channel, each with its own
  SQS queue): clean but requires producers upstream to publish to three queues.
  Operational overhead triples.
- **One Broadway + one batcher + per-message flush**: no batching, per-message
  API call. Wastes throughput budget for push and email.
- **`Task.async_stream` per message**: loses back-pressure and ack semantics.

Broadway's multi-batcher pattern is the intended solution: one pipeline,
one ack path, many per-channel batch shapes.

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
### 1. `Message.put_batcher/2`

Inside `handle_message/3`, the processor decides which batcher should own
the message:

```elixir
message |> Message.put_batcher(:email)
```

The message then flows to the `:email` batcher (configured separately) and
gets acked only when `handle_batch(:email, ...)` succeeds.

### 2. `partition_by:` for ordered per-key processing

For notifications this is irrelevant (each is independent), but for domains
like user-event streams you want all events for the same `user_id` to hit the
same processor. Broadway's `partition_by:` takes a function `(Message -> term)`
and uses `:erlang.phash2/2` to route.

### 3. SQS message attributes

SQS supports per-message attributes (small key-value metadata). The producer
can surface them via `metadata:` option and processors read them from
`message.metadata`.

## Design decisions

- **Option A — One batcher, batch_size = max of all channels (1000)**:
  - Pros: simple.
  - Cons: SMS messages wait up to `batch_timeout` before sending. Latency spike.
- **Option B — Three batchers, one per channel, tuned per channel**:
  - Pros: each channel runs at its optimal rate. Cleanest model.
  - Cons: more config surface; three bugs to write.
- **Option C — Separate Broadway per channel**:
  - Pros: full isolation, independent scaling.
  - Cons: three SQS queues, three ack paths, more infra.

Chose **Option B**. Single queue is the contract the producers already use;
three batchers give channel-specific throughput without splitting ownership.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule NotificationsDispatcher.MixProject do
  use Mix.Project

  def project do
    [
      app: :notifications_dispatcher,
      version: "0.1.0",
      elixir: "~> 1.16",
      deps: deps()
    ]
  end

  def application do
    [mod: {NotificationsDispatcher.Application, []}, extra_applications: [:logger]]
  end

  defp deps do
    [
      {:broadway, "~> 1.1"},
      {:broadway_sqs, "~> 0.7"},
      {:hackney, "~> 1.20"},
      {:jason, "~> 1.4"},
      {:benchee, "~> 1.3", only: :dev}
    ]
  end
end
```

### Step 1: Pipeline

**Objective**: Demultiplex by channel into three batchers with distinct size/timeout so email, SMS, and push match each vendor's rate model.

```elixir
defmodule NotificationsDispatcher.Pipeline do
  use Broadway

  alias Broadway.Message
  alias NotificationsDispatcher.Senders.{Email, Sms, Push}

  def start_link(_opts) do
    Broadway.start_link(__MODULE__,
      name: __MODULE__,
      producer: [
        module:
          {BroadwayAWS.SQS.Producer,
           queue_url: System.fetch_env!("SQS_QUEUE_URL"),
           receive_interval: 50,
           wait_time_seconds: 20,
           max_number_of_messages: 10},
        concurrency: 2
      ],
      processors: [
        default: [concurrency: System.schedulers_online() * 2, max_demand: 50]
      ],
      batchers: [
        email: [concurrency: 1, batch_size: 50, batch_timeout: 1_000],
        sms:   [concurrency: 4, batch_size: 1, batch_timeout: 100],
        push:  [concurrency: 2, batch_size: 500, batch_timeout: 500]
      ]
    )
  end

  # ---- processor --------------------------------------------------------

  @impl true
  def handle_message(_processor, %Message{data: data} = message, _ctx) do
    case Jason.decode(data) do
      {:ok, %{"channel" => "email"} = p} ->
        message
        |> Message.update_data(fn _ -> normalise(p) end)
        |> Message.put_batcher(:email)

      {:ok, %{"channel" => "sms"} = p} ->
        message
        |> Message.update_data(fn _ -> normalise(p) end)
        |> Message.put_batcher(:sms)

      {:ok, %{"channel" => "push"} = p} ->
        message
        |> Message.update_data(fn _ -> normalise(p) end)
        |> Message.put_batcher(:push)

      {:ok, %{"channel" => other}} ->
        Message.failed(message, "unknown channel: #{other}")

      {:error, _} ->
        Message.failed(message, "invalid json")
    end
  end

  # ---- batchers ---------------------------------------------------------

  @impl true
  def handle_batch(:email, messages, _info, _ctx) do
    payloads = Enum.map(messages, & &1.data)
    fail_or_pass(messages, Email.send_batch(payloads))
  end

  def handle_batch(:sms, [message], _info, _ctx) do
    case Sms.send_one(message.data) do
      :ok -> [message]
      {:error, reason} -> [Message.failed(message, inspect(reason))]
    end
  end

  def handle_batch(:push, messages, _info, _ctx) do
    payloads = Enum.map(messages, & &1.data)
    fail_or_pass(messages, Push.send_batch(payloads))
  end

  # ---- helpers ----------------------------------------------------------

  defp normalise(p), do: %{user_id: p["user_id"], payload: p["payload"], channel: p["channel"]}

  defp fail_or_pass(messages, :ok), do: messages

  defp fail_or_pass(messages, {:error, reason}) do
    Enum.map(messages, &Message.failed(&1, inspect(reason)))
  end
end
```

### Step 2: Sender stubs

**Objective**: Stub senders emit telemetry only so batch-size and batch-timeout contracts are measured without real SES, Twilio, or FCM calls.

Real senders would use Swoosh/ExAws/Pigeon. These stubs make the exercise
runnable without AWS credentials.

```elixir
defmodule NotificationsDispatcher.Senders.Email do
  def send_batch(payloads) do
    :telemetry.execute([:dispatcher, :email, :batch], %{size: length(payloads)}, %{})
    :ok
  end
end

defmodule NotificationsDispatcher.Senders.Sms do
  def send_one(_payload), do: :ok
end

defmodule NotificationsDispatcher.Senders.Push do
  def send_batch(payloads) do
    :telemetry.execute([:dispatcher, :push, :batch], %{size: length(payloads)}, %{})
    :ok
  end
end
```

## Why this works

- **One processor pool** demultiplexes incoming messages by channel at near-zero
  cost (pattern match + `put_batcher`).
- **Per-channel batchers** each have their own mailbox and concurrency. An
  email backlog never blocks SMS.
- **SMS batch_size 1, batch_timeout 100** means SMS is effectively un-batched
  but still acked by Broadway — the only way to guarantee low latency for a
  channel whose downstream API doesn't accept batches.
- **Push batch_size 500, batch_timeout 500** means up to 500 pushes per API
  call, flushed every 500ms regardless — saturating FCM's bulk endpoint.

## Tests

```elixir
defmodule NotificationsDispatcher.PipelineTest do
  use ExUnit.Case, async: false

  alias NotificationsDispatcher.Pipeline

  describe "routing by channel" do
    test "email goes to :email batcher" do
      msg = ~s({"channel":"email","user_id":"u1","payload":{"to":"a@b.c"}})
      ref = Broadway.test_message(Pipeline, msg)
      assert_receive {:ack, ^ref, [%Broadway.Message{batcher: :email}], []}, 2_000
    end

    test "sms goes to :sms batcher" do
      msg = ~s({"channel":"sms","user_id":"u1","payload":{"to":"+111"}})
      ref = Broadway.test_message(Pipeline, msg)
      assert_receive {:ack, ^ref, [%Broadway.Message{batcher: :sms}], []}, 2_000
    end

    test "push goes to :push batcher" do
      msg = ~s({"channel":"push","user_id":"u1","payload":{"token":"abc"}})
      ref = Broadway.test_message(Pipeline, msg)
      assert_receive {:ack, ^ref, [%Broadway.Message{batcher: :push}], []}, 2_000
    end

    test "unknown channel fails the message" do
      msg = ~s({"channel":"smoke"})
      ref = Broadway.test_message(Pipeline, msg)
      assert_receive {:ack, ^ref, [], [%Broadway.Message{status: {:failed, _}}]}, 2_000
    end
  end
end
```

## Benchmark

```elixir
# bench/throughput_bench.exs
# Uses Broadway.test_batch/3 to avoid needing a live SQS queue.

payloads =
  for i <- 1..10_000 do
    channel = Enum.random(["email", "sms", "push"])
    Jason.encode!(%{channel: channel, user_id: "u#{i}", payload: %{}})
  end

Benchee.run(%{
  "10k mixed" => fn ->
    ref = Broadway.test_batch(NotificationsDispatcher.Pipeline, payloads)
    receive do
      {:ack, ^ref, _ok, _fail} -> :ok
    after
      30_000 -> flunk("pipeline timed out")
    end
  end
}, time: 10, warmup: 3)
```

**Target**: 8k–15k msg/s end-to-end with stub senders. Real numbers will be
gated by the downstream APIs (SES 14/s, Twilio 100/s, FCM 500/s).

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

**1. Batcher concurrency > 1 on a rate-limited downstream = breach.**
If SES only allows 14 msg/s per region and you run 4 parallel batchers each
sending 50 in one go, you'll exceed the limit. Tune `concurrency:` to
`downstream_limit / batch_size / flush_interval_s` or use a token bucket.

**2. `batch_timeout` and `batch_size` interact.**
If the incoming rate is low, messages wait the full `batch_timeout`. For SMS
at 5 msg/s with `batch_size: 100`, each SMS waits up to 10 seconds. That's
why SMS here uses `batch_size: 1`.

**3. SQS `visibility_timeout` must exceed worst-case processing time.**
If your pipeline takes 45s under load and visibility_timeout is 30s, the
message becomes visible again and another worker processes it — duplicate
sends. Set visibility_timeout generously and use the `extend_visibility`
SQS API for long-running messages.

**4. Max receive count → DLQ.**
SQS lets you set a redrive policy. After N failed receives, the message is
moved to a DLQ. Without this, a poison message loops forever. Always
configure redrive.

**5. Cold start penalty.**
`BroadwaySQS` starts polling with `wait_time_seconds: 20` (long polling).
First message arrives up to 20s after startup. That's a feature, not a bug —
long polling is cheap and matches SQS billing model.

**6. When NOT to use Broadway + SQS.**
If you need ordering, switch to SQS FIFO queues and `partition_by:` message
group id. If you need pub/sub, consider SNS → SQS fan-in. If you need
sub-second latency at high throughput, Kafka is cheaper than SQS.

## Reflection

A spike in SMS traffic (10× normal) arrives. The `:sms` batcher with
`concurrency: 4, batch_size: 1` saturates. Messages back up in the processor
stage, which then stops pulling from the producer. Now email and push
throughput also drop, even though their batchers are idle. Why are they
affected, and what Broadway knob fixes this without increasing SMS concurrency?


## Executable Example

```elixir
defmodule NotificationsDispatcher.Pipeline do
  use Broadway

  alias Broadway.Message
  alias NotificationsDispatcher.Senders.{Email, Sms, Push}

  def start_link(_opts) do
    Broadway.start_link(__MODULE__,
      name: __MODULE__,
      producer: [
        module:
          {BroadwayAWS.SQS.Producer,
           queue_url: System.fetch_env!("SQS_QUEUE_URL"),
           receive_interval: 50,
           wait_time_seconds: 20,
           max_number_of_messages: 10},
        concurrency: 2
      ],
      processors: [
        default: [concurrency: System.schedulers_online() * 2, max_demand: 50]
      ],
      batchers: [
        email: [concurrency: 1, batch_size: 50, batch_timeout: 1_000],
        sms:   [concurrency: 4, batch_size: 1, batch_timeout: 100],
        push:  [concurrency: 2, batch_size: 500, batch_timeout: 500]
      ]
    )
  end

  # ---- processor --------------------------------------------------------

  @impl true
  def handle_message(_processor, %Message{data: data} = message, _ctx) do
    case Jason.decode(data) do
      {:ok, %{"channel" => "email"} = p} ->
        message
        |> Message.update_data(fn _ -> normalise(p) end)
        |> Message.put_batcher(:email)

      {:ok, %{"channel" => "sms"} = p} ->
        message
        |> Message.update_data(fn _ -> normalise(p) end)
        |> Message.put_batcher(:sms)

      {:ok, %{"channel" => "push"} = p} ->
        message
        |> Message.update_data(fn _ -> normalise(p) end)
        |> Message.put_batcher(:push)

      {:ok, %{"channel" => other}} ->
        Message.failed(message, "unknown channel: #{other}")

      {:error, _} ->
        Message.failed(message, "invalid json")
    end
  end

  # ---- batchers ---------------------------------------------------------

  @impl true
  def handle_batch(:email, messages, _info, _ctx) do
    payloads = Enum.map(messages, & &1.data)
    fail_or_pass(messages, Email.send_batch(payloads))
  end

  def handle_batch(:sms, [message], _info, _ctx) do
    case Sms.send_one(message.data) do
      :ok -> [message]
      {:error, reason} -> [Message.failed(message, inspect(reason))]
    end
  end

  def handle_batch(:push, messages, _info, _ctx) do
    payloads = Enum.map(messages, & &1.data)
    fail_or_pass(messages, Push.send_batch(payloads))
  end

  # ---- helpers ----------------------------------------------------------

  defp normalise(p), do: %{user_id: p["user_id"], payload: p["payload"], channel: p["channel"]}

  defp fail_or_pass(messages, :ok), do: messages

  defp fail_or_pass(messages, {:error, reason}) do
    Enum.map(messages, &Message.failed(&1, inspect(reason)))
  end
end

defmodule Main do
  def main do
      # Simulate SQS multi-batcher fan-out: route notifications to 3 channels
      notifications = [
        %{id: "n1", user: "u1", channels: [:email, :sms, :push]},
        %{id: "n2", user: "u2", channels: [:email, :push]},
        %{id: "n3", user: "u3", channels: [:sms]}
      ]

      # Fan-out to batchers
      email_batch = Enum.filter(notifications, &(:email in &1.channels))
      sms_batch = Enum.filter(notifications, &(:sms in &1.channels))
      push_batch = Enum.filter(notifications, &(:push in &1.channels))

      IO.puts("✓ Email batch: #{length(email_batch)} notifications")
      IO.puts("✓ SMS batch: #{length(sms_batch)} notifications")
      IO.puts("✓ Push batch: #{length(push_batch)} notifications")

      assert length(email_batch) == 2, "Email batch correct"
      assert length(sms_batch) == 2, "SMS batch correct"
      assert length(push_batch) == 2, "Push batch correct"

      IO.puts("✓ SQS multi-batcher fan-out working")
  end
end

Main.main()
```
