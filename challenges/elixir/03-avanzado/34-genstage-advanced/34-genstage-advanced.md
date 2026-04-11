# GenStage — Dispatchers, ConsumerSupervisor, and Demand Buffering

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

`api_gateway` needs an internal event processing pipeline. The gateway emits three event
types (`payment`, `signup`, `click`) that must be routed to specialized consumers. Some
events require CPU-bound work (fraud scoring) that should run in parallel. The event
source is an external webhook receiver that delivers events in bursts — the producer does
not always have data ready when consumers ask for it.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       └── middleware/
│           ├── event_router.ex         # ← you implement this (Exercise 1)
│           ├── parallel_processor.ex   # ← and this (Exercise 2)
│           └── async_producer.ex       # ← and this (Exercise 3)
├── test/
│   └── api_gateway/
│       └── middleware/
│           └── event_pipeline_test.exs # given tests
└── mix.exs
```

---

## The business problem

Three consumer teams need events from the gateway:
1. **Payments team** — needs all `:payment` events for fraud scoring (CPU-bound, 50ms each)
2. **Growth team** — needs all `:signup` events for onboarding flows
3. **Analytics team** — needs all `:click` events, can process them in batches

Additionally, the external webhook source delivers events unpredictably. When the webhook
arrives, the GenStage consumers have already demanded data. The producer must buffer
the demand and satisfy it when the data arrives.

---

## GenStage demand model — the core contract

GenStage is a demand-driven pipeline. **Consumers pull; producers push only when asked.**

```
Consumer asks for 10 events  ->  Producer emits up to 10 events
Consumer asks for 10 more    ->  Producer emits up to 10 more (or buffers demand if empty)
```

This model provides natural backpressure: a slow consumer automatically slows its producer
by not asking for more. No separate rate limiting is needed at the pipeline level.

The three built-in dispatchers define how events are routed to consumers:

| Dispatcher | Routing | Ordering guarantee |
|---|---|---|
| `DemandDispatcher` (default) | Round-robin to demanding consumers | None across consumers |
| `BroadcastDispatcher` | Every event to every consumer | Per-consumer |
| `PartitionDispatcher` | One consumer per partition key | Within partition |

---

## Implementation

### Step 1: `mix.exs`

```elixir
defp deps do
  [
    {:gen_stage, "~> 1.2"}
  ]
end
```

### Step 2: `lib/api_gateway/middleware/event_router.ex`

The EventRouter uses `PartitionDispatcher` to route events by type. The hash function
extracts the first element of the event tuple as the partition key. Each consumer subscribes
with `partition: :payment` (or `:signup`, `:click`) and receives only events of that type.

```elixir
defmodule ApiGateway.Middleware.EventRouter do
  @moduledoc """
  Routes events to specialized consumers using PartitionDispatcher.

  Topology:
    EventProducer
      |-- PaymentConsumer  (partition: :payment)
      |-- SignupConsumer   (partition: :signup)
      +-- ClickConsumer    (partition: :click)

  Each consumer receives only events of its type.
  The partition key is extracted from the event tuple: {:payment, data} -> :payment.
  """

  defmodule EventProducer do
    use GenStage

    def start_link(events) do
      GenStage.start_link(__MODULE__, events, name: __MODULE__)
    end

    def init(events) do
      {:producer, events,
       dispatcher:
         {GenStage.PartitionDispatcher,
          partitions: [:payment, :signup, :click],
          hash: fn event -> {event, elem(event, 0)} end}}
    end

    def handle_demand(demand, events) do
      {to_emit, remaining} = Enum.split(events, demand)
      {:noreply, to_emit, remaining}
    end
  end

  defmodule PaymentConsumer do
    use GenStage

    def start_link(producer) do
      GenStage.start_link(__MODULE__, producer, name: __MODULE__)
    end

    def init(producer) do
      {:consumer, :ok, subscribe_to: [{producer, partition: :payment, max_demand: 10}]}
    end

    def handle_events(events, _from, state) do
      Enum.each(events, fn {:payment, data} ->
        IO.puts("[PaymentConsumer] Processing payment: #{inspect(data)}")
      end)

      {:noreply, [], state}
    end
  end

  defmodule SignupConsumer do
    use GenStage

    def start_link(producer) do
      GenStage.start_link(__MODULE__, producer, name: __MODULE__)
    end

    def init(producer) do
      {:consumer, :ok, subscribe_to: [{producer, partition: :signup, max_demand: 10}]}
    end

    def handle_events(events, _from, state) do
      Enum.each(events, fn {:signup, data} ->
        IO.puts("[SignupConsumer] Processing signup: #{inspect(data)}")
      end)

      {:noreply, [], state}
    end
  end

  defmodule ClickConsumer do
    use GenStage

    def start_link(producer) do
      GenStage.start_link(__MODULE__, producer, name: __MODULE__)
    end

    def init(producer) do
      {:consumer, :ok, subscribe_to: [{producer, partition: :click, max_demand: 10}]}
    end

    def handle_events(events, _from, state) do
      Enum.each(events, fn {:click, data} ->
        IO.puts("[ClickConsumer] Processing click batch: #{inspect(data)}")
      end)

      {:noreply, [], state}
    end
  end
