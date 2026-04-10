<!--
type: reference
difficulty: advanced
section: [09-performance-and-optimization]
concepts: [cache-lines, false-sharing, AoS-vs-SoA, struct-padding, data-oriented-design, cache-associativity, prefetcher, L1-L2-L3]
languages: [go, rust]
estimated_reading_time: 80 min
bloom_level: evaluate
prerequisites: [sync-atomic, goroutines, rust-structs, memory-layout]
papers: [Mike Acton — Data-Oriented Design (CppCon 2014), Ulrich Drepper — What Every Programmer Should Know About Memory (2007)]
industry_use: [ClickHouse columnar engine, Disruptor pattern, Linux kernel cache alignment, crossbeam-channel]
language_contrast: high
-->

# CPU Cache Optimization

> The cache line is the unit of memory transfer between DRAM and CPU. Once you internalize
> that reads and writes happen in 64-byte chunks — not individual bytes — the performance
> of every data structure becomes predictable without measurement.

## Mental Model

Modern CPUs have three levels of cache between the register file and main memory. The
critical property: data moves in **cache lines** — 64-byte aligned blocks on all x86-64
and ARM processors in production use. When you read one byte, the CPU fetches the entire
64-byte line into L1. When another core writes to any byte in that same line, your line
is invalidated and must be refetched.

This has three consequences that drive all cache optimization:

**1. Spatial locality**: if you access data at address X, the next access to addresses
X+1 through X+63 is free (already in cache). This is why iterating through a contiguous
array is fast. This is why iterating through a linked list is slow: each node pointer
follows a random address, causing a cache miss per node.

**2. False sharing**: two threads writing to different variables that happen to live in
the same 64-byte cache line will repeatedly invalidate each other's cache entries — even
though they never write the *same* variable. This is one of the most common hidden
bottlenecks in concurrent code. A single padded field can eliminate a 10x slowdown.

**3. Struct layout**: the C and Rust compilers insert padding bytes to align struct fields
to their natural alignment. A poorly ordered struct wastes cache space by interleaving
hot and cold fields, forcing the CPU to load unnecessary bytes to reach the hot data.
Reordering fields — or separating hot fields into a packed inner struct — reduces the
cache footprint of hot paths.

The key insight from data-oriented design: **the CPU does not care about your object
model. It cares about the layout of bytes in memory**. "Struct of arrays" beats "array
of structs" for operations that touch only one field of many objects because it enables
sequential, prefetcher-friendly access to exactly the data being processed.

## Core Concepts

### Cache Line Size and Alignment

On x86-64 (Intel, AMD), ARM64 (Apple M-series, AWS Graviton), and every mainstream
server CPU in production: **cache line size = 64 bytes**. Apple M-series chips have
L1 lines of 128 bytes, but the coherence granularity (the false-sharing boundary) remains
64 bytes for compatibility.

Alignment rules:
- A 64-byte aligned struct whose hot fields fit in 64 bytes needs exactly one cache line
  to load the hot data
- An unaligned struct that straddles a cache line boundary requires two cache line fetches
  for a single load — avoidable with `#[repr(align(64))]` in Rust or `//go:align` (Go 1.23+)

### False Sharing (The 10x Slowdown Pattern)

```
Thread 0                    Thread 1
writes counter0             writes counter1
[..counter0..|..counter1..] ← both in the same 64-byte cache line
     ^               ^
     Both writes invalidate the entire line in the other core's L1 cache.
     Every write becomes a cross-core coherence message.
     At 16 cores, this serializes all counter updates.
```

The fix: ensure each independently written variable occupies its own cache line by
padding to 64 bytes. This wastes memory but eliminates coherence traffic. Use only on
variables that are written frequently from multiple cores.

### AoS vs SoA (Array of Structs vs Struct of Arrays)

**AoS** — the natural OOP layout:
```
[{x:f32, y:f32, z:f32, active:bool}, {x, y, z, active}, ...]
```

**SoA** — the cache-friendly layout for operations over one field:
```
xs: [f32, f32, f32, ...]
ys: [f32, f32, f32, ...]
zs: [f32, f32, f32, ...]
actives: [bool, bool, bool, ...]
```

If an operation reads only `x` values (e.g., collision detection on one axis), AoS loads
the entire struct per element including unused `y`, `z`, `active`. SoA loads only `x`
values — 16 `f32` values fit in one cache line, vs. 4 complete structs. The throughput
improvement for vectorizable loops is typically 2x–8x.

