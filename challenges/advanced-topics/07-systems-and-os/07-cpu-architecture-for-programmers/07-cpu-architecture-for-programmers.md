<!--
type: reference
difficulty: advanced
section: [07-systems-and-os]
concepts: [out-of-order-execution, branch-prediction, store-buffer, MESI-protocol, cache-coherence, NUMA, false-sharing, memory-ordering, TSO, happens-before]
languages: [go, rust]
estimated_reading_time: 90 min
bloom_level: analyze
prerequisites: [virtual-memory-and-paging, concurrency-basics, atomics]
papers: [Lamport1979-sequential-consistency, Sewell2010-x86-TSO]
industry_use: [Linux-kernel, Go-sync-atomic, Rust-std-sync-atomic, JVM-JIT, Disruptor]
language_contrast: high
-->

# CPU Architecture for Programmers

> The CPU is not a sequential instruction executor — it is a speculative, out-of-order,
> cache-coherent superscalar machine, and writing code that the CPU executes efficiently
> requires understanding how its microarchitecture actually behaves.

## Mental Model

The programmer's model of a CPU — "instructions execute one after another, in order,
each immediately visible to all other cores" — is a carefully constructed fiction. Real
CPUs execute instructions out of order (to hide latency), speculatively (to exploit
predicted branches), and with per-core caches that are not immediately visible to other
cores. The reason this fiction mostly holds is that the CPU goes to great lengths to
maintain the **illusion** of sequential execution for a single thread, and the memory
model (TSO on x86, weaker on ARM) defines exactly which reorderings are visible to other
threads.

**Out-of-order execution (OoO)** means the CPU executes instructions in dataflow order
(when their inputs are ready), not program order. The Reorder Buffer (ROB) tracks in-
flight instructions and retires them in program order, committing results to the
architectural state only after they are no longer speculative. A cache miss on a load
instruction does not stall the CPU — other independent instructions continue executing
while the cache miss is resolved, potentially 200+ instructions ahead.

**Branch prediction** allows the CPU to speculatively execute down a predicted path before
the branch condition is known. The Branch Target Buffer (BTB) records recent branch
targets; the TAGE predictor (tournament predictor with geometric history lengths) achieves
~97% accuracy on typical code. When a branch mispredicts, the CPU must flush the pipeline
(discard ~15–20 speculative instructions) and restart from the correct path. This costs
~15 cycles on modern CPUs — the reason tight inner loops with unpredictable branches
underperform loops with predictable ones.

**The store buffer** is the structure that makes x86-64's memory model weaker than
sequential consistency. When a core writes a value, the write goes into the store buffer
(a few-entry FIFO queue between the execution unit and the L1 cache). The write is visible
to the writing core immediately (via store-to-load forwarding) but visible to other cores
only after the store buffer is flushed into the L1 cache. This means on x86-64, stores can
appear reordered with subsequent loads — a phenomenon called **store-load reordering**.
The `MFENCE` instruction (and `LOCK`-prefixed instructions like `XCHG`) act as full memory
barriers that flush the store buffer.

**MESI** (Modified, Exclusive, Shared, Invalid) is the cache coherence protocol used by
all modern multi-core CPUs (with variations — MESIF on Intel, MOESI on AMD). A cache line
in `M` state is exclusively owned and modified; `E` is exclusively owned and clean; `S`
means multiple cores hold a read-only copy; `I` means invalid. A write to a line in `S`
state requires an invalidation broadcast to all cores holding that line, which can take
~100 ns on a large system. This is the mechanism behind **false sharing**: two unrelated
variables in the same cache line (64 bytes) cause unexpected coherence traffic when written
by different cores.

## Core Concepts

### TSO: Total Store Order (x86-64 Memory Model)

x86-64 implements TSO: all stores become globally visible in the order they were issued by
a given core, but a store by core A and a subsequent load by core A from a different address
can be observed reordered by core B. Specifically:

- Loads are not reordered with loads.
- Stores are not reordered with stores.
- Stores are not reordered with older loads.
- Loads **may** be reordered with older stores to different addresses. (The store buffer.)

This means `SeqCst` atomics in Rust and Go's `sync/atomic` add the `LOCK` prefix to
emit a full fence on x86-64, serializing the store with all subsequent memory operations.
`Release`/`Acquire` semantics on x86-64 are "free" — they compile to regular `MOV`
instructions because TSO already prohibits load-load and store-store reordering.
On ARM (which has a weaker memory model), `Release` stores and `Acquire` loads compile
to explicit `STLR`/`LDAR` instructions with different semantics.

