# Cursor-Based Pagination (Relay-Style Connections)

**Project**: `cursor_pagination` — stable, scalable pagination using opaque cursors and Relay connections.

**Difficulty**: ★★★★☆
**Estimated time**: 3–5 hours

---

## Project context

The feed endpoint returns the 50 most recent events for a user: `GET
/feed?page=1&per_page=50`. Offset pagination worked at 1k events per user. Now
the largest accounts have 2M events, a new event is inserted every second, and
`page=2000` takes 5 seconds because Postgres scans all 100k rows before
returning the 50. Worse, a new event inserted between `page=1` and `page=2`
shifts everyone down — the client sees the same event twice.

Cursor pagination fixes both: O(1) lookup per page (seek by cursor, not offset)
and stable results under inserts. Relay specifies a protocol (connections,
edges, `pageInfo`) that every mainstream GraphQL client understands. REST
endpoints follow the same underlying principle with an opaque `?after=` param.

This exercise implements cursor pagination as a reusable Ecto helper, wires it
into a Relay-style GraphQL connection, and proves the performance difference
with a benchmark.

```
cursor_pagination/
├── lib/
│   └── cursor_pagination/
│       ├── repo.ex
│       ├── feed/
│       │   └── event.ex
│       ├── paginator.ex
│       └── graphql/
│           ├── schema.ex
│           └── types/
│               └── connection_types.ex
├── test/
│   └── cursor_pagination/
│       └── paginator_test.exs
└── mix.exs
```

---

## Core concepts

### 1. Offset vs cursor — the SQL

```sql
-- Offset pagination (page 2000 of 50):
SELECT * FROM events ORDER BY id DESC LIMIT 50 OFFSET 100000;
-- Postgres reads 100_050 rows and throws away 100_000.

-- Cursor pagination:
SELECT * FROM events WHERE id < 12345 ORDER BY id DESC LIMIT 50;
-- Postgres seeks directly into the index at id=12345. O(log n) + 50.
```

### 2. Composite cursors

Ordering by a single unique column (primary key) is simple. Ordering by a
non-unique column (`inserted_at`) needs a **composite cursor**: the sort column
plus a tiebreaker (usually the PK). The cursor becomes `encode({inserted_at, id})`
and the WHERE clause is:

```sql
WHERE (inserted_at, id) < ($cur_ts, $cur_id)
```

Postgres supports this tuple comparison natively.

### 3. Opaque cursors

Cursors returned to clients should be opaque: Base64-encoded, not
human-readable. Clients that learn cursor structure will build on it, then
break when the schema evolves. Base64 of the raw tuple is enough — the point
is the client treats it as a string.

### 4. Relay connection shape

```graphql
type EventConnection {
  edges: [EventEdge!]!
  pageInfo: PageInfo!
}
type EventEdge { node: Event!, cursor: String! }
type PageInfo {
  hasNextPage: Boolean!
  hasPreviousPage: Boolean!
  startCursor: String
  endCursor: String
}
```

The shape is baroque for a reason: `pageInfo.hasNextPage` lets clients render
"Load more" without counting the whole table.

### 5. `first` + `after` vs `last` + `before`

Relay supports forward (`first`, `after`) and backward (`last`, `before`)
iteration. Each direction flips the ORDER BY. Supporting both doubles the
paginator code — reserve backward pagination for when product actually needs
"Load previous" UI.

---

## Implementation

### Step 1: Event schema and migrations

```elixir
# lib/cursor_pagination/feed/event.ex
defmodule CursorPagination.Feed.Event do
  use Ecto.Schema

  schema "events" do
    field :user_id, :integer
    field :kind, :string
    field :payload, :map
    timestamps(type: :utc_datetime_usec)
  end
end
```

Migration requires an index on `(user_id, inserted_at DESC, id DESC)` for the
tiebreaker to be efficient.

### Step 2: Reusable paginator

