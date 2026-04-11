# Ash — Extensions, Calculations, Aggregates, and API Generation

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`. The `Config` domain from the previous exercise has
`Service` and `RouteRule` resources with basic CRUD. The platform team now needs:

1. A REST API generated automatically from the resources (no controllers, no serializers)
2. Computed fields on `Service` — request count, error rate, health score — derived
   from the metrics store without duplicating the aggregation logic
3. Token-based authentication so only registered operators can modify gateway config
4. Role-based policies: read-only operators vs. admin operators

Project structure:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── config/
│       │   ├── config.ex                       # already exists — extend with extensions
│       │   └── resources/
│       │       ├── service.ex                  # already exists — add calculations + json_api + policies
│       │       ├── route_rule.ex               # already exists — add aggregates + json_api
│       │       └── operator.ex                 # ← you implement this (AshAuthentication)
│       └── config_web/
│           └── router.ex                       # ← you implement this (AshJsonApi.Router)
├── test/
│   └── api_gateway/
│       └── config/
│           ├── calculations_test.exs           # given tests — must pass without modification
│           └── policy_test.exs                 # given tests — must pass without modification
└── mix.exs
```

---

## The business problem

The platform team consumes configuration via `curl` and a small internal dashboard.
Currently they call the Ecto repo directly through a mix task. Three problems:

1. **No REST API** — they cannot query from the dashboard without deploying code.
2. **No computed health data** — the dashboard wants a `health_score` per service
   (0.0–1.0) derived from recent error rate + latency, but this field does not exist
   in the database.
3. **No authentication** — anyone on the internal network can modify route rules.

Ash extensions solve all three without adding controllers, serializers, or custom
authentication code. You declare the extension in the resource; the framework generates
the behaviour.

---

## Why AshJsonApi over writing Phoenix controllers

A Phoenix controller for a resource has four to six action functions, a view module,
a fallback controller, route declarations, and usually an OpenAPI spec annotation.
For five resources that is thirty to forty files with no business logic — pure
infrastructure plumbing.

`AshJsonApi.Resource` generates all of that from the `json_api` block in the resource.
Routes, serialization (JSON:API format), error formatting, and OpenAPI schema are
all derived from the resource declaration you already wrote.

The tradeoff: you get JSON:API format, not arbitrary JSON. If the dashboard team
wants a custom response shape that deviates from JSON:API, AshJsonApi is the wrong
tool. For standard CRUD-over-REST this is the correct level of abstraction.

---

## Why calculations are not just virtual Ecto fields

An Ecto `@virtual` field exists in the struct but is not persisted and not loaded by
default. You populate it by calling your own function somewhere — in a context module,
in a resolver, in a controller — and the field is silently `nil` if you forget.

An Ash `calculation` is part of the resource contract. It has a declared return type,
explicit dependencies (Ash loads them automatically before computing), and is
opt-in per query (`Ash.Query.load([:health_score])`). It cannot be silently nil
because Ash will raise if you try to access it without explicitly loading it.

For calculations that translate to SQL (`expr/1`), Ash pushes them to the database.
For calculations that require Elixir logic (calling a GenServer, formatting strings),
you implement the `Ash.Resource.Calculation` behaviour. Both surfaces look identical
to the caller.

---

## Implementation

### Step 1: `mix.exs` additions

```elixir
{:ash_json_api, "~> 1.0"},
{:ash_authentication, "~> 4.0"},
{:ash_authentication_phoenix, "~> 2.0"},
{:bcrypt_elixir, "~> 3.0"},
{:open_api_spex, "~> 3.18"}
```

### Step 2: Add calculations to Service — `lib/api_gateway/config/resources/service.ex`

Extend the existing Service resource with calculations and AshJsonApi:

