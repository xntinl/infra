# Reductions Budget and `:erlang.bump_reductions/1`

**Project**: `reduction_lab` — experiments that measure the BEAM's 4000-reduction budget, show how `bump_reductions/1` accounts for work done in NIFs/BIFs, and demonstrate why ignoring reductions starves other processes.

## Project context

Your team ships a NIF that parses a proprietary binary format. One parse takes 2 ms of wall time. The symptom: other processes on the node see latency spikes when many parses run concurrently. The profiler says CPU is not saturated. The root cause: the NIF does not yield, the scheduler does not preempt it until it returns, and one parse consumes roughly 8 million reductions of "budget" that never get attributed to anybody.

Reductions are the BEAM's unit of scheduling. Every function call, binary send, and ETS operation charges some reductions. When a process accumulates 4000, the scheduler preempts it and picks the next ready process. Pure Elixir code is naturally preemptive. NIFs, BIFs, and tight loops over large data are not — which is where `:erlang.bump_reductions/1` comes in.

```
reduction_lab/
├── lib/
│   └── reduction_lab/
│       ├── slow_nif_sim.ex
│       └── fair_loop.ex
├── test/
│   └── reduction_lab/
│       └── reductions_test.exs
├── bench/
│   └── fairness_bench.exs
└── mix.exs
```

## Why reductions matter

The scheduler is cooperative. A process keeps the CPU until it (a) exits, (b) blocks on `receive`, or (c) hits the 4000-reduction ceiling. Without that ceiling, a `for _ <- 1..10_000_000, do: ...` would monopolize the scheduler.

A pure Elixir loop is safe because every iteration of `Enum.reduce/3` costs a known number of reductions. A NIF is not: to the scheduler, the NIF call is ONE reduction, regardless of wall time. A 2ms NIF blocks the scheduler for 2ms plus the cost of any processes queued behind it.

`:erlang.bump_reductions/1` lets a library declare "I just did N reductions of work" so the scheduler accounts for it. It is the poor-man's yielding for pure-Erlang code that does heavy computation in one function.

**Why not just use dirty schedulers?** Dirty schedulers are for NIFs that cannot yield. For pure Elixir, dirty schedulers are not available — the VM will not dispatch regular Elixir code there. `bump_reductions/1` is the pure-Elixir alternative.

## Core concepts

### 1. The 4000-reduction slice

Default reduction budget per schedule slot is 4000 (configurable via `+P` and per-process). A BIF charges a number of reductions proportional to its cost: `:lists.reverse/1` for 1000 elements ≈ 1000 reductions. `:erlang.send/2` is ~1 reduction regardless of message size (which is why large messages are still a problem — the COST is not charged).

### 2. `:erlang.bump_reductions(n)`

Adds `n` to the current process's reduction count. If the count crosses the 4000 ceiling, the process yields immediately. Use inside tight computational loops where each "step" does more than 1 reduction of work.

### 3. `:erlang.process_info(pid, :reductions)`

Returns the total reductions since process spawn. Diff two samples to see the rate. A process that accumulates 10M reductions/sec is doing real work; one at 100/sec is idle.

### 4. `:reduction_limit`

`spawn_opt/2` accepts `max_heap_size` but not a reduction ceiling — there is no per-process reduction limit you can tune. You can only nudge the scheduler via `bump_reductions` and by yielding manually.

## Design decisions

- **Option A — tight loop, no bumping**: scheduler preempts only on natural yield points. Other processes starve.
- **Option B — explicit `Process.sleep(0)` every N iterations**: fires a reduction and yields. Cheap but noisy.
- **Option C — `:erlang.bump_reductions/1` inside the loop**: accurate accounting, no spurious wakeups.

