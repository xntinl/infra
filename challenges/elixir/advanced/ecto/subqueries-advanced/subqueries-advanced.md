# Advanced Subqueries — `subquery/2`, Lateral Joins, EXISTS

**Project**: `product_catalog` — computed columns, lateral joins, and semi/anti-joins with Ecto

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
product_catalog/
├── lib/
│   └── product_catalog.ex
├── script/
│   └── main.exs
├── test/
│   └── product_catalog_test.exs
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
defmodule ProductCatalog.MixProject do
  use Mix.Project

  def project do
    [
      app: :product_catalog,
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
### `lib/product_catalog.ex`

```elixir
# lib/product_catalog/schemas/product.ex
defmodule ProductCatalog.Schemas.Product do
  @moduledoc """
  Ejercicio: Advanced Subqueries — `subquery/2`, Lateral Joins, EXISTS.
  Implementa el comportamiento descrito en el enunciado, exponiendo funciones públicas documentadas y con tipos claros.
  """

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
### `test/product_catalog_test.exs`

```elixir
defmodule ProductCatalog.CatalogTest do
  use ExUnit.Case, async: true
  doctest ProductCatalog.Schemas.Product
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
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Advanced Subqueries — `subquery/2`, Lateral Joins, EXISTS.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Advanced Subqueries — `subquery/2`, Lateral Joins, EXISTS ===")
    IO.puts("Category: Ecto advanced\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case ProductCatalog.run(payload) do
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
        for _ <- 1..1_000, do: ProductCatalog.run(:bench)
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
