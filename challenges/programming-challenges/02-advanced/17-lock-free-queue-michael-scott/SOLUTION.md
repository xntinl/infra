# Solution: Lock-Free Queue (Michael-Scott Algorithm)

## Architecture Overview

The solution is organized into five layers:

1. **Node structure** -- `Node<T>` with an atomic `next` pointer and an `Option<T>` value (None for the sentinel)
2. **Core queue** -- `MSQueue<T>` with atomic `head` and `tail` pointers, implementing enqueue/dequeue with the helping mechanism
3. **Epoch-based reclamation** -- Using `crossbeam-epoch` for safe deferred deallocation of dequeued nodes
4. **Baseline implementations** -- `MutexQueue<T>` for benchmark comparison
5. **Testing** -- Linearizability validation, stress tests, and element accounting across multiple threads

The Michael-Scott queue is more complex than Treiber's stack because it manages two atomic pointers (head and tail) that must remain consistent. The sentinel node eliminates the empty-queue special case, and the helping mechanism ensures progress even when a thread stalls between appending a node and advancing the tail.

## Rust Solution

### Project Setup

```bash
cargo new ms-queue
cd ms-queue
```

```toml
[package]
name = "ms-queue"
version = "0.1.0"
edition = "2021"

[dependencies]
crossbeam-epoch = "0.9"

[dev-dependencies]
criterion = { version = "0.5", features = ["html_reports"] }
rand = "0.8"
crossbeam-queue = "0.3"

[[bench]]
name = "queue_bench"
harness = false
```

### Source: `src/node.rs`

```rust
use crossbeam_epoch::Atomic;
use std::sync::atomic::Ordering;

/// A node in the Michael-Scott queue.
///
/// The sentinel node has `value: None`. Data nodes have `value: Some(T)`.
/// The `next` pointer is atomic to support lock-free CAS operations.
pub struct Node<T> {
    pub value: Option<T>,
    pub next: Atomic<Node<T>>,
}

impl<T> Node<T> {
    /// Create a sentinel node (no value).
    pub fn sentinel() -> Self {
        Self {
            value: None,
            next: Atomic::null(),
        }
    }

    /// Create a data node with a value.
    pub fn new(value: T) -> Self {
        Self {
            value: Some(value),
            next: Atomic::null(),
        }
    }
}
```

### Source: `src/queue.rs`

