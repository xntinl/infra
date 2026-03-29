# 123. TCP/IP Stack Userspace

<!--
difficulty: insane
category: networking-and-protocols
languages: [rust]
concepts: [tcp-ip-stack, tun-device, ethernet-frames, ip-packets, arp, icmp, tcp-state-machine, congestion-control, retransmission, rtt-estimation, socket-api, network-layers]
estimated_time: 60-100 hours
bloom_level: create
prerequisites: [rust-advanced, tcp-fundamentals, ip-networking, binary-encoding, async-rust, state-machines, systems-programming]
-->

## Languages

- Rust (stable)

## Prerequisites

- Advanced Rust: traits, generics, lifetimes, unsafe (for TUN/TAP), async with `tokio`
- TCP internals: three-way handshake, sequence numbers, sliding window, congestion control
- IP networking: IPv4 packet structure, routing, fragmentation, TTL
- ARP protocol: request/reply, cache management, timeout
- ICMP: echo request/reply, destination unreachable, time exceeded
- TUN/TAP virtual network interfaces on Linux
- Binary protocol encoding and checksum algorithms

## Learning Objectives

- **Create** a complete userspace TCP/IP stack that can communicate with real network hosts through a TUN device
- **Implement** all network layers (link, network, transport) with clean interfaces between them
- **Design** a TCP congestion control algorithm implementing slow start, congestion avoidance, and fast retransmit/fast recovery
- **Evaluate** RTT estimation strategies and implement Karn's algorithm with exponential backoff for retransmission timeout calculation

## The Challenge

Build a userspace TCP/IP networking stack in Rust that communicates with the real world through a TUN device. Your stack must handle ARP resolution, IP packet routing, ICMP ping, and a full TCP implementation with congestion control. Applications use your stack through a socket-like API, and the stack must be capable of fetching a web page from a real HTTP server.

This is one of the most demanding systems programming exercises possible. You will implement every layer from raw IP packets up to a reliable byte stream, making hundreds of decisions that production network stacks have refined over decades. The reward is deep understanding of how the internet actually works at the byte level.

## Requirements

1. **TUN Device Interface**: Read and write raw IP packets through a TUN device on Linux
2. **ARP**: Request/reply protocol, ARP cache with expiration, handle ARP requests directed at your IP
3. **IPv4**: Parse and construct IP packets, header checksum, TTL decrement, basic routing (default gateway)
4. **ICMP**: Echo request/reply (respond to pings), destination unreachable generation
5. **TCP Full State Machine**: All 11 RFC 793 states with correct transitions
6. **TCP Three-Way Handshake**: Active and passive open with ISN generation
7. **TCP Data Transfer**: Sequence numbers, acknowledgments, sliding window, MSS negotiation
8. **TCP Flow Control**: Receiver-advertised window, zero-window probing, window updates
9. **TCP Congestion Control**: Slow start, congestion avoidance (Reno), fast retransmit (3 duplicate ACKs), fast recovery
10. **TCP Retransmission**: RTT estimation (Jacobson/Karels algorithm), RTO calculation with exponential backoff, Karn's algorithm
11. **TCP Connection Teardown**: FIN exchange, TIME_WAIT with 2*MSL timer
12. **Socket API**: `bind`, `listen`, `accept`, `connect`, `send`, `recv`, `close` for applications
13. **Integration**: Fetch a real HTTP page (`GET / HTTP/1.0\r\n\r\n`) from a remote server using your stack

## Hints

Minimal hints are provided for insane-level challenges.

<details>
<summary>Hint 1: Layer architecture</summary>

Structure your stack as a pipeline: TUN device -> IP layer -> protocol dispatch (ICMP or TCP) -> socket API. Use channels or trait objects to decouple layers. The IP layer handles checksum verification, TTL, and routing. The transport layer handles per-connection state.
</details>

<details>
<summary>Hint 2: Congestion window</summary>

The congestion window (`cwnd`) starts at 1 MSS. During slow start, `cwnd` doubles each RTT (increment by 1 MSS per ACK). When `cwnd` reaches `ssthresh`, switch to congestion avoidance (increment by 1 MSS per RTT). On triple duplicate ACK, set `ssthresh = cwnd/2`, `cwnd = ssthresh + 3*MSS` (fast recovery). On timeout, set `ssthresh = cwnd/2`, `cwnd = 1 MSS` (back to slow start).
</details>

## Acceptance Criteria

- [ ] TUN device reads and writes raw IP packets
- [ ] ARP resolves MAC addresses and responds to ARP requests
- [ ] ICMP echo reply works (external host can ping your stack's IP)
- [ ] TCP three-way handshake completes with an external host
- [ ] TCP data transfer works bidirectionally with correct sequence numbers
- [ ] Sliding window flow control limits sending to receiver's window
- [ ] Congestion control transitions through slow start and congestion avoidance
- [ ] Fast retransmit triggers on 3 duplicate ACKs
- [ ] RTT estimation produces reasonable RTO values
- [ ] Retransmitted segments use exponential backoff
- [ ] Connection teardown completes cleanly with TIME_WAIT
- [ ] Socket API supports a simple HTTP client that fetches a real web page
- [ ] The stack can handle concurrent connections

## Research Resources

- [RFC 793: Transmission Control Protocol](https://datatracker.ietf.org/doc/html/rfc793) -- TCP state machine and segment processing
- [RFC 5681: TCP Congestion Control](https://datatracker.ietf.org/doc/html/rfc5681) -- slow start, congestion avoidance, fast retransmit, fast recovery
- [RFC 6298: Computing TCP's Retransmission Timer](https://datatracker.ietf.org/doc/html/rfc6298) -- RTT estimation and RTO calculation
- [RFC 826: ARP](https://datatracker.ietf.org/doc/html/rfc826) -- Address Resolution Protocol
- [RFC 792: ICMP](https://datatracker.ietf.org/doc/html/rfc792) -- Internet Control Message Protocol
- [smoltcp](https://github.com/smoltcp-rs/smoltcp) -- Rust userspace TCP/IP stack, excellent reference
- [TCP/IP Illustrated, Volume 1](https://www.amazon.com/TCP-Illustrated-Vol-Addison-Wesley-Professional/dp/0201633469) -- Stevens' definitive reference
- [TUN/TAP Linux docs](https://www.kernel.org/doc/html/latest/networking/tuntap.html)
