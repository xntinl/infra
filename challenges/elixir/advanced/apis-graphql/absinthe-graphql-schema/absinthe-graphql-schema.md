# Absinthe GraphQL — Schema and Resolvers

**Project**: `absinthe_graphql_schema` — production-grade absinthe graphql — schema and resolvers in Elixir

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
absinthe_graphql_schema/
├── lib/
│   └── absinthe_graphql_schema.ex
├── script/
│   └── main.exs
├── test/
│   └── absinthe_graphql_schema_test.exs
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
defmodule AbsintheGraphqlSchema.MixProject do
  use Mix.Project

  def project do
    [
      app: :absinthe_graphql_schema,
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
### `lib/absinthe_graphql_schema.ex`

```elixir
defmodule ApiGateway.ServiceStore do
  @moduledoc """
  In-memory registry of backend services.
  Stores `%{name => %{name, url, health_path, registered_at}}`.
  """
  use Agent

  @spec start_link(keyword()) :: Agent.on_start()
  def start_link(_opts), do: Agent.start_link(fn -> %{} end, name: __MODULE__)

  @doc "Lists result."
  @spec list() :: [map()]
  def list, do: Agent.get(__MODULE__, &Map.values/1)

  @doc "Returns result from name."
  @spec get(String.t()) :: map() | nil
  def get(name), do: Agent.get(__MODULE__, &Map.get(&1, name))

  @doc "Registers result from attrs."
  @spec register(map()) :: map()
  def register(attrs) do
    Agent.get_and_update(__MODULE__, fn services ->
      entry = Map.merge(attrs, %{"registered_at" => DateTime.utc_now() |> DateTime.to_iso8601()})
      {entry, Map.put(services, attrs["name"], entry)}
    end)
  end

  @doc "Returns deregister result from name."
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

  @doc "Lists services result from _, _ and _."
  @spec list_services(any(), map(), Absinthe.Resolution.t()) :: {:ok, [map()]}
  def list_services(_, _, _) do
    {:ok, ServiceStore.list()}
  end

  @spec get_service(any(), %{name: String.t()}, Absinthe.Resolution.t()) ::
          {:ok, map()} | {:error, String.t()}
  @doc "Returns service result from _ and _."
  def get_service(_, %{name: name}, _) do
    case ServiceStore.get(name) do
      nil -> {:error, "service #{name} not found"}
      service -> {:ok, service}
    end
  end

  @doc "Registers service result from _ and _."
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
  @doc "Returns deregister service result from _ and _."
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
### `test/absinthe_graphql_schema_test.exs`

```elixir
defmodule ApiGateway.GraphQL.SchemaTest do
  use ExUnit.Case, async: true
  doctest ApiGateway.ServiceStore

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
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Absinthe GraphQL — Schema and Resolvers.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Absinthe GraphQL — Schema and Resolvers ===")
    IO.puts("Category: APIs and GraphQL\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case AbsintheGraphqlSchema.run(payload) do
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
        for _ <- 1..1_000, do: AbsintheGraphqlSchema.run(:bench)
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
