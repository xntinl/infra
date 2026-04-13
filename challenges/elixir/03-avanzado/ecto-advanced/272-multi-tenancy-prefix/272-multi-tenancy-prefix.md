# Multi-Tenancy with Postgres Schemas and Ecto `prefix`

**Project**: `tenant_billing` — per-tenant data isolation using Postgres schemas and `Ecto.Repo` `prefix` option.

---

## Project context

You run a B2B SaaS where every customer (tenant) is a separate company. Invoices, subscriptions,
and ledger entries are legally segregated: a data leak across tenants is a GDPR incident. You
must design the data model so that cross-tenant queries are structurally impossible, not merely
prevented by a `WHERE tenant_id = ?` clause that a junior developer might forget.

Three strategies dominate production systems:

1. **Row-level tenancy** — single shared table with `tenant_id` column. Cheapest to operate,
   riskiest to misuse (any missed `WHERE` clause leaks rows).
2. **Schema-per-tenant** — one Postgres schema per tenant, tables replicated per schema.
   Strong isolation, slightly more migration overhead.
3. **Database-per-tenant** — one Postgres database per tenant. Maximum isolation, expensive
   to operate past ~100 tenants.

This project implements strategy (2) with Ecto's `prefix` option, which rewrites every SQL
statement to target `"<schema>"."<table>"`.

```
tenant_billing/
├── lib/
│   └── tenant_billing/
│       ├── application.ex
│       ├── repo.ex
│       ├── tenants.ex                # tenant lifecycle (create schema, run migrations)
│       ├── invoices.ex               # context with prefix-aware functions
│       └── schemas/
│           ├── invoice.ex
│           └── line_item.ex
├── priv/
│   └── repo/
│       ├── migrations/               # shared (public) schema migrations
│       └── tenant_migrations/        # per-tenant schema migrations
├── test/
│   └── tenant_billing/
│       └── invoices_test.exs
├── bench/
│   └── prefix_bench.exs
└── mix.exs
```

---

## Why schema-per-tenant and not row-level

Row-level tenancy looks simple until an engineer writes:

```elixir
Repo.all(from i in Invoice, where: i.status == "overdue")
```

and forgets `where: i.tenant_id == ^current_tenant`. The query returns every tenant's overdue
invoices. Postgres row-level security (RLS) mitigates this, but it leaks through `EXPLAIN`
plans, requires careful `SET LOCAL`, and becomes brittle when you mix superuser migrations
with app-level queries.

Schema-per-tenant moves isolation into the SQL namespace itself. A query without a prefix
hits the `public` schema — which contains no tenant data — and returns zero rows. Misuse
produces empty results, not leaks.

---

## Why Ecto `prefix` and not dynamic repos

`Ecto.Repo` supports two orthogonal mechanisms:

- **Dynamic repos** — one Repo process per tenant. Connections are not shared, pool size
  multiplies per tenant. Does not scale past ~50 tenants on a single node.
- **Prefix option** — a single Repo/pool, each query tagged with `prefix: "tenant_42"`.
  Postgres rewrites the statement. Connection pool is shared.

For tens of thousands of tenants, `prefix` is the only viable choice.

---

## Core concepts

### 1. Schema qualification in Postgres

Postgres resolves unqualified table names through `search_path`. With `search_path = public`,
`SELECT * FROM invoices` reads from `public.invoices`. Setting the prefix to `"tenant_42"`
in Ecto produces `SELECT * FROM "tenant_42"."invoices"`. The table in `public` can be absent
without errors.

### 2. Ecto passes `prefix` down the query AST

```elixir
Repo.all(query, prefix: "tenant_42")
```

flows through every join, preload, subquery, and association. Belongs-to preloads in a
different schema require an explicit prefix on the association.

### 3. Migrations run per prefix

Tenant schemas need their own migration runs:

```elixir
Ecto.Migrator.run(Repo, "priv/repo/tenant_migrations", :up,
  all: true, prefix: "tenant_42"
)
```

The `schema_migrations` table is also created inside the tenant schema, so versions are
tracked per tenant.

---

## Design decisions

- **Option A — prefix per query**: pass `prefix: tenant` to every `Repo` call.
  Pros: explicit, no hidden state. Cons: easy to forget, litters the codebase.
- **Option B — prefix via process dictionary**: set `Repo.put_dynamic_prefix/1` in a Plug.
  Pros: request-wide tenancy without per-call boilerplate. Cons: hidden state, hard to test.

We use **Option A wrapped in a context function**: `Invoices.list(tenant, opts)` sets the
prefix internally so callers never handle it. This keeps the explicit SQL contract without
per-call noise at call sites.

