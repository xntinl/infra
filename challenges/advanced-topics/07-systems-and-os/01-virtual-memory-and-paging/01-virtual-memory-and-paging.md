<!--
type: reference
difficulty: advanced
section: [07-systems-and-os]
concepts: [virtual-memory, page-tables, TLB, huge-pages, mmap, madvise, page-faults, NUMA]
languages: [go, rust]
estimated_reading_time: 60 min
bloom_level: analyze
prerequisites: [pointer-arithmetic, basic-linux-cli, go-or-rust-intermediate]
papers: [Barr2010-translation-caching, Navarro2002-hugepages]
industry_use: [PostgreSQL, RocksDB, Redis, ClickHouse, Go-runtime-GC]
language_contrast: medium
-->

# Virtual Memory and Paging

> Understanding the kernel's virtual-to-physical address translation pipeline is the
> prerequisite for every performance optimization that touches memory: allocator design,
> GC tuning, huge page adoption, NUMA placement, and zero-copy I/O.

## Mental Model

Every process lives inside a lie. The addresses a program manipulates — the pointer
returned by `malloc`, the base of a stack frame, the address of a global variable —
are virtual addresses. They have no fixed relationship to physical DRAM until the CPU's
Memory Management Unit (MMU) translates them. That translation happens on every memory
access, and it is the single most frequently executed operation on modern hardware.

The translation is defined by the page table, a multi-level tree structure that the OS
maintains in physical memory. On x86-64 Linux, four levels of tables map a 48-bit virtual
address to a 52-bit physical address in 4 KB chunks (pages). Each level of the tree is
itself a page-sized array of 512 8-byte entries. A full walk costs four sequential memory
reads — four cache misses in the worst case — which is why the CPU caches recent
translations in the TLB (Translation Lookaside Buffer). A TLB miss on a cold page table
entry costs roughly 50–200 ns. A TLB hit costs roughly 1 ns. Everything about memory
performance optimization is about controlling TLB residency.

Huge pages (2 MB or 1 GB on x86-64) collapse a 512-entry second-level subtree into a
single TLB entry. If your working set fits in fewer huge-page TLB entries than 4 KB-page
TLB entries, huge pages improve performance purely by reducing translation overhead. This
is why databases like PostgreSQL, ClickHouse, and most in-memory stores benefit from
transparent huge pages (THP) or explicit `MAP_HUGETLB` mappings. The GC's heap scan in
Go benefits from the same effect: the runtime allocates spans from the OS via `mmap` and
since Go 1.21 requests huge pages via `MADV_HUGEPAGE`.

The third critical concept is the distinction between virtual memory reservation and
physical memory commitment. `mmap` reserves virtual address space without touching physical
memory. Physical pages are allocated lazily by the page fault handler when the program
first writes to each page. This is not a curiosity — it is the mechanism that allows the
Go and Java runtimes to pre-reserve large virtual address ranges for the heap without
committing RAM upfront. It also means OOM conditions can appear much later than the
allocation that caused them, which is a source of production surprises.

## Core Concepts

### 4-Level Page Table (x86-64)

A 64-bit virtual address on Linux is interpreted as:

```
Bits 63–48: sign extension (must match bit 47)
Bits 47–39: PGD index (page global directory) — level 1
Bits 38–30: PUD index (page upper directory)  — level 2
Bits 29–21: PMD index (page middle directory) — level 3
Bits 20–12: PTE index (page table entry)      — level 4
Bits 11–0:  offset within the 4 KB page
```

Each level is a 512-entry array. Each entry is 8 bytes and contains a physical address
(bits 51–12), plus flags: Present, Writable, User/Supervisor, NX (no-execute),
Accessed, Dirty. The CPU hardware walker (page table walker) reads these flags and raises
faults when they are violated — that is how copy-on-write, demand paging, and guard pages
are implemented.

With 5-level paging (LA57, enabled on recent Intel/AMD and Linux kernels) a 57-bit virtual
address space is supported, adding a fifth table level (PGD5). Most production workloads
still run with 4-level paging.

### TLB Shootdowns in Multicore Systems

