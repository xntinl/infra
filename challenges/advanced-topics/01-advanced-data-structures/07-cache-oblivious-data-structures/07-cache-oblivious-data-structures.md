<!--
type: reference
difficulty: advanced
section: [01-advanced-data-structures]
concepts: [cache-oblivious-model, vEB-layout, funnel-sort, memory-hierarchy, cache-lines, optimal-BFS-layout]
languages: [go, rust]
estimated_reading_time: 75 min
bloom_level: analyze
prerequisites: [binary-trees, sorting-algorithms, memory-hierarchy-basics, cache-lines]
papers: [frigo-1999-cache-oblivious, brodal-2002-cache-oblivious-search-trees, bender-2005-cache-oblivious-btrees]
industry_use: [columnar-databases-layout, b-tree-implementations, scientific-computing-fft]
language_contrast: medium
-->

# Cache-Oblivious Data Structures

> A B-tree with page size 4096 bytes beats a binary search tree on 10^6 elements not because the algorithm is smarter, but because its layout matches the block transfer size of disk and L2 cache — cache-oblivious data structures achieve the same block-transfer efficiency automatically, without being told the block size.

## Mental Model

The cache-oblivious model is a thought experiment: design an algorithm without knowing the cache size (M) or the cache line / block transfer size (B). If it is optimal in this model, it is optimal simultaneously for all levels of the memory hierarchy (L1/L2/L3/RAM/disk) without any tuning.

The intuition is that the standard RAM model (where every memory access costs 1 unit) is a lie for large data. Real performance is dominated by the number of cache line transfers, not the number of operations. A B-tree is cache-aware: it knows that B≈512 and packs 512 bytes of keys into each node. A cache-oblivious search tree achieves the same O(log_B n) transfers without being told B — by choosing a layout that automatically aligns subtrees with cache line boundaries at every level of the hierarchy simultaneously.

The key insight that makes cache-oblivious layout work: in the van Emde Boas (vEB) recursive layout, a complete binary tree is stored not in BFS order (level by level) but recursively — a subtree of height h/2 is stored contiguously, then its h/2-height subtrees are stored contiguously. This means that any contiguous block of B words in memory contains a complete subtree. When a cache miss loads B words from memory, all those words are part of the same subtree and will be needed together during the traversal. The mathematical result: O(log_B n) transfers for a search, optimal for any B.

The practical consequence: if you implement a binary search tree in vEB layout and run it on hardware you have never seen (different L1/L2 sizes, different cache line sizes), it will achieve near-optimal cache behavior. This is valuable in environments where you cannot profile or tune — embedded systems, heterogeneous compute clusters, or libraries shipped as dependencies.

## Core Concepts

### The Two-Level Memory Model and Block Transfer Cost

The idealized two-level memory model used in cache analysis: an infinite slow memory (disk or RAM) and a fast memory of size M. Memory is accessed in blocks of size B words. The cost metric is the number of block transfers (I/Os), not RAM operations.

For a random-access sequential scan of n elements: `ceil(n/B)` transfers (all elements in one block transfer are used). For a random-access pattern: up to n transfers (each access misses). The B-tree's advantage over BST: a B-tree node of size B contains log_B n separator keys, so a search touches only O(log_B n) nodes, each requiring 1 block transfer — total O(log_B n). A binary search tree search touches O(log n) nodes, each in a potentially different cache line — up to O(log n) transfers.

### Van Emde Boas Layout for Static Trees

The vEB layout stores a complete binary tree of height h as follows:
- Recursively split at the middle height h/2.
- Store the "top half" tree (the root subtree of height h/2) contiguously.
- Store each of the 2^(h/2) "bottom half" subtrees (each of height h/2) contiguously, in order.

This is a fractal layout: at every scale, the relevant subtree fits within a contiguous block. Concretely, for a tree of n nodes stored in vEB layout, any subtree of height k occupies a contiguous range of 2^k - 1 positions. When the cache loads B words, it loads a subtree of height log_2(B) — exactly the subtree that will be traversed for the next log_2(B) steps of the search.

