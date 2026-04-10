<!--
type: reference
difficulty: advanced
section: [01-advanced-data-structures]
concepts: [van-emde-boas-tree, universe-reduction, proto-vEB, O-log-log-U, integer-priority-queue]
languages: [go, rust]
estimated_reading_time: 80 min
bloom_level: analyze
prerequisites: [recursion-divide-conquer, bit-manipulation, hash-tables, segment-trees]
papers: [van-emde-boas-1975-preserving-order, thorup-2003-integer-priority-queues]
industry_use: [network-routing-tables, IP-lookup-forwarding, integer-sorting]
language_contrast: medium
-->

# Van Emde Boas Tree

> Network routers use variants of van Emde Boas trees for IP forwarding table lookups because O(log log U) — where U is 2^32 for IPv4 — means at most 5 operations per lookup regardless of the number of routes in the table, compared to O(log n) for a B-tree that grows with routing table size.

## Mental Model

The key insight of a van Emde Boas (vEB) tree is that it escapes the O(log n) lower bound for comparison-based data structures by exploiting the bounded universe. When your keys are integers in the range [0, U-1], you can do better than O(log n) because the keyspace is finite and well-structured. The vEB tree achieves O(log log U) per operation — for U = 2^32 (all 32-bit integers), this is at most 5 levels of recursion regardless of how many elements are stored.

The mental model for the recursion: split each key's sqrt(U) bits into two halves. The upper half (high bits) is a "cluster index," and the lower half (low bits) is the position within that cluster. This splits a problem on universe U into a "summary" structure on universe sqrt(U) (tracking which clusters are non-empty) plus sqrt(U) "cluster" structures each on universe sqrt(U). The recurrence is `T(U) = T(sqrt(U)) + O(1)`, which solves to `T(U) = O(log log U)`.

The practical implication is that vEB trees are optimal for integer key sets with a bounded universe — exactly what IP addresses are. The IPv4 routing table has at most ~900,000 routes, but keys are 32-bit integers, so a vEB-based lookup takes at most log2(32) = 5 levels. A B-tree on 900,000 routes takes log_512(900,000) ≈ 3.5 levels — comparable! The vEB advantage appears when the universe is large but the table is sparse (few routes), or when you need successor/predecessor queries as part of a lookup algorithm (longest-prefix matching for routing).

The gap between theory and practice is larger for vEB than for any other structure in this section. The naive implementation uses O(U) space (one node per possible key position), which is 4 GB for U=2^32. The practical versions use hash tables instead of arrays for sparsely populated subtrees — this is the "hash vEB" or "proto-vEB" structure. Understanding both versions is necessary to evaluate whether a vEB tree is appropriate for a given production system.

## Core Concepts

### Universe Splitting and the Recurrence

For a universe U = 2^k, define `sqrt_upper(U) = 2^(ceil(k/2))` and `sqrt_lower(U) = 2^(floor(k/2))`. For a key x:

```
high(x) = floor(x / sqrt_lower(U))   — which cluster x belongs to
low(x)  = x mod sqrt_lower(U)         — position within that cluster
index(h, l) = h × sqrt_lower(U) + l  — reconstruct key from cluster and position
```

The vEB tree for universe U stores:
- `min` and `max` (the minimum and maximum stored elements, O(1) access)
- `summary`: a vEB tree for universe sqrt_upper(U) indicating which clusters are non-empty
- `clusters[0..sqrt_upper(U)-1]`: an array of sqrt_upper(U) vEB trees, each for universe sqrt_lower(U)

The min element is never stored in the cluster structure — it is kept at the root. This is the trick that makes `Insert` O(log log U): the new minimum is placed directly at the root without recursing into the cluster structure. The old minimum is then recursively inserted into its cluster — but the cluster subtree's insert is also O(log log U'), giving the same recurrence.

### Insert, Delete, Successor, Predecessor

**Insert(x)**: If the tree is empty, set min=max=x and return. If x < min, swap x with min (the new min stays at root without further recursion). If x > max, update max. Then recursively insert high(x) into summary and low(x) into clusters[high(x)].

