# 93. Epoll/Kqueue Event Loop Reactor

<!--
difficulty: advanced
category: systems-programming
languages: [rust]
concepts: [io-multiplexing, epoll, kqueue, reactor-pattern, non-blocking-io, event-driven, callback-handlers, timer-management]
estimated_time: 12-18 hours
bloom_level: evaluate
prerequisites: [rust-ownership, tcp-sockets, file-descriptors, non-blocking-io-basics, unix-syscalls, closures-fn-traits]
-->

## Languages

- Rust (stable)

## Prerequisites

- Rust ownership, lifetimes, and trait objects (`Box<dyn Fn>`)
- TCP sockets: `TcpListener`, `TcpStream`, `bind`, `accept`, `read`, `write`
- File descriptors and their role in Unix I/O
- Non-blocking I/O: `O_NONBLOCK`, `EAGAIN`/`EWOULDBLOCK` error handling
- Basic Unix syscalls: `fcntl`, `close`, `read`, `write`
- Closures and the `Fn`/`FnMut`/`FnOnce` trait hierarchy in Rust
- Platform-specific APIs: `epoll` (Linux) or `kqueue` (macOS/BSD)
- Understanding of the C FFI in Rust (`libc` crate)

## Learning Objectives

- **Implement** a single-threaded event loop reactor that multiplexes I/O across hundreds of concurrent connections
- **Evaluate** the trade-offs between level-triggered and edge-triggered notification in epoll, or between `EVFILT_READ`/`EVFILT_WRITE` in kqueue
- **Design** a callback registration system that maps file descriptors to event handlers without runtime borrowing conflicts
- **Analyze** how the reactor pattern achieves concurrency without threads or async runtimes
- **Implement** timer support using timerfd (Linux) or `EVFILT_TIMER` (kqueue) integrated into the event loop
- **Apply** non-blocking socket I/O correctly, handling partial reads, partial writes, and `EAGAIN`
- **Measure** the reactor's scalability by benchmarking connection throughput up to 10,000+ concurrent connections

## The Challenge

The reactor pattern is the foundation of every high-performance network server. Nginx, Redis, Node.js, and Tokio all use some form of event loop driven by OS-level I/O multiplexing. The core idea: instead of one thread per connection, a single thread monitors all connections simultaneously using an OS primitive -- `epoll` on Linux, `kqueue` on macOS/BSD -- and dispatches callbacks when I/O is ready.

The operating system kernel already tracks which file descriptors have data available. `epoll`/`kqueue` exposes this information efficiently: register interest in specific descriptors, then block until one or more are ready. The reactor loop calls the OS, iterates over ready descriptors, and invokes the appropriate handler for each. One thread, thousands of connections, no context switching overhead.

Build an event-driven I/O reactor that supports: registering file descriptors for read/write/error events with associated callbacks, a single-threaded event loop that processes ready events, timer support for delayed and periodic actions, and non-blocking socket I/O. Then build an echo server on top of it that handles 10,000+ concurrent connections.

This challenge uses platform-specific syscalls. On Linux you will use `epoll_create1`, `epoll_ctl`, `epoll_wait`. On macOS you will use `kqueue`, `kevent`. The `libc` crate provides Rust bindings for both.

## Requirements

1. Implement a `Reactor` structure that wraps the OS multiplexer (`epoll` on Linux, `kqueue` on macOS). It must support creating the multiplexer instance and cleaning up on drop
2. Support event registration: add a file descriptor with interest in read, write, or both, with an associated callback (`Box<dyn FnMut(&mut Reactor, RawFd, EventKind)>`)
3. Support event deregistration: remove a file descriptor and its callbacks from the reactor
4. Support modifying interest: change a registered descriptor from read-only to read+write or vice versa
5. Implement the event loop: block on the OS multiplexer, iterate ready events, invoke callbacks. The loop runs until explicitly stopped or no descriptors are registered
6. Implement timer support: register a callback to fire after a delay (one-shot) or at regular intervals. On Linux use `timerfd_create`; on macOS use `EVFILT_TIMER` in kqueue
7. All sockets must be set to non-blocking mode. Handlers must correctly handle `EAGAIN`/`EWOULDBLOCK` by returning control to the event loop
8. Build an echo server using the reactor: accept connections, read data, echo it back. The server must handle partial reads and writes correctly
9. Implement backpressure: if a write would block, register for write-readiness and buffer the data. Resume writing when the OS signals writability
10. Write a load test client that opens N concurrent connections, sends data, and verifies echoed responses. Target: 10,000 concurrent connections on a system with sufficient file descriptor limits

## Hints

**Hint 1 -- Platform abstraction**: Define a trait `Poller` with methods `add`, `modify`, `remove`, and `wait`. Implement `EpollPoller` for Linux and `KqueuePoller` for macOS. Use `#[cfg(target_os = "linux")]` and `#[cfg(target_os = "macos")]` to compile the right one.

