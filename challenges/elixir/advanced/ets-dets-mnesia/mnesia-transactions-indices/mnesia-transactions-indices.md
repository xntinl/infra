# Mnesia Transactions, Dirty Reads, and Secondary Indices

**Project**: `account_ledger` — an accounting table where transfers between accounts must be atomic and idempotent, with fast lookup by either account id or a user-provided reference.

## The business problem

You track a ledger of account balances. A transfer from account A to account B must either fully apply (decrement A, increment B, append a history row) or not at all. Dirty reads are acceptable for monitoring dashboards that tolerate ≤1 s staleness, but are unacceptable for the transfer itself. You also need to look up transfers by an external `reference_id` (idempotency key) without a linear scan — that is a secondary index.

Mnesia is the BEAM-native transactional database. It sits above ETS (`:ram_copies`) and DETS (`:disc_copies`, `:disc_only_copies`), adds a transaction layer based on two-phase locking, and supports secondary indices. It is not a replacement for PostgreSQL — no SQL, no joins beyond hand-rolled code — but for BEAM-only workloads with simple relational shape, it is remarkably productive.

## Project structure

```
account_ledger/
├── lib/
│   └── account_ledger/
│       ├── application.ex
│       ├── schema.ex
│       └── ledger.ex
├── test/
│   └── account_ledger/
│       └── ledger_test.exs
├── bench/
│   └── ledger_bench.exs
├── script/
│   └── main.exs
└── mix.exs
```

## Why Mnesia and not PostgreSQL

If you already run Postgres, use Postgres. Choose Mnesia when:

- you cannot have a non-BEAM dependency (embedded devices, closed environments),
- latency matters more than SQL power (100–500 µs per Mnesia transaction vs 1–5 ms via Postgrex),
- your data model is tuple-shaped and does not benefit from joins,
- you want built-in distribution (`:ram_copies` across a cluster) without running a separate replication layer.

## Why transactions and not dirty operations for the transfer

A naive transfer:

```elixir
old_a = :mnesia.dirty_read({Account, "a"})  # read
:mnesia.dirty_write({Account, "a", old_a.balance - 100})  # write
```

Between the read and write, another process can modify account A. You deduct from a stale balance. Transactions serialize read-modify-write on contested keys using 2PL.

## Why a secondary index and not a manual lookup table

You could maintain `{reference_id, transfer_id}` in a separate table. Two tables, two writes, two deletes, risk of drift. Mnesia's `add_table_index/2` keeps a second index in sync automatically.

## Design decisions

- **Option A — ETS with a GenServer enforcing atomicity**: viable for single-node, fails at a cluster boundary.
- **Option B — Postgres + Ecto transactions**: canonical, requires Postgres. Our constraint forbids it.
- **Option C — Mnesia transactions + secondary index** (chosen): BEAM-native, atomic, replicated with `:ram_copies` across nodes.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule AccountLedger.MixProject do
  use Mix.Project

  def project do
    [app: :account_ledger, version: "0.1.0", elixir: "~> 1.19", deps: deps()]
  end

  def application do
    [extra_applications: [:logger, :mnesia], mod: {AccountLedger.Application, []}]
  end

  defp deps do
    [{:benchee, "~> 1.3", only: :dev}]
  end
end
```

### `mix.exs`
```elixir
```elixir
old_a = :mnesia.dirty_read({Account, "a"})  # read
:mnesia.dirty_write({Account, "a", old_a.balance - 100})  # write
```

Between the read and write, another process can modify account A. You deduct from a stale balance. Transactions serialize read-modify-write on contested keys using 2PL.

## Core concepts

---### 1. Mnesia tables

Created with `:mnesia.create_table/2`. Options include:

- `attributes: [list_of_field_names]` — positional, first is the key.
- `type: :set | :bag | :ordered_set`.
- `ram_copies: [nodes]` / `disc_copies: [nodes]` / `disc_only_copies: [nodes]`.

Records are tuples: `{TableName, key, field2, field3}`. Elixir structs do not map directly; use records via `Record` or pattern-match tuples.

`:mnesia.transaction/1` takes a zero-arg fun. Inside, use `:mnesia.read/1`, `:mnesia.write/1`, `:mnesia.delete/1`. On success the fun returns `{:atomic, value}`; on a detected deadlock or conflict, Mnesia aborts and **retries the fun automatically** — side-effects outside Mnesia must therefore be idempotent.

