<!--
type: reference
difficulty: advanced
section: [03-concurrency-and-parallelism]
concepts: [NUMA, memory-topology, cross-socket-latency, false-sharing, NUMA-aware-allocation, mbind, numactl, hwloc, cache-line-bouncing, memory-bandwidth, local-vs-remote-memory]
languages: [go, rust]
estimated_reading_time: 60-90 min
bloom_level: analyze
prerequisites: [cache-lines, work-stealing-scheduler, memory-models-and-happens-before]
papers: [Lameter 2013 "NUMA: An Overview", Treibig et al. 2010 "LIKWID: A Lightweight Performance Oriented Tool Suite"]
industry_use: [PostgreSQL-buffer-pool, RocksDB, Java-G1GC-NUMA-mode, numactl, hwloc, Linux-kernel-NUMA-scheduler]
language_contrast: medium
-->

# NUMA and Memory Topology

> On a multi-socket server, accessing memory on the wrong socket costs 2-3x more latency than accessing local memory — and most cloud VMs expose this topology whether you know it or not.

## Mental Model

A server with two CPU sockets does not have a single unified memory. Each socket has its own memory controller directly attached to the socket's DRAM. When a core on socket 0 accesses memory that is physically attached to socket 1, the memory request must travel across the **QPI** (Quick Path Interconnect) or **UPI** (Ultra Path Interconnect) — a dedicated high-speed link between sockets. This cross-socket trip adds ~100-150ns of latency compared to ~65ns for local DRAM access (approximately 2x slower), and cross-socket memory bandwidth is limited by the interconnect's bandwidth, which is typically lower than local memory bandwidth.

This architecture is called **NUMA** (Non-Uniform Memory Access). A NUMA system has multiple NUMA nodes; each node is a socket with its own set of CPU cores and attached DRAM. "Local" access means a core accesses memory on its own node. "Remote" access means a core accesses memory on another node. The NUMA factor (ratio of remote to local latency) is typically 1.5-3x for two-socket systems and can be higher for four- or eight-socket systems.

NUMA is not an obscure data center curiosity — it is the standard architecture for any server with more than one CPU socket, including most AWS `c5.metal`, `m5.metal`, and Google `n2-standard-32` instances. A Go service running on an 8-core VM may be on a 2-socket physical server where 4 cores and 2GB of the VM's 4GB are on one NUMA node and 4 cores and 2GB are on the other. NUMA effects are real and measurable in production profiling data.

**False sharing** is the interaction between NUMA and cache coherence at the hardware level. Two variables that are logically independent but physically in the same 64-byte cache line cause cache line bouncing: when thread A writes variable X and thread B writes variable Y (both in the same cache line), each write invalidates the other thread's copy of the cache line, causing a cache miss on the next access. On a NUMA system, this cache miss becomes a cross-socket DRAM access — the most expensive possible outcome.

## Core Concepts

### NUMA Topology and `numactl`

The `numactl` tool (Linux) provides a command-line interface to NUMA topology:

```
numactl --hardware     # show nodes, CPUs per node, memory per node
numactl --show         # show current NUMA policy

# Example output (2-socket, 40-core server):
# available: 2 nodes (0-1)
# node 0 cpus: 0 1 2 3 4 5 6 7 8 9 20 21 22 23 24 25 26 27 28 29
# node 0 size: 95328 MB
# node 1 cpus: 10 11 12 13 14 15 16 17 18 19 30 31 32 33 34 35 36 37 38 39
# node 1 size: 96226 MB
# node distances:
# node  0  1
#   0: 10 21  ← local = 10, remote = 21 (2.1x latency factor)
#   1: 21 10
```

The `node distances` matrix is normalized so local = 10. Remote = 21 means remote accesses cost 2.1x local accesses on this system.

### Memory Allocation Policy (`mbind` / `mmap` + `numa_alloc_onnode`)

Linux provides per-allocation NUMA policies:

- **`MPOL_DEFAULT`**: Allocate on the first-touch NUMA node (the node of the thread that first writes the page). This is the default and works well if threads access only locally-allocated data.
- **`MPOL_BIND`**: Allocate only on the specified node(s). Use when you know which NUMA node's data a thread pool accesses.
- **`MPOL_INTERLEAVE`**: Round-robin allocation across nodes. Use for shared data structures accessed by all threads.
- **`MPOL_PREFERRED`**: Prefer a specific node; fall back to other nodes if full.

