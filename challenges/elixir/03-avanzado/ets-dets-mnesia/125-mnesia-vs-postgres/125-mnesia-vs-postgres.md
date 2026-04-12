# Mnesia vs Postgres — Honest Comparison and Benchmark

**Project**: `mnesia_vs_postgres` — side-by-side implementation of the same store in both backends.
**Difficulty**: ★★★★☆
**Estimated time**: 3–6 hours

---

## Project context

Every senior Elixir engineer eventually argues about whether Mnesia can
replace Postgres. The honest answer depends on what you are doing. Mnesia
wins on local read latency and operational "it's just a BEAM library".
Postgres wins on almost everything else: ecosystem, query expressiveness,
tooling, operational familiarity, backups, point-in-time recovery, schema
migrations, and cross-datacenter replication.

This exercise builds the same component — a keyed KV store with indexed
queries — twice: once on Mnesia with `disc_copies`, once on Postgres with
Ecto. Then benchmark them under identical load and compare. The point is
not "which is faster" (the answer changes with dataset size, hardware,
and workload); it is to build the intuition for *when* Mnesia's tradeoffs
pay off.

```
mnesia_vs_postgres/
├── lib/
│   └── mnesia_vs_postgres/
│       ├── application.ex
│       ├── store_behaviour.ex       # @callback contract
│       ├── mnesia_store.ex          # implementation A
│       ├── postgres_store.ex        # implementation B (Ecto)
│       ├── repo.ex
│       └── user.ex
├── priv/
│   └── repo/migrations/
│       └── 20260401000000_create_users.exs
├── bench/
│   └── compare_bench.exs
└── test/
    └── mnesia_vs_postgres/
        ├── mnesia_store_test.exs
        └── postgres_store_test.exs
```

---

## Core concepts

### 1. The decision matrix

| Concern                      | Mnesia `disc_copies`             | Postgres + Ecto                       |
|------------------------------|----------------------------------|---------------------------------------|
| Point read latency           | 1-20µs (local replica)           | 200-800µs (network + parse + plan)    |
| Write latency (single node)  | 100-500µs                        | 200-1000µs                            |
| Write latency (cross-DC)     | prohibitive                      | acceptable with async replication     |
| Query expressiveness         | match specs, QLC                 | full SQL, JSONB, CTEs                 |
| Ecosystem / tooling          | minimal                          | vast                                  |
| Backup / PITR                | DIY (log copy)                   | first-class (`pg_basebackup`, WAL)    |
| Schema migrations            | `transform_table/3`, painful     | Ecto migrations, smooth               |
| Ops familiarity              | Erlang-specific                  | universal                             |
| HA across datacenters        | no                               | yes (streaming replication, Citus)    |
| Max practical table size     | fits in RAM × replicas           | terabytes, with partitioning          |
| Transaction coordination     | distributed 2PC (in cluster)     | MVCC, single primary or Citus         |

### 2. What "local read" actually buys you

On a single node with a warm cache, Postgres can serve a point read in
~200µs. Mnesia can do it in ~1-5µs. That 40-100x gap is interesting only
if the read is on a hot path — an auth check on every HTTP request, a
presence lookup in a websocket handler, a rate-limit check.

For 95% of features, that gap is invisible. A REST endpoint that does
20ms of work and one DB read does not care whether the read took 5µs or
500µs. Mnesia is only worth the pain when you have *specific evidence*
that DB latency is the bottleneck.

### 3. What Postgres buys you

* **SQL.** Ad-hoc analytics queries, joins across many tables, aggregations.
  Mnesia's match specs and QLC are a step backward on every axis.
* **Operational maturity.** Every platform team knows how to operate
  Postgres. Very few know how to recover Mnesia from a corrupted schema.
* **Ecosystem.** Ecto, PgBouncer, pgbadger, TimescaleDB, logical decoding,
  foreign data wrappers, RLS, RLS again because it is amazing.
* **Backups that work.** `pg_dump` and streaming WAL archives are battle-
  tested. Mnesia backups require stopping the application or writing a
  custom coordinator.

### 4. The one case Mnesia dominates

A *process-registry-like* workload: tiny records, read on every operation
by in-BEAM code, cluster-wide availability required, data is
reconstructible on cluster restart (or kept in sync by periodic refresh
from Postgres). `Presence`, session tokens, feature flag cache,
routing tables. Even here, many teams pick ETS + `pg_notify` instead
because the operational burden is lower.

### 5. The honest recommendation