---

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule TenantBilling.MixProject do
  use Mix.Project

  def project do
    [
      app: :tenant_billing,
      version: "0.1.0",
      elixir: "~> 1.17",
      deps: deps(),
      aliases: aliases()
    ]
  end

  def application do
    [
      mod: {TenantBilling.Application, []},
      extra_applications: [:logger]
    ]
  end

  defp deps do
    [
      {:ecto_sql, "~> 3.12"},
      {:postgrex, "~> 0.19"},
      {:benchee, "~> 1.3", only: :dev}
    ]
  end

  defp aliases do
    [
      "ecto.setup": ["ecto.create", "ecto.migrate"],
      test: ["ecto.create --quiet", "ecto.migrate --quiet", "test"]
    ]
  end
end
```

### Step 1: Repo and application

**Objective**: Configure single Repo per app: tenants switch via SQL prefix on shared connection pool, not separate Repo processes.

```elixir
# lib/tenant_billing/repo.ex
defmodule TenantBilling.Repo do
  use Ecto.Repo,
    otp_app: :tenant_billing,
    adapter: Ecto.Adapters.Postgres
end

# lib/tenant_billing/application.ex
defmodule TenantBilling.Application do
  use Application

  @impl true
  def start(_type, _args) do
    Supervisor.start_link([TenantBilling.Repo], strategy: :one_for_one)
  end
end
```

### Step 2: Schemas

**Objective**: Define Invoice/LineItem schemas with binary_id so same module binds any Postgres schema prefix without modification.

```elixir
# lib/tenant_billing/schemas/invoice.ex
defmodule TenantBilling.Schemas.Invoice do
  use Ecto.Schema
  import Ecto.Changeset

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id

  schema "invoices" do
    field :number, :string
    field :status, :string, default: "draft"
    field :total_cents, :integer, default: 0

    has_many :line_items, TenantBilling.Schemas.LineItem

    timestamps()
  end

  def changeset(invoice, attrs) do
    invoice
    |> cast(attrs, [:number, :status, :total_cents])
    |> validate_required([:number])
    |> validate_inclusion(:status, ~w(draft issued paid void))
  end
end

# lib/tenant_billing/schemas/line_item.ex
defmodule TenantBilling.Schemas.LineItem do
  use Ecto.Schema
  import Ecto.Changeset

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id

  schema "line_items" do
    field :description, :string
    field :unit_cents, :integer
    field :quantity, :integer, default: 1

    belongs_to :invoice, TenantBilling.Schemas.Invoice

    timestamps()
  end

  def changeset(item, attrs) do
    item
    |> cast(attrs, [:description, :unit_cents, :quantity, :invoice_id])
    |> validate_required([:description, :unit_cents, :invoice_id])
    |> validate_number(:quantity, greater_than: 0)
  end
end
```

### Step 3: Tenant lifecycle

**Objective**: Implement create/1 and drop/1 with regex-validated identifiers to prevent SQL injection on Postgres schema names.

```elixir
# lib/tenant_billing/tenants.ex
defmodule TenantBilling.Tenants do
  @moduledoc """
  Creates and drops per-tenant Postgres schemas. Runs tenant migrations.
  """

  alias TenantBilling.Repo

  @tenant_migrations_path "priv/repo/tenant_migrations"

  @doc "Creates a new tenant schema and runs all migrations inside it."
  @spec create(String.t()) :: :ok | {:error, term()}
  def create(tenant) when is_binary(tenant) do
    with :ok <- validate_tenant_name(tenant),
         {:ok, _} <- create_schema(tenant),
         :ok <- migrate(tenant) do
      :ok
    end
  end

  @spec drop(String.t()) :: :ok | {:error, term()}
  def drop(tenant) do
    with :ok <- validate_tenant_name(tenant) do
      Ecto.Adapters.SQL.query(Repo, ~s(DROP SCHEMA "#{tenant}" CASCADE), [])
      |> case do
        {:ok, _} -> :ok
        err -> err
      end
    end
  end

  @spec migrate(String.t()) :: :ok
  def migrate(tenant) do
    Ecto.Migrator.run(Repo, @tenant_migrations_path, :up,
      all: true,
      prefix: tenant,
      dynamic_repo: Repo
    )

    :ok
  end

  # A tenant name becomes a SQL identifier. Never interpolate raw user input.
  defp validate_tenant_name(name) do
    if Regex.match?(~r/\A[a-z][a-z0-9_]{1,62}\z/, name) do
      :ok
    else
      {:error, :invalid_tenant_name}
    end
  end

  defp create_schema(tenant) do
    Ecto.Adapters.SQL.query(Repo, ~s(CREATE SCHEMA IF NOT EXISTS "#{tenant}"), [])
  end