The TLB is per-CPU. When the kernel modifies a page table entry (because a page was
unmapped, a CoW fault was resolved, or `munmap` was called), it must invalidate the
corresponding TLB entries on every CPU that might have cached that translation. This is
done via an inter-processor interrupt (IPI) — the modifying CPU broadcasts a `INVLPG`
shootdown to all CPUs in the process's `mm_cpumask`. In a 96-core machine, a single
`munmap` on a large region can block all 96 cores for the duration of the shootdown.

This is why fragmented `munmap` calls on multi-threaded workloads are expensive, and why
systems like `jemalloc` batch `madvise(MADV_FREE)` calls rather than immediately returning
pages to the OS. It is also why Go's runtime holds onto freed memory for several GC cycles
before calling `madvise(MADV_DONTNEED)` — to avoid constant shootdown storms.

### Huge Pages

Linux supports two paths to huge pages:

1. **Transparent Huge Pages (THP)**: The kernel promotes aligned 2 MB regions to huge
   pages automatically. Controlled via `/sys/kernel/mm/transparent_hugepage/enabled`.
   THP compaction (the `khugepaged` daemon) can cause latency spikes as it migrates pages.

2. **Explicit Huge Pages**: `mmap(..., MAP_HUGETLB, ...)` allocates from a pre-reserved
   pool of huge pages (`/proc/sys/vm/nr_hugepages`). No compaction latency; reservation
   fails immediately if the pool is exhausted. Used by databases that cannot afford THP
   jitter.

### madvise Hints

`madvise(addr, len, advice)` tells the kernel how you intend to use a memory region.
Useful values:

| Flag | Effect |
|------|--------|
| `MADV_SEQUENTIAL` | Prefetch aggressively; drop pages after first pass |
| `MADV_RANDOM` | Disable read-ahead; each page is needed exactly once |
| `MADV_DONTNEED` | Drop pages from RAM; next access faults them back in |
| `MADV_FREE` | Lazy free; pages may be reclaimed before next access |
| `MADV_HUGEPAGE` | Enable THP for this region |
| `MADV_NOHUGEPAGE` | Disable THP for this region |
| `MADV_POPULATE_READ` | Pre-fault pages (read) — avoid demand-paging latency |
| `MADV_POPULATE_WRITE` | Pre-fault pages (write) — allocate physical pages now |

`MADV_FREE` is cheaper than `MADV_DONTNEED` for pages the allocator might reuse because
the kernel can skip zeroing them if they are reclaimed. On Linux ≥ 4.5 it is the preferred
hint for allocator-free paths.

## Implementation: Go

