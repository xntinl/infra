# Solution: Topological Sort with Cycle Detection

## Architecture Overview

The solution is structured around a generic `Graph[T]` type that stores an adjacency list. Three independent sorting strategies operate on this graph:

1. **DFS-based** (recursive and iterative): Uses three-color marking to detect back edges. Produces a reverse-postorder traversal which is a valid topological ordering.
2. **Kahn's algorithm**: Iteratively removes zero-in-degree nodes. Cycle detection comes from comparing result size to graph size.
3. **All orderings**: Backtracking enumeration of every valid topological sort.

Each algorithm returns either the sorted order or an error containing the exact cycle path.

## Go Solution

```go
// topsort.go
package topsort

import (
	"fmt"
	"strings"
)

// Graph represents a directed graph with generic node type.
type Graph[T comparable] struct {
	adjacency map[T][]T
	nodes     map[T]struct{}
}

// NewGraph creates an empty directed graph.
func NewGraph[T comparable]() *Graph[T] {
	return &Graph[T]{
		adjacency: make(map[T][]T),
		nodes:     make(map[T]struct{}),
	}
}

// AddNode adds a node to the graph.
func (g *Graph[T]) AddNode(node T) {
	g.nodes[node] = struct{}{}
	if _, ok := g.adjacency[node]; !ok {
		g.adjacency[node] = nil
	}
}

// AddEdge adds a directed edge from -> to. Both nodes are added if absent.
func (g *Graph[T]) AddEdge(from, to T) {
	g.AddNode(from)
	g.AddNode(to)
	g.adjacency[from] = append(g.adjacency[from], to)
}

// Nodes returns all nodes in deterministic iteration order is not guaranteed;
// callers that need determinism should sort externally.
func (g *Graph[T]) Nodes() []T {
	out := make([]T, 0, len(g.nodes))
	for n := range g.nodes {
		out = append(out, n)
	}
	return out
}

// NodeCount returns the number of nodes.
func (g *Graph[T]) NodeCount() int {
	return len(g.nodes)
}

// Neighbors returns the outgoing neighbors of a node.
func (g *Graph[T]) Neighbors(node T) []T {
	return g.adjacency[node]
}

// CycleError reports a cycle found during topological sort.
type CycleError[T comparable] struct {
	Cycle []T
}

func (e *CycleError[T]) Error() string {
	parts := make([]string, len(e.Cycle))
	for i, n := range e.Cycle {
		parts[i] = fmt.Sprintf("%v", n)
	}
	return "cycle detected: " + strings.Join(parts, " -> ")
}

type color int

const (
	white color = iota
	gray
	black
)

// DFSTopologicalSort performs a recursive DFS-based topological sort.
// Returns the sorted order or a CycleError with the exact cycle path.
func DFSTopologicalSort[T comparable](g *Graph[T]) ([]T, error) {
	colors := make(map[T]color)
	parent := make(map[T]T)
	var result []T

	for _, n := range g.Nodes() {
		colors[n] = white
	}

	var dfs func(node T) error
	dfs = func(node T) error {
		colors[node] = gray
		for _, neighbor := range g.Neighbors(node) {
			switch colors[neighbor] {
			case white:
				parent[neighbor] = node
				if err := dfs(neighbor); err != nil {
					return err
				}
			case gray:
				cycle := extractCycle(parent, node, neighbor)
				return &CycleError[T]{Cycle: cycle}
			}
		}
		colors[node] = black
		result = append([]T{node}, result...)
		return nil
	}

	for _, n := range g.Nodes() {
		if colors[n] == white {
			if err := dfs(n); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

func extractCycle[T comparable](parent map[T]T, from, to T) []T {
	cycle := []T{to}
	current := from
	for current != to {
		cycle = append([]T{current}, cycle...)
		current = parent[current]
	}
	cycle = append([]T{to}, cycle...)
	// Reverse so it reads: to -> ... -> from -> to
	// Actually cycle is already: to -> ... -> from -> to after prepend
	return cycle
}

// IterativeDFSTopologicalSort performs stack-based DFS topological sort.
func IterativeDFSTopologicalSort[T comparable](g *Graph[T]) ([]T, error) {
	type frame struct {
		node     T
		neighIdx int
	}

	colors := make(map[T]color)
	for _, n := range g.Nodes() {
		colors[n] = white
	}

	var result []T

	for _, startNode := range g.Nodes() {
		if colors[startNode] != white {
			continue
		}

		stack := []frame{{node: startNode, neighIdx: 0}}
		colors[startNode] = gray

		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			neighbors := g.Neighbors(top.node)

			if top.neighIdx >= len(neighbors) {
				colors[top.node] = black
				result = append([]T{top.node}, result...)
				stack = stack[:len(stack)-1]
				continue
			}

			neighbor := neighbors[top.neighIdx]
			top.neighIdx++

			switch colors[neighbor] {
			case white:
				colors[neighbor] = gray
				stack = append(stack, frame{node: neighbor, neighIdx: 0})
			case gray:
				cycle := []T{neighbor}
				for i := len(stack) - 1; i >= 0; i-- {
					cycle = append(cycle, stack[i].node)
					if stack[i].node == neighbor {
						break
					}
				}
				reverseSlice(cycle)
				return nil, &CycleError[T]{Cycle: cycle}
			}
		}
	}
	return result, nil
}

func reverseSlice[T any](s []T) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

// KahnTopologicalSort performs Kahn's algorithm (BFS-based) topological sort.
func KahnTopologicalSort[T comparable](g *Graph[T]) ([]T, error) {
	inDegree := make(map[T]int)
	for _, n := range g.Nodes() {
		if _, ok := inDegree[n]; !ok {
			inDegree[n] = 0
		}
		for _, neighbor := range g.Neighbors(n) {
			inDegree[neighbor]++
		}
	}

	var queue []T
	for _, n := range g.Nodes() {
		if inDegree[n] == 0 {
			queue = append(queue, n)
		}
	}

	var result []T
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		result = append(result, node)

		for _, neighbor := range g.Neighbors(node) {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	if len(result) != g.NodeCount() {
		cycle := findCycleInRemaining(g, inDegree)
		return nil, &CycleError[T]{Cycle: cycle}
	}
	return result, nil
}

func findCycleInRemaining[T comparable](g *Graph[T], inDegree map[T]int) []T {
	remaining := make(map[T]bool)
	for n, deg := range inDegree {
		if deg > 0 {
			remaining[n] = true
		}
	}

	var start T
	for n := range remaining {
		start = n
		break
	}

	visited := make(map[T]bool)
	path := []T{start}
	visited[start] = true
	current := start

	for {
		found := false
		for _, neighbor := range g.Neighbors(current) {
			if remaining[neighbor] {
				if visited[neighbor] {
					idx := 0
					for i, n := range path {
						if n == neighbor {
							idx = i
							break
						}
					}
					cycle := append(path[idx:], neighbor)
					return cycle
				}
				visited[neighbor] = true
				path = append(path, neighbor)
				current = neighbor
				found = true
				break
			}
		}
		if !found {
			break
		}
	}
	return path
}

// AllTopologicalSorts returns every valid topological ordering.
// Warning: exponential time/space for dense graphs.
func AllTopologicalSorts[T comparable](g *Graph[T]) ([][]T, error) {
	inDegree := make(map[T]int)
	for _, n := range g.Nodes() {
		if _, ok := inDegree[n]; !ok {
			inDegree[n] = 0
		}
		for _, neighbor := range g.Neighbors(n) {
			inDegree[neighbor]++
		}
	}

	visited := make(map[T]bool)
	var current []T
	var results [][]T

	var backtrack func()
	backtrack = func() {
		if len(current) == g.NodeCount() {
			result := make([]T, len(current))
			copy(result, current)
			results = append(results, result)
			return
		}

		for _, node := range g.Nodes() {
			if !visited[node] && inDegree[node] == 0 {
				visited[node] = true
				current = append(current, node)
				for _, neighbor := range g.Neighbors(node) {
					inDegree[neighbor]--
				}

				backtrack()

				for _, neighbor := range g.Neighbors(node) {
					inDegree[neighbor]++
				}
				current = current[:len(current)-1]
				visited[node] = false
			}
		}
	}

	backtrack()

	if len(results) == 0 && g.NodeCount() > 0 {
		return nil, fmt.Errorf("no valid topological ordering exists (graph contains a cycle)")
	}
	return results, nil
}
```

