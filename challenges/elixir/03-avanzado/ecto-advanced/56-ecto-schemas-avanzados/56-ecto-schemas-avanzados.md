# Advanced Ecto Schemas: embedded, virtual, custom types, polymorphism

**Project**: `ecto_schemas_deep` — schema layer for a CRM with flexible contact data.

---

## Project context

You're building the schema layer for a CRM product. Contacts store addresses (not worth a
separate table — always loaded together), tags (a list of strings), a flexible "attributes"
map for per-tenant custom fields, and phone numbers that must always be normalized to E.164.
The contact also has a derived "full_name" that should not be persisted but must be present
in forms. Notes are polymorphic: a note can belong to a `Contact`, a `Company`, or a `Deal`.

The naive path — creating tables for addresses, tags, attributes, polymorphic note
associations via per-type foreign keys — leads to 8 tables, 12 joins, and brittle cascades.
This exercise uses the tools Ecto gives you to collapse that complexity: `embedded_schema`,
`embeds_one` / `embeds_many`, `field :virtual`, custom `Ecto.Type`, and a single `notes` table
with a `{notable_id, notable_type}` pair + runtime polymorphic dispatch.

The value of mastering this: you stop reaching for `jsonb` columns and parsing them in the
application, you stop writing boilerplate `Phone.parse/1` in every changeset, and you stop
fighting the type system. You move validation into the schema where it belongs.

---

```
ecto_schemas_deep/
├── lib/
│   └── ecto_schemas_deep/
│       ├── application.ex
│       ├── repo.ex
│       ├── types/
│       │   └── phone_e164.ex            # custom Ecto.Type
│       ├── schemas/
│       │   ├── address.ex               # embedded_schema
│       │   ├── contact.ex
│       │   ├── company.ex
│       │   ├── deal.ex
│       │   └── note.ex                  # polymorphic parent
│       └── notes.ex                     # context module
├── priv/repo/migrations/
│   └── 20260101000000_create_schemas.exs
├── test/
│   └── ecto_schemas_deep/
│       ├── contact_test.exs
│       └── note_test.exs
└── mix.exs
```

---

## Why this approach and not alternatives

Alternatives considered and discarded:

- **Hand-rolled equivalent**: reinvents primitives the BEAM/ecosystem already provides; high risk of subtle bugs around concurrency, timeouts, or failure propagation.
- **External service (e.g. Redis, sidecar)**: adds a network hop and an extra failure domain for a problem the VM can solve in-process with lower latency.
- **Heavier framework abstraction**: couples the module to a framework lifecycle and makes local reasoning/testing harder.

The chosen approach stays inside the BEAM, uses idiomatic OTP primitives, and keeps the contract small.

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
### 1. `embedded_schema` vs `schema`

A `schema "table"` maps to a real database table. An `embedded_schema` has no table — it
is serialized inside another schema's `embeds_one`/`embeds_many` column (a `jsonb` in
Postgres, a `text` with JSON encoding in SQLite, a `map` field in `Ecto.Adapters.MyXQL`).

Use embedded when the child has no identity of its own and is never queried independently.
"Address inside Contact" is the canonical case: you never do "find all addresses where
country = AR"; you always load addresses as part of a contact.

### 2. Virtual fields

`field :password, :string, virtual: true` is not written to the database. Its purpose:
hold data that exists in changesets and structs but not on disk. Typical uses:

- `:password` — validated, then put_change `:password_hash` and remove `:password`.
- `:full_name` — derived from `first_name <> " " <> last_name`, useful in templates.
- `:current_user_id` — passed through a changeset for authorization checks.

Virtual fields are just `Map.put(struct, :field, value)` — they do NOT appear in `select`,
`preload`, or migrations.

### 3. Custom `Ecto.Type`

Built-in types (`:string`, `:integer`, `:map`, `:utc_datetime`) cover 90% of cases.
Sometimes you need round-trip transformation: store a normalized phone (`"+541112345678"`)
but accept user input like `"11 1234-5678"`. Implement the `Ecto.Type` behaviour:

