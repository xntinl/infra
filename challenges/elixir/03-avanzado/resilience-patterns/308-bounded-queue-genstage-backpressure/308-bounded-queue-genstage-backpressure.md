# Bounded Queue with GenStage Back-Pressure

**Project**: `ingest_pipeline` — a producer-consumer pipeline where a Kafka-like producer only emits events when consumers demand capacity, guaranteeing bounded memory.

## Project context

You ingest ~5k events/s of clickstream data. Each event is enriched (geo-IP, device fingerprint) and written to a warehouse. The enrichment step occasionally hangs for 10s when the geo-IP service slows. Under the old architecture (unbounded `Task.Supervisor.async_nolink`), those 10s of backlog spawned 50k pending tasks, blew the heap, and crashed the node.

Back-pressure inverts the flow: consumers ask producers for N events. The producer never emits more than the consumer asked for. If enrichment slows, demand drops, the producer pauses its source, and memory stays flat.

GenStage implements this directly. A producer emits events only when it has pending demand. A consumer signals demand via `:manual` or automatic subscription. The runtime tracks the accounting.

```
ingest_pipeline/
├── lib/
│   └── ingest_pipeline/
│       ├── application.ex
│       ├── producer.ex             # GenStage :producer
│       ├── enricher.ex             # GenStage :producer_consumer
│       └── writer.ex               # GenStage :consumer
├── test/
│   └── ingest_pipeline/
│       └── pipeline_test.exs
└── mix.exs
```

## Why GenStage and not Flow / Broadway

- **Flow** is parallel collections; great for batch, not for long-running pipelines where stages have process identity.
- **Broadway** is GenStage + batching + acknowledgements. Overkill for this exercise and hides the demand mechanics.
- **GenStage** is the primitive. Understanding it lets you debug Broadway issues later.

## Why not raw GenServer with a manual queue

You can build bounded queues with `GenServer` and `:queue.in/2` — but the caller must implement producer/consumer coupling, overflow policy, and demand forwarding by hand. GenStage gives you these for free and works correctly under partitioned/dispatcher modes.

## Core concepts

### 1. Demand-driven flow
```
Writer  ── ask(50) ──▶ Enricher ── ask(50) ──▶ Producer
                                                   │
Writer  ◀── 50 events ── Enricher ◀── 50 events ───┘
```
Producer emits up to 50 because that is all demand it has received. Never more.

### 2. `max_demand` and `min_demand`
Consumer's `max_demand: 100` means "never have more than 100 events in-flight to this consumer". `min_demand: 50` means "when my buffer drops below 50, ask for more up to max". This gives smooth overlap: work continues while the next batch is in flight.

### 3. Buffered producer
If the data source produces faster than consumer demand, the producer must either:
- Buffer (`:buffer_size`) — bounded memory inside the producer.
- Drop (`:buffer_keep: :first` / `:last`) — shed load when buffer full.
- Block (custom) — slow the source itself.

## Design decisions

- **Option A — Unbounded buffer**: source never blocks, memory grows. Not acceptable for 24/7 services.
- **Option B — Bounded buffer with drop-on-overflow**: never crashes on memory. Loses events.
- **Option C — Bounded buffer with block-on-overflow**: slows the source via back-pressure. Loses no events.
→ Chose **C** when the source is under our control, **B** with metrics when the source is external and cannot be slowed. Exercise demonstrates both via `buffer_keep`.

- **Option A — `:sync_notify` producer**: GenStage.sync_notify blocks the caller until demand is available.
- **Option B — Own source process**: the producer pulls from its own source (here, simulated via a queue).
→ Chose **B**. More realistic: production producers poll Kafka/Kinesis, not accept push calls.

## Implementation

### Dependencies (`mix.exs`)

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
defmodule IngestPipeline.MixProject do
  use Mix.Project

  def project do
    [app: :ingest_pipeline, version: "0.1.0", elixir: "~> 1.17", deps: deps()]
  end

  def application do
    [mod: {IngestPipeline.Application, []}, extra_applications: [:logger]]
  end

  defp deps, do: [{:gen_stage, "~> 1.2"}]
end
```

### Step 1: Application

**Objective**: Wire stages under :rest_for_one so upstream crashes reset the demand contract, preventing half-connected pipelines that leak producer-side memory.

```elixir
defmodule IngestPipeline.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      IngestPipeline.Producer,
      IngestPipeline.Enricher,
      IngestPipeline.Writer
    ]

    Supervisor.start_link(children, strategy: :rest_for_one, name: IngestPipeline.Sup)
  end
