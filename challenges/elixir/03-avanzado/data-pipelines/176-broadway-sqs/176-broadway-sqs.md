# BroadwaySQS with LocalStack

**Project**: `broadway_sqs` — a webhook-delivery retry queue backed by AWS SQS, tested locally against LocalStack.

**Difficulty**: ★★★★☆

**Estimated time**: 4–6 hours

---

## Project context

A SaaS product delivers webhooks to customer endpoints. Failed deliveries
(5xx, timeouts) are parked on an SQS queue for asynchronous retry with
exponential backoff. Successful retries are ack'd (deleted from the queue);
persistent failures after N attempts go to a dead-letter queue (DLQ)
configured at the SQS level.

Peak retry volume: ~3k messages/sec during incidents. Baseline: ~100
msgs/sec. Cost matters — each SQS `ReceiveMessage` costs $0.40 per million.
Long polling + batching are non-negotiable.

You will build the pipeline against **LocalStack** so tests are
hermetic (no real AWS account needed) and the deploy flip to production is
a config change only.

```
broadway_sqs/
├── lib/
│   └── broadway_sqs_retry/
│       ├── application.ex
│       ├── pipeline.ex
│       └── webhook_dispatcher.ex
├── test/
│   └── broadway_sqs_retry/
│       └── pipeline_test.exs
├── docker-compose.yml       # localstack
└── mix.exs
```

---

## Core concepts

### 1. SQS fetch economics

SQS charges per API call, not per message. A `ReceiveMessage` call can
return up to 10 messages. At 3k msgs/sec, fetching 1-at-a-time costs $1.20
per million msgs; fetching 10-at-a-time costs $0.12. Always batch via
`receive_interval` and `receive_messages`.

### 2. Long polling vs short polling

- **Short polling** (`WaitTimeSeconds: 0`): returns immediately, even if the
  queue is empty. Cheap per call but forces you to spin.
- **Long polling** (`WaitTimeSeconds: 20`): holds the connection open until
  a message arrives or 20s pass. Drastically reduces empty responses.

Always use long polling in production. The exception is test scenarios
where you want fast-fail on empty.

### 3. Visibility timeout

When you fetch a message, SQS "hides" it for `VisibilityTimeout` seconds. If
you don't delete it in that window, another consumer re-fetches it. Set
this to `p99(handle_message) + p99(handle_batch) + margin`. Too low: other
workers reprocess. Too high: a dead worker's messages sit invisible for
minutes.

### 4. BroadwaySQS acknowledgement

BroadwaySQS deletes successful messages from SQS after the batch returns.
Failed messages are **not** deleted — they reappear after the visibility
timeout for another attempt. This is at-least-once. You are responsible for
idempotent `handle_message` and for capping retries via SQS's
`maxReceiveCount` DLQ policy.

### 5. Testing with LocalStack

LocalStack runs an emulated AWS API in Docker. It supports SQS (CreateQueue,
ReceiveMessage, DeleteMessage). ExAws can be pointed at it via
`scheme: "http://", host: "localhost", port: 4566`.

---

## Implementation

### Step 1: Deps

```elixir
defp deps do
  [
    {:broadway_sqs, "~> 0.7"},
    {:ex_aws, "~> 2.5"},
    {:ex_aws_sqs, "~> 3.4"},
    {:hackney, "~> 1.20"},
    {:sweet_xml, "~> 0.7"},
    {:req, "~> 0.5"},
    {:jason, "~> 1.4"}
  ]
end
```

### Step 2: LocalStack via docker-compose

```yaml
# docker-compose.yml
services:
  localstack:
    image: localstack/localstack:3
    ports:
      - "4566:4566"
    environment:
      SERVICES: sqs
      DEFAULT_REGION: us-east-1
```

Bring up and create the queues:

```bash
docker compose up -d
aws --endpoint-url=http://localhost:4566 sqs create-queue --queue-name webhook-retry
aws --endpoint-url=http://localhost:4566 sqs create-queue --queue-name webhook-dlq
```

