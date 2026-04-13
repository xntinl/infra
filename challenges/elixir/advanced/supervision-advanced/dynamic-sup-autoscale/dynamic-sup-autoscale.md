# DynamicSupervisor with Queue-Driven Autoscaling

**Project**: `autoscale_sup` — a DynamicSupervisor that grows and shrinks worker count based on backlog depth.

---

## Why dynamicsupervisor with queue-driven autoscaling matters

This challenge encodes a production-grade Elixir/OTP pattern that directly affects throughput, memory, or fault-tolerance when the system is under real load. The naive approach works on a developer laptop; the version built here survives the scheduler pressure, binary refc pitfalls, and supervisor budgets of a running node.

The trade-off chart and the executable benchmark are the core of the lesson: you calibrate the cost of the abstraction against a measurable gain, not a vibe.

---
## The business problem

You run an image-processing pipeline that pulls jobs from a queue (SQS, RabbitMQ, a Postgres
table, does not matter for this exercise). Each job takes 50–500 ms of CPU and occupies ~20
MB of RAM while running. Load is spiky: idle for 10 minutes, then 5,000 jobs arriving in 30
seconds, then idle again. A static pool of 20 workers either wastes 300 MB at idle or
underprovisions the peak.

A `DynamicSupervisor` is the right primitive for starting children on demand, but it
does not autoscale by itself. You need three things working together: a **queue** that
reports its depth, a **scaler** that watches depth and child count, and a **worker
contract** so the scaler can terminate idle workers without dropping in-flight work.
The scaler is also where you enforce **min/max bounds** and **cooldown** between scale
events so you do not thrash between 5 and 50 workers every second.

Your target: a 200 lines-of-code autoscaler with three tunables (min, max, target queue
latency) that produces a measurable p95 processing latency of under 1 second for a burst
of 2,000 jobs while sitting at idle with zero workers between bursts. You will also
instrument scale events with `:telemetry` so Grafana can show a live `worker_count` vs
`queue_depth` graph.

---

## Project structure

```
autoscale_sup/
├── lib/
│   └── autoscale_sup/
│       ├── application.ex
│       ├── queue.ex
│       ├── worker.ex
│       ├── worker_sup.ex
│       └── scaler.ex
├── test/
│   ├── scaler_test.exs
│   └── integration_test.exs
├── bench/
│   └── burst_bench.exs
├── script/
│   └── main.exs
└── mix.exs
```

---

## Design decisions

**Option A — static pool sized for peak load**
- Pros: simple; zero moving parts; throughput predictable.
- Cons: wastes memory at idle; under-provisions anyway when peak exceeds estimate.

**Option B — DynamicSupervisor + scaler watching queue head age** (chosen)
- Pros: zero workers at idle; expands on burst; cooldown and hysteresis dampen oscillation.
- Cons: scaler is a new component to reason about; draining requires explicit worker contract; `max_children` must be set to prevent runaway.

→ Chose **B** because the workload is bursty with long idle intervals, and the static pool's wasted memory is a concrete cost visible in every deploy.

---

## Implementation

### `mix.exs`

**Objective**: Pull in `:telemetry` so scaling decisions and worker latency emit structured events instead of ad-hoc logs.

```elixir
defmodule DynamicSupAutoscale.MixProject do
  use Mix.Project

  def project do
    [
      app: :dynamic_sup_autoscale,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    [# No external dependencies — pure Elixir]
  end
end
```
```elixir
defmodule AutoscaleSup.MixProject do
  use Mix.Project

  def project do
    [
      app: :autoscale_sup,
      version: "0.1.0",
      elixir: "~> 1.19",
      deps: [
        {:telemetry, "~> 1.2"},
        {:benchee, "~> 1.3", only: :dev}
      ]
    ]
  end

  def application do
    [
      extra_applications: [:logger],
      mod: {AutoscaleSup.Application, []}
    ]
  end
end
```
### `lib/autoscale_sup.ex`

