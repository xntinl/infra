# 11. HTTP Keep-Alive Analysis

<!--
difficulty: advanced
concepts: [http-keep-alive, connection-reuse, http-transport, idle-connections, connection-state]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [tcp-server-and-client, dns-resolver-and-custom-dialer, http-programming]
-->

## Prerequisites

- Go 1.22+ installed
- Completed TCP Server and Client and DNS Resolver exercises
- Understanding of HTTP/1.1 persistent connections
- Familiarity with `net/http` client and transport

## Learning Objectives

After completing this exercise, you will be able to:

- **Analyze** HTTP keep-alive behavior and connection reuse in Go's `http.Transport`
- **Configure** `MaxIdleConns`, `MaxIdleConnsPerHost`, `IdleConnTimeout`, and `MaxConnsPerHost` for production workloads
- **Demonstrate** the difference between keep-alive enabled and disabled using connection tracking
- **Measure** the performance impact of connection reuse vs new connections

## Why HTTP Keep-Alive Matters

HTTP/1.1 defaults to persistent connections (keep-alive). Instead of opening a TCP connection, performing one request, and closing, the client reuses the same connection for multiple requests. This avoids the overhead of TCP handshakes and TLS negotiation. However, misconfigured idle connection pools can leak file descriptors, exhaust ports, or hold connections to backends that have long since rotated.

Understanding how Go's transport manages its connection pool is critical for building efficient HTTP clients and diagnosing connection-related issues in production.

## The Problem

Build an HTTP connection reuse analyzer that:

1. Tracks which connections are reused vs newly created
2. Measures latency difference between first-request and subsequent requests on the same connection
3. Demonstrates the effect of transport configuration on connection pooling behavior

## Requirements

1. **Custom transport with tracing** -- use `httptrace.ClientTrace` to hook into `GotConn` and detect whether a connection was reused
2. **Connection counter** -- track total connections created vs reused across a series of requests
3. **Keep-alive vs no-keep-alive** -- run the same request sequence with `DisableKeepAlives: true` and compare connection counts
4. **Idle pool configuration** -- demonstrate the effect of `MaxIdleConnsPerHost` by sending requests to the same host with different pool sizes
5. **Idle timeout** -- show that connections are closed after `IdleConnTimeout` expires by waiting between requests
6. **Latency comparison** -- measure and report mean latency for reused vs new connections
7. **Tests** -- verify connection reuse behavior, idle timeout eviction, and pool size limits

## Hints

<details>
<summary>Hint 1: Using httptrace to detect reuse</summary>

```go
import "net/http/httptrace"

trace := &httptrace.ClientTrace{
    GotConn: func(info httptrace.GotConnInfo) {
        if info.Reused {
            reusedCount++
        } else {
            newCount++
        }
    },
}
req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
```

</details>

<details>
<summary>Hint 2: Transport configuration</summary>

```go
transport := &http.Transport{
    MaxIdleConns:        100,
    MaxIdleConnsPerHost: 10,
    IdleConnTimeout:     5 * time.Second,
    MaxConnsPerHost:     20,
    DisableKeepAlives:   false,
}
client := &http.Client{Transport: transport}
```

</details>

<details>
<summary>Hint 3: Draining response body for reuse</summary>

```go
// The connection is only returned to the pool if the body is fully read and closed
resp, err := client.Do(req)
if err == nil {
    io.Copy(io.Discard, resp.Body)
    resp.Body.Close()
}
```

</details>

## Verification

```bash
go test -v -race ./...
```

Your tests should confirm:
- Sequential requests to the same host reuse the same connection (GotConn reports `Reused: true`)
- With `DisableKeepAlives: true`, every request creates a new connection
- Setting `MaxIdleConnsPerHost: 1` limits idle connections to one per host
- After `IdleConnTimeout` expires, the next request creates a new connection
- Reused connections have lower latency than new connections

## What's Next

Continue to [12 - HTTP Client Instrumentation](../12-http-client-instrumentation/12-http-client-instrumentation.md) to add full request lifecycle tracing to HTTP clients.

## Summary

- Go's `http.Transport` maintains a pool of idle connections keyed by scheme, host, and port
- `httptrace.ClientTrace.GotConn` reports whether a connection was reused or newly created
- The response body must be fully read and closed for the connection to be returned to the pool
- `MaxIdleConnsPerHost` controls how many idle connections are kept per destination
- `IdleConnTimeout` evicts connections that have been idle too long
- `DisableKeepAlives` forces a new connection for every request, useful for testing

## Reference

- [net/http/httptrace](https://pkg.go.dev/net/http/httptrace)
- [http.Transport](https://pkg.go.dev/net/http#Transport)
- [HTTP Keep-Alive in Go](https://blog.cloudflare.com/the-complete-guide-to-golang-net-http-timeouts/)
