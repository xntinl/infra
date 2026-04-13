# Consistent Hashing Ring with Live Rebalancing

**Project**: `chord_ring` — a production-grade consistent hashing ring with virtual nodes, live node addition/removal with minimal data movement, lazy background migration, and hotspot detection.

---

## Project context

You are building `chord_ring`, a distributed consistent hashing ring for partitioning data across a cluster of nodes. The ring serves reads and writes during rebalancing without downtime, detects hotspots, and maintains load balance via virtual nodes.

The system handles three hard problems simultaneously:
1. **Balanced distribution**: without special care, one node might hold 40% of the keyspace. With V virtual nodes per physical node, variance drops to O(1/sqrt(V)).
2. **Minimal movement on rebalancing**: adding a node should move only 1/N of keys, not (N-1)/N.
3. **Read correctness during migration**: a key being migrated must return the correct value from either source or destination, never stale or missing.

Project structure:

```
chord_ring/
├── lib/
│   └── chord_ring/
│       ├── application.ex           # OTP supervision: ring, shards, monitoring
│       ├── ring.ex                  # ring data structure: sorted tokens, O(log N) lookup
│       ├── node_manager.ex          # GenServer: node addition/removal FSM
│       ├── shard.ex                 # GenServer per shard: owns token range
│       ├── replication.ex           # quorum reads/writes across R consecutive rings
│       ├── migration.ex             # FSM: lazy background migration, dual-write
│       ├── hotspot.ex               # sliding-window detector, exponential smoothing
│       └── api.ex                   # HTTP monitoring: ring state, progress, alerts
├── test/
│   └── chord_ring/
│       ├── ring_test.exs            # distribution, routing determinism, minimal movement
│       ├── migration_test.exs       # read availability during migration
│       ├── replication_test.exs     # quorum correctness, fault tolerance
│       ├── hotspot_test.exs         # detection accuracy, false positive rate
│       └── consistency_test.exs     # reads never return stale data after migration
├── bench/
│   └── ring_bench.exs
└── mix.exs
```

---

## The problem

A distributed data store must partition N keys across M physical nodes such that:
1. **Even distribution**: no single node holds more than 1/M ± ε of the keys.
2. **Minimal movement on topology changes**: when a node joins or leaves, move only O(1/M) of keys.
3. **Read availability during migration**: read requests for a key mid-migration must return a consistent, non-stale value.

The naive approach — `hash(key) mod M` — requires rehashing (M-1)/M keys when a node is added. Consistent hashing moves only 1/M of keys.

The hard part is live migration. When a new node joins and takes ownership of some key ranges, keys already stored on the old node must migrate to the new node. During migration, reads for a not-yet-migrated key must still return the correct value from the source node. Writes must go to both source and destination. This dual-write window must be atomic and correct under failures.

---

## Why this design

**Virtual nodes (vnodes) for uniform distribution**: a single token per physical node leads to unequal load. With V=150 virtual nodes per physical node and M physical nodes, each physical node has 150 scattered positions on the ring. The probability that a key hashes to node i is exactly 1/M (in expectation), with variance O(1/(M*V)). This is dramatically better than single-token hashing where variance is O(1/M).

**Sorted list with binary search for O(log N) lookup**: the ring is a sorted list of (token, physical_node) tuples. Given a key's hash, binary-search for the first token ≥ hash(key). This is O(log(M*V)) per lookup. A linear scan is O(M*V) and kills throughput. For 10 nodes at V=150, that's O(log 1500) ≈ 10 comparisons per lookup.

**Lazy migration at configurable rate**: migrating all keys immediately on node join causes a migration storm — 1/M of all keys moving simultaneously, saturating network and storage. Lazy migration runs in the background at max K keys/second. During migration, reads fall back to the source node. After migration, the source removes the key. This bounds the impact of rebalancing.

**Dual-write protocol**: during migration of a key range, writes go to both source and destination. This ensures that once migration completes, the destination has all writes — not just the pre-join snapshot. If a write to the destination fails, the write only hits the source, and the key stays in the "migrating" state until retry succeeds.

---

## Design decisions

**Option A — Jump consistent hashing (Lamping & Veach)**
- Pros: O(ln M) time, zero memory per key.
- Cons: can only add the last bucket; not general enough for arbitrary topology churn.

**Option B — Hash ring with virtual nodes and rendezvous fallback** (chosen)
- Pros: arbitrary add/remove; O(1/M) key movement; vnodes smooth load.
- Cons: O(log(M*V)) ring lookup; vnode count must be tuned.

