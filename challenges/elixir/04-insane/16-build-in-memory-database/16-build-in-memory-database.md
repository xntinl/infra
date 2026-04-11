# In-Memory Database with MVCC and SQL Subset

**Project**: `memdb` — an in-memory relational database with MVCC, B-tree indexes, and serializable isolation

---

## Project context

You are building `memdb`, a fully featured in-memory relational database engine in Elixir. The database supports a subset of SQL via an Elixir API, provides true MVCC isolation so readers never block writers, detects deadlocks via a wait-for graph, and garbage-collects old row versions automatically.

Project structure:

```
memdb/
├── lib/
│   └── memdb/
│       ├── application.ex           # database supervisor
│       ├── database.ex              # public API: create_table, insert, select, update, delete, begin, commit, rollback
│       ├── table.ex                 # schema definition, column types, constraint enforcement
│       ├── mvcc.ex                  # row versioning: created_xid, expired_xid, visibility rules
│       ├── btree.ex                 # B-tree index: insert, delete, range scan
│       ├── query_planner.ex         # cost-based choice: index scan vs full table scan
│       ├── transaction.ex           # BEGIN/COMMIT/ROLLBACK, snapshot XID, write set
│       ├── lock_manager.ex          # row-level locking: acquire, release, wait
│       ├── wait_for_graph.ex        # distributed wait-for graph, deadlock detection (DFS)
│       └── gc.ex                    # background: prune versions invisible to all active txns
├── test/
│   └── memdb/
│       ├── crud_test.exs            # insert, select, update, delete correctness
│       ├── mvcc_test.exs            # snapshot isolation, no dirty reads, no lost updates
│       ├── btree_test.exs           # index operations, range queries
│       ├── deadlock_test.exs        # cycle detection, victim selection
│       ├── gc_test.exs              # version pruning, dead row count
│       └── benchmark_test.exs       # 1M reads/s, 100k writes/s
├── bench/
│   └── memdb_bench.exs
└── mix.exs
```

---

## The problem

You need an in-memory database that supports concurrent read-write workloads without readers blocking writers or writers blocking readers. PostgreSQL solves this with MVCC. You will implement the same mechanism from scratch, including the visibility rules, the transaction ID counter, the garbage collector that prunes old versions, and the deadlock detector that breaks lock cycles.

---

## Why this design

**MVCC visibility rule**: each row has a `created_xid` (the transaction that created it) and an `expired_xid` (the transaction that deleted it, or 0 if the row is alive). A row is visible to a transaction with `snapshot_xid` if:
- `created_xid < snapshot_xid` (created before our snapshot), AND
- `expired_xid == 0 OR expired_xid > snapshot_xid` (not yet deleted as of our snapshot)

Writers create new row versions rather than updating in place. The old version gains an `expired_xid`.

**B-tree for index**: a B-tree provides O(log N) point lookups and O(log N + K) range scans where K is the number of results. For equality predicates, it outperforms a full table scan at selectivities below ~10%. The query planner estimates selectivity from table cardinality and makes the choice.

**Wait-for graph for deadlock detection**: when transaction T1 waits for a lock held by T2, an edge T1->T2 is added to the wait-for graph. Deadlock = cycle in the graph. The cycle detector runs DFS from each node and aborts the youngest transaction in the cycle.

**GC horizon**: the oldest active transaction's XID is the horizon. Any row version with `expired_xid > 0` and `expired_xid < horizon` is invisible to all current and future transactions — it can be safely deleted. The GC runs on a timer and walks the version chain, pruning below the horizon.

---

## Implementation milestones

### Step 1: Create the project

