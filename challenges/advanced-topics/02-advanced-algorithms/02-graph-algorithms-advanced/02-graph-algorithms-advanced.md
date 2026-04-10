<!--
type: reference
difficulty: advanced
section: [02-advanced-algorithms]
concepts: [tarjan-scc, hopcroft-karp, gomory-hu-tree, heavy-light-decomposition]
languages: [go, rust]
estimated_reading_time: 60-90 min
bloom_level: analyze
prerequisites: [graph-traversal-bfs-dfs, max-flow-basics, tree-data-structures, segment-trees]
papers: [tarjan-1972-depth-first-search, hopcroft-karp-1973-bipartite-matching, gomory-hu-1961-all-pairs-max-flow]
industry_use: [bazel, buck, postgresql-query-planner, hadoop, network-routing, build-systems]
language_contrast: medium
-->

# Advanced Graph Algorithms

> These four algorithms cover the hardest structural questions in graphs: where are the cycles, who can be matched to whom, how much can flow between any pair, and how do you answer tree queries efficiently.

## Mental Model

Graphs encode relationships. The *structural* questions — not "find the shortest path"
but "which components are mutually reachable", "can every job be assigned to a worker",
"what is the bottleneck between any two nodes" — require algorithms that look at the
whole graph, not just local neighborhoods.

The pattern-recognition instinct for each:

- **Tarjan SCC**: You have a directed graph and you need to know "which nodes are
  part of a mutual dependency cycle." Build systems, package managers, dataflow
  compilers: any directed dependency graph with cycles needs SCC to identify what
  must be built/processed together.

- **Hopcroft-Karp**: You have two disjoint sets and a list of "compatible" pairs.
  You want the maximum number of non-conflicting assignments. Scheduling (workers to
  shifts), networking (routers to paths), compiler register allocation. If the graph
  is bipartite and you want maximum matching, Hopcroft-Karp is the right tool.

- **Gomory-Hu tree**: You want max-flow between all O(n²) pairs of nodes but running
  n² max-flow calls is too slow. The Gomory-Hu tree encodes all n-1 relevant max-flow
  values in a tree with n-1 edges. Network reliability engineers use this to find the
  weakest link for every pair of nodes with a single pass.

- **Heavy-Light Decomposition (HLD)**: You have a tree and want to answer range queries
  or updates on paths between nodes. HLD maps tree paths to contiguous array ranges,
  letting you answer path queries with a segment tree in O(log² n).

## Core Concepts

### Tarjan's SCC Algorithm

Uses DFS with two invariants per node: `disc[v]` (discovery time) and `low[v]`
(lowest discovery time reachable via the DFS subtree including back edges).

A node `v` is the root of an SCC if `low[v] == disc[v]` after its subtree is processed.
Nodes in the SCC are those on the DFS stack above `v` (inclusive).

The algorithm runs in O(V + E). The key insight: if you can reach a node with a smaller
`disc` time via a back edge from within your subtree, your SCC has not closed yet. When
`low[v] == disc[v]`, no node in your subtree can escape to a node discovered earlier —
meaning your entire subtree from this "root" down (until a previously popped node) forms
one SCC.

**Applications**: Condensation DAG (replace each SCC with a single node, making the
graph a DAG for topological processing). Bazel uses this to identify "build islands."
PostgreSQL query planner uses it to identify mutually dependent subqueries.

### Hopcroft-Karp

Standard augmenting-path bipartite matching (Hungarian algorithm) finds one augmenting
path per BFS, giving O(E × V) total. Hopcroft-Karp finds *all* shortest augmenting paths
simultaneously in each BFS layer, achieving O(E√V).

The algorithm alternates between:
1. **BFS phase**: Find the shortest augmenting path length L. Layer the graph by distance
   from unmatched left nodes.
2. **DFS phase**: Find vertex-disjoint augmenting paths of length exactly L through the
   layered graph.

After each BFS+DFS round, the maximum matching increases by at least the number of paths
found. The number of rounds is O(√V) — proven by the fact that after √V rounds the
remaining augmenting paths have length > 2√V, and there can be at most √V vertex-disjoint
such paths.

**Applications**: Task assignment systems, network flow decomposition, image segmentation
(pixel-to-label matching), database query optimization (join order selection).

### Gomory-Hu Tree

