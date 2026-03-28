# 5. Custom DNS Resolver with Caching

<!--
difficulty: intermediate-advanced
category: caching-and-networking
languages: [go]
concepts: [dns-protocol, udp-sockets, binary-encoding, caching, concurrency, rfc-1035]
estimated_time: 5-6 hours
bloom_level: analyze
prerequisites: [go-basics, udp-networking, binary-encoding, concurrency, context-package]
-->

## Languages

- Go (1.22+)

## Prerequisites

- UDP socket programming with `net.UDPConn`
- Binary data encoding and decoding (`encoding/binary`, big-endian byte order)
- Goroutines and channels for concurrent query handling
- The `context` package for timeouts and cancellation
- Basic understanding of DNS (what A records, CNAME records, and MX records are)

## Learning Objectives

- **Implement** DNS message encoding and decoding following RFC 1035 wire format
- **Design** a recursive resolver that follows delegation chains from root to authoritative nameserver
- **Apply** TTL-based caching to avoid redundant network queries for recently resolved domains
- **Analyze** how DNS name compression works and implement pointer-following in response parsing
- **Evaluate** resolver reliability through timeout, retry, and fallback strategies

## The Challenge

DNS is the Internet's phone book, but most developers treat it as a black box. You call `net.LookupHost()` and an IP comes back. What actually happens is a multi-step protocol conversation: your resolver sends a UDP packet with a carefully formatted binary query to a nameserver, receives a binary response, parses it, and potentially follows a chain of delegations until it reaches the authoritative answer.

Your task is to build a DNS resolver from scratch in Go. You will encode DNS queries as binary packets following RFC 1035, send them over UDP, parse the binary responses, and implement recursive resolution by following NS (nameserver) referrals. The resolver must cache responses respecting each record's TTL, handle concurrent queries without blocking, and implement timeout and retry logic for unreliable networks.

This challenge strips away every abstraction and puts you face-to-face with a real network protocol at the byte level. You will understand why DNS responses sometimes contain unexpected CNAME chains, why TTL matters for cache coherence, and why resolvers must handle truncated UDP responses gracefully.

## Requirements

1. Encode DNS query messages in wire format: header (12 bytes), question section with QNAME encoding (length-prefixed labels), QTYPE, and QCLASS
2. Decode DNS response messages: parse the header, question, answer, authority, and additional sections. Handle name compression (pointers in the name field)
3. Support record types: A (IPv4), AAAA (IPv6), CNAME (canonical name), and MX (mail exchange)
4. Implement iterative resolution: start from a root nameserver, follow NS delegations through TLD and authoritative servers until you get a final answer
5. Follow CNAME chains: if the answer contains a CNAME instead of the requested type, resolve the canonical name
6. Cache responses with per-record TTL. On cache hit, decrement the TTL based on elapsed time. Evict entries when TTL reaches zero
7. Handle concurrent queries: multiple goroutines can resolve different domains simultaneously without interfering
8. Implement timeout (2 seconds per query) and retry logic (up to 3 attempts) with fallback to a secondary nameserver
9. Return structured results: resolved IPs, the full CNAME chain if any, TTL of each record, and which nameserver provided the answer
10. Provide a CLI interface: `resolve <domain> [record-type]` that prints results in a dig-like format

## Hints

<details>
<summary>Hint 1: DNS header format</summary>

The DNS header is exactly 12 bytes. Use `encoding/binary` with big-endian byte order:

```go
type Header struct {
    ID      uint16
    Flags   uint16
    QDCount uint16 // questions
    ANCount uint16 // answers
    NSCount uint16 // authority records
    ARCount uint16 // additional records
}

func (h *Header) Encode() []byte {
    buf := make([]byte, 12)
    binary.BigEndian.PutUint16(buf[0:2], h.ID)
    binary.BigEndian.PutUint16(buf[2:4], h.Flags)
    // ... remaining fields
    return buf
}
```

Set the RD (Recursion Desired) flag bit at position 8 of the Flags field for queries to recursive resolvers.
</details>

<details>
<summary>Hint 2: QNAME encoding</summary>

Domain names are encoded as a sequence of length-prefixed labels followed by a zero byte:

```go
// "example.com" -> [7]example[3]com[0]
func encodeName(domain string) []byte {
    var buf []byte
    for _, label := range strings.Split(domain, ".") {
        buf = append(buf, byte(len(label)))
        buf = append(buf, []byte(label)...)
    }
    buf = append(buf, 0x00)
    return buf
}
```
</details>

<details>
<summary>Hint 3: Name compression (pointers)</summary>

DNS responses use compression: instead of repeating a name, they reference a previous occurrence with a pointer. A pointer is a two-byte value where the top two bits are `11` and the remaining 14 bits are the offset from the start of the message:

```go
func decodeName(msg []byte, offset int) (string, int) {
    var labels []string
    for {
        length := int(msg[offset])
        if length == 0 {
            offset++
            break
        }
        if length&0xC0 == 0xC0 { // pointer
            ptr := int(binary.BigEndian.Uint16(msg[offset:offset+2])) & 0x3FFF
            suffix, _ := decodeName(msg, ptr)
            labels = append(labels, suffix)
            offset += 2
            break
        }
        offset++
        labels = append(labels, string(msg[offset:offset+length]))
        offset += length
    }
    return strings.Join(labels, "."), offset
}
```
</details>

<details>
<summary>Hint 4: Cache with TTL decay</summary>

Store the time each entry was cached. On lookup, compute remaining TTL:

```go
type CacheEntry struct {
    Records   []DNSRecord
    CachedAt  time.Time
    OrigTTL   time.Duration
}

func (e *CacheEntry) RemainingTTL() time.Duration {
    elapsed := time.Since(e.CachedAt)
    remaining := e.OrigTTL - elapsed
    if remaining < 0 {
        return 0
    }
    return remaining
}
```
</details>

## Acceptance Criteria

- [ ] Correctly encodes DNS query packets for A, AAAA, CNAME, and MX record types
- [ ] Decodes DNS response packets including compressed names (pointer following)
- [ ] Resolves `example.com` type A iteratively from root nameservers to authoritative answer
- [ ] Follows CNAME chains (e.g., `www.github.com` -> CNAME -> A record)
- [ ] Cache returns previously resolved entries with decremented TTL
- [ ] Cache evicts entries whose TTL has expired
- [ ] Concurrent resolution of 50+ domains completes without races or deadlocks
- [ ] Queries time out after 2 seconds and retry up to 3 times
- [ ] CLI outputs domain, record type, value, TTL, and responding nameserver

## Research Resources

- [RFC 1035: Domain Names - Implementation and Specification](https://www.rfc-editor.org/rfc/rfc1035) -- the authoritative specification for DNS wire format
- [Julia Evans: Implement DNS in a Weekend](https://implement-dns.wizardzines.com/) -- excellent walkthrough of building a DNS resolver from scratch
- [Go net package: UDPConn](https://pkg.go.dev/net#UDPConn) -- UDP socket API for sending and receiving DNS packets
- [Cloudflare: DNS Concepts](https://www.cloudflare.com/learning/dns/what-is-dns/) -- accessible explanation of DNS resolution flow
- [Root Nameservers](https://www.iana.org/domains/root/servers) -- the 13 root server addresses you need for iterative resolution
- [Wireshark DNS Analysis](https://wiki.wireshark.org/DNS) -- use Wireshark to inspect real DNS packets and validate your encoding
