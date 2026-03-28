# 27. HTTP/2 Stream Multiplexer

<!--
difficulty: advanced
category: networking-and-protocols
languages: [go]
concepts: [http2, stream-multiplexing, hpack, flow-control, frame-parsing, tcp, binary-protocol]
estimated_time: 12-16 hours
bloom_level: evaluate
prerequisites: [go-basics, tcp-sockets, http-protocol, binary-encoding, goroutines, channels, concurrency-patterns]
-->

## Languages

- Go (1.22+)

## Prerequisites

- TCP socket programming with `net.Listener` and `net.Conn`
- HTTP/1.1 request/response semantics (methods, headers, status codes)
- Binary protocol design (fixed-size headers, big-endian encoding)
- Goroutines, channels, mutexes for concurrent stream handling
- Basic understanding of Huffman coding and table-based compression

## Learning Objectives

- **Implement** HTTP/2 binary framing over a raw TCP connection per RFC 9113
- **Analyze** how stream multiplexing eliminates head-of-line blocking at the HTTP layer
- **Design** a flow control system that manages both per-stream and connection-level windows
- **Implement** HPACK header compression with static table lookups and dynamic table management
- **Evaluate** the trade-offs between stream prioritization strategies and their impact on resource delivery order
- **Build** server push functionality that proactively sends resources the client will need

## The Challenge

HTTP/1.1 suffers from head-of-line blocking: a single TCP connection processes one request-response pair at a time. Browsers work around this by opening 6-8 parallel connections, each consuming memory, CPU, and a slow TCP handshake. HTTP/2 solves this by multiplexing multiple logical streams over a single TCP connection. Each stream carries an independent request-response pair, and frames from different streams interleave freely.

Your task is to build an HTTP/2 server that handles binary frame parsing, stream multiplexing, HPACK header compression, and flow control, all over a raw TCP connection. No `net/http`, no `golang.org/x/net/http2` -- just `net.Listener`, `net.Conn`, and RFC 9113.

HTTP/2 starts with a connection preface: the client sends 24 bytes of magic (`PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n`), followed by a SETTINGS frame. The server responds with its own SETTINGS and acknowledges the client's. From there, every byte on the connection is a 9-byte frame header followed by a payload. Your job is to parse these frames, demultiplex them into streams, compress and decompress headers, enforce flow control windows, and respond with properly framed data.

## Requirements

1. Accept TCP connections and validate the HTTP/2 connection preface (24 magic bytes + client SETTINGS frame)
2. Implement the 9-byte frame parser: length (24 bits), type (8 bits), flags (8 bits), reserved bit, stream ID (31 bits)
3. Handle frame types: DATA (0x0), HEADERS (0x1), PRIORITY (0x2), RST_STREAM (0x3), SETTINGS (0x4), PUSH_PROMISE (0x5), PING (0x6), GOAWAY (0x7), WINDOW_UPDATE (0x8)
4. Implement stream lifecycle: idle -> open -> half-closed (local/remote) -> closed. Reject frames that violate the state machine
5. Implement HPACK header decompression: static table (61 pre-defined entries), dynamic table with configurable max size, integer encoding with prefix bits, string literals with optional Huffman coding
6. Implement HPACK header compression for response headers using indexed fields from the static table and literal fields added to the dynamic table
7. Implement connection-level flow control: track a send window and receive window. Send WINDOW_UPDATE frames when the receive window is consumed. Respect the peer's send window before sending DATA frames
8. Implement per-stream flow control: each stream has independent send and receive windows that operate alongside the connection-level window
9. Implement SETTINGS negotiation: send server settings on connection start, acknowledge client settings, apply settings changes (HEADER_TABLE_SIZE, MAX_CONCURRENT_STREAMS, INITIAL_WINDOW_SIZE, MAX_FRAME_SIZE)
10. Implement server push: send PUSH_PROMISE frames to reserve a stream ID, then send the pushed response on that stream
11. Handle concurrent streams: multiple requests in flight simultaneously on a single connection, processed by independent goroutines
12. Send GOAWAY with the last processed stream ID on shutdown, allowing in-flight streams to complete
13. Respond to PING frames with PONG (PING with ACK flag)

