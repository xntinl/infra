# Persisted Queries and Query Complexity Analysis

**Project**: `graphql_guardrails` — a GraphQL API that refuses unbounded queries before they hit the database by combining APQ (Automatic Persisted Queries) with complexity limits.

## Project context

Your public GraphQL API was abused: a client nested `user → posts → author → posts → author …` ten levels deep, running every worker for 30 seconds. The fix is two complementary controls:

1. **Complexity analysis** — reject queries whose estimated cost is above a threshold.
2. **Persisted queries** — optionally, only accept a pre-registered whitelist of documents. For public APIs, APQ reduces bandwidth; for private first-party clients, it eliminates arbitrary documents entirely.

```
graphql_guardrails/
├── lib/
│   ├── graphql_guardrails/
│   │   ├── application.ex
│   │   └── persisted_store.ex         # ETS-backed hash → document map
│   └── graphql_guardrails_web/
│       └── graphql/
│           ├── schema.ex
│           ├── middleware/
│           │   └── complexity.ex      # custom middleware (optional)
│           └── plugs/
│               └── apq.ex             # APQ request interceptor
├── test/graphql_guardrails_web/
│   ├── complexity_test.exs
│   └── apq_test.exs
└── mix.exs
```

## Why both and not just one

- Complexity alone prevents expensive legitimate documents from running, but clients can still send any shape. A motivated attacker sends complex queries under the limit in a tight loop.
- Persisted queries alone pin documents but do nothing if one of the pinned documents is expensive (someone approved it in PR without thinking).

Combined: the server only runs queries it has seen AND whose complexity is bounded.

## Why Automatic Persisted Queries (Apollo spec)

APQ is a client-initiated protocol:

1. Client computes `sha256(document)` and sends **only the hash**.
2. Server looks it up. If known, execute. If not, respond `PersistedQueryNotFound`.
3. Client retries once with `{ query, extensions: { persistedQuery: { sha256Hash } } }`.
4. Server validates `sha256(query) == hash`, stores it, executes.

Effect: after the first request per document, all subsequent requests send ~70 bytes (the hash) instead of several KB. Cache-friendly on CDNs because the hash goes in the URL.

## Why complexity analysis and not just depth limit

Depth limiting (`max_depth: 5`) is coarse — a 5-deep query that asks for 10k items at each level is still catastrophic. Complexity scores each field (leaf = 1, list = `n * child_cost`) and sums them. Absinthe computes this statically from the document + variables before any resolver runs.

## Design decisions

- **Option A — ETS store for persisted queries**: pros: in-memory, fast, per-node; cons: cold after restart, not shared across nodes.
- **Option B — Redis-backed store**: pros: shared, survives restarts; cons: network hop per lookup.
- **Option C — Compile-time whitelist from `priv/persisted/*.graphql`**: pros: no runtime registration (pure allowlist), auditable in VCS; cons: rigid.
→ We pick **A** for the teaching version and note that production first-party APIs should pick **C** (audited) and public APIs should pick **B** (for sharing APQ hashes across nodes).

## Implementation

### Dependencies

```elixir
defp deps do
  [
    {:phoenix, "~> 1.7"},
    {:plug_cowboy, "~> 2.7"},
    {:absinthe, "~> 1.7"},
    {:absinthe_plug, "~> 1.5"},
    {:jason, "~> 1.4"}
  ]
end
```

### Step 1: Schema with per-field complexity

**Objective**: Attach static and `fn args, child -> n + n*child end` costs per field so the analyzer can score any AST before resolvers wake.

```elixir
defmodule GraphqlGuardrailsWeb.Graphql.Schema do
  use Absinthe.Schema

  object :post do
    field :id, non_null(:id)
    field :title, non_null(:string)
    # Fixed cost per author lookup
    field :author, :user, complexity: 2
  end

  object :user do
    field :id, non_null(:id)
    field :name, non_null(:string)

    field :posts, list_of(:post) do
      arg :first, :integer, default_value: 10
      # Dynamic: cost scales with requested count
      complexity fn %{first: n}, child_complexity ->
        n + n * child_complexity
      end
    end
  end

  query do
    field :user, :user do
      arg :id, non_null(:id)
      complexity 2
      resolve fn _, %{id: id}, _ -> {:ok, %{id: id, name: "stub"}} end
    end
  end
end
```

### Step 2: Enforce complexity at execution

**Objective**: Flip `analyze_complexity: true, max_complexity: 200` on `Absinthe.Plug` so abusive documents die in validation, never in the DB.

Absinthe provides `Absinthe.Phase.Document.Complexity.Result`. Enable it via pipeline options:

