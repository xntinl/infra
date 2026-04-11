# 51. Build a Production Job Queue (Oban-like)

## Context

Your team runs a SaaS platform. Sending confirmation emails, generating PDF invoices, syncing user data to a CRM, and charging credit cards — all of these operations are triggered by HTTP requests but must not block the response. They must survive node crashes, retry on transient failures, and never execute more than once (or at most once more if the process died mid-execution).

The first attempt used an in-memory queue backed by a GenServer. When the node restarted, the queue was empty. Lost jobs meant unpaid invoices and unsent emails. The second attempt used Redis, but job enqueue and database insert were two separate operations: if the database rolled back after the Redis enqueue, a ghost job existed for data that was never committed.

The only correct foundation is PostgreSQL: job insertion is transactional with the business operation. If the outer transaction rolls back, the job does not exist. `SELECT FOR UPDATE SKIP LOCKED` ensures mutual exclusion at the database level without any in-memory coordination between nodes.

You will build `JobQueue`: a production-quality background job system backed by PostgreSQL.

## Why `FOR UPDATE SKIP LOCKED` and not advisory locks

PostgreSQL advisory locks are manual; the application must release them explicitly. If a worker crashes, the advisory lock is held until the session closes. `FOR UPDATE SKIP LOCKED` works at the row level: when a worker claims a job row, the row is locked for the duration of the worker's transaction. Other workers trying to claim the same job see it as locked and skip it (SKIP LOCKED). When the worker commits (marking the job `executing`), the lock is released and the job is no longer available for claiming. The lock lifetime equals the transaction duration — no manual cleanup.

## Why LISTEN/NOTIFY instead of polling-only

Polling at 1-second intervals adds up to 1 second of latency for every enqueued job. `NOTIFY` from a trigger fires immediately when a row is inserted. The LISTEN connection receives the notification within ~5ms on a local database. This reduces median latency from ~500ms (average of 1s interval) to ~10ms. Polling is the fallback when the NOTIFY connection drops — it handles missed notifications without data loss.

## Why at-least-once and not exactly-once

Exactly-once delivery requires distributed transactions or two-phase commit — prohibitively complex. At-least-once is achievable with idempotent workers: the job may execute more than once (if the worker dies after running `perform/1` but before marking the job `completed`), but if `perform/1` is idempotent (safe to run multiple times), the observable outcome is correct. The orphan rescue returns jobs to `available` if the worker dies, which may cause a second execution. This is the explicit design contract.

## Project Structure

```
job_queue/
├── mix.exs
├── priv/
│   └── repo/
│       └── migrations/
│           └── 20240101000000_create_jobs.exs
├── lib/
│   └── job_queue/
│       ├── job.ex              # Job schema: state machine, fields
│       ├── worker.ex           # Worker behaviour: perform/1, enqueue/2, new/2
│       ├── queue/
│       │   ├── supervisor.ex   # DynamicSupervisor for queues
│       │   ├── poller.ex       # GenServer: poll loop, SKIP LOCKED claim
│       │   ├── executor.ex     # Task.Supervisor: run perform/1, handle results
│       │   └── circuit_breaker.ex  # GenServer: failure rate, open/half_open/closed
│       ├── scheduler/
│       │   ├── cron.ex         # Cron expression parser and evaluator
│       │   └── cron_scheduler.ex   # GenServer: evaluate registered workers per minute
│       ├── plugins/
│       │   ├── orphan_rescue.ex    # Periodic scan for stuck executing jobs
│       │   ├── pruner.ex           # Periodic delete of terminal jobs past retention
│       │   └── notify_listener.ex  # LISTEN on jobs_available channel
│       ├── global_limiter.ex   # GenServer: global concurrency counter + waiting queue
│       ├── rate_limiter.ex     # GenServer: per-worker rate limit (token bucket)
│       └── telemetry.ex        # Event emission helpers
├── test/
│   ├── support/
│   │   └── test_worker.ex
│   ├── job_test.exs
│   ├── poller_test.exs
│   ├── orphan_rescue_test.exs
│   ├── uniqueness_test.exs
│   ├── cron_test.exs
│   └── integration/
│       ├── transactional_enqueue_test.exs
│       ├── at_least_once_test.exs
│       ├── concurrency_test.exs
│       └── distributed_test.exs   # 3-node test
└── bench/
    └── throughput.exs
```

