# Upserts with `on_conflict` and Returning Semantics

**Project**: `metrics_ingest` — high-throughput aggregation via `INSERT ... ON CONFLICT DO UPDATE`.

---

## Project context

A fleet of 2,000 IoT devices pushes sensor readings every 5 seconds. You must maintain a
per-device per-minute aggregate (count, sum, min, max) for a real-time dashboard. The naive
approach — `SELECT` then `UPDATE` or `INSERT` — has a race condition: two concurrent
ingests create two aggregate rows for the same minute. You need atomic upsert.

Postgres offers `INSERT ... ON CONFLICT (...) DO UPDATE`. Ecto exposes this through the
`on_conflict:` option on `Repo.insert/2` and `Repo.insert_all/3`. This exercise builds a
production-grade ingest path using `insert_all` with `on_conflict` for bulk efficiency.

```
metrics_ingest/
├── lib/
│   └── metrics_ingest/
│       ├── application.ex
│       ├── repo.ex
│       ├── ingest.ex                 # batch upsert entrypoint
│       ├── buffer.ex                 # GenServer that accumulates and flushes
│       └── schemas/
│           ├── reading.ex            # raw timestamped datapoints
│           └── minute_aggregate.ex   # the upsert target
├── priv/repo/migrations/
├── test/metrics_ingest/
│   └── ingest_test.exs
├── bench/ingest_bench.exs
└── mix.exs
```

---

## Why `on_conflict` and not `SELECT`-then-write

```elixir
# WRONG: TOCTOU race
case Repo.get_by(MinuteAggregate, device_id: d, minute: m) do
  nil  -> Repo.insert(%MinuteAggregate{device_id: d, minute: m, count: 1, sum: v})
  agg  -> Repo.update(MinuteAggregate.inc_changeset(agg, v))
end
```

Two concurrent requests with the same `(device_id, minute)` both `SELECT nil`, both `INSERT`,
and one fails on the unique constraint. Retry logic is required.

With `on_conflict`, a single statement does the right thing regardless of concurrency:

```sql
INSERT INTO minute_aggregates (device_id, minute, count, sum, min, max)
VALUES ($1, $2, 1, $3, $3, $3)
ON CONFLICT (device_id, minute) DO UPDATE
  SET count = minute_aggregates.count + 1,
      sum   = minute_aggregates.sum + EXCLUDED.sum,
      min   = LEAST(minute_aggregates.min, EXCLUDED.min),
      max   = GREATEST(minute_aggregates.max, EXCLUDED.max)
```

---

## Core concepts

### 1. `on_conflict` variants in Ecto

| Value | Meaning |
|-------|---------|
| `:nothing` | `DO NOTHING` — silently ignore duplicates |
| `:replace_all` | overwrite every column from the input row |
| `{:replace, fields}` | overwrite a whitelist |
| `{:replace_all_except, fields}` | overwrite all except a blacklist |
| keyword or query | custom `SET` clause using `EXCLUDED` semantics |

The most powerful variant is a keyword list compiled into an `Ecto.Query`:

```elixir
on_conflict: [inc: [count: 1, sum: value]]
```

This compiles to `SET count = minute_aggregates.count + 1, sum = minute_aggregates.sum + EXCLUDED.sum`.
For non-numeric updates (LEAST/GREATEST), use a raw query expression.

### 2. `conflict_target` is required

Unlike MySQL, Postgres requires you to name the unique constraint or index that defines
"conflict":

```elixir
conflict_target: [:device_id, :minute]
```

or:

```elixir
conflict_target: {:constraint, :minute_aggregates_device_minute_key}
```

The second form is mandatory for partial indexes (where column list is ambiguous).

### 3. `returning` decides what you get back

```elixir
Repo.insert(changeset,
  on_conflict: {:replace, [:value]},
  conflict_target: :key,
  returning: true
)
```

With `returning: true`, the returned struct has fresh values. Without it, the struct is
the one you *sent*, which will be stale on an update branch. For idempotent writes where
callers compare state, always use `returning: true`.

### 4. `insert_all/3` batches thousands of rows at once

A single round-trip can upsert 5,000 rows. Ecto chunks automatically only up to the
Postgres parameter limit (65,535 parameters). You must split into batches respecting
`param_count × rows ≤ 65_535`.

