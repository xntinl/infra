<!-- difficulty: intermediate-advanced -->
<!-- category: concurrency-patterns -->
<!-- languages: [rust] -->
<!-- concepts: [counting-semaphore, async-await, raii-guards, fair-queuing, tokio-runtime] -->
<!-- estimated_time: 6-10 hours -->
<!-- bloom_level: apply, analyze -->
<!-- prerequisites: [mutex-condvar, async-await-model, tokio-basics, raii-pattern, waker-mechanism] -->

# Challenge 66: Counting Semaphore (Sync + Async)

## Languages

Rust (stable, latest edition)

## Prerequisites

- Understanding of counting semaphores (permit-based concurrency limiting)
- Familiarity with `Mutex`, `Condvar`, and RAII guard patterns in Rust
- Working knowledge of async/await, `Future`, `Poll`, `Waker`, and the tokio runtime
- Experience with `Pin`, `Unpin`, and why they matter for self-referential futures
- Understanding of fairness problems: starvation, convoy effects, priority inversion

## Learning Objectives

- **Implement** a counting semaphore that works in both synchronous and asynchronous contexts
- **Apply** RAII guards to ensure permits are always released, even on panic or early return
- **Analyze** fairness properties and implement FIFO wakeup ordering to prevent starvation
- **Design** timeout support for both sync (`Duration`) and async (`tokio::time::timeout`) variants
- **Evaluate** the semaphore's behavior under contention by building a connection pool limiter

## The Challenge

Implement a counting semaphore -- a concurrency primitive that controls access to a shared resource by maintaining a set of permits. A semaphore initialized with N permits allows at most N concurrent accesses. Each `acquire` takes a permit (blocking if none available), and each `release` returns one. Unlike a mutex (which is a semaphore with N=1), a counting semaphore allows tunable parallelism.

You must build two implementations sharing the same core logic: one for synchronous code (using `std` threads, `Mutex`, `Condvar`) and one for asynchronous code (using tokio, `Waker`-based notification). The sync version blocks the thread on acquire; the async version returns a `Future` that yields `Poll::Pending` until a permit is available.

Fairness is the central design challenge. A naive implementation wakes all waiters when a permit is released, and whichever thread/task grabs it first wins. Under high contention, this causes starvation -- some waiters never acquire. Your implementation must use FIFO ordering: waiters are queued in arrival order, and the longest-waiting thread/task gets the next available permit. This requires a wait queue (e.g., `VecDeque` of wakers or condvar waiters).

RAII guards are non-negotiable. The `acquire` method returns a `SemaphoreGuard` whose `Drop` implementation calls `release`. This guarantees permits are returned even if the caller panics, returns early, or drops the guard. Without RAII, every code path must remember to release -- a single missed path means a permanent permit leak.

As a concrete usage example, build a connection pool limiter: a struct that wraps a semaphore and limits the number of concurrent database connections (or HTTP requests, or file handles). This demonstrates why semaphores exist in real systems.

## Requirements

1. Implement `SyncSemaphore` with `new(permits: usize)` constructor
2. Implement `acquire(&self) -> SemaphoreGuard` -- blocks until a permit is available, returns RAII guard
3. Implement `try_acquire(&self) -> Option<SemaphoreGuard>` -- returns immediately, `None` if no permits
4. Implement `acquire_timeout(&self, duration: Duration) -> Option<SemaphoreGuard>` -- blocks up to duration
5. Implement `SemaphoreGuard` with `Drop` that releases the permit automatically
6. Implement fair FIFO queuing -- waiters acquire in arrival order, not in arbitrary wakeup order
7. Implement `AsyncSemaphore` with `new(permits: usize)` constructor
8. Implement `async acquire(&self) -> SemaphoreGuard` -- async, yields until permit available
9. Implement `try_acquire(&self) -> Option<SemaphoreGuard>` -- synchronous, immediate
10. Build `ConnectionLimiter<T>` that wraps a semaphore and a pool of `T`, limiting concurrent access
11. Write a sync stress test: 100 threads competing for 5 permits, verify at most 5 run concurrently
12. Write an async stress test: 200 tokio tasks competing for 10 permits, verify concurrency limit
13. Write a fairness test: 20 waiters acquire in order, verify FIFO ordering
14. Benchmark acquire/release latency under no contention, moderate (50% saturation), and high (95% saturation)