`:mnesia.dirty_read/1` and friends skip the transaction manager. They are 5–10× faster but have no consistency guarantees. Use only for approximate monitoring.

`:mnesia.add_table_index(Table, Attribute)` maintains a reverse lookup. Queries use `:mnesia.index_read(Table, Value, Attribute)`. Each index doubles write cost (main table + index) but turns O(n) scans into O(1) lookups.

## Data flow diagram

```
  Transfer API call: transfer("a", "b", 100, "ref_xyz")

    ┌─── :mnesia.transaction/1 ───────────────────────┐
    │                                                  │
    │  index_read :transfers by :reference_id          │
    │   └─ found? → return existing id (idempotent)    │
    │   └─ not found? continue                         │
    │                                                  │
    │  read :accounts "a" (acquires read-lock)         │
    │  read :accounts "b" (acquires read-lock)         │
    │                                                  │
    │  write :accounts "a" with new balance (→ w-lock) │
    │  write :accounts "b" with new balance (→ w-lock) │
    │  write :transfers record                         │
    │                                                  │
    │  On conflict with concurrent tx: abort + retry   │
    └──────────────────────────────────────────────────┘
              │
              ▼
    {:atomic, {:ok, "tx_..."}} | {:aborted, :insufficient_funds}
```

## Why this works

Mnesia's transaction manager uses two-phase locking with deadlock detection. On lock conflict, the transaction with the youngest wait-chain is aborted and retried. Because the whole fun is retried, side-effects outside Mnesia (logging, external calls) must be idempotent — hence the strict rule: **only mnesia operations inside the fun**. Idempotency of `transfer/4` is guaranteed by the secondary-index read on `:reference_id`: any repeated call with the same reference returns the same transfer id without re-executing the write.

## Tests

```elixir
# test/account_ledger/ledger_test.exs
defmodule AccountLedger.LedgerTest do
  use ExUnit.Case, async: false

  alias AccountLedger.Ledger

  setup do
    :mnesia.clear_table(:accounts)
    :mnesia.clear_table(:transfers)
    :ok = Ledger.open_account("alice", 1_000, "alice@ex")
    :ok = Ledger.open_account("bob", 0, "bob@ex")
    :ok
  end

  describe "transfer/4 — happy path" do
    test "moves money and creates a transfer record" do
      assert {:ok, tx_id} = Ledger.transfer("alice", "bob", 100, "ref_1")
      assert {:ok, 900} = Ledger.balance_strict("alice")
      assert {:ok, 100} = Ledger.balance_strict("bob")
      assert String.starts_with?(tx_id, "tx_")
    end
  end

  describe "transfer/4 — idempotency" do
    test "same reference_id returns the same transfer id without double-charging" do
      {:ok, id1} = Ledger.transfer("alice", "bob", 100, "ref_idem")
      {:ok, id2} = Ledger.transfer("alice", "bob", 100, "ref_idem")

      assert id1 == id2
      assert {:ok, 900} = Ledger.balance_strict("alice")
      assert {:ok, 100} = Ledger.balance_strict("bob")
    end
  end

  describe "transfer/4 — insufficient funds" do
    test "aborts with :insufficient_funds and rolls back balances" do
      assert {:error, :insufficient_funds} = Ledger.transfer("alice", "bob", 2_000, "ref_big")
      assert {:ok, 1_000} = Ledger.balance_strict("alice")
      assert {:ok, 0} = Ledger.balance_strict("bob")
    end
  end

  describe "find_by_reference/1 — secondary index" do
    test "retrieves the transfer via the reference index" do
      {:ok, id} = Ledger.transfer("alice", "bob", 50, "ref_lookup")
      {:ok, rec} = Ledger.find_by_reference("ref_lookup")
      assert elem(rec, 1) == id
    end

    test ":not_found for unknown reference" do
      assert :not_found = Ledger.find_by_reference("ghost")
    end
  end

  describe "balance/1 — dirty read" do
    test "returns a balance snapshot (possibly stale)" do
      assert {:ok, 1_000} = Ledger.balance("alice")
    end

    test ":not_found for missing account" do
      assert :not_found = Ledger.balance("nobody")
    end
  end
end
```

## Benchmark