### NUMA Topology

NUMA (Non-Uniform Memory Access) means that memory access latency depends on which NUMA
node the memory resides on relative to the CPU. A 4-socket server with 96 cores has 4
NUMA nodes; accessing RAM on the local node costs ~80 ns, on a remote node ~150–200 ns
(via the inter-socket QPI/UPI interconnect). Thread scheduling, memory allocation, and
data placement must account for NUMA topology for latency-sensitive workloads.

`numactl --hardware` shows topology. `mbind(2)` and `set_mempolicy(2)` control per-
process memory placement. `libnuma` provides the userspace API. In Go, NUMA awareness
requires CGo or raw syscalls; the runtime does not expose NUMA controls.

### False Sharing

Two variables that fit in the same 64-byte cache line, written by different threads,
cause the line to bounce between cores' caches in M state. Every write by thread A
requires an invalidation of thread B's cached copy, and vice versa. At 10 ns per
write + 100 ns for invalidation, a simple counter increment becomes 10x slower when
two "independent" counters share a cache line.

The fix is padding: ensure each independently-written variable occupies its own cache
line. In Go: `type PaddedCounter struct { v int64; _ [56]byte }`. In Rust:
`#[repr(align(64))]`.

## Implementation: Go

```go
package main

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

const cacheLineSize = 64

// --- False sharing demonstration ---
// Two counters: packed (share a cache line) vs padded (each on its own line).

// Packed: both counters share a single cache line (likely).
type PackedCounters struct {
	a int64
	b int64
}

// Padded: each counter occupies its own cache line.
// The padding ensures 'a' and 'b' never share a 64-byte cache line.
type PaddedCounters struct {
	a   int64
	_   [cacheLineSize - 8]byte // pad to 64 bytes
	b   int64
	_   [cacheLineSize - 8]byte
}

func benchCounters(packed *PackedCounters, padded *PaddedCounters, iterations int) (d1, d2 time.Duration) {
	var wg sync.WaitGroup

	// Packed: two goroutines write to a.a and a.b simultaneously.
	wg.Add(2)
	start := time.Now()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			atomic.AddInt64(&packed.a, 1)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			atomic.AddInt64(&packed.b, 1)
		}
	}()
	wg.Wait()
	d1 = time.Since(start)

	// Padded: same writes, but no false sharing.
	wg.Add(2)
	start = time.Now()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			atomic.AddInt64(&padded.a, 1)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			atomic.AddInt64(&padded.b, 1)
		}
	}()
	wg.Wait()
	d2 = time.Since(start)

	return d1, d2
}

// --- Memory ordering: acquire/release semantics ---
// Demonstrates the producer/consumer pattern that requires at minimum
// Release (producer) + Acquire (consumer) memory ordering.

var (
	data  int64
	ready int64 // flag: 0 = not ready, 1 = ready
)

func producer(value int64) {
	// Store data before signaling ready.
	// atomic.StoreInt64 with Release ordering on ARM; plain STORE on x86 (TSO handles it).
	// In Go, atomic.StoreInt64 compiles to LOCK XCHG on x86 (SeqCst) — stronger than needed,
	// but correct and portable.
	atomic.StoreInt64(&data, value)
	atomic.StoreInt64(&ready, 1)
}

func consumer() int64 {
	// Spin until ready, then read data.
	// atomic.LoadInt64 with Acquire ordering: guaranteed to see all writes by the producer
	// that happened before the StoreRelease of ready.
	for atomic.LoadInt64(&ready) == 0 {
		runtime.Gosched() // yield to avoid spinning all cores
	}
	return atomic.LoadInt64(&data)
}

// --- Branch prediction: predictable vs unpredictable branches ---
// Demonstrates how branch predictability affects throughput.

func sumPredictable(n int) int64 {
	var sum int64
	// The branch (i%2 == 0) alternates perfectly — maximally predictable.
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			sum += int64(i)
		} else {
			sum -= int64(i)
		}
	}
	return sum
}

func sumWithBranch(data []bool, values []int64) int64 {
	var sum int64
	// Unpredictable branch — depends on random data.
	for i, b := range data {
		if b {
			sum += values[i]
		}
	}
	return sum
}

// --- Cache-friendly vs cache-unfriendly access ---
// Row-major (cache-friendly) vs column-major (cache-unfriendly) matrix traversal.

const matrixSize = 512

type Matrix [matrixSize][matrixSize]int64

func sumRowMajor(m *Matrix) int64 {
	var sum int64
	// Sequential access: each element follows the previous in memory.
	// CPU prefetcher can predict this pattern and load cache lines ahead.
	for i := 0; i < matrixSize; i++ {
		for j := 0; j < matrixSize; j++ {
			sum += m[i][j]
		}
	}
	return sum
}

func sumColumnMajor(m *Matrix) int64 {
	var sum int64
	// Stride access: each element is matrixSize*8 = 4096 bytes from the previous.
	// Every access is a cache miss — no spatial locality.
	for j := 0; j < matrixSize; j++ {
		for i := 0; i < matrixSize; i++ {
			sum += m[i][j]
		}
	}
	return sum
}

// verifyNoCacheLine checks that PaddedCounters.a and PaddedCounters.b
// do NOT share a cache line.
func verifyNoCacheLine(p *PaddedCounters) {
	addrA := uintptr(unsafe.Pointer(&p.a))
	addrB := uintptr(unsafe.Pointer(&p.b))
	lineA := addrA / cacheLineSize
	lineB := addrB / cacheLineSize
	if lineA == lineB {
		fmt.Println("WARNING: a and b share a cache line — padding is wrong!")
	} else {
		fmt.Printf("PaddedCounters: a in line %d, b in line %d (correct)\n", lineA, lineB)
	}
}

func main() {
	fmt.Printf("GOMAXPROCS: %d, NumCPU: %d\n\n", runtime.GOMAXPROCS(0), runtime.NumCPU())

	// --- False sharing benchmark ---
	fmt.Println("=== False Sharing ===")
	packed := &PackedCounters{}
	padded := &PaddedCounters{}
	verifyNoCacheLine(padded)

	const iterations = 5_000_000
	d1, d2 := benchCounters(packed, padded, iterations)
	fmt.Printf("Packed   (false sharing):    %v  (%v/op)\n", d1, d1/time.Duration(iterations))
	fmt.Printf("Padded   (no false sharing): %v  (%v/op)\n", d2, d2/time.Duration(iterations))
	fmt.Printf("Speedup: %.1fx\n", float64(d1)/float64(d2))

	// --- Memory ordering ---
	fmt.Println("\n=== Memory Ordering (producer/consumer) ===")
	data = 0
	ready = 0
	var result int64
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		result = consumer()
	}()
	time.Sleep(1 * time.Millisecond) // ensure consumer goroutine is spinning
	producer(42)
	wg.Wait()
	fmt.Printf("Consumer read: %d (expected 42)\n", result)

	// --- Cache locality ---
	fmt.Println("\n=== Cache Locality (512x512 matrix) ===")
	m := new(Matrix)
	for i := range m {
		for j := range m[i] {
			m[i][j] = int64(i*matrixSize + j)
		}
	}

	start := time.Now()
	s1 := sumRowMajor(m)
	d1 = time.Since(start)

	start = time.Now()
	s2 := sumColumnMajor(m)
	d2 = time.Since(start)

	fmt.Printf("Row-major    (cache-friendly):   %v\n", d1)
	fmt.Printf("Column-major (cache-unfriendly): %v\n", d2)
	fmt.Printf("Speedup: %.1fx  (sums: %d == %d)\n", float64(d2)/float64(d1), s1, s2)
}
```