The `mbind(2)` syscall applies a policy to an existing memory region. `numa_alloc_onnode(3)` (from `libnuma`) allocates a new region on a specific node. In Go, use the `unix` package's `Mmap` + `mbind` syscall binding. In Rust, use `nix::sys::mman::mmap` + `mbind`.

### Cache Line Padding and False Sharing Prevention

The standard mitigation for false sharing is to pad shared-mutable variables to cache line boundaries (64 bytes on x86-64, 128 bytes on some ARM designs). The principle: if each variable lives in its own cache line, writes to one variable cannot invalidate another thread's copy of the other variable.

The NUMA exacerbation: false sharing across NUMA nodes is not just a cache invalidation — it is a remote memory access. The cache line must travel across the QPI/UPI interconnect for every invalidation. Under heavy false sharing across sockets, cache line traffic can saturate the QPI bandwidth, degrading throughput to worse than sequential execution.

### hwloc: Hardware Locality

`hwloc` (Portable Hardware Locality library) provides a programmatic API for discovering hardware topology: CPU cores, NUMA nodes, caches, PCI buses. It is used by OpenMPI, BLAS libraries, and database engines to make topology-aware scheduling decisions.

```c
// hwloc topology discovery:
hwloc_topology_t topology;
hwloc_topology_init(&topology);
hwloc_topology_load(&topology);

// Get number of NUMA nodes:
int n_nodes = hwloc_get_nbobjs_by_type(topology, HWLOC_OBJ_NUMANODE);

// Bind current thread to NUMA node 0:
hwloc_obj_t node = hwloc_get_obj_by_type(topology, HWLOC_OBJ_NUMANODE, 0);
hwloc_set_cpubind(topology, node->cpuset, HWLOC_CPUBIND_THREAD);
hwloc_set_membind(topology, node->nodeset, HWLOC_MEMBIND_BIND, 0);
```

In Rust, the `hwloc` crate provides a safe wrapper. In Go, there is no standard binding; use `syscall.Mmap` + `mbind` directly or use a CGo wrapper around `libnuma`.

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

// --- False sharing demonstration ---
//
// Two counters in the same cache line: increment by different goroutines.
// Under high concurrency, the cache line bounces between cores on every write,
// causing 3-10x throughput degradation vs padded counters.
//
// Race detector: clean. All writes use atomic operations.

const cacheLineSize = 64 // 64 bytes on x86-64; 128 bytes on some ARM

// FalseSharingCounters: both counters share a cache line.
type FalseSharingCounters struct {
	a int64 // offset 0
	b int64 // offset 8 — same cache line as a
}

// PaddedCounters: each counter is on its own cache line.
type PaddedCounters struct {
	a   int64
	_a  [cacheLineSize - 8]byte // padding: fill the rest of the cache line
	b   int64
	_b  [cacheLineSize - 8]byte
}

func benchmarkFalseSharing(n int) time.Duration {
	var counters FalseSharingCounters
	var wg sync.WaitGroup
	start := time.Now()
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			atomic.AddInt64(&counters.a, 1)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			atomic.AddInt64(&counters.b, 1)
		}
	}()
	wg.Wait()
	return time.Since(start)
}

func benchmarkPadded(n int) time.Duration {
	var counters PaddedCounters
	var wg sync.WaitGroup
	start := time.Now()
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			atomic.AddInt64(&counters.a, 1)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			atomic.AddInt64(&counters.b, 1)
		}
	}()
	wg.Wait()
	return time.Since(start)
}

// --- NUMA-aware worker pool ---
//
// On a NUMA system, this pool assigns goroutines to worker groups that
// mirror the NUMA topology. Each worker group is responsible for data
// allocated on its NUMA node. Cross-NUMA work is minimized.
//
// This implementation uses GOMAXPROCS as a proxy for the number of NUMA nodes
// (in practice, use hwloc or /sys/devices/system/node/ to discover node count).
//
// Race detector: clean.

type numaWorkerPool struct {
	nNodes  int
	queues  []chan func()
	done    chan struct{}
	wg      sync.WaitGroup
}

