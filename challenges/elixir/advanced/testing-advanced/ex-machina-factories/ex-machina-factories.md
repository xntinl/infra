# Fixtures vs Factories with ExMachina

**Project**: `user_directory` — an Ecto-backed user system whose tests use ExMachina factories instead of static fixtures.

---

## Why ExMachina factories matter

`user_directory` test suite has grown to 600 tests. Originally each test built users
with inline `%User{name: "Jane Doe", email: "j@d.com", ...}` literals. Over time three
problems emerged:

1. **Duplication**: adding a non-null column meant editing ~200 literals.
2. **Hidden coupling**: tests asserted on `user.name == "Jane Doe"` not because the name
   mattered, but because that was the fixture choice.
3. **Brittle invariants**: a new validation invalidated half the literals.

Factories (ExMachina) solve this by centralizing the "default valid entity" definition
in one place. Tests ask for "a user"; only the attributes that matter for the test are
overridden. When the schema evolves, the factory is updated once.

---

## The business problem

Test suites must stay readable and cheap to evolve. Static fixtures are fast to write but
brittle. Inline literals duplicate schema knowledge across hundreds of tests. Factories
give you:

- Tight Ecto integration via `ExMachina.Ecto` — `insert/2` handles changesets.
- `build/2`, `build_list/3`, `params_for/2` cover the common idioms.
- `sequence/2` generates unique values (emails, usernames) without manual counters.

---

## Project structure

```
user_directory/
├── lib/
│   └── user_directory/
│       ├── repo.ex
│       ├── users/
│       │   ├── user.ex
│       │   └── account.ex
│       └── users.ex
├── script/
│   └── main.exs
├── test/
│   ├── support/
│   │   ├── factory.ex              # ExMachina.Ecto factories
│   │   └── data_case.ex
│   ├── user_directory/
│   │   ├── users_test.exs
│   │   └── account_test.exs
│   └── test_helper.exs
└── mix.exs
```

---

## Design decisions

- **Option A — one monolithic factory module**: works for small projects.
- **Option B — domain-split factories (`Factory.Accounts`, `Factory.Billing`)**: cleaner
  at scale, each domain owns its factories.

Chosen: **Option A** for small projects, **Option B** once the factory module exceeds
~300 lines.

- **Option A — factories that always persist**: ok for DB-heavy tests, wasteful for
  in-memory assertions.
- **Option B — build by default, insert only when the test needs a row** (chosen): faster,
  more explicit. Tests pay for the DB only when they need it.

---

## Implementation

### `mix.exs`

```elixir
defmodule UserDirectory.MixProject do
  use Mix.Project

  def project do
    [
      app: :user_directory,
      version: "0.1.0",
      elixir: "~> 1.19",
      elixirc_paths: elixirc_paths(Mix.env()),
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger], mod: {UserDirectory.Application, []}]
  end

  defp deps do
    [
      {:ecto_sql, "~> 3.12"},
      {:postgrex, "~> 0.19"},
      {:ex_machina, "~> 2.8", only: :test}
    ]
  end

  defp elixirc_paths(:test), do: ["lib", "test/support"]
  defp elixirc_paths(_),     do: ["lib"]
end
```

### `lib/user_directory/users/user.ex`

```elixir
defmodule UserDirectory.Users.User do
  use Ecto.Schema
  import Ecto.Changeset

  schema "users" do
    field :email, :string
    field :name, :string
    field :role, :string, default: "member"
    has_many :accounts, UserDirectory.Users.Account
    timestamps()
  end

  def changeset(user, attrs) do
    user
    |> cast(attrs, [:email, :name, :role])
    |> validate_required([:email, :name])
    |> validate_format(:email, ~r/^[^\s]+@[^\s]+$/)
    |> validate_inclusion(:role, ["admin", "member", "readonly"])
    |> unique_constraint(:email)
  end
end
```

```elixir
# lib/user_directory/users/account.ex
defmodule UserDirectory.Users.Account do
  use Ecto.Schema
  import Ecto.Changeset

  schema "accounts" do
    field :provider, :string
    field :external_id, :string
    belongs_to :user, UserDirectory.Users.User
    timestamps()
  end

  def changeset(account, attrs) do
    account
    |> cast(attrs, [:provider, :external_id, :user_id])
    |> validate_required([:provider, :external_id, :user_id])
    |> unique_constraint([:provider, :external_id])
  end
end
```

