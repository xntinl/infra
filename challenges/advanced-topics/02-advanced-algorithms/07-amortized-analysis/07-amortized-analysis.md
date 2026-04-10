<!--
type: reference
difficulty: advanced
section: [02-advanced-algorithms]
concepts: [potential-method, accounting-method, aggregate-analysis, dynamic-array, union-find, splay-tree, fibonacci-heap]
languages: [go, rust]
estimated_reading_time: 45-60 min
bloom_level: analyze
prerequisites: [data-structures-basics, asymptotic-notation, binary-trees]
papers: [tarjan-1985-amortized-complexity, sleator-tarjan-1985-splay-trees, sleator-tarjan-1987-fibonacci-heap]
industry_use: [every-dynamic-array, every-hash-table, go-slice, rust-vec, java-arraylist, union-find-in-kruskal]
language_contrast: low
-->

# Amortized Analysis

> Most runtime analyses look at the worst case of a single operation. Amortized analysis proves that a sequence of operations is fast *on average*, even when individual operations can be slow.

## Mental Model

The confusion: "My vector's `push_back` is O(1) amortized — but sometimes it takes O(n)
to resize! How can it be O(1)?" This is the confusion amortized analysis resolves.

Think of it as **prepaying for expensive operations in advance**. Every cheap O(1) push
puts a "credit" in the bank. When a resize happens, you pay for it with the accumulated
credits. The bank never goes negative. Therefore, n pushes cost O(n) total, or O(1) per
operation *amortized*.

There are three formal methods, all equivalent but with different pedagogical strengths:

1. **Aggregate analysis**: Count the *total* cost of n operations, then divide by n.
   Easy to understand, but doesn't tell you *why* — just *that*.

2. **Accounting method**: Assign "amortized costs" to operations. Some operations are
   overcharged (they pay a tax stored as credit). Some are undercharged (they spend
   stored credit). Prove that the credit never goes negative. The amortized cost per
   operation is an upper bound on the average.

3. **Potential method**: Define a potential function Φ over the data structure's state.
   `Amortized cost = actual cost + ΔΦ`. Prove that ΔΦ absorbs spikes in actual cost.
   More mechanical than accounting; amenable to formal proofs.

The senior engineer's use of amortized analysis: "Can I prove that this data structure
is fast even though individual operations look slow?" If you can define a non-negative
potential that is zero for the initial state and monotonically controlled by the operations,
you can often show O(1) amortized for complex operations that look O(n) in isolation.

## Core Concepts

### Dynamic Array (Aggregate Analysis)

Double the array when full, starting at capacity 1. The cost of n pushes:
- Each insertion: 1 unit.
- Resizes at sizes 1, 2, 4, 8, ...: costs 1, 2, 4, 8, ...

Total resize cost = 1 + 2 + 4 + ... + n/2 = n - 1 < n. Total cost = n (inserts) + (n-1)
(resizes) = 2n - 1 = O(n). Average per operation: O(1). This is the textbook aggregate
analysis.

### Union-Find (Potential Method)

With path compression and union by rank:
- **Union by rank**: always attach the shorter tree under the taller tree.
- **Path compression**: when finding the root, make every traversed node point directly
  to the root.

Without compression: O(log n) per operation. With both: O(α(n)) amortized, where α is
the inverse Ackermann function — effectively O(1) for all practical n (α(10^80) = 5).

The potential function for the proof involves the "weight" of nodes relative to their
rank and the depth at which they sit in the tree. The proof is non-trivial but the
conclusion is simple: path compression does enough "cleanup work" each time it runs to
pay for future operations.

**Where it appears**: Kruskal's algorithm for MST (union-find for cycle detection),
compiler type inference (unification), network connectivity queries.

### Splay Trees

A self-adjusting BST: after any access (search, insert, delete), rotate the accessed
node to the root via "splay operations" (zig, zig-zig, zig-zag rotations). No explicit
balancing information stored.

Individual operations can be O(n) (for a degenerate tree). Amortized cost: O(log n) per
operation for any sequence of n operations.

**Why splay trees matter**: They satisfy the *static optimality* and *working set*
properties that balanced BSTs do not:
- Static optimality: if element i is accessed f_i times, the total splay cost is
  O(sum(f_i × log(n/f_i))) — matching the entropy lower bound for search. Balanced BSTs
  cannot do this.