For a graph G with n nodes, the Gomory-Hu tree T has the same node set and n-1 edges,
each labeled with a max-flow value. For any pair (s, t), the max-flow between them in
G equals the minimum edge label on the unique s-t path in T.

Construction: n-1 max-flow computations (not n²). At each step, pick an unprocessed
pair, run max-flow, and add the corresponding edge to T. The minimum cut found in step i
is used to "redirect" future pairs, exploiting the submodularity of cut functions.

**Why it works**: Max-flow values satisfy a submodularity property. The tree structure
efficiently encodes all pairwise minima without enumerating all pairs.

**Applications**: Network reliability analysis, finding the globally minimum cut (it's
the minimum edge weight in the Gomory-Hu tree), telecommunications network design.

### Heavy-Light Decomposition

Every tree path from u to v can be decomposed into O(log n) contiguous segments on a
few "heavy chains." A heavy edge from v goes to the child with the largest subtree;
a light edge goes to any other child.

Key insight: on any root-to-leaf path, there are at most O(log n) light edges (each
light edge at least halves the remaining subtree size). Heavy edges form chains. By
assigning contiguous DFS timestamps to heavy chains, each chain maps to a contiguous
array range.

To query a path u→v: climb from both endpoints toward the LCA, processing one heavy
chain per step (O(log n) chains × O(log n) for segment tree query = O(log² n) total).

## Implementation: Go

