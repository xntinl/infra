# Ash Framework — Resources and Actions

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`. The configuration management subsystem is currently
a collection of ETS lookups and hand-written Ecto changesets. The platform team
wants to expose service configuration — registered services, route rules, access
policies — through a structured domain API that is queryable, paginatable, and
generates its own database migrations.

Ash Framework replaces imperative Ecto + hand-written action logic with a declarative
resource DSL. Instead of writing `Repo.insert` + changeset + validation functions
separately, you declare what a resource is and Ash generates the behaviour.

Project structure:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── config/
│       │   ├── config.ex                       # Ash.Domain
│       │   └── resources/
│       │       ├── service.ex                  # Service resource
│       │       ├── route_rule.ex               # RouteRule resource
│       │       └── route_rule/
│       │           └── validations/
│       │               └── path_format.ex      # custom validation
│       └── config/
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

1. **Service registry**: list active services with their upstream URLs and health status —
   filtered, sorted, paginated. Currently requires three different ETS lookups and a
   manual sort in application code.
2. **Route matching**: given an inbound path, find the first matching `RouteRule`.
   Currently a linear scan of a list loaded fresh from ETS on every request.
3. **Audit trail**: when a route rule changes (path added, priority shifted), record who
   changed it and when. Currently nothing — changes are silent.

Ash gives you a queryable, filterable, paginatable domain with validations enforced
at the resource level, migrations auto-generated from your declarations, and a uniform
API (`Config.create!/2`, `Config.read!/1`, `Config.update!/2`) that works identically
whether backed by PostgreSQL (production) or the in-memory data layer (tests).

---

## Why declare resources instead of writing Ecto schemas manually

When you write an Ecto schema by hand, you also write: the changeset function, the
validation logic, the query helpers, the CRUD functions in a context module, and the
migration. Each of those is a separate file. Adding a field means touching all five.

Ash collapses all five into one resource declaration. The constraints on an attribute
are both the migration column constraint and the runtime validation. The `actions` block
is both the public API and the changeset logic. When you add a field, you add one
`attribute` line and run `mix ash_postgres.generate_migrations`.

The tradeoff: you give up direct Ecto query control. Ash translates its expression DSL
to Ecto queries, and that translation is occasionally surprising. For complex analytical
queries (aggregations over joins with window functions), dropping down to raw Ecto or
raw SQL is still the right call.

---

## Implementation

### Step 1: `mix.exs` additions

```elixir
{:ash, "~> 3.0"},
{:ash_postgres, "~> 2.0"},
{:ecto_sql, "~> 3.11"},
{:postgrex, "~> 0.18"}
```

### Step 2: Domain — `lib/api_gateway/config/config.ex`

```elixir
defmodule ApiGateway.Config do
  @moduledoc """
  Ash domain for gateway configuration resources.

  All external code calls Config.create!/2, Config.read!/1, etc.
  Direct access to resource modules is an internal detail.
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

The `drain`, `take_offline`, and `activate` actions use `change set_attribute/2` — a
built-in Ash change that sets the attribute to a fixed value. No custom change module
is needed. The caller invokes these as:

```elixir
service
|> Ash.Changeset.for_update(:drain, %{})
|> Config.update!()
```

The `:active` and `:routable` read actions use `filter expr(...)` to apply server-side
filters. The `:routable` action also enables keyset pagination with a default limit of
50, which is suitable for dashboard table views.

The `identity :unique_name, [:name]` generates both a database unique index and a
runtime uniqueness check. The database constraint is the true guard against race
conditions; the Ash-level check provides a friendlier error message.

### Step 4: RouteRule resource — `lib/api_gateway/config/resources/route_rule.ex`

```elixir
defmodule ApiGateway.Config.Resources.RouteRule do
  @moduledoc """
  A routing rule: maps an inbound path prefix to a registered Service.

  Priority determines match order — lower number = higher priority.
  The gateway evaluates rules in ascending priority order and routes
  to the first matching service.
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

The `:ordered` read action combines a filter and a sort using `prepare build(...)`.
The `build` preparation is a built-in Ash preparation that applies query modifiers
at the action level.

The `:for_path` action takes a `:path` argument and uses `contains/2` in the filter
expression to find rules whose `path_prefix` is contained within the requested path.
The `limit: 1` ensures only the highest-priority match is returned.

The `:reprioritize` action demonstrates a custom change function. It reads the
`:new_priority` argument from the changeset and applies it to the `:priority` attribute.
This pattern keeps the action's intent explicit in the DSL while allowing computed
attribute changes.

### Step 5: PathFormat validation — `lib/api_gateway/config/resources/route_rule/validations/path_format.ex`

