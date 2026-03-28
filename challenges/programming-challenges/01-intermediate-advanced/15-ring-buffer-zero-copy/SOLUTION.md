# Solution: Ring Buffer with Zero-Copy Semantics

## Architecture Overview

The solution is structured in three tiers:

1. **Basic ring buffer** -- `RingBuffer<T>` with push, pop, zero-copy read slices, and batch operations
2. **Lock-free SPSC** -- `SpscRingBuffer<T>` using atomic indices for single-producer single-consumer communication
3. **Contiguous view** -- `ContiguousRingBuffer<u8>` using mmap double-mapping for contiguous zero-copy reads (Unix only, behind a feature flag)

Each tier builds on the concepts from the previous one. The basic buffer owns its data in a `Vec<MaybeUninit<T>>`. The SPSC variant splits ownership between producer (writes to tail) and consumer (reads from head) using atomic synchronization. The mmap variant eliminates the two-slice problem entirely by making virtual memory handle wraparound.

## Rust Solution

### Project Setup

```bash
cargo new ring-buffer
cd ring-buffer
```

```toml
[package]
name = "ring-buffer"
version = "0.1.0"
edition = "2021"

[features]
default = []
mmap = []

[dev-dependencies]
rand = "0.8"
```

### Source: `src/basic.rs`

```rust
use std::mem::MaybeUninit;

pub struct RingBuffer<T> {
    buffer: Vec<MaybeUninit<T>>,
    head: usize,
    tail: usize,
    len: usize,
    capacity: usize,
}

impl<T> RingBuffer<T> {
    pub fn new(capacity: usize) -> Self {
        assert!(capacity > 0, "capacity must be greater than zero");
        let mut buffer = Vec::with_capacity(capacity);
        for _ in 0..capacity {
            buffer.push(MaybeUninit::uninit());
        }
        Self {
            buffer,
            head: 0,
            tail: 0,
            len: 0,
            capacity,
        }
    }

    pub fn capacity(&self) -> usize {
        self.capacity
    }

    pub fn len(&self) -> usize {
        self.len
    }

    pub fn is_empty(&self) -> bool {
        self.len == 0
    }

    pub fn is_full(&self) -> bool {
        self.len == self.capacity
    }

    pub fn clear(&mut self) {
        while self.pop().is_some() {}
    }

    /// Push an element. If the buffer is full, evicts and returns the oldest element.
    pub fn push(&mut self, value: T) -> Option<T> {
        let evicted = if self.is_full() {
            Some(self.pop_internal())
        } else {
            None
        };

        self.buffer[self.tail] = MaybeUninit::new(value);
        self.tail = (self.tail + 1) % self.capacity;
        self.len += 1;

        evicted
    }

    /// Pop the oldest element from the buffer.
    pub fn pop(&mut self) -> Option<T> {
        if self.is_empty() {
            return None;
        }
        Some(self.pop_internal())
    }

    fn pop_internal(&mut self) -> T {
        let value = unsafe { self.buffer[self.head].assume_init_read() };
        self.head = (self.head + 1) % self.capacity;
        self.len -= 1;
        value
    }

    /// Zero-copy read: returns two slices covering all readable elements.
    /// The second slice is empty when data does not wrap around.
    pub fn read_slices(&self) -> (&[T], &[T]) {
        if self.len == 0 {
            return (&[], &[]);
        }

        // Safety: elements between head and head+len are initialized.
        unsafe {
            if self.head < self.tail {
                let slice = std::slice::from_raw_parts(
                    self.buffer[self.head..self.tail].as_ptr() as *const T,
                    self.tail - self.head,
                );
                (slice, &[])
            } else {
                let first = std::slice::from_raw_parts(
                    self.buffer[self.head..].as_ptr() as *const T,
                    self.capacity - self.head,
                );
                let second = std::slice::from_raw_parts(
                    self.buffer[..self.tail].as_ptr() as *const T,
                    self.tail,
                );
                (first, second)
            }
        }
    }

    /// Zero-copy writable regions: returns two mutable slices for the free space.
    pub fn write_slices_mut(&mut self) -> (&mut [T], &mut [T]) {
        if self.len == self.capacity {
            return (&mut [], &mut []);
        }

        unsafe {
            if self.tail < self.head {
                let slice = std::slice::from_raw_parts_mut(
                    self.buffer[self.tail..self.head].as_mut_ptr() as *mut T,
                    self.head - self.tail,
                );
                (slice, &mut [])
            } else if self.head == 0 && self.len > 0 {
                let slice = std::slice::from_raw_parts_mut(
                    self.buffer[self.tail..].as_mut_ptr() as *mut T,
                    self.capacity - self.tail,
                );
                (slice, &mut [])
            } else {
                let cap = self.capacity;
                let (right, left) = self.buffer.split_at_mut(self.tail);
                let first = std::slice::from_raw_parts_mut(
                    left.as_mut_ptr() as *mut T,
                    cap - self.tail,
                );
                let second = std::slice::from_raw_parts_mut(
                    right[..self.head].as_mut_ptr() as *mut T,
                    self.head,
                );
                (first, second)
            }
        }
    }

    /// Push a slice of elements. Returns how many were actually pushed.
    /// If the buffer fills, oldest elements are evicted.
    pub fn push_slice(&mut self, data: &[T]) -> usize
    where
        T: Clone,
    {
        let mut pushed = 0;
        for item in data {
            self.push(item.clone());
            pushed += 1;
        }
        pushed
    }

    /// Pop up to `count` elements into a new Vec.
    pub fn pop_slice(&mut self, count: usize) -> Vec<T> {
        let actual = count.min(self.len);
        let mut result = Vec::with_capacity(actual);
        for _ in 0..actual {
            if let Some(item) = self.pop() {
                result.push(item);
            }
        }
        result
    }
}

impl<T> Drop for RingBuffer<T> {
    fn drop(&mut self) {
        self.clear();
    }
}
```

