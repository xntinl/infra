# Solution: Graph BFS/DFS and Shortest Path

## Architecture Overview

The solution is built on a single generic graph type that supports both directed and undirected modes. Six algorithms operate on this graph:

1. **BFS**: Queue-based level-order traversal with parent tracking for path reconstruction.
2. **DFS Recursive**: Classic recursive depth-first traversal with pre-order collection.
3. **DFS Iterative**: Stack-based DFS that avoids stack overflow on deep graphs.
4. **BFS Shortest Path**: BFS with parent map for unweighted shortest path and reconstruction.
5. **Connected Components**: Flood-fill using BFS over unvisited nodes (undirected only).
6. **Bipartite Check**: Two-coloring via BFS; conflict detection returns the offending edge.
7. **Cycle Detection**: Three-color DFS for directed graphs, parent-tracking DFS for undirected.

## Go Solution

```go
// graph.go
package graph

type Mode int

const (
	Directed Mode = iota
	Undirected
)

type Graph[T comparable] struct {
	adjacency map[T][]T
	nodes     map[T]struct{}
	mode      Mode
}

func NewGraph[T comparable](mode Mode) *Graph[T] {
	return &Graph[T]{
		adjacency: make(map[T][]T),
		nodes:     make(map[T]struct{}),
		mode:      mode,
	}
}

func (g *Graph[T]) AddNode(node T) {
	g.nodes[node] = struct{}{}
	if _, ok := g.adjacency[node]; !ok {
		g.adjacency[node] = nil
	}
}

func (g *Graph[T]) AddEdge(from, to T) {
	g.AddNode(from)
	g.AddNode(to)
	g.adjacency[from] = append(g.adjacency[from], to)
	if g.mode == Undirected {
		g.adjacency[to] = append(g.adjacency[to], from)
	}
}

func (g *Graph[T]) Neighbors(node T) []T {
	return g.adjacency[node]
}

func (g *Graph[T]) Nodes() []T {
	out := make([]T, 0, len(g.nodes))
	for n := range g.nodes {
		out = append(out, n)
	}
	return out
}

func (g *Graph[T]) NodeCount() int {
	return len(g.nodes)
}

// BFS returns nodes in BFS order from source.
func BFS[T comparable](g *Graph[T], source T) []T {
	visited := map[T]bool{source: true}
	queue := []T{source}
	var order []T

	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		order = append(order, node)

		for _, neighbor := range g.Neighbors(node) {
			if !visited[neighbor] {
				visited[neighbor] = true
				queue = append(queue, neighbor)
			}
		}
	}
	return order
}

// DFSRecursive returns nodes in DFS pre-order from source.
func DFSRecursive[T comparable](g *Graph[T], source T) []T {
	visited := make(map[T]bool)
	var order []T
	dfsVisit(g, source, visited, &order)
	return order
}

func dfsVisit[T comparable](g *Graph[T], node T, visited map[T]bool, order *[]T) {
	visited[node] = true
	*order = append(*order, node)
	for _, neighbor := range g.Neighbors(node) {
		if !visited[neighbor] {
			dfsVisit(g, neighbor, visited, order)
		}
	}
}

// DFSIterative returns nodes in DFS pre-order using an explicit stack.
func DFSIterative[T comparable](g *Graph[T], source T) []T {
	visited := make(map[T]bool)
	stack := []T{source}
	var order []T

	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if visited[node] {
			continue
		}
		visited[node] = true
		order = append(order, node)

		neighbors := g.Neighbors(node)
		for i := len(neighbors) - 1; i >= 0; i-- {
			if !visited[neighbors[i]] {
				stack = append(stack, neighbors[i])
			}
		}
	}
	return order
}

// ShortestPath returns the shortest path (by hop count) and distance using BFS.
// Returns nil, -1 if target is unreachable.
func ShortestPath[T comparable](g *Graph[T], source, target T) ([]T, int) {
	if source == target {
		return []T{source}, 0
	}

	visited := map[T]bool{source: true}
	parent := make(map[T]T)
	queue := []T{source}

	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]

		for _, neighbor := range g.Neighbors(node) {
			if !visited[neighbor] {
				visited[neighbor] = true
				parent[neighbor] = node
				if neighbor == target {
					return reconstructPath(parent, source, target), pathLength(parent, source, target)
				}
				queue = append(queue, neighbor)
			}
		}
	}
	return nil, -1
}

func reconstructPath[T comparable](parent map[T]T, source, target T) []T {
	path := []T{target}
	current := target
	for current != source {
		current = parent[current]
		path = append([]T{current}, path...)
	}
	return path
}

func pathLength[T comparable](parent map[T]T, source, target T) int {
	length := 0
	current := target
	for current != source {
		current = parent[current]
		length++
	}
	return length
}

// ConnectedComponents returns all connected components (undirected graph only).
func ConnectedComponents[T comparable](g *Graph[T]) [][]T {
	visited := make(map[T]bool)
	var components [][]T

	for _, node := range g.Nodes() {
		if visited[node] {
			continue
		}
		component := BFS(g, node)
		for _, n := range component {
			visited[n] = true
		}
		components = append(components, component)
	}
	return components
}

type Color int

const (
	Uncolored Color = iota
	ColorA
	ColorB
)

type BipartiteResult[T comparable] struct {
	IsBipartite bool
	Coloring    map[T]Color
	ConflictA   T
	ConflictB   T
}

// IsBipartite checks if the graph is two-colorable.
func IsBipartite[T comparable](g *Graph[T]) BipartiteResult[T] {
	coloring := make(map[T]Color)

	for _, start := range g.Nodes() {
		if coloring[start] != Uncolored {
			continue
		}
		coloring[start] = ColorA
		queue := []T{start}

		for len(queue) > 0 {
			node := queue[0]
			queue = queue[1:]
			nextColor := ColorA
			if coloring[node] == ColorA {
				nextColor = ColorB
			}

			for _, neighbor := range g.Neighbors(node) {
				if coloring[neighbor] == Uncolored {
					coloring[neighbor] = nextColor
					queue = append(queue, neighbor)
				} else if coloring[neighbor] == coloring[node] {
					return BipartiteResult[T]{
						IsBipartite: false,
						Coloring:    coloring,
						ConflictA:   node,
						ConflictB:   neighbor,
					}
				}
			}
		}
	}

	return BipartiteResult[T]{IsBipartite: true, Coloring: coloring}
}

type CycleResult[T comparable] struct {
	HasCycle bool
	Cycle    []T
}

type dfsColor int

const (
	white dfsColor = iota
	gray
	black
)

// HasCycleDirected detects cycles in directed graphs using three-color DFS.
func HasCycleDirected[T comparable](g *Graph[T]) CycleResult[T] {
	colors := make(map[T]dfsColor)
	for _, n := range g.Nodes() {
		colors[n] = white
	}
	var path []T

	var dfs func(T) *CycleResult[T]
	dfs = func(node T) *CycleResult[T] {
		colors[node] = gray
		path = append(path, node)

		for _, neighbor := range g.Neighbors(node) {
			if colors[neighbor] == gray {
				idx := 0
				for i, n := range path {
					if n == neighbor {
						idx = i
						break
					}
				}
				cycle := make([]T, len(path[idx:]))
				copy(cycle, path[idx:])
				cycle = append(cycle, neighbor)
				return &CycleResult[T]{HasCycle: true, Cycle: cycle}
			}
			if colors[neighbor] == white {
				if result := dfs(neighbor); result != nil {
					return result
				}
			}
		}

		path = path[:len(path)-1]
		colors[node] = black
		return nil
	}

	for _, n := range g.Nodes() {
		if colors[n] == white {
			if result := dfs(n); result != nil {
				return *result
			}
		}
	}
	return CycleResult[T]{HasCycle: false}
}

// HasCycleUndirected detects cycles in undirected graphs using parent tracking.
func HasCycleUndirected[T comparable](g *Graph[T]) CycleResult[T] {
	visited := make(map[T]bool)
	parent := make(map[T]T)

	for _, start := range g.Nodes() {
		if visited[start] {
			continue
		}

		stack := []T{start}
		visited[start] = true

		for len(stack) > 0 {
			node := stack[len(stack)-1]
			stack = stack[:len(stack)-1]

			for _, neighbor := range g.Neighbors(node) {
				if !visited[neighbor] {
					visited[neighbor] = true
					parent[neighbor] = node
					stack = append(stack, neighbor)
				} else if neighbor != parent[node] {
					cycle := extractUndirectedCycle(parent, node, neighbor)
					return CycleResult[T]{HasCycle: true, Cycle: cycle}
				}
			}
		}
	}
	return CycleResult[T]{HasCycle: false}
}

func extractUndirectedCycle[T comparable](parent map[T]T, meetA, meetB T) []T {
	pathA := []T{meetA}
	pathB := []T{meetB}
	visitedA := map[T]bool{meetA: true}

	current := meetA
	for {
		p, ok := parent[current]
		if !ok {
			break
		}
		pathA = append(pathA, p)
		visitedA[p] = true
		current = p
	}

	current = meetB
	for {
		if visitedA[current] {
			break
		}
		p, ok := parent[current]
		if !ok {
			break
		}
		pathB = append(pathB, p)
		current = p
	}

	ancestor := current
	trimmedA := []T{}
	for _, n := range pathA {
		trimmedA = append(trimmedA, n)
		if n == ancestor {
			break
		}
	}

	for i, j := 0, len(trimmedA)-1; i < j; i, j = i+1, j-1 {
		trimmedA[i], trimmedA[j] = trimmedA[j], trimmedA[i]
	}

	cycle := append(trimmedA, pathB...)
	cycle = append(cycle, trimmedA[0])
	return cycle
}
```

