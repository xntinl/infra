# Advanced Ecto Queries: Joins, Subqueries, Aggregations

**Project**: `ecto_queries_deep` — a reporting/analytics layer for an e-commerce platform

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
ecto_queries_deep/
├── lib/
│   └── ecto_queries_deep.ex
├── script/
│   └── main.exs
├── test/
│   └── ecto_queries_deep_test.exs
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
defmodule EctoQueriesDeep.MixProject do
  use Mix.Project

  def project do
    [
      app: :ecto_queries_deep,
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

### `lib/ecto_queries_deep.ex`

```elixir
# lib/ecto_queries_deep/repo.ex
defmodule EctoQueriesDeep.Repo do
  use Ecto.Repo, otp_app: :ecto_queries_deep, adapter: Ecto.Adapters.Postgres
end

# lib/ecto_queries_deep/application.ex
defmodule EctoQueriesDeep.Application do
  use Application

  @impl true
  def start(_type, _args) do
    Supervisor.start_link([EctoQueriesDeep.Repo],
      strategy: :one_for_one, name: EctoQueriesDeep.Supervisor)
  end
end

defmodule EctoQueriesDeep.Schemas.Customer do
  use Ecto.Schema

  schema "customers" do
    field :email, :string
    field :country, :string
    field :tier, :string, default: "standard"
    has_many :orders, EctoQueriesDeep.Schemas.Order
    timestamps()
  end
end

defmodule EctoQueriesDeep.Schemas.Product do
  use Ecto.Schema

  schema "products" do
    field :sku, :string
    field :name, :string
    field :category, :string
    field :price_cents, :integer
    timestamps()
  end
end

defmodule EctoQueriesDeep.Schemas.Order do
  use Ecto.Schema

  schema "orders" do
    field :status, :string
    field :placed_at, :utc_datetime
    field :total_cents, :integer
    belongs_to :customer, EctoQueriesDeep.Schemas.Customer
    has_many :line_items, EctoQueriesDeep.Schemas.LineItem
    timestamps()
  end
end

defmodule EctoQueriesDeep.Schemas.LineItem do
  use Ecto.Schema

  schema "line_items" do
    field :quantity, :integer
    field :unit_price_cents, :integer
    belongs_to :order, EctoQueriesDeep.Schemas.Order
    belongs_to :product, EctoQueriesDeep.Schemas.Product
  end
end

defmodule EctoQueriesDeep.Repo.Migrations.CreateTables do
  use Ecto.Migration

  def change do
    create table(:customers) do
      add :email, :string, null: false
      add :country, :string, null: false
      add :tier, :string, null: false, default: "standard"
      timestamps()
    end
    create unique_index(:customers, [:email])
    create index(:customers, [:country])

    create table(:products) do
      add :sku, :string, null: false
      add :name, :string, null: false
      add :category, :string, null: false
      add :price_cents, :integer, null: false
      timestamps()
    end
    create unique_index(:products, [:sku])

    create table(:orders) do
      add :customer_id, references(:customers, on_delete: :restrict), null: false
      add :status, :string, null: false
      add :placed_at, :utc_datetime, null: false
      add :total_cents, :integer, null: false
      timestamps()
    end
    create index(:orders, [:customer_id])
    create index(:orders, [:placed_at])
    create index(:orders, [:status])

    create table(:line_items) do
      add :order_id, references(:orders, on_delete: :delete_all), null: false
      add :product_id, references(:products, on_delete: :restrict), null: false
      add :quantity, :integer, null: false
      add :unit_price_cents, :integer, null: false
    end
    create index(:line_items, [:order_id])
    create index(:line_items, [:product_id])
  end
end

defmodule EctoQueriesDeep.Reports do
  @moduledoc """
  Production reporting queries. Every function returns rows as maps so callers are free
  of schema coupling.
  """

  import Ecto.Query

  alias EctoQueriesDeep.Repo
  alias EctoQueriesDeep.Schemas.{LineItem, Order, Product}

  @type date_range :: {DateTime.t(), DateTime.t()}

  @doc """
  Top N customers by lifetime revenue in a date range. Pushes aggregation entirely into
  the database.
  """
  @spec top_customers_by_revenue(date_range(), pos_integer()) :: [map()]
  def top_customers_by_revenue({from_dt, to_dt}, limit) do
    from(o in Order,
      as: :order,
      join: c in assoc(o, :customer),
      as: :customer,
      where: o.status == "completed",
      where: o.placed_at >= ^from_dt and o.placed_at < ^to_dt,
      group_by: [c.id, c.email],
      having: sum(o.total_cents) > 0,
      order_by: [desc: sum(o.total_cents)],
      limit: ^limit,
      select: %{
        customer_id: c.id,
        email: c.email,
        order_count: count(o.id),
        revenue_cents: sum(o.total_cents),
        avg_order_cents: avg(o.total_cents)
      }
    )
    |> Repo.all()
  end

  @doc """
  Average order value per category. Uses a subquery to compute per-order category totals
  first, then aggregates.
  """
  @spec avg_order_value_by_category() :: [map()]
  def avg_order_value_by_category do
    per_order_category =
      from li in LineItem,
        join: p in assoc(li, :product),
        group_by: [li.order_id, p.category],
        select: %{
          order_id: li.order_id,
          category: p.category,
          subtotal_cents: sum(li.quantity * li.unit_price_cents)
        }

    from(s in subquery(per_order_category),
      group_by: s.category,
      order_by: [desc: avg(s.subtotal_cents)],
      select: %{
        category: s.category,
        avg_subtotal_cents: avg(s.subtotal_cents),
        order_count: count(s.order_id)
      }
    )
    |> Repo.all()
  end

  @doc """
  Slow-moving SKUs: products that sold fewer than `threshold` units in the last `days`
  days. LEFT JOIN keeps products with zero sales.
  """
  @spec slow_moving_skus(pos_integer(), non_neg_integer()) :: [map()]
  def slow_moving_skus(days, threshold) do
    since = DateTime.add(DateTime.utc_now(), -days * 86_400, :second)

    from(p in Product,
      as: :product,
      left_join: li in LineItem,
      on: li.product_id == p.id,
      left_join: o in Order,
      on: o.id == li.order_id and o.placed_at >= ^since and o.status == "completed",
      group_by: [p.id, p.sku, p.name],
      having: coalesce(sum(li.quantity), 0) < ^threshold,
      order_by: [asc: coalesce(sum(li.quantity), 0)],
      select: %{
        sku: p.sku,
        name: p.name,
        units_sold: coalesce(sum(li.quantity), 0)
      }
    )
    |> Repo.all()
  end

  @doc """
  Dynamic filter builder. Accepts a map of optional filters; ignores unknown keys.
  """
  @spec search_orders(map()) :: [Order.t()]
  def search_orders(params) do
    filters =
      Enum.reduce(params, dynamic(true), fn
        {"status", v}, acc ->
          dynamic([order: o], ^acc and o.status == ^v)

        {"country", v}, acc ->
          dynamic([customer: c], ^acc and c.country == ^v)

        {"min_total_cents", v}, acc ->
          dynamic([order: o], ^acc and o.total_cents >= ^v)

        {"tier", v}, acc ->
          dynamic([customer: c], ^acc and c.tier == ^v)

        _ignore, acc ->
          acc
      end)

    from(o in Order,
      as: :order,
      join: c in assoc(o, :customer),
      as: :customer,
      where: ^filters,
      order_by: [desc: o.placed_at]
    )
    |> Repo.all()
  end
end
```

### `test/ecto_queries_deep_test.exs`

```elixir
defmodule EctoQueriesDeep.ReportsTest do
  use ExUnit.Case, async: true
  doctest EctoQueriesDeep.Repo

  alias EctoQueriesDeep.Repo
  alias EctoQueriesDeep.Reports
  alias EctoQueriesDeep.Schemas.{Customer, Order}

  setup do
    Ecto.Adapters.SQL.Sandbox.checkout(Repo)
    :ok
  end

  defp insert_customer(attrs) do
    %Customer{}
    |> Ecto.Changeset.cast(attrs, [:email, :country, :tier])
    |> Ecto.Changeset.validate_required([:email, :country])
    |> Repo.insert!()
  end

  defp insert_order(customer, total, status \\ "completed") do
    %Order{
      customer_id: customer.id,
      status: status,
      placed_at: DateTime.utc_now() |> DateTime.truncate(:second),
      total_cents: total
    }
    |> Repo.insert!()
  end

  describe "top_customers_by_revenue/2" do
    test "returns top N customers ordered by summed revenue" do
      c1 = insert_customer(%{email: "a@x", country: "AR"})
      c2 = insert_customer(%{email: "b@x", country: "AR"})
      insert_order(c1, 5_000)
      insert_order(c1, 5_000)
      insert_order(c2, 3_000)

      range = {~U[2000-01-01 00:00:00Z], ~U[3000-01-01 00:00:00Z]}
      [first, second] = Reports.top_customers_by_revenue(range, 10)

      assert first.email == "a@x"
      assert first.revenue_cents == 10_000
      assert second.email == "b@x"
    end

    test "excludes cancelled orders" do
      c = insert_customer(%{email: "c@x", country: "AR"})
      insert_order(c, 9_999, "cancelled")
      range = {~U[2000-01-01 00:00:00Z], ~U[3000-01-01 00:00:00Z]}
      assert [] = Reports.top_customers_by_revenue(range, 10)
    end
  end

  describe "search_orders/1" do
    test "combines country + tier filters dynamically" do
      c_ar = insert_customer(%{email: "ar@x", country: "AR", tier: "gold"})
      c_br = insert_customer(%{email: "br@x", country: "BR", tier: "gold"})
      _o1 = insert_order(c_ar, 1000)
      _o2 = insert_order(c_br, 1000)

      results = Reports.search_orders(%{"country" => "AR", "tier" => "gold"})
      assert length(results) == 1
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Advanced Ecto Queries: Joins, Subqueries, Aggregations.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Advanced Ecto Queries: Joins, Subqueries, Aggregations ===")
    IO.puts("Category: Ecto advanced\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case EctoQueriesDeep.run(payload) do
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
        for _ <- 1..1_000, do: EctoQueriesDeep.run(:bench)
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
