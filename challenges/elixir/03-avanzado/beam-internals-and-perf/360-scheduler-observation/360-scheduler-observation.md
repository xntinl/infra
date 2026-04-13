# Scheduler Observation with `:scheduler` and `:erlang.statistics/1`

**Project**: `scheduler_observatory` — a GenServer that samples scheduler utilization, run queue lengths, and active counts, exposing them as telemetry events you can plot or alert on.

## Project context

A video-processing job node is showing 100% CPU on `top` but throughput is half of a lightly-loaded sibling. `observer` shows normal VM memory. The team suspects scheduler imbalance — one scheduler is pinned by a long-running NIF while the others idle. To confirm, you need the actual per-scheduler utilization, not OS-level CPU.

The BEAM exposes this via `:scheduler.utilization/1` (percentage per scheduler and aggregate) and `:erlang.statistics/1` (multiple counters: `:run_queue_lengths`, `:total_run_queue_lengths`, `:scheduler_wall_time`). Sampling them over intervals yields the same numbers `observer → Load Charts` displays — but as structured data you can export.

```
scheduler_observatory/
├── lib/
│   └── scheduler_observatory/
│       ├── application.ex
│       ├── sampler.ex
│       └── workload.ex
├── test/
│   └── scheduler_observatory/
│       └── sampler_test.exs
├── bench/
│   └── scheduler_pressure_bench.exs
└── mix.exs
```

## Why sample, not just query once

A single `:scheduler.utilization/1` call returns values since VM boot — nearly useless on a long-running node (everything averages out). Utilization is only meaningful over an **interval**: take a snapshot at T1, another at T2, and diff. The library `:scheduler` gives you `sample/0` + `utilization/1` for exactly this.

**Why not `top` or `htop`?** OS-level CPU lumps dirty schedulers, async threads, and regular schedulers. The VM distinguishes them. A dirty CPU scheduler at 100% while all others are at 10% tells a different story than "100% CPU".

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
### 1. Regular vs dirty schedulers

- **Regular schedulers**: default `System.schedulers_online/0` (equal to cores). Run pure Elixir code.
- **Dirty CPU schedulers**: for long CPU-bound NIFs (e.g., JSON decoders, crypto). Default count = online cores.
- **Dirty IO schedulers**: for long IO NIFs. Default 10.

If a NIF that should have been dirty is scheduled on a regular scheduler, it blocks the scheduler for the duration of the NIF — the "BEAM freeze" symptom.

### 2. `:scheduler_wall_time`

Enable with `:erlang.system_flag(:scheduler_wall_time, true)`. Each scheduler tracks `{active_time, total_time}` in nanoseconds. Utilization = `(active2 - active1) / (total2 - total1)`.

It has a non-zero cost (~1% overhead). Turn it on during investigation, turn it off on healthy nodes.

### 3. `:run_queue_lengths`

Returns the queue length of each scheduler. A scheduler with a consistently long queue is oversubscribed. One scheduler long while others are 0 is pathological.

### 4. `:total_active_tasks`

Counts runnable processes across all schedulers. A gentle proxy for load.

## Design decisions

- **Option A — attach `observer`**: visual, one-off. Not automated.
- **Option B — sample with a GenServer, emit telemetry**: integrated with Prometheus/LiveDashboard, alertable.
- **Option C — run a custom NIF probe**: not necessary; built-ins suffice.

Chosen: Option B.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule SchedulerObservatory.MixProject do
  use Mix.Project
  def project, do: [app: :scheduler_observatory, version: "0.1.0", elixir: "~> 1.16", deps: deps()]

  def application do
    [mod: {SchedulerObservatory.Application, []}, extra_applications: [:logger, :scheduler]]
  end

  defp deps do
    [{:telemetry, "~> 1.2"}, {:benchee, "~> 1.3", only: :dev}]
  end
end
```

### Step 1: Application — enable `scheduler_wall_time`

**Objective**: Enable `:scheduler_wall_time` at boot so per-scheduler active:total ratios emerge without losing counts on node restart.

```elixir
defmodule SchedulerObservatory.Application do
  use Application

  @impl true
  def start(_type, _args) do
    :erlang.system_flag(:scheduler_wall_time, true)

    children = [
      {SchedulerObservatory.Sampler, interval_ms: 1_000}
    ]

    Supervisor.start_link(children, strategy: :one_for_one)
  end
