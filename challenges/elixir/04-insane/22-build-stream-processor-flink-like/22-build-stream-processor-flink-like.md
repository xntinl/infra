# Stream Processor

**Project**: `flowex` — an exactly-once stream processing engine with windowed aggregations and distributed snapshots

---

## Project context

You are building `flowex`, a stream processing engine that provides exactly-once semantics, stateful computation, and windowed aggregations. Jobs are defined as DAGs of typed operators; the runtime executes them with configurable parallelism and checkpoints state for fault-tolerant recovery.

Project structure:

```
flowex/
├── lib/
│   └── flowex/
│       ├── application.ex           # framework supervisor
│       ├── job.ex                   # DSL: define operator DAG, validate connectivity
│       ├── runtime.ex               # job executor: topological sort, spawn workers
│       ├── operator.ex              # worker GenServer: stateful, receives events, emits downstream
│       ├── window.ex                # tumbling, sliding, and session window logic
│       ├── watermark.ex             # watermark propagation, late event handling
│       ├── checkpoint.ex            # coordinator: Chandy-Lamport barrier injection, state snapshot
│       ├── state_backend.ex         # operator state storage: ETS + DETS for durability
│       ├── backpressure.ex          # credit-based flow control between operators
│       └── sink.ex                  # output sink: idempotent writes, transactional commits
├── test/
│   └── flowex/
│       ├── window_test.exs          # tumbling, sliding, session semantics
│       ├── watermark_test.exs       # late events, grace period, window firing
│       ├── checkpoint_test.exs      # state snapshot, recovery correctness
│       ├── exactly_once_test.exs    # replay without duplicate output
│       ├── backpressure_test.exs    # no unbounded buffer growth under overload
│       └── throughput_test.exs      # 1M events/second pipeline benchmark
├── bench/
│   └── flowex_bench.exs
└── mix.exs
```

---

## The problem

You need to process a stream of events — clicks, transactions, sensor readings — with operations like filtering, aggregation, and joining, and emit results continuously. The output must be correct even if a worker crashes and restarts. "Correct" here means exactly-once: each input event contributes to exactly one output, never zero (at-least-once) or more than one (duplicate).

---

## Why this design

**DAG of typed operators**: each operator is a GenServer that receives events, optionally updates local state, and emits results downstream. The job DAG describes how operators connect. The runtime topologically sorts the DAG and spawns operators in dependency order. Type information on edges validates connectivity before execution starts.

**Checkpoint barriers (Chandy-Lamport variant)**: the checkpoint coordinator injects a special barrier event into each source. When an operator receives the barrier on all input edges, it takes a snapshot of its local state and forwards the barrier downstream. This ensures all state is captured consistently — no operator includes state that reflects events after the barrier. Recovery replays from the last complete barrier.

**Watermarks for event-time windows**: events arrive with a timestamp but not necessarily in order. A watermark is a system assertion: "no event earlier than timestamp W will arrive." Windows close when the watermark advances past their end time. The grace period allows late events up to `window_end + grace_period` before the window is finalized.

**Credit-based backpressure**: instead of unbounded queues between operators, each operator grants a configurable number of credits to its upstream. The upstream sends one event per credit. When the downstream is slow, it stops granting credits; the upstream blocks. This prevents unbounded memory growth under sustained overload.

---

## Implementation milestones

### Step 1: Create the project

```bash
mix new flowex --sup
cd flowex
mkdir -p lib/flowex test/flowex bench
```

### Step 2: `mix.exs` — dependencies

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: Job DAG and operator

```elixir
# lib/flowex/job.ex
defmodule Flowex.Job do
  @moduledoc """
  Job definition as an operator DAG.

  Usage:
    job = Flowex.Job.new()
    |> Flowex.Job.source(:source_1, Flowex.Sources.KlogSource, [topic: "events"])
    |> Flowex.Job.map(:mapper, fn event -> transform(event) end, parallelism: 4)
    |> Flowex.Job.key_by(:keyed, fn event -> event.user_id end)
    |> Flowex.Job.window(:agg, Flowex.Windows.Tumbling, size: :timer.minutes(1))
    |> Flowex.Job.sink(:output, Flowex.Sinks.EtsSink, [table: :results])
    |> Flowex.Job.edge(:source_1, :mapper)
    |> Flowex.Job.edge(:mapper, :keyed)
    |> Flowex.Job.edge(:keyed, :agg)
    |> Flowex.Job.edge(:agg, :output)
  """

  defstruct operators: %{}, edges: []

  def new(), do: %__MODULE__{}

  def add_operator(job, id, type, impl, opts) do
    %{job | operators: Map.put(job.operators, id, %{type: type, impl: impl, opts: opts})}
  end

  def edge(job, from, to) do
    %{job | edges: [{from, to} | job.edges]}
  end

  @doc "Validates the DAG: no cycles, all edge endpoints exist, connectivity is valid."
  def validate(job) do
    # TODO: check for cycles (DFS cycle detection)
    # TODO: check all edge endpoints refer to declared operators
    # TODO: return {:ok, job} or {:error, reasons}
  end

  @doc "Returns operators in topological execution order."
  def topological_sort(job) do
    # TODO: Kahn's algorithm
  end
end
```

