# 25. CP: Basic Graph BFS/DFS

**Difficulty**: Intermedio

## Prerequisites

- `Vec<Vec<usize>>` (adjacency list representation)
- `VecDeque` for BFS queues
- Recursion and stack-based iteration
- `HashSet` or `Vec<bool>` for visited tracking
- Competitive programming I/O patterns (see exercise 20)

## Learning Objectives

1. Represent graphs as adjacency lists in Rust using `Vec<Vec<usize>>`
2. Implement BFS to find shortest paths in unweighted graphs
3. Implement DFS to explore connected components
4. Detect cycles in undirected and directed graphs
5. Solve grid-based problems (flood fill, island counting) using BFS/DFS

---

## Concepts

### Graph Representation in Rust

The standard competitive programming representation is an **adjacency list**:

```rust
// n nodes (0-indexed), m edges
let n = 5;
let mut adj: Vec<Vec<usize>> = vec![vec![]; n];

// Add undirected edge between u and v
fn add_edge(adj: &mut Vec<Vec<usize>>, u: usize, v: usize) {
    adj[u].push(v);
    adj[v].push(u);
}
```

```
Graph:           Adjacency List:

  0 --- 1        0: [1, 2]
  |   / |        1: [0, 2, 3]
  |  /  |        2: [0, 1]
  2     3        3: [1, 4]
        |        4: [3]
        4
```

For **weighted graphs**, use `Vec<Vec<(usize, i64)>>` where each entry is `(neighbor, weight)`.

For **directed graphs**, only add `adj[u].push(v)` (not the reverse).

### BFS (Breadth-First Search)

BFS explores nodes level by level using a queue. It finds the **shortest path** in
unweighted graphs.

```
BFS from node 0:

  Queue: [0]          Visited: {0}         Distance: [0, -, -, -, -]

  Process 0:
    Neighbors: 1, 2
    Queue: [1, 2]     Visited: {0,1,2}     Distance: [0, 1, 1, -, -]

  Process 1:
    Neighbors: 0(skip), 2(skip), 3
    Queue: [2, 3]     Visited: {0,1,2,3}   Distance: [0, 1, 1, 2, -]

  Process 2:
    Neighbors: 0(skip), 1(skip)
    Queue: [3]

  Process 3:
    Neighbors: 1(skip), 4
    Queue: [4]        Visited: {0,1,2,3,4} Distance: [0, 1, 1, 2, 3]

  Process 4:
    Neighbors: 3(skip)
    Queue: []         DONE
```

```rust
use std::collections::VecDeque;

fn bfs(adj: &Vec<Vec<usize>>, start: usize) -> Vec<i32> {
    let n = adj.len();
    let mut dist = vec![-1i32; n]; // -1 means unreachable
    let mut queue = VecDeque::new();

    dist[start] = 0;
    queue.push_back(start);

    while let Some(u) = queue.pop_front() {
        for &v in &adj[u] {
            if dist[v] == -1 {
                dist[v] = dist[u] + 1;
                queue.push_back(v);
            }
        }
    }

    dist
}
```

### DFS (Depth-First Search)

DFS explores as deep as possible before backtracking. Can be implemented recursively
or with an explicit stack.

```
DFS from node 0 (recursive):

  Visit 0 -> Visit 1 -> Visit 2 (backtrack, 0 visited)
                      -> Visit 3 -> Visit 4 (backtrack)
                                 (backtrack)
          (backtrack)
  -> Visit 2 (already visited, skip)

  Order: 0, 1, 2, 3, 4
```

```rust
// Recursive DFS
fn dfs(adj: &Vec<Vec<usize>>, u: usize, visited: &mut Vec<bool>) {
    visited[u] = true;
    for &v in &adj[u] {
        if !visited[v] {
            dfs(adj, v, visited);
        }
    }
}

// Iterative DFS (using Vec as stack)
fn dfs_iterative(adj: &Vec<Vec<usize>>, start: usize, visited: &mut Vec<bool>) {
    let mut stack = vec![start];
    visited[start] = true;

    while let Some(u) = stack.pop() {
        for &v in &adj[u] {
            if !visited[v] {
                visited[v] = true;
                stack.push(v);
            }
        }
    }
}
```

