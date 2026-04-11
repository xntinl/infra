# Absinthe GraphQL — Schema and Resolvers

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`. The gateway now needs to expose an internal GraphQL API
so the platform's dashboard can query service status, route configurations, and request
metrics from a single endpoint — without the N separate REST calls it would need today.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       ├── router.ex               # already exists — REST routes
│       └── graphql/
│           ├── schema.ex           # root schema
│           ├── types/
│           │   ├── service.ex      # service object, input, queries, mutations
│           │   └── scalars.ex      # custom :datetime scalar
│           └── resolvers/
│               └── service.ex      # resolver functions
├── test/
│   └── api_gateway/
│       └── graphql_schema_test.exs # given tests — must pass without modification
└── mix.exs
```

---

## The business problem

The dashboard team needs a single endpoint to:

1. List all registered services with their name, URL, and status
2. Get a specific service by name
3. Register a new service via mutation
4. Deregister a service via mutation
5. Subscribe to service registration events in real time

REST forces the dashboard to make separate calls and join the data client-side.
GraphQL lets the client declare exactly what it needs — one request, one response.

---

## Why GraphQL over REST here

REST exposes resources. The client gets what the server decided. For the dashboard —
which has three different views (overview table, service detail, metrics panel) each
needing different fields — this means either overfetching (GET /services returns 20
fields when the table only needs 3) or adding custom endpoints per view.

GraphQL inverts the contract: the client declares the shape. The server returns exactly
that shape. For multi-view dashboards with varying data needs, this eliminates both
overfetching and endpoint proliferation.

The trade-off: REST has transparent HTTP caching (ETags, CDN by URL). GraphQL queries
all go to the same URL with POST, so HTTP cache is useless. You need application-level
caching (persisted queries, DataLoader) explicitly.

---

## Why resolvers always return `{:ok, value}` or `{:error, reason}`

Absinthe executes the entire query tree and collects all errors. A resolver returning
`{:error, "not found"}` does not crash the query — it puts a null in that field and
adds the error to the `errors` array in the response. The client receives a partial
result with error context, not a 500. This is intentional GraphQL semantics: partial
responses are valid.

---

## Implementation

### Step 1: `mix.exs` — add Absinthe dependencies

```elixir
defp deps do
  [
    # existing deps...
    {:absinthe, "~> 1.7"},
    {:absinthe_plug, "~> 1.5"},
    {:absinthe_phoenix, "~> 2.0"}  # for subscriptions via Phoenix Channels
  ]
end
```

### Step 2: `lib/api_gateway/graphql/types/scalars.ex`

```elixir
defmodule ApiGateway.GraphQL.Types.Scalars do
  use Absinthe.Schema.Notation

  scalar :datetime, description: "ISO 8601 datetime string" do
    serialize fn
      %DateTime{} = dt -> DateTime.to_iso8601(dt)
    end

    parse fn
      %Absinthe.Blueprint.Input.String{value: value} ->
        case DateTime.from_iso8601(value) do
          {:ok, dt, _} -> {:ok, dt}
          {:error, _}  -> :error
        end
      _ -> :error
    end
  end
end
```

### Step 3: `lib/api_gateway/graphql/types/service.ex`

```elixir
defmodule ApiGateway.GraphQL.Types.Service do
  use Absinthe.Schema.Notation

  @desc "A backend service registered in the gateway"
  object :service do
    field :name,            :string
    field :url,             :string
    field :health_path,     :string
    field :registered_at,   :datetime

    field :status, :string do
      resolve fn service, _, _ ->
        health_path = service["health_path"] || service["url"]

        if health_path do
          {:ok, "healthy"}
        else
          {:ok, "unknown"}
        end
      end
    end
  end

  input_object :service_input do
    field :name,        non_null(:string)
    field :url,         non_null(:string)
    field :health_path, :string
  end

  object :service_queries do
    @desc "List all registered services"
    field :services, list_of(:service) do
      resolve &ApiGateway.GraphQL.Resolvers.Service.list_services/3
    end

    @desc "Get a service by name"
    field :service, :service do
      arg :name, non_null(:string)
      resolve &ApiGateway.GraphQL.Resolvers.Service.get_service/3
    end
  end

  object :service_mutations do
    field :register_service, :service do
      arg :input, non_null(:service_input)
      resolve &ApiGateway.GraphQL.Resolvers.Service.register_service/3
    end

    field :deregister_service, :boolean do
      arg :name, non_null(:string)
      resolve &ApiGateway.GraphQL.Resolvers.Service.deregister_service/3
    end
  end

  object :service_subscriptions do
    field :service_registered, :service do
      config fn _, _ -> {:ok, topic: "services:registered"} end
    end
  end
end
```

