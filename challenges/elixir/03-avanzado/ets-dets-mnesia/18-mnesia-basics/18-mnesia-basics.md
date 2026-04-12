# Mnesia basics: schemas, transactions, and storage types

**Project**: `mnesia_intro` — a single-node order book that introduces schema creation, transactions, dirty vs safe reads, and the `ram_copies` / `disc_copies` / `disc_only_copies` trade-off.

**Difficulty**: ★★★★☆
**Estimated time**: 3–6 hours

---

## Project context

You're prototyping an in-memory order book for a desk that trades maybe 50k orders/day. The
product team wants ACID-ish behavior (no partial updates), persistence across restarts, and the
option to scale to a two-node cluster later. Postgres is an option but the team wants to
evaluate Mnesia first because everything else is already BEAM.

Before writing distributed code you need to internalize Mnesia as a local engine: how the schema
lives on disk, how transactions differ from "dirty" operations, and what each storage type means
for durability and throughput. This exercise stays on a single node; distribution lands in a
later exercise.

The deliverable is an `OrderBook` module with `place/1`, `cancel/1`, `get/1`, and
`match_bids_for/1`, running against three Mnesia tables (`orders`, `executions`, `accounts`).
All writes are wrapped in `:mnesia.transaction/1`. Tests exercise rollback on error, dirty reads
for the hot path, and survival across a node restart.

```
mnesia_intro/
├── lib/
│   └── mnesia_intro/
│       ├── application.ex
│       ├── schema.ex
│       └── order_book.ex
├── test/
│   └── order_book_test.exs
└── mix.exs
```

---

## Core concepts

### 1. Schema, tables, storage types

Mnesia stores its schema in a file under `-mnesia dir`. Before you can create tables, you must
create the schema on the node (`:mnesia.create_schema/1`). The schema itself is a table with a
storage type.

Three storage types for tables:

| Storage             | RAM | Disk | Survives restart | Throughput          |
|---------------------|-----|------|------------------|---------------------|
| `ram_copies`        | yes | no   | no               | ETS-class           |
| `disc_copies`       | yes | yes  | yes              | ETS + async log     |
| `disc_only_copies`  | no  | yes  | yes              | DETS-class (slow)   |

