# 88. TCP State Machine Userspace

<!--
difficulty: advanced
category: networking-and-protocols
languages: [rust]
concepts: [tcp, state-machine, raw-sockets, tun-tap, rfc-793, three-way-handshake, sliding-window, sequence-numbers, flow-control, connection-teardown]
estimated_time: 16-22 hours
bloom_level: evaluate
prerequisites: [rust-basics, tcp-fundamentals, binary-encoding, state-machines, async-rust, network-layers]
-->

## Languages

- Rust (stable)

## Prerequisites

- TCP fundamentals: segments, ports, sequence numbers, acknowledgment numbers, flags (SYN, ACK, FIN, RST)
- Binary data manipulation: big-endian encoding, checksum computation, bitwise flag operations
- TUN/TAP virtual interfaces or raw socket programming on Linux/macOS
- State machine design in Rust (enums with data, pattern matching)
- Async Rust with `tokio` for non-blocking I/O
- IP header structure: version, IHL, total length, identification, TTL, protocol, checksum, source/destination addresses

## Learning Objectives

- **Implement** the TCP three-way handshake (SYN, SYN-ACK, ACK) by constructing and parsing TCP segments with correct sequence and acknowledgment numbers
- **Analyze** the TCP state diagram from RFC 793 and correctly encode all 11 states with valid transitions between them
- **Design** a sliding window flow control mechanism that tracks sent-but-unacknowledged data and respects the receiver's advertised window
- **Evaluate** sequence number arithmetic using modular 32-bit comparison to handle wraparound correctly
- **Implement** the connection teardown sequence (FIN-ACK exchange) with proper half-close semantics
- **Build** a userspace TCP stack that can establish connections, transfer data bidirectionally, and tear down cleanly over a TUN device or raw sockets

## The Challenge

TCP is the backbone of the internet. Every HTTP request, SSH session, database connection, and email transfer relies on TCP's reliable, ordered byte stream. Yet most programmers interact with TCP only through the operating system's socket API, never seeing the state machine, the sequence number dance, or the sliding window that makes it all work.

Your task is to implement the TCP state machine in userspace using Rust. Instead of relying on the kernel's TCP stack, you will read and write raw IP packets through a TUN device (or raw sockets) and implement the TCP protocol yourself. You will build the three-way handshake, data transfer with sequence numbers and acknowledgments, sliding window flow control, and the four-way connection teardown.

The TCP state machine has 11 states and dozens of transitions. RFC 793 specifies what happens when a segment with certain flags arrives in each state. Some transitions are obvious (receiving SYN in LISTEN moves to SYN_RECEIVED), but others are subtle (receiving a SYN in SYN_SENT triggers the simultaneous open path to SYN_RECEIVED). Your implementation must handle these edge cases correctly because real-world TCP stacks will exercise them.

Sequence number arithmetic is one of TCP's most error-prone aspects. Sequence numbers are 32-bit unsigned integers that wrap around. The expression `a < b` does not work -- you need modular comparison: `a` is "before" `b` if `(b - a)` interpreted as a signed 32-bit integer is positive. Getting this wrong causes data corruption that manifests only after the sequence numbers wrap past 2^32, making it extremely difficult to debug in production.

The sliding window adds flow control: the receiver advertises how much buffer space it has, and the sender must not send more than that. Combined with sequence numbers, this creates a window of bytes that are "in flight" -- sent but not yet acknowledged. Your implementation must track this window, advance it as ACKs arrive, and respect the receiver's advertisement.

## Requirements

