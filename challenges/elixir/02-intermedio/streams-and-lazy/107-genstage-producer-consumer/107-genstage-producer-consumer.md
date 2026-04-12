# GenStage `ProducerConsumer` — a 3-stage pipeline with a middle transformer

**Project**: `producer_consumer` — a Producer that emits integers, a middle
stage that squares them, and a Consumer that collects the results, all
with demand propagated up the chain.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Project context

A two-stage Producer → Consumer pipeline (exercise 27) is the base case.
Production pipelines almost always have *middle stages* that transform,
filter, enrich, or aggregate events — and they must both accept demand
from downstream and ask for events upstream. That's the `ProducerConsumer`
type.

The tricky thing about `ProducerConsumer` is that demand isn't 1:1 — a
single event in might produce zero or many events out (filter, expand).
GenStage handles this internally; your job is to produce a list of output
events from a list of input events.

Project structure:

```
producer_consumer/
├── lib/
│   ├── producer_consumer.ex
│   ├── producer_consumer/producer.ex
│   ├── producer_consumer/squarer.ex
│   └── producer_consumer/consumer.ex
├── test/
│   └── producer_consumer_test.exs
└── mix.exs
```

---

## Core concepts

### 1. Three stage types, revisited

| Type               | `init` returns tag    | Required callback       |
|--------------------|-----------------------|-------------------------|
| Producer           | `:producer`           | `handle_demand/2`       |
| ProducerConsumer   | `:producer_consumer`  | `handle_events/3`       |
| Consumer           | `:consumer`           | `handle_events/3`       |

A `ProducerConsumer` does NOT implement `handle_demand/2`. Demand from
downstream is automatically forwarded upstream by GenStage.

### 2. `handle_events/3` return shape differs per type

```elixir
# In a Consumer:
def handle_events(events, _from, state), do: {:noreply, [], state}

# In a ProducerConsumer:
def handle_events(events, _from, state) do
  transformed = Enum.map(events, &transform/1)
  {:noreply, transformed, state}
end
```

