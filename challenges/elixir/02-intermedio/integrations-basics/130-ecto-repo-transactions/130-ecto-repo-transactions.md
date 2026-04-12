# Ecto transactions: `Repo.transaction/1` and a first taste of `Ecto.Multi`

**Project**: `tx_lab` — a two-step "transfer funds" operation implemented three ways: `Repo.transaction/1` with a function, explicit `Repo.rollback/1`, and `Ecto.Multi`.

---

## Project context

`Repo.transaction/1` is the obvious tool for "do several DB writes, all or
nothing". But the form most people reach for first — a closure with a bunch
of `Repo.insert!` calls inside — has a subtle downside: it's hard to
compose, and `{:error, _}` returns from one step don't automatically roll
back the others. You have to bubble up errors manually or use
`Repo.rollback/1`.

`Ecto.Multi` is the declarative alternative: you *describe* the steps, each
named, and Ecto runs them atomically, halting and rolling back on the first
failure. It returns a shape that tells you exactly which step failed and
what the intermediate values were — perfect for contexts that chain four
or five writes with conditional logic.

In this exercise you'll implement a toy bank-transfer function that
debits one account and credits another. You'll do it three ways so the
trade-offs are concrete, not abstract. The schema is tiny (an `Account`
with a `balance`) so the transactional behavior is front and center.

Project structure:

```
tx_lab/
├── config/
│   └── config.exs
├── lib/
│   ├── tx_lab/
│   │   ├── application.ex
│   │   ├── repo.ex
│   │   └── account.ex
│   └── tx_lab.ex
├── priv/
│   └── repo/
│       └── migrations/
│           └── 20260101000001_create_accounts.exs
├── test/
│   ├── tx_lab_test.exs
│   └── test_helper.exs
└── mix.exs
```

---

## Core concepts

### 1. `Repo.transaction/1` with a function

```elixir
Repo.transaction(fn ->
  {:ok, a} = do_debit(...)
  {:ok, b} = do_credit(...)
  {a, b}
end)
```

Returns `{:ok, result}` where `result` is whatever the function returned,
or `{:error, reason}` if you called `Repo.rollback(reason)` inside.
If the function raises, the transaction rolls back and the exception
propagates. `{:error, _}` returns from nested functions **do not**
automatically rollback — you must match them and call `Repo.rollback/1`.

### 2. `Repo.rollback/1` is a non-local return

```elixir
Repo.transaction(fn ->
  case Repo.update(changeset) do
    {:ok, r} -> r
    {:error, cs} -> Repo.rollback(cs)
  end
end)
```

`Repo.rollback/1` throws — it aborts the function immediately. The outer
`Repo.transaction/1` catches the throw, rolls the DB back, and returns
`{:error, reason}`. This keeps the happy path linear while still letting
you abort on errors.

### 3. `Ecto.Multi` — named steps, declarative composition

```elixir
Ecto.Multi.new()
|> Ecto.Multi.update(:debit, debit_changeset)
|> Ecto.Multi.update(:credit, credit_changeset)
|> Repo.transaction()
```

Each `Multi.update/insert/delete/run` step is named. If every step
succeeds, you get `{:ok, %{debit: a, credit: b}}`. If step `:credit`
fails, you get `{:error, :credit, failed_value, %{debit: a}}` — you know
*which* step failed and the changesets/values from the earlier steps that
were already rolled back.

### 4. `Multi.run/3` for arbitrary logic

```elixir
Multi.run(multi, :check, fn _repo, %{debit: a} ->
  if a.balance < 0, do: {:error, :insufficient}, else: {:ok, :ok}
end)
```

Any step can be a `run/3` — a function receiving the repo and the map of
prior results. Return `{:ok, _}` to continue or `{:error, _}` to roll back.

### 5. Optimistic concurrency via `optimistic_lock/3`

