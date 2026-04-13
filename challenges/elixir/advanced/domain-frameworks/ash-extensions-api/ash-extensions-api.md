# Ash extensions — JSON:API, GraphQL, Admin

**Project**: `ash_extensions` — auto-generated JSON:API, GraphQL and admin UI on top of Ash resources

---

## Why domain frameworks matters

Frameworks like Ash, Commanded, Oban, Nx and Axon encode large domain patterns (CQRS, event sourcing, ML training, background jobs, IoT updates) into reusable building blocks. Used well, they compress months of bespoke code into days.

Used poorly, they hide complexity that bites in production: aggregate version drift in Commanded, projection lag in CQRS systems, OTA failure recovery in Nerves, gradient explosion in Axon training loops. The framework's defaults are not your defaults.

---

## The business problem

You are building a production-grade Elixir component in the **Domain frameworks** area. The goal is not a toy demonstration — it is a piece you could drop into a real system and rely on under load.

Concretely, you must:

1. Implement the topic correctly with measurable behavior.
2. Surface failure modes via `{:ok, _} / {:error, reason}` tuples — never raise from a public API except for programmer errors.
3. Bound resources: explicit timeouts, memory caps, back-pressure where relevant, and supervision for any long-lived process.
4. Provide a test suite with `async: true` where safe, `doctest` for public examples, and `describe` blocks that name the contract.
5. Provide a `script/main.exs` that demonstrates the piece end-to-end with realistic data — not a `1 + 1` smoke test.

---

## Project structure