### Struct Field Ordering

The compiler inserts padding to satisfy alignment requirements:
- `bool` or `u8`: 1-byte aligned, no padding
- `u16`/`i16`: 2-byte aligned
- `u32`/`i32`/`f32`: 4-byte aligned
- `u64`/`i64`/`f64`/`usize`/pointer: 8-byte aligned

**Bad ordering** (wastes 7 bytes padding per struct):
```rust
struct Bad {
    a: u8,    // 1 byte + 7 bytes padding to reach next 8-byte boundary
    b: u64,   // 8 bytes
    c: u8,    // 1 byte + 7 bytes padding
    d: u64,   // 8 bytes
}
// sizeof(Bad) = 32 bytes, but useful data = 18 bytes → 44% waste
```

**Good ordering** (largest to smallest, zero wasted bytes):
```rust
struct Good {
    b: u64,   // 8 bytes
    d: u64,   // 8 bytes
    a: u8,    // 1 byte
    c: u8,    // 1 byte + 6 bytes padding to reach struct alignment
}
// sizeof(Good) = 24 bytes → same data, 25% smaller
```

Smaller structs = more structs per cache line = fewer cache misses on iteration.

## Implementation: Go

```go
package main

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"
)

// --- BEFORE: False Sharing ---
// Two goroutines increment counters that share a cache line.
// On a multi-core machine, this will be dramatically slower than the padded version.

type CountersBad struct {
	a int64 // offset 0
	b int64 // offset 8 — same 64-byte cache line as a
}

func BenchmarkFalseSharingBad(b *testing.B) {
	var c CountersBad
	var wg sync.WaitGroup
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				atomic.AddInt64(&c.a, 1)
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				atomic.AddInt64(&c.b, 1)
			}
		}()
		wg.Wait()
	}
}

// --- AFTER: Padded to 64-byte boundaries ---
// Each counter occupies exactly one cache line. No coherence traffic between goroutines.

type PaddedCounter struct {
	value int64
	_     [56]byte // padding to fill the 64-byte cache line
	// sizeof(PaddedCounter) = 64 bytes
}

type CountersGood struct {
	a PaddedCounter
	b PaddedCounter
}

func BenchmarkFalseSharingGood(b *testing.B) {
	var c CountersGood
	var wg sync.WaitGroup
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				atomic.AddInt64(&c.a.value, 1)
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				atomic.AddInt64(&c.b.value, 1)
			}
		}()
		wg.Wait()
	}
}

// Expected results (8-core machine, go test -bench=. -cpu=8 -benchtime=3s):
//
//   BenchmarkFalseSharingBad-8    200000    7821 ns/op
//   BenchmarkFalseSharingGood-8   200000     723 ns/op  ← ~10x faster
//
// The speedup scales with core count and contention frequency.

// --- AoS vs SoA: Summing one field over many structs ---

const N = 1 << 20 // 1 million elements

// AoS: natural OOP layout
type ParticleAoS struct {
	X, Y, Z float32 // 12 bytes
	VX, VY, VZ float32 // 12 bytes
	Mass float32       // 4 bytes
	Active bool        // 1 byte + 3 bytes padding = 32 bytes total
}

// SoA: column-oriented layout
type ParticlesSoA struct {
	X, Y, Z    []float32
	VX, VY, VZ []float32
	Mass       []float32
	Active     []bool
}

// BEFORE: AoS — to compute total mass, we load the entire struct per element.
// Each cache line holds 2 particles. We fetch 32 bytes but use only 4 (Mass field).
func TotalMassAoS(particles []ParticleAoS) float32 {
	var total float32
	for i := range particles {
		if particles[i].Active {
			total += particles[i].Mass
		}
	}
	return total
}

// AFTER: SoA — Mass values are contiguous. 16 float32s per cache line.
// We load only what we need. Hardware prefetcher loves sequential access.
func TotalMassSoA(p *ParticlesSoA) float32 {
	var total float32
	for i := range p.Mass {
		if p.Active[i] {
			total += p.Mass[i]
		}
	}
	return total
}

func BenchmarkTotalMassAoS(b *testing.B) {
	particles := make([]ParticleAoS, N)
	for i := range particles {
		particles[i] = ParticleAoS{Mass: 1.0, Active: i%2 == 0}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = TotalMassAoS(particles)
	}
}

func BenchmarkTotalMassSoA(b *testing.B) {
	p := &ParticlesSoA{
		Mass:   make([]float32, N),
		Active: make([]bool, N),
	}
	for i := range p.Mass {
		p.Mass[i] = 1.0
		p.Active[i] = i%2 == 0
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = TotalMassSoA(p)
	}
}

// Expected results (go test -bench=TotalMass -benchmem):
//   BenchmarkTotalMassAoS-8     500    2341620 ns/op
//   BenchmarkTotalMassSoA-8     500     412000 ns/op  ← ~5.7x faster

// --- Struct Alignment Verification ---

type GoodLayout struct {
	A uint64  // 8 bytes at offset 0
	B uint64  // 8 bytes at offset 8
	C uint32  // 4 bytes at offset 16
	D uint16  // 2 bytes at offset 20
	E uint8   // 1 byte  at offset 22
	F uint8   // 1 byte  at offset 23
	// Total: 24 bytes, no padding wasted
}

type BadLayout struct {
	E uint8   // 1 byte at offset 0, then 7 bytes padding
	A uint64  // 8 bytes at offset 8
	F uint8   // 1 byte at offset 16, then 1 byte padding
	D uint16  // 2 bytes at offset 18, then 4 bytes padding
	C uint32  // 4 bytes at offset 24, then 4 bytes padding
	B uint64  // 8 bytes at offset 32
	// Total: 40 bytes — 16 bytes wasted on padding
}

func main() {
	fmt.Printf("GoodLayout size: %d bytes\n", unsafe.Sizeof(GoodLayout{}))
	fmt.Printf("BadLayout  size: %d bytes\n", unsafe.Sizeof(BadLayout{}))
	fmt.Printf("PaddedCounter size: %d bytes (should be 64)\n", unsafe.Sizeof(PaddedCounter{}))
	fmt.Printf("CPU cores: %d\n", runtime.NumCPU())
}
```