`disc_copies` is the sweet spot: reads are RAM-speed (it's ETS underneath), writes are logged to
a transaction log plus periodic dumps to DETS. You get durability without paying DETS latency on
every read.

### 2. Transactions vs dirty operations

`:mnesia.transaction/1` wraps a function in ACID-ish semantics:

- Reads see a consistent snapshot.
- Writes are atomic: either all succeed or none does.
- Locks are acquired (`:read`, `:write`, `:sticky_write`) and released on commit/abort.
- If the function raises or returns via `:mnesia.abort/1`, the whole block is rolled back.

Dirty operations (`:mnesia.dirty_read`, `:mnesia.dirty_write`) skip the lock manager. They're
ETS-speed on `ram_copies` and `disc_copies`, at the cost of no isolation. Use them for
read-heavy hot paths where stale reads are tolerable.

```
  read()   -> 5 µs  (dirty)  | 40 µs (transaction)
  write()  -> 8 µs  (dirty)  | 80 µs (transaction, incl. log)
```

### 3. Record-based schema

Mnesia tables are schema-ed on Erlang records. In Elixir:

```elixir
require Record
Record.defrecord(:order, [:id, :side, :symbol, :qty, :price, :account_id, :status])
```

Each row is a tuple `{:order, id, side, symbol, qty, price, account_id, status}`. The first
element is the table name, the second is the primary key. Queries by primary key are O(1);
queries by other attributes need `:mnesia.index_read/3` (if you declared an index) or a match
spec.

### 4. Transaction retries

Mnesia may restart a transaction internally when it detects a deadlock. That means the function
you pass can run more than once. It must be idempotent: no `IO.puts`, no `send`, no external
effects, no accumulators held outside the function.

### 5. When Mnesia is not the answer

- Multi-datacenter replication: Mnesia assumes a low-latency cluster. Latency over WAN breaks
  its commit protocol.
- Large tables with high write rate: the transaction log becomes a bottleneck.
- Schema evolution: `:mnesia.transform_table/3` works but is painful compared to SQL migrations.
- Queries across many attributes: no planner, no indexes beyond the ones you declare.

---

## Implementation

### Step 1: `mix.exs`

```elixir
defmodule MnesiaIntro.MixProject do
  use Mix.Project

  def project do
    [
      app: :mnesia_intro,
      version: "0.1.0",
      elixir: "~> 1.16",
      start_permanent: Mix.env() == :prod,
      deps: []
    ]
  end

  def application do
    [
      extra_applications: [:logger, :mnesia],
      mod: {MnesiaIntro.Application, []}
    ]
  end
end
```

### Step 2: `lib/mnesia_intro/schema.ex`

```elixir
defmodule MnesiaIntro.Schema do
  @moduledoc """
  Creates the Mnesia schema and tables on this node if they do not exist.

  Call `ensure!/0` at application startup. It's idempotent: running it twice
  is harmless.
  """
  require Record
  require Logger

  Record.defrecord(:order, [:id, :side, :symbol, :qty, :price, :account_id, :status])
  Record.defrecord(:execution, [:id, :order_id, :qty, :price, :ts])
  Record.defrecord(:account, [:id, :balance, :updated_at])

  @spec ensure!() :: :ok
  def ensure! do
    :stopped = :mnesia.stop()
    create_schema()
    :ok = :mnesia.start()
    ensure_table(:order, [:id, :side, :symbol, :qty, :price, :account_id, :status], [:symbol])
    ensure_table(:execution, [:id, :order_id, :qty, :price, :ts], [:order_id])
    ensure_table(:account, [:id, :balance, :updated_at], [])
    :ok = :mnesia.wait_for_tables([:order, :execution, :account], 5_000)
    :ok
  end

  defp create_schema do
    case :mnesia.create_schema([node()]) do
      :ok -> :ok
      {:error, {_node, {:already_exists, _}}} -> :ok
      {:error, reason} -> raise "mnesia schema creation failed: #{inspect(reason)}"
    end
  end

  defp ensure_table(name, attrs, index_attrs) do
    opts = [
      attributes: attrs,
      disc_copies: [node()],
      index: index_attrs,
      type: :set
    ]

    case :mnesia.create_table(name, opts) do
      {:atomic, :ok} -> :ok
      {:aborted, {:already_exists, ^name}} -> :ok
      {:aborted, reason} -> raise "create_table #{name} failed: #{inspect(reason)}"
    end
  end
end
```

### Step 3: `lib/mnesia_intro/application.ex`

```elixir
defmodule MnesiaIntro.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    MnesiaIntro.Schema.ensure!()
    Supervisor.start_link([], strategy: :one_for_one, name: MnesiaIntro.Supervisor)
  end
end
```

### Step 4: `lib/mnesia_intro/order_book.ex`

```elixir
defmodule MnesiaIntro.OrderBook do
  @moduledoc """
  Order book operations wrapped in Mnesia transactions.

  Side conventions: :buy or :sell. qty and price are positive integers
  (cents and shares — no floats, ever, in financial code).
  """
  require MnesiaIntro.Schema
  import MnesiaIntro.Schema, only: [order: 1, order: 2, execution: 1]

  @type side :: :buy | :sell
  @type order_id :: pos_integer()

  @spec place(map()) :: {:ok, order_id()} | {:error, term()}
  def place(%{side: side, symbol: sym, qty: qty, price: price, account_id: acct})
      when side in [:buy, :sell] and qty > 0 and price > 0 do
    id = :erlang.unique_integer([:positive, :monotonic])

    txn = fn ->
      ensure_account!(acct)

      row =
        order(id: id, side: side, symbol: sym, qty: qty, price: price,
              account_id: acct, status: :open)

      :ok = :mnesia.write(row)
      id
    end

    case :mnesia.transaction(txn) do
      {:atomic, id} -> {:ok, id}
      {:aborted, reason} -> {:error, reason}
    end
  end

  @spec cancel(order_id()) :: :ok | {:error, term()}
  def cancel(id) do
    txn = fn ->
      case :mnesia.read({:order, id}) do
        [] -> :mnesia.abort(:not_found)
        [row] ->
          :mnesia.write(order(row, status: :cancelled))
      end
    end

    case :mnesia.transaction(txn) do
      {:atomic, :ok} -> :ok
      {:aborted, reason} -> {:error, reason}
    end
  end

  @doc """
  Dirty read of a single order — used for hot-path lookups where eventual
  consistency is acceptable.
  """
  @spec get(order_id()) :: {:ok, tuple()} | :error
  def get(id) do
    case :mnesia.dirty_read({:order, id}) do
      [row] -> {:ok, row}
      [] -> :error
    end
  end

  @doc """
  Returns all open buy orders for `symbol`, ordered descending by price.
  Uses an index read (declared on :symbol) for efficient filtering.
  """
  @spec match_bids_for(String.t()) :: [tuple()]
  def match_bids_for(symbol) do
    txn = fn -> :mnesia.index_read(:order, symbol, :symbol) end

    case :mnesia.transaction(txn) do
      {:atomic, rows} ->
        rows
        |> Enum.filter(&match?(order(side: :buy, status: :open), &1))
        |> Enum.sort_by(&order(&1, :price), :desc)

      {:aborted, _} ->
        []
    end
  end

  @spec record_execution(order_id(), pos_integer(), pos_integer()) :: :ok | {:error, term()}
  def record_execution(order_id, qty, price) do
    txn = fn ->
      case :mnesia.read({:order, order_id}) do
        [] ->
          :mnesia.abort(:order_not_found)

        [row] ->
          remaining = order(row, :qty) - qty

          if remaining < 0 do
            :mnesia.abort(:overfill)
          else
            exec =
              execution(id: :erlang.unique_integer([:positive, :monotonic]),
                        order_id: order_id, qty: qty, price: price,
                        ts: System.system_time(:millisecond))

            :mnesia.write(exec)

            new_status = if remaining == 0, do: :filled, else: :open
            :mnesia.write(order(row, qty: remaining, status: new_status))
            :ok
          end
      end
    end

    case :mnesia.transaction(txn) do
      {:atomic, :ok} -> :ok
      {:aborted, reason} -> {:error, reason}
    end
  end

  defp ensure_account!(id) do
    case :mnesia.read({:account, id}) do
      [] ->
        :mnesia.write({:account, id, 0, System.system_time(:millisecond)})

      [_] ->
        :ok
    end
  end
end
```

### Step 5: `test/order_book_test.exs`

```elixir
defmodule MnesiaIntro.OrderBookTest do
  use ExUnit.Case, async: false

  alias MnesiaIntro.OrderBook

  setup do
    # Clean tables before each test — transactional wipe.
    :mnesia.clear_table(:order)
    :mnesia.clear_table(:execution)
    :mnesia.clear_table(:account)
    :ok
  end

  describe "place/1" do
    test "persists a new order and returns its id" do
      {:ok, id} = OrderBook.place(%{side: :buy, symbol: "AAPL", qty: 10, price: 19_050, account_id: 1})
      assert is_integer(id)
      assert {:ok, _} = OrderBook.get(id)
    end

    test "creates the account row on first use" do
      {:ok, _} = OrderBook.place(%{side: :sell, symbol: "MSFT", qty: 5, price: 40_100, account_id: 42})
      assert [_row] = :mnesia.dirty_read({:account, 42})
    end
  end

  describe "cancel/1" do
    test "flips status to :cancelled" do
      {:ok, id} = OrderBook.place(%{side: :buy, symbol: "AAPL", qty: 10, price: 19_000, account_id: 1})
      assert :ok = OrderBook.cancel(id)

      {:ok, row} = OrderBook.get(id)
      # Row is a record tuple: {:order, id, side, symbol, qty, price, account_id, status}
      assert elem(row, 7) == :cancelled
    end

    test "returns :not_found when the id does not exist" do
      assert {:error, :not_found} = OrderBook.cancel(99_999_999)
    end
  end

  describe "record_execution/3 — transactional consistency" do
    test "decrements qty and marks filled atomically" do
      {:ok, id} = OrderBook.place(%{side: :sell, symbol: "GOOG", qty: 4, price: 150_000, account_id: 7})
      :ok = OrderBook.record_execution(id, 4, 150_000)

      {:ok, row} = OrderBook.get(id)
      assert elem(row, 4) == 0           # qty
      assert elem(row, 7) == :filled     # status
    end

    test "aborts on overfill and leaves no execution row" do
      {:ok, id} = OrderBook.place(%{side: :sell, symbol: "GOOG", qty: 2, price: 150_000, account_id: 7})
      assert {:error, :overfill} = OrderBook.record_execution(id, 10, 150_000)

      assert :mnesia.dirty_read({:execution, id}) == []
      {:ok, row} = OrderBook.get(id)
      assert elem(row, 4) == 2  # untouched
    end
  end

  describe "match_bids_for/1" do
    test "returns only open buys for the symbol, sorted by price desc" do
      {:ok, _} = OrderBook.place(%{side: :buy, symbol: "AAPL", qty: 1, price: 100, account_id: 1})
      {:ok, _} = OrderBook.place(%{side: :buy, symbol: "AAPL", qty: 1, price: 200, account_id: 1})
      {:ok, _} = OrderBook.place(%{side: :sell, symbol: "AAPL", qty: 1, price: 300, account_id: 1})

      prices = OrderBook.match_bids_for("AAPL") |> Enum.map(&elem(&1, 5))
      assert prices == [200, 100]
    end
  end
end
```

### Step 6: Run it

```bash
mix deps.get
mix test
```

First run creates `Mnesia.nonode@nohost/` directory under the project root. Delete it to start
fresh. In production set the directory explicitly:

```bash
iex --erl '-mnesia dir "\"/var/lib/my_app/mnesia\""' -S mix
```

---

## Trade-offs and production gotchas

**1. The transaction function must be idempotent.** Mnesia may retry it on deadlock. Any side
effect (IO, send, calls to external services) runs twice. Do effects **after** the transaction
commits.

**2. `dirty_read` is fast but sees uncommitted writes from other transactions.** In the middle of
a transaction, a dirty read by another process can see half-applied state. Use only when stale /
inconsistent data is acceptable.

**3. `disc_copies` doubles RAM usage.** Data lives in ETS and in the DETS log. If the table is
10 GB you need 10+ GB of RAM. Switch to `disc_only_copies` when that's untenable — at the cost of
DETS-level read latency.

**4. Schema lives on disk.** If you rename a table in code but the file still has the old name,
you get `{:aborted, {:no_exists, :foo}}`. Either migrate (`:mnesia.transform_table`) or delete the
schema directory and let the code recreate it (development only).

**5. Indexes are not free.** Each declared index adds an internal table. Writes must update the
index. A three-index table has ~4x the write cost. Index only what you query on.

**6. `:mnesia.start/0` is asynchronous.** Tables aren't ready immediately. Always call
`:mnesia.wait_for_tables/2` before issuing reads at boot — skipping it produces sporadic
`{:aborted, {:no_exists, table}}`.

**7. When NOT to use Mnesia.** Anything with strict multi-DC replication, anything that needs
SQL-class query planning, anything with > 100M rows. Mnesia shines in BEAM-shaped clusters of
2–10 nodes with "small" datasets.

---

## Resources

- [`:mnesia` user's guide](https://www.erlang.org/doc/apps/mnesia/users_guide.html)
- [`:mnesia` reference](https://www.erlang.org/doc/man/mnesia.html)
- [`Record` — Elixir stdlib](https://hexdocs.pm/elixir/Record.html) — how to use Erlang records idiomatically
- [Mnesia in 3 examples — Saša Jurić](https://www.theerlangelist.com/article/mnesia_1)
- [Elixir School — Mnesia](https://elixirschool.com/en/lessons/storage/mnesia)
- [Ulf Wiger — "How we do Mnesia at Klarna"](https://www.youtube.com/watch?v=HQnfDpTGSJg)
