# 7. Container Lifecycle Management

<!--
difficulty: insane
concepts: [container-state-machine, pid-tracking, state-persistence, signal-forwarding, graceful-shutdown, container-logging]
tools: [go, linux, json]
estimated_time: 3h
bloom_level: create
prerequisites: [section 38 exercises 1-6, section 14 context and signals, state machine design]
-->

## Prerequisites

- Go 1.22+ installed
- Linux environment with root access
- Completed exercises 1-6 (namespaces, rootfs, networking, cgroups, overlayfs, image pulling)
- Familiarity with signal handling and process management (section 14)
- Understanding of state machine design patterns

## Learning Objectives

- **Create** a container lifecycle manager that tracks containers through create, start, running, stopped, and removed states
- **Design** a persistent state store that survives runtime restarts and enables container listing and inspection
- **Evaluate** signal forwarding strategies and graceful shutdown semantics for containerized processes

## The Challenge

A real container runtime does not just fork a process and forget about it. It manages a lifecycle: containers are created (filesystem and namespaces prepared), started (process launched), monitored while running, stopped (gracefully or forcefully), and eventually removed (resources cleaned up). The runtime must track the state of each container, persist that state to disk, and allow users to list, inspect, stop, and remove containers.

In this exercise, you will implement a container state machine with durable state persistence. You will create a CLI with subcommands: `create`, `start`, `stop`, `rm`, `ps`, and `inspect`. The runtime maintains a state directory (e.g., `/var/run/mycontainer/`) where each container has a JSON state file recording its configuration, PID, status, creation time, and exit code. The state must survive runtime process restarts -- if your runtime crashes, restarting it should discover and report the status of still-running containers.

Signal forwarding is a critical detail. When the user sends `SIGTERM` or `SIGINT` to the runtime, it must forward the signal to the container's init process, wait for a graceful shutdown period, and then send `SIGKILL` if the process has not exited. The runtime must also handle the case where the container process dies unexpectedly (e.g., OOM kill) and update the state accordingly.

Container logging is the other piece: stdout and stderr from the container process must be captured and written to log files that can be retrieved later via a `logs` subcommand.

## Requirements

1. Implement a state machine with states: `creating`, `created`, `running`, `stopped`, `removed`
2. Create a state directory structure at `/var/run/mycontainer/<container-id>/` with `state.json` and `config.json`
3. Implement CLI subcommands: `create`, `start`, `stop`, `kill`, `rm`, `ps`, `inspect`, `logs`
4. Generate unique container IDs (e.g., truncated SHA256 of creation timestamp + random bytes)
5. Track the container process PID and detect unexpected exits via `os.Process.Wait` or polling `/proc/<pid>`
6. Forward `SIGTERM`, `SIGINT`, and `SIGKILL` to the container process
7. Implement graceful shutdown: `stop` sends `SIGTERM`, waits for `--timeout` seconds (default 10), then sends `SIGKILL`
8. Capture container stdout and stderr to log files in the state directory
9. The `ps` subcommand must list all containers with their ID, image, status, and creation time
10. The `inspect` subcommand must output the full container state as formatted JSON
11. The `rm` subcommand must refuse to remove running containers unless `--force` is specified
12. Recover state on restart: detect running containers by checking if their PID is still alive

## Hints

- Use `encoding/json` to serialize/deserialize state. Define a `ContainerState` struct with all necessary fields.
- For container IDs, `crypto/rand` + `encoding/hex` produces Docker-like random hex IDs.
- Use `os.Process.Signal` to forward signals. `syscall.Kill(pid, signal)` also works for sending signals by PID.
- To detect if a PID is alive: `syscall.Kill(pid, 0)` returns nil if the process exists, error otherwise.
- Capture stdout/stderr by setting `exec.Cmd.Stdout` and `exec.Cmd.Stderr` to `os.File` handles for log files.
- Use `signal.Notify` in the parent process to catch `SIGTERM` and `SIGINT` for forwarding.

## Success Criteria

1. `create` prepares the container filesystem and writes initial state without starting the process
2. `start` launches the container process and transitions state to `running`
3. `stop` gracefully terminates the container with signal escalation
4. `ps` lists all containers with correct status information
5. `inspect` outputs complete container state as JSON
6. `logs` retrieves captured stdout/stderr from the container
7. Container state survives runtime restarts -- restarting the runtime detects running containers
8. `rm` cleans up all container resources and state files

## Research Resources

- [OCI Runtime Spec: Container Lifecycle](https://github.com/opencontainers/runtime-spec/blob/main/runtime.md#lifecycle) -- standard container state transitions
- [OCI Runtime Spec: State](https://github.com/opencontainers/runtime-spec/blob/main/runtime.md#state) -- what state a runtime must track
- [runc source code](https://github.com/opencontainers/runc) -- reference OCI runtime implementation in Go
- [containerd source code](https://github.com/containerd/containerd) -- higher-level container manager
- [Signal handling in Go](https://pkg.go.dev/os/signal) -- signal notification and forwarding patterns
- [Docker container lifecycle](https://docs.docker.com/engine/reference/run/) -- practical lifecycle semantics

## What's Next

The next exercise implements `exec` functionality -- running additional processes inside an already-running container by entering its existing namespaces.

## Summary

- Container lifecycle follows a state machine: creating -> created -> running -> stopped -> removed
- Persistent state files enable container listing, inspection, and crash recovery
- Signal forwarding ensures that user signals reach the container init process for graceful shutdown
- Container logs are captured from stdout/stderr to files retrievable after container exit
- The runtime must handle unexpected container death (OOM kill, crash) and update state accordingly
- CLI subcommands mirror the OCI runtime specification operations
