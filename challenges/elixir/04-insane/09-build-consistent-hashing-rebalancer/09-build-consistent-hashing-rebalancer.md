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
  def new(nodes, v \\ 150) do
    # TODO
    # HINT: for each node, for i <- 1..v do
    #         token = :erlang.phash2("#{node}:#{i}", 0xFFFFFFFF)
    #         {token, node}
    #       end
    # HINT: sort the resulting list by token
    # HINT: deduplicate tokens (hash collisions are possible with phash2)
  end

  @doc "Returns the primary physical node responsible for key."
  def lookup(ring, key) do
    # TODO: token = hash(key); binary search for first {t, node} where t >= token
    # HINT: if no token >= token, wrap around to the first token (ring topology)
  end

  @doc "Returns a list of R consecutive distinct physical nodes starting from key."
  def replicas(ring, key, r) do
    # TODO
  end

  @doc "Returns the fraction of keys that moved when node is added to ring."
  def movement_fraction(old_ring, new_ring, sample_size \\ 10_000) do
    # TODO: generate random keys, compare lookup results
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

  def start_migration(source_node, dest_node, key_range, opts \\ []) do
    # TODO
  end

  def read(key, migration_state) do
    # TODO: if key is in :complete range, read from dest only
    #        if key is in :migrating range and migrated, read from dest
    #        if key is in :migrating range and not yet migrated, read from source
  end

  def write(key, value, migration_state) do
    # TODO: dual-write if key range is in :migrating state
  end
end
```

### Step 5: Given tests — must pass without modification

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

### Step 6: Run the tests

```bash
mix test test/chord_ring/ --trace
```

### Step 7: Benchmark

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

---

## Resources

- DeCandia, G. et al. (2007). *Dynamo: Amazon's Highly Available Key-Value Store* — sections 4.1 (Partitioning), 4.2 (Replication), 4.7 (Membership)
- Stoica, I. et al. (2001). *Chord: A Scalable Peer-to-Peer Lookup Service for Internet Applications*
- Karger, D. et al. (1997). *Consistent Hashing and Random Trees* — the original paper from MIT
- [Apache Cassandra: Data Distribution and Replication](https://cassandra.apache.org/doc/latest/cassandra/architecture/dynamo.html) — production vnode implementation