**Successor(x)**: If x < min, return min. Look up clusters[high(x)].max — if low(x) < clusters[high(x)].max, successor is in the same cluster: recurse. Otherwise, find the next non-empty cluster via summary.successor(high(x)), then return its minimum.

**Delete(x)**: The most complex operation. If x == min, find the next minimum by looking up summary.min, taking clusters[summary.min].min, and recursing. Similar complexity for max.

### Space Complexity and the Hash vEB

The naive implementation stores `sqrt_upper(U)` cluster pointers even when most clusters are empty. For U=2^32, this requires 2^16 = 65536 pointers at the top level alone, and the recursion allocates O(U) space in the worst case.

The practical solution is the "hash vEB" or "proto-vEB": replace the cluster array with a hash map. Non-empty clusters are represented; empty clusters occupy no space. The space complexity drops from O(U) to O(n) (number of stored elements). The time complexity remains O(log log U) expected (with hash table O(1) expected operations).

This is the variant used in practice — and the one worth implementing for production code. The pure-array vEB tree is a theoretical construct useful for understanding the recurrence but rarely deployed.

## Implementation: Go

```go
package main

import (
	"fmt"
	"math"
)

// vEBTree is a hash-based van Emde Boas tree (proto-vEB) for integer keys in [0, U).
// Space: O(n log log U) — the hash map overhead dominates for sparse trees.
// Time: O(log log U) expected for all operations.
//
// Design: we use a map[int]*vEBTree for clusters rather than a preallocated array.
// This is the proto-vEB design that makes it practical for large universes.

type vEBTree struct {
	u       int             // universe size: this tree handles [0, u)
	min     int             // minimum element (not stored in clusters)
	max     int             // maximum element
	hasMin  bool            // false when tree is empty
	summary *vEBTree        // tracks which clusters are non-empty
	cluster map[int]*vEBTree // sparse cluster storage
}

// universeForLevel computes the universe size for a level below this one.
func upperSqrt(u int) int {
	// ceil(log2(u)/2) bits for the upper half
	bits := int(math.Ceil(math.Log2(float64(u))))
	return 1 << uint((bits+1)/2)
}

func lowerSqrt(u int) int {
	bits := int(math.Ceil(math.Log2(float64(u))))
	return 1 << uint(bits/2)
}

func high(x, u int) int { return x / lowerSqrt(u) }
func low(x, u int) int  { return x % lowerSqrt(u) }
func idx(h, l, u int) int { return h*lowerSqrt(u) + l }

func newVEB(u int) *vEBTree {
	if u < 2 {
		u = 2 // minimum universe size
	}
	return &vEBTree{
		u:       u,
		min:     -1,
		max:     -1,
		hasMin:  false,
		cluster: make(map[int]*vEBTree),
	}
}

func (v *vEBTree) isEmpty() bool { return !v.hasMin }

// getOrCreateCluster lazily allocates a cluster subtree.
func (v *vEBTree) getOrCreateCluster(h int) *vEBTree {
	if c, ok := v.cluster[h]; ok {
		return c
	}
	c := newVEB(lowerSqrt(v.u))
	v.cluster[h] = c
	return c
}

// getSummary lazily allocates the summary subtree.
func (v *vEBTree) getSummary() *vEBTree {
	if v.summary == nil {
		v.summary = newVEB(upperSqrt(v.u))
	}
	return v.summary
}

// Insert adds x to the tree. x must be in [0, v.u).
func (v *vEBTree) Insert(x int) {
	if v.u == 2 {
		// Base case: universe of size 2, just track min/max directly
		if !v.hasMin {
			v.min = x
			v.max = x
			v.hasMin = true
		} else {
			if x < v.min {
				v.min = x
			}
			if x > v.max {
				v.max = x
			}
		}
		return
	}

	if !v.hasMin {
		// Tree was empty: set min and max directly without recursion
		v.min = x
		v.max = x
		v.hasMin = true
		return
	}

	if x == v.min || x == v.max {
		return // already present (set semantics)
	}

	if x < v.min {
		// New element becomes the new minimum; swap and insert old min into cluster
		x, v.min = v.min, x
	}
	if x > v.max {
		v.max = x
	}

	h := high(x, v.u)
	l := low(x, v.u)
	c := v.getOrCreateCluster(h)
	if c.isEmpty() {
		// This cluster was empty: also update summary
		v.getSummary().Insert(h)
	}
	c.Insert(l)
}

// Contains returns true if x is in the tree.
func (v *vEBTree) Contains(x int) bool {
	if v.isEmpty() {
		return false
	}
	if x == v.min || x == v.max {
		return true
	}
	if v.u == 2 {
		return false
	}
	h := high(x, v.u)
	c, ok := v.cluster[h]
	if !ok {
		return false
	}
	return c.Contains(low(x, v.u))
}

// Minimum returns the minimum element and true, or -1 and false if empty.
func (v *vEBTree) Minimum() (int, bool) {
	if v.isEmpty() {
		return -1, false
	}
	return v.min, true
}

// Maximum returns the maximum element and true, or -1 and false if empty.
func (v *vEBTree) Maximum() (int, bool) {
	if v.isEmpty() {
		return -1, false
	}
	return v.max, true
}

// Successor returns the smallest element > x, and true, or -1 and false if none exists.
func (v *vEBTree) Successor(x int) (int, bool) {
	if v.isEmpty() {
		return -1, false
	}
	if v.u == 2 {
		// Base case: universe {0, 1}
		if x == 0 && v.max == 1 {
			return 1, true
		}
		return -1, false
	}
	if x < v.min {
		return v.min, true
	}

	h := high(x, v.u)
	l := low(x, v.u)

	// Check if successor is in the same cluster
	if c, ok := v.cluster[h]; ok {
		if maxInCluster, exists := c.Maximum(); exists && l < maxInCluster {
			succLow, _ := c.Successor(l)
			return idx(h, succLow, v.u), true
		}
	}

	// Successor is in a later cluster: find the next non-empty cluster
	if v.summary == nil {
		return -1, false
	}
	succCluster, exists := v.summary.Successor(h)
	if !exists {
		return -1, false
	}
	minInCluster, _ := v.cluster[succCluster].Minimum()
	return idx(succCluster, minInCluster, v.u), true
}

// Predecessor returns the largest element < x, and true, or -1 and false if none exists.
func (v *vEBTree) Predecessor(x int) (int, bool) {
	if v.isEmpty() {
		return -1, false
	}
	if v.u == 2 {
		if x == 1 && v.min == 0 {
			return 0, true
		}
		return -1, false
	}
	if x > v.max {
		return v.max, true
	}

	h := high(x, v.u)
	l := low(x, v.u)

	if c, ok := v.cluster[h]; ok {
		if minInCluster, exists := c.Minimum(); exists && l > minInCluster {
			predLow, _ := c.Predecessor(l)
			return idx(h, predLow, v.u), true
		}
	}

	if v.summary == nil {
		if x > v.min {
			return v.min, true
		}
		return -1, false
	}

	predCluster, exists := v.summary.Predecessor(h)
	if !exists {
		if x > v.min {
			return v.min, true
		}
		return -1, false
	}
	maxInCluster, _ := v.cluster[predCluster].Maximum()
	return idx(predCluster, maxInCluster, v.u), true
}

func main() {
	// Universe: 16-bit integers [0, 65535]
	veb := newVEB(65536)

	// Insert a sparse set of values
	values := []int{2, 3, 4, 5, 7, 14, 15, 512, 1024, 65534, 65535}
	for _, v := range values {
		veb.Insert(v)
	}

	fmt.Println("=== Van Emde Boas Tree (U=65536) ===")
	min, _ := veb.Minimum()
	max, _ := veb.Maximum()
	fmt.Printf("Min: %d, Max: %d\n", min, max)

	fmt.Printf("Contains(5): %v\n", veb.Contains(5))
	fmt.Printf("Contains(6): %v\n", veb.Contains(6))
	fmt.Printf("Contains(512): %v\n", veb.Contains(512))

	// Walk successors from 0 to 20
	fmt.Print("Successor walk from 0: ")
	x := -1
	for {
		next, ok := veb.Successor(x)
		if !ok || next > 20 {
			break
		}
		fmt.Printf("%d ", next)
		x = next
	}
	fmt.Println()

	// Predecessor query
	if pred, ok := veb.Predecessor(600); ok {
		fmt.Printf("Predecessor(600): %d\n", pred) // should be 512
	}

	// Demonstrate O(log log U) depth: for U=65536=2^16, depth is log2(16)=4
	fmt.Printf("\nFor U=65536: log2(log2(U)) = log2(16) = %d levels\n",
		int(math.Log2(math.Log2(65536))))
	fmt.Println("Each operation visits at most 4 recursive calls, regardless of n")
}
```