For transfers, the real-world concern isn't just atomicity — it's two
concurrent transfers both reading the same `balance`, both deciding
it's fine, both writing a new value, and one overwriting the other.
`optimistic_lock/3` bumps a `lock_version` column and fails the update if
another transaction raced past you. We'll use it for the `Multi` version
to show the full shape.

---

## Why Multi and not a closure

A closure (`Repo.transaction(fn -> ... end)`) is fine for two or three
straight-line writes. It breaks down when you need (a) to know *which*
step failed for logging, (b) to pass intermediate values to later steps
declaratively, or (c) to test each step in isolation. `Multi` is a data
structure — you can inspect it, compose it, and its error return tells
you the failing step name plus every prior success. Closures are code
you run; `Multi` is a plan you submit.

---

## Design decisions

**Option A — Closure-only (`Repo.transaction(fn -> ... end)` + `Repo.rollback/1`)**
- Pros: Familiar, reads like regular Elixir; easier for a single short
  transaction.
- Cons: No step names in the error return; chaining `{:ok, _} <- ...`
  inside the closure gets noisy; harder to unit-test a single step.

**Option B — `Ecto.Multi` for anything beyond two steps** (chosen)
- Pros: Named steps, per-step failure reporting, composable (you can
  `Multi.append/2` conditionally); every step has access to the map of
  prior results.
- Cons: A bit more ceremony for trivial 2-step transactions; `Multi.run/3`
  callbacks receive `repo` explicitly, which some find awkward.

→ Chose **B** as the default once a transaction exceeds two steps; the
  `transfer_raw` and `transfer_with_rollback` variants are shown for
  pedagogy and for the truly-simple case.

---

## Implementation

### Step 1: Create the project

```bash
mix new tx_lab --sup
cd tx_lab
```

### Step 2: `mix.exs`, `config/config.exs`, `repo.ex`, `application.ex`

Standard shape — Ecto + SQLite, one Repo child under the Application
supervisor, `test` alias for drop/create/migrate.

```elixir
# mix.exs deps / aliases
defp deps, do: [{:ecto_sql, "~> 3.11"}, {:ecto_sqlite3, "~> 0.17"}]
defp aliases, do: [test: ["ecto.drop --quiet", "ecto.create --quiet", "ecto.migrate --quiet", "test"]]
```

```elixir
# config/config.exs
import Config
config :tx_lab, ecto_repos: [TxLab.Repo]
config :tx_lab, TxLab.Repo,
  database: Path.expand("../tx_lab_#{config_env()}.db", __DIR__),
  pool_size: 5,
  pool: if(config_env() == :test, do: Ecto.Adapters.SQL.Sandbox, else: DBConnection.ConnectionPool)
```

```elixir
# lib/tx_lab/repo.ex
defmodule TxLab.Repo do
  use Ecto.Repo, otp_app: :tx_lab, adapter: Ecto.Adapters.SQLite3
end

# lib/tx_lab/application.ex
defmodule TxLab.Application do
  @moduledoc false
  use Application
  @impl true
  def start(_type, _args) do
    Supervisor.start_link([TxLab.Repo], strategy: :one_for_one, name: TxLab.Supervisor)
  end
end
```

### Step 3: Migration

`priv/repo/migrations/20260101000001_create_accounts.exs`:

```elixir
defmodule TxLab.Repo.Migrations.CreateAccounts do
  use Ecto.Migration

  def change do
    create table(:accounts) do
      add :owner, :string, null: false
      add :balance, :integer, null: false, default: 0
      add :lock_version, :integer, null: false, default: 1
      timestamps()
    end
  end
end
```

### Step 4: Schema — `lib/tx_lab/account.ex`

