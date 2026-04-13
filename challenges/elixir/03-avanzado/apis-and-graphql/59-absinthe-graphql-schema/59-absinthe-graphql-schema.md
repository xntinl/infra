# Absinthe GraphQL — Schema and Resolvers

## Project context

You are building `api_gateway`, an internal HTTP gateway. The gateway needs to expose an internal GraphQL API so the platform's dashboard can query service status and manage the service registry from a single endpoint. All modules are defined from scratch in this exercise.

Project structure:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── service_store.ex        # Agent-backed service registry (defined here)
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

REST forces the dashboard to make separate calls and join the data client-side. GraphQL lets the client declare exactly what it needs — one request, one response.

---

## Why GraphQL over REST here

REST exposes resources. The client gets what the server decided. For the dashboard — which has three different views (overview table, service detail, metrics panel) each needing different fields — this means either overfetching (GET /services returns 20 fields when the table only needs 3) or adding custom endpoints per view.

GraphQL inverts the contract: the client declares the shape. The server returns exactly that shape. For multi-view dashboards with varying data needs, this eliminates both overfetching and endpoint proliferation.

The trade-off: REST has transparent HTTP caching (ETags, CDN by URL). GraphQL queries all go to the same URL with POST, so HTTP cache is useless. You need application-level caching explicitly.

---

## Why resolvers always return `{:ok, value}` or `{:error, reason}`

Absinthe executes the entire query tree and collects all errors. A resolver returning `{:error, "not found"}` does not crash the query — it puts a null in that field and adds the error to the `errors` array in the response. The client receives a partial result with error context, not a 500.

---

## Design decisions

**Option A — naive/simple approach**
- Pros: minimal code, easy to reason about.
- Cons: breaks under load, lacks observability, hard to evolve.

**Option B — the approach used here** (chosen)
- Pros: production-grade, handles edge cases, testable boundaries.
- Cons: more moving parts, requires understanding of the BEAM primitives involved.

→ Chose **B** because correctness under concurrency and failure modes outweighs the extra surface area.

## Why this approach and not alternatives

Alternatives considered and discarded:

- **Hand-rolled equivalent**: reinvents primitives the BEAM/ecosystem already provides; high risk of subtle bugs around concurrency, timeouts, or failure propagation.
- **External service (e.g. Redis, sidecar)**: adds a network hop and an extra failure domain for a problem the VM can solve in-process with lower latency.
- **Heavier framework abstraction**: couples the module to a framework lifecycle and makes local reasoning/testing harder.

The chosen approach stays inside the BEAM, uses idiomatic OTP primitives, and keeps the contract small.

## Implementation

### Step 1: `mix.exs` — add Absinthe dependencies

**Objective**: Pin Absinthe, absinthe_plug, and absinthe_phoenix so HTTP transport, schema DSL, and subscriptions share one compatible version set.

```elixir
defp deps do
  [
    {:jason, "~> 1.4"},
    {:absinthe, "~> 1.7"},
    {:absinthe_plug, "~> 1.5"},
    {:absinthe_phoenix, "~> 2.0"}
  ]
end
```

### Step 2: `lib/api_gateway/service_store.ex`

**Objective**: Back the registry with an Agent keyed by service name so resolvers read and mutate state via a single serialization point, not ad-hoc ETS races.

This Agent-backed store provides the data layer for the GraphQL resolvers.

```elixir
defmodule ApiGateway.ServiceStore do
  @moduledoc """
  In-memory registry of backend services.
  Stores `%{name => %{name, url, health_path, registered_at}}`.
  """
  use Agent

  @spec start_link(keyword()) :: Agent.on_start()
  def start_link(_opts), do: Agent.start_link(fn -> %{} end, name: __MODULE__)

  @spec list() :: [map()]
  def list, do: Agent.get(__MODULE__, &Map.values/1)

  @spec get(String.t()) :: map() | nil
  def get(name), do: Agent.get(__MODULE__, &Map.get(&1, name))

  @spec register(map()) :: map()
  def register(attrs) do
    Agent.get_and_update(__MODULE__, fn services ->
      entry = Map.merge(attrs, %{"registered_at" => DateTime.utc_now() |> DateTime.to_iso8601()})
      {entry, Map.put(services, attrs["name"], entry)}
    end)
  end

  @spec deregister(String.t()) :: :ok | :error
  def deregister(name) do
    Agent.get_and_update(__MODULE__, fn services ->
      case Map.pop(services, name) do
        {nil, _} -> {:error, services}
        {_entry, rest} -> {:ok, rest}
      end
    end)
  end
end
```

### Step 3: `lib/api_gateway/graphql/types/scalars.ex`

**Objective**: Define a `:datetime` scalar with ISO 8601 parse/serialize so the wire format is canonical and invalid strings fail at validation, not in resolvers.

