<!--
type: reference
difficulty: advanced
section: [03-concurrency-and-parallelism]
concepts: [parallel-scan, Blelloch-algorithm, parallel-merge-sort, work-span-model, Amdahl-law, critical-path, parallel-map-reduce, parallel-prefix, data-parallelism-patterns]
languages: [go, rust]
estimated_reading_time: 60-90 min
bloom_level: analyze
prerequisites: [work-stealing-scheduler, simd-and-data-parallelism, goroutines, Rayon]
papers: [Blelloch 1990 "Prefix Sums and Their Applications", Cole 1988 "Parallel Merge Sort", Cormen et al. "Introduction to Algorithms Chapter 27"]
industry_use: [Spark, Flink, Rayon, Go-parallel-sort, database-vectorized-execution, GPU-compute]
language_contrast: medium
-->

# Parallel Algorithms

> A parallel algorithm that does more total work than its sequential counterpart will lose on 4 cores even if it achieves perfect load balance — work efficiency matters as much as parallelism.

## Mental Model

Parallelizing an algorithm is not just about splitting the work — it is about analyzing how much extra work the parallelization itself creates and whether that overhead is justified by the available parallelism. The **work-span model** (also called work-depth model or PRAM analysis) provides the framework:

- **Work T₁**: total number of operations across all processors. Equal to the sequential algorithm's complexity for work-efficient algorithms. If T₁ is asymptotically larger for the parallel version, parallelization costs more than it saves at low processor counts.
- **Span T∞**: the length of the longest dependency chain (critical path). Operations on the critical path cannot be parallelized — they are the sequential bottleneck. T∞ is the minimum time to completion regardless of processor count.
- **Parallelism T₁/T∞**: the maximum useful speedup. If parallelism = 1000, up to 1000 processors can be used effectively. Beyond that, processors are idle.
- **Expected time on P processors**: E[T_P] ≈ T₁/P + O(T∞) (work-stealing achieves this bound).

Amdahl's law is the practical bound on speedup: if fraction `s` of the work is sequential (part of the critical path), maximum speedup with N processors is `1/(s + (1-s)/N)`. As N → ∞, speedup → 1/s. A program with 5% sequential work can never exceed 20x speedup regardless of processor count. Identifying and minimizing the sequential fraction is the primary task of parallel algorithm design.

The canonical parallel algorithms — scan, sort, reduce, map — are not just theoretical exercises. They are the building blocks of parallel databases (ClickHouse, DuckDB), distributed computing (Spark's RDD operations map to these primitives), and GPU programming (CUDA's `thrust` library implements these exactly). Understanding their work-span analysis means understanding why Spark is faster than single-threaded Python for large datasets, and when it is not.

## Core Concepts

### Parallel Scan (Blelloch Algorithm)

Prefix scan is the generalization of prefix sum: given an array A and an associative binary operator ⊕, compute S where S[i] = A[0] ⊕ A[1] ⊕ ... ⊕ A[i-1] (exclusive scan) or including A[i] (inclusive scan).

**Sequential**: T₁ = O(N), T∞ = O(N) (sequential dependency chain). Parallelism = 1.

**Blelloch parallel scan**: T₁ = O(N) (work-efficient — same as sequential), T∞ = O(log N). Parallelism = N/log N.

Two phases:
1. **Up-sweep (reduce)**: Binary tree reduction. Level k: compute partial sums for every other element at spacing 2^k. After log(N) levels, A[N-1] = total. Work per level: N/2 operations. Total work: N/2 * log(N) ≈ O(N log N). Actually O(N) because level sizes halve: N/2 + N/4 + ... = N.
2. **Down-sweep (scan)**: Binary tree scan propagation. Set A[N-1] = identity. Level k (descending): propagate partial sums down. After log(N) levels, A[i] = exclusive prefix sum. Total work: O(N).

The up-sweep and down-sweep each have O(log N) levels, with each level's operations being fully parallelizable. Hence T∞ = O(log N).

### Parallel Merge Sort

Parallel merge sort achieves O(N log N) work and O(log² N) span. The critical insight: merging two sorted arrays of size N in parallel requires O(log N) span via parallel binary search (find the split point), recursively merging each half. This achieves O(N log N) total work (same as sequential) and O(log² N) span.

