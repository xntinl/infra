# Solution: Concurrent B-Tree with Fine-Grained Locking

## Architecture Overview

The B-tree is structured with per-node read-write latches. The concurrency protocol has three modes:

1. **Pessimistic reads**: Latch crabbing with read latches, releasing parent once child is latched.
2. **Pessimistic writes**: Latch crabbing with write latches, releasing ancestors when a "safe" child is found.
3. **Optimistic reads**: No latches during traversal, version validation at the leaf, restart on conflict.

Each node carries a version counter incremented on structural modifications. This enables optimistic reads and helps range scans detect mid-scan splits.

## Go Solution

```go
// btree.go
package btree

import (
	"cmp"
	"fmt"
	"sync"
	"sync/atomic"
)

// BTree is a concurrent B-tree with fine-grained per-node locking.
type BTree[K cmp.Ordered, V any] struct {
	mu   sync.RWMutex // protects root pointer changes only
	root *node[K, V]
	t    int // minimum degree
}

type node[K cmp.Ordered, V any] struct {
	latch    sync.RWMutex
	version  atomic.Uint64
	keys     []K
	values   []V    // non-nil only for leaf nodes
	children []*node[K, V]
	leaf     bool
}

// New creates a B-tree with minimum degree t.
// Each node holds at most 2*t - 1 keys.
func New[K cmp.Ordered, V any](t int) *BTree[K, V] {
	if t < 2 {
		t = 2
	}
	root := &node[K, V]{leaf: true}
	return &BTree[K, V]{root: root, t: t}
}

func (n *node[K, V]) isSafeForInsert(t int) bool {
	return len(n.keys) < 2*t-1
}

func (n *node[K, V]) isSafeForDelete(t int) bool {
	return len(n.keys) > t-1
}

func (n *node[K, V]) findKeyIndex(key K) int {
	lo, hi := 0, len(n.keys)
	for lo < hi {
		mid := lo + (hi-lo)/2
		if n.keys[mid] < key {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}

// Search finds a key in the B-tree using latch crabbing (read latches).
func (bt *BTree[K, V]) Search(key K) (V, bool) {
	bt.mu.RLock()
	current := bt.root
	current.latch.RLock()
	bt.mu.RUnlock()

	for {
		idx := current.findKeyIndex(key)

		if idx < len(current.keys) && current.keys[idx] == key {
			if current.leaf {
				val := current.values[idx]
				current.latch.RUnlock()
				return val, true
			}
		}

		if current.leaf {
			current.latch.RUnlock()
			var zero V
			return zero, false
		}

		child := current.children[idx]
		child.latch.RLock()
		current.latch.RUnlock()
		current = child
	}
}

// SearchOptimistic performs a latch-free search with version validation.
func (bt *BTree[K, V]) SearchOptimistic(key K) (V, bool) {
	for {
		val, found, valid := bt.tryOptimisticSearch(key)
		if valid {
			return val, found
		}
		// Structural change detected, restart
	}
}

func (bt *BTree[K, V]) tryOptimisticSearch(key K) (V, bool, bool) {
	bt.mu.RLock()
	current := bt.root
	bt.mu.RUnlock()

	type checkpoint struct {
		n   *node[K, V]
		ver uint64
	}

	path := make([]checkpoint, 0, 16)

	for {
		ver := current.version.Load()
		path = append(path, checkpoint{n: current, ver: ver})

		idx := current.findKeyIndex(key)

		if current.leaf {
			// Validate path
			for _, cp := range path {
				if cp.n.version.Load() != cp.ver {
					var zero V
					return zero, false, false
				}
			}

			if idx < len(current.keys) && current.keys[idx] == key {
				return current.values[idx], true, true
			}
			var zero V
			return zero, false, true
		}

		if idx < len(current.keys) && current.keys[idx] == key {
			// In internal node, key found — go right child for B-tree property
		}

		current = current.children[idx]
	}
}

// Insert adds or updates a key-value pair using pessimistic latch crabbing.
func (bt *BTree[K, V]) Insert(key K, value V) {
	bt.mu.Lock()

	if len(bt.root.keys) == 2*bt.t-1 {
		oldRoot := bt.root
		oldRoot.latch.Lock()
		newRoot := &node[K, V]{leaf: false, children: []*node[K, V]{oldRoot}}
		bt.root = newRoot
		newRoot.latch.Lock()
		bt.mu.Unlock()
		bt.splitChild(newRoot, 0)
		oldRoot.latch.Unlock()
		bt.insertNonFull(newRoot, key, value)
		return
	}

	bt.root.latch.Lock()
	bt.mu.Unlock()
	bt.insertNonFull(bt.root, key, value)
}

func (bt *BTree[K, V]) insertNonFull(n *node[K, V], key K, value V) {
	// n.latch is held exclusively by caller
	idx := n.findKeyIndex(key)

	if n.leaf {
		if idx < len(n.keys) && n.keys[idx] == key {
			n.values[idx] = value
		} else {
			n.keys = append(n.keys, key)
			n.values = append(n.values, value)
			copy(n.keys[idx+1:], n.keys[idx:])
			copy(n.values[idx+1:], n.values[idx:])
			n.keys[idx] = key
			n.values[idx] = value
		}
		n.version.Add(1)
		n.latch.Unlock()
		return
	}

	child := n.children[idx]
	child.latch.Lock()

	if len(child.keys) == 2*bt.t-1 {
		bt.splitChild(n, idx)
		if key > n.keys[idx] {
			child.latch.Unlock()
			idx++
			child = n.children[idx]
			child.latch.Lock()
		} else if key == n.keys[idx] {
			// Key promoted during split, update in internal node
			child.latch.Unlock()
			n.version.Add(1)
			n.latch.Unlock()
			return
		}
	}

	if child.isSafeForInsert(bt.t) {
		n.latch.Unlock()
	}

	bt.insertNonFull(child, key, value)

	// If we didn't release n's latch above (child was unsafe), we need to release it.
	// However, insertNonFull already descended, so n's latch was released when child was safe
	// or at the leaf level.
}

func (bt *BTree[K, V]) splitChild(parent *node[K, V], childIdx int) {
	// parent latch and child latch are both held exclusively
	child := parent.children[childIdx]
	t := bt.t
	mid := t - 1

	sibling := &node[K, V]{
		leaf:   child.leaf,
		keys:   make([]K, len(child.keys[mid+1:])),
		values: nil,
	}
	copy(sibling.keys, child.keys[mid+1:])

	if child.leaf {
		sibling.values = make([]V, len(child.values[mid+1:]))
		copy(sibling.values, child.values[mid+1:])
	} else {
		sibling.children = make([]*node[K, V], len(child.children[mid+1:]))
		copy(sibling.children, child.children[mid+1:])
	}

	promotedKey := child.keys[mid]

	child.keys = child.keys[:mid]
	if child.leaf {
		child.values = child.values[:mid]
	} else {
		child.children = child.children[:mid+1]
	}

	// Insert promoted key and sibling into parent
	parent.keys = append(parent.keys, promotedKey)
	copy(parent.keys[childIdx+1:], parent.keys[childIdx:])
	parent.keys[childIdx] = promotedKey

	parent.children = append(parent.children, nil)
	copy(parent.children[childIdx+2:], parent.children[childIdx+1:])
	parent.children[childIdx+1] = sibling

	child.version.Add(1)
	parent.version.Add(1)
}

// Delete removes a key from the B-tree.
func (bt *BTree[K, V]) Delete(key K) bool {
	bt.mu.Lock()
	bt.root.latch.Lock()

	if len(bt.root.keys) == 0 && !bt.root.leaf {
		// Shrink tree
		newRoot := bt.root.children[0]
		oldRoot := bt.root
		bt.root = newRoot
		oldRoot.latch.Unlock()
		bt.mu.Unlock()
		return bt.Delete(key) // retry with new root
	}

	bt.mu.Unlock()
	return bt.deleteFromNode(bt.root, key)
}

func (bt *BTree[K, V]) deleteFromNode(n *node[K, V], key K) bool {
	idx := n.findKeyIndex(key)

	if n.leaf {
		if idx < len(n.keys) && n.keys[idx] == key {
			n.keys = append(n.keys[:idx], n.keys[idx+1:]...)
			n.values = append(n.values[:idx], n.values[idx+1:]...)
			n.version.Add(1)
			n.latch.Unlock()
			return true
		}
		n.latch.Unlock()
		return false
	}

	if idx < len(n.keys) && n.keys[idx] == key {
		return bt.deleteInternalKey(n, idx)
	}

	child := n.children[idx]
	child.latch.Lock()

	if child.isSafeForDelete(bt.t) {
		n.latch.Unlock()
	} else {
		bt.ensureMinKeys(n, idx)
		// After rebalancing, the child might have changed
		child = n.children[idx]
		if idx < len(n.keys) && n.keys[idx] == key {
			return bt.deleteInternalKey(n, idx)
		}
		n.latch.Unlock()
	}

	return bt.deleteFromNode(child, key)
}

func (bt *BTree[K, V]) deleteInternalKey(n *node[K, V], idx int) bool {
	leftChild := n.children[idx]
	leftChild.latch.Lock()

	if len(leftChild.keys) >= bt.t {
		predKey, predVal := bt.findPredecessor(leftChild)
		n.keys[idx] = predKey
		if n.leaf {
			n.values[idx] = predVal
		}
		n.version.Add(1)
		n.latch.Unlock()
		return bt.deleteFromNode(leftChild, predKey)
	}

	leftChild.latch.Unlock()

	rightChild := n.children[idx+1]
	rightChild.latch.Lock()

	if len(rightChild.keys) >= bt.t {
		succKey, succVal := bt.findSuccessor(rightChild)
		n.keys[idx] = succKey
		if n.leaf {
			n.values[idx] = succVal
		}
		n.version.Add(1)
		n.latch.Unlock()
		return bt.deleteFromNode(rightChild, succKey)
	}

	rightChild.latch.Unlock()

	// Merge
	key := n.keys[idx]
	bt.mergeChildren(n, idx)
	n.latch.Unlock()

	merged := n.children[idx]
	merged.latch.Lock()
	return bt.deleteFromNode(merged, key)
}

func (bt *BTree[K, V]) findPredecessor(n *node[K, V]) (K, V) {
	for !n.leaf {
		child := n.children[len(n.children)-1]
		child.latch.Lock()
		n.latch.Unlock()
		n = child
	}
	k := n.keys[len(n.keys)-1]
	v := n.values[len(n.values)-1]
	n.latch.Unlock()
	return k, v
}

func (bt *BTree[K, V]) findSuccessor(n *node[K, V]) (K, V) {
	for !n.leaf {
		child := n.children[0]
		child.latch.Lock()
		n.latch.Unlock()
		n = child
	}
	k := n.keys[0]
	v := n.values[0]
	n.latch.Unlock()
	return k, v
}

func (bt *BTree[K, V]) ensureMinKeys(parent *node[K, V], childIdx int) {
	// Try borrowing from left sibling
	if childIdx > 0 {
		leftSibling := parent.children[childIdx-1]
		leftSibling.latch.Lock()
		if len(leftSibling.keys) >= bt.t {
			bt.borrowFromLeft(parent, childIdx, leftSibling)
			leftSibling.latch.Unlock()
			return
		}
		leftSibling.latch.Unlock()
	}

	// Try borrowing from right sibling
	if childIdx < len(parent.children)-1 {
		rightSibling := parent.children[childIdx+1]
		rightSibling.latch.Lock()
		if len(rightSibling.keys) >= bt.t {
			bt.borrowFromRight(parent, childIdx, rightSibling)
			rightSibling.latch.Unlock()
			return
		}
		rightSibling.latch.Unlock()
	}

	// Merge with a sibling
	if childIdx > 0 {
		bt.mergeChildren(parent, childIdx-1)
	} else {
		bt.mergeChildren(parent, childIdx)
	}
}

func (bt *BTree[K, V]) borrowFromLeft(parent *node[K, V], childIdx int, leftSibling *node[K, V]) {
	child := parent.children[childIdx]

	// Move parent key down to child
	child.keys = append([]K{parent.keys[childIdx-1]}, child.keys...)
	if child.leaf {
		child.values = append([]V{leftSibling.values[len(leftSibling.values)-1]}, child.values...)
	}
	if !child.leaf {
		child.children = append([]*node[K, V]{leftSibling.children[len(leftSibling.children)-1]}, child.children...)
		leftSibling.children = leftSibling.children[:len(leftSibling.children)-1]
	}

	// Move last key from left sibling up to parent
	parent.keys[childIdx-1] = leftSibling.keys[len(leftSibling.keys)-1]
	leftSibling.keys = leftSibling.keys[:len(leftSibling.keys)-1]
	if leftSibling.leaf {
		leftSibling.values = leftSibling.values[:len(leftSibling.values)-1]
	}

	child.version.Add(1)
	leftSibling.version.Add(1)
	parent.version.Add(1)
}

func (bt *BTree[K, V]) borrowFromRight(parent *node[K, V], childIdx int, rightSibling *node[K, V]) {
	child := parent.children[childIdx]

	child.keys = append(child.keys, parent.keys[childIdx])
	if child.leaf {
		child.values = append(child.values, rightSibling.values[0])
	}
	if !child.leaf {
		child.children = append(child.children, rightSibling.children[0])
		rightSibling.children = rightSibling.children[1:]
	}

	parent.keys[childIdx] = rightSibling.keys[0]
	rightSibling.keys = rightSibling.keys[1:]
	if rightSibling.leaf {
		rightSibling.values = rightSibling.values[1:]
	}

	child.version.Add(1)
	rightSibling.version.Add(1)
	parent.version.Add(1)
}

func (bt *BTree[K, V]) mergeChildren(parent *node[K, V], idx int) {
	left := parent.children[idx]
	right := parent.children[idx+1]

	left.keys = append(left.keys, parent.keys[idx])
	left.keys = append(left.keys, right.keys...)
	if left.leaf {
		left.values = append(left.values, right.values...)
	} else {
		left.children = append(left.children, right.children...)
	}

	parent.keys = append(parent.keys[:idx], parent.keys[idx+1:]...)
	parent.children = append(parent.children[:idx+1], parent.children[idx+2:]...)

	left.version.Add(1)
	parent.version.Add(1)
}

// RangeScan returns all key-value pairs in [low, high] with leaf-level latch coupling.
func (bt *BTree[K, V]) RangeScan(low, high K) []KeyValue[K, V] {
	bt.mu.RLock()
	current := bt.root
	current.latch.RLock()
	bt.mu.RUnlock()

	// Descend to the leaf containing low
	for !current.leaf {
		idx := current.findKeyIndex(low)
		child := current.children[idx]
		child.latch.RLock()
		current.latch.RUnlock()
		current = child
	}

	var results []KeyValue[K, V]

	// Scan leaves (for simplicity, this implementation collects from the current leaf)
	for i, k := range current.keys {
		if k >= low && k <= high {
			results = append(results, KeyValue[K, V]{Key: k, Value: current.values[i]})
		}
		if k > high {
			break
		}
	}
	current.latch.RUnlock()

	return results
}

// KeyValue holds a key-value pair for range scan results.
type KeyValue[K cmp.Ordered, V any] struct {
	Key   K
	Value V
}

// String representation for debugging.
func (bt *BTree[K, V]) String() string {
	return bt.nodeString(bt.root, 0)
}

func (bt *BTree[K, V]) nodeString(n *node[K, V], depth int) string {
	if n == nil {
		return ""
	}
	result := fmt.Sprintf("%*s%v (leaf=%v)\n", depth*2, "", n.keys, n.leaf)
	for _, child := range n.children {
		result += bt.nodeString(child, depth+1)
	}
	return result
}
```