### Go-specific considerations

Go's `sync/atomic` package compiles to `LOCK`-prefixed instructions on x86-64 for all
operations. This is stronger (SeqCst) than what most producer/consumer patterns require
(Release/Acquire), but on x86-64 the overhead is small because the `LOCK` prefix is
just a store-buffer flush. On ARM, `sync/atomic` would use `STLR`/`LDAR` semantics,
which are proper Acquire/Release fences.

Go does not expose sub-SeqCst memory orderings from Go code. If you need Relaxed
ordering (e.g., for a counter that is only read at shutdown), you must use `unsafe`
to emit a plain `MOV` without the `LOCK` prefix — or accept the SeqCst overhead. For
most services, the `sync/atomic` overhead is negligible; the rare cases where it matters
are tight loops incrementing per-CPU counters, where per-CPU arrays without atomic
operations are the correct answer.

The Go runtime uses false-sharing avoidance internally: `runtime.mstats` fields that
are written by different goroutines are padded. Study `runtime/mstats.go` for examples.

## Implementation: Rust

```rust
use std::sync::atomic::{AtomicI64, Ordering};
use std::sync::Arc;
use std::thread;
use std::time::Instant;

const CACHE_LINE_SIZE: usize = 64;

// --- False sharing: padded counter ---
// #[repr(align(64))] ensures the struct starts on a 64-byte boundary,
// and padding ensures each counter occupies a full cache line.

#[repr(C, align(64))]
struct PaddedAtomic {
    value: AtomicI64,
    _pad: [u8; CACHE_LINE_SIZE - 8],
}

impl PaddedAtomic {
    fn new(v: i64) -> Self {
        Self {
            value: AtomicI64::new(v),
            _pad: [0; CACHE_LINE_SIZE - 8],
        }
    }
}

// Packed: two atomics on the same cache line (likely).
struct PackedAtomics {
    a: AtomicI64,
    b: AtomicI64,
}

fn bench_false_sharing(iterations: usize) -> (std::time::Duration, std::time::Duration) {
    // --- Packed (false sharing) ---
    let packed = Arc::new(PackedAtomics {
        a: AtomicI64::new(0),
        b: AtomicI64::new(0),
    });

    let p = packed.clone();
    let t1 = thread::spawn(move || {
        for _ in 0..iterations {
            p.a.fetch_add(1, Ordering::Relaxed);
        }
    });
    let p = packed.clone();
    let t2 = thread::spawn(move || {
        for _ in 0..iterations {
            p.b.fetch_add(1, Ordering::Relaxed);
        }
    });
    let start = Instant::now();
    t1.join().unwrap();
    t2.join().unwrap();
    let packed_duration = start.elapsed();

    // --- Padded (no false sharing) ---
    let padded_a = Arc::new(PaddedAtomic::new(0));
    let padded_b = Arc::new(PaddedAtomic::new(0));

    let a = padded_a.clone();
    let b = padded_b.clone();
    let t1 = thread::spawn(move || {
        for _ in 0..iterations {
            a.value.fetch_add(1, Ordering::Relaxed);
        }
    });
    let b2 = b.clone();
    let t2 = thread::spawn(move || {
        for _ in 0..iterations {
            b2.value.fetch_add(1, Ordering::Relaxed);
        }
    });
    let start = Instant::now();
    t1.join().unwrap();
    t2.join().unwrap();
    let padded_duration = start.elapsed();

    (packed_duration, padded_duration)
}

// --- Memory ordering: Acquire/Release in Rust ---
// Rust exposes the full C++20 memory model: Relaxed, Acquire, Release, AcqRel, SeqCst.
// This allows writing the minimum barrier strength for correctness.

fn acquire_release_demo() {
    let data = Arc::new(AtomicI64::new(0));
    let flag = Arc::new(AtomicI64::new(0));

    let d = data.clone();
    let f = flag.clone();
    let producer = thread::spawn(move || {
        d.store(42, Ordering::Relaxed); // data write — Relaxed is OK here because...
        // ...the Release store on flag creates a happens-before edge:
        // all writes before this store are visible to any thread that
        // Acquire-loads flag and sees 1.
        f.store(1, Ordering::Release);
    });

    let d = data.clone();
    let f = flag.clone();
    let consumer = thread::spawn(move || {
        // Spin until flag == 1 with Acquire ordering.
        // Acquire-load establishes the happens-before: we see all writes from the
        // thread that did the Release-store of flag.
        while f.load(Ordering::Acquire) == 0 {
            std::hint::spin_loop();
        }
        // At this point, d.load(Relaxed) is guaranteed to see 42 because:
        // 1. The Relaxed store to data happened-before the Release store to flag.
        // 2. The Acquire load of flag sees the Release store.
        // 3. Therefore, the Relaxed load of data sees the Relaxed store of data.
        d.load(Ordering::Relaxed)
    });

    producer.join().unwrap();
    let result = consumer.join().unwrap();
    println!("Acquire/Release result: {result} (expected 42)");
}

// --- Cache-friendly vs unfriendly access ---
const N: usize = 512;

fn sum_row_major(m: &[[i64; N]; N]) -> i64 {
    // Sequential: each element is 8 bytes after the previous → cache line reuse.
    m.iter().flat_map(|row| row.iter()).sum()
}

fn sum_col_major(m: &[[i64; N]; N]) -> i64 {
    // Strided: each element is N*8 = 4096 bytes from the previous → every miss.
    let mut sum = 0i64;
    for j in 0..N {
        for i in 0..N {
            sum += m[i][j];
        }
    }
    sum
}

// --- NUMA: demonstrate memory allocation on a specific NUMA node ---
// Requires libnuma or raw mbind syscall. Here we show the syscall shape.
fn show_numa_info() {
    // Read NUMA topology from /sys/devices/system/node/
    let nodes_path = std::path::Path::new("/sys/devices/system/node");
    if nodes_path.exists() {
        if let Ok(entries) = std::fs::read_dir(nodes_path) {
            let node_count = entries
                .filter_map(|e| e.ok())
                .filter(|e| e.file_name().to_string_lossy().starts_with("node"))
                .count();
            println!("NUMA nodes: {node_count}");
        }
    }
    // On a non-NUMA system this shows 1 node.
    // For actual NUMA binding, use libc::mbind:
    //   mbind(addr, len, MPOL_BIND, &nodemask, maxnode, MPOL_MF_STRICT)
}

fn main() {
    println!("=== CPU Architecture for Programmers (Rust) ===\n");

    // False sharing benchmark.
    println!("=== False Sharing ===");
    let iterations = 5_000_000;
    let (packed_d, padded_d) = bench_false_sharing(iterations);
    println!("Packed   (false sharing):    {:?}", packed_d);
    println!("Padded   (no false sharing): {:?}", padded_d);
    println!("Speedup: {:.1}x", packed_d.as_secs_f64() / padded_d.as_secs_f64());

    // Acquire/Release semantics.
    println!("\n=== Memory Ordering (Acquire/Release) ===");
    acquire_release_demo();

    // Cache locality.
    println!("\n=== Cache Locality ({}x{} matrix) ===", N, N);
    let mut m = Box::new([[0i64; N]; N]);
    for i in 0..N {
        for j in 0..N {
            m[i][j] = (i * N + j) as i64;
        }
    }

    let start = Instant::now();
    let s1 = sum_row_major(&m);
    let row_d = start.elapsed();

    let start = Instant::now();
    let s2 = sum_col_major(&m);
    let col_d = start.elapsed();

    println!("Row-major    (cache-friendly):   {:?}", row_d);
    println!("Column-major (cache-unfriendly): {:?}", col_d);
    println!("Speedup: {:.1}x  (sums: {} == {})", col_d.as_secs_f64() / row_d.as_secs_f64(), s1, s2);

    // NUMA topology.
    println!("\n=== NUMA Topology ===");
    show_numa_info();

    println!("\nDone.");
}
```

