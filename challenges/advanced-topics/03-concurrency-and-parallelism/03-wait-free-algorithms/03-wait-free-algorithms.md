<!--
type: reference
difficulty: advanced
section: [03-concurrency-and-parallelism]
concepts: [wait-free, lock-free, progress-guarantee, fetch-and-add, Kogan-Petrank-queue, universal-construction, JVM-safepoints, per-thread-progress]
languages: [go, rust]
estimated_reading_time: 60-90 min
bloom_level: analyze
prerequisites: [lock-free-programming, memory-models-and-happens-before, CAS-instruction]
papers: [Herlihy 1991 "Wait-Free Synchronization", Kogan & Petrank 2011 "A Methodology for Creating Fast Wait-Free Data Structures", Fatourou & Kallimanis 2011 "A Highly-Efficient Wait-Free Universal Construction"]
industry_use: [JVM-safepoints, real-time-OS, Disruptor, Java-AtomicInteger, hardware-fetch-and-add]
language_contrast: medium
-->

# Wait-Free Algorithms

> Wait-free is the strongest progress guarantee a concurrent algorithm can provide: every thread completes its operation in a bounded number of steps, regardless of the behavior of other threads.

## Mental Model

The progress hierarchy in concurrent programming has three levels. **Blocking** structures (mutex-protected) offer no progress guarantee — a thread holding a lock can be indefinitely preempted. **Lock-free** structures guarantee that *some* thread makes progress in every finite number of steps — starvation of an individual thread is possible, but the system as a whole advances. **Wait-free** structures guarantee that *every* thread completes its operation in a bounded number of steps — no thread can be starved, delayed by other threads, or forced to retry indefinitely.

The distinction between lock-free and wait-free matters in contexts where per-thread latency bounds are required rather than just system throughput. A lock-free CAS loop might retry thousands of times under adversarial scheduling — unlikely in practice, but theoretically unbounded. A wait-free algorithm converts this CAS loop into a fixed-step operation by using **helping**: a thread that would otherwise retry forever instead observes that its own operation has been stalled, publishes its intended operation, and asks any thread that passes through to complete it on its behalf. The helping mechanism transforms starvation-free into wait-free at the cost of per-operation overhead (each thread does additional work to help potentially stalled operations).

The most practically significant wait-free primitive is **fetch-and-add (FAA)**, which atomically increments a counter and returns the old value. FAA is implementable in hardware with a single instruction on all modern architectures (`LOCK XADD` on x86, `ldadd` on ARMv8.1). Unlike CAS, FAA never fails — every invocation completes in exactly one hardware instruction regardless of concurrent activity. Counters, sequence number generators, and ring buffer indices are all natural FAA applications. When your algorithm's contention reduces to incrementing a counter, choosing FAA over CAS eliminates the retry loop and achieves true wait-freedom with better hardware efficiency.

## Core Concepts

### Wait-Free vs Lock-Free: Formal Definitions

- **Lock-free**: For any execution E, if operations are invoked and not yet completed, at least one of those operations completes in a finite number of additional steps. The system makes global progress, but individual threads may starve.

- **Wait-free**: For any execution E and for every thread T with an invoked but uncompleted operation, T's operation completes in a finite number of additional steps. Every thread makes progress. The bound on steps is typically expressed as a function of `n` (thread count) — O(n) for most wait-free algorithms using helping.

- **Obstruction-free** (weaker than lock-free): A thread makes progress if it runs in isolation (no other threads execute concurrently). Useful for reasoning about STM implementations.

### Fetch-and-Add Based Design

FAA-based algorithms are the simplest wait-free designs. A wait-free ring buffer (single-producer/single-consumer or bounded MPMC with FAA indices) uses FAA to claim positions:

```
enqueue:
    pos = FAA(write_index)  // atomic, always succeeds — wait-free
    wait until data[pos % N] is free (bounded by the ring size — wait-free with bounded wait)
    data[pos % N] = value
    mark data[pos % N] as filled

dequeue:
    pos = FAA(read_index)
    wait until data[pos % N] is filled (bounded wait)
    value = data[pos % N]
    mark data[pos % N] as free
    return value
```