```elixir
defmodule ApiGateway.Config.Resources.Service do
  use Ash.Resource,
    domain: ApiGateway.Config,
    data_layer: AshPostgres.DataLayer,
    extensions: [AshJsonApi.Resource],
    authorizers: [Ash.Policy.Authorizer]

  # ... existing postgres, attributes, actions blocks unchanged ...

  calculations do
    # Derived in SQL — efficient for bulk reads
    # Returns true when weight > 50
    calculate :high_priority, :boolean, expr(weight > 50)

    # Derived from the metrics GenServer — cannot be expressed in SQL
    # TODO: implement ApiGateway.Config.Resources.Service.Calculations.HealthScore
    # The calculation must declare load: [] (no DB fields needed — it queries MetricsStore)
    calculate :health_score, :float,
      ApiGateway.Config.Resources.Service.Calculations.HealthScore

    # Formatted upstream display: "name (upstream_url)"
    # TODO: implement ApiGateway.Config.Resources.Service.Calculations.DisplayLabel
    # Declare load: [:name, :upstream_url]
    calculate :display_label, :string,
      ApiGateway.Config.Resources.Service.Calculations.DisplayLabel
  end

  json_api do
    type "services"

    routes do
      base "/services"
      index :routable
      get :read
      post :create
      patch :update
      delete :destroy
      patch :drain, route: "/:id/drain"
      patch :take_offline, route: "/:id/take-offline"
      patch :activate, route: "/:id/activate"
    end
  end

  policies do
    # TODO: allow reads for any authenticated operator (any role)
    policy action_type(:read) do
      # authorize_if actor_present()
    end

    # TODO: allow create/update/destroy only for operators with role :admin
    policy action_type([:create, :update, :destroy]) do
      # authorize_if actor_attribute_equals(:role, :admin)
    end
  end
end
```

### Step 3: HealthScore calculation — `lib/api_gateway/config/resources/service/calculations/health_score.ex`

```elixir
defmodule ApiGateway.Config.Resources.Service.Calculations.HealthScore do
  @moduledoc """
  Computes a 0.0–1.0 health score per service from the in-memory MetricsStore.

  Formula: max(0.0, 1.0 - error_rate - latency_penalty)
  where latency_penalty = p99_ms / 10_000 (capped at 0.5)

  This calculation queries a GenServer, not the database. It is intentionally
  decoupled from the Ecto layer — the score is always fresh, never stale.
  """

  use Ash.Resource.Calculation

  alias ApiGateway.MetricsStore

  @impl true
  def calculate(records, _opts, _context) do
    Enum.map(records, fn service ->
      # TODO: call MetricsStore.get_metrics(service.name) to retrieve
      # %{error_rate: float, p99_latency_ms: number} or nil
      # TODO: if nil, return 1.0 (no data = assume healthy)
      # TODO: compute latency_penalty = min(p99_latency_ms / 10_000.0, 0.5)
      # TODO: return max(0.0, 1.0 - error_rate - latency_penalty) rounded to 2 decimals

      1.0  # placeholder — replace with actual logic
    end)
  end

  @impl true
  def load(_query, _opts, _context), do: [:name]
end
```

### Step 4: DisplayLabel calculation — `lib/api_gateway/config/resources/service/calculations/display_label.ex`

```elixir
defmodule ApiGateway.Config.Resources.Service.Calculations.DisplayLabel do
  @moduledoc """
  Returns a human-readable label for dashboard display: "name (upstream_url)".
  """

  use Ash.Resource.Calculation

  @impl true
  def calculate(records, _opts, _context) do
    # TODO: map each service to "#{service.name} (#{service.upstream_url})"
    Enum.map(records, fn _service -> "" end)
  end

  @impl true
  def load(_query, _opts, _context), do: [:name, :upstream_url]
end
```

### Step 5: Add aggregates to RouteRule — `lib/api_gateway/config/resources/route_rule.ex`

Extend RouteRule with aggregates that count how many rules point to each service:

```elixir
# Inside the existing RouteRule resource:

aggregates do
  # Total rules referencing a service (regardless of active status)
  count :total_rule_count, :service

  # Active rules only
  count :active_rule_count, :service do
    # TODO: filter expr(active == true)
  end
end

json_api do
  type "route-rules"

  routes do
    base "/route-rules"
    index :ordered
    get :read
    post :create
    patch :update
    patch :reprioritize, route: "/:id/reprioritize"
    delete :destroy
  end
end
```

### Step 6: Operator resource with AshAuthentication — `lib/api_gateway/config/resources/operator.ex`

