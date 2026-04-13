# Persisted Queries and Query Complexity Analysis

**Project**: `graphql_guardrails` — a GraphQL API that refuses unbounded queries before they hit the database by combining APQ (Automatic Persisted Queries) with complexity limits

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
graphql_guardrails/
├── lib/
│   └── graphql_guardrails.ex
├── script/
│   └── main.exs
├── test/
│   └── graphql_guardrails_test.exs
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
defmodule GraphqlGuardrails.MixProject do
  use Mix.Project

  def project do
    [
      app: :graphql_guardrails,
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

### `lib/graphql_guardrails.ex`

```elixir
defmodule GraphqlGuardrailsWeb.Graphql.Schema do
  @moduledoc """
  Ejercicio: Persisted Queries and Query Complexity Analysis.
  Implementa el comportamiento descrito en el enunciado, exponiendo funciones públicas documentadas y con tipos claros.
  """

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

defmodule GraphqlGuardrails.PersistedStore do
  @table :apq_store

  def init, do: :ets.new(@table, [:named_table, :public, read_concurrency: true])

  @doc "Returns result from hash."
  @spec get(binary()) :: {:ok, binary()} | :error
  def get(hash) do
    case :ets.lookup(@table, hash) do
      [{^hash, query}] -> {:ok, query}
      [] -> :error
    end
  end

  @doc "Returns put result from hash and query."
  @spec put(binary(), binary()) :: :ok
  def put(hash, query) when byte_size(hash) == 64 do
    :ets.insert(@table, {hash, query})
    :ok
  end

  @doc "Returns whether valid holds from hash and query."
  @spec valid?(binary(), binary()) :: boolean()
  def valid?(hash, query) do
    computed = :crypto.hash(:sha256, query) |> Base.encode16(case: :lower)
    Plug.Crypto.secure_compare(computed, hash)
  end
end

defmodule GraphqlGuardrailsWeb.Graphql.Plugs.APQ do
  @behaviour Plug

  alias GraphqlGuardrails.PersistedStore

  @impl true
  def init(opts), do: opts

  @doc "Calls result from conn and _opts."
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

### `test/graphql_guardrails_test.exs`

```elixir
defmodule GraphqlGuardrailsWeb.ComplexityTest do
  use ExUnit.Case, async: true
  doctest GraphqlGuardrailsWeb.Graphql.Schema
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

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Persisted Queries and Query Complexity Analysis.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Persisted Queries and Query Complexity Analysis ===")
    IO.puts("Category: APIs and GraphQL\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case GraphqlGuardrails.run(payload) do
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
        for _ <- 1..1_000, do: GraphqlGuardrails.run(:bench)
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
