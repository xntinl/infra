# 122. Container Runtime from Scratch

<!--
difficulty: insane
category: containers-and-systems
languages: [go]
concepts: [oci-runtime-spec, linux-namespaces, cgroups-v2, pivot-root, capability-dropping, seccomp, veth-networking, bridge-networking, image-layers, container-lifecycle]
estimated_time: 40-60 hours
bloom_level: create
prerequisites: [linux-namespaces, cgroups-v2, go-syscalls, networking, oci-spec-basics, json-parsing, tar-extraction, seccomp-basics]
-->

## Languages

- Go 1.22+

## Prerequisites

- Linux namespaces: PID, mount, network, UTS, user (challenge 104)
- Cgroups v2: CPU, memory, PID limits (challenge 94)
- Go `syscall` package: clone flags, mount, pivot_root, exec
- Networking: veth pairs, bridge interfaces, IP addressing, routing
- OCI runtime specification basics (JSON config format)
- Tar archive extraction and filesystem layer operations
- Seccomp BPF filter concepts
- **Linux required**: this challenge uses Linux-specific kernel features extensively. macOS users must use a Linux VM or `docker run --privileged -v /path:/work -it ubuntu:latest`

## Learning Objectives

- **Create** an OCI-compatible container runtime that implements the full container lifecycle
- **Synthesize** namespace isolation, cgroup resource limits, filesystem layering, and networking into a cohesive system
- **Evaluate** the security posture of the runtime by analyzing capability dropping and seccomp filtering
- **Design** a layered filesystem using overlay mounts or sequential tar extraction

## The Challenge

Every time you run `docker run`, the Docker daemon calls a container runtime -- typically `runc` -- to create the actual container. The runtime handles the low-level kernel operations: creating namespaces, setting up cgroups, preparing the filesystem, configuring networking, and executing the container process.

The OCI (Open Container Initiative) runtime specification defines how a container runtime should behave. It specifies the JSON configuration format (`config.json`), the container lifecycle states (creating, created, running, stopped), and the operations a runtime must support (create, start, kill, delete).

Build an OCI-compatible container runtime. This means combining everything from challenges 94 (cgroups) and 104 (namespaces) into a single tool, and adding: root filesystem setup from OCI image layers, capability dropping, basic seccomp filtering, veth+bridge networking, and OCI lifecycle management.

This is one of the most demanding systems programming challenges. You are building the same software that runs every container on every server in every cloud.

## Requirements

1. **Namespace creation**: Create PID, mount, network, UTS, and user namespaces for the container process. The container process sees PID 1, its own hostname, an isolated network stack, and a private filesystem
2. **Cgroup resource limits**: Create a cgroup for the container. Support CPU limits (`cpu.max`), memory limits (`memory.max`), and PID limits (`pids.max`). Clean up the cgroup on container deletion
3. **Root filesystem setup**: Accept a directory containing OCI image layers (tar files). Extract layers in order to build the root filesystem. Support the basic layer model: each layer is applied on top of the previous one. Handle whiteout files (`.wh.` prefix) for layer deletion
4. **pivot_root**: Set up the root filesystem, mount `/proc`, create minimal `/dev` entries, then `pivot_root` to the new root. The host filesystem must not be accessible from inside the container
5. **Capability dropping**: Drop all Linux capabilities except a configurable whitelist. Default whitelist: `CAP_NET_BIND_SERVICE`, `CAP_KILL`, `CAP_CHOWN`, `CAP_SETUID`, `CAP_SETGID`. Use `prctl(PR_CAPBSET_DROP)`
6. **Seccomp filter**: Install a basic seccomp BPF filter that whitelists common syscalls and blocks dangerous ones (`kexec_load`, `reboot`, `mount` unless explicitly allowed, `ptrace`). The filter must be installed before exec
7. **Networking**: Create a veth pair. Place one end in the container's network namespace, the other on the host. Create a bridge interface on the host. Assign IP addresses and configure routing so the container can communicate with the host
8. **Container lifecycle**: Implement `create` (set up namespaces and cgroup, do not start the process), `start` (exec the process), `kill` (send signal to the container process), and `delete` (clean up cgroup, mounts, and state). Maintain state in a JSON file
9. **OCI config.json compatibility**: Read the OCI runtime spec `config.json` for: root filesystem path, process args, environment variables, hostname, resource limits, and capability lists. The runtime does not need to support every field -- target the essential subset
10. **CLI interface**: `runtime create <id> --bundle <path>`, `runtime start <id>`, `runtime kill <id> <signal>`, `runtime delete <id>`, `runtime state <id>`

## Hints

Minimal guidance for this difficulty level. Read the OCI runtime spec carefully and study runc's source code.

The veth+bridge networking setup involves: `ip link add veth0 type veth peer name veth1`, moving `veth1` into the container namespace, assigning IPs, adding the host-side veth to a bridge, and setting up routes. The `netlink` Go package simplifies this.

## Acceptance Criteria

- [ ] Container process runs in isolated PID, mount, network, UTS, and user namespaces
- [ ] Cgroup limits are enforced: CPU-bound process is throttled, memory-exceeding process is OOM-killed, fork bomb is contained
- [ ] Root filesystem is correctly assembled from OCI image layers with whiteout support
- [ ] `pivot_root` isolates the container from the host filesystem
- [ ] Capabilities are dropped: the container cannot perform privileged operations outside the whitelist
- [ ] Seccomp filter blocks dangerous syscalls (verify `reboot` syscall fails with EPERM)
- [ ] Networking works: container can ping the host and vice versa via the veth bridge
- [ ] Lifecycle management works: create/start/kill/delete transitions are correct
- [ ] State is persisted: `runtime state <id>` returns the correct lifecycle state
- [ ] `config.json` is parsed and applied correctly for the supported fields
- [ ] Clean shutdown: all namespaces, cgroups, network interfaces, and mount points are cleaned up on delete
- [ ] The runtime handles errors gracefully at every stage with clean rollback

## Research Resources

- [OCI Runtime Specification](https://github.com/opencontainers/runtime-spec/blob/main/spec.md) -- the authoritative specification your runtime must follow
- [OCI Runtime Spec: config.json](https://github.com/opencontainers/runtime-spec/blob/main/config.md) -- container configuration format
- [runc source code](https://github.com/opencontainers/runc) -- the reference OCI runtime implementation in Go
- [Containers from Scratch (Liz Rice)](https://www.youtube.com/watch?v=8fi7uSYlOdc) -- building a container in Go
- [Linux Capabilities man page: capabilities(7)](https://man7.org/linux/man-pages/man7/capabilities.7.html) -- capability system documentation
- [Seccomp BPF (kernel docs)](https://www.kernel.org/doc/html/latest/userspace-api/seccomp_filter.html) -- seccomp filter programming
- [vishvananda/netlink](https://github.com/vishvananda/netlink) -- Go library for Linux netlink (veth, bridge, route operations)
- [libseccomp-golang](https://github.com/seccomp/libseccomp-golang) -- Go bindings for seccomp
- [Build Your Own Container Runtime (Ivan Velichko)](https://iximiuz.com/en/posts/container-networking-is-simple/) -- container networking walkthrough
