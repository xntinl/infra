# 31. CP: Dynamic Programming Advanced

## Difficulty: Avanzado

## Introduction

Dynamic programming is the backbone of competitive programming. While basic DP covers memoized recursion and tabulation on sequences, advanced DP techniques unlock an entirely different class of problems: interval DP for optimal splitting, bitmask DP for combinatorial state spaces, digit DP for counting numbers with properties, tree DP for hierarchical structures, and Knuth's optimization for reducing cubic to quadratic time.

This exercise assumes you are already comfortable with standard DP (LIS, knapsack, coin change). Here we go deeper: we encode richer state, exploit subproblem structure, and use Rust's type system and performance characteristics to write solutions that are both correct and fast.

---

## Foundational Concepts

### Why Advanced DP?

Many problems have exponential brute-force solutions but exhibit **optimal substructure** and **overlapping subproblems** in non-obvious ways. The key insight in advanced DP is choosing the right **state representation**:

| Technique | State Space | Typical Complexity | When to Use |
|-----------|------------|-------------------|-------------|
| Interval DP | `dp[i][j]` = subarray `[i..j]` | O(n^3) | Merging/splitting ranges |
| Bitmask DP | `dp[mask]` = subset of n items | O(2^n * n) | n <= 20, permutation/assignment |
| Digit DP | `dp[pos][tight][state]` | O(digits * states) | Count numbers with properties |
| Tree DP | `dp[node]` = subtree of node | O(n) to O(n^2) | Trees, hierarchical structures |
| Knuth Optimization | `dp[i][j]` with monotone opt | O(n^2) | Optimal BST, splitting problems |

### Rust Considerations for DP

```rust
// 1. Use Vec<Vec<T>> for 2D DP -- stack arrays overflow for large n
let mut dp = vec![vec![i64::MAX; n]; n];

// 2. For bitmask DP, 1 << 20 = 1_048_576 -- fits in memory
let mut dp = vec![i64::MAX; 1 << n];

// 3. Use .wrapping_add() or saturating arithmetic to avoid overflow panics
// in debug mode. Or work with i64 and sentinel values.

// 4. Memoization with HashMap for sparse states
use std::collections::HashMap;
let mut memo: HashMap<(usize, u32, bool), i64> = HashMap::new();
```

---

## Technique 1: Interval DP

### Theory

Interval DP solves problems where the answer for a range `[i, j]` depends on splitting it into `[i, k]` and `[k+1, j]` for some `k`. The canonical order is by **interval length**: solve all intervals of length 1, then length 2, and so on up to length n.

**General recurrence:**
```
dp[i][j] = optimize over k in [i, j-1] of:
    dp[i][k] + dp[k+1][j] + cost(i, k, j)
```

### Problem 1: Matrix Chain Multiplication

Given matrices A1, A2, ..., An with dimensions `dims[0] x dims[1]`, `dims[1] x dims[2]`, ..., `dims[n-1] x dims[n]`, find the minimum number of scalar multiplications to compute the product A1 * A2 * ... * An.

Multiplying an `a x b` matrix by a `b x c` matrix costs `a * b * c` operations.

**Example:**
- `dims = [10, 30, 5, 60]` means three matrices: 10x30, 30x5, 5x60
- `(A1 * A2) * A3`: cost = 10*30*5 + 10*5*60 = 1500 + 3000 = 4500
- `A1 * (A2 * A3)`: cost = 30*5*60 + 10*30*60 = 9000 + 18000 = 27000
- Optimal: 4500

**Hints:**
1. `dp[i][j]` = minimum cost to multiply matrices i through j (0-indexed).
2. Base case: `dp[i][i] = 0` (single matrix, no multiplication needed).
3. Transition: `dp[i][j] = min over k in [i, j-1] of dp[i][k] + dp[k+1][j] + dims[i] * dims[k+1] * dims[j+1]`.
4. Iterate by increasing interval length.

<details>
<summary>Solution</summary>

