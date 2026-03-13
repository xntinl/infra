# 25. HTTP/3 over QUIC

<!--
difficulty: insane
concepts: [http3, quic, alt-svc, qpack, server-push, http3-client, http3-server, protocol-negotiation]
tools: [go, curl]
estimated_time: 3h
bloom_level: create
prerequisites: [quic-transport-protocol, http-keep-alive-analysis, custom-http-transport, tls-server-and-client]
-->

## Prerequisites

- Go 1.22+ installed
- Completed QUIC Transport Protocol exercise
- Understanding of HTTP/2 concepts (multiplexing, header compression, server push)
- Familiarity with `quic-go` and TLS 1.3 configuration

## Learning Objectives

- **Create** an HTTP/3 server and client using `quic-go/http3` that serves requests over QUIC
- **Design** a service that supports both HTTP/2 (TCP) and HTTP/3 (QUIC) with `Alt-Svc` header discovery
- **Evaluate** HTTP/3 vs HTTP/2 performance under simulated packet loss and high-latency conditions

## The Challenge

HTTP/3 replaces TCP with QUIC as the transport for HTTP. This eliminates head-of-line blocking at the transport layer (HTTP/2 over TCP still suffers from it), reduces connection establishment time, and enables connection migration. The `Alt-Svc` header allows servers to advertise HTTP/3 support so clients can upgrade transparently.

Your task is to build a service that serves content over both HTTP/2 (TCP) and HTTP/3 (QUIC). The HTTP/2 server advertises HTTP/3 availability via `Alt-Svc`. You will implement an HTTP/3 client that discovers and uses HTTP/3, and measure the performance difference between HTTP/2 and HTTP/3 under various network conditions.

## Requirements

1. Implement an HTTP/3 server using `quic-go/http3` that serves the same handlers as a standard HTTP server
2. Implement a dual-stack server that listens on both TCP (HTTP/2) and UDP (HTTP/3) on the same port
3. Add `Alt-Svc` header to HTTP/2 responses advertising HTTP/3 support (e.g., `Alt-Svc: h3=":443"; ma=86400`)
4. Implement an HTTP/3 client using `http3.RoundTripper` that connects to the server over QUIC
5. Implement protocol discovery: start with HTTP/2, detect `Alt-Svc`, upgrade to HTTP/3 for subsequent requests
6. Build a benchmark that compares HTTP/2 vs HTTP/3: measure connection establishment time, request latency for multiplexed requests, and behavior under simulated packet loss
7. Demonstrate stream multiplexing advantage: send 20 concurrent requests and compare total completion time between HTTP/2 (where packet loss blocks all streams) and HTTP/3 (where only affected streams are blocked)
8. Configure TLS 1.3 with ALPN negotiation (`h3` for HTTP/3)
9. Handle HTTP/3 graceful shutdown using QUIC connection draining
10. Write tests that verify correct HTTP/3 request/response handling and `Alt-Svc` discovery

## Hints

- `quic-go/http3` provides `http3.Server` which wraps a standard `http.Handler`
- The HTTP/3 server and HTTP/2 server can share the same `http.ServeMux`
- For `Alt-Svc`, add a middleware that injects the header into every HTTP/2 response
- Use `http3.RoundTripper` as the transport for an `http.Client` to make HTTP/3 requests
- To simulate packet loss for benchmarking, use `tc` (Linux) or proxy through a connection that drops packets randomly
- `curl --http3` can test your HTTP/3 server if compiled with QUIC support

## Success Criteria

1. The HTTP/3 server correctly handles GET and POST requests over QUIC
2. The dual-stack server serves both HTTP/2 (TCP) and HTTP/3 (QUIC) simultaneously
3. `Alt-Svc` headers are present in HTTP/2 responses with correct HTTP/3 advertisement
4. The HTTP/3 client successfully connects and exchanges data over QUIC
5. Protocol discovery correctly upgrades from HTTP/2 to HTTP/3 based on `Alt-Svc`
6. Benchmarks show HTTP/3 connection establishment is faster than HTTP/2 (fewer round trips)
7. Under multiplexed load, HTTP/3 demonstrates independence of streams
8. All tests pass with the `-race` flag enabled

## Research Resources

- [quic-go/http3](https://github.com/quic-go/quic-go) -- Go HTTP/3 implementation
- [RFC 9114 -- HTTP/3](https://datatracker.ietf.org/doc/html/rfc9114) -- HTTP/3 specification
- [RFC 7838 -- HTTP Alternative Services](https://datatracker.ietf.org/doc/html/rfc7838) -- Alt-Svc header
- [HTTP/3 Explained (Daniel Stenberg)](https://http3-explained.haxx.se/) -- accessible overview
- [Cloudflare HTTP/3 Blog](https://blog.cloudflare.com/http3-the-past-present-and-future/) -- real-world deployment insights

## What's Next

Continue to [26 - VPN Tunnel Implementation](../26-vpn-tunnel-implementation/26-vpn-tunnel-implementation.md) to build a point-to-point VPN tunnel using TUN devices.

## Summary

- HTTP/3 uses QUIC as its transport, eliminating TCP head-of-line blocking for multiplexed requests
- Dual-stack servers serve HTTP/2 over TCP and HTTP/3 over QUIC simultaneously
- `Alt-Svc` headers let HTTP/2 servers advertise HTTP/3 availability for transparent client upgrades
- `quic-go/http3` provides `http3.Server` and `http3.RoundTripper` for server and client implementations
- HTTP/3 connection establishment requires fewer round trips than HTTP/2 over TCP+TLS
- QPACK replaces HPACK for header compression, adapted for QUIC's out-of-order delivery