end
```

### Step 4: Tenant migration

**Objective**: Place migrations in tenant_migrations/ so Ecto.Migrator runs them per-prefix; schema_migrations table lives inside tenant schema.

```elixir
# priv/repo/tenant_migrations/20260101000000_create_invoices.exs
defmodule TenantBilling.Repo.TenantMigrations.CreateInvoices do
  use Ecto.Migration

  def change do
    create table(:invoices, primary_key: false) do
      add :id, :binary_id, primary_key: true
      add :number, :string, null: false
      add :status, :string, null: false, default: "draft"
      add :total_cents, :integer, null: false, default: 0
      timestamps()
    end

    create unique_index(:invoices, [:number])

    create table(:line_items, primary_key: false) do
      add :id, :binary_id, primary_key: true
      add :invoice_id, references(:invoices, type: :binary_id, on_delete: :delete_all),
        null: false
      add :description, :string, null: false
      add :unit_cents, :integer, null: false
      add :quantity, :integer, null: false, default: 1
      timestamps()
    end

    create index(:line_items, [:invoice_id])
  end
end
```

### Step 5: Context functions — prefix encapsulated

**Objective**: Wrap Repo calls in Invoices context so :prefix is always set implicitly; cross-tenant queries are structurally impossible.

```elixir
# lib/tenant_billing/invoices.ex
defmodule TenantBilling.Invoices do
  import Ecto.Query

  alias TenantBilling.Repo
  alias TenantBilling.Schemas.{Invoice, LineItem}

  @spec create(String.t(), map()) :: {:ok, Invoice.t()} | {:error, Ecto.Changeset.t()}
  def create(tenant, attrs) do
    %Invoice{}
    |> Invoice.changeset(attrs)
    |> Ecto.Changeset.put_change(:id, Ecto.UUID.generate())
    |> Repo.insert(prefix: tenant)
  end

  @spec add_line_item(String.t(), Invoice.t(), map()) ::
          {:ok, LineItem.t()} | {:error, Ecto.Changeset.t()}
  def add_line_item(tenant, %Invoice{id: invoice_id}, attrs) do
    attrs = Map.put(attrs, :invoice_id, invoice_id)

    %LineItem{}
    |> LineItem.changeset(attrs)
    |> Repo.insert(prefix: tenant)
  end

  @spec list(String.t(), keyword()) :: [Invoice.t()]
  def list(tenant, opts \\ []) do
    status = Keyword.get(opts, :status)

    Invoice
    |> maybe_filter_status(status)
    |> Repo.all(prefix: tenant)
  end

  @spec get_with_items(String.t(), Ecto.UUID.t()) :: Invoice.t() | nil
  def get_with_items(tenant, id) do
    Invoice
    |> where([i], i.id == ^id)
    |> preload(:line_items)
    |> Repo.one(prefix: tenant)
  end

  defp maybe_filter_status(query, nil), do: query
  defp maybe_filter_status(query, status), do: where(query, [i], i.status == ^status)
end
```

---

## Why this works

`Repo.insert/2` and `Repo.all/2` pass `prefix` into the query planner. Every generated SQL
statement qualifies the table name with the schema. Preloads inherit the parent query's
prefix unless overridden, so `preload(:line_items)` stays within the same tenant.

The context layer is the single seam that touches `prefix`. Schemas remain prefix-agnostic;
this keeps them reusable for background jobs that may legitimately need to aggregate across
tenants (reporting pipelines, SOC2 audits).

---

## Data flow

```
HTTP request
    │
    ▼
Plug (extracts tenant from JWT claim)
    │  tenant = "tenant_42"
    ▼
TenantBilling.Invoices.list("tenant_42", status: "overdue")
    │  prefix: "tenant_42" attached to query
    ▼
Ecto.Adapters.Postgres
    │  SELECT * FROM "tenant_42"."invoices" WHERE status = 'overdue'
    ▼
