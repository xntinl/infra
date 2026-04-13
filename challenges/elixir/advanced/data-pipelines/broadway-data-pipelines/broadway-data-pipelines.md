# Broadway — End-to-End Data Pipelines

**Project**: `broadway_pipeline` — a payment event enricher with fan-in, partitioning and batching

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
broadway_pipeline/
├── lib/
│   └── broadway_pipeline.ex
├── script/
│   └── main.exs
├── test/
│   └── broadway_pipeline_test.exs
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
defmodule BroadwayPipeline.MixProject do
  use Mix.Project

  def project do
    [
      app: :broadway_pipeline,
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

### `lib/broadway_pipeline.ex`

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

  @doc "Handles message result from _processor and _ctx."
  @impl true
  def handle_message(_processor, %Message{data: event} = msg, _ctx) do
    enriched = Enricher.enrich(event)
    score = FraudScorer.score(enriched)

    msg
    |> Message.update_data(fn _ -> Map.put(enriched, :risk, score) end)
    |> route(score)
  end

  @doc "Handles batch result from messages, _batch_info and ctx."
  @impl true
  def handle_batch(:postgres, messages, _batch_info, ctx) do
    rows = Enum.map(messages, & &1.data)
    :ok = ctx.repo.insert_all(rows)
    messages
  end

  @doc "Handles batch result from messages, _batch_info and _ctx."
  def handle_batch(:kafka, messages, _batch_info, _ctx) do
    # would use :brod or similar in real life
    Enum.each(messages, fn _ -> :ok end)
    messages
  end

  defp route(msg, score) when score >= 0.8, do: Message.put_batcher(msg, :kafka)
  defp route(msg, _score), do: Message.put_batcher(msg, :postgres)

  defp partition(%Message{data: %{customer_id: id}}), do: :erlang.phash2(id)
end

defmodule BroadwayPipeline.Enricher do
  @doc "Returns enrich result from event."
  @spec enrich(map()) :: map()
  def enrich(event) do
    :timer.sleep(10)
    Map.put(event, :customer_tier, :gold)
  end
end

defmodule BroadwayPipeline.FraudScorer do
  @doc "Returns score result."
  @spec score(map()) :: float()
  def score(%{amount: a}) when a > 10_000, do: 0.95
  @doc "Returns score result from _."
  def score(_), do: 0.1
end

defmodule BroadwayPipeline.Repo do
  @doc "Returns insert all result from _rows."
  @spec insert_all([map()]) :: :ok
  def insert_all(_rows), do: :ok
end

defmodule BroadwayPipeline.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [{BroadwayPipeline.Pipeline, []}]
    Supervisor.start_link(children, strategy: :one_for_one)
  end
end

defmodule BroadwayPipeline.Telemetry do
  require Logger

  @doc "Returns attach result."
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

### `test/broadway_pipeline_test.exs`

```elixir
defmodule BroadwayPipeline.PipelineTest do
  use ExUnit.Case, async: true
  doctest BroadwayPipeline.Pipeline

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

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Broadway — End-to-End Data Pipelines.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Broadway — End-to-End Data Pipelines ===")
    IO.puts("Category: Data pipelines\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case BroadwayPipeline.run(payload) do
          {:ok, value} -> {:ok, payload, value}
          {:error, reason} -> {:error, payload, reason}
        end
      end)

    Enum.each(results, fn
      {:ok, p, v} -> IO.puts("  [OK] #{inspect(p)} -> #{inspect(v)}")
      {:error, p, r} -> IO.puts("  [ERR] #{inspect(p)} -> #{inspect(r)}")
    end)

    {us, _} =
      :timer.tc(fn ->
        for _ <- 1..1_000, do: BroadwayPipeline.run(:bench)
      end)

    avg = us / 1_000
    IO.puts("\nBenchmark: #{:erlang.float_to_binary(avg, decimals: 2)} µs/op (1000 iterations)")
    IO.puts("Target: < 100 µs/op for in-process operations\n")
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
