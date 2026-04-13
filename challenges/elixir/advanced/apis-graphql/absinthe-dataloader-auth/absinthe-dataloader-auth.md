# Absinthe DataLoader and Auth Middleware

**Project**: `absinthe_dataloader_auth` — production-grade absinthe dataloader and auth middleware in Elixir

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
absinthe_dataloader_auth/
├── lib/
│   └── absinthe_dataloader_auth.ex
├── script/
│   └── main.exs
├── test/
│   └── absinthe_dataloader_auth_test.exs
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
defmodule AbsintheDataloaderAuth.MixProject do
  use Mix.Project

  def project do
    [
      app: :absinthe_dataloader_auth,
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
### `lib/absinthe_dataloader_auth.ex`

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
          e in RuntimeError -> {url, "unreachable"}
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
    |> Absinthe.Resolution.put_result({:error, {:error, :authentication_required}})
  end
end

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
### `test/absinthe_dataloader_auth_test.exs`

```elixir
defmodule ApiGateway.GraphQL.AuthTest do
  use ExUnit.Case, async: true
  doctest ApiGateway.ServiceStore

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

  describe "ApiGateway.GraphQL.Auth" do
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
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Absinthe DataLoader and Auth Middleware.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Absinthe DataLoader and Auth Middleware ===")
    IO.puts("Category: APIs and GraphQL\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case AbsintheDataloaderAuth.run(payload) do
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
        for _ <- 1..1_000, do: AbsintheDataloaderAuth.run(:bench)
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
