# Schemaless Queries for ETL

**Project**: `data_pipeline` — bulk ETL with Ecto without defining schemas.

---

## Project context

You need to migrate 40 million rows from a legacy `orders_v1` table into `orders_v2` with
column renames, type coercions, and derived fields. Defining full schemas for both tables
is throwaway work — they only exist during the migration window. Ecto supports schemaless
queries: you reference tables by string name, columns by atom, and get back maps.

```
data_pipeline/
├── lib/
│   └── data_pipeline/
│       ├── application.ex
│       ├── repo.ex
│       └── etl.ex
├── priv/repo/migrations/
├── test/data_pipeline/
│   └── etl_test.exs
├── bench/etl_bench.exs
└── mix.exs
```

---

## Core concepts

### 1. Table as string, columns as atoms

```elixir
from o in "orders_v1",
  select: %{id: o.id, customer: o.customer_name, total: o.total_cents}
```

No schema module. The column references are validated at query time (not compile time).
The return is a plain map.

### 2. Type hints for correct decoding

Without a schema, Ecto does not know Postgres types. For anything non-scalar (dates,
decimals, UUIDs), cast explicitly:

```elixir
select: %{
  id: type(o.id, :integer),
  created_at: type(o.created_at, :utc_datetime),
  total: type(o.total_cents, :integer)
}
```

Missing type hints yield `%Postgrex.*` structs or strings — painful downstream.

### 3. `insert_all` with schemaless target

```elixir
Repo.insert_all("orders_v2", rows, returning: [:id])
```

`rows` is a list of maps. No changeset validation, no hooks. Fast: one round-trip per chunk.

### 4. Stream from one table to another

```elixir
Repo.transaction(fn ->
  from(o in "orders_v1", select: %{...})
  |> Repo.stream(max_rows: 1_000)
  |> Stream.chunk_every(500)
  |> Stream.each(fn batch ->
    transformed = Enum.map(batch, &transform/1)
    Repo.insert_all("orders_v2", transformed)
  end)
  |> Stream.run()
end)
```

`Repo.stream/2` uses a Postgres cursor; memory stays flat regardless of row count.

---

## Design decisions

- **Option A — full schemas for both legacy and new tables**: familiar, safe.
  Pros: type-safe. Cons: dead code after migration; changeset hooks interfere with ETL.
- **Option B — schemaless queries with explicit type hints**: lean.
  Pros: no throwaway schemas. Cons: less compile-time safety.

We use **Option B**. The ETL is one-shot; schemas would survive in the codebase as
archaeology. Explicit `type/2` calls restore safety where it matters.

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

### Step 1: Migration — two tables

```elixir
# priv/repo/migrations/20260101000000_create_legacy_and_new.exs
defmodule DataPipeline.Repo.Migrations.CreateLegacyAndNew do
  use Ecto.Migration

  def change do
    create table(:orders_v1) do
      add :customer_name, :string
      add :total_cents, :integer
      add :created_at, :utc_datetime
      add :legacy_status, :string
    end

    create table(:orders_v2) do
      add :customer_key, :string
      add :total_cents, :integer
      add :placed_at, :utc_datetime
      add :status, :string
      add :imported_from, :integer
      timestamps()
    end

    create unique_index(:orders_v2, [:imported_from])
    create index(:orders_v1, [:created_at])
  end
end
```

### Step 2: ETL module

