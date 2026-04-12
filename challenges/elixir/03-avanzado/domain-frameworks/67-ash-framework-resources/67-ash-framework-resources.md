# Ash Framework — Resources and Actions

## Project context

You are building `api_gateway`, an internal HTTP gateway. The configuration management subsystem needs to expose service configuration — registered services, route rules, access policies — through a structured domain API that is queryable, paginatable, and generates its own database migrations. Ash Framework replaces imperative Ecto + hand-written action logic with a declarative resource DSL. All modules are defined from scratch.

Project structure:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── repo.ex                             # Ecto.Repo
│       └── config/
│           ├── config.ex                       # Ash.Domain
│           ├── resources/
│           │   ├── service.ex                  # Service resource
│           │   ├── route_rule.ex               # RouteRule resource
│           │   └── route_rule/
│           │       └── validations/
│           │           └── path_format.ex      # custom validation
│           └── queries.ex                      # composable Ash.Query helpers
├── test/
│   └── api_gateway/
│       └── config/
│           └── service_resource_test.exs       # given tests — must pass without modification
└── mix.exs
```

---

## The business problem

Three read-side operations the platform team needs constantly:

1. **Service registry**: list active services with their upstream URLs — filtered, sorted, paginated.
2. **Route matching**: given an inbound path, find the first matching `RouteRule`.
3. **Audit trail**: when a route rule changes, record who changed it and when.

Ash gives you a queryable, filterable, paginatable domain with validations enforced at the resource level, migrations auto-generated from your declarations, and a uniform API (`Config.create!/2`, `Config.read!/1`, `Config.update!/2`).

---

## Why declare resources instead of writing Ecto schemas manually

When you write an Ecto schema by hand, you also write: the changeset function, the validation logic, the query helpers, the CRUD functions in a context module, and the migration. Each of those is a separate file. Ash collapses all five into one resource declaration. The constraints on an attribute are both the migration column constraint and the runtime validation.

The tradeoff: you give up direct Ecto query control. For complex analytical queries, dropping down to raw Ecto or raw SQL is still the right call.

---

## Implementation

### Step 1: `mix.exs` additions

```elixir
defp deps do
  [
    {:ash, "~> 3.0"},
    {:ash_postgres, "~> 2.0"},
    {:ecto_sql, "~> 3.11"},
    {:postgrex, "~> 0.18"}
  ]
end
```

### Step 2: Domain — `lib/api_gateway/config/config.ex`

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
  end
end
```

### Step 3: Service resource — `lib/api_gateway/config/resources/service.ex`

```elixir
defmodule ApiGateway.Config.Resources.Service do
  @moduledoc """
  A backend service registered in the gateway.

  Attributes:
  - name: human-readable identifier, unique, 2-64 chars
  - upstream_url: HTTPS URL the gateway proxies to
  - status: :active | :draining | :offline
  - weight: relative weight for load-balancing, 1-100
  """

  use Ash.Resource,
    domain: ApiGateway.Config,
    data_layer: AshPostgres.DataLayer

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

  actions do
    defaults [:create, :read, :update, :destroy]

    update :drain do
      description "Marks the service as draining — no new requests routed to it."
      change set_attribute(:status, :draining)
    end

    update :take_offline do
      description "Marks the service as offline."
      change set_attribute(:status, :offline)
    end

    update :activate do
      description "Brings the service back to active status."
      change set_attribute(:status, :active)
    end

    read :active do
      description "Returns only services with status :active."
      filter expr(status == :active)
    end

    read :routable do
      description "Returns services eligible for routing (active or draining)."
      filter expr(status in [:active, :draining])

      pagination do
        keyset? true
        default_limit 50
        countable true
      end
    end
  end

  identities do
    identity :unique_name, [:name]
  end
end
```

The `drain`, `take_offline`, and `activate` actions use `change set_attribute/2` — a built-in Ash change that sets the attribute to a fixed value. The caller invokes these as:

```elixir
service
|> Ash.Changeset.for_update(:drain, %{})
|> Config.update!()
```

The `:active` and `:routable` read actions use `filter expr(...)` to apply server-side filters. The `identity :unique_name, [:name]` generates both a database unique index and a runtime uniqueness check.

### Step 4: RouteRule resource — `lib/api_gateway/config/resources/route_rule.ex`

