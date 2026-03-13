# 38. CP: Minimum Spanning Tree

## Difficulty: Avanzado

## Introduction

A minimum spanning tree (MST) of a connected, weighted, undirected graph is a subset of edges that connects all vertices with the minimum possible total edge weight, forming a tree (no cycles). MST algorithms are fundamental in competitive programming: they appear directly in network design problems and serve as subroutines in approximation algorithms for NP-hard problems like the Traveling Salesman.

There are three classical MST algorithms. Kruskal's algorithm sorts edges by weight and greedily adds the cheapest edge that does not create a cycle, using a Union-Find (Disjoint Set Union) data structure for cycle detection. Prim's algorithm grows the tree from a starting vertex, always adding the cheapest edge that connects the tree to a new vertex, using a priority queue. Boruvka's algorithm works in rounds, where each connected component finds its cheapest outgoing edge, and all such edges are added simultaneously. This exercise covers all three in Rust, along with four competition problems.

---

## Union-Find (Disjoint Set Union)

Union-Find is the backbone of Kruskal's algorithm. It supports two operations: `find` (which component does this element belong to?) and `union` (merge two components). With path compression and union by rank, both operations run in amortized O(alpha(n)) time, where alpha is the inverse Ackermann function -- effectively constant.

```rust
struct UnionFind {
    parent: Vec<usize>,
    rank: Vec<usize>,
    components: usize,
}

impl UnionFind {
    fn new(n: usize) -> Self {
        Self {
            parent: (0..n).collect(),
            rank: vec![0; n],
            components: n,
        }
    }

    fn find(&mut self, x: usize) -> usize {
        if self.parent[x] != x {
            self.parent[x] = self.find(self.parent[x]); // path compression
        }
        self.parent[x]
    }

    /// Returns true if x and y were in different components (union performed).
    fn union(&mut self, x: usize, y: usize) -> bool {
        let rx = self.find(x);
        let ry = self.find(y);
        if rx == ry {
            return false; // already connected
        }
        // Union by rank: attach the shorter tree under the taller one
        match self.rank[rx].cmp(&self.rank[ry]) {
            std::cmp::Ordering::Less => self.parent[rx] = ry,
            std::cmp::Ordering::Greater => self.parent[ry] = rx,
            std::cmp::Ordering::Equal => {
                self.parent[ry] = rx;
                self.rank[rx] += 1;
            }
        }
        self.components -= 1;
        true
    }

    fn connected(&mut self, x: usize, y: usize) -> bool {
        self.find(x) == self.find(y)
    }
}

fn main() {
    let mut uf = UnionFind::new(6);
    assert_eq!(uf.components, 6);

    uf.union(0, 1);
    uf.union(2, 3);
    uf.union(0, 3);
    assert_eq!(uf.components, 3); // {0,1,2,3}, {4}, {5}

    assert!(uf.connected(0, 2));
    assert!(uf.connected(1, 3));
    assert!(!uf.connected(0, 4));

    println!("UnionFind works correctly.");
}
```

---

## Kruskal's Algorithm

Kruskal's algorithm is the most common MST approach in competitive programming because it is simple, works on edge lists (no adjacency list needed), and pairs naturally with Union-Find.

### Algorithm

1. Sort all edges by weight (ascending).
2. Initialize a Union-Find with `n` vertices.
3. For each edge in sorted order, if the two endpoints are in different components, add the edge to the MST and union the components.
4. Stop when the MST has `n - 1` edges (all vertices are connected).

```rust
fn kruskal(n: usize, edges: &mut [(usize, usize, i64)]) -> (i64, Vec<(usize, usize, i64)>) {
    // Sort edges by weight
    edges.sort_by_key(|&(_, _, w)| w);

    let mut uf = UnionFind::new(n);
    let mut mst_weight = 0i64;
    let mut mst_edges = Vec::with_capacity(n - 1);

    for &(u, v, w) in edges.iter() {
        if uf.union(u, v) {
            mst_weight += w;
            mst_edges.push((u, v, w));
            if mst_edges.len() == n - 1 {
                break; // MST is complete
            }
        }
    }

    (mst_weight, mst_edges)
}

struct UnionFind {
    parent: Vec<usize>,
    rank: Vec<usize>,
    components: usize,
}

impl UnionFind {
    fn new(n: usize) -> Self {
        Self {
            parent: (0..n).collect(),
            rank: vec![0; n],
            components: n,
        }
    }

    fn find(&mut self, x: usize) -> usize {
        if self.parent[x] != x {
            self.parent[x] = self.find(self.parent[x]);
        }
        self.parent[x]
    }

    fn union(&mut self, x: usize, y: usize) -> bool {
        let rx = self.find(x);
        let ry = self.find(y);
        if rx == ry {
            return false;
        }
        match self.rank[rx].cmp(&self.rank[ry]) {
            std::cmp::Ordering::Less => self.parent[rx] = ry,
            std::cmp::Ordering::Greater => self.parent[ry] = rx,
            std::cmp::Ordering::Equal => {
                self.parent[ry] = rx;
                self.rank[rx] += 1;
            }
        }
        self.components -= 1;
        true
    }
}

fn main() {
    //     1
    //  0-----1
    //  |  \  |
    // 4|  2\ |3
    //  |    \|
    //  3-----2
    //     5
    let mut edges = vec![
        (0, 1, 1),
        (0, 2, 2),
        (0, 3, 4),
        (1, 2, 3),
        (2, 3, 5),
    ];

    let (weight, mst) = kruskal(4, &mut edges);
    println!("MST weight: {weight}");
    println!("MST edges: {mst:?}");
    assert_eq!(weight, 7); // edges (0,1,1), (0,2,2), (0,3,4)
    assert_eq!(mst.len(), 3);
}
```

