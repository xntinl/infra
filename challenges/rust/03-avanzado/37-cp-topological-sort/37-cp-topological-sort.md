# 37. CP: Topological Sort

## Difficulty: Avanzado

## Introduction

A topological sort of a directed acyclic graph (DAG) is a linear ordering of its vertices such that for every directed edge (u, v), vertex u appears before vertex v in the ordering. This is the canonical algorithm for dependency resolution: build systems, course prerequisites, task scheduling, compiler symbol resolution, and package managers all reduce to topological sort at their core.

There are two classical approaches -- Kahn's algorithm (BFS with in-degree tracking) and DFS-based post-order reversal. Each has distinct advantages: Kahn's naturally detects cycles and can be modified to produce the lexicographically smallest ordering, while the DFS approach integrates seamlessly into existing graph traversal code. This exercise covers both algorithms in depth, cycle detection, and five competition problems.

---

## Graph Representation

Throughout this exercise we use adjacency lists. The standard competitive programming representation in Rust uses `Vec<Vec<usize>>`:

```rust
/// Build a directed adjacency list from a list of edges.
/// Vertices are numbered 0..n.
fn build_graph(n: usize, edges: &[(usize, usize)]) -> Vec<Vec<usize>> {
    let mut adj = vec![vec![]; n];
    for &(u, v) in edges {
        adj[u].push(v); // directed edge u -> v
    }
    adj
}

fn main() {
    // 4 vertices, edges: 0->1, 0->2, 1->3, 2->3
    let adj = build_graph(4, &[(0, 1), (0, 2), (1, 3), (2, 3)]);
    for (u, neighbors) in adj.iter().enumerate() {
        println!("{u} -> {neighbors:?}");
    }
}
```

---

## Kahn's Algorithm (BFS-Based)

Kahn's algorithm works by repeatedly removing vertices with no incoming edges. It maintains an in-degree array and a queue of vertices whose in-degree has reached zero.

### Algorithm

1. Compute the in-degree of every vertex.
2. Add all vertices with in-degree 0 to a queue.
3. While the queue is not empty:
   a. Dequeue a vertex `u` and append it to the result.
   b. For each neighbor `v` of `u`, decrement `in_degree[v]`.
   c. If `in_degree[v]` reaches 0, enqueue `v`.
4. If the result contains all `n` vertices, it is a valid topological order. Otherwise, the graph contains a cycle.

```rust
use std::collections::VecDeque;

fn topological_sort_kahn(adj: &[Vec<usize>]) -> Option<Vec<usize>> {
    let n = adj.len();
    let mut in_degree = vec![0usize; n];

    // Step 1: compute in-degrees
    for neighbors in adj {
        for &v in neighbors {
            in_degree[v] += 1;
        }
    }

    // Step 2: enqueue all zero-indegree vertices
    let mut queue: VecDeque<usize> = (0..n)
        .filter(|&v| in_degree[v] == 0)
        .collect();

    let mut order = Vec::with_capacity(n);

    // Step 3: BFS
    while let Some(u) = queue.pop_front() {
        order.push(u);
        for &v in &adj[u] {
            in_degree[v] -= 1;
            if in_degree[v] == 0 {
                queue.push_back(v);
            }
        }
    }

    // Step 4: check for cycle
    if order.len() == n {
        Some(order)
    } else {
        None // cycle detected: not all vertices were processed
    }
}

fn main() {
    // DAG: 5->0, 5->2, 4->0, 4->1, 2->3, 3->1
    let adj = {
        let mut g = vec![vec![]; 6];
        g[5].push(0);
        g[5].push(2);
        g[4].push(0);
        g[4].push(1);
        g[2].push(3);
        g[3].push(1);
        g
    };

    match topological_sort_kahn(&adj) {
        Some(order) => println!("Topological order: {order:?}"),
        None => println!("Cycle detected!"),
    }
    // One valid output: [4, 5, 0, 2, 3, 1]
}
```

**Why Kahn's detects cycles**: If a cycle exists, the vertices in the cycle never reach in-degree 0. They are never enqueued, so the result is smaller than `n`.

---

## DFS-Based Topological Sort