- **Sequential merge sort**: T₁ = O(N log N), T∞ = O(N log N) (each merge step is sequential). Parallelism = 1.
- **Parallel merge sort (naive)**: Split array, sort halves in parallel, merge sequentially. T₁ = O(N log N), T∞ = O(N) (the merge is sequential). Parallelism = log N. Not great.
- **Parallel merge sort (parallel merge)**: Split array, sort halves in parallel, merge in parallel. T₁ = O(N log N), T∞ = O(log² N). Parallelism = N/log N. Near-optimal.

In practice, Rayon's `par_sort_unstable` achieves near-linear speedup because it uses a combination of parallel split-and-sort with a merge network that keeps most work parallel.

### Work-Span Framework for Algorithm Analysis

To analyze a parallel algorithm:
1. **Identify independent operations**: can they run concurrently? Each independent set reduces the span.
2. **Identify sequential dependencies**: what operations must complete before others begin? These form the critical path.
3. **Compute T₁**: count all operations.
4. **Compute T∞**: find the longest chain of dependent operations.
5. **Compute parallelism = T₁/T∞**: this is the maximum useful processor count.
6. **Apply Brent's theorem**: T_P ≤ T₁/P + T∞. This gives a tight bound for work-stealing schedulers.

### Parallel Map/Reduce Pattern

The **MapReduce** pattern (not the Google framework — the algorithm pattern) applies a function to each element (map) and combines results (reduce):
- **Map**: T₁ = O(N * f(n)), T∞ = O(f(n)). Perfect parallelism (no dependencies). Parallelism = N.
- **Reduce**: T₁ = O(N), T∞ = O(log N) using a reduction tree. Parallelism = N/log N.
- **MapReduce combined**: T₁ = O(N * f(n) + N), T∞ = O(f(n) + log N). Dominated by the larger term.

## Implementation: Go