### Grid as Graph

Many problems use a 2D grid as an implicit graph. Each cell is a node, and its
neighbors are the 4 (or 8) adjacent cells.

```rust
let rows = grid.len();
let cols = grid[0].len();
let directions: [(i32, i32); 4] = [(-1, 0), (1, 0), (0, -1), (0, 1)];

for &(dr, dc) in &directions {
    let nr = r as i32 + dr;
    let nc = c as i32 + dc;
    if nr >= 0 && nr < rows as i32 && nc >= 0 && nc < cols as i32 {
        let (nr, nc) = (nr as usize, nc as usize);
        // process neighbor (nr, nc)
    }
}
```

### Cycle Detection

```
Undirected graph -- DFS with parent tracking:
  If we visit a neighbor that is already visited AND it's not our parent,
  there's a cycle.

Directed graph -- DFS with coloring:
  WHITE (0) = unvisited
  GRAY  (1) = in current DFS path (on the stack)
  BLACK (2) = fully processed

  If we reach a GRAY node, there's a back edge => cycle.
```

---

## Problem 1: BFS Shortest Path in Unweighted Graph

### Statement

Given an undirected unweighted graph with `n` nodes (1-indexed) and `m` edges, and a
source node `s`, find the shortest distance from `s` to every other node. If a node is
unreachable, its distance is `-1`.

### Input Format

```
n m s
u_1 v_1
u_2 v_2
...
u_m v_m
```

### Output Format

`n` integers on a single line: the distance from `s` to nodes 1, 2, ..., n (space-separated).

### Constraints

- 1 <= n <= 10^5
- 0 <= m <= 2 * 10^5
- 1 <= s <= n
- 1 <= u_i, v_i <= n
- No self-loops. Multiple edges possible.

### Examples

```
Input:
5 5 1
1 2
1 3
2 3
2 4
4 5

Output:
0 1 1 2 3
```

```
Input:
4 2 1
1 2
3 4

Output:
0 1 -1 -1
```

### Hints

1. Build an adjacency list (convert 1-indexed to 0-indexed internally).
2. Run BFS from `s`.
3. Output distances, converting back to 1-indexed order.

### Solution Template

```rust
use std::io::{self, Read, Write, BufWriter};
use std::collections::VecDeque;

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let mut iter = input.split_whitespace();
    macro_rules! next {
        ($t:ty) => { iter.next().unwrap().parse::<$t>().unwrap() };
    }
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let n: usize = next!(usize);
    let m: usize = next!(usize);
    let s: usize = next!(usize) - 1; // convert to 0-indexed

    let mut adj: Vec<Vec<usize>> = vec![vec![]; n];
    for _ in 0..m {
        let u = next!(usize) - 1;
        let v = next!(usize) - 1;
        adj[u].push(v);
        adj[v].push(u);
    }

    // TODO: BFS from s
    let mut dist = vec![-1i32; n];
    let mut queue = VecDeque::new();
    dist[s] = 0;
    queue.push_back(s);

    // TODO: Process the queue
    //   while let Some(u) = queue.pop_front() {
    //       for &v in &adj[u] {
    //           if dist[v] == -1 {
    //               dist[v] = dist[u] + 1;
    //               queue.push_back(v);
    //           }
    //       }
    //   }

    let result: Vec<String> = dist.iter().map(|d| d.to_string()).collect();
    writeln!(out, "{}", result.join(" ")).unwrap();
}
```

<details>
<summary>Solution</summary>

