# Window Functions and `select_merge`

**Project**: `leaderboard` — per-user rank and rolling stats using Postgres window functions.

---

## Project context

A gaming platform ranks players weekly. Each player has a row per match. The API must
return, for a user, their rank this week, their percentile, and their 7-day rolling
average score — computed in one query so latency stays under 20 ms at the p99.

Naive approaches:

```elixir
scores = Repo.all(...)                         # pull all rows
ranked = Enum.with_index(Enum.sort(scores))    # rank in Elixir
```

This transfers every row over the wire and re-sorts in-memory. For 500k rows per week,
it is seconds of CPU.

Window functions push rank, percentile, and moving averages into Postgres, where the
planner can index-scan and use incremental aggregation.

```
leaderboard/
├── lib/
│   └── leaderboard/
│       ├── application.ex
│       ├── repo.ex
│       ├── stats.ex
│       └── schemas/
│           └── score.ex
├── priv/repo/migrations/
├── test/leaderboard/
│   └── stats_test.exs
├── bench/stats_bench.exs
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
### 1. Window functions compute per-row values without GROUP BY collapsing rows

```sql
SELECT
  user_id,
  score,
  rank()        OVER (ORDER BY score DESC)      AS global_rank,
  percent_rank() OVER (ORDER BY score DESC)     AS pct,
  avg(score)    OVER (
    PARTITION BY user_id
    ORDER BY played_at
    ROWS BETWEEN 6 PRECEDING AND CURRENT ROW
  ) AS rolling_avg
FROM scores
```

- `rank()` assigns 1 to the highest; ties share a rank and skip the next.
- `dense_rank()` is like rank but does not skip.
- `row_number()` ignores ties; always sequential.
- `percent_rank()` returns (rank-1) / (N-1) in [0, 1].
- `avg(...) OVER (... ROWS BETWEEN ...)` is a sliding window aggregate.

### 2. Ecto exposes windows via `windows:` and `over:`

```elixir
from s in Score,
  windows: [by_user: [partition_by: s.user_id, order_by: s.played_at]],
  select: %{
    user_id: s.user_id,
    rank: rank() |> over(order_by: [desc: s.score]),
    rolling_avg: avg(s.score) |> over(:by_user)
  }
```

The named window `:by_user` is declared once and referenced by name. For frame clauses
(`ROWS BETWEEN ...`), Ecto exposes `frame: fragment("ROWS BETWEEN 6 PRECEDING AND CURRENT ROW")`.

### 3. `select_merge` adds fields to a struct select

```elixir
from s in Score, select: s, select_merge: %{rank: rank() |> over(...)}
```

The result is a `%Score{}` with a virtual `:rank` field (declared on the schema) populated
from the window. This preserves schema ergonomics while adding computed columns.

---

## Design decisions

- **Option A — compute in Elixir after `Repo.all`**. Pros: simple SQL. Cons: full table
  scan and transfer; O(N) CPU on app node.
- **Option B — window functions in SQL**. Pros: pushes work to Postgres, which uses index
  scans. Cons: more complex query, must understand frame clauses.

We use **Option B** with `select_merge` to retain `%Score{}` structs populated with
computed fields.

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

### Step 1: Schema with virtual fields

**Objective**: Declare rank/pct/rolling_avg as virtual so window-function output rides the struct without a migration.

```elixir
# lib/leaderboard/schemas/score.ex
defmodule Leaderboard.Schemas.Score do
  use Ecto.Schema

  schema "scores" do
    field :user_id, :string
    field :score, :integer
    field :played_at, :utc_datetime

    # populated by window functions — not in the table
    field :rank, :integer, virtual: true
    field :pct, :float, virtual: true
    field :rolling_avg, :float, virtual: true

    timestamps(updated_at: false)
  end
