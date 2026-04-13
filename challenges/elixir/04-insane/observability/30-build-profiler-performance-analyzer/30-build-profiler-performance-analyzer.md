# Runtime Profiler and Performance Analyzer

**Project**: `beam_profiler` — a production-safe profiler that attaches to live nodes

---

## Project context

You are building `beam_profiler`, a tool the SRE team will use to diagnose latency spikes in production without restarting services. It attaches to a running node over the Erlang distribution protocol, collects stack samples or function traces, generates flame graphs, and correlates GC pressure with the code paths that triggered it.

Project structure:

```
beam_profiler/
├── lib/
│   └── beam_profiler/
│       ├── application.ex
│       ├── sampler.ex           # ← sampling profiler via :erlang.trace/3
│       ├── instrumenter.ex      # ← function-level tracing via :dbg
│       ├── call_graph.ex        # ← tree aggregation from collected traces
│       ├── flamegraph.ex        # ← Brendan Gregg collapsed-stack format
│       ├── speedscope.ex        # ← Speedscope JSON format export
│       ├── memory_profiler.ex   # ← allocation attribution per call stack
│       ├── gc_tracker.ex        # ← GC events correlated with call stacks
│       └── remote.ex            # ← live attach over distribution protocol
├── test/
│   └── beam_profiler/
│       ├── sampler_test.exs
│       ├── call_graph_test.exs
│       ├── flamegraph_test.exs
│       └── gc_tracker_test.exs
├── bench/
│   └── overhead_bench.exs
└── mix.exs
```

---

## Why stack sampling for most diagnoses and targeted instrumentation for specific functions and not pure instrumentation for everything

sampling answers "where is CPU spent?" cheaply and is representative at scale; instrumentation answers "how often and how long does this specific call take?" with zero sampling error. You pick the tool per question, not one for all.

## Design decisions

**Option A — always-on instrumentation via :erlang.trace/3 on every call**
- Pros: perfect fidelity — no sample misses
- Cons: 30-80% throughput overhead, unsuitable for production

**Option B — statistical stack sampling at 99 Hz** (chosen)
- Pros: <2% overhead in production, flame graphs converge with minutes of data
- Cons: short-lived hot paths may be undersampled

→ Chose **B** because the tool must attach to a live production node; anything above 5% throughput tax is a non-starter.

## The business problem

A production service handling 10k req/s started showing P99 latency spikes at 15:00 every day. Adding `IO.inspect` is not an option on a live system. The profiler must attach without restart, collect data for 30 seconds, and detach — leaving the node in exactly the state it was in before.

Two constraints drive every design decision:

1. **Overhead under 5% CPU** — production traffic must not degrade.
2. **Detach cleanly** — `:dbg.stop()` must restore the original module behavior.

---

## Why sampling vs. instrumentation are different tools

**Sampling profiler**: every N milliseconds, freeze the current call stack of every process and record it. Statistical — some short-lived function calls will never be sampled. But very low overhead: the VM does minimal work between samples. Use this for "where is time going globally?"

**Instrumentation profiler**: wrap every exported function of a module with a timer. Exact timings for every call. Higher overhead (a `:dbg` trace message per function entry/exit). Use this for "how slow is this specific function?"

Running both simultaneously doubles overhead. Present a clear API that makes the user choose.

---

## Why flame graphs aggregate the way they do

A flame graph collapses identical stack paths across all samples. Two samples of `A → B → C` appear as a single bar for C with width 2. The visual width represents accumulated time, not call order.

The Brendan Gregg collapsed format is one line per sample:

```
process_name;A;B;C 1
process_name;A;B;C 1
```

Your `Flamegraph.export/2` must merge these into:

```
process_name;A;B;C 2
```

This merging is the algorithmic core of flame graph generation. A correct implementation is a fold over samples into a `%{stack_string => count}` map.

---

## Implementation

### Step 1: Create the project

**Objective**: Lay out sampler, instrumenter, call-graph, and exporters as sibling modules so each swaps without touching others.


```bash
mix new beam_profiler --sup
cd beam_profiler
mkdir -p test/beam_profiler bench
```

### Step 2: `mix.exs`

**Objective**: Keep deps to Jason for Speedscope export — tracing primitives come from OTP, no external profiler libs.