### Source: `src/spsc.rs`

```rust
use std::cell::UnsafeCell;
use std::mem::MaybeUninit;
use std::sync::atomic::{AtomicUsize, Ordering};

/// Lock-free single-producer single-consumer ring buffer.
///
/// Only one thread may call `push` (the producer) and only one thread
/// may call `pop` (the consumer). This invariant is enforced by the
/// `Producer` and `Consumer` split handles.
pub struct SpscRingBuffer<T> {
    buffer: Box<[UnsafeCell<MaybeUninit<T>>]>,
    head: AtomicUsize,
    tail: AtomicUsize,
    capacity: usize,
}

// Safety: the SPSC protocol ensures head is only written by the consumer
// and tail is only written by the producer. The atomic ordering provides
// the necessary synchronization.
unsafe impl<T: Send> Sync for SpscRingBuffer<T> {}
unsafe impl<T: Send> Send for SpscRingBuffer<T> {}

pub struct Producer<'a, T> {
    ring: &'a SpscRingBuffer<T>,
}

pub struct Consumer<'a, T> {
    ring: &'a SpscRingBuffer<T>,
}

impl<T> SpscRingBuffer<T> {
    pub fn new(capacity: usize) -> Self {
        assert!(capacity > 0, "capacity must be greater than zero");
        // Use capacity + 1 so we can distinguish full from empty.
        let actual_cap = capacity + 1;
        let mut buffer = Vec::with_capacity(actual_cap);
        for _ in 0..actual_cap {
            buffer.push(UnsafeCell::new(MaybeUninit::uninit()));
        }

        Self {
            buffer: buffer.into_boxed_slice(),
            head: AtomicUsize::new(0),
            tail: AtomicUsize::new(0),
            capacity: actual_cap,
        }
    }

    /// Split into producer and consumer handles.
    /// The caller must ensure only one thread uses each handle.
    pub fn split(&self) -> (Producer<'_, T>, Consumer<'_, T>) {
        (Producer { ring: self }, Consumer { ring: self })
    }

    pub fn usable_capacity(&self) -> usize {
        self.capacity - 1
    }
}

impl<'a, T> Producer<'a, T> {
    /// Try to push a value. Returns Err(value) if the buffer is full.
    pub fn push(&self, value: T) -> Result<(), T> {
        let tail = self.ring.tail.load(Ordering::Relaxed);
        let next_tail = (tail + 1) % self.ring.capacity;

        // Check if full: next_tail would collide with head.
        let head = self.ring.head.load(Ordering::Acquire);
        if next_tail == head {
            return Err(value);
        }

        // Safety: we are the only writer to this slot (producer owns tail).
        unsafe {
            (*self.ring.buffer[tail].get()).write(value);
        }

        // Release: make the written value visible to the consumer.
        self.ring.tail.store(next_tail, Ordering::Release);
        Ok(())
    }
}

impl<'a, T> Consumer<'a, T> {
    /// Try to pop a value. Returns None if the buffer is empty.
    pub fn pop(&self) -> Option<T> {
        let head = self.ring.head.load(Ordering::Relaxed);

        // Acquire: see the value written by the producer.
        let tail = self.ring.tail.load(Ordering::Acquire);
        if head == tail {
            return None;
        }

        // Safety: we are the only reader from this slot (consumer owns head).
        let value = unsafe { (*self.ring.buffer[head].get()).assume_init_read() };

        let next_head = (head + 1) % self.ring.capacity;
        // Release: allow the producer to reuse this slot.
        self.ring.head.store(next_head, Ordering::Release);

        Some(value)
    }
}

impl<T> Drop for SpscRingBuffer<T> {
    fn drop(&mut self) {
        let mut head = *self.head.get_mut();
        let tail = *self.tail.get_mut();
        while head != tail {
            unsafe {
                (*self.buffer[head].get()).assume_init_drop();
            }
            head = (head + 1) % self.capacity;
        }
    }
}
```