```elixir
defmodule ApiGateway.Config.Resources.Operator do
  @moduledoc """
  A human operator who manages gateway configuration.

  Roles:
  - :readonly — can read services and route rules, cannot modify
  - :admin    — full CRUD on all config resources
  """

  use Ash.Resource,
    domain: ApiGateway.Config,
    data_layer: AshPostgres.DataLayer,
    extensions: [AshAuthentication]

  postgres do
    table "gateway_operators"
    repo ApiGateway.Repo
  end

  attributes do
    uuid_primary_key :id

    attribute :email, :ci_string do
      allow_nil? false
      public? true
    end

    attribute :role, :atom do
      constraints one_of: [:readonly, :admin]
      default :readonly
      allow_nil? false
      public? true
    end

    timestamps()
  end

  validations do
    validate match(:email, ~r/^[^\s]+@[^\s]+\.[^\s]+$/),
      message: "must be a valid email address"
  end

  authentication do
    strategies do
      password :password do
        identity_field :email

        # TODO: configure resettable block with a stub sender module
        # that logs the reset link to Logger.info in development
      end
    end

    tokens do
      enabled? true
      token_resource ApiGateway.Config.Resources.Token
      signing_secret fn _, _ ->
        Application.fetch_env(:api_gateway, :operator_token_signing_secret)
      end
    end
  end

  actions do
    defaults [:read, :destroy]

    # AshAuthentication generates: :register_with_password, :sign_in_with_password
    # Add domain-specific actions:

    update :promote_to_admin do
      # TODO: change set_attribute(:role, :admin)
    end

    update :demote_to_readonly do
      # TODO: change set_attribute(:role, :readonly)
    end
  end

  identities do
    identity :unique_email, [:email]
  end
end
```

### Step 7: Token resource — `lib/api_gateway/config/resources/token.ex`

```elixir
defmodule ApiGateway.Config.Resources.Token do
  use Ash.Resource,
    domain: ApiGateway.Config,
    data_layer: AshPostgres.DataLayer,
    extensions: [AshAuthentication.TokenResource]

  postgres do
    table "gateway_operator_tokens"
    repo ApiGateway.Repo
  end
end
```

### Step 8: Router — `lib/api_gateway/config_web/router.ex`

```elixir
defmodule ApiGateway.ConfigWeb.Router do
  use Phoenix.Router
  use AshJsonApi.Router,
    domains: [ApiGateway.Config],
    open_api: "/open_api"

  pipeline :api do
    plug :accepts, ["json"]
    plug AshJsonApi.Plug.Parser
  end

  scope "/config/api" do
    pipe_through :api
    forward "/", AshJsonApi.Router, domains: [ApiGateway.Config]
  end
end
```

### Step 9: Given tests — must pass without modification

```elixir
# test/api_gateway/config/calculations_test.exs
defmodule ApiGateway.Config.CalculationsTest do
  use ApiGateway.DataCase, async: true

  alias ApiGateway.Config
  alias ApiGateway.Config.Resources.Service

  defp insert_service(attrs \\ %{}) do
    defaults = %{
      name: "svc-#{:rand.uniform(9_999_999)}",
      upstream_url: "https://upstream.internal",
      status: :active,
      weight: 100
    }

    Service
    |> Ash.Changeset.for_create(:create, Map.merge(defaults, attrs))
    |> Config.create!()
  end

  test "high_priority is true when weight > 50" do
    svc = insert_service(weight: 80)

    [loaded] =
      Service
      |> Ash.Query.filter(id == ^svc.id)
      |> Ash.Query.load([:high_priority])
      |> Config.read!()

    assert loaded.high_priority == true
  end

  test "high_priority is false when weight <= 50" do
    svc = insert_service(weight: 30)

    [loaded] =
      Service
      |> Ash.Query.filter(id == ^svc.id)
      |> Ash.Query.load([:high_priority])
      |> Config.read!()

    assert loaded.high_priority == false
  end

  test "display_label is 'name (upstream_url)'" do
    svc = insert_service(name: "billing", upstream_url: "https://billing.internal")

    [loaded] =
      Service
      |> Ash.Query.filter(id == ^svc.id)
      |> Ash.Query.load([:display_label])
      |> Config.read!()

    assert loaded.display_label == "billing (https://billing.internal)"
  end

  test "health_score is a float between 0.0 and 1.0" do
    svc = insert_service()

    [loaded] =
      Service
      |> Ash.Query.filter(id == ^svc.id)
      |> Ash.Query.load([:health_score])
      |> Config.read!()

    assert is_float(loaded.health_score)
    assert loaded.health_score >= 0.0
    assert loaded.health_score <= 1.0
  end

  test "accessing calculation without load raises" do
    svc = insert_service()

    [raw] =
      Service
      |> Ash.Query.filter(id == ^svc.id)
      |> Config.read!()

    assert_raise RuntimeError, fn ->
      _ = raw.health_score
    end
  end
end
```

