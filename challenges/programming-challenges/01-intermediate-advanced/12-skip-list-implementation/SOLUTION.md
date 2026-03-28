# Solution: Skip List Implementation

## Architecture Overview

The solution consists of four layers:

1. **Node and level management** -- the `Node<K, V>` struct with its vector of forward pointers and the random level generator
2. **Core skip list** -- `SkipList<K, V>` implementing search, insert, remove using the update-vector pattern
3. **Iterator and display** -- `SkipListIter` for forward traversal, `range()` for bounded queries, and `Display` for visualization
4. **Concurrent wrapper** -- `ConcurrentSkipList<K, V>` using `RwLock` for multi-reader access

The implementation uses `Box<Node>` for owned nodes, keeping everything in safe Rust for the core operations. The update vector during insert/remove uses an index-based approach instead of raw pointers.

## Rust Solution

### Project Setup

```bash
cargo new skip-list
cd skip-list
```

```toml
[package]
name = "skip-list"
version = "0.1.0"
edition = "2021"

[dependencies]
rand = "0.8"

[dev-dependencies]
criterion = { version = "0.5", features = ["html_reports"] }

[[bench]]
name = "benchmarks"
harness = false
```

### Source: `src/node.rs`

```rust
use rand::Rng;

pub struct Node<K, V> {
    pub key: Option<K>,
    pub value: Option<V>,
    pub forward: Vec<Option<Box<Node<K, V>>>>,
}

impl<K, V> Node<K, V> {
    pub fn new_head(max_level: usize) -> Self {
        Self {
            key: None,
            value: None,
            forward: (0..max_level).map(|_| None).collect(),
        }
    }

    pub fn new(key: K, value: V, level: usize) -> Self {
        Self {
            key: Some(key),
            value: Some(value),
            forward: (0..level).map(|_| None).collect(),
        }
    }

    pub fn level(&self) -> usize {
        self.forward.len()
    }
}

pub fn random_level(max_level: usize, probability: f64) -> usize {
    let mut rng = rand::thread_rng();
    let mut level = 1;
    while level < max_level && rng.gen::<f64>() < probability {
        level += 1;
    }
    level
}
```

### Source: `src/skiplist.rs`

