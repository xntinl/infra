# 40. CP: Centroid Decomposition

**Difficulty**: Insane

## The Challenge

Centroid decomposition is one of the most powerful techniques in competitive programming for
answering queries on trees efficiently. The core idea is deceptively simple: find the centroid
of a tree (the node whose removal splits the tree into components each of size at most n/2),
remove it, recursively decompose each subtree, and build a "centroid tree" whose depth is
guaranteed to be O(log n). This centroid tree has a remarkable property: the path between any
two nodes in the original tree passes through their lowest common ancestor in the centroid tree.
This means that any path-based query on the original tree can be answered by considering at most
O(log n) centroids — transforming an O(n) per-query brute force into O(log n) per query with
O(n log n) preprocessing. Your mission is to implement centroid decomposition from scratch, build
the centroid tree, and then use it to solve a battery of progressively harder tree query problems
that would be intractable with naive approaches.

The technique matters because trees are the most fundamental non-linear data structure in
competitive programming, and path queries on trees are ubiquitous: "how many paths have weight
at most K?", "what is the closest marked node to each query node?", "what is the XOR of all
edges on the path between u and v?", "update the weight of node v and query the sum of weights
on the path from u to root." Many of these problems have O(n^2) naive solutions that fail on
the n = 200,000 constraints common in modern competitive programming contests. Centroid
decomposition reduces them to O(n log n) or O(n log^2 n), which is fast enough. The technique
also composes beautifully with other data structures — you can maintain a BIT, segment tree, or
sorted array at each centroid node, turning the centroid tree into a multi-level index over the
original tree.

The difficulty of this exercise is not in the decomposition itself — finding centroids and
building the centroid tree is a well-understood 50-line algorithm. The difficulty is in
*applying* the decomposition to solve actual problems, each of which requires a different
auxiliary data structure at each centroid, a different way of aggregating partial answers, and
careful handling of distance computations. You must also achieve the theoretical time complexity
on large inputs (n up to 200,000), which means constant factors matter: your implementation must
use efficient I/O, avoid unnecessary allocations, and minimize cache misses. This is competitive
programming at the algorithmic level — correctness is necessary but not sufficient; you must also
be fast.

## Acceptance Criteria

### Core Centroid Decomposition
- [ ] Implement a function `find_centroid(adj: &[Vec<usize>], root: usize, tree_size: usize, removed: &[bool]) -> usize` that finds the centroid of the subtree rooted at `root`, considering only nodes where `removed[v] == false`
- [ ] The centroid is defined as the node `c` such that when `c` is removed, all remaining connected components have size at most `tree_size / 2`
- [ ] Finding the centroid requires two DFS passes: one to compute subtree sizes, one to find the node where the maximum component size (including the "parent" component) is minimized
- [ ] Implement the full decomposition: `decompose(adj, root, removed) -> CentroidTree` that recursively finds the centroid, marks it as removed, decomposes each resulting component, and builds the centroid tree
- [ ] The centroid tree is stored as a parent array: `centroid_parent[v]` gives the parent of node `v` in the centroid tree (the centroid of the component that contained `v` before `v` was chosen as centroid)
- [ ] The centroid tree root has `centroid_parent[root] = root` (or a sentinel value like `usize::MAX`)
- [ ] The depth of the centroid tree is at most O(log n) — verify this empirically for trees with n = 200,000
- [ ] The total work across all recursive calls is O(n log n) — each node is visited in O(log n) levels of recursion, and each level processes each node at most once

### Distance Computation Infrastructure
- [ ] Implement `dist(u: usize, v: usize) -> i64` that computes the distance between any two nodes in the original tree in O(log n) using LCA (Euler tour + sparse table or binary lifting)
- [ ] Precompute `dist_to_centroid[v][level]`: the distance from node `v` to its ancestor at each level in the centroid tree — this array has O(n log n) total entries
- [ ] Alternatively, precompute depths from each centroid during decomposition: when processing centroid `c`, run a BFS/DFS from `c` within its component and store `dist_from[c][v]` for all `v` in the component
- [ ] The distance storage must be memory-efficient: either a flat array with offsets (CSR-style) or a `Vec<Vec<i64>>` indexed by centroid, storing only nodes in that centroid's component
- [ ] Total memory for distance precomputation is O(n log n)