```go
package main

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const (
	pageSize      = 4096
	hugepageSize  = 2 * 1024 * 1024 // 2 MB
	mappingSize   = 64 * 1024 * 1024 // 64 MB
)

// mmapAnon maps anonymous memory. Equivalent to malloc for large allocations,
// but the OS does not commit physical pages until first access.
func mmapAnon(size int) ([]byte, error) {
	data, err := syscall.Mmap(
		-1, 0, size,
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_ANON|syscall.MAP_PRIVATE,
	)
	if err != nil {
		return nil, fmt.Errorf("mmap failed: %w", err)
	}
	return data, nil
}

// mmapHuge requests a 2 MB huge-page-backed mapping.
// Requires CAP_IPC_LOCK or a non-empty huge page pool:
//   echo 64 > /sys/kernel/mm/hugepages/hugepages-2048kB/nr_hugepages
func mmapHuge(size int) ([]byte, error) {
	// MAP_HUGETLB | (21 << MAP_HUGE_SHIFT) requests 2 MB huge pages.
	// The shift encodes the log2 of the page size.
	const MAP_HUGETLB = 0x40000
	const MAP_HUGE_2MB = 21 << 26 // MAP_HUGE_SHIFT = 26

	fd := -1
	data, _, errno := syscall.Syscall6(
		syscall.SYS_MMAP,
		0,
		uintptr(size),
		uintptr(syscall.PROT_READ|syscall.PROT_WRITE),
		uintptr(syscall.MAP_ANON|syscall.MAP_PRIVATE|MAP_HUGETLB|MAP_HUGE_2MB),
		uintptr(fd),
		0,
	)
	if errno != 0 {
		return nil, fmt.Errorf("mmap huge failed: %w", errno)
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(data)), size), nil
}

// adviseSequential hints to the kernel that we will scan the region linearly.
// The kernel will read-ahead pages and drop them after scanning.
func adviseSequential(data []byte) error {
	_, _, errno := syscall.Syscall(
		syscall.SYS_MADVISE,
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(len(data)),
		syscall.MADV_SEQUENTIAL,
	)
	if errno != 0 {
		return fmt.Errorf("madvise sequential: %w", errno)
	}
	return nil
}

// adviseHugepage asks the kernel to promote this region to huge pages (THP path).
// Only works if the kernel has THP enabled (madvise or always mode).
func adviseHugepage(data []byte) error {
	const MADV_HUGEPAGE = 14
	_, _, errno := syscall.Syscall(
		syscall.SYS_MADVISE,
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(len(data)),
		MADV_HUGEPAGE,
	)
	if errno != 0 {
		return fmt.Errorf("madvise hugepage: %w", errno)
	}
	return nil
}

// prefaultPages forces the OS to allocate physical pages for the entire mapping
// by writing one byte per page. This trades latency at startup for zero
// demand-paging stalls during serving — useful for latency-sensitive paths.
func prefaultPages(data []byte) {
	for i := 0; i < len(data); i += pageSize {
		data[i] = 0
	}
}

// readMapsEntry reads /proc/self/smaps and prints the entry for the given address.
// This is how you verify huge page promotion in production.
func readMapsEntry(addr uintptr) {
	data, err := os.ReadFile("/proc/self/smaps")
	if err != nil {
		fmt.Println("cannot read smaps:", err)
		return
	}
	// A real implementation would parse the entries; here we just dump the raw file.
	// Look for "AnonHugePages:" > 0 to confirm THP promotion.
	_ = addr
	fmt.Printf("smaps excerpt (first 512 bytes):\n%.512s\n", data)
}

func main() {
	// --- Anonymous mmap (normal 4 KB pages) ---
	fmt.Println("=== Anonymous mmap (4 KB pages) ===")
	region, err := mmapAnon(mappingSize)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer syscall.Munmap(region)

	// Advise sequential before the scan — the kernel will prefetch ahead of us.
	if err := adviseSequential(region); err != nil {
		fmt.Println("warning:", err)
	}

	// Prefault to eliminate demand-paging noise in the benchmark below.
	prefaultPages(region)

	// Touch every page to measure access pattern.
	sum := 0
	for i := 0; i < len(region); i += pageSize {
		sum += int(region[i])
	}
	fmt.Printf("Sum (forces TLB walk on every page): %d\n", sum)

	// --- THP path ---
	fmt.Println("\n=== Transparent Huge Pages (MADV_HUGEPAGE) ===")
	// Huge-page-aligned size required for THP promotion.
	thpRegion, err := mmapAnon(mappingSize)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer syscall.Munmap(thpRegion)

	if err := adviseHugepage(thpRegion); err != nil {
		fmt.Println("THP hint not supported or THP disabled:", err)
	} else {
		prefaultPages(thpRegion) // must touch pages to trigger promotion
		fmt.Println("THP region allocated and prefaulted.")
		// After prefaulting: cat /proc/self/smaps | grep -A 20 '<addr>'
		// and look for "AnonHugePages: 65536 kB" (= 64 MB in 2 MB huge pages)
	}

	// Print smaps excerpt so you can see the memory layout.
	readMapsEntry(uintptr(unsafe.Pointer(&thpRegion[0])))

	fmt.Println("\nDone. Check /proc/self/smaps (PID", os.Getpid(), ") for huge page stats.")
}
```

### Go-specific considerations

The Go runtime manages its own heap via `mheap`, which requests memory from the OS using
`mmap`. Since Go 1.21, `mheap` calls `madvise(MADV_HUGEPAGE)` on spans it allocates,
which means most Go heap allocations automatically benefit from THP without any user code
change. However, this only works if THP is set to `madvise` mode:
`echo madvise > /sys/kernel/mm/transparent_hugepage/enabled`.