**Complexity**: O(E log E) for sorting, plus O(E * alpha(V)) for the Union-Find operations. Total: O(E log E).

---

## Prim's Algorithm

Prim's algorithm is a greedy algorithm that grows the MST from a starting vertex. At each step, it adds the cheapest edge connecting a vertex already in the MST to a vertex not yet in the MST. A binary heap efficiently selects the minimum-weight edge.

```rust
use std::collections::BinaryHeap;
use std::cmp::Reverse;

fn prim(adj: &[Vec<(usize, i64)>]) -> (i64, Vec<(usize, usize, i64)>) {
    let n = adj.len();
    if n == 0 {
        return (0, vec![]);
    }

    let mut in_mst = vec![false; n];
    let mut mst_weight = 0i64;
    let mut mst_edges = Vec::with_capacity(n - 1);

    // Min-heap of (weight, target_vertex, source_vertex)
    // Reverse turns BinaryHeap into a min-heap
    let mut heap: BinaryHeap<Reverse<(i64, usize, usize)>> = BinaryHeap::new();

    // Start from vertex 0
    in_mst[0] = true;
    for &(v, w) in &adj[0] {
        heap.push(Reverse((w, v, 0)));
    }

    while let Some(Reverse((w, u, from))) = heap.pop() {
        if in_mst[u] {
            continue; // already in the MST
        }
        in_mst[u] = true;
        mst_weight += w;
        mst_edges.push((from, u, w));

        // Add all edges from the newly added vertex
        for &(v, edge_w) in &adj[u] {
            if !in_mst[v] {
                heap.push(Reverse((edge_w, v, u)));
            }
        }
    }

    (mst_weight, mst_edges)
}

fn main() {
    // Same graph as Kruskal example
    let adj = vec![
        vec![(1, 1), (2, 2), (3, 4)], // vertex 0
        vec![(0, 1), (2, 3)],          // vertex 1
        vec![(0, 2), (1, 3), (3, 5)],  // vertex 2
        vec![(0, 4), (2, 5)],          // vertex 3
    ];

    let (weight, mst) = prim(&adj);
    println!("Prim MST weight: {weight}");
    println!("Prim MST edges: {mst:?}");
    assert_eq!(weight, 7);
    assert_eq!(mst.len(), 3);
}
```

**Note on `Reverse` in Rust**: Rust's `BinaryHeap` is a max-heap. Wrapping the priority in `std::cmp::Reverse` creates a min-heap. This is the standard idiom -- every competitive programming Rust solution uses `BinaryHeap<Reverse<T>>` for Dijkstra's, Prim's, and similar algorithms.

**Complexity**: O((V + E) log V) with a binary heap. Each vertex is extracted at most once, and each edge contributes at most one heap insertion.

---

## Boruvka's Algorithm

Boruvka's algorithm (also known as Boruvka-Sollin) works in rounds. In each round, every connected component finds its minimum-weight outgoing edge (an edge connecting it to a different component), and all such edges are added simultaneously. The algorithm terminates when there is only one component.