```elixir
defmodule ApiGateway.Config.Resources.RouteRule do
  @moduledoc """
  A routing rule: maps an inbound path prefix to a registered Service.
  Priority determines match order — lower number = higher priority.
  """

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
      description "Returns active route rules in ascending priority order."
      filter expr(active == true)
      prepare build(sort: [priority: :asc])
    end

    read :for_path do
      description "Returns rules whose path_prefix is a prefix of the given path."
      argument :path, :string, allow_nil?: false
      filter expr(active == true and contains(^arg(:path), path_prefix))
      prepare build(sort: [priority: :asc], limit: 1)
    end

    update :reprioritize do
      description "Changes the priority of this rule."
      argument :new_priority, :integer do
        allow_nil? false
        constraints min: 1, max: 1000
      end

      change fn changeset, _ ->
        Ash.Changeset.change_attribute(
          changeset,
          :priority,
          Ash.Changeset.get_argument(changeset, :new_priority)
        )
      end
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

### Step 5: PathFormat validation — `lib/api_gateway/config/resources/route_rule/validations/path_format.ex`

```elixir
defmodule ApiGateway.Config.Resources.RouteRule.Validations.PathFormat do
  @moduledoc """
  Validates that path_prefix starts with "/" and contains no whitespace.
  """

  use Ash.Resource.Validation

  @impl true
  def validate(changeset, _opts, _context) do
    path = Ash.Changeset.get_attribute(changeset, :path_prefix)

    cond do
      is_nil(path) ->
        :ok

      not String.starts_with?(path, "/") ->
        {:error, field: :path_prefix, message: "must start with /"}

      String.match?(path, ~r/\s/) ->
        {:error, field: :path_prefix, message: "must not contain whitespace"}

      true ->
        :ok
    end
  end
end
```

### Step 6: Query helpers — `lib/api_gateway/config/queries.ex`

```elixir
defmodule ApiGateway.Config.Queries do
  @moduledoc "Composable Ash.Query helpers for common gateway configuration reads."

  import Ash.Query

  alias ApiGateway.Config.Resources.{Service, RouteRule}
  alias ApiGateway.Config

  @doc "Returns active services ordered by name ascending."
  @spec active_services() :: Ash.Query.t()
  def active_services do
    Service
    |> filter(status == :active)
    |> sort(name: :asc)
  end

  @doc "Returns all services for a given status."
  @spec services_by_status(atom()) :: Ash.Query.t()
  def services_by_status(status) do
    Service
    |> filter(status == ^status)
    |> sort(name: :asc)
  end

  @doc "Returns the highest-priority active rule matching `path`, or nil."
  @spec match_rule(String.t()) :: struct() | nil
  def match_rule(path) when is_binary(path) do
    result =
      RouteRule
      |> filter(active == true and contains(^path, path_prefix))
      |> sort(priority: :asc)
      |> limit(1)
      |> load([:service])
      |> Config.read!()

    case result do
      [rule | _] -> rule
      [] -> nil
    end
  end
end
```

### Step 7: Migration

```elixir
defmodule ApiGateway.Repo.Migrations.CreateGatewayConfig do
  use Ecto.Migration

  def change do
    create table(:gateway_services, primary_key: false) do
      add :id, :uuid, null: false, primary_key: true
      add :name, :string, null: false
      add :upstream_url, :string, null: false
      add :status, :string, null: false, default: "active"
      add :weight, :integer, null: false, default: 100
      timestamps(type: :utc_datetime_usec)
    end

    create unique_index(:gateway_services, [:name])
    create index(:gateway_services, [:status])

    create table(:gateway_route_rules, primary_key: false) do
      add :id, :uuid, null: false, primary_key: true
      add :path_prefix, :string, null: false
      add :priority, :integer, null: false
      add :active, :boolean, null: false, default: true
      add :service_id,
          references(:gateway_services, type: :uuid, on_delete: :restrict),
          null: false
      timestamps(type: :utc_datetime_usec)
    end

    create index(:gateway_route_rules, [:active, :priority])
    create index(:gateway_route_rules, [:service_id])
  end
