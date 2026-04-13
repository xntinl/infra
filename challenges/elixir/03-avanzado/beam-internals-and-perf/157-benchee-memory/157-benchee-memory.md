# Benchee Memory and Reductions Formatter

**Project**: `benchee_memory` — extend Benchee with `memory_time:` and `reduction_time:` to measure allocated bytes and reductions per iteration, not just wall time.

---

## Project context

Wall time is the obvious benchmark metric. It is also the one the BEAM
scheduler is least able to give you honestly: on a shared machine with
other BEAM apps, cache warmth, preemptive yielding, GC interference and
thermal throttling all shift wall time by ±10%. Two benchmarks that
report identical µs can behave entirely differently under real production
load — one allocates 40 KB per call and GC-thrashes at high throughput,
the other allocates 120 bytes and barely registers.

Benchee's `memory_time:` option enables a separate pass that measures
**total allocated bytes** per iteration using `:erlang.process_info/2`
counters. `reduction_time:` measures BEAM reductions — the VM's
internal cost unit, used for scheduler preemption. Reductions are the
single most portable "how much work did this do?" metric across
hardware, OTP versions, and system load.

In this exercise you build `BencheeMemory`, a suite comparing three
implementations of a string-building function: `<>` concatenation,
`IO.iodata_to_binary/1`, and a pre-allocated `Enum.reduce/3` with binary
accumulator. The wall-time differences are small; the allocation and
reduction differences are dramatic — and they are what matters at
1M req/day.

```
benchee_memory/
├── lib/
│   └── benchee_memory/
│       ├── builders.ex
│       └── reporter.ex         # custom formatter example
├── bench/
│   └── builders_bench.exs
├── test/
│   └── benchee_memory/
│       └── builders_test.exs
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

### 1. What "memory" means in Benchee

Benchee's memory measurement is:

```
GC.run()  # before
m0 = :erlang.process_info(pid, :total_heap_size) * word_size
run function
m1 = :erlang.process_info(pid, :total_heap_size) * word_size
allocated = m1 - m0
```

It reports **net heap growth during the call**. This includes:

- Newly allocated terms that survived short-lived scope
- Refc binary metadata on the process heap (each binary > 64 bytes leaves a 24-byte refc record)

It excludes:

- Off-heap refc binary *payload*
- Terms that were allocated and immediately garbage-collected

So a function that allocates a 100 KB binary and returns it will show
~24 bytes of "memory" — the refc record. The 100 KB lives in the binary
allocator. This is by design: Benchee asks "what stays on my heap
after this call?" not "what was allocated in total?".

### 2. Reductions — the portable cost metric

A **reduction** is the BEAM's abstract work unit. Every function call
costs reductions. Every BIF costs reductions proportional to its size.
A scheduler preempts a process after ~4000 reductions.

```elixir
{_, r0} = Process.info(self(), :reductions)
# ...work...
{_, r1} = Process.info(self(), :reductions)
reductions_used = r1 - r0
```

Two invariants make reductions useful for benchmarking:

- **Deterministic given the same Elixir source.** Same code on the same
  OTP version always uses the same reductions.
- **Hardware-independent.** A reduction on M2 costs the same reductions
  on a Raspberry Pi (the wall time differs, the count doesn't).

This means reduction benchmarks reproduce perfectly across your dev
laptop, CI, and production — unlike wall time.

### 3. Why `memory_time` runs a separate pass

Measuring memory requires `:erlang.garbage_collect/1` before and after
each sample to get a stable reading. GC is expensive (milliseconds for
a hot process), so running it inside the wall-time pass would destroy
the wall-time measurement. Benchee runs two passes:

1. Wall-time pass — no GC interference, fast iterations.
2. Memory pass — GC before each sample, slower iterations, accurate
   bytes.

You configure both independently: `time: 5, memory_time: 3`.

### 4. Benchee's built-in formatters

- `Benchee.Formatters.Console` — ASCII table, default
- `Benchee.Formatters.HTML` — static site with charts (separate dep)
- `Benchee.Formatters.Markdown` — commit the report next to the PR

A custom formatter is a module with `format/2` and `write/2` callbacks.
The exercise includes one that emits CSV rows suitable for tracking
regressions in a dashboard.

### 5. Binary concat — three shapes, three cost profiles

```elixir
# Shape A: iterative concatenation
Enum.reduce(list, "", fn s, acc -> acc <> s end)
# Problem: each <> allocates a new binary. For N items: O(N²) bytes allocated.

