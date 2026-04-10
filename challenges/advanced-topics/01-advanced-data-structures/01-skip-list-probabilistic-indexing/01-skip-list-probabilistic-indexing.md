<!--
type: reference
difficulty: advanced
section: [01-advanced-data-structures]
concepts: [skip-list, probabilistic-balancing, tower-construction, concurrent-skip-list, range-queries]
languages: [go, rust]
estimated_reading_time: 60 min
bloom_level: analyze
prerequisites: [linked-lists, probability-expected-value, go-sync-primitives, rust-ownership]
papers: [pugh-1990-skip-lists]
industry_use: [redis-zsets, leveldb-memtable, apache-cassandra-memtable]
language_contrast: high
-->

# Skip List — Probabilistic Indexing

> Redis chose skip lists over balanced BSTs for sorted sets because the same O(log n) expected complexity comes with dramatically simpler concurrent modification, in-place range deletion, and no rotation bookkeeping — the randomness does the balancing work for you.

## Mental Model

A skip list is a linked list that got impatient. If you had a sorted linked list and wanted to find an element, you would scan from the head in O(n). Now imagine photocopying every other node onto a second level, every fourth node onto a third level, and so on. Each level is an express lane: you ride the express until you overshoot, then drop down to a slower lane and continue. The key insight is that you do not need to maintain these express lanes deterministically — you can flip a coin for each inserted element to decide how many levels its tower reaches. With probability p (typically 0.5 or 0.25), a node gets promoted to the next level. The expected number of levels is O(log_{1/p} n), and the expected search time is O(log n).

The practical consequence is that skip lists are self-balancing without rotations. A red-black tree requires complex rebalancing after every insertion or deletion; a skip list just inserts a new node at random levels and updates a bounded number of forward pointers. This is why they are dramatically easier to make concurrent: a lock on a single node's forward pointers is sufficient for many operations, rather than the subtree locks that balanced BSTs require.

The second insight is that skip lists are cache-friendly for sequential access. A range scan on a skip list walks the bottom level like a linked list — all nodes in key order, in memory allocated sequentially (if the allocator cooperates). A balanced BST range scan requires an in-order traversal that jumps unpredictably through the heap. For Redis's sorted set range commands (`ZRANGEBYSCORE`, `ZRANGE`), this sequential access pattern at the bottom level is one of the reasons skip lists were chosen over AVL or red-black trees.

## Core Concepts

### Tower Construction and Level Assignment

Each node has a tower of forward pointers, one per level the node participates in. When a node is inserted, its height is chosen by repeating a coin flip (with probability p) until the flip fails — this is a geometric distribution with parameter (1-p). The maximum level is capped at `MaxLevel = ceil(log_{1/p}(n))` for a list with n elements; this bound ensures the expected search time remains O(log n) even if coin flips produce an unusually tall tower.

The forward pointer at level `k` of a node points to the next node that also participates in level `k` or higher. A search proceeds from the highest non-empty level downward: at each level, advance forward pointers as long as the next key is less than the target, then drop down one level and repeat. At level 0 (the base), all nodes participate, so you find the exact position.

### Update Vector and Concurrent Safety

Insertion and deletion both require computing an "update vector" — an array of length MaxLevel holding the last node visited at each level before the insertion point. This is the set of forward pointers that need to be spliced when linking in the new node or unlinking the deleted one. In a single-threaded implementation the update vector is just a local array. In a concurrent implementation, the update vector represents the window of contention: you must hold the locks for those nodes while performing the splice.

A fully lock-free skip list (like the one in `java.util.concurrent.ConcurrentSkipListMap`) uses a multi-phase approach: first logically mark a node for deletion by CAS-ing a mark bit into its forward pointer, then physically unlink it. This prevents another thread from re-linking a being-deleted node during a concurrent insert. The Rust section covers the soundness argument for why this requires careful memory ordering.

### Expected Complexity and Variance

