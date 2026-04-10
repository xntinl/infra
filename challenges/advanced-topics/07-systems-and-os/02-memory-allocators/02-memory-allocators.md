<!--
type: reference
difficulty: advanced
section: [07-systems-and-os]
concepts: [memory-allocators, tcmalloc, jemalloc, slab-allocator, bump-allocator, thread-local-cache, size-classes, arena-allocation, Go-runtime-allocator, GlobalAlloc]
languages: [go, rust]
estimated_reading_time: 75 min
bloom_level: analyze
prerequisites: [virtual-memory-and-paging, pointer-arithmetic, concurrency-basics]
papers: [Ghemawat2009-tcmalloc, Evans2006-jemalloc, Bonwick1994-slab]
industry_use: [tcmalloc-in-Chrome, jemalloc-in-Firefox-Redis-FreeBSD, mimalloc-in-Snowflake]
language_contrast: high
-->

# Memory Allocators

> The allocator is the invisible performance layer between your code and the OS: the
> difference between a 10 ns allocation and a 500 ns one is the difference between
> a thread-local cache hit and a kernel syscall.

## Mental Model

When you call `malloc(16)` in C, `new(T)` in Go, or `Box::new(v)` in Rust, you are
calling a memory allocator — a library that manages a pool of memory obtained from the
OS via `mmap` or `sbrk`, subdivides it into objects of various sizes, and hands those
objects to the caller. The allocator must solve three problems simultaneously: speed
(allocation must be competitive with a function call on the fast path), space efficiency
(internal fragmentation from rounding up to size classes must not waste more than a few
percent of memory), and concurrency (multiple threads must be able to allocate without
serializing on a single lock).

