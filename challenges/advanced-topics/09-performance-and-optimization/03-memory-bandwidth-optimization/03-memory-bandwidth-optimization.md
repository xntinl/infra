<!--
type: reference
difficulty: advanced
section: [09-performance-and-optimization]
concepts: [memory-bandwidth, DRAM-bandwidth, prefetching, sequential-vs-random-access, NUMA, hardware-prefetcher, streaming-loads, non-temporal-stores]
languages: [go, rust]
estimated_reading_time: 70 min
bloom_level: evaluate
prerequisites: [cpu-cache-optimization, NUMA-basics, virtual-memory]
papers: [Ulrich Drepper — What Every Programmer Should Know About Memory (2007), Intel 64 and IA-32 Architectures Optimization Reference Manual]
industry_use: [ClickHouse streaming reads, Apache Arrow columnar format, Linux NUMA allocator, Intel MKL, RocksDB block cache]
language_contrast: medium
-->

# Memory Bandwidth Optimization

> When every algorithm change has been made and the hot path still hits DRAM, the CPU
> is waiting for data — and no amount of clever code can outrun the speed of physics.

## Mental Model

After cache optimization (keeping hot data in L1/L2/L3), the next bottleneck is memory
bandwidth — the rate at which data can flow from DRAM to CPU. This is a physical
constraint: a typical DDR4 server channel delivers ~25 GB/s, and modern CPUs have 2–8
channels, giving peak theoretical bandwidth of 50–200 GB/s. But that peak is only
achievable with sequential, prefetcher-friendly access patterns. Random access to DRAM
achieves roughly 5–10% of peak bandwidth because each random access waits for a full
DRAM row activation.

The hardware prefetcher is the CPU's mechanism for hiding DRAM latency. It detects
sequential or strided access patterns and speculatively loads cache lines before the
program requests them. The rule: if the prefetcher can predict your next access, latency
is amortized. If it cannot (random access, pointer chasing), every miss waits for full
DRAM latency (~65 ns, ~200 cycles at 3 GHz).

The bandwidth optimization mental model:

```
Bandwidth-bound operation:
  - IPC (instructions per cycle) is low (~0.3–0.8)
  - "cache-references" is high in perf stat
  - "cache-misses / cache-references" ratio > 10%
  - Doubling compute does not improve throughput

Compute-bound operation:
  - IPC is near theoretical maximum (~2.5–4.0 on modern x86)
  - cache-miss rate is low (<5%)
  - Doubling data throughput improves total throughput linearly
```

If `perf stat` shows low IPC with high cache miss rates, you are bandwidth-bound. The
fix is data layout — pack the data, reduce footprint, improve locality. If IPC is high
and cache misses are low, you are compute-bound. The fix is algorithmic or SIMD.

NUMA (Non-Uniform Memory Access) is the extreme version of bandwidth optimization for
multi-socket servers. Each CPU socket has its own local DRAM. Accessing memory attached
to a remote socket (cross-NUMA access) has 2–3x higher latency and competes for the
inter-socket interconnect bandwidth. A 32-core, 2-socket server with poor NUMA placement
can see 50% of bandwidth wasted on cross-socket traffic.

## Core Concepts

### Hardware Prefetcher Patterns

The hardware prefetcher recognizes these access patterns and prefetches automatically:
- Sequential: access to X, X+64, X+128, X+192 — fully prefetched
- Constant stride: access to X, X+128, X+256 — prefetched if stride ≤ 2048 bytes
- Random: access to arbitrary addresses — not prefetched; every access stalls

The prefetcher operates per cache bank and has a finite "stream" budget (typically 8–16
concurrent streams on Intel CPUs). If your code accesses more than 8–16 independent
streams simultaneously, some streams will not be prefetched.

### Non-Temporal Stores

For write-only operations (e.g., initializing a large buffer, writing results of a
streaming transform), normal stores read the cache line from DRAM into cache before
writing it (a read-for-ownership). For large writes where the data will not be read back
soon, non-temporal stores (`_mm_stream_si128`, `MOVNTDQ` instruction) bypass the cache
entirely and write directly to DRAM. This prevents polluting the cache with data that
will not be reused and can increase write throughput by 30–50%.

In Go, non-temporal stores are not accessible without assembly. In Rust, they are
accessible via `std::arch::x86_64::_mm_stream_si32` and similar intrinsics in `unsafe`
blocks.

### Memory Footprint Reduction

