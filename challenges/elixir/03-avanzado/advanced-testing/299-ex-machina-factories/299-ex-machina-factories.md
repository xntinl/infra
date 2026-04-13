# Fixtures vs Factories with ExMachina

**Project**: `user_directory` — an Ecto-backed user system whose tests use ExMachina factories instead of static fixtures.

## Project context

`user_directory` test suite has grown to 600 tests. Originally each test built users
with inline `%User{name: "Jane Doe", email: "j@d.com", ...}` literals. Over time three
problems emerged:

1. **Duplication**: adding a non-null column meant editing ~200 literals.
2. **Hidden coupling**: several tests asserted on `user.name == "Jane Doe"` not because
   the name mattered, but because that was the literal chosen in the fixture.
3. **Brittle invariants**: a changeset validation was added; half the literals became
   invalid and had to be fixed test-by-test.

Factories (ExMachina) solve this by centralizing the "default valid entity" definition
in one place. Tests ask for "a user"; only the attributes that matter for the test are
overridden. When the schema evolves, the factory is updated once.

```
user_directory/
├── lib/
│   └── user_directory/
│       ├── repo.ex
│       ├── users/
│       │   ├── user.ex
│       │   └── account.ex
│       └── users.ex
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

## Why ExMachina over static fixtures

- **Static fixtures** (`priv/test_fixtures.exs` with hardcoded rows): fast to write, brittle
  to evolve, tie tests to specific data, hard to compose (creating a user with 3 accounts
  means more hardcoded rows).
- **Inline literals**: duplication at scale.
- **Factories**: one source of truth, composable (`build_list(3, :account, user: user)`),
  override exactly what matters (`build(:user, email: "x@y.com")`).

## Why ExMachina specifically

- Tight Ecto integration via `ExMachina.Ecto` — `insert/2` handles changesets.
- `build/2`, `build_list/3`, `params_for/2` cover the common idioms (struct, list of
  structs, params map).
- `sequence/2` generates unique values (emails, usernames) without manual counters.

## Core concepts

### 1. `def <name>_factory` in the factory module
Each factory returns a struct with default valid attributes.

### 2. `build/2` returns a struct without hitting the DB
Use when you just need an in-memory value.

### 3. `insert/2` inserts through the repo
Returns the persisted struct.

### 4. `params_for/2` returns a map
Use for testing changesets or controller params.

### 5. `sequence(:key, fn n -> ... end)`
Per-test unique values. Email addresses, usernames, slugs.

## Design decisions

- **Option A — one monolithic factory module**: works for small projects.
- **Option B — domain-split factories (`Factory.Accounts`, `Factory.Billing`)**: cleaner
  at scale, each domain owns its factories.

Chosen: **Option A** for small projects, **Option B** once the factory module exceeds
~300 lines.

Additional:
- **Option A — factories that always persist**: ok for DB-heavy tests, wasteful for
  in-memory assertions.
- **Option B — build by default, insert only when the test needs a row**: faster, more
  explicit.

Chosen: **Option B**. Tests pay for the DB only when they need it.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  [
    {:ecto_sql, "~> 3.12"},
    {:postgrex, "~> 0.19"},
    {:ex_machina, "~> 2.8", only: :test}
  ]
end

defp elixirc_paths(:test), do: ["lib", "test/support"]
defp elixirc_paths(_),     do: ["lib"]
```

### Step 1: schemas

**Objective**: Model `User` with unique email and role inclusion so factories must respect real constraints, not bypass them.

```elixir
# lib/user_directory/users/user.ex
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

### Step 2: the factory

**Objective**: Use `sequence/2` for per-factory uniqueness and compose `admin_user` over `user` so overrides stay minimal and intent-revealing.

```elixir
# test/support/factory.ex
defmodule UserDirectory.Factory do
  use ExMachina.Ecto, repo: UserDirectory.Repo

  alias UserDirectory.Users.{User, Account}

  @doc """
  Default user factory — email is sequence-based to guarantee uniqueness across tests.
  """
  def user_factory do
    %User{
      email: sequence(:email, &"user_#{&1}@example.com"),
      name: sequence(:name, &"User #{&1}"),
      role: "member"
    }
  end

  @doc """
  Admin user — composes over user_factory, overrides only the role.
  """
  def admin_user_factory do
    struct!(user_factory(), role: "admin")
  end

  @doc """
  Account belonging to a fresh user. Callers typically pass `user:` to bind to
  a specific user they already built.
  """
  def account_factory do
    %Account{
      provider: sequence(:provider, ["google", "github", "apple"]),
      external_id: sequence(:external_id, &"ext_#{&1}"),
      user: build(:user)
    }
  end