```elixir
# lib/data_pipeline/etl.ex
defmodule DataPipeline.Etl do
  @moduledoc """
  Schemaless ETL: streams rows from orders_v1, transforms, and inserts into orders_v2.
  """
  import Ecto.Query

  alias DataPipeline.Repo

  @chunk_size 500
  @cursor_chunk 1_000

  @doc """
  Streams all legacy orders through a transformation and inserts them into orders_v2.

  Idempotent: skips rows already imported via a UNIQUE constraint on imported_from.
  """
  @spec migrate_orders() :: {:ok, non_neg_integer()} | {:error, term()}
  def migrate_orders do
    Repo.transaction(fn ->
      legacy_stream()
      |> Stream.chunk_every(@chunk_size)
      |> Stream.map(&transform_batch/1)
      |> Stream.map(&insert_batch/1)
      |> Enum.sum()
    end, timeout: :infinity)
  end

  @doc """
  Source query — schemaless select from orders_v1 with explicit types.
  """
  def legacy_stream do
    query =
      from o in "orders_v1",
        select: %{
          id: type(o.id, :integer),
          customer_name: o.customer_name,
          total_cents: type(o.total_cents, :integer),
          created_at: type(o.created_at, :utc_datetime),
          legacy_status: o.legacy_status
        },
        order_by: [asc: o.id]

    Repo.stream(query, max_rows: @cursor_chunk)
  end

  @doc """
  Transform a batch. Pure function — no DB access.
  """
  def transform_batch(rows) do
    now = DateTime.utc_now() |> DateTime.truncate(:second)

    Enum.map(rows, fn row ->
      %{
        customer_key: customer_key(row.customer_name),
        total_cents: row.total_cents,
        placed_at: row.created_at,
        status: map_status(row.legacy_status),
        imported_from: row.id,
        inserted_at: now,
        updated_at: now
      }
    end)
  end

  defp customer_key(nil), do: "unknown"

  defp customer_key(name) do
    name
    |> String.downcase()
    |> String.replace(~r/[^a-z0-9]/, "")
    |> String.slice(0, 40)
  end

  defp map_status("OPEN"), do: "pending"
  defp map_status("DONE"), do: "completed"
  defp map_status("CANCELED"), do: "cancelled"
  defp map_status(_), do: "unknown"

  defp insert_batch(rows) do
    {n, _} =
      Repo.insert_all(
        "orders_v2",
        rows,
        on_conflict: :nothing,
        conflict_target: :imported_from
      )

    n
  end

  # --------------------------------------------------------------------------
  # Verification helpers — run after migration
  # --------------------------------------------------------------------------

  @doc "Counts rows in both tables and returns the delta."
  def row_count_delta do
    [%{n: v1}] = Repo.all(from o in "orders_v1", select: %{n: count(o.id)})
    [%{n: v2}] = Repo.all(from o in "orders_v2", select: %{n: count(o.id)})
    {v1, v2, v1 - v2}
  end

  @doc "Returns legacy IDs that failed to import (diff by set)."
  def missing_ids(limit \\ 100) do
    query =
      from o in "orders_v1",
        left_join: n in "orders_v2", on: n.imported_from == o.id,
        where: is_nil(n.imported_from),
        select: o.id,
        limit: ^limit

    Repo.all(query)
  end
end
```

---

## Why this works

- `Repo.stream/2` opens a Postgres cursor via `DECLARE ... CURSOR` and fetches in chunks
  of `max_rows`. Memory stays bounded — a 40M row source does not OOM the app node.
- The transform is pure: no DB calls inside `transform_batch`. This decouples CPU work
  from DB latency and makes the transform independently testable.
- `insert_all` with `on_conflict: :nothing` on `imported_from` makes the ETL idempotent.
  Re-running it after a crash skips already-imported rows.
- The whole pipeline is wrapped in a `Repo.transaction` so the cursor remains open. If
  the transform crashes, Postgres rolls back any inserts (though `on_conflict: :nothing`
  means we mostly do not need the rollback — it is a defensive belt).

---

## Data flow

```
orders_v1 (40M rows)
    │
    ▼ Repo.stream (cursor, 1k rows/chunk)
schemaless select %{id, customer_name, total_cents, ...}
    │
    ▼ Stream.chunk_every(500)
batches of 500
    │
    ▼ transform_batch (pure, no DB)
list of maps shaped for orders_v2
    │
    ▼ insert_all on_conflict: :nothing
Postgres: INSERT INTO orders_v2 ... ON CONFLICT (imported_from) DO NOTHING
    │
    ▼
orders_v2 (grows incrementally)
```

---

## Tests

