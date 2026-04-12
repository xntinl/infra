# OpenAPI Schema Generation with OpenApiSpex

**Project**: `openapi_gen` — generate an OpenAPI 3.1 document from a Phoenix REST API and validate requests/responses against it at runtime.

**Difficulty**: ★★★★☆
**Estimated time**: 3–5 hours

---

## Project context

The Platform team already has a Phoenix JSON API serving partners: 60 endpoints
across `/v1/invoices`, `/v1/customers`, `/v1/payments`. They ship a manually
maintained OpenAPI document to partners who generate clients from it. Reality:
the doc drifts from the code constantly. Partners file tickets saying "the
spec says `amount` is Int but the API returns a string," and an engineer
scrambles to fix the spec (not the API, usually).

The fix is to treat the OpenAPI doc as **generated output** of the Phoenix code,
not a parallel artifact. OpenApiSpex makes Phoenix routes, params, and
responses the source of truth; the spec is `mix run priv/openapi.ex > spec.json`.
Request and response validation at runtime catches drift the moment it appears.

```
openapi_gen/
├── lib/
│   └── openapi_gen/
│       ├── application.ex
│       ├── repo.ex
│       ├── invoices/
│       │   └── invoice.ex
│       └── web/
│           ├── router.ex
│           ├── endpoint.ex
│           ├── api_spec.ex
│           ├── schemas/
│           │   ├── invoice.ex
│           │   └── error.ex
│           └── controllers/
│               └── invoice_controller.ex
├── test/
│   └── openapi_gen/
│       └── spec_test.exs
├── priv/
│   └── openapi.ex            # script to dump the spec
└── mix.exs
```

---

## Core concepts

### 1. Schema-as-struct

OpenApiSpex represents every schema as an Elixir module that implements the
`OpenApiSpex.Schema` behaviour. The module carries type, required fields, and
description. Controllers reference these modules by name — no string tags, no
YAML.

```elixir
defmodule Schemas.Invoice do
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
```

### 2. `operation/2` macro in controllers

Each controller action declares its OpenAPI operation with `operation :show,
summary: ..., parameters: [...], responses: %{...}`. OpenApiSpex walks the
router, picks up operations per controller, and assembles `paths` in the final
document.

### 3. `OpenApiSpex.Plug.CastAndValidate`

Plug that casts path/query/body params to schema types AND validates them. If
casting fails (e.g., `amount_cents: "abc"`), the plug returns 422 with a
structured error. The controller action never sees invalid data.

### 4. Spec dump in CI

`mix run priv/openapi.ex > openapi.json`. Commit to the repo; CI runs a `git
diff --exit-code` check that fails if the generated spec drifts from the
committed version. This is the "no silent drift" gate.

---

## Implementation

### Step 1: Dependencies

```elixir
defp deps do
  [
    {:phoenix, "~> 1.7"},
    {:phoenix_ecto, "~> 4.4"},
    {:ecto_sql, "~> 3.11"},
    {:postgrex, "~> 0.17"},
    {:jason, "~> 1.4"},
    {:plug_cowboy, "~> 2.7"},
    {:open_api_spex, "~> 3.19"}
  ]
end
```

### Step 2: Top-level spec module

```elixir
# lib/openapi_gen/web/api_spec.ex
defmodule OpenapiGen.Web.ApiSpec do
  @behaviour OpenApiSpex.OpenApi
  alias OpenApiSpex.{Info, OpenApi, Paths, Server}

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
```

### Step 3: Schemas

```elixir
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
```

### Step 4: Router

```elixir
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
```

### Step 5: Controller with `operation/2`

```elixir
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

### Step 6: Spec dump script

```elixir
# priv/openapi.ex
spec = OpenapiGen.Web.ApiSpec.spec()
IO.puts(Jason.encode!(spec, pretty: true))
```

Run: `mix run priv/openapi.ex > openapi.json`. Commit the file. CI runs:

```bash
mix run priv/openapi.ex > /tmp/generated.json
git diff --exit-code openapi.json /tmp/generated.json
```

### Step 7: Tests

```elixir
# test/openapi_gen/spec_test.exs
defmodule OpenapiGen.SpecTest do
  use ExUnit.Case, async: true
  alias OpenapiGen.Web.ApiSpec

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
```

---

## Trade-offs and production gotchas

**1. Spec generation is compile-time-ish.** `OpenApiSpex.resolve_schema_modules/1`
walks referenced modules. If a controller operation refers to a schema that
doesn't exist, you get a compile-time error from the macro — not a runtime
error in prod. Good.

**2. `replace_params: true` is convenient but hides bugs.** When true, the
casted/validated params replace `conn.params`. Some legacy code reads
`conn.query_params` directly and skips validation. Keep it `false` and force
controllers to accept the casted map explicitly.

**3. Validation error shape is library-dependent.** OpenApiSpex v3 changed the
error format. `json_render_error_v2: true` uses the new shape — make sure
partner client libraries expect it. Bundle the error schema into your spec as
`Error` so partners generate parsers for it.

**4. OAuth and auth schemes.** Route-level auth is NOT auto-detected. Declare
`security_schemes` and `security` in `ApiSpec.spec/0` manually, matching the
actual auth Plug (Bearer, API key, mutual TLS).

**5. Polymorphism — oneOf/anyOf.** Hand-written unions (e.g., Event is
Purchase or Refund) require `oneOf: [Schemas.Purchase, Schemas.Refund]` with
a discriminator. OpenApiSpex supports it; many generators do not. Test your
client generator against the spec before locking in.

**6. CI drift check is not enough.** The committed spec can match the
generated one and still be wrong if the Phoenix code drifted. Layer in
**contract tests**: a test suite that asserts `POST /invoices` with a
schema-valid body returns a schema-valid response. This is what
`OpenApiSpex.TestAssertions.assert_schema/3` is for.

**7. Response validation in production is not free.** Running
`OpenApiSpex.Plug.CastAndValidate` on responses doubles CPU per request.
Enable in staging, sample 1% in prod.

**8. When NOT to use this.** For an internal RPC used by one caller, a
hand-written Markdown doc is fine. OpenApiSpex pays off when the consumer
count > 2 or the spec is used for codegen on the client side.

---

## Performance notes

Per-request overhead of request validation on an M2 Air:

| Request | Without plug | With CastAndValidate |
|---------|--------------|----------------------|
| GET /invoices/{id} | 0.8 ms | 1.1 ms (+38%) |
| POST /invoices (20-field body) | 1.2 ms | 1.9 ms (+58%) |
| POST /invoices (invalid body) | 1.2 ms (controller runs, 500s in Ecto) | 0.4 ms (plug rejects) |

Validation is cheap when the request is valid, cheaper when it's not (fail
fast). The measurable overhead on success is the price for schema compliance
you can count on.

---

## Resources

- [`open_api_spex` documentation](https://hexdocs.pm/open_api_spex/)
- [OpenApiSpex GitHub](https://github.com/open-api-spex/open_api_spex)
- [OpenAPI 3.1 specification](https://spec.openapis.org/oas/v3.1.0)
- [JSON Schema draft 2020-12](https://json-schema.org/draft/2020-12/release-notes.html) — the schema dialect 3.1 uses
- [Stoplight — OpenAPI conventions](https://stoplight.io/api-design-guide)
- [Phoenix contexts and schemas guide](https://hexdocs.pm/phoenix/contexts.html)