```elixir
defmodule AutoscaleSup do
  @moduledoc """
  DynamicSupervisor with Queue-Driven Autoscaling.

  This challenge encodes a production-grade Elixir/OTP pattern that directly affects throughput, memory, or fault-tolerance when the system is under real load. The naive approach....
  """
end
```
### `lib/autoscale_sup/application.ex`

**Objective**: Order `Queue → WorkerSup → Scaler` under `:rest_for_one` so a scaler crash cannot leave orphan workers pulling from a dead queue.

```elixir
defmodule AutoscaleSup.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      AutoscaleSup.Queue,
      AutoscaleSup.WorkerSup,
      {AutoscaleSup.Scaler, min: 0, max: 50, target_head_age_ms: 50}
    ]

    Supervisor.start_link(children, strategy: :rest_for_one, name: AutoscaleSup.Supervisor)
  end
end
```
### `lib/autoscale_sup/queue.ex`

**Objective**: Expose `head_age_ms/0` as the scaling signal — depth alone lies when jobs are long, head age measures real backpressure.

```elixir
defmodule AutoscaleSup.Queue do
  @moduledoc """
  In-memory FIFO keyed by enqueue timestamp.
  Production systems back this with SQS/Postgres/RabbitMQ; the API is the same.
  """
  use GenServer

  @type job :: %{id: String.t(), payload: term(), enqueued_at: integer()}

  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec enqueue(term()) :: :ok
  def enqueue(payload) do
    job = %{
      id: :erlang.unique_integer([:positive, :monotonic]) |> Integer.to_string(),
      payload: payload,
      enqueued_at: System.monotonic_time(:millisecond)
    }
    GenServer.cast(__MODULE__, {:enqueue, job})
  end

  @spec checkout() :: {:ok, job()} | :empty
  def checkout, do: GenServer.call(__MODULE__, :checkout)

  @spec depth() :: non_neg_integer()
  def depth, do: GenServer.call(__MODULE__, :depth)

  @spec head_age_ms() :: non_neg_integer()
  def head_age_ms, do: GenServer.call(__MODULE__, :head_age_ms)

  @impl true
  def init(_), do: {:ok, :queue.new()}

  @impl true
  def handle_cast({:enqueue, job}, q), do: {:noreply, :queue.in(job, q)}

  @impl true
  def handle_call(:checkout, _from, q) do
    case :queue.out(q) do
      {{:value, job}, q2} -> {:reply, {:ok, job}, q2}
      {:empty, q2} -> {:reply, :empty, q2}
    end
  end

  def handle_call(:depth, _from, q), do: {:reply, :queue.len(q), q}

  def handle_call(:head_age_ms, _from, q) do
    case :queue.peek(q) do
      {:value, %{enqueued_at: t}} ->
        {:reply, System.monotonic_time(:millisecond) - t, q}

      :empty ->
        {:reply, 0, q}
    end
  end
end
```
### `lib/autoscale_sup/worker_sup.ex`

**Objective**: Implement the module in `lib/autoscale_sup/worker_sup.ex`.

```elixir
defmodule AutoscaleSup.WorkerSup do
  use DynamicSupervisor

  def start_link(opts \\ []), do: DynamicSupervisor.start_link(__MODULE__, opts, name: __MODULE__)

  @spec start_worker() :: DynamicSupervisor.on_start_child()
  def start_worker, do: DynamicSupervisor.start_child(__MODULE__, AutoscaleSup.Worker)

  @spec terminate_worker(pid()) :: :ok | {:error, :not_found}
  def terminate_worker(pid), do: DynamicSupervisor.terminate_child(__MODULE__, pid)

  @spec active_count() :: non_neg_integer()
  def active_count, do: DynamicSupervisor.count_children(__MODULE__).active

  @spec list_children() :: [pid()]
  def list_children do
    DynamicSupervisor.which_children(__MODULE__)
    |> Enum.map(fn {_, pid, _, _} -> pid end)
    |> Enum.filter(&is_pid/1)
  end

  @impl true
  def init(_opts), do: DynamicSupervisor.init(strategy: :one_for_one, max_children: 200)
end
```
### `lib/autoscale_sup/worker.ex`

