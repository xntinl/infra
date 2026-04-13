# Advanced Subqueries — `subquery/2`, Lateral Joins, EXISTS

**Project**: `product_catalog` — computed columns, lateral joins, and semi/anti-joins with Ecto.

---

## Project context

A storefront shows products with three derived fields: current price (from a pricing
history table), last review date (from reviews), and an "in stock in any warehouse" flag.
Computing these in the app layer requires N+1 queries; preloads do not express these
aggregations cleanly. `subquery/2` in Ecto lifts a sub-SELECT into a composable query
fragment.

```
product_catalog/
├── lib/
│   └── product_catalog/
│       ├── application.ex
│       ├── repo.ex
│       ├── catalog.ex
│       └── schemas/
│           ├── product.ex
│           ├── price.ex
│           ├── review.ex
│           └── warehouse_stock.ex
├── priv/repo/migrations/
├── test/product_catalog/
│   └── catalog_test.exs
├── bench/catalog_bench.exs
└── mix.exs
```

---

## Core concepts

### 1. `subquery/2` — use a query as a source

```elixir
latest_prices =
  from p in Price,
    distinct: p.product_id,
    order_by: [asc: p.product_id, desc: p.effective_at],
    select: %{product_id: p.product_id, amount: p.amount}

from p in Product,
  join: lp in subquery(latest_prices), on: lp.product_id == p.id,
  select: {p.name, lp.amount}
```

The subquery is wrapped into `FROM (SELECT ...) AS lp` in SQL. This is the only way to
join against an aggregated projection — you cannot `join` directly onto a schema with
`distinct on` or `group by`.

### 2. Lateral join — subquery that sees the outer row

```sql
SELECT p.name, r.inserted_at
FROM products p
LEFT JOIN LATERAL (
  SELECT * FROM reviews
  WHERE product_id = p.id
  ORDER BY inserted_at DESC
  LIMIT 1
) r ON TRUE
```

`LATERAL` means "for each row of the outer query, run this subquery". In Ecto, you express
this via `join: ..., on: true` with a parameterized inner query using `fragment/1` — the
DSL does not natively support LATERAL. We show the pattern below.

### 3. `exists/1` — semi-join

```elixir
from p in Product,
  where: exists(from w in WarehouseStock, where: w.product_id == parent_as(:p).id and w.qty > 0)
```

With `parent_as/1` Ecto references the outer query's alias inside the subquery. The
planner executes this as a semi-join: it stops scanning the stock rows for a given
product as soon as one match is found.

### 4. `not exists` — anti-join

For "products with no reviews":

```elixir
from p in Product, as: :p,
  where: not exists(from r in Review, where: r.product_id == parent_as(:p).id)
```

Much faster than `LEFT JOIN ... WHERE r.id IS NULL` on large tables with selective filters.

---

## Design decisions

- **Option A — `subquery/2` joins**: composable, reusable. Pros: works with aggregates
  and distinct. Cons: every join is a subquery materialization.
- **Option B — raw lateral joins via `fragment`**: best performance for "top-N per parent".
  Pros: index-friendly. Cons: raw SQL inside the DSL.

We use **A for current price and stock exists**, **B with a lateral fragment for last
review** where we need "latest row per product".

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

### Step 1: Schemas

**Objective**: Shape Product with has_many prices/reviews/stocks so catalog subqueries (latest price, exists, lateral) bind against known associations.

```elixir
# lib/product_catalog/schemas/product.ex
defmodule ProductCatalog.Schemas.Product do
  use Ecto.Schema

  schema "products" do
    field :sku, :string
    field :name, :string
    has_many :prices, ProductCatalog.Schemas.Price
    has_many :reviews, ProductCatalog.Schemas.Review
    has_many :stocks, ProductCatalog.Schemas.WarehouseStock
    timestamps()
  end
end

# lib/product_catalog/schemas/price.ex
defmodule ProductCatalog.Schemas.Price do
  use Ecto.Schema

  schema "prices" do
    belongs_to :product, ProductCatalog.Schemas.Product
    field :amount_cents, :integer
    field :effective_at, :utc_datetime
    timestamps(updated_at: false)
  end
end

# lib/product_catalog/schemas/review.ex
defmodule ProductCatalog.Schemas.Review do
  use Ecto.Schema

  schema "reviews" do
    belongs_to :product, ProductCatalog.Schemas.Product
    field :rating, :integer
    field :body, :text
    timestamps()
  end
end

# lib/product_catalog/schemas/warehouse_stock.ex
defmodule ProductCatalog.Schemas.WarehouseStock do
  use Ecto.Schema

  schema "warehouse_stock" do
    belongs_to :product, ProductCatalog.Schemas.Product
    field :warehouse_id, :string
    field :qty, :integer, default: 0
    timestamps()
  end
end
```

### Step 2: Catalog context

**Objective**: Compose DISTINCT ON, LATERAL, EXISTS anti-joins, and ranked subqueries so catalog listings collapse N queries into one plan.

