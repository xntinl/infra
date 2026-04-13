# Custom `Ecto.Type` — Money as a First-Class Value

**Project**: `finance_ledger` — type-safe money handling with a custom Ecto type backed by a Postgres composite.

---

## Project context

Money in financial systems must never be a plain `float` (rounding) nor an `integer` of
cents alone (loses currency). A correct representation has two parts:

- **amount** — exact decimal or integer in minor units
- **currency** — ISO-4217 code (`USD`, `EUR`, `BRL`, ...)

Without a type, every function that touches money must remember to carry both. Miss one,
and you are adding dollars to euros. This exercise wraps amount + currency in a
`%Money{}` struct and teaches Ecto to load and dump it as a Postgres composite type, so
the compiler catches every leak.

```
finance_ledger/
├── lib/
│   └── finance_ledger/
│       ├── application.ex
│       ├── repo.ex
│       ├── money.ex                      # %Money{} struct + operations
│       ├── types/
│       │   └── money.ex                  # Ecto.Type implementation
│       └── schemas/
│           └── transaction.ex
├── priv/repo/migrations/
├── test/finance_ledger/
│   └── money_test.exs
├── bench/money_bench.exs
└── mix.exs
```

---

## Why a custom type and not two columns

Two columns (`amount_cents :: bigint`, `currency :: varchar(3)`) works but leaves semantics
in the application layer:

```elixir
# two columns — nothing prevents this
def add(a, b), do: %{amount_cents: a.amount_cents + b.amount_cents, currency: a.currency}
```

That function happily adds 100 USD and 100 EUR and returns "200 USD". A `%Money{}` struct
with an operator-checked `add/2` crashes on mismatched currencies. A custom Ecto type
ensures the struct is what you read and write — never a raw map.

---

## Why a Postgres composite and not two columns inside one type

Three storage options for a composite value:

- **Two columns with a single Ecto type** — the type's `load` reads both columns, `dump`
  writes both. Works, but requires every query to know the column pair.
- **JSONB** — `{"amount": 1234, "currency": "USD"}`. Flexible but no constraint on amount
  being integer.
- **Postgres composite type** — `CREATE TYPE money_t AS (amount bigint, currency char(3))`.
  Single column, typed fields, indexable via expression indexes.

We use **composite type**. It keeps the schema honest and lets Postgres enforce
`currency char(3)` at the storage layer.

---

## Core concepts

### 1. The `Ecto.Type` callbacks

A custom type implements:

```elixir
@behaviour Ecto.Type

def type, do: :money_t            # Postgres type name
def cast(any), do: ...            # user input → struct (changeset validation)
def load(raw), do: ...            # DB raw → struct (on read)
def dump(struct), do: ...         # struct → DB raw (on write)
def equal?(a, b), do: ...         # optional, used in change detection
def embed_as(_), do: :self        # embedded-schema marshaling
```

- `cast/1` is called during `cast/3` in a changeset — input may be a struct, a map, a
  tuple. Be liberal about what you accept.
- `load/1` is called after the Postgrex extension has decoded the raw row — you receive
  the extension's output, not bytes.
- `dump/1` is called on insert/update — you return what the extension's encoder expects.

### 2. Postgrex extensions for composite types

Postgrex does not natively know `money_t`. Two options:

- Register a custom extension using `Postgrex.Types.define/3`. Verbose, but supports
  composites transparently.
- Map the composite as a tuple on the wire (`{amount, currency}`) and coerce in
  `load`/`dump`.

We use the second approach: the column is typed `money_t` in the DB, but our Postgrex
connection uses the `:text` format fallback which returns a string we parse ourselves.
This keeps the type code self-contained and portable.

### 3. `cast/1` must handle multiple inputs

Controllers pass strings (`"12.34 USD"`). Changeset tests pass structs. Import scripts
pass maps. All must cast consistently:

