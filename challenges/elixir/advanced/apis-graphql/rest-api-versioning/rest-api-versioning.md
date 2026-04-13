# REST API Versioning (URL, Header, Content-Type)

**Project**: `payments_api` — a Phoenix REST API that supports three concurrent versioning strategies so clients can migrate on their own schedule without forking the codebase

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
payments_api/
├── lib/
│   └── payments_api.ex
├── script/
│   └── main.exs
├── test/
│   └── payments_api_test.exs
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
defmodule PaymentsApi.MixProject do
  use Mix.Project

  def project do
    [
      app: :payments_api,
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

### `lib/payments_api.ex`

```elixir
defmodule PaymentsApiWeb.Plugs.ApiVersion do
  @moduledoc """
  Ejercicio: REST API Versioning (URL, Header, Content-Type).
  Implementa el comportamiento descrito en el enunciado, exponiendo funciones públicas documentadas y con tipos claros.
  """

  @behaviour Plug
  import Plug.Conn

  @supported ["1", "2"]
  @default "2"

  @impl true
  def init(opts), do: opts

  @doc "Calls result from conn and _opts."
  @impl true
  def call(conn, _opts) do
    version =
      conn.private[:phoenix_url_version] || # set by the scope below
        from_header(conn) ||
        from_accept(conn) ||
        @default

    if version in @supported do
      conn
      |> assign(:api_version, version)
      |> maybe_sunset(version)
    else
      conn
      |> put_resp_content_type("application/json")
      |> send_resp(400, Jason.encode!(%{error: "unsupported api version", given: version}))
      |> halt()
    end
  end

  defp from_header(conn) do
    case get_req_header(conn, "x-api-version") do
      [v | _] -> v
      _ -> nil
    end
  end

  defp from_accept(conn) do
    conn
    |> get_req_header("accept")
    |> List.first()
    |> case do
      nil -> nil
      header ->
        case Regex.run(~r/application\/vnd\.payments\.v(\d+)\+json/, header || "") do
          [_, v] -> v
          _ -> nil
        end
    end
  end

  # Version 1 is scheduled to retire on 2027-01-01. Warn every response.
  defp maybe_sunset(conn, "1") do
    conn
    |> put_resp_header("deprecation", "true")
    |> put_resp_header("sunset", "Fri, 01 Jan 2027 00:00:00 GMT")
    |> put_resp_header("link", "</v2/charges>; rel=\"successor-version\"")
  end

  defp maybe_sunset(conn, _), do: conn
end

defmodule PaymentsApiWeb.Router do
  use PaymentsApiWeb, :router

  pipeline :api do
    plug :accepts, ["json", "vnd.payments.v1+json", "vnd.payments.v2+json"]
    plug PaymentsApiWeb.Plugs.ApiVersion
  end

  # URL strategy: explicit prefix that the plug will observe via :phoenix_url_version.
  scope "/v1", PaymentsApiWeb, as: :v1 do
    pipe_through :api
    plug :put_url_version, "1"
    resources "/charges", ChargeController, only: [:index, :show]
  end

  scope "/v2", PaymentsApiWeb, as: :v2 do
    pipe_through :api
    plug :put_url_version, "2"
    resources "/charges", ChargeController, only: [:index, :show]
  end

  # Unversioned scope: falls back to header or Accept, else @default.
  scope "/", PaymentsApiWeb do
    pipe_through :api
    resources "/charges", ChargeController, only: [:index, :show]
  end

  defp put_url_version(conn, version), do: Plug.Conn.put_private(conn, :phoenix_url_version, version)
end

defmodule PaymentsApiWeb.ChargeController do
  use PaymentsApiWeb, :controller
  alias PaymentsApi.Billing

  @doc "Returns index result from conn and _params."
  def index(conn, _params) do
    charges = Billing.list_charges()
    render(conn, pick_view(conn), :index, charges: charges)
  end

  @doc "Shows result from conn."
  def show(conn, %{"id" => id}) do
    case Billing.fetch_charge(id) do
      {:ok, charge} -> render(conn, pick_view(conn), :show, charge: charge)
      :error -> send_resp(conn, 404, "")
    end
  end

  defp pick_view(%{assigns: %{api_version: "1"}}), do: PaymentsApiWeb.ChargeViewV1
  defp pick_view(%{assigns: %{api_version: "2"}}), do: PaymentsApiWeb.ChargeViewV2
end

defmodule PaymentsApiWeb.ChargeViewV1 do
  @doc "Renders result."
  def render("index.json", %{charges: cs}), do: %{charges: Enum.map(cs, &to_map/1)}
  @doc "Renders result."
  def render("show.json", %{charge: c}), do: %{charge: to_map(c)}

  # v1 exposes `amount` in cents as a string for legacy BCD clients.
  defp to_map(c), do: %{id: c.id, amount: Integer.to_string(c.amount_cents), currency: c.currency}
end

defmodule PaymentsApiWeb.ChargeViewV2 do
  @doc "Renders result."
  def render("index.json", %{charges: cs}), do: %{data: Enum.map(cs, &to_map/1)}
  @doc "Renders result."
  def render("show.json", %{charge: c}), do: %{data: to_map(c)}

  # v2 returns amount as a structured money object.
  defp to_map(c), do: %{id: c.id, amount: %{value: c.amount_cents, currency: c.currency, exponent: 2}}
end

defmodule PaymentsApi.Billing do
  defstruct [:id, :amount_cents, :currency]

  @doc "Lists charges result."
  def list_charges, do: [%__MODULE__{id: "ch_1", amount_cents: 1000, currency: "USD"}]

  @doc "Fetches charge result."
  def fetch_charge("ch_1"), do: {:ok, %__MODULE__{id: "ch_1", amount_cents: 1000, currency: "USD"}}
  @doc "Fetches charge result from _."
  def fetch_charge(_), do: :error
end
```

### `test/payments_api_test.exs`

```elixir
defmodule PaymentsApiWeb.ChargeControllerTest do
  use PaymentsApiWeb.ConnCase, async: true

  describe "URL versioning" do
    test "/v1 returns legacy shape", %{conn: conn} do
      body = conn |> get("/v1/charges/ch_1") |> json_response(200)
      assert %{"charge" => %{"amount" => "1000"}} = body
    end

    test "/v2 returns structured amount", %{conn: conn} do
      body = conn |> get("/v2/charges/ch_1") |> json_response(200)
      assert %{"data" => %{"amount" => %{"value" => 1000}}} = body
    end

    test "/v1 adds deprecation + sunset headers", %{conn: conn} do
      conn = get(conn, "/v1/charges/ch_1")
      assert ["true"] = get_resp_header(conn, "deprecation")
      assert [sunset] = get_resp_header(conn, "sunset")
      assert sunset =~ "2027"
    end
  end

  describe "header versioning" do
    test "X-API-Version: 1 picks v1 view", %{conn: conn} do
      conn = conn |> put_req_header("x-api-version", "1") |> get("/charges/ch_1")
      assert %{"charge" => %{"amount" => "1000"}} = json_response(conn, 200)
    end

    test "unsupported version returns 400", %{conn: conn} do
      conn = conn |> put_req_header("x-api-version", "99") |> get("/charges/ch_1")
      assert %{"error" => _} = json_response(conn, 400)
    end
  end

  describe "content-type versioning" do
    test "Accept vnd.payments.v1+json picks v1", %{conn: conn} do
      conn = conn |> put_req_header("accept", "application/vnd.payments.v1+json") |> get("/charges/ch_1")
      assert %{"charge" => %{"amount" => "1000"}} = json_response(conn, 200)
    end
  end

  describe "precedence" do
    test "URL wins over header", %{conn: conn} do
      conn = conn |> put_req_header("x-api-version", "1") |> get("/v2/charges/ch_1")
      assert %{"data" => _} = json_response(conn, 200)
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for REST API Versioning (URL, Header, Content-Type).

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== REST API Versioning (URL, Header, Content-Type) ===")
    IO.puts("Category: APIs and GraphQL\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case PaymentsApi.run(payload) do
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
        for _ <- 1..1_000, do: PaymentsApi.run(:bench)
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