The `:status` field uses an inline resolver. The resolver receives the service map
(which has string keys from the `ServiceStore`), checks if a `health_path` is set,
and returns a status string. In a production system this would make an HTTP health
check; here we return `"healthy"` as a default since health checking is covered by
the DataLoader exercise.

### Step 4: `lib/api_gateway/graphql/resolvers/service.ex`

```elixir
defmodule ApiGateway.GraphQL.Resolvers.Service do
  @moduledoc """
  Resolver functions for the service queries and mutations.

  Each function receives three arguments:
    1. parent — the parent object (unused for root queries/mutations)
    2. args   — the arguments from the GraphQL query (atom-keyed map)
    3. resolution — the Absinthe resolution struct (contains context)

  Every resolver must return {:ok, value} or {:error, reason}.
  """
  alias ApiGateway.ServiceStore

  def list_services(_, _, _) do
    {:ok, ServiceStore.list()}
  end

  def get_service(_, %{name: name}, _) do
    case ServiceStore.get(name) do
      nil -> {:error, "service #{name} not found"}
      service -> {:ok, service}
    end
  end

  def register_service(_, %{input: input}, _) do
    # Absinthe delivers input as an atom-keyed map: %{name: "x", url: "y"}.
    # ServiceStore expects string keys. Convert at the resolver boundary.
    string_keyed =
      input
      |> Map.from_struct_or_map()
      |> Enum.into(%{}, fn {k, v} -> {to_string(k), v} end)

    service = ServiceStore.register(string_keyed)

    # Publish to subscription topic so connected subscribers are notified
    Absinthe.Subscription.publish(
      ApiGateway.Endpoint,
      service,
      service_registered: "services:registered"
    )

    {:ok, service}
  end

  def deregister_service(_, %{name: name}, _) do
    case ServiceStore.deregister(name) do
      :ok -> {:ok, true}
      :error -> {:error, "service #{name} not found"}
    end
  end

  # Handles both plain maps and structs uniformly
  defp map_from_struct_or_map(%_{} = struct), do: Map.from_struct(struct)
  defp map_from_struct_or_map(map) when is_map(map), do: map
end
```

The `register_service/3` resolver converts atom-keyed input from Absinthe to
string-keyed maps for the `ServiceStore`. This conversion happens at the resolver
boundary — the domain layer (ServiceStore) should not need to know about Absinthe's
conventions. After registration, `Absinthe.Subscription.publish/3` notifies any
connected WebSocket subscribers on the `"services:registered"` topic.

The `deregister_service/3` resolver maps `ServiceStore.deregister/1` results to the
GraphQL contract: `:ok` becomes `{:ok, true}` and `:error` becomes an error tuple
with a descriptive message that appears in the response's `errors` array.

### Step 5: `lib/api_gateway/graphql/schema.ex`

```elixir
defmodule ApiGateway.GraphQL.Schema do
  use Absinthe.Schema

  import_types ApiGateway.GraphQL.Types.Scalars
  import_types ApiGateway.GraphQL.Types.Service

  query do
    import_fields :service_queries
  end

  mutation do
    import_fields :service_mutations
  end

  subscription do
    import_fields :service_subscriptions
  end
end
```

### Step 6: Mount the GraphQL endpoint in the router

```elixir
# In ApiGateway.Router (add after existing routes)
forward "/graphql", Absinthe.Plug,
  schema: ApiGateway.GraphQL.Schema

# Development only — interactive query editor
if Mix.env() == :dev do
  forward "/graphiql", Absinthe.Plug.GraphiQL,
    schema: ApiGateway.GraphQL.Schema
end
```

### Step 7: Given tests — must pass without modification

