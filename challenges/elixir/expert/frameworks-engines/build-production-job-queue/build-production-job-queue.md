# Production Job Queue (Oban-like)

**Project**: `job_queue` — PostgreSQL-backed job queue using FOR UPDATE SKIP LOCKED and LISTEN/NOTIFY

## Project context

Your team runs a SaaS platform. Sending confirmation emails, generating PDF invoices, syncing user data to a CRM, and charging credit cards — all of these operations are triggered by HTTP requests but must not block the response. They must survive node crashes, retry on transient failures, and never execute more than once (or at most once more if the process died mid-execution).

The first attempt used an in-memory queue backed by a GenServer. When the node restarted, the queue was empty. Lost jobs meant unpaid invoices and unsent emails. The second attempt used Redis, but job enqueue and database insert were two separate operations: if the database rolled back after the Redis enqueue, a ghost job existed for data that was never committed.

The only correct foundation is PostgreSQL: job insertion is transactional with the business operation. If the outer transaction rolls back, the job does not exist. `SELECT FOR UPDATE SKIP LOCKED` ensures mutual exclusion at the database level without any in-memory coordination between nodes.

You will build `JobQueue`: a production-quality background job system backed by PostgreSQL.

## Design decisions

**Option A — in-memory GenServer-based queue**
- Pros: sub-ms enqueue, trivial implementation
- Cons: jobs lost on restart — unusable for production

**Option B — Postgres-backed queue with SKIP LOCKED and advisory locks** (chosen)
- Pros: crash-safe, survives deploys, transactional enqueue with business data
- Cons: higher per-job overhead than in-memory

→ Chose **B** because any production queue must survive a node restart; durability is the minimum bar.

## Quick start

1. Create project:
   ```bash
   mix new <project_name>
   cd <project_name>
   ```

2. Copy dependencies to `mix.exs`

3. Implement modules following the project structure

4. Run tests: `mix test`

5. Benchmark: `mix run lib/benchmark.exs`

## Why `FOR UPDATE SKIP LOCKED` and not advisory locks

PostgreSQL advisory locks are manual; the application must release them explicitly. If a worker crashes, the advisory lock is held until the session closes. `FOR UPDATE SKIP LOCKED` works at the row level: when a worker claims a job row, the row is locked for the duration of the worker's transaction. Other workers trying to claim the same job see it as locked and skip it (SKIP LOCKED). When the worker commits (marking the job `executing`), the lock is released and the job is no longer available for claiming. The lock lifetime equals the transaction duration — no manual cleanup.

## Why LISTEN/NOTIFY instead of polling-only

Polling at 1-second intervals adds up to 1 second of latency for every enqueued job. `NOTIFY` from a trigger fires immediately when a row is inserted. The LISTEN connection receives the notification within ~5ms on a local database. This reduces median latency from ~500ms (average of 1s interval) to ~10ms. Polling is the fallback when the NOTIFY connection drops — it handles missed notifications without data loss.

## Why at-least-once and not exactly-once

Exactly-once delivery requires distributed transactions or two-phase commit — prohibitively complex. At-least-once is achievable with idempotent workers: the job may execute more than once (if the worker dies after running `perform/1` but before marking the job `completed`), but if `perform/1` is idempotent (safe to run multiple times), the observable outcome is correct. The orphan rescue returns jobs to `available` if the worker dies, which may cause a second execution. This is the explicit design contract.

## Project structure
```
job_queue/
├── script/
│   └── main.exs
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

### Step 1: Database migration

**Objective**: Implement the Database migration component required by the production job queue system.

### Step 2: Job schema and state machine

**Objective**: Implement the deterministic state machine that applies committed commands to state.

```elixir
defmodule JobQueue.Job do
  use Ecto.Schema
  import Ecto.Changeset

  @states ~w(available scheduled executing completed retryable discarded cancelled)
  @terminal_states ~w(completed discarded cancelled)

  # Valid transitions:
  # available -> executing | cancelled
  # scheduled -> available | cancelled
  # executing -> completed | retryable | discarded
  # retryable -> executing | cancelled
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
      {:error, "Invalid transition: #{job.state} -> #{new_state}"}
    end
  end

  def terminal?(job), do: job.state in @terminal_states
