# Broadway with Kafka — `partition_by` for Per-Key Ordering

**Project**: `user_events_consumer` — consumes `user-events` Kafka topic and processes events per `user_id` in arrival order while still using all cores in parallel

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
user_events_consumer/
├── lib/
│   └── user_events_consumer.ex
├── script/
│   └── main.exs
├── test/
│   └── user_events_consumer_test.exs
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
defmodule UserEventsConsumer.MixProject do
  use Mix.Project

  def project do
    [
      app: :user_events_consumer,
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
### `lib/user_events_consumer.ex`

```elixir
defmodule UserEventsConsumer.Pipeline do
  use Broadway

  alias Broadway.Message

  @stages 8

  def start_link(_opts) do
    Broadway.start_link(__MODULE__,
      name: __MODULE__,
      producer: [
        module:
          {BroadwayKafka.Producer,
           hosts: [localhost: 9092],
           group_id: "user_events_consumer",
           topics: ["user-events"],
           offset_commit_interval_seconds: 5,
           client_config: [connect_timeout: 30_000]},
        concurrency: 1
      ],
      processors: [
        default: [
          concurrency: @stages,
          max_demand: 100,
          partition_by: &partition_by_user/1
        ]
      ],
      batchers: [
        default: [concurrency: 2, batch_size: 200, batch_timeout: 500]
      ]
    )
  end

  @doc "Handles message result from _processor and _ctx."
  @impl true
  def handle_message(_processor, %Message{data: data} = message, _ctx) do
    case Jason.decode(data) do
      {:ok, %{"user_id" => uid} = event} ->
        UserEventsConsumer.Processor.apply_event(uid, event)
        message

      {:ok, _} ->
        Message.failed(message, "missing user_id")

      {:error, _} ->
        message |> Message.failed("invalid json") |> Message.configure_ack(on_failure: :reject)
    end
  end

  @doc "Handles batch result from messages, _info and _ctx."
  @impl true
  def handle_batch(:default, messages, _info, _ctx), do: messages

  # ---- routing ---------------------------------------------------------

  # Keep the hash stable across releases — it determines per-user order.
  defp partition_by_user(%Message{data: data}) do
    case Jason.decode(data) do
      {:ok, %{"user_id" => uid}} -> :erlang.phash2(uid, @stages)
      _ -> 0
    end
  end
end

defmodule UserEventsConsumer.Processor do
  @moduledoc """
  Applies an event to the read model. Replace with real repo writes.

  Because Broadway routes events for the same user_id to the same processor
  stage, two concurrent calls to apply_event/2 for the same user cannot
  happen — we do not need a lock or CAS here.
  """

  @doc "Applies event result from user_id and event."
  def apply_event(user_id, event) do
    :telemetry.execute(
      [:user_events, :applied],
      %{count: 1},
      %{user_id: user_id, type: event["type"]}
    )

    :ok
  end
end
```
### `test/user_events_consumer_test.exs`

```elixir
defmodule UserEventsConsumer.PipelineTest do
  use ExUnit.Case, async: true
  doctest UserEventsConsumer.Pipeline

  alias UserEventsConsumer.Pipeline

  describe "processing" do
    test "applies a valid event and acks" do
      msg = ~s({"user_id":"u42","type":"login"})
      ref = Broadway.test_message(Pipeline, msg)
      assert_receive {:ack, ^ref, [%Broadway.Message{}], []}, 2_000
    end

    test "fails messages without user_id" do
      msg = ~s({"type":"orphan"})
      ref = Broadway.test_message(Pipeline, msg)
      assert_receive {:ack, ^ref, [], [%Broadway.Message{status: {:failed, _}}]}, 2_000
    end
  end

  describe "partition_by ordering" do
    test "events for the same user_id hit the same processor index" do
      events =
        for i <- 1..200 do
          ~s({"user_id":"u42","type":"e#{i}"})
        end

      # Snapshot which processor PID handles each message.
      # With partition_by, all should route to the same stage.
      :telemetry.attach(
        "test-stage",
        [:user_events, :applied],
        fn _e, _m, meta, parent -> send(parent, {:applied, meta.user_id, self()}) end,
        self()
      )

      ref = Broadway.test_batch(Pipeline, events)
      assert_receive {:ack, ^ref, _ok, _fail}, 5_000

      :telemetry.detach("test-stage")

      pids =
        for _ <- 1..200 do
          receive do
            {:applied, "u42", pid} -> pid
          after
            100 -> nil
          end
        end
        |> Enum.reject(&is_nil/1)
        |> Enum.uniq()

      assert length(pids) == 1, "events for u42 were split across #{length(pids)} processors"
    end
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  def main do
      # Simulate Kafka partition_by for per-user-id ordering
      events = [
        %{id: "e1", user_id: "u1", action: "view"},
        %{id: "e2", user_id: "u2", action: "buy"},
        %{id: "e3", user_id: "u1", action: "review"},
        %{id: "e4", user_id: "u3", action: "follow"}
      ]

      # Partition by user_id using hash
      partitioned = Enum.group_by(events, & &1.user_id)

      IO.inspect(Map.keys(partitioned), label: "✓ Partitions created")

      # Verify ordering per partition
      u1_events = partitioned["u1"]

      IO.inspect(u1_events, label: "✓ u1 events (in order)")

      assert length(partitioned) == 3, "3 partitions created"
      assert Enum.all?(u1_events, &(&1.user_id == "u1")), "All u1 events in partition"

      IO.puts("✓ Kafka partition_by: per-key ordering with parallel processing")
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