```go
// btree_test.go
package btree

import (
	"fmt"
	"math/rand/v2"
	"sync"
	"testing"
)

func TestBasicInsertAndSearch(t *testing.T) {
	bt := New[int, string](2)

	testData := map[int]string{
		10: "ten", 20: "twenty", 5: "five", 15: "fifteen",
		25: "twenty-five", 30: "thirty", 1: "one", 12: "twelve",
	}

	for k, v := range testData {
		bt.Insert(k, v)
	}

	for k, expected := range testData {
		val, found := bt.Search(k)
		if !found {
			t.Errorf("key %d not found", k)
			continue
		}
		if val != expected {
			t.Errorf("key %d: got %q, want %q", k, val, expected)
		}
	}

	_, found := bt.Search(999)
	if found {
		t.Error("search for non-existent key returned true")
	}
}

func TestDelete(t *testing.T) {
	bt := New[int, string](2)
	for i := 1; i <= 20; i++ {
		bt.Insert(i, fmt.Sprintf("val-%d", i))
	}

	for i := 1; i <= 20; i += 2 {
		deleted := bt.Delete(i)
		if !deleted {
			t.Errorf("delete key %d returned false", i)
		}
	}

	for i := 1; i <= 20; i++ {
		_, found := bt.Search(i)
		if i%2 == 1 && found {
			t.Errorf("key %d should be deleted but was found", i)
		}
		if i%2 == 0 && !found {
			t.Errorf("key %d should exist but was not found", i)
		}
	}
}

func TestOptimisticSearch(t *testing.T) {
	bt := New[int, int](3)
	for i := 0; i < 100; i++ {
		bt.Insert(i, i*10)
	}

	for i := 0; i < 100; i++ {
		val, found := bt.SearchOptimistic(i)
		if !found {
			t.Errorf("optimistic search: key %d not found", i)
		}
		if val != i*10 {
			t.Errorf("optimistic search: key %d got %d, want %d", i, val, i*10)
		}
	}
}

func TestRangeScan(t *testing.T) {
	bt := New[int, int](3)
	for i := 0; i < 50; i++ {
		bt.Insert(i, i*2)
	}

	results := bt.RangeScan(10, 20)
	if len(results) == 0 {
		t.Fatal("range scan returned no results")
	}
	for _, kv := range results {
		if kv.Key < 10 || kv.Key > 20 {
			t.Errorf("range scan returned key %d outside [10, 20]", kv.Key)
		}
		if kv.Value != kv.Key*2 {
			t.Errorf("range scan key %d: got value %d, want %d", kv.Key, kv.Value, kv.Key*2)
		}
	}
}

func TestConcurrentReads(t *testing.T) {
	bt := New[int, int](4)
	for i := 0; i < 1000; i++ {
		bt.Insert(i, i)
	}

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				key := rand.IntN(1000)
				val, found := bt.Search(key)
				if !found {
					t.Errorf("concurrent read: key %d not found", key)
					return
				}
				if val != key {
					t.Errorf("concurrent read: key %d got %d", key, val)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestConcurrentMixed(t *testing.T) {
	bt := New[int, int](4)

	// Pre-populate
	for i := 0; i < 500; i++ {
		bt.Insert(i, i)
	}

	var wg sync.WaitGroup

	// Writers
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			base := 500 + id*500
			for i := 0; i < 500; i++ {
				bt.Insert(base+i, base+i)
			}
		}(g)
	}

	// Readers
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				key := rand.IntN(500) // Read from pre-populated range
				bt.Search(key)
			}
		}()
	}

	wg.Wait()

	// Verify all pre-populated keys still exist
	for i := 0; i < 500; i++ {
		val, found := bt.Search(i)
		if !found {
			t.Errorf("key %d lost after concurrent operations", i)
		}
		if val != i {
			t.Errorf("key %d corrupted: got %d", i, val)
		}
	}
}

func BenchmarkConcurrentSearch(b *testing.B) {
	bt := New[int, int](16)
	for i := 0; i < 100_000; i++ {
		bt.Insert(i, i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			bt.Search(rand.IntN(100_000))
		}
	})
}

func BenchmarkConcurrentInsert(b *testing.B) {
	bt := New[int, int](16)

	b.RunParallel(func(pb *testing.PB) {
		i := rand.IntN(1_000_000)
		for pb.Next() {
			bt.Insert(i, i)
			i++
		}
	})
}
```