The canonical solution to the concurrency problem is thread-local caching: each thread
holds a small private pool of pre-sized objects. An allocation is served from the thread's
pool without any synchronization. Only when the thread pool runs empty does the allocator
reach into a shared structure (acquiring a lock) to refill it. This is the core idea
behind tcmalloc (Thread-Caching Malloc, Google), jemalloc (Jason Evans' allocator, used in
FreeBSD, Firefox, Redis, and Meta's server fleet), and Go's own `mcache`/`mcentral`/`mheap`
hierarchy. It is also why allocator behavior changes under high core counts — more cores
means more threads, which means more independent thread-local caches, which means more
total memory overhead from caches holding partially-used spans.

The second axis is size classes. Rather than serving every allocation at an exact size
(which would make the free-list reuse problem intractable), allocators round every request
up to the nearest size class. tcmalloc defines 88 size classes from 8 bytes to 256 KB.
jemalloc defines size classes in eight "size class groups" with geometrically spaced
boundaries. Rounding up wastes memory (internal fragmentation) but makes the free list for
each class homogeneous: every object on the list is the same size, and a freed object can
fill any allocation of the same class.

The kernel-side equivalent of these ideas is the slab allocator (Solaris, Linux). The slab
allocator maintains per-object-type caches of partially-allocated kernel objects — `task_struct`,
`inode`, `dentry`, `socket`. When the kernel creates a new `task_struct` for a `fork`, it
pulls one from the slab without zeroing it (the object is always left in a known-clean state
by the destructor), saving the cost of zeroing and of warming the object's fields. The same
principle applies to userspace arenas for hot allocation paths.

## Core Concepts

### tcmalloc Architecture

tcmalloc organizes memory at three levels:

1. **Thread cache (`tcmalloc::ThreadCache`)**: Each thread has a linked list of free objects
   for each of the 88 size classes. Allocation and deallocation at this level are lockless
   (single-threaded per cache). The cache holds a bounded number of objects per size class;
   when it overflows, the excess is returned to the central cache.

2. **Central cache (`tcmalloc::CentralFreeList`)**: One per size class, shared across
   threads. Protected by a per-class spinlock. Holds "spans" (contiguous runs of pages)
   subdivided into objects. Thread caches exchange batches of objects with the central
   cache, amortizing lock acquisition.

3. **Page heap (`tcmalloc::PageHeap`)**: Manages the coarse-grained allocation of page spans
   from the OS. Handles large allocations (> 256 KB) directly; serves spans to the central
   cache for small allocations.

### jemalloc Architecture

jemalloc introduces **arenas** (default: one per CPU). Each arena manages its own
**runs** (page-aligned regions subdivided into objects of one size class), a **tcache**
(thread-local cache), and extent management for large allocations. The per-arena design
reduces cross-CPU false sharing on the arena metadata itself.

jemalloc distinguishes three allocation tiers:
- **Small** (≤ 14 KB on 64-bit): served from tcache → arena bin → run
- **Large** (14 KB – 4 MB): served from arena extent freelist
- **Huge** (> 4 MB): served directly from the OS

### Slab Allocator (Kernel)

The Linux slab allocator (`kmem_cache_alloc`) pre-allocates pools of fixed-size objects.
Each `kmem_cache` tracks free objects in a per-CPU list (the "per-CPU slab"), a partial
list (slabs with some free, some used objects), and a full list. The constructor/destructor
pair on a slab cache ensures objects are left in a known-initialized state, eliminating
repeated zero-and-init cost. The Linux kernel uses SLUB (a simplified slab) since 2.6.23.

### Bump Allocator

The simplest possible allocator: a pointer that advances forward on each allocation,
with no deallocation support (only a bulk `reset` or `free-all`). Used for arena/region
allocators where all objects are freed at once (per-request arenas, parser buffers, arena
allocators for a game frame). Zero overhead per allocation (one pointer add + compare).

### Go Runtime Allocator: mcache / mcentral / mheap

Go's allocator mirrors tcmalloc's three-level structure:

- **mcache**: Per-P (not per-goroutine, not per-OS-thread) cache of spans for each of
  67 size classes. Allocation is lockless — goroutines running on the same P share a
  single mcache without contention.
- **mcentral**: Per-size-class shared structure; protects its span list with a lock.
  Refills empty mcache spans.
- **mheap**: Global heap; requests memory from the OS in 8 KB multiples. Manages the
  heap arena bitmap and span metadata.

Objects larger than 32 KB are allocated directly from `mheap`, bypassing the size-class
hierarchy.

## Implementation: Go

```go
package main

import (
	"fmt"
	"runtime"
	"sync"
	"time"
	"unsafe"
)

// --- Bump Allocator ---
// A simple arena allocator: allocate from a contiguous buffer,
// reset the whole arena at once. Zero per-allocation overhead.
// Use case: per-request scratch memory, parser buffers.

type BumpAllocator struct {
	buf    []byte
	offset int
}

func NewBumpAllocator(size int) *BumpAllocator {
	return &BumpAllocator{buf: make([]byte, size)}
}

// Alloc returns a pointer to n bytes aligned to align bytes.
// Returns nil if the arena is full.
func (a *BumpAllocator) Alloc(n, align int) unsafe.Pointer {
	// Round up offset to alignment.
	aligned := (a.offset + align - 1) &^ (align - 1)
	if aligned+n > len(a.buf) {
		return nil
	}
	ptr := unsafe.Pointer(&a.buf[aligned])
	a.offset = aligned + n
	return ptr
}

// Reset frees all allocations at once — O(1) regardless of object count.
func (a *BumpAllocator) Reset() {
	a.offset = 0
}

// --- Pool-based allocator (simulating a size-class free list) ---
// sync.Pool is Go's built-in thread-local cache for temporary objects.
// The GC may clear pools between GC cycles — only use for truly temporary objects.

type FixedPool struct {
	pool sync.Pool
	size int
}

func NewFixedPool(size int) *FixedPool {
	return &FixedPool{
		size: size,
		pool: sync.Pool{
			New: func() any {
				buf := make([]byte, size)
				return &buf
			},
		},
	}
}

func (p *FixedPool) Get() *[]byte {
	return p.pool.Get().(*[]byte)
}

func (p *FixedPool) Put(b *[]byte) {
	*b = (*b)[:p.size] // reset length, preserve capacity
	p.pool.Put(b)
}

// --- Benchmarking: standard allocator vs pool vs bump ---

func benchmarkStdAlloc(iterations int) time.Duration {
	start := time.Now()
	for i := 0; i < iterations; i++ {
		// Force an allocation that escapes to the heap.
		b := make([]byte, 256)
		_ = b[0]
	}
	return time.Since(start)
}

func benchmarkPoolAlloc(p *FixedPool, iterations int) time.Duration {
	start := time.Now()
	for i := 0; i < iterations; i++ {
		b := p.Get()
		(*b)[0] = 1
		p.Put(b)
	}
	return time.Since(start)
}

func benchmarkBumpAlloc(a *BumpAllocator, iterations int) time.Duration {
	a.Reset()
	start := time.Now()
	for i := 0; i < iterations; i++ {
		ptr := a.Alloc(256, 8)
		if ptr == nil {
			a.Reset() // arena full — reset and continue
			ptr = a.Alloc(256, 8)
		}
		_ = ptr
	}
	return time.Since(start)
}

// showAllocStats uses runtime.ReadMemStats to show allocator behavior.
// The mcache/mcentral/mheap hierarchy is reflected in HeapSys, HeapIdle, HeapInuse.
func showAllocStats() {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	fmt.Printf("HeapAlloc:    %d KB  (currently live objects)\n", ms.HeapAlloc/1024)
	fmt.Printf("HeapSys:      %d KB  (reserved from OS)\n", ms.HeapSys/1024)
	fmt.Printf("HeapIdle:     %d KB  (returned to OS or waiting in mcache)\n", ms.HeapIdle/1024)
	fmt.Printf("HeapInuse:    %d KB  (in use by allocator spans)\n", ms.HeapInuse/1024)
	fmt.Printf("Mallocs:      %d    (total alloc calls since start)\n", ms.Mallocs)
	fmt.Printf("Frees:        %d    (total free calls since start)\n", ms.Frees)
	fmt.Printf("GC cycles:    %d\n", ms.NumGC)
	fmt.Printf("MCacheSys:    %d B  (mcache metadata size)\n", ms.MCacheSys)
	fmt.Printf("MSpanSys:     %d B  (mspan metadata size)\n", ms.MSpanSys)
}

func main() {
	const iterations = 1_000_000

	fmt.Println("=== Memory Allocator Comparison ===\n")

	// Warmup — let the GC and allocator reach steady state.
	for i := 0; i < 10_000; i++ {
		_ = make([]byte, 256)
	}
	runtime.GC()

	fmt.Println("--- Allocator stats before benchmark ---")
	showAllocStats()

	pool := NewFixedPool(256)
	bump := NewBumpAllocator(256 * iterations) // large enough for one full pass

	d1 := benchmarkStdAlloc(iterations)
	d2 := benchmarkPoolAlloc(pool, iterations)
	d3 := benchmarkBumpAlloc(bump, iterations)

	fmt.Printf("\nResults (%d iterations, 256-byte objects):\n", iterations)
	fmt.Printf("  Standard allocator (mcache):  %v  (%v/op)\n", d1, d1/time.Duration(iterations))
	fmt.Printf("  Pool allocator (sync.Pool):   %v  (%v/op)\n", d2, d2/time.Duration(iterations))
	fmt.Printf("  Bump allocator (arena):       %v  (%v/op)\n", d3, d3/time.Duration(iterations))

	runtime.GC()
	fmt.Println("\n--- Allocator stats after benchmark ---")
	showAllocStats()
}
```

### Go-specific considerations

Go's `mcache` is attached to a P (the scheduler's logical processor), not to a goroutine
or OS thread. This means goroutines that are parked (blocking on a channel or I/O) do not
hold an mcache — the P's cache is available to other goroutines. This design is crucial
for Go's goroutine model: if the cache were per-goroutine, a million parked goroutines
would hold a million caches, most of them idle, wasting enormous amounts of memory.