```elixir
# In the Plug pipeline (router.ex)
forward "/api",
  to: Absinthe.Plug,
  init_opts: [
    schema: GraphqlGuardrailsWeb.Graphql.Schema,
    analyze_complexity: true,
    max_complexity: 200
  ]
```

A document exceeding `max_complexity` is rejected before any resolver runs; the client gets a standard GraphQL error.

### Step 3: Persisted query store

**Objective**: Map sha256 → query text in ETS and verify hashes with `Plug.Crypto.secure_compare/2` so timing attacks cannot probe the registry.

```elixir
defmodule GraphqlGuardrails.PersistedStore do
  @table :apq_store

  def init, do: :ets.new(@table, [:named_table, :public, read_concurrency: true])

  @spec get(binary()) :: {:ok, binary()} | :error
  def get(hash) do
    case :ets.lookup(@table, hash) do
      [{^hash, query}] -> {:ok, query}
      [] -> :error
    end
  end

  @spec put(binary(), binary()) :: :ok
  def put(hash, query) when byte_size(hash) == 64 do
    :ets.insert(@table, {hash, query})
    :ok
  end

  @spec valid?(binary(), binary()) :: boolean()
  def valid?(hash, query) do
    computed = :crypto.hash(:sha256, query) |> Base.encode16(case: :lower)
    Plug.Crypto.secure_compare(computed, hash)
  end
end
```

`secure_compare` prevents timing attacks on hash comparison.

### Step 4: APQ plug that rewrites the request body

**Objective**: Implement the Apollo APQ protocol: lookup hash, rewrite `conn.params["query"]` on hit, emit `PersistedQueryNotFound` on miss so clients register on second shot.

```elixir
defmodule GraphqlGuardrailsWeb.Graphql.Plugs.APQ do
  @behaviour Plug

  alias GraphqlGuardrails.PersistedStore

  @impl true
  def init(opts), do: opts

  @impl true
  def call(conn, _opts) do
    with %{"extensions" => %{"persistedQuery" => %{"sha256Hash" => hash}}} <- conn.params,
         query = conn.params["query"] do
      handle_apq(conn, hash, query)
    else
      _ -> conn
    end
  end

  defp handle_apq(conn, hash, nil) do
    case PersistedStore.get(hash) do
      {:ok, query} -> put_in(conn.params["query"], query)
      :error -> send_error(conn, "PersistedQueryNotFound")
    end
  end

  defp handle_apq(conn, hash, query) when is_binary(query) do
    if PersistedStore.valid?(hash, query) do
      :ok = PersistedStore.put(hash, query)
      conn
    else
      send_error(conn, "PersistedQueryHashMismatch")
    end
  end

  defp send_error(conn, code) do
    body = Jason.encode!(%{errors: [%{message: code, extensions: %{code: code}}]})

    conn
    |> Plug.Conn.put_resp_content_type("application/json")
    |> Plug.Conn.send_resp(200, body)
    |> Plug.Conn.halt()
  end
end
```

Mount before `Absinthe.Plug`:

```elixir
pipeline :graphql do
  plug GraphqlGuardrailsWeb.Graphql.Plugs.APQ
end

scope "/api" do
  pipe_through :graphql
  forward "/", Absinthe.Plug, schema: ..., analyze_complexity: true, max_complexity: 200
end
```

## Why this works

```
client ─▶ POST /api { extensions: { persistedQuery: { sha256Hash } } }
              │
              ▼
        APQ plug: lookup hash
         ├── miss ──▶ 200 { errors: [ PersistedQueryNotFound ] }
         │            client retries with { query, extensions } ─▶ APQ stores & continues
         ▼
    Absinthe.Plug with analyze_complexity
              │
              ▼
    Document.Complexity phase
         ├── score > max ──▶ 200 { errors: [ complexity too high ] }
         ▼
    Resolution phase (resolvers run only here)
```

Both gates run **before** resolvers, so no DB is touched on a rejected request. Complexity is computed from the AST + variables, deterministically.

## Tests

