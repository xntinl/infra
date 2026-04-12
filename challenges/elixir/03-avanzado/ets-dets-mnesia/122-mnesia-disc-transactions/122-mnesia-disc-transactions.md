# Mnesia disc_copies, Transactions, and Lock Semantics

**Project**: `mnesia_disc_tx` — persistent distributed ledger with ACID transactions.
**Difficulty**: ★★★★☆
**Estimated time**: 3–6 hours

---

## Project context

A fintech-adjacent internal tool needs to record credit transfers between user
wallets. Every transfer must be atomic: debit A, credit B, or neither. The
data must survive node restarts and individual disk failures. Latency targets
are relaxed (under 50ms p99 is fine), but correctness is non-negotiable — an
orphan debit or a double credit becomes a support ticket.

Mnesia `disc_copies` gives you ACID transactions with synchronous replication.
Unlike `ram_copies`, `disc_copies` writes are logged to an on-disk transaction
log before the transaction commits, so a crash mid-write does not lose
committed transfers. The tradeoff is latency (fsync on the transaction log)
and operational complexity (log files grow and need periodic compaction).

This exercise digs into the transaction semantics that tutorials usually skip:
`:read` vs `:write` vs `:sticky_write` locks, the retry-on-conflict behavior,
the `transaction/1` vs `sync_transaction/1` vs `async_dirty/1` matrix, and how
to detect and avoid deadlocks.

```
mnesia_disc_tx/
├── lib/
│   └── mnesia_disc_tx/
│       ├── application.ex
│       ├── schema.ex
│       ├── ledger.ex               # transfer/3, balance/1
│       └── audit_log.ex            # bag table of transfer events
├── test/
│   └── mnesia_disc_tx/
│       ├── ledger_test.exs
│       └── concurrency_test.exs    # hammer transfer/3 in parallel
└── mix.exs
```

---

## Core concepts

### 1. Lock types

Mnesia offers four lock levels on records. Picking the wrong one is the
single most common source of Mnesia bugs.

| Lock            | Semantics                                             | When to use                         |
|-----------------|-------------------------------------------------------|-------------------------------------|
| `:read`         | shared — many readers, blocks writers                 | read-only transactions              |
| `:write`        | exclusive — blocks readers AND writers                | read-then-write patterns            |
| `:sticky_write` | like `:write` but pins the table to the caller node   | hot-path single-node optimisation   |
| `:none`         | no lock (dirty op)                                    | only via `dirty_*` functions        |

The critical rule: **every read inside a transaction that may be followed by
a write to the same record must use a `:write` lock**, not `:read`. Otherwise
two transactions both read the record concurrently, both decide to write,
one overwrites the other — the second update is lost.

### 2. The read-modify-write pattern

The canonical transfer transaction:

```elixir
:mnesia.transaction(fn ->
  [src] = :mnesia.read(:wallets, src_id, :write)   # <-- :write, not :read
  [dst] = :mnesia.read(:wallets, dst_id, :write)
  :mnesia.write(put_elem(src, 2, src_balance - amount))
  :mnesia.write(put_elem(dst, 2, dst_balance + amount))
end)
```

If you use `:read` here, you have a TOCTOU bug. Mnesia will not warn you —
both transactions commit successfully with corrupt balances.

### 3. Deadlocks and the restart mechanism

Two transactions acquire locks in opposite order:

```
T1: write-lock wallet A ────► tries write-lock B ... waiting
T2: write-lock wallet B ────► tries write-lock A ... waiting
```

Mnesia detects this, aborts one transaction with `{:aborted, {:cyclic, ...}}`,
and **automatically retries the function**. Your transaction body can therefore
execute multiple times — it must be idempotent. Never perform side effects
(HTTP calls, file writes, sending messages to non-transactional processes)
inside a `:mnesia.transaction/1`.

### 4. `transaction/1` vs `sync_transaction/1`

```
transaction/1:       commit returns as soon as the local node commits.
                     Replicas apply asynchronously (but still before the
                     next transaction sees the new state).

sync_transaction/1:  commit returns only after every replica has fsynced
                     the log. Use when a caller-observed commit must
                     survive immediate node death.
```

For financial transfers `sync_transaction/1` is the correct default.

### 5. Transaction log compaction

`disc_copies` appends every committed transaction to `LATEST.LOG`. Mnesia
periodically dumps the log into the table file and truncates it, but this
only happens on clean shutdown by default. In long-running production
systems you need to trigger compaction explicitly:

```elixir
:mnesia.dump_log()            # flush log → table file
```

Call it from a scheduled task — otherwise recovery after a crash replays
every transaction since the last dump.

---

## Implementation

### Step 1: `mix.exs`

```elixir
defmodule MnesiaDiscTx.MixProject do
  use Mix.Project

  def project do
    [app: :mnesia_disc_tx, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application do
    [extra_applications: [:logger, :mnesia], mod: {MnesiaDiscTx.Application, []}]
  end

  defp deps do
    [{:benchee, "~> 1.3", only: :dev}]
  end
end
```

Set Mnesia's data directory via `config/config.exs`:

```elixir
import Config
config :mnesia, dir: ~c"priv/mnesia_#{Mix.env()}_#{node()}"
```

### Step 2: `lib/mnesia_disc_tx/application.ex`

```elixir
defmodule MnesiaDiscTx.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [MnesiaDiscTx.Schema]
    Supervisor.start_link(children, strategy: :one_for_one, name: MnesiaDiscTx.Supervisor)
  end
end
```

### Step 3: `lib/mnesia_disc_tx/schema.ex`

```elixir
defmodule MnesiaDiscTx.Schema do
  @moduledoc false
  use GenServer
  require Logger

  @tables [:wallets, :transfer_log]

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(_opts) do
    :ok = ensure_schema()
    :ok = :mnesia.start()
    :ok = ensure_tables()
    :ok = :mnesia.wait_for_tables(@tables, 15_000)
    schedule_log_dump()
    Logger.info("Mnesia disc_tx ready")
    {:ok, %{}}
  end

  @impl true
  def handle_info(:dump_log, state) do
    :mnesia.dump_log()
    schedule_log_dump()
    {:noreply, state}
  end

  defp schedule_log_dump, do: Process.send_after(self(), :dump_log, :timer.minutes(5))

  defp ensure_schema do
    _ = :mnesia.stop()

    case :mnesia.create_schema([node()]) do
      :ok -> :ok
      {:error, {_, {:already_exists, _}}} -> :ok
      other -> throw({:schema_failed, other})
    end
  end

  defp ensure_tables do
    create(:wallets, attributes: [:id, :balance, :version], disc_copies: [node()], type: :set)

    create(:transfer_log,
      attributes: [:id, :from, :to, :amount, :timestamp],
      disc_copies: [node()],
      type: :bag
    )

    :ok
  end

  defp create(table, opts) do
    case :mnesia.create_table(table, opts) do
      {:atomic, :ok} -> :ok
      {:aborted, {:already_exists, ^table}} -> :ok
      other -> throw({:create_failed, table, other})
    end
  end
end
```

### Step 4: `lib/mnesia_disc_tx/ledger.ex`

```elixir
defmodule MnesiaDiscTx.Ledger do
  @moduledoc """
  ACID transfers between wallets.

  Locking strategy:
    * Reads inside a read-modify-write transaction use :write locks.
    * Lock acquisition order is deterministic (sorted wallet ids) to prevent
      cyclic deadlocks between concurrent transfers.
  """

  @type wallet_id :: String.t()
  @type amount :: pos_integer()

  @spec open_wallet(wallet_id(), non_neg_integer()) :: :ok | {:error, term()}
  def open_wallet(id, initial_balance \\ 0) do
    fun = fn -> :mnesia.write({:wallets, id, initial_balance, 0}) end

    case :mnesia.sync_transaction(fun) do
      {:atomic, :ok} -> :ok
      {:aborted, reason} -> {:error, reason}
    end
  end

  @spec balance(wallet_id()) :: {:ok, non_neg_integer()} | :not_found
  def balance(id) do
    case :mnesia.transaction(fn -> :mnesia.read(:wallets, id, :read) end) do
      {:atomic, [{:wallets, ^id, bal, _v}]} -> {:ok, bal}
      {:atomic, []} -> :not_found
    end
  end

  @spec transfer(wallet_id(), wallet_id(), amount()) ::
          :ok | {:error, :insufficient_funds | :unknown_wallet | term()}
  def transfer(from, to, amount) when from != to and amount > 0 do
    [lock_a, lock_b] = Enum.sort([from, to])

    fun = fn ->
      src = read_for_update(from, lock_a, lock_b)
      dst = read_for_update(to, lock_a, lock_b)

      cond do
        src == :not_found -> :mnesia.abort(:unknown_wallet)
        dst == :not_found -> :mnesia.abort(:unknown_wallet)
        elem(src, 2) < amount -> :mnesia.abort(:insufficient_funds)
        true -> apply_transfer(src, dst, amount)
      end
    end

    case :mnesia.sync_transaction(fun) do
      {:atomic, :ok} -> :ok
      {:aborted, reason} -> {:error, reason}
    end
  end

  def transfer(_, _, _), do: {:error, :invalid_arguments}

  # ---------------------------------------------------------------------------

  defp read_for_update(id, _a, _b) do
    # The caller has already sorted the two ids so deadlock-free order is
    # guaranteed. A :write lock is mandatory here — a :read lock would let
    # two concurrent transfers observe the same balance and both commit.
    case :mnesia.read(:wallets, id, :write) do
      [record] -> record
      [] -> :not_found
    end
  end

  defp apply_transfer({:wallets, from_id, from_bal, from_v} = _src,
                     {:wallets, to_id, to_bal, to_v} = _dst, amount) do
    :mnesia.write({:wallets, from_id, from_bal - amount, from_v + 1})
    :mnesia.write({:wallets, to_id, to_bal + amount, to_v + 1})

    log_entry =
      {:transfer_log, :erlang.unique_integer([:monotonic, :positive]), from_id, to_id,
       amount, System.system_time(:millisecond)}

    :mnesia.write(log_entry)
    :ok
  end
end
```