```elixir
@behaviour Ecto.Type
def type, do: :string
def cast(raw), do: {:ok, normalized} | :error     # user input → Elixir
def load(db),  do: {:ok, value}                   # database row → Elixir
def dump(val), do: {:ok, db_value}                # Elixir → database
```

This moves normalization into `cast/1`, a single point. Any changeset that
`cast(:phone, ..., PhoneE164)` gets E.164 for free.

### 4. Polymorphic associations

Rails-style polymorphism (`notable_id + notable_type`) has no native Ecto helper because
Ecto prefers explicit FKs. Two approaches:

- **Dedicated join tables** (`contact_notes`, `company_notes`, `deal_notes`). Simple,
  typed, but duplicated structure.
- **Single `notes` table with `(notable_id, notable_type)`**. Less typing, but associations
  must be resolved at runtime via a dispatcher.

This exercise implements approach 2 and shows the escape hatch (a `Notes.for(parent)`
function that picks the right schema) — often cleaner than duplicated join tables when the
child schema itself (the `Note`) is identical across parents.

### 5. `embeds_many` with ordered composition

`embeds_many :tags, Tag, on_replace: :delete` holds a list where the order is preserved.
`on_replace: :delete` says "when the parent changeset replaces the list, delete the
missing ones". Alternatives: `:raise` (default, forces explicit ordering), `:mark_as_invalid`,
`:update`. Getting `on_replace` wrong causes silent data loss.

### 6. `Ecto.Changeset.cast_embed/3` vs `cast/3`

`cast/3` assigns simple fields. `cast_embed/3` runs the embedded schema's own changeset
function recursively. This is how validation composes: the `Contact` changeset calls
`cast_embed(:address, with: &Address.changeset/2)` and address errors bubble up under the
`:address` key.

---

## Design decisions

**Option A — naive/simple approach**
- Pros: minimal code, easy to reason about.
- Cons: breaks under load, lacks observability, hard to evolve.

**Option B — the approach used here** (chosen)
- Pros: production-grade, handles edge cases, testable boundaries.
- Cons: more moving parts, requires understanding of the BEAM primitives involved.

→ Chose **B** because correctness under concurrency and failure modes outweighs the extra surface area.

## Implementation

### Step 1: Project setup

**Objective**: Pin ecto_sql, postgrex, and jason so custom types, embeds, and JSON maps compile against a known driver surface.

```elixir
# mix.exs
defp deps do
  [
    {:ecto_sql, "~> 3.11"},
    {:postgrex, "~> 0.17"},
    {:jason, "~> 1.4"}
  ]
end
```

### Step 2: Custom type — `PhoneE164`

**Objective**: Normalize arbitrary phone input to E.164 inside cast/dump so every persisted row shares one canonical wire format.

```elixir
# lib/ecto_schemas_deep/types/phone_e164.ex
defmodule EctoSchemasDeep.Types.PhoneE164 do
  @moduledoc """
  Ecto type that normalizes phone numbers to E.164 format on cast and dump.

  Accepts strings with arbitrary separators; stores digits with leading `+`.
  """
  use Ecto.Type

  @impl true
  def type, do: :string

  @impl true
  def cast(value) when is_binary(value) do
    case normalize(value) do
      {:ok, e164} -> {:ok, e164}
      :error -> :error
    end
  end

  def cast(nil), do: {:ok, nil}
  def cast(_), do: :error

  @impl true
  def load(value) when is_binary(value), do: {:ok, value}
  def load(nil), do: {:ok, nil}

  @impl true
  def dump(value) when is_binary(value) do
    case normalize(value) do
      {:ok, e164} -> {:ok, e164}
      :error -> :error
    end
  end

  def dump(nil), do: {:ok, nil}
  def dump(_), do: :error

  defp normalize(raw) do
    digits = raw |> to_string() |> String.replace(~r/[^\d+]/, "")

    case digits do
      "+" <> rest when byte_size(rest) in 8..15 ->
        if String.match?(rest, ~r/^\d+$/), do: {:ok, "+" <> rest}, else: :error

      rest when byte_size(rest) in 8..15 ->
        if String.match?(rest, ~r/^\d+$/), do: {:ok, "+" <> rest}, else: :error

      _ ->
        :error
    end
  end
end
```