Consumers return an empty output list (they're a sink). ProducerConsumers
return the transformed events — GenStage routes them to whoever is
subscribed.

### 3. Demand propagation in a chain

```
Consumer ──demand 500──▶ Squarer ──demand 500──▶ Producer
Producer ──[1..500]────▶ Squarer ──[1, 4, 9, ...]──▶ Consumer
```

The Squarer doesn't compute its own demand. When the Consumer asks for
500, GenStage asks the Producer for 500 on the Squarer's behalf. This is
the key back-pressure guarantee: a slow Consumer throttles the Producer
automatically.

### 4. ProducerConsumer can fan in/out with dispatchers

By default each stage has a `DemandDispatcher`. For fan-out (one producer,
N consumers load-balanced) use `PartitionDispatcher` or
`BroadcastDispatcher`. We'll stick with the default here.

---

## Implementation

### Step 1: Create the project

```bash
mix new producer_consumer --sup
cd producer_consumer
```

Add `gen_stage` to `mix.exs`:

```elixir
defp deps, do: [{:gen_stage, "~> 1.2"}]
```

Then `mix deps.get`.

### Step 2: `lib/producer_consumer/producer.ex`

```elixir
defmodule ProducerConsumer.Producer do
  @moduledoc "Emits consecutive integers starting at 1."

  use GenStage

  def start_link(_ \\ []), do: GenStage.start_link(__MODULE__, 1, name: __MODULE__)

  @impl true
  def init(counter), do: {:producer, counter}

  @impl true
  def handle_demand(demand, counter) when demand > 0 do
    events = Enum.to_list(counter..(counter + demand - 1))
    {:noreply, events, counter + demand}
  end
end
```

### Step 3: `lib/producer_consumer/squarer.ex`

```elixir
defmodule ProducerConsumer.Squarer do
  @moduledoc """
  The middle stage: receives integers from the Producer, squares them,
  and forwards to anyone subscribed. No `handle_demand` needed — GenStage
  forwards demand from downstream upstream automatically.
  """

  use GenStage

  def start_link(_ \\ []), do: GenStage.start_link(__MODULE__, :ok, name: __MODULE__)

  @impl true
  def init(:ok) do
    # subscribe_to must come from init — we want to be connected at startup.
    {:producer_consumer, :no_state,
     subscribe_to: [{ProducerConsumer.Producer, max_demand: 20, min_demand: 10}]}
  end

  @impl true
  def handle_events(events, _from, state) do
    # 1:1 transform — squaring. Could also filter (return fewer) or expand
    # (return more) events. Return shape is the same.
    {:noreply, Enum.map(events, &(&1 * &1)), state}
  end
end
```

### Step 4: `lib/producer_consumer/consumer.ex`

```elixir
defmodule ProducerConsumer.Consumer do
  @moduledoc "Forwards squared integers to a notify pid for test observability."

  use GenStage

  def start_link(notify_pid) when is_pid(notify_pid),
    do: GenStage.start_link(__MODULE__, notify_pid)

  @impl true
  def init(notify_pid) do
    {:consumer, notify_pid,
     subscribe_to: [{ProducerConsumer.Squarer, max_demand: 20, min_demand: 10}]}
  end

  @impl true
  def handle_events(events, _from, notify_pid) do
    for event <- events, do: send(notify_pid, {:squared, event})
    {:noreply, [], notify_pid}
  end
end
```

### Step 5: `lib/producer_consumer.ex`

```elixir
defmodule ProducerConsumer do
  @moduledoc "Starts the 3-stage pipeline. Returns the Consumer pid."

  alias ProducerConsumer.{Producer, Squarer, Consumer}

  def start_pipeline(notify_pid) do
    {:ok, _p} = Producer.start_link()
    {:ok, _pc} = Squarer.start_link()
    {:ok, c} = Consumer.start_link(notify_pid)
    {:ok, c}
  end
end
```

### Step 6: `test/producer_consumer_test.exs`

```elixir
defmodule ProducerConsumerTest do
  use ExUnit.Case, async: false

  setup do
    {:ok, consumer} = ProducerConsumer.start_pipeline(self())

    on_exit(fn ->
      for name <- [ProducerConsumer.Producer, ProducerConsumer.Squarer] do
        if pid = Process.whereis(name), do: GenStage.stop(pid)
      end

      if Process.alive?(consumer), do: GenStage.stop(consumer)
    end)

    :ok
  end

  test "pipeline produces squared integers in order" do
    # Producer emits 1, 2, 3, ... squarer emits 1, 4, 9, ... consumer forwards.
    for expected <- [1, 4, 9, 16, 25, 36, 49, 64, 81, 100] do
      assert_receive {:squared, ^expected}, 1_000
    end
  end

  test "back-pressure works across the full chain" do
    collected =
      for _ <- 1..50 do
        assert_receive {:squared, n}, 2_000
        n
      end

    expected = for i <- 1..50, do: i * i
    assert collected == expected
  end
end
```

### Step 7: Run

```bash
mix test
```

---

## Trade-offs and production gotchas

**1. A ProducerConsumer's `handle_events` should NOT block**
It runs in the stage's single process. Slow work blocks both the inbound
demand and the outbound emission. For CPU-heavy transforms, use `Flow`
(multiple partitions) or spawn tasks from inside the handler and use a
GenStage with `:consumer_supervisor` semantics.

**2. Output event count differs from input → double-check buffers**
If you expand each input into N outputs (fan-out), the downstream buffer
fills N× faster. Configure `max_demand` accordingly — otherwise you'll
emit more than downstream can absorb in a single tick.

**3. Error handling is per-stage**
If `handle_events` raises, the stage crashes. Under a supervisor it
restarts, but events in flight at the time are lost. For retriable work,
catch errors inside `handle_events` and emit `{:error, event}` markers,
letting a later stage decide what to do.

**4. Subscriber lifecycle**
If a ProducerConsumer dies and restarts, it must re-subscribe. Handling
that manually is tedious; this is exactly why you put everything under a
`Supervisor` with `:rest_for_one` strategy — downstream stages get
restarted too, rewiring automatically.

**5. Don't confuse `min_demand` / `max_demand` of the two subscriptions**
A ProducerConsumer has *two* demand windows: the one it exposes upward
(configured via its subscribers' options) and the one it configures
downward (in its `subscribe_to:`). They don't have to match but they
interact — a tiny upward window on a huge downward one starves downstream.

**6. When NOT to use `ProducerConsumer`**
- Your middle stage is a pure function with no state and no side effects
  — just use `Flow`, which generates the plumbing for you.
- You need fan-out by key — use a `PartitionDispatcher` from the producer
  directly, or skip straight to `Broadway` / `Flow`.
- You only have one source and one sink and no transformation — a plain
  `Producer` → `Consumer` is simpler.

---

## Resources

- [`GenStage` — hexdocs](https://hexdocs.pm/gen_stage/GenStage.html)
- [GenStage README — ProducerConsumer examples](https://github.com/elixir-lang/gen_stage#producerconsumer)
- [José Valim — "Announcing GenStage"](https://elixir-lang.org/blog/2016/07/14/announcing-genstage/)
- [`Flow`](https://hexdocs.pm/flow/Flow.html) — when your middle stage would benefit from parallel partitions
- [`Broadway`](https://hexdocs.pm/broadway/Broadway.html) — for queue-ingestion pipelines built on this pattern