### Go-specific considerations

The lazy allocation of cluster subtrees (`getOrCreateCluster`) is critical for practical memory use. Without it, a vEB tree for U=2^16 would pre-allocate the entire tree structure at creation, consuming O(U) space even for a single element. The hash map (`map[int]*vEBTree`) gives O(1) expected access with a small constant overhead per lookup — for n=10^6 elements, this overhead is negligible compared to the O(log log U) factor.

Go's garbage collector handles the recursive tree structure naturally — deleted subtrees are simply unreferenced and collected. In a C++ or Rust implementation, you must manually manage memory for a recursive tree where subtrees can be nil/null.

For a production routing table lookup (the primary use case), the vEB tree would typically be rebuilt rather than updated incrementally, because BGP routing table updates arrive in batches and the cost of rebuilding is amortized. The Go implementation supports incremental updates, but the hash-map overhead makes it slower than an array-based build.

## Implementation: Rust

```rust
use std::collections::HashMap;

// Van Emde Boas tree implemented with HashMap for sparse cluster storage.
// The Rust version demonstrates using Option<Box<VebTree>> for recursive ownership —
// each subtree owns its children, forming a tree of heap-allocated nodes.

struct VebTree {
    u: usize,
    min: Option<usize>,
    max: Option<usize>,
    summary: Option<Box<VebTree>>,
    cluster: HashMap<usize, Box<VebTree>>,
}

fn upper_sqrt(u: usize) -> usize {
    let bits = (usize::BITS - (u - 1).leading_zeros()) as usize;
    1 << ((bits + 1) / 2)
}

fn lower_sqrt(u: usize) -> usize {
    let bits = (usize::BITS - (u - 1).leading_zeros()) as usize;
    1 << (bits / 2)
}

fn high(x: usize, u: usize) -> usize { x / lower_sqrt(u) }
fn low(x: usize, u: usize) -> usize { x % lower_sqrt(u) }
fn idx(h: usize, l: usize, u: usize) -> usize { h * lower_sqrt(u) + l }

impl VebTree {
    fn new(u: usize) -> Self {
        VebTree {
            u: u.max(2),
            min: None,
            max: None,
            summary: None,
            cluster: HashMap::new(),
        }
    }

    fn is_empty(&self) -> bool { self.min.is_none() }

    fn get_or_create_summary(&mut self) -> &mut VebTree {
        let u = upper_sqrt(self.u);
        self.summary.get_or_insert_with(|| Box::new(VebTree::new(u)))
    }

    fn insert(&mut self, x: usize) {
        if self.u == 2 {
            // Base case: track min and max for universe {0, 1}
            self.min = Some(self.min.map_or(x, |m| m.min(x)));
            self.max = Some(self.max.map_or(x, |m| m.max(x)));
            return;
        }

        match self.min {
            None => {
                // Empty tree: set min and max without recursion
                self.min = Some(x);
                self.max = Some(x);
                return;
            }
            Some(m) if x == m => return, // already present
            _ => {}
        }

        // If x becomes the new min, swap x with current min
        let x = if x < self.min.unwrap() {
            let old_min = self.min.unwrap();
            self.min = Some(x);
            old_min
        } else {
            x
        };

        if x > self.max.unwrap_or(0) {
            self.max = Some(x);
        }

        let h = high(x, self.u);
        let l = low(x, self.u);
        let lu = lower_sqrt(self.u);

        let cluster_was_empty = !self.cluster.contains_key(&h)
            || self.cluster[&h].is_empty();

        let cluster = self.cluster
            .entry(h)
            .or_insert_with(|| Box::new(VebTree::new(lu)));
        cluster.insert(l);

        if cluster_was_empty {
            self.get_or_create_summary().insert(h);
        }
    }

    fn contains(&self, x: usize) -> bool {
        match (self.min, self.max) {
            (None, _) => false,
            (Some(m), _) if m == x => true,
            (_, Some(mx)) if mx == x => true,
            _ if self.u == 2 => false,
            _ => self
                .cluster
                .get(&high(x, self.u))
                .map_or(false, |c| c.contains(low(x, self.u))),
        }
    }

    fn minimum(&self) -> Option<usize> { self.min }
    fn maximum(&self) -> Option<usize> { self.max }

    fn successor(&self, x: usize) -> Option<usize> {
        if self.is_empty() { return None; }
        if self.u == 2 {
            return if x == 0 && self.max == Some(1) { Some(1) } else { None };
        }
        if x < self.min? { return self.min; }

        let h = high(x, self.u);
        let l = low(x, self.u);

        // Check same cluster
        if let Some(c) = self.cluster.get(&h) {
            if let Some(max_l) = c.maximum() {
                if l < max_l {
                    return c.successor(l).map(|sl| idx(h, sl, self.u));
                }
            }
        }

        // Find next non-empty cluster
        let succ_cluster = self.summary.as_ref()?.successor(h)?;
        let min_l = self.cluster.get(&succ_cluster)?.minimum()?;
        Some(idx(succ_cluster, min_l, self.u))
    }
}

fn main() {
    let mut veb = VebTree::new(1024);

    let values = vec![0usize, 1, 3, 5, 10, 100, 200, 500, 1023];
    for &v in &values {
        veb.insert(v);
    }

    println!("Min: {:?}", veb.minimum());
    println!("Max: {:?}", veb.maximum());
    println!("Contains(5): {}", veb.contains(5));
    println!("Contains(6): {}", veb.contains(6));

    // Successor walk
    print!("Successors from -1: ");
    let mut x: Option<usize> = None;
    loop {
        let next = match x {
            None => veb.minimum(),
            Some(prev) => veb.successor(prev),
        };
        match next {
            None => break,
            Some(n) => {
                print!("{} ", n);
                x = Some(n);
            }
        }
    }
    println!();

    println!("Successor(5): {:?}", veb.successor(5));   // 10
    println!("Successor(100): {:?}", veb.successor(100)); // 200
}
```