```rust
use std::io::{self, Read, Write, BufWriter};
use std::collections::VecDeque;

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let mut iter = input.split_whitespace();
    macro_rules! next {
        ($t:ty) => { iter.next().unwrap().parse::<$t>().unwrap() };
    }
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let n: usize = next!(usize);
    let m: usize = next!(usize);
    let s: usize = next!(usize) - 1;

    let mut adj: Vec<Vec<usize>> = vec![vec![]; n];
    for _ in 0..m {
        let u = next!(usize) - 1;
        let v = next!(usize) - 1;
        adj[u].push(v);
        adj[v].push(u);
    }

    let mut dist = vec![-1i32; n];
    let mut queue = VecDeque::new();
    dist[s] = 0;
    queue.push_back(s);

    while let Some(u) = queue.pop_front() {
        for &v in &adj[u] {
            if dist[v] == -1 {
                dist[v] = dist[u] + 1;
                queue.push_back(v);
            }
        }
    }

    let result: Vec<String> = dist.iter().map(|d| d.to_string()).collect();
    writeln!(out, "{}", result.join(" ")).unwrap();
}
```

**Why BFS gives shortest paths**: BFS explores all nodes at distance `d` before any
node at distance `d+1`. So the first time we reach a node, it is via a shortest path.

**Complexity**: O(n + m).

</details>

---

## Problem 2: Connected Components (DFS)

### Statement

Given an undirected graph with `n` nodes (1-indexed) and `m` edges, find the number of
connected components.

### Input Format

```
n m
u_1 v_1
u_2 v_2
...
u_m v_m
```

### Output Format

A single integer: the number of connected components.

### Constraints

- 1 <= n <= 10^5
- 0 <= m <= 2 * 10^5
- 1 <= u_i, v_i <= n

### Examples

```
Input:
5 3
1 2
2 3
4 5

Output:
2
```

Components: {1, 2, 3} and {4, 5}.

```
Input:
6 0

Output:
6
```

Six isolated nodes.

### Hints

1. Use a `visited` array.
2. For each unvisited node, start a DFS/BFS -- that explores one connected component.
3. Count how many times you start a new exploration.

### Solution Template

```rust
use std::io::{self, Read, Write, BufWriter};

fn dfs(adj: &Vec<Vec<usize>>, u: usize, visited: &mut Vec<bool>) {
    // TODO: Mark u as visited
    // TODO: For each neighbor v of u, if not visited, recurse
}

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let mut iter = input.split_whitespace();
    macro_rules! next {
        ($t:ty) => { iter.next().unwrap().parse::<$t>().unwrap() };
    }
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let n: usize = next!(usize);
    let m: usize = next!(usize);

    let mut adj: Vec<Vec<usize>> = vec![vec![]; n];
    for _ in 0..m {
        let u = next!(usize) - 1;
        let v = next!(usize) - 1;
        adj[u].push(v);
        adj[v].push(u);
    }

    let mut visited = vec![false; n];
    let mut components = 0usize;

    // TODO: For each node 0..n, if not visited, run DFS and increment components
    // WARNING: For large n (10^5), recursive DFS may cause stack overflow.
    //          Consider iterative DFS for safety.

    writeln!(out, "{}", components).unwrap();
}
```

<details>
<summary>Solution</summary>

```rust
use std::io::{self, Read, Write, BufWriter};

fn dfs_iterative(adj: &Vec<Vec<usize>>, start: usize, visited: &mut Vec<bool>) {
    let mut stack = vec![start];
    visited[start] = true;

    while let Some(u) = stack.pop() {
        for &v in &adj[u] {
            if !visited[v] {
                visited[v] = true;
                stack.push(v);
            }
        }
    }
}

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let mut iter = input.split_whitespace();
    macro_rules! next {
        ($t:ty) => { iter.next().unwrap().parse::<$t>().unwrap() };
    }
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let n: usize = next!(usize);
    let m: usize = next!(usize);

    let mut adj: Vec<Vec<usize>> = vec![vec![]; n];
    for _ in 0..m {
        let u = next!(usize) - 1;
        let v = next!(usize) - 1;
        adj[u].push(v);
        adj[v].push(u);
    }

    let mut visited = vec![false; n];
    let mut components = 0usize;

    for i in 0..n {
        if !visited[i] {
            dfs_iterative(&adj, i, &mut visited);
            components += 1;
        }
    }

    writeln!(out, "{}", components).unwrap();
}
```