end
```

### Step 2: Migration with supporting indexes

**Objective**: Index (user_id, played_at) and score so the window-function planner avoids sorts on leaderboard reads.

```elixir
# priv/repo/migrations/20260101000000_create_scores.exs
defmodule Leaderboard.Repo.Migrations.CreateScores do
  use Ecto.Migration

  def change do
    create table(:scores) do
      add :user_id, :string, null: false
      add :score, :integer, null: false
      add :played_at, :utc_datetime, null: false
      timestamps(updated_at: false)
    end

    create index(:scores, [:played_at])
    create index(:scores, [:user_id, :played_at])
    create index(:scores, [:score])
  end
end
```

### Step 3: Stats module

**Objective**: Compose rank, percent_rank, and rolling averages via select_merge so one SQL round trip returns analytics-ready rows.

```elixir
# lib/leaderboard/stats.ex
defmodule Leaderboard.Stats do
  @moduledoc """
  Leaderboard queries using Postgres window functions.
  """
  import Ecto.Query

  alias Leaderboard.Repo
  alias Leaderboard.Schemas.Score

  @doc """
  Global leaderboard within a time window. Returns scores sorted by rank asc.

  Each row carries `:rank` (1 = top) and `:pct` (0.0 = top, 1.0 = bottom).
  """
  @spec leaderboard(DateTime.t(), DateTime.t(), non_neg_integer()) :: [Score.t()]
  def leaderboard(from_ts, to_ts, limit \\ 100) do
    query =
      from s in Score,
        where: s.played_at >= ^from_ts and s.played_at < ^to_ts,
        windows: [global: [order_by: [desc: s.score]]],
        select: s,
        select_merge: %{
          rank: rank() |> over(:global),
          pct: percent_rank() |> over(:global)
        },
        order_by: [desc: s.score],
        limit: ^limit

    Repo.all(query)
  end

  @doc """
  Per-user history with a 7-row rolling average.

  Uses a frame clause `ROWS BETWEEN 6 PRECEDING AND CURRENT ROW` so each row's
  `rolling_avg` is the mean of the last 7 scores (including itself).
  """
  @spec user_history(String.t(), non_neg_integer()) :: [Score.t()]
  def user_history(user_id, limit \\ 50) do
    query =
      from s in Score,
        where: s.user_id == ^user_id,
        order_by: [asc: s.played_at],
        select: s,
        select_merge: %{
          rolling_avg:
            fragment(
              "AVG(?) OVER (ORDER BY ? ROWS BETWEEN 6 PRECEDING AND CURRENT ROW)",
              s.score,
              s.played_at
            )
        },
        limit: ^limit

    Repo.all(query)
  end

  @doc """
  Rank of one user within a window — single-row response.

  Computed as a subquery so we do not download the whole leaderboard.
  """
  @spec user_rank(String.t(), DateTime.t(), DateTime.t()) ::
          %{score: integer(), rank: integer(), pct: float()} | nil
  def user_rank(user_id, from_ts, to_ts) do
    ranked =
      from s in Score,
        where: s.played_at >= ^from_ts and s.played_at < ^to_ts,
        windows: [g: [order_by: [desc: s.score]]],
        select: %{
          user_id: s.user_id,
          score: s.score,
          rank: rank() |> over(:g),
          pct: percent_rank() |> over(:g)
        }

    from(r in subquery(ranked),
      where: r.user_id == ^user_id,
      order_by: [asc: r.rank],
      limit: 1,
      select: %{score: r.score, rank: r.rank, pct: r.pct}
    )
    |> Repo.one()
  end

  @doc """
  Top N per user (keeps personal bests).

  Uses `row_number()` partitioned by user to tag rows, then keeps only those with
  rn <= k. This is a classic top-N-per-group pattern.
  """
  @spec top_n_per_user(non_neg_integer()) :: [Score.t()]
  def top_n_per_user(k) do
    ranked =
      from s in Score,
        windows: [by_user: [partition_by: s.user_id, order_by: [desc: s.score]]],
        select: %{
          id: s.id,
          rn: row_number() |> over(:by_user)
        }

    from(s in Score,
      join: r in subquery(ranked),
      on: r.id == s.id,
      where: r.rn <= ^k,
      order_by: [asc: s.user_id, desc: s.score]
    )
    |> Repo.all()
  end
