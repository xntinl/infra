# Row-Level Locking — `FOR UPDATE SKIP LOCKED`

**Project**: `job_queue` — a minimal job queue built on Postgres with contention-free workers.

---

## Project context

You need an in-database job queue. Redis or SQS would do it, but you want
exactly-once semantics under existing transactional infrastructure. Postgres can serve as
a queue if workers pick jobs without trampling each other. The primitive that makes this
possible is `SELECT ... FOR UPDATE SKIP LOCKED`.

Without `SKIP LOCKED`, 10 workers pulling jobs all contend on the same row, and 9 of them
block waiting. With `SKIP LOCKED`, each worker picks a different row in O(1).

```
job_queue/
├── lib/
│   └── job_queue/
│       ├── application.ex
│       ├── repo.ex
│       ├── worker.ex
│       ├── queue.ex
│       └── schemas/
│           └── job.ex
├── priv/repo/migrations/
├── test/job_queue/
│   └── queue_test.exs
├── bench/queue_bench.exs
└── mix.exs
```

---

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

**Ecto-specific insight:**
Ecto separates the query layer (building queries) from the execution layer (sending them). This separation allows for debugging, composability, and testing without a database. Never load all rows first and filter in-memory — write the filter into the query itself, or you've just built an N+1 problem.
### 1. Postgres row-level locks

- `FOR UPDATE` — acquires an exclusive lock on the row; blocks concurrent `FOR UPDATE`
  and UPDATE/DELETE.
- `FOR SHARE` — shared lock; allows other `FOR SHARE` but blocks writers.
- `FOR NO KEY UPDATE` — weaker than `FOR UPDATE`; allows concurrent foreign-key writes.
- `SKIP LOCKED` — if the row would block, skip it and return the next candidate.
- `NOWAIT` — if the row would block, raise an error immediately.

For queues, `FOR UPDATE SKIP LOCKED` gives each worker a unique row with no blocking.

### 2. Locks are released at transaction end

Hold the lock only as long as the work. Commit as soon as the job is marked done (or
failed) to let the row re-enter the pool if retried.

### 3. Ecto `lock:` option

```elixir
from(j in Job, where: j.state == "pending", limit: 1, lock: "FOR UPDATE SKIP LOCKED")
```

The lock clause is a raw SQL string because Ecto does not model each combination
(`NOWAIT`, `OF <table>`). You can also lock specific joined tables: `lock: "FOR UPDATE OF ?"`
with a binding.

### 4. The queue loop

```
loop:
  BEGIN
    SELECT * FROM jobs WHERE state = 'pending'
      ORDER BY scheduled_at ASC LIMIT 1
      FOR UPDATE SKIP LOCKED
    UPDATE jobs SET state = 'running', attempts = attempts + 1 WHERE id = ?
  COMMIT   -- lock released; run the job outside the transaction
  execute_job(...)
  BEGIN
    UPDATE jobs SET state = 'done' WHERE id = ?   (or 'failed' and requeue)
  COMMIT
```

Keep the job execution OUTSIDE any DB transaction. A 30-second HTTP call should not hold
a row lock.

---

## Design decisions

- **Option A — lock during execution**: one transaction from dequeue to completion.
  Pros: simplest code. Cons: long-held locks, connection pool pressure, HTTP retries
  cascade into lock contention.
- **Option B — lock only for dequeue; release before execution; re-enter to update state**.
  Pros: short transactions. Cons: needs idempotent job handlers (work may be duplicated
  if worker dies between commit and completion).

We use **Option B** with idempotent handlers. This is the pattern Oban uses.

---

## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  [
    {:ecto_sql, "~> 3.12"},
    {:postgrex, "~> 0.19"},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 1: Schema and migration

**Objective**: Model jobs with a partial index on `state = 'pending'` so dequeue scans only the live backlog, not the completed tail.

