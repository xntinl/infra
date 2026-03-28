# 4. Topological Sort with Cycle Detection

```yaml
difficulty: intermediate-advanced
languages: [go, rust]
time_estimate: 4-6 hours
tags: [graphs, dfs, topological-sort, cycle-detection, dependency-resolution]
bloom_level: [apply, analyze]
```

## Prerequisites

- Directed graphs: adjacency list representation, DFS traversal
- Generic programming: Go generics (type parameters), Rust generics with trait bounds
- Recursion and iterative stack-based equivalents
- Basic understanding of dependency resolution (package managers, build systems)

## Learning Objectives

After completing this challenge you will be able to:

- **Implement** topological sort using both DFS-based and Kahn's (BFS) algorithms
- **Detect** cycles in directed graphs and reconstruct the exact cycle path
- **Analyze** time and space complexity of graph traversal algorithms
- **Design** generic graph data structures that work with any hashable node type
- **Compare** DFS-based vs Kahn's algorithm trade-offs for different use cases

## The Challenge

Package managers resolve installation order by topologically sorting dependency graphs. Build systems determine compilation order the same way. When a cycle exists, these tools must report the exact circular dependency path so developers can fix it.

Implement a topological sort library that handles real-world graph scenarios: disconnected components, multiple valid orderings, cycles with clear error reporting, and generic node types. Provide both DFS-based (recursive + iterative) and Kahn's algorithm implementations.

## Requirements

1. Define a generic directed graph type parameterized over the node type. Nodes must be comparable and hashable. The graph supports adding nodes, adding directed edges, and querying neighbors.

2. Implement DFS-based topological sort using the three-color marking approach (white/gray/black). When a back edge is detected (gray visits gray), reconstruct and return the exact cycle path as an ordered slice of nodes.

3. Implement Kahn's algorithm (BFS-based) as an alternative. When the result contains fewer nodes than the graph, a cycle exists. Extract the cycle from remaining nodes.

4. Implement an iterative (stack-based) DFS topological sort that produces identical results to the recursive version but does not risk stack overflow on deep graphs.

5. Implement an `AllTopologicalSorts` function that enumerates every valid topological ordering using backtracking. This is exponential in the worst case but useful for small graphs.

6. Handle disconnected components: the algorithms must process all connected components, not just the one reachable from an arbitrary start node.

7. Implement both Go and Rust versions with idiomatic patterns for each language.

## Hints

<details>
<summary>Hint 1: Three-color DFS cycle detection</summary>

The three states map to: not yet visited (white), currently in the recursion stack (gray), and fully processed (black). A cycle exists when DFS encounters a gray node from another gray node. To reconstruct the cycle path, maintain a stack of the current DFS path and slice it from the repeated node.

```go
type color int
const (
    white color = iota
    gray
    black
)
```

</details>

<details>
<summary>Hint 2: Kahn's algorithm cycle extraction</summary>

Kahn's algorithm removes nodes with in-degree zero iteratively. If the sorted result has fewer nodes than the graph, the remaining nodes form one or more cycles. To find a specific cycle, pick any remaining node and follow outgoing edges through remaining nodes until you revisit one.

```go
func kahnSort[T comparable](g *Graph[T]) ([]T, error) {
    inDegree := make(map[T]int)
    queue := make([]T, 0)
    // Initialize in-degrees, seed queue with zero-degree nodes
    // Process until queue empty
    // If len(result) < len(nodes) -> cycle exists
}
```

</details>

<details>
<summary>Hint 3: Enumerating all topological sorts</summary>

Use backtracking: at each step, find all nodes with in-degree zero (among unvisited nodes). For each such node, choose it as next in the ordering, decrement in-degrees of its neighbors, recurse, then undo (increment in-degrees, unmark the node). The base case is when all nodes are placed.

```go
func allSorts[T comparable](g *Graph[T], current []T, visited map[T]bool, inDeg map[T]int, results *[][]T) {
    if len(current) == g.NodeCount() {
        result := make([]T, len(current))
        copy(result, current)
        *results = append(*results, result)
        return
    }
    for _, node := range g.Nodes() {
        if !visited[node] && inDeg[node] == 0 {
            // choose, recurse, unchoose
        }
    }
}
```

</details>

<details>
<summary>Hint 4: Iterative DFS with explicit stack</summary>

Replace the call stack with an explicit stack of `(node, neighborIndex)` pairs. When you push a node, mark it gray. When all neighbors are processed (neighborIndex exhausted), mark it black and prepend to result. A back edge is detected when you encounter a gray node while exploring neighbors.

</details>

<details>
<summary>Hint 5: Rust generics and trait bounds</summary>

Your node type needs `Eq + Hash + Clone + Debug` bounds. Use `HashMap` for adjacency lists and coloring. The `Display` trait on your error type should format the cycle path clearly.

```rust
pub struct Graph<T: Eq + Hash + Clone> {
    adjacency: HashMap<T, Vec<T>>,
}

#[derive(Debug)]
pub enum TopSortError<T: Debug> {
    CycleDetected(Vec<T>),
}
```

</details>

## Acceptance Criteria

- [ ] Generic graph type works with `string`, `int`, and custom struct node types
- [ ] DFS topological sort produces a valid ordering for all DAGs
- [ ] Kahn's algorithm produces a valid ordering for all DAGs
- [ ] Iterative DFS produces identical results to recursive DFS
- [ ] Cycle detection returns the exact cycle path (not just "cycle exists")
- [ ] Disconnected graphs are fully sorted (all components included)
- [ ] `AllTopologicalSorts` returns every valid permutation for small graphs
- [ ] Both Go and Rust implementations pass equivalent test suites
- [ ] All algorithms handle empty graphs and single-node graphs correctly
- [ ] Error messages include the cycle path formatted as `A -> B -> C -> A`

## Resources

- [Topological Sort - Wikipedia](https://en.wikipedia.org/wiki/Topological_sorting)
- [Kahn's Algorithm - Wikipedia](https://en.wikipedia.org/wiki/Topological_sorting#Kahn's_algorithm)
- [CLRS Chapter 22: Elementary Graph Algorithms](https://mitpress.mit.edu/books/introduction-to-algorithms-fourth-edition) - DFS, topological sort, and strongly connected components
- [Go Generics Tutorial](https://go.dev/doc/tutorial/generics) - Type parameters in Go
- [The Rust Programming Language: Generics](https://doc.rust-lang.org/book/ch10-01-syntax.html)
- [Dependency Resolution in Package Managers](https://research.swtch.com/version-sat) - Russ Cox on version selection
