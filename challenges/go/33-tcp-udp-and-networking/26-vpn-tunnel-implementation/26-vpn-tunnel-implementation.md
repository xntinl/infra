# 26. VPN Tunnel Implementation

<!--
difficulty: insane
concepts: [vpn, tun-device, ip-tunneling, packet-encapsulation, point-to-point, encryption, routing, mtu]
tools: [go]
estimated_time: 3h
bloom_level: create
prerequisites: [udp-server-and-client, tls-server-and-client, mutual-tls-authentication, custom-wire-protocol]
-->

## Prerequisites

- Go 1.22+ installed
- Completed UDP Server/Client, TLS, and Custom Wire Protocol exercises
- Understanding of IP networking: IP packets, routing tables, subnets
- Familiarity with TUN/TAP devices (virtual network interfaces)
- Root/admin access for creating TUN devices

## Learning Objectives

- **Create** a point-to-point VPN tunnel that encapsulates IP packets over an encrypted UDP transport
- **Implement** TUN device interaction in Go: reading IP packets from and writing them to a virtual network interface
- **Design** a packet encapsulation protocol with encryption, authentication, and sequence numbering

## The Challenge

A VPN creates a virtual network link between two hosts over an untrusted network. It works by creating a TUN device (a virtual network interface that operates at the IP layer), capturing all IP packets routed through it, encrypting and encapsulating them, sending them over UDP to the peer, which decrypts and injects them into its own TUN device. From the perspective of applications, the TUN device looks like a normal network interface.

Your task is to build a simple point-to-point VPN tunnel. Each side runs a daemon that creates a TUN device, establishes a UDP connection to the peer, and relays IP packets bidirectionally. Packets are encrypted with AES-GCM and authenticated to prevent tampering. The system must handle MTU properly (encapsulated packets are larger than the originals) and detect dead peers through keepalive messages.

## Requirements

1. Create and configure a TUN device using the `songgao/water` library or direct `syscall`/`unix.Open` on `/dev/net/tun`
2. Read IP packets from the TUN device, encapsulate them in a custom protocol with header (sequence number, timestamp, payload length), encrypt the payload with AES-256-GCM, and send over UDP to the peer
3. Receive encapsulated packets from the peer over UDP, decrypt, verify authenticity, and write the decrypted IP packet to the local TUN device
4. Implement key exchange: use a pre-shared key for initial setup, or implement a Diffie-Hellman key exchange at connection startup to derive session keys
5. Handle MTU correctly: set the TUN device MTU to account for encapsulation overhead (UDP header + encryption overhead + custom header) so that applications do not send packets that would exceed the underlying network MTU
6. Implement keepalive: send periodic keepalive packets; declare the peer dead if no packets are received within a configurable timeout
7. Implement sequence numbers to detect replay attacks: reject packets with sequence numbers older than a sliding window
8. Configure routing: add a route to the peer's virtual subnet through the TUN device
9. Support both IPv4 and IPv6 packets through the tunnel
10. Write tests that verify packet encapsulation/decapsulation round-trip, encryption, and replay detection

## Hints

- The `songgao/water` library provides a cross-platform TUN device API for Go; on Linux you can also use `os.OpenFile("/dev/net/tun", ...)` with `ioctl` to create TUN devices
- TUN devices deliver raw IP packets (no Ethernet header); the first 4 bytes indicate the protocol (use IFF_NO_PI flag to skip this)
- AES-GCM from `crypto/aes` + `crypto/cipher` provides both encryption and authentication in a single operation
- UDP is the right transport because encapsulated IP packets should not have TCP's reliability semantics layered underneath (that would cause TCP-over-TCP performance issues)
- Set MTU = underlying_MTU - UDP_header(8) - IP_header(20) - custom_header - GCM_overhead(28) to prevent fragmentation
- Use `exec.Command("ip", "addr", "add", ...)` or `netlink` to configure the TUN device address and routes programmatically

## Success Criteria

1. Two instances of the VPN daemon establish a tunnel and can ping each other over the virtual interface
2. IP packets are encrypted with AES-256-GCM before transmission over UDP
3. Tampering with encrypted packets is detected and the packets are dropped
4. Replay attack detection rejects packets with old sequence numbers
5. Keepalive detects peer failure within the configured timeout
6. MTU is set correctly so that applications do not experience fragmentation
7. Both IPv4 and IPv6 packets traverse the tunnel
8. Encryption/decryption round-trip tests pass with the `-race` flag enabled

## Research Resources

- [songgao/water](https://github.com/songgao/water) -- Go TUN/TAP library
- [WireGuard Protocol](https://www.wireguard.com/protocol/) -- modern VPN protocol design reference
- [OpenVPN Architecture](https://openvpn.net/community-resources/how-to/) -- classical VPN implementation
- [crypto/cipher GCM](https://pkg.go.dev/crypto/cipher#NewGCM) -- AES-GCM authenticated encryption
- [Linux TUN/TAP Documentation](https://www.kernel.org/doc/Documentation/networking/tuntap.txt)
- [RFC 4301 -- IPsec Architecture](https://datatracker.ietf.org/doc/html/rfc4301) -- reference for tunnel-mode encryption

## What's Next

Continue to [27 - NAT Traversal with STUN/TURN](../27-nat-traversal-stun-turn/27-nat-traversal-stun-turn.md) to implement NAT traversal techniques for peer-to-peer connectivity.

## Summary

- VPN tunnels encapsulate IP packets inside encrypted UDP datagrams between two endpoints
- TUN devices are virtual network interfaces that operate at the IP layer, capturing routed packets for the VPN daemon to process
- AES-GCM provides authenticated encryption: encryption and integrity verification in one operation
- MTU must account for encapsulation overhead to prevent fragmentation on the underlying network
- Sequence numbers and sliding windows prevent replay attacks on the encrypted channel
- Keepalive messages detect dead peers and trigger reconnection or failover