The fastest data is data that fits in cache. Before tuning access patterns, reduce the
data footprint:
- Use `u32` instead of `u64` for values that fit in 32 bits — doubles density per cache line
- Use bit-packing for boolean arrays — 64 booleans per cache line vs 64 bytes per boolean
- Compress hot fields: timestamps as seconds since epoch (4 bytes) vs RFC 3339 string
  (25+ bytes)
- Filter before loading: skip rows/elements early so subsequent processing operates on
  a smaller working set

### NUMA Architecture

```
Socket 0                          Socket 1
  CPU 0–15                          CPU 16–31
  L3 cache (32 MB)                  L3 cache (32 MB)
  Local DRAM: 128 GB                Local DRAM: 128 GB
     |                                    |
     +------------ QPI/UPI ---------------+
           Inter-socket interconnect
```

On Linux: `numactl --hardware` shows NUMA topology. `numactl --membind=0` binds memory
allocation to NUMA node 0. Thread affinity (`taskset`) + memory binding (`numactl`) pins
a workload to a single NUMA node, maximizing bandwidth by eliminating cross-socket traffic.

Go's runtime is not NUMA-aware by default. All goroutines can run on any OS thread, and
Go's allocator does not bind to NUMA nodes. For NUMA-sensitive workloads, call
`runtime.LockOSThread()` + `syscall.RawSyscall(syscall.SYS_SCHED_SETAFFINITY, ...)` to
bind to specific CPUs, and use `mmap` with `MPOL_BIND` for NUMA-local allocation.

Rust programs also get no NUMA awareness from the standard allocator. Use the `numa` crate
or `libnuma` FFI for NUMA-local allocation in latency-critical paths.

## Implementation: Go

```go
package main

import (
	"fmt"
	"math/rand"
	"testing"
	"time"
	"unsafe"
)

const (
	Size    = 64 * 1024 * 1024 // 64 MB — larger than typical L3 cache
	Stride1 = 1                // sequential
	Stride8 = 8                // 8-element stride
	Stride64 = 64              // 64-element stride — likely kills prefetcher
)

// --- Sequential vs Random Access ---
//
// BEFORE: Random access — pointer chasing through a large array.
// Each element points to a random other element: the prefetcher cannot predict next address.

type Node struct {
	next  *Node
	value int64
	_pad  [48]byte // ensure each node is exactly 64 bytes (one cache line)
}

func buildRandomList(n int) *Node {
	nodes := make([]Node, n)
	// Shuffle indices to create a random traversal order
	perm := rand.Perm(n)
	for i := 0; i < n-1; i++ {
		nodes[perm[i]].next = &nodes[perm[i+1]]
	}
	nodes[perm[n-1]].next = nil
	return &nodes[perm[0]]
}

func BenchmarkRandomAccess(b *testing.B) {
	const nodeCount = 1 << 16 // 65536 nodes = 4 MB of nodes
	list := buildRandomList(nodeCount)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Each iteration follows a random pointer — cache miss every step
		sum := int64(0)
		n := list
		for n != nil {
			sum += n.value
			n = n.next
		}
		_ = sum
	}
}

// AFTER: Sequential access — iterate a flat array sequentially.
// The hardware prefetcher loads the next cache line before we need it.

func BenchmarkSequentialAccess(b *testing.B) {
	const count = 1 << 16
	data := make([]int64, count)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sum := int64(0)
		for _, v := range data {
			sum += v
		}
		_ = sum
	}
}

// Expected results (the divergence grows as working set exceeds L3):
//   BenchmarkRandomAccess-8      50   32145000 ns/op   (dominated by DRAM latency)
//   BenchmarkSequentialAccess-8  5000   245000 ns/op   (~130x faster)

// --- Stride Impact ---
// Even without random access, large strides fool the prefetcher.

func strideSum(data []int64, stride int) int64 {
	sum := int64(0)
	for i := 0; i < len(data); i += stride {
		sum += data[i]
	}
	return sum
}

func BenchmarkStride1(b *testing.B) {
	data := make([]int64, Size/8)
	b.SetBytes(int64(unsafe.Sizeof(data[0])) * int64(len(data)/Stride1))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = strideSum(data, Stride1)
	}
}

func BenchmarkStride64(b *testing.B) {
	data := make([]int64, Size/8)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = strideSum(data, Stride64)
	}
}

// --- Demonstrating the bandwidth ceiling ---
// A streaming sum saturates memory bandwidth when the array doesn't fit in L3.

func BenchmarkStreamingSum(b *testing.B) {
	// 64 MB array — much larger than typical L3 (6–32 MB)
	data := make([]int32, Size/4) // int32 to pack more per cache line
	b.SetBytes(int64(len(data)) * 4)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sum := int32(0)
		for _, v := range data {
			sum += v
		}
		_ = sum
	}
	// Report GB/s:
	// GB/s = (b.N * 64MB) / elapsed_seconds / 1e9
	// This benchmark will plateau at your machine's DRAM bandwidth ceiling
	// (typically 20–60 GB/s depending on DDR4/DDR5 and channel count)
}

// --- Prefetch hint in Go (via assembly) ---
// Go does not expose software prefetch directly. The workaround for
// read-ahead is to touch the data in a separate goroutine — which
// gets the data into the shared L3. This is a crude approximation;
// for true software prefetch, use Rust (see below) or Go assembly.

func prefetchAhead(data []int64, i int) {
	if i+64 < len(data) {
		// Reading data[i+64] brings cache line at that address into L2/L3.
		// The loop body at i will then find it cache-warm.
		// This is an approximation of `PREFETCHT0 [data+i+64*8]`.
		_ = data[i+64]
	}
}

func BenchmarkWithSoftwarePrefetch(b *testing.B) {
	data := make([]int64, Size/8)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sum := int64(0)
		for j := 0; j < len(data); j++ {
			prefetchAhead(data, j+64) // prefetch 64 elements ahead
			sum += data[j]
		}
		_ = sum
	}
}

// Note: Go's compiler will likely inline prefetchAhead and the hint load
// may be reordered or eliminated. Real software prefetch in Go requires
// assembly using PREFETCHT0/PREFETCHNTA instructions.

func main() {
	fmt.Printf("Array size: %d MB\n", Size/(1024*1024))
	fmt.Printf("Node size:  %d bytes\n", unsafe.Sizeof(Node{}))
	fmt.Printf("Time: %s\n", time.Now().Format(time.RFC3339))
}
```

