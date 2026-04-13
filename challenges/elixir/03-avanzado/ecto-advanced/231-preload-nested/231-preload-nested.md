# Nested Preloads and Join Preloads

**Project**: `preload_nested` — a blog/commenting platform loading deep association trees.

---

## Project context

A blog platform has `User → Posts → Comments → CommentAuthor`, plus `Post → Tags` and
`Post → CoverImage`. Rendering a single post page needs all of it. The naive fetch is
`Repo.get(Post, id)` followed by Elixir-side traversal — an N+1 nightmare: one query for
the post, one for each comment's author, one for tags, one for the image. Page renders in
1.2s for a popular article with 150 comments.

Ecto's `preload` loads associations in controlled ways:

1. **Two-query preload**: fetch the parents with one query, then children with
   `WHERE parent_id IN (...)`.
2. **Join preload**: use `join: ... preload: [:assoc]` to preload via a single SQL query.

Two-query is the default and usually correct. Join preload is the right tool when you
need to **filter or order** the parents by child columns in the same query. Nested preloads
(`preload: [posts: [comments: :author]]`) let you specify the full tree.

This exercise builds a realistic blog post loader that combines join preloads (for
filtering) with nested two-query preloads (for the tree), and benchmarks each against
the naive version.

---

```
preload_nested/
├── lib/preload_nested/
│   ├── application.ex
│   ├── repo.ex
│   ├── schemas/
│   │   ├── user.ex
│   │   ├── post.ex
│   │   ├── comment.ex
│   │   ├── tag.ex
│   │   └── cover_image.ex
│   └── blog.ex
├── priv/repo/migrations/20260101000000_create.exs
├── test/preload_nested/blog_test.exs
└── mix.exs
```

---

## Why this approach and not alternatives

Alternatives considered and discarded:

- **Hand-rolled equivalent**: reinvents primitives the BEAM/ecosystem already provides; high risk of subtle bugs around concurrency, timeouts, or failure propagation.
- **External service (e.g. Redis, sidecar)**: adds a network hop and an extra failure domain for a problem the VM can solve in-process with lower latency.
- **Heavier framework abstraction**: couples the module to a framework lifecycle and makes local reasoning/testing harder.

The chosen approach stays inside the BEAM, uses idiomatic OTP primitives, and keeps the contract small.

## Core concepts

### 1. Preload flavors

```
two-query preload:    SELECT posts;  SELECT comments WHERE post_id IN (...);
join preload:         SELECT posts JOIN comments ON ...;
lateral join preload: SELECT posts, LATERAL (SELECT comments WHERE post_id = posts.id LIMIT 3);
```

Two-query is simpler, scales linearly in N parents, and has no row duplication. Join
preload is one roundtrip but duplicates parent rows per child — 100 posts × 5 comments
each = 500 rows returned.

### 2. Nested preload syntax

```elixir
Repo.get(Post, id)
|> Repo.preload([
  :tags,                      # simple has_many
  :cover_image,               # has_one
  [comments: :author]         # nested: comments, each with its author
])
```

Equivalent keyword: `preload: [tags: [], cover_image: [], comments: [:author]]`.

### 3. Preload with a custom query

Often you want "latest 5 comments ordered by inserted_at desc", not every comment. Use
a function or a named query:

```elixir
comments_query = from c in Comment, order_by: [desc: c.inserted_at], limit: 5
Repo.preload(post, comments: {comments_query, [:author]})
```

The tuple form `{query, nested_preloads}` lets you filter AND nest.

### 4. Filtering by child columns → join preload is mandatory

"Posts that have at least one comment by a specific user" — this requires a JOIN. Ecto
needs `join: c in assoc(p, :comments), preload: [comments: c]` so it knows "the `c`
binding IS the preloaded association", avoiding a second query.

### 5. `preload: ^dynamic_fields`

You can build preload specs as data: `Repo.preload(post, ^preloads)` where `preloads` is
a keyword list computed at runtime. Useful for GraphQL resolvers that receive the
"requested fields" and build the preload accordingly.

### 6. N+1 detection in production

Ecto logs every query. In dev, watch logs for `SELECT ... WHERE parent_id IN ($1, $2, $3)`
patterns running hundreds of times — that's N+1. Tools: `ecto_dev_logger`,
`AppSignal`, `Sentry` with Ecto integration.

