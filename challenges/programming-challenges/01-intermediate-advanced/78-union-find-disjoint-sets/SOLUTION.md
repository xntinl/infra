# Solution: Union-Find (Disjoint Sets)

## Architecture Overview

The solution has two layers:

1. **UnionFind Core**: A flat array-based structure with `parent`, `rank`, and `size` arrays. `Find` uses path compression (every node on the path points directly to root). `Union` uses union by rank (shorter tree attaches under taller tree's root). Together these achieve near-O(1) amortized operations (inverse Ackermann).

2. **Applications**: Three algorithms built on UnionFind:
   - **Kruskal's MST**: Sort edges by weight, greedily add edges connecting different components.
   - **Dynamic Connectivity**: Process edge additions and track component count after each operation.
   - **Cycle Detection**: Before unioning, check if endpoints share a root -- if so, the edge creates a cycle.

## Go Solution

```go
// unionfind.go
package unionfind

import "sort"

type UnionFind struct {
	parent []int
	rank   []int
	size   []int
	count  int
}

func New(n int) *UnionFind {
	parent := make([]int, n)
	rank := make([]int, n)
	size := make([]int, n)
	for i := 0; i < n; i++ {
		parent[i] = i
		size[i] = 1
	}
	return &UnionFind{parent: parent, rank: rank, size: size, count: n}
}

func (uf *UnionFind) Find(x int) int {
	if uf.parent[x] != x {
		uf.parent[x] = uf.Find(uf.parent[x])
	}
	return uf.parent[x]
}

func (uf *UnionFind) Union(x, y int) bool {
	rx, ry := uf.Find(x), uf.Find(y)
	if rx == ry {
		return false
	}
	if uf.rank[rx] < uf.rank[ry] {
		rx, ry = ry, rx
	}
	uf.parent[ry] = rx
	uf.size[rx] += uf.size[ry]
	if uf.rank[rx] == uf.rank[ry] {
		uf.rank[rx]++
	}
	uf.count--
	return true
}

func (uf *UnionFind) Connected(x, y int) bool {
	return uf.Find(x) == uf.Find(y)
}

func (uf *UnionFind) ComponentCount() int {
	return uf.count
}

func (uf *UnionFind) ComponentSize(x int) int {
	return uf.size[uf.Find(x)]
}

// WeightedEdge represents an edge with a weight.
type WeightedEdge struct {
	U, V   int
	Weight float64
}

// Kruskal computes the minimum spanning tree/forest.
// Returns MST edges and total weight.
func Kruskal(numNodes int, edges []WeightedEdge) ([]WeightedEdge, float64) {
	sorted := make([]WeightedEdge, len(edges))
	copy(sorted, edges)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Weight < sorted[j].Weight
	})

	uf := New(numNodes)
	var mst []WeightedEdge
	totalWeight := 0.0

	for _, edge := range sorted {
		if uf.Union(edge.U, edge.V) {
			mst = append(mst, edge)
			totalWeight += edge.Weight
			if len(mst) == numNodes-1 {
				break
			}
		}
	}
	return mst, totalWeight
}

// DynamicConnectivity tracks component counts as edges are added.
// Returns the component count after each edge addition.
func DynamicConnectivity(numNodes int, edges []WeightedEdge) []int {
	uf := New(numNodes)
	counts := make([]int, len(edges))

	for i, edge := range edges {
		uf.Union(edge.U, edge.V)
		counts[i] = uf.ComponentCount()
	}
	return counts
}

// DetectCycle returns the first edge that creates a cycle, or nil if acyclic.
func DetectCycle(numNodes int, edges []WeightedEdge) *WeightedEdge {
	uf := New(numNodes)
	for _, edge := range edges {
		if uf.Connected(edge.U, edge.V) {
			e := edge
			return &e
		}
		uf.Union(edge.U, edge.V)
	}
	return nil
}
```

```go
// unionfind_test.go
package unionfind

import (
	"testing"
)

func TestBasicUnionFind(t *testing.T) {
	uf := New(5)

	if uf.ComponentCount() != 5 {
		t.Errorf("expected 5 components, got %d", uf.ComponentCount())
	}

	if !uf.Union(0, 1) {
		t.Error("union of different sets should return true")
	}
	if uf.ComponentCount() != 4 {
		t.Errorf("expected 4 components, got %d", uf.ComponentCount())
	}

	if !uf.Connected(0, 1) {
		t.Error("0 and 1 should be connected")
	}
	if uf.Connected(0, 2) {
		t.Error("0 and 2 should not be connected")
	}
}

func TestUnionAlreadyConnected(t *testing.T) {
	uf := New(3)
	uf.Union(0, 1)

	if uf.Union(0, 1) {
		t.Error("union of same set should return false")
	}
	if uf.ComponentCount() != 2 {
		t.Errorf("count should still be 2, got %d", uf.ComponentCount())
	}
}

func TestComponentSize(t *testing.T) {
	uf := New(5)
	uf.Union(0, 1)
	uf.Union(1, 2)

	if uf.ComponentSize(0) != 3 {
		t.Errorf("expected size 3, got %d", uf.ComponentSize(0))
	}
	if uf.ComponentSize(3) != 1 {
		t.Errorf("expected size 1, got %d", uf.ComponentSize(3))
	}
}

func TestPathCompression(t *testing.T) {
	uf := New(10)
	// Create a chain: 0-1-2-3-4
	for i := 0; i < 4; i++ {
		uf.Union(i, i+1)
	}

	root := uf.Find(4)
	// After find with path compression, 4 should point directly to root
	if uf.parent[4] != root {
		t.Errorf("path compression failed: parent[4]=%d, root=%d", uf.parent[4], root)
	}
}

func TestTransitiveConnectivity(t *testing.T) {
	uf := New(6)
	uf.Union(0, 1)
	uf.Union(2, 3)
	uf.Union(1, 3) // connects {0,1} with {2,3}

	if !uf.Connected(0, 3) {
		t.Error("0 and 3 should be transitively connected")
	}
	if !uf.Connected(0, 2) {
		t.Error("0 and 2 should be transitively connected")
	}
	if uf.Connected(0, 4) {
		t.Error("0 and 4 should not be connected")
	}
}

func TestKruskalMST(t *testing.T) {
	//    0
	//   /|\
	//  4 8 1
	// /   |  \
	// 3---2---1
	//   7   6
	edges := []WeightedEdge{
		{0, 1, 4},
		{0, 2, 8},
		{1, 2, 6},
		{1, 3, 1},
		{2, 3, 7},
	}

	mst, totalWeight := Kruskal(4, edges)

	if totalWeight != 11 {
		t.Errorf("expected MST weight 11, got %f", totalWeight)
	}
	if len(mst) != 3 {
		t.Errorf("expected 3 MST edges, got %d", len(mst))
	}
}

func TestKruskalDisconnected(t *testing.T) {
	edges := []WeightedEdge{
		{0, 1, 1},
		{2, 3, 2},
	}

	mst, totalWeight := Kruskal(4, edges)

	if totalWeight != 3 {
		t.Errorf("expected weight 3, got %f", totalWeight)
	}
	if len(mst) != 2 {
		t.Errorf("expected 2 edges (forest), got %d", len(mst))
	}
}

func TestDynamicConnectivity(t *testing.T) {
	edges := []WeightedEdge{
		{0, 1, 0},
		{2, 3, 0},
		{1, 2, 0},
		{3, 4, 0},
	}

	counts := DynamicConnectivity(5, edges)
	expected := []int{4, 3, 2, 1}

	for i, c := range counts {
		if c != expected[i] {
			t.Errorf("after edge %d: expected %d components, got %d", i, expected[i], c)
		}
	}
}

func TestDynamicConnectivityRedundant(t *testing.T) {
	edges := []WeightedEdge{
		{0, 1, 0},
		{0, 1, 0}, // redundant
		{2, 3, 0},
	}

	counts := DynamicConnectivity(4, edges)
	// 4 -> 3 (union 0,1) -> 3 (already connected) -> 2 (union 2,3)
	expected := []int{3, 3, 2}

	for i, c := range counts {
		if c != expected[i] {
			t.Errorf("after edge %d: expected %d, got %d", i, expected[i], c)
		}
	}
}

func TestDetectCycle(t *testing.T) {
	edges := []WeightedEdge{
		{0, 1, 0},
		{1, 2, 0},
		{2, 0, 0}, // creates cycle
		{3, 4, 0},
	}

	cycleEdge := DetectCycle(5, edges)
	if cycleEdge == nil {
		t.Fatal("expected cycle edge, got nil")
	}
	if cycleEdge.U != 2 || cycleEdge.V != 0 {
		t.Errorf("expected edge (2,0), got (%d,%d)", cycleEdge.U, cycleEdge.V)
	}
}

func TestDetectNoCycle(t *testing.T) {
	edges := []WeightedEdge{
		{0, 1, 0},
		{1, 2, 0},
		{3, 4, 0},
	}

	cycleEdge := DetectCycle(5, edges)
	if cycleEdge != nil {
		t.Errorf("expected no cycle, got edge (%d,%d)", cycleEdge.U, cycleEdge.V)
	}
}

func TestSingleElement(t *testing.T) {
	uf := New(1)
	if uf.ComponentCount() != 1 {
		t.Errorf("expected 1 component, got %d", uf.ComponentCount())
	}
	if uf.ComponentSize(0) != 1 {
		t.Errorf("expected size 1, got %d", uf.ComponentSize(0))
	}
	if uf.Find(0) != 0 {
		t.Errorf("expected root 0, got %d", uf.Find(0))
	}
}

func TestTwoElements(t *testing.T) {
	uf := New(2)
	if uf.Connected(0, 1) {
		t.Error("0 and 1 should not be connected initially")
	}
	uf.Union(0, 1)
	if !uf.Connected(0, 1) {
		t.Error("0 and 1 should be connected after union")
	}
	if uf.ComponentCount() != 1 {
		t.Errorf("expected 1 component, got %d", uf.ComponentCount())
	}
}
```

## Running the Go Solution

```bash
mkdir -p unionfind && cd unionfind
go mod init unionfind
# Place unionfind.go and unionfind_test.go in the directory
go test -v -count=1 ./...
```

### Expected Output

```
=== RUN   TestBasicUnionFind
--- PASS: TestBasicUnionFind
=== RUN   TestUnionAlreadyConnected
--- PASS: TestUnionAlreadyConnected
=== RUN   TestComponentSize
--- PASS: TestComponentSize
=== RUN   TestPathCompression
--- PASS: TestPathCompression
=== RUN   TestTransitiveConnectivity
--- PASS: TestTransitiveConnectivity
=== RUN   TestKruskalMST
--- PASS: TestKruskalMST
=== RUN   TestKruskalDisconnected
--- PASS: TestKruskalDisconnected
=== RUN   TestDynamicConnectivity
--- PASS: TestDynamicConnectivity
=== RUN   TestDynamicConnectivityRedundant
--- PASS: TestDynamicConnectivityRedundant
=== RUN   TestDetectCycle
--- PASS: TestDetectCycle
=== RUN   TestDetectNoCycle
--- PASS: TestDetectNoCycle
=== RUN   TestSingleElement
--- PASS: TestSingleElement
=== RUN   TestTwoElements
--- PASS: TestTwoElements
PASS
```

## Rust Solution

```rust
// src/lib.rs

pub struct UnionFind {
    parent: Vec<usize>,
    rank: Vec<usize>,
    size: Vec<usize>,
    count: usize,
}

impl UnionFind {
    pub fn new(n: usize) -> Self {
        let parent: Vec<usize> = (0..n).collect();
        let rank = vec![0; n];
        let size = vec![1; n];
        UnionFind { parent, rank, size, count: n }
    }

    pub fn find(&mut self, x: usize) -> usize {
        if self.parent[x] != x {
            self.parent[x] = self.find(self.parent[x]);
        }
        self.parent[x]
    }

    pub fn union(&mut self, x: usize, y: usize) -> bool {
        let mut rx = self.find(x);
        let mut ry = self.find(y);
        if rx == ry {
            return false;
        }
        if self.rank[rx] < self.rank[ry] {
            std::mem::swap(&mut rx, &mut ry);
        }
        self.parent[ry] = rx;
        self.size[rx] += self.size[ry];
        if self.rank[rx] == self.rank[ry] {
            self.rank[rx] += 1;
        }
        self.count -= 1;
        true
    }

    pub fn connected(&mut self, x: usize, y: usize) -> bool {
        self.find(x) == self.find(y)
    }

    pub fn component_count(&self) -> usize {
        self.count
    }

    pub fn component_size(&mut self, x: usize) -> usize {
        let root = self.find(x);
        self.size[root]
    }
}

#[derive(Debug, Clone)]
pub struct WeightedEdge {
    pub u: usize,
    pub v: usize,
    pub weight: f64,
}

/// Kruskal's MST: returns (mst_edges, total_weight).
pub fn kruskal(num_nodes: usize, edges: &[WeightedEdge]) -> (Vec<WeightedEdge>, f64) {
    let mut sorted: Vec<WeightedEdge> = edges.to_vec();
    sorted.sort_by(|a, b| a.weight.partial_cmp(&b.weight).unwrap());

    let mut uf = UnionFind::new(num_nodes);
    let mut mst = Vec::new();
    let mut total_weight = 0.0;

    for edge in sorted {
        if uf.union(edge.u, edge.v) {
            total_weight += edge.weight;
            mst.push(edge);
            if mst.len() == num_nodes - 1 {
                break;
            }
        }
    }
    (mst, total_weight)
}

/// Tracks component count after each edge addition.
pub fn dynamic_connectivity(num_nodes: usize, edges: &[WeightedEdge]) -> Vec<usize> {
    let mut uf = UnionFind::new(num_nodes);
    edges
        .iter()
        .map(|edge| {
            uf.union(edge.u, edge.v);
            uf.component_count()
        })
        .collect()
}

/// Returns the first edge that creates a cycle, or None.
pub fn detect_cycle(num_nodes: usize, edges: &[WeightedEdge]) -> Option<WeightedEdge> {
    let mut uf = UnionFind::new(num_nodes);
    for edge in edges {
        if uf.connected(edge.u, edge.v) {
            return Some(edge.clone());
        }
        uf.union(edge.u, edge.v);
    }
    None
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn basic_union_find() {
        let mut uf = UnionFind::new(5);
        assert_eq!(uf.component_count(), 5);
        assert!(uf.union(0, 1));
        assert_eq!(uf.component_count(), 4);
        assert!(uf.connected(0, 1));
        assert!(!uf.connected(0, 2));
    }

    #[test]
    fn union_already_connected() {
        let mut uf = UnionFind::new(3);
        uf.union(0, 1);
        assert!(!uf.union(0, 1));
        assert_eq!(uf.component_count(), 2);
    }

    #[test]
    fn component_size() {
        let mut uf = UnionFind::new(5);
        uf.union(0, 1);
        uf.union(1, 2);
        assert_eq!(uf.component_size(0), 3);
        assert_eq!(uf.component_size(3), 1);
    }

    #[test]
    fn path_compression() {
        let mut uf = UnionFind::new(10);
        for i in 0..4 {
            uf.union(i, i + 1);
        }
        let root = uf.find(4);
        assert_eq!(uf.parent[4], root);
    }

    #[test]
    fn transitive_connectivity() {
        let mut uf = UnionFind::new(6);
        uf.union(0, 1);
        uf.union(2, 3);
        uf.union(1, 3);
        assert!(uf.connected(0, 3));
        assert!(uf.connected(0, 2));
        assert!(!uf.connected(0, 4));
    }

    #[test]
    fn kruskal_mst() {
        let edges = vec![
            WeightedEdge { u: 0, v: 1, weight: 4.0 },
            WeightedEdge { u: 0, v: 2, weight: 8.0 },
            WeightedEdge { u: 1, v: 2, weight: 6.0 },
            WeightedEdge { u: 1, v: 3, weight: 1.0 },
            WeightedEdge { u: 2, v: 3, weight: 7.0 },
        ];

        let (mst, total) = kruskal(4, &edges);
        assert_eq!(total, 11.0);
        assert_eq!(mst.len(), 3);
    }

    #[test]
    fn kruskal_disconnected() {
        let edges = vec![
            WeightedEdge { u: 0, v: 1, weight: 1.0 },
            WeightedEdge { u: 2, v: 3, weight: 2.0 },
        ];

        let (mst, total) = kruskal(4, &edges);
        assert_eq!(total, 3.0);
        assert_eq!(mst.len(), 2);
    }

    #[test]
    fn dynamic_connectivity_tracking() {
        let edges = vec![
            WeightedEdge { u: 0, v: 1, weight: 0.0 },
            WeightedEdge { u: 2, v: 3, weight: 0.0 },
            WeightedEdge { u: 1, v: 2, weight: 0.0 },
            WeightedEdge { u: 3, v: 4, weight: 0.0 },
        ];

        let counts = dynamic_connectivity(5, &edges);
        assert_eq!(counts, vec![4, 3, 2, 1]);
    }

    #[test]
    fn dynamic_connectivity_redundant() {
        let edges = vec![
            WeightedEdge { u: 0, v: 1, weight: 0.0 },
            WeightedEdge { u: 0, v: 1, weight: 0.0 },
            WeightedEdge { u: 2, v: 3, weight: 0.0 },
        ];

        let counts = dynamic_connectivity(4, &edges);
        assert_eq!(counts, vec![3, 3, 2]);
    }

    #[test]
    fn cycle_detection() {
        let edges = vec![
            WeightedEdge { u: 0, v: 1, weight: 0.0 },
            WeightedEdge { u: 1, v: 2, weight: 0.0 },
            WeightedEdge { u: 2, v: 0, weight: 0.0 },
            WeightedEdge { u: 3, v: 4, weight: 0.0 },
        ];

        let cycle_edge = detect_cycle(5, &edges);
        assert!(cycle_edge.is_some());
        let e = cycle_edge.unwrap();
        assert_eq!(e.u, 2);
        assert_eq!(e.v, 0);
    }

    #[test]
    fn no_cycle() {
        let edges = vec![
            WeightedEdge { u: 0, v: 1, weight: 0.0 },
            WeightedEdge { u: 1, v: 2, weight: 0.0 },
            WeightedEdge { u: 3, v: 4, weight: 0.0 },
        ];

        assert!(detect_cycle(5, &edges).is_none());
    }

    #[test]
    fn single_element() {
        let mut uf = UnionFind::new(1);
        assert_eq!(uf.component_count(), 1);
        assert_eq!(uf.component_size(0), 1);
        assert_eq!(uf.find(0), 0);
    }

    #[test]
    fn two_elements() {
        let mut uf = UnionFind::new(2);
        assert!(!uf.connected(0, 1));
        uf.union(0, 1);
        assert!(uf.connected(0, 1));
        assert_eq!(uf.component_count(), 1);
    }
}
```

## Running the Rust Solution

```bash
cargo new union_find --lib && cd union_find
# Replace src/lib.rs with the code above
cargo test -- --nocapture
```

### Expected Output

```
running 13 tests
test tests::basic_union_find ... ok
test tests::union_already_connected ... ok
test tests::component_size ... ok
test tests::path_compression ... ok
test tests::transitive_connectivity ... ok
test tests::kruskal_mst ... ok
test tests::kruskal_disconnected ... ok
test tests::dynamic_connectivity_tracking ... ok
test tests::dynamic_connectivity_redundant ... ok
test tests::cycle_detection ... ok
test tests::no_cycle ... ok
test tests::single_element ... ok
test tests::two_elements ... ok

test result: ok. 13 passed; 0 failed; 0 ignored
```

## Design Decisions

1. **Array-based over pointer-based**: Union-Find uses flat arrays (`parent[]`, `rank[]`, `size[]`) rather than node objects. This gives excellent cache locality and trivial memory management. Elements are indexed by integer, which maps naturally to graph node IDs.

2. **Recursive path compression**: The recursive `Find` is cleaner than the two-pass iterative version and compresses the full path in one call. The recursion depth is bounded by O(log n) even without compression (due to union by rank), so stack overflow is not a practical concern.

3. **Union by rank over union by size**: Rank tracks tree height upper bounds, while size tracks element counts. Both achieve the same asymptotic bound, but rank is the classic choice and slightly more space-efficient (rank never exceeds O(log n) while size can be up to n).

4. **Bool return from Union**: Returning whether the union actually merged two sets is useful for Kruskal's (count MST edges) and cycle detection (if union returns false, a cycle exists). This avoids a separate `Connected` check before each union.

5. **Kruskal stops at V-1 edges**: Once the MST has V-1 edges, all remaining edges are redundant. Early termination avoids processing the rest of the edge list.

## Common Mistakes

- **Forgetting path compression**: Without path compression, Find degrades to O(log n) per call even with union by rank. With compression, amortized cost drops to alpha(n) (inverse Ackermann, practically constant).
- **Using union by rank alone without compression**: Union by rank gives O(log n) but not the near-O(1) bound. Both optimizations together are needed for the inverse Ackermann amortized complexity.
- **Updating rank on every union**: Rank should only increase when two trees of equal rank merge. Incrementing on every merge overestimates height and reduces balancing effectiveness.
- **Confusing directed and undirected cycle detection**: Union-Find cycle detection works for undirected graphs only. For directed graphs, an edge (u, v) where Find(u) == Find(v) does not necessarily mean a directed cycle -- use DFS-based detection instead.
- **Not handling self-loops**: An edge (u, u) always has Find(u) == Find(u), so it always reports a "cycle." This is correct for undirected graphs (self-loop is a cycle) but may need special handling depending on the application.

## Performance Notes

| Operation | Amortized Time | Notes |
|-----------|---------------|-------|
| Find (with path compression + union by rank) | O(alpha(n)) | alpha is inverse Ackermann, practically <= 4 for n < 10^80 |
| Union | O(alpha(n)) | Dominated by two Find calls |
| Connected | O(alpha(n)) | Two Find calls |
| ComponentCount | O(1) | Maintained incrementally |
| ComponentSize | O(alpha(n)) | One Find call + array lookup |
| Kruskal's MST | O(E log E) | Dominated by edge sorting; UF operations are near-O(1) |

The Union-Find structure is one of the most efficient data structures in practice. For 10 million elements, a sequence of 10 million Find/Union operations completes in roughly the same time as 10 million array accesses. The inverse Ackermann function grows so slowly that it is effectively constant for any conceivable input size.
