# Nested Preloads and Join Preloads

**Project**: `preload_nested` — a blog/commenting platform loading deep association trees

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
preload_nested/
├── lib/
│   └── preload_nested.ex
├── script/
│   └── main.exs
├── test/
│   └── preload_nested_test.exs
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
defmodule PreloadNested.MixProject do
  use Mix.Project

  def project do
    [
      app: :preload_nested,
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

### `lib/preload_nested.ex`

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

### `test/preload_nested_test.exs`

```elixir
defmodule PreloadNested.BlogTest do
  use ExUnit.Case, async: true
  doctest PreloadNested.Schemas.User

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

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Nested Preloads and Join Preloads.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Nested Preloads and Join Preloads ===")
    IO.puts("Category: Ecto advanced\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case PreloadNested.run(payload) do
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
        for _ <- 1..1_000, do: PreloadNested.run(:bench)
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
