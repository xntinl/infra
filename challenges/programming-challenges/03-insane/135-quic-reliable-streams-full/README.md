# 135. QUIC Reliable Streams Full

<!--
difficulty: insane
category: networking-and-protocols
languages: [rust]
concepts: [quic, udp, reliable-streams, stream-multiplexing, flow-control, loss-detection, congestion-control, connection-migration, zero-rtt, stream-prioritization, connection-state]
estimated_time: 80-120 hours
bloom_level: create
prerequisites: [rust-advanced, quic-basics, udp-sockets, async-rust, cryptography, congestion-control, state-machines, concurrent-data-structures]
-->

## Languages

- Rust (stable)

## Prerequisites

- Advanced Rust: async/await with `tokio`, `Arc<Mutex>`, channels, unsafe for performance-critical paths
- QUIC fundamentals: long/short header packets, connection IDs, variable-length integers
- Loss detection and congestion control algorithms (RACK, Cubic/Reno)
- Stream multiplexing concepts: independent streams over a single connection, head-of-line blocking avoidance
- Flow control: per-stream and connection-level credit-based systems
- Cryptography: AEAD encryption, key derivation, 0-RTT resumption tokens

## Learning Objectives

- **Create** a complete QUIC transport implementation supporting multiple concurrent reliable streams over a single UDP connection
- **Implement** per-stream and connection-level flow control with credit-based window management
- **Design** a loss detection and recovery system using RACK-like algorithms with packet-level and time-based detection
- **Evaluate** the trade-offs in stream prioritization schemes and implement a configurable priority system

## The Challenge

Build a complete QUIC transport with reliable streams in Rust. Your implementation must multiplex multiple independent streams over a single UDP connection, with each stream providing reliable, ordered byte delivery. Implement per-stream flow control, connection-level flow control, loss detection with RACK-like recovery, stream prioritization, 0-RTT connection resumption, and connection migration.

QUIC eliminates TCP's head-of-line blocking by allowing multiple streams to share a connection without one blocked stream stalling the others. This is the fundamental innovation that makes HTTP/3 faster than HTTP/2. Building this from scratch means implementing the stream state machines, the credit-based flow control system, the loss recovery algorithm, and the packet scheduling that decides which stream's data to send next.

This challenge represents the full complexity of a modern transport protocol. You will make the same decisions that the engineers at Google, Cloudflare, and Apple made when building their QUIC implementations.

## Requirements

1. **Stream Multiplexing**: Multiple concurrent bidirectional and unidirectional streams over a single UDP connection, each with independent reliable delivery
2. **Stream State Machine**: IDLE, OPEN, HALF_CLOSED_LOCAL, HALF_CLOSED_REMOTE, CLOSED states per stream; handle STREAM, STREAM_DATA_BLOCKED, RESET_STREAM, STOP_SENDING frames
3. **Per-Stream Flow Control**: Each stream has a receive window; sender tracks credits and stops when exhausted; MAX_STREAM_DATA frames extend the window
4. **Connection-Level Flow Control**: Aggregate limit across all streams; MAX_DATA frames extend the connection window; BLOCKED/DATA_BLOCKED signals
5. **Loss Detection**: Time-based and packet-number-based detection; RACK-like algorithm tracking sent packets with timestamps; PTO (probe timeout) for tail loss probes
6. **Congestion Control**: Reno-style with slow start, congestion avoidance, and recovery; bytes-in-flight tracking; pacing support
7. **Packet Construction**: Coalesce frames from multiple streams into packets; respect MTU limits; short header format for post-handshake
8. **ACK Processing**: ACK frames with ranges; track largest acknowledged; compute RTT from ACK timestamps; ECN support awareness
9. **Stream Prioritization**: Configurable priority per stream; weighted fair scheduling across streams; urgency levels
10. **0-RTT Connection Resumption**: Save and restore server configuration; send early data before handshake completes; handle 0-RTT rejection
11. **Connection Migration**: Accept packets from new source addresses; path validation with PATH_CHALLENGE/PATH_RESPONSE; update peer address
12. **Connection Shutdown**: Graceful close with CONNECTION_CLOSE frame; immediate close; idle timeout; stateless reset

## Hints

<details>
<summary>Hint 1: Stream ID encoding</summary>

Stream IDs encode the initiator and directionality in the two least significant bits: `0b00` = client-initiated bidirectional, `0b01` = server-initiated bidirectional, `0b10` = client-initiated unidirectional, `0b11` = server-initiated unidirectional. This means stream IDs are 0, 4, 8, 12... for client-initiated bidirectional streams.
</details>

<details>
<summary>Hint 2: Flow control credits</summary>

Flow control uses absolute byte offsets, not relative windows. A MAX_STREAM_DATA frame says "you may send up to byte offset N on this stream." The sender tracks how many bytes it has sent and stops when it reaches the limit. This is simpler than TCP's relative window and avoids wraparound issues.
</details>

## Acceptance Criteria

- [ ] Multiple streams transfer data independently over a single UDP connection
- [ ] Losing a packet on one stream does not block data delivery on other streams
- [ ] Per-stream flow control limits the sender and extends via MAX_STREAM_DATA
- [ ] Connection-level flow control limits aggregate data across all streams
- [ ] Lost packets are detected and retransmitted within bounded time
- [ ] Congestion control transitions through slow start and congestion avoidance
- [ ] Packets coalesce frames from multiple streams up to the MTU
- [ ] Stream prioritization respects configured weights/urgency
- [ ] 0-RTT sends data before handshake completes (when resuming)
- [ ] Connection migration succeeds after source address change with path validation
- [ ] Graceful connection close exchanges CONNECTION_CLOSE frames
- [ ] All stream state transitions match RFC 9000 Section 3

## Research Resources

- [RFC 9000: QUIC Transport](https://datatracker.ietf.org/doc/html/rfc9000) -- complete QUIC specification; Sections 2-3 (streams), 4 (flow control), 19 (frame types)
- [RFC 9002: QUIC Loss Detection and Congestion Control](https://datatracker.ietf.org/doc/html/rfc9002) -- loss detection algorithms, PTO, congestion control
- [RFC 9001: Using TLS to Secure QUIC](https://datatracker.ietf.org/doc/html/rfc9001) -- 0-RTT, key updates, encryption levels
- [QUIC at Cloudflare (quiche)](https://github.com/cloudflare/quiche) -- production Rust QUIC implementation
- [Quinn](https://github.com/quinn-rs/quinn) -- pure-Rust async QUIC
- [s2n-quic](https://github.com/aws/s2n-quic) -- AWS Rust QUIC implementation with extensive testing
- [HTTP/3 Explained](https://http3-explained.haxx.se/) -- accessible introduction to QUIC's design goals and architecture