```elixir
def cast(%Money{} = m), do: {:ok, m}
def cast(%{"amount" => a, "currency" => c}), do: Money.new(a, c)
def cast(str) when is_binary(str), do: parse(str)
def cast(_), do: :error
```

---

## Design decisions

- **Option A — store as bigint minor units + char(3)** in two columns: portable.
  Pros: standard SQL, indexable. Cons: two columns per money field clutters schemas.
- **Option B — Postgres composite type**: one column.
  Pros: single column, typed fields. Cons: Postgrex composite support needs extra work.

We adopt **Option B with a string-format dump/load** that avoids writing a Postgrex
extension and still yields a single column at the DB layer.

---

## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  [
    {:ecto_sql, "~> 3.12"},
    {:postgrex, "~> 0.19"},
    {:decimal, "~> 2.1"},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 1: Migration — composite type

**Objective**: Create Postgres composite type money_t(amount bigint, currency char(3)) so amounts and currency are atomically paired.

```elixir
# priv/repo/migrations/20260101000000_create_money_type.exs
defmodule FinanceLedger.Repo.Migrations.CreateMoneyType do
  use Ecto.Migration

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

  def down do
    drop table(:transactions)
    execute "DROP TYPE money_t"
  end
end
```

### Step 2: The `Money` struct

**Objective**: Implement %Money{} with add/2 guard raising on currency mismatches; zero-cost struct instead of raw maps.

```elixir
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

  @spec new(integer(), String.t()) :: {:ok, t()} | {:error, :invalid_currency}
  def new(amount, currency) when is_integer(amount) and is_binary(currency) do
    if currency in @currencies do
      {:ok, %__MODULE__{amount: amount, currency: currency}}
    else
      {:error, :invalid_currency}
    end
  end

  def new(amount, currency) when is_binary(amount) do
    case Integer.parse(amount) do
      {n, ""} -> new(n, currency)
      _ -> {:error, :invalid_amount}
    end
  end

  @spec add(t(), t()) :: t()
  def add(%__MODULE__{currency: c} = a, %__MODULE__{currency: c} = b),
    do: %__MODULE__{amount: a.amount + b.amount, currency: c}

  def add(%__MODULE__{currency: c1}, %__MODULE__{currency: c2}),
    do: raise(ArgumentError, "currency mismatch: #{c1} vs #{c2}")

  @spec to_string(t()) :: String.t()
  def to_string(%__MODULE__{amount: a, currency: c}), do: "#{a} #{c}"
end

defimpl String.Chars, for: FinanceLedger.Money do
  def to_string(m), do: FinanceLedger.Money.to_string(m)
end

defimpl Inspect, for: FinanceLedger.Money do
  def inspect(m, _opts), do: "#Money<#{m.amount} #{m.currency}>"
end
```

### Step 3: The `Ecto.Type` implementation

**Objective**: Implement @behaviour Ecto.Type: cast/load/dump parse/serialize wire format "(amount,currency)".

```elixir
# lib/finance_ledger/types/money.ex
defmodule FinanceLedger.Types.Money do
  @moduledoc """
  Ecto.Type for the Postgres composite `money_t`.

  Wire format: `"(1234,USD)"` string.
  """
  use Ecto.Type

  alias FinanceLedger.Money

  @impl true
  def type, do: :money_t

  # cast/1 — input from user / changeset
  @impl true
  def cast(%Money{} = m), do: {:ok, m}

  def cast(%{amount: a, currency: c}), do: Money.new(a, c) |> wrap()
  def cast(%{"amount" => a, "currency" => c}), do: Money.new(a, c) |> wrap()

  def cast(str) when is_binary(str) do
    case String.split(str, " ", parts: 2) do
      [amount, currency] -> Money.new(amount, currency) |> wrap()
      _ -> :error
    end
  end

  def cast(_), do: :error

  # load/1 — from DB into struct
  @impl true
  def load(str) when is_binary(str) do
    case Regex.run(~r/^\((-?\d+),(\w{3})\)$/, str) do
      [_, amount, currency] -> Money.new(String.to_integer(amount), currency) |> wrap()
      _ -> :error
    end
  end

  def load({amount, currency}) when is_integer(amount) and is_binary(currency) do
    Money.new(amount, currency) |> wrap()
  end

  def load(_), do: :error

  # dump/1 — struct to DB
  @impl true
  def dump(%Money{amount: a, currency: c}), do: {:ok, "(#{a},#{c})"}
  def dump(_), do: :error

  @impl true
  def equal?(%Money{} = a, %Money{} = b),
    do: a.amount == b.amount and a.currency == b.currency

  def equal?(_, _), do: false

  @impl true
  def embed_as(_), do: :self

  defp wrap({:ok, m}), do: {:ok, m}
  defp wrap({:error, _}), do: :error
end
```