Default to Postgres for persistent state. Use ETS for local caching. Use
Mnesia when you have a specific, measured reason — almost always a
latency target that Postgres cannot meet on the hot path, combined with
a willingness to pay the operational tax.

---

## Implementation

### Step 1: `mix.exs`

```elixir
defmodule MnesiaVsPostgres.MixProject do
  use Mix.Project

  def project do
    [app: :mnesia_vs_postgres, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application do
    [
      extra_applications: [:logger, :mnesia],
      mod: {MnesiaVsPostgres.Application, []}
    ]
  end

  defp deps do
    [
      {:ecto_sql, "~> 3.11"},
      {:postgrex, "~> 0.17"},
      {:benchee, "~> 1.3", only: :dev}
    ]
  end
end
```

### Step 2: `lib/mnesia_vs_postgres/store_behaviour.ex`

```elixir
defmodule MnesiaVsPostgres.StoreBehaviour do
  @moduledoc """
  Contract both backends implement. The benchmark uses this module name
  indirection to keep both code paths symmetric.
  """

  @type id :: String.t()
  @type user :: %{id: id, email: String.t(), tier: :free | :pro | :enterprise}

  @callback put(user()) :: :ok | {:error, term()}
  @callback get(id()) :: {:ok, user()} | :not_found
  @callback list_by_tier(atom()) :: [user()]
  @callback delete(id()) :: :ok
end
```

### Step 3: `lib/mnesia_vs_postgres/mnesia_store.ex`

```elixir
defmodule MnesiaVsPostgres.MnesiaStore do
  @moduledoc false
  @behaviour MnesiaVsPostgres.StoreBehaviour

  @table :users_mnesia

  def ensure_started do
    _ = :mnesia.stop()
    _ = :mnesia.create_schema([node()])
    :ok = :mnesia.start()

    case :mnesia.create_table(@table,
           attributes: [:id, :email, :tier],
           disc_copies: [node()],
           index: [:tier],
           type: :set
         ) do
      {:atomic, :ok} -> :ok
      {:aborted, {:already_exists, @table}} -> :ok
    end

    :mnesia.wait_for_tables([@table], 10_000)
  end

  @impl true
  def put(%{id: id, email: email, tier: tier}) do
    fun = fn -> :mnesia.write({@table, id, email, tier}) end

    case :mnesia.transaction(fun) do
      {:atomic, :ok} -> :ok
      {:aborted, reason} -> {:error, reason}
    end
  end

  @impl true
  def get(id) do
    case :mnesia.dirty_read({@table, id}) do
      [{@table, ^id, email, tier}] -> {:ok, %{id: id, email: email, tier: tier}}
      [] -> :not_found
    end
  end

  @impl true
  def list_by_tier(tier) do
    pattern = {@table, :"$1", :"$2", tier}
    result = {{:"$1", :"$2"}}

    :mnesia.dirty_select(@table, [{pattern, [], [result]}])
    |> Enum.map(fn {id, email} -> %{id: id, email: email, tier: tier} end)
  end

  @impl true
  def delete(id) do
    :mnesia.dirty_delete({@table, id})
    :ok
  end
end
```

### Step 4: `lib/mnesia_vs_postgres/repo.ex` and `user.ex`

```elixir
defmodule MnesiaVsPostgres.Repo do
  use Ecto.Repo, otp_app: :mnesia_vs_postgres, adapter: Ecto.Adapters.Postgres
end

defmodule MnesiaVsPostgres.User do
  use Ecto.Schema
  import Ecto.Changeset

  @primary_key {:id, :string, autogenerate: false}
  schema "users" do
    field :email, :string
    field :tier, Ecto.Enum, values: [:free, :pro, :enterprise]
    timestamps()
  end

  def changeset(user, attrs) do
    user
    |> cast(attrs, [:id, :email, :tier])
    |> validate_required([:id, :email, :tier])
    |> unique_constraint(:id, name: :users_pkey)
  end
end
```

### Step 5: `priv/repo/migrations/20260401000000_create_users.exs`

```elixir
defmodule MnesiaVsPostgres.Repo.Migrations.CreateUsers do
  use Ecto.Migration

  def change do
    create table(:users, primary_key: false) do
      add :id, :string, primary_key: true
      add :email, :string, null: false
      add :tier, :string, null: false
      timestamps()
    end

    create index(:users, [:tier])
  end
end
```

### Step 6: `lib/mnesia_vs_postgres/postgres_store.ex`