```elixir
defp deps do
  [
    {:jason, "~> 1.4"},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    {:jason, "~> 1.4"},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: `lib/beam_profiler/sampler.ex`

**Objective**: Poll `Process.info(:current_stacktrace)` on a timer so overhead stays O(processes) per tick, not per call.


```elixir
defmodule BeamProfiler.Sampler do
  use GenServer

  @default_hz 100
  @default_duration_ms 30_000

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  @doc """
  Starts sampling all processes at `hz` Hz for `duration_ms` milliseconds.
  Returns {:ok, session_id} immediately. Call collect/1 when done.

  Why sample ALL processes? Because the bottleneck may not be in the processes
  you expect. The overhead of sampling 1000 idle processes is negligible — each
  sample is just :erlang.process_info(pid, :current_stacktrace).
  """
  @spec start_session(keyword()) :: {:ok, reference()}
  def start_session(opts \\ []) do
    hz = Keyword.get(opts, :hz, @default_hz)
    duration_ms = Keyword.get(opts, :duration_ms, @default_duration_ms)
    GenServer.call(__MODULE__, {:start_session, hz, duration_ms})
  end

  @doc """
  Collects all samples gathered in session_id.
  Returns a list of {pid, stacktrace} samples sorted by collection time.
  """
  @spec collect(reference()) :: {:ok, [map()]} | {:error, :session_not_found}
  def collect(session_id) do
    GenServer.call(__MODULE__, {:collect, session_id}, 60_000)
  end

  # ---------------------------------------------------------------------------
  # GenServer
  # ---------------------------------------------------------------------------

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(_opts) do
    :ets.new(:profiler_samples, [:named_table, :public, :bag])
    {:ok, %{sessions: %{}}}
  end

  @impl true
  def handle_call({:start_session, hz, duration_ms}, _from, state) do
    session_id = make_ref()
    interval_ms = div(1_000, hz)

    timer_ref = Process.send_after(self(), {:sample, session_id}, interval_ms)
    deadline = System.monotonic_time(:millisecond) + duration_ms

    session = %{
      interval_ms: interval_ms,
      deadline: deadline,
      timer_ref: timer_ref,
      complete: false,
      sample_count: 0
    }

    new_state = put_in(state, [:sessions, session_id], session)
    {:reply, {:ok, session_id}, new_state}
  end

  @impl true
  def handle_call({:collect, session_id}, _from, state) do
    case Map.get(state.sessions, session_id) do
      nil ->
        {:reply, {:error, :session_not_found}, state}

      session ->
        samples =
          :ets.match_object(:profiler_samples, {session_id, :_, :_, :_})
          |> Enum.map(fn {_sid, pid, stack, ts} -> %{pid: pid, stack: stack, ts: ts} end)
          |> Enum.sort_by(& &1.ts)

        :ets.match_delete(:profiler_samples, {session_id, :_, :_, :_})
        new_state = update_in(state, [:sessions], &Map.delete(&1, session_id))
        {:reply, {:ok, samples}, new_state}
    end
  end

  @impl true
  def handle_info({:sample, session_id}, state) do
    case Map.get(state.sessions, session_id) do
      nil ->
        {:noreply, state}

      session ->
        now = System.monotonic_time(:millisecond)

        if now >= session.deadline do
          new_state = put_in(state, [:sessions, session_id, :complete], true)
          {:noreply, new_state}
        else
          pids = Process.list()
          ts = System.monotonic_time(:nanosecond)

          Enum.each(pids, fn pid ->
            case Process.info(pid, [:current_stacktrace, :registered_name]) do
              nil ->
                :ok

              info ->
                stack = Keyword.get(info, :current_stacktrace, [])
                :ets.insert(:profiler_samples, {session_id, pid, stack, ts})
            end
          end)

          timer_ref = Process.send_after(self(), {:sample, session_id}, session.interval_ms)

          new_state =
            state
            |> put_in([:sessions, session_id, :timer_ref], timer_ref)
            |> update_in([:sessions, session_id, :sample_count], &(&1 + 1))

          {:noreply, new_state}
        end
    end
  end
