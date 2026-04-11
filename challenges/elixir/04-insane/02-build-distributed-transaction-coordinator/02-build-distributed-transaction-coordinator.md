# Distributed Transaction Coordinator

**Project**: `dtx` — a distributed transaction coordinator with ACID semantics across partitions

---

## Project context

You are building `dtx`, a distributed transaction coordinator that provides ACID semantics across multiple independent key-value partitions. Each partition runs as a separate Erlang node with an embedded storage engine. The coordinator orchestrates multi-partition transactions without relying on any external database.

Project structure:

```
dtx/
├── lib/
│   └── dtx/
│       ├── application.ex           # starts coordinator and partition supervisor
│       ├── coordinator.ex           # GenServer: drives prepare/commit/abort decisions
│       ├── participant.ex           # GenServer per partition: votes, persists, applies
│       ├── storage.ex               # MVCC key-value store per partition
│       ├── wal.ex                   # write-ahead log: fsync before vote, replay on restart
│       ├── lock_manager.ex          # row-level locking with deadlock detection
│       ├── wait_for_graph.ex        # distributed wait-for graph, cycle detection
│       └── transaction.ex           # public API: begin/1, read/3, write/4, commit/1, rollback/1
├── test/
│   └── dtx/
│       ├── two_phase_commit_test.exs    # 2PC happy path and crash scenarios
│       ├── snapshot_isolation_test.exs  # no dirty reads, no lost updates
│       ├── deadlock_test.exs            # cycle detection and victim selection
│       ├── recovery_test.exs            # coordinator crash after prepare
│       └── banking_test.exs             # invariant: sum of all balances is constant
├── bench/
│   └── dtx_bench.exs
└── mix.exs
```

---

## The problem

You have 1,000 bank accounts distributed across 3 partitions. A transfer from account A (partition 1) to account B (partition 3) must be atomic: either both the debit and credit happen, or neither does. If the coordinator crashes between the debit and credit, the database must not be left in a state where money has vanished.

Two-phase commit (2PC) is the protocol that solves this. It is a blocking protocol — if the coordinator crashes at the wrong moment, participants can be left in a `prepared` state indefinitely, unable to commit or abort without coordinator recovery. Your WAL is the mechanism that makes recovery possible: the coordinator writes its decision before sending it, so it can re-derive the correct decision after restarting.

The second hard problem is concurrent transactions. Without isolation, two concurrent transfers might both read account X with balance 100, both decide to debit 50, both write 50, and the account ends up at 50 instead of 0. MVCC solves this by giving each transaction a consistent snapshot of committed data at its start time. Writers create new row versions; readers see only the version committed before their snapshot.

---

## Why this design

**WAL before vote**: a participant must write its prepared state to the WAL and fsync it before sending a YES vote. If the participant crashes and restarts, it replays the WAL and knows it voted YES. It must honor that vote and wait for the coordinator's final decision — it cannot unilaterally abort.

**Coordinator WAL at decision time**: the coordinator writes COMMIT or ABORT to its WAL before sending the decision to participants. On restart, it reads the WAL and re-sends the decision to any participant that has not yet acknowledged. This is the only mechanism that breaks the 2PC blocking condition.

**MVCC over locking for reads**: readers never block writers. Each transaction gets a snapshot timestamp at `begin`. Reads consult only versions committed before that timestamp. This eliminates read-write contention at the cost of garbage-collecting old versions.

**Distributed wait-for graph**: deadlock detection in a distributed system cannot rely on a local graph. Transaction T1 on node 1 might wait for a lock held by T2 on node 3, while T2 waits for T1. Neither node can see the full cycle. A dedicated graph process collects wait edges from all participants and runs cycle detection centrally.

---

## Implementation milestones

### Step 1: Create the project

```bash
mix new dtx --sup
cd dtx
mkdir -p lib/dtx test/dtx bench
```

### Step 2: `mix.exs` — dependencies

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev},
    {:stream_data, "~> 0.6", only: :test}
  ]
end
```

### Step 3: MVCC storage

```elixir
# lib/dtx/storage.ex
defmodule Dtx.Storage do
  @moduledoc """
  MVCC key-value store. Each row has a creation transaction ID and an
  expiration transaction ID. A row is visible to snapshot_xid if:
    created_xid < snapshot_xid AND (expired_xid == 0 OR expired_xid > snapshot_xid)
  """

  @doc """
  Reads the latest version of key visible to the given snapshot transaction ID.
  Returns {:ok, value} or {:error, :not_found}.
  """
  def read(table, key, snapshot_xid) do
    # TODO: :ets.lookup/2, filter by visibility rule
  end

  @doc """
  Writes a new version of key. Does not expire the old version yet —
  that happens on commit.
  """
  def write_version(table, key, value, txn_xid) do
    # TODO: insert {key, value, txn_xid, 0} into the versions table
  end

  @doc """
  Called on commit: expire the old version by setting its expired_xid.
  """
  def expire_old_version(table, key, commit_xid) do
    # TODO
  end

  @doc """
  GC: delete all versions where expired_xid > 0 AND expired_xid < horizon.
  horizon is min(all active snapshot_xids).
  """
  def garbage_collect(table, horizon) do
    # TODO
  end
