# Absinthe DataLoader and Auth Middleware

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`. The GraphQL schema is working (previous exercise).
Under load the dashboard team reports slow queries: listing 50 services with their
health status takes 50 individual HTTP checks instead of 1 batched call. You also
need to gate mutations behind the gateway's existing auth middleware.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── graphql/
│       │   ├── schema.ex           # already exists — add plugins/0 and middleware/3
│       │   ├── middleware/
│       │   │   ├── authenticate.ex # ← you implement this
│       │   │   └── handle_errors.ex # ← and this
│       │   └── loader.ex           # ← and this
│       └── ...
├── test/
│   └── api_gateway/
│       └── graphql_auth_test.exs   # given tests — must pass without modification
└── mix.exs
```

---

## The business problem

Two problems compound each other:

**N+1 health checks**: the `status` field on each service calls an HTTP health endpoint.
When the dashboard lists 50 services, that is 50 sequential HTTP calls. Each takes ~50ms.
The query takes 2.5 seconds. DataLoader batches them: collect all health check URLs during
the resolution of one GraphQL level, fire them concurrently, resolve all 50 fields from
the single batched result.

**Unauthenticated mutations**: `registerService` and `deregisterService` have no auth.
Any caller can deregister production services. The auth middleware reads `current_client`
from the Absinthe context (set by the Plug pipeline's `Auth` middleware) and aborts
before the resolver runs if it is missing.

---

## Why DataLoader and not `Task.async_stream`

You could batch health checks with `Task.async_stream` in the list resolver. But this
couples batching logic to the list resolver — the detail resolver does not benefit, and
any future field that needs health status re-implements the same pattern.

DataLoader is a protocol-level solution: it operates at the GraphQL execution level,
collecting all pending loads across the entire query tree (not just one resolver), then
dispatching them before moving to the next resolution level. It works for any source:
HTTP calls, database queries, external APIs.

---

## Why middleware instead of resolver-level auth checks

Without middleware:

```elixir
def register_service(_, %{input: input}, %{context: %{current_client: client}})
    when not is_nil(client) do
  # actual logic
end

def register_service(_, _, _), do: {:error, "authentication required"}
```

Every mutation needs this guard. Add 10 mutations and you have 10 copies of the same
pattern — and 10 places where a developer can forget the `when not is_nil(client)` guard.

With `middleware MyApp.Middleware.Authenticate` in the schema's `middleware/3` callback,
auth is applied to every mutation automatically at schema definition time. Forget it in
one place, the schema enforces it everywhere.

---

## Implementation

### Step 1: `mix.exs` — add dataloader

```elixir
{:dataloader, "~> 2.0"}
```

### Step 2: `lib/api_gateway/graphql/loader.ex`

```elixir
defmodule ApiGateway.GraphQL.Loader do
  @moduledoc """
  DataLoader sources for api_gateway.

  Uses KV source (not Ecto) because the gateway's data lives in ETS and
  external HTTP endpoints, not a relational database.
  """

  def health_source do
    # KV source: given a set of keys, call fetch_health/2 once with all keys
    Dataloader.KV.new(&fetch_health/2)
  end

  # fetch_health/2 receives {:health, urls_set} where urls_set is a MapSet
  # of service URLs that need checking. Returns %{url => status_string}.
  defp fetch_health(:health, urls) do
    # TODO: for each URL in urls, make a concurrent HTTP GET to the health_path
    # TODO: return %{url => "healthy"} or %{url => "unreachable"} for each
    # HINT: Task.async_stream/3 with max_concurrency: 20 and timeout: 3_000
    # HINT: on {:ok, %{status: s}} when s in 200..299 → "healthy"
    # HINT: on {:ok, _} or {:exit, _} → "unreachable"
    # For now, a stub that returns "healthy" for all:
    Map.new(urls, fn url -> {url, "healthy"} end)
  end
end
```

### Step 3: `lib/api_gateway/graphql/middleware/authenticate.ex`

```elixir
defmodule ApiGateway.GraphQL.Middleware.Authenticate do
  @moduledoc """
  Absinthe middleware that aborts resolution if no authenticated client
  is present in the context.

  The Plug pipeline's Auth middleware sets conn.assigns[:client_id].
  The schema's build_context/1 copies it into the Absinthe context as :current_client.
  This middleware reads it.
  """
  @behaviour Absinthe.Middleware

  @impl true
  def call(%{context: %{current_client: client}} = resolution, _opts)
      when not is_nil(client) do
    # Client is authenticated — pass through
    resolution
  end

  def call(resolution, _opts) do
    # TODO: use Absinthe.Resolution.put_result/2 to set {:error, "authentication required"}
    # This stops resolution of this field without crashing the query
  end
end
```

### Step 4: `lib/api_gateway/graphql/middleware/handle_errors.ex`

```elixir
defmodule ApiGateway.GraphQL.Middleware.HandleErrors do
  @moduledoc """
  Post-resolver middleware that normalizes error formats.

  Runs after the resolver. Converts raw strings and Exception structs
  to the map format Absinthe uses for the `errors` array in the response.
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

### Step 5: Update `lib/api_gateway/graphql/schema.ex`

```elixir
defmodule ApiGateway.GraphQL.Schema do
  use Absinthe.Schema

  import Absinthe.Resolution.Helpers, only: [dataloader: 1]

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

  # Called once per request to build the Absinthe context.
  # Copies the client_id from Plug assigns into the GraphQL context.
  def context(ctx) do
    loader =
      Dataloader.new()
      |> Dataloader.add_source(:health, ApiGateway.GraphQL.Loader.health_source())

    ctx
    |> Map.put(:loader, loader)
    # current_client is set here by the Plug pipeline via ApiGateway.Router
    # forward "/graphql", Absinthe.Plug, schema: ..., context: &build_context/1
  end

  # Required to activate DataLoader batching
  def plugins do
    [Absinthe.Middleware.Dataloader | Absinthe.Plugin.defaults()]
  end

  # Called for every field in the schema.
  # Applies Authenticate to all mutations, HandleErrors to everything.
  def middleware(middleware, _field, %Absinthe.Type.Object{identifier: :mutation}) do
    [ApiGateway.GraphQL.Middleware.Authenticate | middleware] ++
      [ApiGateway.GraphQL.Middleware.HandleErrors]
  end

  def middleware(middleware, _field, _object) do
    middleware ++ [ApiGateway.GraphQL.Middleware.HandleErrors]
  end
end
```

### Step 6: Use DataLoader in the `status` field

```elixir
# In ApiGateway.GraphQL.Types.Service — update the :service object
object :service do
  field :name,          :string
  field :url,           :string
  field :health_path,   :string
  field :registered_at, :datetime

  # DataLoader batches all status checks for a list of services into one call
  field :status, :string do
    resolve fn service, _, %{context: %{loader: loader}} ->
      loader
      |> Dataloader.load(:health, :health, service["url"])
      |> Absinthe.Resolution.Helpers.on_load(fn loader ->
        status = Dataloader.get(loader, :health, :health, service["url"])
        {:ok, status}
      end)
    end
  end
end
```

### Step 7: Given tests — must pass without modification

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

### Step 8: Run the tests

```bash
mix test test/api_gateway/graphql_auth_test.exs --trace
```

---

## Trade-off analysis

| Aspect | DataLoader batching | `Task.async_stream` in resolver | No batching |
|--------|--------------------|---------------------------------|-------------|
| N+1 queries | Eliminated | Eliminated for lists only | Full N+1 |
| Scope | Entire query tree | One resolver | — |
| Code location | Field resolver | List resolver | Field resolver |
| Testability | Source tested in isolation | Resolver test | Field test |
| Overhead | Deferred resolution | None | None |

Reflection question: `plugins/0` in the schema must include `Absinthe.Middleware.Dataloader`.
What happens if you add DataLoader to the loader but forget the plugin? Do you get an error,
wrong results, or correct results? Run the test and find out.

---

## Common production mistakes

**1. Missing `plugins/0` declaration**
DataLoader uses a deferred execution model that requires Absinthe's plugin infrastructure.
Without `Absinthe.Middleware.Dataloader` in `plugins/0`, DataLoader fields resolve to nil
silently. No error is raised.

**2. `middleware/3` arity confusion**
Absinthe's `middleware/3` callback receives `(middleware_list, field, object)`. Returning
just a list replaces the entire middleware chain — including the built-in resolver middleware.
Always manipulate the incoming `middleware` list, don't replace it with a bare new list.

**3. Context not propagated from Plug to Absinthe**
`Absinthe.Plug` accepts a `context:` option that receives the conn and returns a map.
Without it, `conn.assigns[:client_id]` never reaches the Absinthe context and the
Authenticate middleware always denies requests.

**4. Authenticate middleware on queries**
Read-only queries often should be accessible without auth (public dashboards, health
checks). The `middleware/3` callback targets `%Absinthe.Type.Object{identifier: :mutation}`
specifically so queries are not blocked.

**5. DataLoader sources not added to the context**
If `context/1` does not add the DataLoader source, the `loader` key is nil in the context
and any field using `dataloader/1` raises a `KeyError` at runtime.

---

## Resources

- [Absinthe Middleware](https://hexdocs.pm/absinthe/Absinthe.Middleware.html) — `call/2` contract and `put_result/2`
- [Dataloader](https://hexdocs.pm/dataloader) — KV and Ecto sources, batching semantics
- [Absinthe.Middleware.Dataloader plugin](https://hexdocs.pm/absinthe/Absinthe.Middleware.Dataloader.html) — why `plugins/0` matters
- [Absinthe Resolution Helpers](https://hexdocs.pm/absinthe/Absinthe.Resolution.Helpers.html) — `on_load/2` for deferred resolution