```elixir
# lib/job_queue/schemas/job.ex
defmodule JobQueue.Schemas.Job do
  use Ecto.Schema
  import Ecto.Changeset

  schema "jobs" do
    field :queue, :string, default: "default"
    field :worker, :string, null: false
    field :args, :map, default: %{}
    field :state, :string, default: "pending"
    field :attempts, :integer, default: 0
    field :max_attempts, :integer, default: 3
    field :scheduled_at, :utc_datetime
    field :completed_at, :utc_datetime
    field :last_error, :string
    timestamps()
  end

  def changeset(job, attrs) do
    job
    |> cast(attrs, [:queue, :worker, :args, :scheduled_at, :max_attempts])
    |> validate_required([:worker])
    |> put_scheduled_at()
  end

  defp put_scheduled_at(cs) do
    case get_field(cs, :scheduled_at) do
      nil -> put_change(cs, :scheduled_at, DateTime.utc_now() |> DateTime.truncate(:second))
      _ -> cs
    end
  end
end
```

```elixir
# priv/repo/migrations/20260101000000_create_jobs.exs
defmodule JobQueue.Repo.Migrations.CreateJobs do
  use Ecto.Migration

  def change do
    create table(:jobs) do
      add :queue, :string, null: false, default: "default"
      add :worker, :string, null: false
      add :args, :map, null: false, default: %{}
      add :state, :string, null: false, default: "pending"
      add :attempts, :integer, null: false, default: 0
      add :max_attempts, :integer, null: false, default: 3
      add :scheduled_at, :utc_datetime, null: false
      add :completed_at, :utc_datetime
      add :last_error, :text
      timestamps()
    end

    # Partial index for the hot pull query
    create index(:jobs, [:queue, :scheduled_at],
             where: "state = 'pending'",
             name: :jobs_pending_idx)

    create index(:jobs, [:state])
  end
end
```

### Step 2: Queue — enqueue and dequeue

**Objective**: Claim one pending row with FOR UPDATE SKIP LOCKED so concurrent workers never block, with exponential-backoff retries on failure.

```elixir
# lib/job_queue/queue.ex
defmodule JobQueue.Queue do
  import Ecto.Query

  alias JobQueue.Repo
  alias JobQueue.Schemas.Job

  @spec enqueue(module(), map(), keyword()) :: {:ok, Job.t()}
  def enqueue(worker, args, opts \\ []) do
    attrs = %{
      worker: to_string(worker),
      args: args,
      queue: Keyword.get(opts, :queue, "default"),
      scheduled_at: Keyword.get(opts, :scheduled_at),
      max_attempts: Keyword.get(opts, :max_attempts, 3)
    }

    %Job{}
    |> Job.changeset(attrs)
    |> Repo.insert()
  end

  @doc """
  Pulls one ready job and marks it as running.

  Returns `{:ok, job}` or `:empty`. Uses `FOR UPDATE SKIP LOCKED` so concurrent
  workers never block each other.
  """
  @spec dequeue(String.t()) :: {:ok, Job.t()} | :empty
  def dequeue(queue \\ "default") do
    now = DateTime.utc_now() |> DateTime.truncate(:second)

    Repo.transaction(fn ->
      candidate =
        from(j in Job,
          where:
            j.queue == ^queue and
              j.state == "pending" and
              j.scheduled_at <= ^now,
          order_by: [asc: j.scheduled_at, asc: j.id],
          limit: 1,
          lock: "FOR UPDATE SKIP LOCKED"
        )
        |> Repo.one()

      case candidate do
        nil ->
          :empty

        job ->
          {:ok, updated} =
            job
            |> Ecto.Changeset.change(state: "running", attempts: job.attempts + 1)
            |> Repo.update()

          updated
      end
    end)
    |> case do
      {:ok, :empty} -> :empty
      {:ok, job} -> {:ok, job}
    end
  end

  @spec complete(Job.t()) :: {:ok, Job.t()}
  def complete(%Job{} = job) do
    job
    |> Ecto.Changeset.change(state: "done", completed_at: now())
    |> Repo.update()
  end

  @spec fail(Job.t(), String.t()) :: {:ok, Job.t()}
  def fail(%Job{} = job, reason) do
    next_state = if job.attempts >= job.max_attempts, do: "dead", else: "pending"

    job
    |> Ecto.Changeset.change(
      state: next_state,
      last_error: reason,
      scheduled_at: backoff(job.attempts)
    )
    |> Repo.update()
  end

  defp backoff(attempts) do
    secs = :math.pow(2, attempts) |> trunc()
    DateTime.utc_now() |> DateTime.add(secs, :second) |> DateTime.truncate(:second)
  end

  defp now, do: DateTime.utc_now() |> DateTime.truncate(:second)
end
```

