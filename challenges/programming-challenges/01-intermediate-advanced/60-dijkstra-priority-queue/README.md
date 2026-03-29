# 60. Dijkstra with Priority Queue

```yaml
difficulty: intermediate-advanced
languages: [go, rust]
time_estimate: 5-7 hours
tags: [graphs, shortest-path, dijkstra, priority-queue, binary-heap, a-star]
bloom_level: [apply, analyze, evaluate]
```

## Prerequisites

- Graphs: adjacency list representation, weighted directed edges
- Binary heaps: insert, extract-min, decrease-key conceptual understanding
- Generic programming: Go generics, Rust generics with trait bounds
- Basic complexity analysis: understanding O(E log V) for heap-based Dijkstra
- Hash maps for distance tracking and path reconstruction

## Learning Objectives

After completing this challenge you will be able to:

- **Implement** Dijkstra's algorithm using a binary heap priority queue
- **Reconstruct** shortest paths (not just distances) via predecessor tracking
- **Optimize** with early termination when a specific target is reached
- **Handle** edge cases: disconnected components, zero-weight edges, self-loops
- **Extend** Dijkstra into A* by incorporating a heuristic function

## The Challenge

Navigation apps, network routing protocols, and game pathfinding all rely on shortest-path algorithms. Dijkstra's algorithm is the foundational approach for graphs with non-negative edge weights. Its efficiency hinges on the priority queue implementation: a naive array gives O(V^2), but a binary heap achieves O((V + E) log V).

Implement Dijkstra's shortest path algorithm with a binary heap priority queue. Support weighted directed graphs, full path reconstruction from source to any reachable node, early termination when searching for a specific target, correct handling of disconnected components, and an A* variant that accepts a heuristic function for informed search.

## Requirements

1. Define a weighted directed graph type with generic node types. Nodes must be comparable and hashable. Support adding nodes, adding weighted edges (non-negative weights), and querying neighbors with weights.

2. Implement Dijkstra's algorithm using a min-heap priority queue. Return a distances map (source to every reachable node) and a predecessors map for path reconstruction. The priority queue must support efficient extraction of the minimum-distance unvisited node.

3. Implement `ShortestPath(source, target) -> (path, distance)` that returns the actual node sequence and total distance. Use early termination: stop as soon as the target is popped from the priority queue, since its distance is finalized.

4. Implement `ShortestPathsFrom(source) -> map[node]PathInfo` that computes shortest paths from a source to all reachable nodes in one pass. Each `PathInfo` contains the distance and the full path.

5. Handle disconnected components: nodes unreachable from the source must report infinity distance (or a sentinel value) and no path. Do not panic or loop infinitely.

6. Implement an A* variant: `AStarPath(source, target, heuristic) -> (path, distance)` where `heuristic(node) -> estimated_cost_to_target`. The priority queue orders by `distance + heuristic(node)`. The heuristic must be admissible (never overestimates) for optimality.

7. Implement both Go and Rust versions with idiomatic patterns for each language.

## Hints

<details>
<summary>Hint 1: Graph and priority queue structures</summary>

The graph stores adjacency as a map of node to list of (neighbor, weight) pairs. The priority queue holds (node, distance) pairs ordered by distance.

```go
type Edge[T comparable] struct {
    To     T
    Weight float64
}

type WeightedGraph[T comparable] struct {
    adjacency map[T][]Edge[T]
}

type pqItem[T comparable] struct {
    node T
    dist float64
}
```

</details>

<details>
<summary>Hint 2: Lazy deletion in the heap</summary>

Go's `container/heap` and Rust's `BinaryHeap` do not support decrease-key natively. Instead, push a new entry with the updated distance. When popping, skip entries whose distance exceeds the known best. This is called lazy deletion and keeps the algorithm correct.

```go
item := heap.Pop(&pq).(pqItem[T])
if item.dist > dist[item.node] {
    continue // stale entry
}
```

</details>

<details>
<summary>Hint 3: Path reconstruction via predecessors</summary>

Maintain a `prev` map: when relaxing edge (u, v), set `prev[v] = u`. To reconstruct the path to target, walk backward from target through `prev` until you reach the source, then reverse.

```rust
fn reconstruct_path<T: Clone + Eq + Hash>(
    prev: &HashMap<T, T>,
    source: &T,
    target: &T,
) -> Vec<T> {
    let mut path = vec![target.clone()];
    let mut current = target;
    while current != source {
        current = prev.get(current).unwrap();
        path.push(current.clone());
    }
    path.reverse();
    path
}
```

</details>

<details>
<summary>Hint 4: Early termination</summary>

When searching for a specific target, stop as soon as the target node is popped from the priority queue. At that point, its shortest distance is finalized because all remaining nodes in the queue have equal or greater distance.

</details>

<details>
<summary>Hint 5: A* heuristic integration</summary>

A* modifies the priority: instead of ordering by `dist[node]`, order by `dist[node] + heuristic(node)`. The actual distances stored in the dist map remain the true distances (without heuristic). Only the priority queue ordering changes. This preserves correctness when the heuristic is admissible.

```go
priority := dist[neighbor] + heuristic(neighbor)
heap.Push(&pq, pqItem[T]{node: neighbor, dist: priority})
```

</details>

## Acceptance Criteria

- [ ] Shortest distances from source to all reachable nodes are correct
- [ ] Path reconstruction returns the actual node sequence, not just distances
- [ ] Early termination produces the correct result and visits fewer nodes than full search
- [ ] Disconnected nodes report infinity distance and empty path
- [ ] Zero-weight edges are handled correctly without infinite loops
- [ ] Self-loops do not cause incorrect results
- [ ] A* with admissible heuristic produces optimal paths
- [ ] A* visits fewer nodes than plain Dijkstra for guided searches
- [ ] Both Go and Rust implementations pass equivalent test suites
- [ ] Empty graph and single-node graph edge cases handled correctly

## Resources

- [Dijkstra's Algorithm - Wikipedia](https://en.wikipedia.org/wiki/Dijkstra%27s_algorithm)
- [A* Search Algorithm - Wikipedia](https://en.wikipedia.org/wiki/A*_search_algorithm)
- [CLRS Chapter 24: Single-Source Shortest Paths](https://mitpress.mit.edu/books/introduction-to-algorithms-fourth-edition) - Dijkstra's and relaxation
- [Go container/heap](https://pkg.go.dev/container/heap) - Priority queue building block
- [Rust BinaryHeap](https://doc.rust-lang.org/std/collections/struct.BinaryHeap.html) - Max-heap with Reverse for min-heap
- [Priority Queues and Dijkstra's](https://www.redblobgames.com/pathfinding/a-star/introduction.html) - Red Blob Games visual guide