end
```

---

## Why this works

- `rank()` and `percent_rank()` are evaluated by Postgres in one pass over the sorted
  result set. With an index on `(score DESC)`, the sort is free.
- `select_merge` injects virtual fields into the `%Score{}` struct. The rest of the app
  continues to treat the result as a normal schema struct.
- `row_number() OVER (PARTITION BY user_id ORDER BY score DESC)` is the canonical top-N-per-
  group idiom in Postgres. Combined with a `WHERE rn <= k` in an outer query, it avoids
  nested loops.
- Named windows (`windows: [by_user: [...]]`) are declared once and can be referenced by
  multiple window-function calls — avoiding syntactic noise.

---

## Data flow — `user_rank/3`

```
user_rank("u-42", from, to)
    │
    ▼
SELECT user_id, score,
       rank()         OVER (ORDER BY score DESC) AS rank,
       percent_rank() OVER (ORDER BY score DESC) AS pct
FROM scores WHERE played_at IN [from, to)       -- CTE materialized
    │
    ▼
SELECT score, rank, pct
FROM (above) AS r
WHERE r.user_id = 'u-42'
ORDER BY rank
LIMIT 1
```

Postgres evaluates the window-producing subquery once, then selects the target row. With
an index on `(played_at, score)`, the whole thing is an index-only scan.

---

## Tests

```elixir
# test/leaderboard/stats_test.exs
defmodule Leaderboard.StatsTest do
  use ExUnit.Case, async: false
  alias Leaderboard.{Repo, Stats}
  alias Leaderboard.Schemas.Score

  setup do
    Ecto.Adapters.SQL.Sandbox.checkout(Repo)
    Ecto.Adapters.SQL.Sandbox.mode(Repo, {:shared, self()})
    Repo.delete_all(Score)

    now = DateTime.utc_now() |> DateTime.truncate(:second)

    Repo.insert_all(Score, [
      %{user_id: "alice", score: 100, played_at: now, inserted_at: now},
      %{user_id: "bob", score: 90, played_at: now, inserted_at: now},
      %{user_id: "carol", score: 80, played_at: now, inserted_at: now},
      %{user_id: "alice", score: 70, played_at: now, inserted_at: now}
    ])

    {:ok, now: now}
  end

  describe "leaderboard/3" do
    test "ranks global scores descending", %{now: now} do
      results = Stats.leaderboard(DateTime.add(now, -1), DateTime.add(now, 1))

      assert [first, _, _, last] = results
      assert first.score == 100
      assert first.rank == 1
      assert last.rank == 4
    end

    test "percent_rank is 0 for the top", %{now: now} do
      [first | _] = Stats.leaderboard(DateTime.add(now, -1), DateTime.add(now, 1))
      assert first.pct == 0.0
    end
  end

  describe "user_rank/3" do
    test "returns the best rank for a user with multiple scores", %{now: now} do
      result = Stats.user_rank("alice", DateTime.add(now, -1), DateTime.add(now, 1))
      assert result.score == 100
      assert result.rank == 1
    end

    test "returns nil for unknown user", %{now: now} do
      assert nil == Stats.user_rank("nobody", DateTime.add(now, -1), DateTime.add(now, 1))
    end
  end

  describe "user_history/2 rolling average" do
    test "computes 7-row rolling mean", %{now: now} do
      Repo.delete_all(Score)

      for n <- 1..10 do
        ts = DateTime.add(now, n)

        Repo.insert!(%Score{
          user_id: "alice",
          score: n * 10,
          played_at: ts
        })
      end

      history = Stats.user_history("alice", 20)

      # The last row is the average of scores 40..100 = 70.0
      last = List.last(history)
      assert_in_delta last.rolling_avg, 70.0, 0.01
    end
  end

  describe "top_n_per_user/1" do
    test "keeps at most k scores per user", %{now: now} do
      Repo.delete_all(Score)

      for u <- ~w(a b),
          n <- 1..5 do
        Repo.insert!(%Score{user_id: u, score: n, played_at: now})
      end

      result = Stats.top_n_per_user(2)
      assert length(result) == 4
      grouped = Enum.group_by(result, & &1.user_id)
      assert Enum.all?(grouped, fn {_u, rows} -> length(rows) == 2 end)
    end
  end