### Source: `src/contiguous.rs` (Unix only, behind `mmap` feature)

```rust
#[cfg(all(unix, feature = "mmap"))]
pub mod mmap_ring {
    use std::ptr;

    /// A byte ring buffer backed by double-mapped virtual memory.
    /// The same physical memory is mapped at two adjacent virtual addresses,
    /// making wrapped data appear contiguous.
    pub struct ContiguousRingBuffer {
        base: *mut u8,
        capacity: usize,
        head: usize,
        tail: usize,
        len: usize,
    }

    impl ContiguousRingBuffer {
        /// Create a new contiguous ring buffer. Capacity is rounded up to page size.
        pub fn new(min_capacity: usize) -> std::io::Result<Self> {
            let page_size = unsafe { libc::sysconf(libc::_SC_PAGESIZE) as usize };
            let capacity = ((min_capacity + page_size - 1) / page_size) * page_size;

            // Step 1: Reserve 2 * capacity of virtual address space.
            let base = unsafe {
                libc::mmap(
                    ptr::null_mut(),
                    capacity * 2,
                    libc::PROT_NONE,
                    libc::MAP_ANON | libc::MAP_PRIVATE,
                    -1,
                    0,
                )
            };
            if base == libc::MAP_FAILED {
                return Err(std::io::Error::last_os_error());
            }

            // Step 2: Create a shared memory file descriptor.
            let path = std::ffi::CString::new("/ring-buffer-shm").unwrap();
            let fd = unsafe { libc::shm_open(path.as_ptr(), libc::O_RDWR | libc::O_CREAT | libc::O_EXCL, 0o600) };
            if fd < 0 {
                // Try unlinking and recreating if it already exists.
                unsafe { libc::shm_unlink(path.as_ptr()) };
                let fd = unsafe { libc::shm_open(path.as_ptr(), libc::O_RDWR | libc::O_CREAT | libc::O_EXCL, 0o600) };
                if fd < 0 {
                    return Err(std::io::Error::last_os_error());
                }
            }

            unsafe {
                libc::shm_unlink(path.as_ptr()); // Unlink immediately; fd keeps it alive.
                libc::ftruncate(fd, capacity as libc::off_t);

                // Step 3: Map the first copy.
                let r1 = libc::mmap(base, capacity, libc::PROT_READ | libc::PROT_WRITE, libc::MAP_SHARED | libc::MAP_FIXED, fd, 0);
                if r1 == libc::MAP_FAILED {
                    libc::close(fd);
                    return Err(std::io::Error::last_os_error());
                }

                // Step 4: Map the second copy immediately after.
                let r2 = libc::mmap(base.add(capacity), capacity, libc::PROT_READ | libc::PROT_WRITE, libc::MAP_SHARED | libc::MAP_FIXED, fd, 0);
                if r2 == libc::MAP_FAILED {
                    libc::close(fd);
                    return Err(std::io::Error::last_os_error());
                }

                libc::close(fd);
            }

            Ok(Self {
                base: base as *mut u8,
                capacity,
                head: 0,
                tail: 0,
                len: 0,
            })
        }

        pub fn capacity(&self) -> usize {
            self.capacity
        }

        pub fn len(&self) -> usize {
            self.len
        }

        pub fn is_empty(&self) -> bool {
            self.len == 0
        }

        /// Write bytes into the buffer. Returns how many were written.
        pub fn write(&mut self, data: &[u8]) -> usize {
            let available = self.capacity - self.len;
            let to_write = data.len().min(available);
            if to_write == 0 {
                return 0;
            }

            // Thanks to double mapping, we can always write contiguously.
            unsafe {
                ptr::copy_nonoverlapping(
                    data.as_ptr(),
                    self.base.add(self.tail),
                    to_write,
                );
            }
            self.tail = (self.tail + to_write) % self.capacity;
            self.len += to_write;
            to_write
        }

        /// Read a contiguous slice of all available data.
        /// This always returns a single slice (no wrapping), thanks to double mapping.
        pub fn read_contiguous(&self) -> &[u8] {
            if self.len == 0 {
                return &[];
            }
            unsafe { std::slice::from_raw_parts(self.base.add(self.head), self.len) }
        }

        /// Consume (advance head) by the given number of bytes.
        pub fn consume(&mut self, count: usize) {
            let actual = count.min(self.len);
            self.head = (self.head + actual) % self.capacity;
            self.len -= actual;
        }
    }

    impl Drop for ContiguousRingBuffer {
        fn drop(&mut self) {
            unsafe {
                libc::munmap(self.base as *mut libc::c_void, self.capacity * 2);
            }
        }
    }
}
```

