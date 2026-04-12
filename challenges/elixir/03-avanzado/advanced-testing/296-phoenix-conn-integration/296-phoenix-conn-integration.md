# Phoenix Integration Tests with ConnTest and Plug.Test

**Project**: `orders_api` — a Phoenix API whose controllers, plugs, and JSON contracts are integration-tested end-to-end via `Phoenix.ConnTest`.

## Project context

`orders_api` exposes a `/orders` REST endpoint. Unit-testing the controller in isolation
(mocking the context, stubbing the router) has blind spots: a misconfigured pipeline,
a missing CORS plug, a format-negotiation bug, a wrong status code — none are caught
by controller-level tests. Integration tests that go through the router and the full
plug pipeline catch those.

`Phoenix.ConnTest` and `Plug.Test` provide a fast in-process HTTP-like harness. No actual
TCP socket — the request is dispatched directly into the plug pipeline and the conn
struct is returned. That is the test bedrock Phoenix recommends.

```
orders_api/
├── config/
│   └── test.exs
├── lib/
│   └── orders_api/
│       ├── application.ex
│       ├── repo.ex
│       ├── orders.ex
│       └── web/
│           ├── endpoint.ex
│           ├── router.ex
│           ├── controllers/
│           │   └── order_controller.ex
│           └── plugs/
│               └── require_client_id.ex
├── test/
│   ├── orders_api_web/
│   │   ├── controllers/
│   │   │   └── order_controller_test.exs
│   │   └── plugs/
│   │       └── require_client_id_test.exs
│   ├── support/
│   │   ├── conn_case.ex
│   │   └── data_case.ex
│   └── test_helper.exs
└── mix.exs
```

## Why full integration over isolated controller tests

A controller test that calls `OrderController.index(conn, %{})` directly bypasses:
- the router (wrong route means no 404 test)
- pipelines (missing `:api` pipeline means missing JSON parser)
- plugs (auth/CORS/rate-limit not exercised)
- format negotiation
- error rendering (`fallback_controller`)

`Phoenix.ConnTest`'s `get/3`, `post/3` etc. dispatch through the full router. One assertion
replaces five.

## Core concepts

### 1. `build_conn/0` returns a bare conn
`ConnCase` builds one in `setup`. Chain `put_req_header/3`, `put_resp_content_type/2`,
etc. to customize.

### 2. `json_response(conn, status)` and `response(conn, status)`
Assert on status code AND return the decoded body in one call. Failing status prints a
diagnostic including the response body — no guessing.

### 3. Dispatched conn is final
After `get(conn, "/orders")`, the returned conn is post-pipeline: headers, status, body
all populated.

### 4. `Plug.Test.conn/3` for plug-only tests
For testing a single plug in isolation (without the full router), `Plug.Test.conn(:get, "/")`
plus direct calls to `MyPlug.call/2` give you a surgical test.

## Design decisions

- **Option A — only test controllers directly**: fast, blind to router/pipeline bugs.
- **Option B — only test via HTTP client against a running server**: slow (~100× overhead
  per test), flaky under load.
- **Option C — `Phoenix.ConnTest` dispatch through the router**: no socket, goes through
  full pipeline, fast.
- **Option D — `Plug.Test.conn/3` for bare-plug unit tests**: surgical, ideal for custom plugs.

Chosen: **C + D**. Use `ConnTest` for controllers and `Plug.Test` for isolated plug logic.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  [
    {:phoenix, "~> 1.7"},
    {:phoenix_ecto, "~> 4.5"},
    {:ecto_sql, "~> 3.12"},
    {:postgrex, "~> 0.19"},
    {:jason, "~> 1.4"},
    {:plug_cowboy, "~> 2.7"}
  ]
end