end
```

### Step 2: Sampler — `lib/scheduler_observatory/sampler.ex`

**Objective**: Diff two `:scheduler.sample/0` snapshots via GenServer so scheduler utilization percentages export to Prometheus/LiveDashboard unchanged.

```elixir
defmodule SchedulerObservatory.Sampler do
  @moduledoc """
  Samples scheduler utilization on a fixed interval.

  Emits telemetry event [:beam, :scheduler, :sample] with measurements:
    - active_percent_total    aggregate across all schedulers
    - active_percent_per      list of per-scheduler percentages
    - run_queue_total
    - run_queue_per           per-scheduler run queue length
  """
  use GenServer

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(opts) do
    interval = Keyword.fetch!(opts, :interval_ms)
    state = %{interval: interval, baseline: :scheduler.sample()}
    Process.send_after(self(), :sample, interval)
    {:ok, state}
  end

  @impl true
  def handle_info(:sample, %{baseline: baseline, interval: interval} = state) do
    current = :scheduler.sample()

    util = :scheduler.utilization(baseline, current)
    aggregate = Enum.find(util, &match?({:total, _, _}, &1))
    per_scheduler = Enum.filter(util, &match?({:normal, _, _, _}, &1))

    run_q_per = :erlang.statistics(:run_queue_lengths)
    run_q_total = :erlang.statistics(:total_run_queue_lengths)

    :telemetry.execute(
      [:beam, :scheduler, :sample],
      %{
        active_percent_total: percent(aggregate),
        active_percent_per: Enum.map(per_scheduler, &percent/1),
        run_queue_total: run_q_total,
        run_queue_per: run_q_per
      },
      %{}
    )

    Process.send_after(self(), :sample, interval)
    {:noreply, %{state | baseline: current}}
  end

  defp percent({:total, active, total}), do: ratio(active, total)
  defp percent({:normal, _id, active, total}), do: ratio(active, total)
  defp percent({_, active, total}), do: ratio(active, total)

  defp ratio(_active, 0), do: 0.0
  defp ratio(active, total), do: active / total * 100
end
```

### Step 3: A workload to actually observe — `lib/scheduler_observatory/workload.ex`

**Objective**: Spawn N CPU-bound loopers burning reductions so telemetry capture shows observable scheduler saturation under synthetic load.

```elixir
defmodule SchedulerObservatory.Workload do
  @doc """
  Spin up N busy-loopers that hog reductions. Useful to see run queues grow.
  """
  def saturate(n) do
    for _ <- 1..n do
      spawn(fn -> loop(0) end)
    end
  end

  defp loop(n) when n > 100_000_000, do: :done
  defp loop(n), do: loop(n + 1)
end
```

## Why this works

`:scheduler.sample/0` is a cheap snapshot (reads per-scheduler counters atomically). `:scheduler.utilization/2` diffs two snapshots and returns percentages that match what `observer` shows. Emitting via telemetry lets any reporter (LiveDashboard, Prometheus, StatsD) consume the data without coupling the sampler to a specific backend.

## Tests — `test/scheduler_observatory/sampler_test.exs`

```elixir
defmodule SchedulerObservatory.SamplerTest do
  use ExUnit.Case, async: false

  setup do
    :erlang.system_flag(:scheduler_wall_time, true)
    :ok
  end

  describe "telemetry events" do
    test "emits [:beam, :scheduler, :sample] at the configured interval" do
      :telemetry.attach(
        "test-handler",
        [:beam, :scheduler, :sample],
        fn event, measurements, meta, pid -> send(pid, {event, measurements, meta}) end,
        self()
      )

      {:ok, _pid} = SchedulerObservatory.Sampler.start_link(interval_ms: 50)

      assert_receive {[:beam, :scheduler, :sample], measurements, _}, 500
      assert is_number(measurements.active_percent_total)
      assert is_list(measurements.active_percent_per)
      assert is_integer(measurements.run_queue_total)
    after
      :telemetry.detach("test-handler")
    end
  end

  describe "values under load" do
    test "aggregate utilization rises when CPU is busy" do
      :erlang.system_flag(:scheduler_wall_time, true)
      base = :scheduler.sample()

      tasks =
        for _ <- 1..System.schedulers_online() do
          Task.async(fn -> Enum.reduce(1..2_000_000, 0, &(&1 + &2)) end)
        end

      Task.await_many(tasks, 5_000)
      after_sample = :scheduler.sample()
      [{:total, active, total} | _] = :scheduler.utilization(base, after_sample)
      assert active / total > 0.1
    end
  end