The DFS approach computes a topological order by recording the post-order (finish time) of a DFS traversal and then reversing it. A vertex is added to the result only after all of its descendants have been fully explored.

```rust
fn topological_sort_dfs(adj: &[Vec<usize>]) -> Option<Vec<usize>> {
    let n = adj.len();

    #[derive(Clone, Copy, PartialEq)]
    enum State {
        Unvisited,
        InStack,   // currently on the recursion stack (gray)
        Completed, // fully processed (black)
    }

    let mut state = vec![State::Unvisited; n];
    let mut order = Vec::with_capacity(n);
    let mut has_cycle = false;

    fn dfs(
        u: usize,
        adj: &[Vec<usize>],
        state: &mut [State],
        order: &mut Vec<usize>,
        has_cycle: &mut bool,
    ) {
        if *has_cycle {
            return;
        }
        state[u] = State::InStack;

        for &v in &adj[u] {
            match state[v] {
                State::Unvisited => dfs(v, adj, state, order, has_cycle),
                State::InStack => {
                    *has_cycle = true; // back edge = cycle
                    return;
                }
                State::Completed => {} // cross/forward edge, ignore
            }
        }

        state[u] = State::Completed;
        order.push(u); // post-order
    }

    for u in 0..n {
        if state[u] == State::Unvisited {
            dfs(u, adj, &mut state, &mut order, &mut has_cycle);
            if has_cycle {
                return None;
            }
        }
    }

    order.reverse(); // reverse post-order = topological order
    Some(order)
}

fn main() {
    let adj = {
        let mut g = vec![vec![]; 6];
        g[5].push(0);
        g[5].push(2);
        g[4].push(0);
        g[4].push(1);
        g[2].push(3);
        g[3].push(1);
        g
    };

    match topological_sort_dfs(&adj) {
        Some(order) => println!("DFS topological order: {order:?}"),
        None => println!("Cycle detected!"),
    }
}
```

### Iterative DFS (Stack-Safe)

For large graphs, recursive DFS can overflow the stack. Here is an iterative version:

```rust
fn topological_sort_dfs_iterative(adj: &[Vec<usize>]) -> Option<Vec<usize>> {
    let n = adj.len();

    #[derive(Clone, Copy, PartialEq)]
    enum State { Unvisited, InStack, Completed }

    let mut state = vec![State::Unvisited; n];
    let mut order = Vec::with_capacity(n);

    // Stack stores (node, index_into_neighbors)
    let mut stack: Vec<(usize, usize)> = Vec::new();

    for start in 0..n {
        if state[start] != State::Unvisited {
            continue;
        }
        state[start] = State::InStack;
        stack.push((start, 0));

        while let Some(&mut (u, ref mut idx)) = stack.last_mut() {
            if *idx < adj[u].len() {
                let v = adj[u][*idx];
                *idx += 1;
                match state[v] {
                    State::Unvisited => {
                        state[v] = State::InStack;
                        stack.push((v, 0));
                    }
                    State::InStack => return None, // cycle
                    State::Completed => {}
                }
            } else {
                state[u] = State::Completed;
                order.push(u);
                stack.pop();
            }
        }
    }

    order.reverse();
    Some(order)
}

fn main() {
    let adj = {
        let mut g = vec![vec![]; 4];
        g[0].push(1);
        g[0].push(2);
        g[1].push(3);
        g[2].push(3);
        g
    };

    match topological_sort_dfs_iterative(&adj) {
        Some(order) => println!("Iterative DFS order: {order:?}"),
        None => println!("Cycle detected!"),
    }
}
```

---

## Lexicographically Smallest Topological Order

When the problem asks for the lexicographically smallest valid topological order, replace Kahn's queue with a min-heap (`BinaryHeap` with `Reverse`):