end
```

### Step 3: DataCase imports Factory

**Objective**: Auto-import factory helpers into every `DataCase` test so `build`, `insert`, `params_for` are first-class without per-file boilerplate.

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
```

### Step 4: tests using factories

**Objective**: Contrast `build/insert/params_for` so tests only pay the DB cost when persistence is actually under assertion.

```elixir
# test/user_directory/users_test.exs
defmodule UserDirectory.UsersTest do
  use UserDirectory.DataCase, async: true

  alias UserDirectory.Users.User
  alias UserDirectory.Repo

  describe "build vs insert — only pay for the DB when needed" do
    test "build returns a valid struct without touching the DB" do
      user = build(:user)

      assert %User{} = user
      assert user.email =~ "@example.com"
      # id is nil — nothing was persisted
      refute user.id
    end

    test "insert persists and returns a row with an id" do
      user = insert(:user)

      assert user.id
      assert Repo.get(User, user.id)
    end
  end

  describe "overrides — override only the attributes the test cares about" do
    test "admin role via the admin_user factory" do
      admin = insert(:admin_user)
      assert admin.role == "admin"
    end

    test "custom email via keyword override" do
      user = build(:user, email: "carla@example.com")
      assert user.email == "carla@example.com"
    end
  end

  describe "params_for — changeset testing without persistence" do
    test "returns a valid params map ready for a changeset" do
      params = params_for(:user)

      changeset = User.changeset(%User{}, params)
      assert changeset.valid?
    end

    test "combined with overrides, tests invalid changesets" do
      params = params_for(:user, email: "not-an-email")

      changeset = User.changeset(%User{}, params)
      refute changeset.valid?
      assert %{email: ["has invalid format"]} = errors_on(changeset)
    end
  end

  describe "composition — building graphs of related entities" do
    test "insert_list creates N independent users" do
      users = insert_list(3, :user)

      assert length(users) == 3
      assert length(Enum.uniq_by(users, & &1.email)) == 3
    end
  end

  # Ecto changeset error helper — typically defined in DataCase
  defp errors_on(changeset) do
    Ecto.Changeset.traverse_errors(changeset, fn {msg, opts} ->
      Regex.replace(~r"%{(\w+)}", msg, fn _, key ->
        opts |> Keyword.get(String.to_existing_atom(key), key) |> to_string()
      end)
    end)
  end
end
```

```elixir
# test/user_directory/account_test.exs
defmodule UserDirectory.AccountTest do
  use UserDirectory.DataCase, async: true

  describe "account factory composition" do
    test "building an account also builds a user" do
      account = build(:account)
      assert account.user
      refute account.user.id
    end

    test "inserting binds the account to the given user" do
      user = insert(:user)
      account = insert(:account, user: user)

      assert account.user_id == user.id
    end
  end
end
```

## Why this works

- `sequence/2` guarantees uniqueness without manual counters. Each call advances a
  per-factory counter.
- `build/1` avoids the DB for tests that just need a struct. `insert/1` goes through
  the repo and validates via the schema's changeset.
- The factory is the single source of truth. Schema changes hit ONE place.
- Overrides are explicit — tests only mention the attributes that matter for the
  assertion. Tests become more readable: `insert(:user, role: "admin")` tells the
  reader "this test cares about the admin role, nothing else".

## Tests

See Step 4.

## Benchmark

Factory overhead is negligible: `build/1` is a struct literal + closure application
(< 5µs). `insert/1` is bound by the DB round-trip (~500µs locally).

```elixir
Benchee.run(%{
  "build(:user)" => fn -> UserDirectory.Factory.build(:user) end
}, time: 2)
```

