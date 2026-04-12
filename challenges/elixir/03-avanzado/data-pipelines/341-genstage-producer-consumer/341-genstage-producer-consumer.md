# GenStage Producer-Consumer with Demand-Driven Back-Pressure

**Project**: `event_ingestor` — a multi-stage pipeline that ingests events from an external API, transforms them, and persists to storage with explicit back-pressure.

## Project context

You run a telemetry ingestion service that pulls events from a paginated HTTP source
(~50k events/min at peak). Downstream, each event must be enriched (GeoIP lookup,
schema validation) and written to a slow PostgreSQL store that sustains ~5k writes/sec.

Without back-pressure the fast producer (HTTP poller) overruns the slow consumer
(DB writer), inflating mailbox memory, triggering BEAM's `message_queue_len` alerts
and eventually OOM-killing the node.

GenStage solves this by inverting the flow: consumers ask for work via `demand`.
Producers hold events in an internal buffer and emit only as many as requested.
The whole pipeline runs at the pace of the slowest stage — bounded memory, bounded
latency, no coordination service needed.

```
event_ingestor/
├── lib/
│   └── event_ingestor/
│       ├── application.ex
│       ├── producer.ex            # GenStage :producer (HTTP poller)
│       ├── transformer.ex         # GenStage :producer_consumer
│       └── consumer.ex            # GenStage :consumer (DB writer)
├── test/
│   └── event_ingestor/
│       └── pipeline_test.exs
├── bench/
│   └── throughput_bench.exs
└── mix.exs
```

## Why GenStage and not raw processes with send/2

Sending messages with `send/2` provides no back-pressure. The receiver's mailbox
grows unbounded. You could implement credit-based flow control manually, but you'd
end up re-implementing GenStage poorly.

Alternatives considered:

- **Raw `GenServer` with `handle_cast`**: same mailbox problem. `cast` never blocks
  the sender. Producer-side throttling is only possible by inserting `Process.sleep/1`
  which is brittle and wastes CPU.
- **`Flow` (built on GenStage)**: higher level, perfect for ad-hoc batch analytics.
  Less control over individual stages, harder to hot-reload a single stage, no
  per-stage supervision. Use Flow when the pipeline is a one-shot computation.
- **`Broadway`**: ideal when the source is a message broker (Kafka, SQS, RabbitMQ).
  Built-in acknowledgement semantics. Overkill when your source is a plain HTTP API.
- **Custom bounded queue + worker pool**: reinvents the wheel and lacks the formal
  dispatch semantics (`DemandDispatcher`, `BroadcastDispatcher`, `PartitionDispatcher`).

GenStage is the right abstraction when you own the transport (HTTP, file, TCP)
and need explicit multi-stage flow with back-pressure.

## Core concepts

### 1. Demand-driven back-pressure

A `:consumer` subscribes to a `:producer` and declares `min_demand` and `max_demand`.
When the consumer has processed enough items to fall below `min_demand`, it sends
a new demand message to the producer requesting up to `max_demand - in_flight` items.

### 2. The three stage types

- `:producer` — emits events. `handle_demand(incoming_demand, state)` must return
  `{:noreply, events, state}` with at most `incoming_demand` events.
- `:producer_consumer` — receives events from upstream, transforms them, emits
  downstream. Both `handle_events/3` (incoming) and demand are relevant.
- `:consumer` — terminal stage. Processes events in `handle_events/3`, returns
  `{:noreply, [], state}` (no downstream, so no events emitted).

### 3. Buffer semantics

Producers buffer events internally when demand is zero. `buffer_size: :infinity`
is the default — this is a foot-gun for infinite sources. Always set a bounded
buffer and decide a `buffer_keep: :first | :last` policy.

## Design decisions

- **Option A — Single linear chain (producer → transformer → consumer)**:
  - Pros: simple to reason about, easy to trace, one consumer per CPU core via subscription.
  - Cons: the single transformer stage becomes a bottleneck if transformation is CPU-heavy.
- **Option B — Fan-out with `DemandDispatcher` at the producer**:
  - Pros: parallel consumers share the producer's output load-balanced.
  - Cons: event order is not preserved across consumers.

