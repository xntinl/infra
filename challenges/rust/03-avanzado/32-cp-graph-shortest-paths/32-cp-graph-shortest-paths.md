# 32. CP: Graph Shortest Paths

## Difficulty: Avanzado

## Introduction

Shortest path problems are among the most frequently tested topics in competitive programming. The right algorithm depends on the graph's properties: non-negative weights call for Dijkstra, negative weights require Bellman-Ford, all-pairs needs Floyd-Warshall, and special structures (0-1 weights, DAGs) admit faster solutions.

In Rust, implementing these algorithms has specific considerations: `BinaryHeap` is a **max-heap** (use `std::cmp::Reverse`), floating-point types do not implement `Ord` (use integer weights or wrapper types), and the borrow checker demands careful graph representation.

This exercise covers five core algorithms with full Rust implementations, increasing in complexity, plus the Rust idioms that make them clean and efficient.

---

## Graph Representation in Rust

Before diving into algorithms, let us establish the graph representation used throughout:

```rust
/// Weighted directed edge
#[derive(Clone, Copy)]
struct Edge {
    to: usize,
    weight: i64,
}

/// Adjacency list representation
fn build_adj_list(n: usize, edges: &[(usize, usize, i64)]) -> Vec<Vec<Edge>> {
    let mut adj = vec![vec![]; n];
    for &(u, v, w) in edges {
        adj[u].push(Edge { to: v, weight: w });
        // For undirected graphs, also add:
        // adj[v].push(Edge { to: u, weight: w });
    }
    adj
}
```

For algorithms that iterate over all edges (Bellman-Ford), an edge list is more convenient:

```rust
struct WeightedEdge {
    from: usize,
    to: usize,
    weight: i64,
}
```

---

## Algorithm 1: Dijkstra's Algorithm

### Theory

Dijkstra computes single-source shortest paths in graphs with **non-negative** edge weights. It maintains a priority queue of `(distance, node)` pairs, always processing the node with the smallest tentative distance.

**Key insight:** Once a node is popped from the priority queue, its distance is final (because all remaining paths are at least as long).

**Complexity:** O((V + E) log V) with a binary heap.

### Rust-Specific: BinaryHeap is a Max-Heap

Rust's `std::collections::BinaryHeap` is a **max-heap**. For Dijkstra, we need a min-heap. The standard approach is to wrap values in `std::cmp::Reverse`:

```rust
use std::collections::BinaryHeap;
use std::cmp::Reverse;

// Push (distance, node) as Reverse to get min-heap behavior
heap.push(Reverse((dist, node)));

// Pop gives the smallest distance first
if let Some(Reverse((d, u))) = heap.pop() { ... }
```

### Problem 1: Single-Source Shortest Paths (Non-Negative Weights)

Given a directed graph with n nodes and m edges with non-negative weights, and a source node s, find the shortest distance from s to every other node.

**Example:**
```
n = 5, s = 0
Edges: (0,1,10), (0,2,3), (1,2,1), (1,3,2), (2,1,4), (2,3,8), (2,4,2), (3,4,7), (4,3,9)

Shortest distances from 0:
  0 -> 0: 0
  0 -> 1: 7  (0->2->1)
  0 -> 2: 3  (0->2)
  0 -> 3: 9  (0->2->1->3)
  0 -> 4: 5  (0->2->4)
```

**Hints:**
1. Initialize `dist[s] = 0`, all others `i64::MAX`.
2. Push `(0, s)` onto the min-heap.
3. When popping `(d, u)`, skip if `d > dist[u]` (stale entry).
4. For each neighbor v of u, relax: if `dist[u] + w < dist[v]`, update and push.

<details>
<summary>Solution</summary>

