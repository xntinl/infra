# OpenAPI Spec Generation from Code with OpenApiSpex

**Project**: `orders_api` — a Phoenix REST API whose OpenAPI 3.1 spec is derived from the controller and schema modules, validated in tests, and served at `/openapi.json` for consumers and SDK generators.

## Project context

Your public API ships with hand-written OpenAPI YAML that drifts from reality within weeks. Every release someone forgets to update a parameter name; SDK-generating consumers chase phantom bugs. The objective: make the spec a compiler-derived artifact so that if the code changes, the spec changes, and if they disagree, CI fails.

`OpenApiSpex` is an Elixir library that treats each controller action as a declarative operation and each `Ecto` or plain struct as a schema. It builds an OpenAPI 3.1 document at runtime, exposes it over HTTP, and provides a plug that **validates every request against that spec**, rejecting malformed input with a standard 422.

```
orders_api/
├── lib/
│   ├── orders_api/
│   │   └── orders.ex
│   └── orders_api_web/
│       ├── endpoint.ex
│       ├── router.ex
│       ├── api_spec.ex                 # top-level OpenApi struct
│       ├── schemas/
│       │   ├── order.ex
│       │   └── error.ex
│       └── controllers/
│           └── order_controller.ex     # uses OpenApiSpex.operation/2
├── test/orders_api_web/
│   ├── api_spec_test.exs
│   └── order_controller_test.exs
└── mix.exs
```

## Why OpenApiSpex and not hand-written YAML

Hand-written specs drift because nothing enforces them. OpenApiSpex inverts the flow:

1. Each controller declares `operation :show, ...` next to the action.
2. Each response or request body references an `OpenApiSpex.Schema` struct.
3. `OpenApiSpex.cast_and_validate/4` is a plug: if a client posts the wrong shape, the plug rejects before the action runs.
4. A test `assert_schema` verifies that **the response** also matches the declared schema.

Result: the spec is code. Refactoring the controller without updating the operation breaks tests.

## Why derive from code and not from reflection

A pure-reflection approach ("introspect the Ecto schema, generate spec") sounds elegant but loses nuance: nullable fields, read-only fields, example values, response codes. The `operation` macro is explicit and keeps each endpoint's contract where the code lives.

## Core concepts

### 1. `ApiSpec` module is the root document
Contains servers, info, paths (auto-collected from the router), components (named schemas).

### 2. `operation/2` macro annotates actions
Declares summary, parameters, request body, responses, and security — all pointing at schema modules.

### 3. `OpenApiSpex.Plug.CastAndValidate`
Plug that validates the **incoming** request against the operation spec. Pairs with `assert_schema` in tests to verify **outgoing** shape.

## Design decisions

- **Option A — one `Schema` module per DTO**: pros: reusable across operations; cons: more files.
- **Option B — inline schemas in the operation macro**: pros: locality; cons: duplicated across endpoints.
→ We pick **A** for anything used more than once. Error schemas, pagination envelopes, and core resources ALWAYS live in their own module.

## Implementation

### Dependencies

```elixir
defp deps do
  [
    {:phoenix, "~> 1.7"},
    {:open_api_spex, "~> 3.21"},
    {:jason, "~> 1.4"}
  ]
end
```

### Step 1: Root spec module

**Objective**: Build the `%OpenApi{}` from `Paths.from_router/1` + `resolve_schema_modules/1` so one function call derives the whole contract from code.

```elixir
defmodule OrdersApiWeb.ApiSpec do
  alias OpenApiSpex.{Info, OpenApi, Paths, Server, Components, SecurityScheme}
  @behaviour OpenApi

  @impl true
  def spec do
    %OpenApi{
      openapi: "3.1.0",
      info: %Info{title: "Orders API", version: "1.0.0"},
      servers: [%Server{url: "https://api.example.com"}],
      paths: Paths.from_router(OrdersApiWeb.Router),
      components: %Components{
        securitySchemes: %{"bearerAuth" => %SecurityScheme{type: "http", scheme: "bearer"}}
      },
      security: [%{"bearerAuth" => []}]
    }
    |> OpenApiSpex.resolve_schema_modules()
  end
end
```

### Step 2: Reusable schemas

**Objective**: Declare Order/OrderCreate/Error as `OpenApiSpex.schema` modules so examples, enums, and UUID formats ride along with every operation that references them.

