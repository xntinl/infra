# 70. Graph BFS/DFS and Shortest Path

```yaml
difficulty: intermediate-advanced
languages: [go, rust]
time_estimate: 5-7 hours
tags: [graphs, bfs, dfs, shortest-path, connected-components, bipartite, cycle-detection]
bloom_level: [apply, analyze, evaluate]
```

## Prerequisites

- Graph fundamentals: vertices, edges, directed vs undirected, adjacency list representation
- Queue and stack data structures for BFS and DFS respectively
- Generic programming: Go generics, Rust generics with trait bounds
- Basic set operations: visited tracking, component labeling
- Recursion and iterative equivalents

## Learning Objectives

After completing this challenge you will be able to:

- **Implement** BFS and DFS (both iterative and recursive) on generic graph types
- **Compute** shortest paths in unweighted graphs using BFS with path reconstruction
- **Identify** connected components in undirected graphs and strongly connected behavior
- **Determine** whether a graph is bipartite using two-coloring during BFS/DFS
- **Detect** cycles in both directed graphs (back-edge detection) and undirected graphs (parent tracking)

## The Challenge

Graph traversals are the building blocks of nearly every graph algorithm. BFS explores layer by layer, giving shortest paths in unweighted graphs. DFS explores as deep as possible, enabling cycle detection and topological ordering. Together, they solve problems from social network analysis (connected components) to map coloring (bipartiteness) to dependency validation (cycle detection).

Implement a comprehensive graph traversal library. Support BFS and DFS (iterative and recursive) on a generic adjacency-list graph. Build on these traversals to solve: BFS-based shortest path with path reconstruction, connected component identification, bipartite checking, and cycle detection for both directed and undirected graphs.

## Requirements

1. Define a generic graph type supporting both directed and undirected modes. Nodes must be comparable and hashable. Support adding nodes, adding edges (directed or undirected), and querying neighbors. The graph mode (directed/undirected) is set at construction time.

2. Implement BFS traversal from a source node. Return nodes in BFS order (level by level). Track parent pointers during traversal for path reconstruction.

3. Implement DFS traversal in both recursive and iterative (stack-based) forms. Return nodes in DFS pre-order. Both variants must produce valid DFS orderings.

4. Implement `ShortestPath(source, target) -> (path, distance)` using BFS. Returns the shortest path as a node sequence and the hop count. For unreachable targets, return an appropriate sentinel.

5. Implement `ConnectedComponents() -> [][]node` for undirected graphs. Each component is a list of nodes. Use BFS or DFS to flood-fill unvisited nodes into components.

6. Implement `IsBipartite() -> (bool, coloring)` that attempts to two-color the graph. If successful, return true and the coloring map. If a conflict is found (adjacent nodes with the same color), return false and the conflicting edge.

7. Implement `HasCycle() -> (bool, cycle_path)` for both directed and undirected graphs. For directed graphs, use DFS with three-color marking to detect back edges. For undirected graphs, use DFS with parent tracking (a neighbor that is visited and is not the parent indicates a cycle). Return the cycle path when found.

8. Implement both Go and Rust versions with idiomatic patterns for each language.

## Hints

<details>
<summary>Hint 1: Graph type with mode flag</summary>

A single graph type handles both directed and undirected modes. For undirected, `AddEdge(a, b)` internally adds both `a->b` and `b->a`.

```go
type GraphMode int
const (
    Directed GraphMode = iota
    Undirected
)

type Graph[T comparable] struct {
    adjacency map[T][]T
    mode      GraphMode
}
```

</details>

<details>
<summary>Hint 2: BFS with parent tracking</summary>

Use a queue and a visited map. Additionally, store the parent of each visited node. To reconstruct the shortest path, walk backward from target through parents to source, then reverse.

```go
parent := make(map[T]T)
queue := []T{source}
visited := map[T]bool{source: true}

for len(queue) > 0 {
    node := queue[0]
    queue = queue[1:]
    for _, neighbor := range g.Neighbors(node) {
        if !visited[neighbor] {
            visited[neighbor] = true
            parent[neighbor] = node
            queue = append(queue, neighbor)
        }
    }
}
```

</details>

<details>
<summary>Hint 3: Bipartite checking with two colors</summary>

Assign colors 0 and 1. Start BFS/DFS from an uncolored node with color 0. For each neighbor, if uncolored, assign the opposite color. If already colored with the same color as the current node, the graph is not bipartite.

```rust
enum Color { A, B }

fn opposite(c: &Color) -> Color {
    match c {
        Color::A => Color::B,
        Color::B => Color::A,
    }
}
```

</details>

<details>
<summary>Hint 4: Cycle detection in undirected graphs</summary>

During DFS on an undirected graph, track the parent of each node. If you encounter a visited neighbor that is not your parent, you found a cycle. To extract the cycle path, use the DFS stack or parent map to trace from both ends to the common ancestor.

</details>

<details>
<summary>Hint 5: Directed cycle detection with three colors</summary>

Use white (unvisited), gray (in current DFS path), black (fully processed). A back edge (current node points to a gray node) means a cycle exists. Maintain the current DFS path to reconstruct the cycle.

```rust
#[derive(Clone, PartialEq)]
enum State { White, Gray, Black }
```

</details>

## Acceptance Criteria

- [ ] BFS returns nodes in correct level-order from the source
- [ ] DFS (recursive and iterative) return valid DFS pre-order traversals
- [ ] BFS shortest path returns minimum hop count and correct path in unweighted graphs
- [ ] Unreachable targets report no path and infinite/sentinel distance
- [ ] Connected components correctly partition an undirected graph
- [ ] Single-node components are included in the result
- [ ] Bipartite check correctly identifies bipartite and non-bipartite graphs
- [ ] Bipartite returns valid two-coloring when the graph is bipartite
- [ ] Cycle detection finds cycles in directed graphs (back-edge method)
- [ ] Cycle detection finds cycles in undirected graphs (parent-tracking method)
- [ ] Acyclic graphs correctly report no cycle
- [ ] Both Go and Rust implementations pass equivalent test suites
- [ ] Empty graph and single-node graph edge cases handled correctly

## Resources

- [Breadth-First Search - Wikipedia](https://en.wikipedia.org/wiki/Breadth-first_search)
- [Depth-First Search - Wikipedia](https://en.wikipedia.org/wiki/Depth-first_search)
- [Bipartite Graph - Wikipedia](https://en.wikipedia.org/wiki/Bipartite_graph)
- [CLRS Chapter 22: Elementary Graph Algorithms](https://mitpress.mit.edu/books/introduction-to-algorithms-fourth-edition) - BFS, DFS, connected components
- [The Rust Programming Language: Collections](https://doc.rust-lang.org/book/ch08-00-common-collections.html) - VecDeque for BFS
- [Graph Traversal Visualizations](https://www.redblobgames.com/pathfinding/grids/graphs.html) - Red Blob Games