---

## Design decisions

- **Option A — one upsert per reading**: write immediately in request path.
  Pros: simplest. Cons: 2,000 devices × 0.2 Hz = 400 Hz of single-row upserts → DB CPU.
- **Option B — buffered `insert_all` upsert**: accumulate in a GenServer, flush every N ms.
  Pros: 100× throughput. Cons: small latency (~100 ms), in-memory buffer is volatile.

We use **Option B**. A per-partition `Buffer` GenServer accumulates readings and flushes
every 100 ms or 1,000 rows, whichever comes first.

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

```elixir
# priv/repo/migrations/20260101000000_create_minute_aggregates.exs
defmodule MetricsIngest.Repo.Migrations.CreateMinuteAggregates do
  use Ecto.Migration

  def change do
    create table(:minute_aggregates) do
      add :device_id, :string, null: false
      add :minute, :utc_datetime, null: false
      add :count, :integer, null: false, default: 0
      add :sum, :float, null: false, default: 0.0
      add :min, :float, null: false
      add :max, :float, null: false
      timestamps()
    end

    create unique_index(:minute_aggregates, [:device_id, :minute],
             name: :minute_aggregates_device_minute_key)
  end
end
```

### Step 2: Schema

```elixir
# lib/metrics_ingest/schemas/minute_aggregate.ex
defmodule MetricsIngest.Schemas.MinuteAggregate do
  use Ecto.Schema

  schema "minute_aggregates" do
    field :device_id, :string
    field :minute, :utc_datetime
    field :count, :integer, default: 0
    field :sum, :float, default: 0.0
    field :min, :float
    field :max, :float
    timestamps()
  end
end
```

### Step 3: Ingest — bulk upsert

```elixir
# lib/metrics_ingest/ingest.ex
defmodule MetricsIngest.Ingest do
  @moduledoc """
  Batch upsert of per-minute aggregates.

  Input: list of readings `%{device_id, ts, value}`.
  Output: `{:ok, affected_count}`.
  """
  import Ecto.Query

  alias MetricsIngest.Repo
  alias MetricsIngest.Schemas.MinuteAggregate

  @doc """
  Upserts aggregates from a batch of raw readings.

  Coalesces readings for the same (device_id, minute) into a single row locally before
  hitting the DB — reduces conflict resolution pressure in Postgres.
  """
  @spec upsert([%{device_id: String.t(), ts: DateTime.t(), value: float()}]) ::
          {:ok, non_neg_integer()}
  def upsert([]), do: {:ok, 0}

  def upsert(readings) do
    now = DateTime.utc_now() |> DateTime.truncate(:second)

    rows =
      readings
      |> coalesce_locally()
      |> Enum.map(fn {{device_id, minute}, agg} ->
        %{
          device_id: device_id,
          minute: minute,
          count: agg.count,
          sum: agg.sum,
          min: agg.min,
          max: agg.max,
          inserted_at: now,
          updated_at: now
        }
      end)

    {n, _} =
      Repo.insert_all(
        MinuteAggregate,
        rows,
        on_conflict: upsert_query(),
        conflict_target: [:device_id, :minute]
      )

    {:ok, n}
  end

  # --------------------------------------------------------------------------
  # Local coalescing before the DB call
  # --------------------------------------------------------------------------

  defp coalesce_locally(readings) do
    Enum.reduce(readings, %{}, fn %{device_id: d, ts: ts, value: v}, acc ->
      key = {d, minute_bucket(ts)}

      Map.update(
        acc,
        key,
        %{count: 1, sum: v, min: v, max: v},
        fn a ->
          %{count: a.count + 1, sum: a.sum + v, min: min(a.min, v), max: max(a.max, v)}
        end
      )
    end)
  end

  defp minute_bucket(%DateTime{} = dt) do
    dt
    |> DateTime.to_unix()
    |> div(60)
    |> Kernel.*(60)
    |> DateTime.from_unix!()
    |> DateTime.truncate(:second)
  end

  # --------------------------------------------------------------------------
  # The update expression
  #
  # We cannot use `[inc: ...]` because we need LEAST/GREATEST for min/max.
  # The fragment uses EXCLUDED to reference the row we attempted to insert.
  # --------------------------------------------------------------------------

  defp upsert_query do
    from(m in MinuteAggregate,
      update: [
        set: [
          count: fragment("? + EXCLUDED.count", m.count),
          sum: fragment("? + EXCLUDED.sum", m.sum),
          min: fragment("LEAST(?, EXCLUDED.min)", m.min),
          max: fragment("GREATEST(?, EXCLUDED.max)", m.max),
          updated_at: fragment("EXCLUDED.updated_at")
        ]
      ]
    )
  end
end
```

