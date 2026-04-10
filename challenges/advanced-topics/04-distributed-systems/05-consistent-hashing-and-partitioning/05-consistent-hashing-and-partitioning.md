<!--
type: reference
difficulty: advanced
section: [04-distributed-systems]
concepts: [consistent-hashing, virtual-nodes, rendezvous-hashing, jump-consistent-hash, partitioning, replication, rebalancing, hash-ring]
languages: [go, rust]
estimated_reading_time: 65 min
bloom_level: analyze
prerequisites: [hash-functions, modular-arithmetic, ring-topology]
papers: [karger-1997-consistent-hashing, lamping-veach-2014-jump-hash, rendezvous-hashing-thaler-1998]
industry_use: [dynamodb, cassandra, redis-cluster, memcached, cdn-routing]
language_contrast: low
-->

# Consistent Hashing and Partitioning

> Consistent hashing solves the "1/N key movement" problem of modular hashing: when you add a node to a cluster, only K/N keys need to move (where K is total keys and N is node count), not K × (1 - 1/(N+1)) — the difference between moving 0.01% of your dataset and moving 99% of it.

## Mental Model

Naive partitioning uses `hash(key) % N` to assign keys to nodes. This works until you add or remove a node: changing N causes `hash(key) % N` to differ from `hash(key) % (N+1)` for almost every key, forcing a massive data migration. A cluster of 10 nodes growing to 11 must move ~91% of its data. This is catastrophic for a live system.

Consistent hashing places both keys and nodes on a conceptual ring (hash space from 0 to 2^32, typically). A key is assigned to the first node clockwise from its position on the ring. When a node is added, only the keys between the new node and its predecessor need to move — roughly 1/N of all keys. When a node is removed, only its keys need to be absorbed by its successor — again roughly 1/N. Every other node is unaffected.

The problem with basic consistent hashing is non-uniform distribution: with N real nodes, the gaps between them on the ring are not equal (they depend on the hash function), so some nodes end up responsible for much larger portions of the ring than others. The fix is virtual nodes: each physical node is represented by V virtual nodes at different positions on the ring. With V=150 virtual nodes per physical node, the standard deviation of load distribution drops to ~10% of the mean, regardless of how heterogeneous the physical cluster is. This is what Cassandra, DynamoDB, and Riak all use.

Consistent hashing's main weakness is that it requires O(log N) lookup time (binary search on the sorted ring) and O(N × V) space. For ultra-low-latency routing (CDN edge selection, session stickiness), alternatives like Rendezvous hashing (O(N) lookup, O(1) space per node) and Jump consistent hash (O(log N), O(1) space total) are used instead.

## Core Concepts

### Consistent Hash Ring with Virtual Nodes

The ring stores all virtual node positions in sorted order. To find the node for a key: hash the key, binary-search the ring for the first virtual node ≥ that hash, and return the physical node it belongs to (wrapping to the first virtual node if past the last one). Virtual nodes ensure load balance without changing the key assignment algorithm.

Replication: to replicate a key on N nodes, walk clockwise from the key's position and collect the next N distinct physical nodes. This is DynamoDB's "preference list" for a key — the ordered set of nodes responsible for storing it, where the first is the primary and the rest are replicas.

### Rendezvous Hashing (Highest Random Weight)

Each node is assigned a weight for each key using `hash(key, node_id)`. The key is assigned to the node with the highest weight. This is O(N) per lookup (compute N hashes) but requires O(1) space — no ring data structure. The key property: when a node is added or removed, only the keys with that node as their highest-weight node are affected.

Rendezvous hashing is used where the node list changes infrequently and lookup performance is not critical: CDN origin selection, routing in service meshes, and session persistence in load balancers.

### Jump Consistent Hash

An algorithm by Lamping & Veach (Google, 2014) that maps a key to one of N buckets with O(log N) time and O(1) space (no data structure). The algorithm is a loop: start with bucket 0, jump to a new bucket using a deterministic PRNG seeded with the key, repeat until the next jump would exceed N. The final bucket is the assignment.

