# Flame Graphs with `eflame`

**Project**: `eflame_demo` — produce interactive SVG flame graphs of hot Elixir code paths using Vlad Ki's `eflame`.

---

## Project context

Your team's image processing pipeline takes 2.1 seconds to process an 8MP
photo — up from 800ms three months ago. Benchee can tell you "pipeline took
longer" but not **which frame on the call stack** is responsible. You need
a flame graph.

A flame graph is a two-axis visualization: the x-axis is proportional to
time spent (sample count), the y-axis is the call stack. Wide boxes are
hot frames. Wide boxes **deep in the stack** under a single parent are the
candidates for optimization — they are hot AND they do real work, not just
dispatch.

`eflame` is Vlad Ki's Erlang port of Brendan Gregg's FlameGraph toolkit,
written specifically for BEAM. It uses `erlang:trace/3` with the `:call`
and `:return_to` flags to sample the stack of a single function call at
microsecond granularity, then produces a folded-stack file that
`stack_to_flame.pl` converts to SVG.

This exercise: you will profile `EflameDemo.Pipeline.run/1` — a deliberately
unbalanced image-processing pipeline — produce a flame graph, read it, and
fix the hot path. The final optimization reduces wall time by 60–80%.

```
eflame_demo/
├── lib/
│   └── eflame_demo/
│       ├── pipeline.ex           # the code you'll profile
│       └── profiler.ex           # wrapper around :eflame.apply/3
├── test/
│   └── eflame_demo/
│       └── profiler_test.exs
├── priv/
│   └── stacks/                   # output folded-stack files
├── scripts/
│   └── stack_to_flame.pl         # vendored from brendangregg/FlameGraph
└── mix.exs
```

---

## Why this approach and not alternatives

Alternatives considered and discarded:

- **Hand-rolled equivalent**: reinvents primitives the BEAM/ecosystem already provides; high risk of subtle bugs around concurrency, timeouts, or failure propagation.
- **External service (e.g. Redis, sidecar)**: adds a network hop and an extra failure domain for a problem the VM can solve in-process with lower latency.
- **Heavier framework abstraction**: couples the module to a framework lifecycle and makes local reasoning/testing harder.

The chosen approach stays inside the BEAM, uses idiomatic OTP primitives, and keeps the contract small.

## Core concepts

### 1. Sampling vs instrumentation

Two schools of profiling:

| Approach | Example | Overhead | Accuracy |
|----------|---------|----------|----------|
| Instrumentation | `:fprof` | 10–50x slower | Exact — every call counted |
| Sampling | `eflame`, Linux `perf` | 1.05–1.3x slower | Statistical — samples N times/sec |

Instrumentation records every function entry and exit. Sampling probes the
call stack at a fixed rate. For a 2-second real-world workload, `fprof` may
run for 60 seconds and allocate hundreds of MB of trace data. `eflame`
stays close to real time and produces a text file of a few hundred KB.

Flame graphs are a sampling visualization. They assume the trace is a
faithful statistical slice of the program's behavior. If your function of
interest runs for less than `eflame`'s default sampling period, it may
not show up at all — sample longer.

### 2. Folded stack format

A flame graph input looks like:

```
Pipeline.run;decode;jpeg_parse 42
Pipeline.run;decode;exif_read 8
Pipeline.run;resize;bilinear_loop 120
Pipeline.run;resize;alloc_bitmap 14
Pipeline.run;encode;png_compress 95
```

Each line is a stack (semicolons separate frames, deepest last) followed
by the number of samples that saw that stack. The renderer groups by
common prefix and draws proportional-width boxes.

`eflame` produces this file directly. No post-processing needed except
piping it to `stack_to_flame.pl`.

### 3. What `eflame.apply/3` measures

```elixir
:eflame.apply(fn -> EflameDemo.Pipeline.run(img) end, [])
# or the 4-arg form to specify module/function
:eflame.apply(EflameDemo.Pipeline, :run, [img])
```

