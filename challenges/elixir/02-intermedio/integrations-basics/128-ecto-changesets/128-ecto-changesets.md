# Ecto changesets: cast, validations, and `valid?` vs `changes`

**Project**: `changeset_lab` — a schemaless and a schema-backed changeset exploring `cast`, `validate_required`, `validate_format`, and the difference between `valid?` and `changes`.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Project context

The changeset is Ecto's most distinctive idea and the piece of the library
most often misunderstood. A changeset is **not** a validation result. It's
a data structure that pairs:

- the original record (`data`),
- a set of proposed changes (`changes`) filtered through `cast`,
- accumulated validation errors (`errors`),
- constraints to be enforced on the database (`constraints`),
- and a derived `valid?` boolean.

You compose changesets with a pipeline of `cast` + `validate_*` calls.
The result is a *plan* for a mutation that `Repo.insert/update/delete` can
execute — or that a Phoenix form can render errors from without ever
touching the database.

This exercise drills the distinction between `valid?` (passed all
validations) and `changes` (what actually differs from `data`). They are
orthogonal: a changeset with **no changes** is still `valid?: true`, and a
changeset with `valid?: true` can still fail at `Repo.insert` because of
a DB constraint.

Project structure:

```
changeset_lab/
├── lib/
│   └── changeset_lab.ex
├── test/
│   └── changeset_lab_test.exs
└── mix.exs
```

No `Repo` is needed — changesets work perfectly against in-memory structs
and plain maps (schemaless changesets). This keeps the focus on the
changeset API itself.

---

## Core concepts

### 1. `cast/3` — filter, not validate

```elixir
cast(%User{}, %{"email" => "a@b.com", "rogue" => "x"}, [:email, :age])
```

`cast` copies only the keys in the third argument from `attrs` into `changes`.
Unknown keys are silently dropped — this is the mass-assignment guard.
String and atom keys are both accepted. Values are coerced to the schema's
type if possible; if coercion fails, the field becomes an error ("is invalid").

### 2. `validate_required/2` — the only validation you (almost) always need

```elixir
validate_required(changeset, [:email])
```

Adds an error if the listed fields are `nil` in `changes` **or** in `data`.
That last part matters: an update changeset where `email` is already set on
`data` passes `validate_required(:email)` even if `changes` doesn't mention
email. This is why `validate_required` and `cast` are complementary — cast
controls *what can change*, required controls *what must be present in the
end*.

### 3. `validate_format/3` and friends — cheap, in-memory

`validate_format/3` runs a regex against the current value. `validate_length/3`,
`validate_number/3`, `validate_inclusion/3`, `validate_acceptance/3` all
work similarly. They do not hit the DB. Constraints (`unique_constraint`,
`foreign_key_constraint`) only raise errors during `Repo.insert/update`.

### 4. `valid?` vs `changes`

| Scenario | `valid?` | `changes`            |
|----------|----------|----------------------|
| cast + all validations pass, no changes | `true`  | `%{}` |
| cast + all validations pass, real change | `true`  | `%{email: "..."}` |
| cast + missing required field | `false` | whatever was cast |
| cast + bad format | `false` | includes the bad value |

Phoenix uses `valid?` to decide whether to render errors; `Repo` uses it to
decide whether to run the SQL. **Empty `changes` on a `Repo.update/1` is a
no-op that still returns `{:ok, record}`** — this is occasionally surprising.

### 5. Schemaless changesets

You don't actually need a schema. Pass a `{data, types}` tuple to `cast` and
you get a changeset over plain maps. Perfect for "contact form" style input
validation that doesn't map to a table.

```elixir
types = %{name: :string, age: :integer}
{%{}, types} |> cast(attrs, Map.keys(types)) |> validate_required([:name])
```

---

## Implementation

### Step 1: Create the project

```bash
mix new changeset_lab
cd changeset_lab
```

### Step 2: `mix.exs`

```elixir
defmodule ChangesetLab.MixProject do
  use Mix.Project

  def project do
    [
      app: :changeset_lab,
      version: "0.1.0",
      elixir: "~> 1.15",
      deps: deps()
    ]
  end

  def application, do: [extra_applications: [:logger]]

  defp deps do
    [
      # `ecto` alone (no `ecto_sql`) gives us Changeset without a Repo.
      {:ecto, "~> 3.11"}
    ]
  end
end
```