### Step 3: Worker — the poll loop

**Objective**: Drive a GenServer that polls, dispatches, and reports complete/fail outside the claim transaction so locks stay under milliseconds.

```elixir
# lib/job_queue/worker.ex
defmodule JobQueue.Worker do
  use GenServer
  require Logger

  alias JobQueue.Queue

  @poll_ms 200

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: Keyword.fetch!(opts, :name))

  @impl true
  def init(opts) do
    queue = Keyword.get(opts, :queue, "default")
    schedule_poll()
    {:ok, %{queue: queue}}
  end

  @impl true
  def handle_info(:poll, %{queue: queue} = state) do
    case Queue.dequeue(queue) do
      {:ok, job} -> run(job)
      :empty -> :ok
    end

    schedule_poll()
    {:noreply, state}
  end

  defp run(job) do
    module = String.to_existing_atom("Elixir." <> job.worker)

    try do
      :ok = module.perform(job.args)
      Queue.complete(job)
    rescue
      e ->
        Logger.error("job #{job.id} failed: #{inspect(e)}")
        Queue.fail(job, Exception.message(e))
    end
  end

  defp schedule_poll, do: Process.send_after(self(), :poll, @poll_ms)
end
```

---

## Why this works

- `FOR UPDATE SKIP LOCKED` makes dequeue O(1) regardless of worker count. Each worker
  sees a distinct row or `nil`. No lock contention, no starvation within a single
  scheduled_at cohort as long as we order by `id` after `scheduled_at`.
- The partial index `WHERE state = 'pending'` is small — bloat is proportional to
  the backlog, not to total processed jobs.
- Short transactions: lock is held only for the SELECT + UPDATE (milliseconds). The
  actual work runs outside the transaction. If the worker crashes mid-work, the row
  stays as `running` with bumped attempts — a reaper (not shown) can requeue rows stuck
  in `running` for > N minutes.
- Idempotency is the worker author's responsibility. For HTTP calls, use
  `Idempotency-Key` derived from `job.id`.

---

## Data flow

```
enqueue(Worker, %{...})
    │
    ▼
INSERT INTO jobs (state='pending', scheduled_at=now())
    │
    ▼
Worker poll loop (every 200ms)
    │
    ▼  BEGIN
    │  SELECT * FROM jobs WHERE state='pending' ...
    │    LIMIT 1 FOR UPDATE SKIP LOCKED
    │  UPDATE jobs SET state='running'
    │  COMMIT                                   ← row is now claimed
    │
    ▼  module.perform(args)   (outside transaction)
    │
    ▼  BEGIN
    │  UPDATE jobs SET state='done' OR 'pending' (with backoff)
    │  COMMIT
```

---

## Tests