```go
// topsort_test.go
package topsort

import (
	"slices"
	"testing"
)

func isValidTopologicalOrder[T comparable](g *Graph[T], order []T) bool {
	if len(order) != g.NodeCount() {
		return false
	}
	position := make(map[T]int)
	for i, n := range order {
		position[n] = i
	}
	for _, n := range g.Nodes() {
		for _, neighbor := range g.Neighbors(n) {
			if position[n] >= position[neighbor] {
				return false
			}
		}
	}
	return true
}

func buildDAG() *Graph[string] {
	g := NewGraph[string]()
	g.AddEdge("A", "B")
	g.AddEdge("A", "C")
	g.AddEdge("B", "D")
	g.AddEdge("C", "D")
	g.AddEdge("D", "E")
	return g
}

func buildCyclicGraph() *Graph[string] {
	g := NewGraph[string]()
	g.AddEdge("A", "B")
	g.AddEdge("B", "C")
	g.AddEdge("C", "A")
	return g
}

func buildDisconnectedDAG() *Graph[string] {
	g := NewGraph[string]()
	g.AddEdge("A", "B")
	g.AddEdge("C", "D")
	return g
}

func TestDFSTopologicalSort_DAG(t *testing.T) {
	g := buildDAG()
	result, err := DFSTopologicalSort(g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !isValidTopologicalOrder(g, result) {
		t.Errorf("invalid topological order: %v", result)
	}
}

func TestDFSTopologicalSort_Cycle(t *testing.T) {
	g := buildCyclicGraph()
	_, err := DFSTopologicalSort(g)
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
	cycleErr, ok := err.(*CycleError[string])
	if !ok {
		t.Fatalf("expected CycleError, got %T", err)
	}
	if len(cycleErr.Cycle) < 3 {
		t.Errorf("cycle path too short: %v", cycleErr.Cycle)
	}
	t.Logf("detected cycle: %s", cycleErr.Error())
}

func TestIterativeDFS_DAG(t *testing.T) {
	g := buildDAG()
	result, err := IterativeDFSTopologicalSort(g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !isValidTopologicalOrder(g, result) {
		t.Errorf("invalid topological order: %v", result)
	}
}

func TestIterativeDFS_Cycle(t *testing.T) {
	g := buildCyclicGraph()
	_, err := IterativeDFSTopologicalSort(g)
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
}

func TestKahnTopologicalSort_DAG(t *testing.T) {
	g := buildDAG()
	result, err := KahnTopologicalSort(g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !isValidTopologicalOrder(g, result) {
		t.Errorf("invalid topological order: %v", result)
	}
}

func TestKahnTopologicalSort_Cycle(t *testing.T) {
	g := buildCyclicGraph()
	_, err := KahnTopologicalSort(g)
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
}

func TestDisconnectedComponents(t *testing.T) {
	g := buildDisconnectedDAG()
	result, err := DFSTopologicalSort(g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 4 {
		t.Errorf("expected 4 nodes, got %d: %v", len(result), result)
	}
	if !isValidTopologicalOrder(g, result) {
		t.Errorf("invalid topological order: %v", result)
	}
}

func TestAllTopologicalSorts(t *testing.T) {
	g := NewGraph[string]()
	g.AddEdge("A", "C")
	g.AddEdge("B", "C")

	results, err := AllTopologicalSorts(g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Two valid orderings: [A,B,C] and [B,A,C]
	if len(results) != 2 {
		t.Errorf("expected 2 orderings, got %d", len(results))
	}
	for _, r := range results {
		if !isValidTopologicalOrder(g, r) {
			t.Errorf("invalid ordering: %v", r)
		}
	}
}

func TestEmptyGraph(t *testing.T) {
	g := NewGraph[string]()
	result, err := DFSTopologicalSort(g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty result, got %v", result)
	}
}

func TestSingleNode(t *testing.T) {
	g := NewGraph[int]()
	g.AddNode(42)
	result, err := DFSTopologicalSort(g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !slices.Equal(result, []int{42}) {
		t.Errorf("expected [42], got %v", result)
	}
}

func TestIntegerNodes(t *testing.T) {
	g := NewGraph[int]()
	g.AddEdge(1, 2)
	g.AddEdge(1, 3)
	g.AddEdge(2, 4)
	g.AddEdge(3, 4)

	result, err := KahnTopologicalSort(g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !isValidTopologicalOrder(g, result) {
		t.Errorf("invalid topological order: %v", result)
	}
}
```

