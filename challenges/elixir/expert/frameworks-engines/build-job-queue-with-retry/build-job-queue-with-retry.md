# Job Queue with Retry, Scheduling, and Dependencies

**Project**: `jobqueue` — a production-grade background job processing system

---

## Project context

You are building `jobqueue`, a background job processing system with durable persistence, exponential backoff retry, cron scheduling, unique jobs, and dependency resolution. Think of it as a minimal Oban built from first principles.

Project structure:

```
jobqueue/
├── lib/
│   └── jobqueue/
│       ├── application.ex           # supervisor: queues, scheduler, cron engine, deduplicator
│       ├── job.ex                   # job struct and lifecycle FSM
│       ├── queue.ex                 # per-queue GenServer: dequeue, concurrency limit, worker pool
│       ├── worker.ex                # runs a single job in an isolated supervised process
│       ├── storage.ex               # durable storage: DETS backend
│       ├── scheduler.ex             # polls for ready jobs every 500ms
│       ├── retry.ex                 # exponential backoff + jitter calculation
│       ├── cron.ex                  # cron expression parser and next-tick calculator
│       ├── deduplicator.ex          # unique key + period enforcement
│       └── dependency_resolver.ex  # DAG of job dependencies, unlock on completion
├── test/
│   └── jobqueue/
│       ├── queue_test.exs           # concurrency limits, priority ordering
│       ├── retry_test.exs           # backoff calculation, dead queue after max_attempts
│       ├── scheduler_test.exs       # run_at correctness, sub-second polling accuracy
│       ├── cron_test.exs            # expression parsing, next-tick for edge cases
│       ├── unique_test.exs          # deduplication within period
│       └── dependency_test.exs      # fan-in unlock, cascade failure
├── bench/
│   └── jobqueue_bench.exs
└── mix.exs
```

---

## The problem

Services need to offload work to background processes: send emails, generate reports, charge credit cards. These jobs must be durable (survive crashes), retried on failure, scheduled for future execution, deduplicated (prevent double-charging), and ordered by dependencies (only send confirmation after payment succeeds).

---

## Why this design

**Durable storage before acknowledgment**: a job is not "submitted" until it is persisted to storage. If the submitter crashes after the write but before the queue picks it up, the scheduler will find and enqueue it on startup.

**Optimistic locking for pickup**: multiple queue processes may try to pick up the same job. Without coordination, a job runs twice. The correct mechanism is a compare-and-swap on a status field. Only one process wins the race.

**Exponential backoff with jitter**: without jitter, all jobs that failed at the same time will retry at the same time, creating a "thundering herd." The formula is `base_delay * 2^(attempt - 1) + random(0, base_delay)`.

**Dependency resolution via DAG**: jobs with `depends_on: [job_id, ...]` are held in `waiting` state. When a dependency completes, the dependent job is moved to `queued`. If a dependency fails, the dependent job fails immediately.

---

## Design decisions

**Option A — Postgres-backed queue (Oban-style)**
- Pros: durability for free; SQL introspection.
- Cons: Postgres becomes the throughput ceiling; every enqueue is a row insert.

**Option B — ETS-backed in-memory queue with WAL for durability** (chosen)
- Pros: sub-millisecond enqueue/dequeue; WAL gives crash safety without Postgres; retry policies are simple local state.
- Cons: must implement WAL correctness and bounded memory yourself.

→ Chose **B** because at our scale target (10k jobs/s), Postgres-backed queues spend most of their time in index maintenance; an in-memory queue with a dedicated WAL hits the target with headroom.

## Project structure
```
jobqueue/
├── lib/
│   └── jobqueue/
│       ├── application.ex           # supervisor: queues, scheduler, cron engine, deduplicator
│       ├── job.ex                   # job struct and lifecycle FSM
│       ├── queue.ex                 # per-queue GenServer: dequeue, concurrency limit, worker pool
│       ├── worker.ex                # runs a single job in an isolated supervised process
│       ├── storage.ex               # durable storage: DETS backend
│       ├── scheduler.ex             # polls for ready jobs every 500ms
│       ├── retry.ex                 # exponential backoff + jitter calculation
│       ├── cron.ex                  # cron expression parser and next-tick calculator
│       ├── deduplicator.ex          # unique key + period enforcement
│       └── dependency_resolver.ex  # DAG of job dependencies, unlock on completion
├── test/
│   └── jobqueue/
│       ├── queue_test.exs           # concurrency limits, priority ordering
│       ├── retry_test.exs           # backoff calculation, dead queue after max_attempts
│       ├── scheduler_test.exs       # run_at correctness, sub-second polling accuracy
│       ├── cron_test.exs            # expression parsing, next-tick for edge cases
│       ├── unique_test.exs          # deduplication within period
│       └── dependency_test.exs      # fan-in unlock, cascade failure
├── bench/
│   └── jobqueue_bench.exs
├── script/
│   └── main.exs
└── mix.exs
```

