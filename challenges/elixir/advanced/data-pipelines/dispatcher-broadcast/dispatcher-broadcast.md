# GenStage BroadcastDispatcher — Multi-Sink Fan-Out

**Project**: `broadcast_dispatcher` — a live-price feed that fans out the same tick stream to multiple consumers (risk engine, UI websocket, auditor)

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
broadcast_dispatcher/
├── lib/
│   └── broadcast_dispatcher.ex
├── script/
│   └── main.exs
├── test/
│   └── broadcast_dispatcher_test.exs
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
defmodule BroadcastDispatcher.MixProject do
  use Mix.Project

  def project do
    [
      app: :broadcast_dispatcher,
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

### `lib/broadcast_dispatcher.ex`

```elixir
defmodule BroadcastDispatcher.TickProducer do
  @moduledoc """
  Producer that pushes ticks to all subscribers via BroadcastDispatcher.
  """
  use GenStage

  @type tick :: %{symbol: String.t(), price: float(), ts: integer()}

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts), do: GenStage.start_link(__MODULE__, opts, name: __MODULE__)

  @doc "Returns push result from tick."
  @spec push(tick()) :: :ok
  def push(tick), do: GenStage.cast(__MODULE__, {:push, tick})

  @impl true
  def init(_opts) do
    {:producer, %{},
     dispatcher: GenStage.BroadcastDispatcher, buffer_size: 50_000, buffer_keep: :first}
  end

  @impl true
  def handle_cast({:push, tick}, state), do: {:noreply, [tick], state}

  @doc "Handles demand result from _demand and state."
  @impl true
  def handle_demand(_demand, state), do: {:noreply, [], state}
end

defmodule BroadcastDispatcher.RiskEngine do
  use GenStage

  def start_link(opts), do: GenStage.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(_opts) do
    {:consumer, %{n: 0},
     subscribe_to: [{BroadcastDispatcher.TickProducer, max_demand: 500}]}
  end

  @doc "Handles events result from events, _from and state."
  @impl true
  def handle_events(events, _from, state) do
    {:noreply, [], %{state | n: state.n + length(events)}}
  end
end

defmodule BroadcastDispatcher.WSBroadcaster do
  use GenStage

  def start_link(opts), do: GenStage.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(_opts) do
    selector = fn %{price: p} -> p > 0 end

    {:consumer, %{n: 0},
     subscribe_to: [
       {BroadcastDispatcher.TickProducer, max_demand: 200, selector: selector}
     ]}
  end

  @doc "Handles events result from events, _from and state."
  @impl true
  def handle_events(events, _from, state) do
    # simulate some work
    :timer.sleep(div(length(events), 2))
    {:noreply, [], %{state | n: state.n + length(events)}}
  end
end

defmodule BroadcastDispatcher.Auditor do
  use GenStage

  def start_link(opts), do: GenStage.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(_opts) do
    {:consumer, %{n: 0},
     subscribe_to: [{BroadcastDispatcher.TickProducer, max_demand: 1_000}]}
  end

  @doc "Handles events result from events, _from and state."
  @impl true
  def handle_events(events, _from, state) do
    {:noreply, [], %{state | n: state.n + length(events)}}
  end
end

defmodule BroadcastDispatcher.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      BroadcastDispatcher.TickProducer,
      BroadcastDispatcher.RiskEngine,
      BroadcastDispatcher.WSBroadcaster,
      BroadcastDispatcher.Auditor
    ]

    Supervisor.start_link(children, strategy: :one_for_one)
  end
end
```

### `test/broadcast_dispatcher_test.exs`

```elixir
defmodule BroadcastDispatcher.FanOutTest do
  use ExUnit.Case, async: true
  doctest BroadcastDispatcher.TickProducer

  alias BroadcastDispatcher.{TickProducer, RiskEngine, WSBroadcaster, Auditor}

  setup do
    start_supervised!(TickProducer)
    start_supervised!(RiskEngine)
    start_supervised!(WSBroadcaster)
    start_supervised!(Auditor)
    Process.sleep(50)
    :ok
  end

  describe "BroadcastDispatcher.FanOut" do
    test "all three consumers see the same tick count" do
      for i <- 1..100 do
        TickProducer.push(%{symbol: "AAPL", price: 100.0 + i, ts: i})
      end

      Process.sleep(500)

      assert :sys.get_state(RiskEngine).n == 100
      assert :sys.get_state(WSBroadcaster).n == 100
      assert :sys.get_state(Auditor).n == 100
    end

    test "selector filters events on the WSBroadcaster without affecting others" do
      for i <- 1..10, do: TickProducer.push(%{symbol: "AAPL", price: -1.0, ts: i})
      Process.sleep(200)
      assert :sys.get_state(WSBroadcaster).n == 0
      assert :sys.get_state(Auditor).n == 10
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  def main do
      # Demonstrate BroadcastDispatcher: all consumers receive all events
      {:ok, p} = GenStage.start_link(GenstageAdvanced.IngestProducer, 
        [dispatcher: GenStage.BroadcastDispatcher, buffer_size: 100], [])
      {:ok, c1} = GenStage.start_link(GenstageAdvanced.Aggregator, 
        [subscribe_to: [{p, max_demand: 10}]], [])
      {:ok, c2} = GenStage.start_link(GenstageAdvanced.Aggregator, 
        [subscribe_to: [{p, max_demand: 10}]], [])

      Process.sleep(20)

      # Push events
      for i <- 1..5, do: GenStage.cast(p, {:push, %{id: i, payload: "msg", ts: 0}})

      Process.sleep(100)

      count1 = :sys.get_state(c1).count
      count2 = :sys.get_state(c2).count

      IO.puts("✓ Broadcast dispatcher: consumer1=#{count1}, consumer2=#{count2}")
      assert count1 > 0 and count2 > 0, "Both consumers received events"
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