end
```

### Step 8: Given tests — must pass without modification

```elixir
# test/api_gateway/config/service_resource_test.exs
defmodule ApiGateway.Config.Resources.ServiceTest do
  use ApiGateway.DataCase, async: true

  alias ApiGateway.Config
  alias ApiGateway.Config.Resources.Service

  defp create_service(attrs \\ %{}) do
    defaults = %{
      name: "svc-#{:rand.uniform(9_999_999)}",
      upstream_url: "https://upstream.internal/api",
      status: :active,
      weight: 100
    }

    Service
    |> Ash.Changeset.for_create(:create, Map.merge(defaults, attrs))
    |> Config.create!()
  end

  test "create/2 inserts a service and returns it" do
    svc = create_service(name: "auth-service", weight: 80)
    assert svc.name == "auth-service"
    assert svc.status == :active
    assert svc.weight == 80
  end

  test "create/2 fails when name is too short" do
    assert_raise Ash.Error.Invalid, fn ->
      create_service(name: "x")
    end
  end

  test "drain/1 sets status to :draining" do
    svc = create_service()

    updated =
      svc
      |> Ash.Changeset.for_update(:drain, %{})
      |> Config.update!()

    assert updated.status == :draining
  end

  test "take_offline/1 sets status to :offline" do
    svc = create_service()

    updated =
      svc
      |> Ash.Changeset.for_update(:take_offline, %{})
      |> Config.update!()

    assert updated.status == :offline
  end

  test "activate/1 restores :active after offline" do
    svc = create_service()

    offline =
      svc
      |> Ash.Changeset.for_update(:take_offline, %{})
      |> Config.update!()

    assert offline.status == :offline

    restored =
      offline
      |> Ash.Changeset.for_update(:activate, %{})
      |> Config.update!()

    assert restored.status == :active
  end

  test "read :active returns only active services" do
    _active = create_service(status: :active)
    draining = create_service(status: :draining)

    active_ids =
      Service
      |> Ash.Query.for_read(:active)
      |> Config.read!()
      |> Enum.map(& &1.id)

    refute draining.id in active_ids
  end

  test "path_format validation rejects path without leading slash" do
    svc = create_service()

    assert_raise Ash.Error.Invalid, fn ->
      ApiGateway.Config.Resources.RouteRule
      |> Ash.Changeset.for_create(:create, %{
        path_prefix: "api/v1",
        priority: 10,
        service_id: svc.id,
        active: true
      })
      |> Config.create!()
    end
  end
end
```

### Step 9: Run the tests

```bash
mix test test/api_gateway/config/ --trace
```

---

## Trade-off analysis

| Aspect | Ash resource | Hand-written Ecto + context | Raw SQL |
|--------|-------------|-----------------------------|---------| 
| Boilerplate | Minimal (one file per resource) | High (schema + changeset + context) | None |
| Query composability | Ash.Query DSL | Ecto.Query DSL | Raw string |
| Generated migrations | `mix ash_postgres.generate_migrations` | `mix ecto.gen.migration` (manual) | Manual |
| Complex joins/windows | Drops to raw Ecto | Native | Native |
| Learning curve | High (new DSL) | Medium | Low |

Reflection question: `identities do identity :unique_name, [:name] end` generates both a database unique index and a runtime uniqueness validation. If two processes call `Config.create!/2` with the same name at the same millisecond, what happens? Does Ash prevent the race or does the database constraint prevent it?

---

## Common production mistakes

**1. Calling resource modules directly instead of through the domain**
`ApiGateway.Config.Resources.Service.create!(attrs)` bypasses the domain. Always call `Config.create!/2`.

**2. Forgetting `public?: true` on attributes**
Attributes without `public?: true` are not exposed through AshJsonApi or AshGraphql.

**3. Using `mix ecto.gen.migration` for Ash resources**
Ash tracks its own resource schema separately. Use `mix ash_postgres.generate_migrations`.

**4. `constraints min: 0` does not mean optional**
`constraints min: 0` sets a lower bound on the value. `allow_nil?: false` controls whether the attribute can be absent. You need both.

---

## Resources

- [Ash Framework — Resources](https://hexdocs.pm/ash/resources.html) — attributes, actions, identities
- [AshPostgres — Migrations](https://hexdocs.pm/ash_postgres/migrations-and-tasks.html) — generate_migrations, snapshots
- [Ash.Query DSL](https://hexdocs.pm/ash/Ash.Query.html) — filter, sort, limit, load
- [Ash.Changeset](https://hexdocs.pm/ash/Ash.Changeset.html) — for_create, for_update, get_argument
- [Ash.Resource.Validation behaviour](https://hexdocs.pm/ash/Ash.Resource.Validation.html) — custom validation modules
