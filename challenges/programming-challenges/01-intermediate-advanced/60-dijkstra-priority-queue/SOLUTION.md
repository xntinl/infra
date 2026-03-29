# Solution: Dijkstra with Priority Queue

## Architecture Overview

The solution centers on a generic weighted directed graph and two pathfinding algorithms:

1. **Dijkstra's Algorithm**: Uses a binary heap priority queue with lazy deletion. Each node is popped at most once at its final shortest distance. Path reconstruction walks the predecessor map backward from target to source.

2. **A* Variant**: Modifies the priority queue ordering to include a heuristic estimate of remaining distance. The actual distances remain true costs; only the queue priority changes. Optimality is guaranteed when the heuristic is admissible.

Both algorithms share the graph type and path reconstruction logic. Early termination is built into the single-target variants.

## Go Solution

```go
// dijkstra.go
package dijkstra

import (
	"container/heap"
	"math"
)

type Edge[T comparable] struct {
	To     T
	Weight float64
}

type WeightedGraph[T comparable] struct {
	adjacency map[T][]Edge[T]
	nodes     map[T]struct{}
}

func NewWeightedGraph[T comparable]() *WeightedGraph[T] {
	return &WeightedGraph[T]{
		adjacency: make(map[T][]Edge[T]),
		nodes:     make(map[T]struct{}),
	}
}

func (g *WeightedGraph[T]) AddNode(node T) {
	g.nodes[node] = struct{}{}
	if _, ok := g.adjacency[node]; !ok {
		g.adjacency[node] = nil
	}
}

func (g *WeightedGraph[T]) AddEdge(from, to T, weight float64) {
	g.AddNode(from)
	g.AddNode(to)
	g.adjacency[from] = append(g.adjacency[from], Edge[T]{To: to, Weight: weight})
}

func (g *WeightedGraph[T]) Neighbors(node T) []Edge[T] {
	return g.adjacency[node]
}

func (g *WeightedGraph[T]) Nodes() []T {
	out := make([]T, 0, len(g.nodes))
	for n := range g.nodes {
		out = append(out, n)
	}
	return out
}

type PathInfo[T comparable] struct {
	Distance float64
	Path     []T
}

// pqItem is a priority queue entry.
type pqItem[T comparable] struct {
	node     T
	priority float64
}

type priorityQueue[T comparable] []pqItem[T]

func (pq priorityQueue[T]) Len() int              { return len(pq) }
func (pq priorityQueue[T]) Less(i, j int) bool     { return pq[i].priority < pq[j].priority }
func (pq priorityQueue[T]) Swap(i, j int)          { pq[i], pq[j] = pq[j], pq[i] }
func (pq *priorityQueue[T]) Push(x interface{})     { *pq = append(*pq, x.(pqItem[T])) }
func (pq *priorityQueue[T]) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	*pq = old[:n-1]
	return item
}

func reconstructPath[T comparable](prev map[T]T, source, target T) []T {
	path := []T{target}
	current := target
	for current != source {
		p, ok := prev[current]
		if !ok {
			return nil
		}
		path = append([]T{p}, path...)
		current = p
	}
	return path
}

// ShortestPath returns the shortest path and distance from source to target.
// Uses early termination when target is popped from the queue.
func ShortestPath[T comparable](g *WeightedGraph[T], source, target T) ([]T, float64) {
	dist := make(map[T]float64)
	prev := make(map[T]T)

	for _, n := range g.Nodes() {
		dist[n] = math.Inf(1)
	}
	dist[source] = 0

	pq := &priorityQueue[T]{}
	heap.Init(pq)
	heap.Push(pq, pqItem[T]{node: source, priority: 0})

	for pq.Len() > 0 {
		item := heap.Pop(pq).(pqItem[T])
		node := item.node

		if item.priority > dist[node] {
			continue // stale entry
		}

		if node == target {
			return reconstructPath(prev, source, target), dist[target]
		}

		for _, edge := range g.Neighbors(node) {
			newDist := dist[node] + edge.Weight
			if newDist < dist[edge.To] {
				dist[edge.To] = newDist
				prev[edge.To] = node
				heap.Push(pq, pqItem[T]{node: edge.To, priority: newDist})
			}
		}
	}

	return nil, math.Inf(1)
}

// ShortestPathsFrom computes shortest paths from source to all reachable nodes.
func ShortestPathsFrom[T comparable](g *WeightedGraph[T], source T) map[T]PathInfo[T] {
	dist := make(map[T]float64)
	prev := make(map[T]T)

	for _, n := range g.Nodes() {
		dist[n] = math.Inf(1)
	}
	dist[source] = 0

	pq := &priorityQueue[T]{}
	heap.Init(pq)
	heap.Push(pq, pqItem[T]{node: source, priority: 0})

	for pq.Len() > 0 {
		item := heap.Pop(pq).(pqItem[T])
		node := item.node

		if item.priority > dist[node] {
			continue
		}

		for _, edge := range g.Neighbors(node) {
			newDist := dist[node] + edge.Weight
			if newDist < dist[edge.To] {
				dist[edge.To] = newDist
				prev[edge.To] = node
				heap.Push(pq, pqItem[T]{node: edge.To, priority: newDist})
			}
		}
	}

	result := make(map[T]PathInfo[T])
	for _, n := range g.Nodes() {
		if math.IsInf(dist[n], 1) {
			result[n] = PathInfo[T]{Distance: math.Inf(1), Path: nil}
			continue
		}
		path := reconstructPath(prev, source, n)
		if path == nil {
			path = []T{source}
		}
		result[n] = PathInfo[T]{Distance: dist[n], Path: path}
	}
	return result
}

// AStarPath performs A* search with a heuristic function.
func AStarPath[T comparable](
	g *WeightedGraph[T],
	source, target T,
	heuristic func(T) float64,
) ([]T, float64) {
	dist := make(map[T]float64)
	prev := make(map[T]T)

	for _, n := range g.Nodes() {
		dist[n] = math.Inf(1)
	}
	dist[source] = 0

	pq := &priorityQueue[T]{}
	heap.Init(pq)
	heap.Push(pq, pqItem[T]{node: source, priority: heuristic(source)})

	for pq.Len() > 0 {
		item := heap.Pop(pq).(pqItem[T])
		node := item.node

		if node == target {
			return reconstructPath(prev, source, target), dist[target]
		}

		if item.priority-heuristic(node) > dist[node] {
			continue
		}

		for _, edge := range g.Neighbors(node) {
			newDist := dist[node] + edge.Weight
			if newDist < dist[edge.To] {
				dist[edge.To] = newDist
				prev[edge.To] = node
				priority := newDist + heuristic(edge.To)
				heap.Push(pq, pqItem[T]{node: edge.To, priority: priority})
			}
		}
	}

	return nil, math.Inf(1)
}
```

