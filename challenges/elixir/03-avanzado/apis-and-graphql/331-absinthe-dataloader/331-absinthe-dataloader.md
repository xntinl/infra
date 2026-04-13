# Absinthe GraphQL Schema with Dataloader (N+1 Solved)

**Project**: `shop_api` — a GraphQL API for an e-commerce catalog that exposes products, categories, and reviews without N+1 explosions.

## Project context

Your team ships a small GraphQL server for a storefront. The previous REST version was replaced because mobile clients kept complaining about underfetching: a product listing required three round trips (product, category, reviews). The GraphQL rewrite was delivered in two days, and within a week the database team paged at 2 a.m. — a single `products(first: 50)` query was issuing **3 + 50 + 50 + 50 = 153 SQL statements**. Classic N+1.

This exercise delivers a production-grade Absinthe schema backed by `Dataloader`: every field batches per request, loads are memoized inside the context, and fields that need authorization run through a middleware chain. The goal is the senior-level baseline — resolvers that do not know they are batched, and queries whose SQL cost is bounded by `O(depth)`, not `O(nodes)`.

```
shop_api/
├── lib/
│   ├── shop_api/
│   │   ├── application.ex
│   │   ├── repo.ex
│   │   ├── catalog.ex              # context module (Ecto queries)
│   │   ├── catalog/
│   │   │   ├── product.ex
│   │   │   ├── category.ex
│   │   │   └── review.ex
│   │   └── graphql/
│   │       ├── schema.ex
│   │       ├── types/
│   │       │   ├── product_types.ex
│   │       │   ├── category_types.ex
│   │       │   └── review_types.ex
│   │       ├── resolvers/
│   │       │   └── catalog_resolver.ex
│   │       └── middleware/
│   │           └── handle_changeset_errors.ex
│   └── shop_api_web/
│       ├── endpoint.ex
│       └── router.ex
├── test/
│   └── shop_api/graphql/schema_test.exs
├── bench/
│   └── dataloader_bench.exs
└── mix.exs
```

## Why Dataloader and not manual batching

Manual batching (`Absinthe.Resolution.Helpers.batch/3`) works for two or three related fields. It stops scaling the moment a resolver needs data from more than one source or wants to share a cache across resolvers in the same request. You end up wiring each batcher by hand and duplicating "load-or-cache" logic across fields.

`Dataloader` solves three problems at once:

1. **Request-scoped cache**: the same `category_id` resolved by product #1 is reused by product #42 without a second query.
2. **Multiple sources**: `Ecto`, HTTP, Redis — each source is a separate loader; the batching rules (`:assoc`, `:many`, custom `load/3`) are uniform.
3. **Lazy loads**: Absinthe's execution phase collects all `dataloader/2` calls across the whole query, dispatches them in parallel, and only then continues resolving. This is what turns N+1 into N-to-1-per-field.

## Why `:assoc` instead of raw `Repo.all` with `preload`

A naive alternative is to preload associations up front:

```elixir
Repo.all(from p in Product, preload: [:category, :reviews])
```

That loads **every association for every query**, even if the client asked only for `products { name }`. GraphQL's whole point is that the client picks the shape; preloading defeats that. Dataloader keeps the server lazy — fields only load what is actually selected.

## Core concepts

### 1. The loader lives in the Absinthe context

Dataloader is created per request in `context/1` and put into `ctx.loader`. Resolvers never call `Repo` directly — they ask the loader.

### 2. `on_load/2` returns a continuation

`dataloader(Catalog, :category)` returns a function that Absinthe's executor calls **after** batching. You do not `await` anything — Absinthe handles it.

### 3. `:assoc` source knows your schema

The Ecto source introspects `has_many`/`belongs_to` and builds the `IN (?, ?, ?)` query automatically. You only write a custom `query/2` when you need extra filters (e.g., soft-delete).

## Design decisions

- **Option A — `dataloader(Catalog)` helper in every field**: pros: one line per field, readable; cons: you must remember to use it.
- **Option B — middleware that auto-batches by convention**: pros: impossible to forget; cons: magic, breaks when a field wants custom logic.
→ We pick **A**. The explicit `dataloader/2` call is the senior-friendly default: grep-able, debuggable, and the resolver is a pure function of `{parent, args, ctx}`.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule ShopApi.MixProject do
  use Mix.Project

  def project do
    [
      app: :shop_api,
      version: "0.1.0",
      elixir: "~> 1.16",
      deps: deps()
    ]
  end

  def application do
    [mod: {ShopApi.Application, []}, extra_applications: [:logger]]
  end

  defp deps do
    [
      {:phoenix, "~> 1.7"},
      {:phoenix_ecto, "~> 4.4"},
      {:ecto_sql, "~> 3.11"},
      {:postgrex, "~> 0.17"},
      {:absinthe, "~> 1.7"},
      {:absinthe_plug, "~> 1.5"},
      {:absinthe_phoenix, "~> 2.0"},
      {:dataloader, "~> 2.0"},
      {:jason, "~> 1.4"},
      {:benchee, "~> 1.3", only: :dev}
    ]
  end
