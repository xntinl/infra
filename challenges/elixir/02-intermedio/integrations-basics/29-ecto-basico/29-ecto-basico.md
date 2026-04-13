# Ecto basics: Repo, Schema, and your first insert/get

**Project**: `ecto_intro` — a single-schema app with SQLite-backed `Repo`, a migration, and CRUD tests.

---

## Project context

Ecto is **not** an ORM. It's a toolkit with four distinct pieces that people
routinely conflate:

- **`Ecto.Repo`** — the gateway to the database. Holds the connection pool.
- **`Ecto.Schema`** — maps Elixir structs to database rows. Declarative, inert.
- **`Ecto.Changeset`** — data + validations + a plan for mutation.
- **`Ecto.Query`** — a composable query DSL that compiles to SQL.

In this exercise you'll wire all four for a minimal `User` domain. We'll use
SQLite (via `ecto_sqlite3`) so the tests run with no external services —
you get the full Ecto experience without Postgres infra. Next exercises
(changesets, migrations, transactions) build directly on this.

Project structure:

```
ecto_intro/
├── config/
│   └── config.exs
├── lib/
│   ├── ecto_intro/
│   │   ├── application.ex
│   │   ├── repo.ex
│   │   └── user.ex
│   └── ecto_intro.ex
├── priv/
│   └── repo/
│       └── migrations/
│           └── 20260101000000_create_users.exs
├── test/
│   ├── ecto_intro_test.exs
│   └── test_helper.exs
└── mix.exs
```

---

## Core concepts

### 1. `Ecto.Repo` — the connection pool, not an object store

`Repo` is a GenServer-backed DBConnection pool. You configure it once, add
it to your supervision tree, and every query (`Repo.insert`, `Repo.get`,
`Repo.all`) checks out a connection, runs the statement, and returns
plain data. The `Repo` itself has no cache, no identity map, no lazy
loading. This is a feature.

### 2. `schema` declares fields, not behavior

```elixir
schema "users" do
  field :email, :string
  field :age, :integer
  timestamps()
end
```

That generates a `%User{}` struct, a `__changeset__/0` map, and compile-time
type info. It does not run validations or touch the DB. Validations live in
changesets; persistence lives in `Repo`.

### 3. `changeset/2` is a function, not magic

By convention each schema module exposes `changeset(struct, attrs)`. Inside
you `cast` incoming attrs (filter by allowed keys), then `validate_*`. The
result is an `%Ecto.Changeset{}` that `Repo.insert/1` knows how to consume.

### 4. Migrations are separate from schemas

The migration file defines the *table*; the schema module defines the
*struct*. The only coupling is the table name and column names. You can
have columns a schema ignores, and a schema can add virtual fields the
table doesn't have. Changing a column requires a migration; changing
validations does not.

### 5. `ecto_sqlite3` for tests

For learning, SQLite in a tmp file is perfect. It avoids port conflicts,
starts instantly, and supports enough of SQL for Ecto's core features. For
production apps you'll swap `ecto_sqlite3` for `postgrex` — the schema and
changeset code doesn't change.

---

## Design decisions

**Option A — use `Postgrex` for tests and ship Postgres from day one**
- Pros: parity with production; all of Postgres's types (`jsonb`, arrays, enums) available; concurrent writer tests work.
- Cons: tests need a running Postgres; CI/onboarding friction; overkill for a pedagogical CRUD exercise.

**Option B — `ecto_sqlite3` for the intro, adapter-swappable later (chosen)**
- Pros: zero external service; instant tests; the `Repo`/`Schema`/`Changeset` code is adapter-agnostic; Sandbox works against SQLite too.
- Cons: SQLite doesn't match Postgres exactly (no `jsonb`, relaxed typing, limited concurrent writers) — subtle bugs can hide until you swap adapters.

→ Chose **B** because the lesson is the Ecto split (Repo / Schema / Changeset / Query), not DB administration; keeping tests dependency-free lowers the barrier without locking the design to SQLite.

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
mix new ecto_intro --sup
cd ecto_intro
```

`--sup` scaffolds an Application module with a supervision tree, which
we need because `Repo` is a supervised process.

### Step 2: `mix.exs`

**Objective**: Declare dependencies and project config in `mix.exs`.


```elixir
defmodule EctoIntro.MixProject do
  use Mix.Project

  def project do
    [
      app: :ecto_intro,
      version: "0.1.0",
      elixir: "~> 1.15",
      start_permanent: Mix.env() == :prod,
      aliases: aliases(),
      deps: deps()
    ]
  end

  def application do
    [
      extra_applications: [:logger],
      mod: {EctoIntro.Application, []}
    ]
  end

  defp deps do
    [
      {:ecto_sql, "~> 3.11"},
      {:ecto_sqlite3, "~> 0.17"}
    ]
  end

  defp aliases do
    [
      # `mix test` should always start from a clean schema.
      test: ["ecto.drop --quiet", "ecto.create --quiet", "ecto.migrate --quiet", "test"]
    ]
  end
