# 19. Building a SOCKS5 Proxy

<!--
difficulty: insane
concepts: [socks5, proxy-protocol, authentication-negotiation, tcp-relay, connect-command, bind-command, udp-associate]
tools: [go, curl]
estimated_time: 90m
bloom_level: create
prerequisites: [concurrent-tcp-server, connection-pooling-implementation, tls-server-and-client, connection-draining]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-18 or equivalent networking experience
- Deep understanding of TCP connection lifecycle, binary protocol parsing, and concurrent server design
- Familiarity with the SOCKS protocol concept (client connects to proxy, proxy connects to destination)

## Learning Objectives

- **Create** a SOCKS5 proxy server implementing the CONNECT command per RFC 1928
- **Implement** the SOCKS5 authentication negotiation handshake (no-auth and username/password)
- **Design** bidirectional TCP relay with proper half-close and connection tracking

## The Challenge

SOCKS5 is a binary protocol where clients negotiate authentication, then request the proxy to establish a TCP connection to a target host. The proxy performs the connection and relays bytes bidirectionally. Unlike HTTP proxies that understand HTTP semantics, SOCKS5 operates at the transport layer and can proxy any TCP protocol.

Your task is to build a SOCKS5 proxy from scratch by parsing the binary protocol handshake, implementing the CONNECT command, and relaying traffic. The implementation must handle concurrent clients, authentication, DNS resolution on the proxy side, and proper error reporting using SOCKS5 reply codes.

## Requirements

1. Parse the SOCKS5 version identifier/method selection message: the client sends version (0x05), number of methods, and method list
2. Implement method negotiation: support no-auth (0x00) and username/password auth (0x02), and reject unsupported methods with 0xFF
3. Implement username/password authentication per RFC 1929: parse the sub-negotiation, validate credentials, and respond with success (0x00) or failure (0x01)
4. Implement the CONNECT command (0x01): parse the request (version, command, reserved, address type, destination address, destination port), establish a TCP connection to the target, and send the reply
5. Support all three address types: IPv4 (0x01), domain name (0x03), and IPv6 (0x04)
6. Resolve domain names on the proxy side using `net.Resolver`, not the client side
7. Implement bidirectional TCP relay between client and destination with proper half-close handling
8. Return correct SOCKS5 reply codes: success (0x00), general failure (0x01), connection refused (0x05), host unreachable (0x04), network unreachable (0x03)
9. Track active connections with metrics: total connections, active relays, bytes transferred per connection
10. Support graceful shutdown with connection draining
11. Write integration tests that use the proxy to connect to a local test server and transfer data

## Hints

- The SOCKS5 handshake is fully specified in RFC 1928 -- read it carefully, as every byte position matters
- Use `io.ReadFull` for reading fixed-length protocol fields to handle partial reads correctly
- For the relay phase, spawn two goroutines (client->dest, dest->client) and use `sync.WaitGroup` to detect when both directions close
- Type-assert `net.Conn` to `*net.TCPConn` to call `CloseWrite()` for half-close support
- Test with `curl --socks5 127.0.0.1:1080 http://example.com` or Go's `golang.org/x/net/proxy` package

## Success Criteria

1. The proxy completes the SOCKS5 handshake with both no-auth and username/password authentication
2. CONNECT command successfully establishes a TCP connection through the proxy
3. Domain names are resolved on the proxy side, not by the client
4. Bidirectional data transfer works correctly through the proxy
5. Invalid credentials are rejected with the correct SOCKS5 error code
6. Connection to unreachable hosts returns appropriate SOCKS5 reply codes
7. The proxy handles at least 50 concurrent connections without goroutine leaks
8. All tests pass with the `-race` flag enabled
9. Graceful shutdown drains active relay connections

## Research Resources

- [RFC 1928 -- SOCKS Protocol Version 5](https://datatracker.ietf.org/doc/html/rfc1928)
- [RFC 1929 -- Username/Password Authentication for SOCKS V5](https://datatracker.ietf.org/doc/html/rfc1929)
- [golang.org/x/net/proxy](https://pkg.go.dev/golang.org/x/net/proxy) -- Go SOCKS5 client for testing
- [Go net package](https://pkg.go.dev/net) -- TCP connections and DNS resolution

## What's Next

Continue to [20 - Custom Wire Protocol](../20-custom-wire-protocol/20-custom-wire-protocol.md) to design and implement a custom binary protocol from scratch.

## Summary

- SOCKS5 is a binary protocol with a three-phase lifecycle: method negotiation, authentication, and command execution
- The CONNECT command instructs the proxy to establish a TCP connection to a target and relay traffic
- Three address types are supported: IPv4 (4 bytes), domain (length-prefixed string), IPv6 (16 bytes)
- Reply codes communicate specific errors (refused, unreachable) back to the client
- Half-close handling ensures unidirectional shutdown does not terminate the other direction
- Production proxies need connection tracking, metrics, and graceful shutdown