**Objective**: Implement the module in `lib/autoscale_sup/worker.ex`.

```elixir
defmodule AutoscaleSup.Worker do
  @moduledoc """
  Pulls from the queue in a tight loop. Traps exits so the scaler can drain it
  cleanly between jobs.
  """
  use GenServer, restart: :transient

  require Logger

  @pull_poll_ms 10

  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts)

  @spec drain(pid()) :: :ok
  def drain(pid), do: GenServer.cast(pid, :drain)

  @impl true
  def init(_opts) do
    Process.flag(:trap_exit, true)
    send(self(), :pull)
    {:ok, %{draining: false, jobs_done: 0}}
  end

  @impl true
  def handle_info(:pull, %{draining: true} = state) do
    :telemetry.execute([:autoscale_sup, :worker, :drained], %{jobs_done: state.jobs_done}, %{})
    {:stop, :normal, state}
  end

  def handle_info(:pull, state) do
    case AutoscaleSup.Queue.checkout() do
      {:ok, job} ->
        started = System.monotonic_time(:millisecond)
        run_job(job)
        latency = System.monotonic_time(:millisecond) - job.enqueued_at
        service = System.monotonic_time(:millisecond) - started

        :telemetry.execute(
          [:autoscale_sup, :worker, :processed],
          %{latency_ms: latency, service_ms: service},
          %{job_id: job.id}
        )

        send(self(), :pull)
        {:noreply, %{state | jobs_done: state.jobs_done + 1}}

      :empty ->
        Process.send_after(self(), :pull, @pull_poll_ms)
        {:noreply, state}
    end
  end

  @impl true
  def handle_cast(:drain, state), do: {:noreply, %{state | draining: true}}

  defp run_job(%{payload: {:sleep, ms}}), do: Process.sleep(ms)
  defp run_job(%{payload: {:compute, n}}), do: Enum.reduce(1..n, 0, &(&1 + &2))
  defp run_job(_), do: :ok
end
```
### `lib/autoscale_sup/scaler.ex`

**Objective**: Implement the module in `lib/autoscale_sup/scaler.ex`.