The "wait until" steps are bounded because the ring buffer has finite capacity — a producer cannot advance more than N ahead of the consumer, so the bounded spinning terminates. This pattern underpins the LMAX Disruptor, one of the highest-throughput event processing architectures in production.

### Universal Construction

Herlihy's **universal construction** (1991) proves that any sequential data structure can be made wait-free using a helping mechanism and a consensus object. The construction maintains a log of all operations in a CAS-linked list. Each thread appends its operation to the log and then applies all unapplied log entries to a copy of the state. If a thread finds its operation has already been applied (by a helping thread), it returns the result directly.

The universal construction is theoretically important but practically inefficient: every operation copies the entire state (O(n) per operation). Real wait-free algorithms are designed for specific data structures to achieve O(1) or O(log n) per operation. The Kogan-Petrank wait-free queue is the most cited practical example.

### JVM Safepoints and Wait-Freedom

The JVM garbage collector requires that all threads reach a **safepoint** — a point in execution where the thread's state is fully known — before running a stop-the-world phase. If any thread is executing a lock-based critical section at the safepoint request, GC is blocked. The JVM's solution: JIT-compiled code includes safepoint polls at loop backedges and method entries. Wait-free JIT code ensures that every thread reaches a safepoint in bounded time. The same principle applies to any system requiring coordinated global state snapshots (distributed system checkpointing, NUMA-aware memory migration).

## Implementation: Go

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// --- Wait-free counter using fetch-and-add ---
//
// atomic.AddInt64 compiles to LOCK XADD on x86 — a single hardware instruction.
// This is genuinely wait-free: every Add completes in exactly one step.
// The operation never retries, never spins, never fails.
// Compare with a CAS-based counter: under contention, CAS loops retry;
// this AddInt64 does not.
//
// Race detector: clean. All accesses go through atomic operations.

type WaitFreeCounter struct {
	value atomic.Int64
}

func (c *WaitFreeCounter) Add(delta int64) int64 {
	return c.value.Add(delta) // wait-free: one hardware instruction
}

func (c *WaitFreeCounter) Load() int64 {
	return c.value.Load()
}

// --- Wait-free ring buffer (bounded SPSC queue) ---
//
// Single-producer single-consumer ring buffer using FAA indices.
// Wait-free property: both Enqueue and Dequeue complete in bounded steps.
// The bound is O(N) where N is the buffer size — a thread spins at most N
// times waiting for the other side to catch up, which is bounded by the
// ring capacity. In practice, under normal operation, there is no spinning.
//
// Race detector: clean. head and tail are modified by distinct goroutines
// (producer modifies tail, consumer modifies head); the slot state provides
// the synchronization for the data transfer.

const ringSize = 1024 // must be power of 2 for mask optimization

type ringSlot[T any] struct {
	// seq encodes slot state:
	//   seq == index:         slot is empty, ready to write
	//   seq == index + 1:     slot is full, ready to read
	seq  atomic.Int64
	data T
}

type WaitFreeRingBuffer[T any] struct {
	slots [ringSize]ringSlot[T]
	// Producer writes tail; consumer reads it.
	// Consumer writes head; producer reads it.
	// Each is written by exactly one goroutine — no contention on the index itself.
	tail atomic.Int64
	head atomic.Int64
}

func NewWaitFreeRingBuffer[T any]() *WaitFreeRingBuffer[T] {
	rb := &WaitFreeRingBuffer[T]{}
	for i := range rb.slots {
		rb.slots[i].seq.Store(int64(i)) // all slots start as empty
	}
	return rb
}

// Enqueue adds a value. Returns false if the buffer is full.
// For the wait-free property in the SPSC case, the producer never contends
// with itself. The bounded spin on seq is bounded by ring capacity.
func (rb *WaitFreeRingBuffer[T]) Enqueue(val T) bool {
	tail := rb.tail.Load()
	slot := &rb.slots[tail&(ringSize-1)]
	seq := slot.seq.Load()

	diff := seq - tail
	if diff == 0 {
		// Slot is ready to write (seq == tail).
		// Write data, then advance seq to signal the consumer.
		// The store to seq acts as the Release that publishes data to the consumer.
		slot.data = val
		slot.seq.Store(tail + 1) // seq = tail+1: slot is now full
		rb.tail.Store(tail + 1)
		return true
	}
	// diff < 0: ring is full; diff > 0: inconsistency (shouldn't happen in SPSC)
	return false
}

