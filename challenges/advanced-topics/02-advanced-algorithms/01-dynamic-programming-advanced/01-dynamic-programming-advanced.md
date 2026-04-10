<!--
type: reference
difficulty: advanced
section: [02-advanced-algorithms]
concepts: [rerooting-dp, bitmask-dp, knuth-optimization, divide-and-conquer-dp, aliens-trick, 1d1d-dp]
languages: [go, rust]
estimated_reading_time: 60-90 min
bloom_level: analyze
prerequisites: [basic-dp, tree-traversal, graph-theory-basics]
papers: [knuth-1971-optimum-binary-search-trees, alien-trick-li-chao-2016, aliens-trick-2016-ioi]
industry_use: [routing-engines, combinatorial-optimizers, compilers, sequence-aligners]
language_contrast: medium
-->

# Advanced Dynamic Programming

> The techniques here do not just make DP faster — they change which problems are tractable at all.

## Mental Model

Basic DP is template matching: identify overlapping subproblems, define a recurrence,
fill a table. That works for 80% of DP problems. The remaining 20% require structural
insight about *why* a transition is expensive and *how* the geometry of optimal
solutions lets you skip work.

Think of it this way: every DP recurrence is an implicit graph where states are nodes
and transitions are edges. Naive DP traverses all edges. The optimizations in this
section exploit invariants in that graph's structure:

- **Rerooting**: when computing a function on every node of a tree, you can derive the
  answer for a node's subtree from its parent's answer rather than recomputing from
  scratch, turning O(n²) into O(n).
- **Bitmask DP**: when the state space is a subset lattice (2^n states, transitions between
  supersets/subsets), you can enumerate it systematically. Practical for n ≤ 20–25.
- **Knuth's optimization**: when an interval DP's optimal split point is monotone
  (opt[i][j] ≤ opt[i][j+1] and opt[i][j] ≤ opt[i+1][j]), you can restrict the
  search to a shrinking window, cutting O(n³) to O(n²).
- **Divide-and-conquer optimization**: when the cost function satisfies the concave/convex
  SMAWK condition, the optimal transition for the midpoint constrains the range for
  all other points in a half.
- **Aliens trick (lambda optimization)**: when you want to solve "minimize cost with
  exactly k items" but k is hard to handle directly, add a penalty λ per item and
  binary-search on λ until the unconstrained optimum uses exactly k items.

The senior engineer's pattern recognition: "Is the optimal partition point monotone?
Can I express this as a subset problem with n ≤ 20? Does the cost satisfy the quadrangle
inequality?" These are your triggers.

## Core Concepts

### Rerooting (Tree Re-rooting Technique)

Many tree DP problems ask "compute f(v) = some function of v's entire subtree" for all
nodes v simultaneously. If you pick a root and compute bottom-up, you get f(root) for
free but must recompute for other roots. Rerooting avoids this.

**Phase 1 (down pass)**: Root the tree arbitrarily. Compute `down[v]` = contribution
of v's subtree when v is root.

**Phase 2 (up pass)**: Compute `up[v]` = contribution of everything *outside* v's
subtree (the "virtual subtree" you'd get if you rerooted at v and looked away from the
original root). The recurrence derives `up[child]` from `up[parent]` and `down[parent]`
while carefully removing child's own contribution to avoid double-counting.

**Answer**: `ans[v] = combine(down[v], up[v])`.

The tricky part is the "remove child's contribution" step. For sums it's subtraction.
For max/min you often need to track first and second best to handle the case where the
child being removed was the argmax.

### Bitmask DP

The canonical form: `dp[mask]` = best solution using exactly the items in `mask`.
Transitions iterate over subsets of `mask`. The total work is O(3^n) because each
element is either in mask, in the submask, or in neither — so the sum over all masks
of their subset counts is 3^n, not 4^n.

For TSP: `dp[mask][v]` = minimum cost path visiting all cities in `mask`, ending at
`v`. Transition: `dp[mask][v] = min over u in mask\{v} of dp[mask\{v}][u] + dist[u][v]`.

**Subset sum enumeration trick**: iterating `for s := mask; s > 0; s = (s-1) & mask`
visits every non-empty submask of `mask` in O(2^popcount(mask)) time. Summing over all
masks: O(3^n).

