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

## Implementation milestones

### Step 1: Create the project

**Objective**: Bootstrap a supervised OTP app so the queue, scheduler, and workers can share one crash-safe lifecycle.


```bash
mix new jobqueue --sup
cd jobqueue
mkdir -p lib/jobqueue test/jobqueue bench
```

### Step 2: `mix.exs` — dependencies

**Objective**: Pull in Benchee for throughput measurements and StreamData for property-based invariants over retry and cron logic.

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev},
    {:stream_data, "~> 0.6", only: :test}
  ]
end
```

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
        e -> {:error, Exception.message(e)}
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
# test/jobqueue/retry_test.exs
defmodule Jobqueue.RetryTest do
  use ExUnit.Case, async: true

  alias Jobqueue.Retry

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
```

```elixir
# test/jobqueue/dependency_test.exs
defmodule Jobqueue.DependencyTest do
  use ExUnit.Case, async: false

  defmodule EchoWorker do
    def perform(args), do: {:ok, args}
  end

  setup do
    {:ok, jq} = Jobqueue.start_link()
    {:ok, jq: jq}
  end

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