```rust
use crate::node::Node;
use crossbeam_epoch::{self as epoch, Atomic, Owned, Shared};
use std::sync::atomic::Ordering;

/// A lock-free FIFO queue implementing the Michael-Scott algorithm.
///
/// The queue maintains two atomic pointers:
/// - `head`: points to the sentinel node. The actual first element is head.next.
/// - `tail`: points to the last node (or a node close to the last).
///
/// The sentinel ensures that head and tail are never null, simplifying the
/// CAS operations for both enqueue and dequeue.
pub struct MSQueue<T> {
    head: Atomic<Node<T>>,
    tail: Atomic<Node<T>>,
}

impl<T> MSQueue<T> {
    /// Create a new empty queue with a sentinel node.
    pub fn new() -> Self {
        let sentinel = Owned::new(Node::sentinel());
        let guard = epoch::pin();
        let sentinel = sentinel.into_shared(&guard);

        Self {
            head: Atomic::from(sentinel),
            tail: Atomic::from(sentinel),
        }
    }

    /// Enqueue a value at the tail of the queue.
    ///
    /// Algorithm:
    /// 1. Create a new node.
    /// 2. Read the current tail and tail.next.
    /// 3. If tail.next is null, try to CAS tail.next from null to the new node.
    ///    - On success, try to advance tail to the new node (this CAS may fail
    ///      if another thread helps -- that is fine).
    ///    - On failure, another enqueuer won. Retry from step 2.
    /// 4. If tail.next is NOT null, tail is lagging. Help by CAS-ing tail
    ///    forward to tail.next, then retry.
    ///
    /// Memory ordering:
    /// - Load tail with Acquire: must see the node's next pointer.
    /// - Load tail.next with Acquire: must see whether it is null or points to a node.
    /// - CAS tail.next with Release: publishes the new node.
    /// - CAS tail with Release: publishes the new tail position.
    pub fn enqueue(&self, value: T) {
        let guard = epoch::pin();
        let new_node = Owned::new(Node::new(value));
        let new_shared = new_node.into_shared(&guard);

        loop {
            let tail = self.tail.load(Ordering::Acquire, &guard);
            // Safety: tail is never null (sentinel always exists) and is protected
            // by the epoch guard.
            let tail_ref = unsafe { tail.deref() };
            let next = tail_ref.next.load(Ordering::Acquire, &guard);

            // Re-read tail to check consistency. If tail changed, start over.
            let current_tail = self.tail.load(Ordering::Acquire, &guard);
            if tail != current_tail {
                continue;
            }

            if next.is_null() {
                // Tail.next is null: try to link our new node.
                match tail_ref.next.compare_exchange(
                    Shared::null(),
                    new_shared,
                    Ordering::Release,
                    Ordering::Relaxed,
                    &guard,
                ) {
                    Ok(_) => {
                        // Successfully linked. Now try to advance tail.
                        // If this CAS fails, another thread (or a future enqueue's
                        // helping mechanism) will advance it.
                        let _ = self.tail.compare_exchange(
                            tail,
                            new_shared,
                            Ordering::Release,
                            Ordering::Relaxed,
                            &guard,
                        );
                        return;
                    }
                    Err(_) => {
                        // Another enqueuer linked their node first. Retry.
                        continue;
                    }
                }
            } else {
                // Tail is lagging: help advance it.
                let _ = self.tail.compare_exchange(
                    tail,
                    next,
                    Ordering::Release,
                    Ordering::Relaxed,
                    &guard,
                );
                // Retry with the updated tail.
            }
        }
    }

    /// Dequeue a value from the head of the queue.
    ///
    /// Algorithm:
    /// 1. Read head, tail, and head.next.
    /// 2. If head.next is null, the queue is empty.
    /// 3. If head == tail and tail.next is not null, tail is lagging. Help advance it.
    /// 4. Otherwise, read the value from head.next, try to CAS head from the
    ///    current sentinel to head.next (which becomes the new sentinel).
    ///    - On success, defer deallocation of the old sentinel.
    ///    - On failure, retry.
    ///
    /// Memory ordering:
    /// - Load head with Acquire: must see the sentinel and its next pointer.
    /// - Load head.next with Acquire: must see the value stored in the next node.
    /// - CAS head with Release: publishes the new sentinel.
    pub fn dequeue(&self) -> Option<T> {
        let guard = epoch::pin();

        loop {
            let head = self.head.load(Ordering::Acquire, &guard);
            let tail = self.tail.load(Ordering::Acquire, &guard);
            // Safety: head is never null.
            let head_ref = unsafe { head.deref() };
            let next = head_ref.next.load(Ordering::Acquire, &guard);

            // Consistency check: if head changed, start over.
            let current_head = self.head.load(Ordering::Acquire, &guard);
            if head != current_head {
                continue;
            }

            if next.is_null() {
                // Queue is empty: head is the sentinel and has no next.
                return None;
            }

            if head == tail {
                // Head and tail point to the same node, but next is not null.
                // This means tail is lagging. Help advance it.
                let _ = self.tail.compare_exchange(
                    tail,
                    next,
                    Ordering::Release,
                    Ordering::Relaxed,
                    &guard,
                );
                continue;
            }

            // Read the value from the next node before CAS-ing head forward.
            // Safety: next is not null (checked above) and protected by the guard.
            let next_ref = unsafe { next.deref() };
            let value = unsafe { std::ptr::read(&next_ref.value) };

            // Try to advance head past the old sentinel to the next node.
            match self.head.compare_exchange(
                head,
                next,
                Ordering::Release,
                Ordering::Relaxed,
                &guard,
            ) {
                Ok(_) => {
                    // Safety: we removed the old sentinel from the queue.
                    // It is no longer reachable. Defer its destruction until
                    // all threads have exited their critical sections.
                    unsafe {
                        guard.defer_destroy(head);
                    }
                    // The value we read is an Option<T>. Data nodes always have Some.
                    return value;
                }
                Err(_) => {
                    // Another dequeuer won. The value we read belongs to a node
                    // that is still in the queue. We must NOT use it.
                    // (We read via ptr::read, so we need to forget it to avoid
                    // a double-free if the successful dequeuer also reads it.)
                    std::mem::forget(value);
                    continue;
                }
            }
        }
    }

    /// Check if the queue appears empty.
    ///
    /// Inherently racy: another thread may enqueue or dequeue between this
    /// check and the caller's next action. Use only for diagnostics.
    pub fn is_empty(&self) -> bool {
        let guard = epoch::pin();
        let head = self.head.load(Ordering::Acquire, &guard);
        let head_ref = unsafe { head.deref() };
        head_ref.next.load(Ordering::Acquire, &guard).is_null()
    }
}

impl<T> Default for MSQueue<T> {
    fn default() -> Self {
        Self::new()
    }
}

impl<T> Drop for MSQueue<T> {
    fn drop(&mut self) {
        // Drain all elements. We have exclusive access.
        while self.dequeue().is_some() {}

        // Drop the remaining sentinel node.
        let guard = epoch::pin();
        let head = self.head.load(Ordering::Relaxed, &guard);
        if !head.is_null() {
            unsafe {
                guard.defer_destroy(head);
            }
        }
    }
}

// Safety: all mutations go through atomic CAS. Epoch reclamation prevents
// use-after-free. T: Send is required because values cross thread boundaries.
unsafe impl<T: Send> Send for MSQueue<T> {}
unsafe impl<T: Send> Sync for MSQueue<T> {}
```

