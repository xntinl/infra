# 4. Cgroups v2: CPU and Memory Limits

<!--
difficulty: insane
concepts: [cgroups-v2, unified-hierarchy, cpu-controller, memory-controller, resource-limits, cgroup-filesystem]
tools: [go, linux, systemd]
estimated_time: 2h
bloom_level: create
prerequisites: [section 38 exercises 1-3, linux cgroups concepts, /sys/fs/cgroup filesystem]
-->

## Prerequisites

- Go 1.22+ installed
- Linux environment with cgroups v2 (unified hierarchy) -- most modern distros default to this
- Root access
- Completed exercises 1-3 (namespaces and veth networking)
- Verify cgroups v2: `mount | grep cgroup2` should show `/sys/fs/cgroup type cgroup2`

## Learning Objectives

- **Create** cgroups v2 resource controllers that enforce CPU and memory limits on container processes
- **Design** a cgroup management system that creates, configures, and cleans up cgroup hierarchies
- **Evaluate** the behavior of processes under resource pressure and out-of-memory conditions

## The Challenge

Namespaces provide isolation -- a container cannot see resources outside its boundaries. But isolation is not limitation: a container with namespace isolation can still consume all available CPU and memory on the host, starving other workloads. Cgroups (control groups) v2 provide resource limiting, accounting, and prioritization using a unified hierarchy mounted at `/sys/fs/cgroup`.

In this exercise, you will create cgroup directories under `/sys/fs/cgroup`, write controller configuration files to set CPU and memory limits, and place the container process into the cgroup. The cgroups v2 API is entirely filesystem-based: you create directories, write values to files like `cpu.max`, `memory.max`, and `cgroup.procs`, and read accounting data from files like `cpu.stat` and `memory.current`.

The challenge has several subtle requirements. You must enable the controllers you need by writing to `cgroup.subtree_control` in the parent cgroup. The container process must be moved into the cgroup before it starts its workload, or early resource usage will be unconstrained. You must also handle the OOM killer: when a cgroup exceeds its memory limit, the kernel kills processes in that cgroup, and your runtime must detect and report this.

CPU limits in cgroups v2 use the bandwidth controller format: `cpu.max` takes two values -- the quota (microseconds of CPU time allowed per period) and the period (microseconds). For example, `50000 100000` means 50% of one CPU core.

## Requirements

1. Create a cgroup directory under `/sys/fs/cgroup/` with a unique name per container
2. Enable `cpu` and `memory` controllers by writing to `cgroup.subtree_control` in the parent
3. Set CPU limits via `cpu.max` using the quota/period format
4. Set memory limits via `memory.max` in bytes
5. Set memory swap limit via `memory.swap.max` (set to 0 to disable swap)
6. Move the container process into the cgroup by writing its PID to `cgroup.procs`
7. Implement `--cpu` flag accepting percentage values (e.g., `--cpu 50` for 50% of one core)
8. Implement `--memory` flag accepting human-readable values (e.g., `--memory 256m`)
9. Read and display resource usage statistics from `cpu.stat` and `memory.current` when the container exits
10. Clean up the cgroup directory when the container exits (remove from hierarchy)
11. Detect and report OOM kills by checking `memory.events` for `oom_kill` counter

## Hints

- The cgroup path might be `/sys/fs/cgroup/mycontainer-<id>/`. Create it with `os.MkdirAll`.
- Write to files with `os.WriteFile`. For example: `os.WriteFile("/sys/fs/cgroup/mycontainer/cpu.max", []byte("50000 100000"), 0644)`.
- To enable controllers: `os.WriteFile("/sys/fs/cgroup/cgroup.subtree_control", []byte("+cpu +memory"), 0644)`.
- You cannot remove a cgroup directory if it still has processes. Ensure the process has exited first.
- Parse human-readable memory values: `256m` -> `256 * 1024 * 1024` bytes.
- Test memory limits by having the container run a program that allocates increasing amounts of memory -- it should be OOM-killed at the limit.

## Success Criteria

1. The container process runs within a dedicated cgroup
2. CPU usage is limited to the specified percentage (verify with a CPU-intensive workload)
3. Memory usage is capped at the specified limit
4. A process exceeding the memory limit is OOM-killed and the runtime reports it
5. Resource usage statistics are printed when the container exits
6. The cgroup directory is cleaned up after container exit
7. Human-readable flags (`--cpu 50`, `--memory 256m`) work correctly
8. The program handles missing cgroups v2 support with a clear error message

## Research Resources

- [cgroups v2 kernel documentation](https://www.kernel.org/doc/Documentation/cgroup-v2.txt) -- authoritative reference for the unified hierarchy
- [CPU bandwidth control](https://www.kernel.org/doc/Documentation/scheduler/sched-bwc.txt) -- quota/period semantics
- [Memory controller documentation](https://www.kernel.org/doc/Documentation/cgroup-v2.txt) -- memory.max, memory.swap.max, memory.events
- [OCI Runtime Spec: Linux Resources](https://github.com/opencontainers/runtime-spec/blob/main/config-linux.md#control-groups) -- how container runtimes specify resource limits
- [cgroups(7) man page](https://man7.org/linux/man-pages/man7/cgroups.7.html) -- cgroups v1 and v2 overview

## What's Next

The next exercise implements overlay filesystems to provide copy-on-write layered storage, allowing containers to share base images efficiently.

## Summary

- Cgroups v2 uses a unified hierarchy at `/sys/fs/cgroup` with a filesystem-based API
- CPU limits use the bandwidth controller format (quota/period in microseconds)
- Memory limits are enforced by the kernel OOM killer when a cgroup exceeds `memory.max`
- Controllers must be enabled in parent cgroups via `cgroup.subtree_control`
- The container process must be placed in the cgroup before starting its workload
- Resource accounting data is available in files like `cpu.stat` and `memory.current`
