<!-- difficulty: intermediate-advanced -->
<!-- category: data-structures -->
<!-- languages: [rust] -->
<!-- concepts: [skip-list, probabilistic-data-structures, iterators, generics, concurrency] -->
<!-- estimated_time: 5-7 hours -->
<!-- bloom_level: apply, analyze -->
<!-- prerequisites: [linked-lists-in-rust, ordering-traits, iterator-trait, rwlock-basics] -->

# Challenge 12: Skip List Implementation

## Languages

Rust (stable, latest edition)

## Prerequisites

- Experience with Rust ownership and borrowing (especially with linked structures)
- Understanding of `Ord`, `PartialOrd`, and comparison traits
- Familiarity with the `Iterator` trait and implementing custom iterators
- Basic understanding of `RwLock` for concurrent read access

## Learning Objectives

- **Implement** a multi-level linked list with probabilistic balancing
- **Apply** Rust's `Iterator` trait to enable idiomatic traversal and range queries
- **Analyze** the performance characteristics of skip lists versus balanced trees
- **Design** a concurrent-read-safe wrapper using `RwLock` without deadlocks
- **Compare** empirical benchmarks against `BTreeMap` for various workloads

## The Challenge

Build a skip list -- a probabilistic data structure that provides O(log n) average-case search, insertion, and deletion by maintaining multiple layers of linked lists. The bottom layer contains all elements in sorted order. Each successive layer acts as an "express lane" that skips over elements, enabling fast traversal.

Your skip list must be generic over key and value types (`K: Ord, V`). Each insertion randomly determines the level of the new node using a coin-flip probability (typically p=0.5). Implement the full `Iterator` trait for forward traversal and a `range()` method that returns an iterator over a key range. The maximum level should be bounded (16 or 32 levels is typical).

Wrap the skip list in a concurrent-read-safe container using `RwLock`, allowing multiple readers or a single writer. Finally, benchmark your implementation against `std::collections::BTreeMap` for insert, lookup, and range query workloads to understand the practical performance trade-offs.

## Requirements

1. Implement `SkipList<K: Ord, V>` with `insert()`, `get()`, `remove()`, and `contains()` methods
2. Use randomized level generation with configurable probability (default p=0.5)
3. Set a maximum level (default 16) and track the current highest active level
4. Implement `Iterator` for forward traversal yielding `(&K, &V)` pairs
5. Implement `range(start..end)` returning an iterator over the specified key range
6. Provide `len()`, `is_empty()`, and `clear()` methods
7. Implement `Display` for visualization of the level structure (show nodes at each level)
8. Wrap in `ConcurrentSkipList<K, V>` using `RwLock` with `read()` and `write()` access
9. Write benchmarks comparing against `BTreeMap` for 10k, 100k, and 1M elements
10. Ensure all operations maintain the skip list invariant: every node at level `i` also exists at all levels below `i`

## Hints

<details>
<summary>Hint 1: Node structure</summary>

Each node holds a key-value pair and a vector of forward pointers (one per level). Using `Option<Box<Node>>` avoids unsafe:

```rust
struct Node<K, V> {
    key: K,
    value: V,
    forward: Vec<Option<Box<Node<K, V>>>>,
}

impl<K, V> Node<K, V> {
    fn new(key: K, value: V, level: usize) -> Self {
        Node {
            key,
            value,
            forward: (0..level).map(|_| None).collect(),
        }
    }
}
```

Note: this owned-box approach is simpler but makes some operations harder than a raw-pointer approach. Either is acceptable.

</details>

<details>
<summary>Hint 2: Random level generation</summary>

```rust
use rand::Rng;

fn random_level(max_level: usize, probability: f64) -> usize {
    let mut rng = rand::thread_rng();
    let mut level = 1;
    while level < max_level && rng.gen::<f64>() < probability {
        level += 1;
    }
    level
}
```

With p=0.5, the expected number of levels for a node is 2, and the expected max level across n elements is about log2(n).

</details>

<details>
<summary>Hint 3: Search path (update vector)</summary>

Before inserting or removing, you walk the list top-down and record the last node visited at each level. This "update vector" tells you where to splice:

```rust
// Pseudocode for building the update vector
let mut update: Vec<*mut Node<K, V>> = vec![std::ptr::null_mut(); max_level];
let mut current = &mut self.head;

for level in (0..self.current_level).rev() {
    while let Some(ref next) = current.forward[level] {
        if next.key < search_key {
            current = current.forward[level].as_mut().unwrap();
        } else {
            break;
        }
    }
    update[level] = current as *mut _;
}
```

This uses raw pointers for the update vector. Alternatively, you can use indices or a recursive approach to stay in safe Rust.

</details>

<details>
<summary>Hint 4: Iterator using a stack of references</summary>

For a safe iterator, collect references by walking the bottom level:

```rust
struct SkipListIter<'a, K, V> {
    current: Option<&'a Node<K, V>>,
}

impl<'a, K, V> Iterator for SkipListIter<'a, K, V> {
    type Item = (&'a K, &'a V);

    fn next(&mut self) -> Option<Self::Item> {
        self.current.map(|node| {
            self.current = node.forward[0].as_deref();
            (&node.key, &node.value)
        })
    }
}
```

</details>

## Acceptance Criteria

- [ ] `SkipList<K: Ord, V>` supports insert, get, remove, and contains
- [ ] Level generation is random with configurable probability
- [ ] Skip list invariant is maintained after every operation
- [ ] `Iterator` implementation enables `for (k, v) in &skip_list` syntax
- [ ] `range()` returns only elements within the specified key range
- [ ] `Display` output shows the multi-level structure visually
- [ ] `ConcurrentSkipList` allows multiple concurrent readers via `RwLock`
- [ ] Benchmarks produce comparison data against `BTreeMap`
- [ ] All tests pass with `cargo test`, including edge cases (empty list, single element, duplicates)

## Research Resources

- [Skip Lists: A Probabilistic Alternative to Balanced Trees (Pugh, 1990)](https://15721.courses.cs.cmu.edu/spring2018/papers/08-oltpindexes1/pugh-skiplists-cacm1990.pdf) -- the original paper
- [Skip List visualization](https://people.ok.ubc.ca/ylucet/DS/SkipList.html) -- interactive visualization for building intuition
- [Open Data Structures: Skip Lists](https://opendatastructures.org/ods-java/4_Skiplists.html) -- textbook chapter with pseudocode
- [Rust `rand` crate documentation](https://docs.rs/rand/latest/rand/) -- random number generation
- [Rust `criterion` crate](https://docs.rs/criterion/latest/criterion/) -- benchmarking framework
- [BTreeMap source in Rust std](https://doc.rust-lang.org/src/alloc/collections/btree/map.rs.html) -- the implementation you are benchmarking against