The `syscall.Mmap` function acquires the goroutine's P (processor), issues the syscall,
and returns. Unlike blocking I/O syscalls, `mmap` does not yield the goroutine to the
scheduler — it blocks the OS thread. For large `mmap` calls (e.g., mapping a 10 GB
database file), this can stall other goroutines on the same thread. Keep large `mmap`
calls off the hot path, or wrap them with `runtime.LockOSThread` to prevent goroutine
migration mid-call.

The Go garbage collector's mark phase scans the heap by iterating over `mheap.arenas`.
Each arena is a 64 MB aligned region. The GC respects the virtual memory layout:
`MADV_DONTNEED` pages are not physically present and do not contribute to GC scan time.

## Implementation: Rust

```rust
use std::ptr;

// We use the libc crate for raw syscall access.
// Add to Cargo.toml: libc = "0.2"
use libc::{
    madvise, mmap, munmap, MADV_HUGEPAGE, MADV_SEQUENTIAL, MAP_ANON, MAP_HUGETLB,
    MAP_PRIVATE, PROT_READ, PROT_WRITE,
};

const PAGE_SIZE: usize = 4096;
const HUGE_PAGE_SIZE: usize = 2 * 1024 * 1024;
const MAPPING_SIZE: usize = 64 * 1024 * 1024;

struct MmapRegion {
    ptr: *mut libc::c_void,
    len: usize,
}

impl MmapRegion {
    // Anonymous mapping — no file backing. Physical pages are committed on first access.
    fn anon(size: usize) -> Result<Self, String> {
        let ptr = unsafe {
            mmap(
                ptr::null_mut(),
                size,
                PROT_READ | PROT_WRITE,
                MAP_ANON | MAP_PRIVATE,
                -1,
                0,
            )
        };
        if ptr == libc::MAP_FAILED {
            return Err(format!("mmap failed: errno {}", unsafe { *libc::__errno_location() }));
        }
        Ok(Self { ptr, len: size })
    }

    // Huge page mapping via MAP_HUGETLB (requires pre-allocated huge page pool).
    // MAP_HUGE_2MB = MAP_HUGETLB | (21 << MAP_HUGE_SHIFT)
    fn huge(size: usize) -> Result<Self, String> {
        // Size must be a multiple of HUGE_PAGE_SIZE.
        let aligned = (size + HUGE_PAGE_SIZE - 1) & !(HUGE_PAGE_SIZE - 1);
        const MAP_HUGE_2MB: i32 = MAP_HUGETLB | (21 << 26);
        let ptr = unsafe {
            mmap(
                ptr::null_mut(),
                aligned,
                PROT_READ | PROT_WRITE,
                MAP_ANON | MAP_PRIVATE | MAP_HUGE_2MB,
                -1,
                0,
            )
        };
        if ptr == libc::MAP_FAILED {
            return Err(format!(
                "mmap huge failed: errno {} (is nr_hugepages > 0?)",
                unsafe { *libc::__errno_location() }
            ));
        }
        Ok(Self { ptr, len: aligned })
    }

    fn as_slice(&self) -> &[u8] {
        unsafe { std::slice::from_raw_parts(self.ptr as *const u8, self.len) }
    }

    fn as_slice_mut(&mut self) -> &mut [u8] {
        unsafe { std::slice::from_raw_parts_mut(self.ptr as *mut u8, self.len) }
    }

    // Advise the kernel that we will read linearly through this region.
    // The kernel will aggressively prefetch and drop pages behind us.
    fn advise_sequential(&self) -> Result<(), String> {
        let rc = unsafe { madvise(self.ptr, self.len, MADV_SEQUENTIAL) };
        if rc != 0 {
            return Err(format!("madvise sequential failed: {rc}"));
        }
        Ok(())
    }

    // Request THP promotion for this region. Requires THP in "madvise" mode.
    fn advise_hugepage(&self) -> Result<(), String> {
        let rc = unsafe { madvise(self.ptr, self.len, MADV_HUGEPAGE) };
        if rc != 0 {
            return Err(format!("madvise hugepage failed: {rc}"));
        }
        Ok(())
    }

    // Pre-fault all pages by writing one byte per page.
    // Eliminates demand-paging latency from the hot path.
    fn prefault(&mut self) {
        let slice = self.as_slice_mut();
        for i in (0..slice.len()).step_by(PAGE_SIZE) {
            slice[i] = 0;
        }
    }
}

impl Drop for MmapRegion {
    fn drop(&mut self) {
        // Safety: ptr and len came from a successful mmap call and have not been freed.
        unsafe {
            munmap(self.ptr, self.len);
        }
    }
}

fn main() {
    println!("=== Anonymous mmap (4 KB pages) ===");

    let mut region = MmapRegion::anon(MAPPING_SIZE).expect("anon mmap");
    region.advise_sequential().unwrap_or_else(|e| println!("warn: {e}"));
    region.prefault();

    // Scan every page — exercises TLB for all entries.
    let sum: u64 = region
        .as_slice()
        .iter()
        .step_by(PAGE_SIZE)
        .map(|b| *b as u64)
        .sum();
    println!("Page-strided sum: {sum}");

    println!("\n=== Transparent Huge Pages (MADV_HUGEPAGE) ===");
    let mut thp_region = MmapRegion::anon(MAPPING_SIZE).expect("thp mmap");
    match thp_region.advise_hugepage() {
        Ok(()) => {
            thp_region.prefault(); // must touch pages to trigger promotion
            println!("THP region prefaulted ({} MB)", MAPPING_SIZE / 1024 / 1024);
            println!("Verify: grep AnonHugePages /proc/{}/smaps", std::process::id());
        }
        Err(e) => println!("THP not available: {e}"),
    }

    println!("\n=== Explicit Huge Page Mapping (MAP_HUGETLB) ===");
    match MmapRegion::huge(MAPPING_SIZE) {
        Ok(mut hr) => {
            hr.prefault();
            println!("Explicit huge pages: {} bytes mapped", hr.len);
        }
        Err(e) => println!("Huge page pool empty or insufficient: {e}"),
    }
}
```

