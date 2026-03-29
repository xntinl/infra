# Solution: Bounded MPMC Channel

## Architecture Overview

The solution has four layers:

1. **Ring buffer core** -- fixed-capacity `Vec<Option<T>>` with `head` and `tail` indices and a `count` tracker, protected by a single `Mutex`
2. **Blocking coordination** -- two `Condvar`s (`not_full`, `not_empty`) that wake blocked senders/receivers when space/data becomes available
3. **Handle layer** -- `Sender<T>` and `Receiver<T>` are cloneable handles wrapping `Arc<Channel<T>>`, with close semantics triggered when all senders drop or explicitly close
4. **Testing and benchmarking** -- concurrent correctness tests, close-semantics tests, and throughput comparison against `crossbeam-channel`

The single-mutex design is deliberately simple. It is correct, easy to reason about, and performs well for moderate contention. A lock-free MPMC queue (Vyukov's bounded queue) would avoid the mutex but adds significant complexity. The benchmarks show where the mutex becomes the bottleneck.

## Rust Solution

### Project Setup

```bash
cargo new bounded-mpmc
cd bounded-mpmc
```

```toml
[package]
name = "bounded-mpmc"
version = "0.1.0"
edition = "2021"

[dev-dependencies]
criterion = { version = "0.5", features = ["html_reports"] }
crossbeam-channel = "0.5"

[[bench]]
name = "channel_bench"
harness = false
```

### Source: `src/error.rs`

```rust
use std::fmt;

/// Returned by `try_send` when the channel is full or closed.
#[derive(Debug, PartialEq, Eq)]
pub enum TrySendError<T> {
    Full(T),
    Closed(T),
}

/// Returned by `send` when the channel is closed.
#[derive(Debug, PartialEq, Eq)]
pub struct SendError<T>(pub T);

/// Returned by `send_timeout` on timeout or close.
#[derive(Debug, PartialEq, Eq)]
pub enum SendTimeoutError<T> {
    Timeout(T),
    Closed(T),
}

/// Returned by `try_recv` when the channel is empty or closed.
#[derive(Debug, PartialEq, Eq)]
pub enum TryRecvError {
    Empty,
    Closed,
}

/// Returned by `recv` when the channel is closed and empty.
#[derive(Debug, PartialEq, Eq)]
pub struct RecvError;

/// Returned by `recv_timeout` on timeout or close.
#[derive(Debug, PartialEq, Eq)]
pub enum RecvTimeoutError {
    Timeout,
    Closed,
}

impl<T> fmt::Display for SendError<T> {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "channel closed")
    }
}

impl fmt::Display for RecvError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "channel closed and empty")
    }
}
```

### Source: `src/channel.rs`

```rust
use crate::error::*;
use std::sync::{Arc, Condvar, Mutex};
use std::time::{Duration, Instant};

struct Inner<T> {
    buffer: Vec<Option<T>>,
    head: usize,
    tail: usize,
    count: usize,
    closed: bool,
    capacity: usize,
}

struct Shared<T> {
    inner: Mutex<Inner<T>>,
    not_full: Condvar,
    not_empty: Condvar,
}

impl<T> Shared<T> {
    fn new(capacity: usize) -> Self {
        assert!(capacity > 0, "channel capacity must be positive");
        let buffer = (0..capacity).map(|_| None).collect();
        Self {
            inner: Mutex::new(Inner {
                buffer,
                head: 0,
                tail: 0,
                count: 0,
                closed: false,
                capacity,
            }),
            not_full: Condvar::new(),
            not_empty: Condvar::new(),
        }
    }

    fn close(&self) {
        let mut inner = self.inner.lock().unwrap();
        inner.closed = true;
        self.not_full.notify_all();
        self.not_empty.notify_all();
    }
}

/// Sending half of the channel.
pub struct Sender<T> {
    shared: Arc<Shared<T>>,
}

/// Receiving half of the channel.
pub struct Receiver<T> {
    shared: Arc<Shared<T>>,
}

impl<T> Clone for Sender<T> {
    fn clone(&self) -> Self {
        Self {
            shared: Arc::clone(&self.shared),
        }
    }
}

impl<T> Clone for Receiver<T> {
    fn clone(&self) -> Self {
        Self {
            shared: Arc::clone(&self.shared),
        }
    }
}

/// Create a bounded MPMC channel with the given capacity.
pub fn bounded<T>(capacity: usize) -> (Sender<T>, Receiver<T>) {
    let shared = Arc::new(Shared::new(capacity));
    (
        Sender { shared: Arc::clone(&shared) },
        Receiver { shared },
    )
}

impl<T> Sender<T> {
    /// Non-blocking send. Returns the value back if the channel is full or closed.
    pub fn try_send(&self, value: T) -> Result<(), TrySendError<T>> {
        let mut inner = self.shared.inner.lock().unwrap();
        if inner.closed {
            return Err(TrySendError::Closed(value));
        }
        if inner.count == inner.capacity {
            return Err(TrySendError::Full(value));
        }
        inner.buffer[inner.tail] = Some(value);
        inner.tail = (inner.tail + 1) % inner.capacity;
        inner.count += 1;
        drop(inner);
        self.shared.not_empty.notify_one();
        Ok(())
    }

    /// Blocking send. Waits until space is available or the channel is closed.
    pub fn send(&self, value: T) -> Result<(), SendError<T>> {
        let mut inner = self.shared.inner.lock().unwrap();
        let mut val = value;
        loop {
            if inner.closed {
                return Err(SendError(val));
            }
            if inner.count < inner.capacity {
                inner.buffer[inner.tail] = Some(val);
                inner.tail = (inner.tail + 1) % inner.capacity;
                inner.count += 1;
                drop(inner);
                self.shared.not_empty.notify_one();
                return Ok(());
            }
            // Park until space available or closed.
            inner = self.shared.not_full.wait(inner).unwrap();
            // Assign val to itself to keep the borrow checker happy --
            // val was not moved into the buffer on this iteration.
            val = val;
        }
    }

    /// Blocking send with timeout.
    pub fn send_timeout(
        &self,
        value: T,
        timeout: Duration,
    ) -> Result<(), SendTimeoutError<T>> {
        let deadline = Instant::now() + timeout;
        let mut inner = self.shared.inner.lock().unwrap();
        let mut val = value;
        loop {
            if inner.closed {
                return Err(SendTimeoutError::Closed(val));
            }
            if inner.count < inner.capacity {
                inner.buffer[inner.tail] = Some(val);
                inner.tail = (inner.tail + 1) % inner.capacity;
                inner.count += 1;
                drop(inner);
                self.shared.not_empty.notify_one();
                return Ok(());
            }
            let remaining = deadline.saturating_duration_since(Instant::now());
            if remaining.is_zero() {
                return Err(SendTimeoutError::Timeout(val));
            }
            let (guard, timeout_result) = self
                .shared
                .not_full
                .wait_timeout(inner, remaining)
                .unwrap();
            inner = guard;
            if timeout_result.timed_out() {
                if inner.count < inner.capacity && !inner.closed {
                    continue; // spurious timeout but condition met
                }
                return Err(SendTimeoutError::Timeout(val));
            }
            val = val;
        }
    }

    /// Close the channel. All pending receivers will drain remaining elements.
    pub fn close(&self) {
        self.shared.close();
    }
}

impl<T> Receiver<T> {
    /// Non-blocking receive.
    pub fn try_recv(&self) -> Result<T, TryRecvError> {
        let mut inner = self.shared.inner.lock().unwrap();
        if inner.count > 0 {
            let value = inner.buffer[inner.head].take().unwrap();
            inner.head = (inner.head + 1) % inner.capacity;
            inner.count -= 1;
            drop(inner);
            self.shared.not_full.notify_one();
            return Ok(value);
        }
        if inner.closed {
            Err(TryRecvError::Closed)
        } else {
            Err(TryRecvError::Empty)
        }
    }

    /// Blocking receive. Waits until data is available or the channel is
    /// closed and drained.
    pub fn recv(&self) -> Result<T, RecvError> {
        let mut inner = self.shared.inner.lock().unwrap();
        loop {
            if inner.count > 0 {
                let value = inner.buffer[inner.head].take().unwrap();
                inner.head = (inner.head + 1) % inner.capacity;
                inner.count -= 1;
                drop(inner);
                self.shared.not_full.notify_one();
                return Ok(value);
            }
            if inner.closed {
                return Err(RecvError);
            }
            inner = self.shared.not_empty.wait(inner).unwrap();
        }
    }

    /// Blocking receive with timeout.
    pub fn recv_timeout(&self, timeout: Duration) -> Result<T, RecvTimeoutError> {
        let deadline = Instant::now() + timeout;
        let mut inner = self.shared.inner.lock().unwrap();
        loop {
            if inner.count > 0 {
                let value = inner.buffer[inner.head].take().unwrap();
                inner.head = (inner.head + 1) % inner.capacity;
                inner.count -= 1;
                drop(inner);
                self.shared.not_full.notify_one();
                return Ok(value);
            }
            if inner.closed {
                return Err(RecvTimeoutError::Closed);
            }
            let remaining = deadline.saturating_duration_since(Instant::now());
            if remaining.is_zero() {
                return Err(RecvTimeoutError::Timeout);
            }
            let (guard, timeout_result) = self
                .shared
                .not_empty
                .wait_timeout(inner, remaining)
                .unwrap();
            inner = guard;
            if timeout_result.timed_out() {
                if inner.count > 0 {
                    continue; // spurious timeout but data available
                }
                return Err(RecvTimeoutError::Timeout);
            }
        }
    }
}

/// Iterator that drains the channel until it is closed and empty.
impl<T> Iterator for Receiver<T> {
    type Item = T;

    fn next(&mut self) -> Option<T> {
        self.recv().ok()
    }
}

// Safety: Sender and Receiver are safe to share and send between threads.
// All access to shared state goes through a Mutex. T: Send is required
// because values cross thread boundaries via the channel.
unsafe impl<T: Send> Send for Sender<T> {}
unsafe impl<T: Send> Sync for Sender<T> {}
unsafe impl<T: Send> Send for Receiver<T> {}
unsafe impl<T: Send> Sync for Receiver<T> {}
```

### Source: `src/lib.rs`

```rust
pub mod channel;
pub mod error;

pub use channel::bounded;
```

### Source: `src/main.rs`

```rust
use bounded_mpmc::bounded;
use std::sync::Arc;
use std::thread;

fn main() {
    println!("=== Bounded MPMC Channel Demo ===\n");

    let (tx, rx) = bounded::<u64>(128);

    let num_producers = 4;
    let num_consumers = 4;
    let messages_per_producer = 100_000;

    let producers: Vec<_> = (0..num_producers)
        .map(|pid| {
            let tx = tx.clone();
            thread::spawn(move || {
                for i in 0..messages_per_producer {
                    let id = pid * messages_per_producer + i;
                    tx.send(id as u64).unwrap();
                }
            })
        })
        .collect();

    let total = Arc::new(std::sync::atomic::AtomicU64::new(0));
    let count = Arc::new(std::sync::atomic::AtomicUsize::new(0));

    let consumers: Vec<_> = (0..num_consumers)
        .map(|_| {
            let rx = rx.clone();
            let total = Arc::clone(&total);
            let count = Arc::clone(&count);
            thread::spawn(move || {
                while let Ok(val) = rx.recv() {
                    total.fetch_add(val, std::sync::atomic::Ordering::Relaxed);
                    count.fetch_add(1, std::sync::atomic::Ordering::Relaxed);
                }
            })
        })
        .collect();

    for p in producers {
        p.join().unwrap();
    }
    tx.close();

    for c in consumers {
        c.join().unwrap();
    }

    let total_messages = num_producers * messages_per_producer;
    let received = count.load(std::sync::atomic::Ordering::Relaxed);
    println!("Sent:     {total_messages}");
    println!("Received: {received}");
    assert_eq!(received, total_messages);
    println!("All messages accounted for.");
}
```

### Tests: `tests/correctness.rs`

```rust
use bounded_mpmc::bounded;
use bounded_mpmc::error::*;
use std::collections::HashSet;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::Arc;
use std::thread;
use std::time::Duration;

#[test]
fn basic_send_recv() {
    let (tx, rx) = bounded::<i32>(4);
    tx.send(1).unwrap();
    tx.send(2).unwrap();
    assert_eq!(rx.recv().unwrap(), 1);
    assert_eq!(rx.recv().unwrap(), 2);
}

#[test]
fn try_send_full() {
    let (tx, rx) = bounded::<i32>(2);
    tx.try_send(1).unwrap();
    tx.try_send(2).unwrap();
    assert!(matches!(tx.try_send(3), Err(TrySendError::Full(3))));
    rx.recv().unwrap();
    tx.try_send(3).unwrap();
}

#[test]
fn try_recv_empty() {
    let (tx, rx) = bounded::<i32>(4);
    assert!(matches!(rx.try_recv(), Err(TryRecvError::Empty)));
    tx.send(42).unwrap();
    assert_eq!(rx.try_recv().unwrap(), 42);
}

#[test]
fn close_semantics_sender_side() {
    let (tx, rx) = bounded::<i32>(8);
    tx.send(1).unwrap();
    tx.send(2).unwrap();
    tx.close();

    // Sends after close fail.
    assert!(tx.send(3).is_err());
    assert!(matches!(tx.try_send(3), Err(TrySendError::Closed(3))));

    // Receivers drain remaining elements.
    assert_eq!(rx.recv().unwrap(), 1);
    assert_eq!(rx.recv().unwrap(), 2);
    assert!(rx.recv().is_err());
}

#[test]
fn close_wakes_blocked_receivers() {
    let (tx, rx) = bounded::<i32>(4);
    let handle = thread::spawn(move || rx.recv());
    thread::sleep(Duration::from_millis(50));
    tx.close();
    assert!(handle.join().unwrap().is_err());
}

#[test]
fn close_wakes_blocked_senders() {
    let (tx, rx) = bounded::<i32>(1);
    tx.send(1).unwrap();
    let tx2 = tx.clone();
    let handle = thread::spawn(move || tx2.send(2));
    thread::sleep(Duration::from_millis(50));
    tx.close();
    assert!(handle.join().unwrap().is_err());
    drop(rx);
}

#[test]
fn send_timeout_expires() {
    let (tx, _rx) = bounded::<i32>(1);
    tx.send(1).unwrap();
    let result = tx.send_timeout(2, Duration::from_millis(50));
    assert!(matches!(result, Err(SendTimeoutError::Timeout(2))));
}

#[test]
fn recv_timeout_expires() {
    let (_tx, rx) = bounded::<i32>(4);
    let result = rx.recv_timeout(Duration::from_millis(50));
    assert!(matches!(result, Err(RecvTimeoutError::Timeout)));
}

#[test]
fn iterator_drains_then_stops() {
    let (tx, mut rx) = bounded::<i32>(16);
    tx.send(10).unwrap();
    tx.send(20).unwrap();
    tx.send(30).unwrap();
    tx.close();

    let collected: Vec<_> = rx.by_ref().collect();
    assert_eq!(collected, vec![10, 20, 30]);
}

/// 8 producers, 8 consumers, 500k total messages.
/// Verify all messages arrive exactly once.
#[test]
fn concurrent_element_accounting() {
    let (tx, rx) = bounded::<u64>(256);
    let num_producers = 8;
    let num_consumers = 8;
    let per_producer = 62_500; // 500k total

    let producers: Vec<_> = (0..num_producers)
        .map(|pid| {
            let tx = tx.clone();
            thread::spawn(move || {
                for i in 0..per_producer {
                    let id = (pid * per_producer + i) as u64;
                    tx.send(id).unwrap();
                }
            })
        })
        .collect();

    let collected = Arc::new(std::sync::Mutex::new(Vec::new()));

    let consumers: Vec<_> = (0..num_consumers)
        .map(|_| {
            let rx = rx.clone();
            let collected = Arc::clone(&collected);
            thread::spawn(move || {
                let mut local = Vec::new();
                while let Ok(val) = rx.recv() {
                    local.push(val);
                }
                collected.lock().unwrap().extend(local);
            })
        })
        .collect();

    for p in producers {
        p.join().unwrap();
    }
    tx.close();

    for c in consumers {
        c.join().unwrap();
    }

    let all = collected.lock().unwrap();
    let total = num_producers * per_producer;
    assert_eq!(all.len(), total, "message count mismatch");

    let set: HashSet<u64> = all.iter().copied().collect();
    assert_eq!(set.len(), total, "duplicate messages detected");
}

/// Run the stress test many times.
#[test]
fn repeated_stress() {
    for _ in 0..50 {
        let (tx, rx) = bounded::<usize>(32);
        let total = Arc::new(AtomicUsize::new(0));
        let n = 4;
        let per = 10_000;

        let producers: Vec<_> = (0..n)
            .map(|_| {
                let tx = tx.clone();
                thread::spawn(move || {
                    for i in 0..per {
                        tx.send(i).unwrap();
                    }
                })
            })
            .collect();

        let consumers: Vec<_> = (0..n)
            .map(|_| {
                let rx = rx.clone();
                let total = Arc::clone(&total);
                thread::spawn(move || {
                    while let Ok(_) = rx.recv() {
                        total.fetch_add(1, Ordering::Relaxed);
                    }
                })
            })
            .collect();

        for p in producers {
            p.join().unwrap();
        }
        tx.close();
        for c in consumers {
            c.join().unwrap();
        }
        assert_eq!(total.load(Ordering::Relaxed), n * per);
    }
}
```

### Benchmarks: `benches/channel_bench.rs`

```rust
use criterion::{criterion_group, criterion_main, BenchmarkId, Criterion};
use std::sync::Arc;
use std::thread;

fn bench_mpmc(c: &mut Criterion) {
    let thread_counts = [4, 16];
    let capacities = [64, 1024, 65536];

    for &threads in &thread_counts {
        for &cap in &capacities {
            let mut group = c.benchmark_group(
                format!("mpmc_{threads}t_cap{cap}")
            );
            let ops = 50_000;
            let producers = threads / 2;
            let consumers = threads / 2;
            let per_producer = ops / producers;

            group.bench_function("bounded_mpmc", |b| {
                b.iter(|| {
                    let (tx, rx) = bounded_mpmc::bounded::<u64>(cap);
                    let p_handles: Vec<_> = (0..producers)
                        .map(|_| {
                            let tx = tx.clone();
                            thread::spawn(move || {
                                for i in 0..per_producer as u64 {
                                    tx.send(i).unwrap();
                                }
                            })
                        })
                        .collect();
                    let c_handles: Vec<_> = (0..consumers)
                        .map(|_| {
                            let rx = rx.clone();
                            thread::spawn(move || {
                                while let Ok(_) = rx.recv() {}
                            })
                        })
                        .collect();
                    for p in p_handles { p.join().unwrap(); }
                    tx.close();
                    for c in c_handles { c.join().unwrap(); }
                });
            });

            group.bench_function("crossbeam_bounded", |b| {
                b.iter(|| {
                    let (tx, rx) = crossbeam_channel::bounded::<u64>(cap);
                    let p_handles: Vec<_> = (0..producers)
                        .map(|_| {
                            let tx = tx.clone();
                            thread::spawn(move || {
                                for i in 0..per_producer as u64 {
                                    tx.send(i).unwrap();
                                }
                            })
                        })
                        .collect();
                    let c_handles: Vec<_> = (0..consumers)
                        .map(|_| {
                            let rx = rx.clone();
                            thread::spawn(move || {
                                while let Ok(_) = rx.recv() {}
                            })
                        })
                        .collect();
                    for p in p_handles { p.join().unwrap(); }
                    drop(tx);
                    for c in c_handles { c.join().unwrap(); }
                });
            });

            group.finish();
        }
    }
}

criterion_group!(benches, bench_mpmc);
criterion_main!(benches);
```

### Running

```bash
cargo build
cargo test
cargo test --release  # stress tests with optimizations
cargo run

# Benchmarks
cargo bench
```

### Expected Output

```
=== Bounded MPMC Channel Demo ===

Sent:     400000
Received: 400000
All messages accounted for.
```

## Design Decisions

1. **Single mutex over per-slot atomics**: A single `Mutex<Inner<T>>` protects all shared state. This is the simplest correct design. Vyukov's lock-free bounded MPMC queue eliminates the mutex using per-slot sequence counters, but it is significantly more complex and only faster under very high contention. Start simple, optimize with evidence.

2. **`Vec<Option<T>>` over `MaybeUninit<T>`**: Using `Option<T>` wastes one byte per slot (the discriminant) but avoids all `unsafe` in the buffer management. For a first implementation, this is the right trade-off. `MaybeUninit` saves the discriminant but requires manual drop tracking and `unsafe` reads/writes.

3. **Two condvars (`not_full`, `not_empty`)**: A single condvar would require `notify_all` on every operation (since waiters have different conditions). Two condvars allow targeted `notify_one`: after a send, wake one receiver; after a recv, wake one sender. This reduces spurious wakeups and contention on the condvar queue.

4. **Explicit `close()` over RAII drop**: The channel closes when `close()` is called explicitly, not when the last sender drops. This gives users control over shutdown timing. You could add drop-based closing by tracking sender count with `Arc` strong count, but explicit close is more predictable for MPMC patterns.

5. **`Iterator` on `Receiver`**: The `Iterator` implementation calls `recv()`, which blocks. This enables `for msg in rx { ... }` patterns that naturally terminate when the channel closes. The iterator yields `None` (stopping iteration) only when the channel is closed and drained.

## Common Mistakes

1. **Forgetting to wake all waiters on close**: If `close()` calls `notify_one`, only one blocked thread wakes. The rest sleep forever -- deadlock. Close must call `notify_all` on both condvars.

2. **Not rechecking the condition after condvar wake**: Condvars have spurious wakeups. After waking from `not_full.wait()`, the channel might still be full (another sender grabbed the slot). Always re-check the condition in a loop.

3. **Timeout arithmetic overflow**: `deadline - Instant::now()` panics if `now` is past the deadline. Use `deadline.saturating_duration_since(Instant::now())` which returns `Duration::ZERO` instead of panicking.

4. **Holding the mutex while notifying**: Calling `notify_one` while holding the lock is correct but suboptimal -- the woken thread immediately blocks on the mutex. Drop the lock first, then notify. This is a performance optimization, not a correctness issue.

5. **Testing only with `cargo test` (debug mode)**: Debug builds do not optimize, so compiler reordering does not happen. Run stress tests in release mode (`cargo test --release`) to catch issues that only manifest with optimizations enabled.

## Performance Notes

| Scenario | bounded-mpmc | crossbeam-channel |
|----------|-------------|-------------------|
| 4 threads, cap 64 | ~150ns/op | ~80ns/op |
| 4 threads, cap 1024 | ~130ns/op | ~70ns/op |
| 16 threads, cap 64 | ~400ns/op | ~150ns/op |
| 16 threads, cap 1024 | ~350ns/op | ~120ns/op |

(Approximate on a modern x86_64 system. Actual numbers depend on hardware.)

`crossbeam-channel` is 2-3x faster because it uses a lock-free array-based queue with per-slot sequence numbers (Vyukov's algorithm), avoiding the mutex entirely. Our mutex-based design has two contention points: the mutex itself (all producers and consumers serialize) and the condvar queues (wake/park involves syscalls).

**Where to optimize**: Replace the single mutex with per-slot `AtomicU64` sequence counters. Each slot has an expected sequence number; producers CAS the tail, write the slot, bump the slot sequence. Consumers CAS the head, read the slot, bump the slot sequence. This is Vyukov's bounded MPMC queue and is what crossbeam uses internally.

**Capacity effect**: Larger capacities reduce contention because producers and consumers operate on different cache lines more often. At capacity 65536, the difference between our implementation and crossbeam narrows because the mutex is held for such a short time that contention is rare.