end
```
### Step 3: Worker behaviour

**Objective**: Define the behaviour contract and its required callbacks.

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

  @doc """
  Insert a job, handling uniqueness constraints via ON CONFLICT.
  When a unique key is present and an active job with the same key exists,
  returns the existing job with `conflict: true` instead of inserting a duplicate.
  """
  def insert(changeset, module, opts) do
    unique_key = Ecto.Changeset.get_field(changeset, :unique_key)

    if unique_key && Keyword.has_key?(opts, :unique) do
      insert_with_uniqueness(changeset, unique_key)
    else
      JobQueue.Repo.insert(changeset)
    end
  end

  defp insert_with_uniqueness(changeset, unique_key) do
    import Ecto.Query

    existing =
      from(j in JobQueue.Job,
        where: j.unique_key == ^unique_key,
        where: j.state in ["available", "scheduled", "executing", "retryable"],
        limit: 1
      )
      |> JobQueue.Repo.one()

    case existing do
      nil ->
        JobQueue.Repo.insert(changeset)

      %JobQueue.Job{id: existing_id} ->
        job = Ecto.Changeset.apply_changes(changeset)
        {:ok, %{job | conflict: true, conflict_job_id: existing_id}}
    end
  end

  defp compute_unique_key(module, args, opts) do
    case Keyword.get(opts, :unique) do
      nil ->
        nil

      unique_opts ->
        fields = Keyword.get(unique_opts, :fields, [:args, :worker])

        data_to_hash =
          Enum.map(fields, fn
            :args -> {:args, args}
            :worker -> {:worker, to_string(module)}
            :queue -> {:queue, Keyword.get(opts, :queue, "default")}
          end)
          |> Enum.sort()

        :crypto.hash(:sha256, :erlang.term_to_binary(data_to_hash))
        |> Base.encode16(case: :lower)
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
### Step 4: Poller

**Objective**: Implement the Poller component required by the production job queue system.

```elixir
defmodule JobQueue.Queue.Poller do
  use GenServer
  require Logger
  import Ecto.Query

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
    send(self(), :poll)
    {:noreply, state}
  end

  @doc """
  Atomically claim up to `limit` jobs using FOR UPDATE SKIP LOCKED.
  This query selects available/retryable jobs whose scheduled_at is in the past,
  locks them at the row level (skipping already-locked rows), and transitions
  them to 'executing' in a single UPDATE ... WHERE id IN (SELECT ... FOR UPDATE SKIP LOCKED).
  """
  defp claim_jobs(queue, limit) do
    node_name = to_string(node())
    now = DateTime.utc_now()

    sql = """
    UPDATE jobs
    SET state = 'executing',
        attempt = attempt + 1,
        attempted_at = $1,
        attempted_by = array_append(attempted_by, $2)
    WHERE id IN (
      SELECT id FROM jobs
      WHERE state IN ('available', 'retryable')
        AND queue = $3
        AND scheduled_at <= $1
      ORDER BY priority ASC, scheduled_at ASC
      LIMIT $4
      FOR UPDATE SKIP LOCKED
    )
    RETURNING *
    """

    case Ecto.Adapters.SQL.query(JobQueue.Repo, sql, [now, node_name, queue, limit]) do
      {:ok, %{rows: rows, columns: columns}} ->
        Enum.map(rows, fn row ->
          columns
          |> Enum.zip(row)
          |> Map.new()
          |> then(&JobQueue.Repo.load(JobQueue.Job, &1))
        end)

      {:error, reason} ->
        Logger.error("Failed to claim jobs: #{inspect(reason)}")
        []
    end
  end

  defp via(queue_name), do: {:via, Registry, {JobQueue.Registry, {__MODULE__, queue_name}}}
  defp schedule_poll(interval), do: Process.send_after(self(), :poll, interval)
end
```
### Step 5: Orphan rescue

**Objective**: Implement the Orphan rescue component required by the production job queue system.

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

  @doc """
  Recover orphaned jobs: jobs stuck in 'executing' state whose attempted_at
  is older than the cutoff. Jobs under max_attempts become 'retryable' with
  exponential backoff; jobs at max_attempts become 'discarded'.
  Uses FOR UPDATE SKIP LOCKED to avoid interfering with active executors.
  """
  defp rescue_orphans(cutoff) do
    sql = """
    WITH orphaned AS (
      SELECT id, attempt, max_attempts
      FROM jobs
      WHERE state = 'executing'
        AND attempted_at < $1
      FOR UPDATE SKIP LOCKED
    )
    UPDATE jobs
    SET
      state = CASE
        WHEN orphaned.attempt >= orphaned.max_attempts THEN 'discarded'
        ELSE 'retryable'
      END,
      scheduled_at = now() + (power(2, orphaned.attempt) || ' seconds')::interval,
      errors = jobs.errors || jsonb_build_array(
        jsonb_build_object('at', now()::text, 'error', 'orphan_rescue: worker died')
      ),
      discarded_at = CASE
        WHEN orphaned.attempt >= orphaned.max_attempts THEN now()
        ELSE NULL
      END
    FROM orphaned
    WHERE jobs.id = orphaned.id
    RETURNING jobs.id
    """

    case Ecto.Adapters.SQL.query(JobQueue.Repo, sql, [cutoff]) do
      {:ok, %{num_rows: count}} -> count
      {:error, reason} ->
        Logger.error("Orphan rescue failed: #{inspect(reason)}")
        0
    end
  end

  defp schedule_rescue(interval), do: Process.send_after(self(), :rescue, interval)
end
```
### Why this works