## Step 1 — Database migration

```elixir
# priv/repo/migrations/20240101000000_create_jobs.exs
defmodule JobQueue.Repo.Migrations.CreateJobs do
  use Ecto.Migration

  def change do
    create table(:jobs) do
      add :queue, :string, null: false
      add :worker, :string, null: false
      add :args, :map, null: false, default: %{}
      add :meta, :map, null: false, default: %{}
      add :state, :string, null: false, default: "available"
      add :priority, :integer, null: false, default: 0
      add :attempt, :integer, null: false, default: 0
      add :max_attempts, :integer, null: false, default: 3
      add :errors, {:array, :map}, null: false, default: []
      add :scheduled_at, :utc_datetime_usec, null: false, default: fragment("now()")
      add :attempted_at, :utc_datetime_usec
      add :completed_at, :utc_datetime_usec
      add :discarded_at, :utc_datetime_usec
      add :cancelled_at, :utc_datetime_usec
      add :attempted_by, {:array, :string}, null: false, default: []
      add :unique_key, :string
      add :depends_on, {:array, :bigint}, null: false, default: []
      timestamps()
    end

    # Poller query index: available/retryable jobs ordered by priority, scheduled_at
    create index(:jobs, [:state, :queue, :priority, :scheduled_at],
      where: "state IN ('available', 'retryable')")

    # Uniqueness: partial index covering non-terminal states
    create unique_index(:jobs, [:unique_key],
      where: "state IN ('available', 'scheduled', 'executing', 'retryable') AND unique_key IS NOT NULL")

    # Orphan rescue: find executing jobs for heartbeat check
    create index(:jobs, [:state, :attempted_at], where: "state = 'executing'")

    # Pruner: find old terminal jobs
    create index(:jobs, [:state, :completed_at], where: "state = 'completed'")

    # Trigger for NOTIFY on insert
    execute """
    CREATE OR REPLACE FUNCTION notify_job_available() RETURNS trigger AS $$
    BEGIN
      PERFORM pg_notify('jobs_available', NEW.queue);
      RETURN NEW;
    END;
    $$ LANGUAGE plpgsql;
    """, "DROP FUNCTION IF EXISTS notify_job_available();"

    execute """
    CREATE TRIGGER jobs_notify_insert
      AFTER INSERT ON jobs
      FOR EACH ROW
      WHEN (NEW.state = 'available')
      EXECUTE FUNCTION notify_job_available();
    """, "DROP TRIGGER IF EXISTS jobs_notify_insert ON jobs;"
  end
end
```

## Step 2 — Job schema and state machine

```elixir
defmodule JobQueue.Job do
  use Ecto.Schema
  import Ecto.Changeset

  @states ~w(available scheduled executing completed retryable discarded cancelled)
  @terminal_states ~w(completed discarded cancelled)

  # Valid transitions:
  # available → executing | cancelled
  # scheduled → available | cancelled
  # executing → completed | retryable | discarded
  # retryable → executing | cancelled
  # (terminal states are irreversible)
  @transitions %{
    "available" => ~w(executing cancelled),
    "scheduled" => ~w(available cancelled),
    "executing" => ~w(completed retryable discarded),
    "retryable" => ~w(executing cancelled)
  }

  schema "jobs" do
    field :queue, :string
    field :worker, :string
    field :args, :map, default: %{}
    field :meta, :map, default: %{}
    field :state, :string, default: "available"
    field :priority, :integer, default: 0
    field :attempt, :integer, default: 0
    field :max_attempts, :integer, default: 3
    field :errors, {:array, :map}, default: []
    field :scheduled_at, :utc_datetime_usec
    field :attempted_at, :utc_datetime_usec
    field :completed_at, :utc_datetime_usec
    field :discarded_at, :utc_datetime_usec
    field :cancelled_at, :utc_datetime_usec
    field :attempted_by, {:array, :string}, default: []
    field :unique_key, :string
    field :depends_on, {:array, :integer}, default: []
    # Virtual: set when uniqueness conflict detected
    field :conflict, :boolean, virtual: true, default: false
    field :conflict_job_id, :integer, virtual: true
    timestamps()
  end

  def new_changeset(attrs) do
    %__MODULE__{}
    |> cast(attrs, [:queue, :worker, :args, :meta, :priority, :max_attempts,
                     :scheduled_at, :unique_key, :depends_on])
    |> validate_required([:queue, :worker])
    |> put_scheduled_at()
    |> put_state()
  end

  defp put_scheduled_at(changeset) do
    if get_field(changeset, :scheduled_at) do
      changeset
    else
      put_change(changeset, :scheduled_at, DateTime.utc_now())
    end
  end

  defp put_state(changeset) do
    scheduled_at = get_field(changeset, :scheduled_at)
    state = if scheduled_at && DateTime.compare(scheduled_at, DateTime.utc_now()) == :gt,
      do: "scheduled", else: "available"
    put_change(changeset, :state, state)
  end

  @doc "Validate that a state transition is allowed"
  def validate_transition(job, new_state) do
    allowed = Map.get(@transitions, job.state, [])
    if new_state in allowed do
      :ok
    else
      {:error, "Invalid transition: #{job.state} → #{new_state}"}
    end
  end

  def terminal?(job), do: job.state in @terminal_states
end
```

