# 28. Build an HTTP/2 Implementation

**Difficulty**: Insane

## The Challenge

Build an HTTP/2 framing layer, HPACK header compression, and stream multiplexer from scratch. HTTP/2 is a binary protocol that multiplexes multiple request-response pairs over a single TCP connection, with flow control, prioritization, and header compression. Every modern web server and browser speaks HTTP/2, yet few developers understand what happens below the abstraction.

You will implement the full frame parser/serializer, the HPACK codec (static table, dynamic table, Huffman encoding), and the stream state machine. Your implementation must handle concurrent streams, respect flow control windows, and correctly manage the connection and stream lifecycle. By the end, you will have a working HTTP/2 server that can serve requests from real browsers and pass the h2spec conformance test suite.

This exercise forces you to work with binary protocols, state machines, concurrent streams, and backpressure — all in async Rust. You will encounter every challenge of network protocol implementation: framing, buffering, flow control, error propagation, and graceful shutdown.

## Acceptance Criteria

### Frame Layer
- [ ] Parse and serialize all 10 HTTP/2 frame types: DATA, HEADERS, PRIORITY, RST_STREAM, SETTINGS, PUSH_PROMISE, PING, GOAWAY, WINDOW_UPDATE, CONTINUATION
- [ ] Enforce maximum frame size (default 16,384 bytes, configurable via SETTINGS)
- [ ] Handle frame flags correctly (END_STREAM, END_HEADERS, PADDED, PRIORITY)
- [ ] Connection preface: send/receive `PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n` + SETTINGS frame
- [ ] Reject invalid frames with appropriate error codes (PROTOCOL_ERROR, FRAME_SIZE_ERROR, etc.)

### HPACK Header Compression (RFC 7541)
- [ ] Implement static table (61 pre-defined header entries)
- [ ] Implement dynamic table with configurable size limit (SETTINGS_HEADER_TABLE_SIZE)
- [ ] Encode/decode indexed header fields (single byte for common headers)
- [ ] Encode/decode literal header fields (with/without indexing, never indexed)
- [ ] Implement Huffman encoding and decoding for header values
- [ ] Dynamic table eviction when size limit is exceeded
- [ ] Handle SETTINGS_HEADER_TABLE_SIZE changes (table size update signal)

### Stream Multiplexing
- [ ] Support concurrent streams (SETTINGS_MAX_CONCURRENT_STREAMS)
- [ ] Implement stream state machine: idle → open → half-closed (local/remote) → closed
- [ ] Odd-numbered streams for client-initiated, even for server push
- [ ] Stream ID monotonically increasing — reject out-of-order IDs
- [ ] Handle RST_STREAM for individual stream cancellation
- [ ] GOAWAY for graceful connection shutdown with last-stream-id

### Flow Control
- [ ] Connection-level flow control window (default 65,535 bytes)
- [ ] Per-stream flow control windows
- [ ] Send WINDOW_UPDATE frames to increase receive window
- [ ] Block DATA frame sending when window is exhausted
- [ ] Handle SETTINGS_INITIAL_WINDOW_SIZE changes to all open streams

### Server Implementation
- [ ] Accept HTTP/2 connections (h2c direct or ALPN negotiation over TLS)
- [ ] Dispatch requests to handler functions based on path
- [ ] Support request/response bodies via DATA frames
- [ ] Support trailers (HEADERS frame after DATA with END_STREAM)
- [ ] Serve static files with proper content-type and content-length

### Correctness and Performance
- [ ] Pass h2spec conformance tests (at least the generic and HPACK sections)
- [ ] Handle 100 concurrent streams without deadlock
- [ ] Benchmark: serve 10,000 requests/second for small responses on localhost
- [ ] No busy-spinning — use async I/O throughout
- [ ] Graceful handling of client disconnects and protocol errors

## Starting Points

- **h2 crate** (`hyperium/h2`): The production Rust HTTP/2 implementation used by hyper/tonic. Study `src/frame/` for frame parsing, `src/hpack/` for HPACK implementation, `src/proto/streams/` for the stream state machine and flow control. This is your primary reference.

- **RFC 7540** (HTTP/2): The protocol specification. Section 4 (frames), Section 5 (streams and multiplexing), Section 6 (frame definitions), and Section 6.9 (flow control) are essential.

- **RFC 7541** (HPACK): Header compression specification. Appendix A has the static table, Appendix B has the Huffman code table. Study the integer encoding scheme (Section 5.1) — it uses a prefix-based variable-length encoding.

- **h2spec** (`summerwind/h2spec`): Conformance testing tool. Run it against your server to find protocol violations. It tests hundreds of edge cases from the RFC.

- **nghttp2**: Reference C implementation. Study `lib/nghttp2_hd.c` for HPACK and `lib/nghttp2_session.c` for the session/stream management.

## Hints

1. Start with the frame layer. Define a `Frame` enum with variants for each frame type. Implement `Frame::parse(bytes: &[u8]) -> Result<(Frame, usize)>` and `Frame::serialize(&self, buf: &mut Vec<u8>)`. The 9-byte frame header (length, type, flags, stream_id) is always the same.

2. For HPACK, implement the integer encoding first (Section 5.1 of RFC 7541) — it is used everywhere. The prefix size varies by context (7-bit for indexed, 6-bit for incremental indexing, etc.). Then build the static table, dynamic table, Huffman codec, and finally the encoder/decoder.

3. The stream state machine is best modeled as a Rust enum with methods for each transition. Invalid transitions should return STREAM_CLOSED or PROTOCOL_ERROR. Keep a `HashMap<u32, StreamState>` for active streams.

4. Flow control is the trickiest part. Maintain two windows per stream (send and receive) plus two for the connection. When sending DATA, check both stream and connection windows. When receiving WINDOW_UPDATE, wake any blocked senders. Use `tokio::sync::Notify` or similar for waking.

5. CONTINUATION frames complicate the frame layer — a HEADERS frame without END_HEADERS must be followed by CONTINUATION frames on the same stream, with no interleaved frames from other streams. Buffer the fragments and decode headers only after END_HEADERS.

6. Use `bytes::Bytes` and `bytes::BytesMut` for zero-copy buffer management. The frame parser should work with `BytesMut` and advance the read cursor.

7. For the async architecture, use a single task per connection that reads frames from the socket and dispatches to per-stream handlers via channels. This avoids complex locking on the connection state.

8. Implement SETTINGS acknowledgment early — the connection preface exchange must complete before any requests can be processed. Send your SETTINGS, receive and ACK the client's SETTINGS, receive the client's ACK of your SETTINGS.

9. Test with `curl --http2` and `nghttp` for real client interop. Use `RUST_LOG=trace` with tracing to log every frame sent and received during debugging.

10. For Huffman coding, the RFC provides a complete code table (Appendix B). Build a decode table (or trie) at compile time using `include!` or `const` evaluation. Encoding is simpler — just look up each byte in the code table and pack bits.

## Resources

- RFC 7540: https://www.rfc-editor.org/rfc/rfc7540
- RFC 7541 (HPACK): https://www.rfc-editor.org/rfc/rfc7541
- h2 crate source: https://github.com/hyperium/h2
- h2spec: https://github.com/summerwind/h2spec
