# Commanded projections and read models

**Project**: `commanded_projections` — read-side Ecto projections built from the event stream.

---

## Project context

You are building a fintech ledger using event sourcing: every deposit, withdrawal, and closure is appended as an event to an event store. Now the product team needs a dashboard that shows, for each account, the current balance, the last 20 transactions, and daily aggregated totals per customer. The aggregate cannot serve these queries — it holds only one account's state and hitting it means a GenServer round-trip that competes with write traffic.

The solution is **projections**: background processes that consume the event stream and write denormalized rows into Postgres. Queries hit Postgres, never the aggregate. This is the "Q" in CQRS: query models tailored to the read patterns, independent of the write model.

You will build three projections on top of [commanded_ecto_projections](https://github.com/commanded/commanded-ecto-projections):

1. `AccountBalanceProjection` — one row per account with current balance and status.
2. `TransactionLogProjection` — append-only log, one row per movement.
3. `DailyTotalsProjection` — aggregated per-day totals, upsert-based.

```
commanded_projections/
├── lib/
│   └── commanded_projections/
│       ├── application.ex
│       ├── repo.ex
│       ├── projections/
│       │   ├── account_balance.ex
│       │   ├── transaction_log.ex
│       │   └── daily_totals.ex
│       └── projectors/
│           ├── account_balance_projector.ex
│           ├── transaction_log_projector.ex
│           └── daily_totals_projector.ex
├── priv/
│   └── repo/migrations/
│       ├── 001_create_account_balances.exs
│       ├── 002_create_transaction_log.exs
│       └── 003_create_daily_totals.exs
├── test/
│   └── projectors_test.exs
└── mix.exs
```

---

## Why this approach and not alternatives

Alternatives considered and discarded:

- **Hand-rolled equivalent**: reinvents primitives the BEAM/ecosystem already provides; high risk of subtle bugs around concurrency, timeouts, or failure propagation.
- **External service (e.g. Redis, sidecar)**: adds a network hop and an extra failure domain for a problem the VM can solve in-process with lower latency.
- **Heavier framework abstraction**: couples the module to a framework lifecycle and makes local reasoning/testing harder.

The chosen approach stays inside the BEAM, uses idiomatic OTP primitives, and keeps the contract small.

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
### 1. Eventually consistent reads — the unavoidable trade-off

CQRS decouples write and read models. The price is that reads lag the writes by one event-handler tick (milliseconds in practice, seconds under back-pressure). If your UI calls `Ledger.dispatch(deposit)` and immediately queries the balance, it may see the old value.

```
 Write side:   Command ──▶ Aggregate ──▶ Event append (sync, ~5ms)
                                              │
                                              ▼
 Read  side:   Projector consumes event ──▶ Ecto.Repo.insert (async)
                                              │
                                              ▼
                                 UI query sees new row  (after ~1–50ms)
```

Design the UI around this: optimistic updates, "processing" states, or reading-from-command-result. Do not pretend it is synchronous.

### 2. Projector = `Commanded.Event.Handler` + Ecto writes

Every projector is a GenServer-backed event handler. Commanded tracks its position in the event store (the **subscription checkpoint**) so restarts resume where they left off. The `Ecto.Projection` macro bundles checkpoint persistence with the Ecto transaction, guaranteeing **exactly-once side effects** inside the read-model database.

### 3. Idempotency by construction

Projectors MAY be invoked more than once per event on crash/retry paths. Two techniques make them safe:

- **Upsert by natural key** — e.g. `account_id` as primary key for `AccountBalanceProjection`. Replays overwrite the same row.
- **Atomic checkpoint + write** — `commanded_ecto_projections` wraps the projection update AND checkpoint advance in one transaction. If either fails, both roll back.

### 4. One projector per read model

Do NOT fan out multiple read models from one projector. Each projection should live in its own process with its own checkpoint. If you rebuild one (e.g., change `transaction_log` schema), you can re-stream the events into just that projection without touching the others.

### 5. Rebuild strategy — the projector's superpower

Because events are the source of truth, you can drop the projection table, reset the subscription, and replay from event 0 to rebuild. This is how you introduce new read models for old data: deploy the projector, and it catches up from the beginning.

```
 Event store (immutable):    [e1, e2, e3, ... eN]
                               │
           ┌───────────────────┼───────────────────┐
           ▼                   ▼                   ▼
       balances            tx_log            daily_totals
     (can rebuild)      (can rebuild)       (can rebuild)
```

---

## Design decisions

**Option A — naive/simple approach**
- Pros: minimal code, easy to reason about.
- Cons: breaks under load, lacks observability, hard to evolve.

**Option B — the approach used here** (chosen)
- Pros: production-grade, handles edge cases, testable boundaries.
- Cons: more moving parts, requires understanding of the BEAM primitives involved.

→ Chose **B** because correctness under concurrency and failure modes outweighs the extra surface area.

## Implementation

### Step 1: `mix.exs`

**Objective**: Add Commanded, Ecto, and commanded_ecto_projections for event-driven read-model synchronization.

```elixir
defmodule CommandedProjections.MixProject do
  use Mix.Project

  def project do
    [app: :commanded_projections, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application do
    [extra_applications: [:logger], mod: {CommandedProjections.Application, []}]
  end

  defp deps do
    [
      {:commanded, "~> 1.4"},
      {:commanded_ecto_projections, "~> 1.4"},
      {:ecto_sql, "~> 3.11"},
      {:postgrex, "~> 0.17"},
      {:jason, "~> 1.4"}
    ]
  end
end
```

### Step 2: Repo — `lib/commanded_projections/repo.ex`

**Objective**: Define Ecto Repo and Commanded configuration to separate event write-store from Postgres read-models.

```elixir
defmodule CommandedProjections.Repo do
  use Ecto.Repo,
    otp_app: :commanded_projections,
    adapter: Ecto.Adapters.Postgres
end
```

Configure in `config/config.exs`:

```elixir
import Config

config :commanded_projections, CommandedProjections.Repo,
  database: "ledger_read",
  hostname: "localhost",
  username: "postgres",
  password: "postgres",
  pool_size: 10

config :commanded_projections, ecto_repos: [CommandedProjections.Repo]

config :commanded_projections, CommandedProjections.App,
  event_store: [
    adapter: Commanded.EventStore.Adapters.InMemory,
    serializer: Commanded.Serialization.JsonSerializer
  ]
```

### Step 3: Migrations — `priv/repo/migrations/`

**Objective**: Create account_balances, transaction_log, and daily_totals tables with projection_versions tracking.

```elixir
# 001_create_account_balances.exs
defmodule CommandedProjections.Repo.Migrations.CreateAccountBalances do
  use Ecto.Migration

  def change do
    create table(:account_balances, primary_key: false) do
      add :account_id, :string, primary_key: true
      add :owner, :string, null: false
      add :balance, :bigint, null: false, default: 0
      add :status, :string, null: false
      timestamps()
    end

    create table(:projection_versions, primary_key: false) do
      add :projection_name, :string, primary_key: true
      add :last_seen_event_number, :bigint, null: false, default: 0
    end
  end
end

# 002_create_transaction_log.exs
defmodule CommandedProjections.Repo.Migrations.CreateTransactionLog do
  use Ecto.Migration

  def change do
    create table(:transaction_log) do
      add :account_id, :string, null: false
      add :kind, :string, null: false
      add :amount, :bigint, null: false
      add :balance_after, :bigint, null: false
      add :occurred_at, :utc_datetime_usec, null: false
    end

    create index(:transaction_log, [:account_id, :occurred_at])
  end
end

# 003_create_daily_totals.exs
defmodule CommandedProjections.Repo.Migrations.CreateDailyTotals do
  use Ecto.Migration

  def change do
    create table(:daily_totals, primary_key: false) do
      add :account_id, :string, primary_key: true
      add :day, :date, primary_key: true
      add :deposits_total, :bigint, null: false, default: 0
      add :withdrawals_total, :bigint, null: false, default: 0
    end
  end
end
```

### Step 4: Ecto schemas — `lib/commanded_projections/projections/*.ex`

**Objective**: Map read-model tables to Ecto schemas to enable ORM queries against projected state.

```elixir
defmodule CommandedProjections.Projections.AccountBalance do
  use Ecto.Schema

  @primary_key {:account_id, :string, []}
  schema "account_balances" do
    field :owner, :string
    field :balance, :integer
    field :status, :string
    timestamps()
  end
end

defmodule CommandedProjections.Projections.TransactionLog do
  use Ecto.Schema

  schema "transaction_log" do
    field :account_id, :string
    field :kind, :string
    field :amount, :integer
    field :balance_after, :integer
    field :occurred_at, :utc_datetime_usec
  end
end

defmodule CommandedProjections.Projections.DailyTotals do
  use Ecto.Schema

  @primary_key false
  schema "daily_totals" do
    field :account_id, :string, primary_key: true
    field :day, :date, primary_key: true
    field :deposits_total, :integer
    field :withdrawals_total, :integer
  end
end
```

### Step 5: The projectors

**Objective**: Implement: The projectors.

```elixir
# lib/commanded_projections/projectors/account_balance_projector.ex
defmodule CommandedProjections.Projectors.AccountBalance do
  use Commanded.Projections.Ecto,
    application: CommandedProjections.App,
    repo: CommandedProjections.Repo,
    name: "account_balance_projector"

  alias CommandedProjections.Projections.AccountBalance
  alias CommandedAggregates.Events.{AccountOpened, MoneyDeposited, MoneyWithdrawn, AccountClosed}

  project(%AccountOpened{} = e, _metadata, fn multi ->
    Ecto.Multi.insert(
      multi,
      :account,
      %AccountBalance{
        account_id: e.account_id,
        owner: e.owner,
        balance: 0,
        status: "open"
      }
    )
  end)

  project(%MoneyDeposited{} = e, _md, fn multi ->
    Ecto.Multi.update_all(
      multi,
      :account,
      from(a in AccountBalance, where: a.account_id == ^e.account_id),
      set: [balance: e.new_balance, updated_at: DateTime.utc_now()]
    )
  end)

  project(%MoneyWithdrawn{} = e, _md, fn multi ->
    Ecto.Multi.update_all(
      multi,
      :account,
      from(a in AccountBalance, where: a.account_id == ^e.account_id),
      set: [balance: e.new_balance, updated_at: DateTime.utc_now()]
    )
  end)

  project(%AccountClosed{} = e, _md, fn multi ->
    Ecto.Multi.update_all(
      multi,
      :account,
      from(a in AccountBalance, where: a.account_id == ^e.account_id),
      set: [status: "closed", updated_at: DateTime.utc_now()]
    )
  end)

  import Ecto.Query
end

# lib/commanded_projections/projectors/transaction_log_projector.ex
defmodule CommandedProjections.Projectors.TransactionLog do
  use Commanded.Projections.Ecto,
    application: CommandedProjections.App,
    repo: CommandedProjections.Repo,
    name: "transaction_log_projector"

  alias CommandedProjections.Projections.TransactionLog
  alias CommandedAggregates.Events.{MoneyDeposited, MoneyWithdrawn}

  project(%MoneyDeposited{} = e, _md, fn multi ->
    Ecto.Multi.insert(multi, :tx, %TransactionLog{
      account_id: e.account_id,
      kind: "deposit",
      amount: e.amount,
      balance_after: e.new_balance,
      occurred_at: e.deposited_at
    })
  end)

  project(%MoneyWithdrawn{} = e, _md, fn multi ->
    Ecto.Multi.insert(multi, :tx, %TransactionLog{
      account_id: e.account_id,
      kind: "withdrawal",
      amount: e.amount,
      balance_after: e.new_balance,
      occurred_at: e.withdrawn_at
    })
  end)
end

# lib/commanded_projections/projectors/daily_totals_projector.ex
defmodule CommandedProjections.Projectors.DailyTotals do
  use Commanded.Projections.Ecto,
    application: CommandedProjections.App,
    repo: CommandedProjections.Repo,
    name: "daily_totals_projector"

  alias CommandedProjections.Projections.DailyTotals
  alias CommandedAggregates.Events.{MoneyDeposited, MoneyWithdrawn}

  project(%MoneyDeposited{} = e, _md, fn multi ->
    upsert_totals(multi, e.account_id, DateTime.to_date(e.deposited_at),
      deposits_delta: e.amount,
      withdrawals_delta: 0
    )
  end)

  project(%MoneyWithdrawn{} = e, _md, fn multi ->
    upsert_totals(multi, e.account_id, DateTime.to_date(e.withdrawn_at),
      deposits_delta: 0,
      withdrawals_delta: e.amount
    )
  end)

  defp upsert_totals(multi, account_id, day, deposits_delta: dd, withdrawals_delta: wd) do
    row = %DailyTotals{
      account_id: account_id,
      day: day,
      deposits_total: dd,
      withdrawals_total: wd
    }

    Ecto.Multi.insert(multi, {:daily, account_id, day}, row,
      on_conflict: [inc: [deposits_total: dd, withdrawals_total: wd]],
      conflict_target: [:account_id, :day]
    )
  end
end
```

### Step 6: Supervision — `lib/commanded_projections/application.ex`

**Objective**: Wire the supervision tree: Supervision — `lib/commanded_projections/application.ex`.

```elixir
defmodule CommandedProjections.App do
  use Commanded.Application, otp_app: :commanded_projections
end

defmodule CommandedProjections.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      CommandedProjections.Repo,
      CommandedProjections.App,
      CommandedProjections.Projectors.AccountBalance,
      CommandedProjections.Projectors.TransactionLog,
      CommandedProjections.Projectors.DailyTotals
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: CommandedProjections.Supervisor)
  end
end
```

### Step 7: Tests — `test/projectors_test.exs`

**Objective**: Provide tests that exercise: Tests — `test/projectors_test.exs`.

```elixir
defmodule CommandedProjections.ProjectorsTest do
  use ExUnit.Case

  alias CommandedProjections.Repo
  alias CommandedProjections.Projections.{AccountBalance, TransactionLog, DailyTotals}
  alias CommandedAggregates.Events.{AccountOpened, MoneyDeposited, MoneyWithdrawn}

  setup do
    :ok = Ecto.Adapters.SQL.Sandbox.checkout(Repo)
    Ecto.Adapters.SQL.Sandbox.mode(Repo, {:shared, self()})
    :ok
  end

  describe "CommandedProjections.Projectors" do
    test "AccountBalance projector inserts on open and updates on deposit" do
      account_id = "acc-" <> Integer.to_string(System.unique_integer([:positive]))

      publish_events([
        %AccountOpened{account_id: account_id, owner: "Alice", overdraft_limit: 0},
        %MoneyDeposited{
          account_id: account_id,
          amount: 500,
          new_balance: 500,
          deposited_at: DateTime.utc_now()
        }
      ])

      wait_for_projection(fn ->
        Repo.get(AccountBalance, account_id)
      end)

      row = Repo.get(AccountBalance, account_id)
      assert row.balance == 500
      assert row.status == "open"
    end

    test "TransactionLog appends one row per money event" do
      account_id = "acc-tx-" <> Integer.to_string(System.unique_integer([:positive]))

      publish_events([
        %AccountOpened{account_id: account_id, owner: "Bob", overdraft_limit: 0},
        %MoneyDeposited{account_id: account_id, amount: 100, new_balance: 100, deposited_at: DateTime.utc_now()},
        %MoneyWithdrawn{account_id: account_id, amount: 30, new_balance: 70, withdrawn_at: DateTime.utc_now()}
      ])

      wait_for_projection(fn ->
        import Ecto.Query
        Repo.aggregate(from(t in TransactionLog, where: t.account_id == ^account_id), :count)
        |> case do
          n when n >= 2 -> :ok
          _ -> nil
        end
      end)
    end
  end

  # ---- helpers ----

  defp publish_events(events) do
    # Test support — publish events directly to the app's event store.
    # In production, events arrive via CommandedAggregates.App.dispatch/1.
    for e <- events do
      CommandedProjections.App.event_store()
      |> elem(0)
      |> send({:publish, e})
    end

    :ok
  end

  defp wait_for_projection(fun, attempts \\ 50) do
    case fun.() do
      nil when attempts > 0 ->
        Process.sleep(20)
        wait_for_projection(fun, attempts - 1)

      other ->
        other
    end
  end
end
```

### Step 8: Run

**Objective**: Verify the implementation by running the test suite.

```bash
mix ecto.create
mix ecto.migrate
mix test
```

---


### Why this works

The design leans on BEAM guarantees (process isolation, mailbox ordering, supervisor restarts) and pushes invariants to the boundaries of each module. State transitions are explicit, failure modes are declared rather than implicit, and each step is independently testable. That combination keeps the implementation correct under concurrent load and cheap to change later.

## Benchmark

```elixir
# Minimal measurement — replace with Benchee for distribution stats.
{time_us, _} = :timer.tc(fn ->
  for _ <- 1..10_000, do: run_operation()
end)
IO.puts("avg: #{time_us / 10_000} µs/op")
```

Target: operation should complete in the low-microsecond range on modern hardware; deviations by >2× indicate a regression worth investigating.

## Deep Dive

Specialized frameworks like Ash (business logic), Commanded (event sourcing), and Nx (numerical computing) abstract away common infrastructure but impose architectural constraints. Ash's declarative resource definitions simplify authorization and querying at the cost of reduced flexibility—deeply nested association policies can degrade query performance. Commanded's event store and aggregate roots enforce event sourcing discipline, making audit trails and temporal queries natural, but require careful snapshot strategy to avoid replaying years of events. Nx brings numerical computing to Elixir, but JIT compilation and lazy evaluation introduce latency; production models benefit from ahead-of-time compilation for inference. For IoT (Nerves), firmware updates must be atomic and resumable—OTA rollback on failure is non-negotiable. Choose frameworks that align with your scaling assumptions: Ash scales horizontally via read replicas; Commanded scales via sharding; Nx scales via distributed training.
## Advanced Considerations

Framework choices like Ash, Commanded, and Nerves create significant architectural constraints that are difficult to change later. Ash's powerful query builder and declarative approach simplify common patterns but can be opaque when debugging complex permission logic or custom filters at scale. Event sourcing with Commanded is powerful for audit trails but creates a different mental model for state management — replaying events to derive current state has CPU and latency costs that aren't apparent in traditional CRUD systems.

Nerves requires understanding the full embedded system stack — from bootloader configuration to over-the-air update mechanisms. A Nerves system that works on your development board may fail in production due to hardware variations, network conditions, or power supply issues. NX's numerical computing is powerful but requires understanding GPU acceleration trade-offs and memory management for large datasets. Livebook provides interactive development but shouldn't be used for production deployments without careful containerization and resource isolation.

The integration between these frameworks and traditional BEAM patterns (supervisors, processes, GenServers) requires careful design. A Commanded projection that rebuilds state from the event log can consume all available CPU, starving other services. NX autograd computations can create unexpected memory usage if not carefully managed. Nerves systems are memory-constrained; performance assumptions from desktop Elixir don't hold. Always prototype these frameworks in realistic environments before committing to them in production systems to validate assumptions.


## Deep Dive: Domain Patterns and Production Implications

Domain-specific frameworks enforce module dependencies and architectural boundaries. Testing domain isolation ensures that constraints are maintained as the codebase grows. Production systems without boundary enforcement often become monolithic and hard to test.

---

## Trade-offs and production gotchas

**1. Eventual consistency bites naive UIs**
Never build a flow that writes + immediately reads via the projection. Either return the new state from the command response, or make the UI eventually-consistent (spinners, optimistic updates).

**2. Checkpoint + write must share a transaction**
If your projector writes with `Repo.insert` and advances the checkpoint in a separate statement, a crash between them double-applies on restart. `commanded_ecto_projections` wraps both in `Ecto.Multi` — use it, do not roll your own.

**3. Different projectors = different databases are fine**
Your balance projection can live in Postgres while a search projection lives in Elasticsearch. Each projector owns its checkpoint in its store; they are independent.

**4. Upsert by natural key, always**
If primary key is an autoincrement id, replaying events duplicates rows. Primary key by `account_id` (or composite: `account_id + day`) makes replays idempotent by overwriting.

**5. Do not enforce cross-projection invariants**
If daily totals say 500 but balance says 499, that is a bug in a projector — not a distributed transaction problem. Each projection is independent; consistency between them must be reachable by replay, not enforced at write time.

**6. Rebuilding a projection drops and replays**
`TRUNCATE` the projection tables, reset the checkpoint in `projection_versions`, and the projector catches up from event 0. For 10M events this takes minutes; plan maintenance windows.

**7. Back-pressure from slow projectors**
A projector that issues one `INSERT` per event caps at ~5k events/sec on a single Postgres connection. Use `Ecto.Multi`, batch inserts, or parallel handlers with partition-by-account-id for high-volume streams.

**8. When NOT to project**
For simple audit logs ("store every event as-is into an audit table"), you do not need a projection — write the audit event directly as part of the aggregate's side effect, or use `Commanded.EventStore` as the query. Projections are for *denormalized* views.

---

## Performance notes

A trivial projector on Postgres (local, one connection) processes roughly 3–5k events/sec. Bottlenecks in order of impact:

1. **Per-event transaction overhead** — mitigate with `handle_events` batch variant when order permits.
2. **Index writes** — every index on the projection table is a write amplification. Keep indexes minimal.
3. **Checkpoint update** — one row update per event. With `{:shared, ...}` advisory locks, contention is negligible; across partitions, consider per-partition checkpoints.

For the daily totals upsert pattern above, the `ON CONFLICT DO UPDATE SET value = value + EXCLUDED.value` idiom compiles to a single write and is idempotent under replay because the delta added equals the delta persisted by the original event — only if the projector is strictly exactly-once (which `commanded_ecto_projections` guarantees via the shared transaction).

---

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?

## Executable Example

```elixir
defmodule CommandedProjections.MixProject do
  use Mix.Project

  def project do
    [app: :commanded_projections, version: "0.1.0", elixir: "~> 1.16", deps: deps()]

  def application do
    [extra_applications: [:logger], mod: {CommandedProjections.Application, []}]

  defp deps do
    [
      {:commanded, "~> 1.4"},
      {:commanded_ecto_projections, "~> 1.4"},
      {:ecto_sql, "~> 3.11"},
      {:postgrex, "~> 0.17"},
      {:jason, "~> 1.4"}
    ]


defmodule CommandedProjections.Repo do
  use Ecto.Repo,
    otp_app: :commanded_projections,
    adapter: Ecto.Adapters.Postgres


import Config

config :commanded_projections, CommandedProjections.Repo,
  database: "ledger_read",
  hostname: "localhost",
  username: "postgres",
  password: "postgres",
  pool_size: 10

config :commanded_projections, ecto_repos: [CommandedProjections.Repo]

config :commanded_projections, CommandedProjections.App,
  event_store: [
    adapter: Commanded.EventStore.Adapters.InMemory,
    serializer: Commanded.Serialization.JsonSerializer
  ]


# 001_create_account_balances.exs
defmodule CommandedProjections.Repo.Migrations.CreateAccountBalances do
  use Ecto.Migration

  def change do
    create table(:account_balances, primary_key: false) do
      add :account_id, :string, primary_key: true
      add :owner, :string, null: false
      add :balance, :bigint, null: false, default: 0
      add :status, :string, null: false
      timestamps()

    create table(:projection_versions, primary_key: false) do
      add :projection_name, :string, primary_key: true
      add :last_seen_event_number, :bigint, null: false, default: 0

# 002_create_transaction_log.exs
defmodule CommandedProjections.Repo.Migrations.CreateTransactionLog do
  use Ecto.Migration

  def change do
    create table(:transaction_log) do
      add :account_id, :string, null: false
      add :kind, :string, null: false
      add :amount, :bigint, null: false
      add :balance_after, :bigint, null: false
      add :occurred_at, :utc_datetime_usec, null: false

    create index(:transaction_log, [:account_id, :occurred_at])
  end
end

# 003_create_daily_totals.exs
defmodule CommandedProjections.Repo.Migrations.CreateDailyTotals do
  use Ecto.Migration

  def change do
    create table(:daily_totals, primary_key: false) do
      add :account_id, :string, primary_key: true
      add :day, :date, primary_key: true
      add :deposits_total, :bigint, null: false, default: 0
      add :withdrawals_total, :bigint, null: false, default: 0
    end
  end
end


defmodule CommandedProjections.Projections.AccountBalance do
  use Ecto.Schema

  @primary_key {:account_id, :string, []}
  schema "account_balances" do
    field :owner, :string
    field :balance, :integer
    field :status, :string
    timestamps()
  end
end

defmodule CommandedProjections.Projections.TransactionLog do
  use Ecto.Schema

  schema "transaction_log" do
    field :account_id, :string
    field :kind, :string
    field :amount, :integer
    field :balance_after, :integer
    field :occurred_at, :utc_datetime_usec
  end
end

defmodule CommandedProjections.Projections.DailyTotals do
  use Ecto.Schema

  @primary_key false
  schema "daily_totals" do
    field :account_id, :string, primary_key: true
    field :day, :date, primary_key: true
    field :deposits_total, :integer
    field :withdrawals_total, :integer
  end
end


# lib/commanded_projections/projectors/account_balance_projector.ex
defmodule CommandedProjections.Projectors.AccountBalance do
  use Commanded.Projections.Ecto,
    application: CommandedProjections.App,
    repo: CommandedProjections.Repo,
    name: "account_balance_projector"

  alias CommandedProjections.Projections.AccountBalance
  alias CommandedAggregates.Events.{AccountOpened, MoneyDeposited, MoneyWithdrawn, AccountClosed}

  project(%AccountOpened{} = e, _metadata, fn multi ->
    Ecto.Multi.insert(
      multi,
      :account,
      %AccountBalance{
        account_id: e.account_id,
        owner: e.owner,
        balance: 0,
        status: "open"
      }
    )
  end)

  project(%MoneyDeposited{} = e, _md, fn multi ->
    Ecto.Multi.update_all(
      multi,
      :account,
      from(a in AccountBalance, where: a.account_id == ^e.account_id),
      set: [balance: e.new_balance, updated_at: DateTime.utc_now()]
    )
  end)

  project(%MoneyWithdrawn{} = e, _md, fn multi ->
    Ecto.Multi.update_all(
      multi,
      :account,
      from(a in AccountBalance, where: a.account_id == ^e.account_id),
      set: [balance: e.new_balance, updated_at: DateTime.utc_now()]
    )
  end)

  project(%AccountClosed{} = e, _md, fn multi ->
    Ecto.Multi.update_all(
      multi,
      :account,
      from(a in AccountBalance, where: a.account_id == ^e.account_id),
      set: [status: "closed", updated_at: DateTime.utc_now()]
    )
  end)

  import Ecto.Query
end

# lib/commanded_projections/projectors/transaction_log_projector.ex
defmodule CommandedProjections.Projectors.TransactionLog do
  use Commanded.Projections.Ecto,
    application: CommandedProjections.App,
    repo: CommandedProjections.Repo,
    name: "transaction_log_projector"

  alias CommandedProjections.Projections.TransactionLog
  alias CommandedAggregates.Events.{MoneyDeposited, MoneyWithdrawn}

  project(%MoneyDeposited{} = e, _md, fn multi ->
    Ecto.Multi.insert(multi, :tx, %TransactionLog{
      account_id: e.account_id,
      kind: "deposit",
      amount: e.amount,
      balance_after: e.new_balance,
      occurred_at: e.deposited_at
    })
  end)

  project(%MoneyWithdrawn{} = e, _md, fn multi ->
    Ecto.Multi.insert(multi, :tx, %TransactionLog{
      account_id: e.account_id,
      kind: "withdrawal",
      amount: e.amount,
      balance_after: e.new_balance,
      occurred_at: e.withdrawn_at
    })
  end)
end

# lib/commanded_projections/projectors/daily_totals_projector.ex
defmodule CommandedProjections.Projectors.DailyTotals do
  use Commanded.Projections.Ecto,
    application: CommandedProjections.App,
    repo: CommandedProjections.Repo,
    name: "daily_totals_projector"

  alias CommandedProjections.Projections.DailyTotals
  alias CommandedAggregates.Events.{MoneyDeposited, MoneyWithdrawn}

  project(%MoneyDeposited{} = e, _md, fn multi ->
    upsert_totals(multi, e.account_id, DateTime.to_date(e.deposited_at),
      deposits_delta: e.amount,
      withdrawals_delta: 0
    )
  end)

  project(%MoneyWithdrawn{} = e, _md, fn multi ->
    upsert_totals(multi, e.account_id, DateTime.to_date(e.withdrawn_at),
      deposits_delta: 0,
      withdrawals_delta: e.amount
    )
  end)

  defp upsert_totals(multi, account_id, day, deposits_delta: dd, withdrawals_delta: wd) do
    row = %DailyTotals{
      account_id: account_id,
      day: day,
      deposits_total: dd,
      withdrawals_total: wd
    }

    Ecto.Multi.insert(multi, {:daily, account_id, day}, row,
      on_conflict: [inc: [deposits_total: dd, withdrawals_total: wd]],
      conflict_target: [:account_id, :day]
    )
  end
end


defmodule CommandedProjections.App do
  use Commanded.Application, otp_app: :commanded_projections
end

defmodule CommandedProjections.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      CommandedProjections.Repo,
      CommandedProjections.App,
      CommandedProjections.Projectors.AccountBalance,
      CommandedProjections.Projectors.TransactionLog,
      CommandedProjections.Projectors.DailyTotals
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: CommandedProjections.Supervisor)
  end
end


defmodule CommandedProjections.ProjectorsTest do
  use ExUnit.Case

  alias CommandedProjections.Repo
  alias CommandedProjections.Projections.{AccountBalance, TransactionLog, DailyTotals}
  alias CommandedAggregates.Events.{AccountOpened, MoneyDeposited, MoneyWithdrawn}

    test "TransactionLog appends one row per money event" do
      account_id = "acc-tx-" <> Integer.to_string(System.unique_integer([:positive]))

      publish_events([
        %AccountOpened{account_id: account_id, owner: "Bob", overdraft_limit: 0},
        %MoneyDeposited{account_id: account_id, amount: 100, new_balance: 100, deposited_at: DateTime.utc_now()},
        %MoneyWithdrawn{account_id: account_id, amount: 30, new_balance: 70, withdrawn_at: DateTime.utc_now()}
      ])

      wait_for_projection(fn ->
        import Ecto.Query
        Repo.aggregate(from(t in TransactionLog, where: t.account_id == ^account_id), :count)
        |> case do
          n when n >= 2 -> :ok
          _ -> nil
        end
      end)
    end
  end

  # ---- helpers ----

  defp publish_events(events) do
    # Test support — publish events directly to the app's event store.
    # In production, events arrive via CommandedAggregates.App.dispatch/1.
    for e <- events do
      CommandedProjections.App.event_store()
      |> elem(0)
      |> send({:publish, e})
    end

    :ok
  end

  defp wait_for_projection(fun, attempts \ 50) do
    case fun.() do
      nil when attempts > 0 ->
        Process.sleep(20)
        wait_for_projection(fun, attempts - 1)

      other ->
        other
    end
  end
end

defmodule Main do
  def main do
      :ok
  end
end

Main.main()
```