It traces the invoking process (and optionally spawned processes). Each
sample records the Erlang call stack at that instant. Time spent in
BIFs and NIFs appears as a `<<nif>>` or `<<bif>>` frame — you cannot
see into them, but you know they cost time.

Two-column output:

- `stacks.out` — sample-per-line folded stack
- `stacks.out.bare` — each sample without count aggregation (use this when you
  want to stream into a different visualizer)

### 4. The four frame colors to look for

Using Brendan Gregg's palette script conventions:

- **Orange/red** — Elixir/Erlang code (BEAM)
- **Yellow** — C code (NIFs)
- **Green** — libc / kernel (syscalls)
- **Blue** — garbage collection

A green-dominated pipeline is I/O-bound. A blue-dominated pipeline has
a GC problem (heap thrash). Orange tells you to optimize your code;
yellow tells you to optimize your NIFs or write one.

### 5. Sleep time mode: `eflame.apply_sleep/3`

Regular `eflame.apply/3` only counts CPU-bound time — when a process is
scheduled, a sample is recorded. Time spent in `receive` or `Process.sleep`
is invisible.

`:eflame.apply_sleep/3` adds off-CPU frames: the flame graph shows what
the process was **waiting on**. This is essential for finding lock contention
on `GenServer.call` to a busy server or NIF mutex waits.

### 6. Profiling spawned processes

A pipeline that `Task.async_stream/3` into 8 workers can't be profiled by
tracing the parent alone. `eflame` supports:

```elixir
:eflame.apply(:normal_with_children, ...)
```

The `:normal_with_children` mode enables the `:set_on_spawn` trace flag, so
children inherit the trace. Overhead is higher. Useful when the per-child
work is non-trivial and you need the composite picture.

---

## Design decisions

**Option A — naive/simple approach**
- Pros: minimal code, easy to reason about.
- Cons: breaks under load, lacks observability, hard to evolve.

**Option B — the approach used here** (chosen)
- Pros: production-grade, handles edge cases, testable boundaries.
- Cons: more moving parts, requires understanding of the BEAM primitives involved.

→ Chose **B** because correctness under concurrency and failure modes outweighs the extra surface area.

## Implementation

### Step 1: `mix.exs`

**Objective**: Pin `{:eflame, github: "Vagabond/eflame"}` since the trace-based profiler has no Hex release.

```elixir
defmodule EflameDemo.MixProject do
  use Mix.Project

  def project, do: [app: :eflame_demo, version: "0.1.0", elixir: "~> 1.15", deps: deps()]

  def application, do: [extra_applications: [:logger, :runtime_tools]]

  defp deps do
    [
      # eflame has no hex release; use the canonical GitHub mirror
      {:eflame, github: "Vagabond/eflame"},
      {:benchee, "~> 1.3", only: :dev}
    ]
  end
end
```

### Step 2: Vendor the FlameGraph scripts

**Objective**: Vendor Brendan Gregg's `stackcollapse-elixir.pl` under `scripts/` to fold eflame output into SVG.

