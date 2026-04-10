<!--
type: reference
difficulty: advanced
section: [07-systems-and-os]
concepts: [blocking-io, select, poll, epoll, io_uring, AIO, POSIX-AIO, Linux-AIO, kqueue, Go-netpoller, Tokio, submission-queue, completion-queue, fixed-buffers]
languages: [go, rust]
estimated_reading_time: 75 min
bloom_level: analyze
prerequisites: [syscall-interface, virtual-memory-and-paging]
papers: [Axboe2019-io_uring, Welsh2001-SEDA]
industry_use: [TiKV-io_uring, NGINX, Node.js-libuv, Tokio, RocksDB, Scylla]
language_contrast: high
-->

# I/O Models

> The choice of I/O model sets the ceiling on your system's achievable throughput and
> latency floor; io_uring changes the equation by eliminating per-operation kernel
> transitions entirely.

## Mental Model

There are two fundamental questions in I/O: "when is data ready?" and "how do I move it?"
Every I/O model answers these questions differently.

**Blocking I/O** (the default): your thread calls `read()`, the kernel suspends the thread
until data arrives, then wakes it. Simple, but one thread per concurrent connection — a
dead end at scale.

**select/poll**: your thread registers interest in many file descriptors, asks the kernel
"which ones are ready?", gets a list back, then reads each. The kernel call itself is
synchronous — you block waiting for *any* fd to become ready. `select` is O(n) in the
number of monitored fds; `poll` is the same but with a more sensible API. Both fail at
10,000+ connections because of the O(n) scan overhead.

**epoll** (Linux) / **kqueue** (BSD/macOS): the kernel maintains a persistent interest
list. You register/deregister fds via `epoll_ctl`, then call `epoll_wait` to get a
batch of ready events. The kernel uses a red-black tree internally (O(log n) for registration,
O(1) per event). This is the foundation of every high-performance event loop: nginx,
Node.js/libuv, Go's netpoller, Java's NIO.

**io_uring** (Linux 5.1+): eliminates the remaining kernel transitions. You submit I/O
operations by writing descriptors to a ring buffer in shared memory (no syscall for
submission, if the kernel thread is polling). Completions appear in a second ring buffer
you read without a syscall. With registered files and buffers, even the pointer indirections
are removed. The result: a `read`+`write` pipeline that was 6 syscalls becomes 0 syscalls
when the ring is busy. Jens Axboe (Linux block layer maintainer) designed and implemented
it; RocksDB, ScyllaDB, and TiKV have migrated hot paths to it.

**Linux AIO (libaio)**: the predecessor to io_uring. Only works on O_DIRECT files (bypasses
page cache). Low-level API (`io_submit`, `io_getevents`). Limited to 64-512 concurrent
operations by default. Obsoleted by io_uring for new code.

The mental model shift with io_uring is from "submit a syscall, get a result" to "post a
work item to a queue, poll the completion queue." This matches how NVMe storage, RDMA, and
GPU compute actually work — asynchronous queues all the way down.

## Core Concepts

### epoll Mechanics

```
epoll_create1(0) → epfd
epoll_ctl(epfd, EPOLL_CTL_ADD, fd, &event) → register interest
epoll_wait(epfd, events, maxevents, timeout) → wait for readiness
read(fd, buf, n) → actually read data (may still block briefly)
```

epoll uses **edge-triggered** (EPOLLET) or **level-triggered** (default) modes:
- Level-triggered: `epoll_wait` returns an fd as long as it has data. Easier to use;
  can cause repeated wake-ups if the consumer is slow.
- Edge-triggered: `epoll_wait` returns an fd only when its state changes (new data
  arrives). Requires reading until EAGAIN to drain all buffered data. Used by nginx
  and Go's netpoller for efficiency.

### io_uring Architecture

io_uring uses two ring buffers in shared memory between the kernel and userspace:

```
Submission Queue (SQ): userspace writes SQEs (Submission Queue Entries)
Completion Queue (CQ): kernel writes CQEs (Completion Queue Entries)
```

Each SQE describes one operation: opcode (`IORING_OP_READ`, `IORING_OP_WRITE`,
`IORING_OP_ACCEPT`, `IORING_OP_RECV`, etc.), file descriptor, buffer, length, offset,
and a `user_data` field for correlation. The kernel processes SQEs asynchronously.
Each completion posts a CQE with the result and the same `user_data`.