end
```

### Step 3: `lib/api_gateway/middleware/parallel_processor.ex`

The `ConsumerSupervisor` spawns one Task per event. `max_demand` controls the maximum number
of concurrent Task processes — this is the backpressure mechanism. Workers are `:temporary`
because restarting a failed worker would re-process the event, violating at-most-once semantics.

```elixir
defmodule ApiGateway.Middleware.ParallelProcessor do
  @moduledoc """
  ConsumerSupervisor that spawns one Task per event.

  max_demand controls the maximum number of concurrent Task processes.
  Workers are :temporary — ConsumerSupervisor does not restart them.
  This is required: restarting a failed worker would re-process the event,
  violating the exactly-once semantics expected by the fraud scorer.
  """

  defmodule FraudJobProducer do
    use GenStage

    def start_link(jobs) do
      GenStage.start_link(__MODULE__, jobs, name: __MODULE__)
    end

    def init(jobs) do
      {:producer, jobs}
    end

    def handle_demand(demand, jobs) do
      {to_emit, remaining} = Enum.split(jobs, demand)
      {:noreply, to_emit, remaining}
    end
  end

  defmodule FraudScoringWorker do
    use Task, restart: :temporary

    def start_link(job) do
      Task.start_link(__MODULE__, :run, [job])
    end

    def run(job) do
      IO.puts("Scoring job #{job.id} (#{job.duration_ms}ms)")
      :timer.sleep(job.duration_ms)
      IO.puts("Job #{job.id} scored")
    end
  end

  defmodule FraudSupervisor do
    use ConsumerSupervisor

    def start_link(opts) do
      ConsumerSupervisor.start_link(__MODULE__, opts, name: __MODULE__)
    end

    def init(_opts) do
      children = [
        %{
          id: FraudScoringWorker,
          start: {FraudScoringWorker, :start_link, []},
          restart: :temporary
        }
      ]

      opts = [
        strategy: :one_for_one,
        subscribe_to: [
          {FraudJobProducer, max_demand: 5}
        ]
      ]

      ConsumerSupervisor.init(children, opts)
    end
  end
end
```

### Step 4: `lib/api_gateway/middleware/async_producer.ex`

The AsyncProducer handles the case where consumer demand arrives before data is available.
It maintains a `{buffer, pending_demand}` tuple as state. The invariant is:
`pending_demand * length(buffer) == 0` — either there is unsatisfied demand (buffer empty)
or there are buffered events (demand fully satisfied), never both simultaneously.

When `handle_demand` is called, it adds to the pending demand and emits whatever is in the
buffer. When `handle_info({:new_data, items})` arrives, it adds items to the buffer and
emits as much as pending demand allows.

```elixir
defmodule ApiGateway.Middleware.AsyncProducer do
  @moduledoc """
  Producer that satisfies demand asynchronously.

  The webhook receiver delivers events via push (handle_info). Consumers
  demand events before they arrive. The producer must buffer the demand
  and satisfy it when the webhook data arrives.

  Invariant: pending_demand * buffer_size == 0.
  Either you have unsatisfied demand (buffer is empty) OR you have buffered
  events (demand was already satisfied). Both simultaneously is a bug.

  State: {buffer, pending_demand}
    buffer:         events available but not yet demanded
    pending_demand: demand received but not yet satisfied
  """
  use GenStage

  def start_link do
    GenStage.start_link(__MODULE__, {[], 0}, name: __MODULE__)
  end

  @doc "Inject events from the webhook receiver (simulates external push)."
  def push(items) when is_list(items) do
    send(__MODULE__, {:new_data, items})
  end

  @impl true
  def init(state) do
    {:producer, state}
  end

  @impl true
  def handle_demand(demand, {buffer, pending_demand}) do
    total_demand = demand + pending_demand
    {to_emit, remaining_buffer} = Enum.split(buffer, total_demand)
    remaining_demand = total_demand - length(to_emit)
    {:noreply, to_emit, {remaining_buffer, remaining_demand}}
  end

  @impl true
  def handle_info({:new_data, items}, {buffer, pending_demand}) do
    new_buffer = buffer ++ items
    {to_emit, remaining_buffer} = Enum.split(new_buffer, pending_demand)
    remaining_demand = pending_demand - length(to_emit)
    {:noreply, to_emit, {remaining_buffer, remaining_demand}}
  end

  @impl true
  def handle_info(_msg, state), do: {:noreply, [], state}
