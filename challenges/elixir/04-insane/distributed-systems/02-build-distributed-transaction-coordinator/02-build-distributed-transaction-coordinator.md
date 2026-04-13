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

## Design decisions

**Option A — Distributed Paxos Commit (Gray & Lamport)**
- Pros: non-blocking on coordinator failure; each participant runs its own Paxos.
- Cons: 3–4 message delays per commit; significantly more code; requires a separate consensus group per participant.

**Option B — Classic two-phase commit with a WAL-backed coordinator** (chosen)
- Pros: 2-round-trip latency; linear in participants; the invariants (prepare → vote → commit record → ack) are simple to audit; a crashed coordinator is recoverable from the WAL.
- Cons: blocks participants between prepare and commit if the coordinator dies before the commit record is durable.

→ Chose **B** because the project's goal is to make the failure window explicit and auditable — 2PC's single point of blocking is easier to reason about and test than a multi-Paxos commit fabric.

---

## Implementation milestones

### Step 1: Create the project

**Objective**: Scaffold the distributed transaction coordinator Mix project with the required directory layout.

```bash
mix new dtx --sup
cd dtx
mkdir -p lib/dtx test/dtx bench
```

### Step 2: `mix.exs` — dependencies

**Objective**: Declare the Mix project configuration and third-party dependencies.

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev},
    {:stream_data, "~> 0.6", only: :test}
  ]
end
```

### Step 3: MVCC storage

**Objective**: Persist records durably to disk with fsync-on-commit semantics.

```elixir
# lib/dtx/storage.ex
defmodule Dtx.Storage do
  @moduledoc """
  MVCC key-value store. Each row has a creation transaction ID and an
  expiration transaction ID. A row is visible to snapshot_xid if:
    created_xid <= snapshot_xid AND (expired_xid == 0 OR expired_xid > snapshot_xid)

  Backed by ETS with :bag type to allow multiple versions per key.
  
  **Multiversion invariant:**
  - Only readers at or after snapshot_xid see the committed version
  - Writers at xid_w create a new version with created_xid = xid_w
  - Committed versions have expired_xid set on the next transaction's commit
  """

  @spec init(atom()) :: atom()
  def init(table_name) do
    :ets.new(table_name, [:named_table, :public, :bag])
    table_name
  end

  @doc """
  Reads the latest version of key visible to the given snapshot transaction ID.
  Returns {:ok, value} or {:error, :not_found}.
  
  Snapshot isolation: return the most recent version created before snapshot_xid.
  """
  @spec read(atom(), term(), pos_integer()) :: {:ok, term()} | {:error, :not_found}
  def read(table, key, snapshot_xid) do
    versions =
      :ets.lookup(table, key)
      |> Enum.filter(fn {_k, _v, created, expired} ->
        created <= snapshot_xid and (expired == 0 or expired > snapshot_xid)
      end)
      |> Enum.sort_by(fn {_k, _v, created, _expired} -> created end, :desc)

    case versions do
      [{_k, value, _created, _expired} | _] -> {:ok, value}
      [] -> {:error, :not_found}
    end
  end

  @doc """
  Writes a new version of key. Does not expire the old version yet —
  that happens on commit.
  """
  @spec write_version(atom(), term(), term(), pos_integer()) :: true
  def write_version(table, key, value, txn_xid) do
    :ets.insert(table, {key, value, txn_xid, 0})
  end

  @doc """
  Called on commit: expire the old version by setting its expired_xid.
  Marks the transaction ID as the expiration boundary for earlier versions.
  """
  @spec expire_old_version(atom(), term(), pos_integer()) :: :ok
  def expire_old_version(table, key, commit_xid) do
    versions = :ets.lookup(table, key)

    Enum.each(versions, fn {k, v, created, expired} = row ->
      if expired == 0 and created < commit_xid do
        :ets.delete_object(table, row)
        :ets.insert(table, {k, v, created, commit_xid})
      end
    end)

    :ok
  end

  @doc """
  GC: delete all versions where expired_xid > 0 AND expired_xid < horizon.
  horizon is min(all active snapshot_xids). Safe to delete old versions
  that no active transaction can see.
  """
  @spec garbage_collect(atom(), pos_integer()) :: non_neg_integer()
  def garbage_collect(table, horizon) do
    match_spec = [
      {{:_, :_, :_, :"$1"}, [{:andalso, {:>, :"$1", 0}, {:<, :"$1", horizon}}], [true]}
    ]
    :ets.select_delete(table, match_spec)
  end
