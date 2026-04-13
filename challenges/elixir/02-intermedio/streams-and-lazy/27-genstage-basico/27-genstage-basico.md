# GenStage basics — a Producer and a Consumer driven by demand

**Project**: `genstage_intro` — the smallest possible GenStage pipeline:
one `Producer` emitting integers, one `Consumer` pulling them, with
back-pressure driven by consumer demand.

---

## Project context

GenStage is the Elixir building block for **demand-driven** data pipelines.
Unlike a plain GenServer where the producer decides when to send data
(and can overwhelm a slow consumer), GenStage flips the flow: the consumer
asks for N items, the producer emits at most N items. This is back-pressure
at the protocol level.

It's the foundation under `Flow` (high-level parallel pipelines) and
`Broadway` (data ingestion). Before those, you need the two-stage base
case: understand `handle_demand/2` in a producer and the `subscribe_to:`
option in a consumer, and the rest generalizes.

Project structure:

```
genstage_intro/
├── lib/
│   ├── genstage_intro.ex
│   ├── genstage_intro/producer.ex
│   └── genstage_intro/consumer.ex
├── test/
│   └── genstage_intro_test.exs
└── mix.exs
```

---

## Core concepts

### 1. Three stage types

- **Producer** — only emits events. Implements `handle_demand/2`.
- **Consumer** — only receives events. Implements `handle_events/3`.
- **ProducerConsumer** — both: emits events downstream AND receives from upstream.

A pipeline is a chain: `Producer -> [ProducerConsumer...] -> Consumer`.

### 2. Demand is an integer request

When a consumer subscribes to a producer, it asks "send me up to N events".
The producer replies with `{:noreply, events, state}` where `events` has
at most N items. If the producer has fewer events ready, it sends what it
has and remembers the remaining demand for later. **The producer never
pushes events that weren't demanded.**

```
Consumer ──demand 500──▶ Producer
Producer ──events [1..500]──▶ Consumer
Consumer ──demand 500──▶ Producer  (once it's processed them)
```

### 3. `min_demand` and `max_demand`

When a consumer subscribes, it configures a window:

- `max_demand` — high-water mark; initial ask.
- `min_demand` — low-water mark; when buffer drains below this, ask again.

Defaults are `500` and `250`. Smaller values give tighter back-pressure
and smaller batches; larger values give higher throughput at the cost of
more in-flight events.

### 4. The producer's state *drives* demand

A producer that can't satisfy demand returns an empty `events` list and
stores the outstanding demand in state. When more data is available
(timer, async fetch, upstream event), it dispatches what it has against
the stored demand. **Never emit more than was demanded** — downstream
buffers aren't prepared for it.

---

## Design decisions

**Option A — Plain GenServer with `handle_info` push**
- Pros: fewer moving parts; no extra dependency.
- Cons: no back-pressure — a fast producer can flood a slow consumer's mailbox until the VM runs out of memory.

**Option B — GenStage with demand-driven producer + consumer** (chosen)
- Pros: back-pressure is protocol-level; consumer pulls exactly `max_demand` events; composes cleanly with Flow and Broadway.
- Cons: adds a dependency; two modules instead of one; you must reason about outstanding demand on slow sources.

→ Chose **B** because the whole point is the demand-driven primitive. Every realistic pipeline needs back-pressure; learning it once pays for Flow and Broadway later.

---

## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  [
    {:gen_stage, "~> 1.2"}
  ]
end
```

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.


```bash
mix new genstage_intro --sup
cd genstage_intro
mix deps.get
```

### Step 2: `lib/genstage_intro/producer.ex`

**Objective**: Implement `producer.ex` — the lazy operator whose resource and memory profile only becomes visible when the stream is actually run.


```elixir
defmodule GenstageIntro.Producer do
  @moduledoc """
  A trivial counter producer. It emits consecutive integers starting at 0,
  as many per batch as the consumer demands.
  """

  use GenStage

  @spec start_link(non_neg_integer()) :: GenServer.on_start()
  def start_link(initial \\ 0) do
    GenStage.start_link(__MODULE__, initial, name: __MODULE__)
  end

  @impl true
  def init(counter), do: {:producer, counter}

  @impl true
  def handle_demand(demand, counter) when demand > 0 do
    # Emit exactly `demand` events: counter, counter+1, ..., counter+demand-1.
    # The producer contract is "emit AT MOST demand"; we always have more
    # integers to hand out, so we satisfy the full request.
    events = Enum.to_list(counter..(counter + demand - 1))
    {:noreply, events, counter + demand}
  end
