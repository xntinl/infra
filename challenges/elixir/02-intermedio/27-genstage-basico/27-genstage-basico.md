# GenStage: Backpressure and Demand-Driven Pipelines

**Project**: `task_queue` — built incrementally across the intermediate level

---

## Project context

`task_queue` is now receiving jobs faster than workers can process them. Without flow control, the queue grows unbounded and the system runs out of memory. The ops team needs a pipeline where producers only emit jobs that consumers are ready to handle — no more, no less.

Project structure at this point:

```
task_queue/
├── lib/
│   └── task_queue/
│       ├── application.ex
│       ├── pipeline/
│       │   ├── job_producer.ex         # ← you implement this
│       │   ├── job_validator.ex        # ← and this (producer_consumer)
│       │   └── job_consumer.ex         # ← and this
│       ├── worker.ex
│       ├── queue_server.ex
│       ├── scheduler.ex
│       └── registry.ex
├── test/
│   └── task_queue/
│       └── genstage_test.exs           # given tests — must pass without modification
└── mix.exs
```

Add to `mix.exs`:

```elixir
{:gen_stage, "~> 1.2"}
```

---

## The business problem

The scheduler dequeues jobs as fast as the queue produces them and fans them out to workers. Under normal load this is fine. Under burst load — 10,000 jobs in 5 seconds — the scheduler overwhelms workers, jobs pile up in GenServer mailboxes, and the VM's memory climbs until OOM.

GenStage solves this with **demand-driven flow**:

1. Consumers declare how many events they can handle right now
2. Producers emit exactly that many — no more
3. If consumers are slow, the producer backs off automatically

The result: memory usage is bounded by the in-flight batch sizes. The pipeline processes at the rate of its slowest stage, not the rate of its fastest producer.

---

## Why GenStage and not `Task.async_stream` or a simple `send`

`Task.async_stream` processes a bounded collection concurrently. It has no concept of a producer that continuously generates events. When new jobs arrive while workers are busy, there is no built-in mechanism to pause the producer.

A direct `send` from producer to consumer creates unbounded mailboxes:

```
producer sends 10,000 messages
consumer processes 100/sec
mailbox grows at 9,900 messages/sec → OOM in ~1 minute
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
| `:producer_consumer` | `handle_events/3` + `handle_demand/2` | events + demand | transformed events |

A pipeline must start with a producer and end with a consumer. Stages in between are producer_consumers. Subscriptions connect stages: `GenStage.sync_subscribe(consumer, to: producer)`.

---

## Implementation

### Step 1: `lib/task_queue/pipeline/job_producer.ex`

```elixir
defmodule TaskQueue.Pipeline.JobProducer do
  @moduledoc """
  GenStage producer that emits jobs from the task queue.

  Responds to demand by dequeuing up to `demand` jobs from
  `TaskQueue.QueueServer`. If fewer jobs are available than demanded,
  emits only what is available — GenStage retries demand on next tick.

  State: `%{pending_demand: integer}` — accumulated unmet demand.
  """

  use GenStage

  def start_link(opts \\ []) do
    GenStage.start_link(__MODULE__, :ok, name: __MODULE__)
  end

  @impl true
  def init(:ok) do
    # TODO: return {:producer, %{pending_demand: 0}}
  end

  @impl true
  def handle_demand(demand, state) when demand > 0 do
    # TODO: dequeue up to `demand` jobs from TaskQueue.QueueServer
    # Build a list of jobs by calling QueueServer.dequeue() in a loop
    # until you have `demand` jobs or the queue is empty
    # Return {:noreply, jobs, %{state | pending_demand: demand - length(jobs)}}
    #
    # HINT:
    # jobs = dequeue_batch(demand)
    # {:noreply, jobs, %{state | pending_demand: demand - length(jobs)}}
  end

  # Private helper: dequeue up to `n` jobs
  # Returns a list (may be shorter than n if queue is empty)
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

### Step 2: `lib/task_queue/pipeline/job_validator.ex`

```elixir
defmodule TaskQueue.Pipeline.JobValidator do
  @moduledoc """
  GenStage producer_consumer that validates jobs from the producer.

  Valid jobs are passed downstream. Invalid jobs are dropped and
  logged. This stage adds a `:validated_at` timestamp to each job.
  """

  use GenStage

  def start_link(opts \\ []) do
    GenStage.start_link(__MODULE__, :ok, name: __MODULE__)
  end

  @impl true
  def init(:ok) do
    # TODO: return {:producer_consumer, :ok}
    # producer_consumer needs no initial state beyond :ok
  end

  @impl true
  def handle_events(jobs, _from, state) do
    # TODO: filter and transform jobs:
    # 1. Keep only jobs that are maps with :type and :args keys
    # 2. Add validated_at: System.monotonic_time() to each valid job
    # 3. Log (or discard) invalid jobs
    # Return {:noreply, valid_jobs, state}
    #
    # HINT:
    # valid_jobs =
    #   jobs
    #   |> Enum.filter(&valid_job?/1)
    #   |> Enum.map(&Map.put(&1, :validated_at, System.monotonic_time()))
    # {:noreply, valid_jobs, state}
  end

  defp valid_job?(%{type: _, args: _}), do: true
  defp valid_job?(_), do: false
end
```

