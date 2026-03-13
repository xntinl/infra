# 1. Linux Namespaces: UTS and PID

<!--
difficulty: insane
concepts: [linux-namespaces, uts-namespace, pid-namespace, syscall-sysprocattr, clone-flags, process-isolation]
tools: [go, linux]
estimated_time: 2h
bloom_level: create
prerequisites: [sections 28-29, sections 33-34, linux fundamentals]
-->

## Prerequisites

- Go 1.22+ installed
- Linux environment (native or VM -- namespaces are Linux-only)
- Root or CAP_SYS_ADMIN capability
- Completed sections 28-29 (unsafe/cgo, code generation) or equivalent systems programming experience
- Familiarity with Linux process model and system calls

## Learning Objectives

- **Create** isolated processes using UTS and PID namespaces from Go
- **Design** a namespace entry mechanism using `syscall.SysProcAttr` and clone flags
- **Evaluate** the isolation guarantees provided by each namespace type

## The Challenge

Linux namespaces are the foundational building block of container runtimes. Every container you have ever run -- Docker, Podman, containerd -- uses namespaces to isolate processes from the host system. The UTS namespace isolates the hostname and domain name, while the PID namespace creates an independent process ID tree where the first process becomes PID 1.

In this exercise, you will write Go code that creates child processes in new UTS and PID namespaces. You will use `syscall.SysProcAttr` to set clone flags (`CLONE_NEWUTS`, `CLONE_NEWPID`) and observe the resulting isolation. Your child process will see a different hostname and will believe it is PID 1, even though the host sees it with a normal PID.

This is a critical first step toward building a container runtime. Understanding how Go interacts with Linux system calls at this level -- and the subtle threading issues that arise with Go's runtime and namespaces -- is essential for the exercises that follow.

The key difficulty lies in Go's multi-threaded runtime: `unshare(2)` affects only the calling thread, but Go may reschedule goroutines across threads. You must use `exec.Command` with `SysProcAttr` to create a new process rather than trying to manipulate namespaces in the current process.

## Requirements

1. Create a Go program that re-executes itself as a child process in new UTS and PID namespaces
2. Use `syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID` in `SysProcAttr.Cloneflags`
3. The child process must set its hostname to a user-provided value using `syscall.Sethostname`
4. The child process must verify it sees PID 1 for itself via `/proc/self/status` or `os.Getpid()`
5. Implement a "run" subcommand pattern: the binary calls itself with a different argument to distinguish parent from child
6. The parent must wait for the child to exit and propagate its exit code
7. Print diagnostic information showing the namespace isolation (hostname, PID, parent PID)
8. Handle errors gracefully -- detect when not running as root and print a clear message
9. Implement a `--hostname` flag for specifying the container hostname
10. Mount a new `/proc` inside the PID namespace so that `ps` shows only the container's processes

## Hints

- Use `os.Args[0]` to re-exec the current binary. A common pattern is `parent run <cmd>` re-execs as `parent child <cmd>`.
- `exec.Command` with `SysProcAttr` creates the child in new namespaces at `fork+exec` time, avoiding Go's threading issues.
- You must mount `/proc` inside the new PID namespace for tools like `ps` to work correctly. Use `syscall.Mount("proc", "/proc", "proc", 0, "")`.
- Check `os.Geteuid() != 0` to detect missing root privileges early.
- The child process inherits the parent's environment. Use environment variables or command-line arguments to pass configuration.
- Remember to unmount `/proc` before the child exits, or use `MS_REC | MS_PRIVATE` mount propagation.

## Success Criteria

1. Running the program as root creates a child with a different hostname
2. `hostname` inside the child returns the user-specified value
3. `os.Getpid()` inside the child returns 1
4. `ps aux` inside the child shows only the child's process tree
5. The host's hostname remains unchanged after the child exits
6. The program prints clear error messages when run without root
7. The parent correctly waits for and reports the child's exit status
8. `go vet` and `go build` succeed without warnings

## Research Resources

- [Linux namespaces man page (namespaces(7))](https://man7.org/linux/man-pages/man7/namespaces.7.html) -- comprehensive overview of all namespace types
- [Go syscall package documentation](https://pkg.go.dev/syscall) -- SysProcAttr and Cloneflags
- [Containers from Scratch (Liz Rice)](https://github.com/lizrice/containers-from-scratch) -- reference Go implementation
- [Linux Containers in 500 Lines of Code](https://blog.lizzie.io/linux-containers-in-500-loc.html) -- step-by-step container implementation
- [unshare(2) man page](https://man7.org/linux/man-pages/man2/unshare.2.html) -- system call details
- [clone(2) man page](https://man7.org/linux/man-pages/man2/clone.2.html) -- clone flags reference

## What's Next

In the next exercise, you will add mount namespace isolation and implement `pivot_root` to give the container its own root filesystem, building on the namespace foundation you have established here.

## Summary

- Linux namespaces provide process-level isolation for various system resources
- UTS namespace isolates hostname; PID namespace creates an independent process ID tree
- Go's `syscall.SysProcAttr` with `Cloneflags` enables namespace creation at fork+exec time
- The re-exec pattern (parent calls itself as child) works around Go's multi-threaded runtime
- PID namespace requires mounting a new `/proc` for process tools to function correctly
- This is the foundational mechanism that all container runtimes build upon