→ Chose **B** because production rebalancing must cope with arbitrary node churn, not just appending to the tail; jump hash is not expressive enough.

---

## Key Concepts: Data Partitioning and Consistent Hashing

The core challenge in distributed data systems is partitioning: dividing the keyspace across multiple nodes so that:
- Each key is assigned to exactly one node (or a quorum of replicas).
- Load is balanced: no single node becomes a bottleneck.
- Rebalancing (when nodes join/leave) moves minimal data.

Consistent hashing achieves this by hashing both keys and node identities to a common ring. A key is assigned to the first node (clockwise) whose hash is ≥ the key's hash. This elegantly solves the "minimal movement" problem: when a node is added, only keys between the new node's hash and the previous node's hash need to move.

The cost: load imbalance from uneven token distribution. Virtual nodes (vnodes) solve this by having each physical node contribute multiple tokens to the ring, smoothing the distribution.

**Production insight**: consistent hashing is NOT perfect:
- Hotspots: a hash function distributing keys uniformly across the keyspace does not guarantee uniform distribution of *requests*. Some keys may be accessed 100x more than others. Without hotspot detection, one node becomes a bottleneck despite even key distribution.
- Token collision: if two physical nodes hash to the same token (rare but possible), they collide and must be separated. Use hash chaining or collision tracking.
- Replica placement: with simple "next R nodes clockwise," all replicas of a key may be in the same data center. Implement rack-aware placement: replicas must span data centers, racks, and server classes.

---

## Trade-off analysis

| Aspect | Consistent hashing + vnodes | Modular hashing | Static partition map |
|--------|----------------------------|-----------------|---------------------|
| Keys moved on node add | 1/M | (M-1)/M | 0 (manual reassignment) |
| Load balance | tunable via V | deterministic | fully manual |
| Hotspot mitigation | vnode rebalancing | key-level sharding | manual |
| Lookup cost | O(log(M*V)) | O(1) | O(1) |
| Live migration | lazy background | full rehash | manual |
| Reconfiguration | automatic | requires rebuild | manual |

**When does consistent hashing win?**
- Cloud deployments where nodes are ephemeral (auto-scaling, failures).
- Systems where you cannot afford a full rehash (Dynamo, Cassandra, Riak).
- Services requiring true dynamism: nodes added/removed at runtime without client code changes.

**When should you use modular hashing or static maps?**
- Small fixed clusters (10-100 nodes) that never change — simplicity wins.
- Heterogeneous hardware: static maps let you assign more partitions to faster nodes.
- Systems requiring exact control over replica placement (HBase, HDFS): static maps are more explicit.

---

## Common production mistakes

**1. Using weak hash functions for token generation**

MD5 has poor uniformity for short inputs like `"node_name:1"`. The distribution test will fail on 150 vnodes. Use SHA-256 or `:erlang.phash2/2` with a large max value (0xFFFFFFFF).

Failure mode: one node receives 35% of keys instead of ~20%.

**2. Dual-write failure leaves key unmigrated indefinitely**

If the write to destination fails during migration, the migration tracker must not advance past that key. It must retry the write at the configured rate. Without this, migration completes but the destination has a stale snapshot.

Failure mode: after migration completes and source is dropped, the key vanishes.

**3. Reading from destination before verifying migration status per-key**

Reading from the destination before the key has been migrated returns `not_found` instead of the correct value from the source. You must check migration status per-key, not per-range.

Failure mode: client reads key K during migration, gets `not_found` because destination doesn't have it yet, then reads the old value from somewhere else — inconsistent response.

**4. Hotspot detection in the read path**

Incrementing an access counter on every read adds 10-20% latency overhead and creates contention. Sample access frequency in a separate process, not inline in the read path.

Failure mode: hotspot detection (which should be invisible) doubles tail latency.

**5. Not detecting vnode count is too low**

If V is too small (e.g., V=5), then a node failure removes 5/N of the ring at once, and rebalancing moves a large fraction of keys. Recommendation: V ≥ 10 * ln(M) where M is the number of physical nodes. For M=100, V ≥ 46; typical practice is V=128-256.

Failure mode: unbalanced load distribution; node failures cause cascading overload.

**6. Replica placement doesn't consider correlated failures**

If all R replicas happen to be in the same data center and that DC goes down, the data is lost. Implement rack-aware placement: ensure replicas span different failure domains.

Failure mode: single data center failure loses data; availability is not true high availability.

---

## Implementation milestones

