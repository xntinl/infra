# 94. Cgroup Resource Limiter

<!--
difficulty: advanced
category: containers-and-systems
languages: [go]
concepts: [cgroups-v2, resource-control, cpu-limits, memory-limits, io-bandwidth, pid-limits, process-management, linux-filesystem-api]
estimated_time: 12-16 hours
bloom_level: evaluate
prerequisites: [go-concurrency, linux-process-model, filesystem-operations, syscall-basics, exec-and-fork]
-->

## Languages

- Go 1.22+

## Prerequisites

- Go goroutines, channels, and the `os/exec` package
- Linux process model: PIDs, process groups, parent-child relationships
- Filesystem operations in Go: `os.MkdirAll`, `os.WriteFile`, `os.ReadFile`
- Basic Linux syscall familiarity: `clone`, `exec`, `wait`
- Understanding of resource consumption: CPU time, RSS memory, disk I/O
- **Linux required**: cgroups v2 is a Linux-specific feature. macOS users should use a Linux VM or Docker container with `--privileged` flag for development. See the note in Research Resources for a Docker-based setup
- Cgroups v2 must be enabled: verify with `mount | grep cgroup2` (should show `/sys/fs/cgroup` mounted as `cgroup2`)

## Learning Objectives

- **Implement** a process resource limiter using the Linux cgroups v2 filesystem interface
- **Evaluate** the behavior of processes under CPU, memory, I/O, and PID limits and understand kernel enforcement mechanisms
- **Design** a CLI tool that creates, configures, and destroys cgroup hierarchies for arbitrary commands
- **Analyze** resource usage statistics from cgroup controller files and present them in a human-readable format
- **Apply** proper cleanup patterns to avoid orphaned cgroups and leaked resources
- **Understand** the relationship between cgroup controllers, the delegation model, and the unified hierarchy

## The Challenge

Linux cgroups (control groups) are the kernel mechanism that makes containers possible. Every container runtime -- Docker, Podman, containerd -- uses cgroups to enforce resource limits on processes. When you set `--memory=512m` on a Docker container, Docker writes `512000000` to a cgroup's `memory.max` file. When you set `--cpus=2`, it writes the appropriate value to `cpu.max`.

Cgroups v2 (the unified hierarchy) organizes all resource controllers under a single filesystem tree at `/sys/fs/cgroup`. Each cgroup is a directory. Resource limits are set by writing to files in that directory. Processes are assigned by writing their PID to the `cgroup.procs` file. The kernel enforces the limits transparently.

Build a CLI tool that creates cgroups, sets resource limits, runs a command within those limits, monitors resource usage, and cleans up. The tool should support: CPU bandwidth limiting (`cpu.max`), memory limits with swap control (`memory.max`, `memory.swap.max`), I/O bandwidth throttling (`io.max`), and PID limits (`pids.max`).

This is the foundational skill for understanding how container runtimes work. Every `docker run --memory=X --cpus=Y` invocation does exactly what you will build here, plus namespace isolation (covered in challenge 104).

## Requirements

1. Implement cgroup creation: create a new cgroup directory under `/sys/fs/cgroup/` with a user-specified name. Verify that required controllers are available in `cgroup.controllers`
2. Implement CPU limiting via `cpu.max`. Accept limits as a fraction of a CPU (e.g., `0.5` = 50% of one core, `2.0` = 200% = two full cores). Convert to the `$QUOTA $PERIOD` format expected by `cpu.max`
3. Implement memory limiting via `memory.max` and `memory.swap.max`. Accept limits in human-readable format (e.g., `256M`, `1G`). Support disabling swap entirely by setting `memory.swap.max` to `0`
4. Implement I/O bandwidth limiting via `io.max`. Accept device major:minor numbers and read/write bandwidth limits in bytes per second
5. Implement PID limiting via `pids.max`. Accept a maximum number of processes allowed in the cgroup
6. Implement process assignment: move a process (by PID) into the cgroup by writing to `cgroup.procs`. Support moving the current process and child processes
7. Implement resource monitoring: read and parse `cpu.stat`, `memory.current`, `memory.stat`, `io.stat`, and `pids.current`. Display formatted statistics
8. Implement a `run` command that creates a cgroup with specified limits, executes a command inside it, streams stdout/stderr, collects exit status, prints resource usage summary, and destroys the cgroup on exit
9. Implement proper cleanup: remove the cgroup on normal exit, signal handling (SIGINT, SIGTERM), and error conditions. Handle the case where processes are still running in the cgroup (kill them first)
10. Write integration tests that verify: CPU throttling (a CPU-intensive task runs slower under limit), memory enforcement (OOM kill when exceeding limit), PID limit enforcement (fork bomb is contained), and cleanup after exit

## Hints

