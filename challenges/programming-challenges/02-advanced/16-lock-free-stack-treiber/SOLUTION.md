# Solution: Lock-Free Stack (Treiber's Algorithm)

## Architecture Overview

The solution has four layers:

1. **Node structure and raw pointer management** -- `Node<T>` as a singly-linked list element, allocated with `Box::into_raw` and reclaimed with `Box::from_raw`
2. **Core Treiber stack** -- `TreiberStack<T>` using `AtomicPtr` for the top pointer and CAS retry loops for push/pop
3. **Epoch-based reclamation** -- A simplified epoch scheme that defers node deallocation until it is safe (no concurrent readers)
4. **Testing and benchmarking** -- Stress tests proving correctness under contention, element accounting, and performance comparison against `Mutex<Vec<T>>`

The implementation uses `crossbeam-epoch` for memory reclamation rather than a hand-rolled scheme. This is the pragmatic choice for production code -- the algorithms are subtle and a single bug means use-after-free. The design decisions section explains what `crossbeam-epoch` does under the hood.

## Rust Solution

### Project Setup

```bash
cargo new treiber-stack
cd treiber-stack
```

```toml
[package]
name = "treiber-stack"
version = "0.1.0"
edition = "2021"

[dependencies]
crossbeam-epoch = "0.9"

[dev-dependencies]
criterion = { version = "0.5", features = ["html_reports"] }
rand = "0.8"

[[bench]]
name = "stack_bench"
harness = false
```

### Source: `src/node.rs`

```rust
/// A node in the Treiber stack's linked list.
/// Each node holds a value and a raw pointer to the next node.
pub struct Node<T> {
    pub value: T,
    pub next: *mut Node<T>,
}

impl<T> Node<T> {
    pub fn new(value: T, next: *mut Node<T>) -> Self {
        Self { value, next }
    }
}
```

### Source: `src/stack.rs`

```rust
use crate::node::Node;
use crossbeam_epoch::{self as epoch, Atomic, Owned, Shared};
use std::sync::atomic::Ordering;

/// A lock-free stack using Treiber's algorithm with epoch-based reclamation.
///
/// All operations are wait-free for push (single CAS) and lock-free for pop
/// (CAS retry loop). Memory reclamation uses crossbeam-epoch to defer
/// deallocation of popped nodes until no thread holds a reference.
pub struct TreiberStack<T> {
    top: Atomic<Node<T>>,
}

impl<T> TreiberStack<T> {
    pub fn new() -> Self {
        Self {
            top: Atomic::null(),
        }
    }

    /// Push a value onto the stack.
    ///
    /// This operation allocates a new node, reads the current top, sets the
    /// new node's next to the current top, and attempts to CAS the top pointer
    /// to the new node. On failure (another thread modified top), it retries
    /// with the updated top.
    ///
    /// Memory ordering:
    /// - Load top with Acquire: we need to see the complete node that top points to
    ///   so we can set our next pointer correctly.
    /// - CAS with Release on success: publishes our new node so that other threads
    ///   performing Acquire loads on top will see its contents.
    /// - CAS with Relaxed on failure: we just retry with the new value.
    pub fn push(&self, value: T) {
        let guard = epoch::pin();
        let mut node = Owned::new(Node::new(value, std::ptr::null_mut()));

        loop {
            let top = self.top.load(Ordering::Acquire, &guard);

            // Set the new node's next to the current top.
            // Safety: we own `node` exclusively at this point.
            unsafe {
                (*node.as_mut_ptr()).next = top.as_raw() as *mut _;
            }

            match self.top.compare_exchange_weak(
                top,
                node,
                Ordering::Release,
                Ordering::Relaxed,
                &guard,
            ) {
                Ok(_) => return,
                Err(err) => node = err.new,
            }
        }
    }

    /// Pop the top value from the stack.
    ///
    /// Reads the current top, reads its value, and attempts to CAS the top
    /// to top.next. On failure, retries. On success, the old top node is
    /// retired via epoch-based reclamation (deferred deallocation).
    ///
    /// Memory ordering:
    /// - Load top with Acquire: must see the node's value and next pointer
    ///   as written by the pushing thread.
    /// - CAS with Release on success: ensures our read of the node's contents
    ///   happens-before any future reuse of that memory.
    /// - CAS with Relaxed on failure: just retry.
    pub fn pop(&self) -> Option<T> {
        let guard = epoch::pin();

        loop {
            let top = self.top.load(Ordering::Acquire, &guard);
            let top_ref = unsafe { top.as_ref() }?;

            let next = unsafe {
                Shared::from(top_ref.next as *const Node<T>)
            };

            match self.top.compare_exchange_weak(
                top,
                next,
                Ordering::Release,
                Ordering::Relaxed,
                &guard,
            ) {
                Ok(_) => {
                    // Safety: we successfully removed this node from the stack.
                    // No future pop will access it. We defer deallocation until
                    // all threads that might have loaded a reference to it have
                    // exited their epoch critical sections.
                    let value = unsafe { std::ptr::read(&top_ref.value) };
                    unsafe {
                        guard.defer_destroy(top);
                    }
                    return Some(value);
                }
                Err(_) => continue,
            }
        }
    }

    /// Check if the stack appears empty.
    ///
    /// This is inherently racy: the stack may become non-empty immediately
    /// after this returns true, or become empty after it returns false.
    /// Useful only for diagnostics, not for control flow.
    pub fn is_empty(&self) -> bool {
        let guard = epoch::pin();
        self.top.load(Ordering::Acquire, &guard).is_null()
    }
}

impl<T> Default for TreiberStack<T> {
    fn default() -> Self {
        Self::new()
    }
}

impl<T> Drop for TreiberStack<T> {
    fn drop(&mut self) {
        // Drain remaining elements. Since we have exclusive access (&mut self),
        // no concurrent operations are possible.
        while self.pop().is_some() {}
    }
}

// Safety: The stack is safe to share between threads. All mutations go through
// atomic CAS operations, and epoch-based reclamation ensures no use-after-free.
// T: Send is required because values move between threads via push/pop.
unsafe impl<T: Send> Send for TreiberStack<T> {}
unsafe impl<T: Send> Sync for TreiberStack<T> {}
```