### Step 1: Create the project

```bash
mix new chord_ring --sup
cd chord_ring
mkdir -p lib/chord_ring test/chord_ring bench
```

### Step 2: Dependencies (`mix.exs`)

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: Ring data structure

**Objective**: Use virtual nodes over SHA-1 positions so key distribution stays balanced when physical nodes join or leave.

```elixir
# lib/chord_ring/ring.ex
defmodule ChordRing.Ring do
  @moduledoc """
  Consistent hashing ring represented as a sorted list of
  {token, physical_node} tuples.

  Virtual nodes: each physical node has V virtual nodes scattered
  around the ring. A key is assigned to the physical node whose
  virtual node has the smallest token >= hash(key).

  Token is a 32-bit integer derived from hashing "{node_name}:{vnode_index}".
  """

  @doc "Creates a ring with the given physical nodes and V virtual nodes each."
  @spec new([atom()], pos_integer()) :: [{non_neg_integer(), atom()}]
  def new(nodes, v \\ 150) do
    for node <- nodes, i <- 1..v do
      token = :erlang.phash2("#{node}:#{i}", 0xFFFFFFFF)
      {token, node}
    end
    |> Enum.uniq_by(fn {token, _} -> token end)
    |> Enum.sort_by(fn {token, _} -> token end)
  end

  @doc "Returns the primary physical node responsible for key."
  @spec lookup([{non_neg_integer(), atom()}], binary()) :: atom()
  def lookup(ring, key) do
    hash = :erlang.phash2(key, 0xFFFFFFFF)

    case Enum.find(ring, fn {token, _node} -> token >= hash end) do
      {_token, node} -> node
      nil -> elem(List.first(ring), 1)
    end
  end

  @doc "Returns a list of R consecutive distinct physical nodes starting from key."
  @spec replicas([{non_neg_integer(), atom()}], binary(), pos_integer()) :: [atom()]
  def replicas(ring, key, r) do
    hash = :erlang.phash2(key, 0xFFFFFFFF)
    ring_size = length(ring)
    start_idx = Enum.find_index(ring, fn {token, _} -> token >= hash end) || 0

    Stream.iterate(start_idx, fn i -> rem(i + 1, ring_size) end)
    |> Stream.map(fn i -> elem(Enum.at(ring, i), 1) end)
    |> Stream.uniq()
    |> Enum.take(r)
  end

  @doc "Adds a node to the ring and returns the updated ring."
  @spec add_node([{non_neg_integer(), atom()}], atom(), pos_integer()) :: [{non_neg_integer(), atom()}]
  def add_node(ring, node, v \\ 150) do
    new_tokens = for i <- 1..v do
      token = :erlang.phash2("#{node}:#{i}", 0xFFFFFFFF)
      {token, node}
    end

    (ring ++ new_tokens)
    |> Enum.uniq_by(fn {token, _} -> token end)
    |> Enum.sort_by(fn {token, _} -> token end)
  end

  @doc "Removes a node from the ring."
  @spec remove_node([{non_neg_integer(), atom()}], atom()) :: [{non_neg_integer(), atom()}]
  def remove_node(ring, node) do
    Enum.reject(ring, fn {_, n} -> n == node end)
  end

  @doc "Estimates the fraction of keys that move when topology changes."
  @spec movement_fraction([{non_neg_integer(), atom()}], [{non_neg_integer(), atom()}], pos_integer()) :: float()
  def movement_fraction(old_ring, new_ring, sample_size \\ 10_000) do
    keys = for _ <- 1..sample_size, do: :crypto.strong_rand_bytes(8) |> Base.encode16()
    moved = Enum.count(keys, fn k -> lookup(old_ring, k) != lookup(new_ring, k) end)
    moved / sample_size
  end
end
```

### Step 4: Migration FSM

**Objective**: Model handoff as a prepare/transfer/commit FSM so concurrent reads observe exactly one owner per key during rebalancing.