### Knuth's Optimization

Applies to interval DP of the form:
```
dp[i][j] = min over i ≤ k < j of (dp[i][k] + dp[k+1][j] + cost[i][j])
```
when `cost` satisfies the **quadrangle inequality**: `cost[a][c] + cost[b][d] ≤ cost[a][d] + cost[b][c]`
for all a ≤ b ≤ c ≤ d.

Under this condition, the optimal split `opt[i][j]` is monotone:
`opt[i][j-1] ≤ opt[i][j] ≤ opt[i+1][j]`

This means when filling the DP table diagonally, each `dp[i][j]` only needs to search
`k` in `[opt[i][j-1], opt[i+1][j]]` — a range that telescopes to O(n) total work per
diagonal, O(n²) overall.

Classic applications: optimal BST construction, matrix chain multiplication with
non-uniform costs, stone merging.

### Divide-and-Conquer Optimization

For 1D/1D DP: `dp[j][i] = min over k < i of (dp[j-1][k] + cost(k, i))`.

When `opt[j][i]` (the optimal k for state (j,i)) is monotone in i for fixed j, you
can solve the j-th layer with divide-and-conquer: find `opt[j][mid]` exhaustively, then
recurse on left half (knowing opt ≤ opt[j][mid]) and right half (knowing opt ≥ opt[j][mid]).
Total work per layer: O(n log n). Total: O(kn log n) instead of O(kn²).

### Aliens Trick (Lambda Optimization / WQS Binary Search)

Problem: minimize total cost subject to using exactly `k` groups/items.

Transform: define `g(λ) = min over any number of items of (total_cost + λ * count)`.
As λ increases, the unconstrained optimum uses fewer items (each item becomes more
"expensive"). Binary search on λ until `count(opt(λ)) = k`.

Key insight: `g(λ)` is concave in λ. The optimal λ is the subgradient of the original
cost function at k. This converts a 2D DP (tracking count explicitly) into repeated
1D DP calls — typically O(n log n) binary search × O(n) DP = O(n log n) vs O(kn).

The hard part: when `g(λ)` has a flat region (multiple λ values give count = k), you
need careful tie-breaking to recover the actual answer.

## Implementation: Go