Jump consistent hash achieves perfect uniformity (each bucket gets exactly K/N keys for K keys) and minimal movement on bucket count changes. Its limitation: the bucket count must grow monotonically (only add buckets, never remove from the middle) — it handles "add node" but not "remove specific node." This makes it suitable for batch clusters (add workers as load grows) but not dynamic clusters where any node may fail.

## Implementation: Go

```go
package main

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"sync"
)

// ---- Consistent Hash Ring with Virtual Nodes ----

// virtualNode is a position on the ring belonging to a physical node.
type virtualNode struct {
	hash     uint32
	nodeID   string
	vnodeIdx int // index of this virtual node within the physical node
}

// ConsistentHashRing implements a consistent hash ring with virtual nodes.
type ConsistentHashRing struct {
	mu           sync.RWMutex
	vnodes       []virtualNode // sorted by hash
	vnodesByNode map[string][]uint32 // physical node -> list of its virtual node hashes
	replicas     int // virtual nodes per physical node
}

func NewConsistentHashRing(replicas int) *ConsistentHashRing {
	return &ConsistentHashRing{
		replicas:     replicas,
		vnodesByNode: make(map[string][]uint32),
	}
}

// hash32 returns a 32-bit hash of a string using SHA-256 (deterministic and well-distributed).
func hash32(s string) uint32 {
	h := sha256.Sum256([]byte(s))
	return binary.BigEndian.Uint32(h[:4])
}

// AddNode places a physical node on the ring at `replicas` positions.
func (r *ConsistentHashRing) AddNode(nodeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	hashes := make([]uint32, 0, r.replicas)
	for i := 0; i < r.replicas; i++ {
		key := fmt.Sprintf("%s#%d", nodeID, i)
		h := hash32(key)
		r.vnodes = append(r.vnodes, virtualNode{hash: h, nodeID: nodeID, vnodeIdx: i})
		hashes = append(hashes, h)
	}
	r.vnodesByNode[nodeID] = hashes

	// Keep the ring sorted: binary search depends on sort order
	sort.Slice(r.vnodes, func(i, j int) bool {
		return r.vnodes[i].hash < r.vnodes[j].hash
	})
}

// RemoveNode removes a physical node and all its virtual nodes from the ring.
func (r *ConsistentHashRing) RemoveNode(nodeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	hashes := r.vnodesByNode[nodeID]
	hashSet := make(map[uint32]struct{}, len(hashes))
	for _, h := range hashes {
		hashSet[h] = struct{}{}
	}

	filtered := r.vnodes[:0]
	for _, vn := range r.vnodes {
		if vn.nodeID != nodeID {
			filtered = append(filtered, vn)
		}
	}
	r.vnodes = filtered
	delete(r.vnodesByNode, nodeID)
}

// Get returns the primary node responsible for a key.
func (r *ConsistentHashRing) Get(key string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.vnodes) == 0 {
		return ""
	}
	h := hash32(key)
	// Binary search for the first virtual node with hash >= h
	idx := sort.Search(len(r.vnodes), func(i int) bool {
		return r.vnodes[i].hash >= h
	})
	// Wrap around to the first node if h is beyond the last virtual node
	idx %= len(r.vnodes)
	return r.vnodes[idx].nodeID
}

// GetReplicationTargets returns the N distinct physical nodes responsible for a key.
// The first is the primary; the rest are replicas.
// This implements DynamoDB's "preference list."
func (r *ConsistentHashRing) GetReplicationTargets(key string, n int) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.vnodes) == 0 {
		return nil
	}
	h := hash32(key)
	startIdx := sort.Search(len(r.vnodes), func(i int) bool {
		return r.vnodes[i].hash >= h
	})

	seen := make(map[string]bool)
	var targets []string
	for i := 0; len(targets) < n; i++ {
		idx := (startIdx + i) % len(r.vnodes)
		nodeID := r.vnodes[idx].nodeID
		if !seen[nodeID] {
			seen[nodeID] = true
			targets = append(targets, nodeID)
		}
		if i >= len(r.vnodes) {
			break // fewer distinct physical nodes than n
		}
	}
	return targets
}

// LoadDistribution returns the fraction of the ring each physical node owns.
// Ideally, each node owns 1/N of the ring; virtual nodes make this approximately true.
func (r *ConsistentHashRing) LoadDistribution() map[string]float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.vnodes) == 0 {
		return nil
	}
	spanByNode := make(map[string]uint64)
	ringSize := uint64(math.MaxUint32) + 1

	for i, vn := range r.vnodes {
		prev := r.vnodes[(i-1+len(r.vnodes))%len(r.vnodes)].hash
		var span uint64
		if vn.hash >= prev {
			span = uint64(vn.hash - prev)
		} else {
			// Wraparound: from prev to MaxUint32 then from 0 to vn.hash
			span = uint64(math.MaxUint32-prev) + uint64(vn.hash) + 1
		}
		spanByNode[vn.nodeID] += span
	}

	result := make(map[string]float64, len(spanByNode))
	for nodeID, span := range spanByNode {
		result[nodeID] = float64(span) / float64(ringSize)
	}
	return result
}

// ---- Jump Consistent Hash ----

// JumpHash maps key to a bucket in [0, numBuckets).
// Algorithm: Lamping & Veach, "A Fast, Minimal Memory, Consistent Hash Algorithm" (2014).
// Runs in O(log N) time; uses O(1) space.
func JumpHash(key uint64, numBuckets int) int {
	var b, j int64 = -1, 0
	// PRNG seeded with the key; each step advances to the next candidate bucket
	for j < int64(numBuckets) {
		b = j
		key = key*2862933555777941757 + 1 // LCG PRNG
		j = int64(float64(b+1) * (float64(int64(1)<<31) / float64((key>>33)+1)))
	}
	return int(b)
}

// ---- Rendezvous Hashing (Highest Random Weight) ----

// RendezvousGet returns the node with the highest hash(key, node_id).
// O(N) per call; O(1) space; used when N is small or node list changes rarely.
func RendezvousGet(key string, nodes []string) string {
	if len(nodes) == 0 {
		return ""
	}
	bestNode := ""
	var bestHash uint32 = 0
	for _, node := range nodes {
		h := hash32(key + ":" + node)
		if h > bestHash {
			bestHash = h
			bestNode = node
		}
	}
	return bestNode
}

func main() {
	fmt.Println("=== Consistent Hash Ring (3 nodes, 150 vnodes each) ===")
	ring := NewConsistentHashRing(150)
	ring.AddNode("node-A")
	ring.AddNode("node-B")
	ring.AddNode("node-C")

	// Show load distribution
	dist := ring.LoadDistribution()
	fmt.Println("Load distribution:")
	for node, fraction := range dist {
		fmt.Printf("  %s: %.1f%%\n", node, fraction*100)
	}

	// Show replication targets for a key
	targets := ring.GetReplicationTargets("user:1234", 3)
	fmt.Printf("Replication targets for user:1234: %v\n", targets)

	// Demonstrate key migration on node addition
	testKeys := []string{"user:1", "user:2", "user:3", "user:4", "user:5",
		"order:99", "session:abc", "cache:xyz", "item:42", "event:7"}

	before := make(map[string]string)
	for _, k := range testKeys {
		before[k] = ring.Get(k)
	}

	ring.AddNode("node-D")
	moved := 0
	for _, k := range testKeys {
		after := ring.Get(k)
		if after != before[k] {
			moved++
			fmt.Printf("  Key %q moved: %s -> %s\n", k, before[k], after)
		}
	}
	fmt.Printf("Keys moved after adding node-D: %d/%d (~%.0f%%, expected ~25%%)\n",
		moved, len(testKeys), float64(moved)/float64(len(testKeys))*100)

	fmt.Println("\n=== Jump Consistent Hash ===")
	// Map 1000 keys to 3 buckets, then to 4 buckets
	counts3 := make(map[int]int)
	counts4 := make(map[int]int)
	movedJump := 0
	for i := uint64(0); i < 1000; i++ {
		b3 := JumpHash(i, 3)
		b4 := JumpHash(i, 4)
		counts3[b3]++
		counts4[b4]++
		if b3 != b4 {
			movedJump++
		}
	}
	fmt.Printf("3 buckets: %v\n", counts3)
	fmt.Printf("4 buckets: %v\n", counts4)
	fmt.Printf("Keys moved from 3->4 buckets: %d/1000 (expected ~250)\n", movedJump)

	fmt.Println("\n=== Rendezvous Hashing ===")
	nodes := []string{"node-A", "node-B", "node-C"}
	for _, k := range testKeys[:5] {
		node := RendezvousGet(k, nodes)
		fmt.Printf("  %q -> %s\n", k, node)
	}
}
```

