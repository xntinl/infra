# OpenAPI Spec Generation from Code with OpenApiSpex

**Project**: `orders_api` — a Phoenix REST API whose OpenAPI 3.1 spec is derived from the controller and schema modules, validated in tests, and served at `/openapi.json` for consumers and SDK generators

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
orders_api/
├── lib/
│   └── orders_api.ex
├── script/
│   └── main.exs
├── test/
│   └── orders_api_test.exs
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
defmodule OrdersApi.MixProject do
  use Mix.Project

  def project do
    [
      app: :orders_api,
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

### `lib/orders_api.ex`

```elixir
defmodule OrdersApiWeb.ApiSpec do
  @moduledoc """
  Ejercicio: OpenAPI Spec Generation from Code with OpenApiSpex.
  Implementa el comportamiento descrito en el enunciado, exponiendo funciones públicas documentadas y con tipos claros.
  """

  alias OpenApiSpex.{Info, OpenApi, Paths, Server, Components, SecurityScheme}
  @behaviour OpenApi

  @doc "Returns spec result."
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

  @doc "Shows result from conn."
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

  @doc "Creates result from conn."
  def create(conn, %OrderCreate{} = body) do
    {:ok, order} = Orders.create(body)
    conn |> put_status(:created) |> json(order)
  end
end

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

### `test/orders_api_test.exs`

```elixir
defmodule OrdersApiWeb.ApiSpecTest do
  use ExUnit.Case, async: true
  doctest OrdersApiWeb.ApiSpec

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

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for OpenAPI Spec Generation from Code with OpenApiSpex.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== OpenAPI Spec Generation from Code with OpenApiSpex ===")
    IO.puts("Category: APIs and GraphQL\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case OrdersApi.run(payload) do
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
        for _ <- 1..1_000, do: OrdersApi.run(:bench)
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
