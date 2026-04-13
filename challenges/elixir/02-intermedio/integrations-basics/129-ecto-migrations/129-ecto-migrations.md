# Ecto migrations: create, alter, indexes, and constraints

**Project**: `migrations_lab` — a repo with three migrations that create a table, alter it (add column + index), and add a database-level constraint.

---

## Project context

Migrations are how you evolve a schema under version control. Ecto's
migration DSL (`create table`, `alter table`, `create index`, `create
constraint`) compiles to the adapter's native DDL. The `schema_migrations`
table tracks which migrations have been applied, so `mix ecto.migrate` is
idempotent.

This exercise builds a small `posts` schema in three migrations to make the
phases visible:

1. Create a `posts` table with a foreign key to `users`.
2. Alter: add a `published_at` column and an index on `(user_id,
   published_at)`.
3. Constraint: enforce that `published_at` cannot be in the future via a
   check constraint — and translate the DB error to a changeset error.

You'll also meet `mix ecto.gen.migration`, the one-way trap of `change/0`
(vs `up/0` + `down/0`), and why SQLite pretends to support `alter table
drop column` but doesn't really.

Project structure:

```
migrations_lab/
├── config/
│   └── config.exs
├── lib/
│   ├── migrations_lab/
│   │   ├── application.ex
│   │   ├── repo.ex
│   │   ├── user.ex
│   │   └── post.ex
│   └── migrations_lab.ex
├── priv/
│   └── repo/
│       └── migrations/
│           ├── 20260101000001_create_users_and_posts.exs
│           ├── 20260101000002_add_published_at_to_posts.exs
│           └── 20260101000003_add_published_at_not_in_future.exs
├── test/
│   ├── migrations_lab_test.exs
│   └── test_helper.exs
└── mix.exs
```

---

## Core concepts

### 1. `mix ecto.gen.migration <name>`

Generates a timestamped `.exs` file in `priv/repo/migrations/` with a
skeleton `change/0`. The timestamp is the sort key — migrations run in
filename order. Never renumber after the fact: everyone else already ran
it under the old number.

### 2. `change/0` vs `up/0` + `down/0`

`change/0` describes the "forward" transition in a way Ecto can
auto-reverse for `mix ecto.rollback`. It works for `create`, `alter add`,
`rename`, `create index`. It does **not** auto-reverse data migrations,
raw SQL, or `drop column` reliably — for those, split into `up/0` + `down/0`.

### 3. `create table(:posts)` — always `:id` unless you say otherwise

```elixir
create table(:posts) do
  add :title, :string, null: false
  add :user_id, references(:users, on_delete: :delete_all), null: false
  timestamps()
end
```

`table/2` adds an auto-increment `id` by default. `references/2` creates
the FK column and the constraint. `on_delete:` is critical: the default
is `:nothing` (cascade is a policy choice, not a default).

### 4. Indexes: separate statement, sometimes concurrent

```elixir
create index(:posts, [:user_id])
create unique_index(:users, [:email])
```

On Postgres, wrap expensive indexes with `create index(..., concurrently:
true)` to avoid locking — but that requires `@disable_ddl_transaction
true` and `@disable_migration_lock true` at the top of the migration.
SQLite doesn't support concurrent indexes; it's fast enough to not need
them on the datasets it's appropriate for.

### 5. Check constraints bridge the DB and the changeset

```elixir
create constraint(:posts, :published_at_not_in_future,
  check: "published_at IS NULL OR published_at <= CURRENT_TIMESTAMP")
```

The constraint runs on the DB. In the changeset, `check_constraint/3`
translates a violation into a friendly `:error` entry — the same pattern
as `unique_constraint/2`.

---

## Why migrations and not raw SQL files in git