### Go-specific considerations

The `sort.Search` binary search on the sorted `vnodes` slice is the performance-critical path — O(log(N×V)) per lookup. With N=100 nodes and V=150 virtual nodes, this is `log2(15000) ≈ 14` comparisons. In a hot path (millions of requests per second), this fits in a few cache lines.

The `sync.RWMutex` allows concurrent reads (the common case for lookups) while writes (AddNode/RemoveNode) are exclusive. Node additions and removals are rare in practice; the RLock path is what matters for performance.

## Implementation: Rust

```rust
use std::collections::HashMap;
use sha2::{Sha256, Digest};

fn hash32(s: &str) -> u32 {
    let mut hasher = Sha256::new();
    hasher.update(s.as_bytes());
    let result = hasher.finalize();
    u32::from_be_bytes([result[0], result[1], result[2], result[3]])
}

#[derive(Debug, Clone)]
struct VirtualNode {
    hash: u32,
    node_id: String,
}

struct ConsistentHashRing {
    vnodes: Vec<VirtualNode>,  // sorted by hash
    nodes_to_hashes: HashMap<String, Vec<u32>>,
    replicas: usize,
}

impl ConsistentHashRing {
    fn new(replicas: usize) -> Self {
        ConsistentHashRing {
            vnodes: Vec::new(),
            nodes_to_hashes: HashMap::new(),
            replicas,
        }
    }

    fn add_node(&mut self, node_id: &str) {
        let mut hashes = Vec::with_capacity(self.replicas);
        for i in 0..self.replicas {
            let key = format!("{}#{}", node_id, i);
            let h = hash32(&key);
            self.vnodes.push(VirtualNode { hash: h, node_id: node_id.to_string() });
            hashes.push(h);
        }
        self.nodes_to_hashes.insert(node_id.to_string(), hashes);
        // Maintain sorted order for binary search
        self.vnodes.sort_unstable_by_key(|vn| vn.hash);
    }

    fn remove_node(&mut self, node_id: &str) {
        self.vnodes.retain(|vn| vn.node_id != node_id);
        self.nodes_to_hashes.remove(node_id);
    }

    fn get(&self, key: &str) -> Option<&str> {
        if self.vnodes.is_empty() { return None; }
        let h = hash32(key);
        // Binary search: first vnode with hash >= h
        let idx = self.vnodes.partition_point(|vn| vn.hash < h) % self.vnodes.len();
        Some(&self.vnodes[idx].node_id)
    }

    fn get_replication_targets(&self, key: &str, n: usize) -> Vec<&str> {
        if self.vnodes.is_empty() { return Vec::new(); }
        let h = hash32(key);
        let start = self.vnodes.partition_point(|vn| vn.hash < h);
        let mut seen = std::collections::HashSet::new();
        let mut targets = Vec::new();
        for i in 0..self.vnodes.len() {
            if targets.len() >= n { break; }
            let vn = &self.vnodes[(start + i) % self.vnodes.len()];
            if seen.insert(vn.node_id.as_str()) {
                targets.push(vn.node_id.as_str());
            }
        }
        targets
    }

    fn load_distribution(&self) -> HashMap<&str, f64> {
        if self.vnodes.is_empty() { return HashMap::new(); }
        let ring_size = u64::MAX;
        let mut spans: HashMap<&str, u64> = HashMap::new();
        for (i, vn) in self.vnodes.iter().enumerate() {
            let prev = self.vnodes[(i + self.vnodes.len() - 1) % self.vnodes.len()].hash;
            let span = if vn.hash >= prev {
                (vn.hash - prev) as u64
            } else {
                (u32::MAX - prev) as u64 + vn.hash as u64 + 1
            };
            *spans.entry(vn.node_id.as_str()).or_insert(0) += span;
        }
        let total = spans.values().sum::<u64>() as f64;
        spans.into_iter().map(|(k, v)| (k, v as f64 / total)).collect()
    }
}

// Jump consistent hash: O(log N), O(1) space.
fn jump_hash(mut key: u64, num_buckets: i32) -> i32 {
    let (mut b, mut j): (i64, i64) = (-1, 0);
    while j < num_buckets as i64 {
        b = j;
        key = key.wrapping_mul(2862933555777941757).wrapping_add(1);
        j = ((b + 1) as f64 * ((1i64 << 31) as f64 / ((key >> 33) + 1) as f64)) as i64;
    }
    b as i32
}

// Rendezvous hashing: assign key to node with highest hash(key:node_id).
fn rendezvous_get<'a>(key: &str, nodes: &'a [&str]) -> Option<&'a str> {
    nodes.iter().max_by_key(|&&n| hash32(&format!("{}:{}", key, n))).copied()
}

fn main() {
    println!("=== Consistent Hash Ring ===");
    let mut ring = ConsistentHashRing::new(150);
    ring.add_node("node-A");
    ring.add_node("node-B");
    ring.add_node("node-C");

    println!("Load distribution:");
    let mut dist: Vec<_> = ring.load_distribution().into_iter().collect();
    dist.sort_by(|a, b| a.0.cmp(b.0));
    for (node, fraction) in &dist {
        println!("  {}: {:.1}%", node, fraction * 100.0);
    }

    let targets = ring.get_replication_targets("user:1234", 3);
    println!("Replication targets for user:1234: {:?}", targets);

    // Key movement on node addition
    let keys = ["user:1", "user:2", "user:3", "user:4", "user:5"];
    let before: Vec<_> = keys.iter().map(|k| ring.get(k).unwrap_or("").to_string()).collect();
    ring.add_node("node-D");
    let moved = keys.iter().zip(&before)
        .filter(|(k, b)| ring.get(k) != Some(b.as_str()))
        .count();
    println!("Keys moved after adding node-D: {}/{}", moved, keys.len());

    println!("\n=== Jump Consistent Hash ===");
    let (mut c3, mut c4) = ([0i32; 3], [0i32; 4]);
    let mut moved_jump = 0;
    for i in 0u64..1000 {
        let b3 = jump_hash(i, 3) as usize;
        let b4 = jump_hash(i, 4) as usize;
        c3[b3] += 1;
        c4[b4] += 1;
        if b3 != b4 { moved_jump += 1; }
    }
    println!("3 buckets: {:?}", c3);
    println!("4 buckets: {:?}", c4);
    println!("Moved 3->4: {}/1000 (expected ~250)", moved_jump);

    println!("\n=== Rendezvous Hashing ===");
    let nodes = vec!["node-A", "node-B", "node-C"];
    for k in &keys {
        println!("  {:?} -> {:?}", k, rendezvous_get(k, &nodes));
    }
}
```