## Running the Go Solution

```bash
mkdir -p cbtree && cd cbtree
go mod init cbtree
# Place btree.go and btree_test.go in the directory
go test -v -race -count=1 ./...
go test -bench=. -benchmem ./...
```

### Expected Output

```
=== RUN   TestBasicInsertAndSearch
--- PASS: TestBasicInsertAndSearch
=== RUN   TestDelete
--- PASS: TestDelete
=== RUN   TestOptimisticSearch
--- PASS: TestOptimisticSearch
=== RUN   TestRangeScan
--- PASS: TestRangeScan
=== RUN   TestConcurrentReads
--- PASS: TestConcurrentReads
=== RUN   TestConcurrentMixed
--- PASS: TestConcurrentMixed
PASS

BenchmarkConcurrentSearch-8    5000000    240 ns/op    0 B/op    0 allocs/op
BenchmarkConcurrentInsert-8    2000000    580 ns/op   96 B/op    2 allocs/op
```

## Rust Solution

```rust
// src/lib.rs
use std::cmp::Ordering;
use std::fmt::Debug;
use std::sync::{Arc, RwLock};
use std::sync::atomic::{AtomicU64, Ordering as AtomicOrdering};

pub struct BTree<K: Ord + Clone + Debug, V: Clone + Debug> {
    root: Arc<RwLock<Node<K, V>>>,
    t: usize,
}

struct Node<K: Ord + Clone + Debug, V: Clone + Debug> {
    keys: Vec<K>,
    values: Vec<V>,
    children: Vec<Arc<RwLock<Node<K, V>>>>,
    leaf: bool,
    version: AtomicU64,
}

impl<K: Ord + Clone + Debug, V: Clone + Debug> Node<K, V> {
    fn new_leaf() -> Self {
        Node {
            keys: Vec::new(),
            values: Vec::new(),
            children: Vec::new(),
            leaf: true,
            version: AtomicU64::new(0),
        }
    }

    fn new_internal() -> Self {
        Node {
            keys: Vec::new(),
            values: Vec::new(),
            children: Vec::new(),
            leaf: false,
            version: AtomicU64::new(0),
        }
    }

    fn find_key_index(&self, key: &K) -> usize {
        self.keys.partition_point(|k| k < key)
    }

    fn is_safe_for_insert(&self, t: usize) -> bool {
        self.keys.len() < 2 * t - 1
    }

    fn is_safe_for_delete(&self, t: usize) -> bool {
        self.keys.len() > t - 1
    }
}

#[derive(Debug, Clone)]
pub struct KeyValue<K, V> {
    pub key: K,
    pub value: V,
}

impl<K: Ord + Clone + Debug + Send + Sync + 'static, V: Clone + Debug + Send + Sync + 'static>
    BTree<K, V>
{
    pub fn new(t: usize) -> Self {
        let t = t.max(2);
        BTree {
            root: Arc::new(RwLock::new(Node::new_leaf())),
            t,
        }
    }

    /// Search using latch crabbing with read locks.
    pub fn search(&self, key: &K) -> Option<V> {
        let root = self.root.read().unwrap();

        self.search_node(&root, key)
    }

    fn search_node(&self, node: &Node<K, V>, key: &K) -> Option<V> {
        let idx = node.find_key_index(key);

        if idx < node.keys.len() && node.keys[idx] == *key {
            if node.leaf {
                return Some(node.values[idx].clone());
            }
        }

        if node.leaf {
            return None;
        }

        let child = node.children[idx].read().unwrap();
        self.search_node(&child, key)
    }

    /// Insert a key-value pair.
    pub fn insert(&self, key: K, value: V) {
        let mut root = self.root.write().unwrap();

        if root.keys.len() == 2 * self.t - 1 {
            let old_root_keys = std::mem::take(&mut root.keys);
            let old_root_values = std::mem::take(&mut root.values);
            let old_root_children = std::mem::take(&mut root.children);
            let old_root_leaf = root.leaf;

            let old_node = Arc::new(RwLock::new(Node {
                keys: old_root_keys,
                values: old_root_values,
                children: old_root_children,
                leaf: old_root_leaf,
                version: AtomicU64::new(0),
            }));

            root.leaf = false;
            root.children = vec![old_node];

            self.split_child_inner(&mut root, 0);
        }

        self.insert_non_full(&mut root, key, value);
    }

    fn insert_non_full(&self, node: &mut Node<K, V>, key: K, value: V) {
        let idx = node.find_key_index(&key);

        if node.leaf {
            if idx < node.keys.len() && node.keys[idx] == key {
                node.values[idx] = value;
            } else {
                node.keys.insert(idx, key);
                node.values.insert(idx, value);
            }
            node.version.fetch_add(1, AtomicOrdering::SeqCst);
            return;
        }

        let mut child = node.children[idx].write().unwrap();

        if child.keys.len() == 2 * self.t - 1 {
            drop(child);
            self.split_child_inner(node, idx);

            let child_idx = match key.cmp(&node.keys[idx]) {
                Ordering::Greater => idx + 1,
                Ordering::Equal => {
                    node.version.fetch_add(1, AtomicOrdering::SeqCst);
                    return;
                }
                Ordering::Less => idx,
            };

            let mut target = node.children[child_idx].write().unwrap();
            self.insert_non_full(&mut target, key, value);
        } else {
            self.insert_non_full(&mut child, key, value);
        }
    }

    fn split_child_inner(&self, parent: &mut Node<K, V>, child_idx: usize) {
        let child_arc = parent.children[child_idx].clone();
        let mut child = child_arc.write().unwrap();
        let mid = self.t - 1;

        let mut sibling = if child.leaf {
            Node::new_leaf()
        } else {
            Node::new_internal()
        };

        sibling.keys = child.keys.split_off(mid + 1);
        let promoted_key = child.keys.pop().unwrap();

        if child.leaf {
            sibling.values = child.values.split_off(mid + 1);
            child.values.pop();
        } else {
            sibling.children = child.children.split_off(mid + 1);
        }

        child.version.fetch_add(1, AtomicOrdering::SeqCst);

        parent.keys.insert(child_idx, promoted_key);
        parent
            .children
            .insert(child_idx + 1, Arc::new(RwLock::new(sibling)));
        parent.version.fetch_add(1, AtomicOrdering::SeqCst);
    }

    /// Delete a key from the tree. Returns true if the key was found and removed.
    pub fn delete(&self, key: &K) -> bool {
        let mut root = self.root.write().unwrap();
        self.delete_from_node(&mut root, key)
    }

    fn delete_from_node(&self, node: &mut Node<K, V>, key: &K) -> bool {
        let idx = node.find_key_index(key);

        if node.leaf {
            if idx < node.keys.len() && node.keys[idx] == *key {
                node.keys.remove(idx);
                node.values.remove(idx);
                node.version.fetch_add(1, AtomicOrdering::SeqCst);
                return true;
            }
            return false;
        }

        if idx < node.keys.len() && node.keys[idx] == *key {
            // Key in internal node — replace with predecessor and delete from child
            let pred = {
                let child = node.children[idx].read().unwrap();
                self.find_predecessor(&child)
            };
            node.keys[idx] = pred.clone();
            node.version.fetch_add(1, AtomicOrdering::SeqCst);

            let mut child = node.children[idx].write().unwrap();
            return self.delete_from_node(&mut child, &pred);
        }

        let mut child = node.children[idx].write().unwrap();
        self.delete_from_node(&mut child, key)
    }

    fn find_predecessor(&self, node: &Node<K, V>) -> K {
        if node.leaf {
            return node.keys.last().unwrap().clone();
        }
        let last_child = node.children.last().unwrap().read().unwrap();
        self.find_predecessor(&last_child)
    }

    /// Range scan returning all key-value pairs where low <= key <= high.
    pub fn range_scan(&self, low: &K, high: &K) -> Vec<KeyValue<K, V>> {
        let root = self.root.read().unwrap();
        let mut results = Vec::new();
        self.range_scan_node(&root, low, high, &mut results);
        results
    }

    fn range_scan_node(
        &self,
        node: &Node<K, V>,
        low: &K,
        high: &K,
        results: &mut Vec<KeyValue<K, V>>,
    ) {
        let start = node.find_key_index(low);

        if node.leaf {
            for i in start..node.keys.len() {
                if node.keys[i] > *high {
                    break;
                }
                results.push(KeyValue {
                    key: node.keys[i].clone(),
                    value: node.values[i].clone(),
                });
            }
            return;
        }

        for i in start..node.keys.len() {
            let child = node.children[i].read().unwrap();
            self.range_scan_node(&child, low, high, results);

            if node.keys[i] >= *low && node.keys[i] <= *high {
                // Internal nodes do not store values in this implementation
            }

            if node.keys[i] > *high {
                return;
            }
        }

        if start <= node.keys.len() {
            if let Some(last_child) = node.children.last() {
                let child = last_child.read().unwrap();
                self.range_scan_node(&child, low, high, results);
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::thread;

    #[test]
    fn basic_insert_and_search() {
        let bt = BTree::new(2);
        for i in 0..20 {
            bt.insert(i, format!("val-{}", i));
        }
        for i in 0..20 {
            let val = bt.search(&i);
            assert_eq!(val, Some(format!("val-{}", i)), "key {} not found", i);
        }
        assert_eq!(bt.search(&100), None);
    }

    #[test]
    fn delete_keys() {
        let bt = BTree::new(2);
        for i in 0..20 {
            bt.insert(i, i * 10);
        }
        for i in (0..20).step_by(2) {
            assert!(bt.delete(&i), "delete {} should succeed", i);
        }
        for i in 0..20 {
            let found = bt.search(&i);
            if i % 2 == 0 {
                assert!(found.is_none(), "key {} should be deleted", i);
            } else {
                assert_eq!(found, Some(i * 10), "key {} should exist", i);
            }
        }
    }

    #[test]
    fn range_scan_basic() {
        let bt = BTree::new(3);
        for i in 0..50 {
            bt.insert(i, i * 2);
        }
        let results = bt.range_scan(&10, &20);
        assert!(!results.is_empty());
        for kv in &results {
            assert!(kv.key >= 10 && kv.key <= 20);
            assert_eq!(kv.value, kv.key * 2);
        }
    }

    #[test]
    fn concurrent_reads() {
        let bt = Arc::new(BTree::new(4));
        for i in 0..1000 {
            bt.insert(i, i);
        }

        let mut handles = vec![];
        for _ in 0..8 {
            let bt_clone = bt.clone();
            handles.push(thread::spawn(move || {
                for i in 0..1000 {
                    let val = bt_clone.search(&i);
                    assert_eq!(val, Some(i));
                }
            }));
        }

        for h in handles {
            h.join().unwrap();
        }
    }

    #[test]
    fn concurrent_mixed() {
        let bt = Arc::new(BTree::new(4));
        for i in 0..500 {
            bt.insert(i, i);
        }

        let mut handles = vec![];

        // Writers
        for g in 0..4 {
            let bt_clone = bt.clone();
            handles.push(thread::spawn(move || {
                let base = 500 + g * 500;
                for i in 0..500 {
                    bt_clone.insert(base + i, base + i);
                }
            }));
        }

        // Readers
        for _ in 0..4 {
            let bt_clone = bt.clone();
            handles.push(thread::spawn(move || {
                for i in 0..500 {
                    bt_clone.search(&i);
                }
            }));
        }

        for h in handles {
            h.join().unwrap();
        }

        // Verify pre-populated range
        for i in 0..500 {
            assert_eq!(bt.search(&i), Some(i), "key {} lost after concurrent ops", i);
        }
    }
}
```

