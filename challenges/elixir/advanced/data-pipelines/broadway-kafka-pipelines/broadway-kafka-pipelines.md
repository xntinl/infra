# Broadway + Kafka — Consumer Groups, Concurrency and Telemetry

**Project**: `broadway_kafka` — a production-grade Kafka consumer pipeline with partition-aware concurrency, manual offset commits, and telemetry-driven autoscaling signals

---

## Why data pipelines matters

GenStage, Flow, and Broadway make back-pressured concurrent data processing a first-class concern. Producers, consumers, dispatchers, and batchers compose into pipelines that absorb bursts without exhausting memory.

The hard problems are exactly-once semantics, checkpointing for resumability, and tuning batcher concurrency against downstream latency. A pipeline that works at 10 events/sec often collapses at 10k unless these concerns were designed in from the start.

---

## The business problem

You are building a production-grade Elixir component in the **Data pipelines** area. The goal is not a toy demonstration — it is a piece you could drop into a real system and rely on under load.

Concretely, you must:

1. Implement the topic correctly with measurable behavior.
2. Surface failure modes via `{:ok, _} / {:error, reason}` tuples — never raise from a public API except for programmer errors.
3. Bound resources: explicit timeouts, memory caps, back-pressure where relevant, and supervision for any long-lived process.
4. Provide a test suite with `async: true` where safe, `doctest` for public examples, and `describe` blocks that name the contract.
5. Provide a `script/main.exs` that demonstrates the piece end-to-end with realistic data — not a `1 + 1` smoke test.

---

## Project structure

```
broadway_kafka/
├── lib/
│   └── broadway_kafka.ex
├── script/
│   └── main.exs
├── test/
│   └── broadway_kafka_test.exs
└── mix.exs
```

---

## Design decisions

**Option A — minimal happy-path implementation**
- Pros: smaller surface area, faster to ship.
- Cons: no resource bounds, no failure-mode coverage, no observability hooks. Falls over the first time production load deviates from the developer's mental model.

**Option B — production-grade contract with explicit bounds** (chosen)
- Pros: timeouts, supervised lifecycle, structured errors, idiomatic `{:ok, _} / {:error, reason}` returns. Tests cover the failure envelope, not just the happy path.
- Cons: more code, more concepts. Pays for itself the first time the upstream service degrades.

Chose **B** because in Data pipelines the failure modes — not the happy path — are what determine whether the system stays up under real production conditions.

---

## Implementation

### `mix.exs`

```elixir
defmodule BroadwayKafka.MixProject do
  use Mix.Project

  def project do
    [
      app: :broadway_kafka,
      version: "0.1.0",
      elixir: "~> 1.19",
      elixirc_paths: elixirc_paths(Mix.env()),
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp elixirc_paths(:test), do: ["lib", "test/support"]
  defp elixirc_paths(_), do: ["lib"]

  defp deps do
    [
      {:ex_doc, "~> 0.31", only: :dev, runtime: false}
    ]
  end
end
```
### `lib/broadway_kafka.ex`

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

  @doc "Handles message result from _processor and _ctx."
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

  @doc "Handles batch result from messages, _batch_info and _ctx."
  @impl true
  def handle_batch(:warehouse, messages, _batch_info, _ctx) do
    rows = Enum.map(messages, & &1.data)

    case Warehouse.upsert(rows) do
      :ok -> messages
      {:error, reason} -> Enum.map(messages, &Message.failed(&1, reason))
    end
  end
end

defmodule BroadwayKafka.Normaliser do
  @doc "Returns normalise result from bin."
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
  @doc "Returns upsert result from _rows."
  @spec upsert([map()]) :: :ok | {:error, term()}
  def upsert(_rows), do: :ok
end