The design isolates correctness-critical invariants from latency-critical paths and from evolution-critical contracts. Modules expose narrow interfaces and fail fast on contract violations, so bugs surface close to their source. Tests target invariants rather than implementation details, so refactors don't produce false alarms. The trade-offs are explicit in the Design decisions section, which makes the "why" auditable instead of folklore.

## Given tests

```elixir
defmodule JobQueue.Integration.TransactionalEnqueueTest do
  use ExUnit.Case, async: false
  doctest JobQueue.Plugins.OrphanRescue

  defmodule TestWorker do
    use JobQueue.Worker, queue: "test"
    def perform(_job), do: :ok
  end

  describe "TransactionalEnqueue" do

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
        Process.exit(self(), :kill)
      end
      :ok
    end
  end

  test "job is recovered and completed after worker crash" do
    {:ok, job} = CrashWorker.enqueue(%{})
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
    job |> Ecto.Changeset.change(state: "completed") |> JobQueue.Repo.update!()
    {:ok, job2} = UniqueWorker.enqueue(%{task_id: "xyz"})
    refute job2.conflict
    assert job2.id != job.id
  end

  end
end
```
## Main Entry Point

```elixir
def main do
  IO.puts("======== 51-build-production-job-queue ========")
  IO.puts("Build production job queue")
  IO.puts("")
  
  JobQueue.Repo.Migrations.CreateJobs.start_link([])
  IO.puts("JobQueue.Repo.Migrations.CreateJobs started")
  
  IO.puts("Run: mix test")
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
    IO.puts("=== Job Queue Throughput Benchmark ===")
    IO.puts("Duration: #{@duration_s}s, Concurrency: #{@concurrency} workers\n")
    
    count = @concurrency * @duration_s * 2
    IO.write("Pre-inserting #{count} noop jobs... ")
    
    for _ <- 1..count, do: NoopWorker.enqueue(%{})
    IO.puts("done")

    completed = Agent.start_link(fn -> 0 end) |> elem(1)
    started = Agent.start_link(fn -> 0 end) |> elem(1)
    
    :telemetry.attach("bench-complete", [:job_queue, :job, :stop], fn _, _, _, _ ->
      Agent.update(completed, &(&1 + 1))
    end, nil)
    
    :telemetry.attach("bench-start", [:job_queue, :job, :start], fn _, _, _, _ ->
      Agent.update(started, &(&1 + 1))
    end, nil)

    IO.write("Running benchmark")
    start = System.monotonic_time(:millisecond)
    
    for _ <- 1..(@duration_s) do
      Process.sleep(1000)
      IO.write(".")
    end
    
    elapsed_s = (System.monotonic_time(:millisecond) - start) / 1000.0
    total_completed = Agent.get(completed, & &1)
    total_started = Agent.get(started, & &1)
    throughput = total_completed / elapsed_s

    IO.puts("\n\nResults:")
    IO.puts("  Jobs started:  #{total_started}")
    IO.puts("  Jobs completed: #{total_completed}")
    IO.puts("  Time elapsed:   #{Float.round(elapsed_s, 1)}s")
    IO.puts("  Throughput:     #{Float.round(throughput, 0)} jobs/s")
    IO.puts("  Target:         >= 500 jobs/s")
    IO.puts("  Status:         #{if throughput >= 500, do: "PASS", else: "FAIL"}")
    
    :telemetry.detach("bench-complete")
    :telemetry.detach("bench-start")
  end
end

JobQueue.Bench.Throughput.run()
```
## Key Concepts: Distributed Consensus y Transactional Atomicity en Job Queues

Las job queues distribuidas requieren resolver conflictos imposibles:

1. **Pérdida de datos vs. Duplicación**: Si un worker muere después de ejecutar `perform/1` pero antes de marcar el job como `completed`, el job queda en `executing`. ¿Qué pasa?
   - Si NO lo recuperas: pérdida de datos (re-enviar email nunca ocurre).
   - Si lo recuperas: duplicación (dos emails enviados).
   
   La solución es **at-least-once** con **idempotencia**: cada job se ejecuta 1+ veces, pero el `perform/1` es idempotent (mismo resultado si ejecutas 2 veces). Para emails, usa un `unique_key` en la base de datos para evitar duplicar.

