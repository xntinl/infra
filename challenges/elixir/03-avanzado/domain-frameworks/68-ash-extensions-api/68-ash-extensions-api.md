# Ash — Extensions, Calculations, Aggregates, and API Generation

## Project context

You are building `api_gateway`, an internal HTTP gateway. The configuration management subsystem needs computed fields on services (health score, display label), token-based authentication via an Operator resource, role-based policies, and a REST API auto-generated from resources. All modules — including the Ash domain, resources, calculations, operator, and policies — are defined from scratch.

Project structure:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── repo.ex                                     # Ecto.Repo
│       ├── metrics_store.ex                            # simple GenServer for metrics
│       └── config/
│           ├── config.ex                               # Ash.Domain
│           └── resources/
│               ├── service.ex                          # Service resource with calculations + policies
│               ├── route_rule.ex                       # RouteRule resource
│               ├── route_rule/
│               │   └── validations/
│               │       └── path_format.ex              # custom validation
│               ├── operator.ex                         # Operator resource (AshAuthentication)
│               └── service/
│                   └── calculations/
│                       ├── health_score.ex             # computed health score
│                       └── display_label.ex            # computed display string
├── test/
│   └── api_gateway/
│       └── config/
│           ├── calculations_test.exs
│           └── policy_test.exs
└── mix.exs
```

---

## The business problem

1. **No REST API** — operators cannot query configuration from the dashboard without deploying code.
2. **No computed health data** — the dashboard wants a `health_score` per service (0.0-1.0) derived from metrics, but this field does not exist in the database.
3. **No authentication** — anyone on the internal network can modify route rules.

Ash extensions solve all three without adding controllers, serializers, or custom authentication code.

---

## Why calculations are not just virtual Ecto fields

An Ecto `@virtual` field exists in the struct but is silently `nil` if you forget to populate it. An Ash `calculation` is part of the resource contract with a declared return type and opt-in loading via `Ash.Query.load([:health_score])`. It cannot be silently nil because Ash will raise if you access it without loading.

---

## Implementation

### Step 1: `mix.exs` additions

```elixir
defp deps do
  [
    {:ash, "~> 3.0"},
    {:ash_postgres, "~> 2.0"},
    {:ash_json_api, "~> 1.0"},
    {:ash_authentication, "~> 4.0"},
    {:ecto_sql, "~> 3.11"},
    {:postgrex, "~> 0.18"},
    {:bcrypt_elixir, "~> 3.0"},
    {:open_api_spex, "~> 3.18"}
  ]
end
```

### Step 2: MetricsStore — `lib/api_gateway/metrics_store.ex`

A simple GenServer that provides metrics data for the HealthScore calculation.

```elixir
defmodule ApiGateway.MetricsStore do
  @moduledoc """
  In-memory metrics store that provides per-service error rate and latency.
  Used by the HealthScore calculation to compute a health score without
  hitting the database.
  """
  use GenServer

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec get_metrics(String.t()) :: %{error_rate: float(), p99_latency_ms: number()} | nil
  def get_metrics(service_name) do
    GenServer.call(__MODULE__, {:get, service_name})
  end

  @spec put_metrics(String.t(), map()) :: :ok
  def put_metrics(service_name, metrics) do
    GenServer.cast(__MODULE__, {:put, service_name, metrics})
  end

  @impl true
  def init(_opts), do: {:ok, %{}}

  @impl true
  def handle_call({:get, name}, _from, state) do
    {:reply, Map.get(state, name), state}
  end

  @impl true
  def handle_cast({:put, name, metrics}, state) do
    {:noreply, Map.put(state, name, metrics)}
  end
end
```

### Step 3: Domain — `lib/api_gateway/config/config.ex`

```elixir
defmodule ApiGateway.Config do
  @moduledoc """
  Ash domain for gateway configuration resources.
  All external code calls Config.create!/2, Config.read!/1, etc.
  """

  use Ash.Domain

  resources do
    resource ApiGateway.Config.Resources.Service
    resource ApiGateway.Config.Resources.RouteRule
    resource ApiGateway.Config.Resources.Operator
  end