## Running the Go Solution

```bash
mkdir -p topsort && cd topsort
go mod init topsort
# Place topsort.go and topsort_test.go in the directory
go test -v -count=1 ./...
```

### Expected Output

```
=== RUN   TestDFSTopologicalSort_DAG
--- PASS: TestDFSTopologicalSort_DAG
=== RUN   TestDFSTopologicalSort_Cycle
    topsort_test.go:75: detected cycle: cycle detected: A -> B -> C -> A
--- PASS: TestDFSTopologicalSort_Cycle
=== RUN   TestIterativeDFS_DAG
--- PASS: TestIterativeDFS_DAG
=== RUN   TestIterativeDFS_Cycle
--- PASS: TestIterativeDFS_Cycle
=== RUN   TestKahnTopologicalSort_DAG
--- PASS: TestKahnTopologicalSort_DAG
=== RUN   TestKahnTopologicalSort_Cycle
--- PASS: TestKahnTopologicalSort_Cycle
=== RUN   TestDisconnectedComponents
--- PASS: TestDisconnectedComponents
=== RUN   TestAllTopologicalSorts
--- PASS: TestAllTopologicalSorts
=== RUN   TestEmptyGraph
--- PASS: TestEmptyGraph
=== RUN   TestSingleNode
--- PASS: TestSingleNode
=== RUN   TestIntegerNodes
--- PASS: TestIntegerNodes
PASS
```

