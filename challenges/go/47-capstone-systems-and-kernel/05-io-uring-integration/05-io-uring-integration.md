<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 4h
-->

# io_uring Integration

## The Challenge

Build a Go library that interfaces with Linux's io_uring subsystem, the modern high-performance asynchronous I/O interface that replaces the older `aio` and `epoll`-based approaches. io_uring uses shared memory ring buffers between userspace and kernel to submit and complete I/O operations with zero or minimal system call overhead, enabling millions of IOPS for storage-bound workloads. Your library must set up the io_uring instance via the `io_uring_setup` syscall, map the shared submission and completion ring buffers into userspace memory, support submitting batches of operations (read, write, fsync, accept, connect, send, recv) without syscalls, and integrate with Go's goroutine scheduler to provide an ergonomic async I/O API. This is a deep systems programming exercise that requires understanding kernel-userspace shared memory, memory barriers, and the io_uring protocol.

## Requirements

1. Implement `io_uring_setup(entries uint32, params *io_uring_params) (int, error)` that creates an io_uring instance, returning a file descriptor; map the SQ (submission queue) and CQ (completion queue) ring buffers into userspace using `mmap` on the io_uring fd with the offsets provided by the kernel in `io_uring_params`.
2. Implement submission queue entry (SQE) construction for these operations: `IORING_OP_READV` (vectored read), `IORING_OP_WRITEV` (vectored write), `IORING_OP_FSYNC`, `IORING_OP_ACCEPT`, `IORING_OP_CONNECT`, `IORING_OP_SEND`, `IORING_OP_RECV`, with proper field packing in the 64-byte SQE struct.
3. Implement submission: write SQEs into the submission ring at the current tail index, advance the tail with a store-release memory barrier, and optionally call `io_uring_enter` to notify the kernel (or rely on `IORING_SETUP_SQPOLL` for kernel-side polling).
4. Implement completion harvesting: read completion queue entries (CQEs) from the completion ring starting at the current head index using a load-acquire memory barrier, extract the result and user_data fields, and advance the head.
5. Implement linked SQEs: chain multiple SQEs together using the `IOSQE_IO_LINK` flag so they execute sequentially (e.g., write then fsync), with the chain aborting if any entry fails.
6. Implement fixed file and buffer registration: use `IORING_REGISTER_FILES` and `IORING_REGISTER_BUFFERS` to pre-register file descriptors and memory buffers with the kernel, enabling zero-copy I/O that avoids per-operation fd lookup and buffer mapping overhead.
7. Build a goroutine-friendly API: `Ring.ReadFile(fd, buf, offset) Future[int]` that submits an SQE, returns a future, and a background goroutine polls the CQ ring for completions, resolving futures when their corresponding CQEs arrive (matched by `user_data`).
8. Implement `IORING_SETUP_SQPOLL` mode where a kernel thread continuously polls the SQ for new entries, eliminating the `io_uring_enter` syscall entirely for submission; detect when the kernel poller has gone idle and wake it with `io_uring_enter`.

## Hints

- The io_uring SQE struct is 64 bytes with a complex union layout; define it in Go using a `[64]byte` array and write accessor methods, or use a struct with explicit field offsets via `unsafe.Offsetof`.
- Memory barriers in Go: use `atomic.StoreUint32` for store-release and `atomic.LoadUint32` for load-acquire on the ring head/tail indices.
- The SQ ring contains an indirection array: `sq_array[tail % entries]` contains the index of the SQE in the SQE array. This allows reordering submissions.
- CQE contains: `user_data uint64` (copied from the SQE for request identification), `res int32` (result, similar to a syscall return value), `flags uint32`.
- For the goroutine-friendly API, use a `map[uint64]chan int` keyed by `user_data` to dispatch completions to the correct waiting goroutine.
- `IORING_SETUP_SQPOLL` requires root or `CAP_SYS_NICE`; the kernel poller thread idles after `sq_thread_idle` milliseconds of inactivity.
- Test with a benchmark that performs random 4KB reads on a file: io_uring should significantly outperform `os.File.ReadAt` due to batching and reduced syscall overhead.

## Success Criteria

1. The io_uring instance is correctly set up with mapped SQ and CQ rings, verified by submitting a `NOP` operation and receiving the completion.
2. File read/write via io_uring produces correct results, verified by comparing with standard `os.File` operations on the same file.
3. Linked SQEs (write + fsync) execute in order and the fsync is skipped if the write fails.
4. Fixed file registration reduces per-operation latency by at least 10% compared to non-registered fds (benchmarked).
5. The goroutine-friendly API correctly resolves futures and supports 100 concurrent in-flight operations.
6. SQPOLL mode eliminates submission syscalls: a burst of 1,000 operations completes with zero `io_uring_enter` calls (verified via `/proc/self/io` or strace).
7. Random 4KB read throughput exceeds 500,000 IOPS on an NVMe SSD, or exceeds standard `os.File.ReadAt` by at least 3x.
8. All code runs on Linux 5.10+ and handles kernel version differences gracefully.

## Research Resources

- io_uring documentation -- https://kernel.dk/io_uring.pdf (Jens Axboe's original design document)
- "Efficient IO with io_uring" -- https://kernel.dk/io_uring.pdf
- liburing source code -- https://github.com/axboe/liburing -- C reference implementation
- Linux man page: `man 2 io_uring_setup`, `man 2 io_uring_enter`, `man 2 io_uring_register`
- Lord of the io_uring tutorial -- https://unixism.net/loti/
- Go `unsafe` and `syscall` packages for raw kernel interaction
