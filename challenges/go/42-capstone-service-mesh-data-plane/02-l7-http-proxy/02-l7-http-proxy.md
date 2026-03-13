# 2. L7 HTTP Proxy

<!--
difficulty: insane
concepts: [http-proxy, reverse-proxy, header-manipulation, host-routing, path-routing, request-rewriting, hop-by-hop-headers, connection-upgrade]
tools: [go]
estimated_time: 3h
bloom_level: create
prerequisites: [42-capstone-service-mesh-data-plane/01-l4-tcp-proxy, 17-http-programming, 33-tcp-udp-and-networking]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercise 01-l4-tcp-proxy or equivalent L4 proxy experience
- Completed Section 17 (HTTP programming) and 33 (networking) or equivalent experience

## Learning Objectives

- **Design** an L7 HTTP reverse proxy that parses, routes, and forwards HTTP requests with full header manipulation
- **Create** a routing engine supporting host-based and path-based routing with configurable rewrite rules
- **Evaluate** the differences between hop-by-hop and end-to-end headers and their impact on proxy correctness

## The Challenge

Moving from L4 to L7 means the proxy now understands the application protocol. An L7 HTTP proxy parses incoming HTTP requests, makes routing decisions based on headers, paths, and hosts, manipulates headers, and forwards requests to the appropriate upstream backend. This is the core of what service mesh sidecars like Envoy and Linkerd-proxy do for HTTP traffic.

You will build an HTTP reverse proxy from scratch -- not using `httputil.ReverseProxy`, but constructing the forwarding logic manually. The proxy must parse incoming requests, match them against a routing table, apply header manipulation rules (add, remove, rewrite), strip hop-by-hop headers, forward the request to the selected upstream, and stream the response back to the client. It must handle chunked transfer encoding, preserve request bodies for retries, and correctly manage the `Host` header, `X-Forwarded-For`, and other proxy-specific headers.

The challenge lies in getting the HTTP semantics exactly right: handling `Connection` header directives, managing `Transfer-Encoding`, supporting HTTP/1.1 keep-alive, and ensuring that request and response bodies are streamed efficiently without buffering entire payloads in memory.

## Requirements

1. Implement an HTTP server that accepts requests and routes them to upstream backends based on configurable rules
2. Support host-based routing (e.g., `api.example.com` routes to backend A) and path-prefix routing (e.g., `/api/v1/` routes to backend B)
3. Implement header manipulation rules: add headers, remove headers, and rewrite header values using configurable rules applied before forwarding
4. Strip hop-by-hop headers (`Connection`, `Keep-Alive`, `Proxy-Authenticate`, `Proxy-Authorization`, `TE`, `Trailer`, `Transfer-Encoding`, `Upgrade`) before forwarding, and process `Connection` header directives to remove nominated headers
5. Set standard proxy headers: `X-Forwarded-For`, `X-Forwarded-Host`, `X-Forwarded-Proto`, and `X-Request-Id` (generated UUID if not present)
6. Stream request and response bodies without buffering the entire payload -- use `io.Copy` or chunked streaming
7. Support HTTP/1.1 keep-alive by correctly managing connection reuse on both client-facing and upstream-facing connections
8. Handle upstream connection failures gracefully, returning 502 Bad Gateway with a structured error body
9. Implement request timeout enforcement per-route, returning 504 Gateway Timeout when the upstream does not respond within the configured duration
10. Log each request with: method, path, upstream selected, response status, and latency
11. Write integration tests using `httptest.Server` as upstream backends that verify routing, header manipulation, and error handling

## Hints

- Build the request forwarding using `http.Client` with a custom `Transport` rather than `httputil.ReverseProxy` -- this gives you full control over header manipulation
- Parse the `Connection` header value to find additional hop-by-hop headers nominated by the sender (e.g., `Connection: close, X-Custom` means `X-Custom` is also hop-by-hop)
- For body streaming, pass `req.Body` directly as the body of the outgoing request -- it implements `io.ReadCloser` and will stream without buffering
- Use `context.WithTimeout` wrapping the request context for per-route timeout enforcement
- Generate `X-Request-Id` using `crypto/rand` for UUID v4 or use a simple counter for testing
- For keep-alive, let the `http.Client` connection pooling handle upstream connection reuse, but be careful to always read and close response bodies

## Success Criteria

1. Host-based and path-based routing correctly directs requests to the appropriate upstream
2. Hop-by-hop headers are stripped and `Connection` header directives are processed correctly
3. `X-Forwarded-For` and other proxy headers are set correctly, appending to existing values when present
4. Request and response bodies are streamed without full buffering (verified with large payloads)
5. Upstream failures return 502 with a structured JSON error body
6. Request timeouts return 504 when the upstream exceeds the configured duration
7. Keep-alive connections are reused for subsequent requests to the same upstream
8. All tests pass with the `-race` flag enabled

## Research Resources

- [HTTP/1.1 RFC 9110 - Connection Management](https://httpwg.org/specs/rfc9110.html#field.connection) -- authoritative spec for hop-by-hop headers and connection management
- [Envoy HTTP connection manager](https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/http/http_connection_management) -- reference architecture for L7 HTTP proxying
- [Go net/http package](https://pkg.go.dev/net/http) -- HTTP client and server implementation details
- [Go httputil.ReverseProxy source](https://cs.opensource.google/go/go/+/refs/tags/go1.22.0:src/net/http/httputil/reverseproxy.go) -- reference implementation to study (but not use)
- [X-Forwarded-For header](https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/X-Forwarded-For) -- proxy header semantics and chaining

## What's Next

Continue to [mTLS Termination](../03-mtls-termination/03-mtls-termination.md) where you will add mutual TLS authentication to secure service-to-service communication through the proxy.

## Summary

- L7 HTTP proxies understand the application protocol and can route based on headers, paths, and hosts
- Hop-by-hop headers must be stripped before forwarding, including headers nominated by the `Connection` header
- Proxy headers like `X-Forwarded-For` enable upstream services to identify the original client
- Body streaming avoids memory exhaustion when proxying large payloads
- Proper error handling returns structured 502/504 responses instead of leaking upstream failures
- Keep-alive connection reuse reduces latency and resource consumption for upstream connections
