# Full-Text Search with `tsvector`

**Project**: `docs_search` — Postgres full-text search integrated with Ecto.

---

## Project context

A knowledge base has 50k articles. Users type "how to deploy phoenix cluster" and expect
relevance-ranked results under 100 ms. Options: Elasticsearch (new service, ops overhead),
Meilisearch (same), or Postgres `tsvector` + GIN (already running). For <1M documents
with moderate query complexity, Postgres FTS is the pragmatic choice.

This exercise builds a search module with ranking, highlighting, and phrase search.

```
docs_search/
├── lib/
│   └── docs_search/
│       ├── application.ex
│       ├── repo.ex
│       ├── search.ex
│       └── schemas/
│           └── article.ex
├── priv/repo/migrations/
├── test/docs_search/
│   └── search_test.exs
├── bench/search_bench.exs
└── mix.exs
```

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
### 1. `tsvector` — the indexed form of a document

Postgres converts a text string into a normalized `tsvector`:

```sql
SELECT to_tsvector('english', 'How to deploy phoenix clusters');
-- 'cluster':5 'deploy':3 'phoenix':4
```

Stop words are dropped, words are lemmatized, positions are stored.

### 2. `tsquery` — the parsed form of a search

```sql
SELECT to_tsquery('english', 'phoenix & deploy');
SELECT plainto_tsquery('english', 'deploy phoenix cluster');  -- auto-AND
SELECT phraseto_tsquery('english', 'deploy phoenix');         -- adjacent
SELECT websearch_to_tsquery('english', '"deploy phoenix" OR elixir -rails');  -- Google-like
```

Use `websearch_to_tsquery` for user-facing input — it handles quotes, OR, negation.

### 3. Match and rank

```sql
WHERE search_vector @@ websearch_to_tsquery('english', $1)
ORDER BY ts_rank(search_vector, websearch_to_tsquery('english', $1)) DESC
```

`@@` tests match, `ts_rank` scores relevance (term frequency × document length).

### 4. Generated column + GIN index

Best practice: store `tsvector` as a generated column, index it with GIN.

```sql
ALTER TABLE articles ADD COLUMN search_vector tsvector
  GENERATED ALWAYS AS (
    setweight(to_tsvector('english', coalesce(title, '')), 'A') ||
    setweight(to_tsvector('english', coalesce(body, '')), 'B')
  ) STORED;

CREATE INDEX articles_search_idx ON articles USING GIN (search_vector);
```

`setweight` lets you boost matches in the title over body. GIN is designed for inverted
indexes; searches are O(1) in the number of distinct terms.

### 5. Highlighting

```sql
ts_headline('english', body, query, 'StartSel=<mark>, StopSel=</mark>, MaxWords=30')
```

Returns a snippet with matched terms wrapped in markup.

---

## Design decisions

- **Option A — compute `tsvector` in Elixir and insert it**. Pros: explicit.
  Cons: every update must remember to recompute.
- **Option B — generated column**: Postgres computes it automatically.
  Pros: zero maintenance, cannot forget. Cons: Postgres 12+.

We use **Option B**. Schema simply declares the column as read-only.

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

### Step 1: Migration

**Objective**: Build a weighted generated tsvector column and a GIN index so search stays consistent with source text on every write.

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
```

### Step 2: Schema

**Objective**: Expose `search_vector` read-only plus virtual `rank`/`headline` so selects hydrate ranked articles without polluting changesets.

```elixir
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
```

### Step 3: Search module

**Objective**: Drive websearch_to_tsquery with ts_rank and ts_headline so users get boolean/phrase syntax, ranking, and snippet highlighting in one query.

```elixir
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

---

## Why this works

- The generated `search_vector` column is populated by Postgres on every INSERT/UPDATE.
  App code never touches it — impossible to forget.
- `setweight` with `'A'`/`'B'`/`'C'` lets `ts_rank` weight title matches 4× over body
  matches (approximate; the factors come from `{0.1, 0.2, 0.4, 1.0}` by default).
