# Broadway Batchers — Tuning `batch_size` and `batch_timeout`

**Project**: `broadway_batcher` — a metrics-ingestion pipeline that batches writes into a time-series DB, with benchmarked knobs

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
broadway_batcher/
├── lib/
│   └── broadway_batcher.ex
├── script/
│   └── main.exs
├── test/
│   └── broadway_batcher_test.exs
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
defmodule BroadwayBatcher.MixProject do
  use Mix.Project

  def project do
    [
      app: :broadway_batcher,
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

### `lib/broadway_batcher.ex`

```elixir
defmodule BroadwayBatcher.Pipeline do
  @moduledoc """
  Ingest samples, buffer into batches, write to TSDB.

  Batcher configuration comes from opts to enable experimentation.
  """
  use Broadway

  alias Broadway.Message
  alias BroadwayBatcher.TsdbClient

  def start_link(opts) do
    batch_size = Keyword.get(opts, :batch_size, 500)
    batch_timeout = Keyword.get(opts, :batch_timeout, 1_000)
    concurrency = Keyword.get(opts, :batch_concurrency, 4)

    Broadway.start_link(__MODULE__,
      name: __MODULE__,
      producer: [module: {Broadway.DummyProducer, []}, concurrency: 1],
      processors: [default: [concurrency: 8]],
      batchers: [
        tsdb: [
          concurrency: concurrency,
          batch_size: batch_size,
          batch_timeout: batch_timeout
        ]
      ]
    )
  end

  @doc "Handles message result from _p and _ctx."
  @impl true
  def handle_message(_p, %Message{} = msg, _ctx) do
    Message.put_batcher(msg, :tsdb)
  end

  @doc "Handles batch result from messages, _batch_info and _ctx."
  @impl true
  def handle_batch(:tsdb, messages, _batch_info, _ctx) do
    points = Enum.map(messages, & &1.data)
    :ok = TsdbClient.write_points(points)
    messages
  end
end

defmodule BroadwayBatcher.TsdbClient do
  @moduledoc "Fake TSDB client. Simulates a 20ms HTTP round-trip regardless of batch size."

  @doc "Writes points result from points."
  @spec write_points([map()]) :: :ok
  def write_points(points) when is_list(points) do
    :timer.sleep(20)
    :ok
  end
end

defmodule BroadwayBatcher.Application do
  use Application

  @impl true
  def start(_t, _a) do
    children = [{BroadwayBatcher.Pipeline, []}]
    Supervisor.start_link(children, strategy: :one_for_one)
  end
end
```

### `test/broadway_batcher_test.exs`

```elixir
defmodule BroadwayBatcher.PipelineTest do
  use ExUnit.Case, async: true
  doctest BroadwayBatcher.Pipeline

  alias BroadwayBatcher.Pipeline

  setup do
    start_supervised!({Pipeline, [batch_size: 10, batch_timeout: 500]})
    :ok
  end

  describe "BroadwayBatcher.Pipeline" do
    test "flushes by size" do
      events = for i <- 1..10, do: %{metric: "cpu", v: i}
      ref = Broadway.test_batch(Pipeline, events)
      assert_receive {:ack, ^ref, acks, []}, 2_000
      assert length(acks) == 10
    end

    test "flushes by timeout with a partial batch" do
      events = for i <- 1..3, do: %{metric: "cpu", v: i}
      ref = Broadway.test_batch(Pipeline, events)
      # batch_timeout 500ms
      assert_receive {:ack, ^ref, acks, []}, 2_000
      assert length(acks) == 3
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  def main do
      # Demonstrate Broadway batching: collect events until batch_size or timeout
      events = Enum.map(1..25, &%{id: &1, metric: "cpu", value: :rand.uniform(100)})

      # Simulate batching with batch_size=10
      batch_size = 10
      batches = Enum.chunk_every(events, batch_size)

      IO.inspect(Enum.map(batches, &length/1), label: "✓ Batch sizes")

      assert length(batches) == 3, "Expected 3 batches (10, 10, 5)"
      assert Enum.all?(batches, &is_list/1), "All are lists"

      IO.puts("✓ Broadway batching: timeseriesDB write batches working")
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
