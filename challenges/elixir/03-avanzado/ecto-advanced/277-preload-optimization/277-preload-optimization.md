# Preload Optimization — N+1, Batch, Join

**Project**: `blog_feed` — measuring and fixing N+1 across deep preloads.

---

## Project context

`GET /feed` returns 50 posts, each with its author, comments (and commenters), and tags.
The naive Phoenix controller issues 1 + 50 + 50 + 50×avg_comments + 50 = hundreds of
queries per request. This exercise quantifies the problem and shows three
`preload`-strategies in Ecto with explicit performance targets.

```
blog_feed/
├── lib/
│   └── blog_feed/
│       ├── application.ex
│       ├── repo.ex
│       ├── feed.ex
│       └── schemas/
│           ├── post.ex
│           ├── author.ex
│           ├── comment.ex
│           └── tag.ex
├── priv/repo/migrations/
├── test/blog_feed/
│   └── feed_test.exs
├── bench/feed_bench.exs
└── mix.exs
```

---

## The three preload strategies

### 1. Separate queries (default)

```elixir
Repo.all(Post) |> Repo.preload(:author)
```

Issues 2 queries: `SELECT posts`, `SELECT authors WHERE id IN (...)`. The second uses the
collected parent IDs. This is the default — Ecto does NOT N+1, despite what developers
new to the stack often assume.

### 2. Join preload

```elixir
from p in Post, join: a in assoc(p, :author), preload: [author: a]
```

One query with a JOIN. The same row of `authors` appears once per matching post, so the
planner deduplicates in memory. Use when you also need to filter/order by the associated
fields (`where: a.verified == true`).

### 3. Custom query preload

```elixir
Repo.preload(posts, comments: from(c in Comment, order_by: [desc: c.inserted_at], limit: 3))
```

Executes the custom subquery bound to parent IDs. The `limit: 3` applies globally here —
not per post. To get "last 3 per post" you need a window function to rank rows per group.

---

## Why N+1 happens

The first-time offender is:

```elixir
for post <- posts do
  Repo.one(from a in Author, where: a.id == ^post.author_id)
end
```

50 queries to resolve 50 authors. The fix is `Repo.preload(posts, :author)`. But deeper
N+1 sneaks through:

```elixir
posts = Repo.preload(posts, comments: :author)
# OK — 3 queries total: posts, comments, authors
```

vs.

```elixir
posts = Repo.preload(posts, :comments)
Enum.map(posts, fn p ->
  Enum.map(p.comments, fn c -> Repo.get!(Author, c.author_id) end)  # N+1
end)
```

The nested `Repo.get!` re-introduces the problem.

---

## Core concepts



---

**Why this matters:**
These concepts form the foundation of production Elixir systems. Understanding them deeply allows you to build fault-tolerant, scalable applications that operate correctly under load and failure.

**Real-world use case:**
This pattern appears in systems like:
- Phoenix applications handling thousands of concurrent connections
- Distributed data processing pipelines
- Financial transaction systems requiring consistency and fault tolerance
- Microservices communicating over unreliable networks

**Common pitfall:**
Many developers overlook that Elixir's concurrency model differs fundamentally from threads. Processes are isolated; shared mutable state does not exist. Trying to force shared-memory patterns leads to deadlocks, race conditions, or silently incorrect behavior. Always think in terms of message passing and immutability.

**Ecto-specific insight:**
Ecto separates the query layer (building queries) from the execution layer (sending them). This separation allows for debugging, composability, and testing without a database. Never load all rows first and filter in-memory — write the filter into the query itself, or you've just built an N+1 problem.
### 1. Preload is bounded by the query planner

When you preload 50 posts with an `IN (id1, id2, ..., id50)`, Postgres must parse a 50-
element array. For 10,000 parents, the IN clause grows unwieldy. Ecto auto-chunks only
when you set `in_parallel: true` with a partitioning option; otherwise it is one big
query.

### 2. `Ecto.Query.preload/3` vs `Repo.preload/3`

