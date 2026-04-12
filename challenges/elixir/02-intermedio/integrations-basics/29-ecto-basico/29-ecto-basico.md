# Ecto: Schemas, Changesets, and Queries

## Goal

Build a `task_queue` persistent job store using Ecto with schemas, changesets for validation, and composable queries. Learn why changesets intercept writes before they reach the database, and why `Ecto.Query` is safer and more composable than raw SQL.

---

## Why Ecto changesets and not direct struct construction

You could insert a job using a bare struct, bypassing all validation. If `type` is nil, the database constraint catches it -- but the error message is a raw Postgres constraint violation. Changesets intercept writes and validate data before it reaches the database, separating three concerns:
- **Structure**: the schema defines columns and types
- **Validation**: the changeset defines business rules
- **Persistence**: `Repo.insert/2` writes a valid changeset

---

## Why `Ecto.Query` and not raw SQL

Raw SQL is untyped, injection-prone, and database-specific. `Ecto.Query` is composable, type-safe, and parameterized. Queries can be built incrementally and reused across functions.

---

## Implementation

### Step 1: `mix.exs`

```elixir
defmodule TaskQueue.MixProject do
  use Mix.Project

  def project do
    [
      app: :task_queue,
      version: "0.1.0",
      elixir: "~> 1.15",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [
      extra_applications: [:logger],
      mod: {TaskQueue.Application, []}
    ]
  end

  defp deps do
    [
      {:ecto_sql, "~> 3.11"},
      {:postgrex, ">= 0.0.0"}
    ]
  end
end
```

### Step 2: `lib/task_queue/application.ex`

```elixir
defmodule TaskQueue.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      TaskQueue.Repo
    ]

    opts = [strategy: :one_for_one, name: TaskQueue.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
```

### Step 3: `lib/task_queue/repo.ex`

```elixir
defmodule TaskQueue.Repo do
  use Ecto.Repo,
    otp_app: :task_queue,
    adapter: Ecto.Adapters.Postgres
end
```

### Step 4: Configuration

```elixir
# config/config.exs
import Config

config :task_queue, TaskQueue.Repo,
  database: "task_queue_dev",
  username: "postgres",
  password: "postgres",
  hostname: "localhost"

import_config "#{config_env()}.exs"
```

```elixir
# config/test.exs
import Config

config :task_queue, TaskQueue.Repo,
  username: "postgres",
  password: "postgres",
  hostname: "localhost",
  database: "task_queue_test#{System.get_env("MIX_TEST_PARTITION")}",
  pool: Ecto.Adapters.SQL.Sandbox,
  pool_size: 10
```

```elixir
# test/test_helper.exs
Ecto.Adapters.SQL.Sandbox.mode(TaskQueue.Repo, :manual)
```

### Step 5: Migration

```elixir
# priv/repo/migrations/20240101000000_create_jobs.exs
defmodule TaskQueue.Repo.Migrations.CreateJobs do
  use Ecto.Migration

  def change do
    create table(:jobs) do
      add :type, :string, null: false
      add :args, :map, default: %{}
      add :status, :string, null: false, default: "pending"
      add :retry_count, :integer, null: false, default: 0
      add :last_error, :string
      add :scheduled_at, :utc_datetime

      timestamps()
    end

    create index(:jobs, [:status, :type])
  end
end
```

### Step 6: `lib/task_queue/jobs/job.ex` -- schema and changeset

The `changeset/2` function uses `cast/3` to whitelist allowed fields, `validate_required/2` to enforce mandatory fields, `validate_inclusion/3` to restrict status to known values, and `validate_number/3` to ensure retry_count is non-negative. Without `validate_required`, a nil `type` would reach the database and trigger a cryptic NOT NULL constraint error instead of a clean `{:error, changeset}`.

```elixir
defmodule TaskQueue.Jobs.Job do
  @moduledoc """
  Ecto schema for a persisted job.

  Status values: pending, running, completed, failed
  """

  use Ecto.Schema
  import Ecto.Changeset

  @valid_statuses ~w(pending running completed failed)

  schema "jobs" do
    field :type, :string
    field :args, :map, default: %{}
    field :status, :string, default: "pending"
    field :retry_count, :integer, default: 0
    field :last_error, :string
    field :scheduled_at, :utc_datetime

    timestamps()
  end

  @doc """
  Validates job attributes for insertion.

  Required: `:type`
  Optional: `:args`, `:status`, `:retry_count`, `:scheduled_at`
  """
  @spec changeset(t(), map()) :: Ecto.Changeset.t()
  def changeset(job, attrs) do
    job
    |> cast(attrs, [:type, :args, :status, :retry_count, :last_error, :scheduled_at])
    |> validate_required([:type])
    |> validate_inclusion(:status, @valid_statuses)
    |> validate_number(:retry_count, greater_than_or_equal_to: 0)
  end

  @doc """
  Changeset for updating job status (e.g., marking as failed with an error).
  """
  @spec status_changeset(t(), map()) :: Ecto.Changeset.t()
  def status_changeset(job, attrs) do
    job
    |> cast(attrs, [:status, :last_error, :retry_count])
    |> validate_inclusion(:status, @valid_statuses)
    |> validate_number(:retry_count, greater_than_or_equal_to: 0)
  end
end
```