### Rust-specific considerations

The recursive `Box<VebTree>` ownership is the idiomatic way to represent a recursive tree in Rust. Each subtree is owned by its parent and deallocated when the parent drops. There is no need for reference counting (`Rc`) or shared ownership (`Arc`) because the tree has a clear single-owner hierarchy.

The `get_or_insert_with` pattern for lazy cluster allocation is idiomatic and avoids the two-step "check then insert" that would require two separate HashMap lookups. This is particularly important in Rust where the borrow checker would require careful lifetime management for a two-step approach.

For a production routing table, the cluster array would be replaced with a flat array (not a HashMap) because the universe is fixed (U=2^16 for prefix matching on /16 subnets) and the access pattern is performance-critical. The array-based version would pre-allocate all cluster nodes upfront, accepting O(U) space for O(1) cluster access.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|----|------|
| Recursive tree ownership | `*vEBTree` pointers, GC-managed | `Box<VebTree>`, drop-managed |
| Cluster allocation | `map[int]*vEBTree` with `nil` checks | `HashMap<usize, Box<VebTree>>` with `Option` |
| Base case handling | Explicit `if u == 2` returns | Pattern matching on `(min, max)` |
| Memory reclamation | Automatic via GC | Automatic via `Drop` on `Box` |
| Integer overflow | Silent wrapping or panic in overflow mode | `usize` with explicit wrapping operations |
| Production routing use | Typically rebuilt in C/C++ for the routers themselves | More likely to be used via FFI or in embedded contexts |