# Shape B: iodata, flatten at the end
Enum.reduce(list, [], fn s, acc -> [acc | s] end)
|> IO.iodata_to_binary()
# One final allocation. O(N) bytes.

# Shape C: binary accumulator (compiler optimization)
Enum.reduce(list, <<>>, fn s, acc -> <<acc::binary, s::binary>> end)
# Compiler sees `acc::binary` in head position — optimizes to in-place append.
# Note: relies on the accumulator being the first segment.
```

Wall time: A loses by 5-10x on N=100. Memory: A loses by 100x. Reductions:
A loses by ~20x. The same code change improves three metrics at once.

### 6. Reading allocation per iteration vs. total

`memory_time` reports "Memory usage" as the average per iteration. In
the report you also see "deviation" — under heavy GC interference,
deviation can be > 50%, signaling non-determinism. Re-run with a longer
`memory_time` until deviation stabilizes.

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

**Objective**: Pin :benchee so memory_time and reduction_time passes measure allocation pressure and VM work per iteration.

```elixir
defmodule BencheeMemory.MixProject do
  use Mix.Project

  def project, do: [app: :benchee_memory, version: "0.1.0", elixir: "~> 1.15", deps: deps()]

  def application, do: [extra_applications: [:logger]]

  defp deps, do: [{:benchee, "~> 1.3"}]
end
```

### Step 2: `lib/benchee_memory/builders.ex`

**Objective**: Implement concat O(N²), iodata O(N), and binary-append compiler-optimized variants so memory/reductions deltas surface.

```elixir
defmodule BencheeMemory.Builders do
  @moduledoc """
  Three implementations of "build a binary from a list of binaries".
  Same external contract; very different allocation profiles.
  """

  @doc "Concatenation with `<>`. O(N²) allocations."
  @spec concat([binary()]) :: binary()
  def concat(list) do
    Enum.reduce(list, "", fn s, acc -> acc <> s end)
  end

  @doc "iolist then `IO.iodata_to_binary/1`. One terminal allocation."
  @spec iodata([binary()]) :: binary()
  def iodata(list) do
    list
    |> Enum.reduce([], fn s, acc -> [acc, s] end)
    |> IO.iodata_to_binary()
  end

  @doc """
  Binary accumulator — triggers the compiler's binary-append optimization
  when the accumulator appears first in the bitstring head.
  """
  @spec binary_append([binary()]) :: binary()
  def binary_append(list) do
    Enum.reduce(list, <<>>, fn s, acc -> <<acc::binary, s::binary>> end)
  end
end
```

### Step 3: `lib/benchee_memory/reporter.ex`

**Objective**: Write custom CSV formatter capturing scenario name, average wall-time, memory, and reductions for regression tracking.

A minimal custom formatter that appends a CSV row per scenario for
regression tracking.

```elixir
defmodule BencheeMemory.Reporter do
  @moduledoc """
  Custom Benchee formatter: emits one CSV row per scenario with
  name, avg wall time (ns), avg memory (bytes), avg reductions.

  Use by placing the formatter in `formatters:` of the Benchee config:

      formatters: [
        Benchee.Formatters.Console,
        {BencheeMemory.Reporter, file: "bench_history.csv"}
      ]
  """

  @behaviour Benchee.Formatter

  @impl true
  def format(suite, opts) do
    file = Keyword.fetch!(opts, :file)
    header? = !File.exists?(file)

    rows =
      for %{name: name, run_time_data: rt, memory_usage_data: mem, reductions_data: red} <-
            suite.scenarios do
        [
          format_timestamp(),
          name,
          rt.statistics.average |> round(),
          if(mem, do: mem.statistics.average |> round(), else: ""),
          if(red, do: red.statistics.average |> round(), else: "")
        ]
        |> Enum.map(&to_string/1)
        |> Enum.join(",")
      end

    header =
      if header? do
        ["timestamp,name,wall_ns,memory_bytes,reductions"]
      else
        []
      end

    {Enum.join(header ++ rows, "\n") <> "\n", file}
  end

  @impl true
  def write({csv, file}, _opts) do
    File.write!(file, csv, [:append])
  end

  defp format_timestamp do
    DateTime.utc_now() |> DateTime.to_iso8601()
  end