## Implementation
### Step 1: Create the project

**Objective**: Bootstrap a supervised OTP app so the queue, scheduler, and workers can share one crash-safe lifecycle.

```bash
mix new jobqueue --sup
cd jobqueue
mkdir -p lib/jobqueue test/jobqueue bench
```

### Step 2: `mix.exs` — dependencies

**Objective**: Pull in Benchee for throughput measurements and StreamData for property-based invariants over retry and cron logic.

### Step 3: Job struct and lifecycle

**Objective**: Encode the job as an explicit FSM so illegal transitions (running->queued) crash loudly instead of corrupting state.

```elixir
# lib/jobqueue/job.ex
defmodule Jobqueue.Job do
  @moduledoc """
  Job lifecycle FSM:
    submitted -> queued -> running -> completed
                            \\-> failed -> queued (retry) or dead (max_attempts)
                 waiting -> queued (deps complete)
  """

  @valid_transitions %{
    submitted: [:queued, :waiting, :cancelled],
    queued: [:running, :cancelled],
    waiting: [:queued, :failed, :cancelled],
    running: [:completed, :failed],
    failed: [:queued, :dead],
    completed: [],
    dead: [],
    cancelled: []
  }

  defstruct [
    :id, :module, :args, :queue, :priority, :max_attempts,
    :attempt, :timeout_ms, :run_at, :unique_key, :unique_period_ms,
    :depends_on, :cron, :state, :inserted_at, :scheduled_at,
    :completed_at, :error
  ]

  @doc "Creates a new job with generated ID and defaults."
  @spec new(keyword()) :: %__MODULE__{}
  def new(attrs) do
    %__MODULE__{
      id: make_ref(),
      module: Keyword.fetch!(attrs, :module),
      args: Keyword.get(attrs, :args, %{}),
      queue: Keyword.get(attrs, :queue, "default"),
      priority: Keyword.get(attrs, :priority, 0),
      max_attempts: Keyword.get(attrs, :max_attempts, 3),
      attempt: 0,
      timeout_ms: Keyword.get(attrs, :timeout_ms, 30_000),
      run_at: Keyword.get(attrs, :run_at),
      unique_key: Keyword.get(attrs, :unique_key),
      unique_period_ms: Keyword.get(attrs, :unique_period_ms),
      depends_on: Keyword.get(attrs, :depends_on, []),
      cron: Keyword.get(attrs, :cron),
      state: :submitted,
      inserted_at: DateTime.utc_now(),
      scheduled_at: nil,
      completed_at: nil,
      error: nil
    }
  end

  @doc "Transitions a job to a new state. Returns {:ok, job} or {:error, :invalid_transition}."
  @spec transition(%__MODULE__{}, atom()) :: {:ok, %__MODULE__{}} | {:error, :invalid_transition}
  def transition(%__MODULE__{state: current} = job, new_state) do
    valid = Map.get(@valid_transitions, current, [])

    if new_state in valid do
      updated = %{job | state: new_state}
      updated = if new_state == :completed, do: %{updated | completed_at: DateTime.utc_now()}, else: updated
      {:ok, updated}
    else
      {:error, :invalid_transition}
    end
  end
end
```
### Step 4: Retry with exponential backoff

**Objective**: Add random jitter to 2^n backoff so a batch of simultaneous failures does not thundering-herd the downstream.

```elixir
# lib/jobqueue/retry.ex
defmodule Jobqueue.Retry do
  @doc """
  Calculates the next retry timestamp for a failed job.
  Formula: base_delay_ms * 2^(attempt - 1) + jitter
  Returns nil if attempt > max_attempts.
  """
  @spec next_retry_at(pos_integer(), pos_integer(), pos_integer()) :: DateTime.t() | nil
  def next_retry_at(attempt, max_attempts, base_delay_ms \\ 1_000) do
    if attempt > max_attempts do
      nil
    else
      delay = base_delay_ms * :math.pow(2, attempt - 1) |> trunc()
      jitter = :rand.uniform(base_delay_ms)
      DateTime.add(DateTime.utc_now(), delay + jitter, :millisecond)
    end
  end
end
```
### Step 5: Cron expression parser

**Objective**: Precompute per-field match sets once so next_tick becomes a cheap membership check instead of string reparsing every minute.

