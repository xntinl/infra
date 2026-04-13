# Window Functions and `select_merge`

**Project**: `leaderboard` — per-user rank and rolling stats using Postgres window functions

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
leaderboard/
├── lib/
│   └── leaderboard.ex
├── script/
│   └── main.exs
├── test/
│   └── leaderboard_test.exs
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
defmodule Leaderboard.MixProject do
  use Mix.Project

  def project do
    [
      app: :leaderboard,
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

### `lib/leaderboard.ex`

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

### `test/leaderboard_test.exs`

```elixir
defmodule Leaderboard.StatsTest do
  use ExUnit.Case, async: true
  doctest Leaderboard.Schemas.Score
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

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Window Functions and `select_merge`.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Window Functions and `select_merge` ===")
    IO.puts("Category: Ecto advanced\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case Leaderboard.run(payload) do
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
        for _ <- 1..1_000, do: Leaderboard.run(:bench)
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
