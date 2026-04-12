# Consistent Hashing Ring with Live Rebalancing

**Project**: `chord_ring` — a production-grade consistent hashing ring with live migration and hotspot detection

---

## Project context

You are building `chord_ring`, a distributed consistent hashing ring with virtual nodes, live node addition and removal with minimal data movement, lazy background migration, and hotspot detection. The ring serves reads and writes during rebalancing without downtime.

Project structure:

```
chord_ring/
├── lib/
│   └── chord_ring/
│       ├── application.ex           # ring supervisor, HTTP monitoring API
│       ├── ring.ex                  # ring data structure: sorted token list, O(log N) lookup
│       ├── node_manager.ex          # GenServer: add/remove physical nodes, update ring
│       ├── shard.ex                 # GenServer per shard: owns a token range, stores KV data
│       ├── replication.ex           # quorum reads/writes across R consecutive ring nodes
│       ├── migration.ex             # FSM: lazy background migration, dual-write protocol
│       ├── hotspot.ex               # sliding window counter, hotspot detection, alerts
│       └── api.ex                   # HTTP monitoring: ring state, migration progress, hotspots
├── test/
│   └── chord_ring/
│       ├── ring_test.exs            # distribution, routing determinism, minimal movement
│       ├── migration_test.exs       # read availability during migration, dual-write correctness
│       ├── replication_test.exs     # quorum reads/writes, fault tolerance
│       ├── hotspot_test.exs         # detection latency, true/false positive
│       └── visualization_test.exs  # ASCII ring output
├── bench/
│   └── ring_bench.exs
└── mix.exs
```

---

## The problem

A distributed data store has N physical nodes. Keys must be assigned to nodes in a way that: distributes load evenly, minimizes data movement when nodes join or leave, and serves reads correctly during migration. The naive approach — `hash(key) mod N` — requires rehashing `(N-1)/N` keys when a node is added. Consistent hashing moves only `1/N` of keys.

The hard part is live migration. When a new node joins and takes ownership of some key ranges, keys already stored on the old node must migrate to the new node. During migration, reads for a not-yet-migrated key must still return the correct value from the source node. Writes must go to both source and destination. This dual-write window must be atomic and correct under failures.

---

## Why this design

**Virtual nodes (`vnodes`) for uniform distribution**: a single token per physical node leads to unequal load (one node might be responsible for 40% of the ring if its token happens to land far from its neighbors). With V=150 virtual nodes per physical node, each physical node has many scattered positions. The variance in load drops to roughly `1/sqrt(V)` of the single-token variance.

**Sorted list with binary search for O(log N) lookup**: the ring is a sorted list of `{token, physical_node}` pairs. Given a key's hash, you binary-search for the first token ≥ hash(key). This is O(log(N*V)) per lookup. A linear scan is O(N*V) and fails the benchmark at scale. Erlang's `:gb_trees` or a sorted list with `:lists.search/2` are both appropriate.

**Lazy migration at configurable rate**: migrating all keys immediately on node join causes a migration storm — 1/N of all keys moving simultaneously, saturating network and disk. Lazy migration runs in the background at max M keys/second. During migration, reads fall back to the source node. After migration, the source removes the key.

**Dual-write protocol**: during migration of a key range, writes go to both source and destination. This ensures that once migration completes, the destination has all writes — not just the pre-join snapshot. The protocol must handle the case where the write to source succeeds but destination fails (write only to source, key stays unmigrated).

---

## Design decisions

**Option A — Jump consistent hashing (Lamping & Veach)**
- Pros: O(ln N) time, zero memory per key.
- Cons: can only add or remove the *last* bucket without remapping; not general enough for arbitrary topology churn.

**Option B — Hash ring with virtual nodes and rendezvous fallback** (chosen)
- Pros: arbitrary add/remove; `1/N` key movement on any topology change; vnodes smooth out per-node load.
- Cons: O(log N) ring lookup; vnode count must be tuned.

→ Chose **B** because production rebalancing must cope with arbitrary node churn, not just appending to the tail; jump hash is not expressive enough.

## Implementation milestones

### Step 1: Create the project

```bash
mix new chord_ring --sup
cd chord_ring
mkdir -p lib/chord_ring test/chord_ring bench
```

### Step 2: `mix.exs` — dependencies

