# 118. DNS Authoritative Server

<!--
difficulty: advanced
category: networking-and-protocols
languages: [go]
concepts: [dns, authoritative-server, zone-files, udp, tcp, dns-wire-format, record-types, edns0, dnssec-awareness, domain-name-compression]
estimated_time: 14-18 hours
bloom_level: evaluate
prerequisites: [go-basics, udp-tcp-sockets, binary-encoding, file-parsing, goroutines, dns-fundamentals]
-->

## Languages

- Go (1.22+)

## Prerequisites

- UDP and TCP socket programming with `net.PacketConn` and `net.Listener`
- Binary data encoding: big-endian integers, byte slicing, domain name wire format
- Text file parsing (for zone files)
- Goroutines for concurrent UDP and TCP handling
- Basic DNS concepts: queries, responses, record types, TTL, authority

## Learning Objectives

- **Implement** the DNS wire format for encoding and decoding messages, including header, question, and resource record sections per RFC 1035
- **Analyze** DNS domain name compression (label pointers) and correctly decode names that use both literal labels and pointers
- **Design** a zone file parser that loads SOA, NS, A, AAAA, CNAME, MX, and TXT records from standard zone file syntax
- **Evaluate** when to use UDP versus TCP for DNS responses, including truncation handling and the TC flag
- **Implement** EDNS0 (RFC 6891) to support larger UDP payloads and advertise server capabilities
- **Build** a concurrent authoritative DNS server that serves zone data over both UDP and TCP with correct response codes

## The Challenge

DNS is the phone book of the internet. Every time you type a URL in your browser, send an email, or connect to an API, a DNS query resolves a human-readable name into an IP address. Authoritative DNS servers hold the ground truth for domain zones -- they are the definitive source for what `example.com`'s IP address is, where its mail should be delivered, and which nameservers are responsible for the zone.

Your task is to build an authoritative DNS server in Go. No `miekg/dns`, no `net.Resolver`, no third-party libraries -- just `net.PacketConn`, `net.Listener`, and the RFCs. You will parse zone files into an in-memory database, encode and decode DNS messages at the wire level, serve queries over both UDP and TCP, implement domain name compression, handle EDNS0 for larger payloads, and return DNSSEC records if they are present in the zone.

The DNS wire format is deceptively compact. Domain names are sequences of length-prefixed labels terminated by a zero byte, but they can also use compression pointers -- two bytes that reference a previously seen name in the message. Getting compression right is critical for responses that fit within the 512-byte UDP limit. Record types each have their own RDATA format: A records are 4 bytes, AAAA records are 16 bytes, MX records have a 16-bit preference followed by a domain name, and TXT records are sequences of character strings. Every field has a specific encoding that must be exactly right for resolvers to accept the response.

Zone files use a text format defined in RFC 1035 Section 5. The SOA record defines the zone's authority parameters, NS records delegate to nameservers, and the various data records (A, AAAA, CNAME, MX, TXT) provide the actual mappings. The `$ORIGIN` and `$TTL` directives control defaults, and the `@` symbol refers to the current origin. Parsing this correctly requires handling multi-line records (parentheses), relative vs. absolute domain names, and escape sequences in TXT records.

## Requirements

1. Implement DNS message encoding and decoding per RFC 1035 Section 4: message header (ID, QR, OPCODE, AA, TC, RD, RA, RCODE, question count, answer count, authority count, additional count), question section, and resource record sections
2. Implement domain name encoding and decoding with compression (RFC 1035 Section 4.1.4): encode as length-prefixed labels, decode with support for pointer labels (two most significant bits set, remaining 14 bits are an offset into the message)
3. Implement domain name compression in response encoding: maintain a compression table of previously written names and emit pointers when a suffix has already been written
4. Implement resource record parsing for RDATA: A (4-byte IPv4), AAAA (16-byte IPv6), CNAME (domain name), MX (16-bit preference + domain name), TXT (one or more character strings, each prefixed with a length byte), NS (domain name), SOA (MNAME, RNAME, serial, refresh, retry, expire, minimum)
5. Implement zone file parsing: read a text zone file supporting `$ORIGIN`, `$TTL`, the `@` shorthand, relative domain names (appended to origin), multi-line records in parentheses, and the seven record types above
6. Listen on UDP port 53 (or configurable port) and respond to DNS queries: match the query name and type against the zone data, set the AA (authoritative answer) flag, and return appropriate answers in the answer section with authority and additional sections as needed
7. Listen on TCP port 53 with the 2-byte length prefix framing: DNS over TCP prepends a 16-bit big-endian length before each message
8. Handle response truncation: if the UDP response exceeds the maximum size (512 bytes default, or the EDNS0 buffer size), set the TC (truncated) flag and truncate the response, prompting the client to retry over TCP
9. Implement EDNS0 (RFC 6891): parse OPT pseudo-records in the additional section, extract the client's UDP buffer size, and include an OPT record in the response advertising the server's buffer size
10. Return DNSSEC records (RRSIG, DNSKEY) if they are present in the zone file, without performing signing (the server serves pre-signed zones)
11. Implement correct RCODE responses: NOERROR (0) for successful queries, NXDOMAIN (3) for names that do not exist in the zone, REFUSED (5) for queries outside the zone, and FORMERR (1) for malformed queries
12. Handle wildcard records (`*.example.com`) per RFC 1034 Section 4.3.3: match query names that have no exact match but fall under a wildcard
13. Include SOA record in the authority section for NXDOMAIN responses (negative caching per RFC 2308)

