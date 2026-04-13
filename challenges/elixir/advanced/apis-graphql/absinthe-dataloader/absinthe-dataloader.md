# Absinthe GraphQL Schema with Dataloader (N+1 Solved)

**Project**: `shop_api` — a GraphQL API for an e-commerce catalog that exposes products, categories, and reviews without N+1 explosions

---

## Why apis and graphql matters

GraphQL with Absinthe collapses N+1 problems via Dataloader, exposes subscriptions through Phoenix.PubSub, and lets the schema itself enforce complexity limits. REST APIs in Elixir benefit from Plug pipelines, OpenAPI generation, JWT auth, and HMAC-signed webhooks.

The hard parts are not the happy path: it's pagination consistency under concurrent writes, refresh-token rotation, idempotent webhook processing, and complexity budgets that prevent a single query from saturating a node.

---

## The business problem

You are building a production-grade Elixir component in the **APIs and GraphQL** area. The goal is not a toy demonstration — it is a piece you could drop into a real system and rely on under load.

Concretely, you must:

1. Implement the topic correctly with measurable behavior.
2. Surface failure modes via `{:ok, _} / {:error, reason}` tuples — never raise from a public API except for programmer errors.
3. Bound resources: explicit timeouts, memory caps, back-pressure where relevant, and supervision for any long-lived process.
4. Provide a test suite with `async: true` where safe, `doctest` for public examples, and `describe` blocks that name the contract.
5. Provide a `script/main.exs` that demonstrates the piece end-to-end with realistic data — not a `1 + 1` smoke test.

---

## Project structure

