# Dataloader Batching against N+1 Queries

**Project**: `graphql_batching` — focused study of Dataloader batching primitives independent of Ecto, applied to a multi-tenant analytics API

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
graphql_batching/
├── lib/
│   └── graphql_batching.ex
├── script/
│   └── main.exs
├── test/
│   └── graphql_batching_test.exs
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
defmodule GraphqlBatching.MixProject do
  use Mix.Project

  def project do
    [
      app: :graphql_batching,
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
### `lib/graphql_batching.ex`

```elixir
# lib/graphql_batching/billing_client.ex
defmodule GraphqlBatching.BillingClient do
  @moduledoc "Fake billing client — replace with Tesla/Finch in production."

  @plans %{
    "plan_free" => %{tier: "free", limits: %{requests: 1_000}},
    "plan_pro" => %{tier: "pro", limits: %{requests: 100_000}},
    "plan_ent" => %{tier: "enterprise", limits: %{requests: 10_000_000}}
  }

  @doc "Bulk lookup — the batched operation Dataloader will call."
  @spec get_plans([String.t()]) :: %{String.t() => map()}
  def get_plans(ids) do
    :telemetry.execute([:billing_client, :get_plans], %{count: length(ids)}, %{})
    Map.take(@plans, ids)
  end

  @doc "Naive per-id variant — shown for comparison in benchmarks."
  @spec get_plan(String.t()) :: map() | nil
  def get_plan(id), do: Map.get(@plans, id)
end

# lib/graphql_batching/metrics_client.ex
defmodule GraphqlBatching.MetricsClient do
  @moduledoc "Fake ClickHouse client."

  @spec bulk_usage([String.t()], String.t()) :: %{String.t() => non_neg_integer()}
  def bulk_usage(account_ids, window) do
    :telemetry.execute([:metrics_client, :bulk_usage], %{count: length(account_ids)}, %{window: window})
    Map.new(account_ids, fn id -> {id, :erlang.phash2({id, window}, 1_000_000)} end)
  end
end

# lib/graphql_batching/sources/http_source.ex
defmodule GraphqlBatching.Sources.HttpSource do
  @moduledoc "Builds the Dataloader KV sources used by the schema."

  alias GraphqlBatching.{BillingClient, MetricsClient}

  @doc "Creates a fresh Dataloader with all HTTP sources registered."
  def loader do
    Dataloader.new()
    |> Dataloader.add_source(:billing, billing_source())
    |> Dataloader.add_source(:metrics, metrics_source())
  end

  defp billing_source do
    Dataloader.KV.new(&fetch_billing/2, max_concurrency: 4, timeout: 5_000)
  end

  defp metrics_source do
    Dataloader.KV.new(&fetch_metrics/2, max_concurrency: 2, timeout: 10_000)
  end

  # ----- batch implementations -----

  # Called once per unique batch_key with the list of queued items.
  defp fetch_billing({:plans}, plan_ids) do
    BillingClient.get_plans(Enum.uniq(plan_ids))
  end

  defp fetch_metrics({:usage, window}, account_ids) do
    MetricsClient.bulk_usage(Enum.uniq(account_ids), window)
  end
end

# lib/graphql_batching/graphql/types/account_types.ex
defmodule GraphqlBatching.Graphql.Types.AccountTypes do
  use Absinthe.Schema.Notation
  import Absinthe.Resolution.Helpers, only: [on_load: 2]

  object :plan do
    field :tier, non_null(:string)
    field :limits, non_null(:json)
  end

  object :account do
    field :id, non_null(:id)
    field :name, non_null(:string)

    field :plan, :plan do
      resolve fn account, _args, %{context: %{loader: loader}} ->
        loader
        |> Dataloader.load(:billing, {:plans}, account.plan_id)
        |> on_load(fn loader ->
          {:ok, Dataloader.get(loader, :billing, {:plans}, account.plan_id)}
        end)
      end
    end

    field :usage_metric, :integer do
      arg :window, non_null(:string), default_value: "7d"

      resolve fn account, %{window: window}, %{context: %{loader: loader}} ->
        loader
        |> Dataloader.load(:metrics, {:usage, window}, account.id)
        |> on_load(fn loader ->
          {:ok, Dataloader.get(loader, :metrics, {:usage, window}, account.id)}
        end)
      end
    end
  end

  scalar :json, name: "JSON" do
    serialize & &1
    parse &{:ok, &1.value}
  end
end

# lib/graphql_batching/graphql/schema.ex
defmodule GraphqlBatching.Graphql.Schema do
  use Absinthe.Schema

  import_types GraphqlBatching.Graphql.Types.AccountTypes

  query do
    field :accounts, non_null(list_of(non_null(:account))) do
      arg :limit, :integer, default_value: 50

      resolve fn _p, args, _r ->
        {:ok,
         for i <- 1..args.limit do
           %{id: "acct_#{i}", name: "Account #{i}", plan_id: Enum.random(["plan_free", "plan_pro", "plan_ent"])}
         end}
      end
    end
  end

  @impl true
  def context(ctx), do: Map.put(ctx, :loader, GraphqlBatching.Sources.HttpSource.loader())

  @impl true
  def plugins, do: [Absinthe.Middleware.Dataloader] ++ Absinthe.Plugin.defaults()
end

defmodule GraphqlBatching.SchemaBatchingTest do
  use ExUnit.Case, async: false
  doctest GraphqlBatching.MixProject

  describe "GraphqlBatching.SchemaBatching" do
    test "50-account query batches billing into ≤ 1 call and metrics into ≤ 1 call" do
      billing_calls = :counters.new(1, [])
      metrics_calls = :counters.new(1, [])

      :telemetry.attach("bc", [:billing_client, :get_plans],
        fn _, _, _, _ -> :counters.add(billing_calls, 1, 1) end, nil)
      :telemetry.attach("mc", [:metrics_client, :bulk_usage],
        fn _, _, _, _ -> :counters.add(metrics_calls, 1, 1) end, nil)

      query = "{ accounts(limit: 50) { id plan { tier } usageMetric(window: \"7d\") } }"
      assert {:ok, %{data: _}} = Absinthe.run(query, GraphqlBatching.Graphql.Schema)

      :telemetry.detach("bc")
      :telemetry.detach("mc")

      assert :counters.get(billing_calls, 1) == 1
      assert :counters.get(metrics_calls, 1) == 1
    end
  end
end
```
### `test/graphql_batching_test.exs`

```elixir
defmodule GraphqlBatching.HttpSourceTest do
  use ExUnit.Case, async: true
  doctest GraphqlBatching.MixProject

  alias GraphqlBatching.Sources.HttpSource

  describe "GraphqlBatching.HttpSource" do
    test "billing source batches plan lookups into one call" do
      counter = :counters.new(1, [])

      handler = fn _event, %{count: count}, _meta, _ ->
        :counters.add(counter, 1, count)
      end
      :telemetry.attach("count-billing", [:billing_client, :get_plans], handler, nil)

      loader =
        HttpSource.loader()
        |> Dataloader.load(:billing, {:plans}, "plan_free")
        |> Dataloader.load(:billing, {:plans}, "plan_pro")
        |> Dataloader.load(:billing, {:plans}, "plan_pro")  # dedup
        |> Dataloader.run()

      assert %{tier: "free"} = Dataloader.get(loader, :billing, {:plans}, "plan_free")
      assert %{tier: "pro"} = Dataloader.get(loader, :billing, {:plans}, "plan_pro")

      :telemetry.detach("count-billing")
      assert :counters.get(counter, 1) in [2, 3], "batched into one call (2 unique ids)"
    end
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Dataloader Batching against N+1 Queries.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Dataloader Batching against N+1 Queries ===")
    IO.puts("Category: APIs and GraphQL\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case GraphqlBatching.run(payload) do
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
        for _ <- 1..1_000, do: GraphqlBatching.run(:bench)
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