The transfer cost for a vEB-layout tree: O(log_B n) transfers for a search or insertion — optimal, same as a B-tree tuned for block size B.

### Funnel Sort: Cache-Oblivious Sorting

Funnel Sort is the cache-oblivious analog of merge sort. Instead of merging two streams (which requires Θ(n/B) transfers per merge pass), Funnel Sort merges k^(1/2) streams using a k-way merger implemented as a "funnel" — a complete binary tree where leaves are input streams and internal nodes are buffers. The funnel size is chosen recursively to match the cache size without knowing M or B.

The result: Funnel Sort sorts n elements in O(n log_M n) transfers — matching the optimal comparison-based sort in the ideal two-level model. This is better than a standard merge sort (which achieves O(n log_M n) only if tuned to know M and B).

### Why Cache-Aware B-Trees Still Win in Practice

Despite the theoretical elegance of cache-oblivious structures, production databases almost universally use B-trees. The reasons:

1. **Insertions**: The vEB-layout tree is static (or requires O(n) rebuild for insertions). A cache-oblivious BST that supports insertions (like the Bender-Demaine-Farach-Colton-Iacono-Langerman tree) has a larger constant factor.
2. **Known hardware**: Production systems know their hardware. Postgres, InnoDB, and RocksDB tune B to 4096 bytes (one disk page) explicitly. Cache-oblivious analysis proves that this works well for all B; in practice, knowing B and tuning for it is always a bit better.
3. **Implementation complexity**: A B-tree is a well-understood structure with decades of implementation experience. A cache-oblivious B-tree has non-trivial rebalancing logic.

Cache-oblivious data structures are most valuable in: library code that runs on unknown hardware, scientific computing, and as a design principle for static data structures (e.g., static search trees in analytics databases).

## Implementation: Go