## Rust Solution

```rust
// src/lib.rs
use std::collections::{HashMap, HashSet, VecDeque};
use std::fmt::{self, Debug, Display};
use std::hash::Hash;

#[derive(Debug)]
pub enum TopSortError<T: Debug> {
    CycleDetected(Vec<T>),
}

impl<T: Debug + Display> fmt::Display for TopSortError<T> {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            TopSortError::CycleDetected(cycle) => {
                write!(f, "cycle detected: ")?;
                for (i, node) in cycle.iter().enumerate() {
                    if i > 0 {
                        write!(f, " -> ")?;
                    }
                    write!(f, "{}", node)?;
                }
                Ok(())
            }
        }
    }
}

impl<T: Debug + Display> std::error::Error for TopSortError<T> {}

pub struct Graph<T: Eq + Hash + Clone> {
    adjacency: HashMap<T, Vec<T>>,
}

impl<T: Eq + Hash + Clone + Debug> Graph<T> {
    pub fn new() -> Self {
        Graph {
            adjacency: HashMap::new(),
        }
    }

    pub fn add_node(&mut self, node: T) {
        self.adjacency.entry(node).or_insert_with(Vec::new);
    }

    pub fn add_edge(&mut self, from: T, to: T) {
        self.add_node(to.clone());
        self.adjacency
            .entry(from)
            .or_insert_with(Vec::new)
            .push(to);
    }

    pub fn nodes(&self) -> Vec<T> {
        self.adjacency.keys().cloned().collect()
    }

    pub fn node_count(&self) -> usize {
        self.adjacency.len()
    }

    pub fn neighbors(&self, node: &T) -> &[T] {
        self.adjacency.get(node).map(|v| v.as_slice()).unwrap_or(&[])
    }

    fn in_degrees(&self) -> HashMap<T, usize> {
        let mut in_deg: HashMap<T, usize> = HashMap::new();
        for node in self.adjacency.keys() {
            in_deg.entry(node.clone()).or_insert(0);
        }
        for neighbors in self.adjacency.values() {
            for neighbor in neighbors {
                *in_deg.entry(neighbor.clone()).or_insert(0) += 1;
            }
        }
        in_deg
    }
}

#[derive(Clone, Copy, PartialEq)]
enum Color {
    White,
    Gray,
    Black,
}

/// DFS-based topological sort with cycle detection.
pub fn dfs_topological_sort<T>(graph: &Graph<T>) -> Result<Vec<T>, TopSortError<T>>
where
    T: Eq + Hash + Clone + Debug,
{
    let mut colors: HashMap<T, Color> = graph
        .nodes()
        .into_iter()
        .map(|n| (n, Color::White))
        .collect();
    let mut result = Vec::new();
    let mut path = Vec::new();

    fn dfs<T: Eq + Hash + Clone + Debug>(
        node: &T,
        graph: &Graph<T>,
        colors: &mut HashMap<T, Color>,
        result: &mut Vec<T>,
        path: &mut Vec<T>,
    ) -> Result<(), TopSortError<T>> {
        colors.insert(node.clone(), Color::Gray);
        path.push(node.clone());

        for neighbor in graph.neighbors(node) {
            match colors.get(neighbor) {
                Some(Color::White) | None => {
                    dfs(neighbor, graph, colors, result, path)?;
                }
                Some(Color::Gray) => {
                    let idx = path.iter().position(|n| n == neighbor).unwrap();
                    let mut cycle: Vec<T> = path[idx..].to_vec();
                    cycle.push(neighbor.clone());
                    return Err(TopSortError::CycleDetected(cycle));
                }
                Some(Color::Black) => {}
            }
        }

        path.pop();
        colors.insert(node.clone(), Color::Black);
        result.insert(0, node.clone());
        Ok(())
    }

    for node in graph.nodes() {
        if colors.get(&node) == Some(&Color::White) {
            dfs(&node, graph, &mut colors, &mut result, &mut path)?;
        }
    }

    Ok(result)
}

/// Kahn's algorithm (BFS-based) topological sort.
pub fn kahn_topological_sort<T>(graph: &Graph<T>) -> Result<Vec<T>, TopSortError<T>>
where
    T: Eq + Hash + Clone + Debug,
{
    let mut in_deg = graph.in_degrees();
    let mut queue: VecDeque<T> = VecDeque::new();

    for (node, &deg) in &in_deg {
        if deg == 0 {
            queue.push_back(node.clone());
        }
    }

    let mut result = Vec::new();

    while let Some(node) = queue.pop_front() {
        result.push(node.clone());
        for neighbor in graph.neighbors(&node) {
            if let Some(deg) = in_deg.get_mut(neighbor) {
                *deg -= 1;
                if *deg == 0 {
                    queue.push_back(neighbor.clone());
                }
            }
        }
    }

    if result.len() != graph.node_count() {
        let remaining: HashSet<T> = graph
            .nodes()
            .into_iter()
            .filter(|n| !result.contains(n))
            .collect();

        let start = remaining.iter().next().unwrap().clone();
        let mut cycle = vec![start.clone()];
        let mut current = start.clone();
        let mut visited = HashSet::new();
        visited.insert(current.clone());

        loop {
            let mut found = false;
            for neighbor in graph.neighbors(&current) {
                if remaining.contains(neighbor) {
                    if visited.contains(neighbor) {
                        let idx = cycle.iter().position(|n| n == neighbor).unwrap();
                        let mut final_cycle: Vec<T> = cycle[idx..].to_vec();
                        final_cycle.push(neighbor.clone());
                        return Err(TopSortError::CycleDetected(final_cycle));
                    }
                    visited.insert(neighbor.clone());
                    cycle.push(neighbor.clone());
                    current = neighbor.clone();
                    found = true;
                    break;
                }
            }
            if !found {
                break;
            }
        }

        return Err(TopSortError::CycleDetected(cycle));
    }

    Ok(result)
}

/// Enumerate all valid topological orderings via backtracking.
pub fn all_topological_sorts<T>(graph: &Graph<T>) -> Result<Vec<Vec<T>>, TopSortError<T>>
where
    T: Eq + Hash + Clone + Debug + Ord,
{
    let mut in_deg = graph.in_degrees();
    let mut visited: HashSet<T> = HashSet::new();
    let mut current = Vec::new();
    let mut results = Vec::new();
    let mut nodes: Vec<T> = graph.nodes();
    nodes.sort();

    fn backtrack<T: Eq + Hash + Clone + Debug + Ord>(
        nodes: &[T],
        graph: &Graph<T>,
        visited: &mut HashSet<T>,
        in_deg: &mut HashMap<T, usize>,
        current: &mut Vec<T>,
        results: &mut Vec<Vec<T>>,
        total: usize,
    ) {
        if current.len() == total {
            results.push(current.clone());
            return;
        }

        for node in nodes {
            if !visited.contains(node) && in_deg.get(node).copied().unwrap_or(0) == 0 {
                visited.insert(node.clone());
                current.push(node.clone());
                for neighbor in graph.neighbors(node) {
                    *in_deg.get_mut(neighbor).unwrap() -= 1;
                }

                backtrack(nodes, graph, visited, in_deg, current, results, total);

                for neighbor in graph.neighbors(node) {
                    *in_deg.get_mut(neighbor).unwrap() += 1;
                }
                current.pop();
                visited.remove(node);
            }
        }
    }

    let total = graph.node_count();
    backtrack(
        &nodes,
        graph,
        &mut visited,
        &mut in_deg,
        &mut current,
        &mut results,
        total,
    );

    if results.is_empty() && graph.node_count() > 0 {
        return Err(TopSortError::CycleDetected(vec![]));
    }

    Ok(results)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn is_valid_topological_order<T: Eq + Hash + Clone + Debug>(
        graph: &Graph<T>,
        order: &[T],
    ) -> bool {
        if order.len() != graph.node_count() {
            return false;
        }
        let position: HashMap<&T, usize> =
            order.iter().enumerate().map(|(i, n)| (n, i)).collect();
        for node in graph.nodes() {
            for neighbor in graph.neighbors(&node) {
                if position[&node] >= position[&neighbor] {
                    return false;
                }
            }
        }
        true
    }

    fn build_dag() -> Graph<String> {
        let mut g = Graph::new();
        g.add_edge("A".into(), "B".into());
        g.add_edge("A".into(), "C".into());
        g.add_edge("B".into(), "D".into());
        g.add_edge("C".into(), "D".into());
        g.add_edge("D".into(), "E".into());
        g
    }

    fn build_cyclic() -> Graph<String> {
        let mut g = Graph::new();
        g.add_edge("A".into(), "B".into());
        g.add_edge("B".into(), "C".into());
        g.add_edge("C".into(), "A".into());
        g
    }

    #[test]
    fn dfs_sort_dag() {
        let g = build_dag();
        let result = dfs_topological_sort(&g).unwrap();
        assert!(is_valid_topological_order(&g, &result));
    }

    #[test]
    fn dfs_sort_cycle() {
        let g = build_cyclic();
        let err = dfs_topological_sort(&g).unwrap_err();
        match err {
            TopSortError::CycleDetected(cycle) => {
                assert!(cycle.len() >= 3, "cycle too short: {:?}", cycle);
            }
        }
    }

    #[test]
    fn kahn_sort_dag() {
        let g = build_dag();
        let result = kahn_topological_sort(&g).unwrap();
        assert!(is_valid_topological_order(&g, &result));
    }

    #[test]
    fn kahn_sort_cycle() {
        let g = build_cyclic();
        assert!(kahn_topological_sort(&g).is_err());
    }

    #[test]
    fn all_sorts_diamond() {
        let mut g: Graph<String> = Graph::new();
        g.add_edge("A".into(), "C".into());
        g.add_edge("B".into(), "C".into());
        let results = all_topological_sorts(&g).unwrap();
        assert_eq!(results.len(), 2);
        for r in &results {
            assert!(is_valid_topological_order(&g, r));
        }
    }

    #[test]
    fn empty_graph() {
        let g: Graph<String> = Graph::new();
        let result = dfs_topological_sort(&g).unwrap();
        assert!(result.is_empty());
    }

    #[test]
    fn single_node() {
        let mut g = Graph::new();
        g.add_node(42);
        let result = dfs_topological_sort(&g).unwrap();
        assert_eq!(result, vec![42]);
    }
}
```

