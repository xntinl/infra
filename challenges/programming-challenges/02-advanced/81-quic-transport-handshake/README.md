# 81. QUIC Transport Handshake

<!--
difficulty: advanced
category: networking-and-protocols
languages: [rust]
concepts: [quic, udp, packet-encoding, connection-id, version-negotiation, state-machine, variable-length-integers, retry-mechanism, cryptographic-handshake]
estimated_time: 14-20 hours
bloom_level: evaluate
prerequisites: [rust-basics, udp-sockets, binary-encoding, state-machines, async-rust, cryptography-basics]
-->

## Languages

- Rust (stable)

## Prerequisites

- UDP socket programming with `std::net::UdpSocket` or `tokio::net::UdpSocket`
- Binary data manipulation: big-endian encoding, bitwise operations, byte slicing
- State machine design patterns in Rust (enums with data, match arms)
- Variable-length integer encoding (QUIC uses a specific format defined in RFC 9000 Section 16)
- Basic symmetric cryptography concepts (AEAD, nonces, key derivation)
- Async Rust with `tokio` (spawn, select, timeouts)

## Learning Objectives

- **Implement** QUIC long header packet encoding and decoding per RFC 9000 Section 17.2, including version, connection ID fields, and packet number encoding
- **Analyze** the variable-length integer format used throughout QUIC and correctly encode/decode values spanning 1, 2, 4, and 8 bytes
- **Design** a connection state machine that tracks handshake progression through Initial, Handshake, and 1-RTT states
- **Evaluate** the security properties of the retry mechanism and why stateless retry tokens prevent address spoofing
- **Implement** version negotiation by responding to unsupported version packets with a Version Negotiation packet listing supported versions
- **Build** the Initial packet exchange flow including packet number spaces, acknowledgment generation, and simplified cryptographic protection using pre-shared keys

## The Challenge

QUIC is the transport protocol underneath HTTP/3, designed to solve TCP's head-of-line blocking, reduce connection establishment latency, and provide built-in encryption. Every time you load a modern website over HTTP/3, a QUIC handshake happens first -- yet most developers never see below the library layer.

Your task is to implement the QUIC Initial handshake over UDP in Rust. No `quinn`, no `quiche`, no `s2n-quic` -- just raw UDP sockets and the RFC. You will encode and decode QUIC long header packets, manage connection IDs, implement version negotiation, build the Initial packet exchange, and handle the retry mechanism.

The full QUIC handshake integrates TLS 1.3 directly into the transport layer, which makes it significantly more complex than TCP's three-way handshake. For this challenge, you will use a simplified cryptographic model with pre-shared keys instead of full TLS 1.3. This lets you focus on the transport mechanics: packet framing, connection ID routing, packet number spaces, and the state machine that drives the handshake forward.

QUIC packets are not self-delimiting like TCP segments. Multiple QUIC packets can be coalesced into a single UDP datagram, and each packet within a datagram may belong to a different encryption level. The long header format used during the handshake carries version information, variable-length connection IDs, and a packet number that is partially encrypted. Getting the byte-level encoding right requires careful attention to the RFC's wire format diagrams.

The retry mechanism adds another dimension. When a server is under load, it can respond with a Retry packet containing a token that the client must echo back in a new Initial packet. This stateless mechanism proves the client owns its source address without the server maintaining per-connection state during the flood. Understanding why this works and implementing it correctly teaches fundamental lessons about protocol security.

## Requirements

1. Implement QUIC variable-length integer encoding and decoding (RFC 9000 Section 16): 6-bit prefix determines whether the value occupies 1, 2, 4, or 8 bytes, with the remaining bits holding the value
2. Implement QUIC long header packet encoding and decoding (RFC 9000 Section 17.2): Header Form bit, Fixed Bit, Long Packet Type (2 bits), Type-Specific Bits (4 bits), Version (32 bits), Destination Connection ID Length + Destination Connection ID, Source Connection ID Length + Source Connection ID
3. Implement Initial packet type (Type 0x00): includes Token Length + Token field and Packet Number + Payload with AEAD protection
4. Implement packet number encoding and decoding with variable-length packet numbers (1-4 bytes) and partial encryption of the packet number field using header protection
5. Implement connection ID generation (random bytes, configurable length 0-20 bytes) and a connection ID routing table mapping connection IDs to connection state
6. Implement version negotiation: when receiving an Initial packet with an unsupported version, respond with a Version Negotiation packet (RFC 9000 Section 17.2.1) listing supported versions
7. Implement the Initial packet exchange: server receives client Initial, generates server connection ID, responds with server Initial containing handshake parameters (simplified as pre-shared key acknowledgment)
8. Implement the retry mechanism: server generates Retry packets (RFC 9000 Section 17.2.5) with a retry token, validates retry tokens on subsequent client Initials, and includes the Retry Integrity Tag
9. Implement a connection state machine with states: Idle, WaitingForInitial, HandshakeInProgress, HandshakeComplete, Draining, Closed
10. Implement acknowledgment frame generation: track received packet numbers and produce ACK frames (RFC 9000 Section 19.3) with ranges
11. Implement simplified AEAD protection using a pre-shared key: derive Initial keys from the Destination Connection ID (as QUIC does with HKDF), encrypt/decrypt packet payloads, apply header protection to the packet number
12. Handle coalesced packets: a single UDP datagram may contain multiple QUIC packets (Initial + Handshake), and your implementation must parse and process each one
13. Implement idle timeout: close connections that receive no packets within a configurable timeout period

