# Zero-Downtime Migrations — Rename, Backfill, Add NOT NULL

**Project**: `schema_evolution` — phased migrations that never block writes or reads.

---

## Project context

A deployed Phoenix app cannot take a maintenance window to migrate. Yet product wants to
rename `email` to `primary_email`, make a new column `email_verified` NOT NULL, and drop
an obsolete index. A naive `ALTER TABLE` locks the table — users see 500s for the duration.

This exercise codifies the phased pattern every senior Ecto user must know. Each change
happens in steps, each step is safe to deploy independently, and the app keeps running.

```
schema_evolution/
├── lib/
│   └── schema_evolution/
│       ├── application.ex
│       ├── repo.ex
│       ├── accounts.ex              # app-level code during migration phases
│       └── schemas/
│           └── user.ex
├── priv/repo/migrations/            # phased migrations (multiple files)
├── test/schema_evolution/
│   └── phased_migration_test.exs
├── bench/migration_bench.exs
└── mix.exs
```

---

## Core concepts

### 1. The four operations that lock a table

Postgres (< 11 has more, ≥ 14 fewer) locks the table exclusively on:

1. `ALTER TABLE ADD COLUMN ... NOT NULL` **without a default** — rewrites every row.
2. `ALTER TABLE DROP COLUMN` — instant, but invalidates plans.
3. `CREATE INDEX` (without `CONCURRENTLY`) — blocks writes for the duration.
4. `ALTER TABLE VALIDATE CONSTRAINT` — scans the whole table (reads only, but long).

The zero-downtime playbook avoids (1), uses `CREATE INDEX CONCURRENTLY`, splits NOT NULL
into `ADD CONSTRAINT NOT VALID` + `VALIDATE` (no exclusive lock), and treats renames as
dual-column phases.

### 2. Rename column — three-phase

```
Phase 1: ADD new_name, dual-write from app (old still authoritative)
Phase 2: Backfill new_name from old_name, flip app to read/write new_name
Phase 3: DROP old_name
```

Each phase is a separate deploy. If any phase fails, you roll back one step.

### 3. Add NOT NULL — two-phase

```
Phase 1: ADD column with DEFAULT, backfill for existing rows (in batches), app writes it
Phase 2: ADD CHECK (... IS NOT NULL) NOT VALID, then VALIDATE CONSTRAINT, then SET NOT NULL
```

`NOT VALID` creates the constraint without scanning. `VALIDATE` scans but does not lock
writes. `SET NOT NULL` after validation is instant.

### 4. Backfill in batches

Never `UPDATE users SET email_verified = false` in one statement — that rewrites the whole
table under a lock. Instead, update 1k rows at a time with a sleep between batches.

### 5. `disable_migration_lock` for concurrent indexes

`CREATE INDEX CONCURRENTLY` cannot run inside a transaction. Ecto wraps migrations in a
transaction by default — disable it:

```elixir
@disable_ddl_transaction true
@disable_migration_lock true
```

---

## Design decisions

- **Option A — single mega-migration during maintenance window**. Pros: simple.
  Cons: downtime, rollback means restoring a backup.
- **Option B — phased migrations with dual-write**. Pros: zero downtime.
  Cons: more deploys, more moving parts, need the app code to handle both schemas during
  the window.

We use **Option B** throughout.

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

### Step 1: Baseline migration (starting point)

**Objective**: Stand up the single-`email` baseline so the phased rollout has a real legacy shape to evolve away from.

```elixir
# priv/repo/migrations/20260101000000_create_users.exs
defmodule SchemaEvolution.Repo.Migrations.CreateUsers do
  use Ecto.Migration

  def change do
    create table(:users) do
      add :email, :string, null: false
      timestamps()
    end

    create unique_index(:users, [:email])
  end
end
```

### Step 2: Phase 1 — add new column (dual-write ready)

**Objective**: Add `primary_email` nullable and dual-write from the changeset so new rows populate both columns without a table rewrite.

```elixir
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
```

App code during Phase 1 (dual-write, read from old):

```elixir
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
```

### Step 3: Phase 2 — backfill existing rows

**Objective**: Keyset-paginate 1k-row UPDATEs outside the DDL lock so pre-Phase-1 rows catch up without holding replica-breaking transactions.

```elixir
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
```

### Step 4: Phase 3 — add NOT NULL without lock

**Objective**: Add CHECK NOT VALID, VALIDATE, then SET NOT NULL so the NOT NULL flip skips the exclusive AccessExclusive scan.

```elixir
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
```

Phase 3 also flips the app schema to read from `primary_email`:

```elixir
# lib/schema_evolution/schemas/user.ex  — phase 3 version
schema "users" do
  field :email, :string                # deprecated, still dual-written for compat
  field :primary_email, :string        # authoritative NOW
  field :email_verified, :boolean, default: false
  timestamps()
end
```

### Step 5: Phase 4 — drop old column

**Objective**: Remove `email` only after the app stops dual-writing so the drop is a metadata-only DDL, not a code-breaker.

```elixir
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
```

### Step 6: Concurrent index creation example

**Objective**: Create the index with `concurrently: true` and disabled DDL transaction so writers keep flowing during the build.