### Rust-specific considerations

`partition_point` is the Rust equivalent of `sort.Search` — it returns the first index where the predicate is false, performing a binary search. The `% self.vnodes.len()` wrap-around handles the case where the hash exceeds all virtual node positions (wrap to the beginning of the ring).

`sort_unstable_by_key` is preferred over `sort_by_key` for `VirtualNode` because stability is not needed and unstable sort is faster. The `hash32` function using `sha2::Sha256` requires adding `sha2 = "0.10"` to `Cargo.toml`.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Binary search | `sort.Search` (first true in sorted order) | `partition_point` (first false, equivalent) |
| Ring storage | `[]virtualNode` (slice, sorted) | `Vec<VirtualNode>` — identical |
| Load distribution | `map[string]float64` | `HashMap<&str, f64>` — lifetime annotation needed |
| Hash function | `crypto/sha256` in stdlib | `sha2` crate (not in stdlib) |
| Node removal | Manual loop with `[:0]` reuse | `.retain()` — more expressive |
| Jump hash | Direct translation | Direct translation — identical algorithm |

## Production War Stories

**DynamoDB's consistent hashing**: The Dynamo paper describes using a variant of consistent hashing where each node is assigned multiple positions on the ring ("coordinate" nodes). The key production insight: DynamoDB does not use consistent hashing for load balancing within a data center — it uses a centralized partition table (stored in a separate ZooKeeper-like service) for exact key-to-node mappings. Consistent hashing is used for the initial assignment when partitions are created, but the partition table is the source of truth. This avoids the O(V×N) memory requirement of virtual nodes at DynamoDB scale.