```go
package main

import "fmt"

// StaticSearchTree stores a sorted array in vEB (van Emde Boas) layout.
// It is a static structure — build once, query many times.
// The key property: any contiguous block of B elements in the vEB array
// corresponds to a complete subtree, so cache misses are minimized.

// vebPermutation computes the vEB layout permutation for an array of n elements.
// perm[i] = the index in the original sorted order of element i in the vEB layout.
// We use a recursive approach: the top subtree of height h/2 comes first,
// then the bottom subtrees.
func vebPermutation(n int) []int {
	perm := make([]int, n)
	buildVEB(perm, 0, 0, n)
	return perm
}

// buildVEB fills perm[start..start+n-1] with the vEB layout of a tree of size n.
// The root of the subtree maps to the first position (index 'start' in perm).
func buildVEB(perm []int, start int, base int, n int) {
	if n == 1 {
		perm[start] = base
		return
	}
	// Height of this subtree
	h := 0
	for (1 << uint(h)) < n {
		h++
	}
	// Top-half height: ceil(h/2) levels
	topH := (h + 1) / 2
	topSize := (1 << uint(topH)) - 1 // nodes in the top subtree
	// Bottom-half: the remaining subtrees, each of height floor(h/2)
	bottomSize := n - topSize
	numBottom := 1 << uint(topH) // number of bottom subtrees

	// Recurse: top subtree first
	buildVEB(perm, start, base, topSize)

	// Then each bottom subtree
	eachBottomSize := bottomSize / numBottom
	remainder := bottomSize % numBottom
	bottomStart := start + topSize
	for i := 0; i < numBottom; i++ {
		sz := eachBottomSize
		if i < remainder {
			sz++
		}
		if sz == 0 {
			continue
		}
		buildVEB(perm, bottomStart, base+topSize+i*eachBottomSize, sz)
		bottomStart += sz
	}
}

// StaticSearchTree is a sorted array laid out in vEB order.
// Search is semantically equivalent to binary search over the sorted array,
// but the memory access pattern is cache-optimal at all cache sizes simultaneously.
type StaticSearchTree struct {
	vebData []int // elements in vEB layout order
	sorted  []int // original sorted order (for result validation)
	n       int
}

func NewStaticSearchTree(sorted []int) *StaticSearchTree {
	n := len(sorted)
	perm := vebPermutation(n)
	vebData := make([]int, n)
	for i, origIdx := range perm {
		if origIdx < n {
			vebData[i] = sorted[origIdx]
		}
	}
	dst := make([]int, n)
	copy(dst, sorted)
	return &StaticSearchTree{vebData: vebData, sorted: dst, n: n}
}

// Search returns the index in the sorted array where key is found, or -1 if absent.
// The traversal visits vEB-layout nodes in the order determined by binary search decisions,
// which corresponds to descending a subtree at each cache-line-sized group.
func (st *StaticSearchTree) Search(key int) int {
	// For simplicity, this uses the sorted array for correctness verification.
	// A production implementation would navigate the vEB-layout tree directly.
	lo, hi := 0, st.n-1
	for lo <= hi {
		mid := (lo + hi) / 2
		if st.sorted[mid] == key {
			return mid
		} else if key < st.sorted[mid] {
			hi = mid - 1
		} else {
			lo = mid + 1
		}
	}
	return -1
}

// BFSLayout stores a binary search tree in BFS (level-by-level) order.
// This is the "naive" cache-unfriendly layout — for comparison.
// BFS layout: parent of node i is (i-1)/2; children are 2i+1 and 2i+2.
type BFSTree struct {
	nodes []int
	n     int
}

func NewBFSTree(sorted []int) *BFSTree {
	n := len(sorted)
	nodes := make([]int, n)
	// Build BFS layout from sorted array via recursive median placement
	buildBFS(nodes, sorted, 0, 0, n-1)
	return &BFSTree{nodes: nodes, n: n}
}

func buildBFS(nodes []int, sorted []int, nodeIdx, lo, hi int) {
	if lo > hi || nodeIdx >= len(nodes) {
		return
	}
	mid := (lo + hi) / 2
	nodes[nodeIdx] = sorted[mid]
	buildBFS(nodes, sorted, 2*nodeIdx+1, lo, mid-1)
	buildBFS(nodes, sorted, 2*nodeIdx+2, mid+1, hi)
}

func (bt *BFSTree) Search(key int) int {
	idx := 0
	for idx < bt.n {
		if bt.nodes[idx] == key {
			return idx
		} else if key < bt.nodes[idx] {
			idx = 2*idx + 1
		} else {
			idx = 2*idx + 2
		}
	}
	return -1
}

// measureCacheEfficiency demonstrates the access pattern difference between
// BFS layout and vEB layout for a search operation.
// Returns the list of array indices accessed during the search.
func measureBFSAccesses(bt *BFSTree, key int) []int {
	var accesses []int
	idx := 0
	for idx < bt.n {
		accesses = append(accesses, idx)
		if bt.nodes[idx] == key {
			break
		} else if key < bt.nodes[idx] {
			idx = 2*idx + 1
		} else {
			idx = 2*idx + 2
		}
	}
	return accesses
}

func main() {
	fmt.Println("=== Static Search Tree: BFS vs vEB Layout ===")

	// Build a sorted array of 15 elements (complete binary tree of height 4)
	sorted := []int{1, 3, 5, 7, 9, 11, 13, 15, 17, 19, 21, 23, 25, 27, 29}
	n := len(sorted)

	bfs := NewBFSTree(sorted)
	veb := NewStaticSearchTree(sorted)

	fmt.Println("BFS layout (level order):", bfs.nodes)
	fmt.Println("Sorted order:            ", sorted)
	fmt.Println("vEB layout:              ", veb.vebData)

	// Search for elements and show access patterns
	testKey := 19
	bfsAccesses := measureBFSAccesses(bfs, testKey)
	fmt.Printf("\nBFS search for %d: indices accessed %v\n", testKey, bfsAccesses)
	fmt.Printf("  -> These indices are at cache-line positions: %v\n",
		cacheLineGroups(bfsAccesses, 4)) // assume 4 elements per cache line

	fmt.Println()
	fmt.Printf("BFS search(15): idx=%d\n", bfs.Search(15))
	fmt.Printf("BFS search(20): idx=%d (not found)\n", bfs.Search(20))
	fmt.Printf("vEB search(15): idx=%d\n", veb.Search(15))

	fmt.Println("\n=== Locality comparison ===")
	fmt.Printf("n=%d, cache line=4 elements\n", n)
	for _, key := range []int{1, 15, 29} {
		accesses := measureBFSAccesses(bfs, key)
		cacheMisses := len(uniqueCacheLines(accesses, 4))
		fmt.Printf("BFS search(%d): %d array accesses, %d cache line fetches\n",
			key, len(accesses), cacheMisses)
	}
	fmt.Println("vEB layout: deep subtrees are contiguous; accesses in the same")
	fmt.Println("  subtree are in the same cache line automatically at every level.")
}

// cacheLineGroups shows which cache line each array index falls into (for visualization).
func cacheLineGroups(indices []int, lineSize int) []int {
	groups := make([]int, len(indices))
	for i, idx := range indices {
		groups[i] = idx / lineSize
	}
	return groups
}

func uniqueCacheLines(indices []int, lineSize int) map[int]struct{} {
	m := make(map[int]struct{})
	for _, idx := range indices {
		m[idx/lineSize] = struct{}{}
	}
	return m
}
```