`preload` inside a query is resolved at query time. `Repo.preload` is a post-query call.
They differ in when filtering happens:

```elixir
# filter applies — posts without comments appear with [] comments
from p in Post, preload: [comments: ^comment_query]

# Repo.preload runs the attached query AFTER posts are loaded
Repo.preload(posts, comments: comment_query)
```

### 3. The `limit` trap

`preload: [comments: from(c in Comment, limit: 3)]` applies `LIMIT 3` to the entire
secondary query, not to each post. If you have 50 posts, you get 3 comments total across
all of them. This is a frequent production bug.

### 4. Join preload vs separate preload — which to choose

| Scenario | Prefer |
|----------|--------|
| 1:1 association (author) | join preload (one row per post anyway) |
| 1:N where N is small and you filter/order by fields | join preload |
| 1:N where N is large (comments on a post) | separate preload |
| Deep nesting (post → comments → commenter) | separate, Ecto optimizes batching |

Rule of thumb: join preloads explode rows multiplicatively. Three `has_many` joined in a
single query produce (N×M×K) rows. Separate preloads stay linear.

---

## Design decisions

- **Option A — one big join**: deliverable in one query. Pros: one round-trip.
  Cons: multiplicative row explosion, memory pressure, slow on large comment sets.
- **Option B — separate preloads**: 1 + k round-trips (k = depth). Pros: linear row count.
  Cons: k round-trips (k is 3–4 typically).

We ship **Option B** as the feed default and expose a `join_for_small_threads/0` variant
for heavy filtering needs.

---

## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  [
    {:ecto_sql, "~> 3.12"},
    {:postgrex, "~> 0.19"},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 1: Schemas

**Objective**: Define Author, Post, Comment, Tag with has_many/belongs_to/many_to_many so all preload strategies bind same graph.

```elixir
# lib/blog_feed/schemas/author.ex
defmodule BlogFeed.Schemas.Author do
  use Ecto.Schema

  schema "authors" do
    field :name, :string
    field :verified, :boolean, default: false
    has_many :posts, BlogFeed.Schemas.Post
    timestamps()
  end
end

# lib/blog_feed/schemas/post.ex
defmodule BlogFeed.Schemas.Post do
  use Ecto.Schema

  schema "posts" do
    field :title, :string
    field :published_at, :utc_datetime
    belongs_to :author, BlogFeed.Schemas.Author
    has_many :comments, BlogFeed.Schemas.Comment
    many_to_many :tags, BlogFeed.Schemas.Tag, join_through: "post_tags"
    timestamps()
  end
end

# lib/blog_feed/schemas/comment.ex
defmodule BlogFeed.Schemas.Comment do
  use Ecto.Schema

  schema "comments" do
    field :body, :string
    belongs_to :post, BlogFeed.Schemas.Post
    belongs_to :author, BlogFeed.Schemas.Author
    timestamps()
  end
end

# lib/blog_feed/schemas/tag.ex
defmodule BlogFeed.Schemas.Tag do
  use Ecto.Schema

  schema "tags" do
    field :name, :string
    many_to_many :posts, BlogFeed.Schemas.Post, join_through: "post_tags"
    timestamps()
  end
end
```

### Step 2: The feed context

**Objective**: Implement latest/1 (separate preload), latest_join/1 (join preload), latest_lateral/1 (top-N) for benchmark comparison.