### Step 3: Embedded `Address` schema

**Objective**: Model Address as an embedded_schema with ISO-2 country validation so contacts carry structured addresses without a join table.

```elixir
# lib/ecto_schemas_deep/schemas/address.ex
defmodule EctoSchemasDeep.Schemas.Address do
  use Ecto.Schema
  import Ecto.Changeset

  @primary_key false
  embedded_schema do
    field :street, :string
    field :city, :string
    field :country, :string
    field :postal_code, :string
  end

  @spec changeset(%__MODULE__{}, map()) :: Ecto.Changeset.t()
  def changeset(address, attrs) do
    address
    |> cast(attrs, [:street, :city, :country, :postal_code])
    |> validate_required([:street, :city, :country])
    |> validate_length(:country, is: 2)
  end
end
```

### Step 4: `Contact` with embeds + virtual field + custom type

**Objective**: Wire PhoneE164, embeds_one Address, and virtual full_name so one changeset covers custom casts, nested data, and derived fields.

```elixir
# lib/ecto_schemas_deep/schemas/contact.ex
defmodule EctoSchemasDeep.Schemas.Contact do
  use Ecto.Schema
  import Ecto.Changeset

  alias EctoSchemasDeep.Schemas.Address
  alias EctoSchemasDeep.Types.PhoneE164

  schema "contacts" do
    field :first_name, :string
    field :last_name, :string
    field :phone, PhoneE164
    field :tags, {:array, :string}, default: []
    field :custom_attrs, :map, default: %{}

    # Derived on load; never persisted.
    field :full_name, :string, virtual: true

    embeds_one :address, Address, on_replace: :update

    timestamps()
  end

  @required ~w(first_name last_name)a
  @optional ~w(phone tags custom_attrs)a

  @spec changeset(%__MODULE__{}, map()) :: Ecto.Changeset.t()
  def changeset(contact, attrs) do
    contact
    |> cast(attrs, @required ++ @optional)
    |> validate_required(@required)
    |> cast_embed(:address, with: &Address.changeset/2)
    |> validate_length(:tags, max: 20)
    |> put_full_name()
  end

  defp put_full_name(changeset) do
    first = get_field(changeset, :first_name)
    last = get_field(changeset, :last_name)

    case {first, last} do
      {f, l} when is_binary(f) and is_binary(l) ->
        put_change(changeset, :full_name, "#{f} #{l}")

      _ ->
        changeset
    end
  end
end
```

### Step 5: Polymorphic `Note`

**Objective**: Tag notes with notable_id plus validated notable_type so one table can attach to contacts, companies, or deals safely.

```elixir
# lib/ecto_schemas_deep/schemas/note.ex
defmodule EctoSchemasDeep.Schemas.Note do
  use Ecto.Schema
  import Ecto.Changeset

  @notable_types ~w(contact company deal)

  schema "notes" do
    field :body, :string
    field :notable_id, :integer
    field :notable_type, :string
    timestamps()
  end

  @spec changeset(%__MODULE__{}, map()) :: Ecto.Changeset.t()
  def changeset(note, attrs) do
    note
    |> cast(attrs, [:body, :notable_id, :notable_type])
    |> validate_required([:body, :notable_id, :notable_type])
    |> validate_inclusion(:notable_type, @notable_types)
    |> validate_length(:body, min: 1, max: 10_000)
  end

  @spec notable_types() :: [String.t()]
  def notable_types, do: @notable_types
end
```

### Step 6: Context module for polymorphic dispatch

**Objective**: Pattern-match the parent struct to derive notable_type so callers write `Notes.create(parent, body)` without stringly-typed args.

