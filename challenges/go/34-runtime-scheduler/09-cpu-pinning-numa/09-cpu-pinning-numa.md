# 9. CPU Pinning and NUMA

<!--
difficulty: insane
concepts: [cpu-affinity, numa, sched-setaffinity, cache-locality, os-thread-pinning, lockosthread, processor-topology]
tools: [go, numactl, lscpu, taskset]
estimated_time: 60m
bloom_level: create
prerequisites: [gmp-model, gomaxprocs-processor-binding, observing-scheduler-godebug, scheduler-latency-trace]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-08 of this section
- Understanding of CPU architecture: cores, hardware threads, cache hierarchy (L1/L2/L3), NUMA nodes
- Linux environment (CPU affinity APIs are OS-specific)
- Familiarity with `runtime.LockOSThread`

## Learning Objectives

- **Create** programs that pin goroutines to specific CPU cores using OS-level affinity APIs
- **Implement** NUMA-aware memory allocation patterns that minimize cross-node memory access latency
- **Evaluate** the performance impact of CPU pinning and NUMA locality on compute-intensive workloads

## The Challenge

Go's scheduler treats all processors as equal, freely migrating goroutines between OS threads and P's. This is excellent for general-purpose workloads but can hurt performance-critical paths. When a goroutine migrates to a different CPU core, its data is no longer in the local L1/L2 cache, causing cache misses. On NUMA (Non-Uniform Memory Access) systems with multiple CPU sockets, accessing memory attached to a remote socket can be 2-3x slower than local memory access.

CPU pinning (thread affinity) locks an OS thread to a specific core, ensuring the goroutine running on it benefits from cache locality. On NUMA systems, allocating memory on the local node and pinning the processing thread to that node eliminates remote memory access penalties.

Your task is to measure the performance impact of CPU migration, implement CPU pinning for Go goroutines using `runtime.LockOSThread` combined with `sched_setaffinity`, and demonstrate NUMA-aware patterns that improve throughput for memory-intensive workloads.

## Requirements

1. Detect system topology: enumerate CPU cores, hardware threads (hyperthreading), and NUMA nodes using `/sys/devices/system/node/` and `/proc/cpuinfo` or the `unix.SchedGetaffinity` syscall
2. Implement CPU pinning: use `runtime.LockOSThread()` to bind a goroutine to its OS thread, then use `unix.SchedSetaffinity` to pin that thread to a specific CPU core
3. Measure cache migration cost: run a goroutine that accesses a large array repeatedly, alternating between pinned (same core) and unpinned (migrating) execution; compare throughput
4. Implement a pinned worker pool: N workers, each locked to a different CPU core, processing items from per-core channels to maximize cache locality
5. Demonstrate NUMA impact: allocate a large buffer, pin a goroutine to a NUMA node that owns the memory vs a remote NUMA node, and measure memory bandwidth difference
6. Build a NUMA-aware allocator pattern: allocate memory using `unix.Mmap` with NUMA policy hints (`MPOL_BIND` via `set_mempolicy`) and verify allocation locality
7. Compare latency: measure memory access latency (pointer-chasing benchmark) for local vs remote NUMA memory
8. Implement a core-affinity scheduler wrapper: a higher-level API that assigns goroutines to specific cores with automatic `LockOSThread` and affinity setup
9. Measure and report: throughput, L1/L2/L3 cache miss rates (via `perf stat` or `/proc/self/perf_events`), and cross-NUMA traffic
10. Write tests that verify CPU pinning actually takes effect (read affinity mask after setting it) and measure the performance delta

## Hints

- `runtime.LockOSThread()` prevents the goroutine from being moved to a different M, but the OS thread can still migrate between cores unless you set CPU affinity
- `unix.SchedSetaffinity(0, &mask)` sets the affinity for the current thread (pid=0 means current); build the CPU set using bitwise operations on `unix.CPUSet`
- On non-NUMA systems (most laptops), the NUMA effects will not be visible; focus on cache locality from CPU pinning instead
- `/sys/devices/system/node/node0/cpulist` shows which CPUs belong to each NUMA node
- For the pointer-chasing benchmark, create a linked list with nodes scattered across memory and measure the time to traverse it -- this is dominated by memory latency, not compute
- `taskset -c 0 ./myprogram` pins the entire process to core 0; your implementation should be more fine-grained, pinning individual goroutines to different cores

## Success Criteria

1. System topology detection correctly identifies CPU count, cores per socket, and NUMA nodes
2. CPU pinning is verified by reading back the affinity mask after setting it
3. Pinned execution shows measurably higher throughput than unpinned for cache-sensitive workloads
4. The pinned worker pool distributes work across cores with each worker staying on its assigned core
5. On NUMA systems, local memory access is measurably faster than remote memory access
6. The core-affinity scheduler correctly pins goroutines to specified cores
7. Performance measurements are statistically significant (multiple runs, standard deviation reported)
8. All tests pass with the `-race` flag enabled

## Research Resources

- [runtime.LockOSThread](https://pkg.go.dev/runtime#LockOSThread) -- pin goroutine to OS thread
- [unix.SchedSetaffinity](https://pkg.go.dev/golang.org/x/sys/unix#SchedSetaffinity) -- set CPU affinity
- [NUMA Architecture](https://www.kernel.org/doc/html/latest/admin-guide/mm/numa_memory_policy.html) -- Linux NUMA memory policy
- [CPU Caches and Why You Care (ScyllaDB)](https://www.scylladb.com/2017/07/06/cpu-caches-and-why-you-care/) -- cache hierarchy explained
- [Linux sched_setaffinity(2)](https://man7.org/linux/man-pages/man2/sched_setaffinity.2.html) -- system call documentation
- [perf-stat](https://man7.org/linux/man-pages/man1/perf-stat.1.html) -- hardware performance counter measurement

## What's Next

Continue to [10 - Scheduler-Friendly Algorithms](../10-scheduler-friendly-algorithms/10-scheduler-friendly-algorithms.md) to design algorithms that cooperate with the Go scheduler for optimal performance.

## Summary

- Go's scheduler migrates goroutines between cores freely, which can cause cache misses on performance-critical paths
- `runtime.LockOSThread()` + `unix.SchedSetaffinity` pins a goroutine to a specific CPU core
- CPU pinning improves throughput for cache-sensitive workloads by preserving L1/L2 cache contents
- NUMA systems have non-uniform memory latency: accessing memory on a remote socket is 2-3x slower
- Per-core worker pools with core affinity maximize cache locality for parallel workloads
- System topology detection enables adaptive pinning based on the runtime hardware configuration
