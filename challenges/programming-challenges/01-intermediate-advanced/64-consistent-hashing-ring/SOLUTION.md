# Solution: Consistent Hashing Ring

## Architecture Overview

The implementation consists of three layers: the hash ring core (sorted virtual node slice with binary search), a key registry that tracks which keys are assigned where (for migration computation), and an analysis layer that computes distribution statistics and renders the ring.

The ring is a sorted slice of `vnode` structs. Each `vnode` holds a `hash uint64` and a `nodeID string` (the physical node it belongs to). Lookups use `sort.Search` for O(log N) binary search. Replication walks clockwise from the found position, collecting distinct physical nodes until the replication factor is satisfied.

Node addition and removal recompute affected key assignments by iterating the key registry. This is an O(K) management-plane operation, acceptable because membership changes are rare compared to key lookups.

## Go Solution

### Project Setup

```bash
mkdir -p chash && cd chash
go mod init chash
```

### Implementation

```go
// ring.go
package chash

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
)

type vnode struct {
	hash   uint64
	nodeID string
	index  int
}

type PhysicalNode struct {
	ID           string
	VirtualCount int
}

type MigrationEntry struct {
	Key      string
	FromNode string
	ToNode   string
}

type Ring struct {
	mu              sync.RWMutex
	vnodes          []vnode
	nodes           map[string]*PhysicalNode
	keys            map[string]string // key -> owning physical node
	replicationFactor int
}

func NewRing(replicationFactor int) *Ring {
	return &Ring{
		nodes:             make(map[string]*PhysicalNode),
		keys:              make(map[string]string),
		replicationFactor: replicationFactor,
	}
}

func hashKey(key string) uint64 {
	h := sha256.Sum256([]byte(key))
	return binary.BigEndian.Uint64(h[:8])
}

func vnodeKey(nodeID string, idx int) string {
	return fmt.Sprintf("%s#%d", nodeID, idx)
}

func (r *Ring) AddNode(node PhysicalNode) []MigrationEntry {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.nodes[node.ID]; exists {
		return nil
	}

	r.nodes[node.ID] = &node

	for i := 0; i < node.VirtualCount; i++ {
		h := hashKey(vnodeKey(node.ID, i))
		r.vnodes = append(r.vnodes, vnode{hash: h, nodeID: node.ID, index: i})
	}

	sort.Slice(r.vnodes, func(i, j int) bool {
		return r.vnodes[i].hash < r.vnodes[j].hash
	})

	var migrations []MigrationEntry
	for key, oldOwner := range r.keys {
		newOwner := r.primaryNodeLocked(key)
		if newOwner != oldOwner {
			migrations = append(migrations, MigrationEntry{Key: key, FromNode: oldOwner, ToNode: newOwner})
			r.keys[key] = newOwner
		}
	}

	return migrations
}

func (r *Ring) RemoveNode(nodeID string) []MigrationEntry {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.nodes[nodeID]; !exists {
		return nil
	}

	filtered := r.vnodes[:0]
	for _, vn := range r.vnodes {
		if vn.nodeID != nodeID {
			filtered = append(filtered, vn)
		}
	}
	r.vnodes = filtered
	delete(r.nodes, nodeID)

	var migrations []MigrationEntry
	for key, oldOwner := range r.keys {
		if oldOwner != nodeID {
			continue
		}
		if len(r.vnodes) == 0 {
			delete(r.keys, key)
			continue
		}
		newOwner := r.primaryNodeLocked(key)
		r.keys[key] = newOwner
		migrations = append(migrations, MigrationEntry{Key: key, FromNode: nodeID, ToNode: newOwner})
	}

	return migrations
}

func (r *Ring) Get(key string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.replicaNodesLocked(key)
}

func (r *Ring) AssignKey(key string) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.vnodes) == 0 {
		return ""
	}

	owner := r.primaryNodeLocked(key)
	r.keys[key] = owner
	return owner
}

func (r *Ring) primaryNodeLocked(key string) string {
	if len(r.vnodes) == 0 {
		return ""
	}

	h := hashKey(key)
	idx := sort.Search(len(r.vnodes), func(i int) bool {
		return r.vnodes[i].hash >= h
	})
	if idx == len(r.vnodes) {
		idx = 0
	}
	return r.vnodes[idx].nodeID
}

func (r *Ring) replicaNodesLocked(key string) []string {
	if len(r.vnodes) == 0 {
		return nil
	}

	h := hashKey(key)
	idx := sort.Search(len(r.vnodes), func(i int) bool {
		return r.vnodes[i].hash >= h
	})
	if idx == len(r.vnodes) {
		idx = 0
	}

	seen := make(map[string]bool)
	var replicas []string

	for i := 0; i < len(r.vnodes) && len(replicas) < r.replicationFactor; i++ {
		pos := (idx + i) % len(r.vnodes)
		nid := r.vnodes[pos].nodeID
		if !seen[nid] {
			seen[nid] = true
			replicas = append(replicas, nid)
		}
	}

	return replicas
}

// Distribution returns per-node key counts.
func (r *Ring) Distribution() map[string]int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	dist := make(map[string]int)
	for _, nid := range r.keys {
		dist[nid]++
	}
	return dist
}

type DistributionStats struct {
	PerNode  map[string]int
	Mean     float64
	StdDev   float64
	MinCount int
	MaxCount int
	MinNode  string
	MaxNode  string
	CoeffVar float64
}

func (r *Ring) Stats() DistributionStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	perNode := make(map[string]int)
	for _, node := range r.nodes {
		perNode[node.ID] = 0
	}
	for _, nid := range r.keys {
		perNode[nid]++
	}
	if len(perNode) == 0 {
		return DistributionStats{PerNode: perNode}
	}

	total, minCount, maxCount := 0, math.MaxInt, 0
	var minNode, maxNode string
	for nid, count := range perNode {
		total += count
		if count < minCount { minCount = count; minNode = nid }
		if count > maxCount { maxCount = count; maxNode = nid }
	}
	mean := float64(total) / float64(len(perNode))
	var sumSq float64
	for _, c := range perNode { d := float64(c) - mean; sumSq += d * d }
	stddev := math.Sqrt(sumSq / float64(len(perNode)))
	cv := 0.0
	if mean > 0 { cv = stddev / mean }

	return DistributionStats{perNode, mean, stddev, minCount, maxCount, minNode, maxNode, cv}
}

// Visualize prints a text representation of the ring showing arc ownership per node.
func (r *Ring) Visualize(width int) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.vnodes) == 0 {
		return "[empty ring]"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Ring (%d physical nodes, %d virtual nodes)\n", len(r.nodes), len(r.vnodes)))

	maxHash := uint64(math.MaxUint64)
	arcOwnership := make(map[string]float64)
	for i, vn := range r.vnodes {
		var arcLen uint64
		if i == 0 {
			arcLen = vn.hash + (maxHash - r.vnodes[len(r.vnodes)-1].hash)
		} else {
			arcLen = vn.hash - r.vnodes[i-1].hash
		}
		arcOwnership[vn.nodeID] += float64(arcLen) / float64(maxHash) * 100.0
	}

	for nid, pct := range arcOwnership {
		barLen := max(1, int(pct/100.0*float64(width-20)))
		sb.WriteString(fmt.Sprintf("  %-10s %5.1f%% %s\n", nid, pct, strings.Repeat("█", barLen)))
	}
	return sb.String()
}

func (r *Ring) NodeCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.nodes)
}

func (r *Ring) KeyCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.keys)
}
```