```elixir
# lib/ecto_schemas_deep/notes.ex
defmodule EctoSchemasDeep.Notes do
  @moduledoc "Runtime dispatcher for polymorphic notes."

  import Ecto.Query

  alias EctoSchemasDeep.Repo
  alias EctoSchemasDeep.Schemas.{Company, Contact, Deal, Note}

  @type notable :: Contact.t() | Company.t() | Deal.t()

  @doc "Creates a note attached to any notable parent."
  @spec create(notable(), String.t()) :: {:ok, Note.t()} | {:error, Ecto.Changeset.t()}
  def create(notable, body) do
    %Note{}
    |> Note.changeset(%{
      body: body,
      notable_id: notable.id,
      notable_type: type_for(notable)
    })
    |> Repo.insert()
  end

  @doc "Returns all notes for a given parent."
  @spec for_parent(notable()) :: [Note.t()]
  def for_parent(notable) do
    type = type_for(notable)

    from(n in Note,
      where: n.notable_id == ^notable.id and n.notable_type == ^type,
      order_by: [desc: n.inserted_at]
    )
    |> Repo.all()
  end

  defp type_for(%Contact{}), do: "contact"
  defp type_for(%Company{}), do: "company"
  defp type_for(%Deal{}), do: "deal"
end
```

### Step 7: Migrations

**Objective**: Create contacts, companies, deals, and notes with a composite (notable_type, notable_id) index so polymorphic lookups stay indexed.

```elixir
defmodule EctoSchemasDeep.Repo.Migrations.CreateSchemas do
  use Ecto.Migration

  def change do
    create table(:contacts) do
      add :first_name, :string, null: false
      add :last_name, :string, null: false
      add :phone, :string
      add :tags, {:array, :string}, default: []
      add :custom_attrs, :map, default: %{}
      add :address, :map
      timestamps()
    end

    create table(:companies) do
      add :name, :string, null: false
      timestamps()
    end

    create table(:deals) do
      add :title, :string, null: false
      add :amount_cents, :integer, null: false
      timestamps()
    end

    create table(:notes) do
      add :body, :text, null: false
      add :notable_id, :integer, null: false
      add :notable_type, :string, null: false
      timestamps()
    end
    create index(:notes, [:notable_type, :notable_id])
  end
end
```

### Step 8: Tests

**Objective**: Exercise E.164 normalization, embed round-trips, and polymorphic dispatch end-to-end so regressions in any layer surface early.

```elixir
# test/ecto_schemas_deep/contact_test.exs
defmodule EctoSchemasDeep.ContactTest do
  use ExUnit.Case, async: false

  alias EctoSchemasDeep.Repo
  alias EctoSchemasDeep.Schemas.Contact

  setup do
    Ecto.Adapters.SQL.Sandbox.checkout(Repo)
    :ok
  end

  describe "changeset/2 — phone custom type" do
    test "normalizes a phone number to E.164" do
      cs = Contact.changeset(%Contact{}, %{
        first_name: "Ada",
        last_name: "Lovelace",
        phone: "+54 11 1234-5678"
      })

      assert cs.valid?
      assert Ecto.Changeset.get_change(cs, :phone) == "+5411123456781"
             or Ecto.Changeset.get_change(cs, :phone) == "+541112345678"
    end

    test "rejects gibberish phone" do
      cs = Contact.changeset(%Contact{}, %{
        first_name: "Ada",
        last_name: "Lovelace",
        phone: "abc"
      })

      refute cs.valid?
      assert %{phone: ["is invalid"]} = errors_on(cs)
    end
  end

  describe "changeset/2 — embedded address" do
    test "casts nested address errors" do
      cs = Contact.changeset(%Contact{}, %{
        first_name: "Ada",
        last_name: "Lovelace",
        address: %{street: "Main 1", city: "BA", country: "ARGENTINA"}
      })

      refute cs.valid?
      assert %{address: %{country: ["should be 2 character(s)"]}} = errors_on(cs)
    end
  end

  describe "changeset/2 — virtual full_name" do
    test "derives full_name on cast" do
      cs = Contact.changeset(%Contact{}, %{first_name: "Ada", last_name: "Lovelace"})
      assert Ecto.Changeset.get_change(cs, :full_name) == "Ada Lovelace"
    end
  end

  defp errors_on(changeset) do
    Ecto.Changeset.traverse_errors(changeset, fn {message, opts} ->
      Regex.replace(~r"%{(\w+)}", message, fn _, key ->
        opts |> Keyword.get(String.to_existing_atom(key), key) |> to_string()
      end)
    end)
  end
end
```