Key features for eliminating syscalls:
- **SQPOLL mode**: a kernel thread polls the SQ for new submissions. After a `io_uring_enter`
  to arm the kernel thread, subsequent submissions require zero syscalls.
- **Registered files**: pre-register an array of file descriptors with the ring. SQEs
  reference fds by index instead of file descriptor number, eliminating the fdtable lookup.
- **Registered buffers**: pre-register memory buffers with the ring. Operations using
  these buffers skip the `get_user_pages` DMA setup per operation.
- **Fixed buffers**: provide buffers to the ring; the kernel fills them and hands them back.
- **Linked operations**: chain SQEs so operation B only starts when A completes.

## Implementation: Go

```go
package main

import (
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"
	"syscall"

	"golang.org/x/sys/unix"
)

// --- epoll-based event loop ---
// Go's net package uses this internally (the netpoller).
// This example shows the raw epoll API for educational purposes.

type epollLoop struct {
	epfd     int
	mu       sync.Mutex
	handlers map[int]func() // fd -> ready callback
}

func newEpollLoop() (*epollLoop, error) {
	epfd, err := unix.EpollCreate1(unix.EPOLL_CLOEXEC)
	if err != nil {
		return nil, fmt.Errorf("epoll_create1: %w", err)
	}
	return &epollLoop{
		epfd:     epfd,
		handlers: make(map[int]func()),
	}, nil
}

// Add registers an fd for EPOLLIN (read readiness) + EPOLLET (edge-triggered).
// Edge-triggered avoids repeated wake-ups when the consumer is slower than the producer.
func (e *epollLoop) Add(fd int, handler func()) error {
	e.mu.Lock()
	e.handlers[fd] = handler
	e.mu.Unlock()

	event := unix.EpollEvent{
		Events: unix.EPOLLIN | unix.EPOLLET,
		Fd:     int32(fd),
	}
	return unix.EpollCtl(e.epfd, unix.EPOLL_CTL_ADD, fd, &event)
}

// Remove unregisters an fd from epoll.
func (e *epollLoop) Remove(fd int) error {
	e.mu.Lock()
	delete(e.handlers, fd)
	e.mu.Unlock()
	return unix.EpollCtl(e.epfd, unix.EPOLL_CTL_DEL, fd, nil)
}

// Run blocks, dispatching ready events. Call from a dedicated goroutine.
func (e *epollLoop) Run() {
	events := make([]unix.EpollEvent, 128)
	for {
		n, err := unix.EpollWait(e.epfd, events, 100 /* ms timeout */)
		if err != nil {
			if err == syscall.EINTR {
				continue // interrupted by signal, retry
			}
			fmt.Fprintln(os.Stderr, "epoll_wait:", err)
			return
		}
		for i := 0; i < n; i++ {
			fd := int(events[i].Fd)
			e.mu.Lock()
			handler, ok := e.handlers[fd]
			e.mu.Unlock()
			if ok {
				go handler() // dispatch to a goroutine for non-blocking handling
			}
		}
	}
}

// --- Go netpoller demonstration ---
// Go's net package uses epoll/kqueue internally via the netpoller.
// Goroutines block on channel/net operations without blocking OS threads —
// the netpoller wakes up the goroutine when the socket is ready.

func goNetpollerDemo() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Println("listen:", err)
		return
	}
	addr := ln.Addr().String()
	defer ln.Close()

	var count atomic.Int64
	var wg sync.WaitGroup

	// Server: accepts connections, reads one byte, responds.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 1)
				c.Read(buf)
				c.Write([]byte{buf[0] + 1})
				count.Add(1)
			}(conn)
		}
	}()

	// Client: sends 10 concurrent requests.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(val byte) {
			defer wg.Done()
			conn, err := net.Dial("tcp", addr)
			if err != nil {
				return
			}
			defer conn.Close()
			conn.Write([]byte{val})
			buf := make([]byte, 1)
			conn.Read(buf)
		}(byte(i))
	}

	wg.Wait()
	fmt.Printf("Go netpoller: handled %d connections (goroutines, not threads)\n", count.Load())
}

// --- io_uring via direct syscall (simplified) ---
// For production use, prefer the github.com/iceber/iouring-go or
// github.com/pawelgaczynski/giouring bindings.
// This demonstrates the setup syscall to show the API shape.

func ioUringSetup(entries uint32) (int, error) {
	// io_uring_setup(2): creates the ring and maps SQ/CQ into the process.
	const SYS_IO_URING_SETUP = 425
	fd, _, errno := syscall.Syscall(
		SYS_IO_URING_SETUP,
		uintptr(entries),
		0, // params struct (nil = defaults)
		0,
	)
	if errno != 0 {
		return 0, fmt.Errorf("io_uring_setup: %w", errno)
	}
	return int(fd), nil
}

func main() {
	fmt.Println("=== I/O Models Demo ===\n")

	// --- epoll loop (educational) ---
	fmt.Println("--- Raw epoll ---")
	loop, err := newEpollLoop()
	if err != nil {
		fmt.Println("epoll unavailable:", err)
	} else {
		// Create a pipe; register the read end with epoll.
		r, w, _ := os.Pipe()
		go loop.Run()

		received := make(chan []byte, 1)
		loop.Add(int(r.Fd()), func() {
			buf := make([]byte, 64)
			n, _ := r.Read(buf)
			received <- buf[:n]
		})

		// Write to the pipe — epoll will wake the handler.
		time.Sleep(10 * time.Millisecond)
		w.Write([]byte("epoll works"))
		w.Close()

		select {
		case data := <-received:
			fmt.Printf("epoll received: %q\n", data)
		case <-time.After(time.Second):
			fmt.Println("epoll timeout")
		}
		loop.Remove(int(r.Fd()))
		r.Close()
		unix.Close(loop.epfd)
	}

	// --- Go netpoller (goroutine-per-connection) ---
	fmt.Println("\n--- Go netpoller (goroutines on epoll) ---")
	goNetpollerDemo()

	// --- io_uring setup ---
	fmt.Println("\n--- io_uring ring setup ---")
	ringFd, err := ioUringSetup(128)
	if err != nil {
		fmt.Println("io_uring_setup failed (requires Linux 5.1+):", err)
	} else {
		fmt.Printf("io_uring ring created: fd=%d, 128 SQE slots\n", ringFd)
		unix.Close(ringFd)
	}

	fmt.Println("\nDone.")
}
```

