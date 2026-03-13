<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 2.5h
-->

# Hazard Pointer Memory Reclamation

## The Challenge

Implement a hazard pointer-based safe memory reclamation scheme for lock-free data structures in Go. In lock-free programming, a fundamental problem is determining when it is safe to free (or reuse) a node that has been unlinked from a shared data structure, because other threads may still hold references to it. Hazard pointers solve this by requiring each thread to publish the addresses it is currently accessing; a node can only be reclaimed when no thread's hazard pointer references it. While Go has garbage collection, this exercise teaches the underlying principles by building a system that controls when nodes are recycled to a free list (avoiding GC pressure) and prevents use-after-recycle bugs in lock-free structures. You will implement the hazard pointer protocol, integrate it with a lock-free stack, and demonstrate that it prevents dangling pointer access.

## Requirements

1. Implement a `HazardPointerDomain` that manages a global array of hazard pointer records, one per goroutine (or per logical thread), where each record contains a configurable number of hazard pointers (default 2) stored as `atomic.Pointer[T]`.
2. Implement `Protect(slot int, ptr *T)` that publishes a pointer in the calling goroutine's hazard pointer slot, making it visible to all goroutines, and `Release(slot int)` that clears the slot.
3. Implement `Retire(ptr *T)` that adds a removed node to the calling goroutine's local retired list; when the retired list reaches a threshold (default `2 * numGoroutines * pointersPerGoroutine`), trigger a `Scan()` that compares retired nodes against all published hazard pointers and reclaims only those not protected by any hazard pointer.
4. The `Scan()` operation must collect all currently published hazard pointers into a set, then iterate the retired list, moving unprotected nodes to a free list for reuse and keeping protected nodes in the retired list for the next scan.
5. Implement a free list (lock-free stack) that recycled nodes are pushed onto, and from which new allocations pop before falling back to `new(T)`.
6. Integrate the hazard pointer system with a lock-free Treiber stack: `Push` and `Pop` operations use hazard pointers to protect the top node during CAS operations, and popped nodes are retired rather than abandoned to GC.
7. Demonstrate correctness under contention: 32 goroutines performing interleaved push/pop operations on the Treiber stack with hazard pointer protection must never access a recycled node that has been re-pushed with different data.
8. Implement per-goroutine registration using `runtime.LockOSThread` or goroutine-local storage via a `sync.Map` keyed by goroutine ID (obtained from the stack trace or a custom ID scheme).

## Hints

- Goroutine IDs are not directly exposed in Go; use a `sync.Map[goroutineID -> threadRecord]` where the goroutine ID is obtained by parsing `runtime.Stack()` output, or assign sequential IDs using an atomic counter at registration time.
- The hazard pointer array should be allocated as a contiguous slice for cache-friendly scanning during `Scan()`.
- Use `atomic.Pointer[T]` (Go 1.19+) for hazard pointer slots to ensure visibility across goroutines.
- The threshold for triggering `Scan()` is a tradeoff: too low causes frequent scans (overhead), too high causes excessive memory retention.
- The free list is itself a lock-free stack using `CompareAndSwap` on the head pointer.
- In the Treiber stack integration: before reading `top.next`, set `hazardPointer[0] = top` and re-read `top` to ensure it hasn't changed (the classic protect-validate loop).
- To verify no use-after-recycle, write a sentinel value into recycled nodes before pushing to the free list, and check that pop operations never see the sentinel.

## Success Criteria

1. The Treiber stack with hazard pointers passes `go test -race` with 32 concurrent goroutines performing 1 million push/pop cycles.
2. No goroutine ever reads a recycled node's stale data (verified by sentinel checking).
3. The free list is utilized: after steady state, at least 80% of allocations come from the free list rather than `new(T)`.
4. Memory usage stabilizes and does not grow linearly with the number of operations (proving reclamation works).
5. The `Scan()` operation correctly identifies and reclaims all unprotected retired nodes.
6. Hazard pointer operations add less than 50% overhead compared to an unprotected lock-free stack (benchmarked).
7. The system handles goroutine creation and destruction gracefully: a goroutine that terminates releases its hazard pointer record for reuse.

## Research Resources

- Maged M. Michael, "Hazard Pointers: Safe Memory Reclamation for Lock-Free Objects" (2004)
- "The Art of Multiprocessor Programming" (Herlihy & Shavit, 2012) -- Chapter 10: Memory Reclamation
- "C++ Concurrency in Action" (Williams, 2019) -- hazard pointer implementation
- Folly hazard pointer implementation (Facebook) -- https://github.com/facebook/folly/blob/main/folly/synchronization/HazardPointer.h
- Go `sync/atomic` package documentation
- Treiber stack: R.K. Treiber, "Systems Programming: Coping with Parallelism" (1986)