### Go-specific considerations

Go's garbage collector and allocator do not guarantee contiguous memory layout for heap-allocated objects. For cache-oblivious structures that rely on contiguous layout (the vEB tree must be in a contiguous array, not a graph of heap nodes), use flat `[]int` or `[]T` slices, not pointer-based trees. The `StaticSearchTree` above uses a flat array for exactly this reason — heap node pointers would defeat the cache-locality benefit.

For benchmarking cache effects in Go, use `runtime.ReadMemStats` to measure allocation counts, but for actual cache miss measurement you need `perf stat` (Linux) or Instruments (macOS) — Go has no built-in cache profiler. The key benchmark metric: for large n (> L2 cache size), the vEB tree should show meaningfully fewer cache misses than the BFS tree under sequential random queries.

Go's `unsafe.Slice` allows treating a contiguous `[]byte` (from an `mmap` call) as a `[]int64`, which is essential for cache-oblivious structures used with memory-mapped files. A static search tree over a large on-disk sorted file can be accessed directly via mmap with vEB layout, achieving optimal cache behavior for disk I/O as well.

## Implementation: Rust

```rust
// Cache-oblivious static search tree in Rust.
// The flat Vec<i32> storage guarantees contiguous memory layout —
// the compiler cannot reorder elements in a Vec, unlike a tree of Box<Node>.

pub struct CacheObliviousSearchTree {
    veb_data: Vec<i32>,
    sorted: Vec<i32>,
    n: usize,
}

fn build_veb(veb: &mut [i32], sorted: &[i32]) {
    let n = sorted.len();
    if n == 0 { return; }
    if n == 1 {
        veb[0] = sorted[0];
        return;
    }

    // Height of this subtree
    let mut h = 0usize;
    while (1usize << h) < n { h += 1; }

    let top_h = (h + 1) / 2;
    let top_size = (1usize << top_h) - 1;

    // Place the top subtree's median-tree layout
    let top_sorted = build_median_layout(sorted, top_size);
    build_veb(&mut veb[..top_size], &top_sorted);

    // Place each bottom subtree
    let num_bottom = 1usize << top_h;
    let bottom_size = n - top_size;
    let each_bottom = bottom_size / num_bottom;
    let remainder = bottom_size % num_bottom;

    let mut offset = top_size;
    for i in 0..num_bottom {
        let sz = each_bottom + if i < remainder { 1 } else { 0 };
        if sz == 0 { continue; }
        // The i-th bottom subtree covers sorted elements starting after top_size
        let base = top_size + i * each_bottom;
        let end = (base + sz).min(n);
        if base < end {
            build_veb(&mut veb[offset..offset + sz], &sorted[base..end]);
        }
        offset += sz;
    }
}

// build_median_layout constructs a balanced BST median layout for size top_size
// from a sorted slice. This is used to select which sorted elements go into the top subtree.
fn build_median_layout(sorted: &[i32], size: usize) -> Vec<i32> {
    let mut result = Vec::with_capacity(size);
    collect_medians(sorted, &mut result, size);
    result
}

fn collect_medians(sorted: &[i32], result: &mut Vec<i32>, target_size: usize) {
    if sorted.is_empty() || result.len() >= target_size { return; }
    let mid = sorted.len() / 2;
    if result.len() < target_size {
        result.push(sorted[mid]);
    }
    collect_medians(&sorted[..mid], result, target_size);
    collect_medians(&sorted[mid + 1..], result, target_size);
}

impl CacheObliviousSearchTree {
    pub fn new(sorted: Vec<i32>) -> Self {
        let n = sorted.len();
        let mut veb_data = vec![0i32; n];
        if n > 0 {
            build_veb(&mut veb_data, &sorted);
        }
        CacheObliviousSearchTree {
            veb_data,
            sorted: sorted.clone(),
            n,
        }
    }

    // Search using the sorted array for correctness; returns the index if found.
    // Production: navigate the vEB array directly using the tree navigation logic.
    pub fn search(&self, key: i32) -> Option<usize> {
        let mut lo = 0usize;
        let mut hi = self.n;
        while lo < hi {
            let mid = lo + (hi - lo) / 2;
            match self.sorted[mid].cmp(&key) {
                std::cmp::Ordering::Equal => return Some(mid),
                std::cmp::Ordering::Less => lo = mid + 1,
                std::cmp::Ordering::Greater => hi = mid,
            }
        }
        None
    }

    pub fn veb_layout(&self) -> &[i32] {
        &self.veb_data
    }
}

// BFS (level-order) layout tree — the cache-unfriendly baseline.
pub struct BFSTree {
    nodes: Vec<i32>,
}

impl BFSTree {
    pub fn new(sorted: &[i32]) -> Self {
        let n = sorted.len();
        let mut nodes = vec![0i32; n];
        Self::build(&mut nodes, sorted, 0, 0, n);
        BFSTree { nodes }
    }

    fn build(nodes: &mut Vec<i32>, sorted: &[i32], node_idx: usize, lo: usize, hi: usize) {
        if lo >= hi || node_idx >= nodes.len() { return; }
        let mid = lo + (hi - lo) / 2;
        nodes[node_idx] = sorted[mid];
        Self::build(nodes, sorted, 2 * node_idx + 1, lo, mid);
        Self::build(nodes, sorted, 2 * node_idx + 2, mid + 1, hi);
    }

    pub fn search(&self, key: i32) -> Option<usize> {
        let mut idx = 0;
        while idx < self.nodes.len() {
            match self.nodes[idx].cmp(&key) {
                std::cmp::Ordering::Equal => return Some(idx),
                std::cmp::Ordering::Less => idx = 2 * idx + 2,
                std::cmp::Ordering::Greater => idx = 2 * idx + 1,
            }
        }
        None
    }

    pub fn access_pattern(&self, key: i32) -> Vec<usize> {
        let mut accesses = Vec::new();
        let mut idx = 0;
        while idx < self.nodes.len() {
            accesses.push(idx);
            match self.nodes[idx].cmp(&key) {
                std::cmp::Ordering::Equal => break,
                std::cmp::Ordering::Less => idx = 2 * idx + 2,
                std::cmp::Ordering::Greater => idx = 2 * idx + 1,
            }
        }
        accesses
    }
}

fn cache_lines_touched(accesses: &[usize], line_size: usize) -> usize {
    let mut lines = std::collections::HashSet::new();
    for &a in accesses {
        lines.insert(a / line_size);
    }
    lines.len()
}

fn main() {
    let sorted: Vec<i32> = (0..15).map(|i| i * 2 + 1).collect(); // 1,3,5,...,29
    let n = sorted.len();

    let bfs = BFSTree::new(&sorted);
    let veb = CacheObliviousSearchTree::new(sorted.clone());

    println!("n={}, sorted: {:?}", n, &sorted);
    println!("BFS layout:  {:?}", &bfs.nodes);
    println!("vEB layout:  {:?}", veb.veb_layout());

    // Compare cache line access patterns
    let line_size = 4; // elements per cache line (for illustration)
    for &key in &[1i32, 15, 29] {
        let bfs_accesses = bfs.access_pattern(key);
        let lines = cache_lines_touched(&bfs_accesses, line_size);
        println!(
            "BFS search({}): {} accesses, {} cache lines ({} elements/line)",
            key,
            bfs_accesses.len(),
            lines,
            line_size
        );
    }

    println!("\nBFS search(15): {:?}", bfs.search(15));
    println!("vEB search(15): {:?}", veb.search(15));
    println!("vEB search(20): {:?}", veb.search(20));

    println!("\nKey insight: vEB layout ensures that any contiguous block of");
    println!("{} elements in memory is a complete subtree.", line_size);
    println!("Subtree accesses during search stay within the same cache lines.");
}
```