```elixir
defmodule TxLab.Account do
  use Ecto.Schema
  import Ecto.Changeset

  schema "accounts" do
    field :owner, :string
    field :balance, :integer, default: 0
    field :lock_version, :integer, default: 1
    timestamps()
  end

  def create_changeset(account, attrs) do
    account
    |> cast(attrs, [:owner, :balance])
    |> validate_required([:owner])
    |> validate_number(:balance, greater_than_or_equal_to: 0)
  end

  @doc """
  Builds an update changeset that adjusts balance by `delta` and bumps
  `lock_version`. `optimistic_lock/3` ensures two concurrent updates can't
  both succeed — the second will fail the WHERE on `lock_version`.
  """
  def balance_changeset(account, delta) do
    account
    |> change(balance: account.balance + delta)
    |> validate_number(:balance, greater_than_or_equal_to: 0, message: "insufficient funds")
    |> optimistic_lock(:lock_version)
  end
end
```

### Step 5: The three implementations — `lib/tx_lab.ex`

```elixir
defmodule TxLab do
  @moduledoc """
  Three takes on a "transfer funds" operation:

    * `transfer_raw/3`         — a closure around `Repo.transaction/1` that
                                 raises on failure (simplest, least flexible).
    * `transfer_with_rollback/3` — the same, but using `Repo.rollback/1` to
                                 return `{:error, reason}` cleanly.
    * `transfer_multi/3`       — the declarative `Ecto.Multi` form, with
                                 optimistic locking and named steps.
  """

  alias TxLab.{Repo, Account}
  alias Ecto.Multi

  @spec create_account(String.t(), integer()) :: {:ok, Account.t()} | {:error, Ecto.Changeset.t()}
  def create_account(owner, balance \\ 0) do
    %Account{}
    |> Account.create_changeset(%{"owner" => owner, "balance" => balance})
    |> Repo.insert()
  end

  # ── 1) Raw closure — raises on bad inputs -------------------------------
  @doc """
  Simplest form. Uses `insert!/update!` which raise on invalid changesets.
  Clean for the happy path; the downside is that callers get an exception
  on failure rather than a tagged tuple.
  """
  def transfer_raw(from_id, to_id, amount) do
    Repo.transaction(fn ->
      from = Repo.get!(Account, from_id)
      to = Repo.get!(Account, to_id)

      # Both updates must succeed; a failing changeset raises, which
      # aborts the transaction and rolls back the debit.
      from = Account.balance_changeset(from, -amount) |> Repo.update!()
      to = Account.balance_changeset(to, amount) |> Repo.update!()

      {from, to}
    end)
  end

  # ── 2) Explicit Repo.rollback/1 — tagged errors -------------------------
  @doc """
  Pattern-matches every step and explicitly rolls back with a structured
  reason. Keeps the happy path linear without needing exceptions.
  """
  def transfer_with_rollback(from_id, to_id, amount) do
    Repo.transaction(fn ->
      with from = %Account{} <- Repo.get(Account, from_id) || Repo.rollback({:not_found, from_id}),
           to = %Account{} <- Repo.get(Account, to_id) || Repo.rollback({:not_found, to_id}),
           {:ok, from} <- Repo.update(Account.balance_changeset(from, -amount)),
           {:ok, to} <- Repo.update(Account.balance_changeset(to, amount)) do
        {from, to}
      else
        {:error, %Ecto.Changeset{} = cs} -> Repo.rollback(cs)
      end
    end)
  end

  # ── 3) Ecto.Multi — declarative, composable -----------------------------
  @doc """
  The declarative form. Each step is named; failure reports which step
  failed and the intermediate values. Perfect for logging and for chaining
  conditional logic via `Multi.run/3`.
  """
  def transfer_multi(from_id, to_id, amount) do
    Multi.new()
    |> Multi.run(:from, fn repo, _ ->
      case repo.get(Account, from_id) do
        nil -> {:error, :not_found_from}
        acc -> {:ok, acc}
      end
    end)
    |> Multi.run(:to, fn repo, _ ->
      case repo.get(Account, to_id) do
        nil -> {:error, :not_found_to}
        acc -> {:ok, acc}
      end
    end)
    |> Multi.update(:debit, fn %{from: from} -> Account.balance_changeset(from, -amount) end)
    |> Multi.update(:credit, fn %{to: to} -> Account.balance_changeset(to, amount) end)
    |> Repo.transaction()
  end
end
```

