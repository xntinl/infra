# Zero-Downtime Migrations — Rename, Backfill, Add NOT NULL

**Project**: `schema_evolution` — phased migrations that never block writes or reads

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
schema_evolution/
├── lib/
│   └── schema_evolution.ex
├── script/
│   └── main.exs
├── test/
│   └── schema_evolution_test.exs
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
defmodule SchemaEvolution.MixProject do
  use Mix.Project

  def project do
    [
      app: :schema_evolution,
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
### `lib/schema_evolution.ex`

```elixir
# priv/repo/migrations/20260101000000_create_users.exs
defmodule SchemaEvolution.Repo.Migrations.CreateUsers do
  @moduledoc """
  Ejercicio: Zero-Downtime Migrations — Rename, Backfill, Add NOT NULL.
  Implementa el comportamiento descrito en el enunciado, exponiendo funciones públicas documentadas y con tipos claros.
  """

  use Ecto.Migration

  def change do
    create table(:users) do
      add :email, :string, null: false
      timestamps()
    end

    create unique_index(:users, [:email])
  end
end

# priv/repo/migrations/20260201000000_phase1_add_primary_email.exs
defmodule SchemaEvolution.Repo.Migrations.Phase1AddPrimaryEmail do
  use Ecto.Migration

  def change do
    # New column is nullable; no table rewrite.
    alter table(:users) do
      add :primary_email, :string
      add :email_verified, :boolean, default: false
    end

    create index(:users, [:primary_email], concurrently: false)
  end
end

# lib/schema_evolution/schemas/user.ex  — phase 1 version
defmodule SchemaEvolution.Schemas.User do
  use Ecto.Schema
  import Ecto.Changeset

  schema "users" do
    field :email, :string              # authoritative
    field :primary_email, :string      # dual-write target
    field :email_verified, :boolean, default: false
    timestamps()
  end

  def changeset(user, attrs) do
    user
    |> cast(attrs, [:email, :primary_email, :email_verified])
    |> validate_required([:email])
    |> write_through_to_primary_email()
    |> unique_constraint(:email)
  end

  # Any write to :email also writes to :primary_email
  defp write_through_to_primary_email(changeset) do
    case get_change(changeset, :email) do
      nil -> changeset
      new -> put_change(changeset, :primary_email, new)
    end
  end
end

# priv/repo/migrations/20260301000000_phase2_backfill_primary_email.exs
defmodule SchemaEvolution.Repo.Migrations.Phase2BackfillPrimaryEmail do
  use Ecto.Migration
  import Ecto.Query

  @disable_ddl_transaction true
  @disable_migration_lock true

  @batch_size 1_000

  def up do
    backfill_in_batches()
  end

  def down, do: :ok

  defp backfill_in_batches do
    stream_batches(0)
    |> Enum.reduce(0, fn batch_ids, total ->
      {n, _} =
        repo().update_all(
          from(u in "users", where: u.id in ^batch_ids, update: [set: [primary_email: u.email]]),
          []
        )

      # Brief pause between batches to let replication lag recover
      Process.sleep(25)
      total + n
    end)
  end

  defp stream_batches(last_id) do
    Stream.unfold(last_id, fn cursor ->
      ids =
        from(u in "users",
          where: u.id > ^cursor and is_nil(u.primary_email),
          order_by: u.id,
          limit: @batch_size,
          select: u.id
        )
        |> repo().all()

      case ids do
        [] -> nil
        ids -> {ids, List.last(ids)}
      end
    end)
  end
end

# priv/repo/migrations/20260401000000_phase3_enforce_not_null.exs
defmodule SchemaEvolution.Repo.Migrations.Phase3EnforceNotNull do
  use Ecto.Migration

  def up do
    # 1. Add constraint without scan. App already writes primary_email for all new rows.
    execute """
    ALTER TABLE users ADD CONSTRAINT primary_email_not_null
    CHECK (primary_email IS NOT NULL) NOT VALID
    """

    # 2. Validate without locking writes (reads existing rows).
    execute "ALTER TABLE users VALIDATE CONSTRAINT primary_email_not_null"

    # 3. Now SET NOT NULL is instant because the CHECK already passed.
    execute "ALTER TABLE users ALTER COLUMN primary_email SET NOT NULL"

    # 4. Drop the redundant CHECK.
    execute "ALTER TABLE users DROP CONSTRAINT primary_email_not_null"
  end

  def down do
    execute "ALTER TABLE users ALTER COLUMN primary_email DROP NOT NULL"
  end
end

# priv/repo/migrations/20260501000000_phase4_drop_email.exs
defmodule SchemaEvolution.Repo.Migrations.Phase4DropEmail do
  use Ecto.Migration

  def change do
    # Instant metadata change. Safe because app no longer reads or writes :email.
    alter table(:users) do
      remove :email, :string
    end
  end
end

# priv/repo/migrations/20260601000000_phase5_add_verified_index.exs
defmodule SchemaEvolution.Repo.Migrations.Phase5AddVerifiedIndex do
  use Ecto.Migration

  @disable_ddl_transaction true
  @disable_migration_lock true

  def change do
    create index(:users, [:email_verified], concurrently: true)
  end
end

# lib/schema_evolution/accounts.ex
defmodule SchemaEvolution.Accounts do
  import Ecto.Query
  alias SchemaEvolution.Repo
  alias SchemaEvolution.Schemas.User

  @doc """
  Lookup that reads whichever column is authoritative in the current phase.

  We use COALESCE so queries work during the backfill window when some rows have
  only :email and others have both.
  """
  def find_by_email(addr) do
    from(u in User,
      where: fragment("COALESCE(?, ?) = ?", u.primary_email, u.email, ^addr),
      limit: 1
    )
    |> Repo.one()
  end

  def create(attrs) do
    %User{}
    |> User.changeset(attrs)
    |> Repo.insert()
  end
end
```
### `test/schema_evolution_test.exs`

```elixir
defmodule SchemaEvolution.PhasedMigrationTest do
  use ExUnit.Case, async: true
  doctest SchemaEvolution.Repo.Migrations.CreateUsers
  alias SchemaEvolution.{Accounts, Repo}
  alias SchemaEvolution.Schemas.User

  setup do
    Ecto.Adapters.SQL.Sandbox.checkout(Repo)
    Ecto.Adapters.SQL.Sandbox.mode(Repo, {:shared, self()})
    Repo.delete_all(User)
    :ok
  end

  describe "dual-write behaviour" do
    test "inserting sets both :email and :primary_email" do
      {:ok, u} = Accounts.create(%{email: "a@b.com"})
      assert u.email == "a@b.com"
      assert u.primary_email == "a@b.com"
    end

    test "updating :email updates :primary_email too" do
      {:ok, u} = Accounts.create(%{email: "a@b.com"})

      {:ok, updated} =
        u
        |> User.changeset(%{email: "x@y.com"})
        |> Repo.update()

      assert updated.primary_email == "x@y.com"
    end
  end

  describe "find_by_email/1 during mixed state" do
    test "finds user by old column when new is nil" do
      # Simulate a row from before Phase 1 by bypassing dual-write
      Repo.insert_all(User, [%{email: "old@x.com", inserted_at: DateTime.utc_now() |> DateTime.truncate(:second), updated_at: DateTime.utc_now() |> DateTime.truncate(:second)}])
      assert %User{} = Accounts.find_by_email("old@x.com")
    end

    test "finds user by new column when both set" do
      {:ok, _} = Accounts.create(%{email: "new@x.com"})
      assert %User{primary_email: "new@x.com"} = Accounts.find_by_email("new@x.com")
    end
  end

  describe "backfill semantics" do
    test "backfill populates primary_email without overwriting existing values" do
      Repo.insert_all(User, [
        %{email: "a@x.com", primary_email: nil, inserted_at: DateTime.utc_now() |> DateTime.truncate(:second), updated_at: DateTime.utc_now() |> DateTime.truncate(:second)},
        %{email: "b@x.com", primary_email: "already@set.com", inserted_at: DateTime.utc_now() |> DateTime.truncate(:second), updated_at: DateTime.utc_now() |> DateTime.truncate(:second)}
      ])

      # Simulate the backfill statement
      Repo.update_all(User, set: [primary_email: :email])
      |> Kernel.then(fn _ -> :ok end)

      Ecto.Adapters.SQL.query!(
        Repo,
        "UPDATE users SET primary_email = email WHERE primary_email IS NULL",
        []
      )

      [a, b] = Repo.all(User)
      assert a.primary_email == a.email
      assert b.primary_email == "already@set.com"
    end
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Zero-Downtime Migrations — Rename, Backfill, Add NOT NULL.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Zero-Downtime Migrations — Rename, Backfill, Add NOT NULL ===")
    IO.puts("Category: Ecto advanced\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case SchemaEvolution.run(payload) do
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
        for _ <- 1..1_000, do: SchemaEvolution.run(:bench)
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