```elixir
defp deps do
  [
    {:plug_cowboy, "~> 2.7"},
    {:jason, "~> 1.4"},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: Ring data structure

```elixir
# lib/chord_ring/ring.ex
defmodule ChordRing.Ring do
  @moduledoc """
  Consistent hashing ring represented as a sorted list of
  {token, physical_node} tuples.

  token is a 32-bit integer derived from hashing "{node_name}:{vnode_index}".
  """

  @doc "Creates a ring with the given physical nodes and V virtual nodes each."
  @spec new([atom()], pos_integer()) :: list()
  def new(nodes, v \\ 150) do
    for node <- nodes, i <- 1..v do
      token = :erlang.phash2("#{node}:#{i}", 0xFFFFFFFF)
      {token, node}
    end
    |> Enum.uniq_by(fn {token, _} -> token end)
    |> Enum.sort_by(fn {token, _} -> token end)
  end

  @doc "Returns the primary physical node responsible for key."
  @spec lookup(list(), binary()) :: atom()
  def lookup(ring, key) do
    hash = :erlang.phash2(key, 0xFFFFFFFF)

    case Enum.find(ring, fn {token, _node} -> token >= hash end) do
      {_token, node} -> node
      nil ->
        {_token, node} = List.first(ring)
        node
    end
  end

  @doc "Returns a list of R consecutive distinct physical nodes starting from key."
  @spec replicas(list(), binary(), pos_integer()) :: [atom()]
  def replicas(ring, key, r) do
    hash = :erlang.phash2(key, 0xFFFFFFFF)
    ring_size = length(ring)
    start_idx = Enum.find_index(ring, fn {token, _} -> token >= hash end) || 0

    Stream.iterate(start_idx, fn i -> rem(i + 1, ring_size) end)
    |> Stream.map(fn i -> elem(Enum.at(ring, i), 1) end)
    |> Stream.uniq()
    |> Enum.take(r)
  end

  @doc "Adds a node to the ring."
  @spec add_node(list(), atom(), pos_integer()) :: list()
  def add_node(ring, node, v \\ 150) do
    new_tokens = for i <- 1..v do
      token = :erlang.phash2("#{node}:#{i}", 0xFFFFFFFF)
      {token, node}
    end

    (ring ++ new_tokens)
    |> Enum.uniq_by(fn {token, _} -> token end)
    |> Enum.sort_by(fn {token, _} -> token end)
  end

  @doc "Returns the fraction of keys that moved when node is added to ring."
  @spec movement_fraction(list(), list(), pos_integer()) :: float()
  def movement_fraction(old_ring, new_ring, sample_size \\ 10_000) do
    keys = for _ <- 1..sample_size, do: :crypto.strong_rand_bytes(8) |> Base.encode16()
    moved = Enum.count(keys, fn k -> lookup(old_ring, k) != lookup(new_ring, k) end)
    moved / sample_size
  end
end
```

### Step 4: Migration FSM

```elixir
# lib/chord_ring/migration.ex
defmodule ChordRing.Migration do
  @moduledoc """
  Lazy migration FSM for a key range.

  States:
    :pending    — range assigned to new node, migration not started
    :migrating  — actively copying keys from source to destination
    :complete   — all keys migrated, source can drop the range

  During :migrating:
    reads  → try destination; fall back to source if key not yet migrated
    writes → write to both source and destination (dual-write)

  Rate limiting: migrate at most max_keys_per_second keys/second.
  Use a token bucket with a GenServer timer to refill tokens.
  """

  @spec start_migration(atom(), atom(), {non_neg_integer(), non_neg_integer()}, keyword()) :: map()
  def start_migration(source_node, dest_node, key_range, opts \\ []) do
    max_keys_per_second = Keyword.get(opts, :max_keys_per_second, 1000)

    %{
      source: source_node,
      dest: dest_node,
      key_range: key_range,
      status: :migrating,
      migrated_keys: MapSet.new(),
      max_rate: max_keys_per_second
    }
  end

  @spec read(term(), map()) :: {:ok, term()} | {:error, :not_found}
  def read(key, migration_state) do
    case migration_state.status do
      :complete ->
        GenServer.call(migration_state.dest, {:get, key})

      :migrating ->
        if MapSet.member?(migration_state.migrated_keys, key) do
          GenServer.call(migration_state.dest, {:get, key})
        else
          GenServer.call(migration_state.source, {:get, key})
        end

      :pending ->
        GenServer.call(migration_state.source, {:get, key})
    end
  end

  @spec write(term(), term(), map()) :: {:ok, map()}
  def write(key, value, migration_state) do
    case migration_state.status do
      :migrating ->
        GenServer.call(migration_state.source, {:put, key, value})
        GenServer.call(migration_state.dest, {:put, key, value})
        {:ok, %{migration_state | migrated_keys: MapSet.put(migration_state.migrated_keys, key)}}

      :complete ->
        GenServer.call(migration_state.dest, {:put, key, value})
        {:ok, migration_state}

      :pending ->
        GenServer.call(migration_state.source, {:put, key, value})
        {:ok, migration_state}
    end
  end
