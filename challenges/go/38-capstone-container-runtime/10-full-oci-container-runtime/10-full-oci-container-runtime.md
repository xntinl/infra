# 10. Full OCI Container Runtime

<!--
difficulty: insane
concepts: [oci-runtime-spec, container-runtime-interface, unified-cli, init-system, seccomp-filters, capability-dropping]
tools: [go, linux, oci-runtime-spec]
estimated_time: 4h+
bloom_level: create
prerequisites: [section 38 exercises 1-9, oci runtime specification, linux security primitives]
-->

## Prerequisites

- Go 1.22+ installed
- Linux environment with root access
- Completed exercises 1-9 (all previous container runtime components)
- Familiarity with the OCI Runtime Specification
- Understanding of Linux capabilities, seccomp, and AppArmor/SELinux concepts

## Learning Objectives

- **Create** a complete OCI-compliant container runtime that integrates all components into a production-quality tool
- **Design** a security hardening layer with capability dropping, seccomp filters, and read-only filesystem options
- **Evaluate** the runtime against the OCI Runtime Specification compliance requirements and real-world container workloads

## The Challenge

You have built every major component of a container runtime across the previous nine exercises. Now it is time to bring them all together into a cohesive, OCI-compliant runtime -- one that can actually run real container images with proper security defaults, comprehensive error handling, and a polished CLI interface.

An OCI-compliant runtime must implement the operations defined in the OCI Runtime Specification: `create`, `start`, `kill`, `delete`, and `state`. It must read an OCI bundle (a directory with a `config.json` and a rootfs) and create a container matching the specification. The `config.json` file defines everything: namespaces, mounts, resource limits, security profiles, process parameters, and hooks.

In this final exercise, you will implement OCI bundle support, reading the standard `config.json` to configure namespaces, mounts, cgroups, and process settings. You will add security hardening: dropping all Linux capabilities except those explicitly granted, applying a default seccomp filter that blocks dangerous system calls, and optionally making the root filesystem read-only. You will implement lifecycle hooks (prestart, createRuntime, createContainer, startContainer, poststart, poststop) that allow external tools to integrate with your runtime. And you will ensure the runtime can serve as a drop-in backend for higher-level tools -- it should be possible to configure containerd or podman to use your runtime.

This is the culmination of the entire container runtime capstone. The result is a tool that demonstrates deep understanding of Linux containers, systems programming in Go, and the standards that make the container ecosystem interoperable.

## Requirements

1. Parse OCI `config.json` bundles and configure the container accordingly
2. Implement all OCI runtime operations: `create`, `start`, `kill`, `delete`, `state`
3. Drop all Linux capabilities except those listed in the config using `capset(2)` or `prctl(2)`
4. Implement a default seccomp filter that blocks dangerous syscalls (e.g., `kexec_load`, `reboot`)
5. Support read-only root filesystem via the `root.readonly` config option
6. Implement OCI lifecycle hooks: prestart, poststart, poststop
7. Support user namespace mapping when `user` namespace is configured (`uid_map`, `gid_map`)
8. Implement the `state` command output format as defined by the OCI spec (JSON with `ociVersion`, `id`, `status`, `pid`, `bundle`)
9. Integrate all components: namespaces, rootfs/overlayfs, cgroups, bridge networking, image pulling
10. Produce the OCI-specified exit status file for container process exit codes
11. Implement `--bundle` flag pointing to an OCI bundle directory
12. Add comprehensive logging with configurable log levels and log file output

## Hints

- The OCI `config.json` schema is defined at [runtime-spec/config.md](https://github.com/opencontainers/runtime-spec/blob/main/config.md). Use Go structs matching the JSON schema.
- For capabilities, use `golang.org/x/sys/unix` with `unix.Prctl` to set the bounding, effective, permitted, inheritable, and ambient capability sets.
- For seccomp, use `libseccomp-golang` or write BPF filters directly with `unix.SockFprog`.
- The `state` command must output JSON to stdout. The format is: `{"ociVersion": "1.0.2", "id": "<id>", "status": "<status>", "pid": <pid>, "bundle": "<path>"}`.
- Lifecycle hooks are executables called at specific points. Use `exec.Command` with a timeout to run them.
- Test OCI compliance by running `oci-runtime-tool` from the OCI project, which validates runtime behavior.

## Success Criteria

1. The runtime reads and applies OCI `config.json` bundles correctly
2. All OCI operations (`create`, `start`, `kill`, `delete`, `state`) work per specification
3. Linux capabilities are correctly dropped to the minimum required set
4. Seccomp filters block dangerous system calls without affecting normal operation
5. Read-only root filesystem prevents writes when configured
6. Lifecycle hooks execute at the correct points with proper timeout handling
7. The runtime can run real-world container images (Alpine, Ubuntu, Nginx)
8. The `state` command output matches the OCI specification format

## Research Resources

- [OCI Runtime Specification](https://github.com/opencontainers/runtime-spec/blob/main/spec.md) -- the authoritative standard you are implementing
- [OCI Runtime Spec: config.json](https://github.com/opencontainers/runtime-spec/blob/main/config.md) -- full container configuration schema
- [runc source code](https://github.com/opencontainers/runc) -- the reference OCI runtime implementation
- [Linux capabilities(7)](https://man7.org/linux/man-pages/man7/capabilities.7.html) -- capability types and system call requirements
- [seccomp(2) man page](https://man7.org/linux/man-pages/man2/seccomp.2.html) -- seccomp-BPF system call filtering
- [runtime-tools validation](https://github.com/opencontainers/runtime-tools) -- OCI compliance testing tool

## What's Next

Congratulations -- you have built a container runtime from scratch. You now understand at a systems level what Docker, containerd, and every other container runtime does. Consider exploring container orchestration (Kubernetes CRI), rootless containers (user namespaces without root), or contributing to an open-source runtime project.

## Summary

- An OCI-compliant runtime implements standardized operations for container lifecycle management
- The `config.json` bundle format defines all aspects of container configuration in a portable way
- Security hardening (capabilities, seccomp, read-only rootfs) is essential for production container runtimes
- Lifecycle hooks enable integration with external tools and monitoring systems
- The runtime integrates all previous exercises: namespaces, filesystems, cgroups, networking, and image management
- Building a container runtime from scratch provides deep understanding of Linux systems programming and container internals
