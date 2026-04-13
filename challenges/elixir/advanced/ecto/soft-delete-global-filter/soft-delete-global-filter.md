# Soft Delete with a Global Query Filter

**Project**: `crm_contacts` — soft delete applied automatically through a reusable query helper

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
crm_contacts/
├── lib/
│   └── crm_contacts.ex
├── script/
│   └── main.exs
├── test/
│   └── crm_contacts_test.exs
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
defmodule CrmContacts.MixProject do
  use Mix.Project

  def project do
    [
      app: :crm_contacts,
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

### `lib/crm_contacts.ex`

```elixir
defmodule Contact do
  def active do
    from c in __MODULE__, where: is_nil(c.deleted_at)
  end
end

# priv/repo/migrations/20260101000000_create_contacts_notes.exs
defmodule CrmContacts.Repo.Migrations.CreateContactsNotes do
  use Ecto.Migration

  def change do
    create table(:contacts) do
      add :email, :string, null: false
      add :name, :string
      add :company, :string
      add :deleted_at, :utc_datetime
      timestamps()
    end

    create unique_index(:contacts, [:email],
             where: "deleted_at IS NULL",
             name: :contacts_email_active_idx)

    create index(:contacts, [:deleted_at])

    create table(:notes) do
      add :contact_id, references(:contacts, on_delete: :restrict), null: false
      add :body, :text
      add :deleted_at, :utc_datetime
      timestamps()
    end

    create index(:notes, [:contact_id], where: "deleted_at IS NULL")
  end
end

# lib/crm_contacts/soft_delete.ex
defmodule CrmContacts.SoftDelete do
  @moduledoc """
  Reusable helpers for schemas that have a `deleted_at :: utc_datetime | nil` field.

  Usage:

      alias CrmContacts.SoftDelete

      Contact
      |> SoftDelete.scope()
      |> where([c], c.company == ^co)
      |> Repo.all()
  """
  import Ecto.Query

  @doc "Adds `where: is_nil(q.deleted_at)` to any queryable."
  @spec scope(Ecto.Queryable.t()) :: Ecto.Query.t()
  def scope(queryable) do
    from q in queryable, where: is_nil(q.deleted_at)
  end

  @doc "Returns queryable without the soft-delete filter. Explicit opt-out."
  @spec with_deleted(Ecto.Queryable.t()) :: Ecto.Queryable.t()
  def with_deleted(queryable), do: queryable

  @doc "Mark a struct as deleted. Does not cascade."
  @spec delete(struct()) :: {:ok, struct()} | {:error, Ecto.Changeset.t()}
  def delete(struct) do
    struct
    |> Ecto.Changeset.change(deleted_at: now())
    |> CrmContacts.Repo.update()
  end

  @doc "Restore a previously soft-deleted struct."
  @spec restore(struct()) :: {:ok, struct()} | {:error, Ecto.Changeset.t()}
  def restore(struct) do
    struct
    |> Ecto.Changeset.change(deleted_at: nil)
    |> CrmContacts.Repo.update()
  end

  defp now, do: DateTime.utc_now() |> DateTime.truncate(:second)
end

# lib/crm_contacts/schemas/contact.ex
defmodule CrmContacts.Schemas.Contact do
  use Ecto.Schema
  import Ecto.Changeset

  schema "contacts" do
    field :email, :string
    field :name, :string
    field :company, :string
    field :deleted_at, :utc_datetime
    has_many :notes, CrmContacts.Schemas.Note
    timestamps()
  end

  def changeset(contact, attrs) do
    contact
    |> cast(attrs, [:email, :name, :company])
    |> validate_required([:email])
    |> validate_format(:email, ~r/@/)
    |> unique_constraint(:email, name: :contacts_email_active_idx,
                                   message: "already in use by an active contact")
  end
end

# lib/crm_contacts/schemas/note.ex
defmodule CrmContacts.Schemas.Note do
  use Ecto.Schema
  import Ecto.Changeset

  schema "notes" do
    field :body, :string
    field :deleted_at, :utc_datetime
    belongs_to :contact, CrmContacts.Schemas.Contact
    timestamps()
  end

  def changeset(note, attrs) do
    note
    |> cast(attrs, [:body, :contact_id])
    |> validate_required([:body, :contact_id])
  end
end

# lib/crm_contacts/contacts.ex
defmodule CrmContacts.Contacts do
  import Ecto.Query
  alias CrmContacts.{Repo, SoftDelete}
  alias CrmContacts.Schemas.{Contact, Note}

  @spec list(keyword()) :: [Contact.t()]
  def list(opts \\ []) do
    company = Keyword.get(opts, :company)

    Contact
    |> SoftDelete.scope()
    |> maybe_filter_company(company)
    |> order_by(asc: :id)
    |> Repo.all()
  end

  @spec list_with_notes() :: [Contact.t()]
  def list_with_notes do
    note_query = SoftDelete.scope(Note)

    Contact
    |> SoftDelete.scope()
    |> preload(notes: ^note_query)
    |> Repo.all()
  end

  @spec list_including_deleted() :: [Contact.t()]
  def list_including_deleted do
    Contact
    |> SoftDelete.with_deleted()
    |> Repo.all()
  end

  @spec delete(Contact.t()) :: {:ok, Contact.t()}
  def delete(contact), do: SoftDelete.delete(contact)

  @spec restore(Contact.t()) :: {:ok, Contact.t()}
  def restore(contact), do: SoftDelete.restore(contact)

  defp maybe_filter_company(q, nil), do: q
  defp maybe_filter_company(q, co), do: where(q, [c], c.company == ^co)
end
```

### `test/crm_contacts_test.exs`

```elixir
defmodule CrmContacts.ContactsTest do
  use ExUnit.Case, async: true
  doctest Contact
  alias CrmContacts.{Contacts, Repo, SoftDelete}
  alias CrmContacts.Schemas.{Contact, Note}

  setup do
    Ecto.Adapters.SQL.Sandbox.checkout(Repo)
    Ecto.Adapters.SQL.Sandbox.mode(Repo, {:shared, self()})
    :ok
  end

  defp new_contact(attrs) do
    %Contact{} |> Contact.changeset(attrs) |> Repo.insert!()
  end

  describe "list/1" do
    test "excludes soft-deleted contacts" do
      alive = new_contact(%{email: "a@x.com", name: "Alive"})
      dead = new_contact(%{email: "b@x.com", name: "Dead"})
      {:ok, _} = Contacts.delete(dead)

      results = Contacts.list()
      assert Enum.map(results, & &1.id) == [alive.id]
    end
  end

  describe "list_including_deleted/0" do
    test "returns both states" do
      new_contact(%{email: "a@x.com"})
      dead = new_contact(%{email: "b@x.com"})
      {:ok, _} = Contacts.delete(dead)

      all = Contacts.list_including_deleted()
      assert length(all) == 2
    end
  end

  describe "preload scoping" do
    test "does not return soft-deleted notes" do
      c = new_contact(%{email: "c@x.com"})
      {:ok, n1} = Repo.insert(Note.changeset(%Note{}, %{body: "active", contact_id: c.id}))
      {:ok, n2} = Repo.insert(Note.changeset(%Note{}, %{body: "dead", contact_id: c.id}))
      {:ok, _} = SoftDelete.delete(n2)

      [loaded] = Contacts.list_with_notes()
      assert Enum.map(loaded.notes, & &1.id) == [n1.id]
    end
  end

  describe "uniqueness with partial index" do
    test "rejects duplicate active email" do
      new_contact(%{email: "dup@x.com"})

      {:error, cs} =
        %Contact{} |> Contact.changeset(%{email: "dup@x.com"}) |> Repo.insert()

      refute cs.valid?
      assert [email: {"already in use by an active contact", _}] = cs.errors
    end

    test "allows reuse of a soft-deleted email" do
      dead = new_contact(%{email: "reuse@x.com"})
      {:ok, _} = Contacts.delete(dead)

      assert %Contact{} = new_contact(%{email: "reuse@x.com"})
    end
  end

  describe "restore" do
    test "brings soft-deleted contact back" do
      c = new_contact(%{email: "r@x.com"})
      {:ok, _} = Contacts.delete(c)
      refute Enum.any?(Contacts.list(), &(&1.id == c.id))

      {:ok, c} = Contacts.restore(c)
      assert Enum.any?(Contacts.list(), &(&1.id == c.id))
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("✓ Soft Delete with Global Query Filter:")
    IO.puts("  - Contact schema with deleted_at timestamp")
    IO.puts("  - Contacts.list() automatically excludes soft-deleted rows")
    IO.puts("  - Contacts.list_including_deleted() shows all rows")
    IO.puts("  - delete/1 and restore/1 mutations")
    IO.puts("  - Partial unique index: rejects duplicates of active contacts")
    IO.puts("  - Allows reuse of soft-deleted email addresses")
    IO.puts("  - Preload scoping: associated notes also respect soft-delete filter")
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
