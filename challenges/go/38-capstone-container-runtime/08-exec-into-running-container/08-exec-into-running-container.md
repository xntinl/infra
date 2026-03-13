# 8. Exec into Running Container

<!--
difficulty: insane
concepts: [nsenter, setns-syscall, namespace-file-descriptors, tty-allocation, pseudo-terminal, exec-in-namespace]
tools: [go, linux]
estimated_time: 2h
bloom_level: create
prerequisites: [section 38 exercises 1-7, linux namespace fd management, pseudo-terminal concepts]
-->

## Prerequisites

- Go 1.22+ installed
- Linux environment with root access
- Completed exercises 1-7 (full container lifecycle with namespaces, cgroups, overlayfs)
- Understanding of Linux namespace file descriptors (`/proc/<pid>/ns/*`)
- Familiarity with pseudo-terminal (PTY) concepts

## Learning Objectives

- **Create** an exec mechanism that enters all namespaces of a running container to spawn additional processes
- **Design** a namespace entry strategy using `setns(2)` via `/proc/<pid>/ns/*` file descriptors
- **Evaluate** the security implications of namespace entry and the differences between `nsenter` and `clone` approaches

## The Challenge

The `docker exec` command is one of the most-used container operations: it runs a new process inside an already-running container, sharing all of its namespaces. This is fundamentally different from `docker run`, which creates new namespaces. With `exec`, you open file descriptors to the target container's namespace files (`/proc/<pid>/ns/uts`, `/proc/<pid>/ns/pid`, `/proc/<pid>/ns/net`, etc.) and call `setns(2)` to join each one.

In this exercise, you will implement an `exec` subcommand for your container runtime. Given a container ID and a command, it will look up the container's init PID, open its namespace file descriptors, enter those namespaces, `chroot` or `pivot_root` into the container's filesystem, and execute the specified command. The new process shares the container's network, PID tree, hostname, and filesystem view.

The hard part in Go is that `setns(2)` must be called before the Go runtime starts additional threads, because namespace changes affect only the calling thread. The standard solution is to use a C constructor function (via cgo) that runs before `main()`, or to use the re-exec pattern where you `clone` a new process with `CLONE_PARENT` into the target namespaces. The `nsenter` package in runc uses the cgo constructor approach. You must also handle PTY allocation for interactive sessions -- when the user runs `exec -it /bin/sh`, you need to set up a pseudo-terminal.

## Requirements

1. Implement an `exec` subcommand that takes a container ID and command as arguments
2. Look up the container's init PID from the persisted state (exercise 7)
3. Open namespace file descriptors from `/proc/<pid>/ns/{uts,pid,mnt,net,ipc,user}` for the target container
4. Enter all target namespaces using `setns(2)` -- handle the Go threading constraint via re-exec or cgo
5. Change the root filesystem to match the container's root using `chroot` or reading the container's rootfs path
6. Execute the specified command inside the container's namespaces with proper environment variables
7. Implement `-it` flag for interactive TTY sessions using pseudo-terminal allocation
8. Set up proper PTY: allocate `/dev/ptmx`, set terminal size, and handle `SIGWINCH` for resize
9. Place the exec'd process in the same cgroup as the container
10. Handle the exec'd process exit code and propagate it to the caller

## Hints

- The simplest Go approach: use `exec.Command("/proc/self/exe", "nsenter", ...)` with `SysProcAttr` to join namespaces. The child re-execs itself with a special subcommand that calls `setns`.
- Use `unix.Setns(fd, nstype)` from `golang.org/x/sys/unix` for the setns system call.
- Open namespace fds before forking: `fd, _ := syscall.Open("/proc/<pid>/ns/mnt", syscall.O_RDONLY, 0)`.
- For PTY allocation, use `github.com/creack/pty` or `os.OpenFile("/dev/ptmx", ...)` with `ioctl` for terminal setup.
- Handle `SIGWINCH` to propagate terminal resize events to the PTY inside the container.
- The exec'd process should inherit the container's environment variables from the image config, augmented by any `-e` flags.

## Success Criteria

1. `exec <container-id> /bin/sh` opens a shell inside the running container
2. The exec'd process sees the container's filesystem, hostname, and network
3. The exec'd process appears in the container's PID namespace
4. Interactive mode (`-it`) provides a working terminal with proper line editing
5. Terminal resize events are propagated correctly
6. The exec'd process runs within the container's cgroup resource limits
7. Exit codes from the exec'd command are propagated to the caller
8. Multiple concurrent exec sessions into the same container work correctly

## Research Resources

- [setns(2) man page](https://man7.org/linux/man-pages/man2/setns.2.html) -- system call for entering existing namespaces
- [runc nsenter package](https://github.com/opencontainers/runc/tree/main/libcontainer/nsenter) -- reference cgo implementation for namespace entry
- [nsenter(1) man page](https://man7.org/linux/man-pages/man1/nsenter.1.html) -- userspace namespace entry tool
- [Pseudo-terminal concepts (pty(7))](https://man7.org/linux/man-pages/man7/pty.7.html) -- PTY architecture
- [creack/pty Go library](https://github.com/creack/pty) -- Go library for pseudo-terminal allocation

## What's Next

The next exercise builds full bridge networking with NAT, enabling containers to communicate with external networks and with each other through a shared bridge.

## Summary

- `exec` enters an existing container's namespaces rather than creating new ones
- `setns(2)` joins a namespace identified by a file descriptor from `/proc/<pid>/ns/*`
- Go's multi-threaded runtime requires the re-exec pattern or cgo constructor for namespace entry
- PTY allocation enables interactive terminal sessions inside the container
- The exec'd process must be placed in the same cgroup as the container for resource accounting
- Multiple concurrent exec sessions must be supported without interfering with each other