### Tests

```go
// ring_test.go
package chash

import (
	"fmt"
	"math"
	"testing"
)

func TestBasicKeyLookup(t *testing.T) {
	ring := NewRing(3)

	ring.AddNode(PhysicalNode{ID: "node-A", VirtualCount: 150})
	ring.AddNode(PhysicalNode{ID: "node-B", VirtualCount: 150})
	ring.AddNode(PhysicalNode{ID: "node-C", VirtualCount: 150})

	replicas := ring.Get("user:1234")
	if len(replicas) != 3 {
		t.Fatalf("expected 3 replicas, got %d", len(replicas))
	}

	// All replicas must be distinct physical nodes
	seen := make(map[string]bool)
	for _, r := range replicas {
		if seen[r] {
			t.Fatalf("duplicate replica: %s", r)
		}
		seen[r] = true
	}
}

func TestReplicationWithFewerNodes(t *testing.T) {
	ring := NewRing(5) // asking for 5 replicas

	ring.AddNode(PhysicalNode{ID: "node-A", VirtualCount: 100})
	ring.AddNode(PhysicalNode{ID: "node-B", VirtualCount: 100})

	replicas := ring.Get("key")
	if len(replicas) != 2 {
		t.Fatalf("expected 2 replicas (only 2 nodes), got %d", len(replicas))
	}
}

func TestMinimalRedistributionOnAdd(t *testing.T) {
	ring := NewRing(1)

	// Start with 10 nodes
	for i := 0; i < 10; i++ {
		ring.AddNode(PhysicalNode{ID: fmt.Sprintf("node-%d", i), VirtualCount: 150})
	}

	// Assign 10K keys
	totalKeys := 10000
	for i := 0; i < totalKeys; i++ {
		ring.AssignKey(fmt.Sprintf("key-%d", i))
	}

	// Add an 11th node
	migrations := ring.AddNode(PhysicalNode{ID: "node-new", VirtualCount: 150})

	migrationPct := float64(len(migrations)) / float64(totalKeys) * 100.0
	t.Logf("Keys migrated: %d / %d (%.1f%%)", len(migrations), totalKeys, migrationPct)

	// Ideal redistribution is 1/11 = ~9.1%. Allow up to 15%.
	if migrationPct > 15.0 {
		t.Errorf("too many migrations: %.1f%% (expected < 15%%)", migrationPct)
	}
}

func TestRemoveNodeMigratesOnlyItsKeys(t *testing.T) {
	ring := NewRing(1)

	for i := 0; i < 5; i++ {
		ring.AddNode(PhysicalNode{ID: fmt.Sprintf("node-%d", i), VirtualCount: 150})
	}

	totalKeys := 5000
	for i := 0; i < totalKeys; i++ {
		ring.AssignKey(fmt.Sprintf("key-%d", i))
	}

	distBefore := ring.Distribution()
	keysOnNode2 := distBefore["node-2"]

	migrations := ring.RemoveNode("node-2")

	if len(migrations) != keysOnNode2 {
		t.Errorf("expected %d migrations, got %d", keysOnNode2, len(migrations))
	}

	// Verify all migrations come from node-2
	for _, m := range migrations {
		if m.FromNode != "node-2" {
			t.Errorf("unexpected migration from %s (expected node-2)", m.FromNode)
		}
	}
}

func TestDistributionUniformity(t *testing.T) {
	ring := NewRing(1)

	nodeCount := 10
	for i := 0; i < nodeCount; i++ {
		ring.AddNode(PhysicalNode{ID: fmt.Sprintf("node-%d", i), VirtualCount: 150})
	}

	keyCount := 100000
	for i := 0; i < keyCount; i++ {
		ring.AssignKey(fmt.Sprintf("key-%d", i))
	}

	stats := ring.Stats()
	t.Logf("Mean: %.0f, StdDev: %.0f, CoeffVar: %.3f", stats.Mean, stats.StdDev, stats.CoeffVar)
	t.Logf("Min: %d (%s), Max: %d (%s)", stats.MinCount, stats.MinNode, stats.MaxCount, stats.MaxNode)

	// StdDev should be < 10% of mean
	threshold := stats.Mean * 0.10
	if stats.StdDev > threshold {
		t.Errorf("distribution too uneven: stddev=%.0f > 10%% of mean (%.0f)", stats.StdDev, threshold)
	}
}

func TestIdempotentAddNode(t *testing.T) {
	ring := NewRing(1)
	ring.AddNode(PhysicalNode{ID: "node-A", VirtualCount: 100})
	migrations := ring.AddNode(PhysicalNode{ID: "node-A", VirtualCount: 100})

	if len(migrations) != 0 {
		t.Error("adding same node twice should produce no migrations")
	}
	if ring.NodeCount() != 1 {
		t.Error("duplicate add should not increase node count")
	}
}

func TestEmptyRing(t *testing.T) {
	ring := NewRing(3)

	replicas := ring.Get("any-key")
	if len(replicas) != 0 {
		t.Error("empty ring should return no replicas")
	}

	owner := ring.AssignKey("key")
	if owner != "" {
		t.Error("empty ring should return empty owner")
	}
}

func BenchmarkLookup(b *testing.B) {
	ring := NewRing(3)
	for i := 0; i < 20; i++ {
		ring.AddNode(PhysicalNode{ID: fmt.Sprintf("node-%d", i), VirtualCount: 200})
	}
	keys := make([]string, 10000)
	for i := range keys {
		keys[i] = fmt.Sprintf("key-%d", i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ring.Get(keys[i%len(keys)])
	}
}
```

