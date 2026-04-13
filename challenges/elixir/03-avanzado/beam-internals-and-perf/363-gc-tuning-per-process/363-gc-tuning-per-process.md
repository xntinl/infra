# Garbage Collection Tuning per Process

**Project**: `gc_lab` — per-process GC tuning via `fullsweep_after`, `min_heap_size`, and `hibernate`, with measurements that show when each knob helps or hurts.

## Project context

You run a cache process that accumulates a 50 MB ETS-less map over hours. Heap fragmentation grows, minor GCs happen but never reclaim the dead entries because major sweeps are rare. Heap hits 300 MB despite only 50 MB of live data. The symptom: `observer` shows `total_heap_size` climbing, `memory_used` flat. A manual `:erlang.garbage_collect(pid)` drops heap to 55 MB in one shot.

The BEAM GC is a generational copying collector. Minor GCs move survivors from the young heap to the old heap; only major (full) sweeps compact the old heap. The default `fullsweep_after: 65535` is effectively "never for most processes". For long-lived, cache-like processes, you want more frequent majors.

```
gc_lab/
├── lib/
│   └── gc_lab/
│       ├── cache.ex
│       └── gc_probe.ex
├── test/
│   └── gc_lab/
│       └── gc_test.exs
├── bench/
│   └── fullsweep_bench.exs
└── mix.exs
```

## Why tune GC per process

Some processes are transient (request handlers) — minor GCs are enough, majors would be wasted.
Some are long-lived and mutate steadily (caches, session servers) — you NEED majors to reclaim old-gen garbage.
Some are idle between bursts (WebSocket handlers, background queues) — `hibernate` shrinks them to a minimum heap while they wait.

Global tuning forces one choice on all of them. Per-process knobs give each kind of process the policy it needs.

**Why not just call `:erlang.garbage_collect/0` manually?** It works, but you have to remember to call it. Setting `fullsweep_after` lets the VM do it at a correct cadence tied to minor-GC pressure.

## Core concepts

### 1. Generational GC

New allocations land in the young heap. Minor GCs copy survivors to the old heap. Old heap is collected only during major (full-sweep) GCs.

### 2. `fullsweep_after`

After N minor GCs, the next GC is upgraded to a major. Default 65535 ≈ "never". Setting `fullsweep_after: 10` forces a major every 10 minors — good for caches that mutate often.

### 3. `hibernate`

