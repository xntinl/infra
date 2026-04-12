# Embedded Schemas and Nested Changesets

**Project**: `form_builder` — multi-step form with nested embeds validated as one unit.

---

## Project context

A customer onboarding form has nested structure: a `Customer` with one `BillingAddress`
and many `ContactMethods`. These sub-objects are not independent tables — they exist only
inside a customer row, stored in a JSONB column. Ecto models this with `embeds_one` and
`embeds_many`, validated through nested changesets.

Embedded schemas are also the idiomatic way to validate structured input in Phoenix
controllers without a DB table — changeset-driven validation of JSON payloads.

```
form_builder/
├── lib/
│   └── form_builder/
│       ├── application.ex
│       ├── repo.ex
│       ├── customers.ex
│       └── schemas/
│           ├── customer.ex
│           ├── billing_address.ex      # embedded
│           └── contact_method.ex       # embedded (many)
├── priv/repo/migrations/
├── test/form_builder/
│   └── customers_test.exs
├── bench/customers_bench.exs
└── mix.exs
```

---

## Core concepts

### 1. `embeds_one` and `embeds_many`

```elixir
schema "customers" do
  field :name, :string
  embeds_one :billing_address, BillingAddress, on_replace: :update
  embeds_many :contact_methods, ContactMethod, on_replace: :delete
end
```

The DB column is JSONB (for Postgres). Ecto encodes the struct to JSON on write, decodes
on read. No join, no separate table.

### 2. `on_replace:` behavior

When an incoming changeset has a new embed, Ecto must decide what to do with the old one:

- `:raise` — default; refuses, forcing explicit handling.
- `:mark_as_invalid` — treats replacement as an error.
- `:update` — merges; good for `embeds_one` when partial updates are expected.
- `:delete` — removes; standard for `embeds_many` where a rebuild replaces the list.

### 3. `cast_embed/3` in the parent changeset

```elixir
customer
|> cast(attrs, [:name])
|> cast_embed(:billing_address, required: true)
|> cast_embed(:contact_methods)
```

This calls the embed's own changeset function (`BillingAddress.changeset/2`) and attaches
it. Errors on the embed surface on the parent changeset under the embed's key.

### 4. JSONB storage

```sql
billing_address JSONB,
contact_methods JSONB DEFAULT '[]'
```

Postgres stores the serialized struct. You can query into it with `->` and `->>`:

```elixir
from c in Customer, where: fragment("?->'country'", c.billing_address) == ^"US"
```

Not as convenient as a column, but adequate for rarely-queried fields.

### 5. Why embeds and not `has_one`/`has_many`

| Scenario | Prefer |
|----------|--------|
| Sub-objects never exist outside the parent | embed |
| Sub-objects are queried independently | has_one/has_many |
| Sub-objects have their own identity (IDs, timestamps) | has_one/has_many |
| Whole-object replacement on update | embed |
| Partial updates to sub-objects by ID | has_one/has_many |

---

## Design decisions

- **Option A — separate tables with FKs**: relational, indexable.
  Pros: easy to query. Cons: more joins; sub-objects are atomic-to-parent but DB does
  not know that.
- **Option B — embeds in JSONB**: sub-objects are nested data.
  Pros: atomic read/write with parent, no joins, less migrations. Cons: hard to query
  by sub-field, no referential integrity.

We use **Option B** — this data is always read with its parent, never independently.

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

### Step 1: Migration

```elixir
# priv/repo/migrations/20260101000000_create_customers.exs
defmodule FormBuilder.Repo.Migrations.CreateCustomers do
  use Ecto.Migration

  def change do
    create table(:customers) do
      add :name, :string, null: false
      add :billing_address, :map
      add :contact_methods, :map, default: %{}, null: false
      timestamps()
    end
  end
end
```

### Step 2: Embedded schemas