### Step 6: `test/test_helper.exs`

```elixir
ExUnit.start()
Ecto.Adapters.SQL.Sandbox.mode(TxLab.Repo, :manual)
```

### Step 7: `test/tx_lab_test.exs`

```elixir
defmodule TxLabTest do
  use ExUnit.Case, async: false

  alias TxLab.{Repo, Account}

  setup do
    :ok = Ecto.Adapters.SQL.Sandbox.checkout(Repo)
    {:ok, a} = TxLab.create_account("alice", 100)
    {:ok, b} = TxLab.create_account("bob", 50)
    {:ok, alice: a, bob: b}
  end

  describe "transfer_raw/3" do
    test "moves funds atomically", %{alice: a, bob: b} do
      assert {:ok, {_from, _to}} = TxLab.transfer_raw(a.id, b.id, 30)

      assert Repo.get!(Account, a.id).balance == 70
      assert Repo.get!(Account, b.id).balance == 80
    end

    test "rolls back when the debit would go negative", %{alice: a, bob: b} do
      # insert! raises on invalid changeset → transaction rolls back.
      assert_raise Ecto.InvalidChangesetError, fn ->
        TxLab.transfer_raw(a.id, b.id, 9_999)
      end

      assert Repo.get!(Account, a.id).balance == 100
      assert Repo.get!(Account, b.id).balance == 50
    end
  end

  describe "transfer_with_rollback/3" do
    test "returns {:error, reason} when the source doesn't exist",
         %{bob: b} do
      assert {:error, {:not_found, -1}} = TxLab.transfer_with_rollback(-1, b.id, 10)
    end

    test "returns {:error, changeset} on insufficient funds",
         %{alice: a, bob: b} do
      assert {:error, %Ecto.Changeset{valid?: false}} =
               TxLab.transfer_with_rollback(a.id, b.id, 9_999)

      # No partial state: both balances untouched.
      assert Repo.get!(Account, a.id).balance == 100
      assert Repo.get!(Account, b.id).balance == 50
    end
  end

  describe "transfer_multi/3" do
    test "success returns a map keyed by step name", %{alice: a, bob: b} do
      assert {:ok, %{from: _, to: _, debit: debit, credit: credit}} =
               TxLab.transfer_multi(a.id, b.id, 20)

      assert debit.balance == 80
      assert credit.balance == 70
    end

    test "reports the failing step and the rolled-back intermediates",
         %{alice: a} do
      assert {:error, :to, :not_found_to, %{from: from}} =
               TxLab.transfer_multi(a.id, -1, 10)

      # `from` was fetched, but its balance was never updated.
      assert from.balance == 100
      assert Repo.get!(Account, a.id).balance == 100
    end

    test "insufficient funds fails at :debit", %{alice: a, bob: b} do
      assert {:error, :debit, %Ecto.Changeset{valid?: false}, _prior} =
               TxLab.transfer_multi(a.id, b.id, 9_999)

      # And both accounts are untouched.
      assert Repo.get!(Account, a.id).balance == 100
      assert Repo.get!(Account, b.id).balance == 50
    end
  end
end
```

### Step 8: Run

```bash
mix test
```

### Why this works

`Repo.transaction/1` wraps its callback in a SAVEPOINT at the DB;
uncaught raises or `Repo.rollback/1` calls inside abort the SAVEPOINT
and return `{:error, reason}`. `Ecto.Multi` is a declarative queue of
named operations — when you hand it to `Repo.transaction/1`, the same
SAVEPOINT mechanics apply, but the error tuple is richer
(`{:error, failed_op, value, changes}`). Optimistic locking via
`optimistic_lock/3` adds a `WHERE lock_version = ?` guard so two
concurrent transfers produce a stale-entry error instead of a lost update.