end
```

### Step 4: `bench/builders_bench.exs`

**Objective**: Measure wall-time, memory, and reductions across N=100 chunks so O(N²) concat penalty surfaces in all three dimensions.

```elixir
alias BencheeMemory.Builders

list = for i <- 1..100, do: "chunk-#{i}-"

Benchee.run(
  %{
    "concat (<>)" => fn -> Builders.concat(list) end,
    "iodata" => fn -> Builders.iodata(list) end,
    "binary_append" => fn -> Builders.binary_append(list) end
  },
  time: 4,
  warmup: 2,
  memory_time: 3,
  reduction_time: 2,
  formatters: [
    {Benchee.Formatters.Console, extended_statistics: true},
    {BencheeMemory.Reporter, file: "priv/bench_history.csv"}
  ]
)
```

### Step 5: `test/benchee_memory/builders_test.exs`

**Objective**: Verify all three builders output identical binary and measure reductions delta so concat O(N²) cost quantifies.

```elixir
defmodule BencheeMemory.BuildersTest do
  use ExUnit.Case, async: true

  alias BencheeMemory.Builders

  @input for i <- 1..50, do: "s#{i}"

  describe "correctness — all three produce the same binary" do
    test "concat" do
      assert Builders.concat(@input) == Enum.join(@input)
    end

    test "iodata" do
      assert Builders.iodata(@input) == Enum.join(@input)
    end

    test "binary_append" do
      assert Builders.binary_append(@input) == Enum.join(@input)
    end
  end

  describe "allocation profile — reductions as proxy" do
    defp measure(fun) do
      :erlang.garbage_collect()
      {_, r0} = Process.info(self(), :reductions)
      _ = fun.()
      {_, r1} = Process.info(self(), :reductions)
      r1 - r0
    end

    test "concat uses more reductions than iodata for N=200" do
      input = for i <- 1..200, do: "chunk-#{i}-"

      r_concat = measure(fn -> Builders.concat(input) end)
      r_iodata = measure(fn -> Builders.iodata(input) end)

      assert r_concat > r_iodata * 2,
             "expected concat to be > 2x iodata reductions, got #{r_concat} vs #{r_iodata}"
    end

    test "binary_append is comparable to iodata for small N" do
      input = for i <- 1..50, do: "s"

      r_iodata = measure(fn -> Builders.iodata(input) end)
      r_append = measure(fn -> Builders.binary_append(input) end)

      # Both should be within 3x of each other (binary append can be slightly more
      # expensive due to allocation strategy)
      ratio = max(r_iodata, r_append) / max(min(r_iodata, r_append), 1)
      assert ratio < 3.0
    end
  end
end
```

### Step 6: Run and read the report

**Objective**: Execute benchmark with memory/reduction passes and CSV export, then compare wall-time, memory, and reductions across all three.

```bash
mkdir -p priv
mix run bench/builders_bench.exs
```

Expected console output on an M2 with N=100 input strings:

```
Name                ips        average  deviation         median       99th %
binary_append     320 K        3.1 µs    ±12.00%        3.0 µs        4.8 µs
iodata            295 K        3.4 µs    ±15.00%        3.2 µs        5.2 µs
concat             42 K       23.9 µs    ±35.00%       22.1 µs       68.3 µs

Comparison:
binary_append     320 K
iodata            295 K      1.08x slower
concat             42 K      7.61x slower

