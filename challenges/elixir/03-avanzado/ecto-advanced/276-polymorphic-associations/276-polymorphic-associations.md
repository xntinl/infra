# Polymorphic Associations — The Correct Way in Postgres

**Project**: `comments_system` — one `Comment` schema attachable to multiple parent types without loose foreign keys.

---

## Project context

`Comment` must attach to `Article`, `Video`, or `Photo`. Rails-style polymorphism stores
`(commentable_type, commentable_id)` without a foreign key — orphans are inevitable, and
no DB-level constraint catches a dangling reference. Ecto's docs explicitly warn against
this pattern.

This exercise builds three production-grade alternatives and shows when each fits:

1. **Join table per parent** — `article_comments`, `video_comments`, `photo_comments`.
2. **Single table with multiple nullable FKs** — one `article_id`, one `video_id`, one
   `photo_id`, exactly one non-null (enforced by CHECK).
3. **Abstract parent table** — `commentables` supertable, every concrete parent has an
   FK to `commentables`.

```
comments_system/
├── lib/
│   └── comments_system/
│       ├── application.ex
│       ├── repo.ex
│       ├── comments.ex
│       └── schemas/
│           ├── article.ex
│           ├── video.ex
│           ├── photo.ex
│           ├── comment.ex                # approach 2 (nullable FKs)
│           └── commentable.ex            # approach 3 (abstract)
├── priv/repo/migrations/
├── test/comments_system/
│   └── comments_test.exs
├── bench/comments_bench.exs
└── mix.exs
```

---

## Why Rails-style `(type, id)` is wrong

```sql
comments
  id | commentable_type | commentable_id
```

No FK. Deleting an `Article` leaves `comments` with dangling references until a sweeper
cleans them up. Queries to "all comments on X" require runtime type dispatch:

```elixir
def load_comments(%Article{id: id}), do: Repo.all(from c in Comment, where: c.commentable_type == "Article" and c.commentable_id == ^id)
```

and the type string is a source of typos. Postgres cannot enforce `commentable_type IN ('Article','Video','Photo')` against a string without a CHECK that drifts from the code.

Worse, a composite index on `(commentable_type, commentable_id)` does not let the planner
join back to the parent table — every comment load is a two-query dance.

---

## Core concepts

### 1. Approach A — join table per parent

```
articles ◀── article_comments ──▶ comments
videos   ◀── video_comments   ──▶ comments
photos   ◀── photo_comments   ──▶ comments
```

Two FKs per join, both NOT NULL, both CASCADE. Comment has no reference to a parent type.
Cleanest referential integrity, most tables.

### 2. Approach B — nullable FKs with CHECK

```sql
comments (
  id,
  article_id INT REFERENCES articles,
  video_id   INT REFERENCES videos,
  photo_id   INT REFERENCES photos,
  CHECK ((article_id IS NOT NULL)::int + (video_id IS NOT NULL)::int + (photo_id IS NOT NULL)::int = 1)
)
```

Exactly one FK is non-null. Adding a new parent type = new column + new CHECK clause
(migration pain, but bounded).

### 3. Approach C — abstract parent

```sql
commentables (id PK)
articles   (id PK, commentable_id FK UNIQUE)
videos     (id PK, commentable_id FK UNIQUE)
comments   (id, commentable_id FK)
```

Every comment points to `commentables.id`. Every concrete parent has a 1:1 link to a
`commentables` row (created in the same transaction as the parent). Elegant, but adds a
join for every read.

### 4. Picking between them

| Criterion | A (join table) | B (nullable FKs) | C (abstract) |
|-----------|----------------|-------------------|--------------|
| Adding a new parent type | new table | new column + CHECK | zero schema change |
| Loading "all comments on X" | single join | WHERE on X_id | two joins |
| "All comments across parents" | UNION over join tables | single table scan | single table scan |
| Referential integrity | strong | strong | strong |
| Orphan risk | 0 | 0 | 0 |
| Query complexity for aggregates | high | low | medium |

---

## Design decisions

- **Option A — per-parent join tables**: ideal when comments are read almost always in
  the context of one parent type. Pros: simple FKs. Cons: cross-type listing needs UNION.