**Why iterative?** Recursive DFS on a graph with 10^5 nodes can exceed the default
stack size (~8MB) if the graph is a long chain. Iterative DFS avoids this.

**To increase stack size** (alternative to iterative):

```rust
// Spawn a thread with a larger stack
let builder = std::thread::Builder::new().stack_size(32 * 1024 * 1024);
let handler = builder.spawn(|| {
    // your main logic here
}).unwrap();
handler.join().unwrap();
```

**Complexity**: O(n + m).

</details>

---

## Problem 3: Cycle Detection in Undirected Graph

### Statement

Given an undirected graph with `n` nodes (1-indexed) and `m` edges, determine if the
graph contains a cycle.

### Input Format

```
n m
u_1 v_1
u_2 v_2
...
u_m v_m
```

### Output Format

Print `YES` if the graph contains a cycle, `NO` otherwise.

### Constraints

- 1 <= n <= 10^5
- 0 <= m <= 2 * 10^5
- 1 <= u_i, v_i <= n
- No self-loops. Multiple edges between the same pair count as a cycle.

### Examples

```
Input:
4 4
1 2
2 3
3 4
4 1

Output:
YES
```

```
Input:
4 3
1 2
2 3
3 4

Output:
NO
```

(This is a tree -- no cycles.)

### Hints

1. A connected component with `n` nodes has a cycle if and only if it has more than
   `n-1` edges. But this doesn't work if we want to detect cycles in individual components.
2. **DFS with parent tracking**: During DFS, if we encounter a visited neighbor that is
   NOT the parent of the current node, we have found a cycle.
3. Be careful with the iterative version: you need to track the parent of each node.

### Solution Template

```rust
use std::io::{self, Read, Write, BufWriter};

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let mut iter = input.split_whitespace();
    macro_rules! next {
        ($t:ty) => { iter.next().unwrap().parse::<$t>().unwrap() };
    }
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let n: usize = next!(usize);
    let m: usize = next!(usize);

    let mut adj: Vec<Vec<usize>> = vec![vec![]; n];
    for _ in 0..m {
        let u = next!(usize) - 1;
        let v = next!(usize) - 1;
        adj[u].push(v);
        adj[v].push(u);
    }

    let mut visited = vec![false; n];
    let mut has_cycle = false;

    // TODO: For each unvisited node, run DFS with parent tracking
    //   Use a stack of (node, parent)
    //   If we encounter a visited neighbor != parent => cycle found

    // CAREFUL: With multi-edges (u-v appears twice), the adjacency list
    // has duplicate entries. A simple parent check may incorrectly flag
    // the second copy. For simplicity, assume no multi-edges, or use
    // edge-index tracking for full correctness.

    writeln!(out, "{}", if has_cycle { "YES" } else { "NO" }).unwrap();
}
```

<details>
<summary>Solution</summary>

```rust
use std::io::{self, Read, Write, BufWriter};

fn has_cycle_dfs(
    adj: &Vec<Vec<usize>>,
    start: usize,
    visited: &mut Vec<bool>,
) -> bool {
    // Stack holds (node, parent)
    let mut stack: Vec<(usize, usize)> = vec![(start, usize::MAX)];
    visited[start] = true;

    while let Some((u, parent)) = stack.pop() {
        for &v in &adj[u] {
            if !visited[v] {
                visited[v] = true;
                stack.push((v, u));
            } else if v != parent {
                return true; // cycle found
            }
        }
    }

    false
}

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let mut iter = input.split_whitespace();
    macro_rules! next {
        ($t:ty) => { iter.next().unwrap().parse::<$t>().unwrap() };
    }
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let n: usize = next!(usize);
    let m: usize = next!(usize);

    let mut adj: Vec<Vec<usize>> = vec![vec![]; n];
    for _ in 0..m {
        let u = next!(usize) - 1;
        let v = next!(usize) - 1;
        adj[u].push(v);
        adj[v].push(u);
    }

    let mut visited = vec![false; n];
    let mut has_cycle = false;

    for i in 0..n {
        if !visited[i] {
            if has_cycle_dfs(&adj, i, &mut visited) {
                has_cycle = true;
                break;
            }
        }
    }

    writeln!(out, "{}", if has_cycle { "YES" } else { "NO" }).unwrap();
}
```