```bash
mix new memdb --sup
cd memdb
mkdir -p lib/memdb test/memdb bench
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

### Step 3: MVCC row storage

```elixir
# lib/memdb/mvcc.ex
defmodule Memdb.MVCC do
  @moduledoc """
  MVCC row version store backed by ETS.

  Row structure: {row_id, data_map, created_xid, expired_xid}

  ETS table type: :bag — multiple versions per row_id are allowed.
  """

  @doc "Inserts a new row version."
  @spec insert(atom(), term(), map(), pos_integer()) :: true
  def insert(table, row_id, data, txn_xid) do
    :ets.insert(table, {row_id, data, txn_xid, 0})
  end

  @doc "Marks existing versions of row_id as expired by txn_xid."
  @spec expire(atom(), term(), pos_integer()) :: :ok
  def expire(table, row_id, txn_xid) do
    versions = :ets.lookup(table, row_id)
    Enum.each(versions, fn {rid, data, created, expired} = row ->
      if expired == 0 and created < txn_xid do
        :ets.delete_object(table, row)
        :ets.insert(table, {rid, data, created, txn_xid})
      end
    end)
    :ok
  end

  @doc "Returns all versions of row_id visible to snapshot_xid."
  @spec visible_versions(atom(), term(), pos_integer()) :: [map()]
  def visible_versions(table, row_id, snapshot_xid) do
    :ets.lookup(table, row_id)
    |> Enum.filter(fn {_rid, _data, created, expired} ->
      created < snapshot_xid and (expired == 0 or expired > snapshot_xid)
    end)
    |> Enum.sort_by(fn {_, _, created, _} -> created end, :desc)
    |> Enum.map(fn {_rid, data, _created, _expired} -> data end)
  end

  @doc "Scans all rows visible to snapshot_xid."
  @spec scan(atom(), pos_integer()) :: [map()]
  def scan(table, snapshot_xid) do
    :ets.tab2list(table)
    |> Enum.filter(fn {_rid, _data, created, expired} ->
      created < snapshot_xid and (expired == 0 or expired > snapshot_xid)
    end)
    |> Enum.group_by(fn {rid, _, _, _} -> rid end)
    |> Enum.map(fn {_rid, versions} ->
      versions
      |> Enum.sort_by(fn {_, _, created, _} -> created end, :desc)
      |> List.first()
      |> elem(1)
    end)
  end

  @doc "Deletes row versions invisible to all active transactions (GC)."
  @spec gc(atom(), pos_integer()) :: non_neg_integer()
  def gc(table, horizon) do
    match_spec = [
      {{:_, :_, :_, :"$1"}, [{:andalso, {:>, :"$1", 0}, {:<, :"$1", horizon}}], [true]}
    ]
    :ets.select_delete(table, match_spec)
  end
end
```

### Step 4: Query planner

```elixir
# lib/memdb/query_planner.ex
defmodule Memdb.QueryPlanner do
  @moduledoc """
  Chooses between index scan and full table scan based on selectivity.

  Cost model (simple):
    full_table_scan_cost = table_cardinality
    index_scan_cost      = log2(table_cardinality) + estimated_result_rows

  Use index scan if:
    index_scan_cost < full_table_scan_cost
    AND the WHERE clause contains an equality or range predicate on an indexed column.
  """

  @doc "Returns {:index_scan, index_name} or {:full_scan}."
  @spec plan(map(), keyword()) :: {:index_scan, atom()} | {:full_scan}
  def plan(table_meta, where_clauses) do
    cardinality = Map.get(table_meta, :cardinality, 0)
    indexes = Map.get(table_meta, :indexes, %{})

    matching_index =
      Enum.find(indexes, fn {_name, indexed_column} ->
        Keyword.has_key?(where_clauses, indexed_column)
      end)

    case matching_index do
      nil ->
        {:full_scan}

      {index_name, _column} ->
        if cardinality > 0 do
          estimated_results = max(1, div(cardinality, 10))
          index_cost = :math.log2(max(cardinality, 1)) + estimated_results
          full_scan_cost = cardinality

          if index_cost < full_scan_cost do
            {:index_scan, index_name}
          else
            {:full_scan}
          end
        else
          {:index_scan, index_name}
        end
    end
  end