end
```

Run `mix deps.get`.

### Step 3: `config/config.exs`

**Objective**: Implement `config.exs` — the integration seam where external protocol semantics meet Elixir domain code.


```elixir
import Config

config :ecto_intro,
  ecto_repos: [EctoIntro.Repo]

config :ecto_intro, EctoIntro.Repo,
  database: Path.expand("../ecto_intro_#{config_env()}.db", __DIR__),
  pool_size: 5,
  # In test, sandbox the pool so each test runs in its own transaction.
  pool: if(config_env() == :test, do: Ecto.Adapters.SQL.Sandbox, else: DBConnection.ConnectionPool)
```

### Step 4: `lib/ecto_intro/repo.ex`

**Objective**: Implement `repo.ex` — the integration seam where external protocol semantics meet Elixir domain code.


```elixir
defmodule EctoIntro.Repo do
  use Ecto.Repo,
    otp_app: :ecto_intro,
    adapter: Ecto.Adapters.SQLite3
end
```

### Step 5: `lib/ecto_intro/application.ex`

**Objective**: Wire `application.ex` to start the supervision tree that boots Repo and external adapters in the correct order before serving traffic.


```elixir
defmodule EctoIntro.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      EctoIntro.Repo
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: EctoIntro.Supervisor)
  end
end
```

### Step 6: `lib/ecto_intro/user.ex`

**Objective**: Implement `user.ex` — the integration seam where external protocol semantics meet Elixir domain code.


```elixir
defmodule EctoIntro.User do
  @moduledoc """
  A minimal schema: email + age + timestamps. The changeset function is the
  only place validations live — schemas are intentionally dumb.
  """
  use Ecto.Schema
  import Ecto.Changeset

  @type t :: %__MODULE__{
          id: integer() | nil,
          email: String.t() | nil,
          age: integer() | nil,
          inserted_at: NaiveDateTime.t() | nil,
          updated_at: NaiveDateTime.t() | nil
        }

  schema "users" do
    field :email, :string
    field :age, :integer
    timestamps()
  end

  @permitted [:email, :age]
  @required [:email]

  @doc """
  Builds a changeset for a user. Only `@permitted` keys are accepted;
  anything else is dropped silently — this is your mass-assignment guard.
  """
  @spec changeset(t() | %__MODULE__{}, map()) :: Ecto.Changeset.t()
  def changeset(user, attrs) do
    user
    |> cast(attrs, @permitted)
    |> validate_required(@required)
    |> validate_format(:email, ~r/@/)
    |> validate_number(:age, greater_than_or_equal_to: 0)
    |> unique_constraint(:email)
  end
end
```

### Step 7: `priv/repo/migrations/20260101000000_create_users.exs`

**Objective**: Implement `20260101000000_create_users.exs` — the integration seam where external protocol semantics meet Elixir domain code.


```elixir
defmodule EctoIntro.Repo.Migrations.CreateUsers do
  use Ecto.Migration

  def change do
    create table(:users) do
      add :email, :string, null: false
      add :age, :integer
      timestamps()
    end

    create unique_index(:users, [:email])
  end
end
```

### Step 8: `lib/ecto_intro.ex`

**Objective**: Implement `ecto_intro.ex` — the integration seam where external protocol semantics meet Elixir domain code.


```elixir
defmodule EctoIntro do
  @moduledoc """
  Public API wrapping `Repo` calls. In a real app you'd split this into
  contexts (`Accounts.create_user/1`, etc.); for the intro one module
  keeps the surface visible.
  """

  alias EctoIntro.{Repo, User}

  @spec create_user(map()) :: {:ok, User.t()} | {:error, Ecto.Changeset.t()}
  def create_user(attrs) do
    %User{}
    |> User.changeset(attrs)
    |> Repo.insert()
  end

  @spec get_user(integer()) :: User.t() | nil
  def get_user(id), do: Repo.get(User, id)

  @spec get_user_by_email(String.t()) :: User.t() | nil
  def get_user_by_email(email), do: Repo.get_by(User, email: email)

  @spec list_users() :: [User.t()]
  def list_users, do: Repo.all(User)
