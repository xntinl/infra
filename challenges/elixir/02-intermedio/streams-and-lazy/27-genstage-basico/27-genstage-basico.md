# GenStage: Backpressure and Demand-Driven Pipelines

## Goal

Build a `task_queue` processing pipeline with GenStage where a producer emits jobs from a queue, a producer_consumer validates them, and a consumer executes them. The pipeline provides built-in backpressure: consumers declare how many events they can handle, and producers emit exactly that many.

---

## Why GenStage and not `Task.async_stream` or a simple `send`

`Task.async_stream` processes a bounded collection concurrently. It has no concept of a continuous producer. When new jobs arrive while workers are busy, there is no built-in mechanism to pause the producer.

A direct `send` from producer to consumer creates unbounded mailboxes:

```
producer sends 10,000 messages
consumer processes 100/sec
mailbox grows at 9,900 messages/sec -> OOM in ~1 minute
```

GenStage's demand protocol inverts control:

```
consumer: "I can handle 10 more events"
producer: emits exactly 10 events
consumer: (processes 10)
consumer: "I can handle 10 more events"
producer: emits exactly 10 more
```

The producer never sends more than what was requested. Mailboxes stay small.

---

## The three roles

| Role | `use GenStage` callback | Receives | Emits |
|------|------------------------|----------|-------|
| `:producer` | `handle_demand/2` | demand count | events |
| `:consumer` | `handle_events/3` | events | nothing |
| `:producer_consumer` | `handle_events/3` | events | transformed events |

---

## Implementation

### Step 1: `mix.exs`

```elixir
defmodule TaskQueue.MixProject do
  use Mix.Project

  def project do
    [
      app: :task_queue,
      version: "0.1.0",
      elixir: "~> 1.15",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [
      extra_applications: [:logger],
      mod: {TaskQueue.Application, []}
    ]
  end

  defp deps do
    [
      {:gen_stage, "~> 1.2"}
    ]
  end
end
```

### Step 2: `lib/task_queue/application.ex`

```elixir
defmodule TaskQueue.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      TaskQueue.QueueServer
    ]

    opts = [strategy: :one_for_one, name: TaskQueue.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
```

### Step 3: `lib/task_queue/queue_server.ex` -- in-memory queue

The QueueServer is a simple GenServer holding an Erlang `:queue`. The GenStage producer dequeues from it when demand arrives.

```elixir
defmodule TaskQueue.QueueServer do
  use GenServer

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, :queue.new(), name: __MODULE__)
  end

  def enqueue(job) when is_map(job) do
    GenServer.call(__MODULE__, {:enqueue, job})
  end

  def dequeue do
    GenServer.call(__MODULE__, :dequeue)
  end

  @impl true
  def init(queue), do: {:ok, queue}

  @impl true
  def handle_call({:enqueue, job}, _from, queue) do
    new_q = :queue.in(job, queue)
    {:reply, {:ok, :queue.len(new_q)}, new_q}
  end

  @impl true
  def handle_call(:dequeue, _from, queue) do
    case :queue.out(queue) do
      {{:value, job}, new_q} -> {:reply, {:ok, job}, new_q}
      {:empty, _} -> {:reply, {:error, :empty}, queue}
    end
  end
end
```

### Step 4: `lib/task_queue/worker.ex` -- job executor

```elixir
defmodule TaskQueue.Worker do
  @moduledoc """
  Executes individual jobs. Used by the GenStage consumer.
  """

  @spec execute(map()) :: {:ok, term()} | {:error, term()}
  def execute(%{type: "noop", args: _}), do: {:ok, :noop}
  def execute(%{type: "echo", args: args}), do: {:ok, args}
  def execute(%{type: "fail", args: %{reason: reason}}), do: {:error, {:job_failed, reason}}
  def execute(%{type: type}), do: {:error, {:unknown_type, type}}
  def execute(_), do: {:error, :invalid_job}
end
```

### Step 5: `lib/task_queue/pipeline/job_producer.ex`