```rust
use std::cmp::Reverse;
use std::collections::BinaryHeap;

#[derive(Clone, Copy)]
struct Edge {
    to: usize,
    weight: i64,
}

fn dijkstra(adj: &[Vec<Edge>], source: usize) -> Vec<i64> {
    let n = adj.len();
    let mut dist = vec![i64::MAX; n];
    dist[source] = 0;

    // Min-heap: (distance, node)
    let mut heap = BinaryHeap::new();
    heap.push(Reverse((0i64, source)));

    while let Some(Reverse((d, u))) = heap.pop() {
        // Skip stale entries
        if d > dist[u] {
            continue;
        }

        for &edge in &adj[u] {
            let new_dist = dist[u] + edge.weight;
            if new_dist < dist[edge.to] {
                dist[edge.to] = new_dist;
                heap.push(Reverse((new_dist, edge.to)));
            }
        }
    }

    dist
}

fn main() {
    let n = 5;
    let edges = vec![
        (0usize, 1usize, 10i64),
        (0, 2, 3),
        (1, 2, 1),
        (1, 3, 2),
        (2, 1, 4),
        (2, 3, 8),
        (2, 4, 2),
        (3, 4, 7),
        (4, 3, 9),
    ];

    let mut adj = vec![vec![]; n];
    for &(u, v, w) in &edges {
        adj[u].push(Edge { to: v, weight: w });
    }

    let dist = dijkstra(&adj, 0);

    for i in 0..n {
        let d = if dist[i] == i64::MAX {
            "INF".to_string()
        } else {
            dist[i].to_string()
        };
        println!("0 -> {}: {}", i, d);
    }
    // Output:
    // 0 -> 0: 0
    // 0 -> 1: 7
    // 0 -> 2: 3
    // 0 -> 3: 9
    // 0 -> 4: 5
}
```

</details>

**Complexity Analysis:**
- **Time:** O((V + E) log V). Each edge causes at most one heap push, and each push/pop is O(log V).
- **Space:** O(V + E) for the graph, O(V) for dist, O(E) worst case for the heap.
- **Trade-offs:** Dijkstra fails on negative-weight edges. For dense graphs (E ~ V^2), the log factor makes it O(V^2 log V), which is worse than the O(V^2) array-based Dijkstra. In competitive programming, the heap-based version is almost always preferred because most graphs are sparse.

### Path Reconstruction

To reconstruct the actual shortest path, track the predecessor of each node:

```rust
fn dijkstra_with_path(adj: &[Vec<Edge>], source: usize) -> (Vec<i64>, Vec<usize>) {
    let n = adj.len();
    let mut dist = vec![i64::MAX; n];
    let mut prev = vec![usize::MAX; n];
    dist[source] = 0;

    let mut heap = BinaryHeap::new();
    heap.push(Reverse((0i64, source)));

    while let Some(Reverse((d, u))) = heap.pop() {
        if d > dist[u] {
            continue;
        }
        for &edge in &adj[u] {
            let new_dist = dist[u] + edge.weight;
            if new_dist < dist[edge.to] {
                dist[edge.to] = new_dist;
                prev[edge.to] = u;
                heap.push(Reverse((new_dist, edge.to)));
            }
        }
    }

    (dist, prev)
}

fn reconstruct_path(prev: &[usize], target: usize) -> Vec<usize> {
    let mut path = vec![];
    let mut cur = target;
    while cur != usize::MAX {
        path.push(cur);
        cur = prev[cur];
    }
    path.reverse();
    path
}
```

---

## Algorithm 2: Bellman-Ford

### Theory

Bellman-Ford handles graphs with **negative-weight** edges and can detect **negative-weight cycles**. It relaxes all edges V-1 times. If any edge can still be relaxed after V-1 iterations, a negative cycle exists.

**When to use over Dijkstra:**
- The graph has negative-weight edges.
- You need to detect negative cycles.
- The graph is small enough (Bellman-Ford is O(VE)).

### Problem 2: Shortest Paths with Negative Weights and Cycle Detection

Given a directed graph that may contain negative-weight edges, find shortest paths from a source or report a negative cycle.