func newNUMAWorkerPool(nNodes, workersPerNode int) *numaWorkerPool {
	p := &numaWorkerPool{
		nNodes: nNodes,
		queues: make([]chan func(), nNodes),
		done:   make(chan struct{}),
	}
	for node := 0; node < nNodes; node++ {
		p.queues[node] = make(chan func(), 256)
		for w := 0; w < workersPerNode; w++ {
			p.wg.Add(1)
			nodeID := node
			go p.worker(nodeID)
		}
	}
	return p
}

func (p *numaWorkerPool) worker(nodeID int) {
	defer p.wg.Done()
	// In production: set CPU affinity to NUMA node `nodeID`'s CPUs.
	// On Linux: unix.SchedSetaffinity(0, mask) where mask = CPUs in nodeID's node.
	// For portability, we omit the affinity call here.
	for {
		select {
		case task := <-p.queues[nodeID]:
			task()
		case <-p.done:
			return
		}
	}
}

// Submit routes work to the appropriate NUMA node's queue.
// The caller computes which NUMA node owns the data being operated on.
func (p *numaWorkerPool) Submit(numaNode int, task func()) {
	p.queues[numaNode%p.nNodes] <- task
}

func (p *numaWorkerPool) Shutdown() {
	close(p.done)
	p.wg.Wait()
}

// --- NUMA topology detection via /sys ---
//
// On Linux, NUMA topology is exposed via /sys/devices/system/node/.
// Each directory node0, node1, ... corresponds to a NUMA node.
// This function returns the number of NUMA nodes without CGo.

func detectNUMANodes() int {
	// Simplified: use GOMAXPROCS as a rough proxy.
	// Production: read /sys/devices/system/node/online via os.ReadFile
	// and parse the CPU list to determine node count.
	nProcs := runtime.GOMAXPROCS(0)
	if nProcs >= 8 {
		return 2 // heuristic: likely 2 NUMA nodes for 8+ core systems
	}
	return 1
}

// --- First-touch NUMA policy demonstration ---
//
// Linux's default NUMA policy allocates pages on the NUMA node of the
// first thread to touch (write) the page.
// If a large buffer is initialized by goroutine A running on node 0,
// but later read by goroutine B running on node 1, B incurs remote
// memory access penalties.
//
// Fix: initialize data on the same NUMA node as the reader.

func firstTouchDemo(size int) []int64 {
	buf := make([]int64, size)
	var wg sync.WaitGroup

	// Bad: initialize on one goroutine (which may be on node 0),
	// then read from another goroutine (which may be on node 1).
	// The first writer determines the physical NUMA node of the allocation.
	wg.Add(1)
	go func() {
		defer wg.Done()
		// This goroutine's NUMA node "owns" the pages due to first-touch.
		for i := range buf {
			buf[i] = int64(i) // first touch: pages allocated on this goroutine's NUMA node
		}
	}()
	wg.Wait()

	return buf
}

// --- Cache line size verification ---

func verifyCacheLineAssumption() {
	var p PaddedCounters
	sizeA := unsafe.Offsetof(p.b) // should be 64 on x86-64
	if sizeA != cacheLineSize {
		fmt.Printf("WARNING: Expected cache line at offset %d, got %d. Adjust padding.\n",
			cacheLineSize, sizeA)
	} else {
		fmt.Printf("Cache line padding correct: b is at offset %d\n", sizeA)
	}
}

