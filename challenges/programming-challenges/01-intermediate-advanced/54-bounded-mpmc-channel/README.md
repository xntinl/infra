<!-- difficulty: intermediate-advanced -->
<!-- category: concurrency-patterns -->
<!-- languages: [rust] -->
<!-- concepts: [mpmc-channel, ring-buffer, atomics, condition-variables, backpressure] -->
<!-- estimated_time: 6-10 hours -->
<!-- bloom_level: apply, analyze -->
<!-- prerequisites: [atomic-operations, mutex-condvar, memory-ordering, generics, trait-implementation] -->

# Challenge 54: Bounded MPMC Channel

## Languages

Rust (stable, latest edition)

## Prerequisites

- Solid understanding of `Mutex`, `Condvar`, and their interaction patterns
- Familiarity with atomic operations (`AtomicUsize`, `AtomicBool`) and memory ordering
- Knowledge of ring buffer data structures and modular arithmetic
- Experience implementing generic types with trait bounds (`Send`, `Sync`)
- Understanding of channel semantics (Go channels, `std::sync::mpsc`, `crossbeam-channel`)

## Learning Objectives

- **Implement** a bounded multi-producer multi-consumer channel backed by a fixed-size ring buffer
- **Apply** condition variables to coordinate blocking send/recv with timeout support
- **Analyze** the trade-offs between blocking and non-blocking channel operations
- **Design** close semantics that propagate gracefully to all waiting producers and consumers
- **Evaluate** throughput and latency against `crossbeam-channel` under varied contention levels

## The Challenge

Build a bounded MPMC channel from scratch. The channel has a fixed capacity set at construction time, backed by a contiguous ring buffer. Multiple threads can send and receive concurrently. When the buffer is full, senders block until space is available. When the buffer is empty, receivers block until data arrives. Both operations support timeouts and non-blocking variants.

The core data structure is a ring buffer with `head` and `tail` indices. Producers write at `tail` and advance it; consumers read at `head` and advance it. The indices wrap around using modular arithmetic. The critical challenge is synchronizing access: you need mutual exclusion for the buffer slots, but condition variables for the blocking/waking logic. A single mutex protecting the entire buffer is the simplest correct approach -- optimize only after correctness is proven.

Close semantics are essential. When the channel is closed, all pending senders must receive an error, all pending receivers must drain remaining elements and then receive `None`, and no new sends are accepted. This is the behavior of Go channels and `crossbeam-channel::bounded`. Getting this right requires careful interaction between the close flag and the condition variable notifications -- a close must wake all blocked threads, not just one.

The iterator interface (`IntoIterator` for the receiver) enables `for value in rx { ... }` patterns. The iterator yields elements until the channel is closed and drained.

This is a foundational concurrency primitive. Understanding how it works internally will make you a better user of channels in every language.

## Requirements

1. Implement `Channel<T>` with a fixed-capacity ring buffer allocated at construction
2. Provide `Sender<T>` and `Receiver<T>` handles, both cloneable, that share the channel via `Arc`
3. Implement `try_send(value: T) -> Result<(), TrySendError<T>>` -- non-blocking, returns error if full or closed
4. Implement `try_recv() -> Result<T, TryRecvError>` -- non-blocking, returns error if empty or closed-and-drained
5. Implement `send(value: T) -> Result<(), SendError<T>>` -- blocks until space available; returns error if closed
6. Implement `recv() -> Result<T, RecvError>` -- blocks until data available; returns `Err` only when closed and empty
7. Implement `send_timeout(value: T, duration: Duration) -> Result<(), SendTimeoutError<T>>` -- blocks up to duration
8. Implement `recv_timeout(duration: Duration) -> Result<T, RecvTimeoutError>` -- blocks up to duration
9. Implement `close()` on the sender side -- subsequent sends fail, receivers drain remaining then get `None`
10. Implement `Iterator` for `Receiver<T>` (or an `IntoIter` wrapper) -- yields until closed and drained
11. Ensure `Sender<T>: Send + Sync` and `Receiver<T>: Send + Sync` with appropriate bounds on `T`
12. Write concurrent correctness test: 8 producers, 8 consumers, 500k total messages, verify all arrive exactly once
13. Write close-semantics test: close mid-stream, verify senders get errors and receivers drain then stop
14. Benchmark against `crossbeam::channel::bounded` at capacity 64, 1024, 65536 with 4 and 16 threads

