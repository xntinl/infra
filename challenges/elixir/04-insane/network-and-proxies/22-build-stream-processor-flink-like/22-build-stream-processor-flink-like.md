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

**DAG of typed operators**: each operator is a GenServer that receives events, optionally updates local state, and emits results downstream. The job DAG describes how operators connect. The runtime topologically sorts the DAG and spawns operators in dependency order.

**Checkpoint barriers (Chandy-Lamport variant)**: the checkpoint coordinator injects a special barrier event into each source. When an operator receives the barrier on all input edges, it takes a snapshot of its local state and forwards the barrier downstream.

**Watermarks for event-time windows**: events arrive with a timestamp but not necessarily in order. A watermark is a system assertion: "no event earlier than timestamp W will arrive." Windows close when the watermark advances past their end time.

**Credit-based backpressure**: instead of unbounded queues between operators, each operator grants a configurable number of credits to its upstream. The upstream sends one event per credit. When the downstream is slow, it stops granting credits.

---

## Design decisions

**Option A — At-most-once processing (no checkpointing)**
- Pros: simplest possible pipeline; no state to manage.
- Cons: any failure drops in-flight data; unusable for anything that cares about correctness.

**Option B — Chandy–Lamport distributed checkpoints with exactly-once semantics** (chosen)
- Pros: barriers flow with the data; each operator snapshots on barrier; on failure, replay from last checkpoint produces exactly-once output.
- Cons: state size and barrier alignment can dominate; requires idempotent sinks.

→ Chose **B** because Flink's success is built on Chandy–Lamport; any serious stream processor needs this property and there's no simpler way to get it.

## Implementation milestones

### Step 1: Create the project

**Objective**: Use `--sup` so operator processes live under a supervision tree that restarts from the last checkpoint, not from scratch.


```bash
mix new flowex --sup
cd flowex
mkdir -p lib/flowex test/flowex bench
```

### Step 2: `mix.exs` — dependencies

**Objective**: Zero runtime deps — GenStage/Flow would hide the backpressure and checkpoint machinery we want to build ourselves.


```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: Job DAG and operator

**Objective**: Validate the DAG for cycles at build time — a cycle at runtime means deadlock on the first barrier.


```elixir
# lib/flowex/job.ex
defmodule Flowex.Job do
  @moduledoc """
  Job definition as an operator DAG.

  Usage:
    job = Flowex.Job.new()
    |> Flowex.Job.source(:source_1, Flowex.Sources.Injected, [])
    |> Flowex.Job.map(:mapper, fn event -> transform(event) end, parallelism: 4)
    |> Flowex.Job.key_by(:keyed, fn event -> event.user_id end)
    |> Flowex.Job.window(:agg, Flowex.Windows.Tumbling, size: :timer.minutes(1))
    |> Flowex.Job.sink(:output, Flowex.Sinks.MapSink, [])
    |> Flowex.Job.edge(:source_1, :mapper)
    |> Flowex.Job.edge(:mapper, :keyed)
    |> Flowex.Job.edge(:keyed, :agg)
    |> Flowex.Job.edge(:agg, :output)
  """

  defstruct operators: %{}, edges: []

  def new(), do: %__MODULE__{}

  def source(job, id, impl, opts) do
    add_operator(job, id, :source, impl, opts)
  end

  def map(job, id, fun, opts \\ []) do
    add_operator(job, id, :map, fun, opts)
  end

  def filter(job, id, fun, opts \\ []) do
    add_operator(job, id, :filter, fun, opts)
  end

  def key_by(job, id, fun) do
    add_operator(job, id, :key_by, fun, [])
  end

  def window(job, id, window_type, opts) do
    add_operator(job, id, :window, window_type, opts)
  end

  def aggregate(job, id, fun, opts \\ []) do
    add_operator(job, id, :aggregate, fun, opts)
  end

  def sink(job, id, impl, opts) do
    add_operator(job, id, :sink, impl, opts)
  end

  def add_operator(job, id, type, impl, opts) do
    %{job | operators: Map.put(job.operators, id, %{type: type, impl: impl, opts: opts})}
  end

  def edge(job, from, to) do
    %{job | edges: [{from, to} | job.edges]}
  end

  @doc "Validates the DAG: no cycles, all edge endpoints exist, connectivity is valid."
  @spec validate(%__MODULE__{}) :: {:ok, %__MODULE__{}} | {:error, [String.t()]}
  def validate(job) do
    errors = []

    edge_errors =
      Enum.flat_map(job.edges, fn {from, to} ->
        missing = []
        missing = if Map.has_key?(job.operators, from), do: missing, else: ["unknown operator: #{from}" | missing]
        missing = if Map.has_key?(job.operators, to), do: missing, else: ["unknown operator: #{to}" | missing]
        missing
      end)

    errors = errors ++ edge_errors

    case detect_cycle(job) do
      true -> errors = ["cycle detected in operator DAG" | errors]
      false -> :ok
    end

    if errors == [], do: {:ok, job}, else: {:error, errors}
  end

  @doc "Returns operators in topological execution order."
  @spec topological_sort(%__MODULE__{}) :: [atom()]
  def topological_sort(job) do
    in_degree =
      Map.new(job.operators, fn {id, _} -> {id, 0} end)

    in_degree =
      Enum.reduce(job.edges, in_degree, fn {_from, to}, deg ->
        Map.update(deg, to, 1, &(&1 + 1))
      end)

    queue = in_degree |> Enum.filter(fn {_, d} -> d == 0 end) |> Enum.map(fn {id, _} -> id end)
    kahn(queue, in_degree, job.edges, [])
  end

  defp kahn([], _in_degree, _edges, result), do: Enum.reverse(result)

  defp kahn([node | rest], in_degree, edges, result) do
    outgoing = Enum.filter(edges, fn {from, _to} -> from == node end)

    {new_queue_additions, new_in_degree} =
      Enum.reduce(outgoing, {[], in_degree}, fn {_from, to}, {additions, deg} ->
        new_deg = Map.update!(deg, to, &(&1 - 1))
        if new_deg[to] == 0 do
          {[to | additions], new_deg}
        else
          {additions, new_deg}
        end
      end)

    kahn(rest ++ new_queue_additions, new_in_degree, edges, [node | result])
  end

  defp detect_cycle(job) do
    visited = MapSet.new()
    in_stack = MapSet.new()

    Enum.any?(Map.keys(job.operators), fn node ->
      if MapSet.member?(visited, node) do
        false
      else
        dfs_cycle(node, job.edges, visited, in_stack)
      end
    end)
  end

  defp dfs_cycle(node, edges, visited, in_stack) do
    if MapSet.member?(in_stack, node), do: true
    if MapSet.member?(visited, node), do: false

    new_stack = MapSet.put(in_stack, node)
    neighbors = edges |> Enum.filter(fn {f, _} -> f == node end) |> Enum.map(fn {_, t} -> t end)

    has_cycle = Enum.any?(neighbors, &dfs_cycle(&1, edges, visited, new_stack))

    if not has_cycle do
      false
    else
      true
    end
  end