end
```

### Step 4: Write-ahead log

**Objective**: Implement the durable write-ahead log that records every mutation before applying it.

```elixir
# lib/dtx/wal.ex
defmodule Dtx.WAL do
  @moduledoc """
  Write-ahead log with CRC32 integrity checks.
  Each record is framed as: <<crc32::32, len::32, payload::binary>>
  where payload is :erlang.term_to_binary(record).
  
  **Durability guarantee:**
  - Participant calls append before voting YES
  - Coordinator calls append before sending decision
  - On restart, replay entire WAL to reconstruct state
  - Truncate at first CRC mismatch (partial write due to crash)
  """

  @doc """
  Appends a log record and fsyncs before returning.
  The coordinator calls this for COMMIT/ABORT decisions.
  Participants call this before voting YES.
  """
  @spec append(Path.t(), term()) :: :ok
  def append(path, record) do
    payload = :erlang.term_to_binary(record)
    crc = :erlang.crc32(payload)
    frame = <<crc::32, byte_size(payload)::32, payload::binary>>

    {:ok, fd} = :file.open(path, [:append, :binary, :raw])
    :ok = :file.write(fd, frame)
    :ok = :file.sync(fd)
    :ok = :file.close(fd)
    :ok
  end

  @doc """
  Replays the WAL from the beginning. Returns a list of records.
  Truncates at the first CRC mismatch (partial write on crash).
  """
  @spec replay(Path.t()) :: [term()]
  def replay(path) do
    case File.read(path) do
      {:ok, data} -> decode_frames(data, [])
      {:error, :enoent} -> []
    end
  end

  defp decode_frames(<<crc::32, len::32, payload::binary-size(len), rest::binary>>, acc) do
    if :erlang.crc32(payload) == crc do
      record = :erlang.binary_to_term(payload)
      decode_frames(rest, [record | acc])
    else
      Enum.reverse(acc)
    end
  end

  defp decode_frames(_incomplete, acc), do: Enum.reverse(acc)
end
```

### Step 5: Two-phase commit coordinator

**Objective**: Coordinate two-phase commits across participants with prepare and commit phases.

```elixir
# lib/dtx/coordinator.ex
defmodule Dtx.Coordinator do
  use GenServer

  @moduledoc """
  Orchestrates two-phase commit across partitions.
  Writes decisions to WAL before sending them to participants.
  On restart, replays WAL to recover in-flight transactions.
  
  **2PC state machine:**
  1. (client request) → prepare all participants
  2. (collect votes) → if all YES, write COMMIT to WAL; else write ABORT
  3. (send decision) → send decision to all; retry until ack
  4. (ack collection) → on all acks, txn is complete
  """

  defstruct [:wal_path, :transactions, :participants]

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: Keyword.get(opts, :name, __MODULE__))
  end

  @doc """
  Runs a two-phase commit across the given participants.

  Phase 1 (Prepare): send prepare to all participants; collect votes.
  Phase 2 (Commit or Abort): if all voted YES, write COMMIT to WAL, send commit.
                              if any voted NO or timed out, write ABORT to WAL, send abort.
  """
  @spec commit(GenServer.server(), term(), [atom()], map()) :: :ok | {:error, :aborted}
  def commit(coordinator \\ __MODULE__, txn_id, participants, writes) do
    GenServer.call(coordinator, {:two_phase_commit, txn_id, participants, writes}, 30_000)
  end

  @impl true
  def init(opts) do
    wal_path = Keyword.get(opts, :wal_path, "dtx_coordinator.wal")
    participants = Keyword.get(opts, :participants, [])

    state = %__MODULE__{
      wal_path: wal_path,
      transactions: %{},
      participants: participants
    }

    recovered_state = recover_from_wal(state)
    {:ok, recovered_state}
  end

  @impl true
  def handle_call({:two_phase_commit, txn_id, participants, writes}, _from, state) do
    # Phase 1: Prepare
    votes =
      participants
      |> Task.async_stream(fn part ->
        try do
          GenServer.call(part, {:prepare, txn_id, Map.get(writes, part, %{})}, 5_000)
        catch
          :exit, _ -> :abort
        end
      end, timeout: 10_000)
      |> Enum.map(fn
        {:ok, result} -> result
        {:exit, _} -> :abort
      end)

    all_yes = Enum.all?(votes, fn v -> v == :yes end)

    # Phase 2: Decision
    decision = if all_yes, do: :commit, else: :abort
    Dtx.WAL.append(state.wal_path, {:decision, txn_id, decision, participants})

    Enum.each(participants, fn part ->
      Task.start(fn ->
        retry_until_ack(part, txn_id, decision)
      end)
    end)

    result = if decision == :commit, do: :ok, else: {:error, :aborted}
    {:reply, result, state}
  end

  defp retry_until_ack(participant, txn_id, decision) do
    try do
      GenServer.call(participant, {decision, txn_id}, 5_000)
    catch
      :exit, _ ->
        Process.sleep(500)
        retry_until_ack(participant, txn_id, decision)
    end
  end

  defp recover_from_wal(state) do
    records = Dtx.WAL.replay(state.wal_path)

    Enum.each(records, fn
      {:decision, txn_id, decision, participants} ->
        Enum.each(participants, fn part ->
          Task.start(fn -> retry_until_ack(part, txn_id, decision) end)
        end)
      _ -> :ok
    end)

    state
  end