**Hint 2 -- Callback ownership**: Store callbacks in a `HashMap<RawFd, Box<dyn FnMut(...)>>`. The reactor owns all callbacks. When an event fires, remove the callback from the map, call it, then re-insert it. This avoids borrowing the map while a callback (which takes `&mut Reactor`) executes.

**Hint 3 -- Non-blocking accept loop**: When the listener is readable, call `accept` in a loop until it returns `EAGAIN`. Each `accept` call may return multiple connections (especially under load). Set each accepted socket to non-blocking immediately.

**Hint 4 -- Write buffering**: Maintain a per-connection `VecDeque<u8>` write buffer. On read, append echoed data to the buffer. Attempt to write immediately. If `EAGAIN`, register for write-readiness. When writable, drain the buffer. When the buffer is empty, deregister write interest.

**Hint 5 -- File descriptor limits**: To test 10K connections, increase the limit: `ulimit -n 65536`. The default on most systems is 1024. The benchmark should check this and warn if insufficient.

## Acceptance Criteria

- [ ] The reactor correctly multiplexes I/O across multiple concurrent connections using epoll or kqueue
- [ ] Callbacks fire on the correct events: read-ready, write-ready, or error
- [ ] Timer callbacks fire at the specified delay with reasonable accuracy (within 10ms for one-shot, within 5% for periodic)
- [ ] The echo server correctly echoes data from all connected clients simultaneously
- [ ] Partial reads and writes are handled: no data loss when a single read/write does not transfer all bytes
- [ ] Backpressure works: slow clients do not cause the server to block or lose data
- [ ] The server handles 10,000 concurrent connections without crashing or leaking file descriptors
- [ ] Event deregistration works: closing a connection removes it from the reactor cleanly
- [ ] The reactor shuts down gracefully when stopped, closing all registered descriptors
- [ ] No unsafe code beyond the libc FFI calls, and all FFI calls have safety comments
- [ ] Memory usage stays bounded: no per-event allocations in the hot path

## Key Concepts

**Reactor pattern**: A design pattern where a single thread waits for events on multiple I/O sources and dispatches handlers. The "reactor" reacts to I/O readiness by invoking registered callbacks. This is the foundation of event-driven programming and the basis for async runtimes like Tokio.

**Level-triggered vs. edge-triggered**: Level-triggered (default epoll, kqueue) notifies as long as the condition is true -- a readable socket triggers on every `epoll_wait` until drained. Edge-triggered notifies only on state change -- you must drain completely or you miss events. Edge-triggered is more efficient but harder to use correctly.

**Non-blocking I/O and EAGAIN**: A non-blocking socket returns `EAGAIN` when an operation would block. This is not an error -- it means "try again later." The reactor loop handles this by returning to `epoll_wait`/`kevent`, which will notify when the socket is ready again.

**Backpressure**: When a client reads slowly, the kernel's TCP send buffer fills up. Further writes return `EAGAIN`. The server must buffer unsent data and register for write-readiness to drain it when the client catches up. Without backpressure handling, the server either blocks (defeating the purpose of non-blocking I/O) or drops data.

**The C10K problem**: Handling 10,000 concurrent connections was a significant challenge in the early 2000s. Thread-per-connection models failed at this scale due to memory and context-switching overhead. Event-driven architectures using epoll/kqueue solved it by handling all connections in a single thread.

## Research Resources

- [The C10K Problem (Dan Kegel)](http://www.kegel.com/c10k.html) -- the seminal article on scaling to 10,000 connections
- [epoll(7) man page](https://man7.org/linux/man-pages/man7/epoll.7.html) -- Linux epoll interface documentation
- [kqueue(2) FreeBSD man page](https://www.freebsd.org/cgi/man.cgi?kqueue) -- kqueue interface documentation
- [libc crate documentation](https://docs.rs/libc/latest/libc/) -- Rust FFI bindings for epoll and kqueue syscalls
- [Scalable I/O: Events vs. Multithreading-based (Doug Schmidt)](https://www.dre.vanderbilt.edu/~schmidt/PDF/reactor-siemens.pdf) -- the original reactor pattern paper
- [Epoll is fundamentally broken (Marek Majkowski, Cloudflare)](https://blog.cloudflare.com/epoll-is-fundamentally-broken-1-2/) -- practical epoll pitfalls and edge cases
- [Building a Simple Event Loop from Scratch (Rust)](https://cfsamsonbooks.gitbook.io/epoll-kqueue-iocp-explained/) -- walkthrough of cross-platform event loops
- [mio: Metal I/O library for Rust](https://docs.rs/mio/latest/mio/) -- study its source for production-grade epoll/kqueue abstractions
- [timerfd_create(2) man page](https://man7.org/linux/man-pages/man2/timerfd_create.2.html) -- Linux timer file descriptors for event loop integration