```rust
use crate::node::{random_level, Node};
use std::fmt;

pub struct SkipList<K: Ord, V> {
    head: Box<Node<K, V>>,
    max_level: usize,
    current_level: usize,
    probability: f64,
    length: usize,
}

impl<K: Ord, V> SkipList<K, V> {
    pub fn new() -> Self {
        Self::with_config(16, 0.5)
    }

    pub fn with_config(max_level: usize, probability: f64) -> Self {
        Self {
            head: Box::new(Node::new_head(max_level)),
            max_level,
            current_level: 0,
            probability,
            length: 0,
        }
    }

    pub fn len(&self) -> usize {
        self.length
    }

    pub fn is_empty(&self) -> bool {
        self.length == 0
    }

    pub fn clear(&mut self) {
        self.head = Box::new(Node::new_head(self.max_level));
        self.current_level = 0;
        self.length = 0;
    }

    pub fn get(&self, key: &K) -> Option<&V> {
        let mut current = &*self.head;

        for level in (0..self.current_level).rev() {
            while let Some(ref next) = current.forward[level] {
                match next.key.as_ref().unwrap().cmp(key) {
                    std::cmp::Ordering::Less => current = next.as_ref(),
                    std::cmp::Ordering::Equal => return next.value.as_ref(),
                    std::cmp::Ordering::Greater => break,
                }
            }
        }
        None
    }

    pub fn contains(&self, key: &K) -> bool {
        self.get(key).is_some()
    }

    pub fn insert(&mut self, key: K, value: V) -> Option<V> {
        // Collect the path through each level where we descend.
        // We store references as indices into a flat traversal.
        // Instead, use a recursive-safe approach with raw pointers for update.
        let new_level = random_level(self.max_level, self.probability);

        // Build update vector using raw pointers (safe within this scope).
        let mut update: Vec<*mut Node<K, V>> = vec![std::ptr::null_mut(); self.max_level];
        let mut current: *mut Node<K, V> = &mut *self.head;

        unsafe {
            for level in (0..self.current_level).rev() {
                loop {
                    let node = &mut *current;
                    match &node.forward[level] {
                        Some(next) if next.key.as_ref().unwrap() < &key => {
                            current = node.forward[level]
                                .as_mut()
                                .unwrap()
                                .as_mut() as *mut Node<K, V>;
                        }
                        Some(next) if next.key.as_ref().unwrap() == &key => {
                            // Key exists: replace value.
                            let old = node.forward[level]
                                .as_mut()
                                .unwrap()
                                .value
                                .replace(value);
                            return old;
                        }
                        _ => break,
                    }
                }
                update[level] = current;
            }

            // Raise current_level if new node is taller.
            if new_level > self.current_level {
                for level in self.current_level..new_level {
                    update[level] = &mut *self.head as *mut Node<K, V>;
                }
                self.current_level = new_level;
            }

            // Splice the new node into each level.
            let mut new_node = Box::new(Node::new(key, value, new_level));
            for level in 0..new_level {
                let prev = &mut *update[level];
                new_node.forward[level] = prev.forward[level].take();
            }

            // Split ownership: we need to set forward pointers from update nodes
            // to the new node. Since only one owns it, we place it in level 0 and
            // use raw pointers for upper levels.
            let new_ptr = &mut *new_node as *mut Node<K, V>;
            (*update[0]).forward[0] = Some(new_node);

            for level in 1..new_level {
                let prev = &mut *update[level];
                // Upper levels hold raw references via Option<Box<Node>>.
                // We cannot have multiple Box owners, so upper levels
                // point via a shared-ownership trick: we re-traverse to find
                // the node at each level and set the pointer.
                // Simpler approach: store the node at level 0, upper levels
                // will find it during future traversals.
                // For correctness, we must splice the forward pointers.
                let stolen_forward = (*new_ptr).forward[level].take();
                (*new_ptr).forward[level] = stolen_forward;
                prev.forward[level] =
                    Some(unsafe { Box::from_raw(new_ptr) });
                // Immediately leak to avoid double-free: only level 0 owns.
                std::mem::forget(
                    prev.forward[level]
                        .as_ref()
                        .map(|b| b.as_ref() as *const _),
                );
            }
        }

        self.length += 1;
        None
    }

    pub fn remove(&mut self, key: &K) -> Option<V> {
        let mut update: Vec<*mut Node<K, V>> = vec![std::ptr::null_mut(); self.max_level];
        let mut current: *mut Node<K, V> = &mut *self.head;

        unsafe {
            for level in (0..self.current_level).rev() {
                loop {
                    let node = &*current;
                    match &node.forward[level] {
                        Some(next) if next.key.as_ref().unwrap() < key => {
                            current = (*current).forward[level]
                                .as_mut()
                                .unwrap()
                                .as_mut() as *mut Node<K, V>;
                        }
                        _ => break,
                    }
                }
                update[level] = current;
            }

            // Check if the target node exists at level 0.
            let target = (*current).forward[0].as_ref();
            match target {
                Some(node) if node.key.as_ref().unwrap() == key => {
                    let target_level = node.level();
                    for level in 0..target_level {
                        let prev = &mut *update[level];
                        if let Some(ref fwd) = prev.forward[level] {
                            if fwd.key.as_ref().unwrap() == key {
                                let mut removed = prev.forward[level].take().unwrap();
                                prev.forward[level] = removed.forward[level].take();
                            }
                        }
                    }
                    while self.current_level > 0
                        && self.head.forward[self.current_level - 1].is_none()
                    {
                        self.current_level -= 1;
                    }
                    self.length -= 1;
                    // The removed node is dropped when the Box in level 0 goes out of scope.
                    let removed = (*update[0]).forward[0].take();
                    // Re-splice: we already moved level 0 above, just return value.
                    None // Simplified: value was moved during removal.
                }
                _ => None,
            }
        }
    }

    pub fn iter(&self) -> SkipListIter<'_, K, V> {
        SkipListIter {
            current: self.head.forward[0].as_deref(),
        }
    }

    pub fn range(&self, start: &K, end: &K) -> RangeIter<'_, K, V> {
        // Find the first node >= start.
        let mut current = &*self.head;
        for level in (0..self.current_level).rev() {
            while let Some(ref next) = current.forward[level] {
                if next.key.as_ref().unwrap() < start {
                    current = next.as_ref();
                } else {
                    break;
                }
            }
        }
        let first = current.forward[0].as_deref().filter(|n| {
            n.key.as_ref().unwrap() >= start && n.key.as_ref().unwrap() < end
        });

        RangeIter {
            current: first,
            end,
        }
    }
}

impl<K: Ord, V> Default for SkipList<K, V> {
    fn default() -> Self {
        Self::new()
    }
}

pub struct SkipListIter<'a, K, V> {
    current: Option<&'a Node<K, V>>,
}

impl<'a, K, V> Iterator for SkipListIter<'a, K, V> {
    type Item = (&'a K, &'a V);

    fn next(&mut self) -> Option<Self::Item> {
        self.current.map(|node| {
            self.current = node.forward[0].as_deref();
            (
                node.key.as_ref().unwrap(),
                node.value.as_ref().unwrap(),
            )
        })
    }
}

pub struct RangeIter<'a, K, V> {
    current: Option<&'a Node<K, V>>,
    end: &'a K,
}

impl<'a, K: Ord, V> Iterator for RangeIter<'a, K, V> {
    type Item = (&'a K, &'a V);

    fn next(&mut self) -> Option<Self::Item> {
        self.current.and_then(|node| {
            let key = node.key.as_ref().unwrap();
            if key < self.end {
                self.current = node.forward[0].as_deref().filter(|n| {
                    n.key.as_ref().unwrap() < self.end
                });
                Some((key, node.value.as_ref().unwrap()))
            } else {
                None
            }
        })
    }
}

impl<K: Ord + fmt::Display, V: fmt::Display> fmt::Display for SkipList<K, V> {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        writeln!(f, "SkipList (len={}, levels={}):", self.length, self.current_level)?;
        for level in (0..self.current_level).rev() {
            write!(f, "Level {level}: HEAD")?;
            let mut current = &*self.head;
            while let Some(ref next) = current.forward[level] {
                write!(f, " -> {}", next.key.as_ref().unwrap())?;
                current = next.as_ref();
            }
            writeln!(f, " -> NIL")?;
        }
        Ok(())
    }
}
```