end
```

### Step 6: Participant

**Objective**: Handle participant-side voting, locking, and durable decision persistence.

```elixir
# lib/dtx/participant.ex
defmodule Dtx.Participant do
  use GenServer

  @moduledoc """
  Per-partition participant in the 2PC protocol.
  Manages MVCC storage, WAL, and lock management.
  
  **Participant state machine:**
  - idle: waiting for prepare
  - prepared: voted YES, waiting for commit/abort decision
  - committed: applied writes to state machine
  - aborted: rolled back writes
  """

  defstruct [:id, :storage_table, :wal_path, :prepared, :xid_counter]

  def start_link(opts) do
    id = Keyword.fetch!(opts, :id)
    GenServer.start_link(__MODULE__, opts, name: id)
  end

  @impl true
  def init(opts) do
    id = Keyword.fetch!(opts, :id)
    table = Dtx.Storage.init(:"dtx_storage_#{id}")
    wal_path = "dtx_participant_#{id}.wal"

    state = %__MODULE__{
      id: id,
      storage_table: table,
      wal_path: wal_path,
      prepared: %{},
      xid_counter: 1
    }

    {:ok, state}
  end

  @impl true
  def handle_call({:prepare, txn_id, writes}, _from, state) do
    Dtx.WAL.append(state.wal_path, {:prepare, txn_id, writes})

    new_prepared = Map.put(state.prepared, txn_id, writes)
    {:reply, :yes, %{state | prepared: new_prepared}}
  end

  def handle_call({:commit, txn_id}, _from, state) do
    case Map.pop(state.prepared, txn_id) do
      {nil, _} ->
        {:reply, :ok, state}
      {writes, remaining} ->
        Enum.each(writes, fn {key, value} ->
          Dtx.Storage.write_version(state.storage_table, key, value, state.xid_counter)
        end)
        Dtx.WAL.append(state.wal_path, {:committed, txn_id})
        {:reply, :ok, %{state | prepared: remaining, xid_counter: state.xid_counter + 1}}
    end
  end

  def handle_call({:abort, txn_id}, _from, state) do
    {_writes, remaining} = Map.pop(state.prepared, txn_id, nil)
    Dtx.WAL.append(state.wal_path, {:aborted, txn_id})
    {:reply, :ok, %{state | prepared: remaining}}
  end

  def handle_call({:read, key, snapshot_xid}, _from, state) do
    result = Dtx.Storage.read(state.storage_table, key, snapshot_xid)
    {:reply, result, state}
  end

  def handle_call({:write, key, value}, _from, state) do
    Dtx.Storage.write_version(state.storage_table, key, value, state.xid_counter)
    {:reply, :ok, %{state | xid_counter: state.xid_counter + 1}}
  end