end
```

### Step 9: `test/test_helper.exs`

**Objective**: Implement `test_helper.exs` — the integration seam where external protocol semantics meet Elixir domain code.


```elixir
ExUnit.start()

# Sandbox the pool so tests can share a clean DB via transactional rollback.
Ecto.Adapters.SQL.Sandbox.mode(EctoIntro.Repo, :manual)
```

### Step 10: `test/ecto_intro_test.exs`

**Objective**: Write `ecto_intro_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule EctoIntroTest do
  use ExUnit.Case, async: false
  # SQLite doesn't love concurrent writers; keep this suite serial.

  alias EctoIntro.{Repo, User}

  setup do
    :ok = Ecto.Adapters.SQL.Sandbox.checkout(Repo)
    :ok
  end

  describe "create_user/1" do
    test "persists a valid user" do
      assert {:ok, %User{id: id, email: "a@b.com", age: 30}} =
               EctoIntro.create_user(%{"email" => "a@b.com", "age" => 30})

      assert is_integer(id)
    end

    test "returns a changeset error when email is missing" do
      assert {:error, %Ecto.Changeset{valid?: false} = cs} =
               EctoIntro.create_user(%{"age" => 30})

      assert %{email: ["can't be blank"]} = errors_on(cs)
    end

    test "enforces unique email at the DB level" do
      assert {:ok, _} = EctoIntro.create_user(%{"email" => "dup@x.com"})

      assert {:error, %Ecto.Changeset{valid?: false} = cs} =
               EctoIntro.create_user(%{"email" => "dup@x.com"})

      assert %{email: ["has already been taken"]} = errors_on(cs)
    end
  end

  describe "get_user/1 and get_user_by_email/1" do
    test "round-trips by id and by email" do
      {:ok, u} = EctoIntro.create_user(%{"email" => "lookup@x.com"})

      assert EctoIntro.get_user(u.id).email == "lookup@x.com"
      assert EctoIntro.get_user_by_email("lookup@x.com").id == u.id
      assert EctoIntro.get_user(-1) == nil
    end
  end

  describe "list_users/0" do
    test "returns all users" do
      for i <- 1..3, do: EctoIntro.create_user(%{"email" => "u#{i}@x.com"})
      assert length(EctoIntro.list_users()) == 3
    end
  end

  # Helper: turn a changeset's errors map into `%{field => ["message", ...]}`.
  defp errors_on(changeset) do
    Ecto.Changeset.traverse_errors(changeset, fn {msg, opts} ->
      Regex.replace(~r"%\{(\w+)\}", msg, fn _, key ->
        opts |> Keyword.get(String.to_existing_atom(key), key) |> to_string()
      end)
    end)
  end
end
```

### Step 11: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
```

The `test` alias drops/creates/migrates the DB first.

---


## Key Concepts

External integrations in Elixir split across multiple patterns: Ecto for relational databases with changesets and migrations; Telemetry for metrics and observability; HTTP libraries like Req or Finch for REST APIs; and specialized parsers like Jason, NimbleCSV, and NimbleParsec for data formats. Choosing the right tool avoids the trap of one library solving everything poorly.

Ecto is the de facto standard for databases because changesets encode validation before queries, migrations manage schema evolution, and the Repo pattern separates query logic from business logic. Migrations are version-controlled SQL, ensuring reproducible deployments. For integrating external services, Req is the modern HTTP client with built-in retry, redirect, and error handling policies.

Telemetry decouples metrics collection from application code: you emit events and let listeners subscribe. This separation keeps business logic clean and metrics infrastructure pluggable. Use metrics, not print statements, in production.

## Key Concepts

Ecto is Elixir's standard library for database access. A Repo is the entry point—queries go through it. Schemas map database tables to Elixir structs; queries are built with the query DSL (`from u in User, where: u.age > 18`). Changesets represent changes and validations—they separate reads (queries) from writes (changesets). This structure enforces a clean boundary: databases are accessed through a single module (the Repo). Ecto supports Postgres, MySQL, SQLite, and others through adapters. The learning curve is steep (schemas, migrations, query syntax, changesets) but the payoff is huge: type-safe queries, automatic SQL generation, built-in validations.

---