### Rust-specific considerations

Rust's ownership model makes `MmapRegion` safe to use despite the underlying `unsafe`
syscalls. The `Drop` implementation guarantees `munmap` is called exactly once, even on
panic — a property that requires discipline to achieve in C. The `unsafe` blocks are
small and well-commented, which is the idiomatic Rust approach: push `unsafe` to the
smallest possible scope and encapsulate it behind a safe API boundary.

The `nix` crate (`nix = "0.27"`) provides higher-level wrappers around `mmap` and
`madvise` via `nix::sys::mman`, removing the need to call `libc` directly. For
production code, prefer `nix` — it handles errno conversion and provides typed flags
(`ProtFlags`, `MapFlags`, `MmapAdvise`) that prevent accidental flag combinations.

For memory-mapped files specifically (e.g., mapping a database or a large dataset),
consider the `memmap2` crate, which wraps `mmap` file operations in a safe, cross-platform
API. `memmap2::MmapOptions::new().populate().map_mut(&file)` triggers the equivalent of
`MAP_POPULATE`, pre-faulting all pages before the mapping is returned.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|----|------|
| mmap API | `syscall.Mmap` (returns `[]byte`) | `libc::mmap` or `nix::sys::mman::mmap` |
| Safety | Slice bounds checked at runtime | `unsafe` required; bounds checking in safe wrappers |
| Huge pages | `MADV_HUGEPAGE` via `syscall.Syscall(SYS_MADVISE, ...)` | `libc::madvise` or `nix::sys::mman::madvise` |
| Cleanup | Manual `syscall.Munmap(data)` | `Drop` trait guarantees cleanup |
| Runtime interference | GC may add `MADV_DONTNEED` to returned heap spans | No runtime; no interference |
| Huge page library support | memmap2-go for file maps | `memmap2` crate |
| NUMA awareness | No stdlib support; use `numactl` or raw `mbind` syscall | `libc::mbind` or `hwloc` crate |
| Production ergonomics | Easier for quick scripts | More control; better for library code |

## Production War Stories

**RocksDB and huge pages**: RocksDB's block cache (a large in-memory LRU of SST data)
allocates its buffers via `mmap` with `MAP_HUGETLB`. Without huge pages, the block cache
of a 128 GB instance requires 32 million 4 KB page table entries. With 2 MB huge pages,
that collapses to 65,536 entries. Meta reported a 10–15% read throughput increase on
their ZippyDB deployment solely from enabling huge pages on the block cache, with no
application code changes.