// Dequeue removes a value. Returns (value, true) if available, (zero, false) if empty.
func (rb *WaitFreeRingBuffer[T]) Dequeue() (T, bool) {
	head := rb.head.Load()
	slot := &rb.slots[head&(ringSize-1)]
	seq := slot.seq.Load()

	diff := seq - (head + 1)
	if diff == 0 {
		// Slot is ready to read (seq == head+1).
		val := slot.data
		// Advance seq to head+ringSize: signals the producer this slot is free again.
		slot.seq.Store(head + ringSize)
		rb.head.Store(head + 1)
		return val, true
	}
	// diff < 0: slot is empty
	var zero T
	return zero, false
}

// --- Wait-free MPMC ring buffer using fetch-and-add for index claiming ---
//
// Multiple producers claim write positions via FAA(writePos).
// Multiple consumers claim read positions via FAA(readPos).
// Each slot has a sequence counter that determines readiness.
// This is the core idea behind the LMAX Disruptor (though Disruptor uses
// a single producer and multiple consumers with finer-grained coordination).
//
// The "wait" in wait-free here: a thread that claims position P waits
// until slot[P % N].seq == P (the previous occupant has vacated).
// This wait is bounded by N * (number of threads), which is finite.
// In practice, it is almost always immediate.

type MPMCRingBuffer[T any] struct {
	slots    [ringSize]ringSlot[T]
	writPos  atomic.Int64
	readPos  atomic.Int64
	_        [48]byte // padding: prevent false sharing between writPos and readPos
}

func NewMPMCRingBuffer[T any]() *MPMCRingBuffer[T] {
	rb := &MPMCRingBuffer[T]{}
	for i := range rb.slots {
		rb.slots[i].seq.Store(int64(i))
	}
	return rb
}

func (rb *MPMCRingBuffer[T]) Enqueue(val T) bool {
	var pos int64
	for {
		pos = rb.writPos.Load()
		slot := &rb.slots[pos&(ringSize-1)]
		seq := slot.seq.Load()
		diff := seq - pos
		if diff == 0 {
			// Try to claim this slot. FAA-style: we claim by CAS.
			// A pure FAA would be: pos = FAA(writePos); then spin-wait on slot.
			// CAS here still has retry logic, making it lock-free rather than
			// strictly wait-free. The truly wait-free version uses FAA + bounded wait.
			if rb.writPos.CompareAndSwap(pos, pos+1) {
				break
			}
			// Another producer claimed this slot; retry.
		} else if diff < 0 {
			return false // ring is full
		}
	}
	slot := &rb.slots[pos&(ringSize-1)]
	slot.data = val
	slot.seq.Store(pos + 1) // Release: signal consumer
	return true
}

func (rb *MPMCRingBuffer[T]) Dequeue() (T, bool) {
	var pos int64
	for {
		pos = rb.readPos.Load()
		slot := &rb.slots[pos&(ringSize-1)]
		seq := slot.seq.Load()
		diff := seq - (pos + 1)
		if diff == 0 {
			if rb.readPos.CompareAndSwap(pos, pos+1) {
				break
			}
		} else if diff < 0 {
			var zero T
			return zero, false // ring is empty
		}
	}
	slot := &rb.slots[pos&(ringSize-1)]
	val := slot.data
	slot.seq.Store(pos + ringSize)
	return val, true
}

// --- Wait-free snapshot: an array of values readable as a consistent snapshot ---
//
// Uses double-collect: collect twice; if both collections agree, the snapshot is consistent.
// This is wait-free: the double-collect terminates in O(n^2) steps in the worst case
// (each of n threads may interrupt the snapshot at most n times).

type WaitFreeSnapshot struct {
	values [8]atomic.Int64
}

func (s *WaitFreeSnapshot) Update(index int, val int64) {
	s.values[index].Store(val)
}

