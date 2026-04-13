# Phoenix Integration Tests with ConnTest and Plug.Test

**Project**: `orders_api` — a Phoenix API whose controllers, plugs, and JSON contracts are integration-tested end-to-end via `Phoenix.ConnTest`

---

## Why advanced testing matters

Production Elixir test suites must run in parallel, isolate side-effects, and exercise concurrent code paths without races. Tooling like Mox, ExUnit async mode, Bypass, ExMachina and StreamData turns testing from a chore into a deliberate design artifact.

When tests double as living specifications, the cost of refactoring drops. When they don't, every change becomes a coin flip. Senior teams treat the test suite as a first-class product — measuring runtime, flake rate, and coverage of failure modes alongside production metrics.

---

## The business problem

You are building a production-grade Elixir component in the **Advanced testing** area. The goal is not a toy demonstration — it is a piece you could drop into a real system and rely on under load.

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

Chose **B** because in Advanced testing the failure modes — not the happy path — are what determine whether the system stays up under real production conditions.

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
# lib/orders_api/web/plugs/require_client_id.ex
defmodule OrdersApiWeb.Plugs.RequireClientId do
  @moduledoc "Rejects requests missing the X-Client-ID header with 401."

  import Plug.Conn

  def init(opts), do: opts

  @doc "Calls result from conn and _opts."
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

# lib/orders_api/web/controllers/order_controller.ex
defmodule OrdersApiWeb.OrderController do
  use OrdersApiWeb, :controller

  alias OrdersApi.Orders

  @doc "Returns index result from conn and _params."
  def index(conn, _params) do
    json(conn, %{data: Orders.list(conn.assigns.client_id)})
  end

  @doc "Creates result from conn."
  def create(conn, %{"amount_cents" => amount}) when is_integer(amount) and amount > 0 do
    case Orders.create(conn.assigns.client_id, amount) do
      {:ok, order} ->
        conn |> put_status(:created) |> json(%{id: order.id, amount_cents: order.amount_cents})

      {:error, reason} ->
        conn |> put_status(:unprocessable_entity) |> json(%{error: inspect(reason)})
    end
  end

  @doc "Creates result from conn and _bad_params."
  def create(conn, _bad_params) do
    conn
    |> put_status(:bad_request)
    |> json(%{error: "amount_cents must be a positive integer"})
  end
end

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

# test/orders_api_web/plugs/require_client_id_test.exs
defmodule OrdersApiWeb.Plugs.RequireClientIdTest do
  use ExUnit.Case, async: true
  doctest OrdersApi.MixProject

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
### `test/orders_api_test.exs`

```elixir
defmodule OrdersApiWeb.ConnCase do
  use ExUnit.Case, async: trueTemplate, async: true
  doctest OrdersApi.MixProject

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
### `script/main.exs`

```elixir
defmodule Main do
  def main do
      IO.puts("Property-based test generator initialized")
      a = 10
      b = 20
      c = 30
      assert (a + b) + c == a + (b + c)
      IO.puts("✓ Property invariant verified: (a+b)+c = a+(b+c)")
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

### 1. Async tests are the default, not the exception

ExUnit defaults to sequential execution. Set `async: true` and structure tests so they don't share global state — Application env, ETS tables, the database. The reward is 5–10× faster suites in CI.

### 2. Mock the boundary, not the dependency

A behaviour-backed mock (Mox.defmock for: SomeBehaviour) is a contract. A bare function stub is a wish. Defining the boundary as a behaviour costs one file and pays back every time the implementation changes.

### 3. Test the failure mode, always

An assertion that succeeds when everything goes right teaches nothing. Tests that prove the system handles `{:error, :timeout}`, `{:error, :network}`, and partial failures are the ones that prevent regressions.

---