`sync.Pool` is the idiomatic Go equivalent of a size-class free list. It is safe to use
from multiple goroutines and uses per-P storage to minimize contention. The critical
caveat: the GC may discard pool contents between cycles. Do not use `sync.Pool` for
objects that are expensive to initialize and must survive across GC cycles — use a
`chan` or explicit free list instead. `sync.Pool` is best for temporary buffers (encoding,
compression, serialization) that are allocated and returned within a single request.

For replacing the system allocator entirely (e.g., to use jemalloc), link it via CGo:
`import "C"` and a `#cgo LDFLAGS: -ljemalloc` directive. The Go allocator sits on top of
the OS allocator for large spans; replacing the libc `malloc` does not affect Go's own
heap management for Go objects.

## Implementation: Rust

```rust
use std::alloc::{GlobalAlloc, Layout, System};
use std::cell::UnsafeCell;
use std::ptr;
use std::sync::atomic::{AtomicUsize, Ordering};

// --- Bump Allocator ---
// A lock-free, single-threaded arena allocator.
// Suitable for per-request allocations that are all freed at once.
// Not suitable as a global allocator in multi-threaded programs.

struct BumpArena {
    buf: Vec<u8>,
    offset: AtomicUsize,
}

impl BumpArena {
    fn new(capacity: usize) -> Self {
        Self {
            buf: vec![0u8; capacity],
            offset: AtomicUsize::new(0),
        }
    }

    fn alloc(&self, layout: Layout) -> *mut u8 {
        loop {
            let current = self.offset.load(Ordering::Relaxed);
            // Round up to alignment requirement.
            let aligned = (current + layout.align() - 1) & !(layout.align() - 1);
            let new_offset = aligned + layout.size();
            if new_offset > self.buf.capacity() {
                return ptr::null_mut();
            }
            // CAS: only one thread wins the race to advance the pointer.
            // This makes the bump allocator safe for concurrent single-producer
            // use, but note that concurrent threads would need separate arenas.
            match self.offset.compare_exchange_weak(
                current,
                new_offset,
                Ordering::AcqRel,
                Ordering::Relaxed,
            ) {
                Ok(_) => {
                    // Safety: we own the range [aligned, new_offset) exclusively.
                    return unsafe { self.buf.as_ptr().add(aligned) as *mut u8 };
                }
                Err(_) => continue, // lost the race, retry
            }
        }
    }

    fn reset(&self) {
        self.offset.store(0, Ordering::Release);
    }

    fn used(&self) -> usize {
        self.offset.load(Ordering::Acquire)
    }
}

// --- Tracking Allocator ---
// Wraps the system allocator and counts allocations/deallocations.
// Useful for detecting allocation pressure in production profiling.

struct TrackingAllocator {
    inner: System,
    alloc_count: AtomicUsize,
    dealloc_count: AtomicUsize,
    alloc_bytes: AtomicUsize,
}

impl TrackingAllocator {
    const fn new() -> Self {
        Self {
            inner: System,
            alloc_count: AtomicUsize::new(0),
            dealloc_count: AtomicUsize::new(0),
            alloc_bytes: AtomicUsize::new(0),
        }
    }

    fn stats(&self) -> (usize, usize, usize) {
        (
            self.alloc_count.load(Ordering::Relaxed),
            self.dealloc_count.load(Ordering::Relaxed),
            self.alloc_bytes.load(Ordering::Relaxed),
        )
    }
}

// Safety: GlobalAlloc is unsafe to implement; we delegate every operation
// to System (the OS allocator) and only add atomic counters.
unsafe impl GlobalAlloc for TrackingAllocator {
    unsafe fn alloc(&self, layout: Layout) -> *mut u8 {
        let ptr = self.inner.alloc(layout);
        if !ptr.is_null() {
            self.alloc_count.fetch_add(1, Ordering::Relaxed);
            self.alloc_bytes.fetch_add(layout.size(), Ordering::Relaxed);
        }
        ptr
    }

    unsafe fn dealloc(&self, ptr: *mut u8, layout: Layout) {
        self.inner.dealloc(ptr, layout);
        self.dealloc_count.fetch_add(1, Ordering::Relaxed);
    }
}

#[global_allocator]
static ALLOCATOR: TrackingAllocator = TrackingAllocator::new();

// --- Object pool for fixed-size objects ---
// A simple free list for a single type, avoiding allocator overhead
// on the hot path. Uses a stack of raw pointers protected by a Mutex.

use std::sync::Mutex;

struct ObjectPool<T> {
    free: Mutex<Vec<Box<T>>>,
    factory: fn() -> T,
}

impl<T> ObjectPool<T> {
    fn new(factory: fn() -> T, pre_alloc: usize) -> Self {
        let pool = Self {
            free: Mutex::new(Vec::with_capacity(pre_alloc)),
            factory,
        };
        {
            let mut guard = pool.free.lock().unwrap();
            for _ in 0..pre_alloc {
                guard.push(Box::new((pool.factory)()));
            }
        }
        pool
    }

    fn get(&self) -> Box<T> {
        self.free
            .lock()
            .unwrap()
            .pop()
            .unwrap_or_else(|| Box::new((self.factory)()))
    }

    fn put(&self, obj: Box<T>) {
        self.free.lock().unwrap().push(obj);
    }
}

// --- Size-class free list (simplified tcmalloc-style) ---
// Demonstrates the core idea: maintain per-size free lists for
// the most common allocation sizes.

const SIZE_CLASSES: [usize; 8] = [8, 16, 32, 64, 128, 256, 512, 1024];

struct SizeClassAllocator {
    // One free list per size class. UnsafeCell for interior mutability
    // (single-threaded use only — a real allocator would use per-thread state).
    lists: UnsafeCell<[Vec<*mut u8>; 8]>,
}

// Safety: only used from a single thread in this example.
unsafe impl Sync for SizeClassAllocator {}

impl SizeClassAllocator {
    const fn new() -> Self {
        Self {
            lists: UnsafeCell::new([
                Vec::new(), Vec::new(), Vec::new(), Vec::new(),
                Vec::new(), Vec::new(), Vec::new(), Vec::new(),
            ]),
        }
    }

    fn size_class_index(size: usize) -> Option<usize> {
        SIZE_CLASSES.iter().position(|&sc| sc >= size)
    }

    unsafe fn alloc_sc(&self, size: usize) -> *mut u8 {
        if let Some(idx) = Self::size_class_index(size) {
            let lists = &mut *self.lists.get();
            if let Some(ptr) = lists[idx].pop() {
                return ptr; // free-list hit — no allocation
            }
            // Cache miss — allocate the rounded-up size class.
            let layout = Layout::from_size_align(SIZE_CLASSES[idx], 8).unwrap();
            System.alloc(layout)
        } else {
            // Larger than our largest size class — go directly to system.
            let layout = Layout::from_size_align(size, 8).unwrap();
            System.alloc(layout)
        }
    }

    unsafe fn free_sc(&self, ptr: *mut u8, size: usize) {
        if let Some(idx) = Self::size_class_index(size) {
            // Return to free list — avoids calling back to the OS allocator.
            let lists = &mut *self.lists.get();
            lists[idx].push(ptr);
        } else {
            let layout = Layout::from_size_align(size, 8).unwrap();
            System.dealloc(ptr, layout);
        }
    }
}

fn main() {
    println!("=== Rust Memory Allocator Patterns ===\n");

    // --- Bump allocator benchmark ---
    let arena = BumpArena::new(10 * 1024 * 1024); // 10 MB arena
    let start = std::time::Instant::now();
    let n = 100_000;
    for _ in 0..n {
        let layout = Layout::from_size_align(256, 8).unwrap();
        let ptr = arena.alloc(layout);
        assert!(!ptr.is_null());
    }
    let bump_duration = start.elapsed();
    println!(
        "Bump allocator: {} allocs in {:?} ({:?}/op)",
        n,
        bump_duration,
        bump_duration / n
    );
    println!("Arena used: {} KB", arena.used() / 1024);
    arena.reset();

    // --- Object pool benchmark ---
    let pool: ObjectPool<[u8; 256]> = ObjectPool::new(|| [0u8; 256], 1000);
    let start = std::time::Instant::now();
    for _ in 0..n {
        let mut obj = pool.get();
        obj[0] = 1;
        pool.put(obj);
    }
    let pool_duration = start.elapsed();
    println!(
        "Object pool:    {} allocs in {:?} ({:?}/op)",
        n,
        pool_duration,
        pool_duration / n
    );

    // --- Standard allocator (Box) benchmark ---
    let start = std::time::Instant::now();
    for _ in 0..n {
        let b = Box::new([0u8; 256]);
        let _ = b[0];
        drop(b);
    }
    let box_duration = start.elapsed();
    println!(
        "Box (system):   {} allocs in {:?} ({:?}/op)",
        n,
        box_duration,
        box_duration / n
    );

    // --- Tracking allocator stats ---
    let (alloc_count, dealloc_count, alloc_bytes) = ALLOCATOR.stats();
    println!("\nTracking allocator stats (global):");
    println!("  Total allocs:    {alloc_count}");
    println!("  Total deallocs:  {dealloc_count}");
    println!("  Total bytes:     {} KB", alloc_bytes / 1024);
    println!("  Live objects:    {}", alloc_count - dealloc_count);
}
```