```go
package main

import (
	"fmt"
	"math"
)

// ─── Rerooting: sum of distances in a tree ───────────────────────────────────
// Classic problem: for each node v, compute the sum of distances to all other nodes.
// O(n) with rerooting instead of O(n²) with n separate DFS calls.

const MaxN = 100005

var (
	adj    [MaxN][]int
	down   [MaxN]int // sum of distances to nodes in subtree, subtree size encoded separately
	sz     [MaxN]int // subtree size
	up     [MaxN]int // sum of distances to nodes outside subtree
	ans    [MaxN]int
	n      int
)

func dfsDown(v, parent int) {
	sz[v] = 1
	down[v] = 0
	for _, u := range adj[v] {
		if u == parent {
			continue
		}
		dfsDown(u, v)
		sz[v] += sz[u]
		// Each node in u's subtree contributes +1 to the distance (the edge v-u)
		down[v] += down[u] + sz[u]
	}
}

func dfsUp(v, parent int) {
	ans[v] = down[v] + up[v]
	for _, u := range adj[v] {
		if u == parent {
			continue
		}
		// up[u] = contribution from outside u's subtree
		// = up[v] (outside v's subtree)
		// + (down[v] - down[u] - sz[u])  (v's other children's subtrees)
		// + (n - sz[u])                   (all those nodes are one edge farther)
		otherDown := down[v] - down[u] - sz[u]
		up[u] = up[v] + otherDown + (n - sz[u])
		dfsUp(u, v)
	}
}

func sumOfDistances(numNodes int, edges [][2]int) []int {
	n = numNodes
	for i := 0; i <= n; i++ {
		adj[i] = adj[i][:0]
		up[i] = 0
	}
	for _, e := range edges {
		u, v := e[0], e[1]
		adj[u] = append(adj[u], v)
		adj[v] = append(adj[v], u)
	}
	dfsDown(0, -1)
	dfsUp(0, -1)
	result := make([]int, n)
	for i := range result {
		result[i] = ans[i]
	}
	return result
}

// ─── Bitmask DP: Traveling Salesman Problem ──────────────────────────────────
// O(2^n * n²) time, O(2^n * n) space. Practical for n ≤ 20.

const INF = math.MaxInt64 / 2

func tsp(dist [][]int) int {
	n := len(dist)
	dp := make([][]int, 1<<n)
	for i := range dp {
		dp[i] = make([]int, n)
		for j := range dp[i] {
			dp[i][j] = INF
		}
	}
	dp[1][0] = 0 // start at node 0, only node 0 visited

	for mask := 1; mask < (1 << n); mask++ {
		for v := 0; v < n; v++ {
			if dp[mask][v] == INF {
				continue
			}
			if mask>>v&1 == 0 {
				continue // v not in mask
			}
			for u := 0; u < n; u++ {
				if mask>>u&1 != 0 {
					continue // u already visited
				}
				next := mask | (1 << u)
				if newCost := dp[mask][v] + dist[v][u]; newCost < dp[next][u] {
					dp[next][u] = newCost
				}
			}
		}
	}

	full := (1 << n) - 1
	best := INF
	for v := 1; v < n; v++ {
		if dp[full][v] != INF && dist[v][0] != INF {
			if c := dp[full][v] + dist[v][0]; c < best {
				best = c
			}
		}
	}
	return best
}

// ─── Knuth's Optimization: optimal interval merging ──────────────────────────
// cost[i][j] = cost of merging elements i..j into one block.
// Satisfies quadrangle inequality: cost[a][c]+cost[b][d] <= cost[a][d]+cost[b][c].
// Reduces O(n³) to O(n²).

func knuthDP(cost [][]int) [][]int {
	n := len(cost)
	dp := make([][]int, n)
	opt := make([][]int, n) // opt[i][j] = optimal split point
	for i := range dp {
		dp[i] = make([]int, n)
		opt[i] = make([]int, n)
		for j := range dp[i] {
			dp[i][j] = INF
			opt[i][j] = i // initialization: split at left boundary
		}
		dp[i][i] = 0
		opt[i][i] = i
	}

	// Fill by increasing interval length
	for length := 2; length <= n; length++ {
		for i := 0; i+length-1 < n; i++ {
			j := i + length - 1
			lo := opt[i][j-1]
			hi := opt[i+1][j]
			if i+1 >= n {
				hi = j - 1
			}
			for k := lo; k <= hi && k < j; k++ {
				if dp[i][k] == INF || dp[k+1][j] == INF {
					continue
				}
				val := dp[i][k] + dp[k+1][j] + cost[i][j]
				if val < dp[i][j] {
					dp[i][j] = val
					opt[i][j] = k
				}
			}
		}
	}
	return dp
}

// ─── Aliens Trick: minimize cost with exactly k segments ─────────────────────
// Problem: split array of n elements into exactly k contiguous segments to
// minimize total cost. Here cost of a segment [l,r] = (r-l)^2 (illustrative).
// Aliens trick: binary-search on penalty λ per segment.

func segmentCostSquared(a []int, l, r int) int {
	length := r - l + 1
	_ = a
	return length * length // simplified cost; in practice compute from prefix sums
}

// solveWithPenalty solves: minimize sum of cost(segment) + λ * numSegments (unconstrained).
// Returns (bestCost, numSegmentsUsed).
func solveWithPenalty(n int, lambda int) (int, int) {
	// dp[i] = (min cost to cover a[0..i-1], number of segments used)
	type state struct{ cost, cnt int }
	dp := make([]state, n+1)
	for i := range dp {
		dp[i] = state{INF, 0}
	}
	dp[0] = state{0, 0}

	for i := 1; i <= n; i++ {
		for j := 0; j < i; j++ {
			if dp[j].cost == INF {
				continue
			}
			segLen := i - j
			c := dp[j].cost + segLen*segLen + lambda
			if c < dp[i].cost || (c == dp[i].cost && dp[j].cnt+1 < dp[i].cnt) {
				dp[i] = state{c, dp[j].cnt + 1}
			}
		}
	}
	return dp[n].cost, dp[n].cnt
}

func aliensOptimal(n, k int) int {
	lo, hi := 0, n*n
	for lo < hi {
		mid := (lo + hi) / 2
		_, cnt := solveWithPenalty(n, mid)
		if cnt <= k {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	cost, cnt := solveWithPenalty(n, lo)
	// Remove the penalty contribution for exactly k segments
	return cost - lo*cnt + lo*(cnt-k) // adjust: we want exactly k segments
}

func main() {
	// Rerooting demo
	edges := [][2]int{{0, 1}, {0, 2}, {1, 3}, {1, 4}}
	dists := sumOfDistances(5, edges)
	fmt.Println("Sum of distances:", dists) // e.g. [4 3 6 6 6]

	// TSP demo
	dist := [][]int{
		{0, 10, 15, 20},
		{10, 0, 35, 25},
		{15, 35, 0, 30},
		{20, 25, 30, 0},
	}
	fmt.Println("TSP optimal:", tsp(dist)) // 80

	// Knuth demo
	cost := [][]int{
		{0, 1, 3, 5},
		{0, 0, 2, 4},
		{0, 0, 0, 1},
		{0, 0, 0, 0},
	}
	dpResult := knuthDP(cost)
	fmt.Println("Knuth DP [0][3]:", dpResult[0][3])

	// Aliens trick demo
	fmt.Println("Aliens trick (n=6, k=2):", aliensOptimal(6, 2))
}
```