## Step 3 — Worker behaviour

```elixir
defmodule JobQueue.Worker do
  @callback perform(job :: JobQueue.Job.t()) ::
    :ok | {:ok, term()} | {:error, term()}

  @doc """
  Define a worker with configuration.
  Usage:
    use JobQueue.Worker,
      queue: "default",
      max_attempts: 5,
      unique: [within: 60, fields: [:args, :worker]],
      cron: "0 * * * *"
  """
  defmacro __using__(opts) do
    quote do
      @behaviour JobQueue.Worker
      @worker_opts unquote(opts)

      def new(args, overrides \\ []) do
        JobQueue.Worker.build_changeset(__MODULE__, args, @worker_opts, overrides)
      end

      def enqueue(args, overrides \\ []) do
        new(args, overrides) |> JobQueue.Worker.insert(__MODULE__, @worker_opts)
      end
    end
  end

  def build_changeset(module, args, opts, overrides) do
    queue = Keyword.get(opts, :queue, "default")
    max_attempts = Keyword.get(opts, :max_attempts, 3)
    unique_key = compute_unique_key(module, args, opts)
    scheduled_at = compute_scheduled_at(Keyword.merge(opts, overrides))

    JobQueue.Job.new_changeset(%{
      worker: to_string(module),
      queue: queue,
      args: args,
      max_attempts: max_attempts,
      unique_key: unique_key,
      scheduled_at: scheduled_at
    })
  end

  def insert(changeset, module, opts) do
    unique_key = Ecto.Changeset.get_field(changeset, :unique_key)

    if unique_key && Keyword.has_key?(opts, :unique) do
      # TODO: check for existing job with same unique_key in non-terminal state
      # TODO: if exists: return {:ok, %Job{conflict: true, conflict_job_id: existing.id}}
      # TODO: if not exists: insert normally
      # HINT: use INSERT ... ON CONFLICT DO NOTHING with RETURNING, check if row was inserted
      JobQueue.Repo.insert(changeset)
    else
      JobQueue.Repo.insert(changeset)
    end
  end

  defp compute_unique_key(_module, _args, opts) do
    case Keyword.get(opts, :unique) do
      nil -> nil
      unique_opts ->
        # TODO: hash selected fields into a stable key
        # HINT: :crypto.hash(:sha256, :erlang.term_to_binary(sorted_fields)) |> Base.encode16()
        nil
    end
  end

  defp compute_scheduled_at(opts) do
    cond do
      schedule_in = Keyword.get(opts, :schedule_in) ->
        DateTime.add(DateTime.utc_now(), schedule_in, :second)
      at = Keyword.get(opts, :scheduled_at) ->
        at
      true ->
        nil
    end
  end
end
```

## Step 4 — Poller

