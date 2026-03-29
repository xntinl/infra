# 104. Namespace Isolation Sandbox

<!--
difficulty: advanced
category: containers-and-systems
languages: [go]
concepts: [linux-namespaces, pid-namespace, mount-namespace, network-namespace, uts-namespace, user-namespace, pivot-root, chroot, process-isolation, clone-flags]
estimated_time: 14-20 hours
bloom_level: evaluate
prerequisites: [go-concurrency, linux-process-model, mount-syscall, filesystem-structure, cgroups-basics, networking-fundamentals]
-->

## Languages

- Go 1.22+

## Prerequisites

- Go goroutines, channels, `os/exec`, and `syscall` package
- Linux process model: PID 1 semantics, process trees, orphan reaping
- Mount syscall: `mount`, `umount`, bind mounts, mount propagation (`MS_PRIVATE`, `MS_REC`)
- Linux filesystem structure: `/proc`, `/sys`, `/dev`, root filesystem layout
- Basic cgroups understanding (challenge 94 recommended as prerequisite)
- Networking fundamentals: network interfaces, IP addresses, routing tables
- **Linux required**: namespaces are a Linux kernel feature. macOS users must use a Linux VM or Docker container (`docker run --privileged -it ubuntu:latest`). Namespace operations require root or `CAP_SYS_ADMIN`
- Understanding of `clone(2)` system call flags

## Learning Objectives

- **Implement** process isolation using Linux namespaces: PID, mount, network, UTS, and user
- **Evaluate** the security boundaries provided by each namespace type and their limitations
- **Design** a filesystem isolation scheme using `pivot_root` that provides a clean root filesystem to the sandboxed process
- **Analyze** how user namespaces enable unprivileged container creation through UID/GID mapping
- **Apply** proper namespace setup ordering (user namespace first, then mount, then PID) to avoid permission issues
- **Implement** minimal `/proc` and `/dev` setup inside the isolated filesystem
- **Understand** how these primitives combine to form the foundation of container runtimes

## The Challenge

Linux namespaces are the isolation mechanism that makes containers work. Each namespace type isolates a specific kernel resource: PID namespaces give a process its own process ID space, mount namespaces give it a private filesystem view, network namespaces give it isolated network interfaces, and UTS namespaces give it its own hostname.

When Docker runs a container, it creates a new process in a set of new namespaces. Inside, the process sees PID 1 (itself), its own root filesystem, its own network stack, and its own hostname. Outside, the host kernel tracks the process under a different PID, with full visibility into all namespaces. The process thinks it is alone; the host knows better.

Build a process sandbox that uses Linux namespaces to isolate a child process. The sandbox must create: a PID namespace (the child sees itself as PID 1), a mount namespace (the child has a private filesystem), a network namespace (isolated network), a UTS namespace (custom hostname), and optionally a user namespace (root inside, unprivileged outside). Combine this with `pivot_root` to give the child a clean root filesystem.

This is the second pillar of container runtimes (after cgroups). Challenge 94 controls how much resources a process can use; this challenge controls what a process can see. Together, they form the core of `docker run`.

## Requirements

1. Implement PID namespace isolation: the child process sees itself as PID 1. It cannot see or signal processes outside its namespace. Mount `/proc` inside the namespace so `ps` works correctly
2. Implement mount namespace isolation: the child has a private view of the filesystem. Mount operations inside the sandbox do not affect the host. Set mount propagation to `MS_PRIVATE` recursively
3. Implement UTS namespace isolation: the child has a custom hostname set by the user. `hostname` inside the sandbox returns the configured value
4. Implement network namespace isolation: the child has an empty network stack (loopback only). No access to host network interfaces. Bring up the loopback interface inside the namespace
5. Implement user namespace isolation (optional but recommended): map UID 0 inside to the calling user's UID outside. This allows the sandbox to run without root privileges on the host
6. Implement filesystem isolation using `pivot_root`: prepare a root filesystem directory, set up necessary mounts (`/proc`, `/dev/null`, `/dev/zero`, `/dev/urandom`, `/dev/random`), call `pivot_root` to switch roots, and remove the old root
7. The sandbox CLI should accept: a root filesystem path, a hostname, and the command to run inside the sandbox
8. Implement proper cleanup: unmount all mounts inside the namespace on exit, handle errors during setup gracefully (tear down partial setup)
9. Write tests that verify: PID 1 visibility, hostname isolation, mount isolation (create a file inside that does not appear outside), and network isolation (host interfaces not visible)
10. Document the namespace setup order and explain why it matters

## Hints

**Hint 1 -- SysProcAttr clone flags**: Go's `exec.Cmd` supports namespace creation via `SysProcAttr.Cloneflags`. Set `syscall.CLONE_NEWPID | syscall.CLONE_NEWNS | syscall.CLONE_NEWUTS | syscall.CLONE_NEWNET`. For user namespaces, add `syscall.CLONE_NEWUSER` and set `UidMappings` / `GidMappings`.