**Go GC and THP interaction**: Early versions of Go's runtime (before 1.12) called
`madvise(MADV_DONTNEED)` aggressively to return unused heap memory to the OS. This
fragmented huge page mappings, preventing THP from promoting regions that the GC had
partially freed. The runtime was changed to prefer `MADV_FREE` (which lets the kernel
decide when to reclaim), and on Linux the `GOGC` and `GOMEMLIMIT` knobs interact with
THP in ways that still surprise teams: a GOMEMLIMIT-induced GC that calls DONTNEED on
half the heap can cause a latency spike 50–100 ms later when those pages must be faulted
back in.

**PostgreSQL shared_buffers**: PostgreSQL uses a single large `mmap` (or `shmget` on
older configs) for its shared buffer pool. On servers with THP in `always` mode,
`khugepaged` periodically scans and promotes pages in the background, causing 5–20 ms
stalls that appear as random query latency spikes. The recommended PostgreSQL configuration
is THP in `madvise` mode plus explicit `MADV_HUGEPAGE` on the shared buffer region —
avoiding compaction jitter while still getting TLB benefit.

**OOM killer and overcommit**: Linux's default overcommit policy (`vm.overcommit_memory=0`)
lets processes `mmap` more virtual memory than physical RAM exists, relying on the fact
that most processes never touch all their reserved space. When actual RSS exceeds available
RAM plus swap, the OOM killer fires. The victim is chosen by `oom_score_adj`. Kubernetes
sets `oom_score_adj` to 1000 for Burstable pods (no memory limit), meaning they are killed
first. A production Kubernetes node that went OOM killed a Go service that had issued a
64 GB `mmap` reservation for a future feature — the reservation itself was counted by the
kernel's heuristics, not just RSS.

## Complexity Analysis

