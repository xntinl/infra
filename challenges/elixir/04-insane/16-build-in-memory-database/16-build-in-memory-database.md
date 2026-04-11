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

**Wait-for graph for deadlock detection**: when transaction T1 waits for a lock held by T2, an edge T1→T2 is added to the wait-for graph. Deadlock = cycle in the graph. The cycle detector runs DFS from each node and aborts the youngest transaction in the cycle.

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
  def insert(table, row_id, data, txn_xid) do
    # TODO: :ets.insert(table, {row_id, data, txn_xid, 0})
  end

  @doc "Marks existing versions of row_id as expired by txn_xid."
  def expire(table, row_id, txn_xid) do
    # TODO: :ets.select_replace/2 to set expired_xid for versions where expired_xid == 0
  end

  @doc "Returns all versions of row_id visible to snapshot_xid."
  def visible_versions(table, row_id, snapshot_xid) do
    # TODO
    # HINT: created_xid < snapshot_xid AND (expired_xid == 0 OR expired_xid > snapshot_xid)
  end

  @doc "Scans all rows visible to snapshot_xid."
  def scan(table, snapshot_xid) do
    # TODO: :ets.tab2list, filter by visibility, deduplicate by row_id (latest version)
  end

  @doc "Deletes row versions invisible to all active transactions (GC)."
  def gc(table, horizon) do
    # TODO: :ets.select_delete for expired_xid > 0 AND expired_xid < horizon
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
  def plan(table_meta, where_clauses) do
    # TODO
  end
end
```

### Step 5: Given tests — must pass without modification

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

### Step 6: Run the tests

```bash
mix test test/memdb/ --trace
```

### Step 7: Benchmark

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