```rust
fn boruvka(n: usize, edges: &[(usize, usize, i64)]) -> (i64, Vec<(usize, usize, i64)>) {
    let mut uf = UnionFind::new(n);
    let mut mst_weight = 0i64;
    let mut mst_edges = Vec::new();

    loop {
        // For each component, find its minimum outgoing edge
        // cheapest[comp] = (weight, edge_index)
        let mut cheapest: Vec<Option<(i64, usize)>> = vec![None; n];

        for (idx, &(u, v, w)) in edges.iter().enumerate() {
            let cu = uf.find(u);
            let cv = uf.find(v);
            if cu == cv {
                continue; // same component, not an outgoing edge
            }

            // Update cheapest for component cu
            if cheapest[cu].is_none() || w < cheapest[cu].unwrap().0 {
                cheapest[cu] = Some((w, idx));
            }
            // Update cheapest for component cv
            if cheapest[cv].is_none() || w < cheapest[cv].unwrap().0 {
                cheapest[cv] = Some((w, idx));
            }
        }

        // Add all cheapest edges
        let mut added = 0;
        for comp in 0..n {
            if let Some((_, edge_idx)) = cheapest[comp] {
                let (u, v, w) = edges[edge_idx];
                if uf.union(u, v) {
                    mst_weight += w;
                    mst_edges.push((u, v, w));
                    added += 1;
                }
            }
        }

        if added == 0 {
            break; // no more edges to add -- MST complete (or graph disconnected)
        }
    }

    (mst_weight, mst_edges)
}

struct UnionFind {
    parent: Vec<usize>,
    rank: Vec<usize>,
    components: usize,
}

impl UnionFind {
    fn new(n: usize) -> Self {
        Self {
            parent: (0..n).collect(),
            rank: vec![0; n],
            components: n,
        }
    }

    fn find(&mut self, x: usize) -> usize {
        if self.parent[x] != x {
            self.parent[x] = self.find(self.parent[x]);
        }
        self.parent[x]
    }

    fn union(&mut self, x: usize, y: usize) -> bool {
        let rx = self.find(x);
        let ry = self.find(y);
        if rx == ry {
            return false;
        }
        match self.rank[rx].cmp(&self.rank[ry]) {
            std::cmp::Ordering::Less => self.parent[rx] = ry,
            std::cmp::Ordering::Greater => self.parent[ry] = rx,
            std::cmp::Ordering::Equal => {
                self.parent[ry] = rx;
                self.rank[rx] += 1;
            }
        }
        self.components -= 1;
        true
    }
}

fn main() {
    let edges = vec![
        (0, 1, 1),
        (0, 2, 2),
        (0, 3, 4),
        (1, 2, 3),
        (2, 3, 5),
    ];

    let (weight, mst) = boruvka(4, &edges);
    println!("Boruvka MST weight: {weight}");
    println!("Boruvka MST edges: {mst:?}");
    assert_eq!(weight, 7);
}
```

**When to use Boruvka's**: It is less common in standard competitive programming than Kruskal's or Prim's, but it is the algorithm of choice for parallel MST computation (each round is embarrassingly parallel) and for certain specialized problems where edges are defined implicitly.

**Complexity**: O(E log V) -- at most O(log V) rounds, each processing all E edges.

---

## Kruskal's vs Prim's vs Boruvka's

| Criterion | Kruskal's | Prim's | Boruvka's |
|-----------|-----------|--------|-----------|
| Data structure | Edge list + Union-Find | Adjacency list + BinaryHeap | Edge list + Union-Find |
| Complexity | O(E log E) | O((V+E) log V) | O(E log V) |
| Best for | Sparse graphs (E ~ V) | Dense graphs (E ~ V^2) | Parallel computation |
| Ease of implementation | Simple | Moderate | Moderate |
| Returns MST edges | Naturally | Naturally | Naturally |
| Works with implicit edges | No (needs sorted list) | Yes (just need neighbors) | Yes |

In practice for competitive programming, **Kruskal's is the default choice** because it is the simplest to implement and works well for most graph sizes encountered in contests.

---

## Sorting Edges in Rust

Kruskal's requires sorted edges. Here are the idiomatic Rust approaches:

```rust
fn main() {
    let mut edges: Vec<(usize, usize, i64)> = vec![
        (0, 1, 5),
        (1, 2, 1),
        (0, 2, 3),
        (2, 3, 2),
    ];

    // Method 1: sort_by_key (most readable)
    edges.sort_by_key(|&(_, _, w)| w);
    println!("Sorted by weight: {edges:?}");

    // Method 2: sort_unstable_by_key (faster, no allocation guarantee)
    edges.sort_unstable_by_key(|&(_, _, w)| w);

    // Method 3: if the edge struct is different, use sort_by
    edges.sort_by(|a, b| a.2.cmp(&b.2));

    // For reverse order (heaviest first):
    edges.sort_by_key(|&(_, _, w)| std::cmp::Reverse(w));
    println!("Sorted by weight (desc): {edges:?}");
}
```

**Performance note**: `sort_unstable_by_key` is generally faster than `sort_by_key` because it does not allocate extra memory for stability. Since edge order among equal weights does not affect MST correctness, unstable sort is preferred.

---

## Problem 1: Minimum Cost to Connect All Points