### Go-specific Considerations

**`sync/atomic` and false sharing**: The false sharing problem is most severe with atomic
operations because they require cache line ownership (MESI protocol's "Modified" state).
An atomic CAS on one variable implicitly locks the entire 64-byte cache line from all
other cores. Two goroutines doing `atomic.Add` to adjacent variables will serialize
completely.

**`sync.Pool` and cache locality**: `sync.Pool` maintains a per-P (per-scheduler thread)
free list. Pool access is cache-warm for the current goroutine's P. Avoid putting large
objects in Pool if they will be accessed across P boundaries — the pool migration path
involves cross-core cache invalidation.

**Go struct alignment**: Go 1.17+ added the `//go:align N` directive for structs needing
explicit cache line alignment. Before 1.17, the only option was manual `[N]byte` padding
fields. The padding field approach is still more portable (works in all Go versions).

**False sharing with `sync.Mutex`**: A `sync.Mutex` is 8 bytes. If you embed multiple
mutexes in a struct without padding, they share cache lines. A highly contended struct
with 8 mutexes in a row will see them invalidating each other on lock/unlock. Pad each
mutex or use a separate struct per mutex.

## Implementation: Rust

```rust
use std::sync::atomic::{AtomicI64, Ordering};
use std::thread;

// --- BEFORE: False Sharing ---
#[derive(Default)]
struct CountersBad {
    a: AtomicI64,  // offset 0
    b: AtomicI64,  // offset 8 — same cache line as a
}

// --- AFTER: Cache-line-padded counters ---
#[repr(align(64))]  // guarantee 64-byte alignment of the struct itself
struct PaddedCounter {
    value: AtomicI64,
    _pad: [u8; 56],  // fill the rest of the 64-byte line
}

impl Default for PaddedCounter {
    fn default() -> Self {
        Self { value: AtomicI64::new(0), _pad: [0; 56] }
    }
}

#[derive(Default)]
struct CountersGood {
    a: PaddedCounter,  // 64 bytes, own cache line
    b: PaddedCounter,  // 64 bytes, own cache line
}

const ITERS: i64 = 1_000_000;

fn false_sharing_bad() -> (i64, i64) {
    let c = std::sync::Arc::new(CountersBad::default());
    let c2 = c.clone();

    let t1 = thread::spawn(move || {
        for _ in 0..ITERS { c.a.fetch_add(1, Ordering::Relaxed); }
    });
    let t2 = thread::spawn(move || {
        for _ in 0..ITERS { c2.b.fetch_add(1, Ordering::Relaxed); }
    });
    t1.join().unwrap();
    t2.join().unwrap();
    // values only used to prevent optimizer from removing the work
    (0, 0)
}

fn false_sharing_good() -> (i64, i64) {
    let c = std::sync::Arc::new(CountersGood::default());
    let c2 = c.clone();

    let t1 = thread::spawn(move || {
        for _ in 0..ITERS { c.a.value.fetch_add(1, Ordering::Relaxed); }
    });
    let t2 = thread::spawn(move || {
        for _ in 0..ITERS { c2.b.value.fetch_add(1, Ordering::Relaxed); }
    });
    t1.join().unwrap();
    t2.join().unwrap();
    (0, 0)
}

// --- AoS vs SoA ---
const N: usize = 1 << 20;

// AoS: entire struct loaded per element
#[derive(Clone)]
struct ParticleAoS {
    x: f32, y: f32, z: f32,
    vx: f32, vy: f32, vz: f32,
    mass: f32,
    active: bool,
    _pad: [u8; 3],  // padding to align to 4 bytes
}

// SoA: each field is a separate contiguous array
struct ParticlesSoA {
    x: Vec<f32>,
    y: Vec<f32>,
    z: Vec<f32>,
    vx: Vec<f32>,
    vy: Vec<f32>,
    vz: Vec<f32>,
    mass: Vec<f32>,
    active: Vec<bool>,
}

fn total_mass_aos(particles: &[ParticleAoS]) -> f32 {
    particles.iter()
        .filter(|p| p.active)
        .map(|p| p.mass)
        .sum()
}

fn total_mass_soa(p: &ParticlesSoA) -> f32 {
    p.mass.iter().zip(p.active.iter())
        .filter(|(_, &active)| active)
        .map(|(&mass, _)| mass)
        .sum()
}

// Struct padding analysis
#[repr(C)]
struct BadLayout {
    e: u8,    // 1 byte + 7 padding
    a: u64,   // 8 bytes
    f: u8,    // 1 byte + 1 padding
    d: u16,   // 2 bytes + 4 padding
    c: u32,   // 4 bytes + 4 padding
    b: u64,   // 8 bytes
    // Total: 40 bytes
}

// Rust's default layout may reorder fields to minimize padding.
// Use #[repr(C)] to force C-compatible ordering (and see the waste).
// Without #[repr(C)], Rust may already produce the optimal layout.
struct GoodLayout {
    a: u64,   // 8 bytes
    b: u64,   // 8 bytes
    c: u32,   // 4 bytes
    d: u16,   // 2 bytes
    e: u8,    // 1 byte
    f: u8,    // 1 byte
    // Total: 24 bytes — Rust achieves this without #[repr(C)]
}

fn main() {
    println!("BadLayout:  {} bytes", std::mem::size_of::<BadLayout>());
    println!("GoodLayout: {} bytes", std::mem::size_of::<GoodLayout>());
    println!("PaddedCounter: {} bytes (should be 64)",
             std::mem::size_of::<PaddedCounter>());
    assert_eq!(std::mem::size_of::<PaddedCounter>(), 64,
               "PaddedCounter must be exactly 64 bytes");

    // time false_sharing_bad() vs false_sharing_good() with std::time::Instant
    let t = std::time::Instant::now();
    false_sharing_bad();
    let bad_us = t.elapsed().as_micros();

    let t = std::time::Instant::now();
    false_sharing_good();
    let good_us = t.elapsed().as_micros();

    println!("False sharing BAD:  {} µs", bad_us);
    println!("False sharing GOOD: {} µs (expected ~10x faster)", good_us);
}
```

### Rust-specific Considerations

**`#[repr(align(N))]`**: Rust allows specifying alignment directly on struct definitions.
`#[repr(align(64))]` guarantees the struct starts at a 64-byte boundary in memory, which
is required for cache-line isolation. Combined with making `sizeof(struct) == 64`, it
guarantees each instance occupies exactly one cache line.

**Automatic field reordering**: Rust's default struct layout (`#[repr(Rust)]`) reorders
fields to minimize padding. This is almost always correct for single-threaded access but
does not protect against false sharing. For false sharing prevention, always use explicit
padding and `#[repr(align(64))]`.

**Crossbeam's `CachePadded<T>`**: The `crossbeam-utils` crate provides a ready-made
`CachePadded<T>` wrapper that handles the platform-specific cache line size (128 bytes
on some architectures) and provides `Deref`/`DerefMut` for ergonomic access. Use this
in production rather than manual padding:

```rust
use crossbeam_utils::CachePadded;
struct Counters {
    a: CachePadded<AtomicI64>,
    b: CachePadded<AtomicI64>,
}
```

**LLVM and struct layout**: When you write a tight loop over an array, LLVM (via Rust's
release mode) may auto-vectorize the loop using SIMD. SIMD vectorization requires aligned
data. `#[repr(align(32))]` enables AVX2; `#[repr(align(64))]` enables AVX-512. If you
plan to add SIMD later, use `align(64)` for hot structs.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Cache line size constant | Manual `[56]byte` padding | `crossbeam_utils::CachePadded<T>` or `#[repr(align(64))]` |
| Struct field reordering | Never — fields stay in declaration order | Yes — default layout reorders; use `#[repr(C)]` to disable |
| Alignment directives | `//go:align N` (Go 1.23+) or manual pad fields | `#[repr(align(N))]` — stable, widely used |
| Detecting false sharing | `-race` flag detects data races, not false sharing; use `perf c2c` | `perf c2c` (Linux); no Rust-specific tool |
| False sharing tooling | `perf c2c` (Linux kernel ≥ 4.10) | Same: `perf c2c` |
| AoS→SoA ergonomics | Manual struct reorganization | Same — manual; `soa_derive` crate automates generation |
| Cache miss profiling | `perf stat -e cache-misses` (external) | `perf stat -e cache-misses` (external) |
| Padding waste detection | `pahole`, `structlayout` | `cargo-show-asm`, `std::mem::size_of` assertions |

## Production War Stories

**Linux kernel per-CPU counters**: The Linux kernel's `percpu` counter mechanism places
each CPU's counter in its own cache line to eliminate false sharing. The `local_t` type
in the kernel is defined with explicit cache line padding. Without this, `jiffies` updates
from all CPUs would serialize — the same pattern shown in the benchmark above, at kernel
scale.

**ClickHouse columnar storage**: ClickHouse stores table columns as contiguous arrays
(SoA layout). A query that filters on `timestamp` only reads the `timestamp` column —
all other columns stay cold. This is why ClickHouse can scan billions of rows per second:
it achieves sequential access to exactly the data being processed. An equivalent row-
oriented (AoS) layout would saturate memory bandwidth on the columns being ignored.

**Disruptor pattern (LMAX)**: The LMAX Disruptor, a high-performance ring buffer used in
financial trading systems, pads each ring buffer slot to exactly one cache line and aligns
the sequence number (the write cursor) to its own cache line. Without padding, multiple
threads publishing to the ring buffer cause cache line bouncing on the sequence number,
limiting throughput. The Disruptor paper documents a 25x throughput improvement from this
single change on their benchmark workload.

**Go's `sync.Map` implementation**: Go's `sync.Map` uses a two-phase read path (dirty map
and read map) to avoid false sharing on reads. The read path is lock-free and the read
map is a separate pointer — reading from the read map doesn't touch the dirty map's lock.
This is an application of cache line separation at the data structure design level.

## Numbers That Matter

| Scenario | Approximate Throughput or Latency |
|----------|----------------------------------|
| Sequential array scan (L1-warm, 64B elements) | ~16 GB/s (4 elements/cycle at 3 GHz) |
| Random pointer chase through 4 GB array | ~90 MB/s (one DRAM miss per pointer) |
| False sharing, 2 cores, tight atomic loop | ~10–20M ops/sec |
| Padded counters, 2 cores, tight atomic loop | ~150–300M ops/sec (~10–15x improvement) |
| False sharing penalty at 16 cores | ~50–100x slowdown vs single-core |
| Struct padding overhead: 40B vs 24B struct | ~40% more cache misses on array iteration |
| AoS vs SoA for single-field scan (1M elements) | SoA: 3–8x faster depending on struct size |

## Common Pitfalls

**Padding variables that are not contended**: Padding every struct field to 64 bytes
wastes memory and pollutes the cache. Only pad variables that are written frequently from
multiple cores simultaneously. Read-mostly variables do not need padding — shared reads
of the same cache line are coherent and free.

**Assuming `sync.Mutex` needs cache line padding**: A mutex that protects a data structure
is fine sharing a cache line with that structure. The false sharing problem only applies
to independently *written* variables. A mutex and its protected data are written together
(lock → write → unlock), so they naturally belong in the same cache region.

**Forgetting alignment when adding padding**: A `[56]byte` pad field after an `int64`
is correct (8 + 56 = 64). A `[55]byte` pad after an `int64` gives 63 bytes — the next
struct instance in an array will straddle a cache line. Always verify:
`sizeof(padded_struct) == 64` and `alignof(padded_struct) >= 64`.

**Converting AoS to SoA everywhere**: SoA is faster for operations that access one field
at a time (filtering, aggregation). It is slower for operations that access all fields
of each object (e.g., full object serialization). Measure both layouts for your access
patterns before committing.

**Missing the -race → perf c2c distinction**: Go's race detector detects concurrent
unsynchronized access — data races. False sharing is not a data race; it is synchronized
access to different variables that happen to share a cache line. `-race` will not find
it. Use `perf c2c` (Linux ≥ 4.10) to find false sharing in production: it attributes
cache line bouncing to specific variables.

## Exercises

**Exercise 1** (30 min): Write a Go benchmark with `CountersBad` vs `CountersPadded` and
run it with `go test -bench=. -cpu=1,2,4,8`. Plot the per-operation time as a function
of core count. Observe the superlinear slowdown of the unpadded version as core count
increases. Write down your explanation for why the slowdown is superlinear.

**Exercise 2** (2–4h): Take a Go struct used in a hot path of a project you own (or a
sample server). Use `unsafe.Sizeof` and `unsafe.Offsetof` to print every field's offset
and size. Identify any padding gaps. Reorder fields to eliminate gaps and verify the
size reduction. Write a benchmark showing the improvement in iteration throughput.

**Exercise 3** (4–8h): Implement a simple particle simulation in both AoS and SoA layouts
in Rust. The simulation updates position based on velocity (X += VX, etc.) for 1 million
particles. Benchmark both layouts with `criterion`. Use `perf stat -e cache-misses` to
confirm the cache miss rate is lower for SoA. Identify which operations would *not*
benefit from SoA (full particle serialization).

**Exercise 4** (8–15h): Find or create a concurrent data structure in Go (e.g., a sharded
map or a worker pool with per-worker statistics). Use `perf c2c` to identify all cache
line hot spots. Apply padding to the identified variables. Measure the throughput
improvement under 8-thread and 16-thread load. Document which variables warranted padding
and which did not, and explain why.

## Further Reading

### Foundational Papers

- Ulrich Drepper — ["What Every Programmer Should Know About Memory"](https://people.freebsd.org/~lstewart/articles/cpumemory.pdf)
  (2007) — sections 3–5 cover cache architecture, TLB, and cache-aware algorithms in
  exhaustive detail. The definitive reference for hardware cache behavior.
- Mike Acton — ["Data-Oriented Design"](https://www.youtube.com/watch?v=rX0ItVEVjHc)
  (CppCon 2014, video) — the most influential talk on SoA layout and thinking about data
  access patterns before code structure.

### Books

- Richard Fabian — *Data-Oriented Design* (2018, free online) —
  [https://www.dataorienteddesign.com/dodbook/](https://www.dataorienteddesign.com/dodbook/) —
  comprehensive treatment of SoA, hot/cold field splitting, and cache-oblivious algorithms
- Brendan Gregg — *Systems Performance* (2nd ed.) — Chapter 7 covers CPU cache
  architecture and cache-miss profiling with `perf`

### Blog Posts

- [Gallery of Processor Cache Effects](http://igoro.com/archive/gallery-of-processor-cache-effects/)
  — Igor Ostrovsky's famous post demonstrating all cache effects with runnable C# code
  and observed results
- [False Sharing Is No Fun](https://mechanical-sympathy.blogspot.com/2011/07/false-sharing.html)
  — Martin Thompson (LMAX Disruptor author) with Java benchmarks showing the effect
- [Go Data Structures: Alignment and Padding](https://go101.org/article/memory-layout.html) —
  Go 101: complete reference for Go struct memory layout

### Tools Documentation

- [`perf c2c`](https://man7.org/linux/man-pages/man1/perf-c2c.1.html) — cache-to-cache
  false sharing detection
- [`pahole`](https://linux.die.net/man/1/pahole) — print holes (padding) in struct
  definitions from DWARF debug info
- [`crossbeam-utils::CachePadded`](https://docs.rs/crossbeam-utils/latest/crossbeam_utils/struct.CachePadded.html)
- [`soa_derive`](https://docs.rs/soa_derive) — Rust proc-macro that generates SoA types
  from AoS struct definitions