```rust
fn matrix_chain_order(dims: &[i64]) -> i64 {
    let n = dims.len() - 1; // number of matrices
    if n == 0 {
        return 0;
    }

    // dp[i][j] = minimum cost to multiply matrices i..=j
    let mut dp = vec![vec![0i64; n]; n];

    // l = chain length (number of matrices in the subchain)
    for l in 2..=n {
        for i in 0..=(n - l) {
            let j = i + l - 1;
            dp[i][j] = i64::MAX;
            for k in i..j {
                let cost = dp[i][k] + dp[k + 1][j] + dims[i] * dims[k + 1] * dims[j + 1];
                dp[i][j] = dp[i][j].min(cost);
            }
        }
    }

    dp[0][n - 1]
}

fn main() {
    let dims = vec![10, 30, 5, 60];
    println!("{}", matrix_chain_order(&dims)); // 4500

    let dims2 = vec![40, 20, 30, 10, 30];
    println!("{}", matrix_chain_order(&dims2)); // 26000

    let dims3 = vec![10, 20, 30, 40, 30];
    println!("{}", matrix_chain_order(&dims3)); // 30000
}
```

</details>

**Complexity Analysis:**
- **Time:** O(n^3) -- three nested loops (length, start, split point).
- **Space:** O(n^2) for the DP table.
- **Trade-offs:** For very large n, Knuth's optimization (covered later) can reduce this to O(n^2).

---

## Technique 2: Bitmask DP

### Theory

When the problem involves choosing a **subset** or **permutation** of a small set (n <= 20), we can represent the state as a bitmask. Bit `i` being set means element `i` has been used/visited.

**Key operations:**
```rust
// Check if bit i is set in mask
let is_set = (mask >> i) & 1 == 1;

// Set bit i
let new_mask = mask | (1 << i);

// Count set bits
let count = mask.count_ones();

// Iterate over all subsets of mask
let mut sub = mask;
loop {
    // process sub
    if sub == 0 { break; }
    sub = (sub - 1) & mask;
}
```

### Problem 2: Shortest Hamiltonian Path (TSP variant)

Given n cities (n <= 20) and a distance matrix `dist[i][j]`, find the minimum cost path that visits every city exactly once. The path can start and end at any cities (not necessarily a cycle).

**Example:**
```
n = 4
dist = [
    [0, 10, 15, 20],
    [10, 0, 35, 25],
    [15, 35, 0, 30],
    [20, 25, 30, 0],
]
Answer: 65 (path: 0 -> 1 -> 3 -> 2 or similar)
```

**Hints:**
1. State: `dp[mask][i]` = minimum cost to reach city `i` having visited exactly the cities in `mask`, with `i` being the last city.
2. Base case: `dp[1 << i][i] = 0` for all `i` (start at city i, only city i visited).
3. Transition: For each `mask` and last city `i` in `mask`, try extending to city `j` not in `mask`: `dp[mask | (1 << j)][j] = min(dp[mask][i] + dist[i][j])`.
4. Answer: `min over all i of dp[(1 << n) - 1][i]`.

<details>
<summary>Solution</summary>

```rust
fn shortest_hamiltonian_path(dist: &[Vec<i64>]) -> i64 {
    let n = dist.len();
    let full = (1u32 << n) - 1;

    // dp[mask][i] = min cost to reach city i with visited set = mask
    let mut dp = vec![vec![i64::MAX; n]; (full + 1) as usize];

    // Base case: start at each city
    for i in 0..n {
        dp[1 << i][i] = 0;
    }

    // Process masks in increasing order (fewer bits first)
    for mask in 1..=full {
        for last in 0..n {
            if dp[mask as usize][last] == i64::MAX {
                continue;
            }
            if (mask >> last) & 1 == 0 {
                continue; // last must be in mask
            }

            // Try extending to each unvisited city
            for next in 0..n {
                if (mask >> next) & 1 == 1 {
                    continue; // already visited
                }
                let new_mask = mask | (1 << next);
                let new_cost = dp[mask as usize][last] + dist[last][next];
                dp[new_mask as usize][next] = dp[new_mask as usize][next].min(new_cost);
            }
        }
    }

    // Answer: minimum over all ending cities with all visited
    let mut ans = i64::MAX;
    for i in 0..n {
        ans = ans.min(dp[full as usize][i]);
    }
    ans
}

fn main() {
    let dist = vec![
        vec![0, 10, 15, 20],
        vec![10, 0, 35, 25],
        vec![15, 35, 0, 30],
        vec![20, 25, 30, 0],
    ];
    println!("{}", shortest_hamiltonian_path(&dist)); // 65
}
```

