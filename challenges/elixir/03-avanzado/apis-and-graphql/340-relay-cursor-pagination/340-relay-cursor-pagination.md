# Pagination: Cursor-Based vs Offset, Relay-Style Connections

**Project**: `feed_api` — a GraphQL API that paginates a 50-million-row activity feed using Relay-spec cursor connections backed by keyset pagination.

## Project context

`feed_api` powers the "activity feed" of a social product. The table has 50 M rows and grows by 1 M per day. The v1 implementation used `OFFSET/LIMIT`; beyond page 100 the database was spending 200 ms scanning rows to skip them. Users near page 500 saw 2-second loads.

This exercise replaces offset pagination with **keyset (seek) pagination**, wrapped in the **Relay Cursor Connections Specification**. The combination gives you stable cursors (insertions don't shift pages), O(1) latency regardless of offset, and a spec that every major GraphQL client (Apollo, Relay, urql) understands for free.

```
feed_api/
├── lib/
│   ├── feed_api/
│   │   ├── application.ex
│   │   ├── repo.ex
│   │   ├── feed.ex                    # context
│   │   └── feed/activity.ex
│   └── feed_api_web/
│       └── graphql/
│           ├── schema.ex
│           ├── types/activity_types.ex
│           └── connection.ex          # Relay connection helpers
├── test/feed_api_web/pagination_test.exs
├── bench/pagination_bench.exs
└── mix.exs
```

## Why keyset and not offset

Offset pagination:

```sql
SELECT * FROM activities ORDER BY id DESC OFFSET 100000 LIMIT 20;
```

The database MUST read and discard 100 000 rows. Latency is O(offset). Worse, if a row is inserted between page 50 and page 51, the user sees the same row twice (or misses one).

Keyset pagination:

```sql
SELECT * FROM activities WHERE id < $last_id ORDER BY id DESC LIMIT 20;
```

Index seek → 20 rows → done. Latency is O(log N) regardless of how deep the user scrolled. Stable under insertions.

The catch: you lose the ability to "jump to page 500". Keyset is "next/prev" only. For an infinite-scroll feed, that is exactly the right model.

## Why Relay connections and not raw `{items, cursor}`

Every team invents their own shape:

```json
{"items": [...], "next_cursor": "abc"}
{"data": [...], "pagination": {"after": "xyz"}}
{"edges": [...]}  // still different
```

The Relay spec standardizes the envelope:

```graphql
type ActivityConnection {
  edges: [ActivityEdge!]!
  pageInfo: PageInfo!
}
type ActivityEdge { cursor: String!, node: Activity! }
type PageInfo { hasNextPage: Boolean!, hasPreviousPage: Boolean!, startCursor: String, endCursor: String }
```

Every mainstream GraphQL client has built-in cache updaters for this shape (`fetchMore`, infinite list semantics). Deviating costs client-side work.

## Core concepts

### 1. Cursor is opaque to the client
It is a base64-encoded record of "the last row you saw". Clients treat it as a black box.

### 2. Cursor contents
The tuple must uniquely identify a row in the sort order. For `ORDER BY inserted_at DESC, id DESC`, the cursor is `{inserted_at, id}`. A cursor with only `inserted_at` breaks on duplicate timestamps.

### 3. `first`/`after` vs `last`/`before`
Relay supports forward AND backward pagination. `first: 10, after: cursor` = next 10 after that cursor. `last: 10, before: cursor` = previous 10 before.

## Design decisions

- **Option A — cursor = base64(`id`)**: pros: simple; cons: only works when sorting by `id`, which is rarely what product wants.
- **Option B — cursor = base64(JSON of the sort tuple)**: pros: generalizes to any sort order; cons: leaks schema; cursors are bigger.
- **Option C — cursor = base64(signed binary)**: pros: tamper-proof; cons: ceremony.
→ We pick **B** for the teaching implementation and note **C** as the production hardening if cursors are exposed to untrusted clients.

## Implementation

### Dependencies

```elixir
defp deps do
  [
    {:phoenix, "~> 1.7"},
    {:ecto_sql, "~> 3.11"},
    {:postgrex, "~> 0.17"},
    {:absinthe, "~> 1.7"},
    {:absinthe_relay, "~> 1.5"},
    {:jason, "~> 1.4"}
  ]
end
```

### Step 1: Schema with an index that supports the sort order

**Objective**: Ship a composite `(inserted_at DESC, id DESC)` index so the keyset predicate seeks in O(log N) instead of degrading to a sort-and-scan.

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
```

The composite index on `(inserted_at DESC, id DESC)` is essential. Postgres can seek to `(t, id)` in O(log N) using that index.

### Step 2: Cursor encode/decode

**Objective**: Encode `(t, id)` as opaque base64 JSON and tuple-compare in SQL so cursors are stable, corruption-safe, and resolve against the composite index.

```elixir
defmodule FeedApiWeb.Graphql.Connection do
  @moduledoc "Keyset pagination with Relay-style connections."
  alias FeedApi.Feed.Activity
  alias FeedApi.Repo
  import Ecto.Query

  @default_limit 20
  @max_limit 100

  def encode_cursor(%Activity{inserted_at: t, id: id}) do
    %{t: DateTime.to_iso8601(t), id: id}
    |> Jason.encode!()
    |> Base.url_encode64(padding: false)
  end

  def decode_cursor(cursor) when is_binary(cursor) do
    with {:ok, raw} <- Base.url_decode64(cursor, padding: false),
         {:ok, %{"t" => t_str, "id" => id}} <- Jason.decode(raw),
         {:ok, t, _} <- DateTime.from_iso8601(t_str) do
      {:ok, %{inserted_at: t, id: id}}
    else
      _ -> {:error, :invalid_cursor}
    end
  end

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
```

### Step 3: GraphQL types

**Objective**: Use `Absinthe.Relay`'s `connection(node_type: :activity)` macro so edge, connection, and pageInfo types are generated spec-compliant by default.

```elixir
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
```

### Step 4: Root schema field

**Objective**: Expose `connection field :feed` so `first/after/last/before` args arrive typed and the resolver only handles keyset logic, not argument parsing.

```elixir
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

Clients now query:

```graphql
{
  feed(first: 20, after: "eyJ0IjoiMjAyNi0wNC0xMlQxMDowMDowMFoiLCJpZCI6OTk5OX0") {
    edges { cursor node { id verb insertedAt } }
    pageInfo { hasNextPage endCursor }
  }
}
```

## Why this works

```
   request: first=20, after=<cursor X>
        │
        ▼
  decode cursor → {inserted_at: T, id: I}
        │
        ▼
  SQL: SELECT * FROM activities
       WHERE (inserted_at, id) < (T, I)
       ORDER BY inserted_at DESC, id DESC
       LIMIT 21
        │
        ▼
  index seek on (inserted_at DESC, id DESC)
  Postgres jumps directly to (T, I) position
  reads 21 rows → done in sub-millisecond
        │
        ▼
  rows[0..19] → edges; row[20] existence → hasNextPage
  each edge's cursor = base64(its {inserted_at, id})
```

The composite tuple comparison `(a.inserted_at, a.id) < (T, I)` is the key trick. Postgres implements lexicographic tuple comparison natively and the planner will use a composite index for it. Without the tuple form — `WHERE inserted_at < T OR (inserted_at = T AND id < I)` — the planner often falls back to a full scan on duplicate timestamps.

## Tests

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

## Benchmark

```elixir
# bench/pagination_bench.exs — assumes a seeded 1M-row table
cursor_deep = # compute: encode_cursor of the row at position 500_000

Benchee.run(%{
  "offset 500_000" => fn ->
    Repo.all(from a in Activity, order_by: [desc: a.id], offset: 500_000, limit: 20)
  end,
  "keyset deep"    => fn ->
    FeedApiWeb.Graphql.Connection.paginate(%{first: 20, after: cursor_deep})
  end
})
```

**Expected**:

- `offset 500_000`: **~50–200 ms** (grows linearly with offset)
- `keyset deep`: **< 2 ms** (constant)

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

**1. Cursor missing the tiebreaker**
`ORDER BY inserted_at DESC` with cursor = `inserted_at` only. Two rows with the same timestamp produce duplicate or missing entries across pages. Always include a unique tiebreaker (`id`).

**2. No `(col1, col2)` index**
The query uses a composite tuple comparison, but if your index is only on `inserted_at`, Postgres falls back to a sort. Add the composite index matching the order.

**3. Allowing unbounded `first`**
A client asking `first: 1_000_000` loads a million rows. Cap at `@max_limit` (100 is common).

**4. Exposing raw `id` as cursor**
For public APIs, cursors leak business signals (total row count, row age). Encrypt or sign them.

**5. `hasPreviousPage` always false**
Easy to implement as "`after != nil`", which is a useful heuristic but technically wrong if someone arrives with a cursor pointing to the first row. Document the semantics.

**6. When NOT to use keyset**
When users need to "jump to page N" (admin dashboards, pagers over static reports), offset is still the right choice. Don't contort UX to fit keyset if the product needs random access.

## Reflection

Your feed sorts by `score DESC` (not time), where `score` is a mutable column recomputed hourly. A user loads page 1, idles for 20 minutes; scores reshuffle. When the user scrolls, the cursor references `(old_score, id)` — rows have moved past the cursor position. Describe what the user sees and sketch two mitigations: server-side snapshotting vs accepting the anomaly with a stale indicator.

## Resources

- [Relay Cursor Connections Specification](https://relay.dev/graphql/connections.htm)
- [Use The Index, Luke — Paging through results](https://use-the-index-luke.com/no-offset)
- [Markus Winand — Pagination done the right way](https://use-the-index-luke.com/sql/partial-results/fetch-next-page)
- [`Absinthe.Relay` connections](https://hexdocs.pm/absinthe_relay/Absinthe.Relay.Connection.html)

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```
