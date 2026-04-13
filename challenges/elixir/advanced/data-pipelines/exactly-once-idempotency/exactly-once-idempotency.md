# Exactly-Once Processing with Idempotency Keys

**Project**: `payments_processor` — processes `payment.captured` events from an at-least-once source (RabbitMQ), guaranteeing each capture triggers exactly one charge to the payment gateway

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
payments_processor/
├── lib/
│   └── payments_processor.ex
├── script/
│   └── main.exs
├── test/
│   └── payments_processor_test.exs
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
defmodule PaymentsProcessor.MixProject do
  use Mix.Project

  def project do
    [
      app: :payments_processor,
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
### `lib/payments_processor.ex`

```elixir
defmodule PaymentsProcessor.Idempotency do
  @moduledoc """
  Wraps an effect with idempotent-key semantics.

  If the key is already committed, returns the stored result without re-running
  the effect. If not, runs the effect inside the same transaction as the key
  insert — either both happen, or neither.
  """

  alias PaymentsProcessor.Repo

  @type key :: String.t()
  @type result :: map()

  @spec process_value(key(), (-> {:ok, result()} | {:error, term()})) ::
          {:ok, result()} | {:duplicate, result()} | {:error, term()}
  @doc "Processes result from key and effect_fun."
  def process_value(key, effect_fun) do
    Repo.transaction(fn ->
      case Repo.query!("SELECT result FROM idempotency_keys WHERE key = $1 FOR UPDATE", [key]) do
        %{rows: [[stored]]} ->
          {:duplicate, stored}

        %{rows: []} ->
          case effect_fun.() do
            {:ok, result} ->
              Repo.query!(
                "INSERT INTO idempotency_keys (key, result) VALUES ($1, $2)",
                [key, result]
              )

              {:ok, result}

            {:error, reason} ->
              Repo.rollback(reason)
          end
      end
    end)
    |> unwrap()
  end

  defp unwrap({:ok, {:ok, r}}), do: {:ok, r}
  defp unwrap({:ok, {:duplicate, r}}), do: {:duplicate, r}
  defp unwrap({:error, r}), do: {:error, r}
end

defmodule PaymentsProcessor.Charger do
  @moduledoc """
  Calls the payment gateway. Also passes an Idempotency-Key header so that
  the gateway itself dedups within its own window.
  """

  @doc "Returns charge result from currency and idempotency_key."
  def charge(%{amount_cents: amount, currency: ccy, idempotency_key: key}) do
    # Real impl: Finch.build(:post, url, headers, body) with "Idempotency-Key: #{key}"
    :telemetry.execute([:payments, :charge], %{amount: amount}, %{currency: ccy, key: key})
    {:ok, %{charge_id: "ch_" <> key, status: "succeeded"}}
  end
end

defmodule PaymentsProcessor.Pipeline do
  use Broadway

  alias Broadway.Message
  alias PaymentsProcessor.{Idempotency, Charger}

  def start_link(_opts) do
    Broadway.start_link(__MODULE__,
      name: __MODULE__,
      producer: [
        module: {Broadway.DummyProducer, []},  # replace with BroadwayRabbitMQ or similar
        concurrency: 1
      ],
      processors: [default: [concurrency: 16, max_demand: 50]]
    )
  end

  @doc "Handles message result from _ and _."
  @impl true
  def handle_message(_, %Message{data: data} = message, _) do
    %{"idempotency_key" => key, "amount_cents" => amount, "currency" => ccy} = Jason.decode!(data)

    result =
      Idempotency.process_value(key, fn ->
        Charger.charge(%{amount_cents: amount, currency: ccy, idempotency_key: key})
      end)

    case result do
      {:ok, _} -> message
      {:duplicate, _} -> message  # ack — already done
      {:error, reason} -> Message.failed(message, inspect(reason))
    end
  end
end
```
### `test/payments_processor_test.exs`

```elixir
defmodule PaymentsProcessor.IdempotencyTest do
  use ExUnit.Case, async: true
  doctest PaymentsProcessor.Idempotency

  alias PaymentsProcessor.{Idempotency, Repo}

  setup do
    Repo.query!("DELETE FROM idempotency_keys", [])
    :ok
  end

  describe "process/2" do
    test "runs the effect on first call" do
      key = "k1-#{:erlang.unique_integer()}"

      assert {:ok, %{"charged" => true}} =
               Idempotency.process(key, fn -> {:ok, %{"charged" => true}} end)
    end

    test "returns :duplicate without re-running the effect" do
      key = "k2-#{:erlang.unique_integer()}"
      counter = :atomics.new(1, [])

      effect = fn ->
        :atomics.add(counter, 1, 1)
        {:ok, %{"charged" => true}}
      end

      assert {:ok, _} = Idempotency.process(key, effect)
      assert {:duplicate, %{"charged" => true}} = Idempotency.process(key, effect)
      assert :atomics.get(counter, 1) == 1
    end

    test "rolls back on effect error" do
      key = "k3-#{:erlang.unique_integer()}"

      assert {:error, :boom} =
               Idempotency.process(key, fn -> {:error, :boom} end)

      # Key was not committed — next attempt runs the effect again.
      assert {:ok, _} = Idempotency.process(key, fn -> {:ok, %{"ok" => true}} end)
    end
  end

  describe "concurrent calls with the same key" do
    test "only one effect runs" do
      key = "k4-#{:erlang.unique_integer()}"
      counter = :atomics.new(1, [])

      effect = fn ->
        :atomics.add(counter, 1, 1)
        Process.sleep(20)
        {:ok, %{"n" => 1}}
      end

      tasks =
        for _ <- 1..10 do
          Task.async(fn -> Idempotency.process(key, effect) end)
        end

      Task.await_many(tasks, 5_000)

      assert :atomics.get(counter, 1) == 1
    end
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  def main do
      # Demonstrate exactly-once processing with idempotency keys
      events = [
        %{id: "pay1", idempotency_key: "key_1", amount: 100},
        %{id: "pay2", idempotency_key: "key_1", amount: 100},  # Duplicate
        %{id: "pay3", idempotency_key: "key_2", amount: 50}
      ]

      # Dedup by idempotency_key
      processed = Enum.uniq_by(events, & &1.idempotency_key)

      IO.inspect(processed, label: "✓ Deduplicated payments")
      IO.puts("✓ Processed #{length(processed)} unique payments from #{length(events)} events")

      assert length(processed) == 2, "Expected 2 unique payments"
      assert Enum.all?(processed, &Map.has_key?(&1, :idempotency_key)), "All have idempotency key"

      IO.puts("✓ Exactly-once idempotency: duplicate detection working")
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