Run `mix deps.get`.

### Step 3: `lib/changeset_lab.ex`

```elixir
defmodule ChangesetLab do
  @moduledoc """
  Two worked examples of `Ecto.Changeset`:

  * `SignUp` — a schema-backed changeset (embedded schema, no DB).
  * `contact_form_changeset/1` — a schemaless changeset over a plain map.

  Both illustrate the four-step pipeline: `cast` → `validate_required` →
  `validate_format` → return. Neither touches a database — `Repo.insert` is
  left to later exercises.
  """

  import Ecto.Changeset

  # --- Schema-backed changeset ---------------------------------------------

  defmodule SignUp do
    @moduledoc """
    Uses `Ecto.Schema` in *embedded* mode: we get the struct, types, and
    changeset metadata without declaring a table.
    """
    use Ecto.Schema

    @primary_key false
    embedded_schema do
      field :email, :string
      field :password, :string
      field :age, :integer
    end
  end

  @permitted [:email, :password, :age]
  @required [:email, :password]

  @doc """
  Builds a changeset for the sign-up form. Validates:
    * email and password are present,
    * email looks like an email (has `@`),
    * password is at least 8 characters,
    * age, if present, is >= 13.
  """
  @spec signup_changeset(map()) :: Ecto.Changeset.t()
  def signup_changeset(attrs) do
    %SignUp{}
    |> cast(attrs, @permitted)
    |> validate_required(@required)
    |> validate_format(:email, ~r/@/, message: "must contain @")
    |> validate_length(:password, min: 8)
    |> validate_number(:age, greater_than_or_equal_to: 13)
  end

  # --- Schemaless changeset -------------------------------------------------

  @doc """
  Schemaless changeset — no struct needed. Handy for one-off input forms
  that don't correspond to a DB table.
  """
  @spec contact_form_changeset(map()) :: Ecto.Changeset.t()
  def contact_form_changeset(attrs) do
    types = %{name: :string, topic: :string, message: :string}

    {%{}, types}
    |> cast(attrs, Map.keys(types))
    |> validate_required([:name, :message])
    |> validate_length(:message, min: 10, max: 1_000)
    |> validate_inclusion(:topic, ~w(bug feature question other))
  end
end
```

### Step 4: `test/changeset_lab_test.exs`

```elixir
defmodule ChangesetLabTest do
  use ExUnit.Case, async: true

  alias Ecto.Changeset

  describe "signup_changeset/1 — valid?" do
    test "valid with all required fields" do
      cs = ChangesetLab.signup_changeset(%{
        "email" => "a@b.com",
        "password" => "secret12"
      })

      assert cs.valid?
      assert cs.changes == %{email: "a@b.com", password: "secret12"}
      assert cs.errors == []
    end

    test "invalid when required fields are missing" do
      cs = ChangesetLab.signup_changeset(%{})

      refute cs.valid?
      assert [{:email, _}, {:password, _}] = Enum.sort(cs.errors)
    end

    test "invalid email format" do
      cs = ChangesetLab.signup_changeset(%{"email" => "nope", "password" => "secret12"})

      refute cs.valid?
      assert {"must contain @", _} = cs.errors[:email]
      # Even though it's invalid, the bad value is still in changes — so the
      # form can re-render it. This is the "changes ≠ valid?" distinction.
      assert cs.changes[:email] == "nope"
    end

    test "password too short" do
      cs = ChangesetLab.signup_changeset(%{"email" => "a@b.com", "password" => "short"})
      refute cs.valid?
      assert cs.errors[:password] |> elem(0) =~ "should be at least"
    end
  end

  describe "signup_changeset/1 — cast filtering" do
    test "unknown keys are silently dropped" do
      cs = ChangesetLab.signup_changeset(%{
        "email" => "a@b.com",
        "password" => "secret12",
        "admin" => true,
        "role" => "superuser"
      })

      assert cs.valid?
      refute Map.has_key?(cs.changes, :admin)
      refute Map.has_key?(cs.changes, :role)
    end

    test "type coercion: age as string → integer" do
      cs = ChangesetLab.signup_changeset(%{
        "email" => "a@b.com",
        "password" => "secret12",
        "age" => "25"
      })

      assert cs.valid?
      assert cs.changes.age === 25
    end

    test "type coercion failure is reported as :invalid" do
      cs = ChangesetLab.signup_changeset(%{
        "email" => "a@b.com",
        "password" => "secret12",
        "age" => "not-a-number"
      })

      refute cs.valid?
      assert {_, [type: :integer, validation: :cast]} = cs.errors[:age]
    end
  end

  describe "contact_form_changeset/1 — schemaless" do
    test "valid form" do
      cs = ChangesetLab.contact_form_changeset(%{
        "name" => "Ada",
        "topic" => "bug",
        "message" => "something is broken here"
      })

      assert cs.valid?
    end

    test "invalid topic" do
      cs = ChangesetLab.contact_form_changeset(%{
        "name" => "Ada",
        "topic" => "gossip",
        "message" => "something is broken here"
      })

      refute cs.valid?
      assert cs.errors[:topic] |> elem(0) =~ "is invalid"
    end

    test "message too short" do
      cs = ChangesetLab.contact_form_changeset(%{
        "name" => "Ada",
        "message" => "short"
      })

      refute cs.valid?
    end
  end

  describe "applying changes" do
    test "Ecto.Changeset.apply_changes/1 returns the updated struct" do
      cs = ChangesetLab.signup_changeset(%{"email" => "a@b.com", "password" => "secret12"})
      result = Changeset.apply_changes(cs)
      assert %ChangesetLab.SignUp{email: "a@b.com", password: "secret12"} = result
    end

    test "apply_action/2 returns {:error, cs} when invalid" do
      cs = ChangesetLab.signup_changeset(%{})
      assert {:error, %Changeset{valid?: false}} = Changeset.apply_action(cs, :insert)
    end
  end
end
```