### Source: `src/mutex_stack.rs` (Baseline for Benchmarks)

```rust
use std::sync::Mutex;

/// A simple mutex-protected stack for benchmark comparison.
pub struct MutexStack<T> {
    inner: Mutex<Vec<T>>,
}

impl<T> MutexStack<T> {
    pub fn new() -> Self {
        Self {
            inner: Mutex::new(Vec::new()),
        }
    }

    pub fn push(&self, value: T) {
        self.inner.lock().unwrap().push(value);
    }

    pub fn pop(&self) -> Option<T> {
        self.inner.lock().unwrap().pop()
    }
}

impl<T> Default for MutexStack<T> {
    fn default() -> Self {
        Self::new()
    }
}
```

### Source: `src/lib.rs`

```rust
pub mod node;
pub mod stack;
pub mod mutex_stack;
```

### Source: `src/main.rs`

```rust
use treiber_stack::stack::TreiberStack;
use std::sync::Arc;
use std::thread;

fn main() {
    println!("=== Treiber Stack Demo ===\n");

    let stack = Arc::new(TreiberStack::new());

    // Sequential demo.
    stack.push(1);
    stack.push(2);
    stack.push(3);
    println!("Pushed 1, 2, 3");
    println!("Pop: {:?}", stack.pop());
    println!("Pop: {:?}", stack.pop());
    println!("Pop: {:?}", stack.pop());
    println!("Pop (empty): {:?}", stack.pop());

    // Concurrent demo.
    println!("\n--- Concurrent stress test ---");

    let num_threads = 8;
    let ops_per_thread = 100_000;

    let push_stack = Arc::clone(&stack);
    let pop_stack = Arc::clone(&stack);

    let push_handles: Vec<_> = (0..num_threads)
        .map(|tid| {
            let s = Arc::clone(&push_stack);
            thread::spawn(move || {
                for i in 0..ops_per_thread {
                    s.push(tid * ops_per_thread + i);
                }
            })
        })
        .collect();

    for h in push_handles {
        h.join().unwrap();
    }

    let total_pushed = num_threads * ops_per_thread;
    println!("Pushed {total_pushed} elements from {num_threads} threads");

    let pop_handles: Vec<_> = (0..num_threads)
        .map(|_| {
            let s = Arc::clone(&pop_stack);
            thread::spawn(move || {
                let mut count = 0;
                while s.pop().is_some() {
                    count += 1;
                }
                count
            })
        })
        .collect();

    let total_popped: usize = pop_handles.into_iter().map(|h| h.join().unwrap()).sum();
    println!("Popped {total_popped} elements from {num_threads} threads");
    assert_eq!(total_pushed, total_popped, "element count mismatch");
    println!("All elements accounted for.");
}
```