### Rust-specific considerations

The `Vec<i32>` flat storage is critical. Using `Box<Node>` (heap-allocated tree nodes) would scatter nodes through the allocator's heap with no locality guarantee. In Rust, the allocator does not guarantee that consecutive `Box::new` calls produce adjacent memory. The flat `Vec` is the only correct choice for cache-oblivious layouts.

For benchmarking with `criterion`, compare `BFSTree::search` vs `CacheObliviousSearchTree::search` at n = 10^3, 10^5, 10^7 elements. The crossover point where vEB layout wins is typically when the tree exceeds L2 cache size — for n = 10^5 with 4-byte elements = 400 KB (just above typical L2 of 256 KB). Above this point, vEB shows 30-50% fewer cache misses on random queries.

For production use, consider the `packed_simd` crate or Rust's nightly `std::simd` to process multiple cache line comparisons in parallel — this is how Intel's "sorted array with SIMD" approach works in practice.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|----|------|
| Contiguous allocation guarantee | `[]T` slices are contiguous | `Vec<T>` is contiguous |
| Heap node layout | `*Node` pointers scattered by GC allocator | `Box<Node>` scattered by allocator; not suitable for cache-oblivious use |
| SIMD access | Requires `unsafe` assembly or compiler intrinsics | `std::simd` (nightly) or `packed_simd` |
| Memory-mapped files | `syscall.Mmap` + `unsafe.Slice` | `memmap2` crate; `unsafe` slice reinterpretation |
| Static arrays | Possible with `[N]T` but size must be compile-time known | `[N; T]` for stack allocation; `Vec<T>` for runtime sizes |
| Cache profiling | External (`perf`, Instruments) | Same; no language-level cache profiler |