## Hints

<details>
<summary>Hint 1: Sync core structure</summary>

```
struct Inner {
    permits: usize,
    waiters: VecDeque<Arc<WaiterSlot>>,
}

struct WaiterSlot {
    notified: AtomicBool,
    condvar: Condvar,
    mutex: Mutex<()>,
}
```

Each waiter creates a `WaiterSlot`, pushes it to the back of the queue, and waits on its personal `Condvar`. When a permit is released, pop the front slot and notify it. This ensures FIFO order because each waiter sleeps on its own condvar, and only the front-of-queue waiter is woken.

</details>

<details>
<summary>Hint 2: Async core structure</summary>

For the async version, replace `Condvar` with `Waker`. Each waiter stores a `Waker` in the queue. When a permit is released, pop the front waiter and call `waker.wake()`. The `Future` implementation for `Acquire` registers the waker in `poll` and returns `Poll::Pending`. On subsequent polls, check if this waiter is at the front and a permit is available.

</details>

<details>
<summary>Hint 3: RAII guard design</summary>

The guard holds an `Arc` reference to the semaphore's inner state. On `Drop`, it increments the permit count and wakes the next waiter. This means the semaphore must outlive all guards -- `Arc` ensures this. Do not use raw references or lifetimes for the guard-to-semaphore link; it becomes unergonomic with async code.

</details>

<details>
<summary>Hint 4: Timeout for sync</summary>

In `acquire_timeout`, compute the deadline as `Instant::now() + duration`. In the wait loop, calculate remaining time as `deadline - Instant::now()`. If remaining is zero or negative, remove this waiter from the queue and return `None`. Use `Condvar::wait_timeout` with the remaining duration. Be careful to remove the waiter slot from the queue on timeout -- otherwise it occupies a queue position forever.

</details>

<details>
<summary>Hint 5: Connection limiter pattern</summary>

```
struct ConnectionLimiter {
    semaphore: AsyncSemaphore,
    // ... pool of connections
}
```

`get_connection` acquires a semaphore permit, takes a connection from the pool, returns a guard that holds both the connection and the semaphore permit. When the guard drops, the connection returns to the pool and the permit is released. This limits concurrency without the caller managing permits manually.

</details>

## Acceptance Criteria

- [ ] Sync semaphore blocks and wakes correctly with RAII guards
- [ ] Async semaphore integrates with tokio and respects `Waker` protocol
- [ ] `try_acquire` never blocks in either variant
- [ ] Timeout support works correctly (returns `None` on expiry, does not leak queue slots)
- [ ] FIFO fairness: 20 sequential waiters acquire in arrival order
- [ ] Stress test: 100 threads / 200 tasks never exceed the permit limit
- [ ] Guards release permits on drop, including on panic (`catch_unwind` test)
- [ ] Connection limiter demo works end-to-end
- [ ] No deadlocks -- run stress tests 100+ times in release mode
- [ ] All tests pass with `cargo test`
- [ ] Code compiles with no warnings

## Research Resources

- [Rust Atomics and Locks (Mara Bos), Chapter 9](https://marabos.nl/atomics/building-locks.html) -- building synchronization primitives from scratch
- [tokio::sync::Semaphore source code](https://github.com/tokio-rs/tokio/blob/master/tokio/src/sync/semaphore.rs) -- production async semaphore, study the waiter queue
- [The Little Book of Semaphores (Allen Downey)](https://greenteapress.com/wp/semaphores/) -- classic patterns: producer-consumer, readers-writers, dining philosophers
- [Waker and Future in Rust (async book)](https://rust-lang.github.io/async-book/02_execution/03_wakeups.html) -- how wakers drive async execution
- [Fairness in Concurrent Programming](https://en.wikipedia.org/wiki/Fairness_(computer_science)) -- starvation, bounded waiting, and FIFO guarantees
