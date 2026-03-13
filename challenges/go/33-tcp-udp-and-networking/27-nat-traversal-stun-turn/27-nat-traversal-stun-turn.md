# 27. NAT Traversal with STUN/TURN

<!--
difficulty: insane
concepts: [nat-traversal, stun, turn, hole-punching, udp-hole-punch, symmetric-nat, relay, ice]
tools: [go]
estimated_time: 3h
bloom_level: create
prerequisites: [udp-server-and-client, tcp-server-and-client, dns-resolver-and-custom-dialer, vpn-tunnel-implementation]
-->

## Prerequisites

- Go 1.22+ installed
- Completed UDP Server/Client and VPN Tunnel exercises
- Understanding of NAT types (full cone, restricted cone, port-restricted, symmetric)
- Familiarity with UDP socket programming and address binding

## Learning Objectives

- **Create** a STUN server and client that discovers public IP addresses and NAT types
- **Implement** UDP hole punching for peer-to-peer connectivity through NAT devices
- **Design** a TURN relay server that forwards traffic when direct peer-to-peer connectivity is impossible

## The Challenge

Most devices are behind NAT (Network Address Translation), which means they do not have a public IP address that peers can connect to directly. Peer-to-peer applications (video calls, file sharing, gaming) need NAT traversal to establish direct connections.

STUN (Session Traversal Utilities for NAT) lets a client discover its public IP address and port mapping by querying a server on the public internet. UDP hole punching uses this information to establish direct peer-to-peer connectivity by having both sides send packets to each other's public address simultaneously, "punching" a hole in both NATs. When hole punching fails (symmetric NAT), TURN (Traversal Using Relays around NAT) provides a relay server that forwards traffic between peers.

Your task is to implement all three components: a STUN server that reports clients' public addresses, a hole-punching coordinator that facilitates direct peer connections, and a TURN relay for cases where direct connectivity fails.

## Requirements

1. Implement a STUN server per RFC 5389: receive binding requests over UDP, extract the client's source IP and port from the UDP header, and respond with a binding response containing the mapped address in a `XOR-MAPPED-ADDRESS` attribute
2. Implement a STUN client that sends a binding request and parses the response to discover its public IP address and port
3. Implement NAT type detection: by sending requests from multiple source ports and comparing the mapped addresses, determine if the client is behind full cone, restricted cone, port-restricted cone, or symmetric NAT
4. Implement a signaling server that coordinates hole punching: two peers register with the server, exchange public addresses, and the server signals both to begin sending packets simultaneously
5. Implement UDP hole punching: both peers send UDP packets to each other's public address (learned via STUN) on a timer until they receive a packet from the peer, at which point the connection is established
6. Implement a TURN relay server: peers that cannot establish direct connectivity allocate a relay address on the TURN server, which forwards all traffic between them
7. Implement TURN client: send allocation requests to the TURN server, receive a relay address, and send data through the relay using channel bindings or send indications
8. Build an end-to-end demo: two peers behind NAT attempt direct connectivity via hole punching; if it fails (timeout), fall back to TURN relay
9. Implement STUN message encoding/decoding: 20-byte header (type, length, magic cookie, transaction ID) followed by TLV attributes
10. Write tests using loopback UDP sockets simulating NAT behavior

## Hints

- STUN messages use a 20-byte header: 2-byte type, 2-byte length, 4-byte magic cookie (0x2112A442), 12-byte transaction ID
- `XOR-MAPPED-ADDRESS` XORs the port with the top 16 bits of the magic cookie and the IP with the full magic cookie -- this prevents NATs from rewriting addresses inside the payload
- For hole punching, both peers must send packets nearly simultaneously; the signaling server coordinates the start time
- Simulate NAT in tests by creating a "NAT proxy" goroutine that translates source addresses on outgoing packets and maintains a mapping table
- TURN allocation uses the ALLOCATE request type (0x0003); the server responds with a `XOR-RELAYED-ADDRESS` attribute containing the relay address
- ICE (Interactive Connectivity Establishment) is the full framework that combines STUN, TURN, and candidate prioritization; this exercise implements the core components

## Success Criteria

1. The STUN server correctly reports the client's public mapped address in binding responses
2. STUN message encoding/decoding handles the XOR-MAPPED-ADDRESS attribute correctly
3. NAT type detection correctly identifies the simulated NAT type
4. UDP hole punching establishes direct connectivity between two peers behind simulated NAT
5. When hole punching fails (symmetric NAT simulation), TURN relay successfully forwards traffic
6. The signaling server coordinates peer discovery and hole-punch initiation
7. End-to-end data transfer works through both direct (hole-punched) and relayed (TURN) paths
8. All tests pass with the `-race` flag enabled

## Research Resources

- [RFC 5389 -- STUN](https://datatracker.ietf.org/doc/html/rfc5389) -- Session Traversal Utilities for NAT
- [RFC 5766 -- TURN](https://datatracker.ietf.org/doc/html/rfc5766) -- Traversal Using Relays around NAT
- [RFC 8445 -- ICE](https://datatracker.ietf.org/doc/html/rfc8445) -- Interactive Connectivity Establishment
- [How NAT Traversal Works (Tailscale)](https://tailscale.com/blog/how-nat-traversal-works/) -- excellent practical overview
- [pion/stun](https://github.com/pion/stun) -- Go STUN implementation (reference)
- [pion/turn](https://github.com/pion/turn) -- Go TURN implementation (reference)

## What's Next

Continue to [28 - Packet Sniffer with BPF](../28-packet-sniffer-bpf/28-packet-sniffer-bpf.md) to capture and analyze raw network packets using BPF filters.

## Summary

- STUN discovers a client's public IP and port by reflecting the source address of incoming UDP packets
- NAT type determines whether direct peer-to-peer connectivity is possible
- UDP hole punching works by having both peers send packets simultaneously, creating NAT mappings that allow return traffic
- TURN provides a relay when direct connectivity is impossible, at the cost of increased latency and server bandwidth
- STUN messages use a fixed header with magic cookie and transaction ID, followed by TLV-encoded attributes
- ICE combines STUN, TURN, and candidate gathering into a complete connectivity framework