- Working set property: the more recently an element was accessed, the cheaper it is to
  access again (O(log k) where k is the number of distinct elements accessed since the
  last access to this element).

**Where it appears**: The Linux kernel's completely fair scheduler (CFS) uses a red-black
tree, but splay trees are used in some compiler symbol tables and in certain cache implementations.

### Fibonacci Heap

A heap that supports decrease-key in O(1) amortized (compared to O(log n) for binary heap).
This makes Dijkstra's algorithm O(E + V log V) instead of O(E log V) — significant for
dense graphs.

The structure: a forest of heap-ordered trees. Lazy merging: link trees only when `extract-min`
is called. `decrease-key` cuts the node from its parent and adds it as a new tree root (a
"lazy cut").

The potential function Φ = (number of trees) + 2 × (number of marked nodes). A node is
marked if it has lost a child since the last time it was a tree root. The O(1) amortized
cost of `decrease-key` is charged against the potential increase. The O(log n) amortized
cost of `extract-min` pays for consolidating trees (linking trees of the same degree).

**Why it matters in practice**: Fibonacci heaps are rarely implemented in production because
the constant factors are high. But they are the *reason* the Dijkstra complexity bound
`O(E + V log V)` appears in papers — and understanding why requires understanding amortized
analysis.

## Implementation: Go