```elixir
defmodule ApiGateway.GraphQL.Types.Scalars do
  @moduledoc """
  Custom scalar types for the GraphQL schema.
  """
  use Absinthe.Schema.Notation

  scalar :datetime, description: "ISO 8601 datetime string" do
    serialize fn
      %DateTime{} = dt -> DateTime.to_iso8601(dt)
      iso when is_binary(iso) -> iso
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

### Step 4: `lib/api_gateway/graphql/types/service.ex`

**Objective**: Group object, input, queries, mutations, and subscriptions for `service` in one notation module so the contract reads top-to-bottom per entity.

```elixir
defmodule ApiGateway.GraphQL.Types.Service do
  @moduledoc """
  GraphQL types for backend services: object, input, queries, mutations.
  """
  use Absinthe.Schema.Notation

  @desc "A backend service registered in the gateway"
  object :service do
    field :name,            :string
    field :url,             :string
    field :health_path,     :string
    field :registered_at,   :string

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

### Step 5: `lib/api_gateway/graphql/resolvers/service.ex`

**Objective**: Convert atom-keyed Absinthe inputs to string keys at the resolver boundary so the store stays ignorant of GraphQL's key conventions.

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

  @spec list_services(any(), map(), Absinthe.Resolution.t()) :: {:ok, [map()]}
  def list_services(_, _, _) do
    {:ok, ServiceStore.list()}
  end

  @spec get_service(any(), %{name: String.t()}, Absinthe.Resolution.t()) ::
          {:ok, map()} | {:error, String.t()}
  def get_service(_, %{name: name}, _) do
    case ServiceStore.get(name) do
      nil -> {:error, "service #{name} not found"}
      service -> {:ok, service}
    end
  end

  @spec register_service(any(), %{input: map()}, Absinthe.Resolution.t()) :: {:ok, map()}
  def register_service(_, %{input: input}, _) do
    # Absinthe delivers input as an atom-keyed map: %{name: "x", url: "y"}.
    # ServiceStore expects string keys. Convert at the resolver boundary.
    string_keyed =
      input
      |> convert_to_map()
      |> Enum.into(%{}, fn {k, v} -> {to_string(k), v} end)

    service = ServiceStore.register(string_keyed)
    {:ok, service}
  end

  @spec deregister_service(any(), %{name: String.t()}, Absinthe.Resolution.t()) ::
          {:ok, boolean()} | {:error, String.t()}
  def deregister_service(_, %{name: name}, _) do
    case ServiceStore.deregister(name) do
      :ok -> {:ok, true}
      :error -> {:error, "service #{name} not found"}
    end
  end

  # Handles both plain maps and structs uniformly
  defp convert_to_map(%_{} = struct), do: Map.from_struct(struct)
  defp convert_to_map(map) when is_map(map), do: map
end
```

### Step 6: `lib/api_gateway/graphql/schema.ex`

**Objective**: Compose the root schema via `import_fields` so per-entity modules plug in without touching the root, keeping schema growth linear in domains.

```elixir
defmodule ApiGateway.GraphQL.Schema do
  @moduledoc """
  Root GraphQL schema for the api_gateway.
  Imports all type modules and wires query, mutation, and subscription roots.
  """
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

### Step 7: Given tests — must pass without modification