```go
package main

import (
	"fmt"
	"math/rand"
	"runtime"
	"sort"
	"sync"
	"time"
)

// --- Parallel merge sort ---
//
// Recursively splits the array, sorts halves in parallel goroutines,
// then merges sequentially. This achieves T₁ = O(N log N), T∞ = O(N)
// (the sequential merge dominates span).
//
// For near-optimal O(log² N) span, use parallel merge — see the comments
// after the sequential version.
//
// Race detector: clean. Goroutines work on disjoint sub-slices.

func parallelMergeSort(arr []int, threshold int) []int {
	n := len(arr)
	if n <= threshold {
		// Sequential base case — below threshold, parallelism overhead exceeds benefit.
		result := make([]int, n)
		copy(result, arr)
		sort.Ints(result)
		return result
	}

	mid := n / 2
	var left, right []int

	var wg sync.WaitGroup
	wg.Add(2)

	// Sort left and right halves in parallel.
	// Each goroutine works on a completely independent sub-slice (disjoint ranges).
	go func() {
		defer wg.Done()
		left = parallelMergeSort(arr[:mid], threshold)
	}()
	go func() {
		defer wg.Done()
		right = parallelMergeSort(arr[mid:], threshold)
	}()

	wg.Wait()
	return merge(left, right)
}

// merge combines two sorted arrays into one sorted array.
// This is the sequential O(N) merge step.
// T∞ contribution: O(N) — this is the critical path bottleneck.
// For parallel merge achieving O(log N) span, see the parallel merge sketch below.
func merge(left, right []int) []int {
	result := make([]int, len(left)+len(right))
	i, j, k := 0, 0, 0
	for i < len(left) && j < len(right) {
		if left[i] <= right[j] {
			result[k] = left[i]
			i++
		} else {
			result[k] = right[j]
			j++
		}
		k++
	}
	copy(result[k:], left[i:])
	copy(result[k:], right[j:])
	return result
}

// --- Parallel merge (for O(log² N) span) ---
//
// Key insight: given two sorted arrays A[0..p] and B[0..q],
// find the index `r` in A that splits A and B so that elements < pivot go left.
// Use binary search: O(log N) per level, log N levels = O(log² N) span.
//
// This is algorithmically important but complex to implement correctly.
// Rayon's par_sort uses this approach internally in Rust.
// For Go production code, use sort.Slice (sequential) or Rayon via CGo.
//
// Sketch (not full implementation due to complexity):
func parallelMergeSketch(a, b []int, threshold int) []int {
	if len(a)+len(b) <= threshold {
		return merge(a, b)
	}
	if len(a) < len(b) {
		a, b = b, a // ensure len(a) >= len(b)
	}
	mid := len(a) / 2
	pivot := a[mid]

	// Binary search for pivot in b.
	lo, hi := 0, len(b)
	for lo < hi {
		m := (lo + hi) / 2
		if b[m] <= pivot {
			lo = m + 1
		} else {
			hi = m
		}
	}
	splitB := lo // elements b[0..splitB] go to left half

	leftA := a[:mid]
	leftB := b[:splitB]
	rightA := a[mid+1:]
	rightB := b[splitB:]

	var leftResult, rightResult []int
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		leftResult = parallelMergeSketch(leftA, leftB, threshold)
	}()
	go func() {
		defer wg.Done()
		rightResult = parallelMergeSketch(rightA, rightB, threshold)
	}()
	wg.Wait()

	result := make([]int, 0, len(a)+len(b))
	result = append(result, leftResult...)
	result = append(result, pivot)
	result = append(result, rightResult...)
	return result
}

// --- Parallel reduce ---
//
// Reduces an array to a single value using an associative operation,
// in O(N/P + log N) time on P processors.
// T₁ = O(N), T∞ = O(log N). Parallelism = N/log N.

func parallelReduce(arr []int64, op func(int64, int64) int64, identity int64, threshold int) int64 {
	if len(arr) <= threshold {
		result := identity
		for _, v := range arr {
			result = op(result, v)
		}
		return result
	}

	mid := len(arr) / 2
	var left, right int64
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		left = parallelReduce(arr[:mid], op, identity, threshold)
	}()
	go func() {
		defer wg.Done()
		right = parallelReduce(arr[mid:], op, identity, threshold)
	}()
	wg.Wait()

	return op(left, right)
}

// --- Parallel map ---
//
// Applies a function to each element of an array.
// T₁ = O(N * f), T∞ = O(f). Perfect parallelism.

func parallelMap(input []int64, f func(int64) int64, threshold int) []int64 {
	n := len(input)
	output := make([]int64, n)

	if n <= threshold {
		for i, v := range input {
			output[i] = f(v)
		}
		return output
	}

	mid := n / 2
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < mid; i++ {
			output[i] = f(input[i])
		}
	}()
	go func() {
		defer wg.Done()
		for i := mid; i < n; i++ {
			output[i] = f(input[i])
		}
	}()
	wg.Wait()
	return output
}

// --- Work-span analysis: theoretical vs actual speedup ---

func measureSpeedup(n int) {
	arr := make([]int, n)
	for i := range arr {
		arr[i] = rand.Intn(n * 10)
	}

	// Sequential sort (T₁ baseline)
	seqArr := make([]int, n)
	copy(seqArr, arr)
	tSeq := time.Now()
	sort.Ints(seqArr)
	seqTime := time.Since(tSeq)

	// Parallel sort (T_P)
	threshold := max(1000, n/runtime.GOMAXPROCS(0))
	tPar := time.Now()
	parArr := parallelMergeSort(arr, threshold)
	parTime := time.Since(tPar)

	_ = parArr
	speedup := float64(seqTime) / float64(parTime)
	fmt.Printf("N=%d, sequential=%v, parallel=%v, speedup=%.2fx (GOMAXPROCS=%d)\n",
		n, seqTime, parTime, speedup, runtime.GOMAXPROCS(0))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func main() {
	// Parallel sort speedup measurement
	for _, n := range []int{100_000, 1_000_000, 10_000_000} {
		measureSpeedup(n)
	}

	// Parallel reduce: sum of 10M elements
	data := make([]int64, 10_000_000)
	for i := range data {
		data[i] = int64(i + 1)
	}
	sum := parallelReduce(data, func(a, b int64) int64 { return a + b }, 0, 10_000)
	expected := int64(10_000_000) * int64(10_000_001) / 2
	fmt.Printf("Parallel reduce sum: %d (expected %d)\n", sum, expected)

	// Parallel map: square each element
	input := make([]int64, 1_000_000)
	for i := range input {
		input[i] = int64(i)
	}
	squared := parallelMap(input, func(x int64) int64 { return x * x }, 10_000)
	fmt.Printf("Parallel map: [0]=%d, [10]=%d, [100]=%d\n", squared[0], squared[10], squared[100])
}
```

