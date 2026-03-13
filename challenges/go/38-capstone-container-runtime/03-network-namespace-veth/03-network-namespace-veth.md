# 3. Network Namespace and Veth Pairs

<!--
difficulty: insane
concepts: [network-namespace, veth-pairs, netlink, ip-address-assignment, namespace-fd, network-isolation]
tools: [go, linux, iproute2]
estimated_time: 2h
bloom_level: create
prerequisites: [section 38 exercises 1-2, section 33 tcp/udp networking, linux networking fundamentals]
-->

## Prerequisites

- Go 1.22+ installed
- Linux environment with root access
- Completed exercises 1-2 (UTS, PID, and mount namespaces)
- Familiarity with Linux networking concepts (interfaces, IP addresses, routing)
- The `iproute2` package installed on your host for verification

## Learning Objectives

- **Create** network namespace isolation with veth pair connectivity from Go
- **Design** a netlink-based interface configuration system that assigns addresses and brings up interfaces programmatically
- **Evaluate** the network isolation guarantees and connectivity paths between host and container namespaces

## The Challenge

A container with its own process tree and filesystem is still sharing the host's network stack. It can bind to any port, see all network interfaces, and sniff traffic. The network namespace (`CLONE_NEWNET`) gives each container its own network stack -- its own interfaces, routing table, iptables rules, and sockets. But a network namespace starts completely empty: not even a loopback interface.

To connect the container to the outside world, you need a veth pair -- a virtual ethernet cable with one end in the host namespace and one end in the container namespace. You create the pair in the host, move one end into the container's network namespace, and configure IP addresses on both ends. This is exactly what Docker and CNI plugins do under the hood.

In this exercise, you will use the `vishvananda/netlink` library (or raw netlink syscalls) to create veth pairs, move interfaces between namespaces, assign IP addresses, bring interfaces up, and configure routing. You will also bring up the loopback interface inside the container so that `localhost` works.

The tricky part is namespace file descriptor management. You need to hold a reference to the container's network namespace (via `/proc/<pid>/ns/net`) and use `netlink.LinkSetNsFd` to move interfaces. Timing matters: you must create the pair before the child's network namespace is fully configured.

## Requirements

1. Add `CLONE_NEWNET` to the clone flags so the child process gets its own network namespace
2. Create a veth pair with a deterministic naming scheme (e.g., `veth0-<id>` on host, `eth0` in container)
3. Move one end of the veth pair into the container's network namespace using the namespace file descriptor
4. Assign an IP address and subnet to the host end of the veth pair (e.g., `10.10.10.1/24`)
5. Assign an IP address and subnet to the container end (e.g., `10.10.10.2/24`)
6. Bring up both ends of the veth pair and the loopback interface inside the container
7. Add a default route inside the container pointing to the host end's IP address
8. Verify connectivity by pinging between host and container
9. Clean up the veth pair on the host side when the container exits
10. Handle errors for all netlink operations with descriptive messages

## Hints

- Use `github.com/vishvananda/netlink` -- it provides a Go-native netlink interface far easier than raw syscalls.
- Get the container's namespace fd with `netlink.GetNetNsFromPid(pid)` or by opening `/proc/<pid>/ns/net`.
- The container process should wait (e.g., on a pipe or signal) for the parent to finish network setup before proceeding with its workload.
- Bring up loopback with `netlink.LinkSetUp` on the `lo` interface inside the container namespace. Without it, `localhost` connections fail.
- Use `netlink.LinkSetNsFd(link, int(fd))` to move a link into a different namespace. The link disappears from the current namespace.
- For cleanup, deleting one end of a veth pair automatically deletes the other end.

## Success Criteria

1. The container has its own network namespace with only `lo` and `eth0` visible
2. The host sees a `veth0-<id>` interface connected to the container
3. Pinging from host to container IP succeeds
4. Pinging from container to host IP succeeds
5. The container cannot see or access the host's other network interfaces
6. Loopback (`127.0.0.1`) works inside the container
7. The veth pair is cleaned up when the container exits
8. `go vet` and `go build` succeed without warnings

## Research Resources

- [network_namespaces(7) man page](https://man7.org/linux/man-pages/man7/network_namespaces.7.html) -- network namespace semantics
- [veth(4) man page](https://man7.org/linux/man-pages/man4/veth.4.html) -- virtual ethernet device pairs
- [vishvananda/netlink Go library](https://github.com/vishvananda/netlink) -- Go bindings for Linux netlink
- [Container Networking from Scratch (talk)](https://www.youtube.com/watch?v=6v_BDHIgOY8) -- practical walkthrough of container networking
- [CNI specification](https://github.com/containernetworking/cni/blob/main/SPEC.md) -- how Kubernetes network plugins work

## What's Next

With process, filesystem, and basic network isolation in place, the next exercise adds resource limits using cgroups v2 to constrain CPU and memory usage.

## Summary

- Network namespaces (`CLONE_NEWNET`) provide complete network stack isolation including interfaces, routing, and sockets
- Veth pairs act as virtual cables connecting two network namespaces
- The `vishvananda/netlink` library provides idiomatic Go access to Linux netlink operations
- Namespace file descriptors from `/proc/<pid>/ns/net` allow moving interfaces between namespaces
- Loopback must be explicitly brought up inside new network namespaces
- Proper cleanup requires deleting host-side veth interfaces when the container exits