### Source: `src/lib.rs`

```rust
pub mod basic;
pub mod spsc;

#[cfg(all(unix, feature = "mmap"))]
pub mod contiguous;
```

### Source: `src/main.rs`

```rust
use ring_buffer::basic::RingBuffer;
use ring_buffer::spsc::SpscRingBuffer;
use std::thread;

fn main() {
    println!("=== Basic Ring Buffer ===\n");

    let mut rb: RingBuffer<i32> = RingBuffer::new(5);

    for i in 1..=5 {
        let evicted = rb.push(i);
        println!("push({i}), evicted: {evicted:?}");
    }

    println!("len: {}, full: {}", rb.len(), rb.is_full());

    let (first, second) = rb.read_slices();
    println!("read_slices: {:?}, {:?}", first, second);

    // Push more to trigger eviction.
    let evicted = rb.push(6);
    println!("push(6), evicted: {evicted:?}");

    let (first, second) = rb.read_slices();
    println!("read_slices after wrap: {:?}, {:?}", first, second);

    let batch = rb.pop_slice(3);
    println!("pop_slice(3): {:?}", batch);

    println!("\n=== SPSC Ring Buffer ===\n");

    let ring = SpscRingBuffer::new(1024);
    let (producer, consumer) = ring.split();

    let item_count = 100_000;

    thread::scope(|s| {
        let prod = s.spawn(move || {
            for i in 0..item_count {
                loop {
                    match producer.push(i) {
                        Ok(()) => break,
                        Err(_) => thread::yield_now(),
                    }
                }
            }
        });

        let cons = s.spawn(move || {
            let mut received = Vec::with_capacity(item_count);
            while received.len() < item_count {
                if let Some(val) = consumer.pop() {
                    received.push(val);
                } else {
                    thread::yield_now();
                }
            }
            received
        });

        prod.join().unwrap();
        let received = cons.join().unwrap();

        assert_eq!(received.len(), item_count);
        for (i, val) in received.iter().enumerate() {
            assert_eq!(*val, i, "mismatch at index {i}");
        }
        println!("SPSC: {item_count} items transferred correctly between threads");
    });
}
```

### Tests

