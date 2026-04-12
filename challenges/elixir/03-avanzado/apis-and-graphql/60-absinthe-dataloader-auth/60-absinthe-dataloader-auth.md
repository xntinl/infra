# Absinthe DataLoader and Auth Middleware

## Project context

You are building `api_gateway`, an internal HTTP gateway with a GraphQL API. This exercise adds DataLoader-based batching for the service health check field and authentication middleware that gates mutations. All modules — including the service store, schema, types, and resolvers — are defined from scratch here.

Project structure:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── service_store.ex                # Agent-backed service registry (defined here)
│       └── graphql/
│           ├── schema.ex                   # root schema with plugins/0 and middleware/3
│           ├── types/
│           │   └── service.ex              # service types with DataLoader status field
│           ├── resolvers/
│           │   └── service.ex              # resolver functions
│           ├── middleware/
│           │   ├── authenticate.ex         # gates mutations behind auth
│           │   └── handle_errors.ex        # normalizes error formats
│           └── loader.ex                   # DataLoader KV source for health checks
├── test/
│   └── api_gateway/
│       └── graphql_auth_test.exs           # given tests — must pass without modification
└── mix.exs
```

---

## The business problem

Two problems compound each other:

**N+1 health checks**: the `status` field on each service calls an HTTP health endpoint. When the dashboard lists 50 services, that is 50 sequential HTTP calls. DataLoader batches them: collect all health check URLs during the resolution of one GraphQL level, fire them concurrently, resolve all 50 fields from the single batched result.

**Unauthenticated mutations**: `registerService` and `deregisterService` have no auth. Any caller can deregister production services. The auth middleware reads `current_client` from the Absinthe context and aborts before the resolver runs if it is missing.

---

## Why DataLoader and not `Task.async_stream`

You could batch health checks with `Task.async_stream` in the list resolver. But this couples batching logic to the list resolver — the detail resolver does not benefit. DataLoader operates at the GraphQL execution level, collecting all pending loads across the entire query tree, then dispatching them before moving to the next resolution level.

---

## Why middleware instead of resolver-level auth checks

Without middleware, every mutation needs a guard clause. Add 10 mutations and you have 10 copies of the same pattern. With `middleware/3` in the schema, auth is applied to every mutation automatically at schema definition time.

---

## Design decisions

**Option A — naive/simple approach**
- Pros: minimal code, easy to reason about.
- Cons: breaks under load, lacks observability, hard to evolve.

**Option B — the approach used here** (chosen)
- Pros: production-grade, handles edge cases, testable boundaries.
- Cons: more moving parts, requires understanding of the BEAM primitives involved.

→ Chose **B** because correctness under concurrency and failure modes outweighs the extra surface area.

## Implementation

### Step 1: `mix.exs`

```elixir
defp deps do
  [
    {:jason, "~> 1.4"},
    {:absinthe, "~> 1.7"},
    {:absinthe_plug, "~> 1.5"},
    {:dataloader, "~> 2.0"}
  ]
end
```

### Step 2: `lib/api_gateway/service_store.ex`

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

### Step 3: `lib/api_gateway/graphql/loader.ex`

```elixir
defmodule ApiGateway.GraphQL.Loader do
  @moduledoc """
  DataLoader sources for api_gateway.
  Uses KV source (not Ecto) because the gateway's data lives in ETS and
  external HTTP endpoints, not a relational database.
  """

  @spec health_source() :: Dataloader.KV.t()
  def health_source do
    Dataloader.KV.new(&fetch_health/2)
  end

  # fetch_health/2 receives {:health, urls_set} where urls_set is a MapSet
  # of service URLs that need checking. Returns %{url => status_string}.
  defp fetch_health(:health, urls) do
    urls
    |> Task.async_stream(
      fn url ->
        try do
          case :httpc.request(:get, {to_charlist(url), []}, [timeout: 3_000], []) do
            {:ok, {{_, status, _}, _, _}} when status in 200..299 ->
              {url, "healthy"}

            _ ->
              {url, "unreachable"}
          end
        rescue
          _ -> {url, "unreachable"}
        catch
          _, _ -> {url, "unreachable"}
        end
      end,
      max_concurrency: 20,
      timeout: 5_000,
      on_timeout: :kill_task
    )
    |> Enum.reduce(%{}, fn
      {:ok, {url, status}}, acc -> Map.put(acc, url, status)
      {:exit, _}, acc -> acc
    end)
  end
