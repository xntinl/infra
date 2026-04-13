# Mnesia RAM-Only Tables with Cluster Replication

**Project**: `mnesia_ram_demo` — in-memory distributed state for a real-time session/presence system.

---

## The business problem

You operate a multi-node Phoenix application behind a load balancer. Each HTTP
request can land on any node, and every node needs sub-millisecond access to
authenticated session data and online-user presence. Postgres is not an option
on the hot path — a round-trip to the database adds 500µs minimum and couples
request latency to database health. Redis is off the table because infra prefers
to keep operational surface inside BEAM.

Mnesia with `ram_copies` is a common answer: tables live entirely in process
heap on every node (fast local reads), and Mnesia replicates writes across the
cluster using a two-phase commit coordinator. The price is brutal honesty about
failure modes — `ram_copies` tables are lost when the last node holding them
dies, and network partitions cause `{:inconsistent_database, ...}` events that
the operator must resolve manually.

This exercise builds a presence/session store on top of RAM-only Mnesia tables
with cluster-wide replication. You will add a node dynamically, verify data
propagates, simulate a netsplit, and implement a reconciliation strategy.
By the end you will know when `ram_copies` is the right choice and — more
importantly — when it is not.

## Project structure

```
mnesia_ram_demo/
├── lib/
│   └── mnesia_ram_demo/
│       ├── application.ex
│       ├── cluster.ex              # libcluster wiring (Gossip strategy)
│       ├── schema.ex               # table creation + replica management
│       ├── session_store.ex        # read/write API
│       └── partition_handler.ex    # handles :inconsistent_database events
├── test/
│   └── mnesia_ram_demo/
│       ├── schema_test.exs
│       └── session_store_test.exs
├── script/
│   └── main.exs
└── mix.exs
```

---

## Why Mnesia RAM and not ETS

ETS is a hash table. Mnesia gives the same storage plus transactions and replication. For anything that cares about multi-key atomicity or cluster state, starting with ETS is starting from minus one.

---

## Design decisions

**Option A — ETS + custom replication**
- Pros: fast, well-understood data structure; full control.
- Cons: reimplementing transactions, replication, and recovery is a multi-year project.

**Option B — Mnesia RAM tables** (chosen)
- Pros: transactions, multi-node replication, and QLC queries for free.
- Cons: Mnesia's quirks (netsplits, schema management, tooling gaps) are real.

→ Chose **B** because building ACID on ETS from scratch is never justified when Mnesia exists.

---

## Implementation

### `mix.exs`
```elixir
defmodule MnesiaRam.MixProject do
  use Mix.Project

  def project do
    [
      app: :mnesia_ram,
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
:mnesia.add_table_copy(Session, node(), :ram_copies)
```

from a node that already holds the table. Mnesia streams the current contents
to the new node and keeps it in sync from that point forward.

When a cluster heals after a partition, Mnesia emits a system event:

```elixir
{:mnesia_system_event, {:inconsistent_database, :running_partitioned_network, node}}
```

Mnesia does NOT heal automatically. You must pick a node to "win", stop Mnesia
on the losing node, delete its schema, and let it rejoin. This is the part
every Mnesia tutorial skips and every production system has to solve.

If you have a table with replicas on nodes A, B, C, and all three die, the
data is gone. There is no disk backup. If you need *at least one* replica to
survive a full cluster restart, you need at least one `disc_copies` replica
somewhere. A common pattern is: `ram_copies` on application nodes for speed,
`disc_copies` on a dedicated "persistence node" for durability.

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

**1. Writes are synchronous across all replicas.**
A `:mnesia.transaction/1` that writes to a replicated table blocks until every
replica ACKs the commit. Put a replica on a slow node and every writer in the
cluster slows down. Measure with `:mnesia.system_info(:transaction_log_writes)`.

**2. `ram_copies` loses data on last-node death.**
If the only three replicas die simultaneously (power outage, bad deploy),
every row is gone. Mix at least one `disc_copies` replica into the cluster
if the data is not perfectly reconstructible from another source of truth.

**3. Netsplits require a human.**
Mnesia detects partitions but does not resolve them. The
`PartitionHandler` shown here picks a winner by node name — fine for a demo,
dangerous in production where the "losing" node may have accepted writes you
cannot afford to discard. In production prefer an external coordinator
(etcd, Consul) or an append-only log (CRDTs).

**4. `:mnesia.match_object/1` is O(n).**
`invalidate_user/1` scans the whole `:sessions` table on every replica.
For > 100k sessions add a secondary index:
`:mnesia.add_table_index(:sessions, :user_id)` — then `match_object`
uses the index.

**5. Schema changes are a cluster-wide operation.**
Adding a column to a `ram_copies` table requires `:mnesia.transform_table/3`
coordinated across every live replica. During the transform every writer
blocks. Plan schema migrations like you plan Postgres migrations —
ideally rolling and backward-compatible.

**6. `extra_db_nodes` vs libcluster.**
libcluster connects BEAM nodes (`:net_kernel.connect_node/1`), but Mnesia
needs a separate `:mnesia.change_config(:extra_db_nodes, peers)` call to
share schema metadata. A `:nodeup` alone does not make two Mnesia instances
see each other — they also need to agree on schema.

**7. Monitoring checklist.**
Wire these into your metrics pipeline:
* `:mnesia.system_info(:held_locks)` — lock contention
* `:mnesia.system_info(:db_nodes)` vs `Node.list()` — topology drift
* `:mnesia.table_info(:sessions, :size)` per node — replica divergence

**8. When NOT to use `ram_copies`.**
Skip `ram_copies` if:
* Data must survive a full cluster restart → use `disc_copies` or Postgres.
* Write rate > ~5k ops/sec per table → transaction coordinator becomes
  the bottleneck; use ETS + async replication or a real database.