```elixir
defmodule OrdersApiWeb.Schemas.Order do
  alias OpenApiSpex.Schema
  require OpenApiSpex

  OpenApiSpex.schema(%{
    title: "Order",
    type: :object,
    required: [:id, :customer_id, :total_cents, :status],
    properties: %{
      id: %Schema{type: :string, format: :uuid},
      customer_id: %Schema{type: :string},
      total_cents: %Schema{type: :integer, minimum: 0},
      status: %Schema{type: :string, enum: ~w(pending paid shipped cancelled)},
      created_at: %Schema{type: :string, format: :"date-time"}
    },
    example: %{
      "id" => "550e8400-e29b-41d4-a716-446655440000",
      "customer_id" => "cus_1",
      "total_cents" => 4999,
      "status" => "paid",
      "created_at" => "2026-04-12T10:00:00Z"
    }
  })
end

defmodule OrdersApiWeb.Schemas.OrderCreate do
  alias OpenApiSpex.Schema
  require OpenApiSpex

  OpenApiSpex.schema(%{
    title: "OrderCreate",
    type: :object,
    required: [:customer_id, :total_cents],
    properties: %{
      customer_id: %Schema{type: :string},
      total_cents: %Schema{type: :integer, minimum: 1}
    }
  })
end

defmodule OrdersApiWeb.Schemas.Error do
  alias OpenApiSpex.Schema
  require OpenApiSpex

  OpenApiSpex.schema(%{
    title: "Error",
    type: :object,
    required: [:code, :message],
    properties: %{
      code: %Schema{type: :string},
      message: %Schema{type: :string},
      details: %Schema{type: :object, additionalProperties: true}
    }
  })
end
```

### Step 3: Annotated controller

**Objective**: Declare `operation/2` on each action so the spec is generated from handlers and `replace_params: true` casts inputs into typed structs before business logic runs.

```elixir
defmodule OrdersApiWeb.OrderController do
  use OrdersApiWeb, :controller
  use OpenApiSpex.ControllerSpecs

  alias OrdersApi.Orders
  alias OrdersApiWeb.Schemas.{Order, OrderCreate, Error}

  tags ["orders"]
  security [%{"bearerAuth" => []}]

  operation :show,
    summary: "Get an order by ID",
    parameters: [id: [in: :path, type: :string, required: true]],
    responses: [
      ok: {"the order", "application/json", Order},
      not_found: {"not found", "application/json", Error}
    ]

  def show(conn, %{id: id}) do
    case Orders.fetch(id) do
      {:ok, order} -> json(conn, order)
      :error -> conn |> put_status(404) |> json(%{code: "not_found", message: "Order #{id} not found"})
    end
  end

  operation :create,
    summary: "Create an order",
    request_body: {"order create payload", "application/json", OrderCreate},
    responses: [
      created: {"created", "application/json", Order},
      unprocessable_entity: {"invalid payload", "application/json", Error}
    ]

  def create(conn, %OrderCreate{} = body) do
    {:ok, order} = Orders.create(body)
    conn |> put_status(:created) |> json(order)
  end
end
```

Note that the `create` action receives the **cast & validated** struct as its `params` — `OpenApiSpex` replaced the raw map.

### Step 4: Router mounts spec, validator, and docs UI

**Objective**: Pipeline `PutApiSpec` + `CastAndValidate` and expose `/openapi.json` and `/docs` so contract, validator, and UI share the single source of truth.

```elixir
defmodule OrdersApiWeb.Router do
  use OrdersApiWeb, :router

  pipeline :api do
    plug :accepts, ["json"]
    plug OpenApiSpex.Plug.PutApiSpec, module: OrdersApiWeb.ApiSpec
    plug OpenApiSpex.Plug.CastAndValidate, replace_params: true
  end

  scope "/api" do
    pipe_through :api
    resources "/orders", OrdersApiWeb.OrderController, only: [:show, :create]
  end

  scope "/" do
    get "/openapi.json", OpenApiSpex.Plug.RenderSpec, []
    get "/docs", OpenApiSpex.Plug.SwaggerUI, path: "/openapi.json"
  end
end
```

## Why this works

```
    request ─▶ PutApiSpec ─▶ CastAndValidate ─▶ controller action
                    │               │                    │
                    │               └─ validates params against operation spec
                    │                  · rejects with 422 + Error schema on mismatch
                    │                  · mutates conn.params to a typed struct
                    ▼
              spec available at conn.private[:open_api_spex]
                    │
                    ▼
              /openapi.json renders it on demand
```