## Running the Rust Solution

```bash
cargo new topsort --lib && cd topsort
# Replace src/lib.rs with the code above
cargo test -- --nocapture
```

### Expected Output

```
running 7 tests
test tests::dfs_sort_dag ... ok
test tests::dfs_sort_cycle ... ok
test tests::kahn_sort_dag ... ok
test tests::kahn_sort_cycle ... ok
test tests::all_sorts_diamond ... ok
test tests::empty_graph ... ok
test tests::single_node ... ok

test result: ok. 7 passed; 0 failed; 0 ignored
```

## Design Decisions

1. **Generic node type**: Both Go and Rust solutions use generics so the graph works with strings, integers, or custom types without code duplication. Go requires `comparable`, Rust requires `Eq + Hash + Clone`.

2. **Three-color scheme over two-color**: Two colors (visited/unvisited) cannot distinguish "currently being processed" from "fully processed." Three colors detect back edges precisely, which is necessary for correct cycle detection in directed graphs.

3. **Cycle path reconstruction**: Rather than just reporting "cycle exists," the path is reconstructed by walking back through the DFS path or parent map. This mirrors what real tools like `go mod` report.

4. **Kahn's cycle detection**: Kahn's algorithm naturally detects cycles when the sorted result is smaller than the graph. Extracting the actual cycle path requires a second pass through remaining nodes.

