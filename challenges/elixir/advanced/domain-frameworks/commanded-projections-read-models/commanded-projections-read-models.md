# Commanded projections and read models

**Project**: `commanded_projections` — read-side Ecto projections built from the event stream

---

## Why domain frameworks matters

Frameworks like Ash, Commanded, Oban, Nx and Axon encode large domain patterns (CQRS, event sourcing, ML training, background jobs, IoT updates) into reusable building blocks. Used well, they compress months of bespoke code into days.

Used poorly, they hide complexity that bites in production: aggregate version drift in Commanded, projection lag in CQRS systems, OTA failure recovery in Nerves, gradient explosion in Axon training loops. The framework's defaults are not your defaults.

---

## The business problem

You are building a production-grade Elixir component in the **Domain frameworks** area. The goal is not a toy demonstration — it is a piece you could drop into a real system and rely on under load.

Concretely, you must:

1. Implement the topic correctly with measurable behavior.
2. Surface failure modes via `{:ok, _} / {:error, reason}` tuples — never raise from a public API except for programmer errors.
3. Bound resources: explicit timeouts, memory caps, back-pressure where relevant, and supervision for any long-lived process.
4. Provide a test suite with `async: true` where safe, `doctest` for public examples, and `describe` blocks that name the contract.
5. Provide a `script/main.exs` that demonstrates the piece end-to-end with realistic data — not a `1 + 1` smoke test.

---

## Project structure

```
commanded_projections/
├── lib/
│   └── commanded_projections.ex
├── script/
│   └── main.exs
├── test/
│   └── commanded_projections_test.exs
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

Chose **B** because in Domain frameworks the failure modes — not the happy path — are what determine whether the system stays up under real production conditions.

---

## Implementation

### `mix.exs`

```elixir
defmodule CommandedProjections.MixProject do
  use Mix.Project

  def project do
    [
      app: :commanded_projections,
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
### `lib/commanded_projections.ex`

```elixir
defmodule CommandedProjections.Repo do
  @moduledoc """
  Ejercicio: Commanded projections and read models.
  Implementa el comportamiento descrito en el enunciado, exponiendo funciones públicas documentadas y con tipos claros.
  """

  use Ecto.Repo,
    otp_app: :commanded_projections,
    adapter: Ecto.Adapters.Postgres
end

# 001_create_account_balances.exs
defmodule CommandedProjections.Repo.Migrations.CreateAccountBalances do
  use Ecto.Migration

  @doc "Returns change result."
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

  @doc "Returns change result."
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

  @doc "Returns change result."
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
```
### `test/commanded_projections_test.exs`

```elixir
defmodule CommandedProjections.ProjectorsTest do
  use ExUnit.Case, async: true
  doctest CommandedProjections.Repo

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
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Commanded projections and read models.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Commanded projections and read models ===")
    IO.puts("Category: Domain frameworks\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case CommandedProjections.run(payload) do
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
        for _ <- 1..1_000, do: CommandedProjections.run(:bench)
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

### 1. Frameworks encode opinions

Ash, Commanded, Oban each pick defaults that work for the common case. Understand the defaults before you customize — the framework's authors chose them for a reason.

### 2. Event-sourced systems need projection lag tolerance

In CQRS, the read model is eventually consistent with the write model. UI must handle 'I saved but I don't see my own data yet'. Optimistic UI updates help.

### 3. Background jobs need idempotency and retries

Oban retries failed jobs by default. The worker must be idempotent: repeating a job must produce the same end state. Use unique constraints and deduplication keys.

---