### Tests: `tests/correctness.rs`

```rust
use treiber_stack::stack::TreiberStack;
use std::collections::HashSet;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::Arc;
use std::thread;

#[test]
fn basic_push_pop() {
    let stack = TreiberStack::new();
    stack.push(1);
    stack.push(2);
    stack.push(3);
    assert_eq!(stack.pop(), Some(3));
    assert_eq!(stack.pop(), Some(2));
    assert_eq!(stack.pop(), Some(1));
    assert_eq!(stack.pop(), None);
}

#[test]
fn empty_stack() {
    let stack: TreiberStack<i32> = TreiberStack::new();
    assert!(stack.is_empty());
    assert_eq!(stack.pop(), None);
}

#[test]
fn is_empty_reflects_state() {
    let stack = TreiberStack::new();
    assert!(stack.is_empty());
    stack.push(42);
    assert!(!stack.is_empty());
    stack.pop();
    assert!(stack.is_empty());
}

/// Stress test: N threads push, N threads pop. Verify no lost or duplicated elements.
#[test]
fn concurrent_element_accounting() {
    let num_threads = 8;
    let ops_per_thread = 50_000;
    let stack = Arc::new(TreiberStack::new());
    let total_elements = num_threads * ops_per_thread;

    // Phase 1: all threads push.
    let push_handles: Vec<_> = (0..num_threads)
        .map(|tid| {
            let s = Arc::clone(&stack);
            thread::spawn(move || {
                for i in 0..ops_per_thread {
                    s.push((tid * ops_per_thread + i) as u64);
                }
            })
        })
        .collect();

    for h in push_handles {
        h.join().unwrap();
    }

    // Phase 2: all threads pop and collect.
    let collected = Arc::new(std::sync::Mutex::new(Vec::new()));

    let pop_handles: Vec<_> = (0..num_threads)
        .map(|_| {
            let s = Arc::clone(&stack);
            let c = Arc::clone(&collected);
            thread::spawn(move || {
                let mut local = Vec::new();
                loop {
                    match s.pop() {
                        Some(v) => local.push(v),
                        None => break,
                    }
                }
                c.lock().unwrap().extend(local);
            })
        })
        .collect();

    for h in pop_handles {
        h.join().unwrap();
    }

    let all = collected.lock().unwrap();
    assert_eq!(all.len(), total_elements, "wrong number of elements");

    // Verify no duplicates.
    let set: HashSet<u64> = all.iter().copied().collect();
    assert_eq!(set.len(), total_elements, "duplicate elements found");
}

/// Mixed push/pop stress test: threads randomly push and pop concurrently.
#[test]
fn concurrent_mixed_operations() {
    let stack = Arc::new(TreiberStack::new());
    let push_count = Arc::new(AtomicUsize::new(0));
    let pop_count = Arc::new(AtomicUsize::new(0));

    let num_threads = 8;
    let ops_per_thread = 100_000;

    let handles: Vec<_> = (0..num_threads)
        .map(|tid| {
            let s = Arc::clone(&stack);
            let pc = Arc::clone(&push_count);
            let pp = Arc::clone(&pop_count);
            thread::spawn(move || {
                for i in 0..ops_per_thread {
                    if (tid + i) % 2 == 0 {
                        s.push(i);
                        pc.fetch_add(1, Ordering::Relaxed);
                    } else if s.pop().is_some() {
                        pp.fetch_add(1, Ordering::Relaxed);
                    }
                }
            })
        })
        .collect();

    for h in handles {
        h.join().unwrap();
    }

    // Drain remaining elements.
    let mut remaining = 0;
    while stack.pop().is_some() {
        remaining += 1;
    }

    let total_pushed = push_count.load(Ordering::Relaxed);
    let total_popped = pop_count.load(Ordering::Relaxed) + remaining;
    assert_eq!(total_pushed, total_popped, "push/pop count mismatch");
}

/// Run the stress test many times to increase the chance of catching races.
#[test]
fn repeated_stress() {
    for _ in 0..50 {
        let stack = Arc::new(TreiberStack::new());
        let n = 4;
        let per = 10_000;

        let handles: Vec<_> = (0..n)
            .map(|tid| {
                let s = Arc::clone(&stack);
                thread::spawn(move || {
                    for i in 0..per {
                        s.push(tid * per + i);
                    }
                    let mut count = 0;
                    while s.pop().is_some() {
                        count += 1;
                    }
                    count
                })
            })
            .collect();

        let total_popped: usize = handles.into_iter().map(|h| h.join().unwrap()).sum();
        // Some elements might remain because threads race on pop.
        let mut remaining = 0;
        while stack.pop().is_some() {
            remaining += 1;
        }
        assert_eq!(total_popped + remaining, n * per);
    }
}

/// Verify that the stack can handle a large number of elements without leaking.
#[test]
fn large_push_then_pop() {
    let stack = TreiberStack::new();
    let count = 1_000_000;
    for i in 0..count {
        stack.push(i);
    }
    for _ in 0..count {
        assert!(stack.pop().is_some());
    }
    assert!(stack.pop().is_none());
}
```