end
```

### Step 7: Transaction API

**Objective**: Model the transaction lifecycle from begin through commit or abort.

```elixir
# lib/dtx/transaction.ex
defmodule Dtx.Transaction do
  @moduledoc """
  Public API for distributed transactions.
  Provides begin/commit/rollback with MVCC snapshot isolation.
  
  **Transaction lifecycle:**
  1. begin(db): acquire a snapshot XID (transaction start point)
  2. read/write: buffer writes; reads consult snapshot
  3. commit(txn): initiate 2PC on written partitions
  """

  defstruct [:id, :db, :snapshot_xid, :write_set]

  @spec begin(term()) :: %__MODULE__{}
  def begin(db) do
    txn_id = :erlang.unique_integer([:positive, :monotonic])
    %__MODULE__{
      id: txn_id,
      db: db,
      snapshot_xid: txn_id,
      write_set: %{}
    }
  end

  @doc """
  Read a key from a partition. Use the transaction's snapshot XID
  to get a consistent view of committed data.
  """
  @spec read(%__MODULE__{}, atom(), term()) :: {:ok, term()} | {:error, :not_found}
  def read(%__MODULE__{} = txn, partition, key) do
    case Map.get(txn.write_set, {partition, key}) do
      nil -> GenServer.call(partition, {:read, key, txn.snapshot_xid})
      value -> {:ok, value}
    end
  end

  @doc "Buffer a write to a partition (not yet durable)."
  @spec write(%__MODULE__{}, atom(), term(), term()) :: {:ok, %__MODULE__{}}
  def write(%__MODULE__{} = txn, partition, key, value) do
    new_writes = Map.put(txn.write_set, {partition, key}, value)
    {:ok, %{txn | write_set: new_writes}}
  end

  @doc """
  Commit: group buffered writes by partition and initiate 2PC.
  All partitions must vote YES for the transaction to commit.
  """
  @spec commit(%__MODULE__{}) :: :ok | {:error, :aborted}
  def commit(%__MODULE__{} = txn) do
    writes_by_partition =
      txn.write_set
      |> Enum.group_by(fn {{part, _key}, _val} -> part end, fn {{_part, key}, val} -> {key, val} end)
      |> Map.new(fn {part, kvs} -> {part, Map.new(kvs)} end)

    participants = Map.keys(writes_by_partition)

    if participants == [] do
      :ok
    else
      Dtx.Coordinator.commit(txn.id, participants, writes_by_partition)
    end
  end

  @spec rollback(%__MODULE__{}) :: :ok
  def rollback(%__MODULE__{}), do: :ok
end
```

### Step 8: Public Dtx API

**Objective**: Implement the Public Dtx API component required by the distributed transaction coordinator system.

```elixir
# lib/dtx.ex
defmodule Dtx do
  @moduledoc """
  Top-level API for the distributed transaction system.
  Manages partition initialization and high-level operations.
  """

  def start(opts \\ []) do
    partitions = Keyword.get(opts, :partitions, 3)
    partition_ids = for i <- 1..partitions, do: :"partition_#{i}"

    children =
      Enum.map(partition_ids, fn id ->
        %{id: id, start: {Dtx.Participant, :start_link, [[id: id]]}}
      end) ++ [
        %{id: Dtx.Coordinator, start: {Dtx.Coordinator, :start_link, [[participants: partition_ids]]}}
      ]

    {:ok, sup} = Supervisor.start_link(children, strategy: :one_for_one)
    {:ok, %{supervisor: sup, partitions: partition_ids}}
  end

  def stop(%{supervisor: sup}), do: Supervisor.stop(sup)

  @doc "Read a key from a partition at a snapshot."
  def read(db, partition, key) do
    snapshot_xid = :erlang.unique_integer([:positive, :monotonic])
    GenServer.call(partition, {:read, key, snapshot_xid})
  end

  @doc "Write a key-value to a partition (for single-partition writes)."
  def write(db, partition, key, value) do
    GenServer.call(partition, {:write, key, value})
  end

  @doc """
  Transfer: a canonical 2PC scenario.
  Debit from_account on one partition, credit to_account on another.
  Atomicity is guaranteed by 2PC.
  """
  def transfer(db, from_account, to_account, amount) do
    from_partition = :"partition_#{rem(from_account, 3) + 1}"
    to_partition = :"partition_#{rem(to_account, 3) + 1}"

    txn = Dtx.Transaction.begin(db)

    with {:ok, from_balance} <- Dtx.Transaction.read(txn, from_partition, "account:#{from_account}"),
         {:ok, to_balance} <- Dtx.Transaction.read(txn, to_partition, "account:#{to_account}") do
      if from_balance >= amount do
        {:ok, txn} = Dtx.Transaction.write(txn, from_partition, "account:#{from_account}", from_balance - amount)
        {:ok, txn} = Dtx.Transaction.write(txn, to_partition, "account:#{to_account}", to_balance + amount)
        Dtx.Transaction.commit(txn)
      else
        Dtx.Transaction.rollback(txn)
        {:error, :insufficient_funds}
      end
    else
      {:error, _} = err ->
        Dtx.Transaction.rollback(txn)
        err
    end
  end