- **Option B — nullable FKs with CHECK**: ideal when parent count is small (<10) and stable.
  Pros: one table, easy aggregates. Cons: migration needed for each new type.
- **Option C — abstract parent table**: ideal when new parent types arrive frequently.
  Pros: zero schema change per type. Cons: extra join, 1:1 row management.

We implement **Option B** as the default and show A and C as documented variants in
`Comments.all_approaches_docs/0`.

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

### Step 1: Migration with CHECK constraint

```elixir
# priv/repo/migrations/20260101000000_polymorphic_comments.exs
defmodule CommentsSystem.Repo.Migrations.PolymorphicComments do
  use Ecto.Migration

  def change do
    create table(:articles) do
      add :title, :string, null: false
      timestamps()
    end

    create table(:videos) do
      add :url, :string, null: false
      timestamps()
    end

    create table(:photos) do
      add :caption, :string
      timestamps()
    end

    create table(:comments) do
      add :body, :text, null: false
      add :article_id, references(:articles, on_delete: :delete_all)
      add :video_id, references(:videos, on_delete: :delete_all)
      add :photo_id, references(:photos, on_delete: :delete_all)
      timestamps()
    end

    create constraint(
             :comments,
             :exactly_one_parent,
             check:
               "(article_id IS NOT NULL)::int + (video_id IS NOT NULL)::int + (photo_id IS NOT NULL)::int = 1"
           )

    create index(:comments, [:article_id], where: "article_id IS NOT NULL")
    create index(:comments, [:video_id], where: "video_id IS NOT NULL")
    create index(:comments, [:photo_id], where: "photo_id IS NOT NULL")
  end
end
```

Partial indexes (`WHERE article_id IS NOT NULL`) keep index size proportional to actual
comments of that parent type, not the whole table.

### Step 2: Schemas

```elixir
# lib/comments_system/schemas/article.ex
defmodule CommentsSystem.Schemas.Article do
  use Ecto.Schema

  schema "articles" do
    field :title, :string
    has_many :comments, CommentsSystem.Schemas.Comment
    timestamps()
  end
end

# lib/comments_system/schemas/video.ex
defmodule CommentsSystem.Schemas.Video do
  use Ecto.Schema

  schema "videos" do
    field :url, :string
    has_many :comments, CommentsSystem.Schemas.Comment
    timestamps()
  end
end

# lib/comments_system/schemas/photo.ex
defmodule CommentsSystem.Schemas.Photo do
  use Ecto.Schema

  schema "photos" do
    field :caption, :string
    has_many :comments, CommentsSystem.Schemas.Comment
    timestamps()
  end
end

# lib/comments_system/schemas/comment.ex
defmodule CommentsSystem.Schemas.Comment do
  use Ecto.Schema
  import Ecto.Changeset

  alias CommentsSystem.Schemas.{Article, Photo, Video}

  schema "comments" do
    field :body, :string
    belongs_to :article, Article
    belongs_to :video, Video
    belongs_to :photo, Photo
    timestamps()
  end

  def changeset(comment, attrs) do
    comment
    |> cast(attrs, [:body, :article_id, :video_id, :photo_id])
    |> validate_required([:body])
    |> validate_exactly_one_parent()
    |> check_constraint(:base,
      name: :exactly_one_parent,
      message: "exactly one of article/video/photo must be set"
    )
  end

  # App-level fast path so users see a nice error before the DB CHECK triggers.
  defp validate_exactly_one_parent(changeset) do
    fks = [:article_id, :video_id, :photo_id]

    set =
      fks
      |> Enum.map(&get_field(changeset, &1))
      |> Enum.count(&(not is_nil(&1)))

    case set do
      1 -> changeset
      0 -> add_error(changeset, :base, "must attach to a parent (article/video/photo)")
      _ -> add_error(changeset, :base, "cannot attach to multiple parents")
    end
  end
end
```

### Step 3: Context — polymorphic API