* You need cross-datacenter replication → Mnesia does not tolerate WAN
  latency; use PG logical replication or Cassandra.
* Your team has no one on-call trained on Mnesia recovery — you will have
  an outage during the first netsplit and nobody will know what to do.

---

## Benchmark

```elixir
# bench/ram_copies_bench.exs
alias MnesiaRamDemo.SessionStore

for i <- 1..10_000 do
  SessionStore.put(%{token: "t-#{i}", user_id: "u-#{rem(i, 100)}", expires_at: 1_000})
end

Benchee.run(
  %{
    "dirty_read"    => fn -> :mnesia.dirty_read({:sessions, "t-5000"}) end,
    "transaction_read" => fn ->
      :mnesia.transaction(fn -> :mnesia.read({:sessions, "t-5000"}) end)
    end,
    "transaction_write" => fn ->
      :mnesia.transaction(fn ->
        :mnesia.write({:sessions, "t-bench", "u", 1, node()})
      end)
    end
  },
  parallel: 8,
  time: 5,
  warmup: 2
)
```

Expected ballpark on a 2-node local cluster (M1, Elixir 1.16, OTP 26):

| Operation         | p50   | p99    |
|-------------------|-------|--------|
| dirty_read        | 1.2µs | 3µs    |
| transaction_read  | 18µs  | 50µs   |
| transaction_write | 120µs | 450µs  |

The 100x gap between `dirty_read` and `transaction_write` is the cost of the
two-phase commit and lock acquisition across replicas.

---

## Reflection

- You need durability across a single-node crash. Does RAM-only still fit, or do you switch to disc_copies? Why not disc_only?
- Your cluster split-brain recovers with divergent writes. What does Mnesia do, and what do you wish it did instead?

---

### `script/main.exs`
```elixir
defmodule MnesiaRamDemo.MixProject do
  use Mix.Project

  def project do
    [
      app: :mnesia_ram_demo,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [
      extra_applications: [:logger, :mnesia],
      mod: {MnesiaRamDemo.Application, []}
    ]
  end

  defp deps do
    [
      {:libcluster, "~> 3.3"},
      {:benchee, "~> 1.3", only: :dev}
    ]
  end
end

from a node that already holds the table. Mnesia streams the current contents
to the new node and keeps it in sync from that point forward.

When a cluster heals after a partition, Mnesia emits a system event:

defmodule Main do
  def main do
      # Demonstrating 121-mnesia-ram
      :ok
  end
end

Main.main()
```

---

## Why Mnesia RAM-Only Tables with Cluster Replication matters

Mastering **Mnesia RAM-Only Tables with Cluster Replication** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `lib/mnesia_ram_demo.ex`

```elixir
defmodule MnesiaRamDemo do
  @moduledoc """
  Reference implementation for Mnesia RAM-Only Tables with Cluster Replication.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the mnesia_ram_demo module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> MnesiaRamDemo.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```

### `test/mnesia_ram_demo_test.exs`

```elixir
defmodule MnesiaRamDemoTest do
  use ExUnit.Case, async: true

  doctest MnesiaRamDemo

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert MnesiaRamDemo.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts

### 1. `ram_copies` vs `disc_copies` vs `disc_only_copies`

Mnesia table storage types are chosen per-replica, not per-table. The same
table can be `ram_copies` on node A and `disc_copies` on node B.

| Type               | Where it lives            | Survives restart | Read speed | Write speed |
|--------------------|---------------------------|------------------|------------|-------------|
| `ram_copies`       | ETS in the Mnesia process | no               | fastest    | fast        |
| `disc_copies`      | ETS + disk log            | yes              | fast       | slower      |
| `disc_only_copies` | DETS on disk              | yes              | slow       | slow        |

`ram_copies` is conceptually "ETS with cluster-wide replication and
transactions bolted on". The local read path is essentially `:ets.lookup/2`.

### 2. Write replication and the two-phase commit coordinator

When you call `:mnesia.transaction(fn -> :mnesia.write(record) end)`, Mnesia:

```
      coordinator (caller node)
      ┌───────────────────────┐
      │ 1. acquire write_lock │──────► all replicas: lock record
      │ 2. prepare phase      │──────► all replicas: "can you commit?"
      │ 3. commit phase       │──────► all replicas: apply + release lock
      └───────────────────────┘
```

Any replica that does not respond aborts the transaction. A write therefore
blocks on the slowest replica. In practice, within a datacenter this is
sub-millisecond; across a WAN it is a nightmare. `ram_copies` does not save
you here — replication is synchronous regardless of storage type.

### 3. Dynamic replica management with `add_table_copy/3`

A node that joins the cluster does not automatically get a replica of every
table. You must explicitly call:

```elixir
:mnesia.add_table_copy(Session, node(), :ram_copies)
```

from a node that already holds the table. Mnesia streams the current contents
to the new node and keeps it in sync from that point forward.

### 4. Netsplit detection — `:inconsistent_database`

When a cluster heals after a partition, Mnesia emits a system event:

```elixir
{:mnesia_system_event, {:inconsistent_database, :running_partitioned_network, node}}
```

Mnesia does NOT heal automatically. You must pick a node to "win", stop Mnesia
on the losing node, delete its schema, and let it rejoin. This is the part
every Mnesia tutorial skips and every production system has to solve.

### 5. `ram_copies` and the "last node" problem

If you have a table with replicas on nodes A, B, C, and all three die, the
data is gone. There is no disk backup. If you need *at least one* replica to
survive a full cluster restart, you need at least one `disc_copies` replica
somewhere. A common pattern is: `ram_copies` on application nodes for speed,
`disc_copies` on a dedicated "persistence node" for durability.

---