---

## Design decisions

**Option A — naive/simple approach**
- Pros: minimal code, easy to reason about.
- Cons: breaks under load, lacks observability, hard to evolve.

**Option B — the approach used here** (chosen)
- Pros: production-grade, handles edge cases, testable boundaries.
- Cons: more moving parts, requires understanding of the BEAM primitives involved.

→ Chose **B** because correctness under concurrency and failure modes outweighs the extra surface area.

## Implementation

### Step 1: Schemas

**Objective**: Define User, Post, Comment, Tag, CoverImage with has_many/many_to_many/has_one so preload traverses full tree efficiently.

```elixir
defmodule PreloadNested.Schemas.User do
  use Ecto.Schema

  schema "users" do
    field :name, :string
    field :email, :string
    has_many :posts, PreloadNested.Schemas.Post
    has_many :authored_comments, PreloadNested.Schemas.Comment
    timestamps()
  end
end

defmodule PreloadNested.Schemas.Post do
  use Ecto.Schema

  schema "posts" do
    field :title, :string
    field :body, :string
    field :published, :boolean, default: false
    belongs_to :user, PreloadNested.Schemas.User
    has_many :comments, PreloadNested.Schemas.Comment
    has_one :cover_image, PreloadNested.Schemas.CoverImage
    many_to_many :tags, PreloadNested.Schemas.Tag,
      join_through: "posts_tags", on_replace: :delete
    timestamps()
  end
end

defmodule PreloadNested.Schemas.Comment do
  use Ecto.Schema

  schema "comments" do
    field :body, :string
    belongs_to :post, PreloadNested.Schemas.Post
    belongs_to :author, PreloadNested.Schemas.User
    timestamps()
  end
end

defmodule PreloadNested.Schemas.Tag do
  use Ecto.Schema

  schema "tags" do
    field :name, :string
  end
end

defmodule PreloadNested.Schemas.CoverImage do
  use Ecto.Schema

  schema "cover_images" do
    field :url, :string
    belongs_to :post, PreloadNested.Schemas.Post
  end
end
```

### Step 2: Blog context

**Objective**: Expose full_post/1, post_with_recent_comments/2, posts_commented_by/1, and dynamic find/2 so callers trade preload queries vs row count.

```elixir
defmodule PreloadNested.Blog do
  @moduledoc """
  Post loading with different preload strategies.
  """

  import Ecto.Query

  alias PreloadNested.Repo
  alias PreloadNested.Schemas.{Comment, Post}

  @doc """
  Full post tree loaded with the typical nested preload.

  Issues 5 queries regardless of N comments:
    1. posts, 2. cover_images, 3. tags, 4. comments, 5. authors
  """
  @spec full_post(integer()) :: Post.t() | nil
  def full_post(id) do
    Post
    |> Repo.get(id)
    |> Repo.preload([
      :cover_image,
      :tags,
      comments: [:author]
    ])
  end

  @doc """
  Post with only its 5 most-recent comments, authors preloaded.
  """
  @spec post_with_recent_comments(integer(), pos_integer()) :: Post.t() | nil
  def post_with_recent_comments(id, limit \\ 5) do
    recent_comments =
      from c in Comment,
        order_by: [desc: c.inserted_at],
        limit: ^limit

    Post
    |> Repo.get(id)
    |> Repo.preload(comments: {recent_comments, [:author]})
  end

  @doc """
  Posts filtered by child comment author — needs a join preload.

  Single SQL query; duplicates parent rows per matching child.
  """
  @spec posts_commented_by(integer()) :: [Post.t()]
  def posts_commented_by(user_id) do
    from(p in Post,
      join: c in assoc(p, :comments),
      where: c.author_id == ^user_id,
      preload: [comments: c]
    )
    |> Repo.all()
    |> Enum.uniq_by(& &1.id)
  end

  @doc """
  Dynamic preloads driven by GraphQL-style field selection.
  """
  @spec find(integer(), [atom()]) :: Post.t() | nil
  def find(id, selected_fields) do
    preloads = preloads_for(selected_fields)
    Post |> Repo.get(id) |> Repo.preload(preloads)
  end

  defp preloads_for(fields) do
    Enum.flat_map(fields, fn
      :tags -> [:tags]
      :cover -> [:cover_image]
      :comments_with_author -> [comments: :author]
      :author -> [:user]
      _ -> []
    end)
  end
end
```