**Cassandra's virtual nodes evolution**: Cassandra before version 1.2 used manual token assignment (each node got a fixed position on the ring). This required careful capacity planning: a new node needed a specific token to split a large partition. Cassandra 1.2+ introduced virtual nodes (vnodes) with automatic token assignment. The production finding: with `num_tokens=256` (default in Cassandra 4), load balancing is nearly perfect even with heterogeneous hardware, at the cost of more complex gossip (each node must gossip 256 token positions instead of 1).

**Redis Cluster and hash slots**: Redis Cluster does not use consistent hashing — it uses 16,384 fixed "hash slots" (`CRC16(key) % 16384`). Each node owns a contiguous range of slots. When nodes are added or removed, slots are migrated between nodes. The tradeoff vs. consistent hashing: slot migration is explicit and controllable (you can move exactly the slots you want), but the migration process requires coordinating slot ownership changes across the cluster, which Raft would be used for in a more sophisticated design. Redis Cluster uses a gossip protocol instead, accepting some period of inconsistency during migrations.

**Akamai CDN and consistent hashing**: Consistent hashing was invented at MIT for CDN use (Karger et al., 1997) — the exact problem it solved was routing HTTP requests to cache servers with minimal rebalancing when servers are added or removed. Akamai's production experience shaped the virtual node design: with hundreds of servers, the variance in ring span was unacceptably high without virtual nodes. The Akamai paper reports that 200 virtual nodes per server achieves a standard deviation of ±10% of the mean load, which is acceptable for cache hit rate optimization.

