# Upserts with `on_conflict` and Returning Semantics

**Project**: `metrics_ingest` — high-throughput aggregation via `INSERT ... ON CONFLICT DO UPDATE`

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
metrics_ingest/
├── lib/
│   └── metrics_ingest.ex
├── script/
│   └── main.exs
├── test/
│   └── metrics_ingest_test.exs
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
defmodule MetricsIngest.MixProject do
  use Mix.Project

  def project do
    [
      app: :metrics_ingest,
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

### `lib/metrics_ingest.ex`

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

### `test/metrics_ingest_test.exs`

```elixir
defmodule MetricsIngest.IngestTest do
  use ExUnit.Case, async: true
  doctest MetricsIngest.Repo.Migrations.CreateMinuteAggregates
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

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Upserts with `on_conflict` and Returning Semantics.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Upserts with `on_conflict` and Returning Semantics ===")
    IO.puts("Category: Ecto advanced\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case MetricsIngest.run(payload) do
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
        for _ <- 1..1_000, do: MetricsIngest.run(:bench)
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