```elixir
# bench/ledger_bench.exs
alias AccountLedger.Ledger

Ledger.open_account("bench_a", 1_000_000_000, "b")
Ledger.open_account("bench_b", 0, "b")

Benchee.run(
  %{
    "dirty read (balance/1)" => fn ->
      Ledger.balance("bench_a")
    end,
    "transactional read (balance_strict/1)" => fn ->
      Ledger.balance_strict("bench_a")
    end,
    "transfer/4 (transaction)" => fn ->
      ref = "bench_#{:erlang.unique_integer([:positive])}"
      Ledger.transfer("bench_a", "bench_b", 1, ref)
    end,
    "find_by_reference/1 (index)" => {
      fn ref -> Ledger.find_by_reference(ref) end,
      before_scenario: fn _ ->
        ref = "bench_scan_#{:erlang.unique_integer([:positive])}"
        Ledger.transfer("bench_a", "bench_b", 1, ref)
        ref
      end
    }
  },
  time: 5,
  warmup: 2
)
```

Target on a single node with `:ram_copies`: dirty read < 2 µs, transactional read < 50 µs, `transfer/4` under 300 µs uncontended, `find_by_reference` under 10 µs thanks to the index.

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

1. **Transaction funs are retried on conflict**: any non-Mnesia side effect (logging, message sending, external call) inside the fun happens multiple times. Keep side-effects strictly outside the fun.
2. **Dirty reads lie**: between the read and any subsequent decision you make, the row can change. Use `balance_strict` for anything that drives a decision (permissions, financial limits).
3. **Deadlocks are silent retries**: under sustained contention you see higher latency, not errors. Monitor `:mnesia.system_info(:transaction_restarts)`.
4. **Indices cost memory and write throughput**: each index roughly doubles the write path. Do not add indices on fields you query only once an hour.
5. **`:ram_copies` is lost on restart**: if all replicas go down simultaneously, data is gone. Use `:disc_copies` for durability.
6. **When NOT to use Mnesia**: multi-node write scaling past a handful of nodes; anything needing SQL; datasets that outgrow RAM on every replica.

## Reflection

You handle 5k transfers per second with a single-node Mnesia. Contention on a popular source account (hot key) causes transactional retries. What are your options within Mnesia to reduce contention, and when do you reach for an event-sourced log (e.g. Commanded) instead? What would the migration look like in terms of idempotency keys and reconciliation?

### `script/main.exs`
```elixir
defmodule AccountLedger.MixProject do
  use Mix.Project

  def project do
    [app: :account_ledger, version: "0.1.0", elixir: "~> 1.19", deps: deps()]
  end

  def application do
    [extra_applications: [:logger, :mnesia], mod: {AccountLedger.Application, []}]
  end

  defp deps do
    [{:benchee, "~> 1.3", only: :dev}]
  end
end

defmodule Main do
  def main do
      # Demonstrating 377-mnesia-transactions-indices
      :ok
  end
end

Main.main()
```

---

## Why Mnesia Transactions, Dirty Reads, and Secondary Indices matters

Mastering **Mnesia Transactions, Dirty Reads, and Secondary Indices** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `lib/account_ledger.ex`

```elixir
defmodule AccountLedger do
  @moduledoc """
  Reference implementation for Mnesia Transactions, Dirty Reads, and Secondary Indices.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the account_ledger module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> AccountLedger.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```

### `test/account_ledger_test.exs`

```elixir
defmodule AccountLedgerTest do
  use ExUnit.Case, async: true

  doctest AccountLedger

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert AccountLedger.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts

### 1. Mnesia tables

Created with `:mnesia.create_table/2`. Options include:

- `attributes: [list_of_field_names]` — positional, first is the key.
- `type: :set | :bag | :ordered_set`.
- `ram_copies: [nodes]` / `disc_copies: [nodes]` / `disc_only_copies: [nodes]`.

Records are tuples: `{TableName, key, field2, field3}`. Elixir structs do not map directly; use records via `Record` or pattern-match tuples.

### 2. Transactions

`:mnesia.transaction/1` takes a zero-arg fun. Inside, use `:mnesia.read/1`, `:mnesia.write/1`, `:mnesia.delete/1`. On success the fun returns `{:atomic, value}`; on a detected deadlock or conflict, Mnesia aborts and **retries the fun automatically** — side-effects outside Mnesia must therefore be idempotent.

### 3. Dirty operations

`:mnesia.dirty_read/1` and friends skip the transaction manager. They are 5–10× faster but have no consistency guarantees. Use only for approximate monitoring.

### 4. Secondary indices

`:mnesia.add_table_index(Table, Attribute)` maintains a reverse lookup. Queries use `:mnesia.index_read(Table, Value, Attribute)`. Each index doubles write cost (main table + index) but turns O(n) scans into O(1) lookups.