## Fault Model

| Failure | Consistent hashing behavior |
|---|---|
| Node failure | Failed node's keys are served by its successor (next clockwise node); no ring reconfiguration needed until the node is formally removed |
| Node addition | 1/N of keys moved from successor nodes; all other nodes unaffected |
| Node removal | Failed node's keys absorbed by successor; moved keys: ~1/N of total |
| Hash collision (two vnodes same position) | Depends on implementation: typically last-write wins or deterministic tiebreak; rare with 32-bit hashes (birthday collision at ~65,536 vnodes) |
| Hot key (one key receives >N% of traffic) | Consistent hashing does not solve hot key — partition the key space or use client-side key sharding (key + random suffix, merge on read) |
| Uneven key distribution | Even with virtual nodes, certain key patterns may cluster; use a key hash function that distributes well (SHA-256, xxHash) |

## Common Pitfalls

**Pitfall 1: Not using virtual nodes in production**

Basic consistent hashing without virtual nodes assigns each physical node one random ring position. With N=10 nodes, the ring has 10 positions; the expected ratio between the largest and smallest arc is O(log N) for N points uniformly distributed on a circle. For N=10, this means one node may own ~30% of the ring while another owns ~5%. Virtual nodes (V=150 per node) reduce this variance to under 5% of the mean.

**Pitfall 2: Virtual node count too low for heterogeneous hardware**

If nodes have different storage capacities (large node: 16TB, small node: 4TB), you want the large node to own ~4× as many virtual nodes. With only 10 virtual nodes per small node, the variance is still high. Cassandra's `num_tokens` parameter controls this; a 4× capacity ratio requires 4× the virtual node count.

**Pitfall 3: Using modular hash for the virtual node key hash**

The virtual node position is `hash("nodeID#i")`. Using a poor hash function (e.g., `hash = string_sum_of_chars % 2^32`) that does not distribute well will cluster virtual nodes in certain ring sectors, defeating the load balancing purpose. Always use a cryptographic-quality hash (SHA-256, MurmurHash3) for virtual node positions.

**Pitfall 4: Jump consistent hash for clusters with arbitrary node removal**