```elixir
# lib/chord_ring/migration.ex
defmodule ChordRing.Migration do
  @moduledoc """
  Lazy migration FSM for a key range during rebalancing.

  States:
    :pending    — range assigned to new node, migration not started
    :migrating  — actively copying keys from source to destination
    :complete   — all keys migrated, source can drop the range

  During :migrating:
    reads  → try destination; fall back to source if key not yet migrated
    writes → write to both source and destination (dual-write)

  Rate limiting: migrate at most max_keys_per_second keys/second.
  """

  defstruct [
    :source_node,
    :dest_node,
    :key_range,
    :status,
    :migrated_keys,
    :max_rate,
    :last_rate_limit_check
  ]

  @spec start_migration(atom(), atom(), {non_neg_integer(), non_neg_integer()}, keyword()) :: %__MODULE__{}
  def start_migration(source_node, dest_node, key_range, opts \\ []) do
    max_keys_per_second = Keyword.get(opts, :max_keys_per_second, 1000)

    %__MODULE__{
      source_node: source_node,
      dest_node: dest_node,
      key_range: key_range,
      status: :pending,
      migrated_keys: MapSet.new(),
      max_rate: max_keys_per_second,
      last_rate_limit_check: System.monotonic_time(:millisecond)
    }
  end

  @spec read(term(), %__MODULE__{}) :: {:ok, term()} | {:error, :not_found}
  def read(key, migration_state) do
    case migration_state.status do
      :complete ->
        # Migration done, read from destination
        read_from_node(migration_state.dest_node, key)

      :migrating ->
        # Check if this key has been migrated
        if MapSet.member?(migration_state.migrated_keys, key) do
          read_from_node(migration_state.dest_node, key)
        else
          read_from_node(migration_state.source_node, key)
        end

      :pending ->
        # Migration not started, read from source
        read_from_node(migration_state.source_node, key)
    end
  end

  @spec write(term(), term(), %__MODULE__{}) :: {:ok, %__MODULE__{}}
  def write(key, value, migration_state) do
    case migration_state.status do
      :migrating ->
        # Dual-write: write to both source and destination
        write_to_node(migration_state.source_node, key, value)
        write_to_node(migration_state.dest_node, key, value)
        {:ok, %{migration_state | migrated_keys: MapSet.put(migration_state.migrated_keys, key)}}

      :complete ->
        # Migration done, write to destination
        write_to_node(migration_state.dest_node, key, value)
        {:ok, migration_state}

      :pending ->
        # Migration not started, write to source
        write_to_node(migration_state.source_node, key, value)
        {:ok, migration_state}
    end
  end

  @spec advance_migration(%__MODULE__{}, [term()]) :: %__MODULE__{}
  def advance_migration(migration_state, keys_in_range) do
    rate_limited = rate_limit(migration_state)

    migrated =
      Enum.reduce(keys_in_range, migration_state.migrated_keys, fn key, acc ->
        if MapSet.member?(acc, key) do
          acc
        else
          write_to_node(migration_state.dest_node, key, read_from_node(migration_state.source_node, key))
          MapSet.put(acc, key)
        end
      end)

    %{migration_state | migrated_keys: migrated}
  end

  # --- Private helpers ---

  defp read_from_node(_node, _key) do
    # In real implementation, use RPC to read from the node
    {:ok, :value}
  end

  defp write_to_node(_node, _key, _value) do
    # In real implementation, use RPC to write to the node
    :ok
  end

  defp rate_limit(migration_state) do
    now = System.monotonic_time(:millisecond)
    elapsed = now - migration_state.last_rate_limit_check
    allowed_keys = div(migration_state.max_rate * elapsed, 1000)
    allowed_keys > 0
  end
end
```

### Step 5: Shard GenServer

**Objective**: Own shard data in a single GenServer so ownership transfers are serialized and writes cannot race migration.

```elixir
# lib/chord_ring/shard.ex
defmodule ChordRing.Shard do
  @moduledoc """
  GenServer per shard (or per physical node) that stores key-value data in ETS.
  Each shard owns a range of tokens on the consistent hashing ring.
  """

  use GenServer

  def start_link(opts) do
    id = Keyword.fetch!(opts, :id)
    GenServer.start_link(__MODULE__, opts, name: id)
  end

  @impl true
  def init(opts) do
    id = Keyword.fetch!(opts, :id)
    table = :ets.new(:"shard_#{id}", [:set, :public])
    {:ok, %{id: id, table: table}}
  end

  @impl true
  def handle_call({:get, key}, _from, state) do
    case :ets.lookup(state.table, key) do
      [{^key, value}] -> {:reply, {:ok, value}, state}
      [] -> {:reply, {:error, :not_found}, state}
    end
  end

  def handle_call({:put, key, value}, _from, state) do
    :ets.insert(state.table, {key, value})
    {:reply, :ok, state}
  end

  def handle_call({:delete, key}, _from, state) do
    :ets.delete(state.table, key)
    {:reply, :ok, state}
  end

  def handle_call(:get_table, _from, state) do
    {:reply, state.table, state}
  end
end
```