end
```

### Step 5: Given tests — must pass without modification

```elixir
# test/api_gateway/middleware/event_pipeline_test.exs
defmodule ApiGateway.Middleware.EventPipelineTest do
  use ExUnit.Case, async: false

  alias ApiGateway.Middleware.AsyncProducer

  describe "AsyncProducer demand buffering" do
    setup do
      {:ok, _} = AsyncProducer.start_link()

      consumer_pid = start_consumer(AsyncProducer)
      {:ok, consumer: consumer_pid}
    end

    test "all pushed events are eventually received", %{consumer: consumer} do
      Process.sleep(50)  # let consumer demand accumulate

      AsyncProducer.push([1, 2, 3])
      Process.sleep(50)
      AsyncProducer.push([4, 5])
      Process.sleep(100)

      received = InlineConsumer.received(consumer)
      assert Enum.sort(received) == [1, 2, 3, 4, 5]
    end

    test "buffer never holds items when demand is pending", %{consumer: _} do
      # After consumer subscribes, it immediately demands items.
      # The buffer should be empty — demand is pending.
      Process.sleep(20)
      {buffer, pending} = :sys.get_state(AsyncProducer)
      # Invariant: either buffer is empty OR pending is 0, never both non-zero
      assert buffer == [] or pending == 0
    end
  end

  defp start_consumer(producer) do
    # Inline consumer module — collects received events in state for inspection.
    defmodule InlineConsumer do
      use GenStage

      def start_link(producer) do
        GenStage.start_link(__MODULE__, producer)
      end

      def received(pid), do: GenStage.call(pid, :received)

      def init(producer) do
        {:consumer, [], subscribe_to: [{producer, min_demand: 0, max_demand: 5}]}
      end

      def handle_events(events, _from, received) do
        {:noreply, [], received ++ events}
      end

      def handle_call(:received, _from, received) do
        {:reply, received, received}
      end
    end

    {:ok, pid} = InlineConsumer.start_link(producer)
    pid
  end
end
```

### Step 6: Run the tests

```bash
mix test test/api_gateway/middleware/event_pipeline_test.exs --trace
```

---

## Trade-off analysis

| Aspect | `PartitionDispatcher` | `BroadcastDispatcher` | `DemandDispatcher` |
|--------|----------------------|----------------------|-------------------|
| Routing | One consumer per key | All consumers | Round-robin |
| Use case | Sharding, type-based routing | Fan-out, cache invalidation | Load balancing |
| Ordering | Within partition | Per consumer | None |
| Consumer subscription | `partition: key` option | Any | Any |

| Aspect | `ConsumerSupervisor` | Manual `Task.async_stream` |
|--------|---------------------|--------------------------|
| Back-pressure | Automatic via `max_demand` | Manual — you control concurrency |
| Failure isolation | Per-event process | Per-task |
| Worker restart | Never (`:temporary`) | Not applicable |
| Best for | Unbounded event streams | Finite collections |

Reflection: why is `restart: :temporary` required on `ConsumerSupervisor` workers?
What would happen if a worker was `:permanent` and crashed?

---

## Common production mistakes

**1. `PartitionDispatcher` hash function returning only the key**
The hash function must return `{event, partition_key}`, not just the key. Returning
only the key silently drops the event — the dispatcher has no event to route.

**2. `ConsumerSupervisor` workers with `restart: :permanent`**
If a worker crashes and is restarted with the original event as its argument, the event
is processed twice. The `ConsumerSupervisor` contract assumes `:temporary` workers.

**3. Violating the demand-buffer invariant**
If `pending_demand > 0` and `buffer` is non-empty simultaneously, events are being
held back unnecessarily. Always emit from the buffer whenever pending demand exists.

**4. Not using `sync_subscribe` when startup order matters**
If the producer starts emitting before consumers have subscribed, the first events are
dropped. Use `GenStage.sync_subscribe/3` to ensure the consumer is subscribed before
the producer processes its first `handle_demand`.

---

## Resources

- [GenStage dispatchers — HexDocs](https://hexdocs.pm/gen_stage/GenStage.html#module-dispatchers)
- [ConsumerSupervisor — HexDocs](https://hexdocs.pm/gen_stage/ConsumerSupervisor.html)
- [Announcing GenStage — Jose Valim](https://elixir-lang.org/blog/2016/07/14/announcing-genstage/)
- [GenStage and Flow — ElixirConf 2016](https://www.youtube.com/watch?v=XPlXNUXmio8)