```go
package main

import "fmt"

// ─── Dynamic Array with amortized O(1) push ───────────────────────────────────

type DynamicArray struct {
	data []int
	sz   int
}

func (d *DynamicArray) Push(v int) {
	if d.sz == len(d.data) {
		newCap := 1
		if len(d.data) > 0 { newCap = len(d.data) * 2 }
		newData := make([]int, newCap)
		copy(newData, d.data)
		d.data = newData
	}
	d.data[d.sz] = v
	d.sz++
}

func (d *DynamicArray) Pop() (int, bool) {
	if d.sz == 0 { return 0, false }
	d.sz--
	return d.data[d.sz], true
}

func (d *DynamicArray) Len() int { return d.sz }

// ─── Union-Find with path compression and union by rank ──────────────────────

type UnionFind struct {
	parent []int
	rank   []int
}

func NewUnionFind(n int) *UnionFind {
	uf := &UnionFind{parent: make([]int, n), rank: make([]int, n)}
	for i := range uf.parent { uf.parent[i] = i }
	return uf
}

// Find with path compression (two-pass: recursive halving)
func (uf *UnionFind) Find(x int) int {
	if uf.parent[x] != x {
		uf.parent[x] = uf.Find(uf.parent[x]) // path compression
	}
	return uf.parent[x]
}

// Union by rank
func (uf *UnionFind) Union(x, y int) bool {
	rx, ry := uf.Find(x), uf.Find(y)
	if rx == ry { return false } // already in same component
	if uf.rank[rx] < uf.rank[ry] { rx, ry = ry, rx }
	uf.parent[ry] = rx
	if uf.rank[rx] == uf.rank[ry] { uf.rank[rx]++ }
	return true
}

// Connected returns true if x and y are in the same component.
func (uf *UnionFind) Connected(x, y int) bool {
	return uf.Find(x) == uf.Find(y)
}

// ─── Splay Tree ───────────────────────────────────────────────────────────────

type SplayNode struct {
	key         int
	left, right *SplayNode
}

type SplayTree struct{ root *SplayNode }

// rotate right: x's left child becomes x's parent
func rotateRight(x *SplayNode) *SplayNode {
	y := x.left
	x.left = y.right
	y.right = x
	return y
}

func rotateLeft(x *SplayNode) *SplayNode {
	y := x.right
	x.right = y.left
	y.left = x
	return y
}

// splay moves the node with the given key (or its predecessor/successor) to the root.
func splay(root *SplayNode, key int) *SplayNode {
	if root == nil { return nil }
	if root.key == key { return root }

	if key < root.key {
		if root.left == nil { return root }
		if key < root.left.key {
			// Zig-zig: left-left
			root.left.left = splay(root.left.left, key)
			root = rotateRight(root)
		} else if key > root.left.key {
			// Zig-zag: left-right
			root.left.right = splay(root.left.right, key)
			if root.left.right != nil { root.left = rotateLeft(root.left) }
		}
		if root.left == nil { return root }
		return rotateRight(root)
	}
	// key > root.key (symmetric)
	if root.right == nil { return root }
	if key > root.right.key {
		root.right.right = splay(root.right.right, key)
		root = rotateLeft(root)
	} else if key < root.right.key {
		root.right.left = splay(root.right.left, key)
		if root.right.left != nil { root.right = rotateRight(root.right) }
	}
	if root.right == nil { return root }
	return rotateLeft(root)
}

func (t *SplayTree) Insert(key int) {
	if t.root == nil { t.root = &SplayNode{key: key}; return }
	t.root = splay(t.root, key)
	if t.root.key == key { return } // duplicate
	node := &SplayNode{key: key}
	if key < t.root.key {
		node.right = t.root; node.left = t.root.left; t.root.left = nil
	} else {
		node.left = t.root; node.right = t.root.right; t.root.right = nil
	}
	t.root = node
}

func (t *SplayTree) Search(key int) bool {
	if t.root == nil { return false }
	t.root = splay(t.root, key)
	return t.root.key == key
}

// ─── Fibonacci Heap (simplified: insert + extract-min + decrease-key) ─────────

type FibNode struct {
	key     int
	degree  int
	marked  bool
	parent  *FibNode
	child   *FibNode
	left    *FibNode
	right   *FibNode
}

type FibHeap struct {
	min  *FibNode
	size int
}

func NewFibHeap() *FibHeap { return &FibHeap{} }

func (h *FibHeap) Insert(key int) *FibNode {
	node := &FibNode{key: key}
	node.left = node; node.right = node
	h.addToRootList(node)
	if h.min == nil || key < h.min.key { h.min = node }
	h.size++
	return node
}

func (h *FibHeap) addToRootList(node *FibNode) {
	if h.min == nil { h.min = node; return }
	// Insert node next to h.min in the circular doubly-linked root list
	node.right = h.min.right
	node.left = h.min
	h.min.right.left = node
	h.min.right = node
	node.parent = nil
}

func (h *FibHeap) ExtractMin() (int, bool) {
	if h.min == nil { return 0, false }
	z := h.min
	// Add z's children to root list
	if z.child != nil {
		child := z.child
		for {
			next := child.right
			h.addToRootList(child)
			child.parent = nil
			child = next
			if child == z.child { break }
		}
	}
	// Remove z from root list
	z.left.right = z.right
	z.right.left = z.left
	if z == z.right { h.min = nil } else { h.min = z.right; h.consolidate() }
	h.size--
	return z.key, true
}

func (h *FibHeap) consolidate() {
	maxDegree := 64
	a := make([]*FibNode, maxDegree)
	// Collect all roots
	var roots []*FibNode
	cur := h.min
	for {
		roots = append(roots, cur)
		cur = cur.right
		if cur == h.min { break }
	}
	for _, w := range roots {
		x := w
		d := x.degree
		for d < maxDegree && a[d] != nil {
			y := a[d]
			if x.key > y.key { x, y = y, x }
			h.link(y, x) // link y under x
			a[d] = nil
			d++
		}
		if d < maxDegree { a[d] = x }
	}
	h.min = nil
	for _, node := range a {
		if node != nil {
			if h.min == nil || node.key < h.min.key { h.min = node }
		}
	}
}

func (h *FibHeap) link(y, x *FibNode) {
	// Remove y from root list (handled implicitly by consolidate loop)
	y.left.right = y.right; y.right.left = y.left
	y.parent = x
	if x.child == nil {
		x.child = y; y.left = y; y.right = y
	} else {
		y.right = x.child; y.left = x.child.left
		x.child.left.right = y; x.child.left = y
	}
	x.degree++
	y.marked = false
}

func (h *FibHeap) DecreaseKey(node *FibNode, newKey int) {
	node.key = newKey
	parent := node.parent
	if parent != nil && node.key < parent.key {
		h.cut(node, parent)
		h.cascadingCut(parent)
	}
	if node.key < h.min.key { h.min = node }
}

func (h *FibHeap) cut(node, parent *FibNode) {
	// Remove node from parent's children
	if node.right == node {
		parent.child = nil
	} else {
		if parent.child == node { parent.child = node.right }
		node.left.right = node.right; node.right.left = node.left
	}
	parent.degree--
	h.addToRootList(node)
	node.marked = false
}

func (h *FibHeap) cascadingCut(node *FibNode) {
	parent := node.parent
	if parent != nil {
		if !node.marked { node.marked = true } else { h.cut(node, parent); h.cascadingCut(parent) }
	}
}

func main() {
	// Dynamic array demo
	var da DynamicArray
	for i := 0; i < 10; i++ { da.Push(i) }
	v, _ := da.Pop()
	fmt.Println("DynamicArray pop:", v, "len:", da.Len())

	// Union-Find demo
	uf := NewUnionFind(6)
	uf.Union(0, 1); uf.Union(1, 2); uf.Union(3, 4)
	fmt.Println("0 and 2 connected:", uf.Connected(0, 2)) // true
	fmt.Println("0 and 3 connected:", uf.Connected(0, 3)) // false

	// Splay Tree demo
	st := &SplayTree{}
	for _, v := range []int{5, 3, 7, 1, 4} { st.Insert(v) }
	fmt.Println("Splay search 4:", st.Search(4)) // true
	fmt.Println("Splay search 6:", st.Search(6)) // false

	// Fibonacci Heap demo
	fh := NewFibHeap()
	n1 := fh.Insert(3)
	fh.Insert(7)
	fh.Insert(1)
	fh.Insert(5)
	fh.DecreaseKey(n1, 0) // decrease 3 → 0
	min1, _ := fh.ExtractMin()
	min2, _ := fh.ExtractMin()
	fmt.Println("FibHeap extract-min sequence:", min1, min2) // 0, 1
}
```