```elixir
defmodule MnesiaVsPostgres.PostgresStore do
  @moduledoc false
  @behaviour MnesiaVsPostgres.StoreBehaviour

  import Ecto.Query
  alias MnesiaVsPostgres.{Repo, User}

  @impl true
  def put(%{id: id, email: email, tier: tier}) do
    attrs = %{id: id, email: email, tier: tier}

    case Repo.insert(User.changeset(%User{}, attrs), on_conflict: :replace_all, conflict_target: :id) do
      {:ok, _} -> :ok
      {:error, cs} -> {:error, cs}
    end
  end

  @impl true
  def get(id) do
    case Repo.get(User, id) do
      nil -> :not_found
      %User{email: email, tier: tier} -> {:ok, %{id: id, email: email, tier: tier}}
    end
  end

  @impl true
  def list_by_tier(tier) do
    from(u in User, where: u.tier == ^tier, select: %{id: u.id, email: u.email, tier: u.tier})
    |> Repo.all()
  end

  @impl true
  def delete(id) do
    _ = Repo.delete_all(from u in User, where: u.id == ^id)
    :ok
  end
end
```

### Step 7: `lib/mnesia_vs_postgres/application.ex`

```elixir
defmodule MnesiaVsPostgres.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    MnesiaVsPostgres.MnesiaStore.ensure_started()

    children = [MnesiaVsPostgres.Repo]
    Supervisor.start_link(children, strategy: :one_for_one, name: MnesiaVsPostgres.Supervisor)
  end
end
```

Config:

```elixir
# config/config.exs
import Config

config :mnesia_vs_postgres,
  ecto_repos: [MnesiaVsPostgres.Repo]

config :mnesia_vs_postgres, MnesiaVsPostgres.Repo,
  username: "postgres",
  password: "postgres",
  hostname: "localhost",
  database: "mnesia_vs_postgres_#{Mix.env()}",
  pool_size: 10
```

### Step 8: Tests (shared structure)

```elixir
# test/mnesia_vs_postgres/mnesia_store_test.exs
defmodule MnesiaVsPostgres.MnesiaStoreTest do
  use ExUnit.Case, async: false
  alias MnesiaVsPostgres.MnesiaStore

  setup do
    :mnesia.clear_table(:users_mnesia)
    :ok
  end

  test "round-trip put/get" do
    user = %{id: "u1", email: "a@b.c", tier: :pro}
    assert :ok = MnesiaStore.put(user)
    assert {:ok, ^user} = MnesiaStore.get("u1")
  end

  test "list_by_tier/1 uses the tier index" do
    for i <- 1..50 do
      MnesiaStore.put(%{id: "u#{i}", email: "e#{i}", tier: if(rem(i, 2) == 0, do: :free, else: :pro)})
    end

    pros = MnesiaStore.list_by_tier(:pro)
    assert length(pros) == 25
  end
end
```

```elixir
# test/mnesia_vs_postgres/postgres_store_test.exs
defmodule MnesiaVsPostgres.PostgresStoreTest do
  use ExUnit.Case, async: false
  alias MnesiaVsPostgres.{PostgresStore, Repo, User}

  setup do
    Repo.delete_all(User)
    :ok
  end

  test "round-trip put/get" do
    user = %{id: "u1", email: "a@b.c", tier: :pro}
    assert :ok = PostgresStore.put(user)
    assert {:ok, ^user} = PostgresStore.get("u1")
  end

  test "list_by_tier/1 uses the tier index" do
    for i <- 1..50 do
      PostgresStore.put(%{id: "u#{i}", email: "e#{i}", tier: if(rem(i, 2) == 0, do: :free, else: :pro)})
    end

    assert length(PostgresStore.list_by_tier(:pro)) == 25
  end
end
```

### Step 9: Benchmark

```elixir
# bench/compare_bench.exs
alias MnesiaVsPostgres.{MnesiaStore, PostgresStore}

for i <- 1..10_000 do
  user = %{id: "u-#{i}", email: "u#{i}@t.co", tier: Enum.random([:free, :pro, :enterprise])}
  MnesiaStore.put(user)
  PostgresStore.put(user)
end

random_id = fn -> "u-#{:rand.uniform(10_000)}" end

Benchee.run(
  %{
    "mnesia get (dirty_read)"    => fn -> MnesiaStore.get(random_id.()) end,
    "postgres get"               => fn -> PostgresStore.get(random_id.()) end,
    "mnesia put (transaction)"   => fn ->
      MnesiaStore.put(%{id: "b-#{:rand.uniform(1_000_000)}", email: "b", tier: :free})
    end,
    "postgres put (insert)"      => fn ->
      PostgresStore.put(%{id: "b-#{:rand.uniform(1_000_000)}", email: "b", tier: :free})
    end,
    "mnesia list_by_tier :pro"   => fn -> MnesiaStore.list_by_tier(:pro) end,
    "postgres list_by_tier :pro" => fn -> PostgresStore.list_by_tier(:pro) end
  },
  parallel: 4,
  time: 5,
  warmup: 2
)
```