Raw `.sql` files in `db/migrations/` tracked by convention work, but
reinvent half the plumbing Ecto ships with: ordering (timestamps vs
filenames), applied-set tracking (`schema_migrations`), rollback, and a
testable DSL that works across Postgres/MySQL/SQLite. `Ecto.Migration`
is that plumbing plus a DSL that's reviewable — `add :foo, :string,
null: false` is easier to diff and reason about than the corresponding
DDL variants across three dialects.

---

## Design decisions

**Option A — One giant migration per feature release**
- Pros: One PR, one file to review.
- Cons: A failed step halfway leaves the DB in a partial state; you
  can't roll back half the change; long-running DDL (indexes) blocks
  shorter ones behind it.

**Option B — Small, single-purpose migrations** (chosen)
- Pros: Each migration is independently rollback-able; `change/0` works
  for most of them; long-running ops (concurrent indexes) get their
  own file with `@disable_ddl_transaction true`; review is focused.
- Cons: More files; more PRs; ordering matters.

→ Chose **B** because a migration that takes 30 minutes to run (index
  on a 500M row table) must be its own file so it can be run
  concurrently without holding a DDL lock on unrelated changes.

---

## Implementation

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Standard library: no external dependencies required
    {:"ecto", "~> 1.0"},
  ]
end
```


### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — isolated from any external state, so we demonstrate this concept cleanly without dependencies.


```bash
mix new migrations_lab --sup
cd migrations_lab
```

### Step 2: `mix.exs`

**Objective**: Declare dependencies and project config in `mix.exs`.


```elixir
defmodule MigrationsLab.MixProject do
  use Mix.Project

  def project do
    [
      app: :migrations_lab,
      version: "0.1.0",
      elixir: "~> 1.15",
      aliases: aliases(),
      deps: deps()
    ]
  end

  def application do
    [
      extra_applications: [:logger],
      mod: {MigrationsLab.Application, []}
    ]
  end

  defp deps do
    [
      {:ecto_sql, "~> 3.11"},
      {:ecto_sqlite3, "~> 0.17"}
    ]
  end

  defp aliases do
    [test: ["ecto.drop --quiet", "ecto.create --quiet", "ecto.migrate --quiet", "test"]]
  end
end
```

### Step 3: `config/config.exs`

**Objective**: Implement `config.exs` — the integration seam where external protocol semantics meet Elixir domain code.


```elixir
import Config

config :migrations_lab, ecto_repos: [MigrationsLab.Repo]

config :migrations_lab, MigrationsLab.Repo,
  database: Path.expand("../migrations_lab_#{config_env()}.db", __DIR__),
  pool_size: 5,
  pool: if(config_env() == :test, do: Ecto.Adapters.SQL.Sandbox, else: DBConnection.ConnectionPool)
```

### Step 4: `lib/migrations_lab/repo.ex`, `application.ex`

**Objective**: Provide `lib/migrations_lab/repo.ex`, `application.ex` — these are the supporting fixtures the main module depends on to make its concept demonstrable.


```elixir
defmodule MigrationsLab.Repo do
  use Ecto.Repo, otp_app: :migrations_lab, adapter: Ecto.Adapters.SQLite3
end

defmodule MigrationsLab.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    Supervisor.start_link([MigrationsLab.Repo],
      strategy: :one_for_one,
      name: MigrationsLab.Supervisor
    )
  end
end
```

### Step 5: Migration 1 — `priv/repo/migrations/20260101000001_create_users_and_posts.exs`

**Objective**: Migration 1 — `priv/repo/migrations/20260101000001_create_users_and_posts.exs`.


```elixir
defmodule MigrationsLab.Repo.Migrations.CreateUsersAndPosts do
  use Ecto.Migration

  def change do
    create table(:users) do
      add :email, :string, null: false
      timestamps()
    end

    create unique_index(:users, [:email])

    create table(:posts) do
      add :title, :string, null: false
      add :body, :text
      # :delete_all — when the user goes, their posts go with them.
      # Default is :nothing, which would leave orphan FK violations.
      add :user_id, references(:users, on_delete: :delete_all), null: false
      timestamps()
    end

    create index(:posts, [:user_id])
  end
end
```

### Step 6: Migration 2 — `priv/repo/migrations/20260101000002_add_published_at_to_posts.exs`

**Objective**: Migration 2 — `priv/repo/migrations/20260101000002_add_published_at_to_posts.exs`.


```elixir
defmodule MigrationsLab.Repo.Migrations.AddPublishedAtToPosts do
  use Ecto.Migration

  def change do
    alter table(:posts) do
      add :published_at, :utc_datetime_usec
    end

    # Composite index that supports "posts by user, newest first" queries.
    create index(:posts, [:user_id, :published_at])
  end
end
```

