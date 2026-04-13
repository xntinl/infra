# Basic Broadway consumer with RabbitMQ and batching

**Project**: `rabbit_worker` — a `Broadway` pipeline that consumes messages
from a RabbitMQ queue via
[`BroadwayRabbitMQ.Producer`](https://hexdocs.pm/broadway_rabbitmq/),
processes them concurrently, batches them for a downstream sink (e.g., a
database insert), and acknowledges with at-least-once semantics. Tests use
Broadway's in-process test helpers — no real broker needed.

---

## Project context

Message queues are how production systems decouple. A web process enqueues
"send welcome email"; a consumer process picks it up asynchronously. The
naive consumer — one process pulling one message at a time — works until
you hit a few hundred messages per second and need backpressure, batching,
retries, and graceful shutdown.

[`Broadway`](https://hexdocs.pm/broadway/) (by Dashbit) handles all of
that. It sits on top of `GenStage`, takes care of demand-driven flow
control, and has first-class producers for SQS, Kafka, Google PubSub, and
RabbitMQ. The RabbitMQ producer supports ack/reject/requeue strategies out
of the box.

This exercise builds the skeleton: a producer pulling from a queue,
processors running work, batchers grouping by key for bulk downstream
writes. Tests use `Broadway.test_message/3` which bypasses the real
producer entirely — perfect for unit coverage.

Project structure:

```
rabbit_worker/
├── lib/
│   ├── rabbit_worker/
│   │   ├── application.ex
│   │   └── pipeline.ex
│   └── rabbit_worker.ex
├── test/
│   └── pipeline_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `use Broadway` — the three stages

```
Producer(s)  →  Processor(s)  →  Batcher(s) → Batch processor(s)
```

- **Producer**: pulls messages from the source (RabbitMQ here).
- **Processor**: `handle_message/3` runs per message, concurrently.
- **Batcher**: groups messages by `:batcher` key.
- **Batch processor**: `handle_batch/4` runs on a batch (flush to DB, etc.).

### 2. Acks — `on_success` and `on_failure`

`BroadwayRabbitMQ.Producer` options:

- `on_success: :ack` (default) — RabbitMQ marks delivered; no redelivery.
- `on_failure: :reject_and_requeue` — at-least-once semantics; failed
  messages go back to the queue (beware infinite retry loops — pair with
  a dead-letter exchange).

Broadway automatically acks on return of `handle_message/3` unless you
call `Broadway.Message.failed/2`.

### 3. Backpressure via `prefetch_count`

RabbitMQ QoS `prefetch_count` limits unacked messages per consumer. The
official guidance: set it to at least `max_demand × processor_count`.
Default is 50. Too low throttles you; too high defeats backpressure.

### 4. Testing with `Broadway.test_message/3`

```elixir
ref = Broadway.test_message(MyPipeline, "payload")
assert_receive {:ack, ^ref, [_successful], []}, 1_000
```

The test injects a message directly into the pipeline, bypassing the
producer. No broker required.

---

## Design decisions

**Option A — plain `AMQP.Basic.consume/3` with a single-process consumer**
- Pros: no extra dep; straightforward for a one-shot drain; you see every wire frame.
- Cons: you re-implement backpressure, batching, graceful shutdown, and retry semantics by hand; sharing work across processors becomes your problem; testing requires a live broker.

**Option B — `Broadway` with `BroadwayRabbitMQ.Producer`, processors, and batchers (chosen)**
- Pros: demand-driven flow control via GenStage; per-stage concurrency knobs; `handle_batch/4` amortises downstream writes; `Broadway.test_message/3` enables tests without a broker; graceful shutdown handled for you.
- Cons: larger mental model; you must size `prefetch_count` against `max_demand × processor_concurrency` or the pipeline stalls; poisoned messages with `:reject_and_requeue` need a DLX or you have an infinite loop.

→ Chose **B** because anything beyond a one-script drain eventually needs backpressure and batching, and Broadway is the idiomatic Elixir answer.

## Implementation

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Standard library: no external dependencies required
    {:"jason", "~> 1.0"},
  ]
end
```


### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — isolated from any external state, so we demonstrate this concept cleanly without dependencies.


```bash
mix new rabbit_worker --sup
cd rabbit_worker
```

Deps in `mix.exs`:

```elixir
defp deps do
  [
    {:broadway, "~> 1.1"},
    {:broadway_rabbitmq, "~> 0.8"},
    {:jason, "~> 1.4"}
  ]
end
```

### Step 2: `lib/rabbit_worker/application.ex`

**Objective**: Wire `application.ex` to start the supervision tree that boots Repo and external adapters in the correct order before serving traffic.


```elixir
defmodule RabbitWorker.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    # In real deployments, read connection params from env / Config.
    # We skip starting the pipeline in :test — tests start it per test.
    children =
      if Application.get_env(:rabbit_worker, :start_pipeline?, true) do
        [RabbitWorker.Pipeline]
      else
        []
      end

    Supervisor.start_link(children,
      strategy: :one_for_one,
      name: RabbitWorker.Supervisor
    )
  end
end
```

### Step 3: `lib/rabbit_worker/pipeline.ex`

**Objective**: Implement `pipeline.ex` — the integration seam where external protocol semantics meet Elixir domain code.


```elixir
defmodule RabbitWorker.Pipeline do
  @moduledoc """
  Broadway pipeline consuming JSON messages from a RabbitMQ queue.

  Messages are decoded in `handle_message/3`, routed to a batcher
  (`:default`), and flushed in batches of 100 (or after 2 seconds) via
  `handle_batch/4`, where a real app would do a bulk DB insert or a
  downstream API call.

  Failures trigger `:reject_and_requeue` — pair this with a dead-letter
  exchange in RabbitMQ to avoid infinite requeue loops.
  """

  use Broadway

  alias Broadway.Message

  @spec start_link(keyword()) :: {:ok, pid()} | {:error, term()}
  def start_link(opts \\ []) do
    producer = Keyword.get(opts, :producer, default_producer())

    Broadway.start_link(__MODULE__,
      name: __MODULE__,
      producer: [module: producer, concurrency: 1],
      processors: [default: [concurrency: 10, max_demand: 10]],
      batchers: [
        default: [concurrency: 2, batch_size: 100, batch_timeout: 2_000]
      ]
    )
  end

  defp default_producer do
    {BroadwayRabbitMQ.Producer,
     queue: "rabbit_worker.jobs",
     connection: [host: "localhost"],
     qos: [prefetch_count: 100],
     on_failure: :reject_and_requeue}
  end

  # ── Callbacks ───────────────────────────────────────────────────────────

  @impl true
  def handle_message(_processor, %Message{data: data} = msg, _context) do
    case decode(data) do
      {:ok, payload} ->
        msg
        |> Message.update_data(fn _ -> payload end)
        |> Message.put_batcher(:default)

      {:error, reason} ->
        Message.failed(msg, reason)
    end
  end

  @impl true
  def handle_batch(:default, messages, _batch_info, _context) do
    # In production: a single Repo.insert_all, or a bulk HTTP push.
    # Here we just log and mark all successful.
    payloads = Enum.map(messages, & &1.data)
    :ok = RabbitWorker.Sink.flush(payloads)

    messages
  end

  # ── Helpers ─────────────────────────────────────────────────────────────

  defp decode(data) when is_binary(data) do
    case Jason.decode(data) do
      {:ok, map} -> {:ok, map}
      {:error, _} = err -> err
    end
  end
end
```

### Step 4: `lib/rabbit_worker.ex` — the sink

**Objective**: Edit `rabbit_worker.ex` — the sink, exposing the integration seam where external protocol semantics meet Elixir domain code.


```elixir
defmodule RabbitWorker.Sink do
  @moduledoc """
  Stand-in for the downstream side effect. Tests replace this with a stub
  that sends to a test PID.
  """

  @callback flush([map()]) :: :ok

  def flush(payloads) do
    # In real code: Repo.insert_all, Httpoison POST, etc.
    # Swappable via application env for tests.
    impl = Application.get_env(:rabbit_worker, :sink, __MODULE__.Default)
    impl.flush(payloads)
  end

  defmodule Default do
    @behaviour RabbitWorker.Sink

    @impl true
    def flush(payloads) do
      IO.inspect(length(payloads), label: "flushed")
      :ok
    end
  end
end
```

### Step 5: `test/pipeline_test.exs`

**Objective**: Write `pipeline_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule RabbitWorker.PipelineTest do
  use ExUnit.Case, async: false

  defmodule TestSink do
    @behaviour RabbitWorker.Sink

    @impl true
    def flush(payloads) do
      send(:test_runner, {:flushed, payloads})
      :ok
    end
  end

  setup do
    Process.register(self(), :test_runner)
    Application.put_env(:rabbit_worker, :sink, TestSink)

    # Start the pipeline with the Broadway dummy producer so we can
    # inject messages via Broadway.test_message/3.
    {:ok, pid} =
      Broadway.start_link(RabbitWorker.Pipeline,
        name: :"TestPipeline#{System.unique_integer([:positive])}",
        producer: [module: {Broadway.DummyProducer, []}],
        processors: [default: [concurrency: 2]],
        batchers: [default: [concurrency: 1, batch_size: 2, batch_timeout: 100]]
      )

    on_exit(fn ->
      if Process.alive?(pid), do: GenServer.stop(pid)
      Application.delete_env(:rabbit_worker, :sink)
    end)

    %{pipeline: pid}
  end

  describe "happy path" do
    test "decodes JSON, batches, and flushes", %{pipeline: pipeline} do
      name = Broadway.producer_names(pipeline) |> hd() |> elem(0)
      # Easier: use Broadway.test_message/3 with the registered name
      pipeline_name = GenServer.call(pipeline, :get_name)
      _ = name
      _ = pipeline_name

      ref1 = Broadway.test_message(pipeline, Jason.encode!(%{"id" => 1}))
      ref2 = Broadway.test_message(pipeline, Jason.encode!(%{"id" => 2}))

      assert_receive {:ack, ^ref1, [_], []}, 1_000
      assert_receive {:ack, ^ref2, [_], []}, 1_000
      assert_receive {:flushed, [%{"id" => _}, %{"id" => _}]}, 1_000
    end
  end

  describe "failure path" do
    test "malformed JSON marks message as failed", %{pipeline: pipeline} do
      ref = Broadway.test_message(pipeline, "{not json")

      assert_receive {:ack, ^ref, [], [_failed]}, 1_000
    end
  end
end
```

> Note: depending on your Broadway version, `Broadway.test_message/3`
> returns a reference you pattern-match on in the ack tuple. Pipelines
> started directly (not via the `use Broadway` `start_link/1`) expose
> their PID; `Broadway.test_message/3` accepts either PID or registered
> name.

Run:

```bash
mix deps.get
mix test
```

---

## Key Concepts

Broadway is a stream processing library for message queues (RabbitMQ, Kafka, Kinesis). You define producers (sources), processors (transformations), and consumers (sinks). Broadway handles concurrency, batching, and graceful shutdown automatically. This is how you build resilient data pipelines: define stages, let Broadway orchestrate parallelism. The trade-off: Broadway is opinionated; if your pipeline is nonstandard, you may need lower-level libraries. For message-queue-driven workflows, Broadway is the right abstraction—it abstracts away the complexity of flow control, acknowledgment, and backpressure.

---

## Deep Dive: Demand-Driven Backpressure and Message Batching

Broadway sits on GenStage, implementing demand-driven flow control: processors only request messages when ready. This prevents unbounded mailbox growth and naturally throttles fast producers against slower consumers. Batching (grouping N messages or waiting T milliseconds) amortizes expensive downstream operations: a single database insert of 100 records beats 100 individual inserts.

The `:prefetch_count` is RabbitMQ's backpressure mechanism—it limits unacked messages the broker delivers. Set too low and you starve processors; too high and you lose backpressure. Safe formula: `prefetch_count ≥ max_demand × processor_concurrency`. In production, monitor stage demand and adjust concurrency and batch size based on downstream latency (database time, API response time).

Graceful shutdown is critical: Broadway drains in-flight messages before terminating. In Kubernetes, ensure `terminationGracePeriodSeconds ≥ expected_shutdown_time`. Without it, pods are force-killed mid-message, causing redelivery storms when the next pod starts. Always pair backpressure monitoring with load testing to find optimal configuration.

## Trade-offs and production gotchas

**1. `reject_and_requeue` without a dead-letter exchange is a time bomb**
A poisoned message fails forever, gets requeued forever, and burns CPU.
Always configure a DLX in RabbitMQ (`x-dead-letter-exchange` queue arg)
or use `:reject_and_requeue_once` to requeue only the first time.

**2. `prefetch_count` and `max_demand` must be sized together**
Rule: `prefetch_count ≥ max_demand × processors.concurrency`. Under this,
Broadway stalls waiting for messages it's not allowed to fetch.

**3. Batches are flushed on `batch_size` *or* `batch_timeout`**
Low-traffic periods still flush on the timeout. Don't set
`batch_timeout` to `:infinity` unless you really mean "only flush when
full" — during quiet times messages sit unacked, and RabbitMQ may close
the channel.

**4. `handle_batch/4` runs in its own process**
It does not share state with `handle_message/3`. Context comes via the
`:context` option on `Broadway.start_link/2`, not via process dict or
globals.

**5. Graceful shutdown takes `:shutdown` × producer+processor time**
Broadway drains in-flight messages before shutting down. Default
`:shutdown` is 30s. On `kubectl delete`, give the pod at least this long
via `terminationGracePeriodSeconds`.

**6. When NOT to use Broadway**
- One-off scripts that drain a queue once: plain `AMQP.Basic.consume` is
  simpler.
- Non-queue event sources (like DB polling): write a plain GenStage
  producer; Broadway optimizes for pub-sub/queue semantics.
- If your throughput is <10 msg/s, the complexity isn't worth it.

---

## Benchmark

<!-- benchmark N/A: integration/configuration exercise -->

## Reflection

- `on_failure: :reject_and_requeue` is at-least-once, but without a dead-letter exchange a genuinely un-processable message loops forever. If you had to pick between `:reject_and_requeue_once` (simpler, caps the damage) and a DLX (harder, preserves the message for inspection), what operational signal would push you to one versus the other?

## Resources

- [Broadway on HexDocs](https://hexdocs.pm/broadway/Broadway.html)
- [BroadwayRabbitMQ.Producer](https://hexdocs.pm/broadway_rabbitmq/BroadwayRabbitMQ.Producer.html)
- [Broadway testing guide](https://hexdocs.pm/broadway/testing.html) — `test_message/3`, dummy producers
- [RabbitMQ Dead-Letter Exchanges](https://www.rabbitmq.com/docs/dlx)
- [`:amqp` client](https://hexdocs.pm/amqp/) — the lower-level lib Broadway uses