```elixir
# lib/product_catalog/catalog.ex
defmodule ProductCatalog.Catalog do
  import Ecto.Query

  alias ProductCatalog.Repo
  alias ProductCatalog.Schemas.{Price, Product, Review, WarehouseStock}

  @doc """
  Catalog listing with:
    - current price (latest by effective_at)
    - last review timestamp (or nil)
    - in_stock flag (any warehouse has qty > 0)
  """
  @spec list_with_derived() :: [map()]
  def list_with_derived do
    latest_price =
      from p in Price,
        distinct: p.product_id,
        order_by: [asc: p.product_id, desc: p.effective_at],
        select: %{product_id: p.product_id, amount_cents: p.amount_cents}

    from(p in Product, as: :p)
    |> join(:left, [p: p], lp in subquery(latest_price), on: lp.product_id == p.id)
    |> join(:left_lateral, [p: p], r in fragment(
         "(SELECT r.inserted_at FROM reviews r WHERE r.product_id = ? ORDER BY r.inserted_at DESC LIMIT 1)",
         p.id
       ), on: true)
    |> select([p: p, lp: lp, r: r], %{
      id: p.id,
      sku: p.sku,
      name: p.name,
      price_cents: lp.amount_cents,
      last_review_at: r,
      in_stock:
        exists(
          from ws in WarehouseStock,
            where: ws.product_id == parent_as(:p).id and ws.qty > 0
        )
    })
    |> order_by([p: p], asc: p.id)
    |> Repo.all()
  end

  @doc """
  Products that have zero reviews. Anti-join pattern.
  """
  @spec products_without_reviews() :: [Product.t()]
  def products_without_reviews do
    from(p in Product, as: :p,
      where: not exists(from r in Review, where: r.product_id == parent_as(:p).id)
    )
    |> Repo.all()
  end

  @doc """
  Products whose latest price dropped vs. the previous price.
  """
  @spec price_drops() :: [%{id: integer(), name: String.t(), old: integer(), new: integer()}]
  def price_drops do
    ranked =
      from p in Price,
        windows: [w: [partition_by: p.product_id, order_by: [desc: p.effective_at]]],
        select: %{
          product_id: p.product_id,
          amount_cents: p.amount_cents,
          rn: row_number() |> over(:w)
        }

    latest = from r in subquery(ranked), where: r.rn == 1, select: %{product_id: r.product_id, amount: r.amount_cents}
    previous = from r in subquery(ranked), where: r.rn == 2, select: %{product_id: r.product_id, amount: r.amount_cents}

    from(p in Product,
      join: l in subquery(latest), on: l.product_id == p.id,
      join: pr in subquery(previous), on: pr.product_id == p.id,
      where: l.amount < pr.amount,
      select: %{id: p.id, name: p.name, old: pr.amount, new: l.amount}
    )
    |> Repo.all()
  end
end
```

---

## Why this works

- `distinct: p.product_id, order_by: [asc: p.product_id, desc: p.effective_at]` is Postgres
  `DISTINCT ON`: one row per `product_id`, picking the row that comes first by the order.
  Used to collapse prices down to "latest per product".
- The lateral-join fragment for `last_review_at` runs one tiny `LIMIT 1` subquery per
  product row. The planner uses an index on `(product_id, inserted_at DESC)` for each.
- `exists/1` with `parent_as(:p)` produces a correlated semi-join; Postgres short-circuits
  as soon as one matching warehouse row is found.
- The `price_drops` query stacks two subqueries: `latest` (rn=1) and `previous` (rn=2).
  Ecto composes them cleanly because subquery projections define typed rows.

---

## Data flow — `list_with_derived/0`

```
SELECT
  p.id, p.sku, p.name,
  lp.amount_cents,             -- FROM subquery: DISTINCT ON(product_id)
  r.inserted_at,               -- LEFT JOIN LATERAL LIMIT 1
  EXISTS (                     -- semi-join
    SELECT 1 FROM warehouse_stock
    WHERE product_id = p.id AND qty > 0
  ) AS in_stock
FROM products p
LEFT JOIN (
  SELECT DISTINCT ON (product_id) product_id, amount_cents
  FROM prices ORDER BY product_id, effective_at DESC
) lp ON lp.product_id = p.id
LEFT JOIN LATERAL (
  SELECT inserted_at FROM reviews
  WHERE product_id = p.id ORDER BY inserted_at DESC LIMIT 1
) r ON TRUE
ORDER BY p.id
```

Three distinct sub-patterns in one query, all index-friendly with proper indexes on
`(product_id, effective_at DESC)`, `(product_id, inserted_at DESC)`, and
`(product_id, qty)`.

---

## Tests