end
```

### Step 1: Ecto schemas

**Objective**: Model Category/Product/Review with bidirectional associations so Dataloader can batch lookups from either side of the FK.

```elixir
defmodule ShopApi.Catalog.Category do
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
```

### Step 2: Context with a Dataloader `data/0`

**Objective**: Expose `data/0` returning `Dataloader.Ecto.new(Repo, query: &query/2)` so per-source filters (min_rating, soft-delete) live next to the domain.

```elixir
defmodule ShopApi.Catalog do
  import Ecto.Query
  alias ShopApi.Repo
  alias ShopApi.Catalog.{Product, Category, Review}

  def data, do: Dataloader.Ecto.new(Repo, query: &query/2)

  # Custom query hook — lets us filter (e.g., hide soft-deleted) per source.
  def query(Review, %{min_rating: min}), do: from(r in Review, where: r.rating >= ^min)
  def query(queryable, _params), do: queryable

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
```

### Step 3: GraphQL types wired with `dataloader/2`

**Objective**: Wire associations via `dataloader(source, :assoc, args: fn)` so args flow to `query/2` and each field batches per (source, args) key.

```elixir
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
```

### Step 4: Root schema with loader in context

**Objective**: Seed a fresh `Dataloader` per request in `context/1` and register `Absinthe.Middleware.Dataloader` as a plugin to flush batches between levels.

```elixir
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

  def context(ctx) do
    loader =
      Dataloader.new()
      |> Dataloader.add_source(ShopApi.Catalog, ShopApi.Catalog.data())

    Map.put(ctx, :loader, loader)
  end

  def plugins, do: [Absinthe.Middleware.Dataloader] ++ Absinthe.Plugin.defaults()
end

defmodule ShopApiWeb.Graphql.Resolvers.CatalogResolver do
  def list_products(_parent, args, _res), do: {:ok, ShopApi.Catalog.list_products(args)}
end
```

## Why this works

Absinthe runs in phases. During the **resolution phase** it walks the query tree top-down; any resolver returning `{:middleware, Absinthe.Middleware.Dataloader, fun}` (which `dataloader/2` builds) gets parked. Once the current level is done, the **dataloader plugin phase** calls `Dataloader.run/1` on the single loader instance from context. All `load/3` calls registered at that level fire as one batched query per source/key pair. The parked resolvers resume with results already in memory.

For a query that fetches 50 products with their category and reviews:

```
phase 1: resolve products          -> 1 SQL (SELECT * FROM products LIMIT 50)
phase 2: park 50 category loads,
         park 50 review loads
phase 3: Dataloader.run/1          -> 2 SQL:
                                      SELECT * FROM categories WHERE id IN ($1..$K)
                                      SELECT * FROM reviews    WHERE product_id IN ($1..$50)
phase 4: resume, build response
```

Three queries total, regardless of result size. That is the whole win.

## Tests

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

## Benchmark

```elixir
# bench/dataloader_bench.exs
query = "{ products(first: 100) { category { name } reviews { rating } } }"

