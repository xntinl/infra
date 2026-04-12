# Mnesia RAM-Only Tables with Cluster Replication

**Project**: `mnesia_ram_demo` — in-memory distributed state for a real-time session/presence system.
**Difficulty**: ★★★★☆
**Estimated time**: 3–6 hours

---

## Project context

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
└── mix.exs
```

---

## Core concepts

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

## Implementation

### Step 1: `mix.exs`

```elixir
defmodule MnesiaRamDemo.MixProject do
  use Mix.Project

  def project do
    [
      app: :mnesia_ram_demo,
      version: "0.1.0",
      elixir: "~> 1.16",
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
```

### Step 2: `lib/mnesia_ram_demo/application.ex`

```elixir
defmodule MnesiaRamDemo.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    topologies = [
      gossip: [
        strategy: Cluster.Strategy.Gossip,
        config: [
          port: 45_892,
          if_addr: "0.0.0.0",
          multicast_addr: "230.1.1.251"
        ]
      ]
    ]

    children = [
      {Cluster.Supervisor, [topologies, [name: MnesiaRamDemo.ClusterSupervisor]]},
      MnesiaRamDemo.Schema,
      MnesiaRamDemo.PartitionHandler
    ]

    opts = [strategy: :one_for_one, name: MnesiaRamDemo.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
```

### Step 3: `lib/mnesia_ram_demo/schema.ex`

```elixir
defmodule MnesiaRamDemo.Schema do
  @moduledoc """
  Idempotent Mnesia schema bootstrapping.

  Rules:
    * Never call `create_schema/1` on an already-running Mnesia — it will fail.
    * `start/0` must tolerate joining an existing cluster AND bootstrapping alone.
    * Waiting for tables is mandatory: Mnesia loads replicas asynchronously,
      and queries against an unloaded table raise `{:aborted, {:no_exists, _}}`.
  """
  use GenServer
  require Logger

  @tables [:sessions, :presence]
  @wait_timeout 15_000

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(_opts) do
    :ok = ensure_mnesia_stopped()
    :ok = ensure_schema()
    :ok = :mnesia.start()
    :ok = ensure_tables()
    :ok = :mnesia.wait_for_tables(@tables, @wait_timeout)
    :net_kernel.monitor_nodes(true)
    Logger.info("Mnesia ready on #{inspect(node())} with tables #{inspect(@tables)}")
    {:ok, %{}}
  end

  @impl true
  def handle_info({:nodeup, node}, state) do
    Logger.info("Node up: #{inspect(node)} — replicating tables")
    replicate_to(node)
    {:noreply, state}
  end

  def handle_info({:nodedown, node}, state) do
    Logger.warning("Node down: #{inspect(node)}")
    {:noreply, state}
  end

  # ---------------------------------------------------------------------------

  defp ensure_mnesia_stopped do
    _ = :mnesia.stop()
    :ok
  end

  defp ensure_schema do
    case :mnesia.create_schema([node()]) do
      :ok -> :ok
      {:error, {_, {:already_exists, _}}} -> :ok
      other -> throw({:schema_creation_failed, other})
    end
  end

  defp ensure_tables do
    Enum.each(@tables, &create_table/1)
    :ok
  end

  defp create_table(:sessions) do
    result =
      :mnesia.create_table(:sessions,
        attributes: [:token, :user_id, :expires_at, :node],
        ram_copies: [node()],
        type: :set
      )

    normalize(result, :sessions)
  end

  defp create_table(:presence) do
    result =
      :mnesia.create_table(:presence,
        attributes: [:user_id, :metadata, :last_seen],
        ram_copies: [node()],
        type: :set
      )

    normalize(result, :presence)
  end

  defp normalize({:atomic, :ok}, _), do: :ok
  defp normalize({:aborted, {:already_exists, _}}, _), do: :ok
  defp normalize(other, table), do: throw({:create_table_failed, table, other})

  defp replicate_to(node) do
    # The remote node must already have Mnesia running. We ask it to add
    # itself as a ram_copies replica for each of our tables.
    Enum.each(@tables, fn table ->
      case :mnesia.add_table_copy(table, node, :ram_copies) do
        {:atomic, :ok} -> Logger.info("Replicated #{table} to #{inspect(node)}")
        {:aborted, {:already_exists, _, _}} -> :ok
        other -> Logger.error("Replication failed for #{table}: #{inspect(other)}")
      end
    end)
  end
end
```

### Step 4: `lib/mnesia_ram_demo/session_store.ex`

```elixir
defmodule MnesiaRamDemo.SessionStore do
  @moduledoc """
  Typed read/write API on top of the Mnesia :sessions table.

  Reads use dirty operations for speed. Writes use transactions so that
  replication across the cluster is atomic.
  """

  @type token :: String.t()
  @type session :: %{token: token, user_id: String.t(), expires_at: integer, node: node()}

  @spec put(session()) :: :ok | {:error, term()}
  def put(%{token: token, user_id: user_id, expires_at: expires_at}) do
    record = {:sessions, token, user_id, expires_at, node()}

    case :mnesia.transaction(fn -> :mnesia.write(record) end) do
      {:atomic, :ok} -> :ok
      {:aborted, reason} -> {:error, reason}
    end
  end

  @spec get(token()) :: {:ok, session()} | :not_found
  def get(token) do
    case :mnesia.dirty_read({:sessions, token}) do
      [{:sessions, ^token, user_id, expires_at, origin_node}] ->
        {:ok, %{token: token, user_id: user_id, expires_at: expires_at, node: origin_node}}

      [] ->
        :not_found
    end
  end

  @spec delete(token()) :: :ok | {:error, term()}
  def delete(token) do
    case :mnesia.transaction(fn -> :mnesia.delete({:sessions, token}) end) do
      {:atomic, :ok} -> :ok
      {:aborted, reason} -> {:error, reason}
    end
  end

  @spec invalidate_user(String.t()) :: {:ok, non_neg_integer()} | {:error, term()}
  def invalidate_user(user_id) do
    fun = fn ->
      matches = :mnesia.match_object({:sessions, :_, user_id, :_, :_})
      Enum.each(matches, fn record -> :mnesia.delete_object(record) end)
      length(matches)
    end

    case :mnesia.transaction(fun) do
      {:atomic, n} -> {:ok, n}
      {:aborted, reason} -> {:error, reason}
    end
  end
end
```

### Step 5: `lib/mnesia_ram_demo/partition_handler.ex`

```elixir
defmodule MnesiaRamDemo.PartitionHandler do
  @moduledoc """
  Subscribes to Mnesia system events and reacts to netsplits.

  Strategy: the lexicographically smallest node wins. Losers stop Mnesia,
  delete their schema, and restart — forcing them to re-sync from the winner.
  This is a *correctness-over-availability* choice: during reconciliation
  the losing nodes reject traffic.
  """
  use GenServer
  require Logger

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(_opts) do
    :mnesia.subscribe(:system)
    {:ok, %{}}
  end

  @impl true
  def handle_info({:mnesia_system_event, event}, state) do
    handle_mnesia_event(event)
    {:noreply, state}
  end

  def handle_info(_other, state), do: {:noreply, state}

  defp handle_mnesia_event({:inconsistent_database, context, peer}) do
    Logger.error("Mnesia split detected (#{context}) against #{inspect(peer)}")

    if node() < peer do
      Logger.warning("This node wins — waiting for peer to resync")
    else
      Logger.warning("This node loses — resetting schema to rejoin #{inspect(peer)}")
      reset_and_rejoin(peer)
    end
  end

  defp handle_mnesia_event(other) do
    Logger.debug("Mnesia system event: #{inspect(other)}")
  end

  defp reset_and_rejoin(peer) do
    :mnesia.stop()
    :mnesia.delete_schema([node()])
    :mnesia.start()
    :mnesia.change_config(:extra_db_nodes, [peer])
  end
end
```

### Step 6: `test/mnesia_ram_demo/session_store_test.exs`

```elixir
defmodule MnesiaRamDemo.SessionStoreTest do
  use ExUnit.Case, async: false

  alias MnesiaRamDemo.SessionStore

  setup do
    :mnesia.clear_table(:sessions)
    :ok
  end

  describe "put/1 and get/1" do
    test "stores and retrieves a session" do
      assert :ok =
               SessionStore.put(%{
                 token: "tkn-1",
                 user_id: "user-1",
                 expires_at: 1_000
               })

      assert {:ok, session} = SessionStore.get("tkn-1")
      assert session.user_id == "user-1"
      assert session.node == node()
    end

    test "returns :not_found for unknown tokens" do
      assert :not_found = SessionStore.get("missing")
    end
  end

  describe "invalidate_user/1" do
    test "deletes every session owned by the user atomically" do
      for i <- 1..5 do
        SessionStore.put(%{
          token: "tkn-#{i}",
          user_id: "target",
          expires_at: 1_000
        })
      end

      SessionStore.put(%{token: "keep", user_id: "other", expires_at: 1_000})

      assert {:ok, 5} = SessionStore.invalidate_user("target")
      assert :not_found = SessionStore.get("tkn-1")
      assert {:ok, _} = SessionStore.get("keep")
    end
  end
end
```

### Step 7: Exercise the cluster

Start two nodes on the same host:

```bash
# Terminal 1
iex --name a@127.0.0.1 --cookie demo -S mix

# Terminal 2
iex --name b@127.0.0.1 --cookie demo -S mix
```

Gossip discovery connects them automatically; the `Schema` GenServer receives
`{:nodeup, :"b@127.0.0.1"}` and calls `:mnesia.add_table_copy/3`. From
node `a`:

```elixir
MnesiaRamDemo.SessionStore.put(%{token: "x", user_id: "u1", expires_at: 9999})
```

From node `b`:

```elixir
MnesiaRamDemo.SessionStore.get("x")
# {:ok, %{token: "x", user_id: "u1", expires_at: 9999, node: :"a@127.0.0.1"}}
```

Simulate a partition by disconnecting and reconnecting:

```elixir
Node.disconnect(:"b@127.0.0.1")
# mutate state on both sides
Node.connect(:"b@127.0.0.1")
# watch the :inconsistent_database event fire in the logs
```

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

## Resources

- [Mnesia User's Guide — erlang.org](https://www.erlang.org/doc/apps/mnesia/users_guide.html)
- [`:mnesia` reference — erlang.org](https://www.erlang.org/doc/man/mnesia.html)
- [Mnesia — The Bad Parts (Dashbit)](https://dashbit.co/blog/mnesia-the-bad-parts) — mandatory reading
- [Learn You Some Erlang — Mnesia](https://learnyousomeerlang.com/mnesia) — chapter on table types and replication
- [libcluster](https://hexdocs.pm/libcluster/readme.html) — cluster formation strategies
- [Horde](https://github.com/derekkraan/horde) — how Horde uses CRDTs instead of Mnesia for distributed registries
