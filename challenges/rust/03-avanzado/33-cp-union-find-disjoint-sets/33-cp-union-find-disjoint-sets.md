# 33. CP: Union-Find / Disjoint Sets

## Difficulty: Avanzado

## Introduction

Union-Find (also called Disjoint Set Union, or DSU) is a data structure that maintains a partition of elements into disjoint sets and supports two operations efficiently: **find** (which set does an element belong to?) and **union** (merge two sets). With path compression and union by rank, both operations run in nearly O(1) amortized time.

Union-Find appears everywhere in competitive programming: connected components in graphs, cycle detection, Kruskal's MST, online connectivity, and problems that require dynamically merging groups. Its simplicity and speed make it one of the most practical data structures to have in your toolkit.

In Rust, Union-Find is a natural fit: no interior mutability tricks are needed, the struct owns its data cleanly, and the borrow checker does not interfere because `find` and `union` only need `&mut self`.

---

## Core Implementation

### Theory

A Union-Find structure stores a forest of trees. Each element points to a parent, and the root of each tree is the **representative** of its set. Two optimizations make this efficient:

1. **Path compression:** During `find`, make every node on the path point directly to the root. This flattens the tree.
2. **Union by rank:** When merging two trees, attach the shorter tree under the root of the taller tree. This keeps trees shallow.

With both optimizations, the amortized time per operation is O(alpha(n)), where alpha is the inverse Ackermann function -- effectively constant (alpha(n) <= 4 for any n up to 10^80).

### Full Rust Implementation

```rust
struct UnionFind {
    parent: Vec<usize>,
    rank: Vec<u32>,
    count: usize, // number of disjoint sets
}

impl UnionFind {
    fn new(n: usize) -> Self {
        UnionFind {
            parent: (0..n).collect(), // each element is its own parent
            rank: vec![0; n],
            count: n,
        }
    }

    /// Find the representative (root) of the set containing x.
    /// Uses path compression.
    fn find(&mut self, x: usize) -> usize {
        if self.parent[x] != x {
            self.parent[x] = self.find(self.parent[x]); // path compression
        }
        self.parent[x]
    }

    /// Merge the sets containing x and y.
    /// Returns true if they were in different sets (merge happened).
    fn union(&mut self, x: usize, y: usize) -> bool {
        let rx = self.find(x);
        let ry = self.find(y);
        if rx == ry {
            return false; // already in the same set
        }

        // Union by rank: attach smaller tree under larger
        match self.rank[rx].cmp(&self.rank[ry]) {
            std::cmp::Ordering::Less => self.parent[rx] = ry,
            std::cmp::Ordering::Greater => self.parent[ry] = rx,
            std::cmp::Ordering::Equal => {
                self.parent[ry] = rx;
                self.rank[rx] += 1;
            }
        }

        self.count -= 1;
        true
    }

    /// Check if x and y are in the same set.
    fn connected(&mut self, x: usize, y: usize) -> bool {
        self.find(x) == self.find(y)
    }

    /// Number of disjoint sets.
    fn set_count(&self) -> usize {
        self.count
    }
}
```

**Why `&mut self` for `find`?** Path compression modifies `self.parent`. This is a deliberate design choice in Rust: the mutation is observable (the internal tree structure changes), even though the logical result (the root) stays the same. In C++, you might use `mutable` or just cast away const. In Rust, the honest thing is `&mut self`.

---

## Problem 1: Number of Connected Components

Given n nodes (0 to n-1) and a list of undirected edges, find the number of connected components.

**Example:**
```
n = 5
edges: (0,1), (1,2), (3,4)

Components: {0,1,2}, {3,4} -> Answer: 2
```

**Hints:**
1. Start with n components (each node is its own component).
2. For each edge (u, v), call `union(u, v)`.
3. The answer is `uf.set_count()`.

<details>
<summary>Solution</summary>

```rust
struct UnionFind {
    parent: Vec<usize>,
    rank: Vec<u32>,
    count: usize,
}

impl UnionFind {
    fn new(n: usize) -> Self {
        UnionFind {
            parent: (0..n).collect(),
            rank: vec![0; n],
            count: n,
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
        self.count -= 1;
        true
    }
}

fn count_components(n: usize, edges: &[(usize, usize)]) -> usize {
    let mut uf = UnionFind::new(n);
    for &(u, v) in edges {
        uf.union(u, v);
    }
    uf.count
}

fn main() {
    let edges = vec![(0, 1), (1, 2), (3, 4)];
    println!("{}", count_components(5, &edges)); // 2

    let edges2 = vec![(0, 1), (1, 2), (2, 3), (3, 4)];
    println!("{}", count_components(5, &edges2)); // 1

    let edges3: Vec<(usize, usize)> = vec![];
    println!("{}", count_components(5, &edges3)); // 5
}
```

