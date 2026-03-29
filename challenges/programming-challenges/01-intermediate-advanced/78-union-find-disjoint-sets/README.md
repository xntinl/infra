# 78. Union-Find (Disjoint Sets)

```yaml
difficulty: intermediate-advanced
languages: [go, rust]
time_estimate: 4-6 hours
tags: [union-find, disjoint-sets, path-compression, union-by-rank, kruskal, connected-components]
bloom_level: [apply, analyze, evaluate]
```

## Prerequisites

- Array/vector-based tree representations (parent array)
- Amortized complexity analysis: understanding inverse Ackermann function
- Generic programming: Go generics, Rust generics with trait bounds
- Graph fundamentals: edges, connectivity, spanning trees
- Sorting algorithms (for Kruskal's MST application)

## Learning Objectives

After completing this challenge you will be able to:

- **Implement** Union-Find with path compression and union by rank for near-O(1) amortized operations
- **Analyze** why path compression and union by rank together achieve inverse Ackermann amortized time
- **Apply** Union-Find to Kruskal's minimum spanning tree algorithm
- **Apply** Union-Find to dynamic connectivity (connected component tracking as edges are added)
- **Detect** cycles in undirected graphs using Union-Find as an alternative to DFS

## The Challenge

When edges arrive one at a time in a dynamic graph, you need to answer: "Are these two nodes already connected?" and "How many connected components remain?" DFS-based approaches recompute from scratch on each query. Union-Find maintains disjoint sets that can be merged and queried in near-constant amortized time, making it the backbone of Kruskal's MST, network connectivity monitoring, and percolation simulations.

Implement a Union-Find data structure with path compression and union by rank. Support find (with path compression), union (by rank), connectivity check, component count, and component size. Then apply it to three problems: Kruskal's minimum spanning tree, dynamic connected component tracking, and cycle detection in undirected graphs.

## Requirements

1. Define a `UnionFind` structure that stores parent and rank arrays. Initialize each element as its own parent with rank 0. Support creation with a known number of elements or dynamic growth.

2. Implement `Find(x) -> root` with path compression: during the find, make every node along the path point directly to the root. This flattens the tree for future operations.

3. Implement `Union(x, y) -> bool` with union by rank: attach the shorter tree under the taller tree's root. Return true if x and y were in different sets (actual merge happened), false if already connected.

4. Implement `Connected(x, y) -> bool` that checks whether two elements share the same root. Implement `ComponentCount() -> int` that returns the number of disjoint sets. Implement `ComponentSize(x) -> int` that returns the size of the component containing x.

5. Implement Kruskal's MST: given a list of weighted edges, sort by weight and use Union-Find to greedily add edges that connect different components. Return the MST edges and total weight.

6. Implement dynamic connectivity tracking: process a stream of `AddEdge(u, v)` operations and after each one report the current component count. Demonstrate that Union-Find handles this in near-O(1) per operation.

7. Implement cycle detection for undirected graphs: for each edge `(u, v)`, if `Find(u) == Find(v)`, the edge creates a cycle. Return the first cycle-creating edge found.

8. Implement both Go and Rust versions with idiomatic patterns for each language.

## Hints

<details>
<summary>Hint 1: Parent and rank arrays</summary>

Use a flat array where `parent[i]` stores the parent of element `i`. Initially `parent[i] = i`. Rank tracks tree height upper bounds for balancing.

```go
type UnionFind struct {
    parent []int
    rank   []int
    size   []int
    count  int // number of components
}

func NewUnionFind(n int) *UnionFind {
    parent := make([]int, n)
    rank := make([]int, n)
    size := make([]int, n)
    for i := 0; i < n; i++ {
        parent[i] = i
        size[i] = 1
    }
    return &UnionFind{parent: parent, rank: rank, size: size, count: n}
}
```

</details>

<details>
<summary>Hint 2: Path compression in Find</summary>

Path compression makes every node on the find path point directly to the root. The iterative approach uses two passes: one to find the root, another to update pointers. The recursive approach is more concise.

```rust
fn find(&mut self, x: usize) -> usize {
    if self.parent[x] != x {
        self.parent[x] = self.find(self.parent[x]); // path compression
    }
    self.parent[x]
}
```

</details>

<details>
<summary>Hint 3: Union by rank</summary>

Always attach the shorter tree under the root of the taller tree. Only increment rank when both trees have equal rank (because the merged tree is now one level taller).

```go
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
```

</details>

<details>
<summary>Hint 4: Kruskal's MST</summary>

Sort all edges by weight. Iterate through edges: for each edge `(u, v, w)`, if `Find(u) != Find(v)`, add the edge to the MST and `Union(u, v)`. Stop when MST has `V-1` edges.

```rust
fn kruskal(num_nodes: usize, mut edges: Vec<(usize, usize, f64)>) -> (Vec<(usize, usize, f64)>, f64) {
    edges.sort_by(|a, b| a.2.partial_cmp(&b.2).unwrap());
    let mut uf = UnionFind::new(num_nodes);
    let mut mst = Vec::new();
    let mut total = 0.0;
    for (u, v, w) in edges {
        if uf.union(u, v) {
            mst.push((u, v, w));
            total += w;
        }
    }
    (mst, total)
}
```

</details>

<details>
<summary>Hint 5: Cycle detection</summary>

Process edges sequentially. Before unioning, check if both endpoints share a root. If they do, adding this edge would create a cycle. Return that edge as the cycle-causing edge.

</details>

## Acceptance Criteria

- [ ] Find with path compression flattens the tree (subsequent finds are O(1))
- [ ] Union by rank keeps trees balanced (height is O(log n) without compression)
- [ ] `Connected` correctly reports whether two elements share a component
- [ ] `ComponentCount` decreases by one on each successful union
- [ ] `ComponentSize` returns correct sizes after unions
- [ ] Kruskal's MST produces a valid minimum spanning tree with correct total weight
- [ ] Kruskal's handles disconnected graphs (returns forest, not tree)
- [ ] Dynamic connectivity correctly tracks component count after each edge addition
- [ ] Cycle detection identifies the first edge that creates a cycle
- [ ] Cycle detection reports no cycle for acyclic graphs (trees/forests)
- [ ] Both Go and Rust implementations pass equivalent test suites
- [ ] Single-element and two-element edge cases handled correctly

## Resources

- [Disjoint-Set Data Structure - Wikipedia](https://en.wikipedia.org/wiki/Disjoint-set_data_structure)
- [Kruskal's Algorithm - Wikipedia](https://en.wikipedia.org/wiki/Kruskal%27s_algorithm)
- [CLRS Chapter 21: Data Structures for Disjoint Sets](https://mitpress.mit.edu/books/introduction-to-algorithms-fourth-edition) - Path compression, union by rank analysis
- [Union-Find Complexity: Inverse Ackermann](https://en.wikipedia.org/wiki/Ackermann_function#Inverse) - Why amortized is nearly O(1)
- [Go Generics Tutorial](https://go.dev/doc/tutorial/generics)
- [The Rust Programming Language: Ownership](https://doc.rust-lang.org/book/ch04-00-understanding-ownership.html) - Mutable borrow patterns for Union-Find