| Operation | Latency | Notes |
|-----------|---------|-------|
| TLB hit | ~1 ns | L1 TLB, single cycle |
| TLB miss (L2 TLB hit) | ~5 ns | Unified L2 TLB, shared |
| TLB miss (page table walk, cached PTEs) | ~15–30 ns | PTEs in L1/L2 cache |
| TLB miss (page table walk, cold) | ~50–200 ns | 4 cache misses at ~50 ns each |
| Page fault (minor, page in TLB already zero'd) | ~500 ns | Kernel fast path |
| Page fault (major, disk read) | 1–10 ms | Depends on storage latency |
| `mmap` syscall (anonymous, no physical allocation) | ~500 ns | Just VA reservation |
| `munmap` (no TLB shootdown) | ~500 ns | Single-threaded case |
| `munmap` (64-core TLB shootdown) | ~50–200 µs | IPI to all 64 cores |
| `madvise(MADV_DONTNEED)` on 1 GB | ~1–5 ms | Kernel must zero PTEs |
| THP promotion by `khugepaged` | ~1–20 ms | Compaction, page migration |

The shootdown cost is the dominant hidden cost in multi-threaded memory management.
Any `munmap` on a region that other threads have recently accessed triggers IPIs to all
CPUs in the process's `mm_cpumask`. At 96 cores, a 1 GB `munmap` in a shared process
(e.g., a multi-threaded server) can block all 96 cores for ~200 µs.

## Common Pitfalls

1. **Calling `munmap` on the hot path in multi-threaded code.** Every `munmap` on a
   process with N threads sends N-1 IPIs. Allocators mitigate this by batching returns
   to the OS. If you are calling `munmap` directly in a loop, you are serializing all
   your threads.

2. **Mapping large files with `MAP_SHARED` without `MAP_POPULATE`, then issuing random
   reads.** Every first access to a 4 KB page triggers a major fault (disk read). For
   sequential reads this is fine; for random access patterns (B-tree lookups, hash table
   probes) the fault latency dominates. Solution: `madvise(MADV_POPULATE_READ)` or
   `madvise(MADV_RANDOM)` to suppress prefetch.

3. **Expecting `mmap` to immediately consume RAM.** Logging "allocated 100 GB" after a
   `mmap` call and then being confused by low RSS is a common mistake. Virtual address
   reservation and physical page allocation are separate events. Use `smaps` or
   `RssAnon` from `/proc/self/status` to measure committed memory.

4. **Enabling THP globally (`always`) on a latency-sensitive service.** The `khugepaged`
   compaction daemon periodically wakes up and migrates pages to form 2 MB regions. These
   migrations stall access to the affected pages. The safe setting for most services is
   `madvise`, so only explicitly opted-in regions are promoted.

5. **Using `mmap` for small allocations.** Each `mmap` call has a minimum granularity of
   one page (4 KB). A 16-byte allocation via `mmap` wastes 4080 bytes. Below ~64 KB,
   prefer `malloc`/`new` which draw from thread-local caches with zero kernel involvement.

## Exercises

**Exercise 1** (30 min): Write a program that maps a 256 MB anonymous region, measures
the time to fault in all pages (prefault loop vs. on-demand access), and reports RSS
before and after. Read `/proc/self/status` for `VmRSS` and `VmVirt`. Observe the
difference between virtual size and resident size.

**Exercise 2** (2–4 h): Implement a simple sequential file scanner using three strategies:
(a) `read()` syscall loop, (b) `mmap` with `MADV_SEQUENTIAL`, (c) `mmap` with
`MADV_POPULATE_READ`. Benchmark all three on a 2 GB file using `time` and `perf stat -e
cache-misses,dTLB-load-misses`. Explain the differences.

**Exercise 3** (4–8 h): Build a "huge page allocator" in Go or Rust that wraps `mmap`
with `MAP_HUGETLB`, falls back to regular pages if the huge page pool is exhausted, and
reports TLB efficiency via `/proc/self/smaps`. Test it with a workload that does
random access over a 512 MB buffer. Compare TLB miss rates with and without huge pages
using `perf stat -e dTLB-load-misses`.

**Exercise 4** (8–15 h): Implement a memory-mapped key-value store: a file-backed hash
table where buckets live in a `mmap`'d file. Implement `put`, `get`, and `delete`. Handle
the case where the file needs to grow (hint: `ftruncate` + `mremap`). Add `MADV_RANDOM`
for the hash bucket region and `MADV_SEQUENTIAL` for iteration. Measure the TLB miss rate
with `perf stat` and tune huge page settings for best throughput.

## Further Reading

### Foundational Papers

- Barr, T. et al. (2010). "Translation Caching: Skip, Don't Walk the Page Table."
  ISCA 2010. Explains why TLB hardware design matters and motivates huge page research.
- Navarro, J., Iyer, S., Druschel, P., Cox, A. (2002). "Practical, Transparent Operating
  System Support for Superpages." OSDI 2002. The original evaluation of huge page
  benefits for database and scientific workloads.
- Lameter, C. (2013). "NUMA (Non-Uniform Memory Access): An Overview." Linux Journal.
  NUMA topology and `mbind`/`set_mempolicy` for NUMA-aware allocation.

### Books

- Kerrisk, M. "The Linux Programming Interface" (2010) — Chapter 49 (Memory Mappings)
  is the definitive reference for `mmap`, `mprotect`, `mremap`, and `msync`.
- Love, R. "Linux Kernel Development" (3rd ed., 2010) — Chapter 15 (The Process Address
  Space) explains `mm_struct`, VMAs, and the page fault handler code path.
- Gorman, M. "Understanding the Linux Virtual Memory Manager" (2004) — Free online;
  covers the zone allocator, `kswapd`, and OOM killer in detail.

### Production Code to Read

- `runtime/mheap.go` in the Go source — `mheap.sysAlloc` and `sysHugePage` show how
  the Go runtime requests huge pages from the OS.
- `mm/mmap.c` in the Linux kernel — `do_mmap` is the kernel-side entry point for every
  `mmap` call; follow it into `mmap_region` to see VMA creation.
- RocksDB `table/block_based/block_based_table_reader.cc` — the huge page mmap path for
  the block cache.

### Conference Talks

- "Huge Pages: You Should Be Using Them" — Percona Live 2019. Practical advice on
  configuring huge pages for MySQL and PostgreSQL in production.
- "Understanding Go's Memory Allocator" — GopherCon 2018. Visualizes the arena, span,
  and page-level allocation structures.
- "How Linux Handles Memory" — FOSDEM 2023. Kernel developer walkthrough of THP,
  compaction, and the OOM killer decision algorithm.