### Step 7: `lib/task_queue/jobs/job_store.ex` -- query interface

The `filter/1` function demonstrates composable queries: it starts with `from(j in Job)` and reduces over the filter map, adding `where` clauses dynamically. Each filter key adds a parameterized clause (`^type`), preventing SQL injection.

```elixir
defmodule TaskQueue.Jobs.JobStore do
  @moduledoc """
  Query interface for persisted jobs.
  All public functions return `{:ok, result}` or `{:error, reason}`.
  """

  import Ecto.Query
  alias TaskQueue.{Repo, Jobs.Job}

  @doc """
  Inserts a new job. Returns `{:ok, job}` or `{:error, changeset}`.
  """
  @spec insert(map()) :: {:ok, Job.t()} | {:error, Ecto.Changeset.t()}
  def insert(attrs) do
    %Job{}
    |> Job.changeset(attrs)
    |> Repo.insert()
  end

  @doc """
  Returns all pending jobs ordered by insertion time (oldest first).
  """
  @spec list_pending() :: [Job.t()]
  def list_pending do
    from(j in Job, where: j.status == "pending", order_by: [asc: j.inserted_at])
    |> Repo.all()
  end

  @doc """
  Returns all failed jobs with fewer than `max_retries` attempts.
  """
  @spec list_retryable(non_neg_integer()) :: [Job.t()]
  def list_retryable(max_retries \\ 3) do
    from(j in Job,
      where: j.status == "failed" and j.retry_count < ^max_retries,
      order_by: [asc: j.inserted_at]
    )
    |> Repo.all()
  end

  @doc """
  Marks a job as failed, increments retry_count, and records the error.
  """
  @spec mark_failed(integer(), String.t()) :: {:ok, Job.t()} | {:error, Ecto.Changeset.t()}
  def mark_failed(job_id, error_message) do
    job = Repo.get!(Job, job_id)

    job
    |> Job.status_changeset(%{
      status: "failed",
      last_error: error_message,
      retry_count: job.retry_count + 1
    })
    |> Repo.update()
  end

  @doc """
  Returns jobs matching a dynamic filter map.
  Supported filter keys: `:type`, `:status`
  """
  @spec filter(map()) :: [Job.t()]
  def filter(filters) when is_map(filters) do
    Enum.reduce(filters, from(j in Job), fn
      {:type, type}, query   -> where(query, [j], j.type == ^type)
      {:status, s}, query    -> where(query, [j], j.status == ^s)
      _, query               -> query
    end)
    |> Repo.all()
  end
end
```

### Step 8: Tests

The tests use `Ecto.Adapters.SQL.Sandbox` for isolation. Each test checks out a database connection in sandbox mode, and all changes are rolled back when the test exits. This means tests can run in any order without contaminating each other.

```elixir
# test/task_queue/ecto_test.exs
defmodule TaskQueue.EctoTest do
  use ExUnit.Case, async: false

  alias TaskQueue.Jobs.{Job, JobStore}

  setup do
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

### Step 9: Run

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
| Composable queries | yes -- `Ecto.Query` | N/A | limited -- string concat |
| SQL injection safety | yes -- parameterized | N/A | manual |
| Schema evolution | migrations | manual `ALTER TABLE` | manual |

Use `Repo.get!/2` when absence is a programming error (e.g., loading a job by ID just returned from an insert). Use `Repo.get/2` when absence is expected (e.g., looking up a user by email that may not exist).

---

## Common production mistakes

**1. Using `cast/3` without `validate_required/2`**
`cast/3` silently drops missing fields. Without `validate_required`, a nil required field reaches the database.

**2. N+1 queries when loading associations**
Use `Repo.all(from j in Job, preload: [:worker])` instead of preloading one-by-one.

**3. `Repo.update/1` on a struct instead of a changeset**
`Repo.update` requires a changeset. Passing a struct updates ALL fields.

**4. Not using `Ecto.Adapters.SQL.Sandbox` in tests**
Without sandbox mode, each test writes to the real database and leaves data behind.

**5. Building WHERE clauses with string interpolation**
Use parameterized queries: `where: j.type == ^type`.

---

## Resources

- [Ecto documentation -- official hex](https://hexdocs.pm/ecto/Ecto.html)
- [Ecto.Changeset -- validation and casting](https://hexdocs.pm/ecto/Ecto.Changeset.html)
- [Ecto.Query -- composable queries](https://hexdocs.pm/ecto/Ecto.Query.html)
- [Ecto.Adapters.SQL.Sandbox -- test isolation](https://hexdocs.pm/ecto_sql/Ecto.Adapters.SQL.Sandbox.html)