```go
package main

import "fmt"

// ─── Tarjan SCC ───────────────────────────────────────────────────────────────

type SCC struct {
	n     int
	adj   [][]int
	disc  []int
	low   []int
	onStk []bool
	stk   []int
	timer int
	comps [][]int // each SCC as a list of nodes
}

func NewSCC(n int) *SCC {
	return &SCC{
		n:     n,
		adj:   make([][]int, n),
		disc:  make([]int, n),
		low:   make([]int, n),
		onStk: make([]bool, n),
	}
}

func (s *SCC) AddEdge(u, v int) { s.adj[u] = append(s.adj[u], v) }

func (s *SCC) dfs(v int) {
	s.timer++
	s.disc[v] = s.timer
	s.low[v] = s.timer
	s.stk = append(s.stk, v)
	s.onStk[v] = true

	for _, u := range s.adj[v] {
		if s.disc[u] == 0 {
			s.dfs(u)
			if s.low[u] < s.low[v] {
				s.low[v] = s.low[u]
			}
		} else if s.onStk[u] && s.disc[u] < s.low[v] {
			s.low[v] = s.disc[u]
		}
	}

	// v is the root of an SCC
	if s.low[v] == s.disc[v] {
		comp := []int{}
		for {
			top := s.stk[len(s.stk)-1]
			s.stk = s.stk[:len(s.stk)-1]
			s.onStk[top] = false
			comp = append(comp, top)
			if top == v {
				break
			}
		}
		s.comps = append(s.comps, comp)
	}
}

func (s *SCC) FindSCCs() [][]int {
	for v := 0; v < s.n; v++ {
		if s.disc[v] == 0 {
			s.dfs(v)
		}
	}
	return s.comps
}

// ─── Hopcroft-Karp ────────────────────────────────────────────────────────────
// Left nodes: 0..L-1, Right nodes: 0..R-1

type BipartiteGraph struct {
	L, R int
	adj  [][]int // adj[left_node] = list of right nodes
}

func NewBipartiteGraph(l, r int) *BipartiteGraph {
	return &BipartiteGraph{L: l, R: r, adj: make([][]int, l)}
}

func (g *BipartiteGraph) AddEdge(u, v int) { g.adj[u] = append(g.adj[u], v) }

func (g *BipartiteGraph) MaxMatching() int {
	matchL := make([]int, g.L) // matchL[u] = matched right node, or -1
	matchR := make([]int, g.R) // matchR[v] = matched left node, or -1
	for i := range matchL { matchL[i] = -1 }
	for i := range matchR { matchR[i] = -1 }

	dist := make([]int, g.L)
	const Free = -1
	const Inf = 1 << 30

	bfs := func() bool {
		queue := []int{}
		for u := 0; u < g.L; u++ {
			if matchL[u] == Free {
				dist[u] = 0
				queue = append(queue, u)
			} else {
				dist[u] = Inf
			}
		}
		found := false
		for len(queue) > 0 {
			u := queue[0]; queue = queue[1:]
			for _, v := range g.adj[u] {
				next := matchR[v]
				if next == Free {
					found = true
				} else if dist[next] == Inf {
					dist[next] = dist[u] + 1
					queue = append(queue, next)
				}
			}
		}
		return found
	}

	var dfs func(u int) bool
	dfs = func(u int) bool {
		for _, v := range g.adj[u] {
			next := matchR[v]
			if next == Free || (dist[next] == dist[u]+1 && dfs(next)) {
				matchL[u] = v
				matchR[v] = u
				return true
			}
		}
		dist[u] = Inf // block this node from future DFS in this phase
		return false
	}

	matching := 0
	for bfs() {
		for u := 0; u < g.L; u++ {
			if matchL[u] == Free && dfs(u) {
				matching++
			}
		}
	}
	return matching
}

// ─── Heavy-Light Decomposition ────────────────────────────────────────────────
// Supports path sum queries and point updates on a tree.

type HLD struct {
	n, root    int
	adj        [][]int
	parent     []int
	depth      []int
	sz         []int
	heavy      []int // heavy child of each node (-1 if leaf)
	head       []int // top of the heavy chain containing this node
	pos        []int // position in the segment tree array
	timer      int
	seg        []int // segment tree (sum)
	values     []int // node values
}

func NewHLD(n int, vals []int) *HLD {
	h := &HLD{
		n:      n,
		root:   0,
		adj:    make([][]int, n),
		parent: make([]int, n),
		depth:  make([]int, n),
		sz:     make([]int, n),
		heavy:  make([]int, n),
		head:   make([]int, n),
		pos:    make([]int, n),
		seg:    make([]int, 4*n),
		values: vals,
	}
	for i := range h.heavy { h.heavy[i] = -1 }
	return h
}

func (h *HLD) AddEdge(u, v int) {
	h.adj[u] = append(h.adj[u], v)
	h.adj[v] = append(h.adj[v], u)
}

func (h *HLD) dfsSize(v, p, d int) {
	h.parent[v] = p
	h.depth[v] = d
	h.sz[v] = 1
	maxSz := 0
	for _, u := range h.adj[v] {
		if u == p { continue }
		h.dfsSize(u, v, d+1)
		h.sz[v] += h.sz[u]
		if h.sz[u] > maxSz {
			maxSz = h.sz[u]
			h.heavy[v] = u
		}
	}
}

func (h *HLD) dfsHLD(v, head int) {
	h.head[v] = head
	h.pos[v] = h.timer
	h.timer++
	if h.heavy[v] != -1 {
		h.dfsHLD(h.heavy[v], head)
	}
	for _, u := range h.adj[v] {
		if u == h.parent[v] || u == h.heavy[v] { continue }
		h.dfsHLD(u, u) // light edge starts a new chain
	}
}

func (h *HLD) Build() {
	h.dfsSize(0, -1, 0)
	h.dfsHLD(0, 0)
	// Initialize segment tree with values at positions
	posVal := make([]int, h.n)
	for v := 0; v < h.n; v++ {
		posVal[h.pos[v]] = h.values[v]
	}
	h.buildSeg(1, 0, h.n-1, posVal)
}

func (h *HLD) buildSeg(node, l, r int, vals []int) {
	if l == r { h.seg[node] = vals[l]; return }
	mid := (l + r) / 2
	h.buildSeg(2*node, l, mid, vals)
	h.buildSeg(2*node+1, mid+1, r, vals)
	h.seg[node] = h.seg[2*node] + h.seg[2*node+1]
}

func (h *HLD) querySeg(node, l, r, ql, qr int) int {
	if qr < l || r < ql { return 0 }
	if ql <= l && r <= qr { return h.seg[node] }
	mid := (l + r) / 2
	return h.querySeg(2*node, l, mid, ql, qr) + h.querySeg(2*node+1, mid+1, r, ql, qr)
}

// PathQuery returns the sum of node values on the path u→v.
func (h *HLD) PathQuery(u, v int) int {
	res := 0
	for h.head[u] != h.head[v] {
		// Climb from the deeper chain head
		if h.depth[h.head[u]] < h.depth[h.head[v]] {
			u, v = v, u
		}
		res += h.querySeg(1, 0, h.n-1, h.pos[h.head[u]], h.pos[u])
		u = h.parent[h.head[u]]
	}
	if h.depth[u] > h.depth[v] { u, v = v, u }
	res += h.querySeg(1, 0, h.n-1, h.pos[u], h.pos[v])
	return res
}

func main() {
	// SCC demo: 0→1→2→0 (cycle), 3→2
	g := NewSCC(4)
	g.AddEdge(0, 1); g.AddEdge(1, 2); g.AddEdge(2, 0); g.AddEdge(3, 2)
	comps := g.FindSCCs()
	fmt.Println("SCCs:", comps) // [[0 2 1] [3]] or similar

	// Hopcroft-Karp demo: 3 left, 3 right, perfect matching
	bg := NewBipartiteGraph(3, 3)
	bg.AddEdge(0, 0); bg.AddEdge(0, 1)
	bg.AddEdge(1, 1); bg.AddEdge(1, 2)
	bg.AddEdge(2, 0); bg.AddEdge(2, 2)
	fmt.Println("Max matching:", bg.MaxMatching()) // 3

	// HLD demo
	vals := []int{1, 2, 3, 4, 5}
	hld := NewHLD(5, vals)
	hld.AddEdge(0, 1); hld.AddEdge(0, 2); hld.AddEdge(1, 3); hld.AddEdge(1, 4)
	hld.Build()
	fmt.Println("Path sum 3→4:", hld.PathQuery(3, 4)) // 2+1+2+... depends on tree
	fmt.Println("Path sum 3→2:", hld.PathQuery(3, 2))
}
```