</details>

**Complexity Analysis:**
- **Time:** O(E * alpha(V)) which is effectively O(E).
- **Space:** O(V).
- **Trade-offs:** BFS/DFS also solves this in O(V + E), but Union-Find is simpler when edges arrive as a stream and you do not need to build an adjacency list.

---

## Problem 2: Redundant Connection

Given a graph that started as a tree with n nodes and had one extra edge added (creating exactly one cycle), find that extra edge. If multiple answers exist, return the last one in the input.

**Example:**
```
n = 5
edges: (0,1), (1,2), (2,3), (3,4), (4,1)

The last edge (4,1) creates a cycle: 1-2-3-4-1. Answer: (4, 1)
```

**Hints:**
1. Process edges in order. For each edge (u, v), check if u and v are already connected.
2. If they are, this edge creates a cycle -- it is the redundant edge.
3. Return the last such edge found (per problem statement, there is exactly one cycle, so the first cycle-creating edge is the answer).

<details>
<summary>Solution</summary>

```rust
fn find_redundant_connection(n: usize, edges: &[(usize, usize)]) -> (usize, usize) {
    let mut uf = UnionFind::new(n);

    for &(u, v) in edges {
        if !uf.union(u, v) {
            return (u, v); // this edge creates a cycle
        }
    }

    unreachable!("Problem guarantees exactly one redundant edge")
}

// (Include UnionFind struct from above)

struct UnionFind {
    parent: Vec<usize>,
    rank: Vec<u32>,
    count: usize,
}

impl UnionFind {
    fn new(n: usize) -> Self {
        UnionFind {
            parent: (0..n).collect(),
            rank: vec![0; n],
            count: n,
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
        self.count -= 1;
        true
    }
}

fn main() {
    let edges = vec![(0, 1), (1, 2), (2, 3), (3, 4), (4, 1)];
    let (u, v) = find_redundant_connection(5, &edges);
    println!("Redundant edge: ({}, {})", u, v); // (4, 1)
}
```

</details>

**Complexity Analysis:**
- **Time:** O(E * alpha(V)).
- **Space:** O(V).

---

## Problem 3: Accounts Merge

Given a list of accounts where each account is `(name, [emails...])`, merge accounts that share at least one email. Two accounts with the same name but no shared emails are different people.

**Example:**
```
accounts = [
    ("John", ["john@mail.com", "john_work@mail.com"]),
    ("John", ["john@mail.com", "john_home@mail.com"]),
    ("Mary", ["mary@mail.com"]),
    ("John", ["johnny@mail.com"]),
]

Merged:
  John: [john@mail.com, john_home@mail.com, john_work@mail.com]
  Mary: [mary@mail.com]
  John: [johnny@mail.com]
```

**Hints:**
1. Assign each email a unique ID.
2. For each account, union all its emails together (first email with each subsequent email).
3. After processing, group emails by their root representative.
4. The name for each group comes from any account that contained one of its emails.

<details>
<summary>Solution</summary>