```elixir
# test/job_queue/queue_test.exs
defmodule JobQueue.QueueTest do
  use ExUnit.Case, async: false
  alias JobQueue.{Queue, Repo}
  alias JobQueue.Schemas.Job

  setup do
    Ecto.Adapters.SQL.Sandbox.checkout(Repo)
    Ecto.Adapters.SQL.Sandbox.mode(Repo, {:shared, self()})
    Repo.delete_all(Job)
    :ok
  end

  describe "enqueue/2" do
    test "creates a pending job" do
      assert {:ok, job} = Queue.enqueue("SendEmail", %{"to" => "a@b.com"})
      assert job.state == "pending"
      assert job.attempts == 0
    end
  end

  describe "dequeue/1" do
    test ":empty when no jobs" do
      assert :empty = Queue.dequeue()
    end

    test "returns oldest pending job in FIFO" do
      {:ok, _} = Queue.enqueue("W", %{n: 1})
      Process.sleep(1_100)
      {:ok, _} = Queue.enqueue("W", %{n: 2})

      assert {:ok, j} = Queue.dequeue()
      assert j.args == %{"n" => 1}
    end

    test "increments attempts" do
      {:ok, _} = Queue.enqueue("W", %{})
      {:ok, j} = Queue.dequeue()
      assert j.attempts == 1
    end

    test "skips jobs scheduled in the future" do
      {:ok, _} =
        Queue.enqueue("W", %{},
          scheduled_at: DateTime.utc_now() |> DateTime.add(3600) |> DateTime.truncate(:second)
        )

      assert :empty = Queue.dequeue()
    end
  end

  describe "SKIP LOCKED behavior" do
    test "parallel dequeues claim disjoint jobs" do
      for _ <- 1..20, do: Queue.enqueue("W", %{})

      tasks =
        for _ <- 1..10 do
          Task.async(fn ->
            Ecto.Adapters.SQL.Sandbox.allow(Repo, self(), self())
            Queue.dequeue()
          end)
        end

      results = Task.await_many(tasks, 5_000)
      ids = results |> Enum.map(fn {:ok, j} -> j.id end) |> Enum.sort()
      assert length(Enum.uniq(ids)) == 10
    end
  end

  describe "fail/2 backoff" do
    test "reschedules with exponential backoff if under max_attempts" do
      {:ok, _} = Queue.enqueue("W", %{})
      {:ok, j} = Queue.dequeue()

      {:ok, j} = Queue.fail(j, "boom")
      assert j.state == "pending"
      assert DateTime.compare(j.scheduled_at, DateTime.utc_now()) == :gt
    end

    test "marks as dead after max_attempts" do
      {:ok, _} = Queue.enqueue("W", %{}, max_attempts: 1)
      {:ok, j} = Queue.dequeue()

      {:ok, j} = Queue.fail(j, "nope")
      assert j.state == "dead"
    end
  end
end
```

---

## Benchmark

```elixir
# bench/queue_bench.exs
alias JobQueue.{Queue, Repo}
alias JobQueue.Schemas.Job

Repo.delete_all(Job)
for _ <- 1..1_000, do: Queue.enqueue("W", %{})

Benchee.run(
  %{
    "dequeue single" => fn -> Queue.dequeue() end
  },
  parallel: 8,
  time: 5, warmup: 2
)
```

**Target**: p99 dequeue < 3 ms with 8 parallel workers. If you see > 20 ms, the
`FOR UPDATE SKIP LOCKED` clause is missing or the partial index is not being used.

---

## Deep Dive

Ecto queries compile to SQL, but the translation is not always obvious. Complex preload patterns spawn subqueries for each association level—a naive nested preload can explode into hundreds of queries. Window functions and CTEs (Common Table Expressions) exist in Ecto but require raw fragments, making the boundary between Elixir and SQL explicit. For high-throughput systems, consider schemaless queries and streaming to defer memory allocation; loading 1M records as `Ecto.Repo.all/2` marshals everything into memory. Multi-tenancy via row-level database policies is cleaner than application-level filtering and leverages PostgreSQL's built-in enforcement. Zero-downtime migrations require careful orchestration: add columns before code that uses them, remove columns after code stops referencing them. Lock contention on hot rows kills throughput—use FOR UPDATE in transactions and understand when Ecto's optimistic locking is sufficient.
## Advanced Considerations

Advanced Ecto usage at scale requires understanding transaction semantics, locking strategies, and query performance under concurrent load. Ecto transactions are database transactions, not application-level transactions; they don't isolate against application-level concurrency issues. Using `:serializable` isolation level prevents anomalies but significantly impacts throughput. The choice between row-level locking with `for_update()` and optimistic locking with version columns affects both concurrency and latency. Deadlocks are not failures in Ecto; they're expected outcomes that require retry logic and careful key ordering to minimize.

Preload optimization is subtle — using `preload` for related data prevents N+1 queries but can create large intermediate result sets that exceed memory limits. Pagination with preloads requires careful consideration of whether to paginate before or after preloading related data. Custom types and schemaless queries provide flexibility but bypass Ecto's validation layer, creating opportunities for subtle bugs where invalid data sneaks into your database. The interaction between Ecto's change tracking and ETS caching can create stale data issues if not carefully managed across process boundaries.

