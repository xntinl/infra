# Custom Query Macros — Extending `Ecto.Query.API`

**Project**: `geo_search` — custom Ecto fragments wrapped as reusable macros (PostGIS distance, time buckets).

---

## Project context

Queries that use Postgres-specific functions (PostGIS, `date_trunc`, JSONB path) fill the
codebase with `fragment("...")` calls that look like SQL in disguise. Extracting them
into named macros turns:

```elixir
from s in Store,
  where: fragment("ST_DWithin(?::geography, ST_SetSRID(ST_MakePoint(?, ?), 4326)::geography, ?)",
                  s.location, ^lon, ^lat, ^radius)
```

into:

```elixir
from s in Store, where: near(s.location, ^lon, ^lat, ^radius)
```

The macro is compiled to exactly the same fragment, but the calling site is readable and
the fragment string exists in one place.

```
geo_search/
├── lib/
│   └── geo_search/
│       ├── application.ex
│       ├── repo.ex
│       ├── query_api.ex              # custom macros
│       ├── stores.ex                  # context using them
│       └── schemas/
│           └── store.ex
├── priv/repo/migrations/
├── test/geo_search/
│   └── stores_test.exs
├── bench/stores_bench.exs
└── mix.exs
```

---

## Core concepts

### 1. Ecto query macros are compile-time transformations

When you write `from s in Store, where: s.x == 1`, the AST of the `where:` clause is
walked by `Ecto.Query.API` at compile time. Custom macros plug into the same mechanism —
they take quoted expressions and return `fragment(...)` calls (which Ecto knows how to
handle).

### 2. The `fragment/2` workhorse

`fragment("SQL with ? placeholders", arg1, arg2, ...)` is the escape hatch into raw SQL.
Everything inside a macro ultimately becomes one or more `fragment/2` calls.

### 3. Macro hygiene

Query macros must be usable inside `where:`, `select:`, `order_by:` — all of which run
inside Ecto's query expression context. That means:

- Use `defmacro` (not `def`).
- Return a `quote do: ... end` block that contains a `fragment/2` call.
- The caller imports your module; no module-name prefix.

### 4. Multi-arity and shorthand

A well-designed API exposes both atomic building blocks and common shorthands:

```elixir
dwithin(point_a, point_b, meters)          # raw primitive
near(col, lon, lat, meters)                # common case
distance_m(col, lon, lat)                  # raw distance select
within_bbox(col, min_lon, min_lat, max_lon, max_lat)
time_bucket(col, "1 hour")                 # date_trunc
```

### 5. Indexed-friendly forms

Fragments must be written so they match the index. `ST_DWithin` on `geography` uses the
GiST index only when the operand order is `(indexed_column, literal_geometry)`. A macro
with a fixed argument order ensures every call site is index-friendly.

---

## Design decisions

- **Option A — inline fragments at each call site**: simplest, but fragment strings
  drift apart across files.
- **Option B — custom query macros in one module**: single source of truth, readable
  call sites. Pros: composable, typo-proof. Cons: macro API design takes thought.

We use **Option B**. Every fragment lives in `QueryApi`; call sites `import QueryApi`.

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

### Step 1: Migration — PostGIS extension

```elixir
# priv/repo/migrations/20260101000000_create_stores.exs
defmodule GeoSearch.Repo.Migrations.CreateStores do
  use Ecto.Migration

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

  def down do
    drop table(:stores)
    execute "DROP EXTENSION postgis"
  end
end
```

### Step 2: Schema with a custom type

```elixir
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

  def changeset(store, attrs) do
    cast(store, attrs, [:name, :location, :opened_at])
    |> validate_required([:name, :location])
  end
end
```

We use `:any` here as a pragmatic placeholder; a production system pairs with `geo_postgis`
library for proper Ecto types. The macros below do not depend on that type — they operate
on fragment expressions.

### Step 3: The macro module

```elixir
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
```

### Step 4: Context using the macros