### Step 5: Run

```bash
mix test
```

---

## Trade-offs and production gotchas

**1. `cast` silently drops unknown keys — forever a debugging gotcha**
You'll spend 20 minutes wondering why `role` isn't saving before realizing
it's not in `@permitted`. Good default for security, rough on first contact.
When debugging, log `changeset.changes` before `Repo.insert`.

**2. `validate_required` checks `data` too**
On updates, a field already set on the record satisfies required even if
the new changeset doesn't mention it. This is correct behavior — but if
you expected "must always be in this submission", it's surprising.

**3. Constraints ≠ validations**
`unique_constraint(:email)` does not check uniqueness in the changeset.
It attaches a pattern that translates a DB `unique_violation` into a
changeset error during `Repo.insert`. If the index doesn't exist, the
violation never fires, and duplicates slip through. See exercise 129.

**4. An empty `changes` still returns `{:ok, record}` on update**
`Repo.update/1` short-circuits when there are no changes. If you rely on
`updated_at` bumping, you'll notice it doesn't. Use `force_change/3` to
force an update when semantics demand it (audit logs, cache invalidation).

**5. `apply_changes/1` vs `apply_action/2`**
`apply_changes/1` always returns the struct regardless of validity. Use it
only when you've already checked `valid?`. `apply_action/2` returns
`{:ok, struct} | {:error, changeset}` — prefer it for contexts that don't
hit a `Repo` but still want the tagged-tuple API for callers.

**6. When NOT to use changesets**
For internal data transformations where you control both sides, changesets
are overhead. For CLI arg parsing, `OptionParser` is simpler. For one-off
coercions, `Ecto.Type.cast/2` directly beats a full changeset. Changesets
shine for *external* input: HTTP forms, JSON bodies, CSV rows.

---

## Resources

- [`Ecto.Changeset` — hexdocs](https://hexdocs.pm/ecto/Ecto.Changeset.html)
- [`cast/4`](https://hexdocs.pm/ecto/Ecto.Changeset.html#cast/4)
- [`validate_required/3`](https://hexdocs.pm/ecto/Ecto.Changeset.html#validate_required/3)
- [Schemaless changesets guide](https://hexdocs.pm/ecto/embedded-schemas.html) and [Schemaless queries](https://hexdocs.pm/ecto/schemaless-queries.html)
- [Dashbit: "The Little Ecto Cookbook"](https://dashbit.co/ebooks/the-little-ecto-cookbook) — full of changeset recipes