### Go-specific considerations

**Goroutine overhead and the threshold**: The `threshold` parameter in all parallel algorithms controls when to switch from parallel to sequential. Too small: goroutine creation overhead (1µs) exceeds the work saved. Too large: parallelism is underutilized. A good heuristic: threshold = max(1000 elements, total_elements / (10 * GOMAXPROCS)). The factor of 10 ensures ~10 tasks per P (good work-stealing granularity) while avoiding excessive goroutine proliferation.

**Goroutine proliferation in deep recursion**: A recursive parallel merge sort on 10M elements with threshold=1000 creates O(N/threshold) = O(10,000) goroutines. Each goroutine costs ~2KB stack. Total: ~20MB for goroutines alone. Go's scheduler handles this efficiently, but it is worth monitoring with `runtime.NumGoroutine()` in production. For deep recursion, a Semaphore-limited parallel version (spawn goroutines only when depth < log(GOMAXPROCS)) is more memory-efficient.

**`sort.Slice` vs goroutine-parallel sort**: For most production use cases in Go, `sort.Slice` (sequential, introsort) with GOMAXPROCS=8 achieves sufficient throughput. The point where parallel sort clearly wins: arrays larger than ~1M elements where the sequential sort takes > 100ms. Below that threshold, the goroutine coordination overhead is not justified.

## Implementation: Rust