## Hints

<details>
<summary>Hint 1: Frame layout and parsing</summary>

Every HTTP/2 frame has a fixed 9-byte header:

```
+-----------------------------------------------+
|                 Length (24)                     |
+---------------+---------------+---------------+
|   Type (8)    |   Flags (8)   |
+-+-------------+---------------+
|R|                 Stream ID (31)               |
+-+---------------------------------------------+
|                Frame Payload (0...)            |
+-----------------------------------------------+
```

Read 9 bytes, parse them into a struct, then read exactly `Length` more bytes for the payload. The length field does not include the 9-byte header itself.
</details>

<details>
<summary>Hint 2: HPACK integer encoding</summary>

HPACK uses a variable-length integer encoding with a prefix of N bits. If the value fits in N bits, it is stored directly. Otherwise, the N-bit prefix is filled with ones, and the remaining value is encoded in 7-bit groups with continuation bits. For decoding:

```
if value < (1 << N) - 1:
    return value  // fits in prefix
else:
    result = value
    shift = 0
    repeat:
        byte = next()
        result += (byte & 0x7F) << shift
        shift += 7
    until byte & 0x80 == 0
    return result
```

Section 5.1 of RFC 7541 has the full algorithm with examples.
</details>

<details>
<summary>Hint 3: Flow control mechanics</summary>

Both the connection and each stream maintain a send window (how many bytes you can send) and a receive window (how many bytes you can accept). When you receive DATA, subtract from your receive window. When the window gets low, send WINDOW_UPDATE to the peer. When you send DATA, subtract from your send window. Block if the window hits zero. WINDOW_UPDATE from the peer adds to your send window. Initial window size is 65535 bytes. The connection window and stream window are independent -- a DATA frame must fit in both.
</details>

## Acceptance Criteria

- [ ] Server validates the 24-byte connection preface and rejects connections that do not start with it
- [ ] Frame parser correctly handles all frame types with proper length, type, flags, and stream ID extraction
- [ ] HPACK decompresses request headers using the static table and dynamic table
- [ ] HPACK compresses response headers with indexed references where possible
- [ ] Multiple concurrent streams on a single connection are handled independently
- [ ] Connection-level flow control prevents sending DATA beyond the peer's window
- [ ] Per-stream flow control tracks each stream's window independently
- [ ] SETTINGS frames are exchanged on connection start and acknowledged correctly
- [ ] Server push sends PUSH_PROMISE followed by the pushed response on the reserved stream
- [ ] GOAWAY frame is sent on shutdown with the correct last-stream-ID
- [ ] PING frames receive ACK responses with the same 8-byte payload
- [ ] RST_STREAM correctly cancels individual streams without affecting others
- [ ] A standard HTTP/2 client (curl with `--http2-prior-knowledge` or `h2load`) can communicate with the server

## Research Resources

- [RFC 9113: HTTP/2](https://datatracker.ietf.org/doc/html/rfc9113) -- the complete HTTP/2 specification (successor to RFC 7540)
- [RFC 7541: HPACK](https://datatracker.ietf.org/doc/html/rfc7541) -- header compression for HTTP/2, including static table, dynamic table, and Huffman coding
- [HTTP/2 Explained (by Daniel Stenberg)](https://http2-explained.haxx.se/) -- accessible walkthrough of HTTP/2 by the author of curl
- [h2spec](https://github.com/summerwind/h2spec) -- HTTP/2 compliance testing tool that verifies against the RFC
- [h2load](https://nghttp2.org/documentation/h2load-howto.html) -- HTTP/2 benchmarking tool from the nghttp2 project
- [Appendix A of RFC 7541: Static Table](https://datatracker.ietf.org/doc/html/rfc7541#appendix-A) -- the 61 pre-defined header field entries
- [Appendix B of RFC 7541: Huffman Code](https://datatracker.ietf.org/doc/html/rfc7541#appendix-B) -- the Huffman code table for HPACK string encoding
