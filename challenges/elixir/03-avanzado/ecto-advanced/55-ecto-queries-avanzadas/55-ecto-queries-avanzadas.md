# Advanced Ecto Queries: Joins, Subqueries, Aggregations

**Project**: `ecto_queries_deep` — a reporting/analytics layer for an e-commerce platform.

---

## Project context

You're the backend lead on an e-commerce platform that grew from 10k to 2M orders over two
years. The product team needs dashboards: top buyers, monthly cohort retention, average order
value by category, slow-moving SKUs. The previous engineer wrote all reports as
`Repo.all |> Enum.group_by |> ...` pipelines. At 2M rows the BEAM process that generates the
"top buyers" report allocates 1.6 GB and takes 42 s. Something has to move into SQL.

The target is to push aggregation, grouping and ranking into PostgreSQL using `Ecto.Query`
composition. A query expressed correctly in Ecto compiles to a single SQL statement executed
in the database, returning only the aggregated rows. You trade "familiar Elixir pipelines"
for two to three orders of magnitude in latency and memory.

This module teaches query composition beyond the `from(x in X, where: ...)` basics: joins
(inner/left/lateral), aggregations with `group_by` and `having`, subqueries as virtual tables,
`select_merge`, and `dynamic/2` for runtime-built filters. The code is written against a
realistic schema used across later exercises (`Order`, `LineItem`, `Product`, `Customer`).

---

```
ecto_queries_deep/
├── lib/
│   └── ecto_queries_deep/
│       ├── application.ex
│       ├── repo.ex
│       ├── schemas/
│       │   ├── customer.ex
│       │   ├── order.ex
│       │   ├── line_item.ex
│       │   └── product.ex
│       └── reports.ex                # all query builders live here
├── priv/
│   └── repo/
│       └── migrations/
│           └── 20260101000000_create_tables.exs
├── test/
│   └── ecto_queries_deep/
│       └── reports_test.exs
├── config/
│   └── config.exs
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

### 1. Query composability

Every `Ecto.Query` is a struct. `from`, `where`, `join`, `select`, `order_by` all return a
new query. This means you can build queries incrementally:

```
base_query ──▶ filter_by_tenant ──▶ filter_by_date ──▶ paginate ──▶ Repo.all
                   (adds where)        (adds where)     (adds limit/offset)
```

Each helper takes a query, returns a query. A report becomes a pipeline of small
composable functions. Prefer this over giant `from(...)` blocks — it is the single biggest
maintainability win in Ecto.

### 2. Binding positions and named bindings

In `from(o in Order, join: c in assoc(o, :customer), where: ...)` the positional bindings
are `[o, c]`. When queries are composed across functions, positions shift and readers get
lost. Named bindings fix this:

```elixir
from(o in Order, as: :order,
  join: c in assoc(o, :customer), as: :customer)
```

Then `where: [as: :customer].country == "AR"` works regardless of what else was added.
Named bindings are required once you compose three or more helpers.

### 3. `group_by` + `having` vs `where`

`where` filters rows **before** aggregation. `having` filters rows **after** aggregation.
A condition on `count(*) > 5` must live in `having` — `count` does not exist per-row.
Postgres enforces: any non-aggregated column in `select` must appear in `group_by`.

### 4. Joins: inner, left, lateral

- `:inner_join` — rows must exist on both sides; drops unmatched orders-without-customer.
- `:left_join` — keeps left rows; right side becomes `nil` when no match. Use for
  "customers and their order count (including customers with zero)".
- `:lateral_join` — right side is a subquery that can reference columns from the left row
  by row. Essential for "top-N per group" (top 3 orders per customer).

### 5. `subquery/1` as a virtual table

A complex aggregation can be wrapped as `subquery(inner)` and joined again. This maps
to `FROM (SELECT ...) AS sub` in SQL. Use it when you need an aggregated value per group
and then filter by that value — the outer query treats the subquery like a schema.

### 6. `dynamic/2` for runtime filters

Search forms let users combine N optional filters. Building a query with `if`-branches
produces unreadable code. `Ecto.Query.dynamic/2` fragments compose as boolean expressions:

```elixir
filters =
  Enum.reduce(params, dynamic(true), fn
    {"country", v}, acc -> dynamic([customer: c], ^acc and c.country == ^v)
    _, acc -> acc
  end)

from(o in Order, as: :order, join: c in assoc(o, :customer), as: :customer, where: ^filters)
```

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

### Step 1: Create the project

```bash
mix new ecto_queries_deep --sup
cd ecto_queries_deep
```

`mix.exs`:

```elixir
defp deps do
  [
    {:ecto_sql, "~> 3.11"},
    {:postgrex, "~> 0.17"}
  ]