### Step 4: Schema using the custom type

**Objective**: Bind `:amount` to the custom type so changeset errors surface invalid currencies before they reach Postgres.

```elixir
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

  def changeset(tx, attrs) do
    tx
    |> cast(attrs, [:ref, :amount])
    |> validate_required([:ref, :amount])
    |> unique_constraint(:ref)
  end
end
```

---

## Why this works

- `dump/1` emits Postgres composite text literal syntax (`(amount,currency)`). Postgres
  parses it into the `money_t` tuple on `INSERT`.
- `load/1` receives either a raw string or a pre-decoded tuple depending on Postgrex's
  type handling; both branches converge on `%Money{}`.
- `cast/1` accepts `"1234 USD"`, `%{"amount" => 1234, "currency" => "USD"}`, and
  `%Money{}`. All flows through the same `Money.new/2` validation.
- `equal?/2` ensures changeset change-detection does not see a no-op as an update.

The struct's `add/2` raises on currency mismatch — the bug you are guarding against is a
loud crash, not a silent miscount.

---

## Data flow

```
Phoenix controller params
   "amount" => "2500", "currency" => "USD"
       │
       ▼  Transaction.changeset(%Transaction{}, params)
   Money.cast/1 ──▶ {:ok, %Money{amount: 2500, currency: "USD"}}
       │
       ▼  Repo.insert
   Money.dump/1 ──▶ {:ok, "(2500,USD)"}
       │
       ▼
   Postgres: INSERT ... VALUES ('(2500,USD)'::money_t)

   Repo.get / SELECT
       ▼
   Money.load/1 ──▶ {:ok, %Money{amount: 2500, currency: "USD"}}
```

---

## Tests

```elixir
# test/finance_ledger/money_test.exs
defmodule FinanceLedger.MoneyTest do
  use ExUnit.Case, async: false
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

---

## Benchmark

```elixir
# bench/money_bench.exs
{:ok, m} = FinanceLedger.Money.new(12_345, "USD")