```go
// dijkstra_test.go
package dijkstra

import (
	"math"
	"slices"
	"testing"
)

func buildTestGraph() *WeightedGraph[string] {
	g := NewWeightedGraph[string]()
	g.AddEdge("A", "B", 4)
	g.AddEdge("A", "C", 2)
	g.AddEdge("B", "D", 3)
	g.AddEdge("C", "B", 1)
	g.AddEdge("C", "D", 5)
	g.AddEdge("D", "E", 1)
	return g
}

func TestShortestPath(t *testing.T) {
	g := buildTestGraph()
	path, dist := ShortestPath(g, "A", "E")

	if dist != 7 {
		t.Errorf("expected distance 7, got %f", dist)
	}
	expected := []string{"A", "C", "B", "D", "E"}
	if !slices.Equal(path, expected) {
		t.Errorf("expected path %v, got %v", expected, path)
	}
}

func TestShortestPathDirect(t *testing.T) {
	g := buildTestGraph()
	path, dist := ShortestPath(g, "A", "C")

	if dist != 2 {
		t.Errorf("expected distance 2, got %f", dist)
	}
	if !slices.Equal(path, []string{"A", "C"}) {
		t.Errorf("expected [A C], got %v", path)
	}
}

func TestUnreachableTarget(t *testing.T) {
	g := buildTestGraph()
	g.AddNode("Z") // isolated node

	path, dist := ShortestPath(g, "A", "Z")

	if !math.IsInf(dist, 1) {
		t.Errorf("expected infinity, got %f", dist)
	}
	if path != nil {
		t.Errorf("expected nil path, got %v", path)
	}
}

func TestShortestPathsFrom(t *testing.T) {
	g := buildTestGraph()
	results := ShortestPathsFrom(g, "A")

	if results["B"].Distance != 3 {
		t.Errorf("A->B expected 3, got %f", results["B"].Distance)
	}
	if results["C"].Distance != 2 {
		t.Errorf("A->C expected 2, got %f", results["C"].Distance)
	}
	if results["D"].Distance != 6 {
		t.Errorf("A->D expected 6, got %f", results["D"].Distance)
	}
	if results["E"].Distance != 7 {
		t.Errorf("A->E expected 7, got %f", results["E"].Distance)
	}
}

func TestDisconnectedComponent(t *testing.T) {
	g := buildTestGraph()
	g.AddNode("X")
	g.AddEdge("X", "Y", 1)

	results := ShortestPathsFrom(g, "A")
	if !math.IsInf(results["X"].Distance, 1) {
		t.Errorf("X should be unreachable, got %f", results["X"].Distance)
	}
}

func TestZeroWeightEdge(t *testing.T) {
	g := NewWeightedGraph[string]()
	g.AddEdge("A", "B", 0)
	g.AddEdge("B", "C", 5)

	path, dist := ShortestPath(g, "A", "C")
	if dist != 5 {
		t.Errorf("expected 5, got %f", dist)
	}
	if !slices.Equal(path, []string{"A", "B", "C"}) {
		t.Errorf("expected [A B C], got %v", path)
	}
}

func TestSelfLoop(t *testing.T) {
	g := NewWeightedGraph[string]()
	g.AddEdge("A", "A", 10)
	g.AddEdge("A", "B", 5)

	path, dist := ShortestPath(g, "A", "B")
	if dist != 5 {
		t.Errorf("expected 5, got %f", dist)
	}
	if !slices.Equal(path, []string{"A", "B"}) {
		t.Errorf("expected [A B], got %v", path)
	}
}

func TestAStarPath(t *testing.T) {
	g := buildTestGraph()
	// Trivial heuristic: always returns 0 (degenerates to Dijkstra)
	zero := func(n string) float64 { return 0 }

	path, dist := AStarPath(g, "A", "E", zero)
	if dist != 7 {
		t.Errorf("expected distance 7, got %f", dist)
	}
	expected := []string{"A", "C", "B", "D", "E"}
	if !slices.Equal(path, expected) {
		t.Errorf("expected %v, got %v", expected, path)
	}
}

func TestEmptyGraph(t *testing.T) {
	g := NewWeightedGraph[string]()
	g.AddNode("A")
	results := ShortestPathsFrom(g, "A")
	if results["A"].Distance != 0 {
		t.Errorf("distance to self should be 0, got %f", results["A"].Distance)
	}
}

func TestSingleEdge(t *testing.T) {
	g := NewWeightedGraph[int]()
	g.AddEdge(1, 2, 7)

	path, dist := ShortestPath(g, 1, 2)
	if dist != 7 {
		t.Errorf("expected 7, got %f", dist)
	}
	if !slices.Equal(path, []int{1, 2}) {
		t.Errorf("expected [1 2], got %v", path)
	}
}
```