### Problem 1: Count Paths with Distance at Most K
- [ ] **Input**: a weighted tree with n nodes and a value K. Count the number of unordered pairs (u, v) where `dist(u, v) <= K`
- [ ] **Approach**: at each centroid `c`, collect the distances from `c` to all nodes in its component. Sort the distances. Use a two-pointer technique to count pairs whose combined distance is at most K. Subtract pairs that lie entirely within the same child subtree (inclusion-exclusion) to avoid double counting
- [ ] The inclusion-exclusion step is critical: for each child subtree of `c`, compute the count of pairs within that subtree and subtract it, because those pairs do not actually pass through `c`
- [ ] Time complexity: O(n log^2 n) — O(n log n) for decomposition, O(n log n) for sorting at each level (summed across levels)
- [ ] Handle edge weights that may be zero or negative (if the problem specifies non-negative weights, validate and document the assumption)
- [ ] Solve correctly for n = 200,000 within 2 seconds
- [ ] Verify against a brute-force O(n^2) solution on small inputs (n <= 1000)

### Problem 2: Closest Marked Vertex (Online Queries)
- [ ] **Input**: a tree with n nodes, initially all unmarked. Two operations: `mark(v)` marks node v, `query(v)` returns the distance to the nearest marked node (or -1 if none are marked)
- [ ] **Approach**: at each centroid node in the centroid tree, maintain the minimum distance to a marked node in that centroid's component. For `mark(v)`: walk up the centroid tree from `v` to the root, updating each ancestor centroid's minimum with `dist(v, centroid_ancestor)`. For `query(v)`: walk up the centroid tree from `v`, and at each ancestor centroid `c`, compute `dist(v, c) + min_marked_dist[c]`, taking the overall minimum
- [ ] `mark(v)` is O(log n): walk up O(log n) centroid tree levels, updating a single value at each
- [ ] `query(v)` is O(log n): walk up O(log n) levels, computing distance + stored minimum at each
- [ ] Handle the edge case where `v` itself is marked (distance 0)
- [ ] Support `q` queries (mix of mark and query) with total time O(q log n)
- [ ] Solve correctly for n = 200,000 and q = 200,000 within 3 seconds

### Problem 3: Tree Distance Queries (All Pairs Offline)
- [ ] **Input**: a tree with n nodes and q queries, each query is a pair (u, v) asking for `dist(u, v)`
- [ ] **Approach**: use LCA with Euler tour and sparse table for O(1) per query after O(n log n) preprocessing. This problem does not require centroid decomposition directly but establishes the distance infrastructure needed for other problems
- [ ] Implement Euler tour: DFS the tree, recording each node when first visited and when returning from children, producing an array of length 2n-1
- [ ] Implement sparse table for range minimum query on the Euler tour depths, enabling O(1) LCA queries
- [ ] `dist(u, v) = depth[u] + depth[v] - 2 * depth[lca(u, v)]` for unweighted trees; adapt for weighted trees using cumulative edge weights from root
- [ ] Answer q queries in O(n log n + q) total time
- [ ] Verify correctness against a naive O(n) per query DFS-based distance computation

### Problem 4: XOR Path Queries
- [ ] **Input**: a tree with n nodes where each edge has a weight. Answer q queries: given (u, v), compute the XOR of all edge weights on the path from u to v
- [ ] **Approach**: precompute `xor_from_root[v]` = XOR of all edge weights on the path from root to v. Then `xor(u, v) = xor_from_root[u] XOR xor_from_root[v]` because the path from root to LCA is XORed twice (canceling out)
- [ ] This elegant property means XOR path queries do not even need LCA — just root the tree, precompute prefix XOR, and answer in O(1)
- [ ] Handle edge weights up to 10^9 (fits in `u64`)
- [ ] Answer q queries in O(n + q) total time
- [ ] Verify against brute force on small inputs