end
```

### Step 5: Shard GenServer

```elixir
# lib/chord_ring/shard.ex
defmodule ChordRing.Shard do
  @moduledoc """
  GenServer per shard that stores key-value data in ETS.
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
end
```

### Step 6: Top-level ChordRing API

```elixir
# lib/chord_ring.ex
defmodule ChordRing do
  @moduledoc """
  Public API for the consistent hashing ring.
  Manages shard processes, routes reads/writes, and handles node additions.
  """

  use GenServer

  defstruct [:ring, :shards, :migrations, :supervisor]

  @spec start(keyword()) :: pid()
  def start(opts) do
    {:ok, pid} = GenServer.start_link(__MODULE__, opts)
    pid
  end

  @spec put(pid(), binary(), term()) :: :ok
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
```

```elixir
# test/chord_ring/migration_test.exs
defmodule ChordRing.MigrationTest do
  use ExUnit.Case, async: false

  test "reads during migration return correct value from source" do
    ring = ChordRing.start(nodes: [:n1, :n2, :n3])
    ChordRing.put(ring, "migrating_key", "original_value")

    # Add n4, triggering migration of some key ranges
    ChordRing.add_node(ring, :n4)

    # Immediately read — migration may not be complete
    {:ok, val} = ChordRing.get(ring, "migrating_key")
    assert val == "original_value",
      "expected original value during migration, got #{inspect(val)}"
  end

  test "writes during migration are visible after migration completes" do
    ring = ChordRing.start(nodes: [:n1, :n2])
    ChordRing.put(ring, "dual_write_key", "v1")
    ChordRing.add_node(ring, :n3)

    ChordRing.put(ring, "dual_write_key", "v2")

    # Wait for migration to complete
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

### Why this works

Virtual nodes hash-spread each physical node into many ring slots, so load distribution is `1 ± ε` even with few nodes. When a node joins or leaves, only the keys that fall into its vnode arcs move, which is provably `1/N` of the keyspace in expectation.

---

## Benchmark

```elixir
# bench/ring_bench.exs
Benchee.run(%{"lookup" => fn -> Ring.locate(ring, "key_#{:rand.uniform(1_000_000)}") end}, time: 10)
```

Target: Lookup p99 < 1 µs at 1024 vnodes; rebalance of 1 M keys on add/remove completes in under 500 ms.

---

## Trade-off analysis

| Aspect | Consistent hashing + vnodes | Modular hashing | Static partition map |
|--------|----------------------------|-----------------|---------------------|
| Keys moved on node add | 1/N | (N-1)/N | 0 (manual reassignment) |
| Load balance | tunable via V | deterministic | fully manual |
| Hotspot mitigation | vnode rebalancing | key-level sharding | manual |
| Lookup cost | O(log(N*V)) | O(1) | O(1) |
| Live migration | lazy background | full rehash required | manual |

Reflection: the Chord DHT protocol proposes O(log N) distributed lookups so no node needs the full routing table. For a system like this one (all nodes accessible, local routing table in memory), why is the O(1) local table approach preferable despite its O(N*V) memory cost?

---

## Common production mistakes

**1. Using MD5 for virtual node hashing**
MD5 has poor uniformity for short inputs like `"node_name:1"`. The distribution test will fail. Use SHA-256 (`:crypto.hash(:sha256, key)`) or `:erlang.phash2/2` with large max value.

**2. Dual-write failure leaves key unmigrated indefinitely**
If the write to destination fails during migration, the migration tracker must not advance past that key. It must retry the write at the configured rate. Without this, migration completes but the destination has a stale snapshot.

**3. Reading from destination before verifying migration status**
Reading from the destination before the key has been migrated returns `not_found` instead of the correct value. You must check migration status per-key, not per-range, or implement full range migration before changing the read routing.

**4. Hotspot detection in the read path**
Incrementing an access counter on every read adds serialization to what should be a concurrent ETS lookup. Sample access frequency in a separate process, not inline in the read path.

## Reflection

- If the workload is 99% reads from a small hot key set, does the vnode-balancing argument still hold? How would you measure it?
- Compare jump hash vs your ring at 3 nodes and at 300 nodes. Where does each win on memory, lookup time, and rebalance cost?

---

## Resources

- DeCandia, G. et al. (2007). *Dynamo: Amazon's Highly Available Key-Value Store* — sections 4.1 (Partitioning), 4.2 (Replication), 4.7 (Membership)
- Stoica, I. et al. (2001). *Chord: A Scalable Peer-to-Peer Lookup Service for Internet Applications*
- Karger, D. et al. (1997). *Consistent Hashing and Random Trees* — the original paper from MIT
- [Apache Cassandra: Data Distribution and Replication](https://cassandra.apache.org/doc/latest/cassandra/architecture/dynamo.html) — production vnode implementation