end
```

### Step 2: Producer (`lib/ingest_pipeline/producer.ex`)

**Objective**: Emit only when pending_demand > 0 so the producer respects downstream capacity and never buffering more than :buffer_size events on heap.

```elixir
defmodule IngestPipeline.Producer do
  use GenStage

  def start_link(opts \\ []) do
    GenStage.start_link(__MODULE__, opts, name: __MODULE__)
  end

  def push(event), do: GenStage.cast(__MODULE__, {:push, event})

  @impl true
  def init(_opts) do
    {:producer, %{queue: :queue.new(), pending_demand: 0}, buffer_size: 10_000}
  end

  @impl true
  def handle_cast({:push, event}, state) do
    state = %{state | queue: :queue.in(event, state.queue)}
    dispatch(state, [])
  end

  @impl true
  def handle_demand(incoming, state) do
    dispatch(%{state | pending_demand: state.pending_demand + incoming}, [])
  end

  defp dispatch(%{pending_demand: 0} = state, acc), do: {:noreply, Enum.reverse(acc), state}

  defp dispatch(%{queue: q, pending_demand: d} = state, acc) do
    case :queue.out(q) do
      {{:value, ev}, q2} ->
        dispatch(%{state | queue: q2, pending_demand: d - 1}, [ev | acc])

      {:empty, _} ->
        {:noreply, Enum.reverse(acc), state}
    end
  end
end
```

### Step 3: Enricher (producer-consumer)

**Objective**: Chain transformation with min_demand/max_demand to propagate back-pressure upstream so producer slows when enrichment lags downstream.

```elixir
defmodule IngestPipeline.Enricher do
  use GenStage

  def start_link(_opts \\ []), do: GenStage.start_link(__MODULE__, :ok, name: __MODULE__)

  @impl true
  def init(:ok) do
    {:producer_consumer, %{},
     subscribe_to: [{IngestPipeline.Producer, min_demand: 50, max_demand: 100}]}
  end

  @impl true
  def handle_events(events, _from, state) do
    enriched = Enum.map(events, &enrich/1)
    {:noreply, enriched, state}
  end

  defp enrich(%{id: id} = event), do: Map.put(event, :enriched_at, System.system_time(:millisecond))
end
```

### Step 4: Writer (consumer)

**Objective**: Configure min_demand/max_demand on terminal sink so writer's throughput ceiling ripples upstream, throttling producer without explicit coupling.

```elixir
defmodule IngestPipeline.Writer do
  use GenStage

  def start_link(_opts \\ []), do: GenStage.start_link(__MODULE__, :ok, name: __MODULE__)

  def test_pid, do: GenStage.call(__MODULE__, :test_pid)
  def set_test_pid(pid), do: GenStage.call(__MODULE__, {:set_test_pid, pid})

  @impl true
  def init(:ok) do
    {:consumer, %{test_pid: nil},
     subscribe_to: [{IngestPipeline.Enricher, min_demand: 10, max_demand: 50}]}
  end

  @impl true
  def handle_events(events, _from, state) do
    if state.test_pid, do: send(state.test_pid, {:written, events})
    {:noreply, [], state}
  end

  @impl true
  def handle_call(:test_pid, _from, state), do: {:reply, state.test_pid, [], state}

  def handle_call({:set_test_pid, pid}, _from, state),
    do: {:reply, :ok, [], %{state | test_pid: pid}}
end
```

## Why this works

- **Strict demand accounting** — the producer's `pending_demand` only grows on `handle_demand/2`; dispatch decrements it. The producer never emits more than it owes downstream.
- **Bounded buffer via `buffer_size: 10_000`** — if the source keeps pushing while consumers are slow, GenStage drops the oldest events (with `:buffer_keep: :first`, the default). Memory is bounded by 10k events regardless of source rate.
- **`:rest_for_one` supervisor** — if the producer crashes, both downstream stages restart (their subscriptions are invalid). Preserves pipeline integrity.
- **Natural flow control** — a slow writer stops asking, which stops the enricher asking, which stops the producer emitting. The source accumulates in its own buffer or drops. At no point does the pipeline grow unbounded.

## Tests

```elixir
defmodule IngestPipeline.PipelineTest do
  use ExUnit.Case, async: false

  setup do
    {:ok, _} = Application.ensure_all_started(:ingest_pipeline)
    :ok = IngestPipeline.Writer.set_test_pid(self())
    :ok
  end

  describe "end-to-end flow" do
    test "events flow from producer to writer enriched" do
      for i <- 1..10, do: IngestPipeline.Producer.push(%{id: i, data: "x"})

      received = collect_until_count(10, 1_000)
      assert length(received) == 10
      assert Enum.all?(received, &Map.has_key?(&1, :enriched_at))
    end
  end

  describe "back-pressure under burst" do
    test "pipeline handles burst without memory explosion" do
      for i <- 1..1_000, do: IngestPipeline.Producer.push(%{id: i})

      {:message_queue_len, mq} =
        Process.info(Process.whereis(IngestPipeline.Producer), :message_queue_len)

      assert mq < 1_000
      received = collect_until_count(1_000, 3_000)
      assert length(received) == 1_000
    end
  end

  defp collect_until_count(target, timeout) do
    deadline = System.monotonic_time(:millisecond) + timeout
    collect_until(target, deadline, [])
  end

  defp collect_until(target, _deadline, acc) when length(acc) >= target, do: acc

  defp collect_until(target, deadline, acc) do
    remaining = max(0, deadline - System.monotonic_time(:millisecond))

    receive do
      {:written, events} -> collect_until(target, deadline, acc ++ events)
    after
      remaining -> acc
    end
  end