**Hint 1 -- CPU quota/period math**: `cpu.max` accepts `"$QUOTA $PERIOD"` where both are in microseconds. For `0.5` CPU: period = 100000 (100ms), quota = 50000 (50ms). For `2.0` CPUs: period = 100000, quota = 200000. Formula: `quota = cpu_fraction * period`.

**Hint 2 -- Controller delegation**: To use controllers in a child cgroup, the parent must have them enabled in `cgroup.subtree_control`. Write `"+cpu +memory +io +pids"` to the parent's `cgroup.subtree_control` before creating the child cgroup.

**Hint 3 -- OOM handling**: When a process exceeds `memory.max`, the kernel's OOM killer terminates it. The process receives SIGKILL. Check `memory.events` for `oom_kill` count after the process exits to report whether OOM occurred.

**Hint 4 -- Cleanup ordering**: To remove a cgroup directory, it must be empty (no processes). First send SIGKILL to all PIDs in `cgroup.procs`, wait for them to exit, then `os.Remove` the directory. Reading `cgroup.procs` may return PIDs even after SIGKILL -- poll until empty.

## Acceptance Criteria

- [ ] Cgroups are created and destroyed cleanly with no orphaned directories in `/sys/fs/cgroup`
- [ ] CPU limiting works: a CPU-bound process with `--cpu=0.5` uses approximately 50% of one core (verified via `/proc/[pid]/stat` or `cpu.stat`)
- [ ] Memory limiting works: a process exceeding `--memory=64M` is OOM-killed. The tool reports the OOM event
- [ ] PID limiting works: a fork bomb with `--pids=10` is contained and does not crash the system
- [ ] I/O bandwidth limiting is configured correctly (write to `io.max` verified by reading it back)
- [ ] Resource usage statistics are collected and displayed after the command exits
- [ ] Signal handling: SIGINT and SIGTERM to the tool result in clean cgroup teardown
- [ ] The tool works as a CLI: `cglimit run --cpu=0.5 --memory=256M --pids=100 -- stress-ng --cpu 4`
- [ ] Controller availability is checked before attempting to set limits
- [ ] Integration tests pass demonstrating resource enforcement

## Key Concepts

**Cgroups v2 unified hierarchy**: Unlike cgroups v1 (which had separate hierarchies per controller), v2 uses a single directory tree. All controllers are managed together. A process belongs to exactly one cgroup. This simplifies management and avoids the inconsistencies of v1's multiple hierarchies.

**Controller files**: Each resource controller exposes files in the cgroup directory. `cpu.max` controls CPU bandwidth. `memory.max` sets the memory limit. `pids.max` limits the number of processes. These are plain text files -- resource control is filesystem I/O.

**Subtree control delegation**: Controllers must be explicitly enabled for child cgroups via the parent's `cgroup.subtree_control`. If you create `/sys/fs/cgroup/mygroup/` and want CPU control, the parent (`/sys/fs/cgroup/`) must have `+cpu` in its `cgroup.subtree_control`.

**OOM killer integration**: When a cgroup exceeds `memory.max`, the kernel invokes the OOM killer on processes within that cgroup. The kernel selects the process to kill based on memory usage. This is the same mechanism Docker uses for `--memory` enforcement.

**Resource accounting**: Even without setting limits, cgroup stat files (`cpu.stat`, `memory.current`, `io.stat`) provide accurate per-cgroup resource accounting. This is how `docker stats` works -- it reads these files.

## Research Resources

- [Linux kernel cgroups v2 documentation](https://www.kernel.org/doc/html/latest/admin-guide/cgroup-v2.html) -- the authoritative reference for all controller files and semantics
- [cgroup v2: Linux's new unified control group hierarchy (LWN)](https://lwn.net/Articles/679786/) -- overview of v2 design and migration from v1
- [Red Hat: Understanding cgroups v2](https://access.redhat.com/documentation/en-us/red_hat_enterprise_linux/9/html/managing_monitoring_and_updating_the_kernel/assembly_using-cgroups-v2-to-control-distribution-of-cpu-time-for-applications_managing-monitoring-and-updating-the-kernel) -- practical guide with examples
- [Go os/exec package documentation](https://pkg.go.dev/os/exec) -- running child processes with controlled stdin/stdout/stderr
- [Go syscall package](https://pkg.go.dev/syscall) -- SysProcAttr for setting cgroup on process creation
- [runc cgroup manager source code](https://github.com/opencontainers/runc/tree/main/libcontainer/cgroups) -- production-grade Go cgroup management
- [Docker-based development for macOS users](https://docs.docker.com/engine/reference/run/#runtime-privilege-and-linux-capabilities) -- run `docker run --privileged -it ubuntu:latest` to get a Linux environment with cgroup access
- [stress-ng: system stress tester](https://github.com/ColinIanKing/stress-ng) -- useful for generating controlled CPU, memory, and I/O load for testing