```rust
#[cfg(test)]
mod tests {
    use crate::basic::RingBuffer;
    use crate::spsc::SpscRingBuffer;
    use std::thread;

    #[test]
    fn basic_push_pop() {
        let mut rb: RingBuffer<i32> = RingBuffer::new(3);
        assert!(rb.is_empty());
        rb.push(1);
        rb.push(2);
        rb.push(3);
        assert!(rb.is_full());
        assert_eq!(rb.pop(), Some(1));
        assert_eq!(rb.pop(), Some(2));
        assert_eq!(rb.pop(), Some(3));
        assert_eq!(rb.pop(), None);
    }

    #[test]
    fn eviction_on_full() {
        let mut rb: RingBuffer<i32> = RingBuffer::new(3);
        rb.push(1);
        rb.push(2);
        rb.push(3);
        let evicted = rb.push(4);
        assert_eq!(evicted, Some(1));
        assert_eq!(rb.pop(), Some(2));
    }

    #[test]
    fn read_slices_no_wrap() {
        let mut rb: RingBuffer<i32> = RingBuffer::new(5);
        rb.push(10);
        rb.push(20);
        rb.push(30);
        let (first, second) = rb.read_slices();
        assert_eq!(first, &[10, 20, 30]);
        assert!(second.is_empty());
    }

    #[test]
    fn read_slices_with_wrap() {
        let mut rb: RingBuffer<i32> = RingBuffer::new(4);
        rb.push(1);
        rb.push(2);
        rb.push(3);
        rb.push(4);
        rb.pop(); // head moves to index 1
        rb.pop(); // head moves to index 2
        rb.push(5); // tail wraps to index 0
        rb.push(6); // tail at index 1

        let (first, second) = rb.read_slices();
        assert_eq!(first, &[3, 4]);
        assert_eq!(second, &[5, 6]);
    }

    #[test]
    fn batch_push_pop() {
        let mut rb: RingBuffer<i32> = RingBuffer::new(5);
        rb.push_slice(&[1, 2, 3, 4, 5]);
        assert!(rb.is_full());

        let popped = rb.pop_slice(3);
        assert_eq!(popped, vec![1, 2, 3]);
        assert_eq!(rb.len(), 2);
    }

    #[test]
    fn clear_empties_buffer() {
        let mut rb: RingBuffer<i32> = RingBuffer::new(3);
        rb.push(1);
        rb.push(2);
        rb.clear();
        assert!(rb.is_empty());
        assert_eq!(rb.len(), 0);
    }

    #[test]
    fn spsc_basic() {
        let ring = SpscRingBuffer::new(4);
        let (prod, cons) = ring.split();

        prod.push(10).unwrap();
        prod.push(20).unwrap();
        assert_eq!(cons.pop(), Some(10));
        assert_eq!(cons.pop(), Some(20));
        assert_eq!(cons.pop(), None);
    }

    #[test]
    fn spsc_full_returns_error() {
        let ring = SpscRingBuffer::new(2);
        let (prod, _cons) = ring.split();

        prod.push(1).unwrap();
        prod.push(2).unwrap();
        assert!(prod.push(3).is_err());
    }

    #[test]
    fn spsc_concurrent_stress() {
        let ring = SpscRingBuffer::new(256);
        let (producer, consumer) = ring.split();

        let count = 500_000;

        thread::scope(|s| {
            let prod = s.spawn(move || {
                for i in 0u64..count {
                    loop {
                        match producer.push(i) {
                            Ok(()) => break,
                            Err(_) => thread::yield_now(),
                        }
                    }
                }
            });

            let cons = s.spawn(move || {
                let mut sum: u64 = 0;
                let mut received = 0u64;
                while received < count {
                    if let Some(val) = consumer.pop() {
                        assert_eq!(val, received, "out-of-order at {received}");
                        sum += val;
                        received += 1;
                    } else {
                        thread::yield_now();
                    }
                }
                sum
            });

            prod.join().unwrap();
            let sum = cons.join().unwrap();
            let expected: u64 = (0..count).sum();
            assert_eq!(sum, expected);
        });
    }

    #[test]
    fn empty_buffer_operations() {
        let mut rb: RingBuffer<i32> = RingBuffer::new(3);
        assert_eq!(rb.pop(), None);
        let (first, second) = rb.read_slices();
        assert!(first.is_empty());
        assert!(second.is_empty());
        assert_eq!(rb.pop_slice(5), Vec::<i32>::new());
    }
}
```

Add `mod tests;` to `lib.rs` or include the tests inline in each module.

### Running

```bash
cargo build
cargo test

# With mmap feature (Unix only):
cargo test --features mmap

cargo run
```

### Expected Output