You are given `n` points in a 2D plane. The cost to connect point `i` and point `j` is the Manhattan distance `|xi - xj| + |yi - yj|`. Return the minimum cost to connect all points such that there is exactly one path between any two points.

**Constraints**: 1 <= n <= 1,000. -10^6 <= xi, yi <= 10^6.

**Key insight**: This is a direct MST problem. The graph is complete (every pair of points has an edge), with edge weight equal to the Manhattan distance. Since the graph has O(n^2) edges, either approach works, but Prim's is slightly more natural for complete graphs.

**Hints**:
- Generate all n*(n-1)/2 edges with their Manhattan distances
- Run Kruskal's or Prim's
- For n <= 1000, O(n^2 log n) is comfortable

<details>
<summary>Solution</summary>

```rust
use std::collections::BinaryHeap;
use std::cmp::Reverse;

/// Kruskal's approach: generate all edges, sort, and select
fn min_cost_kruskal(points: &[(i64, i64)]) -> i64 {
    let n = points.len();
    if n <= 1 {
        return 0;
    }

    // Generate all edges
    let mut edges: Vec<(i64, usize, usize)> = Vec::with_capacity(n * (n - 1) / 2);
    for i in 0..n {
        for j in i + 1..n {
            let dist = (points[i].0 - points[j].0).abs() + (points[i].1 - points[j].1).abs();
            edges.push((dist, i, j));
        }
    }

    edges.sort_unstable();

    let mut uf = UnionFind::new(n);
    let mut total = 0i64;
    let mut count = 0;

    for (w, u, v) in edges {
        if uf.union(u, v) {
            total += w;
            count += 1;
            if count == n - 1 {
                break;
            }
        }
    }

    total
}

/// Prim's approach: better constant factor for dense graphs
fn min_cost_prim(points: &[(i64, i64)]) -> i64 {
    let n = points.len();
    if n <= 1 {
        return 0;
    }

    let dist = |i: usize, j: usize| -> i64 {
        (points[i].0 - points[j].0).abs() + (points[i].1 - points[j].1).abs()
    };

    let mut in_mst = vec![false; n];
    let mut total = 0i64;

    // min_edge[v] = minimum weight edge connecting v to the current MST
    let mut min_edge = vec![i64::MAX; n];
    min_edge[0] = 0;

    // Min-heap of (distance, vertex)
    let mut heap: BinaryHeap<Reverse<(i64, usize)>> = BinaryHeap::new();
    heap.push(Reverse((0, 0)));

    while let Some(Reverse((w, u))) = heap.pop() {
        if in_mst[u] {
            continue;
        }
        in_mst[u] = true;
        total += w;

        for v in 0..n {
            if !in_mst[v] {
                let d = dist(u, v);
                if d < min_edge[v] {
                    min_edge[v] = d;
                    heap.push(Reverse((d, v)));
                }
            }
        }
    }

    total
}

struct UnionFind {
    parent: Vec<usize>,
    rank: Vec<usize>,
}

impl UnionFind {
    fn new(n: usize) -> Self {
        Self {
            parent: (0..n).collect(),
            rank: vec![0; n],
        }
    }

    fn find(&mut self, x: usize) -> usize {
        if self.parent[x] != x {
            self.parent[x] = self.find(self.parent[x]);
        }
        self.parent[x]
    }

    fn union(&mut self, x: usize, y: usize) -> bool {
        let rx = self.find(x);
        let ry = self.find(y);
        if rx == ry {
            return false;
        }
        match self.rank[rx].cmp(&self.rank[ry]) {
            std::cmp::Ordering::Less => self.parent[rx] = ry,
            std::cmp::Ordering::Greater => self.parent[ry] = rx,
            std::cmp::Ordering::Equal => {
                self.parent[ry] = rx;
                self.rank[rx] += 1;
            }
        }
        true
    }
}

fn main() {
    let points = vec![(0, 0), (2, 2), (3, 10), (5, 2), (7, 0)];
    // Expected MST cost: 20
    let result_k = min_cost_kruskal(&points);
    let result_p = min_cost_prim(&points);
    println!("Kruskal: {result_k}");
    println!("Prim:    {result_p}");
    assert_eq!(result_k, result_p);
    assert_eq!(result_k, 20);

    // Single point
    assert_eq!(min_cost_kruskal(&[(0, 0)]), 0);

    // Two points
    assert_eq!(min_cost_kruskal(&[(0, 0), (1, 1)]), 2);

    // Collinear points
    let points = vec![(0, 0), (1, 0), (3, 0), (6, 0)];
    assert_eq!(min_cost_kruskal(&points), 6); // 1 + 2 + 3

    // Square
    let points = vec![(0, 0), (0, 1), (1, 0), (1, 1)];
    assert_eq!(min_cost_kruskal(&points), 3); // three edges of weight 1

    println!("All min_cost_connect tests passed.");
}
```

