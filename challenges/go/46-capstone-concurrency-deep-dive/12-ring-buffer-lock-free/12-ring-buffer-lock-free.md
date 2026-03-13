<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 2h
-->

# Lock-Free Ring Buffer

## The Challenge

Implement a lock-free single-producer single-consumer (SPSC) ring buffer and then extend it to a multi-producer single-consumer (MPSC) ring buffer, optimized for ultra-low-latency inter-goroutine communication. Ring buffers (circular buffers) are the foundation of high-performance message passing systems like the LMAX Disruptor, network packet queues, and audio processing pipelines. Your SPSC buffer must achieve latency under 50 nanoseconds per operation by exploiting the fact that only one thread reads and one thread writes, requiring only atomic loads and stores (no CAS). Your MPSC buffer must support multiple producers using CAS on the write cursor while maintaining a single lockless consumer. Both variants must handle the full/empty boundary without false sharing and support batched operations for throughput.

## Requirements

1. Implement an SPSC ring buffer with a power-of-2 capacity, using separate `head` (consumer reads here) and `tail` (producer writes here) atomic indices; the producer only writes `tail` and reads `head`, the consumer only writes `head` and reads `tail`, so no CAS is needed -- only atomic load/store.
2. Pad the `head` and `tail` indices to separate cache lines (64 bytes apart) to eliminate false sharing between the producer and consumer.
3. Implement `Offer(value T) bool` (non-blocking enqueue, returns false if full), `Poll() (T, bool)` (non-blocking dequeue, returns false if empty), and `OfferBatch(values []T) int` and `PollBatch(buf []T) int` (batch operations that transfer up to N items in a single call).
4. Extend the design to an MPSC ring buffer where multiple producers use `CompareAndSwap` on the `tail` index to claim slots, then write their values and mark the slot as committed using a per-slot sequence number or flag; the single consumer waits for the slot to be committed before reading.
5. Handle the MPSC commit gap: producer A claims slot 5, producer B claims slot 6, B commits before A; the consumer must not read slot 6 until slot 5 is also committed. Implement this by scanning from the consumer's head and blocking on uncommitted slots.
6. Implement backpressure strategies: `Block` (spin-wait until space is available, using `runtime.Gosched()` after a threshold), `Drop` (discard the oldest item to make room), and `Reject` (return false immediately).
7. Support generic value types using Go generics.
8. Implement a `Disruptor`-style ring buffer variant where the consumer publishes its progress back to the producers, enabling producers to batch-check available capacity and avoid per-item CAS overhead.

## Hints

- SPSC only needs sequential consistency between the two threads; `atomic.Store` on the writer side and `atomic.Load` on the reader side is sufficient (release/acquire semantics).
- Cache line padding: `type paddedUint64 struct { val atomic.Uint64; _ [56]byte }`.
- For the buffer array, pre-allocate a slice of `[capacity]slot[T]` where each slot has `value T` and `committed atomic.Bool` (for MPSC).
- The Disruptor pattern: instead of each producer doing CAS on the tail, producers first claim a sequence number range using `atomic.Add`, then write values into the claimed range, then set committed flags. The consumer advances only when all slots up to a sequence number are committed.
- Batch operations dramatically improve throughput: `PollBatch` should read as many consecutive committed items as possible in a single call, reducing per-item overhead.
- Benchmark with `testing.B` using `b.RunParallel` for the MPSC variant.
- Compare your implementation's throughput with Go channels using an equivalent benchmark.

## Success Criteria

1. SPSC ring buffer achieves less than 50 ns per operation (enqueue + dequeue pair) with a single producer and single consumer.
2. SPSC throughput exceeds 100 million items/sec for 8-byte values.
3. MPSC ring buffer with 8 producers achieves at least 30 million items/sec total throughput.
4. The MPSC commit gap is handled correctly: items are always dequeued in order even when producers commit out of order.
5. Batch operations achieve at least 3x throughput compared to single-item operations.
6. All variants pass `go test -race` with appropriate producer/consumer counts.
7. The ring buffer outperforms an equivalent buffered Go channel by at least 3x in the SPSC case.
8. No data loss: producing N items and consuming N items always yields exactly the original N items in order (SPSC) or with correct values (MPSC).

## Research Resources

- LMAX Disruptor paper -- "LMAX Disruptor: High Performance Alternative to Bounded Queues for Exchanging Data Between Concurrent Threads" (Thompson et al., 2011)
- Disruptor source code -- https://github.com/LMAX-Exchange/disruptor
- Dmitry Vyukov, "Bounded MPMC Queue" -- https://www.1024cores.net/home/lock-free-algorithms/queues/bounded-mpmc-queue
- "FastForward: fast and constructive full system simulation" -- SPSC queue implementation
- Linux kernel `kfifo` ring buffer implementation
- Go `testing` package benchmarking documentation