### Source: `src/mutex_queue.rs`

```rust
use std::collections::VecDeque;
use std::sync::Mutex;

/// A simple mutex-protected queue for benchmark comparison.
pub struct MutexQueue<T> {
    inner: Mutex<VecDeque<T>>,
}

impl<T> MutexQueue<T> {
    pub fn new() -> Self {
        Self {
            inner: Mutex::new(VecDeque::new()),
        }
    }

    pub fn enqueue(&self, value: T) {
        self.inner.lock().unwrap().push_back(value);
    }

    pub fn dequeue(&self) -> Option<T> {
        self.inner.lock().unwrap().pop_front()
    }
}

impl<T> Default for MutexQueue<T> {
    fn default() -> Self {
        Self::new()
    }
}
```

### Source: `src/lib.rs`

```rust
pub mod node;
pub mod queue;
pub mod mutex_queue;
```

### Source: `src/main.rs`

```rust
use ms_queue::queue::MSQueue;
use std::sync::Arc;
use std::thread;

fn main() {
    println!("=== Michael-Scott Queue Demo ===\n");

    let queue = Arc::new(MSQueue::new());

    // Sequential demonstration.
    queue.enqueue("first");
    queue.enqueue("second");
    queue.enqueue("third");
    println!("Enqueued: first, second, third");
    println!("Dequeue: {:?}", queue.dequeue());
    println!("Dequeue: {:?}", queue.dequeue());
    println!("Dequeue: {:?}", queue.dequeue());
    println!("Dequeue (empty): {:?}", queue.dequeue());

    // Concurrent demonstration.
    println!("\n--- Concurrent stress test ---");

    let num_producers = 8;
    let num_consumers = 8;
    let items_per_producer = 100_000;
    let total = num_producers * items_per_producer;

    // Producers enqueue sequentially within their range.
    let prod_handles: Vec<_> = (0..num_producers)
        .map(|tid| {
            let q = Arc::clone(&queue);
            thread::spawn(move || {
                let base = tid * items_per_producer;
                for i in 0..items_per_producer {
                    q.enqueue(base + i);
                }
            })
        })
        .collect();

    for h in prod_handles {
        h.join().unwrap();
    }
    println!("Produced {total} elements from {num_producers} threads");

    // Consumers dequeue.
    let collected = Arc::new(std::sync::Mutex::new(Vec::with_capacity(total)));

    let cons_handles: Vec<_> = (0..num_consumers)
        .map(|_| {
            let q = Arc::clone(&queue);
            let c = Arc::clone(&collected);
            thread::spawn(move || {
                let mut local = Vec::new();
                loop {
                    match q.dequeue() {
                        Some(v) => local.push(v),
                        None => break,
                    }
                }
                c.lock().unwrap().extend(local);
            })
        })
        .collect();

    for h in cons_handles {
        h.join().unwrap();
    }

    let all = collected.lock().unwrap();
    println!("Consumed {} elements from {num_consumers} threads", all.len());
    assert_eq!(all.len(), total);
    println!("All elements accounted for.");

    // Verify FIFO per producer.
    let mut per_producer: Vec<Vec<usize>> = vec![Vec::new(); num_producers];
    for &val in all.iter() {
        let tid = val / items_per_producer;
        per_producer[tid].push(val);
    }
    for (tid, values) in per_producer.iter().enumerate() {
        for window in values.windows(2) {
            assert!(
                window[0] < window[1],
                "FIFO violation for producer {tid}: {} before {}",
                window[0],
                window[1]
            );
        }
    }
    println!("Per-producer FIFO ordering verified.");
}
```

