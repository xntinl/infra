# Process Heap Tuning: `:min_heap_size` and `:min_bin_vheap_size`

**Project**: `heap_lab` — experiments that measure allocation-induced GC pressure on short-lived worker processes and demonstrate how `:min_heap_size` and `:min_bin_vheap_size` remove the pressure.

## Project context

Your API spawns a short-lived GenServer per request to isolate per-request state. You see 100k GCs per second in `observer`, but memory usage is stable. The GCs are "young generation" collections on newly-spawned processes that immediately allocate a large map. Each process is born with a tiny heap (233 words), grows through 3-4 collections in the first 5ms, then dies. The GC work is wasted: the heap reaches its final size every time.

Tuning `:min_heap_size` pre-allocates a larger heap on spawn, skipping the initial grow cycles. For binary-heavy processes, `:min_bin_vheap_size` does the same for the binary virtual heap.

```
heap_lab/
├── lib/
│   └── heap_lab/
│       ├── worker.ex
│       └── bench_helpers.ex
├── test/
│   └── heap_lab/
│       └── worker_test.exs
├── bench/
│   └── heap_size_bench.exs
└── mix.exs
```

## Why tune heap sizes and not just let GC handle it

GC is fast but not free. The young generation is collected with a generational copying collector: all live data is copied to the next heap. A process that dies with 50 KB of live data will, on the default 233-word spawn heap, trigger 4-5 growth phases, each copying everything alive. That is 4-5x the minimum required copying.

A pre-sized heap starts at (say) 8192 words. If the process never grows past 6000 words, it dies with zero GCs. Pure win for short-lived workers with predictable size.

**Why not `fullsweep_after`?** That tuns the MAJOR GC frequency — different knob, different problem.

## Core concepts



---

**Why this matters:**
These concepts form the foundation of production Elixir systems. Understanding them deeply allows you to build fault-tolerant, scalable applications that operate correctly under load and failure.

**Real-world use case:**
This pattern appears in systems like:
- Phoenix applications handling thousands of concurrent connections
- Distributed data processing pipelines
- Financial transaction systems requiring consistency and fault tolerance
- Microservices communicating over unreliable networks

**Common pitfall:**
Many developers overlook that Elixir's concurrency model differs fundamentally from threads. Processes are isolated; shared mutable state does not exist. Trying to force shared-memory patterns leads to deadlocks, race conditions, or silently incorrect behavior. Always think in terms of message passing and immutability.
### 1. Two heaps per process

- **Regular heap**: holds tuples, lists, maps, numbers. Measured in words (machine words, 8 bytes on 64-bit).
- **Binary virtual heap**: accounting for refc (reference-counted) binaries. Not a real heap — it tracks how much binary memory this process "owes" to the shared refc pool. When vheap crosses threshold, GC runs to release unused binary refs.

### 2. `spawn_opt` knobs

```elixir
spawn_opt(fun,
  min_heap_size: 8192,
  min_bin_vheap_size: 46368,
  fullsweep_after: 20
)
```

- `min_heap_size`: initial and minimum heap in words. Default 233.
- `min_bin_vheap_size`: initial binary vheap in words. Default 46368 (about 370 KB).
- `fullsweep_after`: number of minor GCs before a major sweep. Default 65535.

### 3. Process-wide default via `+h`

VM flag `+h 8192` sets the default for ALL new processes. Use only if the shape of your workload dominates — otherwise per-spawn is better.

### 4. Overhead of oversizing

A heap size of 1M words wastes 8 MB per process. With 100k processes, that is 800 GB. Sizing must match the actual high-water mark, not the worst case.

## Design decisions

- **Option A — tune nothing, let GC run**: fine for most apps. Measure first.
- **Option B — per-spawn tuning for hot workers**: targeted, avoids wasting memory on idle processes.
- **Option C — VM-wide `+h`**: blunt instrument, useful when 95% of processes are hot workers.