### Step 7: Migration 3 — `priv/repo/migrations/20260101000003_add_published_at_not_in_future.exs`

**Objective**: Migration 3 — `priv/repo/migrations/20260101000003_add_published_at_not_in_future.exs`.


```elixir
defmodule MigrationsLab.Repo.Migrations.AddPublishedAtNotInFuture do
  use Ecto.Migration

  def change do
    # Named so we can reference the constraint from the changeset.
    create constraint(:posts, :published_at_not_in_future,
             check: "published_at IS NULL OR published_at <= CURRENT_TIMESTAMP")
  end
end
```

### Step 8: Schemas — `lib/migrations_lab/user.ex`, `post.ex`

**Objective**: Schemas — `lib/migrations_lab/user.ex`, `post.ex`.


```elixir
defmodule MigrationsLab.User do
  use Ecto.Schema
  import Ecto.Changeset

  schema "users" do
    field :email, :string
    has_many :posts, MigrationsLab.Post
    timestamps()
  end

  def changeset(user, attrs) do
    user
    |> cast(attrs, [:email])
    |> validate_required([:email])
    |> unique_constraint(:email)
  end
end

defmodule MigrationsLab.Post do
  use Ecto.Schema
  import Ecto.Changeset

  schema "posts" do
    field :title, :string
    field :body, :string
    field :published_at, :utc_datetime_usec
    belongs_to :user, MigrationsLab.User
    timestamps()
  end

  def changeset(post, attrs) do
    post
    |> cast(attrs, [:title, :body, :published_at, :user_id])
    |> validate_required([:title, :user_id])
    |> foreign_key_constraint(:user_id)
    # This is what turns the DB check constraint into a field-level error.
    |> check_constraint(:published_at,
         name: :published_at_not_in_future,
         message: "cannot be in the future")
  end
end
```

### Step 9: `lib/migrations_lab.ex`

**Objective**: Implement `migrations_lab.ex` — the integration seam where external protocol semantics meet Elixir domain code.


```elixir
defmodule MigrationsLab do
  alias MigrationsLab.{Repo, User, Post}

  def create_user(attrs), do: %User{} |> User.changeset(attrs) |> Repo.insert()
  def create_post(attrs), do: %Post{} |> Post.changeset(attrs) |> Repo.insert()
end
```

### Step 10: `test/test_helper.exs`

**Objective**: Implement `test_helper.exs` — the integration seam where external protocol semantics meet Elixir domain code.


```elixir
ExUnit.start()
Ecto.Adapters.SQL.Sandbox.mode(MigrationsLab.Repo, :manual)
```

### Step 11: `test/migrations_lab_test.exs`

**Objective**: Write `migrations_lab_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule MigrationsLabTest do
  use ExUnit.Case, async: false

  alias MigrationsLab.{Repo, User, Post}

  setup do
    :ok = Ecto.Adapters.SQL.Sandbox.checkout(Repo)
    {:ok, user} = MigrationsLab.create_user(%{"email" => "a@b.com"})
    {:ok, user: user}
  end

  describe "migrations applied" do
    test "posts table exists with all expected columns" do
      # Poking the information_schema equivalent via a raw query.
      {:ok, %{rows: rows}} = Repo.query("PRAGMA table_info(posts)")
      columns = rows |> Enum.map(&Enum.at(&1, 1)) |> MapSet.new()
      assert "title" in columns
      assert "body" in columns
      assert "user_id" in columns
      assert "published_at" in columns
    end
  end

  describe "foreign key constraint" do
    test "posts require a valid user_id", %{user: user} do
      assert {:ok, _} =
               MigrationsLab.create_post(%{"title" => "hi", "user_id" => user.id})

      assert {:error, %Ecto.Changeset{valid?: false} = cs} =
               MigrationsLab.create_post(%{"title" => "hi", "user_id" => -1})

      assert cs.errors[:user_id]
    end

    test "deleting a user cascades to their posts", %{user: user} do
      {:ok, post} = MigrationsLab.create_post(%{"title" => "hi", "user_id" => user.id})
      Repo.delete!(user)
      refute Repo.get(Post, post.id)
    end
  end

  describe "check constraint" do
    test "published_at in the future is rejected with a friendly message",
         %{user: user} do
      future = DateTime.utc_now() |> DateTime.add(3600, :second)

      assert {:error, %Ecto.Changeset{valid?: false} = cs} =
               MigrationsLab.create_post(%{
                 "title" => "future post",
                 "user_id" => user.id,
                 "published_at" => future
               })

      assert {"cannot be in the future", _} = cs.errors[:published_at]
    end

    test "published_at in the past is accepted", %{user: user} do
      past = DateTime.utc_now() |> DateTime.add(-3600, :second)

      assert {:ok, %Post{published_at: ^past}} =
               MigrationsLab.create_post(%{
                 "title" => "old post",
                 "user_id" => user.id,
                 "published_at" => past
               })
    end
  end
end
```

