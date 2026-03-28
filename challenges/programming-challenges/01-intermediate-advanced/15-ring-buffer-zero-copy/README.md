<!-- difficulty: intermediate-advanced -->
<!-- category: data-structures -->
<!-- languages: [rust] -->
<!-- concepts: [ring-buffer, atomics, zero-copy, memory-mapping, lock-free] -->
<!-- estimated_time: 5-7 hours -->
<!-- bloom_level: apply, analyze -->
<!-- prerequisites: [slices-and-lifetimes, atomic-operations-basics, memory-layout, unix-mmap-concept] -->

# Challenge 15: Ring Buffer with Zero-Copy Semantics

## Languages

Rust (stable, latest edition)

## Prerequisites

- Solid understanding of Rust slices, lifetimes, and borrowing rules
- Basic knowledge of atomic operations (`AtomicUsize`, `Ordering`)
- Familiarity with memory layout and alignment concepts
- Conceptual understanding of memory-mapped I/O (`mmap`)

## Learning Objectives

- **Implement** a fixed-size circular buffer that handles wraparound correctly
- **Apply** zero-copy read semantics by returning slices into the buffer's internal memory
- **Design** a lock-free SPSC variant using atomic read/write indices
- **Analyze** the performance difference between copying and zero-copy access patterns
- **Evaluate** the virtual memory mapping trick for contiguous views of wrapped data

## The Challenge

Build a ring buffer (circular buffer) -- a fixed-size FIFO queue that overwrites the oldest data when full. Ring buffers are fundamental in audio processing, network I/O, and inter-thread communication because they avoid allocation and provide predictable memory access patterns.

Your basic implementation must support zero-copy reads: instead of copying data out, return slices that borrow directly from the buffer's internal storage. The challenge is that data may wrap around the end of the array, requiring either two slices or a contiguous view trick.

Implement a lock-free SPSC (single-producer, single-consumer) variant using atomic operations for the read and write indices. This variant must be safe for use across two threads without any mutex. For bonus points, implement the mmap double-mapping trick: map the same physical memory twice in contiguous virtual address space so that wrapped data always appears contiguous.

## Requirements

1. Implement `RingBuffer<T>` with fixed capacity specified at construction
2. Provide `push()` (returns `Option<T>` with evicted element if full) and `pop()` methods
3. Implement zero-copy `read_slices()` returning `(&[T], &[T])` -- two slices covering the readable region (second is empty when data does not wrap)
4. Provide `write_slices_mut()` returning `(&mut [T], &mut [T])` for the writable region
5. Support batch `push_slice()` and `pop_slice()` for efficient bulk operations
6. Implement `SpscRingBuffer<T>` using `AtomicUsize` for head and tail indices
7. The SPSC variant must be `Send` and usable from two threads without locks
8. Implement `len()`, `capacity()`, `is_empty()`, `is_full()`, and `clear()`
9. Provide a `ContiguousRingBuffer<u8>` that uses mmap to create a double-mapped virtual view (Unix only, behind a feature flag)
10. Write tests proving SPSC correctness under concurrent producer/consumer workloads

## Hints

<details>
<summary>Hint 1: Basic ring buffer structure</summary>

```rust
struct RingBuffer<T> {
    buffer: Vec<T>,
    head: usize,   // next read position
    tail: usize,   // next write position
    len: usize,    // current element count
    capacity: usize,
}

impl<T> RingBuffer<T> {
    fn new(capacity: usize) -> Self {
        assert!(capacity > 0);
        Self {
            buffer: Vec::with_capacity(capacity),
            head: 0,
            tail: 0,
            len: 0,
            capacity,
        }
    }

    fn advance(index: usize, capacity: usize) -> usize {
        (index + 1) % capacity
    }
}
```

Initialize the `Vec` with the full capacity pre-filled (using `Default` or `MaybeUninit` for types without `Default`).

</details>

<details>
<summary>Hint 2: Zero-copy read as two slices</summary>

When data wraps around, return two slices:

```rust
fn read_slices(&self) -> (&[T], &[T]) {
    if self.len == 0 {
        return (&[], &[]);
    }
    if self.head < self.tail {
        (&self.buffer[self.head..self.tail], &[])
    } else {
        (&self.buffer[self.head..], &self.buffer[..self.tail])
    }
}
```

</details>

<details>
<summary>Hint 3: SPSC with atomics</summary>

The key insight: only the producer writes to `tail`, only the consumer writes to `head`. Each reads the other's index. This needs only `Acquire`/`Release` ordering:

```rust
use std::sync::atomic::{AtomicUsize, Ordering};

struct SpscRingBuffer<T> {
    buffer: Box<[std::mem::MaybeUninit<T>]>,
    head: AtomicUsize,   // consumer reads/writes, producer reads
    tail: AtomicUsize,   // producer reads/writes, consumer reads
    capacity: usize,
}

// Producer: write at tail, then Release-store new tail
// Consumer: read at head, then Release-store new head
// Reader loads the other's index with Acquire
```

</details>

<details>
<summary>Hint 4: The mmap double-mapping trick</summary>

Map the same physical memory at two adjacent virtual addresses. When data wraps, the second mapping makes it appear contiguous:

```rust
// Conceptual steps (Unix):
// 1. Create anonymous mmap of 2 * capacity
// 2. Create a file descriptor (memfd_create or shm_open)
// 3. ftruncate to capacity
// 4. mmap fd at base_addr with capacity
// 5. mmap fd at base_addr + capacity with capacity
// Now buffer[capacity..2*capacity] is the same physical memory as buffer[0..capacity]
```

This means a slice starting near the end that wraps around will still be contiguous in virtual memory.

</details>

## Acceptance Criteria

- [ ] `RingBuffer<T>` correctly handles push, pop, and wraparound
- [ ] `push()` on a full buffer returns the evicted (oldest) element
- [ ] `read_slices()` returns valid borrowed slices with no data copying
- [ ] Batch operations (`push_slice`, `pop_slice`) work correctly across wrap boundaries
- [ ] `SpscRingBuffer<T>` is safe for concurrent use from exactly two threads
- [ ] SPSC variant uses only atomic operations, no mutexes
- [ ] Stress test: producer and consumer exchanging 1M+ items with no data corruption
- [ ] `ContiguousRingBuffer<u8>` (mmap variant) provides single contiguous slices across wraps
- [ ] All tests pass with `cargo test`

## Research Resources

- [Lock-Free Single-Producer Single-Consumer Queue (1024cores)](https://www.1024cores.net/home/lock-free-algorithms/queues/bounded-mpmc-queue) -- reference SPSC designs
- [Virtual Ring Buffer (Wikipedia)](https://en.wikipedia.org/wiki/Circular_buffer#Optimization) -- the mmap double-mapping trick
- [Rust Atomics and Locks (Mara Bos), Chapter 5](https://marabos.nl/atomics/) -- channels and ring buffers with atomics
- [The `memfd_create` man page](https://man7.org/linux/man-pages/man2/memfd_create.2.html) -- anonymous file descriptors for shared memory
- [Rust `MaybeUninit` documentation](https://doc.rust-lang.org/std/mem/struct.MaybeUninit.html) -- safe handling of uninitialized memory
- [Linux Kernel Ring Buffer (kfifo)](https://www.kernel.org/doc/html/latest/core-api/kfifo.html) -- kernel-level ring buffer design for reference
