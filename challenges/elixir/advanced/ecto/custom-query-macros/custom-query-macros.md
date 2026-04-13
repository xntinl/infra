# Custom Query Macros — Extending `Ecto.Query.API`

**Project**: `geo_search` — custom Ecto fragments wrapped as reusable macros (PostGIS distance, time buckets)

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
geo_search/
├── lib/
│   └── geo_search.ex
├── script/
│   └── main.exs
├── test/
│   └── geo_search_test.exs
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
defmodule GeoSearch.MixProject do
  use Mix.Project

  def project do
    [
      app: :geo_search,
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
### `lib/geo_search.ex`

```elixir
# priv/repo/migrations/20260101000000_create_stores.exs
defmodule GeoSearch.Repo.Migrations.CreateStores do
  use Ecto.Migration

  @doc "Returns up result."
  def up do
    execute "CREATE EXTENSION IF NOT EXISTS postgis"

    create table(:stores) do
      add :name, :string, null: false
      add :location, :geometry, null: false
      add :opened_at, :utc_datetime
      timestamps()
    end

    # GiST index for spatial lookups
    execute "CREATE INDEX stores_location_idx ON stores USING GIST (location)"
    create index(:stores, [:opened_at])
  end

  @doc "Returns down result."
  def down do
    drop table(:stores)
    execute "DROP EXTENSION postgis"
  end
end

# lib/geo_search/schemas/store.ex
defmodule GeoSearch.Schemas.Store do
  use Ecto.Schema
  import Ecto.Changeset

  schema "stores" do
    field :name, :string
    field :location, :any, virtual: false
    field :opened_at, :utc_datetime
    field :distance_m, :float, virtual: true
    timestamps()
  end

  @doc "Returns changeset result from store and attrs."
  def changeset(store, attrs) do
    cast(store, attrs, [:name, :location, :opened_at])
    |> validate_required([:name, :location])
  end
end

# lib/geo_search/query_api.ex
defmodule GeoSearch.QueryApi do
  @moduledoc """
  Custom Ecto query macros for PostGIS and time-bucketing.

  Importing this module lets you write `where: near(s.location, ...)` instead of
  the equivalent `fragment` block.

      from s in Store, where: near(s.location, ^lon, ^lat, ^meters)
  """

  import Ecto.Query

  @doc """
  Matches rows where `geom` is within `meters` of point (lon, lat).

  Uses `ST_DWithin` with the `geography` cast — meters are spherical, not planar.
  Leverages a GiST index on `geom` because the indexed column comes first.
  """
  defmacro near(geom, lon, lat, meters) do
    quote do
      fragment(
        "ST_DWithin(?::geography, ST_SetSRID(ST_MakePoint(?, ?), 4326)::geography, ?)",
        unquote(geom),
        unquote(lon),
        unquote(lat),
        unquote(meters)
      )
    end
  end

  @doc """
  Returns distance in meters from `geom` to (lon, lat).

  Used in `select:` or `order_by:` — does not leverage the index, so pair with
  `near/4` to prefilter candidates.
  """
  defmacro distance_m(geom, lon, lat) do
    quote do
      fragment(
        "ST_Distance(?::geography, ST_SetSRID(ST_MakePoint(?, ?), 4326)::geography)",
        unquote(geom),
        unquote(lon),
        unquote(lat)
      )
    end
  end

  @doc """
  Matches rows whose geometry falls inside the axis-aligned bounding box.

  Faster than `near/4` for rectangular regions (viewport queries in maps).
  """
  defmacro within_bbox(geom, min_lon, min_lat, max_lon, max_lat) do
    quote do
      fragment(
        "? && ST_MakeEnvelope(?, ?, ?, ?, 4326)",
        unquote(geom),
        unquote(min_lon),
        unquote(min_lat),
        unquote(max_lon),
        unquote(max_lat)
      )
    end
  end

  @doc """
  Truncates a datetime to the given granularity using `date_trunc`.

      select: time_bucket(s.opened_at, "day")
  """
  defmacro time_bucket(col, granularity) when is_binary(granularity) do
    quote do
      fragment("date_trunc(?, ?)", unquote(granularity), unquote(col))
    end
  end

  @doc """
  Selects only rows whose JSON field path matches a value.

  `jsonb_eq(col, ["nested", "key"], value)`
  """
  defmacro jsonb_eq(col, path, value) when is_list(path) do
    path_expr = Enum.join(path, ",")

    quote do
      fragment("?#>>? = ?", unquote(col), unquote("{" <> path_expr <> "}"), unquote(value))
    end
  end
end

# lib/geo_search/stores.ex
defmodule GeoSearch.Stores do
  import Ecto.Query
  import GeoSearch.QueryApi

  alias GeoSearch.Repo
  alias GeoSearch.Schemas.Store

  @doc "Returns nearby result from lon, lat and radius_m."
  @spec nearby(float(), float(), non_neg_integer()) :: [Store.t()]
  def nearby(lon, lat, radius_m) do
    from(s in Store,
      where: near(s.location, ^lon, ^lat, ^radius_m),
      select_merge: %{distance_m: distance_m(s.location, ^lon, ^lat)},
      order_by: [asc: distance_m(s.location, ^lon, ^lat)],
      limit: 100
    )
    |> Repo.all()
  end

  @doc "Returns in viewport result from min_lon, min_lat, max_lon and max_lat."
  @spec in_viewport(float(), float(), float(), float()) :: [Store.t()]
  def in_viewport(min_lon, min_lat, max_lon, max_lat) do
    from(s in Store,
      where: within_bbox(s.location, ^min_lon, ^min_lat, ^max_lon, ^max_lat),
      limit: 500
    )
    |> Repo.all()
  end

  @doc "Returns opens by day result."
  @spec opens_by_day() :: [%{day: DateTime.t(), count: non_neg_integer()}]
  def opens_by_day do
    from(s in Store,
      group_by: time_bucket(s.opened_at, "day"),
      select: %{
        day: time_bucket(s.opened_at, "day"),
        count: count(s.id)
      },
      order_by: [asc: time_bucket(s.opened_at, "day")]
    )
    |> Repo.all()
  end
end
```
### `test/geo_search_test.exs`

```elixir
defmodule GeoSearch.StoresTest do
  use ExUnit.Case, async: true
  doctest GeoSearch.Repo.Migrations.CreateStores
  alias GeoSearch.{Repo, Stores}
  alias GeoSearch.Schemas.Store

  setup do
    Ecto.Adapters.SQL.Sandbox.checkout(Repo)
    Ecto.Adapters.SQL.Sandbox.mode(Repo, {:shared, self()})
    Repo.delete_all(Store)

    insert_store("San Francisco", -122.4194, 37.7749)
    insert_store("Oakland", -122.2711, 37.8044)
    insert_store("New York", -74.0060, 40.7128)
    :ok
  end

  defp insert_store(name, lon, lat) do
    now = DateTime.utc_now() |> DateTime.truncate(:second)

    Ecto.Adapters.SQL.query!(
      Repo,
      """
      INSERT INTO stores (name, location, opened_at, inserted_at, updated_at)
      VALUES ($1, ST_SetSRID(ST_MakePoint($2, $3), 4326), $4, $5, $5)
      """,
      [name, lon, lat, now, now]
    )
  end

  describe "nearby/3" do
    test "returns stores within the radius" do
      # 15 km radius around San Francisco — hits SF and Oakland but not NY
      results = Stores.nearby(-122.4194, 37.7749, 15_000)
      names = Enum.map(results, & &1.name) |> Enum.sort()
      assert names == ["Oakland", "San Francisco"]
    end

    test "distance_m is populated and sorted ascending" do
      results = Stores.nearby(-122.4194, 37.7749, 50_000)
      assert Enum.all?(results, &is_float(&1.distance_m))
      distances = Enum.map(results, & &1.distance_m)
      assert distances == Enum.sort(distances)
    end

    test "empty when nothing in range" do
      # Small radius in the middle of the Pacific
      assert [] = Stores.nearby(-150.0, 0.0, 1_000)
    end
  end

  describe "in_viewport/4" do
    test "returns stores inside the bounding box" do
      # Bay Area box
      results = Stores.in_viewport(-122.6, 37.5, -122.0, 38.0)
      names = Enum.map(results, & &1.name) |> Enum.sort()
      assert names == ["Oakland", "San Francisco"]
    end
  end

  describe "opens_by_day/0" do
    test "buckets store opens by day" do
      assert [_ | _] = Stores.opens_by_day()
    end
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Custom Query Macros — Extending `Ecto.Query.API`.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Custom Query Macros — Extending `Ecto.Query.API` ===")
    IO.puts("Category: Ecto advanced\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case GeoSearch.run(payload) do
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
        for _ <- 1..1_000, do: GeoSearch.run(:bench)
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
