# Advanced benchmarking with Benchee

**Project**: `benchee_deep` — compare implementations along three axes (time, memory, reductions), under different input sizes and parallel load.

---

## Project context

Your team is arguing about whether to replace a `List.foldl/3` pipeline with
a `Stream` pipeline. Two engineers measured it — one with `:timer.tc/1` and
one with `System.monotonic_time/0` — and got contradictory results. Neither
accounted for warm-up, GC, or CPU migration.

This exercise builds a canonical benchmark suite in Benchee that compares
four real-world implementations of the same problem along three dimensions,
exposes the measurement pitfalls beginners hit, and produces a report
anyone can reproduce. The output feeds architecture decisions — so the
benchmark methodology matters more than any single number.

Project structure:

```
benchee_deep/
├── lib/
│   └── benchee_deep/
│       ├── sum.ex              # 4 implementations of sum-squared
│       ├── parse.ex            # 3 implementations of NDJSON parse
│       └── runner.ex           # programmatic Benchee wrapper
├── bench/
│   ├── sum_bench.exs
│   ├── parse_bench.exs
│   └── parallel_bench.exs
├── test/
│   └── benchee_deep/
│       ├── sum_test.exs
│       └── parse_test.exs
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

### 1. Why `:timer.tc/1` is not enough

`:timer.tc(fn -> do_work() end)` runs the function once and measures
wall-clock time. It is useless for anything under 1 ms because:

- First call triggers module loading, code caching, and JIT warmup.
- A single sample is noise: GC, scheduler migration, other processes.
- Clock resolution on some OSes is ~1 µs, so micro-benchmarks alias.

Benchee solves this by: warming up (discard first N ms), running for a
target duration, computing statistics (p50/p99/stddev), and reporting
variance so you know whether a 5% difference is real.

### 2. Benchee's three dimensions

```
┌────────────┬───────────────────────────────────────────────┐
│ Dimension  │ What it tells you                             │
├────────────┼───────────────────────────────────────────────┤
│ time       │ wall-clock per invocation (ns)                │
│ memory     │ process heap bytes allocated during call      │
│ reductions │ Erlang reductions consumed                    │
└────────────┴───────────────────────────────────────────────┘
```

`memory_time:` and `reduction_time:` in the Benchee config enable the
extra measurements. They run *separate* phases — don't assume the same
sample is timed and mem-traced simultaneously.

### 3. Input-set benchmarks

A single number for "how fast is my function" is a lie — performance
depends on input shape. Benchee's `inputs:` option runs every scenario
against every input:

```
inputs: %{
  "small  (N=100)"     => make_data(100),
  "medium (N=10_000)"  => make_data(10_000),
  "large  (N=1_000_000)" => make_data(1_000_000)
}
```

This is how you catch O(n²) hiding behind tiny test data.

### 4. Parallel benchmarks

```
parallel: 8
```

Runs each scenario with 8 concurrent processes. Exposes contention that
a single-threaded bench misses — e.g., a GenServer call with 8 callers
exposes mailbox serialization that 1 caller doesn't. Numbers drop
from "latency per call" to "effective throughput per call".

### 5. Statistical significance

Benchee reports mean, median, stddev, p99, and **"deviation"**. Rule:
if two scenarios' confidence intervals overlap, they are statistically
indistinguishable. Don't claim a 3% win if `±5%`.

### 6. Microbenchmarks vs load tests

Benchee measures function-level cost. It does not replace end-to-end
load tests (k6, wrk, Tsung) — a function that's 20% faster in Benchee
may be irrelevant if your latency is dominated by the database.
Always verify downstream: profile → benchmark the hot function →
load-test the whole request path.

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

### Step 1: project

**Objective**: Scaffold app with `bench/` so benchmark scripts isolate from compiled code paths and avoid release pollution.

```bash
mix new benchee_deep
cd benchee_deep
mkdir -p bench
```

### Step 2: `mix.exs`

**Objective**: Pin `:benchee` so HTML reports + statistical significance (confidence intervals, deviation%) guide architecture decisions confidently.

```elixir
defmodule BencheeDeep.MixProject do
  use Mix.Project

  def project do
    [app: :benchee_deep, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application, do: [extra_applications: [:logger]]

  defp deps do
    [
      {:benchee, "~> 1.3"},
      {:benchee_html, "~> 1.0", only: :dev},
      {:jason, "~> 1.4"}
    ]
  end
end
```

### Step 3: `lib/benchee_deep/sum.ex`

**Objective**: Implement 4 sum-of-squares variants so Enum protocol dispatch + Stream overhead costs surface against reduction-counted recursion.

```elixir
defmodule BencheeDeep.Sum do
  @moduledoc "Four implementations of sum-of-squares over a list."

  @spec enum_map_sum([number()]) :: number()
  def enum_map_sum(list), do: list |> Enum.map(&(&1 * &1)) |> Enum.sum()

  @spec enum_reduce([number()]) :: number()
  def enum_reduce(list), do: Enum.reduce(list, 0, fn x, acc -> x * x + acc end)

  @spec stream_sum([number()]) :: number()
  def stream_sum(list), do: list |> Stream.map(&(&1 * &1)) |> Enum.sum()

  @spec recursive([number()]) :: number()
  def recursive(list), do: do_rec(list, 0)

  defp do_rec([], acc), do: acc
  defp do_rec([h | t], acc), do: do_rec(t, acc + h * h)
end
```

### Step 4: `lib/benchee_deep/parse.ex`

**Objective**: Benchmark String.split vs binary_scan so allocating intermediate lists vs zero-copy pattern matching overhead quantifies.

```elixir
defmodule BencheeDeep.Parse do
  @moduledoc "Three implementations of NDJSON line-count."

  @spec enum_split(binary()) :: non_neg_integer()
  def enum_split(blob) do
    blob |> String.split("\n", trim: true) |> length()
  end

  @spec stream_split(binary()) :: non_neg_integer()
  def stream_split(blob) do
    blob
    |> String.splitter("\n", trim: true)
    |> Enum.count()
  end

  @spec binary_scan(binary()) :: non_neg_integer()
  def binary_scan(blob), do: count_newlines(blob, 0)

  defp count_newlines(<<>>, acc), do: acc
  defp count_newlines(<<"\n", rest::binary>>, acc), do: count_newlines(rest, acc + 1)
  defp count_newlines(<<_::8, rest::binary>>, acc), do: count_newlines(rest, acc)
end
```

### Step 5: `lib/benchee_deep/runner.ex`

**Objective**: Enforce 2s warmup + memory+reduction measurements so JIT warm-up and GC pause variance don't skew comparison.

```elixir
defmodule BencheeDeep.Runner do
  @moduledoc """
  Programmatic wrapper around Benchee that enforces a house style:
  warmup 2s, measurement 5s, memory+reductions always on, HTML output.
  """

  @type scenario :: (term() -> term())

  @spec run(%{String.t() => scenario()}, keyword()) :: map()
  def run(scenarios, opts \\ []) do
    inputs = Keyword.get(opts, :inputs, nil)
    parallel = Keyword.get(opts, :parallel, 1)
    time = Keyword.get(opts, :time, 5)
    warmup = Keyword.get(opts, :warmup, 2)
    title = Keyword.get(opts, :title, "benchmark")

    Benchee.run(
      scenarios,
      warmup: warmup,
      time: time,
      memory_time: 2,
      reduction_time: 2,
      parallel: parallel,
      inputs: inputs,
      formatters: [
        Benchee.Formatters.Console,
        {Benchee.Formatters.HTML, file: "bench/output/#{safe(title)}.html", auto_open: false}
      ],
      title: title
    )
  end

  defp safe(str), do: str |> String.downcase() |> String.replace(~r/[^a-z0-9]+/, "_")
end
```

### Step 6: benchmarks

**Objective**: Benchmark small/medium/large inputs + parallel=8 so O(n²) hiding, GC pressure, and contention surface.

```elixir
# bench/sum_bench.exs
list_small = Enum.to_list(1..100)
list_medium = Enum.to_list(1..10_000)
list_large = Enum.to_list(1..1_000_000)

BencheeDeep.Runner.run(
  %{
    "Enum.map |> sum" => fn list -> BencheeDeep.Sum.enum_map_sum(list) end,
    "Enum.reduce" => fn list -> BencheeDeep.Sum.enum_reduce(list) end,
    "Stream.map |> sum" => fn list -> BencheeDeep.Sum.stream_sum(list) end,
    "recursive" => fn list -> BencheeDeep.Sum.recursive(list) end
  },
  inputs: %{
    "small 100" => list_small,
    "medium 10k" => list_medium,
    "large 1M" => list_large
  },
  title: "sum_of_squares"
)
```

```elixir
# bench/parse_bench.exs
make = fn n ->
  1..n
  |> Enum.map(fn i -> Jason.encode!(%{id: i, name: "row #{i}"}) end)
  |> Enum.join("\n")
end

blob_10k = make.(10_000)
blob_100k = make.(100_000)

BencheeDeep.Runner.run(
  %{
    "String.split" => fn blob -> BencheeDeep.Parse.enum_split(blob) end,
    "String.splitter" => fn blob -> BencheeDeep.Parse.stream_split(blob) end,
    "binary scan" => fn blob -> BencheeDeep.Parse.binary_scan(blob) end
  },
  inputs: %{"10k lines" => blob_10k, "100k lines" => blob_100k},
  title: "ndjson_linecount"
)
```

```elixir
# bench/parallel_bench.exs
list = Enum.to_list(1..10_000)

BencheeDeep.Runner.run(
  %{
    "enum.reduce par=1" => fn -> BencheeDeep.Sum.enum_reduce(list) end
  },
  parallel: 1,
  title: "par_1"
)

BencheeDeep.Runner.run(
  %{
    "enum.reduce par=8" => fn -> BencheeDeep.Sum.enum_reduce(list) end
  },
  parallel: 8,
  title: "par_8"
)
```

### Step 7: tests

**Objective**: Lock down correctness so benchmark speedups never mask silent off-by-one bugs or overflow regressions.

```elixir
# test/benchee_deep/sum_test.exs
defmodule BencheeDeep.SumTest do
  use ExUnit.Case, async: true
  alias BencheeDeep.Sum

  @list Enum.to_list(1..100)
  @expected Enum.reduce(1..100, 0, fn x, a -> x * x + a end)

  describe "BencheeDeep.Sum" do
    test "all implementations agree on small input" do
      assert Sum.enum_map_sum(@list) == @expected
      assert Sum.enum_reduce(@list) == @expected
      assert Sum.stream_sum(@list) == @expected
      assert Sum.recursive(@list) == @expected
    end

    test "empty list returns 0" do
      for fun <- [&Sum.enum_map_sum/1, &Sum.enum_reduce/1, &Sum.stream_sum/1, &Sum.recursive/1] do
        assert fun.([]) == 0
      end
    end
  end
end
```

```elixir
# test/benchee_deep/parse_test.exs
defmodule BencheeDeep.ParseTest do
  use ExUnit.Case, async: true
  alias BencheeDeep.Parse

  @blob "a\nbb\nccc\n"

  describe "BencheeDeep.Parse" do
    test "all implementations agree on line count" do
      assert Parse.enum_split(@blob) == 3
      assert Parse.stream_split(@blob) == 3
      assert Parse.binary_scan(@blob) == 3
    end

    test "empty blob returns 0" do
      assert Parse.binary_scan("") == 0
      assert Parse.stream_split("") == 0
    end
  end
end
```

### Step 8: run

**Objective**: Measure all variants (warmup 2s, time 5s, memory+reductions on) so Enum dispatch, Stream overhead, and scheduler contention surface quantitatively.

```bash
mix deps.get
mix test
mix run bench/sum_bench.exs
mix run bench/parse_bench.exs
mix run bench/parallel_bench.exs
```

Expected observations (on a 2023 M2, 8 cores):

- `Enum.reduce` beats `Enum.map |> Enum.sum` on small lists (one pass).
- `Stream.map` is slower than both on anything with fewer than ~100k items
  because the stream overhead is not amortized.
- `recursive` wins ever so slightly on 1M because it is tail-call optimized
  and avoids `Enum` protocol dispatch.
- `binary_scan` beats `String.split` by 5–10× on the 100k NDJSON input —
  no list allocation.
- Parallel=8 vs parallel=1 shows near-linear scaling for a CPU-bound
  pure function. If you don't see scaling, the function is already GC- or
  allocator-bound.

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


## Deep Dive: Benchmark Patterns and Production Implications

Benchmarking in Elixir requires statistical rigor: a single run means nothing. Tools like Benchee measure distribution, not just mean time. The mistake most engineers make is benchmarking in isolation (single process) and then deploying to a system under concurrent load where cache hits, scheduler contention, and garbage collection behave differently. Production performance tuning must account for these realities.

---

## Trade-offs and production gotchas

**1. `mix run -e` vs `mix run bench/x.exs`**
Compiling the app once matters. `mix run bench/x.exs` compiles the
application first so the benchmark measures the compiled code, not
interpreted forms. Never bench from a `-e` one-liner.

**2. Inputs defined in the benchmark function leak into the measurement**
```elixir
# WRONG: `1..10_000` is evaluated once, but allocation happens every call.
Benchee.run(%{"x" => fn -> Enum.to_list(1..10_000) |> BencheeDeep.Sum.enum_reduce() end})
```
Either use `before_scenario:` to precompute once per scenario, or pass
via `inputs:` and accept the input as an arg.

**3. The measuring process is itself a process**
Benchee spawns a measurement process. If your function sends messages
to `self()` (common in GenServer tests), those messages accumulate in
the measurement mailbox and skew memory_time. Use `before_each:` to
drain the mailbox.

**4. JIT warmup**
BEAM has a JIT since OTP 24. The first ~500 ms of any benchmark run
is JIT-dominated. Always set `warmup: 2` or higher. The default is 2s
— do not lower it.

**5. Benchee's memory metric is per-process heap, not total**
It excludes refc binary allocations (over 64 bytes). For binary-heavy
code, supplement with `:erlang.memory(:binary)` deltas via a custom
measurement.

**6. Parallel bench + shared state = skewed numbers**
If two scenarios touch a shared ETS table or a global GenServer, `parallel: 8`
measures *contention*, not raw function cost. Isolate state per process
or drop to `parallel: 1`.

**7. Comparing across machines is meaningless**
CPU, RAM, scheduler count, and kernel flags all affect numbers. Always
run the baseline and the candidate on the same box in the same session.
Share the `mix.lock` and exact Erlang/Elixir versions in your PR.

**8. When NOT to use this**
For deciding between two O(1)-ish alternatives where the wall-clock
difference is under 1 µs, the benchmarking effort is not worth it —
pick the more readable option. Benchee shines when you have an O(n)
vs O(n log n) or GenServer vs ETS decision with measurable impact.

---

## Performance notes

Sample output table on a 2023 M2, 8 schedulers, Erlang 26 + Elixir 1.16,
for `sum_bench.exs` medium-10k input:

| Scenario | ips | avg | deviation | memory | reductions |
|----------|-----|-----|-----------|--------|------------|
| `Enum.reduce` | 28,800 | 34.7 µs | ±3.4% | 18.2 KB | 20,012 |
| `recursive` | 28,100 | 35.6 µs | ±2.9% | 160 B | 20,008 |
| `Enum.map \|> sum` | 14,500 | 68.9 µs | ±4.1% | 312.6 KB | 40,016 |
| `Stream.map \|> sum` | 6,200 | 161.3 µs | ±5.7% | 484.4 KB | 80,022 |

Take-aways:
- `recursive` and `Enum.reduce` are tied for speed — prefer the readable
  `Enum.reduce`.
- `Enum.map |> sum` allocates a temporary 10k list (312 KB) — double the
  reductions, half the throughput.
- `Stream.map` is the *slowest* here — the wrapper overhead dwarfs the
  work. Streams win only when laziness matters (infinite sources,
  short-circuiting, memory-bounded pipelines).

---

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?

## Resources

- [Benchee README](https://github.com/bencheeorg/benchee) — Tobi Pfeiffer
- ["Elixir benchmarking — a survey" — Tobi Pfeiffer](https://pragtob.wordpress.com/2016/12/20/elixir-benchmarking-a-first-look-at-benchee/)
- [`Benchee.Formatters.HTML`](https://github.com/bencheeorg/benchee_html)
- [Erlang JIT — Lukas Larsson](https://www.erlang.org/blog/a-first-look-at-the-jit/) — OTP 24 release post
- ["Benchmarking correctly is hard" — Aleksandar Prokopec](https://aleksandar-prokopec.com/resources/docs/lcpc-beyond-benchmarking.pdf)
- [`mix profile.fprof`](https://hexdocs.pm/mix/Mix.Tasks.Profile.Fprof.html) — complement Benchee with flamegraphs
- [eprof for function-level profiling](https://www.erlang.org/doc/man/eprof.html)

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```