func main() {
	const N = 10_000_000

	// False sharing comparison
	t1 := benchmarkFalseSharing(N)
	t2 := benchmarkPadded(N)
	fmt.Printf("False sharing: %v\nPadded:        %v\n", t1, t2)
	fmt.Printf("Speedup from padding: %.2fx\n", float64(t1)/float64(t2))

	// Verify padding
	verifyCacheLineAssumption()

	// NUMA worker pool
	nNodes := detectNUMANodes()
	pool := newNUMAWorkerPool(nNodes, 2)
	var taskWg sync.WaitGroup
	for i := 0; i < 100; i++ {
		taskWg.Add(1)
		node := i % nNodes
		pool.Submit(node, func() {
			defer taskWg.Done()
			time.Sleep(time.Microsecond)
		})
	}
	taskWg.Wait()
	pool.Shutdown()
	fmt.Printf("NUMA pool: completed 100 tasks across %d nodes\n", nNodes)

	// First-touch demo
	buf := firstTouchDemo(1_000_000)
	fmt.Printf("First-touch buffer: %d elements, first=%d, last=%d\n",
		len(buf), buf[0], buf[len(buf)-1])
}
```

### Go-specific considerations

**GOMAXPROCS and NUMA nodes**: By default, Go sets GOMAXPROCS to the total number of CPU cores across all NUMA nodes. On a 2-socket 40-core server, GOMAXPROCS=40, and goroutines are scheduled across all 40 cores freely. This means a goroutine may be scheduled on a different NUMA node on each scheduling quantum. For NUMA-sensitive workloads (database buffer pools, high-performance caching), the recommendation is to run separate Go processes per NUMA node (`numactl --cpunodebind=0 --membind=0 ./service`) rather than trying to achieve NUMA awareness within a single Go process.

**Go's allocator and NUMA**: Go's allocator does not support NUMA-aware allocation directly. Memory for `make([]T, n)` and `new(T)` is allocated from Go's heap, which is managed by the GC and is not NUMA-aware. For NUMA-critical allocations, use `syscall.Mmap` + `mbind` (via the `golang.org/x/sys/unix` package) to allocate memory directly from the OS with a specific NUMA policy.

**`runtime.NumCPU()` vs NUMA awareness**: `runtime.NumCPU()` returns the total logical CPU count, not the NUMA topology. To detect NUMA nodes in Go without CGo, read `/sys/devices/system/node/` directory listing (each subdirectory `nodeN` is a NUMA node), or use `numactl --hardware` in a subprocess (not suitable for production).

## Implementation: Rust

```rust
use std::alloc::{alloc, dealloc, Layout};
use std::sync::atomic::{AtomicI64, Ordering};
use std::sync::Arc;
use std::thread;

// --- False sharing: the Rust version ---
//
// repr(C) ensures predictable field layout.
// The padding pattern is the same as Go, but Rust allows alignment
// enforcement at the type level via repr(align).

#[repr(C)]
struct FalseSharingCounters {
    a: AtomicI64, // offset 0
    b: AtomicI64, // offset 8 — same cache line
}

#[repr(C, align(64))] // each CachePadded<T> is aligned to and fills a cache line
struct CachePadded<T> {
    value: T,
    // Rust calculates padding automatically from the type size and alignment.
    // For T = AtomicI64 (8 bytes), this struct is 64 bytes total.
    _padding: [u8; 64 - 8], // 56 bytes of padding
}

// Safety: CachePadded is transparent to Send/Sync — it inherits from T.
unsafe impl<T: Send> Send for CachePadded<T> {}
unsafe impl<T: Send + Sync> Sync for CachePadded<T> {}

struct PaddedCounters {
    a: CachePadded<AtomicI64>,
    b: CachePadded<AtomicI64>,
}

fn bench_false_sharing(n: u64) -> std::time::Duration {
    let counters = Arc::new(FalseSharingCounters {
        a: AtomicI64::new(0),
        b: AtomicI64::new(0),
    });
    let start = std::time::Instant::now();
    let c1 = Arc::clone(&counters);
    let c2 = Arc::clone(&counters);
    let h1 = thread::spawn(move || {
        for _ in 0..n { c1.a.fetch_add(1, Ordering::Relaxed); }
    });
    let h2 = thread::spawn(move || {
        for _ in 0..n { c2.b.fetch_add(1, Ordering::Relaxed); }
    });
    h1.join().unwrap();
    h2.join().unwrap();
    start.elapsed()
}

fn bench_padded(n: u64) -> std::time::Duration {
    let counters = Arc::new(PaddedCounters {
        a: CachePadded { value: AtomicI64::new(0), _padding: [0u8; 56] },
        b: CachePadded { value: AtomicI64::new(0), _padding: [0u8; 56] },
    });
    let start = std::time::Instant::now();
    let c1 = Arc::clone(&counters);
    let c2 = Arc::clone(&counters);
    let h1 = thread::spawn(move || {
        for _ in 0..n { c1.a.value.fetch_add(1, Ordering::Relaxed); }
    });
    let h2 = thread::spawn(move || {
        for _ in 0..n { c2.b.value.fetch_add(1, Ordering::Relaxed); }
    });
    h1.join().unwrap();
    h2.join().unwrap();
    start.elapsed()
}