```elixir
# lib/blog_feed/feed.ex
defmodule BlogFeed.Feed do
  @moduledoc """
  Feed loaders with explicit preload strategies.

  `latest/1` is the production default: linear round-trips, no row explosion.
  `latest_join/1` is an alternative for threads with ≤ 10 comments each.
  """
  import Ecto.Query

  alias BlogFeed.Repo
  alias BlogFeed.Schemas.{Comment, Post, Tag}

  # ------------------------------------------------------------------------
  # Strategy 1 — separate preloads (default)
  # 1 query for posts
  # 1 query for authors
  # 1 query for comments
  # 1 query for comment authors
  # 1 query for tags (+ join table)
  # = 5 queries regardless of post count
  # ------------------------------------------------------------------------

  @spec latest(non_neg_integer()) :: [Post.t()]
  def latest(n \\ 50) do
    posts_query =
      from p in Post,
        order_by: [desc: p.published_at],
        limit: ^n

    comment_query = from c in Comment, order_by: [asc: c.inserted_at]

    posts_query
    |> preload([:author, comments: ^{comment_query, [:author]}, tags: []])
    |> Repo.all()
  end

  # ------------------------------------------------------------------------
  # Strategy 2 — join preload for filtering on associated columns
  # ONE query. Use only when you need to filter/order by joined fields.
  # ------------------------------------------------------------------------

  @spec verified_author_feed(non_neg_integer()) :: [Post.t()]
  def verified_author_feed(n \\ 50) do
    query =
      from p in Post,
        join: a in assoc(p, :author),
        where: a.verified == true,
        order_by: [desc: p.published_at],
        limit: ^n,
        preload: [author: a]

    Repo.all(query)
  end

  # ------------------------------------------------------------------------
  # Strategy 3 — custom per-parent subquery using lateral join
  # Top-N-per-group: the last 3 comments FOR EACH post.
  # ------------------------------------------------------------------------

  @spec latest_with_top_comments(non_neg_integer(), non_neg_integer()) :: [Post.t()]
  def latest_with_top_comments(n \\ 50, top_k \\ 3) do
    posts = from(p in Post, order_by: [desc: p.published_at], limit: ^n) |> Repo.all()
    post_ids = Enum.map(posts, & &1.id)

    top_comments_sql = """
    SELECT c.*
    FROM comments c
    WHERE c.post_id = ANY($1)
      AND c.id IN (
        SELECT id FROM (
          SELECT id,
                 row_number() OVER (PARTITION BY post_id ORDER BY inserted_at DESC) AS rn
          FROM comments
          WHERE post_id = ANY($1)
        ) ranked
        WHERE rn <= $2
      )
    """

    {:ok, %{rows: rows, columns: cols}} =
      Ecto.Adapters.SQL.query(Repo, top_comments_sql, [post_ids, top_k])

    comments =
      rows
      |> Enum.map(fn row ->
        Repo.load(Comment, {cols, row})
      end)

    by_post = Enum.group_by(comments, & &1.post_id)

    for p <- posts do
      %{p | comments: Map.get(by_post, p.id, [])}
    end
  end
end
```

---

## Why this works

**Strategy 1** uses Ecto's built-in IN-based batching. Ecto collects all parent IDs and
issues one query per association level. For a depth-3 preload over 50 parents, the cost
is 1 + 3 = 4 queries regardless of fan-out.

**Strategy 2** uses a single JOIN because the filter (`a.verified == true`) must be
applied at query time, not post-load. Doing it as a separate preload would load unverified
posts too, then filter — wasted work.

**Strategy 3** solves the "top-N per group" problem via a window function in raw SQL.
Ecto's `preload` cannot express "limit N per parent"; you either do it in SQL or in
Elixir after the fact. Doing it in SQL is faster because you transmit fewer rows.

---

## Data flow (Strategy 1)

```
Feed.latest(50)
    │
    ▼
  [Q1]  SELECT * FROM posts ORDER BY published_at DESC LIMIT 50
    │   ──▶ 50 posts, collect author_ids, post_ids
    │
    ▼
  [Q2]  SELECT * FROM authors WHERE id IN (author_ids)
    │
    ▼
  [Q3]  SELECT * FROM comments WHERE post_id IN (post_ids)
    │   ──▶ collect comment_author_ids
    │
    ▼
  [Q4]  SELECT * FROM authors WHERE id IN (comment_author_ids)
    │
    ▼
  [Q5]  SELECT t.*, pt.post_id FROM tags t JOIN post_tags pt ON ...
        WHERE pt.post_id IN (post_ids)
```

Five queries, regardless of input size.

---

## Tests