### Step 4: Buffer — flushes every 100 ms or 1,000 rows

```elixir
# lib/metrics_ingest/buffer.ex
defmodule MetricsIngest.Buffer do
  use GenServer

  @flush_ms 100
  @max_batch 1_000

  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  def push(reading), do: GenServer.cast(__MODULE__, {:push, reading})

  @impl true
  def init(_) do
    schedule_flush()
    {:ok, %{buffer: [], count: 0}}
  end

  @impl true
  def handle_cast({:push, reading}, %{buffer: buf, count: c} = state) do
    state = %{state | buffer: [reading | buf], count: c + 1}

    if state.count >= @max_batch do
      {:noreply, flush(state)}
    else
      {:noreply, state}
    end
  end

  @impl true
  def handle_info(:flush, state) do
    state = flush(state)
    schedule_flush()
    {:noreply, state}
  end

  defp flush(%{buffer: []} = state), do: state

  defp flush(%{buffer: buf} = state) do
    {:ok, _n} = MetricsIngest.Ingest.upsert(buf)
    %{state | buffer: [], count: 0}
  end

  defp schedule_flush, do: Process.send_after(self(), :flush, @flush_ms)
end
```

---

## Why this works

Local coalescing reduces N readings to at most one row per `(device_id, minute)`. For a
batch of 1,000 readings from 200 devices in 2 minutes, you send ~400 rows to Postgres
instead of 1,000. The upsert statement handles intra-row merging; local coalescing handles
intra-batch merging.

The `fragment/1` references `EXCLUDED.*`, which is Postgres's name for the row that *would
have been inserted* had there been no conflict. The left side (`?`) unquotes the column
expression of the existing row.

`insert_all` issues a single SQL statement, so the whole batch is atomic. A failure rolls
back the entire batch; partial flushes are not a concern.

---

## Data flow

```
HTTP POST /ingest [reading, reading, ...]
        │
        ▼
Buffer.push/1  (GenServer cast)
        │
        ▼
accumulate in-memory
        │
        ▼ (every 100 ms OR 1k rows)
Ingest.upsert/1
        ├── coalesce_locally  (map by {device, minute})
        └── Repo.insert_all
                 │
                 ▼
     INSERT ... ON CONFLICT DO UPDATE
     (atomic, single round-trip)
```

---

## Tests

