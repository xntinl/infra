# REST API Versioning (URL, Header, Content-Type)

**Project**: `payments_api` — a Phoenix REST API that supports three concurrent versioning strategies so clients can migrate on their own schedule without forking the codebase.

## Project context

`payments_api` serves mobile, web, and third-party integrations. Breaking changes to the `/charges` resource need to ship without breaking the 12-month-old Android client still in production. The product team asked for all three versioning styles in parallel: URL-based for quick discovery, header-based for cleaner URLs, and content-type negotiation for HATEOAS-style evolution.

This exercise builds a single router and controller that dispatches on whatever strategy the client uses, while keeping a single business-logic module. The senior discipline here is **one domain, many representations** — versioning is a presentation concern.

```
payments_api/
├── lib/
│   ├── payments_api/
│   │   ├── application.ex
│   │   └── billing.ex                 # domain (version-agnostic)
│   └── payments_api_web/
│       ├── endpoint.ex
│       ├── router.ex
│       ├── plugs/
│       │   └── api_version.ex
│       ├── controllers/
│       │   └── charge_controller.ex
│       └── views/
│           ├── charge_view_v1.ex
│           └── charge_view_v2.ex
├── test/payments_api_web/
│   └── charge_controller_test.exs
└── mix.exs
```

## Why support three strategies

The textbook answer is "pick one". Reality is messier:

- **URL versioning (`/v1/charges`)**: hardest to miss, easiest to cache, easiest to log. Cost: duplicated routes, URL churns on every bump.
- **Header versioning (`X-API-Version: 2`)**: clean URLs, easy to default. Cost: invisible to browsers, hard to cache per version, hard to debug from logs without explicit logging.
- **Content-type (`Accept: application/vnd.payments.v2+json`)**: most RESTful per Fielding, leverages existing HTTP negotiation. Cost: clients often get this wrong; proxies strip custom media types.

A production API that must coexist with external integrators ends up offering at least two of these. The cost of supporting all three is one plug.

## Why NOT a new Phoenix app per version

Forking the codebase (`lib/payments_api_v2/`) is the naive path. It doubles maintenance: every bugfix applies twice, every shared utility drifts. The right factoring is **one domain, versioned views**: the business module returns structs, a view module per version translates to JSON.

## Core concepts

### 1. Version as a plug assign
`conn.assigns.api_version` is the single source of truth after the plug runs. Controllers branch on it; views are picked by it.

### 2. Precedence order
URL path wins over header wins over content-type. Document this; it is nearly always the right order.

### 3. Sunset headers
Deprecated versions respond with `Deprecation: true` and `Sunset: <RFC 1123 date>`. Clients see the warning before their code breaks.

## Design decisions

- **Option A — match on version in every controller action**: pros: explicit; cons: noisy, branches in every action.
- **Option B — pick the view module once in the plug**: pros: controller stays version-agnostic; cons: controllers cannot return different fields per version (only shape).
→ We pick **B**. If a version needs entirely different actions, route to a different controller based on the plug — you rarely need more than view-level differences.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  [
    {:phoenix, "~> 1.7"},
    {:jason, "~> 1.4"},
    {:plug_cowboy, "~> 2.7"}
  ]
end
```

### Step 1: Version-resolving plug

```elixir
defmodule PaymentsApiWeb.Plugs.ApiVersion do
  @behaviour Plug
  import Plug.Conn

  @supported ["1", "2"]
  @default "2"

  @impl true
  def init(opts), do: opts

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
```

### Step 2: Router wires URL, header, and content-type strategies

```elixir
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
```

### Step 3: Controller — version-agnostic

```elixir
defmodule PaymentsApiWeb.ChargeController do
  use PaymentsApiWeb, :controller
  alias PaymentsApi.Billing

  def index(conn, _params) do
    charges = Billing.list_charges()
    render(conn, pick_view(conn), :index, charges: charges)
  end

  def show(conn, %{"id" => id}) do
    case Billing.fetch_charge(id) do
      {:ok, charge} -> render(conn, pick_view(conn), :show, charge: charge)
      :error -> send_resp(conn, 404, "")
    end
  end

  defp pick_view(%{assigns: %{api_version: "1"}}), do: PaymentsApiWeb.ChargeViewV1
  defp pick_view(%{assigns: %{api_version: "2"}}), do: PaymentsApiWeb.ChargeViewV2