Zero-downtime migrations require a different mental model than traditional migration scripts. Adding a column is fast; backfilling millions of rows is slow and can lock tables. Deploying code that expects the new column before the migration completes causes failures. Implement feature flags and dual-write patterns for truly zero-downtime deployments. Full-text search with PostgreSQL's tsearch requires careful index maintenance and stop-word configuration; performance characteristics change dramatically with language-specific settings and custom dictionaries.


## Deep Dive: Ecto Patterns and Production Implications

Ecto queries are composable, built up incrementally with pipes. Testing queries requires understanding that a query is lazy—until you call Repo.all, Repo.one, or Repo.update_all, no SQL is executed. This allows for property-based testing of query builders without hitting the database. Production bugs in complex queries often stem from incorrect scoping or ambiguous joins.

---

## Trade-offs and production gotchas

**1. Stuck-in-`running` jobs.** A worker crash after `UPDATE state='running'` but before
completion leaves a zombie. Add a reaper that requeues rows with
`state = 'running' AND updated_at < now() - interval '5 minutes'`.

**2. Hot-row contention on high-QPS queues.** 100 workers all hitting the same queue
with the same scheduled_at sec-precision timestamp can cluster on the same row. Add
millisecond precision or a jitter in `scheduled_at`.

**3. `SKIP LOCKED` requires Postgres 9.5+.** Check compatibility. Older systems need
advisory locks (`pg_try_advisory_xact_lock`).

**4. `ORDER BY scheduled_at` without index breaks FIFO at scale.** The partial index
`(queue, scheduled_at) WHERE state = 'pending'` is essential.

**5. Transactions leaking.** If `Queue.complete/1` is called on a job whose claim
transaction has already rolled back (e.g., DB reconnect), the update succeeds on a
`dead` row. Check state before transitioning: `where: j.state == "running"`.

**6. When NOT to build your own queue.** If your throughput exceeds a few hundred
jobs/second per queue or you need cron, retries, unique jobs, and dashboards — use
Oban. This exercise's design is what Oban's core already does, production-grade.

---

## Reflection

Your queue sits at 500 jobs/second sustained. The partial index is small, dequeue is
fast, everything is healthy. A product launch pushes throughput to 5,000 jobs/second
for 10 minutes. `FOR UPDATE SKIP LOCKED` still works, but latency rises from 2 ms to
40 ms p99. Where does the bottleneck move — index contention, WAL bandwidth, connection
pool, autovacuum? What do you measure first, and what metric triggers a scale-out
decision between "add workers" vs. "partition the queue"?

---

## Executable Example

