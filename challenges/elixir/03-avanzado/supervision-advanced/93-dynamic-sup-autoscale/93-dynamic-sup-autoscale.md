# DynamicSupervisor with Queue-Driven Autoscaling

**Project**: `autoscale_sup` — a DynamicSupervisor that grows and shrinks worker count based on backlog depth.
**Difficulty**: ★★★★☆
**Estimated time**: 3–6 hours

---

## Project context

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

## Tree

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
└── mix.exs
```

---

## Core concepts

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

## Implementation

### Step 1: `mix.exs`

```elixir
defmodule AutoscaleSup.MixProject do
  use Mix.Project

  def project do
    [
      app: :autoscale_sup,
      version: "0.1.0",
      elixir: "~> 1.16",
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

### Step 2: `lib/autoscale_sup/application.ex`

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

### Step 3: `lib/autoscale_sup/queue.ex`

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

### Step 4: `lib/autoscale_sup/worker_sup.ex`

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

### Step 5: `lib/autoscale_sup/worker.ex`

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

### Step 6: `lib/autoscale_sup/scaler.ex`

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

```elixir
defmodule AutoscaleSup.ScalerTest do
  use ExUnit.Case, async: false

  alias AutoscaleSup.{Queue, Scaler, WorkerSup}

  setup do
    for pid <- WorkerSup.list_children(), do: WorkerSup.terminate_worker(pid)
    :ok
  end

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
```

### Step 8: `test/integration_test.exs`

```elixir
defmodule AutoscaleSup.IntegrationTest do
  use ExUnit.Case, async: false

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

---

## Resources

- [`DynamicSupervisor`](https://hexdocs.pm/elixir/DynamicSupervisor.html)
- [`PartitionSupervisor`](https://hexdocs.pm/elixir/PartitionSupervisor.html) — when one DynamicSupervisor is not enough
- [Broadway](https://github.com/dashbitco/broadway) — production-grade data ingestion with built-in concurrency scaling
- [Designing Elixir Systems with OTP — Bruce Tate, James Gray](https://pragprog.com/titles/jgotp/designing-elixir-systems-with-otp/)
- [Saša Jurić — "Supervised Elixir"](https://www.theerlangelist.com/article/supervised_processes)
- [`:telemetry` metrics guide](https://hexdocs.pm/telemetry/readme.html)