end
```

### Step 4: Version-specific views

```elixir
defmodule PaymentsApiWeb.ChargeViewV1 do
  def render("index.json", %{charges: cs}), do: %{charges: Enum.map(cs, &to_map/1)}
  def render("show.json", %{charge: c}), do: %{charge: to_map(c)}

  # v1 exposes `amount` in cents as a string for legacy BCD clients.
  defp to_map(c), do: %{id: c.id, amount: Integer.to_string(c.amount_cents), currency: c.currency}
end

defmodule PaymentsApiWeb.ChargeViewV2 do
  def render("index.json", %{charges: cs}), do: %{data: Enum.map(cs, &to_map/1)}
  def render("show.json", %{charge: c}), do: %{data: to_map(c)}

  # v2 returns amount as a structured money object.
  defp to_map(c), do: %{id: c.id, amount: %{value: c.amount_cents, currency: c.currency, exponent: 2}}
end
```

### Step 5: Domain module

```elixir
defmodule PaymentsApi.Billing do
  defstruct [:id, :amount_cents, :currency]

  def list_charges, do: [%__MODULE__{id: "ch_1", amount_cents: 1000, currency: "USD"}]

  def fetch_charge("ch_1"), do: {:ok, %__MODULE__{id: "ch_1", amount_cents: 1000, currency: "USD"}}
  def fetch_charge(_), do: :error
end
```

## Why this works

```
              ┌──────────────────────────────┐
request ────▶ │  ApiVersion plug              │
              │  1. URL prefix? ("/v1"|"/v2") │
              │  2. X-API-Version header?     │
              │  3. Accept vnd.payments.vN?   │
              │  4. Default "2"               │
              └──────────────┬───────────────┘
                             ▼
                  conn.assigns.api_version
                             │
                             ▼
              ChargeController.pick_view/1
                             │
              ┌──────────────┴──────────────┐
              ▼                             ▼
         ChargeViewV1                  ChargeViewV2
```

The domain layer never sees a version. Every strategy collapses to a single assign — the rest of the stack treats "version" like a theme.

## Tests

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

## Benchmark

Version resolution overhead:

```elixir
Benchee.run(%{
  "version plug (url)"    => fn -> get(build_conn(), "/v2/charges/ch_1") end,
  "version plug (header)" => fn -> get(put_req_header(build_conn(), "x-api-version", "2"), "/charges/ch_1") end
})
```

**Expected**: < 20 µs overhead from the plug itself.

## Trade-offs and production gotchas

**1. Missing `accepts` for custom media types**
If you use `Accept: application/vnd.payments.v2+json`, you MUST register `"vnd.payments.v2+json"` in `plug :accepts`. Otherwise Phoenix responds 406.

**2. Cache busting by version**
`Cache-Control` + `Vary: X-API-Version, Accept` — without `Vary`, intermediaries may serve the wrong version to different clients.

**3. "Default = latest" is dangerous**
A client that does not specify a version SHOULD be treated as "legacy" for safety, not "latest". Otherwise a v3 launch silently changes behavior for clients who never opted in.

**4. Two versions diverging without a gate**
When v1 and v2 differ in business logic (not just shape), people put `if api_version == "1"` inside the domain. Don't. Introduce a versioned adapter or keep v1 frozen.

**5. Sunset without telemetry**
Set a deprecation header AND log every v1 call with client identifier. Without logs, you cannot know who is blocking the migration.

**6. When NOT to version at all**
Additive changes (new optional field) do not require a new version. Bumping on every change trains clients to ignore version semantics.

## Reflection

You discovered that 3% of traffic still hits `/v1`, mostly from one large integrator. Marketing wants to sunset v1 in 30 days. Engineering estimates two weeks to finish v3. What do you propose to the product team, and which telemetry convinces them? How does the `Sunset` header factor into that negotiation?

## Resources

- [RFC 8594 — The Sunset HTTP Header Field](https://www.rfc-editor.org/rfc/rfc8594)
- [Phoenix — accepts plug](https://hexdocs.pm/phoenix/Phoenix.Controller.html#accepts/2)
- [Stripe — API versioning](https://stripe.com/docs/api/versioning)
- [Fielding — REST APIs must be hypertext-driven](https://roy.gbiv.com/untangled/2008/rest-apis-must-be-hypertext-driven)