end
```

### Step 4: Write-ahead log

```elixir
# lib/dtx/wal.ex
defmodule Dtx.WAL do
  @doc """
  Appends a log record and fsyncs before returning.
  The coordinator calls this for COMMIT/ABORT decisions.
  Participants call this before voting YES.
  """
  def append(path, record) do
    # TODO: write <<crc32::32, len::32, term_to_binary(record)::binary>>
    # TODO: :file.sync/1 after write
  end

  @doc """
  Replays the WAL from the beginning. Returns a list of records.
  Truncates at the first CRC mismatch (partial write on crash).
  """
  def replay(path) do
    # TODO
  end
end
```

### Step 5: Two-phase commit coordinator

```elixir
# lib/dtx/coordinator.ex
defmodule Dtx.Coordinator do
  use GenServer

  @doc """
  Runs a two-phase commit across the given participants.

  Phase 1 (Prepare): send prepare to all participants; collect votes.
  Phase 2 (Commit or Abort): if all voted YES, write COMMIT to WAL, send commit.
                              if any voted NO or timed out, write ABORT to WAL, send abort.
  """
  def commit(txn_id, participants, writes) do
    # TODO
    # HINT: write decision to WAL BEFORE sending to participants
    # HINT: handle the case where a participant is unreachable during phase 2
    #       — retry until ack; do not give up
  end

  @impl true
  def init(_opts) do
    # TODO: replay WAL on start; re-send decisions for uncommitted transactions
  end
end
```

### Step 6: Given tests — must pass without modification

```elixir
# test/dtx/two_phase_commit_test.exs
defmodule Dtx.TwoPhaseCommitTest do
  use ExUnit.Case, async: false

  setup do
    {:ok, db} = Dtx.start(partitions: 3)
    on_exit(fn -> Dtx.stop(db) end)
    {:ok, db: db}
  end

  test "happy path: transaction commits atomically across 3 partitions", %{db: db} do
    txn = Dtx.Transaction.begin(db)
    :ok = Dtx.Transaction.write(txn, :partition_1, "account:alice", 900)
    :ok = Dtx.Transaction.write(txn, :partition_2, "account:bob", 1100)
    :ok = Dtx.Transaction.commit(txn)

    assert {:ok, 900}  = Dtx.read(db, :partition_1, "account:alice")
    assert {:ok, 1100} = Dtx.read(db, :partition_2, "account:bob")
  end

  test "coordinator crash after prepare re-commits on restart", %{db: db} do
    txn = Dtx.Transaction.begin(db)
    :ok = Dtx.Transaction.write(txn, :partition_1, "crash_key", "value")

    # Simulate coordinator crash after prepare phase
    Dtx.TestHelpers.crash_coordinator_after_prepare(db, txn.id)

    # Restart the coordinator
    Dtx.TestHelpers.restart_coordinator(db)
    Process.sleep(500)

    # Key must be committed — coordinator must have recovered its decision
    assert {:ok, "value"} = Dtx.read(db, :partition_1, "crash_key")
  end

  test "participant crash before voting aborts the transaction", %{db: db} do
    Dtx.TestHelpers.kill_partition(db, :partition_2)

    txn = Dtx.Transaction.begin(db)
    :ok = Dtx.Transaction.write(txn, :partition_1, "no_commit", "x")
    :ok = Dtx.Transaction.write(txn, :partition_2, "no_commit", "x")

    assert {:error, :aborted} = Dtx.Transaction.commit(txn)
    assert {:error, :not_found} = Dtx.read(db, :partition_1, "no_commit")
  end