## Running the Go Solution

```bash
mkdir -p dijkstra && cd dijkstra
go mod init dijkstra
# Place dijkstra.go and dijkstra_test.go in the directory
go test -v -count=1 ./...
```

### Expected Output

```
=== RUN   TestShortestPath
--- PASS: TestShortestPath
=== RUN   TestShortestPathDirect
--- PASS: TestShortestPathDirect
=== RUN   TestUnreachableTarget
--- PASS: TestUnreachableTarget
=== RUN   TestShortestPathsFrom
--- PASS: TestShortestPathsFrom
=== RUN   TestDisconnectedComponent
--- PASS: TestDisconnectedComponent
=== RUN   TestZeroWeightEdge
--- PASS: TestZeroWeightEdge
=== RUN   TestSelfLoop
--- PASS: TestSelfLoop
=== RUN   TestAStarPath
--- PASS: TestAStarPath
=== RUN   TestEmptyGraph
--- PASS: TestEmptyGraph
=== RUN   TestSingleEdge
--- PASS: TestSingleEdge
PASS
```

## Rust Solution

```rust
// src/lib.rs
use std::cmp::Ordering;
use std::collections::{BinaryHeap, HashMap};
use std::hash::Hash;

#[derive(Debug, Clone)]
pub struct Edge<T> {
    pub to: T,
    pub weight: f64,
}

pub struct WeightedGraph<T: Eq + Hash + Clone> {
    adjacency: HashMap<T, Vec<Edge<T>>>,
}

impl<T: Eq + Hash + Clone> WeightedGraph<T> {
    pub fn new() -> Self {
        WeightedGraph {
            adjacency: HashMap::new(),
        }
    }

    pub fn add_node(&mut self, node: T) {
        self.adjacency.entry(node).or_insert_with(Vec::new);
    }

    pub fn add_edge(&mut self, from: T, to: T, weight: f64) {
        self.add_node(to.clone());
        self.adjacency
            .entry(from)
            .or_insert_with(Vec::new)
            .push(Edge { to, weight });
    }

    pub fn neighbors(&self, node: &T) -> &[Edge<T>] {
        self.adjacency.get(node).map(|v| v.as_slice()).unwrap_or(&[])
    }

    pub fn nodes(&self) -> Vec<T> {
        self.adjacency.keys().cloned().collect()
    }
}

#[derive(Debug, Clone)]
pub struct PathInfo<T> {
    pub distance: f64,
    pub path: Option<Vec<T>>,
}

#[derive(Clone, PartialEq)]
struct State<T> {
    cost: f64,
    node: T,
}

impl<T: PartialEq> Eq for State<T> {}

impl<T: PartialEq> Ord for State<T> {
    fn cmp(&self, other: &Self) -> Ordering {
        other.cost.partial_cmp(&self.cost).unwrap_or(Ordering::Equal)
    }
}

impl<T: PartialEq> PartialOrd for State<T> {
    fn partial_cmp(&self, other: &Self) -> Option<Ordering> {
        Some(self.cmp(other))
    }
}

fn reconstruct_path<T: Eq + Hash + Clone>(prev: &HashMap<T, T>, source: &T, target: &T) -> Vec<T> {
    let mut path = vec![target.clone()];
    let mut current = target;
    while current != source {
        let Some(p) = prev.get(current) else {
            return Vec::new();
        };
        path.push(p.clone());
        current = p;
    }
    path.reverse();
    path
}

/// Dijkstra shortest path from source to target with early termination.
pub fn shortest_path<T: Eq + Hash + Clone>(
    graph: &WeightedGraph<T>,
    source: &T,
    target: &T,
) -> (Option<Vec<T>>, f64) {
    let mut dist: HashMap<T, f64> = HashMap::new();
    let mut prev: HashMap<T, T> = HashMap::new();
    let mut heap = BinaryHeap::new();

    dist.insert(source.clone(), 0.0);
    heap.push(State { cost: 0.0, node: source.clone() });

    while let Some(State { cost, node }) = heap.pop() {
        if &node == target {
            let path = reconstruct_path(&prev, source, target);
            return (Some(path), cost);
        }

        if cost > *dist.get(&node).unwrap_or(&f64::INFINITY) {
            continue;
        }

        for edge in graph.neighbors(&node) {
            let new_dist = cost + edge.weight;
            let current_dist = *dist.get(&edge.to).unwrap_or(&f64::INFINITY);
            if new_dist < current_dist {
                dist.insert(edge.to.clone(), new_dist);
                prev.insert(edge.to.clone(), node.clone());
                heap.push(State { cost: new_dist, node: edge.to.clone() });
            }
        }
    }

    (None, f64::INFINITY)
}

/// Dijkstra from source to all reachable nodes.
pub fn shortest_paths_from<T: Eq + Hash + Clone>(
    graph: &WeightedGraph<T>,
    source: &T,
) -> HashMap<T, PathInfo<T>> {
    let mut dist: HashMap<T, f64> = HashMap::new();
    let mut prev: HashMap<T, T> = HashMap::new();
    let mut heap = BinaryHeap::new();

    dist.insert(source.clone(), 0.0);
    heap.push(State { cost: 0.0, node: source.clone() });

    while let Some(State { cost, node }) = heap.pop() {
        if cost > *dist.get(&node).unwrap_or(&f64::INFINITY) {
            continue;
        }
        for edge in graph.neighbors(&node) {
            let new_dist = cost + edge.weight;
            let current_dist = *dist.get(&edge.to).unwrap_or(&f64::INFINITY);
            if new_dist < current_dist {
                dist.insert(edge.to.clone(), new_dist);
                prev.insert(edge.to.clone(), node.clone());
                heap.push(State { cost: new_dist, node: edge.to.clone() });
            }
        }
    }

    let mut results = HashMap::new();
    for node in graph.nodes() {
        let distance = *dist.get(&node).unwrap_or(&f64::INFINITY);
        let path = if distance.is_infinite() {
            None
        } else if &node == source {
            Some(vec![source.clone()])
        } else {
            let p = reconstruct_path(&prev, source, &node);
            if p.is_empty() { None } else { Some(p) }
        };
        results.insert(node, PathInfo { distance, path });
    }
    results
}

/// A* search with a heuristic function.
pub fn astar_path<T, H>(
    graph: &WeightedGraph<T>,
    source: &T,
    target: &T,
    heuristic: H,
) -> (Option<Vec<T>>, f64)
where
    T: Eq + Hash + Clone,
    H: Fn(&T) -> f64,
{
    let mut dist: HashMap<T, f64> = HashMap::new();
    let mut prev: HashMap<T, T> = HashMap::new();
    let mut heap = BinaryHeap::new();

    dist.insert(source.clone(), 0.0);
    heap.push(State {
        cost: heuristic(source),
        node: source.clone(),
    });

    while let Some(State { cost: _, node }) = heap.pop() {
        if &node == target {
            let path = reconstruct_path(&prev, source, target);
            return (Some(path), *dist.get(target).unwrap());
        }

        let current_dist = *dist.get(&node).unwrap_or(&f64::INFINITY);

        for edge in graph.neighbors(&node) {
            let new_dist = current_dist + edge.weight;
            let known_dist = *dist.get(&edge.to).unwrap_or(&f64::INFINITY);
            if new_dist < known_dist {
                dist.insert(edge.to.clone(), new_dist);
                prev.insert(edge.to.clone(), node.clone());
                let priority = new_dist + heuristic(&edge.to);
                heap.push(State { cost: priority, node: edge.to.clone() });
            }
        }
    }

    (None, f64::INFINITY)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn build_test_graph() -> WeightedGraph<String> {
        let mut g = WeightedGraph::new();
        g.add_edge("A".into(), "B".into(), 4.0);
        g.add_edge("A".into(), "C".into(), 2.0);
        g.add_edge("B".into(), "D".into(), 3.0);
        g.add_edge("C".into(), "B".into(), 1.0);
        g.add_edge("C".into(), "D".into(), 5.0);
        g.add_edge("D".into(), "E".into(), 1.0);
        g
    }

    #[test]
    fn shortest_path_basic() {
        let g = build_test_graph();
        let (path, dist) = shortest_path(&g, &"A".into(), &"E".into());
        assert_eq!(dist, 7.0);
        assert_eq!(
            path.unwrap(),
            vec!["A".to_string(), "C".into(), "B".into(), "D".into(), "E".into()]
        );
    }

    #[test]
    fn shortest_path_direct() {
        let g = build_test_graph();
        let (path, dist) = shortest_path(&g, &"A".into(), &"C".into());
        assert_eq!(dist, 2.0);
        assert_eq!(path.unwrap(), vec!["A".to_string(), "C".into()]);
    }

    #[test]
    fn unreachable_target() {
        let mut g = build_test_graph();
        g.add_node("Z".into());
        let (path, dist) = shortest_path(&g, &"A".into(), &"Z".into());
        assert!(dist.is_infinite());
        assert!(path.is_none());
    }

    #[test]
    fn all_shortest_paths() {
        let g = build_test_graph();
        let results = shortest_paths_from(&g, &"A".into());
        assert_eq!(results[&"B".to_string()].distance, 3.0);
        assert_eq!(results[&"C".to_string()].distance, 2.0);
        assert_eq!(results[&"D".to_string()].distance, 6.0);
        assert_eq!(results[&"E".to_string()].distance, 7.0);
    }

    #[test]
    fn disconnected_component() {
        let mut g = build_test_graph();
        g.add_node("X".into());
        g.add_edge("X".into(), "Y".into(), 1.0);
        let results = shortest_paths_from(&g, &"A".into());
        assert!(results[&"X".to_string()].distance.is_infinite());
    }

    #[test]
    fn zero_weight_edge() {
        let mut g = WeightedGraph::new();
        g.add_edge("A".into(), "B".into(), 0.0);
        g.add_edge("B".into(), "C".into(), 5.0);
        let (path, dist) = shortest_path(&g, &"A".into(), &"C".into());
        assert_eq!(dist, 5.0);
        assert_eq!(
            path.unwrap(),
            vec!["A".to_string(), "B".into(), "C".into()]
        );
    }

    #[test]
    fn self_loop() {
        let mut g: WeightedGraph<String> = WeightedGraph::new();
        g.add_edge("A".into(), "A".into(), 10.0);
        g.add_edge("A".into(), "B".into(), 5.0);
        let (path, dist) = shortest_path(&g, &"A".into(), &"B".into());
        assert_eq!(dist, 5.0);
        assert_eq!(path.unwrap(), vec!["A".to_string(), "B".into()]);
    }

    #[test]
    fn astar_with_zero_heuristic() {
        let g = build_test_graph();
        let (path, dist) = astar_path(&g, &"A".into(), &"E".into(), |_| 0.0);
        assert_eq!(dist, 7.0);
        assert_eq!(
            path.unwrap(),
            vec!["A".to_string(), "C".into(), "B".into(), "D".into(), "E".into()]
        );
    }

    #[test]
    fn single_node() {
        let mut g: WeightedGraph<String> = WeightedGraph::new();
        g.add_node("A".into());
        let results = shortest_paths_from(&g, &"A".into());
        assert_eq!(results[&"A".to_string()].distance, 0.0);
    }

    #[test]
    fn integer_nodes() {
        let mut g = WeightedGraph::new();
        g.add_edge(1, 2, 3.0);
        g.add_edge(2, 3, 4.0);
        let (path, dist) = shortest_path(&g, &1, &3);
        assert_eq!(dist, 7.0);
        assert_eq!(path.unwrap(), vec![1, 2, 3]);
    }
}
```