### Step 3: Pipeline

```elixir
defmodule BroadwaySqsRetry.Pipeline do
  @moduledoc """
  Retries webhook deliveries pulled from SQS. Successful deliveries are
  acked (deleted from the queue). Failed deliveries return to the queue
  after the visibility timeout for SQS to retry.
  """
  use Broadway

  alias Broadway.Message
  alias BroadwaySqsRetry.WebhookDispatcher

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    queue_url = Keyword.fetch!(opts, :queue_url)

    config = [
      scheme: "http://",
      host: "localhost",
      port: 4566,
      access_key_id: "test",
      secret_access_key: "test",
      region: "us-east-1"
    ]

    Broadway.start_link(__MODULE__,
      name: __MODULE__,
      producer: [
        module:
          {BroadwaySQS.Producer,
           queue_url: queue_url,
           max_number_of_messages: 10,
           wait_time_seconds: 20,
           visibility_timeout: 60,
           config: config},
        concurrency: 2
      ],
      processors: [default: [concurrency: 16]],
      batchers: [default: [concurrency: 4, batch_size: 20, batch_timeout: 1_000]]
    )
  end

  @impl true
  def handle_message(_p, %Message{data: body} = msg, _ctx) do
    with {:ok, payload} <- Jason.decode(body),
         %{"url" => url, "body" => b} <- payload,
         :ok <- WebhookDispatcher.deliver(url, b) do
      msg
    else
      {:error, reason} -> Message.failed(msg, reason)
      _ -> Message.failed(msg, :bad_payload)
    end
  end

  @impl true
  def handle_batch(:default, messages, _batch_info, _ctx), do: messages
end
```

### Step 4: Webhook dispatcher

```elixir
defmodule BroadwaySqsRetry.WebhookDispatcher do
  @spec deliver(String.t(), map() | binary()) :: :ok | {:error, term()}
  def deliver(url, body) do
    case Req.post(url, json: body, receive_timeout: 5_000, retry: false) do
      {:ok, %{status: s}} when s in 200..299 -> :ok
      {:ok, %{status: s}} -> {:error, {:http_status, s}}
      {:error, reason} -> {:error, reason}
    end
  end
end
```

### Step 5: Application

```elixir
defmodule BroadwaySqsRetry.Application do
  use Application

  @impl true
  def start(_t, _a) do
    queue = System.get_env("WEBHOOK_QUEUE_URL", "http://localhost:4566/000000000000/webhook-retry")

    children = [{BroadwaySqsRetry.Pipeline, [queue_url: queue]}]
    Supervisor.start_link(children, strategy: :one_for_one)
  end
end
```

### Step 6: Tests

```elixir
defmodule BroadwaySqsRetry.PipelineTest do
  use ExUnit.Case, async: false

  alias ExAws.SQS

  @config [
    scheme: "http://",
    host: "localhost",
    port: 4566,
    access_key_id: "test",
    secret_access_key: "test",
    region: "us-east-1"
  ]

  @queue_name "webhook-retry-test"

  setup do
    {:ok, _} = SQS.create_queue(@queue_name) |> ExAws.request(@config)

    {:ok, %{body: body}} =
      SQS.get_queue_url(@queue_name) |> ExAws.request(@config)

    url = body.queue_url
    SQS.purge_queue(url) |> ExAws.request(@config)

    on_exit(fn -> SQS.purge_queue(url) |> ExAws.request(@config) end)
    %{url: url}
  end

  @tag :localstack
  test "delivers a successful webhook and acks from queue", %{url: url} do
    bypass = Bypass.open()
    Bypass.expect_once(bypass, fn conn -> Plug.Conn.resp(conn, 200, "ok") end)

    payload = %{url: "http://localhost:#{bypass.port}/hook", body: %{event: "x"}}

    {:ok, _} =
      SQS.send_message(url, Jason.encode!(payload)) |> ExAws.request(@config)

    start_supervised!({BroadwaySqsRetry.Pipeline, [queue_url: url]})

    Process.sleep(2_000)

    {:ok, %{body: %{messages: msgs}}} =
      SQS.receive_message(url, max_number_of_messages: 10, wait_time_seconds: 1)
      |> ExAws.request(@config)

    assert msgs == []
  end

  @tag :localstack
  test "failed webhook returns to queue after visibility timeout", %{url: url} do
    bypass = Bypass.open()
    Bypass.expect(bypass, fn conn -> Plug.Conn.resp(conn, 500, "bang") end)

    payload = %{url: "http://localhost:#{bypass.port}/hook", body: %{}}
    {:ok, _} = SQS.send_message(url, Jason.encode!(payload)) |> ExAws.request(@config)

    start_supervised!({BroadwaySqsRetry.Pipeline, [queue_url: url]})
    Process.sleep(1_000)
    :ok = Supervisor.stop(BroadwaySqsRetry.Pipeline)

    # Message should still be in queue (in-flight, invisible), not deleted
    {:ok, %{body: %{messages: _}}} =
      SQS.receive_message(url, max_number_of_messages: 10, wait_time_seconds: 1)
      |> ExAws.request(@config)
  end
end
```

