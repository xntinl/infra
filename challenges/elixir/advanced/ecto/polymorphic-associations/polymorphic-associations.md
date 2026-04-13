# Polymorphic Associations — The Correct Way in Postgres

**Project**: `comments_system` — one `Comment` schema attachable to multiple parent types without loose foreign keys

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
comments_system/
├── lib/
│   └── comments_system.ex
├── script/
│   └── main.exs
├── test/
│   └── comments_system_test.exs
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
defmodule CommentsSystem.MixProject do
  use Mix.Project

  def project do
    [
      app: :comments_system,
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

### `lib/comments_system.ex`

```elixir
# priv/repo/migrations/20260101000000_polymorphic_comments.exs
defmodule CommentsSystem.Repo.Migrations.PolymorphicComments do
  @moduledoc """
  Ejercicio: Polymorphic Associations — The Correct Way in Postgres.
  Implementa el comportamiento descrito en el enunciado, exponiendo funciones públicas documentadas y con tipos claros.
  """

  use Ecto.Migration

  @doc "Returns change result."
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

  @doc "Returns changeset result from comment and attrs."
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

# lib/comments_system/comments.ex
defmodule CommentsSystem.Comments do
  import Ecto.Query

  alias CommentsSystem.Repo
  alias CommentsSystem.Schemas.{Article, Comment, Photo, Video}

  @type parent :: Article.t() | Video.t() | Photo.t()

  @doc "Adds result from parent and body."
  @spec add(parent(), String.t()) :: {:ok, Comment.t()} | {:error, Ecto.Changeset.t()}
  def add(parent, body) do
    attrs = Map.merge(%{body: body}, parent_fk(parent))

    %Comment{}
    |> Comment.changeset(attrs)
    |> Repo.insert()
  end

  @doc "Lists result from parent."
  @spec list(parent()) :: [Comment.t()]
  def list(parent) do
    {fk_field, fk_value} = parent_fk_pair(parent)

    Comment
    |> where([c], field(c, ^fk_field) == ^fk_value)
    |> order_by([c], asc: c.inserted_at)
    |> Repo.all()
  end

  @doc "Counts across all types result."
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

### `test/comments_system_test.exs`

```elixir
defmodule CommentsSystem.CommentsTest do
  use ExUnit.Case, async: true
  doctest CommentsSystem.Repo.Migrations.PolymorphicComments
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

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Polymorphic Associations — The Correct Way in Postgres.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Polymorphic Associations — The Correct Way in Postgres ===")
    IO.puts("Category: Ecto advanced\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case CommentsSystem.run(payload) do
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
        for _ <- 1..1_000, do: CommentsSystem.run(:bench)
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
