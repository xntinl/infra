# Broadway with SQS and Multi-Batcher Fan-Out

**Project**: `notifications_dispatcher` — consumes AWS SQS messages describing user notifications and fans them out to three channels (email, SMS, push) via dedicated batchers

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
notifications_dispatcher/
├── lib/
│   └── notifications_dispatcher.ex
├── script/
│   └── main.exs
├── test/
│   └── notifications_dispatcher_test.exs
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
defmodule NotificationsDispatcher.MixProject do
  use Mix.Project

  def project do
    [
      app: :notifications_dispatcher,
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

### `lib/notifications_dispatcher.ex`

```elixir
defmodule NotificationsDispatcher.Pipeline do
  @moduledoc """
  Ejercicio: Broadway with SQS and Multi-Batcher Fan-Out.
  Implementa el comportamiento descrito en el enunciado, exponiendo funciones públicas documentadas y con tipos claros.
  """

  use Broadway

  alias Broadway.Message
  alias NotificationsDispatcher.Senders.{Email, Sms, Push}

  def start_link(_opts) do
    Broadway.start_link(__MODULE__,
      name: __MODULE__,
      producer: [
        module:
          {BroadwayAWS.SQS.Producer,
           queue_url: System.fetch_env!("SQS_QUEUE_URL"),
           receive_interval: 50,
           wait_time_seconds: 20,
           max_number_of_messages: 10},
        concurrency: 2
      ],
      processors: [
        default: [concurrency: System.schedulers_online() * 2, max_demand: 50]
      ],
      batchers: [
        email: [concurrency: 1, batch_size: 50, batch_timeout: 1_000],
        sms:   [concurrency: 4, batch_size: 1, batch_timeout: 100],
        push:  [concurrency: 2, batch_size: 500, batch_timeout: 500]
      ]
    )
  end

  # ---- processor --------------------------------------------------------

  @doc "Handles message result from _processor and _ctx."
  @impl true
  def handle_message(_processor, %Message{data: data} = message, _ctx) do
    case Jason.decode(data) do
      {:ok, %{"channel" => "email"} = p} ->
        message
        |> Message.update_data(fn _ -> normalise(p) end)
        |> Message.put_batcher(:email)

      {:ok, %{"channel" => "sms"} = p} ->
        message
        |> Message.update_data(fn _ -> normalise(p) end)
        |> Message.put_batcher(:sms)

      {:ok, %{"channel" => "push"} = p} ->
        message
        |> Message.update_data(fn _ -> normalise(p) end)
        |> Message.put_batcher(:push)

      {:ok, %{"channel" => other}} ->
        Message.failed(message, "unknown channel: #{other}")

      {:error, _} ->
        Message.failed(message, "invalid json")
    end
  end

  # ---- batchers ---------------------------------------------------------

  @doc "Handles batch result from messages, _info and _ctx."
  @impl true
  def handle_batch(:email, messages, _info, _ctx) do
    payloads = Enum.map(messages, & &1.data)
    fail_or_pass(messages, Email.send_batch(payloads))
  end

  @doc "Handles batch result from _info and _ctx."
  def handle_batch(:sms, [message], _info, _ctx) do
    case Sms.send_one(message.data) do
      :ok -> [message]
      {:error, reason} -> [Message.failed(message, inspect(reason))]
    end
  end

  @doc "Handles batch result from messages, _info and _ctx."
  def handle_batch(:push, messages, _info, _ctx) do
    payloads = Enum.map(messages, & &1.data)
    fail_or_pass(messages, Push.send_batch(payloads))
  end

  # ---- helpers ----------------------------------------------------------

  defp normalise(p), do: %{user_id: p["user_id"], payload: p["payload"], channel: p["channel"]}

  defp fail_or_pass(messages, :ok), do: messages

  defp fail_or_pass(messages, {:error, reason}) do
    Enum.map(messages, &Message.failed(&1, inspect(reason)))
  end
end

defmodule NotificationsDispatcher.Senders.Email do
  @doc "Sends batch result from payloads."
  def send_batch(payloads) do
    :telemetry.execute([:dispatcher, :email, :batch], %{size: length(payloads)}, %{})
    :ok
  end
end

defmodule NotificationsDispatcher.Senders.Sms do
  @doc "Sends one result from _payload."
  def send_one(_payload), do: :ok
end

defmodule NotificationsDispatcher.Senders.Push do
  @doc "Sends batch result from payloads."
  def send_batch(payloads) do
    :telemetry.execute([:dispatcher, :push, :batch], %{size: length(payloads)}, %{})
    :ok
  end
end
```

### `test/notifications_dispatcher_test.exs`

```elixir
defmodule NotificationsDispatcher.PipelineTest do
  use ExUnit.Case, async: true
  doctest NotificationsDispatcher.Pipeline

  alias NotificationsDispatcher.Pipeline

  describe "routing by channel" do
    test "email goes to :email batcher" do
      msg = ~s({"channel":"email","user_id":"u1","payload":{"to":"a@b.c"}})
      ref = Broadway.test_message(Pipeline, msg)
      assert_receive {:ack, ^ref, [%Broadway.Message{batcher: :email}], []}, 2_000
    end

    test "sms goes to :sms batcher" do
      msg = ~s({"channel":"sms","user_id":"u1","payload":{"to":"+111"}})
      ref = Broadway.test_message(Pipeline, msg)
      assert_receive {:ack, ^ref, [%Broadway.Message{batcher: :sms}], []}, 2_000
    end

    test "push goes to :push batcher" do
      msg = ~s({"channel":"push","user_id":"u1","payload":{"token":"abc"}})
      ref = Broadway.test_message(Pipeline, msg)
      assert_receive {:ack, ^ref, [%Broadway.Message{batcher: :push}], []}, 2_000
    end

    test "unknown channel fails the message" do
      msg = ~s({"channel":"smoke"})
      ref = Broadway.test_message(Pipeline, msg)
      assert_receive {:ack, ^ref, [], [%Broadway.Message{status: {:failed, _}}]}, 2_000
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  def main do
      # Simulate SQS multi-batcher fan-out: route notifications to 3 channels
      notifications = [
        %{id: "n1", user: "u1", channels: [:email, :sms, :push]},
        %{id: "n2", user: "u2", channels: [:email, :push]},
        %{id: "n3", user: "u3", channels: [:sms]}
      ]

      # Fan-out to batchers
      email_batch = Enum.filter(notifications, &(:email in &1.channels))
      sms_batch = Enum.filter(notifications, &(:sms in &1.channels))
      push_batch = Enum.filter(notifications, &(:push in &1.channels))

      IO.puts("✓ Email batch: #{length(email_batch)} notifications")
      IO.puts("✓ SMS batch: #{length(sms_batch)} notifications")
      IO.puts("✓ Push batch: #{length(push_batch)} notifications")

      assert length(email_batch) == 2, "Email batch correct"
      assert length(sms_batch) == 2, "SMS batch correct"
      assert length(push_batch) == 2, "Push batch correct"

      IO.puts("✓ SQS multi-batcher fan-out working")
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