- The GIN index turns `@@` from O(N) into O(log N) for the relevant terms. For 50k
  articles, searches are sub-10 ms.
- `websearch_to_tsquery` is the parser you want for user input. It tolerates malformed
  input (quotes without close, stray operators) that would make `to_tsquery` crash.
- `read_after_writes: true` makes Ecto re-select the `search_vector` after insert, so the
  returned struct reflects the generated value.

---

## Data flow

```
User query: "deploy phoenix cluster -docker"
        │
        ▼
websearch_to_tsquery('english', ...)
   → 'deploy' & 'phoenix' & 'cluster' & !'docker'
        │
        ▼
WHERE search_vector @@ tsquery       -- GIN index lookup
        │
        ▼
ts_rank(search_vector, tsquery)       -- score each hit
        │
        ▼
ts_headline(body, tsquery)            -- snippet with <mark>
        │
        ▼
ORDER BY rank DESC LIMIT 20
```

---

## Tests

```elixir
# test/docs_search/search_test.exs
defmodule DocsSearch.SearchTest do
  use ExUnit.Case, async: false
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

---

## Benchmark

```elixir
# bench/search_bench.exs
alias DocsSearch.{Repo, Search}
alias DocsSearch.Schemas.Article

Repo.delete_all(Article)

for i <- 1..50_000 do
  Repo.insert!(%Article{
    title: "Article #{i} about #{Enum.random(~w(elixir phoenix ecto otp ets))}",
    body: "Lorem ipsum #{i} #{Enum.random(~w(kubernetes distributed clustering database phoenix))} dolor sit amet."
  })
end

Benchee.run(
  %{
    "simple search"     => fn -> Search.run("phoenix") end,
    "phrase search"     => fn -> Search.run(~s("phoenix cluster")) end,
    "negated search"    => fn -> Search.run("phoenix -kubernetes") end
  },
  time: 5, warmup: 2
)
```

**Target**: under 30 ms for 50k articles with GIN index. If you see > 200 ms, the index
is missing or `EXPLAIN` shows a sequential scan.

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

**1. Language config is sticky.** Changing `to_tsvector('english', ...)` to
`'spanish'` requires regenerating every row. Multilingual apps need a column tagged
with the document's language.

**2. Stop words vary by dictionary.** "phoenix" might be a stop word in some dictionaries.
Verify with `SELECT to_tsvector('english', 'phoenix');`.

**3. Updates recompute the tsvector.** For very frequently updated docs, the write
amplification is non-trivial. Measure `pg_stat_statements` after rollout.

**4. GIN index build is slow on large tables.** Creating the GIN index on a 50k row
table is ~5 seconds. On 50M rows it can be 30+ minutes. Use `CREATE INDEX CONCURRENTLY`.

**5. `ts_headline` is expensive on long documents.** It re-parses the body for every
result. For large results, omit it or compute it only for the top 10 after an initial
pass.

**6. When NOT to use Postgres FTS.** For > 10M docs, multilingual analysis,
faceting, or fuzzy matching at scale, move to Elasticsearch or Meilisearch.

---

## Reflection

Your FTS works great for 50k articles. The business expands to Japan; users expect to
search in Japanese. `to_tsvector('japanese', ...)` exists only with the `zhparser` or
`mecab` extensions, neither in the default Postgres build. Describe your options:
(1) ship a custom Postgres image with extensions, (2) switch FTS to Elasticsearch for
everything, (3) run two search paths per language. Which is cheapest to operate at 10k
QPS across 5 languages?

---


## Executable Example

```elixir
# test/docs_search/search_test.exs
defmodule DocsSearch.SearchTest do
  use ExUnit.Case, async: false
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

defmodule Main do
  def main do
    IO.puts("✓ Full-Text Search with `tsvector`")
  - Full-text search with tsvector
    - Ranking and relevance scoring
  end
end

Main.main()
```