### Step 3: `lib/task_queue/pipeline/job_consumer.ex`

```elixir
defmodule TaskQueue.Pipeline.JobConsumer do
  @moduledoc """
  GenStage consumer that executes validated jobs.

  Subscribes to `JobValidator` with a max_demand that limits
  how many jobs are in-flight at once, providing backpressure.
  """

  use GenStage

  @max_demand 5

  def start_link(opts \\ []) do
    GenStage.start_link(__MODULE__, :ok, name: __MODULE__)
  end

  @impl true
  def init(:ok) do
    # TODO: subscribe to JobValidator with max_demand: @max_demand
    # Return {:consumer, :ok, subscribe_to: [{JobValidator, max_demand: @max_demand}]}
    # HINT: {:consumer, :ok, subscribe_to: [{TaskQueue.Pipeline.JobValidator, max_demand: @max_demand}]}
  end

  @impl true
  def handle_events(jobs, _from, state) do
    # TODO: execute each job using TaskQueue.Worker.execute/1
    # Log the result for each job (success or failure)
    # Return {:noreply, [], state}  — consumers always return empty events list
    #
    # HINT:
    # Enum.each(jobs, fn job ->
    #   case TaskQueue.Worker.execute(job) do
    #     {:ok, result}    -> :logger.info("Job done: #{inspect(result)}")
    #     {:error, reason} -> :logger.warning("Job failed: #{inspect(reason)}")
    #   end
    # end)
    # {:noreply, [], state}
  end
end
```

### Step 4: Given tests — must pass without modification

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

### Step 5: Run the tests

```bash
mix deps.get
mix test test/task_queue/genstage_test.exs --trace
```

---

## Trade-off analysis

| Aspect | GenStage pipeline | Plain `send/2` | `Task.async_stream` |
|--------|-------------------|----------------|---------------------|
| Backpressure | built-in via demand | none | bounded by collection size |
| Continuous event streams | yes | yes (unbounded) | no — requires known collection |
| Memory under burst | bounded by `max_demand` | unbounded mailbox | bounded by stream chunk |
| Complexity | high (3 modules, subscriptions) | low | low |
| Best for | long-running pipelines | low-volume messaging | batch processing |

Reflection question: a `JobValidator` with `max_demand: 10` subscribing to a `JobProducer` means the validator requests 10 events at a time. What happens if the validator is slow — does it block the producer from accepting new work from the queue? Trace the demand flow.

---

## Common production mistakes

**1. Forgetting to subscribe stages to each other**

`use GenStage` defines the callbacks, but does not wire the pipeline. Stages must be connected via `GenStage.sync_subscribe/3` or via the `subscribe_to:` option in `init/1`. Without subscriptions, no events flow.

**2. Returning events from a consumer**

```elixir
# Wrong — consumers must return an empty list
def handle_events(events, _from, state) do
  processed = Enum.map(events, &process/1)
  {:noreply, processed, state}  # GenStage will raise
end

# Right
def handle_events(events, _from, state) do
  Enum.each(events, &process/1)
  {:noreply, [], state}
end
```

**3. Setting `max_demand: 1` expecting sequential processing**

`max_demand: 1` processes one event at a time per consumer, but does not guarantee wall-clock ordering across multiple consumers subscribed to the same producer. For guaranteed ordering, use a single consumer.

**4. Not tracking unmet demand in the producer**

If `handle_demand` is called with demand 10 but only 3 items are available, return 3 items and store the unmet 7. When new items arrive (via `handle_info` from the queue), fulfill the pending demand:

```elixir
def handle_info({:new_jobs, jobs}, %{pending_demand: demand} = state) do
  to_emit = Enum.take(jobs, demand)
  {:noreply, to_emit, %{state | pending_demand: demand - length(to_emit)}}
end
```

**5. Using `GenStage.call/2` on a stage from within its own callbacks**

A GenStage stage is a GenServer. Calling `GenServer.call/2` on itself causes a deadlock. Use `GenStage.cast/2` or accumulate state in `handle_events` callbacks.

---

## Resources

- [GenStage — official hex package](https://hexdocs.pm/gen_stage/GenStage.html)
- [Introduction to GenStage — José Valim](https://elixir-lang.org/blog/2016/07/14/announcing-genstage/)
- [Flow: built on GenStage for parallel data processing](https://hexdocs.pm/flow/Flow.html)
- [Backpressure explained — Saša Jurić](https://www.theerlangelist.com/article/gen_stage)