```rust
use std::collections::BinaryHeap;
use std::cmp::Reverse;

fn topological_sort_lex_smallest(adj: &[Vec<usize>]) -> Option<Vec<usize>> {
    let n = adj.len();
    let mut in_degree = vec![0usize; n];

    for neighbors in adj {
        for &v in neighbors {
            in_degree[v] += 1;
        }
    }

    // Min-heap: always process the smallest-numbered available vertex
    let mut heap: BinaryHeap<Reverse<usize>> = (0..n)
        .filter(|&v| in_degree[v] == 0)
        .map(Reverse)
        .collect();

    let mut order = Vec::with_capacity(n);

    while let Some(Reverse(u)) = heap.pop() {
        order.push(u);
        for &v in &adj[u] {
            in_degree[v] -= 1;
            if in_degree[v] == 0 {
                heap.push(Reverse(v));
            }
        }
    }

    if order.len() == n {
        Some(order)
    } else {
        None
    }
}

fn main() {
    // Graph with multiple valid topological orders
    let adj = {
        let mut g = vec![vec![]; 6];
        g[5].push(0);
        g[5].push(2);
        g[4].push(0);
        g[4].push(1);
        g[2].push(3);
        g[3].push(1);
        g
    };

    match topological_sort_lex_smallest(&adj) {
        Some(order) => println!("Lex smallest order: {order:?}"),
        None => println!("Cycle detected!"),
    }
    // Output: [4, 5, 0, 2, 3, 1]
}
```

**Note on `BinaryHeap` in Rust**: Rust's `BinaryHeap` is a max-heap by default. Wrapping elements in `std::cmp::Reverse` turns it into a min-heap. This is the idiomatic Rust approach -- there is no separate `MinHeap` type.

---

## Kahn's vs DFS: When to Use Which

| Criterion | Kahn's (BFS) | DFS Post-Order |
|-----------|-------------|----------------|
| Cycle detection | Natural (check result length) | Natural (back edge detection) |
| Lex smallest order | Use min-heap instead of queue | Not straightforward |
| Parallelism / levels | Each "wave" of the BFS = one parallelism level | Not directly available |
| Integration with other DFS | Separate pass needed | Piggyback on existing DFS |
| Stack safety | Inherently iterative | Recursive by default (needs iterative variant for large graphs) |
| Finding the cycle itself | Track predecessors in BFS | Track the recursion stack |

---

## Problem 1: Course Schedule

There are `n` courses labeled `0` to `n-1`. You are given a list of prerequisite pairs `[a, b]` meaning you must take course `b` before course `a`. Return `true` if you can finish all courses (i.e., the prerequisite graph is a DAG).

**Constraints**: 1 <= n <= 2,000. 0 <= prerequisites.len() <= 5,000.

**Key insight**: This is a direct cycle detection problem. If the prerequisite graph has a cycle, it is impossible to complete all courses. Run Kahn's algorithm and check if all `n` vertices are processed.

**Hints**:
- Build a directed graph where edge (b, a) means "b must come before a"
- Run Kahn's algorithm
- If the topological order has length `n`, return true; otherwise false

<details>
<summary>Solution</summary>

```rust
use std::collections::VecDeque;

fn can_finish(num_courses: usize, prerequisites: &[(usize, usize)]) -> bool {
    let mut adj = vec![vec![]; num_courses];
    let mut in_degree = vec![0usize; num_courses];

    for &(course, prereq) in prerequisites {
        adj[prereq].push(course);
        in_degree[course] += 1;
    }

    let mut queue: VecDeque<usize> = (0..num_courses)
        .filter(|&v| in_degree[v] == 0)
        .collect();

    let mut count = 0;

    while let Some(u) = queue.pop_front() {
        count += 1;
        for &v in &adj[u] {
            in_degree[v] -= 1;
            if in_degree[v] == 0 {
                queue.push_back(v);
            }
        }
    }

    count == num_courses
}

/// Bonus: return one valid course ordering, or None if impossible
fn find_order(num_courses: usize, prerequisites: &[(usize, usize)]) -> Option<Vec<usize>> {
    let mut adj = vec![vec![]; num_courses];
    let mut in_degree = vec![0usize; num_courses];

    for &(course, prereq) in prerequisites {
        adj[prereq].push(course);
        in_degree[course] += 1;
    }

    let mut queue: VecDeque<usize> = (0..num_courses)
        .filter(|&v| in_degree[v] == 0)
        .collect();

    let mut order = Vec::with_capacity(num_courses);

    while let Some(u) = queue.pop_front() {
        order.push(u);
        for &v in &adj[u] {
            in_degree[v] -= 1;
            if in_degree[v] == 0 {
                queue.push_back(v);
            }
        }
    }

    if order.len() == num_courses {
        Some(order)
    } else {
        None
    }
}

fn main() {
    // Can finish: no cycle
    assert!(can_finish(2, &[(1, 0)]));

    // Cannot finish: cycle 0->1->0
    assert!(!can_finish(2, &[(1, 0), (0, 1)]));

    // Larger DAG
    assert!(can_finish(4, &[(1, 0), (2, 0), (3, 1), (3, 2)]));

    // No prerequisites at all
    assert!(can_finish(5, &[]));

    // Course ordering
    let order = find_order(4, &[(1, 0), (2, 0), (3, 1), (3, 2)]);
    assert!(order.is_some());
    let order = order.unwrap();
    println!("Course order: {order:?}");

    // Verify the ordering is valid
    let mut pos = vec![0usize; 4];
    for (i, &c) in order.iter().enumerate() {
        pos[c] = i;
    }
    for &(course, prereq) in &[(1, 0), (2, 0), (3, 1), (3, 2)] {
        assert!(pos[prereq] < pos[course], "Invalid order: {prereq} should come before {course}");
    }

    println!("All course_schedule tests passed.");
}
```