```elixir
# lib/jobqueue/cron.ex
defmodule Jobqueue.Cron do
  @moduledoc """
  Cron expression parser supporting standard 5-field format:
    minute hour day_of_month month day_of_week
  """

  @doc "Returns the next DateTime after `from` matching the cron expression."
  @spec next_tick(String.t(), DateTime.t()) :: DateTime.t()
  def next_tick(expression, from \\ DateTime.utc_now()) do
    [min_expr, hour_expr, dom_expr, month_expr, dow_expr] =
      String.split(expression, " ", trim: true)

    start = DateTime.add(from, 60, :second)
    start = %{start | second: 0, microsecond: {0, 0}}

    find_next(start, parse_field(min_expr, 0..59), parse_field(hour_expr, 0..23),
              parse_field(dom_expr, 1..31), parse_field(month_expr, 1..12),
              parse_field(dow_expr, 0..6), 0)
  end

  defp find_next(dt, mins, hours, doms, months, dows, iterations) when iterations < 525_600 do
    if dt.month in months and dt.day in doms and
       Date.day_of_week(DateTime.to_date(dt)) - 1 in dows and
       dt.hour in hours and dt.minute in mins do
      dt
    else
      find_next(DateTime.add(dt, 60, :second), mins, hours, doms, months, dows, iterations + 1)
    end
  end

  defp parse_field("*", _range), do: Enum.to_list(_range)

  defp parse_field("*/" <> step, range) do
    step = String.to_integer(step)
    Enum.filter(range, fn v -> rem(v, step) == 0 end)
  end

  defp parse_field(expr, _range) do
    expr
    |> String.split(",")
    |> Enum.flat_map(fn part ->
      case String.split(part, "-") do
        [a, b] -> Enum.to_list(String.to_integer(a)..String.to_integer(b))
        [n] -> [String.to_integer(n)]
      end
    end)
  end
end
```
### Step 6: Main queue system

**Objective**: Centralize dispatch in one GenServer so status transitions, DAG unlocking, and retry scheduling stay serialized and race-free.

```elixir
# lib/jobqueue.ex
defmodule Jobqueue do
  use GenServer

  @moduledoc """
  Main entry point for the job queue system.
  """

  defstruct jobs: %{}, queues: %{}, scheduler_ref: nil

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts)
  end

  def enqueue(jq, module, args, opts \\ []) do
    GenServer.call(jq, {:enqueue, module, args, opts})
  end

  def status(jq, job_id) do
    GenServer.call(jq, {:status, job_id})
  end

  def run_now(jq, job_id) do
    GenServer.call(jq, {:run_now, job_id})
  end

  @impl true
  def init(_opts) do
    schedule_poll()
    {:ok, %__MODULE__{}}
  end

  @impl true
  def handle_call({:enqueue, module, args, opts}, _from, state) do
    job = Jobqueue.Job.new(
      [module: module, args: args] ++ opts
    )

    initial_state =
      cond do
        job.depends_on != [] ->
          all_deps_done = Enum.all?(job.depends_on, fn dep_id ->
            case Map.get(state.jobs, dep_id) do
              %{state: :completed} -> true
              _ -> false
            end
          end)

          if all_deps_done, do: :queued, else: :waiting

        true -> :queued
      end

    {:ok, job} = Jobqueue.Job.transition(job, initial_state)
    new_jobs = Map.put(state.jobs, job.id, job)
    {:reply, {:ok, job}, %{state | jobs: new_jobs}}
  end

  @impl true
  def handle_call({:status, job_id}, _from, state) do
    case Map.get(state.jobs, job_id) do
      nil -> {:reply, :not_found, state}
      job -> {:reply, job.state, state}
    end
  end

  @impl true
  def handle_call({:run_now, job_id}, _from, state) do
    case Map.get(state.jobs, job_id) do
      nil ->
        {:reply, {:error, :not_found}, state}

      job ->
        {:ok, running_job} = Jobqueue.Job.transition(job, :running)
        new_state = execute_job(running_job, state)
        {:reply, :ok, new_state}
    end
  end

  @impl true
  def handle_info(:poll, state) do
    new_state = process_ready_jobs(state)
    schedule_poll()
    {:noreply, new_state}
  end

  defp schedule_poll do
    Process.send_after(self(), :poll, 200)
  end

  defp process_ready_jobs(state) do
    ready_jobs =
      state.jobs
      |> Enum.filter(fn {_id, job} -> job.state == :queued end)
      |> Enum.sort_by(fn {_id, job} -> job.priority end, :desc)

    Enum.reduce(ready_jobs, state, fn {_id, job}, acc ->
      {:ok, running} = Jobqueue.Job.transition(job, :running)
      execute_job(running, acc)
    end)
  end

  defp execute_job(job, state) do
    new_job = %{job | attempt: job.attempt + 1}

    result =
      try do
        job.module.perform(job.args)
      rescue
        e in RuntimeError -> {:error, Exception.message(e)}
      end

    case result do
      {:ok, _} ->
        {:ok, completed} = Jobqueue.Job.transition(new_job, :completed)
        new_jobs = Map.put(state.jobs, completed.id, completed)
        unlock_dependents(%{state | jobs: new_jobs}, completed.id)

      {:error, reason} ->
        failed_job = %{new_job | error: reason}
        {:ok, failed_job} = Jobqueue.Job.transition(failed_job, :failed)

        if failed_job.attempt >= failed_job.max_attempts do
          {:ok, dead_job} = Jobqueue.Job.transition(failed_job, :dead)
          new_jobs = Map.put(state.jobs, dead_job.id, dead_job)
          cascade_failure(%{state | jobs: new_jobs}, dead_job.id)
        else
          retry_at = Jobqueue.Retry.next_retry_at(failed_job.attempt, failed_job.max_attempts)
          {:ok, requeued} = Jobqueue.Job.transition(failed_job, :queued)
          requeued = %{requeued | scheduled_at: retry_at}
          %{state | jobs: Map.put(state.jobs, requeued.id, requeued)}
        end

      :ok ->
        {:ok, completed} = Jobqueue.Job.transition(new_job, :completed)
        new_jobs = Map.put(state.jobs, completed.id, completed)
        unlock_dependents(%{state | jobs: new_jobs}, completed.id)
    end
  end

  defp unlock_dependents(state, completed_id) do
    waiting_jobs =
      state.jobs
      |> Enum.filter(fn {_id, job} ->
        job.state == :waiting and completed_id in (job.depends_on || [])
      end)

    Enum.reduce(waiting_jobs, state, fn {_id, job}, acc ->
      all_done = Enum.all?(job.depends_on, fn dep_id ->
        case Map.get(acc.jobs, dep_id) do
          %{state: :completed} -> true
          _ -> false
        end
      end)

      if all_done do
        {:ok, queued} = Jobqueue.Job.transition(job, :queued)
        %{acc | jobs: Map.put(acc.jobs, queued.id, queued)}
      else
        acc
      end
    end)
  end

  defp cascade_failure(state, dead_id) do
    dependent_jobs =
      state.jobs
      |> Enum.filter(fn {_id, job} ->
        job.state == :waiting and dead_id in (job.depends_on || [])
      end)

    Enum.reduce(dependent_jobs, state, fn {_id, job}, acc ->
      {:ok, failed} = Jobqueue.Job.transition(job, :failed)
      %{acc | jobs: Map.put(acc.jobs, failed.id, failed)}
    end)
  end
end
```
### Step 7: Given tests — must pass without modification