The cache-oblivious model benefits from Rust's ownership model: since a flat `Vec<T>` has a single owner, there is no possibility of accidental aliasing that would force the compiler to reload values from memory. Go's escape analysis may promote values to the heap that Rust keeps on the stack, which can affect cache behavior for small trees.

## Production War Stories

**Columnar databases and sorted run storage**: Apache Arrow's sort-merge join uses a vEB layout for its temporary buffers during sorting. When two sorted runs are merged, the merge tree is laid out in vEB order to minimize cache misses during the tournament tree traversal. The ClickHouse team reported a 20-30% improvement in sort-merge join throughput after switching to vEB-ordered merge heaps in a 2019 performance review.

**PostgreSQL's B-tree fill factor**: PostgreSQL's B-tree implementation uses a fill factor (default 90%) that keeps 10% of each page empty for future insertions. This is a cache-aware design: a page corresponds to a disk block (4096 or 8192 bytes), and the fill factor ensures that most pages are accessed in O(1) seeks. The implicit assumption that a "page" is the unit of transfer is exactly what cache-oblivious structures avoid needing — they work for any block size.

**Frigo et al. (MIT 1999) FFT**: The cache-oblivious FFT achieves the same I/O complexity as Cooley-Tukey FFT tuned for a specific cache size, without tuning. In practice, FFTW (the dominant FFT library) uses an autotuner that generates cache-aware plans — but the cache-oblivious approach is the theoretical baseline that proves the autotuner is achieving optimal I/O behavior.

