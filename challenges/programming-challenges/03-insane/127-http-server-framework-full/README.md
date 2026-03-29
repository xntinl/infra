# 127. Complete HTTP Server Framework from Raw TCP

```yaml
difficulty: insane
languages: [go]
time_estimate: 50-80 hours
tags: [http-parsing, tcp, keep-alive, chunked-transfer, radix-router, middleware, websocket, graceful-shutdown, framework-architecture]
bloom_level: [evaluate, create]
```

## Prerequisites

- TCP networking: `net.Conn`, connection lifecycle, read/write deadlines
- HTTP/1.1 protocol: request/response format, headers, content negotiation, persistent connections
- Concurrency: goroutines, channels, `sync` primitives, `context.Context`
- Data structures: radix trees, ring buffers, hash maps
- Encoding: JSON, URL encoding, multipart MIME, base64 (for WebSocket)
- Cryptography basics: SHA-1 for WebSocket handshake

## Learning Objectives

After completing this challenge you will be able to:

- **Create** a complete HTTP/1.1 server from raw TCP sockets with no dependency on `net/http`
- **Implement** the HTTP/1.1 protocol: persistent connections, chunked transfer encoding, and pipelining
- **Design** a framework architecture that separates protocol parsing, routing, middleware, and application logic
- **Evaluate** connection lifecycle management strategies for graceful shutdown under load
- **Implement** the WebSocket upgrade handshake and frame protocol from the RFC specification

## The Challenge

Build a complete HTTP server framework from raw TCP connections. No `net/http`. No `http.Handler`. No `http.ResponseWriter`. You parse HTTP requests from bytes, you build HTTP responses as bytes, you manage connections yourself.

The framework must handle the full HTTP/1.1 lifecycle: persistent connections with keep-alive, chunked transfer encoding for streaming, pipelined requests on a single connection. It routes requests through a radix tree, wraps handlers in a middleware pipeline, provides JSON and form parsing helpers, handles multipart file uploads, upgrades connections to WebSocket, and shuts down gracefully by draining active connections.

This is a framework, not a server. Application developers use your API to build their servers. Your API surface: router registration, middleware composition, request context, response helpers, WebSocket handler registration.

## Requirements

1. **TCP listener**: accept connections, one goroutine per connection, configurable read/write timeouts, max concurrent connections limit

2. **HTTP/1.1 request parser**: parse request line (method, path, version), headers, and body from raw bytes. Support `Content-Length` bodies and `Transfer-Encoding: chunked` bodies. Parse query string parameters and the `Host` header

3. **HTTP/1.1 response writer**: construct responses with status line, headers, and body. Support `Content-Length` responses and `Transfer-Encoding: chunked` for streaming. Buffer headers until first `Write()` call (implicit 200 status)

4. **Keep-alive**: reuse TCP connections for multiple request/response cycles. Honor `Connection: close` from the client. Implement idle timeout (close connections with no activity). Support request pipelining (multiple requests queued on one connection)

5. **Radix tree router**: compressed trie with path parameters (`:id`), catch-all wildcards (`*path`), one tree per HTTP method. Conflict detection at registration time. 405 Method Not Allowed when path matches but method doesn't

6. **Middleware pipeline**: `func(Handler) Handler` composition. Global and per-route middleware. Built-in: logging, panic recovery, request ID, timeout

7. **JSON helpers**: `ctx.JSON(status, value)` serializes to JSON with correct headers. `ctx.Bind(dest)` deserializes request body JSON into a struct with validation

8. **Form parsing**: parse `application/x-www-form-urlencoded` bodies. Parse `multipart/form-data` for file uploads with configurable max memory. Access uploaded files by field name

9. **WebSocket upgrade**: implement the HTTP upgrade handshake (RFC 6455 Sec 4.2.2). Parse and construct WebSocket frames (text, binary, ping, pong, close). Support fragmented messages. Provide a clean API: `ctx.UpgradeWebSocket() -> WSConn`

10. **Request context**: a `Context` object per request carrying: parsed params, query values, request body, response writer, deadline, and a key-value store for middleware data. Must propagate through middleware and handlers

11. **Graceful shutdown**: on signal, stop accepting new connections, wait for active connections to complete (with a timeout), then exit. In-flight requests must not be terminated abruptly

## Hints

1. Build incrementally: TCP listener + HTTP parser -> keep-alive -> router -> middleware -> helpers -> WebSocket -> graceful shutdown. Each layer should be testable in isolation before integrating the next.

2. For the HTTP parser, read from the connection into a buffer. Find `\r\n\r\n` to locate the end of headers. Parse the body based on `Content-Length` or chunked encoding after the headers. Do not read more than necessary from the connection -- the next request's bytes may already be in the buffer (pipelining).

3. The WebSocket handshake requires computing `SHA1(client_key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11")` and base64-encoding the result. After the handshake, the connection switches from HTTP framing to WebSocket framing. Your frame parser must handle the 2-byte header, extended payload lengths (16-bit and 64-bit), masking (client-to-server frames are always masked), and fragmentation (FIN bit).

## Acceptance Criteria

- [ ] Server accepts TCP connections and parses HTTP/1.1 requests without `net/http`
- [ ] Keep-alive: multiple requests on one TCP connection, idle timeout closes stale connections
- [ ] Chunked request body parsing and chunked response streaming both work
- [ ] Radix tree router matches static, parameterized, and wildcard routes
- [ ] 405 Method Not Allowed returned when path exists but method doesn't
- [ ] Middleware pipeline executes in correct onion order
- [ ] `ctx.JSON()` returns properly formatted JSON with `Content-Type: application/json`
- [ ] `ctx.Bind()` deserializes JSON body into struct
- [ ] Form parsing handles URL-encoded and multipart bodies
- [ ] File upload via multipart/form-data stores files accessible by field name
- [ ] WebSocket upgrade handshake completes successfully (verified with a standard WebSocket client)
- [ ] WebSocket text and binary frames are correctly parsed and constructed
- [ ] WebSocket ping/pong handled automatically
- [ ] Graceful shutdown drains active connections within the configured timeout
- [ ] Concurrent stress test: 1000 requests across 100 connections with no races or deadlocks
- [ ] No goroutine leaks after shutdown

## Resources

- [RFC 9112: HTTP/1.1](https://httpwg.org/specs/rfc9112.html) -- the HTTP/1.1 message syntax specification (request line, headers, body, chunked encoding)
- [RFC 9110: HTTP Semantics](https://httpwg.org/specs/rfc9110.html) -- status codes, methods, content negotiation, conditional requests
- [RFC 6455: The WebSocket Protocol](https://tools.ietf.org/html/rfc6455) -- complete WebSocket handshake and framing specification
- [RFC 7578: Multipart Form Data](https://tools.ietf.org/html/rfc7578) -- multipart/form-data encoding for file uploads
- [Gin Framework source](https://github.com/gin-gonic/gin) -- study `context.go`, `tree.go`, and `routergroup.go` for API design inspiration
- [fasthttp](https://github.com/valyala/fasthttp) -- high-performance Go HTTP from raw connections; study the parser and connection reuse patterns
- [Gorilla WebSocket](https://github.com/gorilla/websocket) -- reference WebSocket implementation in Go