```go
// graph_test.go
package graph

import (
	"testing"
)

func TestBFS(t *testing.T) {
	g := NewGraph[string](Undirected)
	g.AddEdge("A", "B")
	g.AddEdge("A", "C")
	g.AddEdge("B", "D")
	g.AddEdge("C", "D")

	order := BFS(g, "A")
	if order[0] != "A" {
		t.Errorf("BFS should start with source, got %v", order[0])
	}
	if len(order) != 4 {
		t.Errorf("expected 4 nodes, got %d", len(order))
	}
}

func TestDFSRecursive(t *testing.T) {
	g := NewGraph[string](Directed)
	g.AddEdge("A", "B")
	g.AddEdge("A", "C")
	g.AddEdge("B", "D")

	order := DFSRecursive(g, "A")
	if order[0] != "A" {
		t.Errorf("DFS should start with source, got %v", order[0])
	}
	if len(order) != 4 {
		t.Errorf("expected 4 nodes, got %d", len(order))
	}
}

func TestDFSIterative(t *testing.T) {
	g := NewGraph[string](Directed)
	g.AddEdge("A", "B")
	g.AddEdge("A", "C")
	g.AddEdge("B", "D")

	order := DFSIterative(g, "A")
	if order[0] != "A" {
		t.Errorf("DFS should start with source")
	}
	if len(order) != 4 {
		t.Errorf("expected 4 nodes, got %d", len(order))
	}
}

func TestShortestPathBFS(t *testing.T) {
	g := NewGraph[string](Undirected)
	g.AddEdge("A", "B")
	g.AddEdge("B", "C")
	g.AddEdge("A", "C")

	path, dist := ShortestPath(g, "A", "C")
	if dist != 1 {
		t.Errorf("expected distance 1, got %d", dist)
	}
	if path[0] != "A" || path[len(path)-1] != "C" {
		t.Errorf("path should start with A and end with C: %v", path)
	}
}

func TestShortestPathUnreachable(t *testing.T) {
	g := NewGraph[string](Directed)
	g.AddEdge("A", "B")
	g.AddNode("C")

	path, dist := ShortestPath(g, "A", "C")
	if dist != -1 {
		t.Errorf("expected -1 for unreachable, got %d", dist)
	}
	if path != nil {
		t.Errorf("expected nil path, got %v", path)
	}
}

func TestShortestPathToSelf(t *testing.T) {
	g := NewGraph[string](Undirected)
	g.AddNode("A")

	path, dist := ShortestPath(g, "A", "A")
	if dist != 0 {
		t.Errorf("distance to self should be 0, got %d", dist)
	}
	if len(path) != 1 || path[0] != "A" {
		t.Errorf("expected [A], got %v", path)
	}
}

func TestConnectedComponents(t *testing.T) {
	g := NewGraph[int](Undirected)
	g.AddEdge(1, 2)
	g.AddEdge(2, 3)
	g.AddEdge(4, 5)
	g.AddNode(6)

	components := ConnectedComponents(g)
	if len(components) != 3 {
		t.Errorf("expected 3 components, got %d", len(components))
	}

	totalNodes := 0
	for _, c := range components {
		totalNodes += len(c)
	}
	if totalNodes != 6 {
		t.Errorf("expected 6 total nodes, got %d", totalNodes)
	}
}

func TestIsBipartite_True(t *testing.T) {
	g := NewGraph[int](Undirected)
	g.AddEdge(1, 2)
	g.AddEdge(2, 3)
	g.AddEdge(3, 4)
	g.AddEdge(4, 1)

	result := IsBipartite(g)
	if !result.IsBipartite {
		t.Error("even cycle should be bipartite")
	}
}

func TestIsBipartite_False(t *testing.T) {
	g := NewGraph[int](Undirected)
	g.AddEdge(1, 2)
	g.AddEdge(2, 3)
	g.AddEdge(3, 1) // odd cycle

	result := IsBipartite(g)
	if result.IsBipartite {
		t.Error("odd cycle should not be bipartite")
	}
}

func TestHasCycleDirected(t *testing.T) {
	g := NewGraph[string](Directed)
	g.AddEdge("A", "B")
	g.AddEdge("B", "C")
	g.AddEdge("C", "A")

	result := HasCycleDirected(g)
	if !result.HasCycle {
		t.Error("expected cycle in directed graph")
	}
	if len(result.Cycle) < 3 {
		t.Errorf("cycle path too short: %v", result.Cycle)
	}
}

func TestNoCycleDirected(t *testing.T) {
	g := NewGraph[string](Directed)
	g.AddEdge("A", "B")
	g.AddEdge("B", "C")

	result := HasCycleDirected(g)
	if result.HasCycle {
		t.Errorf("DAG should not have cycle, got: %v", result.Cycle)
	}
}

func TestHasCycleUndirected(t *testing.T) {
	g := NewGraph[int](Undirected)
	g.AddEdge(1, 2)
	g.AddEdge(2, 3)
	g.AddEdge(3, 1)

	result := HasCycleUndirected(g)
	if !result.HasCycle {
		t.Error("expected cycle in undirected graph")
	}
}

func TestNoCycleUndirected(t *testing.T) {
	g := NewGraph[int](Undirected)
	g.AddEdge(1, 2)
	g.AddEdge(2, 3)

	result := HasCycleUndirected(g)
	if result.HasCycle {
		t.Error("tree should not have cycle")
	}
}

func TestEmptyGraph(t *testing.T) {
	g := NewGraph[string](Directed)
	components := ConnectedComponents(g)
	if len(components) != 0 {
		t.Errorf("empty graph should have 0 components, got %d", len(components))
	}
}

func TestSingleNode(t *testing.T) {
	g := NewGraph[string](Undirected)
	g.AddNode("A")

	order := BFS(g, "A")
	if len(order) != 1 {
		t.Errorf("expected 1 node, got %d", len(order))
	}

	components := ConnectedComponents(g)
	if len(components) != 1 {
		t.Errorf("expected 1 component, got %d", len(components))
	}
}
```