### Go-specific considerations

Go's net package wraps the platform's best event notification mechanism (epoll on Linux,
kqueue on BSD/macOS, IOCP on Windows) in the **netpoller**. When a goroutine calls
`conn.Read()` on an unready socket, the goroutine is parked (not the OS thread), and
the netpoller's epoll thread watches for readiness. When data arrives, the netpoller
wakes the goroutine. This is how Go achieves millions of concurrent connections with a
small thread pool — goroutines are the concurrency unit, not OS threads.

For io_uring in Go, two libraries are available: `iceber/iouring-go` (wraps liburing via
CGo) and `pawelgaczynski/giouring` (pure Go, direct syscalls). Neither is yet as mature
as Tokio's io_uring integration. The standard library does not use io_uring; Go's
netpoller remains epoll-based. For workloads that would benefit from io_uring (NVMe
storage with O_DIRECT, high-IOPS file servers), CGo + liburing is the current practical
path.

## Implementation: Rust

```rust
// Demonstrates epoll and io_uring in Rust.
//
// Add to Cargo.toml:
//   io-uring = "0.6"
//   nix = { version = "0.27", features = ["net", "event"] }
//   tokio = { version = "1", features = ["full"] }

use io_uring::{opcode, types, IoUring};
use std::os::unix::io::AsRawFd;
use std::net::TcpListener;
use std::io;

// --- epoll-based echo server (single-threaded, edge-triggered) ---
// Demonstrates the raw epoll API that underpins every high-performance server.

fn epoll_echo_server(port: u16) -> io::Result<()> {
    let listener = TcpListener::bind(format!("127.0.0.1:{port}"))?;
    listener.set_nonblocking(true)?;
    let listen_fd = listener.as_raw_fd();

    // Create epoll instance.
    let epfd = unsafe { libc::epoll_create1(libc::EPOLL_CLOEXEC) };
    if epfd < 0 {
        return Err(io::Error::last_os_error());
    }

    // Register the listening socket for EPOLLIN (new connection ready).
    let mut ev = libc::epoll_event {
        events: (libc::EPOLLIN | libc::EPOLLET) as u32,
        u64: listen_fd as u64,
    };
    unsafe { libc::epoll_ctl(epfd, libc::EPOLL_CTL_ADD, listen_fd, &mut ev) };

    println!("epoll echo server listening on :{port}");

    let mut events = vec![libc::epoll_event { events: 0, u64: 0 }; 128];
    let mut connected_fds: Vec<std::os::unix::net::UnixStream> = Vec::new();

    for _ in 0..3 {
        // epoll_wait: block until an event is ready (100ms timeout).
        let n = unsafe {
            libc::epoll_wait(epfd, events.as_mut_ptr(), events.len() as i32, 100)
        };
        for i in 0..n as usize {
            let fd = events[i].u64 as i32;
            if fd == listen_fd {
                // Accept new connection.
                let client_fd = unsafe { libc::accept(listen_fd, std::ptr::null_mut(), std::ptr::null_mut()) };
                if client_fd >= 0 {
                    unsafe { libc::fcntl(client_fd, libc::F_SETFL, libc::O_NONBLOCK) };
                    let mut ev = libc::epoll_event {
                        events: (libc::EPOLLIN | libc::EPOLLET) as u32,
                        u64: client_fd as u64,
                    };
                    unsafe { libc::epoll_ctl(epfd, libc::EPOLL_CTL_ADD, client_fd, &mut ev) };
                    println!("  accepted client fd={client_fd}");
                }
            } else {
                // Read data from client and echo it back.
                let mut buf = [0u8; 256];
                let n = unsafe { libc::read(fd, buf.as_mut_ptr() as *mut libc::c_void, buf.len()) };
                if n > 0 {
                    unsafe { libc::write(fd, buf.as_ptr() as *const libc::c_void, n as usize) };
                    println!("  echoed {} bytes on fd={fd}", n);
                } else {
                    unsafe { libc::epoll_ctl(epfd, libc::EPOLL_CTL_DEL, fd, std::ptr::null_mut()) };
                    unsafe { libc::close(fd) };
                }
            }
        }
    }

    unsafe { libc::close(epfd) };
    Ok(())
}

// --- io_uring: vectored file read ---
// Reads a file using io_uring IORING_OP_READV (vectored read).
// Demonstrates the submission/completion queue pattern.

fn iouring_read_file(path: &str) -> io::Result<Vec<u8>> {
    let file = std::fs::File::open(path)?;
    let file_size = file.metadata()?.len() as usize;

    // Create an io_uring instance with 8 SQE slots.
    let mut ring = IoUring::new(8)?;

    // Allocate a buffer large enough for the file.
    let mut buf = vec![0u8; file_size.min(4096)];

    // Build a readv operation: IORING_OP_READV on the file at offset 0.
    let iov = libc::iovec {
        iov_base: buf.as_mut_ptr() as *mut libc::c_void,
        iov_len: buf.len(),
    };

    let sqe = opcode::Readv::new(
        types::Fd(file.as_raw_fd()),
        &iov as *const libc::iovec,
        1,
    )
    .offset(0)
    .build()
    .user_data(0x42); // user_data is returned in the CQE for correlation

    // Submit the SQE to the kernel.
    unsafe {
        ring.submission()
            .push(&sqe)
            .map_err(|e| io::Error::new(io::ErrorKind::Other, format!("push SQE: {e}")))?;
    }
    ring.submit_and_wait(1)?; // wait for exactly 1 completion

    // Drain the completion queue.
    let cqe = ring
        .completion()
        .next()
        .ok_or_else(|| io::Error::new(io::ErrorKind::UnexpectedEof, "no CQE"))?;

    let bytes_read = cqe.result();
    if bytes_read < 0 {
        return Err(io::Error::from_raw_os_error(-bytes_read));
    }
    assert_eq!(cqe.user_data(), 0x42);

    buf.truncate(bytes_read as usize);
    Ok(buf)
}

// --- Tokio: async I/O using epoll under the hood ---
// Tokio's reactor wraps epoll on Linux; io_uring support via tokio-uring (separate crate).

#[tokio::main]
async fn tokio_demo() -> io::Result<()> {
    use tokio::io::{AsyncReadExt, AsyncWriteExt};
    use tokio::net::{TcpListener, TcpStream};

    let listener = TcpListener::bind("127.0.0.1:0").await?;
    let addr = listener.local_addr()?;

    let server = tokio::spawn(async move {
        // Accept one connection, read a byte, respond.
        let (mut conn, _) = listener.accept().await.unwrap();
        let mut buf = [0u8; 1];
        conn.read_exact(&mut buf).await.unwrap();
        conn.write_all(&[buf[0] + 1]).await.unwrap();
    });

    let mut client = TcpStream::connect(addr).await?;
    client.write_all(&[41]).await?;
    let mut response = [0u8; 1];
    client.read_exact(&mut response).await?;
    println!("Tokio response: {} (expected 42)", response[0]);

    server.await.unwrap();
    Ok(())
}

fn main() {
    println!("=== I/O Models (Rust) ===\n");

    // epoll server (runs briefly to demonstrate API).
    println!("--- epoll edge-triggered server ---");
    if let Err(e) = epoll_echo_server(0) {
        println!("epoll demo skipped: {e}");
    }

    // io_uring file read.
    println!("\n--- io_uring readv ---");
    match iouring_read_file("/proc/version") {
        Ok(data) => println!(
            "io_uring read {} bytes: {}",
            data.len(),
            String::from_utf8_lossy(&data).trim()
        ),
        Err(e) => println!("io_uring read failed (needs Linux 5.1+): {e}"),
    }

    // Tokio async I/O.
    println!("\n--- Tokio async I/O ---");
    let rt = tokio::runtime::Runtime::new().unwrap();
    if let Err(e) = rt.block_on(tokio_demo()) {
        println!("tokio demo error: {e}");
    }

    println!("\nDone.");
}
```