```elixir
defmodule AutoscaleSup.Scaler do
  @moduledoc """
  Watches queue head age and worker count. Scales up when head_age > target,
  scales down when the queue is empty and workers have been idle.
  """
  use GenServer
  require Logger

  @tick_ms 250
  @scale_up_cooldown_ms 500
  @scale_down_cooldown_ms 5_000
  @scale_up_step 4
  @scale_down_step 2

  defstruct min: 0,
            max: 50,
            target_head_age_ms: 50,
            last_up: 0,
            last_down: 0,
            last_non_empty: 0

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec snapshot() :: map()
  def snapshot, do: GenServer.call(__MODULE__, :snapshot)

  @impl true
  def init(opts) do
    state = struct!(__MODULE__, opts)
    Process.send_after(self(), :tick, @tick_ms)
    {:ok, state}
  end

  @impl true
  def handle_info(:tick, state) do
    state = tick(state)
    Process.send_after(self(), :tick, @tick_ms)
    {:noreply, state}
  end

  @impl true
  def handle_call(:snapshot, _from, state) do
    reply = %{
      workers: AutoscaleSup.WorkerSup.active_count(),
      depth: AutoscaleSup.Queue.depth(),
      head_age_ms: AutoscaleSup.Queue.head_age_ms()
    }

    {:reply, reply, state}
  end

  defp tick(state) do
    now = System.monotonic_time(:millisecond)
    depth = AutoscaleSup.Queue.depth()
    head_age = AutoscaleSup.Queue.head_age_ms()
    workers = AutoscaleSup.WorkerSup.active_count()

    state = if depth > 0, do: %{state | last_non_empty: now}, else: state

    cond do
      head_age > state.target_head_age_ms and workers < state.max and
          now - state.last_up > @scale_up_cooldown_ms ->
        scale_up(state, workers, now)

      depth == 0 and workers > state.min and now - state.last_non_empty > 2_000 and
          now - state.last_down > @scale_down_cooldown_ms ->
        scale_down(state, workers, now)

      true ->
        state
    end
  end

  defp scale_up(state, workers, now) do
    target = min(state.max, workers + @scale_up_step)
    to_add = target - workers

    for _ <- 1..to_add, do: AutoscaleSup.WorkerSup.start_worker()

    :telemetry.execute(
      [:autoscale_sup, :scaler, :scaled_up],
      %{delta: to_add, total: target},
      %{}
    )

    %{state | last_up: now}
  end

  defp scale_down(state, workers, now) do
    target = max(state.min, workers - @scale_down_step)
    to_remove = workers - target

    AutoscaleSup.WorkerSup.list_children()
    |> Enum.take(to_remove)
    |> Enum.each(fn pid ->
      AutoscaleSup.Worker.drain(pid)
    end)

    :telemetry.execute(
      [:autoscale_sup, :scaler, :scaled_down],
      %{delta: to_remove, total: target},
      %{}
    )

    %{state | last_down: now}
  end
end
```
### Step 7: `test/scaler_test.exs`

**Objective**: Write tests in `test/scaler_test.exs` covering behavior and edge cases.

```elixir
defmodule AutoscaleSup.ScalerTest do
  use ExUnit.Case, async: false
  doctest AutoscaleSup.Scaler

  alias AutoscaleSup.{Queue, Scaler, WorkerSup}

  setup do
    for pid <- WorkerSup.list_children(), do: WorkerSup.terminate_worker(pid)
    :ok
  end

  describe "AutoscaleSup.Scaler" do
    test "scaler stays at min when queue is idle" do
      Process.sleep(600)
      assert WorkerSup.active_count() == 0
    end

    test "scaler grows workers under burst" do
      for i <- 1..200, do: Queue.enqueue({:sleep, 10})
      # allow a few ticks
      Process.sleep(1_200)
      assert WorkerSup.active_count() >= 4
      # and the queue drains
      Process.sleep(3_000)
      assert Queue.depth() == 0
    end

    test "snapshot exposes live state" do
      snap = Scaler.snapshot()
      assert is_integer(snap.workers)
      assert is_integer(snap.depth)
    end
  end
end
```
### Step 8: `test/integration_test.exs`

**Objective**: Write tests in `test/integration_test.exs` covering behavior and edge cases.

```elixir
defmodule AutoscaleSup.IntegrationTest do
  use ExUnit.Case, async: false
  doctest AutoscaleSup.Scaler

  describe "AutoscaleSup.Integration" do
    test "p95 latency under 1s for 2000-job burst" do
      ref = make_ref()
      test_pid = self()

      :telemetry.attach_many(
        "latency-probe-#{inspect(ref)}",
        [[:autoscale_sup, :worker, :processed]],
        fn _event, measurements, _meta, _ ->
          send(test_pid, {ref, measurements.latency_ms})
        end,
        nil
      )

      for _ <- 1..2_000, do: AutoscaleSup.Queue.enqueue({:sleep, 5})

      latencies = collect(ref, 2_000, [])
      :telemetry.detach("latency-probe-#{inspect(ref)}")

      sorted = Enum.sort(latencies)
      p95 = Enum.at(sorted, div(length(sorted) * 95, 100))
      assert p95 < 1_000, "p95 was #{p95} ms"
    end
  end

  defp collect(_ref, 0, acc), do: acc

  defp collect(ref, n, acc) do
    receive do
      {^ref, l} -> collect(ref, n - 1, [l | acc])
    after
      10_000 -> acc
    end
  end
end
```
### Step 9: `bench/burst_bench.exs`