### Rust-specific considerations

Rust's `std::sync::atomic::Ordering` directly maps to the C++20 memory model:
- `Relaxed`: no ordering guarantees; only atomicity of the operation itself.
- `Acquire`: loads — see all writes that happened-before the corresponding Release.
- `Release`: stores — makes all prior writes visible to the corresponding Acquire.
- `AcqRel`: read-modify-write operations (fetch_add, compare_exchange) — both.
- `SeqCst`: total sequential ordering for all SeqCst operations globally.

Using `Relaxed` where `SeqCst` is not required eliminates `LOCK` prefixes on x86-64
and `DMB`/`DSB` barriers on ARM. For high-frequency counters and flags, this can be
significant. The `loom` crate (used in the lock-free data structures topic) can verify
that your chosen orderings are correct by exhaustively exploring all allowed reorderings.

`#[repr(align(64))]` on a struct guarantees 64-byte alignment for the entire struct.
Combined with `std::mem::size_of::<T>() == 64`, it ensures the struct occupies exactly
one cache line. The `crossbeam-utils::CachePadded<T>` wrapper does this automatically
and is the idiomatic choice for production code.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|----|------|
| Memory ordering exposed | Only SeqCst via `sync/atomic` | Full C++20 model (Relaxed→SeqCst) |
| False sharing mitigation | Manual padding `[56]byte` | `#[repr(align(64))]` or `CachePadded<T>` |
| Cache line size assumption | 64 bytes (hardcoded in runtime) | `std::mem::align_of` or `crossbeam` |
| NUMA awareness | No stdlib support; CGo required | `libc::mbind` or `hwloc` crate |
| Atomic fence | No explicit fence API | `std::sync::atomic::fence(Ordering::X)` |
| SeqCst cost on x86 | LOCK prefix (every atomic) | LOCK prefix only for SeqCst |
| Relaxed atomic | Not directly available | Yes, `Ordering::Relaxed` |
| Loom-based ordering verification | No equivalent | `loom` crate (see lock-free structures topic) |
| Per-CPU counters | `runtime.NumCPU()` + slice + manual sharding | `crossbeam-utils::CachePadded` per CPU |
| Memory model documentation | Go memory model (2022) on go.dev | Rust reference + C++20 spec |