### Rust-specific considerations

The `io-uring` crate (from the Tokio project) provides a safe, idiomatic Rust API for
io_uring. The crate uses Rust's lifetimes to prevent use-after-submission bugs: once you
`push` an SQE referencing a buffer, the crate ensures the buffer lives until the CQE
is received. This is a significant safety improvement over the C liburing API, where
use-after-submission is a common source of data corruption.

`tokio-uring` (a separate crate from the standard `tokio`) provides a Tokio runtime
variant that uses io_uring as the event source. It requires an io_uring-enabled runtime
because io_uring's completion model differs from epoll's readiness model — you cannot
mix them in a single event loop transparently. Services that want io_uring for storage
with standard TCP networking must use tokio-uring or glue them at the application level.

For production Go and Rust services that need high-throughput file I/O, consider using
io_uring for the storage path while keeping epoll-based networking. TiKV (the RocksDB
storage engine in TiDB) does exactly this: io_uring for compaction and flush I/O,
standard async sockets for gRPC.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|----|------|
| Event loop | netpoller (epoll/kqueue, internal) | Tokio reactor (epoll on Linux) |
| Async model | Goroutines (implicit async) | async/await + futures |
| epoll access | `golang.org/x/sys/unix.EpollWait` | `libc::epoll_wait` or `nix` |
| io_uring | `iceber/iouring-go` or `giouring` (not stdlib) | `io-uring` crate (Tokio project) |
| io_uring + async | No mature integration | `tokio-uring` crate |
| Zero-copy networking | `sendfile(2)` via `syscall.Sendfile` | `nix::sys::sendfile::sendfile` |
| Buffer management | `sync.Pool` for reuse | `bytes` crate, `BytesMut` |
| Vectored I/O | `syscall.Write` with multiple iov | `io_uring::opcode::Writev` |
| SQPOLL mode | Not exposed in any Go library | Supported in `io-uring` crate |
| Production maturity | Very mature (netpoller stable since Go 1) | Tokio mature; tokio-uring experimental |