end
```

### Step 4: HealthScore calculation — `lib/api_gateway/config/resources/service/calculations/health_score.ex`

```elixir
defmodule ApiGateway.Config.Resources.Service.Calculations.HealthScore do
  @moduledoc """
  Computes a 0.0-1.0 health score per service from the in-memory MetricsStore.

  Formula: max(0.0, 1.0 - error_rate - latency_penalty)
  where latency_penalty = min(p99_ms / 10_000, 0.5)
  """

  use Ash.Resource.Calculation

  @impl true
  def calculate(records, _opts, _context) do
    Enum.map(records, fn service ->
      case ApiGateway.MetricsStore.get_metrics(service.name) do
        nil ->
          1.0

        %{error_rate: error_rate, p99_latency_ms: p99_ms} ->
          latency_penalty = min(p99_ms / 10_000.0, 0.5)
          score = max(0.0, 1.0 - error_rate - latency_penalty)
          Float.round(score, 2)
      end
    end)
  end

  @impl true
  def load(_query, _opts, _context), do: [:name]
end
```

### Step 5: DisplayLabel calculation — `lib/api_gateway/config/resources/service/calculations/display_label.ex`

```elixir
defmodule ApiGateway.Config.Resources.Service.Calculations.DisplayLabel do
  @moduledoc """
  Returns a human-readable label for dashboard display: \"name (upstream_url)\".
  """

  use Ash.Resource.Calculation

  @impl true
  def calculate(records, _opts, _context) do
    Enum.map(records, fn service ->
      "#{service.name} (#{service.upstream_url})"
    end)
  end

  @impl true
  def load(_query, _opts, _context), do: [:name, :upstream_url]
end
```

### Step 6: Service resource — `lib/api_gateway/config/resources/service.ex`

```elixir
defmodule ApiGateway.Config.Resources.Service do
  @moduledoc """
  A backend service registered in the gateway.
  Includes calculations for health_score and display_label,
  AshJsonApi for REST generation, and policies for role-based access.
  """

  use Ash.Resource,
    domain: ApiGateway.Config,
    data_layer: AshPostgres.DataLayer,
    extensions: [AshJsonApi.Resource],
    authorizers: [Ash.Policy.Authorizer]

  postgres do
    table "gateway_services"
    repo ApiGateway.Repo
  end

  attributes do
    uuid_primary_key :id

    attribute :name, :string do
      allow_nil? false
      public? true
      constraints min_length: 2, max_length: 64
    end

    attribute :upstream_url, :string do
      allow_nil? false
      public? true
      constraints min_length: 8
    end

    attribute :status, :atom do
      constraints one_of: [:active, :draining, :offline]
      default :active
      allow_nil? false
      public? true
    end

    attribute :weight, :integer do
      default 100
      allow_nil? false
      public? true
      constraints min: 1, max: 100
    end

    timestamps()
  end

  calculations do
    calculate :high_priority, :boolean, expr(weight > 50)

    calculate :health_score, :float,
      ApiGateway.Config.Resources.Service.Calculations.HealthScore

    calculate :display_label, :string,
      ApiGateway.Config.Resources.Service.Calculations.DisplayLabel
  end

  actions do
    defaults [:create, :read, :update, :destroy]

    update :drain do
      change set_attribute(:status, :draining)
    end

    update :take_offline do
      change set_attribute(:status, :offline)
    end

    update :activate do
      change set_attribute(:status, :active)
    end

    read :active do
      filter expr(status == :active)
    end

    read :routable do
      filter expr(status in [:active, :draining])

      pagination do
        keyset? true
        default_limit 50
        countable true
      end
    end
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
    policy action_type(:read) do
      authorize_if actor_present()
    end

    policy action_type([:create, :update, :destroy]) do
      authorize_if actor_attribute_equals(:role, :admin)
    end
  end

  identities do
    identity :unique_name, [:name]
  end