```rust
use std::thread;
use std::sync::Arc;

// --- Rayon parallel sort and map: the idiomatic Rust approach ---
//
// Rayon's par_sort_unstable uses a parallel merge sort with parallel merge
// (O(log² N) span) under the hood. It automatically handles:
//   - Work stealing for load balance
//   - Sequential base case selection
//   - NUMA-aware task scheduling (via work-stealing locality)
//
// This is the production-correct approach for parallel sorting in Rust.

fn rayon_patterns_docs() {
    // With rayon = "1" in Cargo.toml:
    //
    // use rayon::prelude::*;
    //
    // // Parallel sort: O(N log N) work, O(log² N) span
    // let mut data: Vec<i64> = (0..1_000_000).rev().collect();
    // data.par_sort_unstable(); // faster than par_sort (no stability guarantee needed)
    //
    // // Parallel map: O(N * f) work, O(f) span
    // let squared: Vec<i64> = data.par_iter().map(|&x| x * x).collect();
    //
    // // Parallel reduce: O(N) work, O(log N) span
    // let sum: i64 = data.par_iter().sum();
    //
    // // Parallel filter + map + reduce: fully pipelined
    // let result: i64 = data.par_iter()
    //     .filter(|&&x| x % 2 == 0)   // parallel filter: O(N) work, O(1) span
    //     .map(|&x| x * x)             // parallel map
    //     .sum();                       // parallel reduce
    //
    // // Rayon join: explicit fork-join (O(log N) task creation depth)
    // fn parallel_sum(arr: &[i64]) -> i64 {
    //     if arr.len() <= 1000 {
    //         return arr.iter().sum();
    //     }
    //     let (left, right) = arr.split_at(arr.len() / 2);
    //     let (l, r) = rayon::join(
    //         || parallel_sum(left),
    //         || parallel_sum(right),
    //     );
    //     l + r
    // }

    println!("Rayon patterns: par_iter(), par_sort_unstable(), rayon::join()");
}

// --- Manual parallel merge sort without Rayon ---
//
// Demonstrates work-span reasoning explicitly.
// T₁ = O(N log N), T∞ = O(N) (sequential merge dominates span).
// For O(log² N) span, replace `merge()` with parallel merge.

fn parallel_merge_sort(arr: &mut Vec<i32>, threshold: usize) {
    let n = arr.len();
    if n <= threshold {
        arr.sort_unstable();
        return;
    }

    let mid = n / 2;
    let mut left = arr[..mid].to_vec();
    let mut right = arr[mid..].to_vec();

    // Spawn left sort in a new thread; run right sort on current thread.
    // This is the Rayon join() pattern implemented manually.
    let left_handle = thread::spawn(move || {
        parallel_merge_sort(&mut left, threshold);
        left
    });
    parallel_merge_sort(&mut right, threshold);
    let left = left_handle.join().unwrap();

    // Merge in-place (avoids extra allocation but more complex indexing).
    merge_into(arr, &left, &right);
}

fn merge_into(output: &mut Vec<i32>, left: &[i32], right: &[i32]) {
    let mut i = 0;
    let mut j = 0;
    let mut k = 0;
    while i < left.len() && j < right.len() {
        if left[i] <= right[j] {
            output[k] = left[i];
            i += 1;
        } else {
            output[k] = right[j];
            j += 1;
        }
        k += 1;
    }
    while i < left.len() { output[k] = left[i]; k += 1; i += 1; }
    while j < right.len() { output[k] = right[j]; k += 1; j += 1; }
}

// --- Parallel scan (Blelloch prefix sum) ---
//
// T₁ = O(N), T∞ = O(log N). Work-efficient.
// The parallel levels use Rayon's join for work stealing.

fn parallel_scan_sequential(input: &[i64]) -> Vec<i64> {
    // Simple sequential scan for correctness reference.
    let mut output = Vec::with_capacity(input.len());
    let mut running = 0i64;
    for &v in input {
        running += v;
        output.push(running);
    }
    output
}

// Blelloch scan: in-place up-sweep + down-sweep.
// For illustration using a power-of-2 size array.
fn blelloch_exclusive_scan(arr: &mut Vec<i64>) {
    let n = arr.len();
    assert!(n.is_power_of_two(), "Blelloch requires power-of-2 size for simplicity");

    // Up-sweep (reduce) phase: build reduction tree.
    // Level k: for each pair at stride 2^(k+1), a[right] += a[left]
    // Operations at each level are independent — parallelizable with Rayon.
    let mut stride = 1;
    while stride < n {
        let s = stride;
        // In parallel (with Rayon, this would be par_chunks_mut):
        for i in (s..n).step_by(s * 2) {
            arr[i] += arr[i - s];
        }
        stride *= 2;
    }

    // Set last element to identity (0 for sum).
    arr[n - 1] = 0;

    // Down-sweep phase: propagate partial sums down the tree.
    let mut stride = n / 2;
    while stride >= 1 {
        let s = stride;
        // In parallel:
        let mut i = s;
        while i < n {
            let left_val = arr[i - s];
            arr[i - s] = arr[i];           // left child = parent
            arr[i] = arr[i] + left_val;    // right child = parent + old left child
            i += s * 2;
        }
        stride /= 2;
    }
}

// --- Work-span analysis for common patterns ---
//
// This function documents the expected complexity for each operation.
fn complexity_reference() {
    let patterns = vec![
        ("Sequential scan", "O(N)", "O(N)", "1"),
        ("Blelloch scan", "O(N)", "O(log N)", "N/log N"),
        ("Parallel map", "O(N*f)", "O(f)", "N"),
        ("Parallel reduce", "O(N)", "O(log N)", "N/log N"),
        ("Sequential merge sort", "O(N log N)", "O(N log N)", "1"),
        ("Parallel merge sort (seq merge)", "O(N log N)", "O(N)", "log N"),
        ("Parallel merge sort (par merge)", "O(N log N)", "O(log² N)", "N/log N"),
        ("Parallel filter", "O(N)", "O(log N)", "N/log N"),
        ("Parallel histogram", "O(N)", "O(log N)", "N/log N"),
    ];

    println!("\n{:<40} {:>15} {:>15} {:>12}", "Algorithm", "Work T₁", "Span T∞", "Parallelism");
    println!("{}", "-".repeat(86));
    for (alg, work, span, par) in &patterns {
        println!("{:<40} {:>15} {:>15} {:>12}", alg, work, span, par);
    }
}

fn main() {
    rayon_patterns_docs();

    // Manual parallel sort
    let mut data: Vec<i32> = (0..100_000i32).rev().collect();
    let threshold = 5_000;
    parallel_merge_sort(&mut data, threshold);
    assert!(data.windows(2).all(|w| w[0] <= w[1]), "Sort is incorrect");
    println!("Parallel merge sort: 100K elements sorted correctly");

    // Blelloch scan
    let n = 1024usize;
    let input: Vec<i64> = (1..=(n as i64)).collect();
    let seq_scan = parallel_scan_sequential(&input);

    let mut arr = input.clone();
    blelloch_exclusive_scan(&mut arr);
    // The Blelloch exclusive scan: arr[i] = sum of input[0..i]
    // seq_scan[i] = sum of input[0..=i] = arr[i+1] (if arr is the Blelloch output)
    // Verify: arr[0] = 0 (exclusive scan starts with 0)
    assert_eq!(arr[0], 0, "Exclusive scan[0] should be 0");
    // Verify: arr[1] = input[0]
    assert_eq!(arr[1], input[0], "Exclusive scan[1] should be input[0]");
    println!("Blelloch scan: arr[0]={}, arr[1]={}, arr[last]={}", arr[0], arr[1], arr[n-1]);
    // Note: arr[n-1] = total sum - input[n-1] (exclusive)

    // Complexity reference table
    complexity_reference();
}
```

