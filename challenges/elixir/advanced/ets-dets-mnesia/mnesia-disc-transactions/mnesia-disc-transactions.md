# Mnesia disc_copies, Transactions, and Lock Semantics

**Project**: `mnesia_disc_tx` — persistent distributed ledger with ACID transactions.

---

## The business problem

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

## Project structure

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
├── script/
│   └── main.exs
└── mix.exs
```

---

## Why Mnesia disc_copies and not external DB

An external DB is another service to run, monitor, and reach over the network. Mnesia lives in-process with ACID guarantees and replicates to other BEAM nodes directly.

---

## Design decisions

**Option A — SQL RDBMS with the app as client**
- Pros: mature tooling, well-understood semantics, SQL query power.
- Cons: network hop on every query; schema lives outside the code.

**Option B — Mnesia `disc_copies` tables with transactions** (chosen)
- Pros: in-process reads; BEAM-native transactions; schema is Elixir.
- Cons: operational maturity gap vs Postgres; no SQL; tooling is sparse.

→ Chose **B** because for a BEAM-native service where data stays inside the cluster, the locality and integration wins.

---

## Implementation

### `mix.exs`
```elixir
defmodule MnesiaDiscTransactions.MixProject do
  use Mix.Project

  def project do
    [
      app: :mnesia_disc_transactions,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    []
  end
end
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

```
transaction/1:       commit returns as soon as the local node commits.
                     Replicas apply asynchronously (but still before the
                     next transaction sees the new state).

sync_transaction/1:  commit returns only after every replica has fsynced
                     the log. Use when a caller-observed commit must
                     survive immediate node death.
```

For financial transfers `sync_transaction/1` is the correct default.

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

## Deep Dive

ETS (Erlang Term Storage) is RAM-only and process-linked; table destruction triggers if the owner crashes, causing silent data loss in careless designs. Match specifications (match_specs) are micro-programs that filter/transform data at the C layer, orders of magnitude faster than fetching all records and filtering in Elixir. Mnesia adds disk persistence and replication but introduces transaction overhead and deadlock potential; dirty operations bypass locks for speed but sacrifice consistency guarantees. For caching, named tables (public by design) are globally visible but require careful name management; consider ETS sharding (multiple small tables) to reduce lock contention on hot keys. DETS (Disk ETS) persists to disk but is single-process bottleneck and slower than a real database. At scale, prefer ETS for in-process state and Mnesia/PostgreSQL for shared, persistent data.
## Advanced Considerations

ETS and DETS performance characteristics change dramatically based on access patterns and table types. Ordered sets provide range queries but slower access than hash tables; set types don't support duplicate keys while bags do. The `heir` option for ETS tables is essential for fault tolerance — when a table owner crashes, the heir process can take ownership and prevent data loss. Without it, the table is lost immediately. Mnesia replicates entire tables across nodes; choosing which nodes should have replicas and whether they're RAM or disk replicas affects both consistency guarantees and network traffic during cluster operations.

DETS persistence comes with significant performance implications — writes are synchronous to disk by default, creating latency spikes. Using `sync: false` improves throughput but risks data loss on crashes. The maximum DETS table size is limited by available memory and the file system; planning capacity requires understanding your growth patterns. Mnesia's transaction system provides ACID guarantees, but dirty operations bypass these guarantees for performance. Understanding when to use dirty reads versus transactional reads significantly impacts both correctness and latency.

Debugging ETS and DETS issues is challenging because problems often emerge under load when many processes contend for the same table. Table memory fragmentation is invisible to code but can exhaust memory. Using match specs instead of iteration over large tables can dramatically improve performance but requires careful construction. The interaction between ETS, replication, and distributed systems creates subtle consistency issues — a node with a stale ETS replica can serve incorrect data during network partitions. Always monitor table sizes and replication status with structured logging.

## Deep Dive: Etsdets Patterns and Production Implications

ETS tables are in-memory, non-distributed key-value stores with tunable semantics (ordered_set, duplicate_bag). Under concurrent read/write load, ETS table semantics matter: bag semantics allow fast appends but slow deletes; ordered_set allows range queries but slower inserts. Testing ETS behavior under concurrent load is non-trivial; single-threaded tests miss lock contention. Production ETS tables often fail under load due to concurrency assumptions that quiet tests don't exercise.

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

## Reflection

- Your dataset outgrows RAM. `disc_only_copies` is an option. What does that cost on the read path, and is it still better than Postgres?
- How do you back up a Mnesia cluster without stopping it, and what do you wish Postgres tooling gave you that Mnesia does not?

---

### `script/main.exs`
```elixir
defmodule MnesiaDiscTx.MixProject do
  use Mix.Project

  def project do
    [app: :mnesia_disc_tx, version: "0.1.0", elixir: "~> 1.19", deps: deps()]
  end

  def application do
    [extra_applications: [:logger, :mnesia], mod: {MnesiaDiscTx.Application, []}]
  end

  defp deps do
    [{:benchee, "~> 1.3", only: :dev}]
  end
end

If you use `:read` here, you have a TOCTOU bug. Mnesia will not warn you —
both transactions commit successfully with corrupt balances.

Two transactions acquire locks in opposite order:

Mnesia detects this, aborts one transaction with `{:aborted, {:cyclic, ...}}`,
and **automatically retries the function**. Your transaction body can therefore
execute multiple times — it must be idempotent. Never perform side effects
(HTTP calls, file writes, sending messages to non-transactional processes)
inside a `:mnesia.transaction/1`.

For financial transfers `sync_transaction/1` is the correct default.

`disc_copies` appends every committed transaction to `LATEST.LOG`. Mnesia
periodically dumps the log into the table file and truncates it, but this
only happens on clean shutdown by default. In long-running production
systems you need to trigger compaction explicitly:

defmodule Main do
  def main do
      # Demonstrating 122-mnesia-disc-transactions
      :ok
  end
end

Main.main()
```

---

## Why Mnesia disc_copies, Transactions, and Lock Semantics matters

Mastering **Mnesia disc_copies, Transactions, and Lock Semantics** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `lib/mnesia_disc_tx.ex`

```elixir
defmodule MnesiaDiscTx do
  @moduledoc """
  Reference implementation for Mnesia disc_copies, Transactions, and Lock Semantics.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the mnesia_disc_tx module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> MnesiaDiscTx.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```

### `test/mnesia_disc_tx_test.exs`

```elixir
defmodule MnesiaDiscTxTest do
  use ExUnit.Case, async: true

  doctest MnesiaDiscTx

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert MnesiaDiscTx.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts

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