5. **All orderings via backtracking**: This is intentionally exponential. It is useful for verification (testing that your single-sort output is among valid orderings) and for small dependency graphs where you want to enumerate options.

## Common Mistakes

- **Forgetting disconnected components**: Starting DFS from a single node misses nodes in other components. Always iterate over all nodes and start DFS from unvisited ones.
- **Confusing back edges with cross edges**: In directed graphs, only back edges (gray-to-gray in DFS tree) indicate cycles. Cross edges (gray-to-black) are valid in DAGs.
- **Not reversing DFS postorder**: DFS postorder gives reverse topological order. Prepending to result or reversing at the end is required.
- **Map iteration order in Go**: Go maps iterate in random order, so topological sort output may vary between runs. This is correct (multiple valid orderings exist) but can confuse tests that compare exact sequences. Test validity of ordering, not exact equality.

## Performance Notes

| Algorithm | Time | Space |
|-----------|------|-------|
| DFS topological sort | O(V + E) | O(V) |
| Kahn's algorithm | O(V + E) | O(V) |
| Iterative DFS | O(V + E) | O(V) |
| All topological sorts | O(V! * V) worst case | O(V! * V) |

For graphs with millions of nodes, use the iterative DFS to avoid stack overflow. Kahn's algorithm is preferable when you need to process nodes in-order (streaming) since it naturally produces output incrementally.

## Going Further

- Implement parallel topological sort: nodes at the same "depth" (same position in Kahn's BFS layers) can be processed concurrently
- Add weighted edges and implement critical path analysis (longest path in DAG)
- Implement Tarjan's algorithm for strongly connected components, which generalizes cycle detection
- Build a dependency resolver that handles version constraints (like Go modules or Cargo)
- Implement incremental topological sort that efficiently updates the ordering when edges are added or removed