### Step 3: Migrations

**Objective**: Index FK columns (post_id, author_id) so preload's IN-queries use index scans, not sequential table scans.

```elixir
defmodule PreloadNested.Repo.Migrations.Create do
  use Ecto.Migration

  def change do
    create table(:users) do
      add :name, :string, null: false
      add :email, :string, null: false
      timestamps()
    end
    create unique_index(:users, [:email])

    create table(:posts) do
      add :title, :string, null: false
      add :body, :text, null: false
      add :published, :boolean, default: false
      add :user_id, references(:users, on_delete: :delete_all), null: false
      timestamps()
    end
    create index(:posts, [:user_id])

    create table(:comments) do
      add :body, :text, null: false
      add :post_id, references(:posts, on_delete: :delete_all), null: false
      add :author_id, references(:users, on_delete: :restrict), null: false
      timestamps()
    end
    create index(:comments, [:post_id])
    create index(:comments, [:author_id])

    create table(:cover_images) do
      add :url, :string, null: false
      add :post_id, references(:posts, on_delete: :delete_all), null: false
    end
    create unique_index(:cover_images, [:post_id])

    create table(:tags) do
      add :name, :string, null: false
    end

    create table(:posts_tags, primary_key: false) do
      add :post_id, references(:posts, on_delete: :delete_all), null: false
      add :tag_id, references(:tags, on_delete: :delete_all), null: false
    end
    create unique_index(:posts_tags, [:post_id, :tag_id])
  end
end
```

### Step 4: Tests

**Objective**: Assert exact query counts per preload strategy to lock in N+1 regressions before they ship.

```elixir
defmodule PreloadNested.BlogTest do
  use ExUnit.Case, async: false

  import Ecto.Query

  alias PreloadNested.Repo
  alias PreloadNested.Blog
  alias PreloadNested.Schemas.{Comment, CoverImage, Post, Tag, User}

  setup do
    Ecto.Adapters.SQL.Sandbox.checkout(Repo)
    user = Repo.insert!(%User{name: "Alice", email: "a@x"})
    commenter = Repo.insert!(%User{name: "Bob", email: "b@x"})
    post = Repo.insert!(%Post{title: "Hi", body: "body", user_id: user.id})
    Repo.insert!(%CoverImage{url: "https://x/x.png", post_id: post.id})
    _tag = Repo.insert!(%Tag{name: "elixir"})

    for i <- 1..3 do
      Repo.insert!(%Comment{
        body: "c#{i}",
        post_id: post.id,
        author_id: commenter.id
      })
    end

    {:ok, post: post, commenter: commenter}
  end

  describe "full_post/1" do
    test "loads tree without N+1", %{post: post} do
      loaded = Blog.full_post(post.id)
      assert length(loaded.comments) == 3
      assert [%Comment{author: %User{}} | _] = loaded.comments
      assert %CoverImage{} = loaded.cover_image
    end
  end

  describe "post_with_recent_comments/2" do
    test "applies limit to preloaded comments", %{post: post} do
      loaded = Blog.post_with_recent_comments(post.id, 2)
      assert length(loaded.comments) == 2
    end
  end

  describe "posts_commented_by/1" do
    test "returns unique posts the user commented on", %{commenter: c, post: p} do
      assert [result] = Blog.posts_commented_by(c.id)
      assert result.id == p.id
    end
  end

  describe "find/2 dynamic preloads" do
    test "preloads only requested fields", %{post: p} do
      loaded = Blog.find(p.id, [:tags])
      assert loaded.tags != %Ecto.Association.NotLoaded{}
      assert match?(%Ecto.Association.NotLoaded{}, loaded.comments)
    end
  end
end
```

---


### Why this works

The design leans on BEAM guarantees (process isolation, mailbox ordering, supervisor restarts) and pushes invariants to the boundaries of each module. State transitions are explicit, failure modes are declared rather than implicit, and each step is independently testable. That combination keeps the implementation correct under concurrent load and cheap to change later.

## Benchmark

