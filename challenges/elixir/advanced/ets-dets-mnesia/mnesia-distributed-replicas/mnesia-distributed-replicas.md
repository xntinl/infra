# Mnesia Distributed with `:ram_copies` and `:disc_copies`

**Project**: `cluster_config` — a configuration service replicated across every BEAM node in a cluster, where reads are local and writes are applied via Mnesia transactions that replicate synchronously.

## The business problem

You run a cluster of BEAM nodes serving a high-traffic API. Feature flags and tenant configuration must be readable with sub-microsecond latency (every request checks them) and updatable by an admin from any node. The update must propagate to all replicas quickly and survive individual node restarts. You do not want to run Redis, etcd, or ZooKeeper for this.

Mnesia's distribution model fits: tables can be replicated on multiple nodes with three storage types:

- **`:ram_copies`**: in memory only; fastest reads, fastest writes; lost on full cluster restart.
- **`:disc_copies`**: in memory *and* on disk; same speed for reads; writes hit both memory and the transaction log.
- **`:disc_only_copies`**: on disk only; slowest; uses DETS; for oversized tables.

Transactions use two-phase commit across replicas — writes are synchronous across nodes in the majority. We configure a mixed setup: two `:disc_copies` for durability and one `:ram_copies` for speed.

## Project structure

```
cluster_config/
├── lib/
│   └── cluster_config/
│       ├── application.ex
│       ├── schema.ex
│       └── config.ex
├── test/
│   └── cluster_config/
│       └── config_test.exs
├── bench/
│   └── replica_bench.exs
├── script/
│   └── main.exs
└── mix.exs
```

## Why Mnesia replication and not libcluster + PubSub

You could store config in each node's ETS and broadcast updates over `Phoenix.PubSub`. Problems:

- a node joining the cluster mid-update misses the broadcast — you need a resync step,
- broadcasts are fire-and-forget; there is no ack or retry,
- split-brain creates two "current" configs with no reconciliation.

Mnesia's 2PC + schema metadata ensures that a writer commits only after a majority of replicas accept — stronger guarantees without extra code.

## Why `:disc_copies` on a subset of nodes

Every replica slows writes (more commit messages). Placing `:disc_copies` on 2–3 "seed" nodes and `:ram_copies` on the rest gives durability (a full cluster restart survives) without paying disk-sync cost on every node.

## Design decisions

- **Option A — single `:disc_only_copies` node**: low write cost but a single point of failure, and non-local reads for all other nodes.
- **Option B — `:ram_copies` everywhere**: fastest reads and writes but no durability.
- **Option C — mixed `:disc_copies` seeds + `:ram_copies` rest** (chosen): durable majority, cheap addition of more read replicas.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule ClusterConfig.MixProject do
  use Mix.Project

  def project do
    [app: :cluster_config, version: "0.1.0", elixir: "~> 1.19", deps: deps()]
  end

  def application do
    [
      extra_applications: [:logger, :mnesia],
      mod: {ClusterConfig.Application, []}
    ]
  end

  defp deps do
    [{:benchee, "~> 1.3", only: :dev}]
  end
end
```

### `mix.exs`
```elixir
```elixir
defmodule ClusterConfig.MixProject do
  use Mix.Project

  def project do
    [app: :cluster_config, version: "0.1.0", elixir: "~> 1.19", deps: deps()]
  end

  def application do
    [
      extra_applications: [:logger, :mnesia],
      mod: {ClusterConfig.Application, []}
    ]
  end

  defp deps do
    [{:benchee, "~> 1.3", only: :dev}]
  end
end
```

### Step 1: Schema setup (safe to run on every node at boot)

**Objective**: Idempotently create a mixed `disc_copies`/`ram_copies` replica set and join peers via `change_config(:extra_db_nodes)`.