### Rust-specific considerations

The `#[global_allocator]` attribute designates a type implementing `GlobalAlloc` as the
process-wide allocator. This is the hook for replacing the default system allocator with
jemalloc (`tikv-jemallocator` crate) or mimalloc (`mimalloc` crate). TiKV (the storage
layer of TiDB) replaces the system allocator with jemalloc precisely because RocksDB's
allocation pattern (many small, short-lived objects for compaction) performs significantly
better with jemalloc's arena model.

The `unsafe impl GlobalAlloc` requires careful attention: the `alloc` function must return
a pointer aligned to `layout.align()` and of size at least `layout.size()`, or null on
failure. The `dealloc` function receives the same layout that was passed to `alloc` — the
allocator does not need to track sizes internally. This is a deliberate API choice that
enables size-class free lists without per-object headers.

Rust's `Vec` and `Box` call `GlobalAlloc::alloc`/`dealloc` through the `std::alloc`
module. Custom collections can accept an allocator parameter via the `Allocator` trait
(nightly, stabilizing in Rust 2024), enabling per-collection arena allocation — the
standard library's `Vec<T, A: Allocator>` accepts any allocator.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|----|------|
| Default allocator | Go runtime (tcmalloc-inspired mcache/mcentral/mheap) | System allocator (ptmalloc on Linux, HeapAlloc on Windows) |
| Thread-local cache | Per-P mcache (67 size classes) | None by default; jemalloc tcache if substituted |
| Custom allocator | Not natively supported; CGo to link jemalloc | `#[global_allocator]` or per-collection `Allocator` trait |
| Pool for temporaries | `sync.Pool` (GC-cleared between cycles) | `typed_arena`, `bumpalo`, or custom `ObjectPool` |
| Bump allocator | Manual implementation or `arena` experimental pkg | `bumpalo` crate; also `typed-arena` |
| Large allocation path | Directly from mheap (> 32 KB) | System allocator; or custom |
| Fragmentation visibility | `runtime.ReadMemStats` (HeapIdle, HeapInuse) | `jemalloc_ctl` stats via `tikv-jemalloc-ctl` |
| Replacing the allocator | Link jemalloc via CGo; only affects C heap | `#[global_allocator]` affects all Box/Vec/String |
| GC interaction | Allocator and GC are tightly integrated | No GC; allocator is orthogonal to ownership |