### Problem 5: Count Paths with XOR Equal to K (Centroid Decomposition + Hashing)
- [ ] **Input**: a tree with n edge-weighted nodes and a value K. Count unordered pairs (u, v) where the XOR of all edge weights on the path from u to v equals K
- [ ] **Approach**: at each centroid `c`, compute `xor_from_c[v]` for all nodes `v` in the component. For each node `v`, the path through `c` to some node `u` has XOR value `xor_from_c[v] XOR xor_from_c[u]`. We want this to equal K, so for each `v`, count the number of nodes `u` with `xor_from_c[u] = xor_from_c[v] XOR K`. Use a HashMap or array to count XOR values and look up complements
- [ ] Apply inclusion-exclusion: subtract pairs within the same child subtree of `c`
- [ ] Time complexity: O(n log n) (each level of the centroid tree processes O(n) nodes with O(1) hash lookups)
- [ ] Handle the case K = 0 (counting paths with XOR equal to zero, which includes the trivial path from a node to itself — clarify whether (v, v) is counted)
- [ ] Solve for n = 200,000 within 2 seconds

### Problem 6: Colored Tree Updates (Centroid Decomposition + BIT)
- [ ] **Input**: a tree with n nodes, each with a color (initially all white). Operations: `toggle(v)` flips the color of node v (white to black or black to white), `query(v)` returns the sum of distances from v to all black nodes
- [ ] **Approach**: at each centroid in the centroid tree, maintain two values: `count[c]` (number of black nodes in c's component) and `sum_dist[c]` (sum of distances from those black nodes to c). For `toggle(v)`: walk up the centroid tree, updating `count` and `sum_dist` at each ancestor. For `query(v)`: walk up the centroid tree, at each ancestor `c`, add `sum_dist[c] + count[c] * dist(v, c)`, then subtract the contribution already counted from the child subtree to avoid double counting
- [ ] The subtraction step requires maintaining `count_child[c]` and `sum_dist_child[c]` for the child direction — keep two values per centroid: one for the full component, one for the component below (toward the queried node)
- [ ] Both operations are O(log n)
- [ ] Solve for n = 200,000, q = 200,000 within 3 seconds

### Input/Output Performance
- [ ] Implement fast I/O: read all input at once and parse manually (not `stdin.lines()` which allocates per line)
- [ ] Use a custom reader that reads into a large buffer and parses integers directly from bytes, skipping whitespace
- [ ] Output is buffered: use `BufWriter<Stdout>` and write all output before flushing
- [ ] Total I/O time should be under 100ms for n = 200,000 inputs
- [ ] Integer parsing handles both positive and negative numbers without using `str::parse::<i64>()` (which is slow due to locale handling and error checking)

### Correctness and Stress Testing
- [ ] Implement a brute-force solver for each problem that uses O(n^2) or O(n * q) naive approaches
- [ ] Write a random tree generator that produces trees with n nodes and random edge weights
- [ ] Generate random trees of sizes n = 10, 50, 100, 500, 1000 and verify that the centroid decomposition solution matches the brute-force solution on every instance
- [ ] Test pathological tree shapes: path graph (a single chain — worst case for many tree algorithms), star graph (one center connected to all others), binary tree (balanced), caterpillar graph
- [ ] The path graph is the worst case for centroid decomposition depth — verify that even a chain of 200,000 nodes produces a centroid tree of depth at most ceil(log2(200000)) = 18
- [ ] Run at least 1000 random test cases per problem without a single mismatch

### Problem 7: Path Count Modulo Prime (Centroid Decomposition + NTT — Stretch Goal)
- [ ] **Input**: a weighted tree with n nodes and a value K. For each value d from 1 to K, count the number of unordered pairs (u, v) with `dist(u, v) == d`, modulo a prime p
- [ ] **Approach**: at each centroid `c`, collect distances from `c` to all nodes in its component into a polynomial `f(x) = sum(x^{dist(c,v)})`. The number of paths through `c` with distance exactly `d` is the coefficient of `x^d` in `f(x)^2`, divided by 2 (for unordered pairs), minus self-pairs, minus pairs within the same child subtree
- [ ] Use NTT (Number Theoretic Transform) to multiply the polynomial `f(x)` with itself in O(K log K) time
- [ ] Apply inclusion-exclusion per child subtree using the same polynomial squaring approach
- [ ] Total complexity: O(n log n * K log K / n) amortized, but in practice O(n log^2 n) if K is O(n)
- [ ] This is the hardest problem in the set — it combines centroid decomposition with polynomial multiplication

### Benchmarking
- [ ] Measure the time for centroid decomposition on trees of size n = 10,000, 50,000, 100,000, 200,000
- [ ] Measure per-query time for "closest marked vertex" with random mark/query sequences
- [ ] Compare against a naive O(n) per-query BFS baseline on the same inputs
- [ ] Identify the constant factor: how many nanoseconds per node per centroid tree level does your implementation take?
- [ ] Profile memory usage: verify total memory is O(n log n) by plotting memory vs. n
- [ ] Compare frame-pointer-based stack depth (chain graph) against the theoretical log2(n) bound

### Code Organization
- [ ] A reusable `CentroidDecomposition` struct that stores the centroid tree, parent array, depth array, and provides iteration over centroid ancestors
- [ ] The decomposition struct is generic over the auxiliary data structure stored at each centroid (for different problems, you store different things)
- [ ] A reusable `LCA` struct with Euler tour and sparse table for O(1) LCA and distance queries
- [ ] A reusable `FastIO` struct for competitive programming I/O
- [ ] Each problem is solved in a separate function that uses the shared infrastructure
- [ ] All solutions fit in a single file (competitive programming style) but are organized with clear sections
- [ ] Provide a `main()` function that reads an input format specifier (problem number) and dispatches to the correct solver, enabling a single binary to solve all problems
- [ ] Include a `debug!` macro that prints only when compiled in debug mode (`#[cfg(debug_assertions)]`), used for tracing decomposition steps during development
- [ ] Include assertions in debug mode that verify centroid tree depth is at most ceil(log2(n)) and that component sizes decrease by at least half at each level
- [ ] The `CentroidDecomposition` struct provides an `ancestors(v)` iterator that yields centroid tree ancestors from `v` up to the root, used by every problem's update/query traversal

### Edge Cases and Degenerate Trees
- [ ] Handle the trivial tree: n = 1 (single node). The centroid is that node, the centroid tree is a single node, and all queries return trivial answers (0 paths, distance 0 to self, etc.)
- [ ] Handle n = 2: two nodes connected by an edge. The centroid can be either node. Both problems involving path counts should report exactly 1 pair
- [ ] Handle disconnected components: if the input is a forest (multiple trees), decompose each tree independently
- [ ] Handle edge weights of zero: zero-weight edges are valid and should not cause division-by-zero or incorrect counting
- [ ] Handle very large edge weights (up to 10^18): use `i64` or `u64` throughout, and verify no overflow in distance summation. For Problem 6 (sum of distances), the sum can reach O(n * max_weight * log n) — verify this fits in `i64`
- [ ] Handle duplicate edge weights: multiple edges with the same weight should not confuse the counting logic
- [ ] The star graph (one center connected to n-1 leaves) produces a centroid tree of depth 2 — verify your decomposition handles this correctly and that all problems produce correct answers on star graphs
- [ ] The binary tree (balanced) produces a centroid tree of depth exactly log2(n) — verify this matches

### Competitive Programming Submission Format
- [ ] Each problem can be solved as a standalone single-file program suitable for submission to an online judge
- [ ] Input is read from stdin, output is written to stdout
- [ ] No external crates are used (only std) — online judges typically do not support external dependencies
- [ ] The solution compiles with `rustc` directly (no Cargo needed) for maximum judge compatibility
- [ ] Include a `#[allow(unused)]` prelude to avoid warnings from unused helper functions when submitting a subset of the code
- [ ] The solution handles Windows-style line endings (`\r\n`) gracefully in the input parser
- [ ] Time measurements use `std::time::Instant` and are printed to stderr so they do not interfere with stdout output for the judge
- [ ] Memory usage is tracked with a custom allocator wrapper (in debug mode) that counts allocations and peak memory

## Starting Points

- **cp-algorithms: Centroid Decomposition**: https://cp-algorithms.com/tree/centroid-decomposition.html — the single best reference for the technique. It explains the algorithm step by step with pseudocode, proves the O(log n) depth bound, and walks through the "count paths with distance at most K" problem. Start here before reading anything else
- **Competitive Programmer's Handbook by Antti Laaksonen, Chapter 14 (Tree Algorithms)**: https://cses.fi/book/book.pdf — covers tree traversals, LCA, centroid decomposition, and heavy-light decomposition. The treatment is concise and implementation-oriented, with complexity proofs
- **CSES Problem Set — Tree Algorithms section**: https://cses.fi/problemset/ — a curated set of tree problems that build from basic traversals to advanced decomposition techniques. Problems like "Distance Queries", "Finding a Centroid", and "Path Queries" are direct applications. Submit your solutions to get automated verdicts on large test cases
- **Codeforces Blog: "Centroid Decomposition of a Tree" by Tanuj Khattar**: https://codeforces.com/blog/entry/51741 — an excellent tutorial with implementation in C++ and worked examples for multiple problem types. The section on "handling updates" (marking/unmarking nodes) is particularly relevant for Problems 2 and 6
- **Codeforces Blog: "Introduction to Centroid Decomposition" by Rezwan Arefin**: https://codeforces.com/blog/entry/81661 — another well-written tutorial with diagrams showing how the centroid tree is constructed and why the depth is logarithmic. Includes a clean C++ template that translates well to Rust
- **Algorithms Live! — Centroid Decomposition (YouTube)**: https://www.youtube.com/watch?v=3pk2Hsmtvmk — a video walkthrough that visualizes the decomposition process on concrete examples. Useful if the text-based explanations are not clicking
- **"Divide and Conquer on Trees" by adamant (Codeforces)**: https://codeforces.com/blog/entry/44351 — a broader treatment covering centroid decomposition as a special case of divide-and-conquer on trees, connecting it to related techniques and showing how the same framework solves different problem types
- **IOI Training Camp notes on Tree Decompositions**: various national olympiad training materials cover centroid decomposition with contest-style problems and editorial solutions. The Polish, Russian, and Chinese olympiad training materials are particularly strong on this topic
- **Rust competitive programming template**: study templates from high-rated Rust competitive programmers on Codeforces (filter by language). Common patterns include: `proconio` macro for fast input, `BufWriter` for output, adjacency list representation as `Vec<Vec<(usize, i64)>>`, and iterative DFS with explicit stacks to avoid stack overflow on deep trees

## Hints

1. The centroid of a tree is unique (or there are exactly two adjacent centroids, which can be broken arbitrarily). To find it, root the tree at any node, compute subtree sizes, then walk from the root toward the child with the largest subtree until you find a node where no child subtree exceeds n/2. This is O(n) per call and the total work across all recursive calls is O(n log n)
2. Use iterative DFS, not recursive DFS, for computing subtree sizes. Rust's default stack size is 8MB, which supports roughly 100,000 recursive calls. A path graph with n = 200,000 nodes will cause a stack overflow. Either set the stack size to 64MB with `std::thread::Builder::new().stack_size(64 * 1024 * 1024)` and run your solution in a spawned thread, or use an iterative DFS with an explicit stack
3. The `removed` array is the key state that makes the recursion work. When you find centroid `c`, set `removed[c] = true`. Then for each neighbor of `c`, start a new decomposition of the component containing that neighbor. Because `c` is removed, the DFS from each neighbor stays within its component. After all recursive calls return, do NOT unset `removed[c]` — the centroid tree is a persistent decomposition
4. For the "count paths with distance at most K" problem, the inclusion-exclusion trick is: at centroid `c` with children subtrees S1, S2, ..., Sk, count all pairs (u, v) where u and v are in c's component and dist(u, c) + dist(v, c) <= K. Then subtract pairs where u and v are in the SAME subtree Si (because those pairs don't actually pass through c). The subtraction uses the same sorted-array two-pointer technique, but only with distances from within each Si
5. For the two-pointer counting technique: sort all distances from centroid `c` to nodes in its component. Set left = 0, right = end. While left < right: if dist[left] + dist[right] <= K, then all elements from left+1 to right are valid pairs with left, so add (right - left) to the count and increment left. Otherwise, decrement right. This counts all valid pairs in O(n) time after O(n log n) sorting
6. For "closest marked vertex" queries, the key insight is that you don't need to store all marked node distances at each centroid — just the minimum. When you mark node v, walk up from v in the centroid tree: at each ancestor centroid c, update `best[c] = min(best[c], dist(v, c))`. When you query node v, walk up similarly, computing `dist(v, c) + best[c]` at each ancestor c and taking the minimum. Both operations touch O(log n) centroids
7. Precompute distances from each centroid to all nodes in its component during the decomposition phase. Store these in a flat array indexed by (centroid, node). This avoids computing distances online during queries. The total storage is O(n log n) because each node appears in O(log n) centroid components
8. For the XOR path problem, remember that XOR is its own inverse: `a XOR a = 0`. This means the path XOR from u to v through their LCA has the common prefix from root canceling out. For the centroid decomposition approach, `xor_path(u, v) through c = xor_from_c[u] XOR xor_from_c[v]`. To count paths with XOR = K, for each node v in the component, you want to count nodes u with `xor_from_c[u] = xor_from_c[v] XOR K`. A HashMap gives O(1) lookup per node
9. For the colored tree update problem (Problem 6), maintaining separate counters for "full component" and "child component" at each centroid is the trickiest part. When walking up the centroid tree from v during a query, at each ancestor c, the contribution is `sum_dist[c] + count[c] * dist(v, c)`, but you must subtract the contribution from the child direction (the subtree containing v that was already counted by the child centroid). Store `child_sum_dist` and `child_count` at each centroid for this subtraction
10. For fast I/O in Rust competitive programming, read the entire input with `std::io::Read::read_to_end` into a byte buffer, then parse integers manually. A simple parser: skip whitespace, read digits, convert to integer. This avoids the overhead of `BufRead::lines()` which allocates a `String` per line. Output via `BufWriter` and call `flush()` once at the end
11. When implementing stress tests, generate random trees by iterating i from 1 to n-1 and connecting node i to a random node in [0, i-1]. This produces a random labeled tree. For weighted trees, assign random weights in a given range. For pathological cases, connect i to i-1 (chain) or all to 0 (star)
12. The centroid tree has the property that for any two nodes u, v in the original tree, their LCA in the centroid tree lies on the path from u to v in the original tree. This is what makes centroid decomposition work for path queries — any path passes through the LCA in the centroid tree, which is at most O(log n) levels up from either endpoint
13. For Rust specifically, represent the adjacency list as `Vec<Vec<(usize, i64)>>` where each entry is (neighbor, edge_weight). Pre-allocate the outer Vec with `vec![vec![]; n]`. For unweighted trees, use `Vec<Vec<usize>>`. Avoid `HashMap` for adjacency lists — the constant factor is too high for competitive programming
14. When multiple problems share the same tree infrastructure, build the centroid decomposition once and pass it to each problem solver. The decomposition itself is problem-independent; only the auxiliary data structures at each centroid change between problems
15. Profile your solution on a chain of n = 200,000 nodes with maximum edge weights. This is typically the worst case for both time (deepest centroid tree) and memory (largest distance arrays). If your solution handles this case within the time limit, it handles all cases
16. For competitive programming contests, centroid decomposition can often be replaced by heavy-light decomposition or Euler tour techniques for simpler problems. The problems in this exercise are specifically chosen to be natural fits for centroid decomposition — practice recognizing when a problem calls for this technique versus alternatives
