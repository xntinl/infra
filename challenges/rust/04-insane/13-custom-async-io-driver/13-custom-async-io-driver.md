# 13. Custom Async I/O Driver
**Difficulty**: Insane

## The Challenge

Every time you write `.await` in a Tokio or async-std program, an I/O reactor somewhere is polling the operating system for readiness events, managing timer deadlines, and waking the appropriate futures. In this exercise, you build that reactor from scratch. No Tokio, no async-std, no mio -- just raw system calls, a hand-written event loop, and a custom `Future`-based API layered on top.

You will build a single-threaded async runtime called `typhoon` that provides non-blocking TCP networking and timers. At its core is an event loop that calls `epoll_wait` (Linux) or `kqueue` (macOS) to detect I/O readiness, a timer wheel for scheduling timeouts with O(1) insertion and amortized O(1) expiration, and a task scheduler that drives `Future` objects to completion using `Waker`-based notifications. On top of this foundation, you will build `TcpListener` and `TcpStream` types that implement `Future` and `AsyncRead`/`AsyncWrite` traits, enabling users to write async/await code against your runtime.

This is not a toy. Your reactor must handle at least 10,000 concurrent connections without degradation, must support backpressure (when a write buffer is full, the corresponding future must yield rather than spin), and must correctly handle edge-triggered vs level-triggered semantics depending on the platform. You will benchmark it against Tokio on a simple echo server to establish a performance baseline.

## Acceptance Criteria

### Event Loop / Reactor

- [ ] Implements a `Reactor` struct that wraps an `epoll` (Linux) or `kqueue` (macOS) file descriptor
- [ ] Uses conditional compilation (`#[cfg(target_os = "linux")]` / `#[cfg(target_os = "macos")]`) to support both platforms with a unified internal API
- [ ] The reactor maintains a slab of `IoSource` registrations, each tracking the file descriptor, interest (readable/writable), and the `Waker` to notify
- [ ] `Reactor::register(fd, interest) -> io::Result<SourceId>` registers a new file descriptor
- [ ] `Reactor::reregister(id, interest)` updates the interest set for an existing registration
- [ ] `Reactor::deregister(id)` removes a registration and closes the kernel-side subscription
- [ ] `Reactor::poll(timeout: Option<Duration>)` calls the OS poll function, collects readiness events, and wakes the appropriate tasks via stored `Waker`s
- [ ] Uses edge-triggered mode on `epoll` (`EPOLLET`) and `EV_CLEAR` on kqueue so that a single readiness notification wakes the task exactly once
- [ ] Handles `EINTR` from poll calls by retrying immediately
- [ ] The reactor is thread-local (stored in a `RefCell<Option<Reactor>>` accessible via `Reactor::with_current()`)

### Timer Wheel

- [ ] Implements a hierarchical timer wheel with at least two levels (e.g., 256 slots at 1ms granularity for the first 256ms, then 64 coarser slots for up to ~16 seconds, with overflow into a sorted list)
- [ ] `TimerWheel::insert(deadline: Instant, waker: Waker) -> TimerId` schedules a timer with O(1) insertion
- [ ] `TimerWheel::cancel(id: TimerId)` cancels a pending timer
- [ ] `TimerWheel::poll(now: Instant) -> Vec<Waker>` returns all expired timers' wakers and cascades from higher wheels as needed
- [ ] The timer wheel is integrated into the reactor's poll loop: the timeout passed to `epoll_wait`/`kevent` is the duration until the next timer expiration
- [ ] Provides a `sleep(duration: Duration) -> Sleep` future that resolves after the given duration
- [ ] `sleep` futures are cancel-safe: dropping the future cancels the underlying timer

### Task Scheduler

