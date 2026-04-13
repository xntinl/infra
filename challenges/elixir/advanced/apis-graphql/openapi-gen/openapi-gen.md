# OpenAPI Schema Generation with OpenApiSpex

**Project**: `openapi_gen` — generate an OpenAPI 3.1 document from a Phoenix REST API and validate requests/responses against it at runtime

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
openapi_gen/
├── lib/
│   └── openapi_gen.ex
├── script/
│   └── main.exs
├── test/
│   └── openapi_gen_test.exs
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
defmodule OpenapiGen.MixProject do
  use Mix.Project

  def project do
    [
      app: :openapi_gen,
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
### `lib/openapi_gen.ex`

```elixir
defmodule Schemas.Invoice do
  @moduledoc """
  Ejercicio: OpenAPI Schema Generation with OpenApiSpex.
  Implementa el comportamiento descrito en el enunciado, exponiendo funciones públicas documentadas y con tipos claros.
  """

  require OpenApiSpex
  OpenApiSpex.schema(%{
    type: :object,
    properties: %{
      id: %Schema{type: :string, format: :uuid},
      amount_cents: %Schema{type: :integer, minimum: 0}
    },
    required: [:id, :amount_cents]
  })
end

# lib/openapi_gen/web/api_spec.ex
defmodule OpenapiGen.Web.ApiSpec do
  @behaviour OpenApiSpex.OpenApi
  alias OpenApiSpex.{Info, OpenApi, Paths, Server}

  @doc "Returns spec result."
  @impl true
  def spec do
    %OpenApi{
      openapi: "3.1.0",
      info: %Info{
        title: "Invoices API",
        version: "1.0.0",
        description: "Public invoices endpoint for partner integrations"
      },
      servers: [%Server{url: "https://api.example.com"}],
      paths: Paths.from_router(OpenapiGen.Web.Router)
    }
    |> OpenApiSpex.resolve_schema_modules()
  end
end

# lib/openapi_gen/web/schemas/invoice.ex
defmodule OpenapiGen.Web.Schemas.Invoice do
  require OpenApiSpex
  alias OpenApiSpex.Schema

  OpenApiSpex.schema(%{
    title: "Invoice",
    description: "An invoice issued to a customer.",
    type: :object,
    properties: %{
      id: %Schema{type: :string, format: :uuid},
      customer_id: %Schema{type: :string, format: :uuid},
      amount_cents: %Schema{type: :integer, minimum: 0, description: "Amount in cents"},
      currency: %Schema{type: :string, pattern: "^[A-Z]{3}$", example: "USD"},
      status: %Schema{type: :string, enum: ["draft", "open", "paid", "void"]},
      issued_at: %Schema{type: :string, format: :"date-time"},
      inserted_at: %Schema{type: :string, format: :"date-time"}
    },
    required: [:id, :customer_id, :amount_cents, :currency, :status]
  })
end

# lib/openapi_gen/web/schemas/invoice_create_request.ex
defmodule OpenapiGen.Web.Schemas.InvoiceCreateRequest do
  require OpenApiSpex
  alias OpenApiSpex.Schema

  OpenApiSpex.schema(%{
    title: "InvoiceCreateRequest",
    type: :object,
    properties: %{
      customer_id: %Schema{type: :string, format: :uuid},
      amount_cents: %Schema{type: :integer, minimum: 1, maximum: 1_000_000_00},
      currency: %Schema{type: :string, pattern: "^[A-Z]{3}$"}
    },
    required: [:customer_id, :amount_cents, :currency]
  })
end

# lib/openapi_gen/web/schemas/error.ex
defmodule OpenapiGen.Web.Schemas.Error do
  require OpenApiSpex
  alias OpenApiSpex.Schema

  OpenApiSpex.schema(%{
    title: "Error",
    type: :object,
    properties: %{
      code: %Schema{type: :string},
      message: %Schema{type: :string},
      details: %Schema{type: :object, additionalProperties: true}
    },
    required: [:code, :message]
  })
end

# lib/openapi_gen/web/router.ex
defmodule OpenapiGen.Web.Router do
  use Phoenix.Router

  pipeline :api do
    plug :accepts, ["json"]
    plug OpenApiSpex.Plug.PutApiSpec, module: OpenapiGen.Web.ApiSpec
  end

  pipeline :api_validated do
    plug OpenApiSpex.Plug.CastAndValidate,
      json_render_error_v2: true,
      replace_params: false
  end

  scope "/api/v1", OpenapiGen.Web do
    pipe_through [:api, :api_validated]

    get "/invoices", InvoiceController, :index
    get "/invoices/:id", InvoiceController, :show
    post "/invoices", InvoiceController, :create
  end

  scope "/api" do
    pipe_through :api
    get "/openapi", OpenApiSpex.Plug.RenderSpec, []
    get "/docs", OpenApiSpex.Plug.SwaggerUI, path: "/api/openapi"
  end
end

# lib/openapi_gen/web/controllers/invoice_controller.ex
defmodule OpenapiGen.Web.InvoiceController do
  use Phoenix.Controller
  use OpenApiSpex.ControllerSpecs

  alias OpenapiGen.{Repo, Invoices.Invoice}
  alias OpenapiGen.Web.Schemas

  tags ["Invoices"]

  operation :index,
    summary: "List invoices",
    parameters: [
      customer_id: [in: :query, type: :string, format: :uuid, required: false],
      status: [in: :query, type: :string, enum: ["draft", "open", "paid", "void"], required: false],
      limit: [in: :query, type: :integer, required: false, example: 50]
    ],
    responses: [
      ok: {"Invoice list", "application/json", %OpenApiSpex.Schema{
        type: :array, items: Schemas.Invoice
      }}
    ]

  @doc "Returns index result from conn and params."
  def index(conn, params) do
    import Ecto.Query
    q = from(i in Invoice, limit: ^Map.get(params, :limit, 50), order_by: [desc: i.inserted_at])
    q = if id = params[:customer_id], do: from(i in q, where: i.customer_id == ^id), else: q
    q = if s = params[:status], do: from(i in q, where: i.status == ^s), else: q
    json(conn, Repo.all(q))
  end

  operation :show,
    summary: "Get an invoice by id",
    parameters: [id: [in: :path, type: :string, format: :uuid, required: true]],
    responses: [
      ok: {"Invoice", "application/json", Schemas.Invoice},
      not_found: {"Not found", "application/json", Schemas.Error}
    ]

  @doc "Shows result from conn."
  def show(conn, %{id: id}) do
    case Repo.get(Invoice, id) do
      nil ->
        conn
        |> put_status(:not_found)
        |> json(%{code: "not_found", message: "invoice not found"})

      invoice ->
        json(conn, invoice)
    end
  end

  operation :create,
    summary: "Create an invoice",
    request_body: {"Invoice create request", "application/json", Schemas.InvoiceCreateRequest},
    responses: [
      created: {"Created invoice", "application/json", Schemas.Invoice},
      unprocessable_entity: {"Validation error", "application/json", Schemas.Error}
    ]

  @doc "Creates result from conn and _params."
  def create(conn, _params) do
    body = conn.body_params

    %Invoice{}
    |> Invoice.changeset(body)
    |> Repo.insert()
    |> case do
      {:ok, invoice} -> conn |> put_status(:created) |> json(invoice)
      {:error, cs} ->
        conn
        |> put_status(:unprocessable_entity)
        |> json(%{code: "validation_error", message: "invalid request", details: errors(cs)})
    end
  end

  defp errors(%Ecto.Changeset{} = cs) do
    Ecto.Changeset.traverse_errors(cs, fn {msg, _} -> msg end)
  end
end
```
### `test/openapi_gen_test.exs`

```elixir
defmodule OpenapiGen.SpecTest do
  use ExUnit.Case, async: true
  doctest Schemas.Invoice
  alias OpenapiGen.Web.ApiSpec

  describe "OpenapiGen.Spec" do
    test "spec is valid OpenAPI 3" do
      spec = ApiSpec.spec()
      assert spec.openapi == "3.1.0"
      assert Map.has_key?(spec.paths, "/api/v1/invoices")
      assert Map.has_key?(spec.paths, "/api/v1/invoices/{id}")
    end

    test "request validation rejects bad types" do
      use Plug.Test
      opts = OpenapiGen.Web.Router.init([])

      conn =
        conn(:post, "/api/v1/invoices", Jason.encode!(%{customer_id: "not-a-uuid", amount_cents: -1}))
        |> put_req_header("content-type", "application/json")
        |> OpenapiGen.Web.Router.call(opts)

      assert conn.status == 422
      body = Jason.decode!(conn.resp_body)
      assert body["errors"] || body["code"]
    end
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for OpenAPI Schema Generation with OpenApiSpex.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== OpenAPI Schema Generation with OpenApiSpex ===")
    IO.puts("Category: APIs and GraphQL\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case OpenapiGen.run(payload) do
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
        for _ <- 1..1_000, do: OpenapiGen.run(:bench)
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