**Subtlety with iterative DFS and parent tracking**: In the iterative version above,
a node may be pushed to the stack multiple times before being processed. The `visited`
check when popping ensures we only process it once, but a neighbor already visited might
be flagged as a cycle even when it is just the parent re-encountered via a different stack
path. For undirected graphs without multi-edges, this simple approach works correctly.
For multi-edges, use edge-index-based tracking.

**Alternative (simpler check)**: For a connected component of `k` nodes, if it has `>= k`
edges, it has a cycle. But counting edges per component also requires DFS.

**Complexity**: O(n + m).

</details>

---

## Problem 4: Flood Fill

### Statement

Given a 2D grid of characters and a starting position `(r, c)`, change the color of all
cells connected to `(r, c)` (same original color, connected via 4-directional adjacency)
to a new color. Print the modified grid.

### Input Format

```
rows cols
grid[0][0] grid[0][1] ... grid[0][cols-1]
grid[1][0] grid[1][1] ... grid[1][cols-1]
...
r c new_color
```

Grid cells and new_color are single characters. `r` and `c` are 0-indexed.

### Output Format

The modified grid, one row per line, characters space-separated.

### Constraints

- 1 <= rows, cols <= 500
- 0 <= r < rows, 0 <= c < cols
- Grid cells are lowercase letters.

### Examples

```
Input:
3 3
a a b
a a b
b b a
0 0 x

Output:
x x b
x x b
b b a
```

All cells connected to (0,0) with color 'a' become 'x'.

```
Input:
2 2
a b
b a
0 0 a

Output:
a b
b a
```

Starting color is already 'a' and new color is 'a' -- no change.

### Hints

1. If the starting cell's color equals the new color, do nothing (avoid infinite loop).
2. BFS/DFS from `(r, c)`, visiting all cells with the original color.
3. Change each visited cell to the new color.
4. Use the 4-directional neighbor pattern.

### Solution Template

```rust
use std::io::{self, Read, Write, BufWriter};
use std::collections::VecDeque;

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let mut iter = input.split_whitespace();
    macro_rules! next {
        () => { iter.next().unwrap() };
        ($t:ty) => { iter.next().unwrap().parse::<$t>().unwrap() };
    }
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let rows: usize = next!(usize);
    let cols: usize = next!(usize);

    let mut grid: Vec<Vec<u8>> = Vec::with_capacity(rows);
    for _ in 0..rows {
        let row: Vec<u8> = (0..cols).map(|_| next!().as_bytes()[0]).collect();
        grid.push(row);
    }

    let r: usize = next!(usize);
    let c: usize = next!(usize);
    let new_color: u8 = next!().as_bytes()[0];

    let old_color = grid[r][c];

    // TODO: If old_color == new_color, skip (nothing to do)

    // TODO: BFS from (r, c)
    //   Queue of (row, col)
    //   Change grid[r][c] to new_color
    //   For each cell popped, check 4 neighbors:
    //     if in bounds and color == old_color => change and enqueue

    // Print grid
    for row in &grid {
        let row_str: Vec<String> = row.iter().map(|&b| (b as char).to_string()).collect();
        writeln!(out, "{}", row_str.join(" ")).unwrap();
    }
}
```

<details>
<summary>Solution</summary>