The producer responds to demand by dequeuing up to `demand` jobs from the QueueServer. If fewer jobs are available, it emits only what exists and tracks unmet demand in its state. GenStage retries demand automatically on the next tick.

```elixir
defmodule TaskQueue.Pipeline.JobProducer do
  @moduledoc """
  GenStage producer that emits jobs from the task queue.
  """

  use GenStage

  def start_link(opts \\ []) do
    GenStage.start_link(__MODULE__, :ok, name: __MODULE__)
  end

  @impl true
  def init(:ok) do
    {:producer, %{pending_demand: 0}}
  end

  @impl true
  def handle_demand(demand, state) when demand > 0 do
    jobs = dequeue_batch(demand)
    {:noreply, jobs, %{state | pending_demand: demand - length(jobs)}}
  end

  defp dequeue_batch(n), do: dequeue_batch(n, [])

  defp dequeue_batch(0, acc), do: Enum.reverse(acc)

  defp dequeue_batch(n, acc) do
    case TaskQueue.QueueServer.dequeue() do
      {:ok, job}       -> dequeue_batch(n - 1, [job | acc])
      {:error, :empty} -> Enum.reverse(acc)
    end
  end
end
```

### Step 6: `lib/task_queue/pipeline/job_validator.ex`

The validator is a producer_consumer -- it receives events from the producer, filters invalid ones, annotates valid ones with a `:validated_at` timestamp, and passes them downstream. Invalid jobs are silently dropped (in production you would log them).

```elixir
defmodule TaskQueue.Pipeline.JobValidator do
  @moduledoc """
  GenStage producer_consumer that validates jobs from the producer.
  Valid jobs are passed downstream. Invalid jobs are dropped.
  """

  use GenStage

  def start_link(opts \\ []) do
    GenStage.start_link(__MODULE__, :ok, name: __MODULE__)
  end

  @impl true
  def init(:ok) do
    {:producer_consumer, :ok}
  end

  @impl true
  def handle_events(jobs, _from, state) do
    valid_jobs =
      jobs
      |> Enum.filter(&valid_job?/1)
      |> Enum.map(&Map.put(&1, :validated_at, System.monotonic_time()))

    {:noreply, valid_jobs, state}
  end

  defp valid_job?(%{type: _, args: _}), do: true
  defp valid_job?(_), do: false
end
```

### Step 7: `lib/task_queue/pipeline/job_consumer.ex`

The consumer executes validated jobs. It must return an empty event list `[]` -- consumers do not emit events downstream. The `subscribe_to` option in `init/1` wires the consumer to the validator with a `max_demand` that limits in-flight events.

```elixir
defmodule TaskQueue.Pipeline.JobConsumer do
  @moduledoc """
  GenStage consumer that executes validated jobs.
  """

  use GenStage

  @max_demand 5

  def start_link(opts \\ []) do
    GenStage.start_link(__MODULE__, :ok, name: __MODULE__)
  end

  @impl true
  def init(:ok) do
    {:consumer, :ok, subscribe_to: [{TaskQueue.Pipeline.JobValidator, max_demand: @max_demand}]}
  end

  @impl true
  def handle_events(jobs, _from, state) do
    Enum.each(jobs, fn job ->
      case TaskQueue.Worker.execute(job) do
        {:ok, result}    -> :logger.info("Job done: #{inspect(result)}")
        {:error, reason} -> :logger.warning("Job failed: #{inspect(reason)}")
      end
    end)

    {:noreply, [], state}
  end
end
```

### Step 8: Tests

The tests call `handle_demand/2` and `handle_events/3` directly to verify behavior without starting the full GenStage subscription pipeline. This is the idiomatic way to unit-test GenStage stages.