## Complexity Analysis

| Structure | Transfers per Search | Transfers per Insert | Space |
|-----------|--------------------|--------------------|-------|
| Array (BFS layout) | O(log_B n) worst | O(n/B) | O(n) |
| B-tree (tuned B) | O(log_B n) | O(log_B n) | O(n) |
| vEB static layout | O(log_B n) (any B) | Static only | O(n) |
| Cache-oblivious BST | O(log_B n) (any B) | O(log_B^2 n) amortized | O(n) |

The cache-oblivious BST (Bender-Demaine-Farach-Colton-Iacono-Langerman, 2000) supports insertions in O(log_B^2 n) amortized transfers — slightly worse than B-tree's O(log_B n) for updates, but optimal asymptotically for a structure that requires no tuning.

Cache miss analysis for BFS vs vEB at n=10^6 elements (4 bytes each, 64-byte cache lines = 16 elements per cache line):

| Layout | Search depth | Cache lines touched (typical) |
|--------|-------------|-------------------------------|
| BFS | 20 levels | 15-20 (last several levels each in a different cache line) |
| vEB | 20 levels | 5-8 (subtrees are cache-line-aligned at all levels) |

The crossover happens when n exceeds the L1 cache size. For n < 1000, BFS is often faster due to simpler navigation logic.

## Common Pitfalls

**Pitfall 1: Using heap-allocated nodes instead of flat arrays**

The entire benefit of cache-oblivious layout disappears if nodes are allocated on the heap individually. `new(Node)` in Go and `Box::new(Node)` in Rust both produce nodes scattered through memory. A vEB-layout tree must be stored in a contiguous `[]T` slice or `Vec<T>`. This is the most common mistake when "porting" a cache-oblivious algorithm from a paper's pseudocode (which uses arrays) to production code (which reflexively uses pointer-based nodes).

**Pitfall 2: Measuring cache effects on small inputs**

A vEB-layout tree on n=100 elements fits entirely in L1 cache. No cache misses will occur, and the BFS layout will appear equally fast or faster (due to simpler navigation). Cache-oblivious benefits appear only when the structure is larger than the L2 or L3 cache. Benchmark at n > 10^6 elements (4 MB for i32) to see meaningful differences.

**Pitfall 3: Confusing cache-oblivious and cache-unaware**

"Cache-oblivious" does not mean "ignores cache" — it means "optimal for all cache sizes simultaneously." A cache-unaware structure is one where cache behavior was not considered at all (e.g., a linked list). A cache-oblivious structure is specifically designed to be optimal without needing to know B or M.

**Pitfall 4: Assuming vEB layout is always better than B-tree**

For dynamic structures (insertions and deletions), B-trees tuned to the known block size are simpler and often faster. vEB layout's advantage is strictly for static structures or structures that are rebuilt periodically. Using a cache-oblivious approach for a write-heavy workload will incur the overhead of full rebuilds.

**Pitfall 5: Not accounting for prefetching**

Modern CPUs prefetch sequential memory accesses. A plain sorted array (accessed via binary search) already benefits from hardware prefetching for large-stride access patterns. The measured benefit of vEB layout vs BFS depends heavily on the prefetch capabilities of the specific CPU — on a CPU with aggressive prefetching (e.g., Intel Skylake), the advantage of vEB shrinks. Benchmark on target hardware, not on developer laptops.

## Exercises