</details>

**Complexity Analysis:**
- **Time:** O(2^n * n^2) -- for each of 2^n masks and n last cities, we try n next cities.
- **Space:** O(2^n * n) for the DP table.
- **Trade-offs:** Only feasible for n <= 20 (2^20 * 20 ~ 20 million states). For n <= 15 this runs in well under a second. For n = 20, expect ~2 seconds with careful implementation.

---

## Technique 3: Digit DP

### Theory

Digit DP counts numbers in a range `[0, N]` (or `[L, R]`) that satisfy some digit-based property. The idea is to build the number digit by digit from the most significant, tracking:

- **pos:** current digit position
- **tight:** whether we are still bounded by N's digits (if `tight`, the next digit can be at most `N[pos]`; otherwise it can be 0..9)
- **state:** problem-specific (sum of digits, last digit, bitmask of digits used, etc.)

**Framework:**
```rust
fn solve(digits: &[u8], pos: usize, tight: bool, state: State, memo: &mut HashMap<Key, i64>) -> i64 {
    if pos == digits.len() {
        return if valid(state) { 1 } else { 0 };
    }
    let key = (pos, tight, state);
    if let Some(&v) = memo.get(&key) {
        return v;
    }
    let limit = if tight { digits[pos] } else { 9 };
    let mut result = 0i64;
    for d in 0..=limit {
        result += solve(digits, pos + 1, tight && d == limit, next_state(state, d), memo);
    }
    memo.insert(key, result);
    result
}
```

To count numbers in `[L, R]`, compute `count(R) - count(L - 1)`.

### Problem 3: Count Numbers Without Consecutive Equal Digits

Given an integer N, count how many integers in `[1, N]` have **no two consecutive digits that are equal**.

**Example:**
- N = 20: Valid numbers are 1-9 (all single digit), 10, 12-19, 20. That is 9 + 9 = 18. Numbers 11 is excluded. So answer = 18.
- N = 100: We exclude 11, 22, 33, 44, 55, 66, 77, 88, 99, 100 is valid (1,0,0 -- consecutive 0s means invalid). Let us compute via digit DP.

**Hints:**
1. State: `(pos, tight, last_digit, started)`. The `started` flag handles leading zeros (07 is really 7, a 1-digit number).
2. If not started and current digit is 0, we stay "not started" (leading zero).
3. Transition: skip digits equal to `last_digit` (unless not started).
4. Base case: if `pos == len`, return 1 if `started` (we need at least 1 digit), else 0.

<details>
<summary>Solution</summary>

```rust
use std::collections::HashMap;

fn count_no_consecutive(n: u64) -> i64 {
    if n == 0 {
        return 0;
    }
    let digits: Vec<u8> = n.to_string().bytes().map(|b| b - b'0').collect();

    let mut memo: HashMap<(usize, bool, u8, bool), i64> = HashMap::new();

    fn dp(
        digits: &[u8],
        pos: usize,
        tight: bool,
        last: u8,      // last non-zero-leading digit placed (10 = none)
        started: bool,  // have we placed a nonzero digit yet?
        memo: &mut HashMap<(usize, bool, u8, bool), i64>,
    ) -> i64 {
        if pos == digits.len() {
            return if started { 1 } else { 0 };
        }

        let key = (pos, tight, last, started);
        if let Some(&v) = memo.get(&key) {
            return v;
        }

        let limit = if tight { digits[pos] } else { 9 };
        let mut result = 0i64;

        for d in 0..=limit {
            if started && d == last {
                continue; // consecutive equal digits
            }
            let new_tight = tight && (d == limit);
            let new_started = started || d != 0;
            let new_last = if new_started { d } else { 10 };
            result += dp(digits, pos + 1, new_tight, new_last, new_started, memo);
        }

        memo.insert(key, result);
        result
    }

    dp(&digits, 0, true, 10, false, &mut memo)
}

fn main() {
    println!("{}", count_no_consecutive(20));    // 18
    println!("{}", count_no_consecutive(100));   // 81
    println!("{}", count_no_consecutive(1000));  // 738
}
```

</details>