**Complexity**: Kruskal's is O(n^2 log n) (sorting n^2 edges). Prim's with a binary heap is O(n^2 log n) as well, but with a better constant factor since it avoids materializing all edges. For complete graphs with n up to ~1000, both are fast.

</details>

---

## Problem 2: Second Minimum Spanning Tree

Given a connected, weighted, undirected graph, find the weight of the second minimum spanning tree. The second MST is the spanning tree with the smallest total weight that is strictly greater than the MST weight. If no such tree exists (all spanning trees have the same weight), return -1.

**Constraints**: 2 <= n <= 1,000. n-1 <= edges.len() <= n*(n-1)/2. All edge weights are positive.

**Key insight**: The second MST differs from the first MST by exactly one edge swap. For each non-MST edge (u, v, w), adding it to the MST creates a cycle. Remove the heaviest edge on the path from u to v in the MST, and you get a candidate spanning tree. The second MST is the minimum over all such candidates.

**Hints**:
- Build the MST using Kruskal's
- Build the MST as an adjacency list for traversal
- For each non-MST edge (u, v, w), find the maximum-weight edge on the MST path from u to v
- The second MST weight = MST weight - max_on_path + w
- Take the minimum such value that is strictly greater than MST weight
- For the path query, a simple DFS/BFS per query is O(V) and the total is O(E*V), acceptable for n <= 1000

<details>
<summary>Solution</summary>

```rust
fn second_mst(n: usize, edges: &[(usize, usize, i64)]) -> Option<i64> {
    // Step 1: Build MST with Kruskal's
    let mut sorted_edges: Vec<(i64, usize, usize, usize)> = edges
        .iter()
        .enumerate()
        .map(|(idx, &(u, v, w))| (w, u, v, idx))
        .collect();
    sorted_edges.sort_unstable();

    let mut uf = UnionFind::new(n);
    let mut mst_weight = 0i64;
    let mut in_mst = vec![false; edges.len()];
    let mut mst_adj: Vec<Vec<(usize, i64)>> = vec![vec![]; n];

    for &(w, u, v, idx) in &sorted_edges {
        if uf.union(u, v) {
            mst_weight += w;
            in_mst[idx] = true;
            mst_adj[u].push((v, w));
            mst_adj[v].push((u, w));
        }
    }

    // Step 2: For each non-MST edge, find the max edge on the MST path
    // Uses DFS from u to v in the MST tree
    fn max_on_path(
        adj: &[Vec<(usize, i64)>],
        start: usize,
        end: usize,
        n: usize,
    ) -> i64 {
        // DFS to find the path and track the maximum edge weight
        let mut visited = vec![false; n];
        let mut stack: Vec<(usize, i64)> = vec![(start, 0)]; // (node, max_weight_so_far)
        let mut parent_max: Vec<(usize, i64)> = vec![(usize::MAX, 0); n]; // (parent, edge_weight)
        visited[start] = true;

        while let Some((u, _)) = stack.pop() {
            if u == end {
                // Trace back the path and find the maximum edge weight
                let mut max_w = 0i64;
                let mut cur = end;
                while cur != start {
                    let (p, w) = parent_max[cur];
                    max_w = max_w.max(w);
                    cur = p;
                }
                return max_w;
            }
            for &(v, w) in &adj[u] {
                if !visited[v] {
                    visited[v] = true;
                    parent_max[v] = (u, w);
                    stack.push((v, 0));
                }
            }
        }

        0 // should not reach here if graph is connected
    }

    // Step 3: Try swapping each non-MST edge
    let mut second_weight = i64::MAX;

    for (idx, &(u, v, w)) in edges.iter().enumerate() {
        if in_mst[idx] {
            continue;
        }
        let max_w = max_on_path(&mst_adj, u, v, n);
        let candidate = mst_weight - max_w + w;
        if candidate > mst_weight && candidate < second_weight {
            second_weight = candidate;
        }
    }

    if second_weight == i64::MAX {
        None // no second MST exists (all spanning trees have the same weight)
    } else {
        Some(second_weight)
    }
}

struct UnionFind {
    parent: Vec<usize>,
    rank: Vec<usize>,
}

impl UnionFind {
    fn new(n: usize) -> Self {
        Self {
            parent: (0..n).collect(),
            rank: vec![0; n],
        }
    }

    fn find(&mut self, x: usize) -> usize {
        if self.parent[x] != x {
            self.parent[x] = self.find(self.parent[x]);
        }
        self.parent[x]
    }

    fn union(&mut self, x: usize, y: usize) -> bool {
        let rx = self.find(x);
        let ry = self.find(y);
        if rx == ry {
            return false;
        }
        match self.rank[rx].cmp(&self.rank[ry]) {
            std::cmp::Ordering::Less => self.parent[rx] = ry,
            std::cmp::Ordering::Greater => self.parent[ry] = rx,
            std::cmp::Ordering::Equal => {
                self.parent[ry] = rx;
                self.rank[rx] += 1;
            }
        }
        true
    }
}

fn main() {
    // Triangle: edges (0,1,1), (1,2,2), (0,2,3)
    // MST: {(0,1,1), (1,2,2)} weight = 3
    // Swap (0,2,3) for (1,2,2): new tree {(0,1,1), (0,2,3)} weight = 4
    let edges = vec![(0, 1, 1), (1, 2, 2), (0, 2, 3)];
    let result = second_mst(3, &edges);
    assert_eq!(result, Some(4));

    // Square with diagonal
    //   0---1
    //   |\ /|
    //   | X |
    //   |/ \|
    //   3---2
    let edges = vec![
        (0, 1, 1), (1, 2, 2), (2, 3, 3), (0, 3, 4),
        (0, 2, 5), (1, 3, 6),
    ];
    let result = second_mst(4, &edges);
    // MST: (0,1,1), (1,2,2), (2,3,3) = 6
    // Try adding (0,3,4): removes max on path 0->1->2->3 which is 3, new = 6-3+4 = 7
    // Try adding (0,2,5): removes max on path 0->1->2 which is 2, new = 6-2+5 = 9
    // Try adding (1,3,6): removes max on path 1->2->3 which is 3, new = 6-3+6 = 9
    // Second MST = 7
    assert_eq!(result, Some(7));

    // All edges same weight: MST = second MST
    let edges = vec![(0, 1, 5), (1, 2, 5), (0, 2, 5)];
    // MST weight = 10, any swap gives 10 (not strictly greater)
    let result = second_mst(3, &edges);
    assert_eq!(result, None);

    println!("All second_mst tests passed.");
}
```

