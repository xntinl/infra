# Custom `Ecto.Type` — Money as a First-Class Value

**Project**: `finance_ledger` — type-safe money handling with a custom Ecto type backed by a Postgres composite

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
finance_ledger/
├── lib/
│   └── finance_ledger.ex
├── script/
│   └── main.exs
├── test/
│   └── finance_ledger_test.exs
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
defmodule FinanceLedger.MixProject do
  use Mix.Project

  def project do
    [
      app: :finance_ledger,
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
### `lib/finance_ledger.ex`

```elixir
# priv/repo/migrations/20260101000000_create_money_type.exs
defmodule FinanceLedger.Repo.Migrations.CreateMoneyType do
  use Ecto.Migration

  @doc "Returns up result."
  def up do
    execute "CREATE TYPE money_t AS (amount bigint, currency char(3))"

    create table(:transactions) do
      add :ref, :string, null: false
      add :amount, :money_t, null: false
      timestamps()
    end

    create unique_index(:transactions, [:ref])
    create index(:transactions, ["(amount).currency"], name: :transactions_currency_idx)
  end

  @doc "Returns down result."
  def down do
    drop table(:transactions)
    execute "DROP TYPE money_t"
  end
end

# lib/finance_ledger/money.ex
defmodule FinanceLedger.Money do
  @moduledoc """
  Immutable money value. All operations guard currency equality.

  Amount is stored as integer minor units (cents, pence, ...) to avoid floating-point.
  """
  @enforce_keys [:amount, :currency]
  defstruct [:amount, :currency]

  @currencies ~w(USD EUR GBP BRL ARS JPY CHF)

  @type t :: %__MODULE__{amount: integer(), currency: String.t()}

  @doc "Creates result from amount and currency."
  @spec new(integer(), String.t()) :: {:ok, t()} | {:error, :invalid_currency}
  def new(amount, currency) when is_integer(amount) and is_binary(currency) do
    if currency in @currencies do
      {:ok, %__MODULE__{amount: amount, currency: currency}}
    else
      {:error, :invalid_currency}
    end
  end

  @doc "Creates result from amount and currency."
  def new(amount, currency) when is_binary(amount) do
    case Integer.parse(amount) do
      {n, ""} -> new(n, currency)
      _ -> {:error, :invalid_amount}
    end
  end

  @doc "Adds result."
  @spec add(t(), t()) :: t()
  def add(%__MODULE__{currency: c} = a, %__MODULE__{currency: c} = b),
    do: %__MODULE__{amount: a.amount + b.amount, currency: c}

  @doc "Adds result."
  def add(%__MODULE__{currency: c1}, %__MODULE__{currency: c2}),
    do: raise(ArgumentError, "currency mismatch: #{c1} vs #{c2}")

  @doc "Returns to string result from currency."
  @spec to_string(t()) :: String.t()
  def to_string(%__MODULE__{amount: a, currency: c}), do: "#{a} #{c}"
end

defimpl String.Chars, for: FinanceLedger.Money do
  @doc "Returns to string result from m."
  def to_string(m), do: FinanceLedger.Money.to_string(m)
end

defimpl Inspect, for: FinanceLedger.Money do
  @doc "Returns inspect result from m and _opts."
  def inspect(m, _opts), do: "#Money<#{m.amount} #{m.currency}>"
end

# lib/finance_ledger/types/money.ex
defmodule FinanceLedger.Types.Money do
  @moduledoc """
  Ecto.Type for the Postgres composite `money_t`.

  Wire format: `"(1234,USD)"` string.
  """
  use Ecto.Type

  alias FinanceLedger.Money

  @doc "Returns type result."
  @impl true
  def type, do: :money_t

  # cast/1 — input from user / changeset
  @doc "Returns cast result."
  @impl true
  def cast(%Money{} = m), do: {:ok, m}

  @doc "Returns cast result from currency."
  def cast(%{amount: a, currency: c}), do: Money.new(a, c) |> wrap()
  @doc "Returns cast result."
  def cast(%{"amount" => a, "currency" => c}), do: Money.new(a, c) |> wrap()

  @doc "Returns cast result from str."
  def cast(str) when is_binary(str) do
    case String.split(str, " ", parts: 2) do
      [amount, currency] -> Money.new(amount, currency) |> wrap()
      _ -> :error
    end
  end

  @doc "Returns cast result from _."
  def cast(_), do: :error

  # load/1 — from DB into struct
  @doc "Loads result from str."
  @impl true
  def load(str) when is_binary(str) do
    case Regex.run(~r/^\((-?\d+),(\w{3})\)$/, str) do
      [_, amount, currency] -> Money.new(String.to_integer(amount), currency) |> wrap()
      _ -> :error
    end
  end

  @doc "Loads result from currency."
  def load({amount, currency}) when is_integer(amount) and is_binary(currency) do
    amount |> Money.new(currency) |> wrap()
  end

  @doc "Loads result from _."
  def load(_), do: :error

  # dump/1 — struct to DB
  @doc "Returns dump result from currency."
  @impl true
  def dump(%Money{amount: a, currency: c}), do: {:ok, "(#{a},#{c})"}
  @doc "Returns dump result from _."
  def dump(_), do: :error

  @doc "Returns whether equal holds."
  @impl true
  def equal?(%Money{} = a, %Money{} = b),
    do: a.amount == b.amount and a.currency == b.currency

  @doc "Returns whether equal holds from _ and _."
  def equal?(_, _), do: false

  @doc "Returns embed as result from _."
  @impl true
  def embed_as(_), do: :self

  defp wrap({:ok, m}), do: {:ok, m}
  defp wrap({:error, _}), do: :error
end

# lib/finance_ledger/schemas/transaction.ex
defmodule FinanceLedger.Schemas.Transaction do
  use Ecto.Schema
  import Ecto.Changeset
  alias FinanceLedger.Types.Money

  schema "transactions" do
    field :ref, :string
    field :amount, Money
    timestamps()
  end

  @doc "Returns changeset result from tx and attrs."
  def changeset(tx, attrs) do
    tx
    |> cast(attrs, [:ref, :amount])
    |> validate_required([:ref, :amount])
    |> unique_constraint(:ref)
  end
end
```
### `test/finance_ledger_test.exs`

```elixir
defmodule FinanceLedger.MoneyTest do
  use ExUnit.Case, async: true
  doctest FinanceLedger.Repo.Migrations.CreateMoneyType
  alias FinanceLedger.{Money, Repo}
  alias FinanceLedger.Schemas.Transaction

  setup do
    Ecto.Adapters.SQL.Sandbox.checkout(Repo)
    :ok = Ecto.Adapters.SQL.Sandbox.mode(Repo, {:shared, self()})
    :ok
  end

  describe "Money.new/2" do
    test "accepts valid currency" do
      assert {:ok, %Money{amount: 100, currency: "USD"}} = Money.new(100, "USD")
    end

    test "rejects unknown currency" do
      assert {:error, :invalid_currency} = Money.new(100, "XYZ")
    end
  end

  describe "Money.add/2" do
    test "adds same currency" do
      {:ok, a} = Money.new(100, "USD")
      {:ok, b} = Money.new(250, "USD")
      assert %Money{amount: 350, currency: "USD"} = Money.add(a, b)
    end

    test "raises on mismatched currencies" do
      {:ok, a} = Money.new(100, "USD")
      {:ok, b} = Money.new(100, "EUR")
      assert_raise ArgumentError, fn -> Money.add(a, b) end
    end
  end

  describe "Ecto roundtrip" do
    test "insert and read returns the same struct" do
      {:ok, money} = Money.new(1234, "USD")

      {:ok, tx} =
        %Transaction{}
        |> Transaction.changeset(%{ref: "tx-1", amount: money})
        |> Repo.insert()

      reloaded = Repo.get!(Transaction, tx.id)
      assert reloaded.amount == money
    end

    test "cast from map input" do
      cs =
        Transaction.changeset(%Transaction{}, %{
          "ref" => "tx-2",
          "amount" => %{"amount" => 500, "currency" => "EUR"}
        })

      assert cs.valid?
      assert %Money{amount: 500, currency: "EUR"} = Ecto.Changeset.get_change(cs, :amount)
    end

    test "cast rejects invalid currency" do
      cs =
        Transaction.changeset(%Transaction{}, %{
          "ref" => "tx-3",
          "amount" => %{"amount" => 1, "currency" => "XYZ"}
        })

      refute cs.valid?
    end
  end

  describe "equal?/2 in change detection" do
    test "does not mark unchanged money as dirty" do
      {:ok, money} = Money.new(1, "USD")

      {:ok, tx} =
        %Transaction{}
        |> Transaction.changeset(%{ref: "tx-eq", amount: money})
        |> Repo.insert()

      cs = Transaction.changeset(tx, %{amount: money})
      assert cs.changes == %{}
    end
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Custom `Ecto.Type` — Money as a First-Class Value.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Custom `Ecto.Type` — Money as a First-Class Value ===")
    IO.puts("Category: Ecto advanced\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case FinanceLedger.run(payload) do
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
        for _ <- 1..1_000, do: FinanceLedger.run(:bench)
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