## Production War Stories

**False sharing in Linux's `struct zone`**: The Linux kernel's memory zone structure
had multiple fields written by different CPUs packed in adjacent memory. Under high
allocation pressure on many-core machines, these fields false-shared and caused severe
performance degradation. The fix was to add `____cacheline_aligned_in_smp` annotations
to separate the hot fields. The lesson: false sharing is not just a userspace problem —
the Linux kernel spends considerable effort annotating its hot structures.

**Java's Disruptor and cache line padding**: LMAX Exchange built the Disruptor library
specifically to avoid false sharing in high-frequency trading. The `RingBuffer` sequence
number is padded with 7 `long` fields on each side to prevent any adjacent data from
sharing its cache line. Disruptor achieves ~6 ns per message-pass latency on a single
machine — possible only because every hot path eliminates false sharing, lock contention,
and garbage collection.

**Go scheduler's `p` struct padding**: The Go runtime's `p` (processor) struct, which
holds per-P allocator caches and scheduler queues, is carefully padded to avoid false
sharing between Ps running on different cores. The `mcache` pointer is in the first
cache line; the run queue head and tail are in a separate cache line. Without this
padding, every goroutine steal (when a P steals work from another P's run queue) would
invalidate the allocator cache line, slowing allocations on the victim P.

**ARM vs x86 memory model in Go**: Before Go 1.19, Go's memory model was informally
documented and relied on x86-64's relatively strong TSO guarantees. When AWS Graviton
(ARM64) machines became popular, several Go programs that worked correctly on x86-64
showed races on ARM because ARM's memory model allows more reorderings than TSO. The
Go 1.19 memory model revision formally specified acquire/release semantics for channel
operations, mutex lock/unlock, and sync.Once, providing the guarantee without relying on
the underlying CPU model.

## Complexity Analysis

| Event | Cost | Notes |
|-------|------|-------|
| L1 cache hit | ~4 cycles (~1.5 ns) | 32–64 KB, per-core |
| L2 cache hit | ~12 cycles (~4 ns) | 256 KB–1 MB, per-core |
| L3 cache hit | ~30–50 cycles (~10–17 ns) | 8–64 MB, shared |
| DRAM access (local NUMA) | ~200 cycles (~70 ns) | Main memory |
| DRAM access (remote NUMA) | ~400 cycles (~130 ns) | Via QPI/UPI |
| Branch misprediction | ~15–20 cycles | Pipeline flush + restart |
| `MFENCE` / `LOCK`-prefixed op | ~10–30 cycles | Store buffer flush |
| Cache line invalidation (MESI) | ~100–300 ns | Depends on load; IPI to remote core |
| False sharing at 10M ops/s | ~1–10 µs per op | Line bouncing between cores |
| Context switch | ~2–10 µs | Including TLB and cache effects |
| `NUMA mbind` migration | ~100 µs per MB | Moving pages between NUMA nodes |

## Common Pitfalls

1. **Using `sync/atomic` for a counter that is only read at program end.** If the counter
   never needs to be visible to other threads while they are running, a regular (non-atomic)
   increment is correct. The only reason to use atomics is when multiple threads read/write
   concurrently. Unnecessary atomic operations add LOCK prefix overhead and serialize the
   store buffer.

2. **Placing `sync.WaitGroup` next to a hot counter.** `sync.WaitGroup` is a small struct
   (~12 bytes). If it is embedded in a struct next to a frequently-written counter, the
   WaitGroup's internal state and the counter share a cache line, causing false sharing.
   Pad or separate them.

3. **Assuming ARM and x86-64 behave the same for lock-free code.** Code that "works" on
   x86-64 may have data races on ARM because ARM's memory model permits more reorderings.
   Always use proper atomics with correct orderings (Acquire/Release at minimum), not
   raw loads/stores even on x86-64. The x86-64 behavior may mask a real race.

4. **Neglecting NUMA topology in large-scale Go services.** The Go runtime allocates
   memory from a single global heap managed by `mheap`. There is no NUMA-awareness:
   goroutines on socket 0 may have their heap pages allocated on socket 1's DRAM. For
   services running on 2- or 4-socket machines, consider using `numactl --localalloc`
   to run processes per-NUMA-node, or use `mbind` via CGo for the most critical buffers.

5. **Misusing `sync.Pool` to avoid cache misses.** `sync.Pool` stores objects in per-P
   storage — but when a goroutine migrates between Ps (a common occurrence), the pooled
   object is on a different P's storage, potentially on a different NUMA node's cache.
   `sync.Pool` reduces allocation pressure, not cache misses; do not expect it to improve
   data locality.

## Exercises

**Exercise 1** (30 min): Write a Go benchmark that demonstrates false sharing. Two
goroutines increment adjacent vs padded `int64` counters 10M times each. Run with
`-cpuprofile` and inspect the `sync/atomic.AddInt64` flame graph. Use `perf stat -e
cache-misses` to count the cache miss difference.

**Exercise 2** (2–4 h): Implement a per-CPU counter in Rust: an array of
`CachePadded<AtomicI64>` with one slot per CPU (`num_cpus::get()`). Threads increment
the counter on their current CPU, identified via `libc::sched_getcpu()`. Read the total
by summing all slots. Benchmark against a single `AtomicI64` counter at 8, 16, and 32
concurrent threads.

**Exercise 3** (4–8 h): Implement a NUMA-aware allocator in Go using CGo and `numactl`.
The allocator pre-allocates a pool per NUMA node using `mbind(MPOL_BIND)`, then serves
allocations from the pool local to the calling thread's CPU. Benchmark memory bandwidth
(using a sequential write loop) with and without NUMA-local allocation on a 2-socket
machine.

**Exercise 4** (8–15 h): Build a JVM-Disruptor-inspired ring buffer in Rust: a fixed-size
array of `CachePadded<Slot>` where each slot holds a message. One producer writes
sequences; multiple consumers track their own sequence numbers. Prove with `loom` that
your ordering choices are correct. Benchmark against `crossbeam::ArrayQueue` to measure
the false-sharing elimination benefit.

## Further Reading

### Foundational Papers

- Lamport, L. (1979). "How to Make a Multiprocessor Computer That Correctly Executes
  Multiprocess Programs." IEEE TC. Defines sequential consistency.
- Sewell, P. et al. (2010). "x86-TSO: A Rigorous and Usable Programmer's Model for x86
  Multiprocessors." CACM. The formal TSO model with examples of what x86-64 allows.
- McKenney, P. et al. (2010). "Read-Copy Update: Using Execution History to Solve
  Concurrency Problems." PDCS. RCU's design, which exploits CPU memory model properties.

### Books

- Bos, M. "Rust Atomics and Locks" (O'Reilly, 2023). Chapter 3 (Memory Ordering) is
  the clearest explanation of happens-before and the C++20 model available. Essential.
- Hennessy, J., Patterson, D. "Computer Architecture: A Quantitative Approach" (6th ed.).
  Chapter 5 (Memory Hierarchy Design) and Appendix L (Memory Hierarchy) for TLB and
  cache design; Appendix I for multi-processor coherence.
- Harris, T., Larus, J., Rajwar, R. "Transactional Memory" (2nd ed., 2010). Background
  on cache coherence and hardware transactional memory.

### Production Code to Read

- Go memory model: https://go.dev/ref/mem — the 2022 revision with formal definitions.
- `crossbeam-utils/src/cache_padded.rs` — the `CachePadded<T>` implementation.
- Linux `include/linux/cache.h` — `____cacheline_aligned_in_smp` and friends.
- `runtime/internal/atomic/` in the Go source — per-architecture atomic implementations.

### Conference Talks

- "The Rust Memory Model" — RustConf 2023. How Rust maps to C++20 memory ordering.
- "Writing Cache-Friendly Code" — CppCon 2014. Concrete patterns for cache-friendly
  data structures, measured with `perf stat`.
- "NUMA: An Overview for Application Developers" — Linux Plumbers 2016. How NUMA topology
  affects Go and C++ applications at scale.
- "Lock-Free Programming and the Disruptor" — QCon 2011. Martin Thompson's original
  Disruptor presentation with cache line padding motivation.