```elixir
# test/support/factory.ex
defmodule UserDirectory.Factory do
  use ExMachina.Ecto, repo: UserDirectory.Repo

  alias UserDirectory.Users.{User, Account}

  def user_factory do
    %User{
      email: sequence(:email, &"user_#{&1}@example.com"),
      name: sequence(:name, &"User #{&1}"),
      role: "member"
    }
  end

  def admin_user_factory do
    struct!(user_factory(), role: "admin")
  end

  def account_factory do
    %Account{
      provider: sequence(:provider, ["google", "github", "apple"]),
      external_id: sequence(:external_id, &"ext_#{&1}"),
      user: build(:user)
    }
  end
end
```

### `test/user_directory_test.exs`

```elixir
# test/support/data_case.ex
defmodule UserDirectory.DataCase do
  use ExUnit.CaseTemplate

  using do
    quote do
      alias UserDirectory.Repo
      import Ecto
      import Ecto.Changeset
      import Ecto.Query
      import UserDirectory.Factory
    end
  end

  setup tags do
    pid = Ecto.Adapters.SQL.Sandbox.start_owner!(UserDirectory.Repo, shared: not tags[:async])
    on_exit(fn -> Ecto.Adapters.SQL.Sandbox.stop_owner(pid) end)
    :ok
  end
end

# test/user_directory/users_test.exs
defmodule UserDirectory.UsersTest do
  use UserDirectory.DataCase, async: true

  alias UserDirectory.Users.User
  alias UserDirectory.Repo

  describe "build vs insert" do
    test "build returns a valid struct without touching the DB" do
      user = build(:user)
      assert %User{} = user
      assert user.email =~ "@example.com"
      refute user.id
    end

    test "insert persists and returns a row with an id" do
      user = insert(:user)
      assert user.id
      assert Repo.get(User, user.id)
    end
  end

  describe "overrides" do
    test "admin role via the admin_user factory" do
      admin = insert(:admin_user)
      assert admin.role == "admin"
    end

    test "custom email via keyword override" do
      user = build(:user, email: "carla@example.com")
      assert user.email == "carla@example.com"
    end
  end

  describe "params_for" do
    test "returns a valid params map ready for a changeset" do
      params = params_for(:user)
      changeset = User.changeset(%User{}, params)
      assert changeset.valid?
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== ExMachina Factories Demo ===")
    IO.puts("This example uses ExMachina, available only in :test env.")
    IO.puts("Run `mix test` to execute the factory-driven suite.")
    IO.puts("=== Done ===")
  end
end

Main.main()
```

Run with: `elixir script/main.exs` (or `mix test` for the factory suite).

---

## Key concepts

### 1. `def <name>_factory` in the factory module
Each factory returns a struct with default valid attributes.

### 2. `build/2` vs `insert/2`
`build/2` returns a struct without hitting the DB. `insert/2` inserts through the repo
and returns the persisted struct. Default to `build` — pay for the DB only when you need it.

### 3. `params_for/2` for changeset testing
Returns a map of attributes for testing changesets or controller params, without going
through the schema.

### 4. `sequence(:key, fn n -> ... end)`
Per-test unique values. Email addresses, usernames, slugs. Counters are VM-global —
don't assert on the exact number.

### 5. Production gotchas

- Never assert on a factory default (e.g. `"User 42"`). Only assert on attributes you explicitly set.
- Sequences leak across tests (they're VM-global). That's OK for uniqueness, not for exact values.
- `after_build` hooks hide logic; prefer explicit `insert(:account, user: user)` composition.
- For a test where ONE field matters and no others are required, a literal beats a factory.
- If your schema has NO required fields, literals are simpler.

---

## Resources

- [ExMachina — hex docs](https://hexdocs.pm/ex_machina/readme.html)
- [Ecto testing guide](https://hexdocs.pm/ecto/testing-with-ecto.html)
- [Factories vs fixtures — Thoughtbot](https://thoughtbot.com/blog/factories-should-be-the-exception)