**Complexity**: O(V + E) time, O(V + E) space, where V is the number of courses and E is the number of prerequisites.

</details>

---

## Problem 2: Alien Dictionary

Given a list of words sorted lexicographically according to an unknown alien language's alphabet, determine the order of characters in the alien alphabet. If no valid order exists (cycle), return an empty string. If multiple valid orders exist, return any one.

**Constraints**: 1 <= words.len() <= 100. 1 <= words[i].len() <= 100. Words contain only lowercase letters.

**Key insight**: Comparing adjacent words in the sorted list reveals ordering constraints between characters. The first position where two adjacent words differ gives a directed edge in the character ordering graph. Then run topological sort on that graph.

**Hints**:
- Compare each consecutive pair of words
- Find the first differing character: `word1[i] != word2[i]` gives edge `word1[i] -> word2[i]`
- If `word1` is a prefix of `word2`, no edge is produced (this is valid)
- If `word2` is a proper prefix of `word1`, the input is invalid (return empty)
- Characters that appear but have no ordering constraints are still in the result

<details>
<summary>Solution</summary>

```rust
use std::collections::{HashMap, HashSet, VecDeque};

fn alien_order(words: &[&str]) -> String {
    // Collect all unique characters
    let mut chars: HashSet<u8> = HashSet::new();
    for word in words {
        for &b in word.as_bytes() {
            chars.insert(b);
        }
    }

    // Build directed graph from adjacent word comparisons
    let mut adj: HashMap<u8, Vec<u8>> = HashMap::new();
    let mut in_degree: HashMap<u8, usize> = HashMap::new();

    for &c in &chars {
        adj.entry(c).or_default();
        in_degree.entry(c).or_insert(0);
    }

    for i in 0..words.len() - 1 {
        let w1 = words[i].as_bytes();
        let w2 = words[i + 1].as_bytes();
        let min_len = w1.len().min(w2.len());

        // Check for invalid case: w1 is longer and is a prefix of w2
        if w1.len() > w2.len() && w1[..min_len] == w2[..min_len] {
            return String::new(); // invalid input
        }

        // Find first differing character
        for j in 0..min_len {
            if w1[j] != w2[j] {
                adj.entry(w1[j]).or_default().push(w2[j]);
                *in_degree.entry(w2[j]).or_insert(0) += 1;
                break; // only the first difference matters
            }
        }
    }

    // Kahn's algorithm
    let mut queue: VecDeque<u8> = in_degree
        .iter()
        .filter(|(_, &deg)| deg == 0)
        .map(|(&c, _)| c)
        .collect();

    let mut result = Vec::new();

    while let Some(c) = queue.pop_front() {
        result.push(c);
        if let Some(neighbors) = adj.get(&c) {
            for &next in neighbors {
                let deg = in_degree.get_mut(&next).unwrap();
                *deg -= 1;
                if *deg == 0 {
                    queue.push_back(next);
                }
            }
        }
    }

    // Cycle check
    if result.len() != chars.len() {
        return String::new();
    }

    String::from_utf8(result).unwrap()
}

fn main() {
    // Example 1: wertf
    let result = alien_order(&["wrt", "wrf", "er", "ett", "rftt"]);
    println!("Alien order: {result}");
    // One valid output: "wertf"
    assert_eq!(result.len(), 5);

    // Example 2: cycle -> empty
    let result = alien_order(&["z", "x", "z"]);
    assert_eq!(result, "");

    // Example 3: invalid prefix
    let result = alien_order(&["abc", "ab"]);
    assert_eq!(result, "");

    // Example 4: single word
    let result = alien_order(&["z"]);
    assert_eq!(result, "z");

    // Example 5: two words, one constraint
    let result = alien_order(&["ab", "ba"]);
    println!("Two-word order: {result}");
    assert!(result.contains('a'));
    assert!(result.contains('b'));

    println!("All alien_dictionary tests passed.");
}
```