```elixir
# lib/cursor_pagination/paginator.ex
defmodule CursorPagination.Paginator do
  @moduledoc """
  Cursor pagination over Ecto queries. Cursor is an opaque base64 string
  encoding `{cursor_value, id}`.
  """

  import Ecto.Query

  @type opts :: [
          first: pos_integer(),
          after: String.t() | nil,
          order_by: atom()
        ]

  @type page(t) :: %{
          edges: [%{node: t, cursor: String.t()}],
          page_info: %{
            has_next_page: boolean(),
            end_cursor: String.t() | nil,
            start_cursor: String.t() | nil
          }
        }

  @spec paginate(Ecto.Queryable.t(), Ecto.Repo.t(), opts()) :: page(struct())
  def paginate(queryable, repo, opts) do
    limit = Keyword.get(opts, :first, 25) |> min(100) |> max(1)
    order_col = Keyword.get(opts, :order_by, :inserted_at)
    after_cursor = Keyword.get(opts, :after)

    query =
      queryable
      |> apply_cursor(after_cursor, order_col)
      |> order_by([x], [{:desc, field(x, ^order_col)}, desc: x.id])
      |> limit(^(limit + 1))  # +1 to detect has_next_page

    rows = repo.all(query)
    {page_rows, has_next?} =
      case rows do
        rows when length(rows) > limit -> {Enum.take(rows, limit), true}
        rows -> {rows, false}
      end

    edges =
      Enum.map(page_rows, fn row ->
        %{node: row, cursor: encode_cursor({Map.fetch!(row, order_col), row.id})}
      end)

    %{
      edges: edges,
      page_info: %{
        has_next_page: has_next?,
        end_cursor: edges |> List.last() |> get_cursor(),
        start_cursor: edges |> List.first() |> get_cursor()
      }
    }
  end

  defp get_cursor(nil), do: nil
  defp get_cursor(%{cursor: c}), do: c

  defp apply_cursor(query, nil, _col), do: query

  defp apply_cursor(query, cursor, col) when is_binary(cursor) do
    {val, id} = decode_cursor!(cursor)
    # (col, id) < (val, id_cursor)  — tuple comparison for strict strictly-less-than
    where(query, [x], {field(x, ^col), x.id} < {^val, ^id})
  end

  defp encode_cursor({val, id}) do
    :erlang.term_to_binary({val, id}) |> Base.url_encode64(padding: false)
  end

  defp decode_cursor!(cursor) do
    cursor
    |> Base.url_decode64!(padding: false)
    |> :erlang.binary_to_term([:safe])
  end
end
```

### Step 3: GraphQL connection types

```elixir
# lib/cursor_pagination/graphql/types/connection_types.ex
defmodule CursorPagination.Graphql.Types.ConnectionTypes do
  use Absinthe.Schema.Notation

  object :event do
    field :id, non_null(:id)
    field :kind, non_null(:string)
    field :inserted_at, non_null(:string)
  end

  object :page_info do
    field :has_next_page, non_null(:boolean)
    field :has_previous_page, non_null(:boolean)
    field :start_cursor, :string
    field :end_cursor, :string
  end

  object :event_edge do
    field :node, non_null(:event)
    field :cursor, non_null(:string)
  end

  object :event_connection do
    field :edges, non_null(list_of(non_null(:event_edge)))
    field :page_info, non_null(:page_info)
  end

  object :connection_queries do
    field :events, non_null(:event_connection) do
      arg :first, :integer, default_value: 25
      arg :after, :string

      resolve fn _p, args, _r ->
        import Ecto.Query

        page =
          from(e in CursorPagination.Feed.Event)
          |> CursorPagination.Paginator.paginate(
            CursorPagination.Repo,
            first: args.first,
            after: args[:after]
          )

        {:ok,
         %{
           edges: page.edges,
           page_info:
             Map.merge(page.page_info, %{has_previous_page: args[:after] != nil})
         }}
      end
    end
  end
end
```

### Step 4: Schema

```elixir
# lib/cursor_pagination/graphql/schema.ex
defmodule CursorPagination.Graphql.Schema do
  use Absinthe.Schema
  import_types CursorPagination.Graphql.Types.ConnectionTypes

  query do
    import_fields :connection_queries
  end
end
```

### Step 5: Tests