### Tests: `tests/correctness.rs`

```rust
use ms_queue::queue::MSQueue;
use std::collections::HashSet;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::Arc;
use std::thread;

#[test]
fn basic_fifo_order() {
    let queue = MSQueue::new();
    queue.enqueue(1);
    queue.enqueue(2);
    queue.enqueue(3);
    assert_eq!(queue.dequeue(), Some(1));
    assert_eq!(queue.dequeue(), Some(2));
    assert_eq!(queue.dequeue(), Some(3));
    assert_eq!(queue.dequeue(), None);
}

#[test]
fn empty_queue() {
    let queue: MSQueue<i32> = MSQueue::new();
    assert!(queue.is_empty());
    assert_eq!(queue.dequeue(), None);
}

#[test]
fn single_element() {
    let queue = MSQueue::new();
    queue.enqueue(42);
    assert!(!queue.is_empty());
    assert_eq!(queue.dequeue(), Some(42));
    assert!(queue.is_empty());
}

#[test]
fn interleaved_enqueue_dequeue() {
    let queue = MSQueue::new();
    queue.enqueue(1);
    assert_eq!(queue.dequeue(), Some(1));
    queue.enqueue(2);
    queue.enqueue(3);
    assert_eq!(queue.dequeue(), Some(2));
    queue.enqueue(4);
    assert_eq!(queue.dequeue(), Some(3));
    assert_eq!(queue.dequeue(), Some(4));
    assert_eq!(queue.dequeue(), None);
}

/// Element accounting: every enqueued value is dequeued exactly once.
#[test]
fn concurrent_element_accounting() {
    let num_producers = 8;
    let num_consumers = 8;
    let per_producer = 50_000;
    let total = num_producers * per_producer;

    let queue = Arc::new(MSQueue::new());

    // Phase 1: all producers enqueue.
    let prod_handles: Vec<_> = (0..num_producers)
        .map(|tid| {
            let q = Arc::clone(&queue);
            thread::spawn(move || {
                let base = tid * per_producer;
                for i in 0..per_producer {
                    q.enqueue((base + i) as u64);
                }
            })
        })
        .collect();

    for h in prod_handles {
        h.join().unwrap();
    }

    // Phase 2: all consumers dequeue.
    let collected = Arc::new(std::sync::Mutex::new(Vec::with_capacity(total)));

    let cons_handles: Vec<_> = (0..num_consumers)
        .map(|_| {
            let q = Arc::clone(&queue);
            let c = Arc::clone(&collected);
            thread::spawn(move || {
                let mut local = Vec::new();
                loop {
                    match q.dequeue() {
                        Some(v) => local.push(v),
                        None => break,
                    }
                }
                c.lock().unwrap().extend(local);
            })
        })
        .collect();

    for h in cons_handles {
        h.join().unwrap();
    }

    let all = collected.lock().unwrap();
    assert_eq!(all.len(), total, "wrong element count");

    let set: HashSet<u64> = all.iter().copied().collect();
    assert_eq!(set.len(), total, "duplicate elements detected");
}

/// Linearizability test: per-producer FIFO ordering is preserved.
#[test]
fn per_producer_fifo_ordering() {
    let num_producers = 4;
    let per_producer = 100_000;
    let queue = Arc::new(MSQueue::new());

    // Each producer enqueues (tid, sequence_number).
    let prod_handles: Vec<_> = (0..num_producers)
        .map(|tid| {
            let q = Arc::clone(&queue);
            thread::spawn(move || {
                for seq in 0..per_producer {
                    q.enqueue((tid as u64, seq as u64));
                }
            })
        })
        .collect();

    for h in prod_handles {
        h.join().unwrap();
    }

    // Single consumer dequeues everything and checks per-producer order.
    let mut last_seq = vec![0u64; num_producers];
    let mut first = vec![true; num_producers];
    let mut count = 0;

    while let Some((tid, seq)) = queue.dequeue() {
        let tid = tid as usize;
        if first[tid] {
            first[tid] = false;
        } else {
            assert!(
                seq > last_seq[tid],
                "FIFO violation: producer {tid}, expected > {}, got {seq}",
                last_seq[tid]
            );
        }
        last_seq[tid] = seq;
        count += 1;
    }

    assert_eq!(count, num_producers * per_producer);
}

/// Mixed concurrent enqueue/dequeue with accounting.
#[test]
fn concurrent_mixed_operations() {
    let queue = Arc::new(MSQueue::new());
    let enqueue_count = Arc::new(AtomicUsize::new(0));
    let dequeue_count = Arc::new(AtomicUsize::new(0));

    let num_threads = 16;
    let ops = 50_000;

    let handles: Vec<_> = (0..num_threads)
        .map(|tid| {
            let q = Arc::clone(&queue);
            let ec = Arc::clone(&enqueue_count);
            let dc = Arc::clone(&dequeue_count);
            thread::spawn(move || {
                for i in 0..ops {
                    if (tid + i) % 2 == 0 {
                        q.enqueue(i);
                        ec.fetch_add(1, Ordering::Relaxed);
                    } else if q.dequeue().is_some() {
                        dc.fetch_add(1, Ordering::Relaxed);
                    }
                }
            })
        })
        .collect();

    for h in handles {
        h.join().unwrap();
    }

    // Drain remaining.
    let mut remaining = 0;
    while queue.dequeue().is_some() {
        remaining += 1;
    }

    let total_enqueued = enqueue_count.load(Ordering::Relaxed);
    let total_dequeued = dequeue_count.load(Ordering::Relaxed) + remaining;
    assert_eq!(total_enqueued, total_dequeued, "accounting mismatch");
}

/// Repeated stress test to increase race condition detection probability.
#[test]
fn repeated_stress() {
    for _ in 0..30 {
        let queue = Arc::new(MSQueue::new());
        let n = 8;
        let per = 10_000;

        let handles: Vec<_> = (0..n)
            .map(|tid| {
                let q = Arc::clone(&queue);
                thread::spawn(move || {
                    for i in 0..per {
                        q.enqueue(tid * per + i);
                    }
                })
            })
            .collect();

        for h in handles {
            h.join().unwrap();
        }

        let mut count = 0;
        while queue.dequeue().is_some() {
            count += 1;
        }
        assert_eq!(count, n * per);
    }
}

/// Large volume test.
#[test]
fn million_elements() {
    let queue = MSQueue::new();
    let count = 1_000_000;
    for i in 0..count {
        queue.enqueue(i);
    }
    for expected in 0..count {
        assert_eq!(queue.dequeue(), Some(expected));
    }
    assert_eq!(queue.dequeue(), None);
}
```