### Go-specific Considerations

**No direct prefetch API**: Go's standard library has no API for software prefetch hints.
Competitive performance engineering in Go either relies on the hardware prefetcher
(sequential/strided access) or uses `//go:nosplit` assembly functions with `PREFETCHT0`
instructions. For most workloads, restructuring data to be sequential is preferable to
adding assembly prefetch hints.

**GC and memory locality**: Go's GC moves objects during compaction (in theory — current
GC does not compact, but future versions may). Objects allocated together tend to stay
adjacent. Large allocations (`make([]T, N)` for large N) get their own spans and are
contiguous. Small allocations use size-class spans and may be adjacent to unrelated objects.
For maximum locality, prefer large slices over many small heap allocations.

**`sync.Pool` and GC interaction**: `sync.Pool` objects are cleared at each GC cycle.
If your hot path allocates large temporary buffers, `sync.Pool` can keep them warm between
calls. But the pool is per-P (Go scheduler thread) and objects can migrate between Ps on
context switches — use Pool for allocation reduction, not strict locality guarantees.

## Implementation: Rust

```rust
// To use x86_64 intrinsics: requires nightly or specific target feature.
// Add to build.rs or use RUSTFLAGS="-C target-feature=+sse4.1,+avx2"

#[cfg(target_arch = "x86_64")]
use std::arch::x86_64::{_mm_prefetch, _MM_HINT_T0};

const SIZE: usize = 64 * 1024 * 1024; // 64 MB

// --- Sequential vs Random Access ---

// BEFORE: Random access through linked list (cache-unfriendly)
struct Node {
    next: Option<Box<Node>>,
    value: i64,
    _pad: [u8; 48], // pad to 64 bytes / one cache line
}

// AFTER: Sequential array scan (cache-friendly)
fn sequential_sum(data: &[i64]) -> i64 {
    data.iter().sum()
}

fn random_sum(data: &[i64], indices: &[usize]) -> i64 {
    // Access data in random order specified by indices array.
    // Each access is likely a cache miss for large data arrays.
    indices.iter().map(|&i| data[i]).sum()
}

// --- Software Prefetch in Rust (unsafe) ---
// PREFETCHT0: prefetch to all cache levels (L1/L2/L3)
// PREFETCHNTA: non-temporal prefetch (only L1, minimizes cache pollution
//              for data that will be accessed once)

fn sum_with_prefetch(data: &[i64]) -> i64 {
    let mut sum = 0i64;
    let prefetch_distance = 64usize; // prefetch 64 elements ahead

    for (i, &val) in data.iter().enumerate() {
        // Prefetch 64 elements ahead — at 8 bytes/element, that's 512 bytes = 8 cache lines
        #[cfg(target_arch = "x86_64")]
        if i + prefetch_distance < data.len() {
            unsafe {
                // SAFETY: pointer is within bounds of the slice
                let ptr = data.as_ptr().add(i + prefetch_distance) as *const i8;
                _mm_prefetch(ptr, _MM_HINT_T0);
            }
        }
        sum += val;
    }
    sum
}

// --- Non-temporal stores (write streaming) ---
// For write-only large buffers, bypass cache to avoid read-for-ownership.
// This is important when writing results of a transform that won't be re-read.

#[cfg(target_arch = "x86_64")]
fn memset_nontemporal(dst: &mut [i32], value: i32) {
    use std::arch::x86_64::_mm_stream_si32;
    // SAFETY: _mm_stream_si32 requires 4-byte aligned pointer.
    // &mut i32 is always 4-byte aligned.
    unsafe {
        for chunk in dst.iter_mut() {
            _mm_stream_si32(chunk as *mut i32, value);
        }
        // _mm_sfence ensures the non-temporal stores are visible to other cores
        std::arch::x86_64::_mm_sfence();
    }
}

// Safe fallback for non-x86_64
#[cfg(not(target_arch = "x86_64"))]
fn memset_nontemporal(dst: &mut [i32], value: i32) {
    dst.fill(value);
}

// --- Criterion benchmarks ---
// Cargo.toml: criterion = { version = "0.5", features = ["html_reports"] }

#[cfg(test)]
mod benches {
    use super::*;
    use criterion::{criterion_group, criterion_main, BenchmarkId, Criterion, Throughput};

    fn bench_access_patterns(c: &mut Criterion) {
        let data: Vec<i64> = vec![1i64; SIZE / 8];
        let mut rng_indices: Vec<usize> = (0..data.len()).collect();
        // Fisher-Yates shuffle
        for i in (1..rng_indices.len()).rev() {
            let j = rand::random::<usize>() % (i + 1);
            rng_indices.swap(i, j);
        }

        let mut group = c.benchmark_group("access_patterns");
        group.throughput(Throughput::Bytes((SIZE) as u64));

        group.bench_function("sequential", |b| {
            b.iter(|| criterion::black_box(sequential_sum(&data)))
        });

        group.bench_function("random", |b| {
            b.iter(|| criterion::black_box(random_sum(&data, &rng_indices)))
        });

        group.bench_function("sequential_with_prefetch", |b| {
            b.iter(|| criterion::black_box(sum_with_prefetch(&data)))
        });

        group.finish();
    }

    criterion_group!(benches, bench_access_patterns);
    criterion_main!(benches);
}

fn main() {
    let data: Vec<i64> = vec![1i64; 1024];
    let sequential = sequential_sum(&data);

    let indices: Vec<usize> = (0..data.len()).rev().collect(); // reverse order
    let random = random_sum(&data, &indices);

    println!("Sequential sum: {}", sequential);
    println!("Random (reverse) sum: {}", random);
    assert_eq!(sequential, random, "Both should sum to the same value");

    let mut buf = vec![0i32; 1024];
    memset_nontemporal(&mut buf, 42);
    assert!(buf.iter().all(|&v| v == 42));
    println!("Non-temporal memset: OK");
}
```