```elixir
# lib/form_builder/schemas/billing_address.ex
defmodule FormBuilder.Schemas.BillingAddress do
  use Ecto.Schema
  import Ecto.Changeset

  @primary_key false
  embedded_schema do
    field :street, :string
    field :city, :string
    field :postal_code, :string
    field :country, :string
  end

  def changeset(addr, attrs) do
    addr
    |> cast(attrs, [:street, :city, :postal_code, :country])
    |> validate_required([:street, :city, :country])
    |> validate_length(:country, is: 2)
    |> validate_format(:country, ~r/^[A-Z]{2}$/, message: "must be ISO 3166-1 alpha-2")
  end
end

# lib/form_builder/schemas/contact_method.ex
defmodule FormBuilder.Schemas.ContactMethod do
  use Ecto.Schema
  import Ecto.Changeset

  @types ~w(email phone sms)

  @primary_key false
  embedded_schema do
    field :kind, :string
    field :value, :string
    field :primary, :boolean, default: false
  end

  def changeset(method, attrs) do
    method
    |> cast(attrs, [:kind, :value, :primary])
    |> validate_required([:kind, :value])
    |> validate_inclusion(:kind, @types)
    |> validate_by_kind()
  end

  defp validate_by_kind(changeset) do
    case get_field(changeset, :kind) do
      "email" -> validate_format(changeset, :value, ~r/@/)
      "phone" -> validate_format(changeset, :value, ~r/^\+?\d{7,15}$/)
      "sms" -> validate_format(changeset, :value, ~r/^\+?\d{7,15}$/)
      _ -> changeset
    end
  end
end
```

### Step 3: Parent schema

```elixir
# lib/form_builder/schemas/customer.ex
defmodule FormBuilder.Schemas.Customer do
  use Ecto.Schema
  import Ecto.Changeset

  alias FormBuilder.Schemas.{BillingAddress, ContactMethod}

  schema "customers" do
    field :name, :string
    embeds_one :billing_address, BillingAddress, on_replace: :update
    embeds_many :contact_methods, ContactMethod, on_replace: :delete
    timestamps()
  end

  def changeset(customer, attrs) do
    customer
    |> cast(attrs, [:name])
    |> validate_required([:name])
    |> cast_embed(:billing_address, required: true)
    |> cast_embed(:contact_methods,
      with: &ContactMethod.changeset/2,
      required: true
    )
    |> validate_at_least_one_contact()
    |> validate_exactly_one_primary_contact()
  end

  defp validate_at_least_one_contact(changeset) do
    case get_field(changeset, :contact_methods, []) do
      [] -> add_error(changeset, :contact_methods, "must have at least one contact method")
      _ -> changeset
    end
  end

  defp validate_exactly_one_primary_contact(changeset) do
    methods = get_field(changeset, :contact_methods, [])
    primary_count = Enum.count(methods, & &1.primary)

    case primary_count do
      1 -> changeset
      0 -> add_error(changeset, :contact_methods, "exactly one contact method must be primary")
      _ -> add_error(changeset, :contact_methods, "only one contact method can be primary")
    end
  end
end
```

### Step 4: Context

```elixir
# lib/form_builder/customers.ex
defmodule FormBuilder.Customers do
  import Ecto.Query
  alias FormBuilder.Repo
  alias FormBuilder.Schemas.Customer

  def create(attrs) do
    %Customer{}
    |> Customer.changeset(attrs)
    |> Repo.insert()
  end

  def update(%Customer{} = c, attrs) do
    c
    |> Customer.changeset(attrs)
    |> Repo.update()
  end

  def get(id), do: Repo.get(Customer, id)

  @doc """
  Query customers located in a given country — demonstrates JSONB navigation.
  """
  def by_country(country_code) do
    from(c in Customer,
      where: fragment("?->>'country' = ?", c.billing_address, ^country_code)
    )
    |> Repo.all()
  end
end
```

---

## Why this works