Benchee.run(%{
  "100 products + category + reviews (dataloaded)" => fn ->
    {:ok, _} = Absinthe.run(query, ShopApiWeb.Graphql.Schema)
  end
}, time: 5, warmup: 2)
```

**Expected on a warm Postgres + SSD**: < 15 ms per full execution. Compare against the naive version (remove `dataloader/2`, resolve each field with `Repo.get/2`) — you should see **> 200 ms** and ~200 queries in the Postgres log.

## Deep Dive: Query Complexity and N+1 Prevention Patterns

GraphQL's flexibility is a double-edged sword. A query like `{ users { posts { comments { author { email } } } } }`
becomes a DDoS vector if unchecked: a resolver that loads each post's comments naively yields 1000 database 
queries for a 100-user query.

**Three strategies to prevent N+1**:
1. **Dataloader batching** (Absinthe-native): Queue fields in phase 1 (`load/3`), flush in phase 2 (`run/1`).
   Single database call per level. Works across HTTP boundaries via custom sources.
2. **Ecto select/5 eager loading** (preload): Best when schema relationships are known at resolver definition time.
   Fine-grained control; requires discipline in your types.
3. **Complexity analysis** (persisted queries): Assign a "weight" to each field (users=2, posts=5, comments=10).
   Reject queries exceeding a threshold BEFORE execution. Prevents runaway queries entirely.

**Production gotcha**: Complexity analysis doesn't prevent slow queries — it prevents expensive queries.
A query that hits 50,000 database rows but under the complexity limit still runs. Combine with database 
query timeouts and active monitoring.

**Subscription patterns** (real-time): Subscriptions over PubSub break traditional Dataloader batching 
because events arrive asynchronously. Use a separate resolver that doesn't call the loader; instead, 
publish (source) and subscribe (sink) directly. This keeps subscriptions cheap and doesn't starve 
the dataloader queue.

**Field-level authorization**: Dataloader sources can enforce per-user visibility rules at load time, 
not in the resolver. This is cleaner than filtering after the fact and reduces unnecessary database 
queries for unauthorized fields.

---

## Advanced Considerations

API implementations at scale require careful consideration of request handling, error responses, and the interaction between multiple clients with different performance expectations. The distinction between public APIs and internal APIs affects error reporting granularity, versioning strategies, and backwards compatibility guarantees fundamentally. Versioning APIs through headers, paths, or query parameters each have trade-offs in terms of maintenance burden, client complexity, and developer experience across multiple client versions. When deprecating API endpoints, the migration window and support period must balance client migration costs with infrastructure maintenance costs and team capacity constraints.

GraphQL adds complexity around query costs, depth limits, and the interaction between nested resolvers and N+1 query problems. A deeply nested GraphQL query can trigger hundreds of database queries if not carefully managed with proper preloading and query analysis. Implementing query cost analysis prevents malicious or poorly-written queries from starving resources and degrading service for other clients. The caching layer becomes more complex with GraphQL because the same data may be accessed through multiple query paths, each with different caching semantics and TTL requirements that must be carefully coordinated at the application level.

Error handling and status codes require careful design to balance information disclosure with security concerns. Too much detail in error messages helps attackers; too little detail frustrates legitimate users. Implement structured error responses with specific error codes that clients can use to handle different failure scenarios intelligently and retry appropriately. Rate limiting, circuit breakers, and backpressure mechanisms prevent API overload but require careful configuration based on expected traffic patterns and SLA requirements.


## Deep Dive: Apis Patterns and Production Implications

API testing requires testing schema validation, error messages, pagination, and rate limiting—not just happy paths. The mistake is testing only the happy path and assuming error handling works. Production APIs with weak error handling become support nightmares.

---

## Trade-offs and production gotchas

**1. Loader leaks between requests**
If you build the loader outside `context/1` (e.g., in `application.ex`) you share cache across requests. User A sees user B's category. The loader MUST be request-scoped.

**2. `dataloader/2` with dynamic args requires a key function**
When the same association is loaded with different filters (e.g., `min_rating: 3` vs `min_rating: 5`), you must pass `args: fn args -> ... end` so the loader keys the batch by filter. Otherwise all calls share one cache slot and the first filter wins.

**3. Using `preload` inside the context query hook**
If `query/2` adds `preload: [:x]`, those preloads fire on every batch — you've reintroduced N+1 at a deeper layer. Let Dataloader handle nested associations.

**4. No timeout on the loader**
`Dataloader.run/1` defaults to `:infinity`. A slow source locks the whole request. Configure `:timeout` when adding the source or wrap with `Task.async_nolink` + `Task.yield`.

**5. Ignoring `plugins/0`**
If you forget `def plugins, do: [Absinthe.Middleware.Dataloader] ++ Absinthe.Plugin.defaults()`, every `dataloader/2` call becomes a no-op that returns `nil`. Silent bug.

**6. When NOT to use this**
For mutations or single-entity queries (`product(id: $id)`), Dataloader adds a phase overhead without any batching win. Call the context directly in the resolver.

## Reflection

If your resolver needs to call two sources where the second depends on the first (e.g., load user, then load user's preferences from Redis), does Dataloader batch across them? What does the execution graph look like, and where would you add a custom middleware to parallelize instead of serialize?

## Resources

- [Absinthe — Dataloader guide](https://hexdocs.pm/absinthe/dataloader.html)
- [`Dataloader.Ecto` source](https://github.com/absinthe-graphql/dataloader/blob/main/lib/dataloader/ecto.ex)
- [Absinthe execution phases](https://hexdocs.pm/absinthe/Absinthe.Pipeline.html)
- [The N+1 problem, explained by Brooklyn Zelenka](https://dashbit.co/blog/writing-efficient-absinthe-queries)

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```
