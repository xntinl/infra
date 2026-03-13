# 9. Container Networking: Bridge and NAT

<!--
difficulty: insane
concepts: [linux-bridge, nat-iptables, port-forwarding, container-to-container-networking, bridge-networking-model, ip-masquerade]
tools: [go, linux, iptables, iproute2]
estimated_time: 3h
bloom_level: create
prerequisites: [section 38 exercises 1-8, section 33 tcp/udp networking, linux bridge and iptables concepts]
-->

## Prerequisites

- Go 1.22+ installed
- Linux environment with root access
- Completed exercises 1-8 (full container runtime with exec)
- Understanding of Linux bridge networking, iptables/nftables, and NAT
- Familiarity with IP routing and forwarding

## Learning Objectives

- **Create** a bridge networking driver that connects multiple containers through a shared virtual bridge with NAT for external access
- **Design** an iptables rule management system for port forwarding, masquerading, and inter-container communication policies
- **Evaluate** the Docker bridge networking model and the trade-offs between bridge, host, and macvlan networking modes

## The Challenge

Exercise 3 gave each container a veth pair with a direct connection to the host, but that approach does not scale. Real container networking uses a bridge: a virtual network switch that connects multiple containers on the same host. Docker creates `docker0`, a Linux bridge device. Each container's veth connects to this bridge, and iptables rules provide NAT (masquerade) for outbound traffic and DNAT for port forwarding.

In this exercise, you will implement the Docker bridge networking model from scratch. You will create a bridge device, connect container veth pairs to it, configure iptables rules for SNAT/masquerade (so containers can reach the internet), DNAT/port forwarding (so external traffic can reach containers), and inter-container communication filtering. You will also implement IP address management (IPAM) to assign unique addresses from a subnet to each container.

The complexity here is in iptables rule management. You need rules in the `nat` table for masquerading and port forwarding, and rules in the `filter` table for inter-container isolation policies. Rules must be idempotent (do not add duplicates on restart), cleaned up when containers exit, and not conflict with existing host firewall rules. You also need to enable IP forwarding on the host (`/proc/sys/net/ipv4/ip_forward`).

Port forwarding adds another dimension: when the user runs `--publish 8080:80`, traffic arriving at the host on port 8080 must be redirected to the container's IP on port 80. This requires DNAT rules in the `PREROUTING` chain and matching rules in the `FORWARD` chain.

## Requirements

1. Create a Linux bridge device (e.g., `mybridge0`) if it does not already exist
2. Assign an IP address to the bridge (e.g., `172.20.0.1/24`) and bring it up
3. Connect each container's veth pair to the bridge instead of a direct host connection
4. Implement IPAM: allocate IP addresses from the bridge subnet, track allocations, prevent duplicates
5. Configure iptables masquerade rule for the bridge subnet to enable outbound internet access
6. Enable IP forwarding on the host via `/proc/sys/net/ipv4/ip_forward`
7. Implement `--publish` flag for port forwarding using iptables DNAT rules
8. Add iptables FORWARD rules to allow traffic between containers on the same bridge
9. Implement `--network none` mode that creates a network namespace with only loopback
10. Set up DNS inside containers by writing `/etc/resolv.conf` with the host's DNS servers
11. Clean up iptables rules and bridge connections when containers are removed
12. Support multiple simultaneous containers sharing the same bridge

## Hints

- Use `netlink.LinkAdd(&netlink.Bridge{...})` to create the bridge device.
- Use `netlink.LinkSetMaster(veth, bridge)` to connect a veth to the bridge.
- For iptables, use `github.com/coreos/go-iptables/iptables` or call `iptables` commands directly.
- Masquerade rule: `iptables -t nat -A POSTROUTING -s 172.20.0.0/24 ! -o mybridge0 -j MASQUERADE`.
- DNAT rule: `iptables -t nat -A PREROUTING -p tcp --dport 8080 -j DNAT --to-destination 172.20.0.2:80`.
- IPAM can be simple: store allocated IPs in a JSON file, allocate from a sequential pool.

## Success Criteria

1. Multiple containers can communicate with each other via bridge IP addresses
2. Containers can reach the external internet (e.g., `ping 8.8.8.8`)
3. DNS resolution works inside containers
4. Port forwarding maps host ports to container ports correctly
5. External clients can reach containerized services via published ports
6. IP addresses are uniquely assigned and tracked across container lifecycles
7. All iptables rules are cleaned up when containers are removed
8. The bridge persists across container restarts and handles multiple concurrent containers

## Research Resources

- [Docker bridge networking](https://docs.docker.com/network/bridge/) -- the networking model you are implementing
- [Linux bridge documentation](https://wiki.linuxfoundation.org/networking/bridge) -- kernel bridge device details
- [iptables tutorial](https://www.frozentux.net/iptables-tutorial/iptables-tutorial.html) -- comprehensive iptables reference
- [go-iptables library](https://github.com/coreos/go-iptables) -- Go bindings for iptables management
- [CNI bridge plugin source](https://github.com/containernetworking/plugins/tree/main/plugins/main/bridge) -- reference CNI implementation
- [Linux IP forwarding](https://www.kernel.org/doc/Documentation/networking/ip-sysctl.txt) -- kernel sysctl documentation

## What's Next

The final exercise brings everything together into a complete OCI-compliant container runtime with a unified CLI, comprehensive error handling, and production-ready features.

## Summary

- Bridge networking connects multiple containers through a virtual switch, mirroring the Docker `bridge` driver
- Iptables masquerade (SNAT) enables outbound internet access from the container subnet
- DNAT rules implement port forwarding from host ports to container IPs
- IPAM tracks IP address allocation from the bridge subnet to prevent conflicts
- IP forwarding must be enabled on the host for cross-namespace traffic routing
- Proper cleanup of iptables rules and bridge connections is critical to prevent network leaks