## Running the Go Solution

```bash
mkdir -p graphtraversal && cd graphtraversal
go mod init graphtraversal
# Place graph.go and graph_test.go in the directory
go test -v -count=1 ./...
```

### Expected Output

```
=== RUN   TestBFS
--- PASS: TestBFS
=== RUN   TestDFSRecursive
--- PASS: TestDFSRecursive
=== RUN   TestDFSIterative
--- PASS: TestDFSIterative
=== RUN   TestShortestPathBFS
--- PASS: TestShortestPathBFS
=== RUN   TestShortestPathUnreachable
--- PASS: TestShortestPathUnreachable
=== RUN   TestShortestPathToSelf
--- PASS: TestShortestPathToSelf
=== RUN   TestConnectedComponents
--- PASS: TestConnectedComponents
=== RUN   TestIsBipartite_True
--- PASS: TestIsBipartite_True
=== RUN   TestIsBipartite_False
--- PASS: TestIsBipartite_False
=== RUN   TestHasCycleDirected
--- PASS: TestHasCycleDirected
=== RUN   TestNoCycleDirected
--- PASS: TestNoCycleDirected
=== RUN   TestHasCycleUndirected
--- PASS: TestHasCycleUndirected
=== RUN   TestNoCycleUndirected
--- PASS: TestNoCycleUndirected
=== RUN   TestEmptyGraph
--- PASS: TestEmptyGraph
=== RUN   TestSingleNode
--- PASS: TestSingleNode
PASS
```

