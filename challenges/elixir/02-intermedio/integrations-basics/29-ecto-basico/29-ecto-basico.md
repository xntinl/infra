# Ecto basics: Repo, Schema, and your first insert/get

**Project**: `ecto_intro` — a single-schema app with SQLite-backed `Repo`, a migration, and CRUD tests.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

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

## Implementation

### Step 1: Create the project

```bash
mix new ecto_intro --sup
cd ecto_intro
```

`--sup` scaffolds an Application module with a supervision tree, which
we need because `Repo` is a supervised process.

### Step 2: `mix.exs`

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

```elixir
defmodule EctoIntro.Repo do
  use Ecto.Repo,
    otp_app: :ecto_intro,
    adapter: Ecto.Adapters.SQLite3
end
```

### Step 5: `lib/ecto_intro/application.ex`

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

```elixir
ExUnit.start()

# Sandbox the pool so tests can share a clean DB via transactional rollback.
Ecto.Adapters.SQL.Sandbox.mode(EctoIntro.Repo, :manual)
```

### Step 10: `test/ecto_intro_test.exs`

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

```bash
mix test
```

The `test` alias drops/creates/migrates the DB first.

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

## Resources

- [Ecto — hexdocs](https://hexdocs.pm/ecto/Ecto.html)
- [`Ecto.Repo`](https://hexdocs.pm/ecto/Ecto.Repo.html)
- [`Ecto.Schema`](https://hexdocs.pm/ecto/Ecto.Schema.html)
- [`Ecto.Changeset`](https://hexdocs.pm/ecto/Ecto.Changeset.html)
- [`ecto_sqlite3`](https://hexdocs.pm/ecto_sqlite3/) — SQLite adapter used here
- [Dashbit: "The Little Ecto Cookbook"](https://dashbit.co/ebooks/the-little-ecto-cookbook)
- [Plataformatec/Dashbit blog — Ecto SQL sandbox](https://dashbit.co/blog/)
