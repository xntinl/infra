<!--
type: reference
difficulty: advanced
section: [01-advanced-data-structures]
concepts: [path-copying, fat-node, persistent-bst, version-tree, mvcc, functional-data-structures, structural-sharing]
languages: [go, rust]
estimated_reading_time: 90 min
bloom_level: analyze
prerequisites: [binary-trees, garbage-collection-basics, rust-arc, immutability-patterns]
papers: [driscoll-1989-making-data-structures-persistent, okasaki-1998-functional-data-structures]
industry_use: [postgresql-mvcc, git-object-model, clojure-persistent-vectors, compiler-ssa-forms]
language_contrast: high
-->

# Persistent Data Structures

> PostgreSQL's MVCC (Multi-Version Concurrency Control) is the most deployed instance of persistent data structures in the world: every row version is kept immutably, readers never block writers, and the "current" version is just a pointer to the latest node in a version tree — the same structural sharing that makes purely functional languages efficient.

## Mental Model

A persistent data structure preserves its previous versions when modified. "Fully persistent" means you can update any past version and branch the history. "Partially persistent" means you can only update the latest version. "Ephemeral" is the normal case — modification destroys the previous state.

The key insight separating someone who just knows the term from someone who has used it in production: persistence does not mean copying. Copying the entire data structure on every modification would make persistence O(n) per operation and useless. Efficient persistence relies on **structural sharing** — the new version shares most of its structure with the old version, only copying the nodes on the path from the root to the modified element.

For a balanced binary search tree with n elements, a point update requires copying only the O(log n) nodes on the path from root to leaf — the other O(n - log n) nodes are shared by reference. The new root and its path are freshly allocated; everything else is untouched. Old versions remain valid because nothing in the shared structure was mutated.

The three methods for achieving persistence:

**Path copying** (simplest): On any structural modification, copy every node on the path from root to the modified location. The new version gets a new root pointing to the new path; all unchanged subtrees are shared. This is O(log n) extra space and time per operation for balanced trees.

**Fat node method**: Instead of copying nodes, each node stores a version list — a sorted list of (version, value) pairs for each field. A query at version v reads the value current at that version. This gives O(1) space per update but O(log history_length) per query (binary search through the version list). Used when the number of queries per update is large.

**Node copying with limited copying**: A hybrid that reduces the amortized space cost below the O(log n) of path copying. Rarely used in practice.

Path copying is the dominant technique in production systems because it composes naturally with garbage collection (unreachable old versions are collected automatically), it supports lock-free concurrent access (old versions are immutable, new versions are published atomically via pointer swap), and it is simple enough to reason about correctness.

## Core Concepts

### Structural Sharing and the Version Tree

When you modify a persistent data structure, you get a new root. The old root is still valid. If you keep both roots, you have two "versions" of the structure. If you perform n updates starting from the initial version, you have n+1 roots and a tree of versions — the version DAG (or version tree for partial persistence).

The memory cost analysis: each version points to O(log n) new nodes (for a balanced tree). After v versions, the total memory is O(n + v × log n) — the initial O(n) plus O(log n) new nodes per version. This is optimal: you cannot share more without risking mutation of shared nodes.

### Persistent BST via Path Copying

A persistent BST insert works as follows: search for the insertion point (reading only, no modification), then copy nodes from root to the new leaf, updating each copied node's appropriate child pointer to point to the next copied node. The resulting path of new nodes is "published" by returning the new root. All old roots remain valid and can be queried concurrently.

The same works for delete, with the added complexity of the standard BST deletion (find in-order successor to replace a 2-child deleted node). Every node that would normally be mutated is instead copied.

### Persistent Arrays: The Radix Tree Approach

Clojure's persistent vector and Scala's `Vector` use a radix tree (a 32-ary branching tree) for arrays. An update to position i copies only the nodes on the path from root to the leaf containing i — O(log_{32}(n)) = O(log n / log 32) nodes. For n=10^6 elements, this is 4 nodes. For n=10^9, it is 6 nodes. The 32-ary branching is specifically chosen because 32 pointers fit in a single cache line, minimizing the performance overhead of the extra indirection.