- `cast_embed/3` delegates validation to the embed's own changeset function. Errors
  surface nested: `changeset.errors` has `[billing_address: ..., contact_methods: ...]`
  and the embed's changesets are accessible via `changeset.changes.billing_address`.
- `on_replace: :update` on `billing_address` means `cast_embed` merges partial updates —
  if the input has only `city`, `street` from the existing value is retained.
- `on_replace: :delete` on `contact_methods` means the incoming list replaces the old one
  entirely. That is the right semantic for a list where membership is expressed by
  position, not identity.
- The `validate_exactly_one_primary_contact/1` combines data across embeds — a logic
  that could not live on either embed alone.
- The JSONB column is queryable with `->` and `->>`; not as fast as a regular column,
  but good enough for the "customers in country X" lookup.

---

## Data flow

```
HTTP params:
  %{
    "name" => "Acme",
    "billing_address" => %{"street" => ..., "country" => "US"},
    "contact_methods" => [
      %{"kind" => "email", "value" => "a@b.com", "primary" => true}
    ]
  }
      │
      ▼
Customer.changeset(%Customer{}, params)
      │  cast :name
      │  cast_embed :billing_address ─▶ BillingAddress.changeset (nested errors bubble up)
      │  cast_embed :contact_methods  ─▶ list of ContactMethod.changeset
      │  validate_at_least_one_contact
      │  validate_exactly_one_primary_contact
      │
      ▼
Repo.insert
      │
      ▼
INSERT INTO customers (name, billing_address, contact_methods) VALUES
  ('Acme', '{...}', '[{...}]')
```

---

## Tests

```elixir
# test/form_builder/customers_test.exs
defmodule FormBuilder.CustomersTest do
  use ExUnit.Case, async: false
  alias FormBuilder.{Customers, Repo}
  alias FormBuilder.Schemas.Customer

  setup do
    Ecto.Adapters.SQL.Sandbox.checkout(Repo)
    Ecto.Adapters.SQL.Sandbox.mode(Repo, {:shared, self()})
    Repo.delete_all(Customer)
    :ok
  end

  defp valid_attrs do
    %{
      "name" => "Acme",
      "billing_address" => %{
        "street" => "1 Main",
        "city" => "SF",
        "postal_code" => "94111",
        "country" => "US"
      },
      "contact_methods" => [
        %{"kind" => "email", "value" => "a@b.com", "primary" => true}
      ]
    }
  end

  describe "create/1 happy path" do
    test "inserts customer with embeds" do
      assert {:ok, c} = Customers.create(valid_attrs())
      assert c.billing_address.country == "US"
      assert [%{kind: "email"}] = c.contact_methods
    end
  end

  describe "nested validation" do
    test "surfaces nested errors from billing_address" do
      attrs = put_in(valid_attrs(), ["billing_address", "country"], "USA")

      {:error, cs} = Customers.create(attrs)
      assert cs.changes.billing_address.errors != []
    end

    test "requires at least one contact method" do
      attrs = Map.put(valid_attrs(), "contact_methods", [])
      {:error, cs} = Customers.create(attrs)
      assert [contact_methods: {"must have at least one contact method", _}] = cs.errors
    end

    test "rejects multiple primary contacts" do
      attrs =
        put_in(valid_attrs(), ["contact_methods"], [
          %{"kind" => "email", "value" => "a@b.com", "primary" => true},
          %{"kind" => "phone", "value" => "+123456789", "primary" => true}
        ])

      {:error, cs} = Customers.create(attrs)
      assert [contact_methods: {"only one contact method can be primary", _}] = cs.errors
    end

    test "rejects invalid email format in contact method" do
      attrs =
        put_in(valid_attrs(), ["contact_methods"], [
          %{"kind" => "email", "value" => "not-an-email", "primary" => true}
        ])

      {:error, cs} = Customers.create(attrs)
      assert cs.valid? == false
    end
  end

  describe "update with on_replace: :update" do
    test "partial update to billing_address keeps untouched fields" do
      {:ok, c} = Customers.create(valid_attrs())

      {:ok, updated} =
        Customers.update(c, %{"billing_address" => %{"city" => "NYC"}})

      assert updated.billing_address.city == "NYC"
      assert updated.billing_address.street == "1 Main"
    end
  end

  describe "update with on_replace: :delete" do
    test "replacing contact_methods deletes the old ones" do
      {:ok, c} = Customers.create(valid_attrs())

      {:ok, updated} =
        Customers.update(c, %{
          "contact_methods" => [
            %{"kind" => "phone", "value" => "+19998887777", "primary" => true}
          ]
        })

      assert [%{kind: "phone"}] = updated.contact_methods
    end
  end

  describe "JSONB queries" do
    test "by_country/1 filters via JSONB path" do
      {:ok, _} = Customers.create(valid_attrs())

      attrs = put_in(valid_attrs(), ["billing_address", "country"], "DE")
      {:ok, _} = Customers.create(attrs)

      assert [%{billing_address: %{country: "US"}}] = Customers.by_country("US")
    end
  end
end
```