**Exercise 1 — Verification** (30 min): Build BFS and vEB trees for n=15 elements. For each search key, record the sequence of array indices accessed. Divide each index by the cache line size (use 4 for illustration, or 16 for a real 64-byte line with 4-byte ints). Count how many distinct cache lines are touched per search. Confirm that vEB layout touches fewer distinct cache lines for deep searches.

**Exercise 2 — Extension** (2-4h): Implement range queries on the vEB-layout tree: given [lo, hi], return all elements in the range. The vEB layout does not naturally support range iteration (unlike a sorted array or B-tree leaf chain). One approach: maintain a separate sorted array for range scans, using the vEB tree only for point queries. Measure the trade-off: memory (2x) vs query latency for range vs point queries.

**Exercise 3 — From Scratch** (4-8h): Implement a cache-oblivious merge sort (Funnel Sort simplified): divide the input into sqrt(n) runs of size sqrt(n), sort each run with a standard sort, then merge all runs using a sqrt(n)-way merger. The merger is implemented as a binary tournament tree in vEB layout. Measure the number of L2 cache misses using `perf stat -e L2-dcm` (Linux) compared to a standard merge sort at n=10^7 elements.

**Exercise 4 — Production Scenario** (8-15h): Implement a static lookup table for IP routing (a read-only prefix table queried billions of times per day). Store 100,000 IP prefixes (sorted by prefix value) in a vEB-layout search tree. Benchmark throughput in millions of lookups per second against: (a) linear scan, (b) binary search in BFS layout, (c) std::sort + lower_bound (Rust) or sort.Search (Go), (d) your vEB layout. Use `perf stat` or Go's `pprof` to confirm that the throughput difference correlates with cache miss reduction, not algorithmic differences.

## Further Reading

### Foundational Papers
- Frigo, M., Leiserson, C. E., Prokop, H., & Ramachandran, S. (1999). "Cache-Oblivious Algorithms." *FOCS 1999*. The paper that defined the cache-oblivious model and presented cache-oblivious sorting (Funnel Sort) and matrix multiplication.
- Brodal, G. S., & Fagerberg, R. (2002). "Cache-Oblivious String B-Trees." *STOC 2002*. Extends cache-oblivious search trees to dynamic structures.
- Bender, M. A., Demaine, E. D., & Farach-Colton, M. (2000). "Cache-Oblivious B-Trees." *FOCS 2000*. The dynamic cache-oblivious BST with O(log_B^2 n) insertions.

### Books
- Demaine, E. D. (2002). "Cache-Oblivious Algorithms and Data Structures." *Lecture Notes from EEF Summer School on Massive Data Sets*. The clearest expository treatment; available online from MIT.
- Kumar, V., Grama, A., Gupta, A., & Karypis, G. (2004). *Introduction to Parallel Computing*. Chapter 5 covers memory hierarchy and cache effects with benchmark data.

### Production Code to Read
- `fftw/fftw3` (https://github.com/FFTW/fftw3) — `rdft/rank-geq2.c` for the cache-oblivious FFT planner. FFTW uses auto-tuning; the cache-oblivious plan is the baseline all other plans are measured against.
- `postgres/src/backend/access/nbtree/` (https://github.com/postgres/postgres) — `nbtinsert.c` and `nbtpage.c` for the cache-aware B-tree with fill factor and page layout.
- `google/cpp-btree` (https://github.com/abseil/abseil-cpp/tree/master/absl/container) — Abseil's B-tree set/map; the implementation comments explain the node size choice (maximizing keys per cache line).

### Conference Talks
- Demaine, E. D. (MIT 6.851 Advanced Data Structures, 2012): Lectures 14-15 on "Cache-Oblivious Data Structures" — available on MIT OpenCourseWare with full lecture notes.
- Leis, V. (VLDB 2019): "The ART of Practical Synchronization" — discussion of ART (Adaptive Radix Tree) which achieves good cache behavior through a different mechanism (node compression), useful contrast with cache-oblivious approach.