Jump consistent hash requires that bucket count changes be monotone (only grow). If you have 10 nodes and node 5 fails, you cannot simply remove slot 5 — you must remap slot 5's keys to slot 10 (the last bucket) and then reduce the count to 9. This is not what consistent hashing users expect (they expect the failed node's keys to go to the adjacent healthy node). Use ring-based consistent hashing when arbitrary node failures are expected.

**Pitfall 5: Not handling replication target overlap during node transitions**

When a node is added and keys migrate to it, the replication targets for migrated keys change. If your replication factor is 3 and you add a node between two existing nodes, some keys will now have their 3 replicas distributed differently. During the transition, a read with quorum N=2 may read from a mix of old and new replicas, potentially returning stale data. Production systems handle this with a "transition window" where reads use an expanded quorum (N+1) until the new node is fully populated.

## Exercises

**Exercise 1** (30 min): Instrument the Go implementation to count how many keys in a 1,000-key set change nodes when you: (a) add a 4th node, (b) remove a node. Verify that both operations move approximately 1/4 of the keys (25%), not all of them. Compare with a modular hash (`hash(key) % N`) that would move nearly all keys.

**Exercise 2** (2-4h): Implement weighted virtual nodes: some physical nodes should own a larger share of the ring (e.g., a 16TB node should own 4× the ring of a 4TB node). Modify `AddNode` to accept a `weight` parameter and assign `replicas * weight` virtual nodes. Verify with `LoadDistribution` that the weighted node owns approximately `weight / sum(weights)` of the ring.

**Exercise 3** (4-8h): Implement a consistent hash ring with replication and simulated failure: when a node is removed, reads that hash to that node's range should be redirected to the next replica in the preference list. Add a `GetWithFallback(key string, n int) string` method that returns the first available node from the replication targets, skipping failed nodes. Test with a 5-node cluster where 2 nodes are marked as failed.

**Exercise 4** (8-15h): Build a distributed in-memory key-value store using consistent hashing for partitioning and 3-way replication. Each "node" is a goroutine with its own `map[string]string`. The coordinator (main goroutine) uses the hash ring to route reads and writes. For writes, use quorum W=2 (write to 2/3 replicas, consider successful). For reads, use quorum R=2 (read from 2/3 replicas, return most recent value). Simulate a node failure and verify that reads and writes continue with the remaining 2 replicas.

## Further Reading

### Foundational Papers
- Karger, D. et al. (1997). "Consistent Hashing and Random Trees: Distributed Caching Protocols for Relieving Hot Spots on the World Wide Web." *STOC 1997*. The original paper; defines the ring model and virtual nodes. Section 4 covers the variance analysis for virtual nodes.
- Lamping, J. & Veach, E. (2014). "A Fast, Minimal Memory, Consistent Hash Algorithm." arXiv:1406.2294. The Jump hash paper — 5 pages, includes the mathematical derivation and benchmark comparison with ring-based approaches.
- Thaler, D. & Ravishankar, C. (1998). "Using Name-Based Mappings to Increase Hit Rates." *IEEE/ACM Transactions on Networking*. The original rendezvous hashing paper.

### Books
- Kleppmann, M. (2017). *Designing Data-Intensive Applications*. Chapter 6 (Partitioning) covers consistent hashing, virtual nodes, and rebalancing with production examples from Cassandra, DynamoDB, and Riak.

### Production Code to Read
- Apache Cassandra: `src/java/org/apache/cassandra/dht/` — The `Murmur3Partitioner.java` and token management code. The `TokenMetadata.java` class manages the ring and computes replication targets.
- `stathat/consistent` (https://github.com/stathat/consistent) — A clean Go implementation of consistent hashing with virtual nodes. 200 lines; read-worthy for seeing the ring data structure.
- Redis Cluster source: `src/cluster.c` — The `clusterGetSlaveRank` and `clusterHandleSlaveFailover` functions show how Redis Cluster handles node failure, which is different from consistent hashing's automatic key migration.

### Talks
- Karger, D. (2012): "Consistent Hashing and Why It Matters." MIT OpenCourseWare lecture on distributed systems. The inventor explains the original CDN motivation.
- Lamport, L. (2014): Jump Consistent Hash presented at LADIS workshop — a 10-minute lightning talk that covers the algorithm's derivation visually.
