# Ecto changesets: cast, validations, and `valid?` vs `changes`

**Project**: `changeset_lab` — a schemaless and a schema-backed changeset exploring `cast`, `validate_required`, `validate_format`, and the difference between `valid?` and `changes`.

---

## Why ecto changesets matters

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

---

## Project structure

```
changeset_lab/
├── lib/
│   └── changeset_lab.ex
├── script/
│   └── main.exs
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

## Why changesets and not ad-hoc validation

A `with`-chain of `Map.fetch`, pattern matches, and regex checks works
for one field but collapses under real forms: you need to collect
multiple errors (not bail on the first), preserve the bad input for
re-rendering, distinguish "not provided" from "provided but invalid",
and translate DB constraint violations into the same error list.
Changesets solve all four in one data structure designed around that
exact workflow.

---

## Design decisions

**Option A — One big `validate_all/1` function returning `{:ok, map} | {:error, list}`**
- Pros: Simple signature; no Ecto dep needed; familiar to most devs.
- Cons: Errors are a flat list — hard to bind to form fields; no
  distinction between cast failure and validation failure; no hook
  for DB constraint errors.

**Option B — `Ecto.Changeset` pipeline (cast + validate_*)** (chosen)
- Pros: Errors are keyed by field (form-friendly); `changes` preserves
  bad input; `valid?` and `changes` are orthogonal and meaningful;
  constraint hooks integrate with `Repo.insert/update` cleanly.
- Cons: Requires the `:ecto` dep even for schemaless use; the API is
  learned, not intuitive (`cast` vs `validate_required` surprises).

→ Chose **B** because any form that grows beyond three fields hits the
  limits of ad-hoc validation fast; changesets scale from schemaless
  forms to full DB-backed writes without rewriting the validation layer.

---

## Implementation

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — isolated from any external state, so we demonstrate this concept cleanly without dependencies.

```bash
mix new changeset_lab
cd changeset_lab
```

### `mix.exs`
**Objective**: Declare dependencies and project config in `mix.exs`.

```elixir
defmodule ChangesetLab.MixProject do
  use Mix.Project

  def project do
    [
      app: :changeset_lab,
      version: "0.1.0",
      elixir: "~> 1.19",
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

### `lib/changeset_lab.ex`

**Objective**: Implement `changeset_lab.ex` — the integration seam where external protocol semantics meet Elixir domain code.

```elixir
defmodule ChangesetLab do
  @moduledoc """
  Two worked examples of `Ecto.Changeset`:

  * `SignUp` — a schema-backed changeset (embedded schema, no DB).
  * `contact_form_changeset/1` — a schemaless changeset over a plain map.

  Both illustrate the four-step pipeline: `cast` → `validate_required` →
  `validate_format` → return. Neither touches a database — database persistence via
  `Repo.insert` is beyond this exercise's scope.
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

**Objective**: Write `changeset_lab_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.

```elixir
defmodule ChangesetLabTest do
  use ExUnit.Case, async: true

  doctest ChangesetLab

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

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.

```bash
mix test
```

### Why this works

`cast/3` filters attributes against a whitelist — the mass-assignment
guard. `validate_*` functions append to the changeset's `errors` list
without bailing, so one pass yields every problem at once. `valid?` is
derived from `errors == []`, and `changes` carries whatever was
accepted (including bad-but-castable values) so forms can re-render
the user's input next to the error message. Schemaless mode uses the
same pipeline with an in-memory type map, so "form that doesn't map to
a table" reuses the same machinery.

---

### `script/main.exs`

```elixir
defmodule Main do
  defmodule ChangesetLab do
    @moduledoc """
    Two worked examples of `Ecto.Changeset`:

    * `SignUp` — a schema-backed changeset (embedded schema, no DB).
    * `contact_form_changeset/1` — a schemaless changeset over a plain map.

    Both illustrate the four-step pipeline: `cast` → `validate_required` →
    `validate_format` → return. Neither touches a database — database persistence via
    `Repo.insert` is beyond this exercise's scope.
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

  def main do
    IO.puts("=== User Demo ===
  ")
  
    # Demo: Create and validate a changeset
  changeset = User.changeset(%User{}, %{"name" => "Alice", "email" => "alice@example.com"})
  IO.puts("1. Valid changeset: valid=#{changeset.valid?}")
  assert changeset.valid?

  changeset_bad = User.changeset(%User{}, %{"name" => ""})
  IO.puts("2. Invalid changeset: valid=#{changeset_bad.valid?}")
  assert not changeset_bad.valid?

  IO.puts("
  ✓ Ecto changesets demo completed!")
  end

end

Main.main()
```

## Deep Dive: ETS Concurrency Trade-Offs and Operation Atomicity

ETS (Erlang Term Storage) is mutable, shared, in-process state—antithetical to Elixir's immutability. But it's required for specific cases: large shared datasets, fast lookups under contention, atomic counters across processes. Trade-off: operations aren't composable (no atomic multi-table updates without extra bookkeeping), and debugging is harder because mutations are invisible in code.

Use ETS when: (1) true sharing between processes, (2) data is large (megabytes), (3) sub-millisecond latency required. Use GenServer when: (1) single process owns state, (2) dataset is small, (3) complex transition logic.

Most common mistake: using ETS to work around GenServer bottlenecks without profiling. Profile usually shows either handler logic is expensive (move it out) or contention from N processes calling it. ETS solves contention via sharding: split state across tables/processes indexed by key. Always profile before choosing ETS.

## Benchmark

<!-- benchmark N/A: changeset evaluation is microseconds-per-field on
     modern hardware; wall time is dominated by DB round-trips, not
     validation. Target: a 10-field signup changeset runs in well under
     100µs end-to-end. -->

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
violation never fires, and duplicates slip through.

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

## Reflection

- Your signup form has 20 fields across three tabs. Would you use one
  changeset or three? What are the tradeoffs in error display,
  per-tab persistence, and the "user went back and forth" case?
- A legacy endpoint accepts a payload with inconsistent key casing
  (`email`, `Email`, `EMAIL`). Cast only accepts predictable keys.
  How would you normalize input before casting without swallowing the
  silent-drop safety cast provides?

## Resources

- [`Ecto.Changeset` — hexdocs](https://hexdocs.pm/ecto/Ecto.Changeset.html)
- [`cast/4`](https://hexdocs.pm/ecto/Ecto.Changeset.html#cast/4)
- [`validate_required/3`](https://hexdocs.pm/ecto/Ecto.Changeset.html#validate_required/3)
- [Schemaless changesets guide](https://hexdocs.pm/ecto/embedded-schemas.html) and [Schemaless queries](https://hexdocs.pm/ecto/schemaless-queries.html)
- [Dashbit: "The Little Ecto Cookbook"](https://dashbit.co/ebooks/the-little-ecto-cookbook) — full of changeset recipes

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

### `test/changeset_lab_test.exs`

```elixir
defmodule ChangesetLabTest do
  use ExUnit.Case, async: true

  doctest ChangesetLab

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert ChangesetLab.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts
Changesets represent intended changes to data with validations and error tracking. A changeset carries the original data, the proposed changes, and validation results—all without touching the database. Changesets separate concerns: form validation is local (in the changeset), database constraints are handled separately (foreign keys, unique indexes). This enables layered error handling: first validate in Elixir, then catch DB-level violations and translate to user-friendly messages. The pattern: validate with `validate_*` functions, then `Repo.insert/2` or `Repo.update/2` to execute. Changesets are central to Ecto; mastering them is essential for clean data mutation. They also provide a testing boundary: test changesets independently without database setup.

---