// --- NUMA-aware allocation via mbind ---
//
// On Linux, mbind(2) sets the NUMA memory policy for a memory range.
// This requires the nix crate or direct libc::mbind call.
//
// Production use requires:
//   nix = { version = "0.27", features = ["mman"] }
//   or direct libc::mbind
//
// The pattern:
//
// use std::alloc::System;
//
// struct NumaAllocator {
//     node: u8,
// }
//
// unsafe impl GlobalAlloc for NumaAllocator {
//     unsafe fn alloc(&self, layout: Layout) -> *mut u8 {
//         let ptr = libc::mmap(
//             ptr::null_mut(), layout.size(),
//             libc::PROT_READ | libc::PROT_WRITE,
//             libc::MAP_PRIVATE | libc::MAP_ANONYMOUS, -1, 0
//         ) as *mut u8;
//         if ptr.is_null() { return ptr; }
//         // Bind the memory to the specified NUMA node.
//         let nodemask: u64 = 1u64 << self.node;
//         libc::mbind(
//             ptr as *mut libc::c_void, layout.size(),
//             MPOL_BIND,
//             &nodemask as *const u64 as *const libc::c_ulong,
//             64, // maxnode: number of bits in nodemask
//             0   // flags
//         );
//         ptr
//     }
//     unsafe fn dealloc(&self, ptr: *mut u8, layout: Layout) {
//         libc::munmap(ptr as *mut libc::c_void, layout.size());
//     }
// }

// --- hwloc topology wrapper pattern ---
//
// The hwloc crate provides topology-aware allocation:
//
// use hwloc::{Topology, ObjectType, CPUBIND_THREAD, MEMBIND_BIND};
//
// fn bind_to_numa_node(topology: &Topology, node_index: usize) {
//     let node = topology.objects_with_type(&ObjectType::NUMANode)
//                        .unwrap()[node_index];
//     // Bind CPU affinity of this thread to the node's CPUs.
//     topology.set_cpubind(node.cpuset().unwrap(), CPUBIND_THREAD).ok();
//     // Bind memory allocation of this thread to the node's memory.
//     topology.set_membind_nodeset(node.nodeset().unwrap(), MEMBIND_BIND, 0).ok();
// }

// --- NUMA-aware thread pool ---
//
// Assigns each thread to a NUMA node; threads process work from that node's queue.

struct NumaPool {
    senders: Vec<std::sync::mpsc::SyncSender<Box<dyn FnOnce() + Send>>>,
    handles: Vec<thread::JoinHandle<()>>,
}

impl NumaPool {
    fn new(n_nodes: usize, threads_per_node: usize) -> Self {
        let mut senders = Vec::new();
        let mut handles = Vec::new();

        for node in 0..n_nodes {
            let (tx, rx) = std::sync::mpsc::sync_channel::<Box<dyn FnOnce() + Send>>(256);
            senders.push(tx);

            for _ in 0..threads_per_node {
                let rx = std::sync::Mutex::new(rx.clone());
                // Cloning SyncReceiver is not supported; we share via Arc<Mutex<Receiver>>.
                // For illustration, use one receiver per node.
                let _ = node; // in production: set CPU affinity here
                handles.push(thread::spawn(move || {
                    while let Ok(task) = rx.lock().unwrap().recv() {
                        task();
                    }
                }));
                break; // one thread per node for simplicity in this example
            }
        }
        NumaPool { senders, handles }
    }

    fn submit(&self, node: usize, f: impl FnOnce() + Send + 'static) {
        let idx = node % self.senders.len();
        let _ = self.senders[idx].send(Box::new(f));
    }

    fn shutdown(self) {
        drop(self.senders); // drop senders; receivers will see disconnection
        for h in self.handles { let _ = h.join(); }
    }
}

// --- Cache line size at compile time ---

const CACHE_LINE_SIZE: usize = 64; // x86-64; use 128 for some ARM designs

const _: () = assert!(
    std::mem::size_of::<CachePadded<AtomicI64>>() == CACHE_LINE_SIZE,
    "CachePadded size must equal cache line size"
);