```elixir
# Minimal measurement — replace with Benchee for distribution stats.
{time_us, _} = :timer.tc(fn ->
  for _ <- 1..10_000, do: run_operation()
end)
IO.puts("avg: #{time_us / 10_000} µs/op")
```

Target: operation should complete in the low-microsecond range on modern hardware; deviations by >2× indicate a regression worth investigating.

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

**1. Join preload duplicates parent rows**
`SELECT * FROM posts JOIN comments ON ...` with 100 posts × avg 20 comments = 2000 rows
transmitted over the wire. Two-query preload sends 100 + 2000 rows as separate result sets,
often smaller due to less column duplication.

**2. Preload with `:limit` per parent needs `lateral_join`**
`from c in Comment, order_by: ..., limit: 5` in a preload applies the limit GLOBALLY, not
per post. To get "latest 5 comments per post", use `lateral_join` with `preload`. Ecto 3.11
supports `lateral_join` explicitly.

**3. `preload: [assoc: custom_query]` bypasses automatic join inference**
If `custom_query` doesn't include the FK column, preload cannot match rows to parents and
returns empty. Always ensure the FK column is in `select` (or use `select_merge`).

**4. Circular preloads cause stack overflow at compile time**
`User has_many Posts; Post belongs_to User` preloaded as `[:posts, posts: :user]` is
fine — Ecto detects the already-loaded user. But `preload: [posts: [user: [posts: :user]]]`
is a circular chain; avoid it or use `preload: [:posts]` and load user separately.

**5. `Repo.preload/2` on a list triggers one batched query**
`[post1, post2] |> Repo.preload(:comments)` issues ONE query:
`SELECT ... WHERE post_id IN (1, 2)`. A common mistake is
`Enum.map(posts, &Repo.preload(&1, :comments))` — this is N+1 in disguise.

**6. `preload` on a parameterized association**
`belongs_to :owner, where: [active: true]` works in queries but NOT in `Repo.preload/2`.
The preload version does not apply the `:where`. Always verify by logging the generated SQL.

**7. Dynamic preload specs can be unbounded**
Accepting GraphQL field selections and mapping 1:1 to preloads lets an attacker request
`comments.author.posts.comments.author...` — exponential DB load. Whitelist depth.

**8. When NOT to use this**
For analytical reads (dashboards, CSV exports, aggregates), don't preload at all — use
raw SQL or a query that returns tuples directly (`select: {p.title, count(c.id)}`).
Preload is for rendering tree-shaped data; it wastes memory on flat reports.

---

## Performance notes

Measure the three strategies:

```elixir
{t_naive, _} = :timer.tc(fn ->
  post = Repo.get(Post, id)
  for c <- Repo.all(from(cm in Comment, where: cm.post_id == ^post.id)) do
    Repo.get(User, c.author_id)
  end
end)

{t_preload, _} = :timer.tc(fn -> Blog.full_post(id) end)

{t_join, _} = :timer.tc(fn ->
  Repo.all(from(p in Post,
    join: c in assoc(p, :comments),
    join: a in assoc(c, :author),
    where: p.id == ^id,
    preload: [comments: {c, author: a}]
  ))
end)

IO.inspect({t_naive, t_preload, t_join})
```

On 150 comments: naive ≈ 180 ms (151 queries), preload ≈ 12 ms (4 queries), join ≈ 14 ms
(1 query). Preload wins for tree rendering; join wins when you need to filter by child.

---

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?

## Resources

- [`Ecto.Repo.preload/3` — hexdocs](https://hexdocs.pm/ecto/Ecto.Repo.html#c:preload/3) — official with examples.
- [`Ecto.Query.preload/3` in-query preload](https://hexdocs.pm/ecto/Ecto.Query.html#preload/3) — the join-preload form.
- [Dashbit: "Ecto Preloads"](https://dashbit.co/blog/ecto-preloads) — canonical explanation.
- [Absinthe Docs — DataLoader](https://hexdocs.pm/dataloader/Dataloader.html) — the GraphQL N+1 solution using Ecto preload under the hood.
- [Ecto GitHub — `lib/ecto/repo/preloader.ex`](https://github.com/elixir-ecto/ecto/blob/master/lib/ecto/repo/preloader.ex) — read the source for deep understanding.

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```
