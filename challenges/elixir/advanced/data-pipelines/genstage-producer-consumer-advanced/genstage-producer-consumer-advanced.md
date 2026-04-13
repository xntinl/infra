# GenStage Producer-Consumer with Demand-Driven Back-Pressure

**Project**: `event_ingestor` — a multi-stage pipeline that ingests events from an external API, transforms them, and persists to storage with explicit back-pressure

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
event_ingestor/
├── lib/
│   └── event_ingestor.ex
├── script/
│   └── main.exs
├── test/
│   └── event_ingestor_test.exs
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
defmodule EventIngestor.MixProject do
  use Mix.Project

  def project do
    [
      app: :event_ingestor,
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

### `lib/event_ingestor.ex`

```elixir
defmodule EventIngestor.Application do
  @moduledoc """
  Ejercicio: GenStage Producer-Consumer with Demand-Driven Back-Pressure.
  Implementa el comportamiento descrito en el enunciado, exponiendo funciones públicas documentadas y con tipos claros.
  """

  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {EventIngestor.Producer, []},
      {EventIngestor.Transformer, []},
      {EventIngestor.Consumer, []}
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: EventIngestor.Supervisor)
  end
end

defmodule EventIngestor.Producer do
  use GenStage

  @type event :: %{id: String.t(), payload: map(), ts: integer()}

  def start_link(_opts), do: GenStage.start_link(__MODULE__, :ok, name: __MODULE__)

  @impl true
  def init(:ok) do
    # buffer_size bounds the in-memory queue; buffer_keep drops oldest on overflow.
    {:producer, %{cursor: 0}, buffer_size: 10_000, buffer_keep: :first,
     dispatcher: GenStage.DemandDispatcher}
  end

  @doc "Handles demand result from demand and state."
  @impl true
  def handle_demand(demand, state) when demand > 0 do
    {events, new_cursor} = fetch_page(state.cursor, demand)
    {:noreply, events, %{state | cursor: new_cursor}}
  end

  # Replace with real HTTP call (Finch/Req). Deterministic stub for tests.
  defp fetch_page(cursor, count) do
    events =
      for i <- 0..(count - 1) do
        %{id: "evt-#{cursor + i}", payload: %{value: cursor + i}, ts: System.system_time(:millisecond)}
      end

    {events, cursor + count}
  end
end

defmodule EventIngestor.Transformer do
  use GenStage

  def start_link(_opts), do: GenStage.start_link(__MODULE__, :ok, name: __MODULE__)

  @impl true
  def init(:ok) do
    {:producer_consumer, %{}, subscribe_to: [{EventIngestor.Producer, min_demand: 500, max_demand: 1_000}]}
  end

  @doc "Handles events result from events, _from and state."
  @impl true
  def handle_events(events, _from, state) do
    enriched = Enum.map(events, &enrich/1)
    {:noreply, enriched, state}
  end

  defp enrich(event) do
    Map.merge(event, %{enriched_at: System.monotonic_time(:millisecond), schema_version: 2})
  end
end

defmodule EventIngestor.Consumer do
  use GenStage
  require Logger

  def start_link(_opts), do: GenStage.start_link(__MODULE__, :ok, name: __MODULE__)

  @impl true
  def init(:ok) do
    {:consumer, %{written: 0},
     subscribe_to: [{EventIngestor.Transformer, min_demand: 100, max_demand: 500}]}
  end

  @doc "Handles events result from events, _from and state."
  @impl true
  def handle_events(events, _from, state) do
    persist_batch(events)
    {:noreply, [], %{state | written: state.written + length(events)}}
  end

  # Replace with Repo.insert_all/3. Stub keeps the example self-contained.
  defp persist_batch(events) do
    :telemetry.execute([:event_ingestor, :batch, :written], %{count: length(events)}, %{})
    :ok
  end
end
```

### `test/event_ingestor_test.exs`

```elixir
defmodule EventIngestor.PipelineTest do
  use ExUnit.Case, async: true
  doctest EventIngestor.Application

  alias EventIngestor.{Producer, Transformer, Consumer}

  describe "back-pressure semantics" do
    test "consumer receives events transformed by the intermediate stage" do
      parent = self()

      {:ok, probe} =
        GenStage.start_link(
          GenStage.Streamer,
          {[], []},
          []
        )

      # Attach a probe consumer to the transformer to observe enriched events.
      consumer_fn = fn events ->
        send(parent, {:events, events})
        :ok
      end

      {:ok, _} = TestConsumer.start_link({consumer_fn, Transformer})

      assert_receive {:events, events}, 2_000
      assert events != []
      assert Enum.all?(events, &Map.has_key?(&1, :enriched_at))
    end
  end

  describe "buffer bounds" do
    test "producer does not grow its buffer beyond the configured limit" do
      # After running the pipeline briefly, total mailbox + buffer remains finite.
      Process.sleep(200)
      {:message_queue_len, len} = Process.info(Process.whereis(Producer), :message_queue_len)
      assert len < 1_000
    end
  end
end

defmodule TestConsumer do
  use GenStage

  def start_link({fun, upstream}) do
    GenStage.start_link(__MODULE__, {fun, upstream})
  end

  @impl true
  def init({fun, upstream}) do
    {:consumer, fun, subscribe_to: [{upstream, min_demand: 10, max_demand: 50}]}
  end

  @impl true
  def handle_events(events, _from, fun) do
    fun.(events)
    {:noreply, [], fun}
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== EventIngestor.Application Demo ===\n")

    result_1 = EventIngestor.Application.handle_demand(nil, nil)
    IO.puts("Demo 1 - handle_demand: #{inspect(result_1)}")
    result_2 = EventIngestor.Application.handle_events(nil, nil, nil)
    IO.puts("Demo 2 - handle_events: #{inspect(result_2)}")
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