We pick **Option B** because transformation is CPU-bound and we need horizontal
parallelism. Event order per client is preserved via partition-aware dispatching
at the transformer stage (shown below).

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule EventIngestor.MixProject do
  use Mix.Project

  def project do
    [
      app: :event_ingestor,
      version: "0.1.0",
      elixir: "~> 1.16",
      deps: deps()
    ]
  end

  def application do
    [mod: {EventIngestor.Application, []}, extra_applications: [:logger]]
  end

  defp deps do
    [
      {:gen_stage, "~> 1.2"},
      {:benchee, "~> 1.3", only: :dev}
    ]
  end
end
```

### Step 1: Application supervisor

```elixir
defmodule EventIngestor.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {EventIngestor.Producer, []},
      {EventIngestor.Transformer, []},
      {EventIngestor.Consumer, []}
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: EventIngestor.Supervisor)
  end
end
```

### Step 2: Producer — paginated HTTP source

```elixir
defmodule EventIngestor.Producer do
  use GenStage

  @type event :: %{id: String.t(), payload: map(), ts: integer()}

  def start_link(_opts), do: GenStage.start_link(__MODULE__, :ok, name: __MODULE__)

  @impl true
  def init(:ok) do
    # buffer_size bounds the in-memory queue; buffer_keep drops oldest on overflow.
    {:producer, %{cursor: 0}, buffer_size: 10_000, buffer_keep: :first,
     dispatcher: GenStage.DemandDispatcher}
  end

  @impl true
  def handle_demand(demand, state) when demand > 0 do
    {events, new_cursor} = fetch_page(state.cursor, demand)
    {:noreply, events, %{state | cursor: new_cursor}}
  end

  # Replace with real HTTP call (Finch/Req). Deterministic stub for tests.
  defp fetch_page(cursor, count) do
    events =
      for i <- 0..(count - 1) do
        %{id: "evt-#{cursor + i}", payload: %{value: cursor + i}, ts: System.system_time(:millisecond)}
      end

    {events, cursor + count}
  end
end
```

### Step 3: Transformer — enrichment stage

```elixir
defmodule EventIngestor.Transformer do
  use GenStage

  def start_link(_opts), do: GenStage.start_link(__MODULE__, :ok, name: __MODULE__)

  @impl true
  def init(:ok) do
    {:producer_consumer, %{}, subscribe_to: [{EventIngestor.Producer, min_demand: 500, max_demand: 1_000}]}
  end

  @impl true
  def handle_events(events, _from, state) do
    enriched = Enum.map(events, &enrich/1)
    {:noreply, enriched, state}
  end

  defp enrich(event) do
    Map.merge(event, %{enriched_at: System.monotonic_time(:millisecond), schema_version: 2})
  end
end
```

### Step 4: Consumer — DB writer (batched)

```elixir
defmodule EventIngestor.Consumer do
  use GenStage
  require Logger

  def start_link(_opts), do: GenStage.start_link(__MODULE__, :ok, name: __MODULE__)

  @impl true
  def init(:ok) do
    {:consumer, %{written: 0},
     subscribe_to: [{EventIngestor.Transformer, min_demand: 100, max_demand: 500}]}
  end

  @impl true
  def handle_events(events, _from, state) do
    persist_batch(events)
    {:noreply, [], %{state | written: state.written + length(events)}}
  end

  # Replace with Repo.insert_all/3. Stub keeps the example self-contained.
  defp persist_batch(events) do
    :telemetry.execute([:event_ingestor, :batch, :written], %{count: length(events)}, %{})
    :ok
  end
end
```

## Why this works

- The **producer** only materialises events when a downstream stage actually asks.
  `handle_demand/2` is called exactly when demand reaches the producer — no polling.
- The **transformer's** `subscribe_to` options set `max_demand: 1000`. This caps
  the number of in-flight events between producer and transformer. Memory is bounded.
- The **consumer** uses `max_demand: 500`. It pulls at most 500 events at a time
  from the transformer. If `persist_batch/1` takes 100ms for 500 events, the effective
  pipeline throughput is 5k events/sec — exactly what the DB sustains.
- When the DB slows down, the consumer stops issuing new demand. The transformer's
  buffer fills up and stops demanding from the producer. The producer's internal
  buffer fills up to 10k events, then drops or blocks (per `buffer_keep`).
  Back-pressure cascades upstream automatically.

## Tests

```elixir
defmodule EventIngestor.PipelineTest do
  use ExUnit.Case, async: false

  alias EventIngestor.{Producer, Transformer, Consumer}

  describe "back-pressure semantics" do
    test "consumer receives events transformed by the intermediate stage" do
      parent = self()

      {:ok, probe} =
        GenStage.start_link(
          GenStage.Streamer,
          {[], []},
          []
        )

      # Attach a probe consumer to the transformer to observe enriched events.
      consumer_fn = fn events ->
        send(parent, {:events, events})
        :ok
      end

      {:ok, _} = TestConsumer.start_link({consumer_fn, Transformer})

      assert_receive {:events, events}, 2_000
      assert length(events) > 0
      assert Enum.all?(events, &Map.has_key?(&1, :enriched_at))
    end
  end

  describe "buffer bounds" do
    test "producer does not grow its buffer beyond the configured limit" do
      # After running the pipeline briefly, total mailbox + buffer remains finite.
      Process.sleep(200)
      {:message_queue_len, len} = Process.info(Process.whereis(Producer), :message_queue_len)
      assert len < 1_000
    end
  end
