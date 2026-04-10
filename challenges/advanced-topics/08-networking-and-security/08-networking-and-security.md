# Networking and Security — Reference Overview

## Why This Section Matters

Networking and security are the two domains where ignorance has the highest blast radius. A
developer who misunderstands virtual memory loses some performance. A developer who
misunderstands TLS handshakes or nonce reuse ships a product that silently loses all user
data to a passive observer. The asymmetry between "it seems to work" and "it is secure" is
nowhere more dangerous than in cryptographic protocol implementation.

This section is for engineers who need to go beyond calling `tls.Dial()` or
`reqwest::Client::new()`. It is for engineers who are asked: "Is our key exchange forward
secret? Can an attacker replay our 0-RTT data? Why is our DNS resolver vulnerable to
cache poisoning? What does constant-time actually mean and which of our endpoints need it?"

The perspective throughout is threat-model first. Every cryptographic choice is a claim
about which attacks it defeats and which it does not. Every protocol is a security
boundary, and every boundary has seams. The content covers TLS 1.3, modern cryptographic
primitives, zero-knowledge proofs, QUIC, DNS internals, public key infrastructure, and
side-channel attacks — with complete runnable implementations in Go and Rust.

Go and Rust are the two languages that dominate new security-critical infrastructure. Go
ships an unusually complete `crypto/*` standard library; the stdlib is the right first
choice for most workloads, and understanding its internals prevents cargo-culting dangerous
configuration. Rust's ownership model eliminates entire classes of memory-safety
vulnerabilities; its `ring`, `rustls`, and `RustCrypto` ecosystem provides audited
primitives with strong constant-time guarantees.

---

## Subtopics

| # | Topic | Key Concepts | Reading Time | Difficulty |
|---|-------|-------------|-------------|-----------|
| 01 | [TLS Internals](./01-tls-internals/01-tls-internals.md) | TLS 1.3 handshake, HKDF, AEAD, session tickets, 0-RTT, mTLS | 75 min | Advanced |
| 02 | [Cryptographic Primitives](./02-cryptographic-primitives/02-cryptographic-primitives.md) | AES-GCM, ChaCha20-Poly1305, BLAKE3, HKDF, nonce discipline | 75 min | Advanced |
| 03 | [Zero-Knowledge Proofs](./03-zero-knowledge-proofs/03-zero-knowledge-proofs.md) | Schnorr, Fiat-Shamir, Pedersen commitments, Bulletproofs | 90 min | Expert |
| 04 | [Protocol Design](./04-protocol-design/04-protocol-design.md) | Length-prefix framing, multiplexing, backpressure, versioning | 60 min | Advanced |
| 05 | [QUIC Protocol](./05-quic-protocol/05-quic-protocol.md) | Stream multiplexing, connection migration, 0-RTT, congestion | 75 min | Advanced |
| 06 | [DNS and Name Resolution](./06-dns-and-name-resolution/06-dns-and-name-resolution.md) | Wire format, DNSSEC, DoH/DoT, cache poisoning, amplification | 75 min | Advanced |
| 07 | [Key Exchange and PKI](./07-key-exchange-and-pki/07-key-exchange-and-pki.md) | X25519, Ed25519, X.509 chain, OCSP stapling, CT logs | 75 min | Advanced |
| 08 | [Side-Channel Attacks](./08-side-channel-attacks/08-side-channel-attacks.md) | Timing attacks, cache-timing, Spectre/Meltdown, constant-time | 75 min | Expert |

---

## Dependency Map

Security topics are deeply interconnected. The dependency graph below reflects conceptual
prerequisites — you can read topics out of order, but the earlier topics provide mental
models that make the later ones land faster.

```
Cryptographic Primitives
        |
        +---> TLS Internals ---------> Key Exchange and PKI
        |           |
        |           +---> QUIC Protocol
        |
        +---> Zero-Knowledge Proofs
        |
        +---> Side-Channel Attacks

Protocol Design --------> QUIC Protocol
                  \
                   +-----> DNS and Name Resolution

Key Exchange and PKI ---> TLS Internals
                    \
                     +---> Zero-Knowledge Proofs
```

Start with **Cryptographic Primitives** (topic 02) if you are unfamiliar with AES-GCM,
AEAD, or HKDF — these are vocabulary used throughout every other topic.

**TLS Internals** (topic 01) and **Key Exchange and PKI** (topic 07) are mutually
reinforcing: TLS describes how keys are used in the handshake; PKI describes how trust in
those keys is established. Read them together or back-to-back.

**Protocol Design** (topic 04) is a prerequisite for **QUIC** (topic 05) — QUIC is most
legible after you understand why HTTP/1.1 framing was a mistake and what properties a
well-designed binary protocol needs.

**Side-Channel Attacks** (topic 08) cuts across all other topics: the constant-time
requirement applies to every cryptographic primitive, and understanding *why* is what
separates engineers who write correct crypto from those who merely write crypto that
passes tests.

---

## Time Investment

| Goal | Topics | Total Time |
|------|--------|-----------|
| TLS configuration literacy | 01, 07 | 2.5 h reading + 4 h exercises |
| Secure service implementation | 01, 02, 07 | 3.75 h reading + 8 h exercises |
| Protocol engineering | 04, 05, 06 | 3.5 h reading + 8 h exercises |
| Cryptographic system design | 02, 03, 08 | 4 h reading + 12 h exercises |
| Full section mastery | 01–08 | ~10 h reading + 40–60 h exercises |

---

## Prerequisites

Before starting this section you should be comfortable with:

- **Go or Rust at the intermediate level** — you must be able to read and write idiomatic
  code, understand error handling patterns, and work with byte slices / `&[u8]`.
- **Basic cryptography vocabulary** — symmetric vs asymmetric, what a hash function
  guarantees, what a digital signature is. You do not need to know the math, but the
  vocabulary is assumed from the first paragraph.
- **TCP/IP fundamentals** — the three-way handshake, what a socket is, what a port is,
  what happens when a TCP segment is dropped.
- **Binary data literacy** — reading hex dumps, understanding big-endian vs little-endian,
  bit manipulation. The DNS and protocol design topics parse wire formats by hand.
- **Concurrency fundamentals** — goroutines and channels (Go), `async/await` and `Send +
  Sync` (Rust). The QUIC topic covers stream multiplexing which requires understanding
  concurrent I/O.

Optional but strongly recommended:

- The **lock-free data structures** material at
  `rust/04-insane/02-lock-free-data-structures/` provides the memory-model foundation
  that makes the side-channel attacks topic much more concrete.
- The **zero-knowledge proof system** challenge at
  `programming-challenges/03-insane/45-zero-knowledge-proof-system/` is an excellent
  companion to topic 03 — read the challenge description first, then use topic 03 as the
  conceptual background.
- Dan Boneh and Victor Shoup, "A Graduate Course in Applied Cryptography" (freely
  available at crypto.stanford.edu) — chapters 1–5 cover the foundations assumed
  throughout this section.
- RFC 8446 (TLS 1.3) — not light reading, but topic 01 will feel shallow without it.