**Example:**
```
n = 4, s = 0
Edges: (0,1,1), (1,2,-1), (2,3,-1), (3,1,-1)

There is a cycle 1->2->3->1 with weight -1 + -1 + -1 = -3 (negative).
Bellman-Ford should detect this.
```

**Hints:**
1. Initialize `dist[s] = 0`, all others `i64::MAX`.
2. Repeat V-1 times: for each edge (u, v, w), if `dist[u] != MAX` and `dist[u] + w < dist[v]`, set `dist[v] = dist[u] + w`.
3. After V-1 iterations, do one more pass. If any edge can still be relaxed, a negative cycle is reachable from s.

<details>
<summary>Solution</summary>

```rust
struct WEdge {
    from: usize,
    to: usize,
    weight: i64,
}

enum BellmanFordResult {
    Distances(Vec<i64>),
    NegativeCycle,
}

fn bellman_ford(n: usize, edges: &[WEdge], source: usize) -> BellmanFordResult {
    let mut dist = vec![i64::MAX; n];
    dist[source] = 0;

    // Relax all edges V-1 times
    for _ in 0..n - 1 {
        let mut updated = false;
        for e in edges {
            if dist[e.from] != i64::MAX && dist[e.from] + e.weight < dist[e.to] {
                dist[e.to] = dist[e.from] + e.weight;
                updated = true;
            }
        }
        // Early termination: no update means we are done
        if !updated {
            break;
        }
    }

    // Check for negative cycles (one more relaxation pass)
    for e in edges {
        if dist[e.from] != i64::MAX && dist[e.from] + e.weight < dist[e.to] {
            return BellmanFordResult::NegativeCycle;
        }
    }

    BellmanFordResult::Distances(dist)
}

fn main() {
    // Graph with negative cycle
    let edges = vec![
        WEdge { from: 0, to: 1, weight: 1 },
        WEdge { from: 1, to: 2, weight: -1 },
        WEdge { from: 2, to: 3, weight: -1 },
        WEdge { from: 3, to: 1, weight: -1 },
    ];

    match bellman_ford(4, &edges, 0) {
        BellmanFordResult::NegativeCycle => println!("Negative cycle detected!"),
        BellmanFordResult::Distances(d) => {
            for (i, &dist) in d.iter().enumerate() {
                println!("0 -> {}: {}", i, dist);
            }
        }
    }

    // Graph without negative cycle
    let edges2 = vec![
        WEdge { from: 0, to: 1, weight: 4 },
        WEdge { from: 0, to: 2, weight: 5 },
        WEdge { from: 1, to: 2, weight: -3 },
        WEdge { from: 2, to: 3, weight: 4 },
    ];

    match bellman_ford(4, &edges2, 0) {
        BellmanFordResult::NegativeCycle => println!("Negative cycle detected!"),
        BellmanFordResult::Distances(d) => {
            for (i, &dist) in d.iter().enumerate() {
                let s = if dist == i64::MAX { "INF".to_string() } else { dist.to_string() };
                println!("0 -> {}: {}", i, s);
            }
        }
    }
    // Output:
    // 0 -> 0: 0
    // 0 -> 1: 4
    // 0 -> 2: 1
    // 0 -> 3: 5
}
```

</details>

**Complexity Analysis:**
- **Time:** O(VE). V-1 passes over all E edges, plus one detection pass.
- **Space:** O(V + E).
- **Trade-offs:** Much slower than Dijkstra for large sparse graphs (O(VE) vs O((V+E) log V)). Only use when negative weights are present. The early termination optimization helps in practice but does not change worst-case complexity.

---

## Algorithm 3: Floyd-Warshall

### Theory

Floyd-Warshall computes **all-pairs shortest paths** in O(V^3). It works with negative weights (but not negative cycles). The key idea: for each intermediate node k, check if going through k improves the path from i to j.

```
dp[i][j] = min(dp[i][j], dp[i][k] + dp[k][j])
```

The order of loops is critical: **k must be the outermost loop**.

### Problem 3: All-Pairs Shortest Paths

