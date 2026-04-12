# BEAM schedulers, reductions, and run queues

**Project**: `schedulers_deep` — observe the BEAM scheduler from Elixir, measure scheduler utilization, and detect run-queue imbalance under load.

---

## Project context

You are tuning a backend service that handles ~40k req/s on a 16-core box. Load
tests show only 55% CPU utilization while p99 latency is already over budget.
`top` shows `beam.smp` pegged on one core and mostly idle on the rest. Something
is pinning work onto a single scheduler.

Before you can fix it, you need to actually observe BEAM internals from Elixir:
how many schedulers are running, how busy each one is, which run queues have
backlog, and which processes are eating reductions. This exercise builds a small
toolkit on top of `:erlang.statistics/1`, `:scheduler`, and `:recon` to produce
the same numbers `observer_cli` shows — so you understand what the numbers mean.

Project structure:

```
schedulers_deep/
├── lib/
│   └── schedulers_deep/
│       ├── reporter.ex          # scheduler_wall_time sampling
│       ├── run_queue.ex         # per-scheduler run queue lengths
│       ├── reductions.ex        # top-N processes by reductions
│       └── load_gen.ex          # generates synthetic CPU load
├── test/
│   └── schedulers_deep/
│       ├── reporter_test.exs
│       └── reductions_test.exs
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

### 1. What the scheduler actually is

A BEAM scheduler is an OS thread that runs Erlang processes cooperatively. By
default, `beam.smp` starts one *normal* scheduler per logical CPU core. Each
scheduler owns a **run queue** of ready-to-run processes and picks the next
one based on priority bands (`:max`, `:high`, `:normal`, `:low`).

```
  ┌────────── beam.smp OS process ──────────┐
  │                                         │
  │  Sched 1     Sched 2  ...   Sched N     │  (N = erlang:system_info(:schedulers))
  │    │           │              │         │
  │  [runq1]     [runq2]        [runqN]     │  per-scheduler run queues
  │    │           │              │         │
  │   pid_a       pid_b          pid_c      │
  │   pid_d                      pid_e      │
  └─────────────────────────────────────────┘
```

Plus separate pools for **dirty CPU** schedulers, **dirty IO** schedulers, and
**async thread** pools — covered in exercises 146 and 147.

### 2. Reductions: cooperative preemption without OS threads

Erlang processes don't run until they block — they run until they burn a
fixed **reduction budget** (currently 4000 per scheduling slot; the classic
"2000" number is outdated but still quoted in older books). One reduction
is roughly one function call. When the counter hits zero, the scheduler
preempts the process and moves on.

This is why BEAM stays responsive under CPU load: no single process can hog
a scheduler for more than ~1ms of wall time on modern hardware. See
exercise 148 for a deep dive.

### 3. `scheduler_wall_time`: the only honest utilization metric

`top` and `htop` report OS-thread utilization, but a BEAM scheduler that is
"spinning" waiting for work still shows as 100% busy to the OS. The only way
to know whether schedulers are **actually doing useful work** is
`:erlang.statistics(:scheduler_wall_time)`.

It returns `{active_time, total_time}` per scheduler. Ratio = true utilization.

```
sched 1: {3_000_000, 10_000_000}  → 30% actually working
sched 2: {9_800_000, 10_000_000}  → 98% (hot — investigate)
sched 3: {  200_000, 10_000_000}  → 2%  (idle — work is not migrating here)
```

Enable it once with `:erlang.system_flag(:scheduler_wall_time, true)`. It has
measurable overhead — turn it off in prod when you're done.

### 4. Run queue imbalance

BEAM tries to balance work across schedulers via **process migration**: idle
schedulers steal from busy ones. Migration has limits — it prefers to keep
a process on the scheduler it last ran on (cache locality). If you
`spawn` thousands of processes from a single parent, the children initially
land on the parent's scheduler. Migration catches up, but not instantly.

`:erlang.statistics(:run_queue_lengths)` returns the current length per
scheduler. A healthy system has them roughly equal. One queue with 500 entries
and the rest empty is a smoking gun.

### 5. Why a single hot process kills throughput

If one process runs a tight CPU loop, BEAM preempts it every 4000 reductions —
but on a 1-core machine or when all work depends on that process's output,
the rest of the system waits. Examples from production:

- A `GenServer.call` to a hot process serializes all requests.
- A `Task.async_stream` with `max_concurrency: 1` underuses schedulers.
- NIFs that don't yield block the scheduler until they return (see 146).

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

### Step 1: create the project

```bash
mix new schedulers_deep --sup
cd schedulers_deep
```

### Step 2: `mix.exs`

```elixir
defmodule SchedulersDeep.MixProject do
  use Mix.Project

  def project do
    [
      app: :schedulers_deep,
      version: "0.1.0",
      elixir: "~> 1.16",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger], mod: {SchedulersDeep.Application, []}]
  end

  defp deps do
    [
      {:recon, "~> 2.5"},
      {:benchee, "~> 1.3", only: :dev}
    ]
  end