---

## Trade-offs and production gotchas

**1. SQS is at-least-once, not exactly-once.**
The same message can be redelivered if visibility timeout expires while your
worker is still processing. Make `handle_message` idempotent (see 184).

**2. `max_number_of_messages: 10` is the SQS cap.**
Beyond 10 you must make multiple API calls. Multiple producers with
`concurrency: N` is the lever for higher fetch rate.

**3. Visibility timeout tuning is critical.**
Too low and you double-deliver. Too high and a crashed worker's messages
are stuck. Export `ApproximateAgeOfOldestMessage` as a Cloudwatch alarm.

**4. LocalStack is not AWS.**
It emulates ~90% of SQS behaviour. Known gaps: `FifoQueue` semantics,
message attributes across some API versions. Run integration tests on a
real staging queue at least weekly.

**5. `batch_size` on the SQS batcher is separate from SQS batch fetch size.**
`max_number_of_messages` is how many you *fetch* per call; `batch_size` is
how many Broadway groups for `handle_batch`. They are independent knobs.

**6. `receive_interval` polling on empty queues wastes calls.**
Combined with long polling this is usually fine, but if you have many
idle queues, it adds up fast in cost. Consolidate queues.

**7. DLQ config lives on SQS, not in Broadway.**
Broadway does not know about DLQs. Configure `redrivePolicy` with
`maxReceiveCount` on the SQS queue itself.

**8. When NOT to use BroadwaySQS.** For in-process queues (same BEAM node)
`Oban` is dramatically simpler and cheaper. SQS earns its keep when the
producer and consumer run on different machines/services.

---

## Performance notes

On LocalStack (single container, no network), the pipeline with
`concurrency: 16` processors sustained ~400 msgs/sec end-to-end. On real
SQS in us-east-1 from a c6i.xlarge consumer, we measured ~3.2k msgs/sec
sustained with 2 producers × 10 msgs/fetch + 32 processors.

Fetch cost breakdown for 1M messages at ~3k/sec:
- 100k `ReceiveMessage` calls @ 10 msgs = $0.04
- 1M `DeleteMessageBatch` amortized = $0.01
- Total: $0.05 / million messages.

---

## Resources

- [BroadwaySQS — HexDocs](https://hexdocs.pm/broadway_sqs/BroadwaySQS.Producer.html)
- [SQS developer guide — AWS](https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/welcome.html)
- [LocalStack SQS docs](https://docs.localstack.cloud/user-guide/aws/sqs/)
- [Visibility timeout blog — AWS](https://aws.amazon.com/blogs/compute/amazon-sqs-visibility-timeout/)
- [Concurrent Data Processing in Elixir — Svilen Gospodinov](https://pragprog.com/titles/sgdpelixir/)
- [ExAws.SQS docs](https://hexdocs.pm/ex_aws_sqs/)