end
```

### Step 5: Lock manager and wait-for graph

```elixir
# lib/memdb/lock_manager.ex
defmodule Memdb.LockManager do
  use GenServer

  @moduledoc """
  Row-level lock manager with deadlock detection via wait-for graph.

  Maintains a map of {table, row_id} => holder_txn_id and a wait-for
  graph as an adjacency list. When a cycle is detected, the youngest
  transaction in the cycle is aborted.
  """

  defstruct locks: %{}, waiters: %{}, wait_graph: %{}

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: Keyword.get(opts, :name, __MODULE__))
  end

  @doc "Acquires a lock on {table, row_id} for the given transaction."
  @spec acquire(GenServer.server(), atom(), term(), pos_integer(), timeout()) ::
          :ok | {:error, :deadlock}
  def acquire(server \\ __MODULE__, table, row_id, txn_id, timeout \\ 5_000) do
    GenServer.call(server, {:acquire, table, row_id, txn_id}, timeout)
  end

  @doc "Releases all locks held by the given transaction."
  @spec release_all(GenServer.server(), pos_integer()) :: :ok
  def release_all(server \\ __MODULE__, txn_id) do
    GenServer.call(server, {:release_all, txn_id})
  end

  @impl true
  def init(_opts), do: {:ok, %__MODULE__{}}

  @impl true
  def handle_call({:acquire, table, row_id, txn_id}, from, state) do
    lock_key = {table, row_id}

    case Map.get(state.locks, lock_key) do
      nil ->
        new_locks = Map.put(state.locks, lock_key, txn_id)
        {:reply, :ok, %{state | locks: new_locks}}

      ^txn_id ->
        {:reply, :ok, state}

      holder_id ->
        new_graph = Map.update(state.wait_graph, txn_id, [holder_id], &[holder_id | &1])

        if has_cycle?(new_graph, txn_id) do
          {:reply, {:error, :deadlock}, state}
        else
          new_waiters = Map.put(state.waiters, {lock_key, txn_id}, from)
          {:noreply, %{state | wait_graph: new_graph, waiters: new_waiters}}
        end
    end
  end

  @impl true
  def handle_call({:release_all, txn_id}, _from, state) do
    released_keys =
      state.locks
      |> Enum.filter(fn {_key, holder} -> holder == txn_id end)
      |> Enum.map(fn {key, _} -> key end)

    new_locks = Map.drop(state.locks, released_keys)
    new_graph = Map.delete(state.wait_graph, txn_id)

    {new_locks, new_waiters, replies} =
      Enum.reduce(released_keys, {new_locks, state.waiters, []}, fn lock_key, {locks, waiters, reps} ->
        waiting =
          waiters
          |> Enum.filter(fn {{lk, _tid}, _from} -> lk == lock_key end)
          |> Enum.sort_by(fn {{_, tid}, _} -> tid end)

        case waiting do
          [] ->
            {locks, waiters, reps}

          [{{_lk, waiter_txn}, waiter_from} | _] ->
            new_locks = Map.put(locks, lock_key, waiter_txn)
            new_waiters = Map.delete(waiters, {lock_key, waiter_txn})
            {new_locks, new_waiters, [{waiter_from, :ok} | reps]}
        end
      end)

    Enum.each(replies, fn {from, reply} -> GenServer.reply(from, reply) end)

    {:reply, :ok, %{state | locks: new_locks, waiters: new_waiters, wait_graph: new_graph}}
  end

  defp has_cycle?(graph, start) do
    do_cycle_check(graph, start, MapSet.new(), MapSet.new([start]))
  end

  defp do_cycle_check(graph, current, visited, path) do
    neighbors = Map.get(graph, current, [])

    Enum.any?(neighbors, fn neighbor ->
      if MapSet.member?(path, neighbor) do
        true
      else
        if MapSet.member?(visited, neighbor) do
          false
        else
          do_cycle_check(graph, neighbor, MapSet.put(visited, current), MapSet.put(path, neighbor))
        end
      end
    end)
  end