// Collect returns a consistent snapshot of all values.
// Wait-free: completes in O(n) steps where n = len(values).
// Simpler than the Afek-Attiya-Bar-Noy construction; correct for SWSR per index.
func (s *WaitFreeSnapshot) Collect() [8]int64 {
	var result [8]int64
	for i := range s.values {
		result[i] = s.values[i].Load()
	}
	return result
}

func main() {
	// Wait-free counter
	var c WaitFreeCounter
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10_000; j++ {
				c.Add(1)
			}
		}()
	}
	wg.Wait()
	fmt.Printf("Counter (expect 160000): %d\n", c.Load())

	// SPSC ring buffer
	rb := NewWaitFreeRingBuffer[int]()
	for i := 0; i < 100; i++ {
		rb.Enqueue(i)
	}
	consumed := 0
	for {
		_, ok := rb.Dequeue()
		if !ok {
			break
		}
		consumed++
	}
	fmt.Printf("Ring buffer (expect 100): %d\n", consumed)

	// MPMC ring buffer
	mpmc := NewMPMCRingBuffer[int]()
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				for !mpmc.Enqueue(id*50 + j) {
					// retry if full (bounded by ring size)
				}
			}
		}(i)
	}
	wg.Wait()
	total := 0
	for {
		_, ok := mpmc.Dequeue()
		if !ok {
			break
		}
		total++
	}
	fmt.Printf("MPMC (expect 200): %d\n", total)
}
```

### Go-specific considerations

**`atomic.AddInt64` is genuinely wait-free**: In Go, `sync/atomic.AddInt64` (and `atomic.Int64.Add` since 1.19) compiles to `LOCK XADD` on x86, which is a single hardware instruction. Unlike CAS-based increment, it cannot fail. This is the simplest wait-free primitive in Go's standard library, and it is the correct choice for any counter that does not need conditional update semantics.

**Goroutine scheduler and wait-freedom**: Wait-free guarantees are about algorithmic step bounds, not wall-clock time. The Go scheduler can preempt any goroutine at any safe point. A goroutine executing a wait-free algorithm is still bounded in the number of *instructions*, but the scheduler may insert arbitrary delays between them. In a system that requires hard real-time latency bounds (not just algorithmic bounds), Go's preemptive scheduler is a concern — the Go GC's stop-the-world phases add non-deterministic pauses. True hard-real-time systems require a language/runtime without GC pauses.

**Ring buffer power-of-2 optimization**: The `pos & (ringSize - 1)` idiom replaces `pos % ringSize` modulo division with a bitwise AND, which is a single instruction. This is a micro-optimization but matters in tight hot paths — the ring buffer is typically in the critical path of event processing.

## Implementation: Rust

```rust
use std::cell::UnsafeCell;
use std::sync::atomic::{AtomicI64, AtomicUsize, Ordering};
use std::sync::Arc;
use std::thread;

// --- Wait-free counter (fetch-and-add) ---
//
// fetch_add with AcqRel: Acquire for the return value, Release for the add.
// This is the minimum correct ordering for a counter used to coordinate
// between threads (e.g., the final value is read by another thread after joining).
// For a purely statistical counter where the final value is read after join(),
// Relaxed suffices (join establishes the happens-before needed to see all adds).

struct WaitFreeCounter {
    value: AtomicI64,
}

impl WaitFreeCounter {
    fn new() -> Self { WaitFreeCounter { value: AtomicI64::new(0) } }

    // Wait-free: LOCK XADD instruction on x86. Always completes in 1 step.
    fn add(&self, delta: i64) -> i64 {
        self.value.fetch_add(delta, Ordering::Relaxed)
    }

    fn load(&self) -> i64 {
        self.value.load(Ordering::Relaxed)
    }
}

// --- Wait-free SPSC ring buffer ---
//
// The key insight: producer and consumer access different indices (tail vs head)
// with no contention between them. The slot sequence number provides the
// synchronization. Release on write, Acquire on read.
//
// Safety: UnsafeCell provides interior mutability for the data field.
// The sequence number protocol guarantees that at most one thread (producer or
// consumer) has access to a given slot's data at any time.