### Go-specific considerations

- **Recursive path compression**: The recursive `Find` in union-find works well for typical
  tree depths (O(log n) before compression). For very large n with extremely imbalanced
  initial unions, convert to iterative two-pass path compression to avoid stack overflow.
- **Splay tree with pointer tricks**: Go's GC handles the pointer rotation cleanly. The
  shown splay uses top-down splaying (splay inline during traversal), which is simpler than
  bottom-up. The difference matters for concurrency: top-down allows read-optimistic paths.
- **Fibonacci heap in Go**: The circular doubly-linked list implemented with pointers is
  natural in Go. The GC handles the complex pointer structure. In practice, use a d-ary heap
  (standard `container/heap`) unless you specifically need O(1) decrease-key.

## Implementation: Rust

```rust
// ─── Dynamic Array (illustrating the amortized analysis; Vec<T> in practice) ─

struct DynamicArray {
    data: Vec<i64>, // uses Vec's internal doubling — illustrative only
    sz: usize,
}

impl DynamicArray {
    fn new() -> Self { DynamicArray { data: Vec::new(), sz: 0 } }

    fn push(&mut self, v: i64) {
        // Vec::push is itself amortized O(1) — we re-implement to make the
        // doubling explicit for educational purposes
        if self.sz == self.data.len() {
            let new_cap = if self.data.is_empty() { 1 } else { self.data.len() * 2 };
            self.data.resize(new_cap, 0);
        }
        self.data[self.sz] = v;
        self.sz += 1;
    }

    fn pop(&mut self) -> Option<i64> {
        if self.sz == 0 { return None; }
        self.sz -= 1;
        Some(self.data[self.sz])
    }

    fn len(&self) -> usize { self.sz }
}

// ─── Union-Find ───────────────────────────────────────────────────────────────

struct UnionFind {
    parent: Vec<usize>,
    rank: Vec<usize>,
}

impl UnionFind {
    fn new(n: usize) -> Self {
        UnionFind { parent: (0..n).collect(), rank: vec![0; n] }
    }

    // Iterative path compression (two-pass: find root, then compress)
    fn find(&mut self, mut x: usize) -> usize {
        let mut root = x;
        while self.parent[root] != root { root = self.parent[root]; }
        while self.parent[x] != root {
            let next = self.parent[x];
            self.parent[x] = root;
            x = next;
        }
        root
    }

    fn union(&mut self, x: usize, y: usize) -> bool {
        let (rx, ry) = (self.find(x), self.find(y));
        if rx == ry { return false; }
        let (big, small) = if self.rank[rx] >= self.rank[ry] { (rx, ry) } else { (ry, rx) };
        self.parent[small] = big;
        if self.rank[big] == self.rank[small] { self.rank[big] += 1; }
        true
    }

    fn connected(&mut self, x: usize, y: usize) -> bool {
        self.find(x) == self.find(y)
    }
}

// ─── Splay Tree (key insight: self-adjusting amortized O(log n)) ──────────────

// Index-based splay to avoid Rust's borrow checker conflicts with self-referential nodes.
struct SplayTree {
    key: Vec<i64>,
    left: Vec<Option<usize>>,
    right: Vec<Option<usize>>,
    root: Option<usize>,
}

impl SplayTree {
    fn new() -> Self {
        SplayTree { key: Vec::new(), left: Vec::new(), right: Vec::new(), root: None }
    }

    fn alloc(&mut self, k: i64) -> usize {
        let id = self.key.len();
        self.key.push(k); self.left.push(None); self.right.push(None);
        id
    }

    fn rotate_right(&mut self, x: usize) -> usize {
        let y = self.left[x].unwrap();
        self.left[x] = self.right[y];
        self.right[y] = Some(x);
        y
    }

    fn rotate_left(&mut self, x: usize) -> usize {
        let y = self.right[x].unwrap();
        self.right[x] = self.left[y];
        self.left[y] = Some(x);
        y
    }

    fn splay(&mut self, root: Option<usize>, k: i64) -> Option<usize> {
        let r = root?;
        if self.key[r] == k { return root; }

        if k < self.key[r] {
            let left = self.left[r]?;
            if k < self.key[left] {
                self.left[left] = self.splay(self.left[left], k);
                let r = self.rotate_right(r);
                if self.right[r].is_none() { return Some(r); }
                return Some(self.rotate_right(r));
            } else if k > self.key[left] {
                self.right[left] = self.splay(self.right[left], k);
                if self.right[left].is_some() {
                    self.left[r] = Some(self.rotate_left(left));
                }
            }
            if self.left[r].is_none() { Some(r) } else { Some(self.rotate_right(r)) }
        } else {
            let right = self.right[r]?;
            if k > self.key[right] {
                self.right[right] = self.splay(self.right[right], k);
                let r = self.rotate_left(r);
                if self.left[r].is_none() { return Some(r); }
                return Some(self.rotate_left(r));
            } else if k < self.key[right] {
                self.left[right] = self.splay(self.left[right], k);
                if self.left[right].is_some() {
                    self.right[r] = Some(self.rotate_right(right));
                }
            }
            if self.right[r].is_none() { Some(r) } else { Some(self.rotate_left(r)) }
        }
    }

    fn insert(&mut self, k: i64) {
        if self.root.is_none() { self.root = Some(self.alloc(k)); return; }
        self.root = self.splay(self.root, k);
        let r = self.root.unwrap();
        if self.key[r] == k { return; }
        let node = self.alloc(k);
        if k < self.key[r] {
            self.right[node] = Some(r);
            self.left[node] = self.left[r];
            self.left[r] = None;
        } else {
            self.left[node] = Some(r);
            self.right[node] = self.right[r];
            self.right[r] = None;
        }
        self.root = Some(node);
    }

    fn search(&mut self, k: i64) -> bool {
        self.root = self.splay(self.root, k);
        self.root.map_or(false, |r| self.key[r] == k)
    }
}

fn main() {
    // Dynamic array
    let mut da = DynamicArray::new();
    for i in 0..10i64 { da.push(i); }
    println!("DynamicArray pop: {:?}, len: {}", da.pop(), da.len());

    // Union-Find
    let mut uf = UnionFind::new(6);
    uf.union(0, 1); uf.union(1, 2); uf.union(3, 4);
    println!("0 and 2 connected: {}", uf.connected(0, 2)); // true
    println!("0 and 3 connected: {}", uf.connected(0, 3)); // false

    // Splay tree
    let mut st = SplayTree::new();
    for &v in &[5i64, 3, 7, 1, 4] { st.insert(v); }
    println!("Search 4: {}", st.search(4)); // true
    println!("Search 6: {}", st.search(6)); // false
}
```