## Production War Stories

**Network routing (BGP/OSPF forwarding)**: The classic application described by van Emde Boas in the 1975 paper is integer priority queues for network routing — computing shortest paths with integer link weights using Dijkstra's algorithm, where the priority queue operations are O(log log U) instead of O(log n). Modern routing ASICs (FPGAs and custom silicon) use a variant of this structure for hardware-accelerated IP forwarding table lookups. The Cisco IOS and Juniper JunOS forwarding plane implementations use trie-based structures (Patricia tries, which have the same O(depth = address length) guarantee as vEB trees but with less memory overhead for real-world routing tables).

**The Linux kernel's timer wheel** (kernel source: `kernel/time/timer.c`): The Linux kernel timer implementation uses a cascade of time wheels — effectively a bucket-based structure with O(1) amortized operations for timers within a bounded time range. While not a vEB tree, the same universe-splitting idea applies: divide the time range into levels, each responsible for a fraction of the total range. The resulting O(1) amortized insert/delete for the common case (timer fires within the next few seconds) is what makes the Linux timer subsystem scalable to millions of concurrent timers.

**Thorup's integer sorting** (SODA 2002): Mikkel Thorup showed that using vEB-tree-like data structures, you can sort n integers in the range [0, U) in O(n log log U) time — strictly better than comparison-based O(n log n). This is used in specialized database engines that sort integer keys (e.g., integer primary keys in column stores) and in certain network packet schedulers that order packets by integer sequence numbers.