end
```

---

## Benchmark

```elixir
# bench/stats_bench.exs
alias Leaderboard.{Repo, Stats}
alias Leaderboard.Schemas.Score

Repo.delete_all(Score)
now = DateTime.utc_now() |> DateTime.truncate(:second)

rows =
  for i <- 1..100_000 do
    %{
      user_id: "u-#{rem(i, 5_000)}",
      score: :rand.uniform(10_000),
      played_at: DateTime.add(now, -:rand.uniform(86_400)),
      inserted_at: now
    }
  end

Enum.chunk_every(rows, 5_000) |> Enum.each(&Repo.insert_all(Score, &1))

from_ts = DateTime.add(now, -86_400)
to_ts = now

Benchee.run(
  %{
    "leaderboard top 100" => fn -> Stats.leaderboard(from_ts, to_ts, 100) end,
    "user_rank single"    => fn -> Stats.user_rank("u-42", from_ts, to_ts) end,
    "top 3 per user"      => fn -> Stats.top_n_per_user(3) end
  },
  time: 5, warmup: 2
)
```

**Target**: `leaderboard top 100` under 30 ms against 100k rows with an index on `score`.
`user_rank` under 15 ms. If you see > 100 ms, run `EXPLAIN ANALYZE` — the window sort
should be using the index, not doing an external merge.

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

**1. Window functions cannot reference another window's output in the same SELECT.**
If you need `rank > 10`, you wrap in a subquery: `WHERE r.rank > 10`. Ecto expresses this
with `from(r in subquery(ranked), where: r.rank > 10)`.

**2. `partition_by` without `order_by` yields undefined order within partitions.** If you
`row_number()` with `partition_by: :user_id` but no `order_by`, results vary per run.

**3. Frame clause defaults bite.** Without an explicit frame, `avg() OVER (ORDER BY ...)`
uses `RANGE UNBOUNDED PRECEDING` — a cumulative average, not a rolling one. Always state
the frame.

**4. Virtual fields are per-select.** `Score.changeset/2` does not include `:rank`. If
you try to insert a struct with a virtual field populated, Ecto silently drops it — which
is correct but confusing if you are logging.

**5. `percent_rank()` divides by N-1; with N=1 it is 0.** Do not compute percentiles over
empty or singleton partitions without a guard.

**6. When NOT to use window functions.** For "aggregate per group" (SUM, AVG), GROUP BY
is simpler and faster — windows are for *per-row* results alongside aggregates.

---

## Reflection

Your leaderboard endpoint takes 25 ms at p99 for 100k rows. Marketing launches a tournament
and expects 10× traffic on this endpoint. Adding a read replica spreads load, but window
functions still re-scan each call. What caching layer do you add — materialized view?
per-bucket denormalized leaderboard table? an ETS cache in the app? — and what is the
maximum staleness your users tolerate before caching hurts the product more than it helps?

---

## Executable Example

```elixir
defp deps do
  [
    {:ecto_sql, "~> 3.12"},
    {:postgrex, "~> 0.19"},
    {:benchee, "~> 1.3", only: :dev}
  ]
end

# lib/leaderboard/schemas/score.ex
defmodule Leaderboard.Schemas.Score do
  use Ecto.Schema

  schema "scores" do
    field :user_id, :string
    field :score, :integer
    field :played_at, :utc_datetime

    # populated by window functions — not in the table
    field :rank, :integer, virtual: true
    field :pct, :float, virtual: true
    field :rolling_avg, :float, virtual: true

    timestamps(updated_at: false)
  end
end