The expected search path visits O(log n) nodes. More precisely, the expected number of nodes examined at level k is bounded by 1/p + 1/(1-p), and summing across ceil(log_{1/p}(n)) levels gives O(log_{1/p} n). The constant factor depends on p: p=0.5 gives O(2 log n), p=0.25 gives O(1.33 log n) but with shorter towers, trading search depth for memory (each node uses fewer forward pointers on average). Redis uses p=0.25 for its ZSET skip list.

The variance is the property you must understand before using skip lists in latency-sensitive systems. The expected case is O(log n), but the worst case is O(n) with probability that decreases exponentially in MaxLevel. For n=10^6 elements with p=0.5 and MaxLevel=32, the probability of a search exceeding 4×log n steps is astronomically small. In practice this tail risk is negligible, but you should verify MaxLevel is set appropriately for your n.

## Implementation: Go

```go
package main

import (
	"fmt"
	"math"
	"math/rand"
	"sync"
)

const (
	maxLevel = 32
	p        = 0.25 // Redis uses 0.25; lower p = shorter towers = less memory per node
)

type node struct {
	key     int
	value   interface{}
	forward []*node // forward[i] is the next node at level i
	mu      sync.Mutex
}

// SkipList is a concurrent skip list using per-node locking.
// The design uses a sentinel head node at level maxLevel-1 so that
// the update vector never has nil entries — this simplifies locking.
type SkipList struct {
	head    *node
	level   int // current highest level with any nodes
	length  int
	mu      sync.RWMutex // protects level and length; per-node mu protects forward pointers
	randSrc *rand.Rand
}

func newNode(key int, value interface{}, level int) *node {
	return &node{
		key:     key,
		value:   value,
		forward: make([]*node, level),
	}
}

func NewSkipList() *SkipList {
	head := newNode(math.MinInt64, nil, maxLevel)
	return &SkipList{
		head:    head,
		level:   1,
		randSrc: rand.New(rand.NewSource(42)),
	}
}

// randomLevel returns a level in [1, maxLevel] using the geometric distribution.
// This is the core of probabilistic balancing: each promotion is an independent coin flip.
func (sl *SkipList) randomLevel() int {
	lvl := 1
	// rand.Float64() returns [0.0, 1.0); the loop continues while the coin comes up heads
	for lvl < maxLevel && sl.randSrc.Float64() < p {
		lvl++
	}
	return lvl
}

// Search returns the value for key and true if found, or nil and false.
// No locks needed for the search path itself because we only read forward pointers,
// but we use RLock to ensure the level field is stable during traversal.
func (sl *SkipList) Search(key int) (interface{}, bool) {
	sl.mu.RLock()
	currentLevel := sl.level
	sl.mu.RUnlock()

	current := sl.head
	for i := currentLevel - 1; i >= 0; i-- {
		// Advance at level i as long as the next node's key is less than target
		for current.forward[i] != nil && current.forward[i].key < key {
			current = current.forward[i]
		}
	}
	// current is now the largest node with key < target; level 0 forward is the candidate
	candidate := current.forward[0]
	if candidate != nil && candidate.key == key {
		return candidate.value, true
	}
	return nil, false
}

// Insert adds or updates key with value.
// The update vector is the set of nodes whose level-i forward pointer must be
// updated when linking in the new node.
func (sl *SkipList) Insert(key int, value interface{}) {
	update := make([]*node, maxLevel)
	current := sl.head

	sl.mu.Lock()
	for i := sl.level - 1; i >= 0; i-- {
		for current.forward[i] != nil && current.forward[i].key < key {
			current = current.forward[i]
		}
		update[i] = current
	}

	candidate := current.forward[0]
	if candidate != nil && candidate.key == key {
		// Key already exists: update value in place, no structural change needed
		candidate.value = value
		sl.mu.Unlock()
		return
	}

	newLevel := sl.randomLevel()
	if newLevel > sl.level {
		// New towers above current max level must start from the head sentinel
		for i := sl.level; i < newLevel; i++ {
			update[i] = sl.head
		}
		sl.level = newLevel
	}

	n := newNode(key, value, newLevel)
	// Splice n into each level: n.forward[i] = update[i].forward[i]; update[i].forward[i] = n
	for i := 0; i < newLevel; i++ {
		n.forward[i] = update[i].forward[i]
		update[i].forward[i] = n
	}
	sl.length++
	sl.mu.Unlock()
}

// Delete removes key. Returns true if the key was present.
func (sl *SkipList) Delete(key int) bool {
	update := make([]*node, maxLevel)
	current := sl.head

	sl.mu.Lock()
	defer sl.mu.Unlock()

	for i := sl.level - 1; i >= 0; i-- {
		for current.forward[i] != nil && current.forward[i].key < key {
			current = current.forward[i]
		}
		update[i] = current
	}

	target := current.forward[0]
	if target == nil || target.key != key {
		return false
	}

	// Unlink target from all levels where it appears
	for i := 0; i < sl.level; i++ {
		if update[i].forward[i] != target {
			break // target does not reach this level
		}
		update[i].forward[i] = target.forward[i]
	}

	// Shrink level if top levels are now empty
	for sl.level > 1 && sl.head.forward[sl.level-1] == nil {
		sl.level--
	}
	sl.length--
	return true
}

// RangeQuery returns all key-value pairs with keys in [lo, hi] in ascending order.
// The skip list's level-0 chain is a sorted linked list, so range scans are O(k + log n)
// where k is the number of results — this is the property Redis exploits for ZRANGEBYSCORE.
func (sl *SkipList) RangeQuery(lo, hi int) [][2]interface{} {
	sl.mu.RLock()
	currentLevel := sl.level
	sl.mu.RUnlock()

	var results [][2]interface{}
	current := sl.head

	// Use the express lanes to reach the first node >= lo
	for i := currentLevel - 1; i >= 0; i-- {
		for current.forward[i] != nil && current.forward[i].key < lo {
			current = current.forward[i]
		}
	}
	current = current.forward[0] // first node with key >= lo

	// Walk the bottom level until we exceed hi
	for current != nil && current.key <= hi {
		results = append(results, [2]interface{}{current.key, current.value})
		current = current.forward[0]
	}
	return results
}

func (sl *SkipList) Len() int {
	sl.mu.RLock()
	defer sl.mu.RUnlock()
	return sl.length
}

func main() {
	sl := NewSkipList()

	// Insert 10 entries; each gets a random tower height
	for i := 0; i < 10; i++ {
		sl.Insert(i*10, fmt.Sprintf("value-%d", i*10))
	}
	fmt.Printf("List length: %d\n", sl.Len())

	// Point lookup
	if v, ok := sl.Search(40); ok {
		fmt.Printf("Search(40) = %v\n", v)
	}

	// Range query: all keys in [25, 65]
	results := sl.RangeQuery(25, 65)
	fmt.Printf("RangeQuery(25, 65): %d results\n", len(results))
	for _, r := range results {
		fmt.Printf("  key=%v value=%v\n", r[0], r[1])
	}

	// Delete a key
	deleted := sl.Delete(40)
	fmt.Printf("Delete(40) = %v, new length = %d\n", deleted, sl.Len())

	// Verify deletion
	_, found := sl.Search(40)
	fmt.Printf("Search(40) after delete: found=%v\n", found)
}
```