### Rust-specific considerations

- **Iterative path compression**: Recursive `find` on a path graph of 10^6 nodes overflows
  Rust's 8 MB stack. The iterative two-pass version shown (find root, then compress) is
  idiomatic Rust for union-find and avoids all stack concerns.
- **Index-based splay tree**: Pointer-based splay trees require `Rc<RefCell<Node>>` or
  `unsafe` raw pointers in Rust — both are painful. The index-based approach (shown) avoids
  this entirely while maintaining the same algorithmic complexity. The downside: no in-place
  deletion of allocated nodes without a free-list.
- **`Vec<T>` amortization contract**: Rust's `Vec` guarantees O(1) amortized `push` but
  the growth factor is not specified (it happens to be 2× in the current implementation).
  This is a documented guarantee, not an implementation detail: the `Vec` documentation
  states "pushing elements to the end is amortized constant time."
- **Fibonacci heap ownership**: Implementing a Fibonacci heap in safe Rust requires arena
  allocation (e.g., `typed-arena` crate) or index-based nodes. The circular doubly-linked
  list with parent pointers is one of the most ownership-hostile data structures in existence.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|----|------|
| Dynamic array growth | `append` uses 2× (implementation detail); guaranteed O(1) amortized | `Vec::push` guaranteed O(1) amortized; 2× growth factor |
| Union-Find path compression | Recursive is safe for typical depths; iterative for large n | Must use iterative; recursive risks stack overflow |
| Splay tree pointers | `*SplayNode` pointers work naturally with GC | Index-based or `unsafe`; no safe pointer-based tree without `Rc<RefCell>` |
| Fibonacci heap | Circular linked list with `*FibNode` pointers works but is GC-heavy | Index-based or unsafe; one of Rust's hardest data structures |
| `container/heap` | Binary heap with custom `Less`; O(log n) push/pop | `BinaryHeap<T>` in std; no `decrease-key`; must use `(priority, item)` pairs |
| Performance | GC pauses affect amortized bounds under heavy allocation | Deterministic allocation; amortized bounds hold without GC interference |

