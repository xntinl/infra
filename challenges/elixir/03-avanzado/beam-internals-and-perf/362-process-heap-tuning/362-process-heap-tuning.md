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

## Trade-offs and production gotchas

**1. Memory grows linearly with worker count.** 100k workers at 16k words each = 128 GB. Pre-sizing is NOT free.

**2. High-water-mark varies.** If 95th percentile is 4k words but 99th is 20k words, sizing to 4k regresses the slow tail. Measure the distribution, not the average.

**3. Wrong `min_bin_vheap_size` can cause OOM.** A process that legitimately holds 100 MB of binary refs but has vheap set to 16 KB will GC constantly. Underbake here and throughput collapses.

**4. GenServer workers from `spawn_link` inherit defaults.** `GenServer.start_link/3` does not expose `min_heap_size`. Use `:proc_lib.start_link/3` or pass via `spawn_opt` in a custom start function.

**5. `fullsweep_after` is unrelated.** It controls old-gen sweep frequency. Tuning it to `0` (always full-sweep) hurts; `:infinity` is rarely safe either.

**6. When NOT to tune.** Apps where most processes are long-lived (a GenServer per user session that lives hours): GC has time to stabilize, tuning yields < 1% gain.

## Reflection

You reduce GC count by 80% on a worker but overall throughput improves only 3%. Where is the CPU time actually going? List three hypotheses and the measurement you would use for each.

## Resources

- [`spawn_opt/2` — erlang.org](https://www.erlang.org/doc/man/erlang.html#spawn_opt-2)
- [Process heap internals — The BEAM Book](https://blog.stenmans.org/theBeamBook/)
- [Erlang in Anger — Memory chapter](https://www.erlang-in-anger.com/)
- [Tuning the Erlang VM — Lukas Larsson](https://www.erlang-solutions.com/blog/erlang-19-0-garbage-collector/)
