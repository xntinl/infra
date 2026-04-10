<!--
type: reference
difficulty: advanced
section: [08-networking-and-security]
concepts: [dns-wire-format, label-compression, dnssec, rrsig-dnskey-ds, doh, dot, recursive-resolver, cache-poisoning, dns-amplification]
languages: [go, rust]
estimated_reading_time: 60-90 min
bloom_level: analyze
prerequisites: [udp-fundamentals, protocol-design, cryptographic-primitives]
papers: [Bernstein & Lange 2012 — DNSCurve, Vixie et al. 1997 — RFC 2181 Clarifications to DNS, Arends et al. 2005 — RFC 4035 DNSSEC Protocol Modifications]
industry_use: [bind9, unbound, cloudflare-1.1.1.1, google-8.8.8.8, coredns, nextdns]
language_contrast: high
-->

# DNS and Name Resolution

> DNS is the one system that attackers compromise before everything else — poisoning a
> single resolver entry redirects all traffic, including TLS-protected traffic, if
> certificate pinning is absent.

## Mental Model

DNS is a hierarchical, distributed key-value database with a 45-year-old wire format
that was never designed for adversarial conditions. A DNS query is two UDP datagrams:
a 12-byte header plus question section (sent by the client), and a response containing
answer records (sent by the server). UDP provides no authentication — the first UDP
packet that arrives with the correct transaction ID wins. This is the entire foundation
of DNS cache poisoning.

The critical insight is that DNS cache poisoning is a race condition: the attacker must
inject a forged response before the legitimate response arrives. The original Kaminsky
attack (2008) made this practical: by sending thousands of forged responses with random
transaction IDs, an attacker can poison a resolver's cache for a domain in seconds.
DNSSEC is the cryptographic fix: every resource record set is signed, and the chain of
trust anchors to the root zone's public key (the IANA trust anchor). A DNSSEC-validating
resolver will reject unsigned or incorrectly signed responses.

The second insight is the difference between recursive and iterative resolution:
- **Iterative**: the client does all the work. It queries the root, follows referrals to
  the TLD, then to the authoritative server. Result: many round trips, no caching.
- **Recursive**: the resolver does all the work. The client queries its resolver (8.8.8.8,
  1.1.1.1, etc.), the resolver walks the hierarchy, caches results, and returns the final
  answer. Almost all production clients use recursive resolvers.

The cache is the attack surface: a recursive resolver that serves millions of clients is
a high-value target. Poisoning a resolver once affects all its clients.

DoH (DNS over HTTPS) and DoT (DNS over TLS) encrypt DNS queries in transit, preventing
passive observation and ISP DNS hijacking. They do not replace DNSSEC — they protect
the *transport*, not the *content*. A resolver that lies in response to queries can still
do so over DoH.

## Core Concepts

### DNS Wire Format

A DNS message:
```
+--[12-byte header]--+--[question section]--+--[answer section]--+
| ID (2B)            | QNAME (labels)       | NAME               |
| FLAGS (2B)         | QTYPE (2B)           | TYPE (2B)          |
| QDCOUNT (2B)       | QCLASS (2B)          | CLASS (2B)         |
| ANCOUNT (2B)       |                      | TTL (4B)           |
| NSCOUNT (2B)       |                      | RDLENGTH (2B)      |
| ARCOUNT (2B)       |                      | RDATA (variable)   |
+--------------------+----------------------+--------------------+
```

A domain name in DNS wire format is encoded as a series of labels. Each label is a
length byte followed by the label bytes. The root is encoded as a single zero byte.
`example.com` is encoded as: `\x07example\x03com\x00`.

**Label compression**: to avoid repeating long names in the answer section, DNS supports
pointer compression. A pointer is two bytes with the high two bits set to 11, followed
by a 14-bit offset from the start of the message. Parsers must implement compression
correctly and must detect and reject infinite pointer loops (the "compression loop" attack).

### DNSSEC Records

DNSSEC adds four new record types:
- **DNSKEY**: public key for signing zone data. The zone operator publishes this.
- **RRSIG**: signature over a resource record set, made with the zone's private key.
- **DS**: Delegation Signer — a hash of the child zone's DNSKEY, published by the parent.
  Creates the chain of trust from parent to child.
- **NSEC / NSEC3**: proves the non-existence of a name (authenticated denial of existence).