end
```

### Step 3: `lib/schedulers_deep/reporter.ex`

```elixir
defmodule SchedulersDeep.Reporter do
  @moduledoc """
  Samples `:scheduler_wall_time` over a window and returns per-scheduler
  utilization as a percentage.

  Rationale: `:scheduler_wall_time` is cumulative since the flag was enabled.
  A single read is useless. You need two reads delta-ed over a window.
  """

  @type utilization :: %{pos_integer() => float()}

  @doc "Enables the `:scheduler_wall_time` flag globally."
  @spec enable() :: boolean()
  def enable, do: :erlang.system_flag(:scheduler_wall_time, true)

  @spec disable() :: boolean()
  def disable, do: :erlang.system_flag(:scheduler_wall_time, false)

  @doc """
  Samples utilization over `window_ms` and returns a map
  `%{scheduler_id => utilization_percent}`.
  """
  @spec sample(pos_integer()) :: utilization()
  def sample(window_ms) when window_ms > 0 do
    before = read_sorted()
    Process.sleep(window_ms)
    now = read_sorted()
    diff(before, now)
  end

  defp read_sorted do
    :erlang.statistics(:scheduler_wall_time)
    |> Enum.sort()
  end

  defp diff(before, now) do
    Enum.zip(before, now)
    |> Enum.reduce(%{}, fn {{id, a0, t0}, {id, a1, t1}}, acc ->
      active = a1 - a0
      total = t1 - t0
      ratio = if total == 0, do: 0.0, else: active / total * 100.0
      Map.put(acc, id, Float.round(ratio, 2))
    end)
  end

  @doc "Returns counts of normal, dirty-cpu and dirty-io schedulers."
  @spec scheduler_counts() :: %{normal: pos_integer(), dirty_cpu: non_neg_integer(), dirty_io: non_neg_integer()}
  def scheduler_counts do
    %{
      normal: :erlang.system_info(:schedulers),
      dirty_cpu: :erlang.system_info(:dirty_cpu_schedulers),
      dirty_io: :erlang.system_info(:dirty_io_schedulers)
    }
  end
end
```

### Step 4: `lib/schedulers_deep/run_queue.ex`

```elixir
defmodule SchedulersDeep.RunQueue do
  @moduledoc """
  Snapshots per-scheduler run queue lengths. Imbalance often explains
  why adding cores doesn't improve throughput.
  """

  @spec snapshot() :: %{pos_integer() => non_neg_integer()}
  def snapshot do
    :erlang.statistics(:run_queue_lengths)
    |> Enum.with_index(1)
    |> Map.new(fn {len, id} -> {id, len} end)
  end

  @doc """
  Returns `{max, min, ratio}`. A ratio greater than ~3 suggests migration
  is not keeping up and you should investigate hot spawners.
  """
  @spec imbalance() :: {non_neg_integer(), non_neg_integer(), float()}
  def imbalance do
    lens = :erlang.statistics(:run_queue_lengths)
    max_l = Enum.max(lens)
    min_l = Enum.min(lens)
    ratio = if min_l == 0, do: max_l * 1.0, else: max_l / min_l
    {max_l, min_l, Float.round(ratio, 2)}
  end
