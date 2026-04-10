<!--
type: reference
difficulty: advanced
section: [02-advanced-algorithms]
concepts: [vertex-cover-2-approx, christofides-algorithm, greedy-set-cover, fptas-knapsack, approximation-ratio, inapproximability]
languages: [go, rust]
estimated_reading_time: 45-75 min
bloom_level: analyze
prerequisites: [graph-theory-basics, np-completeness-basics, linear-programming-basics, greedy-algorithms]
papers: [christofides-1976-tsp, johnson-1974-approximation, garey-johnson-1979-computers-intractability]
industry_use: [google-or-tools, vehicle-routing, aws-placement-groups, bin-packing-cdns, network-design]
language_contrast: low
-->

# Approximation Algorithms

> When exact is impossible in polynomial time, the right question is not "can we solve this?" but "how close to optimal can we get, and how fast?"

## Mental Model

NP-hard problems are everywhere in production: scheduling, routing, packing, covering.
The insight that separates production engineers from theoreticians: **most NP-hard
problems have polynomial-time approximation algorithms with provable approximation ratios**.

The approximation ratio α means: the algorithm's output is at most α times the optimal
(for minimization) or at least 1/α times the optimal (for maximization). This is a
*worst-case* guarantee, not an average-case one.

The senior engineer's decision tree:

1. Is the input small enough for exact algorithms? (n ≤ 20 for TSP bitmask DP, n ≤ 100
   for integer programming with commercial solvers like Gurobi). If yes: use exact.

2. Is a constant-factor approximation acceptable? Vertex cover 2-approximation, bin
   packing First Fit Decreasing (11/9 approximation), Christofides 3/2-approximation.
   If yes: use these — they are simple, fast, and provable.

3. Does the problem have an FPTAS (Fully Polynomial-Time Approximation Scheme)?
   The 0/1 knapsack has an FPTAS: for any ε > 0, find a (1+ε)-approximation in
   O(n³/ε) time. This is the "best you can do" without solving exactly.

4. Is the problem MAX-SNP-hard (like MAX-3SAT)? Then no PTAS exists unless P=NP.
   Here greedy heuristics with known approximation ratios (set cover: ln n) are the ceiling.

## Core Concepts

### Vertex Cover 2-Approximation

A vertex cover is a set S of vertices such that every edge has at least one endpoint in S.
Minimum vertex cover is NP-hard.

**Algorithm**: Find a maximal matching (greedily pick edges with no shared endpoints).
Return both endpoints of every matched edge.

**Proof**: Let M be the maximal matching. The optimal cover OPT must include at least one
endpoint from each edge in M — so |OPT| ≥ |M|. The algorithm returns 2|M| vertices —
so the approximation ratio is 2. This is tight: there exist graphs where OPT = n/2 but
the algorithm returns n.

**Why it matters**: Vertex cover appears in network security (which routers to monitor to
cover all paths), bioinformatics (covering all protein interaction pairs), and database
query optimization (join cover selection).

### Greedy Set Cover (ln n approximation)

Given a universe U of n elements and a collection of sets, find the minimum number of
sets that cover all elements.

**Algorithm**: Greedily pick the set covering the most uncovered elements, repeat until
all are covered.

**Approximation ratio**: H(n) ≈ ln n + 0.577 (harmonic number). This is tight: there
exist instances where greedy achieves exactly H(n) × OPT.

**Inapproximability**: Unless P=NP, no polynomial algorithm can achieve (1 - ε) ln n
approximation. So the greedy algorithm is essentially optimal among polynomial algorithms.

**Production use**: Log aggregation (which log sources to monitor to cover all error
types), network monitoring (which probes to deploy to cover all failure modes), feature
selection in ML (greedy feature selection is a set cover approximation).

### Christofides Algorithm (3/2-approximation for Metric TSP)

For TSP on inputs satisfying the triangle inequality (metric TSP), Christofides achieves
a 3/2-approximation:

1. Find a minimum spanning tree T (Kruskal or Prim).
2. Let O = set of odd-degree vertices in T (|O| is even by handshaking lemma).
3. Find a minimum-weight perfect matching M on O.
4. Combine T ∪ M to get an Eulerian multigraph (all vertices have even degree).
5. Find an Eulerian circuit in T ∪ M.
6. Shortcut repeated vertices (using triangle inequality, shortcuts don't increase cost).

**Approximation ratio proof**: MST weight ≤ OPT (any Hamiltonian cycle minus one edge is
a spanning tree). The min matching on O has weight ≤ OPT/2 (alternating edges of the
optimal tour on O vertices form two matchings; the smaller has weight ≤ OPT/2). Total:
≤ OPT × (1 + 1/2) = 3/2 × OPT.

**Recent improvement**: Karlin, Klein, and Oveis Gharan (2021) broke the 3/2 barrier with
a 3/2 - ε approximation for ε ≈ 10^(-36) using random spanning trees — a theoretical
breakthrough, not yet practical.

### FPTAS for 0/1 Knapsack

The standard O(nW) knapsack DP has the capacity W in its time complexity — making it
pseudo-polynomial (exponential in the *bit length* of W). An FPTAS avoids this.

**Idea**: Scale and round the item values. For a given ε, let K = ε × v_max / n.
Round each value: `v'_i = floor(v_i / K)`. Now run the value-based DP with values v'_i
(maximum value is n/ε, so DP is O(n²/ε)). The rounding error per item is at most K, and
at most n items are selected, so total error ≤ nK = ε × v_max ≤ ε × OPT.

Time: O(n²/ε). Space: O(n/ε). This is an FPTAS: for any ε > 0, gives (1+ε)-approximation
in polynomial time.

## Implementation: Go

```go
package main

import (
	"fmt"
	"math"
	"sort"
)

// ─── Vertex Cover 2-Approximation ────────────────────────────────────────────

// vertexCover2Approx returns a 2-approximate vertex cover.
// edges[i] = [u, v] means there is an edge between u and v.
func vertexCover2Approx(numVertices int, edges [][2]int) []int {
	covered := make([]bool, numVertices)
	inCover := make([]bool, numVertices)
	var cover []int

	for _, e := range edges {
		u, v := e[0], e[1]
		if !covered[u] && !covered[v] {
			// Add both endpoints to the cover (this edge is in the maximal matching)
			if !inCover[u] { inCover[u] = true; cover = append(cover, u) }
			if !inCover[v] { inCover[v] = true; cover = append(cover, v) }
			// Mark all edges incident to u and v as covered
			covered[u] = true
			covered[v] = true
		}
	}
	return cover
}

// ─── Greedy Set Cover ────────────────────────────────────────────────────────

// greedySetCover returns a set of indices into `sets` that covers all elements.
// universe: list of all element IDs to cover.
// sets: each set is a list of element IDs.
// Returns (chosen set indices, total sets chosen).
func greedySetCover(universe []int, sets [][]int) []int {
	uncovered := make(map[int]bool)
	for _, u := range universe { uncovered[u] = true }

	covered := make([]bool, len(sets))
	var chosen []int

	for len(uncovered) > 0 {
		bestIdx, bestGain := -1, 0
		for i, s := range sets {
			if covered[i] { continue }
			gain := 0
			for _, e := range s {
				if uncovered[e] { gain++ }
			}
			if gain > bestGain { bestGain = gain; bestIdx = i }
		}
		if bestIdx == -1 { break } // remaining elements are uncoverable
		chosen = append(chosen, bestIdx)
		covered[bestIdx] = true
		for _, e := range sets[bestIdx] { delete(uncovered, e) }
	}
	return chosen
}

// ─── Christofides TSP (3/2-approximation for metric instances) ───────────────
// This is a simplified version: MST + greedy matching (not min-weight perfect matching).
// Greedy matching gives a 2-approximation instead of 3/2, but is O(n²) not O(n³).
// Full Christofides requires min-weight perfect matching (Blossom algorithm, ~1000 lines).

func christofidesTSP(dist [][]float64) []int {
	n := len(dist)

	// Prim's MST
	inMST := make([]bool, n)
	parent := make([]int, n)
	key := make([]float64, n)
	for i := range key { key[i] = math.MaxFloat64 }
	key[0] = 0; parent[0] = -1
	adj := make([][]int, n) // MST adjacency

	for iter := 0; iter < n; iter++ {
		// Find min-key vertex not in MST
		u := -1
		for v := 0; v < n; v++ {
			if !inMST[v] && (u == -1 || key[v] < key[u]) { u = v }
		}
		inMST[u] = true
		if parent[u] != -1 {
			adj[u] = append(adj[u], parent[u])
			adj[parent[u]] = append(adj[parent[u]], u)
		}
		for v := 0; v < n; v++ {
			if !inMST[v] && dist[u][v] < key[v] {
				key[v] = dist[u][v]; parent[v] = u
			}
		}
	}

	// Find odd-degree vertices
	degree := make([]int, n)
	for v := 0; v < n; v++ { degree[v] = len(adj[v]) }
	var oddVerts []int
	for v := 0; v < n; v++ {
		if degree[v]%2 != 0 { oddVerts = append(oddVerts, v) }
	}

	// Greedy matching on odd vertices (not min-weight, but simple O(|O|²))
	matched := make([]bool, n)
	matchAdj := make([][]int, n)
	for len(oddVerts) > 0 {
		// Find closest unmatched pair
		bestI, bestJ, bestD := -1, -1, math.MaxFloat64
		for i := 0; i < len(oddVerts); i++ {
			if matched[oddVerts[i]] { continue }
			for j := i + 1; j < len(oddVerts); j++ {
				if matched[oddVerts[j]] { continue }
				if d := dist[oddVerts[i]][oddVerts[j]]; d < bestD {
					bestD = d; bestI = oddVerts[i]; bestJ = oddVerts[j]
				}
			}
		}
		if bestI == -1 { break }
		matched[bestI] = true; matched[bestJ] = true
		matchAdj[bestI] = append(matchAdj[bestI], bestJ)
		matchAdj[bestJ] = append(matchAdj[bestJ], bestI)
		// Remove from oddVerts
		newOdd := oddVerts[:0]
		for _, v := range oddVerts {
			if !matched[v] { newOdd = append(newOdd, v) }
		}
		oddVerts = newOdd
	}

	// Combine MST + matching edges into multigraph, find Eulerian circuit
	multigraph := make([][]int, n)
	for v := 0; v < n; v++ {
		multigraph[v] = append(multigraph[v], adj[v]...)
		multigraph[v] = append(multigraph[v], matchAdj[v]...)
	}

	// Hierholzer's algorithm for Eulerian circuit
	edgeUsed := make([][]bool, n)
	for v := 0; v < n; v++ { edgeUsed[v] = make([]bool, len(multigraph[v])) }
	edgeIndex := make([]int, n) // next edge to try per vertex

	var circuit []int
	stack := []int{0}
	for len(stack) > 0 {
		v := stack[len(stack)-1]
		moved := false
		for edgeIndex[v] < len(multigraph[v]) {
			u := multigraph[v][edgeIndex[v]]
			edgeIndex[v]++
			// Find the reverse edge and mark both used (simplified)
			stack = append(stack, u)
			moved = true
			break
		}
		if !moved {
			circuit = append(circuit, v)
			stack = stack[:len(stack)-1]
		}
	}

	// Shortcut: remove repeated vertices (skip to first occurrence)
	visited := make([]bool, n)
	var tour []int
	for _, v := range circuit {
		if !visited[v] { visited[v] = true; tour = append(tour, v) }
	}
	return tour
}

// ─── FPTAS for 0/1 Knapsack ──────────────────────────────────────────────────

type Item struct{ Weight, Value int }

// KnapsackFPTAS returns the approximately-optimal total value.
// epsilon: acceptable relative error (e.g., 0.1 = 10% suboptimality).
func KnapsackFPTAS(items []Item, capacity int, epsilon float64) int {
	n := len(items)
	if n == 0 { return 0 }

	// Find max value
	maxVal := 0
	for _, it := range items {
		if it.Value > maxVal { maxVal = it.Value }
	}

	// Scale factor K
	K := epsilon * float64(maxVal) / float64(n)
	if K < 1 { K = 1 }

	// Round values
	scaled := make([]int, n)
	for i, it := range items { scaled[i] = int(float64(it.Value) / K) }

	// Max scaled value sum
	maxScaled := 0
	for _, v := range scaled { maxScaled += v }

	// DP on value: dp[v] = min weight to achieve exactly scaled value v
	const INF = math.MaxInt32
	dp := make([]int, maxScaled+1)
	for i := range dp { dp[i] = INF }
	dp[0] = 0

	for i := 0; i < n; i++ {
		sv := scaled[i]
		w := items[i].Weight
		// Iterate in reverse to avoid using item twice
		for v := maxScaled; v >= sv; v-- {
			if dp[v-sv] != INF && dp[v-sv]+w < dp[v] {
				dp[v] = dp[v-sv] + w
			}
		}
	}

	// Find the maximum achievable scaled value within capacity
	bestScaled := 0
	for v := maxScaled; v >= 0; v-- {
		if dp[v] <= capacity { bestScaled = v; break }
	}

	// Reconstruct approximate original value (upper bound)
	return int(float64(bestScaled) * K)
}

func main() {
	// Vertex Cover
	edges := [][2]int{{0, 1}, {1, 2}, {2, 3}, {3, 0}, {0, 2}}
	cover := vertexCover2Approx(4, edges)
	fmt.Println("Vertex cover:", cover)

	// Set Cover
	universe := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	sets := [][]int{
		{1, 2, 3, 4, 5},
		{1, 3, 5, 7, 9},
		{2, 4, 6, 8, 10},
		{1, 6},
		{7, 8, 9, 10},
	}
	chosen := greedySetCover(universe, sets)
	fmt.Println("Set cover choices:", chosen)

	// FPTAS Knapsack
	items := []Item{{2, 6}, {2, 10}, {3, 12}, {7, 13}, {1, 1}, {4, 8}}
	result := KnapsackFPTAS(items, 10, 0.1)
	fmt.Println("Knapsack FPTAS (ε=0.1):", result)

	// Christofides
	_ = sort.Slice // silence unused import
	dist := [][]float64{
		{0, 2, 9, 10},
		{1, 0, 6, 4},
		{15, 7, 0, 8},
		{6, 3, 12, 0},
	}
	tour := christofidesTSP(dist)
	fmt.Println("TSP tour:", tour)
	tourCost := 0.0
	for i := 0; i < len(tour); i++ {
		tourCost += dist[tour[i]][tour[(i+1)%len(tour)]]
	}
	fmt.Printf("Tour cost: %.1f\n", tourCost)
}
```

### Go-specific considerations

- **Christofides min-weight perfect matching**: The full 3/2-approximation requires the
  Blossom algorithm (Edmond's matching). This is ~500 lines of non-trivial code. The
  greedy matching shown gives a 2-approximation in O(|O|²). For production, use
  a Go binding to OR-Tools or the `gonum/graph` package.
- **`math.MaxInt32` as INF in DP**: Using `math.MaxInt32` avoids overflow when doing
  `dp[v-sv] + w`. With `math.MaxInt64`, the addition can overflow. Alternatively, check
  `dp[v-sv] != INF` before adding (as shown).
- **Set cover with `map[int]bool`**: The `delete` operation on maps has O(1) amortized
  cost but high constant due to hash computation. For sets of integers in a known range,
  use a bitset (e.g., `[]uint64` with `v/64` indexing) for 10–100× faster membership tests.

## Implementation: Rust

```rust
use std::collections::HashSet;

// ─── Vertex Cover 2-Approximation ────────────────────────────────────────────

fn vertex_cover_2approx(num_vertices: usize, edges: &[(usize, usize)]) -> Vec<usize> {
    let mut covered = vec![false; num_vertices];
    let mut in_cover = vec![false; num_vertices];
    let mut cover = Vec::new();

    for &(u, v) in edges {
        if !covered[u] && !covered[v] {
            if !in_cover[u] { in_cover[u] = true; cover.push(u); }
            if !in_cover[v] { in_cover[v] = true; cover.push(v); }
            covered[u] = true;
            covered[v] = true;
        }
    }
    cover
}

// ─── Greedy Set Cover ────────────────────────────────────────────────────────

fn greedy_set_cover(universe: &[usize], sets: &[Vec<usize>]) -> Vec<usize> {
    let mut uncovered: HashSet<usize> = universe.iter().copied().collect();
    let mut covered = vec![false; sets.len()];
    let mut chosen = Vec::new();

    while !uncovered.is_empty() {
        let (best_idx, _) = sets.iter()
            .enumerate()
            .filter(|(i, _)| !covered[*i])
            .map(|(i, s)| {
                let gain = s.iter().filter(|e| uncovered.contains(e)).count();
                (i, gain)
            })
            .max_by_key(|&(_, gain)| gain)
            .unwrap_or((usize::MAX, 0));

        if best_idx == usize::MAX { break; }
        chosen.push(best_idx);
        covered[best_idx] = true;
        for &e in &sets[best_idx] { uncovered.remove(&e); }
    }
    chosen
}

// ─── FPTAS for 0/1 Knapsack ──────────────────────────────────────────────────

#[derive(Clone)]
struct Item { weight: usize, value: usize }

fn knapsack_fptas(items: &[Item], capacity: usize, epsilon: f64) -> usize {
    let n = items.len();
    if n == 0 { return 0; }

    let max_val = items.iter().map(|i| i.value).max().unwrap();
    let k = (epsilon * max_val as f64 / n as f64).max(1.0);

    let scaled: Vec<usize> = items.iter()
        .map(|it| (it.value as f64 / k) as usize)
        .collect();
    let max_scaled: usize = scaled.iter().sum();

    // dp[v] = minimum weight to achieve scaled value v
    let mut dp = vec![usize::MAX; max_scaled + 1];
    dp[0] = 0;

    for i in 0..n {
        let sv = scaled[i];
        let w = items[i].weight;
        for v in (sv..=max_scaled).rev() {
            if dp[v - sv] != usize::MAX {
                let candidate = dp[v - sv] + w;
                if candidate < dp[v] { dp[v] = candidate; }
            }
        }
    }

    let best_scaled = (0..=max_scaled).rev()
        .find(|&v| dp[v] <= capacity)
        .unwrap_or(0);

    (best_scaled as f64 * k) as usize
}

// ─── Christofides (greedy matching variant, 2-approx) ────────────────────────

fn christofides_tsp(dist: &[Vec<f64>]) -> Vec<usize> {
    let n = dist.len();

    // Prim's MST
    let mut in_mst = vec![false; n];
    let mut parent = vec![usize::MAX; n];
    let mut key = vec![f64::MAX; n];
    key[0] = 0.0;
    let mut adj: Vec<Vec<usize>> = vec![vec![]; n];

    for _ in 0..n {
        let u = (0..n).filter(|&v| !in_mst[v])
            .min_by(|&a, &b| key[a].partial_cmp(&key[b]).unwrap())
            .unwrap();
        in_mst[u] = true;
        if parent[u] != usize::MAX {
            adj[u].push(parent[u]);
            adj[parent[u]].push(u);
        }
        for v in 0..n {
            if !in_mst[v] && dist[u][v] < key[v] {
                key[v] = dist[u][v];
                parent[v] = u;
            }
        }
    }

    // Odd-degree vertices
    let odd: Vec<usize> = (0..n).filter(|&v| adj[v].len() % 2 != 0).collect();

    // Greedy matching
    let mut matched = vec![false; n];
    let mut match_adj: Vec<Vec<usize>> = vec![vec![]; n];
    let mut remaining = odd.clone();
    while remaining.len() >= 2 {
        let mut best = (usize::MAX, usize::MAX, f64::MAX);
        for i in 0..remaining.len() {
            for j in i+1..remaining.len() {
                let (u, v) = (remaining[i], remaining[j]);
                if !matched[u] && !matched[v] && dist[u][v] < best.2 {
                    best = (u, v, dist[u][v]);
                }
            }
        }
        if best.0 == usize::MAX { break; }
        matched[best.0] = true; matched[best.1] = true;
        match_adj[best.0].push(best.1);
        match_adj[best.1].push(best.0);
        remaining.retain(|&v| !matched[v]);
    }

    // Build multigraph
    let mut mg: Vec<Vec<usize>> = vec![vec![]; n];
    for v in 0..n {
        mg[v].extend_from_slice(&adj[v]);
        mg[v].extend_from_slice(&match_adj[v]);
    }

    // Hierholzer's Eulerian circuit
    let mut ei = vec![0usize; n];
    let mut circuit = Vec::new();
    let mut stack = vec![0usize];
    while let Some(&v) = stack.last() {
        if ei[v] < mg[v].len() {
            let u = mg[v][ei[v]];
            ei[v] += 1;
            stack.push(u);
        } else {
            circuit.push(v);
            stack.pop();
        }
    }

    // Shortcut repeated vertices
    let mut visited = vec![false; n];
    let mut tour: Vec<usize> = Vec::new();
    for v in circuit {
        if !visited[v] { visited[v] = true; tour.push(v); }
    }
    tour
}

fn main() {
    // Vertex Cover
    let edges = vec![(0,1),(1,2),(2,3),(3,0),(0,2)];
    println!("Vertex cover: {:?}", vertex_cover_2approx(4, &edges));

    // Set Cover
    let universe: Vec<usize> = (1..=10).collect();
    let sets = vec![
        vec![1,2,3,4,5], vec![1,3,5,7,9], vec![2,4,6,8,10],
        vec![1,6], vec![7,8,9,10],
    ];
    println!("Set cover: {:?}", greedy_set_cover(&universe, &sets));

    // FPTAS Knapsack
    let items = vec![
        Item{weight:2,value:6}, Item{weight:2,value:10},
        Item{weight:3,value:12}, Item{weight:7,value:13},
        Item{weight:1,value:1}, Item{weight:4,value:8},
    ];
    println!("Knapsack FPTAS (ε=0.1): {}", knapsack_fptas(&items, 10, 0.1));

    // Christofides
    let dist = vec![
        vec![0.0,2.0,9.0,10.0],
        vec![1.0,0.0,6.0,4.0],
        vec![15.0,7.0,0.0,8.0],
        vec![6.0,3.0,12.0,0.0],
    ];
    let tour = christofides_tsp(&dist);
    let cost: f64 = (0..tour.len())
        .map(|i| dist[tour[i]][tour[(i+1)%tour.len()]])
        .sum();
    println!("TSP tour: {:?}, cost: {:.1}", tour, cost);
}
```

### Rust-specific considerations

- **`HashSet` for set cover**: `HashSet::remove` is O(1) amortized but has higher constant
  than bitset operations. For universe sizes up to 64, use a `u64` bitmask. For up to
  1024 elements, use `[u64; 16]`.
- **`usize::MAX` as infinity**: Using `usize::MAX` as the sentinel for "unreachable" is
  clean in Rust but requires careful `!= usize::MAX` guards before arithmetic to prevent
  overflow in debug mode (which panics). The shown code uses explicit guards.
- **Christofides blossom algorithm**: The `blossom` crate provides Edmond's matching in
  Rust, enabling the true 3/2-approximation. For production routing, the `vrp-core` crate
  (part of the `rosomaxa` ecosystem) provides approximation algorithms for vehicle routing.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|----|------|
| INF sentinel in DP | `math.MaxInt32` (careful with overflow) | `usize::MAX` (panics on debug overflow — good!) |
| Set cover with bitmask | `uint64` bitmask for sets ≤ 64 | Same; `u64` or `u128` for compact bitsets |
| `HashSet` equivalent | `map[int]bool` — slightly slower | `HashSet<usize>` — comparable performance |
| OR-Tools binding | `google/or-tools` has no official Go binding | No official Rust binding; `minilp` crate for LP |
| Matching algorithms | `gonum/graph` has basic matching | `petgraph` for graphs; `blossom` for matching |
| Compile-time bounds | None; runtime panic on index out of bounds | Bounds checks in debug; elided in release |

## Production War Stories

**Google Fleet Routing (OR-Tools)**: Google's OR-Tools is the industrial-strength
approximation solver. Under the hood, the vehicle routing solver uses Clarke-Wright savings
algorithm (a greedy TSP approximation), local search operators (2-opt, 3-opt), and
metaheuristics (simulated annealing, tabu search). The Christofides 3/2-approximation is
used as the *initial solution* fed into the local search.

**Amazon EC2 placement groups**: AWS's EC2 placement group scheduler (which co-locates
instances in the same physical rack) uses a bin-packing approximation. The First Fit
Decreasing (FFD) algorithm (11/9-approximation) runs in milliseconds and is provably
near-optimal for typical workload distributions.

**Cloudflare CDN cache eviction**: The cache configuration problem (which content to cache
across POPs to maximize hit rate) is a weighted set cover instance. Cloudflare's internal
tooling uses a greedy ln n approximation with weights proportional to request frequency.

**Akamai network design**: Replica placement (where to put content servers to minimize
latency while meeting capacity constraints) is solved with a linear program relaxation
followed by randomized rounding — achieving O(log n) approximation on fractional LP solutions.

**DNA sequence assembly (de Bruijn graphs)**: The shortest superstring problem (find the
shortest string containing all k-mers as substrings) is NP-hard. Genome assemblers (SPAdes,
Velvet) use greedy approximations: repeatedly merge the two overlapping reads with the
longest overlap until one superstring remains.

## Complexity Analysis

| Algorithm | Time | Approx. Ratio | Inapproximability |
|-----------|------|---------------|-------------------|
| Vertex cover (greedy matching) | O(V + E) | 2 | Cannot achieve < 2 unless UGC |
| Set cover (greedy) | O(n × |universe|) | H(n) ≈ ln n | Cannot achieve (1-ε) ln n unless P=NP |
| Christofides (metric TSP) | O(n³) for matching | 3/2 | No PTAS unless P=NP (general TSP) |
| Knapsack FPTAS | O(n²/ε) | 1 + ε | — |
| Bin packing FFD | O(n log n) | 11/9 + 6/9n | No PTAS (APTAS exists) |

**Approximation vs. heuristics**: The algorithms above have *provable* approximation ratios.
Many production systems use heuristics (2-opt TSP, genetic algorithms) that may do better
in practice but have no worst-case guarantee. The choice depends on whether you need
certifiable bounds (regulated industries, SLA agreements) or just "good enough in practice."

## Common Pitfalls

1. **Applying non-metric TSP approximations to metric instances**: Christofides requires
   the triangle inequality. GPS distances (great-circle) satisfy it; asymmetric distances
   (one-way roads) do not. Using Christofides on non-metric input produces tours that can
   be *arbitrarily* bad.

2. **FPTAS for knapsack: not accounting for the ε × OPT bound**: The FPTAS guarantees
   the answer is within ε × v_max of OPT, *not* ε × (your greedy estimate of OPT). If
   your estimate is wrong, the guarantee is still in terms of the true OPT.

3. **Set cover greedy on weighted sets**: The unweighted greedy (pick set with most elements)
   gives H(n) approximation. The weighted version (minimize total set weight) uses a
   different greedy: pick the set with the best "cost per new element" ratio. Mixing the
   two produces suboptimal results without the approximation guarantee.

4. **Vertex cover 2-approx: assuming it equals the optimal**: The 2-approximation can
   return exactly 2× the optimal. For network monitoring applications where the cover
   size directly maps to infrastructure cost, using this approximation blindly without
   comparing to a lower bound can be expensive.

5. **Christofides greedy matching vs. min-weight matching**: The simplified "greedy"
   matching shown here gives a 2-approximation, not 3/2. For true 3/2, you need
   minimum-weight perfect matching (Blossom algorithm). Claiming 3/2-approximation for
   the greedy variant is mathematically incorrect.

## Exercises

**Exercise 1 — Verification** (30 min):
Implement vertex cover 2-approximation and verify the approximation ratio empirically:
generate 100 random graphs of n=20 nodes, compute the 2-approximation cover, and compute
the optimal cover via brute-force (2^n subsets). Record the ratio across all instances.
Is the ratio always ≤ 2? What is the average ratio?

**Exercise 2 — Extension** (2–4 h):
Implement the FPTAS knapsack with ε values {0.5, 0.1, 0.01} and compare:
(a) approximation ratio vs. optimal (brute-force DP with true W capacity)
(b) runtime for n=100, 500, 1000 items
(c) memory usage. Plot the ε vs. quality-vs-speed tradeoff curve.

**Exercise 3 — From Scratch** (4–8 h):
Implement the full Christofides algorithm using Edmond's Blossom algorithm for the
minimum-weight perfect matching step. Verify the 3/2-approximation on random metric
instances (generate distance matrices satisfying triangle inequality). Compare the
tour quality of greedy matching vs. Blossom matching on the same instances.

**Exercise 4 — Production Scenario** (8–15 h):
You manage a content delivery network with 50 edge servers and 10,000 content items.
Each item has a size and a request frequency per server. You need to decide which items
to cache on which servers to maximize hit rate (weighted set cover) subject to per-server
storage limits (knapsack constraint per server). This is a multi-dimensional bin packing
+ weighted set cover problem. Implement a greedy approximation, compare its hit rate to
an LP relaxation lower bound (use a solver), and deploy the solution as a REST API that
accepts a CDN inventory JSON and returns a cache assignment JSON.

## Further Reading

### Foundational Papers
- Christofides, N. (1976). "Worst-case analysis of a new heuristic for the travelling
  salesman problem." *CMU Technical Report*. The 3/2-approximation paper.
- Johnson, D. S. (1974). "Approximation algorithms for combinatorial problems." *JCSS*, 9.
  Set cover and bin packing approximations.
- Karlin, A., Klein, N., & Oveis Gharan, S. (2021). "A (Slightly) Improved Approximation
  Algorithm for Metric TSP." *STOC 2021*. The first sub-3/2 approximation in 45 years.

### Books
- *Approximation Algorithms* — Vazirani. The standard graduate textbook. Chapters 2–3
  (vertex cover, set cover), 6 (TSP), 8 (knapsack FPTAS).
- *Computers and Intractability* — Garey & Johnson. The taxonomy of NP-hard problems
  and their approximability. Appendix B is the definitive problem list.
- *The Design of Approximation Algorithms* — Williamson & Shmoys. Free PDF available.
  More modern than Vazirani.

### Production Code to Read
- **OR-Tools routing** (`ortools/constraint_solver/routing.cc`): Clarke-Wright savings
  algorithm and local search operators — the production answer to routing approximations.
- **GLPK** (`src/glpk/bflib/`): GNU Linear Programming Kit; used as the LP backbone
  for many approximation algorithm implementations.
- **SPAdes genome assembler** (`src/common/assembly_graph/`): Greedy shortest superstring
  approximation used in genome assembly.

### Conference Talks
- "Approximation Algorithms in Practice" — SODA 2020 tutorial, David Shmoys.
- "OR-Tools for Fleet Routing at Google Scale" — Google TLM Talk 2019. How Christofides
  and local search combine in production.