Memory usage statistics:
binary_append   ~12 KB  memory usage
iodata          ~13 KB  memory usage
concat         ~850 KB  memory usage

Memory Comparison:
binary_append   ~12 KB
iodata          ~13 KB  1.08x memory usage
concat         ~850 KB  70x memory usage
```

---


### Why this works

The design leans on BEAM guarantees (process isolation, mailbox ordering, supervisor restarts) and pushes invariants to the boundaries of each module. State transitions are explicit, failure modes are declared rather than implicit, and each step is independently testable. That combination keeps the implementation correct under concurrent load and cheap to change later.

## Benchmark

```elixir
# Minimal measurement — replace with Benchee for distribution stats.
{time_us, _} = :timer.tc(fn ->
  for _ <- 1..10_000, do: run_operation()
end)
IO.puts("avg: #{time_us / 10_000} µs/op")
```

Target: operation should complete in the low-microsecond range on modern hardware; deviations by >2× indicate a regression worth investigating.

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

**1. `memory_time` reports heap, not binary allocator.** A function that
returns a 10 MB refc binary reports ~24 bytes on-heap and misses the real
allocation. To measure binary allocator pressure, sample
`:erlang.memory(:binary)` before and after manually.

**2. Reductions are not wall time.** A BIF that takes 10 µs might cost 5
reductions; an Elixir function that takes 1 µs might cost 20. Reductions
measure work-the-VM-thinks-you-did, not actual CPU. Use both metrics side
by side.

**3. `:erlang.garbage_collect/1` before memory samples.** If you don't GC,
the "before" heap includes whatever survived the previous benchmark
iteration. Benchee does this for you; if you hand-roll a measurement,
remember it.

**4. Warmup matters for reductions too.** OTP 26 JIT compilation changes
reduction counts subtly for the first few thousand calls. `warmup: 2`
minimum.

**5. `deviation` > 25% means the sample is unreliable.** Extend `memory_time`
or ensure no other work is running on the node (background supervisors,
log flushing). Run `:observer.start()` and check for activity during
the benchmark.

**6. Process dictionary reads cost memory in the report.** `Process.put/2`
and `Process.get/1` allocate and re-allocate the dict. Benchmarks that
use the process dict show "baseline" memory above zero.

**7. Custom formatters run after every run.** If you run the benchmark
under CI on every PR, your CSV grows unboundedly. Rotate it monthly or
commit to git-LFS.

**8. When NOT to use this.** For tiny functions (single-digit ns), Benchee's
measurement overhead dominates. Use `:timer.tc/1` in a tight loop with
`:erlang.statistics(:reductions)` direct sampling, or move to the lower
level `:fprof`.

---

## Reductions as a regression signal

The `reductions` metric is under-appreciated. A useful CI practice:
on every PR, run the benchmark, append the `reductions` column to a
CSV, diff against the previous commit. Any >10% regression blocks the
PR. This catches performance drops that wall-time does not — wall-time
noise is too high in CI to make a reliable regression signal.

Example GitHub Actions step:

```yaml
- name: Benchmark regressions
  run: |
    mix run bench/builders_bench.exs
    ruby scripts/check_regressions.rb priv/bench_history.csv --threshold 10
```

---

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?

## Resources

- [Benchee memory measurement docs](https://hexdocs.pm/benchee/writing-benchmarks.html#measuring-memory-consumption) — the specific section on memory
- [`:erlang.process_info/2`](https://www.erlang.org/doc/man/erlang.html#process_info-2) — the counters Benchee reads
- [`:erlang.statistics/1`](https://www.erlang.org/doc/man/erlang.html#statistics-1) — reduction counters at the VM level
- [José Valim — iolist vs binary](https://elixir-lang.org/blog/) — the canonical write-up on binary concatenation cost
- [Chris Keathley — benchmarking in Elixir](https://keathley.io/) — practical benchmark-driven optimization
- [Dashbit — memory and reductions in production](https://dashbit.co/blog/) — real-world stories of reduction-based alerting

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```