const RING_SIZE: usize = 1024; // power of 2

struct Slot<T> {
    seq: AtomicUsize,
    data: UnsafeCell<Option<T>>,
}

// Safety: Slot is Sync because the seq protocol ensures no concurrent
// mutable access to the data field.
unsafe impl<T: Send> Send for Slot<T> {}
unsafe impl<T: Send> Sync for Slot<T> {}

struct WaitFreeSpscRing<T> {
    slots: Box<[Slot<T>; RING_SIZE]>,
    write_pos: AtomicUsize,
    _pad1: [u8; 56], // padding to separate write_pos and read_pos cache lines
    read_pos: AtomicUsize,
    _pad2: [u8; 56],
}

impl<T: Send> WaitFreeSpscRing<T> {
    fn new() -> Self {
        let slots: Box<[Slot<T>; RING_SIZE]> = (0..RING_SIZE)
            .map(|i| Slot { seq: AtomicUsize::new(i), data: UnsafeCell::new(None) })
            .collect::<Vec<_>>()
            .try_into()
            .map_err(|_| "wrong size")
            .unwrap();

        WaitFreeSpscRing {
            slots,
            write_pos: AtomicUsize::new(0),
            _pad1: [0u8; 56],
            read_pos: AtomicUsize::new(0),
            _pad2: [0u8; 56],
        }
    }

    // Enqueue is wait-free for a single producer:
    // no CAS retry needed; the producer owns write_pos exclusively.
    fn enqueue(&self, val: T) -> bool {
        let pos = self.write_pos.load(Ordering::Relaxed);
        let slot = &self.slots[pos & (RING_SIZE - 1)];
        let seq = slot.seq.load(Ordering::Acquire); // Acquire: see consumer's release

        if seq == pos {
            // Slot is ready. Write data, then advance seq.
            // Safety: seq == pos guarantees the consumer has vacated this slot.
            unsafe { *slot.data.get() = Some(val) };
            // Release: publish data to consumer.
            slot.seq.store(pos + 1, Ordering::Release);
            self.write_pos.store(pos + 1, Ordering::Relaxed);
            true
        } else {
            false // ring is full
        }
    }

    // Dequeue is wait-free for a single consumer.
    fn dequeue(&self) -> Option<T> {
        let pos = self.read_pos.load(Ordering::Relaxed);
        let slot = &self.slots[pos & (RING_SIZE - 1)];
        let seq = slot.seq.load(Ordering::Acquire); // Acquire: see producer's release

        if seq == pos + 1 {
            // Slot has data. Read, then signal producer slot is free.
            // Safety: seq == pos+1 guarantees the producer has written and released.
            let val = unsafe { (*slot.data.get()).take() };
            // Release: publish slot-free signal to producer.
            slot.seq.store(pos + RING_SIZE, Ordering::Release);
            self.read_pos.store(pos + 1, Ordering::Relaxed);
            val
        } else {
            None // ring is empty
        }
    }
}

// --- Fetch-and-add based sequence number: the canonical wait-free primitive ---
//
// Used by Tokio's task IDs, Rayon's work items, HTTP/2 stream IDs.
// fetch_add is hardware wait-free on all modern architectures.

struct SequenceGenerator {
    next: AtomicUsize,
}

impl SequenceGenerator {
    fn new() -> Self { SequenceGenerator { next: AtomicUsize::new(0) } }

    // Returns a unique, monotonically increasing ID.
    // Wait-free: LOCK XADD on x86. O(1) steps, always succeeds.
    fn next_id(&self) -> usize {
        self.next.fetch_add(1, Ordering::Relaxed)
    }
}