end
```

### Step 4: `lib/beam_profiler/call_graph.ex`

**Objective**: Fold stacks into `{nodes, edges}` with self/total counts so hotspots and subtrees fall out of one map traversal.


```elixir
defmodule BeamProfiler.CallGraph do
  @moduledoc """
  Builds a call graph from collected stack samples.

  Each node in the tree represents {module, function, arity}.
  The root is a virtual :root node. Each sample is a path from :root
  down to the leaf (the function currently executing).

  Node attributes:
  - total_samples: how many samples include this node anywhere in the path (inclusive time)
  - self_samples: how many samples have this node as the LEAF (exclusive/self time)
  - call_count: number of distinct samples entering this node from above
  """

  defstruct [:nodes, :edges]

  @type t :: %__MODULE__{
    nodes: %{term() => %{total_samples: non_neg_integer(), self_samples: non_neg_integer()}},
    edges: %{{term(), term()} => non_neg_integer()}
  }

  @doc """
  Builds a call graph from a list of samples.
  Each sample is %{pid: pid, stack: [{module, function, arity}, ...]}.
  """
  @spec build([map()]) :: t()
  def build(samples) do
    {nodes, edges} =
      Enum.reduce(samples, {%{}, %{}}, fn sample, {nodes_acc, edges_acc} ->
        stack = sample.stack

        reversed =
          stack
          |> Enum.map(fn
            {mod, fun, arity, _info} -> {mod, fun, arity}
            {mod, fun, arity} -> {mod, fun, arity}
          end)
          |> Enum.reverse()

        leaf =
          case reversed do
            [] -> nil
            list -> List.last(list)
          end

        path = [:root | reversed]

        nodes_acc =
          Enum.reduce(reversed, nodes_acc, fn mfa, acc ->
            Map.update(acc, mfa, %{total_samples: 1, self_samples: 0}, fn node ->
              %{node | total_samples: node.total_samples + 1}
            end)
          end)

        nodes_acc =
          if leaf do
            Map.update(nodes_acc, leaf, %{total_samples: 1, self_samples: 1}, fn node ->
              %{node | self_samples: node.self_samples + 1}
            end)
          else
            nodes_acc
          end

        edges_acc =
          path
          |> Enum.chunk_every(2, 1, :discard)
          |> Enum.reduce(edges_acc, fn [parent, child], acc ->
            Map.update(acc, {parent, child}, 1, &(&1 + 1))
          end)

        {nodes_acc, edges_acc}
      end)

    %__MODULE__{nodes: nodes, edges: edges}
  end

  @doc """
  Returns the top N nodes by self time (exclusive CPU consumption).
  Use this to find hotspots.
  """
  @spec top_by_self(t(), non_neg_integer()) :: [{mfa(), non_neg_integer()}]
  def top_by_self(%__MODULE__{nodes: nodes}, n \\ 20) do
    nodes
    |> Enum.map(fn {mfa, %{self_samples: s}} -> {mfa, s} end)
    |> Enum.sort_by(fn {_mfa, s} -> s end, :desc)
    |> Enum.take(n)
  end

  @doc """
  Returns the subtree rooted at node_mfa up to max_depth.
  Use this to explore why a specific function is hot.
  """
  @spec subtree(t(), mfa(), non_neg_integer()) :: t()
  def subtree(%__MODULE__{nodes: nodes, edges: edges} = _graph, node_mfa, max_depth \\ 5) do
    reachable = bfs_collect(edges, node_mfa, max_depth)
    filtered_nodes = Map.take(nodes, reachable)

    filtered_edges =
      edges
      |> Enum.filter(fn {{from, to}, _} -> from in reachable and to in reachable end)
      |> Map.new()

    %__MODULE__{nodes: filtered_nodes, edges: filtered_edges}
  end

  defp bfs_collect(edges, start, max_depth) do
    do_bfs(edges, [{start, 0}], MapSet.new([start]), max_depth)
    |> MapSet.to_list()
  end

  defp do_bfs(_edges, [], visited, _max_depth), do: visited

  defp do_bfs(edges, [{current, depth} | rest], visited, max_depth) when depth < max_depth do
    children =
      edges
      |> Enum.filter(fn {{from, _to}, _} -> from == current end)
      |> Enum.map(fn {{_from, to}, _} -> to end)
      |> Enum.reject(&MapSet.member?(visited, &1))

    new_visited = Enum.reduce(children, visited, &MapSet.put(&2, &1))
    new_queue = rest ++ Enum.map(children, &{&1, depth + 1})
    do_bfs(edges, new_queue, new_visited, max_depth)
  end

  defp do_bfs(edges, [_ | rest], visited, max_depth) do
    do_bfs(edges, rest, visited, max_depth)
  end