```rust
use std::collections::HashMap;

struct UnionFind {
    parent: Vec<usize>,
    rank: Vec<u32>,
}

impl UnionFind {
    fn new(n: usize) -> Self {
        UnionFind {
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

    fn union(&mut self, x: usize, y: usize) {
        let rx = self.find(x);
        let ry = self.find(y);
        if rx == ry {
            return;
        }
        match self.rank[rx].cmp(&self.rank[ry]) {
            std::cmp::Ordering::Less => self.parent[rx] = ry,
            std::cmp::Ordering::Greater => self.parent[ry] = rx,
            std::cmp::Ordering::Equal => {
                self.parent[ry] = rx;
                self.rank[rx] += 1;
            }
        }
    }
}

fn accounts_merge(accounts: &[(&str, Vec<&str>)]) -> Vec<(String, Vec<String>)> {
    // Map each email to a unique ID
    let mut email_to_id: HashMap<&str, usize> = HashMap::new();
    let mut id_to_email: Vec<&str> = Vec::new();

    for (_, emails) in accounts {
        for &email in emails {
            if !email_to_id.contains_key(email) {
                let id = id_to_email.len();
                email_to_id.insert(email, id);
                id_to_email.push(email);
            }
        }
    }

    let mut uf = UnionFind::new(id_to_email.len());

    // Map email ID -> account name
    let mut id_to_name: HashMap<usize, &str> = HashMap::new();

    for (name, emails) in accounts {
        if emails.is_empty() {
            continue;
        }
        let first_id = email_to_id[emails[0]];
        id_to_name.entry(first_id).or_insert(name);

        for &email in &emails[1..] {
            let eid = email_to_id[email];
            uf.union(first_id, eid);
            id_to_name.entry(eid).or_insert(name);
        }
    }

    // Group emails by root
    let mut groups: HashMap<usize, Vec<String>> = HashMap::new();
    for (email, &id) in &email_to_id {
        let root = uf.find(id);
        groups
            .entry(root)
            .or_default()
            .push(email.to_string());
    }

    // Build result
    let mut result: Vec<(String, Vec<String>)> = Vec::new();
    for (root, mut emails) in groups {
        emails.sort();
        let name = id_to_name[&root].to_string();
        result.push((name, emails));
    }

    result.sort();
    result
}

fn main() {
    let accounts = vec![
        ("John", vec!["john@mail.com", "john_work@mail.com"]),
        ("John", vec!["john@mail.com", "john_home@mail.com"]),
        ("Mary", vec!["mary@mail.com"]),
        ("John", vec!["johnny@mail.com"]),
    ];

    let merged = accounts_merge(&accounts);
    for (name, emails) in &merged {
        println!("{}: {:?}", name, emails);
    }
    // John: ["john@mail.com", "john_home@mail.com", "john_work@mail.com"]
    // John: ["johnny@mail.com"]
    // Mary: ["mary@mail.com"]
}
```

</details>

**Complexity Analysis:**
- **Time:** O(E * alpha(E) + E log E), where E is the total number of emails. The sorting dominates.
- **Space:** O(E) for the ID mappings and Union-Find.
- **Trade-offs:** BFS/DFS on an email-to-email graph also works but requires building an adjacency list. Union-Find is more natural here since we are merging sets.

---

## Problem 4: Minimum Spanning Tree with Kruskal's Algorithm

Given a weighted undirected graph, find the minimum spanning tree (MST): a subset of edges that connects all nodes with minimum total weight.

**Kruskal's algorithm:**
1. Sort all edges by weight.
2. Process edges in order. For each edge, if it connects two different components, add it to the MST.
3. Stop when we have V-1 edges (or all edges are processed).

Union-Find is the perfect companion: checking and merging components is exactly what it does.

**Example:**
```
n = 6
Edges (u, v, weight):
(0,1,4), (0,2,4), (1,2,2), (2,3,3), (2,5,2), (3,4,3), (3,5,1), (4,5,6)

MST edges: (3,5,1), (1,2,2), (2,5,2), (2,3,3), (0,1,4)
Total weight: 1 + 2 + 2 + 3 + 4 = 12
```

**Hints:**
1. Sort edges by weight.
2. For each edge, use `uf.union(u, v)`. If it returns true, add to MST.
3. The MST has exactly n-1 edges.

<details>
<summary>Solution</summary>

```rust
struct UnionFind {
    parent: Vec<usize>,
    rank: Vec<u32>,
    count: usize,
}

impl UnionFind {
    fn new(n: usize) -> Self {
        UnionFind {
            parent: (0..n).collect(),
            rank: vec![0; n],
            count: n,
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
        self.count -= 1;
        true
    }
}

fn kruskal(n: usize, edges: &mut [(usize, usize, i64)]) -> (i64, Vec<(usize, usize, i64)>) {
    edges.sort_by_key(|&(_, _, w)| w);

    let mut uf = UnionFind::new(n);
    let mut mst_weight = 0i64;
    let mut mst_edges = Vec::new();

    for &(u, v, w) in edges.iter() {
        if uf.union(u, v) {
            mst_weight += w;
            mst_edges.push((u, v, w));
            if mst_edges.len() == n - 1 {
                break;
            }
        }
    }

    (mst_weight, mst_edges)
}

fn main() {
    let mut edges = vec![
        (0, 1, 4i64),
        (0, 2, 4),
        (1, 2, 2),
        (2, 3, 3),
        (2, 5, 2),
        (3, 4, 3),
        (3, 5, 1),
        (4, 5, 6),
    ];

    let (weight, mst) = kruskal(6, &mut edges);
    println!("MST weight: {}", weight); // 12
    println!("MST edges:");
    for (u, v, w) in &mst {
        println!("  {} -- {} (weight {})", u, v, w);
    }
}
```