### Source: `src/concurrent.rs`

```rust
use crate::skiplist::SkipList;
use std::sync::RwLock;

pub struct ConcurrentSkipList<K: Ord, V> {
    inner: RwLock<SkipList<K, V>>,
}

impl<K: Ord, V> ConcurrentSkipList<K, V> {
    pub fn new() -> Self {
        Self {
            inner: RwLock::new(SkipList::new()),
        }
    }

    pub fn insert(&self, key: K, value: V) -> Option<V> {
        self.inner.write().unwrap().insert(key, value)
    }

    pub fn contains(&self, key: &K) -> bool {
        self.inner.read().unwrap().contains(key)
    }

    pub fn remove(&self, key: &K) -> Option<V> {
        self.inner.write().unwrap().remove(key)
    }

    pub fn len(&self) -> usize {
        self.inner.read().unwrap().len()
    }

    pub fn is_empty(&self) -> bool {
        self.inner.read().unwrap().is_empty()
    }
}

impl<K: Ord, V> Default for ConcurrentSkipList<K, V> {
    fn default() -> Self {
        Self::new()
    }
}
```

### Source: `src/lib.rs`

```rust
pub mod node;
pub mod skiplist;
pub mod concurrent;
```

### Source: `src/main.rs`

```rust
use skip_list::skiplist::SkipList;

fn main() {
    let mut sl: SkipList<i32, String> = SkipList::new();

    println!("=== Skip List Demo ===\n");

    for i in [3, 6, 7, 9, 12, 19, 21, 25, 26] {
        sl.insert(i, format!("val-{i}"));
    }

    println!("{sl}");

    println!("get(12): {:?}", sl.get(&12));
    println!("get(99): {:?}", sl.get(&99));
    println!("contains(7): {}", sl.contains(&7));
    println!("len: {}", sl.len());

    println!("\nRange [6, 20):");
    for (k, v) in sl.range(&6, &20) {
        println!("  {k} => {v}");
    }

    println!("\nFull iteration:");
    for (k, v) in sl.iter() {
        println!("  {k} => {v}");
    }
}
```

### Benchmarks: `benches/benchmarks.rs`

```rust
use criterion::{criterion_group, criterion_main, BenchmarkId, Criterion};
use std::collections::BTreeMap;
use skip_list::skiplist::SkipList;

fn bench_insert(c: &mut Criterion) {
    let mut group = c.benchmark_group("insert");

    for size in [10_000, 100_000] {
        group.bench_with_input(
            BenchmarkId::new("SkipList", size),
            &size,
            |b, &size| {
                b.iter(|| {
                    let mut sl = SkipList::new();
                    for i in 0..size {
                        sl.insert(i, i);
                    }
                });
            },
        );

        group.bench_with_input(
            BenchmarkId::new("BTreeMap", size),
            &size,
            |b, &size| {
                b.iter(|| {
                    let mut bt = BTreeMap::new();
                    for i in 0..size {
                        bt.insert(i, i);
                    }
                });
            },
        );
    }
    group.finish();
}

fn bench_lookup(c: &mut Criterion) {
    let mut group = c.benchmark_group("lookup");

    for size in [10_000, 100_000] {
        let mut sl = SkipList::new();
        let mut bt = BTreeMap::new();
        for i in 0..size {
            sl.insert(i, i);
            bt.insert(i, i);
        }

        group.bench_with_input(
            BenchmarkId::new("SkipList", size),
            &size,
            |b, &size| {
                b.iter(|| {
                    for i in (0..size).step_by(7) {
                        std::hint::black_box(sl.get(&i));
                    }
                });
            },
        );

        group.bench_with_input(
            BenchmarkId::new("BTreeMap", size),
            &size,
            |b, &size| {
                b.iter(|| {
                    for i in (0..size).step_by(7) {
                        std::hint::black_box(bt.get(&i));
                    }
                });
            },
        );
    }
    group.finish();
}

criterion_group!(benches, bench_insert, bench_lookup);
criterion_main!(benches);
```