end
```

### Step 5: `lib/beam_profiler/flamegraph.ex`

**Objective**: Reverse stacks and fold into `%{joined => count}` so identical paths merge — the core of flame graph rendering.


```elixir
defmodule BeamProfiler.Flamegraph do
  @moduledoc """
  Exports a CallGraph to Brendan Gregg's collapsed stack format.

  Format: each line is "frame1;frame2;frame3 count"
  where frames go from outermost (root) to innermost (leaf).

  Example:
    Elixir.MyApp.Router.call/2;Elixir.MyApp.Controller.index/2 42
    Elixir.MyApp.Router.call/2;Elixir.MyApp.Controller.show/2 18

  Feed this to flamegraph.pl or Speedscope for visualization.
  """

  @doc """
  Writes the collapsed stack file for the given samples.
  Does NOT require a pre-built CallGraph — operates directly on raw samples
  for simplicity. Use CallGraph.build/1 when you need tree analytics.
  """
  @spec export(String.t(), [map()]) :: :ok | {:error, term()}
  def export(path, samples) do
    counts =
      Enum.reduce(samples, %{}, fn sample, acc ->
        stack =
          sample.stack
          |> Enum.reverse()
          |> Enum.map(fn
            {mod, fun, arity, _info} -> "#{inspect(mod)}.#{fun}/#{arity}"
            {mod, fun, arity} -> "#{inspect(mod)}.#{fun}/#{arity}"
          end)

        stack_string = Enum.join(stack, ";")

        if stack_string != "" do
          Map.update(acc, stack_string, 1, &(&1 + 1))
        else
          acc
        end
      end)

    content =
      counts
      |> Enum.map(fn {stack, count} -> "#{stack} #{count}" end)
      |> Enum.join("\n")

    File.write!(path, content <> "\n")
    :ok
  end

  @doc """
  Exports to Speedscope JSON format for interactive visualization.
  See: https://www.speedscope.app/file-format-spec.json
  """
  @spec export_speedscope(String.t(), [map()]) :: :ok | {:error, term()}
  def export_speedscope(path, samples) do
    all_frames =
      samples
      |> Enum.flat_map(fn sample ->
        Enum.map(sample.stack, fn
          {mod, fun, arity, _info} -> "#{inspect(mod)}.#{fun}/#{arity}"
          {mod, fun, arity} -> "#{inspect(mod)}.#{fun}/#{arity}"
        end)
      end)
      |> Enum.uniq()

    frame_index = all_frames |> Enum.with_index() |> Map.new()
    shared_frames = Enum.map(all_frames, fn name -> %{"name" => name} end)

    profile_samples =
      Enum.map(samples, fn sample ->
        indices =
          sample.stack
          |> Enum.reverse()
          |> Enum.map(fn
            {mod, fun, arity, _info} -> Map.get(frame_index, "#{inspect(mod)}.#{fun}/#{arity}", 0)
            {mod, fun, arity} -> Map.get(frame_index, "#{inspect(mod)}.#{fun}/#{arity}", 0)
          end)

        indices
      end)

    speedscope = %{
      "$schema" => "https://www.speedscope.app/file-format-schema.json",
      "shared" => %{"frames" => shared_frames},
      "profiles" => [
        %{
          "type" => "sampled",
          "name" => "beam_profiler",
          "unit" => "none",
          "startValue" => 0,
          "endValue" => length(profile_samples),
          "samples" => profile_samples,
          "weights" => List.duplicate(1, length(profile_samples))
        }
      ]
    }

    File.write!(path, Jason.encode!(speedscope, pretty: true))
    :ok
  end