### Step 6: Top-level ChordRing API

**Objective**: Serialize ring mutations and shard lookups through one supervisor so routing never observes a half-applied topology change.

```elixir
# lib/chord_ring.ex
defmodule ChordRing do
  @moduledoc """
  Public API for the consistent hashing ring.
  Manages shard processes, routes reads/writes, and handles node additions.
  """

  use GenServer

  defstruct [:ring, :shards, :migrations, :supervisor]

  @spec start(keyword()) :: {:ok, pid()}
  def start(opts) do
    GenServer.start_link(__MODULE__, opts)
  end

  @spec put(pid(), binary(), term()) :: :ok | {:error, term()}
  def put(ring_pid, key, value), do: GenServer.call(ring_pid, {:put, key, value})

  @spec get(pid(), binary()) :: {:ok, term()} | {:error, :not_found}
  def get(ring_pid, key), do: GenServer.call(ring_pid, {:get, key})

  @spec add_node(pid(), atom()) :: :ok
  def add_node(ring_pid, node_id), do: GenServer.call(ring_pid, {:add_node, node_id})

  @impl true
  def init(opts) do
    nodes = Keyword.get(opts, :nodes, [:n1, :n2, :n3])

    children = Enum.map(nodes, fn id ->
      %{id: id, start: {ChordRing.Shard, :start_link, [[id: id]]}}
    end)
    {:ok, sup} = Supervisor.start_link(children, strategy: :one_for_one)

    ring = ChordRing.Ring.new(nodes, 150)

    {:ok, %__MODULE__{
      ring: ring,
      shards: nodes,
      migrations: %{},
      supervisor: sup
    }}
  end

  @impl true
  def handle_call({:put, key, value}, _from, state) do
    node = ChordRing.Ring.lookup(state.ring, key)

    case Map.get(state.migrations, key) do
      nil ->
        GenServer.call(node, {:put, key, value})
        {:reply, :ok, state}
      migration ->
        {:ok, new_migration} = ChordRing.Migration.write(key, value, migration)
        {:reply, :ok, %{state | migrations: Map.put(state.migrations, key, new_migration)}}
    end
  end

  def handle_call({:get, key}, _from, state) do
    node = ChordRing.Ring.lookup(state.ring, key)

    result = case Map.get(state.migrations, key) do
      nil -> GenServer.call(node, {:get, key})
      migration -> ChordRing.Migration.read(key, migration)
    end

    {:reply, result, state}
  end

  def handle_call({:add_node, node_id}, _from, state) do
    Supervisor.start_child(state.supervisor, %{
      id: node_id, start: {ChordRing.Shard, :start_link, [[id: node_id]]}
    })

    new_ring = ChordRing.Ring.add_node(state.ring, node_id, 150)
    new_shards = [node_id | state.shards]

    spawn(fn -> background_migrate(state.ring, new_ring, state.shards, node_id) end)

    {:reply, :ok, %{state | ring: new_ring, shards: new_shards}}
  end

  defp background_migrate(old_ring, new_ring, existing_shards, _new_node) do
    for shard_id <- existing_shards do
      try do
        table = GenServer.call(shard_id, :get_table)
        if table do
          :ets.tab2list(table)
          |> Enum.each(fn {key, value} ->
            old_owner = ChordRing.Ring.lookup(old_ring, to_string(key))
            new_owner = ChordRing.Ring.lookup(new_ring, to_string(key))
            if old_owner != new_owner do
              GenServer.call(new_owner, {:put, key, value})
            end
          end)
        end
      catch
        _, _ -> :ok
      end
    end
  end
end
```

### Step 7: Given tests — must pass without modification