```elixir
defmodule JobQueue.Queue.Poller do
  use GenServer
  require Logger

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: via(opts[:queue]))
  end

  def init(opts) do
    state = %{
      queue: opts[:queue],
      concurrency: opts[:concurrency] || 10,
      poll_interval: opts[:poll_interval] || 1_000,
      executing: 0
    }
    schedule_poll(state.poll_interval)
    {:ok, state}
  end

  def handle_info(:poll, state) do
    available_slots = state.concurrency - state.executing
    if available_slots > 0 do
      jobs = claim_jobs(state.queue, available_slots)
      new_executing = state.executing + length(jobs)
      Enum.each(jobs, &JobQueue.Queue.Executor.execute/1)
      schedule_poll(state.poll_interval)
      {:noreply, %{state | executing: new_executing}}
    else
      schedule_poll(state.poll_interval)
      {:noreply, state}
    end
  end

  def handle_info({:job_done, _job_id}, state) do
    {:noreply, %{state | executing: state.executing - 1}}
  end

  def handle_cast(:notify, state) do
    # Immediate poll triggered by LISTEN/NOTIFY
    send(self(), :poll)
    {:noreply, state}
  end

  defp claim_jobs(queue, limit) do
    # TODO: use FOR UPDATE SKIP LOCKED to atomically claim jobs
    # SQL:
    #   UPDATE jobs SET state = 'executing', attempted_at = now(),
    #          attempted_by = array_append(attempted_by, $node)
    #   WHERE id IN (
    #     SELECT id FROM jobs
    #     WHERE state IN ('available', 'retryable')
    #       AND queue = $queue
    #       AND scheduled_at <= now()
    #     ORDER BY priority ASC, scheduled_at ASC
    #     LIMIT $limit
    #     FOR UPDATE SKIP LOCKED
    #   )
    #   RETURNING *
    []
  end

  defp via(queue_name), do: {:via, Registry, {JobQueue.Registry, {__MODULE__, queue_name}}}
  defp schedule_poll(interval), do: Process.send_after(self(), :poll, interval)
end
```

## Step 5 — Orphan rescue

```elixir
defmodule JobQueue.Plugins.OrphanRescue do
  use GenServer
  require Logger

  @default_interval_ms 60_000
  @default_timeout_ms 300_000  # 5 minutes

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts)
  end

  def init(opts) do
    interval = Keyword.get(opts, :interval, @default_interval_ms)
    timeout = Keyword.get(opts, :execution_timeout, @default_timeout_ms)
    schedule_rescue(interval)
    {:ok, %{interval: interval, execution_timeout: timeout}}
  end

  def handle_info(:rescue, state) do
    cutoff = DateTime.add(DateTime.utc_now(), -state.execution_timeout, :millisecond)
    rescued = rescue_orphans(cutoff)
    :telemetry.execute([:job_queue, :job, :rescued], %{count: rescued}, %{})
    Logger.info("Orphan rescue: recovered #{rescued} jobs")
    schedule_rescue(state.interval)
    {:noreply, state}
  end

  defp rescue_orphans(cutoff) do
    # TODO: SQL:
    #   UPDATE jobs SET
    #     state = CASE WHEN attempt >= max_attempts THEN 'discarded' ELSE 'retryable' END,
    #     attempt = attempt + 1,
    #     scheduled_at = now() + (backoff_seconds || ' seconds')::interval,
    #     errors = errors || jsonb_build_array(jsonb_build_object(
    #       'at', now(), 'error', 'orphan_rescue: worker died'
    #     ))
    #   WHERE state = 'executing'
    #     AND attempted_at < $cutoff
    #   FOR UPDATE SKIP LOCKED
    #   RETURNING id
    0
  end

  defp schedule_rescue(interval), do: Process.send_after(self(), :rescue, interval)
end
```

## Given tests

