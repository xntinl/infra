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

```bash
mix new beam_profiler --sup
cd beam_profiler
mkdir -p test/beam_profiler bench
```

### Step 2: `mix.exs`

```elixir
defp deps do
  [
    {:jason, "~> 1.4"},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: `lib/beam_profiler/sampler.ex`

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
    {:ok, %{sessions: %{}}}
  end

  @impl true
  def handle_call({:start_session, hz, duration_ms}, _from, state) do
    session_id = make_ref()
    interval_ms = div(1_000, hz)

    # TODO: start a timer that fires every interval_ms
    # TODO: on each tick, collect stacktraces from all processes:
    #   pids = Process.list()
    #   samples = Enum.map(pids, fn pid ->
    #     case Process.info(pid, [:current_stacktrace, :registered_name]) do
    #       nil -> nil   # process died between list and info
    #       info -> %{pid: pid, stack: info[:current_stacktrace], ts: System.monotonic_time()}
    #     end
    #   end)
    # TODO: accumulate samples in session state (ETS for memory efficiency)
    # TODO: after duration_ms, mark session as complete (send :session_done to self)

    # Design question: why store samples in ETS rather than the GenServer state map?
    # A 30-second session at 100 Hz with 500 processes = 1.5M samples.
    # Storing in a map would cause process heap growth and GC pressure on the profiler
    # itself, skewing the data. ETS is off-heap.

    {:reply, {:ok, session_id}, state}
  end

  @impl true
  def handle_call({:collect, session_id}, _from, state) do
    # TODO: check if session exists and is complete
    # TODO: read all samples from ETS for this session_id
    # TODO: delete ETS entries to free memory
    {:reply, {:error, :not_implemented}, state}
  end

  @impl true
  def handle_info({:sample, session_id}, state) do
    # TODO: collect one round of samples from all processes
    # TODO: insert into ETS table :profiler_samples with key {session_id, timestamp}
    # TODO: reschedule next sample tick unless session is done
    {:noreply, state}
  end
end
```

### Step 4: `lib/beam_profiler/call_graph.ex`

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

  @doc """
  Builds a call graph from a list of samples.
  Each sample is %{pid: pid, stack: [{module, function, arity}, ...]}.
  """
  @spec build([map()]) :: t()
  def build(samples) do
    # TODO: for each sample:
    #   1. reverse the stack (samples come leaf-first, we want root-first)
    #   2. walk the path, creating or updating nodes
    #   3. increment self_samples only for the original leaf
    # HINT: use a map %{node_id => %{total: n, self: n}} as the accumulator
    # HINT: node_id = {module, function, arity} | :root
  end

  @doc """
  Returns the top N nodes by self time (exclusive CPU consumption).
  Use this to find hotspots.
  """
  @spec top_by_self(t(), non_neg_integer()) :: [{mfa(), non_neg_integer()}]
  def top_by_self(%__MODULE__{} = graph, n \\ 20) do
    # TODO: sort nodes by self_samples descending, return top n
  end

  @doc """
  Returns the subtree rooted at node_mfa up to max_depth.
  Use this to explore why a specific function is hot.
  """
  @spec subtree(t(), mfa(), non_neg_integer()) :: t()
  def subtree(%__MODULE__{} = graph, node_mfa, max_depth \\ 5) do
    # TODO: BFS/DFS from node_mfa, limiting depth
  end
end
```

### Step 5: `lib/beam_profiler/flamegraph.ex`

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
    # TODO: fold samples into %{stack_string => count}
    #   stack_string = Enum.join(reversed_mfa_list, ";")
    #   mfa_string = "#{inspect(mod)}.#{fun}/#{arity}"
    # TODO: write "stack_string count\n" for each entry
    # TODO: File.write!(path, content)
  end

  @doc """
  Exports to Speedscope JSON format for interactive visualization.
  See: https://www.speedscope.app/file-format-spec.json
  """
  @spec export_speedscope(String.t(), [map()]) :: :ok | {:error, term()}
  def export_speedscope(path, samples) do
    # TODO: build Speedscope JSON with "shared.frames" and "profiles" sections
    # HINT: Speedscope uses integer frame indices to avoid string repetition
    # HINT: the "sampled" profile type expects {startValue, endValue, stack: [indices]}
  end
end
```

### Step 6: Given tests — must pass without modification

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
```

```elixir
# test/beam_profiler/flamegraph_test.exs
defmodule BeamProfiler.FlamegraphTest do
  use ExUnit.Case, async: true

  alias BeamProfiler.Flamegraph

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
```

### Step 7: Run the tests

```bash
mix test test/beam_profiler/ --trace
```

### Step 8: Overhead benchmark

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

## Resources

- [`:dbg` module — Erlang/OTP](https://www.erlang.org/doc/man/dbg.html) — read the match specification section; the pattern language is powerful but complex
- [`:erlang.trace/3` — Erlang/OTP](https://www.erlang.org/doc/man/erlang.html#trace-3) — the low-level tracing primitive `:dbg` builds on
- [Brendan Gregg — Flame Graphs](https://www.brendangregg.com/flamegraphs.html) — original blog post including `flamegraph.pl` source
- [Speedscope](https://github.com/jlfwong/speedscope) — interactive flame graph viewer; read the file format spec before implementing `export_speedscope/2`
- [`recon` library](https://hex.pm/packages/recon) — Fred Hebert's production-safe tracing toolkit; study `recon_trace.erl` for rate-limiting patterns
- [The Erlang Runtime System (ERTS)](https://adoptingerlang.org/docs/development/erts/) — open-source book; chapters on the process model and GC are essential background