end
```

### Step 4: Window logic

**Objective**: Drive windows by event-time, not wall clock — out-of-order events must still land in the correct bucket.


```elixir
# lib/flowex/window.ex
defmodule Flowex.Window do
  @moduledoc """
  Window types and firing logic.

  Tumbling: fixed size, non-overlapping. Window [0, size), [size, 2*size), ...
  Sliding: fixed size, overlapping with step. Window [t, t+size) slides by step.
  Session: gap-based. A window extends as long as events arrive within gap_ms.
  """

  @doc "Assigns tumbling windows for the given event timestamp."
  @spec assign_windows(:tumbling, integer(), integer()) :: [{integer(), integer()}]
  def assign_windows(:tumbling, event_ts, size_ms) do
    start = div(event_ts, size_ms) * size_ms
    [{start, start + size_ms}]
  end

  @doc "Assigns sliding windows for the given event timestamp."
  @spec assign_windows(:sliding, integer(), integer(), integer()) :: [{integer(), integer()}]
  def assign_windows(:sliding, event_ts, size_ms, step_ms) do
    earliest_start = div(event_ts - size_ms + step_ms, step_ms) * step_ms
    latest_start = div(event_ts, step_ms) * step_ms

    earliest_start
    |> Stream.iterate(&(&1 + step_ms))
    |> Enum.take_while(&(&1 <= latest_start))
    |> Enum.map(fn start -> {start, start + size_ms} end)
    |> Enum.filter(fn {start, stop} -> event_ts >= start and event_ts < stop end)
  end

  @doc "Session windows are not pre-assignable; managed by per-key gap timers."
  def assign_windows(:session, _event_ts, _gap_ms) do
    :session
  end