## Production War Stories

**TiKV's migration to io_uring**: TiKV, the storage layer of TiDB, processes NVMe storage
with RocksDB. In their initial async implementation using standard Linux AIO (libaio),
they were limited to ~64 outstanding operations and suffered from the O_DIRECT restriction.
After migrating the compaction and flush paths to io_uring with registered buffers and
SQPOLL mode, TiKV reported 40–60% higher IOPS on the same hardware and significantly
reduced CPU time spent in kernel I/O submit paths. The key win was eliminating per-
operation `io_submit` syscalls via SQPOLL — the submission loop became a pure memory
write.

**NGINX and epoll edge-triggered mode**: NGINX uses `EPOLLET` (edge-triggered) for its
epoll registrations. This means NGINX only gets a wake-up when new data arrives, not
repeatedly while data is available. The consequence: NGINX's `recv()` loop must read
until `EAGAIN` on each wake-up, otherwise data is silently buffered without future
wake-ups. This is a common bug in NGINX module development: calling `ngx_recv()` and
returning without draining to `EAGAIN` causes requests to stall indefinitely.

**Go netpoller and blocking calls in CGo**: Go's netpoller only intercepts Go network
operations. CGo calls that perform blocking I/O (e.g., a C library calling `read(2)`)
block the OS thread the goroutine is running on. This steals the thread from the
scheduler, forcing the runtime to create a new OS thread. A service with 1000 concurrent
CGo blocking I/O calls will have 1000+ OS threads, degrading scheduler efficiency. The
fix is to wrap blocking CGo calls with `runtime.LockOSThread()` and use a bounded goroutine
pool to limit concurrency.

