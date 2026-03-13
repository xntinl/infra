# 24. QUIC Transport Protocol

<!--
difficulty: insane
concepts: [quic, udp-transport, stream-multiplexing, 0-rtt, connection-migration, tls13, quic-go]
tools: [go]
estimated_time: 4h
bloom_level: create
prerequisites: [udp-server-and-client, tls-server-and-client, mutual-tls-authentication, connection-pooling-implementation]
-->

## Prerequisites

- Go 1.22+ installed
- Completed UDP Server/Client, TLS Server/Client, and Connection Pooling exercises
- Understanding of TCP limitations (head-of-line blocking, handshake latency)
- Familiarity with TLS 1.3 concepts (0-RTT, key exchange)

## Learning Objectives

- **Create** a QUIC-based client and server using `quic-go` that demonstrates stream multiplexing, 0-RTT, and connection migration
- **Evaluate** QUIC vs TCP performance characteristics for latency, multiplexing, and connection establishment
- **Design** an application protocol on top of QUIC streams with proper stream lifecycle management

## The Challenge

QUIC is a transport protocol built on UDP that solves fundamental problems with TCP: head-of-line blocking (one lost packet stalls all streams), slow connection establishment (TCP handshake + TLS handshake = 2-3 round trips), and connection binding to IP addresses (TCP connections break when the client changes networks).

QUIC integrates TLS 1.3 into the transport layer for 1-RTT (or 0-RTT for repeat connections), multiplexes independent streams within a single connection (a lost packet on one stream does not block others), and supports connection migration (connections survive IP address changes).

Your task is to build a QUIC-based file transfer service that demonstrates stream multiplexing, 0-RTT connection establishment, and the performance advantages over TCP. You will use `quic-go` (the Go implementation of QUIC) and build a protocol that transfers multiple files concurrently over independent QUIC streams within a single connection.

## Requirements

1. Implement a QUIC server using `quic-go` that accepts connections and handles multiple streams per connection
2. Implement a QUIC client that opens a connection and creates multiple streams for concurrent requests
3. Design a request/response protocol over QUIC streams: each stream carries one file transfer request and response
4. Demonstrate stream multiplexing: transfer 10 files concurrently over 10 streams within a single QUIC connection
5. Implement 0-RTT connection establishment: on the first connection, perform a full handshake; on subsequent connections, use 0-RTT to send data immediately
6. Compare latency: measure connection establishment time for TCP+TLS vs QUIC first-connection vs QUIC 0-RTT
7. Demonstrate stream independence: artificially delay one stream's data and show that other streams are not blocked (unlike TCP)
8. Implement proper stream lifecycle: open stream, send request, receive response, close stream, handle errors
9. Configure TLS 1.3 with self-signed certificates for the QUIC connection
10. Track per-stream metrics: bytes transferred, duration, stream ID
11. Write tests that verify multiplexed transfer, 0-RTT savings, and stream independence

## Hints

- `quic-go` provides `quic.Listener` (server) and `quic.Dial` (client), which return `quic.Connection` objects supporting `OpenStream()` and `AcceptStream()`
- Each `quic.Stream` implements `io.ReadWriteCloser`, so you can use it like a TCP connection
- For 0-RTT, configure `quic.Config` with `Allow0RTT: true` on the server and use `quic.DialEarly` on the client
- To demonstrate stream independence, use `OpenStreamSync` and write data with deliberate delays on one stream while measuring throughput on others
- Generate TLS certificates using `crypto/tls` and `crypto/x509` as in the TLS exercises, but ensure the config uses TLS 1.3

## Success Criteria

1. The server accepts QUIC connections and handles concurrent streams from a single connection
2. 10 files transfer concurrently over 10 streams within one connection
3. 0-RTT connection establishment is measurably faster than 1-RTT on repeat connections
4. Delayed data on one stream does not reduce throughput on other streams
5. Connection establishment latency comparison shows QUIC advantage over TCP+TLS
6. Per-stream metrics accurately report bytes and duration
7. Stream errors on one stream do not crash other streams or the connection
8. All tests pass with the `-race` flag enabled

## Research Resources

- [quic-go](https://github.com/quic-go/quic-go) -- Go QUIC implementation
- [RFC 9000 -- QUIC: A UDP-Based Multiplexed and Secure Transport](https://datatracker.ietf.org/doc/html/rfc9000) -- the QUIC specification
- [RFC 9001 -- Using TLS to Secure QUIC](https://datatracker.ietf.org/doc/html/rfc9001) -- QUIC-TLS integration
- [QUIC Working Group](https://quicwg.org/) -- specifications and resources
- [How QUIC Works (Cloudflare)](https://blog.cloudflare.com/the-road-to-quic/) -- accessible overview

## What's Next

Continue to [25 - HTTP/3 over QUIC](../25-http3-over-quic/25-http3-over-quic.md) to build HTTP/3 services on top of the QUIC transport.

## Summary

- QUIC provides multiplexed streams over a single UDP connection, eliminating TCP head-of-line blocking
- TLS 1.3 is integrated into QUIC's handshake, enabling 1-RTT connections (0-RTT for repeat connections)
- Each QUIC stream is independent: loss on one stream does not affect others
- `quic-go` implements the full QUIC stack with an API similar to `net.Listener`/`net.Conn`
- Connection migration allows QUIC connections to survive network changes (WiFi to cellular)
- Stream multiplexing replaces the need for connection pooling -- one connection supports thousands of concurrent streams