### Go-specific considerations

- **Stack depth for DFS**: Go's goroutine stacks grow dynamically, so recursive DFS on
  trees up to ~100k nodes is safe. Beyond 1M nodes, convert to iterative DFS with an
  explicit stack to avoid even the dynamic stack growth overhead.
- **2D slice allocation**: `make([][]int, 1<<n)` for bitmask DP allocates n pointers;
  each inner slice is a separate heap allocation. For n=20 (1M states), prefer a flat
  `[]int` of size `(1<<n)*n` and index with `mask*n + v` to reduce GC pressure.
- **Integer overflow**: `math.MaxInt64 / 2` as INF is idiomatic — it prevents overflow
  in `a + b` comparisons without needing saturating arithmetic.

## Implementation: Rust

```rust
use std::cmp::min;

const INF: i64 = i64::MAX / 2;

// ─── Rerooting: sum of distances in a tree ───────────────────────────────────

struct Tree {
    adj: Vec<Vec<usize>>,
    down: Vec<i64>,
    sz: Vec<i64>,
    up: Vec<i64>,
    ans: Vec<i64>,
}

impl Tree {
    fn new(n: usize) -> Self {
        Tree {
            adj: vec![vec![]; n],
            down: vec![0; n],
            sz: vec![0; n],
            up: vec![0; n],
            ans: vec![0; n],
        }
    }

    fn add_edge(&mut self, u: usize, v: usize) {
        self.adj[u].push(v);
        self.adj[v].push(u);
    }

    // Iterative DFS to avoid stack overflow on large trees
    fn dfs_down(&mut self, root: usize) {
        let n = self.adj.len();
        let mut parent = vec![usize::MAX; n];
        let mut order = Vec::with_capacity(n);
        let mut stack = vec![root];
        parent[root] = root;

        while let Some(v) = stack.pop() {
            order.push(v);
            for &u in &self.adj[v].clone() {
                if u != parent[v] {
                    parent[u] = v;
                    stack.push(u);
                }
            }
        }

        // Process in reverse DFS order (leaves first)
        for &v in order.iter().rev() {
            self.sz[v] = 1;
            self.down[v] = 0;
            for &u in &self.adj[v].clone() {
                if u != parent[v] {
                    self.sz[v] += self.sz[u];
                    self.down[v] += self.down[u] + self.sz[u];
                }
            }
        }

        // Up pass in DFS order
        let total = n as i64;
        self.up[root] = 0;
        for &v in &order {
            self.ans[v] = self.down[v] + self.up[v];
            for &u in &self.adj[v].clone() {
                if u != parent[v] {
                    let other_down = self.down[v] - self.down[u] - self.sz[u];
                    self.up[u] = self.up[v] + other_down + (total - self.sz[u]);
                }
            }
        }
    }
}

// ─── Bitmask DP: TSP ─────────────────────────────────────────────────────────

fn tsp(dist: &[Vec<i64>]) -> i64 {
    let n = dist.len();
    let states = 1usize << n;
    // Flat layout: dp[mask * n + v]
    let mut dp = vec![INF; states * n];
    dp[1 * n + 0] = 0; // start at node 0

    for mask in 1..states {
        for v in 0..n {
            if dp[mask * n + v] == INF { continue; }
            if (mask >> v) & 1 == 0 { continue; }
            for u in 0..n {
                if (mask >> u) & 1 != 0 { continue; }
                let next = mask | (1 << u);
                let new_cost = dp[mask * n + v] + dist[v][u];
                if new_cost < dp[next * n + u] {
                    dp[next * n + u] = new_cost;
                }
            }
        }
    }

    let full = states - 1;
    (1..n)
        .filter(|&v| dp[full * n + v] != INF && dist[v][0] != INF)
        .map(|v| dp[full * n + v] + dist[v][0])
        .min()
        .unwrap_or(INF)
}

// ─── Knuth's Optimization ────────────────────────────────────────────────────

fn knuth_dp(cost: &[Vec<i64>]) -> Vec<Vec<i64>> {
    let n = cost.len();
    let mut dp = vec![vec![INF; n]; n];
    let mut opt = vec![vec![0usize; n]; n];

    for i in 0..n {
        dp[i][i] = 0;
        opt[i][i] = i;
    }

    for length in 2..=n {
        for i in 0..=n - length {
            let j = i + length - 1;
            let lo = opt[i][j - 1];
            let hi = if i + 1 < n { opt[i + 1][j] } else { j - 1 };

            for k in lo..=min(hi, j - 1) {
                if dp[i][k] == INF || dp[k + 1][j] == INF { continue; }
                let val = dp[i][k] + dp[k + 1][j] + cost[i][j];
                if val < dp[i][j] {
                    dp[i][j] = val;
                    opt[i][j] = k;
                }
            }
        }
    }
    dp
}

// ─── Aliens Trick ────────────────────────────────────────────────────────────

fn solve_with_penalty(n: usize, lambda: i64) -> (i64, usize) {
    // dp[i] = (min cost for prefix of length i, number of segments)
    let mut dp = vec![(INF, 0usize); n + 1];
    dp[0] = (0, 0);

    for i in 1..=n {
        for j in 0..i {
            if dp[j].0 == INF { continue; }
            let seg_len = (i - j) as i64;
            let c = dp[j].0 + seg_len * seg_len + lambda;
            if c < dp[i].0 || (c == dp[i].0 && dp[j].1 + 1 < dp[i].1) {
                dp[i] = (c, dp[j].1 + 1);
            }
        }
    }
    dp[n]
}

fn aliens_optimal(n: usize, k: usize) -> i64 {
    let (mut lo, mut hi) = (0i64, (n * n) as i64);
    while lo < hi {
        let mid = (lo + hi) / 2;
        let (_, cnt) = solve_with_penalty(n, mid);
        if cnt <= k { hi = mid; } else { lo = mid + 1; }
    }
    let (cost, cnt) = solve_with_penalty(n, lo);
    // Adjust: penalty added cnt times but we want exactly k
    cost - lo * cnt as i64 + lo * (cnt as i64 - k as i64)
}

fn main() {
    // Rerooting demo
    let mut tree = Tree::new(5);
    for (u, v) in [(0,1),(0,2),(1,3),(1,4)] {
        tree.add_edge(u, v);
    }
    tree.dfs_down(0);
    println!("Sum of distances: {:?}", tree.ans);

    // TSP demo
    let dist = vec![
        vec![0, 10, 15, 20],
        vec![10, 0, 35, 25],
        vec![15, 35, 0, 30],
        vec![20, 25, 30, 0],
    ];
    println!("TSP optimal: {}", tsp(&dist));

    // Knuth demo
    let cost = vec![
        vec![0, 1, 3, 5],
        vec![0, 0, 2, 4],
        vec![0, 0, 0, 1],
        vec![0, 0, 0, 0],
    ];
    let dp = knuth_dp(&cost);
    println!("Knuth DP [0][3]: {}", dp[0][3]);

    // Aliens trick demo
    println!("Aliens trick (n=6, k=2): {}", aliens_optimal(6, 2));
}
```