end
```

### Step 7: PathFormat validation — `lib/api_gateway/config/resources/route_rule/validations/path_format.ex`

```elixir
defmodule ApiGateway.Config.Resources.RouteRule.Validations.PathFormat do
  @moduledoc "Validates that path_prefix starts with / and has no whitespace."

  use Ash.Resource.Validation

  @impl true
  def validate(changeset, _opts, _context) do
    path = Ash.Changeset.get_attribute(changeset, :path_prefix)

    cond do
      is_nil(path) -> :ok
      not String.starts_with?(path, "/") -> {:error, field: :path_prefix, message: "must start with /"}
      String.match?(path, ~r/\s/) -> {:error, field: :path_prefix, message: "must not contain whitespace"}
      true -> :ok
    end
  end
end
```

### Step 8: RouteRule resource — `lib/api_gateway/config/resources/route_rule.ex`

```elixir
defmodule ApiGateway.Config.Resources.RouteRule do
  @moduledoc "Maps inbound path prefix to a registered Service."

  use Ash.Resource,
    domain: ApiGateway.Config,
    data_layer: AshPostgres.DataLayer

  postgres do
    table "gateway_route_rules"
    repo ApiGateway.Repo
  end

  attributes do
    uuid_primary_key :id

    attribute :path_prefix, :string do
      allow_nil? false
      public? true
      constraints min_length: 1, max_length: 256
    end

    attribute :priority, :integer do
      allow_nil? false
      public? true
      constraints min: 1, max: 1000
    end

    attribute :active, :boolean, default: true, public?: true
    timestamps()
  end

  validations do
    validate {ApiGateway.Config.Resources.RouteRule.Validations.PathFormat, []}
  end

  actions do
    defaults [:create, :read, :update, :destroy]

    read :ordered do
      filter expr(active == true)
      prepare build(sort: [priority: :asc])
    end
  end

  relationships do
    belongs_to :service, ApiGateway.Config.Resources.Service do
      allow_nil? false
      public? true
    end
  end
end
```

### Step 9: Operator resource — `lib/api_gateway/config/resources/operator.ex`

```elixir
defmodule ApiGateway.Config.Resources.Operator do
  @moduledoc """
  A human operator who manages gateway configuration.
  Roles: :readonly (read only) and :admin (full CRUD).
  """

  use Ash.Resource,
    domain: ApiGateway.Config,
    data_layer: AshPostgres.DataLayer

  postgres do
    table "gateway_operators"
    repo ApiGateway.Repo
  end

  attributes do
    uuid_primary_key :id

    attribute :email, :string do
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

  actions do
    defaults [:read, :create, :destroy]

    update :promote_to_admin do
      change set_attribute(:role, :admin)
    end

    update :demote_to_readonly do
      change set_attribute(:role, :readonly)
    end
  end

  identities do
    identity :unique_email, [:email]
  end
end
```

### Step 10: Given tests — must pass without modification

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

### Step 11: Run the tests

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
| Queryable (filter/sort) | Yes (expr only) | No | Yes |

Reflection question: `calculate :health_score` calls `MetricsStore.get_metrics/1` for each service record. If a query returns 200 services, that is 200 GenServer calls. How would you redesign `HealthScore.calculate/3` to call a batch function once?

---

## Common production mistakes

**1. Returning `[values]` from `calculate/3` in the wrong order**
`calculate/3` receives `records` in query order and must return values in the same order. Sorting inside `calculate/3` silently assigns wrong values.

**2. `json_api` block without `public?: true` on attributes**
AshJsonApi exposes only public attributes. Omitted attributes are invisible in the REST response with no error.

**3. Policies that allow actions but not the data layer**
A policy that says "readonly can read" must also allow the underlying data layer read. Test with real actor structs.

**4. AshAuthentication token resource not in the domain**
If omitted from `resources do ... end`, token verification silently fails.

---

## Resources

- [AshJsonApi](https://hexdocs.pm/ash_json_api) — routes, JSON:API format, OpenAPI generation
- [AshAuthentication](https://hexdocs.pm/ash_authentication) — password strategy, tokens
- [Ash Calculations](https://hexdocs.pm/ash/calculations.html) — expr vs module, load dependencies
- [Ash Policies](https://hexdocs.pm/ash/policies.html) — authorize_if, actor_attribute_equals, actor_present