**Complexity**: O(C) time and space, where C is the total number of characters across all words. Building the graph is O(C), and topological sort is O(V + E) where V is the number of distinct characters and E is the number of edges derived from word comparisons.

</details>

---

## Problem 3: Task Scheduling with Dependencies

You are given `n` tasks labeled `0` to `n-1`, a list of dependencies `[a, b]` meaning task `b` must complete before task `a` can start, and each task takes a given amount of time. Assuming unlimited parallelism (you can run as many tasks simultaneously as their dependencies allow), find the minimum total time to complete all tasks.

**Constraints**: 1 <= n <= 100,000. 0 <= dependencies.len() <= 200,000. 1 <= duration[i] <= 1,000.

**Key insight**: This is the critical path problem. The minimum completion time equals the length of the longest path in the DAG, where "length" is the sum of task durations along the path. Compute the earliest start time for each task using topological order, then the answer is the maximum (earliest_start + duration) across all tasks.

**Hints**:
- Process tasks in topological order (Kahn's algorithm)
- For each task `u`, its earliest start time is the maximum of `(earliest_start[p] + duration[p])` over all prerequisites `p`
- The answer is `max(earliest_start[u] + duration[u])` over all tasks

<details>
<summary>Solution</summary>

```rust
use std::collections::VecDeque;

fn minimum_completion_time(
    n: usize,
    dependencies: &[(usize, usize)], // (task, prerequisite)
    duration: &[u64],
) -> u64 {
    let mut adj = vec![vec![]; n]; // prerequisite -> dependent tasks
    let mut in_degree = vec![0usize; n];

    for &(task, prereq) in dependencies {
        adj[prereq].push(task);
        in_degree[task] += 1;
    }

    // earliest_start[u] = earliest time task u can begin
    let mut earliest_start = vec![0u64; n];

    let mut queue: VecDeque<usize> = (0..n)
        .filter(|&v| in_degree[v] == 0)
        .collect();

    while let Some(u) = queue.pop_front() {
        let u_finish = earliest_start[u] + duration[u];
        for &v in &adj[u] {
            // v can start only after u finishes
            earliest_start[v] = earliest_start[v].max(u_finish);
            in_degree[v] -= 1;
            if in_degree[v] == 0 {
                queue.push_back(v);
            }
        }
    }

    // The total completion time is the latest finish time
    (0..n)
        .map(|u| earliest_start[u] + duration[u])
        .max()
        .unwrap_or(0)
}

fn main() {
    // Task 0: 3 units, Task 1: 2 units, Task 2: 4 units, Task 3: 1 unit
    // Dependencies: 1 needs 0, 2 needs 0, 3 needs 1 and 2
    // Timeline:
    //   t=0: start 0
    //   t=3: start 1 and 2 (parallel)
    //   t=5: task 1 done
    //   t=7: task 2 done -> start 3
    //   t=8: done
    let result = minimum_completion_time(
        4,
        &[(1, 0), (2, 0), (3, 1), (3, 2)],
        &[3, 2, 4, 1],
    );
    assert_eq!(result, 8);

    // No dependencies: all tasks run in parallel, answer is the longest task
    let result = minimum_completion_time(3, &[], &[5, 3, 7]);
    assert_eq!(result, 7);

    // Linear chain: 0 -> 1 -> 2 -> 3
    let result = minimum_completion_time(
        4,
        &[(1, 0), (2, 1), (3, 2)],
        &[2, 3, 1, 4],
    );
    assert_eq!(result, 10); // 2 + 3 + 1 + 4

    // Single task
    let result = minimum_completion_time(1, &[], &[42]);
    assert_eq!(result, 42);

    // Diamond dependency
    //   0 (dur=1) -> 1 (dur=5)
    //   0 (dur=1) -> 2 (dur=2)
    //   1 -> 3 (dur=1)
    //   2 -> 3 (dur=1)
    // Critical path: 0(1) -> 1(5) -> 3(1) = 7
    let result = minimum_completion_time(
        4,
        &[(1, 0), (2, 0), (3, 1), (3, 2)],
        &[1, 5, 2, 1],
    );
    assert_eq!(result, 7);

    println!("All task_scheduling tests passed.");
}
```

**Complexity**: O(V + E) time and space. Each vertex and edge is processed exactly once during the topological sort.

</details>

---

## Problem 4: Longest Path in a DAG

Given a weighted directed acyclic graph, find the length of the longest path from any vertex to any other vertex. Edge weights can be negative.

**Constraints**: 1 <= n <= 100,000. 0 <= edges.len() <= 200,000. -10,000 <= weight <= 10,000.

**Key insight**: Unlike general graphs (where longest path is NP-hard), longest path in a DAG can be solved in O(V + E) using topological sort. Process vertices in topological order, relaxing edges in the opposite direction from shortest path (take the maximum instead of the minimum).

**Hints**:
- Initialize `dist[v] = 0` for all vertices (we want the longest path starting from any vertex)
- Process vertices in topological order
- For each edge (u, v, w), update `dist[v] = max(dist[v], dist[u] + w)`
- The answer is `max(dist[v])` over all vertices

<details>
<summary>Solution</summary>

```rust
use std::collections::VecDeque;

fn longest_path_dag(n: usize, edges: &[(usize, usize, i64)]) -> i64 {
    let mut adj: Vec<Vec<(usize, i64)>> = vec![vec![]; n];
    let mut in_degree = vec![0usize; n];

    for &(u, v, w) in edges {
        adj[u].push((v, w));
        in_degree[v] += 1;
    }

    // Topological sort via Kahn's
    let mut queue: VecDeque<usize> = (0..n)
        .filter(|&v| in_degree[v] == 0)
        .collect();

    let mut topo_order = Vec::with_capacity(n);
    while let Some(u) = queue.pop_front() {
        topo_order.push(u);
        for &(v, _) in &adj[u] {
            in_degree[v] -= 1;
            if in_degree[v] == 0 {
                queue.push_back(v);
            }
        }
    }

    // DP: longest path ending at each vertex
    let mut dist = vec![0i64; n];
    let mut global_max = 0i64;

    for &u in &topo_order {
        for &(v, w) in &adj[u] {
            if dist[u] + w > dist[v] {
                dist[v] = dist[u] + w;
            }
            global_max = global_max.max(dist[v]);
        }
    }

    global_max
}

/// Variant: longest path from a specific source vertex
fn longest_path_from_source(n: usize, edges: &[(usize, usize, i64)], source: usize) -> Vec<i64> {
    let mut adj: Vec<Vec<(usize, i64)>> = vec![vec![]; n];
    let mut in_degree = vec![0usize; n];

    for &(u, v, w) in edges {
        adj[u].push((v, w));
        in_degree[v] += 1;
    }

    let mut queue: VecDeque<usize> = (0..n)
        .filter(|&v| in_degree[v] == 0)
        .collect();

    let mut topo_order = Vec::with_capacity(n);
    while let Some(u) = queue.pop_front() {
        topo_order.push(u);
        for &(v, _) in &adj[u] {
            in_degree[v] -= 1;
            if in_degree[v] == 0 {
                queue.push_back(v);
            }
        }
    }

    let neg_inf = i64::MIN / 2;
    let mut dist = vec![neg_inf; n];
    dist[source] = 0;

    for &u in &topo_order {
        if dist[u] == neg_inf {
            continue; // unreachable from source
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
    // Simple chain: 0 --(5)--> 1 --(3)--> 2 --(7)--> 3
    let edges = vec![(0, 1, 5), (1, 2, 3), (2, 3, 7)];
    assert_eq!(longest_path_dag(4, &edges), 15);

    // Diamond: 0->(1,w=3), 0->(2,w=2), 1->(3,w=4), 2->(3,w=6)
    // Longest: 0->2->3 = 2+6 = 8
    let edges = vec![(0, 1, 3), (0, 2, 2), (1, 3, 4), (2, 3, 6)];
    assert_eq!(longest_path_dag(4, &edges), 8);

    // Negative edges
    let edges = vec![(0, 1, -2), (0, 2, 5), (1, 3, 10), (2, 3, 1)];
    // Path 0->1->3 = -2+10 = 8, Path 0->2->3 = 5+1 = 6
    assert_eq!(longest_path_dag(4, &edges), 8);

    // Single vertex
    assert_eq!(longest_path_dag(1, &[]), 0);

    // From-source variant
    let edges = vec![(0, 1, 5), (0, 2, 3), (1, 3, 2), (2, 3, 8)];
    let dist = longest_path_from_source(4, &edges, 0);
    assert_eq!(dist[0], 0);
    assert_eq!(dist[1], 5);
    assert_eq!(dist[2], 3);
    assert_eq!(dist[3], 11); // max(5+2, 3+8) = max(7, 11) = 11

    println!("All longest_path tests passed.");
}
```

**Complexity**: O(V + E) time, O(V + E) space. Topological sort is O(V + E), and the relaxation pass processes each edge exactly once.

</details>

---

## Problem 5: Parallel Job Scheduling (Minimum Number of Rounds)

You have `n` jobs with dependencies. Jobs can be executed in parallel rounds: in each round, you can run any number of jobs whose dependencies have all been completed in previous rounds. Find the minimum number of rounds needed to complete all jobs.

**Constraints**: 1 <= n <= 100,000. 0 <= dependencies.len() <= 200,000.

**Key insight**: This is equivalent to finding the length of the longest chain in the DAG (the longest path in terms of number of vertices, not edge weights). Each "layer" of Kahn's BFS is one round. Alternatively, the answer is 1 plus the maximum distance from any source (zero-indegree vertex) to any vertex, where distance counts edges.

**Hints**:
- Run Kahn's algorithm, but process the queue level by level (like BFS by layers)
- Count how many layers the BFS produces
- Equivalently, compute the longest path in the unweighted DAG (all edges have weight 1) and add 1

<details>
<summary>Solution</summary>

```rust
use std::collections::VecDeque;

fn min_rounds(n: usize, dependencies: &[(usize, usize)]) -> Option<usize> {
    let mut adj = vec![vec![]; n];
    let mut in_degree = vec![0usize; n];

    for &(task, prereq) in dependencies {
        adj[prereq].push(task);
        in_degree[task] += 1;
    }

    let mut queue: VecDeque<usize> = (0..n)
        .filter(|&v| in_degree[v] == 0)
        .collect();

    let mut rounds = 0;
    let mut processed = 0;

    while !queue.is_empty() {
        rounds += 1;
        let level_size = queue.len();

        for _ in 0..level_size {
            let u = queue.pop_front().unwrap();
            processed += 1;

            for &v in &adj[u] {
                in_degree[v] -= 1;
                if in_degree[v] == 0 {
                    queue.push_back(v);
                }
            }
        }
    }

    if processed == n {
        Some(rounds)
    } else {
        None // cycle detected
    }
}

/// Alternative: compute the "depth" of each vertex (longest path from any source)
fn min_rounds_depth(n: usize, dependencies: &[(usize, usize)]) -> Option<usize> {
    let mut adj = vec![vec![]; n];
    let mut in_degree = vec![0usize; n];

    for &(task, prereq) in dependencies {
        adj[prereq].push(task);
        in_degree[task] += 1;
    }

    let mut queue: VecDeque<usize> = (0..n)
        .filter(|&v| in_degree[v] == 0)
        .collect();

    let mut depth = vec![0usize; n];
    let mut processed = 0;

    while let Some(u) = queue.pop_front() {
        processed += 1;
        for &v in &adj[u] {
            depth[v] = depth[v].max(depth[u] + 1);
            in_degree[v] -= 1;
            if in_degree[v] == 0 {
                queue.push_back(v);
            }
        }
    }

    if processed == n {
        Some(depth.iter().max().copied().unwrap_or(0) + 1)
    } else {
        None
    }
}

fn main() {
    // Linear chain: 0 -> 1 -> 2 -> 3 => 4 rounds
    let result = min_rounds(4, &[(1, 0), (2, 1), (3, 2)]);
    assert_eq!(result, Some(4));

    // All independent: 4 tasks, no deps => 1 round
    let result = min_rounds(4, &[]);
    assert_eq!(result, Some(1));

    // Diamond: 0->1, 0->2, 1->3, 2->3
    // Round 1: {0}, Round 2: {1,2}, Round 3: {3} => 3 rounds
    let result = min_rounds(4, &[(1, 0), (2, 0), (3, 1), (3, 2)]);
    assert_eq!(result, Some(3));

    // Two independent chains: 0->1->2 and 3->4
    // Round 1: {0,3}, Round 2: {1,4}, Round 3: {2} => 3 rounds
    let result = min_rounds(5, &[(1, 0), (2, 1), (4, 3)]);
    assert_eq!(result, Some(3));

    // Cycle: should return None
    let result = min_rounds(3, &[(1, 0), (2, 1), (0, 2)]);
    assert_eq!(result, None);

    // Verify both approaches agree
    let deps = vec![(1, 0), (2, 0), (3, 1), (3, 2)];
    assert_eq!(min_rounds(4, &deps), min_rounds_depth(4, &deps));

    let deps = vec![(1, 0), (2, 1), (3, 2)];
    assert_eq!(min_rounds(4, &deps), min_rounds_depth(4, &deps));

    println!("All parallel_scheduling tests passed.");
}
```

**Complexity**: O(V + E) time, O(V + E) space. Both the layer-by-layer BFS and the depth-based approach process each vertex and edge exactly once.

</details>

---

## Complexity Summary

| Problem | Time | Space | Technique |
|---------|------|-------|-----------|
| Course Schedule | O(V + E) | O(V + E) | Kahn's cycle detection |
| Alien Dictionary | O(C) | O(C) | Graph from word comparisons + Kahn's |
| Task Scheduling | O(V + E) | O(V + E) | Topological order + critical path DP |
| Longest Path in DAG | O(V + E) | O(V + E) | Topological order + edge relaxation |
| Parallel Job Scheduling | O(V + E) | O(V + E) | Kahn's BFS by layers |

---

## Verification

Create a test project and run all solutions:

```bash
cargo new topological-sort-lab && cd topological-sort-lab
```

Paste any solution into `src/main.rs` and run:

```bash
cargo run
```

For the iterative DFS version, test with a large chain to verify stack safety:

```bash
# Build a chain of 1,000,000 nodes
# The iterative version handles this; recursive would overflow
cargo run --release
```

Run clippy for idiomatic checks:

```bash
cargo clippy -- -W clippy::pedantic
```

---

## What You Learned

- **Kahn's algorithm** (BFS with in-degree tracking) produces a topological order by repeatedly removing zero-indegree vertices. If not all vertices are removed, a cycle exists. It is inherently iterative and stack-safe.
- **DFS-based topological sort** records the reverse post-order of a depth-first traversal. It detects cycles via back edges (revisiting a vertex on the current recursion stack). The iterative variant avoids stack overflow on large graphs.
- **Lexicographically smallest topological order** requires replacing Kahn's queue with a min-heap (`BinaryHeap<Reverse<usize>>`). The DFS approach cannot easily produce lex-smallest ordering.
- **Cycle detection** is a natural byproduct of both algorithms: Kahn's checks whether all vertices were processed; DFS checks for back edges.
- **Critical path analysis** (task scheduling) uses topological order to propagate earliest start times: for each task, its earliest start is the maximum finish time of all prerequisites.
- **Longest path in a DAG** is solvable in O(V + E) -- unlike general graphs where it is NP-hard -- by relaxing edges in topological order, taking maximums instead of minimums.
- **Layer-by-layer BFS** in Kahn's algorithm directly gives the minimum number of parallel rounds needed to complete all tasks, where each BFS layer is one round.