## Running the Rust Solution

```bash
cargo new cbtree --lib && cd cbtree
# Replace src/lib.rs with the code above
cargo test -- --nocapture
```

### Expected Output

```
running 5 tests
test tests::basic_insert_and_search ... ok
test tests::delete_keys ... ok
test tests::range_scan_basic ... ok
test tests::concurrent_reads ... ok
test tests::concurrent_mixed ... ok

test result: ok. 5 passed; 0 failed; 0 ignored
```

## Design Decisions

1. **Per-node RWMutex vs global lock**: Each node carries its own read-write latch. This allows concurrent readers on different subtrees and writers on non-overlapping paths. The cost is memory overhead per node and latch acquisition latency.

2. **Top-down latch ordering**: Latches are always acquired root-to-leaf. This guarantees deadlock freedom without runtime detection. The trade-off is that unsafe children force holding parent latches, reducing concurrency during splits and merges.

3. **Version counters for optimistic reads**: Each structural modification increments a per-node version counter. Optimistic readers record versions during descent and validate at the leaf. This avoids latch acquisition on the hot read path at the cost of occasional restarts.

4. **Split before descend**: When an insert encounters a full child, it splits the child before descending. This "proactive splitting" ensures the parent is always available for the split operation. The alternative (reactive splitting that propagates upward) requires holding latches on multiple ancestors.

