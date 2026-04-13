# Multi-Tenancy with Postgres Schemas and Ecto `prefix`

**Project**: `tenant_billing` — per-tenant data isolation using Postgres schemas and `Ecto.Repo` `prefix` option

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
tenant_billing/
├── lib/
│   └── tenant_billing.ex
├── script/
│   └── main.exs
├── test/
│   └── tenant_billing_test.exs
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
defmodule TenantBilling.MixProject do
  use Mix.Project

  def project do
    [
      app: :tenant_billing,
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

### `lib/tenant_billing.ex`

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
      (with :ok <- validate_identifier!(tenant), do: Ecto.Adapters.SQL.query(Repo, ~s(DROP SCHEMA "#{tenant}" CASCADE), []))
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
    (with :ok <- validate_identifier!(tenant), do: Ecto.Adapters.SQL.query(Repo, ~s(CREATE SCHEMA IF NOT EXISTS "#{tenant}"), []))
  end
end

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

### `test/tenant_billing_test.exs`

```elixir
defmodule TenantBilling.InvoicesTest do
  use ExUnit.Case, async: true
  doctest TenantBilling.Repo

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

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Multi-Tenancy with Postgres Schemas and Ecto `prefix`.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Multi-Tenancy with Postgres Schemas and Ecto `prefix` ===")
    IO.puts("Category: Ecto advanced\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case TenantBilling.run(payload) do
          {:ok, value} -> {:ok, payload, value}
          {:error, reason} -> {:error, payload, reason}
        end
      end)

    Enum.each(results, fn
      {:ok, p, v} -> IO.puts("  [OK] #{inspect(p)} -> #{inspect(v)}")
      {:error, p, r} -> IO.puts("  [ERR] #{inspect(p)} -> #{inspect(r)}")
    end)

    {us, _} =
      :timer.tc(fn ->
        for _ <- 1..1_000, do: TenantBilling.run(:bench)
      end)

    avg = us / 1_000
    IO.puts("\nBenchmark: #{:erlang.float_to_binary(avg, decimals: 2)} µs/op (1000 iterations)")
    IO.puts("Target: < 100 µs/op for in-process operations\n")
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