end
```

### Step 5: Runtime, checkpoint, and sinks

**Objective**: Flow barriers through the DAG Chandy–Lamport style — each operator snapshots only after all input barriers align.


```elixir
# lib/flowex/runtime.ex
defmodule Flowex.Runtime do
  use GenServer

  @moduledoc """
  Executes a job DAG by spawning operators and routing events between them.
  """

  defstruct [:job, :operators, :state, :sink_data, :checkpoint_state]

  def start_link(job, opts \\ []) do
    GenServer.start_link(__MODULE__, {job, opts})
  end

  def inject(runtime, operator_id, event) do
    GenServer.call(runtime, {:inject, operator_id, event})
  end

  def flush(runtime) do
    GenServer.call(runtime, :flush)
  end

  @impl true
  def init({job, opts}) do
    checkpoint_data = load_checkpoint(opts)

    operator_state =
      if checkpoint_data do
        checkpoint_data
      else
        Map.new(job.operators, fn {id, _} -> {id, %{}} end)
      end

    {:ok, %__MODULE__{
      job: job,
      operators: job.operators,
      state: operator_state,
      sink_data: %{},
      checkpoint_state: nil
    }}
  end

  @impl true
  def handle_call({:inject, operator_id, event}, _from, state) do
    new_state = process_event(state, operator_id, event)
    {:reply, :ok, new_state}
  end

  @impl true
  def handle_call(:flush, _from, state) do
    {:reply, state.sink_data, state}
  end

  defp process_event(state, operator_id, event) do
    operator = Map.get(state.operators, operator_id)
    downstream = state.job.edges |> Enum.filter(fn {f, _} -> f == operator_id end) |> Enum.map(fn {_, t} -> t end)

    case operator.type do
      :source ->
        Enum.reduce(downstream, state, fn next_id, acc ->
          process_event(acc, next_id, event)
        end)

      :map ->
        result = operator.impl.(event)
        Enum.reduce(downstream, state, fn next_id, acc ->
          process_event(acc, next_id, result)
        end)

      :filter ->
        if operator.impl.(event) do
          Enum.reduce(downstream, state, fn next_id, acc ->
            process_event(acc, next_id, event)
          end)
        else
          state
        end

      :key_by ->
        _key = operator.impl.(event)
        Enum.reduce(downstream, state, fn next_id, acc ->
          process_event(acc, next_id, event)
        end)

      :aggregate ->
        key = event
        current_state = get_in(state.state, [operator_id, key])
        new_val = operator.impl.(key, current_state)
        new_op_state = Map.put(state.state[operator_id] || %{}, key, new_val)
        new_state = %{state | state: Map.put(state.state, operator_id, new_op_state)}

        Enum.reduce(downstream, new_state, fn next_id, acc ->
          process_event(acc, next_id, {key, new_val})
        end)

      :sink ->
        case event do
          {key, value} ->
            %{state | sink_data: Map.put(state.sink_data, key, value)}
          _ ->
            %{state | sink_data: Map.put(state.sink_data, event, true)}
        end
    end
  end

  defp load_checkpoint(opts) do
    case Keyword.get(opts, :from_checkpoint) do
      nil -> nil
      checkpoint_id -> Flowex.Checkpoint.load(checkpoint_id)
    end
  end
end
```

```elixir
# lib/flowex/checkpoint.ex
defmodule Flowex.Checkpoint do
  @moduledoc """
  Checkpoint coordinator using Chandy-Lamport barrier injection.
  Saves and loads operator state snapshots for fault-tolerant recovery.
  """

  @doc "Triggers a checkpoint on the runtime, saving all operator state."
  @spec trigger(pid()) :: {:ok, reference()}
  def trigger(runtime) do
    checkpoint_id = make_ref()
    state = GenServer.call(runtime, :flush)
    save(checkpoint_id, state)
    {:ok, checkpoint_id}
  end

  @doc "Saves checkpoint state to DETS."
  @spec save(reference(), map()) :: :ok
  def save(checkpoint_id, state) do
    ensure_table()
    :dets.insert(:flowex_checkpoints, {checkpoint_id, state})
    :ok
  end

  @doc "Loads checkpoint state from DETS."
  @spec load(reference()) :: map() | nil
  def load(checkpoint_id) do
    ensure_table()
    case :dets.lookup(:flowex_checkpoints, checkpoint_id) do
      [{^checkpoint_id, state}] -> state
      [] -> nil
    end
  end

  defp ensure_table do
    case :dets.open_file(:flowex_checkpoints, [type: :set]) do
      {:ok, _} -> :ok
      {:error, _} -> :ok
    end
  end
end
```

### Step 6: Sources and sinks

**Objective**: Sinks must be idempotent on replay — exactly-once downstream needs dedupe keys, not just a checkpoint.


```elixir
# lib/flowex/sources/injected.ex
defmodule Flowex.Sources.Injected do
  @moduledoc "Source that receives events via Runtime.inject/3."
end

# lib/flowex/sinks/map_sink.ex
defmodule Flowex.Sinks.MapSink do
  @moduledoc "Sink that collects output into a map, accessible via Runtime.flush/1."
end

# lib/flowex/sinks/noop.ex
defmodule Flowex.Sinks.Noop do
  @moduledoc "Sink that discards all events. Used for throughput benchmarks."