## Hints

<details>
<summary>Hint 1: Variable-length integer encoding</summary>

The two most significant bits of the first byte encode the length:
- `00` = 1 byte (6-bit value, max 63)
- `01` = 2 bytes (14-bit value, max 16383)
- `10` = 4 bytes (30-bit value, max 1073741823)
- `11` = 8 bytes (62-bit value, max 4611686018427387903)

To decode: read the first byte, mask off the top 2 bits to get the prefix, read the remaining bytes, and combine. To encode: choose the smallest representation that fits the value.
</details>

<details>
<summary>Hint 2: Long header packet structure</summary>

```
Long Header Packet {
  Header Form (1) = 1,
  Fixed Bit (1) = 1,
  Long Packet Type (2),
  Type-Specific Bits (4),
  Version (32),
  Destination Connection ID Length (8),
  Destination Connection ID (0..160),
  Source Connection ID Length (8),
  Source Connection ID (0..160),
  Type-Specific Payload (..),
}
```

For Initial packets, the type-specific payload starts with a variable-length Token Length, followed by Token bytes, then a variable-length Remainder Length, then the packet number and encrypted payload.
</details>

<details>
<summary>Hint 3: Connection state machine transitions</summary>

Model states as a Rust enum with associated data:

```rust
enum ConnectionState {
    Idle,
    WaitingForInitial { retry_token: Option<Vec<u8>> },
    HandshakeInProgress { client_conn_id: ConnectionId, server_conn_id: ConnectionId },
    HandshakeComplete { /* negotiated params */ },
    Draining { drain_start: Instant },
    Closed,
}
```

Use `match` to enforce valid transitions. The compiler will warn you about unhandled states.
</details>

<details>
<summary>Hint 4: Initial key derivation</summary>

QUIC derives Initial encryption keys from the client's Destination Connection ID using HKDF. For this challenge, you can simplify: use the connection ID as a seed to an HKDF-like derivation (e.g., using `ring` or `hkdf` crate) to produce a client key, server key, client IV, server IV, and header protection keys. This mirrors the real protocol's structure without requiring full TLS 1.3.
</details>

## Acceptance Criteria

- [ ] Variable-length integers encode and decode correctly for all four size classes (1, 2, 4, 8 bytes) with round-trip property tests
- [ ] Long header packets encode and decode correctly, preserving all fields through a serialize/deserialize round trip
- [ ] Initial packets include proper token and length fields and survive round-trip encoding
- [ ] A client can send an Initial packet to the server and receive a server Initial in response
- [ ] Version negotiation responds to unknown versions with a Version Negotiation packet listing supported versions
- [ ] Retry mechanism works: server issues Retry, client resends Initial with retry token, server validates and proceeds
- [ ] Connection IDs are properly generated, stored, and used for routing incoming packets to the correct connection
- [ ] ACK frames correctly represent received packet number ranges including gaps
- [ ] Packet payloads are encrypted with AEAD and packet numbers have header protection applied
- [ ] Coalesced packets in a single UDP datagram are parsed and processed individually
- [ ] Idle timeout closes connections that receive no traffic within the configured period
- [ ] State machine rejects invalid transitions (e.g., receiving Handshake packet in Idle state)

## Research Resources

- [RFC 9000: QUIC: A UDP-Based Multiplexed and Secure Transport](https://datatracker.ietf.org/doc/html/rfc9000) -- the core QUIC transport specification, Sections 7 (handshake), 16 (variable-length integers), and 17 (packet formats) are essential
- [RFC 9001: Using TLS to Secure QUIC](https://datatracker.ietf.org/doc/html/rfc9001) -- describes how TLS 1.3 integrates with QUIC; Section 5 covers packet protection and header protection
- [RFC 9002: QUIC Loss Detection and Congestion Control](https://datatracker.ietf.org/doc/html/rfc9002) -- context for packet number spaces and acknowledgment processing
- [QUIC Invariants (RFC 8999)](https://datatracker.ietf.org/doc/html/rfc8999) -- the minimal properties that all QUIC versions must preserve, useful for understanding version negotiation
- [quiche source code](https://github.com/cloudflare/quiche) -- Cloudflare's Rust QUIC implementation, excellent reference for packet encoding and state machine design
- [Quinn source code](https://github.com/quinn-rs/quinn) -- pure-Rust async QUIC implementation, useful for studying connection ID management and coalesced packet handling
- [QUIC packet format visualization](https://quic.xargs.org/) -- interactive walkthrough of a real QUIC connection with byte-level annotations