end
```

`config/config.exs`:

```elixir
import Config

config :ecto_queries_deep, ecto_repos: [EctoQueriesDeep.Repo]

config :ecto_queries_deep, EctoQueriesDeep.Repo,
  database: "ecto_queries_deep_#{config_env()}",
  username: "postgres",
  password: "postgres",
  hostname: "localhost",
  pool_size: 10
```

### Step 2: Repo and schemas

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
```

Schemas (trimmed; see project tree):

```elixir
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
```

### Step 3: Migrations

```elixir
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
```

### Step 4: Reports module — the core of this exercise

```elixir
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

### Step 5: Tests

```elixir
defmodule EctoQueriesDeep.ReportsTest do
  use ExUnit.Case, async: false

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

## Trade-offs and production gotchas

**1. N+1 still lurks in `select`**
Putting `c.email` in `select` while joining is fine; putting `%{customer: c}` and then
accessing `c.orders` in the result is an N+1 waiting to happen. Use `preload` with joins
for association loading, not `select`.

**2. `group_by` column lists get out of sync with `select`**
Every non-aggregated column in `select` must appear in `group_by`. When you add `c.tier`
to `select` and forget `group_by`, Postgres returns `column must appear in GROUP BY`.

**3. `having` on aliased aggregates does not work everywhere**
`having: total > 100` where `total` is a `select` alias fails on many databases. Repeat
the aggregate: `having: sum(o.total_cents) > 100`. It's ugly but portable.

**4. `dynamic/2` silently skips unknown keys**
Our `search_orders` reduces over params and ignores unknown filters. If your API accepts
`status_in: [...]` and you forget to add a clause, users think the filter works and the
query returns all rows. Always have an explicit whitelist test.

**5. Subqueries can block index usage**
`FROM (SELECT ... GROUP BY ...) sub WHERE sub.x = 1` often cannot use an index on `x`
because Postgres cannot push the predicate through the aggregation. Measure with
`EXPLAIN ANALYZE`, not intuition.

**6. `avg` returns `Decimal` in Postgres**
`avg(integer)` is `numeric` in SQL → `Decimal` in Elixir. Your tests that do
`assert avg == 5` fail silently against `Decimal.new(5)`. Use `Decimal.equal?/2`.

**7. `coalesce(sum(x), 0)` is mandatory with LEFT JOIN**
A LEFT JOIN with no matches yields `sum(NULL) = NULL`. If you compare `< threshold`,
`NULL < 10` is `NULL` (not true) and the row gets dropped. Always wrap aggregates
over outer joins in `coalesce`.

**8. When NOT to use this**
If the report needs 50+ lines of business logic (ranking ties broken by multiple rules,
currency conversion with rate history, forecasting) — push it to a materialized view
or an OLAP store (ClickHouse, DuckDB). Ecto queries excel at composition; they become
write-only past ~80 lines.

---

## Performance notes

Measure a report both ways:

```elixir
{t_elixir, _} = :timer.tc(fn ->
  Order |> Repo.all()
  |> Enum.group_by(& &1.customer_id)
  |> Enum.map(fn {id, orders} -> {id, Enum.sum(Enum.map(orders, & &1.total_cents))} end)
  |> Enum.sort_by(&elem(&1, 1), :desc)
  |> Enum.take(10)
end)

{t_sql, _} = :timer.tc(fn ->
  Reports.top_customers_by_revenue({~U[2000-01-01 00:00:00Z], ~U[3000-01-01 00:00:00Z]}, 10)
end)

IO.inspect({t_elixir, t_sql}, label: "microseconds")
```

With 100k orders expect `t_sql` to be 20–50× faster and allocate almost nothing on the
BEAM heap, because the aggregated result set is ~10 rows.

---

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?

## Resources

- [`Ecto.Query` — hexdocs](https://hexdocs.pm/ecto/Ecto.Query.html) — canonical reference; read Composition and Bindings sections first.
- [Ecto.Query.dynamic/2](https://hexdocs.pm/ecto/Ecto.Query.html#dynamic/2) — official examples of runtime filter composition.
- [Programming Ecto — Darin Wilson & Eric Meadows-Jönsson](https://pragprog.com/titles/wmecto/programming-ecto/) — chapters 5–7 on query composition.
- [PostgreSQL `EXPLAIN ANALYZE`](https://www.postgresql.org/docs/current/using-explain.html) — understand what your Ecto query actually runs.
- [Dashbit blog](https://dashbit.co/blog) — recurring posts on Ecto query composition.
- [Phoenix LiveDashboard Ecto page source](https://github.com/phoenixframework/phoenix_live_dashboard) — real-world aggregation queries.