end
```

### Step 5: `lib/schedulers_deep/reductions.ex`

```elixir
defmodule SchedulersDeep.Reductions do
  @moduledoc """
  Top-N processes by reductions delta — this is how you find the process
  actually burning CPU, as opposed to the one with the largest mailbox.
  """

  @doc """
  Returns the top `n` processes by reductions consumed over `window_ms`.

  Each entry is `%{pid: pid, reductions: delta, initial_call: mfa,
  current_function: mfa, registered: name_or_nil}`.
  """
  @spec top(pos_integer(), pos_integer()) :: [map()]
  def top(n, window_ms) when n > 0 and window_ms > 0 do
    before = snapshot_reductions()
    Process.sleep(window_ms)
    now = snapshot_reductions()

    now
    |> Enum.map(fn {pid, r1} ->
      delta = r1 - Map.get(before, pid, 0)
      {pid, delta}
    end)
    |> Enum.sort_by(fn {_pid, d} -> -d end)
    |> Enum.take(n)
    |> Enum.map(&describe/1)
  end

  defp snapshot_reductions do
    for pid <- Process.list(), into: %{} do
      case Process.info(pid, :reductions) do
        {:reductions, r} -> {pid, r}
        nil -> {pid, 0}
      end
    end
  end

  defp describe({pid, delta}) do
    info = Process.info(pid, [:initial_call, :registered_name, :current_function]) || []

    %{
      pid: pid,
      reductions: delta,
      initial_call: Keyword.get(info, :initial_call),
      current_function: Keyword.get(info, :current_function),
      registered: registered(Keyword.get(info, :registered_name))
    }
  end

  defp registered([]), do: nil
  defp registered(nil), do: nil
  defp registered(name), do: name
end
```

### Step 6: `lib/schedulers_deep/load_gen.ex`

```elixir
defmodule SchedulersDeep.LoadGen do
  @moduledoc """
  Synthetic load generator — spawns `n` CPU-bound processes each looping
  for `duration_ms`. Useful to reproduce scheduler saturation in tests.
  """

  @spec run(pos_integer(), pos_integer()) :: :ok
  def run(n, duration_ms) do
    parent = self()
    refs = for _ <- 1..n, do: spawn_worker(parent, duration_ms)
    Enum.each(refs, fn ref -> receive do {:done, ^ref} -> :ok end end)
    :ok
  end

  defp spawn_worker(parent, duration_ms) do
    ref = make_ref()

    spawn_link(fn ->
      deadline = System.monotonic_time(:millisecond) + duration_ms
      burn(deadline)
      send(parent, {:done, ref})
    end)

    ref
  end

  defp burn(deadline) do
    if System.monotonic_time(:millisecond) >= deadline do
      :ok
    else
      _ = Enum.reduce(1..10_000, 0, fn i, acc -> acc + i end)
      burn(deadline)
    end
  end
end
```

### Step 7: `lib/schedulers_deep/application.ex`

```elixir
defmodule SchedulersDeep.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    SchedulersDeep.Reporter.enable()
    Supervisor.start_link([], strategy: :one_for_one, name: SchedulersDeep.Supervisor)
  end
end
```

### Step 8: tests

```elixir
# test/schedulers_deep/reporter_test.exs
defmodule SchedulersDeep.ReporterTest do
  use ExUnit.Case, async: false

  alias SchedulersDeep.{Reporter, LoadGen}

  setup do
    Reporter.enable()
    on_exit(fn -> Reporter.disable() end)
    :ok
  end

  test "sample/1 returns one entry per scheduler" do
    util = Reporter.sample(200)
    assert map_size(util) == :erlang.system_info(:schedulers)
    assert Enum.all?(util, fn {_id, pct} -> pct >= 0.0 and pct <= 100.0 end)
  end

  test "loaded schedulers show non-zero utilization" do
    parent = self()

    spawn_link(fn ->
      LoadGen.run(:erlang.system_info(:schedulers) * 2, 300)
      send(parent, :load_done)
    end)

    util = Reporter.sample(200)
    assert_receive :load_done, 2_000
    assert Enum.any?(util, fn {_id, pct} -> pct > 5.0 end)
  end

  test "scheduler_counts/0 returns positive normal count" do
    counts = Reporter.scheduler_counts()
    assert counts.normal > 0
    assert counts.dirty_cpu >= 0
    assert counts.dirty_io >= 0
  end
end
```

```elixir
# test/schedulers_deep/reductions_test.exs
defmodule SchedulersDeep.ReductionsTest do
  use ExUnit.Case, async: false

  alias SchedulersDeep.Reductions

  test "top/2 returns processes sorted by reductions" do
    spawn(fn -> Enum.reduce(1..1_000_000, 0, fn i, a -> a + i end) end)
    results = Reductions.top(5, 100)
    assert length(results) <= 5
    deltas = Enum.map(results, & &1.reductions)
    assert deltas == Enum.sort(deltas, :desc)
  end

  test "top/2 entries have expected shape" do
    [first | _] = Reductions.top(3, 50)
    assert is_pid(first.pid)
    assert is_integer(first.reductions)
    assert match?({_m, _f, _a}, first.initial_call) or is_nil(first.initial_call)
  end