end
```

### Step 6: Database — public API

```elixir
# lib/memdb/database.ex
defmodule Memdb.Database do
  use GenServer

  @moduledoc """
  Public API for the in-memory database with MVCC.

  Provides create_table, insert, select, update, delete, and
  transaction management (begin, commit, rollback).
  """

  defstruct [:tables, :xid_counter, :active_txns, :lock_manager]

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts)
  end

  def create_table(db, name, opts) do
    GenServer.call(db, {:create_table, name, opts})
  end

  def create_index(db, table, column) do
    GenServer.call(db, {:create_index, table, column})
  end

  def insert(db, table, data) when is_pid(db) do
    GenServer.call(db, {:insert, table, data})
  end

  def insert(%{db: db} = txn, table, opts) do
    GenServer.call(db, {:txn_insert, txn, table, opts})
  end

  def select(%{db: db} = txn, table, opts) do
    GenServer.call(db, {:txn_select, txn, table, opts})
  end

  def update(%{db: db} = txn, table, opts) do
    GenServer.call(db, {:txn_update, txn, table, opts})
  end

  def delete(%{db: db} = txn, table, opts) do
    GenServer.call(db, {:txn_delete, txn, table, opts})
  end

  def begin(db) do
    GenServer.call(db, :begin_txn)
  end

  def commit(%{db: db} = txn) do
    GenServer.call(db, {:commit, txn})
  end

  def rollback(%{db: db} = txn) do
    GenServer.call(db, {:rollback, txn})
  end

  @impl true
  def init(_opts) do
    {:ok, lock_mgr} = Memdb.LockManager.start_link([])
    {:ok, %__MODULE__{
      tables: %{},
      xid_counter: 1,
      active_txns: %{},
      lock_manager: lock_mgr
    }}
  end

  @impl true
  def handle_call({:create_table, name, opts}, _from, state) do
    columns = Keyword.get(opts, :columns, [])
    ets_table = :ets.new(name, [:bag, :public])
    table_meta = %{ets: ets_table, columns: columns, indexes: %{}, cardinality: 0}
    {:reply, :ok, %{state | tables: Map.put(state.tables, name, table_meta)}}
  end

  @impl true
  def handle_call({:create_index, table_name, column}, _from, state) do
    table_meta = Map.get(state.tables, table_name)
    index_name = :"#{table_name}_#{column}_idx"
    new_meta = %{table_meta | indexes: Map.put(table_meta.indexes, index_name, column)}
    {:reply, :ok, %{state | tables: Map.put(state.tables, table_name, new_meta)}}
  end

  @impl true
  def handle_call({:insert, table_name, data}, _from, state) do
    table_meta = Map.get(state.tables, table_name)
    xid = state.xid_counter
    row_id = :erlang.unique_integer([:positive])
    Memdb.MVCC.insert(table_meta.ets, row_id, data, xid)
    new_counter = xid + 1
    new_meta = %{table_meta | cardinality: table_meta.cardinality + 1}
    {:reply, :ok, %{state | xid_counter: new_counter, tables: Map.put(state.tables, table_name, new_meta)}}
  end

  @impl true
  def handle_call(:begin_txn, _from, state) do
    xid = state.xid_counter
    txn = %{xid: xid, snapshot_xid: xid, db: self(), write_set: []}
    new_state = %{state |
      xid_counter: xid + 1,
      active_txns: Map.put(state.active_txns, xid, txn)
    }
    {:reply, txn, new_state}
  end

  @impl true
  def handle_call({:txn_select, txn, table_name, opts}, _from, state) do
    table_meta = Map.get(state.tables, table_name)
    where = Keyword.get(opts, :where, [])
    lock = Keyword.get(opts, :lock)

    rows = Memdb.MVCC.scan(table_meta.ets, txn.snapshot_xid)
    filtered = filter_rows(rows, where)

    if lock == :for_update do
      Enum.each(filtered, fn row ->
        row_key = Map.get(row, :id, :erlang.phash2(row))
        Memdb.LockManager.acquire(state.lock_manager, table_name, row_key, txn.xid)
      end)
    end

    {:reply, filtered, state}
  end

  @impl true
  def handle_call({:txn_update, txn, table_name, opts}, _from, state) do
    table_meta = Map.get(state.tables, table_name)
    where = Keyword.get(opts, :where, [])
    set = Keyword.get(opts, :set, [])

    all_rows = :ets.tab2list(table_meta.ets)

    visible_rows =
      all_rows
      |> Enum.filter(fn {_rid, _data, created, expired} ->
        created < txn.snapshot_xid and (expired == 0 or expired > txn.snapshot_xid)
      end)
      |> Enum.group_by(fn {rid, _, _, _} -> rid end)
      |> Enum.map(fn {rid, versions} ->
        latest = versions |> Enum.sort_by(fn {_, _, c, _} -> c end, :desc) |> List.first()
        {rid, elem(latest, 1)}
      end)

    matching = Enum.filter(visible_rows, fn {_rid, data} -> matches_where?(data, where) end)

    conflict =
      Enum.any?(matching, fn {rid, _data} ->
        versions = :ets.lookup(table_meta.ets, rid)
        Enum.any?(versions, fn {_, _, created, _} ->
          created >= txn.snapshot_xid
        end)
      end)

    if conflict do
      {:reply, {:error, :write_conflict}, state}
    else
      Enum.each(matching, fn {rid, data} ->
        Memdb.MVCC.expire(table_meta.ets, rid, txn.xid)
        new_data = Enum.reduce(set, data, fn {k, v}, acc -> Map.put(acc, k, v) end)
        Memdb.MVCC.insert(table_meta.ets, rid, new_data, txn.xid)
      end)

      {:reply, :ok, state}
    end
  end

  @impl true
  def handle_call({:txn_insert, txn, table_name, data}, _from, state) do
    table_meta = Map.get(state.tables, table_name)
    row_id = :erlang.unique_integer([:positive])
    Memdb.MVCC.insert(table_meta.ets, row_id, data, txn.xid)
    {:reply, :ok, state}
  end

  @impl true
  def handle_call({:txn_delete, txn, table_name, opts}, _from, state) do
    table_meta = Map.get(state.tables, table_name)
    where = Keyword.get(opts, :where, [])

    all_rows = :ets.tab2list(table_meta.ets)
    visible_rows =
      all_rows
      |> Enum.filter(fn {_rid, _data, created, expired} ->
        created < txn.snapshot_xid and (expired == 0 or expired > txn.snapshot_xid)
      end)
      |> Enum.group_by(fn {rid, _, _, _} -> rid end)
      |> Enum.map(fn {rid, versions} ->
        latest = versions |> Enum.sort_by(fn {_, _, c, _} -> c end, :desc) |> List.first()
        {rid, elem(latest, 1)}
      end)

    matching = Enum.filter(visible_rows, fn {_rid, data} -> matches_where?(data, where) end)

    Enum.each(matching, fn {rid, _data} ->
      Memdb.MVCC.expire(table_meta.ets, rid, txn.xid)
    end)

    {:reply, :ok, state}
  end

  @impl true
  def handle_call({:commit, txn}, _from, state) do
    Memdb.LockManager.release_all(state.lock_manager, txn.xid)
    new_active = Map.delete(state.active_txns, txn.xid)
    {:reply, :ok, %{state | active_txns: new_active}}
  end

  @impl true
  def handle_call({:rollback, txn}, _from, state) do
    Memdb.LockManager.release_all(state.lock_manager, txn.xid)
    new_active = Map.delete(state.active_txns, txn.xid)
    {:reply, :ok, %{state | active_txns: new_active}}
  end

  defp filter_rows(rows, where) do
    Enum.filter(rows, fn row -> matches_where?(row, where) end)
  end

  defp matches_where?(row, where) do
    Enum.all?(where, fn {key, value} -> Map.get(row, key) == value end)
  end