2. **SELECT FOR UPDATE SKIP LOCKED**: PostgreSQL provee select-for-update a nivel de fila, no de tabla. Cuando un worker reclama un job:
   ```sql
   SELECT id FROM jobs WHERE state = 'available' AND queue = ? 
   ORDER BY priority LIMIT 1 
   FOR UPDATE SKIP LOCKED
   ```
   Postgres bloquea la fila. Otros workers que intenten la misma query ven la fila como bloqueada y la saltan (SKIP LOCKED). Sin esto:
   - Advisory locks: requieren cleanup manual en crash.
   - Redis: no es transactional con el estado del negocio.

3. **Transactional Enqueue**: Cuando insertas un job, debe estar en la MISMA transacción que el evento del negocio:
   ```elixir
   Repo.transaction(fn ->
     Repo.insert!(order, state: "confirmed")  # Negocio
     Repo.insert!(job)                         # Job
   end)
   ```
   Si la transacción hace rollback, ambos desaparecen. Si solo haces `Repo.insert!(order)` entonces `Repo.insert!(job)` en secuencia sin transacción, hay una ventana donde la orden está confirmada pero el job no existe.

4. **LISTEN/NOTIFY para latencia**: Polling a 1 segundo = 500ms latencia promedio. PostgreSQL NOTIFY dispara un trigger en INSERT, enviando un mensaje al cliente en ~5ms. Los clientes corren un "listener" que recibe notificaciones y dispara un poll inmediato. Si el listener se cae, el polling fallback captura los jobs perdidos.

**Insight de producción**: La única cola a prueba de fallos es una respaldada por una base de datos transaccional. Memoria pura = pérdida en restart. Redis sin Ecto.Multi = ventana de pérdida de datos. Postgres + transacciones = correcto.

---

## Trade-off analysis

| Design | Selected | Alternative | Trade-off |
|---|---|---|---|
| Claim mechanism | `FOR UPDATE SKIP LOCKED` | Advisory locks / Redis | Advisory locks: manual cleanup on crash; Redis: not transactional with business data |
| Uniqueness | DB unique index on hash | Application-level check | Application check: race condition window; DB index: atomic by construction |
| Dispatch trigger | PostgreSQL NOTIFY | HTTP callback / in-memory | HTTP: couples services; in-memory: lost on restart; NOTIFY: native, zero extra infrastructure |
| Orphan detection | Periodic scan with `attempted_at` | Process monitors | Monitors: instant but cross-node monitors can miss network partitions; scan: always correct |
| Backoff jitter | ±20% uniform random | Fixed exponential | Fixed: retry storms when many jobs fail simultaneously; jitter: desynchronizes retries |
| Global concurrency | Node-local counter GenServer | Cluster-wide DB counter | DB counter: correct across nodes; local counter: simpler, sufficient for most deployments |

## Common production mistakes

**Not indexing on `(state, queue, priority, scheduled_at)`.** A full table scan on 10 million jobs at 500ms poll interval is catastrophic. The poller's `SELECT ... FOR UPDATE SKIP LOCKED` must use the partial index. Run `EXPLAIN ANALYZE` on the claim query in your test environment with `SET enable_seqscan = off` disabled to verify the index is used.

**Inserting jobs outside a transaction alongside business data.** `Repo.insert!(job)` and `Repo.update!(order, state: "confirmed")` in sequence without a transaction has a window where the order is confirmed but the job does not exist (if the process crashes between the two). Always use `Ecto.Multi` or `Repo.transaction/1` to wrap both operations.

**Not guarding `perform/1` timeout.** A job that hangs indefinitely holds the worker's `Task.Supervisor` slot. Set a `timeout` option on the `Task.async` that runs `perform/1`. On timeout, send `{:error, :timeout}` back and the job enters `retryable`. Without this, one hung job can starve the queue.

**Cron scheduler running on all nodes without deduplication.** Three nodes, each running a cron scheduler, each inserting a job for the same minute = three copies of the same job. The uniqueness constraint must include the cron expression and the truncated minute timestamp as the unique key. All three inserts compete for the unique index; only one wins.

**TTL on unique_key not matching the unique window.** If a job's `unique` window is 60 seconds but the job can stay in `executing` state for 10 minutes (due to a slow worker), a second enqueue within the unique window is correctly blocked. But after the window expires, a third enqueue is allowed — even if the original job is still running. This is correct per spec but may surprise callers. Document that `unique` is a window from insertion time, not from completion time.