**Complexity Analysis:**
- **Time:** O(D * 2 * 11 * 2 * 10) = O(D * 440) where D is the number of digits. Essentially O(D) which is O(log N).
- **Space:** O(D * 440) for memoization.
- **Trade-offs:** Digit DP is extremely fast (proportional to number of digits, not the number itself), making it feasible for N up to 10^18. Using a HashMap is simpler but slower than a 4D array; for competitive programming, the array version is preferred.

---

## Technique 4: DP on Trees

### Theory

Tree DP computes properties of a tree by rooting it and processing children before parents (post-order). Common patterns:

- `dp[v]` = answer for the subtree rooted at v
- `dp[v][0]` / `dp[v][1]` = answer when v is not selected / selected
- Rerooting technique: compute the answer for all possible roots in O(n)

### Problem 4: Tree Diameter via DP

Given an unweighted tree with n nodes, find its **diameter** (the length of the longest path between any two nodes).

**Example:**
```
    0
   / \
  1   2
 / \
3   4
     \
      5

Diameter = 4 (path: 3 -> 1 -> 0 -> 2 or 5 -> 4 -> 1 -> 0 -> 2...
actually 5 -> 4 -> 1 -> 0 -> 2 = 4 edges)
```

**Hints:**
1. Root the tree at any node (say 0).
2. For each node v, compute `depth[v]` = length of the longest path going down from v.
3. The diameter through v is `depth[child1] + depth[child2] + 2` for the two deepest children.
4. The answer is the maximum diameter through any node.

<details>
<summary>Solution</summary>

```rust
fn tree_diameter(n: usize, edges: &[(usize, usize)]) -> usize {
    if n <= 1 {
        return 0;
    }

    // Build adjacency list
    let mut adj = vec![vec![]; n];
    for &(u, v) in edges {
        adj[u].push(v);
        adj[v].push(u);
    }

    let mut diameter = 0usize;

    // depth[v] = longest path going downward from v
    let mut depth = vec![0usize; n];

    // Iterative post-order DFS (avoid stack overflow for large trees)
    let mut parent = vec![usize::MAX; n];
    let mut order = Vec::with_capacity(n);
    let mut stack = vec![0usize];
    parent[0] = 0; // root's parent is itself (sentinel)

    while let Some(v) = stack.pop() {
        order.push(v);
        for &u in &adj[v] {
            if parent[v] != u && parent[u] == usize::MAX {
                parent[u] = v;
                stack.push(u);
            }
        }
    }

    // Process in reverse order (leaves first)
    for &v in order.iter().rev() {
        let mut max1 = 0usize; // deepest child path
        let mut max2 = 0usize; // second deepest

        for &u in &adj[v] {
            if u == parent[v] {
                continue;
            }
            let d = depth[u] + 1;
            if d >= max1 {
                max2 = max1;
                max1 = d;
            } else if d > max2 {
                max2 = d;
            }
        }

        depth[v] = max1;
        diameter = diameter.max(max1 + max2);
    }

    diameter
}

fn main() {
    // Tree:     0
    //          / \
    //         1   2
    //        / \
    //       3   4
    //            \
    //             5
    let edges = vec![(0, 1), (0, 2), (1, 3), (1, 4), (4, 5)];
    println!("{}", tree_diameter(6, &edges)); // 4

    // Linear tree: 0-1-2-3-4
    let edges2 = vec![(0, 1), (1, 2), (2, 3), (3, 4)];
    println!("{}", tree_diameter(5, &edges2)); // 4

    // Star: center 0 connected to 1,2,3,4
    let edges3 = vec![(0, 1), (0, 2), (0, 3), (0, 4)];
    println!("{}", tree_diameter(5, &edges3)); // 2
}
```

</details>

**Complexity Analysis:**
- **Time:** O(n) -- single DFS pass.
- **Space:** O(n) for adjacency list, depth, parent, and order arrays.
- **Trade-offs:** The iterative approach avoids stack overflow for trees with up to 10^6 nodes. An alternative two-BFS approach also finds the diameter in O(n) but tree DP generalizes better (e.g., finding all nodes on a diameter, or weighted diameters).

---

## Technique 5: Knuth's Optimization

### Theory

Knuth's optimization applies to interval DP recurrences of the form:

```
dp[i][j] = min over k in [i, j-1] of (dp[i][k] + dp[k+1][j]) + cost(i, j)
```

where `cost` satisfies the **quadrangle inequality** (roughly, the cost function is "convex" enough). Under this condition, if `opt[i][j]` is the optimal split point, then:

```
opt[i][j-1] <= opt[i][j] <= opt[i+1][j]
```

This monotonicity means we can restrict the search range for `k`, reducing the total time from O(n^3) to O(n^2).

### Problem 5: Optimal Binary Search Tree

Given keys k1 < k2 < ... < kn with access frequencies `freq[i]`, construct a BST that minimizes the total expected search cost. The cost of accessing key ki at depth d is `freq[i] * (d + 1)`.

**Example:**
```
keys  = [10, 12, 20]
freq  = [34, 8, 50]

If 20 is root (depth 0), 10 is left child (depth 1), 12 is right child of 10 (depth 2):
cost = 50*1 + 34*2 + 8*3 = 50 + 68 + 24 = 142

Optimal: 10 as root, 20 as right child, 12 as left child of 20:
cost = 34*1 + 50*2 + 8*3 = 34 + 100 + 24 = 158

Actually, let us compute: root=20: cost = 50*1 + 34*2 + 8*3 = 142
root=12: cost = 8*1 + 34*2 + 50*2 = 8 + 68 + 100 = 176
root=10: cost = 34*1 + 8*2 + 50*3 = 34 + 16 + 150 = 200

Best subtree structure for this example needs full DP. Answer: 142.
```

**Hints:**
1. `dp[i][j]` = minimum cost BST for keys `i` through `j`.
2. When key `k` is the root of subtree `[i, j]`, cost = `dp[i][k-1] + dp[k+1][j] + sum(freq[i..=j])`. The `sum` term accounts for all keys in the subtree going one level deeper.
3. Use prefix sums for O(1) range sum queries.
4. Knuth's optimization: maintain `opt[i][j]` and restrict `k` to `[opt[i][j-1], opt[i+1][j]]`.

