# 27. Build a Container Runtime

**Difficulty**: Insane

## The Challenge

Build an OCI-compatible container runtime in Rust. Containers are not magic — they are a combination of Linux kernel features: namespaces for isolation, cgroups for resource limits, and filesystem tricks for root jail. You will implement all of these from scratch using raw system calls.

Your runtime will take a root filesystem (extracted OCI image layer) and a command, then create an isolated process that sees its own PID namespace, its own network stack, its own mount table, and its own hostname. From inside the container, the process believes it is the only thing running on the machine. From outside, the host controls CPU, memory, and PIDs via cgroups v2.

This is how Docker, Podman, and Kubernetes work at the lowest level. Building this yourself removes the abstraction and forces you to understand every syscall, every capability, and every security boundary. You will also encounter Rust's unsafe FFI boundary with Linux syscalls, and you will design safe abstractions over inherently unsafe operations.

## Acceptance Criteria

### Namespace Isolation
- [ ] Create new PID namespace — container process sees itself as PID 1
- [ ] Create new UTS namespace — container can set its own hostname
- [ ] Create new MNT namespace — container has its own mount table
- [ ] Create new NET namespace — container has isolated network stack
- [ ] Create new USER namespace — map UID 0 inside to unprivileged UID outside (rootless containers)
- [ ] Use `clone3` or `unshare` + `fork` for namespace creation

### Filesystem Isolation
- [ ] Use `pivot_root` (not `chroot`) to change the root filesystem
- [ ] Mount `/proc` inside the container (new procfs for PID namespace)
- [ ] Mount `/dev` with minimal devices (`/dev/null`, `/dev/zero`, `/dev/urandom`)
- [ ] Support overlay filesystem (lower=image layers, upper=container writable layer, merged=rootfs)
- [ ] Clean up mounts on container exit

### Resource Limits (cgroups v2)
- [ ] Create cgroup for container under `/sys/fs/cgroup/`
- [ ] Set memory limit (`memory.max`) — OOM kills container, not host
- [ ] Set CPU limit (`cpu.max`) — throttle CPU usage
- [ ] Set PID limit (`pids.max`) — prevent fork bombs
- [ ] Clean up cgroup on container exit

### Networking
- [ ] Create veth pair connecting container namespace to host bridge
- [ ] Assign IP address to container end of veth
- [ ] Configure default route inside container
- [ ] Enable NAT for outbound traffic (via iptables/nftables rules)

### Runtime Lifecycle
- [ ] Implement OCI lifecycle: `create` → `start` → `kill` → `delete`
- [ ] Store container state in `/run/mycontainer/<id>/`
- [ ] Support `exec` — run additional process in existing container namespace
- [ ] Signal forwarding — relay SIGTERM/SIGINT to container PID 1
- [ ] Wait for container exit and return exit code

### Safety and Correctness
- [ ] All unsafe syscall wrappers have safe Rust APIs with proper error handling
- [ ] Resource cleanup on panic (cgroups, mounts, network interfaces)
- [ ] Run test suite: fork bomb contained by pids.max, memory limit enforced, hostname isolated
- [ ] Works as unprivileged user (rootless via user namespaces)

## Starting Points

- **youki** (`containers/youki`): Production Rust OCI runtime. Study `crates/libcontainer/src/namespaces.rs` for namespace setup order, `crates/libcontainer/src/process/` for the parent-child synchronization protocol using pipes, and `crates/libcgroups/src/v2/` for cgroups v2 management.

- **runc** (`opencontainers/runc`): Reference OCI runtime in Go. Study `libcontainer/nsenter/` for the namespace entry sequence and `libcontainer/cgroups/fs2/` for cgroups v2. The Go code is readable and well-commented.

- **Linux man pages**: `namespaces(7)`, `clone(2)`, `unshare(2)`, `pivot_root(2)`, `cgroups(7)`, `veth(4)`, `mount_namespaces(7)`, `user_namespaces(7)`. These are the authoritative references.

- **"Containers from Scratch"** by Liz Rice: Conference talk and Go implementation that builds a container in ~100 lines. Good conceptual overview before diving into production details.

- **OCI Runtime Spec** (`opencontainers/runtime-spec`): The formal specification your runtime should conform to. Focus on `config.md` (container configuration) and `runtime.md` (lifecycle operations).

- **nix crate** (`nix-rust/nix`): Safe Rust wrappers for POSIX/Linux syscalls. Use `nix::sched::clone`, `nix::mount`, `nix::unistd::pivot_root` instead of raw `libc` calls where possible.

## Hints

1. The namespace creation order matters. Create USER namespace first (for rootless), then PID, MNT, UTS, NET. The child process needs to write `/proc/self/uid_map` and `/proc/self/gid_map` after USER namespace creation but before doing anything else.

2. Parent-child synchronization requires a pipe or socket pair. The parent creates namespaces, the child sets up the filesystem and cgroups, and they coordinate via messages on the pipe. Study youki's `InitProcess` and `ContainerProcess` for the protocol.

3. `pivot_root` requires that the new root and the old root are on different mount points. The standard trick: bind-mount the new root onto itself (`mount --bind newroot newroot`), then `pivot_root(newroot, oldroot)`, then `umount2(oldroot, MNT_DETACH)`.

4. For overlay filesystem: `mount -t overlay overlay -o lowerdir=/layers/1:/layers/0,upperdir=/writable,workdir=/work /merged`. The `lowerdir` stack is colon-separated, read-only. `upperdir` captures writes.

5. cgroups v2 is filesystem-based. Create a directory under the unified hierarchy, write limits to control files, then write the container PID to `cgroup.procs`. Clean up by removing the directory.

6. For networking, use `ip link add veth0 type veth peer name veth1`, move `veth1` into the container namespace with `ip link set veth1 netns <pid>`, assign addresses, and bring interfaces up. Consider using rtnetlink via the `netlink` crate instead of shelling out to `ip`.

7. Rust's `std::process::Command` creates processes with `fork+exec` which inherits all namespaces. For namespace isolation you need `clone3` with namespace flags, which means `unsafe` FFI. Wrap this in a safe `ContainerProcess::spawn()` API.

8. Test with a static binary (e.g., busybox compiled statically) as the container entrypoint. This avoids needing shared libraries in the container rootfs.

9. Implement cleanup as RAII guards: `CgroupGuard` that deletes the cgroup on drop, `MountGuard` that unmounts on drop, `NamespaceGuard` that cleans up network interfaces on drop.

10. For rootless containers, you need to map UIDs/GIDs. Write `0 <host_uid> 1` to `/proc/<pid>/uid_map` and set `deny` in `/proc/<pid>/setgroups` before writing `gid_map`. This is the most fiddly part — get it working first.

## Resources

- Linux `namespaces(7)` man page
- Linux `cgroups(7)` man page
- OCI Runtime Specification: https://github.com/opencontainers/runtime-spec
- youki source: https://github.com/containers/youki
- Liz Rice "Containers from Scratch": https://www.youtube.com/watch?v=8fi7uSYlOdc