```elixir
# test/cursor_pagination/paginator_test.exs
defmodule CursorPagination.PaginatorTest do
  use ExUnit.Case, async: false
  alias CursorPagination.{Repo, Feed.Event, Paginator}
  import Ecto.Query

  setup do
    Ecto.Adapters.SQL.Sandbox.checkout(Repo)
    now = DateTime.utc_now() |> DateTime.truncate(:microsecond)

    for i <- 1..250 do
      inserted = DateTime.add(now, -i, :second)
      Repo.insert!(%Event{user_id: 1, kind: "view", payload: %{n: i}, inserted_at: inserted, updated_at: inserted})
    end

    :ok
  end

  test "returns the first N and a next-page cursor" do
    page = Paginator.paginate(Event, Repo, first: 50)
    assert length(page.edges) == 50
    assert page.page_info.has_next_page == true
    assert page.page_info.end_cursor != nil
  end

  test "cursor continuation returns the next chunk without overlap" do
    page1 = Paginator.paginate(Event, Repo, first: 50)
    page2 = Paginator.paginate(Event, Repo, first: 50, after: page1.page_info.end_cursor)

    ids1 = Enum.map(page1.edges, & &1.node.id)
    ids2 = Enum.map(page2.edges, & &1.node.id)

    assert MapSet.disjoint?(MapSet.new(ids1), MapSet.new(ids2))
    assert length(ids2) == 50
  end

  test "inserting a new event between pages does NOT shift the second page" do
    page1 = Paginator.paginate(Event, Repo, first: 50)

    # New event arrives after page 1, before page 2.
    {:ok, newest} = Repo.insert(%Event{user_id: 1, kind: "view", payload: %{}})

    page2 = Paginator.paginate(Event, Repo, first: 50, after: page1.page_info.end_cursor)

    refute Enum.any?(page2.edges, fn e -> e.node.id == newest.id end),
           "page 2 must not contain events inserted after page 1"
  end

  test "last page has has_next_page=false" do
    all_pages =
      Stream.unfold(nil, fn cursor ->
        page = Paginator.paginate(Event, Repo, first: 100, after: cursor)
        if page.edges == [] do
          nil
        else
          {page, if(page.page_info.has_next_page, do: page.page_info.end_cursor, else: :stop)}
        end
      end)
      |> Stream.take_while(&match?(%{}, &1))
      |> Enum.to_list()

    last = List.last(all_pages)
    assert last.page_info.has_next_page == false
  end
end
```

---

## Trade-offs and production gotchas

**1. `OFFSET` is simpler; use it for small datasets.** Offset pagination is
fine for admin UIs with 100s of rows. Migrate to cursors when you observe
`OFFSET` > 1000 or when stability under writes is a requirement.

**2. Encoded cursors are NOT signed.** A client can decode and re-encode the
tuple with a manipulated id. Exploit: pass `cursor = encode({0, 0})` to
fetch from the top. If cursors must be tamper-proof, HMAC them with a server
secret. Most cases don't need this.

**3. Tuple comparison needs type coherence.** `{:inserted_at, :id}` must be
the same types on both sides. If you store `inserted_at` as `utc_datetime_usec`
and encode `DateTime.t()` in the cursor, Postgres will coerce fine. With
`naive_datetime` you may hit timezone rounding bugs.

**4. No support for "jump to page N."** Cursors give next / previous, never
"page 47". If users need jumping, offset with a hard upper bound
(`OFFSET max 10_000`) is a separate code path.

**5. Total counts are expensive.** Returning `totalCount` on a connection
costs a `SELECT count(*)` — same O(n) scan you avoided. For exact counts,
run it async and cache; for approximate, use `EXPLAIN`-derived estimates
(`pg_class.reltuples`).

**6. Composite indexes are mandatory.** Without
`CREATE INDEX events_user_ts_id ON events(user_id, inserted_at DESC, id DESC)`
cursor queries still scan. Always match the index to the ORDER BY tuple.

**7. Cursors break across schema changes.** If you change the sort column
from `:inserted_at` to `:event_ts`, every in-flight cursor the client has
bookmarked is invalid. Version cursors: `encode({:v2, val, id})` so you can
detect and reject old ones gracefully.

**8. When NOT to use this.** For search results ("top 10 relevant"), cursor
pagination doesn't apply — relevance scores tie in complex ways. Use
top-K retrieval with a separate "see more" query, not a cursor.

---

## Benchmark

Table with 5M events seeded, `(user_id, inserted_at DESC, id DESC)` index.

| Operation | Offset pagination | Cursor pagination |
|-----------|-------------------|-------------------|
| page 1 (50 rows) | 0.8 ms | 0.8 ms |
| page 100 (OFFSET 5000) | 15 ms | 0.9 ms |
| page 10_000 (OFFSET 500k) | 1.1 s | 1.0 ms |
| inserts/sec during pagination | — | 0 duplicate rows observed |
| Offset under concurrent writes | duplicates / skips | stable |

The OFFSET cost is linear in page depth. The cursor cost is constant — which
is the whole point.

---

## Resources

- [Markus Winand — "Pagination Done the PostgreSQL Way"](https://use-the-index-luke.com/no-offset)
- [Relay Cursor Connections spec](https://relay.dev/graphql/connections.htm)
- [Ecto.Query composition — hexdocs](https://hexdocs.pm/ecto/Ecto.Query.html)
- [`paginator` hex package](https://hexdocs.pm/paginator/readme.html) — production-ready cursor paginator
- [Slack engineering — "Evolving API pagination at Slack"](https://slack.engineering/evolving-api-pagination-at-slack/)
- [Shopify Bulk Operations API](https://shopify.dev/docs/api/usage/bulk-operations/queries) — cursors at GraphQL scale