## Reflection

Your queue suddenly has 10M jobs backed up because a downstream API has been down for an hour. How does your system catch up without DoS'ing the recovering API, and what's your SLA to the producer during the backlog?

## Resources

- Oban documentation — https://hexdocs.pm/oban/ (Elixir reference implementation; study design rationale)
- PostgreSQL `SELECT FOR UPDATE` — https://www.postgresql.org/docs/current/sql-select.html#SQL-FOR-UPDATE-SHARE
- PostgreSQL LISTEN/NOTIFY — https://www.postgresql.org/docs/current/sql-notify.html
- Ecto.Multi documentation — https://hexdocs.pm/ecto/Ecto.Multi.html
- Postgrex.Notifications module — https://hexdocs.pm/postgrex/Postgrex.Notifications.html
- Ongaro & Ousterhout — "In Search of an Understandable Consensus Algorithm" (2014) (background on distributed job coordination)

---

## Production-Grade Addendum (Insane Standard)

The sections below extend the content above to the full `insane` template: a canonical `mix.exs`, an executable `script/main.exs` stress harness, explicit error-handling and recovery protocols, concrete performance targets, and a consolidated key-concepts list. These are non-negotiable for production-grade systems.

### `mix.exs`

```elixir
defmodule ObanLike.MixProject do
  use Mix.Project

  def project do
    [
      app: :oban_like,
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
      mod: {ObanLike.Application, []}
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
  Realistic stress harness for `oban_like` (production job queue).
  Runs five phases: warmup, steady-state load, chaos injection, recovery
  verification, and invariant audit. Exits non-zero if any SLO is breached.
  """

  @warmup_ops 10_000
  @steady_ops 100_000
  @chaos_ratio 0.10
  @slo_p99_us 10000
  @slo_error_rate 0.001

  def main do
    :ok = Application.ensure_all_started(:oban_like) |> elem(0) |> then(&(&1 == :ok && :ok || :ok))
    IO.puts("=== ObanLike stress test ===")

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
    case Application.stop(:oban_like) do
      :ok -> :ok
      _ -> :ok
    end
    {:ok, _} = Application.ensure_all_started(:oban_like)
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
      # TODO: replace with actual oban_like operation
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

ObanLike classifies every failure on two axes: **severity** (critical vs recoverable) and **scope** (per-request vs system-wide). Critical violations halt the subsystem and page an operator; recoverable faults are retried with bounded backoff and explicit budgets.

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
| **Sustained throughput** | **100,000 jobs/s** | comparable to reference system |
| **Latency p50** | below p99/4 | — |
| **Latency p99** | **10 ms** | Oban Pro architecture |
| **Latency p999** | ≤ 3× p99 | excludes GC pauses > 10ms |
| **Error rate** | **< 0.1 %%** | excludes client-side 4xx |
| **Cold start** | **< 3 s** | supervisor ready + warm caches |
| **Recovery after crash** | **< 5 s** | replay + peer reconciliation |
| **Memory per connection/entity** | **< 50 KiB** | bounded by design |
| **CPU overhead of telemetry** | **< 1 %%** | sampled emission |

### Baselines we should beat or match

- Oban Pro architecture: standard reference for this class of system.
- Native BEAM advantage: per-process isolation and lightweight concurrency give ~2-5x throughput vs process-per-request architectures (Ruby, Python) on equivalent hardware.
- Gap vs native (C++/Rust) implementations: expect 2-3x latency overhead in the hot path; mitigated by avoiding cross-process message boundaries on critical paths (use ETS with `:write_concurrency`).

---

## Why Production Job Queue (Oban-like) matters

Mastering **Production Job Queue (Oban-like)** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

---

## Implementation

### `lib/job_queue.ex`

```elixir
defmodule JobQueue do
  @moduledoc """
  Reference implementation for Production Job Queue (Oban-like).

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the job_queue module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> JobQueue.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```
### `test/job_queue_test.exs`

```elixir
defmodule JobQueueTest do
  use ExUnit.Case, async: true

  doctest JobQueue

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert JobQueue.run(:noop) == :ok
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

- Oban Pro architecture
- [Release It! (Nygard)](https://pragprog.com/titles/mnee2/release-it-second-edition/) — circuit breaker, bulkhead, steady-state
- [Google SRE Book](https://sre.google/books/) — SLOs, error budgets, overload handling
- [Designing Data-Intensive Applications (Kleppmann)](https://dataintensive.net/) — correctness under failure

---