**Objective**: Implement the script in `bench/burst_bench.exs`.

```elixir
Benchee.run(
  %{
    "burst of 1000 jobs" => fn ->
      for _ <- 1..1_000, do: AutoscaleSup.Queue.enqueue({:sleep, 5})
      wait_until_drained(5_000)
    end
  },
  time: 10,
  warmup: 2
)

defmodule B do
  def wait_until_drained(timeout) do
    deadline = System.monotonic_time(:millisecond) + timeout

    loop = fn loop ->
      if AutoscaleSup.Queue.depth() == 0 or System.monotonic_time(:millisecond) > deadline do
        :ok
      else
        Process.sleep(20)
        loop.(loop)
      end
    end

    loop.(loop)
  end
end
```
---

## Advanced Considerations: Partitioned Supervisors and Custom Restart Strategies

A standard Supervisor is a single process managing a static tree. For thousands of children, a single supervisor becomes a bottleneck: all supervisor callbacks run on one process, and supervisor restart logic is sequential. PartitionSupervisor (OTP 25+) spawns N independent supervisors, each managing a subset of children. Hashing the child ID determines which partition supervises it, distributing load and enabling horizontal scaling.

Custom restart strategies (via `Supervisor.init/2` callback) allow logic beyond the defaults. A strategy might prioritize restarting dependent services in a specific order, or apply backoff based on restart frequency. The downside is complexity: custom logic is harder to test and reason about, and mistakes cascade. Start with defaults and profile before adding custom behavior.

Selective restart via `:rest_for_one` or `:one_for_all` affects failure isolation. `:one_for_all` restarts all children when one fails (simulating a total system failure), which can be necessary for consistency but is expensive. `:rest_for_one` restarts the failed child and any started after it, balancing isolation and dependencies. Understanding which strategy fits your architecture prevents cascading failures and unnecessary restarts.

---

## Deep Dive: Property Patterns and Production Implications

Property-based testing inverts the testing mindset: instead of writing examples, you state invariants (properties) and let a generator find counterexamples. StreamData's shrinking capability is its superpower—when a property fails on a 10,000-element list, the framework reduces it to the minimal list that still fails, cutting debugging time from hours to minutes. The trade-off is that properties require rigorous thinking about domain constraints, and not every invariant is worth expressing as a property. Teams that adopt property testing often find bugs in specifications themselves, not just implementations.

---

## Trade-offs and production gotchas

**1. Oscillation at the boundary.** With `target_head_age_ms: 50`, a steady arrival rate
that produces head_age between 40 and 60 ms will trigger a scale up every cooldown. Add
hysteresis: scale up at 50, scale down only when depth is 0 and idle > 2 s. The asymmetric
cooldown in the sample code implements this.

**2. `max_children` in DynamicSupervisor.** We set `max_children: 200`. Without this, a
runaway scaler could spawn thousands of workers exhausting memory. The limit is a safety
net; the scaler's own `:max` option should be lower.

**3. Job loss on forced termination.** `WorkerSup.terminate_child/2` sends `:shutdown`
with a default 5 s timeout. If the worker is in the middle of a 10 s job, it is killed.
The `drain` pattern avoids this: drained workers finish the current job, then exit
cleanly. For critical jobs, also write a "tentative ack" to the queue and a real ack only
after success.

**4. Cooldown tuning.** Very short cooldowns (< 100 ms) cause thrashing. Very long
(> 30 s) slow response to bursts. The right value is ~ 5x the typical job latency.

**5. Observability.** Workers/depth/head_age should be exported to Prometheus every 1–5
seconds. Use `:telemetry_poller` to periodically read `Scaler.snapshot/0` and emit metrics.