end
```

### Step 6: Given tests — must pass without modification

**Objective**: Pin leaf-vs-inclusive accounting and stack-merge semantics — the two invariants that break flame graph correctness silently.


```elixir
# test/beam_profiler/call_graph_test.exs
defmodule BeamProfiler.CallGraphTest do
  use ExUnit.Case, async: true

  alias BeamProfiler.CallGraph

  @sample_a %{
    pid: self(),
    stack: [{MyApp.Controller, :index, 2}, {MyApp.Router, :call, 2}]
  }
  @sample_b %{
    pid: self(),
    stack: [{MyApp.Controller, :show, 2}, {MyApp.Router, :call, 2}]
  }
  @sample_c %{
    pid: self(),
    stack: [{MyApp.Controller, :index, 2}, {MyApp.Router, :call, 2}]
  }


  describe "CallGraph" do

  test "self samples count only for leaf nodes" do
    graph = CallGraph.build([@sample_a, @sample_b, @sample_c])
    # Router.call appears in all 3 samples as non-leaf → self = 0
    # Controller.index is leaf in 2 samples → self = 2
    # Controller.show is leaf in 1 sample → self = 1

    assert get_self(graph, {MyApp.Router, :call, 2}) == 0
    assert get_self(graph, {MyApp.Controller, :index, 2}) == 2
    assert get_self(graph, {MyApp.Controller, :show, 2}) == 1
  end

  test "total samples count inclusive time" do
    graph = CallGraph.build([@sample_a, @sample_b, @sample_c])
    # Router.call appears in all 3 samples
    assert get_total(graph, {MyApp.Router, :call, 2}) == 3
  end

  test "top_by_self returns hottest leaf first" do
    graph = CallGraph.build([@sample_a, @sample_b, @sample_c])
    [{top_mfa, _} | _] = CallGraph.top_by_self(graph, 5)
    assert top_mfa == {MyApp.Controller, :index, 2}
  end

  defp get_self(graph, mfa) do
    case Map.get(graph.nodes, mfa) do
      nil -> 0
      node -> node.self_samples
    end
  end

  defp get_total(graph, mfa) do
    case Map.get(graph.nodes, mfa) do
      nil -> 0
      node -> node.total_samples
    end
  end


  end
end
```

```elixir
# test/beam_profiler/flamegraph_test.exs
defmodule BeamProfiler.FlamegraphTest do
  use ExUnit.Case, async: true

  alias BeamProfiler.Flamegraph


  describe "Flamegraph" do

  test "identical stacks are merged with summed counts" do
    samples = [
      %{pid: self(), stack: [{C, :f, 1}, {B, :g, 2}, {A, :h, 3}]},
      %{pid: self(), stack: [{C, :f, 1}, {B, :g, 2}, {A, :h, 3}]},
      %{pid: self(), stack: [{D, :x, 0}, {A, :h, 3}]}
    ]

    path = System.tmp_dir!() |> Path.join("test_flamegraph.txt")
    :ok = Flamegraph.export(path, samples)

    content = File.read!(path)
    lines = String.split(content, "\n", trim: true)

    counts = Map.new(lines, fn line ->
      [stack, count] = String.split(line, " ")
      {stack, String.to_integer(count)}
    end)

    # The A→B→C path appeared twice → count 2
    assert Enum.any?(counts, fn {_k, v} -> v == 2 end)
    # The A→D path appeared once → count 1
    assert Enum.any?(counts, fn {_k, v} -> v == 1 end)

    File.rm!(path)
  end


  end
end
```

### Step 7: Run the tests

**Objective**: Use `--trace` so flamegraph-file tests fail with clear ordering when temp-dir cleanup races across async cases.


```bash
mix test test/beam_profiler/ --trace
```

### Step 8: Overhead benchmark

**Objective**: Sample 500 dummy processes so per-round cost is measurable against the 5% production budget at 99 Hz.


```elixir
# bench/overhead_bench.exs
# Measure CPU cost of one sampling round against N processes
pids = for _ <- 1..500, do: spawn(fn -> Process.sleep(:infinity) end)

Benchee.run(
  %{
    "sample 500 processes" => fn ->
      Enum.each(pids, fn pid ->
        Process.info(pid, [:current_stacktrace])
      end)
    end
  },
  time: 5,
  warmup: 2
)