## Production War Stories

**Redis and jemalloc**: Redis replaced its default libc allocator with jemalloc in 2009.
The primary benefit was fragmentation reduction: Redis's mixed-size object pattern (short
strings, lists, sorted sets with varying element counts) caused ptmalloc's glibc allocator
to accumulate fragmentation until RSS was 2–4x the logical data size. jemalloc's extent
tree and size-class design keeps fragmentation below 1.2x RSS/data ratio in practice.
Redis exposes `MEMORY DOCTOR` which calls `jemalloc_stats_print` to report arena
utilization.

**Meta's server fleet and TCMalloc 2.0**: Meta runs TCMalloc across its server fleet.
A 2022 investigation found that the per-thread cache in TCMalloc 1.x held too many
objects on services with O(10,000) threads, consuming 10–20 GB of cache memory per host
for metadata alone. TCMalloc 2.0 introduced "Temeraire" (huge-page-aware span management)
and reduced per-thread cache sizes based on actual usage, recovering 8–12 GB per host.

**Go's GC and allocator interaction**: Go's `GOGC` variable (default 100) means "trigger
GC when live heap doubles." A service that allocates 1 GB of live objects during request
handling will trigger a GC at 2 GB. If the service allocates many short-lived 256-byte
objects (e.g., JSON decoding), each lives in a size-class span. When the GC sweeps them,
the span becomes partially free — it stays in `mcache` until all objects in it are freed.
A sweep-dominated GC profile (seen in high-QPS JSON services) can be resolved by using
`sync.Pool` for decoder buffers, dropping allocation rate by 80–90% and GC frequency
accordingly.