- [ ] Implements a single-threaded executor that maintains a `VecDeque` of runnable task IDs
- [ ] Tasks are heap-allocated `Pin<Box<dyn Future<Output = ()>>>` stored in a slab
- [ ] `spawn(future) -> JoinHandle<T>` spawns a new task and returns a handle that can be awaited for the result
- [ ] `JoinHandle<T>` implements `Future<Output = Result<T, JoinError>>` where `JoinError` covers cancellation and panic cases
- [ ] The executor's `block_on(future) -> T` function drives the main future to completion by looping: poll the main future, drain the run queue, call reactor poll, repeat
- [ ] `Waker` implementation is custom: waking a task adds its ID to the run queue (using a `Rc<RefCell<VecDeque<TaskId>>>` shared between the executor and waker clones)
- [ ] The `RawWaker` vtable is manually implemented with correct `clone`, `wake`, `wake_by_ref`, and `drop` semantics
- [ ] Dropping a `JoinHandle` does NOT cancel the task (fire-and-forget semantics, like Tokio's `JoinHandle`)
- [ ] A `yield_now()` future that reschedules the current task to the back of the run queue

### TCP Networking

- [ ] `TcpListener::bind(addr) -> io::Result<TcpListener>` creates a non-blocking listening socket, binds it, and registers it with the reactor
- [ ] `TcpListener::accept(&self) -> Accept<'_>` returns a future that resolves to `(TcpStream, SocketAddr)`
- [ ] The `Accept` future tries `accept4`/`accept` in non-blocking mode; if it would block (`EAGAIN`/`EWOULDBLOCK`), it registers interest and returns `Poll::Pending`
- [ ] `TcpStream::connect(addr) -> Connect` returns a future that performs non-blocking connect and resolves when the connection is established (handles `EINPROGRESS`)
- [ ] `TcpStream` implements `AsyncRead` and `AsyncWrite` traits (define your own traits matching the Tokio signatures: `poll_read`, `poll_write`, `poll_flush`, `poll_shutdown`)
- [ ] Read operations handle `EAGAIN` by re-registering for readable interest and returning `Pending`
- [ ] Write operations handle `EAGAIN` by re-registering for writable interest and returning `Pending`
- [ ] Write operations handle partial writes correctly (advancing the buffer cursor)
- [ ] `TcpStream::shutdown(how: Shutdown)` properly calls `libc::shutdown` and deregisters from the reactor
- [ ] `Drop` for both `TcpListener` and `TcpStream` deregisters from the reactor and closes the file descriptor

### Backpressure and Correctness

- [ ] When a write returns `EAGAIN`, the stream future yields control rather than busy-looping
- [ ] When the accept backlog is full, the listener future yields rather than spinning
- [ ] Demonstrates backpressure with an asymmetric producer-consumer test: a fast writer and a slow reader, verifying that the writer suspends when the kernel buffer fills
- [ ] No busy-waiting anywhere: when there are no ready events and no expired timers, the thread sleeps in the OS poll call
- [ ] Correct handling of `EPOLLHUP`/`EPOLLRDHUP` (Linux) and `EV_EOF` (macOS) -- these wake the task which then reads 0 bytes and handles the connection close
- [ ] Handles the thundering herd problem for accept: only one task is woken per incoming connection

### Testing and Benchmarks

- [ ] An echo server test: spawn 100 concurrent clients that each send 1KB of random data, read it back, and verify byte-for-byte equality
- [ ] A timer accuracy test: schedule 1000 timers at random intervals between 10ms and 500ms, verify each fires within 5ms of its deadline
- [ ] A cancellation test: spawn a task, drop its `JoinHandle`, verify the task still runs to completion
- [ ] A connection test: test `TcpStream::connect` to a listening socket and verify bidirectional communication
- [ ] A backpressure test as described above
- [ ] A load test: 10,000 concurrent echo connections, verify no fd leaks (check `/proc/self/fd` count or equivalent)
- [ ] Benchmark: measure echo server throughput (requests/sec and bytes/sec) with 100 concurrent connections, compare against an equivalent Tokio echo server

## Starting Points

- Study `mio` crate source (`src/sys/unix/selector/epoll.rs` and `src/sys/unix/selector/kqueue.rs`) for how to correctly wrap epoll/kqueue system calls and handle edge cases like `EINTR`
- Read the `tokio` reactor source in `tokio/src/runtime/io/driver.rs` and `tokio/src/runtime/io/registration.rs` to understand how a production reactor maps readiness events to wakers
- Study `tokio/src/runtime/task/raw.rs` and `tokio/src/runtime/task/waker.rs` for how to build a correct `RawWaker` vtable -- this is one of the most error-prone parts
- Read the Kafka paper on timer wheels: "Hashed and Hierarchical Timing Wheels" by Varghese and Lauck (1996) for the theory behind O(1) timer insertion
- Study `tokio/src/time/wheel/mod.rs` for a production implementation of a hierarchical timer wheel
- Read the Linux `epoll(7)` man page thoroughly, especially the edge-triggered semantics section and the notes about `EPOLLONESHOT`
- Study the `async-io` crate by Stjepan Glavina (used by `smol` runtime) for a simpler reactor design that still handles the key edge cases
- Read "Epoll is fundamentally broken" blog posts to understand common pitfalls with edge-triggered notification

## Hints

1. Start with the reactor, not the executor. Get `epoll_create1`/`kqueue` working and verify you can detect readiness on a simple pipe pair before building any async abstractions. Use the `libc` crate for system call bindings.

2. For the slab data structure (mapping registration IDs to their state), use a simple `Vec<Option<Entry>>` with a free list (stack of empty indices). This gives O(1) insert and remove. The `slab` crate does exactly this, but implement your own since this is a learning exercise.

3. Edge-triggered mode means you get notified when the state *changes* from not-ready to ready, not every time you poll. This means your `AsyncRead` implementation must drain the socket completely (read in a loop until `EAGAIN`) to avoid missing events. However, for simplicity, you can re-register after each `Pending` return using `EPOLLONESHOT` to get oneshot semantics instead.

4. The `RawWaker` vtable has four function pointers: `clone`, `wake`, `wake_by_ref`, and `drop`. The data pointer should be an `Rc<TaskWaker>` where `TaskWaker` holds the task ID and a reference to the run queue. `clone` clones the `Rc` (increment refcount via `Rc::into_raw`/`Rc::from_raw`), `wake` pushes the task ID and consumes the `Rc`, `wake_by_ref` pushes the task ID without consuming, and `drop` drops the `Rc`.

5. For the timer wheel, think of it as a clock. The first level has 256 slots, each representing 1ms. A cursor advances every millisecond. When a timer falls within the current rotation (0-255ms from now), it goes directly into the appropriate slot. When it is further out, it goes into a second-level wheel with coarser granularity. On each tick, check if higher-level timers need to be cascaded down.

6. `block_on` should look roughly like: `loop { poll main future; if ready, return result; drain run queue (poll each queued task); compute next timer deadline; reactor.poll(deadline); }`. The key insight is that the reactor poll both waits for I/O events AND handles the sleep until the next timer.

7. For `TcpStream::connect`, non-blocking connect returns `EINPROGRESS`. You must then register for writable interest. When the socket becomes writable, call `getsockopt(SO_ERROR)` to check if the connection succeeded or failed. This is a common gotcha -- writability alone does not mean the connection succeeded.

8. Set `SO_REUSEADDR` on the listening socket before binding. Set `TCP_NODELAY` on connected sockets. Set all sockets to non-blocking mode with `fcntl(fd, F_SETFL, O_NONBLOCK)`.

9. For the accept loop, call `accept` in a loop until you get `EAGAIN`, not just once per readiness notification. In edge-triggered mode, a single readiness event might correspond to multiple pending connections.

10. For the echo server benchmark, use a simple protocol: the client sends N bytes, the server reads and immediately writes them back. Use `criterion` for microbenchmarks and a custom harness for the load test. Measure latency percentiles (p50, p99) in addition to throughput.

11. Backpressure testing: create a connected pair, set the write buffer to a small size via `SO_SNDBUF`, then spawn a task that writes continuously. Verify that the write future yields (returns `Pending`) when the buffer is full, rather than returning an error or blocking the thread.

12. For macOS kqueue, the `kevent` struct uses `ident` (the fd) and `filter` (`EVFILT_READ`/`EVFILT_WRITE`) to identify subscriptions. Unlike epoll where you register a single fd with multiple events, kqueue uses separate entries for read and write interest. Handle this difference in your abstraction layer.

13. Memory safety: every file descriptor must be closed exactly once. Use a `DropGuard` pattern or Rust's `OwnedFd` (available since Rust 1.63) to ensure fds are closed on drop. Leaking fds under load is the most common bug in custom reactors.

14. For `JoinHandle<T>`, the result needs to be transferred from the task to the handle. Use a shared `Rc<RefCell<Option<T>>>` -- the task writes the result when complete and wakes the `JoinHandle`'s waker, the `JoinHandle` reads it when polled.

15. Test timer accuracy carefully. `Instant::now()` has platform-dependent resolution. On Linux, `clock_gettime(CLOCK_MONOTONIC)` gives nanosecond precision. Your timer wheel tick interval should be at least 1ms to avoid excessive overhead. Accept that timers may fire slightly late (due to the poll loop granularity) but should never fire early.
