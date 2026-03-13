# 43. CP: Link-Cut Trees

**Difficulty**: Insane

## The Challenge

Link-cut trees, introduced by Sleator and Tarjan in 1983, are one of the most powerful and elegant data structures in computer science. They maintain a forest of rooted trees under dynamic modifications — linking two trees together, cutting an edge, and querying aggregate values along root-to-node paths — all in O(log n) amortized time. They are the canonical solution to the "dynamic trees" problem and underpin efficient implementations of maximum flow algorithms (including Dinic's algorithm and Goldberg-Tarjan push-relabel), dynamic connectivity queries, and online bridge/articulation point detection in graphs that change over time.

The core idea is deceptively simple but fiendishly difficult to implement correctly. The forest of "represented trees" is maintained using auxiliary splay trees. Each root-to-leaf path in the represented tree is decomposed into "preferred paths," and each preferred path is stored as a splay tree keyed by depth. The `access(v)` operation restructures the preferred paths so that v and the root of its tree share a preferred path, enabling path queries. `link(u, v)` makes u a child of v by connecting their auxiliary trees. `cut(v)` detaches v from its parent. Every one of these operations requires careful manipulation of splay tree pointers, parent pointers that distinguish "path parents" from "tree parents," and aggregate values that must be maintained through rotations.

This challenge asks you to implement link-cut trees from scratch in Rust, then use them to solve increasingly difficult competitive programming problems. You will need to handle path aggregation (sum, min, max along a path), subtree aggregation (a significantly harder extension), dynamic LCA queries, and edge-weight problems. The Rust implementation adds its own challenges: the splay tree involves pervasive pointer manipulation that fights Rust's ownership model, requiring either unsafe code with raw pointers, arena allocation, or creative use of index-based structures. Getting the implementation both correct and performant is a rite of passage for advanced competitive programmers and data structure enthusiasts.

---

## Acceptance Criteria

### Core Splay Tree Operations
- [ ] Implement a splay tree node with fields: left child, right child, parent, value, aggregate, lazy propagation tag, and a flag indicating whether the node is the root of its auxiliary tree
- [ ] Implement `is_root(v)`: returns true if v is the root of its auxiliary splay tree (i.e., v's parent's left/right child is not v)
- [ ] Implement `rotate(v)`: single rotation (zig) that moves v up one level, handling both left and right cases, updating parent pointers correctly including the path-parent pointer
- [ ] Implement `splay(v)`: splay v to the root of its auxiliary tree using zig, zig-zig, and zig-zag cases; stop when `is_root(v)` is true (not when parent is null)
- [ ] All rotations correctly maintain aggregate values (push up) and propagate lazy tags (push down) before restructuring
- [ ] The splay operation handles the path-parent pointer: when v is splayed to the root, v's parent pointer still points to the path-parent (the top of the next preferred path segment)
- [ ] Implement `push_down(v)`: propagate all lazy tags from v to its children before any structural operation
- [ ] Implement `push_up(v)`: recompute v's aggregate from its children's aggregates and v's own value after any structural change
- [ ] Before splaying v, push down all lazy tags on the path from the splay tree root to v (collect ancestors on a stack, push down top-to-bottom)

### Core Link-Cut Tree Operations
- [ ] Implement `access(v)`: makes v the deepest node on its preferred path from the root; returns the last node that was previously on the preferred path from the root (useful for LCA)
- [ ] `access` works by: splay v, cut v's right child (the deeper part of the old preferred path), then follow path-parent pointers upward, splaying and reconnecting at each level
- [ ] Implement `make_root(v)`: makes v the root of its represented tree by accessing v and then reversing the path (flip the splay tree using lazy propagation)
- [ ] Implement `link(u, v)`: makes u a child of v in the represented tree; precondition: u and v are in different trees; make_root(u), access(v), set u's parent to v
- [ ] Implement `cut(u, v)`: removes the edge between u and v; make_root(u), access(v), detach u from v's left child
- [ ] Implement `find_root(v)`: finds the root of the represented tree containing v; access(v), then walk left to the leftmost node (shallowest), splay it for amortized complexity
- [ ] Implement `connected(u, v)`: returns true if u and v are in the same represented tree; uses find_root
- [ ] All operations maintain the invariant that the splay tree's in-order traversal corresponds to the preferred path ordered by depth
- [ ] Implement `depth(v)`: returns the depth of v in the represented tree (number of edges from v to the root); this is the size of the left subtree in the auxiliary splay tree after `access(v)`
- [ ] Implement `path_length(u, v)`: returns the number of edges on the path from u to v using `make_root(u)`, `access(v)`, and reading the splay tree size
- [ ] Handle invalid operations gracefully: `link` when already connected returns an error or panics with a clear message; `cut` when no edge exists does likewise

### Path Aggregation
- [ ] Support path queries: given two nodes u and v, compute an aggregate (sum, min, max, xor) of all node values on the path from u to v
- [ ] Implement using `make_root(u)`, `access(v)`, then read the aggregate of v's auxiliary splay tree
- [ ] Support path updates: add a value to all nodes on the path from u to v using lazy propagation
- [ ] Lazy propagation correctly handles the `reverse` flag from `make_root` combined with additive/assignment updates
- [ ] Implement at least two different aggregate types (e.g., sum + max, or sum + xor) to demonstrate the generality of the approach
- [ ] Support single-node value updates: change the value of a specific node and update aggregates accordingly
- [ ] Verify path query results against brute-force path traversal on the represented tree for all test cases
- [ ] Support path set (assign all nodes on a path to a single value) in addition to path add

### Edge Weight Support
- [ ] Support edge-weighted link-cut trees by representing each edge as a virtual node between its endpoints
- [ ] When linking u to v with weight w, create a node for the edge with value w and link u-edge-v
- [ ] When cutting u-v, identify and remove the edge node
- [ ] Path queries on edge-weighted trees should aggregate edge weights, not node values
- [ ] Demonstrate that edge weighting works correctly with path queries and updates
- [ ] Support updating an edge's weight: given edge (u, v), find its virtual node and change its value, then update aggregates along the affected path
- [ ] Implement `find_min_edge(u, v)`: return the minimum-weight edge on the path from u to v (used in max flow algorithms)
- [ ] Implement `find_max_edge(u, v)`: return the maximum-weight edge on the path from u to v (used in minimum spanning tree verification)

### Lazy Propagation
- [ ] Implement lazy tag for path reversal (used by `make_root`): stores a boolean flip flag, swaps left/right children when pushed down
- [ ] Implement lazy tag for path addition: add a constant to all values on a path
- [ ] Implement lazy tag for path assignment: set all values on a path to a constant
- [ ] Multiple lazy tags compose correctly: push down must apply tags in the right order
- [ ] Lazy tags are pushed down before any structural operation (splay, access) that needs to read child pointers
- [ ] Implement a unit test that applies all combinations of lazy tags (reverse, add, assign) in different orders and verifies the result matches eager application
- [ ] Lazy tags correctly propagate through `find_root` and `connected` operations, not just `access` and `make_root`
- [ ] The reversal lazy tag correctly updates aggregate values that are not symmetric (e.g., "leftmost value" vs "rightmost value" aggregates swap when reversed)

### Subtree Aggregation (Advanced Extension)
- [ ] Implement subtree aggregate queries: given a node v, compute the aggregate value of all nodes in v's subtree in the represented tree
- [ ] This requires maintaining "virtual" children information: each node tracks the aggregate of its non-preferred (virtual) children separately from its splay tree (preferred path) children
- [ ] When `access(v)` changes preferred children, update the virtual aggregate by adding the old preferred child's contribution and removing the new preferred child's contribution
- [ ] Subtree aggregation correctly accounts for all descendants, not just those on the preferred path
- [ ] Verify subtree aggregates against a brute-force DFS on the represented tree

### Dynamic Connectivity
- [ ] Implement dynamic connectivity queries: maintain a graph under edge insertions and deletions, answer "are u and v connected?" queries
- [ ] Use link-cut trees as the spanning forest, with a strategy for handling non-tree edges
- [ ] Handle edge deletion when the deleted edge is in the spanning forest: search for a replacement edge among non-tree edges
- [ ] Support at least 100,000 operations (mixed link/cut/connected queries) within 2 seconds
- [ ] Implement online bridge detection: after each edge insertion/deletion, report whether a given edge is a bridge
- [ ] Track the number of non-tree edges covering each tree edge to efficiently determine bridge status
- [ ] Implement a `count_components()` method that returns the number of connected components in the current forest
- [ ] Support edge existence queries: given u and v, determine whether there is a direct edge between them in the represented tree (not just connectivity)

### Dynamic LCA Queries
- [ ] Implement `lca(u, v)`: returns the lowest common ancestor of u and v in the current rooted tree
- [ ] Use the property that `access(u)` followed by `access(v)` returns the LCA as the last node where the access path changed preferred children
- [ ] Handle the case where the tree is re-rooted (after `make_root`): LCA is defined with respect to the current root
- [ ] Verify LCA correctness against a naive O(n) implementation on random trees
- [ ] Implement `dist(u, v)`: return the number of edges on the unique path from u to v, using LCA and depth queries
- [ ] Test LCA with degenerate tree shapes: path graphs (chain), star graphs, and complete binary trees

### Competitive Programming Problems
- [ ] **Problem 1 — Dynamic Path Queries**: Given a tree of N nodes with values, support: update node value, query sum/min/max on path between two nodes, link two trees, cut an edge. Handle up to 200,000 operations.
- [ ] **Problem 2 — Maximum Flow via Link-Cut Trees**: Implement Dinic's maximum flow algorithm using link-cut trees to find and augment along blocking flows in O(V^2 * E / log V) time, improving over the standard O(V^2 * E)
- [ ] **Problem 3 — Online Bridge Detection**: Given an initially empty graph, process edge additions and report whether each edge is a bridge after each operation, using link-cut trees to maintain the 2-edge-connected components
- [ ] **Problem 4 — Dynamic Tree Path Maximum**: Maintain a weighted forest; support link, cut, and "find the maximum weight edge on the path from u to v" queries. This is the classic application that motivated link-cut trees.
- [ ] **Problem 5 — Dynamic LCA with Updates**: Given a rooted tree that changes via link/cut operations, answer LCA queries online. Verify results against a precomputed heavy-light decomposition on static snapshots.
- [ ] Each problem solution passes within the time limit (typically 2-3 seconds for N, Q up to 200,000)
- [ ] Provide a test generator for each problem that creates random instances of configurable size, along with a brute-force solver for verification

### Implementation Quality
- [ ] The implementation uses arena allocation (Vec-based with indices) or raw pointers with unsafe — document the choice and trade-offs
- [ ] No memory leaks: if using unsafe, verify with Miri or similar tooling
- [ ] Implement a debug mode that validates the splay tree invariants after every operation: BST property, parent pointer consistency, aggregate correctness
- [ ] Provide a stress test that compares link-cut tree results against a brute-force O(n) implementation on random forests with random operations
- [ ] Benchmark: 200,000 random operations (mix of link, cut, path query) on a forest of 100,000 nodes should complete in under 2 seconds in release mode
- [ ] The code compiles with no warnings under `#[deny(warnings)]`
- [ ] Provide a generic interface: the aggregate type and lazy tag type are parameterized via traits, allowing users to plug in different aggregation strategies without modifying the core link-cut tree logic
- [ ] Document the amortized analysis: explain why each operation is O(log n) amortized, referencing the potential function argument from Sleator and Tarjan
- [ ] Implement a visualization mode that outputs the current state of the auxiliary splay trees and preferred path decomposition in a human-readable format (useful for debugging)

---

## Starting Points

These are real resources to study before and during implementation:

1. **Sleator and Tarjan - "A Data Structure for Dynamic Trees"** (Journal of Computer and System Sciences, 1983) — The original paper. Dense but precise. Read Sections 1-3 for the core algorithm. The key insight is the preferred path decomposition and the use of splay trees as auxiliary structures.

2. **Tarjan - "Data Structures and Network Algorithms"** (SIAM, 1983) — Chapter 5 covers dynamic trees in a more tutorial style than the original paper. Includes worked examples of access, link, and cut operations.

3. **MIT 6.851 Advanced Data Structures** (Erik Demaine, Lecture 19) — Lecture notes and video freely available. Covers link-cut trees with clear diagrams of the preferred path decomposition and auxiliary splay tree structure. One of the best pedagogical treatments.

4. **CP-Algorithms: Link-Cut Trees** (https://cp-algorithms.com/data_structures/link-cut-tree.html) — Competitive programming focused tutorial with clean pseudocode. Covers the standard operations and common contest applications. Good as a reference during implementation.

5. **Competitive Programmer's Handbook (Laaksonen)** and **Algorithms Live! YouTube channel** — Both cover link-cut trees in the context of competitive programming. The YouTube videos show step-by-step examples with visual diagrams.

6. **Renato Werneck - "Design and Analysis of Data Structures for Dynamic Trees"** (PhD thesis, Princeton, 2006) — Comprehensive treatment of dynamic trees including link-cut trees, Euler tour trees, and top trees. Chapter 2 is an excellent self-contained introduction.

7. **KACTL (KTH Algorithm Competition Template Library)** (https://github.com/kth-competitive-programming/kactl) — The `content/graph/LinkCutTree.h` file contains a battle-tested C++ implementation in under 50 lines. Study it for the minimal correct implementation, then extend.

8. **Tarjan's Splay Tree Paper** — "Self-Adjusting Binary Search Trees" (Sleator and Tarjan, 1985). Understanding splay trees deeply is a prerequisite. Pay special attention to the amortized analysis using the potential function, which extends to link-cut trees.

9. **ecnerwala's competitive programming library** (https://github.com/ecnerwala/cp-book) — Contains a clean C++ link-cut tree implementation with path aggregation and lazy propagation. Study the `push` and `pull` functions for maintaining invariants through rotations.

10. **Codeforces Blog: Link-Cut Tree** (search Codeforces for "link cut tree tutorial") — Multiple community tutorials with problem sets. Particularly useful for finding contest problems to test your implementation against online judges.

---

## Hints

1. Start with a plain splay tree before attempting the full link-cut tree. Implement `rotate`, `splay`, `insert`, `find`, and verify they work correctly. The link-cut tree's correctness depends entirely on the splay tree working perfectly, especially the parent pointer management during rotations.

2. The most subtle part of the implementation is the distinction between "real" parent pointers (within the same auxiliary splay tree) and "path-parent" pointers (connecting different auxiliary splay trees). A node v has a path-parent if `v.parent` is set but `v.parent.left != v` and `v.parent.right != v`. The `is_root` function checks exactly this condition. Getting this wrong causes silent corruption.

3. For the Rust implementation, the arena-based approach is strongly recommended over raw pointers. Use a `Vec<Node>` and represent all pointers as `usize` indices (or `Option<usize>` for nullable pointers). This avoids unsafe code entirely while maintaining cache-friendly memory layout. Define a `LinkCutTree` struct that owns the arena and provides methods.

4. The `access(v)` operation is the heart of everything. Here is the sequence: (a) splay(v) to bring v to the root of its auxiliary tree; (b) set v's right child to null (detach the deeper part of the old preferred path); (c) while v has a path-parent p: splay(p), set p's right child to v (making v's preferred path extend through p), splay(v); (d) return the last p encountered (this is the LCA if you called access on another node just before).

5. `make_root(v)` must reverse the order of nodes on the preferred path from v to the root. Since the splay tree is keyed by depth, reversing the path means swapping left and right children throughout the splay tree. Do this lazily: set a `reversed` flag on the root and push it down when needed. This is identical to the "reverse a range" operation in a splay tree.

6. When implementing lazy propagation, always `push_down(v)` before accessing v's children. In particular, `splay` must push down along the path from the root to v before rotating. A common pattern: before splaying v, walk up to find the root (following parent pointers while `!is_root`), collect the path on a stack, then push down from top to bottom.

7. For path aggregation, maintain an `aggregate` field on each node that combines the node's own value with its left and right subtrees' aggregates. After every rotation or structural change, call `push_up(v)` to recompute v's aggregate from its children. The order is: push_down before touching children, push_up after modifying structure.

8. Lazy tag composition is tricky when you have multiple tag types (reverse + add + assign). Define a clear order: (a) if an assign tag is present, it overrides any add tag; (b) reverse swaps left and right children and recursively flips the reverse tag on children; (c) add is applied after assign. Implement `compose_tags(existing, new)` and test it thoroughly in isolation.

9. For edge-weighted link-cut trees, the standard trick is to create a "virtual node" for each edge. When linking u to v with weight w, you actually do `link(u, edge_node)` and `link(edge_node, v)`. When querying the path from u to v, the aggregated value includes the edge nodes' values. When cutting, you cut both links. This means your tree has up to 2N-1 nodes for N original nodes.

10. For the maximum flow application (Dinic's algorithm with link-cut trees): use the link-cut tree to represent the blocking flow tree. When finding an augmenting path, `find_root` gives the end of the path. Use path minimum to find the bottleneck edge. Use path update to subtract the bottleneck capacity. Cut saturated edges. This gives O(EV log V) for unit-capacity graphs.

11. For dynamic bridge detection: maintain a spanning forest using link-cut trees. Each non-tree edge has a "level" and is associated with both endpoints. When a tree edge is deleted, search for a replacement among non-tree edges at the appropriate level. This is the offline version; the online version using link-cut trees with Euler tour trees is more complex but achievable.

12. Stress testing is absolutely essential. Write a brute-force `NaiveForest` that maintains the forest as adjacency lists and computes path queries by BFS/DFS. Generate random sequences of link/cut/query operations, run both implementations, and assert identical results. Run with at least 10,000 operations on forests of size 1,000. Many subtle bugs only appear with specific operation sequences.

13. For performance, the constant factor matters enormously in competitive programming. Avoid heap allocation during operations. Use `u32` instead of `usize` for indices if node count fits. Minimize branch mispredictions in the splay loop. Consider implementing the "semi-splay" variant if full splay is too slow in practice (though the theoretical bound requires full splay).

14. A common bug: forgetting to splay `find_root`'s result back to the root after walking to the leftmost node. Without this splay, the amortized O(log n) bound breaks and you get O(n) worst case. The sequence is: `access(v)`, walk left to leftmost node u, `splay(u)`, return u.

15. Another common bug: in `cut(u, v)`, after `make_root(u)` and `access(v)`, you need to verify that u is indeed v's left child (meaning the edge u-v actually exists). If it does not, the cut should fail or be a no-op. In contests, this edge case often corresponds to cutting an edge that was already cut.

16. To verify your implementation handles all edge cases, test these specific scenarios: (a) link and cut the same edge repeatedly; (b) access every node in a long chain (path graph); (c) link N nodes into a star graph, then cut all edges; (d) make_root on the same node twice in a row; (e) query on a single-node tree; (f) link-cut-link the same pair of nodes.

17. For the subtree aggregation extension, the key insight is that each splay tree node must maintain two separate aggregate values: `path_aggregate` (aggregating the splay tree subtree, i.e., the preferred path segment) and `virtual_aggregate` (aggregating all non-preferred children's full subtree values). When access changes a preferred child, subtract the old preferred child's total from `path_aggregate` and add it to `virtual_aggregate`, then do the reverse for the new preferred child. This bookkeeping in `access` is the most error-prone part.

18. For competitive programming submissions, you will likely need to read input and write output efficiently. Use `BufReader` and `BufWriter` with `stdin`/`stdout`. Parse input with a custom scanner that reads by whitespace tokens. The I/O overhead can be significant for 200,000+ operations — some solutions spend more time on I/O than on the actual data structure operations.

19. If you find that your splay tree implementation is correct but too slow, profile it. The most common performance issue is excessive memory indirection. With arena allocation (`Vec<Node>`), accessing `nodes[nodes[v].left]` involves two array lookups, which is cache-friendly. With `Box<Node>` pointers, each access is a pointer chase that may miss the cache. Arena allocation typically gives a 2-3x speedup for link-cut trees.

20. An alternative to link-cut trees for some problems is the Euler Tour Tree (ETT), which represents each tree as an Euler tour stored in a balanced BST. ETTs support subtree queries more naturally but do not support path queries efficiently. For problems requiring both path and subtree queries, link-cut trees with the virtual aggregate extension are the only known O(log n) solution. Understanding when to use each data structure is itself an important skill.

21. For the `access` operation, trace through a concrete example by hand before coding. Draw a forest of 7 nodes, choose a preferred path decomposition, draw the auxiliary splay trees, then execute `access(v)` step by step: splay v, detach right child, follow path-parent to next auxiliary tree root, splay that root, attach v's tree as right child, splay v again. Repeat until no path-parent exists. Drawing this on paper is almost mandatory for understanding.

22. When implementing the generic aggregate trait, define something like `trait Aggregate: Clone + Default { fn combine(&self, other: &Self) -> Self; fn identity() -> Self; }` and `trait LazyTag: Clone + Default { fn apply(&self, aggregate: &mut Aggregate, size: usize); fn compose(&self, other: &Self) -> Self; }`. This lets users instantiate the link-cut tree with `SumAggregate`, `MaxAggregate`, `XorAggregate`, etc. without modifying core code.

23. For debugging, implement an `assert_invariants` method that traverses every node and checks: (a) if node v has left child l, then l's parent is v; (b) if v has right child r, then r's parent is v; (c) if v is not `is_root`, then v's parent's left or right child is v; (d) v's aggregate equals `combine(left.aggregate, v.value, right.aggregate)`; (e) no lazy tags are pending on any node (call after a full push_down sweep). Run this after every operation in debug builds. It will catch most bugs immediately.

24. Consider implementing a "top tree" interface on top of your link-cut trees for extra credit. Top trees extend link-cut trees with the ability to maintain information about subtrees, not just paths. They were introduced by Alstrup, Holm, de Lichtenberg, and Thorup, and can solve problems like maintaining the diameter of a dynamic tree in O(log^2 n) time. This is at the absolute frontier of competitive programming data structures.

25. For the Dinic's max flow problem (Problem 2), the high-level algorithm is: (a) BFS from source to sink to compute level graph; (b) Find blocking flow in level graph using DFS + link-cut tree. For each augmenting path found by DFS: link all edges, find path minimum (bottleneck capacity), subtract bottleneck from all edges on path, cut all edges with zero remaining capacity. The link-cut tree turns the O(VE) blocking flow phase into O(E log V), giving overall O(V^2 E / log V) for general graphs.

26. When implementing for competitive programming, you will encounter time limits that are tight. In addition to algorithmic correctness, you need fast I/O. Use a custom reader that reads the entire input at once with `std::io::Read::read_to_end`, then parses tokens from the byte buffer. For output, accumulate into a `String` and write once at the end. This can save 100-200ms on problems with 200,000+ operations.

27. A subtle correctness issue arises with `make_root`: if you make_root(u), then the "root" of the represented tree changes, which means that parent-child relationships in the represented tree are reversed along the path from u to the old root. All operations that depend on root identity (like LCA, which is defined relative to the root) must account for this. If you make_root(u) and later make_root(v), the root changes again. Your tests must exercise sequences of make_root calls to verify consistency.

28. For the online bridge detection problem: after adding edge (u, v), if u and v were already connected (find_root(u) == find_root(v)), the edge is a non-tree edge and creates a cycle. Mark all edges on the path from u to v in the spanning tree as "covered" (not bridges). If u and v were not connected, the edge becomes a tree edge (link) and is initially a bridge (it could become non-bridge later when a non-tree edge covers it). Use path aggregation to track the minimum coverage count on each edge.
