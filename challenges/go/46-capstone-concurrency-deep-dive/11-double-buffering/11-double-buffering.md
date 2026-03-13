<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 1.5h
-->

# Double Buffering for Concurrent Read/Write

## The Challenge

Implement a lock-free double-buffering system that allows a single writer to update a data structure while multiple concurrent readers access the previous version without any locking, blocking, or copying overhead. Double buffering maintains two copies of the data: readers access the "front" buffer while the writer modifies the "back" buffer; when the writer is done, it atomically swaps the buffers. This pattern is used in graphics rendering, configuration hot-reloading, routing table updates in network switches, and read-heavy concurrent data access. Your implementation must handle the subtlety that readers who started before the swap must finish reading the old buffer before the writer can reuse it, requiring a reference-counting or epoch-based mechanism to track active readers.

## Requirements

1. Implement `DoubleBuffer[T]` with two internal buffers and an atomic pointer indicating which is the current "read" buffer; readers load the pointer atomically to determine which buffer to read.
2. Implement `Read(func(snapshot *T))` that provides the reader with a reference to the current read buffer for the duration of the callback, ensuring the buffer is not modified or swapped away while the reader is using it.
3. Implement `Write(func(draft *T))` that provides the writer with exclusive access to the back buffer for modification; after the callback completes, the writer atomically swaps the front and back buffer pointers, making the modified buffer the new read buffer.
4. After swapping, the writer must wait until all readers that started before the swap have finished reading the old buffer before it can begin modifying it again (which is now the back buffer). Implement this using per-reader reference counting with atomic operations.
5. Implement `ReadLock() *T` and `ReadUnlock()` as an alternative API to the callback-based `Read`, for use cases where the reader needs to hold the reference across multiple function calls; the reader must call `ReadUnlock` when done to decrement the reference count.
6. Support multiple concurrent readers (hundreds) with zero contention between them -- readers should not contend on a single atomic variable; use per-CPU or sharded reference counters to avoid cache line bouncing.
7. Implement a `TripleBuffer[T]` variant that uses three buffers, allowing the writer to always have a buffer available without waiting for readers to drain, at the cost of readers potentially skipping an update.
8. Demonstrate the system with a practical example: a concurrent routing table where the writer periodically rebuilds the entire table and swaps it in, while hundreds of reader goroutines perform lookups without ever blocking.

## Hints

- Use `atomic.Pointer[T]` for the buffer pointer swap; the swap is a single `Store` operation (or `Swap` if you need the old pointer).
- For sharded reference counting, use an array of `atomic.Int64` padded to cache line size (64 bytes), with each reader selecting its shard using its goroutine ID or a thread-local index.
- The writer waits for all shards' reference counts for the old buffer to reach zero: `for !allShardsZero(oldBuffer) { runtime.Gosched() }`.
- Triple buffering: the writer always writes to the buffer that is neither the current read buffer nor the one being drained; this is tracked using a 2-bit atomic index.
- For the routing table example, use a `map[string]string` as the buffer contents, with `Read` providing a pointer to the map for lookups and `Write` rebuilding the entire map.
- Do NOT use `sync.RWMutex` -- the entire point is to achieve lock-free reads.
- `runtime_procPin()` is not available in user code; use `atomic.AddInt64` on a sharded counter indexed by `runtime.GOMAXPROCS(0)` or a similar scheme.

## Success Criteria

1. 256 concurrent readers and 1 writer achieve zero-contention reads: reader throughput scales linearly with the number of readers (no degradation up to 256).
2. A reader never sees a partially-modified buffer: the routing table is always in a consistent state.
3. The writer waits correctly for old readers to drain: a reader that holds `ReadLock` for 100 ms does not cause the writer to overwrite its buffer.
4. The triple buffer variant allows the writer to proceed immediately without waiting for readers, verified by timing.
5. All code passes `go test -race` with 256 readers and 1 writer.
6. Reader latency is under 100 nanoseconds for a simple pointer dereference read (no locking overhead).
7. The routing table demo handles 10 million lookups per second across 8 reader goroutines with the writer swapping every 100 ms.

## Research Resources

- "Read-Copy-Update" (RCU) in the Linux Kernel -- https://www.kernel.org/doc/html/latest/RCU/whatisRCU.html -- the canonical lock-free read pattern
- "Left-Right: A Concurrency Control Technique with Wait-Free Population Oblivious Reads" (Ramalhete & Correia, 2015)
- "seqlock" pattern in the Linux kernel -- version-counter-based read/write synchronization
- Triple buffering in graphics -- https://en.wikipedia.org/wiki/Multiple_buffering#Triple_buffering
- Go `sync/atomic` package documentation
- Go `runtime` package for `GOMAXPROCS` and goroutine scheduling