end
```

### Step 9: Test Helpers

**Objective**: Implement the Test Helpers component required by the distributed transaction coordinator system.

```elixir
# lib/dtx/test_helpers.ex
defmodule Dtx.TestHelpers do
  @moduledoc "Test helpers for simulating crashes and restarts."

  def crash_coordinator_after_prepare(db, txn_id) do
    Process.exit(Process.whereis(Dtx.Coordinator), :kill)
  end

  def restart_coordinator(db) do
    Dtx.Coordinator.start_link(name: Dtx.Coordinator, participants: [])
  end

  def kill_partition(db, partition_name) do
    case Process.whereis(partition_name) do
      nil -> :ok
      pid -> Process.exit(pid, :kill)
    end
  end
end
```

### Step 10: Given tests — must pass without modification

**Objective**: Validate behavior against the frozen test suite that must pass unmodified.

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
    {:ok, txn} = Dtx.Transaction.write(txn, :partition_1, "account:alice", 900)
    {:ok, txn} = Dtx.Transaction.write(txn, :partition_2, "account:bob", 1100)
    :ok = Dtx.Transaction.commit(txn)

    assert {:ok, 900}  = Dtx.read(db, :partition_1, "account:alice")
    assert {:ok, 1100} = Dtx.read(db, :partition_2, "account:bob")
  end

  test "coordinator crash after prepare re-commits on restart", %{db: db} do
    txn = Dtx.Transaction.begin(db)
    {:ok, txn} = Dtx.Transaction.write(txn, :partition_1, "crash_key", "value")

    Dtx.TestHelpers.crash_coordinator_after_prepare(db, txn.id)
    Dtx.TestHelpers.restart_coordinator(db)
    Process.sleep(500)

    assert {:ok, "value"} = Dtx.read(db, :partition_1, "crash_key")
  end

  test "participant crash before voting aborts the transaction", %{db: db} do
    Dtx.TestHelpers.kill_partition(db, :partition_2)

    txn = Dtx.Transaction.begin(db)
    {:ok, txn} = Dtx.Transaction.write(txn, :partition_1, "no_commit", "x")
    {:ok, txn} = Dtx.Transaction.write(txn, :partition_2, "no_commit", "x")

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

### Step 11: Run the tests

**Objective**: Execute the provided test suite to verify the implementation passes.

```bash
mix test test/dtx/ --trace
```

---

## Quick start

For a production deployment:

1. **Durability**: replace in-memory log with `:dets` for coordinator and participants
2. **Distribution**: replace GenServer.call with `:erpc` for cross-machine RPCs
3. **Isolation levels**: implement repeatable-read and serializable isolation
4. **Deadlock detection**: implement a separate process for cycle detection in the wait-for graph

---
## Main Entry Point

```elixir
def main do
  IO.puts("======== 02-build-distributed-transaction-coordinator ========")
  IO.puts("Build Distributed Transaction Coordinator")
  IO.puts("")
  
  Dtx.Storage.start_link([])
  IO.puts("Dtx.Storage started")
  
  IO.puts("Run: mix test")
end
```