Given a directed weighted graph, find the shortest path between every pair of nodes.

**Example:**
```
n = 4
Edges: (0,1,3), (0,3,7), (1,0,8), (1,2,2), (2,0,5), (2,3,1), (3,0,2)

Shortest path matrix:
     0  1  2  3
0  [ 0, 3, 5, 6]
1  [ 5, 0, 2, 3]
2  [ 3, 6, 0, 1]
3  [ 2, 5, 7, 0]
```

**Hints:**
1. Initialize `dist[i][j] = weight(i, j)` if edge exists, `INF` otherwise, `0` for diagonal.
2. Triple nested loop: for k, for i, for j: `dist[i][j] = min(dist[i][j], dist[i][k] + dist[k][j])`.
3. After the algorithm, if `dist[i][i] < 0` for any i, there is a negative cycle through i.

<details>
<summary>Solution</summary>

```rust
const INF: i64 = i64::MAX / 2; // Use half-max to avoid overflow on addition

fn floyd_warshall(n: usize, edges: &[(usize, usize, i64)]) -> Vec<Vec<i64>> {
    let mut dist = vec![vec![INF; n]; n];

    // Diagonal
    for i in 0..n {
        dist[i][i] = 0;
    }

    // Edge weights
    for &(u, v, w) in edges {
        dist[u][v] = dist[u][v].min(w); // handle parallel edges
    }

    // Floyd-Warshall: k is outermost
    for k in 0..n {
        for i in 0..n {
            if dist[i][k] == INF {
                continue; // optimization: skip unreachable
            }
            for j in 0..n {
                if dist[k][j] == INF {
                    continue;
                }
                let through_k = dist[i][k] + dist[k][j];
                if through_k < dist[i][j] {
                    dist[i][j] = through_k;
                }
            }
        }
    }

    dist
}

fn main() {
    let edges = vec![
        (0, 1, 3i64),
        (0, 3, 7),
        (1, 0, 8),
        (1, 2, 2),
        (2, 0, 5),
        (2, 3, 1),
        (3, 0, 2),
    ];

    let dist = floyd_warshall(4, &edges);

    for i in 0..4 {
        let row: Vec<String> = dist[i]
            .iter()
            .map(|&d| if d >= INF { "INF".to_string() } else { d.to_string() })
            .collect();
        println!("{}: [{}]", i, row.join(", "));
    }
    // Output:
    // 0: [0, 3, 5, 6]
    // 1: [5, 0, 2, 3]
    // 2: [3, 6, 0, 1]
    // 3: [2, 5, 7, 0]
}
```

</details>

**Complexity Analysis:**
- **Time:** O(V^3). Three nested loops over V.
- **Space:** O(V^2) for the distance matrix.
- **Trade-offs:** Simple and handles negative weights. Impractical for V > ~2000 in competitive programming (2000^3 = 8 * 10^9 operations). For sparse graphs where you need all-pairs, running Dijkstra from each node (O(V(V+E) log V)) can be faster.

### Detecting Negative Cycles with Floyd-Warshall

After running the algorithm, check the diagonal:

```rust
fn has_negative_cycle(dist: &[Vec<i64>]) -> bool {
    for i in 0..dist.len() {
        if dist[i][i] < 0 {
            return true;
        }
    }
    false
}
```

---

## Algorithm 4: 0-1 BFS

### Theory

When edge weights are only **0 or 1**, we can use a **deque** (double-ended queue) instead of a priority queue. Push 0-weight edges to the front and 1-weight edges to the back. This gives O(V + E) time, better than Dijkstra's O((V + E) log V).

**Why it works:** The deque maintains the invariant that distances are non-decreasing from front to back, just like a priority queue but without the log factor.

### Problem 4: Grid Shortest Path with Walls

Given a grid where cells are either empty (0) or walls (1), find the minimum number of walls you must break to travel from top-left to bottom-right. Moving to an empty cell costs 0, breaking a wall costs 1.