Target: `build/1` < 10µs; `insert/1` < 1ms.

## Deep Dive: Property Patterns and Production Implications

Property-based testing inverts the testing mindset: instead of writing examples, you state invariants (properties) and let a generator find counterexamples. StreamData's shrinking capability is its superpower—when a property fails on a 10,000-element list, the framework reduces it to the minimal list that still fails, cutting debugging time from hours to minutes. The trade-off is that properties require rigorous thinking about domain constraints, and not every invariant is worth expressing as a property. Teams that adopt property testing often find bugs in specifications themselves, not just implementations.

---

## Advanced Considerations

Production testing strategies require careful attention to resource management and test isolation across multiple concurrent test processes. In large codebases, tests can consume significant memory and CPU resources, especially when using concurrent testing without proper synchronization and cleanup. The BEAM scheduler's preemptive nature means test processes may interfere with each other if shared resources aren't properly isolated at the process boundary. Pay careful attention to how Ecto's sandbox mode interacts with your supervision tree — if you have GenServers that hold state across tests, the sandbox rollback mechanism may leave phantom processes in your monitoring systems that continue consuming resources until forced cleanup occurs.

When scaling tests to production-grade test suites, consider the cost of stub verification and the memory overhead of generated test cases. Each property-based test invocation can create thousands of synthetic test cases, potentially causing garbage collection pressure that's invisible during local testing but becomes critical in CI/CD pipelines running long test suites continuously. The interaction between concurrent tests and ETS tables (often used in caches and registry patterns) requires explicit `inherited: true` options to prevent unexpected sharing between test processes, which can cause mysterious failures when tests run in different orders or under load.

For distributed testing scenarios using tools like `Peer`, network simulation can mask real latency issues and failure modes. Test timeouts that work locally may fail in CI due to scheduler contention and GC pauses. Always include substantial buffers for timeout values and monitor actual execution times under load. The coordination between multiple test nodes requires careful cleanup — a failure in test coordination can leave zombie processes consuming resources indefinitely. Implement proper telemetry hooks within your test helpers to diagnose production-like scenarios and capture performance characteristics.


## Trade-offs and production gotchas

**1. Tests asserting on factory defaults**
If a test asserts on `user.name == "User 42"` because that is what `sequence/2`
returned, the test is wrong. Only assert on attributes you explicitly set in the test.

**2. Sequences leaking across tests**
Sequences are VM-global. Two async tests both calling `build(:user)` see different
email integers but the counter keeps growing. This is OK for uniqueness — just do
not assert on the exact number.

**3. Factories that always persist with `insert/1`**
If every factory call hits the DB, your test suite's DB cost explodes. Default to
`build/1`; switch to `insert/1` only when the DB row is needed.

**4. Complex factory `after_build` hooks**
ExMachina supports hooks but they hide logic. Prefer explicit `insert(:account, user: user)`
over a hook that conditionally creates a user. Make composition visible.

**5. Overusing factories where literals are clearer**
For a test with exactly ONE field that matters, `%User{email: "a"}` reads better than
`build(:user, email: "a")` IF the literal is valid. Use factories when the schema has
required fields the test doesn't care about.

**6. When NOT to use this**
If your schema has no required fields (rare), literals are simpler. If the test is
genuinely about a specific fixture (imported from a known file), a fixture is honest.

## Reflection

Factories centralize what a "valid" entity looks like. When validation rules diverge
between "what the production DB accepts" and "what the test factory generates" — for
instance, the factory generates emails the production validator would reject — the
test suite is green while production is broken. How would you detect this drift
automatically?

## Resources

- [ExMachina](https://github.com/thoughtbot/ex_machina)
- [`ExMachina.Ecto`](https://hexdocs.pm/ex_machina/ExMachina.Ecto.html)
- [ThoughtBot — factories vs fixtures](https://thoughtbot.com/blog/factories-should-be-the-bare-minimum)
- [`Ecto.Changeset.traverse_errors/2`](https://hexdocs.pm/ecto/Ecto.Changeset.html#traverse_errors/2)

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```