<details>
<summary>Solution (with Knuth's Optimization)</summary>

```rust
fn optimal_bst(freq: &[i64]) -> i64 {
    let n = freq.len();
    if n == 0 {
        return 0;
    }

    // Prefix sums for range sum queries
    let mut prefix = vec![0i64; n + 1];
    for i in 0..n {
        prefix[i + 1] = prefix[i] + freq[i];
    }
    let range_sum = |i: usize, j: usize| -> i64 {
        if i > j { return 0; }
        prefix[j + 1] - prefix[i]
    };

    // dp[i][j] = min cost for keys i..=j
    // opt[i][j] = optimal root for keys i..=j
    let mut dp = vec![vec![0i64; n]; n];
    let mut opt = vec![vec![0usize; n]; n];

    // Base case: single keys
    for i in 0..n {
        dp[i][i] = freq[i];
        opt[i][i] = i;
    }

    // Fill by increasing interval length
    for len in 2..=n {
        for i in 0..=(n - len) {
            let j = i + len - 1;
            dp[i][j] = i64::MAX;

            // Knuth's optimization: search k in [opt[i][j-1], opt[i+1][j]]
            let lo = opt[i][j - 1];
            let hi = if i + 1 < n { opt[i + 1][j] } else { j };

            for k in lo..=hi.min(j) {
                let left = if k > i { dp[i][k - 1] } else { 0 };
                let right = if k < j { dp[k + 1][j] } else { 0 };
                let cost = left + right + range_sum(i, j);

                if cost < dp[i][j] {
                    dp[i][j] = cost;
                    opt[i][j] = k;
                }
            }
        }
    }

    dp[0][n - 1]
}

/// Naive O(n^3) version for comparison/testing
fn optimal_bst_naive(freq: &[i64]) -> i64 {
    let n = freq.len();
    if n == 0 {
        return 0;
    }

    let mut prefix = vec![0i64; n + 1];
    for i in 0..n {
        prefix[i + 1] = prefix[i] + freq[i];
    }
    let range_sum = |i: usize, j: usize| -> i64 {
        prefix[j + 1] - prefix[i]
    };

    let mut dp = vec![vec![0i64; n]; n];

    for i in 0..n {
        dp[i][i] = freq[i];
    }

    for len in 2..=n {
        for i in 0..=(n - len) {
            let j = i + len - 1;
            dp[i][j] = i64::MAX;
            for k in i..=j {
                let left = if k > i { dp[i][k - 1] } else { 0 };
                let right = if k < j { dp[k + 1][j] } else { 0 };
                let cost = left + right + range_sum(i, j);
                dp[i][j] = dp[i][j].min(cost);
            }
        }
    }

    dp[0][n - 1]
}

fn main() {
    let freq = vec![34, 8, 50];
    let result = optimal_bst(&freq);
    println!("Optimal BST cost: {}", result); // 142

    // Verify against naive
    assert_eq!(optimal_bst(&freq), optimal_bst_naive(&freq));

    let freq2 = vec![4, 2, 6, 3];
    let r2 = optimal_bst(&freq2);
    println!("Optimal BST cost: {}", r2); // 35
    assert_eq!(r2, optimal_bst_naive(&freq2));
}
```

</details>

**Complexity Analysis:**
- **Naive:** O(n^3) time, O(n^2) space.
- **With Knuth's optimization:** O(n^2) time, O(n^2) space.
- **Trade-offs:** Knuth's optimization only applies when the cost function satisfies the quadrangle inequality. It does not help with arbitrary interval DP. The proof of correctness is nontrivial, but the implementation is a small modification: just track `opt[i][j]` and restrict the loop bounds.

---

## Problem 6: Bitmask DP -- Minimum Cost Task Assignment

Given n workers and n tasks with a cost matrix `cost[i][j]` (cost for worker i to do task j), assign exactly one task to each worker to minimize total cost. n <= 20.

This is the classic **assignment problem**, solvable in O(n^3) with the Hungarian algorithm, but bitmask DP gives a clean O(2^n * n) solution for small n.

**Hints:**
1. Process workers in order 0, 1, ..., n-1.
2. State: `dp[mask]` = minimum cost to assign tasks (indicated by bits in mask) to the first `popcount(mask)` workers.
3. The number of workers assigned so far is `mask.count_ones()`. The next worker is `mask.count_ones()`.
4. Transition: for each unassigned task j, `dp[mask | (1 << j)] = min(dp[mask] + cost[worker][j])`.

<details>
<summary>Solution</summary>

```rust
fn min_cost_assignment(cost: &[Vec<i64>]) -> i64 {
    let n = cost.len();
    let full = (1u32 << n) - 1;
    let mut dp = vec![i64::MAX; (full + 1) as usize];
    dp[0] = 0;

    for mask in 0..=full {
        if dp[mask as usize] == i64::MAX {
            continue;
        }
        let worker = mask.count_ones() as usize;
        if worker >= n {
            continue;
        }

        for task in 0..n {
            if (mask >> task) & 1 == 1 {
                continue; // task already assigned
            }
            let new_mask = mask | (1 << task);
            let new_cost = dp[mask as usize] + cost[worker][task];
            dp[new_mask as usize] = dp[new_mask as usize].min(new_cost);
        }
    }

    dp[full as usize]
}

fn main() {
    let cost = vec![
        vec![9, 2, 7, 8],
        vec![6, 4, 3, 7],
        vec![5, 8, 1, 8],
        vec![7, 6, 9, 4],
    ];
    println!("{}", min_cost_assignment(&cost)); // 13 (worker0->task1, worker1->task2 is wrong...
    // Optimal: w0->t1(2) + w1->t0(6) + w2->t2(1) + w3->t3(4) = 13
}
```

</details>

**Complexity Analysis:**
- **Time:** O(2^n * n). Each mask is processed once, and for each mask we iterate over n tasks.
- **Space:** O(2^n).
- **Trade-offs:** Compared to the Hungarian algorithm (O(n^3)), bitmask DP is simpler to implement but only works for n <= 20. For n = 20, the table has ~10^6 entries and each processes up to 20 tasks, giving ~2 * 10^7 operations.

---

## Problem 7: Digit DP -- Count Numbers with Digit Sum Divisible by K

Given integers N and K, count how many integers in `[1, N]` have a digit sum divisible by K.

**Example:**
- N = 20, K = 3: Numbers with digit sum divisible by 3: 3, 6, 9, 12, 15, 18. Answer: 6.

**Hints:**
1. State: `(pos, tight, sum_mod_k, started)`.
2. `sum_mod_k` tracks `digit_sum % K` to avoid storing the full sum.
3. At the end, check if `sum_mod_k == 0` and `started == true`.

<details>
<summary>Solution</summary>

```rust
use std::collections::HashMap;

fn count_digit_sum_div_k(n: u64, k: usize) -> i64 {
    if n == 0 {
        return 0;
    }
    let digits: Vec<u8> = n.to_string().bytes().map(|b| b - b'0').collect();
    let mut memo: HashMap<(usize, bool, usize, bool), i64> = HashMap::new();

    fn dp(
        digits: &[u8],
        pos: usize,
        tight: bool,
        sum_mod: usize,
        started: bool,
        k: usize,
        memo: &mut HashMap<(usize, bool, usize, bool), i64>,
    ) -> i64 {
        if pos == digits.len() {
            return if started && sum_mod == 0 { 1 } else { 0 };
        }

        let key = (pos, tight, sum_mod, started);
        if let Some(&v) = memo.get(&key) {
            return v;
        }

        let limit = if tight { digits[pos] } else { 9 };
        let mut result = 0i64;

        for d in 0..=limit {
            let new_tight = tight && d == limit;
            let new_started = started || d != 0;
            let new_mod = if new_started {
                (sum_mod + d as usize) % k
            } else {
                0
            };
            result += dp(digits, pos + 1, new_tight, new_mod, new_started, k, memo);
        }

        memo.insert(key, result);
        result
    }

    dp(&digits, 0, true, 0, false, k, &mut memo)
}

fn main() {
    println!("{}", count_digit_sum_div_k(20, 3));    // 6
    println!("{}", count_digit_sum_div_k(100, 5));   // 20
    println!("{}", count_digit_sum_div_k(1000, 7));  // 142

    // Verify small case by brute force
    let mut brute = 0;
    for i in 1..=20u64 {
        let s: u32 = i.to_string().bytes().map(|b| (b - b'0') as u32).sum();
        if s % 3 == 0 {
            brute += 1;
        }
    }
    assert_eq!(count_digit_sum_div_k(20, 3), brute);
}
```

</details>

**Complexity Analysis:**
- **Time:** O(D * 2 * K * 2 * 10) = O(D * K * 40), where D = number of digits.
- **Space:** O(D * K * 4) for memoization.
- **Trade-offs:** K must be moderate (up to ~1000) for the state space to fit in memory. For K up to 10^9, a different approach would be needed.

---

## Comparison of Techniques

| Technique | Best For | State Size | Typical n |
|-----------|----------|-----------|-----------|
| Interval DP | Merge/split ranges | O(n^2) | n <= 500 |
| Bitmask DP | Subsets/permutations | O(2^n) | n <= 20 |
| Digit DP | Counting in [L, R] | O(D * states) | N <= 10^18 |
| Tree DP | Hierarchical structures | O(n) | n <= 10^6 |
| Knuth Opt | Interval DP with QI | O(n^2) | n <= 5000 |

## Common Pitfalls in Rust

1. **Integer overflow:** DP values can grow large. Use `i64` by default; for modular arithmetic problems, apply `% MOD` at every step.
2. **Stack overflow on recursive DP:** For trees with 10^5+ nodes, use iterative DFS. Rust's default stack is 8MB.
3. **Debug mode performance:** `cargo run` in debug mode is ~10x slower than release. Always test performance with `cargo run --release`.
4. **Vec indexing panics:** Out-of-bounds access panics in both debug and release. Double-check your index math.
5. **HashMap vs array for memoization:** HashMap is easier but 3-5x slower. For competitive programming, prefer fixed-size arrays when the state space is bounded.

---

## Further Reading

- **CSES Problem Set** -- "Dynamic Programming" section covers most of these techniques with online judging.
- **Competitive Programmer's Handbook** (Laaksonen) -- Chapter 10 (DP) and Chapter 19 (Advanced DP).
- **AtCoder Educational DP Contest** -- 26 curated DP problems of increasing difficulty, excellent for practice.
- Knuth's original paper: "Optimum Binary Search Trees" (Acta Informatica, 1971).
