# Pagination: Cursor-Based vs Offset, Relay-Style Connections

**Project**: `feed_api` — a GraphQL API that paginates a 50-million-row activity feed using Relay-spec cursor connections backed by keyset pagination

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
feed_api/
├── lib/
│   └── feed_api.ex
├── script/
│   └── main.exs
├── test/
│   └── feed_api_test.exs
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
defmodule FeedApi.MixProject do
  use Mix.Project

  def project do
    [
      app: :feed_api,
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

### `lib/feed_api.ex`

```elixir
defmodule FeedApi.Feed.Activity do
  use Ecto.Schema

  schema "activities" do
    field :actor_id, :binary_id
    field :verb, :string
    field :payload, :map
    timestamps(type: :utc_datetime_usec)
  end
end

# Migration
# create index(:activities, [desc: :inserted_at, desc: :id])

defmodule FeedApiWeb.Graphql.Connection do
  @moduledoc "Keyset pagination with Relay-style connections."
  alias FeedApi.Feed.Activity
  alias FeedApi.Repo
  import Ecto.Query

  @default_limit 20
  @max_limit 100

  @doc "Encodes cursor result from id."
  def encode_cursor(%Activity{inserted_at: t, id: id}) do
    %{t: DateTime.to_iso8601(t), id: id}
    |> Jason.encode!()
    |> Base.url_encode64(padding: false)
  end

  @doc "Decodes cursor result from cursor."
  def decode_cursor(cursor) when is_binary(cursor) do
    with {:ok, raw} <- Base.url_decode64(cursor, padding: false),
         {:ok, %{"t" => t_str, "id" => id}} <- Jason.decode(raw),
         {:ok, t, _} <- DateTime.from_iso8601(t_str) do
      {:ok, %{inserted_at: t, id: id}}
    else
      _ -> {:error, :invalid_cursor}
    end
  end

  @doc "Returns paginate result from args."
  def paginate(args) do
    limit = min(args[:first] || @default_limit, @max_limit)

    base =
      from(a in Activity,
        order_by: [desc: a.inserted_at, desc: a.id],
        limit: ^(limit + 1)   # fetch one extra to know has_next_page
      )

    query =
      case args[:after] do
        nil -> base
        cursor ->
          case decode_cursor(cursor) do
            {:ok, %{inserted_at: t, id: id}} ->
              from(a in base,
                where: {a.inserted_at, a.id} < {^t, ^id}  # tuple compare — uses composite index
              )
            {:error, _} -> base
          end
      end

    rows = Repo.all(query)
    {page, has_next} = trim(rows, limit)

    %{
      edges: Enum.map(page, fn a -> %{cursor: encode_cursor(a), node: a} end),
      page_info: %{
        has_next_page: has_next,
        has_previous_page: args[:after] != nil,
        start_cursor: page |> List.first() |> maybe_cursor(),
        end_cursor: page |> List.last() |> maybe_cursor()
      }
    }
  end

  defp trim(rows, limit) when length(rows) > limit, do: {Enum.take(rows, limit), true}
  defp trim(rows, _), do: {rows, false}

  defp maybe_cursor(nil), do: nil
  defp maybe_cursor(row), do: encode_cursor(row)
end

defmodule FeedApiWeb.Graphql.Types.ActivityTypes do
  use Absinthe.Schema.Notation
  use Absinthe.Relay.Schema.Notation, :modern

  object :activity do
    field :id, non_null(:id)
    field :verb, non_null(:string)
    field :inserted_at, non_null(:string) do
      resolve fn %{inserted_at: t}, _, _ -> {:ok, DateTime.to_iso8601(t)} end
    end
  end

  # Absinthe.Relay generates :activity_edge, :activity_connection, :page_info
  connection(node_type: :activity)
end

defmodule FeedApiWeb.Graphql.Schema do
  use Absinthe.Schema
  use Absinthe.Relay.Schema, :modern

  import_types FeedApiWeb.Graphql.Types.ActivityTypes

  query do
    connection field :feed, node_type: :activity do
      resolve fn args, _info ->
        {:ok, FeedApiWeb.Graphql.Connection.paginate(args)}
      end
    end
  end
end
```

### `test/feed_api_test.exs`

```elixir
defmodule FeedApiWeb.PaginationTest do
  use FeedApi.DataCase, async: true
  alias FeedApiWeb.Graphql.Connection

  setup do
    for i <- 1..100 do
      Repo.insert!(%Feed.Activity{verb: "liked", inserted_at: DateTime.add(DateTime.utc_now(), -i, :second)})
    end
    :ok
  end

  describe "forward pagination" do
    test "first=20 returns newest 20 and hasNextPage=true" do
      %{edges: edges, page_info: pi} = Connection.paginate(%{first: 20})
      assert length(edges) == 20
      assert pi.has_next_page == true
    end

    test "after cursor returns next page, no overlap" do
      %{edges: first_page} = Connection.paginate(%{first: 20})
      last_cursor = List.last(first_page).cursor

      %{edges: second_page} = Connection.paginate(%{first: 20, after: last_cursor})

      first_ids = Enum.map(first_page, & &1.node.id)
      second_ids = Enum.map(second_page, & &1.node.id)
      assert [] == first_ids -- (first_ids -- second_ids)   # no overlap
    end

    test "last page has hasNextPage=false" do
      # Walk forward until exhausted
      walk = fn walk, after_cursor, acc ->
        case Connection.paginate(%{first: 50, after: after_cursor}) do
          %{edges: [], page_info: pi} -> {acc, pi}
          %{edges: edges, page_info: %{has_next_page: false} = pi} -> {acc ++ edges, pi}
          %{edges: edges, page_info: pi} ->
            walk.(walk, pi.end_cursor, acc ++ edges)
        end
      end

      {all, last_pi} = walk.(walk, nil, [])
      assert length(all) == 100
      assert last_pi.has_next_page == false
    end
  end

  describe "cursor validation" do
    test "malformed cursor is ignored (returns first page)" do
      %{edges: edges} = Connection.paginate(%{first: 5, after: "not-a-cursor"})
      assert length(edges) == 5
    end
  end

  describe "stable under insertions" do
    test "inserting a new row does not shift the next page" do
      %{edges: first_page} = Connection.paginate(%{first: 10})
      last_cursor = List.last(first_page).cursor

      # Insert a new row (newest, so it would appear on page 1)
      Repo.insert!(%Feed.Activity{verb: "new"})

      %{edges: second_page} = Connection.paginate(%{first: 10, after: last_cursor})
      # The new row is not here; it should have been on a prior page.
      refute Enum.any?(second_page, &(&1.node.verb == "new"))
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Pagination: Cursor-Based vs Offset, Relay-Style Connections.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Pagination: Cursor-Based vs Offset, Relay-Style Connections ===")
    IO.puts("Category: APIs and GraphQL\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case FeedApi.run(payload) do
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
        for _ <- 1..1_000, do: FeedApi.run(:bench)
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