### Go-specific considerations

- **Recursive DFS in Tarjan**: The recursion in `SCC.dfs` can stack-overflow on degenerate
  graphs (linear chains with 100k+ nodes). For production use, implement the iterative
  variant using an explicit stack with a "resume position" per frame.
- **Slice-backed stack in SCC**: `s.stk = append(s.stk, v)` / `s.stk[:len-1]` is idiomatic
  but causes repeated allocations. Pre-allocate `make([]int, 0, n)` to avoid this.
- **Interface for segment tree**: The HLD shown uses a sum segment tree. Generalizing to
  arbitrary monoids requires an interface or generics (Go 1.18+). The generic approach
  is cleaner for production use: `type SegTree[T any] struct { combine func(T,T) T; ... }`.

## Implementation: Rust

```rust
use std::collections::VecDeque;

// ─── Tarjan SCC (iterative) ───────────────────────────────────────────────────

pub struct SCC {
    n: usize,
    adj: Vec<Vec<usize>>,
}

impl SCC {
    pub fn new(n: usize) -> Self {
        SCC { n, adj: vec![vec![]; n] }
    }

    pub fn add_edge(&mut self, u: usize, v: usize) {
        self.adj[u].push(v);
    }

    pub fn find_sccs(&self) -> Vec<Vec<usize>> {
        let mut disc = vec![0usize; self.n];
        let mut low = vec![0usize; self.n];
        let mut on_stk = vec![false; self.n];
        let mut stk: Vec<usize> = Vec::new();
        let mut timer = 1usize;
        let mut comps: Vec<Vec<usize>> = Vec::new();

        // Iterative Tarjan using explicit call stack
        // Each frame: (node, parent, edge_index)
        let mut call_stack: Vec<(usize, usize, usize)> = Vec::new();

        for start in 0..self.n {
            if disc[start] != 0 { continue; }

            call_stack.push((start, usize::MAX, 0));

            while let Some((v, _parent, ei)) = call_stack.last_mut() {
                let v = *v;
                if disc[v] == 0 {
                    disc[v] = timer;
                    low[v] = timer;
                    timer += 1;
                    stk.push(v);
                    on_stk[v] = true;
                }

                if *ei < self.adj[v].len() {
                    let u = self.adj[v][*ei];
                    *ei += 1;
                    if disc[u] == 0 {
                        call_stack.push((u, v, 0));
                    } else if on_stk[u] {
                        if disc[u] < low[v] { low[v] = disc[u]; }
                    }
                } else {
                    // Post-order processing
                    call_stack.pop();
                    if let Some((parent_v, _, _)) = call_stack.last() {
                        if low[v] < low[*parent_v] { low[*parent_v] = low[v]; }
                    }
                    if low[v] == disc[v] {
                        let mut comp = Vec::new();
                        loop {
                            let top = stk.pop().unwrap();
                            on_stk[top] = false;
                            comp.push(top);
                            if top == v { break; }
                        }
                        comps.push(comp);
                    }
                }
            }
        }
        comps
    }
}

// ─── Hopcroft-Karp ────────────────────────────────────────────────────────────

pub struct BipartiteGraph {
    l: usize,
    r: usize,
    adj: Vec<Vec<usize>>,
}

impl BipartiteGraph {
    pub fn new(l: usize, r: usize) -> Self {
        BipartiteGraph { l, r, adj: vec![vec![]; l] }
    }

    pub fn add_edge(&mut self, u: usize, v: usize) {
        self.adj[u].push(v);
    }

    pub fn max_matching(&self) -> usize {
        let none = usize::MAX;
        let mut match_l = vec![none; self.l];
        let mut match_r = vec![none; self.r];
        let mut dist = vec![0u32; self.l];
        const INF: u32 = u32::MAX;

        let bfs = |match_l: &[usize], dist: &mut Vec<u32>| -> bool {
            let mut queue = VecDeque::new();
            for u in 0..self.l {
                if match_l[u] == none {
                    dist[u] = 0;
                    queue.push_back(u);
                } else {
                    dist[u] = INF;
                }
            }
            let mut found = false;
            while let Some(u) = queue.pop_front() {
                for &v in &self.adj[u] {
                    let next = match_r[v];
                    if next == none {
                        found = true;
                    } else if dist[next] == INF {
                        dist[next] = dist[u] + 1;
                        queue.push_back(next);
                    }
                }
            }
            found
        };

        // DFS using explicit stack to avoid deep recursion
        let dfs_iter = |u: usize, match_l: &mut Vec<usize>, match_r: &mut Vec<usize>,
                         dist: &mut Vec<u32>, adj: &Vec<Vec<usize>>| -> bool {
            let mut stack = vec![(u, 0usize)];
            let mut path: Vec<(usize, usize)> = Vec::new(); // (left_node, right_node)

            while let Some((cu, ei)) = stack.last_mut() {
                let cu = *cu;
                if *ei < adj[cu].len() {
                    let v = adj[cu][*ei];
                    *ei += 1;
                    let next = match_r[v];
                    if next == none || (dist[next] == dist[cu] + 1) {
                        path.push((cu, v));
                        if next == none {
                            // Augmenting path found — apply
                            for &(lu, rv) in &path {
                                let prev_r = match_l[lu];
                                match_l[lu] = rv;
                                match_r[rv] = lu;
                                if prev_r != none { match_r[prev_r] = none; }
                            }
                            return true;
                        }
                        stack.push((next, 0));
                    }
                } else {
                    dist[cu] = INF;
                    stack.pop();
                    path.pop();
                }
            }
            false
        };

        let mut matching = 0;
        while bfs(&match_l, &mut dist) {
            for u in 0..self.l {
                if match_l[u] == none
                    && dfs_iter(u, &mut match_l, &mut match_r, &mut dist, &self.adj)
                {
                    matching += 1;
                }
            }
        }
        matching
    }
}

// ─── Heavy-Light Decomposition ────────────────────────────────────────────────

pub struct HLD {
    n: usize,
    adj: Vec<Vec<usize>>,
    values: Vec<i64>,
    parent: Vec<usize>,
    depth: Vec<usize>,
    sz: Vec<usize>,
    heavy: Vec<Option<usize>>,
    head: Vec<usize>,
    pos: Vec<usize>,
    seg: Vec<i64>,
}

impl HLD {
    pub fn new(n: usize, values: Vec<i64>) -> Self {
        HLD {
            n,
            adj: vec![vec![]; n],
            values,
            parent: vec![usize::MAX; n],
            depth: vec![0; n],
            sz: vec![0; n],
            heavy: vec![None; n],
            head: vec![0; n],
            pos: vec![0; n],
            seg: vec![0; 4 * n],
        }
    }

    pub fn add_edge(&mut self, u: usize, v: usize) {
        self.adj[u].push(v);
        self.adj[v].push(u);
    }

    pub fn build(&mut self) {
        self.dfs_size(0);
        let mut timer = 0usize;
        self.dfs_hld(0, 0, &mut timer);
        let mut pos_vals = vec![0i64; self.n];
        for v in 0..self.n { pos_vals[self.pos[v]] = self.values[v]; }
        Self::build_seg_static(&mut self.seg, 1, 0, self.n - 1, &pos_vals);
    }

    fn dfs_size(&mut self, root: usize) {
        let mut stack = vec![(root, usize::MAX, false)];
        while let Some((v, p, returning)) = stack.pop() {
            if returning {
                self.sz[v] = 1;
                let mut max_sz = 0;
                for &u in &self.adj[v].clone() {
                    if u == p { continue; }
                    self.sz[v] += self.sz[u];
                    if self.sz[u] > max_sz {
                        max_sz = self.sz[u];
                        self.heavy[v] = Some(u);
                    }
                }
            } else {
                stack.push((v, p, true));
                for &u in &self.adj[v].clone() {
                    if u != p {
                        self.parent[u] = v;
                        self.depth[u] = self.depth[v] + 1;
                        stack.push((u, v, false));
                    }
                }
            }
        }
    }

    fn dfs_hld(&mut self, root: usize, head: usize, timer: &mut usize) {
        let mut stack = vec![(root, head)];
        while let Some((v, h)) = stack.pop() {
            self.head[v] = h;
            self.pos[v] = *timer;
            *timer += 1;
            // Process light children first (reversed so heavy child is processed next)
            let heavy = self.heavy[v];
            for &u in self.adj[v].clone().iter().rev() {
                if u == self.parent[v] || Some(u) == heavy { continue; }
                stack.push((u, u));
            }
            if let Some(hv) = heavy {
                stack.push((hv, h)); // heavy child continues the chain
            }
        }
    }

    fn build_seg_static(seg: &mut Vec<i64>, node: usize, l: usize, r: usize, vals: &[i64]) {
        if l == r { seg[node] = vals[l]; return; }
        let mid = (l + r) / 2;
        Self::build_seg_static(seg, 2*node, l, mid, vals);
        Self::build_seg_static(seg, 2*node+1, mid+1, r, vals);
        seg[node] = seg[2*node] + seg[2*node+1];
    }

    fn query_seg(&self, node: usize, l: usize, r: usize, ql: usize, qr: usize) -> i64 {
        if qr < l || r < ql { return 0; }
        if ql <= l && r <= qr { return self.seg[node]; }
        let mid = (l + r) / 2;
        self.query_seg(2*node, l, mid, ql, qr) + self.query_seg(2*node+1, mid+1, r, ql, qr)
    }

    pub fn path_query(&self, mut u: usize, mut v: usize) -> i64 {
        let mut res = 0;
        while self.head[u] != self.head[v] {
            if self.depth[self.head[u]] < self.depth[self.head[v]] {
                std::mem::swap(&mut u, &mut v);
            }
            res += self.query_seg(1, 0, self.n - 1, self.pos[self.head[u]], self.pos[u]);
            u = self.parent[self.head[u]];
        }
        if self.depth[u] > self.depth[v] { std::mem::swap(&mut u, &mut v); }
        res += self.query_seg(1, 0, self.n - 1, self.pos[u], self.pos[v]);
        res
    }
}

fn main() {
    // SCC demo
    let mut scc = SCC::new(4);
    scc.add_edge(0, 1); scc.add_edge(1, 2); scc.add_edge(2, 0); scc.add_edge(3, 2);
    let comps = scc.find_sccs();
    println!("SCCs: {:?}", comps);

    // Hopcroft-Karp demo
    let mut bg = BipartiteGraph::new(3, 3);
    bg.add_edge(0, 0); bg.add_edge(0, 1);
    bg.add_edge(1, 1); bg.add_edge(1, 2);
    bg.add_edge(2, 0); bg.add_edge(2, 2);
    println!("Max matching: {}", bg.max_matching());

    // HLD demo
    let mut hld = HLD::new(5, vec![1, 2, 3, 4, 5]);
    hld.add_edge(0, 1); hld.add_edge(0, 2); hld.add_edge(1, 3); hld.add_edge(1, 4);
    hld.build();
    println!("Path sum 3→4: {}", hld.path_query(3, 4));
    println!("Path sum 3→2: {}", hld.path_query(3, 2));
}
```