### Step 4: Window logic

```elixir
# lib/flowex/window.ex
defmodule Flowex.Window do
  @moduledoc """
  Window types and firing logic.

  Tumbling: fixed size, non-overlapping. Window [0, size), [size, 2*size), ...
  Sliding: fixed size, overlapping with step. Window [t, t+size) slides by step.
  Session: gap-based. A window extends as long as events arrive within gap_ms.
           Fires when no event arrives within gap_ms of the last event.

  All windows use event time (event's embedded timestamp), not processing time.
  Windows fire when the watermark advances past window_end.
  """

  def assign_windows(:tumbling, event_ts, size_ms) do
    start = div(event_ts, size_ms) * size_ms
    [{start, start + size_ms}]
  end

  def assign_windows(:sliding, event_ts, size_ms, step_ms) do
    # TODO: list of overlapping windows this event belongs to
    # HINT: first window that contains event_ts is
    #        {floor((event_ts - size_ms) / step_ms + 1) * step_ms, ...}
  end

  def assign_windows(:session, _event_ts, _gap_ms) do
    # Session windows are not pre-assignable; managed by per-key gap timers
    :session
  end
end
```

### Step 5: Given tests — must pass without modification

```elixir
# test/flowex/window_test.exs
defmodule Flowex.WindowTest do
  use ExUnit.Case, async: true

  alias Flowex.Window

  test "tumbling windows: event assigned to exactly one window" do
    size = 60_000  # 1 minute
    event_ts = 90_000  # 1.5 minutes

    windows = Window.assign_windows(:tumbling, event_ts, size)
    assert length(windows) == 1
    [{start, stop}] = windows
    assert start == 60_000
    assert stop == 120_000
    assert start <= event_ts and event_ts < stop
  end

  test "sliding windows: event may belong to multiple windows" do
    size = 60_000
    step = 30_000
    event_ts = 90_000

    windows = Window.assign_windows(:sliding, event_ts, size, step)
    assert length(windows) >= 2

    for {start, stop} <- windows do
      assert start <= event_ts and event_ts < stop
    end
  end
end
```

```elixir
# test/flowex/checkpoint_test.exs
defmodule Flowex.CheckpointTest do
  use ExUnit.Case, async: false

  test "checkpoint + recovery produces correct output without duplicates" do
    job = build_word_count_job()
    {:ok, runtime} = Flowex.Runtime.start_link(job)

    # Process 1000 events
    for i <- 1..1_000 do
      Flowex.Runtime.inject(runtime, :source, %{word: "word_#{rem(i, 10)}", ts: i * 1_000})
    end

    # Take checkpoint
    {:ok, checkpoint_id} = Flowex.Checkpoint.trigger(runtime)

    # Process 200 more events (these will be replayed)
    for i <- 1_001..1_200 do
      Flowex.Runtime.inject(runtime, :source, %{word: "word_#{rem(i, 10)}", ts: i * 1_000})
    end

    # Kill and restart from checkpoint
    Process.exit(runtime, :kill)
    {:ok, runtime2} = Flowex.Runtime.start_link(job, from_checkpoint: checkpoint_id)

    # Replay the 200 events
    for i <- 1_001..1_200 do
      Flowex.Runtime.inject(runtime2, :source, %{word: "word_#{rem(i, 10)}", ts: i * 1_000})
    end

    # Flush and read results
    results = Flowex.Runtime.flush(runtime2)

    # Total counts must match 1200 events, no duplicates from replay
    total = results |> Map.values() |> Enum.sum()
    assert total == 1_200, "expected 1200 total word counts, got #{total}"
  end

  defp build_word_count_job do
    Flowex.Job.new()
    |> Flowex.Job.source(:source, Flowex.Sources.Injected, [])
    |> Flowex.Job.map(:extract, fn e -> e.word end, parallelism: 1)
    |> Flowex.Job.key_by(:keyed, fn word -> word end)
    |> Flowex.Job.aggregate(:count, fn _word, acc -> (acc || 0) + 1 end, parallelism: 1)
    |> Flowex.Job.sink(:sink, Flowex.Sinks.MapSink, [])
    |> Flowex.Job.edge(:source, :extract)
    |> Flowex.Job.edge(:extract, :keyed)
    |> Flowex.Job.edge(:keyed, :count)
    |> Flowex.Job.edge(:count, :sink)
  end
end
```