```elixir
# test/blog_feed/feed_test.exs
defmodule BlogFeed.FeedTest do
  use ExUnit.Case, async: false
  alias BlogFeed.{Feed, Repo}
  alias BlogFeed.Schemas.{Author, Comment, Post, Tag}

  setup do
    Ecto.Adapters.SQL.Sandbox.checkout(Repo)
    Ecto.Adapters.SQL.Sandbox.mode(Repo, {:shared, self()})
    {:ok, a1} = Repo.insert(%Author{name: "Ada", verified: true})
    {:ok, a2} = Repo.insert(%Author{name: "Alan", verified: false})

    for n <- 1..5 do
      Repo.insert!(%Post{
        title: "P#{n}",
        published_at: DateTime.utc_now(),
        author_id: Enum.random([a1.id, a2.id])
      })
    end

    :ok
  end

  describe "latest/1" do
    test "returns posts with preloaded author" do
      posts = Feed.latest(10)
      assert length(posts) == 5
      assert Enum.all?(posts, &match?(%Author{}, &1.author))
    end

    test "preloads comments and their authors" do
      post = hd(Feed.latest(1))
      author = Repo.one(from a in Author, limit: 1)
      Repo.insert!(%Comment{body: "hi", post_id: post.id, author_id: author.id})

      [reloaded] = Feed.latest(1)
      assert [c] = reloaded.comments
      assert %Author{} = c.author
    end

    test "query count is bounded regardless of post count" do
      me = self()

      :telemetry.attach(
        "count-queries",
        [:blog_feed, :repo, :query],
        fn _event, _measurements, _meta, _config -> send(me, :query) end,
        nil
      )

      _ = Feed.latest(5)

      count = count_messages(:query, 0)
      :telemetry.detach("count-queries")

      # 5 queries: posts, authors, comments, comment authors, tags
      assert count in 4..6
    end
  end

  describe "verified_author_feed/1" do
    test "returns only posts by verified authors" do
      posts = Feed.verified_author_feed(10)
      assert Enum.all?(posts, & &1.author.verified)
    end
  end

  defp count_messages(key, acc) do
    receive do
      ^key -> count_messages(key, acc + 1)
    after
      50 -> acc
    end
  end
end
```

---

## Benchmark

```elixir
# bench/feed_bench.exs
Benchee.run(
  %{
    "latest (preload separate)" => fn -> BlogFeed.Feed.latest(50) end,
    "verified (join preload)"   => fn -> BlogFeed.Feed.verified_author_feed(50) end,
    "latest + top 3 comments"   => fn -> BlogFeed.Feed.latest_with_top_comments(50, 3) end
  },
  time: 5, warmup: 2
)
```

**Target**: `latest/1` under 15 ms for 50 posts × 20 comments each on local Postgres.
If you see > 50 ms, confirm via `:telemetry` that you are issuing 5 queries — not 55.

---

## Deep Dive

Ecto queries compile to SQL, but the translation is not always obvious. Complex preload patterns spawn subqueries for each association level—a naive nested preload can explode into hundreds of queries. Window functions and CTEs (Common Table Expressions) exist in Ecto but require raw fragments, making the boundary between Elixir and SQL explicit. For high-throughput systems, consider schemaless queries and streaming to defer memory allocation; loading 1M records as `Ecto.Repo.all/2` marshals everything into memory. Multi-tenancy via row-level database policies is cleaner than application-level filtering and leverages PostgreSQL's built-in enforcement. Zero-downtime migrations require careful orchestration: add columns before code that uses them, remove columns after code stops referencing them. Lock contention on hot rows kills throughput—use FOR UPDATE in transactions and understand when Ecto's optimistic locking is sufficient.
## Advanced Considerations

Advanced Ecto usage at scale requires understanding transaction semantics, locking strategies, and query performance under concurrent load. Ecto transactions are database transactions, not application-level transactions; they don't isolate against application-level concurrency issues. Using `:serializable` isolation level prevents anomalies but significantly impacts throughput. The choice between row-level locking with `for_update()` and optimistic locking with version columns affects both concurrency and latency. Deadlocks are not failures in Ecto; they're expected outcomes that require retry logic and careful key ordering to minimize.