end
```

### Step 3: `lib/genstage_intro/consumer.ex`

**Objective**: Implement `consumer.ex` — the lazy operator whose resource and memory profile only becomes visible when the stream is actually run.


```elixir
defmodule GenstageIntro.Consumer do
  @moduledoc """
  Subscribes to `Producer` and forwards every event to a caller-supplied pid
  as `{:consumed, event}`. Using a forwarder pid (rather than printing) keeps
  the consumer deterministic and testable.
  """

  use GenStage

  @spec start_link(pid()) :: GenServer.on_start()
  def start_link(notify_pid) when is_pid(notify_pid) do
    GenStage.start_link(__MODULE__, notify_pid)
  end

  @impl true
  def init(notify_pid) do
    # Subscribe at init. Small max_demand keeps batches small so tests can
    # observe back-pressure clearly.
    {:consumer, notify_pid,
     subscribe_to: [{GenstageIntro.Producer, max_demand: 10, min_demand: 5}]}
  end

  @impl true
  def handle_events(events, _from, notify_pid) do
    for event <- events, do: send(notify_pid, {:consumed, event})
    # Consumers return {:noreply, [], state} — they do not emit events themselves.
    {:noreply, [], notify_pid}
  end
end
```

### Step 4: `lib/genstage_intro.ex` — start the pipeline

**Objective**: Edit `genstage_intro.ex` — start the pipeline, exposing the lazy operator whose resource and memory profile only becomes visible when the stream is actually run.


```elixir
defmodule GenstageIntro do
  @moduledoc """
  Starts a Producer and a Consumer wired together. Returns the consumer pid
  so tests can stop it deterministically.
  """

  alias GenstageIntro.{Producer, Consumer}

  @spec start_pipeline(pid()) :: {:ok, pid()}
  def start_pipeline(notify_pid) do
    {:ok, _producer} = Producer.start_link(0)
    {:ok, consumer} = Consumer.start_link(notify_pid)
    {:ok, consumer}
  end
end
```

### Step 5: `test/genstage_intro_test.exs`

**Objective**: Write `genstage_intro_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule GenstageIntroTest do
  use ExUnit.Case, async: false

  setup do
    {:ok, consumer} = GenstageIntro.start_pipeline(self())

    on_exit(fn ->
      # Stop producer and consumer between tests so each test starts fresh.
      if Process.whereis(GenstageIntro.Producer),
        do: GenStage.stop(GenstageIntro.Producer)

      if Process.alive?(consumer), do: GenStage.stop(consumer)
    end)

    :ok
  end

  test "consumer receives a stream of events starting at 0" do
    # With max_demand: 10, the first batch has at most 10 events.
    assert_receive {:consumed, 0}, 500
    assert_receive {:consumed, 1}, 500
    # Let a few more come in.
    for expected <- 2..9 do
      assert_receive {:consumed, ^expected}, 500
    end
  end

  test "back-pressure: events keep flowing as consumer drains" do
    # Collect 50 events — this forces multiple demand rounds.
    received =
      for _ <- 1..50 do
        assert_receive {:consumed, n}, 1_000
        n
      end

    # They arrive strictly in order from the producer.
    assert received == Enum.to_list(0..49)
  end
end
```

### Step 6: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
```

### Why this works

The consumer subscribes with `max_demand: 10`, which sends an initial `{:"$gen_producer", {consumer, ref}, {:ask, 10}}` message to the producer. The producer's `handle_demand/2` is invoked with `demand=10`, emits ten integers, and returns them in `{:noreply, events, state}`. Once the consumer drains below `min_demand: 5`, it asks again. The producer never sends events that weren't asked for — that is the entire back-pressure contract.

---

<!-- benchmark N/A: GenStage throughput depends heavily on downstream work; measure in your own environment with different workloads -->


## Key Concepts: Producer-Consumer Pipelines with Backpressure

GenStage is a framework for building multi-stage pipelines where each stage is a GenServer that either produces events (producer), consumes them (consumer), or both (producer-consumer). The key innovation is **demand-driven backpressure**: a consumer tells its upstream producer how many events it can handle, and the producer respects that limit.

Without backpressure, a fast producer can overwhelm a slow consumer, causing unbounded memory growth. GenStage's solution: consumers request events explicitly, and the producer only delivers up to that demand. This requires thinking differently from simple message passing: instead of sending and hoping the receiver is ready, you coordinate. Real-world use cases: streaming files from disk, processing message queues, or splitting a large dataset across worker processes. The learning curve is steeper than Enum/Stream, but for production data pipelines, it's essential.


## Trade-offs and production gotchas

**1. Demand is the *only* back-pressure signal**
Don't buffer events inside the producer "just in case". If a downstream is
slow, demand slows and your producer naturally pauses. Buffering defeats
the whole design.