```elixir
defmodule ApiGateway.Config.Resources.RouteRule.Validations.PathFormat do
  @moduledoc """
  Validates that path_prefix starts with "/" and contains no whitespace.

  A route rule with path_prefix "/api/v1" matches requests to "/api/v1/users"
  but not to "api/v1/users" (missing leading slash). Whitespace in a path prefix
  would never match real HTTP requests.
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

The validation runs on every create and update action. The `cond` handles three cases:

1. **nil path**: allowed through — the `allow_nil? false` constraint on the attribute
   will catch this separately with its own error message.
2. **Missing leading slash**: returns a field-level error that Ash formats into the
   standard error structure.
3. **Contains whitespace**: `~r/\s/` matches any whitespace character (space, tab,
   newline). HTTP paths never contain whitespace, so this is always a mistake.

### Step 6: Query helpers — `lib/api_gateway/config/queries.ex`

```elixir
defmodule ApiGateway.Config.Queries do
  @moduledoc """
  Composable Ash.Query helpers for common gateway configuration reads.
  """

  import Ash.Query

  alias ApiGateway.Config.Resources.{Service, RouteRule}
  alias ApiGateway.Config

  @doc "Returns active services ordered by name ascending."
  def active_services do
    Service
    |> filter(status == :active)
    |> sort(name: :asc)
  end

  @doc "Returns all services for a given status."
  def services_by_status(status) do
    Service
    |> filter(status == ^status)
    |> sort(name: :asc)
  end

  @doc "Returns active route rules sorted by priority ascending."
  def ordered_rules do
    RouteRule
    |> filter(active == true)
    |> sort(priority: :asc)
  end

  @doc """
  Returns the highest-priority active rule whose path_prefix is a prefix of `path`.
  Returns nil if no rule matches.
  """
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

`match_rule/1` demonstrates a complete query composition: filter, sort, limit, and
relationship loading. The `contains/2` expression in Ash translates to a SQL `LIKE`
or `POSITION` check depending on the backend. The `load([:service])` preloads the
associated service in the same query, avoiding an N+1 if multiple rules are fetched.

### Step 7: Migration

Run `mix ash_postgres.generate_migrations --name create_gateway_config` to let Ash
generate this from the resource declarations. The output should be equivalent to:

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
| Learning curve | High (new DSL, new mental model) | Medium | Low |
| Multi-tenancy support | Built-in | Manual | Manual |
| Policy/authorisation | Built-in (`Ash.Policy.Authorizer`) | Manual | Manual |

Reflection question: `identities do identity :unique_name, [:name] end` generates both
a database unique index and a runtime uniqueness validation. If two processes call
`Config.create!/2` with the same name at the same millisecond, what happens? Does Ash
prevent the race or does the database constraint prevent it? What error does the caller
receive, and how does it differ from the error returned when validation fails at the
Ash layer before touching the database?

---

## Common production mistakes

**1. Calling resource modules directly instead of through the domain**
`ApiGateway.Config.Resources.Service.create!(attrs)` bypasses the domain. Policies,
hooks, and domain-level configuration do not apply. Always call `Config.create!/2`.

**2. Forgetting `public?: true` on attributes**
Attributes without `public?: true` are not exposed through AshJsonApi or AshGraphql.
The attribute exists in the database but is invisible to API consumers with no error
message to explain why.

**3. Using `mix ecto.gen.migration` for Ash resources**
Ash tracks its own resource schema separately from Ecto migrations. If you write a
migration by hand that Ash did not generate, `mix ash_postgres.generate_migrations`
will detect a drift and generate a conflicting migration.

**4. `constraints min: 0` does not mean optional**
`constraints min: 0` sets a lower bound on the value. `allow_nil?: false` controls
whether the attribute can be absent. You need both to express "required integer >= 0".

**5. Replaying actions in tests without resetting the domain**
If you use `async: true` with `DataCase` and multiple tests insert services with the
same name, the unique constraint fails. Use randomized names in test fixtures (as shown
above) or wrap each test in a transaction that is rolled back.

---

## Resources

- [Ash Framework — Resources](https://hexdocs.pm/ash/resources.html) — attributes, actions, identities
- [AshPostgres — Migrations](https://hexdocs.pm/ash_postgres/migrations-and-tasks.html) — generate_migrations, snapshots
- [Ash.Query DSL](https://hexdocs.pm/ash/Ash.Query.html) — filter, sort, limit, load
- [Ash.Changeset](https://hexdocs.pm/ash/Ash.Changeset.html) — for_create, for_update, get_argument
- [Ash.Resource.Validation behaviour](https://hexdocs.pm/ash/Ash.Resource.Validation.html) — custom validation modules