end
```

### Step 4: `lib/api_gateway/graphql/middleware/authenticate.ex`

```elixir
defmodule ApiGateway.GraphQL.Middleware.Authenticate do
  @moduledoc """
  Absinthe middleware that aborts resolution if no authenticated client
  is present in the context.

  The context key :current_client must be set by the caller (e.g., from
  a Plug pipeline that validates X-Client-ID). This middleware reads it.
  """
  @behaviour Absinthe.Middleware

  @impl true
  def call(%{context: %{current_client: client}} = resolution, _opts)
      when not is_nil(client) do
    resolution
  end

  def call(resolution, _opts) do
    resolution
    |> Absinthe.Resolution.put_result({:error, "authentication required"})
  end
end
```

### Step 5: `lib/api_gateway/graphql/middleware/handle_errors.ex`

```elixir
defmodule ApiGateway.GraphQL.Middleware.HandleErrors do
  @moduledoc """
  Post-resolver middleware that normalizes error formats.
  Converts raw strings and Exception structs to the map format
  Absinthe uses for the `errors` array in the response.
  """
  @behaviour Absinthe.Middleware

  @impl true
  def call(%{errors: []} = resolution, _opts), do: resolution

  def call(%{errors: errors} = resolution, _opts) do
    normalized = Enum.map(errors, fn
      msg when is_binary(msg) -> %{message: msg, extensions: %{code: "ERROR"}}
      error -> %{message: Exception.message(error), extensions: %{code: "INTERNAL"}}
    end)

    %{resolution | errors: normalized}
  end
end
```

### Step 6: `lib/api_gateway/graphql/types/service.ex`

```elixir
defmodule ApiGateway.GraphQL.Types.Service do
  @moduledoc """
  GraphQL types for backend services: object, input, queries, mutations.
  The :status field uses DataLoader to batch health checks across services.
  """
  use Absinthe.Schema.Notation

  @desc "A backend service registered in the gateway"
  object :service do
    field :name,          :string
    field :url,           :string
    field :health_path,   :string
    field :registered_at, :string

    field :status, :string do
      resolve fn service, _, %{context: %{loader: loader}} ->
        url = service["url"]

        if url do
          loader
          |> Dataloader.load(:health, :health, url)
          |> Absinthe.Resolution.Helpers.on_load(fn loader ->
            status = Dataloader.get(loader, :health, :health, url)
            {:ok, status || "unknown"}
          end)
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
end
```

### Step 7: `lib/api_gateway/graphql/resolvers/service.ex`

```elixir
defmodule ApiGateway.GraphQL.Resolvers.Service do
  @moduledoc """
  Resolver functions for the service queries and mutations.
  """
  alias ApiGateway.ServiceStore

  @spec list_services(any(), map(), Absinthe.Resolution.t()) :: {:ok, [map()]}
  def list_services(_, _, _), do: {:ok, ServiceStore.list()}

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

  defp convert_to_map(%_{} = struct), do: Map.from_struct(struct)
  defp convert_to_map(map) when is_map(map), do: map
end
```

### Step 8: `lib/api_gateway/graphql/schema.ex`

```elixir
defmodule ApiGateway.GraphQL.Schema do
  @moduledoc """
  Root GraphQL schema for the api_gateway with DataLoader and auth middleware.
  """
  use Absinthe.Schema

  import_types ApiGateway.GraphQL.Types.Service

  query do
    import_fields :service_queries
  end

  mutation do
    import_fields :service_mutations
  end

  @doc """
  Called once per request to build the Absinthe context.
  Initializes DataLoader with the health check source.
  """
  def context(ctx) do
    loader =
      Dataloader.new()
      |> Dataloader.add_source(:health, ApiGateway.GraphQL.Loader.health_source())

    ctx
    |> Map.put(:loader, loader)
  end

  @doc """
  Required to activate DataLoader batching. Without this declaration,
  DataLoader fields silently resolve to nil.
  """
  def plugins do
    [Absinthe.Middleware.Dataloader | Absinthe.Plugin.defaults()]
  end

  @doc """
  Called for every field in the schema. Applies Authenticate to all
  mutations and HandleErrors to everything.
  """
  def middleware(middleware, _field, %Absinthe.Type.Object{identifier: :mutation}) do
    [ApiGateway.GraphQL.Middleware.Authenticate | middleware] ++
      [ApiGateway.GraphQL.Middleware.HandleErrors]
  end

  def middleware(middleware, _field, _object) do
    middleware ++ [ApiGateway.GraphQL.Middleware.HandleErrors]
  end