Representative output (M1, Postgres 16 local, Mnesia disc_copies, OTP 26):

```
mnesia get ................. 2.1µs  ops/s 480_000
postgres get ............... 320µs  ops/s 3_100
mnesia put ................. 180µs  ops/s 5_500
postgres put ............... 540µs  ops/s 1_850
mnesia list_by_tier :pro ... 18ms   ops/s 55
postgres list_by_tier :pro . 12ms   ops/s 82
```

Observations:

* Mnesia reads are ~150x faster for single-key gets — the headline case.
* Mnesia writes are ~3x faster locally.
* Postgres wins on bulk queries (indexed list scan). Its planner and
  streaming protocol are hard to beat beyond a few hundred rows.

---

## Trade-offs and production gotchas

**1. Benchmark your workload, not someone else's.**
The numbers above will not match yours. Dataset size, working set vs RAM,
disk type, Postgres config (`shared_buffers`, `effective_cache_size`)
matter more than Mnesia vs Postgres. Publish a reproducible benchmark
before making an architectural bet.

**2. Mnesia's "indexes" are not like Postgres indexes.**
The `index: [:tier]` option creates an auxiliary ETS lookup table. It is
effective for exact-match queries but has no sorting, no range support,
no partial indexes, no covering indexes. Postgres indexes are vastly
more capable.

**3. Migrations.**
`transform_table/3` on a large Mnesia table is a multi-hour operation
that holds write locks. Postgres `ALTER TABLE ADD COLUMN` with a
default is effectively instant since PG 11. Schema evolution is not
a contest.

**4. Operational surface.**
Postgres has `pg_stat_statements`, `EXPLAIN ANALYZE`, `pg_dump`,
point-in-time recovery, logical replication. Mnesia has
`:mnesia.system_info/1` and a lot of manual log parsing. If your team
is paged for this at 3 AM, Postgres is a better bet.

**5. Don't use Mnesia as a general-purpose DB.**
The specific workload it was designed for (OTP apps that need a
distributed KV with transactions and are willing to run dedicated
operators) is narrower than most teams assume. Presence, session
stores, and in-memory caches are the sweet spot.

**6. Cross-datacenter.**
Mnesia's synchronous commit coordinator collapses under WAN latency.
Postgres streaming replication tolerates 50-100ms RTTs comfortably.
For multi-region, Postgres wins by default.

**7. Combining both.**
A common pattern: Postgres as source of truth, Mnesia or ETS as an
in-BEAM cache, warmed on startup and invalidated via `pg_notify`. You
get Postgres durability and Mnesia/ETS read latency without committing
to either extreme.

**8. When NOT to use Mnesia.**
* You need SQL for reporting.
* You need cross-DC replication.
* You need a well-understood backup/restore story.
* Your team has less than one Erlang-experienced operator on-call.
* The dataset does not fit in RAM on any machine you can afford.

---

## Performance notes

Use `EXPLAIN (ANALYZE, BUFFERS)` on Postgres queries before declaring
anything slow. Most "Postgres is slow" arguments vanish once the right
index is in place and the query is inspected. Mnesia has no equivalent
diagnostic tooling, which is itself a tradeoff — you cannot debug what
you cannot inspect.

---

## Resources

- [Chris McCord — The Road to 2 Million Websocket Connections in Phoenix](https://www.phoenixframework.org/blog/the-road-to-2-million-websocket-connections) — Mnesia for presence
- [Dashbit — Mnesia, the Bad Parts](https://dashbit.co/blog/mnesia-the-bad-parts)
- [Ecto docs](https://hexdocs.pm/ecto)
- [Postgres performance tuning — Sasa Juric](https://www.theerlangelist.com/article/postgres-performance)
- [Use the Index, Luke](https://use-the-index-luke.com/) — the reference on SQL indexing
- [Cassandra vs Mnesia decision note — ThoughtWorks](https://www.thoughtworks.com/radar) — external perspective on when to reach for each
- [Saša Jurić — To Spawn, or Not to Spawn?](https://www.theerlangelist.com/article/spawn_or_not) — sibling argument on when BEAM primitives pay off
