# 23. DNS Recursive Resolver

<!--
difficulty: insane
concepts: [dns, recursive-resolution, iterative-queries, root-servers, delegation, caching, ttl, dns-message-format]
tools: [go, dig]
estimated_time: 3h
bloom_level: create
prerequisites: [udp-server-and-client, dns-resolver-and-custom-dialer, building-a-line-based-protocol, custom-wire-protocol]
-->

## Prerequisites

- Go 1.22+ installed
- Completed UDP Server/Client and DNS Resolver exercises
- Understanding of the DNS hierarchy (root servers, TLD servers, authoritative servers)
- Familiarity with DNS record types (A, AAAA, CNAME, NS, SOA) and message format

## Learning Objectives

- **Create** a recursive DNS resolver that starts from root servers and follows delegation chains to resolve domain names
- **Implement** DNS message encoding and decoding per RFC 1035 wire format
- **Design** a TTL-aware cache that stores and evicts DNS records according to their time-to-live

## The Challenge

When you type `example.com` in a browser, a recursive resolver does the hard work: it asks a root server "who handles `.com`?", then asks that TLD server "who handles `example.com`?", then asks the authoritative server for the actual IP address. Each step involves encoding a DNS query, sending it via UDP, parsing the response, and following referrals. A cache avoids repeating queries for records that have not expired.

Your task is to build a recursive DNS resolver from scratch. You will encode and decode DNS messages in the RFC 1035 wire format, implement the recursive resolution algorithm that follows NS delegations from root to authoritative, build a TTL-aware cache, and serve DNS queries on a local UDP port so other tools (like `dig @127.0.0.1`) can use your resolver.

## Requirements

1. Implement DNS message encoding per RFC 1035: header (12 bytes), question section (name, type, class), and resource record sections (answer, authority, additional)
2. Implement DNS name compression: domain names use pointer labels (0xC0 prefix) that reference earlier occurrences in the message
3. Implement the recursive resolution algorithm: start with a hardcoded list of root server IPs, send iterative queries, follow NS referrals in the authority section, use glue records from the additional section when available
4. Handle CNAME chains: when a query returns a CNAME instead of the requested record type, resolve the CNAME target recursively
5. Implement a TTL-aware cache: store resolved records with their TTL, decrement TTL over time, evict expired records, and serve cached responses without querying upstream
6. Serve DNS queries on a configurable UDP port: receive queries from clients (e.g., `dig`), resolve them recursively, and send responses
7. Handle multiple record types: A, AAAA, CNAME, NS, MX, TXT, and SOA
8. Implement query timeout and retry: if a server does not respond within 2 seconds, retry once, then try the next server in the NS set
9. Detect and prevent resolution loops (a CNAME chain that loops back to itself)
10. Log the full resolution chain for debugging: which servers were queried, what referrals were followed, and which answers were cached

## Hints

- The DNS wire format is tricky: names are sequences of length-prefixed labels terminated by a zero byte, and pointers use the two high bits of the length byte as a flag
- Use `encoding/binary.BigEndian` for all 16-bit and 32-bit fields in the DNS header and resource records
- Root server IPs are well-known and stable; hardcode the list of 13 root server IPs (a.root-servers.net through m.root-servers.net)
- The resolution algorithm is fundamentally a loop: send query to current server, check if response has answers; if it has NS referrals, extract the NS target, find the NS IP from glue records or resolve it recursively, and repeat
- Use `sync.Map` or a mutex-protected map with a goroutine that periodically sweeps expired entries for the cache
- Test against real DNS infrastructure carefully -- use low query rates to avoid being rate-limited

## Success Criteria

1. The resolver correctly resolves A records for well-known domains (e.g., `google.com`, `github.com`) starting from root servers
2. CNAME chains are followed correctly (e.g., `www.github.com` -> `github.github.io` -> IP)
3. NS delegation is followed correctly from root -> TLD -> authoritative
4. The cache serves repeated queries without upstream traffic, respecting TTL expiration
5. The resolver serves queries on a UDP port that `dig @127.0.0.1 -p PORT example.com` can query
6. DNS name compression is decoded correctly in responses from real servers
7. Query timeouts trigger retries on alternate servers
8. Resolution loops are detected and return SERVFAIL
9. All tests pass with the `-race` flag enabled

## Research Resources

- [RFC 1035 -- Domain Names -- Implementation and Specification](https://datatracker.ietf.org/doc/html/rfc1035) -- the DNS bible
- [RFC 1034 -- Domain Names -- Concepts and Facilities](https://datatracker.ietf.org/doc/html/rfc1034) -- DNS concepts
- [How DNS Works (comic)](https://howdns.works/) -- visual explanation of recursive resolution
- [Root Server IPs](https://www.iana.org/domains/root/servers) -- hardcoded root server addresses
- [miekg/dns](https://github.com/miekg/dns) -- reference Go DNS library (for comparison, not for use)

## What's Next

Continue to [24 - QUIC Transport Protocol](../24-quic-transport-protocol/24-quic-transport-protocol.md) to implement QUIC transport fundamentals on top of UDP.

## Summary

- Recursive DNS resolution follows a chain from root servers through TLD servers to authoritative servers
- The DNS wire format uses length-prefixed labels with pointer-based name compression
- DNS messages have a fixed 12-byte header followed by question, answer, authority, and additional sections
- TTL-aware caching avoids redundant queries and reduces resolution latency
- CNAME chains require recursive re-resolution of the target name
- Glue records in the additional section provide IP addresses for NS servers to avoid circular dependencies