```elixir
# lib/cluster_config/schema.ex
defmodule ClusterConfig.Schema do
  @moduledoc "Replicated schema bootstrap. Idempotent across restarts."
  require Logger

  @table :configs

  def setup(opts) do
    disc_nodes = Keyword.get(opts, :disc_nodes, [node()])
    :mnesia.stop()

    case :mnesia.create_schema(disc_nodes) do
      :ok -> Logger.info("mnesia: created schema on #{inspect(disc_nodes)}")
      {:error, {_, {:already_exists, _}}} -> :ok
      {:error, reason} -> Logger.warning("mnesia: create_schema: #{inspect(reason)}")
    end

    :ok = :mnesia.start()

    connect_peers(Node.list())

    create_or_update_table(disc_nodes)
    :ok = :mnesia.wait_for_tables([@table], 10_000)
  end

  defp connect_peers([]), do: :ok

  defp connect_peers(peers) do
    case :mnesia.change_config(:extra_db_nodes, peers) do
      {:ok, connected} -> Logger.info("mnesia: joined #{inspect(connected)}")
      {:error, reason} -> Logger.warning("mnesia: extra_db_nodes: #{inspect(reason)}")
    end
  end

  defp create_or_update_table(disc_nodes) do
    ram_nodes = Node.list() -- disc_nodes

    case :mnesia.create_table(@table,
           attributes: [:key, :value, :updated_at],
           type: :set,
           disc_copies: disc_nodes,
           ram_copies: ram_nodes
         ) do
      {:atomic, :ok} ->
        Logger.info("mnesia: created table #{@table}")

      {:aborted, {:already_exists, _}} ->
        ensure_local_copy(disc_nodes)

      other ->
        Logger.error("mnesia: create_table failed: #{inspect(other)}")
    end
  end

  defp ensure_local_copy(disc_nodes) do
    storage = if node() in disc_nodes, do: :disc_copies, else: :ram_copies

    case :mnesia.add_table_copy(@table, node(), storage) do
      {:atomic, :ok} -> :ok
      {:aborted, {:already_exists, _, _}} -> :ok
      other -> Logger.warning("mnesia: add_table_copy: #{inspect(other)}")
    end
  end
end
```

### Step 2: Public API

**Objective**: Expose dirty local reads for hot paths and transactional writes so cross-node commits stay atomic under 2PC.

```elixir
# lib/cluster_config/config.ex
defmodule ClusterConfig.Config do
  @moduledoc "Cluster-wide config: fast local dirty reads, transactional cross-node writes."

  @table :configs

  @spec put(String.t(), term()) :: :ok | {:error, term()}
  def put(key, value) do
    now = System.system_time(:millisecond)

    tx = fn -> :mnesia.write({@table, key, value, now}) end

    case :mnesia.transaction(tx) do
      {:atomic, :ok} -> :ok
      {:aborted, reason} -> {:error, reason}
    end
  end

  @spec get(String.t()) :: {:ok, term()} | :not_found
  def get(key) do
    case :mnesia.dirty_read({@table, key}) do
      [{@table, ^key, value, _ts}] -> {:ok, value}
      [] -> :not_found
    end
  end

  @spec get_strict(String.t()) :: {:ok, term()} | :not_found
  def get_strict(key) do
    tx = fn ->
      case :mnesia.read({@table, key}) do
        [{@table, ^key, value, _ts}] -> {:ok, value}
        [] -> :not_found
      end
    end

    case :mnesia.transaction(tx) do
      {:atomic, result} -> result
      {:aborted, reason} -> {:error, reason}
    end
  end

  @spec delete(String.t()) :: :ok | {:error, term()}
  def delete(key) do
    tx = fn -> :mnesia.delete({@table, key}) end

    case :mnesia.transaction(tx) do
      {:atomic, :ok} -> :ok
      {:aborted, reason} -> {:error, reason}
    end
  end

  @spec list_keys() :: [String.t()]
  def list_keys, do: :mnesia.dirty_all_keys(@table)

  @spec replica_info() :: %{disc_copies: [node()], ram_copies: [node()]}
  def replica_info do
    %{
      disc_copies: :mnesia.table_info(@table, :disc_copies),
      ram_copies: :mnesia.table_info(@table, :ram_copies)
    }
  end
end
```

### Step 3: Application

**Objective**: Run `Schema.setup/1` at boot so replicas join the cluster before any caller issues a transaction.

```elixir
# lib/cluster_config/application.ex
defmodule ClusterConfig.Application do
  use Application

  @impl true
  def start(_type, _args) do
    disc_nodes =
      Application.get_env(:cluster_config, :disc_nodes, [node()])

    ClusterConfig.Schema.setup(disc_nodes: disc_nodes)

    Supervisor.start_link([], strategy: :one_for_one, name: ClusterConfig.Supervisor)
  end
end
```

## Data flow diagram

```
  Cluster nodes:  a@h (disc)  b@h (disc)  c@h (ram)  d@h (ram)

  Config.put("flag.x", true) called on c@h
    ↓
    :mnesia.transaction starts on c@h
    ↓
    2PC: coordinator sends prepare to a, b, c, d
    ↓
    each replica: acquire write lock, stage write
    ↓
    coordinator sends commit to all
    ↓
    a, b write to ETS + transaction log (disc)
    c, d write to ETS only
    ↓
    {:atomic, :ok}

  Config.get("flag.x") called on d@h
    ↓
    :mnesia.dirty_read → local ETS → {:ok, true}
    (no cross-node traffic)
```

## Why this works