### Benchmarks: `benches/queue_bench.rs`

```rust
use criterion::{criterion_group, criterion_main, BenchmarkId, Criterion};
use ms_queue::queue::MSQueue;
use ms_queue::mutex_queue::MutexQueue;
use crossbeam_queue::SegQueue;
use std::sync::Arc;
use std::thread;

fn bench_enqueue_dequeue(c: &mut Criterion) {
    let thread_counts = [1, 4, 8, 16];
    let ops_per_thread = 50_000;

    for &threads in &thread_counts {
        let mut group = c.benchmark_group(format!("queue_{threads}_threads"));

        group.bench_function("MSQueue", |b| {
            b.iter(|| {
                let queue = Arc::new(MSQueue::new());
                let handles: Vec<_> = (0..threads)
                    .map(|_| {
                        let q = Arc::clone(&queue);
                        thread::spawn(move || {
                            for i in 0..ops_per_thread {
                                q.enqueue(i);
                            }
                            for _ in 0..ops_per_thread {
                                while q.dequeue().is_none() {}
                            }
                        })
                    })
                    .collect();
                for h in handles { h.join().unwrap(); }
            });
        });

        group.bench_function("SegQueue", |b| {
            b.iter(|| {
                let queue = Arc::new(SegQueue::new());
                let handles: Vec<_> = (0..threads)
                    .map(|_| {
                        let q = Arc::clone(&queue);
                        thread::spawn(move || {
                            for i in 0..ops_per_thread {
                                q.push(i);
                            }
                            for _ in 0..ops_per_thread {
                                while q.pop().is_err() {}
                            }
                        })
                    })
                    .collect();
                for h in handles { h.join().unwrap(); }
            });
        });

        group.bench_function("MutexQueue", |b| {
            b.iter(|| {
                let queue = Arc::new(MutexQueue::new());
                let handles: Vec<_> = (0..threads)
                    .map(|_| {
                        let q = Arc::clone(&queue);
                        thread::spawn(move || {
                            for i in 0..ops_per_thread {
                                q.enqueue(i);
                            }
                            for _ in 0..ops_per_thread {
                                loop {
                                    if q.dequeue().is_some() { break; }
                                }
                            }
                        })
                    })
                    .collect();
                for h in handles { h.join().unwrap(); }
            });
        });

        group.finish();
    }
}

fn bench_producer_consumer(c: &mut Criterion) {
    let mut group = c.benchmark_group("producer_consumer_8P_8C");
    let items = 100_000;

    group.bench_function("MSQueue", |b| {
        b.iter(|| {
            let queue = Arc::new(MSQueue::new());
            let mut handles = Vec::new();

            for _ in 0..8 {
                let q = Arc::clone(&queue);
                handles.push(thread::spawn(move || {
                    for i in 0..items {
                        q.enqueue(i);
                    }
                }));
            }

            for _ in 0..8 {
                let q = Arc::clone(&queue);
                handles.push(thread::spawn(move || {
                    let mut count = 0;
                    while count < items {
                        if q.dequeue().is_some() {
                            count += 1;
                        }
                    }
                }));
            }

            for h in handles { h.join().unwrap(); }
        });
    });

    group.bench_function("SegQueue", |b| {
        b.iter(|| {
            let queue = Arc::new(SegQueue::new());
            let mut handles = Vec::new();

            for _ in 0..8 {
                let q = Arc::clone(&queue);
                handles.push(thread::spawn(move || {
                    for i in 0..items {
                        q.push(i);
                    }
                }));
            }

            for _ in 0..8 {
                let q = Arc::clone(&queue);
                handles.push(thread::spawn(move || {
                    let mut count = 0;
                    while count < items {
                        if q.pop().is_ok() {
                            count += 1;
                        }
                    }
                }));
            }

            for h in handles { h.join().unwrap(); }
        });
    });

    group.finish();
}

criterion_group!(benches, bench_enqueue_dequeue, bench_producer_consumer);
criterion_main!(benches);
```