```elixir
# test/data_pipeline/etl_test.exs
defmodule DataPipeline.EtlTest do
  use ExUnit.Case, async: false
  alias DataPipeline.{Etl, Repo}

  setup do
    Ecto.Adapters.SQL.Sandbox.checkout(Repo)
    Ecto.Adapters.SQL.Sandbox.mode(Repo, {:shared, self()})
    Ecto.Adapters.SQL.query!(Repo, "TRUNCATE orders_v1, orders_v2 RESTART IDENTITY", [])
    :ok
  end

  defp seed_legacy(rows) do
    now = DateTime.utc_now() |> DateTime.truncate(:second)

    data =
      Enum.map(rows, fn {name, total, status} ->
        %{customer_name: name, total_cents: total, legacy_status: status, created_at: now}
      end)

    Repo.insert_all("orders_v1", data)
  end

  describe "migrate_orders/0" do
    test "copies rows with transforms" do
      seed_legacy([
        {"Ada Lovelace", 1000, "OPEN"},
        {"Alan Turing", 2500, "DONE"}
      ])

      assert {:ok, 2} = Etl.migrate_orders()

      rows = Repo.all(from o in "orders_v2", select: %{key: o.customer_key, status: o.status})
      assert Enum.any?(rows, &(&1.key == "adalovelace" and &1.status == "pending"))
      assert Enum.any?(rows, &(&1.key == "alanturing" and &1.status == "completed"))
    end

    test "is idempotent" do
      seed_legacy([{"Repeat", 500, "OPEN"}])
      {:ok, 1} = Etl.migrate_orders()
      {:ok, 0} = Etl.migrate_orders()
      assert [_] = Repo.all(from o in "orders_v2", select: o.id)
    end
  end

  describe "transform_batch/1 (pure)" do
    test "maps statuses" do
      rows = [%{id: 1, customer_name: "X", total_cents: 1, created_at: DateTime.utc_now(), legacy_status: "CANCELED"}]
      [out] = Etl.transform_batch(rows)
      assert out.status == "cancelled"
    end

    test "normalises customer names" do
      rows = [%{id: 1, customer_name: "Jane D'Oe!!", total_cents: 1, created_at: DateTime.utc_now(), legacy_status: "OPEN"}]
      [out] = Etl.transform_batch(rows)
      assert out.customer_key == "janedoe"
    end

    test "handles nil customer_name" do
      rows = [%{id: 1, customer_name: nil, total_cents: 1, created_at: DateTime.utc_now(), legacy_status: "OPEN"}]
      [out] = Etl.transform_batch(rows)
      assert out.customer_key == "unknown"
    end
  end

  describe "missing_ids/1" do
    test "returns legacy IDs not yet imported" do
      seed_legacy([{"A", 1, "OPEN"}, {"B", 2, "OPEN"}])
      {:ok, 2} = Etl.migrate_orders()

      # Insert a new legacy row after migration
      seed_legacy([{"C", 3, "OPEN"}])

      assert Etl.missing_ids() != []
    end
  end
end
```

---

## Benchmark

```elixir
# bench/etl_bench.exs
alias DataPipeline.{Etl, Repo}

Ecto.Adapters.SQL.query!(Repo, "TRUNCATE orders_v1, orders_v2 RESTART IDENTITY", [])

rows =
  for _ <- 1..50_000 do
    %{
      customer_name: "c_#{:rand.uniform(10_000)}",
      total_cents: :rand.uniform(100_000),
      created_at: DateTime.utc_now() |> DateTime.truncate(:second),
      legacy_status: Enum.random(~w(OPEN DONE CANCELED))
    }
  end

Enum.chunk_every(rows, 5_000)
|> Enum.each(&Repo.insert_all("orders_v1", &1))

Benchee.run(
  %{
    "migrate 50k rows" => fn -> Etl.migrate_orders() end
  },
  time: 10, warmup: 2
)
```

**Target**: 50k rows migrated in under 4 seconds on local Postgres (≥ 12k rows/sec). If
you see < 2k rows/sec, the cursor chunk size is too small or you are not batching inserts.

---

## Trade-offs and production gotchas

**1. No compile-time safety on column names.** A typo in `o.total_cnts` raises at runtime
during the first execution. Mitigation: unit tests that exercise every query path.

**2. `Repo.stream` requires a transaction.** Using it outside one raises
`ArgumentError`. Long transactions hold a cursor — ensure the isolation level is
`read committed` (default) to avoid bloat.

**3. Type coercion for non-scalar fields is mandatory.** `o.created_at` without `type/2`
comes back as a string or tuple depending on Postgrex config — varies across versions.

**4. `insert_all` does not run changesets.** If the target table has a `NOT NULL` column
you forgot, you discover it via a Postgrex error at chunk N. Always run a single-row
migration first as a smoke test.

**5. Idempotency via unique constraint on `imported_from`.** Without it, retries
duplicate rows. Never rely on "check first" patterns for ETL idempotency.

**6. When NOT to go schemaless.** If the legacy table will remain in production alongside
the new one (e.g., shadow read), define a schema. Schemaless is for one-off migrations.

---

## Reflection

The migration runs nightly against the production replica; the cutover happens after a
final catch-up pass. During the catch-up, new orders are being inserted into `orders_v1`
by the live app. Describe the exact sequence of steps between the catch-up ETL, the
read-path cutover, and the write-path cutover that guarantees no order is lost and no
duplicate is created. What role does the `imported_from` unique constraint play in that
sequence?

---

## Resources

- [Ecto — schemaless queries](https://hexdocs.pm/ecto/schemaless-queries.html)
- [`Repo.stream/2`](https://hexdocs.pm/ecto/Ecto.Repo.html#c:stream/2)
- [Postgres — DECLARE CURSOR](https://www.postgresql.org/docs/current/sql-declare.html)
- [Dashbit — "Streaming with Ecto"](https://dashbit.co/blog)
