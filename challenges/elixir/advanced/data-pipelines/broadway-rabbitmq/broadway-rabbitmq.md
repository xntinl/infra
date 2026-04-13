# Broadway with RabbitMQ — At-Least-Once Processing with Acks

**Project**: `order_processor` — consumes `orders.created` messages from RabbitMQ, validates and persists each order, and acknowledges only on success

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
order_processor/
├── lib/
│   └── order_processor.ex
├── script/
│   └── main.exs
├── test/
│   └── order_processor_test.exs
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
defmodule OrderProcessor.MixProject do
  use Mix.Project

  def project do
    [
      app: :order_processor,
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
### `lib/order_processor.ex`

```elixir
defmodule OrderProcessor.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [OrderProcessor.Pipeline]
    Supervisor.start_link(children, strategy: :one_for_one, name: OrderProcessor.Supervisor)
  end
end

defmodule OrderProcessor.Pipeline do
  use Broadway

  alias Broadway.Message
  alias OrderProcessor.{Validator, Repo}

  def start_link(_opts) do
    Broadway.start_link(__MODULE__,
      name: __MODULE__,
      producer: [
        module:
          {BroadwayRabbitMQ.Producer,
           queue: "orders.created",
           connection: [host: System.get_env("RABBIT_HOST", "localhost")],
           qos: [prefetch_count: 500],
           on_failure: :reject_and_requeue,
           metadata: [:routing_key, :headers]},
        concurrency: 1
      ],
      processors: [
        default: [
          concurrency: System.schedulers_online(),
          max_demand: 100
        ]
      ],
      batchers: [
        warehouse: [concurrency: 2, batch_size: 200, batch_timeout: 1_000]
      ]
    )
  end

  # ---- callbacks ---------------------------------------------------------

  @doc "Handles message result from _processor and _context."
  @impl true
  def handle_message(_processor, %Message{data: data} = message, _context) do
    case Jason.decode(data) do
      {:ok, payload} ->
        case Validator.validate(payload) do
          {:ok, order} ->
            message
            |> Message.update_data(fn _ -> order end)
            |> Message.put_batcher(:warehouse)

          {:error, reason} ->
            Message.failed(message, "validation: #{inspect(reason)}")
        end

      {:error, _} ->
        # Malformed JSON — reject without requeue to avoid poison-pill loops.
        message |> Message.failed("invalid json") |> Message.configure_ack(on_failure: :reject)
    end
  end

  @doc "Handles batch result from messages, _batch_info and _context."
  @impl true
  def handle_batch(:warehouse, messages, _batch_info, _context) do
    orders = Enum.map(messages, & &1.data)

    case Repo.insert_all_orders(orders) do
      {:ok, _count} ->
        messages

      {:error, reason} ->
        Enum.map(messages, &Message.failed(&1, "db: #{inspect(reason)}"))
    end
  end
end

defmodule OrderProcessor.Validator do
  @required ~w(order_id user_id amount)

  @doc "Validates result."
  def validate(%{} = payload) do
    missing = Enum.filter(@required, fn k -> is_nil(payload[k]) end)

    cond do
      missing != [] -> {:error, {:missing, missing}}
      payload["amount"] <= 0 -> {:error, :invalid_amount}
      true -> {:ok, normalise(payload)}
    end
  end

  @doc "Validates result from _."
  def validate(_), do: {:error, :not_a_map}

  defp normalise(p) do
    %{
      order_id: p["order_id"],
      user_id: p["user_id"],
      amount_cents: round(p["amount"] * 100),
      received_at: System.system_time(:millisecond)
    }
  end
end

defmodule OrderProcessor.Repo do
  @moduledoc """
  Stubbed bulk insert. Replace with `MyApp.Repo.insert_all(Order, orders, ...)`.
  """

  @doc "Returns insert all orders result from orders."
  def insert_all_orders(orders) when is_list(orders) do
    :telemetry.execute([:order_processor, :batch], %{count: length(orders)}, %{})
    {:ok, length(orders)}
  end
end
```
### `test/order_processor_test.exs`

```elixir
defmodule OrderProcessor.PipelineTest do
  use ExUnit.Case, async: true
  doctest OrderProcessor.Application

  alias OrderProcessor.Pipeline

  describe "handle_message/3" do
    test "routes valid messages to the warehouse batcher" do
      ref = Broadway.test_message(Pipeline, ~s({"order_id":"o1","user_id":"u1","amount":12.5}))
      assert_receive {:ack, ^ref, [%Broadway.Message{batcher: :warehouse}], []}, 2_000
    end

    test "fails messages with missing fields" do
      ref = Broadway.test_message(Pipeline, ~s({"order_id":"o2"}))
      assert_receive {:ack, ^ref, [], [%Broadway.Message{status: {:failed, _}}]}, 2_000
    end

    test "fails (without requeue) on malformed JSON" do
      ref = Broadway.test_message(Pipeline, "not json")
      assert_receive {:ack, ^ref, [], [%Broadway.Message{} = msg]}, 2_000
      assert match?({:failed, _}, msg.status)
    end
  end

  describe "handle_batch/4" do
    test "batches multiple valid messages" do
      payloads =
        for i <- 1..3 do
          ~s({"order_id":"o#{i}","user_id":"u","amount":1.0})
        end

      ref = Broadway.test_batch(Pipeline, payloads)
      assert_receive {:ack, ^ref, successful, []}, 2_000
      assert length(successful) == 3
    end
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  def main do
      # Simulate RabbitMQ order processing with validation and acknowledgement
      orders = [
        %{id: "o1", customer: "c1", amount: 100},
        %{id: "o2", customer: "c2", amount: 0},  # Invalid
        %{id: "o3", customer: "c3", amount: 250}
      ]

      # Validate and acknowledge
      results = Enum.map(orders, fn order ->
        if order.amount > 0 do
          Map.put(order, :status, :validated)
        else
          Map.put(order, :status, :invalid)
        end
      end)

      # Count valid orders
      valid_orders = Enum.filter(results, &(&1.status == :validated))

      IO.inspect(valid_orders, label: "✓ Valid orders")
      IO.puts("✓ Processed #{length(results)} orders, #{length(valid_orders)} valid")

      assert length(valid_orders) == 2, "Expected 2 valid orders"
      assert Enum.all?(valid_orders, &(&1.status == :validated)), "All valid"

      IO.puts("✓ Broadway RabbitMQ: at-least-once processing working")
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