```elixir
# test/integration/transactional_enqueue_test.exs
defmodule JobQueue.Integration.TransactionalEnqueueTest do
  use ExUnit.Case, async: false

  defmodule TestWorker do
    use JobQueue.Worker, queue: "test"
    def perform(_job), do: :ok
  end

  test "job does not exist when transaction rolls back" do
    result = JobQueue.Repo.transaction(fn ->
      {:ok, job} = TestWorker.enqueue(%{value: 1})
      JobQueue.Repo.rollback(:intentional)
      job
    end)
    assert {:error, :intentional} = result
    # No job should exist
    assert JobQueue.Repo.aggregate(JobQueue.Job, :count) == 0
  end

  test "job exists when transaction commits" do
    {:ok, job} = JobQueue.Repo.transaction(fn ->
      {:ok, j} = TestWorker.enqueue(%{value: 2})
      j
    end)
    assert JobQueue.Repo.get(JobQueue.Job, job.id) != nil
  end
end

# test/integration/at_least_once_test.exs
defmodule JobQueue.Integration.AtLeastOnceTest do
  use ExUnit.Case, async: false
  @tag timeout: 30_000

  defmodule CrashWorker do
    use JobQueue.Worker, queue: "crash-test", max_attempts: 3
    def perform(job) do
      if job.attempt == 0 do
        # Simulate crash by killing the executor task
        Process.exit(self(), :kill)
      end
      :ok
    end
  end

  test "job is recovered and completed after worker crash" do
    {:ok, job} = CrashWorker.enqueue(%{})
    # Wait for orphan rescue to recover and retry
    assert_job_state(job.id, "completed", timeout: 20_000)
  end

  defp assert_job_state(job_id, state, opts) do
    timeout = Keyword.get(opts, :timeout, 5_000)
    deadline = System.monotonic_time(:millisecond) + timeout
    wait_for_state(job_id, state, deadline)
  end

  defp wait_for_state(job_id, state, deadline) do
    if System.monotonic_time(:millisecond) > deadline do
      flunk("Job #{job_id} did not reach state #{state}")
    end
    job = JobQueue.Repo.get!(JobQueue.Job, job_id)
    if job.state == state do
      :ok
    else
      Process.sleep(100)
      wait_for_state(job_id, state, deadline)
    end
  end
end

# test/integration/concurrency_test.exs
defmodule JobQueue.Integration.ConcurrencyTest do
  use ExUnit.Case, async: false
  @tag timeout: 30_000

  defmodule SlowWorker do
    use JobQueue.Worker, queue: "slow-test"
    def perform(_job), do: Process.sleep(500)
  end

  test "queue never exceeds concurrency limit" do
    concurrency = 3
    n = concurrency * 3  # 9 jobs

    peak_concurrent = Agent.start_link(fn -> 0 end) |> elem(1)
    current = Agent.start_link(fn -> 0 end) |> elem(1)

    :telemetry.attach("concurrency-test", [:job_queue, :job, :start], fn _, _, meta, _ ->
      c = Agent.get_and_update(current, fn c -> {c + 1, c + 1} end)
      Agent.update(peak_concurrent, fn peak -> max(peak, c) end)
    end, nil)

    :telemetry.attach("concurrency-test-stop", [:job_queue, :job, :stop], fn _, _, _, _ ->
      Agent.update(current, fn c -> c - 1 end)
    end, nil)

    for _ <- 1..n, do: SlowWorker.enqueue(%{})

    # Wait for all jobs to complete
    Process.sleep(5_000)

    peak = Agent.get(peak_concurrent, & &1)
    assert peak <= concurrency, "Peak concurrent #{peak} exceeded limit #{concurrency}"

    :telemetry.detach("concurrency-test")
    :telemetry.detach("concurrency-test-stop")
  end
end

# test/uniqueness_test.exs
defmodule JobQueue.UniquenessTest do
  use ExUnit.Case, async: false

  defmodule UniqueWorker do
    use JobQueue.Worker,
      queue: "unique-test",
      unique: [within: 60, fields: [:args, :worker]]
    def perform(_job), do: :ok
  end

  test "duplicate enqueue returns conflict without inserting" do
    {:ok, job1} = UniqueWorker.enqueue(%{task_id: "abc"})
    {:ok, job2} = UniqueWorker.enqueue(%{task_id: "abc"})

    assert job2.conflict == true
    assert job2.conflict_job_id == job1.id
    assert JobQueue.Repo.aggregate(JobQueue.Job, :count) == 1
  end

  test "same args after completion can be enqueued again" do
    {:ok, job} = UniqueWorker.enqueue(%{task_id: "xyz"})
    # Mark job as completed
    job |> Ecto.Changeset.change(state: "completed") |> JobQueue.Repo.update!()
    {:ok, job2} = UniqueWorker.enqueue(%{task_id: "xyz"})
    refute job2.conflict
    assert job2.id != job.id
  end
end
```

## Benchmark