### Rust-specific Considerations

**`target-feature` and intrinsics**: Software prefetch intrinsics require the target CPU
to support them. Enable with `RUSTFLAGS="-C target-feature=+sse4.1"` or in `.cargo/config.toml`:
```toml
[target.x86_64-unknown-linux-gnu]
rustflags = ["-C", "target-cpu=native"]
```
`target-cpu=native` enables all features of the build machine's CPU.

**`std::hint::prefetch_read_data` (nightly)**: Rust nightly provides
`std::hint::prefetch_read_data(ptr, locality)` as a safe, portable prefetch API. Once
stabilized, this will be the preferred alternative to `_mm_prefetch`. Check the tracking
issue before using it in production code.

**NUMA and Rust allocator**: The default Rust global allocator (`malloc`/`jemalloc`) is
not NUMA-aware. For NUMA-local allocation, use the `libnuma` crate (FFI to `libnuma`)
or the `hwlocality` crate which provides Rust bindings to `hwloc` (the hardware locality
library). Pin threads to cores with `core_affinity` crate + NUMA-local mmap for the
tightest latency control.

**`Vec` and memory fragmentation**: Many small `Vec` allocations across a long-lived
server accumulate fragmentation. Use arena allocators (`typed-arena`, `bumpalo`) for
short-lived groups of allocations: allocate many objects into an arena, process them,
drop the arena at once. This keeps related objects spatially adjacent and reduces DRAM
bandwidth by improving cache reuse.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Software prefetch API | None in stdlib; requires assembly | `_mm_prefetch` via `std::arch` (unsafe) |
| Non-temporal stores | No direct access | `_mm_stream_*` via `std::arch` (unsafe) |
| NUMA-aware allocation | Via `mmap + MPOL_BIND` syscall | Via `libnuma` FFI or `hwlocality` crate |
| Sequential scan auto-optimization | Compiler generates efficient loops | LLVM vectorizes and unrolls aggressively |
| GC and memory locality | GC may move short-lived objects' neighbors | Deterministic; layout guaranteed stable |
| Arena allocation | `sync.Pool` (approximate) | `bumpalo`, `typed-arena` (precise) |
| Bandwidth measurement | `b.SetBytes(n)` in benchmarks (reports MB/s) | `criterion::Throughput::Bytes(n)` |
| Streaming bandwidth ceiling | Same hardware limit for both languages | Same hardware limit for both languages |

