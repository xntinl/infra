# 26. WebSocket Server Protocol Implementation

<!--
difficulty: advanced
category: networking-and-protocols
languages: [go]
concepts: [websocket, rfc-6455, tcp, http-upgrade, frame-parsing, masking, fragmentation, compression, concurrency]
estimated_time: 10-14 hours
bloom_level: evaluate
prerequisites: [go-basics, tcp-sockets, http-protocol, binary-encoding, goroutines, channels, crypto-sha1-base64]
-->

## Languages

- Go (1.22+)

## Prerequisites

- TCP socket programming with `net.Listener` and `net.Conn`
- HTTP/1.1 request/response format (method, headers, status lines)
- Binary data manipulation (big-endian encoding, bitwise operations, byte slicing)
- Goroutines, channels, and `sync.Mutex` for concurrent connection handling
- SHA-1 hashing and Base64 encoding (`crypto/sha1`, `encoding/base64`)
- Basic understanding of DEFLATE compression (`compress/flate`)

## Learning Objectives

- **Implement** the WebSocket opening handshake by upgrading an HTTP/1.1 connection per RFC 6455 Section 4
- **Analyze** the WebSocket frame format and correctly parse opcodes, payload lengths, masking keys, and extension bits
- **Design** a fragmentation system that reassembles multi-frame messages transparently to application code
- **Evaluate** the security implications of frame masking and why servers must reject unmasked client frames
- **Implement** per-message compression (permessage-deflate, RFC 7692) with shared and per-message compression contexts
- **Build** a concurrent server that handles multiple clients simultaneously with broadcast support

## The Challenge

WebSocket is the protocol that gives the web bidirectional, full-duplex communication over a single TCP connection. Every chat application, live dashboard, collaborative editor, and real-time game you use in a browser depends on it. Yet most developers interact with WebSocket only through libraries that hide the protocol machinery.

Your task is to build a WebSocket server from raw TCP sockets in Go. No `gorilla/websocket`, no `nhooyr.io/websocket`, no `net/http` -- just `net.Listener`, `net.Conn`, and the RFC. You will implement the HTTP upgrade handshake, parse and construct binary frames, handle control frames (ping, pong, close), reassemble fragmented messages, apply per-message compression, and serve multiple concurrent clients with broadcast capability.

The WebSocket frame format is compact but precise. A single bit in the wrong position means a broken connection. Payload lengths have three encoding modes depending on size. Masking uses a 4-byte rotating XOR that clients must apply and servers must undo. Getting this right requires reading the RFC carefully and testing against real browsers.

The protocol has subtle asymmetries that trip up first-time implementors. Clients must mask all frames; servers must not mask any. Control frames can appear between fragments of a data message and must be handled immediately. The close handshake requires both sides to exchange close frames before dropping the TCP connection. Per-message compression adds another layer: the DEFLATE stream must have its sync marker stripped before sending and restored before decompressing.

## Requirements

1. Listen on a TCP port and parse incoming HTTP/1.1 upgrade requests manually (read the request line, headers, and blank line terminator from the raw TCP stream)
2. Validate the upgrade request: check `Upgrade: websocket`, `Connection: Upgrade`, `Sec-WebSocket-Version: 13`, and the presence of `Sec-WebSocket-Key`
3. Compute the `Sec-WebSocket-Accept` response header by concatenating the client key with the magic GUID `258EAFA5-E914-47DA-95CA-5AB0DC65E740`, SHA-1 hashing, and Base64 encoding
4. Send back a valid `101 Switching Protocols` response with the correct headers
5. Implement frame parsing per RFC 6455 Section 5.2: FIN bit, RSV bits, opcode (4 bits), MASK bit, payload length (7 bits, 7+16 bits, 7+64 bits), masking key (4 bytes), and payload data
6. Implement frame construction for server-to-client messages (servers must not mask outbound frames)
7. Handle all standard opcodes: text (0x1), binary (0x2), close (0x8), ping (0x9), pong (0xA)
8. Implement masking/unmasking: XOR each payload byte with `masking_key[i % 4]`
9. Implement fragmented message reassembly: a message can span multiple frames (first frame has FIN=0, continuation frames have opcode=0, final frame has FIN=1). Control frames may be interleaved between fragments
10. Implement the close handshake: when receiving a close frame, respond with a close frame echoing the status code, then close the TCP connection
11. Implement per-message compression using the `permessage-deflate` extension (RFC 7692): negotiate via `Sec-WebSocket-Extensions` header, compress outbound messages with DEFLATE, decompress inbound messages, handle the trailing `0x00 0x00 0xFF 0xFF` removal
12. Handle concurrent clients: each connection runs in its own goroutine, with a connection registry that supports broadcast (send a message to all connected clients)
13. Implement a heartbeat mechanism: send periodic ping frames and disconnect clients that do not respond with pong within a timeout