```elixir
# bench/throughput.exs
defmodule JobQueue.Bench.Throughput do
  @duration_s 30
  @concurrency 50

  defmodule NoopWorker do
    use JobQueue.Worker, queue: "bench"
    def perform(_job), do: :ok
  end

  def run do
    # Ensure queue is started with target concurrency
    # Pre-insert jobs
    count = @concurrency * @duration_s * 2  # 2× to ensure queue is never starved
    IO.puts("Pre-inserting #{count} noop jobs...")
    for _ <- 1..count, do: NoopWorker.enqueue(%{})

    completed = Agent.start_link(fn -> 0 end) |> elem(1)
    :telemetry.attach("bench-complete", [:job_queue, :job, :stop], fn _, _, _, _ ->
      Agent.update(completed, &(&1 + 1))
    end, nil)

    start = System.monotonic_time(:millisecond)
    Process.sleep(@duration_s * 1000)
    elapsed_s = (System.monotonic_time(:millisecond) - start) / 1000.0

    total = Agent.get(completed, & &1)
    throughput = total / elapsed_s

    IO.puts("Completed: #{total} jobs in #{Float.round(elapsed_s, 1)}s")
    IO.puts("Throughput: #{Float.round(throughput, 0)} jobs/s")
    IO.puts("Target:     500 jobs/s")
    IO.puts("Pass:       #{if throughput >= 500, do: "YES", else: "NO"}")
    :telemetry.detach("bench-complete")
  end
end

JobQueue.Bench.Throughput.run()
```

## Trade-offs

| Design | Selected | Alternative | Trade-off |
|---|---|---|---|
| Claim mechanism | `FOR UPDATE SKIP LOCKED` | Advisory locks / Redis | Advisory locks: manual cleanup on crash; Redis: not transactional with business data |
| Uniqueness | DB unique index on hash | Application-level check | Application check: race condition window; DB index: atomic by construction |
| Dispatch trigger | PostgreSQL NOTIFY | HTTP callback / in-memory | HTTP: couples services; in-memory: lost on restart; NOTIFY: native, zero extra infrastructure |
| Orphan detection | Periodic scan with `attempted_at` | Process monitors | Monitors: instant but cross-node monitors can miss network partitions; scan: always correct |
| Backoff jitter | ±20% uniform random | Fixed exponential | Fixed: retry storms when many jobs fail simultaneously; jitter: desynchronizes retries |
| Global concurrency | Node-local counter GenServer | Cluster-wide DB counter | DB counter: correct across nodes; local counter: simpler, sufficient for most deployments |

## Production mistakes

**Not indexing on `(state, queue, priority, scheduled_at)`.** A full table scan on 10 million jobs at 500ms poll interval is catastrophic. The poller's `SELECT ... FOR UPDATE SKIP LOCKED` must use the partial index. Run `EXPLAIN ANALYZE` on the claim query in your test environment with `SET enable_seqscan = off` disabled to verify the index is used.

**Inserting jobs outside a transaction alongside business data.** `Repo.insert!(job)` and `Repo.update!(order, state: "confirmed")` in sequence without a transaction has a window where the order is confirmed but the job does not exist (if the process crashes between the two). Always use `Ecto.Multi` or `Repo.transaction/1` to wrap both operations.

**Not guarding `perform/1` timeout.** A job that hangs indefinitely holds the worker's `Task.Supervisor` slot. Set a `timeout` option on the `Task.async` that runs `perform/1`. On timeout, send `{:error, :timeout}` back and the job enters `retryable`. Without this, one hung job can starve the queue.

**Cron scheduler running on all nodes without deduplication.** Three nodes, each running a cron scheduler, each inserting a job for the same minute = three copies of the same job. The uniqueness constraint must include the cron expression and the truncated minute timestamp as the unique key. All three inserts compete for the unique index; only one wins.

**TTL on unique_key not matching the unique window.** If a job's `unique` window is 60 seconds but the job can stay in `executing` state for 10 minutes (due to a slow worker), a second enqueue within the unique window is correctly blocked. But after the window expires, a third enqueue is allowed — even if the original job is still running. This is correct per spec but may surprise callers. Document that `unique` is a window from insertion time, not from completion time.

## Resources

- Oban documentation — https://hexdocs.pm/oban/ (Elixir reference implementation; study design rationale)
- PostgreSQL `SELECT FOR UPDATE` — https://www.postgresql.org/docs/current/sql-select.html#SQL-FOR-UPDATE-SHARE
- PostgreSQL LISTEN/NOTIFY — https://www.postgresql.org/docs/current/sql-notify.html
- Ecto.Multi documentation — https://hexdocs.pm/ecto/Ecto.Multi.html
- Postgrex.Notifications module — https://hexdocs.pm/postgrex/Postgrex.Notifications.html
- Ongaro & Ousterhout — "In Search of an Understandable Consensus Algorithm" (2014) (background on distributed job coordination)