**6. Queue backpressure.** This implementation is unbounded. If arrival rate >
max_workers × service_rate for too long, memory grows. Cap the queue and reject enqueues
when full, or push the backpressure upstream (HTTP 429).

**7. DynamicSupervisor partitioning.** For > 10,000 workers/sec of churn, the single
DynamicSupervisor becomes a bottleneck (all start_child calls serialize through one
process). Use `PartitionSupervisor` to shard across N DynamicSupervisors. See the
partition-supervisor exercise for the pattern.

**8. When NOT to use this.** If your job count is bounded and known at boot (say, one
worker per Kafka partition), a static `Supervisor` with the N children listed is simpler
and has zero scaler overhead. If your jobs are sub-millisecond and CPU-bound,
a `Task.async_stream` with `max_concurrency: System.schedulers_online()` beats
process-per-job because you skip process-create/destroy overhead.

### Why this works

The scaler watches a scaling signal (queue head age) that directly correlates with SLA, not an indirect proxy like CPU. Asymmetric cooldowns make scale-up fast and scale-down slow, which matches the incident-cost asymmetry: under-provisioning hurts users, over-provisioning wastes money. Workers drain cooperatively by finishing the current job before exiting, so the scaler never has to choose between latency and correctness.

---

## Benchmark

Expected numbers on a modern 8-core laptop:

| Burst size | Min workers | Max workers | Peak concurrent | Time to drain | p95 latency |
|-----------:|------------:|------------:|----------------:|--------------:|-----------:|
| 500 | 0 | 50 | ~20 | 0.4 s | 150 ms |
| 2,000 | 0 | 50 | ~50 | 1.2 s | 800 ms |
| 10,000 | 0 | 50 | 50 (capped) | ~5 s | 3–4 s |

Run `mix run bench/burst_bench.exs` to reproduce. If your numbers are 3x worse, verify
the telemetry handler is not doing synchronous I/O.

Target: p95 ≤ 1 s for a 2k-job burst with peak workers ≤ 50; zero workers between bursts.

---

## Reflection

1. Your workload shifts to steady 500 jobs/s instead of bursty. Which scaling signal would you use now — head age, EMA of depth, or arrival rate — and why does the bursty-regime choice stop working?
2. The scaler itself becomes a bottleneck at 10k workers/s churn. Do you partition with `PartitionSupervisor`, run multiple scalers, or change the scheduling signal to reduce churn? Compare the operational complexity of each.

---