### Step 12: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
```

### Why this works

Each migration is a timestamped file; `mix ecto.migrate` runs them in
sort order and records applied versions in `schema_migrations`, making
the operation idempotent. `change/0` describes the forward direction
in a dialect Ecto can auto-reverse (`create` → `drop`, `alter add` →
`alter remove`). `references/2` couples FK column creation and the
constraint in one statement. `check_constraint/3` in the changeset
bridges the DB-level guarantee (enforced even against direct SQL
writes) to the form-level error (per-field message the UI can render).

---

## Key Concepts

Database migrations are versioned, timestamped DDL files that encode schema evolution under version control. Each migration is idempotent—running the same migration twice has no effect because the `schema_migrations` table tracks applied versions. This design prevents the class of bugs where "forgot to run migration X" silently breaks downstream code. The `change/0` callback is bidirectional: Ecto infers rollback semantics, so `create table` automatically reverses to `drop table`. However, `change/0` only works for reversible operations; data migrations, raw SQL, or destructive alterations require explicit `up/0` + `down/0` pairs. In production, index creation on large tables locks writes—use `concurrently: true` with `@disable_ddl_transaction true` to avoid this. Foreign key policies (`on_delete:`, `on_update:`) must be chosen intentionally, not left to defaults, as they determine cascade behavior during deletes. The bridge between database constraints and Elixir changesets—using `check_constraint/3`, `unique_constraint/2`, etc.—ensures the database is the source of truth while changesets provide friendly error messages for forms and APIs.

---

## Deep Dive: State Management and Message Handling Patterns

Understanding state transitions is central to reliable OTP systems. Every `handle_call` or `handle_cast` receives current state and returns new state—immutability forces explicit reasoning. This prevents entire classes of bugs: missing state updates are immediately visible.

Key insight: separate pure logic (state → new state) from side effects (logging, external calls). Move pure logic to private helpers; use handlers for orchestration. This makes servers testable—test pure functions independently.

In production, monitor state size and mutation frequency. Unbounded growth is a memory leak; excessive mutations signal hot spots needing optimization. Always profile before reaching for performance solutions like ETS.

## Benchmark

<!-- benchmark N/A: migrations are one-shot DDL; the metric that
     matters is duration on the target dataset (minutes, not
     microseconds). Target: every migration should either complete in
     under 100ms on a dev DB, or declare itself long-running via
     `@disable_ddl_transaction true` and be run concurrently. -->

---

## Key Concepts

Migrations track database schema changes. Each migration file (timestamped, in `priv/repo/migrations/`) declares a forward change (`change/0`) that Ecto can auto-reverse. `mix ecto.migrate` applies pending migrations; `mix ecto.rollback` reverts them. The `schema_migrations` table tracks applied versions, so migrations are idempotent—running the same migration twice has no effect. This is crucial: you can safely run `mix ecto.migrate` on deployment without worrying about already-applied migrations. The trade-off: once a migration is applied in production, you cannot edit it—always write a new migration to correct past mistakes. Migrations coupled with changesets enforce the boundary: database changes are version-controlled and reversible.

---

## Trade-offs and production gotchas

**1. `change/0` cannot always auto-reverse**
Adding a column, creating an index, renaming — fine. Raw SQL via `execute/1`
is not reversible unless you give the opposite statement. If in doubt, use
`up/0` + `down/0` explicitly and test the rollback.

**2. Never edit an already-applied migration**
Once teammates or production have run it, editing the file does nothing on
their DB (the row in `schema_migrations` is by version number). Always
write a *new* migration to correct a past one.

**3. Index creation locks tables — especially on Postgres**
A `CREATE INDEX` on a large hot table can block writes for minutes. Use
`concurrently: true` with `@disable_ddl_transaction true` and
`@disable_migration_lock true`. SQLite doesn't have this problem because
it barely has concurrent writes to begin with.

**4. `references/2` defaults to `on_delete: :nothing`**
Which means a user delete will fail if any post references them. Always
pick a policy intentionally: `:delete_all`, `:nilify_all`, `:restrict`
or `:nothing`. The "forgot to set it" path produces confusing FK errors
months later.

**5. SQLite's `alter table drop column` is lying**
SQLite only recently added `DROP COLUMN` support. Many adapters emulate
it by rebuilding the table. For teaching, fine; for a migration you run
against a production DB, test drops carefully.

**6. Check constraints vs changeset validations**
Both can reject "published_at in the future". Do one, not both. Database
constraints protect against bypasses (other services, SQL); changeset
validations give per-field error messages in forms. The `check_constraint/3`
pattern used above bridges the two: the DB is the source of truth, the
changeset renders the message.

**7. When NOT to roll a new migration**
For seed data, use `priv/repo/seeds.exs` or a Mix task — not a migration.
Migrations should be pure schema changes. Data backfills belong in a
separate, idempotent, resumable script (you'll want to re-run them).

---

## Reflection

- You need to add a NOT NULL column to a 500M row table. A single
  migration is clearly wrong (it'll lock writes). Sketch the multi-
  migration plan (add nullable → backfill → add check/NOT NULL), and
  explain which parts belong in migrations vs a separate backfill script.
- A junior writes a data migration that updates every row in a table
  inside `change/0`. It runs fine in dev but OOMs prod. What's the
  failure mode, and what's the minimal refactor to make it safe?

---

## Executable Example

Copy the code below into a file (e.g., `solution.exs`) and run with `elixir solution.exs`:

```elixir
defmodule Main do
  defmodule MigrationsLab.User do
    use Ecto.Schema
    import Ecto.Changeset

    schema "users" do
      field :email, :string
      has_many :posts, MigrationsLab.Post
      timestamps()
    end

    def changeset(user, attrs) do
      user
      |> cast(attrs, [:email])
      |> validate_required([:email])
      |> unique_constraint(:email)
    end
  end

  defmodule MigrationsLab.Post do
    use Ecto.Schema
    import Ecto.Changeset

    schema "posts" do
      field :title, :string
      field :body, :string
      field :published_at, :utc_datetime_usec
      belongs_to :user, MigrationsLab.User
      timestamps()
    end

    def changeset(post, attrs) do
      post
      |> cast(attrs, [:title, :body, :published_at, :user_id])
      |> validate_required([:title, :user_id])
      |> foreign_key_constraint(:user_id)
      # This is what turns the DB check constraint into a field-level error.
      |> check_constraint(:published_at,
           name: :published_at_not_in_future,
           message: "cannot be in the future")
    end
  end

  def main do
    IO.puts("=== Repo Demo ===
  ")
  
    # Demo: Migration structure
  IO.puts("1. Migrations define database schema changes")
  IO.puts("2. Use: mix ecto.migrate")
  IO.puts("3. Use: mix ecto.rollback")

  IO.puts("
  ✓ Ecto migrations demo completed!")
  end

end

Main.main()
```


## Resources

- [`Ecto.Migration` — hexdocs](https://hexdocs.pm/ecto_sql/Ecto.Migration.html)
- [`mix ecto.gen.migration`](https://hexdocs.pm/ecto_sql/Mix.Tasks.Ecto.Gen.Migration.html)
- [`mix ecto.migrate`](https://hexdocs.pm/ecto_sql/Mix.Tasks.Ecto.Migrate.html)
- [Dashbit: migration safety patterns](https://dashbit.co/blog/automatic-and-manual-ecto-migrations)
- [`fly_postgres_elixir` / production migration guides](https://fly.io/phoenix-files/safe-ecto-migrations/) — excellent real-world list of gotchas