**Snowflake and mimalloc**: Snowflake's query execution engine replaced the system
allocator with `mimalloc` and reported 20–30% lower P99 latency on OLAP queries. The
cause was fragmentation: OLAP queries allocate large, irregular buffers for join spill
and aggregation state, then free them all at query end. ptmalloc's coalescing heuristics
failed to return this memory to the OS promptly, inflating RSS and causing THP compaction
pressure. mimalloc's segment-per-thread design handles this pattern better.

## Complexity Analysis

| Operation | Latency | Notes |
|-----------|---------|-------|
| Bump allocator alloc | ~2–5 ns | Pointer add + compare; no synchronization |
| Thread-local free list hit (tcmalloc/jemalloc tcache) | ~10–15 ns | No lock; only pointer manipulation |
| Central free list miss (lock acquisition) | ~50–200 ns | Spinlock; contention under high parallelism |
| `sync.Pool` Get (Go, warm) | ~15–30 ns | Per-P storage, lockless |
| `sync.Pool` Get (Go, cold/GC cleared) | ~100–500 ns | Falls back to `New` function |
| `Box::new` (Rust, system allocator) | ~20–50 ns | ptmalloc free list hit |
| `mmap` for new span (OS allocation) | ~500–2000 ns | Kernel call |
| jemalloc arena refill from OS | ~1–5 µs | Extent allocation + TLB initialization |
| Object pool Get (Mutex-protected, uncontended) | ~30–60 ns | Mutex lock + Vec pop |

