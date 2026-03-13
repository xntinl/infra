# 3. Stream Multiplexing

<!--
difficulty: insane
concepts: [stream-multiplexing, http2-streams, flow-control, stream-states, stream-priority, window-updates, connection-flow-control, head-of-line-blocking]
tools: [go]
estimated_time: 4h
bloom_level: create
prerequisites: [44-capstone-http2-implementation/02-hpack-header-compression, 13-goroutines-and-channels, 15-sync-primitives]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-02 (frame parsing, HPACK) or equivalent HTTP/2 framing and header compression experience
- Completed Sections 13-15 (goroutines, channels, sync primitives) or equivalent concurrency experience

## Learning Objectives

- **Design** an HTTP/2 stream multiplexer that manages concurrent streams over a single TCP connection with per-stream and connection-level flow control
- **Create** a stream state machine implementing the full HTTP/2 stream lifecycle (idle, open, half-closed, closed) with correct transition validation
- **Evaluate** how HTTP/2's flow control prevents fast senders from overwhelming slow receivers and how stream multiplexing eliminates HTTP/1.1's head-of-line blocking at the application layer

## The Challenge

HTTP/2's most important feature is stream multiplexing: multiple independent request-response exchanges happen concurrently over a single TCP connection. Each stream has a unique identifier, its own flow control window, and its own lifecycle state machine. The multiplexer must interleave frames from different streams on the shared connection, ensure that flow control prevents any single stream from monopolizing bandwidth, and correctly manage stream state transitions as requests are sent, responses are received, and streams are closed or reset.

You will build the stream multiplexer, which is the heart of HTTP/2. The multiplexer maintains a table of active streams, each with its own state machine (idle -> open -> half-closed local/remote -> closed). It implements flow control at two levels: per-stream flow control windows that limit how much data can be sent on each stream, and a connection-level flow control window that limits total data across all streams. When a flow control window is exhausted, the sender must stop sending DATA frames on that stream until the receiver sends a WINDOW_UPDATE frame.

The hardest part is the interaction between the connection's single read/write goroutine pair and the multiple goroutines that represent concurrent streams. Frames arriving on the connection must be demultiplexed to the correct stream. Frames being sent by different streams must be serialized onto the single connection. Flow control decisions must be made without blocking the connection's read loop (which would prevent WINDOW_UPDATE frames from being received). And stream state transitions must be atomic and consistent.

## Requirements

1. Implement a `Stream` struct with: stream ID, state (idle, open, half-closed local, half-closed remote, closed), send flow control window, receive flow control window, and a channel for receiving frames demultiplexed from the connection
2. Implement the HTTP/2 stream state machine with all valid transitions as defined in RFC 9113 Section 5.1, rejecting invalid transitions with a `STREAM_CLOSED` or `PROTOCOL_ERROR` as appropriate
3. Implement stream creation: client-initiated streams use odd IDs, server-initiated streams use even IDs, and stream IDs must monotonically increase
4. Implement the connection multiplexer that manages a table of active streams, demultiplexes incoming frames to the correct stream's receive channel, and serializes outgoing frames from all streams onto the connection
5. Implement per-stream flow control: track send and receive windows, block DATA frame sends when the send window is zero, and send WINDOW_UPDATE frames when the receive window is consumed
6. Implement connection-level flow control: a separate flow control window that applies to the total DATA across all streams, in addition to per-stream windows
7. Implement WINDOW_UPDATE processing: when a WINDOW_UPDATE frame is received, increase the corresponding flow control window and unblock any streams waiting to send
8. Implement `SETTINGS_MAX_CONCURRENT_STREAMS` enforcement: reject new stream creation when the limit is reached, returning `REFUSED_STREAM`
9. Implement stream reset via RST_STREAM: immediately transition the stream to closed state and release its resources, notifying any waiting goroutines
10. Implement a frame scheduler that determines which stream's frames to send next when multiple streams have data ready, using a simple round-robin or weighted fair queuing approach
11. Implement `SETTINGS_INITIAL_WINDOW_SIZE` changes: when this setting is updated, adjust all existing streams' flow control windows by the delta
12. Write integration tests that verify concurrent streams, flow control blocking and unblocking, stream state transitions, and maximum concurrent streams enforcement

## Hints

- Use a dedicated write goroutine that reads from a shared "outgoing frames" channel and writes to the connection -- all streams send their frames to this channel, and the write goroutine serializes them
- Use a dedicated read goroutine that reads frames from the connection and demultiplexes them by stream ID to the appropriate stream's receive channel
- For flow control blocking, use a condition variable (`sync.Cond`) or a channel that is signaled when a WINDOW_UPDATE increases the window: the sending goroutine waits on the condition, and the read goroutine signals it when a WINDOW_UPDATE arrives
- Protect the stream table with a `sync.RWMutex` -- stream lookup happens on every frame read and must be fast
- For `SETTINGS_INITIAL_WINDOW_SIZE` changes, compute the delta between old and new values and add it to every open stream's send window -- be careful of integer overflow causing the window to exceed 2^31-1 (a flow control error)
- For the frame scheduler, maintain a ready queue of streams that have frames to send and cycle through them, sending one frame per stream per round
- When a stream transitions to closed, remove it from the stream table and close its receive channel to unblock any waiting readers

## Success Criteria

1. Multiple streams correctly operate concurrently over a single connection
2. Stream state transitions follow RFC 9113 exactly, rejecting invalid transitions
3. Per-stream flow control prevents data transmission when the window is exhausted
4. Connection-level flow control limits total data across all streams
5. WINDOW_UPDATE frames correctly increase flow control windows and unblock waiting senders
6. Maximum concurrent streams is enforced, refusing new streams when the limit is reached
7. RST_STREAM immediately terminates a stream and releases its resources
8. The frame scheduler fairly distributes connection bandwidth among ready streams
9. SETTINGS_INITIAL_WINDOW_SIZE changes correctly adjust all existing stream windows
10. All tests pass with the `-race` flag enabled

## Research Resources

- [RFC 9113 - HTTP/2 Streams and Multiplexing](https://httpwg.org/specs/rfc9113.html#StreamsLayer) -- authoritative specification for stream lifecycle and multiplexing
- [RFC 9113 - Flow Control](https://httpwg.org/specs/rfc9113.html#FlowControl) -- flow control semantics and WINDOW_UPDATE processing
- [RFC 9113 - Stream States](https://httpwg.org/specs/rfc9113.html#StreamStates) -- the stream state machine diagram and transition rules
- [Go sync.Cond](https://pkg.go.dev/sync#Cond) -- condition variable for flow control blocking and unblocking
- [Go x/net/http2 transport source](https://cs.opensource.google/go/x/net/+/master:http2/transport.go) -- reference multiplexer implementation to study (but not use)
- [HTTP/2 flow control (Cloudflare)](https://blog.cloudflare.com/http-2-for-web-developers/) -- practical explanation of flow control behavior

## What's Next

Continue to [Server Push](../04-server-push/04-server-push.md) where you will implement HTTP/2 server push to proactively send resources to clients.

## Summary

- Stream multiplexing enables multiple concurrent request-response exchanges over a single TCP connection
- The stream state machine enforces correct lifecycle transitions: idle, open, half-closed, and closed
- Per-stream and connection-level flow control prevent fast senders from overwhelming slow receivers
- WINDOW_UPDATE frames are the mechanism for receivers to grant additional flow control capacity
- A dedicated write goroutine serializes frames from multiple streams onto the shared connection
- Frame scheduling determines bandwidth allocation among competing streams
- SETTINGS changes to initial window size require retroactive adjustment of all existing stream windows