**Example:**
```
Grid:
0 0 1
1 0 1
1 1 0

Answer: 1 (path: (0,0)->(0,1)->(1,1)->(2,2) breaks 0 walls,
         but wait, (2,2) is 0, so cost = grid[0][0] + grid[0][1] + grid[1][1] + grid[2][2] = 0.
         Actually the cost is the number of 1-cells entered, excluding start.
         Path (0,0)->(0,1)->(1,1)->(2,1)->(2,2): costs = 0+0+1+0 = 1?
         Let us reconsider: cost of entering a cell = grid value of that cell.
         (0,0) start, free. (0,1) = 0. (1,1) = 0. (2,2) need to reach...
         (1,1)->(2,1)=1, cost 1. (2,1)->(2,2)=0, cost 0. Total: 1.
         OR (1,1)->(1,2)=1, cost 1. (1,2)->(2,2)=0, cost 0. Total: 1.
         Is there a 0-cost path? (0,0)->(0,1)->(1,1)...no way to (2,2) without hitting a 1.
         Answer: 1)
```

**Hints:**
1. Use a `VecDeque<(usize, usize)>` as the deque.
2. When moving to a cell with value 0, push_front (cost 0). Value 1, push_back (cost 1).
3. `dist[r][c]` = minimum walls broken to reach (r, c).
4. Process until deque is empty.

<details>
<summary>Solution</summary>

```rust
use std::collections::VecDeque;

fn min_walls_to_break(grid: &[Vec<u8>]) -> i32 {
    let rows = grid.len();
    if rows == 0 {
        return 0;
    }
    let cols = grid[0].len();
    if cols == 0 {
        return 0;
    }

    let mut dist = vec![vec![i32::MAX; cols]; rows];
    dist[0][0] = grid[0][0] as i32; // cost to enter start cell

    let mut deque = VecDeque::new();
    deque.push_back((0usize, 0usize));

    let directions: [(i32, i32); 4] = [(-1, 0), (1, 0), (0, -1), (0, 1)];

    while let Some((r, c)) = deque.pop_front() {
        for &(dr, dc) in &directions {
            let nr = r as i32 + dr;
            let nc = c as i32 + dc;
            if nr < 0 || nr >= rows as i32 || nc < 0 || nc >= cols as i32 {
                continue;
            }
            let nr = nr as usize;
            let nc = nc as usize;
            let new_dist = dist[r][c] + grid[nr][nc] as i32;

            if new_dist < dist[nr][nc] {
                dist[nr][nc] = new_dist;
                if grid[nr][nc] == 0 {
                    deque.push_front((nr, nc));
                } else {
                    deque.push_back((nr, nc));
                }
            }
        }
    }

    dist[rows - 1][cols - 1]
}

fn main() {
    let grid = vec![
        vec![0u8, 0, 1],
        vec![1, 0, 1],
        vec![1, 1, 0],
    ];
    println!("{}", min_walls_to_break(&grid)); // 1

    let grid2 = vec![
        vec![0u8, 0, 0],
        vec![0, 0, 0],
        vec![0, 0, 0],
    ];
    println!("{}", min_walls_to_break(&grid2)); // 0

    let grid3 = vec![
        vec![0u8, 1, 1],
        vec![1, 1, 1],
        vec![1, 1, 0],
    ];
    println!("{}", min_walls_to_break(&grid3)); // 3
}
```

</details>

**Complexity Analysis:**
- **Time:** O(V + E) = O(rows * cols). Each cell is processed at most once from the front of the deque.
- **Space:** O(rows * cols) for the distance matrix and deque.
- **Trade-offs:** Strictly better than Dijkstra for 0-1 weighted graphs. In grid problems this is especially valuable since E = 4V, making Dijkstra O(V log V) and 0-1 BFS O(V).

---

## Algorithm 5: Shortest Path in a DAG

### Theory

In a **Directed Acyclic Graph** (DAG), we can find shortest paths in O(V + E) by processing nodes in **topological order**. Since there are no cycles, once we process a node, its distance is final.