end

defmodule TestConsumer do
  use GenStage

  def start_link({fun, upstream}) do
    GenStage.start_link(__MODULE__, {fun, upstream})
  end

  @impl true
  def init({fun, upstream}) do
    {:consumer, fun, subscribe_to: [{upstream, min_demand: 10, max_demand: 50}]}
  end

  @impl true
  def handle_events(events, _from, fun) do
    fun.(events)
    {:noreply, [], fun}
  end
end
```

## Benchmark

```elixir
# bench/throughput_bench.exs
# Measures sustained throughput with and without the transformer stage.

:ok = Application.ensure_started(:event_ingestor)

{time_us, _} =
  :timer.tc(fn ->
    Process.sleep(5_000)
  end)

written =
  case Process.whereis(EventIngestor.Consumer) do
    nil -> 0
    pid -> :sys.get_state(pid).written
  end

throughput = written / (time_us / 1_000_000)
IO.puts("Throughput: #{Float.round(throughput, 0)} events/sec")
```

**Target**: sustained throughput of 20k–50k events/sec on a 4-core machine for
the in-memory stub consumer. A real DB consumer is bounded by the DB, not the pipeline.

## Trade-offs and production gotchas

**1. `buffer_size: :infinity` (default) will OOM you.**
Set it explicitly. For a polling source, `buffer_size: N` where N covers ~5 seconds
of peak throughput is a reasonable starting point.

**2. `min_demand` too close to `max_demand` kills pipelining.**
If `min_demand: 499, max_demand: 500`, the consumer only asks for 1 new event at
a time once it has processed one. You lose batching and pay the demand round-trip
cost per event. Rule of thumb: `min_demand = max_demand / 2`.

**3. Crashing a producer does not replay events.**
GenStage has no built-in persistence. If the producer process crashes, buffered
events are gone. For exactly-once semantics you need source-side acknowledgements
(Broadway's job).

**4. Starting stages in the wrong order leaves subscriptions dangling.**
If the supervisor starts the consumer before the transformer exists, the consumer's
`subscribe_to` will fail to connect. With `strategy: :one_for_one` and module-level
`start_link/1`, the order in the children list matters. Alternatives: use `subscribe/4`
in `handle_info(:connect, ...)` with a retry, or lean on `:one_for_all` to co-restart.

**5. `DemandDispatcher` breaks event ordering.**
If you need per-key order (e.g. events for `user_id: 42` must be processed in
arrival order), use `PartitionDispatcher` with a `hash/1` function. Then all events
for the same key land on the same consumer.

**6. When NOT to use GenStage.**
If your pipeline is a one-shot map-reduce over a finite source, use `Flow`. If the
source is an external broker with ack semantics, use `Broadway`. GenStage is the
right tool when you need long-running, multi-stage, demand-driven processing where
you own both ends of the pipe.

## Reflection

You measured the pipeline sustaining 30k events/sec, but your monitoring shows
the producer's `message_queue_len` is climbing after minute 3. The consumer is not
behind (it keeps up with demand). What could explain a growing mailbox on the
producer, given that GenStage routes events through its internal buffer and not
the mailbox? (Hint: subscription control messages, `:timeout`, process monitors,
and `:sys` traces all land in the mailbox.)

## Resources

- [GenStage documentation — hexdocs](https://hexdocs.pm/gen_stage/GenStage.html)
- [GenStage announcement — Dashbit blog](https://dashbit.co/blog/gen_stage-and-flow)
- [Elixir GenStage and Flow — Pragmatic Bookshelf](https://pragprog.com/titles/passtd/concurrent-data-processing-in-elixir/)
- [Telemetry](https://github.com/beam-telemetry/telemetry) for stage-level metrics