fn main() {
    // False sharing benchmark
    let n = 10_000_000u64;
    let t_false = bench_false_sharing(n);
    let t_padded = bench_padded(n);
    println!("False sharing: {t_false:?}");
    println!("Padded:        {t_padded:?}");
    let speedup = t_false.as_nanos() as f64 / t_padded.as_nanos() as f64;
    println!("Speedup from padding: {speedup:.2}x");

    // NUMA pool
    let pool = NumaPool::new(2, 2);
    let counter = Arc::new(AtomicI64::new(0));
    for i in 0..20 {
        let c = Arc::clone(&counter);
        pool.submit(i % 2, move || { c.fetch_add(1, Ordering::Relaxed); });
    }
    // Wait briefly for tasks to complete (no WaitGroup in this example).
    thread::sleep(std::time::Duration::from_millis(10));
    println!("NUMA pool tasks completed: {}", counter.load(Ordering::Relaxed));
    pool.shutdown();
}
```

### Rust-specific considerations

**`repr(align(N))` for cache line alignment**: Rust's `#[repr(align(64))]` attribute on a struct ensures that the struct starts on a 64-byte aligned boundary and has a size that is a multiple of 64 bytes. This is the idiomatic way to prevent false sharing in Rust. The `crossbeam` crate provides `CachePadded<T>` for exactly this purpose — prefer it over hand-written padding for production code.

**`Allocator` trait (nightly) and NUMA**: Rust's allocator API (stable as of `std::alloc::Allocator` on nightly) allows custom allocators per-collection (`Vec`, `Box`, etc.). A NUMA allocator can be implemented via the `Allocator` trait and used as `Vec<T, NumaAllocator>`. The `allocator-api2` crate backports this API to stable Rust. For Linux-specific NUMA allocation, combine `mmap(MAP_ANONYMOUS)` + `mbind(MPOL_BIND)` in the `alloc` implementation.

**Thread affinity in Rust**: Rust's `std::thread` does not provide CPU affinity. Use the `core_affinity` crate (wraps `sched_setaffinity` on Linux) or direct `libc::sched_setaffinity` syscall. The `hwloc` crate provides both topology discovery and affinity setting in one API.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Cache line padding | `[N]byte` padding fields; manual calculation | `repr(align(64))`; `crossbeam::utils::CachePadded<T>` |
| NUMA-aware allocation | `syscall.Mmap` + `unix.Mbind`; no standard API | `mmap` + `mbind` via `libc` or `nix`; `Allocator` trait for per-collection NUMA |
| CPU affinity | No standard library; `unix.SchedSetaffinity` via `golang.org/x/sys` | `core_affinity` crate; `libc::sched_setaffinity` |
| Topology discovery | Read `/sys/devices/system/node/`; no standard API | `hwloc` crate (safe wrapper around C hwloc library) |
| GOMAXPROCS and NUMA | No NUMA awareness; goroutines migrate freely | No concept (no Go-like scheduler); Tokio worker threads can be pinned |
| GC and NUMA | GC scans all goroutine stacks regardless of NUMA node | No GC; allocations are NUMA-pinned if the allocator is |

## Production War Stories

**PostgreSQL buffer pool and NUMA (2012-present)**: PostgreSQL's shared buffer pool is a large mmap'd region of memory (~25% of RAM by default). On a 2-socket NUMA system, the first PostgreSQL process to initialize the buffer pool causes the pages to be allocated on its NUMA node (first-touch policy). All subsequent processes accessing the pool from the other NUMA node incur ~2x latency on every buffer read. The fix: interleaved allocation (`numactl --interleave=all pg_ctl start`) distributes the buffer pool pages across all NUMA nodes, halving the average remote-access rate. This configuration is now the standard PostgreSQL deployment recommendation for multi-socket servers and is documented in the PostgreSQL manual.

**RocksDB block cache and NUMA locality (2019)**: RocksDB, the key-value store underlying TiKV, Cassandra, and MySQL (MyRocks), has a configurable block cache (a shard-based LRU cache). By default, all shards are allocated from the process's main allocator with no NUMA awareness. On a 4-socket server (typical for high-end database deployments), cross-socket cache accesses caused measurable throughput degradation. Meta's RocksDB team added NUMA-aware shard allocation: each shard group is bound to a specific NUMA node, and threads that access shard groups are pinned to the corresponding node's CPUs. This reduced p99 read latency by 15% on 4-socket servers in production.