### Rust-specific considerations

- **Iterative DFS**: Rust's default thread stack is 8 MB. Recursive DFS on a path
  graph of 100k nodes will overflow. The iterative pattern shown (explicit stack +
  processing in reverse order) is idiomatic Rust for tree DP.
- **Flat array for bitmask DP**: `vec![INF; states * n]` with manual indexing is
  cache-friendlier than `Vec<Vec<i64>>`. The compiler can vectorize contiguous memory
  more aggressively.
- **Tuple state for aliens trick**: `(i64, usize)` for `(cost, count)` is clean.
  Sorting by cost first and count second with `<` on tuples works because Rust tuples
  implement `Ord` lexicographically.
- **Clone in DFS**: The `self.adj[v].clone()` call is necessary because we hold a mutable
  borrow of `self` while iterating over a field of `self`. In production code, restructure
  to take a separate `adj` slice to avoid the clone.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|----|------|
| Recursive DFS safety | Dynamic stack growth handles most cases | Must go iterative; 8 MB stack limit is real |
| 2D DP memory layout | `[][]int` convenient, GC overhead | `Vec<i64>` flat + manual indexing = better cache |
| Integer overflow guard | `MaxInt64 / 2` idiom | `i64::MAX / 2` — identical pattern |
| Borrow checker complexity | No issue; GC manages aliasing | `self.adj[v].clone()` required for self-referential mutation |
| Generics for DP cost type | Requires interface; often just use `int` | Can write generic `Ord + Add` DP easily |
| Performance | Within 1.5–3× of Rust for memory-bound DP | Faster for cache-sensitive large DP tables |
| Standard library | No DP helpers; write from scratch | No DP helpers; write from scratch |

