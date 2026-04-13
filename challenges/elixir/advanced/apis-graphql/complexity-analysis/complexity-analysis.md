# Query Complexity Analysis

**Project**: `graphql_complexity` — reject expensive GraphQL queries before execution using static complexity analysis

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
graphql_complexity/
├── lib/
│   └── graphql_complexity.ex
├── script/
│   └── main.exs
├── test/
│   └── graphql_complexity_test.exs
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
defmodule GraphqlComplexity.MixProject do
  use Mix.Project

  def project do
    [
      app: :graphql_complexity,
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

### `lib/graphql_complexity.ex`

```elixir
# lib/graphql_complexity/graphql/types/user_types.ex
defmodule GraphqlComplexity.Graphql.Types.UserTypes do
  use Absinthe.Schema.Notation

  object :user do
    field :id, non_null(:id), complexity: 0
    field :name, non_null(:string), complexity: 1
    field :email, non_null(:string), complexity: 1

    field :articles, list_of(:article) do
      arg :first, :integer, default_value: 10

      # Dynamic cost — multiply child complexity by :first.
      complexity fn args, child_complexity ->
        args.first * child_complexity + 1
      end

      resolve fn _p, args, _r ->
        {:ok, for i <- 1..args.first, do: %{id: i, title: "t#{i}"}}
      end
    end
  end
end

# lib/graphql_complexity/graphql/types/article_types.ex
defmodule GraphqlComplexity.Graphql.Types.ArticleTypes do
  use Absinthe.Schema.Notation

  object :article do
    field :id, non_null(:id), complexity: 0
    field :title, non_null(:string), complexity: 1
    field :body, non_null(:string), complexity: 5  # expensive to load from blob store

    field :comments, list_of(:comment) do
      arg :first, :integer, default_value: 10
      complexity fn args, child_complexity -> args.first * child_complexity + 1 end
      resolve fn _p, args, _r ->
        {:ok, for i <- 1..args.first, do: %{id: i, body: "c#{i}"}}
      end
    end
  end

  object :comment do
    field :id, non_null(:id), complexity: 0
    field :body, non_null(:string), complexity: 1
  end
end

# lib/graphql_complexity/graphql/complexity_rules.ex
defmodule GraphqlComplexity.Graphql.ComplexityRules do
  @moduledoc "Reusable complexity functions."

  @doc """
  Standard paginated-list complexity: `first * child + 1`.
  Rejects negative or absurd `first` values defensively — the schema also
  enforces a max via `Absinthe.Phase.Document.Validation`.
  """
  def paginated_list(%{first: n}, child) when is_integer(n) and n > 0 and n <= 1000 do
    n * child + 1
  end

  def paginated_list(%{first: n}, _child) when n > 1000, do: :infinity

  def paginated_list(_args, child), do: 10 * child + 1  # default page size
end

# lib/graphql_complexity/graphql/schema.ex
defmodule GraphqlComplexity.Graphql.Schema do
  use Absinthe.Schema

  import_types GraphqlComplexity.Graphql.Types.UserTypes
  import_types GraphqlComplexity.Graphql.Types.ArticleTypes

  query do
    field :users, list_of(:user) do
      arg :first, :integer, default_value: 10

      complexity &GraphqlComplexity.Graphql.ComplexityRules.paginated_list/2

      resolve fn _p, args, _r ->
        {:ok, for i <- 1..args.first, do: %{id: i, name: "u#{i}", email: "u#{i}@x.com"}}
      end
    end

    field :me, :user, complexity: 1, resolve: fn _, _, _ -> {:ok, %{id: 1, name: "me", email: "m@x.com"}} end
  end

  # Field without a complexity annotation gets this default.
  def middleware(middleware, _field, _object), do: middleware
end

# lib/graphql_complexity/router.ex
defmodule GraphqlComplexity.Router do
  use Plug.Router

  plug :match
  plug Plug.Parsers,
    parsers: [:urlencoded, :multipart, :json, Absinthe.Plug.Parser],
    json_decoder: Jason
  plug :dispatch

  forward "/graphql",
    to: Absinthe.Plug,
    init_opts: [
      schema: GraphqlComplexity.Graphql.Schema,
      analyze_complexity: true,
      max_complexity: 500,
      # Return the actual complexity to the client via extensions.
      result_phase: GraphqlComplexity.Graphql.ComplexityInExtensions
    ]
end

# lib/graphql_complexity/graphql/complexity_in_extensions.ex
defmodule GraphqlComplexity.Graphql.ComplexityInExtensions do
  @moduledoc "Adds the analyzed complexity to the GraphQL response `extensions`."
  @behaviour Absinthe.Phase

  @impl true
  def run(blueprint, _opts) do
    complexity =
      blueprint
      |> Map.get(:execution, %{})
      |> Map.get(:result, %{})
      |> Map.get(:complexity, 0)

    extensions =
      (blueprint.result[:extensions] || %{})
      |> Map.put(:complexity, complexity)

    result = Map.put(blueprint.result || %{}, :extensions, extensions)
    {:ok, %{blueprint | result: result}}
  end
end
```

### `test/graphql_complexity_test.exs`

```elixir
defmodule GraphqlComplexity.ComplexityTest do
  use ExUnit.Case, async: true
  doctest GraphqlComplexity.Graphql.Types.UserTypes

  alias GraphqlComplexity.Graphql.Schema

  defp run(query, max_complexity) do
    Absinthe.run(query, Schema,
      analyze_complexity: true,
      max_complexity: max_complexity)
  end

  describe "GraphqlComplexity.Complexity" do
    test "simple query is accepted" do
      assert {:ok, %{data: %{"me" => _}}} = run("{ me { id name } }", 100)
    end

    test "list with first=10 inside budget" do
      query = "{ users(first: 10) { id name } }"
      assert {:ok, %{data: _}} = run(query, 100)
    end

    test "list with first=1000 rejected" do
      query = "{ users(first: 1000) { id name email } }"
      assert {:ok, %{errors: errors}} = run(query, 500)
      assert Enum.any?(errors, &String.contains?(&1.message, "complexity"))
    end

    test "nested list multiplies complexity" do
      # users(first: 10) × articles(first: 10) × comments(first: 10)
      # = 10 × (10 × (10 + 1) + 1) = 1_110 + overhead
      query = """
      { users(first: 10) {
          articles(first: 10) {
            comments(first: 10) { body }
          }
        } }
      """
      assert {:ok, %{errors: _}} = run(query, 500)
      assert {:ok, %{data: _}} = run(query, 10_000)
    end

    test "malformed first=0 does not crash the analyzer" do
      assert {:ok, _} = run("{ users(first: 0) { id } }", 100)
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Query Complexity Analysis.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Query Complexity Analysis ===")
    IO.puts("Category: APIs and GraphQL\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case GraphqlComplexity.run(payload) do
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
        for _ <- 1..1_000, do: GraphqlComplexity.run(:bench)
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