```elixir
# test/metrics_ingest/ingest_test.exs
defmodule MetricsIngest.IngestTest do
  use ExUnit.Case, async: false
  alias MetricsIngest.{Ingest, Repo}
  alias MetricsIngest.Schemas.MinuteAggregate

  setup do
    Ecto.Adapters.SQL.Sandbox.checkout(Repo)
    :ok = Ecto.Adapters.SQL.Sandbox.mode(Repo, {:shared, self()})
    Repo.delete_all(MinuteAggregate)
    :ok
  end

  describe "upsert/1" do
    test "creates a fresh aggregate for a new (device, minute)" do
      ts = ~U[2026-04-12 10:00:15Z]

      {:ok, 1} = Ingest.upsert([%{device_id: "d1", ts: ts, value: 5.0}])

      [agg] = Repo.all(MinuteAggregate)
      assert agg.count == 1
      assert agg.sum == 5.0
      assert agg.min == 5.0
      assert agg.max == 5.0
    end

    test "merges two readings in the same minute" do
      ts = ~U[2026-04-12 10:00:15Z]

      {:ok, 1} = Ingest.upsert([%{device_id: "d1", ts: ts, value: 2.0}])
      {:ok, 1} = Ingest.upsert([%{device_id: "d1", ts: ts, value: 8.0}])

      [agg] = Repo.all(MinuteAggregate)
      assert agg.count == 2
      assert agg.sum == 10.0
      assert agg.min == 2.0
      assert agg.max == 8.0
    end

    test "coalesces readings locally in a single batch" do
      ts = ~U[2026-04-12 10:00:00Z]

      readings =
        for v <- 1..100, do: %{device_id: "d1", ts: ts, value: v / 1.0}

      {:ok, 1} = Ingest.upsert(readings)

      [agg] = Repo.all(MinuteAggregate)
      assert agg.count == 100
      assert agg.min == 1.0
      assert agg.max == 100.0
    end

    test "separates different minutes into distinct rows" do
      readings = [
        %{device_id: "d1", ts: ~U[2026-04-12 10:00:15Z], value: 1.0},
        %{device_id: "d1", ts: ~U[2026-04-12 10:01:15Z], value: 2.0}
      ]

      {:ok, 2} = Ingest.upsert(readings)
      assert Repo.aggregate(MinuteAggregate, :count) == 2
    end
  end

  describe "concurrency" do
    test "two concurrent batches for same bucket produce correct total" do
      ts = ~U[2026-04-12 10:00:15Z]

      tasks =
        for _ <- 1..10 do
          Task.async(fn ->
            Ecto.Adapters.SQL.Sandbox.allow(Repo, self(), self())
            readings = for _ <- 1..50, do: %{device_id: "d1", ts: ts, value: 1.0}
            Ingest.upsert(readings)
          end)
        end

      _ = Task.await_many(tasks, 5_000)

      [agg] = Repo.all(MinuteAggregate)
      assert agg.count == 500
      assert agg.sum == 500.0
    end
  end
end
```

---

## Benchmark

```elixir
# bench/ingest_bench.exs
readings =
  for _ <- 1..1_000 do
    %{
      device_id: "d#{:rand.uniform(200)}",
      ts: DateTime.utc_now(),
      value: :rand.uniform() * 100
    }
  end

Benchee.run(
  %{
    "upsert 1k readings" => fn -> MetricsIngest.Ingest.upsert(readings) end
  },
  time: 5, warmup: 2
)
```

**Target**: under 15 ms for 1,000 readings coalesced into ~200 rows on local Postgres.
If you exceed 50 ms, check that local coalescing is in fact reducing the row count sent
to the DB.

---

## Trade-offs and production gotchas

**1. `on_conflict` query is not validated against the schema.** Ecto does not type-check
the fragment. A typo in a column name surfaces as a Postgres error at runtime.

**2. `inserted_at` races on concurrent upserts.** The column gets the value from whichever
inserter's `INSERT` won the race. For true "first seen" semantics, use a DB default
(`DEFAULT now()`) and do not pass the column in the row map.

**3. `returning: true` does not work with `insert_all/3`.** Use `returning: [:id]` with
an explicit column list.

**4. Large batches hit the parameter limit.** 65,535 parameters ÷ 8 columns = ~8,000 rows.
For bigger batches, chunk with `Stream.chunk_every/2`.

**5. Partial indexes require named `conflict_target`.** If your unique index has a
`WHERE deleted_at IS NULL`, Postgres rejects a column-list conflict target because several
indexes could match. Use `{:constraint, :my_index_name}`.

**6. When NOT to use `on_conflict` updates.** If the "conflict" semantic is "reject the
duplicate with an error", prefer a plain `unique_constraint` on the changeset and handle
the `{:error, changeset}` path. Upsert-update is for merge semantics, not dedup semantics.

---

## Reflection

Your dashboard reads from `minute_aggregates`. A late-arriving reading for a 10-minute-old
minute upserts the row and increments `count`, but the dashboard already reported the old
total. How do you design the read side so that late arrivals are visible without breaking
monotonicity — and what is the cost of correctness vs. promptness?

---

## Resources

- [Postgres `INSERT ... ON CONFLICT`](https://www.postgresql.org/docs/current/sql-insert.html#SQL-ON-CONFLICT)
- [Ecto — "Upsert" guide](https://hexdocs.pm/ecto/constraints-and-upserts.html)
- [Dashbit blog — "Ecto upserts"](https://dashbit.co/blog)
- [pg_stat_statements](https://www.postgresql.org/docs/current/pgstatstatements.html) for measuring conflict rates