</details>

**Complexity Analysis:**
- **Time:** O(E log E + E * alpha(V)) = O(E log E). Sorting dominates.
- **Space:** O(V + E).
- **Trade-offs:** Kruskal's with Union-Find is simpler to implement than Prim's with a priority queue. Prim's can be faster for dense graphs (O(V^2) with adjacency matrix), but for sparse graphs Kruskal's is preferred in competitive programming.

---

## Weighted Union-Find

### Theory

Sometimes we need to track a **relationship** between elements, not just whether they are in the same set. Weighted Union-Find stores a weight for each element representing its distance/difference from its root.

**Key idea:** If we know `weight[a]` (distance from a to its root) and `weight[b]` (distance from b to its root), and they share a root, then the distance from a to b is `weight[a] - weight[b]`.

### Problem 5: Evaluate Division (Weighted Union-Find)

Given equations like `a / b = k`, answer queries of the form `x / y = ?`. If either variable is unknown or they are in different groups, return -1.0.

**Example:**
```
equations: [("a","b",2.0), ("b","c",3.0)]
means: a/b = 2.0, b/c = 3.0

queries:
  a/c = ? -> a/b * b/c = 2.0 * 3.0 = 6.0
  b/a = ? -> 1 / (a/b) = 0.5
  a/e = ? -> -1.0 (e is unknown)
  a/a = ? -> 1.0
```

**Hints:**
1. Each variable has a weight representing `variable / root`.
2. When unioning a and b with ratio a/b = k: find roots of a and b, then set the weight of one root to maintain consistency.
3. For query x/y: if same root, answer is `weight[x] / weight[y]`.

<details>
<summary>Solution</summary>