end
```

### Step 7: Given tests — must pass without modification

```elixir
# test/memdb/mvcc_test.exs
defmodule Memdb.MVCCTest do
  use ExUnit.Case, async: false

  setup do
    {:ok, db} = Memdb.Database.start_link()
    :ok = Memdb.Database.create_table(db, :accounts,
      columns: [id: :integer, balance: :integer])
    {:ok, db: db}
  end

  test "snapshot isolation: T2 does not see T1's uncommitted writes", %{db: db} do
    :ok = Memdb.Database.insert(db, :accounts, %{id: 1, balance: 1_000})

    t1 = Memdb.Database.begin(db)
    :ok = Memdb.Database.update(t1, :accounts, where: [id: 1], set: [balance: 500])

    # T2 starts AFTER T1 has written, but BEFORE T1 commits
    t2 = Memdb.Database.begin(db)

    # T2 must see balance = 1_000, not 500
    rows = Memdb.Database.select(t2, :accounts, where: [id: 1])
    assert [%{balance: 1_000}] = rows

    :ok = Memdb.Database.commit(t1)

    # T2 still sees 1_000 (its snapshot is from before T1 committed)
    rows = Memdb.Database.select(t2, :accounts, where: [id: 1])
    assert [%{balance: 1_000}] = rows

    # New transaction sees 500
    t3 = Memdb.Database.begin(db)
    rows = Memdb.Database.select(t3, :accounts, where: [id: 1])
    assert [%{balance: 500}] = rows
  end

  test "no lost updates: concurrent increments both apply" do
    :ok = Memdb.Database.insert(db, :accounts, %{id: 2, balance: 100})

    t1 = Memdb.Database.begin(db)
    t2 = Memdb.Database.begin(db)

    [%{balance: b1}] = Memdb.Database.select(t1, :accounts, where: [id: 2])
    [%{balance: b2}] = Memdb.Database.select(t2, :accounts, where: [id: 2])

    :ok = Memdb.Database.update(t1, :accounts, where: [id: 2], set: [balance: b1 + 50])
    :ok = Memdb.Database.commit(t1)

    # T2 must detect write-write conflict and retry or abort
    result = Memdb.Database.update(t2, :accounts, where: [id: 2], set: [balance: b2 + 30])
    case result do
      {:error, :write_conflict} ->
        # Retry
        t2b = Memdb.Database.begin(db)
        [%{balance: current}] = Memdb.Database.select(t2b, :accounts, where: [id: 2])
        :ok = Memdb.Database.update(t2b, :accounts, where: [id: 2], set: [balance: current + 30])
        :ok = Memdb.Database.commit(t2b)
      :ok ->
        :ok = Memdb.Database.commit(t2)
    end

    t_read = Memdb.Database.begin(db)
    [%{balance: final}] = Memdb.Database.select(t_read, :accounts, where: [id: 2])
    assert final == 180, "expected 100+50+30=180, got #{final}"
  end