Preload optimization is subtle — using `preload` for related data prevents N+1 queries but can create large intermediate result sets that exceed memory limits. Pagination with preloads requires careful consideration of whether to paginate before or after preloading related data. Custom types and schemaless queries provide flexibility but bypass Ecto's validation layer, creating opportunities for subtle bugs where invalid data sneaks into your database. The interaction between Ecto's change tracking and ETS caching can create stale data issues if not carefully managed across process boundaries.

Zero-downtime migrations require a different mental model than traditional migration scripts. Adding a column is fast; backfilling millions of rows is slow and can lock tables. Deploying code that expects the new column before the migration completes causes failures. Implement feature flags and dual-write patterns for truly zero-downtime deployments. Full-text search with PostgreSQL's tsearch requires careful index maintenance and stop-word configuration; performance characteristics change dramatically with language-specific settings and custom dictionaries.


## Deep Dive: Ecto Patterns and Production Implications

Ecto queries are composable, built up incrementally with pipes. Testing queries requires understanding that a query is lazy—until you call Repo.all, Repo.one, or Repo.update_all, no SQL is executed. This allows for property-based testing of query builders without hitting the database. Production bugs in complex queries often stem from incorrect scoping or ambiguous joins.

---

## Trade-offs and production gotchas

**1. `limit` in preload applies globally.** `preload: [comments: from(c in Comment, limit: 3)]`
returns 3 comments total across all parents. Use window functions (strategy 3) for
per-parent limits.

**2. Join preloads multiply rows.** A post with 10 comments and 3 tags joined in one query
returns 30 rows (10×3). Ecto deduplicates in Elixir, but the DB shipped 30× the data.
For fan-out > 2, prefer separate preloads.

**3. `order_by` inside a preload applies to the secondary query, not the result order
of the parent.** `preload: [comments: from(c in Comment, order_by: ...)]` orders the
comments inside each parent's list, but `posts` still need their own `order_by`.

**4. `assoc_loaded?/1` guards against accessing unpreloaded fields.** `post.comments`
when not preloaded is a `%Ecto.Association.NotLoaded{}` marker. Access it and Ecto
raises. Write helpers that check `Ecto.assoc_loaded?/1`.

**5. Postgres plan cache pressure.** Thousands of distinct `IN (...)` clauses with
varying array lengths can pressure the plan cache. If parent sets have stable sizes,
consider batching by chunk size of 100 so the planner reuses plans.

**6. When NOT to preload.** For write-heavy endpoints that only touch `post.id`, skip
the preload entirely. A `select: p.id` is 10× faster than `Repo.all(Post)`.

---

## Reflection

Your feed shows 50 posts with 20 comments each and 3 tags per post. Strategy 1 (separate
preloads) ships 50 + 1000 + 150 rows. Strategy 2 (single JOIN) ships 50 × 20 × 3 = 3000
rows. At what fan-out numbers does JOIN beat separate preloads? Derive the formula:
which is faster, N + NA + NB + NC rows in 4 round-trips, or N×A×B×C rows in 1 round-trip,
given a round-trip latency of R ms and a per-row transfer cost of t ms?

---

## Executable Example