end
```

### Step 9: smoke run

```bash
mix test
iex -S mix
iex> SchedulersDeep.Reporter.enable()
iex> spawn(fn -> SchedulersDeep.LoadGen.run(32, 3_000) end)
iex> SchedulersDeep.Reporter.sample(1_000)
iex> SchedulersDeep.RunQueue.snapshot()
iex> SchedulersDeep.Reductions.top(5, 1_000)
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

## Trade-offs and production gotchas

**1. `scheduler_wall_time` has overhead**
Each context switch stamps a monotonic timestamp. On a 64-core box the
overhead is measurable (2–4%). Enable it long enough to diagnose, then
disable. For continuous production observability, sample once every
30–60 seconds for 1-second windows.

**2. `Process.list/0` is expensive on large nodes**
It walks the process table. On a node with 500k processes the call alone
takes several milliseconds. Prefer `:recon.proc_count/2` which does the
same in C and can limit work.

**3. Reductions are not CPU time**
Two processes with equal reductions can have very different wall-clock
cost (one doing arithmetic vs one doing binary matching). Reductions are
a proxy, not a metric. For real CPU, combine `scheduler_wall_time` with
per-process accounting via tracing.

**4. Run queue imbalance is normal for short bursts**
Don't over-index on a single `run_queue_lengths` snapshot. Sample over
several seconds. Persistent imbalance (>5 seconds) is the real signal.

**5. Schedulers can be online but bound**
`+S 16:16` starts 16 schedulers, all online. `+S 16:4` starts 16 but
only 4 online. Useful for containers where `system_info(:schedulers)`
reports the host CPU count instead of the cgroup limit. Fix: set
`ERL_FLAGS="+S $(nproc):$(nproc)"` in your release.

**6. Busy-wait inflates OS CPU numbers**
Schedulers briefly busy-wait before sleeping to reduce wake latency.
This makes `top` lie. Flag `+sbwt none +sbwtdcpu none +sbwtdio none`
disables it — useful for containerized workloads where CPU is billed.

**7. When NOT to use this**
If you have Phoenix LiveDashboard, it already exposes these metrics.
This toolkit is worth writing when you need to export numbers to
Prometheus, embed them in a custom health endpoint, or reason about
them in test code. For interactive diagnosis, use LiveDashboard or
`observer_cli`.

---

## Performance notes

Expected wall-time cost on a 2023 M2 laptop, 8 normal schedulers:

| Call | p50 | p99 |
|------|-----|-----|
| `Reporter.sample(1_000)` | ~1001 ms | ~1005 ms (dominated by sleep) |
| `RunQueue.snapshot/0` | 8 µs | 30 µs |
| `Reductions.top(10, 1_000)` with 200 procs | ~1002 ms | ~1010 ms |
| `Reductions.top(10, 1_000)` with 50k procs | ~1050 ms | ~1200 ms |

`top/2` scales linearly with process count because of `Process.list/0`.
Use `:recon.proc_count(:reductions, N)` for large nodes.

---

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?

## Resources

- [Erlang `:erlang.statistics/1`](https://www.erlang.org/doc/man/erlang.html#statistics-1) — authoritative docs on every counter
- [The BEAM Book — Scheduling](https://blog.stenmans.org/theBeamBook/#CH-Scheduling) — Erik Stenman, free
- [Erlang in Anger — Fred Hébert](https://www.erlang-in-anger.com/) — chapter 4 on `scheduler_wall_time` and recon
- [`:recon.scheduler_usage/1`](https://ferd.github.io/recon/recon.html#scheduler_usage-1) — source implementation of the same idea
- ["A Brief BEAM Primer" — Rickard Green](https://www.erlang.org/blog/a-brief-beam-primer/) — Erlang/OTP core developer
- [Phoenix LiveDashboard source — Home page](https://github.com/phoenixframework/phoenix_live_dashboard/blob/main/lib/phoenix/live_dashboard/pages/home_page.ex)