**Complexity**: O(E log E) for Kruskal's, plus O(E * V) for all path queries. Total: O(E * V), which is acceptable for n <= 1000. For larger inputs, precompute path maximums using LCA with sparse table in O(V log V) preprocessing and O(1) per query.

</details>

---

## Problem 3: MST with Mandatory Edge

Given a connected, weighted, undirected graph and a specific edge that must be included in the spanning tree, find the minimum weight spanning tree that includes that mandatory edge.

**Constraints**: 2 <= n <= 100,000. n-1 <= edges.len() <= 200,000. The mandatory edge connects two distinct vertices in the graph.

**Key insight**: First add the mandatory edge to the spanning tree. This merges its two endpoints into one component. Then run Kruskal's on the remaining edges, treating the two endpoints as already connected.

**Hints**:
- Initialize the Union-Find with the mandatory edge already unioned
- Start the MST weight with the mandatory edge weight
- Run Kruskal's normally on all other edges
- If the mandatory edge was already in the normal MST, the result is the same MST

<details>
<summary>Solution</summary>

```rust
fn mst_with_mandatory_edge(
    n: usize,
    edges: &[(usize, usize, i64)],
    mandatory: (usize, usize, i64),
) -> i64 {
    let mut sorted: Vec<(i64, usize, usize)> = edges
        .iter()
        .map(|&(u, v, w)| (w, u, v))
        .collect();
    sorted.sort_unstable();

    let mut uf = UnionFind::new(n);

    // Force the mandatory edge
    let (mu, mv, mw) = mandatory;
    uf.union(mu, mv);
    let mut total = mw;
    let mut edge_count = 1usize;

    // Run Kruskal's on remaining edges
    for &(w, u, v) in &sorted {
        // Skip the mandatory edge itself if it appears in the list
        if (u == mu && v == mv) || (u == mv && v == mu) {
            continue;
        }
        if uf.union(u, v) {
            total += w;
            edge_count += 1;
            if edge_count == n - 1 {
                break;
            }
        }
    }

    total
}

struct UnionFind {
    parent: Vec<usize>,
    rank: Vec<usize>,
}

impl UnionFind {
    fn new(n: usize) -> Self {
        Self {
            parent: (0..n).collect(),
            rank: vec![0; n],
        }
    }

    fn find(&mut self, x: usize) -> usize {
        if self.parent[x] != x {
            self.parent[x] = self.find(self.parent[x]);
        }
        self.parent[x]
    }

    fn union(&mut self, x: usize, y: usize) -> bool {
        let rx = self.find(x);
        let ry = self.find(y);
        if rx == ry {
            return false;
        }
        match self.rank[rx].cmp(&self.rank[ry]) {
            std::cmp::Ordering::Less => self.parent[rx] = ry,
            std::cmp::Ordering::Greater => self.parent[ry] = rx,
            std::cmp::Ordering::Equal => {
                self.parent[ry] = rx;
                self.rank[rx] += 1;
            }
        }
        true
    }
}

fn main() {
    // Graph: 0-1(1), 1-2(2), 0-2(3), 2-3(4), 1-3(5)
    // Normal MST: (0,1,1), (1,2,2), (2,3,4) = 7
    // Mandatory edge (1,3,5): force it, then add (0,1,1), (1,2,2) = 8
    let edges = vec![
        (0, 1, 1), (1, 2, 2), (0, 2, 3), (2, 3, 4), (1, 3, 5),
    ];

    let normal_mst = {
        let mut uf = UnionFind::new(4);
        let mut sorted: Vec<_> = edges.iter().map(|&(u, v, w)| (w, u, v)).collect();
        sorted.sort_unstable();
        let mut total = 0;
        for (w, u, v) in sorted {
            if uf.union(u, v) {
                total += w;
            }
        }
        total
    };
    println!("Normal MST weight: {normal_mst}");
    assert_eq!(normal_mst, 7);

    // With mandatory edge (1,3,5)
    let result = mst_with_mandatory_edge(4, &edges, (1, 3, 5));
    println!("MST with mandatory (1,3,5): {result}");
    assert_eq!(result, 8); // (1,3,5) + (0,1,1) + (1,2,2)

    // Mandatory edge that is already in MST
    let result = mst_with_mandatory_edge(4, &edges, (0, 1, 1));
    println!("MST with mandatory (0,1,1): {result}");
    assert_eq!(result, 7); // same as normal MST

    // Mandatory edge (0,2,3)
    let result = mst_with_mandatory_edge(4, &edges, (0, 2, 3));
    println!("MST with mandatory (0,2,3): {result}");
    assert_eq!(result, 8); // (0,2,3) + (0,1,1) + (2,3,4)

    println!("All mst_mandatory_edge tests passed.");
}
```