end
```

The `middleware/3` callback is invoked at schema compilation time for every field. It pattern-matches on `%Absinthe.Type.Object{identifier: :mutation}` to add `Authenticate` only to mutation fields. Read queries remain accessible without auth.

### Step 9: Given tests — must pass without modification

```elixir
# test/api_gateway/graphql_auth_test.exs
defmodule ApiGateway.GraphQL.AuthTest do
  use ExUnit.Case, async: false

  alias ApiGateway.GraphQL.Schema

  setup do
    Agent.update(ApiGateway.ServiceStore, fn _ -> %{} end)
    :ok
  end

  @register_mutation """
  mutation Register($input: ServiceInput!) {
    registerService(input: $input) {
      name
    }
  }
  """

  @deregister_mutation """
  mutation Deregister($name: String!) {
    deregisterService(name: $name)
  }
  """

  test "mutation without auth returns authentication error" do
    assert {:ok, result} =
             Absinthe.run(@register_mutation, Schema,
               variables: %{"input" => %{"name" => "x", "url" => "http://x"}},
               context: %{}
             )

    assert [error] = result.errors
    assert error.message =~ "authentication"
  end

  test "mutation with valid client succeeds" do
    assert {:ok, result} =
             Absinthe.run(@register_mutation, Schema,
               variables: %{"input" => %{"name" => "payments", "url" => "http://payments:4001"}},
               context: %{current_client: "dashboard"}
             )

    refute Map.has_key?(result, :errors)
    assert result.data["registerService"]["name"] == "payments"
  end

  test "deregister without auth returns authentication error" do
    ApiGateway.ServiceStore.register(%{"name" => "geo", "url" => "http://geo:4002"})

    assert {:ok, result} =
             Absinthe.run(@deregister_mutation, Schema,
               variables: %{"name" => "geo"},
               context: %{}
             )

    assert [error] = result.errors
    assert error.message =~ "authentication"
    # The service must still exist — the mutation was rejected
    assert ApiGateway.ServiceStore.get("geo") != nil
  end

  test "queries do not require auth" do
    ApiGateway.ServiceStore.register(%{"name" => "cache", "url" => "http://cache:4003"})

    assert {:ok, %{data: %{"services" => services}}} =
             Absinthe.run("query { services { name } }", Schema, context: %{})

    assert length(services) == 1
  end
end
```

### Step 10: Run the tests

```bash
mix test test/api_gateway/graphql_auth_test.exs --trace
```

---


### Why this works

The design leans on BEAM guarantees (process isolation, mailbox ordering, supervisor restarts) and pushes invariants to the boundaries of each module. State transitions are explicit, failure modes are declared rather than implicit, and each step is independently testable. That combination keeps the implementation correct under concurrent load and cheap to change later.

## Trade-off analysis

| Aspect | DataLoader batching | `Task.async_stream` in resolver | No batching |
|--------|--------------------|---------------------------------|-------------|
| N+1 queries | Eliminated | Eliminated for lists only | Full N+1 |
| Scope | Entire query tree | One resolver | — |
| Code location | Field resolver | List resolver | Field resolver |
| Testability | Source tested in isolation | Resolver test | Field test |

Reflection question: `plugins/0` in the schema must include `Absinthe.Middleware.Dataloader`. What happens if you add DataLoader to the loader but forget the plugin?

---

## Common production mistakes

**1. Missing `plugins/0` declaration**
DataLoader fields resolve to nil silently. No error is raised.

**2. `middleware/3` arity confusion**
Always manipulate the incoming `middleware` list, don't replace it with a bare new list — that removes the built-in resolver middleware.

**3. Context not propagated from Plug to Absinthe**
Without configuring `Absinthe.Plug` with a `context:` option, `conn.assigns[:client_id]` never reaches the Absinthe context and the Authenticate middleware always denies requests.

**4. Authenticate middleware on queries**
Read-only queries often should be accessible without auth. The `middleware/3` callback targets mutations specifically so queries are not blocked.

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

- [Absinthe Middleware](https://hexdocs.pm/absinthe/Absinthe.Middleware.html) — `call/2` contract and `put_result/2`
- [Dataloader](https://hexdocs.pm/dataloader) — KV and Ecto sources, batching semantics
- [Absinthe.Middleware.Dataloader plugin](https://hexdocs.pm/absinthe/Absinthe.Middleware.Dataloader.html) — why `plugins/0` matters
- [Absinthe Resolution Helpers](https://hexdocs.pm/absinthe/Absinthe.Resolution.Helpers.html) — `on_load/2` for deferred resolution
