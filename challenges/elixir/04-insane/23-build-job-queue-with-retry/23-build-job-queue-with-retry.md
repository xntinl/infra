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
│       ├── storage.ex               # durable storage: PostgreSQL or DETS backend
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

Services need to offload work to background processes: send emails, generate reports, charge credit cards. These jobs must be durable (survive crashes), retried on failure, scheduled for future execution, deduplicated (prevent double-charging), and ordered by dependencies (only send confirmation after payment succeeds). Each requirement adds complexity; the interaction between them is the hard part.

---

## Why this design

**Durable storage before acknowledgment**: a job is not "submitted" until it is persisted to storage. If the submitter crashes after the write but before the queue picks it up, the scheduler will find and enqueue it on startup. This is the fundamental contract: durability comes from the storage layer, not from the process that holds the queue.

**Optimistic locking for pickup**: multiple queue processes may try to pick up the same job. Without coordination, a job runs twice. The correct mechanism is `SELECT ... FOR UPDATE SKIP LOCKED` (PostgreSQL) or a compare-and-swap on a status field (DETS). Only one process wins the race; others skip to the next available job.

**Exponential backoff with jitter**: without jitter, all jobs that failed at the same time will retry at the same time, creating a "thundering herd" that overwhelms the downstream service again. Jitter adds a random component to the retry delay, spreading the load. The formula is `base_delay * 2^(attempt - 1) + random(0, base_delay)`.

**Dependency resolution via DAG**: jobs with `depends_on: [job_id, ...]` are held in `waiting` state. When a dependency completes, the dependent job is moved to `queued`. If a dependency fails, the dependent job fails immediately (cascade). Track the dependency DAG in the storage layer, not in memory.

---

## Implementation milestones

### Step 1: Create the project

```bash
mix new jobqueue --sup
cd jobqueue
mkdir -p lib/jobqueue test/jobqueue bench
```

### Step 2: `mix.exs` — dependencies

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev},
    {:stream_data, "~> 0.6", only: :test}
  ]
end
```

### Step 3: Job struct and lifecycle

```elixir
# lib/jobqueue/job.ex
defmodule Jobqueue.Job do
  @moduledoc """
  Job lifecycle:

    submitted → queued → running → completed
                         ↘ failed → queued (retry if attempts < max_attempts)
                                  → dead (max_attempts reached)
                       ↓
                    waiting (has unresolved dependencies)
                       ↓ (on all deps complete)
                    queued

  Cancellation: from submitted, queued, or waiting → cancelled.
  Running jobs cannot be cancelled without process termination.
  """

  @states [:submitted, :queued, :waiting, :running, :completed, :failed, :dead, :cancelled]

  defstruct [
    :id,
    :module,
    :args,
    :queue,
    :priority,
    :max_attempts,
    :attempt,
    :timeout_ms,
    :run_at,
    :unique_key,
    :unique_period_ms,
    :depends_on,
    :cron,
    :state,
    :inserted_at,
    :scheduled_at,
    :completed_at,
    :error
  ]

  # TODO: new/1 — generates UUID, sets defaults, validates required fields
  # TODO: transition/2 — valid state transitions only; returns {:ok, job} or {:error, :invalid}
end
```

### Step 4: Retry with exponential backoff

```elixir
# lib/jobqueue/retry.ex
defmodule Jobqueue.Retry do
  @doc """
  Calculates the next retry timestamp for a failed job.

  Formula: base_delay_ms * 2^(attempt - 1) + jitter
  where jitter is uniform random in [0, base_delay_ms].

  Returns nil if attempt > max_attempts (job should move to :dead).
  """
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

```elixir
# lib/jobqueue/cron.ex
defmodule Jobqueue.Cron do
  @moduledoc """
  Cron expression parser supporting standard 5-field format:
    minute hour day_of_month month day_of_week
    e.g. "0 9 * * 1-5" = 9am every weekday

  Each field supports:
    * = any value
    N = specific value
    N-M = range
    */N = every N
    N,M = list
  """

  @doc "Returns the next DateTime after `from` matching the cron expression."
  def next_tick(expression, from \\ DateTime.utc_now()) do
    # TODO: parse expression, find next matching datetime
    # HINT: iterate minutes from `from + 1 minute`, check each against parsed fields
    # HINT: add special handling for day_of_week vs day_of_month (both must match if specified)
  end
end
```

### Step 6: Given tests — must pass without modification

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

    # Each delay should be roughly double the previous (ignoring jitter)
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
    min_delay = 1 * base + 0      # min: no jitter
    max_delay = 1 * base + base   # max: full jitter

    for t <- samples do
      delay = DateTime.diff(t, DateTime.utc_now(), :millisecond)
      assert delay >= min_delay - 10  # small tolerance for execution time
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

    # Child must be in :waiting state while parent is pending
    assert Jobqueue.status(jq, child.id) == :waiting

    # Let parent complete
    Process.sleep(500)
    assert Jobqueue.status(jq, parent.id) == :completed

    # Child should now be :queued or :completed
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

### Step 7: Run the tests

```bash
mix test test/jobqueue/ --trace
```

### Step 8: Benchmark

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
    end,
    "scheduler poll — 10k ready jobs" => fn ->
      Jobqueue.Scheduler.poll(jq)
    end
  },
  parallel: 4,
  time: 5,
  warmup: 2,
  formatters: [Benchee.Formatters.Console]
)
```

---

## Trade-off analysis

| Aspect | PostgreSQL backend (like Oban) | DETS backend (this project) | In-memory only |
|--------|-------------------------------|----------------------------|---------------|
| Durability | full ACID | fsync-based | none |
| Concurrent dequeue | `SELECT FOR UPDATE SKIP LOCKED` | compare-and-swap | GenServer serialization |
| Query capability | full SQL (by queue, status, etc.) | ETS match_object | in-memory maps |
| Horizontal scale | multiple nodes competing for same DB | single node | single process |
| Recovery after crash | full replay from DB | DETS replay | lost |

Reflection: Oban uses PostgreSQL advisory locks as an alternative to `FOR UPDATE SKIP LOCKED` for deduplication. What are the trade-offs between the two approaches for high-throughput job insertion?

---

## Common production mistakes

**1. Scheduling without sub-second polling**
A job with `run_at = now + 5 seconds` that is only polled every 10 seconds runs 5 seconds late. The scheduler must poll at least every 500ms. Use `Process.send_after(self(), :poll, 500)` rescheduled at the end of each poll cycle.

**2. Jitter not applied correctly**
`base_delay * 2^attempt + jitter` where jitter is drawn once per backoff strategy (not per job instance) means all jobs with the same attempt count get the same jitter. Draw jitter independently for each retry decision.

**3. Missed cron ticks on restart**
After a restart, the cron engine must check whether any scheduled ticks were missed during downtime. Fire at most once per missed tick window (not once per missed tick) to avoid a burst of duplicate jobs on restart.

**4. Dependency resolution not handling already-completed dependencies**
If `depends_on: [job_id]` is submitted after the dependency is already completed, the dependent job must immediately move to `queued`. Check dependency status at submission time, not only when notified of completion.

---

## Resources

- [Oban source code](https://github.com/sorentwo/oban) — the reference Elixir job queue implementation; study the PostgreSQL backend and producer/consumer protocol
- Wiggins, A. — *Exponential Backoff and Jitter* — AWS Architecture Blog
- [PostgreSQL `FOR UPDATE SKIP LOCKED`](https://www.postgresql.org/docs/current/sql-select.html#SQL-FOR-UPDATE-SHARE) — the mechanism for scalable queue polling
- Standard cron expression specification (POSIX and extended Vixie cron formats)