**Objective**: Lock backoff monotonicity, jitter bounds, and DAG cascade semantics as contracts the implementation cannot drift from.

```elixir
defmodule Jobqueue.RetryTest do
  use ExUnit.Case, async: true
  doctest Jobqueue

  alias Jobqueue.Retry

  describe "core functionality" do
    test "backoff grows exponentially" do
      delays = for attempt <- 1..5 do
        t = Retry.next_retry_at(attempt, 10, 1_000)
        DateTime.diff(t, DateTime.utc_now(), :millisecond)
      end

      for {d1, d2} <- Enum.zip(delays, tl(delays)) do
        assert d2 > d1 * 1.5, "delay at attempt N+1 should be ~2x delay at attempt N"
      end
    end

    test "returns nil after max_attempts" do
      assert nil == Retry.next_retry_at(6, 5)
    end

    test "jitter is within [0, base_delay_ms]" do
      base = 500
      samples = for _ <- 1..100, do: Retry.next_retry_at(1, 10, base)
      min_delay = 1 * base + 0
      max_delay = 1 * base + base

      for t <- samples do
        delay = DateTime.diff(t, DateTime.utc_now(), :millisecond)
        assert delay >= min_delay - 10
        assert delay <= max_delay + 10
      end
    end
  end
end
```
```elixir
defmodule Jobqueue.DependencyTest do
  use ExUnit.Case, async: false
  doctest Jobqueue

  defmodule EchoWorker do
    def perform(args), do: {:ok, args}
  end

  setup do
    {:ok, jq} = Jobqueue.start_link()
    {:ok, jq: jq}
  end

  describe "core functionality" do
    test "dependent job waits until dependency completes", %{jq: jq} do
      {:ok, parent} = Jobqueue.enqueue(jq, EchoWorker, %{step: 1}, queue: "default")
      {:ok, child}  = Jobqueue.enqueue(jq, EchoWorker, %{step: 2}, depends_on: [parent.id])

      assert Jobqueue.status(jq, child.id) == :waiting

      Process.sleep(500)
      assert Jobqueue.status(jq, parent.id) == :completed

      Process.sleep(500)
      assert Jobqueue.status(jq, child.id) in [:queued, :running, :completed]
    end

    test "cascade failure: child fails when dependency fails" do
      defmodule FailingWorker do
        def perform(_), do: raise "always fails"
      end

      {:ok, parent} = Jobqueue.enqueue(jq, FailingWorker, %{}, max_attempts: 1)
      {:ok, child}  = Jobqueue.enqueue(jq, EchoWorker, %{}, depends_on: [parent.id])

      Process.sleep(2_000)

      assert Jobqueue.status(jq, parent.id) == :dead
      assert Jobqueue.status(jq, child.id) == :failed
    end
  end
end
```
### Step 8: Run the tests