Postgres (schema-scoped lookup)
```

---

## Tests

```elixir
# test/tenant_billing/invoices_test.exs
defmodule TenantBilling.InvoicesTest do
  use ExUnit.Case, async: false

  alias TenantBilling.{Invoices, Repo, Tenants}

  @tenant_a "tenant_acme"
  @tenant_b "tenant_globex"

  setup_all do
    :ok = ensure_tenant(@tenant_a)
    :ok = ensure_tenant(@tenant_b)

    on_exit(fn ->
      Tenants.drop(@tenant_a)
      Tenants.drop(@tenant_b)
    end)

    :ok
  end

  setup do
    truncate(@tenant_a)
    truncate(@tenant_b)
    :ok
  end

  describe "tenant isolation" do
    test "invoice created in tenant A is invisible from tenant B" do
      {:ok, invoice} = Invoices.create(@tenant_a, %{number: "A-001"})
      assert invoice.number == "A-001"

      assert Invoices.list(@tenant_a) |> length() == 1
      assert Invoices.list(@tenant_b) == []
    end

    test "same invoice number allowed in different tenants" do
      assert {:ok, _} = Invoices.create(@tenant_a, %{number: "INV-1"})
      assert {:ok, _} = Invoices.create(@tenant_b, %{number: "INV-1"})
    end
  end

  describe "preload respects prefix" do
    test "line_items load from the same tenant schema" do
      {:ok, invoice} = Invoices.create(@tenant_a, %{number: "A-10"})

      {:ok, _} =
        Invoices.add_line_item(@tenant_a, invoice, %{
          description: "consulting",
          unit_cents: 50_000,
          quantity: 2
        })

      loaded = Invoices.get_with_items(@tenant_a, invoice.id)
      assert length(loaded.line_items) == 1
    end
  end

  describe "validation" do
    test "rejects invalid tenant names to prevent SQL injection" do
      assert {:error, :invalid_tenant_name} = Tenants.create(~s(evil"; DROP SCHEMA public))
    end
  end

  defp ensure_tenant(tenant) do
    Tenants.drop(tenant)
    Tenants.create(tenant)
  end

  defp truncate(tenant) do
    Ecto.Adapters.SQL.query!(
      Repo,
      ~s(TRUNCATE TABLE "#{tenant}"."line_items", "#{tenant}"."invoices" CASCADE),
      []
    )
  end
end
```

---

## Benchmark

Overhead of the prefix machinery should be negligible vs. unprefixed queries.

```elixir
# bench/prefix_bench.exs
alias TenantBilling.{Invoices, Repo}
alias TenantBilling.Schemas.Invoice

TenantBilling.Tenants.create("bench_tenant")

for n <- 1..1_000 do
  {:ok, _} = Invoices.create("bench_tenant", %{number: "B-#{n}"})
end

Benchee.run(
  %{
    "list with prefix" => fn ->
      Invoices.list("bench_tenant")
    end,
    "raw SQL baseline" => fn ->
      Ecto.Adapters.SQL.query!(Repo, ~s(SELECT * FROM "bench_tenant"."invoices"), [])
    end
  },
  time: 5, warmup: 2
)
```

**Target**: `list with prefix` within 20% of the raw SQL baseline. If it is 2× slower, the
bottleneck is preload or changeset allocation, not the prefix mechanism.

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

**1. Migrations must be replayed per tenant.** Adding a column requires iterating across
thousands of schemas. Wrap migrations in a rollout script that processes tenants in batches
with a tracker table (`tenant_schema_versions`) so you can resume after failure.

**2. Foreign keys cannot cross schemas cleanly.** A `line_item.invoice_id` reference stays
inside the tenant schema. If you need a global `users` table, keep it in `public` and
deliberately do not enforce an FK to tenant tables.

**3. Connection pool pressure.** Each `SET search_path` or prefixed query still uses the
shared pool. If one tenant runs a slow report, everyone waits. Consider a separate Repo
with its own pool for analytical workloads.

**4. `pg_dump` and backups.** A schema-per-tenant database can have 50k schemas. `pg_dump`
with no filter dumps everything. Use `--schema=tenant_42` for per-tenant backups and
restores — essential for "export my data" GDPR requests.

**5. Never interpolate tenant names.** The `validate_tenant_name/1` guard is load-bearing;
an attacker-controlled tenant string inside `~s("#{tenant}")` is SQL injection.

**6. When NOT to use schema-per-tenant.** For < 50 tenants, a `tenants` table with
row-level security is simpler. For > 100k tenants, the Postgres system catalog starts to
slow down (`pg_class` grows linearly with tables × tenants). Past that scale, shard across
databases.

---

## Reflection

If you migrated from row-level to schema-per-tenant today, how would you run both models
side by side during the cutover so that a partial failure does not corrupt data in either
representation? Sketch the dual-write phase and the invariant you would check before
switching the read path.

---

## Resources

- [Ecto `prefix` documentation](https://hexdocs.pm/ecto/multi-tenancy-with-query-prefixes.html)
- [Ecto.Migrator — running migrations with prefix](https://hexdocs.pm/ecto_sql/Ecto.Migrator.html)
- [Postgres — schemas and search_path](https://www.postgresql.org/docs/current/ddl-schemas.html)
- [Triplex](https://github.com/ateliware/triplex) — battle-tested schema-per-tenant library, read the source for edge cases

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```
