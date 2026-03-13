# 39. Build a Memory-Safe Kernel Module

**Difficulty**: Insane

## The Challenge

The Linux kernel is the most deployed piece of software on Earth — running on billions of devices
from embedded routers to cloud hypervisors — and it is written almost entirely in C, a language
that makes memory corruption trivially easy. Since 2021, the Rust-for-Linux project has been
building the infrastructure to write kernel modules in Rust, giving developers memory safety,
type safety, and fearless concurrency inside the kernel itself, where a single use-after-free
means a system crash or a root exploit. Your mission is to build a fully functional Linux kernel
module in Rust: a character device driver that userspace programs can open, read, write, and
ioctl, backed by safe Rust abstractions over the raw kernel C APIs. This module will maintain
internal state, expose entries in procfs for runtime introspection, handle concurrent access from
multiple processes, and respond to kernel events — all without a single line of unsafe code in
your business logic.

Writing kernel code in Rust is not like writing userspace Rust. There is no standard library —
`std` does not exist in kernelspace. There is no heap allocator by default — you must use the
kernel's `kmalloc`/`kfree` through Rust-for-Linux's `kernel::alloc` abstractions, and every
allocation can fail (there is no OOM killer to bail you out, and `GFP_KERNEL` allocations in
interrupt context are forbidden). There is no `println!` — you use `pr_info!` which writes to
the kernel ring buffer via `printk`. There are no threads — you have kernel workqueues,
tasklets, and softirqs. Panicking is catastrophic: a panic in the kernel means a kernel oops or
a full system panic, not a clean process exit. The Rust-for-Linux project provides safe
abstractions for many kernel APIs, but they are intentionally minimal and tightly scoped — you
will need to understand both the Rust abstractions and the underlying C APIs they wrap.

The character device driver you build is the classic entry point for kernel development: it
creates a `/dev/yourdevice` node that userspace programs interact with using standard file
operations (open, read, write, close) and ioctl commands for control operations. Your driver
will implement a ring buffer in kernelspace: writes append data, reads consume data (FIFO), and
ioctl commands control buffer size, query statistics, and flush the buffer. You will expose
buffer statistics (bytes written, bytes read, current fill level, number of opens) through a
procfs entry at `/proc/yourdevice_stats`. You will handle multiple simultaneous opens correctly,
with either shared or exclusive access modes. You will implement blocking I/O using kernel wait
queues so that a `read()` on an empty buffer blocks until data is available (or returns
`EAGAIN` in non-blocking mode). And you will support poll/select/epoll so that event-driven
userspace programs can efficiently wait for data availability. Every safety invariant will be
enforced by Rust's type system, and every interaction with the kernel's C API will be wrapped in
a safe abstraction layer that you document with `// SAFETY:` comments.

## Acceptance Criteria

### Development Environment
- [ ] A working Rust-for-Linux kernel build environment: a Linux kernel source tree (6.6+) with Rust support enabled (`CONFIG_RUST=y`)
- [ ] Rust toolchain configured for kernel development: `rustup component add rust-src` and the correct `rustc` version matching the kernel tree's requirements
- [ ] `make LLVM=1 rustavailable` passes in the kernel source tree, confirming the toolchain is compatible
- [ ] The module builds as an out-of-tree module using `make -C /path/to/kernel M=$(pwd)` or as an in-tree module
- [ ] A `Kbuild` file that specifies the Rust module: `obj-m := yourmodule.o` with appropriate configuration
- [ ] A QEMU or VM-based test environment where the module can be loaded and tested without risking the host kernel
- [ ] A Makefile or justfile with targets: `build`, `load` (`insmod`), `unload` (`rmmod`), `test`, `clean`

### Module Skeleton
- [ ] The module uses the `module!` macro from `kernel::prelude` to declare name, author, description, and license (`GPL`)
- [ ] Implement the `kernel::Module` trait with `init()` returning `Result<Self>` where `Self` contains all module state
- [ ] Implement `Drop` for the module to clean up all resources (device registration, procfs entries, allocations) when the module is unloaded
- [ ] The module prints a message to the kernel log on load (`pr_info!("module loaded\n")`) and unload
- [ ] The module compiles with zero warnings and zero `unsafe` blocks outside of clearly documented FFI boundary wrappers
- [ ] All error paths return proper kernel error codes (`ENOMEM`, `EINVAL`, `EFAULT`, `EBUSY`, etc.) using `kernel::error::Result`