Because the validator runs before the action, the action NEVER sees malformed input. And because `/openapi.json` is derived from the same `operation` declarations, the public spec cannot lie.

## Tests

```elixir
defmodule OrdersApiWeb.ApiSpecTest do
  use ExUnit.Case, async: true

  describe "OpenAPI document" do
    test "resolves without errors" do
      spec = OrdersApiWeb.ApiSpec.spec()
      assert spec.openapi == "3.1.0"
      assert Map.has_key?(spec.paths, "/api/orders")
    end

    test "is valid JSON" do
      spec = OrdersApiWeb.ApiSpec.spec()
      assert {:ok, _} = Jason.encode(spec)
    end
  end
end

defmodule OrdersApiWeb.OrderControllerTest do
  use OrdersApiWeb.ConnCase, async: true
  import OpenApiSpex.TestAssertions

  @api_spec OrdersApiWeb.ApiSpec.spec()

  describe "GET /api/orders/:id" do
    test "returns Order schema on success", %{conn: conn} do
      resp = conn |> get("/api/orders/known") |> json_response(200)
      assert_schema(resp, "Order", @api_spec)
    end

    test "returns Error schema on miss", %{conn: conn} do
      resp = conn |> get("/api/orders/missing") |> json_response(404)
      assert_schema(resp, "Error", @api_spec)
    end
  end

  describe "POST /api/orders" do
    test "rejects malformed body with 422", %{conn: conn} do
      resp = conn |> post("/api/orders", %{total_cents: -1}) |> json_response(422)
      assert %{"errors" => _} = resp
    end

    test "creates and returns Order on valid body", %{conn: conn} do
      body = %{customer_id: "cus_1", total_cents: 100}
      resp = conn |> post("/api/orders", body) |> json_response(201)
      assert_schema(resp, "Order", @api_spec)
    end
  end
end
```

## Benchmark

Validation overhead per request:

```elixir
# bench/cast_validate.exs
spec = OrdersApiWeb.ApiSpec.spec()
payload = %{"customer_id" => "cus_1", "total_cents" => 100}

Benchee.run(%{
  "cast + validate OrderCreate" => fn ->
    OpenApiSpex.cast_value(payload, OrdersApiWeb.Schemas.OrderCreate.schema(), spec)
  end
})
```

**Expected**: < 100 µs per cast on a small schema.

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

**1. `resolve_schema_modules/1` is mandatory**
Without it, `$ref` pointers in the final JSON point at atoms instead of components. `/openapi.json` renders but tools reject it.

**2. `replace_params: true` changes your controller signature**
When true, actions receive typed structs. If you had pattern matches on plain maps, they break. Leave it `false` if you need gradual adoption.

**3. Enum drift**
When you add a new `status`, update the `enum` in the `Order` schema. Tests pass locally but downstream SDKs reject the new value because they were generated from stale spec.

**4. Large spec at boot**
`ApiSpec.spec/0` is built on every `/openapi.json` request. For hundreds of endpoints, memoize with `:persistent_term` or cache as a compiled module.

**5. Security definitions missing**
If your operation declares `security [%{"bearerAuth" => []}]` but you forgot to add the `securitySchemes` in components, the spec validates but Swagger UI cannot show the "Authorize" button.

**6. When NOT to use OpenApiSpex**
For internal RPC-shaped APIs used by two services you control, the ceremony outweighs the benefit. Prefer it for public APIs, mobile BFFs, and anywhere you generate SDKs.

## Reflection

Your spec is now source of truth. A product manager pushes to add a field `discount_code` to the order payload. You add it to the `OrderCreate` schema as optional — but half the mobile clients in the wild send the old payload. Does the validator accept them? What about if you add the field as `required: true`? Design the migration path.

## Resources

- [OpenApiSpex hexdocs](https://hexdocs.pm/open_api_spex/)
- [OpenAPI 3.1 specification](https://spec.openapis.org/oas/v3.1.0)
- [`OpenApiSpex.Plug.CastAndValidate`](https://hexdocs.pm/open_api_spex/OpenApiSpex.Plug.CastAndValidate.html)
- [JSON Schema validation draft 2020-12](https://json-schema.org/draft/2020-12/json-schema-core.html)

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```
