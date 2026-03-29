<!-- difficulty: insane -->
<!-- category: data-structures, functional-programming -->
<!-- languages: [rust] -->
<!-- concepts: [persistent-data-structures, structural-sharing, tries, HAMT, path-copying, transients] -->
<!-- estimated_time: 35-55 hours -->
<!-- bloom_level: analyze, evaluate, create -->
<!-- prerequisites: [tree-data-structures, hash-maps, bitwise-operations, reference-counting, amortized-analysis] -->

# Challenge 148: Persistent Immutable Data Structures

## Languages

Rust (stable, latest edition)

## Prerequisites

- Strong understanding of tree-based data structures and trie variants
- Familiarity with bitwise operations (popcount, bit masking, bit partitioning)
- Experience with `Arc<T>` and reference counting for shared ownership
- Understanding of amortized complexity analysis and the concept of structural sharing

## Learning Objectives

- **Analyze** how structural sharing enables O(log N) updates on immutable collections while preserving all previous versions
- **Evaluate** the memory and performance trade-offs between persistent and ephemeral data structures
- **Create** a persistent vector using bit-partitioned tries with O(log32 N) access and update
- **Create** a persistent hash map using Hash Array Mapped Tries (HAMT) with near-O(1) operations
- **Create** transient (temporarily mutable) variants for efficient batch operations

## The Challenge

Build persistent (immutable, versioned) data structures that use structural sharing to provide efficient updates without mutating the original. After an "update" operation, you get a new version of the structure that shares most of its internal nodes with the previous version. Both versions remain valid and accessible indefinitely.

The core data structures are:

**Persistent Vector**: A bit-partitioned trie (like Clojure's PersistentVector) where the index is split into 5-bit chunks, each selecting a child in a 32-way branching tree. Access is O(log32 N) -- effectively O(1) for practical sizes (log32 of 1 billion is only 6). Updates create a new path from root to the modified leaf, sharing all untouched branches.

**Persistent Hash Map**: A Hash Array Mapped Trie (HAMT, like Clojure's PersistentHashMap) where the hash is consumed 5 bits at a time to navigate a 32-way trie. Collisions are resolved with linked chains at the leaves. The HAMT uses population count (popcount) compressed arrays to avoid allocating 32-slot arrays when most are empty.

**Persistent List**: A simple linked list with `Arc` nodes providing structural sharing on `tail` operations.

**Transient variants**: Temporarily mutable versions of the persistent structures that can be mutated in place for batch operations, then "frozen" back into a persistent version. Transients detect stale references using an owner ID.

## Requirements

1. Implement `PersistentList<T>` with `cons`, `head`, `tail`, `len`, `iter` -- all O(1) except `len` which is O(1) stored
2. Implement `PersistentVec<T>` (bit-partitioned trie, branching factor 32): `get(index)`, `set(index, value)`, `push`, `pop`, `len`, `iter`
3. `PersistentVec` must use `Arc<Node>` for structural sharing -- set/push/pop return a new version, the old version remains valid
4. Implement `PersistentHashMap<K, V>` (HAMT): `get`, `insert`, `remove`, `len`, `iter`, `keys`, `values`
5. HAMT nodes must use popcount-compressed arrays: a 32-bit bitmap indicates which slots are present, the data array only stores present entries
6. Handle hash collisions in the HAMT via collision nodes (linked list of entries with the same hash prefix)
7. Implement `TransientVec<T>` from `PersistentVec<T>` with an owner ID: mutations are in-place while the owner matches, freezing produces a new `PersistentVec`
8. Implement `TransientHashMap<K, V>` with the same owner-ID-based mutation pattern
9. All operations must have correct asymptotic complexity: O(log32 N) for vec access/update, O(log32 N) for HAMT get/insert/remove
10. Version history must work: after `let v2 = v1.set(0, x)`, both `v1` and `v2` are usable with their respective values
11. Implement `Iterator` for all three structures
12. Write stress tests: insert 100,000 elements, verify all versions are correct

## Hints

Insane challenges provide minimal guidance. These are directional signposts only.

- For the persistent vector, a node is either `Internal(Arc<[Option<Arc<Node<T>>>; 32]>)` or `Leaf(Arc<[Option<T>; 32]>)`. The tree depth is `ceil(log32(len))`. Index bits `[30:26]` select the child at depth 0, `[25:21]` at depth 1, and so on.
- For path copying: to set index `i`, clone the nodes along the path from root to the leaf containing `i`, modify only the cloned leaf, and return a new root pointing to the cloned path. All other branches are shared via `Arc`.
- The HAMT bitmap trick: a 32-bit bitmap where bit `j` is set if child `j` exists. The array stores only present children contiguously. To find the array index of child `j`: `bitmap & ((1 << j) - 1)).count_ones()`.
- For transients, each mutable node carries an owner ID (`Arc<()>` works). Mutation is allowed if the node's owner matches the transient's owner. Otherwise, the node is cloned first (copy-on-write within the transient).

## Acceptance Criteria

- [ ] `PersistentList<T>` supports `cons`, `head`, `tail` in O(1) with structural sharing
- [ ] `PersistentVec<T>` supports `get`, `set`, `push`, `pop` in O(log32 N) with path copying
- [ ] Previous versions of `PersistentVec` remain valid after updates (structural sharing verified)
- [ ] `PersistentHashMap<K, V>` supports `get`, `insert`, `remove` in O(log32 N) using HAMT with popcount compression
- [ ] Hash collisions are handled correctly in the HAMT
- [ ] `TransientVec` and `TransientHashMap` perform batch operations without path copying, then freeze to persistent
- [ ] All structures implement `Iterator`
- [ ] Stress test: 100,000+ element operations with version verification
- [ ] All tests pass with `cargo test`

## Research Resources

- [Understanding Clojure's Persistent Vectors (Jean Niklas L'orange)](https://hypirion.com/musings/understanding-persistent-vector-pt-1) -- the definitive visual explanation of bit-partitioned tries
- [Ideal Hash Trees (Bagwell, 2001)](https://infoscience.epfl.ch/record/64398/files/idealhashtrees.pdf) -- the original HAMT paper
- [Understanding Clojure's PersistentHashMap](https://blog.higher-order.net/2009/09/08/understanding-clojures-persistenthashmap-deftwice.html) -- HAMT internals
- [Clojure PersistentVector source](https://github.com/clojure/clojure/blob/master/src/jvm/clojure/lang/PersistentVector.java) -- reference implementation
- [The `im` crate source code](https://github.com/bodil/im-rs) -- production Rust persistent data structures
- [Transients in Clojure](https://clojure.org/reference/transients) -- the transient optimization pattern
