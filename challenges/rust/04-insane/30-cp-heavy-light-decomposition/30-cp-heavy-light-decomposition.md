# 30. CP: Heavy-Light Decomposition

**Difficulty**: Insane

## The Challenge

Heavy-Light Decomposition (HLD) is one of the most powerful techniques for answering path queries on trees in competitive programming, reducing arbitrary tree-path operations to a logarithmic number of contiguous array-range operations. The core idea is deceptively elegant: classify each edge in a rooted tree as "heavy" (connecting a node to its child with the largest subtree) or "light" (all other edges), then observe that any root-to-leaf path crosses at most O(log n) light edges. This means any path between two nodes can be decomposed into at most O(log n) contiguous "heavy chains," and if each chain is mapped to a contiguous segment of a flat array, you can answer path queries by performing O(log n) segment tree operations -- yielding O(log^2 n) per query. Combined with Euler tour ordering for subtree queries, HLD transforms a tree into a linear structure that supports both path and subtree operations with standard data structures.

Your task is to implement a complete HLD infrastructure from scratch in Rust: compute subtree sizes via DFS, classify heavy and light edges, decompose the tree into heavy chains, assign position indices that map each node to a position in a flat array (ensuring each heavy chain occupies a contiguous range), and build a segment tree over this flat array. You must then support six types of operations: path sum query (sum of values on all vertices along the path from u to v), path maximum query (maximum value on a path), path update (add a value to all vertices on a path), single-vertex update, subtree sum query (sum of all values in the subtree rooted at a given node), and LCA computation via HLD (which falls out naturally from the decomposition). The implementation must handle all of these operations on trees with up to 200,000 nodes and 200,000 queries within a 2-second time limit, demanding both algorithmic efficiency and low constant-factor Rust code.

Beyond the standard vertex-weighted HLD, this challenge requires you to handle the more subtle edge-weighted variant, where values live on edges rather than vertices. The standard trick is to push each edge's weight down to the child node (since every node except the root has exactly one parent edge), but you must be careful during path queries to exclude the LCA node itself (since the LCA's stored value corresponds to the edge above it, which is not on the u-to-v path). You must also implement the "make root" operation conceptually -- given that the tree structure is fixed but the logical root can change, how do subtree queries behave when the root is re-designated? This requires understanding the relationship between Euler tour intervals and re-rooting without actually rebuilding the decomposition. Finally, you must handle queries that combine different operations (e.g., update a path, then query a subtree) and verify that your segment tree's lazy propagation interacts correctly with the HLD's decomposition of paths into chain segments.

## Acceptance Criteria

### Tree Representation and Input

- [ ] Read a tree of n nodes (1-indexed or 0-indexed, your choice) with n-1 edges
  - Support both unweighted (values on vertices) and weighted (values on edges) input formats
  - Store the tree as an adjacency list: `Vec<Vec<(usize, i64)>>` for weighted, `Vec<Vec<usize>>` for unweighted
  - Root the tree at node 1 (or 0) by default; support arbitrary root specification

- [ ] Compute parent, depth, and subtree size arrays via a single DFS
  - `parent[v]`: parent of node v in the rooted tree (-1 or sentinel for the root)
  - `depth[v]`: depth of node v (root has depth 0)
  - `subtree_size[v]`: number of nodes in the subtree rooted at v (including v itself)
  - Use iterative DFS (explicit stack) to avoid stack overflow on deep trees (up to 200,000 depth in a worst-case chain graph)

### Heavy-Light Decomposition

- [ ] Classify edges as heavy or light
  - For each non-leaf node v, the heavy child is the child u with the largest `subtree_size[u]`
  - Break ties arbitrarily (e.g., pick the first such child in adjacency order)
  - All other children of v are light children
  - The heavy edge connects v to its heavy child; all other edges from v to its children are light

- [ ] Decompose the tree into heavy chains
  - `chain_head[v]`: the topmost node of the heavy chain containing v
  - A heavy chain is a maximal path of heavy edges; the head is the topmost node (closest to root)
  - For the heavy child u of v: `chain_head[u] = chain_head[v]`
  - For every light child w of v: `chain_head[w] = w` (it starts a new chain)
  - The root's chain head is the root itself

- [ ] Assign positions in the flat array via a modified DFS
  - `pos[v]`: the index of node v in the flat (Euler-tour-like) array
  - Process the heavy child first during DFS to ensure each heavy chain maps to a contiguous range
  - Then process all light children in any order
  - After this DFS, for any heavy chain with head h, the chain occupies positions `[pos[h], pos[h] + chain_length - 1]`
  - Also record `end[v]`: the last position in the subtree of v, so `[pos[v], end[v]]` covers the entire subtree