### Go-specific considerations

Go's garbage collector makes memory management in skip lists straightforward — deleted nodes are simply unreferenced and collected. This is in sharp contrast to Rust or C++, where you must reason carefully about when a deleted node is safe to free given concurrent readers.

The `sync.RWMutex` protecting `level` and `length` is coarse-grained. The implementation above uses it for the entire Insert/Delete operation to keep the code correct and auditable. A production skip list (like `tidwall/btree` or the one inside `etcd`) uses a striped lock design or fine-grained per-level locking with careful ordering to avoid deadlock. The rule: lock acquisition order must always be from higher levels to lower levels, never the reverse.

Go's `math/rand` is not goroutine-safe; using a `rand.Rand` struct with a local source (as above) is correct. In a highly concurrent skip list, per-goroutine random sources (using `rand.New` per goroutine) eliminate contention on the RNG entirely.

The `interface{}` value type here costs one allocation per inserted value (boxing). In a type-parameterized version using Go generics (`[K comparable, V any]`), the value can be stored inline for small types, eliminating that allocation. For integer values in a ZSET-equivalent, inlining the score avoids a pointer dereference on every comparison.

## Implementation: Rust

```rust
use std::ptr::NonNull;
use std::alloc::{alloc, dealloc, Layout};
use std::fmt;

// Rust implementation of a single-threaded skip list with raw pointers.
// A production concurrent skip list would use crossbeam-epoch for hazard-pointer-style
// reclamation, which is why the concurrent Go version above is more instructive for
// understanding the algorithm — the Rust version here shows ownership challenges.

const MAX_LEVEL: usize = 32;
const P: f64 = 0.25;

struct Node<K, V> {
    key: K,
    value: V,
    // forward is a variable-length array allocated inline after the node struct.
    // We store level here so we can compute the layout for deallocation.
    level: usize,
    // forward[i] is a raw pointer to the next node at level i.
    // Using NonNull instead of *mut to make the semantics explicit: a null forward
    // pointer means "end of list at this level".
    forward: [Option<NonNull<Node<K, V>>>; MAX_LEVEL],
}

impl<K, V> Node<K, V> {
    fn new(key: K, value: V, level: usize) -> NonNull<Node<K, V>> {
        // SAFETY: Layout is valid for Node<K, V>. We check for allocation failure.
        let layout = Layout::new::<Node<K, V>>();
        let ptr = unsafe { alloc(layout) as *mut Node<K, V> };
        assert!(!ptr.is_null(), "allocation failed");
        unsafe {
            (*ptr).level = level;
            std::ptr::write(&mut (*ptr).key, key);
            std::ptr::write(&mut (*ptr).value, value);
            for i in 0..MAX_LEVEL {
                (*ptr).forward[i] = None;
            }
        }
        // SAFETY: we just asserted ptr is non-null
        unsafe { NonNull::new_unchecked(ptr) }
    }
}

pub struct SkipList<K: Ord, V> {
    head: NonNull<Node<K, V>>,
    level: usize,
    length: usize,
    // Xorshift64 for random level generation — no external crate, deterministic for demo
    rng_state: u64,
}

impl<K: Ord + Default, V: Default> SkipList<K, V> {
    pub fn new() -> Self {
        // Head sentinel: key and value are defaults (never observed externally)
        let head = Node::new(K::default(), V::default(), MAX_LEVEL);
        SkipList {
            head,
            level: 1,
            length: 0,
            rng_state: 0xdeadbeefcafe1234,
        }
    }
}

impl<K: Ord, V> SkipList<K, V> {
    // Xorshift64 PRNG — sufficient for randomized level assignment
    fn rand_f64(&mut self) -> f64 {
        let mut x = self.rng_state;
        x ^= x << 13;
        x ^= x >> 7;
        x ^= x << 17;
        self.rng_state = x;
        // Map to [0.0, 1.0)
        (x >> 11) as f64 / (1u64 << 53) as f64
    }

    fn random_level(&mut self) -> usize {
        let mut lvl = 1;
        while lvl < MAX_LEVEL && self.rand_f64() < P {
            lvl += 1;
        }
        lvl
    }

    pub fn search(&self, key: &K) -> Option<&V> {
        // SAFETY: head is always valid; we only follow non-None forward pointers
        let mut current = self.head;
        for i in (0..self.level).rev() {
            unsafe {
                while let Some(next) = current.as_ref().forward[i] {
                    if &next.as_ref().key < key {
                        current = next;
                    } else {
                        break;
                    }
                }
            }
        }
        unsafe {
            let candidate = current.as_ref().forward[0];
            if let Some(node) = candidate {
                if &node.as_ref().key == key {
                    return Some(&node.as_ref().value);
                }
            }
        }
        None
    }

    pub fn insert(&mut self, key: K, value: V) {
        let mut update: [Option<NonNull<Node<K, V>>>; MAX_LEVEL] = [None; MAX_LEVEL];
        let mut current = self.head;

        // Compute the update vector: the last node at each level before the insertion point
        for i in (0..self.level).rev() {
            unsafe {
                while let Some(next) = current.as_ref().forward[i] {
                    if next.as_ref().key < key {
                        current = next;
                    } else {
                        break;
                    }
                }
                update[i] = Some(current);
            }
        }

        // Check if key already exists; update in place
        unsafe {
            if let Some(candidate) = current.as_ref().forward[0] {
                if candidate.as_ref().key == key {
                    // Writing through a raw pointer: safe because we own the list
                    // and no other reference to this node's value can exist while
                    // we hold &mut self.
                    let vptr = &mut (*candidate.as_ptr()).value as *mut V;
                    std::ptr::drop_in_place(vptr);
                    std::ptr::write(vptr, value);
                    return;
                }
            }
        }

        let new_level = self.random_level();
        if new_level > self.level {
            for i in self.level..new_level {
                update[i] = Some(self.head);
            }
            self.level = new_level;
        }

        let new_node = Node::new(key, value, new_level);
        // Splice new_node into each level
        for i in 0..new_level {
            unsafe {
                let upd = update[i].expect("update vector entry must be set");
                (*new_node.as_ptr()).forward[i] = upd.as_ref().forward[i];
                (*upd.as_ptr()).forward[i] = Some(new_node);
            }
        }
        self.length += 1;
    }

    pub fn delete(&mut self, key: &K) -> bool {
        let mut update: [Option<NonNull<Node<K, V>>>; MAX_LEVEL] = [None; MAX_LEVEL];
        let mut current = self.head;

        for i in (0..self.level).rev() {
            unsafe {
                while let Some(next) = current.as_ref().forward[i] {
                    if &next.as_ref().key < key {
                        current = next;
                    } else {
                        break;
                    }
                }
                update[i] = Some(current);
            }
        }

        unsafe {
            let target = current.as_ref().forward[0];
            match target {
                None => return false,
                Some(t) if &t.as_ref().key != key => return false,
                Some(target_node) => {
                    for i in 0..self.level {
                        let upd = update[i].expect("update must be set");
                        if upd.as_ref().forward[i] != Some(target_node) {
                            break; // target not present at this level
                        }
                        (*upd.as_ptr()).forward[i] = target_node.as_ref().forward[i];
                    }
                    // Drop key and value, then deallocate the node
                    std::ptr::drop_in_place(&mut (*target_node.as_ptr()).key as *mut K);
                    std::ptr::drop_in_place(&mut (*target_node.as_ptr()).value as *mut V);
                    dealloc(target_node.as_ptr() as *mut u8, Layout::new::<Node<K, V>>());
                }
            }
        }

        while self.level > 1 {
            unsafe {
                if self.head.as_ref().forward[self.level - 1].is_none() {
                    self.level -= 1;
                } else {
                    break;
                }
            }
        }
        self.length -= 1;
        true
    }

    pub fn len(&self) -> usize {
        self.length
    }
}

impl<K: Ord, V> Drop for SkipList<K, V> {
    fn drop(&mut self) {
        // Walk level 0 to visit every node exactly once
        unsafe {
            let mut current = self.head.as_ref().forward[0];
            while let Some(node) = current {
                let next = node.as_ref().forward[0];
                std::ptr::drop_in_place(&mut (*node.as_ptr()).key as *mut K);
                std::ptr::drop_in_place(&mut (*node.as_ptr()).value as *mut V);
                dealloc(node.as_ptr() as *mut u8, Layout::new::<Node<K, V>>());
                current = next;
            }
            // Drop the sentinel head
            dealloc(self.head.as_ptr() as *mut u8, Layout::new::<Node<K, V>>());
        }
    }
}

fn main() {
    let mut sl: SkipList<i32, String> = SkipList::new();

    for i in 0..10i32 {
        sl.insert(i * 10, format!("value-{}", i * 10));
    }
    println!("Length: {}", sl.len());

    match sl.search(&40) {
        Some(v) => println!("search(40) = {}", v),
        None => println!("search(40): not found"),
    }

    let deleted = sl.delete(&40);
    println!("delete(40) = {}, new length = {}", deleted, sl.len());

    match sl.search(&40) {
        Some(_) => println!("search(40) after delete: found (BUG)"),
        None => println!("search(40) after delete: not found (correct)"),
    }
}
```