**Hint 2 -- The /proc/self/exe re-exec pattern**: The sandbox process must perform setup (mounts, pivot_root) inside the new namespaces. Use the re-exec pattern: the parent creates a child with new namespaces, the child detects it is in the "init" role (via an environment variable), performs the setup, then execs the target command.

**Hint 3 -- pivot_root requirements**: `pivot_root(new_root, put_old)` requires both arguments to be mount points. Bind-mount the new root onto itself (`mount --bind rootfs rootfs`) to satisfy this. After pivot, `umount` and `rmdir` the `put_old` directory. Then `chdir("/")`.

**Hint 4 -- /proc mount**: Inside the PID namespace, mount a new `/proc`: `mount("proc", "/proc", "proc", 0, "")`. This gives the sandboxed process a correct view of its own PID namespace. Without this, `/proc` shows the host's processes.

**Hint 5 -- User namespace UID mapping**: Write to `/proc/self/uid_map` and `/proc/self/gid_map` to map UID 0 inside to the calling user's UID outside. You must also write `deny` to `/proc/self/setgroups` before writing `gid_map`. Format: `"0 1000 1"` maps inside UID 0 to outside UID 1000, with a range of 1.

## Acceptance Criteria

- [ ] PID namespace works: `ps aux` inside the sandbox shows only the sandboxed processes, starting from PID 1
- [ ] Mount namespace works: creating a file inside the sandbox's filesystem does not create it on the host
- [ ] UTS namespace works: `hostname` inside returns the configured hostname, not the host's
- [ ] Network namespace works: `ip link` inside shows only the loopback interface, not host interfaces
- [ ] `pivot_root` works: the sandboxed process sees the provided rootfs as `/`, the host's filesystem is not accessible
- [ ] `/proc` is correctly mounted inside the namespace and reflects the PID namespace
- [ ] Minimal `/dev` entries work: `/dev/null`, `/dev/zero`, `/dev/urandom` are available inside the sandbox
- [ ] Cleanup works: all mounts are undone after the sandboxed process exits
- [ ] The tool handles missing rootfs gracefully with a clear error message
- [ ] User namespace (if implemented): the sandbox runs without root on the host, with UID 0 inside

## Key Concepts

**PID namespace**: Creates an isolated process ID space. The first process in a new PID namespace gets PID 1 and acts as an init process -- it reaps orphaned children. Processes inside cannot see or signal processes outside the namespace. The host can see all processes.

**Mount namespace**: Gives a process a private view of the mount table. Mounts performed inside are invisible outside (and vice versa, if propagation is set to `MS_PRIVATE`). This is what allows containers to have their own root filesystem without affecting the host.

**UTS namespace**: Isolates the hostname and domain name. Inside the namespace, `sethostname` changes only the namespace's hostname. Trivial but important for container identity.

**Network namespace**: Creates an isolated network stack: interfaces, routing table, firewall rules, sockets. A new network namespace starts with only a loopback interface. To connect it to the host, create a veth pair (one end in the namespace, one on the host) -- this is covered in challenge 122.

**User namespace**: Maps UIDs/GIDs between the namespace and the host. Inside, a process can be UID 0 (root) while being an unprivileged user on the host. This enables rootless containers. The kernel checks the outer UID for permission checks on resources outside the namespace.

**pivot_root vs chroot**: `chroot` changes the root directory but leaves the process in the same mount namespace -- it can escape with file descriptor tricks. `pivot_root` atomically swaps the root filesystem and the old root, then the old root can be unmounted entirely. This is strictly more secure and is what container runtimes use.

## Research Resources

- [Linux namespaces man page: namespaces(7)](https://man7.org/linux/man-pages/man7/namespaces.7.html) -- overview of all namespace types
- [Linux namespaces man page: clone(2)](https://man7.org/linux/man-pages/man2/clone.2.html) -- clone flags for namespace creation
- [pivot_root(2) man page](https://man7.org/linux/man-pages/man2/pivot_root.2.html) -- filesystem root switching
- [user_namespaces(7)](https://man7.org/linux/man-pages/man7/user_namespaces.7.html) -- UID/GID mapping and capabilities
- [Containers from Scratch (Liz Rice, GopherCon)](https://www.youtube.com/watch?v=8fi7uSYlOdc) -- excellent talk building a container in Go step by step
- [Linux Containers in 500 Lines of Code](https://blog.lizzie.io/linux-containers-in-500-loc.html) -- minimal container implementation walkthrough
- [runc namespace setup source](https://github.com/opencontainers/runc/blob/main/libcontainer/nsenter/) -- production Go namespace code
- [Namespaces in Operation (LWN, 8-part series)](https://lwn.net/Articles/531114/) -- deep dive into each namespace type
- [Go syscall.SysProcAttr documentation](https://pkg.go.dev/syscall#SysProcAttr) -- Cloneflags, UidMappings, GidMappings