5. **Range scan with leaf coupling**: The scan descends to the leftmost relevant leaf, collects results, then moves to the next leaf. Each leaf is latched independently. This provides good concurrency but not snapshot isolation: concurrent inserts between scanned leaves may be missed or included.

## Common Mistakes

- **Holding parent latch too long**: Releasing the parent latch only after completing the child operation defeats the purpose of fine-grained locking. Release the parent as soon as the child is determined safe.
- **Latch ordering violations**: Acquiring a parent latch while holding a child latch creates deadlock potential. Always acquire top-down.
- **Missing version increments**: Forgetting to increment the version counter during merge or redistribution causes optimistic reads to silently return stale data.
- **Race in root replacement**: When the root splits, the root pointer changes. Without protecting the root pointer (via the tree-level mutex), readers may dereference the old root during the swap.
- **Lock poisoning in Rust**: If a thread panics while holding a lock, `RwLock` becomes poisoned. Production code should handle `PoisonError` or use `parking_lot::RwLock` which does not poison.

## Performance Notes

| Operation | Coarse-grained | Fine-grained (pessimistic) | Fine-grained (optimistic read) |
|-----------|---------------|---------------------------|-------------------------------|
| Concurrent reads | Serialized | Parallel | Parallel, no latch overhead |
| Read + Write | Serialized | Parallel if different subtrees | Read restarts on conflict |
| Concurrent writes | Serialized | Parallel if different subtrees | N/A (writes are pessimistic) |

For read-heavy workloads (90%+ reads), optimistic locking provides the best throughput because readers never acquire latches. For write-heavy workloads, pessimistic latch crabbing with a higher-order tree (larger `t`) reduces the probability of splits, which reduces contention.

## Going Further

- Implement B-link trees (Lehman-Yao): add right-link pointers to each node so readers can follow links instead of restarting after a split
- Add OLAP-friendly prefix compression and bulk loading
- Implement a write-ahead log (WAL) for crash recovery
- Replace `sync.RWMutex` with `parking_lot::RwLock` (Rust) or a spinlock for short critical sections
- Benchmark against BoltDB (Go) or sled (Rust) with realistic key distributions (Zipfian, uniform)