## Production War Stories

**Go's `append` and Rust's `Vec::push`**: Every Go slice and Rust Vec uses the doubling
strategy with O(1) amortized push. The Go compiler knows this and avoids unnecessary
allocations by passing slice headers by value. The Go blog post "Arrays, slices (and strings):
The mechanics of 'append'" describes the amortized analysis that every Go developer should know.

**Kruskal's MST in network infrastructure**: Every major networking company (Cisco, Juniper,
Arista) implements Kruskal's algorithm for spanning tree protocol computation. The union-find
with path compression and union by rank is the standard implementation because the O(α(n))
bound is indistinguishable from O(1) for network sizes.

**Linux Kernel CFS scheduler**: The completely fair scheduler uses a red-black tree for its
run queue. Red-black tree operations are O(log n) worst case with small constants. The
"amortized" behavior of the scheduler comes from the fairness property: each task's virtual
runtime changes by a constant per scheduling tick, so the tree structure changes slowly.

**Java's `HashMap` rehashing**: Java's HashMap doubles its capacity when the load factor
exceeds 0.75. The amortized O(1) insert is achieved by the same doubling argument as
dynamic arrays, applied to the hash table's bucket array. This is documented in the
`HashMap` Javadoc.

**Compiler type inference (unification)**: Hindley-Milner type inference uses union-find
to unify type variables. The OCaml compiler, Haskell's GHC, and Rust's own type checker
all use path-compressed union-find for this purpose. The O(α(n)) bound enables type
inference to scale to files with tens of thousands of expressions.

## Complexity Analysis

| Data Structure | Operation | Amortized | Worst Case | Analysis Method |
|----------------|-----------|-----------|------------|----------------|
| Dynamic array | push | O(1) | O(n) | Aggregate / Potential |
| Dynamic array | pop | O(1) | O(1) | — |
| Union-Find | find, union | O(α(n)) | O(log n) | Potential (complex) |
| Splay tree | search, insert, delete | O(log n) | O(n) | Potential (access lemma) |
| Fibonacci heap | insert | O(1) | O(1) | Potential |
| Fibonacci heap | decrease-key | O(1) | O(log n) | Potential |
| Fibonacci heap | extract-min | O(log n) | O(n) | Potential |

**Why Fibonacci heap is rarely used in practice**: The O(1) amortized decrease-key is
theoretically beautiful but the constant factors for insert and extract-min are large
(~10–20×) compared to a binary heap. For Dijkstra on typical graphs (sparse, E ≈ 5V),
the V log V term dominates even with O(E) decrease-key calls, so the binary heap's
simplicity wins. Fibonacci heaps shine only for dense graphs (E ≈ V²).

## Common Pitfalls

1. **Confusing amortized and average**: "Amortized O(1)" means the total cost for n
   operations is O(n). "Average O(1)" could mean many things (average over random inputs,
   average over time, etc.). They are different guarantees. Dynamic array push is amortized
   O(1) — not just average.