## Production War Stories

**Apache Arrow and columnar memory**: Apache Arrow defines an in-memory columnar format
where each column is a contiguous buffer with known alignment (64-byte aligned by spec).
Arrow's design decision: sequential access to one column should achieve near-peak memory
bandwidth. The `arrow2` crate in Rust and `go-arrow` implement this format; their query
engines process columns as contiguous slices, achieving 40–80 GB/s on modern servers for
filter and projection operations.

**RocksDB Block Cache (Meta, 2021)**: RocksDB's block cache stores compressed data blocks.
A benchmarking effort at Meta found that random access to the block cache — caused by
LRU eviction scattering hot blocks across memory — was the bottleneck for read-heavy
workloads. They switched to a two-level cache: small DRAM cache for hot blocks (random
access acceptable) and a larger NVMe SSD cache for warm blocks. The key insight: DRAM
bandwidth is finite and must be protected for the hottest data.

**NUMA-aware Kafka broker (LinkedIn, 2019)**: LinkedIn's high-throughput Kafka brokers
run on 2-socket servers. They found that uncontrolled OS thread scheduling caused
network receive threads to run on Socket 0 while the disk I/O threads ran on Socket 1 —
all inter-thread communication crossed the NUMA boundary. Pinning receive threads to
Socket 0 cores and disk threads to Socket 1 cores eliminated cross-socket traffic and
improved P99 producer latency by 35%.

## Numbers That Matter

| Scenario | Bandwidth or Latency |
|----------|---------------------|
| L1 → register bandwidth | ~1 TB/s (theoretical) |
| L2 → L1 bandwidth | ~500 GB/s |
| L3 → L2 bandwidth | ~100–200 GB/s |
| DRAM → CPU (single channel DDR4-3200) | ~25 GB/s |
| DRAM → CPU (dual-channel DDR4-3200) | ~50 GB/s |
| DRAM → CPU (8-channel DDR4-3200, server) | ~200 GB/s |
| Sequential scan of 64 MB array | 30–50 GB/s (hardware-prefetched, L3→DRAM) |
| Random pointer chase (4 MB array, no L3) | 1–3 GB/s (latency-limited, ~65 ns/access) |
| Cross-NUMA access penalty | 2–3x higher latency, 50% lower bandwidth |
| Non-temporal write vs normal write (large buffer) | 30–50% higher throughput |

## Common Pitfalls

**Assuming bandwidth is the bottleneck without measuring**: A hot loop that looks like a
streaming scan may actually fit in L3 — in which case it is compute-bound, not bandwidth-
bound. Always check: `perf stat -e cache-misses,cache-references`. If miss rate is low,
the data is in cache and you are optimizing the wrong thing.

