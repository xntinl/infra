# 6. Full HTTP/2 Server

<!--
difficulty: insane
concepts: [http2-server, protocol-integration, request-handling, response-streaming, tls-alpn, h2c-upgrade, content-negotiation, http-handler, connection-management]
tools: [go]
estimated_time: 6h
bloom_level: create
prerequisites: [44-capstone-http2-implementation/05-connection-error-handling]
-->

## Prerequisites

- Go 1.22+ installed
- Completed all exercises 01-05 in this section or equivalent experience building each HTTP/2 component independently
- Understanding of how production HTTP/2 servers (Go's net/http, nginx, Apache) handle the full request-response lifecycle

## Learning Objectives

- **Design** a complete HTTP/2 server by integrating frame parsing, HPACK compression, stream multiplexing, flow control, server push, and error handling into a unified, production-grade server implementation
- **Create** an HTTP request handler interface compatible with Go's `http.Handler` pattern that processes HTTP/2 requests using the custom protocol implementation
- **Evaluate** the end-to-end correctness and performance of the integrated HTTP/2 server against real HTTP/2 clients (curl, browsers) and compare latency and throughput with Go's standard library HTTP/2 implementation

## The Challenge

You have built every layer of the HTTP/2 protocol stack. Now you must assemble them into a server that real HTTP/2 clients can connect to, send requests, and receive responses. This is the capstone integration challenge -- the difficulty is not in any single component, but in making five independently developed protocol layers work together correctly under concurrent load from real-world clients.

A complete HTTP/2 server must: accept TLS connections with ALPN negotiation to select the `h2` protocol, validate the connection preface, negotiate settings, create streams for incoming requests, decompress headers using HPACK, deliver request bodies via flow-controlled DATA frames, pass requests to application handlers, stream response headers and bodies back through the protocol stack, manage flow control windows, handle errors at both stream and connection levels, support server push, and gracefully shut down when requested.

The integration challenges are significant. The HPACK codec must be synchronized with the connection's frame processing -- a HEADERS frame arrives in the read loop, must be decoded by the HPACK decoder (which updates the dynamic table), and the decoded headers must be delivered to the stream's goroutine. Flow control must be managed across all active streams without deadlocking -- a stream blocked on sending DATA must not prevent the connection from processing WINDOW_UPDATE frames. Error handling must correctly propagate from any layer to the appropriate error response. And all of this must perform well enough to serve real traffic.

## Requirements

1. Implement TLS listener setup with ALPN negotiation: configure `tls.Config` with `NextProtos: ["h2", "http/1.1"]` and dispatch to HTTP/2 handling when the client selects `h2`
2. Implement h2c (HTTP/2 over cleartext) support via the HTTP/1.1 upgrade mechanism: detect the `Upgrade: h2c` header, respond with 101 Switching Protocols, and transition the connection to HTTP/2
3. Implement the connection setup sequence: validate the client's connection preface, send the server's SETTINGS frame, and process the client's SETTINGS acknowledgment
4. Implement request processing: when a HEADERS frame arrives with END_HEADERS, decode the headers using HPACK, construct an `http.Request` object, and dispatch it to the registered `http.Handler`
5. Handle CONTINUATION frames: if a HEADERS frame does not have END_HEADERS set, accumulate subsequent CONTINUATION frames until END_HEADERS is received, then decode the complete header block
6. Implement request body delivery: when DATA frames arrive for a stream, make them available to the handler via the `http.Request.Body` reader, correctly handling flow control by sending WINDOW_UPDATE frames as the handler consumes data
7. Implement response writing: the handler writes response headers and body via an `http.ResponseWriter` implementation that encodes headers with HPACK, sends HEADERS frames, and sends response body as DATA frames with flow control
8. Implement response streaming: large response bodies must be split into multiple DATA frames respecting the maximum frame size and flow control windows, with DATA frames interleaved with frames from other streams
9. Implement trailer support: if the handler sets trailers, send them as a HEADERS frame with END_STREAM after the final DATA frame
10. Integrate server push: provide a `PushOptions` interface on the `http.ResponseWriter` that allows handlers to trigger push promises for related resources
11. Implement connection management: track all active connections, enforce a maximum connections limit, and provide a `Shutdown(ctx context.Context)` method that gracefully drains all connections using GOAWAY
12. Implement a request router that maps paths to handlers, similar to `http.ServeMux`, and integrate it with the HTTP/2 server
13. Write end-to-end tests using Go's `crypto/tls` client (configured for HTTP/2) that verify: basic request-response, concurrent streams, large request and response bodies, server push, trailer headers, graceful shutdown, and error handling
14. Write a benchmark comparing your implementation's throughput and latency against Go's standard `net/http` HTTP/2 server for simple request-response workloads

## Hints

- Use Go's `http.Handler` and `http.ResponseWriter` interfaces so that existing HTTP handlers work unmodified with your HTTP/2 server
- For the `ResponseWriter`, buffer the first `WriteHeader` call and send the HEADERS frame, then subsequent `Write` calls send DATA frames
- For CONTINUATION frame handling, accumulate the header block fragments in a `bytes.Buffer` and pass the complete buffer to the HPACK decoder only when END_HEADERS is received -- no other frame type may be interleaved between HEADERS and CONTINUATION
- For request body flow control, implement `Request.Body` as a pipe where the connection's read goroutine writes DATA frame payloads and the handler goroutine reads -- send WINDOW_UPDATE from the read goroutine when the handler has consumed data
- For response body flow control, implement a blocking write in the `ResponseWriter` that waits for available window before sending each DATA frame -- use the flow control channel/condition variable from exercise 03
- For TLS ALPN, set `tls.Config.NextProtos = []string{"h2", "http/1.1"}` and check `tls.Conn.ConnectionState().NegotiatedProtocol` after the handshake
- For h2c upgrade, parse the `HTTP2-Settings` header from the upgrade request (base64url-encoded SETTINGS payload) and apply it as the client's initial settings
- Generate a test TLS certificate using `crypto/x509` and `crypto/ecdsa` in test setup to avoid needing external certificate files

## Success Criteria

1. Real HTTP/2 clients (curl with `--http2`, Go's `net/http` client) can connect, send requests, and receive correct responses
2. TLS ALPN correctly negotiates the `h2` protocol
3. Multiple concurrent streams are handled correctly, with responses interleaved on the connection
4. Large request and response bodies are streamed correctly with flow control
5. CONTINUATION frames are correctly accumulated and decoded
6. Server push delivers pushed resources that clients can receive
7. Trailer headers are correctly sent after the response body
8. Graceful shutdown via GOAWAY allows in-flight requests to complete
9. Stream errors do not affect other streams on the same connection
10. Connection errors are correctly classified and trigger GOAWAY with the appropriate error code
11. The benchmark demonstrates that the custom implementation is within 3x of Go's standard library HTTP/2 performance
12. All tests pass with the `-race` flag enabled
13. No goroutine leaks after connection setup, sustained load, and teardown

## Research Resources

- [RFC 9113 - HTTP/2](https://httpwg.org/specs/rfc9113.html) -- the complete HTTP/2 specification
- [RFC 7301 - TLS ALPN](https://www.rfc-editor.org/rfc/rfc7301) -- Application-Layer Protocol Negotiation for TLS
- [RFC 9113 - Starting HTTP/2](https://httpwg.org/specs/rfc9113.html#starting) -- connection preface, h2 and h2c startup sequences
- [Go net/http package](https://pkg.go.dev/net/http) -- Handler and ResponseWriter interfaces
- [Go crypto/tls package](https://pkg.go.dev/crypto/tls) -- TLS configuration with ALPN support
- [Go x/net/http2 source](https://cs.opensource.google/go/x/net/+/master:http2/server.go) -- reference implementation to study (but not use)
- [h2spec conformance testing tool](https://github.com/summerwind/h2spec) -- automated HTTP/2 compliance testing tool

## Summary

- A complete HTTP/2 server integrates five protocol layers: framing, HPACK, multiplexing, flow control, and error handling
- TLS ALPN negotiation enables seamless protocol selection between HTTP/1.1 and HTTP/2
- CONTINUATION frames require special handling: no other frame types may appear between HEADERS and the final CONTINUATION
- Request and response body streaming through flow control requires careful coordination between the connection's I/O goroutines and per-stream handler goroutines
- Compatibility with Go's `http.Handler` interface enables existing HTTP handlers to work without modification
- Integration testing against real HTTP/2 clients validates conformance beyond unit-level correctness
- Performance benchmarking against the standard library establishes the overhead cost of the custom implementation