end
```

## Benchmark — `bench/scheduler_pressure_bench.exs`

```elixir
:erlang.system_flag(:scheduler_wall_time, true)

pre = :scheduler.sample()

# Saturate
tasks =
  for _ <- 1..(System.schedulers_online() * 4) do
    Task.async(fn -> Enum.reduce(1..5_000_000, 0, &(&1 + &2)) end)
  end

Task.await_many(tasks, 30_000)
post = :scheduler.sample()

:scheduler.utilization(pre, post)
|> Enum.each(&IO.inspect/1)

IO.puts("run queue lengths: #{inspect(:erlang.statistics(:run_queue_lengths))}")
```

**Expected**: aggregate > 90% during saturation, per-scheduler numbers close to each other (balanced). If one scheduler is > 95% and others < 20%, a NIF or port driver is not releasing the scheduler.

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

**1. `scheduler_wall_time` has ~1% overhead.** Enable during investigation; disable in steady state. Some teams leave it on permanently — measure before committing.

**2. Utilization is interval-dependent.** A 1-second sample misses sub-second spikes. For latency SLO work, use 100ms samples; for capacity planning, 10s is enough.

**3. Reading `:scheduler.sample()` atomically is not instant across cores.** On a 64-core box, there is minor drift. Fine for percentages, not for synchronizing to a deadline.

**4. Dirty scheduler utilization is separate.** `:scheduler.utilization/1` filters tags — look for `:cpu` and `:io` tuples for dirty schedulers specifically.

**5. A quiet scheduler is not necessarily idle.** It might be busy polling `:gen_tcp.recv` with zero timeout, which registers as active but does no useful work.

**6. When NOT to measure this.** Very short-lived processes, CLI scripts — the VM exits before any numbers stabilize. Use `:timer.tc/1` instead.

## Reflection

You see aggregate utilization at 30% but tail latencies are 10x normal. Scheduler run queue lengths are all 0. What else do you measure to pinpoint the slowdown? (Hint: scheduler busy is not the only wait.)


## Executable Example

```elixir
defmodule SchedulerObservatory.Sampler do
  @moduledoc """
  Samples scheduler utilization on a fixed interval.

  Emits telemetry event [:beam, :scheduler, :sample] with measurements:
    - active_percent_total    aggregate across all schedulers
    - active_percent_per      list of per-scheduler percentages
    - run_queue_total
    - run_queue_per           per-scheduler run queue length
  """
  use GenServer

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(opts) do
    interval = Keyword.fetch!(opts, :interval_ms)
    state = %{interval: interval, baseline: :scheduler.sample()}
    Process.send_after(self(), :sample, interval)
    {:ok, state}
  end

  @impl true
  def handle_info(:sample, %{baseline: baseline, interval: interval} = state) do
    current = :scheduler.sample()

    util = :scheduler.utilization(baseline, current)
    aggregate = Enum.find(util, &match?({:total, _, _}, &1))
    per_scheduler = Enum.filter(util, &match?({:normal, _, _, _}, &1))

    run_q_per = :erlang.statistics(:run_queue_lengths)
    run_q_total = :erlang.statistics(:total_run_queue_lengths)

    :telemetry.execute(
      [:beam, :scheduler, :sample],
      %{
        active_percent_total: percent(aggregate),
        active_percent_per: Enum.map(per_scheduler, &percent/1),
        run_queue_total: run_q_total,
        run_queue_per: run_q_per
      },
      %{}
    )

    Process.send_after(self(), :sample, interval)
    {:noreply, %{state | baseline: current}}
  end

  defp percent({:total, active, total}), do: ratio(active, total)
  defp percent({:normal, _id, active, total}), do: ratio(active, total)
  defp percent({_, active, total}), do: ratio(active, total)

  defp ratio(_active, 0), do: 0.0
  defp ratio(active, total), do: active / total * 100
end

defmodule Main do
  def main do
      IO.puts("Recon diagnostics initialized")
      memory_stats = :erlang.memory()
      if is_list(memory_stats) do
        IO.puts("✓ Recon diagnostics: memory info available")
      end
  end
end

Main.main()
```