Benchee.run(
  %{
    "cast struct"  => fn -> FinanceLedger.Types.Money.cast(m) end,
    "cast map"     => fn -> FinanceLedger.Types.Money.cast(%{"amount" => 100, "currency" => "USD"}) end,
    "dump + load"  => fn ->
      {:ok, raw} = FinanceLedger.Types.Money.dump(m)
      FinanceLedger.Types.Money.load(raw)
    end
  },
  time: 3, warmup: 1
)
```

**Target**: `cast struct` < 300 ns, `dump + load` < 2 µs. If slower, check the regex in
`load/1` — compiling it per-call is a typical cause.

---

## Deep Dive

Ecto queries compile to SQL, but the translation is not always obvious. Complex preload patterns spawn subqueries for each association level—a naive nested preload can explode into hundreds of queries. Window functions and CTEs (Common Table Expressions) exist in Ecto but require raw fragments, making the boundary between Elixir and SQL explicit. For high-throughput systems, consider schemaless queries and streaming to defer memory allocation; loading 1M records as `Ecto.Repo.all/2` marshals everything into memory. Multi-tenancy via row-level database policies is cleaner than application-level filtering and leverages PostgreSQL's built-in enforcement. Zero-downtime migrations require careful orchestration: add columns before code that uses them, remove columns after code stops referencing them. Lock contention on hot rows kills throughput—use FOR UPDATE in transactions and understand when Ecto's optimistic locking is sufficient.
## Advanced Considerations

Advanced Ecto usage at scale requires understanding transaction semantics, locking strategies, and query performance under concurrent load. Ecto transactions are database transactions, not application-level transactions; they don't isolate against application-level concurrency issues. Using `:serializable` isolation level prevents anomalies but significantly impacts throughput. The choice between row-level locking with `for_update()` and optimistic locking with version columns affects both concurrency and latency. Deadlocks are not failures in Ecto; they're expected outcomes that require retry logic and careful key ordering to minimize.

Preload optimization is subtle — using `preload` for related data prevents N+1 queries but can create large intermediate result sets that exceed memory limits. Pagination with preloads requires careful consideration of whether to paginate before or after preloading related data. Custom types and schemaless queries provide flexibility but bypass Ecto's validation layer, creating opportunities for subtle bugs where invalid data sneaks into your database. The interaction between Ecto's change tracking and ETS caching can create stale data issues if not carefully managed across process boundaries.

Zero-downtime migrations require a different mental model than traditional migration scripts. Adding a column is fast; backfilling millions of rows is slow and can lock tables. Deploying code that expects the new column before the migration completes causes failures. Implement feature flags and dual-write patterns for truly zero-downtime deployments. Full-text search with PostgreSQL's tsearch requires careful index maintenance and stop-word configuration; performance characteristics change dramatically with language-specific settings and custom dictionaries.


## Deep Dive: Ecto Patterns and Production Implications

Ecto queries are composable, built up incrementally with pipes. Testing queries requires understanding that a query is lazy—until you call Repo.all, Repo.one, or Repo.update_all, no SQL is executed. This allows for property-based testing of query builders without hitting the database. Production bugs in complex queries often stem from incorrect scoping or ambiguous joins.

---

## Trade-offs and production gotchas

**1. Composite columns cannot be indexed directly.** `CREATE INDEX ON transactions(amount)`
is valid but rarely useful. Index on sub-fields: `CREATE INDEX ON transactions((amount).currency)`.

**2. `dump/1` is called for every changeset change, not only on insert.** A slow `dump`
quadratically affects bulk imports. Keep it branchless and avoid regex.

**3. Querying inside `money_t` needs `(col).field` syntax.** `where: t.amount.currency == ^"USD"`
is not supported by Ecto's query DSL — use `fragment("(?).currency", t.amount)`.

**4. Decimal amounts need a different approach.** If you need 4-decimal precision for FX
rates, switch to `Decimal` and store as `numeric(19,4)`. The composite's `bigint` field
is integer-only.

**5. Migrations that alter the composite type break all dependent columns.** Postgres
does not let you add a field to an in-use composite. Plan the shape carefully before you
ship, or use JSONB if you need evolution.

**6. When NOT to use a custom type.** If money never crosses currencies in your system
(single-currency ledger), two columns or a plain integer are simpler. The type's value is
proportional to the number of operations you guard.

---

## Reflection

You ship v1 with `money_t(bigint, char(3))`. A year later, regulation requires 4-decimal
precision (basis points). You cannot alter the composite in place. Sketch the migration
strategy: new type `money_v2_t`, dual-write window, backfill script, cutover. What is the
failure mode if an old process writes to the new column after cutover, and how do you
prevent it?

---

## Resources

- [`Ecto.Type` docs](https://hexdocs.pm/ecto/Ecto.Type.html)
- [Postgres composite types](https://www.postgresql.org/docs/current/rowtypes.html)
- [Postgrex — custom extensions](https://hexdocs.pm/postgrex/Postgrex.TypeModule.html)
- [`ex_money`](https://github.com/kipcole9/money) — production-grade money library, read the source

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```