```elixir
# lib/comments_system/comments.ex
defmodule CommentsSystem.Comments do
  import Ecto.Query

  alias CommentsSystem.Repo
  alias CommentsSystem.Schemas.{Article, Comment, Photo, Video}

  @type parent :: Article.t() | Video.t() | Photo.t()

  @spec add(parent(), String.t()) :: {:ok, Comment.t()} | {:error, Ecto.Changeset.t()}
  def add(parent, body) do
    attrs = Map.merge(%{body: body}, parent_fk(parent))

    %Comment{}
    |> Comment.changeset(attrs)
    |> Repo.insert()
  end

  @spec list(parent()) :: [Comment.t()]
  def list(parent) do
    {fk_field, fk_value} = parent_fk_pair(parent)

    Comment
    |> where([c], field(c, ^fk_field) == ^fk_value)
    |> order_by([c], asc: c.inserted_at)
    |> Repo.all()
  end

  @spec count_across_all_types() :: %{atom() => non_neg_integer()}
  def count_across_all_types do
    query =
      from c in Comment,
        select: %{
          articles: count() |> filter(not is_nil(c.article_id)),
          videos: count() |> filter(not is_nil(c.video_id)),
          photos: count() |> filter(not is_nil(c.photo_id))
        }

    Repo.one(query)
  end

  # ------------------------------------------------------------------------

  defp parent_fk(%Article{id: id}), do: %{article_id: id}
  defp parent_fk(%Video{id: id}), do: %{video_id: id}
  defp parent_fk(%Photo{id: id}), do: %{photo_id: id}

  defp parent_fk_pair(%Article{id: id}), do: {:article_id, id}
  defp parent_fk_pair(%Video{id: id}), do: {:video_id, id}
  defp parent_fk_pair(%Photo{id: id}), do: {:photo_id, id}
end
```

---

## Why this works

- The CHECK constraint guarantees exactly one parent at the DB layer, so a misbehaving
  client or a forgotten changeset validation still cannot write garbage.
- Every FK is a real `REFERENCES` with `ON DELETE CASCADE`. Deleting an article removes
  its comments automatically.
- Partial indexes mean a query `WHERE article_id = 42` uses an index that contains only
  comments with a non-null `article_id` — smaller, faster.
- `count_across_all_types/0` uses `filter()` aggregates (Postgres FILTER clause) to count
  per type in a single scan, no UNION.

---

## Data flow

```
Comments.add(%Article{id: 1}, "great post")
    │
    ▼  parent_fk/1  ─▶ %{article_id: 1}
    │
    ▼  Comment.changeset
    │    validate_exactly_one_parent   (app-level fast fail)
    │    check_constraint              (DB-level guard)
    │
    ▼  Repo.insert
    │
    ▼  INSERT ... (comments)
         │
         ▼  CHECK evaluated by Postgres
         │
         ▼  COMMIT
```

---

## Tests

```elixir
# test/comments_system/comments_test.exs
defmodule CommentsSystem.CommentsTest do
  use ExUnit.Case, async: false
  alias CommentsSystem.{Comments, Repo}
  alias CommentsSystem.Schemas.{Article, Comment, Photo, Video}

  setup do
    Ecto.Adapters.SQL.Sandbox.checkout(Repo)
    Ecto.Adapters.SQL.Sandbox.mode(Repo, {:shared, self()})

    {:ok, article} = Repo.insert(%Article{title: "Hello"})
    {:ok, video} = Repo.insert(%Video{url: "https://v"})
    {:ok, photo} = Repo.insert(%Photo{caption: "sunset"})

    {:ok, article: article, video: video, photo: photo}
  end

  describe "add/2" do
    test "attaches to an article", %{article: a} do
      assert {:ok, c} = Comments.add(a, "nice")
      assert c.article_id == a.id
      assert c.video_id == nil
    end

    test "attaches to a video", %{video: v} do
      {:ok, c} = Comments.add(v, "cool")
      assert c.video_id == v.id
    end
  end

  describe "list/1" do
    test "returns only comments of the given parent", %{article: a, video: v} do
      {:ok, _} = Comments.add(a, "on article")
      {:ok, _} = Comments.add(v, "on video")

      assert [c] = Comments.list(a)
      assert c.body == "on article"
    end
  end

  describe "integrity" do
    test "deleting a parent cascades its comments", %{article: a} do
      {:ok, _} = Comments.add(a, "x")
      Repo.delete!(a)
      assert Repo.aggregate(Comment, :count) == 0
    end

    test "attaching to zero parents is rejected by the changeset" do
      cs = Comment.changeset(%Comment{}, %{body: "orphan"})
      refute cs.valid?
    end

    test "attaching to two parents is rejected by the changeset", %{article: a, video: v} do
      cs =
        Comment.changeset(%Comment{}, %{body: "double", article_id: a.id, video_id: v.id})

      refute cs.valid?
    end

    test "DB CHECK catches a direct SQL write bypassing changeset" do
      assert_raise Postgrex.Error, ~r/exactly_one_parent/, fn ->
        Ecto.Adapters.SQL.query!(
          Repo,
          "INSERT INTO comments (body, inserted_at, updated_at) VALUES ('x', now(), now())",
          []
        )
      end
    end
  end

  describe "cross-type aggregate" do
    test "single-scan count by type", %{article: a, video: v, photo: p} do
      {:ok, _} = Comments.add(a, "1")
      {:ok, _} = Comments.add(a, "2")
      {:ok, _} = Comments.add(v, "3")
      {:ok, _} = Comments.add(p, "4")

      assert %{articles: 2, videos: 1, photos: 1} = Comments.count_across_all_types()
    end
  end
end
```