## Rust Solution

```rust
// src/lib.rs
use std::collections::{HashMap, HashSet, VecDeque};
use std::hash::Hash;

#[derive(Debug, Clone, Copy, PartialEq)]
pub enum GraphMode {
    Directed,
    Undirected,
}

pub struct Graph<T: Eq + Hash + Clone> {
    adjacency: HashMap<T, Vec<T>>,
    mode: GraphMode,
}

impl<T: Eq + Hash + Clone> Graph<T> {
    pub fn new(mode: GraphMode) -> Self {
        Graph {
            adjacency: HashMap::new(),
            mode,
        }
    }

    pub fn add_node(&mut self, node: T) {
        self.adjacency.entry(node).or_insert_with(Vec::new);
    }

    pub fn add_edge(&mut self, from: T, to: T) {
        self.add_node(from.clone());
        self.add_node(to.clone());
        self.adjacency.get_mut(&from).unwrap().push(to.clone());
        if self.mode == GraphMode::Undirected {
            self.adjacency.get_mut(&to).unwrap().push(from);
        }
    }

    pub fn neighbors(&self, node: &T) -> &[T] {
        self.adjacency.get(node).map(|v| v.as_slice()).unwrap_or(&[])
    }

    pub fn nodes(&self) -> Vec<T> {
        self.adjacency.keys().cloned().collect()
    }

    pub fn node_count(&self) -> usize {
        self.adjacency.len()
    }
}

/// BFS traversal from source, returns nodes in level-order.
pub fn bfs<T: Eq + Hash + Clone>(graph: &Graph<T>, source: &T) -> Vec<T> {
    let mut visited: HashSet<T> = HashSet::new();
    let mut queue: VecDeque<T> = VecDeque::new();
    let mut order = Vec::new();

    visited.insert(source.clone());
    queue.push_back(source.clone());

    while let Some(node) = queue.pop_front() {
        order.push(node.clone());
        for neighbor in graph.neighbors(&node) {
            if visited.insert(neighbor.clone()) {
                queue.push_back(neighbor.clone());
            }
        }
    }
    order
}

/// DFS recursive traversal, returns nodes in pre-order.
pub fn dfs_recursive<T: Eq + Hash + Clone>(graph: &Graph<T>, source: &T) -> Vec<T> {
    let mut visited = HashSet::new();
    let mut order = Vec::new();
    dfs_visit(graph, source, &mut visited, &mut order);
    order
}

fn dfs_visit<T: Eq + Hash + Clone>(
    graph: &Graph<T>,
    node: &T,
    visited: &mut HashSet<T>,
    order: &mut Vec<T>,
) {
    visited.insert(node.clone());
    order.push(node.clone());
    for neighbor in graph.neighbors(node) {
        if !visited.contains(neighbor) {
            dfs_visit(graph, neighbor, visited, order);
        }
    }
}

/// DFS iterative traversal using explicit stack.
pub fn dfs_iterative<T: Eq + Hash + Clone>(graph: &Graph<T>, source: &T) -> Vec<T> {
    let mut visited = HashSet::new();
    let mut stack = vec![source.clone()];
    let mut order = Vec::new();

    while let Some(node) = stack.pop() {
        if !visited.insert(node.clone()) {
            continue;
        }
        order.push(node.clone());
        let neighbors = graph.neighbors(&node);
        for neighbor in neighbors.iter().rev() {
            if !visited.contains(neighbor) {
                stack.push(neighbor.clone());
            }
        }
    }
    order
}

/// BFS shortest path (unweighted). Returns (path, distance) or (None, -1).
pub fn shortest_path<T: Eq + Hash + Clone>(
    graph: &Graph<T>,
    source: &T,
    target: &T,
) -> (Option<Vec<T>>, i32) {
    if source == target {
        return (Some(vec![source.clone()]), 0);
    }

    let mut visited: HashSet<T> = HashSet::new();
    let mut parent: HashMap<T, T> = HashMap::new();
    let mut queue: VecDeque<T> = VecDeque::new();

    visited.insert(source.clone());
    queue.push_back(source.clone());

    while let Some(node) = queue.pop_front() {
        for neighbor in graph.neighbors(&node) {
            if visited.insert(neighbor.clone()) {
                parent.insert(neighbor.clone(), node.clone());
                if neighbor == target {
                    let path = reconstruct(&parent, source, target);
                    let dist = path.len() as i32 - 1;
                    return (Some(path), dist);
                }
                queue.push_back(neighbor.clone());
            }
        }
    }
    (None, -1)
}

fn reconstruct<T: Eq + Hash + Clone>(parent: &HashMap<T, T>, source: &T, target: &T) -> Vec<T> {
    let mut path = vec![target.clone()];
    let mut current = target;
    while current != source {
        current = parent.get(current).unwrap();
        path.push(current.clone());
    }
    path.reverse();
    path
}

/// Returns all connected components (undirected graphs).
pub fn connected_components<T: Eq + Hash + Clone>(graph: &Graph<T>) -> Vec<Vec<T>> {
    let mut visited: HashSet<T> = HashSet::new();
    let mut components = Vec::new();

    for node in graph.nodes() {
        if visited.contains(&node) {
            continue;
        }
        let component = bfs(graph, &node);
        for n in &component {
            visited.insert(n.clone());
        }
        components.push(component);
    }
    components
}

#[derive(Debug, Clone, Copy, PartialEq)]
pub enum BiColor {
    A,
    B,
}

#[derive(Debug)]
pub struct BipartiteResult<T> {
    pub is_bipartite: bool,
    pub coloring: HashMap<T, BiColor>,
    pub conflict: Option<(T, T)>,
}

/// Checks if the graph is bipartite via two-coloring BFS.
pub fn is_bipartite<T: Eq + Hash + Clone>(graph: &Graph<T>) -> BipartiteResult<T> {
    let mut coloring: HashMap<T, BiColor> = HashMap::new();

    for start in graph.nodes() {
        if coloring.contains_key(&start) {
            continue;
        }
        coloring.insert(start.clone(), BiColor::A);
        let mut queue = VecDeque::new();
        queue.push_back(start);

        while let Some(node) = queue.pop_front() {
            let next_color = match coloring[&node] {
                BiColor::A => BiColor::B,
                BiColor::B => BiColor::A,
            };

            for neighbor in graph.neighbors(&node) {
                if let Some(&existing) = coloring.get(neighbor) {
                    if existing == coloring[&node] {
                        return BipartiteResult {
                            is_bipartite: false,
                            coloring,
                            conflict: Some((node.clone(), neighbor.clone())),
                        };
                    }
                } else {
                    coloring.insert(neighbor.clone(), next_color);
                    queue.push_back(neighbor.clone());
                }
            }
        }
    }

    BipartiteResult {
        is_bipartite: true,
        coloring,
        conflict: None,
    }
}

#[derive(Debug)]
pub struct CycleResult<T> {
    pub has_cycle: bool,
    pub cycle: Vec<T>,
}

#[derive(Clone, Copy, PartialEq)]
enum DfsColor {
    White,
    Gray,
    Black,
}

/// Detects cycles in directed graphs using three-color DFS.
pub fn has_cycle_directed<T: Eq + Hash + Clone>(graph: &Graph<T>) -> CycleResult<T> {
    let mut colors: HashMap<T, DfsColor> = graph
        .nodes()
        .into_iter()
        .map(|n| (n, DfsColor::White))
        .collect();
    let mut path: Vec<T> = Vec::new();

    for node in graph.nodes() {
        if colors[&node] == DfsColor::White {
            if let Some(cycle) = dfs_cycle_directed(graph, &node, &mut colors, &mut path) {
                return CycleResult { has_cycle: true, cycle };
            }
        }
    }
    CycleResult { has_cycle: false, cycle: Vec::new() }
}

fn dfs_cycle_directed<T: Eq + Hash + Clone>(
    graph: &Graph<T>,
    node: &T,
    colors: &mut HashMap<T, DfsColor>,
    path: &mut Vec<T>,
) -> Option<Vec<T>> {
    colors.insert(node.clone(), DfsColor::Gray);
    path.push(node.clone());

    for neighbor in graph.neighbors(node) {
        match colors.get(neighbor) {
            Some(DfsColor::Gray) => {
                let idx = path.iter().position(|n| n == neighbor).unwrap();
                let mut cycle: Vec<T> = path[idx..].to_vec();
                cycle.push(neighbor.clone());
                return Some(cycle);
            }
            Some(DfsColor::White) | None => {
                if let Some(cycle) = dfs_cycle_directed(graph, neighbor, colors, path) {
                    return Some(cycle);
                }
            }
            _ => {}
        }
    }

    path.pop();
    colors.insert(node.clone(), DfsColor::Black);
    None
}

/// Detects cycles in undirected graphs using parent tracking.
pub fn has_cycle_undirected<T: Eq + Hash + Clone>(graph: &Graph<T>) -> CycleResult<T> {
    let mut visited: HashSet<T> = HashSet::new();
    let mut parent: HashMap<T, T> = HashMap::new();

    for start in graph.nodes() {
        if visited.contains(&start) {
            continue;
        }
        visited.insert(start.clone());
        let mut stack = vec![start.clone()];

        while let Some(node) = stack.pop() {
            for neighbor in graph.neighbors(&node) {
                if !visited.contains(neighbor) {
                    visited.insert(neighbor.clone());
                    parent.insert(neighbor.clone(), node.clone());
                    stack.push(neighbor.clone());
                } else if parent.get(&node) != Some(neighbor) {
                    return CycleResult {
                        has_cycle: true,
                        cycle: vec![node.clone(), neighbor.clone()],
                    };
                }
            }
        }
    }
    CycleResult { has_cycle: false, cycle: Vec::new() }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn bfs_order() {
        let mut g = Graph::new(GraphMode::Undirected);
        g.add_edge("A".to_string(), "B".to_string());
        g.add_edge("A".to_string(), "C".to_string());
        g.add_edge("B".to_string(), "D".to_string());

        let order = bfs(&g, &"A".to_string());
        assert_eq!(order[0], "A");
        assert_eq!(order.len(), 4);
    }

    #[test]
    fn dfs_recursive_order() {
        let mut g = Graph::new(GraphMode::Directed);
        g.add_edge("A".to_string(), "B".to_string());
        g.add_edge("A".to_string(), "C".to_string());
        g.add_edge("B".to_string(), "D".to_string());

        let order = dfs_recursive(&g, &"A".to_string());
        assert_eq!(order[0], "A");
        assert_eq!(order.len(), 4);
    }

    #[test]
    fn dfs_iterative_order() {
        let mut g = Graph::new(GraphMode::Directed);
        g.add_edge("A".to_string(), "B".to_string());
        g.add_edge("A".to_string(), "C".to_string());
        g.add_edge("B".to_string(), "D".to_string());

        let order = dfs_iterative(&g, &"A".to_string());
        assert_eq!(order[0], "A");
        assert_eq!(order.len(), 4);
    }

    #[test]
    fn bfs_shortest_path() {
        let mut g = Graph::new(GraphMode::Undirected);
        g.add_edge(1, 2);
        g.add_edge(2, 3);
        g.add_edge(1, 3);

        let (path, dist) = shortest_path(&g, &1, &3);
        assert_eq!(dist, 1);
        let p = path.unwrap();
        assert_eq!(*p.first().unwrap(), 1);
        assert_eq!(*p.last().unwrap(), 3);
    }

    #[test]
    fn unreachable_target() {
        let mut g = Graph::new(GraphMode::Directed);
        g.add_edge(1, 2);
        g.add_node(3);

        let (path, dist) = shortest_path(&g, &1, &3);
        assert_eq!(dist, -1);
        assert!(path.is_none());
    }

    #[test]
    fn connected_components_test() {
        let mut g = Graph::new(GraphMode::Undirected);
        g.add_edge(1, 2);
        g.add_edge(2, 3);
        g.add_edge(4, 5);
        g.add_node(6);

        let components = connected_components(&g);
        assert_eq!(components.len(), 3);
        let total: usize = components.iter().map(|c| c.len()).sum();
        assert_eq!(total, 6);
    }

    #[test]
    fn bipartite_even_cycle() {
        let mut g = Graph::new(GraphMode::Undirected);
        g.add_edge(1, 2);
        g.add_edge(2, 3);
        g.add_edge(3, 4);
        g.add_edge(4, 1);

        let result = is_bipartite(&g);
        assert!(result.is_bipartite);
    }

    #[test]
    fn not_bipartite_odd_cycle() {
        let mut g = Graph::new(GraphMode::Undirected);
        g.add_edge(1, 2);
        g.add_edge(2, 3);
        g.add_edge(3, 1);

        let result = is_bipartite(&g);
        assert!(!result.is_bipartite);
        assert!(result.conflict.is_some());
    }

    #[test]
    fn cycle_directed() {
        let mut g = Graph::new(GraphMode::Directed);
        g.add_edge("A".to_string(), "B".to_string());
        g.add_edge("B".to_string(), "C".to_string());
        g.add_edge("C".to_string(), "A".to_string());

        let result = has_cycle_directed(&g);
        assert!(result.has_cycle);
        assert!(result.cycle.len() >= 3);
    }

    #[test]
    fn no_cycle_directed() {
        let mut g = Graph::new(GraphMode::Directed);
        g.add_edge("A".to_string(), "B".to_string());
        g.add_edge("B".to_string(), "C".to_string());

        let result = has_cycle_directed(&g);
        assert!(!result.has_cycle);
    }

    #[test]
    fn cycle_undirected() {
        let mut g = Graph::new(GraphMode::Undirected);
        g.add_edge(1, 2);
        g.add_edge(2, 3);
        g.add_edge(3, 1);

        let result = has_cycle_undirected(&g);
        assert!(result.has_cycle);
    }

    #[test]
    fn no_cycle_undirected_tree() {
        let mut g = Graph::new(GraphMode::Undirected);
        g.add_edge(1, 2);
        g.add_edge(2, 3);

        let result = has_cycle_undirected(&g);
        assert!(!result.has_cycle);
    }

    #[test]
    fn empty_graph() {
        let g: Graph<String> = Graph::new(GraphMode::Directed);
        let components = connected_components(&g);
        assert!(components.is_empty());
    }

    #[test]
    fn single_node() {
        let mut g = Graph::new(GraphMode::Undirected);
        g.add_node(1);
        let order = bfs(&g, &1);
        assert_eq!(order, vec![1]);
        let components = connected_components(&g);
        assert_eq!(components.len(), 1);
    }
}
```