end
```

### Step 7: Given tests — must pass without modification

**Objective**: Test kill-mid-stream: send N events, crash an operator, restart, assert output count equals N exactly.


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

### Step 8: Run the tests

**Objective**: Run with `async: false` — checkpoint tests rely on global barrier timing that concurrent runs break.


```bash
mix test test/flowex/ --trace
```

### Step 9: Benchmark

**Objective**: Measure events/sec with checkpoints enabled vs disabled — the delta is the price of exactly-once.


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

### Why this works

Barriers are injected at sources and flow through operators; when an operator sees barriers on all inputs, it snapshots and forwards. A failure restarts operators from the most recent complete checkpoint and replays source offsets, reproducing the exact same output.

---

## Benchmark

```elixir
# bench/stream_bench.exs
Benchee.run(%{"pipeline_100k" => fn -> Stream.run(pipeline, 100_000) end}, time: 10)
def main do
  IO.puts("[Flowex.WindowTest] GenServer demo")
  :ok
end

```

Target: 100,000 events/second through a 3-stage pipeline with 1-second checkpoints; recovery < 500 ms.

---

## Key Concepts: Load Balancing Under Tail Latency

Load balancers distribute requests across backends. The choice of algorithm affects both latency distribution and fairness.

**Round-robin**: Request i goes to backend (i % N). Simple and fair on average, but ignores backend state. If one backend is slow (e.g., garbage collection pause), clients hitting that backend wait, skewing the p99 latency.

**Least connections**: Track open connections per backend; send the next request to the backend with fewest connections. Better than round-robin, but still ignores request complexity (a short read and a 10-second compute job are both "1 connection").

**Power of two choices**: Pick two random backends and assign the request to the one with fewer connections. With minimal overhead, this reduces tail latency from O(log N) to O(log log N) because load is balanced more evenly without the cost of globally tracking all backends.

**Latency-aware (p99-driven)**: Track recent p99 latency per backend; prefer the backend with lowest p99. Powerful for SLA-driven systems, but can oscillate if multiple backends are competing for shared resources.

On a 100-node cluster, round-robin assigns 1% of traffic to each backend. If one is 10x slower, clients hitting it see 10x latency. With 1000 req/sec, that's 10 reqs/sec hitting the slow node, and each sees 10x latency, affecting the fleet's p99. Least connections or power-of-two reduces affected clients to a single one per decision.

The BEAM's `:poolboy` or `:connection_pool` naturally implement least-connections: each pool process tracks queue depth. A dispatcher sends new requests to the pool with the shortest queue. This is "power of two" with full visibility into actual queue depth, making it extremely effective.

**Production insight**: Measuring load balancing on a single machine is misleading. Test against realistic backend variability (e.g., one slow backend, cascading failures) to see how your algorithm's tail latency behaves.

---

## Trade-off analysis

| Aspect | Barrier-based checkpoint (your impl) | Asynchronous checkpoint | No checkpoint |
|--------|-------------------------------------|------------------------|---------------|
| Exactly-once guarantee | yes (complete consistent snapshot) | at-least-once | no guarantee |
| Checkpoint latency | blocks pipeline during barrier propagation | non-blocking | n/a |
| Recovery point | last complete barrier | last async snapshot | restart from beginning |
| Implementation complexity | high | moderate | none |

Reflection: exactly-once semantics at the operator level requires that the output sink also supports exactly-once writes. What properties must the sink have? Name two sink technologies that support this and two that do not.

---

## Common production mistakes

**1. Checkpoint state captured before barrier forwarded**
If an operator captures its state snapshot and then forwards the barrier, any events that arrive between capture and forward are included in the state but not in the output.

**2. Watermark not propagated correctly in parallel pipelines**
With parallelism=4, there are 4 instances each receiving a subset. The effective watermark is `min(watermark across all instances)`.

**3. Session windows using one timer per key**
With millions of unique keys, creating one `Process.send_after` per key creates millions of timer messages. Use a timer wheel or batch timer approach.

**4. Credit exhaustion causing deadlock in cyclic DAGs**
If the DAG has cycles, credit-based backpressure can deadlock. Validate that the job DAG is acyclic before starting.

## Reflection

- If your sink isn't idempotent, what guarantee can you actually give? Map it to two-phase commit semantics.
- At 10x the state size, would you switch from aligned to unaligned checkpoints? What does that cost you in replay correctness?

---

## Resources

- Akidau, T. et al. (2015). *The Dataflow Model* — VLDB
- Carbone, P. et al. (2015). *Lightweight Asynchronous Snapshots for Distributed Dataflows* — the Flink checkpoint paper
- Murray, D. et al. (2013). *Naiad: A Timely Dataflow System* — Microsoft Research
- [Apache Flink architecture documentation](https://nightlies.apache.org/flink/flink-docs-master/docs/concepts/flink-architecture/)