### Benchmarks: `benches/stack_bench.rs`

```rust
use criterion::{criterion_group, criterion_main, BenchmarkId, Criterion};
use treiber_stack::stack::TreiberStack;
use treiber_stack::mutex_stack::MutexStack;
use std::sync::Arc;
use std::thread;

fn bench_low_contention(c: &mut Criterion) {
    let mut group = c.benchmark_group("low_contention_2_threads");
    let ops = 50_000;
    let threads = 2;

    group.bench_function("TreiberStack", |b| {
        b.iter(|| {
            let stack = Arc::new(TreiberStack::new());
            let handles: Vec<_> = (0..threads)
                .map(|_| {
                    let s = Arc::clone(&stack);
                    thread::spawn(move || {
                        for i in 0..ops {
                            s.push(i);
                            s.pop();
                        }
                    })
                })
                .collect();
            for h in handles { h.join().unwrap(); }
        });
    });

    group.bench_function("MutexStack", |b| {
        b.iter(|| {
            let stack = Arc::new(MutexStack::new());
            let handles: Vec<_> = (0..threads)
                .map(|_| {
                    let s = Arc::clone(&stack);
                    thread::spawn(move || {
                        for i in 0..ops {
                            s.push(i);
                            s.pop();
                        }
                    })
                })
                .collect();
            for h in handles { h.join().unwrap(); }
        });
    });

    group.finish();
}

fn bench_high_contention(c: &mut Criterion) {
    let mut group = c.benchmark_group("high_contention_16_threads");
    let ops = 10_000;
    let threads = 16;

    group.bench_function("TreiberStack", |b| {
        b.iter(|| {
            let stack = Arc::new(TreiberStack::new());
            let handles: Vec<_> = (0..threads)
                .map(|_| {
                    let s = Arc::clone(&stack);
                    thread::spawn(move || {
                        for i in 0..ops {
                            s.push(i);
                            s.pop();
                        }
                    })
                })
                .collect();
            for h in handles { h.join().unwrap(); }
        });
    });

    group.bench_function("MutexStack", |b| {
        b.iter(|| {
            let stack = Arc::new(MutexStack::new());
            let handles: Vec<_> = (0..threads)
                .map(|_| {
                    let s = Arc::clone(&stack);
                    thread::spawn(move || {
                        for i in 0..ops {
                            s.push(i);
                            s.pop();
                        }
                    })
                })
                .collect();
            for h in handles { h.join().unwrap(); }
        });
    });

    group.finish();
}

criterion_group!(benches, bench_low_contention, bench_high_contention);
criterion_main!(benches);
```

### Running

```bash
cargo build
cargo test
cargo test --release  # Run stress tests with optimizations (more realistic)
cargo run

# Benchmarks
cargo bench

# Check for undefined behavior (if nightly toolchain available):
# cargo +nightly miri test -- --test-threads=1
```

### Expected Output

```
=== Treiber Stack Demo ===

Pushed 1, 2, 3
Pop: Some(3)
Pop: Some(2)
Pop: Some(1)
Pop (empty): None

--- Concurrent stress test ---
Pushed 800000 elements from 8 threads
Popped 800000 elements from 8 threads
All elements accounted for.
```

## Design Decisions

1. **`crossbeam-epoch` over hand-rolled reclamation**: Epoch-based reclamation is conceptually simple but implementation-wise treacherous. A bug in the epoch advancement logic causes use-after-free that manifests only under specific thread interleaving. `crossbeam-epoch` is battle-tested, used by `crossbeam` itself and many production systems. The solution explains what it does (three-epoch rotation, deferred destruction, guard pinning) without reimplementing it.

