# 10. Scheduler-Friendly Algorithms

<!--
difficulty: insane
concepts: [scheduler-cooperation, goroutine-yielding, batched-work, lock-free, cache-friendly, work-partitioning, preemption-points]
tools: [go]
estimated_time: 60m
bloom_level: create
prerequisites: [gmp-model, cooperative-vs-preemptive, runtime-gosched, work-stealing, observing-scheduler-godebug, scheduler-latency-trace]
-->

## Prerequisites

- Go 1.22+ installed
- Completed all prior exercises in this section (01-09)
- Deep understanding of the GMP model, preemption, work stealing, and scheduling latency
- Experience with profiling and benchmarking Go programs

## Learning Objectives

- **Create** algorithms that cooperate with the Go scheduler to minimize scheduling latency and maximize throughput
- **Implement** batched processing patterns that balance compute granularity against scheduling overhead
- **Evaluate** the impact of algorithm design choices on scheduler behavior, cache efficiency, and tail latency

## The Challenge

Go's scheduler is cooperative with asynchronous preemption. While preemption prevents complete starvation, tight loops that rarely hit preemption points (function calls, channel operations, memory allocation) can delay other goroutines for milliseconds. Algorithms that produce long-running compute phases without yielding degrade latency for everything else sharing the scheduler.

Scheduler-friendly algorithms are designed to cooperate with the runtime: they partition work into appropriately sized chunks, yield at natural boundaries, use data structures that benefit from cache locality, and avoid unnecessary synchronization that forces goroutines through the scheduler.

Your task is to take several common algorithmic patterns (sorting, matrix operations, graph traversal, producer-consumer pipelines) and redesign them to be scheduler-friendly. You will measure the impact on both throughput and tail latency of concurrent goroutines sharing the same processors.

## Requirements

1. Build a scheduler-hostile baseline: implement merge sort on a large slice (10M elements) in a single goroutine with no yielding, and measure the scheduling latency impact on a concurrent latency-measuring goroutine running on the same P (`GOMAXPROCS=1`)
2. Build a scheduler-friendly version: insert explicit yield points (`runtime.Gosched()`) at natural boundaries (every N merge operations), and show the reduction in tail latency for the concurrent goroutine
3. Implement batched channel processing: instead of sending one item per channel operation (high scheduling overhead), batch N items into a slice and send the batch; compare throughput and scheduling overhead between item-at-a-time and batched processing
4. Implement a work-partitioning parallel sort: split the data into chunks (one per P), sort each chunk locally, then merge; measure speedup and verify the work-stealing scheduler balances the load
5. Design a cache-friendly graph traversal: compare BFS using a `[]int` queue (cache-friendly, sequential access) vs a linked-list queue (cache-hostile, pointer chasing) on a large graph; measure throughput difference
6. Implement a lock-free ring buffer for producer-consumer communication: compare with a mutex-protected queue and a buffered channel on throughput and scheduling overhead (lock contention forces goroutines through the scheduler)
7. Build an adaptive yielding algorithm: dynamically adjust yield frequency based on observed scheduling latency -- yield more often when latency is high (other goroutines are waiting), less often when the system is idle
8. Measure GC friendliness: compare algorithms that allocate many small objects (GC pressure) vs pre-allocated slab patterns (minimal GC) on tail latency and total throughput
9. Implement a pipeline with backpressure: stages connected by bounded channels where slow consumers naturally apply backpressure to fast producers without explicit rate limiting
10. Write comprehensive benchmarks comparing scheduler-friendly vs scheduler-hostile versions of each algorithm

## Hints

- With `GOMAXPROCS=1`, a single CPU-bound goroutine monopolizes the processor; a concurrent goroutine's scheduling latency directly reflects how long until the compute goroutine yields or is preempted
- Go's preemption relies on safepoints at function calls; a tight loop with no function calls will not be preempted (though the asynchronous preemption signal can interrupt it, the delay can be significant)
- Batch sizes for channel processing should balance: too small = high scheduling overhead from channel ops; too large = high latency for individual items; typical sweet spot is 64-256 items
- For the lock-free ring buffer, use `sync/atomic` with `Load`/`Store` on head and tail indices; the key insight is that the producer only writes head and the consumer only writes tail, avoiding cache line contention
- Cache-friendly data structures keep related data in contiguous memory; Go slices are cache-friendly, linked lists are not
- Adaptive yielding can use a sliding window of recent scheduling latencies measured by a monitoring goroutine to determine yield frequency

## Success Criteria

1. The scheduler-hostile sort causes measurably higher scheduling latency (>5ms p99) for concurrent goroutines vs the scheduler-friendly version (<1ms p99) on `GOMAXPROCS=1`
2. Batched channel processing shows at least 3x higher throughput than item-at-a-time processing
3. Work-partitioned parallel sort achieves near-linear speedup up to `GOMAXPROCS` cores
4. Cache-friendly BFS is measurably faster than linked-list BFS on large graphs
5. The lock-free ring buffer shows lower tail latency than mutex-protected queue under high contention
6. Adaptive yielding reduces tail latency without significantly impacting throughput
7. Pre-allocated slab patterns show lower GC pause impact than small-allocation patterns
8. All benchmarks are reproducible with stable results across multiple runs
9. All tests pass with the `-race` flag enabled

## Research Resources

- [Go Preemption Design](https://github.com/golang/proposal/blob/master/design/24543-non-cooperative-preemption.md) -- non-cooperative preemption proposal
- [runtime.Gosched](https://pkg.go.dev/runtime#Gosched) -- explicit yield point
- [Mechanical Sympathy (Martin Thompson)](https://mechanical-sympathy.blogspot.com/) -- cache-friendly algorithm design
- [Lock-Free Data Structures (Herb Sutter)](https://herbsutter.com/2012/01/01/lock-free-programming/) -- lock-free design principles
- [sync/atomic](https://pkg.go.dev/sync/atomic) -- atomic operations for lock-free algorithms
- [Go GC Guide](https://tip.golang.org/doc/gc-guide) -- understanding GC pressure and tuning

## What's Next

You have completed the Runtime Scheduler section. These techniques form the foundation for writing high-performance Go applications that cooperate with the runtime rather than fighting against it.

## Summary

- Scheduler-friendly algorithms yield at natural boundaries to prevent goroutine starvation
- Batched processing amortizes channel and scheduling overhead across multiple items
- Work partitioning splits data into per-P chunks for cache-local parallel processing
- Cache-friendly data structures (contiguous slices) dramatically outperform pointer-based structures
- Lock-free algorithms reduce scheduler interaction by avoiding mutex contention
- Adaptive yielding balances throughput against tail latency based on runtime scheduler load
- GC-friendly allocation patterns (pre-allocation, object pools) reduce GC pause impact on latency