The chain of trust:
```
Root zone (.)
  → DS record for .com → DNSKEY for .com zone
        → DS record for example.com → DNSKEY for example.com
              → RRSIG over example.com's A record
```

A validating resolver starts with the root's DNSKEY (the trust anchor, hardcoded),
verifies the DS record for .com, then .com's DS record for example.com, then example.com's
RRSIG. If any link is broken (invalid signature, expired signature, missing DS), the
resolver returns SERVFAIL.

### DNS Attack Surface

**Cache poisoning**: inject a forged record into a resolver's cache. Defenses: random
source ports (RFC 5452), random transaction IDs, DNSSEC validation.

**Amplification DDoS**: DNS servers respond to queries with responses 10–100x larger
(queries are small; responses may include DNSSEC records, multiple answers, additional
section). Attackers spoof the victim's IP as the query source. Defense: Response Rate
Limiting (RRL), BCP38 filtering (ISPs should not forward packets with spoofed source IPs).

**DNS hijacking**: attacker controls the resolver or the authoritative server. Defense:
DNSSEC plus certificate pinning (a hijacked resolver cannot forge DNSSEC signatures
without the zone's private key).

**Subdomain takeover**: an organization points a CNAME to a cloud provider, then stops
using that provider. The cloud provider re-allocates the hostname; an attacker claims it.
The CNAME now points to attacker-controlled infrastructure. Defense: monitor for
dangling CNAMEs.

## Implementation: Go

```go
package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
)

// DNSHeader is the 12-byte fixed DNS message header.
type DNSHeader struct {
	ID      uint16
	Flags   uint16
	QDCount uint16 // number of questions
	ANCount uint16 // number of answers
	NSCount uint16 // number of authority records
	ARCount uint16 // number of additional records
}

// DNSFlags decodes the DNS FLAGS field.
// Bit layout: QR(1) OPCODE(4) AA(1) TC(1) RD(1) RA(1) Z(1) AD(1) CD(1) RCODE(4)
type DNSFlags struct {
	QR     bool   // 0=query, 1=response
	Opcode uint8  // 0=standard query
	AA     bool   // authoritative answer
	TC     bool   // truncated
	RD     bool   // recursion desired
	RA     bool   // recursion available
	AD     bool   // authentic data (DNSSEC)
	CD     bool   // checking disabled (DNSSEC)
	RCode  uint8  // response code (0=ok, 2=servfail, 3=nxdomain)
}

func parseFlags(flags uint16) DNSFlags {
	return DNSFlags{
		QR:     (flags>>15)&1 == 1,
		Opcode: uint8((flags >> 11) & 0xF),
		AA:     (flags>>10)&1 == 1,
		TC:     (flags>>9)&1 == 1,
		RD:     (flags>>8)&1 == 1,
		RA:     (flags>>7)&1 == 1,
		AD:     (flags>>5)&1 == 1,
		CD:     (flags>>4)&1 == 1,
		RCode:  uint8(flags & 0xF),
	}
}

// buildQuery constructs a DNS query packet for an A record lookup.
// This is what your OS resolver does before sending UDP to 8.8.8.8.
func buildQuery(name string, qtype uint16, recursionDesired bool) ([]byte, uint16, error) {
	// Construct a random transaction ID. In a real resolver, this must be truly random
	// to prevent prediction-based cache poisoning (Kaminsky attack).
	txID := uint16(0xABCD) // fixed for demonstration; use crypto/rand in production

	rdBit := uint16(0)
	if recursionDesired {
		rdBit = 0x0100 // set RD bit in flags
	}

	header := DNSHeader{
		ID:      txID,
		Flags:   rdBit,
		QDCount: 1,
	}

	var msg []byte
	msg = binary.BigEndian.AppendUint16(msg, header.ID)
	msg = binary.BigEndian.AppendUint16(msg, header.Flags)
	msg = binary.BigEndian.AppendUint16(msg, header.QDCount)
	msg = binary.BigEndian.AppendUint16(msg, header.ANCount) // 0
	msg = binary.BigEndian.AppendUint16(msg, header.NSCount) // 0
	msg = binary.BigEndian.AppendUint16(msg, header.ARCount) // 0

	// Encode QNAME as length-prefixed labels.
	labels := strings.Split(strings.TrimSuffix(name, "."), ".")
	for _, label := range labels {
		if len(label) > 63 {
			return nil, 0, fmt.Errorf("label %q exceeds 63-byte limit", label)
		}
		msg = append(msg, byte(len(label)))
		msg = append(msg, []byte(label)...)
	}
	msg = append(msg, 0x00) // root label terminator

	// QTYPE and QCLASS
	msg = binary.BigEndian.AppendUint16(msg, qtype) // 1 = A record
	msg = binary.BigEndian.AppendUint16(msg, 1)     // 1 = IN class

	return msg, txID, nil
}

// parseName decodes a DNS wire-format name starting at offset, handling pointer compression.
// Pointer compression is the source of infinite loop attacks — track visited offsets.
func parseName(msg []byte, offset int) (string, int, error) {
	var labels []string
	visited := make(map[int]bool)
	originalOffset := offset
	followedPointer := false

	for {
		if offset >= len(msg) {
			return "", 0, errors.New("name extends beyond message")
		}
		if visited[offset] {
			return "", 0, errors.New("dns compression loop detected")
		}
		visited[offset] = true

		length := int(msg[offset])

		if length == 0 {
			// Root label
			if !followedPointer {
				originalOffset = offset + 1
			}
			break
		}

		if length&0xC0 == 0xC0 {
			// Pointer: next two bytes are the target offset.
			if offset+1 >= len(msg) {
				return "", 0, errors.New("truncated pointer")
			}
			ptr := int(binary.BigEndian.Uint16(msg[offset:offset+2]) & 0x3FFF)
			if !followedPointer {
				originalOffset = offset + 2
				followedPointer = true
			}
			offset = ptr
			continue
		}

		if length&0xC0 != 0 {
			return "", 0, fmt.Errorf("unsupported label type 0x%02X", length&0xC0)
		}

		offset++
		if offset+length > len(msg) {
			return "", 0, errors.New("label extends beyond message")
		}
		labels = append(labels, string(msg[offset:offset+length]))
		offset += length
	}

	name := strings.Join(labels, ".")
	return name, originalOffset, nil
}

// DNSRecord represents a parsed resource record.
type DNSRecord struct {
	Name  string
	Type  uint16
	Class uint16
	TTL   uint32
	RData []byte
}

// parseRecord parses one resource record from msg starting at offset.
func parseRecord(msg []byte, offset int) (*DNSRecord, int, error) {
	name, newOffset, err := parseName(msg, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("parse name: %w", err)
	}
	offset = newOffset

	if offset+10 > len(msg) {
		return nil, 0, errors.New("record header truncated")
	}

	rtype := binary.BigEndian.Uint16(msg[offset : offset+2])
	class := binary.BigEndian.Uint16(msg[offset+2 : offset+4])
	ttl := binary.BigEndian.Uint32(msg[offset+4 : offset+8])
	rdLen := int(binary.BigEndian.Uint16(msg[offset+8 : offset+10]))
	offset += 10

	if offset+rdLen > len(msg) {
		return nil, 0, errors.New("rdata truncated")
	}
	rdata := msg[offset : offset+rdLen]
	offset += rdLen

	return &DNSRecord{Name: name, Type: rtype, Class: class, TTL: ttl, RData: rdata}, offset, nil
}

// formatARecord formats an A record's RDATA as an IP address.
func formatARecord(rdata []byte) (string, error) {
	if len(rdata) != 4 {
		return "", fmt.Errorf("A record RDATA must be 4 bytes, got %d", len(rdata))
	}
	return net.IP(rdata).String(), nil
}

// resolvePlain performs a plain (unauthenticated) DNS A query over UDP.
// This is vulnerable to cache poisoning without DNSSEC validation.
func resolvePlain(name, resolver string) ([]string, error) {
	const typeA = 1
	query, txID, err := buildQuery(name, typeA, true)
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	conn, err := net.Dial("udp", resolver+":53")
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	if _, err := conn.Write(query); err != nil {
		return nil, fmt.Errorf("write query: %w", err)
	}

	resp := make([]byte, 512) // UDP DNS responses are limited to 512 bytes without EDNS0
	n, err := conn.Read(resp)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	resp = resp[:n]

	if len(resp) < 12 {
		return nil, errors.New("response too short")
	}

	respID := binary.BigEndian.Uint16(resp[0:2])
	if respID != txID {
		return nil, fmt.Errorf("transaction ID mismatch: got %d want %d", respID, txID)
	}

	flags := parseFlags(binary.BigEndian.Uint16(resp[2:4]))
	if !flags.QR {
		return nil, errors.New("response has QR=0 (this is a query, not a response)")
	}
	if flags.RCode != 0 {
		return nil, fmt.Errorf("DNS error: rcode=%d", flags.RCode)
	}

	anCount := int(binary.BigEndian.Uint16(resp[4:6]))
	qdCount := int(binary.BigEndian.Uint16(resp[6:8]))
	_ = qdCount

	// Skip the question section.
	offset := 12
	for range qdCount {
		_, newOffset, err := parseName(resp, offset)
		if err != nil {
			return nil, fmt.Errorf("skip question name: %w", err)
		}
		offset = newOffset + 4 // skip QTYPE + QCLASS
	}

	var addrs []string
	for range anCount {
		rec, newOffset, err := parseRecord(resp, offset)
		if err != nil {
			return nil, fmt.Errorf("parse answer: %w", err)
		}
		offset = newOffset
		if rec.Type == typeA {
			addr, err := formatARecord(rec.RData)
			if err == nil {
				addrs = append(addrs, addr)
			}
		}
	}

	return addrs, nil
}

func main() {
	addrs, err := resolvePlain("example.com", "8.8.8.8")
	if err != nil {
		fmt.Printf("resolve error: %v\n", err)
		return
	}
	fmt.Printf("example.com A records: %v\n", addrs)

	// Demonstrate building and printing a query
	q, _, _ := buildQuery("google.com", 28 /*AAAA*/, true)
	fmt.Printf("AAAA query for google.com: %d bytes, first 12: %x\n", len(q), q[:12])
}
```

### Go-specific considerations

**`net.LookupHost` vs raw UDP.** For production resolution, use `net.LookupHost` which
delegates to the OS resolver (or `cgo` resolver), respects `/etc/hosts`, and supports
LDAP/mDNS on some platforms. The raw UDP implementation above is for understanding the
wire format, not production use.

**`net.Resolver` with DoH.** Go 1.20 added `net.Resolver.Dial` which can route resolver
queries to any transport. Use this to implement DoH: dial HTTPS to `1.1.1.1`, serialize
the DNS message as the body, send with `Content-Type: application/dns-message`.

**`miekg/dns` for production DNS code.** The `miekg/dns` library is the standard DNS
library in Go. It handles all record types, DNSSEC, zone file parsing, and DoH/DoT. Use
it instead of the raw parsing code above for anything beyond education.

**DNSSEC validation in Go.** Go's stdlib does not validate DNSSEC. Use `miekg/dns` with
the `DNSSEC` query option and implement the chain-of-trust validation yourself, or use
a local unbound resolver that validates DNSSEC and returns SERVFAIL for invalid responses.

## Implementation: Rust

```rust
use std::convert::TryInto;
use std::net::UdpSocket;

const MAX_DNS_MSG: usize = 512;

/// Build a minimal DNS A query.
fn build_a_query(name: &str, tx_id: u16) -> Vec<u8> {
    let mut msg = Vec::with_capacity(64);

    // Header
    msg.extend_from_slice(&tx_id.to_be_bytes());     // ID
    msg.extend_from_slice(&0x0100u16.to_be_bytes()); // FLAGS: RD=1
    msg.extend_from_slice(&1u16.to_be_bytes());      // QDCOUNT=1
    msg.extend_from_slice(&0u16.to_be_bytes());      // ANCOUNT=0
    msg.extend_from_slice(&0u16.to_be_bytes());      // NSCOUNT=0
    msg.extend_from_slice(&0u16.to_be_bytes());      // ARCOUNT=0

    // QNAME
    for label in name.trim_end_matches('.').split('.') {
        assert!(label.len() <= 63, "label too long");
        msg.push(label.len() as u8);
        msg.extend_from_slice(label.as_bytes());
    }
    msg.push(0x00); // root terminator

    // QTYPE=A(1), QCLASS=IN(1)
    msg.extend_from_slice(&1u16.to_be_bytes());
    msg.extend_from_slice(&1u16.to_be_bytes());

    msg
}

/// Parse a DNS name from the message at offset, handling pointer compression.
/// Returns (name, next_offset_after_name).
fn parse_name(msg: &[u8], mut offset: usize) -> Result<(String, usize), String> {
    let mut labels: Vec<String> = Vec::new();
    let mut followed_pointer = false;
    let mut next_offset = 0;
    let mut visited = std::collections::HashSet::new();

    loop {
        if offset >= msg.len() {
            return Err("name extends beyond message".into());
        }
        if !visited.insert(offset) {
            return Err("dns compression loop detected".into());
        }

        let byte = msg[offset];

        if byte == 0 {
            if !followed_pointer {
                next_offset = offset + 1;
            }
            break;
        }

        if byte & 0xC0 == 0xC0 {
            // Pointer
            if offset + 1 >= msg.len() {
                return Err("truncated pointer".into());
            }
            let ptr = (u16::from_be_bytes([byte & 0x3F, msg[offset + 1]]) as usize);
            if !followed_pointer {
                next_offset = offset + 2;
                followed_pointer = true;
            }
            offset = ptr;
            continue;
        }

        let label_len = byte as usize;
        offset += 1;
        if offset + label_len > msg.len() {
            return Err("label extends beyond message".into());
        }
        let label = std::str::from_utf8(&msg[offset..offset + label_len])
            .map_err(|e| format!("invalid label utf8: {e}"))?;
        labels.push(label.to_owned());
        offset += label_len;
    }

    if !followed_pointer {
        next_offset = offset + 1;
    }

    Ok((labels.join("."), next_offset))
}

/// Send a DNS A query to the resolver and return the IP addresses.
fn resolve_a(name: &str, resolver: &str) -> Result<Vec<String>, Box<dyn std::error::Error>> {
    let socket = UdpSocket::bind("0.0.0.0:0")?;
    socket.set_read_timeout(Some(std::time::Duration::from_secs(5)))?;
    socket.connect(format!("{resolver}:53"))?;

    let tx_id: u16 = 0xABCD; // use rand in production
    let query = build_a_query(name, tx_id);
    socket.send(&query)?;

    let mut resp = vec![0u8; MAX_DNS_MSG];
    let n = socket.recv(&mut resp)?;
    resp.truncate(n);

    if resp.len() < 12 {
        return Err("response too short".into());
    }

    let resp_id = u16::from_be_bytes([resp[0], resp[1]]);
    if resp_id != tx_id {
        return Err(format!("transaction ID mismatch: {resp_id} != {tx_id}").into());
    }

    let flags = u16::from_be_bytes([resp[2], resp[3]]);
    let qr = (flags >> 15) & 1;
    let rcode = flags & 0xF;
    if qr != 1 { return Err("response has QR=0".into()); }
    if rcode != 0 { return Err(format!("DNS rcode={rcode}").into()); }

    let qd_count = u16::from_be_bytes([resp[4], resp[5]]) as usize;
    let an_count = u16::from_be_bytes([resp[6], resp[7]]) as usize;

    let mut offset = 12;
    // Skip question section
    for _ in 0..qd_count {
        let (_, new_offset) = parse_name(&resp, offset)?;
        offset = new_offset + 4; // QTYPE + QCLASS
    }

    let mut addrs = Vec::new();
    for _ in 0..an_count {
        let (_, new_offset) = parse_name(&resp, offset)?;
        offset = new_offset;
        if offset + 10 > resp.len() { break; }

        let rtype = u16::from_be_bytes([resp[offset], resp[offset+1]]);
        offset += 8; // skip type+class+ttl
        let rdlen = u16::from_be_bytes([resp[offset], resp[offset+1]]) as usize;
        offset += 2;

        if rtype == 1 && rdlen == 4 && offset + 4 <= resp.len() {
            // A record
            let ip = format!("{}.{}.{}.{}", resp[offset], resp[offset+1], resp[offset+2], resp[offset+3]);
            addrs.push(ip);
        }
        offset += rdlen;
    }

    Ok(addrs)
}

fn main() {
    match resolve_a("example.com", "8.8.8.8") {
        Ok(addrs) => println!("example.com: {addrs:?}"),
        Err(e) => eprintln!("resolve error: {e}"),
    }
}
```

### Rust-specific considerations

**`hickory-dns` (formerly `trust-dns`) for production.** The `hickory-dns` crate is the
standard DNS library in the Rust ecosystem. It supports all record types, DNSSEC
validation, DoH, DoT, and zone server functionality. Use it in production; the raw
implementation above is for learning the wire format.

**Bit manipulation ergonomics.** Rust's exhaustive match and `TryFrom<u8>` make parsing
flag fields more ergonomic than Go's bit shifts. A `DnsFlags` struct with `TryFrom<u16>`
can return `Err` for invalid opcode values, making malformed-packet handling explicit.

**Zero-copy parsing.** The `hickory-dns` crate uses a cursor abstraction that avoids
copying DNS messages. For high-throughput resolvers, use `bytes::Bytes` slices to
implement zero-copy record parsing.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Production DNS library | `miekg/dns` | `hickory-dns` |
| OS resolver integration | `net.LookupHost` | `hickory-resolver` with OS backend |
| DoH client | `net.Resolver.Dial` + `http.Client` | `hickory-resolver` with DoH transport |
| DNSSEC validation | `miekg/dns` + manual chain verification | `hickory-resolver` with DNSSEC feature |
| Zone serving | `miekg/dns` zone server | `hickory-server` |
| Wire parsing safety | Manual bounds checks | Manual bounds checks (similar risk) |
| Pointer loop detection | Manual visited set | Manual visited set |
| Async resolver | `goroutine + net.Resolver` | `hickory-resolver` (tokio-based) |

## Production War Stories

**Kaminsky Attack (2008).** Dan Kaminsky discovered that a DNS resolver could be poisoned
in seconds by sending thousands of forged responses with random transaction IDs. Before
randomized source ports were deployed (RFC 5452), the attack space was just 65,536
transaction IDs — trivially brute-forceable. Randomized source ports increased the attack
space to ~10^9, making cache poisoning impractical. Every major resolver patched within
weeks of the coordinated disclosure. The lesson: unauthenticated protocols are vulnerable
to off-path injection when the identifier space is small.

**Cloudflare 1.1.1.1 launch (2018).** Cloudflare launched 1.1.1.1 as a DoH/DoT resolver.
Within the first hour, they received unexpected DNS traffic for every kind of garbage
domain — because many ISPs had been transparently intercepting and responding to DNS
queries, and some of those ISPs had misconfigured devices that started sending traffic to
1.1.1.1 when ISP-level DNS was bypassed. This revealed just how pervasive ISP-level DNS
manipulation was. The lesson: DNS is not a neutral utility; it is actively modified at
many points in the network.

**BGP hijack affecting DNS (2018).** Amazon's Route 53 was briefly hijacked via BGP
prefix announcement. Attackers diverted queries for myetherwallet.com to a spoofed
server and served a fake DNS response. Users saw a certificate warning (the attacker did
not have a valid TLS certificate) but some proceeded anyway. The attack worked because
the domain was not DNSSEC-signed. If it had been, the forged DNS response would have
been rejected by validating resolvers.

## Security Analysis

**DNSSEC does not encrypt.** DNSSEC prevents cache poisoning and authoritative server
spoofing, but all DNS queries and responses remain in plaintext. An observer can see
every hostname you resolve. DoH and DoT encrypt the transport, preventing passive
observation. For full protection, you need both: DNSSEC for data integrity, DoH/DoT
for transport privacy.

**DNSSEC adoption challenges.** As of 2024, approximately 30% of TLDs and 3–5% of second-
level domains are DNSSEC-signed. The main obstacle is operational complexity: key rollover
requires coordinated updates to DS records in the parent zone, and a misconfigured DNSSEC
zone causes SERVFAIL for all validating resolvers — a catastrophic failure mode for any
domain.

**DNS rebinding attacks.** An attacker controls `evil.com`. They return a short-TTL
response pointing to `evil.com`'s IP. After the TTL expires, the attacker's DNS server
returns the victim's internal IP (e.g., 192.168.1.1). A browser that has cached `evil.com`
now sends requests to the victim's internal network, bypassing the Same-Origin Policy.
Defense: BIND/unbound's `no-dns-rebinding` option; browsers checking for RFC 1918
addresses in responses from public names.

## Common Pitfalls

1. **Not validating pointer targets.** A compressed DNS name pointer with target offset
   pointing back to itself (offset N pointing to offset N) is a valid encoding of an
   infinite loop. Always track visited offsets and reject loops.

2. **Truncating at 512 bytes without EDNS0.** Legacy DNS over UDP truncates at 512 bytes
   and sets the TC (truncated) bit, prompting the client to retry over TCP. DNSSEC
   responses are often larger than 512 bytes. All modern resolvers support EDNS0 (RFC
   6891), which extends the UDP payload to 4096+ bytes. Implementing DNS without EDNS0
   breaks DNSSEC.

3. **Using DNS TTL for cache expiry without DNSSEC validation.** A poisoned record with
   a long TTL (e.g., 86400 seconds) stays in cache for a day. If you implement a resolver
   that trusts TTLs but does not validate DNSSEC signatures, a single successful poisoning
   attack has a 24-hour effect.

4. **Trusting the "additional" section without validation.** The DNS additional section
   can contain unsolicited records (e.g., glue records). Without DNSSEC, these are
   unverified. A nameserver that inserts forged records in the additional section of a
   response can poison the resolver's cache for domains the resolver did not ask about.

5. **Not setting query deadlines.** A UDP DNS query to a dead server will hang forever
   without a timeout. Always set a read deadline on the UDP socket before querying.

## Exercises

**Exercise 1** (30 min): Use `dig +dnssec +multi @1.1.1.1 cloudflare.com A` to see
DNSSEC signatures. Identify the RRSIG record and the DNSKEY record. Use
`dig @1.1.1.1 +cd cloudflare.com A` (checking disabled) to see what happens when you
bypass DNSSEC validation. Compare the `AD` (Authentic Data) flag in both responses.

**Exercise 2** (2–4h): Extend the Go `resolvePlain` function to support AAAA (IPv6),
CNAME, MX, and TXT records. Parse each RDATA format correctly. Write a test that
resolves `google.com`, verifies at least one A record is returned, and verifies that
the response transaction ID matches the query transaction ID.

**Exercise 3** (4–8h): Implement a minimal DoH client in Go. POST the DNS query (encoded
as `application/dns-message`) to `https://1.1.1.1/dns-query`. Parse the response. Compare
the resolved addresses to those from `resolvePlain` on port 53. Add a DoH resolver to
a small HTTP client that uses DoH for all hostname resolution (override `net.Resolver.Dial`).

**Exercise 4** (8–15h): Implement a caching DNS resolver in Rust using `hickory-dns`.
The resolver should: (a) forward queries to 1.1.1.1 over DoT, (b) cache responses
respecting TTLs, (c) implement DNSSEC validation (reject SERVFAIL from validation
failures), (d) apply Response Rate Limiting to prevent use as an amplifier. Benchmark
the cache hit rate and latency improvement for a realistic query workload (use the
Alexa Top 1M as a query source).

## Further Reading

### Foundational Papers

- RFC 1035 — "Domain Names — Implementation and Specification" (Mockapetris, 1987). The
  original DNS specification. Section 4 covers the wire format; required reading for
  anyone implementing a DNS parser.
- RFC 4035 — "Protocol Modifications for the DNS Security Extensions" (Arends et al.,
  2005). DNSSEC protocol changes; section 5 (authenticating referrals) is the hardest
  part.
- RFC 8484 — "DNS Queries over HTTPS (DoH)". Short and readable; covers the DoH wire
  format and the `application/dns-message` media type.

### Books

- Liu and Albitz, "DNS and BIND" (5th ed., O'Reilly) — the operational reference for
  BIND9, covering zone configuration, DNSSEC key management, and troubleshooting.

### Production Code to Read

- `miekg/dns` (`github.com/miekg/dns`) — particularly `msg.go` for DNS message parsing
  and `server.go` for the server implementation. The library is 15 years old and handles
  every edge case in the DNS spec.
- CoreDNS source (`coredns/coredns`) — the Kubernetes DNS server, built on `miekg/dns`.
  Study `plugin/forward` for resolver forwarding and `plugin/cache` for TTL-based caching.
- Cloudflare's `cloudflare/odoh-go` — Oblivious DoH implementation, which adds a layer
  of anonymity on top of DoH by relaying queries through a proxy.

### Security Advisories / CVEs to Study

- CVE-2008-1447 (Kaminsky attack) — the foundational DNS cache poisoning vulnerability.
- CVE-2020-8617 (BIND TSIG HMAC truncation) — BIND accepted truncated HMAC signatures
  for TSIG-authenticated requests, allowing bypass of zone transfer authentication.
- CVE-2015-5477 (BIND TKEY query crash) — malformed TKEY query caused BIND to assert
  and crash. Demonstrated that DNS parsers are high-value targets.