```elixir
# priv/repo/migrations/20260601000000_phase5_add_verified_index.exs
defmodule SchemaEvolution.Repo.Migrations.Phase5AddVerifiedIndex do
  use Ecto.Migration

  @disable_ddl_transaction true
  @disable_migration_lock true

  def change do
    create index(:users, [:email_verified], concurrently: true)
  end
end
```

`CONCURRENTLY` cannot be inside a transaction — hence the two `@disable_*` module
attributes. Ecto refuses to run otherwise and tells you the flag to set.

### Step 7: Context during the migration window

**Objective**: Read via COALESCE(primary_email, email) so the context tolerates mixed-phase rows and survives a Phase-2 rollback.

```elixir
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

---

## Why this works

- **No exclusive locks**: every schema change uses a non-blocking form.
  `ADD COLUMN` without NOT NULL; `ADD CONSTRAINT NOT VALID` + `VALIDATE`;
  `CREATE INDEX CONCURRENTLY`.
- **Dual-write during the cut**: the app writes both columns so that between Phase 1 and
  Phase 3 every new row has both populated. Backfill covers only pre-Phase-1 rows.
- **COALESCE reads**: the query path tolerates the mixed state, so rollbacks (revert from
  Phase 2 back to Phase 1) do not break production reads.
- **Batched backfill**: 1000-row UPDATEs finish in milliseconds each. No replica lag,
  no long transaction holding row locks.

---

## Data flow of the phased rollout

```
t0        PHASE 1 DEPLOY
          migration: ADD primary_email (nullable)
          app:       dual-write (old + new), read from old
          state:     new rows have both columns; old rows have only :email

t1        PHASE 2 DEPLOY
          migration: backfill in batches
          app:       unchanged (still dual-write, read old)
          state:     ALL rows now have both columns

t2        PHASE 3 DEPLOY
          migration: ADD CONSTRAINT NOT VALID, VALIDATE, SET NOT NULL, DROP CHECK
          app:       switch READ to primary_email, keep dual-write
          state:     primary_email is enforced NOT NULL; reads authoritative

t3        PHASE 4 DEPLOY
          migration: DROP COLUMN email
          app:       drop :email from the schema, stop dual-write
          state:     migration complete
```

---

## Tests

```elixir
# test/schema_evolution/phased_migration_test.exs
defmodule SchemaEvolution.PhasedMigrationTest do
  use ExUnit.Case, async: false
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

---

## Benchmark

```elixir
# bench/migration_bench.exs
alias SchemaEvolution.Repo

Repo.query!("TRUNCATE users RESTART IDENTITY", [])

now = DateTime.utc_now() |> DateTime.truncate(:second)

rows =
  for i <- 1..50_000 do
    %{email: "u#{i}@x.com", primary_email: nil, inserted_at: now, updated_at: now}
  end

Enum.chunk_every(rows, 5_000)
|> Enum.each(&Repo.insert_all("users", &1))

batch_update = fn ->
  Ecto.Adapters.SQL.query!(Repo,
    """
    UPDATE users SET primary_email = email
    WHERE id IN (SELECT id FROM users WHERE primary_email IS NULL LIMIT 1000)
    """, [])
end

Benchee.run(
  %{
    "single 1k batch" => batch_update
  },
  time: 5, warmup: 2
)
```

**Target**: a 1k-row batch update under 30 ms on local Postgres. A full 50k backfill in
50 batches with 25 ms sleeps between = ~3 seconds wall-clock, bounded replica lag.

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

**1. `NOT NULL` with `DEFAULT` is a trap on Postgres < 11.** It rewrites every row. On
Postgres 11+, default is stored as metadata, instant. Check your server version.

**2. `CREATE INDEX CONCURRENTLY` can fail halfway.** It leaves an INVALID index. You must
`DROP INDEX CONCURRENTLY name` and retry. Monitor `pg_index.indisvalid`.

**3. Batched backfills can deadlock with app writes.** Update by PK ordering and keep
batches small. If you see deadlocks, reduce batch size.

**4. Dual-write code must handle `{:error, changeset}`.** If the new column has a unique
constraint, the dual-write could fail on the new side while the old side would have
succeeded. Wrap in a transaction so both commit or neither does.

**5. Rolling back Phase 3 requires re-allowing NULL.** Keep the down migration simple so
the on-call engineer does not need to think under pressure.

**6. When NOT to phase.** For a brand-new table with no production traffic yet, a single
migration is fine. Phasing is for live systems with replicas and > 0 QPS.

---

## Reflection

Your Phase 2 backfill completes successfully. Phase 3 begins and Postgres raises during
`VALIDATE CONSTRAINT` — one row has `primary_email IS NULL` because a dual-write failed
silently in Phase 1 (a bug in `write_through_to_primary_email`). The migration is stuck.
Describe the recovery: how do you identify the bad row, decide to fix it or abort the
phase, and preserve the invariant that you never read a NULL `primary_email` from the
app? What observability do you add *before* running Phase 3 to catch this earlier?

---

## Resources

- [Postgres — explicit locking](https://www.postgresql.org/docs/current/explicit-locking.html)
- [PgHero — "Strong Migrations" rules](https://github.com/ankane/strong_migrations)
- [Ecto — `@disable_ddl_transaction`](https://hexdocs.pm/ecto_sql/Ecto.Migration.html#module-transaction-callbacks)
- [Dashbit — "Zero-downtime migrations in Ecto"](https://dashbit.co/blog)

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```