1. Implement IP packet parsing and construction (IPv4): version, IHL, total length, protocol (TCP = 6), checksum, source and destination addresses
2. Implement TCP segment parsing and construction: source port, destination port, sequence number, acknowledgment number, data offset, flags (SYN, ACK, FIN, RST, PSH), window size, checksum (including pseudo-header), urgent pointer
3. Implement the TCP Internet checksum (RFC 1071): one's complement sum of the pseudo-header, TCP header, and payload, with proper padding for odd-length data
4. Implement all 11 TCP states: CLOSED, LISTEN, SYN_SENT, SYN_RECEIVED, ESTABLISHED, FIN_WAIT_1, FIN_WAIT_2, CLOSE_WAIT, CLOSING, LAST_ACK, TIME_WAIT
5. Implement the three-way handshake for active open (client: CLOSED -> SYN_SENT -> ESTABLISHED) and passive open (server: CLOSED -> LISTEN -> SYN_RECEIVED -> ESTABLISHED)
6. Implement data transfer: send data with sequence numbers, receive data and generate ACKs, reassemble out-of-order segments into the correct byte stream
7. Implement a sliding window for flow control: track the send window (limited by receiver's advertised window), advance the window when ACKs arrive, block sending when the window is full
8. Implement the connection teardown: active close (FIN_WAIT_1 -> FIN_WAIT_2 -> TIME_WAIT -> CLOSED) and passive close (CLOSE_WAIT -> LAST_ACK -> CLOSED)
9. Implement the TIME_WAIT state with a 2*MSL timer (use a short duration like 5 seconds for testing instead of the standard 2 minutes)
10. Implement RST handling: send RST for invalid segments, close the connection immediately upon receiving RST
11. Implement a simple retransmission mechanism: track sent segments with timestamps and retransmit if no ACK arrives within a timeout (fixed timeout is acceptable, no need for RTT estimation)
12. Use a TUN device to send and receive raw IP packets (on Linux), or raw sockets as an alternative
13. Expose a simple API for applications: `connect(addr)`, `listen(port)`, `accept()`, `send(data)`, `recv()`, `close()`

## Hints

<details>
<summary>Hint 1: TCP checksum with pseudo-header</summary>

The TCP checksum covers a pseudo-header (source IP, destination IP, zero byte, protocol number, TCP length) prepended to the TCP header and payload. Compute the one's complement sum of all 16-bit words, fold any carry bits back in, then take the one's complement. For odd-length data, pad with a zero byte for the checksum computation but do not transmit the padding.

```rust
fn checksum(data: &[u8]) -> u16 {
    let mut sum = 0u32;
    let mut i = 0;
    while i + 1 < data.len() {
        sum += u32::from(u16::from_be_bytes([data[i], data[i + 1]]));
        i += 2;
    }
    if i < data.len() {
        sum += u32::from(data[i]) << 8;
    }
    while sum >> 16 != 0 {
        sum = (sum & 0xFFFF) + (sum >> 16);
    }
    !(sum as u16)
}
```
</details>

<details>
<summary>Hint 2: Sequence number comparison</summary>

TCP sequence numbers are 32-bit and wrap around. Use wrapping arithmetic for comparisons:

```rust
fn seq_lt(a: u32, b: u32) -> bool {
    (a.wrapping_sub(b) as i32) < 0
}

fn seq_lte(a: u32, b: u32) -> bool {
    a == b || seq_lt(a, b)
}
```

This works because if `a` is "before" `b` in the sequence space, `a - b` wraps to a large unsigned value whose top bit is set, which is negative when interpreted as signed.
</details>

<details>
<summary>Hint 3: TUN device setup on Linux</summary>

Create a TUN device using the `tun` crate or by opening `/dev/net/tun` directly with `ioctl`. Configure it with an IP address in a private range (e.g., `10.0.0.1/24`). Packets sent to addresses in that subnet will arrive on the TUN device as raw IP packets, and packets written to the TUN device will be injected into the kernel's network stack.

```bash
# Manual setup (the program should do this via ioctl)
ip tuntap add mode tun name tun0
ip addr add 10.0.0.1/24 dev tun0
ip link set tun0 up
```
</details>

<details>
<summary>Hint 4: State machine with enum and match</summary>

Model the state machine as a method that takes the current state and incoming event, returning the new state and any outbound segments to send:

```rust
fn on_segment(&mut self, seg: &TcpSegment) -> Vec<TcpSegment> {
    let mut outbound = Vec::new();
    self.state = match self.state {
        TcpState::Listen => {
            if seg.flags.contains(SYN) {
                outbound.push(self.build_syn_ack(seg));
                TcpState::SynReceived
            } else {
                TcpState::Listen
            }
        }
        // ... other states
    };
    outbound
}
```
</details>

## Acceptance Criteria

- [ ] IP packets are correctly parsed and constructed with valid checksums
- [ ] TCP segments are correctly parsed and constructed with valid checksums (including pseudo-header)
- [ ] The three-way handshake completes successfully between a client and server running your userspace TCP
- [ ] Data sent from the client arrives at the server in the correct order with no corruption
- [ ] Bidirectional data transfer works: both sides can send and receive simultaneously
- [ ] Sliding window flow control limits the sender to the receiver's advertised window
- [ ] Connection teardown completes cleanly with the proper FIN-ACK exchange
- [ ] TIME_WAIT state holds the connection for the configured duration before transitioning to CLOSED
- [ ] RST segments immediately close the connection from any state
- [ ] Segments with invalid sequence numbers are rejected and trigger appropriate ACKs
- [ ] The retransmission mechanism resends unacknowledged segments after the timeout
- [ ] The API (`connect`, `listen`, `accept`, `send`, `recv`, `close`) works correctly from application code
- [ ] State transitions match the RFC 793 state diagram (test all 11 states)

## Research Resources

- [RFC 793: Transmission Control Protocol](https://datatracker.ietf.org/doc/html/rfc793) -- the original TCP specification, Section 3.2 (terminology), 3.3 (sequence numbers), 3.4 (state diagram), and 3.9 (event processing) are essential
- [RFC 1071: Computing the Internet Checksum](https://datatracker.ietf.org/doc/html/rfc1071) -- detailed algorithm for the one's complement checksum used by TCP and IP
- [RFC 7323: TCP Extensions for High Performance](https://datatracker.ietf.org/doc/html/rfc7323) -- window scaling and timestamps, useful context for understanding why the base window field is only 16 bits
- [TCP/IP Illustrated, Volume 1 by W. Richard Stevens](https://www.amazon.com/TCP-Illustrated-Vol-Addison-Wesley-Professional/dp/0201633469) -- Chapter 17-24 cover TCP in exceptional detail with packet traces
- [smoltcp source code](https://github.com/smoltcp-rs/smoltcp) -- a Rust userspace TCP/IP stack, excellent reference for packet parsing and state machine implementation
- [TUN/TAP documentation](https://www.kernel.org/doc/html/latest/networking/tuntap.html) -- Linux kernel documentation for TUN/TAP virtual network devices
- [RFC 9293: Transmission Control Protocol (TCP)](https://datatracker.ietf.org/doc/html/rfc9293) -- the modern consolidated TCP specification that supersedes RFC 793