Mnesia transactions use two-phase commit across every replica listed for the table. The transaction succeeds only when every replica acknowledges the commit. `:dirty_read` bypasses locking entirely and consults the local in-memory replica — a single pointer dereference plus an ETS lookup. Mixing `:disc_copies` and `:ram_copies` gives you O(N) commit latency (bounded by the slowest replica's disk sync) but O(1) read latency on every node.

## Tests

```elixir
# test/cluster_config/config_test.exs
defmodule ClusterConfig.ConfigTest do
  use ExUnit.Case, async: false

  alias ClusterConfig.Config

  setup do
    :mnesia.clear_table(:configs)
    :ok
  end

  describe "put/2 and get/1" do
    test "stored value is readable on the same node" do
      :ok = Config.put("flag.x", true)
      assert {:ok, true} = Config.get("flag.x")
    end

    test "get returns :not_found for unknown keys" do
      assert :not_found = Config.get("ghost")
    end
  end

  describe "get_strict/1" do
    test "returns via a transactional read" do
      :ok = Config.put("tenant.acme.tier", :pro)
      assert {:ok, :pro} = Config.get_strict("tenant.acme.tier")
    end
  end

  describe "delete/1" do
    test "removes a key" do
      :ok = Config.put("ephemeral", "v")
      :ok = Config.delete("ephemeral")
      assert :not_found = Config.get("ephemeral")
    end
  end

  describe "list_keys/0" do
    test "lists currently stored keys" do
      :ok = Config.put("a", 1)
      :ok = Config.put("b", 2)
      keys = Config.list_keys()
      assert Enum.sort(keys) == ["a", "b"]
    end
  end

  describe "replica_info/0" do
    test "exposes the disc and ram copy lists" do
      info = Config.replica_info()
      assert is_list(info.disc_copies)
      assert is_list(info.ram_copies)
      assert node() in info.disc_copies or node() in info.ram_copies
    end
  end
end
```

## Benchmark

```elixir
# bench/replica_bench.exs
alias ClusterConfig.Config

for i <- 1..1_000, do: Config.put("warm.#{i}", i)

Benchee.run(
  %{
    "dirty_read (local)" => fn ->
      Config.get("warm.42")
    end,
    "strict_read (transactional)" => fn ->
      Config.get_strict("warm.42")
    end,
    "put (cluster-wide commit)" => fn ->
      Config.put("bench.#{:erlang.unique_integer([:positive])}", :v)
    end
  },
  time: 5,
  warmup: 2,
  parallel: 4
)
```

Target on a single node: dirty read < 2 µs, strict read < 40 µs, put < 150 µs. On a 3-node LAN cluster the `put` latency grows to ~500 µs due to 2PC round-trips; reads remain local and do not change.

## Deep Dive

ETS (Erlang Term Storage) is RAM-only and process-linked; table destruction triggers if the owner crashes, causing silent data loss in careless designs. Match specifications (match_specs) are micro-programs that filter/transform data at the C layer, orders of magnitude faster than fetching all records and filtering in Elixir. Mnesia adds disk persistence and replication but introduces transaction overhead and deadlock potential; dirty operations bypass locks for speed but sacrifice consistency guarantees. For caching, named tables (public by design) are globally visible but require careful name management; consider ETS sharding (multiple small tables) to reduce lock contention on hot keys. DETS (Disk ETS) persists to disk but is single-process bottleneck and slower than a real database. At scale, prefer ETS for in-process state and Mnesia/PostgreSQL for shared, persistent data.
## Advanced Considerations

ETS and DETS performance characteristics change dramatically based on access patterns and table types. Ordered sets provide range queries but slower access than hash tables; set types don't support duplicate keys while bags do. The `heir` option for ETS tables is essential for fault tolerance — when a table owner crashes, the heir process can take ownership and prevent data loss. Without it, the table is lost immediately. Mnesia replicates entire tables across nodes; choosing which nodes should have replicas and whether they're RAM or disk replicas affects both consistency guarantees and network traffic during cluster operations.

DETS persistence comes with significant performance implications — writes are synchronous to disk by default, creating latency spikes. Using `sync: false` improves throughput but risks data loss on crashes. The maximum DETS table size is limited by available memory and the file system; planning capacity requires understanding your growth patterns. Mnesia's transaction system provides ACID guarantees, but dirty operations bypass these guarantees for performance. Understanding when to use dirty reads versus transactional reads significantly impacts both correctness and latency.

Debugging ETS and DETS issues is challenging because problems often emerge under load when many processes contend for the same table. Table memory fragmentation is invisible to code but can exhaust memory. Using match specs instead of iteration over large tables can dramatically improve performance but requires careful construction. The interaction between ETS, replication, and distributed systems creates subtle consistency issues — a node with a stale ETS replica can serve incorrect data during network partitions. Always monitor table sizes and replication status with structured logging.

## Deep Dive: Distributed Patterns and Production Implications

Distributed testing with Peer spawns multiple Erlang nodes in separate BEAM instances, allowing you to test actual node failure, network partitions, and message delays. This is essential for OTP applications but adds latency and complexity. The key insight is that distributed tests reveal assumptions about network reliability that single-node tests cannot—timeouts, partial failures, and split-brain scenarios are invisible to local tests.

---

## Trade-offs and production gotchas

1. **Cluster restart loses `:ram_copies` data**: if every node is down at once and your critical data is in `:ram_copies` only, it is gone. Always keep at least one `:disc_copies` replica.
2. **`extra_db_nodes` is lazy**: calling it does not join existing tables automatically — you must `add_table_copy/3` per table. Have a bootstrap routine that knows the full table list.
3. **Writes scale inversely with replica count**: a 5-replica cluster pays 5 acknowledgments per write. For read-heavy workloads this is fine; for write-heavy ones, consider partitioning.
4. **Netsplit: partitions keep writing locally**: Mnesia allows writes in both halves of a split. On merge, `:mnesia` reports `inconsistent_database` and requires manual resolution. Use `:set_master_nodes/1` or write your own reconciliation policy.
5. **Schema changes require coordination**: adding a column (attribute) to a table is a hot-transformation that must be performed on one node while all others are online. Miss a node and its local replica is stale.
6. **When NOT to use Mnesia**: multi-region clusters (2PC latency across regions is lethal), workloads that outgrow a few hundred GB per table, heavy OLAP. Use Postgres, Cassandra or ClickHouse instead.

## Reflection

Your cluster has `:disc_copies` on nodes 1–2 and `:ram_copies` on nodes 3–5. Node 2 is taken down for maintenance. While it is offline, node 1 reboots unexpectedly. Does the cluster keep accepting writes? If it does, what does the recovery of node 1 look like, and what failure modes should your run-book cover?

### `script/main.exs`
```elixir
defmodule ClusterConfig.MixProject do
  use Mix.Project

  def project do
    [app: :cluster_config, version: "0.1.0", elixir: "~> 1.19", deps: deps()]
  end

  def application do
    [
      extra_applications: [:logger, :mnesia],
      mod: {ClusterConfig.Application, []}
    ]
  end

  defp deps do
    [{:benchee, "~> 1.3", only: :dev}]
  end
end

**Objective**: Idempotently create a mixed `disc_copies`/`ram_copies` replica set and join peers via `change_config(:extra_db_nodes)`.

**Objective**: Expose dirty local reads for hot paths and transactional writes so cross-node commits stay atomic under 2PC.

**Objective**: Run `Schema.setup/1` at boot so replicas join the cluster before any caller issues a transaction.

defmodule Main do
  def main do
      # Demonstrating 378-mnesia-distributed-replicas
      :ok
  end
end

Main.main()
```

---

## Why Mnesia Distributed with ` matters

Mastering **Mnesia Distributed with `** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `lib/cluster_config.ex`

```elixir
defmodule ClusterConfig do
  @moduledoc """
  Reference implementation for Mnesia Distributed with `:ram_copies` and `:disc_copies`.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the cluster_config module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> ClusterConfig.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```

### `test/cluster_config_test.exs`

```elixir
defmodule ClusterConfigTest do
  use ExUnit.Case, async: true

  doctest ClusterConfig

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert ClusterConfig.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts

### 1. Schema bootstrap

Mnesia has a global schema describing tables and replicas. Before creating replicated tables you must:

1. Connect the nodes (libcluster or manual `Node.connect`).
2. `:mnesia.create_schema([nodes...])` on all nodes while Mnesia is stopped.
3. `:mnesia.start()` on every node.
4. `:mnesia.change_config(:extra_db_nodes, [peers])` to tell the local Mnesia about peers.

In production the schema creation is a one-time operation, usually from a migration script or an init container.

### 2. Adding and removing replicas

Live cluster membership changes use `:mnesia.add_table_copy/3` and `:mnesia.del_table_copy/2`. They run transactionally.

### 3. Replication granularity

Mnesia replicates per table. Each table has its own `ram_copies`, `disc_copies`, `disc_only_copies` lists. You can keep `configs` replicated everywhere while `bulky_events` lives only on archive nodes.

### 4. Read locality

`:mnesia.dirty_read/1` always hits the local replica (if present). Transactional reads may go cross-node in some cases — check `:mnesia.table_info(:configs, :where_to_read)`.