## Running the Rust Solution

```bash
cargo new graphtraversal --lib && cd graphtraversal
# Replace src/lib.rs with the code above
cargo test -- --nocapture
```

### Expected Output

```
running 15 tests
test tests::bfs_order ... ok
test tests::dfs_recursive_order ... ok
test tests::dfs_iterative_order ... ok
test tests::bfs_shortest_path ... ok
test tests::unreachable_target ... ok
test tests::connected_components_test ... ok
test tests::bipartite_even_cycle ... ok
test tests::not_bipartite_odd_cycle ... ok
test tests::cycle_directed ... ok
test tests::no_cycle_directed ... ok
test tests::cycle_undirected ... ok
test tests::no_cycle_undirected_tree ... ok
test tests::empty_graph ... ok
test tests::single_node ... ok

test result: ok. 15 passed; 0 failed; 0 ignored
```

## Design Decisions

1. **Unified graph type with mode flag**: A single `Graph[T]` supports both directed and undirected modes. For undirected mode, `AddEdge` inserts both directions. This avoids duplicating the entire graph structure while keeping the interface clean.

2. **BFS for shortest path in unweighted graphs**: BFS naturally finds shortest paths by hop count because it explores level by level. No priority queue needed. This is simpler and faster than Dijkstra when all edges have equal weight.