Chosen: Option B. Measure the 99th-percentile heap high-water of the worker, set `min_heap_size` to 2x that value, and verify GCs drop.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule HeapLab.MixProject do
  use Mix.Project
  def project, do: [app: :heap_lab, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  def application, do: [extra_applications: [:logger]]
  defp deps, do: [{:benchee, "~> 1.3", only: :dev}]
end
```

### Step 1: Worker — `lib/heap_lab/worker.ex`

**Objective**: Allocate representative workload map and list, then measure spawn_opt'd process GC counts and final heap size.

```elixir
defmodule HeapLab.Worker do
  @moduledoc """
  A representative short-lived worker. Allocates a map and a list
  sized to simulate a "handle one request" workload, then exits.
  """

  @default_spawn_opts []

  def run(items, opts \\ []) do
    spawn_opts = Keyword.merge(@default_spawn_opts, Keyword.get(opts, :spawn_opts, []))
    parent = self()

    pid =
      :erlang.spawn_opt(
        fn ->
          work(items)
          {:garbage_collection, gcs} = Process.info(self(), :garbage_collection)
          {:total_heap_size, heap} = Process.info(self(), :total_heap_size)
          send(parent, {:stats, Keyword.fetch!(gcs, :minor_gcs), heap})
        end,
        spawn_opts
      )

    receive do
      {:stats, gcs, heap} -> %{pid: pid, gcs: gcs, heap: heap}
    after
      5_000 -> raise "worker timed out"
    end
  end

  defp work(items) do
    map =
      for i <- 1..items, into: %{} do
        {i, {"key_#{i}", i * 2, [i, i + 1, i + 2]}}
      end

    _ = Map.values(map) |> Enum.sum_by(fn {_, v, _} -> v end)
    :ok
  end
end
```

### Step 2: Helpers — `lib/heap_lab/bench_helpers.ex`

**Objective**: Aggregate GC counts and heap size deltas across N spawned workers to measure min_heap_size impact quantitatively.

```elixir
defmodule HeapLab.BenchHelpers do
  def run_n(n, items, opts) do
    results = for _ <- 1..n, do: HeapLab.Worker.run(items, opts)

    %{
      avg_gcs: avg(results, :gcs),
      avg_heap_words: avg(results, :heap)
    }
  end

  defp avg(results, key) do
    total = Enum.reduce(results, 0, &(Map.fetch!(&1, key) + &2))
    total / length(results)
  end
end
```

## Why this works

`min_heap_size: 8192` allocates 8192 words (64 KB) on spawn. A worker that tops out at 6000 words never triggers a grow cycle. The cost is paid up-front instead of amortized across GCs, but total CPU time drops because the "grow + copy-live" sequence is skipped entirely.

For binary-heavy processes, `min_bin_vheap_size` prevents premature refc releases. A process reading a 100 MB file via `File.stream!` touches many binary refs; without vheap headroom, it GCs itself into the ground.

## Tests — `test/heap_lab/worker_test.exs`

```elixir
defmodule HeapLab.WorkerTest do
  use ExUnit.Case, async: true

  describe "baseline vs tuned heap" do
    test "tuned heap eliminates minor GCs" do
      baseline = HeapLab.Worker.run(500, spawn_opts: [])
      tuned = HeapLab.Worker.run(500, spawn_opts: [min_heap_size: 16_384])

      assert baseline.gcs > 0
      assert tuned.gcs == 0
    end

    test "over-sized heap is observable in total_heap_size" do
      small = HeapLab.Worker.run(10, spawn_opts: [])
      huge = HeapLab.Worker.run(10, spawn_opts: [min_heap_size: 65_536])

      assert huge.heap > small.heap * 10
    end
  end

  describe "binary vheap" do
    test "spawn_opt accepts min_bin_vheap_size" do
      %{pid: _} = HeapLab.Worker.run(10, spawn_opts: [min_bin_vheap_size: 92_736])
    end
  end
end
```

## Benchmark — `bench/heap_size_bench.exs`

```elixir
Benchee.run(
  %{
    "default heap"       => fn -> HeapLab.Worker.run(500, spawn_opts: []) end,
    "min_heap_size 4k"   => fn -> HeapLab.Worker.run(500, spawn_opts: [min_heap_size: 4_096]) end,
    "min_heap_size 16k"  => fn -> HeapLab.Worker.run(500, spawn_opts: [min_heap_size: 16_384]) end,
    "min_heap_size 64k"  => fn -> HeapLab.Worker.run(500, spawn_opts: [min_heap_size: 65_536]) end
  },
  time: 3,
  warmup: 1
)
```

**Expected**: default runs ~30% slower than min_heap_size: 16384 for this workload. min_heap_size: 65536 is slower than 16384 because of the wasted allocation.

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

**1. Memory grows linearly with worker count.** 100k workers at 16k words each = 128 GB. Pre-sizing is NOT free.

**2. High-water-mark varies.** If 95th percentile is 4k words but 99th is 20k words, sizing to 4k regresses the slow tail. Measure the distribution, not the average.

**3. Wrong `min_bin_vheap_size` can cause OOM.** A process that legitimately holds 100 MB of binary refs but has vheap set to 16 KB will GC constantly. Underbake here and throughput collapses.

**4. GenServer workers from `spawn_link` inherit defaults.** `GenServer.start_link/3` does not expose `min_heap_size`. Use `:proc_lib.start_link/3` or pass via `spawn_opt` in a custom start function.

**5. `fullsweep_after` is unrelated.** It controls old-gen sweep frequency. Tuning it to `0` (always full-sweep) hurts; `:infinity` is rarely safe either.

**6. When NOT to tune.** Apps where most processes are long-lived (a GenServer per user session that lives hours): GC has time to stabilize, tuning yields < 1% gain.

## Reflection

You reduce GC count by 80% on a worker but overall throughput improves only 3%. Where is the CPU time actually going? List three hypotheses and the measurement you would use for each.


## Executable Example

```elixir
defmodule HeapLab.Worker do
  @moduledoc """
  A representative short-lived worker. Allocates a map and a list
  sized to simulate a "handle one request" workload, then exits.
  """

  @default_spawn_opts []

  def run(items, opts \\ []) do
    spawn_opts = Keyword.merge(@default_spawn_opts, Keyword.get(opts, :spawn_opts, []))
    parent = self()

    pid =
      :erlang.spawn_opt(
        fn ->
          work(items)
          {:garbage_collection, gcs} = Process.info(self(), :garbage_collection)
          {:total_heap_size, heap} = Process.info(self(), :total_heap_size)
          send(parent, {:stats, Keyword.fetch!(gcs, :minor_gcs), heap})
        end,
        spawn_opts
      )

    receive do
      {:stats, gcs, heap} -> %{pid: pid, gcs: gcs, heap: heap}
    after
      5_000 -> raise "worker timed out"
    end
  end

  defp work(items) do
    map =
      for i <- 1..items, into: %{} do
        {i, {"key_#{i}", i * 2, [i, i + 1, i + 2]}}
      end

    _ = Map.values(map) |> Enum.sum_by(fn {_, v, _} -> v end)
    :ok
  end
end

defmodule Main do
  def main do
      IO.puts("Benchmarking initialized")
      {elapsed_us, result} = :timer.tc(fn ->
        Enum.reduce(1..1000, 0, &+/2)
      end)
      if is_number(elapsed_us) do
        IO.puts("✓ Benchmark completed: sum(1..1000) = " <> inspect(result) <> " in " <> inspect(elapsed_us) <> "µs")
      end
  end
end

Main.main()
```
