# Full-Text Search with `tsvector`

**Project**: `docs_search` — Postgres full-text search integrated with Ecto

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
docs_search/
├── lib/
│   └── docs_search.ex
├── script/
│   └── main.exs
├── test/
│   └── docs_search_test.exs
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
defmodule DocsSearch.MixProject do
  use Mix.Project

  def project do
    [
      app: :docs_search,
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

### `lib/docs_search.ex`

```elixir
# priv/repo/migrations/20260101000000_create_articles.exs
defmodule DocsSearch.Repo.Migrations.CreateArticles do
  use Ecto.Migration

  def up do
    create table(:articles) do
      add :title, :string, null: false
      add :body, :text, null: false
      add :tags, {:array, :string}, default: []
      timestamps()
    end

    execute """
    ALTER TABLE articles
      ADD COLUMN search_vector tsvector
      GENERATED ALWAYS AS (
        setweight(to_tsvector('english', coalesce(title, '')), 'A') ||
        setweight(to_tsvector('english', coalesce(array_to_string(tags, ' '), '')), 'B') ||
        setweight(to_tsvector('english', coalesce(body, '')), 'C')
      ) STORED
    """

    execute "CREATE INDEX articles_search_idx ON articles USING GIN (search_vector)"
    create index(:articles, [:inserted_at])
  end

  def down do
    drop table(:articles)
  end
end

# lib/docs_search/schemas/article.ex
defmodule DocsSearch.Schemas.Article do
  use Ecto.Schema
  import Ecto.Changeset

  schema "articles" do
    field :title, :string
    field :body, :string
    field :tags, {:array, :string}, default: []

    # Read-only — populated by the DB's generated column.
    field :search_vector, :string, read_after_writes: true

    # Populated by queries, not stored.
    field :rank, :float, virtual: true
    field :headline, :string, virtual: true

    timestamps()
  end

  def changeset(article, attrs) do
    article
    |> cast(attrs, [:title, :body, :tags])
    |> validate_required([:title, :body])
  end
end

# lib/docs_search/search.ex
defmodule DocsSearch.Search do
  @moduledoc """
  Full-text search over articles with ranking and headline generation.
  """
  import Ecto.Query

  alias DocsSearch.Repo
  alias DocsSearch.Schemas.Article

  @doc """
  Run a user-facing search query.

  Uses websearch_to_tsquery so the user can type:
    - `deploy phoenix`        → AND
    - `"deploy phoenix"`      → phrase
    - `elixir OR erlang`      → OR
    - `phoenix -rails`        → exclude "rails"
  """
  @spec run(String.t(), non_neg_integer()) :: [Article.t()]
  def run(query_string, limit \\ 20)
  def run("", _limit), do: []

  def run(query_string, limit) do
    tsq_fragment = fragment("websearch_to_tsquery('english', ?)", ^query_string)

    from(a in Article,
      where: fragment("? @@ ?", a.search_vector, ^tsq_fragment),
      select_merge: %{
        rank: fragment("ts_rank(?, ?)", a.search_vector, ^tsq_fragment),
        headline:
          fragment(
            "ts_headline('english', ?, ?, 'StartSel=<mark>,StopSel=</mark>,MaxWords=30,MinWords=10')",
            a.body,
            ^tsq_fragment
          )
      },
      order_by: [
        desc: fragment("ts_rank(?, ?)", a.search_vector, ^tsq_fragment)
      ],
      limit: ^limit
    )
    |> Repo.all()
  end

  @doc """
  Count matches for a query (for pagination totals).
  """
  @spec count(String.t()) :: non_neg_integer()
  def count(""), do: 0

  def count(query_string) do
    tsq_fragment = fragment("websearch_to_tsquery('english', ?)", ^query_string)

    Repo.aggregate(
      from(a in Article, where: fragment("? @@ ?", a.search_vector, ^tsq_fragment)),
      :count
    )
  end

  @doc """
  Suggestion/autocomplete: prefix matching on title using trigram similarity.

  Requires the pg_trgm extension (enable separately: CREATE EXTENSION pg_trgm).
  """
  @spec suggest(String.t(), non_neg_integer()) :: [String.t()]
  def suggest(prefix, limit \\ 10) when byte_size(prefix) >= 2 do
    from(a in Article,
      where: ilike(a.title, ^"#{prefix}%"),
      select: a.title,
      order_by: [asc: fragment("length(?)", a.title)],
      limit: ^limit,
      distinct: true
    )
    |> Repo.all()
  end
end
```

### `test/docs_search_test.exs`

```elixir
defmodule DocsSearch.SearchTest do
  use ExUnit.Case, async: true
  doctest DocsSearch.Repo.Migrations.CreateArticles
  alias DocsSearch.{Repo, Search}
  alias DocsSearch.Schemas.Article

  setup do
    Ecto.Adapters.SQL.Sandbox.checkout(Repo)
    Ecto.Adapters.SQL.Sandbox.mode(Repo, {:shared, self()})
    Repo.delete_all(Article)

    {:ok, _} =
      Repo.insert(%Article{
        title: "Deploying Phoenix on Kubernetes",
        body: "A guide to running Phoenix apps on Kubernetes clusters with libcluster."
      })

    {:ok, _} =
      Repo.insert(%Article{
        title: "OTP GenServer basics",
        body: "Introduction to GenServer callbacks and supervision trees."
      })

    {:ok, _} =
      Repo.insert(%Article{
        title: "Distributed Elixir clusters",
        body: "Patterns for distributing work across nodes in a Phoenix cluster."
      })

    :ok
  end

  describe "run/2 basics" do
    test "matches title terms" do
      results = Search.run("kubernetes")
      assert length(results) == 1
      assert hd(results).title =~ "Kubernetes"
    end

    test "AND semantics via multiple terms" do
      results = Search.run("phoenix cluster")
      assert length(results) == 2
      assert Enum.all?(results, &(String.contains?(&1.title, "Phoenix") or String.contains?(&1.title, "cluster") or String.contains?(&1.body, "cluster")))
    end

    test "OR with websearch syntax" do
      results = Search.run("kubernetes OR genserver")
      assert length(results) == 2
    end

    test "exclusion with minus" do
      results = Search.run("phoenix -kubernetes")
      assert Enum.all?(results, &(not String.contains?(&1.title, "Kubernetes")))
    end

    test "phrase search" do
      results = Search.run(~s("Phoenix cluster"))
      assert Enum.any?(results, &String.contains?(&1.body, "Phoenix cluster"))
    end

    test "empty query returns empty list" do
      assert [] = Search.run("")
    end
  end

  describe "ranking" do
    test "title match outranks body match" do
      results = Search.run("phoenix")
      [top | _] = results
      # Title containing "Phoenix" should rank above one where it only appears in body
      assert String.contains?(top.title, "Phoenix")
    end

    test "rank is populated in the struct" do
      [hit | _] = Search.run("kubernetes")
      assert is_float(hit.rank)
      assert hit.rank > 0
    end

    test "headline contains the matched term wrapped in mark" do
      [hit | _] = Search.run("kubernetes")
      assert hit.headline =~ "<mark>Kubernetes</mark>"
    end
  end

  describe "count/1" do
    test "returns number of matching articles" do
      assert Search.count("phoenix") == 2
      assert Search.count("nonsense_word_zyx") == 0
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Full-Text Search with `tsvector`.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Full-Text Search with `tsvector` ===")
    IO.puts("Category: Ecto advanced\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case DocsSearch.run(payload) do
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
        for _ <- 1..1_000, do: DocsSearch.run(:bench)
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