```elixir
defp deps do
  [
    {:ecto_sql, "~> 3.12"},
    {:postgrex, "~> 0.19"},
    {:benchee, "~> 1.3", only: :dev}
  ]
end

# lib/job_queue/schemas/job.ex
defmodule JobQueue.Schemas.Job do
  use Ecto.Schema
  import Ecto.Changeset

  schema "jobs" do
    field :queue, :string, default: "default"
    field :worker, :string, null: false
    field :args, :map, default: %{}
    field :state, :string, default: "pending"
    field :attempts, :integer, default: 0
    field :max_attempts, :integer, default: 3
    field :scheduled_at, :utc_datetime
    field :completed_at, :utc_datetime
    field :last_error, :string
    timestamps()
  end

  def changeset(job, attrs) do
    job
    |> cast(attrs, [:queue, :worker, :args, :scheduled_at, :max_attempts])
    |> validate_required([:worker])
    |> put_scheduled_at()
  end

  defp put_scheduled_at(cs) do
    case get_field(cs, :scheduled_at) do
      nil -> put_change(cs, :scheduled_at, DateTime.utc_now() |> DateTime.truncate(:second))
      _ -> cs
    end
  end
end

# priv/repo/migrations/20260101000000_create_jobs.exs
defmodule JobQueue.Repo.Migrations.CreateJobs do
  use Ecto.Migration

  def change do
    create table(:jobs) do
      add :queue, :string, null: false, default: "default"
      add :worker, :string, null: false
      add :args, :map, null: false, default: %{}
      add :state, :string, null: false, default: "pending"
      add :attempts, :integer, null: false, default: 0
      add :max_attempts, :integer, null: false, default: 3
      add :scheduled_at, :utc_datetime, null: false
      add :completed_at, :utc_datetime
      add :last_error, :text
      timestamps()
    end

    # Partial index for the hot pull query
    create index(:jobs, [:queue, :scheduled_at],
             where: "state = 'pending'",
             name: :jobs_pending_idx)

    create index(:jobs, [:state])
  end
end

# lib/job_queue/queue.ex
defmodule JobQueue.Queue do
  import Ecto.Query

  alias JobQueue.Repo
  alias JobQueue.Schemas.Job

  @spec enqueue(module(), map(), keyword()) :: {:ok, Job.t()}
  def enqueue(worker, args, opts \\ []) do
    attrs = %{
      worker: to_string(worker),
      args: args,
      queue: Keyword.get(opts, :queue, "default"),
      scheduled_at: Keyword.get(opts, :scheduled_at),
      max_attempts: Keyword.get(opts, :max_attempts, 3)
    }

    %Job{}
    |> Job.changeset(attrs)
    |> Repo.insert()
  end

  @doc """
  Pulls one ready job and marks it as running.

  Returns `{:ok, job}` or `:empty`. Uses `FOR UPDATE SKIP LOCKED` so concurrent
  workers never block each other.
  """
  @spec dequeue(String.t()) :: {:ok, Job.t()} | :empty
  def dequeue(queue \\ "default") do
    now = DateTime.utc_now() |> DateTime.truncate(:second)

    Repo.transaction(fn ->
      candidate =
        from(j in Job,
          where:
            j.queue == ^queue and
              j.state == "pending" and
              j.scheduled_at <= ^now,
          order_by: [asc: j.scheduled_at, asc: j.id],
          limit: 1,
          lock: "FOR UPDATE SKIP LOCKED"
        )
        |> Repo.one()

      case candidate do
        nil ->
          :empty

        job ->
          {:ok, updated} =
            job
            |> Ecto.Changeset.change(state: "running", attempts: job.attempts + 1)
            |> Repo.update()

          updated
      end
    end)
    |> case do
      {:ok, :empty} -> :empty
      {:ok, job} -> {:ok, job}
    end
  end

  @spec complete(Job.t()) :: {:ok, Job.t()}
  def complete(%Job{} = job) do
    job
    |> Ecto.Changeset.change(state: "done", completed_at: now())
    |> Repo.update()
  end

  @spec fail(Job.t(), String.t()) :: {:ok, Job.t()}
  def fail(%Job{} = job, reason) do
  end
    next_state = if job.attempts >= job.max_attempts, do: "dead", else: "pending"

    job
    |> Ecto.Changeset.change(
      state: next_state,
      last_error: reason,
      scheduled_at: backoff(job.attempts)
    )
    |> Repo.update()
  end

  defp backoff(attempts) do
    secs = :math.pow(2, attempts) |> trunc()
    DateTime.utc_now() |> DateTime.add(secs, :second) |> DateTime.truncate(:second)
  end

  defp now, do: DateTime.utc_now() |> DateTime.truncate(:second)
end

# lib/job_queue/worker.ex
defmodule JobQueue.Worker do
  end
  use GenServer
  require Logger

  alias JobQueue.Queue

  @poll_ms 200

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: Keyword.fetch!(opts, :name))

  @impl true
  def init(opts) do
    queue = Keyword.get(opts, :queue, "default")
    schedule_poll()
    {:ok, %{queue: queue}}
  end

  @impl true
  def handle_info(:poll, %{queue: queue} = state) do
    case Queue.dequeue(queue) do
      {:ok, job} -> run(job)
      :empty -> :ok
    end

    schedule_poll()
    {:noreply, state}
  end

  defp run(job) do
    module = String.to_existing_atom("Elixir." <> job.worker)

    try do
      :ok = module.perform(job.args)
      Queue.complete(job)
    rescue
      e ->
        Logger.error("job #{job.id} failed: #{inspect(e)}")
        Queue.fail(job, Exception.message(e))
    end
  end

  defp schedule_poll, do: Process.send_after(self(), :poll, @poll_ms)
end

defmodule Main do
  def main do
      # Demonstrating 283-row-level-locking
      :ok
  end
end

Main.main()
end
```