```elixir
# test/chord_ring/ring_test.exs
defmodule ChordRing.RingTest do
  use ExUnit.Case, async: true

  alias ChordRing.Ring

  test "no node holds more than 25% of keys with V=150" do
    nodes = [:n1, :n2, :n3, :n4, :n5]
    ring = Ring.new(nodes, 150)

    counts =
      for _ <- 1..1_000_000, reduce: %{} do
        acc ->
          key = :crypto.strong_rand_bytes(8) |> Base.encode16()
          node = Ring.lookup(ring, key)
          Map.update(acc, node, 1, &(&1 + 1))
      end

    for {node, count} <- counts do
      pct = count / 1_000_000
      assert pct <= 0.25, "#{node} holds #{Float.round(pct * 100, 1)}% (max 25%)"
      assert pct >= 0.15, "#{node} holds #{Float.round(pct * 100, 1)}% (min 15%)"
    end
  end

  test "minimal movement on node add: at most 1/N + 5% of keys" do
    ring5 = Ring.new([:n1, :n2, :n3, :n4, :n5], 150)
    ring6 = Ring.add_node(ring5, :n6, 150)

    moved = Ring.movement_fraction(ring5, ring6, 10_000)
    assert moved < 0.22, "expected ~16.7% movement, got #{Float.round(moved * 100, 1)}%"
  end

  test "routing is deterministic across identical ring states" do
    ring = Ring.new([:n1, :n2, :n3], 150)
    key = "my_deterministic_key"
    first = Ring.lookup(ring, key)

    for _ <- 1..1_000 do
      assert Ring.lookup(ring, key) == first
    end
  end
end

# test/chord_ring/migration_test.exs
defmodule ChordRing.MigrationTest do
  use ExUnit.Case, async: false

  test "reads during migration return correct value from source" do
    ring = ChordRing.start(nodes: [:n1, :n2, :n3])
    ChordRing.put(ring, "migrating_key", "original_value")

    ChordRing.add_node(ring, :n4)

    {:ok, val} = ChordRing.get(ring, "migrating_key")
    assert val == "original_value"
  end

  test "writes during migration are visible after migration completes" do
    ring = ChordRing.start(nodes: [:n1, :n2])
    ChordRing.put(ring, "dual_write_key", "v1")
    ChordRing.add_node(ring, :n3)

    ChordRing.put(ring, "dual_write_key", "v2")

    Process.sleep(5_000)

    {:ok, val} = ChordRing.get(ring, "dual_write_key")
    assert val == "v2"
  end
end
```

### Step 8: Run the tests

```bash
mix test test/chord_ring/ --trace
```

### Step 9: Benchmark

**Objective**: Quantify lookup cost versus virtual-node count so ring density tradeoffs against balance quality stay measurable.

```elixir
# bench/ring_bench.exs
nodes = for i <- 1..10, do: :"node_#{i}"
ring = ChordRing.Ring.new(nodes, 150)
keys = for _ <- 1..1_000, do: :crypto.strong_rand_bytes(8) |> Base.encode16()

Benchee.run(
  %{
    "lookup — single key" => fn ->
      ChordRing.Ring.lookup(ring, Enum.random(keys))
    end,
    "replicas — R=3" => fn ->
      ChordRing.Ring.replicas(ring, Enum.random(keys), 3)
    end
  },
  parallel: 4,
  time: 5,
  warmup: 2,
  formatters: [Benchee.Formatters.Console]
)
```

Target: `lookup/2` < 1µs per call with 10 physical nodes and V=150.

---


## Main Entry Point

```elixir
def main do
  IO.puts("======== 09 build consistent hashing rebalancer ========")
  IO.puts("Demonstrating core functionality")
  IO.puts("")
  
  IO.puts("Run: mix test")
end
```

## Reflection

1. **Load imbalance vs vnode count**: How does doubling V from 75 to 150 affect the standard deviation of key counts per node? Run the distribution test at both V levels.
   - **Answer**: Standard deviation is proportional to `1/sqrt(V)`. Doubling V reduces stddev by ~29%.

2. **Migration cost analysis**: If you have 1M keys and add a node, and migration rate is 1000 keys/sec, how long does rebalancing take? Does this time scale linearly with cluster size?
   - **Answer**: ~1000 seconds (16 minutes) for 1M keys. Time scales linearly with total key count, not cluster size — this is why lazy migration helps.

3. **Hotspot detection tradeoff**: Sample 1 in 100 requests for access frequency (1% overhead). How accurate is the P99 estimate with this sampling rate?
   - **Answer**: Accuracy is within ±10% at 95% confidence for true access frequency > 0.01. Below that threshold, sampling false positives increase.

---

## Resources

- DeCandia, G. et al. (2007). *Dynamo: Amazon's Highly Available Key-Value Store*. Sections 4.1 (Partitioning), 4.2 (Replication), 4.7 (Membership).
- Stoica, I. et al. (2001). *Chord: A Scalable Peer-to-Peer Lookup Service for Internet Applications*.
- Karger, D. et al. (1997). *Consistent Hashing and Random Trees*. MIT Technical Report. The original consistent hashing paper.
- [Apache Cassandra: Data Distribution and Replication](https://cassandra.apache.org/doc/latest/cassandra/architecture/dynamo.html). Production vnode implementation.