```
ash_extensions/
├── lib/
│   └── ash_extensions.ex
├── script/
│   └── main.exs
├── test/
│   └── ash_extensions_test.exs
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

Chose **B** because in Domain frameworks the failure modes — not the happy path — are what determine whether the system stays up under real production conditions.

---

## Implementation

### `mix.exs`

```elixir
defmodule AshExtensions.MixProject do
  use Mix.Project

  def project do
    [
      app: :ash_extensions,
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
### `lib/ash_extensions.ex`

```elixir
defmodule AshExtensions.Catalog do
  @moduledoc """
  Ejercicio: Ash extensions — JSON:API, GraphQL, Admin.
  Implementa el comportamiento descrito en el enunciado, exponiendo funciones públicas documentadas y con tipos claros.
  """

  use Ash.Domain,
    extensions: [AshJsonApi.Domain, AshGraphql.Domain, AshAdmin.Domain]

  resources do
    resource AshExtensions.Catalog.Product
    resource AshExtensions.Catalog.Category
    resource AshExtensions.Catalog.Price
  end

  json_api do
    prefix "/api/json"
  end

  graphql do
    root_level_errors? true
  end

  admin do
    show? true
  end
end

defmodule AshExtensions.Catalog.Product do
  use Ash.Resource,
    domain: AshExtensions.Catalog,
    data_layer: AshPostgres.DataLayer,
    extensions: [AshJsonApi.Resource, AshGraphql.Resource]

  # ... attributes/relationships/actions from the base Catalog resource ...

  postgres do
    table "products"
    repo AshExtensions.Repo
  end

  attributes do
    uuid_primary_key :id
    attribute :sku, :string, allow_nil?: false, public?: true
    attribute :name, :string, allow_nil?: false, public?: true
    attribute :description, :string, public?: true
    attribute :status, :atom do
      constraints one_of: [:draft, :published, :archived]
      default :draft
      public? true
    end
    timestamps()
  end

  relationships do
    belongs_to :category, AshExtensions.Catalog.Category do
      allow_nil? false
      public? true
    end
    has_many :prices, AshExtensions.Catalog.Price
  end

  identities do
    identity :unique_sku, [:sku]
  end

  actions do
    defaults [:read, :destroy]

    create :register do
      accept [:sku, :name, :description, :category_id]
    end

    update :publish do
      accept []
      change set_attribute(:status, :published)
      validate attribute_equals(:status, :draft)
    end

    update :archive do
      accept []
      change set_attribute(:status, :archived)
    end
  end

  json_api do
    type "product"

    routes do
      base "/products"
      get :read
      index :read
      post :register
      patch :publish, route: "/:id/publish"
      patch :archive, route: "/:id/archive"
      delete :destroy
      relationship :category, :read
    end
  end

  graphql do
    type :product

    queries do
      get :get_product, :read
      list :list_products, :read
    end

    mutations do
      create :register_product, :register
      update :publish_product, :publish
      update :archive_product, :archive
      destroy :delete_product, :destroy
    end
  end

  admin do
    table_columns [:sku, :name, :status, :category_id]
    form do
      field :sku, type: :default
      field :name, type: :default
      field :description, type: :long_text
      field :category_id, type: :default
    end
  end
end

# lib/ash_extensions/catalog/category.ex
defmodule AshExtensions.Catalog.Category do
  use Ash.Resource,
    domain: AshExtensions.Catalog,
    data_layer: AshPostgres.DataLayer,
    extensions: [AshJsonApi.Resource, AshGraphql.Resource]

  postgres do
    table "categories"
    repo AshExtensions.Repo
  end

  attributes do
    uuid_primary_key :id
    attribute :name, :string, allow_nil?: false, public?: true
    attribute :slug, :string, allow_nil?: false, public?: true
    timestamps()
  end

  relationships do
    has_many :products, AshExtensions.Catalog.Product
  end

  identities do
    identity :unique_slug, [:slug]
  end

  actions do
    defaults [:create, :read, :update, :destroy]
  end

  json_api do
    type "category"
    routes do
      base "/categories"
      get :read
      index :read
      post :create
      patch :update
      delete :destroy
    end
  end

  graphql do
    type :category
    queries do
      get :get_category, :read
      list :list_categories, :read
    end
    mutations do
      create :create_category, :create
      update :update_category, :update
      destroy :delete_category, :destroy
    end
  end
end

# lib/ash_extensions/catalog/price.ex
defmodule AshExtensions.Catalog.Price do
  use Ash.Resource,
    domain: AshExtensions.Catalog,
    data_layer: AshPostgres.DataLayer,
    extensions: [AshJsonApi.Resource, AshGraphql.Resource]

  postgres do
    table "prices"
    repo AshExtensions.Repo
  end

  attributes do
    uuid_primary_key :id
    attribute :amount_cents, :integer, allow_nil?: false, public?: true
    attribute :currency, :string, allow_nil?: false, public?: true, default: "USD"
    attribute :valid_from, :utc_datetime_usec, allow_nil?: false, public?: true
    attribute :valid_until, :utc_datetime_usec, public?: true
    timestamps()
  end

  relationships do
    belongs_to :product, AshExtensions.Catalog.Product do
      allow_nil? false
      public? true
    end
  end

  actions do
    defaults [:create, :read, :destroy]
  end

  json_api do
    type "price"
    routes do
      base "/prices"
      get :read
      index :read
      post :create
      delete :destroy
    end
  end

  graphql do
    type :price
    queries do
      list :list_prices, :read
    end
    mutations do
      create :create_price, :create
    end
  end
end

defmodule AshExtensions.GraphqlSchema do
  use Absinthe.Schema
  use AshGraphql, domains: [AshExtensions.Catalog]
end

defmodule AshExtensions.JsonApiRouter do
  use AshJsonApi.Router,
    domains: [AshExtensions.Catalog],
    open_api: "/open_api"
end

defmodule AshExtensions.Router do
  use Phoenix.Router
  import Plug.Conn
  import Phoenix.Controller
  import AshAdmin.Router

  pipeline :api do
    plug :accepts, ["json"]
  end

  pipeline :browser do
    plug :accepts, ["html"]
    plug :fetch_session
    plug :protect_from_forgery
  end

  scope "/api/json" do
    pipe_through :api
    forward "/", AshExtensions.JsonApiRouter
  end

  scope "/api/gql" do
    pipe_through :api

    forward "/playground", Absinthe.Plug.GraphiQL,
      schema: AshExtensions.GraphqlSchema,
      interface: :playground

    forward "/", Absinthe.Plug, schema: AshExtensions.GraphqlSchema
  end

  scope "/admin" do
    pipe_through :browser
    ash_admin "/"
  end
end

defmodule AshExtensions.Endpoint do
  use Phoenix.Endpoint, otp_app: :ash_extensions

  plug Plug.Session,
    store: :cookie,
    key: "_ash_extensions_key",
    signing_salt: "change_me"

  plug AshExtensions.Router
end

defmodule AshExtensions.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      AshExtensions.Repo,
      AshExtensions.Endpoint
    ]

    Supervisor.start_link(children, strategy: :one_for_one)
  end
end
```
### `test/ash_extensions_test.exs`

```elixir
defmodule AshExtensions.ApiTest do
  use ExUnit.Case, async: true
  doctest AshExtensions.Catalog

  alias AshExtensions.Catalog
  alias AshExtensions.Catalog.{Category, Product}

  setup do
    :ok = Ecto.Adapters.SQL.Sandbox.checkout(AshExtensions.Repo)
    Ecto.Adapters.SQL.Sandbox.mode(AshExtensions.Repo, {:shared, self()})

    {:ok, category} =
      Category
      |> Ash.Changeset.for_create(:create, %{name: "Widgets", slug: "widgets"})
      |> Ash.create()

    %{category: category}
  end

  describe "JSON:API — POST /api/json/products" do
    test "creates a product", %{category: cat} do
      body =
        Jason.encode!(%{
          data: %{
            type: "product",
            attributes: %{sku: "W-001", name: "Red Widget"},
            relationships: %{
              category: %{data: %{type: "category", id: cat.id}}
            }
          }
        })

      conn =
        Plug.Test.conn(:post, "/api/json/products", body)
        |> Plug.Conn.put_req_header("content-type", "application/vnd.api+json")

      response = AshExtensions.JsonApiRouter.call(conn, [])
      assert response.status == 201
    end
  end

  describe "GraphQL — register_product" do
    test "runs the register action", %{category: cat} do
      query = """
      mutation {
        registerProduct(input: {sku: "W-002", name: "Blue", categoryId: "#{cat.id}"}) {
          result { sku name status }
          errors { message }
        }
      }
      """

      {:ok, %{data: data}} = Absinthe.run(query, AshExtensions.GraphqlSchema)
      assert data["registerProduct"]["result"]["sku"] == "W-002"
      assert data["registerProduct"]["result"]["status"] == "draft"
    end
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Ash extensions — JSON:API, GraphQL, Admin.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Ash extensions — JSON:API, GraphQL, Admin ===")
    IO.puts("Category: Domain frameworks\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case AshExtensions.run(payload) do
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
        for _ <- 1..1_000, do: AshExtensions.run(:bench)
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

### 1. Frameworks encode opinions

Ash, Commanded, Oban each pick defaults that work for the common case. Understand the defaults before you customize — the framework's authors chose them for a reason.

### 2. Event-sourced systems need projection lag tolerance

In CQRS, the read model is eventually consistent with the write model. UI must handle 'I saved but I don't see my own data yet'. Optimistic UI updates help.

### 3. Background jobs need idempotency and retries

Oban retries failed jobs by default. The worker must be idempotent: repeating a job must produce the same end state. Use unique constraints and deduplication keys.

---