**ScyllaDB and io_uring for networking**: ScyllaDB (a Cassandra-compatible database written
in C++) was among the first databases to use io_uring for TCP networking, not just storage.
By submitting `accept`, `recv`, and `send` operations through io_uring with registered
sockets, ScyllaDB eliminated the epoll+syscall-per-operation overhead. At 10 Gbps, the
savings from eliminating per-recv syscalls reduced CPU utilization by ~15% on their
benchmark workloads.

## Complexity Analysis

| Operation | Latency | Notes |
|-----------|---------|-------|
| `read(2)` (ready fd, no copy) | ~200–400 ns | Syscall + kernel buffer to user |
| `epoll_wait` (at least one ready) | ~200–300 ns | Syscall + kernel data copy |
| `epoll_ctl` (add/modify) | ~300–500 ns | Syscall + rb-tree insert |
| `io_uring` submit (SQPOLL, no syscall) | ~50–100 ns | Ring write only |
| `io_uring` CQE poll (no syscall) | ~50–100 ns | Ring read only |
| `io_uring_enter` (without SQPOLL) | ~300–500 ns | Syscall to kick kernel |
| `sendfile(2)` 64 KB | ~2–5 µs | Zero-copy kernel path |
| NVMe `pread` 4 KB (via io_uring) | ~50–200 µs | Device latency dominates |
| NVMe `pread` 4 KB (via libaio) | ~60–250 µs | Similar; overhead is io_uring vs AIO |
| TCP `send` 1 KB (epoll, loopback) | ~5–20 µs | Round-trip including send+recv |
| TCP `send` 1 KB (io_uring, loopback) | ~3–12 µs | ~30% better than epoll at high concurrency |

The io_uring advantage grows with concurrency: at low concurrency the syscall overhead
is negligible; at 10,000+ concurrent operations the saved syscalls become significant.

## Common Pitfalls

1. **Not draining edge-triggered fds to `EAGAIN`.** With `EPOLLET`, if you call `read()`
   once and return, the fd will not wake up again until new data arrives — even if there
   was more data in the kernel buffer. Always loop `read()` until `EAGAIN` on ET fds.

2. **Using SQPOLL without careful CPU pinning.** In SQPOLL mode, the kernel spawns a
   dedicated thread that polls the SQ. If that thread is not pinned to a CPU, it contends
   with application threads for CPU time. Pin the SQPOLL kernel thread to an isolated CPU
   with `IORING_SETUP_SQ_AFF` and `isolcpus` in the kernel command line.

3. **Submitting io_uring operations with stack-allocated buffers.** io_uring operations
   are asynchronous. If the buffer is on the stack and the function returns before the CQE
   arrives, the kernel writes to freed stack memory. Always ensure buffers outlive their
   operations, or use registered buffers that the ring owns.