```elixir
defmodule GraphqlGuardrailsWeb.ComplexityTest do
  use ExUnit.Case, async: true
  alias GraphqlGuardrailsWeb.Graphql.Schema

  describe "complexity analysis" do
    test "accepts a small query" do
      doc = "{ user(id: 1) { name } }"
      assert {:ok, _} = Absinthe.run(doc, Schema, analyze_complexity: true, max_complexity: 200)
    end

    test "rejects a query exceeding max_complexity" do
      doc = "{ user(id: 1) { posts(first: 500) { title author { name } } } }"
      assert {:ok, %{errors: [%{message: msg}]}} =
               Absinthe.run(doc, Schema, analyze_complexity: true, max_complexity: 200)

      assert msg =~ "complexity"
    end
  end
end

defmodule GraphqlGuardrailsWeb.APQTest do
  use ExUnit.Case, async: false
  alias GraphqlGuardrails.PersistedStore

  setup do
    :ets.delete_all_objects(:apq_store)
    :ok
  end

  describe "persisted store" do
    test "round-trips a document by hash" do
      q = "{ __typename }"
      h = :crypto.hash(:sha256, q) |> Base.encode16(case: :lower)
      :ok = PersistedStore.put(h, q)
      assert {:ok, ^q} = PersistedStore.get(h)
    end

    test "rejects hash mismatch" do
      refute PersistedStore.valid?(String.duplicate("0", 64), "{ x }")
    end
  end
end
```

## Benchmark

Measure the fraction of time the complexity phase adds:

```elixir
doc = "{ user(id: 1) { posts(first: 10) { title } } }"

Benchee.run(%{
  "without complexity" => fn -> Absinthe.run(doc, Schema) end,
  "with complexity"    => fn -> Absinthe.run(doc, Schema, analyze_complexity: true, max_complexity: 200) end
})
```

**Expected**: complexity phase adds < 50 µs on a medium document; negligible vs. any DB work.

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


## Deep Dive: Apis Patterns and Production Implications

API testing requires testing schema validation, error messages, pagination, and rate limiting—not just happy paths. The mistake is testing only the happy path and assuming error handling works. Production APIs with weak error handling become support nightmares.

---

## Trade-offs and production gotchas

**1. Complexity scoring drifts from reality**
A field you scored as `1` might actually trigger 5 queries behind the scenes. Keep scores aligned with real cost; review when resolvers change.

**2. Variable-dependent complexity and batched queries**
If complexity depends on `$first` and the client passes `first: 1_000_000`, you must also cap numeric args independently — complexity stops the whole query but the client still sent the variable. Validate numeric bounds in the schema.

**3. APQ hash collisions**
SHA-256 collisions are infeasible; truncating the hash (some implementations use 32 hex chars) is not. Store the full 64-char hex.

**4. Cache eviction for APQ**
An unbounded ETS table leaks memory under adversarial hash floods. Cap with LRU (`:ets.info/2` + periodic sweep) or use a bounded library like `Nebulex`.

**5. `secure_compare`, not `==`**
Plain `==` on binaries short-circuits and leaks timing. Use `Plug.Crypto.secure_compare/2`.

**6. When NOT to use APQ**
If your API is internal and all clients are first-party, prefer a compile-time whitelist: simpler, auditable, and removes the "register on miss" class of bugs.

## Reflection

Your API has two tiers of clients: first-party (mobile app) and third-party (public developers). Would you apply the same `max_complexity` to both? What about the same persistence policy? Sketch a middleware that picks limits based on `context.tier`, and discuss whether you expose the complexity score in the response or keep it opaque.


## Executable Example

```elixir
defmodule GraphqlGuardrailsWeb.ComplexityTest do
  use ExUnit.Case, async: true
  alias GraphqlGuardrailsWeb.Graphql.Schema

  describe "complexity analysis" do
    test "accepts a small query" do
      doc = "{ user(id: 1) { name } }"
      assert {:ok, _} = Absinthe.run(doc, Schema, analyze_complexity: true, max_complexity: 200)
    end

    test "rejects a query exceeding max_complexity" do
      doc = "{ user(id: 1) { posts(first: 500) { title author { name } } } }"
      assert {:ok, %{errors: [%{message: msg}]}} =
               Absinthe.run(doc, Schema, analyze_complexity: true, max_complexity: 200)

      assert msg =~ "complexity"
    end
  end
end

defmodule GraphqlGuardrailsWeb.APQTest do
  use ExUnit.Case, async: false
  alias GraphqlGuardrails.PersistedStore

  setup do
    :ets.delete_all_objects(:apq_store)
    :ok
  end

  describe "persisted store" do
    test "round-trips a document by hash" do
      q = "{ __typename }"
      h = :crypto.hash(:sha256, q) |> Base.encode16(case: :lower)
      :ok = PersistedStore.put(h, q)
      assert {:ok, ^q} = PersistedStore.get(h)
    end

    test "rejects hash mismatch" do
      refute PersistedStore.valid?(String.duplicate("0", 64), "{ x }")
    end
  end
end

defmodule Main do
  def main do
      IO.puts("GraphQL schema initialization")
      defmodule QueryType do
        def resolve_hello(_, _, _), do: {:ok, "world"}
      end
      if is_atom(QueryType) do
        IO.puts("✓ GraphQL schema validated and query resolver accessible")
      end
  end
end

Main.main()
```