defmodule BroadwayKafka.Telemetry do
  require Logger

  @doc "Returns attach result."
  @spec attach() :: :ok
  def attach do
    :telemetry.attach_many(
      "kafka-pipeline-telemetry",
      [
        [:broadway, :batcher, :stop],
        [:broadway, :processor, :message, :exception],
        [:broadway_kafka, :assignments, :received]
      ],
      &process_request/4,
      nil
    )
  end

  @doc "Handles result from meas, meta and _."
  def process_request([:broadway, :batcher, :stop], meas, meta, _) do
    Logger.info(
      "batch flushed batcher=#{meta.batcher} size=#{length(meta.messages)} ms=#{div(meas.duration, 1_000_000)}"
    )
  end

  @doc "Handles result from _meas, meta and _."
  def process_request([:broadway, :processor, :message, :exception], _meas, meta, _) do
    Logger.error("processor exception kind=#{meta.kind} reason=#{inspect(meta.reason)}")
  end

  @doc "Handles result from _meas, meta and _."
  def process_request([:broadway_kafka, :assignments, :received], _meas, meta, _) do
    Logger.info("kafka assignments received: #{inspect(meta.assignments)}")
  end
end
```
### `test/broadway_kafka_test.exs`

```elixir
defmodule BroadwayKafka.PipelineTest do
  use ExUnit.Case, async: true
  doctest BroadwayKafka.Pipeline

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

  describe "BroadwayKafka.Pipeline" do
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
end
```
### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== BroadwayKafka.Pipeline Demo ===\n")

    result_1 = BroadwayKafka.Pipeline.handle_message(nil, nil, nil)
    IO.puts("Demo 1 - handle_message: #{inspect(result_1)}")
    result_2 = BroadwayKafka.Pipeline.handle_batch(nil, nil, nil, nil)
    IO.puts("Demo 2 - handle_batch: #{inspect(result_2)}")
    result_3 = BroadwayKafka.Pipeline.normalise(nil)
    IO.puts("Demo 3 - normalise: #{inspect(result_3)}")
  end
end

Main.main()
```
---

## Trade-offs and production gotchas

**1. Bounded resources are the contract**
Any operation that can grow without bound (mailbox, ETS table, process count, DB connections) must have an explicit cap. Without one, a bad input or a slow upstream eventually exhausts the VM.

**2. Tagged tuples over exceptions**
Public APIs return `{:ok, value} / {:error, reason}`. Exceptions are reserved for programmer errors (FunctionClauseError, KeyError) — operational errors are data, not control flow.

**3. Timeouts are not optional**
`GenServer.call` defaults to 5000 ms. `Task.await` defaults to 5000 ms. `Repo.transaction` inherits the connection's timeout. In production, every call should pass an explicit `:timeout` matched to the operation's SLA.

**4. Supervision encodes recovery**
Long-lived processes belong under a Supervisor with an explicit restart strategy. Choose `:transient` for processes that should not restart on `:normal` exit, `:permanent` for daemons, `:temporary` for workers that handle one-off jobs.

**5. Telemetry events are the production debugger**
Emit `:telemetry.execute/3` for every business operation. Attach handlers in production for metrics; attach handlers in tests for assertions. The same code path serves both.

**6. Async tests need isolated state**
`async: true` parallelizes test execution. Any test that writes to global state (Application env, named ETS tables, the database without sandbox) must declare `async: false` or the suite becomes flaky.

---

## Reflection

1. If load on this component grew by 100×, which assumption breaks first — the data structure, the process model, or the failure handling? Justify with a measurement plan, not a guess.
2. What three telemetry events would you emit to decide, six months from now, whether this implementation is still the right one? Name the events, the metadata, and the alert thresholds.
3. The implementation here uses the recommended primitive. Under what specific conditions would you reach for a different primitive instead? Be concrete about the trigger.

---

## Key concepts

### 1. Demand drives back-pressure

GenStage's pull model means slow consumers don't drown fast producers. Producers ask 'give me N events when you have them' rather than producers shoving events downstream.

### 2. Batchers trade latency for throughput

Broadway batchers accumulate events before flushing. A batch size of 100 with a 1-second timeout balances throughput against latency — tune both axes.

### 3. Idempotency is not optional

At-least-once delivery is the default in distributed pipelines. Exactly-once requires idempotent processing, deduplication keys, and durable checkpoints.

---
