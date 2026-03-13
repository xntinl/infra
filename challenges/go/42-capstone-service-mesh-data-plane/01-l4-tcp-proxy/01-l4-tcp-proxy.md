# 1. L4 TCP Proxy

<!--
difficulty: insane
concepts: [tcp-proxy, connection-tracking, connection-pooling, half-close, net-listener, bidirectional-copy, graceful-drain, timeouts]
tools: [go]
estimated_time: 3h
bloom_level: create
prerequisites: [33-tcp-udp-and-networking, 13-goroutines-and-channels, 15-sync-primitives]
-->

## Prerequisites

- Go 1.22+ installed
- Completed Section 33 (TCP/UDP networking), 13-14 (concurrency), and 15 (sync primitives) or equivalent experience
- Familiarity with the TCP protocol, connection lifecycle, and socket options

## Learning Objectives

- **Design** a production-grade L4 TCP proxy with full connection lifecycle management
- **Create** a connection tracking table that monitors active connections, byte counts, and durations
- **Evaluate** trade-offs between per-connection goroutines and connection pooling strategies for upstream backends

## The Challenge

Service mesh data planes like Envoy operate at two layers: L4 (transport) and L7 (application). The L4 proxy is the foundation -- it accepts TCP connections from downstream clients, establishes connections to upstream backends, and bidirectionally copies bytes between them without inspecting the payload. This sounds simple, but production-grade implementations must handle half-closed connections, idle timeouts, connection draining, backend health, and connection tracking.

Your task is to build an L4 TCP proxy that accepts inbound connections, maps them to upstream backends using a configurable routing table, and transparently proxies traffic in both directions. The proxy must maintain a connection tracking table that records every active connection's source, destination, bytes transferred, and duration. It must handle TCP half-close correctly (one side closes their write end while the other continues sending), enforce configurable idle timeouts, and support graceful shutdown by draining active connections before exiting.

The real challenge is not just copying bytes -- it is managing the full connection lifecycle correctly under concurrent load while maintaining accurate metrics and supporting operational controls like connection draining and maximum connection limits.

## Requirements

1. Implement a TCP listener that accepts inbound connections and proxies them to a configurable upstream address
2. Support multiple upstream backends with a simple round-robin selection for each new connection
3. Implement bidirectional byte copying using `io.Copy` or equivalent, handling EOF and errors on each direction independently
4. Handle TCP half-close correctly: when one side sends FIN, close only the write end to the other side while continuing to read from the still-open direction
5. Maintain a connection tracking table that records: connection ID, client address, upstream address, bytes sent, bytes received, connection start time, and current state (active, draining, closed)
6. Enforce configurable idle timeouts using `net.Conn.SetDeadline` -- close connections that have had no data transfer for a configurable duration
7. Enforce a maximum concurrent connections limit, returning TCP RST to new connections when the limit is reached
8. Implement graceful shutdown: stop accepting new connections, send a configurable drain timeout to active connections, then force-close remaining connections
9. Expose a metrics summary method returning total connections handled, active connections, bytes transferred, and error counts
10. All connection tracking operations must be safe for concurrent access
11. Write integration tests using loopback connections that verify bidirectional data transfer, half-close behavior, and idle timeout enforcement

## Hints

- Use `net.TCPConn` specifically (type-assert from `net.Conn`) to access `CloseRead()` and `CloseWrite()` for half-close support
- Spawn two goroutines per proxied connection (one for each direction) and use a `sync.WaitGroup` to detect when both directions have completed
- For idle timeouts, reset the deadline on every successful read by wrapping the connection in a custom `net.Conn` that calls `SetDeadline` on each `Read`
- Use a `sync.Map` or a mutex-protected map for the connection tracking table -- connections are added and removed frequently from different goroutines
- For graceful shutdown, use a `context.Context` passed to the accept loop and a separate timer for the drain period
- Track bytes transferred by wrapping `io.Copy` with an `io.TeeReader` or a custom `io.Writer` that increments an atomic counter

## Success Criteria

1. The proxy correctly forwards TCP traffic bidirectionally between client and upstream
2. Half-close is handled correctly -- closing one direction does not terminate the other
3. The connection tracking table accurately reflects all active connections and byte counts
4. Idle connections are closed after the configured timeout
5. New connections are rejected when the maximum connection limit is reached
6. Graceful shutdown drains active connections within the configured timeout
7. The proxy handles at least 100 concurrent connections without goroutine leaks
8. All tests pass with the `-race` flag enabled

## Research Resources

- [Go net package](https://pkg.go.dev/net) -- TCP listener, connection, and half-close APIs
- [Envoy L4 filter architecture](https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/listeners/network_filters) -- reference design for L4 proxy filters
- [TCP half-close explained](https://thenotexpert.com/tcp-half-close/) -- understanding FIN vs RST and half-close semantics
- [Go io.Copy](https://pkg.go.dev/io#Copy) -- efficient byte copying between connections
- [HAProxy connection management](https://docs.haproxy.org/2.8/configuration.html#4.2-timeout%20client) -- production timeout and connection management patterns

## What's Next

Continue to [L7 HTTP Proxy](../02-l7-http-proxy/02-l7-http-proxy.md) where you will build on top of this L4 foundation to add HTTP-aware routing and header manipulation.

## Summary

- L4 proxies operate at the TCP level, transparently forwarding bytes without inspecting payload content
- Half-close handling requires using `CloseRead`/`CloseWrite` on `net.TCPConn` to support unidirectional shutdown
- Connection tracking tables provide observability into active proxy state for operational tooling
- Idle timeouts prevent resource leaks from abandoned connections
- Graceful shutdown with connection draining ensures in-flight requests complete before the proxy exits
- Concurrent connection limits protect the proxy from resource exhaustion under load