The critical insight: the fast path (thread-local cache hit) is 10–20x faster than the
slow path (central allocator), which is 100–1000x faster than going to the OS. Allocator
performance depends almost entirely on cache hit rate.

## Common Pitfalls

1. **Using `sync.Pool` for long-lived objects.** The GC discards pool contents at every
   GC cycle. A pool that holds 100 large buffers will be emptied every few seconds on a
   busy service, forcing 100 new allocations. Use a `chan` with a known capacity instead.

2. **Not sizing `sync.Pool` objects correctly.** If you put a 4 KB buffer back after using
   only 256 bytes of it, you return 4 KB to the pool. The next caller gets 4 KB when it
   needs 256 B. Use typed pools: one pool per expected size.

3. **Allocating inside a hot loop without an arena.** Parsers, serializers, and query
   planners often allocate thousands of small objects per call. Without a per-call arena,
   each object goes through the thread-local cache, generating GC pressure proportional
   to allocation rate. A bump arena for the call's scratch memory eliminates that pressure.

4. **Replacing the Go allocator via CGo.** Calling `jemalloc` from Go via CGo only affects
   C allocations (`C.malloc`). Go objects (`make`, `new`, `&T{}`) always use Go's own
   allocator. There is no way to replace Go's allocator from Go code.

5. **Ignoring internal fragmentation in size classes.** A 33-byte allocation in jemalloc
   is served from the 40-byte size class, wasting 7 bytes. For a service that stores
   millions of 33-byte strings, this is a 21% memory waste. Profile with `jemalloc_stats`
   or `MALLOC_CONF=stats_print:true` to identify dominant size classes and redesign data
   structures to align with them.

