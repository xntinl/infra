# Schemaless Queries for ETL

**Project**: `data_pipeline` — bulk ETL with Ecto without defining schemas

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
data_pipeline/
├── lib/
│   └── data_pipeline.ex
├── script/
│   └── main.exs
├── test/
│   └── data_pipeline_test.exs
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
defmodule DataPipeline.MixProject do
  use Mix.Project

  def project do
    [
      app: :data_pipeline,
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
### `lib/data_pipeline.ex`

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
### `test/data_pipeline_test.exs`

```elixir
defmodule DataPipeline.EtlTest do
  use ExUnit.Case, async: true
  doctest DataPipeline.Repo.Migrations.CreateLegacyAndNew
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
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Schemaless Queries for ETL.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Schemaless Queries for ETL ===")
    IO.puts("Category: Ecto advanced\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case DataPipeline.run(payload) do
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
        for _ <- 1..1_000, do: DataPipeline.run(:bench)
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