**Go's GOMAXPROCS and NUMA: the container over-provisioning problem**: A production Go service at a large cloud provider was deployed in containers with `cpu_limit=4` on a 2-socket 32-core host. GOMAXPROCS defaulted to 32 (host core count, before `automaxprocs` was standard). The 4 virtual CPUs for the container were distributed across both NUMA nodes (2 on node 0, 2 on node 1) due to the cloud provider's vCPU allocation policy. With GOMAXPROCS=32, Go created 32 P's competing for 4 vCPUs. Cross-NUMA scheduling caused goroutines to migrate between NUMA nodes frequently, causing cross-socket memory accesses on every migration. The fix: `automaxprocs` set GOMAXPROCS=4, reducing P count to match vCPU count and eliminating unnecessary NUMA-crossing goroutine migrations.

**Java G1GC NUMA mode**: Java's G1 garbage collector added NUMA support in JDK 14. Without NUMA mode, the GC's allocation regions were not NUMA-aware — young generation objects allocated by a thread on NUMA node 0 might end up on node 1's memory. With G1GC NUMA mode (`-XX:+UseNUMA`), the GC pins allocation regions to specific NUMA nodes and uses thread-local allocation buffers (TLABs) that allocate from the same NUMA node as the allocating thread. Performance improvement for NUMA-sensitive workloads: 20-40% throughput increase in HBase benchmarks on 4-socket servers.

## Complexity Analysis

- **NUMA latency factor**: 1.5-2x for 2-socket systems (local DRAM ~65ns, remote ~100-150ns). 3-5x for 4-socket or 8-socket systems where data may traverse multiple hops.

- **False sharing throughput degradation**: Without padding, two threads writing to the same cache line achieve ~1/N the throughput of N independent cache lines, because each write invalidates the other thread's copy. On a 2-socket NUMA system, the invalidation requires a cross-socket coherence message: 1 write = 1 QPI transaction (~60ns) vs 1 write = 1 local cache operation (~4ns). Under 8 threads on 2 NUMA nodes with heavy false sharing: effective write throughput ≈ QPI bandwidth / (cache line size) ≈ 20 GB/s / 64B ≈ 312M writes/s vs ~3.2B writes/s with proper padding.