## Running the Rust Solution

```bash
cargo new dijkstra_pq --lib && cd dijkstra_pq
# Replace src/lib.rs with the code above
cargo test -- --nocapture
```

### Expected Output

```
running 10 tests
test tests::shortest_path_basic ... ok
test tests::shortest_path_direct ... ok
test tests::unreachable_target ... ok
test tests::all_shortest_paths ... ok
test tests::disconnected_component ... ok
test tests::zero_weight_edge ... ok
test tests::self_loop ... ok
test tests::astar_with_zero_heuristic ... ok
test tests::single_node ... ok
test tests::integer_nodes ... ok

test result: ok. 10 passed; 0 failed; 0 ignored
```

## Design Decisions

1. **Lazy deletion over decrease-key**: Neither Go's `container/heap` nor Rust's `BinaryHeap` supports decrease-key. Pushing duplicate entries and skipping stale ones on pop is simpler and has the same asymptotic complexity O((V + E) log V). The constant factor is slightly worse due to extra heap entries, but the implementation is far simpler.

2. **Float distances with infinity sentinel**: Using `float64` / `f64` with `Inf` as the sentinel for unreachable nodes is natural and avoids special-case integer sentinels like `MaxInt`. The trade-off is floating-point comparison subtleties, but for shortest-path distances this is safe.