2. **Assuming amortized bounds hold under concurrent access**: Amortized bounds assume
   sequential access patterns. Under concurrent access, a "cheap" operation (that spent
   accumulated credit) followed by an "expensive" operation (that needs that credit) on
   different threads can break the analysis. Union-find requires a lock for concurrent use
   unless using a specific concurrent design.

3. **Union-Find: not applying path compression and union by rank together**: Path
   compression alone gives O(log n) amortized. Union by rank alone gives O(log n)
   worst case. Together they give O(α(n)). Using only one is significantly worse.

4. **Splay tree: measuring worst-case per operation**: Measuring the latency of a single
   splay operation can show O(n) — leading to the wrong conclusion that splay trees are
   slow. The correct benchmark runs a *sequence* of operations and measures total time.

5. **Dynamic array: pop not triggering shrink**: Many implementations shrink the array
   when size drops below 25% of capacity (to maintain O(1) amortized pop). Without
   shrinking, a push-pop-push-pop sequence causes O(1) amortized push but the array
   never shrinks, causing memory waste. The standard fix: shrink to half capacity when
   size drops to 1/4 capacity.

## Exercises

**Exercise 1 — Verification** (30 min):
Instrument the dynamic array to count total character comparisons (or copies) across n=10^6
push operations. Verify empirically that the total is ≤ 2n. Plot the "credit balance"
(pushes done since last resize, minus capacity gain from last resize) as a function of n.
It should be non-negative at all times.

**Exercise 2 — Extension** (2–4 h):
Implement weighted union-find (union by weight, not rank — the weight tracks the actual
subtree size). Prove and verify empirically that union by weight gives O(log n) find
without path compression. Then add path compression and show the O(α(n)) improvement.
Use 10^6 random union and find operations and measure both the operation count and the
total path-traversal length.

**Exercise 3 — From Scratch** (4–8 h):
Implement a splay tree that supports the "split at key k" and "merge two splay trees"
operations (in addition to insert/delete/search). These make splay trees competitive
with treaps for persistent sequence problems. Implement a persistent version using
copy-on-splay and verify that the O(log n) amortized bound still holds. Profile memory
usage: how many nodes are shared between versions?

**Exercise 4 — Production Scenario** (8–15 h):
Build a social network connectivity service. Input: a stream of "friending" events (user A
and user B become friends). Query: "are users A and B in the same connected component?"
Requirements: process 10^6 friending events, answer 10^6 connectivity queries, all in
< 100ms total. Compare three implementations: (a) union-find with both optimizations,
(b) union-find with only path compression, (c) union-find with only union by rank.
Measure total time, max single-operation time, and average path length before and after
path compression. Expose as a Go or Rust service with a gRPC API.

## Further Reading

### Foundational Papers
- Tarjan, R. E. (1985). "Amortized computational complexity." *SIAM Journal on Algebraic
  and Discrete Methods*, 6(2), 306–318. The paper that introduced the potential method.
- Sleator, D. D., & Tarjan, R. E. (1985). "Self-adjusting binary search trees." *Journal
  of the ACM*, 32(3), 652–686. The splay tree paper; the access lemma and amortized proof.
- Fredman, M. L., & Tarjan, R. E. (1987). "Fibonacci heaps and their uses in improved
  network optimization algorithms." *Journal of the ACM*, 34(3), 596–615.

### Books
- *Introduction to Algorithms (CLRS)* — Chapter 17: Amortized Analysis. The standard
  treatment: aggregate, accounting, and potential methods with dynamic table and union-find.
- *Data Structures and Network Algorithms* — Tarjan. Dense but authoritative; covers
  union-find, splay trees, and the potential method in depth.

### Production Code to Read
- **Go runtime `append`** (`src/runtime/slice.go`): `growslice` — the actual doubling
  implementation with size and alignment edge cases.
- **Rust `Vec::push`** (`library/alloc/src/vec/mod.rs`): `push` → `reserve_for_push` →
  `grow_amortized`. Annotated with the amortized bound.
- **Linux kernel red-black tree** (`lib/rbtree.c`): `rb_insert_color` — the O(log n)
  worst-case insert that CFS relies on.

### Conference Talks
- "Amortized Analysis and Data Structure Design" — MIT 6.046J Lecture 13 (OCW).
  Erik Demaine's treatment with dynamic tables and union-find.
- "Understanding Vec's Amortized Growth" — RustConf 2018. The Rust standard library's
  approach to memory growth and amortized guarantees.