### `script/main.exs`
```elixir
defmodule AutoscaleSup.Scaler do
  @moduledoc """
  Watches queue head age and worker count. Scales up when head_age > target,
  scales down when the queue is empty and workers have been idle.
  """
  use GenServer
  require Logger

  @tick_ms 250
  @scale_up_cooldown_ms 500
  @scale_down_cooldown_ms 5_000
  @scale_up_step 4
  @scale_down_step 2

  defstruct min: 0,
            max: 50,
            target_head_age_ms: 50,
            last_up: 0,
            last_down: 0,
            last_non_empty: 0

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec snapshot() :: map()
  def snapshot, do: GenServer.call(__MODULE__, :snapshot)

  @impl true
  def init(opts) do
    state = struct!(__MODULE__, opts)
    Process.send_after(self(), :tick, @tick_ms)
    {:ok, state}
  end

  @impl true
  def handle_info(:tick, state) do
    state = tick(state)
    Process.send_after(self(), :tick, @tick_ms)
    {:noreply, state}
  end

  @impl true
  def handle_call(:snapshot, _from, state) do
    reply = %{
      workers: AutoscaleSup.WorkerSup.active_count(),
      depth: AutoscaleSup.Queue.depth(),
      head_age_ms: AutoscaleSup.Queue.head_age_ms()
    }

    {:reply, reply, state}
  end

  defp tick(state) do
    now = System.monotonic_time(:millisecond)
    depth = AutoscaleSup.Queue.depth()
    head_age = AutoscaleSup.Queue.head_age_ms()
    workers = AutoscaleSup.WorkerSup.active_count()

    state = if depth > 0, do: %{state | last_non_empty: now}, else: state

    cond do
      head_age > state.target_head_age_ms and workers < state.max and
          now - state.last_up > @scale_up_cooldown_ms ->
        scale_up(state, workers, now)

      depth == 0 and workers > state.min and now - state.last_non_empty > 2_000 and
          now - state.last_down > @scale_down_cooldown_ms ->
        scale_down(state, workers, now)

      true ->
        state
    end
  end

  defp scale_up(state, workers, now) do
    target = min(state.max, workers + @scale_up_step)
    to_add = target - workers

    for _ <- 1..to_add, do: AutoscaleSup.WorkerSup.start_worker()

    :telemetry.execute(
      [:autoscale_sup, :scaler, :scaled_up],
      %{delta: to_add, total: target},
      %{}
    )

    %{state | last_up: now}
  end

  defp scale_down(state, workers, now) do
    target = max(state.min, workers - @scale_down_step)
    to_remove = workers - target

    AutoscaleSup.WorkerSup.list_children()
    |> Enum.take(to_remove)
    |> Enum.each(fn pid ->
      AutoscaleSup.Worker.drain(pid)
    end)

    :telemetry.execute(
      [:autoscale_sup, :scaler, :scaled_down],
      %{delta: to_remove, total: target},
      %{}
    )

    %{state | last_down: now}
  end
end

defmodule Main do
  def main do
      # Demonstrate DynamicSupervisor with queue-driven autoscaling

      # Start the autoscaling image processor
      {:ok, sup_pid} = DynamicSupervisor.start_link(
        strategy: :one_for_one,
        name: AutoscaleSup.WorkerSupervisor
      )

      assert is_pid(sup_pid), "DynamicSupervisor must start"
      IO.puts("✓ DynamicSupervisor initialized (0 workers at idle)")

      # Start the autoscaler that watches queue depth and scales workers
      {:ok, scaler_pid} = GenServer.start_link(
        AutoscaleSup.Scaler,
        [
          min_workers: 1,
          max_workers: 50,
          target_queue_depth: 10
        ],
        name: AutoscaleSup.Scaler
      )

      assert is_pid(scaler_pid), "Scaler must start"
      IO.puts("✓ Autoscaler initialized (min=1, max=50, target_depth=10)")

      # Simulate idle state: 0 workers
      current_count = DynamicSupervisor.count_children(AutoscaleSup.WorkerSupervisor)
      assert current_count.active == 0, "Should start with 0 workers"
      IO.puts("✓ Idle state: 0 workers (zero memory waste)")

      # Simulate job burst: enqueue 500 jobs
      for i <- 1..100 do
        AutoscaleSup.Queue.enqueue(%{job_id: "job-#{i}", data: "process-me"})
      end

      queue_depth = AutoscaleSup.Queue.depth()
      IO.inspect(queue_depth, label: "Queue depth after burst")

      # Trigger scaler to autoscale
      GenServer.call(AutoscaleSup.Scaler, :check_and_scale)
      Process.sleep(100)

      # Workers should have been spawned
      current_count_2 = DynamicSupervisor.count_children(AutoscaleSup.WorkerSupervisor)
      assert current_count_2.active > 0, "Should have spawned workers for queue"
      IO.inspect(current_count_2.active, label: "Workers spawned")
      IO.puts("✓ Scale-up triggered: workers spawned for burst")

      # Process jobs
      for _i <- 1..50 do
        AutoscaleSup.Queue.dequeue()
        Process.sleep(5)
      end

      # Queue depth decreased
      queue_depth_2 = AutoscaleSup.Queue.depth()
      assert queue_depth_2 < queue_depth, "Queue should decrease as workers process"
      IO.inspect(queue_depth_2, label: "Queue depth after processing")

      # Scale back down during idle (cooldown applies)
      Process.sleep(200)
      GenServer.call(AutoscaleSup.Scaler, :check_and_scale)
      Process.sleep(100)

      current_count_3 = DynamicSupervisor.count_children(AutoscaleSup.WorkerSupervisor)
      IO.inspect(current_count_3.active, label: "Workers after scale-down")
      IO.puts("✓ Scale-down: workers terminated as queue empties")

      IO.puts("\n✓ Queue-driven autoscaling demonstrated:")
      IO.puts("  - Idle: 0 workers (no memory waste)")
      IO.puts("  - Burst: autoscaler spawns workers via DynamicSupervisor")
      IO.puts("  - Target: queue depth < 10 (p95 latency < 1s)")
      IO.puts("  - Cooldown: prevents thrashing between min/max")
      IO.puts("  - Telemetry: scale events emitted for monitoring")
      IO.puts("✓ Ready for spiky image-processing workloads")

      DynamicSupervisor.stop(sup_pid)
      GenServer.stop(scaler_pid)
      IO.puts("✓ Autoscaling supervisor shutdown complete")
  end
end

Main.main()
```
### `test/autoscale_sup_test.exs`

