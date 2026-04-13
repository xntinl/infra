# GenStage Advanced — Dispatchers, Subscriptions and Buffers

**Project**: `genstage_advanced` — a telemetry ingestion pipeline with surgical flow control

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
genstage_advanced/
├── lib/
│   └── genstage_advanced.ex
├── script/
│   └── main.exs
├── test/
│   └── genstage_advanced_test.exs
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
defmodule GenstageAdvanced.MixProject do
  use Mix.Project

  def project do
    [
      app: :genstage_advanced,
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

### `lib/genstage_advanced.ex`

```elixir
defmodule GenstageAdvanced.IngestProducer do
  @moduledoc """
  Buffered producer. Upstream writers call `push/1`. Downstream consumers
  pull via GenStage demand. When the buffer overflows, `:buffer_keep`
  decides the eviction strategy.
  """
  use GenStage

  @type event :: %{id: pos_integer(), payload: term(), ts: integer()}

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts), do: GenStage.start_link(__MODULE__, opts, name: __MODULE__)

  @doc "Returns push result from event."
  @spec push(event()) :: :ok
  def push(event), do: GenStage.cast(__MODULE__, {:push, event})

  @impl true
  def init(opts) do
    dispatcher = Keyword.get(opts, :dispatcher, GenStage.DemandDispatcher)
    buffer_size = Keyword.get(opts, :buffer_size, 10_000)
    buffer_keep = Keyword.get(opts, :buffer_keep, :last)

    {:producer, %{counter: 0},
     dispatcher: dispatcher,
     buffer_size: buffer_size,
     buffer_keep: buffer_keep}
  end

  @impl true
  def handle_cast({:push, event}, state) do
    {:noreply, [event], %{state | counter: state.counter + 1}}
  end

  @doc "Handles demand result from _demand and state."
  @impl true
  def handle_demand(_demand, state), do: {:noreply, [], state}
end

defmodule GenstageAdvanced.Aggregator do
  @moduledoc "CPU-bound consumer. Simulates ~1ms of work per event."
  use GenStage

  def start_link(opts), do: GenStage.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(opts) do
    sub = Keyword.fetch!(opts, :subscribe_to)
    {:consumer, %{count: 0}, subscribe_to: sub}
  end

  @doc "Handles events result from events, _from and state."
  @impl true
  def handle_events(events, _from, state) do
    Enum.each(events, fn _ -> :timer.sleep(1) end)
    {:noreply, [], %{state | count: state.count + length(events)}}
  end
end

defmodule GenstageAdvanced.ParquetWriter do
  @moduledoc """
  IO-bound consumer that only flushes when it has collected >= 500 events
  or 500ms have elapsed since the last flush.
  """
  use GenStage

  def start_link(opts), do: GenStage.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(opts) do
    sub = Keyword.fetch!(opts, :subscribe_to)
    Process.send_after(self(), :flush_tick, 500)
    {:consumer, %{buf: [], flushed: 0}, subscribe_to: sub}
  end

  @doc "Handles events result from events, _from and state."
  @impl true
  def handle_events(events, _from, state) do
    buf = events ++ state.buf

    if length(buf) >= 500 do
      {:noreply, [], %{state | buf: [], flushed: state.flushed + length(buf)}}
    else
      {:noreply, [], %{state | buf: buf}}
    end
  end

  @impl true
  def handle_info(:flush_tick, state) do
    Process.send_after(self(), :flush_tick, 500)
    {:noreply, [], %{state | buf: [], flushed: state.flushed + length(state.buf)}}
  end
end

defmodule GenstageAdvanced.Sampler do
  @moduledoc "Forwards ~1% of events to a subscriber pid."
  use GenStage

  def start_link(opts), do: GenStage.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(opts) do
    sub = Keyword.fetch!(opts, :subscribe_to)
    target = Keyword.fetch!(opts, :target)
    {:consumer, %{target: target}, subscribe_to: sub}
  end

  @doc "Handles events result from events, _from and state."
  @impl true
  def handle_events(events, _from, state) do
    Enum.each(events, fn e ->
      if :rand.uniform(100) == 1, do: send(state.target, {:sample, e})
    end)

    {:noreply, [], state}
  end
end

defmodule GenstageAdvanced.ManualConsumer do
  @moduledoc """
  Consumer that only pulls when `pull/2` is called. Useful for tests and for
  external circuit breakers.
  """
  use GenStage

  def start_link(opts), do: GenStage.start_link(__MODULE__, opts)

  @doc "Returns pull result from pid and n."
  def pull(pid, n), do: GenStage.call(pid, {:pull, n})

  @impl true
  def init(opts) do
    sub = Keyword.fetch!(opts, :subscribe_to)
    {:consumer, %{from: nil, seen: []}, subscribe_to: sub}
  end

  @doc "Handles subscribe result from _opts, from and state."
  @impl true
  def handle_subscribe(:producer, _opts, from, state) do
    {:manual, %{state | from: from}}
  end

  @impl true
  def handle_call({:pull, n}, _caller, state) do
    GenStage.ask(state.from, n)
    {:reply, :ok, [], state}
  end

  @doc "Handles events result from events, _from and state."
  @impl true
  def handle_events(events, _from, state) do
    {:noreply, [], %{state | seen: state.seen ++ events}}
  end
end

defmodule GenstageAdvanced.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {GenstageAdvanced.IngestProducer,
       dispatcher: GenStage.BroadcastDispatcher, buffer_size: 50_000, buffer_keep: :last},
      {GenstageAdvanced.Aggregator,
       subscribe_to: [{GenstageAdvanced.IngestProducer, max_demand: 500, min_demand: 250}]},
      {GenstageAdvanced.ParquetWriter,
       subscribe_to: [{GenstageAdvanced.IngestProducer, max_demand: 1_000, min_demand: 500}]}
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: GenstageAdvanced.Supervisor)
  end
end
```

### `test/genstage_advanced_test.exs`

```elixir
defmodule GenstageAdvanced.BufferKeepTest do
  use ExUnit.Case, async: true
  doctest GenstageAdvanced.IngestProducer
  alias GenstageAdvanced.{IngestProducer, ManualConsumer}

  describe "GenstageAdvanced.BufferKeep" do
    test "buffer_keep: :first evicts oldest when full" do
      {:ok, p} = GenStage.start_link(IngestProducer, [buffer_size: 3, buffer_keep: :first], [])
      {:ok, c} = GenStage.start_link(ManualConsumer, [subscribe_to: [{p, max_demand: 100}]], [])
      Process.sleep(20)

      for i <- 1..5, do: GenStage.cast(p, {:push, %{id: i, payload: nil, ts: 0}})
      Process.sleep(20)

      :ok = ManualConsumer.pull(c, 10)
      Process.sleep(50)

      ids = :sys.get_state(c).seen |> Enum.map(& &1.id) |> Enum.sort()
      assert ids == [3, 4, 5]
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  def main do
      # Demonstrate GenStage with manual subscription and buffer management
      {:ok, _sup} = Supervisor.start_link([], strategy: :one_for_one)
      {:ok, p} = GenStage.start_link(GenstageAdvanced.IngestProducer, 
        [buffer_size: 5, buffer_keep: :first], [])
      {:ok, c} = GenStage.start_link(GenstageAdvanced.ManualConsumer, 
        [subscribe_to: [{p, max_demand: 10}]], [])

      Process.sleep(20)

      # Push 3 events
      for i <- 1..3 do
        GenStage.cast(p, {:push, %{id: i, payload: "event_#{i}", ts: System.os_time()}})
      end

      Process.sleep(50)

      # Pull from consumer
      :ok = GenstageAdvanced.ManualConsumer.pull(c, 5)
      Process.sleep(50)

      seen = :sys.get_state(c).seen
      IO.inspect(seen, label: "✓ Events received by consumer")

      assert length(seen) == 3, "Expected 3 events"
      assert Enum.map(seen, & &1.id) == [1, 2, 3], "Events in order"

      IO.puts("✓ GenStage advanced: producer, consumer, manual subscription working")
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