4. **Mixing io_uring and standard epoll for the same fd.** An fd registered with io_uring
   and also added to an epoll set can cause unexpected behavior: both the io_uring kernel
   thread and the epoll consumer may try to handle the same readiness event. Use one
   mechanism per fd.

5. **Assuming Go's goroutines are free.** Each goroutine has a minimum stack of 2 KB
   (growing as needed). At 1 million goroutines, the stack memory alone is ~2 GB. More
   critically, goroutine creation and scheduling have overhead (~1 µs per goroutine start).
   For a 100,000 req/s server that creates one goroutine per request, that is 100 ms/s
   spent on goroutine creation alone. Use goroutine pools (e.g., `fasthttp`) for extreme
   request rates.

## Exercises

**Exercise 1** (30 min): Write a program using raw `epoll_wait` in Go (via
`golang.org/x/sys/unix`) that monitors 100 UDP sockets and prints statistics every
second: how many sockets had data, total bytes received, epoll_wait call count. Compare
the approach to a goroutine-per-socket implementation.

**Exercise 2** (2–4 h): Implement a single-threaded HTTP/1.1 echo server using
edge-triggered epoll in Rust with `libc::epoll_wait`. Handle concurrent connections with
a connection state machine (reading headers, reading body, sending response). Measure
maximum connections per second with `wrk`.

**Exercise 3** (4–8 h): Use the `io-uring` Rust crate to implement a file copy utility
that copies a 1 GB file using linked SQEs: `readv` → `writev` chains with a 64 KB buffer.
Measure throughput against `cp` and `sendfile`. Explore the effect of buffer count
(1 vs 4 vs 16 concurrent buffers in flight) on throughput.

**Exercise 4** (8–15 h): Implement an io_uring-based TCP proxy in Rust using `tokio-uring`
or `io-uring` directly. The proxy listens on port A, connects to upstream port B, and
forwards traffic bidirectionally with zero-copy: `splice(2)` for data passing between
the two sockets. Measure CPU usage and latency at 10,000 req/s, and compare against
a standard Tokio `TcpStream` proxy.

## Further Reading

### Foundational Papers

- Axboe, J. (2019). "Efficient I/O with io_uring." Kernel Recipes 2019. The design
  document for io_uring from its creator; explains SQ/CQ layout, SQPOLL, and registered
  buffers.
- Welsh, M., Culler, D., Brewer, E. (2001). "SEDA: An Architecture for Well-Conditioned,
  Scalable Internet Services." SOSP 2001. The staged event-driven architecture that
  epoll-based servers evolved toward.
- Liao, X. et al. (2008). "When Is Parallelism Not Enough? A Study of Scalable I/O for
  Internet Services." ICDCS 2008. Empirical analysis of epoll vs. thread-per-connection.

### Books

- Kerrisk, M. "The Linux Programming Interface" — Chapter 63 (Alternative I/O Models)
  covers `select`, `poll`, `epoll`, signal-driven I/O, and Linux AIO.
- Love, R. "Linux System Programming" (2nd ed.) — Chapter 4 (Advanced File I/O) covers
  `readv`/`writev`, `sendfile`, and Linux AIO.
- Gregg, B. "BPF Performance Tools" — Chapter 9 (Disk I/O) has BPF tools for measuring
  I/O latency that help tune io_uring parameters.

### Production Code to Read

- `tokio/tokio/src/io/driver/` — Tokio's epoll reactor implementation.
- `tokio-uring/src/` — io_uring runtime built on top of Tokio.
- `src/runtime/netpoll_epoll.go` in Go source — Go's netpoller for Linux.
- `linux/io_uring/io_uring.c` in the Linux kernel — the io_uring implementation.
- `tikv/tikv/components/engine_rocks/` — TiKV's io_uring integration with RocksDB.

### Conference Talks

- "io_uring: The New I/O API of the Linux Kernel" — Linux Storage and Filesystems
  Conference 2019. Jens Axboe's original presentation of io_uring.
- "Taming io_uring from Go" — GopherCon 2022. Current state of io_uring Go bindings.
- "Tokio and io_uring" — RustConf 2022. How tokio-uring differs from standard Tokio.
- "ScyllaDB's io_uring Journey" — P99 CONF 2021. Performance results from using io_uring
  for both storage and networking in a production database.
