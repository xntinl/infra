# 5. Connection and Error Handling

<!--
difficulty: insane
concepts: [connection-lifecycle, goaway, error-codes, stream-errors, connection-errors, graceful-shutdown, settings-negotiation, ping-keepalive]
tools: [go]
estimated_time: 2h
bloom_level: create
prerequisites: [44-capstone-http2-implementation/04-server-push, 14-select-and-context]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-04 (frames, HPACK, multiplexing, server push) or equivalent HTTP/2 implementation experience
- Completed Section 14 (select and context) for timeout and cancellation patterns

## Learning Objectives

- **Design** a robust HTTP/2 connection lifecycle manager that handles settings negotiation, error classification, graceful shutdown via GOAWAY, and connection keepalive via PING
- **Create** a comprehensive error handling system that distinguishes stream errors (RST_STREAM) from connection errors (GOAWAY) and applies the correct recovery action for each
- **Evaluate** the difference between stream-level and connection-level errors and how proper error classification prevents a single bad request from tearing down an entire connection

## The Challenge

A production HTTP/2 implementation must handle a constant stream of error conditions: malformed frames, protocol violations, flow control errors, timeout expirations, and graceful shutdowns. HTTP/2 draws a critical distinction between stream errors (which affect only one request) and connection errors (which tear down the entire connection). Getting this classification wrong either crashes too hard (killing all requests because of one bad one) or not hard enough (allowing protocol violations to corrupt connection state).

You will implement the full error handling and connection lifecycle system. This includes SETTINGS negotiation (the initial exchange that configures both sides of the connection), GOAWAY for graceful shutdown (allowing in-flight requests to complete while refusing new ones), RST_STREAM for stream-level error termination, PING for connection keepalive and latency measurement, and proper error code propagation. You must classify every possible error condition into the correct category and apply the right response: stream errors send RST_STREAM and continue the connection, connection errors send GOAWAY and tear down.

The GOAWAY frame is particularly nuanced. When a server wants to shut down gracefully, it sends GOAWAY with the last stream ID it will process. The client must not start new streams with IDs higher than this value but may continue existing streams. The server may process streams it has already received even if their IDs are higher than the GOAWAY value. Both sides must handle the case where GOAWAY arrives while new streams are being created -- a race condition that requires careful synchronization.

## Requirements

1. Implement SETTINGS negotiation: on connection establishment, both sides send a SETTINGS frame with their parameters and must acknowledge the peer's SETTINGS with a SETTINGS ACK frame
2. Implement all standard HTTP/2 settings: `HEADER_TABLE_SIZE`, `ENABLE_PUSH`, `MAX_CONCURRENT_STREAMS`, `INITIAL_WINDOW_SIZE`, `MAX_FRAME_SIZE`, `MAX_HEADER_LIST_SIZE`
3. Define all HTTP/2 error codes as constants: `NO_ERROR (0x0)`, `PROTOCOL_ERROR (0x1)`, `INTERNAL_ERROR (0x2)`, `FLOW_CONTROL_ERROR (0x3)`, `SETTINGS_TIMEOUT (0x4)`, `STREAM_CLOSED (0x5)`, `FRAME_SIZE_ERROR (0x6)`, `REFUSED_STREAM (0x7)`, `CANCEL (0x8)`, `COMPRESSION_ERROR (0x9)`, `CONNECT_ERROR (0xa)`, `ENHANCE_YOUR_CALM (0xb)`, `INADEQUATE_SECURITY (0xc)`, `HTTP_1_1_REQUIRED (0xd)`
4. Implement stream error handling: send RST_STREAM with the appropriate error code and transition the stream to closed state, while leaving all other streams and the connection unaffected
5. Implement connection error handling: send GOAWAY with the last processed stream ID and error code, then close the connection after a short grace period for the GOAWAY frame to be transmitted
6. Classify each error condition to the correct level: frame size violations on a specific stream are stream errors, HPACK decompression failures are connection errors, flow control window overflow is a connection error, frames on idle streams are connection errors
7. Implement GOAWAY for graceful shutdown: send GOAWAY with `NO_ERROR` and the last stream ID, wait for in-flight streams to complete (up to a configurable timeout), then close the connection
8. Implement GOAWAY reception: stop creating new streams with IDs above the last stream ID, allow existing streams to complete, and retry refused requests on a new connection
9. Implement PING keepalive: periodically send PING frames and expect PING ACK responses within a configurable timeout, treating missing ACKs as connection failure
10. Implement SETTINGS timeout enforcement: if the peer does not acknowledge a SETTINGS frame within a configurable timeout, treat it as a connection error of type `SETTINGS_TIMEOUT`
11. Write integration tests that verify: SETTINGS negotiation, stream error isolation, connection error teardown, graceful GOAWAY shutdown, and PING keepalive timeout