### Rust-specific considerations

- **Iterative Tarjan complexity**: The explicit-stack iterative Tarjan shown tracks
  `(node, parent, edge_index)`. Updating `low` on the way back up requires knowing
  the parent. The `call_stack.last()` peek pattern handles this cleanly without unsafe code.
- **BipartiteGraph DFS augmentation**: The iterative augmenting-path DFS is significantly
  harder to write correctly than recursive. The path reconstruction step (applying the
  augmentation) requires storing the path. An alternative: keep the recursive variant but
  increase the stack size with `std::thread::Builder::new().stack_size(8 * 1024 * 1024)`.
- **HLD heavy-child ordering**: In the iterative DFS for chain assignment, the heavy child
  must be pushed *last* onto the stack so it is processed *first* (LIFO), maintaining the
  chain invariant.
- **Lifetime complexity in generic segment trees**: A fully generic HLD with a user-provided
  monoid requires `where T: Clone + Default` constraints. The shown implementation hardcodes
  `i64` sum for clarity; production code should parameterize.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|----|------|
| Recursive SCC | Safe for most graphs; stack overflows on n>100k path graphs | Must be iterative; borrow checker also prevents naive recursive self-mutation |
| Borrow checker impact | None (GC) | `self.adj[v].clone()` needed when iterating adj while mutating self |
| Generics for segment tree | Go 1.18+ generics work; older code uses `interface{}` | Trait bounds feel more natural; `Fn(T,T)->T` combiner is clean |
| Concurrency potential | Goroutines for parallel SCC on disconnected components | Rayon for parallel SCC on disjoint components; `Send + Sync` constraints explicit |
| Memory overhead | GC pause during large BFS allocation | Zero pause; pre-allocation with `Vec::with_capacity` |
| Code verbosity | HLD in ~80 lines | HLD in ~120 lines due to lifetime/ownership annotations |