```rust
use std::collections::HashMap;

struct WeightedUnionFind {
    parent: Vec<usize>,
    rank: Vec<u32>,
    weight: Vec<f64>, // weight[i] = i / parent[i], or after compression, i / root[i]
}

impl WeightedUnionFind {
    fn new(n: usize) -> Self {
        WeightedUnionFind {
            parent: (0..n).collect(),
            rank: vec![0; n],
            weight: vec![1.0; n], // each node is its own root, so weight = 1.0
        }
    }

    /// Returns (root, weight_to_root) where weight_to_root = x / root
    fn find(&mut self, x: usize) -> (usize, f64) {
        if self.parent[x] == x {
            return (x, 1.0);
        }
        let (root, parent_weight) = self.find(self.parent[x]);
        self.parent[x] = root;
        self.weight[x] *= parent_weight; // path compression: x / root = (x / parent) * (parent / root)
        (root, self.weight[x])
    }

    /// Union x and y with the relation x / y = ratio
    fn union(&mut self, x: usize, y: usize, ratio: f64) -> bool {
        let (rx, wx) = self.find(x); // wx = x / rx
        let (ry, wy) = self.find(y); // wy = y / ry

        if rx == ry {
            return false; // already in same set
        }

        // We want: rx / ry = (x / ratio) / y * ...
        // x / y = ratio
        // x = wx * rx, y = wy * ry (in terms of roots)
        // Actually: wx = x/rx means x = wx * rx
        // We need rx_root / ry_root such that x / y = ratio
        // rx / ry should be set so that wx / wy * (ry / rx) ...
        // Let's think: after union, root of ry points to rx.
        // weight[ry] = ry / rx = (x / ratio) / y * ...
        // x / rx = wx, y / ry = wy
        // x / y = ratio => (wx * rx) / (wy * ry) = ratio... no, that's not right.
        // x = wx means x/root_x = wx, so x = wx * root_x value...
        // Actually weight[x] = x / root, so x = weight[x] * root in the "ratio graph"
        // We need: x / y = ratio, weight[x] = x/rx, weight[y] = y/ry
        // Attach ry under rx: weight[ry] = ry / rx
        // We need: x/y = (x/rx) / (y/ry) * (ry/rx)^(-1)...
        // x/y = wx / wy * (ry / rx)... no:
        // x = wx * [value of rx], y = wy * [value of ry]
        // x/y = (wx/wy) * ([value of rx] / [value of ry])
        // We set [value of ry] / [value of rx] = weight[ry] = ratio * wy / wx
        // So that x/y = (wx/wy) * (1 / (ratio * wy / wx)) = (wx/wy) * wx/(ratio*wy) -- wrong

        // Simpler: We want weight[ry] = ry/rx
        // x/y = ratio, x/rx = wx, y/ry = wy
        // x/y = (x/rx) * (rx/ry) * (ry/y) = wx * (1/weight[ry]) * (1/wy) = ratio
        // => weight[ry] = wx / (ratio * wy)

        let new_weight = wx / (ratio * wy);

        match self.rank[rx].cmp(&self.rank[ry]) {
            std::cmp::Ordering::Less => {
                self.parent[rx] = ry;
                self.weight[rx] = 1.0 / new_weight; // rx / ry = 1/new_weight => rx under ry
            }
            std::cmp::Ordering::Greater => {
                self.parent[ry] = rx;
                self.weight[ry] = new_weight;
            }
            std::cmp::Ordering::Equal => {
                self.parent[ry] = rx;
                self.weight[ry] = new_weight;
                self.rank[rx] += 1;
            }
        }

        true
    }

    /// Query x / y. Returns None if not in the same set.
    fn query(&mut self, x: usize, y: usize) -> Option<f64> {
        let (rx, wx) = self.find(x);
        let (ry, wy) = self.find(y);
        if rx != ry {
            None
        } else {
            Some(wx / wy) // x/root / (y/root) = x/y
        }
    }
}

fn evaluate_division(
    equations: &[(&str, &str, f64)],
    queries: &[(&str, &str)],
) -> Vec<f64> {
    let mut var_to_id: HashMap<&str, usize> = HashMap::new();
    let mut next_id = 0usize;

    let mut get_id = |var: &str, map: &mut HashMap<&str, usize>, next: &mut usize| -> usize {
        if let Some(&id) = map.get(var) {
            id
        } else {
            let id = *next;
            map.insert(var, id);
            *next += 1;
            id
        }
    };

    // First pass: assign IDs
    for &(a, b, _) in equations {
        get_id(a, &mut var_to_id, &mut next_id);
        get_id(b, &mut var_to_id, &mut next_id);
    }

    let mut uf = WeightedUnionFind::new(next_id);

    // Process equations
    for &(a, b, ratio) in equations {
        let ia = var_to_id[a];
        let ib = var_to_id[b];
        uf.union(ia, ib, ratio);
    }

    // Answer queries
    let mut results = Vec::new();
    for &(x, y) in queries {
        match (var_to_id.get(x), var_to_id.get(y)) {
            (Some(&ix), Some(&iy)) => {
                results.push(uf.query(ix, iy).unwrap_or(-1.0));
            }
            _ => results.push(-1.0),
        }
    }

    results
}

fn main() {
    let equations = vec![
        ("a", "b", 2.0),
        ("b", "c", 3.0),
    ];
    let queries = vec![
        ("a", "c"),
        ("b", "a"),
        ("a", "e"),
        ("a", "a"),
        ("c", "b"),
    ];

    let results = evaluate_division(&equations, &queries);
    for (i, &r) in results.iter().enumerate() {
        println!("{}/{} = {:.4}", queries[i].0, queries[i].1, r);
    }
    // a/c = 6.0000
    // b/a = 0.5000
    // a/e = -1.0000
    // a/a = 1.0000
    // c/b = 0.3333
}
```

</details>

**Complexity Analysis:**
- **Time:** O((E + Q) * alpha(V)) where E = equations, Q = queries, V = variables.
- **Space:** O(V).
- **Trade-offs:** This can also be solved with BFS/DFS on a ratio graph. The Union-Find approach is more elegant for online queries (new equations can be added dynamically). The BFS approach is simpler if all equations are known upfront.

---

## Problem 6: Online Connectivity Queries

Given n nodes and a sequence of operations: either add an edge or query whether two nodes are connected. Answer each query as it arrives.

**Example:**
```
n = 5
Operations:
  add(0, 1)
  query(0, 2)  -> false
  add(1, 2)
  query(0, 2)  -> true
  add(3, 4)
  query(0, 4)  -> false
  add(2, 4)
  query(0, 4)  -> true
```

**Hints:**
1. This is the exact use case Union-Find was designed for.
2. `add(u, v)` -> `uf.union(u, v)`.
3. `query(u, v)` -> `uf.connected(u, v)`.

<details>
<summary>Solution</summary>