end
```

```elixir
# test/memdb/deadlock_test.exs
defmodule Memdb.DeadlockTest do
  use ExUnit.Case, async: false

  test "deadlock is detected within 1 second and one transaction aborts" do
    {:ok, db} = Memdb.Database.start_link()
    :ok = Memdb.Database.create_table(db, :items, columns: [id: :integer, val: :text])
    :ok = Memdb.Database.insert(db, :items, %{id: 1, val: "a"})
    :ok = Memdb.Database.insert(db, :items, %{id: 2, val: "b"})

    t1 = Memdb.Database.begin(db)
    t2 = Memdb.Database.begin(db)

    # T1 locks row 1
    Memdb.Database.select(t1, :items, where: [id: 1], lock: :for_update)

    # T2 locks row 2
    Memdb.Database.select(t2, :items, where: [id: 2], lock: :for_update)

    # T1 tries to lock row 2 (held by T2) — blocks
    task1 = Task.async(fn ->
      Memdb.Database.select(t1, :items, where: [id: 2], lock: :for_update)
    end)

    # T2 tries to lock row 1 (held by T1) — creates cycle
    task2 = Task.async(fn ->
      Memdb.Database.select(t2, :items, where: [id: 1], lock: :for_update)
    end)

    results = Task.await_many([task1, task2], 3_000)

    # Exactly one must have received {:error, :deadlock}
    aborts = Enum.count(results, fn r -> r == {:error, :deadlock} end)
    assert aborts == 1, "expected exactly 1 deadlock abort, got: #{inspect(results)}"
  end