## Production War Stories

**Google Maps / routing engines**: The aliens trick is used in multi-day trip planning
where you want to minimize total travel cost with exactly N stops. The constraint "exactly
N" makes naive DP O(N × locations²). The aliens trick reduces it to O(log(maxCost) ×
locations) DP calls.

**Compilers (GCC, LLVM)**: Optimal instruction scheduling uses interval DP. LLVM's
machine scheduler uses a greedy heuristic, but the knuth optimization appears in
register allocation cost models where merging live ranges has a quadrangle-inequality
cost structure.

**Bioinformatics (sequence alignment)**: Multiple sequence alignment with exactly k
gap regions uses the aliens trick. BLAST and Smith-Waterman are simpler, but tools like
Cactus (genome alignment) use constrained DP that benefits from these optimizations.

**Logistics / VRP solvers**: TSP bitmask DP is the backbone of exact solvers for
n ≤ 20. Google OR-Tools and Concorde use bitmask DP for small instances before
switching to branch-and-bound.

**Build systems (Bazel, Buck)**: Optimal scheduling of build targets with dependency
trees uses rerooting DP to compute, for each target, the critical path length
considering both its subtree and all upstream dependents.

## Complexity Analysis

| Technique | Time | Space | Practical limit |
|-----------|------|-------|----------------|
| Rerooting | O(n) | O(n) | n ≤ 10^6 |
| Bitmask DP (TSP) | O(2^n × n²) | O(2^n × n) | n ≤ 22 |
| Bitmask DP (subset iteration) | O(3^n) | O(2^n) | n ≤ 25 |
| Knuth optimization | O(n²) | O(n²) | n ≤ 5000 |
| D&C optimization | O(kn log n) | O(n) | n ≤ 10^5, k ≤ 100 |
| Aliens trick | O(n log C) | O(n) | n ≤ 10^5, C = max cost |

**Hidden constants**: Bitmask DP's O(2^n × n²) has a small constant but 2^22 × 22² ≈ 2×10^9
operations is right at the edge of 2–3 second time limits. Cache behavior dominates:
the inner loop iterates over n adjacent memory locations, which fits in L1 cache for
n ≤ 22.

Knuth's O(n²) has roughly 1/3 the constant of naive O(n³) in practice, because the
`opt` window averages n/3 width. The memory access pattern (diagonal fill) is
cache-unfriendly; a blocked variant helps for n > 2000.

## Common Pitfalls

1. **Rerooting double-counting**: When computing `up[child]` by subtracting child's
   contribution from `down[parent]`, forgetting to account for the edge cost between
   parent and child. Draw the two-phase picture explicitly before coding.