## Hints

<details>
<summary>Hint 1: DNS message header format</summary>

The header is exactly 12 bytes:
```
  0  1  2  3  4  5  6  7  8  9 10 11 12 13 14 15
+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+
|                      ID                         |
+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+
|QR|   Opcode  |AA|TC|RD|RA|   Z    |   RCODE     |
+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+
|                    QDCOUNT                       |
+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+
|                    ANCOUNT                       |
+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+
|                    NSCOUNT                       |
+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+
|                    ARCOUNT                       |
+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+
```

Use bit shifting and masking to extract the flag fields from the second 16-bit word. The QR bit (bit 15) distinguishes queries (0) from responses (1).
</details>

<details>
<summary>Hint 2: Domain name compression decoding</summary>

When reading a domain name, check the two most significant bits of each label length byte:
- `00` = literal label (remaining 6 bits are the length)
- `11` = pointer (remaining 14 bits are the offset into the message)

Pointers can appear at any position in a name, and they always terminate the name (no more labels follow a pointer). Watch out for pointer loops: limit the number of pointer follows to prevent infinite recursion on malformed packets.
</details>

<details>
<summary>Hint 3: Zone file parsing with parentheses</summary>

Zone file records can span multiple lines when enclosed in parentheses. When you encounter an opening parenthesis, continue reading lines until the matching closing parenthesis, then join them and parse as a single record. Strip comments (lines starting with `;` or anything after `;` outside quotes).
</details>

<details>
<summary>Hint 4: EDNS0 OPT record structure</summary>

The OPT pseudo-record has a specific encoding:
- Name: root (single zero byte)
- Type: 41 (OPT)
- Class: requestor's UDP payload size (reuses the class field)
- TTL: extended RCODE and flags (including the DO bit for DNSSEC OK)
- RDLENGTH: length of options
- RDATA: zero or more {option-code, option-length, option-data} tuples

Parse the class field as the client's buffer size and use it to determine the maximum response size for UDP.
</details>

## Acceptance Criteria

- [ ] DNS messages are correctly encoded and decoded with valid header, question, and resource record sections
- [ ] Domain names with compression pointers are decoded correctly, including names that use multiple levels of pointers
- [ ] Response encoding uses domain name compression to minimize message size
- [ ] Zone files are parsed correctly with support for `$ORIGIN`, `$TTL`, `@`, relative names, multi-line records, and all seven record types (SOA, NS, A, AAAA, CNAME, MX, TXT)
- [ ] UDP queries receive authoritative responses with the AA flag set
- [ ] TCP queries work with the 2-byte length prefix framing
- [ ] Queries for names in the zone receive NOERROR with correct answer records
- [ ] Queries for non-existent names receive NXDOMAIN with the SOA record in the authority section
- [ ] Queries outside the zone receive REFUSED
- [ ] `dig` (or `kdig`) can query the server and receive valid responses for all record types
- [ ] EDNS0 is supported: the server parses OPT records and includes one in responses
- [ ] Large responses that exceed the UDP buffer size have the TC flag set
- [ ] Wildcard records match appropriately when no exact match exists
- [ ] RRSIG and DNSKEY records from the zone file are served correctly when queried

## Research Resources

- [RFC 1035: Domain Names - Implementation and Specification](https://datatracker.ietf.org/doc/html/rfc1035) -- the foundational DNS specification, Sections 3 (domain name space), 4 (messages), and 5 (master files / zone files)
- [RFC 1034: Domain Names - Concepts and Facilities](https://datatracker.ietf.org/doc/html/rfc1034) -- DNS concepts including wildcards (Section 4.3.3) and the resolution algorithm
- [RFC 6891: Extension Mechanisms for DNS (EDNS0)](https://datatracker.ietf.org/doc/html/rfc6891) -- the OPT pseudo-record format, extended UDP buffer sizes, and extended RCODEs
- [RFC 2308: Negative Caching of DNS Queries](https://datatracker.ietf.org/doc/html/rfc2308) -- SOA in authority section for NXDOMAIN, minimum TTL for negative caching
- [RFC 4034: Resource Records for the DNS Security Extensions](https://datatracker.ietf.org/doc/html/rfc4034) -- RRSIG, DNSKEY, DS, and NSEC record types and wire formats
- [`dig` command reference](https://linux.die.net/man/1/dig) -- essential tool for testing DNS servers; `dig @127.0.0.1 -p 1053 example.com A` queries your server directly
- [DNS wire format visualizer](https://www.netmeister.org/blog/dns-size.html) -- helps understand how message size accumulates across sections