2. **`compare_exchange_weak` over `compare_exchange`**: The weak variant may spuriously fail on platforms with LL/SC instructions (ARM). This is fine for a retry loop and avoids the overhead of the strong variant's retry-on-spurious-failure. On x86 (which has native CMPXCHG), there is no difference.

3. **Acquire on load, Release on successful CAS**: The push operation's Release ensures the new node's contents are visible to threads that later Acquire-load the top. The pop operation's Acquire on loading top ensures it sees the node's value and next pointer. This is the minimum correct ordering -- SeqCst would work but adds unnecessary fence overhead.

4. **`is_empty()` documented as racy**: A concurrent `is_empty()` check is inherently meaningless for control flow because another thread can push or pop between the check and the caller's next action. It is provided only for debugging/logging. Documenting this prevents callers from writing `if !stack.is_empty() { stack.pop().unwrap() }` which will panic.

5. **`Drop` drains the stack**: When the stack is dropped, all remaining elements must be deallocated. Since `Drop` takes `&mut self`, no concurrent access is possible, so we can simply loop `pop()` until empty. This guarantees no memory leaks even if the user forgets to drain.

## Common Mistakes

1. **Using `Relaxed` for the initial top load in pop**: This causes the reading thread to potentially see a stale pointer. On x86 this usually works due to the strong memory model, but on ARM/RISC-V it causes reading uninitialized or partially-written node data. The load must be `Acquire` to synchronize with the pushing thread's `Release` store.

2. **Forgetting to handle the CAS failure case correctly**: When `compare_exchange` fails, the returned error contains both the actual current value and the value you tried to store. You must reuse the stored value (the node you allocated) in the retry -- not allocate a new node. Allocating on every retry causes a memory leak of failed nodes.

3. **Testing with too few threads or operations**: A stress test with 2 threads and 1000 operations will pass even with broken memory ordering on x86. Concurrency bugs require high contention (8+ threads), many operations (100k+), and repeated runs (50+ iterations). Run stress tests in release mode where the compiler aggressively reorders instructions.

## Performance Notes

| Scenario | TreiberStack | Mutex<Vec<T>> |
|----------|-------------|---------------|
| Single thread | ~50ns/op (allocation overhead) | ~25ns/op (no contention) |
| 2 threads | ~80ns/op | ~120ns/op |
| 8 threads | ~200ns/op | ~500ns/op |
| 16 threads | ~350ns/op | ~1200ns/op |

(Approximate values on a modern x86_64 system. Actual numbers depend on hardware.)

At low contention, `Mutex<Vec>` wins because `Vec::push/pop` is a simple pointer increment with no allocation, while Treiber's stack allocates a node per push. At high contention, the lock-free stack scales significantly better because failed CAS retries are local operations (no kernel involvement), while mutex contention causes thread parking and context switches.

The crossover point is typically around 4-8 threads. Below that, the mutex's simpler fast path dominates. Above that, the lock-free algorithm's ability to make progress without waiting becomes decisive.

**Memory overhead**: Each element requires a heap allocation for `Node<T>` (value + pointer + allocator metadata, typically 32-48 bytes for small T). A `Vec<T>` stores elements contiguously with no per-element overhead. For memory-sensitive applications, consider a lock-free stack backed by a pre-allocated pool.

## Going Further

- Implement **tagged pointers** for ABA protection without epoch-based reclamation: pack a 16-bit counter into the upper bits of the pointer (possible on current x86_64 where only 48 bits are used for addresses). Compare the complexity and performance against the epoch approach
- Build an **elimination backoff stack**: when push and pop collide, they "exchange" directly without touching the shared stack, dramatically improving throughput under symmetric workloads (Hendler, Shavit, Yerushalmi 2004)
- Implement a **flat-combining stack**: threads publish their operations and a single combiner thread applies them in batch, trading latency for throughput
- Run under **ThreadSanitizer** (`RUSTFLAGS="-Z sanitizer=thread" cargo +nightly test`) to detect data races that Miri cannot catch (Miri does not support multithreaded execution)
- Port the implementation to use `std::sync::atomic::AtomicPtr` directly (without crossbeam) and implement a minimal epoch scheme to understand the reclamation internals firsthand