## Exercises

**Exercise 1** (30 min): Implement a benchmark that compares `sync.Pool`, a `chan`-based
free list, and direct `make` for 1024-byte buffers under 8 concurrent goroutines. Use
`benchstat` to compare results across 10 runs. Observe how GC frequency affects `sync.Pool`
throughput.

**Exercise 2** (2–4 h): Implement a per-request arena allocator in Go that is backed by
a `sync.Pool` of large byte slices. The arena should support `Alloc(n int) []byte` and
`Reset()`. Use it to replace `make` calls in a JSON decoder and measure the reduction in
GC cycles via `runtime.ReadMemStats`.

**Exercise 3** (4–8 h): Implement a size-class allocator in Rust with 8 size classes
(8, 16, 32, 64, 128, 256, 512, 1024 bytes) using per-thread free lists (via
`thread_local!`). Benchmark it against `Box::new` for each size class. Implement
`unsafe impl GlobalAlloc` and test it as a global allocator.

**Exercise 4** (8–15 h): Replace the Go runtime allocator's behavior by implementing a
`sync.Pool`-backed slab allocator for a specific type that dominates your profiling output.
Instrument both the original and pooled code with `pprof` heap profiles. Show the
reduction in allocation rate, GC pause time, and RSS under a sustained load test
(e.g., 10,000 req/s for 60 seconds via `wrk`).

## Further Reading

### Foundational Papers

- Ghemawat, S., Menage, P. (2009). "TCMalloc: Thread-Caching Malloc."
  Google internal paper, publicly referenced. The original description of thread-local
  caching and central free lists.
- Evans, J. (2006). "A Scalable Concurrent `malloc(3)` Implementation for FreeBSD."
  BSDCan 2006. jemalloc's original design paper; still accurate for core concepts.
- Bonwick, J. (1994). "The Slab Allocator: An Object-Caching Kernel Memory Allocator."
  USENIX ATC 1994. The kernel slab allocator; explains the constructor/destructor model.
- Berger, E.D. et al. (2000). "Hoard: A Scalable Memory Allocator for Multithreaded
  Applications." ASPLOS 2000. Theoretical grounding for per-thread heap design.

### Books

- Love, R. "Linux Kernel Development" (3rd ed.) — Chapter 12 (Memory Management) covers
  the slab allocator, `kmalloc`, and SLUB.
- Kerrisk, M. "The Linux Programming Interface" — Chapter 7 (Memory Allocation) covers
  `malloc`, `brk`, and `mmap`-based allocation from the userspace perspective.

### Production Code to Read

- `runtime/malloc.go` in the Go source — the complete mcache/mcentral/mheap implementation.
  `mallocgc` is the entry point for every Go allocation.
- `tcmalloc/src/` in Google's TCMalloc repo — `tcmalloc.cc` and `thread_cache.cc` for the
  thread cache implementation.
- `src/jemalloc.c` in jemalloc — the `je_malloc_default` function and arena selection.
- `bumpalo/src/lib.rs` — a production-quality bump allocator in 700 lines of Rust.

### Conference Talks

- "Allocator Wars" — CppCon 2015. Comparison of allocator designs and their tradeoffs.
- "Go Memory Management and Allocation" — GopherCon Russia 2018. Detailed walkthrough
  of mcache, mcentral, mheap with diagrams.
- "High Performance Go: Allocation and the GC" — dotGo 2019. Profiling allocation
  hotspots and using pools to reduce GC pressure.
- "jemalloc: Memory Allocation for Multithreaded Applications" — FOSDEM 2017. Arena
  design, extent management, and profiling with `heap_profiling`.