## Production War Stories

**Bazel build system**: Tarjan SCC is the core of Bazel's cycle detection in BUILD
files. When you have a circular dependency, Bazel reports the SCC. The condensation DAG
then drives topological scheduling of build actions.

**PostgreSQL query planner**: The planner uses SCC to identify correlated subqueries
and cyclic join graphs. Nodes in the same SCC are materialized together before outer
query processing.

**Kubernetes scheduler**: Bipartite matching (Hopcroft-Karp variant) underlies the
scheduler's pod-to-node assignment. The extended version handles affinity constraints
by adding/removing edges dynamically.

**Telecommunications network design (AT&T, circa 1990s)**: Gomory-Hu trees were used
to find the minimum cut in backbone networks. The all-pairs max-flow in a single tree
traversal enabled engineers to identify the globally weakest link for network hardening.

**Competitive programming judges and databases**: HLD appears in PostgreSQL's internal
tree structure queries (for hierarchical data) and in range-tree indices used by
PostGIS for spatial queries on hierarchical regions.

## Complexity Analysis

| Algorithm | Time | Space | Notes |
|-----------|------|-------|-------|
| Tarjan SCC | O(V + E) | O(V) | Linear; constant ≈ 2–3 DFS passes |
| Hopcroft-Karp | O(E√V) | O(V + E) | Tight bound; in practice √V rounds ≈ 5–10 |
| Gomory-Hu tree | O(n × maxflow) | O(n²) for the flow calls | With push-relabel: O(n³ V²/³) |
| HLD build | O(n) | O(n) | Two DFS passes |
| HLD path query | O(log² n) | O(1) per query | O(log n) if segment tree replaced with BIT |