**Complexity**: O(E log E) for sorting, plus O(E * alpha(V)) for Union-Find operations. Same as standard Kruskal's.

</details>

---

## Problem 4: Network Cable Minimization

A company needs to connect `n` offices with network cables. You are given the cost of laying cable between each pair of offices. However, `k` pairs of offices already have existing connections (cost 0). Find the minimum additional cost to ensure all offices are connected.

**Constraints**: 1 <= n <= 100,000. 0 <= k <= n-1. You are given additional potential edges with positive costs.

**Key insight**: This is MST with some edges having zero cost (the existing connections). Simply add all existing connections first (they are free), then run Kruskal's on the remaining edges.

**Hints**:
- Union all existing connections first in the Union-Find
- Then sort the potential new edges by cost and run Kruskal's
- The answer is the sum of the new edges added
- Alternatively, include zero-cost edges in the edge list and run standard Kruskal's

<details>
<summary>Solution</summary>

```rust
fn min_cable_cost(
    n: usize,
    existing: &[(usize, usize)],
    potential: &[(usize, usize, i64)],
) -> Option<i64> {
    let mut uf = UnionFind::new(n);

    // Step 1: Add all existing connections (free)
    for &(u, v) in existing {
        uf.union(u, v);
    }

    // Step 2: Sort potential edges by cost
    let mut sorted: Vec<(i64, usize, usize)> = potential
        .iter()
        .map(|&(u, v, w)| (w, u, v))
        .collect();
    sorted.sort_unstable();

    // Step 3: Kruskal's on potential edges
    let mut total_cost = 0i64;

    for (w, u, v) in sorted {
        if uf.union(u, v) {
            total_cost += w;
        }
    }

    // Check if all offices are connected
    let root = uf.find(0);
    let all_connected = (1..n).all(|i| uf.find(i) == root);

    if all_connected {
        Some(total_cost)
    } else {
        None // impossible to connect all offices
    }
}

struct UnionFind {
    parent: Vec<usize>,
    rank: Vec<usize>,
}

impl UnionFind {
    fn new(n: usize) -> Self {
        Self {
            parent: (0..n).collect(),
            rank: vec![0; n],
        }
    }

    fn find(&mut self, x: usize) -> usize {
        if self.parent[x] != x {
            self.parent[x] = self.find(self.parent[x]);
        }
        self.parent[x]
    }

    fn union(&mut self, x: usize, y: usize) -> bool {
        let rx = self.find(x);
        let ry = self.find(y);
        if rx == ry {
            return false;
        }
        match self.rank[rx].cmp(&self.rank[ry]) {
            std::cmp::Ordering::Less => self.parent[rx] = ry,
            std::cmp::Ordering::Greater => self.parent[ry] = rx,
            std::cmp::Ordering::Equal => {
                self.parent[ry] = rx;
                self.rank[rx] += 1;
            }
        }
        true
    }
}

fn main() {
    // 5 offices, 2 existing connections
    // Existing: 0-1, 2-3
    // Potential: 1-2 (cost 5), 3-4 (cost 3), 0-4 (cost 10), 1-3 (cost 4)
    let existing = vec![(0, 1), (2, 3)];
    let potential = vec![(1, 2, 5), (3, 4, 3), (0, 4, 10), (1, 3, 4)];

    let result = min_cable_cost(5, &existing, &potential);
    // After existing: {0,1}, {2,3}, {4}
    // Sorted potential: (3,3,4), (4,1,3), (5,1,2), (10,0,4)
    // Add (3,4,3): {0,1}, {2,3,4} -- cost 3
    // Add (1,3,4): {0,1,2,3,4} -- cost 4
    // Total: 7
    assert_eq!(result, Some(7));

    // All already connected
    let existing = vec![(0, 1), (1, 2), (2, 3)];
    let potential = vec![(0, 3, 100)];
    assert_eq!(min_cable_cost(4, &existing, &potential), Some(0));

    // Impossible to connect
    let existing = vec![];
    let potential = vec![(0, 1, 5)]; // only 2 of 4 nodes can be reached
    assert_eq!(min_cable_cost(4, &existing, &potential), None);

    // No existing connections
    let potential = vec![(0, 1, 3), (1, 2, 1), (0, 2, 2)];
    let result = min_cable_cost(3, &[], &potential);
    assert_eq!(result, Some(3)); // edges (1,2,1) and (0,2,2)

    // Large test: chain of existing connections
    let n = 100;
    let existing: Vec<(usize, usize)> = (0..n - 1).map(|i| (i, i + 1)).collect();
    let potential = vec![(0, n - 1, 1000)];
    assert_eq!(min_cable_cost(n, &existing, &potential), Some(0));

    println!("All network_cable tests passed.");
}
```