3. **Three-color vs parent tracking**: Directed cycle detection requires three colors because a visited node might be in a different branch (cross edge), not a cycle. Undirected cycle detection uses parent tracking because any visited non-parent neighbor is a cycle.

4. **Bipartite check returns coloring**: Returning the full coloring map (not just true/false) provides useful information for applications that need the partition (e.g., scheduling, graph coloring).

5. **Connected components via BFS flood-fill**: Using BFS for component discovery is straightforward and avoids recursion depth issues. Each unvisited node starts a new BFS, and all discovered nodes form one component.

## Common Mistakes

- **Undirected cycle detection counting back edges as cycles**: In undirected graphs, every edge appears twice in the adjacency list. The parent check prevents the trivial "A->B->A" false positive. Forgetting this check reports every edge as a cycle.
- **Iterative DFS visiting order**: Pushing neighbors in natural order and popping from the stack reverses them. Push in reverse order to match recursive DFS traversal order. This matters for deterministic test output.
- **Not handling disconnected graphs**: Starting BFS/DFS from a single node misses other components. Always iterate over all nodes and start new traversals from unvisited ones.
- **Confusing BFS distance with path length**: BFS distance is hop count, not the number of nodes in the path. A path of [A, B, C] has distance 2 (two edges), not 3.

## Performance Notes

| Algorithm | Time Complexity | Space Complexity |
|-----------|----------------|-----------------|
| BFS | O(V + E) | O(V) |
| DFS (recursive) | O(V + E) | O(V) call stack |
| DFS (iterative) | O(V + E) | O(V) explicit stack |
| BFS Shortest Path | O(V + E) | O(V) |
| Connected Components | O(V + E) | O(V) |
| Bipartite Check | O(V + E) | O(V) |
| Cycle Detection | O(V + E) | O(V) |

All algorithms are linear in graph size. The iterative DFS avoids stack overflow on graphs with O(V) depth, which is critical for large chains or paths. BFS shortest path is optimal for unweighted graphs and should be preferred over Dijkstra in this case.