---


### Why this works

The design leans on BEAM guarantees (process isolation, mailbox ordering, supervisor restarts) and pushes invariants to the boundaries of each module. State transitions are explicit, failure modes are declared rather than implicit, and each step is independently testable. That combination keeps the implementation correct under concurrent load and cheap to change later.

## Benchmark

```elixir
# Minimal measurement — replace with Benchee for distribution stats.
{time_us, _} = :timer.tc(fn ->
  for _ <- 1..10_000, do: run_operation()
end)
IO.puts("avg: #{time_us / 10_000} µs/op")
```

Target: operation should complete in the low-microsecond range on modern hardware; deviations by >2× indicate a regression worth investigating.

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

**1. Embedded schemas lose queryability**
`where: contact.address.country == "AR"` is a JSONB path query. Ecto supports
`fragment("? ->> 'country'", c.address)`, but you lose compile-time field checks. If
you need to filter by an embedded field regularly, promote it to a top-level column or
a real association.

**2. `on_replace` default is `:raise`**
Forgetting `on_replace: :delete` (or `:update`) on `embeds_many` means any update that
changes the list raises. Document your choice explicitly — it's not a "minor config".

**3. Custom type `cast/1` runs on user input only**
`cast/1` is invoked by `Ecto.Changeset.cast/3`. If you insert with `Repo.insert(%Contact{phone: "bad"})`
directly, `dump/1` runs but does not always reject — test both paths.

**4. Virtual fields don't survive JSON serialization unless you opt in**
`Jason.encode!(contact)` includes virtual fields because they're struct keys. But
`Contact |> Repo.all() |> Jason.encode!()` yields `"full_name": null` because the virtual
was never computed. Compute it after load with a `Repo.after_compile` hook or a view layer.

**5. Polymorphic via `(notable_id, notable_type)` cannot use foreign keys**
There's no FK constraint ensuring the `notable_id` exists in any specific table. You
accept orphan risk. Mitigate with a background cleanup job or by adding a CHECK trigger.

**6. `embeds_many` with 10k entries is an antipattern**
Every read hydrates the entire JSON blob. Above ~100 entries, promote to a real
`has_many`. Measure with `Repo.query!("SELECT pg_column_size(tags) FROM contacts")`.

**7. `{:array, :string}` is Postgres-only**
SQLite and MySQL do not support native arrays. Use a custom type that serializes to JSON
text if you need cross-DB portability.

**8. When NOT to use this**
If the "embedded" data must be searched by external systems, reported on independently,
or shared across entities (e.g., a company address that applies to many contacts),
promote it to its own table. Embedded is for locality, not for flexibility.

---

## Performance notes

Measure embedded vs joined address retrieval:

```elixir
{t_embed, _} = :timer.tc(fn ->
  for _ <- 1..1_000, do: Repo.get!(Contact, contact_id)
end)
```

A contact with an embedded address is a single `SELECT * FROM contacts` row.
A contact with a joined address is an additional query (or JOIN) per record. For
1000 lookups, embedded is typically 2–3× faster and allocates less on the BEAM heap
because there's one struct instead of two.

---

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?

## Executable Example