### Step 6: Run the tests

```bash
mix test test/flowex/ --trace
```

### Step 7: Benchmark

```elixir
# bench/flowex_bench.exs
job = Flowex.Job.new()
|> Flowex.Job.source(:source, Flowex.Sources.Injected, [])
|> Flowex.Job.filter(:filter, fn e -> e.value > 0 end, parallelism: 2)
|> Flowex.Job.map(:transform, fn e -> %{e | value: e.value * 2} end, parallelism: 2)
|> Flowex.Job.key_by(:keyed, fn e -> rem(e.id, 100) end)
|> Flowex.Job.window(:agg, Flowex.Windows.Tumbling, size: 1_000, parallelism: 4)
|> Flowex.Job.sink(:sink, Flowex.Sinks.Noop, [])
|> Flowex.Job.edge(:source, :filter)
|> Flowex.Job.edge(:filter, :transform)
|> Flowex.Job.edge(:transform, :keyed)
|> Flowex.Job.edge(:keyed, :agg)
|> Flowex.Job.edge(:agg, :sink)

{:ok, runtime} = Flowex.Runtime.start_link(job)

events = for i <- 1..1_000_000, do: %{id: i, value: :rand.uniform(100), ts: i}

{elapsed_us, _} = :timer.tc(fn ->
  Enum.each(events, fn e -> Flowex.Runtime.inject(runtime, :source, e) end)
  Flowex.Runtime.flush(runtime)
end)

throughput = 1_000_000 / (elapsed_us / 1_000_000)
IO.puts("Throughput: #{Float.round(throughput)} events/second")
```

Target: 1M events/second through a 5-operator pipeline. P99 end-to-end latency under 200ms.

---

## Trade-off analysis

| Aspect | Barrier-based checkpoint (your impl) | Asynchronous checkpoint | No checkpoint |
|--------|-------------------------------------|------------------------|---------------|
| Exactly-once guarantee | yes (complete consistent snapshot) | at-least-once (state may be ahead of acked input) | no guarantee |
| Checkpoint latency | blocks pipeline during barrier propagation | non-blocking | n/a |
| Recovery point | last complete barrier | last async snapshot | restart from beginning |
| Implementation complexity | high | moderate | none |
| Suitable for | financial aggregation, billing | analytics, approximate counts | fire-and-forget metrics |

Reflection: exactly-once semantics at the operator level requires that the output sink also supports exactly-once writes. What properties must the sink have? Name two sink technologies that support this and two that do not.

---

## Common production mistakes

**1. Checkpoint state captured before barrier forwarded**
If an operator captures its state snapshot and then forwards the barrier, any events that arrive between capture and forward are included in the state but not in the output. The barrier must be forwarded after state is captured AND after the snapshot is persisted.

**2. Watermark not propagated correctly in parallel pipelines**
With parallelism=4 for an operator, there are 4 instances, each receiving a subset of events. The effective watermark for the operator is `min(watermark across all 4 instances)` — a single slow instance holds back all window firings. Implement a watermark aggregator per operator.

**3. Session windows using one timer per key**
With millions of unique keys, creating one `Process.send_after` per key per event creates millions of timer messages. Use a timer wheel or batch timer approach: a single GenServer wakes up every `gap_ms / 10` and checks which sessions have expired.

**4. Credit exhaustion causing deadlock in cyclic DAGs**
If operator A feeds operator B which feeds back to A (cyclic DAG), credit-based backpressure can deadlock: A waits for B's credits, B waits for A's credits. Validate that the job DAG is acyclic before starting execution.

---

## Resources

- Akidau, T. et al. (2015). *The Dataflow Model* — VLDB — the conceptual foundation for windowing and watermarks
- Carbone, P. et al. (2015). *Lightweight Asynchronous Snapshots for Distributed Dataflows* — the Flink checkpoint paper
- Murray, D. et al. (2013). *Naiad: A Timely Dataflow System* — Microsoft Research
- [Apache Flink architecture documentation](https://nightlies.apache.org/flink/flink-docs-master/docs/concepts/flink-architecture/) — study the checkpoint protocol in depth