```elixir
# test/api_gateway/graphql_schema_test.exs
defmodule ApiGateway.GraphQL.SchemaTest do
  use ExUnit.Case, async: false

  alias ApiGateway.GraphQL.Schema

  setup do
    Agent.update(ApiGateway.ServiceStore, fn _ -> %{} end)
    :ok
  end

  @list_query """
  query {
    services {
      name
      url
    }
  }
  """

  @get_query """
  query GetService($name: String!) {
    service(name: $name) {
      name
      url
    }
  }
  """

  @register_mutation """
  mutation Register($input: ServiceInput!) {
    registerService(input: $input) {
      name
      url
    }
  }
  """

  @deregister_mutation """
  mutation Deregister($name: String!) {
    deregisterService(name: $name)
  }
  """

  test "lists registered services" do
    ApiGateway.ServiceStore.register(%{"name" => "payments", "url" => "http://payments:4001"})

    assert {:ok, %{data: %{"services" => services}}} =
             Absinthe.run(@list_query, Schema)

    assert length(services) == 1
    assert hd(services)["name"] == "payments"
  end

  test "returns error for unknown service" do
    assert {:ok, %{data: %{"service" => nil}, errors: [error]}} =
             Absinthe.run(@get_query, Schema, variables: %{"name" => "ghost"})

    assert error.message =~ "not found"
  end

  test "registers a service via mutation" do
    assert {:ok, %{data: %{"registerService" => svc}}} =
             Absinthe.run(@register_mutation, Schema,
               variables: %{"input" => %{"name" => "geo", "url" => "http://geo:4002"}}
             )

    assert svc["name"] == "geo"
    assert ApiGateway.ServiceStore.get("geo") != nil
  end

  test "deregisters a service via mutation" do
    ApiGateway.ServiceStore.register(%{"name" => "cache", "url" => "http://cache:4003"})

    assert {:ok, %{data: %{"deregisterService" => true}}} =
             Absinthe.run(@deregister_mutation, Schema, variables: %{"name" => "cache"})

    assert ApiGateway.ServiceStore.get("cache") == nil
  end

  test "deregister returns error for unknown service" do
    assert {:ok, %{errors: [_]}} =
             Absinthe.run(@deregister_mutation, Schema, variables: %{"name" => "ghost"})
  end
end
```

### Step 8: Run the tests

```bash
mix test test/api_gateway/graphql_schema_test.exs --trace
```

---

## Trade-off analysis

| Aspect | GraphQL (Absinthe) | REST (Plug.Router) |
|--------|-------------------|-------------------|
| Client control | Client chooses fields | Server decides shape |
| HTTP caching | None (POST /graphql) | ETags, CDN by URL |
| Type safety | Schema is the contract | OpenAPI optional |
| N+1 risk | High (mitigated by DataLoader, next exercise) | Explicit per endpoint |
| Subscriptions | Built-in via WebSocket | SSE or polling |
| Learning curve | Higher | Lower |

Reflection question: the schema has both a `list_services` query and the `register_service`
mutation publishes to a subscription. What happens if a subscriber is connected when a new
service registers — and what happens if no subscriber is connected? Is data lost?

---

## Common production mistakes

**1. Resolvers returning raw values instead of `{:ok, value}`**
A resolver that returns `service` (not `{:ok, service}`) will produce an error like
`"Expected {:ok, _} or {:error, _} from resolver"`. Absinthe is strict about this contract.

**2. Subscriptions without the `Absinthe.Middleware.Dataloader` plugin**
If you add DataLoader later (next exercise) but forget to add it to `plugins/0` in the
schema, batching silently does not happen and you get N+1 queries.

**3. `input_object` fields with atom keys in resolvers**
Absinthe converts input arguments to atom-keyed maps. `input.name` works; `input["name"]`
does not. The confusion arises because the ServiceStore uses string keys. Always convert
at the resolver boundary, not deeper in the domain.

**4. Mutations without auth middleware**
Any mutation that changes state must be gated behind the gateway's auth. The cleanest
place is Absinthe middleware — `middleware MyApp.Middleware.Authenticate` before
`resolve`. If you add it field by field you will inevitably miss one.

**5. Large schemas in a single file**
`import_types` lets you split types into modules. A 1000-line `schema.ex` is a maintenance
problem. Split by domain concept from the start: `Types.Service`, `Types.Metrics`,
`Resolvers.Service`, etc.

---

## Resources

- [Absinthe docs](https://hexdocs.pm/absinthe) — the canonical reference for schema, types, resolvers
- [Absinthe.Schema.Notation](https://hexdocs.pm/absinthe/Absinthe.Schema.Notation.html) — `object`, `input_object`, `field`, `arg`
- [Absinthe subscriptions](https://hexdocs.pm/absinthe/subscriptions.html) — WebSocket-based real-time updates
- [GraphQL spec](https://spec.graphql.org/) — understand why partial responses and the `errors` array exist