---

## Benchmark

```elixir
# bench/customers_bench.exs
alias FormBuilder.{Customers, Repo}

attrs = %{
  "name" => "Bench",
  "billing_address" => %{"street" => "S", "city" => "C", "country" => "US"},
  "contact_methods" => [%{"kind" => "email", "value" => "a@b.com", "primary" => true}]
}

Benchee.run(
  %{
    "create"        => fn -> Customers.create(attrs) end,
    "validate only" => fn ->
      FormBuilder.Schemas.Customer.changeset(%FormBuilder.Schemas.Customer{}, attrs)
    end
  },
  time: 3, warmup: 1,
  before_scenario: fn input -> Repo.delete_all(FormBuilder.Schemas.Customer); input end
)
```

**Target**: `validate only` under 200 µs (pure changeset, no DB). `create` under 5 ms
on local Postgres.

---

## Trade-offs and production gotchas

**1. JSONB cannot be indexed as cheaply as a column.** Queries like "country = US" need
an expression index: `CREATE INDEX ... ON customers ((billing_address->>'country'))`.

**2. Schema changes to embeds require manual migration of existing rows.** Adding a
required field to `BillingAddress` and deploying breaks reads of old data where the
field is missing. Either keep embeds additive-only or run a backfill job.

**3. `on_replace: :raise` is the default and footgun.** Forgetting to set it means a
valid-looking update crashes at changeset time. Set it explicitly on every embed.

**4. `cast_embed(..., required: true)` does not mean "at least one in embeds_many".**
It only validates that the key is present. The "at least one" check is an extra
validator you write manually.

**5. Nested error formatting is opaque.** `changeset.errors` lists top-level errors;
embed errors live under `changeset.changes.billing_address.errors`. Any error
serializer must traverse the tree.

**6. When NOT to use embeds.** If you need to query or mutate sub-objects independently
(e.g., "show all contact methods created this week across customers"), use a separate
table. Embeds are for data whose lifecycle is identical to the parent.

---

## Reflection

Your product pivoted: `BillingAddress` is now shared across customers (same company,
multiple contacts). You need to extract it from the JSONB embed into its own table with
FKs. Sketch the migration: how do you deduplicate existing embedded addresses into rows,
replace the JSONB with a foreign key, and keep the API stable for clients that still
send `billing_address` in the request body? What is the cutover phase where both writers
and readers must handle both shapes?

---

## Resources

- [`Ecto.Schema.embedded_schema/2`](https://hexdocs.pm/ecto/Ecto.Schema.html#embedded_schema/2)
- [`Ecto.Changeset.cast_embed/3`](https://hexdocs.pm/ecto/Ecto.Changeset.html#cast_embed/3)
- [Dashbit — "Working with JSON and embeds in Ecto"](https://dashbit.co/blog)
- [Postgres JSONB operators](https://www.postgresql.org/docs/current/functions-json.html)