# priv/repo/migrations/20260101000000_create_scores.exs
defmodule Leaderboard.Repo.Migrations.CreateScores do
  use Ecto.Migration

  def change do
    create table(:scores) do
      add :user_id, :string, null: false
      add :score, :integer, null: false
      add :played_at, :utc_datetime, null: false
      timestamps(updated_at: false)
    end

    create index(:scores, [:played_at])
    create index(:scores, [:user_id, :played_at])
    create index(:scores, [:score])
  end
end

# lib/leaderboard/stats.ex
defmodule Leaderboard.Stats do
  end
  @moduledoc """
  Leaderboard queries using Postgres window functions.
  """
  import Ecto.Query

  alias Leaderboard.Repo
  alias Leaderboard.Schemas.Score

  @doc """
  Global leaderboard within a time window. Returns scores sorted by rank asc.

  Each row carries `:rank` (1 = top) and `:pct` (0.0 = top, 1.0 = bottom).
  """
  @spec leaderboard(DateTime.t(), DateTime.t(), non_neg_integer()) :: [Score.t()]
  def leaderboard(from_ts, to_ts, limit \\ 100) do
    query =
      from s in Score,
        where: s.played_at >= ^from_ts and s.played_at < ^to_ts,
        windows: [global: [order_by: [desc: s.score]]],
        select: s,
        select_merge: %{
          rank: rank() |> over(:global),
          pct: percent_rank() |> over(:global)
        },
        order_by: [desc: s.score],
        limit: ^limit

    Repo.all(query)
  end

  @doc """
  Per-user history with a 7-row rolling average.

  Uses a frame clause `ROWS BETWEEN 6 PRECEDING AND CURRENT ROW` so each row's
  `rolling_avg` is the mean of the last 7 scores (including itself).
  """
  @spec user_history(String.t(), non_neg_integer()) :: [Score.t()]
  def user_history(user_id, limit \\ 50) do
    query =
      from s in Score,
        where: s.user_id == ^user_id,
        order_by: [asc: s.played_at],
        select: s,
        select_merge: %{
          rolling_avg:
            fragment(
              "AVG(?) OVER (ORDER BY ? ROWS BETWEEN 6 PRECEDING AND CURRENT ROW)",
              s.score,
              s.played_at
            )
        },
        limit: ^limit

    Repo.all(query)
  end

  @doc """
  Rank of one user within a window — single-row response.

  Computed as a subquery so we do not download the whole leaderboard.
  """
  @spec user_rank(String.t(), DateTime.t(), DateTime.t()) ::
          %{score: integer(), rank: integer(), pct: float()} | nil
  def user_rank(user_id, from_ts, to_ts) do
    ranked =
      from s in Score,
        where: s.played_at >= ^from_ts and s.played_at < ^to_ts,
        windows: [g: [order_by: [desc: s.score]]],
        select: %{
          user_id: s.user_id,
          score: s.score,
          rank: rank() |> over(:g),
          pct: percent_rank() |> over(:g)
        }

    from(r in subquery(ranked),
      where: r.user_id == ^user_id,
      order_by: [asc: r.rank],
      limit: 1,
      select: %{score: r.score, rank: r.rank, pct: r.pct}
    )
    |> Repo.one()
  end

  @doc """
  Top N per user (keeps personal bests).

  Uses `row_number()` partitioned by user to tag rows, then keeps only those with
  rn <= k. This is a classic top-N-per-group pattern.
  """
  @spec top_n_per_user(non_neg_integer()) :: [Score.t()]
  def top_n_per_user(k) do
    ranked =
      from s in Score,
        windows: [by_user: [partition_by: s.user_id, order_by: [desc: s.score]]],
        select: %{
          id: s.id,
          rn: row_number() |> over(:by_user)
        }

    from(s in Score,
      join: r in subquery(ranked),
      on: r.id == s.id,
      where: r.rn <= ^k,
      order_by: [asc: s.user_id, desc: s.score]
    )
    |> Repo.all()
  end
end

defmodule Main do
  def main do
      # Demonstrating 278-window-functions-select-merge
      :ok
  end
end

Main.main()
```