## Complexity Analysis

| Operation | vEB (array) | vEB (hash) | B-tree | Red-Black Tree |
|-----------|-------------|------------|--------|----------------|
| Insert | O(log log U) | O(log log U) expected | O(log n) | O(log n) |
| Delete | O(log log U) | O(log log U) expected | O(log n) | O(log n) |
| Successor | O(log log U) | O(log log U) expected | O(log n) | O(log n) |
| Minimum | O(1) | O(1) | O(log n) | O(log n) |
| Space | O(U) | O(n log log U) | O(n) | O(n) |

For U=2^32 and n=10^6: vEB gives ≤5 levels; a B-tree with page size 512 gives ≤3 levels. The vEB win is larger for U=2^128 (IPv6 addresses: ≤7 levels for vEB vs ~5 levels for a B-tree on 10^6 routes). The space overhead of O(n log log U) for the hash vEB is acceptable: for n=10^6 elements in U=2^32, it is approximately 20×10^6 bytes = 20 MB.

Cache behavior: the hash vEB's access pattern is irregular (HashMap access is a pointer chase). For the array vEB with U=2^16, all cluster arrays fit in L2 cache (65536 pointers = 512 KB), making it significantly faster than the hash variant for lookup-heavy workloads.

## Common Pitfalls

**Pitfall 1: Storing the minimum in the cluster structure**

The vEB tree's O(log log U) time depends on NOT storing the minimum in the cluster. If you store min in the cluster structure alongside other elements, the `Insert` operation must always recurse even for new minimums, breaking the recurrence. The invariant is: `min` is stored at the root node and nowhere else.

Detection: an insert of a new minimum causes O(log log U) recursive cluster inserts instead of O(1). The structure works correctly but is O(log^2 log U) due to double recursion.

**Pitfall 2: Using unsigned integers without careful underflow handling**

The successor/predecessor algorithms require computing `high(x)` and `low(x)` for values including 0 and U-1. With unsigned integers in Go (`uint`) or Rust (`usize`), subtraction underflow causes wrapping. The implementation must use signed integers internally or explicitly handle edge cases at 0 and U-1.

**Pitfall 3: Universe size not being a power of 2**

The `upperSqrt` and `lowerSqrt` functions assume U is a power of 2. For arbitrary U, you must round up to the next power of 2. Failing to do so causes `index(high, low) != original_value` for some inputs, producing subtle corruption.

**Pitfall 4: Not accounting for hash map overhead in tight memory budgets**