```rust
use std::io::{self, Read, Write, BufWriter};
use std::collections::VecDeque;

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let mut iter = input.split_whitespace();
    macro_rules! next {
        () => { iter.next().unwrap() };
        ($t:ty) => { iter.next().unwrap().parse::<$t>().unwrap() };
    }
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let rows: usize = next!(usize);
    let cols: usize = next!(usize);

    let mut grid: Vec<Vec<u8>> = Vec::with_capacity(rows);
    for _ in 0..rows {
        let row: Vec<u8> = (0..cols).map(|_| next!().as_bytes()[0]).collect();
        grid.push(row);
    }

    let r: usize = next!(usize);
    let c: usize = next!(usize);
    let new_color: u8 = next!().as_bytes()[0];

    let old_color = grid[r][c];

    if old_color == new_color {
        // Nothing to do, just print
        for row in &grid {
            let row_str: Vec<String> = row.iter().map(|&b| (b as char).to_string()).collect();
            writeln!(out, "{}", row_str.join(" ")).unwrap();
        }
        return;
    }

    let directions: [(i32, i32); 4] = [(-1, 0), (1, 0), (0, -1), (0, 1)];
    let mut queue = VecDeque::new();

    grid[r][c] = new_color;
    queue.push_back((r, c));

    while let Some((cr, cc)) = queue.pop_front() {
        for &(dr, dc) in &directions {
            let nr = cr as i32 + dr;
            let nc = cc as i32 + dc;

            if nr >= 0 && nr < rows as i32 && nc >= 0 && nc < cols as i32 {
                let (nr, nc) = (nr as usize, nc as usize);
                if grid[nr][nc] == old_color {
                    grid[nr][nc] = new_color;
                    queue.push_back((nr, nc));
                }
            }
        }
    }

    for row in &grid {
        let row_str: Vec<String> = row.iter().map(|&b| (b as char).to_string()).collect();
        writeln!(out, "{}", row_str.join(" ")).unwrap();
    }
}
```

**Key trick**: By changing the color *before* enqueueing (not after dequeueing), we
avoid visiting the same cell twice. The color change acts as the "visited" marker.

**Complexity**: O(rows * cols) in the worst case.

</details>

---

## Problem 5: Number of Islands

### Statement

Given a 2D grid of `0`s and `1`s, count the number of **islands**. An island is a group
of `1`s connected 4-directionally (up, down, left, right). Water is `0`.

### Input Format

```
rows cols
grid[0][0] grid[0][1] ... grid[0][cols-1]
grid[1][0] grid[1][1] ... grid[1][cols-1]
...
```

### Output Format

A single integer: the number of islands.

### Constraints

- 1 <= rows, cols <= 1000
- Grid cells are `0` or `1`.

### Examples

```
Input:
4 5
1 1 1 1 0
1 1 0 1 0
1 1 0 0 0
0 0 0 0 0

Output:
1
```

```
Input:
4 5
1 1 0 0 0
1 1 0 0 0
0 0 1 0 0
0 0 0 1 1

Output:
3
```

### Hints

1. This is the connected components problem on a grid.
2. Scan each cell. When you find a `1`, start BFS/DFS to "sink" all connected `1`s
   (change them to `0`). Increment the island count.
3. The grid modification acts as the visited marker.

### Solution Template

```rust
use std::io::{self, Read, Write, BufWriter};
use std::collections::VecDeque;

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let mut iter = input.split_whitespace();
    macro_rules! next {
        ($t:ty) => { iter.next().unwrap().parse::<$t>().unwrap() };
    }
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let rows: usize = next!(usize);
    let cols: usize = next!(usize);
    let mut grid: Vec<Vec<u8>> = Vec::with_capacity(rows);
    for _ in 0..rows {
        let row: Vec<u8> = (0..cols).map(|_| next!(u8)).collect();
        grid.push(row);
    }

    let directions: [(i32, i32); 4] = [(-1, 0), (1, 0), (0, -1), (0, 1)];
    let mut islands = 0usize;

    for r in 0..rows {
        for c in 0..cols {
            if grid[r][c] == 1 {
                islands += 1;

                // TODO: BFS/DFS to sink this island
                //   - Set grid[r][c] = 0
                //   - Enqueue (r, c)
                //   - Process queue: for each cell, check 4 neighbors
                //     if neighbor is 1, set to 0 and enqueue
            }
        }
    }

    writeln!(out, "{}", islands).unwrap();
}
```

