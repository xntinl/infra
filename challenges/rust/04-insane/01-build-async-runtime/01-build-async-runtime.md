# 1. Build an Async Runtime

**Difficulty**: Insane

## The Challenge

Build a minimal but functional async runtime from scratch. No tokio, no async-std,
no smol — just you, `std::future::Future`, `std::task::Waker`, and raw system calls.

By the end you will have a working executor that can spawn multiple concurrent tasks,
drive them to completion through cooperative scheduling, and perform non-blocking I/O
on real file descriptors using your platform's readiness API (epoll on Linux, kqueue
on macOS/BSD).

This is how you stop treating async Rust as magic and start understanding it as
machinery. Every `.await` compiles down to a state machine that your runtime polls.
You are building the thing that does the polling.

## Acceptance Criteria

- [ ] Implement a `Task` type that wraps a `Pin<Box<dyn Future<Output = ()> + Send>>`
- [ ] Construct a valid `RawWaker` / `RawWakerVTable` that re-enqueues tasks on wake
- [ ] Implement a single-threaded executor with a run loop that parks when idle
- [ ] Provide a `spawn()` function that submits new futures to the executor
- [ ] Implement a `block_on()` function that drives a single future to completion
- [ ] Integrate platform I/O readiness (epoll or kqueue) so that `TcpStream`-like
      futures actually wait for data without busy-spinning
- [ ] Implement a basic `Timer` future backed by timerfd (Linux) or kqueue timer
      (macOS) that resolves after a specified duration
- [ ] Demonstrate correctness: run 10,000 concurrent timer tasks that each sleep for
      a random duration, verify they all complete and the executor exits cleanly
- [ ] The executor must not busy-spin — it must park the thread when no tasks are ready
- [ ] No dependencies beyond `std` and `libc` (or the `nix` crate for syscall wrappers)

## Starting Points

- **tokio runtime scheduler**: Study `tokio/tokio/src/runtime/scheduler/current_thread/mod.rs`.
  This is tokio's single-threaded executor. Follow how `Context` is built and how
  the LIFO slot optimization works. You do not need the optimization — but the
  structure reveals the essential moving parts.
- **RawWaker construction**: Study `tokio/tokio/src/runtime/task/waker.rs`. The raw
  waker vtable has four function pointers: clone, wake, wake_by_ref, drop. Each one
  must manage the reference count of the task correctly or you get use-after-free.
- **async-task crate** (`smol-rs/async-task`): A minimal task abstraction. Study
  `src/raw.rs` — the `RawTask` layout is a single allocation containing header,
  schedule function, and the future itself. Understand why this layout matters.
- **mio source** (`tokio-rs/mio`): Look at `src/sys/unix/selector/epoll.rs` and
  `src/sys/unix/selector/kqueue.rs` for how readiness events map to tokens. Your I/O
  reactor is a simpler version of this.
- **Rust RFC 2394** (async/await): The RFC that introduced the `Future` trait design
  and the rationale for `Pin` + `Waker` over alternatives like callback-based designs.

## Hints

1. Your executor needs two core data structures: a task queue (VecDeque or crossbeam
   channel) and an I/O reactor (the epoll/kqueue fd plus a slab mapping tokens to
   wakers). The run loop alternates between draining the task queue and polling the
   reactor.

2. The `RawWakerVTable` functions receive a `*const ()` — this is your task pointer
   cast to raw. You need a reference-counting scheme (Arc or manual) so that waking
   a task from an I/O callback does not dangle. Get this wrong and Miri will catch it.

3. Do not try to build a multi-threaded work-stealing executor first. Get single-threaded
   working completely. The jump to multi-threaded requires a lock-free injection queue
   and thread parking — that is a separate project.

4. Your `Timer` future needs to register a timeout with the reactor on first poll,
   then return `Poll::Pending`. When the reactor sees the timer fire, it wakes the
   associated task. On the next poll, the timer returns `Poll::Ready(())`. This
   two-phase register-then-complete pattern is the fundamental async I/O pattern.

5. Thread parking when idle: after draining the task queue, call
   `epoll_wait` / `kevent` with a timeout. If new tasks were enqueued (via wake)
   while you were parked, you need a mechanism to break out of the syscall — a
   self-pipe or eventfd works.

## Going Further

- Add a multi-threaded work-stealing scheduler. Study `tokio/tokio/src/runtime/scheduler/multi_thread/worker.rs`
  for the steal-half strategy and LIFO slot.
- Implement `JoinHandle<T>` with proper output retrieval and cancellation on drop.
- Add a `select!`-like macro that polls multiple futures and returns the first one ready.
- Build a simple TCP echo server on top of your runtime and benchmark it against tokio
  with `wrk` or `hey`.
- Run your runtime under Miri (`MIRIFLAGS="-Zmiri-disable-isolation" cargo +nightly miri run`)
  to validate that your unsafe waker code is sound.

## Resources

- [The Rust Async Book](https://rust-lang.github.io/async-book/) — Chapter on
  executing futures and the waker pattern
- [RFC 2394: async/await](https://rust-lang.github.io/rfcs/2394-async_await.html) —
  The design rationale for the current Future + Waker model
- [tokio source](https://github.com/tokio-rs/tokio) — `tokio/src/runtime/` is the
  runtime, `tokio/src/io/` is the I/O driver
- [async-task source](https://github.com/smol-rs/async-task) — Minimal task cell
  implementation
- [mio source](https://github.com/tokio-rs/mio) — Cross-platform I/O reactor
- [Phil Opp: "Async/Await"](https://os.phil-opp.com/async-await/) — Builds an
  executor in an OS kernel context, excellent diagrams of the state machine
- [withoutboats: "The Waker API"](https://boats.gitlab.io/blog/) — Blog posts on
  the original design decisions behind Waker
- [Carl Lerche: "Reducing tail latencies with automatic cooperative task yielding"](https://tokio.rs/blog/2020-04-preemption) —
  Why voluntary yield points matter in real runtimes
- `man 7 epoll`, `man 2 kevent` — The actual syscall documentation you will be
  calling through `libc` or `nix`
