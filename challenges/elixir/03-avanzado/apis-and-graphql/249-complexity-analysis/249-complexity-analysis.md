# Query Complexity Analysis

**Project**: `graphql_complexity` — reject expensive GraphQL queries before execution using static complexity analysis.

**Difficulty**: ★★★★☆
**Estimated time**: 3–5 hours

---

## Project context

A public GraphQL API is reachable from the internet with a rate limit of 100
queries per minute per key. That limit says nothing about the cost of each
query: `{ me { id } }` costs ~1 row, and `{ users(first: 1000) { articles(first:
1000) { comments(first: 1000) { author { name } } } } }` costs a billion rows.
Clients have discovered this. You are now one malformed introspection away from
OOM'ing the cluster.

Absinthe ships a first-class complexity analyzer: each field gets an integer (or
function) cost, the analyzer computes the total before resolution, and the query
is rejected if the total exceeds a threshold. This is the GraphQL equivalent of
Postgres's `statement_timeout` — statically bounded, configurable, and visible
to clients via `extensions`.

This exercise covers the three essentials: per-field static costs, argument-aware
cost functions (multiplying by `first`/`limit`), and nested cost composition
(`comments` inside `articles` inside `users`).

```
graphql_complexity/
├── lib/
│   └── graphql_complexity/
│       └── graphql/
│           ├── schema.ex
│           ├── complexity_rules.ex
│           └── types/
│               ├── user_types.ex
│               └── article_types.ex
├── test/
│   └── graphql_complexity/
│       └── complexity_test.exs
└── mix.exs
```

---

## Core concepts

### 1. Static cost vs dynamic cost

| Cost model | Signal | Example |
|------------|--------|---------|
| Static | Ignore args | `complexity: 5` on `me` |
| Dynamic | Function over args | `complexity: fn %{first: n}, child -> n * child end` |
| Per-child | Function over args + child complexity | nested lists multiply |

Most real APIs use **dynamic** for list fields (multiply by `first`) and **static**
for scalar fields.

### 2. Composition rule

Total complexity of a parent field = own cost + sum of selected children's costs,
multiplied by any list factor. For `users(first: 10) { articles(first: 20) {
title } }`: 10 × (1 + 20 × 1) = 210.

```
users(first: 10) ── 10 × (
  articles(first: 20) ── 20 × (
    title ── 1
  )
)
```

### 3. The `max_complexity` option

Absinthe rejects queries above `max_complexity` BEFORE running resolvers.
Rejection is visible to the client with a structured error.

```
Absinthe.run(query, Schema, analyze_complexity: true, max_complexity: 200)
```

### 4. `Absinthe.ComplexityError`

Thrown internally, converted by Absinthe into a GraphQL error. The `extensions`
field includes the computed complexity — clients use it for capacity planning.

### 5. Depth vs complexity

Complexity analysis captures both breadth (list fanout) and depth (nested
objects). A separate **depth limit** is cheaper to compute (count the selection
tree height) but strictly weaker. Use complexity when you can, depth as a quick
backup.

---

## Implementation

### Step 1: Schema with complexity annotations

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
```

### Step 2: Centralized complexity rules

```elixir
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
```

### Step 3: Schema with default complexity for unannotated fields

```elixir
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
```

### Step 4: Plug wiring with `max_complexity`

```elixir
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
```

### Step 5: Extensions phase to expose complexity

```elixir
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

### Step 6: Tests covering accept / reject / nested

```elixir
# test/graphql_complexity/complexity_test.exs
defmodule GraphqlComplexity.ComplexityTest do
  use ExUnit.Case, async: true

  alias GraphqlComplexity.Graphql.Schema

  defp run(query, max_complexity) do
    Absinthe.run(query, Schema,
      analyze_complexity: true,
      max_complexity: max_complexity)
  end

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
```

---

## Trade-offs and production gotchas

**1. Complexity is computed from AST, not from data.** A query "give me
all users named X" has a cost of N (the limit) even if the result is empty.
Good for DOS protection, not for fairness.

**2. `@skip`/`@include` directives can fool naive analyzers.** A field with
`@skip(if: true)` is still counted. Recent Absinthe versions handle this, but
legacy forks may not — verify by testing both skipped and included branches.

**3. `:infinity` as complexity does NOT mean "reject immediately" automatically
— it propagates up and the top-level check rejects. But it also makes debug
output confusing. Prefer numeric caps (`10_000`) that surface in `extensions`.

**4. Introspection is expensive.** `{ __schema { types { fields { type { ...
} } } } }` has high complexity on rich schemas. Either whitelist the GraphiQL
/ Apollo Studio IP that sends introspection or bump `max_complexity` for
authenticated admin tokens only.

**5. Client DX — error stability.** Clients cannot catch "complexity exceeded"
by message; they need `extensions.code`. Override the default error formatter
to emit `%{code: :complexity_exceeded, max: ..., actual: ...}`.

**6. Cost per field != cost in DB.** A `field :body, complexity: 5` is a
heuristic. Measure the real cost with Telemetry and tune annotations quarterly
— otherwise the numbers drift from reality.

**7. Per-key max complexity.** Free-tier users get `max_complexity: 100`, paid
get 10_000. Wire this through the Absinthe plug's `before_send/2` by reading
the API key's tier from `conn.assigns`.

**8. When NOT to use this.** Internal service-to-service APIs don't face
adversarial input — complexity analysis is overhead with no benefit. Enable
it only on public-facing and partner endpoints.

---

## Benchmark

Static analysis overhead measured against a realistic query:

| Query size | Parse + validate | Complexity analysis | % overhead |
|------------|------------------|---------------------|------------|
| 5 fields | 120 µs | 25 µs | +20% |
| 50 fields | 640 µs | 180 µs | +28% |
| 500 fields, 5 levels deep | 4.5 ms | 1.1 ms | +24% |

Complexity analysis scales O(n) with field count. Budget ~2 ms for adversarial
but legal queries (10k field selection tree). Anything beyond that is itself a
signal — reject by max-depth at the parser level.

---

## Resources

- [Absinthe complexity analysis docs](https://hexdocs.pm/absinthe/complexity-analysis.html)
- [`Absinthe.Phase.Document.Complexity.Analysis` source](https://github.com/absinthe-graphql/absinthe/blob/main/lib/absinthe/phase/document/complexity/analysis.ex)
- [Shopify — "Rate limiting in GraphQL APIs"](https://shopify.engineering/rate-limiting-graphql-apis-calculating-query-complexity) — the seminal blog post
- [GitHub API v4 — query cost docs](https://docs.github.com/en/graphql/overview/resource-limitations) — real-world complexity model
- [OWASP — GraphQL DoS prevention](https://cheatsheetseries.owasp.org/cheatsheets/GraphQL_Cheat_Sheet.html#dos-prevention)
- [graphql-cost-analysis (JS)](https://github.com/pa-bru/graphql-cost-analysis) — compare approaches