Enum.each(pids, &Process.exit(&1, :kill))
```

```bash
mix run bench/overhead_bench.exs
```

Expected: sampling 500 processes should complete in under 1ms per round. At 100 Hz that is 0.1% overhead — well within the 5% budget. If you see > 5ms, investigate whether `:current_stacktrace` is being called with deep recursion limits.

---

### Why this works

The design separates concerns along their real axes: what must be correct (the BEAM profiler invariants), what must be fast (the hot path isolated from slow paths), and what must be evolvable (external contracts kept narrow). Each module has one job and fails loudly when given inputs outside its contract, so bugs surface near their source instead of as mysterious downstream symptoms. The tests exercise the invariants directly rather than implementation details, which keeps them useful across refactors.


## Main Entry Point

```elixir
def main do
  IO.puts("======== 30 build profiler performance analyzer ========")
  IO.puts("Demonstrating core functionality")
  IO.puts("")
  
  IO.puts("Run: mix test")
end
```

## Benchmark

```elixir
# bench/overhead_bench.exs
alias BeamProfiler.{Sampler, CallGraph, Flamegraph}

# Setup: simulate production workload with N concurrent processes
spawn_load = fn n ->
  for _ <- 1..n do
    spawn(fn ->
      for _ <- 1..10_000 do
        _ = :math.sin(:rand.uniform()) + :math.cos(:rand.uniform())
        # Simulate I/O wait
        :timer.sleep(1)
      end
    end)
  end
end

IO.puts("Spawning 100 worker processes...")
pids = spawn_load.(100)

IO.puts("Benchmarking sampler overhead...")

Benchee.run(
  %{
    "sample 100 processes at 100 Hz" => fn ->
      {:ok, session} = Sampler.start_session(hz: 100, duration_ms: 1000)
      :timer.sleep(1_010)
      {:ok, samples} = Sampler.collect(session)
      samples
    end,
    "sample 500 processes at 100 Hz" => fn ->
      pids2 = spawn_load.(400)
      {:ok, session} = Sampler.start_session(hz: 100, duration_ms: 1000)
      :timer.sleep(1_010)
      {:ok, samples} = Sampler.collect(session)
      Enum.each(pids2, &Process.exit(&1, :kill))
      samples
    end,
    "build call graph from 1000 samples" => fn ->
      {:ok, session} = Sampler.start_session(hz: 100, duration_ms: 100)
      :timer.sleep(110)
      {:ok, samples} = Sampler.collect(session)
      if length(samples) > 0 do
        CallGraph.build(samples)
      end
    end,
    "export flame graph (1000 samples)" => fn ->
      {:ok, session} = Sampler.start_session(hz: 100, duration_ms: 100)
      :timer.sleep(110)
      {:ok, samples} = Sampler.collect(session)
      path = System.tmp_dir!() |> Path.join("bench_flame_#{:erlang.unique_integer()}.txt")
      if length(samples) > 0 do
        Flamegraph.export(path, samples)
      end
      File.rm!(path)
    end
  },
  parallel: 1,
  time: 10,
  warmup: 2,
  formatters: [Benchee.Formatters.Console]
)

Enum.each(pids, &Process.exit(&1, :kill))

