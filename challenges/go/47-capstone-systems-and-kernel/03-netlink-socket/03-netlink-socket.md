<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 3h
-->

# Netlink Socket Interface

## The Challenge

Build a Go library that communicates with the Linux kernel via Netlink sockets to query and manipulate network configuration: listing network interfaces, managing IP addresses, manipulating routing tables, and monitoring network events in real time. Netlink is the primary IPC mechanism between the Linux kernel and userspace for networking subsystem configuration, replacing the older `ioctl`-based approach. Your library must construct and parse Netlink messages at the binary protocol level, handle the multi-part message protocol, support request-response and subscription-based (multicast) communication, and implement the RTNETLINK protocol family for route and address management.

## Requirements

1. Implement a Netlink socket abstraction that opens an `AF_NETLINK` socket with the `NETLINK_ROUTE` protocol using raw `syscall.Socket`, supports `Bind` with a Netlink address specifying multicast groups, and handles `Send`/`Recv` with proper Netlink message framing.
2. Implement Netlink message construction: build messages with the standard 16-byte Netlink header (`nlmsghdr`: length, type, flags, sequence number, PID) followed by protocol-specific payloads, with proper alignment to 4-byte boundaries using `NLMSG_ALIGN`.
3. Implement Netlink message parsing: parse response messages handling multi-part responses (`NLM_F_MULTI` flag with `NLMSG_DONE` terminator), error responses (`NLMSG_ERROR` with errno), and nested TLV (type-length-value) attributes (`rtattr`).
4. Implement `ListLinks()` that sends an `RTM_GETLINK` dump request and parses the responses to return a list of network interfaces with their names, indices, MAC addresses, MTU, and operational state by parsing `ifinfomsg` and its nested attributes.
5. Implement `ListAddrs(ifIndex int)` that sends an `RTM_GETADDR` dump request filtered by interface index and returns the IP addresses (both IPv4 and IPv6) with prefix lengths by parsing `ifaddrmsg` and its attributes.
6. Implement `AddAddr(ifIndex int, addr netip.Prefix)` and `DelAddr(ifIndex int, addr netip.Prefix)` that send `RTM_NEWADDR` and `RTM_DELADDR` requests with the appropriate flags and attributes to add or remove IP addresses from interfaces.
7. Implement `ListRoutes()` and `AddRoute(dst netip.Prefix, gateway netip.Addr, ifIndex int)` for reading and manipulating the routing table using `RTM_GETROUTE`, `RTM_NEWROUTE`, and `RTM_DELROUTE` messages with `rtmsg` headers.
8. Implement a Netlink event monitor that subscribes to `RTNLGRP_LINK`, `RTNLGRP_IPV4_ADDR`, and `RTNLGRP_IPV4_ROUTE` multicast groups and streams real-time notifications of network changes (interface up/down, address added/removed, route changes) to a Go channel.

## Hints

- Use `syscall.Socket(syscall.AF_NETLINK, syscall.SOCK_RAW|syscall.SOCK_CLOEXEC, syscall.NETLINK_ROUTE)` to create the socket.
- Netlink messages must be aligned to 4 bytes; use `(len + 3) & ^3` for alignment calculation.
- The dump flag `NLM_F_DUMP` (which is `NLM_F_ROOT | NLM_F_MATCH`) requests all entries; without it, you get a single entry.
- Each attribute (`rtattr`) has a 4-byte header (2-byte length, 2-byte type) followed by the payload; attributes can be nested (indicated by the `NLA_F_NESTED` flag on the type).
- Use `binary.LittleEndian` for all Netlink message encoding/decoding on x86_64 (Netlink uses host byte order, not network byte order).
- For multicast subscriptions, set the `Groups` field in `SockaddrNetlink` to the bitmask of desired groups, or use `setsockopt` with `NETLINK_ADD_MEMBERSHIP`.
- The `vishvananda/netlink` Go library is the standard implementation -- study it for reference but implement your own from scratch.
- Test route and address manipulation in a network namespace (`ip netns`) to avoid modifying the host's configuration.

## Success Criteria

1. `ListLinks()` correctly returns all network interfaces matching `ip link show` output.
2. `ListAddrs()` correctly returns all IP addresses matching `ip addr show` output.
3. `AddAddr` and `DelAddr` correctly add and remove IP addresses, verified with `ip addr show` (tested in a network namespace).
4. `ListRoutes()` correctly returns the routing table matching `ip route show`.
5. `AddRoute` and `DelRoute` correctly manipulate routes, verified with `ip route show` (tested in a network namespace).
6. The event monitor correctly detects and reports interface state changes (link up/down) within 100 ms of the event.
7. All Netlink messages are correctly aligned and all multi-part responses are fully consumed.
8. Error responses from the kernel are correctly parsed and returned as Go errors with descriptive messages.

## Research Resources

- Linux Netlink man page -- `man 7 netlink`, `man 7 rtnetlink`
- RFC 3549 -- "Linux Netlink as an IP Services Protocol"
- "Understanding Linux Networking Internals" (Benvenuti, 2006)
- vishvananda/netlink Go library -- https://github.com/vishvananda/netlink
- iproute2 source code -- the implementation behind `ip` command
- Linux kernel Netlink header -- https://github.com/torvalds/linux/blob/master/include/uapi/linux/netlink.h