This works even with **negative weights** (unlike Dijkstra) and is faster than Bellman-Ford.

### Problem 5: Longest Path in a DAG (Negation Trick)

Given a DAG with weighted edges, find the longest path from a source to all other nodes. This is equivalent to shortest path with negated weights.

**Example:**
```
n = 6, source = 0
Edges (DAG): (0,1,5), (0,2,3), (1,3,6), (1,2,2), (2,4,4), (2,5,2), (2,3,7), (3,5,1), (3,4,-1), (4,5,-2)

Longest path from 0:
0 -> 0: 0
0 -> 1: 5
0 -> 2: 7  (0->1->2, weight 5+2)
0 -> 3: 14 (0->1->2->3, weight 5+2+7)
0 -> 4: 13 (0->1->3->4, no... 0->1->2->3->4? 5+2+7+(-1)=13. Or 0->2->4? 3+4=7.
             Or 0->1->2->4? 5+2+4=11. Best: 5+2+7-1=13)
0 -> 5: 15 (0->1->2->3->5, 5+2+7+1=15)
```

**Hints:**
1. Topological sort using Kahn's algorithm (BFS with in-degree).
2. Process nodes in topological order.
3. For longest path, negate all weights and find shortest path, then negate the result. Or directly take max instead of min during relaxation.

<details>
<summary>Solution</summary>

```rust
fn topological_sort(adj: &[Vec<(usize, i64)>]) -> Vec<usize> {
    let n = adj.len();
    let mut in_degree = vec![0usize; n];
    for u in 0..n {
        for &(v, _) in &adj[u] {
            in_degree[v] += 1;
        }
    }

    let mut queue = std::collections::VecDeque::new();
    for i in 0..n {
        if in_degree[i] == 0 {
            queue.push_back(i);
        }
    }

    let mut order = Vec::with_capacity(n);
    while let Some(u) = queue.pop_front() {
        order.push(u);
        for &(v, _) in &adj[u] {
            in_degree[v] -= 1;
            if in_degree[v] == 0 {
                queue.push_back(v);
            }
        }
    }

    order
}

/// Shortest path in a DAG from source
fn dag_shortest_path(adj: &[Vec<(usize, i64)>], source: usize) -> Vec<i64> {
    let n = adj.len();
    let order = topological_sort(adj);
    let mut dist = vec![i64::MAX; n];
    dist[source] = 0;

    for &u in &order {
        if dist[u] == i64::MAX {
            continue;
        }
        for &(v, w) in &adj[u] {
            if dist[u] + w < dist[v] {
                dist[v] = dist[u] + w;
            }
        }
    }

    dist
}

/// Longest path in a DAG from source
fn dag_longest_path(adj: &[Vec<(usize, i64)>], source: usize) -> Vec<i64> {
    let n = adj.len();
    let order = topological_sort(adj);
    let mut dist = vec![i64::MIN; n];
    dist[source] = 0;

    for &u in &order {
        if dist[u] == i64::MIN {
            continue;
        }
        for &(v, w) in &adj[u] {
            if dist[u] + w > dist[v] {
                dist[v] = dist[u] + w;
            }
        }
    }

    dist
}

fn main() {
    let n = 6;
    let edges = vec![
        (0, 1, 5i64),
        (0, 2, 3),
        (1, 3, 6),
        (1, 2, 2),
        (2, 4, 4),
        (2, 5, 2),
        (2, 3, 7),
        (3, 5, 1),
        (3, 4, -1),
        (4, 5, -2),
    ];

    let mut adj = vec![vec![]; n];
    for &(u, v, w) in &edges {
        adj[u].push((v, w));
    }

    println!("Longest paths from 0:");
    let longest = dag_longest_path(&adj, 0);
    for i in 0..n {
        let d = if longest[i] == i64::MIN {
            "UNREACHABLE".to_string()
        } else {
            longest[i].to_string()
        };
        println!("  0 -> {}: {}", i, d);
    }

    println!("\nShortest paths from 0:");
    let shortest = dag_shortest_path(&adj, 0);
    for i in 0..n {
        let d = if shortest[i] == i64::MAX {
            "UNREACHABLE".to_string()
        } else {
            shortest[i].to_string()
        };
        println!("  0 -> {}: {}", i, d);
    }
}
```