IO.puts("\nOverhead analysis:")
IO.puts("  - Sampling 100 processes at 100 Hz = 10ms/s = <1% CPU overhead")
IO.puts("  - Sampling 500 processes at 100 Hz = 50ms/s = <5% CPU overhead (production limit)")
IO.puts("  - Call graph build should be sub-millisecond")
IO.puts("  - Flame graph export should be sub-millisecond")
```

Target: <2% throughput overhead at 99 Hz sampling on a node doing 50k req/s (100 processes sampling = 1ms/s = 0.1% CPU). At 500 processes (production scale) sampling should stay under 5% CPU overhead.

## Key Concepts: Stack Sampling, Instrumentation, and Flame Graphs

**Stack sampling vs. instrumentation**: Two complementary profiling techniques, different answers.
- **Sampling** (statistical): Freeze all processes every N milliseconds, record current stacks. Low overhead (< 1% CPU), representative, but short-lived functions may be missed.
- **Instrumentation** (exact): Wrap function entry/exit with timers. Every call is measured. High overhead (5-30% CPU), but all calls are seen. Use for targeted deep-dive into specific functions.

Together they form a profiler strategy: sample globally to find hot modules, then instrument the hottest function to get exact timing.

**Flame graph semantics**: A flame graph collapses identical stack paths across all samples. Each unique path is one bar; the bar's width is the number of samples. Reading a flame graph:
- Wide bar = function is always on the stack (may be a hot loop or a caller of hot code).
- Narrow bar = function rarely sampled (either truly fast or rarely called).
- Tall stack = deep call chain (each row is a caller, top row is leaf/hottest function).

The key insight: a function appearing in every sample but never as a leaf (not the topmost frame) is expensive because it calls other expensive functions, not because of its own code.

**Production profiling constraints**: Attaching a profiler to a live production node has strict budgets.
- **Overhead budget**: < 5% CPU overhead for profiling. At 100 Hz sampling of 500 processes, expect ~50ms/s = 0.05 CPU time.
- **Memory budget**: Do not buffer unbounded samples. Flush/discard old samples periodically.
- **Detach safety**: `:dbg.stop()` must completely remove all tracing instrumentation. A leaked trace handler consumes memory silently.

**GC correlation**: Garbage collection pauses show up in flame graphs as wide stacks of `:erts_internal` functions. The correlation problem: if you see `:erts_internal:garbage_collection` in many samples, you know GC is taking CPU time, but not which user code triggered it. Solve by logging GC events separately and correlating by timestamp.

---

## Trade-off analysis

| Aspect | Sampling (100 Hz) | Instrumentation (:dbg) | Both simultaneously |
|--------|------------------|----------------------|---------------------|
| CPU overhead | ~0.1–1% | 5–30% | additive |
| Accuracy | statistical (±1%) | exact | exact |
| Coverage | all processes | selected modules only | selected + statistical |
| Minimum resolvable call time | ~10ms (1/100Hz) | any duration | any |
| Safe in production | yes | use rate limiting | risky |
| Correlates with GC events | no (separate tool) | no | no |

Reflection: why does sampling miss functions that complete in under 10ms at 100 Hz? What sampling rate would you need to reliably capture a 1ms function called 1000 times/second?

---

## Common production mistakes

**1. Not rate-limiting `:dbg` traces**
`:dbg` sends a message per traced function call. A function called 100k/s generates 100k messages/s to the trace handler. This saturates the handler mailbox and can OOM the node. Use `:dbg.tpl/4` match specs with a `{message, [], [{silent, true}]}` action and add rate limiting in the tracer process.

**2. Forgetting to call `:dbg.stop()` on exit**
If your profiler crashes without stopping `:dbg`, the node continues tracing silently, consuming memory. Always use `try/after` or a monitoring process that calls `:dbg.stop()` when the profiler dies.

**3. Correlating GC events to the wrong process**
`{:garbage_collection, pid, info}` trace messages arrive asynchronously. By the time you process the message, the process may be in a different function. The correlation must happen inside the trace handler at message receipt time, not after batching.

**4. Reversed stack direction**
`:current_stacktrace` returns `[current_frame, caller, caller_of_caller, ...]`. Flame graphs expect `[root, ..., leaf]`. Reversing the list before building the flame graph is mandatory.

**5. Attaching to a production node without a rate-limit budget**
The `recon` library's `recon_trace` module implements a safety limit on trace message rate. Study its approach before attaching to any node handling real traffic.

---

## Reflection

If a latency spike lasts exactly 400ms once per hour, will your 99 Hz sampler catch it? What sampling strategy would guarantee capture, and at what overhead cost?

## Resources

- [`:dbg` module — Erlang/OTP](https://www.erlang.org/doc/man/dbg.html) — read the match specification section; the pattern language is powerful but complex
- [`:erlang.trace/3` — Erlang/OTP](https://www.erlang.org/doc/man/erlang.html#trace-3) — the low-level tracing primitive `:dbg` builds on
- [Brendan Gregg — Flame Graphs](https://www.brendangregg.com/flamegraphs.html) — original blog post including `flamegraph.pl` source
- [Speedscope](https://github.com/jlfwong/speedscope) — interactive flame graph viewer; read the file format spec before implementing `export_speedscope/2`
- [`recon` library](https://hex.pm/packages/recon) — Fred Hebert's production-safe tracing toolkit; study `recon_trace.erl` for rate-limiting patterns
- [The Erlang Runtime System (ERTS)](https://adoptingerlang.org/docs/development/erts/) — open-source book; chapters on the process model and GC are essential background