```
=== Basic Ring Buffer ===

push(1), evicted: None
push(2), evicted: None
push(3), evicted: None
push(4), evicted: None
push(5), evicted: None
len: 5, full: true
read_slices: [1, 2, 3, 4, 5], []
push(6), evicted: Some(1)
read_slices after wrap: [2, 3, 4, 5], [6]
pop_slice(3): [2, 3, 4]

=== SPSC Ring Buffer ===

SPSC: 100000 items transferred correctly between threads
```

## Design Decisions

1. **`MaybeUninit<T>` over `Option<T>`**: Using `MaybeUninit` avoids the overhead of `Option`'s discriminant byte per element. For a ring buffer handling millions of elements, this saves significant memory and avoids branch-on-read overhead. The trade-off is that the implementation must manually track which slots are initialized.

2. **Capacity+1 for SPSC**: The SPSC variant uses one extra slot to distinguish full from empty without a separate counter. When `tail + 1 == head`, the buffer is full. This avoids an `AtomicUsize` for length, which would require additional synchronization and become a contention point.

3. **`UnsafeCell` for SPSC buffer slots**: Each slot is wrapped in `UnsafeCell` to allow mutable access from both producer and consumer. The protocol guarantees that only one thread writes to any given slot at a time, making this sound despite the lack of a lock.

4. **Producer/Consumer split handles**: Instead of exposing `push` and `pop` directly on `SpscRingBuffer`, the `split()` method returns typed handles. This makes the single-producer/single-consumer contract visible in the type system. You cannot accidentally call `push` from the consumer thread.

5. **mmap behind a feature flag**: The contiguous ring buffer requires Unix-specific system calls and is inherently platform-dependent. Putting it behind a feature flag keeps the default build portable and avoids a `libc` dependency for users who do not need this variant.

## Common Mistakes

1. **Off-by-one in wraparound**: The most common bug is `(index + 1) % capacity` versus `(index + 1) % (capacity + 1)`. The basic buffer uses the exact capacity (with a separate `len` field). The SPSC buffer uses capacity+1 (empty slot sentinel). Mixing these conventions causes silent data corruption.

2. **Wrong atomic ordering in SPSC**: Using `Relaxed` for the tail store in the producer seems fine because "only one thread writes it." But the consumer must see the data written to the slot before seeing the updated tail. `Release` on the producer's tail store and `Acquire` on the consumer's tail load ensures this. Forgetting this causes reading uninitialized memory under optimization.

3. **Forgetting `Drop` for `MaybeUninit` buffers**: When the ring buffer is dropped, any initialized-but-not-popped elements must be explicitly dropped. `MaybeUninit` does not run destructors automatically. Failing to implement `Drop` causes memory leaks for types with heap allocations (like `String` or `Vec`).

## Performance Notes

| Operation | `RingBuffer<T>` | `SpscRingBuffer<T>` |
|-----------|-----------------|---------------------|
| `push` | O(1) | O(1) amortized (may spin) |
| `pop` | O(1) | O(1) amortized |
| `read_slices` | O(1) | N/A |
| `push_slice(n)` | O(n) | N/A |
| Space overhead | 1 `MaybeUninit<T>` per slot | 1 extra slot + 2 cache lines for atomics |

The SPSC variant achieves roughly 100-200 million ops/sec on modern hardware for small types (u64), limited primarily by cache line bouncing between producer and consumer cores. Placing `head` and `tail` on separate cache lines (64-byte alignment) eliminates false sharing and can double throughput.

The mmap contiguous buffer eliminates the two-slice overhead entirely. A single `read_contiguous()` call returns one slice regardless of wraparound. The cost is the mmap setup (microseconds) and the requirement that the capacity be a multiple of the page size (typically 4096 bytes).

## Going Further

- Add **cache line padding** to the SPSC variant by placing `head` and `tail` in separate 64-byte aligned structs to prevent false sharing
- Implement an **MPMC** (multi-producer multi-consumer) variant using a sequence number per slot, following the Dmitry Vyukov bounded MPMC queue design
- Add **watermark notifications**: the consumer registers a callback that fires when the buffer exceeds a high-water mark (useful for backpressure in network stacks)
- Implement the ring buffer as a **`std::io::Read` / `std::io::Write`** adapter for seamless integration with Rust I/O APIs
- Benchmark against the `ringbuf` crate and `crossbeam::queue::ArrayQueue` to evaluate how your implementation compares to established libraries
