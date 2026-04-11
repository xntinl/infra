# Ecto: Schemas, Changesets, and Queries

**Project**: `task_queue` — built incrementally across the intermediate level

---

## Project context

`task_queue` has been persisting jobs only in memory. When the node restarts, all pending and completed jobs are lost. The ops team needs a persistent job store so that completed job history is queryable, failed jobs can be retried after a restart, and auditors can reconstruct what happened.

Project structure at this point:

```
task_queue/
├── lib/
│   └── task_queue/
│       ├── application.ex
│       ├── jobs/
│       │   ├── job.ex              # ← you implement the schema
│       │   └── job_store.ex        # ← you implement the query interface
│       ├── worker.ex
│       ├── queue_server.ex
│       ├── scheduler.ex
│       └── registry.ex
├── priv/
│   └── repo/
│       └── migrations/
│           └── 20240101000000_create_jobs.exs  # ← you implement this
├── test/
│   └── task_queue/
│       └── ecto_test.exs           # given tests — must pass without modification
├── config/
│   └── test.exs                    # ← add Repo config
└── mix.exs                         # ← add Ecto and Postgrex/SQLite
```

Add to `mix.exs`:

```elixir
{:ecto_sql, "~> 3.11"},
{:postgrex, ">= 0.0.0"}            # or {:ecto_sqlite3, "~> 0.15"} for SQLite
```

---

## The business problem

The product team wants to:
1. Query all jobs of type `"send_email"` that failed in the last 24 hours
2. Retry all jobs with status `:failed` and `retry_count < 3`
3. Show job completion history for a given customer ID

These queries require a structured schema, validated changesets, and composable query fragments. The in-memory `QueueServer` cannot support any of this.

---

## Why Ecto changesets and not direct struct construction

You could insert a job into the database using a bare struct:

```elixir
%TaskQueue.Jobs.Job{type: "send_email", status: :pending}
|> Repo.insert()
```

This bypasses all validation. If `type` is nil, the database constraint catches it — but the error message is a raw Postgres constraint violation, not a user-friendly validation error. If `status` is `:invalid_status`, it reaches the database as-is.

Changesets intercept writes and validate data before it reaches the database:

```elixir
Job.changeset(%Job{}, %{type: "send_email", status: :pending})
# Returns a changeset struct with :valid? field and :errors list
# Only insert if changeset.valid? == true
```

This separates three concerns:
- **Structure**: the schema defines columns and their types
- **Validation**: the changeset defines business rules (required fields, valid values)
- **Persistence**: `Repo.insert/2` writes a valid changeset

---

## Why `Ecto.Query` and not raw SQL strings

Raw SQL is untyped, injection-prone, and database-specific:

```elixir
# Wrong — SQL injection risk, PostgreSQL-specific syntax, no type checking
Repo.query!("SELECT * FROM jobs WHERE type = '#{type}' AND status = 'failed'")
```

`Ecto.Query` is composable, type-safe, and database-agnostic:

```elixir
# Right — composable, parameterized, portable
from(j in Job, where: j.type == ^type and j.status == :failed)
|> Repo.all()
```

Queries can be built incrementally and reused across functions without string manipulation.

---

## Implementation

### Step 1: Repo module and configuration

```elixir
# lib/task_queue/repo.ex
defmodule TaskQueue.Repo do
  use Ecto.Repo,
    otp_app: :task_queue,
    adapter: Ecto.Adapters.Postgres
end
```

Add the Repo to the supervision tree in `lib/task_queue/application.ex`:

```elixir
children = [
  TaskQueue.Repo,
  # ... rest of children
]
```

Add Repo configuration to `config/config.exs`:

```elixir
# config/config.exs
config :task_queue, TaskQueue.Repo,
  database: "task_queue_dev",
  username: "postgres",
  password: "postgres",
  hostname: "localhost"
```

Add test configuration to `config/test.exs` (required for `Ecto.Adapters.SQL.Sandbox`):

```elixir
# config/test.exs
config :task_queue, TaskQueue.Repo,
  username: "postgres",
  password: "postgres",
  hostname: "localhost",
  database: "task_queue_test#{System.get_env("MIX_TEST_PARTITION")}",
  pool: Ecto.Adapters.SQL.Sandbox,
  pool_size: 10
```

Add to `test/test_helper.exs`:

```elixir
Ecto.Adapters.SQL.Sandbox.mode(TaskQueue.Repo, :manual)
```