### Step 5: `test/mnesia_disc_tx/ledger_test.exs`

```elixir
defmodule MnesiaDiscTx.LedgerTest do
  use ExUnit.Case, async: false

  alias MnesiaDiscTx.Ledger

  setup do
    :mnesia.clear_table(:wallets)
    :mnesia.clear_table(:transfer_log)
    :ok
  end

  describe "transfer/3" do
    test "moves funds atomically" do
      Ledger.open_wallet("alice", 1_000)
      Ledger.open_wallet("bob", 0)

      assert :ok = Ledger.transfer("alice", "bob", 300)
      assert {:ok, 700} = Ledger.balance("alice")
      assert {:ok, 300} = Ledger.balance("bob")
    end

    test "rejects insufficient funds without mutating state" do
      Ledger.open_wallet("alice", 100)
      Ledger.open_wallet("bob", 0)

      assert {:error, :insufficient_funds} = Ledger.transfer("alice", "bob", 500)
      assert {:ok, 100} = Ledger.balance("alice")
      assert {:ok, 0} = Ledger.balance("bob")
    end

    test "rejects unknown wallets" do
      Ledger.open_wallet("alice", 100)
      assert {:error, :unknown_wallet} = Ledger.transfer("alice", "ghost", 10)
    end

    test "rejects zero or negative amounts" do
      Ledger.open_wallet("alice", 100)
      Ledger.open_wallet("bob", 0)
      assert {:error, :invalid_arguments} = Ledger.transfer("alice", "bob", 0)
      assert {:error, :invalid_arguments} = Ledger.transfer("alice", "bob", -5)
    end
  end
end
```

### Step 6: `test/mnesia_disc_tx/concurrency_test.exs`

```elixir
defmodule MnesiaDiscTx.ConcurrencyTest do
  use ExUnit.Case, async: false

  alias MnesiaDiscTx.Ledger

  @moduletag :concurrency

  setup do
    :mnesia.clear_table(:wallets)
    :mnesia.clear_table(:transfer_log)
    Ledger.open_wallet("alice", 10_000)
    Ledger.open_wallet("bob", 10_000)
    :ok
  end

  test "conservation of funds under parallel transfers" do
    # 200 concurrent transfers of 1 unit each in both directions.
    tasks =
      for i <- 1..200 do
        Task.async(fn ->
          if rem(i, 2) == 0 do
            Ledger.transfer("alice", "bob", 1)
          else
            Ledger.transfer("bob", "alice", 1)
          end
        end)
      end

    Enum.each(Task.await_many(tasks, 10_000), fn r -> assert r == :ok end)

    {:ok, alice} = Ledger.balance("alice")
    {:ok, bob} = Ledger.balance("bob")
    assert alice + bob == 20_000, "total funds must be conserved"
  end
end
```

### Step 7: Run

```bash
iex --name disc@127.0.0.1 -S mix
```

```elixir
MnesiaDiscTx.Ledger.open_wallet("alice", 1_000)
MnesiaDiscTx.Ledger.open_wallet("bob", 0)
MnesiaDiscTx.Ledger.transfer("alice", "bob", 250)
MnesiaDiscTx.Ledger.balance("bob")  # {:ok, 250}
```