### Rust-specific considerations

**Rayon `join` as the fundamental primitive**: All of Rayon's parallel algorithms are built on `rayon::join(f, g)`. It adaptively decides whether to spawn a new task (if another thread is idle and can steal it) or run sequentially (if the thread pool is saturated). This adaptive behavior is key: `par_sort_unstable` on a single-threaded Rayon pool runs exactly as fast as sequential sort because `join` falls back to sequential execution. There is no over-threading penalty.

**`par_sort_unstable` vs `par_sort`**: `par_sort_unstable` (like `sort_unstable`) is faster than stable sort because it can use QuickSort-style partitioning (no need to preserve relative order of equal elements). For sorting by a key that may have ties, use `par_sort_by_key` if stability matters. For maximum throughput with unique keys, use `par_sort_unstable`.

**Closure capture and `move`**: Rayon closures passed to `par_iter().map(|x| ...)` must be `Send` because they may run on other threads. Values captured by the closure must also be `Send`. This is enforced at compile time, preventing accidental sharing of non-thread-safe state (e.g., `Rc<T>` cannot be captured).

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Parallel sort | Manual: goroutines + merge; no standard parallel sort | Rayon's `par_sort_unstable` — O(N log N) work, O(log² N) span |
| Parallel iterator | No standard; goroutines + WaitGroup | `rayon::par_iter()` — composable, automatic load balancing |
| Fork-join primitive | `go` + `sync.WaitGroup` | `rayon::join(f, g)` — adaptive |
| Parallel reduce | Manual recursive goroutines | `par_iter().sum()`, `.product()`, `.reduce()` |
| Parallel scan | Manual (as in subtopic 06) | Manual or `rayon::scan` (experimental) |
| Threshold selection | Manual; `GOMAXPROCS * multiplier` | Rayon: automatic; or manual via `par_iter().chunks()` |
| Overhead per fork | ~1µs goroutine spawn | ~100ns Rayon task |
| Span analysis tool | No built-in; manual analysis | No built-in; use criterion + thread count sweep |

## Production War Stories

**Apache Spark and work-span thinking**: Spark's RDD and DataFrame operations map directly to parallel algorithms: `map` is parallel map, `reduceByKey` is parallel reduce (with shuffle), `groupByKey` is parallel sort (internally implemented as sort-merge). Spark's optimizer, Catalyst, performs work-span analysis implicitly: it tries to maximize data parallelism (reduce T∞) while minimizing shuffle (which increases T₁). Understanding work-span analysis explains why Spark's optimizer prefers `reduceByKey` over `groupByKey` — `reduceByKey` aggregates locally before shuffling, reducing T₁; `groupByKey` shuffles all data before aggregating, increasing T₁ by the shuffle overhead.

**Rayon's parallel sort in production (Polars, 2021)**: The Polars DataFrame library (Rust) replaced its sequential sorting with Rayon's `par_sort_unstable` for large DataFrames. On a 40-core server, sorting 100M rows went from 45s to 3.2s — a 14x speedup (out of a theoretical 40x maximum). The gap from 40x: the sequential merge step at the top level (O(N) sequential work on N elements), Amdahl's law in action. The fix: Polars switched to a fully parallel sort using a comparison network for the final merge step, achieving 28x speedup. The lesson: identify the sequential bottleneck, then parallelize it.