```elixir
# test/task_queue/genstage_test.exs
defmodule TaskQueue.GenStageTest do
  use ExUnit.Case, async: false

  alias TaskQueue.Pipeline.{JobProducer, JobValidator, JobConsumer}
  alias TaskQueue.QueueServer

  setup do
    drain_queue()
    :ok
  end

  defp drain_queue do
    case QueueServer.dequeue() do
      {:ok, _}         -> drain_queue()
      {:error, :empty} -> :ok
    end
  end

  describe "JobValidator.handle_events/3" do
    test "passes valid jobs through" do
      valid_job = %{type: "echo", args: %{msg: "hello"}}
      {:noreply, events, _} = JobValidator.handle_events([valid_job], self(), :ok)
      assert length(events) == 1
      assert hd(events).type == "echo"
    end

    test "attaches validated_at timestamp" do
      valid_job = %{type: "echo", args: %{}}
      {:noreply, [validated], _} = JobValidator.handle_events([valid_job], self(), :ok)
      assert Map.has_key?(validated, :validated_at)
    end

    test "drops invalid jobs" do
      invalid_job = %{bad: "structure"}
      {:noreply, events, _} = JobValidator.handle_events([invalid_job], self(), :ok)
      assert events == []
    end

    test "filters mixed batch" do
      jobs = [
        %{type: "noop", args: %{}},
        %{bad: "job"},
        %{type: "echo", args: %{}}
      ]
      {:noreply, valid, _} = JobValidator.handle_events(jobs, self(), :ok)
      assert length(valid) == 2
    end
  end

  describe "JobProducer.handle_demand/2" do
    test "returns empty list when queue is empty" do
      {:noreply, events, _state} = JobProducer.handle_demand(5, %{pending_demand: 0})
      assert events == []
    end

    test "emits available jobs up to demand" do
      QueueServer.enqueue(%{type: "noop", args: %{}})
      QueueServer.enqueue(%{type: "noop", args: %{}})

      {:noreply, events, _state} = JobProducer.handle_demand(10, %{pending_demand: 0})
      assert length(events) == 2
    end

    test "does not emit more than demand" do
      for _ <- 1..5, do: QueueServer.enqueue(%{type: "noop", args: %{}})

      {:noreply, events, _state} = JobProducer.handle_demand(3, %{pending_demand: 0})
      assert length(events) == 3
    end
  end

  describe "JobConsumer.handle_events/3" do
    test "executes jobs and returns empty events" do
      jobs = [%{type: "noop", args: %{}, validated_at: System.monotonic_time()}]
      {:noreply, [], _state} = JobConsumer.handle_events(jobs, self(), :ok)
    end
  end
end
```

### Step 9: Run

```bash
mix deps.get
mix test test/task_queue/genstage_test.exs --trace
```

---

## Trade-off analysis

| Aspect | GenStage pipeline | Plain `send/2` | `Task.async_stream` |
|--------|-------------------|----------------|---------------------|
| Backpressure | built-in via demand | none | bounded by collection size |
| Continuous event streams | yes | yes (unbounded) | no -- requires known collection |
| Memory under burst | bounded by `max_demand` | unbounded mailbox | bounded by stream chunk |
| Complexity | high (3 modules, subscriptions) | low | low |
| Best for | long-running pipelines | low-volume messaging | batch processing |

---

## Common production mistakes

**1. Forgetting to subscribe stages to each other**
`use GenStage` defines callbacks but does not wire the pipeline. Use `subscribe_to:` in `init/1` or `GenStage.sync_subscribe/3`.

**2. Returning events from a consumer**
Consumers must return `{:noreply, [], state}`. Returning events raises.

**3. Setting `max_demand: 1` expecting sequential processing**
It processes one event at a time per consumer but does not guarantee ordering across multiple consumers.

**4. Not tracking unmet demand in the producer**
If `handle_demand` returns fewer events than demanded, store the unmet demand and fulfill it when new items arrive via `handle_info`.

**5. Using `GenStage.call/2` on a stage from within its own callbacks**
A GenStage stage is a GenServer. Calling itself causes a deadlock.

---

## Resources

- [GenStage -- official hex package](https://hexdocs.pm/gen_stage/GenStage.html)
- [Introduction to GenStage -- Jose Valim](https://elixir-lang.org/blog/2016/07/14/announcing-genstage/)
- [Flow: built on GenStage for parallel data processing](https://hexdocs.pm/flow/Flow.html)