### Step 2: Migration — `priv/repo/migrations/20240101000000_create_jobs.exs`


```elixir
defmodule TaskQueue.Repo.Migrations.CreateJobs do
  use Ecto.Migration

  def change do
    create table(:jobs) do
      # TODO: add columns:
      # - type (string, not null)
      # - args (map, default %{})
      # - status (string, not null, default "pending")
      # - retry_count (integer, not null, default 0)
      # - last_error (string, nullable)
      # - scheduled_at (utc_datetime, nullable)
      # HINT:
      # add :type, :string, null: false
      # add :args, :map, default: %{}
      # add :status, :string, null: false, default: "pending"
      # add :retry_count, :integer, null: false, default: 0
      # add :last_error, :string
      # add :scheduled_at, :utc_datetime

      timestamps()
    end

    # TODO: add an index on (status, type) for efficient filtering
    # HINT: create index(:jobs, [:status, :type])
  end
end
```

### Step 3: `lib/task_queue/jobs/job.ex` — schema and changeset

```elixir
defmodule TaskQueue.Jobs.Job do
  @moduledoc """
  Ecto schema for a persisted job.

  Status values: :pending, :running, :completed, :failed
  """

  use Ecto.Schema
  import Ecto.Changeset

  @valid_statuses ~w(pending running completed failed)

  schema "jobs" do
    # TODO: define fields matching the migration:
    # field :type, :string
    # field :args, :map, default: %{}
    # field :status, :string, default: "pending"
    # field :retry_count, :integer, default: 0
    # field :last_error, :string
    # field :scheduled_at, :utc_datetime

    timestamps()
  end

  @doc """
  Validates job attributes for insertion.

  Required: `:type`
  Optional: `:args`, `:status`, `:retry_count`, `:scheduled_at`

  ## Examples

      iex> TaskQueue.Jobs.Job.changeset(%TaskQueue.Jobs.Job{}, %{type: "send_email"})
      #Ecto.Changeset<valid?: true, ...>

      iex> TaskQueue.Jobs.Job.changeset(%TaskQueue.Jobs.Job{}, %{})
      #Ecto.Changeset<valid?: false, errors: [type: {"can't be blank", ...}], ...>

  """
  @spec changeset(t(), map()) :: Ecto.Changeset.t()
  def changeset(job, attrs) do
    job
    |> cast(attrs, [:type, :args, :status, :retry_count, :last_error, :scheduled_at])
    # TODO: validate that :type is required
    # HINT: |> validate_required([:type])
    # TODO: validate that :status is one of @valid_statuses
    # HINT: |> validate_inclusion(:status, @valid_statuses)
    # TODO: validate that :retry_count is >= 0
    # HINT: |> validate_number(:retry_count, greater_than_or_equal_to: 0)
  end

  @doc """
  Changeset for updating job status (e.g., marking as failed with an error).
  """
  @spec status_changeset(t(), map()) :: Ecto.Changeset.t()
  def status_changeset(job, attrs) do
    job
    |> cast(attrs, [:status, :last_error, :retry_count])
    # TODO: validate :status inclusion
    # TODO: validate :retry_count >= 0
  end
end
```

### Step 4: `lib/task_queue/jobs/job_store.ex` — query interface