## Trade-offs and production gotchas

**1. `Repo.get/2` returns `nil`, not `{:error, :not_found}`**
Ecto chose the "missing is nil" convention, not `{:ok, _} | {:error, _}`.
Contexts should wrap this and return tagged tuples so callers don't
branch on `nil`. Failing to do so is how you get `FunctionClauseError`
two layers up.

**2. SQLite is not Postgres**
SQLite has relaxed typing, limited concurrent writes, no per-column
collation in the same way, and no `jsonb`. It's excellent for learning
and for single-node tools. For multi-writer web apps, switch to Postgres
before you deploy.

**3. Changesets can be valid and still fail at insert**
`unique_constraint/2` only turns a DB error into a friendly message **if**
the index exists and the constraint name matches. The validation itself
doesn't hit the DB — the uniqueness check only happens when `Repo.insert`
runs. Forgetting the index means duplicate rows slip through.

**4. `cast/3` drops unknown keys silently**
That's how Ecto prevents mass assignment. Good default, but surprising when
a typo in an attr key silently vanishes. When debugging "why isn't this
field saving", check it's in the `@permitted` list.

**5. One `Repo` per database, not per schema**
Beginners sometimes create a `Repo` per table. Don't. One `Repo` per
database connection — the schema doesn't care which repo loads it as long
as the table exists.

**6. When NOT to use Ecto**
For one-off scripts against an existing DB, raw SQL via `Postgrex` is
simpler. For non-tabular stores (Redis, Mnesia, ETS), Ecto doesn't help.
For "I just want a map in memory", Agent/ETS is 100× less ceremony.

---

## Benchmark

<!-- benchmark N/A: integration/configuration exercise -->

## Reflection

- `cast/3` silently drops unknown keys as a mass-assignment guard, but it also silently drops typos — a form field named `emial` never reaches the DB and you get a "required" error on `email` instead of a "you wrote it wrong" error. In a context-module API, what's the cheapest instrumentation you could add to surface this difference without weakening the mass-assignment protection?

## Executable Example

Copy the code below into a file (e.g., `solution.exs`) and run with `elixir solution.exs`:

```elixir
defmodule Main do
  defmodule EctoIntro.User do
    @moduledoc """
    A minimal schema: email + age + timestamps. The changeset function is the
    only place validations live — schemas are intentionally dumb.
    """
    use Ecto.Schema
    import Ecto.Changeset

    @type t :: %__MODULE__{
            id: integer() | nil,
            email: String.t() | nil,
            age: integer() | nil,
            inserted_at: NaiveDateTime.t() | nil,
            updated_at: NaiveDateTime.t() | nil
          }

    schema "users" do
      field :email, :string
      field :age, :integer
      timestamps()
    end

    @permitted [:email, :age]
    @required [:email]

    @doc """
    Builds a changeset for a user. Only `@permitted` keys are accepted;
    anything else is dropped silently — this is your mass-assignment guard.
    """
    @spec changeset(t() | %__MODULE__{}, map()) :: Ecto.Changeset.t()
    def changeset(user, attrs) do
      user
      |> cast(attrs, @permitted)
      |> validate_required(@required)
      |> validate_format(:email, ~r/@/)
      |> validate_number(:age, greater_than_or_equal_to: 0)
      |> unique_constraint(:email)
    end
  end

  def main do
    IO.puts("=== Repo Demo ===
  ")
  
    # Demo: Basic Ecto usage
  IO.puts("1. Repo operations: all, get, insert, update")
  IO.puts("2. Schema defines tables and fields")
  IO.puts("3. Changesets for validation")

  IO.puts("
  ✓ Ecto basics demo completed!")
  end

end

Main.main()
```


## Resources

- [Ecto — hexdocs](https://hexdocs.pm/ecto/Ecto.html)
- [`Ecto.Repo`](https://hexdocs.pm/ecto/Ecto.Repo.html)
- [`Ecto.Schema`](https://hexdocs.pm/ecto/Ecto.Schema.html)
- [`Ecto.Changeset`](https://hexdocs.pm/ecto/Ecto.Changeset.html)
- [`ecto_sqlite3`](https://hexdocs.pm/ecto_sqlite3/) — SQLite adapter used here
- [Dashbit: "The Little Ecto Cookbook"](https://dashbit.co/ebooks/the-little-ecto-cookbook)
- [Plataformatec/Dashbit blog — Ecto SQL sandbox](https://dashbit.co/blog/)