defp elixirc_paths(:test), do: ["lib", "test/support"]
defp elixirc_paths(_),     do: ["lib"]
```

### Step 1: the custom plug

```elixir
# lib/orders_api/web/plugs/require_client_id.ex
defmodule OrdersApiWeb.Plugs.RequireClientId do
  @moduledoc "Rejects requests missing the X-Client-ID header with 401."

  import Plug.Conn

  def init(opts), do: opts

  def call(conn, _opts) do
    case get_req_header(conn, "x-client-id") do
      [client_id | _] when client_id != "" ->
        assign(conn, :client_id, client_id)

      _ ->
        conn
        |> put_status(:unauthorized)
        |> Phoenix.Controller.json(%{error: "missing x-client-id"})
        |> halt()
    end
  end
end
```

### Step 2: the controller

```elixir
# lib/orders_api/web/controllers/order_controller.ex
defmodule OrdersApiWeb.OrderController do
  use OrdersApiWeb, :controller

  alias OrdersApi.Orders

  def index(conn, _params) do
    json(conn, %{data: Orders.list(conn.assigns.client_id)})
  end

  def create(conn, %{"amount_cents" => amount}) when is_integer(amount) and amount > 0 do
    case Orders.create(conn.assigns.client_id, amount) do
      {:ok, order} ->
        conn |> put_status(:created) |> json(%{id: order.id, amount_cents: order.amount_cents})

      {:error, reason} ->
        conn |> put_status(:unprocessable_entity) |> json(%{error: inspect(reason)})
    end
  end

  def create(conn, _bad_params) do
    conn
    |> put_status(:bad_request)
    |> json(%{error: "amount_cents must be a positive integer"})
  end
end
```

### Step 3: ConnCase

```elixir
# test/support/conn_case.ex
defmodule OrdersApiWeb.ConnCase do
  use ExUnit.CaseTemplate

  using do
    quote do
      import Plug.Conn
      import Phoenix.ConnTest

      alias OrdersApiWeb.Router.Helpers, as: Routes

      @endpoint OrdersApiWeb.Endpoint
    end
  end

  setup tags do
    pid = Ecto.Adapters.SQL.Sandbox.start_owner!(OrdersApi.Repo, shared: not tags[:async])
    on_exit(fn -> Ecto.Adapters.SQL.Sandbox.stop_owner(pid) end)

    {:ok, conn: Phoenix.ConnTest.build_conn()}
  end
end
```

### Step 4: controller integration tests

```elixir
# test/orders_api_web/controllers/order_controller_test.exs
defmodule OrdersApiWeb.OrderControllerTest do
  use OrdersApiWeb.ConnCase, async: true

  describe "POST /orders — happy path" do
    test "creates an order and returns 201 with the order payload", %{conn: conn} do
      conn =
        conn
        |> put_req_header("x-client-id", "client_abc")
        |> put_req_header("content-type", "application/json")
        |> post("/orders", %{amount_cents: 2500})

      assert %{"id" => id, "amount_cents" => 2500} = json_response(conn, 201)
      assert is_binary(id) or is_integer(id)
    end
  end

  describe "POST /orders — auth" do
    test "returns 401 when x-client-id header is missing", %{conn: conn} do
      conn = post(conn, "/orders", %{amount_cents: 100})

      assert %{"error" => "missing x-client-id"} = json_response(conn, 401)
    end

    test "returns 401 when x-client-id header is empty", %{conn: conn} do
      conn =
        conn
        |> put_req_header("x-client-id", "")
        |> post("/orders", %{amount_cents: 100})

      assert json_response(conn, 401)
    end
  end

  describe "POST /orders — validation" do
    test "returns 400 when amount_cents is missing", %{conn: conn} do
      conn =
        conn
        |> put_req_header("x-client-id", "c")
        |> post("/orders", %{})

      assert %{"error" => _} = json_response(conn, 400)
    end

    test "returns 400 when amount_cents is zero", %{conn: conn} do
      conn =
        conn
        |> put_req_header("x-client-id", "c")
        |> post("/orders", %{amount_cents: 0})

      assert json_response(conn, 400)
    end
  end

  describe "GET /orders — list" do
    test "returns an empty list for a fresh client", %{conn: conn} do
      conn =
        conn
        |> put_req_header("x-client-id", "new_client")
        |> get("/orders")

      assert %{"data" => []} = json_response(conn, 200)
    end
  end