**Hopcroft-Karp in practice**: The √V bound is achieved for dense random graphs.
For sparse graphs (the typical case in scheduling), the number of BFS rounds is much
smaller — often 3–5 for practical instances. The BFS phase uses O(E) work; the DFS
phase uses O(V) amortized (each node is "blocked" at most once per round).

## Common Pitfalls

1. **Tarjan: using `disc[u]` directly as `low` update for back edges**: Some implementations
   update `low[v] = min(low[v], low[u])` for back edges instead of `min(low[v], disc[u])`.
   This is wrong — you must use the discovery time, not the `low` value, for back edges.

2. **Hopcroft-Karp: not resetting `dist` to INF for matched nodes at BFS start**: Every
   matched left node must start with `dist = INF`; only free left nodes start at 0.
   Forgetting this causes the BFS to explore already-matched nodes as if they were free.

3. **HLD: query spanning two different chains includes the LCA twice**: When both `head[u]`
   and `head[v]` reach the same ancestor, the LCA node itself must be counted exactly once.
   The standard fix: after the while loop, query `[pos[u], pos[v]]` (inclusive both ends).

4. **HLD: edge weights vs. node weights**: The description here uses node weights. For edge
   weights, store the weight of edge (parent→child) on the child node, and exclude the LCA
   node from the final query (query `[pos[u], pos[v]-1]` with depth[u] < depth[v]).

