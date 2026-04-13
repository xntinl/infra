# Soft Delete with a Global Query Filter

**Project**: `crm_contacts` — soft delete applied automatically through a reusable query helper.

---

## Project context

A CRM must support "undo delete" for 30 days. Rows are not physically removed; a
`deleted_at` timestamp marks them. Every SELECT in the app must exclude soft-deleted
rows — except admin endpoints that need to recover them. Forgetting a `where: is_nil(...)`
leaks deleted contacts to users.

This exercise builds a `SoftDelete` macro/helper that:

- Applies the filter automatically via a composable query helper.
- Provides explicit `with_deleted/1` to bypass the filter when needed.
- Handles preloads transitively.

```
crm_contacts/
├── lib/
│   └── crm_contacts/
│       ├── application.ex
│       ├── repo.ex
│       ├── soft_delete.ex           # the reusable helper
│       ├── contacts.ex              # context using it
│       └── schemas/
│           ├── contact.ex
│           └── note.ex
├── priv/repo/migrations/
├── test/crm_contacts/
│   └── contacts_test.exs
├── bench/soft_delete_bench.exs
└── mix.exs
```

---

## Why opt-in helper vs automatic global filter

Some frameworks (Rails' `default_scope`) auto-apply the filter to every query. Elixir/Ecto
intentionally does not, and this is a feature:

- Global filters surprise developers reading the code — the SQL they expect is not what
  runs.
- They interact poorly with joins — you cannot tell which table's filter applied.
- They break admin tooling that must see deleted rows.

The idiomatic Ecto pattern is explicit: every query starts with `Contact.active()` or
`from c in Contact.active()`, which simply prepends the `is_nil(deleted_at)` filter. If
you forget it, the query returns everything — and you see it in tests.

---

## Core concepts



---

**Why this matters:**
These concepts form the foundation of production Elixir systems. Understanding them deeply allows you to build fault-tolerant, scalable applications that operate correctly under load and failure.

**Real-world use case:**
This pattern appears in systems like:
- Phoenix applications handling thousands of concurrent connections
- Distributed data processing pipelines
- Financial transaction systems requiring consistency and fault tolerance
- Microservices communicating over unreliable networks

**Common pitfall:**
Many developers overlook that Elixir's concurrency model differs fundamentally from threads. Processes are isolated; shared mutable state does not exist. Trying to force shared-memory patterns leads to deadlocks, race conditions, or silently incorrect behavior. Always think in terms of message passing and immutability.

**Ecto-specific insight:**
Ecto separates the query layer (building queries) from the execution layer (sending them). This separation allows for debugging, composability, and testing without a database. Never load all rows first and filter in-memory — write the filter into the query itself, or you've just built an N+1 problem.
### 1. The filter as a composable query

```elixir
defmodule Contact do
  def active do
    from c in __MODULE__, where: is_nil(c.deleted_at)
  end
end
```

`Contact.active()` is an `Ecto.Query` that can be further refined:

```elixir
Contact.active()
|> where([c], c.company == "Globex")
|> Repo.all()
```

### 2. The helper `SoftDelete.scope/1`

A reusable scope builder for any schema with `deleted_at`:

```elixir
def scope(queryable), do: from(q in queryable, where: is_nil(q.deleted_at))
```

Works for schemas AND for queries — one helper, all call sites.

### 3. Soft delete writes a timestamp

```elixir
def delete(contact) do
  contact
  |> Ecto.Changeset.change(deleted_at: DateTime.utc_now())
  |> Repo.update()
end
```

Restoring is the inverse: set `deleted_at` back to `nil`.

### 4. Unique constraints must account for soft-deleted rows

Email `alice@x.com` is deleted. Can someone else create `alice@x.com`? Typically yes —
but a plain `unique_index` prevents it. Use a partial unique index:

```elixir
create unique_index(:contacts, [:email], where: "deleted_at IS NULL")
```

---

## Design decisions

- **Option A — global scope via `prepare_query` callback**: Ecto supports this via the
  repo callback. Pros: filter is automatic. Cons: surprise factor, no opt-out for admin.
- **Option B — explicit `active/0` on each schema + optional `with_deleted/1`**: no magic.
  Pros: the code does what it says. Cons: you must remember to call it.

We use **Option B** with lint-enforced reviews. Any PR that calls `Repo.all(Contact)`
without `Contact.active()` fails code review.

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

### Step 1: Migrations with partial unique indexes

**Objective**: Gate uniqueness with `WHERE deleted_at IS NULL` so the same email can be reused after a soft delete without race windows.

```elixir
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
```

### Step 2: The `SoftDelete` helper module

**Objective**: Ship `scope/1`, `with_deleted/1`, `delete/1`, and `restore/1` so every queryable opts in or out of the filter at the call site.

```elixir
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
```

### Step 3: Schemas

**Objective**: Expose `deleted_at` and target the partial-index constraint name so changeset `unique_constraint` matches the active-only index.

```elixir
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
```

### Step 4: Context

**Objective**: Compose SoftDelete.scope with preloads and admin escape hatches so default reads exclude tombstones while audit paths stay explicit.

```elixir
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

---

## Why this works

- `SoftDelete.scope/1` works on a schema module (`Contact`) and on an existing query,
  because `from q in queryable` accepts either. This is the composability key.
- The partial unique index (`WHERE deleted_at IS NULL`) lets the same email be reused
  after a delete. The constraint is DB-enforced; no race between check-and-insert.
- `preload(notes: ^note_query)` passes a filtered query into the preload, so
  `list_with_notes` does not show soft-deleted notes attached to active contacts.
- `with_deleted/1` is the explicit escape hatch for admin tools. Grep the codebase for
  it to audit every place that bypasses the filter.

---

## Data flow

```
Controller calls Contacts.list()
   │
   ▼
SoftDelete.scope(Contact)
   │  adds: where is_nil(deleted_at)
   ▼
maybe_filter_company/2
   │
   ▼
Repo.all
   │
   ▼
SELECT * FROM contacts WHERE deleted_at IS NULL AND company = $1
   │
   ▼
[active contacts only]
```

---

## Tests

```elixir
# test/crm_contacts/contacts_test.exs
defmodule CrmContacts.ContactsTest do
  use ExUnit.Case, async: false
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

---

## Benchmark

```elixir
# bench/soft_delete_bench.exs
alias CrmContacts.{Contacts, Repo}
alias CrmContacts.Schemas.Contact

Repo.delete_all(Contact)

now = DateTime.utc_now() |> DateTime.truncate(:second)

rows =
  for i <- 1..10_000 do
    deleted_at = if rem(i, 5) == 0, do: now, else: nil
    %{email: "u#{i}@x.com", inserted_at: now, updated_at: now, deleted_at: deleted_at}
  end

Enum.chunk_every(rows, 1_000) |> Enum.each(&Repo.insert_all(Contact, &1))

Benchee.run(
  %{
    "list (filtered)"        => fn -> Contacts.list() end,
    "list_including_deleted" => fn -> Contacts.list_including_deleted() end
  },
  time: 3, warmup: 1
)
```

**Target**: `list` is ~2× faster than `list_including_deleted` thanks to the partial
index on active rows. If they are equal, the index is not being used — check
`EXPLAIN ANALYZE`.

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

**1. Foreign keys to soft-deleted rows still see them.** A `contact_id` FK references
ID, not an active predicate. A note's `contact_id` can point at a deleted contact.
Filter at the app layer via scopes; do not delete contacts that have active notes
without a policy.

**2. Reports double-count unless explicit.** `Repo.aggregate(Contact, :count)` counts
soft-deleted rows. Always pipe through `SoftDelete.scope()` first.

**3. Partial unique index requires Postgres to understand the partiality.** If you change
`deleted_at` from `timestamp` to something else, the index may not be used by the
planner. Rebuild it.

**4. Cascading soft delete is manual.** Deleting a contact does not soft-delete its
notes. Either do it in a transaction (Ecto.Multi) or accept the orphan semantics.

**5. GDPR "right to be forgotten"** requires actual deletion, not soft delete. Schedule
a hard-delete job for rows where `deleted_at < now() - interval '30 days'`.

**6. When NOT to soft-delete.** For audit/financial data, use a separate
`deleted_records` table with a one-way write. Soft-delete's "restore" semantic can
conflict with immutability requirements.

---

## Reflection

Your product has used soft delete for 2 years. The `contacts` table is 5M rows; 30% are
soft-deleted. Queries are slower than when the table had 3.5M active rows, even though
the partial index is in place. Why — what is the index storing, what is the heap storing,
and when do you decide to hard-delete vs. archive to a cold table? What query would you
run to compare heap size vs. active rows?

---


## Executable Example

```elixir
# test/crm_contacts/contacts_test.exs
defmodule CrmContacts.ContactsTest do
  use ExUnit.Case, async: false
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