### Running

```bash
cargo build
cargo test
cargo test --release  # Stress tests should be run optimized
cargo run

# Benchmarks
cargo bench
```

### Expected Output

```
=== Michael-Scott Queue Demo ===

Enqueued: first, second, third
Dequeue: Some("first")
Dequeue: Some("second")
Dequeue: Some("third")
Dequeue (empty): None

--- Concurrent stress test ---
Produced 800000 elements from 8 threads
Consumed 800000 elements from 8 threads
All elements accounted for.
Per-producer FIFO ordering verified.
```

## Design Decisions

1. **Sentinel node always present**: The sentinel simplifies both enqueue and dequeue by eliminating the empty-queue special case. Without it, enqueue into an empty queue requires a CAS on head (which normally only dequeue touches), creating a contention point. With the sentinel, enqueue always operates on tail.next, and dequeue always operates on head. This separation reduces CAS conflicts.

2. **Helping mechanism in enqueue**: When an enqueuer detects that tail.next is not null, it means another thread successfully appended a node but has not yet updated tail. Instead of waiting, the detecting thread advances tail itself. This ensures the algorithm is lock-free (progress guaranteed even if threads stall). Without helping, a stalled thread could prevent all other enqueuers from making progress.

3. **`ptr::read` + `mem::forget` pattern in dequeue**: When the CAS fails, we have already read the value from the node via `ptr::read`. If we do not call `mem::forget`, the value would be dropped when the `Option<T>` goes out of scope -- but the node is still in the queue and the actual winner will also read it, causing a double-free. `mem::forget` prevents the drop. This is one of the subtlest correctness requirements in the algorithm.