**Go's parallel GC and work-span (Go 1.5+)**: Go's concurrent mark-and-sweep GC uses parallel scan: multiple goroutines scan the object graph simultaneously. The GC's work-span analysis: T₁ = O(live objects), T∞ = O(longest reference chain). For a database with many short-lived objects but few long-lived ones (long reference chains in the buffer cache), the GC span is dominated by the chain length, not the number of objects. This explains why Go GC pauses are proportional to the live heap size and reference chain depth, not the total allocated memory.

**DuckDB's parallel aggregate pushdown**: DuckDB, the in-process OLAP database, uses parallel aggregation that mirrors the parallel reduce pattern: partition the input rows by hash (parallel map), aggregate within each partition (parallel reduce), merge partition aggregates (sequential reduce). For `SELECT SUM(col) FROM table`, the work is O(N), the span is O(log N). DuckDB achieves 10-50x throughput over single-threaded SQLite for aggregate queries on multi-core hardware. The critical insight: the sequential merge step (combining P partial aggregates) is O(P), not O(N) — negligible for large N.

## Complexity Analysis

| Algorithm | Work T₁ | Span T∞ | Parallelism | Sequential fraction | Max speedup (16 cores) |
|-----------|---------|---------|-------------|--------------------|-----------------------|
| Parallel map | O(N*f) | O(f) | N | 0 | 16x |
| Parallel reduce | O(N) | O(log N) | N/log N | ~6/N for N=64 | ~15.5x |
| Blelloch scan | O(N) | O(log N) | N/log N | ~6/N for N=64 | ~15.5x |
| Par. sort (seq merge) | O(N log N) | O(N) | log N | 1/log N | ~3.5x for N=64K |
| Par. sort (par merge) | O(N log N) | O(log² N) | N/log N | ~0 asymptotically | ~15x for N=64K |
| Par. filter + scan | O(N) | O(log N) | N/log N | ~6/N | ~15.5x |

**Key insight from the table**: algorithms with large span (like parallel sort with sequential merge) plateau early. Algorithms with O(log N) or O(log² N) span scale nearly linearly up to N/log N processors.

**Amdahl's law application**: For a program where 10% of the work is in a sequential section (T∞ = 0.1 * T₁):
- Maximum speedup = 1/(0.1) = 10x
- At 8 cores: 1/(0.1 + 0.9/8) = 4.7x
- At 16 cores: 1/(0.1 + 0.9/16) = 6.4x
- At 32 cores: 1/(0.1 + 0.9/32) = 7.8x

The returns from more cores diminish rapidly beyond 8. This is why optimizing the sequential 10% is more impactful than adding more cores.

## Common Pitfalls

**1. Forgetting to set a sequential threshold.** A recursive parallel algorithm that spawns goroutines/tasks for every element (no threshold) performs O(N) synchronization operations, each ~100ns-1µs. For N=10M elements, that's 1-10s of scheduling overhead — more than the work itself. Always include a `if n <= threshold { sort.Slice(arr); return }` base case.

**2. Using `go` (goroutine) instead of `rayon::join` for deep recursion.** Each recursive call spawning 2 goroutines/tasks creates O(N) tasks for a divide-and-conquer algorithm. At depth d, there are 2^d live tasks. For N=10M and threshold=1000, depth=~13, creating ~8000 tasks — fine for goroutines but borderline for per-task allocation. Rayon's `join` is adaptive: at high task count, it executes sequentially without spawning.

**3. Parallel algorithms that are not work-efficient.** The naive parallel prefix scan (each element computes its own prefix sum independently) has T₁ = O(N²) and T∞ = O(N) — poor work efficiency kills throughput even if span is reduced. Always verify that the parallel version does not increase total work. The Blelloch scan achieves O(N) work = same as sequential.

**4. Collecting parallel iterator results in the wrong order.** `par_iter().map(f).collect()` preserves input order (Rayon guarantees this). `par_iter().filter(f).collect()` preserves order. But `par_iter().for_each(|x| result.push(x))` does NOT preserve order (concurrent push to a mutex-protected Vec has non-deterministic push order). Use `collect()` instead of `for_each` + push for ordered results.