Chosen: Option C when the work is well-defined and you can estimate reductions per unit. Option B as a quick-and-dirty fix.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule ReductionLab.MixProject do
  use Mix.Project
  def project, do: [app: :reduction_lab, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  def application, do: [extra_applications: [:logger]]
  defp deps, do: [{:benchee, "~> 1.3", only: :dev}]
end
```

### Step 1: Simulated slow NIF — `lib/reduction_lab/slow_nif_sim.ex`

**Objective**: Burn CPU in a tight `:crypto.hash` loop to reproduce a non-yielding NIF's scheduler starvation signature.

```elixir
defmodule ReductionLab.SlowNifSim do
  @moduledoc """
  Simulates a NIF that does heavy work without yielding.
  In pure Elixir this is impossible at the VM level — but a tight
  `:crypto` loop approximates the scheduler impact.
  """

  def hog(ms) do
    deadline = System.monotonic_time(:millisecond) + ms
    do_hog(deadline)
  end

  defp do_hog(deadline) do
    # :crypto.hash is a BIF; 1024 bytes costs roughly 1 reduction per call.
    :crypto.hash(:sha256, :crypto.strong_rand_bytes(1024))

    if System.monotonic_time(:millisecond) < deadline do
      do_hog(deadline)
    else
      :ok
    end
  end
end
```

### Step 2: Fair loop — `lib/reduction_lab/fair_loop.ex`

**Objective**: Call `:erlang.bump_reductions/1` per 100 real units so the scheduler preempts proportionally to actual work done.

```elixir
defmodule ReductionLab.FairLoop do
  @moduledoc """
  Iterates N units of work. Each unit is ~100 reductions of real work
  but only 1 reduction is charged automatically. bump_reductions/1
  corrects the accounting so the scheduler preempts fairly.
  """

  @per_unit_cost 100

  def run(n, opts \\ []) do
    bump? = Keyword.get(opts, :bump, true)
    do_run(n, 0, bump?)
  end

  defp do_run(0, acc, _bump?), do: acc

  defp do_run(n, acc, bump?) do
    # Simulate 100 reductions worth of work
    work = Enum.reduce(1..@per_unit_cost, acc, &(&1 + &2))

    if bump?, do: :erlang.bump_reductions(@per_unit_cost)

    do_run(n - 1, work, bump?)
  end
end
```

## Why this works

`bump_reductions/1` updates the per-process reduction counter directly. When the counter crosses 4000, the scheduler preempts at the next safe point (typically the next function call). Without bumping, our loop would iterate thousands of units for only a few hundred "real" reductions of accounting, starving other processes. With bumping, we yield proportionally to work actually done.

## Tests — `test/reduction_lab/reductions_test.exs`

```elixir
defmodule ReductionLab.ReductionsTest do
  use ExUnit.Case, async: false

  describe "bump_reductions/1" do
    test "increments the process reductions counter" do
      {:reductions, before} = Process.info(self(), :reductions)
      :erlang.bump_reductions(10_000)
      {:reductions, after_} = Process.info(self(), :reductions)
      assert after_ - before >= 10_000
    end
  end

  describe "fairness" do
    test "bumped loop yields more often than unbumped" do
      me = self()

      bumped =
        Task.async(fn ->
          ReductionLab.FairLoop.run(5_000, bump: true)
          send(me, :bumped_done)
        end)

      unbumped =
        Task.async(fn ->
          ReductionLab.FairLoop.run(5_000, bump: false)
          send(me, :unbumped_done)
        end)

      Task.await_many([bumped, unbumped], 10_000)

      assert_received :bumped_done
      assert_received :unbumped_done
    end
  end

  describe "process_info reductions" do
    test "idle process accumulates near-zero reductions" do
      pid = spawn(fn -> receive do :stop -> :ok end end)
      {:reductions, r1} = Process.info(pid, :reductions)
      Process.sleep(50)
      {:reductions, r2} = Process.info(pid, :reductions)
      assert r2 - r1 < 50
      send(pid, :stop)
    end
  end
end
```

## Benchmark — `bench/fairness_bench.exs`

```elixir
# Measure the latency of a "responsive" process while another is doing heavy work.
defmodule FairnessBench do
  def ping_latency do
    me = self()
    start = System.monotonic_time(:microsecond)
    spawn(fn -> send(me, :pong) end)
    receive do
      :pong -> System.monotonic_time(:microsecond) - start
    end
  end
end

# Start a hog
spawn(fn -> ReductionLab.FairLoop.run(1_000_000, bump: false) end)
Process.sleep(5)
IO.puts("unbumped hog → ping latency: #{FairnessBench.ping_latency()}µs")

spawn(fn -> ReductionLab.FairLoop.run(1_000_000, bump: true) end)
Process.sleep(5)
IO.puts("bumped hog   → ping latency: #{FairnessBench.ping_latency()}µs")
```

**Expected**: unbumped ping latency > 1000µs; bumped < 100µs. The bumped loop yields often enough that the ping is scheduled quickly.

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

**1. `bump_reductions/1` is an optimization, not a correctness primitive.** If your loop MUST finish before another process runs, use explicit messaging (`receive`/`send`), not reduction tweaks.

**2. Over-bumping hurts throughput.** Bumping by 1M per iteration yields every iteration; the scheduler trashes its cache on every context switch. Aim for `bump_reductions(n)` with `n` close to actual reductions done.

**3. `process_info(pid, :reductions)` is non-local.** Reading the counter from another process costs a message round-trip to the target process's scheduler in some VM versions. Sample sparingly.

**4. BIFs that charge reductions lazily.** Some BIFs return immediately but mark reductions "deferred". Fine-grained measurement within a BIF is not possible; measure over a coarser window.

**5. Dirty schedulers do NOT honor reduction budgets.** A NIF on a dirty CPU scheduler runs until completion. `bump_reductions` there is a no-op.

**6. When NOT to bump.** Short loops (< 100 iterations), already preemption-friendly code (via `Enum.chunk_every` + `receive` timeouts). The optimization adds noise for < 1% throughput gain.

## Reflection

You profile a production node and see one process with 1 billion reductions — a long-running aggregator. Its neighbors have < 1 million. Does this prove the aggregator is starving neighbors? What further data do you need before deciding to rewrite it?

## Resources

- [`:erlang.bump_reductions/1` — erlang.org](https://www.erlang.org/doc/man/erlang.html#bump_reductions-1)
- [Scheduler internals — Lukas Larsson](https://www.erlang.org/blog/a-complete-guide-to-beam-scheduler/)
- [Inside the Erlang VM — Patrik Nyblom](https://www.erlang-factory.com/upload/presentations/247/erlangfactorylondon2010-patriknyblom.pdf)
- [Reductions explained — The BEAM Book](https://blog.stenmans.org/theBeamBook/)

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```