```elixir
defmodule TaskQueue.Jobs.JobStore do
  @moduledoc """
  Query interface for persisted jobs.

  All public functions return `{:ok, result}` or `{:error, reason}`.
  Queries are composable — filter functions return queryable fragments.
  """

  import Ecto.Query
  alias TaskQueue.{Repo, Jobs.Job}

  @doc """
  Inserts a new job. Returns `{:ok, job}` or `{:error, changeset}`.

  ## Examples

      iex> TaskQueue.Jobs.JobStore.insert(%{type: "send_email", args: %{to: "user@example.com"}})
      {:ok, %TaskQueue.Jobs.Job{type: "send_email", status: "pending"}}

  """
  @spec insert(map()) :: {:ok, Job.t()} | {:error, Ecto.Changeset.t()}
  def insert(attrs) do
    # TODO: build a changeset with Job.changeset/2 and call Repo.insert/1
    # HINT: %Job{} |> Job.changeset(attrs) |> Repo.insert()
  end

  @doc """
  Returns all pending jobs ordered by insertion time (oldest first).
  """
  @spec list_pending() :: [Job.t()]
  def list_pending do
    # TODO: query jobs where status == "pending", ordered by inserted_at asc
    # HINT:
    # from(j in Job, where: j.status == "pending", order_by: [asc: j.inserted_at])
    # |> Repo.all()
  end

  @doc """
  Returns all failed jobs with fewer than `max_retries` attempts.
  """
  @spec list_retryable(non_neg_integer()) :: [Job.t()]
  def list_retryable(max_retries \\ 3) do
    # TODO: query jobs where status == "failed" and retry_count < max_retries
    # HINT:
    # from(j in Job,
    #   where: j.status == "failed" and j.retry_count < ^max_retries,
    #   order_by: [asc: j.inserted_at]
    # )
    # |> Repo.all()
  end

  @doc """
  Marks a job as failed, increments retry_count, and records the error.
  """
  @spec mark_failed(integer(), String.t()) :: {:ok, Job.t()} | {:error, Ecto.Changeset.t()}
  def mark_failed(job_id, error_message) do
    # TODO: fetch the job, then apply status_changeset with:
    #   status: "failed", last_error: error_message, retry_count: job.retry_count + 1
    # HINT:
    # job = Repo.get!(Job, job_id)
    # job
    # |> Job.status_changeset(%{
    #     status: "failed",
    #     last_error: error_message,
    #     retry_count: job.retry_count + 1
    #   })
    # |> Repo.update()
  end

  @doc """
  Returns jobs matching a dynamic filter map.

  Supported filter keys: `:type`, `:status`

  ## Examples

      iex> TaskQueue.Jobs.JobStore.filter(%{type: "send_email", status: "pending"})
      [%Job{type: "send_email", status: "pending"}, ...]

  """
  @spec filter(map()) :: [Job.t()]
  def filter(filters) when is_map(filters) do
    # TODO: start with `from(j in Job)` and compose filters dynamically
    # For each key in filters, add a where clause
    # HINT:
    # Enum.reduce(filters, from(j in Job), fn
    #   {:type, type}, query   -> where(query, [j], j.type == ^type)
    #   {:status, s}, query    -> where(query, [j], j.status == ^s)
    #   _, query               -> query
    # end)
    # |> Repo.all()
  end
end
```

### Step 5: Given tests — must pass without modification

```elixir
# test/task_queue/ecto_test.exs
defmodule TaskQueue.EctoTest do
  use ExUnit.Case, async: false

  alias TaskQueue.Jobs.{Job, JobStore}

  setup do
    # Using Ecto Sandbox for test isolation
    :ok = Ecto.Adapters.SQL.Sandbox.checkout(TaskQueue.Repo)
  end

  describe "Job.changeset/2" do
    test "valid changeset with required type" do
      changeset = Job.changeset(%Job{}, %{type: "send_email"})
      assert changeset.valid?
    end

    test "invalid changeset missing type" do
      changeset = Job.changeset(%Job{}, %{})
      refute changeset.valid?
      assert {:type, _} = hd(changeset.errors)
    end

    test "invalid changeset with bad status" do
      changeset = Job.changeset(%Job{}, %{type: "noop", status: "invalid_status"})
      refute changeset.valid?
    end

    test "invalid changeset with negative retry_count" do
      changeset = Job.changeset(%Job{}, %{type: "noop", retry_count: -1})
      refute changeset.valid?
    end
  end

  describe "JobStore.insert/1" do
    test "inserts a valid job" do
      assert {:ok, job} = JobStore.insert(%{type: "send_email", args: %{to: "user@example.com"}})
      assert job.id != nil
      assert job.type == "send_email"
      assert job.status == "pending"
      assert job.retry_count == 0
    end

    test "returns changeset error for invalid job" do
      assert {:error, changeset} = JobStore.insert(%{})
      refute changeset.valid?
    end
  end

  describe "JobStore.list_pending/0" do
    test "returns only pending jobs" do
      {:ok, _} = JobStore.insert(%{type: "noop"})
      {:ok, j2} = JobStore.insert(%{type: "noop"})
      JobStore.mark_failed(j2.id, "network error")

      pending = JobStore.list_pending()
      assert length(pending) >= 1
      assert Enum.all?(pending, fn j -> j.status == "pending" end)
    end
  end

  describe "JobStore.list_retryable/1" do
    test "returns failed jobs with retry_count below max" do
      {:ok, job} = JobStore.insert(%{type: "send_email"})
      {:ok, _}   = JobStore.mark_failed(job.id, "timeout")

      retryable = JobStore.list_retryable(3)
      assert Enum.any?(retryable, fn j -> j.id == job.id end)
    end

    test "excludes jobs at or above max_retries" do
      {:ok, job} = JobStore.insert(%{type: "send_email"})
      # Exhaust retries
      {:ok, j1} = JobStore.mark_failed(job.id, "e1")
      {:ok, j2} = JobStore.mark_failed(j1.id, "e2")
      {:ok, _}  = JobStore.mark_failed(j2.id, "e3")

      retryable = JobStore.list_retryable(3)
      refute Enum.any?(retryable, fn j -> j.id == job.id end)
    end
  end

  describe "JobStore.filter/1" do
    test "filters by type" do
      {:ok, _} = JobStore.insert(%{type: "send_email"})
      {:ok, _} = JobStore.insert(%{type: "send_sms"})

      results = JobStore.filter(%{type: "send_email"})
      assert Enum.all?(results, fn j -> j.type == "send_email" end)
    end

    test "filters by status" do
      {:ok, job} = JobStore.insert(%{type: "noop"})
      JobStore.mark_failed(job.id, "error")

      failed = JobStore.filter(%{status: "failed"})
      assert Enum.any?(failed, fn j -> j.id == job.id end)
    end

    test "combines multiple filters" do
      {:ok, _} = JobStore.insert(%{type: "send_email"})
      {:ok, j2} = JobStore.insert(%{type: "send_email"})
      JobStore.mark_failed(j2.id, "error")

      results = JobStore.filter(%{type: "send_email", status: "failed"})
      assert Enum.all?(results, fn j -> j.type == "send_email" and j.status == "failed" end)
    end
  end
end
```