```elixir
# test/api_gateway/config/policy_test.exs
defmodule ApiGateway.Config.PolicyTest do
  use ApiGateway.DataCase, async: true

  alias ApiGateway.Config
  alias ApiGateway.Config.Resources.{Service, Operator}

  defp insert_service(attrs \\ %{}) do
    Service
    |> Ash.Changeset.for_create(:create, %{
      name: "svc-#{:rand.uniform(9_999_999)}",
      upstream_url: "https://up.internal",
      status: :active,
      weight: 100
    } |> Map.merge(attrs))
    |> Config.create!()
  end

  defp make_operator(role) do
    %Operator{id: Ecto.UUID.generate(), role: role, email: "op@test.io"}
  end

  test "readonly operator can read services" do
    _svc = insert_service()
    op = make_operator(:readonly)

    result =
      Service
      |> Ash.Query.for_read(:active, %{}, actor: op)
      |> Config.read()

    assert {:ok, _services} = result
  end

  test "readonly operator cannot create services" do
    op = make_operator(:readonly)

    result =
      Service
      |> Ash.Changeset.for_create(:create, %{
        name: "new-svc",
        upstream_url: "https://new.internal",
        status: :active,
        weight: 50
      }, actor: op)
      |> Config.create()

    assert {:error, %Ash.Error.Forbidden{}} = result
  end

  test "admin operator can create services" do
    op = make_operator(:admin)

    result =
      Service
      |> Ash.Changeset.for_create(:create, %{
        name: "admin-svc-#{:rand.uniform(999_999)}",
        upstream_url: "https://admin.internal",
        status: :active,
        weight: 75
      }, actor: op)
      |> Config.create()

    assert {:ok, _svc} = result
  end
end
```

### Step 10: Run the tests

```bash
mix test test/api_gateway/config/ --trace
```

---

## Trade-off analysis

| Aspect | Ash calculations | Virtual Ecto field | Database computed column |
|--------|-----------------|--------------------|--------------------------|
| Declaration site | Resource (single source) | Schema + populating code | Migration |
| Auto-loaded | No — opt-in via `load` | No — silent nil | Yes — always |
| External data (GenServer) | Yes (module calculation) | Yes (manual call) | No |
| SQL pushdown | Yes (expr calculations) | No | Yes |
| Type-checked | Yes | No | Depends on DB |
| Queryable (filter/sort) | Yes (expr only) | No | Yes |

Reflection question: `calculate :health_score` calls `MetricsStore.get_metrics/1`
for each service record in the list. If a query returns 200 services, that is 200
GenServer calls inside `calculate/3`. What is the latency profile of this vs. a
single batch call? How would you redesign `HealthScore.calculate/3` to call
`MetricsStore.get_batch_metrics/1` once instead of N times?

---

## Common production mistakes

**1. Returning `[values]` from `calculate/3` in the wrong order**
`calculate/3` receives `records` in query order and must return values in the same
order. If you sort or filter inside `calculate/3`, the N-th value no longer
corresponds to the N-th record. Ash will silently assign the wrong value to the wrong
resource struct.

**2. `json_api` block without `public?: true` on attributes**
AshJsonApi exposes only public attributes. Declaring `json_api do type "services" end`
without `public?: true` on `:upstream_url` means the field is in the database but
never serialized in the REST response — with no error.

**3. Policies that allow actions but not the data layer**
`authorize_if actor_present()` allows the action, but AshPostgres still enforces
row-level filtering based on policies. A policy that says "readonly can read services"
must also allow the underlying data layer read. Test with both `:read` and actual
data queries.

**4. AshAuthentication token resource not in the domain**
`AshAuthentication.TokenResource` must be registered in the same domain as the user
resource. If it is omitted from `resources do ... end`, token verification silently
fails — no meaningful error message.

**5. Using `actor: nil` to bypass policies in tests**
In test code it is tempting to omit the actor to skip policies. This hides bugs.
Test with real actor structs that match each role. The test helpers above show the
correct approach: a plain struct with `%Operator{role: :readonly}` is sufficient —
no database insert required.

---

## Resources

- [AshJsonApi](https://hexdocs.pm/ash_json_api) — routes, JSON:API format, OpenAPI generation
- [AshAuthentication](https://hexdocs.pm/ash_authentication) — password strategy, tokens, lifecycle hooks
- [Ash Calculations](https://hexdocs.pm/ash/calculations.html) — expr vs module, load dependencies
- [Ash Aggregates](https://hexdocs.pm/ash/aggregates.html) — count, sum, avg, filter on aggregates
- [Ash Policies](https://hexdocs.pm/ash/policies.html) — authorize_if, actor_attribute_equals, actor_present