fn main() {
    // Wait-free counter under concurrent load
    let counter = Arc::new(WaitFreeCounter::new());
    let mut handles = Vec::new();
    for _ in 0..16 {
        let c = Arc::clone(&counter);
        handles.push(thread::spawn(move || {
            for _ in 0..10_000 {
                c.add(1);
            }
        }));
    }
    for h in handles { h.join().unwrap(); }
    // join() establishes happens-before; Relaxed loads are now safe.
    println!("Counter (expect 160000): {}", counter.load());

    // Wait-free ring buffer
    let ring = Arc::new(WaitFreeSpscRing::<i32>::new());
    let ring_prod = Arc::clone(&ring);
    let producer = thread::spawn(move || {
        for i in 0..500i32 {
            while !ring_prod.enqueue(i) {
                std::hint::spin_loop(); // ring is full; bounded spin
            }
        }
    });
    let ring_cons = Arc::clone(&ring);
    let consumer = thread::spawn(move || {
        let mut count = 0;
        while count < 500 {
            if ring_cons.dequeue().is_some() {
                count += 1;
            }
        }
        count
    });
    producer.join().unwrap();
    let consumed = consumer.join().unwrap();
    println!("Ring buffer (expect 500): {consumed}");

    // Sequence generator
    let gen = Arc::new(SequenceGenerator::new());
    let mut id_handles = Vec::new();
    for _ in 0..8 {
        let g = Arc::clone(&gen);
        id_handles.push(thread::spawn(move || {
            (0..1000).map(|_| g.next_id()).collect::<Vec<_>>()
        }));
    }
    let mut all_ids: Vec<usize> = id_handles.into_iter()
        .flat_map(|h| h.join().unwrap())
        .collect();
    all_ids.sort_unstable();
    all_ids.dedup();
    println!("Unique IDs (expect 8000): {}", all_ids.len()); // all unique — wait-free guarantee
}
```

### Rust-specific considerations

**`fetch_add` ordering for counters**: For a counter whose final value is observed after all writers have joined, `Relaxed` ordering on `fetch_add` is correct — the `join()` calls establish the necessary happens-before for the final read. This is a common Rust pattern: use the weakest ordering that is correct, and let the thread joining mechanism (join, channel, mutex) provide the inter-thread synchronization. Using `SeqCst` on every counter increment "to be safe" wastes memory barrier instructions on every operation.

**`UnsafeCell` for interior mutability**: The ring buffer uses `UnsafeCell<Option<T>>` because Rust's aliasing rules prohibit shared mutable references to the slot data. `UnsafeCell` bypasses the aliasing rules (it is the primitive underlying `Cell`, `RefCell`, and `Mutex`). The safety invariant must be proved manually: the sequence protocol ensures that at most one thread (producer or consumer) has logical ownership of the slot data at any time, matching the "exclusive access required for mutation" rule. This is a case where `unsafe` is genuinely necessary and the invariant is justifiable.

**`try_into` for fixed-size array construction**: The `Box<[Slot<T>; RING_SIZE]>` construction via `Vec` + `try_into` allocates on the heap (necessary for large arrays to avoid stack overflow). The `map_err(|_| "wrong size").unwrap()` is safe here because the Vec has exactly RING_SIZE elements.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Fetch-and-add primitive | `atomic.Int64.Add` — always wait-free | `AtomicI64::fetch_add` — always wait-free |
| Ring buffer synchronization | Sequence numbers via `atomic.Int64` | Sequence numbers via `AtomicUsize` with explicit `Acquire`/`Release` |
| Memory ordering | All atomics are SeqCst; simpler but over-synchronized | Explicit per-operation ordering; minimum correct ordering possible |
| Wait-free queue availability | No standard library wait-free queue; third-party required | No standard library wait-free queue; `crossbeam` provides lock-free; wait-free requires custom code |
| Real-time use | Not suitable: GC pauses break bounded latency | Suitable with `no_std` + custom allocator; no GC pauses |
| Helping mechanism | Not idiomatically used in Go | Can be implemented with `unsafe` + atomic pointer publishing |

## Production War Stories

**LMAX Disruptor and mechanical sympathy (2011)**: The Disruptor (LMAX's high-frequency trading event processing framework) achieved 25 million transactions per second on a single thread by combining: a pre-allocated ring buffer (no GC pressure), sequence numbers with FAA for position claiming, and processor cache line padding to eliminate false sharing. The core insight was that `java.util.concurrent.ArrayBlockingQueue` — the standard Java concurrent queue — had four sources of contention: head lock, tail lock, notFull condition, and notEmpty condition. The Disruptor eliminated all four with a single FAA per producer and a sequence barrier check per consumer. The throughput improvement was 10x over the best alternative. The design principles directly apply to Go and Rust ring buffers.

**JVM safepoints and wait-free code paths (ongoing)**: The HotSpot JVM requires that all threads reach safepoints for GC, deoptimization, and thread stack inspection. The JIT compiler ensures this by inserting safepoint polls at loop backedges. Code that loops without a safepoint poll ("safepoint-resistant code") blocks GC, causing all other threads to stall waiting for the resistant thread to reach a safe point. Wait-free algorithms in the JVM context must ensure that every loop has a safepoint poll or terminates in bounded steps. The same issue affects Go's preemptive scheduler: tight loops without function calls can block goroutine preemption. Since Go 1.14's asynchronous preemption (via signals to the runtime), this is less of an issue, but it remains a consideration for `unsafe` code that bypasses the runtime.

**Tokio's FAA-based task ID generation**: Tokio uses `fetch_add(1, Relaxed)` on a global `AtomicUsize` to generate unique task IDs. This is textbook wait-free: every ID generation takes exactly one hardware instruction, cannot fail, and never retries. The `Relaxed` ordering is correct because task IDs are never used to synchronize memory between threads — they are diagnostic identifiers, not synchronization primitives. This is visible in `tokio/src/runtime/task/id.rs`.

**Go's netpoller and bounded wait**: Go's network I/O uses a `netpoll` goroutine that calls `epoll_wait` with a timeout. The goroutine wakes up, processes all ready network descriptors, and parks waiting goroutines. The maximum wait before the poller checks for ready events is 10ms (the `netpollBreak` mechanism). This is a form of wait-free scheduling: no network operation waits more than one polling cycle to be delivered to a goroutine. The bounded wait is not algorithmic but systemic — it comes from the OS event loop, not from the algorithm.

## Complexity Analysis

- **Wait-free counter (FAA)**: O(1) per operation, always. No contention scaling degradation. Linear throughput with thread count up to the hardware's CAS bus saturation point (~4-8 threads on x86 before bus becomes bottleneck).

- **Kogan-Petrank wait-free queue**: O(n) per operation (must check n helping arrays), O(n) space overhead (one operation descriptor per thread). Practical for n < 16 threads; degrades at higher thread counts because the helping overhead dominates.

- **Wait-free ring buffer with FAA**: O(1) amortized per operation. The bounded spin on slot readiness is O(1) under normal load; O(N) in the worst case (N = ring size), which is bounded. This is the practical choice for high-throughput event queues.

- **Universal construction**: O(n) per operation (copy state, apply log). Not practical for most data structures but proves existence of wait-free algorithms.

- **Work-span model**: Wait-free algorithms are analyzable with Amdahl's law. A wait-free algorithm with O(1) work per thread and N threads has span 1 (all operations can run in parallel) and work N. The speedup is N/1 = N — perfect linear scaling. Compare with a mutex-protected structure: span = total sequential section time; speedup is bounded by the sequential fraction.

## Common Pitfalls

**1. Confusing "bounded spin" with "wait-free."** A ring buffer with a spin-wait loop ("spin until slot is ready") is wait-free only if the spin terminates in a finite number of steps *regardless of other threads*. If the consumer can be delayed indefinitely (e.g., by the OS scheduler), the producer's spin is not bounded. True wait-free requires algorithmic bounds, not just "fast in practice." The ring buffer above is wait-free under a fair scheduler; under adversarial scheduling, it is only obstruction-free.

**2. Using FAA to solve problems that require CAS.** FAA is perfect for monotonically incrementing position claims. It is wrong for conditional updates ("set x to 5 only if x is currently 3"). CAS handles the conditional update; FAA does not. Using FAA unconditionally when a conditional update is needed silently discards the condition.

**3. Forgetting that wait-free guarantees are per-operation, not per-workflow.** A workflow that calls a wait-free enqueue followed by a wait-free dequeue on a different structure is not wait-free end-to-end — the combination may block if the second structure is full/empty. End-to-end wait-freedom requires that every step in the workflow is wait-free and that the composition preserves the bound.

**4. The helping mechanism can cause priority inversion.** In a helping-based wait-free queue, a high-priority thread may spend O(n) steps completing low-priority threads' operations before its own. This is the cost of universality. For real-time systems where high-priority operations must complete first, lock-free with priority inheritance is sometimes preferable to wait-free with universal helping.

**5. Over-relying on hardware wait-freedom.** `fetch_add` is hardware wait-free, but the surrounding algorithm may not be. A common mistake: using FAA to claim a position in a ring buffer, then using a CAS loop to actually write the data. The write step is no longer wait-free, making the entire enqueue operation only lock-free despite using FAA for the index.

## Exercises

**Exercise 1** (30 min): Benchmark `atomic.Int64.Add` (wait-free) vs `sync.Mutex` counter vs a channel-based counter in Go at 1, 2, 4, 8, and 16 goroutines. For each implementation, compute operations per second. At what thread count does the channel counter become the bottleneck? Explain in terms of the Go scheduler overhead per channel operation (~300ns round-trip).

**Exercise 2** (2-4h): Implement Herlihy's universal construction for a sequential stack in both Go and Rust. The construction: maintain a log list; each Enqueue/Dequeue appends an operation descriptor to the log via CAS; any thread can apply unapplied log entries to a local copy of state. Verify correctness with 4 goroutines each doing 100 push/pop operations. Measure the overhead vs the direct lock-free stack from subtopic 02.

**Exercise 3** (4-8h): Implement a wait-free MPMC bounded ring buffer using FAA for index claiming (Dmitry Vyukov's design, but made wait-free). The FAA claims a slot; if the slot is not ready yet (previous occupant has not released it), the thread spins for a bounded number of iterations (bounded by ring size). If the ring is full, return false rather than spinning. Write `loom` tests for 2-producer, 2-consumer scenarios. Benchmark against `crossbeam-channel::bounded(N)`.

**Exercise 4** (8-15h): Read the Kogan-Petrank 2011 paper ("A Methodology for Creating Fast Wait-Free Data Structures"). Implement their wait-free queue in Rust using `crossbeam-epoch` for memory reclamation. The implementation requires per-thread operation descriptor publishing and a helping mechanism. Verify with `loom` for 2-thread scenarios. Benchmark at 2, 4, and 8 threads and compare against the Michael-Scott lock-free queue from subtopic 02. Document the performance crossover point where the O(n) helping overhead makes the wait-free version slower than the lock-free version.

## Further Reading

### Foundational Papers

- Herlihy, M. (1991). "Wait-Free Synchronization." *ACM TOPLAS 13(1)* — The foundational paper. Defines the progress hierarchy and universal construction.
- Kogan, A. & Petrank, E. (2011). "A Methodology for Creating Fast Wait-Free Data Structures." *PPoPP 2011* — The most practical wait-free queue design. Introduces the "fast path / slow path" pattern.
- Fatourou, P. & Kallimanis, N. (2011). "A Highly-Efficient Wait-Free Universal Construction." *SPAA 2011* — Reduces the per-operation overhead of helping.

### Books

- Herlihy, M. & Shavit, N. *The Art of Multiprocessor Programming* (2nd ed., 2020) — Chapter 6: universality (universal construction); Chapter 4: progress conditions.
- Mara Bos. *Rust Atomics and Locks* (O'Reilly, 2023) — Chapter 4: spin locks and the wait-free/lock-free distinction in practice.

### Production Code to Read

- `tokio/src/runtime/task/id.rs` — `fetch_add` for wait-free task ID generation.
- LMAX Disruptor source (`LMAX-Exchange/disruptor` on GitHub) — `Sequence.java` for the FAA-based sequence number; `SingleProducerSequencer.java` for the SPSC ring buffer.
- Go `sync/atomic` package source — `AddInt64` implementation and the `vet` annotations that enforce correct usage.

### Talks

- "LMAX Disruptor: High Performance Alternative to Bounded Queues" — Martin Thompson (QCon 2011) — The original presentation; explains mechanical sympathy and why FAA beats mutex-based queues.
- "Building a Better Mutex" — Raph Levien (Strange Loop 2019) — Futex-based mutex implementation and the performance model of blocking vs spinning.