**2. `handle_demand` must be fast**
It runs in the producer process; while it executes, no new demand is
served. If your source is slow (HTTP, DB), fetch in a separate task and
reply with `{:noreply, [], state}`, storing outstanding demand. Dispatch
when the data arrives.

**3. Beware unbounded demand on fast consumers**
`max_demand: 10_000` + a fast producer = 10_000 events in flight per
subscription. RAM pressure can be large. Start with defaults and tune
downward only when measurement says to.

**4. Producer termination does not drain the consumer**
When a producer stops, its subscribers get a cancel event. Any events
still in transit are delivered; anything *not yet* produced is lost. If
you need graceful drain, wire a signal through the data itself (e.g. a
final `:end_of_stream` event) rather than relying on process death.

**5. One producer per "logical source"**
The classic pitfall is starting one producer per request. Producers are
long-lived; request-lifecycle work belongs in a `Task` or `Broadway`
pipeline, not a fresh `GenStage`.

**6. When NOT to use GenStage**
- Event rate fits a single process comfortably and you don't need
  back-pressure → a plain GenServer with `handle_info` is simpler.
- You want parallel map/reduce over data → use `Flow`, which is GenStage
  configured for you.
- You want data ingestion from queues (SQS, Kafka, RabbitMQ) → use
  `Broadway`, which is GenStage pre-wired with acknowledgements and rate
  limiting.

---

## Reflection

- Your producer wraps a slow HTTP API (200 ms per call, batches of 50). Where would you put the async fetch so `handle_demand/2` never blocks the producer process? Sketch the state machine.
- A consumer with `max_demand: 10_000` and `min_demand: 5_000` feels fast but spikes RAM. How do you decide the right window size in production? What signal tells you to reduce it?

---

## Executable Example

Copy the code below into a file (e.g., `solution.exs`) and run with `elixir solution.exs`:

```elixir
defmodule Main do
  defmodule GenstageIntro.Producer do
    @moduledoc """
    A trivial counter producer. It emits consecutive integers starting at 0,
    as many per batch as the consumer demands.
    """

    use GenStage

    @spec start_link(non_neg_integer()) :: GenServer.on_start()
    def start_link(initial \\ 0) do
      GenStage.start_link(__MODULE__, initial, name: __MODULE__)
    end

    @impl true
    def init(counter), do: {:producer, counter}

    @impl true
    def handle_demand(demand, counter) when demand > 0 do
      # Emit exactly `demand` events: counter, counter+1, ..., counter+demand-1.
      # The producer contract is "emit AT MOST demand"; we always have more
      # integers to hand out, so we satisfy the full request.
      events = Enum.to_list(counter..(counter + demand - 1))
      {:noreply, events, counter + demand}
    end
  end

  def main do
    IO.puts("GenstageIntro OK")
  end

end

Main.main()
```


## Resources

- [`GenStage` — hexdocs](https://hexdocs.pm/gen_stage/GenStage.html)
- [`gen_stage` — GitHub README](https://github.com/elixir-lang/gen_stage)
- [José Valim — "Announcing GenStage"](https://elixir-lang.org/blog/2016/07/14/announcing-genstage/) — original blog post with design rationale
- [`Flow`](https://hexdocs.pm/flow/Flow.html) — high-level, parallel pipelines on top of GenStage
- [`Broadway`](https://hexdocs.pm/broadway/Broadway.html) — data ingestion built on GenStage


## Deep Dive

Streams are lazy, composable data pipelines that process one element at a time without materializing intermediate collections. This is fundamentally different from Enum, which materializes the entire dataset before the next operation.

**Lazy evaluation semantics:**
Stream operations return a `%Stream{}` struct containing a function. The actual computation is deferred until consumed by a terminal operation (`.run()`, `Enum.to_list()`, etc.). This allows streams to:
- Chain indefinite sequences (e.g., `Stream.iterate(0, &(&1 + 1))`)
- Transform without memory bloat (e.g., processing multi-gigabyte files)
- Compose reusable pipelines as first-class values

**Resource lifecycle in streams:**
Streams wrapping resources (`Stream.resource/3`) must define cleanup functions. A stream created from a file remains "open" (in terms of the lambda) until the consumer finishes or errors. If the consumer crashes or stops early, the cleanup function still runs — critical for proper file/socket/port management.

**Backpressure and demand:**
Unlike streams in other languages, Elixir's synchronous streams don't inherently implement backpressure. Backpressure is demand-based: the consumer pulls data at its own pace. `GenStage` and `Flow` add explicit backpressure — the producer waits for the consumer to request more elements. This is why benchmarking matters: a naive stream consumer can overwhelm memory if the pipeline produces faster than it consumes.