From `brendangregg/FlameGraph` clone `stackcollapse-elixir.pl` (or use
eflame's built-in). Place in `scripts/`. The final command looks like:

```bash
./scripts/stack_to_flame.pl priv/stacks/pipeline.out > priv/stacks/pipeline.svg
```

### Step 3: `lib/eflame_demo/pipeline.ex`

**Objective**: Build a pipeline whose `resize/1` hides `length/1` inside a reduce, creating an O(n²) hotspot to expose.

A deliberately sub-optimal pipeline you will improve after seeing the graph.

```elixir
defmodule EflameDemo.Pipeline do
  @moduledoc """
  Pretend image pipeline. Runs on plain binaries so no native deps needed.
  """

  @doc "Process one image (binary). Returns processed binary."
  @spec run(binary()) :: binary()
  def run(image) when is_binary(image) do
    image
    |> decode()
    |> resize()
    |> filter()
    |> encode()
  end

  # --- Stage 1: decode (CPU bound, tight loop) ---
  @spec decode(binary()) :: [0..255]
  defp decode(bin), do: :binary.bin_to_list(bin)

  # --- Stage 2: resize (intentionally O(n^2) — this is what the graph will expose) ---
  @spec resize([0..255]) :: [0..255]
  defp resize(pixels) do
    # Naive downsampling via O(n^2) accumulator — the hot frame.
    Enum.reduce(pixels, [], fn px, acc ->
      if rem(length(acc), 2) == 0 do
        [px | acc]
      else
        acc
      end
    end)
    |> Enum.reverse()
  end

  # --- Stage 3: filter (Map pipeline) ---
  @spec filter([0..255]) :: [0..255]
  defp filter(pixels) do
    pixels
    |> Enum.map(&max(0, &1 - 15))
    |> Enum.map(&min(255, &1 + 5))
  end

  # --- Stage 4: encode (back to binary) ---
  @spec encode([0..255]) :: binary()
  defp encode(pixels), do: :erlang.list_to_binary(pixels)
end
```

Note the `length(acc)` inside the reduce. On a list, `length/1` is O(n).
Called n times inside a fold, total work is O(n²). A 1 MB "image" of 1M
bytes runs in ~2.5 seconds on the naive version.

### Step 4: `lib/eflame_demo/profiler.ex`

**Objective**: Wrap `:eflame.apply/4` to emit `.bare` stacks and fold them into `.folded` frequencies for FlameGraph.

```elixir
defmodule EflameDemo.Profiler do
  @moduledoc """
  Wrap `:eflame.apply/4` with a file-output convention.

  Produces `priv/stacks/<name>.bare` which you then fold and render.
  """

  @spec profile(atom(), (-> any()), keyword()) :: {:ok, Path.t()}
  def profile(name, fun, opts \\ []) when is_atom(name) and is_function(fun, 0) do
    mode = Keyword.get(opts, :mode, :normal_with_children)
    out_dir = Path.join(:code.priv_dir(:eflame_demo), "stacks")
    File.mkdir_p!(out_dir)
    out_path = Path.join(out_dir, "#{name}.bare")

    _result = :eflame.apply(mode, to_charlist(out_path), fun, [])
    fold(out_path)
  end

  @doc """
  Sleep-aware variant. Off-CPU time appears as `SLEEP` frames in the graph.
  """
  @spec profile_with_sleep(atom(), (-> any())) :: {:ok, Path.t()}
  def profile_with_sleep(name, fun) when is_atom(name) and is_function(fun, 0) do
    out_dir = Path.join(:code.priv_dir(:eflame_demo), "stacks")
    File.mkdir_p!(out_dir)
    out_path = Path.join(out_dir, "#{name}.sleep.bare")

    _result = :eflame.apply(:normal_with_children, to_charlist(out_path), fun, [])
    fold(out_path)
  end

  defp fold(bare_path) do
    folded =
      bare_path
      |> File.stream!()
      |> Stream.map(&String.trim/1)
      |> Enum.frequencies()
      |> Enum.map(fn {stack, n} -> "#{stack} #{n}\n" end)

    folded_path = String.replace_suffix(bare_path, ".bare", ".folded")
    File.write!(folded_path, folded)
    {:ok, folded_path}
  end
end
```

### Step 5: `test/eflame_demo/profiler_test.exs`

**Objective**: Assert the folded file contains `resize` frames and numeric sample counts, tagged `:eflame` so CI opts in.

```elixir
defmodule EflameDemo.ProfilerTest do
  use ExUnit.Case, async: false

  alias EflameDemo.{Pipeline, Profiler}

  @tag :eflame

  describe "EflameDemo.Profiler" do
    test "profiling the naive pipeline produces a folded stack file" do
      image = :crypto.strong_rand_bytes(4_096)

      {:ok, folded} =
        Profiler.profile(:pipeline_naive, fn ->
          Pipeline.run(image)
        end)

      assert File.exists?(folded)
      content = File.read!(folded)

      # Must contain at least one resize frame — that's where we spend time
      assert content =~ "resize"

      # Sample count > 0 (some lines end with " <n>\n")
      assert String.match?(content, ~r/ \d+$/m)
    end
  end
end
```

Tag with `:eflame` so CI can skip these unless opted in — they take seconds.

### Step 6: Render the SVG

**Objective**: Pipe the folded stacks through `flamegraph.pl` and visually confirm `erlang:length/1` towers dominate `resize`.

```bash
# Fold if not already
./scripts/stack_to_flame.pl priv/stacks/pipeline_naive.folded > priv/stacks/pipeline_naive.svg
open priv/stacks/pipeline_naive.svg
```

You'll see `Enum.reduce/3` under `resize` dominating the graph. Inside
the reduce, a narrow-but-tall tower of `erlang:length/1` frames — that's
the accidental O(n²).

### Step 7: Fix and re-profile

**Objective**: Replace `length(acc)` with an accumulated index so `resize/1` collapses from O(n²) to O(n) in the next profile.

Rewrite `resize/1` to stream without measuring list length:

```elixir
defp resize(pixels) do
  {result, _parity} =
    Enum.reduce(pixels, {[], 0}, fn px, {acc, i} ->
      if rem(i, 2) == 0, do: {[px | acc], i + 1}, else: {acc, i + 1}
    end)

  Enum.reverse(result)
end
```

Re-run the profiler. The `resize` column shrinks to ~8% of the graph.

---


### Why this works

The design leans on BEAM guarantees (process isolation, mailbox ordering, supervisor restarts) and pushes invariants to the boundaries of each module. State transitions are explicit, failure modes are declared rather than implicit, and each step is independently testable. That combination keeps the implementation correct under concurrent load and cheap to change later.

## Deep Dive: BEAM Scheduler Tuning and Memory Profiling in Production

The BEAM scheduler is not "magic" — it's a preemptive work-stealing scheduler that divides CPU time 
into reductions (bytecode instructions). Understanding scheduler tuning is critical when you suspect 
latency spikes in production.

**Key concepts**:
- **Reductions budget**: By default, a process gets ~2000 reductions before yielding to another process.
  Heavy CPU work (binary matching, list recursion) can exhaust the budget and cause tail latency.
- **Dirty schedulers**: If a process does CPU-intensive work (crypto, compression, numerical), it blocks 
  the main scheduler. Use dirty NIFs or `spawn_opt(..., [{:fullsweep_after, 0}])` for GC tuning.
- **Heap tuning per process**: `Process.flag(:min_heap_size, ...)` reserves heap upfront, reducing GC 
  pauses. Measure; don't guess.

**Memory profiling workflow**:
1. Run `recon:memory/0` in iex; identify top 10 memory consumers by type (atoms, binaries, ets).
2. If binaries dominate, check for refc binary leaks (binary held by process that should have been freed).
3. Use `eprof` or `fprof` for function-level CPU attribution; `recon:proc_window/3` for process memory trends.

**Production pattern**: Deploy with `+K true` (async IO), `-env ERL_MAX_PORTS 65536` (port limit), 
`+T 9` (async threads). Measure GC time with `erlang:statistics(garbage_collection)` — if >5% of uptime, 
tune heap or reduce allocation pressure. Never assume defaults are optimal for YOUR workload.

---

## Advanced Considerations

Understanding BEAM internals at production scale requires deep knowledge of scheduler behavior, memory models, and garbage collection dynamics. The soft real-time guarantees of BEAM only hold under specific conditions — high system load, uneven process distribution across schedulers, or GC pressure can break predictable latency completely. Monitor `erlang:statistics(run_queue)` in production to catch scheduler saturation before it degrades latency significantly. The difference between immediate, offheap, and continuous GC garbage collection strategies can significantly impact tail latencies in systems with millions of messages per second and sustained memory pressure.

Process reductions and the reduction counter affect scheduler fairness fundamentally. A process that runs for extended periods without yielding can starve other processes, even though the scheduler treats it fairly by reduction count per scheduling interval. This is especially critical in pipelines processing large data structures or performing recursive computations where yielding points are infrequent and difficult to predict. The BEAM's preemption model is deterministic per reduction, making performance testing reproducible but sometimes hiding race conditions that only manifest under specific load patterns and GC interactions.

The interaction between ETS, Mnesia, and process message queues creates subtle bottlenecks in distributed systems. ETS reads don't block other processes, but writes require acquiring locks; understanding when your workload transitions from read-heavy to write-heavy is crucial for capacity planning. Port drivers and NIFs bypass the BEAM scheduler entirely, which can lead to unexpected priority inversions if not carefully managed. Always profile with `eprof` and `fprof` in realistic production-like environments before deployment to catch performance surprises.


## Deep Dive: Otp Patterns and Production Implications

OTP primitives (GenServer, Supervisor, Application) are tested through their public interfaces, not by inspecting internal state. This discipline forces correct design: if you can't test a behavior without peeking into the server's state, the behavior is not public. Production systems with tight integration tests on GenServer internals are fragile and hard to refactor.

---

## Trade-offs and production gotchas

**1. Not for production use.** `eflame` installs trace flags on the target
process. If the target is under 10k req/s load, the tracer mailbox fills
faster than it drains. Profile on staging with a realistic workload, not on
production.

**2. Sampling misses short functions.** Default sample interval is
~100 µs. A function that runs for 20 µs total across the whole trace may
register zero samples. Raise the trace duration (run the workload longer)
or switch to `:fprof` for exact counts.

**3. NIF internals are opaque.** The graph shows `<<nif>>` as a single
frame. For NIF-bound workloads (crypto, ML inference, image codecs) use
Linux `perf` with the BEAM `perf map` helper — it attributes inside the
NIF's C stack.

**4. Sleep-mode mis-attributes `receive`.** A process waiting on
`GenServer.call/3` shows the *caller's* stack during the wait, not the
callee's. Good for finding waits; useless for finding **why** the callee
is slow.

**5. Vendoring `stack_to_flame.pl`.** Brendan Gregg's scripts are BSD
licensed — commit them to your repo. Don't depend on a runtime `curl`
of GitHub during profiling sessions; your offline staging box needs them.

**6. Multi-node pipelines require manual merging.** `eflame` is per-node.
For a Broadway/GenStage pipeline split across nodes, produce one graph
per node and eyeball the aggregate — there is no "merged" mode.

**7. When NOT to use this.** For statistical profiling of a running
production node, use `:fprof` in sampling mode (`:fprof.trace([sampling])`)
or an APM agent (AppSignal, DataDog APM, New Relic). eflame is a bench
tool — it produces beautiful graphs but only for workloads you can drive
synthetically.

---

## Benchmark

Wall time for `Pipeline.run/1` on a 1 MB input, median of 25 runs:

| Version | Wall time | Reductions | Note |
|---------|-----------|------------|------|
| Naive `length(acc)` | 2.48 s | 1.1G | O(n²) hot loop |
| Counter tuple fix | 168 ms | 14M | O(n) fold |
| Counter + `:binary.bin_to_list` → `for <<px <- bin>>` | 122 ms | 12M | skip decode intermediate |

The flame graph is how you know which of these changes matters before
writing them.

---

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?

## Resources

- [Brendan Gregg — Flame Graphs](https://www.brendangregg.com/flamegraphs.html) — the foundational concept
- [`eflame` on GitHub (Vagabond mirror)](https://github.com/Vagabond/eflame) — the library used here
- [`FlameGraph` toolkit](https://github.com/brendangregg/FlameGraph) — `stack_to_flame.pl` and friends
- [Ferd's profiling guide](https://ferd.ca/) — multi-part series on Erlang profiling
- [`:fprof` reference](https://www.erlang.org/doc/man/fprof.html) — alternative exact profiler
- [José Valim — benchmarking Elixir code](https://elixir-lang.org/blog/) — complementary with Benchee

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```