## Hints

- For SETTINGS negotiation, send your SETTINGS immediately after the connection preface and start a timer -- if SETTINGS ACK is not received within the timeout, send GOAWAY with `SETTINGS_TIMEOUT`
- For error classification, create a `classifyError(frameType, streamID, error) ErrorLevel` function that returns `StreamError` or `ConnectionError` based on the error type and context
- For GOAWAY, maintain an `atomic.Uint32` holding the last stream ID that will be processed -- all stream creation must check this value before allocating a new stream ID
- For graceful shutdown, send GOAWAY twice: first with `MaxStreamID` (2^31-1) to stop new streams, then with the actual last processed stream ID after a short delay -- this handles the race condition where streams are created between GOAWAY send and receipt
- For PING keepalive, use a ticker goroutine that sends PING frames and tracks outstanding pings -- if the count of outstanding pings exceeds a threshold, consider the connection dead
- Use `context.WithTimeout` for SETTINGS timeout enforcement -- cancel the context when SETTINGS ACK is received
- Store the peer's settings in a struct protected by an `atomic.Pointer` so that settings can be read from any goroutine without locking

## Success Criteria

1. SETTINGS negotiation completes successfully with both sides applying the peer's settings
2. SETTINGS timeout is enforced and triggers a connection error when the peer does not acknowledge
3. Stream errors (RST_STREAM) terminate only the affected stream while other streams continue unaffected
4. Connection errors (GOAWAY) correctly shut down the entire connection with the appropriate error code
5. Error classification correctly categorizes all error conditions as stream-level or connection-level
6. Graceful GOAWAY allows in-flight streams to complete while refusing new streams
7. GOAWAY reception prevents new stream creation and allows existing streams to finish
8. PING keepalive detects dead connections within the configured timeout
9. The two-phase GOAWAY pattern handles the race condition between GOAWAY and new stream creation
10. All tests pass with the `-race` flag enabled

## Research Resources

- [RFC 9113 - Error Handling](https://httpwg.org/specs/rfc9113.html#ErrorHandler) -- authoritative specification for HTTP/2 error codes and error handling
- [RFC 9113 - GOAWAY](https://httpwg.org/specs/rfc9113.html#GOAWAY) -- GOAWAY frame semantics and graceful shutdown
- [RFC 9113 - SETTINGS](https://httpwg.org/specs/rfc9113.html#SETTINGS) -- settings negotiation and acknowledgment
- [RFC 9113 - PING](https://httpwg.org/specs/rfc9113.html#PING) -- PING frame for keepalive and latency measurement
- [Go x/net/http2 error handling](https://cs.opensource.google/go/x/net/+/master:http2/goaway_test.go) -- reference tests for GOAWAY behavior
- [gRPC keepalive](https://grpc.io/docs/guides/keepalive/) -- practical keepalive configuration in a production HTTP/2 implementation

## What's Next

Continue to [Full HTTP/2 Server](../06-full-http2-server/06-full-http2-server.md) where you will integrate all components into a complete HTTP/2 server implementation.

## Summary

- HTTP/2 distinguishes between stream errors (isolated to one request) and connection errors (terminate the entire connection)
- Correct error classification prevents a single bad request from unnecessarily tearing down a connection with many active streams
- GOAWAY enables graceful shutdown by signaling the last stream ID the sender will process
- The two-phase GOAWAY pattern handles the race condition between shutdown signaling and stream creation
- SETTINGS negotiation configures both sides of the connection and must be acknowledged within a timeout
- PING keepalive provides connection liveness detection and round-trip latency measurement