**Complexity**: O(E log E + (V + E) * alpha(V)) where E is the number of potential edges. The existing connections add O(k * alpha(V)). Total: O(E log E).

</details>

---

## Complexity Summary

| Problem | Time | Space | Technique |
|---------|------|-------|-----------|
| Min Cost Connect Points | O(n^2 log n) | O(n^2) | Kruskal's or Prim's on complete graph |
| Second Minimum ST | O(E * V) | O(V + E) | MST + edge swap with path max query |
| MST with Mandatory Edge | O(E log E) | O(V + E) | Pre-union mandatory edge + Kruskal's |
| Network Cable Minimization | O(E log E) | O(V + E) | Pre-union existing edges + Kruskal's |

---

## Verification

Create a test project and run all solutions:

```bash
cargo new mst-lab && cd mst-lab
```

Paste any solution into `src/main.rs` and run:

```bash
cargo run
```

To verify Kruskal's and Prim's produce the same MST weight on random graphs:

```rust
// Add to any solution's main():
let points: Vec<(i64, i64)> = (0..100)
    .map(|i| (i * 7 % 1000, i * 13 % 1000))
    .collect();
assert_eq!(min_cost_kruskal(&points), min_cost_prim(&points));
```

Run clippy for idiomatic checks:

```bash
cargo clippy -- -W clippy::pedantic
```

---

## What You Learned

- **Kruskal's algorithm** sorts edges by weight and greedily adds the cheapest edge that does not create a cycle, using Union-Find for O(E log E) total time. It is the default MST algorithm in competitive programming due to its simplicity.
- **Union-Find (DSU)** with path compression and union by rank provides near-constant-time `find` and `union` operations (amortized O(alpha(n))). It is the essential data structure for connectivity queries.
- **Prim's algorithm** grows the MST from a starting vertex using a `BinaryHeap<Reverse<T>>` min-heap (Rust's max-heap inverted with `std::cmp::Reverse`). It is preferred for dense graphs where the adjacency list is more natural than an edge list.
- **Boruvka's algorithm** works in rounds where each component finds its cheapest outgoing edge. While less common in contests, it enables parallelism and works well with implicit edge definitions.
- **Sorting edges** in Rust uses `sort_unstable_by_key` for best performance (no allocation for stability). For edge tuples `(u, v, w)`, sorting by `|e| e.2` gives weight-sorted order.
- **Edge swap technique** for the second MST adds a non-MST edge (creating a cycle) and removes the heaviest edge on the MST path between its endpoints, producing the next-cheapest spanning tree.
- **Pre-union technique** handles mandatory edges and existing connections by initializing the Union-Find with those edges already merged before running standard Kruskal's on the remaining edges.