```elixir
# test/product_catalog/catalog_test.exs
defmodule ProductCatalog.CatalogTest do
  use ExUnit.Case, async: false
  alias ProductCatalog.{Catalog, Repo}
  alias ProductCatalog.Schemas.{Price, Product, Review, WarehouseStock}

  setup do
    Ecto.Adapters.SQL.Sandbox.checkout(Repo)
    Ecto.Adapters.SQL.Sandbox.mode(Repo, {:shared, self()})
    Repo.delete_all(WarehouseStock)
    Repo.delete_all(Review)
    Repo.delete_all(Price)
    Repo.delete_all(Product)

    {:ok, p} = Repo.insert(%Product{sku: "A1", name: "Widget"})
    {:ok, _} = Repo.insert(%Price{product_id: p.id, amount_cents: 1000, effective_at: DateTime.utc_now() |> DateTime.add(-3600)})
    {:ok, _} = Repo.insert(%Price{product_id: p.id, amount_cents: 900, effective_at: DateTime.utc_now()})
    {:ok, _} = Repo.insert(%WarehouseStock{product_id: p.id, warehouse_id: "w1", qty: 5})
    {:ok, _} = Repo.insert(%Review{product_id: p.id, rating: 5, body: "ok"})

    {:ok, no_review} = Repo.insert(%Product{sku: "A2", name: "NoReview"})
    {:ok, _} = Repo.insert(%Price{product_id: no_review.id, amount_cents: 500, effective_at: DateTime.utc_now()})

    {:ok, p: p, no_review: no_review}
  end

  describe "list_with_derived/0" do
    test "picks latest price", %{p: p} do
      rows = Catalog.list_with_derived()
      row = Enum.find(rows, &(&1.id == p.id))
      assert row.price_cents == 900
    end

    test "populates last_review_at when reviews exist", %{p: p} do
      rows = Catalog.list_with_derived()
      row = Enum.find(rows, &(&1.id == p.id))
      assert %DateTime{} = row.last_review_at
    end

    test "last_review_at is nil for products without reviews", %{no_review: nr} do
      rows = Catalog.list_with_derived()
      row = Enum.find(rows, &(&1.id == nr.id))
      assert row.last_review_at == nil
    end

    test "in_stock is true when any warehouse has qty > 0", %{p: p} do
      rows = Catalog.list_with_derived()
      row = Enum.find(rows, &(&1.id == p.id))
      assert row.in_stock == true
    end
  end

  describe "products_without_reviews/0" do
    test "returns only products with zero reviews", %{no_review: nr} do
      results = Catalog.products_without_reviews()
      assert Enum.map(results, & &1.id) == [nr.id]
    end
  end

  describe "price_drops/0" do
    test "lists products where latest price is below previous", %{p: p} do
      [drop] = Catalog.price_drops()
      assert drop.id == p.id
      assert drop.old == 1000
      assert drop.new == 900
    end
  end
end
```

---

## Benchmark

```elixir
# bench/catalog_bench.exs
Benchee.run(
  %{
    "list_with_derived"     => fn -> ProductCatalog.Catalog.list_with_derived() end,
    "products_without_rev"  => fn -> ProductCatalog.Catalog.products_without_reviews() end,
    "price_drops"           => fn -> ProductCatalog.Catalog.price_drops() end
  },
  time: 5, warmup: 2
)
```

**Target**: `list_with_derived` under 20 ms for 5k products with proper indexes. If the
lateral join shows up > 5 ms in `EXPLAIN ANALYZE`, the `(product_id, inserted_at DESC)`
index on `reviews` is missing.

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

**1. `DISTINCT ON` requires `ORDER BY` starting with the distinct columns.** `distinct: p.product_id`
*must* be paired with `order_by: [asc: p.product_id, desc: ...]`. Mismatches raise at runtime.

**2. Lateral joins are not first-class in Ecto.** We use `join: :left_lateral` which Ecto
3.11+ supports when combined with a `fragment/2` source. Older Ecto versions need raw SQL.

**3. `exists` with `parent_as` requires `as: :alias` on the outer query.** Omitting the
binding alias breaks the correlation.

**4. Subquery in `join` materializes fully.** For large tables, prefer a CTE or a query
parameter limiting the subquery. `DISTINCT ON` over 10M rows without an index is slow
even as a subquery.

**5. `select`ing into `%{...}` maps loses schema callbacks.** If you need changeset-based
mutations, load the struct in a second query keyed by the derived result.

**6. When NOT to subquery.** If the "derived" column is always the same across all rows
(a config value, a constant), hardcode it in Elixir. Subqueries add planning overhead.

---

## Reflection

Your `list_with_derived` query runs in 18 ms for 5k products. The product team wants the
same list filtered by "products with a 5-star review from the last 30 days". You can add
a `WHERE` clause with another `EXISTS` subquery, or materialize a denormalized
`product_features` table that precomputes this flag nightly. Compare: the read-latency
cost today vs. the staleness introduced tomorrow. Which is more expensive for your
business?

---

## Resources

- [`Ecto.Query.subquery/2`](https://hexdocs.pm/ecto/Ecto.Query.html#subquery/2)
- [`parent_as/1` and `as:` bindings](https://hexdocs.pm/ecto/Ecto.Query.html#module-bindings)
- [Postgres — LATERAL subqueries](https://www.postgresql.org/docs/current/queries-table-expressions.html#QUERIES-LATERAL)
- [Postgres — DISTINCT ON](https://www.postgresql.org/docs/current/sql-select.html#SQL-DISTINCT)

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```