5. **Gomory-Hu: incorrect "redirect" step**: After finding the min cut between s and t,
   the redirect step reassigns some tree edges. Skipping this step turns the algorithm
   into n max-flow calls that may not cover all pairs. Use a validated reference implementation.

## Exercises

**Exercise 1 — Verification** (30 min):
Implement Tarjan's SCC and verify it on the graph of Wikipedia "2-satisfiability" (2-SAT).
2-SAT reduces to SCC: a formula is satisfiable iff no variable and its negation are in the
same SCC. Construct a known-satisfiable and known-unsatisfiable 2-SAT instance, run SCC,
and verify the decision is correct.

**Exercise 2 — Extension** (2–4 h):
Extend the Hopcroft-Karp implementation to return the actual matching (not just the count),
and implement the König's theorem corollary to find the minimum vertex cover from the
maximum matching. Verify on a job-scheduling instance: 8 workers, 10 shifts, random
compatibility matrix.

**Exercise 3 — From Scratch** (4–8 h):
Implement the full Gomory-Hu tree construction using Ford-Fulkerson (or push-relabel) as
the underlying max-flow primitive. Verify that for every pair (s, t) in a 10-node graph,
`min(edge weights on s-t path in Gomory-Hu tree) == max-flow(s, t)` in the original graph.
Benchmark against n² individual max-flow calls.

**Exercise 4 — Production Scenario** (8–15 h):
Build a dependency resolver for a package manager. Input: a directed graph of package
dependencies (with version constraints as edge labels). Use Tarjan SCC to detect cycles
and report them clearly. Use HLD to answer "what is the maximum version constraint
tightness on the path between two packages in the dependency tree?" Expose this as a
CLI tool (Go or Rust). Include tests for the cycle detection, the path query, and a
fuzzer that generates random dependency graphs and verifies the resolver's output.

## Further Reading

### Foundational Papers
- Tarjan, R. E. (1972). "Depth-first search and linear graph algorithms." *SIAM Journal
  on Computing*, 1(2), 146–160. The original SCC paper; the low-link concept is defined here.
- Hopcroft, J., & Karp, R. (1973). "An n^(5/2) algorithm for maximum matchings in
  bipartite graphs." *SIAM Journal on Computing*, 2(4), 225–231.
- Gomory, R. E., & Hu, T. C. (1961). "Multi-terminal network flows." *Journal of the
  Society for Industrial and Applied Mathematics*, 9(4), 551–570.

### Books
- *Introduction to Algorithms (CLRS)* — Chapter 22 (DFS, SCC), Chapter 26 (max-flow).
- *Algorithm Design* — Kleinberg & Tardos. Chapter 7 (network flow) and Chapter 3 (graphs).
- *Competitive Programming 4* — Halim et al. Volume 2 for HLD and advanced matching.

### Production Code to Read
- **Bazel source** (`src/main/java/com/google/devtools/build/lib/graph/`): `DFS.java` and
  `Digraph.java` contain the SCC implementation used in dependency analysis.
- **OR-Tools bipartite matching** (`ortools/graph/linear_assignment.h`): Production
  implementation of the Hungarian algorithm with Hopcroft-Karp-style acceleration.
- **PostGIS** (`postgis/liblwgeom/lwgeodetic_tree.c`): HLD applied to geographic tree
  traversal in spatial queries.

### Conference Talks
- "Graph Algorithms in the Real World" — MIT OpenCourseWare 6.046J Lecture 20.
- "Network Flow in Practice: From Algorithms to Production" — Strange Loop 2018.