### Step 6: Run migrations and tests

```bash
mix deps.get
mix ecto.create
mix ecto.migrate
mix test test/task_queue/ecto_test.exs --trace
```

---

## Trade-off analysis

| Aspect | Ecto changeset | Direct struct insert | Raw SQL |
|--------|---------------|----------------------|---------|
| Validation before DB | yes | no | no |
| Error messages | structured `{field, {msg, opts}}` | database constraint error | raw driver error |
| Composable queries | yes — `Ecto.Query` | N/A | limited — string concat |
| SQL injection safety | yes — parameterized | N/A | manual |
| Schema evolution | migrations | manual `ALTER TABLE` | manual |

Reflection question: `Repo.get!/2` raises on not found. `Repo.get/2` returns `nil`. When would you prefer `get!` in production code, and when would `nil` be the safer return?

---

## Common production mistakes

**1. Using `cast/3` without `validate_required/2`**

`cast/3` silently drops fields not in the allowed list and sets missing fields to `nil`. Without `validate_required/2`, a changeset with a nil required field is marked valid and reaches the database — where the `NOT NULL` constraint fires with a cryptic error.

**2. N+1 queries when loading associations**

```elixir
# Wrong — runs one query per job to load its worker
jobs = Repo.all(Job)
Enum.map(jobs, fn job -> Repo.preload(job, :worker) end)

# Right — single query with JOIN
Repo.all(from j in Job, preload: [:worker])
```

**3. `Repo.update/1` on a struct instead of a changeset**

`Repo.update` requires a changeset, not a struct. Passing a struct directly updates ALL fields including `inserted_at`, which bypasses `updated_at` tracking.

**4. Not using `Ecto.Adapters.SQL.Sandbox` in tests**

Without sandbox mode, each test writes to the real database and leaves data behind, contaminating subsequent tests. Configure the repo with `pool: Ecto.Adapters.SQL.Sandbox` in `config/test.exs`.

**5. Building WHERE clauses with string interpolation**

```elixir
# Wrong — SQL injection
Repo.query!("SELECT * FROM jobs WHERE type = '#{type}'")

# Right — parameterized
from(j in Job, where: j.type == ^type) |> Repo.all()
```

---

## Resources

- [Ecto documentation — official hex](https://hexdocs.pm/ecto/Ecto.html)
- [Ecto.Changeset — validation and casting](https://hexdocs.pm/ecto/Ecto.Changeset.html)
- [Ecto.Query — composable queries](https://hexdocs.pm/ecto/Ecto.Query.html)
- [Ecto.Adapters.SQL.Sandbox — test isolation](https://hexdocs.pm/ecto_sql/Ecto.Adapters.SQL.Sandbox.html)