### Character Device Registration
- [ ] Register a character device using `kernel::miscdev::Registration` (misc device) or direct `cdev` registration
- [ ] The device appears as `/dev/yourdevice` with configurable permissions (default: 0666)
- [ ] The device major/minor number is dynamically allocated (not hardcoded)
- [ ] Implement the `kernel::file::Operations` trait for the device, providing: `open`, `read`, `write`, `ioctl`, `release`, `poll`
- [ ] Each file operation receives a `File` reference and the operation-specific arguments, returning `Result<ReturnType>`
- [ ] The `open` callback allocates per-file state (a `FileState` struct tracking the file's read/write position and flags)
- [ ] The `release` callback frees per-file state and decrements the open count
- [ ] Multiple processes can have the device open simultaneously; per-device state is shared via `Arc<Mutex<DeviceState>>`

### Ring Buffer Implementation
- [ ] Implement a kernel-space ring buffer using `kernel::alloc::KVec` or a manually managed buffer with `kernel::alloc::flags::GFP_KERNEL` allocation
- [ ] The buffer has a configurable maximum capacity (default: 64KB, configurable via module parameter or ioctl)
- [ ] `write()` appends data to the ring buffer, returning the number of bytes written; if the buffer is full, it either blocks (in blocking mode) or returns `EAGAIN`
- [ ] `read()` consumes data from the ring buffer (FIFO order), returning the number of bytes read; if the buffer is empty, it either blocks or returns `EAGAIN`
- [ ] Handle partial reads and writes correctly: if the user requests 4096 bytes but only 100 are available, return 100
- [ ] The ring buffer uses head and tail indices with wrap-around arithmetic (modular indexing), not data copying
- [ ] Buffer memory is allocated with `GFP_KERNEL` during module init or first open, and freed on module unload
- [ ] Handle the `copy_from_user` / `copy_to_user` boundary correctly — use the Rust-for-Linux `UserSliceReader` and `UserSliceWriter` abstractions to safely transfer data between userspace and kernelspace

### Concurrency and Synchronization
- [ ] Per-device state (the ring buffer, statistics counters) is protected by a `kernel::sync::Mutex` or `kernel::sync::SpinLock`
- [ ] Multiple concurrent readers and writers are handled correctly — no data corruption under concurrent access
- [ ] Use `kernel::sync::CondVar` (or wait queues) to implement blocking reads: a reader on an empty buffer sleeps until a writer produces data
- [ ] Use `kernel::sync::CondVar` to implement blocking writes: a writer on a full buffer sleeps until a reader consumes data
- [ ] Wake up blocked readers when new data is written; wake up blocked writers when data is consumed
- [ ] Non-blocking mode (O_NONBLOCK) returns `EAGAIN` instead of blocking — check the file flags in the `File` reference
- [ ] Demonstrate deadlock-free operation under concurrent load: multiple threads writing, multiple threads reading, with buffer pressure

### Ioctl Interface
- [ ] Define ioctl command numbers using the `_IO`, `_IOR`, `_IOW`, `_IOWR` macros (or their Rust equivalents) with a unique magic number
- [ ] `IOCTL_GET_BUFSIZE`: returns the current buffer capacity
- [ ] `IOCTL_SET_BUFSIZE`: resizes the buffer (only allowed when the buffer is empty; returns `EBUSY` if data is present)
- [ ] `IOCTL_FLUSH`: discards all data in the buffer, resets head and tail to zero
- [ ] `IOCTL_GET_STATS`: copies a `DeviceStats` struct to userspace containing: `bytes_written: u64`, `bytes_read: u64`, `current_fill: u32`, `open_count: u32`, `overflow_count: u64`
- [ ] `IOCTL_SET_MODE`: sets the device mode — `MODE_FIFO` (default, single consumer), `MODE_BROADCAST` (all readers see all data)
- [ ] Validate ioctl command numbers and argument sizes — reject unknown commands with `ENOTTY`
- [ ] Handle the userspace pointer argument safely using `UserSlice` for reading ioctl data from userspace

### Procfs Interface
- [ ] Create a `/proc/yourdevice_stats` entry when the module loads, remove it on unload
- [ ] Reading the procfs entry outputs human-readable statistics: buffer size, fill level, total bytes read/written, open count, overflow count
- [ ] The procfs entry uses `kernel::seq_file` or the procfs registration API for correct sequential reading semantics
- [ ] Statistics are read under a lock to ensure a consistent snapshot
- [ ] The procfs file is read-only (permissions 0444)

### Poll/Select/Epoll Support
- [ ] Implement `poll` in the file operations to support `select()`, `poll()`, and `epoll_wait()` from userspace
- [ ] Return `POLLIN | POLLRDNORM` when the buffer has data available for reading
- [ ] Return `POLLOUT | POLLWRNORM` when the buffer has space available for writing
- [ ] Register the file with the wait queue using `poll_wait()` so that the kernel wakes the polling process when data availability changes
- [ ] A userspace test program using `epoll` can efficiently wait for data availability without busy-looping

### Module Parameters
- [ ] Declare a module parameter for default buffer size: `module_param!(buf_size, u32, 0644)` with a default of 65536
- [ ] Declare a module parameter for maximum simultaneous opens: `module_param!(max_opens, u32, 0644)` with a default of 16
- [ ] Parameters are readable via `/sys/module/yourmodule/parameters/buf_size` and `/sys/module/yourmodule/parameters/max_opens`
- [ ] Parameters are validated on module load: `buf_size` must be a power of two between 4096 and 16MB; `max_opens` must be between 1 and 256
- [ ] Invalid parameters cause module load to fail with `EINVAL` and a descriptive `pr_err!` message

### Error Handling and Safety
- [ ] Every `unsafe` block has a `// SAFETY:` comment explaining why the invariants are upheld
- [ ] No raw pointer dereferences in business logic — all pointer operations are encapsulated in the safe abstraction layer
- [ ] Every allocation failure is handled: `GFP_KERNEL` allocations return `Result`, propagated with `?`
- [ ] No panicking code paths: no `unwrap()`, no `expect()`, no indexing with `[]` (use `.get()` and handle `None`)
- [ ] If a resource allocation fails during `init()`, all previously allocated resources are freed before returning the error (RAII handles this automatically if types implement `Drop`)
- [ ] The module survives `rmmod` followed by immediate `insmod` with no resource leaks
- [ ] The module handles signals correctly: a blocked `read()` interrupted by a signal returns `EINTR`

### Userspace Test Suite
- [ ] A C or Rust userspace program that exercises all device functionality
- [ ] Test basic write/read round-trip: write "hello", read back "hello"
- [ ] Test FIFO ordering: write A, B, C; read back A, B, C in order
- [ ] Test partial reads: write 1000 bytes, read 100 bytes ten times
- [ ] Test blocking behavior: spawn a reader thread on an empty device, write from another thread, verify the reader unblocks and receives the data
- [ ] Test non-blocking mode: open with `O_NONBLOCK`, read from empty device, verify `EAGAIN`
- [ ] Test all ioctl commands: set buffer size, get stats, flush
- [ ] Test concurrent access: 4 writer threads and 4 reader threads, verify total bytes read equals total bytes written
- [ ] Test procfs output: read `/proc/yourdevice_stats` and verify statistics match expected values
- [ ] Test epoll: use `epoll_wait` to detect data availability, verify it wakes on write
- [ ] Test module unload with open file descriptors: verify the module waits or returns appropriate errors
- [ ] Stress test: continuous writes and reads for 60 seconds, verify no kernel warnings, no memory leaks (check `/proc/meminfo` before and after)

### Interrupt Handling (Stretch Goal)
- [ ] Register a virtual interrupt handler using a workqueue-triggered timer (simulating a hardware interrupt since most virtual environments do not have real hardware to interrupt on)
- [ ] The interrupt handler runs in atomic context: it must not sleep, must not allocate with `GFP_KERNEL`, and must complete quickly
- [ ] Use a bottom-half mechanism (workqueue or tasklet) to defer non-urgent processing from the interrupt handler to a sleepable context
- [ ] The interrupt handler writes a timestamp to the ring buffer, demonstrating data flow from interrupt context to userspace readers
- [ ] The top-half (interrupt handler) and bottom-half (workqueue) communicate via a lock-free mechanism (e.g., a simple flag or per-CPU variable) — spinlocks in interrupt context must use `spin_lock_irqsave` to prevent deadlocks with the same lock held outside interrupt context
- [ ] Demonstrate the interrupt-to-userspace pipeline: the timer fires, the interrupt handler queues work, the workqueue writes data to the ring buffer and wakes blocked readers, userspace receives the data

### Kernel Memory Debugging
- [ ] Run the module under KASAN (`CONFIG_KASAN=y`) and verify zero memory errors across all test scenarios
- [ ] Run under KMSAN (`CONFIG_KMSAN=y`) if available in your kernel version to detect use of uninitialized memory
- [ ] Run under lockdep (`CONFIG_PROVE_LOCKING=y`) and verify no locking order violations or potential deadlocks are reported
- [ ] After running the full test suite, verify no memory leaks by comparing `slabtop` output or `/proc/slabinfo` before and after loading/unloading the module 100 times
- [ ] Verify that all `GFP_KERNEL` allocations are paired with corresponding frees by tracing with `ftrace` or `trace-cmd`

### Documentation
- [ ] A header comment in the module source explaining the device's purpose, supported operations, and ioctl interface
- [ ] Document the ioctl command numbers and their argument types in a separate header-like Rust module that the userspace test can reference
- [ ] Document the procfs output format
- [ ] List all known limitations (e.g., maximum buffer size, no seek support, single-device instance)
- [ ] Document the QEMU testing setup: kernel configuration options required (`CONFIG_RUST=y`, `CONFIG_MODULES=y`, `CONFIG_PROC_FS=y`), initramfs contents, and QEMU invocation command

### Sysfs Attribute Interface
- [ ] Create a sysfs attribute under `/sys/class/misc/yourdevice/` that exposes the current buffer fill level as a readable attribute
- [ ] Create a writable sysfs attribute that allows flushing the buffer by writing "1" to it (equivalent to the FLUSH ioctl but accessible without opening the device)
- [ ] The sysfs attributes are registered during module initialization and removed on unload
- [ ] Sysfs read/write callbacks use `kernel::sysfs` abstractions (or the underlying `kobject` API wrapped in safe Rust)

## Starting Points

- **Rust-for-Linux project**: https://rust-for-linux.com/ — the official home of Rust kernel support. Start with the documentation overview, which explains how Rust integrates into the kernel build system, the available safe abstractions, and the project's design philosophy of "no unsafe in driver code"
- **Kernel module samples in Rust**: https://github.com/Rust-for-Linux/linux/tree/rust/samples/rust — the kernel source tree contains sample Rust modules including `rust_minimal.rs` (hello world), `rust_print.rs` (printing), and `rust_miscdev.rs` (misc character device). These are your most direct starting points — they show the exact macro invocations, trait implementations, and patterns blessed by the project
- **`kernel` crate documentation**: build the docs from the kernel tree with `make LLVM=1 rustdoc` and browse `Documentation/output/rust/`. The `kernel::prelude`, `kernel::miscdev`, `kernel::file`, `kernel::sync`, and `kernel::alloc` modules document every safe abstraction. Pay special attention to `kernel::file::Operations` — this is the trait your driver implements
- **"Writing Linux Kernel Modules in Rust" by the Rust-for-Linux team**: https://lore.kernel.org/rust-for-linux/ — the mailing list archives contain design discussions, code reviews, and explanations of why abstractions are shaped the way they are. Search for discussions about `file_operations`, misc device registration, and memory allocation
- **Linux Device Drivers, 3rd Edition (LDD3)**: https://lwn.net/Kernel/LDD3/ — the classic reference for Linux driver development, available free online. While in C, chapters 3 (Char Drivers), 5 (Concurrency and Race Conditions), 6 (Advanced Char Driver Operations including ioctl and poll), and 7 (Time, Delays, and Deferred Work) map directly to what you are building. Understand the C version, then translate to Rust abstractions
- **The `kernel::sync` module**: study the kernel crate's synchronization primitives. `Mutex<T>` in kernel Rust is backed by the kernel's `struct mutex`. `SpinLock<T>` wraps `spinlock_t`. `CondVar` wraps wait queues. These are not the `std::sync` types — they integrate with the kernel's lockdep deadlock detector and follow kernel locking rules (no sleeping while holding a spinlock)
- **Out-of-tree Rust kernel module template**: https://github.com/Rust-for-Linux/rust-out-of-tree-module — a minimal template for building Rust kernel modules outside the kernel source tree. This simplifies your development setup: you point it at a Rust-enabled kernel build directory and compile your module against it
- **QEMU for kernel development**: use QEMU with a minimal initramfs to test your module. Boot a custom kernel with Rust support, load your module with `insmod`, test it, unload with `rmmod`. This avoids risking your host system and enables rapid iteration. The kernel's `tools/testing/selftests/` directory shows how to build minimal test environments
- **`copy_from_user`/`copy_to_user` in Rust**: study how `kernel::uaccess::UserSlice` wraps these operations. In C, forgetting to use `copy_from_user` and directly dereferencing a user pointer is a security vulnerability (arbitrary kernel read/write). The Rust abstraction makes this mistake impossible by requiring all userspace data transfers to go through `UserSliceReader`/`UserSliceWriter`

## Hints

1. Start with the `rust-out-of-tree-module` template, not a full kernel source tree. Get a minimal "hello world" module loading in QEMU before adding any functionality. The build system setup is the first hurdle — once `insmod` prints your `pr_info!` message, you have a working foundation
2. The `module!` macro is required and must appear exactly once. It generates the `init_module` and `cleanup_module` C entry points that the kernel calls. Your `init` function receives no arguments and must return `impl kernel::Module`. The returned value is stored by the kernel and dropped when the module is unloaded — this is your RAII cleanup mechanism
3. For the character device, `kernel::miscdev::Registration` is the easiest path. Misc devices automatically allocate a minor number under major 10, create the `/dev/` node via udev, and handle most of the boilerplate. Your `Operations` trait implementation is where the real logic lives
4. The `kernel::file::Operations` trait has associated types and constants that control which operations are supported. Set `HAS_READ = true`, `HAS_WRITE = true`, `HAS_IOCTL = true`, `HAS_POLL = true`. Operations not marked as supported return `ENOTTY` or `EINVAL` automatically
5. For the ring buffer, power-of-two sizes simplify modular arithmetic: `index & (capacity - 1)` is equivalent to `index % capacity` but faster. Store `head` and `tail` as absolute positions (ever-increasing `u64`), and mask them when indexing into the buffer. The buffer is empty when `head == tail` and full when `tail - head == capacity`
6. Kernel Rust's `Mutex<T>` returns a guard from `.lock()` — this is familiar from `std::sync::Mutex`. However, kernel mutexes must not be held across operations that might sleep unexpectedly. In general, hold the mutex for the minimum time: lock, copy data, unlock, then do the `copy_to_user`
7. For blocking I/O, the pattern is: (a) acquire lock, (b) check if data is available, (c) if not, register on wait queue and release lock, (d) sleep, (e) when woken, reacquire lock and re-check (because of spurious wakeups), (f) repeat. Rust-for-Linux's `CondVar` encapsulates this pattern, but you must understand the underlying wait queue semantics
8. When implementing `poll`, you must call `poll_wait` with your wait queue even if data is currently available. The kernel uses this call to register the file in the epoll interest list. Then return the current readiness mask. The kernel will call `poll` again when the wait queue is signaled
9. The `UserSlice` API enforces the kernel/user boundary. You cannot just `memcpy` from a user pointer — the kernel must use special instructions that handle page faults gracefully (returning `EFAULT` instead of crashing). `UserSliceReader::read_all()` and `UserSliceWriter::write_slice()` do this safely
10. For ioctl, you need to define command numbers that encode the direction (read/write), type (magic number), number (command index), and size (argument struct size). In C this is `_IOR`, `_IOW`, etc. In Rust kernel code, use the equivalent macros from `kernel::ioctl`. The magic number should be unique to your driver (pick an unused value from `Documentation/userspace-api/ioctl/ioctl-number.rst`)
11. Procfs entries in Rust-for-Linux use callback-based registration. You provide a function that writes to a `seq_file` (a kernel abstraction for sequential file output). The function is called each time userspace reads the file. Use `seq_printf!` or the Rust equivalent to format your statistics
12. Module parameters in Rust-for-Linux are declared with the `module_param!` macro and stored as global statics. Access them via the generated getter function. Validate in `init()` — if validation fails, return `Err(EINVAL)` and the module load is aborted cleanly
13. For the resize ioctl, you must: acquire the mutex, verify the buffer is empty, allocate a new buffer (which may fail — `GFP_KERNEL` can return `ENOMEM`), copy the old metadata, swap in the new buffer, free the old one, release the mutex. If allocation fails after freeing the old buffer, you have lost data — always allocate first, then swap
14. Signal handling in blocking I/O: when a process receives a signal while blocked in your `read()`, the kernel sets `signal_pending(current)`. Your sleep function should check for this and return `EINTR`. The `CondVar::wait` abstraction may handle this automatically, but verify by testing with `kill -SIGINT` on a blocked reader
15. Test your module under memory pressure: set up a cgroup with limited memory, load the module, and exercise it. Verify that allocation failures in `write()` return `ENOMEM` gracefully rather than panicking or corrupting state. The kernel's fault injection framework (`/sys/kernel/debug/fail_page_alloc/`) can also force allocation failures for testing
16. Run `dmesg` after every test cycle and check for kernel warnings, BUG messages, or lockdep complaints. Enable `CONFIG_PROVE_LOCKING=y` in your test kernel to get deadlock detection. Enable `CONFIG_KASAN=y` for kernel address sanitizer to detect memory safety issues in any unsafe blocks