## Hints

<details>
<summary>Hint 1: Parsing the HTTP upgrade request</summary>

Read from the TCP connection byte-by-byte or use `bufio.Reader` to find the end of the HTTP headers (the `\r\n\r\n` sequence). Parse the request line to extract the method and path, then parse headers into a map. The upgrade request looks like:

```
GET /chat HTTP/1.1
Host: server.example.com
Upgrade: websocket
Connection: Upgrade
Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==
Sec-WebSocket-Version: 13
```

Do not use `net/http` -- parse this yourself from the raw bytes.
</details>

<details>
<summary>Hint 2: Frame length encoding</summary>

The payload length field has three modes:
- If the 7-bit value is 0-125, that is the length
- If the 7-bit value is 126, the next 2 bytes (big-endian uint16) are the length
- If the 7-bit value is 127, the next 8 bytes (big-endian uint64) are the length

Always read the first two bytes of a frame first, then decide how many more bytes to read for the extended length before reading the masking key and payload.
</details>

<details>
<summary>Hint 3: Fragmentation with interleaved control frames</summary>

Control frames (ping, pong, close) can arrive between fragments of a data message. Your reader must handle this: when you are in the middle of reassembling a fragmented text message and a ping frame arrives, respond to the ping immediately, then continue reading continuation frames. Do not mix control frame data into the message buffer.
</details>

<details>
<summary>Hint 4: permessage-deflate trailing bytes</summary>

RFC 7692 requires removing the trailing `0x00 0x00 0xFF 0xFF` from compressed data before sending, and appending it back before decompressing. The DEFLATE stream is a series of blocks; this 4-byte sequence is a BFINAL=0 empty stored block that acts as a sync marker. If you forget to add it back before decompressing, `compress/flate` will return an unexpected EOF.
</details>

## Acceptance Criteria

- [ ] Server accepts TCP connections and completes the HTTP upgrade handshake correctly
- [ ] A standard WebSocket client (browser or `websocat`) can connect and exchange text messages
- [ ] Server correctly parses frames with all three payload length encodings (7-bit, 16-bit, 64-bit)
- [ ] Server unmasks client frames and does not mask server frames
- [ ] Fragmented messages are reassembled into complete messages before delivery to application code
- [ ] Control frames interleaved with fragments are handled correctly (ping response during fragmentation)
- [ ] Close handshake completes properly: server echoes close frame with status code, then closes TCP
- [ ] Ping frames receive pong responses with the same payload
- [ ] Per-message compression works: compressed frames from a client are decompressed, server sends compressed frames
- [ ] Multiple concurrent clients can connect and all receive broadcast messages
- [ ] Heartbeat disconnects unresponsive clients after the timeout period
- [ ] No goroutine leaks after client disconnection

## Research Resources

- [RFC 6455: The WebSocket Protocol](https://datatracker.ietf.org/doc/html/rfc6455) -- the complete protocol specification, Section 5 covers framing in detail
- [RFC 7692: Compression Extensions for WebSocket](https://datatracker.ietf.org/doc/html/rfc7692) -- permessage-deflate negotiation and frame transformation
- [MDN: Writing a WebSocket Server](https://developer.mozilla.org/en-US/docs/Web/API/WebSockets_API/Writing_WebSocket_servers) -- accessible walkthrough of the handshake and framing
- [websocat](https://github.com/vi/websocat) -- command-line WebSocket client for testing, supports binary frames and compression
- [Autobahn TestSuite](https://github.com/crossbario/autobahn-testsuite) -- the industry-standard WebSocket compliance test suite with 500+ test cases
- [Go: compress/flate package](https://pkg.go.dev/compress/flate) -- DEFLATE compression/decompression for permessage-deflate