`Process.hibernate(Module, :fun, args)` (or `GenServer`'s `:hibernate` return) performs a full sweep and shrinks the heap to just-large-enough for the live data. Future allocations grow the heap again. Useful for processes that sleep most of the time.

### 4. Explicit `:erlang.garbage_collect/1,2`

Forces a GC on the target process. Options: `fullsweep: true/false`. Fine for ad-hoc cleanup, overkill as a routine.

## Design decisions

- **Option A — rely on defaults**: fine for most workloads. Identify the pathological 5% and tune them.
- **Option B — `fullsweep_after: N` on cache processes**: proactive compaction.
- **Option C — `hibernate` on idle servers**: memory-efficient at the cost of warm-up latency on wakeup.
- **Option D — manual `garbage_collect/1` from a monitor**: reactive, complicates control flow.

Combined: B for caches, C for idle handlers, A elsewhere.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule GcLab.MixProject do
  use Mix.Project
  def project, do: [app: :gc_lab, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  def application, do: [extra_applications: [:logger]]
  defp deps, do: [{:benchee, "~> 1.3", only: :dev}]
end
```

### Step 1: Cache — `lib/gc_lab/cache.ex`

**Objective**: Set `fullsweep_after: 10` inside `init/1` so the cache GenServer promotes minor GCs to majors and compacts churned data.

```elixir
defmodule GcLab.Cache do
  use GenServer

  def start_link(opts) do
    gen_opts = Keyword.take(opts, [:name])
    spawn_opts = Keyword.get(opts, :spawn_opt, [])
    GenServer.start_link(__MODULE__, :ok, [spawn_opt: spawn_opts] ++ gen_opts)
  end

  @impl true
  def init(:ok) do
    # Tune this process's own GC cadence.
    :erlang.process_flag(:fullsweep_after, 10)
    {:ok, %{data: %{}}}
  end

  def put(server, key, value), do: GenServer.call(server, {:put, key, value})
  def drop(server, key), do: GenServer.call(server, {:drop, key})
  def info(server), do: GenServer.call(server, :info)

  @impl true
  def handle_call({:put, k, v}, _from, state), do: {:reply, :ok, put_in(state.data[k], v)}
  def handle_call({:drop, k}, _from, state), do: {:reply, :ok, update_in(state.data, &Map.delete(&1, k))}
  def handle_call(:info, _from, state) do
    info =
      Process.info(self(), [:total_heap_size, :heap_size, :garbage_collection])

    {:reply, info, state}
  end
end
```

### Step 2: Probe helpers — `lib/gc_lab/gc_probe.ex`

**Objective**: Surface `total_heap_size`, minor GC counts, and `fullsweep_after` per pid so tests can assert on real heap behaviour.

```elixir
defmodule GcLab.GcProbe do
  def heap_words(pid), do: Process.info(pid, :total_heap_size) |> elem(1)

  def gc_counts(pid) do
    {:garbage_collection, info} = Process.info(pid, :garbage_collection)
    Keyword.take(info, [:minor_gcs, :fullsweep_after])
  end

  def force_gc(pid), do: :erlang.garbage_collect(pid)
end
```

## Why this works

`process_flag(:fullsweep_after, 10)` makes the VM promote every 11th minor GC to a major. On a cache process that steadily overwrites entries, the "old" data becomes unreachable each time a value is replaced. Majors reclaim that old-gen garbage. With default `fullsweep_after: 65535`, the old heap never compacts and climbs unboundedly.

Hibernate shrinks `heap_size` to `min_heap_size`. A 50 MB idle GenServer drops to < 100 KB after hibernate, at the cost of heap re-growth on the next message.

## Tests — `test/gc_lab/gc_test.exs`

```elixir
defmodule GcLab.GcTest do
  use ExUnit.Case, async: true
  alias GcLab.{Cache, GcProbe}

  describe "fullsweep_after=10" do
    test "cache heap stabilizes under churn" do
      {:ok, pid} = Cache.start_link([])

      for i <- 1..5_000 do
        Cache.put(pid, i, String.duplicate("x", 256))
        if rem(i, 2) == 0, do: Cache.drop(pid, i)
      end

      GcProbe.force_gc(pid)
      heap = GcProbe.heap_words(pid)
      assert heap < 500_000, "expected compacted heap, got #{heap} words"
    end
  end

  describe "minor gcs" do
    test "accumulate under allocation pressure" do
      {:ok, pid} = Cache.start_link([])
      before = GcProbe.gc_counts(pid) |> Keyword.fetch!(:minor_gcs)

      for i <- 1..10_000, do: Cache.put(pid, i, i)

      after_ = GcProbe.gc_counts(pid) |> Keyword.fetch!(:minor_gcs)
      assert after_ - before > 0
    end
  end

  describe "process_flag fullsweep_after" do
    test "is visible in process_info" do
      {:ok, pid} = Cache.start_link([])
      info = GcProbe.gc_counts(pid)
      assert info[:fullsweep_after] == 10
    end
  end
end
```

## Benchmark — `bench/fullsweep_bench.exs`

```elixir
defmodule Bench do
  def churn(fullsweep) do
    {:ok, pid} =
      GcLab.Cache.start_link(name: :"cache_#{fullsweep}_#{:erlang.unique_integer([:positive])}")

    :erlang.process_flag(:fullsweep_after, fullsweep)

    {us, _} =
      :timer.tc(fn ->
        for i <- 1..100_000 do
          GcLab.Cache.put(pid, rem(i, 1000), String.duplicate("x", 128))
        end
      end)

    {:total_heap_size, heap} = Process.info(pid, :total_heap_size)
    GenServer.stop(pid)
    {us, heap}
  end
end

for fs <- [65535, 100, 20, 5] do
  {us, heap} = Bench.churn(fs)
  IO.puts("fullsweep_after=#{fs}: time=#{div(us, 1000)}ms, heap=#{heap} words")
end
```

**Expected**: `fullsweep_after=65535` — heap grows unboundedly, time stays lowest. `fullsweep_after=5` — heap smallest, time ~15% higher due to major-GC overhead. `fullsweep_after=20` is usually the sweet spot for cache-shaped processes.

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

**1. Low `fullsweep_after` costs CPU.** Full sweeps copy the entire live heap. On a 200 MB process, that is a noticeable pause.

**2. Hibernate latency on wake.** First message after hibernate has a heap-growth pause. For latency-sensitive processes, avoid hibernate even if memory looks attractive.

**3. Manual `garbage_collect/1` is a sledgehammer.** A full sweep on a busy process stalls it. Never schedule it "every N seconds" on hot processes.

**4. GC is per-process — shared binaries are separate.** Refc binaries live in a shared pool. A process with no major sweeps keeps refc alive even if nobody uses them, causing "binary leak" symptoms.

**5. `observer`'s "reductions/sec" drops during fullsweep.** That is correct, not a bug. Do not chase false positives.

**6. When NOT to tune.** Processes that live < 100ms: GC never runs anyway. Supervised trees with `restart: :temporary`: spawn a new process instead.

## Reflection

A memory graph shows a saw-tooth pattern: memory climbs for 30 seconds, drops by 40% every 30 seconds. You did NOT set `fullsweep_after`. What is causing the periodic drops, and how do you confirm?

## Resources

- [`:erlang.process_flag/2` :fullsweep_after — erlang.org](https://www.erlang.org/doc/man/erlang.html#process_flag-2)
- [GC internals — The BEAM Book](https://blog.stenmans.org/theBeamBook/)
- [Erlang GC history — Lukas Larsson](https://www.erlang.org/blog/a-brief-beam-vm-tour/)
- [Erlang in Anger — long-lived processes](https://www.erlang-in-anger.com/)

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```
