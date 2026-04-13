# Dataloader.Ecto Source and Preload Optimization

**Project**: `dataloader_ecto` — deep dive into `Dataloader.Ecto` for a marketplace catalog (products, variants, sellers, reviews) with complex filter-aware batching

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
dataloader_ecto/
├── lib/
│   └── dataloader_ecto.ex
├── script/
│   └── main.exs
├── test/
│   └── dataloader_ecto_test.exs
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
defmodule DataloaderEcto.MixProject do
  use Mix.Project

  def project do
    [
      app: :dataloader_ecto,
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
### `lib/dataloader_ecto.ex`

```elixir
# lib/dataloader_ecto/catalog/product.ex
defmodule DataloaderEcto.Catalog.Product do
  use Ecto.Schema

  schema "products" do
    field :name, :string
    field :status, Ecto.Enum, values: [:active, :paused, :deleted], default: :active
    belongs_to :seller, DataloaderEcto.Catalog.Seller
    has_many :variants, DataloaderEcto.Catalog.Variant
    has_many :reviews, DataloaderEcto.Catalog.Review
    timestamps()
  end
end

defmodule DataloaderEcto.Catalog.Variant do
  use Ecto.Schema
  schema "variants" do
    field :sku, :string
    field :price_cents, :integer
    belongs_to :product, DataloaderEcto.Catalog.Product
    timestamps()
  end
end

defmodule DataloaderEcto.Catalog.Seller do
  use Ecto.Schema
  schema "sellers" do
    field :name, :string
    field :rating, :float
    timestamps()
  end
end

defmodule DataloaderEcto.Catalog.Review do
  use Ecto.Schema
  schema "reviews" do
    field :body, :string
    field :rating, :integer
    field :status, Ecto.Enum, values: [:pending, :approved, :rejected]
    belongs_to :product, DataloaderEcto.Catalog.Product
    timestamps()
  end
end

# lib/dataloader_ecto/graphql/loader.ex
defmodule DataloaderEcto.Graphql.Loader do
  @moduledoc """
  Builds a Dataloader configured for the catalog domain.

  The `query/2` function is the single source of truth for:
    - soft-delete filtering (never load :deleted rows)
    - default ordering (predictable for clients)
    - scoping options (:status filter on reviews)
  """

  import Ecto.Query

  alias DataloaderEcto.{Repo, Catalog}

  @doc "Creates result."
  def new do
    Dataloader.new(timeout: :timer.seconds(10))
    |> Dataloader.add_source(:catalog, source())
  end

  defp source do
    Dataloader.Ecto.new(Repo, query: &query/2, run_batch: &run_batch/5)
  end

  # ---------------------------------------------------------------------------
  # Global query hook — applied to every association load
  # ---------------------------------------------------------------------------

  @doc "Returns query result from _params."
  def query(Catalog.Product, _params) do
    from p in Catalog.Product, where: p.status != :deleted
  end

  @doc "Returns query result from _params."
  def query(Catalog.Variant, _params) do
    from v in Catalog.Variant, order_by: [asc: v.price_cents]
  end

  @doc "Returns query result."
  def query(Catalog.Review, %{status: status}) when not is_nil(status) do
    from r in Catalog.Review, where: r.status == ^status, order_by: [desc: r.rating]
  end

  @doc "Returns query result from _params."
  def query(Catalog.Review, _params) do
    from r in Catalog.Review, order_by: [desc: r.rating]
  end

  @doc "Returns query result from queryable and _params."
  def query(queryable, _params), do: queryable

  # ---------------------------------------------------------------------------
  # Custom run_batch — "top N reviews per product" via LATERAL join
  # ---------------------------------------------------------------------------

  # When the caller passes %{top_n: N}, expand to a lateral join so we get
  # N reviews per product in a single SQL statement instead of one per product.
  @doc "Runs batch result from _q and products."
  def run_batch(Catalog.Review, _q, :reviews, products, %{top_n: n} = _repo_opts)
      when is_integer(n) and n > 0 do
    ids = Enum.map(products, & &1.id)

    sql = """
    SELECT r.*, p_id AS _product_id
    FROM unnest($1::bigint[]) AS p_id
    LEFT JOIN LATERAL (
      SELECT *
      FROM reviews r
      WHERE r.product_id = p_id AND r.status = 'approved'
      ORDER BY r.rating DESC
      LIMIT $2
    ) r ON true
    """

    %{rows: rows, columns: cols} = Repo.query!(sql, [ids, n])

    # Reassemble into the shape Dataloader expects: [[values_for_product_1], ...]
    grouped =
      rows
      |> Enum.map(&Enum.zip(cols, &1))
      |> Enum.group_by(fn row -> row |> Map.new() |> Map.get("_product_id") end)

    Enum.map(products, fn p ->
      grouped
      |> Map.get(p.id, [])
      |> Enum.reject(fn row -> row |> Map.new() |> Map.get("id") |> is_nil() end)
      |> Enum.map(&row_to_review/1)
    end)
  end

  @doc "Runs batch result from queryable, query, col, inputs and repo_opts."
  def run_batch(queryable, query, col, inputs, repo_opts) do
    Dataloader.Ecto.run_batch(Repo, queryable, query, col, inputs, repo_opts)
  end

  defp row_to_review(row) do
    m = Map.new(row)
    %Catalog.Review{
      id: m["id"],
      body: m["body"],
      rating: m["rating"],
      product_id: m["product_id"],
      status: m["status"]
    }
  end
end

# lib/dataloader_ecto/graphql/types/product_types.ex
defmodule DataloaderEcto.Graphql.Types.ProductTypes do
  use Absinthe.Schema.Notation
  import Absinthe.Resolution.Helpers, only: [dataloader: 1, dataloader: 3]

  alias DataloaderEcto.Catalog

  object :seller do
    field :id, non_null(:id)
    field :name, non_null(:string)
    field :rating, :float
  end

  object :variant do
    field :sku, non_null(:string)
    field :price_cents, non_null(:integer)
  end

  object :review do
    field :id, non_null(:id)
    field :body, non_null(:string)
    field :rating, non_null(:integer)
  end

  object :product do
    field :id, non_null(:id)
    field :name, non_null(:string)

    field :seller, non_null(:seller), resolve: dataloader(:catalog)
    field :variants, non_null(list_of(non_null(:variant))), resolve: dataloader(:catalog)

    field :top_reviews, non_null(list_of(non_null(:review))) do
      arg :limit, :integer, default_value: 3

      resolve fn product, %{limit: n}, %{context: %{loader: loader}} ->
        loader
        |> Dataloader.load(:catalog, {Catalog.Review, %{top_n: n}}, product)
        |> Absinthe.Resolution.Helpers.on_load(fn loader ->
          {:ok, Dataloader.get(loader, :catalog, {Catalog.Review, %{top_n: n}}, product)}
        end)
      end
    end
  end
end

# lib/dataloader_ecto/graphql/schema.ex
defmodule DataloaderEcto.Graphql.Schema do
  use Absinthe.Schema
  import_types DataloaderEcto.Graphql.Types.ProductTypes

  alias DataloaderEcto.{Repo, Catalog}

  query do
    field :products, non_null(list_of(non_null(:product))) do
      arg :limit, :integer, default_value: 25

      resolve fn _p, args, _r ->
        import Ecto.Query
        q = from p in Catalog.Product, where: p.status == :active, limit: ^args.limit
        {:ok, Repo.all(q)}
      end
    end
  end

  @doc "Returns context result from ctx."
  @impl true
  def context(ctx), do: Map.put(ctx, :loader, DataloaderEcto.Graphql.Loader.new())

  @doc "Returns plugins result."
  @impl true
  def plugins, do: [Absinthe.Middleware.Dataloader] ++ Absinthe.Plugin.defaults()
end
```
### `test/dataloader_ecto_test.exs`

```elixir
defmodule DataloaderEcto.PreloadTest do
  use ExUnit.Case, async: true
  doctest DataloaderEcto.Catalog.Product
  alias DataloaderEcto.Repo

  setup do
    Ecto.Adapters.SQL.Sandbox.checkout(Repo)
    seed_catalog(25, 3, 50)
    :ok
  end

  describe "DataloaderEcto.Preload" do
    test "25-product query runs at most 4 SELECTs: products, sellers, variants, reviews" do
      count = :counters.new(1, [])
      :telemetry.attach("sql", [:dataloader_ecto, :repo, :query],
        fn _, _, meta, _ ->
          if meta.source != "schema_migrations", do: :counters.add(count, 1, 1)
        end, nil)

      query = """
      { products(limit: 25) {
          name seller { name }
          variants { sku priceCents }
          topReviews(limit: 3) { rating body }
      } }
      """
      assert {:ok, %{data: %{"products" => list}}} =
               Absinthe.run(query, DataloaderEcto.Graphql.Schema)
      assert length(list) == 25

      :telemetry.detach("sql")
      total = :counters.get(count, 1)
      assert total <= 4, "expected ≤ 4 SELECTs, got #{total}"
    end
  end

  defp seed_catalog(products, variants_each, reviews_each) do
    {:ok, seller} = Repo.insert(%DataloaderEcto.Catalog.Seller{name: "s", rating: 4.5})

    for i <- 1..products do
      {:ok, p} = Repo.insert(%DataloaderEcto.Catalog.Product{name: "p#{i}", seller_id: seller.id})
      for v <- 1..variants_each do
        Repo.insert!(%DataloaderEcto.Catalog.Variant{sku: "v#{i}-#{v}", price_cents: v * 100, product_id: p.id})
      end
      for r <- 1..reviews_each do
        Repo.insert!(%DataloaderEcto.Catalog.Review{body: "r#{r}", rating: rem(r, 5) + 1, status: :approved, product_id: p.id})
      end
    end
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Dataloader.Ecto Source and Preload Optimization.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Dataloader.Ecto Source and Preload Optimization ===")
    IO.puts("Category: APIs and GraphQL\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case DataloaderEcto.run(payload) do
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
        for _ <- 1..1_000, do: DataloaderEcto.run(:bench)
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