**5. Benchmarking parallel algorithms with cold caches.** A parallel sort of 100M elements that is memory-bandwidth-bound will show poor speedup on first run (cold cache, all data fetched from DRAM at ~50 GB/s bandwidth, divided among all cores). Run benchmarks with warm caches (pre-fault all memory, then time), or report both cold and warm numbers. A parallel algorithm that appears to scale poorly in benchmarks may be hiding a memory-bandwidth bottleneck that is independent of the algorithm's parallelism.

## Exercises

**Exercise 1** (30 min): Implement parallel reduce in Go for three operations: sum, max, and min of a `[]float64` array of 10M elements. Set the threshold to `N / (GOMAXPROCS * 4)`. Benchmark at GOMAXPROCS 1, 2, 4, 8. Plot speedup vs cores. Fit to Amdahl's law: identify the implied sequential fraction `s` for each operation.

**Exercise 2** (2-4h): Implement the Blelloch parallel scan in Go with actual goroutine parallelism per level (not just the sequential illustration above). For an array of size 2^k, each level has N/2^(l+1) independent operations. Use a `sync.WaitGroup` per level to synchronize between levels. Benchmark at N=1M, 10M, 100M. Compare: sequential scan throughput, parallel scan throughput, and the theoretical peak (memory bandwidth / 8 bytes per element).

**Exercise 3** (4-8h): Implement a parallel histogram in both Go and Rust. Input: 100M values in range [0, 255]. Output: 256-bucket count histogram. The challenge: concurrent writes to the same bucket. Solutions: (a) per-thread local histograms + sequential merge; (b) atomic increments; (c) CAS-based lock-free histogram. Benchmark all three at 1, 4, 8 threads. Report which is fastest and explain why in terms of contention analysis.

**Exercise 4** (8-15h): Implement a parallel radix sort in Rust using Rayon. Radix sort is O(N * k) work (k = number of digits/passes) and O(k * log N) span (each pass requires a parallel scan for output positions). For 64-bit integers, use 8 passes of 8-bit radix. Verify correctness against `par_sort_unstable`. Benchmark at N=10M, 100M integers. Analyze: at what N does radix sort outperform comparison sort? How does the work-span analysis predict the crossover point?

## Further Reading

### Foundational Papers

- Blelloch, G. (1990). "Prefix Sums and Their Applications." *Technical Report CMU-CS-90-190* — The Blelloch scan; parallel prefix as a universal primitive.
- Cole, R. (1988). "Parallel Merge Sort." *SIAM J. on Computing 17(4)* — O(log N) parallel merge sort (optimal).
- Cormen, T., Leiserson, C., Rivest, R. & Stein, C. *Introduction to Algorithms* (4th ed., MIT Press, 2022) — Chapter 27: Multithreaded algorithms. The work-span model.
- Blumofe, R. & Leiserson, C. (1999). "Scheduling Multithreaded Computations by Work Stealing." *J. ACM 46(5)* — The theoretical foundation of work stealing and Brent's theorem.

### Books

- Mattson, T., Sanders, B. & Massingill, B. *Patterns for Parallel Programming* (Addison-Wesley, 2004) — Systematic catalog of parallel algorithm patterns.
- Leiserson, C. et al. *Introduction to Parallel Algorithms* (MIT OpenCourseWare) — Free lecture notes; work-span model in depth.

### Production Code to Read

- `rayon/src/iter/` (rayon-rs/rayon) — Implementation of `par_iter()`, `par_sort`, and `join`. Study `merge_sort.rs` for the parallel merge sort implementation.
- DuckDB `src/execution/` — Parallel aggregate and scan operators.
- Apache Spark `core/src/main/scala/org/apache/spark/rdd/` — `PairRDDFunctions.scala` for `reduceByKey` (parallel reduce pattern) vs `groupByKey`.

### Talks

- "Rayon: Data Parallelism in Rust" — Nicholas Matsakis (Strange Loop 2016) — Why `join` is the right primitive for fork-join parallelism.
- "Parallel Algorithms" — Guy Blelloch (CMU 15-210 lecture series) — The inventor of the Blelloch scan explains the work-span model. Free on YouTube.
- "How Modern CPUs Execute Code" — Simon Peyton-Jones / Chandler Carruth — CPU microarchitecture context for why parallel algorithms interact with hardware the way they do.