end
```

```elixir
# test/dtx/banking_test.exs
defmodule Dtx.BankingTest do
  use ExUnit.Case, async: false

  @accounts 1_000
  @initial_balance 1_000
  @transfers 10_000

  test "total balance is conserved across 10,000 concurrent transfers" do
    {:ok, db} = Dtx.start(partitions: 3)

    # Seed accounts
    for i <- 1..@accounts do
      partition = rem(i, 3) + 1
      :ok = Dtx.write(db, :"partition_#{partition}", "account:#{i}", @initial_balance)
    end

    # Run concurrent transfers
    tasks = for _ <- 1..@transfers do
      Task.async(fn ->
        from = :rand.uniform(@accounts)
        to   = :rand.uniform(@accounts)
        amount = :rand.uniform(100)
        Dtx.transfer(db, from, to, amount)
      end)
    end

    Task.await_many(tasks, 60_000)

    # Sum all balances
    total = for i <- 1..@accounts, reduce: 0 do
      acc ->
        partition = rem(i, 3) + 1
        {:ok, bal} = Dtx.read(db, :"partition_#{partition}", "account:#{i}")
        acc + bal
    end

    assert total == @accounts * @initial_balance,
      "invariant violated: expected #{@accounts * @initial_balance}, got #{total}"
  end
end
```

### Step 7: Run the tests

```bash
mix test test/dtx/ --trace
```

### Step 8: Benchmark

```elixir
# bench/dtx_bench.exs
{:ok, db} = Dtx.start(partitions: 3)

Benchee.run(
  %{
    "single-partition transaction" => fn ->
      txn = Dtx.Transaction.begin(db)
      Dtx.Transaction.write(txn, :partition_1, "bench", :rand.uniform(1_000_000))
      Dtx.Transaction.commit(txn)
    end,
    "cross-partition transaction (2 partitions)" => fn ->
      txn = Dtx.Transaction.begin(db)
      Dtx.Transaction.write(txn, :partition_1, "bench_a", :rand.uniform())
      Dtx.Transaction.write(txn, :partition_2, "bench_b", :rand.uniform())
      Dtx.Transaction.commit(txn)
    end
  },
  parallel: 20,
  time: 10,
  warmup: 3,
  formatters: [Benchee.Formatters.Console]
)
```

Target: 1,000 cross-partition transactions/second on a 3-node cluster on localhost.

---

## Trade-off analysis

| Aspect | 2PC (your impl) | Paxos Commit | Spanner TrueTime |
|--------|----------------|--------------|-----------------|
| Blocking condition | coordinator crash after prepare | never blocks (Paxos per participant) | never blocks (TrueTime bounds) |
| Latency | 2 round trips + fsync | 2+ round trips | 2 round trips + TrueTime wait |
| Clock dependency | none | none | atomic clocks required |
| Failure tolerance | coordinator WAL required | f+1 failures tolerated | globally distributed |
| Implementation complexity | moderate | high | impractical without atomic clocks |

Fill in measured latency and throughput from your benchmark.

Architectural question: 3PC was proposed to solve 2PC's blocking problem. Explain why 3PC still blocks under network partitions. Why does the industry still use 2PC despite this?

---

## Common production mistakes

**1. Not fsyncing the WAL before voting YES**
If a participant votes YES without persisting that decision, a crash and restart leaves it in an unknown state. It cannot reconstruct whether it voted YES and must conservatively abort — breaking atomicity.

**2. Coordinator sends decision before writing to WAL**
If the coordinator writes COMMIT to participants but crashes before writing it to its own WAL, a restart will not know whether to commit or abort. This is the classic coordinator failure scenario.

**3. Giving up on phase 2 message delivery**
Once the coordinator has decided (written to WAL), it must keep retrying phase 2 until all participants acknowledge. A participant stuck in `prepared` state must eventually be resolved. There is no timeout that is safe to abort after.

**4. Deadlock detector on the critical path**
Running deadlock detection synchronously on lock acquisition blocks every transaction waiting for a lock. The detector must run asynchronously on a timer, sampling the wait-for graph at configurable intervals.

**5. MVCC without garbage collection**
Old row versions accumulate indefinitely. In a long-running system with many short transactions, this becomes a memory leak. Track the oldest active snapshot and periodically purge versions invisible to all active transactions.

---

## Resources

- Gray, J. & Lamport, L. — *Consensus on Transaction Commit* — formal analysis of 2PC and Paxos Commit as an alternative
- Gray, J. & Reuter, A. — *Transaction Processing: Concepts and Techniques* (1992) — chapters 7–9 on 2PC, locking, and recovery are the canonical reference
- Corbett, J. et al. (2012). *Spanner: Google's Globally Distributed Database* — how TrueTime enables external consistency without 2PC blocking
- [PostgreSQL `twophase.c`](https://github.com/postgres/postgres/blob/master/src/backend/access/transam/twophase.c) — reference implementation of coordinator-side 2PC with WAL
- Bernstein, P. & Goodman, N. (1983). *Multiversion Concurrency Control* — the original MVCC paper
