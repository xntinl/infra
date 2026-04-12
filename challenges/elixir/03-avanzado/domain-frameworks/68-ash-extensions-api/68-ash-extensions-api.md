# Ash extensions — JSON:API, GraphQL, Admin

**Project**: `ash_extensions` — auto-generated JSON:API, GraphQL and admin UI on top of Ash resources.

---

## Project context

You built the `Catalog` domain with Ash resources (exercise 67). Product managers now want a REST API for external partners (stable, cacheable, JSON:API-compatible), a GraphQL endpoint for the mobile app (flexible, single-request), and an internal admin UI for the ops team to manage the catalog without shipping frontend code.

Hand-writing each of these from scratch is weeks of work and triples the surface where bugs hide. Ash ships three first-party extensions that derive these APIs from the resource definitions:

- [`AshJsonApi`](https://hexdocs.pm/ash_json_api/) — JSON:API 1.0 REST routes (Plug-compatible, mount in Phoenix).
- [`AshGraphql`](https://hexdocs.pm/ash_graphql/) — Absinthe schema generation from resources.
- [`AshAdmin`](https://hexdocs.pm/ash_admin/) — LiveView admin UI for CRUD, forms and action invocation.

In this exercise you wire all three onto the `Catalog` domain from the previous exercise. You will learn how one declarative resource turns into three consumer-facing APIs — and what the trade-offs of auto-generation are in production.

```
ash_extensions/
├── lib/
│   └── ash_extensions/
│       ├── application.ex
│       ├── endpoint.ex               # Phoenix endpoint
│       ├── router.ex                 # mounts json_api / graphql / admin
│       ├── catalog/
│       │   ├── catalog.ex            # extends domain with api extensions
│       │   ├── product.ex            # adds json_api + graphql blocks
│       │   ├── category.ex
│       │   └── price.ex
│       └── graphql_schema.ex
├── test/
│   └── api_test.exs
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

### 1. Extensions sit on resources, routes sit on domains

Each extension has two parts:

- A **resource-level DSL block** (`json_api do ... end`, `graphql do ... end`) declaring endpoints, field names, filters.
- A **domain-level config** listing which resources are exposed and under what base path.

Mount order: the domain's router macro emits a Plug that knows about every public action of every resource, based on the resource-level declarations.

```
   resource Product  ──┐
                        │
   resource Category ──┼──▶ Ash.Domain ──▶ AshJsonApi.Router → /api/json
                        │                ──▶ AshGraphql.Schema → /api/gql
   resource Price   ──┘                ──▶ AshAdmin.Router → /admin
```

### 2. JSON:API — convention-over-configuration REST

`AshJsonApi` produces `/products`, `/products/:id`, `/products/:id/relationships/category`, filtering via `?filter[status]=draft`, pagination via `?page[limit]=20`, sparse fieldsets via `?fields[product]=sku,name`. The spec is strict — use it and your clients get standardized behaviour for free.

### 3. GraphQL — resources become types, actions become fields

```elixir
graphql do
  type :product
  queries do
    get :get_product, :read
    list :list_products, :read
  end
  mutations do
    create :register_product, :register
    update :publish_product, :publish
  end
end
```

Each `list_products` query supports filtering by any public attribute, relationship loading via the GraphQL selection set, and pagination via `first:`/`after:` (Relay-style cursors).

### 4. Admin UI — derived forms with zero frontend code

`AshAdmin` scans the domain and renders a LiveView with:

- A resource list on the left.
- A table + create/edit forms derived from attribute types.
- Invocable custom actions (e.g., "Publish", "Archive") as buttons.
- Relationship editors (searchable selects driven by `read` actions).

It is opinionated. For a bespoke internal UI, you will still write LiveView. For the 90% case ("let the ops team do CRUD without deploying frontend"), it is enough.

### 5. All three share the same policies and validations

Because the extensions all go through the Ash action pipeline, authorization and validation rules you wrote in exercise 67 apply uniformly. There is no "REST-only" bypass.

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

### Step 1: `mix.exs`

```elixir
defmodule AshExtensions.MixProject do
  use Mix.Project

  def project do
    [app: :ash_extensions, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application do
    [extra_applications: [:logger], mod: {AshExtensions.Application, []}]
  end

  defp deps do
    [
      {:ash, "~> 3.0"},
      {:ash_postgres, "~> 2.0"},
      {:ash_json_api, "~> 1.4"},
      {:ash_graphql, "~> 1.3"},
      {:ash_admin, "~> 0.11"},
      {:ash_phoenix, "~> 2.0"},
      {:absinthe_plug, "~> 1.5"},
      {:phoenix, "~> 1.7"},
      {:phoenix_live_view, "~> 0.20"},
      {:plug_cowboy, "~> 2.6"},
      {:jason, "~> 1.4"}
    ]
  end
end
```

### Step 2: Extend the domain — `lib/ash_extensions/catalog/catalog.ex`

```elixir
defmodule AshExtensions.Catalog do
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
```

### Step 3: Extend the Product resource

```elixir
defmodule AshExtensions.Catalog.Product do
  use Ash.Resource,
    domain: AshExtensions.Catalog,
    data_layer: AshPostgres.DataLayer,
    extensions: [AshJsonApi.Resource, AshGraphql.Resource]

  # ... attributes/relationships/actions from exercise 67 ...

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
```

### Step 4: Category and Price — same pattern

```elixir
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
```

### Step 5: GraphQL schema — `lib/ash_extensions/graphql_schema.ex`

```elixir
defmodule AshExtensions.GraphqlSchema do
  use Absinthe.Schema
  use AshGraphql, domains: [AshExtensions.Catalog]
end
```

### Step 6: JSON:API router module

```elixir
defmodule AshExtensions.JsonApiRouter do
  use AshJsonApi.Router,
    domains: [AshExtensions.Catalog],
    open_api: "/open_api"
end
```

### Step 7: Phoenix router — `lib/ash_extensions/router.ex`

```elixir
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
```

### Step 8: Endpoint + Application

```elixir
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

### Step 9: Tests — `test/api_test.exs`

```elixir
defmodule AshExtensions.ApiTest do
  use ExUnit.Case, async: false

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

### Step 10: Run

```bash
mix deps.get
mix ash_postgres.generate_migrations --name api_initial
mix ash_postgres.migrate
mix test
iex -S mix phx.server
# visit http://localhost:4000/admin
# visit http://localhost:4000/api/gql/playground
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

**1. Auto-generated APIs expose your internals**
Every public attribute becomes visible via at least one API. Auditing "what can an external partner see?" requires reading the resource, not the controller. Use `public?: false` aggressively on fields you do not want exposed.

**2. Filter surface can be larger than expected**
AshJsonApi exposes filter operators on every attribute (`eq`, `in`, `gt`, `lt`, `like`) unless you restrict via the `filterable?` attribute on policies. A naive deployment lets anyone scan your table with `?filter[price][lt]=...`. Lock down filter shapes for untrusted clients.

**3. GraphQL depth attacks**
Without depth/complexity limits, an attacker issues `product { category { products { category { products ... }}}}` and hammers your DB. Configure Absinthe's `Absinthe.Complexity` plug and reject queries above a threshold.

**4. AshAdmin is not a public surface**
It has minimal auth by default. Put it behind your session auth and a role check — never expose it to the internet. Use `ash_authentication` or a Plug guard.

**5. Breaking GraphQL schema changes are silent**
Renaming a field on a resource renames the GraphQL field. Mobile clients pinned to the old field break. Introduce a deprecation path: keep the old field, add the new one, migrate clients, remove later.

**6. JSON:API + custom routes — all or nothing**
The JSON:API spec is strict. Once you expose `POST /products/:id/publish`, you're outside strict JSON:API for that endpoint. Either embrace sub-resources (`/products/:id/relationships/status`) or accept that some actions are pragmatic REST.

**7. Admin forms are opinionated, not bespoke**
A custom workflow (multi-step form, preview of an action's effect before confirming) does not fit AshAdmin's single-form-per-action model. For those cases build a custom LiveView and keep Admin for the CRUD majority.

**8. When NOT to use Ash extensions**
If the only consumer is one LiveView app, AshAdmin and AshGraphql are overkill. Plain Phoenix + Ash code interface is simpler and faster. Extensions earn their weight when you have ≥2 consumers of different shapes.

---

## Performance notes

Auto-generated APIs have three overhead layers:

| Layer | Typical cost | Mitigation |
|-------|--------------|------------|
| Plug parsing + body decode | 0.2–1 ms | Keep payloads small, use HTTP/2 |
| Ash action pipeline (policies, changes, validations) | 0.5–2 ms | Precompile policies, avoid runtime closures |
| Underlying Ecto query | dominates (5–100 ms) | Indexes, preloads, proper `load:` lists |

GraphQL query complexity matters more than raw request/response time: a single GraphQL mutation with a deep selection may fire 5 SQL queries where the equivalent REST sequence would fire 10 round-trips. That is the main perf win.

Benchmark a representative workload with [Wrk](https://github.com/wg/wrk) against `/api/json/products` and `/api/gql`. Expect 1–3k rps per core with warm caches; Ecto preloads dominate beyond that.

---

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?

## Resources

- [AshJsonApi hexdocs](https://hexdocs.pm/ash_json_api/) — full DSL
- [AshGraphql hexdocs](https://hexdocs.pm/ash_graphql/) — full DSL
- [AshAdmin hexdocs](https://hexdocs.pm/ash_admin/) — setup guide
- [JSON:API specification](https://jsonapi.org/format/) — know this before exposing REST routes
- [Absinthe.Complexity docs](https://hexdocs.pm/absinthe/complexity-analysis.html) — GraphQL safety net
- [Zach Daniel — "Ash 3.0 overview"](https://www.youtube.com/results?search_query=ash+3.0+zach+daniel) — creator's talks
- [Ash + Phoenix production guide](https://hexdocs.pm/ash_phoenix/) — recommended integration patterns