```elixir
# lib/geo_search/stores.ex
defmodule GeoSearch.Stores do
  import Ecto.Query
  import GeoSearch.QueryApi

  alias GeoSearch.Repo
  alias GeoSearch.Schemas.Store

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

  @spec in_viewport(float(), float(), float(), float()) :: [Store.t()]
  def in_viewport(min_lon, min_lat, max_lon, max_lat) do
    from(s in Store,
      where: within_bbox(s.location, ^min_lon, ^min_lat, ^max_lon, ^max_lat),
      limit: 500
    )
    |> Repo.all()
  end

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

---

## Why this works

- Macros expand at compile time into exactly the fragment expression you would have
  written. Runtime cost is zero.
- Argument order is fixed in the macro so call sites always produce index-friendly SQL.
  Callers cannot accidentally reverse `geom` and the literal point.
- `distance_m/3` is both a `select` and an `order_by`; the macro works in both contexts
  because it returns a fragment expression.
- `time_bucket/2` accepts only a binary literal granularity. Passing a variable would let
  SQL injection in (since `date_trunc('day', x)` takes the unit as a string). The
  `when is_binary(granularity)` guard on the macro enforces literal-only at compile time.

---

## Data flow — `nearby/3`

```
nearby(lon=-122.4, lat=37.7, radius_m=1000)
    │
    ▼
macro expansion:
  WHERE ST_DWithin(s.location::geography,
                   ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography,
                   $3)
  SELECT ..., ST_Distance(...) AS distance_m
  ORDER BY ST_Distance(...)
  LIMIT 100
    │
    ▼
Postgres planner: GiST index scan via ST_DWithin, then re-compute ST_Distance for sort
    │
    ▼
[%Store{distance_m: 123.4}, ...]
```

---

## Tests

```elixir
# test/geo_search/stores_test.exs
defmodule GeoSearch.StoresTest do
  use ExUnit.Case, async: false
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

---

## Benchmark

```elixir
# bench/stores_bench.exs
alias GeoSearch.{Repo, Stores}
alias GeoSearch.Schemas.Store

Repo.delete_all(Store)
now = DateTime.utc_now() |> DateTime.truncate(:second)

for _ <- 1..10_000 do
  lon = :rand.uniform() * 360 - 180
  lat = :rand.uniform() * 180 - 90

  Ecto.Adapters.SQL.query!(Repo,
    """
    INSERT INTO stores (name, location, opened_at, inserted_at, updated_at)
    VALUES ('s', ST_SetSRID(ST_MakePoint($1, $2), 4326), $3, $3, $3)
    """, [lon, lat, now])
end

Benchee.run(
  %{
    "nearby 1km SF"   => fn -> Stores.nearby(-122.4, 37.7, 1_000) end,
    "viewport bay"    => fn -> Stores.in_viewport(-122.6, 37.5, -122.0, 38.0) end,
    "opens_by_day"    => fn -> Stores.opens_by_day() end
  },
  time: 5, warmup: 2
)
```

**Target**: `nearby 1km SF` under 10 ms with 10k stores and GiST index. `viewport bay`
under 15 ms. If `nearby` is > 100 ms, `EXPLAIN` the query — the GiST index should be
used via `ST_DWithin`.

---

## Trade-offs and production gotchas

**1. Macros run at compile time.** Errors surface with confusing stack traces pointing
into the macro body. Write targeted unit tests that compile and execute the macro on a
known schema.

**2. `fragment/2` is opaque to Ecto's type-checker.** A typo in the SQL surfaces as a
Postgrex error at runtime. Keep macros small so the fragment string is obvious.

**3. Argument order matters for index usage.** `ST_DWithin(geom_col, literal, m)` uses
the index; swapping `(literal, geom_col, m)` does not. The macro enforces the correct
order at every call site.

**4. Macros imported everywhere pollute the namespace.** `near/4` might collide with an
app-defined function. Scope imports to contexts: `import QueryApi, only: [near: 4]`.

**5. `time_bucket/2` with a dynamic granularity is insecure.** Since the unit becomes
part of the SQL, accepting `user_input` would allow SQL injection. The macro's guard
`when is_binary(granularity)` alone is not enough — the macro is always called with a
literal. At runtime, if you need variable granularity, build a whitelist in Elixir.

**6. When NOT to write a macro.** For a one-off fragment, inline it. Macros earn their
keep when used 3+ times or when argument order is load-bearing for correctness.

---

## Reflection

Your `near/4` macro is used in 15 places. Product asks for "near me but only open now"
— adding an `AND opened_at <= now()` clause to every call. You have two options: add a
parameter to `near/4` for a time predicate, or introduce a new macro `near_open/5`. What
is the design principle: grow existing macros or keep them small and compose at the
call site? Where does the break-even land when you have 5 orthogonal filters?

---

## Resources

- [`Ecto.Query.API`](https://hexdocs.pm/ecto/Ecto.Query.API.html) — all built-in query macros
- [`fragment/2` documentation](https://hexdocs.pm/ecto/Ecto.Query.API.html#fragment/1)
- [PostGIS reference](https://postgis.net/docs/reference.html)
- [`geo_postgis`](https://github.com/bryanjos/geo_postgis) — production PostGIS types for Ecto