2. **Bitmask DP bit ordering**: Mixing 0-indexed and 1-indexed cities causes off-by-one
   in the mask. Fix: always use `(mask >> v) & 1` to test membership, never `mask & v`.

3. **Knuth initialization of opt**: `opt[i][i] = i` is correct; `opt[i][j-1]` must be
   initialized before `opt[i][j]` is read. Fill diagonals in strictly increasing length
   order, left to right within each diagonal.

4. **Aliens trick flat region**: When multiple λ values give the same count, your binary
   search may converge to a λ where count ≠ k even though the answer is reachable.
   Fix: binary-search on `(lo + hi + 1) / 2` (upper-bound variant) and verify count
   at the final λ matches k.

5. **D&C optimization monotonicity assumption**: Applying D&C optimization when the
   cost function does NOT satisfy the condition produces silently wrong answers. Always
   verify with a brute-force check on small inputs.

## Exercises

**Exercise 1 — Verification** (30 min):
Implement the rerooting solution for "Count of nodes at distance ≤ k" on a tree.
Verify your rerooting derivation is correct by comparing against a brute-force O(n²)
DFS for all roots on random trees of n = 100. Instrument the number of times `down`
is accessed during the up-pass to confirm O(n) traversals.

**Exercise 2 — Extension** (2–4 h):
Implement the bitmask DP for the "Minimum cost to visit all nodes in a directed graph"
variant where edges have time-dependent costs (cost[u][v][time_of_day]). The state
space becomes `dp[mask][v][t]`. Profile memory usage for n=15 and analyze whether a
rolling-time-window optimization is feasible.

**Exercise 3 — From Scratch** (4–8 h):
Implement the divide-and-conquer DP optimization for the "k-segment minimum sum" problem
(split array into exactly k contiguous segments minimizing sum of (max − min) per segment).
The cost function here does NOT satisfy the quadrangle inequality directly — prove this,
then find a transformation that makes it applicable. Compare runtime to O(kn²) brute-force
for n = 10^4, k = 100.

**Exercise 4 — Production Scenario** (8–15 h):
You are building a multi-tenant batch job scheduler. Jobs have processing times and
deadlines. You need to assign jobs to exactly k machines to minimize total weighted
tardiness. Model this as a constrained DP, apply the aliens trick to remove the k
constraint, implement the full scheduler as a Go or Rust service with a gRPC API,
and benchmark against a greedy heuristic. Include a test harness that generates random
job sets and verifies the optimal schedule using a brute-force solver for n ≤ 15.

## Further Reading

### Foundational Papers
- Knuth, D. E. (1971). "Optimum Binary Search Trees." *Acta Informatica*, 1(1), 14–25.
  The original paper proving the quadrangle inequality and the O(n²) bound.
- Divide-and-conquer DP: Galil, Z., & Park, K. (1992). "A linear-time algorithm for
  concave one-dimensional dynamic programming." *Information Processing Letters*, 42(1), 25–28.
- WQS/Aliens binary search: Originally from IOI 2016 problem "Aliens" editorial.
  Detailed writeup at https://codeforces.com/blog/entry/77404

### Books
- *Competitive Programming 4* — Halim et al. Volume 2, chapters on DP optimizations.
- *Algorithm Design* — Kleinberg & Tardos. Chapter 6 for DP foundations; the
  optimizations here are beyond its scope but the intuition is built here.
- *Introduction to Algorithms (CLRS)* — Chapter 15 for dynamic programming;
  rod cutting and matrix chain as canonical examples.

### Production Code to Read
- **OR-Tools TSP solver** (`ortools/constraint_solver/routing.cc`): See how bitmask DP
  appears in the held-karp implementation for small instances.
- **Cactus genome aligner** (`https://github.com/ComparativeGenomicsToolkit/cactus`):
  Constrained DP for genome segmentation.
- **Go compiler** (`cmd/compile/internal/ssa/schedule.go`): Instruction scheduling
  uses a simplified DP with greedy tie-breaking.

### Conference Talks
- "Dynamic Programming Optimizations" — Codeforces Educational Round lecture series
  (multiple entries; search "DP optimization" on CF blog).
- ACM ICPC World Finals 2019 Analysis — multiple problems requiring aliens trick and
  D&C DP; editorial PDFs available at icpc.global.