3. **Separate single-target vs all-targets functions**: `ShortestPath` uses early termination for efficiency when only one target matters. `ShortestPathsFrom` runs Dijkstra to completion. Sharing the core logic but splitting the interface keeps both use cases efficient.

4. **A* as a thin wrapper**: The A* variant only changes the priority queue ordering. All internal distance tracking uses true distances (without heuristic). This separation is critical: storing `distance + heuristic` as the actual distance would break relaxation correctness.

5. **Generic node type**: Both Go and Rust solutions use generics, allowing the graph to work with strings, integers, or coordinates. This makes the A* variant testable with coordinate-based heuristics.

## Common Mistakes

- **Not skipping stale heap entries**: Without the `if item.priority > dist[node]` check, the algorithm processes the same node multiple times at suboptimal distances, producing incorrect results.
- **Storing heuristic in the distance map**: The distance map must track true costs. Only the priority queue uses `cost + heuristic`. Mixing these produces suboptimal or incorrect paths.
- **Negative weights**: Dijkstra does not handle negative edge weights. If a graph has negative weights, use Bellman-Ford instead. The algorithm silently produces wrong results with negative weights.
- **Forgetting to initialize all nodes**: Nodes not in the initial distance map default to infinity. If the `Nodes()` method misses some, those nodes become permanently unreachable even if edges point to them.

## Performance Notes

| Operation | Time Complexity | Space Complexity |
|-----------|----------------|-----------------|
| Dijkstra (binary heap) | O((V + E) log V) | O(V + E) |
| Dijkstra (early termination) | O((V + E) log V) worst case, often much less | O(V + E) |
| A* (binary heap) | O((V + E) log V) worst case | O(V + E) |
| Path reconstruction | O(V) | O(V) |

A* with a good heuristic can dramatically reduce the number of visited nodes. For grid-based pathfinding, Manhattan or Euclidean distance heuristics typically reduce explored nodes by 50-90% compared to plain Dijkstra.