The hash vEB has O(n log log U) space but with a hash map constant of ~3-10 entries per stored pointer. For n=10^6 elements, this is 20-60 MB — significant for embedded systems or GPU memory. The array vEB has O(U) space but constant-per-element overhead of 1 pointer. For small U (U ≤ 2^16 = 65536), the array vEB fits in 512 KB and outperforms the hash version on both space and time.

**Pitfall 5: Treating vEB as a drop-in replacement for sorted sets in all cases**

The vEB tree requires integer keys in a bounded universe. Attempting to use it for string keys (by converting to integers via hashing) destroys the structure's correctness — two different strings that hash to the same integer will collide. The structure is specifically and only for integer priority queues with a fixed universe bound.

## Exercises

**Exercise 1 — Verification** (30 min): Instrument the Go vEB tree to count recursive calls per `Successor` operation. Insert n=1000 random integers from U=[0, 65536). Verify that the maximum recursive call count is ≤ log2(log2(65536)) = log2(16) = 4 levels.

**Exercise 2 — Extension** (2-4h): Implement `Delete` for the vEB tree. The delete operation must handle the special case where the deleted element is the minimum: the new minimum must be found by looking up `clusters[summary.min].min`, and the summary must be updated if the cluster becomes empty. Test with a sequence of inserts followed by all-deletes, verifying the tree is empty at the end.

**Exercise 3 — From Scratch** (4-8h): Implement the array-based vEB tree for U=2^16 (no hash map). Pre-allocate all cluster arrays at construction time. Compare memory usage and lookup throughput against the hash-based implementation for n=100, n=1000, n=10000, and n=65000. At what n does the hash version use more memory than the array version?

**Exercise 4 — Production Scenario** (8-15h): Build a simplified IP forwarding table: store (prefix, prefix_length, next_hop) entries and implement longest-prefix matching for IPv4 addresses. Use a vEB tree to index the 32 prefix-length levels (0-32), and for each prefix length use a hash map from prefix to next_hop. Benchmark against a sorted prefix array with binary search. The vEB advantage should appear when querying prefixes in the /8 to /24 range (most BGP routes).

## Further Reading

### Foundational Papers
- van Emde Boas, P., Kaas, R., & Zijlstra, E. (1977). "Design and Implementation of an Efficient Priority Queue." *Mathematical Systems Theory*, 10(1), 99–127. The full paper with the practical implementation details.
- Thorup, M. (2003). "Integer Priority Queues with Decrease Key in Constant Time and the Single Source Shortest Paths Problem." *STOC 2003*. Shows how vEB-tree ideas lead to O(m + n log log n) Dijkstra.
- Willard, D. E. (1983). "Log-Logarithmic Worst-Case Range Queries Are Possible in Space Theta(N)." *Information Processing Letters*, 17(2), 81–84. Proves O(log log n) successor with O(n) space via "y-fast tries."

### Books
- Cormen, T. H., Leiserson, C. E., Rivest, R. L., & Stein, C. (2022). *Introduction to Algorithms* (4th ed.). Chapter 20 covers vEB trees with the full correctness proof.
- Kaplan, H. (2008). *Persistent Data Structures*. Lecture notes; Chapter 4 covers integer data structures including vEB trees.

### Production Code to Read
- `linux/kernel/time/timer.c` (https://github.com/torvalds/linux) — The timer wheel is a practical approximation of hierarchical bucket structures with the same universe-splitting concept.
- `google/or-tools` (https://github.com/google/or-tools) — The routing solver uses integer priority queues with vEB-like behavior for Dijkstra's algorithm in large graphs.

### Conference Talks
- Thorup, M. (SODA 2002): "Randomized Sorting in O(n log log n) Time and Linear Space Using Addition, Shift, and Bit-wise Boolean Operations" — shows the full implications of O(log log U) operations for sorting.
- Demaine, E. (MIT 6.851 Advanced Data Structures, 2012): Lecture 11 on "Integer Data Structures" — available on MIT OpenCourseWare; the clearest explanation of the vEB tree construction.
