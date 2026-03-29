# 148. Persistent Immutable Data Structures

```yaml
difficulty: insane
languages: [rust]
time_estimate: 40-55 hours
tags: [data-structures, immutability, structural-sharing, trie, hamt, persistent-vector, functional-programming]
bloom_level: [create]
```

## Prerequisites

- Rust ownership, borrowing, lifetimes, `Rc`/`Arc`, `Clone`, and interior mutability patterns
- Tree data structures: tries, hash array mapped tries, balanced trees
- Bit manipulation: popcount, bit partitioning, bitmap indexing
- Algorithmic complexity: amortized analysis, understanding O(log32 N) vs O(log2 N)
- Functional programming concepts: immutability, structural sharing, persistent data structures theory

## Learning Objectives

After completing this challenge you will be able to:

- **Create** a persistent vector using a bit-partitioned trie with O(log32 N) indexed access and updates
- **Create** a persistent hash map using a hash array mapped trie (HAMT) with structural sharing
- **Create** transient (mutable) variants that optimize batch operations while preserving the persistent API
- **Create** a version history system where every mutation produces a new version while sharing unchanged structure with previous versions

## The Challenge

Build a library of persistent (immutable, versioned) data structures in Rust that use structural sharing to achieve near-constant-time operations. When you "modify" a persistent vector or map, you get back a new version; the old version remains unchanged and accessible. Internally, the new version shares almost all of its tree structure with the old version, copying only the nodes along the path from root to the modified leaf.

This is the approach used by Clojure's PersistentVector and PersistentHashMap, Scala's immutable collections, and Immutable.js, implemented in Rust with proper ownership semantics.

## Requirements

1. **Persistent vector**: Implement a bit-partitioned trie with branching factor 32 (5-bit partitioning per level). Support `get(index)`, `set(index, value)`, `push_back(value)`, `pop_back()`, and `len()`. All operations return a new vector sharing structure with the original. Access and update in O(log32 N) time. Maintain a tail optimization: the rightmost leaf is stored directly to amortize push_back.

2. **Persistent hash map**: Implement a hash array mapped trie (HAMT). Each internal node uses a 32-bit bitmap to indicate which of 32 possible children are present, and a compressed array containing only the present children. Support `get(key)`, `insert(key, value)`, `remove(key)`, and `len()`. Handle hash collisions with collision nodes at maximum trie depth. All operations return a new map.

3. **Persistent list**: Implement a persistent singly-linked list using `Rc` (or `Arc` for thread safety). Support `cons(value)`, `head()`, `tail()`, and `len()`. Tail sharing means `cons` is O(1) and multiple lists can share their suffixes.

4. **Transient variants**: For each persistent structure, provide a transient mode for batch mutations. Transient vectors and maps mutate nodes in-place (when the reference count is 1) instead of copying. Enter transient mode explicitly, perform batch mutations, then convert back to persistent. Enforce at the type level that transients cannot be shared across threads.

5. **Version history**: Every persistent structure carries a version identifier. Provide a `VersionLog` that tracks the sequence of versions created from a root. Support retrieving any historical version by its identifier. Demonstrate that old versions remain fully functional after subsequent mutations.

6. **Iterator support**: All structures implement `IntoIterator` and provide efficient iterators that traverse the trie without allocating. The iterator for the persistent vector yields elements in index order; the map iterator yields key-value pairs in arbitrary order.

## Hints

1. The bit-partitioned trie uses 5 bits of the index per level. For index `i` at level `l`, the child slot is `(i >> (l * 5)) & 0x1F`. The tree depth is at most 7 for collections up to 32^7 elements. Each internal node is an array of up to 32 children wrapped in `Rc<Node>`.

2. For the HAMT, use `bitmap.count_ones()` (popcount) on the bits below the target position to compute the index into the compressed child array. This maps a sparse 32-slot node into a dense array, saving memory when most slots are empty.

## Acceptance Criteria

- [ ] Persistent vector: `get`, `set`, `push_back`, `pop_back` all return new versions; old versions remain valid and return original values
- [ ] Persistent vector achieves O(log32 N) performance: 1M element operations complete within 2x of equivalent `Vec` random access time
- [ ] Persistent hash map: `insert`, `get`, `remove` work correctly; hash collisions handled at max depth
- [ ] HAMT uses bitmap compression: node memory proportional to present children, not fixed 32 slots
- [ ] Persistent list: `cons` shares tails between lists; two lists derived from the same tail share memory
- [ ] Transient mode: batch insert of 100k elements into transient vector is within 3x of `Vec::push` performance
- [ ] Version history: create 100 versions of a vector, retrieve version 50, verify it contains exactly the state at version 50
- [ ] All structures implement `IntoIterator` and produce correct element sequences
- [ ] `Arc`-based variants are `Send + Sync` and can be shared across threads safely
- [ ] No unsafe code in the public API (internal unsafe for performance is acceptable if documented)

## Resources

- [Bagwell: "Ideal Hash Trees" (2001)](https://infoscience.epfl.ch/record/64398) - Original HAMT paper
- [Hickey: "Clojure Persistent Data Structures" (2008)](https://hypirion.com/musings/understanding-persistent-vector-pt-1) - Understanding persistent vectors
- [Okasaki: "Purely Functional Data Structures" (1998)](https://www.cambridge.org/core/books/purely-functional-data-structures/0409255DA1B48FA731859AC72E34D494) - Theoretical foundation
- [Steindorfer & Vinju: "Optimizing Hash-Array Mapped Tries" (2015)](https://michael.steindorfer.name/publications/oopsla15.pdf) - CHAMP optimization
- [Rust Reference Counting: Rc and Arc](https://doc.rust-lang.org/std/rc/struct.Rc.html) - Rust shared ownership primitives