end
```

## Benchmark

```elixir
# Measure sustained throughput. Target: >50k events/s on a laptop.
{:ok, _} = Application.ensure_all_started(:ingest_pipeline)
:ok = IngestPipeline.Writer.set_test_pid(self())

n = 50_000
{t, _} = :timer.tc(fn ->
  for i <- 1..n, do: IngestPipeline.Producer.push(%{id: i})

  receive_loop = fn loop, count ->
    if count >= n do
      :done
    else
      receive do
        {:written, events} -> loop.(loop, count + length(events))
      end
    end
  end

  receive_loop.(receive_loop, 0)
end)

IO.puts("#{n / (t / 1_000_000)} events/s")
```

Expected: 50k–200k ev/s depending on CPU.

## Advanced Considerations: Circuit Breakers and Bulkheads in Production

A circuit breaker monitors downstream service health and rejects new requests when failures exceed a threshold, failing fast instead of queuing indefinitely. States: `:closed` (normal), `:open` (fast-fail), `:half_open` (testing recovery). A timeout-based pattern monitors; once requests succeed again, the circuit closes. Half-open tests with a single request; if it succeeds, all requests resume.

Bulkheads isolate resource pools so one slow endpoint doesn't starve others. A GenServer pool with a bounded queue (e.g., `:queue.len(state) >= 100`) can return `{:error, :overloaded}` immediately, preventing queue buildup. Combined with exponential backoff on the client (caller retries with increasing delays), this creates a natural circuit breaker behavior without explicit state.

Graceful degradation means serving stale data or reduced functionality when a service is slow. A cached value with a 5-minute TTL is acceptable for many reads; serve it if the live source is timing out. Feature flags allow disabling expensive operations at runtime. Cascading timeout windows (outer service times out after 5s, inner calls must complete in 3s) prevent unbounded waiting. The cost is complexity: tracking degradation modes, testing failure scenarios, and ensuring data consistency under partial failures.

---


## Deep Dive: Resilience Patterns and Production Implications

Resilience patterns (circuit breakers, timeouts, retries) are easy to implement but hard to test. The insight is that resilience patterns must be tested under failure: timeouts matter only when calls actually take time, retries matter only when transient failures occur. Production systems with untested resilience patterns often fail gracefully in test and catastrophically in production.

---

## Trade-offs and production gotchas

**1. Producer `cast` is itself unbounded** — `handle_cast` mailbox is not GenStage's bounded buffer. If you push at 100k/s and `handle_cast` only drains at 50k/s, the mailbox grows. Use `GenStage.call` for true back-pressure at the push boundary, or rate-limit the source.

**2. `max_demand` tuning** — too low (5) underuses CPU, waiting for demand round-trips. Too high (10k) defeats the purpose, letting tons of work in flight. Start at 100 and tune from benchmarks.

**3. `buffer_keep: :first` drops new events** — the default drops the *oldest* in buffer. `:last` drops newest. Pick based on your domain: for clickstream, drop-oldest is usually acceptable; for financial ticks, you may prefer drop-newest to preserve recent state.

**4. Consumer crashes = demand loss** — when a consumer dies, its unfulfilled demand vanishes. The producer won't know to re-emit. Idempotent consumers + retry-on-restart are mandatory.

**5. `:rest_for_one` vs `:one_for_one`** — rest_for_one restarts everything downstream of a failure, restoring subscriptions. `:one_for_one` leaves downstream orphaned after a producer crash.

**6. When NOT to use this** — for fire-and-forget notifications where losing 1% of events is OK and latency matters, skip GenStage and use `Task.start/1`. Back-pressure adds latency (demand round-trips).

## Reflection

You set `max_demand: 100` on the writer but `max_demand: 10` on the enricher-to-writer link. What happens? Which number actually limits the writer's in-flight events?

## Resources

- [GenStage — hex docs](https://hexdocs.pm/gen_stage/GenStage.html)
- [GenStage Tutorial — Elixir Blog](https://elixir-lang.org/blog/2016/07/14/announcing-genstage/)
- [Broadway — source](https://github.com/dashbitco/broadway) — production-grade GenStage
- [Flow — docs](https://hexdocs.pm/flow/Flow.html)