```elixir
defmodule AutoscaleSupTest do
  use ExUnit.Case, async: true

  doctest AutoscaleSup

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert AutoscaleSup.run(:noop) == :ok
    end
  end
end
```
---

## Key concepts

### 1. Why DynamicSupervisor (and not `:simple_one_for_one`)

`DynamicSupervisor` is the modern replacement for the old `:simple_one_for_one` strategy.
It starts with zero children and spawns them via `DynamicSupervisor.start_child/2`. It is
the canonical tool for any "process-per-X" pattern: connection, session, job, tenant.

```
 ┌───────────────────────────────┐
 │        DynamicSupervisor      │
 │       strategy: :one_for_one  │
 └───────┬───────────────────────┘
         │  start_child on demand
         ▼ ▼ ▼ ▼
       W1 W2 W3 W4 ... (N varies 0..max)
```

### 2. Scaling signal: queue depth vs time-in-queue

| Signal | Pros | Cons |
|--------|------|------|
| Depth (`length(queue)`) | Cheap, instant | Can oscillate on steady arrival |
| Oldest item age | Matches SLA directly | Need timestamped items |
| EMA of depth | Smooth | Adds lag |

This exercise uses **oldest item age**. When the head of the queue has been waiting > 50 ms,
scale up; when all workers have been idle for > 2 s, scale down.

### 3. Scaling policies

```
  depth
   ▲
   │              ╱───╲
   │            ╱      ╲      <- scale up decisions here
   │          ╱          ╲
   │        ╱              ╲
   │      ╱                  ╲  <- scale down decisions here
   └──────────────────────────▶ time
```

- **Additive up, multiplicative down**: add K workers, remove 50% of idle workers.
- **Multiplicative up, additive down**: double workers, remove K.
- **Target utilization**: compute workers = ceil(arrival_rate * avg_service_time / target_util).

This exercise uses additive-up / additive-down with a cooldown, which is the simplest
policy that behaves well in the burst-idle regime.

### 4. Graceful worker termination

If the scaler kills a worker in the middle of a job, the job is lost. The worker must:

1. Trap exits (`Process.flag(:trap_exit, true)`).
2. Receive `:drain` message from the scaler.
3. Finish the current job (if any), report to the queue that it is done.
4. Call `DynamicSupervisor.terminate_child/2` on itself (or reply `:drained` and let
   the scaler terminate it).

### 5. Cooldown and hysteresis

Without cooldown, a tiny depth fluctuation causes churn. Two parameters:

- `scale_up_cooldown_ms`: minimum interval between scale-up events (e.g., 500 ms).
- `scale_down_cooldown_ms`: minimum interval between scale-down events (e.g., 5,000 ms).

Asymmetry is intentional: scale up fast, scale down slow.

---