### Persistence in MVCC (Multi-Version Concurrency Control)

PostgreSQL's MVCC is the largest-scale deployment of persistent data structures in production. Each row update creates a new row tuple (the "new version") without overwriting the old one. Each transaction has a snapshot: a set of (transaction_id, row_version) pairs visible to it. A query reads only the row versions visible to its snapshot, ignoring newer versions created by concurrent transactions.

This is exactly a persistent data structure: the "current" state is a pointer to the latest version; old versions are preserved for readers with older snapshots. VACUUM (PostgreSQL's garbage collector) reclaims row versions that no older transaction can see — the analog of GC collecting unreachable versions.

## Implementation: Go

```go
package main

import "fmt"

// PersistentBST implements a persistent binary search tree via path copying.
// Each operation returns a new root; the old root remains valid.
// Thread-safe for concurrent reads of any version (nodes are immutable after creation).
// Concurrent writes must serialize root-pointer publication via an atomic or mutex.

type bstNode struct {
	key   int
	value string
	left  *bstNode // immutable after creation
	right *bstNode // immutable after creation
}

// newNode creates an immutable BST node. Once created, fields are never written.
func newNode(key int, value string, left, right *bstNode) *bstNode {
	return &bstNode{key: key, value: value, left: left, right: right}
}

// insert returns a new root with (key, value) inserted.
// The old root is unchanged. Path copying creates O(log n) new nodes.
func insert(root *bstNode, key int, value string) *bstNode {
	if root == nil {
		return newNode(key, value, nil, nil)
	}
	switch {
	case key < root.key:
		// Copy current node, updating only the left child pointer
		newLeft := insert(root.left, key, value)
		return newNode(root.key, root.value, newLeft, root.right)
	case key > root.key:
		newRight := insert(root.right, key, value)
		return newNode(root.key, root.value, root.left, newRight)
	default:
		// Key already exists: copy node with updated value, share both children
		return newNode(root.key, value, root.left, root.right)
	}
}

// get searches the tree rooted at root, reading shared nodes without copying.
func get(root *bstNode, key int) (string, bool) {
	for root != nil {
		switch {
		case key < root.key:
			root = root.left
		case key > root.key:
			root = root.right
		default:
			return root.value, true
		}
	}
	return "", false
}

// minNode returns the leftmost (minimum) node in the subtree.
func minNode(n *bstNode) *bstNode {
	for n.left != nil {
		n = n.left
	}
	return n
}

// delete returns a new root with key removed.
// Returns (new_root, true) if deleted, (old_root, false) if not found.
func delete(root *bstNode, key int) (*bstNode, bool) {
	if root == nil {
		return nil, false
	}
	switch {
	case key < root.key:
		newLeft, deleted := delete(root.left, key)
		if !deleted {
			return root, false
		}
		return newNode(root.key, root.value, newLeft, root.right), true
	case key > root.key:
		newRight, deleted := delete(root.right, key)
		if !deleted {
			return root, false
		}
		return newNode(root.key, root.value, root.left, newRight), true
	default:
		// Found the node to delete
		switch {
		case root.left == nil:
			return root.right, true
		case root.right == nil:
			return root.left, true
		default:
			// Two children: replace with in-order successor
			successor := minNode(root.right)
			// Delete the successor from the right subtree, then copy current node
			// with the successor's key/value and the pruned right subtree
			newRight, _ := delete(root.right, successor.key)
			return newNode(successor.key, successor.value, root.left, newRight), true
		}
	}
}

// inorder collects all (key, value) pairs in sorted order from a given root.
func inorder(root *bstNode) [][2]string {
	if root == nil {
		return nil
	}
	var result [][2]string
	result = append(result, inorder(root.left)...)
	result = append(result, [2]string{fmt.Sprintf("%d", root.key), root.value})
	result = append(result, inorder(root.right)...)
	return result
}

// PersistentBST wraps root management and version tracking.
// Each Insert/Delete call produces a new version; all previous versions remain accessible.
type PersistentBST struct {
	roots [](*bstNode) // roots[i] is the root of version i
}

func NewPersistentBST() *PersistentBST {
	return &PersistentBST{roots: []*bstNode{nil}} // version 0 is the empty tree
}

func (p *PersistentBST) Insert(key int, value string) int {
	current := p.roots[len(p.roots)-1]
	newRoot := insert(current, key, value)
	p.roots = append(p.roots, newRoot)
	return len(p.roots) - 1 // return version number
}

func (p *PersistentBST) Delete(key int) (int, bool) {
	current := p.roots[len(p.roots)-1]
	newRoot, deleted := delete(current, key)
	if !deleted {
		return len(p.roots) - 1, false // no change; same version
	}
	p.roots = append(p.roots, newRoot)
	return len(p.roots) - 1, true
}

func (p *PersistentBST) GetAtVersion(version, key int) (string, bool) {
	if version < 0 || version >= len(p.roots) {
		return "", false
	}
	return get(p.roots[version], key)
}

func (p *PersistentBST) GetLatest(key int) (string, bool) {
	return get(p.roots[len(p.roots)-1], key)
}

func (p *PersistentBST) InorderAtVersion(version int) [][2]string {
	if version < 0 || version >= len(p.roots) {
		return nil
	}
	return inorder(p.roots[version])
}

func (p *PersistentBST) CurrentVersion() int { return len(p.roots) - 1 }

// PersistentArray implements a persistent array using a path-copying binary trie.
// This is a simplified version of Clojure's persistent vector (branching factor 2 instead of 32).
// A branching factor of 32 or 64 would be more cache-efficient in practice.
type trieNode struct {
	children [2]*trieNode // binary trie: 0 = left, 1 = right
	value    interface{}  // only set on leaf nodes
	isLeaf   bool
}

type PersistentArray struct {
	root  *trieNode
	size  int
	depth int // tree height; derived from size
}

func newPersistentArray(size int) *PersistentArray {
	depth := 0
	for (1 << uint(depth)) < size {
		depth++
	}
	return &PersistentArray{depth: depth, size: size}
}

// trieGet returns the value at position i from the trie rooted at root.
func trieGet(root *trieNode, i, depth int) interface{} {
	if root == nil {
		return nil
	}
	if depth == 0 || root.isLeaf {
		return root.value
	}
	bit := (i >> uint(depth-1)) & 1
	return trieGet(root.children[bit], i, depth-1)
}

// trieSet returns a new trie with position i set to value.
func trieSet(root *trieNode, i int, depth int, value interface{}) *trieNode {
	if depth == 0 {
		// Leaf level: copy and update
		return &trieNode{value: value, isLeaf: true}
	}
	// Copy the current node, updating only the child on the path to i
	newNode := &trieNode{}
	if root != nil {
		*newNode = *root // copy all fields
	}
	bit := (i >> uint(depth-1)) & 1
	newNode.children[bit] = trieSet(root.children[bit], i, depth-1, value)
	return newNode
}

func (pa *PersistentArray) Get(i int) interface{} {
	return trieGet(pa.root, i, pa.depth)
}

func (pa *PersistentArray) Set(i int, value interface{}) *PersistentArray {
	newRoot := trieSet(pa.root, i, pa.depth, value)
	return &PersistentArray{root: newRoot, size: pa.size, depth: pa.depth}
}

func main() {
	fmt.Println("=== Persistent BST ===")
	p := NewPersistentBST()

	v1 := p.Insert(5, "five")
	v2 := p.Insert(3, "three")
	v3 := p.Insert(7, "seven")
	v4 := p.Insert(1, "one")
	v5 := p.Insert(4, "four")

	fmt.Printf("Version %d: ", v5)
	for _, pair := range p.InorderAtVersion(v5) {
		fmt.Printf("%s=%s ", pair[0], pair[1])
	}
	fmt.Println()

	// Delete 3 to get version 6
	v6, deleted := p.Delete(3)
	fmt.Printf("Delete(3) succeeded: %v, new version: %d\n", deleted, v6)

	// v2 still sees key 3 — the old version is intact
	if val, ok := p.GetAtVersion(v2, 3); ok {
		fmt.Printf("Version %d (before delete), key 3: %q\n", v2, val)
	}

	// v6 does not have key 3
	if _, ok := p.GetAtVersion(v6, 3); !ok {
		fmt.Printf("Version %d (after delete), key 3: not found\n", v6)
	}

	// Demonstrate branching: insert from an old version
	vBranch := v3 // branch from after inserting 5, 3, 7
	// Create v7 by inserting 10 at version v3 (not v6)
	_ = vBranch
	v7 := p.Insert(10, "ten") // this inserts into the latest (v6), not a branch
	fmt.Printf("Version %d inorder: ", v7)
	for _, pair := range p.InorderAtVersion(v7) {
		fmt.Printf("%s=%s ", pair[0], pair[1])
	}
	fmt.Println()

	fmt.Println("\n=== Persistent Array (path-copying trie) ===")
	arr := newPersistentArray(8)
	arr1 := arr.Set(0, 10).Set(1, 20).Set(2, 30).Set(3, 40).Set(4, 50)
	arr2 := arr1.Set(2, 999) // new version with arr[2] = 999

	fmt.Printf("arr1[2] = %v\n", arr1.Get(2)) // 30 — old version intact
	fmt.Printf("arr2[2] = %v\n", arr2.Get(2)) // 999 — new version
	fmt.Printf("arr1[0] = %v\n", arr1.Get(0)) // 10 — shared
	fmt.Printf("arr2[0] = %v\n", arr2.Get(0)) // 10 — same shared node
}
```

### Go-specific considerations

Go's garbage collector is the primary enabler of clean persistent data structure implementations. In the BST above, old versions that are no longer referenced by any external variable are automatically collected. There is no need to track which nodes are shared or implement reference counting. The immutability invariant (nodes are never written after creation) means the GC never sees stale pointers or concurrent write races through the shared structure.

The version slice (`p.roots [](*bstNode)`) itself holds strong references to all version roots, preventing collection of the trees rooted there. To allow GC of old versions, either use weak references (not available in Go stdlib, but implementable via `runtime.SetFinalizer`) or simply remove entries from the slice.

For concurrent access in Go: multiple goroutines can read any version concurrently without any synchronization. A single goroutine (or a mutex-protected writer) publishes new versions by atomically updating a shared `*bstNode` pointer using `atomic.Pointer[bstNode]` (Go 1.19+). Readers snapshot the root pointer before beginning their traversal. This is the lock-free reader pattern used in read-heavy caches.

## Implementation: Rust

```rust
use std::sync::Arc;

// Persistent BST in Rust using Arc for shared ownership.
// Arc<T> is reference-counted, immutable, and thread-safe — exactly what path copying needs.
// Nodes are cloned (shallow copy, O(log n) per operation) when creating the new version.
// Old versions remain valid as long as any Arc points to them.

#[derive(Clone)]
struct Node {
    key: i32,
    value: String,
    // Arc<Node> allows shared ownership of child nodes across versions.
    // Option<Arc<Node>> = nullable pointer with reference counting.
    left: Option<Arc<Node>>,
    right: Option<Arc<Node>>,
}

impl Node {
    fn new(key: i32, value: String, left: Option<Arc<Node>>, right: Option<Arc<Node>>) -> Arc<Node> {
        Arc::new(Node { key, value, left, right })
    }
}

fn insert(root: Option<Arc<Node>>, key: i32, value: String) -> Arc<Node> {
    match root {
        None => Node::new(key, value, None, None),
        Some(n) => {
            if key < n.key {
                // Path copying: create a new node with only left updated
                Node::new(n.key, n.value.clone(), Some(insert(n.left.clone(), key, value)), n.right.clone())
            } else if key > n.key {
                Node::new(n.key, n.value.clone(), n.left.clone(), Some(insert(n.right.clone(), key, value)))
            } else {
                // Update existing key: copy node with new value, share both children
                Node::new(n.key, value, n.left.clone(), n.right.clone())
            }
        }
    }
}

fn get(mut root: Option<Arc<Node>>, key: i32) -> Option<String> {
    loop {
        match root {
            None => return None,
            Some(n) => {
                if key < n.key {
                    root = n.left.clone();
                } else if key > n.key {
                    root = n.right.clone();
                } else {
                    return Some(n.value.clone());
                }
            }
        }
    }
}

fn min_node(n: &Arc<Node>) -> Arc<Node> {
    let mut current = n.clone();
    while let Some(left) = current.left.clone() {
        current = left;
    }
    current
}

fn delete(root: Option<Arc<Node>>, key: i32) -> (Option<Arc<Node>>, bool) {
    match root {
        None => (None, false),
        Some(n) => {
            if key < n.key {
                let (new_left, deleted) = delete(n.left.clone(), key);
                if !deleted { return (Some(n), false); }
                (Some(Node::new(n.key, n.value.clone(), new_left, n.right.clone())), true)
            } else if key > n.key {
                let (new_right, deleted) = delete(n.right.clone(), key);
                if !deleted { return (Some(n), false); }
                (Some(Node::new(n.key, n.value.clone(), n.left.clone(), new_right)), true)
            } else {
                // Found the node to delete
                match (&n.left, &n.right) {
                    (None, right) => (right.clone(), true),
                    (left, None) => (left.clone(), true),
                    (_, Some(right)) => {
                        let successor = min_node(right);
                        let (new_right, _) = delete(n.right.clone(), successor.key);
                        (Some(Node::new(
                            successor.key,
                            successor.value.clone(),
                            n.left.clone(),
                            new_right,
                        )), true)
                    }
                }
            }
        }
    }
}

fn inorder(root: &Option<Arc<Node>>, result: &mut Vec<(i32, String)>) {
    if let Some(n) = root {
        inorder(&n.left, result);
        result.push((n.key, n.value.clone()));
        inorder(&n.right, result);
    }
}

// PersistentBST keeps all version roots.
// Arc reference counts prevent collection of shared nodes as long as any version references them.
struct PersistentBST {
    versions: Vec<Option<Arc<Node>>>,
}

impl PersistentBST {
    fn new() -> Self {
        PersistentBST { versions: vec![None] }
    }

    fn insert(&mut self, key: i32, value: &str) -> usize {
        let current = self.versions.last().cloned().flatten();
        let new_root = insert(current, key, value.to_string());
        self.versions.push(Some(new_root));
        self.versions.len() - 1
    }

    fn delete(&mut self, key: i32) -> (usize, bool) {
        let current = self.versions.last().cloned().flatten();
        let (new_root, deleted) = delete(current, key);
        if deleted {
            self.versions.push(new_root);
            (self.versions.len() - 1, true)
        } else {
            (self.versions.len() - 1, false)
        }
    }

    fn get_at_version(&self, version: usize, key: i32) -> Option<String> {
        self.versions.get(version).and_then(|r| get(r.clone(), key))
    }

    fn inorder_at_version(&self, version: usize) -> Vec<(i32, String)> {
        let mut result = Vec::new();
        if let Some(root) = self.versions.get(version) {
            inorder(root, &mut result);
        }
        result
    }

    fn current_version(&self) -> usize { self.versions.len() - 1 }
}

fn main() {
    let mut tree = PersistentBST::new();

    let v1 = tree.insert(5, "five");
    let v2 = tree.insert(3, "three");
    let v3 = tree.insert(7, "seven");
    let _v4 = tree.insert(1, "one");
    let v5 = tree.insert(4, "four");

    print!("Version {}: ", v5);
    for (k, v) in tree.inorder_at_version(v5) {
        print!("{}={} ", k, v);
    }
    println!();

    let (v6, deleted) = tree.delete(3);
    println!("Delete(3): succeeded={}, version={}", deleted, v6);

    // Old version still has key 3
    println!("Version {} key 3: {:?}", v2, tree.get_at_version(v2, 3));

    // New version does not
    println!("Version {} key 3: {:?}", v6, tree.get_at_version(v6, 3));

    // Shared node: versions v1 and v2 share the node for key=5
    // Arc reference count for the root of v1 is >= 1 (held by versions vec)
    // and the key-5 node itself may be shared by several versions
    println!("Version {} key 5: {:?}", v1, tree.get_at_version(v1, 5));
    println!("Version {} key 5: {:?}", v6, tree.get_at_version(v6, 5));
}
```

### Rust-specific considerations

`Arc<Node>` is the key type. `Arc` provides shared ownership with atomic reference counting — multiple versions can hold `Arc` pointers to the same node, and the node is dropped only when the last `Arc` goes out of scope. This is safer than `Rc<Node>` (which is single-threaded) or raw pointers (which require `unsafe` and manual reference tracking).

The `.clone()` calls on `Arc<Node>` are O(1) — they increment the atomic reference count without allocating. The actual node allocation happens only in `Node::new` (one allocation per new node on the path). This is the correct interpretation of "path copying is O(log n) per update."

The `.clone()` calls on `String` inside node fields are O(k) where k is the string length — a potential performance concern for large values. In production, replace `String` with `Arc<str>` or a byte-slice handle to avoid copying values on path copying. Values are shared across versions as frequently as structure nodes.

For truly lock-free concurrent persistent BSTs in Rust, use `crossbeam`'s `ArcSwap` or `arc-swap` crate to atomically publish new roots. The pattern: `let root = ArcSwap::new(Arc::new(initial_tree))`. Writers call `root.swap(Arc::new(new_tree))`, readers call `root.load()` — this is the same pattern as the Go `atomic.Pointer` version.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|----|------|
| Shared node ownership | `*bstNode` pointers, GC collects unreachable nodes | `Arc<Node>` with reference counting; drop when refcount hits 0 |
| Old version collection | Automatic (GC); no code required | Automatic (when all Arcs to a version are dropped) |
| Concurrent reads | No synchronization needed (nodes are immutable) | Same; `Arc` is `Send + Sync`, so `Arc<Node>` can be shared across threads |
| Root publication | `atomic.Pointer[bstNode]` (Go 1.19+) | `ArcSwap<Node>` from the `arc-swap` crate |
| Copy cost per update | O(log n) pointer allocations + GC marking | O(log n) Arc increments + Arc allocations |
| Memory overhead | GC header per node (~16 bytes) | Arc control block (~16 bytes for refcount + weak count) |

The practical difference is that Go's GC handles cycles transparently (if they existed), while `Arc` would leak cyclic structures. Persistent BSTs are acyclic by definition, so this difference does not apply here. The GC vs reference counting tradeoff does affect pause behavior: Go's GC pauses are bounded by the live set size, while `Arc` drops are O(1) per node but can cascade (dropping one root can trigger O(log n) node drops, each O(1)).

## Production War Stories

**PostgreSQL MVCC** (PostgreSQL source: `storage/heap/heapam.c`): PostgreSQL's row-level versioning is path copying applied to heap pages. Each `UPDATE` creates a new tuple version (a copy of the row with `xmin` and `xmax` timestamps) and marks the old tuple as "deleted by transaction xid." Readers see only tuples whose `xmin` <= their snapshot and `xmax` > their snapshot. The VACUUM process is the GC: it removes tuple versions where `xmax` < the oldest active transaction's snapshot. The design directly mirrors persistent data structure theory — the "old version" (expired tuple) is preserved as long as any transaction might need it, then collected.

**Git's object model** (git source: `object.c`): Git's commit graph is a persistent directed acyclic graph. Every file version is stored as an immutable blob; every directory is an immutable tree of (name, hash) pairs; every commit is an immutable snapshot of the root tree plus parent commit hashes. A commit is structurally identical to a persistent data structure version: it shares subtrees with previous commits via content-addressed hashing (SHA-1/SHA-256). A branch is just a mutable pointer (a ref) to the latest version's root hash.

**Clojure's persistent vector** (Clojure source: `clojure/lang/PersistentVector.java`): Clojure's core data structure is a persistent vector implemented as a 32-ary trie. The branching factor of 32 was chosen by Rich Hickey specifically because 32 pointers fit in exactly one cache line (256 bytes on common architectures). An update to index i copies log_{32}(n) nodes — at most 7 nodes for a billion-element vector. The implementation is described in the paper "Efficient Immutable Collections" (Bagwell, 2001), which introduced the HAMT (Hash Array Mapped Trie) that underlies Clojure, Scala, and Haskell's `Data.Map`.

**Compiler SSA (Static Single Assignment)** (LLVM, GCC): SSA form, used by every modern compiler for optimization, requires that each variable is assigned exactly once. Variable "versions" after branches are represented via φ (phi) functions — exactly the version branching of a persistent data structure. The intermediate representation after SSA conversion is a DAG of immutable value nodes, where each optimization pass produces a new set of nodes sharing most of the previous structure (the unmodified parts of the program).

## Complexity Analysis

| Operation | Path Copying BST | Fat Node BST | Ephemeral BST |
|-----------|-----------------|--------------|---------------|
| Insert | O(log n) time, O(log n) new nodes | O(1) extra space | O(log n) |
| Delete | O(log n) time, O(log n) new nodes | O(1) extra space | O(log n) |
| Query at version v | O(log n) | O(log n + log h) where h=history | O(log n) |
| Space after v updates | O(n + v × log n) | O(n + v) | O(n) |
| Memory per version | O(log n) new allocations | O(1) | O(1) |

The persistent array (radix trie, branching factor 32) has O(log_{32}(n)) per operation — for n=10^9, this is 6 nodes. In practice, Clojure's vector benchmarks show about 3x slowdown compared to a mutable array for sequential access, and 2x for random access — a reasonable trade for immutable semantics.

Cache behavior: path copying creates O(log n) new allocations per update, each potentially in a different cache line. This is the primary performance cost compared to an ephemeral structure that updates in-place. For write-heavy workloads with large n, this allocation pressure is significant. For read-heavy workloads where multiple readers query different versions concurrently (MVCC databases, functional collections), the performance is often better than locking because readers never block.

## Common Pitfalls

**Pitfall 1: Mutating a "copied" node after sharing it**

The invariant of path copying is that once a node is shared between two versions, neither version ever mutates it. In Go, this is not enforced by the type system — the programmer must ensure `bstNode` fields are never written after creation. A common bug is forgetting that a "new" node returned by an insert helper was created but might have been stored in two places before a second write happens. Using `const` struct fields (not available in Go) or an immutability linter enforces this.

In Rust, `Arc<T>` enforces immutability: you cannot get a `&mut T` through an `Arc<T>` (you would need `Arc<Mutex<T>>`). This is one of the strongest arguments for using `Arc<Node>` in Rust persistent structures — the type system prevents accidental mutation.

**Pitfall 2: O(n) copy instead of O(log n) path copying**

A version built by cloning the entire tree structure (`deepcopy(root)`) is correct but costs O(n) time and space per update. This is the "trivial" approach to persistence and is what many teams implement first when they think "I need to preserve the old version." The path copying insight (share everything except the path) cuts this to O(log n).

Detection: memory allocation spikes proportional to the tree size on every update, not proportional to tree depth.

**Pitfall 3: Version history grows unboundedly**

Storing all versions in a slice (`p.roots [](*bstNode)`) means memory never shrinks. For a system that creates millions of versions (e.g., a document store with one version per edit), this becomes a memory leak. Implement a version retention policy: keep the last k versions, keep versions for active transactions (MVCC), or use weak references for old versions.

**Pitfall 4: Assuming persistent structures are always slower**

Persistent structures are often assumed to be 2-5x slower than mutable equivalents. This is true for single-threaded workloads without concurrency. Under concurrent read pressure, a persistent structure that never requires a lock for reads can outperform a mutable structure protected by a `RWMutex` at high reader counts (>8 concurrent readers), because the RWMutex itself becomes a bottleneck.

**Pitfall 5: Deep recursion causing stack overflow on large trees**

The path copying `insert` and `delete` functions are recursive to depth O(log n). For a balanced tree with n=10^6 elements, that is ~20 recursive calls — no concern. But a degenerate tree (all elements inserted in sorted order) has O(n) height and causes a stack overflow for n > ~10000 elements. For production use, either enforce balance (use a persistent AVL or red-black tree) or convert to iterative path copying with an explicit stack.

## Exercises

**Exercise 1 — Verification** (30 min): Count the number of new node allocations per `Insert` call in the Go implementation. Insert elements in random order into a persistent BST of n=100 elements. Confirm that each insert allocates exactly `depth_of_new_node + 1` nodes (the path from root to new leaf, plus the new leaf itself). Compare this to a naive "clone entire tree" approach.

**Exercise 2 — Extension** (2-4h): Implement a persistent red-black tree (or AVL tree) to guarantee O(log n) height and eliminate the stack overflow risk. The key challenge is that tree rotations during rebalancing still require path copying — a left rotation that would normally mutate 3 pointers must instead create 3 new nodes. Verify that the height remains O(log n) after inserting n=10000 elements in sorted order.

**Exercise 3 — From Scratch** (4-8h): Implement a persistent hash map using a Hash Array Mapped Trie (HAMT). The HAMT uses a 32-ary trie indexed by the hash of the key. Each interior node stores a 32-bit bitmap indicating which children are present, with actual children in a compact array (no null slots — this is the space optimization that makes HAMTs competitive with hash tables). This is the structure underlying Clojure's `PersistentHashMap` and Scala's `HashMap`.

**Exercise 4 — Production Scenario** (8-15h): Implement a simplified MVCC key-value store: multiple goroutines can read and write concurrently, each operation sees a consistent snapshot of the database at a specific version. Writers acquire a global write lock to produce the next version; readers snapshot the current root without locking. Implement a garbage collection routine that removes versions older than the oldest active reader's snapshot. Benchmark against a `sync.RWMutex`-protected map at 1, 4, and 16 concurrent readers with 1 writer.

## Further Reading

### Foundational Papers
- Driscoll, J. R., Sarnak, N., Sleator, D. D., & Tarjan, R. E. (1989). "Making Data Structures Persistent." *Journal of Computer and System Sciences*, 38(1), 86–124. The defining paper; introduces fat node, node copying, and path copying with amortized analysis.
- Okasaki, C. (1998). *Purely Functional Data Structures*. Cambridge University Press. The canonical reference for persistent data structures; covers persistent queues, heaps, and trees with full complexity analysis.
- Bagwell, P. (2001). "Ideal Hash Trees." École Polytechnique Fédérale de Lausanne. Introduces the HAMT structure used by Clojure's persistent collections.

### Books
- Okasaki, C. (1998). *Purely Functional Data Structures*. Cambridge University Press. Read Chapter 2 (persistence and amortization), Chapter 4 (lazy evaluation for persistence), and Chapter 10 (data structural bootstrapping).
- Pfenning, F. (2004). *Lecture Notes on Persistence*. CMU 15-312. The clearest derivation of path copying complexity.

### Production Code to Read
- `postgres/src/backend/storage/heap/heapam.c` (https://github.com/postgres/postgres) — `heap_insert`, `heap_update`, `heap_delete` for the MVCC row versioning. The `xmin`/`xmax` fields in `HeapTupleHeader` are the version identifiers.
- `clojure/lang/PersistentVector.java` (https://github.com/clojure/clojure) — The 32-ary trie implementation with path copying and tail optimization.
- `golang/go/src/cmd/compile/internal/ssa/` (https://github.com/golang/go) — The Go compiler's SSA representation; each SSA value is an immutable node in the persistent value graph.

### Conference Talks
- Hickey, R. (JVM Language Summit 2009): "Are We There Yet?" — Rich Hickey's talk introducing Clojure's persistent data structures and the motivation for immutability in concurrent systems.
- Ramachandra, H. (PGConf 2019): "Understanding PostgreSQL MVCC" — detailed walkthrough of the tuple versioning mechanism.