```elixir
defmodule Main do
  defp deps do
  [
    {:ecto_sql, "~> 3.12"},
    {:postgrex, "~> 0.19"},
    {:benchee, "~> 1.3", only: :dev}
  ]


# lib/blog_feed/schemas/author.ex
defmodule BlogFeed.Schemas.Author do
  use Ecto.Schema

  schema "authors" do
    field :name, :string
    field :verified, :boolean, default: false
    has_many :posts, BlogFeed.Schemas.Post
    timestamps()
  end
end

# lib/blog_feed/schemas/post.ex
defmodule BlogFeed.Schemas.Post do
  use Ecto.Schema

  schema "posts" do
    field :title, :string
    field :published_at, :utc_datetime
    belongs_to :author, BlogFeed.Schemas.Author
    has_many :comments, BlogFeed.Schemas.Comment
    many_to_many :tags, BlogFeed.Schemas.Tag, join_through: "post_tags"
    timestamps()
  end
end

# lib/blog_feed/schemas/comment.ex
defmodule BlogFeed.Schemas.Comment do
  use Ecto.Schema

  schema "comments" do
    field :body, :string
    belongs_to :post, BlogFeed.Schemas.Post
    belongs_to :author, BlogFeed.Schemas.Author
    timestamps()
  end
end

# lib/blog_feed/schemas/tag.ex
defmodule BlogFeed.Schemas.Tag do
  use Ecto.Schema

  schema "tags" do
    field :name, :string
    many_to_many :posts, BlogFeed.Schemas.Post, join_through: "post_tags"
    timestamps()
  end
end


# lib/blog_feed/feed.ex
defmodule BlogFeed.Feed do
  @moduledoc """
  Feed loaders with explicit preload strategies.

  `latest/1` is the production default: linear round-trips, no row explosion.
  `latest_join/1` is an alternative for threads with ≤ 10 comments each.
  """
  import Ecto.Query

  alias BlogFeed.Repo
  alias BlogFeed.Schemas.{Comment, Post, Tag}

  # ------------------------------------------------------------------------
  # Strategy 1 — separate preloads (default)
  # 1 query for posts
  # 1 query for authors
  # 1 query for comments
  # 1 query for comment authors
  # 1 query for tags (+ join table)
  # = 5 queries regardless of post count
  # ------------------------------------------------------------------------

  @spec latest(non_neg_integer()) :: [Post.t()]
  def latest(n \ 50) do
    posts_query =
      from p in Post,
        order_by: [desc: p.published_at],
        limit: ^n

    comment_query = from c in Comment, order_by: [asc: c.inserted_at]

    posts_query
    |> preload([:author, comments: ^{comment_query, [:author]}, tags: []])
    |> Repo.all()
  end

  # ------------------------------------------------------------------------
  # Strategy 2 — join preload for filtering on associated columns
  # ONE query. Use only when you need to filter/order by joined fields.
  # ------------------------------------------------------------------------

  @spec verified_author_feed(non_neg_integer()) :: [Post.t()]
  def verified_author_feed(n \ 50) do
    query =
      from p in Post,
        join: a in assoc(p, :author),
        where: a.verified == true,
        order_by: [desc: p.published_at],
        limit: ^n,
        preload: [author: a]

    Repo.all(query)
  end

  # ------------------------------------------------------------------------
  # Strategy 3 — custom per-parent subquery using lateral join
  # Top-N-per-group: the last 3 comments FOR EACH post.
  # ------------------------------------------------------------------------

  @spec latest_with_top_comments(non_neg_integer(), non_neg_integer()) :: [Post.t()]
  def latest_with_top_comments(n \ 50, top_k \ 3) do
    posts = from(p in Post, order_by: [desc: p.published_at], limit: ^n) |> Repo.all()
    post_ids = Enum.map(posts, & &1.id)

    top_comments_sql = """
    SELECT c.*
    FROM comments c
    WHERE c.post_id = ANY($1)
      AND c.id IN (
        SELECT id FROM (
          SELECT id,
                 row_number() OVER (PARTITION BY post_id ORDER BY inserted_at DESC) AS rn
          FROM comments
          WHERE post_id = ANY($1)
        ) ranked
        WHERE rn <= $2
      )
    """

    {:ok, %{rows: rows, columns: cols}} =
      Ecto.Adapters.SQL.query(Repo, top_comments_sql, [post_ids, top_k])

    comments =
      rows
      |> Enum.map(fn row ->
        Repo.load(Comment, {cols, row})
      end)

    by_post = Enum.group_by(comments, & &1.post_id)

    for p <- posts do
      %{p | comments: Map.get(by_post, p.id, [])}
    end
  end
end

defmodule Main do
  def main do
      :ok
  end
end
end

Main.main()
```