end
```

### Step 5: plug-only tests using Plug.Test

```elixir
# test/orders_api_web/plugs/require_client_id_test.exs
defmodule OrdersApiWeb.Plugs.RequireClientIdTest do
  use ExUnit.Case, async: true

  import Plug.Test

  alias OrdersApiWeb.Plugs.RequireClientId

  describe "RequireClientId plug in isolation" do
    test "assigns client_id when header is present" do
      conn =
        conn(:get, "/")
        |> put_req_header("x-client-id", "c_42")
        |> RequireClientId.call(%{})

      assert conn.assigns.client_id == "c_42"
      refute conn.halted
    end

    test "halts with 401 when header is missing" do
      conn =
        conn(:get, "/")
        |> RequireClientId.call(%{})

      assert conn.halted
      assert conn.status == 401
    end
  end
end
```

## Why this works

`Phoenix.ConnTest` dispatches the conn through `@endpoint.call/2`, which runs the full
endpoint, pipelines, router, and controller. The returned conn is the final state —
status, headers, body all populated. `json_response/2` both asserts on status and
parses the body; on mismatch it prints the full body for debugging.

`Plug.Test.conn/3` builds a minimal conn suitable for calling a plug directly. It does
not go through the endpoint; it exercises the plug function in isolation — the right
tool for testing a custom plug without the noise of the whole router.

## Tests

See Steps 4 and 5.

## Benchmark

A ConnTest round-trip is ~500µs (no socket). A 100-test suite finishes in well under
2 seconds exclusive of DB setup.

```elixir
Benchee.run(%{
  "GET /orders" => fn ->
    Phoenix.ConnTest.build_conn()
    |> Plug.Conn.put_req_header("x-client-id", "b")
    |> Phoenix.ConnTest.get("/orders")
  end
}, time: 3)
```

Target: < 1ms/op.

## Trade-offs and production gotchas

**1. Building conn inside test body without `setup`**
Every test must start from a fresh conn. ConnCase's setup returns one — use the `conn`
from the test context, don't call `build_conn()` inside the test body unnecessarily.

**2. Asserting on string body then decoding manually**
`response(conn, 200)` returns the raw body. For JSON, always use `json_response/2` —
it both asserts status and decodes in one call.

**3. Forgetting `put_req_header("content-type", ...)` for JSON POST**
The endpoint's `Plug.Parsers` relies on content type to pick a parser. Without it, the
body is not parsed and `params` arrive empty. Use `post(conn, "/orders", json_map)` —
ConnTest sets the header automatically when a map is passed.

**4. Session state leaks across tests**
`ConnCase.setup` returns a fresh conn. If your test chains `conn = post(...); conn = get(...)`,
you are implicitly carrying session/cookies. That's usually desired, but be deliberate.

**5. Using integration tests for business-logic edge cases**
If a test asserts on 30 validation branches, each round-trips the router for nothing.
Put business logic tests at the context (`Orders`) level; keep integration tests for
HTTP contract.

**6. When NOT to use this**
Pure function / context module tests (`Orders.list/1`) are better tested directly. Use
ConnTest for HTTP surface and authorization; use context tests for domain rules.

## Reflection

ConnTest bypasses TCP. For what categories of production bug (network, TLS, keepalives,
WebSocket upgrade) is that bypass a false assurance, and what complementary tool would
close the gap?

## Resources

- [`Phoenix.ConnTest`](https://hexdocs.pm/phoenix/Phoenix.ConnTest.html)
- [`Plug.Test`](https://hexdocs.pm/plug/Plug.Test.html)
- [Phoenix testing guide](https://hexdocs.pm/phoenix/testing.html)
- [Phoenix `json_response/2`](https://hexdocs.pm/phoenix/Phoenix.ConnTest.html#json_response/2)