4. **Consistency re-reads**: Both enqueue and dequeue re-read head/tail after loading next to detect concurrent modifications. Without this, the algorithm can operate on a node that has already been dequeued and freed (even with epoch protection, the pointer might be dangling relative to the current queue state). This is the standard Michael-Scott consistency check from the original paper.

5. **`crossbeam-epoch` for reclamation**: The same rationale as the Treiber stack solution -- hand-rolling epoch reclamation is error-prone and the bugs are near-impossible to reproduce reliably. `crossbeam-epoch` is the de facto standard in the Rust ecosystem. The solution describes the mechanism (guard pinning, deferred destruction, epoch advancement) so the learner understands it.

## Common Mistakes

1. **Forgetting the helping mechanism**: Without helping, the algorithm is not lock-free. If a thread appends a node (CAS on tail.next succeeds) but stalls before updating tail, all subsequent enqueuers spin indefinitely trying to CAS tail.next on a non-null pointer. The helping mechanism breaks this by allowing any thread to advance tail.

2. **Reading the value after the CAS instead of before**: A common optimization attempt is to defer the `ptr::read` until after the CAS succeeds. This is unsafe because between loading `next` and succeeding the CAS, another dequeuer might succeed, and the node's memory could be reclaimed. Reading before the CAS (while the node is guaranteed to be alive under epoch protection) is the only safe approach.

3. **Dropping the sentinel in `Drop`**: The queue destructor must drain all elements AND then free the remaining sentinel. Forgetting to free the sentinel causes a memory leak of one node. Conversely, trying to free it without proper epoch protection causes undefined behavior.

## Performance Notes

| Scenario | MSQueue | SegQueue | Mutex<VecDeque> |
|----------|---------|----------|-----------------|
| 1 thread | ~80ns/op | ~40ns/op | ~30ns/op |
| 4 threads | ~150ns/op | ~80ns/op | ~200ns/op |
| 8 threads | ~250ns/op | ~120ns/op | ~600ns/op |
| 16 threads | ~400ns/op | ~180ns/op | ~1500ns/op |

(Approximate values on modern x86_64. Actual numbers depend on hardware and workload.)

**Why is SegQueue faster?** `crossbeam::SegQueue` is not a classic Michael-Scott queue. It is a segmented queue that amortizes allocation by operating on arrays (segments) rather than individual nodes. This drastically reduces the per-operation allocation overhead and improves cache locality. The Michael-Scott queue allocates one node per enqueue, which is its primary bottleneck.

**At 1 thread**, `Mutex<VecDeque>` wins because it has no per-operation allocation and no atomic overhead. The VecDeque grows amortized and accesses are simple pointer increments.

**At 8+ threads**, both lock-free implementations pull ahead because the mutex becomes a serialization bottleneck. Under high contention, threads waiting for the mutex are descheduled by the OS, adding microseconds of latency per contention event. Lock-free algorithms retry in userspace without kernel involvement.

**Memory overhead**: Each enqueue allocates a `Node<T>` on the heap (value + atomic pointer + allocator metadata). With `crossbeam-epoch`, deferred nodes accumulate until the next epoch flip. Under sustained high throughput, the deferred garbage can be significant. `SegQueue` amortizes this by batching into segments.

## Going Further

- Implement the queue **without `crossbeam-epoch`** using a hand-rolled hazard pointer scheme (Maged M. Michael, 2004). Compare the implementation complexity and performance against the epoch version
- Build a **bounded variant** (LCRQ -- LCRQ: An Efficient Wait-Free Queue, Morrison & Afek 2013) that limits queue size and provides backpressure semantics
- Implement a **work-stealing deque** (Chase-Lev deque) using similar CAS patterns. This is the foundation of Rayon's parallel iterators and Tokio's work-stealing scheduler
- Add **batch enqueue/dequeue** operations that amortize CAS costs over multiple elements (similar to how SegQueue works internally)
- Profile with `perf stat` to measure CAS retry rates, cache misses, and branch mispredictions under different contention levels. Use this data to explain why throughput degrades non-linearly with thread count