<details>
<summary>Solution</summary>

```rust
use std::io::{self, Read, Write, BufWriter};
use std::collections::VecDeque;

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let mut iter = input.split_whitespace();
    macro_rules! next {
        ($t:ty) => { iter.next().unwrap().parse::<$t>().unwrap() };
    }
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let rows: usize = next!(usize);
    let cols: usize = next!(usize);
    let mut grid: Vec<Vec<u8>> = Vec::with_capacity(rows);
    for _ in 0..rows {
        let row: Vec<u8> = (0..cols).map(|_| next!(u8)).collect();
        grid.push(row);
    }

    let directions: [(i32, i32); 4] = [(-1, 0), (1, 0), (0, -1), (0, 1)];
    let mut islands = 0usize;

    for r in 0..rows {
        for c in 0..cols {
            if grid[r][c] == 1 {
                islands += 1;

                // BFS to sink this island
                let mut queue = VecDeque::new();
                grid[r][c] = 0;
                queue.push_back((r, c));

                while let Some((cr, cc)) = queue.pop_front() {
                    for &(dr, dc) in &directions {
                        let nr = cr as i32 + dr;
                        let nc = cc as i32 + dc;

                        if nr >= 0 && nr < rows as i32 && nc >= 0 && nc < cols as i32 {
                            let (nr, nc) = (nr as usize, nc as usize);
                            if grid[nr][nc] == 1 {
                                grid[nr][nc] = 0;
                                queue.push_back((nr, nc));
                            }
                        }
                    }
                }
            }
        }
    }

    writeln!(out, "{}", islands).unwrap();
}
```

**Pattern**: "Count connected components on a grid" = scan + BFS/DFS + mark visited.
This is one of the most common competitive programming patterns for grid problems.

**Complexity**: O(rows * cols). Each cell is visited at most once.

</details>

---

## Summary Cheat Sheet

| Algorithm | Data Structure      | Finds                    | Complexity |
|-----------|---------------------|--------------------------|------------|
| BFS       | `VecDeque` (queue)  | Shortest path (unweight) | O(V + E)   |
| DFS       | `Vec` (stack) / rec | Components, cycles, topo | O(V + E)   |

### Graph Building Patterns in Rust

```rust
// Adjacency list (most common)
let mut adj: Vec<Vec<usize>> = vec![vec![]; n];

// Weighted adjacency list
let mut adj: Vec<Vec<(usize, i64)>> = vec![vec![]; n];

// Edge list (for Kruskal's, etc.)
let mut edges: Vec<(usize, usize, i64)> = Vec::new(); // (u, v, weight)

// Adjacency matrix (small graphs only, n <= 1000)
let mut adj: Vec<Vec<bool>> = vec![vec![false; n]; n];
```

### Grid Navigation Pattern

```rust
let dirs: [(i32, i32); 4] = [(-1,0), (1,0), (0,-1), (0,1)];

for &(dr, dc) in &dirs {
    let nr = r as i32 + dr;
    let nc = c as i32 + dc;
    if nr >= 0 && nr < rows as i32 && nc >= 0 && nc < cols as i32 {
        let (nr, nc) = (nr as usize, nc as usize);
        // process (nr, nc)
    }
}
```

### Common Pitfalls

- **Stack overflow with recursive DFS**: Default stack is ~8MB. For n > 10^4, use
  iterative DFS or spawn a thread with a larger stack.
- **1-indexed vs 0-indexed**: CP problems often use 1-indexed nodes. Convert on input.
- **Forgetting to mark visited before enqueueing**: In BFS, mark visited when you
  enqueue, not when you dequeue. Otherwise the same node gets enqueued multiple times.
- **Directed vs undirected**: For undirected, add edges in both directions.
  For cycle detection in directed graphs, use 3-color DFS (not parent tracking).
- **Grid bounds**: Always check bounds before accessing `grid[nr][nc]`. The cast to `i32`
  and back to `usize` is the standard pattern for handling negative indices.