</details>

**Complexity Analysis:**
- **Time:** O(V + E). Topological sort is O(V + E), and the relaxation pass is O(V + E).
- **Space:** O(V + E).
- **Trade-offs:** Only works on DAGs. The fastest possible shortest path algorithm for this graph class. Works with negative weights (unlike Dijkstra) and is faster than Bellman-Ford (O(VE)).

---

## Problem 6: Dijkstra with Multiple States -- Shortest Path with K Stops

Given a weighted directed graph, find the shortest path from source to destination using **at most K intermediate stops** (K + 1 edges total). This is the classic "cheapest flights within K stops" problem.

**Example:**
```
n = 4, source = 0, dest = 3, K = 1
Edges: (0,1,100), (1,2,100), (2,3,100), (0,2,500)

Direct: 0->2->3 = 600 (1 stop at 2, valid since K=1)
Via 1: 0->1->2->3 = 300 (2 stops, exceeds K=1)
Answer: 600
```

**Hints:**
1. State: `(cost, node, stops_remaining)`.
2. Use Dijkstra but include `stops` in the state.
3. `dist[node][stops]` = minimum cost to reach node with exactly `stops` stops remaining.
4. Only relax if `stops > 0`.

<details>
<summary>Solution</summary>

```rust
use std::cmp::Reverse;
use std::collections::BinaryHeap;

#[derive(Clone, Copy)]
struct Edge {
    to: usize,
    weight: i64,
}

fn shortest_with_k_stops(
    adj: &[Vec<Edge>],
    source: usize,
    dest: usize,
    k: usize,
) -> Option<i64> {
    let n = adj.len();
    // dist[node][stops_used] = min cost
    let mut dist = vec![vec![i64::MAX; k + 2]; n];
    dist[source][0] = 0;

    // (cost, node, stops_used)
    let mut heap = BinaryHeap::new();
    heap.push(Reverse((0i64, source, 0usize)));

    while let Some(Reverse((cost, u, stops))) = heap.pop() {
        if u == dest {
            return Some(cost);
        }

        if cost > dist[u][stops] {
            continue;
        }

        if stops > k {
            continue; // used too many stops
        }

        for &edge in &adj[u] {
            let new_cost = cost + edge.weight;
            let new_stops = stops + 1;

            // new_stops = number of edges used, max allowed is k + 1
            if new_stops <= k + 1 && new_cost < dist[edge.to][new_stops] {
                dist[edge.to][new_stops] = new_cost;
                heap.push(Reverse((new_cost, edge.to, new_stops)));
            }
        }
    }

    None
}

fn main() {
    let n = 4;
    let edges_raw = vec![
        (0, 1, 100i64),
        (1, 2, 100),
        (2, 3, 100),
        (0, 2, 500),
    ];

    let mut adj = vec![vec![]; n];
    for &(u, v, w) in &edges_raw {
        adj[u].push(Edge { to: v, weight: w });
    }

    match shortest_with_k_stops(&adj, 0, 3, 1) {
        Some(cost) => println!("Shortest with K=1 stops: {}", cost), // 600
        None => println!("No path found"),
    }

    match shortest_with_k_stops(&adj, 0, 3, 2) {
        Some(cost) => println!("Shortest with K=2 stops: {}", cost), // 300
        None => println!("No path found"),
    }
}
```

</details>

**Complexity Analysis:**
- **Time:** O((V * K + E * K) log(V * K)). The state space is V * K, and each state processes its edges.
- **Space:** O(V * K) for the dist table and heap.
- **Trade-offs:** This is a standard technique of augmenting the Dijkstra state. It generalizes to other constraints (fuel, tolls, etc.) but the state space grows multiplicatively with each added dimension.