- [ ] Initialize the flat value array
  - `flat[pos[v]] = value[v]` for vertex-weighted trees
  - `flat[pos[v]] = edge_weight(parent[v], v)` for edge-weighted trees (root's position gets 0 or is excluded)

### Segment Tree with Lazy Propagation

- [ ] Implement a segment tree supporting the following operations
  - `build(arr)`: construct from the flat array in O(n)
  - `update_point(i, val)`: set or add to a single position
  - `update_range(l, r, val)`: add `val` to all positions in [l, r] (range increment with lazy propagation)
  - `query_sum(l, r) -> i64`: return the sum of values in [l, r]
  - `query_max(l, r) -> i64`: return the maximum value in [l, r]

- [ ] Implement lazy propagation for range updates
  - Each node stores a pending additive lazy value
  - On `push_down`, propagate the lazy value to both children, updating their stored sums (by `lazy * segment_length`) and maximums (by `lazy`)
  - Ensure lazy propagation is called before any query or update that descends into children

- [ ] Support both sum and max queries simultaneously
  - Each segment tree node stores both the sum and the max of its range
  - Both are updated correctly during point updates, range updates, and lazy propagation
  - Alternatively, build two separate segment trees (one for sum, one for max) if simpler

- [ ] Handle the edge case where the tree is a single node (n = 1)
  - No edges, all path queries are trivial, subtree queries return the node's own value

### LCA via HLD

- [ ] Implement LCA computation using the heavy-light decomposition
  - To find `lca(u, v)`: repeatedly move the node whose chain head is deeper up to the parent of its chain head
  - When both nodes are on the same chain, the one with smaller depth is the LCA
  - Time complexity: O(log n) per query

- [ ] Verify correctness against a brute-force LCA
  - For two nodes u and v, the brute-force LCA walks both up to the root and finds the deepest common ancestor
  - Stress test on random trees with 10,000 nodes and 10,000 LCA queries

### Path Queries and Updates

- [ ] Implement `path_sum(u, v) -> i64`
  - Decompose the path u -> v into O(log n) chain segments
  - For each segment, query the segment tree for the sum over the contiguous range
  - Accumulate the results
  - Use the HLD-LCA procedure: while `chain_head[u] != chain_head[v]`, process the chain of the deeper node and move it up

- [ ] Implement `path_max(u, v) -> i64`
  - Same decomposition as path_sum, but take the maximum across all segments instead of summing
  - Initialize the result to `i64::MIN` and take max with each segment's maximum

- [ ] Implement `path_update(u, v, val)`
  - Add `val` to every vertex on the path from u to v
  - Decompose into chain segments and perform range updates on the segment tree
  - Same traversal logic as path_sum

- [ ] Implement `vertex_update(u, val)`
  - Set or add to the value of a single vertex
  - Maps directly to `segment_tree.update_point(pos[u], val)`

- [ ] Handle the vertex-weighted vs edge-weighted distinction
  - For vertex-weighted: include the LCA node in path queries and updates
  - For edge-weighted: exclude the LCA node, since its position stores the edge *above* it, which is not on the path
  - In the edge-weighted case, when u and v are on the same chain, query `[pos[deeper] ... pos[shallower] + 1]` instead of `[pos[deeper] ... pos[shallower]]`
  - Clearly parameterize or template this distinction

### Subtree Queries

- [ ] Implement `subtree_sum(v) -> i64`
  - Query the segment tree over the range `[pos[v], end[v]]`
  - This works because the DFS ordering ensures the subtree of v occupies a contiguous range

- [ ] Implement `subtree_update(v, val)`
  - Add `val` to all vertices in the subtree of v
  - Range update on `[pos[v], end[v]]`

- [ ] Handle the re-rooting scenario (stretch goal)
  - Given a new root r, answer subtree queries without rebuilding the HLD
  - If the query node v is not an ancestor of r in the original rooting, the subtree is the same
  - If v is an ancestor of r, the "subtree" becomes the entire tree minus the subtree of the child of v on the path to r
  - Implement this case analysis using the original HLD and LCA queries

### Performance Requirements

- [ ] Handle n = 200,000 nodes and q = 200,000 queries within 2 seconds (release mode)
  - Mixed workload: path sum queries, path max queries, path updates, vertex updates, subtree queries
  - Worst-case tree structures: chain graph (depth = n), star graph (depth = 1), balanced binary tree

- [ ] Benchmark specific tree shapes
  - **Chain graph** (n = 200,000): O(log n) chains each of length O(n), but only 1 chain for a heavy path; path queries should still be fast
  - **Star graph** (n = 200,000): every edge is light, all chains have length 1, but paths between leaves have length 2 (through center)
  - **Random tree** (n = 200,000): typical case, chains of varying lengths
  - **Perfect binary tree** (n = 2^18 - 1 = 262,143): predictable O(log n) heavy chains
  - All must complete 200K queries under 2 seconds

- [ ] Memory usage: O(n log n) for the segment tree, O(n) for the HLD arrays
  - Total memory under 100MB for n = 200,000

- [ ] Use iterative DFS for tree traversal to avoid stack overflow
  - Rust's default stack size is 8MB, which can overflow for n = 200,000 in a chain graph with recursive DFS
  - Either use explicit stack-based DFS or increase the stack size via `std::thread::Builder`

### Competitive Programming I/O

- [ ] Provide a `main()` function with fast I/O
  - Use `BufReader<Stdin>` for input and `BufWriter<Stdout>` for output
  - Parse all input before processing (or use a streaming tokenizer)
  - Batch all output and flush at the end

- [ ] Input format
  - First line: `n q` (number of nodes, number of queries)
  - Second line: `v_1 v_2 ... v_n` (initial vertex values)
  - Next n-1 lines: `u_i v_i` (edges, 1-indexed)
  - Next q lines: operations in one of the formats:
    - `1 u v` -- query path sum from u to v
    - `2 u v` -- query path max from u to v
    - `3 u v w` -- add w to all vertices on path from u to v
    - `4 u w` -- add w to vertex u
    - `5 v` -- query subtree sum of v
    - `6 v w` -- add w to all vertices in subtree of v

- [ ] Output: one line per query operation (types 1, 2, 5), containing the answer

### Code Structure

- [ ] Organize into clear modules
  - `mod hld` containing `struct HLD` with fields for `parent`, `depth`, `subtree_size`, `chain_head`, `pos`, `end`, and methods for `new(adj, root)`, `lca(u, v)`, `path_segments(u, v)` (returns the list of [l, r] ranges on the flat array)
  - `mod segtree` containing `struct SegTree` with `build`, `update_point`, `update_range`, `query_sum`, `query_max`, and `push_down`
  - `mod solve` combining HLD + SegTree to implement the six operation types

- [ ] Each method has a doc comment explaining its purpose, time complexity, and any invariants

- [ ] No `unsafe` code unless justified for performance-critical inner loops with documented safety invariants

- [ ] Use `usize` for indices, `i64` for values (to handle negative values from updates and large sums)

### Testing and Verification

- [ ] Unit tests for HLD construction
  - **Chain graph** (1-2-3-4-5): verify chain_head, pos, depth arrays
  - **Star graph** (1 connected to 2,3,4,5): verify all leaves start new chains
  - **Balanced binary tree**: verify chain structure and logarithmic chain count per path
  - **Random trees**: verify `chain_head[v]` is an ancestor of v, verify `pos` is a valid permutation of `[0, n)`

- [ ] Unit tests for LCA
  - Known LCA pairs on hand-crafted trees
  - Property: `lca(u, v)` is an ancestor of both u and v
  - Property: `lca(u, u) == u`
  - Property: `lca(u, v) == lca(v, u)`
  - Stress test against brute-force LCA on random trees

- [ ] Unit tests for path queries
  - Compute path sum by walking the actual path (using LCA + parent pointers) and summing values, compare against HLD query
  - Compute path max similarly
  - Test after updates: perform an update, then verify the query reflects the change
  - Test paths of length 0 (u == v), length 1 (adjacent nodes), and length n-1 (diameter of a chain)

- [ ] Unit tests for subtree queries
  - Verify subtree_sum against brute-force DFS summation
  - Test after subtree updates
  - Test subtree of the root (should equal total sum of all values)
  - Test subtree of a leaf (should equal the leaf's value)

- [ ] Stress tests
  - Generate random trees with n = 1,000 nodes, random values, perform 10,000 random operations, verify against brute-force after each operation
  - Increase n to 200,000 for performance-only testing (no brute-force verification, just check for panics and timing)

- [ ] Edge-weighted tests
  - Construct edge-weighted tree, verify path queries exclude the LCA correctly
  - Verify edge-weighted path update does not modify the LCA's stored value
  - Test on a path graph where the distinction matters for every query

## Starting Points

- **cp-algorithms**: Heavy-Light Decomposition article at cp-algorithms.com provides a complete explanation with C++ code for path queries and subtree queries
- **Codeforces Blog**: "Heavy-light decomposition" by anudeep2011 is one of the most referenced HLD tutorials in the competitive programming community, with detailed diagrams and worked examples
- **Competitive Programming 4** by Steven Halim: Section on tree decomposition techniques covers HLD alongside centroid decomposition
- **USACO Guide**: The advanced tree techniques section covers HLD with practice problems graded by difficulty
- **Codeforces Problem 243D** "Cubes" and **SPOJ QTREE** series: Classic problems requiring HLD, available for online testing
- **AtCoder Library (ACL)**: While it does not include HLD directly, the LazySegTree implementation serves as an excellent reference for the segment tree component
- **Algorithms Live! Episode on HLD**: YouTube walkthrough of the decomposition with visual animations

## Hints

1. **The key insight of HLD is the "at most O(log n) light edges" property.** Every time you traverse a light edge going up from a node, you move to a node whose subtree is at least twice as large (because the heavy child had a larger subtree). This means you can cross at most log2(n) light edges before reaching the root. Since each heavy chain is a single segment tree range query, the total work per path query is O(log n) segment tree queries, each taking O(log n), for O(log^2 n) total.

2. **Process the heavy child first in the DFS that assigns positions.** This is what guarantees each heavy chain maps to a contiguous range in the flat array. If you process a light child first, the heavy chain gets broken by the light child's subtree, destroying the contiguity property. The heavy child's position is always `pos[parent] + 1`.

3. **The LCA algorithm via HLD works by "climbing chains."** While u and v are on different chains, move the one whose chain head is deeper to the parent of its chain head. When they are on the same chain, the shallower one is the LCA. This is essentially the same traversal used for path queries, which is why HLD gives you LCA "for free."

4. **For the segment tree with lazy propagation, the most common bug is forgetting to push down before accessing children.** Every function that recurses into the left or right child must call `push_down(node)` first. A simple rule: at the top of every recursive function (update or query), push down the current node's lazy value.

5. **For edge-weighted trees, the "exclude LCA" trick is essential.** When querying a path u-v, you decompose it into chain segments. In the final segment (where u and v are on the same chain), the LCA is the shallower node. For edge weights, you must query `[pos[deeper], pos[shallower] + 1]` (i.e., exclusive of the shallower node) instead of `[pos[deeper], pos[shallower]]`. This is because `flat[pos[shallower]]` holds the weight of the edge from shallower to its parent, which is not part of the u-v path.

6. **Iterative DFS is non-negotiable for contest-safe Rust.** A chain graph of 200K nodes means 200K recursive calls, each consuming ~hundreds of bytes of stack. Rust's default 8MB stack cannot handle this. Use an explicit `Vec<(usize, usize)>` as a stack, pushing children in reverse order. For computing subtree sizes, process nodes in reverse order of their DFS discovery to simulate post-order traversal.

7. **When combining HLD with a segment tree, separate the "decompose path into ranges" logic from the "query each range" logic.** Write a method `path_ranges(u, v) -> Vec<(usize, usize)>` that returns all [l, r] contiguous ranges on the flat array. Then your path_sum, path_max, and path_update functions just iterate over these ranges and delegate to the segment tree. This separation reduces bugs and makes it easy to swap the underlying data structure.

8. **For subtree queries, the position interval [pos[v], end[v]] works because the DFS visits all descendants of v before backtracking.** The `end[v]` value is simply `pos[v] + subtree_size[v] - 1`. You already computed subtree sizes, so this is a trivial calculation. No additional DFS is needed.

9. **The re-rooting trick (stretch goal) is elegant but requires careful case analysis.** When re-rooting the tree at node r, the "subtree" of a node v can change. If v is not on the path from the original root to r, its subtree is unchanged. If v is on this path, its "new subtree" is everything except the subtree of its child that is an ancestor of r. You can detect this using the LCA and Euler tour positions: the relevant child c of v is the one where `pos[c] <= pos[r] <= end[c]`.

10. **For the O(log^2 n) to not blow up in practice**, keep the constant factor low. Pre-compute all HLD arrays (chain_head, pos, depth, parent) once. During queries, avoid allocating vectors for chain segments -- instead, process each segment inline as you climb chains. Use `while` loops rather than collecting into a `Vec` and then iterating. In Rust, mark the segment tree query/update methods as `#[inline]` if they are called in a tight loop.

11. **Testing strategy for competitive programming tree problems**: generate random trees by creating a random Prufer sequence (which uniquely encodes a labeled tree), convert it to an edge list, root the tree, and run HLD on it. Compare path query results against a brute-force walk using parent pointers. For stress testing, 1,000 nodes with 10,000 operations is usually enough to catch logic bugs while running in under a second.

12. **If you want to reduce from O(log^2 n) to O(log n) per query**, you can use Euler Tour + segment tree for subtree queries combined with a global segment tree that handles path queries via HLD, but with the segment tree supporting "walk" operations that process the path in a single descent. This is significantly more complex and rarely needed in contest settings, but is a worthy stretch goal.
