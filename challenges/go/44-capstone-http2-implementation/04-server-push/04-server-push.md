# 4. Server Push

<!--
difficulty: insane
concepts: [server-push, push-promise, promised-streams, push-cancellation, cache-aware-push, push-prioritization, settings-enable-push]
tools: [go]
estimated_time: 2h
bloom_level: create
prerequisites: [44-capstone-http2-implementation/03-stream-multiplexing, 17-http-programming]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-03 (frames, HPACK, stream multiplexing) or equivalent HTTP/2 implementation experience
- Completed Section 17 (HTTP programming) or equivalent HTTP semantics experience

## Learning Objectives

- **Design** a server push mechanism that proactively sends resources to clients using PUSH_PROMISE frames and server-initiated streams
- **Create** a push policy engine that determines which resources to push based on the requested URL, with duplicate push detection and client cancellation support
- **Evaluate** the practical trade-offs of server push including bandwidth waste, cache invalidation challenges, and why browsers have largely deprecated its use

## The Challenge

HTTP/2 server push allows the server to send resources to the client before the client requests them. When a client requests an HTML page, the server can push the associated CSS and JavaScript files immediately, eliminating the round trip the client would normally need to discover and request those resources. The server sends a PUSH_PROMISE frame on the original stream that contains the request headers the client would have sent, then sends the pushed response on a new server-initiated stream.

You will implement the full server push lifecycle. The server must create PUSH_PROMISE frames containing synthesized request headers (method, path, scheme, authority), associate them with the originating client request stream, open a new server-initiated stream (even-numbered ID) for each pushed resource, and send the response on that stream. The client side must process PUSH_PROMISE frames, match incoming pushed responses to their promises, and provide a mechanism to cancel unwanted pushes via RST_STREAM.

The implementation must handle the edge cases that make push tricky: the client may have disabled push via SETTINGS, the server must not push resources that would create circular dependencies, pushed streams consume the client's concurrent stream limit, and the server must not push after sending END_STREAM on the associated stream. You will also implement a push policy engine that uses a configurable mapping from request paths to pushable resources, with deduplication to avoid pushing the same resource twice on the same connection.

## Requirements

1. Implement PUSH_PROMISE frame sending on the server side: create a frame containing the promised stream ID and the compressed request headers for the pushed resource
2. Implement server-initiated stream creation for pushed responses using even-numbered stream IDs that monotonically increase
3. Implement the PUSH_PROMISE -> response sequence: send PUSH_PROMISE on the originating stream, then send HEADERS and DATA frames on the promised stream
4. Implement client-side PUSH_PROMISE processing: receive the promise, validate the promised request headers, and prepare to receive the pushed response on the promised stream
5. Implement push cancellation: the client can send RST_STREAM with `CANCEL` error code on a promised stream ID to indicate it does not want the pushed resource
6. Respect `SETTINGS_ENABLE_PUSH`: if the client has sent `SETTINGS_ENABLE_PUSH=0`, the server must not send any PUSH_PROMISE frames -- doing so is a connection error
7. Enforce that PUSH_PROMISE is only sent on streams that are open or half-closed (remote) from the client's perspective, and never after END_STREAM has been sent on the associated stream
8. Implement a push policy engine: a configurable map from request path patterns to lists of pushable resource paths, consulted on each incoming request
9. Implement push deduplication: track which resources have already been pushed on the current connection and do not push them again
10. Ensure pushed streams count toward `SETTINGS_MAX_CONCURRENT_STREAMS` and reject pushes when the limit would be exceeded
11. Write integration tests that verify push promise delivery, pushed response reception, push cancellation via RST_STREAM, and SETTINGS_ENABLE_PUSH enforcement

## Hints

- PUSH_PROMISE frames must be sent on the originating stream (the client's request stream) but they create a new server-initiated stream with an even ID
- The server must reserve (allocate) the promised stream ID before sending PUSH_PROMISE to ensure no race condition with other stream creations
- For the push policy engine, use a `map[string][]string` where the key is a request path pattern (e.g., `/index.html`) and the value is a list of resource paths to push (e.g., `/style.css`, `/app.js`)
- For push deduplication, maintain a `map[string]bool` of already-pushed resource paths on the connection state
- The client should store received PUSH_PROMISE frames in a map of `promisedStreamID -> promisedRequest` and match them when frames arrive on the promised stream
- For push cancellation, the client sends RST_STREAM immediately after receiving PUSH_PROMISE if it does not want the resource (e.g., it already has it cached)
- Remember that pushed requests must be safe and cacheable -- only GET and HEAD methods are valid for push promises

## Success Criteria

1. The server correctly sends PUSH_PROMISE frames and delivers pushed responses on server-initiated streams
2. The client correctly receives and processes PUSH_PROMISE frames and matches them to pushed responses
3. Push cancellation via RST_STREAM stops the server from sending further data on the cancelled stream
4. SETTINGS_ENABLE_PUSH=0 is respected and no PUSH_PROMISE frames are sent when push is disabled
5. Push deduplication prevents the same resource from being pushed twice on the same connection
6. Pushed streams correctly count toward the concurrent stream limit
7. PUSH_PROMISE is rejected if sent after END_STREAM on the associated stream
8. All tests pass with the `-race` flag enabled

## Research Resources

- [RFC 9113 - Server Push](https://httpwg.org/specs/rfc9113.html#PushResources) -- authoritative specification for server push
- [RFC 9113 - PUSH_PROMISE](https://httpwg.org/specs/rfc9113.html#PUSH_PROMISE) -- PUSH_PROMISE frame format and semantics
- [HTTP/2 Server Push (web.dev)](https://web.dev/articles/performance-http2) -- practical overview of server push use cases and limitations
- [Chrome removing HTTP/2 push](https://developer.chrome.com/blog/removing-push/) -- why browsers deprecated server push and the lessons learned
- [103 Early Hints](https://developer.chrome.com/blog/early-hints/) -- the alternative to server push that has gained adoption

## What's Next

Continue to [Connection and Error Handling](../05-connection-error-handling/05-connection-error-handling.md) where you will implement robust connection lifecycle management and HTTP/2 error handling.

## Summary

- Server push allows proactive resource delivery via PUSH_PROMISE frames and server-initiated streams
- Push promises are sent on the originating client stream but create new even-numbered server streams
- SETTINGS_ENABLE_PUSH gives clients control over whether the server may push resources
- Push deduplication prevents wasted bandwidth from redundant pushes
- Push cancellation via RST_STREAM allows clients to abort unwanted pushes early
- Server push has practical limitations that led to its deprecation in browsers, making it a valuable study in protocol design trade-offs