---

## Benchmark

```elixir
# bench/comments_bench.exs
alias CommentsSystem.{Comments, Repo}
alias CommentsSystem.Schemas.{Article, Video}

{:ok, a} = Repo.insert(%Article{title: "bench"})

for _ <- 1..10_000, do: Comments.add(a, "c")

Benchee.run(
  %{
    "list/1 by article"   => fn -> Comments.list(a) end,
    "count by type"       => fn -> Comments.count_across_all_types() end
  },
  time: 3, warmup: 1
)
```

**Target**: `list/1` under 5 ms for 10k comments (hits partial index). `count_across_all_types`
is O(table size) — acceptable for <1M rows, move to a materialized view beyond that.

---

## Trade-offs and production gotchas

**1. Adding a new parent type is a migration and a CHECK rewrite.** In Postgres, a new
column with an updated CHECK constraint requires `ALTER TABLE ... DROP CONSTRAINT + ADD
CONSTRAINT NOT VALID + VALIDATE CONSTRAINT` to avoid locking. Plan downtime or dual-write.

**2. Every query still needs the right FK.** `list/1` dispatches on struct type — if a
new type is added, the pattern match must be extended. Compile warnings help (strict
pattern matching); do not use wildcard catch-alls.

**3. `Comment` cannot have a clean `belongs_to :commentable`.** Downstream code must
resolve the parent by inspecting which FK is set. Provide a helper:
`Comment.parent_for/1` that returns `{Article, id}` etc.

**4. Index bloat if comments are skewed.** If 99% of comments are on articles, the partial
indexes on `video_id` and `photo_id` are small and harmless — but an analytics query that
filters by creation time without FK uses the main heap. Add `(inserted_at)` as a global
index.

**5. Rails-style string `commentable_type` is still wrong.** Do not soft-migrate to
"generic commentable" by adding a string column later; you reintroduce every problem this
design avoided.

**6. When NOT to use Option B.** Past ~10 parent types, the table widens, CHECK becomes
unwieldy, and Option C (abstract parent) starts to pay off.

---

## Reflection

You have 5 parent types today and expect 20 more next year as the product grows.
Rewriting from Option B to Option C requires a zero-downtime migration: the `comments`
table must gain a `commentable_id` FK and lose its per-type FKs, without blocking writes.
Sketch the migration in phases (add column, backfill, dual-write, cutover reads, drop
old columns) and identify the exact moment the CHECK constraint is disabled — what
invariant protects the data during that window?

---

## Resources

- [Ecto — "Polymorphic associations with many_to_many"](https://hexdocs.pm/ecto/polymorphic-associations-with-many-to-many.html)
- [Postgres — CHECK constraints](https://www.postgresql.org/docs/current/ddl-constraints.html)
- [José Valim — "Rails polymorphic associations"](https://dashbit.co/blog) — why Ecto rejects them
- [Partial indexes](https://www.postgresql.org/docs/current/indexes-partial.html)