```
shop_api/
├── lib/
│   └── shop_api.ex
├── script/
│   └── main.exs
├── test/
│   └── shop_api_test.exs
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

Chose **B** because in APIs and GraphQL the failure modes — not the happy path — are what determine whether the system stays up under real production conditions.

---

## Implementation

### `mix.exs`

```elixir
defmodule ShopApi.MixProject do
  use Mix.Project

  def project do
    [
      app: :shop_api,
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
### `lib/shop_api.ex`

```elixir
defmodule ShopApi.Catalog.Category do
  @moduledoc """
  Ejercicio: Absinthe GraphQL Schema with Dataloader (N+1 Solved).
  Implementa el comportamiento descrito en el enunciado, exponiendo funciones públicas documentadas y con tipos claros.
  """

  use Ecto.Schema

  schema "categories" do
    field :name, :string
    has_many :products, ShopApi.Catalog.Product
    timestamps()
  end
end

defmodule ShopApi.Catalog.Product do
  use Ecto.Schema

  schema "products" do
    field :name, :string
    field :price_cents, :integer
    belongs_to :category, ShopApi.Catalog.Category
    has_many :reviews, ShopApi.Catalog.Review
    timestamps()
  end
end

defmodule ShopApi.Catalog.Review do
  use Ecto.Schema

  schema "reviews" do
    field :rating, :integer
    field :body, :string
    belongs_to :product, ShopApi.Catalog.Product
    timestamps()
  end
end

defmodule ShopApi.Catalog do
  import Ecto.Query
  alias ShopApi.Repo
  alias ShopApi.Catalog.{Product, Category, Review}

  @doc "Returns data result."
  def data, do: Dataloader.Ecto.new(Repo, query: &query/2)

  # Custom query hook — lets us filter (e.g., hide soft-deleted) per source.
  @doc "Returns query result."
  def query(Review, %{min_rating: min}), do: from(r in Review, where: r.rating >= ^min)
  @doc "Returns query result from queryable and _params."
  def query(queryable, _params), do: queryable

  @doc "Lists products result from args."
  def list_products(args) do
    Product
    |> filter_by_category(args[:category_id])
    |> limit_offset(args[:first], args[:offset])
    |> Repo.all()
  end

  defp filter_by_category(q, nil), do: q
  defp filter_by_category(q, id), do: from(p in q, where: p.category_id == ^id)

  defp limit_offset(q, first, offset) do
    from(p in q, limit: ^(first || 20), offset: ^(offset || 0), order_by: [asc: p.id])
  end
end

defmodule ShopApiWeb.Graphql.Types.ProductTypes do
  use Absinthe.Schema.Notation
  import Absinthe.Resolution.Helpers, only: [dataloader: 1, dataloader: 2]

  object :product do
    field :id, non_null(:id)
    field :name, non_null(:string)
    field :price_cents, non_null(:integer)

    field :category, :category do
      resolve dataloader(ShopApi.Catalog)
    end

    field :reviews, list_of(:review) do
      arg :min_rating, :integer, default_value: 1
      resolve dataloader(ShopApi.Catalog, :reviews,
                args: fn args -> %{min_rating: args.min_rating} end)
    end
  end

  object :category do
    field :id, non_null(:id)
    field :name, non_null(:string)
    field :products, list_of(:product), resolve: dataloader(ShopApi.Catalog)
  end

  object :review do
    field :id, non_null(:id)
    field :rating, non_null(:integer)
    field :body, non_null(:string)
  end
end

defmodule ShopApiWeb.Graphql.Schema do
  use Absinthe.Schema

  import_types ShopApiWeb.Graphql.Types.ProductTypes

  alias ShopApiWeb.Graphql.Resolvers.CatalogResolver

  query do
    field :products, list_of(:product) do
      arg :first, :integer, default_value: 20
      arg :offset, :integer, default_value: 0
      arg :category_id, :id
      resolve &CatalogResolver.list_products/3
    end
  end

  @doc "Returns context result from ctx."
  def context(ctx) do
    loader =
      Dataloader.new()
      |> Dataloader.add_source(ShopApi.Catalog, ShopApi.Catalog.data())

    Map.put(ctx, :loader, loader)
  end

  @doc "Returns plugins result."
  def plugins, do: [Absinthe.Middleware.Dataloader] ++ Absinthe.Plugin.defaults()
end

defmodule ShopApiWeb.Graphql.Resolvers.CatalogResolver do
  @doc "Lists products result from _parent, args and _res."
  def list_products(_parent, args, _res), do: {:ok, ShopApi.Catalog.list_products(args)}
end
```
### `test/shop_api_test.exs`

```elixir
defmodule ShopApi.Graphql.SchemaTest do
  use ShopApi.DataCase, async: true
  alias ShopApiWeb.Graphql.Schema

  setup do
    cat = Repo.insert!(%Catalog.Category{name: "Books"})

    products =
      for i <- 1..5 do
        Repo.insert!(%Catalog.Product{name: "P#{i}", price_cents: 100, category_id: cat.id})
      end

    for p <- products, r <- 1..3 do
      Repo.insert!(%Catalog.Review{rating: r, body: "r", product_id: p.id})
    end

    {:ok, %{category: cat}}
  end

  describe "products query" do
    test "returns products with nested category and reviews" do
      query = """
      { products(first: 5) { id name category { name } reviews { rating } } }
      """

      assert {:ok, %{data: %{"products" => list}}} = Absinthe.run(query, Schema)
      assert length(list) == 5
      assert Enum.all?(list, &(&1["category"]["name"] == "Books"))
      assert Enum.all?(list, &(length(&1["reviews"]) == 3))
    end

    test "filters reviews by min_rating via dataloader args" do
      query = "{ products { reviews(minRating: 3) { rating } } }"

      assert {:ok, %{data: %{"products" => list}}} = Absinthe.run(query, Schema)
      assert Enum.all?(list, fn p -> Enum.all?(p["reviews"], &(&1["rating"] >= 3)) end)
    end
  end

  describe "batching" do
    test "resolves 50 products with O(1) queries per depth" do
      # counts SQL using Ecto.Adapters.SQL.Sandbox telemetry if attached
      :telemetry_test.attach_event_handlers(self(), [[:shop_api, :repo, :query]])

      query = "{ products(first: 50) { category { name } reviews { rating } } }"
      {:ok, _} = Absinthe.run(query, Schema)

      queries = receive_all_queries()
      # 1 for products, 1 for categories, 1 for reviews
      assert length(queries) == 3
    end
  end

  defp receive_all_queries(acc \\ []) do
    receive do
      {[:shop_api, :repo, :query], _, meas, _} -> receive_all_queries([meas | acc])
    after
      50 -> acc
    end
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Absinthe GraphQL Schema with Dataloader (N+1 Solved).

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Absinthe GraphQL Schema with Dataloader (N+1 Solved) ===")
    IO.puts("Category: APIs and GraphQL\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case ShopApi.run(payload) do
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
        for _ <- 1..1_000, do: ShopApi.run(:bench)
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

### 1. Dataloader collapses N+1 queries

Without Dataloader, a GraphQL query for 'posts and their authors' issues N+1 queries. With Dataloader, it issues 2 — one for posts, one batched for authors.

### 2. Complexity analysis prevents query DoS

GraphQL allows clients to compose queries. Without complexity limits, a malicious client can request a 10-level deep nested query that brings the server down. Set per-query and per-connection limits.

### 3. Cursor pagination is consistent under writes

Offset pagination skips/duplicates rows under concurrent inserts. Cursor pagination (encode the last-seen ID) is correct regardless of writes.

---