end
```

### Step 8: Run the tests

```bash
mix test test/memdb/ --trace
```

### Step 9: Benchmark

```elixir
# bench/memdb_bench.exs
{:ok, db} = Memdb.Database.start_link()
:ok = Memdb.Database.create_table(db, :bench, columns: [id: :integer, val: :text])
:ok = Memdb.Database.create_index(db, :bench, :id)

for i <- 1..1_000_000, do: Memdb.Database.insert(db, :bench, %{id: i, val: "v#{i}"})

Benchee.run(
  %{
    "select by indexed column" => fn ->
      k = :rand.uniform(1_000_000)
      txn = Memdb.Database.begin(db)
      Memdb.Database.select(txn, :bench, where: [id: k])
    end,
    "insert single row" => fn ->
      txn = Memdb.Database.begin(db)
      :ok = Memdb.Database.insert(txn, :bench, %{id: :rand.uniform(10_000_000), val: "x"})
      Memdb.Database.commit(txn)
    end
  },
  parallel: 8,
  time: 10,
  warmup: 3,
  formatters: [Benchee.Formatters.Console]
)
```

Targets: 1M reads/second, 100k writes/second on a 1M-row table.

---

## Trade-off analysis

| Aspect | MVCC (your impl) | Two-phase locking (2PL) | Serializable Snapshot Isolation |
|--------|-----------------|------------------------|-------------------------------|
| Read-write conflict | readers never block writers | readers block writers | readers never block writers |
| Write-write conflict | abort or wait | wait for lock | abort on rw-dependency cycle |
| Isolation level | snapshot isolation (default) | serializable | serializable |
| Phantom reads | possible | prevented by range locks | prevented by SSI |
| GC overhead | required | none | required |

Fill in measured latency from the benchmark.

---

## Common production mistakes

**1. XID comparison with `<=` instead of `<`**
A row created by XID N is visible to a snapshot taken at XID N only if the transaction has committed. If it has not, using `<=` would expose uncommitted data. Use strict `<` and track which XIDs have committed.

**2. GC deleting versions still visible to long-running transactions**
A transaction that has been running for a long time may have a very old snapshot XID. The GC must compute `horizon = min(active_snapshot_xids)` before every GC pass, not once at startup. An old transaction prevents GC from reclaiming versions it needs.

**3. Deadlock detection on the lock acquisition hot path**
Cycle detection with DFS is O(V + E). Running it synchronously on every lock acquire adds latency. Run the detector on a timer (every 100ms or configurable). Accept that deadlocks are detected within one timer interval, not instantly.

**4. B-tree mutations without copy-on-write**
Updating the B-tree in place under concurrent reads requires locking. A persistent (immutable) B-tree where each mutation produces a new root reference eliminates read/write conflicts at the cost of GC pressure on old tree versions.

---

## Resources

- [PostgreSQL MVCC documentation](https://www.postgresql.org/docs/current/mvcc-intro.html)
- Ports, D. & Gritter, K. (2012). *Serializable Snapshot Isolation in PostgreSQL* — VLDB
- [MySQL InnoDB MVCC internals](https://dev.mysql.com/doc/refman/8.0/en/innodb-multi-versioning.html)
- Petrov, A. — *Database Internals* — Part I (Storage Engines), Part II (Distributed Systems)