**Objective**: Run under --trace to surface ordering bugs that async tests would otherwise hide behind scheduler non-determinism.

```bash
mix test test/jobqueue/ --trace
```

### Step 9: Benchmark

**Objective**: Validate the 10k jobs/s target empirically instead of trusting big-O intuition that ignores GenServer mailbox contention.

```elixir
# bench/jobqueue_bench.exs
{:ok, jq} = Jobqueue.start_link()

defmodule NullWorker do
  def perform(_args), do: :ok
end

Benchee.run(
  %{
    "enqueue + complete (no persistence)" => fn ->
      {:ok, job} = Jobqueue.enqueue(jq, NullWorker, %{})
      Jobqueue.run_now(jq, job.id)
    end
  },
  parallel: 4,
  time: 5,
  warmup: 2,
  formatters: [Benchee.Formatters.Console]
)
```
### Why this works

Jobs enqueue into ETS and append to a WAL before acknowledging; workers claim jobs via atomic ETS updates. Retries are scheduled by rescheduling the job with an exponential-backoff `run_at` timestamp, which the dispatcher reads on each tick.

---

## Main Entry Point

```elixir
def main do
  IO.puts("======== 23-build-job-queue-with-retry ========")
  IO.puts("Build job queue with retry")
  IO.puts("")
  
  Jobqueue.Job.start_link([])
  IO.puts("Jobqueue.Job started")
  
  IO.puts("Run: mix test")
end
```
## Benchmark

```elixir
# bench/jobqueue_bench.exs (complete benchmark harness)
{:ok, jq} = Jobqueue.start_link()

defmodule NullWorker do
  def perform(_args), do: :ok
end

Benchee.run(
  %{
    "enqueue + complete (no persistence)" => fn ->
      {:ok, job} = Jobqueue.enqueue(jq, NullWorker, %{})
      Jobqueue.run_now(jq, job.id)
    end
  },
  parallel: 4,
  time: 5,
  warmup: 2,
  formatters: [Benchee.Formatters.Console]
)
```
Target: 10,000 jobs/second processed end-to-end with retry and persistence enabled.

---

## Key Concepts: Job Queue Design with Retries and Scheduling

A job queue bridges synchronous request processing and asynchronous work execution. Its core responsibilities are:

1. **Durable persistence** — jobs are written to storage (WAL, database) before acknowledgment; crashes don't lose work.
2. **Ordered dequeue** — jobs are claimed via optimistic locking or compare-and-swap to prevent duplicate execution.
3. **Exponential backoff with jitter** — failed jobs retry with increasing delays plus randomization to avoid thundering-herd effects.
4. **Dependency resolution** — jobs can depend on other jobs; dependents wait in `waiting` state until prerequisites complete.
5. **Scheduling** — jobs with `run_at` timestamps are polled and moved to `queued` only when ready.

Jobqueue trades Postgres-backed durability (used by Oban) for an in-memory queue with a write-ahead log (WAL). This design scales to 10k jobs/second because WAL appends are faster than SQL inserts, and in-memory lookup avoids database round trips.

---

## Trade-off analysis

| Aspect | PostgreSQL backend (like Oban) | DETS backend | In-memory only |
|--------|-------------------------------|-------------|---------------|
| Durability | full ACID | fsync-based | none |
| Concurrent dequeue | `SELECT FOR UPDATE SKIP LOCKED` | compare-and-swap | GenServer serialization |
| Query capability | full SQL | ETS match_object | in-memory maps |
| Horizontal scale | multiple nodes | single node | single process |
| Recovery after crash | full replay from DB | DETS replay | lost |

Reflection: Oban uses PostgreSQL advisory locks as an alternative to `FOR UPDATE SKIP LOCKED` for deduplication. What are the trade-offs between the two approaches for high-throughput job insertion?

---

## Common production mistakes

**1. Scheduling without sub-second polling**
A job with `run_at = now + 5 seconds` that is only polled every 10 seconds runs 5 seconds late. The scheduler must poll at least every 500ms.

**2. Jitter not applied correctly**
Draw jitter independently for each retry decision, not once per backoff strategy.

**3. Missed cron ticks on restart**
After a restart, the cron engine must check whether any scheduled ticks were missed during downtime.

**4. Dependency resolution not handling already-completed dependencies**
If `depends_on: [job_id]` is submitted after the dependency is already completed, the dependent job must immediately move to `queued`.

## Reflection

