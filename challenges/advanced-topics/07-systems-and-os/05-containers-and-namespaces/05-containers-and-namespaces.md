<!--
type: reference
difficulty: advanced
section: [07-systems-and-os]
concepts: [linux-namespaces, cgroups-v2, overlayfs, seccomp, container-runtime, clone-syscall, pivot_root, user-namespace]
languages: [go, rust]
estimated_reading_time: 75 min
bloom_level: analyze
prerequisites: [syscall-interface, virtual-memory-and-paging]
papers: [Biederman2006-namespaces, Heo2015-cgroups-unified]
industry_use: [Docker, containerd, runc, Kubernetes, Podman, Firecracker]
language_contrast: high
-->

# Containers and Namespaces

> A container is not a kernel feature — it is a user-space construct built from six
> Linux namespace types, cgroups, and a filesystem trick; understanding those primitives
> is what lets you build secure, efficient container runtimes and debug mysterious
> container behavior in production.

## Mental Model

"Containers" are not a kernel abstraction. There is no `container_create()` syscall.
What Docker, containerd, and runc actually do is orchestrate three independent kernel
mechanisms: **namespaces** (isolation), **cgroups** (resource limits), and **overlayfs**
(layered filesystem). Strip away the tooling and you are left with:

1. Call `clone(2)` with namespace flags to create a process in a new isolated environment.
2. Write limits into the cgroup hierarchy to cap CPU, memory, and I/O.
3. Set up an overlayfs mount that layers read-only base layers (the container image) under
   a writable upper layer (the container's runtime writes).
4. Call `pivot_root(2)` or `chroot(2)` to make the container see its own root filesystem.
5. Optionally, load a seccomp filter to restrict syscalls and drop capabilities.
6. `execve(2)` the container's entrypoint.

This is exactly what `runc` (OCI runtime, used by Docker and containerd) does, in
approximately this order. Understanding each step lets you diagnose the class of problems
that production platforms encounter: namespace leaks that accumulate over time, cgroup
memory limits that fire the OOM killer instead of throttling gracefully, overlayfs
directory count bugs (inode limits), capability escalations via `user_namespaces`, and
seccomp false positives that crash legitimate JVM or Go processes.

Namespaces provide **what a process can see**; cgroups provide **what a process can use**.
These are orthogonal. A process in a PID namespace still consumes CPU cycles counted by
the parent's cgroup. A process with a memory cgroup limit of 128 MB will be killed by the
OOM killer when it exceeds that limit, regardless of its namespace membership.

## Core Concepts

### Linux Namespaces

Linux provides seven namespace types (as of kernel 5.x):

| Namespace | Flag | Isolates |
|-----------|------|---------|
| PID | `CLONE_NEWPID` | Process IDs — PID 1 inside the namespace |
| Network | `CLONE_NEWNET` | Network interfaces, routing tables, ports |
| Mount | `CLONE_NEWNS` | Filesystem mount points |
| UTS | `CLONE_NEWUTS` | Hostname and NIS domain name |
| IPC | `CLONE_NEWIPC` | System V IPC, POSIX message queues |
| User | `CLONE_NEWUSER` | UID/GID mappings (unprivileged containers) |
| Cgroup | `CLONE_NEWCGROUP` | cgroup root view |

Namespaces are reference-counted kernel objects. They exist as long as any process or
file descriptor holds a reference. `/proc/<pid>/ns/` contains symbolic links to each
namespace the process belongs to. Bind-mounting these links preserves namespaces after
all processes exit — a common technique for network namespace persistence (used by CNI
plugins to keep pod network namespaces alive across container restarts).

### cgroups v2 (Unified Hierarchy)

cgroups v2 (Linux 4.5+, default in most distros since 2020) uses a single unified
hierarchy at `/sys/fs/cgroup/`. Every process belongs to exactly one cgroup. Key
controllers:

| Controller | File | Effect |
|------------|------|--------|
| memory | `memory.max` | OOM kill when RSS exceeds this |
| memory | `memory.high` | Throttle via memory pressure notifications |
| cpu | `cpu.weight` | Relative CPU time shares (1–10000, default 100) |
| cpu | `cpu.max` | Hard CPU quota: "N µs per M µs period" |
| io | `io.max` | Per-device IOPS and bandwidth caps |
| pids | `pids.max` | Maximum number of PIDs in the cgroup |

The transition from cgroups v1 to v2 matters for Kubernetes: systemd-based nodes use the
unified hierarchy by default, and `kubelet` now uses cgroups v2 for pod memory limits.
The key behavioral difference: cgroups v2 memory accounting includes the page cache, so
a container reading large files may hit its `memory.max` limit even with low RSS.

### overlayfs

overlayfs is a union filesystem that presents multiple directories as a single merged
view. For containers:
- **lowerdir**: read-only base layers (container image layers)
- **upperdir**: writable layer (container's runtime writes)
- **workdir**: internal overlayfs scratch directory
- **merged**: the unified view the container sees

When a container writes to a file that exists in the lowerdir, overlayfs performs a
**copy-up**: the file is copied from the lowerdir to the upperdir, then modified. This
means the first write to any large file in a base layer has copy-up overhead proportional
to the file size. Long-running containers accumulate writes in the upperdir; production
systems use volume mounts for large, frequently-written files (databases, logs) to avoid
copy-up overhead and upperdir growth.

### User Namespaces

User namespaces allow unprivileged users to create containers. A process in a user
namespace can map its UID 0 (root inside the namespace) to a non-root UID on the host.
This is the mechanism behind Podman's rootless containers. The security implication:
with `CLONE_NEWUSER`, an unprivileged process can create network namespaces, mount
namespaces, and other namespaces without any special privileges. The kernel carefully
limits what operations are allowed from within a user namespace to prevent privilege
escalation to the host.

## Implementation: Go

```go
// +build linux
//
// This program creates a minimal container: a new PID + network + UTS + mount namespace,
// with a cgroup memory limit, and executes /bin/sh inside it.
//
// Run as root: sudo go run .
// Requires: /bin/sh, /proc (for bind mounts)

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

const (
	containerRootfs = "/tmp/minicontainer"
	cgroupBase      = "/sys/fs/cgroup/minicontainer"
	memoryMax       = "128m" // 128 MB memory limit
)

// setupCgroupV2 creates a cgroup for the container and sets resource limits.
// Writes to the cgroup filesystem — requires root.
func setupCgroupV2(pid int) error {
	if err := os.MkdirAll(cgroupBase, 0755); err != nil {
		return fmt.Errorf("mkdir cgroup: %w", err)
	}
	// Set memory limit.
	if err := os.WriteFile(filepath.Join(cgroupBase, "memory.max"), []byte(memoryMax), 0644); err != nil {
		return fmt.Errorf("write memory.max: %w", err)
	}
	// CPU weight (relative to other cgroups).
	if err := os.WriteFile(filepath.Join(cgroupBase, "cpu.weight"), []byte("50"), 0644); err != nil {
		return fmt.Errorf("write cpu.weight: %w", err)
	}
	// Move the process into the cgroup.
	if err := os.WriteFile(filepath.Join(cgroupBase, "cgroup.procs"), []byte(strconv.Itoa(pid)), 0644); err != nil {
		return fmt.Errorf("write cgroup.procs: %w", err)
	}
	return nil
}

// cleanupCgroup removes the cgroup after the container exits.
func cleanupCgroup() {
	_ = os.Remove(cgroupBase) // only works when cgroup.procs is empty
}

// setupRootfs prepares a minimal root filesystem for the container.
// In production, this would be an overlayfs mount over image layers.
// Here we create a tmpfs with just /proc.
func setupRootfs(root string) error {
	for _, dir := range []string{root, root + "/proc", root + "/bin", root + "/lib", root + "/lib64"} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	// In a real runtime, copy or bind-mount the base image here.
	// For this demo we skip the filesystem and just mount /proc inside the namespace.
	return nil
}

// childProcess is the function that runs inside the new namespaces.
// It is invoked by re-executing the current binary with a special argument.
func childProcess() error {
	// We are now in new PID, UTS, mount, and network namespaces.

	// Set a hostname visible only inside this container.
	if err := syscall.Sethostname([]byte("minicontainer")); err != nil {
		return fmt.Errorf("sethostname: %w", err)
	}

	// Mount /proc so `ps` works inside the container.
	// This /proc sees only PIDs in our PID namespace.
	if err := syscall.Mount("proc", "/proc", "proc", 0, ""); err != nil {
		return fmt.Errorf("mount proc: %w", err)
	}
	defer syscall.Unmount("/proc", 0)

	// Mount a tmpfs for /tmp.
	if err := syscall.Mount("tmpfs", "/tmp", "tmpfs", 0, "size=64m"); err != nil {
		return fmt.Errorf("mount tmpfs: %w", err)
	}
	defer syscall.Unmount("/tmp", 0)

	fmt.Println("[container] Namespaces active. Hostname:", func() string {
		h, _ := os.Hostname()
		return h
	}())
	fmt.Println("[container] PID:", os.Getpid(), "(should be 1 in PID namespace)")

	// Execute /bin/sh as the container's PID 1.
	cmd := exec.Command("/bin/sh")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// parentProcess forks the child into new namespaces and configures cgroups.
func parentProcess() error {
	// Re-exec ourselves with the "__CHILD__" argument so the child runs childProcess().
	self, err := os.Executable()
	if err != nil {
		return err
	}

	cmd := exec.Command(self, "__CHILD__")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// SysProcAttr.Cloneflags tells the kernel to create these namespaces
	// when clone(2) is called for this process.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWPID |
			syscall.CLONE_NEWUTS |
			syscall.CLONE_NEWNS |
			syscall.CLONE_NEWNET |
			syscall.CLONE_NEWIPC,
		// Pdeathsig: send SIGKILL to child if parent dies.
		Pdeathsig: syscall.SIGKILL,
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start child: %w", err)
	}

	// Configure cgroup for the child before it begins executing.
	if err := setupCgroupV2(cmd.Process.Pid); err != nil {
		fmt.Println("cgroup setup failed (may need cgroups v2):", err)
		// Continue without cgroup limits — still demonstrates namespaces.
	}
	defer cleanupCgroup()

	fmt.Printf("[parent] Container PID: %d\n", cmd.Process.Pid)
	fmt.Printf("[parent] Cgroup: %s\n", cgroupBase)

	// Show the namespace files for the child.
	for _, ns := range []string{"pid", "uts", "net", "mnt", "ipc"} {
		link, _ := os.Readlink(fmt.Sprintf("/proc/%d/ns/%s", cmd.Process.Pid, ns))
		fmt.Printf("[parent] %s namespace: %s\n", ns, link)
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("wait: %w", err)
	}
	return nil
}

// inspectNamespaces prints the calling process's namespace membership.
func inspectNamespaces() {
	fmt.Println("=== Current Process Namespaces ===")
	for _, ns := range []string{"pid", "uts", "net", "mnt", "ipc", "user", "cgroup"} {
		link, err := os.Readlink(fmt.Sprintf("/proc/self/ns/%s", ns))
		if err != nil {
			fmt.Printf("  %s: error: %v\n", ns, err)
			continue
		}
		fmt.Printf("  %-8s: %s\n", ns, link)
	}
}

func main() {
	// The child re-execs itself with "__CHILD__" to enter the child code path.
	if len(os.Args) > 1 && os.Args[1] == "__CHILD__" {
		if err := childProcess(); err != nil {
			fmt.Fprintln(os.Stderr, "[child] error:", err)
			os.Exit(1)
		}
		return
	}

	fmt.Println("=== Minimal Container Runtime Demo ===\n")
	inspectNamespaces()
	fmt.Println()

	if os.Getuid() != 0 {
		fmt.Println("WARNING: not running as root — namespace creation may fail.")
		fmt.Println("Run with: sudo go run .")
	}

	if err := parentProcess(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}

	fmt.Println("[parent] Container exited.")
}
```

### Go-specific considerations

Go's `exec.Cmd.SysProcAttr.Cloneflags` directly maps to the `clone(2)` syscall flags.
When `cmd.Start()` is called, the Go runtime internally calls `clone` with the specified
flags, creating the child process in the requested namespaces. This is the same mechanism
`runc` uses under the hood.

The `CLONE_NEWUSER` flag enables user namespaces. Combined with
`SysProcAttr.UidMappings` and `SysProcAttr.GidMappings`, this allows an unprivileged
process to create a container where the container's root (UID 0) maps to an unprivileged
UID on the host — the foundation of rootless containers.

For production container runtimes in Go, the `opencontainers/runc` and
`containerd/containerd` repositories are authoritative. The `libcontainer` package inside
`runc` provides a complete container lifecycle API built on exactly these primitives.

## Implementation: Rust

```rust
// +linux
//
// Builds a minimal container using nix crate for namespace creation,
// cgroup management, and filesystem setup.
//
// Add to Cargo.toml:
//   nix = { version = "0.27", features = ["process", "mount", "unistd", "sched"] }
//
// Run as root: sudo cargo run

use nix::mount::{mount, MntFlags, MsFlags};
use nix::sched::{clone, CloneFlags};
use nix::sys::signal::Signal;
use nix::sys::wait::waitpid;
use nix::unistd::{sethostname, getpid, execv};
use std::ffi::CString;
use std::fs;
use std::path::Path;

const CHILD_STACK_SIZE: usize = 1024 * 1024; // 1 MB stack for the child

// Container configuration.
struct ContainerConfig {
    hostname: String,
    memory_max: String,
    cgroup_path: String,
}

impl ContainerConfig {
    fn default() -> Self {
        Self {
            hostname: "nix-container".to_string(),
            memory_max: "128m".to_string(),
            cgroup_path: "/sys/fs/cgroup/nix-container".to_string(),
        }
    }
}

// setup_cgroup_v2 creates cgroup entries for the container.
fn setup_cgroup_v2(config: &ContainerConfig, pid: nix::unistd::Pid) -> Result<(), String> {
    let path = Path::new(&config.cgroup_path);
    fs::create_dir_all(path).map_err(|e| format!("mkdir cgroup: {e}"))?;

    fs::write(path.join("memory.max"), &config.memory_max)
        .map_err(|e| format!("write memory.max: {e}"))?;

    fs::write(path.join("cpu.weight"), "50")
        .map_err(|e| format!("write cpu.weight: {e}"))?;

    fs::write(path.join("cgroup.procs"), pid.to_string())
        .map_err(|e| format!("write cgroup.procs: {e}"))?;

    println!("[parent] cgroup configured at {}", config.cgroup_path);
    Ok(())
}

fn cleanup_cgroup(path: &str) {
    let _ = fs::remove_dir(path);
}

// child_fn runs inside the new namespaces.
// This function is called by clone(2); it must return an i32 exit code.
fn child_fn() -> isize {
    let config = ContainerConfig::default();

    // Set hostname visible only inside this UTS namespace.
    if let Err(e) = sethostname(&config.hostname) {
        eprintln!("[child] sethostname: {e}");
        return 1;
    }

    // Mount /proc inside the new PID namespace.
    // This /proc shows only PIDs visible in our namespace.
    let _ = fs::create_dir_all("/proc");
    if let Err(e) = mount(
        Some("proc"),
        "/proc",
        Some("proc"),
        MsFlags::empty(),
        None::<&str>,
    ) {
        eprintln!("[child] mount proc: {e}");
        // Non-fatal if /proc already mounted.
    }

    println!("[child] hostname: {}", config.hostname);
    println!("[child] PID inside namespace: {}", getpid());

    // Execute a shell as PID 1 inside the container.
    let shell = CString::new("/bin/sh").unwrap();
    let args: Vec<CString> = vec![CString::new("/bin/sh").unwrap()];
    match execv(&shell, &args) {
        Ok(_) => 0,
        Err(e) => {
            eprintln!("[child] execv: {e}");
            1
        }
    }
}

fn print_namespaces(pid: nix::unistd::Pid) {
    println!("[parent] Namespaces for PID {}:", pid);
    for ns in &["pid", "uts", "net", "mnt", "ipc", "user"] {
        let link = fs::read_link(format!("/proc/{pid}/ns/{ns}"));
        match link {
            Ok(l) => println!("  {:8}: {:?}", ns, l),
            Err(e) => println!("  {:8}: error: {e}", ns),
        }
    }
}

fn inspect_self_namespaces() {
    println!("=== Current Process Namespaces ===");
    for ns in &["pid", "uts", "net", "mnt", "ipc", "user", "cgroup"] {
        let link = fs::read_link(format!("/proc/self/ns/{ns}"));
        match link {
            Ok(l) => println!("  {:8}: {:?}", ns, l),
            Err(e) => println!("  {:8}: error: {e}", ns),
        }
    }
}

fn main() {
    println!("=== Minimal Container Runtime (Rust/nix) ===\n");
    inspect_self_namespaces();
    println!();

    let config = ContainerConfig::default();

    // Allocate a stack for the child process.
    // clone(2) requires a pre-allocated stack; the child uses it for its execution.
    let mut stack = vec![0u8; CHILD_STACK_SIZE];

    let flags = CloneFlags::CLONE_NEWPID
        | CloneFlags::CLONE_NEWUTS
        | CloneFlags::CLONE_NEWNS
        | CloneFlags::CLONE_NEWNET
        | CloneFlags::CLONE_NEWIPC;

    // clone(2): fork the child into new namespaces.
    // Safety: child_fn does not capture any references from this scope.
    let child_pid = unsafe {
        clone(
            Box::new(child_fn),
            &mut stack,
            flags,
            Some(Signal::SIGCHLD as i32),
        )
    };

    let child_pid = match child_pid {
        Ok(pid) => pid,
        Err(e) => {
            eprintln!("clone failed: {e} (run as root?)");
            return;
        }
    };

    println!("[parent] Container PID: {}", child_pid);

    // Configure cgroup before the child makes progress.
    if let Err(e) = setup_cgroup_v2(&config, child_pid) {
        eprintln!("cgroup setup failed (may need cgroups v2): {e}");
    }

    print_namespaces(child_pid);

    // Wait for the container to exit.
    match waitpid(child_pid, None) {
        Ok(status) => println!("[parent] Container exited: {:?}", status),
        Err(e) => eprintln!("[parent] waitpid: {e}"),
    }

    cleanup_cgroup(&config.cgroup_path);
    println!("[parent] Cleanup done.");
}
```

### Rust-specific considerations

The `nix` crate's `clone` function takes a `Box<dyn FnMut() -> isize>` closure and
allocates the child's stack from the provided buffer. The closure captures the
`ContainerConfig` by reference, which requires careful attention: the child closure
must not outlive the parent's stack frame. In the example above, `ContainerConfig::default()`
is called inside the closure to avoid captures.

For production use, the `youki` project (a container runtime written in Rust, OCI-
compatible) is the reference implementation. It handles the full OCI runtime spec,
including overlayfs setup, user namespace ID mapping, seccomp profile loading, and
pivot_root. Study `youki/crates/libcontainer/` for the authoritative Rust container
runtime implementation.

The `caps` crate handles Linux capability management in Rust. For seccomp, the `seccomp`
crate (crates.io) provides a safe API for building and loading BPF seccomp profiles.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|----|------|
| `clone` syscall | `exec.Cmd.SysProcAttr.Cloneflags` | `nix::sched::clone` (explicit stack) |
| User namespace mapping | `SysProcAttr.UidMappings` / `GidMappings` | Write to `/proc/<pid>/uid_map` via `nix` |
| Namespace file manipulation | `os.Readlink("/proc/pid/ns/...")` | `fs::read_link(...)` |
| cgroup v2 management | Write to `/sys/fs/cgroup/` via `os.WriteFile` | Write via `fs::write` |
| overlayfs mounting | `syscall.Mount` with overlayfs options | `nix::mount::mount` |
| Capabilities | Raw `SYS_CAPGET`/`SYS_CAPSET` | `caps` crate |
| Seccomp loading | `syscall.SYS_SECCOMP` | `seccomp` crate |
| Production OCI runtime | `opencontainers/runc` | `youki` |
| Self-re-exec pattern | Required (Go cannot fork without exec) | Not required (can fork directly) |
| Fork-safety | Go cannot safely fork a multi-threaded process | Rust can fork; Rust is not multi-threaded by default |

A critical Go-specific note: Go programs cannot safely `fork(2)` without immediately
calling `exec(2)`. A forked Go program with goroutines running in the parent will have
those goroutines' state duplicated in the child, causing undefined behavior. This is
why the container demo above uses `exec.Command(self, "__CHILD__")` (a re-exec pattern)
rather than directly forking. Rust programs without threads can `fork` safely.

## Production War Stories

**Kubernetes cgroup OOM and page cache**: A Kubernetes pod with `memory.max = 512MB`
was getting OOM-killed despite low application RSS. The root cause: cgroups v2 includes
the page cache in the memory accounting. The pod's init container ran `apt-get install`
which filled the page cache with package data. The application container then inherited
this page cache usage (which the kernel counts against `memory.max`), and the first
significant memory allocation triggered an OOM kill. Fix: add `memory.high` set 20%
below `memory.max` to trigger early memory pressure and page reclaim before the hard
limit fires.

**PID namespace and zombie reaping**: When a container's PID 1 process exits, all
processes in that PID namespace are killed (the PID namespace requires an active PID 1).
More subtly: a container that spawns child processes but whose PID 1 does not `wait()` on
them will accumulate zombie processes. In a Kubernetes pod, if the entrypoint process does
not reap its children, the zombie count grows without bound. Solution: use `tini` or
`dumb-init` as the container's PID 1 — they properly reap zombie children.

**Mount namespace and `/etc/resolv.conf`**: Docker and Kubernetes inject `/etc/resolv.conf`
by bind-mounting a host-managed file into the container's mount namespace. When the host's
DNS configuration changes (DHCP lease renewal, VPN connect), the injected file is updated.
But a container with its own `/etc/resolv.conf` bind mount sees the old file until the
bind mount is explicitly refreshed. This caused production DNS failures in clusters with
rotating DNS servers.

**overlayfs inode exhaustion**: overlayfs uses a separate inode namespace for each merged
directory. On ext4-backed Docker roots with 100+ container layers, the inode count in
the overlay metadata directory can exhaust the underlying filesystem's inode limit,
causing `ENOSPC` even with available disk space. Production fix: use xfs (which has
dynamic inodes) or set `inode_ratio` in mke2fs to reserve more inodes.

**User namespace and capability confusion**: A Go service running inside a user namespace
with UID 0 inside the namespace (but non-root on the host) attempted to open a raw socket
(`socket(AF_PACKET, SOCK_RAW, ...)`). This requires `CAP_NET_RAW`. Inside the user
namespace, the process had `CAP_NET_RAW` in its namespace. But `AF_PACKET` sockets
require `CAP_NET_RAW` in the **initial network namespace**, not the process's current one.
The `socket()` call returned EPERM, which the service interpreted as a network
misconfiguration. Understanding the distinction between capabilities in the initial
namespace vs. the current namespace is essential for debugging container permission errors.

## Complexity Analysis

| Operation | Latency | Notes |
|-----------|---------|-------|
| `clone(2)` with 5 namespace flags | ~500 µs – 2 ms | Depends on number of new namespaces |
| Network namespace creation | ~200 µs | Creates new netlink socket, loopback |
| Mount namespace creation | ~100 µs | Copies parent's mount table |
| cgroup `mkdir` + write | ~100–500 µs | Filesystem write to cgroupfs |
| overlayfs mount | ~1–5 ms | Scans lower dirs for whiteout detection |
| `pivot_root` | ~10–50 µs | Atomic root directory swap |
| `execve` after namespace setup | ~1–5 ms | Binary loading, ELF parsing, dynamic linking |
| Full container start (runc) | ~100–300 ms | All of the above plus image layer setup |
| Firecracker microVM start | ~100–150 ms | KVM-based; faster than full OS boot |

## Common Pitfalls

1. **Not running `umount --recursive` before removing mount namespaces.** If a process
   in a mount namespace exits without unmounting filesystems it mounted, those mounts
   persist until the last reference to the mount namespace is dropped. In long-running
   systems (container orchestrators), accumulated mounts from failed containers can
   exhaust the kernel's mount count limit.

2. **Misunderstanding `CLONE_NEWPID` semantics.** `CLONE_NEWPID` creates a new PID
   namespace for the child — but the parent still sees the child with its original host
   PID. The child sees itself as PID 1. If the child's process then calls `getpid()`, it
   returns 1. But `/proc/<host-pid>/status` still shows `Pid: <host-pid>` from the parent's
   view. Both views are correct.

3. **Forgetting cgroup v1 vs v2 behavioral differences.** cgroups v1 `memory.limit_in_bytes`
   does not include the page cache; cgroups v2 `memory.max` does. A service that ran fine
   with cgroups v1 limits may OOM-kill under cgroups v2 on the same workload, because
   cgroups v2 counts page cache against the limit.

4. **Using `docker run --privileged` in production.** `--privileged` disables all
   seccomp filtering, drops all capability restrictions, and gives the container access to
   all devices. It is equivalent to running as root on the host. Use `--cap-add` to grant
   only the specific capabilities needed.

5. **Ignoring the copy-up cost of overlayfs for large files.** A container that writes
   to a large file from the base image layer (e.g., modifying a 2 GB database file in the
   container) will trigger a copy-up that copies the entire file from lowerdir to upperdir.
   Database containers should always use volume mounts (`-v /host/data:/container/data`)
   to bypass overlayfs entirely for large mutable data.

## Exercises

**Exercise 1** (30 min): Use `unshare -u /bin/bash` to create a new UTS namespace and
change the hostname. Observe that `hostname` in the new shell shows the new name while
the host sees the original. Then use `lsns` to list all running namespaces and explain
the output.

**Exercise 2** (2–4 h): Write a Go program that creates a new network namespace, creates
a `veth` pair, moves one end into the new namespace, assigns IP addresses, and sends
a ping between the two ends. This is the core of what Docker does when it creates a
container with a bridge network. Use `golang.org/x/sys/unix` for `netlink` operations.

**Exercise 3** (4–8 h): Implement `overlayfs` mount setup in Go: given a list of
read-only lower directories (container image layers) and a writable upper directory,
mount them as an overlay and `pivot_root` into the merged view. Write a file in the
overlay and verify that the write appears in the upper directory while the lower
directories are unchanged.

**Exercise 4** (8–15 h): Build a minimal OCI-compliant container runtime in Go or Rust
that: parses a `config.json` (OCI runtime spec), creates the appropriate namespaces,
sets up cgroup v2 limits, mounts overlayfs, sets up a `/dev` tmpfs, loads a seccomp
profile, drops capabilities to a minimal set, and calls `execve` on the container
entrypoint. Test it with `runc spec` to generate a config and compare behavior with runc.

## Further Reading

### Foundational Papers

- Biederman, E. (2006). "Multiple Instances of the Global Linux Namespaces." Ottawa Linux
  Symposium 2006. The original design document for Linux namespaces.
- Heo, T. (2015). "cgroups v2." LSFMM 2015. Design rationale for the unified cgroup
  hierarchy; explains the problems with cgroups v1.
- Walsh, D. (2017). "Rootless Containers." Container Camp 2017. How user namespaces
  enable unprivileged container creation.

### Books

- Kerrisk, M. "The Linux Programming Interface" — Chapter 28 (Process Creation and
  Program Execution) and Chapter 43 (IPC and Namespaces) are the reference.
- Love, R. "Linux Kernel Development" — Chapter 3 (Process Management) covers `clone`,
  `fork`, and `exec` at the kernel level.

### Production Code to Read

- `opencontainers/runc` — `libcontainer/` is the authoritative container lifecycle
  implementation: namespaces, cgroups, overlayfs, seccomp, capabilities.
- `youki` — Rust OCI runtime. `crates/libcontainer/src/` for namespace and cgroup setup.
- `moby/moby` (Docker) — `daemon/execdriver/` and `pkg/sysinfo/` for how Docker manages
  namespace and cgroup setup on top of runc.
- Linux kernel: `kernel/nsproxy.c` (namespace reference counting), `mm/memcontrol.c`
  (memory cgroup accounting).

### Conference Talks

- "Building Container Runtimes from Scratch" — KubeCon 2019. Live coding of a minimal
  container runtime in Go using the techniques in this document.
- "The State of cgroups v2 in Kubernetes" — KubeCon 2021. Migration pain points and
  memory accounting behavior changes.
- "Rootless Containers" — ContainerCon 2017. How Podman achieves rootless containers
  with user namespaces.
- "What Goes Wrong with Containers" — SREcon 2020. Production incident post-mortems
  involving namespace leaks, cgroup OOM, and overlayfs limits.