---

## Benchmark

`:timer.tc(fn -> TxLab.transfer_multi(a.id, b.id, 1) end)` on SQLite
typically clocks around 1-3 ms including the two SELECTs and two
UPDATEs; on Postgres with a local socket, closer to 500µs-1ms. Target:
a simple 4-step transfer should stay under 5ms on commodity hardware;
higher numbers point at contention or missing indexes on the PK lookups.

---

## Trade-offs and production gotchas

**1. `{:error, _}` from nested calls does NOT auto-rollback**
Inside a `Repo.transaction(fn -> ... end)`, returning `{:error, reason}`
from the outer function returns it to the caller — but the transaction
still commits anything that succeeded. Use `Repo.rollback/1` explicitly,
or pattern-match `{:ok, _}` and raise on errors.

**2. `Multi` is not just for Repo operations**
`Multi.run/3` runs any function inside the transaction. Handy for mixing a
DB write with an external effect that should only fire if the DB state
committed — but remember that the external effect commits immediately; if
the DB fails *after* the external effect, you have no rollback. Order
matters: put side effects last, or use an outbox pattern.

**3. Optimistic locking is not pessimistic locking**
`optimistic_lock/3` detects conflicts — it doesn't prevent them. Two
concurrent transfers can both read, both write, and one fails with
`Ecto.StaleEntryError`. Callers must retry. For high-contention scenarios,
`Repo.transaction/1` with `SELECT ... FOR UPDATE` (`Ecto.Query.lock/2`)
serializes instead.

**4. SQLite serializes all writers**
In the tests above, SQLite runs writes serially regardless of our locking.
Postgres will actually let two transfers race, at which point optimistic
lock retries matter. Test concurrency on the DB you'll deploy against.

**5. `Multi`'s error tuple has four elements**
`{:error, failed_op, failed_value, changes_so_far}`. That last element is
gold for logging — you know exactly which prior steps had succeeded.
Destructure it; don't throw it away.

**6. Don't nest transactions expecting savepoints**
Ecto supports nested `Repo.transaction/1` but treats it as a single
outer transaction. Nested `rollback/1` rolls back the **entire** outer
transaction. For true savepoints you need the adapter's savepoint API
directly — rarely needed.

**7. When NOT to use transactions**
Read-only workflows. Idempotent operations. Operations where partial
success is an acceptable outcome (metrics ingestion). Transactions cost
connection-holding time — use them only when atomicity is part of the
contract.

---

## Reflection

- Your transfer function needs to also send a webhook notification.
  Where do you put the HTTP call — inside the `Multi` (via `Multi.run/3`)
  or after `Repo.transaction/1` returns `{:ok, _}`? What's the failure
  mode of each choice, and how does an outbox pattern resolve it?
- Under high contention, `optimistic_lock/3` starts returning
  `Ecto.StaleEntryError` for ~5% of transfers. Do you switch to
  `Ecto.Query.lock/2` (SELECT FOR UPDATE), add client-side retry
  logic, or redesign the data model? What factors (p99 latency,
  throughput, connection count) push you toward each?

---

## Resources

- [`Ecto.Repo.transaction/2`](https://hexdocs.pm/ecto/Ecto.Repo.html#c:transaction/2)
- [`Ecto.Multi` — hexdocs](https://hexdocs.pm/ecto/Ecto.Multi.html)
- [`optimistic_lock/3`](https://hexdocs.pm/ecto/Ecto.Changeset.html#optimistic_lock/3)
- [Dashbit: "Transaction safety in Ecto"](https://dashbit.co/blog/) — articles on Multi and transactional patterns
- [`Ecto.Query.lock/2`](https://hexdocs.pm/ecto/Ecto.Query.html#lock/3) — pessimistic locking when you need it