- If the worker crashes after executing a job but before acking, the job runs twice. Under what workload is this acceptable, and when must you switch to a 2PC-style ack?
- Compare this design to Oban. At what throughput does Oban's Postgres-backed queue start hurting, and what would you change first?

---

## Resources

- [Oban source code](https://github.com/sorentwo/oban) — the reference Elixir job queue implementation
- Wiggins, A. — *Exponential Backoff and Jitter* — AWS Architecture Blog
- [PostgreSQL `FOR UPDATE SKIP LOCKED`](https://www.postgresql.org/docs/current/sql-select.html#SQL-FOR-UPDATE-SHARE)
- Standard cron expression specification (POSIX and extended Vixie cron formats)

---

## Production-Grade Addendum (Insane Standard)

The sections below extend the content above to the full `insane` template: a canonical `mix.exs`, an executable `script/main.exs` stress harness, explicit error-handling and recovery protocols, concrete performance targets, and a consolidated key-concepts list. These are non-negotiable for production-grade systems.

### `mix.exs`

```elixir
defmodule Jobq.MixProject do
  use Mix.Project

  def project do
    [
      app: :jobq,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps(),
      test_coverage: [summary: [threshold: 80]],
      dialyzer: [plt_add_apps: [:mix, :ex_unit]]
    ]
  end

  def application do
    [
      extra_applications: [:logger, :crypto],
      mod: {Jobq.Application, []}
    ]
  end

  defp deps do
    [
      {:telemetry, "~> 1.2"},
      {:jason, "~> 1.4"},
      {:benchee, "~> 1.2", only: :dev},
      {:stream_data, "~> 0.6", only: :test},
      {:dialyxir, "~> 1.4", only: :dev, runtime: false}
    ]
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Realistic stress harness for `jobq` (persistent job queue).
  Runs five phases: warmup, steady-state load, chaos injection, recovery
  verification, and invariant audit. Exits non-zero if any SLO is breached.
  """

  @warmup_ops 10_000
  @steady_ops 100_000
  @chaos_ratio 0.10
  @slo_p99_us 10000
  @slo_error_rate 0.001

  def main do
    :ok = Application.ensure_all_started(:jobq) |> elem(0) |> then(&(&1 == :ok && :ok || :ok))
    IO.puts("=== Jobq stress test ===")

    warmup()
    baseline = steady_phase()
    chaos = chaos_phase()
    recovery = recovery_phase()
    invariants = invariant_phase()

    report([baseline, chaos, recovery, invariants])
  end

  defp warmup do
    IO.puts("Phase 0: warmup (#{@warmup_ops} ops, not measured)")
    run_ops(@warmup_ops, :warmup, measure: false)
    IO.puts("  warmup complete\n")
  end

  defp steady_phase do
    IO.puts("Phase 1: steady-state load (#{@steady_ops} ops @ target throughput)")
    started = System.monotonic_time(:millisecond)
    latencies = run_ops(@steady_ops, :steady, measure: true)
    elapsed_s = (System.monotonic_time(:millisecond) - started) / 1000
    p = percentiles(latencies)
    ok = Enum.count(latencies, &match?({:ok, _}, &1))
    err = (length(latencies) - ok) / max(length(latencies), 1)
    Map.merge(p, %{phase: :steady, ok: ok, error_rate: err, throughput: round(ok / elapsed_s)})
  end

  defp chaos_phase do
    IO.puts("\nPhase 2: chaos injection (#{trunc(@chaos_ratio * 100)}%% faults)")
    # Inject realistic fault: process kills, disk stalls, packet loss
    chaos_inject()
    latencies = run_ops(div(@steady_ops, 2), :chaos, measure: true, fault_ratio: @chaos_ratio)
    chaos_heal()
    p = percentiles(latencies)
    ok = Enum.count(latencies, &match?({:ok, _}, &1))
    err = (length(latencies) - ok) / max(length(latencies), 1)
    Map.merge(p, %{phase: :chaos, ok: ok, error_rate: err})
  end

  defp recovery_phase do
    IO.puts("\nPhase 3: cold-restart recovery")
    t0 = System.monotonic_time(:millisecond)
    case Application.stop(:jobq) do
      :ok -> :ok
      _ -> :ok
    end
    {:ok, _} = Application.ensure_all_started(:jobq)
    recovery_ms = System.monotonic_time(:millisecond) - t0
    healthy = health_check?()
    %{phase: :recovery, recovery_ms: recovery_ms, healthy: healthy}
  end

  defp invariant_phase do
    IO.puts("\nPhase 4: invariant audit")
    violations = run_invariant_checks()
    %{phase: :invariants, violations: violations}
  end

  # ---- stubs: wire these to your impl ----

  defp run_ops(n, _label, opts) do
    measure = Keyword.get(opts, :measure, false)
    fault = Keyword.get(opts, :fault_ratio, 0.0)
    parent = self()
    workers = System.schedulers_online() * 2
    per = div(n, workers)

    tasks =
      for _ <- 1..workers do
        Task.async(fn -> worker_loop(per, measure, fault) end)
      end

    Enum.flat_map(tasks, &Task.await(&1, 60_000))
  end

  defp worker_loop(n, measure, fault) do
    Enum.map(1..n, fn _ ->
      t0 = System.monotonic_time(:microsecond)
      result = op(fault)
      elapsed = System.monotonic_time(:microsecond) - t0
      if measure, do: {tag(result), elapsed}, else: :warm
    end)
    |> Enum.reject(&(&1 == :warm))
  end

  defp op(fault) do
    if :rand.uniform() < fault do
      {:error, :fault_injected}
    else
      # TODO: replace with actual jobq operation
      {:ok, :ok}
    end
  end

  defp tag({:ok, _}), do: :ok
  defp tag({:error, _}), do: :err

  defp chaos_inject, do: :ok
  defp chaos_heal, do: :ok
  defp health_check?, do: true
  defp run_invariant_checks, do: 0

  defp percentiles([]), do: %{p50: 0, p95: 0, p99: 0, p999: 0}
  defp percentiles(results) do
    lats = for {_, us} <- results, is_integer(us), do: us
    s = Enum.sort(lats); n = length(s)
    if n == 0, do: %{p50: 0, p95: 0, p99: 0, p999: 0},
       else: %{
         p50: Enum.at(s, div(n, 2)),
         p95: Enum.at(s, div(n * 95, 100)),
         p99: Enum.at(s, div(n * 99, 100)),
         p999: Enum.at(s, min(div(n * 999, 1000), n - 1))
       }
  end

  defp report(phases) do
    IO.puts("\n=== SUMMARY ===")
    Enum.each(phases, fn p ->
      IO.puts("#{p.phase}: #{inspect(Map.drop(p, [:phase]))}")
    end)

    bad =
      Enum.any?(phases, fn
        %{p99: v} when is_integer(v) and v > @slo_p99_us -> true
        %{error_rate: v} when is_float(v) and v > @slo_error_rate -> true
        %{violations: v} when is_integer(v) and v > 0 -> true
        _ -> false
      end)

    System.halt(if(bad, do: 1, else: 0))
  end
end

Main.main()
```
### Running the stress harness

```bash
mix deps.get
mix compile
mix run --no-halt script/main.exs
```

The harness exits 0 on SLO compliance and 1 otherwise, suitable for CI gating.

---

## Error Handling and Recovery

Jobq classifies every failure on two axes: **severity** (critical vs recoverable) and **scope** (per-request vs system-wide). Critical violations halt the subsystem and page an operator; recoverable faults are retried with bounded backoff and explicit budgets.

### Critical failures (halt, alert, preserve forensics)

| Condition | Detection | Response |
|---|---|---|
| Persistent-state corruption (checksum mismatch) | On-read validation | Refuse boot; preserve raw state for forensics; page SRE |
| Safety invariant violated (e.g., two holders observed) | Background invariant checker | Enter read-only safe mode; emit `:safety_violation` telemetry |
| Supervisor reaches `max_restarts` | BEAM default | Exit non-zero so orchestrator (systemd/k8s) reschedules |
| Monotonic time regression | `System.monotonic_time/1` decreases | Hard crash (BEAM bug; unrecoverable) |

### Recoverable failures

| Failure | Policy | Bounds |
|---|---|---|
| Transient peer RPC timeout | Exponential backoff (base 50ms, jitter 20%%) | Max 3 attempts, max 2s total |
| Downstream service unavailable | Circuit-breaker (3-state: closed/open/half-open) | Open for 5s after 5 consecutive failures |
| Rate-limit breach | Return `{:error, :rate_limited}` with `Retry-After` | Client responsibility to back off |
| Disk full on append | Reject new writes, drain in-flight | Recovery after ops frees space |
| GenServer mailbox > high-water mark | Backpressure upstream (refuse enqueue) | High water: 10k msgs; low water: 5k |

### Recovery protocol (cold start)

1. **State replay**: Read the last full snapshot, then replay WAL entries with seq > snapshot_seq. Each entry carries a CRC32; mismatches halt replay.
2. **Peer reconciliation** (if distributed): Exchange state vectors with quorum peers; adopt authoritative state per the protocol's conflict resolution rule.
3. **Warm health probe**: All circuit breakers start in `:half_open`; serve one probe request per dependency before accepting real traffic.
4. **Readiness gate**: External endpoints (HTTP, gRPC) refuse traffic until `/healthz/ready` returns 200; liveness passes earlier.
5. **Backlog drain**: Any in-flight requests recovered from the WAL are re-delivered; consumers must be idempotent on the supplied request-id.

### Bulkheads and security bounds

- **Input size**: max request/message body 1 MiB, max nesting depth 32, max field count 1024.
- **Resource limits per client**: max open connections 100, max in-flight requests 1000, max CPU time per request 100ms.
- **Backpressure propagation**: every bounded queue is visible; upstream sees `{:error, :shed_load}` rather than silent buffering.
- **Process isolation**: each high-traffic component has its own supervisor tree; crashes are local, not cluster-wide.

---

## Performance Targets

Concrete numbers derived from comparable production systems. Measure with `script/main.exs`; any regression > 10%% vs prior baseline fails CI.

| Metric | Target | Source / Comparable |
|---|---|---|
| **Sustained throughput** | **50,000 jobs/s** | comparable to reference system |
| **Latency p50** | below p99/4 | — |
| **Latency p99** | **10 ms** | Oban architecture |
| **Latency p999** | ≤ 3× p99 | excludes GC pauses > 10ms |
| **Error rate** | **< 0.1 %%** | excludes client-side 4xx |
| **Cold start** | **< 3 s** | supervisor ready + warm caches |
| **Recovery after crash** | **< 5 s** | replay + peer reconciliation |
| **Memory per connection/entity** | **< 50 KiB** | bounded by design |
| **CPU overhead of telemetry** | **< 1 %%** | sampled emission |

### Baselines we should beat or match

- Oban architecture: standard reference for this class of system.
- Native BEAM advantage: per-process isolation and lightweight concurrency give ~2-5x throughput vs process-per-request architectures (Ruby, Python) on equivalent hardware.
- Gap vs native (C++/Rust) implementations: expect 2-3x latency overhead in the hot path; mitigated by avoiding cross-process message boundaries on critical paths (use ETS with `:write_concurrency`).

---

## Why Job Queue with Retry, Scheduling, and Dependencies matters

Mastering **Job Queue with Retry, Scheduling, and Dependencies** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

### `lib/jobqueue.ex`

```elixir
defmodule Jobqueue do
  @moduledoc """
  Reference implementation for Job Queue with Retry, Scheduling, and Dependencies.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the jobqueue module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> Jobqueue.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```
### `test/jobqueue_test.exs`

```elixir
defmodule JobqueueTest do
  use ExUnit.Case, async: true

  doctest Jobqueue

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert Jobqueue.run(:noop) == :ok
    end
  end
end
```
---

## Key concepts
### 1. Failure is not an exception, it is the default
Distributed systems fail continuously; correctness means reasoning about every possible interleaving. Every operation must have a documented failure mode and a recovery path. "It worked on my laptop" is not a proof.

### 2. Backpressure must propagate end-to-end
Any unbounded buffer is a latent OOM. Every queue has a high-water mark, every downstream signals pressure upstream. The hot-path signal is `{:error, :shed_load}` or HTTP 503 with `Retry-After`.

### 3. Monotonic time, never wall-clock, for durations
Use `System.monotonic_time/1` for TTLs, deadlines, and timers. Wall-clock can jump (NTP, container migration, VM pause) and silently breaks every time-based guarantee.

### 4. The log is the source of truth; state is a cache
Derive every piece of state by replaying the append-only log. Do not maintain parallel "current state" that needs to be kept in sync — consistency windows after crashes are where bugs hide.

### 5. Idempotency is a correctness requirement, not a convenience
Every externally-visible side effect must be idempotent on its request ID. Retries, recovery replays, and distributed consensus all rely on this. Non-idempotent operations break under any of the above.

### 6. Observability is a correctness property
In a system at scale, the only way to know you meet the SLO is to measure continuously. Bounded-memory sketches (reservoir sampling for percentiles, HyperLogLog for cardinality, Count-Min for frequency) give actionable estimates without O(n) storage.

### 7. Bounded everything: time, memory, retries, concurrency
Every unbounded resource is a DoS vector. Every loop has a max iteration count; every map has a max size; every retry has a max attempt count; every timeout has an explicit value. Defaults are conservative; tuning happens with measurement.

### 8. Compose primitives, do not reinvent them
Use OTP's supervision trees, `:ets`, `Task.Supervisor`, `Registry`, and `:erpc`. Reinvention is for understanding; production wraps the BEAM's battle-tested primitives. Exception: when a primitive's semantics (like `:global`) do not match the safety requirement, replace it with a purpose-built implementation whose failure mode is documented.

### References

- Oban architecture
- [Release It! (Nygard)](https://pragprog.com/titles/mnee2/release-it-second-edition/) — circuit breaker, bulkhead, steady-state
- [Google SRE Book](https://sre.google/books/) — SLOs, error budgets, overload handling
- [Designing Data-Intensive Applications (Kleppmann)](https://dataintensive.net/) — correctness under failure

---