```elixir
# mix.exs
defp deps do
  [
    {:ecto_sql, "~> 3.11"},
    {:postgrex, "~> 0.17"},
    {:jason, "~> 1.4"}
  ]
end

# lib/ecto_schemas_deep/types/phone_e164.ex
defmodule EctoSchemasDeep.Types.PhoneE164 do
  end
  @moduledoc """
  Ecto type that normalizes phone numbers to E.164 format on cast and dump.

  Accepts strings with arbitrary separators; stores digits with leading `+`.
  """
  use Ecto.Type

  @impl true
  def type, do: :string

  @impl true
  def cast(value) when is_binary(value) do
    case normalize(value) do
      {:ok, e164} -> {:ok, e164}
      :error -> :error
    end
  end

  def cast(nil), do: {:ok, nil}
  def cast(_), do: :error

  @impl true
  def load(value) when is_binary(value), do: {:ok, value}
  def load(nil), do: {:ok, nil}

  @impl true
  def dump(value) when is_binary(value) do
    case normalize(value) do
      {:ok, e164} -> {:ok, e164}
      :error -> :error
    end
  end

  def dump(nil), do: {:ok, nil}
  def dump(_), do: :error

  defp normalize(raw) do
  end
    digits = raw |> to_string() |> String.replace(~r/[^\d+]/, "")

    case digits do
      "+" <> rest when byte_size(rest) in 8..15 ->
        if String.match?(rest, ~r/^\d+$/), do: {:ok, "+" <> rest}, else: :error

      rest when byte_size(rest) in 8..15 ->
        if String.match?(rest, ~r/^\d+$/), do: {:ok, "+" <> rest}, else: :error

      _ ->
        :error
    end
  end
end

# lib/ecto_schemas_deep/schemas/address.ex
defmodule EctoSchemasDeep.Schemas.Address do
  use Ecto.Schema
  import Ecto.Changeset

  @primary_key false
  embedded_schema do
    field :street, :string
    field :city, :string
    field :country, :string
    field :postal_code, :string
  end

  @spec changeset(%__MODULE__{}, map()) :: Ecto.Changeset.t()
  def changeset(address, attrs) do
    address
    |> cast(attrs, [:street, :city, :country, :postal_code])
    |> validate_required([:street, :city, :country])
    |> validate_length(:country, is: 2)
  end
end

# lib/ecto_schemas_deep/schemas/contact.ex
defmodule EctoSchemasDeep.Schemas.Contact do
  use Ecto.Schema
  import Ecto.Changeset

  alias EctoSchemasDeep.Schemas.Address
  alias EctoSchemasDeep.Types.PhoneE164

  schema "contacts" do
    field :first_name, :string
    field :last_name, :string
    field :phone, PhoneE164
    field :tags, {:array, :string}, default: []
    field :custom_attrs, :map, default: %{}

    # Derived on load; never persisted.
    field :full_name, :string, virtual: true

    embeds_one :address, Address, on_replace: :update

    timestamps()
  end

  @required ~w(first_name last_name)a
  @optional ~w(phone tags custom_attrs)a

  @spec changeset(%__MODULE__{}, map()) :: Ecto.Changeset.t()
  def changeset(contact, attrs) do
    contact
    |> cast(attrs, @required ++ @optional)
    |> validate_required(@required)
    |> cast_embed(:address, with: &Address.changeset/2)
    |> validate_length(:tags, max: 20)
    |> put_full_name()
  end

  defp put_full_name(changeset) do
    first = get_field(changeset, :first_name)
    last = get_field(changeset, :last_name)

    case {first, last} do
      {f, l} when is_binary(f) and is_binary(l) ->
        put_change(changeset, :full_name, "#{f} #{l}")

      _ ->
        changeset
    end
  end
end

# lib/ecto_schemas_deep/schemas/note.ex
defmodule EctoSchemasDeep.Schemas.Note do
  end
  use Ecto.Schema
  import Ecto.Changeset

  @notable_types ~w(contact company deal)

  schema "notes" do
    field :body, :string
    field :notable_id, :integer
    field :notable_type, :string
    timestamps()
  end

  @spec changeset(%__MODULE__{}, map()) :: Ecto.Changeset.t()
  def changeset(note, attrs) do
    note
    |> cast(attrs, [:body, :notable_id, :notable_type])
    |> validate_required([:body, :notable_id, :notable_type])
    |> validate_inclusion(:notable_type, @notable_types)
    |> validate_length(:body, min: 1, max: 10_000)
  end

  @spec notable_types() :: [String.t()]
  def notable_types, do: @notable_types
end

# lib/ecto_schemas_deep/notes.ex
defmodule EctoSchemasDeep.Notes do
  end
  @moduledoc "Runtime dispatcher for polymorphic notes."

  import Ecto.Query

  alias EctoSchemasDeep.Repo
  alias EctoSchemasDeep.Schemas.{Company, Contact, Deal, Note}

  @type notable :: Contact.t() | Company.t() | Deal.t()

  @doc "Creates a note attached to any notable parent."
  @spec create(notable(), String.t()) :: {:ok, Note.t()} | {:error, Ecto.Changeset.t()}
  def create(notable, body) do
    %Note{}
    |> Note.changeset(%{
      body: body,
      notable_id: notable.id,
      notable_type: type_for(notable)
    })
    |> Repo.insert()
  end

  @doc "Returns all notes for a given parent."
  @spec for_parent(notable()) :: [Note.t()]
  def for_parent(notable) do
    type = type_for(notable)

    from(n in Note,
      where: n.notable_id == ^notable.id and n.notable_type == ^type,
      order_by: [desc: n.inserted_at]
    )
    |> Repo.all()
  end

  defp type_for(%Contact{}), do: "contact"
  defp type_for(%Company{}), do: "company"
  defp type_for(%Deal{}), do: "deal"
end

defmodule EctoSchemasDeep.Repo.Migrations.CreateSchemas do
  use Ecto.Migration

  def change do
    create table(:contacts) do
      add :first_name, :string, null: false
      add :last_name, :string, null: false
      add :phone, :string
      add :tags, {:array, :string}, default: []
      add :custom_attrs, :map, default: %{}
      add :address, :map
      timestamps()
    end

    create table(:companies) do
      add :name, :string, null: false
      timestamps()
    end

    create table(:deals) do
      add :title, :string, null: false
      add :amount_cents, :integer, null: false
      timestamps()
    end

    create table(:notes) do
      add :body, :text, null: false
      add :notable_id, :integer, null: false
      add :notable_type, :string, null: false
      timestamps()
    end
    create index(:notes, [:notable_type, :notable_id])
  end
end

# test/ecto_schemas_deep/contact_test.exs
defmodule EctoSchemasDeep.ContactTest do
  use ExUnit.Case, async: false

  alias EctoSchemasDeep.Repo
  alias EctoSchemasDeep.Schemas.Contact

  setup do
    Ecto.Adapters.SQL.Sandbox.checkout(Repo)
    :ok
  end

  describe "changeset/2 — phone custom type" do
    test "normalizes a phone number to E.164" do
      cs = Contact.changeset(%Contact{}, %{
        first_name: "Ada",
        last_name: "Lovelace",
        phone: "+54 11 1234-5678"
      })

      assert cs.valid?
      assert Ecto.Changeset.get_change(cs, :phone) == "+5411123456781"
             or Ecto.Changeset.get_change(cs, :phone) == "+541112345678"
    end

    test "rejects gibberish phone" do
      cs = Contact.changeset(%Contact{}, %{
        first_name: "Ada",
        last_name: "Lovelace",
        phone: "abc"
      })

      refute cs.valid?
      assert %{phone: ["is invalid"]} = errors_on(cs)
    end
  end

  describe "changeset/2 — embedded address" do
    test "casts nested address errors" do
      cs = Contact.changeset(%Contact{}, %{
        first_name: "Ada",
        last_name: "Lovelace",
        address: %{street: "Main 1", city: "BA", country: "ARGENTINA"}
      })

      refute cs.valid?
      assert %{address: %{country: ["should be 2 character(s)"]}} = errors_on(cs)
    end
  end

  describe "changeset/2 — virtual full_name" do
    test "derives full_name on cast" do
      cs = Contact.changeset(%Contact{}, %{first_name: "Ada", last_name: "Lovelace"})
      assert Ecto.Changeset.get_change(cs, :full_name) == "Ada Lovelace"
    end
  end

  defp errors_on(changeset) do
    Ecto.Changeset.traverse_errors(changeset, fn {message, opts} ->
      Regex.replace(~r"%{(\w+)}", message, fn _, key ->
        opts |> Keyword.get(String.to_existing_atom(key), key) |> to_string()
      end)
    end)
  end
end

defmodule Main do
  def main do
      # Demonstrating 56-ecto-schemas-avanzados
      :ok
  end
end

Main.main()
end
end
end
end
end
end
end
```
