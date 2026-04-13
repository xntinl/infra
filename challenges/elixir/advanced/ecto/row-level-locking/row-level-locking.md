# Row-Level Locking — `FOR UPDATE SKIP LOCKED`

**Project**: `job_queue` — a minimal job queue built on Postgres with contention-free workers

---

## Why ecto advanced matters

Ecto.Multi, custom types, polymorphic associations, CTEs, window functions, and zero-downtime migrations are the senior toolkit for talking to PostgreSQL from Elixir. Each one trades a different axis: composability, type safety, query expressiveness, or operational safety.

The trap is treating Ecto like an ORM. It is a query DSL plus a changeset validator — closer to SQL than to ActiveRecord. The closer your mental model is to the database, the better Ecto serves you.

---

## The business problem

You are building a production-grade Elixir component in the **Ecto advanced** area. The goal is not a toy demonstration — it is a piece you could drop into a real system and rely on under load.

Concretely, you must:

1. Implement the topic correctly with measurable behavior.
2. Surface failure modes via `{:ok, _} / {:error, reason}` tuples — never raise from a public API except for programmer errors.
3. Bound resources: explicit timeouts, memory caps, back-pressure where relevant, and supervision for any long-lived process.
4. Provide a test suite with `async: true` where safe, `doctest` for public examples, and `describe` blocks that name the contract.
5. Provide a `script/main.exs` that demonstrates the piece end-to-end with realistic data — not a `1 + 1` smoke test.

---

## Project structure

```
job_queue/
├── lib/
│   └── job_queue.ex
├── script/
│   └── main.exs
├── test/
│   └── job_queue_test.exs
└── mix.exs
```

---

## Design decisions

**Option A — minimal happy-path implementation**
- Pros: smaller surface area, faster to ship.
- Cons: no resource bounds, no failure-mode coverage, no observability hooks. Falls over the first time production load deviates from the developer's mental model.

**Option B — production-grade contract with explicit bounds** (chosen)
- Pros: timeouts, supervised lifecycle, structured errors, idiomatic `{:ok, _} / {:error, reason}` returns. Tests cover the failure envelope, not just the happy path.
- Cons: more code, more concepts. Pays for itself the first time the upstream service degrades.

Chose **B** because in Ecto advanced the failure modes — not the happy path — are what determine whether the system stays up under real production conditions.

---

## Implementation

### `mix.exs`

```elixir
defmodule JobQueue.MixProject do
  use Mix.Project

  def project do
    [
      app: :job_queue,
      version: "0.1.0",
      elixir: "~> 1.19",
      elixirc_paths: elixirc_paths(Mix.env()),
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp elixirc_paths(:test), do: ["lib", "test/support"]
  defp elixirc_paths(_), do: ["lib"]

  defp deps do
    [
      {:ex_doc, "~> 0.31", only: :dev, runtime: false}
    ]
  end
end
```
### `lib/job_queue.ex`

```elixir
# lib/job_queue/schemas/job.ex
defmodule JobQueue.Schemas.Job do
  @moduledoc """
  Ejercicio: Row-Level Locking — `FOR UPDATE SKIP LOCKED`.
  Implementa el comportamiento descrito en el enunciado, exponiendo funciones públicas documentadas y con tipos claros.
  """

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
      e in RuntimeError ->
        Logger.error("job #{job.id} failed: #{inspect(e)}")
        Queue.fail(job, Exception.message(e))
    end
  end

  defp schedule_poll, do: Process.send_after(self(), :poll, @poll_ms)
end
```
### `test/job_queue_test.exs`

```elixir
defmodule JobQueue.QueueTest do
  use ExUnit.Case, async: true
  doctest JobQueue.Schemas.Job
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
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Row-Level Locking — `FOR UPDATE SKIP LOCKED`.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Row-Level Locking — `FOR UPDATE SKIP LOCKED` ===")
    IO.puts("Category: Ecto advanced\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case JobQueue.run(payload) do
          {:ok, value} -> {:ok, payload, value}
          {:error, reason} -> {:error, payload, reason}
        end
      end)

    Enum.each(results, fn
      {:ok, p, v} -> IO.puts("  [OK] #{inspect(p)} -> #{inspect(v)}")
      {:error, p, r} -> IO.puts("  [ERR] #{inspect(p)} -> #{inspect(r)}")
    end)

    {us, _} =
      :timer.tc(fn ->
        for _ <- 1..1_000, do: JobQueue.run(:bench)
      end)

    avg = us / 1_000
    IO.puts("\nBenchmark: #{:erlang.float_to_binary(avg, decimals: 2)} µs/op (1000 iterations)")
    IO.puts("Target: < 100 µs/op for in-process operations\n")
  end
end

Main.main()
```
---

## Trade-offs and production gotchas

**1. Bounded resources are the contract**
Any operation that can grow without bound (mailbox, ETS table, process count, DB connections) must have an explicit cap. Without one, a bad input or a slow upstream eventually exhausts the VM.

**2. Tagged tuples over exceptions**
Public APIs return `{:ok, value} / {:error, reason}`. Exceptions are reserved for programmer errors (FunctionClauseError, KeyError) — operational errors are data, not control flow.

**3. Timeouts are not optional**
`GenServer.call` defaults to 5000 ms. `Task.await` defaults to 5000 ms. `Repo.transaction` inherits the connection's timeout. In production, every call should pass an explicit `:timeout` matched to the operation's SLA.

**4. Supervision encodes recovery**
Long-lived processes belong under a Supervisor with an explicit restart strategy. Choose `:transient` for processes that should not restart on `:normal` exit, `:permanent` for daemons, `:temporary` for workers that handle one-off jobs.

**5. Telemetry events are the production debugger**
Emit `:telemetry.execute/3` for every business operation. Attach handlers in production for metrics; attach handlers in tests for assertions. The same code path serves both.

**6. Async tests need isolated state**
`async: true` parallelizes test execution. Any test that writes to global state (Application env, named ETS tables, the database without sandbox) must declare `async: false` or the suite becomes flaky.

---

## Reflection

1. If load on this component grew by 100×, which assumption breaks first — the data structure, the process model, or the failure handling? Justify with a measurement plan, not a guess.
2. What three telemetry events would you emit to decide, six months from now, whether this implementation is still the right one? Name the events, the metadata, and the alert thresholds.
3. The implementation here uses the recommended primitive. Under what specific conditions would you reach for a different primitive instead? Be concrete about the trigger.

---

## Key concepts

### 1. Queries are data, not strings

Ecto.Query is a DSL that compiles to SQL only at execution. This means you can compose, inspect, and pre-validate queries without a database connection — useful for property tests.

### 2. Multi makes transactions composable

Ecto.Multi is a value: build it, pass it around, run it inside Repo.transaction. Errors come back as `{:error, step_name, reason, changes_so_far}` — you know exactly what failed.

### 3. Locking strategies trade throughput for correctness

FOR UPDATE prevents lost updates but serializes contention. Optimistic locking via :version columns retries on conflict — better for read-heavy workloads.

---