### Running and Testing

```bash
go test -v -race ./...
go test -bench=. -benchmem ./...
go test -v -run TestDistributionUniformity ./...
```

### Expected Output

```
=== RUN   TestBasicKeyLookup
--- PASS: TestBasicKeyLookup (0.00s)
=== RUN   TestMinimalRedistributionOnAdd
    ring_test.go:52: Keys migrated: 876 / 10000 (8.8%)
--- PASS: TestMinimalRedistributionOnAdd (0.02s)
=== RUN   TestDistributionUniformity
    ring_test.go:78: Mean: 10000, StdDev: 312, CoeffVar: 0.031
    ring_test.go:79: Min: 9543 (node-7), Max: 10621 (node-2)
--- PASS: TestDistributionUniformity (0.18s)
BenchmarkLookup-8       2145832     558.2 ns/op    168 B/op    5 allocs/op
PASS
```

## Design Decisions

**Decision 1: SHA-256 truncated to uint64.** Provides excellent uniform distribution for virtual node placement. FNV-1a is faster but clusters with structured input. The 64-bit ring space (1.8e19 positions) is sufficient for any practical ring.

**Decision 2: Sorted slice with binary search.** The ring is read-heavy (lookups) and rarely mutated (node add/remove). A sorted slice gives cache-friendly O(log N) lookups. Re-sorting on insertion is O(N) but acceptable since membership changes are infrequent.