**Prefetching too aggressively**: Software prefetch that is too far ahead pollutes the
cache with data that gets evicted before use. A prefetch distance of 8–16 cache lines
(512–1024 bytes) is typical for sequential scans. For larger structs or irregular access,
the correct distance must be measured. Too little: prefetch doesn't arrive in time.
Too much: prefetched data evicts other useful data.

**NUMA binding on containers**: Container environments (Docker, Kubernetes) may not expose
NUMA topology to the process. `numactl` inside a container may not work as expected.
Verify with `numactl --hardware` inside the container before designing NUMA-pinned
allocation strategies.

**Optimizing bandwidth before cache**: If the working set fits in L3 (most workloads
under 32 MB), bandwidth optimization has no effect. Check working set size before
pursuing NUMA affinity, non-temporal stores, or prefetch tuning. The cache hierarchy
absorbs bandwidth pressure for in-cache workloads.

**Sequential scans that are actually scattered**: A Go `for _, v := range slice` loop
looks sequential, but if the slice contains pointers to heap objects, each pointer
dereference follows a potentially random address. The loop structure is sequential; the
access pattern is not. Profile with `perf stat -e cache-misses` to confirm.

## Exercises

**Exercise 1** (30 min): Write a Go benchmark that compares sequential array sum vs
linked-list traversal for the same number of elements. Run with `b.SetBytes(N*8)` to
report bandwidth. Increase the array size from 1 KB to 256 MB and observe where the
bandwidth plateau occurs (marks the L3 cache capacity).

**Exercise 2** (2–4h): Implement a matrix transpose in Go for an N×N matrix of `float64`
values (N = 4096). Naive transpose has poor cache behavior for large matrices. Implement
a cache-blocking version that tiles the transpose into 64×64 blocks. Benchmark both.
Explain why blocking improves performance in terms of cache line utilization.

**Exercise 3** (4–8h): In Rust, implement a streaming dot product of two `f32` arrays
that are larger than L3 cache (128 MB each). Measure bandwidth with criterion's
`Throughput::Bytes`. Add software prefetch hints. Compare: no prefetch vs manual
prefetch vs LLVM auto-vectorized (release build). Use `perf stat -e cache-misses` to
confirm whether prefetch reduces misses.

**Exercise 4** (8–15h): On a two-NUMA-socket Linux server (or via emulation with
`numactl --hardware`), write a benchmark that allocates a 2 GB buffer and measures
read throughput when: (a) threads and memory are on the same NUMA node; (b) threads
are on one node, memory on the other. Measure the bandwidth and latency difference.
Then write a NUMA-aware version that binds both thread affinity and memory policy to
the same node. Verify the improvement.

## Further Reading

### Foundational Papers

- Ulrich Drepper — ["What Every Programmer Should Know About Memory"](https://people.freebsd.org/~lstewart/articles/cpumemory.pdf)
  (2007) — sections 6–7 cover prefetching, NUMA architecture, and non-temporal memory
  access in full technical depth
- Intel — ["Intel 64 and IA-32 Architectures Optimization Reference Manual"](https://www.intel.com/content/www/us/en/developer/articles/technical/intel-sdm.html)
  — Chapter 9 covers memory access optimization including prefetch guidelines and
  non-temporal store usage

### Books

- Brendan Gregg — *Systems Performance* (2nd ed., 2020) — Chapter 7 covers DRAM
  architecture, bandwidth measurement, and NUMA profiling
- Denis Bakhvalov — *Performance Analysis and Tuning on Modern CPUs* (2020, free online)
  — Chapter 8 covers memory access optimization with perf annotations

### Blog Posts

- [Latency Numbers Every Programmer Should Know](https://norvig.com/21-days.html#answers)
  (Peter Norvig, updated) — the canonical reference card
- [What Every Programmer Should Know About Memory — summary](https://lwn.net/Articles/250967/)
  (LWN summary of Drepper's paper)
- [NUMA Deep Dive](https://frankdenneman.nl/2016/07/06/numa-deep-dive-series/)
  (Frank Denneman) — series covering NUMA architecture from a systems administrator
  and application developer perspective

### Tools Documentation

- [`numactl`](https://linux.die.net/man/8/numactl) — NUMA binding for processes
- [`perf mem`](https://man7.org/linux/man-pages/man1/perf-mem.1.html) — memory access profiling
- [`core_affinity`](https://docs.rs/core_affinity) — Rust CPU pinning
- [`hwlocality`](https://docs.rs/hwlocality) — Rust bindings to hwloc for topology-aware allocation