Kill the node (`Ctrl+C Ctrl+C`) and restart. The wallets are still there —
`disc_copies` persisted them.

---

## Trade-offs and production gotchas

**1. `:read` inside a read-modify-write transaction is a bug.**
It looks innocuous in code review. Under load it silently corrupts balances.
Prefer `:write` locks any time the read might be followed by a write to the
same key in the same transaction.

**2. Transaction bodies retry — keep them pure.**
Never send messages to non-Mnesia processes, perform HTTP calls, or print
from inside `:mnesia.transaction/1`. Mnesia can legitimately retry your
function several times on lock conflict. Effects belong outside the
transaction, after `{:atomic, _}` is returned.

**3. Always acquire locks in a deterministic order.**
`Enum.sort([from, to])` inside `transfer/3` is not cosmetic — it prevents
deadlocks. Without it, under contention you will see
`{:aborted, {:cyclic, ...}}` and Mnesia will eventually abort one side.

**4. `sync_transaction/1` vs `transaction/1`.**
`transaction/1` returns as soon as the local commit succeeds and replicas
catch up asynchronously. If the caller node dies 100µs after return, a
replica may not have persisted yet. For financial state use
`sync_transaction/1`; reserve `transaction/1` for lower-stakes workflows.

**5. Transaction log growth.**
Without a periodic `:mnesia.dump_log/0`, the log grows unbounded. After a
crash, recovery replays every entry — startup can take minutes. Dump on a
cron-style schedule (every 5-15 minutes is typical).

**6. `set` vs `bag` vs `ordered_set`.**
The `:transfer_log` table is `:bag`: multiple entries per id are fine, and
writes are O(1). If you switch to `:ordered_set` for sorted iteration, every
write becomes O(log n) and range scans become cheap. For an append-only
audit log, `:bag` with a monotonic unique id is usually the right call.

**7. Mnesia is not Postgres.**
No foreign keys, no CHECK constraints, no JSONB operators, no `EXPLAIN`. You
own schema invariants in application code. This is fine for tightly-scoped
tables; do not reach for Mnesia as a general-purpose database.

**8. When NOT to use `disc_copies` with transactions.**
* Throughput target > ~10k transfers/sec — the coordinator serialises commits.
* Hot-key contention on a single record — fall back to CRDTs or event
  sourcing with a single-writer aggregate.
* Multi-datacenter deployment — transaction latency will dominate; use
  Postgres with logical replication or a purpose-built system (CockroachDB).
* Schema will evolve frequently — Mnesia migrations are painful; Ecto +
  Postgres is a better fit.

---

## Benchmark

```elixir
alias MnesiaDiscTx.Ledger

Ledger.open_wallet("src", 1_000_000_000)
Ledger.open_wallet("dst", 0)

Benchee.run(
  %{
    "transfer/3 (sync_transaction)" => fn -> Ledger.transfer("src", "dst", 1) end,
    "balance/1 (read-only tx)"      => fn -> Ledger.balance("src") end
  },
  parallel: 8,
  time: 10,
  warmup: 3
)
```

Expected on a single-node disc_copies setup (M1, NVMe, OTP 26):

| Operation          | p50    | p99    |
|--------------------|--------|--------|
| balance/1          | 15µs   | 40µs   |
| transfer/3         | 250µs  | 2.5ms  |

Doubling concurrency typically halves per-thread throughput once lock
contention hits the hot keys. The way to scale is to shard across many keys
or move to `mnesia_frag`.

---

## Resources

- [Mnesia transactions — erlang.org](https://www.erlang.org/doc/apps/mnesia/mnesia_chap4.html)
- [`:mnesia.sync_transaction/1` docs](https://www.erlang.org/doc/man/mnesia.html#sync_transaction-1)
- [Saša Jurić — Elixir in Action, 2nd ed, ch. "Complex processes"](https://pragprog.com/titles/sjelixir2/elixir-in-action-second-edition/) — transaction patterns
- [Ferd — Troubleshooting Mnesia deadlocks](https://ferd.ca/) — general distributed systems insight
- [OTP source: mnesia_tm.erl](https://github.com/erlang/otp/blob/master/lib/mnesia/src/mnesia_tm.erl) — the transaction manager
- [Designing for Scalability with Erlang/OTP — Cesarini & Vinoski, ch. 15](https://www.oreilly.com/library/view/designing-for-scalability/9781449361556/) — Mnesia in production
