<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 3h
-->

# Lock-Free MPMC Queue

## The Challenge

Implement a lock-free multi-producer multi-consumer (MPMC) bounded queue in Go using only atomic operations -- no mutexes, no channels, no `sync` package primitives. Your queue must support concurrent enqueue and dequeue operations from an arbitrary number of goroutines without any locks, achieving linearizable semantics where every operation appears to take effect atomically at some point between its invocation and return. The queue must be bounded (fixed capacity set at creation time) and must handle the full/empty boundary conditions without spinning indefinitely. This exercise forces you to reason at the hardware memory model level, understand compare-and-swap loops, deal with the ABA problem, and navigate Go's memory model constraints using `sync/atomic`.

## Requirements

1. Implement a bounded MPMC queue with a fixed capacity (power of 2) specified at creation time, using a ring buffer as the underlying storage with atomic head and tail indices.
2. The `Enqueue(value)` operation must be lock-free: it uses `CompareAndSwap` on the tail index to claim a slot, writes the value, and then marks the slot as filled using an atomic sequence number or generation counter.
3. The `Dequeue()` operation must be lock-free: it uses `CompareAndSwap` on the head index to claim a slot, waits for the slot to be marked as filled, reads the value, and marks the slot as empty using the sequence number.
4. Solve the ABA problem using per-slot sequence numbers (also called generation counters or turn indicators): each slot has an atomic counter that alternates between "writable" and "readable" states, preventing a dequeuer from reading a slot that has been recycled by a fast producer.
5. Handle the full queue case: `Enqueue` returns `false` (non-blocking) when the queue is full rather than spinning or blocking. Handle the empty queue case: `Dequeue` returns `(zero, false)` when the queue is empty.
6. Support arbitrary value types using Go generics (`Queue[T]`).
7. Implement a `TryEnqueueTimeout(value, timeout)` variant that retries the enqueue in a spin loop with exponential backoff (using `runtime.Gosched()`) until success or timeout.
8. Achieve throughput of at least 10 million operations per second with 8 producers and 8 consumers on a modern multi-core machine.

## Hints

- The classic Dmitry Vyukov MPMC bounded queue uses a ring buffer where each cell has a `sequence` field initialized to its index; producers CAS on `enqueuePos`, consumers CAS on `dequeuePos`.
- Use `sync/atomic.Uint64` (Go 1.19+) for the head and tail indices, and `sync/atomic.Uint64` for per-cell sequence numbers.
- The key invariant: a cell is writable when `cell.sequence == pos` (where pos is the claimed tail position), and readable when `cell.sequence == pos + 1`.
- Use `pos & (capacity - 1)` for index wrapping (requires power-of-2 capacity).
- `runtime.Gosched()` yields the goroutine's time slice and is preferable to busy-spinning in Go's cooperative scheduler.
- Padding struct fields to 64 bytes (cache line size) prevents false sharing between the head and tail indices: use `_ [56]byte` padding fields.
- Test with `go test -race -count=100` to stress-test for data races.

## Success Criteria

1. 8 producers and 8 consumers each processing 1 million items results in exactly 8 million items dequeued with no duplicates and no lost items.
2. The queue passes `go test -race` with 100 concurrent goroutines.
3. No mutex, channel, or `sync.Mutex`/`sync.RWMutex` appears anywhere in the implementation.
4. Throughput exceeds 10 million ops/sec with 8P/8C on a machine with 8+ cores.
5. `Enqueue` on a full queue returns `false` immediately without blocking.
6. `Dequeue` on an empty queue returns `false` immediately without blocking.
7. The queue handles uint64 index wraparound correctly (test with artificially high starting indices).

## Research Resources

- Dmitry Vyukov's bounded MPMC queue -- https://www.1024cores.net/home/lock-free-algorithms/queues/bounded-mpmc-queue
- "Simple, Fast, and Practical Non-Blocking and Blocking Concurrent Queue Algorithms" (Michael & Scott, 1996)
- Go `sync/atomic` package documentation -- https://pkg.go.dev/sync/atomic
- "The Go Memory Model" -- https://go.dev/ref/mem
- Intel 64 Architecture Memory Ordering White Paper
- "C++ Concurrency in Action" (Williams, 2019) -- Chapter 7 on lock-free data structures
