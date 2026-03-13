# 2. Mount Namespace and Root Filesystem

<!--
difficulty: insane
concepts: [mount-namespace, pivot-root, rootfs, bind-mounts, mount-propagation, chroot-vs-pivot-root]
tools: [go, linux]
estimated_time: 2h
bloom_level: create
prerequisites: [section 38 exercise 1, linux filesystem concepts]
-->

## Prerequisites

- Go 1.22+ installed
- Linux environment with root access
- Completed exercise 1 (UTS and PID namespaces)
- Understanding of Linux filesystem hierarchy and mount points
- A minimal root filesystem (Alpine miniroot or debootstrap output)

## Learning Objectives

- **Create** mount namespace isolation with private mount propagation
- **Design** a root filesystem switch using `pivot_root` instead of `chroot`
- **Evaluate** the security differences between `chroot` and `pivot_root`

## The Challenge

A container without its own root filesystem is not much of a container. The mount namespace isolates the set of mount points visible to a process, and `pivot_root` atomically swaps the root filesystem. Together, they give each container a completely independent filesystem view.

In this exercise, you will extend the namespace setup from exercise 1 to include `CLONE_NEWNS` (mount namespace). You will prepare a minimal root filesystem, set up bind mounts for essential pseudo-filesystems (`/proc`, `/sys`, `/dev`), and use `pivot_root` to make the container see only its own root. The old root is moved to a temporary mount point and then unmounted, leaving the container with no access to the host filesystem.

The critical subtlety here is mount propagation. By default, mount events propagate between namespaces. You must set the mount propagation to `MS_PRIVATE` or `MS_SLAVE` before creating new mounts, or the container's mounts will leak to the host. Getting this wrong is a real security vulnerability in container runtimes.

`pivot_root` requires that the new root and the put-old directory be on different mount points, which means you need to bind-mount the new root onto itself first. This is a non-obvious requirement that trips up many implementors.

## Requirements

1. Add `CLONE_NEWNS` to the clone flags from exercise 1
2. Set mount propagation to `MS_PRIVATE | MS_REC` on the root mount to prevent mount leaks
3. Prepare a minimal root filesystem directory (provide instructions for creating one with Alpine)
4. Bind-mount the rootfs onto itself (`MS_BIND`)
5. Create and mount essential pseudo-filesystems: `/proc`, `/sys`, `/dev` (as devtmpfs or bind)
6. Implement `pivot_root` using `syscall.PivotRoot` to switch the root
7. Unmount the old root after pivoting and remove the mount point
8. Verify the container cannot access any host filesystem paths
9. Implement a `--rootfs` flag to specify the root filesystem path
10. Create `/dev/null`, `/dev/zero`, `/dev/random`, and `/dev/urandom` inside the container
11. Ensure `/etc/resolv.conf` and `/etc/hosts` are available inside the container

## Hints

- Download Alpine miniroot: `curl -O https://dl-cdn.alpinelinux.org/alpine/v3.19/releases/x86_64/alpine-minirootfs-3.19.1-x86_64.tar.gz` and extract it.
- The bind-mount-onto-self trick: `syscall.Mount(rootfs, rootfs, "", syscall.MS_BIND|syscall.MS_REC, "")` is required before `pivot_root`.
- After `pivot_root(new, old)`, the old root is at `old` relative to the new root. Use `syscall.Unmount` with `MNT_DETACH`.
- Set `MS_PRIVATE` recursively on `/` before doing anything: `syscall.Mount("", "/", "", syscall.MS_PRIVATE|syscall.MS_REC, "")`.
- Create device nodes with `syscall.Mknod` or bind-mount from the host's `/dev`.
- Use `os.MkdirAll` to create mount points inside the rootfs before mounting.

## Success Criteria

1. The container sees only the provided root filesystem
2. `ls /` inside the container shows the miniroot contents, not the host
3. `/proc` is mounted and functional inside the container
4. The host filesystem is completely inaccessible from the container
5. Device files (`/dev/null`, `/dev/zero`) work correctly
6. No mount events leak from the container to the host
7. After the container exits, no stale mounts remain on the host
8. The program handles missing rootfs paths with clear error messages

## Research Resources

- [pivot_root(2) man page](https://man7.org/linux/man-pages/man2/pivot_root.2.html) -- system call semantics and requirements
- [mount_namespaces(7)](https://man7.org/linux/man-pages/man7/mount_namespaces.7.html) -- mount propagation types and behavior
- [Alpine Linux miniroot downloads](https://alpinelinux.org/downloads/) -- minimal root filesystem
- [OCI Runtime Spec: Filesystem Bundle](https://github.com/opencontainers/runtime-spec/blob/main/bundle.md) -- how OCI runtimes structure root filesystems
- [Bind Mounts and Mount Propagation](https://www.kernel.org/doc/Documentation/filesystems/sharedsubtree.txt) -- kernel documentation on shared subtrees

## What's Next

With filesystem isolation in place, the next exercise adds network namespace isolation using veth pairs and bridge networking, giving the container its own network stack.

## Summary

- Mount namespace (`CLONE_NEWNS`) isolates the mount table so the container has its own filesystem view
- Mount propagation must be set to `MS_PRIVATE` to prevent mount leaks between namespaces
- `pivot_root` is preferred over `chroot` because it fully replaces the root mount and is harder to escape
- The bind-mount-onto-self pattern is required to satisfy `pivot_root`'s requirement for separate mount points
- Essential pseudo-filesystems (`/proc`, `/sys`, `/dev`) must be mounted inside the container
- The old root must be unmounted after pivoting to prevent host filesystem access
