<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 2h
-->

# Direct System Calls

## The Challenge

Build a Go library that bypasses the Go standard library and invokes Linux system calls directly using the `syscall` and `unsafe` packages, implementing core file I/O, memory management, and process control operations from scratch. This exercise strips away every abstraction to reveal how programs actually interact with the kernel. You must implement `open`, `read`, `write`, `close`, `mmap`, `mprotect`, `fork` (via `clone`), `exec`, `wait`, `pipe`, `dup2`, `getpid`, `kill`, and `socket`/`bind`/`listen`/`accept` without using any `os`, `io`, `net`, or higher-level Go packages. You will work directly with file descriptors, raw memory pointers, process IDs, and signal numbers, understanding the exact contract between userspace and kernel.

## Requirements

1. Implement file operations using raw syscalls: `Open(path string, flags int, mode uint32) (int, error)`, `Read(fd int, buf []byte) (int, error)`, `Write(fd int, buf []byte) (int, error)`, `Close(fd int) error`, `Lseek(fd int, offset int64, whence int) (int64, error)`, translating errno values to Go errors.
2. Implement memory mapping: `Mmap(addr uintptr, length uintptr, prot int, flags int, fd int, offset int64) (uintptr, error)` and `Munmap(addr uintptr, length uintptr) error`; demonstrate mapping a file into memory, modifying it, and observing the changes persisted to disk.
3. Implement `Mprotect(addr uintptr, length uintptr, prot int) error` and demonstrate changing a memory region from read-write to read-only, then attempting a write to trigger a SIGSEGV (caught via signal handler).
4. Implement process creation using `Clone(flags uintptr, childStack uintptr) (int, error)` or `Fork() (int, error)` via `syscall.Syscall`, and `Execve(path string, argv []string, envp []string) error`; demonstrate spawning a child process that runs `/bin/echo`.
5. Implement `Waitpid(pid int, options int) (int, int, error)` that returns the child's PID and exit status, correctly parsing the wait status bitmask using macros `WIFEXITED`, `WEXITSTATUS`, `WIFSIGNALED`, `WTERMSIG`.
6. Implement pipe and I/O redirection: `Pipe() (int, int, error)` returns two file descriptors, and `Dup2(oldfd, newfd int) error` redirects one fd to another; demonstrate piping output of one process to the input of another (equivalent to shell `cmd1 | cmd2`).
7. Implement a minimal TCP server using raw socket syscalls: `Socket(domain, typ, protocol int) (int, error)`, `Bind(fd int, addr *SockaddrInet4) error`, `Listen(fd int, backlog int) error`, `Accept(fd int) (int, *SockaddrInet4, error)`, and demonstrate a working echo server.
8. All syscall wrappers must handle `EINTR` (interrupted system call) by retrying automatically, and must convert raw errno values to descriptive Go error types.

## Hints

- Use `syscall.Syscall`, `syscall.Syscall6`, and `syscall.RawSyscall` for invoking system calls; the syscall numbers are in `syscall` package constants (e.g., `syscall.SYS_OPEN`).
- `unsafe.Pointer` and `unsafe.Sizeof` are essential for passing struct pointers to syscalls.
- For `Mmap`, the returned pointer is a `uintptr`; convert it to a `[]byte` using `unsafe.Slice((*byte)(unsafe.Pointer(addr)), length)`.
- `Fork` in Go is dangerous because the Go runtime is multi-threaded; the child process inherits a single thread while mutexes may be in an inconsistent state. Call `Execve` immediately after `Fork` to replace the process image.
- For `SockaddrInet4`, you need to construct the `sockaddr_in` struct manually with `AF_INET`, port in network byte order (`htons`), and the IP address as a 4-byte array.
- `EINTR` handling: wrap each syscall in a loop that retries if the return value is `syscall.EINTR`.
- Test on Linux (this exercise is Linux-specific); use Docker or a VM if developing on macOS.

## Success Criteria

1. File operations work end-to-end: open a file, write "hello", seek to beginning, read it back, and close it -- all without using `os.File`.
2. Memory mapping a file, modifying a byte via the mapped pointer, and unmapping results in the change being visible when reading the file.
3. `Mprotect` to read-only followed by a write causes a signal that is caught and reported (not a crash).
4. Fork+exec successfully spawns `/bin/echo hello` and `Waitpid` returns exit status 0.
5. Pipe+dup2 correctly pipes the output of `echo hello` into `cat`, reading "hello" from the pipe.
6. The raw socket echo server accepts a TCP connection, echoes received data, and closes the connection.
7. All syscall wrappers correctly handle `EINTR` and translate errno to Go errors.
8. All code compiles and runs on Linux (x86_64 or arm64).

## Research Resources

- Linux man pages: `man 2 open`, `man 2 mmap`, `man 2 clone`, `man 2 socket` -- https://man7.org/linux/man-pages/
- "Linux System Programming" (Love, 2013) -- comprehensive reference
- Go `syscall` package source code -- https://cs.opensource.google/go/go/+/refs/tags/go1.22.0:src/syscall/
- Go `unsafe` package documentation -- https://pkg.go.dev/unsafe
- "Advanced Programming in the UNIX Environment" (Stevens & Rago, 2013)
- Linux kernel syscall table -- https://filippo.io/linux-syscall-table/