```rust
struct UnionFind {
    parent: Vec<usize>,
    rank: Vec<u32>,
}

impl UnionFind {
    fn new(n: usize) -> Self {
        UnionFind {
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
        if rx == ry { return false; }
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

    fn connected(&mut self, x: usize, y: usize) -> bool {
        self.find(x) == self.find(y)
    }
}

enum Op {
    Add(usize, usize),
    Query(usize, usize),
}

fn process_operations(n: usize, ops: &[Op]) -> Vec<bool> {
    let mut uf = UnionFind::new(n);
    let mut results = Vec::new();

    for op in ops {
        match *op {
            Op::Add(u, v) => {
                uf.union(u, v);
            }
            Op::Query(u, v) => {
                results.push(uf.connected(u, v));
            }
        }
    }

    results
}

fn main() {
    let ops = vec![
        Op::Add(0, 1),
        Op::Query(0, 2),   // false
        Op::Add(1, 2),
        Op::Query(0, 2),   // true
        Op::Add(3, 4),
        Op::Query(0, 4),   // false
        Op::Add(2, 4),
        Op::Query(0, 4),   // true
    ];

    let results = process_operations(5, &ops);
    for r in &results {
        println!("{}", r);
    }
}
```

</details>

**Complexity Analysis:**
- **Time:** O(Q * alpha(V)) total for Q operations.
- **Space:** O(V).

---

## Union-Find with Size Tracking

An alternative to union by rank is **union by size**: always attach the smaller tree under the larger tree's root. This has the same theoretical complexity but also lets you query the size of any component.

```rust
struct UnionFindSize {
    parent: Vec<usize>,
    size: Vec<usize>,
    count: usize,
}

impl UnionFindSize {
    fn new(n: usize) -> Self {
        UnionFindSize {
            parent: (0..n).collect(),
            size: vec![1; n],
            count: n,
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
        // Attach smaller under larger
        if self.size[rx] < self.size[ry] {
            self.parent[rx] = ry;
            self.size[ry] += self.size[rx];
        } else {
            self.parent[ry] = rx;
            self.size[rx] += self.size[ry];
        }
        self.count -= 1;
        true
    }

    fn component_size(&mut self, x: usize) -> usize {
        let root = self.find(x);
        self.size[root]
    }
}
```

This variant is useful when problems ask "how large is the component containing x?" or "what is the size of the largest component?".

---

## Comparison with Alternatives

| Approach | Build Time | Query Time | Add Edge | Delete Edge |
|----------|-----------|------------|----------|-------------|
| Union-Find | O(E * alpha(V)) | O(alpha(V)) | O(alpha(V)) | Not supported |
| BFS/DFS | O(V + E) | O(V + E) | Rebuild required | Rebuild required |
| Union-Find with rollback | O(E * log(V)) | O(log(V)) | O(log(V)) | O(log(V)) (LIFO) |

Union-Find does not support edge deletion. If you need deletions, consider:
- **Offline processing:** If all operations are known upfront, process in reverse (deletions become additions).
- **Link-Cut Trees:** O(log n) per operation including deletions, but much more complex.
- **Union-Find with rollback:** Supports undo of the last union (LIFO order only), useful in divide-and-conquer on queries.

---

## Common Pitfalls in Rust

1. **Recursive `find` with large n:** Path compression uses recursion. For n > 10^5, the recursion depth after initial finds can be deep. In practice, path compression keeps trees very flat after the first round of finds, but if you are concerned, use an iterative version:

```rust
fn find_iterative(&mut self, mut x: usize) -> usize {
    // Find root
    let mut root = x;
    while self.parent[root] != root {
        root = self.parent[root];
    }
    // Path compression
    while self.parent[x] != root {
        let next = self.parent[x];
        self.parent[x] = root;
        x = next;
    }
    root
}
```

2. **`find` requires `&mut self`:** This means you cannot call `find` twice simultaneously (e.g., `self.find(x) == self.find(y)` does not compile in a single expression without storing the results). Always bind to variables first.

3. **0-indexed vs 1-indexed:** Competitive programming problems often use 1-indexed nodes. Allocate `n + 1` elements and ignore index 0, or subtract 1 from all inputs.

---

## Further Reading

- **CSES Problem Set** -- "Road Reparation" (Kruskal), "Road Construction" (online connectivity with component sizes).
- **Competitive Programmer's Handbook** (Laaksonen) -- Chapter 15 (Union-Find).
- **cp-algorithms.com** -- "Disjoint Set Union" with weighted and rollback variants.
- Tarjan's original analysis: "Efficiency of a Good But Not Linear Set Union Algorithm" (JACM, 1975).