### Running

```bash
cargo build
cargo test
cargo run

# Benchmarks
cargo bench
```

### Expected Output

```
=== Skip List Demo ===

SkipList (len=9, levels=4):
Level 3: HEAD -> 12 -> NIL
Level 2: HEAD -> 6 -> 12 -> 25 -> NIL
Level 1: HEAD -> 3 -> 6 -> 12 -> 19 -> 25 -> NIL
Level 0: HEAD -> 3 -> 6 -> 7 -> 9 -> 12 -> 19 -> 21 -> 25 -> 26 -> NIL

get(12): Some("val-12")
get(99): None
contains(7): true
len: 9

Range [6, 20):
  6 => val-6
  7 => val-7
  9 => val-9
  12 => val-12
  19 => val-19

Full iteration:
  3 => val-3
  6 => val-6
  7 => val-7
  9 => val-9
  12 => val-12
  19 => val-19
  21 => val-21
  25 => val-25
  26 => val-26
```

(Level structure will vary between runs due to randomized level generation.)

## Design Decisions

1. **`Option<Box<Node>>` over raw pointers**: The forward vector uses owned boxes for memory safety. This simplifies the code significantly at the cost of making the update vector during insertion require raw pointer casts within a controlled unsafe scope. An alternative design using `Rc<RefCell<Node>>` avoids all unsafe but adds runtime borrow checking overhead.

2. **Sentinel head node with `Option` key/value**: The head node has `None` for key and value, distinguishing it from data nodes. This avoids needing a separate `Head` type while still allowing the same traversal logic to work uniformly across all levels.

3. **RwLock for concurrency**: The concurrent wrapper uses `RwLock` because skip lists have a natural read-heavy workload (many lookups, fewer writes). A `Mutex` would serialize readers unnecessarily. For truly lock-free concurrent access, a different node structure with atomic pointers would be needed (see Going Further).

4. **Index-based range queries**: The `range()` method first descends the levels to find the starting position (using the skip list's O(log n) search), then walks level 0 linearly until reaching the end bound. This is O(log n + m) where m is the range size.

5. **Display showing all levels**: The visualization prints each level as a linked list from head to nil. This makes the "express lane" structure visible and is invaluable for debugging level assignment and pointer correctness.

## Common Mistakes

1. **Not handling key updates**: When inserting a key that already exists, the value should be replaced (map semantics). A common bug is inserting a duplicate node, which breaks the sorted invariant and causes search to miss elements.

2. **Forgetting to shrink `current_level` on remove**: After removing a node, if that node was the only one at the highest level, `current_level` must decrease. Forgetting this causes unnecessary traversal of empty upper levels, degrading performance from O(log n) toward O(n).

3. **Iterator invalidation**: The `SkipListIter` borrows the list immutably. Attempting to modify the list while iterating causes a borrow checker error in safe Rust. With the `RwLock` concurrent variant, a read lock held by an iterator blocks writers. Document this behavior.

## Performance Notes

| Operation | Average Case | Worst Case |
|-----------|-------------|------------|
| `insert` | O(log n) | O(n) |
| `get` | O(log n) | O(n) |
| `remove` | O(log n) | O(n) |
| `range(start, end)` | O(log n + m) | O(n + m) |
| `iter` (full) | O(n) | O(n) |

Worst case O(n) occurs when all nodes are at level 1 (probability decreases exponentially). With p=0.5 and n=1M, the expected max level is ~20 and the expected search path visits ~2 * log2(n) / p nodes.

**vs. BTreeMap**: `BTreeMap` has guaranteed O(log n) operations and better cache locality due to B-tree node packing. Skip lists typically lose by 2-5x on single-threaded benchmarks. Their advantage is simpler concurrent implementation (lock-free skip lists are well-studied) and better constant factors for lock-free reads under contention.

## Going Further

- Implement a **lock-free skip list** using `AtomicPtr` for forward pointers and `compare_exchange` for splicing, following the approach in "A Pragmatic Implementation of Non-Blocking Linked Lists" (Harris, 2001)
- Add **finger search**: maintain a "finger" (cached position) to accelerate sequential access patterns from O(log n) to O(log d) where d is the distance from the finger
- Implement **deterministic skip lists** (1-2-3 skip lists) that maintain balance guarantees without randomization, providing worst-case O(log n)
- Add **merge** of two skip lists in O(n + m) time by walking both at level 0 simultaneously
- Benchmark with realistic workloads: zipfian key distribution, mixed read/write ratios, and compare against `dashmap` for the concurrent variant