- **NUMA-aware allocation benefit**: For a workload where each thread exclusively accesses its own data: NUMA-aware allocation (bind data to the thread's NUMA node) achieves local DRAM throughput (~50-100 GB/s per node) vs a non-NUMA-aware allocation where 50% of accesses are remote (effective bandwidth: `1 / (0.5/50 + 0.5/20)` ≈ 28 GB/s). Approximately 2x improvement for such workloads.

- **Interleaved allocation amortization**: For truly shared data (a buffer pool accessed by all threads equally), interleaved allocation averages remote and local accesses: expected latency = (n_local * local_lat + n_remote * remote_lat) / n_total. For 2 nodes, half accesses are local (65ns) and half remote (130ns): average ~97ns vs always-remote 130ns or always-local 65ns. Interleaving halves the worst-case impact at the cost of being 50% slower than optimal for local threads.

## Common Pitfalls

**1. Assuming NUMA only matters on physical servers.** Cloud VMs expose NUMA topology. A `c5.18xlarge` AWS instance (72 vCPUs) has 2 NUMA nodes visible to the guest OS. Any service running on this instance type may experience NUMA effects if it accesses memory from multiple CPU sockets. Check with `numactl --hardware` in your cloud environment before dismissing NUMA.

**2. Over-padding to avoid false sharing.** Padding every shared variable to 64 bytes reduces cache efficiency: an array of counters padded to 64 bytes uses 8x the memory of an array of int64. Under single-threaded access, the cache holds 1 counter per 64-byte line instead of 8, increasing the cache miss rate for sequential scans. Pad only variables that are concurrently written by different threads. Variables that are only read concurrently (no writes) do not need padding.

**3. First-touch initialization on the wrong thread.** A `make([]T, n)` in Go allocates virtual memory, but physical pages are not mapped until first written (demand paging). If a long-lived buffer is initialized by the main goroutine (possibly on node 0) and then accessed exclusively by workers on node 1, all access is remote. Fix: initialize data on the same goroutine/thread that will access it (or use `madvise(MADV_HUGEPAGE)` + interleave for shared data).

**4. Ignoring NUMA in benchmarks.** A benchmark that runs on a single core (single goroutine) will not exhibit NUMA effects because all memory accesses are local. The same code in production with 32 goroutines across 2 NUMA nodes may run 1.5-2x slower. Always benchmark with the same thread count and NUMA topology as the production environment.

**5. Using `numactl --membind=0` without CPU affinity.** Binding memory to node 0 without binding CPU affinity to node 0 is worse than the default: threads on node 1 will always incur remote memory access (all memory is on node 0) instead of approximately 50% remote access. Bind both memory and CPU to the same node, or use interleaving for shared data.

## Exercises

**Exercise 1** (30 min): Write a Go benchmark that measures the impact of false sharing: allocate a struct with two `int64` fields, increment each from a separate goroutine in a loop, and time 10M iterations. Then add `[56]byte` padding between the fields and repeat. The expected speedup on a multi-core machine is 2-10x. If running in a VM, record whether the speedup is smaller (VMs may limit NUMA exposure).

**Exercise 2** (2-4h): Implement a NUMA-aware ring buffer in Rust that can be configured to allocate its buffer on a specific NUMA node using `libc::mmap` + `libc::mbind`. Write a benchmark that measures read throughput from a buffer allocated on node 0 when reading from node 0's CPUs vs node 1's CPUs (use `core_affinity` to pin threads). Report the NUMA latency factor for your test machine.

**Exercise 3** (4-8h): Implement a NUMA-aware cache in Go: divide the cache into N shards, where N equals the number of NUMA nodes detected from `/sys/devices/system/node/`. Allocate each shard's backing memory on its corresponding NUMA node (using `unix.Mmap` + `unix.Mbind`). Assign cache operations to the shard corresponding to the caller's NUMA node (detect via `unix.SchedGetaffinity`). Benchmark against a single-shard cache at 8 goroutines across 2 NUMA nodes. Expected improvement: 1.5-2x throughput for read-heavy workloads.

**Exercise 4** (8-15h): Read the PostgreSQL documentation on NUMA configuration and the `pg_numa_info()` function (added in PostgreSQL 17). Set up a PostgreSQL instance on a 2-NUMA-node system (or simulate with `numactl` pinning). Run `pgbench` (the standard PostgreSQL benchmark) with: default allocation, `--membind=0`, and `--interleave=all`. Record throughput and p99 latency for each. Write a report explaining the results in terms of buffer pool access patterns, QPI bandwidth, and the first-touch NUMA policy.

## Further Reading

### Foundational Papers

- Lameter, C. (2013). "NUMA: An Overview." *ACM Queue 11(7)* — The clearest practical introduction to NUMA for software engineers. Available free online.
- Drepper, U. (2007). "What Every Programmer Should Know About Memory." Free online — Section 5: NUMA programming. The comprehensive reference.
- Treibig, J. et al. (2010). "LIKWID: A Lightweight Performance-Oriented Tool Suite for x86 Multi-Core Environments." — Hardware performance counter analysis for NUMA and cache behavior.

### Books

- Patterson, D. & Hennessy, J. *Computer Organization and Design: x86 Edition* (2020) — NUMA and memory hierarchy.

### Production Code to Read

- PostgreSQL `src/backend/storage/buffer/bufmgr.c` — Buffer pool initialization and the `NUMA_INTERLEAVE` option.
- Linux kernel `mm/mempolicy.c` — The `mbind` syscall implementation.
- `crossbeam/src/utils/cache_padded.rs` — The idiomatic Rust `CachePadded<T>` implementation.
- `numactl` source code — `libnuma.c` shows how `numa_alloc_onnode` and `mbind` are used.

### Talks

- "NUMA Deep Dive" — Frank Denneman (VMworld, various years) — Practical NUMA impact on virtualized environments. Directly relevant to cloud deployments.
- "Memory Bottlenecks in Modern CPUs" — John McCalpin (HPC conferences) — Memory subsystem limits including NUMA bandwidth.