## Hints

<details>
<summary>Hint 1: Ring buffer layout</summary>

Allocate a `Vec<UnsafeCell<MaybeUninit<T>>>` of the requested capacity. Use `head: usize` and `tail: usize` indices with `count: usize` to track occupancy. The slot at index `tail % capacity` is the next write position; `head % capacity` is the next read position. `count` avoids the ambiguity of `head == tail` meaning either full or empty.

</details>

<details>
<summary>Hint 2: Synchronization strategy</summary>

Start with a single `Mutex` protecting all shared state (head, tail, count, closed flag) and two `Condvar`s: `not_full` (wakes senders) and `not_empty` (wakes receivers). After a successful send, notify `not_empty`. After a successful recv, notify `not_full`. After close, notify both (use `notify_all`). This is correct and simple. Avoid lock-free tricks until this works.

</details>

<details>
<summary>Hint 3: Close semantics</summary>

Set a `closed: bool` flag inside the mutex. On close: set the flag, then call `not_full.notify_all()` and `not_empty.notify_all()`. In `send`, check `closed` after acquiring the lock and after waking from a condvar wait -- a close may have happened while you were sleeping. In `recv`, if `closed && count == 0`, return the "disconnected" error; if `closed && count > 0`, still return data.

</details>

<details>
<summary>Hint 4: Timeout with Condvar</summary>

`Condvar::wait_timeout` returns a `(MutexGuard, WaitTimeoutResult)`. Check `WaitTimeoutResult::timed_out()` after waking. But also re-check the condition -- a spurious wakeup may have occurred without the condition changing. Loop: wait with remaining duration, subtract elapsed on each iteration, break if condition met or timeout expired.

</details>

<details>
<summary>Hint 5: Avoiding unsafe</summary>

You can avoid `UnsafeCell<MaybeUninit<T>>` entirely by using `Option<T>` in each slot. This is simpler but wastes discriminant space and requires branching on read. For a first implementation, `Vec<Option<T>>` is fine. Optimize to `MaybeUninit` only if benchmarks show the difference matters.

</details>

## Acceptance Criteria

- [ ] Channel supports multiple concurrent producers and consumers
- [ ] `try_send`/`try_recv` never block and return appropriate errors
- [ ] `send`/`recv` block correctly and wake on data/space availability
- [ ] `send_timeout`/`recv_timeout` respect the deadline and return timeout errors
- [ ] Close propagates to all blocked threads -- senders get errors, receivers drain then stop
- [ ] Iterator interface yields all elements then stops after close
- [ ] No lost or duplicated messages under concurrent stress (8P + 8C, 500k messages)
- [ ] No deadlocks -- run stress test 100+ times in release mode
- [ ] Benchmarks compare against `crossbeam::channel::bounded` at multiple capacities
- [ ] All tests pass with `cargo test`
- [ ] Code compiles with no warnings

## Research Resources

- [Rust Atomics and Locks (Mara Bos), Chapter 9: Channels](https://marabos.nl/atomics/channels.html) -- builds a channel from scratch, covers exactly this territory
- [crossbeam-channel source code](https://github.com/crossbeam-rs/crossbeam/tree/master/crossbeam-channel/src) -- production-grade bounded channel, study the flavors module
- [Go channel implementation (runtime/chan.go)](https://github.com/golang/go/blob/master/src/runtime/chan.go) -- the reference implementation for bounded channel semantics
- [Condvar documentation (Rust std)](https://doc.rust-lang.org/std/sync/struct.Condvar.html) -- spurious wakeups, wait_timeout, and interaction with Mutex
- [1024cores: Bounded MPMC Queue](https://www.1024cores.net/home/lock-free-algorithms/queues/bounded-mpmc-queue) -- Dmitry Vyukov's lock-free bounded MPMC queue, a more advanced approach
