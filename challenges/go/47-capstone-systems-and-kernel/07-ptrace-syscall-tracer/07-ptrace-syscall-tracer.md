<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 4h
-->

# ptrace Syscall Tracer

## The Challenge

Build a system call tracer in Go using the `ptrace` system call, similar to `strace`, that can attach to a running process or spawn a new one and intercept every system call it makes, logging the syscall name, arguments (decoded with type awareness), return value, and timing. Your tracer must handle multi-threaded programs (tracing all threads), decode arguments for major syscalls (open, read, write, mmap, socket, connect, exec), follow child processes created by fork/clone, and present output in a human-readable format with optional JSON output for machine consumption. This exercise requires deep understanding of the ptrace API, process state management, and the x86_64/arm64 syscall ABI.

## Requirements

1. Implement process spawning under ptrace: use `syscall.PtraceTraceme()` in the child (via `exec.Command` with `SysProcAttr.Ptrace = true`) and `syscall.Wait4` in the parent to catch the initial stop, then set ptrace options with `PTRACE_O_TRACESYSGOOD | PTRACE_O_TRACEFORK | PTRACE_O_TRACEVFORK | PTRACE_O_TRACECLONE | PTRACE_O_TRACEEXEC`.
2. Implement the syscall tracing loop: use `PTRACE_SYSCALL` to resume the tracee until the next syscall entry/exit, distinguish entry from exit using the `PTRACE_O_TRACESYSGOOD` flag (which sets bit 7 of the signal number), and read the syscall number and arguments from registers using `PTRACE_GETREGS`.
3. Decode the syscall number to its name using a mapping table for the target architecture (x86_64: RAX for syscall number, RDI/RSI/RDX/R10/R8/R9 for arguments; arm64: X8 for syscall number, X0-X5 for arguments).
4. Implement argument decoding for at least these syscalls with full type awareness: `openat` (decode dirfd, path string read from tracee memory, flags as OR'd constants, mode), `read`/`write` (fd, buffer preview read from tracee memory, count), `mmap` (addr, length, prot flags, map flags, fd, offset), `connect` (fd, sockaddr struct decoded as IP:port), `execve` (path, argv array, envp array read from tracee memory).
5. Read string and struct arguments from the tracee's memory using `PTRACE_PEEKDATA` to read words from the tracee's address space, reconstructing strings by reading until a null terminator.
6. Track timing: record the wall-clock time at syscall entry and exit to calculate per-syscall duration; optionally aggregate to show which syscalls consume the most time (similar to `strace -c`).
7. Handle multi-threaded tracees: when a new thread is created (detected via `PTRACE_EVENT_CLONE`), obtain the new thread's TID and add it to the set of traced threads, multiplexing output by TID.
8. Support attaching to an already-running process using `PTRACE_ATTACH`, tracing its syscalls, and detaching cleanly with `PTRACE_DETACH` on Ctrl+C.

## Hints

- `runtime.LockOSThread()` is mandatory in the tracer goroutine because ptrace operations are per-OS-thread, and Go's scheduler can migrate goroutines between threads.
- Use `syscall.PtraceGetRegs` and `syscall.PtraceSetRegs` to read/write the tracee's register state.
- On x86_64, at syscall entry: `Orig_rax` contains the syscall number; at syscall exit: `Rax` contains the return value. A return value of `-errno` indicates an error.
- `PTRACE_PEEKDATA` reads one `uintptr`-sized word at a time; to read a string, read words sequentially and scan for null bytes.
- For the summary mode (`-c`), maintain a `map[string]struct{ count int; totalTime time.Duration; errors int }` and print a formatted table on exit.
- Fork/clone events are delivered as `waitpid` status with `status >> 8 == (SIGTRAP | PTRACE_EVENT_CLONE << 8)`; the new thread's TID is obtained via `PTRACE_GETEVENTMSG`.
- Handle `ESRCH` errors gracefully: the tracee may exit between a wait and the next ptrace call.

## Success Criteria

1. Tracing `ls /tmp` outputs syscall entries for `openat`, `getdents64`, `write`, etc., with correct argument decoding (directory path visible in openat args).
2. String arguments (filenames, paths) are correctly read from the tracee's memory and displayed.
3. The tracer correctly follows `fork`/`clone` and traces syscalls in child threads, with output tagged by TID.
4. Syscall timing is accurate: the total time for `sleep 1` shows approximately 1 second spent in `nanosleep`.
5. The summary mode (`-c`) produces a table of syscall counts, times, and error counts matching the format of `strace -c`.
6. Attaching to a running process with `PTRACE_ATTACH` successfully traces its syscalls and `PTRACE_DETACH` on Ctrl+C allows the process to continue.
7. Socket address decoding correctly shows IP:port for `connect` calls in a `curl` trace.
8. The tracer handles the tracee exiting cleanly without panicking.

## Research Resources

- Linux ptrace man page -- `man 2 ptrace`
- strace source code -- https://github.com/strace/strace -- the reference implementation
- "Advanced Programming in the UNIX Environment" (Stevens & Rago, 2013) -- process control chapters
- x86_64 syscall ABI -- System V AMD64 ABI supplement
- Go `syscall` package ptrace functions -- https://pkg.go.dev/syscall
- Linux kernel syscall table -- https://filippo.io/linux-syscall-table/
