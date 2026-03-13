# 31. CP: Persistent Data Structures

**Difficulty**: Insane

## The Challenge

Implement persistent data structures that preserve all previous versions of themselves after modification. In standard (ephemeral) data structures, an update destroys the old version. In persistent data structures, both the old and new versions coexist, enabling queries on any historical state. This is achieved through structural sharing — new versions reuse most of the old structure, only creating new nodes along the modified path.

The flagship application is the persistent segment tree, which enables problems like "Kth smallest element in a range" to be solved in O(log n) per query with O(n log n) preprocessing. Each version shares O(n) nodes with adjacent versions, with only O(log n) new nodes per update (path copying). The total space is O(n log n) — manageable for competitive programming constraints.

Implementing persistence in Rust is uniquely challenging because of ownership rules. You cannot have multiple versions pointing to shared subtrees with `Box` alone — you need `Rc<T>` (or arena allocation) for shared ownership. This exercise forces you to confront the tension between Rust's ownership model and persistent data structure patterns, and to find efficient solutions using arenas, `Rc`, or raw indices.

## Acceptance Criteria

### Persistent Segment Tree
- [ ] Implement path-copying persistence: each update creates O(log n) new nodes, shares the rest
- [ ] Support point update (create new version) and range query (on any version)
- [ ] Node structure: `left: NodeRef`, `right: NodeRef`, `value: T` where `NodeRef` allows sharing
- [ ] Store roots array: `roots[version]` points to the root of that version
- [ ] Merge query on two versions (required for Kth smallest in range)
- [ ] Space complexity: O(n log n) for n updates on a range of size n

### Persistent Array
- [ ] Implement persistent array using a persistent segment tree as backing store
- [ ] `get(version, index) -> T` in O(log n)
- [ ] `set(version, index, value) -> new_version` in O(log n)
- [ ] Support at least 200,000 versions

### Persistent Union-Find
- [ ] Union-find with path copying (not path compression — it breaks persistence)
- [ ] `find(version, x) -> root` using persistent array for parent storage
- [ ] `union(version, x, y) -> new_version` with union by rank
- [ ] O(log^2 n) per operation (log n for find depth, log n for persistent array access)

### Problems (implement and solve all)
- [ ] **Kth Smallest in Range**: Given array A of n elements, answer Q queries: "what is the Kth smallest element in A[l..r]?" Build persistent segment tree over sorted values, one version per array prefix. Answer by binary searching on `roots[r] - roots[l-1]`.
- [ ] **Online Historical Queries**: Given a sequence of array updates and queries, support: "set A[i] = v" (creates new version) and "what was A[i] in version t?". All queries are online (each query depends on previous answer).
- [ ] **Range Distinct Count**: Given array, answer: "how many distinct values in A[l..r]?" Offline solution using persistence + sweep.
- [ ] **Dynamic Connectivity Rollback**: Given a graph with edge insertions and deletions (but deletions only undo the most recent insertion — stack-like), answer connectivity queries. Use persistent union-find.

### Performance
- [ ] N = 200,000 elements, Q = 200,000 queries, each under 3 seconds
- [ ] Memory usage under 512 MB (careful with node allocation)
- [ ] Fast I/O for competitive programming

### Rust-Specific Requirements
- [ ] Implement at least two approaches: `Rc`-based nodes and arena-based nodes (index into `Vec<Node>`)
- [ ] Benchmark both approaches — arena should be 3-10x faster due to cache locality and no reference counting
- [ ] No memory leaks (Rc cycles are impossible in tree structures, but verify)

## Starting Points

- **cp-algorithms.com — Persistent Segment Tree**: Clear explanation with C++ code using raw pointers. Study the `build`, `update`, and `query` functions. Your Rust version replaces pointers with either `Rc<Node>` or `usize` indices into an arena.

- **MIT 6.851: Advanced Data Structures** (Erik Demaine): Lecture 1 covers persistence theory — full persistence, partial persistence, confluent persistence. You only need partial persistence (query any version, update only the latest), but understanding the full taxonomy helps.

- **"Making Data Structures Persistent"** (Driscoll, Sarnak, Sleator, Tarjan, 1986): The foundational paper. Section 2 on path copying is exactly what you will implement.

- **typed-arena crate**: Arena allocator for Rust. Using `Arena<Node>` avoids Rc overhead and gives you cache-friendly allocation. Each node is an index into the arena's backing Vec.

- **Competitive Programming 3** (Halim): Chapter on range trees includes persistent segment tree with practical examples.

## Hints

1. Start with the arena-based approach — it is simpler in Rust. Define `struct Node { left: u32, right: u32, value: i64 }` and `struct Arena { nodes: Vec<Node> }`. Node references are `u32` indices. Creating a new node is `arena.nodes.push(node); arena.nodes.len() - 1`. Use `0` or `u32::MAX` as null sentinel.

2. For the persistent segment tree, `update(arena, root, pos, val, lo, hi) -> new_root` creates a new node with the same children as the old node, except along the path to `pos`. Pseudocode:
   ```
   new_node = clone(old_node)
   if leaf: new_node.value = val
   else if pos in left half: new_node.left = update(arena, old_node.left, pos, val, lo, mid)
   else: new_node.right = update(arena, old_node.right, pos, val, mid+1, hi)
   return new_node_index
   ```

3. For "Kth smallest in range [l, r]": coordinate compress values, build initial empty persistent segment tree (version 0), then for each prefix `i`, create version `i` by inserting `A[i]` (increment count at position `rank(A[i])`). To answer Kth smallest in `[l, r]`, walk down `roots[r]` and `roots[l-1]` simultaneously: if `left_count(r) - left_count(l-1) >= k`, go left; else subtract and go right.

4. The `Rc`-based approach uses `Rc<Node>` where `Node { left: Option<Rc<Node>>, right: Option<Rc<Node>>, value: i64 }`. Clone is cheap (reference count bump). But Rc adds 16 bytes overhead per node (strong + weak counts) and prevents cache-friendly layout.

5. For the persistent union-find, you CANNOT use path compression because it mutates the tree (breaking old versions). Use union-by-rank only, which gives O(log n) find without compression. Store parent and rank arrays as persistent arrays.

6. Memory management: with n=200,000 and log2(200,000)≈18, you create about 18 nodes per update. With 200,000 updates, that is 3.6M nodes. At ~20 bytes per node (arena), that is ~72MB — well within limits. Pre-allocate the arena: `Vec::with_capacity(4_000_000)`.

7. For the `Rc` approach, use `Rc::make_mut` for copy-on-write semantics. If the Rc has refcount 1, it mutates in place (no copy). If refcount > 1, it clones. This gives automatic structural sharing.

8. When debugging, add a `version_size(root) -> usize` function that counts reachable nodes from a root. Early versions should have exactly `2n-1` nodes (full segment tree), while later versions share most nodes with previous versions.

9. For competitive programming, the arena approach with `u32` indices is the fastest. Pre-allocate, never deallocate, and use `nodes.len() as u32` as the "allocator". This is exactly how C++ competitive programmers handle persistent structures with raw arrays.

10. Common bug: forgetting to create version 0 (the initial empty tree with all zeros). The Kth smallest solution requires `roots[0]` to be a valid empty tree that `roots[l-1]` can reference when `l = 1`.

## Resources

- cp-algorithms: https://cp-algorithms.com/data_structures/segment_tree.html#persistent-segment-tree
- Codeforces problems: 813D, 840D, 786C, 893F
- SPOJ: MKTHNUM (Kth smallest in range — the classic persistent seg tree problem)
- MIT 6.851 lectures: https://courses.csail.mit.edu/6.851/