**Objective**: Exercise list, get, register, deregister, and unknown-service paths through `Absinthe.run/3` so resolver wiring is proven end-to-end.

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

  describe "ApiGateway.GraphQL.Schema" do
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
end
```

### Step 8: Run the tests

**Objective**: Run `mix test --trace` on the schema suite so failures surface with explicit test names, not buried in a batched report.

```bash
mix test test/api_gateway/graphql_schema_test.exs --trace
```

---


### Why this works

The design leans on BEAM guarantees (process isolation, mailbox ordering, supervisor restarts) and pushes invariants to the boundaries of each module. State transitions are explicit, failure modes are declared rather than implicit, and each step is independently testable. That combination keeps the implementation correct under concurrent load and cheap to change later.

## Deep Dive: Query Complexity and N+1 Prevention Patterns

GraphQL's flexibility is a double-edged sword. A query like `{ users { posts { comments { author { email } } } } }`
becomes a DDoS vector if unchecked: a resolver that loads each post's comments naively yields 1000 database 
queries for a 100-user query.

**Three strategies to prevent N+1**:
1. **Dataloader batching** (Absinthe-native): Queue fields in phase 1 (`load/3`), flush in phase 2 (`run/1`).
   Single database call per level. Works across HTTP boundaries via custom sources.
2. **Ecto select/5 eager loading** (preload): Best when schema relationships are known at resolver definition time.
   Fine-grained control; requires discipline in your types.
3. **Complexity analysis** (persisted queries): Assign a "weight" to each field (users=2, posts=5, comments=10).
   Reject queries exceeding a threshold BEFORE execution. Prevents runaway queries entirely.

**Production gotcha**: Complexity analysis doesn't prevent slow queries — it prevents expensive queries.
A query that hits 50,000 database rows but under the complexity limit still runs. Combine with database 
query timeouts and active monitoring.

**Subscription patterns** (real-time): Subscriptions over PubSub break traditional Dataloader batching 
because events arrive asynchronously. Use a separate resolver that doesn't call the loader; instead, 
publish (source) and subscribe (sink) directly. This keeps subscriptions cheap and doesn't starve 
the dataloader queue.

**Field-level authorization**: Dataloader sources can enforce per-user visibility rules at load time, 
not in the resolver. This is cleaner than filtering after the fact and reduces unnecessary database 
queries for unauthorized fields.

---

## Advanced Considerations

API implementations at scale require careful consideration of request handling, error responses, and the interaction between multiple clients with different performance expectations. The distinction between public APIs and internal APIs affects error reporting granularity, versioning strategies, and backwards compatibility guarantees fundamentally. Versioning APIs through headers, paths, or query parameters each have trade-offs in terms of maintenance burden, client complexity, and developer experience across multiple client versions. When deprecating API endpoints, the migration window and support period must balance client migration costs with infrastructure maintenance costs and team capacity constraints.

GraphQL adds complexity around query costs, depth limits, and the interaction between nested resolvers and N+1 query problems. A deeply nested GraphQL query can trigger hundreds of database queries if not carefully managed with proper preloading and query analysis. Implementing query cost analysis prevents malicious or poorly-written queries from starving resources and degrading service for other clients. The caching layer becomes more complex with GraphQL because the same data may be accessed through multiple query paths, each with different caching semantics and TTL requirements that must be carefully coordinated at the application level.

Error handling and status codes require careful design to balance information disclosure with security concerns. Too much detail in error messages helps attackers; too little detail frustrates legitimate users. Implement structured error responses with specific error codes that clients can use to handle different failure scenarios intelligently and retry appropriately. Rate limiting, circuit breakers, and backpressure mechanisms prevent API overload but require careful configuration based on expected traffic patterns and SLA requirements.


## Trade-off analysis

| Aspect | GraphQL (Absinthe) | REST (Plug.Router) |
|--------|-------------------|-------------------|
| Client control | Client chooses fields | Server decides shape |
| HTTP caching | None (POST /graphql) | ETags, CDN by URL |
| Type safety | Schema is the contract | OpenAPI optional |
| N+1 risk | High (mitigated by DataLoader) | Explicit per endpoint |
| Subscriptions | Built-in via WebSocket | SSE or polling |
| Learning curve | Higher | Lower |

Reflection question: the schema has both a `list_services` query and the `register_service` mutation publishes to a subscription. What happens if a subscriber is connected when a new service registers — and what happens if no subscriber is connected? Is data lost?

---

## Common production mistakes

**1. Resolvers returning raw values instead of `{:ok, value}`**
A resolver that returns `service` (not `{:ok, service}`) will produce an error like `"Expected {:ok, _} or {:error, _} from resolver"`. Absinthe is strict about this contract.

**2. `input_object` fields with atom keys in resolvers**
Absinthe converts input arguments to atom-keyed maps. `input.name` works; `input["name"]` does not. The confusion arises because the ServiceStore uses string keys. Always convert at the resolver boundary, not deeper in the domain.

**3. Mutations without auth middleware**
Any mutation that changes state must be gated behind the gateway's auth. The cleanest place is Absinthe middleware — `middleware MyApp.Middleware.Authenticate` before `resolve`. If you add it field by field you will inevitably miss one.

**4. Large schemas in a single file**
`import_types` lets you split types into modules. A 1000-line `schema.ex` is a maintenance problem. Split by domain concept from the start.

---

## Benchmark

```elixir
# Minimal measurement — replace with Benchee for distribution stats.
{time_us, _} = :timer.tc(fn ->
  for _ <- 1..10_000, do: run_operation()
end)
IO.puts("avg: #{time_us / 10_000} µs/op")
```

Target: operation should complete in the low-microsecond range on modern hardware; deviations by >2× indicate a regression worth investigating.

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?

## Resources

- [Absinthe docs](https://hexdocs.pm/absinthe) — the canonical reference for schema, types, resolvers
- [Absinthe.Schema.Notation](https://hexdocs.pm/absinthe/Absinthe.Schema.Notation.html) — `object`, `input_object`, `field`, `arg`
- [Absinthe subscriptions](https://hexdocs.pm/absinthe/subscriptions.html) — WebSocket-based real-time updates
- [GraphQL spec](https://spec.graphql.org/) — understand why partial responses and the `errors` array exist

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```
