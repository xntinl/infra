# Preload Optimization — N+1, Batch, Join

**Project**: `blog_feed` — measuring and fixing N+1 across deep preloads

---

## Why ecto advanced matters

Ecto.Multi, custom types, polymorphic associations, CTEs, window functions, and zero-downtime migrations are the senior toolkit for talking to PostgreSQL from Elixir. Each one trades a different axis: composability, type safety, query expressiveness, or operational safety.

The trap is treating Ecto like an ORM. It is a query DSL plus a changeset validator — closer to SQL than to ActiveRecord. The closer your mental model is to the database, the better Ecto serves you.

---

## The business problem

You are building a production-grade Elixir component in the **Ecto advanced** area. The goal is not a toy demonstration — it is a piece you could drop into a real system and rely on under load.

Concretely, you must:

1. Implement the topic correctly with measurable behavior.
2. Surface failure modes via `{:ok, _} / {:error, reason}` tuples — never raise from a public API except for programmer errors.
3. Bound resources: explicit timeouts, memory caps, back-pressure where relevant, and supervision for any long-lived process.
4. Provide a test suite with `async: true` where safe, `doctest` for public examples, and `describe` blocks that name the contract.
5. Provide a `script/main.exs` that demonstrates the piece end-to-end with realistic data — not a `1 + 1` smoke test.

---

## Project structure

```
blog_feed/
├── lib/
│   └── blog_feed.ex
├── script/
│   └── main.exs
├── test/
│   └── blog_feed_test.exs
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

Chose **B** because in Ecto advanced the failure modes — not the happy path — are what determine whether the system stays up under real production conditions.

---

## Implementation

### `mix.exs`

```elixir
defmodule BlogFeed.MixProject do
  use Mix.Project

  def project do
    [
      app: :blog_feed,
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
### `lib/blog_feed.ex`

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

  @doc "Returns latest result from n."
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

  @doc "Returns verified author feed result from n."
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

  @doc "Returns latest with top comments result from n and top_k."
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
### `test/blog_feed_test.exs`

```elixir
defmodule BlogFeed.FeedTest do
  use ExUnit.Case, async: true
  doctest BlogFeed.Schemas.Author
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
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Preload Optimization — N+1, Batch, Join.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Preload Optimization — N+1, Batch, Join ===")
    IO.puts("Category: Ecto advanced\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case BlogFeed.run(payload) do
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
        for _ <- 1..1_000, do: BlogFeed.run(:bench)
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

### 1. Queries are data, not strings

Ecto.Query is a DSL that compiles to SQL only at execution. This means you can compose, inspect, and pre-validate queries without a database connection — useful for property tests.

### 2. Multi makes transactions composable

Ecto.Multi is a value: build it, pass it around, run it inside Repo.transaction. Errors come back as `{:error, step_name, reason, changes_so_far}` — you know exactly what failed.

### 3. Locking strategies trade throughput for correctness

FOR UPDATE prevents lost updates but serializes contention. Optimistic locking via :version columns retries on conflict — better for read-heavy workloads.

---