---

## Algorithm Comparison

| Algorithm | Time | Negative Weights | Neg. Cycle Detection | Use Case |
|-----------|------|-----------------|---------------------|----------|
| Dijkstra | O((V+E) log V) | No | No | General SSSP, non-negative |
| Bellman-Ford | O(VE) | Yes | Yes | SSSP with negative weights |
| Floyd-Warshall | O(V^3) | Yes | Yes (diagonal check) | All-pairs, small V |
| 0-1 BFS | O(V+E) | N/A (0-1 only) | No | 0-1 weighted graphs |
| DAG Shortest | O(V+E) | Yes | N/A (no cycles) | DAGs only |

## Common Pitfalls in Rust

1. **BinaryHeap is max-heap:** Always wrap in `Reverse` for Dijkstra. Forgetting this gives the **longest** path instead.
2. **Overflow with `i64::MAX`:** Never add to `i64::MAX` directly. Either use `i64::MAX / 2` as infinity or check before adding.
3. **`f64` does not implement `Ord`:** You cannot put `f64` in a `BinaryHeap`. Use integer weights or a newtype with manual `Ord` implementation.
4. **Stale heap entries:** Always check `if d > dist[u] { continue; }` when popping from the heap. Without this, Dijkstra degrades to O(E^2).
5. **Graph input format:** Competitive programming often uses 1-indexed nodes. Convert to 0-indexed immediately: `let u = u - 1;`.

---

## Problem 7: Competitive Programming I/O Pattern

Here is the complete pattern for reading a graph problem from stdin and writing to stdout, as used in competitive programming judges:

```rust
use std::io::{self, Read, Write, BufWriter};
use std::cmp::Reverse;
use std::collections::BinaryHeap;

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let mut iter = input.split_whitespace();
    let n: usize = iter.next().unwrap().parse().unwrap();
    let m: usize = iter.next().unwrap().parse().unwrap();
    let s: usize = iter.next().unwrap().parse::<usize>().unwrap() - 1; // 1-indexed to 0-indexed

    let mut adj = vec![vec![]; n];
    for _ in 0..m {
        let u: usize = iter.next().unwrap().parse::<usize>().unwrap() - 1;
        let v: usize = iter.next().unwrap().parse::<usize>().unwrap() - 1;
        let w: i64 = iter.next().unwrap().parse().unwrap();
        adj[u].push((v, w));
    }

    // Dijkstra
    let mut dist = vec![i64::MAX; n];
    dist[s] = 0;
    let mut heap = BinaryHeap::new();
    heap.push(Reverse((0i64, s)));

    while let Some(Reverse((d, u))) = heap.pop() {
        if d > dist[u] { continue; }
        for &(v, w) in &adj[u] {
            let nd = d + w;
            if nd < dist[v] {
                dist[v] = nd;
                heap.push(Reverse((nd, v)));
            }
        }
    }

    for i in 0..n {
        if dist[i] == i64::MAX {
            write!(out, "-1").unwrap();
        } else {
            write!(out, "{}", dist[i]).unwrap();
        }
        if i + 1 < n { write!(out, " ").unwrap(); }
    }
    writeln!(out).unwrap();
}
```

This pattern uses `read_to_string` + `split_whitespace` for fast parsing and `BufWriter` for fast output -- both essential for tight time limits in competitive programming.

---

## Further Reading

- **CSES Problem Set** -- "Shortest Routes I" (Dijkstra), "Shortest Routes II" (Floyd-Warshall), "High Score" (Bellman-Ford + longest path).
- **Competitive Programmer's Handbook** (Laaksonen) -- Chapters 13 (Shortest Paths).
- **cp-algorithms.com** -- Detailed write-ups with proofs for each algorithm.
- For Rust competitive programming I/O, see the `proconio` crate or hand-rolled fast readers.