**Decision 3: Per-node configurable virtual node count.** Enables weighted consistent hashing: a node with 2x the capacity gets 2x the virtual nodes and absorbs roughly 2x the keys.

## Common Mistakes

**Mistake 1: Forgetting ring wrap-around.** When binary search returns `idx == len(vnodes)`, wrap to index 0. Missing this leaves keys with high hash values unassigned.

**Mistake 2: Not deduplicating physical nodes during replica collection.** Walking clockwise must skip vnodes belonging to already-collected physical nodes, or all replicas might land on the same server.

**Mistake 3: Low-entropy virtual node hashing.** Using `hash(nodeID) + i` instead of `hash(nodeID + "#" + i)` clusters virtual nodes near one ring position. Each virtual node must be independently hashed.

## Performance Notes

- Lookup is O(log V*N) where V is average virtual nodes per node and N is physical node count. For 20 nodes with 200 vnodes each (4000 total), this is ~12 comparisons.
- Node addition is O(V*log(V*N)) for inserting V virtual nodes into the sorted slice, plus O(K) for migration computation.
- Memory: each vnode is ~40 bytes (uint64 + string + int). 20 nodes * 200 vnodes = 4000 vnodes = ~160KB. The key registry dominates memory at scale.
- The `sync.RWMutex` allows concurrent reads. Under high read load, consider sharding the ring into segments with per-segment locks, or using `sync.Map` for the key registry.