### Rust-specific considerations

The core challenge with skip lists in Rust is that the data structure is inherently a graph of mutable aliased pointers — every node is reachable from multiple levels of the skip list simultaneously. The borrow checker cannot verify safety across these aliases statically, which is why `unsafe` is required for the pointer traversals.

The soundness argument is: at any point, `&mut SkipList` provides exclusive access to the entire structure. No reference into the skip list (keys or values) is handed out that would outlive a mutation. The raw pointer arithmetic operates within this exclusive-ownership window. A concurrent skip list would need an entirely different approach — either `Arc<Mutex<Node>>` per node (with high overhead), or epoch-based memory reclamation via `crossbeam-epoch` (which defers deallocation until all readers have exited their critical section).

For a production concurrent implementation in Rust, look at `crossbeam-skiplist` (part of the crossbeam project). It uses `crossbeam-epoch` for memory reclamation and provides a `SkipMap` and `SkipSet` with `Send + Sync` and lock-free reads. The key design difference from this implementation is that logical deletion (marking a node's pointer as logically removed before physically unlinking it) prevents concurrent insertions from re-linking into a being-deleted position.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|----|------|
| Memory management | GC handles reclamation; deleted nodes are collected | Must manually deallocate; concurrent version needs epoch-based reclamation |
| Concurrency model | `sync.RWMutex` on the list + per-node mutexes; straightforward to reason about | `unsafe` with raw pointers; lock-free requires `crossbeam-epoch` |
| Standard library support | No skip list in stdlib; `tidwall/btree` for production use | `crossbeam-skiplist` is the de facto standard |
| Typical performance | ~200-400 ns per operation (locked); GC pauses visible under heavy allocation | ~100-200 ns with `crossbeam-skiplist`; no GC pauses |
| Type safety | `interface{}` values until Go generics; generic version avoids boxing | Fully generic with `K: Ord`; zero-cost abstractions |
| Tower height randomness | `math/rand` per-list source or per-goroutine | Custom PRNG or `rand` crate; no stdlib skip list |

The most significant production difference is memory reclamation under concurrency. In Go, the GC ensures that a goroutine reading a node that another goroutine just deleted will still see valid memory — the node is not freed until the GC confirms no references remain. In Rust, without epoch-based reclamation, a thread that dereferences a deleted node's pointer is undefined behavior. This is not a minor caveat: it is the reason lock-free data structures in Rust require either `crossbeam-epoch` or hazard pointers, while their Go equivalents can get away with simpler lock-based designs backed by the GC.

The performance gap also matters in different ways. Go's locked skip list shows a throughput cliff under heavy concurrent writes due to mutex contention. Rust's `crossbeam-skiplist` remains nearly linear in throughput scaling because lock-free CAS operations allow concurrent progress. If your workload is read-heavy (as Redis ZSETs typically are), Go's RWMutex (allowing concurrent reads) closes most of that gap.

## Production War Stories

**Redis ZSET implementation** (Redis source: `t_zset.c`, `zskiplist`): Antirez (Salvatore Sanfilippo) documented the skip list vs. balanced BST choice in a 2011 comment that has been cited extensively. The decisive factors were: (1) range operations like `ZRANGEBYSCORE` are O(log n + k) where k is results, beating BST in-order traversal for the access pattern Redis sees; (2) in-place rank computation without augmenting the tree; (3) simpler code under concurrent access. Redis's skip list uses a level-0 backward pointer for `ZREVRANGEBYSCORE` — a detail impossible with a standard singly-linked skip list, showing how the original structure was modified for production needs.

**Apache Cassandra memtable** (Cassandra source: `ConcurrentSkipListMap` backed by Java's `java.util.concurrent`): Cassandra's memtable (the in-memory write buffer before flushing to SSTables) uses a concurrent skip list specifically because it supports non-blocking reads concurrent with writes. When a flush thread drains the memtable while write threads continue inserting, the skip list's logical-deletion protocol allows the flush thread to iterate the structure without taking a global lock.

**LevelDB / RocksDB memtable** (RocksDB source: `memtable/skiplist.h`): RocksDB's skip list is used for the same memtable role as Cassandra's. Its implementation adds an important production detail: the arena allocator. Rather than calling `malloc` per node, all nodes are allocated from a contiguous arena. This improves cache locality dramatically (nodes inserted in time order are physically adjacent), eliminates allocator overhead, and allows the entire memtable to be freed in O(1) when it is flushed and replaced.

## Complexity Analysis

| Operation | Expected | Worst Case | Space |
|-----------|----------|------------|-------|
| Search | O(log n) | O(n) | — |
| Insert | O(log n) | O(n) | O(log n) new pointers |
| Delete | O(log n) | O(n) | — |
| Range query [lo, hi] | O(log n + k) | O(n) | O(k) output |
| Space (total) | O(n) | O(n log n) | — |

The average space per node with p=0.25 is 1/(1-0.25) = 1.33 forward pointers. With p=0.5 it is 2.0 pointers per node. In practice, the tower heights are capped at MaxLevel, and for typical n < 10^8, MaxLevel=32 with p=0.25 means the top levels are very sparse. The hidden constant in O(log n) search is 1/(1-p) × log_{1/p}(n) — for p=0.25, approximately 1.78 log_{0.25}(n) = 0.89 log_2(n), which is better than the naive 2 log_2(n) of a p=0.5 list.

Cache behavior: the bottom level is a linked list, so sequential scans have one pointer dereference per node. Higher levels skip over nodes, which means pointer chasing through non-adjacent memory. For random searches, the first few levels (the express lanes) are the most cache-unfriendly because their nodes are spread sparsely through memory.

## Common Pitfalls

**Pitfall 1: Not capping MaxLevel correctly for the expected n**

The `randomLevel` function can theoretically return any height up to MaxLevel. If MaxLevel is too high (say, 64 for n=1000), the sentinel head has 64 forward pointers and the top levels are permanently empty. Search still terminates in O(log n) but wastes iteration over empty levels. The correct formula is `MaxLevel = ceil(log_{1/p}(n_max))`. For p=0.25 and n_max=10^6, MaxLevel=10.7 → 11 suffices; using 32 wastes pointer traversals.

Detection: add a counter to `Search` tracking how many levels are empty at the start. If it is consistently > 5, your MaxLevel is too high for your n.

**Pitfall 2: Non-thread-safe RNG shared across goroutines**

The global `rand.Float64()` in Go is protected by an internal mutex since Go 1.20 (it uses a per-goroutine source via `rand/v2`), but older code and many copy-pasted implementations use a single `*rand.Rand` without synchronization. The result is a data race that manifests as corrupted tower heights — not a crash, just subtly degraded performance as towers become taller than expected because the state machine inside the RNG gets corrupted.

Detection: `go test -race` will catch this if your tests exercise concurrent inserts. Fix: use `rand/v2` (Go 1.22+) or allocate a separate `*rand.Rand` per goroutine.

**Pitfall 3: Incorrect update vector acquisition during concurrent deletion**

In a coarse-locked implementation, the update vector must be computed and the splice must happen atomically under the same lock acquisition. A common mistake is to compute the update vector under an RLock, then upgrade to a write lock for the splice. Between releasing the RLock and acquiring the WLock, another goroutine may insert or delete a node that invalidates entries in the update vector, causing forward pointer corruption.

Detection: a skip list whose output from a range query is non-monotone after concurrent modifications. Fix: acquire the write lock before computing the update vector, not after.

**Pitfall 4: Off-by-one in level iteration (searching from wrong initial level)**

Search must start at `sl.level - 1`, not `MaxLevel - 1`. Starting at MaxLevel always works but wastes time traversing empty levels at the top. Starting at a level higher than `sl.level - 1` is harmless; starting below it (e.g., due to a stale cached level) may miss elements. The bug manifests as elements being "not found" that are present.

Detection: a test that inserts elements, reads `sl.level`, then searches — if search uses a stale level snapshot, it will fail after concurrent inserts that raised `sl.level`.

**Pitfall 5: Forgetting the backward pointer for reverse range queries**

The standard skip list supports `RangeQuery(lo, hi)` efficiently but not `ReverseRange(hi, lo)` — to walk backward, you need either a doubly-linked bottom level (as Redis implements with `backward` pointers) or to collect all results and reverse them (O(k) extra). This is often discovered in production when a new API endpoint requires descending-order results and the implementation takes a full O(n) scan.

## Exercises

**Exercise 1 — Verification** (30 min): Instrument the Go implementation to print the tower height of each inserted node. Insert 1000 elements with p=0.25 and p=0.5. Verify that the empirical distribution of heights follows the geometric distribution: roughly 75%/50% of nodes at level 1, ~18.75%/25% at level 2, ~4.7%/12.5% at level 3, and so on.

**Exercise 2 — Extension** (2-4h): Add a `Rank(key int) int` operation that returns the 1-based rank of a key in sorted order (like Redis's `ZRANK`). Hint: augment each forward pointer with a "width" field indicating how many level-0 steps it spans. During search, accumulate widths to compute rank in O(log n).

**Exercise 3 — From Scratch** (4-8h): Implement a concurrent skip list in Go using fine-grained per-node mutexes instead of a global list mutex. The invariant to maintain: locks must always be acquired in ascending key order to prevent deadlock. Validate with `go test -race` and a stress test running 8 goroutines performing concurrent inserts and deletes.

**Exercise 4 — Production Scenario** (8-15h): Implement a simplified Redis ZSET using your skip list: support `ZADD`, `ZREM`, `ZSCORE`, `ZRANK`, `ZRANGEBYSCORE`, and `ZREVRANGEBYSCORE`. Add a hash map alongside the skip list for O(1) `ZSCORE` lookups (the same dual-structure Redis uses). Benchmark against a sorted slice with binary search at n=100, n=10000, and n=1000000.

## Further Reading

### Foundational Papers
- Pugh, W. (1990). "Skip Lists: A Probabilistic Alternative to Balanced Trees." *Communications of the ACM*, 33(6), 668–676. The original paper; introduces level assignment, expected complexity proof, and comparison with AVL trees.
- Pugh, W. (1990). "A Skip List Cookbook." University of Maryland Technical Report. Implementation guide covering the sentinel node design, deletion, and concurrent variants.

### Books
- Sedgewick, R. & Wayne, K. (2011). *Algorithms* (4th ed.). Chapter 3.3 covers skip lists alongside balanced BSTs. The visual diagrams are the clearest available.
- Herlihy, M. & Shavit, N. (2012). *The Art of Multiprocessor Programming*. Chapter 14 covers lock-free concurrent skip lists with a rigorous linearizability proof.

### Production Code to Read
- `redis/src/t_zset.c` (https://github.com/antirez/redis/blob/unstable/src/t_zset.c) — The zskiplist implementation. Read `zslInsert` and `zslGetRank` to see the width-augmented forward pointers for rank queries.
- `facebook/rocksdb/memtable/skiplist.h` (https://github.com/facebook/rocksdb) — Arena-allocated skip list. The comment at the top of the file explaining the arena design is essential reading.
- `crossbeam-rs/crossbeam/crossbeam-skiplist/src/` (https://github.com/crossbeam-rs/crossbeam) — The Rust production reference. Study `base.rs` for the epoch-reclamation integration.

### Conference Talks
- Sanfilippo, A. (Redis Conf 2014): "Inside Redis: The Data Structures" — covers the ZSET skip list choice with benchmark data.
- Herlihy, M. (PODC 2006): "The Multiprocessor Synchronization Revolution" — broader context for why lock-free structures matter; skip list is the running example.
